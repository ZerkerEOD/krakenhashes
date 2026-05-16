// Models for the scheduler rewrite. The legacy job/task models in jobs.go
// remain in place during the cutover window; these are the new types that
// the rewrite's dispatcher reads/writes.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SchedulingUnit is the atom of scheduling in the rewrite. One row per
// schedulable "thing": a non-increment job has exactly one unit, an
// --increment 1-4 job has four (one per length). The dispatcher selects,
// allocates agents to, and dispatches chunks for SchedulingUnits — it does
// not see job_executions directly.
type SchedulingUnit struct {
	ID                   uuid.UUID       `json:"id" db:"id"`
	ParentJobID          uuid.UUID       `json:"parent_job_id" db:"parent_job_id"`
	LayerIndex           int             `json:"layer_index" db:"layer_index"`
	Status               string          `json:"status" db:"status"`
	Priority             int             `json:"priority" db:"priority"`
	MaxAgents            int             `json:"max_agents" db:"max_agents"`
	AttackMode           int             `json:"attack_mode" db:"attack_mode"`
	EffectiveKeyspace    int64           `json:"effective_keyspace" db:"effective_keyspace"`
	IsAccurateKeyspace   bool            `json:"is_accurate_keyspace" db:"is_accurate_keyspace"`
	WordlistRef          *string         `json:"wordlist_ref,omitempty" db:"wordlist_ref"`
	RuleFileRef          *string         `json:"rule_file_ref,omitempty" db:"rule_file_ref"`
	MaskString           *string         `json:"mask_string,omitempty" db:"mask_string"`
	CustomCharsets       json.RawMessage `json:"custom_charsets,omitempty" db:"custom_charsets"`
	RetryBudgetRemaining int             `json:"retry_budget_remaining" db:"retry_budget_remaining"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at" db:"updated_at"`
}

// SchedulingUnit status values. Match the CHECK constraint in migration
// 000146 exactly.
const (
	SchedulingUnitStatusPending   = "pending"
	SchedulingUnitStatusRunning   = "running"
	SchedulingUnitStatusCompleted = "completed"
	SchedulingUnitStatusFailed    = "failed"
	SchedulingUnitStatusCancelled = "cancelled"
)

// KeyspaceInterval is one explicit slice of a SchedulingUnit's keyspace.
// Coverage of a unit is the union of its non-failed intervals; gaps are the
// complement of that union within [0, EffectiveKeyspace). The range is
// half-open: [RangeStart, RangeEnd) in BASE units.
type KeyspaceInterval struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	SchedulingUnitID uuid.UUID  `json:"scheduling_unit_id" db:"scheduling_unit_id"`
	RangeStart       int64      `json:"range_start" db:"range_start"`
	RangeEnd         int64      `json:"range_end" db:"range_end"`
	Status           string     `json:"status" db:"status"`
	TaskID           *uuid.UUID `json:"task_id,omitempty" db:"task_id"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// KeyspaceInterval status values. Match the CHECK constraint in migration
// 000147 exactly.
const (
	KeyspaceIntervalStatusAssigned  = "assigned"
	KeyspaceIntervalStatusRunning   = "running"
	KeyspaceIntervalStatusCompleted = "completed"
	KeyspaceIntervalStatusFailed    = "failed"
)

// UndispatchedRange is one gap returned by the dispatcher's
// undispatched_ranges query. Half-open [Start, End) in BASE units.
type UndispatchedRange struct {
	Start int64
	End   int64
}

// Size returns the gap width in base units.
func (r UndispatchedRange) Size() int64 { return r.End - r.Start }
