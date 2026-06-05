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
	ID          uuid.UUID `json:"id" db:"id"`
	ParentJobID uuid.UUID `json:"parent_job_id" db:"parent_job_id"`
	LayerIndex  int       `json:"layer_index" db:"layer_index"`
	Status      string    `json:"status" db:"status"`
	// Priority and MaxAgents intentionally live on job_executions, not
	// here. They are read live via JOIN in scheduler/cycle.go
	// (buildUnitInfos) so that operator edits in the admin UI take
	// effect on the next scheduler cycle. Migration 000153 dropped the
	// denormalized columns from this table.
	AttackMode int `json:"attack_mode" db:"attack_mode"`
	// EffectiveKeyspace is total work in effective hashes (base × rules × salts).
	// Updated continuously by IngestProgressV2 as agent reports — DECREASES as
	// salts get removed during salted-hashlist jobs. Used as input to chunk-size
	// math, NOT for coverage tracking. Frontend reads this for "total work" display.
	EffectiveKeyspace    int64           `json:"effective_keyspace" db:"effective_keyspace"`
	// BaseKeyspace is the chunkable dimension (wordlist size for -a 0, etc.).
	// Set once at unit creation from job_executions.base_keyspace (or
	// job_increment_layers.base_keyspace for layer units). INVARIANT after
	// creation. Used for coverage tracking (intervals tile [0, BaseKeyspace))
	// AND as the divisor in chunk-size math (basePerSec = speed / multiplier
	// where multiplier = EffectiveKeyspace / BaseKeyspace). Nullable for old
	// rows that predate migration 000151; dispatcher treats nil as "skip — wait
	// for accurate keyspace."
	BaseKeyspace         *int64          `json:"base_keyspace,omitempty" db:"base_keyspace"`
	IsAccurateKeyspace   bool            `json:"is_accurate_keyspace" db:"is_accurate_keyspace"`
	// WordlistRefs holds one or more wordlist paths/refs. Single-entry for
	// -a 0/-a 9, two-entry for -a 1 (combinator), single-entry plus
	// MaskString for -a 6/-a 7 (hybrid). Stored as TEXT[].
	WordlistRefs         []string        `json:"wordlist_refs,omitempty" db:"wordlist_refs"`
	// RuleFileRefs is a list because hashcat supports rule stacking via
	// multiple -r flags (cartesian product). Stored as TEXT[] in Postgres
	// and read/written through pq.Array() in the repository.
	RuleFileRefs         []string        `json:"rule_file_refs,omitempty" db:"rule_file_refs"`
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
