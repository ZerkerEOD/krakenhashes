// Package migrationgate holds the process-global state of an in-progress
// storage migration so the scheduler (pause dispatch), the upload handlers
// (freeze writes) and the HTTP maintenance middleware (serve the landing page)
// can all consult one source of truth without importing the migration engine.
//
// The durable state lives in the network_shares row; this is the fast in-memory
// mirror plus live progress for the maintenance landing page.
package migrationgate

import (
	"sync"
	"time"
)

// Phases mirror models.MigrationState.
const (
	PhaseIdle       = "idle"
	PhaseDraining   = "draining"
	PhaseMigrating  = "migrating"
	PhaseValidating = "validating"
	PhaseCompleted  = "completed"
	PhaseFailed     = "failed"
)

// Progress is the live snapshot shown on the maintenance landing page and the
// migration-status endpoint.
type Progress struct {
	Phase          string     `json:"phase"`
	Direction      string     `json:"direction"`
	Message        string     `json:"message"`
	FilesTotal     int        `json:"files_total"`
	FilesDone      int        `json:"files_done"`
	BytesTotal     int64      `json:"bytes_total"`
	BytesDone      int64      `json:"bytes_done"`
	CurrentFile    string     `json:"current_file"`
	ThroughputMBps float64    `json:"throughput_mbps"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	DrainDeadline  *time.Time `json:"drain_deadline,omitempty"`
	AgentsBusy     int        `json:"agents_busy"`
	Error          string     `json:"error,omitempty"`
}

var (
	mu   sync.RWMutex
	prog = Progress{Phase: PhaseIdle}
)

// Get returns a copy of the current progress snapshot.
func Get() Progress {
	mu.RLock()
	defer mu.RUnlock()
	return prog
}

// Set mutates the progress snapshot under the lock.
func Set(mutate func(*Progress)) {
	mu.Lock()
	defer mu.Unlock()
	mutate(&prog)
}

// SetPhase updates just the phase.
func SetPhase(phase string) {
	mu.Lock()
	defer mu.Unlock()
	prog.Phase = phase
}

// Phase returns the current phase.
func Phase() string {
	mu.RLock()
	defer mu.RUnlock()
	return prog.Phase
}

// Reset returns the gate to idle (called after a migration finishes/fails and
// the operator dismisses it, or at startup).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	prog = Progress{Phase: PhaseIdle}
}

// DispatchPaused reports whether the scheduler must NOT dispatch new work.
// True throughout an active migration (drain + locked phases) so agents drain
// and stay drained until it completes.
func DispatchPaused() bool {
	switch Phase() {
	case PhaseDraining, PhaseMigrating, PhaseValidating:
		return true
	default:
		return false
	}
}

// WritesFrozen reports whether wordlist/rule uploads must be rejected. Only the
// locked copy/validate phases freeze writes; the drain phase still allows them
// so agents can finish and flush.
func WritesFrozen() bool {
	switch Phase() {
	case PhaseMigrating, PhaseValidating:
		return true
	default:
		return false
	}
}

// MaintenanceActive reports whether the HTTP maintenance landing page should be
// served for data routes. Locked phases only.
func MaintenanceActive() bool {
	return WritesFrozen()
}
