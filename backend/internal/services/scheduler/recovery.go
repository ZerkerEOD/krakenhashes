package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

// RecoverResult describes the outcome of a single recovery decision.
type RecoverResult struct {
	// Handled is true when the task was owned by the new scheduler
	// (scheduling_unit_id IS NOT NULL) and recovery ran. Callers in the
	// graceful-shutdown path use this to decide whether to fall through
	// to the legacy SetTaskPending path.
	Handled bool

	// Truncated is true when restore_point was > range_start and the
	// interval was shortened to [range_start, restore_point), with the
	// remainder becoming a gap. Truncated=false with Handled=true means
	// the interval was marked failed (no progress was reported).
	Truncated bool
}

// RecoverTaskByID runs the §8.2 split-and-gap algorithm on a single
// task. It is the canonical recovery primitive — the sweeper and the
// graceful-shutdown handler both end up here.
//
// Behavior:
//   - If the task has no scheduling_unit_id (legacy task), returns
//     {Handled: false} with no DB writes. The caller should run its
//     legacy recovery path.
//   - If restore_point > range_start, truncates the interval to
//     [range_start, restore_point) and marks the task completed. The
//     remainder [restore_point, range_end) becomes a gap automatically
//     because no row covers it.
//   - Otherwise marks the interval failed and the task failed. The
//     full [range_start, range_end) range becomes available for the
//     next dispatch cycle because the exclusion constraint ignores
//     failed intervals.
//
// reason is recorded in job_tasks.failure_reason for both branches —
// used by sweeper ("heartbeat timeout") and graceful-shutdown
// ("agent disconnect") to distinguish causes in diagnostics.
func RecoverTaskByID(ctx context.Context, database *db.DB, taskID uuid.UUID, reason string) (RecoverResult, error) {
	var (
		schedulingUnitID uuid.NullUUID
		rangeStart       sql.NullInt64
		rangeEnd         sql.NullInt64
		restorePoint     sql.NullInt64
		intervalID       uuid.NullUUID
	)

	// One query gathers everything: task fields plus the interval row
	// that points back at this task. LEFT JOIN because a task may have
	// no interval (defensive — shouldn't happen for scheduler-v2 tasks
	// once the dispatcher has run, but guards against partial writes).
	const lookupQuery = `
		SELECT
			t.scheduling_unit_id,
			t.range_start,
			t.range_end,
			t.restore_point,
			i.id AS interval_id
		FROM job_tasks t
		LEFT JOIN job_keyspace_intervals i ON i.task_id = t.id
		WHERE t.id = $1
	`
	err := database.QueryRowContext(ctx, lookupQuery, taskID).Scan(
		&schedulingUnitID, &rangeStart, &rangeEnd, &restorePoint, &intervalID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RecoverResult{Handled: false}, fmt.Errorf("recover: task %s not found", taskID)
	}
	if err != nil {
		return RecoverResult{}, fmt.Errorf("recover: lookup task %s: %w", taskID, err)
	}

	// Legacy task — caller handles.
	if !schedulingUnitID.Valid {
		return RecoverResult{Handled: false}, nil
	}

	// New-scheduler task. range_start / range_end should be populated
	// by the dispatcher; if they aren't (shouldn't happen, but defensive),
	// fall back to failing both the interval and the task.
	if !rangeStart.Valid || !rangeEnd.Valid {
		err := failTaskAndInterval(ctx, database, taskID, intervalID, reason)
		return RecoverResult{Handled: true, Truncated: false}, err
	}

	truncated, err := applyRecovery(ctx, database, taskID, intervalID, rangeStart.Int64, restorePoint, reason)
	if err != nil {
		return RecoverResult{Handled: true}, err
	}
	return RecoverResult{Handled: true, Truncated: truncated}, nil
}

// applyRecovery executes the truncate-or-fail decision in a transaction.
// Returns true if the interval was truncated (progress was made), false
// if it was failed (no progress).
func applyRecovery(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	rangeStart int64,
	restorePoint sql.NullInt64,
	reason string,
) (bool, error) {
	truncated := false

	err := database.WithTx(ctx, func(tx *sql.Tx) error {
		// Decide: truncate (progress) or fail (no progress).
		if intervalID.Valid && restorePoint.Valid && restorePoint.Int64 > rangeStart {
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
			if n == 1 {
				truncated = true
				// Task gets completed status — the chunk did real work.
				if _, terr := tx.ExecContext(ctx, `
					UPDATE job_tasks
					SET status = 'completed', failure_reason = $2, completed_at = NOW()
					WHERE id = $1
				`, taskID, reason); terr != nil {
					return fmt.Errorf("complete task: %w", terr)
				}
				return nil
			}
			// Truncate-window mismatch (race or already-completed
			// interval): fall through to the fail-both path.
		}

		// No-progress branch: fail interval (if present), fail task.
		if intervalID.Valid {
			if _, err := tx.ExecContext(ctx, `
				UPDATE job_keyspace_intervals
				SET status = 'failed'
				WHERE id = $1 AND status IN ('assigned', 'running')
			`, intervalID.UUID); err != nil {
				return fmt.Errorf("fail interval: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_tasks
			SET status = 'failed', failure_reason = $2
			WHERE id = $1
		`, taskID, reason); err != nil {
			return fmt.Errorf("fail task: %w", err)
		}
		return nil
	})

	return truncated, err
}

// failTaskAndInterval is the all-failed path for malformed tasks
// (scheduler-v2 task with NULL range_start / range_end). Defensive.
func failTaskAndInterval(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	reason string,
) error {
	return database.WithTx(ctx, func(tx *sql.Tx) error {
		if intervalID.Valid {
			if _, err := tx.ExecContext(ctx, `
				UPDATE job_keyspace_intervals
				SET status = 'failed'
				WHERE id = $1 AND status IN ('assigned', 'running')
			`, intervalID.UUID); err != nil {
				return fmt.Errorf("fail interval: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_tasks
			SET status = 'failed', failure_reason = $2
			WHERE id = $1
		`, taskID, reason); err != nil {
			return fmt.Errorf("fail task: %w", err)
		}
		return nil
	})
}
