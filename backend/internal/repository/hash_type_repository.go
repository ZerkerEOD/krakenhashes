package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/lib/pq"
)

// HashTypeRepository handles database operations for hash types.
type HashTypeRepository struct {
	db *db.DB
}

// NewHashTypeRepository creates a new instance of HashTypeRepository.
func NewHashTypeRepository(database *db.DB) *HashTypeRepository {
	return &HashTypeRepository{db: database}
}

// Create inserts a new hash type record into the database.
// Note: Typically managed via migrations, but might be needed for admin UI.
func (r *HashTypeRepository) Create(ctx context.Context, hashType *models.HashType) error {
	query := `
		INSERT INTO hash_types (id, name, description, example, needs_processing, processing_logic, is_enabled, slow, is_salted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.ExecContext(ctx, query,
		hashType.ID,
		hashType.Name,
		hashType.Description,
		hashType.Example,
		hashType.NeedsProcessing,
		hashType.ProcessingLogic,
		hashType.IsEnabled,
		hashType.Slow,
		hashType.IsSalted,
	)
	if err != nil {
		// Check for primary key violation
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // 23505 is unique_violation
			return fmt.Errorf("hash type with ID %d already exists: %w", hashType.ID, ErrDuplicateRecord)
		}
		return fmt.Errorf("failed to create hash type %d: %w", hashType.ID, err)
	}
	return nil
}

// GetByID retrieves a hash type by its ID (hashcat mode number).
func (r *HashTypeRepository) GetByID(ctx context.Context, id int) (*models.HashType, error) {
	query := `
		SELECT id, name, description, example, needs_processing, processing_logic, is_enabled, slow, is_salted
		FROM hash_types
		WHERE id = $1
	`
	var hashType models.HashType
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&hashType.ID,
		&hashType.Name,
		&hashType.Description,
		&hashType.Example,
		&hashType.NeedsProcessing,
		&hashType.ProcessingLogic,
		&hashType.IsEnabled,
		&hashType.Slow,
		&hashType.IsSalted,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("hash type with ID %d not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get hash type by ID %d: %w", id, err)
	}
	return &hashType, nil
}

// List retrieves hash types from the database.
// It can optionally filter by the `is_enabled` status.
func (r *HashTypeRepository) List(ctx context.Context, enabledOnly bool) ([]models.HashType, error) {
	baseQuery := `
		SELECT id, name, description, example, needs_processing, processing_logic, is_enabled, slow, is_salted
		FROM hash_types
	`
	args := []interface{}{}
	query := baseQuery

	if enabledOnly {
		query += " WHERE is_enabled = $1"
		args = append(args, true)
	}

	query += " ORDER BY id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list hash types: %w", err)
	}
	defer rows.Close()

	var hashTypes []models.HashType
	for rows.Next() {
		var hashType models.HashType
		if err := rows.Scan(
			&hashType.ID,
			&hashType.Name,
			&hashType.Description,
			&hashType.Example,
			&hashType.NeedsProcessing,
			&hashType.ProcessingLogic,
			&hashType.IsEnabled,
			&hashType.Slow,
			&hashType.IsSalted,
		); err != nil {
			return nil, fmt.Errorf("failed to scan hash type row: %w", err)
		}
		hashTypes = append(hashTypes, hashType)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash type rows: %w", err)
	}

	return hashTypes, nil
}

// Update modifies an existing hash type record.
// Note: Typically managed via migrations.
func (r *HashTypeRepository) Update(ctx context.Context, hashType *models.HashType) error {
	query := `
		UPDATE hash_types
		SET name = $1, description = $2, example = $3, needs_processing = $4, processing_logic = $5, is_enabled = $6, slow = $7, is_salted = $8
		WHERE id = $9
	`
	// Note: Not updating created_at, only updated_at if it existed.
	// For simplicity, we assume migrations handle this or it's not critical for hash types.
	result, err := r.db.ExecContext(ctx, query,
		hashType.Name,
		hashType.Description,
		hashType.Example,
		hashType.NeedsProcessing,
		hashType.ProcessingLogic,
		hashType.IsEnabled,
		hashType.Slow,
		hashType.IsSalted,
		hashType.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update hash type %d: %w", hashType.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log this error
		// fmt.Printf("Warning: Could not get rows affected after updating hash type %d: %v\n", hashType.ID, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hash type with ID %d not found for update: %w", hashType.ID, ErrNotFound)
	}

	return nil
}

// Delete removes a hash type record from the database.
// Warning: This is potentially dangerous if hashes or hashlists reference this type.
// Consider disabling instead of deleting.
func (r *HashTypeRepository) Delete(ctx context.Context, id int) error {
	// Check for dependencies (hashes, hashlists) before deleting?
	// For now, rely on foreign key constraints or manual checks.
	query := `DELETE FROM hash_types WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		// Check for foreign key constraint violation
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23503" { // 23503 is foreign_key_violation
			return fmt.Errorf("cannot delete hash type %d: it is still referenced by hashes or hashlists", id)
		}
		return fmt.Errorf("failed to delete hash type %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log this error
		// fmt.Printf("Warning: Could not get rows affected after deleting hash type %d: %v\n", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hash type with ID %d not found for deletion: %w", id, ErrNotFound)
	}

	return nil
}
