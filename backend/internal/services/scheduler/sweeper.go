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
	TaskID       uuid.UUID
	UnitID       uuid.UUID
	IntervalID   uuid.UUID
	RangeStart   int64
	RangeEnd     int64
	RestorePoint sql.NullInt64
	Reason       string

	// Truncated: progress was preserved and the remainder re-opened as a
	// gap. Discarded: no resumable progress, so the task and interval
	// rows were deleted outright. Both false means the rows were left
	// behind in a terminal state (cancelled by a delete guard).
	Truncated bool
	Discarded bool
}

// EvictTimedOutTasks scans for job_tasks whose last_activity_at is older
// than the heartbeat-timeout setting, and for each one runs the §8.2
// split-and-gap algorithm:
//   - If the task reported a restore_point > range_start, truncate the
//     interval to [range_start, restore_point) and mark it completed.
//     The remaining range [restore_point, range_end) is automatically a
//     gap, which the next dispatch cycle picks up.
//   - Otherwise (no progress reported), DELETE the task and interval
//     rows so the full [range_start, range_end) range becomes available
//     for redispatch. A heartbeat/grace timeout is a benign stop — the
//     same physical disconnect the graceful-shutdown handler already
//     classifies benign — so this uses PolicyDiscardOnNoProgress rather
//     than leaving a 'failed' row that would permanently fail the job.
//
// Either way the eviction reason ("heartbeat timeout") is recorded on
// the task while it still exists; only the truncate branch keeps a row.
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
	// task_id). LEFT JOIN to the interval because we want to evict the
	// task even if its interval is somehow missing (defensive). LEFT
	// JOIN to the agent so we can include disconnect-grace expiry as a
	// second eviction trigger — see §8.7 and migration 000150. A task
	// is stale if ANY of:
	//   - status IN ('assigned','running') and last_activity_at is
	//     older than the heartbeat timeout (agent connected but silent)
	//   - status IN ('assigned','running') and its agent's
	//     disconnect_grace_expires_at has passed (agent gone, hasn't
	//     come back, and we caught the task before HandleAgentDisconnection
	//     severed the agent_id link)
	//   - status='reconnect_pending' (legacy disconnect path NULLs out
	//     agent_id so neither check above can fire) and the task hasn't
	//     been updated within the heartbeat timeout — meaning the
	//     2-minute reconnect goroutine either never fired (canceled ctx)
	//     or fired and saw nothing reconnect. We use updated_at as a
	//     proxy for "how long has this been orphaned" since there's no
	//     agent link to consult. Restricted to scheduler-v2 tasks
	//     (scheduling_unit_id IS NOT NULL) so legacy tasks keep their
	//     existing recovery path.
	const query = `
		SELECT
			t.id, t.scheduling_unit_id, t.range_start, t.range_end, t.restore_point,
			i.id AS interval_id
		FROM job_tasks t
		LEFT JOIN job_keyspace_intervals i ON i.task_id = t.id
		LEFT JOIN agents a ON a.id = t.agent_id
		WHERE t.scheduling_unit_id IS NOT NULL
		  AND t.range_start IS NOT NULL
		  AND t.range_end IS NOT NULL
		  AND (
			  (
				  t.status IN ('assigned', 'running')
				  AND t.last_activity_at IS NOT NULL
				  AND t.last_activity_at < NOW() - ($1 || ' seconds')::INTERVAL
			  )
			  OR
			  (
				  t.status IN ('assigned', 'running')
				  AND a.disconnect_grace_expires_at IS NOT NULL
				  AND a.disconnect_grace_expires_at < NOW()
			  )
			  OR
			  (
				  t.status = 'reconnect_pending'
				  AND t.updated_at < NOW() - ($1 || ' seconds')::INTERVAL
			  )
		  )
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

		truncated, discarded, err := evictOne(ctx, database, s.TaskID, s.IntervalID, s.RangeStart, s.RestorePoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("sweeper: evict task %s: %w", s.TaskID, err))
			continue
		}
		ev.Truncated = truncated
		ev.Discarded = discarded
		evicted = append(evicted, ev)
	}
	return evicted, errs
}

// RecoverStrandedPendingTasks heals scheduler-v2 tasks that were parked
// in 'pending' with their agent link cleared while their keyspace
// interval stayed 'assigned'/'running' — the GH #77 stranding. That
// combination is invisible to every other recovery path: the dispatcher
// never reclaims a 'pending' task, and firstGap counts a non-failed
// interval as covered, so the unit looks fully tiled while no agent is
// working the range. The job then hangs at "covered but never complete"
// forever.
//
// The fix that stops NEW strandings is in ClearStoppedTaskAgent; this
// function exists to heal rows that were already stranded before that
// fix (and any future path that reintroduces the pattern). Each row goes
// through the same applyRecovery helper as evictOne: restore_point >
// range_start truncates the interval and completes the task, otherwise
// both rows are deleted and the whole range re-opens. Deleted, not
// failed — a stranded row is a bookkeeping accident, not an agent
// failure, and a 'failed' task would permanently fail the job.
//
// Why this is safe to run on every sweep:
//   - Idempotent: a recovered row is either gone entirely or 'completed'
//     with a 'completed' interval, so it stops matching the predicate.
//     There is nothing to re-recover on the next tick.
//   - Race-safe against in-flight stops: minAgeSeconds (the heartbeat
//     timeout) means a task must have sat untouched that long before we
//     touch it, which is far longer than the stopped-progress /
//     stop-ack round trip. And shrinking an interval can never violate
//     the keyspace exclusion constraint — the new range is a strict
//     subset of a range that was already exclusive.
//   - The JOIN is INNER on purpose: a v2 task sitting 'pending' with NO
//     live interval is NOT stranded — its range is already a gap the
//     dispatcher will re-issue, and failing the task would only add
//     noise to the job's task list.
//
// This deliberately does not widen EvictTimedOutTasks: that sweep's
// statuses mean "in flight" and its reason is hardcoded to
// "heartbeat timeout" in evictOne.
//
// Returns the number of rows recovered. Per-row errors accumulate in
// errs so one bad row can't abort the pass.
func RecoverStrandedPendingTasks(
	ctx context.Context,
	database *db.DB,
	minAgeSeconds int,
) (recovered int, errs []error) {
	if minAgeSeconds <= 0 {
		minAgeSeconds = 120
	}

	const query = `
		SELECT t.id, t.range_start, t.restore_point, i.id
		FROM job_tasks t
		JOIN job_keyspace_intervals i ON i.task_id = t.id
		WHERE t.scheduling_unit_id IS NOT NULL
		  AND t.status = 'pending'
		  AND t.agent_id IS NULL
		  AND t.range_start IS NOT NULL AND t.range_end IS NOT NULL
		  AND i.status IN ('assigned', 'running')
		  AND t.updated_at < NOW() - ($1 || ' seconds')::INTERVAL
	`
	rows, err := database.QueryContext(ctx, query, minAgeSeconds)
	if err != nil {
		errs = append(errs, fmt.Errorf("sweeper: query stranded pending tasks: %w", err))
		return 0, errs
	}

	// Collect first so the rows are closed before applyRecovery opens its
	// own transactions (mirrors EvictTimedOutTasks).
	type stranded struct {
		TaskID       uuid.UUID
		RangeStart   int64
		RestorePoint sql.NullInt64
		IntervalID   uuid.NullUUID
	}
	var strandedTasks []stranded
	for rows.Next() {
		var s stranded
		if err := rows.Scan(&s.TaskID, &s.RangeStart, &s.RestorePoint, &s.IntervalID); err != nil {
			errs = append(errs, fmt.Errorf("sweeper: scan stranded task: %w", err))
			continue
		}
		strandedTasks = append(strandedTasks, s)
	}
	if err := rows.Err(); err != nil {
		errs = append(errs, fmt.Errorf("sweeper: stranded row iteration: %w", err))
	}
	rows.Close()

	for _, s := range strandedTasks {
		if _, _, err := applyRecovery(ctx, database, s.TaskID, s.IntervalID, s.RangeStart, s.RestorePoint,
			"stranded pending task reclaimed", PolicyDiscardOnNoProgress); err != nil {
			errs = append(errs, fmt.Errorf("sweeper: recover stranded task %s: %w", s.TaskID, err))
			continue
		}
		recovered++
	}
	return recovered, errs
}

// evictOne runs the per-task eviction by delegating to applyRecovery
// (recovery.go), which is the canonical split-and-gap implementation
// shared with the graceful-shutdown path.
//
// PolicyDiscardOnNoProgress is deliberate: this sweep's predicate
// includes disconnect_grace_expires_at, i.e. exactly the physical
// disconnect that HandleAgentDisconnection already treats as benign.
// Leaving a 'failed' row here would make the job's terminal state depend
// on which handler wins that race.
func evictOne(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	rangeStart int64,
	restorePoint sql.NullInt64,
) (truncated bool, discarded bool, err error) {
	return applyRecovery(ctx, database, taskID, intervalID, rangeStart, restorePoint,
		"heartbeat timeout", PolicyDiscardOnNoProgress)
}
