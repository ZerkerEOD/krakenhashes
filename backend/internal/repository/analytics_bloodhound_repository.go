package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/bloodhound"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// jsonbArg returns a value suitable for a JSONB parameter: nil (SQL NULL) for empty input, or the
// JSON text otherwise (PostgreSQL infers the jsonb type from the column and parses it).
func jsonbArg(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// CreateWithBloodhound inserts a new analytics report AND its derived BloodHound context in a single
// atomic INSERT. This is the crux of the never-persist design: the report row and its staged context
// land together, so the async queue never observes a queued report without its context (no race).
func (r *AnalyticsRepository) CreateWithBloodhound(ctx context.Context, report *models.AnalyticsReport, dc *bloodhound.DerivedContext) error {
	var ctxJSON []byte
	if dc != nil {
		b, err := json.Marshal(dc)
		if err != nil {
			return fmt.Errorf("failed to marshal bloodhound context: %w", err)
		}
		ctxJSON = b
	}

	query := `
		INSERT INTO analytics_reports (
			id, client_id, user_id, start_date, end_date, status,
			analytics_data, total_hashlists, total_hashes, total_cracked,
			queue_position, custom_patterns, hashlist_ids, created_at, bloodhound_context
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
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
		report.HashlistIDs,
		report.CreatedAt,
		jsonbArg(ctxJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to create analytics report with bloodhound context: %w", err)
	}
	return nil
}

// GetBloodhoundContext returns the staged derived context for a report, or (nil, nil) when the
// column is NULL (no dump was uploaded, or it has already been cleared). It is intentionally the
// ONLY read path for this column — the general report SELECTs omit it so it can never leak.
func (r *AnalyticsRepository) GetBloodhoundContext(ctx context.Context, id uuid.UUID) (*bloodhound.DerivedContext, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT bloodhound_context FROM analytics_reports WHERE id = $1`, id).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("analytics report with ID %s not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get bloodhound context for %s: %w", id, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var dc bloodhound.DerivedContext
	if err := json.Unmarshal(raw, &dc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bloodhound context for %s: %w", id, err)
	}
	return &dc, nil
}

// ClearBloodhoundContext sets the staged context to NULL. Called after a report finishes generating
// so BloodHound-derived data does not linger.
func (r *AnalyticsRepository) ClearBloodhoundContext(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE analytics_reports SET bloodhound_context = NULL WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to clear bloodhound context for %s: %w", id, err)
	}
	return nil
}

// SweepStaleBloodhoundContexts is the deletion backstop: it NULLs any staged context whose report
// has completed, or has been failed longer than failedTTL. This guarantees no derived AD data
// lingers even if a per-report clear was missed or the process crashed mid-generation.
func (r *AnalyticsRepository) SweepStaleBloodhoundContexts(ctx context.Context, failedTTL time.Duration) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE analytics_reports
		SET bloodhound_context = NULL
		WHERE bloodhound_context IS NOT NULL
		  AND (status = 'completed'
		       OR (status = 'failed' AND COALESCE(started_at, created_at) < now() - $1::interval))
	`, fmt.Sprintf("%d seconds", int64(failedTTL.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("failed to sweep stale bloodhound contexts: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetHashlistAccountRefs returns the distinct (username, domain) identities of ALL hashes (cracked
// or not) in the given hashlists. Used at upload time to seed the in-scope set so the parser retains
// facts only for accounts relevant to the engagement.
func (r *AnalyticsRepository) GetHashlistAccountRefs(ctx context.Context, hashlistIDs []int64) ([]models.AccountRef, error) {
	return r.accountRefs(ctx, hashlistIDs, false)
}

// GetCrackedAccountRefs returns the distinct (username, domain) identities of CRACKED hashes in the
// given hashlists. Used at enrichment time to determine which in-scope accounts were compromised.
func (r *AnalyticsRepository) GetCrackedAccountRefs(ctx context.Context, hashlistIDs []int64) ([]models.AccountRef, error) {
	return r.accountRefs(ctx, hashlistIDs, true)
}

func (r *AnalyticsRepository) accountRefs(ctx context.Context, hashlistIDs []int64, crackedOnly bool) ([]models.AccountRef, error) {
	if len(hashlistIDs) == 0 {
		return nil, nil
	}
	query := `
		SELECT DISTINCT h.username, h.domain
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		WHERE hh.hashlist_id = ANY($1)
		  AND h.username IS NOT NULL AND h.username <> ''`
	if crackedOnly {
		query += ` AND h.is_cracked = true`
	}

	rows, err := r.db.QueryContext(ctx, query, pq.Array(hashlistIDs))
	if err != nil {
		return nil, fmt.Errorf("failed to query account refs: %w", err)
	}
	defer rows.Close()

	var refs []models.AccountRef
	for rows.Next() {
		var username string
		var domain sql.NullString
		if err := rows.Scan(&username, &domain); err != nil {
			return nil, fmt.Errorf("failed to scan account ref: %w", err)
		}
		ref := models.AccountRef{Username: username}
		if domain.Valid {
			d := domain.String
			ref.Domain = &d
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating account refs: %w", err)
	}
	return refs, nil
}
