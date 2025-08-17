package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobIntegrationManager manages the integration between WebSocket and job execution services
type JobIntegrationManager struct {
	wsIntegration        *JobWebSocketIntegration
	jobSchedulingService *services.JobSchedulingService
	wsHandler            interface {
		SendMessage(agentID int, msg *wsservice.Message) error
		GetConnectedAgents() []int
	}
}

// NewJobIntegrationManager creates a new job integration manager
func NewJobIntegrationManager(
	wsHandler interface {
		SendMessage(agentID int, msg *wsservice.Message) error
		GetConnectedAgents() []int
	},
	jobSchedulingService *services.JobSchedulingService,
	jobExecutionService *services.JobExecutionService,
	hashlistSyncService *services.HashlistSyncService,
	benchmarkRepo *repository.BenchmarkRepository,
	presetJobRepo repository.PresetJobRepository,
	hashlistRepo *repository.HashListRepository,
	hashRepo *repository.HashRepository,
	jobTaskRepo *repository.JobTaskRepository,
	agentRepo *repository.AgentRepository,
	deviceRepo *repository.AgentDeviceRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	potfileService *services.PotfileService,
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
		jobTaskRepo,
		agentRepo,
		deviceRepo,
		systemSettingsRepo,
		potfileService,
		db,
		wordlistManager,
		ruleManager,
		binaryManager,
	)

	// Set the WebSocket integration in the scheduling service
	jobSchedulingService.SetWebSocketIntegration(wsIntegration)

	return &JobIntegrationManager{
		wsIntegration:        wsIntegration,
		jobSchedulingService: jobSchedulingService,
		wsHandler:            wsHandler,
	}
}

// ProcessJobProgress handles job progress messages from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessJobProgress(ctx context.Context, agentID int, payload json.RawMessage) error {
	var progress models.JobProgress
	if err := json.Unmarshal(payload, &progress); err != nil {
		return fmt.Errorf("failed to unmarshal job progress: %w", err)
	}

	return m.wsIntegration.HandleJobProgress(ctx, agentID, &progress)
}

// ProcessBenchmarkResult handles benchmark result messages from agents (implements interfaces.JobHandler)
func (m *JobIntegrationManager) ProcessBenchmarkResult(ctx context.Context, agentID int, payload json.RawMessage) error {
	var result wsservice.BenchmarkResultPayload
	if err := json.Unmarshal(payload, &result); err != nil {
		return fmt.Errorf("failed to unmarshal benchmark result: %w", err)
	}

	return m.wsIntegration.HandleBenchmarkResult(ctx, agentID, &result)
}

// RecoverTask attempts to recover a task that was in reconnect_pending state (implements interfaces.JobHandler)
func (m *JobIntegrationManager) RecoverTask(ctx context.Context, taskID string, agentID int, keyspaceProcessed int64) error {
	return m.wsIntegration.RecoverTask(ctx, taskID, agentID, keyspaceProcessed)
}

// GetWebSocketIntegration returns the WebSocket integration instance
func (m *JobIntegrationManager) GetWebSocketIntegration() *JobWebSocketIntegration {
	return m.wsIntegration
}

// StartScheduler starts the job scheduling service
func (m *JobIntegrationManager) StartScheduler(ctx context.Context) {
	debug.Log("Starting job scheduler", nil)
	// Start scheduler with 30 second interval
	go m.jobSchedulingService.StartScheduler(ctx, 30*time.Second)
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
