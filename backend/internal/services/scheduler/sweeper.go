package scheduler

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

// EvictedTask describes one task the sweeper found stale and recovered.
// The actual interval truncation (the §8.2 split-and-gap algorithm) is
// invoked from here, but the *redispatch* of the resulting gap is the
// dispatcher's concern on the next cycle.
type EvictedTask struct {
	TaskID        uuid.UUID
	UnitID        uuid.UUID
	IntervalID    uuid.UUID
	RangeStart    int64
	RangeEnd      int64
	RestorePoint  sql.NullInt64
	Reason        string
}

// EvictTimedOutTasks scans for job_tasks whose last_activity_at is older
// than the heartbeat-timeout setting, and for each one runs the §8.2
// split-and-gap algorithm:
//   - If the task reported a restore_point > range_start, truncate the
//     interval to [range_start, restore_point) and mark it completed.
//     The remaining range [restore_point, range_end) is automatically a
//     gap, which the next dispatch cycle picks up.
//   - Otherwise (no progress reported), mark the interval as failed.
//     The exclusion constraint excludes failed intervals, so the full
//     [range_start, range_end) range becomes available for redispatch.
//
// Either way, the task itself is marked failed with a reason of
// "heartbeat timeout".
//
// heartbeatTimeoutSeconds is passed in by the caller; in production it
// reads system_settings.task_heartbeat_timeout_seconds once per cycle.
//
// Returns the list of evictions for caller-side logging/observability.
// Per-task errors are returned in errs so the sweeper doesn't bail out
// on a single bad row.
func EvictTimedOutTasks(
	ctx context.Context,
	database *db.DB,
	heartbeatTimeoutSeconds int,
) (evicted []EvictedTask, errs []error) {
	if heartbeatTimeoutSeconds <= 0 {
		heartbeatTimeoutSeconds = 120
	}

	// Find stale tasks plus the interval row they own (joined via
	// task_id). LEFT JOIN because we want to evict the task even if its
	// interval is somehow missing (shouldn't happen, but defensive).
	const query = `
		SELECT
			t.id, t.scheduling_unit_id, t.range_start, t.range_end, t.restore_point,
			i.id AS interval_id
		FROM job_tasks t
		LEFT JOIN job_keyspace_intervals i ON i.task_id = t.id
		WHERE t.status IN ('assigned', 'running')
		  AND t.scheduling_unit_id IS NOT NULL
		  AND t.range_start IS NOT NULL
		  AND t.range_end IS NOT NULL
		  AND t.last_activity_at IS NOT NULL
		  AND t.last_activity_at < NOW() - ($1 || ' seconds')::INTERVAL
	`
	rows, err := database.QueryContext(ctx, query, heartbeatTimeoutSeconds)
	if err != nil {
		errs = append(errs, fmt.Errorf("sweeper: query stale tasks: %w", err))
		return nil, errs
	}

	// Collect into a slice first so we can close the rows before issuing
	// further queries.
	type stale struct {
		TaskID       uuid.UUID
		UnitID       uuid.UUID
		RangeStart   int64
		RangeEnd     int64
		RestorePoint sql.NullInt64
		IntervalID   uuid.NullUUID
	}
	var staleTasks []stale
	for rows.Next() {
		var s stale
		if err := rows.Scan(
			&s.TaskID,
			&s.UnitID,
			&s.RangeStart,
			&s.RangeEnd,
			&s.RestorePoint,
			&s.IntervalID,
		); err != nil {
			errs = append(errs, fmt.Errorf("sweeper: scan stale task: %w", err))
			continue
		}
		staleTasks = append(staleTasks, s)
	}
	if err := rows.Err(); err != nil {
		errs = append(errs, fmt.Errorf("sweeper: row iteration: %w", err))
	}
	rows.Close()

	// Now act on each stale row, one per transaction.
	for _, s := range staleTasks {
		ev := EvictedTask{
			TaskID:       s.TaskID,
			UnitID:       s.UnitID,
			RangeStart:   s.RangeStart,
			RangeEnd:     s.RangeEnd,
			RestorePoint: s.RestorePoint,
			Reason:       "heartbeat timeout",
		}
		if s.IntervalID.Valid {
			ev.IntervalID = s.IntervalID.UUID
		}

		if err := evictOne(ctx, database, s.TaskID, s.IntervalID, s.RangeStart, s.RestorePoint); err != nil {
			errs = append(errs, fmt.Errorf("sweeper: evict task %s: %w", s.TaskID, err))
			continue
		}
		evicted = append(evicted, ev)
	}
	return evicted, errs
}

// evictOne runs the per-task eviction in a transaction: split-and-gap
// the interval (or fail it outright), then mark the task failed.
func evictOne(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	rangeStart int64,
	restorePoint sql.NullInt64,
) error {
	return database.WithTx(ctx, func(tx *sql.Tx) error {
		if intervalID.Valid {
			// Decide: truncate (progress was made) or fail (no progress).
			if restorePoint.Valid && restorePoint.Int64 > rangeStart {
				// Truncate to restore_point. The query mirrors
				// KeyspaceIntervalRepository.Truncate but runs inside
				// the transaction.
				res, err := tx.ExecContext(ctx, `
					UPDATE job_keyspace_intervals
					SET range_end = $1, status = 'completed'
					WHERE id = $2
					  AND status IN ('assigned', 'running')
					  AND range_start < $1
					  AND $1 <= range_end
				`, restorePoint.Int64, intervalID.UUID)
				if err != nil {
					return fmt.Errorf("truncate interval: %w", err)
				}
				n, _ := res.RowsAffected()
				if n == 0 {
					// Truncate window didn't match (race?). Fall
					// through to fail-the-interval path so the
					// range becomes a gap.
					if _, ferr := tx.ExecContext(ctx, `
						UPDATE job_keyspace_intervals
						SET status = 'failed'
						WHERE id = $1 AND status IN ('assigned','running')
					`, intervalID.UUID); ferr != nil {
						return fmt.Errorf("fallback fail interval: %w", ferr)
					}
				}
			} else {
				// No progress: mark interval failed.
				if _, err := tx.ExecContext(ctx, `
					UPDATE job_keyspace_intervals
					SET status = 'failed'
					WHERE id = $1 AND status IN ('assigned', 'running')
				`, intervalID.UUID); err != nil {
					return fmt.Errorf("fail interval: %w", err)
				}
			}
		}

		// Mark the task failed regardless of which interval branch we
		// took. failure_reason is the new column; status moves to
		// failed.
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_tasks
			SET status = 'failed', failure_reason = 'heartbeat timeout'
			WHERE id = $1
		`, taskID); err != nil {
			return fmt.Errorf("fail task: %w", err)
		}
		return nil
	})
}
