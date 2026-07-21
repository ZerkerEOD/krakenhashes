package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// IsShuttingDown reports whether the given agent is currently
	// in a graceful-shutdown handshake. The cycle excludes such
	// agents from idle-allocation candidates. Implementations should
	// return false for unknown agent IDs.
	IsShuttingDown(agentID int) bool
	// WasRecentlyRejected reports whether the given agent rejected a
	// task assignment within the rejection-cooldown window. Used by
	// getIdleAgents to break the inter-cycle race where the dispatcher
	// would otherwise immediately re-dispatch to an agent that just
	// rejected (the agent's real task hasn't yet flipped to 'running'
	// in the DB). Implementations should return false for unknown
	// agent IDs.
	WasRecentlyRejected(agentID int) bool
	// IsFileMapReady reports whether the agent has finished its startup
	// file-map build and is safe to dispatch to (GH #61). Returns true
	// unless the agent has EXPLICITLY reported file_map_ready=false on its
	// periodic agent_status message. Fail-open: unknown agents (older
	// versions that never report the field, or agents whose first status
	// hasn't arrived) are treated as ready, mirroring the semantics of
	// IsShuttingDown/WasRecentlyRejected. Implementations should return
	// true for unknown agent IDs.
	IsFileMapReady(agentID int) bool
}

// Cycle is the scheduler-v2 dispatch pipeline. Holds long-lived
// dependencies; RunOnce performs one full pass and returns a result.
type Cycle struct {
	db                 *db.DB
	unitRepo           *repository.SchedulingUnitRepository
	intervalRepo       *repository.KeyspaceIntervalRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	deviceRepo         *repository.AgentDeviceRepository
	agentRepo          *repository.AgentRepository
	scheduleRepo       *repository.AgentScheduleRepository
	wsSender           WSSender
	// binaryResolver may be nil — if so, sendAssignment falls back to
	// an empty BinaryPath (the agent will fail to spawn hashcat, but
	// the rest of the dispatch path still exercises). Production
	// wiring passes services.JobExecutionService.
	binaryResolver BinaryResolver
	// jobStarter transitions the parent job_execution from 'pending'
	// to 'running' on first dispatch. May be nil in tests — production
	// wiring passes services.JobExecutionService. The legacy scheduler
	// did this from inside executeTaskAssignment; the v2 cycle calls
	// it explicitly after each successful dispatch (StartExecution is
	// gated on WHERE status='pending' so the call is idempotent).
	jobStarter JobExecutionStarter
	// compatCache replaces the per-cycle linear scan with a cached
	// lookup. May be nil — if so, compatFn falls back to the inline
	// version.IsCompatibleStr call (correct but slower).
	compatCache *CompatCache

	// diag records per-agent "why idle" diagnostics for the UI. May be
	// nil (capture disabled). Set via SetDiagnostics after construction.
	diag DiagnosticsRecorder

	// running is the single-flight guard. Today, the runner's
	// single-goroutine ticker pattern guarantees no overlap, but a
	// future refactor that adds a manual cycle trigger (e.g., on agent
	// connect or job creation) without remembering to coordinate with
	// the ticker would break that. CompareAndSwap returns false on
	// overlap and we skip — we deliberately don't queue (a sync.Mutex
	// would queue the second caller and produce exactly the pile-up
	// we're guarding against).
	running atomic.Bool

	// fanoutMu serializes appends to CycleResult.Errors from the
	// parallel fan-out goroutines in RunOnce. Held only briefly.
	fanoutMu sync.Mutex
}

// NewCycle wires the dependencies. binaryResolver, jobStarter,
// deviceRepo, agentRepo, scheduleRepo, and compatCache may be nil for
// tests that don't exercise those code paths. In production, agentRepo
// and scheduleRepo together gate idle agents on the agent-scheduling
// window (only run during the agent's approved hours when the global
// `agent_scheduling_enabled` setting is on AND the per-agent toggle is
// on). Mirrors legacy JobExecutionService.filterAvailableAgents.
func NewCycle(
	database *db.DB,
	unitRepo *repository.SchedulingUnitRepository,
	intervalRepo *repository.KeyspaceIntervalRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	deviceRepo *repository.AgentDeviceRepository,
	agentRepo *repository.AgentRepository,
	scheduleRepo *repository.AgentScheduleRepository,
	wsSender WSSender,
	binaryResolver BinaryResolver,
	jobStarter JobExecutionStarter,
	compatCache *CompatCache,
) *Cycle {
	return &Cycle{
		db:                 database,
		unitRepo:           unitRepo,
		intervalRepo:       intervalRepo,
		systemSettingsRepo: systemSettingsRepo,
		deviceRepo:         deviceRepo,
		agentRepo:          agentRepo,
		scheduleRepo:       scheduleRepo,
		wsSender:           wsSender,
		binaryResolver:     binaryResolver,
		jobStarter:         jobStarter,
		compatCache:        compatCache,
	}
}

// SetDiagnostics wires the diagnostics recorder used to explain agent idleness.
// Optional; if never set, idle-reason capture is disabled.
func (c *Cycle) SetDiagnostics(d DiagnosticsRecorder) { c.diag = d }

// recordIdleReasons captures, for each idle-eligible agent that did NOT get a
// task or benchmark this cycle, the reason it's sitting idle — so the agent page
// can explain it. Agents that DID get work have their stale reasons cleared.
// Recording is deduped/buffered by the recorder, so calling this every cycle is
// cheap. compatFn is the same binary-compatibility check the allocator used.
func (c *Cycle) recordIdleReasons(agentInfos []AgentInfo, unitInfos []UnitInfo, allocations []Allocation, benchGaps []BenchmarkGap, compatFn CompatibilityFn) {
	if c.diag == nil {
		return
	}
	busy := make(map[int]bool, len(allocations)+len(benchGaps))
	for _, a := range allocations {
		busy[a.AgentID] = true
	}
	for _, g := range benchGaps {
		busy[g.AgentID] = true
	}
	for _, ag := range agentInfos {
		sid := strconv.Itoa(ag.ID)
		if busy[ag.ID] {
			// Got a task or benchmark this cycle → no longer idle.
			c.diag.ClearScope(models.DiagScopeAgent, sid)
			continue
		}
		if len(unitInfos) == 0 {
			c.diag.Record(models.DiagScopeAgent, sid, models.DiagReasonNoSchedulableWork,
				models.DiagSeverityInfo, "no jobs have dispatchable work right now")
			continue
		}
		compatibleAny := false
		for _, u := range unitInfos {
			if compatFn(u.ID, ag.ID) {
				compatibleAny = true
				break
			}
		}
		if !compatibleAny {
			bin := ag.BinaryVersion
			if bin == "" {
				bin = "default"
			}
			detail := fmt.Sprintf("agent provides binary %q; no schedulable job targets a compatible version", bin)
			c.diag.Record(models.DiagScopeAgent, sid, models.DiagReasonNoCompatibleJob,
				models.DiagSeverityWarning, detail)
			continue
		}
		// Compatible with at least one unit but still unallocated: caps reached
		// or keyspace-saturated this cycle (e.g. enforce_max_agents surplus).
		c.diag.Record(models.DiagScopeAgent, sid, models.DiagReasonAtCapacity,
			models.DiagSeverityInfo, "compatible jobs are at their agent cap or fully dispatched this cycle")
	}
}

// enabledDeviceIDsForAgent returns the hashcat device IDs that should be
// passed via -d to hashcat. Returns nil if all devices are enabled (in
// which case the agent omits -d and hashcat uses everything). Matches
// the legacy behavior at job_websocket_integration.go:797-810.
func (c *Cycle) enabledDeviceIDsForAgent(_ context.Context, agentID int) ([]int, error) {
	if c.deviceRepo == nil {
		return nil, nil
	}
	devices, err := c.deviceRepo.GetByAgentID(agentID)
	if err != nil {
		return nil, fmt.Errorf("get devices for agent %d: %w", agentID, err)
	}
	var enabled []int
	hasDisabled := false
	for i := range devices {
		if !devices[i].Enabled {
			hasDisabled = true
		} else {
			enabled = append(enabled, devices[i].GetHashcatDeviceID())
		}
	}
	if !hasDisabled {
		// All enabled: agent uses every device; -d omitted.
		return nil, nil
	}
	return enabled, nil
}

// CycleResult is the per-cycle summary, used for logging and tests.
type CycleResult struct {
	UnitsSchedulable int
	IdleAgents       int
	Allocations      int
	// Benchmarked is the number of planned allocations that were turned into
	// benchmark dispatches this cycle (agent/unit pairs that lacked an accurate
	// keyspace or a cached speed) instead of task dispatches.
	Benchmarked int
	Dispatched  int
	Errors      []error
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
	// Single-flight guard. Returns the zero CycleResult immediately if
	// another RunOnce is already in flight (e.g., a slow DB query
	// blew past the ticker interval, or a future caller invoked
	// RunOnce manually from a different goroutine). We deliberately
	// skip rather than queue — queueing would produce the pile-up
	// we're guarding against.
	if !c.running.CompareAndSwap(false, true) {
		debug.Warning("scheduler-v2: RunOnce called while a cycle is already in flight; skipping")
		return CycleResult{}, nil
	}
	defer c.running.Store(false)

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

	// Step 4: allocate over ALL candidate units (accurate AND inaccurate).
	// The allocator decides which (agent, unit) pairs would run this cycle
	// per priority + max_agents + overflow + binary-compat. The next step
	// decides, per pair, whether it dispatches a real chunk or first needs a
	// benchmark — driving benchmarks off what would actually be dispatched
	// (so we only ever benchmark agent/unit combos that matter this cycle).
	mode := c.readOverflowMode(ctx)
	allocations := AllocateAgentsByPriority(unitInfos, agentInfos, mode, compatFn)
	res.Allocations = len(allocations)
	if len(allocations) == 0 {
		// Every idle-eligible agent is unallocated this cycle — record why
		// (no compatible job, no schedulable work) for the agent page.
		c.recordIdleReasons(agentInfos, unitInfos, allocations, nil, compatFn)
		return res, nil
	}

	// Step 4.5: classify each planned allocation as benchmark-vs-dispatch.
	// AllocateAgentsByPriority places each agent at most once, so a pair is
	// EITHER a benchmark OR a task this cycle — never both (hashcat runs one
	// thing at a time). A pair needs a benchmark when:
	//   - the unit's keyspace isn't accurate yet — the benchmark's progress[1]
	//     supplies the accurate effective keyspace (see HandleBenchmarkResult),
	//     OR
	//   - the agent has no cached speed for the unit's (attack_mode, hash_type,
	//     salt_count) combo — the dispatcher needs a speed to size the chunk.
	// Otherwise the pair is ready to dispatch a real chunk now.
	var benchGaps []BenchmarkGap
	readyAllocations := make([]Allocation, 0, len(allocations))
	for _, alloc := range allocations {
		u := unitsByID[alloc.UnitID]
		if u == nil {
			res.Errors = append(res.Errors, fmt.Errorf("cycle: missing unit for allocation %s", alloc.UnitID))
			continue
		}
		hashType, saltNull := c.lookupHashTypeAndSalt(ctx, u.ParentJobID)
		if hashType < 0 {
			res.Errors = append(res.Errors, fmt.Errorf("cycle: hash-type lookup failed for unit %s", u.ID))
			continue
		}
		var saltPtr *int
		if saltNull.Valid {
			sc := int(saltNull.Int64)
			saltPtr = &sc
		}
		needsBench := !u.IsAccurateKeyspace
		if !needsBench {
			has, hErr := agentHasBenchmarkFor(ctx, c.db, alloc.AgentID, u.AttackMode, hashType, saltPtr)
			if hErr != nil {
				res.Errors = append(res.Errors, fmt.Errorf("cycle: benchmark check (agent=%d unit=%s): %w", alloc.AgentID, u.ID, hErr))
				needsBench = true // can't confirm a cached speed → benchmark rather than mis-size a chunk
			} else if !has {
				needsBench = true
			}
		}
		if needsBench {
			// Storm guard: skip if this (agent, job, combo) is blocklisted —
			// a recent benchmark failed and AttributeBenchmarkFailure added an
			// entry (job-scoped or global). Without this we'd re-fire the same
			// failing benchmark every cycle, hammering the agent. The agent
			// sits this unit out until the entry clears/expires; another
			// compatible agent can still pick the unit up.
			if blocked, bErr := agentBenchmarkBlocklisted(ctx, c.db, alloc.AgentID, u.ParentJobID, u.AttackMode, hashType); bErr != nil {
				res.Errors = append(res.Errors, fmt.Errorf("cycle: blocklist check (agent=%d unit=%s): %w", alloc.AgentID, u.ID, bErr))
				continue
			} else if blocked {
				continue
			}
			benchGaps = append(benchGaps, BenchmarkGap{
				AgentID:    alloc.AgentID,
				UnitID:     u.ID,
				AttackMode: u.AttackMode,
				HashType:   hashType,
				SaltCount:  saltPtr,
			})
			continue
		}
		readyAllocations = append(readyAllocations, alloc)
	}

	// Step 4.6: dispatch benchmarks for the pairs that need one. Fire-and-
	// forget — the result lands via HandleBenchmarkResult, which caches the
	// speed AND (for inaccurate units) flips is_accurate_keyspace using
	// progress[1]. These agents are excluded from task dispatch this cycle
	// (they aren't in readyAllocations) and from future idle pools until the
	// benchmark completes (getIdleAgents in-flight guard), so the agent never
	// runs a benchmark and a task at once. They pick up their real task on a
	// later cycle once the benchmark result lands.
	if len(benchGaps) > 0 {
		_, bdErrs := DispatchBenchmarks(ctx, c.db, c.wsSender, c.binaryResolver, benchGaps, unitsByID)
		res.Errors = append(res.Errors, bdErrs...)
		res.Benchmarked = len(benchGaps)
	}

	// Step 5: dispatch the ready allocations (accurate keyspace + cached speed).
	// Step 11m: read default_chunk_duration first — the v1 / unified system
	// setting the UI exposes. Fall back to the v2-era duplicate
	// target_chunk_seconds (migration 000149) only when default_chunk_duration
	// is missing or zero. Either name returns the same concept: target wall
	// time per chunk in seconds. The per-job override
	// (job_executions.chunk_size_seconds, Step 11k) still wins inside dispatchOne.
	targetChunkSec := c.readIntSetting(ctx, "default_chunk_duration", 0)
	if targetChunkSec <= 0 {
		targetChunkSec = c.readIntSetting(ctx, "target_chunk_seconds", 60)
	}
	minChunkSec := c.readIntSetting(ctx, "min_chunk_seconds", 5)

	var dispatched []DispatchedTask
	if len(readyAllocations) > 0 {
		dispatchIn := DispatchInputs{
			Allocations:        readyAllocations,
			Units:              unitsByID,
			AgentSpeeds:        c.readAgentSpeeds(ctx, readyAllocations, unitsByID),
			TargetChunkSeconds: targetChunkSec,
			MinChunkSeconds:    minChunkSec,
		}
		var dispatchErrs []error
		dispatched, dispatchErrs = DispatchOneChunkPerAgent(ctx, c.db, dispatchIn)
		res.Errors = append(res.Errors, dispatchErrs...)
		res.Dispatched = len(dispatched)
	}

	// Step 6: fan out WebSocket task_assignment messages.
	//
	// Each dispatched task's per-agent metadata lookup
	// (binary_path, extra_parameters, enabled_devices, hashlist meta)
	// runs read-only against the DB and finishes with a non-blocking
	// channel send. Nothing in the loop body shares mutable state
	// with another iteration except res.Errors, so we fan out with a
	// bounded semaphore to slash tail latency on large batches (e.g.,
	// 15 agents allocated in one cycle).
	//
	// Concurrency cap is a system setting so operators can dial it
	// down if the DB shows lock contention or up if dispatch tail is
	// visible. Default 16 matches typical fleet sizes.
	fanoutLimit := c.readIntSetting(ctx, "scheduler_v2_fanout_concurrency", 16)
	if fanoutLimit < 1 {
		fanoutLimit = 1
	}
	sem := make(chan struct{}, fanoutLimit)
	var wg sync.WaitGroup
	appendErr := func(err error) {
		c.fanoutMu.Lock()
		res.Errors = append(res.Errors, err)
		c.fanoutMu.Unlock()
	}
	for _, dt := range dispatched {
		unit := unitsByID[dt.UnitID]
		if unit == nil {
			appendErr(fmt.Errorf("cycle: missing unit for dispatched task %s", dt.TaskID))
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(dt DispatchedTask, unit *models.SchedulingUnit) {
			defer wg.Done()
			defer func() { <-sem }()
			// Transition the parent job_execution from 'pending' to
			// 'running' on first dispatch. StartJobExecution is gated
			// on WHERE status='pending' at the repo layer, so
			// concurrent calls for the same job are no-ops on all but
			// one — safe to call from goroutines. Without this call
			// the job UI stays "pending" forever even though tasks
			// are flowing — the legacy scheduler did this from
			// inside executeTaskAssignment; the v2 cycle has no
			// equivalent and must do it explicitly here.
			if c.jobStarter != nil {
				if err := c.jobStarter.StartJobExecution(ctx, unit.ParentJobID); err != nil {
					debug.Debug("scheduler-v2: StartJobExecution(%s) noop or non-fatal: %v", unit.ParentJobID, err)
				}
			}
			if err := c.sendAssignment(ctx, dt, unit, targetChunkSec); err != nil {
				appendErr(fmt.Errorf("cycle: send task %s to agent %d: %w", dt.TaskID, dt.AgentID, err))
				// Don't roll back the DB insert. The heartbeat sweep
				// recovers if the agent never gets the message.
			}
		}(dt, unit)
	}
	wg.Wait()

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

	// Step 8: record why any idle-eligible agent ended up with neither a task
	// nor a benchmark this cycle (binary mismatch, caps, etc.), and clear the
	// reasons for agents that did get work. Buffered + deduped by the recorder.
	c.recordIdleReasons(agentInfos, unitInfos, allocations, benchGaps, compatFn)

	return res, nil
}

// computeStarvingUnits returns the schedulable units (from the
// allocator's input) that ended up with zero allocations this cycle
// AND could legitimately benefit from a preemption. Only those at
// non-zero priority are eligible — priority-0 jobs never preempt
// anything (and at the bottom of the queue there's nothing lower to
// preempt anyway).
//
// Parent-cap awareness: a unit whose parent job is already at its
// MaxAgents cap (via in-flight tasks or via sibling units that took
// the cap this cycle) is NOT starving — preempting another job to
// free an agent wouldn't help because the parent cap blocks any new
// allocation to this unit. Without this check, increment-job sibling
// layers (which share the parent cap) get treated as starving and
// trigger needless preemption of lower-priority jobs every cycle —
// the dispatched task gets preempted milliseconds later and ends up
// in 'pending' with NULL agent_id (via SetTaskPending), thrashing
// agents and preventing the lower-priority job from making progress.
func computeStarvingUnits(units []UnitInfo, allocations []Allocation) []UnitInfo {
	allocated := map[uuid.UUID]bool{}
	// Build unit_id → parent map and tally per-parent allocations
	// produced this cycle. UnitInfo.ActiveAgentCount already includes
	// in-flight tasks across the parent's siblings.
	unitParent := map[uuid.UUID]uuid.UUID{}
	for _, u := range units {
		unitParent[u.ID] = u.ParentJobID
	}
	parentAllocatedThisCycle := map[uuid.UUID]int{}
	for _, a := range allocations {
		allocated[a.UnitID] = true
		if pid, ok := unitParent[a.UnitID]; ok {
			parentAllocatedThisCycle[pid]++
		}
	}

	var starving []UnitInfo
	for _, u := range units {
		if u.Priority <= 0 {
			continue
		}
		if allocated[u.ID] {
			continue
		}
		// Parent-cap check: if the parent's max_agents is set and the
		// parent has no remaining slots, this unit isn't starving —
		// it's parent-capped. Preemption can't help.
		if u.MaxAgents > 0 {
			parentTotal := u.ActiveAgentCount + parentAllocatedThisCycle[u.ParentJobID]
			if parentTotal >= u.MaxAgents {
				continue
			}
		}
		starving = append(starving, u)
	}
	return starving
}

// buildUnitInfos converts SchedulingUnit rows into the allocator's
// UnitInfo. JOINs job_executions to get the binary_version AND the
// live priority + max_agents — those two fields are NOT denormalized
// onto scheduling_units (migration 000153 dropped them) precisely
// because they need to reflect operator edits in the admin UI on the
// next scheduler cycle. Also returns a map of unit_id ->
// *SchedulingUnit for the dispatcher's lookup needs.
func (c *Cycle) buildUnitInfos(ctx context.Context, units []*models.SchedulingUnit) ([]UnitInfo, map[uuid.UUID]*models.SchedulingUnit, error) {
	if len(units) == 0 {
		return nil, nil, nil
	}

	// Collect parent job IDs and query the live job-level fields.
	parentIDs := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		parentIDs = append(parentIDs, u.ParentJobID)
	}

	// Single query: parent_id -> (binary_version, priority, max_agents).
	// ANY($1) with a UUID array; lib/pq handles the array marshalling.
	type parentRow struct {
		binaryVersion string
		priority      int
		maxAgents     int
	}
	parents := make(map[uuid.UUID]parentRow)
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, COALESCE(binary_version, ''), priority, max_agents
		FROM job_executions
		WHERE id = ANY($1::uuid[])
	`, uuidSliceToTextArray(parentIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("query parent job fields: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var pr parentRow
		if err := rows.Scan(&id, &pr.binaryVersion, &pr.priority, &pr.maxAgents); err != nil {
			return nil, nil, fmt.Errorf("scan parent job row: %w", err)
		}
		parents[id] = pr
	}

	// Get active-agent counts per PARENT JOB. For non-increment jobs
	// this is identical to per-unit. For increment jobs (multiple
	// layer units per parent), the count aggregates across siblings so
	// the allocator's parent-cap check sees the true total. All sibling
	// UnitInfos receive the same value below.
	counts, err := c.activeAgentCountsByParent(ctx, units)
	if err != nil {
		return nil, nil, err
	}

	// Per-unit interval coverage (sum of non-failed range widths in base
	// units). Used to compute MaxNewChunksThisCycle below — without
	// this hint, the FIFO/RR overflow paths blindly pile extras on the
	// oldest tier unit even when its keyspace is fully tiled by
	// in-flight chunks, leaving the surplus agents idle for the cycle.
	coverage, err := c.intervalCoverageByUnit(ctx, units)
	if err != nil {
		return nil, nil, fmt.Errorf("compute interval coverage: %w", err)
	}

	// Conservative per-chunk effective-keyspace estimate used to bound
	// "how many more chunks fit on this unit this cycle." Mirrors the
	// dispatcher's sizeChunk math when agent_speed is unknown:
	// chunk_size_effective ≈ target_chunk_seconds × conservative_speed.
	// Reading the system setting once here avoids per-unit lookups.
	targetChunkSeconds := c.readTargetChunkSeconds(ctx)
	if targetChunkSeconds <= 0 {
		targetChunkSeconds = 60
	}
	chunkEstimateEffective := int64(targetChunkSeconds) * ConservativeAgentSpeed

	infos := make([]UnitInfo, 0, len(units))
	byID := make(map[uuid.UUID]*models.SchedulingUnit, len(units))
	for _, u := range units {
		p := parents[u.ParentJobID]

		// MaxNewChunksThisCycle: how many fresh chunks could realistically
		// dispatch on this unit this cycle. Floor of MaxAgents so Phase 1
		// cap-fill always fits; ceiling of remaining-effective-keyspace
		// divided by the chunk-size estimate so overflow doesn't pile
		// past what the dispatcher could actually use. Unbounded (-1
		// sentinel via 0 default) for units missing base_keyspace —
		// safest fallback is current behavior.
		maxNew := 0
		if u.BaseKeyspace != nil && *u.BaseKeyspace > 0 {
			coveredBase := coverage[u.ID]
			remainingBase := *u.BaseKeyspace - coveredBase
			if remainingBase < 0 {
				remainingBase = 0
			}
			// remainingEffective = effective × remainingBase / base, an EXACT
			// big.Int multiply-then-divide (effective_keyspace is NUMERIC and the
			// product can exceed int64, e.g. base 1e10 × multiplier 1e9 = 1e19).
			// Using the exact ratio rather than a pre-divided/truncated multiplier
			// keeps this per-cycle chunk cap from under-counting on non-integer
			// ratios (base×2.9999 would have truncated to ×2). chunksFit only needs
			// to be an int per-cycle cap; clamp anything past MaxInt32 to
			// "effectively unlimited."
			remainingEffective := u.EffectiveKeyspace.MulInt64(remainingBase).DivInt64(*u.BaseKeyspace)
			if remainingEffective.CmpInt64(remainingBase) < 0 {
				// effective should be >= base; guard a shrunk/zero effective by
				// falling back to at least the base remainder (mirrors the old
				// multiplier>=1 clamp).
				remainingEffective = models.NewBigInt(remainingBase)
			}
			chunksFit := 0
			if remainingEffective.IsPositive() {
				fit := remainingEffective.DivInt64(chunkEstimateEffective)
				if v, ok := fit.Int64Checked(); ok && v <= int64(math.MaxInt32) {
					chunksFit = int(v)
				} else {
					chunksFit = math.MaxInt32
				}
				if chunksFit < 1 {
					chunksFit = 1
				}
			}
			// Floor at MaxAgents so the Phase 1 baseline cap always
			// fits even when the estimate would otherwise round to 0.
			// MaxAgents=0 means "unlimited," which we read as the larger
			// of the two.
			maxNew = chunksFit
			if p.maxAgents > 0 && maxNew < p.maxAgents {
				maxNew = p.maxAgents
			}
		}

		infos = append(infos, UnitInfo{
			ID:                    u.ID,
			ParentJobID:           u.ParentJobID,
			Priority:              p.priority,
			MaxAgents:             p.maxAgents,
			BinaryVersion:         p.binaryVersion,
			ActiveAgentCount:      counts[u.ParentJobID],
			CreatedAtNanos:        u.CreatedAt.UnixNano(),
			MaxNewChunksThisCycle: maxNew,
		})
		byID[u.ID] = u
	}
	return infos, byID, nil
}

// intervalCoverageByUnit returns unit_id -> sum of (range_end - range_start)
// for all non-failed intervals on that unit. Used by buildUnitInfos to
// compute "remaining base keyspace" for the per-cycle overflow cap.
func (c *Cycle) intervalCoverageByUnit(ctx context.Context, units []*models.SchedulingUnit) (map[uuid.UUID]int64, error) {
	if len(units) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT scheduling_unit_id, COALESCE(SUM(range_end - range_start), 0)
		FROM job_keyspace_intervals
		WHERE scheduling_unit_id = ANY($1::uuid[])
		  AND status <> 'failed'
		GROUP BY scheduling_unit_id
	`, uuidSliceToTextArray(ids))
	if err != nil {
		return nil, fmt.Errorf("query interval coverage: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]int64, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var covered int64
		if err := rows.Scan(&id, &covered); err != nil {
			return nil, fmt.Errorf("scan interval coverage: %w", err)
		}
		out[id] = covered
	}
	return out, rows.Err()
}

// readTargetChunkSeconds fetches the target_chunk_seconds system setting,
// falling back to 60s if unset/invalid. Used by buildUnitInfos to
// estimate how many more chunks fit on a unit this cycle.
func (c *Cycle) readTargetChunkSeconds(ctx context.Context) int {
	if c.systemSettingsRepo == nil {
		return 60
	}
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	setting, err := c.systemSettingsRepo.GetSetting(rctx, "target_chunk_seconds")
	if err != nil || setting == nil || setting.Value == nil {
		return 60
	}
	n, err := strconv.Atoi(*setting.Value)
	if err != nil || n <= 0 {
		return 60
	}
	return n
}

// activeAgentCountsByParent returns parent_job_id -> count of in-flight
// (assigned/running) scheduler-v2 tasks aggregated across ALL units of
// the parent, including units that may no longer be in the schedulable
// set. Critical for the parent-cap allocator: once a sibling unit has
// no more gaps it falls out of the schedulable list, but its running
// task still counts toward the parent's max_agents cap. Filtering on
// parent_job_id (not unit IDs) keeps the count correct in that case.
// Tasks without scheduling_unit_id are legacy and excluded by the JOIN.
func (c *Cycle) activeAgentCountsByParent(ctx context.Context, units []*models.SchedulingUnit) (map[uuid.UUID]int, error) {
	// De-dupe parent IDs — increment jobs have multiple units sharing
	// one parent.
	seen := make(map[uuid.UUID]struct{}, len(units))
	parentIDs := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		if _, dup := seen[u.ParentJobID]; dup {
			continue
		}
		seen[u.ParentJobID] = struct{}{}
		parentIDs = append(parentIDs, u.ParentJobID)
	}
	out := map[uuid.UUID]int{}
	rows, err := c.db.QueryContext(ctx, `
		SELECT su.parent_job_id, COUNT(*)
		FROM job_tasks t
		JOIN scheduling_units su ON su.id = t.scheduling_unit_id
		WHERE su.parent_job_id = ANY($1::uuid[])
		  AND t.status IN ('assigned', 'running')
		GROUP BY su.parent_job_id
	`, uuidSliceToTextArray(parentIDs))
	if err != nil {
		return nil, fmt.Errorf("query active agent counts by parent: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var parentID uuid.UUID
		var n int
		if err := rows.Scan(&parentID, &n); err != nil {
			return nil, fmt.Errorf("scan active count: %w", err)
		}
		out[parentID] = n
	}
	return out, nil
}

// getIdleAgents returns agents that are:
//   - currently WebSocket-connected (per wsSender.GetConnectedAgents,
//     the source of truth for "agent is online" — not the agents.status
//     or agents.last_heartbeat columns, which are derived snapshots
//     maintained by legacy handlers and can be stale),
//   - operator-enabled (is_enabled is the only operator-controlled
//     "this agent should accept work" knob),
//   - not currently running a scheduler-v2 task.
//
// Intentionally does NOT filter on sync_status. The agent's per-task
// pre-flight (ensureHashlist, ensureAssociationFiles, ensureClientPotfile,
// etc. in agent/internal/jobs/jobs.go) handles file sync per-job at
// dispatch time. The legacy sync_status='completed' gate was a global
// "files are warm" precondition that doesn't match the v2 dispatch
// model.
func (c *Cycle) getIdleAgents(ctx context.Context) ([]AgentInfo, error) {
	connected := c.wsSender.GetConnectedAgents()
	if len(connected) == 0 {
		return nil, nil
	}

	// Filter out agents that are currently in graceful-shutdown
	// handshake, OR that rejected a task within the rejection-cooldown
	// window. Without these the cycle would push a fresh task at an
	// agent we already know can't accept it — wasting the round trip,
	// creating zombie 'assigned' rows, and (for the rejection case)
	// looping a rejection storm until the agent's real task finally
	// flips to 'running' in the DB.
	live := make([]int, 0, len(connected))
	for _, agentID := range connected {
		if c.wsSender.IsShuttingDown(agentID) {
			continue
		}
		if c.wsSender.WasRecentlyRejected(agentID) {
			debug.Debug("scheduler-v2: agent %d in rejection cooldown; skipping this cycle", agentID)
			continue
		}
		// Skip agents that EXPLICITLY reported their startup file map isn't
		// built yet. Dispatching to one would only earn a rejection that the
		// backend records as a permanent failed task row (GH #61). Fail-open:
		// agents that never report readiness (older versions) return true here
		// and stay eligible.
		if !c.wsSender.IsFileMapReady(agentID) {
			debug.Debug("scheduler-v2: agent %d file map not ready; skipping this cycle", agentID)
			continue
		}
		live = append(live, agentID)
	}
	if len(live) == 0 {
		return nil, nil
	}

	// Postgres int[] -> use ANY().
	connectedArr := intSliceToBigintArray(live)

	// An agent is idle only if it has no in-flight scheduler-v2 task AND no
	// in-flight benchmark. The benchmark exclusion enforces "one hashcat
	// invocation at a time" ACROSS cycles: DispatchBenchmarks records a
	// benchmark_requests row (completed_at NULL) before sending, and a real
	// benchmark can take minutes to return. Without this guard the next cycle
	// would see the still-benchmarking agent as idle and hand it a task,
	// colliding with the running benchmark. The window matches
	// benchmarkInFlightWindow so a crashed/never-returned benchmark eventually
	// frees the agent for a retry rather than wedging it forever.
	benchWindowSecs := int(benchmarkInFlightWindow / time.Second)
	rows, err := c.db.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.binary_version, '')
		FROM agents a
		WHERE a.id = ANY($1::bigint[])
		  AND a.is_enabled = true
		  AND a.status <> 'updating'
		  AND NOT EXISTS (
			  SELECT 1 FROM job_tasks t
			  WHERE t.agent_id = a.id
				AND t.status IN ('assigned', 'running')
				AND t.scheduling_unit_id IS NOT NULL
		  )
		  AND NOT EXISTS (
			  SELECT 1 FROM benchmark_requests br
			  WHERE br.agent_id = a.id
				AND br.completed_at IS NULL
				AND br.requested_at > NOW() - ($2 || ' seconds')::INTERVAL
		  )
	`, connectedArr, benchWindowSecs)
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

	// Agent scheduling filter (mirrors legacy
	// JobExecutionService.filterAvailableAgents). If the global
	// `agent_scheduling_enabled` setting is on, AND the per-agent
	// SchedulingEnabled toggle is on, AND the current UTC time is NOT
	// inside the agent's approved schedule window, exclude the agent.
	// Either dep being nil disables the filter entirely (safe default
	// for tests that don't wire these up).
	if c.scheduleRepo == nil || c.agentRepo == nil || c.systemSettingsRepo == nil || len(out) == 0 {
		return out, nil
	}
	schedulingSetting, sErr := c.systemSettingsRepo.GetSetting(ctx, "agent_scheduling_enabled")
	globalOn := sErr == nil && schedulingSetting != nil && schedulingSetting.Value != nil && *schedulingSetting.Value == "true"
	if !globalOn {
		return out, nil
	}
	filtered := out[:0]
	for _, ai := range out {
		agent, aErr := c.agentRepo.GetByID(ctx, ai.ID)
		if aErr != nil {
			debug.Warning("scheduler-v2: lookup agent %d for scheduling check: %v", ai.ID, aErr)
			// Treat as ineligible — safer than overdispatching outside an
			// agent's window.
			continue
		}
		if !agent.SchedulingEnabled {
			// Per-agent scheduling off → agent runs 24/7 (when connected).
			filtered = append(filtered, ai)
			continue
		}
		scheduled, schedErr := c.scheduleRepo.IsAgentScheduledNow(ctx, ai.ID)
		if schedErr != nil {
			debug.Warning("scheduler-v2: IsAgentScheduledNow(agent=%d): %v", ai.ID, schedErr)
			continue
		}
		if scheduled {
			filtered = append(filtered, ai)
		}
	}
	return filtered, nil
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
	payload, err := BuildTaskAssignment(unit, dt.TaskID, dt.RangeStart, dt.RangeEnd, dt.EffectiveKeyspaceStart, dt.EffectiveKeyspaceEnd)
	if err != nil {
		return fmt.Errorf("build assignment: %w", err)
	}

	// Enrich from parent job_execution + hashlist + agent. Path format
	// matches the legacy emitter at job_websocket_integration.go:602,
	// 1106 — the agent expects the processed-into-DB version at
	// "hashlists/<id>.hash". Mode 9 additionally needs the original
	// file path so hashcat can pair hashes with candidates by line
	// order; that lives in hashlists.original_file_path.
	//
	// Three fields the legacy dispatcher set that the v2 cycle
	// previously dropped on the floor (the agent reads them all):
	//   - job_executions.additional_args     → payload.JobAdditionalArgs
	//   - job_executions.base_keyspace       → payload.BaseKeyspace
	//   - hashlists.client_id                → payload.ClientID
	//
	// Without these the agent runs hashcat with wrong tuning, can't
	// resolve client potfile routing, and (when -O kernel-split fires)
	// can land --skip/--limit in the wrong window. Restoring them here
	// matches the legacy behavior verbatim.
	var hashlistID int64
	var hashType int
	var originalFilePath string
	var jobAdditionalArgs sql.NullString
	var baseKeyspace sql.NullInt64
	var hashlistClientID uuid.NullUUID
	var slow bool
	// LEFT JOIN hash_types so a missing/unknown hash type can never break dispatch —
	// slow just defaults to FALSE (no -S) via COALESCE.
	err = c.db.QueryRowContext(ctx, `
		SELECT je.hashlist_id, h.hash_type_id, COALESCE(h.original_file_path, ''),
		       je.additional_args, je.base_keyspace, h.client_id, COALESCE(ht.slow, FALSE)
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		LEFT JOIN hash_types ht ON ht.id = h.hash_type_id
		WHERE je.id = $1
	`, unit.ParentJobID).Scan(&hashlistID, &hashType, &originalFilePath,
		&jobAdditionalArgs, &baseKeyspace, &hashlistClientID, &slow)
	if err != nil {
		return fmt.Errorf("lookup parent + hashlist: %w", err)
	}

	payload.HashlistID = hashlistID
	payload.HashType = hashType
	payload.Slow = slow
	payload.HashlistPath = fmt.Sprintf("hashlists/%d.hash", hashlistID)
	payload.ChunkDuration = chunkDuration
	payload.ReportInterval = 5
	if unit.AttackMode == AttackModeAssociation && originalFilePath != "" {
		payload.OriginalHashlistPath = originalFilePath
	}
	if jobAdditionalArgs.Valid {
		payload.JobAdditionalArgs = jobAdditionalArgs.String
	}
	// Prefer the UNIT's base_keyspace over the parent job's. For
	// non-increment jobs they're equal (populateSingleUnit copies job's
	// base to the unit). For INCREMENT-mode jobs the job's base is the
	// SUM of all layer bases (e.g., 245218371 = 1+95+9025+...+81M+81M+81M),
	// while each unit holds its layer's local base (e.g., 81450625 for
	// ?a?a?a?a?a?a). The agent rescales when these disagree (visible in
	// agent log as "Keyspace coordinate conversion: ratio=0.33") — that
	// works but is fragile and obscures intent. Sending the layer's own
	// base aligns server and agent coordinates; ratio becomes 1 and no
	// rescale fires.
	if unit.BaseKeyspace != nil && *unit.BaseKeyspace > 0 {
		payload.BaseKeyspace = *unit.BaseKeyspace
	} else if baseKeyspace.Valid {
		payload.BaseKeyspace = baseKeyspace.Int64
	}
	if hashlistClientID.Valid {
		payload.ClientID = hashlistClientID.UUID.String()
	}

	// TODO (separate v2 gap): ClientPotfilePath is intentionally NOT
	// set here. The legacy dispatcher set it only when the user
	// referenced a "potfile:ID" prefix in their job's wordlist list,
	// and the resolution happened at dispatch time. The v2 unit
	// creation (populateSchedulingUnits in job_execution_v2.go) does
	// NOT yet handle "potfile:ID" or "client:UUID" wordlist prefixes,
	// so by the time we get here the prefix has either been stripped
	// or stored as a raw ref the agent can't resolve. Fixing this is
	// scoped to job_execution_v2.go, not this dispatch site. Until
	// then, jobs that reference a client potfile as a wordlist will
	// silently degrade (the agent will fail to find the file).
	// The crack-save path is unaffected — hashes.client_id routing
	// happens server-side in processCrackedHashes regardless.

	// Agent-level fields: extra_parameters + enabled device list.
	// Matches the legacy lookup at job_websocket_integration.go:561
	// (agentRepo.GetByID) and :793 (deviceRepo.GetByAgentID).
	var agentExtraParams sql.NullString
	if err := c.db.QueryRowContext(ctx, `
		SELECT extra_parameters FROM agents WHERE id = $1
	`, dt.AgentID).Scan(&agentExtraParams); err != nil {
		debug.Warning("scheduler-v2: lookup agent extra_parameters for agent %d failed: %v", dt.AgentID, err)
	} else if agentExtraParams.Valid {
		payload.ExtraParameters = agentExtraParams.String
	}

	// Enabled-device list: only populated when SOME devices are
	// disabled. If all are enabled, leave nil so the agent uses every
	// device hashcat detected. Matches legacy behavior at
	// job_websocket_integration.go:797-810.
	if enabled, err := c.enabledDeviceIDsForAgent(ctx, dt.AgentID); err != nil {
		debug.Warning("scheduler-v2: lookup enabled devices for agent %d failed: %v", dt.AgentID, err)
	} else {
		payload.EnabledDevices = enabled
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

	// Attach the current on-server MD5 for every wordlist/rule/binary this
	// task references so the agent can verify each file and re-download any
	// that are missing or stale before running hashcat (GH #61). Without
	// this, a running agent never learns about a rule/wordlist changed after
	// it connected and the job fails until the agent restarts.
	c.attachFileMD5s(ctx, payload)

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

// attachFileMD5s looks up the current on-server MD5 for each wordlist, rule,
// and binary the task references and stores them on the payload keyed by the
// exact wire path (e.g. "rules/hashcat/best64.rule"). The agent verifies each
// referenced file against these hashes at dispatch time and re-downloads any
// that are missing or stale — this is how a running agent picks up files
// changed on the server after it connected (GH #61). The DB md5 is kept current
// by the upload handlers, the directory monitor (direct-on-disk edits), and the
// potfile writers, so a dispatch-time lookup reflects the freshest file.
//
// Best-effort by design: a path with no matching verified DB row (an ephemeral
// filtered wordlist, or a client potfile served from a different table) is
// simply omitted, and the agent falls back to its existence check for that file.
// Lookup failures never block a dispatch.
func (c *Cycle) attachFileMD5s(ctx context.Context, payload *wsservice.TaskAssignmentPayload) {
	if len(payload.WordlistPaths) > 0 {
		m := make(map[string]string, len(payload.WordlistPaths))
		for _, p := range payload.WordlistPaths {
			name := strings.TrimPrefix(p, "wordlists/")
			var md5 string
			if err := c.db.QueryRowContext(ctx,
				`SELECT md5_hash FROM wordlists WHERE file_name = $1 AND verification_status = 'verified' LIMIT 1`,
				name).Scan(&md5); err == nil && md5 != "" {
				m[p] = md5
			}
		}
		if len(m) > 0 {
			payload.WordlistMD5s = m
		}
	}

	if len(payload.RulePaths) > 0 {
		m := make(map[string]string, len(payload.RulePaths))
		for _, p := range payload.RulePaths {
			name := strings.TrimPrefix(p, "rules/")
			var md5 string
			if err := c.db.QueryRowContext(ctx,
				`SELECT md5_hash FROM rules WHERE file_name = $1 AND verification_status = 'verified' LIMIT 1`,
				name).Scan(&md5); err == nil && md5 != "" {
				m[p] = md5
			}
		}
		if len(m) > 0 {
			payload.RuleMD5s = m
		}
	}

	// BinaryPath format: "binaries/<binary_version_id>". Sent for completeness;
	// binary versions are immutable (a new binary gets a new id and path), so
	// the agent treats the binary directory as present-or-absent rather than
	// re-verifying this hash.
	if payload.BinaryPath != "" {
		idStr := strings.TrimPrefix(payload.BinaryPath, "binaries/")
		if id, convErr := strconv.Atoi(idStr); convErr == nil {
			var md5 string
			if err := c.db.QueryRowContext(ctx,
				`SELECT md5_hash FROM binary_versions WHERE id = $1`, id).Scan(&md5); err == nil {
				payload.BinaryMD5 = md5
			}
		}
	}
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
