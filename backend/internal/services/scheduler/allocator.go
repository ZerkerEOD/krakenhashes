package scheduler

import (
	"sort"

	"github.com/google/uuid"
)

// AllocateAgentsByPriority assigns idle agents to schedulable units per
// the priority + max_agents + overflow rules from plan §6.4 and §9.
//
// The algorithm walks priority tiers from highest to lowest. At each
// tier:
//
//  1. Fill each unit to its max_agents (or take everything if
//     max_agents = 0), in unit creation order (FIFO among siblings).
//  2. If agents remain and the mode is not enforce_max_agents,
//     distribute the surplus within the tier per the mode:
//        - fifo:        oldest unit at the tier eats all surplus.
//        - round_robin: rotate one agent at a time across all units at
//                       the tier (skipping units already at their max).
//        - enforce_max_agents: no tier-local overflow; surplus descends.
//  3. Any agents still unassigned descend to the next priority tier.
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

	for _, tier := range tiers {
		// === Step 1: fill each unit to its max_agents ===
		for i := range tier {
			u := &tier[i]
			capacity := u.RemainingCapacity() // -1 if unlimited

			for j := 0; j < len(free); {
				agent := free[j]
				if !compatible(u.ID, agent.ID) {
					j++
					continue
				}
				if capacity == 0 {
					break
				}
				allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
				free = removeAt(free, j)
				if capacity > 0 {
					capacity--
				}
			}
		}

		// === Step 2: tier-local overflow ===
		if len(free) > 0 && mode != OverflowEnforceMaxAgents {
			allocations = appendTierOverflow(allocations, tier, &free, mode, compatible)
		}

		// === Step 3: anything left descends to the next tier ===
		if len(free) == 0 {
			break
		}
	}

	return allocations
}

// appendTierOverflow distributes the remaining `free` agents across tier
// units under the given mode. Mutates the free slice via the pointer.
func appendTierOverflow(
	allocations []Allocation,
	tier []UnitInfo,
	freePtr *[]AgentInfo,
	mode OverflowMode,
	compatible CompatibilityFn,
) []Allocation {
	free := *freePtr
	defer func() { *freePtr = free }()

	switch mode {
	case OverflowFIFO:
		// Oldest unit at tier (already at front after sort) gets all
		// surplus that's compatible with it.
		if len(tier) == 0 {
			return allocations
		}
		oldest := tier[0]
		for j := 0; j < len(free); {
			agent := free[j]
			if !compatible(oldest.ID, agent.ID) {
				j++
				continue
			}
			allocations = append(allocations, Allocation{UnitID: oldest.ID, AgentID: agent.ID})
			free = removeAt(free, j)
		}
		return allocations

	case OverflowRoundRobin:
		// Cycle through tier units, giving each one agent at a time
		// (whichever compatible agent is at the head of the queue).
		unitIdx := 0
		// Guard against degenerate cases (no compatible agent at all)
		// by capping iterations.
		exhaustedUnits := make(map[uuid.UUID]bool, len(tier))

		for len(free) > 0 && len(exhaustedUnits) < len(tier) {
			u := tier[unitIdx%len(tier)]
			unitIdx++
			if exhaustedUnits[u.ID] {
				continue
			}

			placed := false
			for j := 0; j < len(free); j++ {
				agent := free[j]
				if !compatible(u.ID, agent.ID) {
					continue
				}
				allocations = append(allocations, Allocation{UnitID: u.ID, AgentID: agent.ID})
				free = removeAt(free, j)
				placed = true
				break
			}
			if !placed {
				exhaustedUnits[u.ID] = true
			}
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
