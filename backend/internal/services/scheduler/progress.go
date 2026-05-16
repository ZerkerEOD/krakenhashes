package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
)

// IngestProgressV2 updates the scheduler-v2-side columns when an agent
// reports progress for a task. Called from the legacy
// HandleJobProgress with a one-liner; no-op for legacy tasks (those
// without a scheduling_unit_id).
//
// What it updates:
//   - job_tasks.restore_point — from progress.KeyspaceProcessed. The
//     sweeper uses this as the truncation point if the task goes
//     stale.
//   - job_tasks.last_activity_at — NOW(). The sweeper's heartbeat
//     check reads this column.
//   - scheduling_units.effective_keyspace — when the unit's
//     keyspace was estimated and the agent reports a first-update
//     progress[1] (totalEffectiveKeyspace != nil). Flips
//     is_accurate_keyspace=true so the dispatcher trusts the new
//     value for future chunks.
//
// Errors are returned but the legacy caller should log and continue;
// progress ingestion failures should not break progress reporting.
func IngestProgressV2(
	ctx context.Context,
	database *db.DB,
	taskID uuid.UUID,
	keyspaceProcessed int64,
	totalEffectiveKeyspace *int64,
) error {

	// First lookup: is this a v2 task? Get the unit ID and the
	// task's range so we can validate restore_point. One query.
	var schedulingUnitID uuid.NullUUID
	var rangeStart, rangeEnd sql.NullInt64
	err := database.QueryRowContext(ctx, `
		SELECT scheduling_unit_id, range_start, range_end
		FROM job_tasks
		WHERE id = $1
	`, taskID).Scan(&schedulingUnitID, &rangeStart, &rangeEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // task gone; legacy already logs this case
	}
	if err != nil {
		return fmt.Errorf("ingest-v2: lookup task %s: %w", taskID, err)
	}
	if !schedulingUnitID.Valid {
		return nil // legacy task; no v2 columns to update
	}

	// Clamp restore_point to [range_start, range_end]. Hashcat
	// sometimes reports values just past the chunk end as it
	// finalizes; clamping avoids the restore_within_range CHECK
	// constraint firing on the update.
	var restorePoint sql.NullInt64
	if rangeStart.Valid && rangeEnd.Valid {
		rp := keyspaceProcessed
		if rp < rangeStart.Int64 {
			rp = rangeStart.Int64
		}
		if rp > rangeEnd.Int64 {
			rp = rangeEnd.Int64
		}
		restorePoint = sql.NullInt64{Int64: rp, Valid: true}
	}

	// Always update last_activity_at; conditionally update
	// restore_point.
	if restorePoint.Valid {
		_, err = database.ExecContext(ctx, `
			UPDATE job_tasks
			SET restore_point = $1, last_activity_at = NOW()
			WHERE id = $2
		`, restorePoint.Int64, taskID)
	} else {
		_, err = database.ExecContext(ctx, `
			UPDATE job_tasks SET last_activity_at = NOW() WHERE id = $1
		`, taskID)
	}
	if err != nil {
		return fmt.Errorf("ingest-v2: update task progress: %w", err)
	}

	// First-progress effective_keyspace refinement. Only fires when
	// the agent sends total_effective_keyspace (typically on the
	// first progress update) AND the unit hasn't been flipped to
	// accurate yet.
	if totalEffectiveKeyspace != nil && *totalEffectiveKeyspace > 0 {
		_, err = database.ExecContext(ctx, `
			UPDATE scheduling_units
			SET effective_keyspace = $1, is_accurate_keyspace = true
			WHERE id = $2 AND is_accurate_keyspace = false
		`, *totalEffectiveKeyspace, schedulingUnitID.UUID)
		if err != nil {
			return fmt.Errorf("ingest-v2: refine unit effective_keyspace: %w", err)
		}
	}

	return nil
}
