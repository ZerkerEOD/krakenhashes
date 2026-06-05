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
//
// What it does NOT update: scheduling_units.effective_keyspace. The
// earlier "Step 7h2" refresh-from-chunk-progress logic was reverted
// because it caused the denominator drift that produced the
// "Overall Progress: 51.45%" display bug. The user-stated model is
// simpler: track BASE keyspace internally, derive effective at job
// creation time (= base × original multiplier) and never refresh.
// Salt removal makes the job run FASTER but doesn't change the work
// accounting — the same base_keyspace × original_rules × original_salts
// "effective work" was promised at job start, and that's what gets
// tracked through completion.
//
// totalEffectiveKeyspace parameter retained for signature compatibility
// with HandleJobProgress, but currently unused. The parameter can be
// removed in a follow-up cleanup pass.
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

	// Step 11l: if keyspaceProcessed is zero/negative, SKIP the
	// restore_point write entirely. The OLD Step 9k defense converted
	// 0 → range_start before writing — which OVERWROTE a previously-
	// stored good value (e.g., 5,328,876) with range_start (4,444,140)
	// when the agent sent a final `status=failed` message with
	// KeyspaceProcessed=0. After that overwrite, RecoverTaskByID
	// couldn't truncate because restore_point == range_start, and the
	// task's actual work (80% of the chunk) was thrown away.
	//
	// Correct behavior: a 0 reading means "no useful progress info";
	// don't touch restore_point. Always refresh last_activity_at so
	// the sweeper doesn't time us out.
	if keyspaceProcessed <= 0 {
		_, err = database.ExecContext(ctx, `
			UPDATE job_tasks SET last_activity_at = NOW() WHERE id = $1
		`, taskID)
		if err != nil {
			return fmt.Errorf("ingest-v2: update last_activity_at: %w", err)
		}
		_ = totalEffectiveKeyspace
		return nil
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

	// Step 11l: MONOTONIC guard on restore_point UPDATE.
	// `COALESCE(restore_point, 0) <= $1` allows the first write
	// (NULL row) AND any forward-only updates. A stale/out-of-order
	// message with a smaller value will no-op silently instead of
	// regressing the value. Matches the Step 10c-3 pattern for
	// keyspace_processed.
	if restorePoint.Valid {
		_, err = database.ExecContext(ctx, `
			UPDATE job_tasks
			SET restore_point = $1, last_activity_at = NOW()
			WHERE id = $2
			  AND COALESCE(restore_point, 0) <= $1
		`, restorePoint.Int64, taskID)
	} else {
		_, err = database.ExecContext(ctx, `
			UPDATE job_tasks SET last_activity_at = NOW() WHERE id = $1
		`, taskID)
	}
	if err != nil {
		return fmt.Errorf("ingest-v2: update task progress: %w", err)
	}

	// (Step 7h2 multiplier-refresh logic removed — see function doc.)
	// totalEffectiveKeyspace intentionally unused.
	_ = totalEffectiveKeyspace

	return nil
}
