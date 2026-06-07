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

// unit is the "one unit per parent" helper — each call gets a fresh
// random ParentJobID, so the new parent-cap tracker in the allocator
// treats every unit independently (matches the non-increment-job
// production case). Tests that need sibling units sharing a parent
// (i.e., increment-job semantics) should use siblingUnit() instead.
func unit(id uuid.UUID, priority, maxAgents, activeCount int, createdAtNanos int64) UnitInfo {
	return UnitInfo{
		ID:               id,
		ParentJobID:      uuid.New(),
		Priority:         priority,
		MaxAgents:        maxAgents,
		ActiveAgentCount: activeCount,
		CreatedAtNanos:   createdAtNanos,
	}
}

// siblingUnit produces a UnitInfo that shares a ParentJobID with other
// units of the same increment job. The parent's MaxAgents and
// ActiveAgentCount are passed once and stamped onto every sibling — this
// matches what buildUnitInfos does in production.
func siblingUnit(id, parentJobID uuid.UUID, priority, parentMaxAgents, parentActiveCount int, createdAtNanos int64) UnitInfo {
	return UnitInfo{
		ID:               id,
		ParentJobID:      parentJobID,
		Priority:         priority,
		MaxAgents:        parentMaxAgents,
		ActiveAgentCount: parentActiveCount,
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

// An agent compatible ONLY with a lower-priority unit must be assigned to
// that unit rather than left idle while higher-priority (incompatible) units
// hold the top tier. Regression for the binary-6.x-vs-all-7.x starvation:
// the agent literally cannot run the top tier, so freezing it wastes capacity.
// The core invariant: no compatible agent idles while a compatible job has work.
func TestAllocator_CompatibilityDescent(t *testing.T) {
	p1000a, p1000b, p1000c := uuid.New(), uuid.New(), uuid.New()
	p900, p850, p700 := uuid.New(), uuid.New(), uuid.New()
	p300 := uuid.New()
	units := []UnitInfo{
		unit(p1000a, 1000, 0, 0, 1),
		unit(p1000b, 1000, 0, 0, 2),
		unit(p1000c, 1000, 0, 0, 3),
		unit(p900, 900, 0, 0, 4),
		unit(p850, 850, 0, 0, 5),
		unit(p700, 700, 0, 0, 6),
		unit(p300, 300, 0, 0, 7),
	}
	// The single agent (a "binary 6.x" agent) is compatible only with p300.
	agents := []AgentInfo{agentN(1)}
	compat := func(uid uuid.UUID, _ int) bool { return uid == p300 }

	for _, mode := range []OverflowMode{
		OverflowFIFO, OverflowRoundRobin,
		OverflowEnforceMaxAgents, OverflowMaxAgentsFIFO, OverflowMaxAgentsRoundRobin,
	} {
		t.Run(string(mode), func(t *testing.T) {
			out := AllocateAgentsByPriority(units, agents, mode, compat)
			set := allocationSet(out)
			if len(set[p300]) != 1 || (len(set[p300]) == 1 && set[p300][0] != 1) {
				t.Fatalf("%s: expected agent 1 on the only compatible (p300) unit, got %v", mode, out)
			}
		})
	}
}

// Conversely, a capable agent is consumed by the highest tier it can run and
// does NOT leak down to lower-priority work while that tier still wants agents.
// This guards that the descent fix did not weaken strict priority for contention.
func TestAllocator_CapableAgentStaysOnTopTier(t *testing.T) {
	high := uuid.New()
	low := uuid.New()
	units := []UnitInfo{
		unit(high, 1000, 0, 0, 1), // unlimited capacity, always wants agents
		unit(low, 300, 0, 0, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2)}

	for _, mode := range []OverflowMode{OverflowFIFO, OverflowRoundRobin} {
		t.Run(string(mode), func(t *testing.T) {
			out := AllocateAgentsByPriority(units, agents, mode, alwaysCompatible)
			set := allocationSet(out)
			if len(set[high]) != 2 {
				t.Fatalf("%s: expected both agents monopolized by the high tier, got high=%d low=%d",
					mode, len(set[high]), len(set[low]))
			}
			if len(set[low]) != 0 {
				t.Fatalf("%s: low tier must get nothing while the high tier wants agents, got %d", mode, len(set[low]))
			}
		})
	}
}

// 20 compatible agents, 3 same-priority jobs with max_agents 3/2/2 under
// max_agents_round_robin: baseline fills 3/2/2, then surplus overflows so NO
// compatible agent idles while those jobs have dispatchable work (the user's
// "they hit the 3,2,2 then filter the rest in round-robin" scenario).
func TestAllocator_MaxAgentsRoundRobin_NoIdleWhenWorkExists(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	units := []UnitInfo{
		unit(a, 1000, 3, 0, 1),
		unit(b, 1000, 2, 0, 2),
		unit(c, 1000, 2, 0, 3),
	}
	agents := make([]AgentInfo, 20)
	for i := range agents {
		agents[i] = agentN(i + 1)
	}
	out := AllocateAgentsByPriority(units, agents, OverflowMaxAgentsRoundRobin, alwaysCompatible)
	if len(out) != 20 {
		t.Fatalf("expected all 20 agents allocated (none idle while work exists), got %d", len(out))
	}
	set := allocationSet(out)
	if len(set[a]) < 3 || len(set[b]) < 2 || len(set[c]) < 2 {
		t.Fatalf("expected baseline caps filled (a>=3,b>=2,c>=2), got a=%d b=%d c=%d",
			len(set[a]), len(set[b]), len(set[c]))
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

// Increment job: 4 sibling units (one per layer) share a parent with
// max_agents=1. The fill phase must cap TOTAL allocations across the 4
// units at 1 — not 1 per layer (which would let an increment job
// consume max_agents × N_layers agents). This is the regression test
// for the bug observed in the FIFO/enforce_max_agents testing pass.
func TestAllocator_IncrementJob_ParentCapEnforced(t *testing.T) {
	parent := uuid.New()
	layer1 := uuid.New()
	layer2 := uuid.New()
	layer3 := uuid.New()
	layer4 := uuid.New()
	units := []UnitInfo{
		siblingUnit(layer1, parent, 5, 1, 0, 1),
		siblingUnit(layer2, parent, 5, 1, 0, 2),
		siblingUnit(layer3, parent, 5, 1, 0, 3),
		siblingUnit(layer4, parent, 5, 1, 0, 4),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if len(out) != 1 {
		t.Fatalf("expected exactly 1 allocation under parent_max=1 across 4 sibling layers, got %d: %+v", len(out), out)
	}
}

// Same scenario but the parent already has an in-flight task on one of
// its layers — the cycle must not allocate any new agents (cap of 1 is
// already used by the existing task).
func TestAllocator_IncrementJob_ParentCapRespectsActiveAgentCount(t *testing.T) {
	parent := uuid.New()
	layer1 := uuid.New()
	layer2 := uuid.New()
	// ActiveAgentCount=1 represents one in-flight task across the
	// parent's units (typically on layer 1, but the allocator only
	// cares about the per-parent total).
	units := []UnitInfo{
		siblingUnit(layer1, parent, 5, 1, 1, 1),
		siblingUnit(layer2, parent, 5, 1, 1, 2),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if len(out) != 0 {
		t.Fatalf("expected 0 allocations (parent already at cap), got %d: %+v", len(out), out)
	}
}

// Increment job with parent_max=4 and 4 layers should distribute the
// 4 agents across all 4 layers (one each), not pile them on layer 1.
// The allocator iterates units in FIFO order; once a layer has taken
// its share, the next sibling gets the next agent.
func TestAllocator_IncrementJob_ParentCapDistributesAcrossLayers(t *testing.T) {
	parent := uuid.New()
	layer1 := uuid.New()
	layer2 := uuid.New()
	layer3 := uuid.New()
	layer4 := uuid.New()
	units := []UnitInfo{
		siblingUnit(layer1, parent, 5, 4, 0, 1),
		siblingUnit(layer2, parent, 5, 4, 0, 2),
		siblingUnit(layer3, parent, 5, 4, 0, 3),
		siblingUnit(layer4, parent, 5, 4, 0, 4),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4)}

	out := AllocateAgentsByPriority(units, agents, OverflowEnforceMaxAgents, alwaysCompatible)
	if len(out) != 4 {
		t.Fatalf("expected 4 allocations (parent_max=4, 4 layers, 4 agents), got %d: %+v", len(out), out)
	}
	// Each layer must have received at least one agent — the fill loop
	// fills layer 1 first (oldest), but it caps at 1 due to remaining
	// parent capacity decreasing as agents are placed.
	bySite := allocationSet(out)
	for _, lid := range []uuid.UUID{layer1, layer2, layer3, layer4} {
		if len(bySite[lid]) == 0 {
			t.Errorf("layer %s got 0 agents; expected 1", lid)
		}
		if len(bySite[lid]) > 1 {
			t.Errorf("layer %s got %d agents; expected 1 (parent cap should prevent piling on layer 1)", lid, len(bySite[lid]))
		}
	}
}

// ---------------------------------------------------------------------------
// Max-Agents overflow modes (priority-agnostic overflow)
// ---------------------------------------------------------------------------

// Max-Agents-FIFO: every tier fills to max_agents first (strict caps),
// then any surplus piles on the OLDEST UNIT OVERALL by created_at,
// regardless of priority. Distinct from Priority-FIFO (which would dump
// extras on the oldest within the top tier and starve lower tiers).
func TestAllocator_MaxAgentsFIFO_RespectsCapsThenOverflows(t *testing.T) {
	// Tier 5: 2 units, both max=2 → 4 cap. Created 100 and 200.
	// Tier 4: 1 unit,  max=1       → 1 cap. Created 50 (OLDEST overall).
	// Total cap = 5. With 8 agents, surplus = 3.
	tier5a := uuid.New()
	tier5b := uuid.New()
	tier4 := uuid.New()
	units := []UnitInfo{
		unit(tier5a, 5, 2, 0, 100),
		unit(tier5b, 5, 2, 0, 200),
		unit(tier4, 4, 1, 0, 50),
	}
	agents := []AgentInfo{
		agentN(1), agentN(2), agentN(3), agentN(4),
		agentN(5), agentN(6), agentN(7), agentN(8),
	}

	out := AllocateAgentsByPriority(units, agents, OverflowMaxAgentsFIFO, alwaysCompatible)
	if len(out) != 8 {
		t.Fatalf("expected all 8 agents allocated (cap 5 + 3 overflow), got %d: %+v", len(out), out)
	}
	bySite := allocationSet(out)
	if got := len(bySite[tier5a]); got != 2 {
		t.Errorf("tier5a should be exactly at cap (2), got %d", got)
	}
	if got := len(bySite[tier5b]); got != 2 {
		t.Errorf("tier5b should be exactly at cap (2), got %d", got)
	}
	// tier4 is OLDEST (created 50) — gets its baseline 1 + all 3 extras = 4
	if got := len(bySite[tier4]); got != 4 {
		t.Errorf("tier4 (oldest overall) should absorb baseline + 3 overflow = 4, got %d", got)
	}
}

// Max-Agents-FIFO crosses priority tiers: lower-priority oldest unit
// gets the overflow even though higher-priority units exist. The exact
// scenario that distinguishes this mode from Priority-FIFO.
func TestAllocator_MaxAgentsFIFO_CrossesTiers(t *testing.T) {
	hi := uuid.New() // priority 100, newer
	lo := uuid.New() // priority 1, older — should absorb extras
	units := []UnitInfo{
		unit(hi, 100, 1, 0, 200),
		unit(lo, 1, 1, 0, 100),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3), agentN(4), agentN(5)}

	out := AllocateAgentsByPriority(units, agents, OverflowMaxAgentsFIFO, alwaysCompatible)
	if len(out) != 5 {
		t.Fatalf("expected all 5 agents allocated, got %d", len(out))
	}
	bySite := allocationSet(out)
	if got := len(bySite[hi]); got != 1 {
		t.Errorf("hi-priority unit must stay at cap (1), got %d", got)
	}
	if got := len(bySite[lo]); got != 4 {
		t.Errorf("lo-priority unit (oldest overall) should absorb 1 baseline + 3 overflow = 4, got %d", got)
	}
}

// Max-Agents-Round-Robin: rotates the overflow across ALL units by
// created_at, one agent per unit per pass. Distinct from Priority-RR
// (which only rotates within the top tier).
func TestAllocator_MaxAgentsRoundRobin_RotatesAcrossTiers(t *testing.T) {
	// 3 units across 2 tiers, each with max=1. Cap = 3.
	// 9 agents → 6 in overflow → 2 extra per unit if rotation works.
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	units := []UnitInfo{
		unit(a, 5, 1, 0, 100), // oldest
		unit(b, 3, 1, 0, 200),
		unit(c, 5, 1, 0, 300),
	}
	agents := []AgentInfo{
		agentN(1), agentN(2), agentN(3),
		agentN(4), agentN(5), agentN(6),
		agentN(7), agentN(8), agentN(9),
	}

	out := AllocateAgentsByPriority(units, agents, OverflowMaxAgentsRoundRobin, alwaysCompatible)
	if len(out) != 9 {
		t.Fatalf("expected all 9 agents allocated, got %d", len(out))
	}
	bySite := allocationSet(out)
	// Baseline 1 each + ~2 each from rotation. Tolerate ±1 because
	// rotation order depends on which agent is at the head of the free
	// pool when each unit's turn comes around.
	for id, label := range map[uuid.UUID]string{a: "a", b: "b", c: "c"} {
		got := len(bySite[id])
		if got < 2 || got > 4 {
			t.Errorf("unit %s got %d agents; expected 2-4 (1 baseline + ~2 rotated)", label, got)
		}
	}
}

// Max-Agents-FIFO with compatibility constraints: if the oldest unit
// can't accept a given agent (binary version etc.), that agent falls
// through to the next compatible oldest unit.
func TestAllocator_MaxAgentsFIFO_RespectsCompatibility(t *testing.T) {
	old := uuid.New() // older, but ONLY accepts agent 1
	new := uuid.New() // newer, accepts everyone
	units := []UnitInfo{
		unit(old, 5, 1, 0, 100),
		unit(new, 5, 1, 0, 200),
	}
	agents := []AgentInfo{agentN(1), agentN(2), agentN(3)}
	compat := func(unitID uuid.UUID, agentID int) bool {
		if unitID == old && agentID != 1 {
			return false
		}
		return true
	}

	out := AllocateAgentsByPriority(units, agents, OverflowMaxAgentsFIFO, compat)
	if len(out) != 3 {
		t.Fatalf("expected all 3 agents allocated, got %d", len(out))
	}
	bySite := allocationSet(out)
	// Old gets agent 1 (baseline). Overflow (2 agents) all go to `new`
	// because `old` rejects them — `old` ends with 1, `new` with 2.
	if got := len(bySite[old]); got != 1 {
		t.Errorf("old (only agent 1 compatible) should have 1 agent, got %d", got)
	}
	if got := len(bySite[new]); got != 2 {
		t.Errorf("new (catch-all for incompatible overflow) should have 2 agents, got %d", got)
	}
}
