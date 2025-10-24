package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// HashRepository handles database operations for individual hashes and their associations.
type HashRepository struct {
	db *db.DB
}

// NewHashRepository creates a new instance of HashRepository.
func NewHashRepository(database *db.DB) *HashRepository {
	return &HashRepository{db: database}
}

// GetByHashValues retrieves existing hashes based on a list of hash values.
func (r *HashRepository) GetByHashValues(ctx context.Context, hashValues []string) ([]*models.Hash, error) {
	if len(hashValues) == 0 {
		return []*models.Hash{}, nil
	}

	query := `
		SELECT id, hash_value, original_hash, hash_type_id, is_cracked, password, last_updated, username, domain
		FROM hashes
		WHERE hash_value = ANY($1)
	`
	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashValues))
	if err != nil {
		return nil, fmt.Errorf("failed to get hashes by values: %w", err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
			&hash.Username,
			&hash.Domain,
		); err != nil {
			return nil, fmt.Errorf("failed to scan hash row: %w", err)
		}
		hashes = append(hashes, &hash)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash rows: %w", err)
	}

	return hashes, nil
}

// CreateBatch inserts multiple new hash records into the database.
// It returns the newly created hashes (potentially with updated IDs from the DB, though UUIDs are generated client-side here).
func (r *HashRepository) CreateBatch(ctx context.Context, hashes []*models.Hash) ([]*models.Hash, error) {
	debug.Debug("[DB:CreateBatch] Received %d hashes to create", len(hashes))
	if len(hashes) == 0 {
		return []*models.Hash{}, nil
	}

	txn, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction for batch hash create: %w", err)
	}
	defer txn.Rollback() // Rollback if commit isn't reached

	stmt, err := txn.PrepareContext(ctx, `
		INSERT INTO hashes (id, hash_value, original_hash, username, domain, hash_type_id, is_cracked, password, last_updated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement for batch hash create: %w", err)
	}
	defer stmt.Close()

	for i, hash := range hashes {
		// Generate UUID if not already set (though handler usually does this)
		if hash.ID == uuid.Nil {
			hash.ID = uuid.New()
		}
		if hash.LastUpdated.IsZero() {
			hash.LastUpdated = time.Now()
		}
		debug.Debug("[DB:CreateBatch] Attempting insert %d: ID=%s, Value='%s'", i+1, hash.ID, hash.HashValue)
		_, err := stmt.ExecContext(ctx,
			hash.ID,
			hash.HashValue,
			hash.OriginalHash,
			hash.Username,
			hash.Domain,
			hash.HashTypeID,
			hash.IsCracked,
			hash.Password,
			hash.LastUpdated,
		)
		if err != nil {
			// If ON CONFLICT is removed, we might need error handling for other potential issues.
			// For now, let's return the error directly if ExecContext fails.
			return nil, fmt.Errorf("failed to execute batch hash insert for hash %s (ID: %s): %w", hash.HashValue, hash.ID, err)
		}
	}

	if err = txn.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction for batch hash create: %w", err)
	}
	debug.Info("[DB:CreateBatch] Transaction committed successfully for %d hashes", len(hashes))

	// Since ON CONFLICT DO NOTHING doesn't return IDs, and we generate UUIDs client-side,
	// we return the original input slice. The caller needs GetByHashValues to get actual DB state if needed.
	return hashes, nil
}

// UpdateBatch updates multiple existing hash records, typically for cracking status.
func (r *HashRepository) UpdateBatch(ctx context.Context, hashes []*models.Hash) error {
	if len(hashes) == 0 {
		return nil
	}

	txn, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for batch hash update: %w", err)
	}
	defer txn.Rollback()

	stmt, err := txn.PrepareContext(ctx, `
		UPDATE hashes
		SET is_cracked = $1, password = $2, username = COALESCE(username, $3), domain = COALESCE(domain, $4), last_updated = $5
		WHERE id = $6
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for batch hash update: %w", err)
	}
	defer stmt.Close()

	for _, hash := range hashes {
		if hash.ID == uuid.Nil {
			fmt.Printf("Warning: Skipping hash update for hash value %s due to missing ID\n", hash.HashValue)
			continue // Cannot update without an ID
		}
		result, err := stmt.ExecContext(ctx,
			hash.IsCracked,
			hash.Password,
			hash.Username, // Add username argument (COALESCE handles NULL case in SQL)
			hash.Domain,   // Add domain argument (COALESCE handles NULL case in SQL)
			time.Now(),    // Update last_updated time
			hash.ID,
		)
		if err != nil {
			return fmt.Errorf("failed to execute batch hash update for hash ID %s: %w", hash.ID, err)
		}
		rowsAffected, _ := result.RowsAffected() // Ignore error checking rows affected for simplicity
		if rowsAffected == 0 {
			fmt.Printf("Warning: No rows affected when updating hash ID %s\n", hash.ID)
		}
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for batch hash update: %w", err)
	}

	return nil
}

// AddBatchToHashList creates association records between a hashlist and multiple hashes.
func (r *HashRepository) AddBatchToHashList(ctx context.Context, associations []*models.HashListHash) error {
	if len(associations) == 0 {
		return nil
	}

	txn, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for batch hashlist association: %w", err)
	}
	defer txn.Rollback()

	stmt, err := txn.PrepareContext(ctx, `
		INSERT INTO hashlist_hashes (hashlist_id, hash_id)
		VALUES ($1, $2)
		ON CONFLICT (hashlist_id, hash_id) DO NOTHING -- Ignore if association already exists
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for batch hashlist association: %w", err)
	}
	defer stmt.Close()

	for _, assoc := range associations {
		if assoc.HashID == uuid.Nil {
			fmt.Printf("Warning: Skipping hashlist association due to missing HashID (List: %d, Hash: %s)\n", assoc.HashlistID, assoc.HashID)
			continue
		}
		_, err := stmt.ExecContext(ctx, assoc.HashlistID, assoc.HashID)
		if err != nil {
			// Should be handled by ON CONFLICT, but log if not.
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
				fmt.Printf("Warning: Unique constraint violation during batch association (should be handled by ON CONFLICT): %v\n", err)
			} else {
				return fmt.Errorf("failed to execute batch hashlist association for List %d, Hash %s: %w", assoc.HashlistID, assoc.HashID, err)
			}
		}
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for batch hashlist association: %w", err)
	}
	debug.Info("[DB:AddBatchToHashList] Transaction committed successfully for %d associations", len(associations))

	return nil
}

// SearchHashes finds hashes by value and retrieves associated hashlist info for a specific user.
func (r *HashRepository) SearchHashes(ctx context.Context, hashValues []string, userID uuid.UUID) ([]models.HashSearchResult, error) {
	if len(hashValues) == 0 {
		return []models.HashSearchResult{}, nil
	}

	// Query to find hashes and their associated hashlists owned by the user
	query := `
		SELECT
		    h.id, h.hash_value, h.original_hash, h.hash_type_id, h.is_cracked, h.password, h.last_updated, h.username,
		    hl.id AS hashlist_id, hl.name AS hashlist_name
		FROM hashes h
		JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
		JOIN hashlists hl ON hlh.hashlist_id = hl.id
		WHERE h.hash_value = ANY($1)
		  AND hl.user_id = $2
		ORDER BY h.hash_value, hl.name; -- Group results by hash value
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashValues), userID)
	if err != nil {
		return nil, fmt.Errorf("failed to search hashes for user %s: %w", userID, err)
	}
	defer rows.Close()

	results := make(map[uuid.UUID]*models.HashSearchResult) // Map hash ID to result

	for rows.Next() {
		var hash models.Hash
		var hashlistID int64
		var hashlistName string

		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
			&hash.Username,
			&hashlistID,
			&hashlistName,
		); err != nil {
			return nil, fmt.Errorf("failed to scan hash search result row: %w", err)
		}

		// Check if we've seen this hash ID before
		if _, exists := results[hash.ID]; !exists {
			// First time seeing this hash, create the result entry
			results[hash.ID] = &models.HashSearchResult{
				Hash:      hash,
				Hashlists: []models.HashlistInfo{},
			}
		}

		// Add the hashlist info to the existing hash entry
		results[hash.ID].Hashlists = append(results[hash.ID].Hashlists, models.HashlistInfo{
			ID:   hashlistID,
			Name: hashlistName,
		})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash search results: %w", err)
	}

	// Convert map to slice
	finalResults := make([]models.HashSearchResult, 0, len(results))
	for _, result := range results {
		finalResults = append(finalResults, *result)
	}

	return finalResults, nil
}

// GetHashesByHashlistID retrieves hashes associated with a specific hashlist, with pagination.
func (r *HashRepository) GetHashesByHashlistID(ctx context.Context, hashlistID int64, limit, offset int) ([]models.Hash, int, error) {
	// Query to count total hashes for the hashlist
	countQuery := `SELECT COUNT(h.id)
				  FROM hashes h
				  JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
				  WHERE hlh.hashlist_id = $1`
	var totalCount int
	err := r.db.QueryRowContext(ctx, countQuery, hashlistID).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count hashes for hashlist %d: %w", hashlistID, err)
	}

	if totalCount == 0 {
		return []models.Hash{}, 0, nil
	}

	// Query to retrieve the paginated hashes
	// Sort by is_cracked DESC to show cracked hashes first, then by id for consistency
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
		WHERE hlh.hashlist_id = $1
		ORDER BY h.is_cracked DESC, h.id
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, hashlistID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get hashes for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var hashes []models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan hash row for hashlist %d: %w", hashlistID, err)
		}
		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating hash rows for hashlist %d: %w", hashlistID, err)
	}

	return hashes, totalCount, nil
}

// GetUncrackedHashValuesByHashlistID retrieves only the hash_value strings for uncracked hashes
// associated with a specific hashlist. Uses DISTINCT to ensure unique hash values only
// (e.g., when multiple users have the same password, only send the hash once to hashcat).
func (r *HashRepository) GetUncrackedHashValuesByHashlistID(ctx context.Context, hashlistID int64) ([]string, error) {
	query := `
		SELECT DISTINCT h.hash_value
		FROM hashes h
		JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
		WHERE hlh.hashlist_id = $1 AND h.is_cracked = FALSE
		ORDER BY h.hash_value
	`

	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to query uncracked hash values for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var hashValues []string
	for rows.Next() {
		var hashValue string
		if err := rows.Scan(&hashValue); err != nil {
			return nil, fmt.Errorf("failed to scan uncracked hash value for hashlist %d: %w", hashlistID, err)
		}
		hashValues = append(hashValues, hashValue)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating uncracked hash values for hashlist %d: %w", hashlistID, err)
	}

	return hashValues, nil
}

// GetByHashValueForUpdate retrieves a hash by its value within a transaction, locking the row.
func (r *HashRepository) GetByHashValueForUpdate(tx *sql.Tx, hashValue string) (*models.Hash, error) {
	query := `
		SELECT id, hash_value, original_hash, hash_type_id, is_cracked, password, last_updated, username, domain
		FROM hashes
		WHERE hash_value = $1
		FOR UPDATE -- Lock the row
	`
	row := tx.QueryRow(query, hashValue)

	hash := &models.Hash{}
	err := row.Scan(
		&hash.ID,
		&hash.HashValue,
		&hash.OriginalHash,
		&hash.HashTypeID,
		&hash.IsCracked,
		&hash.Password,
		&hash.LastUpdated,
		&hash.Username,
		&hash.Domain,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("error scanning hash row: %w", err)
	}

	return hash, nil
}

// UpdateCrackStatus updates the cracked status and password for a hash within a transaction.
func (r *HashRepository) UpdateCrackStatus(tx *sql.Tx, hashID uuid.UUID, password string, crackedAt time.Time, username *string) error {
	query := `
		UPDATE hashes
		SET is_cracked = TRUE, password = $1, username = COALESCE(username, $2), last_updated = $3
		WHERE id = $4 AND is_cracked = FALSE -- Only update if not already cracked
	`
	result, err := tx.Exec(query, password, username, crackedAt, hashID)
	if err != nil {
		return fmt.Errorf("failed to update crack status for hash %s: %w", hashID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected after update crack status: %w", err)
	}

	if rowsAffected == 0 {
		// This could happen if the hash was already cracked between the SELECT FOR UPDATE and this UPDATE.
		// Or if the ID doesn't exist (which shouldn't happen if GetBy... succeeded).
		// Check if it's already cracked.
		var isCracked bool
		checkQuery := `SELECT is_cracked FROM hashes WHERE id = $1`
		err := tx.QueryRow(checkQuery, hashID).Scan(&isCracked)
		if err != nil {
			return fmt.Errorf("error checking hash status after update attempt: %w", err)
		}
		if isCracked {
			debug.Info("Hash %s was already marked as cracked when attempting update.", hashID)
			return nil // Race condition, but effectively already done.
		}
		return fmt.Errorf("hash %s not found or already cracked during update (rows affected 0)", hashID)
	}

	return nil
}

// ---- Transactional methods for RetentionService ----

// Querier defines methods implemented by both *sql.DB and *sql.Tx
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// GetHashIDsByHashlistIDTx retrieves all hash IDs associated with a hashlist within a transaction.
func (r *HashRepository) GetHashIDsByHashlistIDTx(tx *sql.Tx, hashlistID int64) ([]uuid.UUID, error) {
	query := queries.GetHashIDsByHashlistIDQuery                          // Assumes this const exists in queries pkg
	rows, err := tx.QueryContext(context.Background(), query, hashlistID) // Use background context within tx
	if err != nil {
		return nil, fmt.Errorf("failed to query hash IDs by hashlist ID %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var hashIDs []uuid.UUID
	for rows.Next() {
		var hashID uuid.UUID
		if err := rows.Scan(&hashID); err != nil {
			return nil, fmt.Errorf("failed to scan hash ID for hashlist %d: %w", hashlistID, err)
		}
		hashIDs = append(hashIDs, hashID)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash ID rows for hashlist %d: %w", hashlistID, err)
	}
	return hashIDs, nil
}

// DeleteHashlistAssociationsTx deletes all entries from hashlist_hashes for a given hashlist ID within a transaction.
func (r *HashRepository) DeleteHashlistAssociationsTx(tx *sql.Tx, hashlistID int64) error {
	query := queries.DeleteHashlistAssociationsQuery // Assumes this const exists
	result, err := tx.ExecContext(context.Background(), query, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to delete hashlist associations for hashlist ID %d: %w", hashlistID, err)
	}
	_, err = result.RowsAffected() // Optional: Check rows affected
	if err != nil {
		debug.Warning("Failed to get rows affected after deleting hashlist associations for %d: %v", hashlistID, err)
	}
	return nil
}

// IsHashOrphanedTx checks if a hash is associated with any hashlist within a transaction.
func (r *HashRepository) IsHashOrphanedTx(tx *sql.Tx, hashID uuid.UUID) (bool, error) {
	query := queries.CheckHashAssociationExistsQuery // Assumes this const exists
	var exists bool
	err := tx.QueryRowContext(context.Background(), query, hashID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if hash %s is orphaned: %w", hashID, err)
	}
	return !exists, nil // Orphaned if it doesn't exist in the junction table
}

// DeleteHashByIDTx deletes a hash from the hashes table by its ID within a transaction.
func (r *HashRepository) DeleteHashByIDTx(tx *sql.Tx, hashID uuid.UUID) error {
	query := queries.DeleteHashByIDQuery // Assumes this const exists
	result, err := tx.ExecContext(context.Background(), query, hashID)
	if err != nil {
		return fmt.Errorf("failed to delete hash by ID %s: %w", hashID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Failed to get rows affected after deleting hash %s: %v", hashID, err)
	} else if rowsAffected == 0 {
		debug.Warning("Hash %s not found for deletion (or already deleted)", hashID)
	}
	return nil
}

// CrackedHashParams defines parameters for querying cracked hashes
type CrackedHashParams struct {
	Limit  int
	Offset int
}

// GetCrackedHashes retrieves all cracked hashes with pagination
func (r *HashRepository) GetCrackedHashes(ctx context.Context, params CrackedHashParams) ([]*models.Hash, int64, error) {
	// First, get the total count
	countQuery := `SELECT COUNT(*) FROM hashes WHERE is_cracked = true`
	var totalCount int64
	err := r.db.QueryRowContext(ctx, countQuery).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count cracked hashes: %w", err)
	}

	// Then get the paginated results
	query := `
		SELECT id, hash_value, original_hash, username, domain, hash_type_id, is_cracked, password, last_updated
		FROM hashes
		WHERE is_cracked = true
		ORDER BY last_updated DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query cracked hashes: %w", err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan cracked hash row: %w", err)
		}
		hashes = append(hashes, &hash)
	}
	
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating cracked hash rows: %w", err)
	}

	return hashes, totalCount, nil
}

// GetCrackedHashesByHashlist retrieves cracked hashes for a specific hashlist
func (r *HashRepository) GetCrackedHashesByHashlist(ctx context.Context, hashlistID int64, params CrackedHashParams) ([]*models.Hash, int64, error) {
	// First, get the total count
	countQuery := `
		SELECT COUNT(*)
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = $1 AND h.is_cracked = true
	`
	var totalCount int64
	err := r.db.QueryRowContext(ctx, countQuery, hashlistID).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count cracked hashes for hashlist %d: %w", hashlistID, err)
	}

	// Then get the paginated results
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = $1 AND h.is_cracked = true
		ORDER BY h.last_updated DESC
		LIMIT $2 OFFSET $3
	`
	
	rows, err := r.db.QueryContext(ctx, query, hashlistID, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query cracked hashes for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan cracked hash row for hashlist %d: %w", hashlistID, err)
		}
		hashes = append(hashes, &hash)
	}
	
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating cracked hash rows for hashlist %d: %w", hashlistID, err)
	}

	return hashes, totalCount, nil
}

// GetCrackedHashesByClient retrieves cracked hashes for a specific client
func (r *HashRepository) GetCrackedHashesByClient(ctx context.Context, clientID uuid.UUID, params CrackedHashParams) ([]*models.Hash, int64, error) {
	// First, get the total count
	countQuery := `
		SELECT COUNT(*)
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN hashlists hl ON hh.hashlist_id = hl.id
		WHERE hl.client_id = $1 AND h.is_cracked = true
	`
	var totalCount int64
	err := r.db.QueryRowContext(ctx, countQuery, clientID).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count cracked hashes for client %s: %w", clientID, err)
	}

	// Then get the paginated results
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN hashlists hl ON hh.hashlist_id = hl.id
		WHERE hl.client_id = $1 AND h.is_cracked = true
		ORDER BY h.last_updated DESC
		LIMIT $2 OFFSET $3
	`
	
	rows, err := r.db.QueryContext(ctx, query, clientID, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query cracked hashes for client %s: %w", clientID, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan cracked hash row for client %s: %w", clientID, err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating cracked hash rows for client %s: %w", clientID, err)
	}

	return hashes, totalCount, nil
}

// GetCrackedHashesByJob retrieves cracked hashes for a specific job execution
func (r *HashRepository) GetCrackedHashesByJob(ctx context.Context, jobID uuid.UUID, params CrackedHashParams) ([]*models.Hash, int64, error) {
	// First, get the total count
	countQuery := `
		SELECT COUNT(*)
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN job_executions j ON j.hashlist_id = hh.hashlist_id
		WHERE j.id = $1 AND h.is_cracked = true
	`
	var totalCount int64
	err := r.db.QueryRowContext(ctx, countQuery, jobID).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count cracked hashes for job %s: %w", jobID, err)
	}

	// Then get the paginated results
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN job_executions j ON j.hashlist_id = hh.hashlist_id
		WHERE j.id = $1 AND h.is_cracked = true
		ORDER BY h.last_updated DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, jobID, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query cracked hashes for job %s: %w", jobID, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan cracked hash row for job %s: %w", jobID, err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating cracked hash rows for job %s: %w", jobID, err)
	}

	return hashes, totalCount, nil
}

// StreamHashesForHashlist streams all hashes for a hashlist with their original format.
// This is used for generating download files in the original uploaded format.
// The callback is called for each hash to allow streaming without loading all into memory.
func (r *HashRepository) StreamHashesForHashlist(ctx context.Context, hashlistID int64, callback func(*models.Hash) error) error {
	query := `
		SELECT
			h.id,
			h.hash_value,
			h.original_hash,
			h.username,
			h.domain,
			h.hash_type_id,
			h.is_cracked,
			h.password,
			h.last_updated
		FROM hashes h
		INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = $1
		ORDER BY h.id
	`

	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to query hashes for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var hash models.Hash
		if err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		); err != nil {
			return fmt.Errorf("failed to scan hash row for hashlist %d: %w", hashlistID, err)
		}

		// Call the callback for this hash
		if err := callback(&hash); err != nil {
			return fmt.Errorf("callback error for hash %s in hashlist %d: %w", hash.ID, hashlistID, err)
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating hash rows for hashlist %d: %w", hashlistID, err)
	}

	return nil
}
