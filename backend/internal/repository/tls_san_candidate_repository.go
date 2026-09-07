package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// TLSSANCandidate is one observed address that the server certificate does not
// currently cover.
type TLSSANCandidate struct {
	ID            int64      `json:"id"`
	Address       string     `json:"address"`
	Kind          string     `json:"kind"`
	Source        string     `json:"source"`
	FirstSeenAt   time.Time  `json:"first_seen_at"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	HitCount      int64      `json:"hit_count"`
	LastAgentID   *int       `json:"last_agent_id,omitempty"`
	LastAgentName *string    `json:"last_agent_name,omitempty"`
	LastUserAgent *string    `json:"last_user_agent,omitempty"`
	LastPort      *int       `json:"last_port,omitempty"`
	DismissedAt   *time.Time `json:"dismissed_at,omitempty"`
}

// TLSSANCandidateObservation is one aggregated sighting, ready to be upserted.
type TLSSANCandidateObservation struct {
	Address       string
	Kind          string
	Source        string
	Hits          int64
	LastSeenAt    time.Time
	LastAgentID   *int
	LastAgentName string
	LastUserAgent string
	LastPort      int
}

// Retention bounds. The Host header is caller-controlled, so without a hard cap
// a scanner spraying random Host values would grow this table without limit.
const (
	sanCandidateRetention   = 30 * 24 * time.Hour
	sanCandidateMaxRetained = 200
)

// TLSSANCandidateRepository stores observed certificate-name candidates.
type TLSSANCandidateRepository struct {
	db *db.DB
}

func NewTLSSANCandidateRepository(database *db.DB) *TLSSANCandidateRepository {
	return &TLSSANCandidateRepository{db: database}
}

// Upsert records a batch of aggregated observations in one transaction.
//
// Called from the discovery cache's periodic flush rather than per-request, so
// the request hot path never touches the database.
func (r *TLSSANCandidateRepository) Upsert(ctx context.Context, observations []TLSSANCandidateObservation) error {
	if len(observations) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const query = `
		INSERT INTO tls_san_candidates (
			address, kind, source, first_seen_at, last_seen_at, hit_count,
			last_agent_id, last_agent_name, last_user_agent, last_port
		)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (address, source) DO UPDATE SET
			last_seen_at    = EXCLUDED.last_seen_at,
			hit_count       = tls_san_candidates.hit_count + EXCLUDED.hit_count,
			last_agent_id   = COALESCE(EXCLUDED.last_agent_id, tls_san_candidates.last_agent_id),
			last_agent_name = COALESCE(EXCLUDED.last_agent_name, tls_san_candidates.last_agent_name),
			last_user_agent = COALESCE(EXCLUDED.last_user_agent, tls_san_candidates.last_user_agent),
			last_port       = COALESCE(EXCLUDED.last_port, tls_san_candidates.last_port),
			-- A fresh sighting un-dismisses the row. An address an admin
			-- dismissed but that an agent is still failing on is worth raising
			-- again; silently swallowing it would hide a live problem.
			dismissed_at    = NULL,
			dismissed_by    = NULL`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare candidate upsert: %w", err)
	}
	defer stmt.Close()

	for _, o := range observations {
		if _, err := stmt.ExecContext(ctx,
			o.Address, o.Kind, o.Source, o.LastSeenAt, o.Hits,
			o.LastAgentID, nullString(o.LastAgentName), nullString(o.LastUserAgent), nullPort(o.LastPort),
		); err != nil {
			return fmt.Errorf("failed to upsert candidate %s: %w", o.Address, err)
		}
	}

	return tx.Commit()
}

// List returns candidates, newest sighting first, with agent-reported failures
// ranked above passive observations so the agent that cannot connect is the
// first row an administrator sees.
func (r *TLSSANCandidateRepository) List(ctx context.Context, includeDismissed bool) ([]TLSSANCandidate, error) {
	query := `
		SELECT id, address, kind, source, first_seen_at, last_seen_at, hit_count,
		       last_agent_id, last_agent_name, last_user_agent, last_port, dismissed_at
		FROM tls_san_candidates`
	if !includeDismissed {
		query += ` WHERE dismissed_at IS NULL`
	}
	query += `
		ORDER BY (source = 'agent_tls_failure') DESC, last_seen_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list SAN candidates: %w", err)
	}
	defer rows.Close()

	return scanCandidates(rows)
}

// ListRecentAgentFailures returns agent-reported failures seen within the given
// window, which is what drives the "an agent cannot verify this certificate"
// banner.
func (r *TLSSANCandidateRepository) ListRecentAgentFailures(ctx context.Context, within time.Duration) ([]TLSSANCandidate, error) {
	const query = `
		SELECT id, address, kind, source, first_seen_at, last_seen_at, hit_count,
		       last_agent_id, last_agent_name, last_user_agent, last_port, dismissed_at
		FROM tls_san_candidates
		WHERE dismissed_at IS NULL
		  AND source = 'agent_tls_failure'
		  AND last_seen_at > NOW() - $1::interval
		ORDER BY last_seen_at DESC`

	rows, err := r.db.QueryContext(ctx, query, fmt.Sprintf("%d seconds", int(within.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("failed to list recent agent TLS failures: %w", err)
	}
	defer rows.Close()

	return scanCandidates(rows)
}

// Dismiss hides a candidate from the default list.
func (r *TLSSANCandidateRepository) Dismiss(ctx context.Context, id int64, userID uuid.UUID) error {
	const query = `
		UPDATE tls_san_candidates
		SET dismissed_at = NOW(), dismissed_by = $2
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("failed to dismiss SAN candidate %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read dismiss result: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("SAN candidate %d not found: %w", id, ErrNotFound)
	}
	return nil
}

// DeleteByAddress removes every row for an address, used once it has been added
// to the certificate so it stops being suggested.
func (r *TLSSANCandidateRepository) DeleteByAddress(ctx context.Context, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	const query = `DELETE FROM tls_san_candidates WHERE address = ANY($1)`
	if _, err := r.db.ExecContext(ctx, query, pq.Array(addresses)); err != nil {
		return fmt.Errorf("failed to delete SAN candidates: %w", err)
	}
	return nil
}

// Prune enforces the retention window and the hard row cap.
//
// Eviction order matters: agent-reported failures are the actionable signal and
// are dropped last, and a failure attributed to a still-registered agent is never
// evicted by the cap at all.
func (r *TLSSANCandidateRepository) Prune(ctx context.Context) error {
	const byAge = `
		DELETE FROM tls_san_candidates
		WHERE last_seen_at < NOW() - $1::interval`
	if _, err := r.db.ExecContext(ctx, byAge,
		fmt.Sprintf("%d seconds", int(sanCandidateRetention.Seconds()))); err != nil {
		return fmt.Errorf("failed to prune SAN candidates by age: %w", err)
	}

	const byCap = `
		DELETE FROM tls_san_candidates
		WHERE id IN (
			SELECT c.id
			FROM tls_san_candidates c
			LEFT JOIN agents a ON a.id = c.last_agent_id
			WHERE c.dismissed_at IS NULL
			  AND NOT (c.source = 'agent_tls_failure' AND a.id IS NOT NULL)
			ORDER BY
				CASE c.source
					WHEN 'host_header' THEN 0
					WHEN 'tls_sni'     THEN 1
					ELSE 2
				END,
				c.last_seen_at ASC
			OFFSET $1
		)`
	if _, err := r.db.ExecContext(ctx, byCap, sanCandidateMaxRetained); err != nil {
		return fmt.Errorf("failed to prune SAN candidates by cap: %w", err)
	}

	return nil
}

func scanCandidates(rows *sql.Rows) ([]TLSSANCandidate, error) {
	var out []TLSSANCandidate
	for rows.Next() {
		var c TLSSANCandidate
		var agentID sql.NullInt64
		var agentName, userAgent sql.NullString
		var port sql.NullInt64
		var dismissedAt sql.NullTime

		if err := rows.Scan(
			&c.ID, &c.Address, &c.Kind, &c.Source,
			&c.FirstSeenAt, &c.LastSeenAt, &c.HitCount,
			&agentID, &agentName, &userAgent, &port, &dismissedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan SAN candidate: %w", err)
		}

		if agentID.Valid {
			v := int(agentID.Int64)
			c.LastAgentID = &v
		}
		if agentName.Valid {
			c.LastAgentName = &agentName.String
		}
		if userAgent.Valid {
			c.LastUserAgent = &userAgent.String
		}
		if port.Valid {
			v := int(port.Int64)
			c.LastPort = &v
		}
		if dismissedAt.Valid {
			c.DismissedAt = &dismissedAt.Time
		}

		out = append(out, c)
	}
	return out, rows.Err()
}

// nullPort maps an unreported port to SQL NULL, so COALESCE in the upsert keeps
// whatever the previous sighting recorded.
func nullPort(i int) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(i), Valid: true}
}
