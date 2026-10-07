package models

import (
	"time"

	"github.com/google/uuid"
)

// Storage backends for wordlists/rules (the migratable resources). Binaries,
// charsets, hashlists and per-client wordlists/potfiles always stay local.
const (
	StorageBackendLocal = "local"
	StorageBackendShare = "share"
)

// Network-share protocols. Used mainly to generate the correct agent-side
// mount command; the server itself just reads a compose-mounted path.
const (
	ShareTypeSMB = "smb"
	ShareTypeNFS = "nfs"
)

// Migration lifecycle states surfaced to the maintenance-mode UI.
const (
	MigrationStateIdle       = "idle"
	MigrationStateDraining   = "draining"   // waiting for agents to finish/flush; reads+writes still allowed
	MigrationStateMigrating  = "migrating"  // locked; copying files
	MigrationStateValidating = "validating" // MD5-verifying the copied files
	MigrationStateCompleted  = "completed"
	MigrationStateFailed     = "failed"
)

// Migration directions.
const (
	MigrationDirectionToShare = "to_share"
	MigrationDirectionToLocal = "to_local"
)

// NetworkShare is the single, server-global network-share configuration plus
// the active storage backend and live migration state (a singleton row in
// network_shares). It holds NO secrets: the server mounts the share via
// docker-compose and network_direct agents get their credentials from the
// operator, so only non-secret connection info lives here (used to pre-fill
// the agent setup command and validate the server mount).
type NetworkShare struct {
	ID           uuid.UUID         `json:"id"`
	ShareType    string            `json:"share_type"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	ServerHost   string            `json:"server_host"`
	ShareName    string            `json:"share_name"`
	MountOptions map[string]string `json:"mount_options"`

	StorageBackend      string     `json:"storage_backend"`
	MigrationState      string     `json:"migration_state"`
	MigrationDirection  *string    `json:"migration_direction,omitempty"`
	MigrationStartedAt  *time.Time `json:"migration_started_at,omitempty"`
	MigrationFinishedAt *time.Time `json:"migration_finished_at,omitempty"`
	MigrationError      *string    `json:"migration_error,omitempty"`

	LastValidatedAt     *time.Time `json:"last_validated_at,omitempty"`
	LastValidationError *string    `json:"last_validation_error,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MigrationActive reports whether a migration is currently in a phase that
// blocks normal data access (locked copy or validation). The drain phase is
// deliberately excluded: reads/writes continue while agents finish and flush.
func (s *NetworkShare) MigrationActive() bool {
	return s.MigrationState == MigrationStateMigrating || s.MigrationState == MigrationStateValidating
}

// MigrationInProgress reports whether any migration phase (including drain) is
// running — used to show the maintenance UI from the moment it is triggered.
func (s *NetworkShare) MigrationInProgress() bool {
	switch s.MigrationState {
	case MigrationStateDraining, MigrationStateMigrating, MigrationStateValidating:
		return true
	default:
		return false
	}
}
