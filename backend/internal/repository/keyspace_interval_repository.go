package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// KeyspaceIntervalRepository handles persistence for job_keyspace_intervals,
// the explicit interval set that replaces the dispatched_keyspace watermark.
// See migration 000147 and scheduler-rewrite/plan.md §4.1 / §4.4.
type KeyspaceIntervalRepository struct {
	db *db.DB
}

func NewKeyspaceIntervalRepository(database *db.DB) *KeyspaceIntervalRepository {
	return &KeyspaceIntervalRepository{db: database}
}

// Insert creates a new interval. The Postgres no_overlap_per_unit exclusion
// constraint will reject any non-failed interval that overlaps an existing
// non-failed interval on the same scheduling_unit; the caller should be
// prepared to retry on that error (e.g., recompute the gap and try again).
func (r *KeyspaceIntervalRepository) Insert(ctx context.Context, interval *models.KeyspaceInterval) error {
	if interval.ID == uuid.Nil {
		interval.ID = uuid.New()
	}
	if interval.Status == "" {
		interval.Status = models.KeyspaceIntervalStatusAssigned
	}

	const query = `
		INSERT INTO job_keyspace_intervals (
			id, scheduling_unit_id, range_start, range_end, status, task_id
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`
	err := r.db.QueryRowContext(ctx, query,
		interval.ID,
		interval.SchedulingUnitID,
		interval.RangeStart,
		interval.RangeEnd,
		interval.Status,
		interval.TaskID,
	).Scan(&interval.CreatedAt, &interval.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert keyspace_interval: %w", err)
	}
	return nil
}

// GetByID returns one interval by ID. Returns sql.ErrNoRows if absent.
func (r *KeyspaceIntervalRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.KeyspaceInterval, error) {
	const query = `
		SELECT id, scheduling_unit_id, range_start, range_end, status,
		       task_id, created_at, updated_at
		FROM job_keyspace_intervals
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	return scanKeyspaceInterval(row)
}

// GetByUnitID returns all intervals for a scheduling_unit, ordered by
// range_start. Includes failed intervals — callers that want only the
// covering set should filter on status.
func (r *KeyspaceIntervalRepository) GetByUnitID(ctx context.Context, unitID uuid.UUID) ([]*models.KeyspaceInterval, error) {
	const query = `
		SELECT id, scheduling_unit_id, range_start, range_end, status,
		       task_id, created_at, updated_at
		FROM job_keyspace_intervals
		WHERE scheduling_unit_id = $1
		ORDER BY range_start ASC
	`
	rows, err := r.db.QueryContext(ctx, query, unitID)
	if err != nil {
		return nil, fmt.Errorf("failed to query intervals for unit %s: %w", unitID, err)
	}
	defer rows.Close()

	var intervals []*models.KeyspaceInterval
	for rows.Next() {
		i, err := scanKeyspaceInterval(rows)
		if err != nil {
			return nil, err
		}
		intervals = append(intervals, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return intervals, nil
}

// UndispatchedRanges returns the gap set: every [start, end) range in
// [0, effective_keyspace) that is NOT covered by a non-failed interval for
// the given scheduling_unit. Returned in ascending start order, so the
// dispatcher picks the first (smallest-start) gap to fill — this is the
// "fill gaps before appending" rule from plan §6.5 / §8.3.
//
// The query computes gaps with a window-function pass:
//   1. Order non-failed intervals by range_start.
//   2. For each, look at the previous row's range_end (0 if first).
//   3. If range_start > previous range_end, emit [prev_end, range_start)
//      as a gap.
//   4. After the last interval, emit [last_end, effective_keyspace) if
//      last_end < effective_keyspace.
//
// An empty unit (no intervals) returns one gap covering the whole
// effective_keyspace.
func (r *KeyspaceIntervalRepository) UndispatchedRanges(ctx context.Context, unitID uuid.UUID) ([]models.UndispatchedRange, error) {
	const query = `
		WITH unit AS (
			SELECT effective_keyspace FROM scheduling_units WHERE id = $1
		),
		ordered AS (
			SELECT range_start, range_end
			FROM job_keyspace_intervals
			WHERE scheduling_unit_id = $1 AND status <> 'failed'
		),
		with_prev AS (
			SELECT range_start, range_end,
			       LAG(range_end, 1, 0::BIGINT) OVER (ORDER BY range_start) AS prev_end
			FROM ordered
		),
		mid_gaps AS (
			SELECT prev_end AS gap_start, range_start AS gap_end
			FROM with_prev
			WHERE range_start > prev_end
		),
		tail_gap AS (
			SELECT COALESCE(MAX(range_end), 0) AS gap_start,
			       (SELECT effective_keyspace FROM unit) AS gap_end
			FROM ordered
			HAVING COALESCE(MAX(range_end), 0) < (SELECT effective_keyspace FROM unit)
		)
		SELECT gap_start, gap_end FROM mid_gaps
		UNION ALL
		SELECT gap_start, gap_end FROM tail_gap
		ORDER BY gap_start ASC
	`
	rows, err := r.db.QueryContext(ctx, query, unitID)
	if err != nil {
		return nil, fmt.Errorf("failed to query undispatched ranges for unit %s: %w", unitID, err)
	}
	defer rows.Close()

	var gaps []models.UndispatchedRange
	for rows.Next() {
		var g models.UndispatchedRange
		if err := rows.Scan(&g.Start, &g.End); err != nil {
			return nil, fmt.Errorf("failed to scan undispatched range: %w", err)
		}
		gaps = append(gaps, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return gaps, nil
}

// UpdateStatus moves an interval through its lifecycle (assigned -> running
// -> completed, or any -> failed). The DB CHECK constraint enforces the
// value is valid.
func (r *KeyspaceIntervalRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	const query = `UPDATE job_keyspace_intervals SET status = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update interval status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Truncate shrinks an interval's range_end down to newEnd and marks it
// completed. This is the §8.2 recovery primitive: when an agent disconnects
// at restore_point R inside task [S, S+L), the interval becomes [S, R)
// completed and the rest [R, S+L) automatically becomes a gap (no row
// covers it).
//
// Truncation only makes sense for intervals that are currently 'assigned'
// or 'running'; the query rejects others to avoid corrupting completed or
// failed records.
//
// newEnd must satisfy interval.range_start < newEnd <= interval.range_end.
func (r *KeyspaceIntervalRepository) Truncate(ctx context.Context, id uuid.UUID, newEnd int64) error {
	const query = `
		UPDATE job_keyspace_intervals
		SET range_end = $1, status = 'completed'
		WHERE id = $2
		  AND status IN ('assigned', 'running')
		  AND range_start < $1
		  AND $1 <= range_end
	`
	res, err := r.db.ExecContext(ctx, query, newEnd, id)
	if err != nil {
		return fmt.Errorf("failed to truncate interval: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if n == 0 {
		return errors.New("truncate failed: interval not found, wrong status, or newEnd out of range")
	}
	return nil
}

func scanKeyspaceInterval(scanner rowScanner) (*models.KeyspaceInterval, error) {
	i := &models.KeyspaceInterval{}
	err := scanner.Scan(
		&i.ID,
		&i.SchedulingUnitID,
		&i.RangeStart,
		&i.RangeEnd,
		&i.Status,
		&i.TaskID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to scan keyspace_interval: %w", err)
	}
	return i, nil
}
