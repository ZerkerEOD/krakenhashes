package scheduler

import (
	"sort"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// AllocateAgentsByPriority assigns idle agents to schedulable units per
// the priority + max_agents + overflow rules.
//
// Two structurally distinct flows are dispatched on by `mode`:
//
// **Priority modes** (fifo, round_robin) — tier-by-tier, original
// semantic. Walk priority tiers from highest to lowest; at each tier:
//
//  1. Fill each unit to its max_agents (or take everything if
//     max_agents = 0), in unit creation order (FIFO among siblings).
//     Parent caps are enforced across sibling units of increment jobs.
//  2. If agents remain, drain the tier's overflow per the mode:
//     - fifo:        oldest unit at the tier eats all surplus.
//     - round_robin: rotate one agent at a time across the tier's
//     units (skipping units that won't accept more).
//     Overflow deliberately exceeds caps within the tier.
//  3. Any agents still free descend to the next priority tier.
//
// Top-priority tiers monopolize the agents they can USE: a tier's
// fill+overflow consumes every agent COMPATIBLE with it before any
// lower tier is reached, so capable agents never leak downward while
// the top tier still wants them. Agents that remain free after a tier
// are incompatible with it (or it is keyspace-saturated this cycle) and
// descend — this upholds the core invariant that a compatible agent is
// never idle while a compatible job has dispatchable work.
//
// **Strict / Max-Agents modes** (enforce_max_agents, max_agents_fifo,
// max_agents_round_robin) — all-tiers-then-overflow. Phase 1 walks
// every tier and fills each unit to its parent-aware capacity (no
// overflow during this phase, so every job at every priority gets its
// baseline). Phase 2 drains any remaining free agents:
//
//   - enforce_max_agents:       surplus idles (no overflow).
//   - max_agents_fifo:          surplus piles on the highest-priority
//     unit with remaining work (FIFO within
//     tier); descends only when the higher
//     tier has no schedulable gap left.
//   - max_agents_round_robin:   surplus rotates across all units in
//     priority-DESC then created_at-ASC
//     order — each pass visits higher
//     priorities first.
//
// The Max-Agents family guarantees every job's baseline cap (Phase 1
// fills all tiers without starvation) AND uses extras to accelerate
// higher-priority work first. The Priority family (fifo, round_robin)
// concentrates agents on the highest tier that can use them, descending
// only with agents that tier can't use — so lower tiers still receive
// agents that are exclusively compatible with them, but never agents the
// top tier could have used.
//
// Inputs:
//   - units: schedulable units, in any order. The function sorts.
//   - agents: idle agents available this cycle.
//   - mode: overflow policy.
//   - compatible: callback that reports whether an agent can run a unit
//     (binary-version filter etc.).
//
// Output: list of (unit, agent) pairs in the order they were allocated.
// One agent appears at most once in the output. Order is mostly
// informational — the dispatcher treats the list as a set.
func AllocateAgentsByPriority(
	units []UnitInfo,
	agents []AgentInfo,
	mode OverflowMode,
	compatible CompatibilityFn,
) []Allocation {
	if len(units) == 0 || len(agents) == 0 {
		return nil
	}
	if !mode.IsValid() {
		// Defensive default. The scheduling cycle should never call us
		// with an invalid mode; if it does, behave like the safest
		// option: respect max_agents strictly so we don't violate
		// operator intent.
		mode = OverflowEnforceMaxAgents
	}

	sortedUnits := sortUnitsByPriorityThenCreated(units)
	tiers := groupByPriority(sortedUnits)

	// Free agents pool. We mutate by removing entries as we allocate.
	free := make([]AgentInfo, len(agents))
	copy(free, agents)

	var allocations []Allocation

	// Parent-cycle allocation tracker: how many agents have been
	// allocated to each parent job in THIS cycle so far. For increment
	// jobs (multiple units per parent), this enforces the parent's
	// MaxAgents across all sibling units — otherwise each layer would
	// independently consume up to MaxAgents and the parent cap would
	// be silently multiplied by the layer count. For non-increment
	// jobs (one unit per parent) this is equivalent to per-unit
	// tracking.
	parentAllocated := make(map[uuid.UUID]int)

	// Per-unit allocation tracker for the cycle. Used by canAcceptMore
	// (overflow paths) to enforce MaxNewChunksThisCycle so we don't
	// pile extras on a keyspace-saturated unit while merged-style units
	// with plenty of remaining work sit at baseline.
	unitAllocated := make(map[uuid.UUID]int)

	// Detect increment jobs (multiple sibling units share a parent).
	// In overflow, increment jobs MUST respect parent MaxAgents (the
	// parent cap is the meaningful limit because layers share it);
	// non-increment jobs may exceed their unit's MaxAgents in overflow
	// up to MaxNewChunksThisCycle (the historical Priority-FIFO
	// "overflow exceeds cap" semantic). Pre-computing once is cheaper
	// than re-counting per canAcceptMore call.
	siblingCount := make(map[uuid.UUID]int, len(units))
	for _, u := range units {
		siblingCount[u.ParentJobID]++
	}
	isIncrement := func(parentID uuid.UUID) bool { return siblingCount[parentID] > 1 }

	// Two structurally different flows depending on whether overflow
	// is priority-respecting or priority-bypassing:
	//
	//   * Priority modes (fifo, round_robin): walk tier-by-tier; for
	//     each tier, fill to cap THEN drain overflow into the tier
	//     before descending. The top tier monopolizes every agent it can
	//     USE; only agents it can't use (incompatible, or it's saturated
	//     this cycle) descend to lower tiers — they're never frozen idle
	//     while a compatible lower-priority job has work.
	//
	//   * Strict / Max-Agents modes (enforce_max_agents,
	//     max_agents_fifo, max_agents_round_robin): fill every tier to
	//     cap first (no per-tier overflow), then either idle or drain
	//     extras globally by created_at (ignoring priority). Every job
	//     is guaranteed its baseline; extras boost throughput
	//     priority-agnostically.

	// canAcceptMore reports whether `u` can take another agent THIS
	// cycle under the overflow rules. Returns false when either:
	//   - the per-unit chunk hint (MaxNewChunksThisCycle) is exhausted
	//     by allocations already placed this cycle (keyspace-saturation
	//     signal computed by the cycle from interval coverage), OR
	//   - the unit belongs to an increment job AND the parent's
	//     MaxAgents cap is reached (non-increment jobs are allowed to
	//     exceed their unit's MaxAgents in Priority-mode overflow per
	//     the historical "tier-local overflow exceeds cap" semantic;
	//     increment jobs cannot because the parent cap is the only
	//     meaningful limit when sibling layer units share it).
	canAcceptMore := func(u UnitInfo) bool {
		if u.MaxNewChunksThisCycle > 0 && unitAllocated[u.ID] >= u.MaxNewChunksThisCycle {
			return false
		}
		if isIncrement(u.ParentJobID) && u.MaxAgents > 0 {
			parentUsed := u.ActiveAgentCount + parentAllocated[u.ParentJobID]
			if parentUsed >= u.MaxAgents {
				return false
			}
		}
		return true
	}

	switch mode {
	case OverflowFIFO, OverflowRoundRobin:
		// Priority flow: highest-priority tier monopolizes the agents it
		// can USE. For each tier (highest first): fill units to cap, then
		// drain the tier's overflow (exceeding caps within the tier per the
		// mode). Both phases give the tier first claim on every agent that
		// is COMPATIBLE with it. Whatever agents remain free after that are,
		// by construction, incompatible with this tier (or it's
		// keyspace-saturated this cycle and genuinely can't take more) — so
		// they descend to the highest-priority lower tier they CAN serve
		// rather than sit idle.
		//
		// This preserves strict priority for contention (a capable agent is
		// always consumed by the highest tier that can use it before any
		// lower tier is reached) while honoring the core invariant: a
		// compatible agent is never left idle when a compatible job has
		// dispatchable work. The previous unconditional `break` violated
		// that — it stranded an agent whose only runnable job lived in a
		// lower tier (e.g. a binary-6.x agent under an all-7.x top tier).
		for _, tier := range tiers {
			fillTier(tier, &allocations, &free, parentAllocated, unitAllocated, compatible)
			if len(free) > 0 {
				allocations = appendTierOverflow(allocations, tier, &free, mode, parentAllocated, unitAllocated, canAcceptMore, compatible)
			}
			if len(free) == 0 {
				break // nothing left to place
			}
			// Agents still free here couldn't be used by this tier; let
			// them descend to the next (lower-priority) tier.
		}

	default:
		// Strict / Max-Agents flow: fill ALL tiers first, then maybe
		// drain extras globally.
		for _, tier := range tiers {
			fillTier(tier, &allocations, &free, parentAllocated, unitAllocated, compatible)
		}
		if len(free) > 0 {
			switch mode {
			case OverflowMaxAgentsFIFO:
				byPriorityThenFIFO := sortUnitsByPriorityThenCreatedAt(sortedUnits)
				allocations = appendGlobalFIFOOverflow(allocations, byPriorityThenFIFO, &free, parentAllocated, unitAllocated, canAcceptMore, compatible)
			case OverflowMaxAgentsRoundRobin:
				byPriorityThenFIFO := sortUnitsByPriorityThenCreatedAt(sortedUnits)
				allocations = appendGlobalRoundRobinOverflow(allocations, byPriorityThenFIFO, &free, parentAllocated, unitAllocated, canAcceptMore, compatible)
				// OverflowEnforceMaxAgents: surplus idles. No-op.
			}
		}
	}

	return allocations
}

// localityScore rates how well running `unit` on `agent` avoids a file
// download. It is a pure dispatch TIEBREAK — never a gate — so a higher score
// only orders otherwise-equal candidates; it never changes priority,
// max_agents, or overflow fairness, and never excludes an agent. The payoff:
// a job prefers an agent that already holds (or reads off the share) its
// 15–40 GB wordlists/rules over one that must download them first.
//
//   - A network_direct agent reads every shareable wordlist/rule straight off
//     the mounted share and downloads nothing, so it scores the FULL
//     RequiredFiles count — the strongest possible match. getIdleAgents has
//     already excluded any network_direct agent whose mount is offline (via
//     IsShareReady), so a scored one can genuinely serve the unit.
//   - Any other agent scores the number of the unit's RequiredFiles it already
//     holds locally (HeldFiles).
//
// A score of 0 — the agent holds none of the files, its inventory is unknown,
// or the unit has no shareable files — leaves ordering exactly as it was
// before WS7: the earliest eligible candidate wins.
func localityScore(agent AgentInfo, unit UnitInfo) int {
	if len(unit.RequiredFiles) == 0 {
		return 0
	}
	if agent.StorageTier == models.StorageTierNetworkDirect {
		return len(unit.RequiredFiles)
	}
	if len(agent.HeldFiles) == 0 {
		return 0
	}
	n := 0
	for _, f := range unit.RequiredFiles {
		if agent.HeldFiles[f] {
			n++
		}
	}
	return n
}

// bestAgentForUnit returns the index into `free` of the agent `u` should take
// next: among agents that are compatible AND (when capacityGate is true)
// capacity-eligible, the one with the highest localityScore, ties broken by
// earliest index so the pre-WS7 pool/FIFO order is preserved wherever locality
// does not distinguish. Returns -1 when no free agent qualifies.
//
// `capacity` is the unit's remaining parent-aware headroom (-1 = unlimited).
// With capacityGate, a non-cloud agent is ineligible once capacity reaches 0
// (cloud agents bypass the max_agents budget, matching fillTier's original
// rule). The overflow paths pass capacityGate=false because they gate per unit
// through canAcceptMore instead, and pass any capacity value (ignored).
func bestAgentForUnit(u UnitInfo, free []AgentInfo, capacity int, capacityGate bool, compatible CompatibilityFn) int {
	best := -1
	bestScore := -1
	for j := range free {
		agent := free[j]
		if !compatible(u.ID, agent.ID) {
			continue
		}
		if capacityGate && !agent.IsCloud && capacity == 0 {
			continue
		}
		if s := localityScore(agent, u); best == -1 || s > bestScore {
			best = j
			bestScore = s
		}
	}
	return best
}

// fillTier runs the fill phase for one tier: walk each unit in tier
// order (already FIFO by created_at) and allocate compatible agents up
// to the parent-aware capacity. Mutates `allocations`, `free`, and
// `parentAllocated` through the passed pointers / map.
//
// Among the compatible, capacity-eligible agents for each unit it takes the
// best locality match first (bestAgentForUnit) — a pure tiebreak, so when no
// agent holds the unit's files (all score 0) it degenerates to the original
// "first eligible agent in pool order," preserving prior behavior.
//
// Parent capacity = MaxAgents − in-flight tasks across all sibling
// units − agents already allocated to this parent elsewhere in the
// cycle. For non-increment jobs (one unit per parent) this is
// equivalent to per-unit accounting; for increment jobs (multiple
// layer units per parent) it enforces the parent's MaxAgents across
// siblings.
func fillTier(
	tier []UnitInfo,
	allocationsPtr *[]Allocation,
	freePtr *[]AgentInfo,
	parentAllocated map[uuid.UUID]int,
	unitAllocated map[uuid.UUID]int,
	compatible CompatibilityFn,
) {
	free := *freePtr
	defer func() { *freePtr = free }()

	for i := range tier {
		u := &tier[i]
		capacity := -1 // -1 = unlimited
		if u.MaxAgents > 0 {
			capacity = u.MaxAgents - u.ActiveAgentCount - parentAllocated[u.ParentJobID]
			if capacity < 0 {
				capacity = 0
			}
		}

		// Cloud agents bypass the max_agents budget entirely.
		//
		// max_agents governs the SHARED on-prem pool — it exists so one job
		// cannot monopolise the fleet. A rented instance is dedicated to this
		// job, was paid for by this job's client, and competes with nobody, so
		// charging it against fleet fairness would mean a job with the default
		// max_agents=1 and one on-prem agent could never use capacity it had
		// already been billed for. Cloud concurrency is bounded by budget and
		// cloud_max_instances instead. bestAgentForUnit encodes the same rule
		// via its capacityGate: a non-cloud agent is skipped once capacity==0,
		// a cloud agent is not.
		for {
			j := bestAgentForUnit(*u, free, capacity, true, compatible)
			if j < 0 {
				break // no compatible, capacity-eligible agent left for this unit
			}
			agent := free[j]
			*allocationsPtr = append(*allocationsPtr, Allocation{UnitID: u.ID, AgentID: agent.ID})
			free = removeAt(free, j)
			if !agent.IsCloud {
				parentAllocated[u.ParentJobID]++
				if capacity > 0 {
					capacity--
				}
			}
			unitAllocated[u.ID]++
		}
	}
}

// sortUnitsByPriorityThenCreatedAt returns a copy of units sorted by
// Priority DESC (highest priority first), then CreatedAtNanos ASC
// (oldest first within a tier). Used by the Max-Agents-* overflow
// phase.
//
// Semantic note: Max-Agents modes differ from Priority modes only in
// Phase 1 — Phase 1 fills EVERY tier to cap (no starvation), while
// Priority modes drain each tier (including overflow) before
// descending. Phase 2 in both families respects priority: extras land
// on the highest-priority job with remaining work first, descending
// only when that tier is exhausted (no schedulable gap). The "ignore
// priority overall" semantic was wrong and produced surprising
// allocations (e.g., p4 mask absorbing all extras while p5 merged still
// had work).
func sortUnitsByPriorityThenCreatedAt(units []UnitInfo) []UnitInfo {
	out := make([]UnitInfo, len(units))
	copy(out, units)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].CreatedAtNanos < out[j].CreatedAtNanos
	})
	return out
}

// appendGlobalFIFOOverflow dumps every remaining agent on the
// highest-priority compatible unit, falling back to lower-priority
// tiers only after the higher tier has no compatible unit accepting
// more work. The caller passes `units` already sorted by priority DESC
// then created_at ASC, so walking units in order and filling each to its
// canAcceptMore limit produces priority-respecting routing with FIFO ordering
// inside each tier. Within a unit, bestAgentForUnit takes the best locality
// match first — a pure tiebreak (all-score-0 → pool order, i.e. the prior
// "first compatible" behavior). An agent incompatible with every accepting
// unit is left in the free pool for the next cycle. Mutates the free slice via
// the pointer.
func appendGlobalFIFOOverflow(
	allocations []Allocation,
	units []UnitInfo,
	freePtr *[]AgentInfo,
	parentAllocated map[uuid.UUID]int,
	unitAllocated map[uuid.UUID]int,
	canAcceptMore func(UnitInfo) bool,
	compatible CompatibilityFn,
) []Allocation {
	free := *freePtr
	defer func() { *freePtr = free }()
	if len(units) == 0 {
		return allocations
	}
	for i := range units {
		u := units[i]
		for len(free) > 0 && canAcceptMore(u) {
			j := bestAgentForUnit(u, free, 0, false, compatible)
			if j < 0 {
				break // no compatible agent left for this unit; cascade to next
			}
			agent := free[j]
			allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
			free = removeAt(free, j)
			parentAllocated[u.ParentJobID]++
			unitAllocated[u.ID]++
		}
		if len(free) == 0 {
			break
		}
	}
	return allocations
}

// appendGlobalRoundRobinOverflow rotates remaining agents across ALL
// units in priority-DESC then created_at-ASC order, one agent per unit
// per pass. Each cycle visits the highest-priority units first, then
// descends — so distribution is roughly proportional but tilted toward
// higher priorities. An agent incompatible with the current unit
// slides to the next compatible agent in the free pool for that unit's
// turn; if no agent fits, that unit is marked exhausted and the
// rotation moves on. Mutates the free slice via the pointer.
func appendGlobalRoundRobinOverflow(
	allocations []Allocation,
	units []UnitInfo,
	freePtr *[]AgentInfo,
	parentAllocated map[uuid.UUID]int,
	unitAllocated map[uuid.UUID]int,
	canAcceptMore func(UnitInfo) bool,
	compatible CompatibilityFn,
) []Allocation {
	free := *freePtr
	defer func() { *freePtr = free }()
	if len(units) == 0 {
		return allocations
	}

	exhaustedUnits := make(map[uuid.UUID]bool, len(units))
	unitIdx := 0
	for len(free) > 0 && len(exhaustedUnits) < len(units) {
		u := units[unitIdx%len(units)]
		unitIdx++
		if exhaustedUnits[u.ID] {
			continue
		}
		if !canAcceptMore(u) {
			exhaustedUnits[u.ID] = true
			continue
		}
		// Best locality match for this unit's turn; -1 = no compatible agent
		// left, so the unit is done for this rotation. Tiebreak only — with no
		// holders it is the earliest compatible agent, the prior behavior.
		j := bestAgentForUnit(u, free, 0, false, compatible)
		if j < 0 {
			exhaustedUnits[u.ID] = true
			continue
		}
		agent := free[j]
		allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
		free = removeAt(free, j)
		parentAllocated[u.ParentJobID]++
		unitAllocated[u.ID]++
	}
	return allocations
}

// appendTierOverflow distributes the remaining `free` agents across tier
// units under the given mode. Mutates the free slice via the pointer.
// canAcceptMore is the per-unit gate (keyspace + parent cap) shared with
// the global overflow paths.
func appendTierOverflow(
	allocations []Allocation,
	tier []UnitInfo,
	freePtr *[]AgentInfo,
	mode OverflowMode,
	parentAllocated map[uuid.UUID]int,
	unitAllocated map[uuid.UUID]int,
	canAcceptMore func(UnitInfo) bool,
	compatible CompatibilityFn,
) []Allocation {
	free := *freePtr
	defer func() { *freePtr = free }()

	switch mode {
	case OverflowFIFO:
		// Walk tier units oldest-first; fill each to its capacity
		// (canAcceptMore = keyspace not saturated, increment-job parent cap
		// honored) before cascading the surplus to the next-oldest — without
		// this cascade all extras pile on tier[0] even when its dispatcher
		// can't use them. Within a unit, bestAgentForUnit takes the best
		// locality match first; a pure tiebreak, so with nothing held it is
		// the earliest compatible agent — the prior behavior.
		if len(tier) == 0 {
			return allocations
		}
		for i := range tier {
			u := &tier[i]
			for len(free) > 0 && canAcceptMore(*u) {
				j := bestAgentForUnit(*u, free, 0, false, compatible)
				if j < 0 {
					break
				}
				agent := free[j]
				allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
				free = removeAt(free, j)
				parentAllocated[u.ParentJobID]++
				unitAllocated[u.ID]++
			}
			if len(free) == 0 {
				break
			}
		}
		return allocations

	case OverflowRoundRobin:
		// Cycle through tier units, giving each one agent at a time
		// (whichever compatible agent is at the head of the queue).
		// Skip units whose keyspace is saturated or whose parent cap
		// is reached (increment jobs).
		unitIdx := 0
		exhaustedUnits := make(map[uuid.UUID]bool, len(tier))

		for len(free) > 0 && len(exhaustedUnits) < len(tier) {
			u := tier[unitIdx%len(tier)]
			unitIdx++
			if exhaustedUnits[u.ID] {
				continue
			}
			if !canAcceptMore(u) {
				exhaustedUnits[u.ID] = true
				continue
			}

			// Best locality match for this unit's turn; -1 means no compatible
			// agent remains, so the unit is exhausted for this rotation.
			// Tiebreak only — degenerates to the earliest compatible agent when
			// nothing is held.
			j := bestAgentForUnit(u, free, 0, false, compatible)
			if j < 0 {
				exhaustedUnits[u.ID] = true
				continue
			}
			agent := free[j]
			allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
			free = removeAt(free, j)
			parentAllocated[u.ParentJobID]++
			unitAllocated[u.ID]++
		}
		return allocations
	}
	return allocations
}

// sortUnitsByPriorityThenCreated returns a copy of units sorted by
// priority DESC, then created_at ASC (oldest-first within tier).
func sortUnitsByPriorityThenCreated(units []UnitInfo) []UnitInfo {
	out := make([]UnitInfo, len(units))
	copy(out, units)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].CreatedAtNanos < out[j].CreatedAtNanos
	})
	return out
}

// groupByPriority returns the input split into runs sharing the same
// priority. Input must already be sorted by priority DESC.
func groupByPriority(units []UnitInfo) [][]UnitInfo {
	if len(units) == 0 {
		return nil
	}
	var tiers [][]UnitInfo
	start := 0
	for i := 1; i <= len(units); i++ {
		if i == len(units) || units[i].Priority != units[start].Priority {
			tier := make([]UnitInfo, i-start)
			copy(tier, units[start:i])
			tiers = append(tiers, tier)
			start = i
		}
	}
	return tiers
}

// removeAt removes element i from s and returns the resulting slice. Does
// not preserve order — uses the swap-with-last trick because the
// allocator doesn't depend on agent order within the free pool.
func removeAt(s []AgentInfo, i int) []AgentInfo {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}
