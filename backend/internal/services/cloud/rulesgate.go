package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

/*
 * This file connects decideProvisioningAction to the two places that can start
 * spending, and it is deliberately the only place that decides WHICH rules
 * apply WHERE.
 *
 * The split is soft versus hard, not cheap versus expensive:
 *
 *   SOFT (priority floor, finishing-soon) filter the AUTOSCALER'S candidate
 *   set. They mean "not worth spending on automatically". An operator clicking
 *   Provision has made that judgement by hand, so these must not block them.
 *
 *   HARD (spend cap, time window) gate EVERY path including the manual one. A
 *   per-job spend cap an admin can click past is not a spend cap, and a
 *   provisioning window normally encodes something external — a contract
 *   clause, a change freeze — rather than an operator preference.
 *
 *   MIN STARVATION is autoscaler-only for a different reason: the state does
 *   not exist in the database and there is no starvation observation to age on
 *   the manual path, so enforcing it there would make the rule unbypassable,
 *   which it was never meant to be.
 *
 * Every site narrows the rules object to the rules it enforces, rather than
 * re-implementing any of them. A nil pointer means "rule not configured", so
 * narrowing is just clearing the fields this site does not own, and the
 * semantics that actually matter — in-band zero as OFF, inclusive thresholds,
 * fail-closed on an unreadable figure — stay in the one evaluator.
 */

// resolveRules loads the rules in force for a client, failing closed.
//
// GetRules already errors when the system-default row is missing; this adds the
// belt-and-braces nil check so a future change to that contract cannot silently
// hand an all-nil (maximally permissive) policy to a caller.
func (s *Service) resolveRules(ctx context.Context, clientID uuid.UUID) (*models.CloudProvisioningRules, error) {
	if s.rules == nil {
		return nil, fmt.Errorf("cloud provisioning rules repository is not wired in")
	}
	rules, err := s.rules.GetRules(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("read cloud provisioning rules for client %s: %w", clientID, err)
	}
	if rules == nil {
		return nil, fmt.Errorf("cloud provisioning rules for client %s resolved to nothing", clientID)
	}
	return rules, nil
}

/*
 * jobCommittedCents totals cloud spend attributable to ONE job execution over
 * its whole life.
 *
 * The join runs through cloud_instances rather than cloud_spend_ledger's own
 * job_execution_id, which looks like the obvious column and is a trap: only
 * `reservation` rows carry it, so filtering on it sums GROSS reservations with
 * no releases netted out. Every job whose instance terminated early — the
 * common case, since TTL and idle drain both release — would over-report, and a
 * per-job cap fed that number refuses launches for money that was handed back.
 *
 * Deliberately NOT month-windowed like cloud_spend_ledger's budget window: a
 * per-job cap that resets at a month boundary is not a per-job cap.
 */
func (s *Service) jobCommittedCents(ctx context.Context, jobID uuid.UUID) (int64, error) {
	var cents int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(l.cents), 0)
		FROM cloud_spend_ledger l
		JOIN cloud_instances ci ON ci.id = l.cloud_instance_id
		WHERE ci.job_execution_id = $1
		  AND l.kind IN ('reservation','release','reconciliation')`, jobID).Scan(&cents)
	if err != nil {
		return 0, fmt.Errorf("read committed cloud spend for job %s: %w", jobID, err)
	}
	return cents, nil
}

/*
 * checkProvisioningRules enforces the HARD rails before anything costs money.
 *
 * Called from ProvisionForJob, so it covers the autoscaler and the manual admin
 * route alike. The spend cap is evaluated here with a zero reserve, which
 * catches a job already at or past its ceiling; the launch's own cost is
 * checked again in attemptLaunch once the offer is priced, because that is the
 * first moment the reserve amount exists.
 */
func (s *Service) checkProvisioningRules(ctx context.Context, jobID, clientID uuid.UUID) error {
	rules, err := s.resolveRules(ctx, clientID)
	if err != nil {
		return err
	}

	in := ProvisioningInput{
		JobExecutionID: jobID,
		ClientID:       clientID,
		Now:            time.Now(),
	}

	// Narrow to the hard rules. The soft ones belong to CloudEligibleJobs and
	// must not fire on the manual path; starvation has nothing to age here.
	hard := *rules
	hard.MinJobPriority = nil
	hard.MinStarvationSeconds = nil
	hard.SkipIfFinishingWithinSeconds = nil

	if hard.MaxSpendPerJobCents != nil && *hard.MaxSpendPerJobCents > 0 {
		committed, err := s.jobCommittedCents(ctx, jobID)
		if err != nil {
			// SpendReadable false makes the evaluator refuse, which is the
			// point: renting now could be the launch that carries this job past
			// its cap, and "we could not check" is not a reason to spend.
			debug.Error("Cloud: cannot read committed spend for job %s: %v", jobID, err)
		} else {
			in.SpendReadable = true
			in.JobSpendCommittedCents = committed
		}
	} else {
		// No cap configured, so the figure is never consulted. Asserting it is
		// readable keeps the input honest rather than relying on a rule that
		// happens to be skipped.
		in.SpendReadable = true
	}

	if allowed, reason := decideProvisioningAction(in, &hard); !allowed {
		return fmt.Errorf("cloud provisioning rules refuse this job: %s", reason)
	}
	return nil
}

/*
 * checkJobSpendCap re-checks the per-job ceiling once the launch has a price.
 *
 * Split from checkProvisioningRules because the reserve amount does not exist
 * until an offer has been ranked and PlanLaunch has sized the reservation. A
 * cap enforced only on money already spent is discovered one instance too late,
 * so the launch under consideration has to count against the ceiling BEFORE it
 * happens — the same reason the budget engine reserves rather than accruing.
 */
func (s *Service) checkJobSpendCap(ctx context.Context, jobID, clientID uuid.UUID, reserveCents int64) error {
	rules, err := s.resolveRules(ctx, clientID)
	if err != nil {
		return err
	}
	if rules.MaxSpendPerJobCents == nil || *rules.MaxSpendPerJobCents <= 0 {
		return nil
	}

	in := ProvisioningInput{
		JobExecutionID:         jobID,
		ClientID:               clientID,
		Now:                    time.Now(),
		NextLaunchReserveCents: reserveCents,
	}
	if committed, err := s.jobCommittedCents(ctx, jobID); err != nil {
		debug.Error("Cloud: cannot read committed spend for job %s: %v", jobID, err)
	} else {
		in.SpendReadable = true
		in.JobSpendCommittedCents = committed
	}

	// Only the cap. Re-running the window here would let a launch that started
	// inside the window fail because ranking took it past the boundary.
	capOnly := models.CloudProvisioningRules{MaxSpendPerJobCents: rules.MaxSpendPerJobCents}
	if allowed, reason := decideProvisioningAction(in, &capOnly); !allowed {
		return fmt.Errorf("cloud provisioning rules refuse this launch: %s", reason)
	}
	return nil
}

/*
 * applySoftRules filters the autoscaler's candidate list.
 *
 * Runs per job with rules resolved once per CLIENT, because a pass over a busy
 * queue is dominated by jobs belonging to a handful of clients and GetRules is
 * a round trip each time.
 *
 * A job whose rules cannot be read is DROPPED rather than passed through.
 * Dropping costs a delayed launch; passing through spends money under a policy
 * nobody could read.
 */
func (s *Service) applySoftRules(ctx context.Context, jobs []EligibleJob) []EligibleJob {
	byClient := make(map[uuid.UUID]*models.CloudProvisioningRules, len(jobs))

	out := jobs[:0]
	for _, job := range jobs {
		rules, ok := byClient[job.ClientID]
		if !ok {
			resolved, err := s.resolveRules(ctx, job.ClientID)
			if err != nil {
				debug.Error("Cloud autoscaler: cannot read provisioning rules for client %s; "+
					"skipping its jobs this pass: %v", job.ClientID, err)
				byClient[job.ClientID] = nil
				continue
			}
			byClient[job.ClientID] = resolved
			rules = resolved
		}
		if rules == nil {
			continue
		}

		/*
		 * Starvation is carried to the autoscaler rather than evaluated here.
		 *
		 * This function has no starvation ages — they live in the snapshot the
		 * autoscaler owns — and fetching them here would sample a signal the
		 * scheduler republishes every 3 seconds at the point a database query
		 * happens to run. Handing the threshold along keeps the comparison
		 * where the observation is.
		 */
		if rules.MinStarvationSeconds != nil {
			job.MinStarvationSeconds = *rules.MinStarvationSeconds
		}

		in := ProvisioningInput{
			JobExecutionID: job.JobExecutionID,
			ClientID:       job.ClientID,
			Priority:       job.Priority,
			Now:            time.Now(),
			// The hard rules are enforced in ProvisionForJob; asserting the
			// spend figure is readable here keeps this narrowed input from
			// tripping a rule it does not own.
			SpendReadable: true,
		}

		soft := models.CloudProvisioningRules{
			MinJobPriority:               rules.MinJobPriority,
			SkipIfFinishingWithinSeconds: rules.SkipIfFinishingWithinSeconds,
		}

		/*
		 * The projection is fetched ONLY when the finishing-soon rule is armed,
		 * and only after the priority floor has already passed.
		 *
		 * Project() is several joins and an aggregate over every scheduling
		 * unit of the job, so running it for a job that the floor rejects for
		 * free is pure waste on every 60-second pass.
		 */
		if soft.SkipIfFinishingWithinSeconds != nil && *soft.SkipIfFinishingWithinSeconds > 0 {
			if allowed, _ := decideProvisioningAction(
				ProvisioningInput{Priority: job.Priority, Now: in.Now, SpendReadable: true},
				&models.CloudProvisioningRules{MinJobPriority: rules.MinJobPriority},
			); allowed {
				s.fillProjection(ctx, job.JobExecutionID, &in)
			}
		}

		if allowed, reason := decideProvisioningAction(in, &soft); !allowed {
			debug.Debug("Cloud autoscaler: job %s not eligible for automatic provisioning: %s",
				job.JobExecutionID, reason)
			continue
		}
		out = append(out, job)
	}
	return out
}

// fillProjection populates the estimator-derived fields, leaving
// ProjectionAvailable false when the estimate cannot be produced — which SKIPS
// the finishing-soon rule rather than failing it, since a job we cannot project
// is not a job we know is about to finish.
func (s *Service) fillProjection(ctx context.Context, jobID uuid.UUID, in *ProvisioningInput) {
	if s.estimator == nil {
		return
	}
	// Zero extra speed and zero rate: this asks "how long on EXISTING capacity",
	// which is the question the rule poses. Costing is the budget engine's job.
	proj, err := s.estimator.Project(ctx, jobID, 0, 0, 0)
	if err != nil {
		debug.Warning("Cloud autoscaler: could not project job %s (%v); "+
			"the finishing-soon rule will not fire for it this pass", jobID, err)
		return
	}
	in.ProjectionAvailable = true
	in.RemainingBase = proj.RemainingBase
	in.TimeToFinish = proj.TimeToFinish
	in.HaveThroughput = proj.TimeToFinishKnown
}
