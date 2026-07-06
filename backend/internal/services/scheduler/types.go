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
	// OverflowFIFO ("Priority - FIFO" in the UI): sends all overflow
	// agents at a tier to the oldest job at that tier (by created_at
	// ASC). Tier-local — lower-priority tiers may starve if the top
	// tier has agents to spare.
	OverflowFIFO OverflowMode = "fifo"

	// OverflowRoundRobin ("Priority - Round Robin" in the UI):
	// distributes overflow agents one at a time across all eligible
	// jobs at the tier. Tier-local — lower-priority tiers may starve.
	OverflowRoundRobin OverflowMode = "round_robin"

	// OverflowEnforceMaxAgents disallows overflow entirely. After every
	// tier has filled to its max_agents cap, any remaining agents stay
	// idle. Most predictable; can waste capacity.
	OverflowEnforceMaxAgents OverflowMode = "enforce_max_agents"

	// OverflowMaxAgentsFIFO ("Max Agents - FIFO" in the UI): phase 1
	// fills every job at every priority tier to its max_agents (strict
	// caps, like OverflowEnforceMaxAgents — no tier starves). Phase 2
	// dumps surplus agents on the highest-priority job with remaining
	// work (FIFO inside that tier), descending only when that tier has
	// no schedulable gap left. Differs from OverflowFIFO in Phase 1:
	// OverflowFIFO drains overflow into a single tier before descending,
	// starving lower tiers; this mode guarantees baseline first, then
	// uses extras to accelerate the highest-priority work.
	OverflowMaxAgentsFIFO OverflowMode = "max_agents_fifo"

	// OverflowMaxAgentsRoundRobin ("Max Agents - Round Robin" in the
	// UI): same phase 1 as OverflowMaxAgentsFIFO. Phase 2 rotates
	// surplus agents through units in priority-DESC then created_at-ASC
	// order — each round visits the highest-priority units first then
	// descends, so distribution is roughly proportional but tilted
	// toward higher priorities.
	OverflowMaxAgentsRoundRobin OverflowMode = "max_agents_round_robin"
)

// IsValid reports whether the mode is one of the documented values.
func (m OverflowMode) IsValid() bool {
	switch m {
	case OverflowFIFO,
		OverflowRoundRobin,
		OverflowEnforceMaxAgents,
		OverflowMaxAgentsFIFO,
		OverflowMaxAgentsRoundRobin:
		return true
	}
	return false
}

// UnitInfo is the subset of models.SchedulingUnit that the allocator needs.
// Keeping it narrow lets unit tests construct cheap synthetic units without
// touching the database. The selector populates it from the repository in
// production.
//
// `MaxAgents` and `ActiveAgentCount` are PARENT-job scoped, not per-unit.
// For non-increment jobs (one unit per parent) this is equivalent to
// per-unit accounting. For increment jobs (multiple layer units per
// parent), `MaxAgents` is the cap across all sibling units, and
// `ActiveAgentCount` is the sum of in-flight tasks across all of them.
// The allocator enforces this by tracking per-parent allocation across
// the cycle.
type UnitInfo struct {
	ID          uuid.UUID
	ParentJobID uuid.UUID
	Priority    int

	// MaxAgents is the PARENT JOB's max_agents — the cap on total
	// agents across all sibling units sharing the same ParentJobID.
	// 0 means unlimited. All units of the same parent carry the same
	// value (populated by buildUnitInfos via JOIN to job_executions).
	MaxAgents int

	BinaryVersion string

	// ActiveAgentCount is the count of currently-assigned-or-running
	// tasks across ALL units of the same parent job — not just this
	// unit. The allocator subtracts it from MaxAgents to compute
	// remaining parent capacity. All sibling units of the same parent
	// carry the same value.
	ActiveAgentCount int

	// CreatedAtNanos sortable timestamp for tier-internal FIFO selection.
	// Stored as nanos so tests don't depend on time.Time equality
	// gymnastics.
	CreatedAtNanos int64

	// MaxNewChunksThisCycle is the cycle's pre-computed upper bound on
	// how many new chunks can be dispatched on this unit THIS cycle.
	// Computed in buildUnitInfos from:
	//   - remaining effective keyspace (base_keyspace - covered intervals,
	//     times multiplier)
	//   - target chunk duration × conservative agent speed (chunk size
	//     estimate in effective candidates)
	//   - floor of MaxAgents so baseline cap-fill always fits
	//
	// Used by the FIFO and Round-Robin overflow paths to cascade extras
	// off a unit whose keyspace is saturated for this cycle. Without it,
	// "all extras to oldest" piles agents on a unit whose dispatcher
	// will fail to create tasks (firstGap returns empty), leaving those
	// agents idle for the cycle. A value of 0 means "no overflow
	// allocations beyond Phase 1 cap" — the unit is full for the cycle.
	// A negative or unset value means "no hint available" — allocator
	// falls back to unbounded (current behavior).
	MaxNewChunksThisCycle int
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

// JobExecutionStarter transitions a job_execution from 'pending' to
// 'running' on first dispatch. The repository-side StartExecution is
// idempotent (it gates on WHERE status='pending'), so calling it for
// every dispatched task is safe and only the first call has effect.
// Production implementation is services.JobExecutionService.
type JobExecutionStarter interface {
	StartJobExecution(ctx context.Context, jobExecutionID uuid.UUID) error
}

// DiagnosticsRecorder records deduplicated "why isn't this agent working"
// reasons so the UI can explain idleness without the scheduler spamming the DB.
// Record coalesces recurrences; ClearScope resolves a scope's reasons (e.g. the
// agent picked up work). Production implementation is
// services.DiagnosticsService. The interface keeps the scheduler package free of
// a circular import; a nil recorder disables capture.
type DiagnosticsRecorder interface {
	Record(scope, scopeID, reason, severity, detail string)
	ClearScope(scope, scopeID string)
}
