package cloud

import (
	"strings"
	"testing"
	"time"

	// tzdata is embedded so the DST cases below run everywhere, including a
	// scratch container with no /usr/share/zoneinfo. Skipping them there would
	// hide exactly the failure they exist to catch, since the spring-forward
	// bug is invisible on any other day of the year.
	_ "time/tzdata"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// These tests are pure: no database, no provider, no clock. They cover the
// rules that decide WHEN the feature is allowed to start spending.

func strPtr(v string) *string { return &v }

// seededDefaultRules mirrors the system-default row the migration inserts
// (0, 180, 900, 0, no window, UTC).
func seededDefaultRules() *models.CloudProvisioningRules {
	return &models.CloudProvisioningRules{
		MinJobPriority:               intPtr(0),
		MinStarvationSeconds:         intPtr(180),
		SkipIfFinishingWithinSeconds: intPtr(900),
		MaxSpendPerJobCents:          i64Ptr(0),
		ProvisioningWindowTZ:         strPtr("UTC"),
	}
}

// healthyInput is a job that passes every rule in seededDefaultRules: high
// priority, long starved, no spend yet, and a long way from finishing. Each
// test perturbs one field so a refusal can only come from the rule under test.
func healthyInput() ProvisioningInput {
	return ProvisioningInput{
		JobExecutionID:         uuid.New(),
		ClientID:               uuid.New(),
		Priority:               500,
		StarvingFor:            10 * time.Minute,
		StarvationTracked:      true,
		JobSpendCommittedCents: 0,
		SpendReadable:          true,
		NextLaunchReserveCents: 0,
		TimeToFinish:           6 * time.Hour,
		HaveThroughput:         true,
		RemainingBase:          1_000_000_000,
		ProjectionAvailable:    true,
		Now:                    time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestDecideProvisioningActionPriorityFloor(t *testing.T) {
	tests := []struct {
		name      string
		floor     *int
		priority  int
		wantAllow bool
	}{
		{"floor not configured", nil, 0, true},
		{"floor off (in-band zero)", intPtr(0), 0, true},
		{"below the floor", intPtr(700), 699, false},
		// Inclusive, matching the budget ladder: exactly at the floor qualifies.
		{"exactly at the floor", intPtr(700), 700, true},
		{"above the floor", intPtr(700), 1000, true},
		{"floor of one excludes an unprioritised job", intPtr(1), 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules := seededDefaultRules()
			rules.MinJobPriority = tc.floor
			in := healthyInput()
			in.Priority = tc.priority

			allowed, reason := decideProvisioningAction(in, rules)
			assertDecision(t, allowed, reason, tc.wantAllow)
			if !allowed && !strings.Contains(reason, "min_job_priority") {
				t.Errorf("refusal must name the rule and its value, got %q", reason)
			}
		})
	}
}

func TestDecideProvisioningActionStarvation(t *testing.T) {
	tests := []struct {
		name      string
		minSecs   *int
		starving  time.Duration
		tracked   bool
		wantAllow bool
	}{
		{"rule not configured", nil, 0, true, true},
		{"rule off (in-band zero)", intPtr(0), 0, true, true},
		{"starved too briefly", intPtr(180), 60 * time.Second, true, false},
		{"one second short", intPtr(180), 179 * time.Second, true, false},
		// Inclusive: exactly the configured age is enough.
		{"exactly at the threshold", intPtr(180), 180 * time.Second, true, true},
		{"well past the threshold", intPtr(180), time.Hour, true, true},

		// The manual admin route has no starvation observation to age. Failing
		// the rule there would make it unbypassable, which it is not meant to
		// be, so an untracked input SKIPS it rather than failing it.
		{"untracked skips the rule entirely", intPtr(180), 0, false, true},
		{"untracked skips even a huge threshold", intPtr(86_400), 0, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules := seededDefaultRules()
			rules.MinStarvationSeconds = tc.minSecs
			in := healthyInput()
			in.StarvingFor = tc.starving
			in.StarvationTracked = tc.tracked

			allowed, reason := decideProvisioningAction(in, rules)
			assertDecision(t, allowed, reason, tc.wantAllow)
			if !allowed && !strings.Contains(reason, "min_starvation_seconds") {
				t.Errorf("refusal must name the rule and its value, got %q", reason)
			}
		})
	}
}

func TestDecideProvisioningActionPerJobSpendCap(t *testing.T) {
	tests := []struct {
		name      string
		capCents  *int64
		committed int64
		reserve   int64
		readable  bool
		wantAllow bool
	}{
		{"cap not configured", nil, 999_999, 0, true, true},
		{"cap off (in-band zero)", i64Ptr(0), 999_999, 0, true, true},
		{"well under the cap", i64Ptr(10_000), 1_000, 0, true, true},
		{"one cent under the cap", i64Ptr(10_000), 9_999, 0, true, true},
		// Inclusive, as everywhere else: at the cap is over it.
		{"exactly at the cap", i64Ptr(10_000), 10_000, 0, true, false},
		{"past the cap", i64Ptr(10_000), 12_000, 0, true, false},

		// The launch under consideration counts before it happens, or the cap
		// is discovered one instance too late.
		{"reserve alone crosses the cap", i64Ptr(10_000), 6_000, 5_000, true, false},
		{"reserve exactly fills the cap", i64Ptr(10_000), 6_000, 4_000, true, false},
		{"reserve still leaves room", i64Ptr(10_000), 6_000, 3_999, true, true},
		{"unpriced reserve does not invent spend", i64Ptr(10_000), 6_000, 0, true, true},

		// An unreadable ceiling has to behave like an engaged one.
		{"unreadable spend with a cap configured refuses", i64Ptr(10_000), 0, 0, false, false},
		// ...but with the cap off there is no ceiling to be unsure about, the
		// same way checkGlobalCap returns on a disabled ceiling before it ever
		// asks the ledger.
		{"unreadable spend with the cap off allows", i64Ptr(0), 0, 0, false, true},
		{"unreadable spend with no cap configured allows", nil, 0, 0, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules := seededDefaultRules()
			rules.MaxSpendPerJobCents = tc.capCents
			in := healthyInput()
			in.JobSpendCommittedCents = tc.committed
			in.NextLaunchReserveCents = tc.reserve
			in.SpendReadable = tc.readable

			allowed, reason := decideProvisioningAction(in, rules)
			assertDecision(t, allowed, reason, tc.wantAllow)
			if !allowed && !strings.Contains(reason, "max_spend_per_job_cents") {
				t.Errorf("refusal must name the rule and its value, got %q", reason)
			}
		})
	}
}

/*
 * TestDecideProvisioningActionFinishingSoon is the highest-consequence table in
 * this file.
 *
 * TimeToFinish is zero in three unrelated situations, and only one of them means
 * "about to finish". Every row where throughput is unknown must ALLOW: a job
 * nothing is working on is precisely the job worth renting for, and refusing it
 * here would make cloud burst do nothing at all, silently, with no error logged.
 */
func TestDecideProvisioningActionFinishingSoon(t *testing.T) {
	const skipWithin = 900 // seconds, the seeded default

	tests := []struct {
		name          string
		timeToFinish  time.Duration
		haveThrough   bool
		projAvailable bool
		remainingBase int64
		ruleOff       bool
		wantAllow     bool
		wantReason    string
	}{
		{
			// The whole point of the feature: nothing is working this job, so
			// the estimator reports zero. That is "unknown", not "instant".
			name: "starving with no throughput", timeToFinish: 0, haveThrough: false,
			projAvailable: true, remainingBase: 1_000_000_000_000, wantAllow: true,
		},
		{
			// Estimator errored: no projection at all, so the rule cannot fire.
			name: "estimator unavailable", timeToFinish: 0, haveThrough: false,
			projAvailable: false, remainingBase: 0, wantAllow: true,
		},
		{
			name: "finishing well inside the window", timeToFinish: 300 * time.Second, haveThrough: true,
			projAvailable: true, remainingBase: 1_000_000, wantAllow: false,
			wantReason: "skip_if_finishing_within_seconds",
		},
		{
			name: "one second outside the window", timeToFinish: 901 * time.Second, haveThrough: true,
			projAvailable: true, remainingBase: 1_000_000_000, wantAllow: true,
		},
		{
			// Inclusive, matching every other threshold here.
			name: "exactly at the window", timeToFinish: 900 * time.Second, haveThrough: true,
			projAvailable: true, remainingBase: 1_000_000_000, wantAllow: false,
			wantReason: "skip_if_finishing_within_seconds",
		},
		{
			// The one zero that really does mean "no work": refused by the
			// non-configurable keyspace guard, not by the skip window.
			name: "no remaining keyspace", timeToFinish: 0, haveThrough: true,
			projAvailable: true, remainingBase: 0, wantAllow: false,
			wantReason: "already assigned to existing tasks",
		},
		{
			name: "inside the window but the rule is off", timeToFinish: 300 * time.Second, haveThrough: true,
			projAvailable: true, remainingBase: 1_000_000, ruleOff: true, wantAllow: true,
		},

		/*
		 * The three rows below exist to make each guard INDEPENDENTLY
		 * load-bearing. Without them the conditions mutually shadow: every
		 * haveThrough:false row above also has timeToFinish 0, and the only
		 * projAvailable:false row does too, so `in.HaveThroughput` and
		 * `in.ProjectionAvailable` could both be deleted from the rule and this
		 * table would stay green — while cloud burst silently stopped
		 * provisioning for every job whose estimator returns a duration it does
		 * not trust. Each row below fails if its own guard is removed.
		 */
		{
			// Pins HaveThroughput: a positive duration that is NOT trusted,
			// because no agent is currently working the job. Reachable whenever
			// the estimator has a stale or partial speed row.
			name: "positive duration but no throughput", timeToFinish: 300 * time.Second, haveThrough: false,
			projAvailable: true, remainingBase: 1_000_000_000, wantAllow: true,
		},
		{
			// Pins ProjectionAvailable: a duration left over in the struct when
			// the estimator could not run at all must not be acted on.
			name: "positive duration but no projection", timeToFinish: 300 * time.Second, haveThrough: true,
			projAvailable: false, remainingBase: 1_000_000_000, wantAllow: true,
		},
		{
			// Pins TimeToFinish > 0 on its own, with both trust flags set and
			// real work remaining — the one combination the other rows never
			// produce.
			//
			// The residual is deliberate and small: a genuinely sub-second
			// projection floors to zero here and is allowed through, so the
			// autoscaler may briefly consider a job that is about to finish.
			// The alternative — treating zero as a duration — reintroduces
			// exactly the "no throughput reads as instant" failure this rule
			// exists to prevent, and that one refuses forever rather than
			// costing one re-evaluation.
			name: "sub-second projection floors to zero", timeToFinish: 0, haveThrough: true,
			projAvailable: true, remainingBase: 1_000_000_000, wantAllow: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules := seededDefaultRules()
			if tc.ruleOff {
				rules.SkipIfFinishingWithinSeconds = intPtr(0)
			} else {
				rules.SkipIfFinishingWithinSeconds = intPtr(skipWithin)
			}

			in := healthyInput()
			in.TimeToFinish = tc.timeToFinish
			in.HaveThroughput = tc.haveThrough
			in.ProjectionAvailable = tc.projAvailable
			in.RemainingBase = tc.remainingBase

			allowed, reason := decideProvisioningAction(in, rules)
			assertDecision(t, allowed, reason, tc.wantAllow)
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.wantReason)
			}
		})
	}
}

// TestDecideProvisioningActionNoRemainingKeyspaceIsNotConfigurable pins the one
// rule an admin cannot switch off: a job with no work left is not worth renting
// for at any setting, including with the skip window disabled.
func TestDecideProvisioningActionNoRemainingKeyspace(t *testing.T) {
	rules := seededDefaultRules()
	rules.SkipIfFinishingWithinSeconds = intPtr(0)

	in := healthyInput()
	in.RemainingBase = 0
	in.TimeToFinish = 0

	allowed, reason := decideProvisioningAction(in, rules)
	if allowed {
		t.Fatal("a job with no remaining keyspace must be refused even with the skip rule off")
	}
	// The message must describe UNDISPATCHED keyspace, not a finished job: this
	// state is also reached by a job stuck behind tasks on dead agents, which
	// the UI is still showing as part-way done.
	if !strings.Contains(reason, "already assigned to existing tasks") {
		t.Errorf("reason = %q, want it to explain that the keyspace is spoken for", reason)
	}
	if strings.Contains(reason, "no remaining") {
		t.Errorf("reason = %q must not tell an operator their unfinished job has no work left", reason)
	}

	// Without a projection the count is not trustworthy, so the guard must not
	// fire on a zero that is simply unknown.
	in.ProjectionAvailable = false
	if allowed, _ := decideProvisioningAction(in, rules); !allowed {
		t.Error("an unavailable projection must not be read as an empty keyspace")
	}
}

/*
 * TestDecideProvisioningActionRejectsAZeroNow guards the one observation with no
 * companion flag.
 *
 * time.Time{} is 00:00:00, which is inside every midnight-wrapping window — and
 * midnight-wrapping is how "only rent overnight" is written. A caller that
 * forgot to set Now would therefore find the window OPEN on precisely the
 * configurations an operator wrote to keep it shut, and only on those.
 */
func TestDecideProvisioningActionRejectsAZeroNow(t *testing.T) {
	rules := seededDefaultRules()
	rules.ProvisioningWindowStart = strPtr("22:00:00")
	rules.ProvisioningWindowEnd = strPtr("06:00:00")
	rules.ProvisioningWindowTZ = strPtr("UTC")

	in := healthyInput()
	in.Now = time.Time{}

	allowed, reason := decideProvisioningAction(in, rules)
	if allowed {
		t.Error("a zero Now sits inside every overnight window; it must refuse rather than " +
			"silently report the window open")
	}
	if !strings.Contains(reason, "current time") {
		t.Errorf("refusal should name the missing observation, got %q", reason)
	}

	// With no window configured the clock is never consulted, so a zero Now is
	// harmless and must not block anything.
	noWindow := seededDefaultRules()
	noWindow.ProvisioningWindowStart = nil
	noWindow.ProvisioningWindowEnd = nil
	if allowed, reason := decideProvisioningAction(in, noWindow); !allowed {
		t.Errorf("with no window configured a zero Now is irrelevant, refused with %q", reason)
	}
}

/*
 * TestWithinWindowOffValueIgnoresTheTimezone pins the evaluation ORDER.
 *
 * start == end means "always", so no zone can change the answer. Consulting one
 * first makes an unreadable zone refuse a window the operator explicitly
 * switched off — and on a host with no zone database that is every window, so a
 * client row of 00:00:00-00:00:00 would get zero provisioning permanently, with
 * the admin unable to correct it through an API that validates zones the same
 * way.
 */
func TestWithinWindowOffValueIgnoresTheTimezone(t *testing.T) {
	inside, err := withinWindow(time.Now(), "00:00:00", "00:00:00", "Not/AZone")
	if err != nil {
		t.Errorf("a disabled window must not consult the zone database, got %v", err)
	}
	if !inside {
		t.Error("start == end is the in-band OFF value and means always open")
	}

	// A real window with an unreadable zone still fails closed.
	if inside, err := withinWindow(time.Now(), "22:00:00", "06:00:00", "Not/AZone"); err == nil || inside {
		t.Error("a configured window with an unreadable zone must refuse")
	}
}

/*
 * TestDecideProvisioningActionSeparatesUnresolvedFromUnconfigured pins the one
 * asymmetry in this function: nil and empty are NOT the same answer.
 *
 * They are easy to conflate because every rail is skipped on a nil pointer, so
 * an empty struct reads as "allow". But nil arrives only from a failed
 * resolution — MergeProvisioningRules returns it when the system-default row is
 * missing — and inheriting the empty struct's behaviour there would hand the
 * autoscaler unrestricted spending permission precisely when the table is
 * broken. If someone ever "simplifies" this by defaulting nil to an empty
 * value, this test is what stops it.
 */
func TestDecideProvisioningActionSeparatesUnresolvedFromUnconfigured(t *testing.T) {
	in := healthyInput()
	// Everything that could possibly be constrained, at a value some configured
	// rule would refuse.
	in.Priority = 0
	in.StarvingFor = 0
	in.JobSpendCommittedCents = 1_000_000
	in.TimeToFinish = time.Second

	// Unresolved: refuse, and say so in a way an operator can act on.
	allowed, reason := decideProvisioningAction(in, nil)
	if allowed {
		t.Error("nil rules mean the policy could not be read; that must refuse, not allow everything")
	}
	if !strings.Contains(reason, "system default row") {
		t.Errorf("refusal must point at the missing row an operator has to fix, got %q", reason)
	}

	// Unconfigured: an admin's actual choice that every rail is off. Allow.
	if allowed, reason := decideProvisioningAction(in, &models.CloudProvisioningRules{}); !allowed {
		t.Errorf("unconfigured rules must constrain nothing, refused with %q", reason)
	}
}

/*
 * TestDecideProvisioningActionEvaluatesCheapestFirst pins the evaluation order.
 *
 * A job that fails the priority floor must report the priority refusal even
 * though it would also fail the window, the starvation age and the spend cap.
 * The order is not cosmetic: the caller gathers the expensive facts lazily in
 * the same sequence, so an early rule firing is what keeps a disqualified job
 * from costing an estimator query on every pass.
 */
func TestDecideProvisioningActionEvaluatesCheapestFirst(t *testing.T) {
	rules := &models.CloudProvisioningRules{
		MinJobPriority:               intPtr(700),
		MinStarvationSeconds:         intPtr(180),
		SkipIfFinishingWithinSeconds: intPtr(900),
		MaxSpendPerJobCents:          i64Ptr(1_000),
		ProvisioningWindowStart:      strPtr("22:00:00"),
		ProvisioningWindowEnd:        strPtr("06:00:00"),
		ProvisioningWindowTZ:         strPtr("UTC"),
	}

	in := healthyInput()
	in.Priority = 10                                       // fails the floor
	in.StarvingFor = time.Second                           // would fail starvation
	in.JobSpendCommittedCents = 5_000                      // would fail the spend cap
	in.TimeToFinish = 30 * time.Second                     // would fail finishing-soon
	in.Now = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) // would fail the window

	allowed, reason := decideProvisioningAction(in, rules)
	if allowed {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(reason, "min_job_priority") {
		t.Errorf("cheapest rule must win, got %q", reason)
	}

	// Clearing the priority problem should surface the next rule in order,
	// which is the window.
	in.Priority = 1000
	_, reason = decideProvisioningAction(in, rules)
	if !strings.Contains(reason, "provisioning window") {
		t.Errorf("window is second in order, got %q", reason)
	}
}

// TestDecideProvisioningActionWindowFailsClosed covers a window we cannot read.
// A constraint an operator configured and we cannot evaluate must be assumed
// real, or a typo in a timezone name becomes round-the-clock spending.
func TestDecideProvisioningActionWindowFailsClosed(t *testing.T) {
	rules := seededDefaultRules()
	rules.ProvisioningWindowStart = strPtr("22:00:00")
	rules.ProvisioningWindowEnd = strPtr("06:00:00")
	rules.ProvisioningWindowTZ = strPtr("Mars/Olympus_Mons")

	allowed, reason := decideProvisioningAction(healthyInput(), rules)
	if allowed {
		t.Fatal("an unreadable provisioning window must refuse, not silently allow")
	}
	if !strings.Contains(reason, "cannot be evaluated") {
		t.Errorf("reason = %q, want it to say the window could not be evaluated", reason)
	}
}

func TestWithinWindow(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 8, 21, h, m, 0, 0, time.UTC)
	}

	tests := []struct {
		name       string
		now        time.Time
		start, end string
		tz         string
		want       bool
	}{
		// start == end is the in-band OFF value: always open.
		{"equal bounds are always open", at(3, 0), "09:00:00", "09:00:00", "UTC", true},
		{"equal bounds at midnight", at(23, 59), "00:00:00", "00:00:00", "UTC", true},

		{"inside a normal range", at(12, 0), "09:00:00", "17:00:00", "UTC", true},
		{"before a normal range", at(8, 59), "09:00:00", "17:00:00", "UTC", false},
		{"after a normal range", at(17, 1), "09:00:00", "17:00:00", "UTC", false},
		// Half-open [start, end): the start instant is inside, the end is not.
		{"exactly at the start", at(9, 0), "09:00:00", "17:00:00", "UTC", true},
		{"exactly at the end", at(17, 0), "09:00:00", "17:00:00", "UTC", false},

		// end < start wraps midnight and is a UNION. Reading it as an empty set
		// would disable overnight bursting for everyone who configured it the
		// obvious way.
		{"wrap: late evening", at(23, 0), "22:00:00", "06:00:00", "UTC", true},
		{"wrap: exactly at the start", at(22, 0), "22:00:00", "06:00:00", "UTC", true},
		{"wrap: after midnight", at(2, 0), "22:00:00", "06:00:00", "UTC", true},
		{"wrap: just before the end", at(5, 59), "22:00:00", "06:00:00", "UTC", true},
		{"wrap: exactly at the end", at(6, 0), "22:00:00", "06:00:00", "UTC", false},
		{"wrap: midday is outside", at(12, 0), "22:00:00", "06:00:00", "UTC", false},

		// The window is evaluated in its own zone, which is the entire reason
		// the zone is stored: a UTC server expressing local business hours.
		{"zone shifts the answer", at(12, 0), "09:00:00", "17:00:00", "America/New_York", false},
		{"zone shifts the answer the other way", at(20, 0), "09:00:00", "17:00:00", "America/New_York", true},

		// Empty zone is UTC, matching LoadLocation and the seeded default.
		{"empty zone is UTC", at(12, 0), "09:00:00", "17:00:00", "", true},

		// HH:MM is what a human types in the admin UI; HH:MM:SS is what the
		// TIME column renders.
		{"minute precision parses", at(12, 0), "09:00", "17:00", "UTC", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withinWindow(tc.now, tc.start, tc.end, tc.tz)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("withinWindow(%s, %s-%s, %s) = %v, want %v",
					tc.now.Format(time.RFC3339), tc.start, tc.end, tc.tz, got, tc.want)
			}
		})
	}
}

// TestWithinWindowFailsClosed: an unreadable window must surface an error so the
// caller refuses. Returning true with an error would be the worst of both.
func TestWithinWindowFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		start, end string
		tz         string
	}{
		{"unparseable timezone", "09:00:00", "17:00:00", "Not/AZone"},
		{"garbage start", "not-a-time", "17:00:00", "UTC"},
		{"garbage end", "09:00:00", "", "UTC"},
		{"out of range hour", "25:00:00", "17:00:00", "UTC"},
		{"date instead of a time of day", "2026-08-21", "17:00:00", "UTC"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withinWindow(time.Now(), tc.start, tc.end, tc.tz)
			if err == nil {
				t.Fatal("expected an error so the caller fails closed")
			}
			if got {
				t.Error("must not report the window as open when it cannot be read")
			}
		})
	}
}

/*
 * TestWithinWindowAcrossDST is the regression for building absolute timestamps
 * out of local midnight plus a duration.
 *
 * On 2026-03-08 America/New_York skips 02:00-03:00. time.Date(…, 2, 30, …) in
 * that zone normalises to 03:30 EDT, so a "02:00-04:00" window built that way
 * silently starts an hour late — no error, twice a year, in whichever direction
 * the offset moved. Comparing wall clock to wall clock has no such case.
 */
func TestWithinWindowAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York must load: %v", err)
	}

	// Spring forward: 2026-03-08 02:00 EST becomes 03:00 EDT.
	spring := []struct {
		name       string
		now        time.Time
		start, end string
		want       bool
	}{
		{"before the skipped hour", time.Date(2026, 3, 8, 1, 45, 0, 0, ny), "01:30", "03:30", true},
		// 03:15 EDT: the wall clock reads 03:15, which is inside 01:30-03:30
		// however the offset got there.
		{"after the skipped hour", time.Date(2026, 3, 8, 3, 15, 0, 0, ny), "01:30", "03:30", true},
		{"past the end", time.Date(2026, 3, 8, 4, 0, 0, 0, ny), "01:30", "03:30", false},
		// A window entirely inside the hour that does not exist simply never
		// opens that day, which is what the operator who typed it would expect.
		{"window inside the skipped hour never opens", time.Date(2026, 3, 8, 3, 15, 0, 0, ny), "02:00", "02:30", false},
		{"window inside the skipped hour is open the next day", time.Date(2026, 3, 9, 2, 15, 0, 0, ny), "02:00", "02:30", true},
	}
	for _, tc := range spring {
		t.Run("spring/"+tc.name, func(t *testing.T) {
			got, err := withinWindow(tc.now, tc.start, tc.end, "America/New_York")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("withinWindow(%s, %s-%s) = %v, want %v",
					tc.now.Format(time.RFC3339), tc.start, tc.end, got, tc.want)
			}
		})
	}

	/*
	 * Fall back: 2026-11-01 01:30 happens TWICE, once at UTC-4 and once at
	 * UTC-5. Both are 01:30 on the wall clock, so both are inside a 01:00-02:00
	 * window. Constructed from UTC instants because time.Date cannot express the
	 * second occurrence — it resolves the ambiguity to the first.
	 */
	fall := []struct {
		name string
		now  time.Time
	}{
		{"first 01:30 (EDT)", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)},
		{"second 01:30 (EST)", time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)},
	}
	for _, tc := range fall {
		t.Run("fall/"+tc.name, func(t *testing.T) {
			if local := tc.now.In(ny).Format("15:04"); local != "01:30" {
				t.Fatalf("test setup: local time is %s, want 01:30", local)
			}
			got, err := withinWindow(tc.now, "01:00", "02:00", "America/New_York")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got {
				t.Error("both occurrences of an ambiguous wall-clock time are inside the window")
			}
		})
	}
}

// assertDecision checks the verdict and that a refusal always carries a reason:
// "provisioning blocked" with no numbers is how a feature becomes unsupportable
// at 3am.
func assertDecision(t *testing.T, allowed bool, reason string, wantAllow bool) {
	t.Helper()
	if allowed != wantAllow {
		t.Errorf("allowed = %v (%q), want %v", allowed, reason, wantAllow)
	}
	if allowed && reason != "" {
		t.Errorf("an allowed decision must carry no reason, got %q", reason)
	}
	if !allowed && reason == "" {
		t.Error("a refusal must explain itself")
	}
}
