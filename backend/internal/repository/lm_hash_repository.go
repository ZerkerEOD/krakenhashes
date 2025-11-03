package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type LMHashRepository struct {
	db *db.DB
}

func NewLMHashRepository(database *db.DB) *LMHashRepository {
	return &LMHashRepository{db: database}
}

// CreateLMMetadata creates a metadata entry for a new LM hash
func (r *LMHashRepository) CreateLMMetadata(ctx context.Context, hashID uuid.UUID) error {
	query := `
		INSERT INTO lm_hash_metadata (hash_id)
		VALUES ($1)
		ON CONFLICT (hash_id) DO NOTHING
	`
	_, err := r.db.ExecContext(ctx, query, hashID)
	return err
}

// GetLMMetadata retrieves LM metadata for a single hash
func (r *LMHashRepository) GetLMMetadata(ctx context.Context, hashID uuid.UUID) (*models.LMHashMetadata, error) {
	query := `
		SELECT hash_id, first_half_cracked, second_half_cracked,
		       first_half_password, second_half_password, created_at, updated_at
		FROM lm_hash_metadata
		WHERE hash_id = $1
	`

	var metadata models.LMHashMetadata
	err := r.db.QueryRowContext(ctx, query, hashID).Scan(
		&metadata.HashID, &metadata.FirstHalfCracked, &metadata.SecondHalfCracked,
		&metadata.FirstHalfPassword, &metadata.SecondHalfPassword,
		&metadata.CreatedAt, &metadata.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No metadata = hash not yet processed
	}
	return &metadata, err
}

// GetLMMetadataByHashes bulk retrieves LM metadata for multiple hashes
func (r *LMHashRepository) GetLMMetadataByHashes(ctx context.Context, hashIDs []uuid.UUID) (map[uuid.UUID]*models.LMHashMetadata, error) {
	if len(hashIDs) == 0 {
		return make(map[uuid.UUID]*models.LMHashMetadata), nil
	}

	query := `
		SELECT hash_id, first_half_cracked, second_half_cracked,
		       first_half_password, second_half_password, created_at, updated_at
		FROM lm_hash_metadata
		WHERE hash_id = ANY($1)
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uuid.UUID]*models.LMHashMetadata)
	for rows.Next() {
		var metadata models.LMHashMetadata
		err := rows.Scan(
			&metadata.HashID, &metadata.FirstHalfCracked, &metadata.SecondHalfCracked,
			&metadata.FirstHalfPassword, &metadata.SecondHalfPassword,
			&metadata.CreatedAt, &metadata.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		result[metadata.HashID] = &metadata
	}

	return result, nil
}

// UpdateLMHalfCrack updates one half of an LM hash (first or second)
// If metadata doesn't exist yet, it will be created
func (r *LMHashRepository) UpdateLMHalfCrack(ctx context.Context, tx *sql.Tx, hashID uuid.UUID, halfPosition string, password string) error {
	var query string

	if halfPosition == "first" {
		query = `
			INSERT INTO lm_hash_metadata (hash_id, first_half_cracked, first_half_password, updated_at)
			VALUES ($1, TRUE, $2, $3)
			ON CONFLICT (hash_id) DO UPDATE
			SET first_half_cracked = TRUE,
			    first_half_password = $2,
			    updated_at = $3
		`
	} else {
		query = `
			INSERT INTO lm_hash_metadata (hash_id, second_half_cracked, second_half_password, updated_at)
			VALUES ($1, TRUE, $2, $3)
			ON CONFLICT (hash_id) DO UPDATE
			SET second_half_cracked = TRUE,
			    second_half_password = $2,
			    updated_at = $3
		`
	}

	_, err := tx.ExecContext(ctx, query, hashID, password, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update LM %s half: %w", halfPosition, err)
	}

	return nil
}

// CheckAndFinalizeLMCrack checks if both halves are cracked, and if so, returns the full password
// Returns: wasFinalized bool, fullPassword string, error
func (r *LMHashRepository) CheckAndFinalizeLMCrack(ctx context.Context, tx *sql.Tx, hashID uuid.UUID) (bool, string, error) {
	// Check if both halves are now cracked
	var firstHalfPwd, secondHalfPwd sql.NullString
	var bothCracked bool

	query := `
		SELECT
			(first_half_cracked AND second_half_cracked) AS both_cracked,
			first_half_password,
			second_half_password
		FROM lm_hash_metadata
		WHERE hash_id = $1
	`

	err := tx.QueryRowContext(ctx, query, hashID).Scan(&bothCracked, &firstHalfPwd, &secondHalfPwd)
	if err == sql.ErrNoRows {
		// No metadata yet - this shouldn't happen, but handle gracefully
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check LM crack status: %w", err)
	}

	if !bothCracked {
		return false, "", nil // Not both cracked yet
	}

	// Both halves cracked - combine password
	fullPassword := ""
	if firstHalfPwd.Valid {
		fullPassword += firstHalfPwd.String
	}
	if secondHalfPwd.Valid {
		fullPassword += secondHalfPwd.String
	}

	return true, fullPassword, nil
}
