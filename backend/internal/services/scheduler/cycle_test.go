package scheduler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestRunOnce_SingleFlight verifies that RunOnce short-circuits when
// another cycle is already in flight. The runner's ticker pattern
// guarantees no overlap today, but the guard exists so a future
// refactor adding a manual cycle trigger can't silently produce
// pile-ups.
//
// Strategy: pre-set the running flag to true (simulating a cycle
// already in flight) and call RunOnce. The guard should fire before
// any dependency is touched and return the zero CycleResult with no
// error. The running flag must still be true on return (the deferred
// Store(false) is registered AFTER the guard check, so the early-exit
// path can't accidentally clear an in-flight cycle's flag).
func TestRunOnce_SingleFlight(t *testing.T) {
	c := &Cycle{}

	// Simulate a cycle already in flight.
	if !c.running.CompareAndSwap(false, true) {
		t.Fatal("test setup: running flag should be false on a fresh Cycle")
	}

	res, err := c.RunOnce(context.Background())
	if err != nil {
		t.Errorf("guard short-circuit should return nil error, got: %v", err)
	}
	if res != (CycleResult{}) {
		t.Errorf("guard short-circuit should return zero CycleResult, got: %+v", res)
	}
	if !c.running.Load() {
		t.Error("guard short-circuit must NOT clear the running flag (would corrupt the in-flight cycle's state)")
	}
}

// TestRunOnce_GuardReleasesOnReturn verifies the deferred Store(false)
// fires on the success path. Uses a fresh Cycle with no dependencies;
// the cycle body bails early on the SelectSchedulableUnits call
// (unitRepo is nil) but the guard ordering is what we're testing, not
// the cycle body's correctness.
func TestRunOnce_GuardReleasesOnReturn(t *testing.T) {
	c := &Cycle{}

	// Recover from the inevitable nil-deref inside the cycle body —
	// we're only testing that the deferred Store(false) registered.
	func() {
		defer func() { _ = recover() }()
		_, _ = c.RunOnce(context.Background())
	}()

	if c.running.Load() {
		t.Error("running flag should be false after RunOnce returns (deferred Store missed)")
	}
}

// TestComputeStarvingUnits_RespectsParentCap is the regression test
// for the dispatch-then-preempt thrash bug. With an increment job
// where parent_max=1 and 4 layers, layer 1 takes the agent; layers
// 2, 3, 4 get 0 allocations this cycle but are NOT starving — they
// are parent-capped. Treating them as starving causes preemption to
// stop lower-priority jobs' tasks for nothing (the freed agents
// can't be placed on any of those siblings because the parent cap
// is already used).
func TestComputeStarvingUnits_RespectsParentCap(t *testing.T) {
	parent := uuid.New()
	layer1 := uuid.New()
	layer2 := uuid.New()
	layer3 := uuid.New()
	layer4 := uuid.New()
	otherJob := uuid.New() // a different parent at lower priority

	units := []UnitInfo{
		// 4 sibling layers, parent_max=1, ActiveAgentCount=0 fresh
		{ID: layer1, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 0},
		{ID: layer2, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 0},
		{ID: layer3, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 0},
		{ID: layer4, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 0},
		// An unrelated unit at priority 5 with no allocation — this
		// IS truly starving and should remain in the result.
		{ID: uuid.New(), ParentJobID: otherJob, Priority: 5, MaxAgents: 3, ActiveAgentCount: 0},
	}
	// Layer 1 took the parent's only slot this cycle.
	allocs := []Allocation{
		{UnitID: layer1, AgentID: 1},
	}

	starving := computeStarvingUnits(units, allocs)
	for _, u := range starving {
		if u.ParentJobID == parent {
			t.Errorf("layer %s incorrectly flagged as starving; parent already at cap (1)", u.ID)
		}
	}
	if len(starving) != 1 {
		t.Errorf("expected exactly 1 truly-starving unit (the otherJob unit), got %d", len(starving))
	}
}

// TestComputeStarvingUnits_RespectsActiveAgentCount mirrors the above
// but the parent cap is satisfied by an EXISTING in-flight task (from
// a prior cycle, reflected in ActiveAgentCount) — not by this cycle's
// allocation. Layers 2-4 should still be excluded.
func TestComputeStarvingUnits_RespectsActiveAgentCount(t *testing.T) {
	parent := uuid.New()
	layer1 := uuid.New()
	layer2 := uuid.New()
	units := []UnitInfo{
		// parent_max=1, 1 task already running on parent (from prior cycle)
		{ID: layer1, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 1},
		{ID: layer2, ParentJobID: parent, Priority: 5, MaxAgents: 1, ActiveAgentCount: 1},
	}
	// No new allocations this cycle (everything blocked).
	allocs := []Allocation{}

	starving := computeStarvingUnits(units, allocs)
	if len(starving) != 0 {
		t.Errorf("expected 0 starving units (parent already at cap via in-flight), got %d", len(starving))
	}
}

// TestComputeStarvingUnits_UnboundedMaxAgentsStillStarves verifies the
// parent-cap check doesn't accidentally exclude units whose parent
// MaxAgents is 0 (unlimited). Such a unit with 0 allocations IS truly
// starving.
func TestComputeStarvingUnits_UnboundedMaxAgentsStillStarves(t *testing.T) {
	parent := uuid.New()
	u := uuid.New()
	units := []UnitInfo{
		{ID: u, ParentJobID: parent, Priority: 5, MaxAgents: 0, ActiveAgentCount: 0},
	}
	starving := computeStarvingUnits(units, []Allocation{})
	if len(starving) != 1 {
		t.Errorf("unbounded-max unit with 0 allocations should be starving, got %d", len(starving))
	}
}
