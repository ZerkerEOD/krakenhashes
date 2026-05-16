// Package scheduler is the new scheduling pipeline introduced by the
// rewrite. The 6-step cycle from plan §6.1 lives here:
//  1. EvictTimedOutTasks       (sweeper.go)
//  2. RefreshCompatibilityCache (not in Phase A; lands later)
//  3. SelectSchedulableUnits   (selector.go)
//  4. AllocateAgentsByPriority (allocator.go)
//  5. DispatchOneChunkPerAgent (dispatcher.go)
//  6. CommitTransaction        (each function owns its own transaction)
//
// This package intentionally has no dependency on the legacy
// job_scheduling_service.go. The clean cutover replaces the old pipeline
// in one PR; until then the legacy code keeps running on master and this
// package is exercised only by its tests.
package scheduler

import (
	"context"

	"github.com/google/uuid"
)

// OverflowMode is the per-tier overflow policy applied after every unit at
// a given priority tier has been filled to its max_agents. Values match
// the agent_overflow_allocation_mode system_setting documented in
// migration 000149.
type OverflowMode string

const (
	// OverflowFIFO sends all overflow agents at a tier to the oldest job
	// at that tier (by created_at ASC).
	OverflowFIFO OverflowMode = "fifo"

	// OverflowRoundRobin distributes overflow agents one at a time across
	// all eligible jobs at the tier.
	OverflowRoundRobin OverflowMode = "round_robin"

	// OverflowEnforceMaxAgents disallows tier-local overflow entirely.
	// Surplus agents descend straight to the next priority tier.
	OverflowEnforceMaxAgents OverflowMode = "enforce_max_agents"
)

// IsValid reports whether the mode is one of the three documented values.
func (m OverflowMode) IsValid() bool {
	switch m {
	case OverflowFIFO, OverflowRoundRobin, OverflowEnforceMaxAgents:
		return true
	}
	return false
}

// UnitInfo is the subset of models.SchedulingUnit that the allocator needs.
// Keeping it narrow lets unit tests construct cheap synthetic units without
// touching the database. The selector populates it from the repository in
// production.
type UnitInfo struct {
	ID            uuid.UUID
	Priority      int
	MaxAgents     int // 0 means unlimited at this tier
	BinaryVersion string

	// ActiveAgentCount is the count of currently-assigned-or-running
	// tasks against this unit. Used to subtract from MaxAgents when
	// computing per-tier capacity.
	ActiveAgentCount int

	// CreatedAtNanos sortable timestamp for tier-internal FIFO selection.
	// Stored as nanos so tests don't depend on time.Time equality
	// gymnastics.
	CreatedAtNanos int64
}

// RemainingCapacity returns how many additional agents this unit can
// accept at its tier under strict-max-agents accounting, or -1 if
// MaxAgents is 0 (unlimited). The allocator clamps this against the
// available agent pool size at runtime.
func (u UnitInfo) RemainingCapacity() int {
	if u.MaxAgents <= 0 {
		return -1
	}
	if u.ActiveAgentCount >= u.MaxAgents {
		return 0
	}
	return u.MaxAgents - u.ActiveAgentCount
}

// AgentInfo is what the allocator needs to know about an agent.
type AgentInfo struct {
	ID             int
	BinaryVersion  string
	BenchmarkSpeed int64 // hashes/sec, from agent_benchmarks cache
}

// Allocation is one (unit, agent) pair the allocator decided on for this
// scheduling cycle. The dispatcher consumes the list and inserts the
// corresponding intervals + tasks.
type Allocation struct {
	UnitID  uuid.UUID
	AgentID int
}

// CompatibilityFn reports whether a given agent is compatible with a given
// unit. The production implementation is backed by the compatibility cache
// (binary-version match etc.); tests pass a synthetic closure.
type CompatibilityFn func(unitID uuid.UUID, agentID int) bool

// BinaryResolver picks the hashcat binary version ID to use for a given
// (agent, job_execution) pair. Production implementation is
// services.JobExecutionService.DetermineBinaryForTask, which walks the
// agent + job binary-version patterns through the version resolver.
// The interface keeps the scheduler package free of a circular import.
type BinaryResolver interface {
	DetermineBinaryForTask(ctx context.Context, agentID int, jobExecutionID uuid.UUID) (int64, error)
}
