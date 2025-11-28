package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"strconv"
	"strings"
)

// JobWebSocketIntegration handles the integration between job scheduling and WebSocket communication
type JobWebSocketIntegration struct {
	wsHandler interface {
		SendMessage(agentID int, msg *wsservice.Message) error
	}
	jobSchedulingService *services.JobSchedulingService
	jobExecutionService  *services.JobExecutionService
	hashlistSyncService  *services.HashlistSyncService
	benchmarkRepo        *repository.BenchmarkRepository
	presetJobRepo        repository.PresetJobRepository
	hashlistRepo         *repository.HashListRepository
	hashRepo             *repository.HashRepository
	lmHashRepo           *repository.LMHashRepository
	jobTaskRepo           *repository.JobTaskRepository
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	agentRepo             *repository.AgentRepository
	deviceRepo            *repository.AgentDeviceRepository
	clientRepo            *repository.ClientRepository
	systemSettingsRepo    *repository.SystemSettingsRepository
	potfileService          *services.PotfileService
	hashlistCompletionService *services.HashlistCompletionService
	db                      *sql.DB
	wordlistManager         wordlist.Manager
	ruleManager             rule.Manager
	binaryManager           binary.Manager

	// Progress tracking
	progressMutex   sync.RWMutex
	taskProgressMap map[string]*models.JobProgress // TaskID -> Progress
}

// NewJobWebSocketIntegration creates a new job WebSocket integration service
func NewJobWebSocketIntegration(
	wsHandler interface {
		SendMessage(agentID int, msg *wsservice.Message) error
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
	clientRepo *repository.ClientRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	potfileService *services.PotfileService,
	hashlistCompletionService *services.HashlistCompletionService,
	db *sql.DB,
	wordlistManager wordlist.Manager,
	ruleManager rule.Manager,
	binaryManager binary.Manager,
) *JobWebSocketIntegration {
	return &JobWebSocketIntegration{
		wsHandler:                 wsHandler,
		jobSchedulingService:      jobSchedulingService,
		jobExecutionService:       jobExecutionService,
		hashlistSyncService:       hashlistSyncService,
		benchmarkRepo:             benchmarkRepo,
		presetJobRepo:             presetJobRepo,
		hashlistRepo:              hashlistRepo,
		hashRepo:                  hashRepo,
		lmHashRepo:                lmHashRepo,
		jobTaskRepo:               jobTaskRepo,
		jobIncrementLayerRepo:     jobIncrementLayerRepo,
		agentRepo:                 agentRepo,
		deviceRepo:                deviceRepo,
		clientRepo:                clientRepo,
		systemSettingsRepo:        systemSettingsRepo,
		potfileService:            potfileService,
		hashlistCompletionService: hashlistCompletionService,
		db:                        db,
		wordlistManager:           wordlistManager,
		ruleManager:               ruleManager,
		binaryManager:             binaryManager,
		taskProgressMap:           make(map[string]*models.JobProgress),
	}
}

// SyncAgentFiles triggers a file sync and waits for completion
func (s *JobWebSocketIntegration) SyncAgentFiles(ctx context.Context, agentID int, timeout time.Duration) error {
	// Reset agent sync status to pending before sending sync request
	// This ensures we wait for a NEW sync completion, not an old one
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	agent.SyncStatus = models.AgentSyncStatusPending
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to reset agent sync status: %w", err)
	}

	debug.Log("Reset agent sync status to pending", map[string]interface{}{
		"agent_id": agentID,
	})

	// Create file sync request payload
	payload := map[string]interface{}{
		"request_id": fmt.Sprintf("sync-%d-%d", agentID, time.Now().UnixNano()),
		"file_types": []string{"wordlist", "rule", "binary"},
	}

	payloadBytes, _ := json.Marshal(payload)
	msg := &wsservice.Message{
		Type:    wsservice.TypeSyncRequest,
		Payload: payloadBytes,
	}

	// Send sync request to agent
	if err := s.wsHandler.SendMessage(agentID, msg); err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}

	debug.Log("Sent file sync request to agent, waiting for completion", map[string]interface{}{
		"agent_id": agentID,
		"timeout":  timeout,
	})

	// Poll agent.SyncStatus until completed or timeout
	deadline := time.Now().Add(timeout)
	pollInterval := 1 * time.Second

	for time.Now().Before(deadline) {
		agent, err := s.agentRepo.GetByID(ctx, agentID)
		if err != nil {
			return fmt.Errorf("failed to get agent status: %w", err)
		}

		if agent.SyncStatus == models.AgentSyncStatusCompleted {
			debug.Log("Agent file sync completed successfully", map[string]interface{}{
				"agent_id": agentID,
			})
			return nil
		}

		if agent.SyncStatus == models.AgentSyncStatusFailed {
			errMsg := "unknown error"
			if agent.SyncError.Valid {
				errMsg = agent.SyncError.String
			}
			return fmt.Errorf("agent sync failed: %s", errMsg)
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("sync timed out after %v", timeout)
}

// SendJobAssignment sends a job task assignment to an agent via WebSocket
func (s *JobWebSocketIntegration) SendJobAssignment(ctx context.Context, task *models.JobTask, jobExecution *models.JobExecution) error {
	debug.Log("Sending job assignment to agent", map[string]interface{}{
		"task_id":  task.ID,
		"agent_id": task.AgentID,
		"job_id":   jobExecution.ID,
	})

	// Get agent details to find agent int ID
	if task.AgentID == nil {
		return fmt.Errorf("task has no agent assigned")
	}
	agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Get hashlist details
	hashlist, err := s.hashlistRepo.GetByID(ctx, jobExecution.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Check if this task belongs to an increment layer
	var maskToUse string
	if task.IncrementLayerID != nil {
		// This task belongs to a layer - fetch the layer to get its mask
		layer, err := s.jobIncrementLayerRepo.GetByID(ctx, *task.IncrementLayerID)
		if err != nil {
			return fmt.Errorf("failed to get increment layer: %w", err)
		}

		maskToUse = layer.Mask

		debug.Log("Using layer-specific mask for task", map[string]interface{}{
			"task_id":    task.ID,
			"layer_id":   layer.ID,
			"layer_mask": layer.Mask,
			"layer_idx":  layer.LayerIndex,
		})
	} else {
		// Regular job - use job's mask
		maskToUse = jobExecution.Mask
	}

	// Build wordlist and rule paths using job execution's self-contained configuration
	var wordlistPaths []string
	for _, wordlistIDStr := range jobExecution.WordlistIDs {
		// Convert string ID to int
		wordlistID, err := strconv.Atoi(wordlistIDStr)
		if err != nil {
			return fmt.Errorf("invalid wordlist ID %s: %w", wordlistIDStr, err)
		}

		// Look up the actual wordlist file path
		wordlist, err := s.wordlistManager.GetWordlist(ctx, wordlistID)
		if err != nil {
			return fmt.Errorf("failed to get wordlist %d: %w", wordlistID, err)
		}
		if wordlist == nil {
			return fmt.Errorf("wordlist %d not found", wordlistID)
		}

		// Use the actual file path from the database
		wordlistPath := fmt.Sprintf("wordlists/%s", wordlist.FileName)
		wordlistPaths = append(wordlistPaths, wordlistPath)
	}

	var rulePaths []string
	// Check if this is a rule split task with a chunk file
	if task.IsRuleSplitTask && task.RuleChunkPath != nil && *task.RuleChunkPath != "" {
		// Extract job directory from the chunk path
		pathParts := strings.Split(*task.RuleChunkPath, string(filepath.Separator))
		var jobDirName string
		chunkFilename := filepath.Base(*task.RuleChunkPath)

		// Find the job directory name
		for i, part := range pathParts {
			if strings.HasPrefix(part, "job_") && i < len(pathParts)-1 {
				jobDirName = part
				break
			}
		}

		// Create the rule path with job directory
		var rulePath string
		if jobDirName != "" {
			rulePath = fmt.Sprintf("rules/chunks/%s/%s", jobDirName, chunkFilename)
		} else {
			// Fallback to just chunk filename
			rulePath = fmt.Sprintf("rules/chunks/%s", chunkFilename)
		}
		rulePaths = append(rulePaths, rulePath)

		debug.Log("Using rule chunk for task", map[string]interface{}{
			"task_id":    task.ID,
			"chunk_path": *task.RuleChunkPath,
			"agent_path": rulePath,
			"job_dir":    jobDirName,
		})
	} else {
		// Standard rule processing
		for _, ruleIDStr := range jobExecution.RuleIDs {
			// Convert string ID to int
			ruleID, err := strconv.Atoi(ruleIDStr)
			if err != nil {
				return fmt.Errorf("invalid rule ID %s: %w", ruleIDStr, err)
			}

			// Look up the actual rule file path
			rule, err := s.ruleManager.GetRule(ctx, ruleID)
			if err != nil {
				return fmt.Errorf("failed to get rule %d: %w", ruleID, err)
			}
			if rule == nil {
				return fmt.Errorf("rule %d not found", ruleID)
			}

			// Use the actual file path from the database
			rulePath := fmt.Sprintf("rules/%s", rule.FileName)
			rulePaths = append(rulePaths, rulePath)
		}
	}

	// Determine which binary to use: Agent → Job → Default
	effectiveBinaryID, err := s.jobExecutionService.DetermineBinaryForTask(ctx, agent.ID, jobExecution.ID)
	if err != nil {
		return fmt.Errorf("failed to determine binary for task: %w", err)
	}

	// Get binary version details
	binaryVersion, err := s.binaryManager.GetVersion(ctx, effectiveBinaryID)
	if err != nil {
		return fmt.Errorf("failed to get binary version %d: %w", effectiveBinaryID, err)
	}
	if binaryVersion == nil {
		return fmt.Errorf("binary version %d not found", effectiveBinaryID)
	}

	// Use the actual binary path - the ID is used as the directory name
	binaryPath := fmt.Sprintf("binaries/%d", binaryVersion.ID)

	// Get report interval from settings or use default
	reportInterval := 5 // Default 5 seconds
	if val, err := s.jobExecutionService.GetSystemSetting(ctx, "progress_reporting_interval"); err == nil {
		reportInterval = val
	}

	// Get enabled devices for the agent
	var enabledDeviceIDs []int
	if task.AgentID != nil {
		devices, err := s.deviceRepo.GetByAgentID(*task.AgentID)
		if err != nil {
			debug.Error("Failed to get agent devices: %v", err)
			// Continue without device specification
		} else {
			// Only include device IDs if some devices are disabled
			hasDisabledDevice := false
			for _, device := range devices {
				if !device.Enabled {
					hasDisabledDevice = true
				} else {
					enabledDeviceIDs = append(enabledDeviceIDs, device.GetHashcatDeviceID())
				}
			}
			// If all devices are enabled, don't include the device list
			if !hasDisabledDevice {
				enabledDeviceIDs = nil
			}
		}
	}

	// Create task assignment payload
	assignment := wsservice.TaskAssignmentPayload{
		TaskID:          task.ID.String(),
		JobExecutionID:  jobExecution.ID.String(),
		HashlistID:      jobExecution.HashlistID,
		HashlistPath:    fmt.Sprintf("hashlists/%d.hash", jobExecution.HashlistID),
		AttackMode:      int(jobExecution.AttackMode),
		HashType:        hashlist.HashTypeID,
		KeyspaceStart:   task.KeyspaceStart,
		KeyspaceEnd:     task.KeyspaceEnd,
		WordlistPaths:   wordlistPaths,
		RulePaths:       rulePaths,
		Mask:            maskToUse, // Layer mask or job mask
		BinaryPath:      binaryPath,
		ChunkDuration:   task.ChunkDuration,
		ReportInterval:  reportInterval,
		OutputFormat:    "3",                   // hash:plain format
		ExtraParameters: agent.ExtraParameters, // Agent-specific hashcat parameters
		EnabledDevices:  enabledDeviceIDs,      // Only populated if some devices are disabled
		IsKeyspaceSplit: task.IsKeyspaceSplit,
	}

	// Only add increment fields for regular jobs (NOT for layer tasks)
	if task.IncrementLayerID == nil {
		assignment.IncrementMode = jobExecution.IncrementMode
		assignment.IncrementMin = jobExecution.IncrementMin
		assignment.IncrementMax = jobExecution.IncrementMax
	}
	// For layer tasks: increment fields remain empty/unset

	// DEBUG: Log increment mode values before marshaling
	debug.Info("Task assignment increment values - Mode: %s, Min: %v, Max: %v",
		assignment.IncrementMode, assignment.IncrementMin, assignment.IncrementMax)

	// Marshal payload
	payloadBytes, err := json.Marshal(assignment)
	if err != nil {
		return fmt.Errorf("failed to marshal task assignment: %w", err)
	}

	// DEBUG: Log the marshaled JSON
	debug.Info("Marshaled task assignment JSON: %s", string(payloadBytes))

	// Create WebSocket message
	msg := &wsservice.Message{
		Type:    wsservice.TypeTaskAssignment,
		Payload: payloadBytes,
	}

	// Update task status to assigned BEFORE sending via WebSocket
	// This ensures the task is marked as assigned even if WebSocket fails
	err = s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusAssigned)
	if err != nil {
		return fmt.Errorf("failed to update task status to assigned: %w", err)
	}

	// Send via WebSocket
	err = s.wsHandler.SendMessage(agent.ID, msg)
	if err != nil {
		// Revert task status back to pending since we couldn't send it
		revertErr := s.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusPending)
		if revertErr != nil {
			debug.Error("Failed to revert task status after WebSocket error: %v", revertErr)
		}
		return fmt.Errorf("failed to send task assignment via WebSocket: %w", err)
	}

	// Update agent metadata to mark as busy AFTER successful send
	// This prevents agents from getting stuck in busy state if the assignment fails
	if agent.Metadata == nil {
		agent.Metadata = make(map[string]string)
	}
	agent.Metadata["busy_status"] = "true"
	agent.Metadata["current_task_id"] = task.ID.String()
	agent.Metadata["current_job_id"] = jobExecution.ID.String()
	if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
		debug.Error("Failed to update agent metadata after task assignment: %v", err)
		// Don't fail the assignment, the agent is still running the task
	}

	debug.Log("Job assignment sent successfully", map[string]interface{}{
		"task_id":  task.ID,
		"agent_id": agent.ID,
	})

	return nil
}

// SendJobStop sends a stop command for a job to an agent
func (s *JobWebSocketIntegration) SendJobStop(ctx context.Context, taskID uuid.UUID, reason string) error {
	// Get task details
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Get agent details
	if task.AgentID == nil {
		return fmt.Errorf("task has no agent assigned")
	}
	agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	debug.Log("Sending job stop command to agent", map[string]interface{}{
		"task_id":  taskID,
		"agent_id": agent.ID,
		"reason":   reason,
	})

	// Create stop payload
	stopPayload := wsservice.JobStopPayload{
		TaskID: taskID.String(),
		Reason: reason,
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(stopPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal job stop: %w", err)
	}

	// Create WebSocket message
	msg := &wsservice.Message{
		Type:    wsservice.TypeJobStop,
		Payload: payloadBytes,
	}

	// Send via WebSocket
	err = s.wsHandler.SendMessage(agent.ID, msg)
	if err != nil {
		return fmt.Errorf("failed to send job stop via WebSocket: %w", err)
	}

	debug.Log("Job stop command sent successfully", map[string]interface{}{
		"task_id":  taskID,
		"agent_id": agent.ID,
	})

	return nil
}

// SendBenchmarkRequest sends a benchmark request to an agent
// SendForceCleanup sends a force cleanup command to an agent
func (s *JobWebSocketIntegration) SendForceCleanup(ctx context.Context, agentID int) error {
	debug.Log("Sending force cleanup command to agent", map[string]interface{}{
		"agent_id": agentID,
	})

	// Create the force cleanup message
	msg := &wsservice.Message{
		Type: wsservice.TypeForceCleanup,
		// No payload needed for force cleanup
		Payload: json.RawMessage("{}"),
	}

	// Send the message to the agent
	if err := s.wsHandler.SendMessage(agentID, msg); err != nil {
		return fmt.Errorf("failed to send force cleanup: %w", err)
	}

	debug.Log("Force cleanup command sent successfully", map[string]interface{}{
		"agent_id": agentID,
	})

	return nil
}

func (s *JobWebSocketIntegration) SendBenchmarkRequest(ctx context.Context, agentID int, hashType int, attackMode models.AttackMode, binaryVersionID int) error {
	// Get agent details
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	requestID := fmt.Sprintf("benchmark-%d-%d-%d-%d", agentID, hashType, attackMode, time.Now().Unix())
	binaryPath := fmt.Sprintf("binaries/hashcat_%d", binaryVersionID)

	debug.Log("Sending benchmark request to agent", map[string]interface{}{
		"agent_id":    agentID,
		"hash_type":   hashType,
		"attack_mode": attackMode,
		"request_id":  requestID,
	})

	// Create benchmark request payload
	benchmarkReq := wsservice.BenchmarkRequestPayload{
		RequestID:  requestID,
		HashType:   hashType,
		AttackMode: int(attackMode),
		BinaryPath: binaryPath,
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(benchmarkReq)
	if err != nil {
		return fmt.Errorf("failed to marshal benchmark request: %w", err)
	}

	// Create WebSocket message
	msg := &wsservice.Message{
		Type:    wsservice.TypeBenchmarkRequest,
		Payload: payloadBytes,
	}

	// Send via WebSocket
	err = s.wsHandler.SendMessage(agent.ID, msg)
	if err != nil {
		return fmt.Errorf("failed to send benchmark request via WebSocket: %w", err)
	}

	debug.Log("Benchmark request sent successfully", map[string]interface{}{
		"agent_id":   agentID,
		"request_id": requestID,
	})

	return nil
}

// RequestAgentBenchmark implements the JobWebSocketIntegration interface for requesting benchmarks
func (s *JobWebSocketIntegration) RequestAgentBenchmark(ctx context.Context, agentID int, jobExecution *models.JobExecution, layerID *uuid.UUID, layerMask string) error {
	// Get hashlist to get hash type
	hashlist, err := s.hashlistRepo.GetByID(ctx, jobExecution.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Get agent details
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Build wordlist and rule paths for a more accurate benchmark
	var wordlistPaths []string
	for _, wordlistIDStr := range jobExecution.WordlistIDs {
		// Convert string ID to int
		wordlistID, err := strconv.Atoi(wordlistIDStr)
		if err != nil {
			continue // Skip invalid IDs
		}

		// Look up the actual wordlist file path
		wordlist, err := s.wordlistManager.GetWordlist(ctx, wordlistID)
		if err != nil || wordlist == nil {
			continue // Skip missing wordlists
		}

		// Use the actual file path from the database
		wordlistPath := fmt.Sprintf("wordlists/%s", wordlist.FileName)
		wordlistPaths = append(wordlistPaths, wordlistPath)
	}

	var rulePaths []string
	for _, ruleIDStr := range jobExecution.RuleIDs {
		// Convert string ID to int
		ruleID, err := strconv.Atoi(ruleIDStr)
		if err != nil {
			continue // Skip invalid IDs
		}

		// Look up the actual rule file path
		rule, err := s.ruleManager.GetRule(ctx, ruleID)
		if err != nil || rule == nil {
			continue // Skip missing rules
		}

		// Use the actual file path from the database
		rulePath := fmt.Sprintf("rules/%s", rule.FileName)
		rulePaths = append(rulePaths, rulePath)
	}

	// Determine which binary to use: Agent → Job → Default
	effectiveBinaryID, err := s.jobExecutionService.DetermineBinaryForTask(ctx, agent.ID, jobExecution.ID)
	if err != nil {
		return fmt.Errorf("failed to determine binary for benchmark: %w", err)
	}

	// Get binary path from binary version
	binaryVersion, err := s.binaryManager.GetVersion(ctx, effectiveBinaryID)
	if err != nil {
		return fmt.Errorf("failed to get binary version %d: %w", effectiveBinaryID, err)
	}
	if binaryVersion == nil {
		return fmt.Errorf("binary version %d not found", effectiveBinaryID)
	}

	// Use the actual binary path - the ID is used as the directory name
	binaryPath := fmt.Sprintf("binaries/%d", binaryVersion.ID)

	// Get enabled devices for the agent
	var enabledDeviceIDs []int
	devices, err := s.deviceRepo.GetByAgentID(agentID)
	if err != nil {
		debug.Error("Failed to get agent devices for benchmark: %v", err)
		// Continue without device specification
	} else {
		// Only include device IDs if some devices are disabled
		hasDisabledDevice := false
		for _, device := range devices {
			if !device.Enabled {
				hasDisabledDevice = true
			} else {
				enabledDeviceIDs = append(enabledDeviceIDs, device.GetHashcatDeviceID())
			}
		}
		// If all devices are enabled, don't include the device list
		if !hasDisabledDevice {
			enabledDeviceIDs = nil
		}
	}

	// Determine which ID and mask to use for the benchmark
	benchmarkEntityID := jobExecution.ID.String()
	maskToUse := jobExecution.Mask

	if layerID != nil && layerMask != "" {
		// This is a layer benchmark - use layer ID and mask
		benchmarkEntityID = layerID.String()
		maskToUse = layerMask

		debug.Log("Requesting layer-specific benchmark", map[string]interface{}{
			"job_id":     jobExecution.ID,
			"layer_id":   layerID,
			"layer_mask": layerMask,
		})
	}

	requestID := fmt.Sprintf("benchmark-%d-%d-%d-%d", agentID, hashlist.HashTypeID, jobExecution.AttackMode, time.Now().Unix())

	debug.Log("Sending enhanced benchmark request to agent", map[string]interface{}{
		"agent_id":        agentID,
		"hash_type":       hashlist.HashTypeID,
		"attack_mode":     jobExecution.AttackMode,
		"request_id":      requestID,
		"wordlist_count":  len(wordlistPaths),
		"rule_count":      len(rulePaths),
		"has_mask":        maskToUse != "",
		"mask":            maskToUse,
		"entity_id":       benchmarkEntityID,
		"is_layer":        layerID != nil,
		"enabled_devices": enabledDeviceIDs,
	})

	// Get speedtest timeout from system settings
	speedtestTimeout := 180 // Default to 3 minutes
	if s.systemSettingsRepo != nil {
		if setting, err := s.systemSettingsRepo.GetSetting(ctx, "speedtest_timeout_seconds"); err == nil && setting.Value != nil {
			if timeout, err := strconv.Atoi(*setting.Value); err == nil && timeout > 0 {
				speedtestTimeout = timeout
			}
		}
	}

	// Create enhanced benchmark request payload with job-specific configuration
	benchmarkReq := wsservice.BenchmarkRequestPayload{
		RequestID:       requestID,
		JobExecutionID:  benchmarkEntityID,                                                  // LAYER ID for layer benchmarks, JOB ID for regular
		TaskID:          fmt.Sprintf("benchmark-%s-%d", benchmarkEntityID, time.Now().Unix()), // Generate a task ID for the benchmark
		HashType:        hashlist.HashTypeID,
		AttackMode:      int(jobExecution.AttackMode),
		BinaryPath:      binaryPath,
		HashlistID:      jobExecution.HashlistID,
		HashlistPath:    fmt.Sprintf("hashlists/%d.hash", jobExecution.HashlistID),
		WordlistPaths:   wordlistPaths,
		RulePaths:       rulePaths,
		Mask:            maskToUse,             // LAYER MASK for layer benchmarks, JOB MASK for regular
		TestDuration:    30,                    // 30-second benchmark for accuracy
		TimeoutDuration: speedtestTimeout,      // Configurable timeout for speedtest
		ExtraParameters: agent.ExtraParameters, // Agent-specific hashcat parameters
		EnabledDevices:  enabledDeviceIDs,      // Only populated if some devices are disabled
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(benchmarkReq)
	if err != nil {
		return fmt.Errorf("failed to marshal benchmark request: %w", err)
	}

	// Create WebSocket message
	msg := &wsservice.Message{
		Type:    wsservice.TypeBenchmarkRequest,
		Payload: payloadBytes,
	}

	// Send via WebSocket
	err = s.wsHandler.SendMessage(agent.ID, msg)
	if err != nil {
		return fmt.Errorf("failed to send benchmark request via WebSocket: %w", err)
	}

	debug.Log("Enhanced benchmark request sent successfully", map[string]interface{}{
		"agent_id":   agentID,
		"request_id": requestID,
	})

	return nil
}

// HandleJobProgress processes job progress updates from agents
func (s *JobWebSocketIntegration) HandleJobProgress(ctx context.Context, agentID int, progress *models.JobProgress) error {
	debug.Log("Processing job progress from agent", map[string]interface{}{
		"agent_id":           agentID,
		"task_id":            progress.TaskID,
		"keyspace_processed": progress.KeyspaceProcessed,
		"effective_progress": progress.EffectiveProgress,
		"progress_percent":   progress.ProgressPercent,
		"hash_rate":          progress.HashRate,
	})

	// Validate task exists before processing
	task, err := s.jobTaskRepo.GetByID(ctx, progress.TaskID)
	if err != nil {
		// Log and ignore progress updates for non-existent tasks (could be orphaned)
		debug.Warning("Received progress for non-existent task %d (ignoring): agent=%d, error=%v", progress.TaskID, agentID, err)
		// Don't return error - just ignore the update
		return nil
	}

	// Verify the task is assigned to this agent
	if task.AgentID == nil || *task.AgentID != agentID {
		expectedAgent := 0
		if task.AgentID != nil {
			expectedAgent = *task.AgentID
		}
		debug.Error("Progress from wrong agent: task=%d, expected=%d, actual=%d", progress.TaskID, expectedAgent, agentID)
		return fmt.Errorf("task not assigned to this agent")
	}

	// Update task status to running if it's still assigned
	if task.Status == models.JobTaskStatusAssigned {
		// Use StartTask to update both status and started_at timestamp
		err = s.jobTaskRepo.StartTask(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to start task", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
			// Fallback to just updating status
			err = s.jobTaskRepo.UpdateStatus(ctx, progress.TaskID, models.JobTaskStatusRunning)
			if err != nil {
				debug.Log("Failed to update task status to running", map[string]interface{}{
					"task_id": progress.TaskID,
					"error":   err.Error(),
				})
			}
		} else {
			debug.Log("Started task", map[string]interface{}{
				"task_id": progress.TaskID,
			})
		}

		// If this is an increment layer task starting for the first time, update layer status to running
		if task.IncrementLayerID != nil {
			layer, err := s.jobIncrementLayerRepo.GetByID(ctx, *task.IncrementLayerID)
			if err != nil {
				debug.Error("Failed to get layer for status update: %v", err)
			} else if layer.Status == models.JobIncrementLayerStatusPending {
				debug.Log("Updating layer status from pending to running", map[string]interface{}{
					"layer_id":   task.IncrementLayerID,
					"task_id":    progress.TaskID,
				})

				err = s.jobIncrementLayerRepo.UpdateStatus(ctx, *task.IncrementLayerID, models.JobIncrementLayerStatusRunning)
				if err != nil {
					debug.Error("Failed to update layer status to running: %v", err)
				} else {
					debug.Log("Layer status updated to running", map[string]interface{}{
						"layer_id": task.IncrementLayerID,
					})
				}
			}
		}
	}

	// Update task effective keyspace from hashcat progress[1] if we haven't already
	// IMPORTANT: For keyspace-split tasks (mask attacks with --skip/--limit), progress[1] reports the
	// ENTIRE job's effective keyspace, not the chunk's. We should NOT update effective keyspace for
	// keyspace-split tasks because we already calculated proportional values during task creation.
	// Only update for rule-split tasks where progress[1] correctly reflects the chunk's rule range.
	if progress.TotalEffectiveKeyspace != nil && *progress.TotalEffectiveKeyspace > 0 && !task.IsActualKeyspace && !task.IsKeyspaceSplit {
		// IMPORTANT: progress.TotalEffectiveKeyspace is the CHUNK's actual keyspace size (not cumulative!)
		// It represents the total keyspace for this specific chunk's rules (only valid for rule-split tasks)
		chunkActualKeyspace := *progress.TotalEffectiveKeyspace

		// Get the current start position (where this chunk begins in the cumulative keyspace)
		effectiveStart := int64(0)
		if task.EffectiveKeyspaceStart != nil {
			effectiveStart = *task.EffectiveKeyspaceStart
		}

		// Calculate new end = start + chunk's actual size
		actualEffectiveEnd := effectiveStart + chunkActualKeyspace

		// Update task with actual values AND store chunk size for cascade calculations
		err = s.jobTaskRepo.UpdateTaskEffectiveKeyspaceWithChunkSize(ctx, progress.TaskID,
			effectiveStart, actualEffectiveEnd, chunkActualKeyspace)
		if err != nil {
			debug.Error("Failed to update task effective keyspace from progress[1]: %v", err)
		} else {
			debug.Info("Updated task %s: start=%d, end=%d, chunk_size=%d (is_actual_keyspace=true)",
				progress.TaskID, effectiveStart, actualEffectiveEnd, chunkActualKeyspace)

			// Get job execution repository for effective keyspace updates
			database := &db.DB{DB: s.db}
			jobExecRepo := repository.NewJobExecutionRepository(database)

			// Get job details for keyspace update logic
			job, err := jobExecRepo.GetByID(ctx, task.JobExecutionID)
			if err != nil {
				debug.Error("Failed to get job for keyspace update: %v", err)
			} else {
				// Handle increment mode jobs differently - update layer, then recalc job total
				if job.IncrementMode != "" && job.IncrementMode != "off" && task.IncrementLayerID != nil {
					// Update the layer's effective keyspace
					err = s.jobIncrementLayerRepo.UpdateEffectiveKeyspace(ctx, *task.IncrementLayerID, chunkActualKeyspace)
					if err != nil {
						debug.Error("Failed to update layer effective keyspace: %v", err)
					} else {
						debug.Info("Updated layer %s effective_keyspace to %d (actual from hashcat)",
							*task.IncrementLayerID, chunkActualKeyspace)

						// Recalculate job's total effective keyspace as sum of all layers
						totalKeyspace, err := s.jobIncrementLayerRepo.GetTotalEffectiveKeyspace(ctx, task.JobExecutionID)
						if err != nil {
							debug.Error("Failed to get total effective keyspace from layers: %v", err)
						} else {
							// Update job's effective keyspace to the sum of all layers
							err = jobExecRepo.UpdateEffectiveKeyspace(ctx, task.JobExecutionID, totalKeyspace)
							if err != nil {
								debug.Error("Failed to update job effective keyspace from layer sum: %v", err)
							} else {
								oldEffective := int64(0)
								if job.EffectiveKeyspace != nil {
									oldEffective = *job.EffectiveKeyspace
								}
								debug.Info("Updated increment job %s effective_keyspace from %d to %d (sum of all layers)",
									task.JobExecutionID, oldEffective, totalKeyspace)
							}
						}
					}
				} else if !job.UsesRuleSplitting && task.ChunkNumber == 1 {
					// Regular (non-increment) single-task jobs - update effective_keyspace to match actual
					// This ensures progress calculations use actual keyspace, not estimates
					// Check if this is the only task for this job
					allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, task.JobExecutionID)
					if err == nil && len(allTasks) == 1 {
						// Single task job - update effective_keyspace to match actual total
						if job.EffectiveKeyspace != nil {
							newEffectiveKeyspace := chunkActualKeyspace
							if *job.EffectiveKeyspace != newEffectiveKeyspace {
								err = jobExecRepo.UpdateEffectiveKeyspace(ctx, task.JobExecutionID, newEffectiveKeyspace)
								if err != nil {
									debug.Error("Failed to update job effective keyspace to actual: %v", err)
								} else {
									debug.Info("Updated job %s effective_keyspace from %d (estimated) to %d (actual from hashcat)",
										task.JobExecutionID, *job.EffectiveKeyspace, newEffectiveKeyspace)
								}
							}
						}
					}
				}
			}

			// PROGRESSIVE REFINEMENT: Recalculate job's effective_keyspace based on completed actuals + estimate for remaining
			// This handles multi-task jobs where hashlist changes between tasks
			// Reuse job variable from above
			// IMPORTANT: Only refine if we have a valid baseline from benchmark
			// Progressive refinement should ENHANCE accuracy, not replace initial benchmark value
			if job != nil && job.UsesRuleSplitting && job.IsAccurateKeyspace && job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
				// Get all tasks for this job
				allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, task.JobExecutionID)
				if err == nil && len(allTasks) > 0 {
					// Calculate: sum of actuals + smart estimate for remaining
					totalActualKeyspace := int64(0)
					totalActualRules := 0
					totalRemainingRules := 0
					pendingTaskCount := 0

					for _, t := range allTasks {
						// Include tasks that have reported actual keyspace (completed OR running with actual)
						if t.ChunkActualKeyspace != nil && *t.ChunkActualKeyspace > 0 {
							totalActualKeyspace += *t.ChunkActualKeyspace
							if t.RuleStartIndex != nil && t.RuleEndIndex != nil {
								totalActualRules += (*t.RuleEndIndex - *t.RuleStartIndex)
							}
						} else if t.Status == "pending" {
							// Only count truly pending tasks (not running)
							pendingTaskCount++
							if t.RuleStartIndex != nil && t.RuleEndIndex != nil {
								totalRemainingRules += (*t.RuleEndIndex - *t.RuleStartIndex)
							}
						}
					}

					// Calculate new effective_keyspace
					newEffectiveKeyspace := totalActualKeyspace

					if pendingTaskCount > 0 && totalActualRules > 0 {
						// Estimate remaining based on: (avg keyspace per rule from completed) × (remaining rules)
						// Use current hashlist size for estimate
						hashlistRepo := repository.NewHashListRepository(database)
						currentHashCount, err := hashlistRepo.GetUncrackedHashCount(ctx, job.HashlistID)
						if err == nil && currentHashCount > 0 {
							// Average actual keyspace per rule from completed tasks
							avgKeyspacePerRule := float64(totalActualKeyspace) / float64(totalActualRules)

							// Estimate for remaining tasks using CURRENT hashlist size
							estimatedRemaining := int64(avgKeyspacePerRule * float64(totalRemainingRules))

							newEffectiveKeyspace = totalActualKeyspace + estimatedRemaining

							debug.Info("Progressive refinement for job %s: actual=%d (from %d rules), estimated=%d (for %d rules with %d hashes), total=%d",
								task.JobExecutionID, totalActualKeyspace, totalActualRules, estimatedRemaining, totalRemainingRules, currentHashCount, newEffectiveKeyspace)
						}
					}

					// Update if changed significantly (avoid tiny fluctuations)
					if job.EffectiveKeyspace == nil || absInt64(*job.EffectiveKeyspace-newEffectiveKeyspace) > 1000 {
						// SAFETY: Never reduce effective_keyspace to 0 or a tiny value for rule-split jobs
						// This prevents overwriting benchmark results with incomplete chunk data
						if newEffectiveKeyspace == 0 {
							debug.Log("Skipping progressive refinement - calculated keyspace is 0", map[string]interface{}{
								"job_id": task.JobExecutionID,
								"current_effective": *job.EffectiveKeyspace,
							})
						} else if job.EffectiveKeyspace != nil && newEffectiveKeyspace < (*job.EffectiveKeyspace / 10) {
							// New value is less than 10% of current - suspicious, log warning
							debug.Warning("Skipping progressive refinement - new value too low", map[string]interface{}{
								"job_id": task.JobExecutionID,
								"current": *job.EffectiveKeyspace,
								"new": newEffectiveKeyspace,
								"reduction_percent": (1.0 - float64(newEffectiveKeyspace)/float64(*job.EffectiveKeyspace)) * 100,
							})
						} else {
							// Safe to update
							err = jobExecRepo.UpdateEffectiveKeyspace(ctx, task.JobExecutionID, newEffectiveKeyspace)
							if err != nil {
								debug.Error("Failed to update progressive effective keyspace: %v", err)
							} else {
								oldValue := int64(0)
								if job.EffectiveKeyspace != nil {
									oldValue = *job.EffectiveKeyspace
								}
								debug.Info("Updated job %s effective_keyspace from %d to %d (progressive refinement)",
									task.JobExecutionID, oldValue, newEffectiveKeyspace)
							}
						}
					}
				}
			}

			// CASCADE: Recalculate all subsequent chunks' positions
			// IMPORTANT: Only do this for rule-split tasks where chunk_actual_keyspace accurately represents
			// the chunk's portion of work. For keyspace-split tasks, hashcat's progress[1] reports the
			// ENTIRE job's effective keyspace (not just the chunk's), so cascade recalculation would corrupt
			// the effective keyspace chain with incorrect values.
			if task.ChunkNumber > 0 && !task.IsKeyspaceSplit {
				err = s.recalculateSubsequentChunks(ctx, task.JobExecutionID, task.ChunkNumber)
				if err != nil {
					debug.Error("Failed to cascade update subsequent chunks: %v", err)
				} else {
					debug.Info("Cascaded effective keyspace updates to chunks after chunk %d", task.ChunkNumber)
				}
			}
		}
	}

	// Store progress in memory
	s.progressMutex.Lock()
	s.taskProgressMap[progress.TaskID.String()] = progress
	s.progressMutex.Unlock()

	// Check if this is a failure update
	if progress.Status == "failed" && progress.ErrorMessage != "" {
		debug.Log("Task failed with error", map[string]interface{}{
			"task_id": progress.TaskID,
			"error":   progress.ErrorMessage,
		})

		// Mark task as permanently failed and decrement dispatched keyspace
		// Agent-reported failures are considered permanent and the job will be marked as failed
		err := s.jobTaskRepo.MarkTaskFailedPermanently(ctx, progress.TaskID, progress.ErrorMessage)
		if err != nil {
			debug.Error("Failed to mark task as permanently failed: %v", err)
		}

		// Clear agent busy status
		if task.AgentID != nil {
			agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
			if err == nil && agent.Metadata != nil {
				agent.Metadata["busy_status"] = "false"
				delete(agent.Metadata, "current_task_id")
				delete(agent.Metadata, "current_job_id")
				if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
					debug.Error("Failed to clear agent busy status after task failure: %v", err)
				}
			}
		}

		// Update job execution status to failed
		// Wrap sql.DB in custom DB type
		database := &db.DB{DB: s.db}
		jobExecRepo := repository.NewJobExecutionRepository(database)
		if err := jobExecRepo.UpdateStatus(ctx, task.JobExecutionID, models.JobExecutionStatusFailed); err != nil {
			debug.Error("Failed to update job execution status: %v", err)
		}
		if err := jobExecRepo.UpdateErrorMessage(ctx, task.JobExecutionID, progress.ErrorMessage); err != nil {
			debug.Error("Failed to update job execution error message: %v", err)
		}

		// Handle task failure cleanup
		err = s.jobExecutionService.HandleTaskCompletion(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to handle failed task cleanup", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		return nil
	}

	// Check if all hashes cracked flag is set (status code 6 from hashcat)
	// This check must happen BEFORE status-specific processing because the agent sends this
	// flag with status="running" when hashcat reports status code 6 in JSON output
	if progress.AllHashesCracked {
		debug.Info("Task %s reported all hashes cracked (hashcat status code 6) - triggering hashlist completion handler", progress.TaskID)

		// Part 18a: When all hashes are cracked, the task has fully processed its keyspace
		// Set keyspace_processed to the full chunk size (even if restore_point is 0 due to instant completion)
		fullKeyspaceProcessed := task.KeyspaceEnd - task.KeyspaceStart
		effectiveProcessed := fullKeyspaceProcessed
		if task.EffectiveKeyspaceEnd != nil && task.EffectiveKeyspaceStart != nil {
			effectiveProcessed = *task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart
		}

		// Update task progress to 100% so the task shows correct completion status
		var hashRatePtr *int64
		if progress.HashRate > 0 {
			hashRatePtr = &progress.HashRate
		}
		if err := s.jobTaskRepo.UpdateProgress(ctx, progress.TaskID, fullKeyspaceProcessed, effectiveProcessed, hashRatePtr, 100.0); err != nil {
			debug.Error("Failed to update task progress for all-hashes-cracked: %v", err)
		} else {
			debug.Info("Updated task %s progress to 100%% for all-hashes-cracked (keyspace_processed=%d, effective=%d)",
				progress.TaskID, fullKeyspaceProcessed, effectiveProcessed)
		}

		// Determine expected crack count for processing status
		// Agent should send this in progress.CrackedCount, but if it's 0, get from hashlist
		expectedCracks := progress.CrackedCount

		// Get job to find hashlist ID
		job, err := s.jobExecutionService.GetJobExecutionByID(ctx, task.JobExecutionID)
		if err != nil {
			debug.Error("Failed to get job for hashlist completion check: %v", err)
		} else {
			database := &db.DB{DB: s.db}

			// If agent didn't report crack count, get it from hashlist as fallback
			if expectedCracks == 0 {
				hashlistRepo := repository.NewHashListRepository(database)
				hashlist, hashlistErr := hashlistRepo.GetByID(ctx, job.HashlistID)
				if hashlistErr != nil {
					debug.Error("Failed to get hashlist for crack count fallback: %v", hashlistErr)
				} else if hashlist.CrackedHashes > 0 {
					expectedCracks = hashlist.CrackedHashes
					debug.Warning("Agent sent CrackedCount=0 for AllHashesCracked, using hashlist cracked count as fallback: %d", expectedCracks)
				}
			}

			// Part 18h: Set THIS job's progress to 100% IMMEDIATELY when AllHashesCracked is received.
			// This prevents race conditions where ProcessJobCompletion (layer-based completion)
			// might complete the job before HandleHashlistFullyCracked can set 100% progress.
			// The polling service skips completed jobs, so we must set 100% BEFORE completion.
			jobExecRepo := repository.NewJobExecutionRepository(database)
			if err := jobExecRepo.UpdateProgressPercent(ctx, task.JobExecutionID, 100.0); err != nil {
				debug.Warning("Failed to set job %s progress to 100%% on AllHashesCracked: %v", task.JobExecutionID, err)
			} else {
				debug.Info("Set job %s progress to 100%% on AllHashesCracked (status code 6)", task.JobExecutionID)
			}

			// Part 18f: ALWAYS trigger HandleHashlistFullyCracked BEFORE the early return
			// This ensures all jobs on the hashlist are handled even when we return early
			// for processing mode (waiting for crack batches)
			if s.hashlistCompletionService != nil {
				go func() {
					// Use a background context with timeout to avoid hanging
					bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()

					// Pass the triggering task ID to prevent sending stop signal to it
					taskID := progress.TaskID
					if err := s.hashlistCompletionService.HandleHashlistFullyCracked(bgCtx, job.HashlistID, &taskID); err != nil {
						debug.Error("Failed to handle hashlist fully cracked: %v", err)
					}
				}()
			}

			// Set task to processing if we have expected cracks
			if expectedCracks > 0 {
				debug.Info("Task %s expects %d cracks from all-hashes-cracked - setting to processing status to wait for batches",
					progress.TaskID, expectedCracks)

				// Part 18i: Process any cracked hashes in this message BEFORE returning.
				// This ensures the final crack that triggered AllHashesCracked is added to potfile.
				// Without this, the early return would skip processCrackedHashes at the end of the function.
				if progress.CrackedCount > 0 && len(progress.CrackedHashes) > 0 {
					if err := s.processCrackedHashes(ctx, progress.TaskID, progress.CrackedHashes); err != nil {
						debug.Error("Failed to process cracked hashes on AllHashesCracked: %v", err)
					} else {
						debug.Info("Processed %d cracked hashes from AllHashesCracked message for task %s",
							len(progress.CrackedHashes), progress.TaskID)
					}
				}

				// Set task to processing with expected crack count
				err = s.jobTaskRepo.SetTaskProcessing(ctx, progress.TaskID, expectedCracks)
				if err != nil {
					debug.Error("Failed to set task processing for all-hashes-cracked: %v", err)
					// Continue anyway - hashlist completion will still proceed
				} else {
					debug.Info("Task set to processing for all-hashes-cracked, waiting for crack batches [task_id=%s, expected_cracks=%d]",
						progress.TaskID, expectedCracks)

					// Check if this was the last task with pending work for the job
					// If so, set job to processing status as well
					s.checkJobProcessingStatus(ctx, task.JobExecutionID)

					// Return early to prevent Status=="completed" block from completing the task
					// HandleHashlistFullyCracked was already triggered above
					return nil
				}
			}
		}
	}

	// Check if this is a completion update
	if progress.Status == "completed" {
		debug.Log("Task completed", map[string]interface{}{
			"task_id":          progress.TaskID,
			"progress_percent": progress.ProgressPercent,
			"cracked_count":    progress.CrackedCount,
		})

		// Update the final progress first
		err := s.jobSchedulingService.ProcessTaskProgress(ctx, progress.TaskID, progress)
		if err != nil {
			debug.Error("Failed to process final task progress: %v", err)
		}

		// Check if we need to wait for crack batches
		// If agent reports cracks in progress message, set task to processing status
		// and wait for crack batches + completion signal
		if progress.CrackedCount > 0 {
			debug.Info("Task %s expects %d cracks - setting to processing status to wait for batches",
				progress.TaskID, progress.CrackedCount)

			// Set task to processing with expected crack count
			err = s.jobTaskRepo.SetTaskProcessing(ctx, progress.TaskID, progress.CrackedCount)
			if err != nil {
				debug.Error("Failed to set task processing: %v", err)
				// Fall through to complete anyway on error
			} else {
				// Don't clear agent busy status yet - agent will send crack_batches_complete signal
				// Agent is free to take new work after sending completion signal
				debug.Log("Task set to processing, waiting for crack batches", map[string]interface{}{
					"task_id":         progress.TaskID,
					"expected_cracks": progress.CrackedCount,
				})

				// Check if this was the last task with pending work for the job
				// If so, set job to processing status as well
				s.checkJobProcessingStatus(ctx, task.JobExecutionID)

				return nil
			}
		}

		// No cracks expected, complete task immediately
		debug.Log("Task has no cracks, completing immediately", map[string]interface{}{
			"task_id": progress.TaskID,
		})

		// Mark task as complete
		err = s.jobTaskRepo.CompleteTask(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to mark task as complete", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		// Clear agent busy status
		if task.AgentID != nil {
			agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
			if err == nil && agent.Metadata != nil {
				agent.Metadata["busy_status"] = "false"
				delete(agent.Metadata, "current_task_id")
				delete(agent.Metadata, "current_job_id")
				if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
					debug.Error("Failed to clear agent busy status after task completion: %v", err)
				}
			}
		}

		// Reset consecutive failure counters on success
		err = s.jobSchedulingService.HandleTaskSuccess(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to handle task success", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		// Handle task completion cleanup
		err = s.jobExecutionService.HandleTaskCompletion(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to handle task completion", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		// Check if job is complete
		err = s.jobSchedulingService.ProcessJobCompletion(ctx, task.JobExecutionID)
		if err != nil {
			debug.Log("Failed to process job completion", map[string]interface{}{
				"job_execution_id": task.JobExecutionID,
				"error":            err.Error(),
			})
		}

		return nil
	}

	// Forward to job scheduling service for normal progress updates
	err = s.jobSchedulingService.ProcessTaskProgress(ctx, progress.TaskID, progress)
	if err != nil {
		return fmt.Errorf("failed to process task progress: %w", err)
	}

	// Note: Hash rate metric recording removed here to prevent duplicate entries.
	// The metric is already recorded in job_scheduling_service.go with full device information.

	// Process cracked hashes if any
	if progress.CrackedCount > 0 && len(progress.CrackedHashes) > 0 {
		err = s.processCrackedHashes(ctx, progress.TaskID, progress.CrackedHashes)
		if err != nil {
			debug.Log("Failed to process cracked hashes", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}
	}

	// Check if task is complete based on keyspace
	// KeyspaceProcessed is the restore_point from hashcat, which is ABSOLUTE (not relative to KeyspaceStart)
	// Compare against KeyspaceEnd directly, not (KeyspaceEnd - KeyspaceStart)
	if task.KeyspaceEnd > 0 && progress.KeyspaceProcessed >= task.KeyspaceEnd {
		// Task is complete - restore_point has reached or exceeded the task's end position
		err = s.jobTaskRepo.CompleteTask(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to mark task as complete", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		// Clear agent busy status
		if task.AgentID != nil {
			agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
			if err == nil && agent.Metadata != nil {
				agent.Metadata["busy_status"] = "false"
				delete(agent.Metadata, "current_task_id")
				delete(agent.Metadata, "current_job_id")
				if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
					debug.Error("Failed to clear agent busy status after task completion (keyspace): %v", err)
				}
			}
		}

		// Handle task completion cleanup
		err = s.jobExecutionService.HandleTaskCompletion(ctx, progress.TaskID)
		if err != nil {
			debug.Log("Failed to handle task completion", map[string]interface{}{
				"task_id": progress.TaskID,
				"error":   err.Error(),
			})
		}

		// Check if job is complete
		err = s.jobSchedulingService.ProcessJobCompletion(ctx, task.JobExecutionID)
		if err != nil {
			debug.Log("Failed to process job completion", map[string]interface{}{
				"job_execution_id": task.JobExecutionID,
				"error":            err.Error(),
			})
		}
	}

	return nil
}

// absInt64 returns the absolute value of an int64
func absInt64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// HandleCrackBatch processes crack batch messages from agents
func (s *JobWebSocketIntegration) HandleCrackBatch(ctx context.Context, agentID int, crackBatch *models.CrackBatch) error {
	debug.Log("Processing crack batch from agent", map[string]interface{}{
		"agent_id":      agentID,
		"task_id":       crackBatch.TaskID,
		"crack_count":   len(crackBatch.CrackedHashes),
	})

	// Validate task exists
	task, err := s.jobTaskRepo.GetByID(ctx, crackBatch.TaskID)
	if err != nil {
		debug.Warning("Received crack batch for non-existent task %s (ignoring): agent=%d, error=%v",
			crackBatch.TaskID, agentID, err)
		return nil
	}

	// Verify the task is assigned to this agent
	if task.AgentID == nil || *task.AgentID != agentID {
		expectedAgent := 0
		if task.AgentID != nil {
			expectedAgent = *task.AgentID
		}
		debug.Error("Crack batch from wrong agent: task=%s, expected=%d, actual=%d",
			crackBatch.TaskID, expectedAgent, agentID)
		return fmt.Errorf("task not assigned to this agent")
	}

	// Process cracked hashes with retry logic
	if len(crackBatch.CrackedHashes) > 0 {
		err = s.retryProcessCrackedHashes(ctx, agentID, crackBatch.TaskID, crackBatch.CrackedHashes)
		if err != nil {
			debug.Error("CRITICAL: Crack batch permanently failed after retries [agent_id=%d, task_id=%s, batch_size=%d, error=%v]",
				agentID, crackBatch.TaskID, len(crackBatch.CrackedHashes), err)
			return err
		}

		// Increment received crack count for processing status tracking
		err = s.jobTaskRepo.IncrementReceivedCrackCount(ctx, crackBatch.TaskID, len(crackBatch.CrackedHashes))
		if err != nil {
			debug.Error("Failed to increment received crack count: %v", err)
			// Don't fail the whole operation - cracks are already processed
		} else {
			debug.Log("Incremented received crack count", map[string]interface{}{
				"task_id":     crackBatch.TaskID,
				"batch_size":  len(crackBatch.CrackedHashes),
			})

			// Check if task is ready to complete (only if in processing status)
			if task.Status == models.JobTaskStatusProcessing {
				ready, err := s.jobTaskRepo.CheckTaskReadyToComplete(ctx, crackBatch.TaskID)
				if err != nil {
					debug.Error("Failed to check if task ready to complete: %v", err)
				} else if ready {
					debug.Info("Task %s has received all expected crack batches and agent signaled complete - completing task",
						crackBatch.TaskID)
					s.checkTaskCompletion(ctx, crackBatch.TaskID)
				}
			}
		}
	}

	return nil
}

// retryProcessCrackedHashes wraps processCrackedHashes with retry logic for transient database errors
func (s *JobWebSocketIntegration) retryProcessCrackedHashes(ctx context.Context, agentID int, taskID uuid.UUID, crackedHashes []models.CrackedHash) error {
	const maxRetries = 3
	backoffDelays := []time.Duration{0, 1 * time.Second, 2 * time.Second} // Exponential backoff

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Wait before retry (skip on first attempt)
		if attempt > 1 {
			delay := backoffDelays[attempt-1]
			debug.Info("Retrying crack batch processing (attempt %d/%d, delay=%v) [agent_id=%d, task_id=%s, batch_size=%d]",
				attempt, maxRetries, delay, agentID, taskID, len(crackedHashes))
			time.Sleep(delay)
		}

		// Attempt to process cracks
		err := s.processCrackedHashes(ctx, taskID, crackedHashes)
		if err == nil {
			// Success!
			if attempt > 1 {
				debug.Info("Crack batch processed successfully after %d retries [agent_id=%d, task_id=%s, batch_size=%d]",
					attempt, agentID, taskID, len(crackedHashes))
			}
			return nil
		}

		// Check if error is transient and retryable
		if !isTransientDatabaseError(err) {
			// Non-transient error, fail immediately
			debug.Error("Non-retryable error processing crack batch [agent_id=%d, task_id=%s, batch_size=%d, error=%v]",
				agentID, taskID, len(crackedHashes), err)
			return err
		}

		// Transient error, log and retry
		lastErr = err
		debug.Warning("Transient database error processing crack batch (attempt %d/%d) [agent_id=%d, task_id=%s, batch_size=%d, error=%v]",
			attempt, maxRetries, agentID, taskID, len(crackedHashes), err)
	}

	// All retries exhausted
	return fmt.Errorf("max retries exhausted: %w", lastErr)
}

// isTransientDatabaseError determines if an error is transient and should be retried
func isTransientDatabaseError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// PostgreSQL shared memory exhaustion
	if strings.Contains(errStr, "could not resize shared memory") ||
		strings.Contains(errStr, "no space left on device") {
		return true
	}

	// PostgreSQL deadlocks
	if strings.Contains(errStr, "deadlock detected") {
		return true
	}

	// Connection failures
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection closed") {
		return true
	}

	// Timeouts
	if strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "timeout") {
		return true
	}

	// Temporary network errors
	if strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "too many connections") {
		return true
	}

	return false
}

// HandleCrackBatchesComplete processes crack_batches_complete signal from agents
// This signals that the agent has finished sending all crack batches for a task
func (s *JobWebSocketIntegration) HandleCrackBatchesComplete(ctx context.Context, agentID int, message *models.CrackBatchesComplete) error {
	debug.Log("Processing crack_batches_complete signal from agent", map[string]interface{}{
		"agent_id": agentID,
		"task_id":  message.TaskID,
	})

	// Validate task exists
	task, err := s.jobTaskRepo.GetByID(ctx, message.TaskID)
	if err != nil {
		debug.Warning("Received crack_batches_complete for non-existent task %s (ignoring): agent=%d, error=%v",
			message.TaskID, agentID, err)
		return nil
	}

	// Verify the task is assigned to this agent
	if task.AgentID == nil || *task.AgentID != agentID {
		expectedAgent := 0
		if task.AgentID != nil {
			expectedAgent = *task.AgentID
		}
		debug.Error("crack_batches_complete from wrong agent: task=%s, expected=%d, actual=%d",
			message.TaskID, expectedAgent, agentID)
		return fmt.Errorf("task not assigned to this agent")
	}

	// Mark batches complete
	err = s.jobTaskRepo.MarkBatchesComplete(ctx, message.TaskID)
	if err != nil {
		debug.Error("Failed to mark batches complete: %v", err)
		return err
	}

	// Clear agent busy status - agent is now free for new work
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err == nil && agent.Metadata != nil {
		agent.Metadata["busy_status"] = "false"
		delete(agent.Metadata, "current_task_id")
		delete(agent.Metadata, "current_job_id")
		if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
			debug.Error("Failed to clear agent busy status after batches complete: %v", err)
		} else {
			debug.Log("Cleared agent busy status - agent free for new work", map[string]interface{}{
				"agent_id": agentID,
				"task_id":  message.TaskID,
			})
		}
	}

	// Check if task is ready to complete
	if task.Status == models.JobTaskStatusProcessing {
		ready, err := s.jobTaskRepo.CheckTaskReadyToComplete(ctx, message.TaskID)
		if err != nil {
			debug.Error("Failed to check if task ready to complete: %v", err)
			return err
		}

		if ready {
			debug.Info("Task %s ready to complete after crack_batches_complete signal", message.TaskID)
			s.checkTaskCompletion(ctx, message.TaskID)
		} else {
			debug.Log("Task %s not ready to complete yet (waiting for more crack batches)", map[string]interface{}{
				"task_id": message.TaskID,
			})
		}
	} else {
		debug.Warning("Received crack_batches_complete for task not in processing status", map[string]interface{}{
			"task_id": message.TaskID,
			"status":  task.Status,
		})
	}

	return nil
}

// checkJobProcessingStatus checks if a job should transition to processing status
// This happens when the last task with no remaining work enters processing status
func (s *JobWebSocketIntegration) checkJobProcessingStatus(ctx context.Context, jobExecutionID uuid.UUID) {
	// Wrap sql.DB in custom DB type
	database := &db.DB{DB: s.db}
	jobExecRepo := repository.NewJobExecutionRepository(database)

	// Get job
	job, err := jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		debug.Error("Failed to get job for processing status check: %v", err)
		return
	}

	// Only transition if job is currently running
	if job.Status != models.JobExecutionStatusRunning {
		return
	}

	// Check if there's any remaining work
	jobsWithWork, err := jobExecRepo.GetJobsWithPendingWork(ctx)
	if err != nil {
		debug.Error("Failed to check jobs with pending work: %v", err)
		return
	}

	hasRemainingWork := false
	for _, j := range jobsWithWork {
		if j.ID == jobExecutionID {
			hasRemainingWork = true
			break
		}
	}

	// If no remaining work, check if there are processing tasks
	if !hasRemainingWork {
		processingTasks, err := s.jobTaskRepo.GetProcessingTasksForJob(ctx, jobExecutionID)
		if err != nil {
			debug.Error("Failed to get processing tasks: %v", err)
			return
		}

		if len(processingTasks) > 0 {
			// Job has no remaining work and has processing tasks - set job to processing
			err = jobExecRepo.SetJobProcessing(ctx, jobExecutionID)
			if err != nil {
				debug.Error("Failed to set job to processing status: %v", err)
			} else {
				debug.Info("Set job %s to processing status (no remaining work, %d tasks processing)",
					jobExecutionID, len(processingTasks))
			}
		}
	}
}

// checkTaskCompletion completes a task that has received all crack batches
func (s *JobWebSocketIntegration) checkTaskCompletion(ctx context.Context, taskID uuid.UUID) {
	// Get task
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		debug.Error("Failed to get task for completion: %v", err)
		return
	}

	// Mark task as complete
	err = s.jobTaskRepo.CompleteTask(ctx, taskID)
	if err != nil {
		debug.Error("Failed to mark task as complete: %v", err)
		return
	}

	debug.Log("Task completed after receiving all crack batches", map[string]interface{}{
		"task_id":         taskID,
		"expected_cracks": task.ExpectedCrackCount,
		"received_cracks": task.ReceivedCrackCount,
	})

	// Reset consecutive failure counters on success
	err = s.jobSchedulingService.HandleTaskSuccess(ctx, taskID)
	if err != nil {
		debug.Error("Failed to handle task success: %v", err)
	}

	// Handle task completion cleanup
	err = s.jobExecutionService.HandleTaskCompletion(ctx, taskID)
	if err != nil {
		debug.Error("Failed to handle task completion: %v", err)
	}

	// Check if job is complete
	err = s.jobSchedulingService.ProcessJobCompletion(ctx, task.JobExecutionID)
	if err != nil {
		debug.Error("Failed to process job completion: %v", err)
	}
}

// HandleBenchmarkResult processes benchmark results from agents
func (s *JobWebSocketIntegration) HandleBenchmarkResult(ctx context.Context, agentID int, result *wsservice.BenchmarkResultPayload) error {
	debug.Log("Processing benchmark result from agent", map[string]interface{}{
		"agent_id":    agentID,
		"hash_type":   result.HashType,
		"attack_mode": result.AttackMode,
		"speed":       result.Speed,
		"success":     result.Success,
	})

	if !result.Success {
		debug.Log("Benchmark failed", map[string]interface{}{
			"agent_id": agentID,
			"error":    result.Error,
		})
		return fmt.Errorf("benchmark failed: %s", result.Error)
	}

	// Get agent
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Store benchmark result
	benchmark := &models.AgentBenchmark{
		AgentID:    agent.ID,
		AttackMode: models.AttackMode(result.AttackMode),
		HashType:   result.HashType,
		Speed:      result.Speed,
	}

	err = s.benchmarkRepo.CreateOrUpdateAgentBenchmark(ctx, benchmark)
	if err != nil {
		return fmt.Errorf("failed to store benchmark result: %w", err)
	}

	debug.Log("Benchmark result stored successfully", map[string]interface{}{
		"agent_id":    agentID,
		"hash_type":   result.HashType,
		"attack_mode": result.AttackMode,
		"speed":       result.Speed,
	})

	// Update benchmark_requests table to mark this benchmark as complete
	_, err = s.db.ExecContext(ctx, `
		UPDATE benchmark_requests
		SET completed_at = CURRENT_TIMESTAMP,
			success = $1,
			error_message = $2
		WHERE agent_id = $3
		  AND attack_mode = $4
		  AND hash_type = $5
		  AND completed_at IS NULL
	`, result.Success, result.Error, agentID, result.AttackMode, result.HashType)

	if err != nil {
		debug.Warning("Failed to update benchmark_requests table: %v", err)
	} else {
		debug.Log("Updated benchmark_requests table for completion", map[string]interface{}{
			"agent_id":    agentID,
			"hash_type":   result.HashType,
			"attack_mode": result.AttackMode,
			"success":     result.Success,
		})
	}

	// Handle total effective keyspace from hashcat progress[1]
	if result.TotalEffectiveKeyspace > 0 {
		// Parse the ID from the result - could be a layer or a job
		if result.JobExecutionID == "" {
			debug.Error("Benchmark result from agent %d missing job_execution_id", agentID)
			return fmt.Errorf("benchmark result missing job_execution_id")
		}

		entityID, err := uuid.Parse(result.JobExecutionID)
		if err != nil {
			debug.Error("Failed to parse job_execution_id from benchmark result: %v", err)
			return fmt.Errorf("invalid job_execution_id in benchmark result: %w", err)
		}

		// First, try to interpret this as a LAYER ID
		layer, err := s.jobIncrementLayerRepo.GetByID(ctx, entityID)
		if err == nil && layer != nil {
			// This is a LAYER benchmark
			debug.Info("Benchmark result is for increment layer %s (mask: %s)", entityID, layer.Mask)

			// Update LAYER's effective keyspace using the specialized method
			err = s.jobIncrementLayerRepo.UpdateKeyspace(ctx, layer.ID, result.TotalEffectiveKeyspace, true)
			if err != nil {
				debug.Error("Failed to update layer keyspace: %v", err)
				return fmt.Errorf("failed to update layer keyspace: %w", err)
			}

			debug.Info("Layer %s (mask %s): Set accurate effective keyspace from hashcat: %d",
				layer.ID, layer.Mask, result.TotalEffectiveKeyspace)

			// Also set parent job's is_accurate_keyspace to true
			// This allows frontend to show "accurate" instead of "estimated" for increment mode jobs
			database := &db.DB{DB: s.db}
			jobExecRepo := repository.NewJobExecutionRepository(database)
			if err := jobExecRepo.SetIsAccurateKeyspace(ctx, layer.JobExecutionID, true); err != nil {
				debug.Warning("Failed to set job is_accurate_keyspace: %v", err)
			} else {
				debug.Info("Set job %s is_accurate_keyspace=true after layer benchmark", layer.JobExecutionID)
			}

			// Update metadata for forced benchmark completion
			if agent.Metadata != nil {
				if pendingJob, exists := agent.Metadata["pending_benchmark_job"]; exists && pendingJob == entityID.String() {
					agent.Metadata["forced_benchmark_completed_for_job"] = layer.JobExecutionID.String() // Use parent job ID
					delete(agent.Metadata, "pending_benchmark_job")
					delete(agent.Metadata, "benchmark_requested_at")

					err := s.agentRepo.Update(ctx, agent)
					if err != nil {
						debug.Warning("Failed to update agent metadata after layer benchmark: %v", err)
					} else {
						debug.Info("Agent %d completed forced benchmark for layer %s", agent.ID, layer.ID)
					}
				}
			}

			return nil // Layer benchmark handled, done
		}

		// Not a layer, treat as a regular JOB ID
		jobExec, err := s.jobExecutionService.GetJobExecutionByID(ctx, entityID)
		if err != nil || jobExec == nil {
			debug.Error("ID %s is neither a layer nor a job: %v", entityID, err)
			return fmt.Errorf("entity %s not found: %w", entityID, err)
		}

		debug.Info("Benchmark result is for job %s", entityID)

		// First benchmark for this job?
		if jobExec.EffectiveKeyspace == nil || !jobExec.IsAccurateKeyspace {
			// Set job-level effective keyspace from hashcat progress[1]
			jobExec.EffectiveKeyspace = &result.TotalEffectiveKeyspace
			jobExec.IsAccurateKeyspace = true

			// Calculate avg_rule_multiplier for future task estimates
			if jobExec.BaseKeyspace != nil && *jobExec.BaseKeyspace > 0 && jobExec.MultiplicationFactor > 0 {
				multiplier := float64(result.TotalEffectiveKeyspace) /
					float64(*jobExec.BaseKeyspace) /
					float64(jobExec.MultiplicationFactor)
				jobExec.AvgRuleMultiplier = &multiplier

				debug.Info("Job %s: Set accurate effective keyspace from hashcat: %d (avg_rule_multiplier: %.5f)",
					jobExec.ID, result.TotalEffectiveKeyspace, multiplier)
			} else {
				debug.Info("Job %s: Set accurate effective keyspace from hashcat: %d",
					jobExec.ID, result.TotalEffectiveKeyspace)
			}

			// Update job in database
			if err := s.jobExecutionService.UpdateKeyspaceInfo(ctx, jobExec); err != nil {
				debug.Error("Failed to update job keyspace info: %v", err)
				return fmt.Errorf("failed to update job keyspace info: %w", err)
			}
		} else {
			// Subsequent benchmark - validate consistency (should match job total)
			diff := result.TotalEffectiveKeyspace - *jobExec.EffectiveKeyspace
			if diff < 0 {
				diff = -diff // abs value
			}
			threshold := *jobExec.EffectiveKeyspace / 1000 // 0.1%

			if diff > threshold {
				debug.Warning("Agent %d benchmark differs from job total: observed=%d, expected=%d, diff=%d",
					agentID, result.TotalEffectiveKeyspace, *jobExec.EffectiveKeyspace, diff)
			} else {
				debug.Info("Agent %d benchmark validates job effective keyspace (diff=%d)", agentID, diff)
			}
		}

		// Update metadata for forced benchmark completion
		// This allows the scheduler to prioritize this agent for the job's first task
		if agent.Metadata != nil {
			if pendingJob, exists := agent.Metadata["pending_benchmark_job"]; exists && pendingJob == jobExec.ID.String() {
				// This was a forced benchmark - set completion flag for prioritization
				agent.Metadata["forced_benchmark_completed_for_job"] = jobExec.ID.String()
				delete(agent.Metadata, "pending_benchmark_job")
				delete(agent.Metadata, "benchmark_requested_at")

				err := s.agentRepo.Update(ctx, agent)
				if err != nil {
					debug.Warning("Failed to update agent metadata after forced benchmark: %v", err)
				} else {
					debug.Info("Agent %d completed forced benchmark for job %s, set priority flag", agent.ID, jobExec.ID)
				}
			}
		}

		// Also clear pending benchmark metadata from any other agents waiting for this job
		agents, err := s.agentRepo.List(ctx, nil)
		if err == nil {
			for i := range agents {
				otherAgent := &agents[i]
				if otherAgent.ID != agentID && otherAgent.Metadata != nil {
					if pendingJob, exists := otherAgent.Metadata["pending_benchmark_job"]; exists && pendingJob == jobExec.ID.String() {
						delete(otherAgent.Metadata, "pending_benchmark_job")
						delete(otherAgent.Metadata, "benchmark_requested_at")
						err := s.agentRepo.Update(ctx, otherAgent)
						if err != nil {
							debug.Warning("Failed to clear benchmark metadata for agent %d: %v", otherAgent.ID, err)
						} else {
							debug.Info("Cleared pending benchmark metadata for agent %d after job %s benchmark completed", otherAgent.ID, jobExec.ID)
						}
					}
				}
			}
		}
	}

	return nil
}

// processCrackedHashes processes cracked hashes from a job progress update
func (s *JobWebSocketIntegration) processCrackedHashes(ctx context.Context, taskID uuid.UUID, crackedHashes []models.CrackedHash) error {
	// Get task details
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Get job execution details
	jobExecution, err := s.jobExecutionService.GetJobExecutionByID(ctx, task.JobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	var crackedCount int
	crackedAt := time.Now()

	// OPTIMIZATION: Do ONE bulk lookup for all hash values instead of individual queries
	hashValues := make([]string, len(crackedHashes))
	for i, crackedEntry := range crackedHashes {
		hashValues[i] = crackedEntry.Hash
	}

	// Check if this is an LM hash job (hash_type = 3000)
	// LM hashes are split into two 16-char halves by hashcat, so we need special handling
	isLMHash := jobExecution.HashType == 3000

	// Bulk lookup all hashes in one query
	var allHashes []*models.Hash
	var lmHashMatches map[string][]*repository.LMHashMatch

	if isLMHash {
		// For LM hashes, use partial matching (16-char halves)
		lmHashMatches, err = s.hashRepo.GetByHashValuesLMPartial(ctx, hashValues)
		if err != nil {
			return fmt.Errorf("failed to bulk lookup LM hashes: %w", err)
		}
		debug.Info("LM hash lookup found %d matches for %d half-hashes", len(lmHashMatches), len(hashValues))
	} else {
		// For other hash types, use exact matching
		allHashes, err = s.hashRepo.GetByHashValues(ctx, hashValues)
		if err != nil {
			return fmt.Errorf("failed to bulk lookup hashes: %w", err)
		}
	}

	// Create a map for quick lookup: hash_value -> []*models.Hash
	hashMap := make(map[string][]*models.Hash)
	if !isLMHash {
		for _, hash := range allHashes {
			hashMap[hash.HashValue] = append(hashMap[hash.HashValue], hash)
		}
	}

	// Process hashes in mini-batches with separate transactions to avoid connection leaks
	// Larger batch size (20000) reduces transaction count and lock contention
	// For 1.75M cracks: 20000/batch = 88 transactions vs 5000/batch = 350 transactions
	const batchSize = 20000
	var tx *sql.Tx
	var txHashCount int
	var hashUpdateBatch []repository.HashUpdate

	// Batch potfile staging inserts to reduce database overhead
	// For 1.75M cracks, this reduces 1.75M inserts to ~175 batch inserts (10k each)
	const potfileBatchSize = 10000
	var potfileBatch []services.PotfileStagingEntry

	// Pre-load potfile settings ONCE instead of querying for every crack
	// This eliminates millions of redundant database queries (N+1 problem)
	var shouldStagePotfile bool

	if s.potfileService != nil && s.systemSettingsRepo != nil && s.hashlistRepo != nil && s.clientRepo != nil {
		// Check if potfile is globally enabled
		potfileSetting, err := s.systemSettingsRepo.GetSetting(ctx, "potfile_enabled")
		if err == nil && potfileSetting != nil && potfileSetting.Value != nil && *potfileSetting.Value == "true" {
			// Get hashlist ONCE to check exclusions
			hashlist, err := s.hashlistRepo.GetByID(ctx, jobExecution.HashlistID)
			if err != nil {
				debug.Warning("Failed to get hashlist for potfile check: %v", err)
				shouldStagePotfile = false
			} else {
				// Check if client has potfile excluded
				clientExcluded := false
				if hashlist.ClientID != uuid.Nil {
					clientExcluded, err = s.clientRepo.IsExcludedFromPotfile(ctx, hashlist.ClientID)
					if err != nil {
						debug.Warning("Failed to check client potfile exclusion: %v", err)
					}
				}

				if clientExcluded {
					debug.Info("Client %s is excluded from potfile", hashlist.ClientID)
					shouldStagePotfile = false
				} else {
					// Check if hashlist is excluded
					hashlistExcluded, err := s.hashlistRepo.IsExcludedFromPotfile(ctx, jobExecution.HashlistID)
					if err != nil {
						debug.Warning("Failed to check hashlist potfile exclusion: %v", err)
						shouldStagePotfile = false
					} else {
						shouldStagePotfile = !hashlistExcluded
						if shouldStagePotfile {
							debug.Info("Potfile staging enabled for hashlist %d", jobExecution.HashlistID)
						}
					}
				}
			}
		}
	}

	// Helper to commit current transaction
	commitTx := func() error {
		if tx != nil {
			if err := tx.Commit(); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to commit transaction: %w", err)
			}
			tx = nil
			txHashCount = 0
		}
		return nil
	}

	// Ensure final commit/rollback
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	// Track which hashlists are affected by these cracks and by how many
	// Key: hashlist ID, Value: count of newly cracked hashes
	affectedHashlists := make(map[int64]int)

	// Process LM hashes differently (partial crack tracking)
	if isLMHash {
		// Pre-load LM metadata for all matched hashes
		var hashIDs []uuid.UUID
		for _, matches := range lmHashMatches {
			for _, match := range matches {
				hashIDs = append(hashIDs, match.Hash.ID)
			}
		}

		lmMetadataMap, err := s.lmHashRepo.GetLMMetadataByHashes(ctx, hashIDs)
		if err != nil {
			return fmt.Errorf("failed to get LM metadata: %w", err)
		}

		// Process each LM half-hash crack
		for _, crackedEntry := range crackedHashes {
			halfHash := crackedEntry.Hash  // This is a 16-char half
			password := crackedEntry.Plain

			matches, found := lmHashMatches[halfHash]
			if !found || len(matches) == 0 {
				debug.Warning("LM half-hash not found: %s", halfHash)
				continue
			}

			for _, match := range matches {
				hash := match.Hash
				halfPosition := match.MatchedHalf  // "first" or "second"

				// Get or create LM metadata
				metadata := lmMetadataMap[hash.ID]
				if metadata == nil {
					// Create metadata entry
					if err := s.lmHashRepo.CreateLMMetadata(ctx, hash.ID); err != nil {
						debug.Error("Failed to create LM metadata for hash %s: %v", hash.ID, err)
						continue
					}
					metadata = &models.LMHashMetadata{HashID: hash.ID}
					lmMetadataMap[hash.ID] = metadata
				}

				// Check if this half already cracked
				if (halfPosition == "first" && metadata.FirstHalfCracked) ||
				   (halfPosition == "second" && metadata.SecondHalfCracked) {
					debug.Info("LM %s half already cracked, skipping [hash_id=%s]", halfPosition, hash.ID)
					continue
				}

				// Start transaction if needed
				if tx == nil {
					tx, err = s.db.Begin()
					if err != nil {
						return fmt.Errorf("failed to start transaction: %w", err)
					}
					txHashCount = 0
				}

				// Update this half as cracked
				err = s.lmHashRepo.UpdateLMHalfCrack(ctx, tx, hash.ID, halfPosition, password)
				if err != nil {
					debug.Error("Failed to update LM half crack: %v", err)
					continue
				}

				debug.Info("LM %s half cracked [hash_id=%s, password='%s']", halfPosition, hash.ID, password)

				// Update metadata in our map
				if halfPosition == "first" {
					metadata.FirstHalfCracked = true
					metadata.FirstHalfPassword = sql.NullString{String: password, Valid: true}

					// Auto-complete blank second half (the constant aad3b435b51404ee is not an encrypted value)
					secondHalf := strings.ToLower(hash.HashValue[16:32])
					if secondHalf == "aad3b435b51404ee" {
						debug.Info("Auto-completing blank LM second half [hash_id=%s]", hash.ID)
						err = s.lmHashRepo.UpdateLMHalfCrack(ctx, tx, hash.ID, "second", "")
						if err != nil {
							debug.Error("Failed to auto-complete blank second half: %v", err)
						} else {
							metadata.SecondHalfCracked = true
							metadata.SecondHalfPassword = sql.NullString{String: "", Valid: true}
						}
					}
				} else {
					metadata.SecondHalfCracked = true
					metadata.SecondHalfPassword = sql.NullString{String: password, Valid: true}

					// Auto-complete blank first half if applicable
					firstHalf := strings.ToLower(hash.HashValue[0:16])
					if firstHalf == "aad3b435b51404ee" && !metadata.FirstHalfCracked {
						debug.Info("Auto-completing blank LM first half [hash_id=%s]", hash.ID)
						err = s.lmHashRepo.UpdateLMHalfCrack(ctx, tx, hash.ID, "first", "")
						if err != nil {
							debug.Error("Failed to auto-complete blank first half: %v", err)
						} else {
							metadata.FirstHalfCracked = true
							metadata.FirstHalfPassword = sql.NullString{String: "", Valid: true}
						}
					}
				}

				// Check if both halves are now cracked
				wasFinalized, fullPassword, err := s.lmHashRepo.CheckAndFinalizeLMCrack(ctx, tx, hash.ID)
				if err != nil {
					debug.Error("Failed to check LM finalization: %v", err)
					continue
				}

				if wasFinalized {
					// Both halves cracked - mark main hash as cracked
					hashUpdateBatch = append(hashUpdateBatch, repository.HashUpdate{
						HashID:    hash.ID,
						Password:  fullPassword,
						Username:  nil,
						CrackedAt: crackedAt,
					})

					crackedCount++
					txHashCount++

					debug.Info("LM FULLY CRACKED [hash_id=%s, full_password='%s']", hash.ID, fullPassword)

					// Query which hashlists contain this hash and increment their counters
					hashlistIDs, err := s.hashRepo.GetHashlistIDsForHash(ctx, hash.ID)
					if err != nil {
						debug.Warning("Failed to get hashlist IDs for hash %s: %v", hash.ID, err)
					} else {
						for _, hashlistID := range hashlistIDs {
							affectedHashlists[hashlistID]++
						}
					}

					// Stage for potfile if enabled
					if shouldStagePotfile {
						potfileBatch = append(potfileBatch, services.PotfileStagingEntry{
							Password:  fullPassword,
							HashValue: hash.HashValue,
						})
					}
				}

				// Execute batched updates and commit when batch is full
				if txHashCount >= batchSize {
					// Execute the batched updates in one query
					if len(hashUpdateBatch) > 0 {
						rowsAffected, err := s.hashRepo.UpdateCrackStatusBatch(tx, hashUpdateBatch)
						if err != nil {
							return fmt.Errorf("failed to batch update hashes: %w", err)
						}
						debug.Info("Batch updated %d LM hashes out of %d queued", rowsAffected, len(hashUpdateBatch))
						hashUpdateBatch = nil
					}

					if err := commitTx(); err != nil {
						return err
					}
				}
			}
		}
	} else {
		// NON-LM HASH PROCESSING (original logic)
		// Process each cracked hash
		for _, crackedEntry := range crackedHashes {
			hashValue := crackedEntry.Hash
			password := crackedEntry.Plain
			crackPos := crackedEntry.CrackPos

			// Lookup from our pre-loaded map instead of querying database
			hashes, found := hashMap[hashValue]
			if !found || len(hashes) == 0 {
				debug.Log("Hash not found in hashlist", map[string]interface{}{
					"hash_value":  hashValue,
					"hashlist_id": jobExecution.HashlistID,
				})
				continue
			}

			// Update ALL hashes with this hash_value (e.g., multiple users with same password)
			// This ensures that Administrator, Administrator1, Administrator2 all get marked as cracked
			for _, hash := range hashes {
			// Check if hash is already cracked to prevent double counting
			if hash.IsCracked {
				debug.Warning("Skipping already-cracked hash in crack batch [hash_id=%s, hash_value=%s, current_password=%s, new_password=%s, last_updated=%v, hashlist_id=%d]",
					hash.ID, hashValue, hash.Password, password, hash.LastUpdated, jobExecution.HashlistID)
				continue
			}

			// Start new transaction if needed
			if tx == nil {
				tx, err = s.db.Begin()
				if err != nil {
					return fmt.Errorf("failed to start transaction: %w", err)
				}
				txHashCount = 0
			}

			// Collect hash update for batch processing
			hashUpdateBatch = append(hashUpdateBatch, repository.HashUpdate{
				HashID:    hash.ID,
				Password:  password,
				Username:  nil,
				CrackedAt: crackedAt,
			})

			crackedCount++
			txHashCount++

			// Query which hashlists contain this hash and increment their counters
			hashlistIDs, err := s.hashRepo.GetHashlistIDsForHash(ctx, hash.ID)
			if err != nil {
				debug.Warning("Failed to get hashlist IDs for hash %s: %v", hash.ID, err)
			} else {
				for _, hashlistID := range hashlistIDs {
					affectedHashlists[hashlistID]++
				}
			}

			debug.Log("Queued hash for batch update", map[string]interface{}{
				"hash_id":     hash.ID,
				"hash_value":  hashValue,
				"username":    hash.Username,
				"hashlist_id": jobExecution.HashlistID,
				"crack_pos":   crackPos,
				"password":    password,
			})

			// Check if this NTLM hash has a linked LM hash and propagate crack
			if jobExecution.HashType == 1000 { // NTLM
				linkedLMHash, err := s.hashRepo.GetLinkedHash(ctx, hash.ID, "lm_ntlm")
				if err != nil {
					debug.Warning("Failed to check for linked LM hash: %v", err)
				} else if linkedLMHash != nil && linkedLMHash.HashTypeID == 3000 {
					// Check if LM hash is already cracked
					if !linkedLMHash.IsCracked {
						// Uppercase the NTLM password for LM
						lmPassword := strings.ToUpper(password)

						debug.Info("Propagating crack from NTLM hash %s to linked LM hash %s (password: %s -> %s)",
							hash.ID, linkedLMHash.ID, password, lmPassword)

						// Add LM hash to update batch
						hashUpdateBatch = append(hashUpdateBatch, repository.HashUpdate{
							HashID:    linkedLMHash.ID,
							Password:  lmPassword,
							Username:  nil,
							CrackedAt: crackedAt,
						})
						txHashCount++

						// Track affected hashlists for the linked LM hash
						lmHashlistIDs, err := s.hashRepo.GetHashlistIDsForHash(ctx, linkedLMHash.ID)
						if err != nil {
							debug.Warning("Failed to get hashlist IDs for linked LM hash %s: %v", linkedLMHash.ID, err)
						} else {
							for _, hashlistID := range lmHashlistIDs {
								affectedHashlists[hashlistID]++
							}
						}
					} else {
						debug.Debug("Linked LM hash %s is already cracked, skipping propagation", linkedLMHash.ID)
					}
				}
			}

			// Execute batched updates and commit when batch is full
			if txHashCount >= batchSize {
				// Execute the batched updates in one query
				rowsAffected, err := s.hashRepo.UpdateCrackStatusBatch(tx, hashUpdateBatch)
				if err != nil {
					return fmt.Errorf("failed to batch update hashes: %w", err)
				}
				debug.Info("Batch updated %d hashes out of %d queued", rowsAffected, len(hashUpdateBatch))

				// Critical validation: detect if we lost updates
				if rowsAffected != int64(len(hashUpdateBatch)) {
					debug.Error("CRITICAL: Batch UPDATE mismatch! Queued %d but updated only %d (LOST %d updates)",
						len(hashUpdateBatch), rowsAffected, len(hashUpdateBatch)-int(rowsAffected))
				}

				hashUpdateBatch = nil // Reset batch

				if err := commitTx(); err != nil {
					return err
				}
			}
		}

			// Stage password for pot-file (batched, done once per unique hash value)
			// All checks pre-loaded before loop to avoid millions of redundant queries
			if shouldStagePotfile {
				potfileBatch = append(potfileBatch, services.PotfileStagingEntry{
					Password:  password,
					HashValue: hashValue,
				})

				// Flush batch when it reaches size limit
				if len(potfileBatch) >= potfileBatchSize {
					if err := s.potfileService.StageBatch(ctx, potfileBatch); err != nil {
						debug.Warning("Failed to stage password batch for pot-file: %v", err)
					} else {
						debug.Info("Successfully staged %d passwords for pot-file", len(potfileBatch))
					}
					potfileBatch = nil // Reset batch
				}
			}
		}
	} // End of else block for non-LM processing

	// Flush any remaining hash updates before committing
	if len(hashUpdateBatch) > 0 && tx != nil {
		rowsAffected, err := s.hashRepo.UpdateCrackStatusBatch(tx, hashUpdateBatch)
		if err != nil {
			return fmt.Errorf("failed to batch update final hashes: %w", err)
		}
		debug.Info("Final batch updated %d hashes out of %d queued", rowsAffected, len(hashUpdateBatch))

		// Critical validation: detect if we lost updates
		if rowsAffected != int64(len(hashUpdateBatch)) {
			debug.Error("CRITICAL: Final batch UPDATE mismatch! Queued %d but updated only %d (LOST %d updates)",
				len(hashUpdateBatch), rowsAffected, len(hashUpdateBatch)-int(rowsAffected))
		}

		hashUpdateBatch = nil
	}

	// Flush any remaining potfile staging entries
	if len(potfileBatch) > 0 {
		if err := s.potfileService.StageBatch(ctx, potfileBatch); err != nil {
			debug.Warning("Failed to stage final password batch for pot-file: %v", err)
		} else {
			debug.Info("Successfully staged final batch of %d passwords for pot-file", len(potfileBatch))
		}
	}

	// Commit any remaining updates
	if err := commitTx(); err != nil {
		return err
	}

	// Update cracked count for ALL affected hashlists AFTER all hashes are processed
	// This ensures that if a hash appears in multiple hashlists, all of them get updated
	// For example, if 2 cracked hashes belong to hashlists [98, 98, 99, 100]:
	//   - Hashlist 98 increments by 2
	//   - Hashlist 99 increments by 1
	//   - Hashlist 100 increments by 1
	if len(affectedHashlists) > 0 {
		debug.Info("Updating cracked counts for %d affected hashlists", len(affectedHashlists))
		for hashlistID, count := range affectedHashlists {
			debug.Info("Incrementing hashlist %d cracked count by %d", hashlistID, count)
			err = s.hashlistRepo.IncrementCrackedCount(ctx, hashlistID, count)
			if err != nil {
				debug.Error("Failed to update hashlist cracked count for hashlist %d: %v",
					hashlistID, err)
				// Don't fail the entire batch if counter update fails
			}
		}
	} else if crackedCount > 0 {
		// This should not happen - if we cracked hashes, they should belong to at least one hashlist
		debug.Warning("Cracked %d hashes but no affected hashlists found - this indicates a data integrity issue", crackedCount)
	}

	// Update job task crack count (still use total crackedCount for the task)
	if crackedCount > 0 {
		err = s.jobTaskRepo.UpdateCrackCount(ctx, taskID, crackedCount)
		if err != nil {
			debug.Error("Failed to update job task crack count for task %s: %v",
				taskID, err)
			// Don't fail the entire batch if counter update fails
		}
	} else {
		debug.Debug("No new cracks to update counters for task %s", taskID)
	}

	return nil
}

// GetTaskProgress returns the current progress for a task
func (s *JobWebSocketIntegration) GetTaskProgress(taskID string) *models.JobProgress {
	s.progressMutex.RLock()
	defer s.progressMutex.RUnlock()

	return s.taskProgressMap[taskID]
}

// StartScheduledJobAssignment starts the process of assigning scheduled jobs to agents
func (s *JobWebSocketIntegration) StartScheduledJobAssignment(ctx context.Context) {
	// This would be called when the scheduling service assigns a task to an agent
	// The scheduling service would call SendJobAssignment for each assigned task
	debug.Log("Job assignment integration service started", nil)
}

// RecoverTask attempts to recover a task that was in reconnect_pending state
func (s *JobWebSocketIntegration) RecoverTask(ctx context.Context, taskID string, agentID int, keyspaceProcessed int64) error {
	debug.Log("Attempting to recover task", map[string]interface{}{
		"task_id":            taskID,
		"agent_id":           agentID,
		"keyspace_processed": keyspaceProcessed,
	})
	
	// Parse task ID as UUID
	taskUUID, err := uuid.Parse(taskID)
	if err != nil {
		return fmt.Errorf("invalid task ID format: %w", err)
	}
	
	// Get the task from database
	task, err := s.jobTaskRepo.GetByID(ctx, taskUUID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	
	// Check task status and handle recovery appropriately
	switch task.Status {
	case models.JobTaskStatusRunning:
		// Task is already running, no recovery needed
		debug.Log("Task already running, no recovery needed", map[string]interface{}{
			"task_id": taskID,
			"status":  task.Status,
		})
		return nil
		
	case models.JobTaskStatusCompleted:
		// Task is already completed, agent shouldn't be running it
		debug.Log("Task already completed, agent should stop", map[string]interface{}{
			"task_id": taskID,
			"status":  task.Status,
		})
		// Return an error to trigger job_stop on the agent
		return fmt.Errorf("task %s is already completed", taskID)
		
	case models.JobTaskStatusReconnectPending, models.JobTaskStatusPending:
		// These states can be recovered
		debug.Log("Task can be recovered", map[string]interface{}{
			"task_id": taskID,
			"status":  task.Status,
		})
		// Continue with recovery below
		
	case models.JobTaskStatusFailed:
		// Check if task can be retried
		maxRetries := 3 // Get from settings
		if task.RetryCount < maxRetries {
			debug.Log("Failed task can be retried", map[string]interface{}{
				"task_id":     taskID,
				"status":      task.Status,
				"retry_count": task.RetryCount,
				"max_retries": maxRetries,
			})
			// Continue with recovery below
		} else {
			return fmt.Errorf("task %s has exceeded maximum retries (%d)", taskID, maxRetries)
		}
		
	default:
		// Other states (cancelled, etc.) cannot be recovered
		return fmt.Errorf("task %s cannot be recovered from state: %s", taskID, task.Status)
	}
	
	// Update task status back to running and reassign to the agent
	err = s.jobTaskRepo.UpdateStatus(ctx, taskUUID, models.JobTaskStatusRunning)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	
	// Update task assignment to the reconnected agent
	task.AgentID = &agentID
	task.Status = models.JobTaskStatusRunning
	task.DetailedStatus = "running" // Ensure detailed_status matches the status for constraint
	if keyspaceProcessed > 0 {
		task.KeyspaceProcessed = keyspaceProcessed
	}
	
	err = s.jobTaskRepo.Update(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to update task assignment: %w", err)
	}
	
	debug.Log("Successfully recovered task", map[string]interface{}{
		"task_id":  taskID,
		"agent_id": agentID,
		"job_id":   task.JobExecutionID,
	})
	
	// Ensure the job remains in running state
	// Wrap sql.DB in custom DB type
	database := &db.DB{DB: s.db}
	jobExecRepo := repository.NewJobExecutionRepository(database)
	err = jobExecRepo.UpdateStatus(ctx, task.JobExecutionID, models.JobExecutionStatusRunning)
	if err != nil {
		// Log but don't fail - task recovery is more important
		debug.Log("Failed to update job status during task recovery", map[string]interface{}{
			"job_id": task.JobExecutionID,
			"error":  err.Error(),
		})
	}
	
	return nil
}

// HandleAgentDisconnection marks tasks as reconnect_pending when an agent disconnects
func (s *JobWebSocketIntegration) HandleAgentDisconnection(ctx context.Context, agentID int) error {
	debug.Log("Handling agent disconnection", map[string]interface{}{
		"agent_id": agentID,
	})
	
	// Find all running or assigned tasks for this agent
	// Wrap sql.DB in custom DB type
	database := &db.DB{DB: s.db}
	taskRepo := repository.NewJobTaskRepository(database)
	
	// Get task IDs that are currently running or assigned to this agent
	taskIDs, err := taskRepo.GetTasksByAgentAndStatus(ctx, agentID, models.JobTaskStatusRunning)
	if err != nil {
		debug.Log("Failed to get running tasks for disconnected agent", map[string]interface{}{
			"agent_id": agentID,
			"error":    err.Error(),
		})
	}
	
	// Also get assigned tasks
	assignedTaskIDs, err := taskRepo.GetTasksByAgentAndStatus(ctx, agentID, models.JobTaskStatusAssigned)
	if err != nil {
		debug.Log("Failed to get assigned tasks for disconnected agent", map[string]interface{}{
			"agent_id": agentID,
			"error":    err.Error(),
		})
	}
	
	// Combine both lists
	if assignedTaskIDs != nil {
		taskIDs = append(taskIDs, assignedTaskIDs...)
	}
	
	// Get full task objects and mark each as reconnect_pending
	var tasks []models.JobTask
	for _, taskID := range taskIDs {
		// Get the full task object
		task, err := taskRepo.GetByID(ctx, taskID)
		if err != nil || task == nil {
			debug.Log("Failed to get task details", map[string]interface{}{
				"task_id": taskID,
				"error":   err,
			})
			continue
		}
		
		debug.Log("Marking task as reconnect_pending due to agent disconnection", map[string]interface{}{
			"task_id":  taskID,
			"agent_id": agentID,
			"job_id":   task.JobExecutionID,
		})
		
		// Update task status to reconnect_pending
		err = taskRepo.UpdateStatus(ctx, taskID, models.JobTaskStatusReconnectPending)
		if err != nil {
			debug.Log("Failed to mark task as reconnect_pending", map[string]interface{}{
				"task_id": taskID,
				"error":   err.Error(),
			})
			continue
		}
		
		// Clear the agent_id from the task so it can be reassigned
		task.AgentID = nil
		task.Status = models.JobTaskStatusReconnectPending
		err = taskRepo.Update(ctx, task)
		if err != nil {
			debug.Log("Failed to clear agent_id from task", map[string]interface{}{
				"task_id": taskID,
				"error":   err.Error(),
			})
		}
		
		tasks = append(tasks, *task)
	}
	
	if len(tasks) > 0 {
		debug.Log("Successfully marked tasks as reconnect_pending", map[string]interface{}{
			"agent_id":    agentID,
			"task_count":  len(tasks),
		})
		
		// Start a timer to handle grace period expiration (2 minutes)
		go s.handleReconnectGracePeriod(ctx, tasks, agentID)
	}
	
	return nil
}

// HandleAgentReconnectionWithNoTask handles when an agent reconnects but reports no running task
// It finds all reconnect_pending tasks assigned to this agent and resets them for retry
func (s *JobWebSocketIntegration) HandleAgentReconnectionWithNoTask(ctx context.Context, agentID int) (int, error) {
	debug.Log("Handling agent reconnection with no running task", map[string]interface{}{
		"agent_id": agentID,
	})
	
	// Get all reconnect_pending tasks for this agent
	reconnectTasks, err := s.jobTaskRepo.GetReconnectPendingTasksByAgent(ctx, agentID)
	if err != nil {
		debug.Log("Failed to get reconnect_pending tasks for agent", map[string]interface{}{
			"agent_id": agentID,
			"error":    err.Error(),
		})
		return 0, fmt.Errorf("failed to get reconnect_pending tasks: %w", err)
	}
	
	if len(reconnectTasks) == 0 {
		debug.Log("No reconnect_pending tasks found for agent", map[string]interface{}{
			"agent_id": agentID,
		})
		return 0, nil
	}
	
	debug.Log("Found reconnect_pending tasks to reset", map[string]interface{}{
		"agent_id":   agentID,
		"task_count": len(reconnectTasks),
	})
	
	// Get max retry attempts from settings
	maxRetries := 3
	retrySetting, err := s.systemSettingsRepo.GetSetting(ctx, "max_chunk_retry_attempts")
	if err == nil && retrySetting.Value != nil {
		if retries, err := strconv.Atoi(*retrySetting.Value); err == nil {
			maxRetries = retries
		}
	}
	
	resetCount := 0
	failedCount := 0
	
	for _, task := range reconnectTasks {
		// Check if task can be retried
		if task.RetryCount < maxRetries {
			// Reset task for retry
			err := s.jobTaskRepo.ResetTaskForRetry(ctx, task.ID)
			if err != nil {
				debug.Log("Failed to reset task for retry", map[string]interface{}{
					"task_id":  task.ID,
					"agent_id": agentID,
					"error":    err.Error(),
				})
				continue
			}
			
			debug.Log("Task reset for retry after agent reconnection", map[string]interface{}{
				"task_id":      task.ID,
				"agent_id":     agentID,
				"retry_count":  task.RetryCount + 1,
				"max_retries":  maxRetries,
			})
			resetCount++
		} else {
			// Mark as permanently failed after all retries exhausted
			errorMsg := fmt.Sprintf("Agent %d reconnected without task after %d retry attempts", agentID, task.RetryCount)
			err := s.jobTaskRepo.MarkTaskFailedPermanently(ctx, task.ID, errorMsg)
			if err != nil {
				debug.Log("Failed to mark task as permanently failed", map[string]interface{}{
					"task_id":  task.ID,
					"agent_id": agentID,
					"error":    err.Error(),
				})
				continue
			}

			debug.Log("Task permanently failed after max retries", map[string]interface{}{
				"task_id":     task.ID,
				"agent_id":    agentID,
				"retry_count": task.RetryCount,
			})
			failedCount++
		}
	}
	
	debug.Log("Completed processing reconnect_pending tasks for agent", map[string]interface{}{
		"agent_id":     agentID,
		"total_tasks":  len(reconnectTasks),
		"reset_count":  resetCount,
		"failed_count": failedCount,
	})
	
	// Check if affected jobs need status update
	jobIDs := make(map[uuid.UUID]bool)
	for _, task := range reconnectTasks {
		jobIDs[task.JobExecutionID] = true
	}
	
	for jobID := range jobIDs {
		// Check if any tasks are still active for this job
		allTasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
		if err != nil {
			debug.Log("Failed to check job tasks", map[string]interface{}{
				"job_id": jobID,
				"error":  err.Error(),
			})
			continue
		}
		
		hasActiveTasks := false
		for _, task := range allTasks {
			if task.Status == models.JobTaskStatusRunning || 
			   task.Status == models.JobTaskStatusReconnectPending ||
			   task.Status == models.JobTaskStatusAssigned {
				hasActiveTasks = true
				break
			}
		}
		
		// If no active tasks remain and we have pending tasks, ensure job is in pending state
		if !hasActiveTasks {
			hasPendingTasks := false
			for _, task := range allTasks {
				if task.Status == models.JobTaskStatusPending {
					hasPendingTasks = true
					break
				}
			}
			
			if hasPendingTasks {
				// Ensure job is in pending state for rescheduling
				// Use jobExecutionRepo from the service
				database := &db.DB{DB: s.db}
				jobExecutionRepo := repository.NewJobExecutionRepository(database)
				err := jobExecutionRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending)
				if err != nil {
					debug.Log("Failed to update job status to pending", map[string]interface{}{
						"job_id": jobID,
						"error":  err.Error(),
					})
				} else {
					debug.Log("Job marked as pending for rescheduling", map[string]interface{}{
						"job_id": jobID,
					})
				}
			}
		}
	}
	
	return resetCount, nil
}

// handleReconnectGracePeriod waits for the grace period and then marks tasks as failed if not recovered
func (s *JobWebSocketIntegration) handleReconnectGracePeriod(ctx context.Context, tasks []models.JobTask, agentID int) {
	gracePeriod := 2 * time.Minute
	debug.Log("Starting reconnect grace period timer", map[string]interface{}{
		"agent_id":      agentID,
		"task_count":    len(tasks),
		"grace_period":  gracePeriod.String(),
	})
	
	time.Sleep(gracePeriod)
	
	debug.Log("Grace period expired, checking tasks", map[string]interface{}{
		"agent_id": agentID,
	})
	
	// Wrap sql.DB in custom DB type
	database := &db.DB{DB: s.db}
	taskRepo := repository.NewJobTaskRepository(database)
	
	for _, task := range tasks {
		// Check if task is still in reconnect_pending state
		currentTask, err := taskRepo.GetByID(ctx, task.ID)
		if err != nil {
			debug.Log("Failed to get task status after grace period", map[string]interface{}{
				"task_id": task.ID,
				"error":   err.Error(),
			})
			continue
		}
		
		if currentTask != nil && currentTask.Status == models.JobTaskStatusReconnectPending {
			debug.Log("Task still in reconnect_pending after grace period, marking as pending for reassignment", map[string]interface{}{
				"task_id": task.ID,
			})
			
			// Mark task as pending so it can be reassigned to another agent
			err = taskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusPending)
			if err != nil {
				debug.Log("Failed to mark task as pending after grace period", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
			}
		}
	}
}

// recalculateSubsequentChunks updates start/end positions for all chunks after completedChunkNumber
// This ensures the chain is self-correcting when actual keyspace sizes are received
func (s *JobWebSocketIntegration) recalculateSubsequentChunks(ctx context.Context, jobExecutionID uuid.UUID, completedChunkNumber int) error {
	// Get all tasks for this job ordered by chunk number
	query := `
		SELECT id, chunk_number, chunk_actual_keyspace,
		       effective_keyspace_start, effective_keyspace_end
		FROM job_tasks
		WHERE job_execution_id = $1
		ORDER BY chunk_number ASC`

	rows, err := s.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get tasks: %w", err)
	}
	defer rows.Close()

	type taskInfo struct {
		id                     uuid.UUID
		chunkNumber            int
		chunkActualKeyspace    *int64
		effectiveKeyspaceStart *int64
		effectiveKeyspaceEnd   *int64
	}

	var tasks []taskInfo
	for rows.Next() {
		var t taskInfo
		if err := rows.Scan(&t.id, &t.chunkNumber, &t.chunkActualKeyspace,
			&t.effectiveKeyspaceStart, &t.effectiveKeyspaceEnd); err != nil {
			return fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}

	// Calculate cumulative positions
	cumulativeEnd := int64(0)
	needsUpdate := false

	for _, t := range tasks {
		expectedStart := cumulativeEnd

		// Calculate expected end based on actual or estimated chunk size
		var expectedEnd int64
		if t.chunkActualKeyspace != nil {
			// Use actual chunk size
			expectedEnd = expectedStart + *t.chunkActualKeyspace
			cumulativeEnd = expectedEnd
		} else {
			// Use estimated chunk size
			if t.effectiveKeyspaceStart != nil && t.effectiveKeyspaceEnd != nil {
				chunkSize := *t.effectiveKeyspaceEnd - *t.effectiveKeyspaceStart
				expectedEnd = expectedStart + chunkSize
				cumulativeEnd = expectedEnd
			} else {
				// Can't calculate without start/end
				continue
			}
		}

		// Check if this task needs correction
		currentStart := int64(0)
		if t.effectiveKeyspaceStart != nil {
			currentStart = *t.effectiveKeyspaceStart
		}
		currentEnd := int64(0)
		if t.effectiveKeyspaceEnd != nil {
			currentEnd = *t.effectiveKeyspaceEnd
		}

		if currentStart != expectedStart || currentEnd != expectedEnd {
			// Task needs update
			debug.Info("Recalculating chunk %d: old[%d-%d] -> new[%d-%d]",
				t.chunkNumber, currentStart, currentEnd, expectedStart, expectedEnd)

			updateQuery := `
				UPDATE job_tasks
				SET effective_keyspace_start = $2,
				    effective_keyspace_end = $3,
				    updated_at = CURRENT_TIMESTAMP
				WHERE id = $1`

			_, err = s.db.ExecContext(ctx, updateQuery, t.id, expectedStart, expectedEnd)
			if err != nil {
				debug.Error("Failed to update chunk %d: %v", t.chunkNumber, err)
				continue
			}
			needsUpdate = true
		}
	}

	if needsUpdate {
		debug.Info("Recalculated effective keyspace positions for job %s after chunk %d completed",
			jobExecutionID, completedChunkNumber)
	}

	return nil
}
