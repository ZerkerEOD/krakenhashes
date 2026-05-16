package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// alwaysCompatible returns true for every (unit, agent) pair. Most
// allocator tests don't care about compatibility and use this stub.
func alwaysCompatible(_ uuid.UUID, _ int) bool { return true }

// allocationSet turns a slice of Allocations into a set keyed by unit ID
// with the agent IDs as values. Used to compare without caring about
// emission order.
func allocationSet(allocs []Allocation) map[uuid.UUID][]int {
	out := map[uuid.UUID][]int{}
	for _, a := range allocs {
		out[a.UnitID] = append(out[a.UnitID], a.AgentID)
	}
	return out
}

func unit(id uuid.UUID, priority, maxAgents, activeCount int, createdAtNanos int64) UnitInfo {
	return UnitInfo{
		ID:               id,
		Priority:         priority,
		MaxAgents:        maxAgents,
		ActiveAgentCount: activeCount,
		CreatedAtNanos:   createdAtNanos,
	}
}

func agentN(id int) AgentInfo {
	return AgentInfo{ID: id, BinaryVersion: "1.0.0", BenchmarkSpeed: 1_000_000_000}
}

// ---------------------------------------------------------------------------
// Trivial cases
// ---------------------------------------------------------------------------

func TestAllocator_NoUnits(t *testing.T) {
	out := AllocateAgentsByPriority(nil, []AgentInfo{agentN(1)}, OverflowFIFO, alwaysCompatible)
	if len(out) != 0 {
		t.Fatalf("no units should produce no allocations, got %d", len(out))
	}
}

func TestAllocator_NoAgents(t *testing.T) {
	u := uuid.New()
	out := AllocateAgentsByPriority([]UnitInfo{unit(u, 0, 0, 0, 0)}, nil, OverflowFIFO, alwaysCompatible)
	if len(out) != 0 {
		t.Fatalf("no agents should produce no allocations, got %d", len(out))
	}
}

// ---------------------------------------------------------------------------
// Single-tier scenarios
// ---------------------------------------------------------------------------

// One unit, max_agents=0 (unlimited), 3 agents: all go to that unit.
func TestAllocator_SingleUnitUnlimited(t *testing.T) {
	u := uuid.New()
	units := []UnitInfo{unit(u, 100, 0, 0, 1)}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}

	out := AllocateAgentsByPriority(units, agents, OverflowFIFO, alwaysCompatible)
	set := allocationSet(out)
	if len(set[u]) != 3 {
		t.Fatalf("expected 3 allocations to unit, got %d", len(set[u]))
	}
}

// One unit, max_agents=2, 5 agents, enforce_max_agents: 2 allocated, 3 idle.
func TestAllocator_EnforceMaxAgents_SingleUnit(t *testing.T) {
	u := uuid.New()
	units := []UnitInfo{unit(u, 100, 2, 0, 1)}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if got := len(out); got != 2 {
		t.Fatalf("expected exactly 2 allocations under enforce_max_agents, got %d", got)
	}
}

// Same scenario under fifo: 5 allocated to the single unit (overflow goes
// to the oldest unit, which is also the only unit).
func TestAllocator_FIFO_AllOverflowToOldest(t *testing.T) {
	u := uuid.New()
	units := []UnitInfo{unit(u, 100, 2, 0, 1)}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5)}

	out := AllocateAgentsByPriority(units, agents, OverflowFIFO, alwaysCompatible)
	if got := len(out); got != 5 {
		t.Fatalf("expected 5 allocations under fifo, got %d", got)
	}
}

// Two units at same priority, both max_agents=2, 6 agents, fifo:
// each unit gets its 2, then 2 overflow agents go to the OLDER unit.
// Final: older unit gets 4, newer gets 2.
func TestAllocator_FIFO_OverflowToOldestAtTier(t *testing.T) {
	older := uuid.New()
	newer := uuid.New()
	units := []UnitInfo{
		unit(older, 100, 2, 0, 1),
		unit(newer, 100, 2, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5), agentN(6)}

	out := AllocateAgentsByPriority(units, agents, OverflowFIFO, alwaysCompatible)
	set := allocationSet(out)
	if got := len(set[older]); got != 4 {
		t.Fatalf("older unit: expected 4 (2 normal + 2 overflow), got %d", got)
	}
	if got := len(set[newer]); got != 2 {
		t.Fatalf("newer unit: expected 2, got %d", got)
	}
}

// Same scenario under round_robin: each unit gets 2, then 2 overflow
// distributed one-at-a-time → older gets 3, newer gets 3.
func TestAllocator_RoundRobin_EvenOverflowAtTier(t *testing.T) {
	older := uuid.New()
	newer := uuid.New()
	units := []UnitInfo{
		unit(older, 100, 2, 0, 1),
		unit(newer, 100, 2, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5), agentN(6)}

	out := AllocateAgentsByPriority(units, agents, OverflowRoundRobin, alwaysCompatible)
	set := allocationSet(out)
	if got := len(set[older]); got != 3 {
		t.Fatalf("older unit: expected 3 under round_robin, got %d", got)
	}
	if got := len(set[newer]); got != 3 {
		t.Fatalf("newer unit: expected 3 under round_robin, got %d", got)
	}
}

// Same scenario under enforce_max_agents: each unit gets exactly 2; the
// other 2 agents are unallocated (no overflow within tier, no lower tier
// to descend to).
func TestAllocator_EnforceMaxAgents_HardCap(t *testing.T) {
	older := uuid.New()
	newer := uuid.New()
	units := []UnitInfo{
		unit(older, 100, 2, 0, 1),
		unit(newer, 100, 2, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5), agentN(6)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if got := len(out); got != 4 {
		t.Fatalf("expected 4 allocations (2 per unit, no overflow), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Multi-tier descent
// ---------------------------------------------------------------------------

// High tier max=2 fills, surplus descends to low tier (which has max=0
// unlimited).
func TestAllocator_DescendToLowerTier(t *testing.T) {
	high := uuid.New()
	low := uuid.New()
	units := []UnitInfo{
		unit(high, 200, 2, 0, 1),
		unit(low, 100, 0, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5)}

	for _, mode := range []OverflowMode{OverflowFIFO, OverflowRoundRobin, OverflowEnforceMaxAgents} {
		t.Run(string(mode), func(t *testing.T) {
			out := AllocateAgentsByPriority(units, agents, mode, alwaysCompatible)
			set := allocationSet(out)

			// Under fifo/round_robin: high tier's max_agents=2 is filled (2 agents),
			// then tier-local overflow distributes the remaining 3 within the high
			// tier (since high tier has only one unit and max_agents=2 capacity isn't
			// hit because... wait this is wrong). Let me re-check.
			//
			// Under fifo, with one unit at the tier, after fill phase agent count
			// for high unit is 2. Then overflow happens: fifo gives all remaining
			// 3 to the oldest unit at the tier (= high unit). So high gets 5, low
			// gets 0. That's the documented behavior.
			//
			// Under round_robin, the surplus rotates across units AT THE SAME TIER.
			// Tier has only one unit, so it goes to that unit. High gets 5, low gets 0.
			//
			// Only under enforce_max_agents does the descent kick in for this layout.
			switch mode {
			case OverflowFIFO, OverflowRoundRobin:
				if len(set[high]) != 5 {
					t.Fatalf("%s: expected high=5 (tier-local overflow), got %d", mode, len(set[high]))
				}
				if len(set[low]) != 0 {
					t.Fatalf("%s: expected low=0, got %d", mode, len(set[low]))
				}
			case OverflowEnforceMaxAgents:
				if len(set[high]) != 2 {
					t.Fatalf("enforce: expected high=2 (hard cap), got %d", len(set[high]))
				}
				if len(set[low]) != 3 {
					t.Fatalf("enforce: expected low=3 (descent), got %d", len(set[low]))
				}
			}
		})
	}
}

// Active agents already on a unit count against its max_agents.
func TestAllocator_ActiveAgentCountSubtractsFromMax(t *testing.T) {
	u := uuid.New()
	// MaxAgents=3, already running 2 → only 1 more slot.
	units := []UnitInfo{unit(u, 100, 3, 2, 1)}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if got := len(out); got != 1 {
		t.Fatalf("expected 1 allocation (max=3, active=2), got %d", got)
	}
}

// A unit already AT max_agents gets nothing.
func TestAllocator_UnitAtMaxGetsNothing(t *testing.T) {
	u := uuid.New()
	units := []UnitInfo{unit(u, 100, 2, 2, 1)}
	agents := []AgentInfo{agentN(1), agentN(2)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if got := len(out); got != 0 {
		t.Fatalf("expected 0 allocations (unit at max), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Compatibility filtering
// ---------------------------------------------------------------------------

// Two units, two agents. Each agent is compatible with exactly one unit.
// Each unit ends up with exactly its compatible agent.
func TestAllocator_CompatibilityFilter(t *testing.T) {
	unitA := uuid.New()
	unitB := uuid.New()
	units := []UnitInfo{
		unit(unitA, 100, 0, 0, 1),
		unit(unitB, 100, 0, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2)}

	compat := func(uid uuid.UUID, aid int) bool {
		return (uid == unitA && aid == 1) || (uid == unitB && aid == 2)
	}

	out := AllocateAgentsByPriority(units, agents, OverflowFIFO, compat)
	set := allocationSet(out)
	if len(set[unitA]) != 1 || set[unitA][0] != 1 {
		t.Fatalf("unit A should have agent 1, got %v", set[unitA])
	}
	if len(set[unitB]) != 1 || set[unitB][0] != 2 {
		t.Fatalf("unit B should have agent 2, got %v", set[unitB])
	}
}

// If no compatible agents exist for a unit, it gets nothing — even under
// round_robin, the round-robin loop terminates after marking the unit
// exhausted.
func TestAllocator_RoundRobin_TerminatesWhenNoneCompatible(t *testing.T) {
	u := uuid.New()
	units := []UnitInfo{unit(u, 100, 2, 0, 1)}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}

	compat := func(_ uuid.UUID, _ int) bool { return false }

	// Should return without hanging. Allocations expected to be empty.
	done := make(chan []Allocation)
	go func() {
		done <- AllocateAgentsByPriority(units, agents, OverflowRoundRobin, compat)
	}()

	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("expected 0 allocations (no compatible agents), got %d", len(got))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AllocateAgentsByPriority appears to be hung on incompatible round_robin")
	}
}

// One agent appears at most once in the output across the entire
// allocation.
func TestAllocator_NoDoubleAssignment(t *testing.T) {
	unitA := uuid.New()
	unitB := uuid.New()
	units := []UnitInfo{
		unit(unitA, 100, 0, 0, 1),
		unit(unitB, 100, 0, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}

	out := AllocateAgentsByPriority(units, agents, OverflowFIFO, alwaysCompatible)
	seen := map[int]bool{}
	for _, a := range out {
		if seen[a.AgentID] {
			t.Fatalf("agent %d allocated twice", a.AgentID)
		}
		seen[a.AgentID] = true
	}
}

