package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
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
				// Step 11o: truncate the TASK row to reflect what was
				// actually completed — not the originally dispatched range.
				// Update range_end to restore_point, scale
				// effective_keyspace_end proportionally, set
				// progress_percent = 100 (the task is fully done for its
				// new smaller range), and update keyspace_processed to
				// match. Frontend will then display "X.XXT - Y.YYT |
				// 100%" honestly instead of the misleading "5.96T - 8.01T
				// | 79.82%" of the original range.
				//
				// The unprocessed remainder (restore_point → old range_end)
				// becomes a gap automatically because the interval's
				// range_end was also truncated above.
				//
				// The proportional eff_end / eff_processed math uses
				// existing columns on the task row, so we can do it in
				// SQL without re-querying.
				// Proportional-eff math runs in NUMERIC to avoid int64
				// overflow. Without the cast, ($3 - range_start) ×
				// (effective_keyspace_end - effective_keyspace_start)
				// can exceed int64 max (~9.2e18) easily: a 600M base
				// chunk on a job with a 77× rule multiplier produces
				// 600M × 46B = 2.8e19, well past the limit. The
				// overflow surfaces as `pq: bigint out of range` and
				// leaves the task stuck in 'running' forever because
				// the sweeper retries the same overflowing SQL.
				// NUMERIC handles arbitrary precision; we cast the
				// final value back to bigint for storage.
				if _, terr := tx.ExecContext(ctx, `
					UPDATE job_tasks
					SET status = 'completed',
					    range_end = $3,
					    keyspace_end = $3,
					    restore_point = $3,
					    keyspace_processed = $3 - range_start,
					    progress_percent = 100.0,
					    effective_keyspace_end = CASE
					        WHEN effective_keyspace_start IS NOT NULL
					         AND effective_keyspace_end IS NOT NULL
					         AND range_end > range_start
					        THEN effective_keyspace_start +
					             ( (($3 - range_start)::numeric * (effective_keyspace_end - effective_keyspace_start)::numeric)
					               / NULLIF((range_end - range_start)::numeric, 0)
					             )::bigint
					        ELSE effective_keyspace_end
					    END,
					    effective_keyspace_processed = CASE
					        WHEN effective_keyspace_start IS NOT NULL
					         AND effective_keyspace_end IS NOT NULL
					         AND range_end > range_start
					        THEN ( (($3 - range_start)::numeric * (effective_keyspace_end - effective_keyspace_start)::numeric)
					               / NULLIF((range_end - range_start)::numeric, 0)
					             )::bigint
					        ELSE effective_keyspace_processed
					    END,
					    failure_reason = $2,
					    completed_at = NOW()
					WHERE id = $1
				`, taskID, reason, restorePoint.Int64); terr != nil {
					return fmt.Errorf("complete task: %w", terr)
				}

				// Step 11p: cascade unit/layer/job status pending when work
				// remains and no other tasks are in flight. Run inside the
				// same transaction so all updates atomically commit.
				cascadePendingFromRecovery(ctx, tx, taskID)

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
		// Status guard: don't flip an already-terminal task. Without
		// this, a second RecoverTaskByID invocation (e.g., disconnect +
		// agent-reported failure both fire) overwrites a successful
		// truncate-and-complete with 'failed'. The interval row is
		// guarded the same way above. See Bug B / Finding 3.
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_tasks
			SET status = 'failed', failure_reason = $2
			WHERE id = $1
			  AND status NOT IN ('completed', 'cancelled')
		`, taskID, reason); err != nil {
			return fmt.Errorf("fail task: %w", err)
		}

		// Step 11p: same pending cascade for the fail-both branch. A
		// failed task that releases its range still leaves the unit/
		// layer/job in a possibly-idle state; honest status display
		// requires the cascade to fire here too.
		cascadePendingFromRecovery(ctx, tx, taskID)

		return nil
	})

	return truncated, err
}

// cascadePendingFromRecovery mirrors the Step 11n cascades from
// HandleTaskCompletion. Needed in recovery.go because RecoverTaskByID
// doesn't go through HandleTaskCompletion — it writes the task UPDATE
// directly. Without this duplication, scheduling_units /
// job_increment_layers / job_executions would stay 'running' after an
// agent disconnect even when no work is in flight.
//
// All three UPDATEs are idempotent and safe to no-op:
//   - unit: only flips 'running' → 'pending' when gaps remain AND no
//     in-flight tasks for the unit
//   - layer: only flips when its corresponding unit just went pending
//   - job: only flips when no in-flight tasks anywhere AND at least
//     one unit isn't completed yet
//
// Best-effort: errors are logged but don't fail the recovery transaction.
func cascadePendingFromRecovery(ctx context.Context, tx *sql.Tx, taskID uuid.UUID) {
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_units su
		SET status = 'pending', updated_at = NOW()
		WHERE su.id = (SELECT scheduling_unit_id FROM job_tasks WHERE id = $1)
		  AND su.status = 'running'
		  AND su.base_keyspace IS NOT NULL
		  AND (
			SELECT COALESCE(SUM(range_end - range_start), 0)
			FROM job_keyspace_intervals jki
			WHERE jki.scheduling_unit_id = su.id
			  AND jki.status NOT IN ('failed', 'cancelled')
		  ) < su.base_keyspace
		  AND NOT EXISTS (
			SELECT 1 FROM job_tasks t
			WHERE t.scheduling_unit_id = su.id
			  AND t.status IN ('assigned', 'running', 'processing')
		  )
	`, taskID); err != nil {
		debug.Warning("recovery cascade: unit->pending for task %s: %v", taskID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE job_increment_layers l
		SET status = 'pending', updated_at = NOW()
		FROM job_tasks t, scheduling_units su
		WHERE t.id = $1
		  AND t.increment_layer_id = l.id
		  AND su.id = t.scheduling_unit_id
		  AND l.status = 'running'
		  AND su.status = 'pending'
	`, taskID); err != nil {
		debug.Warning("recovery cascade: layer->pending for task %s: %v", taskID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE job_executions je
		SET status = 'pending', updated_at = NOW()
		WHERE je.id = (SELECT job_execution_id FROM job_tasks WHERE id = $1)
		  AND je.status = 'running'
		  AND NOT EXISTS (
			SELECT 1 FROM job_tasks t
			WHERE t.job_execution_id = je.id
			  AND t.status IN ('assigned', 'running', 'processing')
		  )
		  AND EXISTS (
			SELECT 1 FROM scheduling_units su
			WHERE su.parent_job_id = je.id AND su.status <> 'completed'
		  )
	`, taskID); err != nil {
		debug.Warning("recovery cascade: job->pending for task %s: %v", taskID, err)
	}
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
