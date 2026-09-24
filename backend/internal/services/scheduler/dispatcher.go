package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
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

	// CloudChunkSeconds is the target chunk duration for RENTED agents
	// (system setting 'cloud_chunk_duration_seconds', default ~3x the on-prem
	// value). Per-chunk overhead — hashcat startup, kernel autotune, wordlist
	// load — is billed at rental rates, so cloud chunks are deliberately
	// longer. Zero falls back to TargetChunkSeconds.
	CloudChunkSeconds int

	// CloudTTLRemaining is agent_id -> seconds of life left, for rented
	// agents only. A chunk is NEVER planned past an instance's death: doing
	// so claims a keyspace interval the instance cannot finish, and that
	// interval stays claimed until the sweeper evicts it (network grace plus
	// a sweep tick), during which nobody works it.
	CloudTTLRemaining map[int]int

	// CloudTeardownSlackSeconds is reserved at the end of a rented instance's
	// TTL for drain and destroy.
	CloudTeardownSlackSeconds int

	// EndgameShares is unit_id -> agent_id -> chunk size in BASE units,
	// precomputed by the caller when a unit has entered the endgame.
	//
	// Computed in the caller, not here, because tapering needs the SET of
	// agents on a unit while dispatchOne only ever sees one allocation at a
	// time inside its own transaction.
	EndgameShares map[uuid.UUID]map[int]int64
}

// DispatchedTask is the output handed back to the scheduling cycle so it
// can fire WebSocket task_assignment messages.
type DispatchedTask struct {
	TaskID     uuid.UUID
	UnitID     uuid.UUID
	AgentID    int
	RangeStart int64
	RangeEnd   int64
	// Effective-keyspace range (base × multiplier). Forwarded to the
	// agent in the task assignment so non-hashcat executors (mock) can
	// report effective progress without re-deriving the multiplier.
	// Zero when the multiplier wasn't computable (effective coords
	// stored NULL in the task row — see overflow guard above).
	EffectiveKeyspaceStart int64
	EffectiveKeyspaceEnd   int64
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

	// Intra-cycle dedup: an agent that already received a task this cycle
	// is busy from our perspective even though no DB row has flipped to
	// 'running' yet. Without this guard, the allocator (which works from a
	// single getIdleAgents snapshot per cycle) can hand the same agent to
	// multiple unit allocations and we'd dispatch N tasks before the agent
	// has acknowledged any of them. The agent rejects all but the first,
	// causing zombie 'assigned' rows and benchmark-failure attribution
	// against a perfectly healthy agent.
	dispatchedThisCycle := make(map[int]bool, len(in.Allocations))

	for _, alloc := range in.Allocations {
		if dispatchedThisCycle[alloc.AgentID] {
			debug.Debug("scheduler-v2: agent %d already dispatched this cycle; deferring alloc for unit %s to next cycle", alloc.AgentID, alloc.UnitID)
			continue
		}

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
			dispatchedThisCycle[alloc.AgentID] = true
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
	//
	// All values below are in BASE keyspace units (what hashcat's
	// --skip/--limit operate on for -a 0 with rules). The agent benchmark
	// speed is in EFFECTIVE hashes/sec; we divide by the
	// effective/base multiplier inside sizeChunk to convert to a
	// base-words-per-second rate. Without this conversion the chunk
	// blows up by the rule × salt multiplier and hashcat ignores
	// --limit because it exceeds the wordlist size.
	//
	// The unit's BaseKeyspace may be nil for rows that predate
	// migration 000151 (legacy units that weren't backfilled). Skip
	// dispatch in that case — the unit needs base_keyspace set before
	// we can chunk correctly.
	if unit.BaseKeyspace == nil || *unit.BaseKeyspace <= 0 {
		debug.Warning("scheduler-v2: unit %s has nil/zero base_keyspace; skipping dispatch (re-create the job after migration 000151 has backfilled)", unit.ID)
		return nil, nil
	}
	baseKeyspace := *unit.BaseKeyspace
	speed, hasSpeed := in.AgentSpeeds[alloc.AgentID]
	if !hasSpeed || speed <= 0 {
		speed = ConservativeAgentSpeed
	}

	// Chunk duration: system default, then the per-job value, then the cloud
	// floor for rented agents, then the TTL clamp. See resolveChunkDuration for
	// why the cloud value is a floor rather than an override.
	var jobChunkSize sql.NullInt32
	if qerr := database.QueryRowContext(ctx, `
		SELECT chunk_size_seconds FROM job_executions WHERE id = $1
	`, unit.ParentJobID).Scan(&jobChunkSize); qerr != nil {
		// Previously swallowed. A failed read is not "the job has no
		// preference" — it is a missing signal, and on a rented agent it is the
		// difference between a 20-minute and a 60-minute chunk.
		debug.Warning("scheduler-v2: reading chunk_size_seconds for job %s failed (%v); falling back to the system target",
			unit.ParentJobID, qerr)
	}
	jobChunkSec := 0
	if jobChunkSize.Valid {
		jobChunkSec = int(jobChunkSize.Int32)
	}

	ttlRemaining, isCloud := in.CloudTTLRemaining[alloc.AgentID]
	plan := resolveChunkDuration(
		in.TargetChunkSeconds,
		jobChunkSec,
		in.CloudChunkSeconds,
		in.MinChunkSeconds,
		ttlRemaining,
		in.CloudTeardownSlackSeconds,
		isCloud,
	)
	if plan.Skip {
		// Not enough life left to do anything useful. Dispatching here would
		// claim an interval the instance cannot finish.
		debug.Info("scheduler-v2: skipping dispatch to cloud agent %d — only %ds of TTL left", alloc.AgentID, ttlRemaining)
		return nil, nil
	}
	chunkDurationSec := plan.DurationSec

	chunkSize := sizeChunk(
		gap.End-gap.Start,
		baseKeyspace,
		unit.EffectiveKeyspace.Big(),
		speed,
		chunkDurationSec,
		in.MinChunkSeconds,
	)

	// Endgame tapering overrides the time-boxed size so every agent on this
	// unit converges on the same finish time. Without it, a fast rented GPU
	// finishes the tail and then bills while a slow on-prem card grinds a
	// chunk it started ten minutes earlier.
	if shares, ok := in.EndgameShares[unit.ID]; ok {
		if share, ok := shares[alloc.AgentID]; ok && share > 0 {
			chunkSize = share
			if avail := gap.End - gap.Start; chunkSize > avail {
				chunkSize = avail
			}
		}
	}
	rangeStart := gap.Start
	rangeEnd := gap.Start + chunkSize
	// Defense in depth: never dispatch past the wordlist end. The gap
	// query should already cap at base_keyspace, but this guards
	// against any future drift.
	if rangeEnd > baseKeyspace {
		rangeEnd = baseKeyspace
	}

	// Effective-coordinate snapshot for the bar visualization. Computed as an
	// EXACT big.Int multiply-then-divide from the unit's authoritative
	// effective/base pair (effective × range / base) — NOT a pre-divided
	// multiplier. The old EffectiveKeyspace.DivInt64(base) truncated the ratio
	// to an integer first (base×2.9999 → 2), so effEnd came out at base×2 instead
	// of base×3. Multiply-then-divide floors only at the end, so an exact base×k
	// unit yields exactly k×range.
	//
	// Step 11c correction: coords are LAYER-LOCAL (no cumulative offset).
	// Each layer is conceptually its own self-contained job; layer-N's
	// task at base [0, X) gets eff [0, X×effective/base). The frontend
	// computes the per-layer display offset for increment-mode bar
	// rendering (Step 11d) — the math doesn't live in the backend.
	// effective_keyspace is NUMERIC (base × rules × salts can exceed int64),
	// so the effective coords are computed in big.Int and stored into the
	// NUMERIC effective_keyspace_start/end columns.
	//
	// Tripwire (Step 11f): a multi-character mask or rule-bearing wordlist
	// should always produce effective > base. effective <= base on a layer
	// with base > 1 indicates that something has shrunk
	// scheduling_units.effective_keyspace post-creation (regression of
	// Step 7h2, a future bug, or stale data). Log loudly so the next
	// dispatch makes it visible instead of silently producing broken
	// bar coords.
	if unit.EffectiveKeyspace.CmpInt64(baseKeyspace) <= 0 && baseKeyspace > 1 {
		debug.Warning("scheduler-v2: suspicious effective<=base (eff=%s, base=%d) for unit %s — effective coords may be wrong; check that no code path is shrinking scheduling_units.effective_keyspace post-creation",
			unit.EffectiveKeyspace.String(), baseKeyspace, unit.ID)
	}
	effStart := unit.EffectiveKeyspace.MulInt64(rangeStart).DivInt64(baseKeyspace)
	effEnd := unit.EffectiveKeyspace.MulInt64(rangeEnd).DivInt64(baseKeyspace)
	// big.Int can't overflow; a reversed ordering would only come from bad
	// inputs. Fall back to NULL effective coords in that case (the bar's
	// frontend has a fallback to base coords).
	var effStartArg, effEndArg interface{} = effStart, effEnd
	if effEnd.Cmp(effStart) < 0 {
		debug.Warning("scheduler-v2: effective coord ordering invalid for unit %s (rangeEnd=%d, eff=%s); leaving NULL",
			unit.ID, rangeEnd, unit.EffectiveKeyspace.String())
		effStartArg = nil
		effEndArg = nil
	}

	// Step 11a: for increment-mode jobs, look up the layer_id so the
	// task row carries it. Without this, HandleTaskCompletion's
	// layer-completion cascade (which gates on task.IncrementLayerID
	// != nil) never fires for v2 tasks and layers stay 'pending'
	// indefinitely.
	var incrementLayerIDArg interface{}
	if unit.LayerIndex > 0 {
		var layerID uuid.UUID
		if qerr := database.QueryRowContext(ctx, `
			SELECT id FROM job_increment_layers
			WHERE job_execution_id = $1 AND layer_index = $2
		`, unit.ParentJobID, unit.LayerIndex).Scan(&layerID); qerr != nil {
			debug.Warning("scheduler-v2: increment_layer_id lookup failed for unit %s (layer %d): %v", unit.ID, unit.LayerIndex, qerr)
		} else {
			incrementLayerIDArg = layerID
		}
	}

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
		// Legacy columns attack_cmd / chunk_number are intentionally NOT
		// set. Scheduler-v2 tasks don't carry a server-built hashcat
		// command (the agent constructs it from the task_assignment
		// payload) and they aren't sequence-numbered (coverage is
		// tracked via job_keyspace_intervals). The model fields are
		// *string / *int and the repository scanner uses sql.NullString
		// / sql.NullInt32, so NULL is now legal at the row layer. Hard
		// cutover drops the columns entirely.
		// is_keyspace_split = true: this task's range is a chunk of the
		// unit's keyspace, and the effective_keyspace_start/end above are
		// the AUTHORITATIVE pre-computed proportional values. Without
		// this flag, the legacy progress-update path at
		// job_websocket_integration.go's `UpdateTaskEffectiveKeyspaceWithChunkSize`
		// branch overwrites our eff coords with hashcat's progress[1]
		// (which is per-chunk and gives a different/wrong meaning for
		// our case). The comment at that site explicitly says "we
		// already calculated proportional values during task creation"
		// — v2 is exactly that case.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO job_tasks (
				id, job_execution_id, agent_id, status,
				keyspace_start, keyspace_end, chunk_duration,
				scheduling_unit_id, range_start, range_end,
				effective_keyspace_start, effective_keyspace_end,
				increment_layer_id,
				is_keyspace_split,
				last_activity_at, benchmark_speed
			) VALUES (
				$1, $2, $3, 'assigned',
				$4, $5, $6,
				$7, $4, $5,
				$10, $11,
				$12,
				true,
				$8, $9
			)
		`,
			taskID,
			unit.ParentJobID,
			alloc.AgentID,
			rangeStart, rangeEnd,
			chunkDurationSec, // resolveChunkDuration: system default -> per-job value -> cloud floor -> TTL clamp
			unit.ID,
			now,
			speed,
			effStartArg, effEndArg,
			incrementLayerIDArg,
		)
		if err != nil {
			return fmt.Errorf("insert task: %w", err)
		}

		// Step 11e: transition unit + layer status pending->running on
		// the first dispatch. Idempotent (gated on status='pending');
		// subsequent dispatches no-op.
		if _, err := tx.ExecContext(ctx, `
			UPDATE scheduling_units SET status = 'running', updated_at = NOW()
			WHERE id = $1 AND status = 'pending'
		`, unit.ID); err != nil {
			// Best-effort: log only, don't fail the dispatch.
			debug.Warning("scheduler-v2: failed to transition unit %s to running: %v", unit.ID, err)
		}
		if incrementLayerIDArg != nil {
			if _, err := tx.ExecContext(ctx, `
				UPDATE job_increment_layers
				SET status = 'running', started_at = COALESCE(started_at, NOW()), updated_at = NOW()
				WHERE id = $1 AND status = 'pending'
			`, incrementLayerIDArg); err != nil {
				debug.Warning("scheduler-v2: failed to transition layer to running: %v", err)
			}
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

	dt := &DispatchedTask{
		TaskID:     taskID,
		UnitID:     unit.ID,
		AgentID:    alloc.AgentID,
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
	}
	// Mirror the persisted effective coords onto the dispatch result so
	// the cycle can forward them to the agent. Skipped when the overflow
	// guard above zeroed effStartArg/effEndArg — leaving the dispatched
	// task's effective fields zero signals "unknown" to the agent.
	//
	// The agent payload keeps these as int64 (mock executors use them only
	// for effective-progress display; the full-precision values live in the
	// NUMERIC effective_keyspace_start/end columns). .Int64() truncates only
	// in the extreme >9.2e18 case, which is acceptable for that display.
	if effStartArg != nil && effEndArg != nil {
		dt.EffectiveKeyspaceStart = effStart.Int64()
		dt.EffectiveKeyspaceEnd = effEnd.Int64()
	}
	return dt, nil
}

// firstGap returns the smallest-start undispatched range for a unit, or
// (zero, false, nil) if there is no gap.
//
// Gap arithmetic operates in BASE keyspace units. The tail bound is the
// unit's base_keyspace (= wordlist size for -a 0), not effective_keyspace —
// effective_keyspace decreases as salts get removed, but the chunkable
// dimension (number of base words to feed hashcat) is invariant. Using
// effective_keyspace here would falsely report the unit "done" as soon as
// a few salts crack.
//
// Units that predate migration 000151 may have NULL base_keyspace; the
// query returns no rows in that case (HAVING fails) and the dispatcher
// skips the unit until it's backfilled.
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
			       (SELECT base_keyspace FROM scheduling_units WHERE id = $1) AS gap_end
			FROM ordered
			HAVING COALESCE(MAX(range_end), 0) < (SELECT base_keyspace FROM scheduling_units WHERE id = $1)
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

// sizeChunk returns the chunk size in BASE keyspace units (what hashcat's
// --skip/--limit operate on for -a 0 with rules). Mirrors the v1 formula
// at job_scheduling_task_assignment.go:1198-1211:
//
//	multiplier    = effectiveKeyspace / baseKeyspace   // rules × salts
//	basePerSec    = speed / multiplier                  // base words/sec
//	chunkBase     = basePerSec × targetSeconds
//
// Inputs:
//
//	gapBase            — available gap size in BASE units
//	baseKeyspace       — total chunkable dimension (e.g., wordlist size)
//	effectiveKeyspace  — current total effective work (base × rules × salts);
//	                     updated continuously by IngestProgressV2 as salts
//	                     get removed, so the multiplier auto-adjusts
//	speed              — agent's benchmark speed in EFFECTIVE hashes/sec
//	targetSec, minSec  — sizing knobs (system settings)
//
// If baseKeyspace or effectiveKeyspace is missing/zero we can't compute a
// multiplier; fall back to taking the whole gap so we make progress while
// the next agent benchmark / first-progress refines the unit.
// chunkPlan is the outcome of chunk-duration resolution for one allocation.
type chunkPlan struct {
	// DurationSec is the target wall time for this chunk, in seconds.
	DurationSec int
	// Skip is true when a rented agent has too little life left for the
	// dispatch to be worth making at all.
	Skip bool
}

/*
 * resolveChunkDuration picks the target wall time for one chunk.
 *
 * Rules, in the order they bind (later wins):
 *
 *  1. targetSec   — the system default (default_chunk_duration, falling back to
 *     target_chunk_seconds), used when the job carries nothing.
 *  2. jobChunkSec — job_executions.chunk_size_seconds, when > 0.
 *  3. cloudSec    — for RENTED agents only, a FLOOR rather than an override.
 *  4. the TTL clamp — always last, and the only rule that may lower the result
 *     below the floor.
 *
 * WHY THE CLOUD VALUE IS A FLOOR AND NOT A PRECEDENCE QUESTION.
 *
 * job_executions.chunk_size_seconds is INT DEFAULT 900 (migration 000049) and
 * its preset/workflow source is NOT NULL (migration 000018); the preset form
 * fills the system default into the field on blur. So the column is never NULL
 * and never 0, and "the operator chose 1200" is indistinguishable from "the
 * create form pre-filled 1200".
 *
 * Treating that inherited number as a deliberate override is what made
 * cloud_chunk_duration_seconds unreachable: the old code applied the cloud
 * value and then overwrote it on 100% of jobs, so every rented GPU ran the
 * on-prem chunk size while the setting claimed 3600.
 *
 * The two cases the schema cannot tell apart have the SAME right answer on
 * rented hardware. hashcat startup, kernel autotune and wordlist load are fixed
 * wall time billed at $0.50-$22/hr, so the only thing that shrinks them as a
 * fraction of the bill is a longer chunk. max() rather than assignment, so a
 * job asking for LONGER than the cloud target keeps its own value: the floor
 * only ever overrides "shorter", which is precisely the intent the setting
 * exists to overrule.
 *
 * An operator who genuinely wants short chunks on rented hardware lowers
 * cloud_chunk_duration_seconds; 0 (or an unset key) disables the floor entirely
 * and restores per-job authority on cloud.
 *
 * A rented agent with an unknown or zero TTL is skipped, not dispatched with an
 * unclamped chunk. Preserved from the original: claiming an interval the
 * instance cannot finish strands that keyspace until the sweeper evicts it.
 */
func resolveChunkDuration(
	targetSec, jobChunkSec, cloudSec, minSec int,
	ttlRemaining, teardownSlack int,
	isCloud bool,
) chunkPlan {
	dur := targetSec
	if jobChunkSec > 0 {
		dur = jobChunkSec
	}

	if !isCloud {
		return chunkPlan{DurationSec: dur}
	}

	// Cloud floor. Never lowers; only raises.
	if cloudSec > dur {
		dur = cloudSec
	}

	// TTL clamp, unchanged and deliberately last: nothing above it — job value
	// or cloud floor — may outlive the instance it would run on.
	affordable := ttlRemaining - teardownSlack
	if affordable < minSec {
		return chunkPlan{Skip: true}
	}
	if affordable < dur {
		dur = affordable
	}
	return chunkPlan{DurationSec: dur}
}

func sizeChunk(gapBase int64, baseKeyspace int64, effectiveKeyspace *big.Int, speed int64, targetSec, minSec int) int64 {
	if gapBase <= 0 {
		return 0
	}
	if baseKeyspace <= 0 || effectiveKeyspace == nil || effectiveKeyspace.Sign() <= 0 || speed <= 0 {
		// No multiplier signal yet — take the whole gap so progress
		// can begin and the agent's first-progress report can refine
		// effective_keyspace for future cycles.
		return gapBase
	}

	// chunkBase = speed × baseKeyspace × targetSec / effectiveKeyspace, computed
	// ENTIRELY in big.Int. The key is folding targetSec INTO the division rather
	// than first truncating speed×baseKeyspace/effectiveKeyspace to an int64
	// "base words/sec": when one base word already costs more than a second of
	// budget (effectiveKeyspace ≫ speed×baseKeyspace — e.g. a heavily-salted
	// WPA list with a ~264k multiplier), that intermediate rate floors to 0, the
	// old code clamped it to 1, and emitted a 1×targetSec chunk that ran for
	// HOURS. Folding targetSec in keeps the fractional rate, yielding the right
	// (small) base-word count. big.Int also makes the intermediate
	// speed×baseKeyspace (which overflows int64 for huge wordlists + fast hashes)
	// safe regardless of magnitude.
	speedBase := new(big.Int).Mul(big.NewInt(speed), big.NewInt(baseKeyspace))

	targetBig := new(big.Int).Mul(speedBase, big.NewInt(int64(targetSec)))
	targetBig.Div(targetBig, effectiveKeyspace)
	if !targetBig.IsInt64() {
		// Even speed×baseKeyspace×targetSec/effectiveKeyspace exceeds int64
		// (huge unsalted wordlist + fast hash, tiny multiplier): the chunk
		// would be larger than any realistic gap, so take the whole gap.
		return gapBase
	}
	target := targetBig.Int64()
	if target < 1 {
		// Sub-1 base word for the whole target window (extreme multiplier):
		// dispatch the smallest indivisible unit; the chunk-overrun guard
		// stops it if it runs long.
		target = 1
	}
	if target > gapBase {
		return gapBase
	}

	// minSec floor — same fold-in-big.Int treatment so it never collapses to 0.
	floorBig := new(big.Int).Mul(speedBase, big.NewInt(int64(minSec)))
	floorBig.Div(floorBig, effectiveKeyspace)
	floor := int64(1)
	if floorBig.IsInt64() && floorBig.Int64() > 1 {
		floor = floorBig.Int64()
	}
	if floor > gapBase {
		return gapBase
	}
	if target < floor {
		return floor
	}
	return target
}
