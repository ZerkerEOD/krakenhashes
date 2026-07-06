package models

import "time"

// Diagnostic scopes.
const (
	DiagScopeAgent = "agent"
	DiagScopeJob   = "job"
	DiagScopeTask  = "task"
)

// Diagnostic severities.
const (
	DiagSeverityInfo    = "info"
	DiagSeverityWarning = "warning"
	DiagSeverityError   = "error"
)

// Agent "why isn't it working" reason codes. These are stable machine-readable
// keys; the detail field carries the human-readable specifics (e.g. which
// binary version the agent provides vs. what the schedulable jobs require).
const (
	DiagReasonNoCompatibleJob   = "no_compatible_job"   // binary/attack-mode mismatch with all schedulable units
	DiagReasonBlocklisted       = "blocklisted"         // benchmark blocklist active for the eligible combos
	DiagReasonBenchmarking      = "benchmarking"        // a benchmark is in flight for this agent
	DiagReasonOutsideSchedule   = "outside_schedule"    // current time outside the agent's schedule window
	DiagReasonAgentDisabled     = "agent_disabled"      // is_enabled = false
	DiagReasonShuttingDown      = "shutting_down"       // graceful shutdown in progress
	DiagReasonRejectionCooldown = "rejection_cooldown"  // recently rejected a task
	DiagReasonNoSchedulableWork = "no_schedulable_work" // no units with dispatchable work this cycle
	DiagReasonAtCapacity        = "at_capacity"         // compatible units all at cap (enforce_max_agents)
)

// SchedulingDiagnostic is one deduplicated diagnostic row: a single
// (scope, scope_id, reason_code) tuple whose count/last_seen are bumped in
// place on every recurrence rather than inserting new rows.
type SchedulingDiagnostic struct {
	ID         int64      `json:"id"`
	Scope      string     `json:"scope"`
	ScopeID    string     `json:"scope_id"`
	ReasonCode string     `json:"reason_code"`
	Severity   string     `json:"severity"`
	Detail     string     `json:"detail"`
	Count      int64      `json:"count"`
	FirstSeen  time.Time  `json:"first_seen"`
	LastSeen   time.Time  `json:"last_seen"`
	ClearedAt  *time.Time `json:"cleared_at,omitempty"`
}
