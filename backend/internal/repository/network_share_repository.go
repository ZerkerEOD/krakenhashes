package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// NetworkShareRepository owns the singleton network_shares row (the
// server-global share config + active storage backend + migration state).
type NetworkShareRepository struct {
	db *db.DB
}

// NewNetworkShareRepository creates a repository.
func NewNetworkShareRepository(database *db.DB) *NetworkShareRepository {
	return &NetworkShareRepository{db: database}
}

const networkShareColumns = `
	id, share_type, name, enabled, server_host, share_name, mount_options,
	storage_backend, migration_state, migration_direction,
	migration_started_at, migration_finished_at, migration_error,
	last_validated_at, last_validation_error, created_at, updated_at`

func scanNetworkShare(s interface{ Scan(...interface{}) error }) (*models.NetworkShare, error) {
	var (
		ns          models.NetworkShare
		mountOpts   []byte
		direction   sql.NullString
		startedAt   sql.NullTime
		finishedAt  sql.NullTime
		migErr      sql.NullString
		validatedAt sql.NullTime
		validErr    sql.NullString
	)
	err := s.Scan(
		&ns.ID, &ns.ShareType, &ns.Name, &ns.Enabled, &ns.ServerHost, &ns.ShareName, &mountOpts,
		&ns.StorageBackend, &ns.MigrationState, &direction,
		&startedAt, &finishedAt, &migErr,
		&validatedAt, &validErr, &ns.CreatedAt, &ns.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	ns.MountOptions = map[string]string{}
	if len(mountOpts) > 0 {
		// Ignore a malformed blob rather than fail the whole read; mount
		// options are advisory (they only shape the agent setup command).
		_ = json.Unmarshal(mountOpts, &ns.MountOptions)
	}
	if direction.Valid {
		ns.MigrationDirection = &direction.String
	}
	if startedAt.Valid {
		ns.MigrationStartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		ns.MigrationFinishedAt = &finishedAt.Time
	}
	if migErr.Valid {
		ns.MigrationError = &migErr.String
	}
	if validatedAt.Valid {
		ns.LastValidatedAt = &validatedAt.Time
	}
	if validErr.Valid {
		ns.LastValidationError = &validErr.String
	}
	return &ns, nil
}

// Get returns the singleton network-share row, or (nil, nil) when the share
// has never been configured (local-only, today's default).
func (r *NetworkShareRepository) Get(ctx context.Context) (*models.NetworkShare, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+networkShareColumns+` FROM network_shares WHERE singleton = true`)
	ns, err := scanNetworkShare(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get network share: %w", err)
	}
	return ns, nil
}

// GetStorageBackend returns the active backend ('local'|'share'), defaulting
// to 'local' when no share row exists. Cheap enough for the storage-path
// resolver to call on a cache miss.
func (r *NetworkShareRepository) GetStorageBackend(ctx context.Context) (string, error) {
	var backend string
	err := r.db.QueryRowContext(ctx, `SELECT storage_backend FROM network_shares WHERE singleton = true`).Scan(&backend)
	if err == sql.ErrNoRows {
		return models.StorageBackendLocal, nil
	}
	if err != nil {
		return models.StorageBackendLocal, fmt.Errorf("get storage backend: %w", err)
	}
	return backend, nil
}

// Save upserts the singleton row with every field of ns (read-modify-write).
// Callers Get the current row, mutate it, then Save — this keeps config edits
// and migration/backend transitions on one simple path. updated_at is stamped
// here; created_at is preserved on update.
func (r *NetworkShareRepository) Save(ctx context.Context, ns *models.NetworkShare) error {
	if ns.ID == uuid.Nil {
		ns.ID = uuid.New()
	}
	if ns.ShareType == "" {
		ns.ShareType = models.ShareTypeSMB
	}
	if ns.StorageBackend == "" {
		ns.StorageBackend = models.StorageBackendLocal
	}
	if ns.MigrationState == "" {
		ns.MigrationState = models.MigrationStateIdle
	}
	mountOpts, err := json.Marshal(ns.MountOptions)
	if err != nil {
		return fmt.Errorf("marshal mount options: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO network_shares (
			id, singleton, share_type, name, enabled, server_host, share_name, mount_options,
			storage_backend, migration_state, migration_direction,
			migration_started_at, migration_finished_at, migration_error,
			last_validated_at, last_validation_error, updated_at
		) VALUES (
			$1, true, $2, $3, $4, $5, $6, $7,
			$8, $9, $10,
			$11, $12, $13,
			$14, $15, NOW()
		)
		ON CONFLICT (singleton) DO UPDATE SET
			share_type            = EXCLUDED.share_type,
			name                  = EXCLUDED.name,
			enabled               = EXCLUDED.enabled,
			server_host           = EXCLUDED.server_host,
			share_name            = EXCLUDED.share_name,
			mount_options         = EXCLUDED.mount_options,
			storage_backend       = EXCLUDED.storage_backend,
			migration_state       = EXCLUDED.migration_state,
			migration_direction   = EXCLUDED.migration_direction,
			migration_started_at  = EXCLUDED.migration_started_at,
			migration_finished_at = EXCLUDED.migration_finished_at,
			migration_error       = EXCLUDED.migration_error,
			last_validated_at     = EXCLUDED.last_validated_at,
			last_validation_error = EXCLUDED.last_validation_error,
			updated_at            = NOW()
	`,
		ns.ID, ns.ShareType, ns.Name, ns.Enabled, ns.ServerHost, ns.ShareName, mountOpts,
		ns.StorageBackend, ns.MigrationState, nsNullString(ns.MigrationDirection),
		nsNullTime(ns.MigrationStartedAt), nsNullTime(ns.MigrationFinishedAt), nsNullString(ns.MigrationError),
		nsNullTime(ns.LastValidatedAt), nsNullString(ns.LastValidationError),
	)
	if err != nil {
		return fmt.Errorf("save network share: %w", err)
	}
	return nil
}

// nsNullString / nsNullTime convert optional pointers to driver-friendly
// nulls (prefixed to avoid colliding with sso_repository's nullString).
func nsNullString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func nsNullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}
