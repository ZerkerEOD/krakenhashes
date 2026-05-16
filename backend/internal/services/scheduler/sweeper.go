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
	// task_id). LEFT JOIN to the interval because we want to evict the
	// task even if its interval is somehow missing (defensive). LEFT
	// JOIN to the agent so we can include disconnect-grace expiry as a
	// second eviction trigger — see §8.7 and migration 000150. A task
	// is stale if EITHER:
	//   - its last_activity_at is older than the heartbeat timeout
	//     (agent connected but silent), OR
	//   - its agent's disconnect_grace_expires_at has passed (agent
	//     gone and didn't come back).
	const query = `
		SELECT
			t.id, t.scheduling_unit_id, t.range_start, t.range_end, t.restore_point,
			i.id AS interval_id
		FROM job_tasks t
		LEFT JOIN job_keyspace_intervals i ON i.task_id = t.id
		LEFT JOIN agents a ON a.id = t.agent_id
		WHERE t.status IN ('assigned', 'running')
		  AND t.scheduling_unit_id IS NOT NULL
		  AND t.range_start IS NOT NULL
		  AND t.range_end IS NOT NULL
		  AND (
			  (
				  t.last_activity_at IS NOT NULL
				  AND t.last_activity_at < NOW() - ($1 || ' seconds')::INTERVAL
			  )
			  OR
			  (
				  a.disconnect_grace_expires_at IS NOT NULL
				  AND a.disconnect_grace_expires_at < NOW()
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

		if err := evictOne(ctx, database, s.TaskID, s.IntervalID, s.RangeStart, s.RestorePoint); err != nil {
			errs = append(errs, fmt.Errorf("sweeper: evict task %s: %w", s.TaskID, err))
			continue
		}
		evicted = append(evicted, ev)
	}
	return evicted, errs
}

// evictOne runs the per-task eviction by delegating to applyRecovery
// (recovery.go), which is the canonical split-and-gap implementation
// shared with the graceful-shutdown path.
func evictOne(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	intervalID uuid.NullUUID,
	rangeStart int64,
	restorePoint sql.NullInt64,
) error {
	_, err := applyRecovery(ctx, database, taskID, intervalID, rangeStart, restorePoint, "heartbeat timeout")
	return err
}
