package cloud

import (
	"math"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/scheduler"
)

/*
 * Drift guards for the minimum-rental floor, not unit tests.
 *
 * budget.go cannot import scheduler — the dependency runs the other way, and
 * adding that edge would make the scheduler unbuildable without the cloud
 * package — so commissioningBudget RESTATES a number scheduler owns. A restated
 * constant is only safe while something proves it still matches, and this is
 * that something.
 *
 * What goes wrong without it is quiet and expensive. Shorten the scheduler's
 * sync grace or benchmark window and commissioningBudget silently overstates
 * what setup costs, so every rental is floored longer than it needs to be and
 * budgets buy fewer parallel instances. LENGTHEN them and it understates,
 * which is the damaging direction: the floor drops below what an instance needs
 * to reach its first task, and the engine goes back to selling rentals that
 * expire mid-commissioning — exactly the waste the floor replaced a hardcoded
 * five minutes to prevent.
 *
 * A _test.go file adds no edge to the production build, which is the same
 * reasoning commissioning_test.go relies on for the CommissioningGrace guard.
 */
func TestCommissioningBudgetMatchesSchedulerReadinessBudget(t *testing.T) {
	readiness := scheduler.ReadinessBudget()

	if readiness <= 0 {
		t.Fatalf("scheduler.ReadinessBudget() = %s; a non-positive budget makes this guard vacuous", readiness)
	}

	if commissioningBudget != readiness {
		t.Errorf("commissioningBudget (%s) no longer matches scheduler.ReadinessBudget() (%s).\n"+
			"budget.go restates that number because cloud must not import scheduler in production "+
			"code, and the minimum useful rental is derived from it. While they disagree, every "+
			"rental is floored against a commissioning cost the scheduler does not actually have: "+
			"too high wastes budget headroom on over-long rentals, too low goes back to renting "+
			"instances that expire before they can be given any work.\n"+
			"Update commissioningBudget in budget.go to match, or change ReadinessBudget() back.",
			commissioningBudget, readiness)
	}
}

/*
 * The capability rung must survive the efficiency rung being switched off.
 *
 * cloud_max_commissioning_pct = 0 is a supported configuration — it means "I
 * will accept an inefficient rental" — but it must not mean "I will accept a
 * rental that cannot be given work at all". resolveChunkDuration skips dispatch
 * outright when (remaining TTL - teardown slack) is under one minimum chunk, so
 * a rental below that floor is not merely wasteful, it is inert: it boots,
 * bills, and is never handed a single chunk.
 */
func TestMinRentalTTLKeepsCapabilityFloorWhenEfficiencyDisabled(t *testing.T) {
	capability := ceilMinute(commissioningBudget + defaultTeardownSlack + minUsefulChunk)

	if got := MinRentalTTL(0); got < capability {
		t.Errorf("MinRentalTTL(0) = %s, want >= %s (commissioning %s + teardown slack %s + min chunk %s).\n"+
			"Disabling the efficiency rung must not disable the floor entirely: below this the "+
			"dispatcher refuses to size a chunk, so the instance bills without ever being given work.",
			got, capability, commissioningBudget, defaultTeardownSlack, minUsefulChunk)
	}

	// Negative and absurd values are configuration mistakes, not policies.
	// Neither may produce a floor lower than the capability rung.
	for _, pct := range []int{-1, -100} {
		if got := MinRentalTTL(pct); got < capability {
			t.Errorf("MinRentalTTL(%d) = %s, want >= %s; a nonsensical setting must not weaken the floor",
				pct, got, capability)
		}
	}
}

// The shipped default must be strictly stronger than the bare capability floor,
// or configuring a percentage bought nothing and the efficiency rung is
// decorative.
func TestDefaultSettingsFloorExceedsCapabilityFloor(t *testing.T) {
	capability := ceilMinute(commissioningBudget + defaultTeardownSlack + minUsefulChunk)
	pct := DefaultSettings().MaxCommissioningPct

	if pct <= 0 || pct > 100 {
		t.Fatalf("DefaultSettings().MaxCommissioningPct = %d, want 1..100; "+
			"outside that range this guard proves nothing", pct)
	}

	got := MinRentalTTL(pct)
	if got <= capability {
		t.Errorf("MinRentalTTL(%d%%) = %s, which is no stronger than the capability floor %s.\n"+
			"At this percentage the efficiency rung never binds, so commissioning is allowed to be "+
			"almost the entire bill and the setting has no effect. Lower the default percentage.",
			pct, got, capability)
	}
}

/*
 * NewBudgetEngine's compiled-in default is what protects a deployment whose
 * settings table cannot be read, so it has to satisfy the same invariant as the
 * settings default rather than relying on LoadSettings having run. Same
 * reasoning as TestNewReaperDefaultsAreSafe.
 */
func TestNewBudgetEngineDefaultFloorIsSafe(t *testing.T) {
	e := NewBudgetEngine(nil)

	if e.MaxCommissioningPct != DefaultSettings().MaxCommissioningPct {
		t.Errorf("NewBudgetEngine().MaxCommissioningPct = %d, want %d (the shipped default).\n"+
			"An engine built without settings must behave like a fresh install, not like one "+
			"with the efficiency rung switched off.",
			e.MaxCommissioningPct, DefaultSettings().MaxCommissioningPct)
	}

	if got := MinRentalTTL(e.MaxCommissioningPct); got < commissioningBudget {
		t.Errorf("compiled-in floor %s is below the commissioning budget %s; "+
			"an instance rented for this long cannot finish starting up", got, commissioningBudget)
	}
}

/*
 * The floor is compared against max_instance_ttl_minutes, so it must land on a
 * whole number of minutes.
 *
 * Found by the existing rulesgate suite rather than by reasoning: unrounded, the
 * shipped default came out at 1h0m36.363636363s, so a client configured with the
 * single most obvious value an operator would type — 60 — was refused by 36
 * seconds, and the refusal explained itself in nine decimal places. Every
 * provisioning test in the package failed the same way.
 */
func TestMinRentalTTLLandsOnWholeMinutes(t *testing.T) {
	for pct := 0; pct <= 100; pct++ {
		got := MinRentalTTL(pct)
		if got%time.Minute != 0 {
			t.Errorf("MinRentalTTL(%d) = %s, which is not a whole number of minutes.\n"+
				"It is compared against max_instance_ttl_minutes, so a fractional floor rejects "+
				"round values an operator would reasonably enter and prints an unreadable minimum.",
				pct, got)
		}
	}
}

// A client set to exactly one hour — the value the admin guide recommends and
// the tester guide hands out — must be accepted at the shipped default.
func TestOneHourTTLIsAcceptedAtDefaultSettings(t *testing.T) {
	if floor := MinRentalTTL(DefaultSettings().MaxCommissioningPct); floor > time.Hour {
		t.Errorf("floor at the shipped default is %s, which refuses a 60-minute max_instance_ttl_minutes.\n"+
			"One hour is the value the admin guide recommends; if the default policy cannot accept "+
			"it, either the percentage or the guidance is wrong.", floor)
	}
}

// MinRentalTTL is pure and total, so the whole policy is a table.
func TestMinRentalTTL(t *testing.T) {
	capability := ceilMinute(commissioningBudget + defaultTeardownSlack + minUsefulChunk)

	cases := []struct {
		name string
		pct  int
		want time.Duration
	}{
		{"efficiency disabled falls back to capability", 0, capability},
		{"shipped default puts a round one-hour floor on rentals", 33, time.Hour},
		{"half the bill may be commissioning", 50, 2 * commissioningBudget},
		{"commissioning may be the whole rental", 100, capability},
		{"over 100 is clamped, not honoured", 250, capability},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MinRentalTTL(tc.pct); got != tc.want {
				t.Errorf("MinRentalTTL(%d) = %s, want %s", tc.pct, got, tc.want)
			}
		})
	}
}

/*
 * clampTTL is the sizing arithmetic, kept pure so the interesting cases do not
 * need a database, a job, or a provider.
 *
 * The property that matters is the last group: narrowing the TTL towards the
 * work a job actually has left must never produce a value that PlanLaunch then
 * refuses. Narrowing exists to free budget headroom; if it can turn a launch
 * that would have succeeded into a refusal, it is worse than not narrowing at
 * all — and the refusal quotes a lifetime the operator never configured.
 */
func TestClampTTL(t *testing.T) {
	const (
		floor = time.Hour
		upper = 4 * time.Hour
	)

	cases := []struct {
		name                string
		target, floor, want time.Duration
		upper               time.Duration
	}{
		{"a long job rides the ceiling", 10 * time.Hour, floor, upper, upper},
		{"a job in the middle is sized to its own need", 2 * time.Hour, floor, 2 * time.Hour, upper},
		{"a nearly-finished job is raised to the floor, not refused", 3 * time.Minute, floor, floor, upper},
		{"exactly the floor is kept", floor, floor, floor, upper},
		{"exactly the ceiling is kept", upper, floor, upper, upper},

		/*
		 * When the operator's own ceiling is under the floor, the ceiling wins
		 * and PlanLaunch refuses using the number they set. Returning the floor
		 * here would make the refusal quote a lifetime the client never allowed,
		 * pointing them at the wrong setting.
		 */
		{"a ceiling below the floor is passed through untouched", 30 * time.Minute, floor, 30 * time.Minute, 30 * time.Minute},
		{"a tiny target under a tiny ceiling still yields the ceiling", time.Minute, floor, 30 * time.Minute, 30 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampTTL(tc.target, tc.floor, tc.upper); got != tc.want {
				t.Errorf("clampTTL(target=%s, floor=%s, upper=%s) = %s, want %s",
					tc.target, tc.floor, tc.upper, got, tc.want)
			}
		})
	}
}

// Sizing must never make a launch WORSE than not sizing at all: whatever it
// returns has to be something PlanLaunch would also have accepted for the
// unnarrowed ceiling.
func TestClampTTLNeverCausesARefusalTheCeilingWouldNot(t *testing.T) {
	floor := MinRentalTTL(DefaultSettings().MaxCommissioningPct)

	for _, upper := range []time.Duration{floor, floor + time.Minute, 4 * time.Hour} {
		for _, target := range []time.Duration{
			0, time.Second, time.Minute, 30 * time.Minute, floor, 2 * floor, 100 * time.Hour,
		} {
			got := clampTTL(target, floor, upper)
			if got > upper {
				t.Errorf("clampTTL(%s, %s, %s) = %s, which exceeds the ceiling", target, floor, upper, got)
			}
			// The ceiling was acceptable, so the sized value must be too.
			if upper >= floor && got < floor {
				t.Errorf("clampTTL(%s, %s, %s) = %s: sized below the floor even though the "+
					"ceiling %s clears it, so narrowing turned a viable launch into a refusal",
					target, floor, upper, got, upper)
			}
		}
	}
}

/*
 * A saturated projection must ride the ceiling, not fall to the floor.
 *
 * The estimator deliberately saturates at time.Duration(math.MaxInt64) rather
 * than reporting nonsense (see maxProjection), so a job with negligible
 * throughput really does project to about 292 years. sizeTTL therefore must not
 * add commissioning to it first: that addition wraps to a large NEGATIVE
 * duration, and a negative target reads as "below the floor" and gets rounded
 * UP to the minimum rental — handing the job that needs the most runway the
 * least. This pins the arithmetic sizeTTL relies on to avoid that.
 */
func TestSaturatedProjectionDoesNotWrapIntoTheFloor(t *testing.T) {
	// A variable, not a const: constant overflow is a compile error, while the
	// runtime wrap is precisely the hazard being guarded against.
	saturated := time.Duration(math.MaxInt64)
	floor := MinRentalTTL(DefaultSettings().MaxCommissioningPct)
	upper := 4 * time.Hour

	// The guard sizeTTL applies: only add commissioning when the projection is
	// already under the ceiling.
	target := upper
	if saturated < upper {
		target = saturated + commissioningBudget
	}

	if got := clampTTL(target, floor, upper); got != upper {
		t.Errorf("a saturated projection sized to %s, want the ceiling %s.\n"+
			"Adding commissioning before comparing overflows to %s, which clampTTL rounds up to "+
			"the floor — the longest job gets the shortest rental.",
			got, upper, saturated+commissioningBudget)
	}

	// And the unguarded form really is the trap being defended against.
	if saturated+commissioningBudget > 0 {
		t.Fatalf("saturated+commissioningBudget = %s, expected it to wrap negative; "+
			"if this no longer overflows, the guard in sizeTTL may be unnecessary",
			saturated+commissioningBudget)
	}
}

/*
 * A sized TTL must outlive its own last chunk's crack upload.
 *
 * Two waits meet at the end of a rental and they were not the same length.
 * resolveChunkDuration refuses to plan a chunk past (remaining TTL - teardown
 * slack), so the last chunk ends ~120s before the deadline; but the reaper will
 * hold an instance for CrackDrainGrace (10 minutes) while its agent is still
 * uploading, because a large flush genuinely takes minutes.
 *
 * The reaper's patience is worthless if the machine is gone: ttl_epoch is armed
 * IN-GUEST and the watchdog powers off regardless. So a TTL sized to end exactly
 * when the work ends gave the upload 120 seconds and killed anything slower
 * mid-flush — losing cracks on the ORDINARY SUCCESSFUL PATH.
 *
 * This pins the tail to the grace the reaper already honours.
 */
func TestDrainTailCoversTheReapersCrackDrainGrace(t *testing.T) {
	grace := DefaultSettings().CrackDrainGrace
	if grace <= 0 {
		t.Fatalf("DefaultSettings().CrackDrainGrace = %s; a non-positive grace makes this guard vacuous", grace)
	}

	s := &Service{CrackDrainGrace: grace}
	tail := s.drainTail()

	if tail < grace {
		t.Errorf("drainTail() = %s, which is shorter than CrackDrainGrace (%s).\n"+
			"The backend would wait that long for an upload the guest's watchdog has already "+
			"powered off, so cracks are lost on the ordinary successful path.", tail, grace)
	}
	if tail <= defaultTeardownSlack {
		t.Errorf("drainTail() = %s, no better than the teardown slack alone (%s); "+
			"the chunk planner already reserves that, so this adds nothing", tail, defaultTeardownSlack)
	}

	// Unwired or deliberately disabled must still owe the slack the chunk
	// planner has already subtracted — degrading to zero would plan chunks into
	// a window that does not exist.
	if got := (&Service{}).drainTail(); got < defaultTeardownSlack {
		t.Errorf("drainTail() with no grace = %s, want >= the teardown slack %s", got, defaultTeardownSlack)
	}
}
