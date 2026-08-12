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

// ErrTaskGone reports that the task a recovery targeted no longer
// exists. Benign stops DELETE the task row (see
// PolicyDiscardOnNoProgress), so the loser of a stopped-progress /
// stop-ack race now routinely finds nothing left to recover. Callers
// use errors.Is to log that at Info instead of warning about it.
var ErrTaskGone = errors.New("task no longer exists")

// RecoverPolicy selects what the no-progress branch does with the task
// row and its keyspace interval. The unprocessed range re-opens for
// re-dispatch either way — only the bookkeeping differs.
type RecoverPolicy int

const (
	// PolicyFailOnNoProgress marks both rows 'failed'. Zero value on
	// purpose: a future caller that forgets to pick a policy gets a
	// visible failed row, never a silent deletion.
	PolicyFailOnNoProgress RecoverPolicy = iota

	// PolicyDiscardOnNoProgress DELETEs both rows instead. This is the
	// right answer for a benign stop (operator stop, agent shutdown or
	// disconnect, assignment rejection, heartbeat eviction): a zero
	// restore point means hashcat never got far enough to produce one,
	// which is not an error — the work is simply discarded and redone.
	//
	// It matters because HasFailedTasks is a COUNT(*) > 0, not a
	// threshold: a single 'failed' row makes CompleteJobExecution call
	// FailExecution permanently, even after the re-opened range has been
	// redone successfully.
	PolicyDiscardOnNoProgress

	// PolicyCancelOnNoProgress marks the task 'cancelled' and discards
	// only the interval. Use it for a benign stop that is KNOWN to have
	// produced cracks the DB can't see yet: a crack batch still in flight
	// behind the status message has not bumped the task's counters, so
	// the delete guard would wave it through and
	// hashes.cracked_by_task_id (ON DELETE SET NULL) would lose the
	// attribution LoopbackRepository.GetNewDeltaPlaintexts INNER JOINs
	// on. 'cancelled' keeps the row — and the crack attribution — while
	// still not poisoning the job the way 'failed' would.
	PolicyCancelOnNoProgress
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
	// remainder becoming a gap.
	Truncated bool

	// Discarded is true when the no-progress branch ran under
	// PolicyDiscardOnNoProgress and actually removed the task row (and
	// its interval). Truncated and Discarded are mutually exclusive;
	// both false with Handled=true means the rows were marked failed —
	// or that a delete guard (cracks present, terminal status) held the
	// task back and it was cancelled instead.
	Discarded bool
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
// This is the FAIL-on-no-progress variant and stays the safe default:
// a caller that treats a stop as an error keeps producing a visible
// failed row. Benign stops should call RecoverStoppedTaskByID instead.
//
// reason is recorded in job_tasks.failure_reason for both branches —
// used by sweeper ("heartbeat timeout") and graceful-shutdown
// ("agent disconnect") to distinguish causes in diagnostics.
func RecoverTaskByID(ctx context.Context, database *db.DB, taskID uuid.UUID, reason string) (RecoverResult, error) {
	return recoverTaskByID(ctx, database, taskID, reason, PolicyFailOnNoProgress)
}

// RecoverStoppedTaskByID is RecoverTaskByID for a benign stop: identical
// truncate-and-preserve behaviour when hashcat left a usable restore
// point, but when it left none the task and interval rows are DELETED
// rather than marked 'failed'.
//
// The discarded work is unresumable either way; what changes is that the
// job no longer inherits a permanent failure from a stop that worked
// exactly as designed. See PolicyDiscardOnNoProgress.
func RecoverStoppedTaskByID(ctx context.Context, database *db.DB, taskID uuid.UUID, reason string) (RecoverResult, error) {
	return recoverTaskByID(ctx, database, taskID, reason, PolicyDiscardOnNoProgress)
}

// RecoverStoppedTaskWithCracksByID is RecoverStoppedTaskByID for a benign
// stop whose final message carried cracks. The task row survives as
// 'cancelled' so its crack attribution is preserved; the interval is
// still released so the range re-opens. See PolicyCancelOnNoProgress.
func RecoverStoppedTaskWithCracksByID(ctx context.Context, database *db.DB, taskID uuid.UUID, reason string) (RecoverResult, error) {
	return recoverTaskByID(ctx, database, taskID, reason, PolicyCancelOnNoProgress)
}

func recoverTaskByID(ctx context.Context, database *db.DB, taskID uuid.UUID, reason string, policy RecoverPolicy) (RecoverResult, error) {
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
		return RecoverResult{Handled: false}, fmt.Errorf("recover: task %s not found: %w", taskID, ErrTaskGone)
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
		err := failTaskAndInterval(ctx, database, taskID, intervalID, reason, policy)
		return RecoverResult{Handled: true, Truncated: false}, err
	}

	truncated, discarded, err := applyRecovery(ctx, database, taskID, intervalID, rangeStart.Int64, restorePoint, reason, policy)
	if err != nil {
		return RecoverResult{Handled: true}, err
	}
	return RecoverResult{Handled: true, Truncated: truncated, Discarded: discarded}, nil
}

// applyRecovery executes the truncate-or-discard-or-fail decision in a
// transaction. Returns truncated=true if the interval was shortened at
// restore_point (progress was made), discarded=true if the no-progress
// branch deleted the rows under PolicyDiscardOnNoProgress.
func applyRecovery(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	rangeStart int64,
	restorePoint sql.NullInt64,
	reason string,
	policy RecoverPolicy,
) (bool, bool, error) {
	truncated := false
	discarded := false

	err := database.WithTx(ctx, func(tx *sql.Tx) error {
		// Lock the task row FIRST — before either branch touches the
		// interval — so every recovery takes the same task -> interval
		// order, and so a concurrent crack-count writer
		// (IncrementReceivedCrackCount, a plain UPDATE job_tasks) can't
		// land between the delete guard's read of those counters and the
		// DELETE itself.
		//
		// The same SELECT captures the parent IDs the Step 11p cascade
		// needs. It has to happen here, before the deletes: once the task
		// row is gone, resolving unit / layer / job from it yields NULL
		// and all three cascade UPDATEs silently no-op, leaving them
		// stuck 'running' — and the cascade is best-effort, so nothing
		// would report it.
		var (
			unitID  uuid.NullUUID
			layerID uuid.NullUUID
			jobID   uuid.NullUUID
		)
		lockErr := tx.QueryRowContext(ctx, `
			SELECT scheduling_unit_id, increment_layer_id, job_execution_id
			FROM job_tasks
			WHERE id = $1
			FOR UPDATE
		`, taskID).Scan(&unitID, &layerID, &jobID)
		if lockErr != nil && !errors.Is(lockErr, sql.ErrNoRows) {
			return fmt.Errorf("lock task: %w", lockErr)
		}
		// ErrNoRows means a concurrent recovery already discarded the
		// task. We deliberately keep going: the interval statements below
		// are all no-ops if it took the interval too, but if anything
		// left one behind, a live interval outliving its task is exactly
		// the GH #77 stranding (firstGap counts it as covered forever).

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
				//
				// Status guard: mirrors the no-progress branch below.
				// The operator-stop path CANCELS the task first
				// (JobSchedulingService.StopJob) and only then sends
				// job_stop; the agent's stop-ack then lands here. Without
				// the guard that ack would resurrect a 'cancelled' task as
				// 'completed'. The interval UPDATE above is already
				// guarded the same way, so a terminal task simply keeps
				// its status while its interval is still truncated (the
				// unprocessed remainder correctly re-opens as a gap).
				//
				// detailed_status is rewritten too (here and in the
				// fail branch below): SendJobStop stamps 'stopping' on
				// the row when it issues the stop, so leaving it alone
				// would strand a finished task displaying "stopping"
				// forever. Values match the vocabulary the normal
				// completion paths use in job_task_repository.go.
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
					    completed_at = NOW(),
					    detailed_status = CASE WHEN crack_count > 0
					        THEN 'completed_with_cracks' ELSE 'completed_no_cracks' END
					WHERE id = $1
					  AND status NOT IN ('completed', 'cancelled')
				`, taskID, reason, restorePoint.Int64); terr != nil {
					return fmt.Errorf("complete task: %w", terr)
				}

				// Step 11p: cascade unit/layer/job status pending when work
				// remains and no other tasks are in flight. Run inside the
				// same transaction so all updates atomically commit. The
				// task row survives this branch, so the taskID resolver is
				// fine here (the no-progress branch below can't use it).
				cascadePendingFromRecovery(ctx, tx, taskID)

				return nil
			}
			// Truncate-window mismatch (race or already-completed
			// interval): fall through to the no-progress path.
		}

		// No-progress branch. hashcat left no resumable restore point, so
		// the whole [range_start, range_end) range is discarded and
		// re-opens for the next dispatch cycle. Only the bookkeeping
		// differs by policy.
		if policy != PolicyFailOnNoProgress {
			// A zero restore point means the task never got far enough to
			// produce one — that is not an error, so don't leave a
			// 'failed' row behind. HasFailedTasks is a COUNT(*) > 0, so
			// one such row fails the whole job permanently even after the
			// re-opened range has been redone successfully.
			//
			// Interval first, then task — mirrors the agent-rejection
			// delete in job_websocket_integration.go, and keeps the
			// task -> interval lock order the FOR UPDATE above started.
			//
			// The status guard is load-bearing. On a second recovery call
			// the truncate branch above is re-entered and falls through to
			// here once its UPDATE matches nothing (the interval is
			// already 'completed'). Deleting a 'completed' interval would
			// permanently un-cover a range that was already searched.
			//
			// Deleting by task_id rather than by intervalID is deliberate:
			// it can't leave a sibling interval pointing at a task row
			// we're about to remove. job_keyspace_intervals.task_id has NO
			// foreign key (migration 000147), so nothing cascades — the
			// interval has to be deleted explicitly.
			if _, derr := tx.ExecContext(ctx, `
				DELETE FROM job_keyspace_intervals
				WHERE task_id = $1
				  AND status IN ('assigned', 'running')
			`, taskID); derr != nil {
				return fmt.Errorf("discard interval: %w", derr)
			}

			// Crack guard: never delete a task that produced cracks.
			// hashes.cracked_by_task_id is ON DELETE SET NULL (migration
			// 000098) and LoopbackRepository.GetNewDeltaPlaintexts INNER
			// JOINs job_tasks on it, so nulling that pointer would
			// silently drop the plaintext from the loopback delta. The
			// three counters cover cracks that are counted but not yet
			// attributed; the NOT EXISTS rides the partial index
			// idx_hashes_cracked_by_task_id.
			//
			// PolicyCancelOnNoProgress skips the delete entirely: the
			// caller already knows cracks are in flight that these
			// counters cannot see yet.
			var deletedRows int64
			if policy == PolicyDiscardOnNoProgress {
				res, derr := tx.ExecContext(ctx, `
					DELETE FROM job_tasks
					WHERE id = $1
					  AND status NOT IN ('completed', 'cancelled')
					  AND COALESCE(crack_count, 0) = 0
					  AND COALESCE(received_crack_count, 0) = 0
					  AND COALESCE(expected_crack_count, 0) = 0
					  AND NOT EXISTS (SELECT 1 FROM hashes WHERE cracked_by_task_id = $1)
				`, taskID)
				if derr != nil {
					return fmt.Errorf("discard task: %w", derr)
				}
				deletedRows, _ = res.RowsAffected()
			}
			// Fallback when the delete was blocked or skipped (cracks
			// present, or the row is already terminal / already gone):
			// mark it 'cancelled', the codebase's existing "not an error,
			// don't poison the job" status — never 'failed'. The interval
			// was discarded above either way, so the range re-opens
			// regardless.
			if deletedRows > 0 {
				discarded = true
			} else if _, uerr := tx.ExecContext(ctx, `
				UPDATE job_tasks
				SET status = 'cancelled',
				    detailed_status = 'cancelled',
				    failure_reason = $2,
				    completed_at = NOW()
				WHERE id = $1
				  AND status NOT IN ('completed', 'cancelled')
			`, taskID, reason); uerr != nil {
				return fmt.Errorf("cancel task: %w", uerr)
			}
		} else {
			// Fail-both: the caller classified this stop as a real
			// failure, so leave visible rows behind.
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
				SET status = 'failed', failure_reason = $2, detailed_status = 'failed'
				WHERE id = $1
				  AND status NOT IN ('completed', 'cancelled')
			`, taskID, reason); err != nil {
				return fmt.Errorf("fail task: %w", err)
			}
		}

		// Step 11p: same pending cascade for the no-progress branch. A
		// released range still leaves the unit/layer/job in a
		// possibly-idle state; honest status display requires the cascade
		// to fire here too.
		//
		// Driven by the IDs read under FOR UPDATE above, not by taskID:
		// under PolicyDiscardOnNoProgress the task row no longer exists,
		// and the subquery form would resolve to NULL and no-op.
		//
		// Ordering: this MUST run AFTER the deletes. The unit cascade's
		// NOT EXISTS (... status IN ('assigned','running','processing'))
		// would otherwise still match the task being recovered and
		// suppress the transition.
		cascadePendingFromIDs(ctx, tx, unitID, layerID, jobID)

		return nil
	})

	return truncated, discarded, err
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
//
// This is the thin taskID-based resolver: it looks the three parent IDs
// up off the task row and delegates. Only usable while that row still
// exists — the discard path reads the IDs before deleting it and calls
// cascadePendingFromIDs directly.
func cascadePendingFromRecovery(ctx context.Context, tx *sql.Tx, taskID uuid.UUID) {
	var (
		unitID  uuid.NullUUID
		layerID uuid.NullUUID
		jobID   uuid.NullUUID
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_unit_id, increment_layer_id, job_execution_id
		FROM job_tasks
		WHERE id = $1
	`, taskID).Scan(&unitID, &layerID, &jobID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			debug.Warning("recovery cascade: resolve parents for task %s: %v", taskID, err)
		}
		return
	}
	cascadePendingFromIDs(ctx, tx, unitID, layerID, jobID)
}

// cascadePendingFromIDs carries the three cascade UPDATEs with the task
// subqueries replaced by explicit parameters, so it works after the task
// row has been deleted. Each UPDATE is skipped when its ID is NULL.
func cascadePendingFromIDs(ctx context.Context, tx *sql.Tx, unitID, layerID, jobID uuid.NullUUID) {
	if unitID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE scheduling_units su
			SET status = 'pending', updated_at = NOW()
			WHERE su.id = $1
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
		`, unitID.UUID); err != nil {
			debug.Warning("recovery cascade: unit->pending for unit %s: %v", unitID.UUID, err)
		}
	}

	if layerID.Valid && unitID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_increment_layers l
			SET status = 'pending', updated_at = NOW()
			FROM scheduling_units su
			WHERE l.id = $1
			  AND su.id = $2
			  AND l.status = 'running'
			  AND su.status = 'pending'
		`, layerID.UUID, unitID.UUID); err != nil {
			debug.Warning("recovery cascade: layer->pending for layer %s: %v", layerID.UUID, err)
		}
	}

	if jobID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_executions je
			SET status = 'pending', updated_at = NOW()
			WHERE je.id = $1
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
		`, jobID.UUID); err != nil {
			debug.Warning("recovery cascade: job->pending for job %s: %v", jobID.UUID, err)
		}
	}
}

// failTaskAndInterval is the terminal path for malformed tasks
// (scheduler-v2 task with NULL range_start / range_end). Defensive.
//
// The interval is always failed — that is the only status that re-opens
// a range. The TASK status follows the policy: a benign stop lands on
// 'cancelled' so a malformed row can't poison the job through
// HasFailedTasks, exactly as in the no-progress branch. There is no
// range to delete against here, so 'cancelled' is as far as the benign
// policies can go.
func failTaskAndInterval(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	reason string,
	policy RecoverPolicy,
) error {
	taskStatus := "failed"
	if policy != PolicyFailOnNoProgress {
		taskStatus = "cancelled"
	}
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
			SET status = $3, detailed_status = $3, failure_reason = $2
			WHERE id = $1
			  AND status NOT IN ('completed', 'cancelled')
		`, taskID, reason, taskStatus); err != nil {
			return fmt.Errorf("fail task: %w", err)
		}
		return nil
	})
}
