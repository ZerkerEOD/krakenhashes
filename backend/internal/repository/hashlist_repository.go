package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// HashListRepository handles database operations for hashlists.
type HashListRepository struct {
	db *db.DB
}

// NewHashListRepository creates a new instance of HashListRepository.
func NewHashListRepository(database *db.DB) *HashListRepository {
	return &HashListRepository{db: database}
}

// Create inserts a new hashlist record into the database.
// It updates the hashlist.ID field with the newly generated serial ID.
func (r *HashListRepository) Create(ctx context.Context, hashlist *models.HashList) error {
	query := `
		INSERT INTO hashlists (name, user_id, client_id, hash_type_id, status, exclude_from_potfile, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	var clientIDArg interface{} // Handle NULL client_id
	if hashlist.ClientID != uuid.Nil {
		clientIDArg = hashlist.ClientID
	} else {
		clientIDArg = nil
	}

	row := r.db.QueryRowContext(ctx, query,
		hashlist.Name,
		hashlist.UserID,
		clientIDArg,
		hashlist.HashTypeID,
		hashlist.Status,
		hashlist.ExcludeFromPotfile,
		hashlist.CreatedAt,
		hashlist.UpdatedAt,
	)

	err := row.Scan(&hashlist.ID) // Scan the returned ID into the struct
	if err != nil {
		return fmt.Errorf("failed to create hashlist and scan ID: %w", err)
	}
	return nil
}

// UpdateStatus updates the status and optionally the error message of a hashlist.
func (r *HashListRepository) UpdateStatus(ctx context.Context, id int64, status string, errorMessage string) error {
	query := `
		UPDATE hashlists
		SET status = $1, error_message = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.ExecContext(ctx, query, status, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist status for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for status update: %w", id, ErrNotFound)
	}
	return nil
}

// UpdateStatsAndStatus updates the hash counts, status, and error message of a hashlist after processing.
func (r *HashListRepository) UpdateStatsAndStatus(ctx context.Context, id int64, totalHashes, crackedHashes int, status, errorMessage string) error {
	query := `
		UPDATE hashlists
		SET total_hashes = $1, cracked_hashes = $2, status = $3, error_message = $4, updated_at = $5
		WHERE id = $6
	`
	result, err := r.db.ExecContext(ctx, query, totalHashes, crackedHashes, status, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist stats and status for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for stats/status update: %w", id, ErrNotFound)
	}
	return nil
}

// UpdateStatsStatusAndAssociationFields updates hash counts, status, error message, original file path, and work factor flag.
// Used after hashlist processing to store all final state including association attack support fields.
func (r *HashListRepository) UpdateStatsStatusAndAssociationFields(ctx context.Context, id int64, totalHashes, crackedHashes int, status, errorMessage, originalFilePath string, hasMixedWorkFactors bool) error {
	query := `
		UPDATE hashlists
		SET total_hashes = $1, cracked_hashes = $2, status = $3, error_message = $4,
		    original_file_path = $5, has_mixed_work_factors = $6, updated_at = $7
		WHERE id = $8
	`
	result, err := r.db.ExecContext(ctx, query, totalHashes, crackedHashes, status, errorMessage, originalFilePath, hasMixedWorkFactors, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist stats, status and association fields for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after updating hashlist %d: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for stats/status/association update: %w", id, ErrNotFound)
	}
	return nil
}

// UpdateClientID updates the client_id for a hashlist.
func (r *HashListRepository) UpdateClientID(ctx context.Context, id int64, clientID uuid.UUID) error {
	query := `
		UPDATE hashlists
		SET client_id = $1, updated_at = $2
		WHERE id = $3
	`
	var clientIDArg interface{} // Handle NULL client_id
	if clientID != uuid.Nil {
		clientIDArg = clientID
	} else {
		clientIDArg = nil
	}

	result, err := r.db.ExecContext(ctx, query, clientIDArg, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist client for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
		debug.Warning("Could not get rows affected after updating hashlist %d client: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for client update: %w", id, ErrNotFound)
	}
	return nil
}

// GetByID retrieves a hashlist by its ID.
func (r *HashListRepository) GetByID(ctx context.Context, id int64) (*models.HashList, error) {
	query := `
		SELECT
			h.id, h.name, h.user_id, h.client_id, h.hash_type_id,
			h.total_hashes, h.cracked_hashes, h.status, h.error_message,
			h.exclude_from_potfile, h.original_file_path, h.has_mixed_work_factors,
			h.created_at, h.updated_at,
			c.name AS client_name
		FROM hashlists h
		LEFT JOIN clients c ON h.client_id = c.id
		WHERE h.id = $1
	`
	var hashlist models.HashList
	var clientID sql.Null[uuid.UUID]    // Handle nullable client_id
	var clientName sql.NullString       // Handle nullable client_name
	var originalFilePath sql.NullString // Handle nullable original_file_path
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&hashlist.ID,
		&hashlist.Name,
		&hashlist.UserID,
		&clientID,
		&hashlist.HashTypeID,
		&hashlist.TotalHashes,
		&hashlist.CrackedHashes,
		&hashlist.Status,
		&hashlist.ErrorMessage,
		&hashlist.ExcludeFromPotfile,
		&originalFilePath,
		&hashlist.HasMixedWorkFactors,
		&hashlist.CreatedAt,
		&hashlist.UpdatedAt,
		&clientName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("hashlist with ID %d not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get hashlist by ID %d: %w", id, err)
	}
	if clientID.Valid {
		hashlist.ClientID = clientID.V
	}
	if clientName.Valid {
		hashlist.ClientName = &clientName.String
	}
	if originalFilePath.Valid {
		hashlist.OriginalFilePath = &originalFilePath.String
	}
	return &hashlist, nil
}

// List retrieves hashlists, optionally filtered and paginated.
type ListHashlistsParams struct {
	UserID   *uuid.UUID
	ClientID *uuid.UUID
	Status   *string
	NameLike *string // For searching by name pattern
	Limit    int
	Offset   int
}

func (r *HashListRepository) List(ctx context.Context, params ListHashlistsParams) ([]models.HashList, int, error) {
	debug.Info("[HashlistRepo.List] Called with params: %+v", params)
	// Select hashlist columns prefixed with 'h.' and client name prefixed with 'c.'
	baseQuery := `
		SELECT
			h.id, h.name, h.user_id, h.client_id, h.hash_type_id,
			h.total_hashes, h.cracked_hashes, h.status,
			h.error_message, h.exclude_from_potfile, h.original_file_path, h.has_mixed_work_factors,
			h.created_at, h.updated_at,
			c.name AS client_name
		FROM hashlists h
		LEFT JOIN clients c ON h.client_id = c.id
	`
	// Count needs to consider the same join and filters
	countQuery := `SELECT COUNT(h.id) FROM hashlists h LEFT JOIN clients c ON h.client_id = c.id`

	conditions := []string{}
	args := []interface{}{}
	argID := 1

	if params.UserID != nil {
		// Use h.user_id
		conditions = append(conditions, fmt.Sprintf("h.user_id = $%d", argID))
		args = append(args, *params.UserID)
		argID++
	}
	if params.ClientID != nil {
		// Use h.client_id
		conditions = append(conditions, fmt.Sprintf("h.client_id = $%d", argID))
		args = append(args, *params.ClientID)
		argID++
	}
	if params.Status != nil {
		// Use h.status
		conditions = append(conditions, fmt.Sprintf("h.status = $%d", argID))
		args = append(args, *params.Status)
		argID++
	}
	if params.NameLike != nil {
		// Use h.name
		conditions = append(conditions, fmt.Sprintf("h.name ILIKE $%d", argID))
		args = append(args, "%"+*params.NameLike+"%") // Add wildcards for ILIKE
		argID++
	}
	// TODO: Add filtering by client_name if needed in the future?
	// if params.ClientNameLike != nil { ... }

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + joinConditions(conditions, " AND ")
	}

	// Log the count query and args
	debug.Info("[HashlistRepo.List] Executing Count Query: %s | Args: %v", countQuery+whereClause, args)

	// Get total count matching filters
	var totalCount int
	err := r.db.QueryRowContext(ctx, countQuery+whereClause, args...).Scan(&totalCount)
	if err != nil {
		debug.Error("[HashlistRepo.List] Error executing count query: %v", err)
		return nil, 0, fmt.Errorf("failed to count hashlists: %w", err)
	}
	debug.Info("[HashlistRepo.List] Total Count Found: %d", totalCount)

	if totalCount == 0 {
		// No need to run the main query if count is 0
		return []models.HashList{}, 0, nil
	}

	// Construct final query with ordering and pagination
	finalQuery := baseQuery + whereClause
	// Order by h.created_at
	finalQuery += " ORDER BY h.created_at DESC"

	if params.Limit > 0 {
		finalQuery += fmt.Sprintf(" LIMIT $%d", argID)
		args = append(args, params.Limit)
		argID++
	}
	if params.Offset >= 0 { // Allow offset 0
		finalQuery += fmt.Sprintf(" OFFSET $%d", argID)
		args = append(args, params.Offset)
		argID++
	}

	// Log the final query and args
	debug.Info("[HashlistRepo.List] Executing List Query: %s | Args: %v", finalQuery, args)

	rows, err := r.db.QueryContext(ctx, finalQuery, args...)
	if err != nil {
		debug.Error("[HashlistRepo.List] Error executing list query: %v", err)
		return nil, 0, fmt.Errorf("failed to list hashlists with pagination/filters: %w", err)
	}
	defer rows.Close()

	var hashlists []models.HashList
	for rows.Next() {
		var hashlist models.HashList
		var clientID sql.Null[uuid.UUID]    // Use sql.Null for nullable UUID
		var clientName sql.NullString       // Use sql.NullString for nullable client name from LEFT JOIN
		var originalFilePath sql.NullString // Handle nullable original_file_path

		if err := rows.Scan(
			&hashlist.ID,
			&hashlist.Name,
			&hashlist.UserID,
			&clientID, // Scan into nullable UUID
			&hashlist.HashTypeID,
			&hashlist.TotalHashes,
			&hashlist.CrackedHashes,
			&hashlist.Status,
			&hashlist.ErrorMessage,
			&hashlist.ExcludeFromPotfile,
			&originalFilePath,
			&hashlist.HasMixedWorkFactors,
			&hashlist.CreatedAt,
			&hashlist.UpdatedAt,
			&clientName, // Scan into nullable string
		); err != nil {
			debug.Error("[HashlistRepo.List] Error scanning row: %v", err)
			return nil, 0, fmt.Errorf("failed to scan hashlist row: %w", err)
		}

		// Assign ClientID only if it's valid (not NULL in DB)
		if clientID.Valid {
			hashlist.ClientID = clientID.V
		}

		// Assign ClientName only if it's valid (client existed and name is not NULL)
		if clientName.Valid {
			hashlist.ClientName = &clientName.String
		}

		// Assign OriginalFilePath only if it's valid
		if originalFilePath.Valid {
			hashlist.OriginalFilePath = &originalFilePath.String
		}

		hashlists = append(hashlists, hashlist)
	}
	if err = rows.Err(); err != nil {
		debug.Error("[HashlistRepo.List] Error iterating rows: %v", err)
		return nil, 0, fmt.Errorf("error iterating hashlist rows: %w", err)
	}

	debug.Info("[HashlistRepo.List] Successfully retrieved %d hashlists", len(hashlists))
	if len(hashlists) > 0 {
		debug.Debug("[HashlistRepo.List] First hashlist: %+v", hashlists[0])
	}

	return hashlists, totalCount, nil
}

// GetByClientID retrieves all hashlists associated with a specific client ID.
func (r *HashListRepository) GetByClientID(ctx context.Context, clientID uuid.UUID) ([]models.HashList, error) {
	query := `
		SELECT id, name, user_id, client_id, hash_type_id, total_hashes, cracked_hashes, status, error_message, created_at, updated_at
		FROM hashlists
		WHERE client_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to query hashlists by client ID %s: %w", clientID, err)
	}
	defer rows.Close()

	var hashlists []models.HashList
	for rows.Next() {
		var hashlist models.HashList
		var dbClientID sql.Null[uuid.UUID] // Scan into nullable type first
		if err := rows.Scan(
			&hashlist.ID,
			&hashlist.Name,
			&hashlist.UserID,
			&dbClientID, // Scan into nullable
			&hashlist.HashTypeID,
			&hashlist.TotalHashes,
			&hashlist.CrackedHashes,
			&hashlist.Status,
			&hashlist.ErrorMessage,
			&hashlist.CreatedAt,
			&hashlist.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan hashlist row for client ID %s: %w", clientID, err)
		}
		if dbClientID.Valid { // Assign only if valid
			hashlist.ClientID = dbClientID.V
		}
		hashlists = append(hashlists, hashlist)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hashlist rows for client ID %s: %w", clientID, err)
	}

	return hashlists, nil
}

// Delete removes a hashlist record and performs cleanup of orphaned hashes.
// It finds associated hashes, deletes the hashlist (cascading to hashlist_hashes),
// and then deletes any hashes that are no longer referenced by any hashlist.
func (r *HashListRepository) Delete(ctx context.Context, id int64) error {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for hashlist deletion %d: %w", id, err)
	}
	defer tx.Rollback() // Rollback on error or panic

	// 1. Find all hash IDs associated with this hashlist *before* deleting it.
	findHashesQuery := `SELECT hash_id FROM hashlist_hashes WHERE hashlist_id = $1`
	rows, err := tx.QueryContext(ctx, findHashesQuery, id)
	if err != nil {
		debug.Error("[Delete:%d] Failed to query associated hash IDs: %v", id, err)
		return fmt.Errorf("failed to query associated hash IDs for hashlist %d: %w", id, err)
	}
	var associatedHashIDs []uuid.UUID
	for rows.Next() {
		var hashID uuid.UUID
		if err := rows.Scan(&hashID); err != nil {
			rows.Close() // Close rows before returning
			debug.Error("[Delete:%d] Failed to scan associated hash ID: %v", id, err)
			return fmt.Errorf("failed to scan associated hash ID for hashlist %d: %w", id, err)
		}
		associatedHashIDs = append(associatedHashIDs, hashID)
	}
	if err = rows.Err(); err != nil {
		debug.Error("[Delete:%d] Error iterating associated hash IDs: %v", id, err)
		return fmt.Errorf("error iterating associated hash IDs for hashlist %d: %w", id, err)
	}
	rows.Close()
	debug.Info("[Delete:%d] Found %d associated hash IDs initially.", id, len(associatedHashIDs))

	// 2. Delete the hashlist itself (this cascades to hashlist_hashes)
	deleteHashlistQuery := `DELETE FROM hashlists WHERE id = $1`
	result, err := tx.ExecContext(ctx, deleteHashlistQuery, id)
	if err != nil {
		debug.Error("[Delete:%d] Failed to delete hashlist record: %v", id, err)
		return fmt.Errorf("failed to delete hashlist %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning but continue, primary deletion might have worked.
		debug.Warning("[Delete:%d] Could not get rows affected after deleting hashlist: %v", id, err)
	} else if rowsAffected == 0 {
		// Hashlist didn't exist, nothing to delete. Commit the (empty) transaction.
		_ = tx.Commit()
		return fmt.Errorf("hashlist %d not found for deletion: %w", id, ErrNotFound)
	}
	debug.Info("[Delete:%d] Deleted hashlist record.", id)

	// 3. Check and delete orphaned hashes in batches
	if len(associatedHashIDs) > 0 {
		debug.Info("[Delete:%d] Checking for orphaned hashes among %d candidates...", id, len(associatedHashIDs))

		// Process in batches to avoid shared memory issues with large arrays
		checkBatchSize := 50000 // Check 50k hashes at a time for orphan status
		deleteBatchSize := 10000 // Delete in smaller batches
		totalOrphansDeleted := 0

		for batchStart := 0; batchStart < len(associatedHashIDs); batchStart += checkBatchSize {
			batchEnd := batchStart + checkBatchSize
			if batchEnd > len(associatedHashIDs) {
				batchEnd = len(associatedHashIDs)
			}

			checkBatch := associatedHashIDs[batchStart:batchEnd]

			// Find orphaned hashes in this batch
			// An orphan is a hash that was in this hashlist but is NOT in any other hashlist
			// After the CASCADE delete, orphaned hashes will have NO entries in hashlist_hashes
			orphanQuery := `
				SELECT id FROM hashes
				WHERE id = ANY($1)
				AND NOT EXISTS (
					SELECT 1 FROM hashlist_hashes WHERE hash_id = hashes.id
				)
			`

			rows, err := tx.QueryContext(ctx, orphanQuery, pq.Array(checkBatch))
			if err != nil {
				debug.Error("[Delete:%d] Failed to query orphaned hashes in batch %d-%d: %v", id, batchStart, batchEnd, err)
				return fmt.Errorf("failed to query orphaned hashes for hashlist %d: %w", id, err)
			}

			var orphanedHashIDs []uuid.UUID
			for rows.Next() {
				var hashID uuid.UUID
				if err := rows.Scan(&hashID); err != nil {
					rows.Close()
					debug.Error("[Delete:%d] Failed to scan orphaned hash ID: %v", id, err)
					return fmt.Errorf("failed to scan orphaned hash ID: %w", err)
				}
				orphanedHashIDs = append(orphanedHashIDs, hashID)
			}
			rows.Close()

			if len(orphanedHashIDs) > 0 {
				debug.Info("[Delete:%d] Found %d orphaned hashes in batch %d-%d", id, len(orphanedHashIDs), batchStart, batchEnd)

				// Delete orphaned hashes in smaller batches
				for i := 0; i < len(orphanedHashIDs); i += deleteBatchSize {
					end := i + deleteBatchSize
					if end > len(orphanedHashIDs) {
						end = len(orphanedHashIDs)
					}

					deleteBatch := orphanedHashIDs[i:end]
					deleteOrphanQuery := `DELETE FROM hashes WHERE id = ANY($1)`

					result, err := tx.ExecContext(ctx, deleteOrphanQuery, pq.Array(deleteBatch))
					if err != nil {
						debug.Error("[Delete:%d] Failed to delete orphaned hash batch: %v", id, err)
						return fmt.Errorf("failed to delete orphaned hash batch: %w", err)
					}

					deleted, _ := result.RowsAffected()
					totalOrphansDeleted += int(deleted)
				}
			}

			// Progress logging every 100k candidates checked
			if batchStart > 0 && batchStart%100000 == 0 {
				debug.Info("[Delete:%d] Progress: checked %d/%d candidates, deleted %d orphans so far...",
					id, batchEnd, len(associatedHashIDs), totalOrphansDeleted)
			}
		}

		debug.Info("[Delete:%d] Deleted %d orphaned hashes total.", id, totalOrphansDeleted)
	}

	// 4. Commit the transaction
	if err = tx.Commit(); err != nil {
		debug.Error("[Delete:%d] Failed to commit transaction: %v", id, err)
		return fmt.Errorf("failed to commit hashlist deletion transaction for %d: %w", id, err)
	}

	debug.Info("[Delete:%d] Hashlist deletion and orphan cleanup completed successfully.", id)
	return nil
}

// DeletionProgressCallback is called during deletion to report progress with phase information.
// Parameters:
//   - status: current phase status ("deleting_hashes", "clearing_references", "cleaning_orphans", "finalizing")
//   - phase: human-readable phase description
//   - checked: hashes checked/deleted so far in current phase
//   - total: total candidates for current phase
//   - deleted: orphans deleted so far
//   - refsCleared: cracked_by_task_id references cleared so far
//   - refsTotal: total references to clear
type DeletionProgressCallback func(status, phase string, checked, total, deleted, refsCleared, refsTotal int64)

// DeleteWithProgress removes a hashlist and performs orphan cleanup while reporting progress via callback.
// This allows long-running deletions to report their progress for UI updates.
func (r *HashListRepository) DeleteWithProgress(ctx context.Context, id int64, onProgress DeletionProgressCallback) error {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for hashlist deletion %d: %w", id, err)
	}
	defer tx.Rollback() // Rollback on error or panic

	// 1. Find all hash IDs associated with this hashlist *before* deleting it.
	findHashesQuery := `SELECT hash_id FROM hashlist_hashes WHERE hashlist_id = $1`
	rows, err := tx.QueryContext(ctx, findHashesQuery, id)
	if err != nil {
		debug.Error("[DeleteWithProgress:%d] Failed to query associated hash IDs: %v", id, err)
		return fmt.Errorf("failed to query associated hash IDs for hashlist %d: %w", id, err)
	}
	var associatedHashIDs []uuid.UUID
	for rows.Next() {
		var hashID uuid.UUID
		if err := rows.Scan(&hashID); err != nil {
			rows.Close()
			debug.Error("[DeleteWithProgress:%d] Failed to scan associated hash ID: %v", id, err)
			return fmt.Errorf("failed to scan associated hash ID for hashlist %d: %w", id, err)
		}
		associatedHashIDs = append(associatedHashIDs, hashID)
	}
	if err = rows.Err(); err != nil {
		debug.Error("[DeleteWithProgress:%d] Error iterating associated hash IDs: %v", id, err)
		return fmt.Errorf("error iterating associated hash IDs for hashlist %d: %w", id, err)
	}
	rows.Close()
	totalCandidates := int64(len(associatedHashIDs))
	debug.Info("[DeleteWithProgress:%d] Found %d associated hash IDs initially.", id, totalCandidates)

	// Report initial progress (0 checked)
	if onProgress != nil {
		onProgress("deleting_hashes", "Removing hashes", 0, totalCandidates, 0, 0, 0)
	}

	// 2. Delete the hashlist itself (this cascades to hashlist_hashes)
	deleteHashlistQuery := `DELETE FROM hashlists WHERE id = $1`
	result, err := tx.ExecContext(ctx, deleteHashlistQuery, id)
	if err != nil {
		debug.Error("[DeleteWithProgress:%d] Failed to delete hashlist record: %v", id, err)
		return fmt.Errorf("failed to delete hashlist %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("[DeleteWithProgress:%d] Could not get rows affected after deleting hashlist: %v", id, err)
	} else if rowsAffected == 0 {
		_ = tx.Commit()
		return fmt.Errorf("hashlist %d not found for deletion: %w", id, ErrNotFound)
	}
	debug.Info("[DeleteWithProgress:%d] Deleted hashlist record.", id)

	// 3. Check and delete orphaned hashes in batches
	var totalOrphansDeleted int64 = 0
	if len(associatedHashIDs) > 0 {
		debug.Info("[DeleteWithProgress:%d] Checking for orphaned hashes among %d candidates...", id, len(associatedHashIDs))

		checkBatchSize := 50000
		deleteBatchSize := 10000

		for batchStart := 0; batchStart < len(associatedHashIDs); batchStart += checkBatchSize {
			// Check context for cancellation
			select {
			case <-ctx.Done():
				debug.Warning("[DeleteWithProgress:%d] Context cancelled during orphan cleanup", id)
				return ctx.Err()
			default:
			}

			batchEnd := batchStart + checkBatchSize
			if batchEnd > len(associatedHashIDs) {
				batchEnd = len(associatedHashIDs)
			}

			checkBatch := associatedHashIDs[batchStart:batchEnd]

			orphanQuery := `
				SELECT id FROM hashes
				WHERE id = ANY($1)
				AND NOT EXISTS (
					SELECT 1 FROM hashlist_hashes WHERE hash_id = hashes.id
				)
			`

			rows, err := tx.QueryContext(ctx, orphanQuery, pq.Array(checkBatch))
			if err != nil {
				debug.Error("[DeleteWithProgress:%d] Failed to query orphaned hashes in batch %d-%d: %v", id, batchStart, batchEnd, err)
				return fmt.Errorf("failed to query orphaned hashes for hashlist %d: %w", id, err)
			}

			var orphanedHashIDs []uuid.UUID
			for rows.Next() {
				var hashID uuid.UUID
				if err := rows.Scan(&hashID); err != nil {
					rows.Close()
					debug.Error("[DeleteWithProgress:%d] Failed to scan orphaned hash ID: %v", id, err)
					return fmt.Errorf("failed to scan orphaned hash ID: %w", err)
				}
				orphanedHashIDs = append(orphanedHashIDs, hashID)
			}
			rows.Close()

			if len(orphanedHashIDs) > 0 {
				debug.Info("[DeleteWithProgress:%d] Found %d orphaned hashes in batch %d-%d", id, len(orphanedHashIDs), batchStart, batchEnd)

				for i := 0; i < len(orphanedHashIDs); i += deleteBatchSize {
					end := i + deleteBatchSize
					if end > len(orphanedHashIDs) {
						end = len(orphanedHashIDs)
					}

					deleteBatch := orphanedHashIDs[i:end]
					deleteOrphanQuery := `DELETE FROM hashes WHERE id = ANY($1)`

					result, err := tx.ExecContext(ctx, deleteOrphanQuery, pq.Array(deleteBatch))
					if err != nil {
						debug.Error("[DeleteWithProgress:%d] Failed to delete orphaned hash batch: %v", id, err)
						return fmt.Errorf("failed to delete orphaned hash batch: %w", err)
					}

					deleted, _ := result.RowsAffected()
					totalOrphansDeleted += deleted
				}
			}

			// Report progress after each check batch
			if onProgress != nil {
				onProgress("cleaning_orphans", "Cleaning orphan hashes", int64(batchEnd), totalCandidates, totalOrphansDeleted, 0, 0)
			}

			// Progress logging every 100k candidates checked
			if batchStart > 0 && batchStart%100000 == 0 {
				debug.Info("[DeleteWithProgress:%d] Progress: checked %d/%d candidates, deleted %d orphans so far...",
					id, batchEnd, len(associatedHashIDs), totalOrphansDeleted)
			}
		}

		debug.Info("[DeleteWithProgress:%d] Deleted %d orphaned hashes total.", id, totalOrphansDeleted)
	}

	// 4. Commit the transaction
	if err = tx.Commit(); err != nil {
		debug.Error("[DeleteWithProgress:%d] Failed to commit transaction: %v", id, err)
		return fmt.Errorf("failed to commit hashlist deletion transaction for %d: %w", id, err)
	}

	// Final progress report
	if onProgress != nil {
		onProgress("completed", "Deletion complete", totalCandidates, totalCandidates, totalOrphansDeleted, 0, 0)
	}

	debug.Info("[DeleteWithProgress:%d] Hashlist deletion and orphan cleanup completed successfully.", id)
	return nil
}

// DeleteWithProgressStreaming removes a hashlist using batch deletion for progress reporting.
// This method deletes hashlist_hashes in batches FIRST (with progress updates), then deletes
// the hashlist record (CASCADE handles jobs), and finally cleans up orphan hashes.
// For large hashlists (24M+ records), this provides real-time progress without blocking.
func (r *HashListRepository) DeleteWithProgressStreaming(ctx context.Context, id int64, onProgress DeletionProgressCallback) error {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for hashlist deletion %d: %w", id, err)
	}
	defer tx.Rollback() // Rollback on error or panic

	// Step 1: Get count FIRST (very fast with index) - report progress immediately
	var totalCandidates int64
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM hashlist_hashes WHERE hashlist_id = $1`, id).Scan(&totalCandidates)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to count hashes: %v", id, err)
		return fmt.Errorf("failed to count hashes for hashlist %d deletion: %w", id, err)
	}
	debug.Info("[DeleteWithProgressStreaming:%d] Total hash candidates: %d", id, totalCandidates)

	// Report initial progress IMMEDIATELY
	if onProgress != nil {
		onProgress("deleting_hashes", "Removing hashes", 0, totalCandidates, 0, 0, 0)
	}

	// Step 2: Create temp table and capture hash_ids for orphan detection later
	// We need to capture IDs BEFORE deleting hashlist_hashes
	_, err = tx.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS deletion_candidates (
			hash_id UUID PRIMARY KEY
		) ON COMMIT DROP
	`)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to create temp table: %v", id, err)
		return fmt.Errorf("failed to create temp table for hashlist %d deletion: %w", id, err)
	}

	// Truncate in case of reuse within same session
	_, err = tx.ExecContext(ctx, `TRUNCATE deletion_candidates`)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to truncate temp table: %v", id, err)
		return fmt.Errorf("failed to truncate temp table for hashlist %d deletion: %w", id, err)
	}

	// Populate temp table (server-side, no Go memory used)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO deletion_candidates (hash_id)
		SELECT hash_id FROM hashlist_hashes WHERE hashlist_id = $1
	`, id)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to populate temp table: %v", id, err)
		return fmt.Errorf("failed to populate temp table for hashlist %d deletion: %w", id, err)
	}
	debug.Info("[DeleteWithProgressStreaming:%d] Temp table populated with hash_ids for orphan detection", id)

	// Handle empty hashlist case
	if totalCandidates == 0 {
		deleteHashlistQuery := `DELETE FROM hashlists WHERE id = $1`
		result, err := tx.ExecContext(ctx, deleteHashlistQuery, id)
		if err != nil {
			debug.Error("[DeleteWithProgressStreaming:%d] Failed to delete hashlist record: %v", id, err)
			return fmt.Errorf("failed to delete hashlist %d: %w", id, err)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			_ = tx.Commit()
			return fmt.Errorf("hashlist %d not found for deletion: %w", id, ErrNotFound)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit hashlist deletion transaction for %d: %w", id, err)
		}
		debug.Info("[DeleteWithProgressStreaming:%d] Hashlist deletion completed (no hashes to cleanup).", id)
		return nil
	}

	// Step 3: BATCH DELETE from hashlist_hashes WITH PROGRESS
	// Key change: Delete junction table entries BEFORE deleting hashlist to enable progress reporting
	// This avoids the CASCADE blocking issue where PostgreSQL deletes 24M rows in one operation
	const batchSize = 50000
	var deletedFromJunction int64 = 0

	debug.Info("[DeleteWithProgressStreaming:%d] Starting batch deletion of hashlist_hashes (batch size: %d)", id, batchSize)

	for {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			debug.Warning("[DeleteWithProgressStreaming:%d] Context cancelled during hashlist_hashes deletion", id)
			return ctx.Err()
		default:
		}

		// Delete batch using ctid for efficient batch deletion without loading IDs
		result, err := tx.ExecContext(ctx, `
			DELETE FROM hashlist_hashes
			WHERE ctid IN (
				SELECT ctid FROM hashlist_hashes
				WHERE hashlist_id = $1
				LIMIT $2
			)
		`, id, batchSize)
		if err != nil {
			debug.Error("[DeleteWithProgressStreaming:%d] Failed to delete hashlist_hashes batch: %v", id, err)
			return fmt.Errorf("failed to delete hashlist_hashes batch for hashlist %d: %w", id, err)
		}

		rowsDeleted, _ := result.RowsAffected()
		if rowsDeleted == 0 {
			break // No more rows to delete
		}
		deletedFromJunction += rowsDeleted

		// Report progress after each batch (orphans deleted = 0 during this phase)
		if onProgress != nil {
			onProgress("deleting_hashes", "Removing hashes", deletedFromJunction, totalCandidates, 0, 0, 0)
		}

		// Progress logging every 500k rows
		if deletedFromJunction > 0 && deletedFromJunction%500000 < int64(batchSize) {
			debug.Info("[DeleteWithProgressStreaming:%d] Deleting hashlist_hashes: %d/%d (%.1f%%)",
				id, deletedFromJunction, totalCandidates, float64(deletedFromJunction)/float64(totalCandidates)*100)
		}
	}

	debug.Info("[DeleteWithProgressStreaming:%d] Deleted %d rows from hashlist_hashes", id, deletedFromJunction)

	// Step 3.5: Pre-emptively SET NULL on cracked_by_task_id to avoid CASCADE bottleneck
	// Get all task_ids from jobs on this hashlist
	taskIDsQuery := `
		SELECT jt.id FROM job_tasks jt
		JOIN job_executions je ON jt.job_execution_id = je.id
		WHERE je.hashlist_id = $1
	`
	taskRows, err := tx.QueryContext(ctx, taskIDsQuery, id)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to get task IDs: %v", id, err)
		return fmt.Errorf("failed to get task IDs for hashlist %d: %w", id, err)
	}
	var taskIDs []int64
	for taskRows.Next() {
		var taskID int64
		if err := taskRows.Scan(&taskID); err != nil {
			taskRows.Close()
			debug.Error("[DeleteWithProgressStreaming:%d] Failed to scan task ID: %v", id, err)
			return fmt.Errorf("failed to scan task ID: %w", err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err = taskRows.Err(); err != nil {
		taskRows.Close()
		debug.Error("[DeleteWithProgressStreaming:%d] Error iterating task rows: %v", id, err)
		return fmt.Errorf("error iterating task rows: %w", err)
	}
	taskRows.Close()

	// Batch SET NULL on cracked_by_task_id (if there are any tasks)
	if len(taskIDs) > 0 {
		debug.Info("[DeleteWithProgressStreaming:%d] Pre-emptively clearing cracked_by_task_id for %d tasks", id, len(taskIDs))

		// Count affected hashes first for progress
		var refsTotal int64
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM hashes WHERE cracked_by_task_id = ANY($1)
		`, pq.Array(taskIDs)).Scan(&refsTotal)
		if err != nil {
			debug.Warning("[DeleteWithProgressStreaming:%d] Could not count cracked hashes: %v", id, err)
			refsTotal = 0
		}

		if refsTotal > 0 {
			debug.Info("[DeleteWithProgressStreaming:%d] Clearing cracked_by_task_id for %d hashes", id, refsTotal)

			// Report phase change
			if onProgress != nil {
				onProgress("clearing_references", "Clearing task references", 0, refsTotal, 0, 0, refsTotal)
			}

			// Batch update in chunks to avoid long locks
			const refsBatchSize = 50000
			var refsCleared int64
			for {
				// Check context for cancellation
				select {
				case <-ctx.Done():
					debug.Warning("[DeleteWithProgressStreaming:%d] Context cancelled during refs clearing", id)
					return ctx.Err()
				default:
				}

				result, err := tx.ExecContext(ctx, `
					UPDATE hashes SET cracked_by_task_id = NULL
					WHERE id IN (
						SELECT id FROM hashes
						WHERE cracked_by_task_id = ANY($1)
						LIMIT $2
					)
				`, pq.Array(taskIDs), refsBatchSize)
				if err != nil {
					debug.Error("[DeleteWithProgressStreaming:%d] Failed to clear cracked_by_task_id batch: %v", id, err)
					return fmt.Errorf("failed to clear cracked_by_task_id for hashlist %d: %w", id, err)
				}

				rowsUpdated, _ := result.RowsAffected()
				if rowsUpdated == 0 {
					break
				}
				refsCleared += rowsUpdated

				// Progress callback every batch
				if onProgress != nil {
					onProgress("clearing_references", "Clearing task references", refsCleared, refsTotal, 0, refsCleared, refsTotal)
				}

				// Progress logging every 500k rows
				if refsCleared > 0 && refsCleared%500000 < int64(refsBatchSize) {
					debug.Info("[DeleteWithProgressStreaming:%d] Cleared cracked_by_task_id: %d/%d (%.1f%%)",
						id, refsCleared, refsTotal, float64(refsCleared)/float64(refsTotal)*100)
				}
			}
			debug.Info("[DeleteWithProgressStreaming:%d] Cleared cracked_by_task_id for %d hashes", id, refsCleared)
		}
	}

	// Report finalizing phase
	if onProgress != nil {
		onProgress("finalizing", "Finalizing deletion", 0, 0, 0, 0, 0)
	}

	// Step 4: Delete hashlist record (CASCADE handles job_executions, agent_hashlists, linked_hashlists)
	// hashlist_hashes is already empty and cracked_by_task_id refs cleared, so CASCADE is instant!
	deleteHashlistQuery := `DELETE FROM hashlists WHERE id = $1`
	result, err := tx.ExecContext(ctx, deleteHashlistQuery, id)
	if err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to delete hashlist record: %v", id, err)
		return fmt.Errorf("failed to delete hashlist %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("[DeleteWithProgressStreaming:%d] Could not get rows affected after deleting hashlist: %v", id, err)
	} else if rowsAffected == 0 {
		_ = tx.Commit()
		return fmt.Errorf("hashlist %d not found for deletion: %w", id, ErrNotFound)
	}
	debug.Info("[DeleteWithProgressStreaming:%d] Deleted hashlist record (CASCADE handled jobs, agents, linked)", id)

	// Step 5: Stream orphan detection with keyset pagination
	// Now that hashlist_hashes entries are gone, find hashes that are no longer in ANY hashlist
	const orphanBatchSize = 10000
	var cursor uuid.UUID = uuid.Nil
	var checked, totalOrphansDeleted int64

	debug.Info("[DeleteWithProgressStreaming:%d] Starting orphan hash cleanup", id)

	// Report phase change to orphan cleanup
	if onProgress != nil {
		onProgress("cleaning_orphans", "Cleaning orphan hashes", 0, totalCandidates, 0, 0, 0)
	}

	for {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			debug.Warning("[DeleteWithProgressStreaming:%d] Context cancelled during orphan cleanup", id)
			return ctx.Err()
		default:
		}

		// Get next batch of candidates using keyset pagination
		rows, err := tx.QueryContext(ctx, `
			SELECT hash_id FROM deletion_candidates
			WHERE hash_id > $1
			ORDER BY hash_id
			LIMIT $2
		`, cursor, orphanBatchSize)
		if err != nil {
			debug.Error("[DeleteWithProgressStreaming:%d] Failed to query batch at cursor %s: %v", id, cursor, err)
			return fmt.Errorf("failed to query batch for hashlist %d deletion: %w", id, err)
		}

		var batchIDs []uuid.UUID
		for rows.Next() {
			var hashID uuid.UUID
			if err := rows.Scan(&hashID); err != nil {
				rows.Close()
				debug.Error("[DeleteWithProgressStreaming:%d] Failed to scan hash ID: %v", id, err)
				return fmt.Errorf("failed to scan hash ID for hashlist %d deletion: %w", id, err)
			}
			batchIDs = append(batchIDs, hashID)
			cursor = hashID
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			debug.Error("[DeleteWithProgressStreaming:%d] Error iterating batch rows: %v", id, err)
			return fmt.Errorf("error iterating batch rows for hashlist %d deletion: %w", id, err)
		}
		rows.Close()

		if len(batchIDs) == 0 {
			break // No more candidates
		}

		// Find orphans (hashes not in any hashlist anymore)
		orphanRows, err := tx.QueryContext(ctx, `
			SELECT id FROM hashes
			WHERE id = ANY($1)
			  AND NOT EXISTS (SELECT 1 FROM hashlist_hashes WHERE hash_id = hashes.id)
		`, pq.Array(batchIDs))
		if err != nil {
			debug.Error("[DeleteWithProgressStreaming:%d] Failed to query orphans in batch: %v", id, err)
			return fmt.Errorf("failed to query orphans for hashlist %d deletion: %w", id, err)
		}

		var orphanIDs []uuid.UUID
		for orphanRows.Next() {
			var hashID uuid.UUID
			if err := orphanRows.Scan(&hashID); err != nil {
				orphanRows.Close()
				debug.Error("[DeleteWithProgressStreaming:%d] Failed to scan orphan hash ID: %v", id, err)
				return fmt.Errorf("failed to scan orphan hash ID for hashlist %d deletion: %w", id, err)
			}
			orphanIDs = append(orphanIDs, hashID)
		}
		if err = orphanRows.Err(); err != nil {
			orphanRows.Close()
			debug.Error("[DeleteWithProgressStreaming:%d] Error iterating orphan rows: %v", id, err)
			return fmt.Errorf("error iterating orphan rows for hashlist %d deletion: %w", id, err)
		}
		orphanRows.Close()

		// Delete orphans in this batch
		if len(orphanIDs) > 0 {
			result, err := tx.ExecContext(ctx, `DELETE FROM hashes WHERE id = ANY($1)`, pq.Array(orphanIDs))
			if err != nil {
				debug.Error("[DeleteWithProgressStreaming:%d] Failed to delete orphan batch: %v", id, err)
				return fmt.Errorf("failed to delete orphan batch for hashlist %d deletion: %w", id, err)
			}
			deleted, _ := result.RowsAffected()
			totalOrphansDeleted += deleted
		}

		checked += int64(len(batchIDs))

		// Report progress: we're past the junction table deletion, now doing orphan cleanup
		if onProgress != nil {
			onProgress("cleaning_orphans", "Cleaning orphan hashes", checked, totalCandidates, totalOrphansDeleted, 0, 0)
		}

		// Progress logging every 100k candidates checked during orphan phase
		if checked > 0 && checked%100000 < int64(orphanBatchSize) {
			debug.Info("[DeleteWithProgressStreaming:%d] Orphan cleanup: checked %d/%d candidates, deleted %d orphans",
				id, checked, totalCandidates, totalOrphansDeleted)
		}
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		debug.Error("[DeleteWithProgressStreaming:%d] Failed to commit transaction: %v", id, err)
		return fmt.Errorf("failed to commit hashlist deletion transaction for %d: %w", id, err)
	}

	// Final progress report
	if onProgress != nil {
		onProgress("completed", "Deletion complete", totalCandidates, totalCandidates, totalOrphansDeleted, 0, 0)
	}

	sharedPreserved := totalCandidates - totalOrphansDeleted
	debug.Info("[DeleteWithProgressStreaming:%d] Hashlist deletion completed: %d total hashes, %d orphans deleted, %d shared preserved",
		id, totalCandidates, totalOrphansDeleted, sharedPreserved)
	return nil
}

// Helper function to join conditions (replace with strings.Join if no args involved)
func joinConditions(conditions []string, separator string) string {
	if len(conditions) == 0 {
		return ""
	}
	result := conditions[0]
	for i := 1; i < len(conditions); i++ {
		result += separator + conditions[i]
	}
	return result
}

// UpdateStatsAndStatusWithPath updates the hash counts, status, error message, and file path of a hashlist after processing.
func (r *HashListRepository) UpdateStatsAndStatusWithPath(ctx context.Context, id int64, totalHashes, crackedHashes int, status, errorMessage, filePath string) error {
	query := `
		UPDATE hashlists
		SET total_hashes = $1, cracked_hashes = $2, status = $3, error_message = $4, file_path = $5, updated_at = $6
		WHERE id = $7
	`
	result, err := r.db.ExecContext(ctx, query, totalHashes, crackedHashes, status, errorMessage, filePath, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist stats, status and path for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for stats/status/path update: %w", id, ErrNotFound)
	}
	return nil
}

// IncrementCrackedCount atomically increases the cracked_hashes count for a specific hashlist.
func (r *HashListRepository) IncrementCrackedCount(ctx context.Context, id int64, count int) error {
	if count <= 0 {
		return nil // Nothing to increment
	}
	query := `
		UPDATE hashlists
		SET cracked_hashes = cracked_hashes + $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, count, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to increment cracked count for hashlist %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
		debug.Warning("Error checking rows affected after incrementing cracked count for hashlist %d: %v", id, err)
	} else if rowsAffected == 0 {
		// This might happen if the hashlist was deleted between processing steps, log as warning.
		debug.Warning("Hashlist %d not found when trying to increment cracked count.", id)
	}
	return nil
}

// IncrementCrackedCountTx atomically increments the cracked hashes count for a hashlist within a transaction.
func (r *HashListRepository) IncrementCrackedCountTx(tx *sql.Tx, id int64, count int) error {
	query := `UPDATE hashlists SET cracked_hashes = cracked_hashes + $1, updated_at = $2 WHERE id = $3`
	_, err := tx.Exec(query, count, time.Now(), id) // Use tx.Exec instead of r.db.ExecContext
	if err != nil {
		return fmt.Errorf("failed to increment cracked count for hashlist %d within transaction: %w", id, err)
	}
	return nil
}

// DeleteTx removes a hashlist record from the database by its ID within a transaction.
func (r *HashListRepository) DeleteTx(tx *sql.Tx, id int64) error {
	query := queries.DeleteHashlistQuery                           // Assumes this const exists
	result, err := tx.ExecContext(context.Background(), query, id) // Use Tx
	if err != nil {
		return fmt.Errorf("failed to delete hashlist %d within transaction: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Warning: Could not get rows affected after deleting hashlist %d in tx: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist with ID %d not found for deletion in tx: %w", id, ErrNotFound)
	}

	return nil
}

// SyncCrackedCount updates the cracked_hashes count for a hashlist to match the actual count of cracked hashes.
// This ensures the cached count reflects reality, including pre-cracked hashes from previous uploads.
func (r *HashListRepository) SyncCrackedCount(ctx context.Context, hashlistID int64) error {
	query := `
		UPDATE hashlists 
		SET cracked_hashes = (
			SELECT COUNT(*) 
			FROM hashlist_hashes hh 
			JOIN hashes h ON hh.hash_id = h.id 
			WHERE hh.hashlist_id = $1 AND h.is_cracked = true
		),
		updated_at = $2
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, hashlistID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to sync cracked count for hashlist %d: %w", hashlistID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after syncing cracked count for hashlist %d: %v", hashlistID, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for cracked count sync: %w", hashlistID, ErrNotFound)
	}

	return nil
}

// IsExcludedFromPotfile checks if a hashlist is excluded from potfile
func (r *HashListRepository) IsExcludedFromPotfile(ctx context.Context, hashlistID int64) (bool, error) {
	query := `SELECT exclude_from_potfile FROM hashlists WHERE id = $1`
	var excluded bool
	err := r.db.QueryRowContext(ctx, query, hashlistID).Scan(&excluded)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("hashlist with ID %d not found: %w", hashlistID, ErrNotFound)
		}
		return false, fmt.Errorf("failed to check potfile exclusion for hashlist %d: %w", hashlistID, err)
	}
	return excluded, nil
}

// GetHashlistsContainingHashes returns all hashlists that contain any of the specified hash values
// This is used to find all hashlists affected by newly cracked hashes for cross-hashlist updates
func (r *HashListRepository) GetHashlistsContainingHashes(ctx context.Context, hashValues []string) ([]models.HashList, error) {
	if len(hashValues) == 0 {
		return []models.HashList{}, nil
	}

	query := `
		SELECT DISTINCT hl.id, hl.name, hl.hash_type_id, hl.total_hashes,
		       hl.cracked_hashes, hl.user_id, hl.client_id, hl.created_at, hl.updated_at,
		       hl.status, hl.exclude_from_potfile
		FROM hashlists hl
		JOIN hashlist_hashes hh ON hl.id = hh.hashlist_id
		JOIN hashes h ON hh.hash_id = h.id
		WHERE h.hash_value = ANY($1)
		ORDER BY hl.id`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashValues))
	if err != nil {
		return nil, fmt.Errorf("failed to query hashlists containing hashes: %w", err)
	}
	defer rows.Close()

	var hashlists []models.HashList
	for rows.Next() {
		var hl models.HashList
		var clientID sql.NullString
		err := rows.Scan(&hl.ID, &hl.Name, &hl.HashTypeID, &hl.TotalHashes,
			&hl.CrackedHashes, &hl.UserID, &clientID, &hl.CreatedAt, &hl.UpdatedAt,
			&hl.Status, &hl.ExcludeFromPotfile)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hashlist: %w", err)
		}

		if clientID.Valid {
			if parsedUUID, parseErr := uuid.Parse(clientID.String); parseErr == nil {
				hl.ClientID = parsedUUID
			}
		}

		hashlists = append(hashlists, hl)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hashlists: %w", err)
	}

	return hashlists, nil
}

// GetUncrackedHashCount returns the number of uncracked hashes for a given hashlist
// Used for progressive effective keyspace refinement when hashlist changes
func (r *HashListRepository) GetUncrackedHashCount(ctx context.Context, hashlistID int64) (int, error) {
	query := `
		SELECT COUNT(DISTINCT h.id)
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = $1 AND h.is_cracked = false`

	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get uncracked hash count: %w", err)
	}

	return count, nil
}

// LinkHashlists creates a bidirectional link between two hashlists
func (r *HashListRepository) LinkHashlists(ctx context.Context, hashlistID1, hashlistID2 int64, linkType string) error {
	query := `
		INSERT INTO linked_hashlists (hashlist_id_1, hashlist_id_2, link_type, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, hashlistID1, hashlistID2, linkType, time.Now())
	if err != nil {
		return fmt.Errorf("failed to link hashlists %d and %d: %w", hashlistID1, hashlistID2, err)
	}
	return nil
}

// CountJobExecutions returns the number of job_executions for a given hashlist.
// Used to report stats during hashlist deletion.
func (r *HashListRepository) CountJobExecutions(ctx context.Context, hashlistID int64) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_executions WHERE hashlist_id = $1`, hashlistID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count job_executions for hashlist %d: %w", hashlistID, err)
	}
	return count, nil
}

// GetLinkedHashlist returns the linked hashlist for a given hashlist ID
func (r *HashListRepository) GetLinkedHashlist(ctx context.Context, hashlistID int64, linkType string) (*models.HashList, error) {
	// Check both directions: either hashlist_id_1 or hashlist_id_2 could be our ID
	query := `
		SELECT hl.id, hl.name, hl.user_id, hl.client_id, hl.hash_type_id,
		       hl.total_hashes, hl.cracked_hashes, hl.status, hl.error_message,
		       hl.exclude_from_potfile, hl.created_at, hl.updated_at
		FROM hashlists hl
		INNER JOIN linked_hashlists lhl ON (
			(lhl.hashlist_id_1 = $1 AND lhl.hashlist_id_2 = hl.id) OR
			(lhl.hashlist_id_2 = $1 AND lhl.hashlist_id_1 = hl.id)
		)
		WHERE lhl.link_type = $2
		LIMIT 1
	`

	var hl models.HashList
	var clientID sql.NullString
	var errorMessage sql.NullString

	err := r.db.QueryRowContext(ctx, query, hashlistID, linkType).Scan(
		&hl.ID,
		&hl.Name,
		&hl.UserID,
		&clientID,
		&hl.HashTypeID,
		&hl.TotalHashes,
		&hl.CrackedHashes,
		&hl.Status,
		&errorMessage,
		&hl.ExcludeFromPotfile,
		&hl.CreatedAt,
		&hl.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No linked hashlist found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get linked hashlist for %d: %w", hashlistID, err)
	}

	// Handle nullable fields
	if clientID.Valid {
		if parsedUUID, err := uuid.Parse(clientID.String); err == nil {
			hl.ClientID = parsedUUID
		}
	}
	hl.ErrorMessage = errorMessage

	return &hl, nil
}
