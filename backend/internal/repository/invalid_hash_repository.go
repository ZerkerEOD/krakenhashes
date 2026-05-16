package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/lib/pq"
)

// MaxInvalidHashRowsPerHashlist caps how many invalid lines a single upload
// can persist. Beyond this, the handler truncates the list and surfaces a
// "truncated" flag in the response so the user knows there are more.
const MaxInvalidHashRowsPerHashlist = 10000

// InvalidHashRepository handles persistence of per-line validation failures
// captured at hashlist upload time (GitHub issue #38).
type InvalidHashRepository struct {
	db *db.DB
}

// NewInvalidHashRepository returns a repository for the invalid_hashes table.
func NewInvalidHashRepository(database *db.DB) *InvalidHashRepository {
	return &InvalidHashRepository{db: database}
}

// BulkInsert writes invalid-line rows for a hashlist in a single COPY-style
// statement. Pass a non-nil tx to participate in an existing transaction.
//
// Caller is responsible for capping the slice length to
// MaxInvalidHashRowsPerHashlist.
func (r *InvalidHashRepository) BulkInsert(ctx context.Context, tx *sql.Tx, hashlistID int64, entries []models.InvalidHash) error {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > MaxInvalidHashRowsPerHashlist {
		entries = entries[:MaxInvalidHashRowsPerHashlist]
	}

	var stmt *sql.Stmt
	var err error
	if tx != nil {
		stmt, err = tx.PrepareContext(ctx, pq.CopyIn("invalid_hashes", "hashlist_id", "line_number", "content", "reason"))
	} else {
		// COPY requires a transaction; open a short-lived one.
		innerTx, beginErr := r.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("failed to begin tx for invalid_hashes bulk insert: %w", beginErr)
		}
		defer func() {
			if err != nil {
				_ = innerTx.Rollback()
			}
		}()
		stmt, err = innerTx.PrepareContext(ctx, pq.CopyIn("invalid_hashes", "hashlist_id", "line_number", "content", "reason"))
		if err != nil {
			return fmt.Errorf("failed to prepare COPY for invalid_hashes: %w", err)
		}
		defer stmt.Close()
		for _, e := range entries {
			if _, err = stmt.ExecContext(ctx, hashlistID, e.LineNumber, e.Content, e.Reason); err != nil {
				return fmt.Errorf("failed to stage invalid_hash row (line %d): %w", e.LineNumber, err)
			}
		}
		if _, err = stmt.ExecContext(ctx); err != nil {
			return fmt.Errorf("failed to flush invalid_hashes COPY: %w", err)
		}
		if err = innerTx.Commit(); err != nil {
			return fmt.Errorf("failed to commit invalid_hashes COPY tx: %w", err)
		}
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to prepare COPY for invalid_hashes: %w", err)
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err = stmt.ExecContext(ctx, hashlistID, e.LineNumber, e.Content, e.Reason); err != nil {
			return fmt.Errorf("failed to stage invalid_hash row (line %d): %w", e.LineNumber, err)
		}
	}
	if _, err = stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("failed to flush invalid_hashes COPY: %w", err)
	}
	return nil
}

// ListByHashlist returns a paginated slice of invalid lines for a hashlist,
// ordered by line_number ascending. limit and offset apply directly.
func (r *InvalidHashRepository) ListByHashlist(ctx context.Context, hashlistID int64, limit, offset int) ([]models.InvalidHash, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	const query = `
		SELECT id, hashlist_id, line_number, content, reason, created_at
		FROM invalid_hashes
		WHERE hashlist_id = $1
		ORDER BY line_number ASC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, hashlistID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list invalid hashes for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()
	out := make([]models.InvalidHash, 0, limit)
	for rows.Next() {
		var e models.InvalidHash
		if err := rows.Scan(&e.ID, &e.HashlistID, &e.LineNumber, &e.Content, &e.Reason, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan invalid_hash row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LineNumbersByHashlist returns the set of line numbers the processor must
// skip when committing a partially-valid hashlist. Returned as a map for O(1)
// membership tests during streaming reads.
func (r *InvalidHashRepository) LineNumbersByHashlist(ctx context.Context, hashlistID int64) (map[int]struct{}, error) {
	const query = `SELECT line_number FROM invalid_hashes WHERE hashlist_id = $1`
	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to load invalid line numbers for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()
	out := make(map[int]struct{})
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("failed to scan invalid line number: %w", err)
		}
		out[n] = struct{}{}
	}
	return out, rows.Err()
}

// DeleteByHashlist removes every invalid_hashes row for a hashlist. Used by
// the revalidate flow so a fresh validation pass starts from a clean slate
// (GitHub issue #38).
func (r *InvalidHashRepository) DeleteByHashlist(ctx context.Context, hashlistID int64) error {
	const query = `DELETE FROM invalid_hashes WHERE hashlist_id = $1`
	if _, err := r.db.ExecContext(ctx, query, hashlistID); err != nil {
		return fmt.Errorf("failed to delete invalid hashes for hashlist %d: %w", hashlistID, err)
	}
	return nil
}

// CountByHashlist returns the total number of invalid lines persisted for a
// hashlist. Useful for pagination metadata.
func (r *InvalidHashRepository) CountByHashlist(ctx context.Context, hashlistID int64) (int, error) {
	const query = `SELECT COUNT(*) FROM invalid_hashes WHERE hashlist_id = $1`
	var n int
	if err := r.db.QueryRowContext(ctx, query, hashlistID).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count invalid hashes for hashlist %d: %w", hashlistID, err)
	}
	return n, nil
}
