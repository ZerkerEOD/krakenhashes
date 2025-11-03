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

// LMHashMatch contains a hash and which half (first or second) matched the search
type LMHashMatch struct {
	Hash        *models.Hash
	MatchedHalf string // "first" or "second"
}

// GetByHashValuesLMPartial finds LM hashes by matching 16-char half hashes
// against the first or second 16 characters of full 32-char LM hashes.
// Returns a map of: 16-char search half -> list of matching hashes with position indicator
func (r *HashRepository) GetByHashValuesLMPartial(ctx context.Context, hashHalves []string) (map[string][]*LMHashMatch, error) {
	if len(hashHalves) == 0 {
		return make(map[string][]*LMHashMatch), nil
	}

	// Query that matches each 16-char half against LEFT or RIGHT of full hash
	// and returns which position matched
	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.hash_type_id, h.is_cracked,
			h.password, h.last_updated, h.username, h.domain,
			search_half,
			CASE
				WHEN LEFT(h.hash_value, 16) = search_half THEN 'first'
				WHEN RIGHT(h.hash_value, 16) = search_half THEN 'second'
			END AS matched_half
		FROM hashes h
		CROSS JOIN LATERAL unnest($1::text[]) AS search_half
		WHERE (LEFT(h.hash_value, 16) = search_half OR RIGHT(h.hash_value, 16) = search_half)
		  AND h.hash_type_id = 3000
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashHalves))
	if err != nil {
		return nil, fmt.Errorf("failed to get LM hashes by partial values: %w", err)
	}
	defer rows.Close()

	// Build map: 16-char half -> list of matches
	result := make(map[string][]*LMHashMatch)
	for rows.Next() {
		var hash models.Hash
		var searchHalf string
		var matchedHalf string

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
			&searchHalf,
			&matchedHalf,
		); err != nil {
			return nil, fmt.Errorf("failed to scan LM hash row: %w", err)
		}

		match := &LMHashMatch{
			Hash:        &hash,
			MatchedHalf: matchedHalf,
		}

		result[searchHalf] = append(result[searchHalf], match)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating LM hash rows: %w", err)
	}

	return result, nil
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

	// Build multi-row INSERT query for better performance
	query := `INSERT INTO hashes (id, hash_value, original_hash, username, domain, hash_type_id, is_cracked, password, last_updated) VALUES `
	args := make([]interface{}, 0, len(hashes)*9)

	for i, hash := range hashes {
		// Generate UUID if not already set (though handler usually does this)
		if hash.ID == uuid.Nil {
			hash.ID = uuid.New()
		}
		if hash.LastUpdated.IsZero() {
			hash.LastUpdated = time.Now()
		}

		if i > 0 {
			query += ", "
		}

		// Add placeholders for this hash's 9 parameters
		baseParam := i * 9
		query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			baseParam+1, baseParam+2, baseParam+3, baseParam+4, baseParam+5,
			baseParam+6, baseParam+7, baseParam+8, baseParam+9)

		// Add parameters in the correct order
		args = append(args,
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
	}

	debug.Debug("[DB:CreateBatch] Executing multi-row INSERT for %d hashes", len(hashes))
	_, err = txn.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute batch hash insert: %w", err)
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

	// Build UPDATE FROM VALUES query for better performance
	query := `UPDATE hashes h SET
		is_cracked = v.is_cracked,
		password = v.password,
		username = COALESCE(h.username, v.username),
		domain = COALESCE(h.domain, v.domain),
		last_updated = v.last_updated
	FROM (VALUES `

	args := make([]interface{}, 0, len(hashes)*6)
	validHashes := 0

	for _, hash := range hashes {
		if hash.ID == uuid.Nil {
			debug.Warning("Skipping hash update for hash value %s due to missing ID", hash.HashValue)
			continue
		}

		if validHashes > 0 {
			query += ", "
		}

		// Add placeholders for this hash's 6 parameters
		baseParam := validHashes * 6
		query += fmt.Sprintf("($%d::uuid, $%d::boolean, $%d::text, $%d::text, $%d::text, $%d::timestamp)",
			baseParam+1, baseParam+2, baseParam+3, baseParam+4, baseParam+5, baseParam+6)

		// Add parameters in the correct order
		args = append(args,
			hash.ID,
			hash.IsCracked,
			hash.Password,
			hash.Username,
			hash.Domain,
			time.Now(),
		)
		validHashes++
	}

	if validHashes == 0 {
		return nil // Nothing to update
	}

	query += `) AS v(id, is_cracked, password, username, domain, last_updated) WHERE h.id = v.id`

	debug.Debug("[DB:UpdateBatch] Executing multi-row UPDATE for %d hashes", validHashes)
	result, err := txn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute batch hash update: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	debug.Info("[DB:UpdateBatch] Updated %d hash records", rowsAffected)

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

	// Build multi-row INSERT query for better performance
	query := `INSERT INTO hashlist_hashes (hashlist_id, hash_id) VALUES `
	args := make([]interface{}, 0, len(associations)*2)
	validAssociations := 0

	for _, assoc := range associations {
		if assoc.HashID == uuid.Nil {
			debug.Warning("Skipping hashlist association due to missing HashID (List: %d, Hash: %s)", assoc.HashlistID, assoc.HashID)
			continue
		}

		if validAssociations > 0 {
			query += ", "
		}

		// Add placeholders for this association's 2 parameters
		baseParam := validAssociations * 2
		query += fmt.Sprintf("($%d, $%d)", baseParam+1, baseParam+2)

		// Add parameters
		args = append(args, assoc.HashlistID, assoc.HashID)
		validAssociations++
	}

	if validAssociations == 0 {
		return nil // Nothing to insert
	}

	query += " ON CONFLICT (hashlist_id, hash_id) DO NOTHING"

	debug.Debug("[DB:AddBatchToHashList] Executing multi-row INSERT for %d associations", validAssociations)
	_, err = txn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute batch hashlist association: %w", err)
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction for batch hashlist association: %w", err)
	}
	debug.Info("[DB:AddBatchToHashList] Transaction committed successfully for %d associations", validAssociations)

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
	// Optimized: Query directly from hashlist_hashes instead of expensive JOIN
	countQuery := `SELECT COUNT(*)
				  FROM hashlist_hashes
				  WHERE hashlist_id = $1`
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
	// Include LM metadata for partial crack status
	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated,
			COALESCE(
				(lm.first_half_cracked OR lm.second_half_cracked) AND NOT (lm.first_half_cracked AND lm.second_half_cracked),
				FALSE
			) AS is_partially_lm_cracked,
			lm.first_half_password,
			lm.second_half_password
		FROM hashes h
		JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
		LEFT JOIN lm_hash_metadata lm ON h.id = lm.hash_id
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
		var lmFirstHalfPassword, lmSecondHalfPassword sql.NullString

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
			&hash.IsPartiallyLMCracked,
			&lmFirstHalfPassword,
			&lmSecondHalfPassword,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan hash row for hashlist %d: %w", hashlistID, err)
		}

		// Convert sql.NullString to *string for LM passwords
		if lmFirstHalfPassword.Valid {
			hash.LMFirstHalfPassword = &lmFirstHalfPassword.String
		}
		if lmSecondHalfPassword.Valid {
			hash.LMSecondHalfPassword = &lmSecondHalfPassword.String
		}

		hashes = append(hashes, hash)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating hash rows for hashlist %d: %w", hashlistID, err)
	}

	return hashes, totalCount, nil
}

// GetAllHashesByHashlistID retrieves ALL hashes for a hashlist (no pagination)
// Used for creating hash links between hashlists
func (r *HashRepository) GetAllHashesByHashlistID(ctx context.Context, hashlistID int64) ([]*models.Hash, error) {
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hlh ON h.id = hlh.hash_id
		WHERE hlh.hashlist_id = $1
		ORDER BY h.id
	`

	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all hashes for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		hash := &models.Hash{}
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
			return nil, fmt.Errorf("failed to scan hash row: %w", err)
		}
		hashes = append(hashes, hash)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash rows for hashlist %d: %w", hashlistID, err)
	}

	return hashes, nil
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
// HashUpdate represents a single hash update for batch processing
type HashUpdate struct {
	HashID    uuid.UUID
	Password  string
	Username  *string
	CrackedAt time.Time
}

// UpdateCrackStatusBatch updates multiple hashes at once using an efficient bulk query
// This dramatically reduces database round-trips compared to individual updates
func (r *HashRepository) UpdateCrackStatusBatch(tx *sql.Tx, updates []HashUpdate) (int64, error) {
	if len(updates) == 0 {
		return 0, nil
	}

	// Build a bulk UPDATE using PostgreSQL's UPDATE ... FROM pattern
	// This updates all hashes in a single query instead of N individual queries
	query := `
		UPDATE hashes h
		SET
			is_cracked = TRUE,
			password = u.password,
			username = COALESCE(h.username, u.username),
			last_updated = u.cracked_at
		FROM (VALUES
	`

	args := make([]interface{}, 0, len(updates)*4)
	for i, update := range updates {
		if i > 0 {
			query += ", "
		}
		query += fmt.Sprintf("($%d::uuid, $%d::text, $%d::text, $%d::timestamp)",
			i*4+1, i*4+2, i*4+3, i*4+4)
		args = append(args, update.HashID, update.Password, update.Username, update.CrackedAt)
	}

	query += `) AS u(hash_id, password, username, cracked_at)
		WHERE h.id = u.hash_id AND h.is_cracked = FALSE
	`

	result, err := tx.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to batch update crack status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("error getting rows affected: %w", err)
	}

	return rowsAffected, nil
}

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

// StreamUncrackedHashValuesForHashlist streams only uncracked hash_value fields for agents.
// This is optimized for agent hashlist downloads - only returns hash_value (not full hash object)
// and only returns uncracked hashes. Uses streaming to minimize memory usage.
func (r *HashRepository) StreamUncrackedHashValuesForHashlist(
	ctx context.Context,
	hashlistID int64,
	callback func(hashValue string) error,
) error {
	query := `
		SELECT DISTINCT h.hash_value
		FROM hashes h
		INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = $1 AND h.is_cracked = FALSE
		ORDER BY h.hash_value
	`

	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to query uncracked hash values for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var hashValue string
		if err := rows.Scan(&hashValue); err != nil {
			return fmt.Errorf("failed to scan hash value for hashlist %d: %w", hashlistID, err)
		}

		// Call callback for each hash value
		if err := callback(hashValue); err != nil {
			return fmt.Errorf("callback error for hashlist %d: %w", hashlistID, err)
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating hash values for hashlist %d: %w", hashlistID, err)
	}

	return nil
}

// StreamUncrackedLMHashHalvesForHashlist streams unique 16-character halves for LM hashlists.
// LM hashes must be cracked as two separate 16-character halves, not as full 32-character hashes.
// This method extracts both first and second halves from each LM hash and returns only unique values
// to avoid sending duplicates (e.g., the blank constant aad3b435b51404ee will appear only once).
func (r *HashRepository) StreamUncrackedLMHashHalvesForHashlist(
	ctx context.Context,
	hashlistID int64,
	callback func(hashHalf string) error,
) error {
	query := `
		SELECT DISTINCT half
		FROM (
			SELECT SUBSTRING(h.hash_value, 1, 16) AS half
			FROM hashes h
			INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = $1 AND h.is_cracked = FALSE
			UNION
			SELECT SUBSTRING(h.hash_value, 17, 16) AS half
			FROM hashes h
			INNER JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = $1 AND h.is_cracked = FALSE
		) AS halves
		ORDER BY half
	`

	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to query LM hash halves for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var hashHalf string
		if err := rows.Scan(&hashHalf); err != nil {
			return fmt.Errorf("failed to scan LM hash half for hashlist %d: %w", hashlistID, err)
		}

		// Call callback for each unique hash half
		if err := callback(hashHalf); err != nil {
			return fmt.Errorf("callback error for hashlist %d: %w", hashlistID, err)
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating LM hash halves for hashlist %d: %w", hashlistID, err)
	}

	return nil
}

// GetHashlistIDsForHash returns all hashlist IDs that contain a specific hash.
// This is used to determine which hashlists need their counters updated when a hash is cracked.
func (r *HashRepository) GetHashlistIDsForHash(ctx context.Context, hashID uuid.UUID) ([]int64, error) {
	query := `
		SELECT hashlist_id
		FROM hashlist_hashes
		WHERE hash_id = $1
	`

	rows, err := r.db.QueryContext(ctx, query, hashID)
	if err != nil {
		return nil, fmt.Errorf("failed to query hashlists for hash %s: %w", hashID, err)
	}
	defer rows.Close()

	var hashlistIDs []int64
	for rows.Next() {
		var hashlistID int64
		if err := rows.Scan(&hashlistID); err != nil {
			return nil, fmt.Errorf("failed to scan hashlist ID for hash %s: %w", hashID, err)
		}
		hashlistIDs = append(hashlistIDs, hashlistID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hashlist rows for hash %s: %w", hashID, err)
	}

	return hashlistIDs, nil
}

// BatchLinkHashes creates links between pairs of hashes in bulk
func (r *HashRepository) BatchLinkHashes(ctx context.Context, links []struct {
	HashID1  uuid.UUID
	HashID2  uuid.UUID
	LinkType string
}) error {
	if len(links) == 0 {
		return nil
	}

	query := `
		INSERT INTO linked_hashes (hash_id_1, hash_id_2, link_type, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (hash_id_1, hash_id_2) DO NOTHING
	`

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for batch link hashes: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch link hashes statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, link := range links {
		_, err := stmt.ExecContext(ctx, link.HashID1, link.HashID2, link.LinkType, now)
		if err != nil {
			return fmt.Errorf("failed to link hashes %s and %s: %w", link.HashID1, link.HashID2, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch link hashes transaction: %w", err)
	}

	return nil
}

// GetLinkedHash returns the linked hash for a given hash ID
func (r *HashRepository) GetLinkedHash(ctx context.Context, hashID uuid.UUID, linkType string) (*models.Hash, error) {
	// Check both directions: either hash_id_1 or hash_id_2 could be our ID
	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.password, h.is_cracked,
		       h.username, h.domain, h.hash_type_id, h.last_updated
		FROM hashes h
		INNER JOIN linked_hashes lh ON (
			(lh.hash_id_1 = $1 AND lh.hash_id_2 = h.id) OR
			(lh.hash_id_2 = $1 AND lh.hash_id_1 = h.id)
		)
		WHERE lh.link_type = $2
		LIMIT 1
	`

	var hash models.Hash
	var originalHash sql.NullString
	var password sql.NullString
	var username sql.NullString
	var domain sql.NullString

	err := r.db.QueryRowContext(ctx, query, hashID, linkType).Scan(
		&hash.ID,
		&hash.HashValue,
		&originalHash,
		&password,
		&hash.IsCracked,
		&username,
		&domain,
		&hash.HashTypeID,
		&hash.LastUpdated,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No linked hash found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get linked hash for %s: %w", hashID, err)
	}

	// Handle nullable fields
	if originalHash.Valid {
		hash.OriginalHash = originalHash.String
	}
	if password.Valid {
		hash.Password = &password.String
	}
	if username.Valid {
		hash.Username = &username.String
	}
	if domain.Valid {
		hash.Domain = &domain.String
	}

	return &hash, nil
}
