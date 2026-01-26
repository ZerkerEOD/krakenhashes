package binary

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// store implements the Store interface for PostgreSQL
type store struct {
	db *sql.DB
}

// NewStore creates a new PostgreSQL store instance
func NewStore(db *sql.DB) Store {
	return &store{db: db}
}

// CreateVersion implements Store.CreateVersion
func (s *store) CreateVersion(ctx context.Context, version *BinaryVersion) error {
	err := s.db.QueryRowContext(
		ctx,
		queries.CreateBinaryVersion,
		version.BinaryType,
		version.CompressionType,
		version.SourceURL,
		version.FileName,
		version.MD5Hash,
		version.FileSize,
		version.CreatedBy,
		version.IsActive,
		version.VerificationStatus,
		version.IsDefault,
		version.SourceType,
		version.Description,
		version.Version,
	).Scan(&version.ID, &version.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create binary version: %w", err)
	}

	return nil
}

// GetVersion implements Store.GetVersion
func (s *store) GetVersion(ctx context.Context, id int64) (*BinaryVersion, error) {
	version := &BinaryVersion{}
	err := s.db.QueryRowContext(ctx, queries.GetBinaryVersion, id).Scan(
		&version.ID,
		&version.BinaryType,
		&version.CompressionType,
		&version.SourceURL,
		&version.FileName,
		&version.MD5Hash,
		&version.FileSize,
		&version.CreatedAt,
		&version.CreatedBy,
		&version.IsActive,
		&version.IsDefault,
		&version.LastVerifiedAt,
		&version.VerificationStatus,
		&version.SourceType,
		&version.Description,
		&version.Version,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("binary version not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get binary version: %w", err)
	}

	return version, nil
}

// ListVersions implements Store.ListVersions
func (s *store) ListVersions(ctx context.Context, filters map[string]interface{}) ([]*BinaryVersion, error) {
	query := queries.ListBinaryVersionsBase
	var args []interface{}
	argCount := 1

	// Add filters to query
	if binaryType, ok := filters["binary_type"]; ok {
		query += fmt.Sprintf(" AND binary_type = $%d", argCount)
		args = append(args, binaryType)
		argCount++
	}
	if isActive, ok := filters["is_active"]; ok {
		query += fmt.Sprintf(" AND is_active = $%d", argCount)
		args = append(args, isActive)
		argCount++
	}
	if status, ok := filters["verification_status"]; ok {
		query += fmt.Sprintf(" AND verification_status = $%d", argCount)
		args = append(args, status)
		argCount++
	}

	// Order by most recent first
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query binary versions: %w", err)
	}
	defer rows.Close()

	var versions []*BinaryVersion
	for rows.Next() {
		version := &BinaryVersion{}
		err := rows.Scan(
			&version.ID,
			&version.BinaryType,
			&version.CompressionType,
			&version.SourceURL,
			&version.FileName,
			&version.MD5Hash,
			&version.FileSize,
			&version.CreatedAt,
			&version.CreatedBy,
			&version.IsActive,
			&version.IsDefault,
			&version.LastVerifiedAt,
			&version.VerificationStatus,
			&version.SourceType,
			&version.Description,
			&version.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan binary version: %w", err)
		}
		versions = append(versions, version)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating binary versions: %w", err)
	}

	return versions, nil
}

// UpdateVersion implements Store.UpdateVersion
func (s *store) UpdateVersion(ctx context.Context, version *BinaryVersion) error {
	result, err := s.db.ExecContext(
		ctx,
		queries.UpdateBinaryVersion,
		version.BinaryType,
		version.CompressionType,
		version.SourceURL,
		version.FileName,
		version.MD5Hash,
		version.FileSize,
		version.IsActive,
		version.IsDefault,
		version.LastVerifiedAt,
		version.VerificationStatus,
		version.SourceType,
		version.Description,
		version.Version,
		version.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update binary version: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("binary version not found: %d", version.ID)
	}

	return nil
}

// DeleteVersion implements Store.DeleteVersion
func (s *store) DeleteVersion(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, queries.DeleteBinaryVersion, id)
	if err != nil {
		return fmt.Errorf("failed to delete binary version: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("binary version not found: %d", id)
	}

	return nil
}

// GetLatestActive implements Store.GetLatestActive
func (s *store) GetLatestActive(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error) {
	version := &BinaryVersion{}
	err := s.db.QueryRowContext(ctx, queries.GetLatestActiveBinaryVersion, binaryType).Scan(
		&version.ID,
		&version.BinaryType,
		&version.CompressionType,
		&version.SourceURL,
		&version.FileName,
		&version.MD5Hash,
		&version.FileSize,
		&version.CreatedAt,
		&version.CreatedBy,
		&version.IsActive,
		&version.IsDefault,
		&version.LastVerifiedAt,
		&version.VerificationStatus,
		&version.SourceType,
		&version.Description,
		&version.Version,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active verified version found for type: %s", binaryType)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest active version: %w", err)
	}

	return version, nil
}

// CreateAuditLog implements Store.CreateAuditLog
func (s *store) CreateAuditLog(ctx context.Context, log *BinaryAuditLog) error {
	// Convert details to JSONB
	detailsJSON, err := json.Marshal(log.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal audit log details: %w", err)
	}

	err = s.db.QueryRowContext(
		ctx,
		queries.CreateBinaryAuditLog,
		log.BinaryVersionID,
		log.Action,
		log.PerformedBy,
		detailsJSON,
	).Scan(&log.ID, &log.PerformedAt)

	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}

	return nil
}

// SetDefault implements Store.SetDefault
func (s *store) SetDefault(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, queries.SetBinaryDefault, id)
	if err != nil {
		return fmt.Errorf("failed to set binary as default: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("binary version not found or not active: %d", id)
	}

	return nil
}

// GetDefault implements Store.GetDefault
func (s *store) GetDefault(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error) {
	version := &BinaryVersion{}
	err := s.db.QueryRowContext(ctx, queries.GetDefaultBinaryVersion, binaryType).Scan(
		&version.ID,
		&version.BinaryType,
		&version.CompressionType,
		&version.SourceURL,
		&version.FileName,
		&version.MD5Hash,
		&version.FileSize,
		&version.CreatedAt,
		&version.CreatedBy,
		&version.IsActive,
		&version.IsDefault,
		&version.LastVerifiedAt,
		&version.VerificationStatus,
		&version.SourceType,
		&version.Description,
		&version.Version,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no default binary version found for type: %s", binaryType)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default binary version: %w", err)
	}

	return version, nil
}

// CountActiveBinaries implements Store.CountActiveBinaries
func (s *store) CountActiveBinaries(ctx context.Context, binaryType BinaryType) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, queries.CountActiveBinaries, binaryType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active binaries: %w", err)
	}
	return count, nil
}

// UpdateReferencesToDefault implements Store.UpdateReferencesToDefault
func (s *store) UpdateReferencesToDefault(ctx context.Context, oldID, newID int64) error {
	// Start a transaction to ensure atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update preset_jobs references
	_, err = tx.ExecContext(ctx, queries.UpdatePresetJobsBinaryReference, oldID, newID)
	if err != nil {
		return fmt.Errorf("failed to update preset_jobs references: %w", err)
	}

	// Update job_executions references (only pending/queued jobs)
	_, err = tx.ExecContext(ctx, queries.UpdateJobExecutionsBinaryReference, oldID, newID)
	if err != nil {
		return fmt.Errorf("failed to update job_executions references: %w", err)
	}

	// Reset agent binary overrides for agents using the deleted binary
	// The ON DELETE SET NULL constraint will handle binary_version_id
	// We just need to reset the binary_override flag to false
	resetAgentsQuery := `
		UPDATE agents
		SET binary_override = false
		WHERE binary_version_id = $1`
	result, err := tx.ExecContext(ctx, resetAgentsQuery, oldID)
	if err != nil {
		return fmt.Errorf("failed to reset agent binary overrides: %w", err)
	}

	// Log how many agents were affected
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		debug.Info("Reset binary override for %d agents using binary %d", rowsAffected, oldID)
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
