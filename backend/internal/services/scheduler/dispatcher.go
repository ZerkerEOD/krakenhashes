package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// DispatchInputs bundles everything DispatchOneChunkPerAgent needs to
// turn an Allocation list into rows in job_keyspace_intervals + job_tasks.
type DispatchInputs struct {
	Allocations []Allocation

	// Units indexed by ID. The dispatcher needs each unit's parent_job_id
	// (legacy job_tasks NOT NULL constraint) and effective_keyspace
	// (chunk-size bound).
	Units map[uuid.UUID]*models.SchedulingUnit

	// AgentSpeeds: hashes/sec per agent. Used to size chunks. 0 or
	// missing means "unknown" — we'll fall back to a conservative
	// constant (see ConservativeAgentSpeed).
	AgentSpeeds map[int]int64

	// Sizing knobs (system settings 'target_chunk_seconds' and
	// 'min_chunk_seconds'). The caller reads them once per cycle.
	TargetChunkSeconds int
	MinChunkSeconds    int
}

// DispatchedTask is the output handed back to the scheduling cycle so it
// can fire WebSocket task_assignment messages.
type DispatchedTask struct {
	TaskID     uuid.UUID
	UnitID     uuid.UUID
	AgentID    int
	RangeStart int64
	RangeEnd   int64
}

// ConservativeAgentSpeed is used when AgentSpeeds[agentID] is missing or
// zero. 1 GH/s is roughly mid-range for a current consumer GPU on a fast
// hash; pessimistic enough that the first chunk is small enough to land
// quickly and refine the cache, optimistic enough to be useful.
const ConservativeAgentSpeed int64 = 1_000_000_000

// DispatchOneChunkPerAgent iterates the allocations and, for each,
// inserts one job_keyspace_intervals row + one job_tasks row in a
// per-allocation transaction. Returns the dispatched tasks; the caller
// fans out WebSocket messages.
//
// Per-allocation transactions (vs. one big transaction for everything):
// if one insert fails — e.g., a race with another scheduling cycle that
// already claimed the gap — only that allocation is lost. The rest
// proceed.
//
// The exclusion constraint on job_keyspace_intervals serves as the
// last-line defense against races; a violation here is logged and the
// allocation is skipped.
//
// Returns the successfully dispatched tasks. Per-allocation errors are
// embedded in the returned `errs` slice so the caller can decide whether
// to alert or just log.
func DispatchOneChunkPerAgent(
	ctx context.Context,
	database *db.DB,
	in DispatchInputs,
) (dispatched []DispatchedTask, errs []error) {
	if in.TargetChunkSeconds <= 0 {
		in.TargetChunkSeconds = 60
	}
	if in.MinChunkSeconds <= 0 {
		in.MinChunkSeconds = 5
	}

	for _, alloc := range in.Allocations {
		unit, ok := in.Units[alloc.UnitID]
		if !ok {
			errs = append(errs, fmt.Errorf("dispatcher: no unit row for %s", alloc.UnitID))
			continue
		}

		task, err := dispatchOne(ctx, database, alloc, unit, in)
		if err != nil {
			errs = append(errs, fmt.Errorf("dispatcher: alloc %s/%d: %w", alloc.UnitID, alloc.AgentID, err))
			continue
		}
		if task != nil {
			dispatched = append(dispatched, *task)
		}
	}
	return dispatched, errs
}

// dispatchOne handles a single allocation in its own transaction.
// Returns (nil, nil) if no work was dispatched because the unit has no
// remaining gap (raced with another cycle).
func dispatchOne(
	ctx context.Context,
	database *db.DB,
	alloc Allocation,
	unit *models.SchedulingUnit,
	in DispatchInputs,
) (*DispatchedTask, error) {
	// 1. Pick the first gap on the unit.
	gap, ok, err := firstGap(ctx, database, alloc.UnitID)
	if err != nil {
		return nil, fmt.Errorf("query gap: %w", err)
	}
	if !ok {
		// No gap left — unit fully covered. Caller should pick this up
		// in completion detection on the next cycle.
		return nil, nil
	}

	// 2. Size the chunk.
	speed, hasSpeed := in.AgentSpeeds[alloc.AgentID]
	if !hasSpeed || speed <= 0 {
		speed = ConservativeAgentSpeed
	}
	chunkSize := sizeChunk(gap.End-gap.Start, speed, in.TargetChunkSeconds, in.MinChunkSeconds)
	rangeStart := gap.Start
	rangeEnd := gap.Start + chunkSize

	// 3. Insert interval + task in a transaction.
	taskID := uuid.New()
	intervalID := uuid.New()
	now := time.Now()

	err = database.WithTx(ctx, func(tx *sql.Tx) error {
		// Insert job_tasks first so we have its ID to reference from
		// the interval row. The legacy NOT NULL columns
		// (job_execution_id, agent_id, keyspace_start/end,
		// chunk_duration) are mirrored from the new columns until
		// migration 000150 drops them.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO job_tasks (
				id, job_execution_id, agent_id, status,
				keyspace_start, keyspace_end, chunk_duration,
				scheduling_unit_id, range_start, range_end,
				last_activity_at, benchmark_speed
			) VALUES (
				$1, $2, $3, 'assigned',
				$4, $5, $6,
				$7, $4, $5,
				$8, $9
			)
		`,
			taskID,
			unit.ParentJobID,
			alloc.AgentID,
			rangeStart, rangeEnd,
			in.TargetChunkSeconds,
			unit.ID,
			now,
			speed,
		)
		if err != nil {
			return fmt.Errorf("insert task: %w", err)
		}

		// Then the interval, pointing at the task.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO job_keyspace_intervals (
				id, scheduling_unit_id, range_start, range_end, status, task_id
			) VALUES ($1, $2, $3, $4, 'assigned', $5)
		`,
			intervalID,
			unit.ID,
			rangeStart, rangeEnd,
			taskID,
		)
		if err != nil {
			return fmt.Errorf("insert interval: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &DispatchedTask{
		TaskID:     taskID,
		UnitID:     unit.ID,
		AgentID:    alloc.AgentID,
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
	}, nil
}

// firstGap returns the smallest-start undispatched range for a unit, or
// (zero, false, nil) if there is no gap.
func firstGap(ctx context.Context, database *db.DB, unitID uuid.UUID) (models.UndispatchedRange, bool, error) {
	const query = `
		WITH ordered AS (
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
			       (SELECT effective_keyspace FROM scheduling_units WHERE id = $1) AS gap_end
			FROM ordered
			HAVING COALESCE(MAX(range_end), 0) < (SELECT effective_keyspace FROM scheduling_units WHERE id = $1)
		)
		SELECT gap_start, gap_end FROM (
			SELECT * FROM mid_gaps
			UNION ALL
			SELECT * FROM tail_gap
		) AS gaps
		ORDER BY gap_start ASC
		LIMIT 1
	`
	var gap models.UndispatchedRange
	err := database.QueryRowContext(ctx, query, unitID).Scan(&gap.Start, &gap.End)
	if err == sql.ErrNoRows {
		return models.UndispatchedRange{}, false, nil
	}
	if err != nil {
		return models.UndispatchedRange{}, false, err
	}
	return gap, true, nil
}

// sizeChunk returns the chunk size in BASE units, given the available
// gap, the agent's speed, and the target/min chunk durations.
//
// Target: speed * target_seconds. Floor: speed * min_seconds, unless the
// remaining gap is smaller than the floor — in that case we take the
// whole gap (per plan §8.4 — don't leave tiny gaps orphaned).
func sizeChunk(gapSize int64, speed int64, targetSec, minSec int) int64 {
	if gapSize <= 0 {
		return 0
	}
	target := speed * int64(targetSec)
	if target <= 0 {
		// Defensive: zero or negative target means we don't have a
		// useful sizing signal. Take the whole gap; the dispatcher's
		// caller can decide whether to clamp.
		return gapSize
	}
	if target > gapSize {
		// Gap fits within target — take the whole thing.
		return gapSize
	}

	floor := speed * int64(minSec)
	if floor > gapSize {
		// Gap is smaller than floor — take whole gap.
		return gapSize
	}
	if target < floor {
		// Pathological config (target < min); honor the floor.
		return floor
	}
	return target
}
