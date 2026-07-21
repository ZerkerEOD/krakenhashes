package models

import (
	"time"

	"github.com/google/uuid"
)

// LoopbackSessionStatus is the lifecycle state of a loopback session.
type LoopbackSessionStatus string

const (
	// LoopbackSessionStatusWaiting: round-0 jobs are still running; the controller
	// is waiting for them to reach a terminal state before the first delta round.
	LoopbackSessionStatusWaiting LoopbackSessionStatus = "waiting"
	// LoopbackSessionStatusActive: at least one delta round has been spawned and the
	// controller is monitoring the current round's re-runs.
	LoopbackSessionStatusActive LoopbackSessionStatus = "active"
	// LoopbackSessionStatusCompleted: a round produced no new plaintext (dry), or the
	// max-rounds cap was reached.
	LoopbackSessionStatusCompleted LoopbackSessionStatus = "completed"
	LoopbackSessionStatusFailed    LoopbackSessionStatus = "failed"
	LoopbackSessionStatusCancelled LoopbackSessionStatus = "cancelled"
)

// LoopbackSourceType records how a session was started.
type LoopbackSourceType string

const (
	LoopbackSourceWorkflow LoopbackSourceType = "workflow"
	LoopbackSourcePreset   LoopbackSourceType = "preset"
	LoopbackSourceCustom   LoopbackSourceType = "custom"
)

// LoopbackJobRole distinguishes a round-0 original job from a controller-spawned re-run.
type LoopbackJobRole string

const (
	LoopbackJobRoleOriginal LoopbackJobRole = "original"
	LoopbackJobRoleRerun    LoopbackJobRole = "rerun"
)

// LoopbackSession is the durable controller for one loopback group: a workflow run
// or a standalone preset/custom run. It survives a backend restart so a session that
// is waiting on long-running round-0 jobs is not lost.
type LoopbackSession struct {
	ID               uuid.UUID             `json:"id" db:"id"`
	HashlistID       int64                 `json:"hashlist_id" db:"hashlist_id"`
	SourceType       LoopbackSourceType    `json:"source_type" db:"source_type"`
	SourceWorkflowID *uuid.UUID            `json:"source_workflow_id,omitempty" db:"source_workflow_id"`
	Name             string                `json:"name" db:"name"`
	Status           LoopbackSessionStatus `json:"status" db:"status"`
	CurrentRound     int                   `json:"current_round" db:"current_round"`
	MaxRounds        int                   `json:"max_rounds" db:"max_rounds"`
	ErrorMessage     *string               `json:"error_message,omitempty" db:"error_message"`
	CreatedBy        *uuid.UUID            `json:"created_by,omitempty" db:"created_by"`
	CreatedAt        time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at" db:"updated_at"`

	// Populated by joins for the "Pending Loopback" UI (not persisted directly).
	Jobs []LoopbackSessionJob `json:"jobs,omitempty"`
}

// LoopbackSessionJob links a job_execution to a session at a specific round.
type LoopbackSessionJob struct {
	ID             int64           `json:"id" db:"id"`
	SessionID      uuid.UUID       `json:"session_id" db:"session_id"`
	JobExecutionID uuid.UUID       `json:"job_execution_id" db:"job_execution_id"`
	Round          int             `json:"round" db:"round"`
	Role           LoopbackJobRole `json:"role" db:"role"`
	IsMutatable    bool            `json:"is_mutatable" db:"is_mutatable"`
	OriginJobID    *uuid.UUID      `json:"origin_job_id,omitempty" db:"origin_job_id"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`

	// Fields populated by a join with job_executions for UI rendering.
	JobName   string             `json:"job_name,omitempty" db:"job_name"`
	JobStatus JobExecutionStatus `json:"job_status,omitempty" db:"job_status"`
}

// IsMutatableAttack reports whether an attack config's mutating part can be re-run
// against a delta wordlist. Only wordlist-consuming modes whose *mutation* is
// separable from the wordlist qualify:
//   - Straight (0) WITH rules  → the rules are the mutation (delta × rules)
//   - Hybrid (6/7)             → the mask is the mutation (delta ± mask)
//
// Ineligible (return false), and therefore only feed the delta pool with their cracks:
//   - Straight (0) with NO rules → nothing mutates; the word was already tried as-is
//   - Brute-force (3)            → no wordlist to swap the delta into
//   - Combination (1)            → no clean "which side is the mutation" (each side can
//     carry its own rules); also not offered as a preset/custom attack here
//   - Association (9)            → 1:1 wordlist mapping, no loopback semantics
func IsMutatableAttack(mode AttackMode, ruleIDs IDArray) bool {
	switch mode {
	case AttackModeStraight:
		return len(ruleIDs) > 0
	case AttackModeHybridWordlistMask, AttackModeHybridMaskWordlist:
		return true
	default:
		return false
	}
}
