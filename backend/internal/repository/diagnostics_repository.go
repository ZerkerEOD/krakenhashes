package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// DiagnosticsRepository persists deduplicated scheduling diagnostics. Writes
// are expected to arrive in batches from the buffered DiagnosticsService, not
// per-event, so the per-row UPSERT cost is amortized.
type DiagnosticsRepository struct {
	db *db.DB
}

// NewDiagnosticsRepository constructs a DiagnosticsRepository.
func NewDiagnosticsRepository(database *db.DB) *DiagnosticsRepository {
	return &DiagnosticsRepository{db: database}
}

// DiagUpsert is one diagnostic to write. CountDelta is added to the existing
// row's count (or seeds a new row), so callers send the number of occurrences
// accumulated since the last flush.
type DiagUpsert struct {
	Scope      string
	ScopeID    string
	ReasonCode string
	Severity   string
	Detail     string
	CountDelta int64
}

// DiagClear identifies a (scope, scope_id) whose active diagnostics should be
// marked cleared, optionally narrowed to a single reason code.
type DiagClear struct {
	Scope      string
	ScopeID    string
	ReasonCode string // empty = clear all active reasons for the scope
}

// UpsertBatch applies upserts and clears in a single transaction. Recurrence of
// an existing (scope, scope_id, reason_code) bumps count + last_seen and
// un-clears it; clears set cleared_at on currently-active rows.
func (r *DiagnosticsRepository) UpsertBatch(ctx context.Context, upserts []DiagUpsert, clears []DiagClear) error {
	if len(upserts) == 0 && len(clears) == 0 {
		return nil
	}
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, u := range upserts {
			if u.CountDelta <= 0 {
				u.CountDelta = 1
			}
			if u.Severity == "" {
				u.Severity = models.DiagSeverityInfo
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO scheduling_diagnostics
					(scope, scope_id, reason_code, severity, detail, count, first_seen, last_seen)
				VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
				ON CONFLICT (scope, scope_id, reason_code) DO UPDATE SET
					severity   = EXCLUDED.severity,
					detail     = EXCLUDED.detail,
					count      = scheduling_diagnostics.count + EXCLUDED.count,
					last_seen  = NOW(),
					cleared_at = NULL
			`, u.Scope, u.ScopeID, u.ReasonCode, u.Severity, u.Detail, u.CountDelta); err != nil {
				return fmt.Errorf("upsert diagnostic (%s/%s/%s): %w", u.Scope, u.ScopeID, u.ReasonCode, err)
			}
		}
		for _, c := range clears {
			if c.ReasonCode != "" {
				if _, err := tx.ExecContext(ctx, `
					UPDATE scheduling_diagnostics
					SET cleared_at = NOW()
					WHERE scope = $1 AND scope_id = $2 AND reason_code = $3 AND cleared_at IS NULL
				`, c.Scope, c.ScopeID, c.ReasonCode); err != nil {
					return fmt.Errorf("clear diagnostic (%s/%s/%s): %w", c.Scope, c.ScopeID, c.ReasonCode, err)
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE scheduling_diagnostics
				SET cleared_at = NOW()
				WHERE scope = $1 AND scope_id = $2 AND cleared_at IS NULL
			`, c.Scope, c.ScopeID); err != nil {
				return fmt.Errorf("clear diagnostics (%s/%s): %w", c.Scope, c.ScopeID, err)
			}
		}
		return nil
	})
}

// ListActiveByScope returns the active (uncleared) diagnostics for a scope,
// most-recent first. When `since` is non-zero, only rows refreshed at/after it
// are returned — a reason the scheduler stopped recording (agent started
// working, got disabled, etc.) freezes its last_seen and ages out of the view
// without needing an explicit clear.
func (r *DiagnosticsRepository) ListActiveByScope(ctx context.Context, scope, scopeID string, since time.Time) ([]models.SchedulingDiagnostic, error) {
	query := `
		SELECT id, scope, scope_id, reason_code, severity, detail, count, first_seen, last_seen, cleared_at
		FROM scheduling_diagnostics
		WHERE scope = $1 AND scope_id = $2 AND cleared_at IS NULL`
	args := []interface{}{scope, scopeID}
	if !since.IsZero() {
		query += ` AND last_seen >= $3`
		args = append(args, since)
	}
	query += ` ORDER BY last_seen DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list diagnostics for %s/%s: %w", scope, scopeID, err)
	}
	defer rows.Close()

	var out []models.SchedulingDiagnostic
	for rows.Next() {
		var d models.SchedulingDiagnostic
		if err := rows.Scan(&d.ID, &d.Scope, &d.ScopeID, &d.ReasonCode, &d.Severity,
			&d.Detail, &d.Count, &d.FirstSeen, &d.LastSeen, &d.ClearedAt); err != nil {
			return nil, fmt.Errorf("scan diagnostic: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
