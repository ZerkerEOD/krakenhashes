package cloud

import (
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

/*
 * ProvisioningInput is everything decideProvisioningAction is allowed to look
 * at: facts already gathered by the caller, and nothing that could reach a
 * database or a provider API.
 *
 * Deliberately values rather than a context and a set of repositories. The same
 * decision is made from the autoscaler's timer loop, from the manual admin
 * route, and from a dry-run preview endpoint; if the evaluator could issue its
 * own queries, every boundary case in the table below would be a round trip and
 * the three callers would silently diverge on which reads they had done first.
 *
 * The "...Available"/"...Readable"/"...Tracked" companions to each measurement
 * exist because zero is a legitimate value for all of them. Which way a missing
 * observation falls is decided per rule, and each one is argued at its rule.
 */
type ProvisioningInput struct {
	JobExecutionID uuid.UUID
	ClientID       uuid.UUID

	// Priority is job_executions.priority verbatim — an absolute number, the
	// same scale the scheduler dispatches on, never a percentage of the
	// configured ceiling.
	Priority int

	// StarvingFor is how long the job has been continuously starving.
	StarvingFor time.Duration
	// StarvationTracked is false when the caller has no starvation observation
	// at all, which SKIPS the starvation rule rather than failing it.
	StarvationTracked bool

	// JobSpendCommittedCents is cloud spend already committed to this job
	// execution over its whole life (reservations plus incurred), not windowed
	// to the current month.
	JobSpendCommittedCents int64
	// SpendReadable is false when that number could not be read. It REFUSES.
	SpendReadable bool
	// NextLaunchReserveCents is what the launch under consideration would
	// commit. Zero when the offer has not been priced yet, which only makes the
	// cap check looser, never wrong.
	NextLaunchReserveCents int64

	// TimeToFinish is the estimator's projection on EXISTING capacity. Read
	// only in company with HaveThroughput and ProjectionAvailable — see the
	// warning on the finishing-soon rule.
	TimeToFinish time.Duration
	// HaveThroughput reports that some agent is actually working this job, so
	// TimeToFinish was computed from a real speed.
	HaveThroughput bool
	// RemainingBase is remaining base keyspace.
	RemainingBase int64
	// ProjectionAvailable is false when the estimator could not run at all,
	// which SKIPS the projection rules.
	ProjectionAvailable bool

	// Now is passed rather than read from the clock so the provisioning window
	// is testable, including across a DST transition.
	Now time.Time
}

/*
 * decideProvisioningAction answers whether the admin rules permit renting paid
 * capacity for one job right now, and says why not when they do not.
 *
 * Modelled on decideBudgetAction: pure, table-testable, and the reason string is
 * surfaced verbatim to the operator. Where decideBudgetAction answers HOW MUCH
 * may be spent, this answers WHEN anything may be spent at all.
 *
 * Rules are evaluated CHEAPEST FIRST and the first refusal wins, so a job that
 * fails the priority floor never costs an estimator call or a spend query.
 *
 * That ordering is for the CALLER'S benefit, but it cannot be exploited by
 * calling this function early with a half-filled input. ProvisioningInput is a
 * plain value, so every fact must be gathered before the single call, and the
 * unset ones do not read as "not asked": SpendReadable false is a refusal, and
 * ProjectionAvailable false silently skips two rules. A caller wanting to skip
 * the expensive lookups should evaluate the cheap rules itself — priority
 * against MinJobPriority, the clock against the window — and only build the
 * full input once those pass.
 *
 * Thresholds are inclusive, matching the budget ladder: a job exactly at the
 * priority floor qualifies, and a job exactly at the skip window is skipped.
 */
func decideProvisioningAction(in ProvisioningInput, rules *models.CloudProvisioningRules) (allowed bool, reason string) {
	/*
	 * Nil rules REFUSE. It is the one input that does not mean "unconfigured".
	 *
	 * An empty *CloudProvisioningRules and a nil one look alike but arrive by
	 * opposite routes. Empty means every rail is genuinely off, which the merge
	 * produces from a system default whose columns are all NULL — a policy an
	 * admin chose. Nil is only reachable when the policy could NOT BE RESOLVED:
	 * MergeProvisioningRules returns it when the system-default row is missing,
	 * i.e. the migration did not run or something truncated the table.
	 *
	 * Since every rail here is skipped when its pointer is nil, treating the two
	 * alike converts a broken database into unrestricted spending permission —
	 * any job, any priority, any hour, no per-job cap — at exactly the moment
	 * nobody can see why. Same call as an unreadable spend figure below, and as
	 * checkGlobalCap on an unreadable ceiling: a policy we cannot read has to
	 * behave like an engaged one.
	 */
	if rules == nil {
		return false, "cloud provisioning rules could not be resolved (the system default row is missing); " +
			"refusing to provision until the rules are readable"
	}

	// 1. Priority floor. Free: the caller already has the priority it sorted on.
	if rules.MinJobPriority != nil && *rules.MinJobPriority > 0 && in.Priority < *rules.MinJobPriority {
		return false, fmt.Sprintf("job priority %d is below the cloud provisioning floor (min_job_priority %d)",
			in.Priority, *rules.MinJobPriority)
	}

	// 2. Provisioning window. Costs a timezone load and no I/O.
	if rules.ProvisioningWindowStart != nil && rules.ProvisioningWindowEnd != nil {
		/*
		 * A zero Now is the one observation in this struct whose zero value is
		 * not neutral, so it is rejected rather than used.
		 *
		 * Every other fact here has an explicit companion flag —
		 * ProjectionAvailable, SpendReadable, StarvationTracked — precisely so
		 * that "not gathered" is distinguishable from a real reading. Now has
		 * none, and time.Time{} is 00:00:00, which falls INSIDE every
		 * midnight-wrapping window and inside any window containing midnight.
		 * Since overnight windows are the common case ("only rent when the
		 * office is closed"), a caller that forgot to set Now would find the
		 * window open on exactly the configurations written to keep it shut.
		 */
		if in.Now.IsZero() {
			return false, "the current time was not supplied, so the provisioning window cannot be evaluated"
		}

		tz := ""
		if rules.ProvisioningWindowTZ != nil {
			tz = *rules.ProvisioningWindowTZ
		}
		inside, err := withinWindow(in.Now, *rules.ProvisioningWindowStart, *rules.ProvisioningWindowEnd, tz)
		if err != nil {
			// A window an operator configured and we cannot read is a
			// constraint we must assume is real. Allowing here would turn a
			// typo in a timezone name into round-the-clock spending.
			return false, fmt.Sprintf("provisioning window %s-%s (%s) cannot be evaluated: %v",
				*rules.ProvisioningWindowStart, *rules.ProvisioningWindowEnd, tzLabel(tz), err)
		}
		if !inside {
			return false, fmt.Sprintf("outside the provisioning window %s-%s %s (local time now %s)",
				*rules.ProvisioningWindowStart, *rules.ProvisioningWindowEnd, tzLabel(tz),
				clockInZone(in.Now, tz))
		}
	}

	/*
	 * 3. Starvation age.
	 *
	 * StarvationTracked == false SKIPS this rule instead of failing it. The
	 * manual admin route has no starvation observation to age — an operator
	 * pressing "rent capacity for this job" is not waiting three scheduler
	 * ticks — and failing there would make the rule unbypassable, which it was
	 * never meant to be. Only the autoscaler, which does track starvation, is
	 * held to it.
	 */
	if rules.MinStarvationSeconds != nil && *rules.MinStarvationSeconds > 0 && in.StarvationTracked {
		required := time.Duration(*rules.MinStarvationSeconds) * time.Second
		if in.StarvingFor < required {
			return false, fmt.Sprintf("job has been starving for %s (min_starvation_seconds %s)",
				in.StarvingFor.Round(time.Second), required)
		}
	}

	/*
	 * 4. Per-job spend cap.
	 *
	 * A cap of zero is the in-band OFF value, so nothing about spend is
	 * consulted and an unreadable figure does not matter — the same shape as
	 * checkGlobalCap, which returns on a disabled ceiling before it ever asks
	 * the ledger what has been committed.
	 *
	 * With a cap configured, an unreadable spend figure REFUSES. An unreadable
	 * ceiling has to behave like an engaged one: renting now could be exactly
	 * the launch that carries this job past the cap, and "we could not check"
	 * is not a reason to spend.
	 */
	if rules.MaxSpendPerJobCents != nil && *rules.MaxSpendPerJobCents > 0 {
		capCents := *rules.MaxSpendPerJobCents
		if !in.SpendReadable {
			return false, fmt.Sprintf("cannot read committed cloud spend for this job to check it "+
				"against its per-job cap (max_spend_per_job_cents %d)", capCents)
		}
		// The launch under consideration counts against the cap before it
		// happens, for the same reason the budget engine reserves before
		// launching rather than accruing after: a cap enforced only on money
		// already spent is discovered one instance too late.
		projected := in.JobSpendCommittedCents + in.NextLaunchReserveCents
		if projected >= capCents {
			return false, fmt.Sprintf("job cloud spend at %d cents committed plus %d reserved for this "+
				"launch = %d, at or over the per-job cap (max_spend_per_job_cents %d)",
				in.JobSpendCommittedCents, in.NextLaunchReserveCents, projected, capCents)
		}
	}

	// 5a. Nothing left to work on. Not an admin-configurable rule: a job with no
	//     remaining base keyspace is never worth renting for at any setting.
	//     Placed ahead of the finishing-soon rule so it consumes the one
	//     zero-TimeToFinish case that genuinely does mean "there is no work".
	if in.ProjectionAvailable && in.RemainingBase <= 0 {
		// "Undispatched", not "finished": the estimator subtracts every interval
		// whose task is not failed, so a job whose whole range is held by tasks
		// on stalled or disconnected agents lands here while the UI shows it at
		// 40%. The refusal is right — a new instance would have nothing to take
		// until the reaper fails those intervals — but the operator reading this
		// sentence must not be told their job is complete.
		return false, "all of this job's remaining keyspace is already assigned to existing tasks; " +
			"a new instance would have nothing to work on"
	}

	/*
	 * 5b. Finishing soon.
	 *
	 * WARNING TO THE NEXT READER: every one of the four conditions below is
	 * load-bearing. TimeToFinish == 0 does NOT mean "finishes immediately". It
	 * is also what the estimator produces when no agent is working the job at
	 * all — which is EXACTLY the starving job this whole feature exists to rent
	 * for — and what a floored big.Int division produces for a sub-second
	 * projection.
	 *
	 * Drop HaveThroughput (or ProjectionAvailable, or the > 0) and this rule
	 * reads "0s, comfortably inside any skip window" and refuses to provision
	 * at the precise moment provisioning is needed. It does so silently, on
	 * every pass, and the symptom an operator reports is "cloud burst does
	 * nothing" with no error logged anywhere.
	 *
	 * So the rule fires ONLY on a positive duration we trust. Unknown
	 * throughput always falls through to allow, and the launch is then bounded
	 * by the budget rails rather than by a guess.
	 */
	if rules.SkipIfFinishingWithinSeconds != nil && *rules.SkipIfFinishingWithinSeconds > 0 &&
		in.ProjectionAvailable && in.HaveThroughput && in.TimeToFinish > 0 {
		window := time.Duration(*rules.SkipIfFinishingWithinSeconds) * time.Second
		if in.TimeToFinish <= window {
			return false, fmt.Sprintf("job is projected to finish in %s on existing capacity "+
				"(skip_if_finishing_within_seconds %s)", in.TimeToFinish.Round(time.Second), window)
		}
	}

	return true, ""
}

/*
 * withinWindow reports whether now falls inside a wall-clock provisioning
 * window, evaluated in tz.
 *
 *   start == end : ALWAYS. This is the window's in-band OFF value.
 *   end < start  : WRAPS MIDNIGHT and is a UNION, not an empty set.
 *                  "22:00-06:00" is the normal way to say "only rent overnight",
 *                  and reading it as empty would silently disable overnight
 *                  bursting for everyone who configured it the obvious way.
 *
 * The comparison is on TIME OF DAY in the location, deliberately NOT on absolute
 * timestamps built by adding a duration to local midnight. That construction
 * breaks twice a year: on a spring-forward day 02:30 does not exist, and
 * time.Date normalises it to 03:30 in the new offset, shifting the entire window
 * by an hour without any error to notice. Comparing wall clock to wall clock has
 * no such case — a window inside the skipped hour simply never opens that day,
 * which is what an operator reading "02:00-02:30" would expect.
 *
 * The interval is half-open, [start, end): a launch exactly at the start time is
 * inside, exactly at the end time is not. That keeps the midnight-wrapping union
 * from overlapping itself at the boundaries.
 *
 * Any error here must be treated as a refusal by the caller.
 */
func withinWindow(now time.Time, start, end string, tz string) (bool, error) {
	startOfDay, err := parseClock(start)
	if err != nil {
		return false, fmt.Errorf("window start: %w", err)
	}
	endOfDay, err := parseClock(end)
	if err != nil {
		return false, fmt.Errorf("window end: %w", err)
	}

	/*
	 * The OFF value is answered BEFORE the zone is loaded, and the order is
	 * load-bearing.
	 *
	 * start == end means "always", so no zone can change the answer — and
	 * consulting one anyway makes an unreadable zone refuse a window the
	 * operator explicitly switched off. That is the same doctrine the spend cap
	 * follows above: a cap of zero is off, so nothing about spend is consulted
	 * and an unreadable figure cannot matter.
	 *
	 * It is not hypothetical. A host with no /usr/share/zoneinfo fails to load
	 * every IANA name, so with the zone checked first a client row of
	 * 00:00:00-00:00:00 ("always open") yields "provisioning window cannot be
	 * evaluated" and zero provisioning, permanently — and UpsertRules validates
	 * zone names the same way, so an admin cannot even correct the row through
	 * the API. Answering the OFF case first means an operator who has disabled
	 * the window is never exposed to the zone database at all.
	 */
	if startOfDay == endOfDay {
		return true, nil
	}

	// LoadLocation("") is UTC, matching the column's default and the seeded
	// system-default row.
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false, fmt.Errorf("unknown timezone %q: %w", tz, err)
	}

	local := now.In(loc)
	nowOfDay := time.Duration(local.Hour())*time.Hour +
		time.Duration(local.Minute())*time.Minute +
		time.Duration(local.Second())*time.Second

	if startOfDay < endOfDay {
		return nowOfDay >= startOfDay && nowOfDay < endOfDay, nil
	}
	return nowOfDay >= startOfDay || nowOfDay < endOfDay, nil
}

// parseClock converts a time-of-day string to its offset from midnight.
//
// Both layouts are accepted because the value arrives from two places: a
// Postgres TIME column, which renders as HH:MM:SS, and an admin API payload,
// where HH:MM is what a human types.
func parseClock(v string) (time.Duration, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.Parse(layout, v); err == nil {
			return time.Duration(t.Hour())*time.Hour +
				time.Duration(t.Minute())*time.Minute +
				time.Duration(t.Second())*time.Second, nil
		}
	}
	return 0, fmt.Errorf("unparseable time of day %q (want HH:MM or HH:MM:SS)", v)
}

// tzLabel renders an empty timezone as UTC, which is what LoadLocation does with
// it, so a refusal message never reads "window 22:00-06:00 () ".
func tzLabel(tz string) string {
	if tz == "" {
		return "UTC"
	}
	return tz
}

// clockInZone renders now as wall-clock time in tz for a refusal message. Falls
// back to UTC on an unloadable zone: withinWindow has already accepted the zone
// by the time this is reached, so the fallback exists only so that formatting a
// message can never be the thing that fails.
func clockInZone(now time.Time, tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return now.UTC().Format("15:04:05") + " UTC"
	}
	return now.In(loc).Format("15:04:05")
}
