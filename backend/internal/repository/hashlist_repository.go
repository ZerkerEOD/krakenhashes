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

// UpdateFilePathAndStatus updates the file path and status of a hashlist, typically after file upload is complete.
func (r *HashListRepository) UpdateFilePathAndStatus(ctx context.Context, id int64, filePath string, status string) error {
	query := `
		UPDATE hashlists
		SET file_path = $1, status = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.ExecContext(ctx, query, filePath, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update hashlist file path and status for %d: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Log warning
	} else if rowsAffected == 0 {
		return fmt.Errorf("hashlist %d not found for file path/status update: %w", id, ErrNotFound)
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
			h.id, h.name, h.user_id, h.client_id, h.hash_type_id, h.file_path,
			h.total_hashes, h.cracked_hashes, h.status, h.error_message,
			h.exclude_from_potfile, h.created_at, h.updated_at,
			c.name AS client_name
		FROM hashlists h
		LEFT JOIN clients c ON h.client_id = c.id
		WHERE h.id = $1
	`
	var hashlist models.HashList
	var clientID sql.Null[uuid.UUID] // Handle nullable client_id
	var filePath sql.NullString       // Handle nullable file_path
	var clientName sql.NullString     // Handle nullable client_name
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&hashlist.ID,
		&hashlist.Name,
		&hashlist.UserID,
		&clientID,
		&hashlist.HashTypeID,
		&filePath,
		&hashlist.TotalHashes,
		&hashlist.CrackedHashes,
		&hashlist.Status,
		&hashlist.ErrorMessage,
		&hashlist.ExcludeFromPotfile,
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
	if filePath.Valid {
		hashlist.FilePath = filePath.String
	}
	if clientName.Valid {
		hashlist.ClientName = &clientName.String
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
			h.file_path, h.total_hashes, h.cracked_hashes, h.status,
			h.error_message, h.exclude_from_potfile, h.created_at, h.updated_at,
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
		var clientID sql.Null[uuid.UUID] // Use sql.Null for nullable UUID
		var clientName sql.NullString    // Use sql.NullString for nullable client name from LEFT JOIN
		var filePath sql.NullString       // Use sql.NullString for nullable file_path

		if err := rows.Scan(
			&hashlist.ID,
			&hashlist.Name,
			&hashlist.UserID,
			&clientID, // Scan into nullable UUID
			&hashlist.HashTypeID,
			&filePath, // Scan into nullable string
			&hashlist.TotalHashes,
			&hashlist.CrackedHashes,
			&hashlist.Status,
			&hashlist.ErrorMessage,
			&hashlist.ExcludeFromPotfile,
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

		// Explicitly set FilePath to empty string for list view (security)
		// Note: filePath.Valid check is not needed here since we're always setting to empty
		hashlist.FilePath = ""

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
		SELECT id, name, user_id, client_id, hash_type_id, file_path, total_hashes, cracked_hashes, status, error_message, created_at, updated_at
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
			&hashlist.FilePath,
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
