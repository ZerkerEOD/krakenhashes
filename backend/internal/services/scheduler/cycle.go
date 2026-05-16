package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// WSSender is the subset of the WebSocket service the cycle needs.
// Defined as an interface so tests can swap a fake.
type WSSender interface {
	SendMessage(agentID int, msg *wsservice.Message) error
	GetConnectedAgents() []int
}

// Cycle is the scheduler-v2 dispatch pipeline. Holds long-lived
// dependencies; RunOnce performs one full pass and returns a result.
type Cycle struct {
	db                 *db.DB
	unitRepo           *repository.SchedulingUnitRepository
	intervalRepo       *repository.KeyspaceIntervalRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	wsSender           WSSender
	// binaryResolver may be nil — if so, sendAssignment falls back to
	// an empty BinaryPath (the agent will fail to spawn hashcat, but
	// the rest of the dispatch path still exercises). Production
	// wiring passes services.JobExecutionService.
	binaryResolver BinaryResolver
	// compatCache replaces the per-cycle linear scan with a cached
	// lookup. May be nil — if so, compatFn falls back to the inline
	// version.IsCompatibleStr call (correct but slower).
	compatCache *CompatCache
}

// NewCycle wires the dependencies. binaryResolver and compatCache may
// be nil for tests that don't exercise those code paths.
func NewCycle(
	database *db.DB,
	unitRepo *repository.SchedulingUnitRepository,
	intervalRepo *repository.KeyspaceIntervalRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	wsSender WSSender,
	binaryResolver BinaryResolver,
	compatCache *CompatCache,
) *Cycle {
	return &Cycle{
		db:                 database,
		unitRepo:           unitRepo,
		intervalRepo:       intervalRepo,
		systemSettingsRepo: systemSettingsRepo,
		wsSender:           wsSender,
		binaryResolver:     binaryResolver,
		compatCache:        compatCache,
	}
}

// CycleResult is the per-cycle summary, used for logging and tests.
type CycleResult struct {
	UnitsSchedulable int
	IdleAgents       int
	Allocations      int
	Dispatched       int
	Errors           []error
}

// RunOnce performs a single scheduling cycle: select schedulable
// units, find idle compatible agents, allocate them, dispatch one
// chunk per allocation, fan out WebSocket task_assignment messages.
//
// Per-step errors are accumulated in CycleResult.Errors so a single bad
// row doesn't abort the cycle. The cycle returns a non-nil error only
// when an early-stage failure (e.g., GetSchedulable) makes the rest
// pointless.
func (c *Cycle) RunOnce(ctx context.Context) (CycleResult, error) {
	res := CycleResult{}

	// Step 3: select schedulable units (Step 1 = sweeper, runs separately
	// via SweeperRunner; Step 2 = compatibility cache refresh, deferred
	// to a later phase — for now we recompute compatibility per cycle).
	units, err := SelectSchedulableUnits(ctx, c.unitRepo, c.intervalRepo)
	if err != nil {
		return res, fmt.Errorf("cycle: select schedulable: %w", err)
	}
	res.UnitsSchedulable = len(units)
	if len(units) == 0 {
		return res, nil
	}

	// Build UnitInfo with active-agent counts and binary versions
	// (joined from parent job_execution — scheduling_units doesn't
	// carry binary_version yet; Phase E may add it for perf).
	unitInfos, unitsByID, err := c.buildUnitInfos(ctx, units)
	if err != nil {
		return res, fmt.Errorf("cycle: build unit infos: %w", err)
	}

	// Step 2 (per-cycle rebuild): find idle compatible agents.
	agentInfos, err := c.getIdleAgents(ctx)
	if err != nil {
		return res, fmt.Errorf("cycle: get idle agents: %w", err)
	}
	res.IdleAgents = len(agentInfos)
	if len(agentInfos) == 0 {
		return res, nil
	}

	// Compatibility closure: prefer the cache when wired, fall back
	// to the inline closure for tests / setups that didn't construct
	// a CompatCache. Functionally identical; the cache just avoids
	// the per-cycle O(units × agents) version-pattern parse.
	var compatFn CompatibilityFn
	if c.compatCache != nil {
		compatFn = c.compatCache.CompatFn(ctx)
	} else {
		compatFn = func(unitID uuid.UUID, agentID int) bool {
			var unitVer, agentVer string
			for _, u := range unitInfos {
				if u.ID == unitID {
					unitVer = u.BinaryVersion
					break
				}
			}
			for _, a := range agentInfos {
				if a.ID == agentID {
					agentVer = a.BinaryVersion
					break
				}
			}
			if unitVer == "" {
				return true
			}
			return version.IsCompatibleStr(agentVer, unitVer)
		}
	}

	// Step 4: allocate.
	mode := c.readOverflowMode(ctx)
	allocations := AllocateAgentsByPriority(unitInfos, agentInfos, mode, compatFn)
	res.Allocations = len(allocations)
	if len(allocations) == 0 {
		return res, nil
	}

	// Step 5: dispatch.
	targetChunkSec := c.readIntSetting(ctx, "target_chunk_seconds", 60)
	minChunkSec := c.readIntSetting(ctx, "min_chunk_seconds", 5)

	dispatchIn := DispatchInputs{
		Allocations:        allocations,
		Units:              unitsByID,
		AgentSpeeds:        c.readAgentSpeeds(ctx, allocations, unitsByID),
		TargetChunkSeconds: targetChunkSec,
		MinChunkSeconds:    minChunkSec,
	}
	dispatched, dispatchErrs := DispatchOneChunkPerAgent(ctx, c.db, dispatchIn)
	res.Errors = append(res.Errors, dispatchErrs...)
	res.Dispatched = len(dispatched)

	// Step 6: fan out WebSocket task_assignment messages.
	for _, dt := range dispatched {
		unit := unitsByID[dt.UnitID]
		if unit == nil {
			res.Errors = append(res.Errors, fmt.Errorf("cycle: missing unit for dispatched task %s", dt.TaskID))
			continue
		}
		if err := c.sendAssignment(ctx, dt, unit, targetChunkSec); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("cycle: send task %s to agent %d: %w", dt.TaskID, dt.AgentID, err))
			// Don't roll back the DB insert. The heartbeat sweep
			// recovers if the agent never gets the message.
		}
	}

	// Step 7: preemption. If any schedulable units got zero
	// allocations this cycle, look for compatible lower-priority
	// running tasks we can stop to free agents for them. The
	// preempted tasks become gaps via the existing graceful-
	// shutdown -> RecoverTaskByID flow, and the freed agents are
	// picked up in the next cycle.
	starving := computeStarvingUnits(unitInfos, allocations)
	if len(starving) > 0 {
		preempted, perrs := FindAndPreempt(ctx, c.db, c.wsSender, starving, compatFn)
		res.Errors = append(res.Errors, perrs...)
		if len(preempted) > 0 {
			debug.Info("scheduler-v2: issued %d preemption(s)", len(preempted))
		}
	}

	return res, nil
}

// computeStarvingUnits returns the schedulable units (from the
// allocator's input) that ended up with zero allocations this cycle.
// Only those at non-zero priority are eligible — priority-0 jobs
// never preempt anything (and at the bottom of the queue there's
// nothing lower to preempt anyway).
func computeStarvingUnits(units []UnitInfo, allocations []Allocation) []UnitInfo {
	allocated := map[uuid.UUID]bool{}
	for _, a := range allocations {
		allocated[a.UnitID] = true
	}
	var starving []UnitInfo
	for _, u := range units {
		if u.Priority <= 0 {
			continue
		}
		if !allocated[u.ID] {
			starving = append(starving, u)
		}
	}
	return starving
}

// buildUnitInfos converts SchedulingUnit rows into the allocator's
// UnitInfo. Joins job_executions to get the binary_version. Also returns
// a map of unit_id -> *SchedulingUnit for the dispatcher's lookup needs.
func (c *Cycle) buildUnitInfos(ctx context.Context, units []*models.SchedulingUnit) ([]UnitInfo, map[uuid.UUID]*models.SchedulingUnit, error) {
	if len(units) == 0 {
		return nil, nil, nil
	}

	// Collect parent job IDs and query binary_version for each.
	parentIDs := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		parentIDs = append(parentIDs, u.ParentJobID)
	}

	// Single query: parent_id -> binary_version. Use ANY() with a UUID
	// array; lib/pq handles the array marshalling.
	binaryVersions := make(map[uuid.UUID]string)
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, COALESCE(binary_version, '') FROM job_executions
		WHERE id = ANY($1::uuid[])
	`, uuidSliceToTextArray(parentIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("query binary_versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var ver string
		if err := rows.Scan(&id, &ver); err != nil {
			return nil, nil, fmt.Errorf("scan binary_version: %w", err)
		}
		binaryVersions[id] = ver
	}

	// Get active-agent counts per unit. One query: unit_id -> count of
	// (assigned, running) tasks. Tasks without scheduling_unit_id are
	// legacy and excluded.
	counts, err := c.activeAgentCountsByUnit(ctx, units)
	if err != nil {
		return nil, nil, err
	}

	infos := make([]UnitInfo, 0, len(units))
	byID := make(map[uuid.UUID]*models.SchedulingUnit, len(units))
	for _, u := range units {
		infos = append(infos, UnitInfo{
			ID:               u.ID,
			Priority:         u.Priority,
			MaxAgents:        u.MaxAgents,
			BinaryVersion:    binaryVersions[u.ParentJobID],
			ActiveAgentCount: counts[u.ID],
			CreatedAtNanos:   u.CreatedAt.UnixNano(),
		})
		byID[u.ID] = u
	}
	return infos, byID, nil
}

func (c *Cycle) activeAgentCountsByUnit(ctx context.Context, units []*models.SchedulingUnit) (map[uuid.UUID]int, error) {
	ids := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	out := map[uuid.UUID]int{}
	rows, err := c.db.QueryContext(ctx, `
		SELECT scheduling_unit_id, COUNT(*)
		FROM job_tasks
		WHERE scheduling_unit_id = ANY($1::uuid[])
		  AND status IN ('assigned', 'running')
		GROUP BY scheduling_unit_id
	`, uuidSliceToTextArray(ids))
	if err != nil {
		return nil, fmt.Errorf("query active agent counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan active count: %w", err)
		}
		out[id] = n
	}
	return out, nil
}

// getIdleAgents returns agents that are:
//   - currently WebSocket-connected (per wsSender.GetConnectedAgents),
//   - enabled and sync-completed,
//   - not currently running a scheduler-v2 task.
//
// The query intersects with the connected-agent set in memory because
// WebSocket connectedness lives in the handler, not the database.
func (c *Cycle) getIdleAgents(ctx context.Context) ([]AgentInfo, error) {
	connected := c.wsSender.GetConnectedAgents()
	if len(connected) == 0 {
		return nil, nil
	}

	// Postgres int[] -> use ANY().
	connectedArr := intSliceToBigintArray(connected)

	rows, err := c.db.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.binary_version, '')
		FROM agents a
		WHERE a.id = ANY($1::bigint[])
		  AND a.is_enabled = true
		  AND a.sync_status = 'completed'
		  AND NOT EXISTS (
			  SELECT 1 FROM job_tasks t
			  WHERE t.agent_id = a.id
				AND t.status IN ('assigned', 'running')
				AND t.scheduling_unit_id IS NOT NULL
		  )
	`, connectedArr)
	if err != nil {
		return nil, fmt.Errorf("query idle agents: %w", err)
	}
	defer rows.Close()

	var out []AgentInfo
	for rows.Next() {
		var id int
		var ver string
		if err := rows.Scan(&id, &ver); err != nil {
			return nil, fmt.Errorf("scan idle agent: %w", err)
		}
		out = append(out, AgentInfo{
			ID:             id,
			BinaryVersion:  ver,
			BenchmarkSpeed: 0, // filled later by readAgentSpeeds
		})
	}
	return out, nil
}

// readAgentSpeeds returns a (agent_id -> speed) map populated from
// agent_benchmarks for the (attack_mode, hash_type, salt_count) combo
// each allocated unit needs. Missing entries fall through to
// ConservativeAgentSpeed in the dispatcher.
//
// Salt-aware lookup:
//   - For salted hash types, the desired salt_count is the parent
//     hashlist's total_hashes (per the legacy benchmark insertion
//     convention at job_scheduling_benchmark_planning.go).
//   - For non-salted hash types, the desired salt_count is NULL.
//   - The query prefers an exact (NULL-safe) salt_count match and
//     falls back to the most-recent benchmark for the combo with any
//     salt count, so a new salt count doesn't kill the chunk size.
func (c *Cycle) readAgentSpeeds(ctx context.Context, allocations []Allocation, unitsByID map[uuid.UUID]*models.SchedulingUnit) map[int]int64 {
	out := map[int]int64{}
	for _, alloc := range allocations {
		u := unitsByID[alloc.UnitID]
		if u == nil {
			continue
		}
		hashType, saltCount := c.lookupHashTypeAndSalt(ctx, u.ParentJobID)
		if hashType < 0 {
			continue
		}
		var speed int64
		// IS NOT DISTINCT FROM is NULL-safe equality. The ORDER BY
		// pushes the exact-salt match to the top, then most-recent.
		err := c.db.QueryRowContext(ctx, `
			SELECT speed FROM agent_benchmarks
			WHERE agent_id = $1 AND attack_mode = $2 AND hash_type = $3
			ORDER BY
			  CASE WHEN salt_count IS NOT DISTINCT FROM $4 THEN 0 ELSE 1 END,
			  updated_at DESC
			LIMIT 1
		`, alloc.AgentID, u.AttackMode, hashType, saltCount).Scan(&speed)
		if err == nil && speed > 0 {
			out[alloc.AgentID] = speed
		}
	}
	return out
}

// lookupHashTypeAndSalt resolves (hash_type_id, desired_salt_count)
// for a scheduling unit. desired_salt_count is the hashlist's
// total_hashes when the hash type is salted, or sql.NullInt64{}
// (meaning NULL) otherwise. Returns (-1, NullInt64{}) on any lookup
// failure; the caller treats that as "unknown" and skips speed
// lookup.
func (c *Cycle) lookupHashTypeAndSalt(ctx context.Context, parentJobID uuid.UUID) (int, sql.NullInt64) {
	var ht int
	var isSalted bool
	var totalHashes int
	err := c.db.QueryRowContext(ctx, `
		SELECT h.hash_type_id, ht.is_salted, h.total_hashes
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN hash_types ht ON ht.id = h.hash_type_id
		WHERE je.id = $1
	`, parentJobID).Scan(&ht, &isSalted, &totalHashes)
	if err != nil {
		return -1, sql.NullInt64{}
	}
	if isSalted && totalHashes > 0 {
		return ht, sql.NullInt64{Int64: int64(totalHashes), Valid: true}
	}
	return ht, sql.NullInt64{} // NULL salt_count
}

// sendAssignment composes the WebSocket TaskAssignmentPayload for a
// dispatched chunk and ships it to the agent. Fills mode-specific
// fields via BuildTaskAssignment, then enriches with the legacy
// fields the agent expects (HashlistPath, HashType, BinaryPath,
// ChunkDuration, etc.) by joining the parent job_execution and
// hashlist.
//
// For Phase B these enrichments are minimal — Phase E will pre-
// populate them on the scheduling_unit at creation time, eliminating
// the per-dispatch lookups.
func (c *Cycle) sendAssignment(ctx context.Context, dt DispatchedTask, unit *models.SchedulingUnit, chunkDuration int) error {
	payload, err := BuildTaskAssignment(unit, dt.TaskID, dt.RangeStart, dt.RangeEnd)
	if err != nil {
		return fmt.Errorf("build assignment: %w", err)
	}

	// Enrich from parent job_execution + hashlist. Path format matches
	// the legacy emitter at job_websocket_integration.go:602,1106 —
	// the agent expects the processed-into-DB version at
	// "hashlists/<id>.hash". Mode 9 additionally needs the original
	// file path so hashcat can pair hashes with candidates by line
	// order; that lives in hashlists.original_file_path.
	var hashlistID int64
	var hashType int
	var originalFilePath string
	err = c.db.QueryRowContext(ctx, `
		SELECT je.hashlist_id, h.hash_type_id, COALESCE(h.original_file_path, '')
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1
	`, unit.ParentJobID).Scan(&hashlistID, &hashType, &originalFilePath)
	if err != nil {
		return fmt.Errorf("lookup parent + hashlist: %w", err)
	}

	payload.HashlistID = hashlistID
	payload.HashType = hashType
	payload.HashlistPath = fmt.Sprintf("hashlists/%d.hash", hashlistID)
	payload.ChunkDuration = chunkDuration
	payload.ReportInterval = 5
	if unit.AttackMode == AttackModeAssociation && originalFilePath != "" {
		payload.OriginalHashlistPath = originalFilePath
	}

	// Resolve binary path via the injected resolver. Matches the
	// legacy format from job_websocket_integration.go:781 —
	// "binaries/<binary_version_id>". Errors here are non-fatal; we
	// log and send the assignment with an empty BinaryPath, which
	// the agent will reject. That's a clear failure signal in the
	// agent log rather than a silent fall-through to "default."
	if c.binaryResolver != nil {
		binaryID, berr := c.binaryResolver.DetermineBinaryForTask(ctx, dt.AgentID, unit.ParentJobID)
		if berr != nil {
			debug.Warning("scheduler-v2: binary resolution failed for task %s (agent %d, job %s): %v",
				dt.TaskID, dt.AgentID, unit.ParentJobID, berr)
		} else {
			payload.BinaryPath = fmt.Sprintf("binaries/%d", binaryID)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	msg := &wsservice.Message{Type: wsservice.TypeTaskAssignment, Payload: body}

	if err := c.wsSender.SendMessage(dt.AgentID, msg); err != nil {
		return fmt.Errorf("send WebSocket: %w", err)
	}
	debug.Info("scheduler-v2 dispatched task %s to agent %d (range [%d, %d))",
		dt.TaskID, dt.AgentID, dt.RangeStart, dt.RangeEnd)
	return nil
}

// readOverflowMode reads the agent_overflow_allocation_mode setting,
// defaulting to fifo if unset / unparseable.
func (c *Cycle) readOverflowMode(ctx context.Context) OverflowMode {
	if c.systemSettingsRepo == nil {
		return OverflowFIFO
	}
	setting, err := c.systemSettingsRepo.GetSetting(ctx, "agent_overflow_allocation_mode")
	if err != nil || setting == nil || setting.Value == nil {
		return OverflowFIFO
	}
	mode := OverflowMode(*setting.Value)
	if !mode.IsValid() {
		return OverflowFIFO
	}
	return mode
}

// readIntSetting reads a system_setting expected to be an integer.
// Returns def on any failure (missing setting, parse error, non-positive).
func (c *Cycle) readIntSetting(ctx context.Context, key string, def int) int {
	if c.systemSettingsRepo == nil {
		return def
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	setting, err := c.systemSettingsRepo.GetSetting(readCtx, key)
	if err != nil || setting == nil || setting.Value == nil {
		return def
	}
	n, err := strconv.Atoi(*setting.Value)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// uuidSliceToTextArray formats a UUID slice as a Postgres text[]
// literal so the driver can cast to uuid[] via the $1::uuid[] hint.
// Used for `WHERE id = ANY(...)` queries. Returns an empty literal for
// empty input.
func uuidSliceToTextArray(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return "{}"
	}
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id.String()
	}
	out += "}"
	return out
}

// intSliceToBigintArray formats an int slice as a Postgres array
// literal usable with $1::bigint[].
func intSliceToBigintArray(ids []int) string {
	if len(ids) == 0 {
		return "{}"
	}
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += strconv.Itoa(id)
	}
	out += "}"
	return out
}
