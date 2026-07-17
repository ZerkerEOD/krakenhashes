package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	khdb "github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/scheduler"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobIntegrationManager manages the integration between WebSocket and job execution services
type JobIntegrationManager struct {
	wsIntegration        *JobWebSocketIntegration
	jobExecutionService  *services.JobExecutionService
	jobSchedulingService *services.JobSchedulingService
	wsHandler            interface {
		SendMessage(agentID int, msg *wsservice.Message) error
		GetConnectedAgents() []int
		IsShuttingDown(agentID int) bool
		WasRecentlyRejected(agentID int) bool
		IsFileMapReady(agentID int) bool
		MarkRejected(agentID int)
		RegisterInventoryCallback(agentID int) <-chan *wsservice.FileSyncResponsePayload
		UnregisterInventoryCallback(agentID int)
	}

	// Scheduler-v2 runners. The legacy scheduler is no longer started
	// (its source remains in-tree until the hard-cutover release that
	// drops job_scheduling_service.go and its co-located files).
	schedulerV2Runner *scheduler.Runner
	sweeperRunner     *scheduler.SweeperRunner
	// compatCache is the (agent_id, unit_id) compatibility lookup.
	// Warmed at StartScheduler and refreshed periodically; misses
	// fall through to lazy single-pair evaluation.
	compatCache *scheduler.CompatCache

	// diagnosticsService buffers + batches per-agent "why idle" reasons
	// the cycle records, and serves them (force-flushed) to the agent UI.
	diagnosticsService *services.DiagnosticsService

	// agentUpdateSweeper drives the agent auto-update promote/timeout loop.
	// Set via SetAgentUpdateSweeper and started in StartScheduler so it
	// shares the scheduler's lifecycle. May be nil (auto-update unwired).
	agentUpdateSweeper *services.AgentUpdateSweeper

	// Chunk-overrun guard: stops tasks running past chunk_duration × tolerance
	// and feeds the measured speed back so the re-dispatched remainder is sized
	// correctly. Recovery (truncate/re-gap) is left to the stop + heartbeat
	// sweeper path. overrunSignaled debounces repeat stops until the agent
	// actually goes silent.
	jobTaskRepo        *repository.JobTaskRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	overrunSignaled    map[uuid.UUID]time.Time
	overrunMu          sync.Mutex
}

// SetAgentUpdateSweeper wires the agent auto-update sweeper so it starts and
// stops with the scheduler.
func (m *JobIntegrationManager) SetAgentUpdateSweeper(sweeper *services.AgentUpdateSweeper) {
	m.agentUpdateSweeper = sweeper
}

// NewJobIntegrationManager creates a new job integration manager
func NewJobIntegrationManager(
	wsHandler interface {
		SendMessage(agentID int, msg *wsservice.Message) error
		GetConnectedAgents() []int
		IsShuttingDown(agentID int) bool
		WasRecentlyRejected(agentID int) bool
		IsFileMapReady(agentID int) bool
		MarkRejected(agentID int)
		RegisterInventoryCallback(agentID int) <-chan *wsservice.FileSyncResponsePayload
		UnregisterInventoryCallback(agentID int)
	},
	jobSchedulingService *services.JobSchedulingService,
	jobExecutionService *services.JobExecutionService,
	hashlistSyncService *services.HashlistSyncService,
	benchmarkRepo *repository.BenchmarkRepository,
	presetJobRepo repository.PresetJobRepository,
	hashlistRepo *repository.HashListRepository,
	hashRepo *repository.HashRepository,
	lmHashRepo *repository.LMHashRepository,
	jobTaskRepo *repository.JobTaskRepository,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
	agentRepo *repository.AgentRepository,
	deviceRepo *repository.AgentDeviceRepository,
	scheduleRepo *repository.AgentScheduleRepository,
	clientRepo *repository.ClientRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	assocWordlistRepo *repository.AssociationWordlistRepository,
	potfileService *services.PotfileService,
	clientPotfileService *services.ClientPotfileService,
	clientWordlistRepo *repository.ClientWordlistRepository,
	clientPotfileRepo *repository.ClientPotfileRepository,
	hashlistCompletionService *services.HashlistCompletionService,
	db *sql.DB,
	wordlistManager wordlist.Manager,
	ruleManager rule.Manager,
	binaryManager binary.Manager,
) *JobIntegrationManager {
	// Create the WebSocket integration
	wsIntegration := NewJobWebSocketIntegration(
		wsHandler,
		jobSchedulingService,
		jobExecutionService,
		hashlistSyncService,
		benchmarkRepo,
		presetJobRepo,
		hashlistRepo,
		hashRepo,
		lmHashRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		agentRepo,
		deviceRepo,
		clientRepo,
		systemSettingsRepo,
		assocWordlistRepo,
		potfileService,
		clientPotfileService,
		clientWordlistRepo,
		clientPotfileRepo,
		hashlistCompletionService,
		db,
		wordlistManager,
		ruleManager,
		binaryManager,
	)

	// Set the WebSocket integration in the scheduling service
	jobSchedulingService.SetWebSocketIntegration(wsIntegration)

	mgr := &JobIntegrationManager{
		wsIntegration:        wsIntegration,
		jobExecutionService:  jobExecutionService,
		jobSchedulingService: jobSchedulingService,
		wsHandler:            wsHandler,
		jobTaskRepo:          jobTaskRepo,
		systemSettingsRepo:   systemSettingsRepo,
		overrunSignaled:      make(map[uuid.UUID]time.Time),
	}

	// Scheduler-v2 owns all jobs. The legacy scheduler runner is no
	// longer started (see StartScheduler) and the converter runs once
	// at boot to migrate any pre-existing v1 jobs.
	database := &khdb.DB{DB: db}
	unitRepo := repository.NewSchedulingUnitRepository(database)
	intervalRepo := repository.NewKeyspaceIntervalRepository(database)
	mgr.compatCache = scheduler.NewCompatCache(database)
	// jobExecutionService satisfies both scheduler.BinaryResolver
	// (DetermineBinaryForTask) and scheduler.JobExecutionStarter
	// (StartJobExecution) structurally. Pass it twice so the cycle
	// can populate BinaryPath AND transition the parent job from
	// pending→running on first dispatch without the scheduler
	// package importing services (which would be a circular
	// dependency).
	// deviceRepo lets the cycle emit -d <enabled-IDs> in task
	// assignments when the user has disabled some GPUs. agentRepo +
	// scheduleRepo enable the agent-scheduling check in getIdleAgents
	// (mirror of legacy filterAvailableAgents).
	cycle := scheduler.NewCycle(database, unitRepo, intervalRepo, systemSettingsRepo, deviceRepo, agentRepo, scheduleRepo, wsHandler, jobExecutionService, jobExecutionService, mgr.compatCache)
	// Diagnostics: the cycle records per-agent idle reasons; the service
	// buffers/dedups them and the agent UI reads them back force-flushed.
	mgr.diagnosticsService = services.NewDiagnosticsService(repository.NewDiagnosticsRepository(database))
	cycle.SetDiagnostics(mgr.diagnosticsService)
	mgr.schedulerV2Runner = scheduler.NewRunner(cycle, 3*time.Second)
	mgr.sweeperRunner = scheduler.NewSweeperRunner(database, systemSettingsRepo, 10*time.Second)
	debug.Info("scheduler-v2: runners constructed (start deferred)")

	return mgr
}

// ProcessJobProgress handles job progress messages from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessJobProgress(ctx context.Context, agentID int, payload json.RawMessage) error {
	var progress models.JobProgress
	if err := json.Unmarshal(payload, &progress); err != nil {
		return fmt.Errorf("failed to unmarshal job progress: %w", err)
	}

	return m.wsIntegration.HandleJobProgress(ctx, agentID, &progress)
}

// ProcessCrackBatch handles crack batch messages from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessCrackBatch(ctx context.Context, agentID int, payload json.RawMessage) error {
	var crackBatch models.CrackBatch
	if err := json.Unmarshal(payload, &crackBatch); err != nil {
		return fmt.Errorf("failed to unmarshal crack batch: %w", err)
	}

	return m.wsIntegration.HandleCrackBatch(ctx, agentID, &crackBatch)
}

// ProcessCrackBatchesComplete handles crack_batches_complete signal from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessCrackBatchesComplete(ctx context.Context, agentID int, payload json.RawMessage) error {
	var signal models.CrackBatchesComplete
	if err := json.Unmarshal(payload, &signal); err != nil {
		return fmt.Errorf("failed to unmarshal crack_batches_complete: %w", err)
	}

	return m.wsIntegration.HandleCrackBatchesComplete(ctx, agentID, &signal)
}

// ProcessBenchmarkResult handles benchmark result messages from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessBenchmarkResult(ctx context.Context, agentID int, payload json.RawMessage) error {
	var result wsservice.BenchmarkResultPayload
	if err := json.Unmarshal(payload, &result); err != nil {
		return fmt.Errorf("failed to unmarshal benchmark result: %w", err)
	}

	return m.wsIntegration.HandleBenchmarkResult(ctx, agentID, &result)
}

// ProcessPendingOutfiles handles pending_outfiles messages from agents on reconnect (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessPendingOutfiles(ctx context.Context, agentID int, payload json.RawMessage) error {
	return m.wsIntegration.ProcessPendingOutfiles(ctx, agentID, payload)
}

// ProcessOutfileDeleteRejected handles outfile_delete_rejected messages when agent rejects deletion due to line count mismatch
func (m *JobIntegrationManager) ProcessOutfileDeleteRejected(ctx context.Context, agentID int, payload json.RawMessage) error {
	return m.wsIntegration.ProcessOutfileDeleteRejected(ctx, agentID, payload)
}

// RecoverTask attempts to recover a task that was in reconnect_pending state (implements interfaces.JobHandler)
func (m *JobIntegrationManager) RecoverTask(ctx context.Context, taskID string, agentID int, keyspaceProcessed int64) error {
	return m.wsIntegration.RecoverTask(ctx, taskID, agentID, keyspaceProcessed)
}

// InvalidateAgentCompat forces the binary-version compatibility cache to
// re-evaluate one agent immediately rather than waiting for the next periodic
// re-warm (≤30s). Called when an agent's binary_version changes (admin settings
// update) and on connect/disconnect so a freshly online agent — possibly with a
// version changed while it was offline — is scheduled against current data.
// Safe to call with a nil manager or cache (no-op).
func (m *JobIntegrationManager) InvalidateAgentCompat(ctx context.Context, agentID int) {
	if m == nil || m.compatCache == nil {
		return
	}
	m.compatCache.OnAgentChanged(ctx, agentID)
}

// HandleAgentReconnectionWithNoTask handles when an agent reconnects without a running task (implements interfaces.JobHandler)
func (m *JobIntegrationManager) HandleAgentReconnectionWithNoTask(ctx context.Context, agentID int) (int, error) {
	// Refresh this agent's compatibility row up front: it may have come back
	// online with a binary_version changed while it was disconnected, and the
	// scheduler should route it correctly on the very next cycle.
	m.InvalidateAgentCompat(ctx, agentID)
	return m.wsIntegration.HandleAgentReconnectionWithNoTask(ctx, agentID)
}

// ClearStoppedTaskAgent clears agent_id after stop is acknowledged (implements JobHandler)
func (m *JobIntegrationManager) ClearStoppedTaskAgent(ctx context.Context, taskID uuid.UUID, agentID int) error {
	return m.wsIntegration.ClearStoppedTaskAgent(ctx, taskID, agentID)
}

// HandleAgentDisconnection forwards disconnect events to the WebSocket
// integration so tasks get flagged reconnect_pending and the grace-period
// timer starts. Without this delegation the wsservice type-asserts against
// this manager (not wsIntegration) and emits
// "Job handler does not support disconnection handling".
func (m *JobIntegrationManager) HandleAgentDisconnection(ctx context.Context, agentID int) error {
	// Refresh this agent's compat row from current DB state on the way out so
	// it never lingers stale (a disconnected agent is filtered from scheduling
	// anyway; this keeps the cache honest if its version was just changed).
	m.InvalidateAgentCompat(ctx, agentID)
	return m.wsIntegration.HandleAgentDisconnection(ctx, agentID)
}

// GetWebSocketIntegration returns the WebSocket integration instance
func (m *JobIntegrationManager) GetWebSocketIntegration() *JobWebSocketIntegration {
	return m.wsIntegration
}

// ConvertLegacyJobsToV2 runs the one-shot startup converter that
// migrates any pre-existing v1 jobs (job_executions without a
// scheduling_units row) into v2 jobs, or deletes them if their
// wordlist/rule refs no longer resolve.
//
// Idempotent: safe to call on every boot. After the first successful
// run there will be nothing to convert.
func (m *JobIntegrationManager) ConvertLegacyJobsToV2(ctx context.Context) error {
	if m.jobExecutionService == nil {
		return nil
	}
	return m.jobExecutionService.ConvertLegacyJobsToV2(ctx)
}

// RepairPendingJobKeyspaces delegates to the job execution service's boot-time
// keyspace repair (see JobExecutionService.RepairPendingJobKeyspaces). Returns
// the number of pending jobs whose keyspace was recomputed accurately.
func (m *JobIntegrationManager) RepairPendingJobKeyspaces(ctx context.Context) (int, error) {
	if m.jobExecutionService == nil {
		return 0, nil
	}
	return m.jobExecutionService.RepairPendingJobKeyspaces(ctx)
}

// StartScheduler starts scheduler-v2. The legacy runner is no longer
// started — its source code is retained for one more release as a
// rollback option, but it does not tick.
func (m *JobIntegrationManager) StartScheduler(ctx context.Context) {
	debug.Info("scheduler-v2: starting runner, sweeper, and compat cache refresh")

	if m.diagnosticsService != nil {
		m.diagnosticsService.Start(ctx)
	}
	if m.schedulerV2Runner != nil {
		m.schedulerV2Runner.Start(ctx)
	}
	if m.sweeperRunner != nil {
		go m.sweeperRunner.Run(ctx)
	}
	if m.agentUpdateSweeper != nil {
		go m.agentUpdateSweeper.Run(ctx)
	}
	// Chunk-overrun guard: stop tasks running well past their chunk-time target.
	go m.runOverrunGuard(ctx)

	// Warm the compatibility cache and start a periodic refresh
	// goroutine that re-evaluates everything every 30 seconds.
	// Misses between refreshes fall through to lazy single-pair
	// EvaluatePair so this isn't a correctness backstop, just a
	// freshness one.
	if m.compatCache != nil {
		if err := m.compatCache.WarmAll(ctx); err != nil {
			debug.Warning("compat cache initial warm failed: %v", err)
		}
		go m.runCompatCacheRefresh(ctx)
	}
}

// runCompatCacheRefresh periodically rewarms the cache so an
// undelivered OnAgent / OnUnit invalidation can't leave stale rows
// forever. 30s is plenty — the cycle runs every 3s and is the only
// reader; staleness only matters within that window.
func (m *JobIntegrationManager) runCompatCacheRefresh(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.compatCache.WarmAll(ctx); err != nil {
				debug.Warning("compat cache periodic warm failed: %v", err)
			}
		}
	}
}

// DiagnosticsService exposes the buffered diagnostics store so HTTP handlers
// (e.g. the agent detail page) can read force-flushed "why idle" reasons.
func (m *JobIntegrationManager) DiagnosticsService() *services.DiagnosticsService {
	return m.diagnosticsService
}

// StopJob stops a running job
func (m *JobIntegrationManager) StopJob(ctx context.Context, jobExecutionID uuid.UUID, reason string) error {
	debug.Log("Stop job requested", map[string]interface{}{
		"job_execution_id": jobExecutionID,
		"reason":           reason,
	})

	// Stop the job in the scheduling service
	err := m.jobSchedulingService.StopJob(ctx, jobExecutionID, reason)
	if err != nil {
		return fmt.Errorf("failed to stop job: %w", err)
	}

	// Get all tasks for this job execution
	tasks, err := m.wsIntegration.jobTaskRepo.GetTasksByJobExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get tasks for job: %w", err)
	}

	// Send stop commands to all agents running tasks for this job
	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning {
			// Skip if no agent assigned
			if task.AgentID == nil {
				continue
			}

			// Get agent details
			agent, err := m.wsIntegration.agentRepo.GetByID(ctx, *task.AgentID)
			if err != nil {
				debug.Log("Failed to get agent for task stop", map[string]interface{}{
					"task_id":  task.ID,
					"agent_id": task.AgentID,
					"error":    err.Error(),
				})
				continue
			}

			// Send stop command to agent
			err = m.wsIntegration.SendJobStop(ctx, task.ID, reason)
			if err != nil {
				debug.Log("Failed to send stop command to agent", map[string]interface{}{
					"task_id":  task.ID,
					"agent_id": agent.ID,
					"error":    err.Error(),
				})
			}
		}
	}

	return nil
}

// GetConnectedAgentCount returns the number of connected agents
func (m *JobIntegrationManager) GetConnectedAgentCount() int {
	return len(m.wsHandler.GetConnectedAgents())
}

// GetTask retrieves a task by ID from the database
func (m *JobIntegrationManager) GetTask(ctx context.Context, taskID string) (*models.JobTask, error) {
	// Parse task ID as UUID
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return nil, fmt.Errorf("invalid task ID format: %w", err)
	}

	// Get the task from database
	task, err := m.wsIntegration.jobTaskRepo.GetByID(ctx, taskUUID)
	if err != nil {
		return nil, err
	}

	return task, nil
}
