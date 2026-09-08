package cloud

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/scheduler"
)

/*
 * This is a drift guard, not a unit test.
 *
 * A rented instance cannot be given a task until it has finished a file sync
 * and then a benchmark. The scheduler decides how long that may legitimately
 * take; the cloud reaper decides when an instance that has not been given a
 * task gets destroyed. Those two numbers live in different packages, and when
 * the reaper's was the smaller one the result was not merely wasted spend but a
 * LOOP: the instance was destroyed mid-sync, the job was still starving, the
 * autoscaler rented a replacement, and that one died the same way. Neither
 * dead-on-arrival breaker noticed, because both key on ready_at IS NULL and an
 * instance killed during sync has ready_at set.
 *
 * So the invariant is: commissioning teardown must be strictly more patient
 * than the scheduler's readiness budget. Asserting it here means moving either
 * constant fails CI instead of quietly reopening the loop.
 *
 * The import direction matters. cloud must not depend on scheduler in
 * production code (the scheduler deliberately keeps its starvation publisher
 * structural to avoid the reverse edge), but a _test.go file adds no edge to
 * the production build.
 */
func TestCommissioningGraceExceedsSchedulerReadinessBudget(t *testing.T) {
	budget := scheduler.ReadinessBudget()
	grace := DefaultSettings().CommissioningGrace

	if budget <= 0 {
		t.Fatalf("scheduler.ReadinessBudget() = %s; a non-positive budget makes this guard vacuous", budget)
	}

	if grace <= budget {
		t.Errorf("CommissioningGrace (%s) must exceed scheduler.ReadinessBudget() (%s).\n"+
			"As written, a healthy agent can still be syncing or benchmarking when the reaper "+
			"destroys it — and because its job is still starving, the autoscaler immediately "+
			"rents a replacement that dies the same way. Raise CommissioningGrace or shorten "+
			"the scheduler's sync/benchmark windows.", grace, budget)
	}
}

// NewReaper's compiled-in default is what protects a deployment whose settings
// table cannot be read, so it has to satisfy the same invariant as the settings
// default rather than relying on LoadSettings having run.
func TestNewReaperDefaultsAreSafe(t *testing.T) {
	r := NewReaper(nil, nil, nil, nil)

	if r.CommissioningGrace <= scheduler.ReadinessBudget() {
		t.Errorf("NewReaper CommissioningGrace = %s, want > %s (scheduler readiness budget)",
			r.CommissioningGrace, scheduler.ReadinessBudget())
	}
	if r.IdleDrain <= 0 {
		t.Errorf("NewReaper IdleDrain = %s, want positive", r.IdleDrain)
	}
	// The two clocks are deliberately different magnitudes. If someone collapses
	// them back to the same value, the split has been undone in spirit.
	if r.CommissioningGrace == r.IdleDrain {
		t.Error("CommissioningGrace equals IdleDrain; the split exists because a cold start " +
			"and a between-chunks gap need different patience")
	}
}
