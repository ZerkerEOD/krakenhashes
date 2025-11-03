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

// AnalyticsRepository handles database operations for analytics reports
type AnalyticsRepository struct {
	db *db.DB
}

// NewAnalyticsRepository creates a new instance of AnalyticsRepository
func NewAnalyticsRepository(database *db.DB) *AnalyticsRepository {
	return &AnalyticsRepository{db: database}
}

// Create inserts a new analytics report into the database
func (r *AnalyticsRepository) Create(ctx context.Context, report *models.AnalyticsReport) error {
	query := `
		INSERT INTO analytics_reports (
			id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := r.db.ExecContext(ctx, query,
		report.ID,
		report.ClientID,
		report.UserID,
		report.StartDate,
		report.EndDate,
		report.Status,
		report.AnalyticsData,
		report.TotalHashlists,
		report.TotalHashes,
		report.TotalCracked,
		report.QueuePosition,
		report.CustomPatterns,
		report.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create analytics report: %w", err)
	}

	return nil
}

// GetByID retrieves an analytics report by its ID
func (r *AnalyticsRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.AnalyticsReport, error) {
	query := `
		SELECT id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, created_at, started_at, completed_at, error_message
		FROM analytics_reports
		WHERE id = $1
	`

	var report models.AnalyticsReport
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&report.ID,
		&report.ClientID,
		&report.UserID,
		&report.StartDate,
		&report.EndDate,
		&report.Status,
		&report.AnalyticsData,
		&report.TotalHashlists,
		&report.TotalHashes,
		&report.TotalCracked,
		&report.QueuePosition,
		&report.CustomPatterns,
		&report.CreatedAt,
		&report.StartedAt,
		&report.CompletedAt,
		&report.ErrorMessage,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get analytics report by ID %s: %w", id, err)
	}

	return &report, nil
}

// GetByClient retrieves all analytics reports for a specific client
func (r *AnalyticsRepository) GetByClient(ctx context.Context, clientID uuid.UUID) ([]*models.AnalyticsReport, error) {
	query := `
		SELECT id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, created_at, started_at, completed_at, error_message
		FROM analytics_reports
		WHERE client_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to query analytics reports for client %s: %w", clientID, err)
	}
	defer rows.Close()

	reports := make([]*models.AnalyticsReport, 0)
	for rows.Next() {
		var report models.AnalyticsReport
		err := rows.Scan(
			&report.ID,
			&report.ClientID,
			&report.UserID,
			&report.StartDate,
			&report.EndDate,
			&report.Status,
			&report.AnalyticsData,
			&report.TotalHashlists,
			&report.TotalHashes,
			&report.TotalCracked,
			&report.QueuePosition,
			&report.CustomPatterns,
			&report.CreatedAt,
			&report.StartedAt,
			&report.CompletedAt,
			&report.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan analytics report row: %w", err)
		}
		reports = append(reports, &report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating analytics report rows: %w", err)
	}

	return reports, nil
}

// UpdateStatus updates the status of an analytics report
func (r *AnalyticsRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `
		UPDATE analytics_reports
		SET status = $1,
			started_at = CASE WHEN $1::VARCHAR = 'processing' THEN NOW() ELSE started_at END,
			completed_at = CASE WHEN $1::VARCHAR IN ('completed', 'failed') THEN NOW() ELSE completed_at END
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update analytics report status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// UpdateAnalyticsData updates the analytics data for a report
func (r *AnalyticsRepository) UpdateAnalyticsData(ctx context.Context, id uuid.UUID, data *models.AnalyticsData) error {
	query := `
		UPDATE analytics_reports
		SET analytics_data = $1
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, data, id)
	if err != nil {
		return fmt.Errorf("failed to update analytics data: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// UpdateError updates the error message for a failed report
func (r *AnalyticsRepository) UpdateError(ctx context.Context, id uuid.UUID, errorMsg string) error {
	query := `
		UPDATE analytics_reports
		SET error_message = $1
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, errorMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update error message: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// UpdateQueuePosition updates the queue position for a specific report
func (r *AnalyticsRepository) UpdateQueuePosition(ctx context.Context, id uuid.UUID, position int) error {
	query := `
		UPDATE analytics_reports
		SET queue_position = $1
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, position, id)
	if err != nil {
		return fmt.Errorf("failed to update queue position: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// Delete removes an analytics report from the database
func (r *AnalyticsRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM analytics_reports WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete analytics report: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// GetQueuedReports retrieves all reports that are queued for processing
func (r *AnalyticsRepository) GetQueuedReports(ctx context.Context) ([]*models.AnalyticsReport, error) {
	query := `
		SELECT id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, created_at, started_at, completed_at, error_message
		FROM analytics_reports
		WHERE status = 'queued'
		ORDER BY queue_position ASC, created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query queued analytics reports: %w", err)
	}
	defer rows.Close()

	var reports []*models.AnalyticsReport
	for rows.Next() {
		var report models.AnalyticsReport
		err := rows.Scan(
			&report.ID,
			&report.ClientID,
			&report.UserID,
			&report.StartDate,
			&report.EndDate,
			&report.Status,
			&report.AnalyticsData,
			&report.TotalHashlists,
			&report.TotalHashes,
			&report.TotalCracked,
			&report.QueuePosition,
			&report.CustomPatterns,
			&report.CreatedAt,
			&report.StartedAt,
			&report.CompletedAt,
			&report.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queued report row: %w", err)
		}
		reports = append(reports, &report)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating queued report rows: %w", err)
	}

	return reports, nil
}

// GetNextQueuePosition returns the next available queue position
func (r *AnalyticsRepository) GetNextQueuePosition(ctx context.Context) (int, error) {
	query := `
		SELECT COALESCE(MAX(queue_position), 0) + 1
		FROM analytics_reports
		WHERE status = 'queued'
	`

	var position int
	err := r.db.QueryRowContext(ctx, query).Scan(&position)
	if err != nil {
		return 0, fmt.Errorf("failed to get next queue position: %w", err)
	}

	return position, nil
}

// UpdateQueuePositions recalculates queue positions for all queued reports
func (r *AnalyticsRepository) UpdateQueuePositions(ctx context.Context) error {
	query := `
		UPDATE analytics_reports
		SET queue_position = subquery.new_position
		FROM (
			SELECT id, ROW_NUMBER() OVER (ORDER BY queue_position ASC, created_at ASC) as new_position
			FROM analytics_reports
			WHERE status = 'queued'
		) AS subquery
		WHERE analytics_reports.id = subquery.id
	`

	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to update queue positions: %w", err)
	}

	return nil
}

// GetHashlistsByClientAndDateRange retrieves hashlists for a client within a date range
func (r *AnalyticsRepository) GetHashlistsByClientAndDateRange(ctx context.Context, clientID uuid.UUID, startDate, endDate time.Time) ([]int64, error) {
	query := `
		SELECT id
		FROM hashlists
		WHERE client_id = $1
		  AND created_at >= $2
		  AND created_at <= $3
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, clientID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query hashlists by date range: %w", err)
	}
	defer rows.Close()

	var hashlistIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan hashlist ID: %w", err)
		}
		hashlistIDs = append(hashlistIDs, id)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hashlist rows: %w", err)
	}

	return hashlistIDs, nil
}

// GetLinkedHashlistCount returns the number of linked hashlist pairs
// This is used to calculate the "effective" hashlist count for analytics
// When hashlists are linked (e.g., LM-NTLM pairs), they should count as ONE hashlist
func (r *AnalyticsRepository) GetLinkedHashlistCount(ctx context.Context, hashlistIDs []int64) (int, error) {
	if len(hashlistIDs) == 0 {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM linked_hashlists
		WHERE hashlist_id_1 = ANY($1) AND hashlist_id_2 = ANY($1)
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get linked hashlist count: %w", err)
	}

	return count, nil
}

// GetCrackedPasswordsByHashlists retrieves all cracked passwords from the specified hashlists
// Uses effective hash logic: linked pairs return only ONE password (prefer NTLM, fallback to LM)
func (r *AnalyticsRepository) GetCrackedPasswordsByHashlists(ctx context.Context, hashlistIDs []int64) ([]*models.Hash, error) {
	if len(hashlistIDs) == 0 {
		return []*models.Hash{}, nil
	}

	// Use effective hashes query to avoid double-counting linked pairs
	query := `
		WITH linked_pairs AS (
			-- For linked pairs, return ONE entry if either hash is cracked
			SELECT DISTINCT ON (lh.id)
				h2.id,
				h2.hash_value,
				h2.original_hash,
				COALESCE(h2.username, h1.username) as username,
				1000 as hash_type_id,
				true as is_cracked,
				COALESCE(h2.password, h1.password) as password,
				GREATEST(h1.last_updated, h2.last_updated) as last_updated
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000
			WHERE lh.link_type = 'lm_ntlm'
			  AND (h1.is_cracked = true OR h2.is_cracked = true)
			  AND (h1.password IS NOT NULL OR h2.password IS NOT NULL)
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- Standalone cracked hashes
			SELECT
				h.id,
				h.hash_value,
				h.original_hash,
				h.username,
				h.hash_type_id,
				h.is_cracked,
				h.password,
				h.last_updated
			FROM hashes h
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND h.is_cracked = true
			  AND h.password IS NOT NULL
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		)
		SELECT * FROM linked_pairs
		UNION ALL
		SELECT * FROM standalone_hashes
		ORDER BY password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked passwords: %w", err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash row: %w", err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash rows: %w", err)
	}

	return hashes, nil
}

// GetEffectiveHashes retrieves all hashes with linked pairs deduplicated
// Linked LM/NTLM pairs (from linked_hashes table) count as ONE entry
// Standalone hashes count as individual entries
// This ensures analytics don't double-count hashes that were linked during upload
func (r *AnalyticsRepository) GetEffectiveHashes(ctx context.Context, hashlistIDs []int64) ([]*models.Hash, error) {
	if len(hashlistIDs) == 0 {
		return []*models.Hash{}, nil
	}

	query := `
		WITH linked_pairs AS (
			-- For explicitly linked LM/NTLM pairs (created during pwdump upload)
			-- Create ONE effective entry per linked pair
			SELECT DISTINCT ON (lh.id)
				h2.id,
				h2.hash_value,
				h2.original_hash,
				COALESCE(h2.username, h1.username) as username,
				COALESCE(h2.domain, h1.domain) as domain,
				1000 as hash_type_id,  -- Represent linked pairs as NTLM
				(h1.is_cracked OR h2.is_cracked) as is_cracked,  -- Either/or logic
				COALESCE(h2.password, h1.password) as password,  -- Prefer NTLM password
				GREATEST(h1.last_updated, h2.last_updated) as last_updated
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000  -- LM
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000  -- NTLM
			WHERE lh.link_type = 'lm_ntlm'
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- All hashes NOT in linked_hashes table
			-- This includes hashes from separate uploads even if usernames match
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
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		)
		SELECT * FROM linked_pairs
		UNION ALL
		SELECT * FROM standalone_hashes
		ORDER BY username, hash_type_id
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query effective hashes: %w", err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan effective hash row: %w", err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating effective hash rows: %w", err)
	}

	return hashes, nil
}

// HashWithHashlist is a temporary struct for analytics queries that need hashlist tracking
type HashWithHashlist struct{
	Hash       models.Hash
	HashlistID int64
}

// GetCrackedPasswordsWithHashlists retrieves cracked passwords with hashlist tracking for reuse analysis
func (r *AnalyticsRepository) GetCrackedPasswordsWithHashlists(ctx context.Context, hashlistIDs []int64) ([]HashWithHashlist, error) {
	if len(hashlistIDs) == 0 {
		return []HashWithHashlist{}, nil
	}

	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.username,
			h.hash_type_id, h.is_cracked, h.password, h.last_updated,
			hh.hashlist_id
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		ORDER BY h.password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked passwords with hashlists: %w", err)
	}
	defer rows.Close()

	var results []HashWithHashlist
	for rows.Next() {
		var hwh HashWithHashlist
		err := rows.Scan(
			&hwh.Hash.ID,
			&hwh.Hash.HashValue,
			&hwh.Hash.OriginalHash,
			&hwh.Hash.Username,
			&hwh.Hash.HashTypeID,
			&hwh.Hash.IsCracked,
			&hwh.Hash.Password,
			&hwh.Hash.LastUpdated,
			&hwh.HashlistID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash with hashlist row: %w", err)
		}
		results = append(results, hwh)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash with hashlist rows: %w", err)
	}

	return results, nil
}

// GetJobTaskSpeedsByHashlists retrieves average speeds from job tasks related to the hashlists
func (r *AnalyticsRepository) GetJobTaskSpeedsByHashlists(ctx context.Context, hashlistIDs []int64) ([]int64, error) {
	if len(hashlistIDs) == 0 {
		return []int64{}, nil
	}

	query := `
		SELECT jt.average_speed
		FROM job_tasks jt
		JOIN job_executions je ON jt.job_execution_id = je.id
		WHERE je.hashlist_id = ANY($1)
		  AND jt.average_speed IS NOT NULL
		  AND jt.average_speed > 0
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query job task speeds: %w", err)
	}
	defer rows.Close()

	var speeds []int64
	for rows.Next() {
		var speed int64
		if err := rows.Scan(&speed); err != nil {
			return nil, fmt.Errorf("failed to scan speed: %w", err)
		}
		speeds = append(speeds, speed)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating speed rows: %w", err)
	}

	return speeds, nil
}

// GetHashlistsInfo retrieves hashlist information for the report
// Uses effective hash counting: linked LM/NTLM pairs count as ONE entry
func (r *AnalyticsRepository) GetHashlistsInfo(ctx context.Context, hashlistIDs []int64) (totalHashes, totalCracked int, err error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	// Use the same effective hashes logic to get accurate counts
	query := `
		WITH linked_pairs AS (
			-- Linked LM/NTLM pairs count as ONE entry
			SELECT DISTINCT ON (lh.id)
				(h1.is_cracked OR h2.is_cracked) as is_cracked
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000
			WHERE lh.link_type = 'lm_ntlm'
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- Standalone hashes count individually
			SELECT
				h.is_cracked
			FROM hashes h
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		),
		effective_hashes AS (
			SELECT * FROM linked_pairs
			UNION ALL
			SELECT * FROM standalone_hashes
		)
		SELECT
			COUNT(*) as total_hashes,
			COUNT(CASE WHEN is_cracked = true THEN 1 END) as total_cracked
		FROM effective_hashes
	`

	err = r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs)).Scan(&totalHashes, &totalCracked)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get hashlist info: %w", err)
	}

	return totalHashes, totalCracked, nil
}

// GetHashTypesByIDs retrieves hash type names for given IDs
func (r *AnalyticsRepository) GetHashTypesByIDs(ctx context.Context, hashTypeIDs []int) (map[int]string, error) {
	if len(hashTypeIDs) == 0 {
		return make(map[int]string), nil
	}

	query := `
		SELECT id, name
		FROM hash_types
		WHERE id = ANY($1)
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashTypeIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query hash types: %w", err)
	}
	defer rows.Close()

	hashTypes := make(map[int]string)
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("failed to scan hash type: %w", err)
		}
		hashTypes[id] = name
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash types: %w", err)
	}

	return hashTypes, nil
}

// List retrieves all analytics reports with pagination support
func (r *AnalyticsRepository) List(ctx context.Context, limit, offset int) ([]*models.AnalyticsReport, int, error) {
	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) FROM analytics_reports`
	err := r.db.QueryRowContext(ctx, countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count analytics reports: %w", err)
	}

	// Get paginated results
	query := `
		SELECT id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, created_at, started_at, completed_at, error_message
		FROM analytics_reports
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query analytics reports: %w", err)
	}
	defer rows.Close()

	var reports []*models.AnalyticsReport
	for rows.Next() {
		var report models.AnalyticsReport
		err := rows.Scan(
			&report.ID,
			&report.ClientID,
			&report.UserID,
			&report.StartDate,
			&report.EndDate,
			&report.Status,
			&report.AnalyticsData,
			&report.TotalHashlists,
			&report.TotalHashes,
			&report.TotalCracked,
			&report.QueuePosition,
			&report.CustomPatterns,
			&report.CreatedAt,
			&report.StartedAt,
			&report.CompletedAt,
			&report.ErrorMessage,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan analytics report row: %w", err)
		}
		reports = append(reports, &report)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating analytics report rows: %w", err)
	}

	return reports, total, nil
}

// UpdateSummaryFields updates the summary fields in the analytics report
func (r *AnalyticsRepository) UpdateSummaryFields(ctx context.Context, reportID uuid.UUID, totalHashlists, totalHashes, totalCracked int) error {
	query := `
		UPDATE analytics_reports
		SET total_hashlists = $1,
		    total_hashes = $2,
		    total_cracked = $3
		WHERE id = $4
	`

	result, err := r.db.ExecContext(ctx, query, totalHashlists, totalHashes, totalCracked, reportID)
	if err != nil {
		return fmt.Errorf("failed to update summary fields: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no report found with id %s", reportID)
	}

	return nil
}

// GetHashCountsByType retrieves total and cracked hash counts grouped by hash type
// Uses effective hash logic: linked pairs count as ONE entry with NTLM type
func (r *AnalyticsRepository) GetHashCountsByType(ctx context.Context, hashlistIDs []int64) (map[int]struct{ Total, Cracked int }, error) {
	if len(hashlistIDs) == 0 {
		return make(map[int]struct{ Total, Cracked int }), nil
	}

	// Use effective hashes to count by type (linked pairs appear as NTLM)
	query := `
		WITH linked_pairs AS (
			-- Linked pairs represented as NTLM (1000)
			SELECT DISTINCT ON (lh.id)
				1000 as hash_type_id,
				(h1.is_cracked OR h2.is_cracked) as is_cracked
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000
			WHERE lh.link_type = 'lm_ntlm'
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- Standalone hashes with original type
			SELECT
				h.hash_type_id,
				h.is_cracked
			FROM hashes h
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		),
		effective_hashes AS (
			SELECT * FROM linked_pairs
			UNION ALL
			SELECT * FROM standalone_hashes
		)
		SELECT
			hash_type_id,
			COUNT(*) as total,
			SUM(CASE WHEN is_cracked THEN 1 ELSE 0 END) as cracked
		FROM effective_hashes
		GROUP BY hash_type_id
		ORDER BY hash_type_id
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query hash counts by type: %w", err)
	}
	defer rows.Close()

	counts := make(map[int]struct{ Total, Cracked int })
	for rows.Next() {
		var hashTypeID int
		var total, cracked int
		if err := rows.Scan(&hashTypeID, &total, &cracked); err != nil {
			return nil, fmt.Errorf("failed to scan hash counts: %w", err)
		}
		counts[hashTypeID] = struct{ Total, Cracked int }{Total: total, Cracked: cracked}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash counts: %w", err)
	}

	return counts, nil
}

// GetDomainsByHashlists retrieves unique domains from hashes in the specified hashlists
func (r *AnalyticsRepository) GetDomainsByHashlists(ctx context.Context, hashlistIDs []int64) ([]string, error) {
	if len(hashlistIDs) == 0 {
		return []string{}, nil
	}

	query := `
		SELECT DISTINCT h.domain
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.domain IS NOT NULL
		  AND h.domain != ''
		ORDER BY h.domain
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query domains: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("failed to scan domain: %w", err)
		}
		domains = append(domains, domain)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating domain rows: %w", err)
	}

	return domains, nil
}

// GetDomainStats retrieves total and cracked hash counts for a specific domain
func (r *AnalyticsRepository) GetDomainStats(ctx context.Context, hashlistIDs []int64, domain string) (total, cracked int, err error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN h.is_cracked THEN 1 ELSE 0 END) as cracked
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.domain = $2
	`

	err = r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), domain).Scan(&total, &cracked)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get domain stats for domain %s: %w", domain, err)
	}

	return total, cracked, nil
}

// GetCrackedPasswordsByHashlistsAndDomain retrieves cracked passwords filtered by domain
func (r *AnalyticsRepository) GetCrackedPasswordsByHashlistsAndDomain(ctx context.Context, hashlistIDs []int64, domain string) ([]*models.Hash, error) {
	if len(hashlistIDs) == 0 {
		return []*models.Hash{}, nil
	}

	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.domain, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		  AND h.domain = $2
		ORDER BY h.password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked passwords for domain %s: %w", domain, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.Domain,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash row for domain %s: %w", domain, err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash rows for domain %s: %w", domain, err)
	}

	return hashes, nil
}

// GetCrackedPasswordsWithHashlistsAndDomain retrieves cracked passwords with hashlist tracking for a specific domain
func (r *AnalyticsRepository) GetCrackedPasswordsWithHashlistsAndDomain(ctx context.Context, hashlistIDs []int64, domain string) ([]HashWithHashlist, error) {
	if len(hashlistIDs) == 0 {
		return []HashWithHashlist{}, nil
	}

	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.username, h.domain,
			h.hash_type_id, h.is_cracked, h.password, h.last_updated,
			hh.hashlist_id
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		  AND h.domain = $2
		ORDER BY h.password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked passwords with hashlists for domain %s: %w", domain, err)
	}
	defer rows.Close()

	var results []HashWithHashlist
	for rows.Next() {
		var hwh HashWithHashlist
		err := rows.Scan(
			&hwh.Hash.ID,
			&hwh.Hash.HashValue,
			&hwh.Hash.OriginalHash,
			&hwh.Hash.Username,
			&hwh.Hash.Domain,
			&hwh.Hash.HashTypeID,
			&hwh.Hash.IsCracked,
			&hwh.Hash.Password,
			&hwh.Hash.LastUpdated,
			&hwh.HashlistID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash with hashlist row for domain %s: %w", domain, err)
		}
		results = append(results, hwh)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash with hashlist rows for domain %s: %w", domain, err)
	}

	return results, nil
}

// GetHashCountsByTypeDomain retrieves hash counts by type for a specific domain
func (r *AnalyticsRepository) GetHashCountsByTypeDomain(ctx context.Context, hashlistIDs []int64, domain string) (map[int]struct{ Total, Cracked int }, error) {
	if len(hashlistIDs) == 0 {
		return make(map[int]struct{ Total, Cracked int }), nil
	}

	query := `
		SELECT
			h.hash_type_id,
			COUNT(*) as total,
			SUM(CASE WHEN h.is_cracked THEN 1 ELSE 0 END) as cracked
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.domain = $2
		GROUP BY h.hash_type_id
		ORDER BY h.hash_type_id
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query hash counts by type for domain %s: %w", domain, err)
	}
	defer rows.Close()

	counts := make(map[int]struct{ Total, Cracked int })
	for rows.Next() {
		var hashTypeID int
		var total, cracked int
		if err := rows.Scan(&hashTypeID, &total, &cracked); err != nil {
			return nil, fmt.Errorf("failed to scan hash counts for domain %s: %w", domain, err)
		}
		counts[hashTypeID] = struct{ Total, Cracked int }{Total: total, Cracked: cracked}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash counts for domain %s: %w", domain, err)
	}

	return counts, nil
}

// GetWindowsHashCounts retrieves RAW hash counts for all Windows hash types
// Used for individual hash type cards - shows actual counts (10 NTLM, 9 LM)
func (r *AnalyticsRepository) GetWindowsHashCounts(ctx context.Context, hashlistIDs []int64) (map[int]struct{ Total, Cracked int }, error) {
	if len(hashlistIDs) == 0 {
		return make(map[int]struct{ Total, Cracked int }), nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000,  // NTLM
		1100,  // DCC/MS Cache
		2100,  // DCC2/MS Cache 2
		3000,  // LM
		5500,  // NetNTLMv1
		5600,  // NetNTLMv2
		7500,  // Kerberos 5 AS-REQ Pre-Auth (etype 23)
		13100, // Kerberos 5 TGS-REP (etype 23)
		18200, // Kerberos 5 AS-REP (etype 23)
		19600, // Kerberos 5 TGS-REP AES128 (etype 17)
		19700, // Kerberos 5 TGS-REP AES256 (etype 18)
		19800, // Kerberos 5 Pre-Auth AES128 (etype 17)
		19900, // Kerberos 5 Pre-Auth AES256 (etype 18)
		27000, // NetNTLMv1 (NT)
		27100, // NetNTLMv2 (NT)
		28800, // Kerberos 5 DB (etype 17)
		28900, // Kerberos 5 DB (etype 18)
	}

	// Raw counts - no effective hash logic
	query := `
		SELECT
			h.hash_type_id,
			COUNT(*) as total,
			COUNT(CASE WHEN h.is_cracked = true THEN 1 END) as cracked
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		GROUP BY h.hash_type_id
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes))
	if err != nil {
		return nil, fmt.Errorf("failed to query Windows hash counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[int]struct{ Total, Cracked int })
	for rows.Next() {
		var hashTypeID, total, cracked int
		if err := rows.Scan(&hashTypeID, &total, &cracked); err != nil {
			return nil, fmt.Errorf("failed to scan Windows hash count row: %w", err)
		}
		counts[hashTypeID] = struct{ Total, Cracked int }{Total: total, Cracked: cracked}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating Windows hash count rows: %w", err)
	}

	return counts, nil
}

// GetWindowsOverviewCounts retrieves EFFECTIVE hash counts for Windows Hash Overview section
// Uses linked-aware counting: linked LM/NTLM pairs count as ONE entry (9 pairs + 1 standalone = 10 total)
// If either LM or NTLM is cracked in a linked pair, the pair counts as cracked
func (r *AnalyticsRepository) GetWindowsOverviewCounts(ctx context.Context, hashlistIDs []int64) (total, cracked int, err error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000,  // NTLM
		1100,  // DCC/MS Cache
		2100,  // DCC2/MS Cache 2
		3000,  // LM
		5500,  // NetNTLMv1
		5600,  // NetNTLMv2
		7500,  // Kerberos 5 AS-REQ Pre-Auth (etype 23)
		13100, // Kerberos 5 TGS-REP (etype 23)
		18200, // Kerberos 5 AS-REP (etype 23)
		19600, // Kerberos 5 TGS-REP AES128 (etype 17)
		19700, // Kerberos 5 TGS-REP AES256 (etype 18)
		19800, // Kerberos 5 Pre-Auth AES128 (etype 17)
		19900, // Kerberos 5 Pre-Auth AES256 (etype 18)
		27000, // NetNTLMv1 (NT)
		27100, // NetNTLMv2 (NT)
		28800, // Kerberos 5 DB (etype 17)
		28900, // Kerberos 5 DB (etype 18)
	}

	query := `
		WITH linked_pairs AS (
			-- Linked LM/NTLM pairs count as ONE entry
			SELECT DISTINCT ON (lh.id)
				(h1.is_cracked OR h2.is_cracked) as is_cracked
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000
			WHERE lh.link_type = 'lm_ntlm'
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- Standalone Windows hashes (not linked)
			SELECT
				h.is_cracked
			FROM hashes h
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND h.hash_type_id = ANY($2)
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		),
		effective_hashes AS (
			SELECT * FROM linked_pairs
			UNION ALL
			SELECT * FROM standalone_hashes
		)
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN is_cracked = true THEN 1 END) as cracked
		FROM effective_hashes
	`

	err = r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes)).Scan(&total, &cracked)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query Windows overview counts: %w", err)
	}

	return total, cracked, nil
}

// GetWindowsOverviewCountsDomain retrieves EFFECTIVE hash counts for Windows Hash Overview section for a specific domain
// Uses linked-aware counting: linked LM/NTLM pairs count as ONE entry
// If either LM or NTLM is cracked in a linked pair, the pair counts as cracked
func (r *AnalyticsRepository) GetWindowsOverviewCountsDomain(ctx context.Context, hashlistIDs []int64, domain string) (total, cracked int, err error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000,  // NTLM
		1100,  // DCC/MS Cache
		2100,  // DCC2/MS Cache 2
		3000,  // LM
		5500,  // NetNTLMv1
		5600,  // NetNTLMv2
		7500,  // Kerberos 5 AS-REQ Pre-Auth (etype 23)
		13100, // Kerberos 5 TGS-REP (etype 23)
		18200, // Kerberos 5 AS-REP (etype 23)
		19600, // Kerberos 5 TGS-REP AES128 (etype 17)
		19700, // Kerberos 5 TGS-REP AES256 (etype 18)
		19800, // Kerberos 5 Pre-Auth AES128 (etype 17)
		19900, // Kerberos 5 Pre-Auth AES256 (etype 18)
		27000, // NetNTLMv1 (NT)
		27100, // NetNTLMv2 (NT)
		28800, // Kerberos 5 DB (etype 17)
		28900, // Kerberos 5 DB (etype 18)
	}

	query := `
		WITH linked_pairs AS (
			-- Linked LM/NTLM pairs count as ONE entry
			SELECT DISTINCT ON (lh.id)
				(h1.is_cracked OR h2.is_cracked) as is_cracked
			FROM linked_hashes lh
			JOIN hashes h1 ON lh.hash_id_1 = h1.id AND h1.hash_type_id = 3000
			JOIN hashes h2 ON lh.hash_id_2 = h2.id AND h2.hash_type_id = 1000
			WHERE lh.link_type = 'lm_ntlm'
			  AND COALESCE(h1.domain, h2.domain) = $3
			  AND EXISTS (
				  SELECT 1 FROM hashlist_hashes hh
				  WHERE (hh.hash_id = h1.id OR hh.hash_id = h2.id)
					AND hh.hashlist_id = ANY($1)
			  )
		),
		standalone_hashes AS (
			-- Standalone Windows hashes (not linked)
			SELECT
				h.is_cracked
			FROM hashes h
			JOIN hashlist_hashes hh ON h.id = hh.hash_id
			WHERE hh.hashlist_id = ANY($1)
			  AND h.hash_type_id = ANY($2)
			  AND h.domain = $3
			  AND NOT EXISTS (
				  SELECT 1 FROM linked_hashes lh
				  WHERE (lh.hash_id_1 = h.id OR lh.hash_id_2 = h.id)
					AND lh.link_type = 'lm_ntlm'
			  )
		),
		effective_hashes AS (
			SELECT * FROM linked_pairs
			UNION ALL
			SELECT * FROM standalone_hashes
		)
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN is_cracked = true THEN 1 END) as cracked
		FROM effective_hashes
	`

	err = r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes), domain).Scan(&total, &cracked)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query Windows overview counts for domain %s: %w", domain, err)
	}

	return total, cracked, nil
}

// GetLMPartialCrackCount returns the number of partially cracked LM hashes
func (r *AnalyticsRepository) GetLMPartialCrackCount(ctx context.Context, hashlistIDs []int64) (int, error) {
	if len(hashlistIDs) == 0 {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM lm_hash_metadata lm
		JOIN hashes h ON lm.hash_id = h.id
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND ((lm.first_half_cracked = true AND lm.second_half_cracked = false)
		    OR (lm.first_half_cracked = false AND lm.second_half_cracked = true))
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get LM partial crack count: %w", err)
	}

	return count, nil
}

// GetLMPasswordLengths returns the length distribution based on LM hash structure
// LM hashes are 32 hex chars (16 bytes). If the second half (last 16 chars) is 'aad3b435b51404ee',
// the password is ≤7 characters. Otherwise, it's 8-14 characters.
func (r *AnalyticsRepository) GetLMPasswordLengths(ctx context.Context, hashlistIDs []int64) (underEight, eightToFourteen int, error error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	query := `
		SELECT
			COUNT(CASE WHEN LOWER(SUBSTRING(h.hash_value, 17, 16)) = 'aad3b435b51404ee' THEN 1 END) as under_eight,
			COUNT(CASE WHEN LOWER(SUBSTRING(h.hash_value, 17, 16)) != 'aad3b435b51404ee' THEN 1 END) as eight_to_fourteen
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = 3000
	`

	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs)).Scan(&underEight, &eightToFourteen)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get LM password lengths: %w", err)
	}

	return underEight, eightToFourteen, nil
}

// GetWindowsUniqueUserCount returns the count of distinct usernames across all Windows hash types
func (r *AnalyticsRepository) GetWindowsUniqueUserCount(ctx context.Context, hashlistIDs []int64) (int, error) {
	if len(hashlistIDs) == 0 {
		return 0, nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000, 1100, 2100, 3000, 5500, 5600, 7500, 13100, 18200,
		19600, 19700, 19800, 19900, 27000, 27100, 28800, 28900,
	}

	query := `
		SELECT COUNT(DISTINCT h.username) as unique_users
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		  AND h.username IS NOT NULL
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get Windows unique user count: %w", err)
	}

	return count, nil
}

// GetLinkedHashCorrelation returns correlation statistics for linked LM/NTLM hashes
func (r *AnalyticsRepository) GetLinkedHashCorrelation(ctx context.Context, hashlistIDs []int64) (both, onlyNTLM, onlyLM, neither int, error error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, 0, 0, nil
	}

	query := `
		SELECT
			COUNT(CASE
				WHEN h1.is_cracked = true AND h2.is_cracked = true THEN 1
			END) as both_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = false AND h2.is_cracked = true THEN 1
			END) as only_ntlm_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = true AND h2.is_cracked = false THEN 1
			END) as only_lm_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = false AND h2.is_cracked = false THEN 1
			END) as neither_cracked
		FROM linked_hashes lh
		JOIN hashes h1 ON lh.hash_id_1 = h1.id
		JOIN hashes h2 ON lh.hash_id_2 = h2.id
		JOIN hashlist_hashes hh1 ON h1.id = hh1.hash_id
		WHERE hh1.hashlist_id = ANY($1)
		  AND lh.link_type = 'lm_ntlm'
		  AND h1.hash_type_id = 3000
		  AND h2.hash_type_id = 1000
	`

	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs)).Scan(&both, &onlyNTLM, &onlyLM, &neither)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to get linked hash correlation: %w", err)
	}

	return both, onlyNTLM, onlyLM, neither, nil
}

// GetLMPartialCracks retrieves details about partially cracked LM hashes
func (r *AnalyticsRepository) GetLMPartialCracks(ctx context.Context, hashlistIDs []int64, limit int) ([]models.LMPartialCrackDetail, int, int, error) {
	if len(hashlistIDs) == 0 {
		return []models.LMPartialCrackDetail{}, 0, 0, nil
	}

	query := `
		SELECT
			h.username,
			h.domain,
			lm.first_half_cracked,
			lm.first_half_password,
			lm.second_half_cracked,
			lm.second_half_password,
			hl.name as hashlist_name
		FROM lm_hash_metadata lm
		JOIN hashes h ON lm.hash_id = h.id
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN hashlists hl ON hh.hashlist_id = hl.id
		WHERE hh.hashlist_id = ANY($1)
		  AND ((lm.first_half_cracked = true AND lm.second_half_cracked = false)
		    OR (lm.first_half_cracked = false AND lm.second_half_cracked = true))
		ORDER BY h.username, h.domain
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), limit)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to query LM partial cracks: %w", err)
	}
	defer rows.Close()

	var details []models.LMPartialCrackDetail
	firstHalfOnly := 0
	secondHalfOnly := 0

	for rows.Next() {
		var detail models.LMPartialCrackDetail
		err := rows.Scan(
			&detail.Username,
			&detail.Domain,
			&detail.FirstHalfCracked,
			&detail.FirstHalfPwd,
			&detail.SecondHalfCracked,
			&detail.SecondHalfPwd,
			&detail.HashlistName,
		)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to scan LM partial crack row: %w", err)
		}

		// Count which half is cracked
		if detail.FirstHalfCracked && !detail.SecondHalfCracked {
			firstHalfOnly++
		} else if !detail.FirstHalfCracked && detail.SecondHalfCracked {
			secondHalfOnly++
		}

		details = append(details, detail)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("error iterating LM partial crack rows: %w", err)
	}

	return details, firstHalfOnly, secondHalfOnly, nil
}

// GetCrackedLMPasswords retrieves all cracked LM passwords for mask generation
func (r *AnalyticsRepository) GetCrackedLMPasswords(ctx context.Context, hashlistIDs []int64) ([]*models.Hash, error) {
	if len(hashlistIDs) == 0 {
		return []*models.Hash{}, nil
	}

	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = 3000
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		ORDER BY h.password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked LM passwords: %w", err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan LM password row: %w", err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating LM password rows: %w", err)
	}

	return hashes, nil
}

// GetHashesGroupedByHashValue retrieves hashes grouped by hash_value for hash reuse analysis
func (r *AnalyticsRepository) GetHashesGroupedByHashValue(ctx context.Context, hashlistIDs []int64, hashTypes []int) ([]HashWithHashlist, error) {
	if len(hashlistIDs) == 0 {
		return []HashWithHashlist{}, nil
	}

	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.username,
			h.hash_type_id, h.is_cracked, h.password, h.last_updated,
			hh.hashlist_id
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		ORDER BY h.hash_value, h.username
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), pq.Array(hashTypes))
	if err != nil {
		return nil, fmt.Errorf("failed to query hashes grouped by hash value: %w", err)
	}
	defer rows.Close()

	var results []HashWithHashlist
	for rows.Next() {
		var hwh HashWithHashlist
		err := rows.Scan(
			&hwh.Hash.ID,
			&hwh.Hash.HashValue,
			&hwh.Hash.OriginalHash,
			&hwh.Hash.Username,
			&hwh.Hash.HashTypeID,
			&hwh.Hash.IsCracked,
			&hwh.Hash.Password,
			&hwh.Hash.LastUpdated,
			&hwh.HashlistID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash with hashlist row: %w", err)
		}
		results = append(results, hwh)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash with hashlist rows: %w", err)
	}

	return results, nil
}

// ==================== Domain-Specific Windows Hash Methods ====================

// GetWindowsHashCountsDomain retrieves hash counts for Windows hash types filtered by domain
func (r *AnalyticsRepository) GetWindowsHashCountsDomain(ctx context.Context, hashlistIDs []int64, domain string) (map[int]struct{ Total, Cracked int }, error) {
	if len(hashlistIDs) == 0 {
		return make(map[int]struct{ Total, Cracked int }), nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000, 1100, 2100, 3000, 5500, 5600, 7500, 13100, 18200,
		19600, 19700, 19800, 19900, 27000, 27100, 28800, 28900,
	}

	query := `
		SELECT
			h.hash_type_id,
			COUNT(*) as total,
			COUNT(CASE WHEN h.is_cracked = true THEN 1 END) as cracked
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		  AND h.domain = $3
		GROUP BY h.hash_type_id
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query Windows hash counts for domain %s: %w", domain, err)
	}
	defer rows.Close()

	counts := make(map[int]struct{ Total, Cracked int })
	for rows.Next() {
		var hashTypeID, total, cracked int
		if err := rows.Scan(&hashTypeID, &total, &cracked); err != nil {
			return nil, fmt.Errorf("failed to scan Windows hash count row for domain %s: %w", domain, err)
		}
		counts[hashTypeID] = struct{ Total, Cracked int }{Total: total, Cracked: cracked}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating Windows hash count rows for domain %s: %w", domain, err)
	}

	return counts, nil
}

// GetLMPartialCrackCountDomain returns the number of partially cracked LM hashes for a specific domain
func (r *AnalyticsRepository) GetLMPartialCrackCountDomain(ctx context.Context, hashlistIDs []int64, domain string) (int, error) {
	if len(hashlistIDs) == 0 {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM lm_hash_metadata lm
		JOIN hashes h ON lm.hash_id = h.id
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.domain = $2
		  AND ((lm.first_half_cracked = true AND lm.second_half_cracked = false)
		    OR (lm.first_half_cracked = false AND lm.second_half_cracked = true))
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), domain).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get LM partial crack count for domain %s: %w", domain, err)
	}

	return count, nil
}

// GetLMPasswordLengthsDomain returns the length distribution based on LM hash structure for a specific domain
// LM hashes are 32 hex chars (16 bytes). If the second half (last 16 chars) is 'aad3b435b51404ee',
// the password is ≤7 characters. Otherwise, it's 8-14 characters.
func (r *AnalyticsRepository) GetLMPasswordLengthsDomain(ctx context.Context, hashlistIDs []int64, domain string) (underEight, eightToFourteen int, error error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, nil
	}

	query := `
		SELECT
			COUNT(CASE WHEN LOWER(SUBSTRING(h.hash_value, 17, 16)) = 'aad3b435b51404ee' THEN 1 END) as under_eight,
			COUNT(CASE WHEN LOWER(SUBSTRING(h.hash_value, 17, 16)) != 'aad3b435b51404ee' THEN 1 END) as eight_to_fourteen
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = 3000
		  AND h.domain = $2
	`

	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), domain).Scan(&underEight, &eightToFourteen)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get LM password lengths for domain %s: %w", domain, err)
	}

	return underEight, eightToFourteen, nil
}

// GetWindowsUniqueUserCountDomain returns the count of distinct usernames across Windows hash types for a specific domain
func (r *AnalyticsRepository) GetWindowsUniqueUserCountDomain(ctx context.Context, hashlistIDs []int64, domain string) (int, error) {
	if len(hashlistIDs) == 0 {
		return 0, nil
	}

	// Windows hash type IDs
	windowsHashTypes := []int{
		1000, 1100, 2100, 3000, 5500, 5600, 7500, 13100, 18200,
		19600, 19700, 19800, 19900, 27000, 27100, 28800, 28900,
	}

	query := `
		SELECT COUNT(DISTINCT h.username) as unique_users
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		  AND h.domain = $3
		  AND h.username IS NOT NULL
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), pq.Array(windowsHashTypes), domain).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get Windows unique user count for domain %s: %w", domain, err)
	}

	return count, nil
}

// GetLinkedHashCorrelationDomain returns correlation statistics for linked LM/NTLM hashes in a specific domain
func (r *AnalyticsRepository) GetLinkedHashCorrelationDomain(ctx context.Context, hashlistIDs []int64, domain string) (both, onlyNTLM, onlyLM, neither int, error error) {
	if len(hashlistIDs) == 0 {
		return 0, 0, 0, 0, nil
	}

	query := `
		SELECT
			COUNT(CASE
				WHEN h1.is_cracked = true AND h2.is_cracked = true THEN 1
			END) as both_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = false AND h2.is_cracked = true THEN 1
			END) as only_ntlm_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = true AND h2.is_cracked = false THEN 1
			END) as only_lm_cracked,
			COUNT(CASE
				WHEN h1.is_cracked = false AND h2.is_cracked = false THEN 1
			END) as neither_cracked
		FROM linked_hashes lh
		JOIN hashes h1 ON lh.hash_id_1 = h1.id
		JOIN hashes h2 ON lh.hash_id_2 = h2.id
		JOIN hashlist_hashes hh1 ON h1.id = hh1.hash_id
		WHERE hh1.hashlist_id = ANY($1)
		  AND h1.domain = $2
		  AND lh.link_type = 'lm_ntlm'
		  AND h1.hash_type_id = 3000
		  AND h2.hash_type_id = 1000
	`

	err := r.db.QueryRowContext(ctx, query, pq.Array(hashlistIDs), domain).Scan(&both, &onlyNTLM, &onlyLM, &neither)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to get linked hash correlation for domain %s: %w", domain, err)
	}

	return both, onlyNTLM, onlyLM, neither, nil
}

// GetLMPartialCracksDomain retrieves details about partially cracked LM hashes in a specific domain
func (r *AnalyticsRepository) GetLMPartialCracksDomain(ctx context.Context, hashlistIDs []int64, domain string, limit int) ([]models.LMPartialCrackDetail, int, int, error) {
	if len(hashlistIDs) == 0 {
		return []models.LMPartialCrackDetail{}, 0, 0, nil
	}

	query := `
		SELECT
			h.username,
			h.domain,
			lm.first_half_cracked,
			lm.first_half_password,
			lm.second_half_cracked,
			lm.second_half_password,
			hl.name as hashlist_name
		FROM lm_hash_metadata lm
		JOIN hashes h ON lm.hash_id = h.id
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN hashlists hl ON hh.hashlist_id = hl.id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.domain = $2
		  AND ((lm.first_half_cracked = true AND lm.second_half_cracked = false)
		    OR (lm.first_half_cracked = false AND lm.second_half_cracked = true))
		ORDER BY h.username, h.domain
		LIMIT $3
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), domain, limit)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to query LM partial cracks for domain %s: %w", domain, err)
	}
	defer rows.Close()

	var details []models.LMPartialCrackDetail
	firstHalfOnly := 0
	secondHalfOnly := 0

	for rows.Next() {
		var detail models.LMPartialCrackDetail
		err := rows.Scan(
			&detail.Username,
			&detail.Domain,
			&detail.FirstHalfCracked,
			&detail.FirstHalfPwd,
			&detail.SecondHalfCracked,
			&detail.SecondHalfPwd,
			&detail.HashlistName,
		)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to scan LM partial crack row for domain %s: %w", domain, err)
		}

		// Count which half is cracked
		if detail.FirstHalfCracked && !detail.SecondHalfCracked {
			firstHalfOnly++
		} else if !detail.FirstHalfCracked && detail.SecondHalfCracked {
			secondHalfOnly++
		}

		details = append(details, detail)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("error iterating LM partial crack rows for domain %s: %w", domain, err)
	}

	return details, firstHalfOnly, secondHalfOnly, nil
}

// GetCrackedLMPasswordsDomain retrieves all cracked LM passwords for mask generation in a specific domain
func (r *AnalyticsRepository) GetCrackedLMPasswordsDomain(ctx context.Context, hashlistIDs []int64, domain string) ([]*models.Hash, error) {
	if len(hashlistIDs) == 0 {
		return []*models.Hash{}, nil
	}

	query := `
		SELECT h.id, h.hash_value, h.original_hash, h.username, h.hash_type_id, h.is_cracked, h.password, h.last_updated
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = 3000
		  AND h.domain = $2
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		ORDER BY h.password
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query cracked LM passwords for domain %s: %w", domain, err)
	}
	defer rows.Close()

	var hashes []*models.Hash
	for rows.Next() {
		var hash models.Hash
		err := rows.Scan(
			&hash.ID,
			&hash.HashValue,
			&hash.OriginalHash,
			&hash.Username,
			&hash.HashTypeID,
			&hash.IsCracked,
			&hash.Password,
			&hash.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan LM password row for domain %s: %w", domain, err)
		}
		hashes = append(hashes, &hash)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating LM password rows for domain %s: %w", domain, err)
	}

	return hashes, nil
}

// GetHashesGroupedByHashValueDomain retrieves hashes grouped by hash_value for hash reuse analysis in a specific domain
func (r *AnalyticsRepository) GetHashesGroupedByHashValueDomain(ctx context.Context, hashlistIDs []int64, hashTypes []int, domain string) ([]HashWithHashlist, error) {
	if len(hashlistIDs) == 0 {
		return []HashWithHashlist{}, nil
	}

	query := `
		SELECT
			h.id, h.hash_value, h.original_hash, h.username,
			h.hash_type_id, h.is_cracked, h.password, h.last_updated,
			hh.hashlist_id
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.hash_type_id = ANY($2)
		  AND h.domain = $3
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		ORDER BY h.hash_value, h.username
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs), pq.Array(hashTypes), domain)
	if err != nil {
		return nil, fmt.Errorf("failed to query hashes grouped by hash value for domain %s: %w", domain, err)
	}
	defer rows.Close()

	var results []HashWithHashlist
	for rows.Next() {
		var hwh HashWithHashlist
		err := rows.Scan(
			&hwh.Hash.ID,
			&hwh.Hash.HashValue,
			&hwh.Hash.OriginalHash,
			&hwh.Hash.Username,
			&hwh.Hash.HashTypeID,
			&hwh.Hash.IsCracked,
			&hwh.Hash.Password,
			&hwh.Hash.LastUpdated,
			&hwh.HashlistID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan hash with hashlist row for domain %s: %w", domain, err)
		}
		results = append(results, hwh)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hash with hashlist rows for domain %s: %w", domain, err)
	}

	return results, nil
}
