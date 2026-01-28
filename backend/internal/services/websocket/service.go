package websocket

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobHandler interface for handling job-related WebSocket messages
type JobHandler interface {
	ProcessJobProgress(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessCrackBatch(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessCrackBatchesComplete(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessBenchmarkResult(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessPendingOutfiles(ctx context.Context, agentID int, payload json.RawMessage) error
	ProcessOutfileDeleteRejected(ctx context.Context, agentID int, payload json.RawMessage) error
	RecoverTask(ctx context.Context, taskID string, agentID int, keyspaceProcessed int64) error
	HandleAgentReconnectionWithNoTask(ctx context.Context, agentID int) (int, error)
	GetTask(ctx context.Context, taskID string) (*models.JobTask, error)
	ClearStoppedTaskAgent(ctx context.Context, taskID uuid.UUID, agentID int) error
}

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Agent -> Server messages
	TypeHeartbeat            MessageType = "heartbeat"
	TypeTaskStatus           MessageType = "task_status"
	TypeJobProgress          MessageType = "job_progress"
	TypeJobStatus            MessageType = "job_status"              // Status-only (synchronous)
	TypeCrackBatch           MessageType = "crack_batch"             // Cracks-only (asynchronous)
	TypeCrackBatchesComplete MessageType = "crack_batches_complete" // Signal that all crack batches sent
	TypeBenchmarkResult      MessageType = "benchmark_result"
	TypeAgentStatus          MessageType = "agent_status"
	TypeErrorReport          MessageType = "error_report"
	TypeHardwareInfo         MessageType = "hardware_info"
	TypeSyncResponse         MessageType = "file_sync_response"
	TypeSyncStatus           MessageType = "file_sync_status"
	TypeHashcatOutput        MessageType = "hashcat_output"
	TypeDeviceDetection      MessageType = "device_detection"
	TypePhysicalDeviceDetection MessageType = "physical_device_detection"
	TypeDeviceUpdate            MessageType = "device_update"
	TypeBufferedMessages        MessageType = "buffered_messages"
	TypeCurrentTaskStatus       MessageType = "current_task_status"
	TypeAgentShutdown           MessageType = "agent_shutdown"
	TypePendingOutfiles         MessageType = "pending_outfiles"          // Agent reports tasks with unacknowledged outfiles
	TypeOutfileDeleteRejected   MessageType = "outfile_delete_rejected"   // Agent rejects outfile deletion (line count mismatch)
	TypeTaskStopAck             MessageType = "task_stop_ack"             // Agent acknowledges stop command (GH Issue #12)
	TypeStateSyncResponse       MessageType = "state_sync_response"       // Agent responds with state sync (GH Issue #12)

	// Server -> Agent messages
	TypeTaskAssignment         MessageType = "task_assignment"
	TypeJobStop                MessageType = "job_stop"
	TypeBenchmarkRequest       MessageType = "benchmark_request"
	TypeAgentCommand           MessageType = "agent_command"
	TypeConfigUpdate           MessageType = "config_update"
	TypeSyncRequest            MessageType = "file_sync_request"
	TypeSyncCommand            MessageType = "file_sync_command"
	TypeForceCleanup           MessageType = "force_cleanup"
	TypeBufferAck              MessageType = "buffer_ack"
	TypeRequestCrackRetransmit MessageType = "request_crack_retransmit" // Backend requests full outfile retransmission
	TypeOutfileDeleteApproved  MessageType = "outfile_delete_approved"  // Backend confirms safe to delete outfile
	TypeTaskCompleteAck        MessageType = "task_complete_ack"        // Backend acknowledges task completion (GH Issue #12)
	TypeStateSyncRequest       MessageType = "state_sync_request"       // Backend requests agent state sync (GH Issue #12)

	// Download progress messages
	TypeDownloadProgress MessageType = "download_progress"
	TypeDownloadComplete MessageType = "download_complete"
	TypeDownloadFailed   MessageType = "download_failed"

	// Sync status messages
	TypeSyncStarted   MessageType = "sync_started"
	TypeSyncCompleted MessageType = "sync_completed"
	TypeSyncFailed    MessageType = "sync_failed"
	TypeSyncProgress  MessageType = "sync_progress"

	// Diagnostics message types (GH Issue #23)
	TypeDebugStatusReport   MessageType = "debug_status_report"   // Agent reports debug status
	TypeDebugToggle         MessageType = "debug_toggle"          // Server requests debug toggle
	TypeDebugToggleAck      MessageType = "debug_toggle_ack"      // Agent acknowledges debug toggle
	TypeLogRequest          MessageType = "log_request"           // Server requests agent logs
	TypeLogData             MessageType = "log_data"              // Agent sends log data
	TypeLogStatusRequest    MessageType = "log_status_request"    // Server requests log status
	TypeLogStatusResponse   MessageType = "log_status_response"   // Agent responds with log status
	TypeLogPurge            MessageType = "log_purge"             // Server requests log purge
	TypeLogPurgeAck         MessageType = "log_purge_ack"         // Agent acknowledges log purge
)

// Client represents a connected agent
type Client struct {
	LastSeen time.Time
}

// Message represents a WebSocket message
type Message struct {
	Type         MessageType      `json:"type"`
	Payload      json.RawMessage  `json:"payload"`
	HardwareInfo *models.Hardware `json:"hardware_info,omitempty"`
	OSInfo       json.RawMessage  `json:"os_info,omitempty"`
}


// HeartbeatPayload represents a heartbeat message from agent
type HeartbeatPayload struct {
	AgentID     int     `json:"agent_id"`
	LoadAverage float64 `json:"load_average"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
}

// TaskStatusPayload represents task status update from agent
type TaskStatusPayload struct {
	AgentID   int       `json:"agent_id"`
	TaskID    string    `json:"task_id"`
	Status    string    `json:"status"`
	Progress  float64   `json:"progress"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
}

// AgentStatusPayload represents agent status update
type AgentStatusPayload struct {
	AgentID     int                    `json:"agent_id"`
	Status      string                 `json:"status"`
	Version     string                 `json:"version"`
	LastError   string                 `json:"last_error,omitempty"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Environment map[string]string      `json:"environment"`
	OSInfo      map[string]interface{} `json:"os_info,omitempty"`
}

// ErrorReportPayload represents detailed error report from agent
type ErrorReportPayload struct {
	AgentID    int       `json:"agent_id"`
	Error      string    `json:"error"`
	Stack      string    `json:"stack"`
	Context    any       `json:"context"`
	ReportedAt time.Time `json:"reported_at"`
}

// FileSyncRequestPayload represents a request for the agent to report its current files
type FileSyncRequestPayload struct {
	RequestID string   `json:"request_id"`
	FileTypes []string `json:"file_types"`         // "wordlist", "rule", "binary", "hashlist"
	Category  string   `json:"category,omitempty"` // Filter by category if needed
}

// FileInfo represents information about a file for synchronization
type FileInfo struct {
	Name      string `json:"name"`
	MD5Hash   string `json:"md5_hash"` // MD5 hash used for synchronization
	Size      int64  `json:"size"`
	FileType  string `json:"file_type"` // "wordlist", "rule", "binary", "hashlist"
	Category  string `json:"category,omitempty"`
	ID        int    `json:"id,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// FileSyncResponsePayload represents the agent's response with its current files
type FileSyncResponsePayload struct {
	RequestID string     `json:"request_id"`
	AgentID   int        `json:"agent_id"`
	Files     []FileInfo `json:"files"`
}

// FileSyncCommandPayload represents a command to download specific files
type FileSyncCommandPayload struct {
	RequestID string     `json:"request_id"`
	Action    string     `json:"action"` // "download", "verify", etc.
	Files     []FileInfo `json:"files"`
}

// FileSyncStatusPayload represents a status update for file synchronization
type FileSyncStatusPayload struct {
	RequestID string           `json:"request_id"`
	AgentID   int              `json:"agent_id"`
	Status    string           `json:"status"`   // "in_progress", "completed", "failed"
	Progress  int              `json:"progress"` // 0-100 percentage
	Results   []FileSyncResult `json:"results,omitempty"`
}

// FileSyncResult represents the result of a file sync operation
type FileSyncResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	MD5Hash string `json:"md5_hash,omitempty"`
}

// SyncStartedPayload represents sync start notification from agent
type SyncStartedPayload struct {
	AgentID     int `json:"agent_id"`
	FilesToSync int `json:"files_to_sync"`
}

// SyncCompletedPayload represents sync completion notification from agent
type SyncCompletedPayload struct {
	AgentID     int `json:"agent_id"`
	FilesSynced int `json:"files_synced"`
}

// SyncFailedPayload represents sync failure notification from agent
type SyncFailedPayload struct {
	AgentID int    `json:"agent_id"`
	Error   string `json:"error"`
}

// SyncProgressPayload represents sync progress update from agent
type SyncProgressPayload struct {
	AgentID     int `json:"agent_id"`
	FilesToSync int `json:"files_to_sync"`
	FilesSynced int `json:"files_synced"`
	Percentage  int `json:"percentage"`
}

// TaskAssignmentPayload represents a job task assignment sent to an agent
type TaskAssignmentPayload struct {
	TaskID          string   `json:"task_id"`
	JobExecutionID  string   `json:"job_execution_id"`
	HashlistID      int64    `json:"hashlist_id"`
	HashlistPath    string   `json:"hashlist_path"`
	AttackMode      int      `json:"attack_mode"`
	HashType        int      `json:"hash_type"`
	KeyspaceStart   int64    `json:"keyspace_start"`
	KeyspaceEnd     int64    `json:"keyspace_end"`
	WordlistPaths   []string `json:"wordlist_paths"`
	RulePaths       []string `json:"rule_paths"`
	Mask            string   `json:"mask,omitempty"`
	IncrementMode   string   `json:"increment_mode,omitempty"`
	IncrementMin    *int     `json:"increment_min,omitempty"`
	IncrementMax    *int     `json:"increment_max,omitempty"`
	BinaryPath      string   `json:"binary_path"`
	ChunkDuration   int      `json:"chunk_duration"`
	ReportInterval  int      `json:"report_interval"`
	OutputFormat    string   `json:"output_format"`
	ExtraParameters string   `json:"extra_parameters,omitempty"`
	EnabledDevices  []int    `json:"enabled_devices,omitempty"`
	IsKeyspaceSplit bool     `json:"is_keyspace_split"`
	// Association attack fields (mode 9)
	AssociationWordlistPath string `json:"association_wordlist_path,omitempty"` // Path to the association wordlist
	OriginalHashlistPath    string `json:"original_hashlist_path,omitempty"`    // Path to the original hashlist file (preserves order)
}

// BenchmarkResultPayload represents benchmark results from an agent
type BenchmarkResultPayload struct {
	JobExecutionID         string        `json:"job_execution_id"`                  // Job execution ID to match with request
	AttackMode             int           `json:"attack_mode"`
	HashType               int           `json:"hash_type"`
	Speed                  int64         `json:"speed"`                             // Total hashes per second
	DeviceSpeeds           []DeviceSpeed `json:"device_speeds,omitempty"`           // Per-device speeds
	TotalEffectiveKeyspace int64         `json:"total_effective_keyspace"`          // Hashcat progress[1] from full job run
	Success                bool          `json:"success"`
	Error                  string        `json:"error,omitempty"`
}

// DeviceSpeed represents speed for a single device
type DeviceSpeed struct {
	DeviceID   int    `json:"device_id"`
	DeviceName string `json:"device_name"`
	Speed      int64  `json:"speed"` // H/s for this device
}

// JobStopPayload represents a job stop command
type JobStopPayload struct {
	TaskID         string `json:"task_id"`
	JobExecutionID string `json:"job_execution_id"`
	Reason         string `json:"reason"`
	StopID         string `json:"stop_id,omitempty"` // Unique ID for tracking ACK (GH Issue #12)
}

// BenchmarkRequestPayload represents a benchmark request sent to an agent
type BenchmarkRequestPayload struct {
	RequestID       string `json:"request_id"`
	JobExecutionID  string `json:"job_execution_id"`               // Job execution ID for tracking benchmark results
	AttackMode      int    `json:"attack_mode"`
	HashType        int    `json:"hash_type"`
	BinaryPath      string `json:"binary_path"`
	// Additional fields for real-world speed test
	TaskID          string   `json:"task_id,omitempty"`
	HashlistID      int64    `json:"hashlist_id,omitempty"`
	HashlistPath    string   `json:"hashlist_path,omitempty"`
	WordlistPaths   []string `json:"wordlist_paths,omitempty"`
	RulePaths       []string `json:"rule_paths,omitempty"`
	Mask            string   `json:"mask,omitempty"`
	TestDuration    int      `json:"test_duration,omitempty"`    // Duration in seconds for speed test
	TimeoutDuration int      `json:"timeout_duration,omitempty"` // Maximum time to wait for speedtest (seconds)
	ExtraParameters         string   `json:"extra_parameters,omitempty"`          // Agent-specific hashcat parameters
	EnabledDevices          []int    `json:"enabled_devices,omitempty"`           // List of enabled device IDs
	AssociationWordlistPath string   `json:"association_wordlist_path,omitempty"` // For mode 9 association attacks
}

// TaskCompleteAckPayload is sent by the backend to acknowledge task completion (GH Issue #12)
type TaskCompleteAckPayload struct {
	TaskID    string `json:"task_id"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message,omitempty"` // Optional message (e.g., "task already completed")
}

// TaskStopAckPayload is sent by the agent to acknowledge a stop command (GH Issue #12)
type TaskStopAckPayload struct {
	TaskID    string `json:"task_id"`
	StopID    string `json:"stop_id"`    // Unique ID from the stop command for tracking
	Stopped   bool   `json:"stopped"`    // Whether the task was actually stopped
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message,omitempty"` // Optional reason (e.g., "task already completed")
}

// StateSyncRequestPayload is sent by the backend to request agent state (GH Issue #12)
type StateSyncRequestPayload struct {
	RequestID string `json:"request_id"`
	AgentID   int    `json:"agent_id"`
}

// StateSyncResponsePayload is sent by the agent in response to state sync request (GH Issue #12)
type StateSyncResponsePayload struct {
	RequestID          string   `json:"request_id"`
	HasRunningTask     bool     `json:"has_running_task"`
	TaskID             string   `json:"task_id,omitempty"`
	JobID              string   `json:"job_id,omitempty"`
	Status             string   `json:"status"`                        // idle, running, completing
	PendingCompletions []string `json:"pending_completions,omitempty"` // Task IDs with pending completion ACKs
}

// ============================================================================
// Diagnostics Payload Types (GH Issue #23)
// ============================================================================

// DebugStatusReportPayload is sent by the agent to report debug status
type DebugStatusReportPayload struct {
	Enabled            bool   `json:"enabled"`
	Level              string `json:"level"`
	FileLoggingEnabled bool   `json:"file_logging_enabled"`
	LogFilePath        string `json:"log_file_path,omitempty"`
	LogFileExists      bool   `json:"log_file_exists"`
	LogFileSize        int64  `json:"log_file_size"`
	LogFileModified    int64  `json:"log_file_modified"`
	BufferCount        int    `json:"buffer_count"`
	BufferCapacity     int    `json:"buffer_capacity"`
}

// DebugTogglePayload is sent by the server to toggle debug mode
type DebugTogglePayload struct {
	Enable bool `json:"enable"`
}

// DebugToggleAckPayload is sent by the agent to acknowledge debug toggle
type DebugToggleAckPayload struct {
	Success         bool   `json:"success"`
	Enabled         bool   `json:"enabled"`
	RestartRequired bool   `json:"restart_required"`
	Message         string `json:"message,omitempty"`
}

// LogRequestPayload is sent by the server to request logs
type LogRequestPayload struct {
	RequestID  string `json:"request_id"`
	HoursBack  int    `json:"hours_back,omitempty"`
	IncludeAll bool   `json:"include_all,omitempty"`
}

// LogEntryPayload represents a single log entry
type LogEntryPayload struct {
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
	Level     string `json:"level"`
	Message   string `json:"message"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Function  string `json:"function,omitempty"`
}

// LogDataPayload is sent by the agent with log data
type LogDataPayload struct {
	RequestID   string            `json:"request_id"`
	AgentID     int               `json:"agent_id"`
	Entries     []LogEntryPayload `json:"entries"`
	FileContent string            `json:"file_content,omitempty"`
	TotalCount  int               `json:"total_count"`
	Truncated   bool              `json:"truncated"`
	Error       string            `json:"error,omitempty"`
}

// LogStatusRequestPayload is sent by the server to request log status
type LogStatusRequestPayload struct {
	RequestID string `json:"request_id"`
}

// LogStatusResponsePayload is sent by the agent with log status
type LogStatusResponsePayload struct {
	RequestID       string `json:"request_id"`
	LogFileExists   bool   `json:"log_file_exists"`
	LogFilePath     string `json:"log_file_path,omitempty"`
	LogFileSize     int64  `json:"log_file_size"`
	LogFileModified int64  `json:"log_file_modified"`
	DebugEnabled    bool   `json:"debug_enabled"`
	BufferCount     int    `json:"buffer_count"`
}

// LogPurgePayload is sent by the server to request log purge
type LogPurgePayload struct {
	RequestID string `json:"request_id"`
}

// LogPurgeAckPayload is sent by the agent to acknowledge log purge
type LogPurgeAckPayload struct {
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

// Service handles WebSocket business logic
type Service struct {
	agentService *services.AgentService
	clients      map[int]*Client
	mu           sync.RWMutex
	jobHandler   JobHandler // Interface for handling job-related messages

	// Semaphore for limiting concurrent crack batch processing
	crackBatchSem chan struct{}
	crackBatchWg  sync.WaitGroup
}

// NewService creates a new WebSocket service
func NewService(agentService *services.AgentService) *Service {
	// Limit concurrent crack batch processing to 10 goroutines
	// With 100-connection pool: 10 batches * ~5 connections/batch = ~50 connections peak
	// This doubles crack processing throughput while staying under 50% pool capacity
	maxConcurrentCrackBatches := 10

	return &Service{
		agentService:  agentService,
		clients:       make(map[int]*Client),
		crackBatchSem: make(chan struct{}, maxConcurrentCrackBatches),
	}
}

// SetJobHandler sets the job handler for processing job-related messages
func (s *Service) SetJobHandler(handler JobHandler) {
	s.jobHandler = handler
}

// GetJobHandler returns the job handler for processing job-related messages
func (s *Service) GetJobHandler() JobHandler {
	return s.jobHandler
}

// HandleMessage processes incoming WebSocket messages
func (s *Service) HandleMessage(ctx context.Context, agent *models.Agent, msg *Message) error {
	// Update heartbeat on ANY message received from the agent
	// This ensures the agent is considered alive as long as it's communicating
	if err := s.agentService.UpdateHeartbeat(ctx, agent.ID); err != nil {
		// Log but don't fail the message processing
		fmt.Printf("Warning: failed to update heartbeat for agent %d: %v\n", agent.ID, err)
	}

	switch msg.Type {
	case TypeHeartbeat:
		return s.handleHeartbeat(ctx, agent, msg)
	case TypeTaskStatus:
		return s.handleTaskStatus(ctx, agent, msg)
	case TypeJobProgress:
		return s.handleJobProgress(ctx, agent, msg)
	case TypeJobStatus:
		return s.handleJobStatus(ctx, agent, msg)
	case TypeCrackBatch:
		return s.handleCrackBatch(ctx, agent, msg)
	case TypeCrackBatchesComplete:
		return s.handleCrackBatchesComplete(ctx, agent, msg)
	case TypeBenchmarkResult:
		return s.handleBenchmarkResult(ctx, agent, msg)
	case TypeAgentStatus:
		return s.handleAgentStatus(ctx, agent, msg)
	case TypeErrorReport:
		return s.handleErrorReport(ctx, agent, msg)
	case TypeHardwareInfo:
		return s.handleHardwareInfo(ctx, agent, msg)
	case TypeSyncResponse:
		// File sync response is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeSyncRequest:
		return s.handleSyncRequest(ctx, agent, msg)
	case TypeSyncCommand:
		return s.handleSyncCommand(ctx, agent, msg)
	case TypeHashcatOutput:
		return s.handleHashcatOutput(ctx, agent, msg)
	case TypeCurrentTaskStatus:
		// Current task status is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeAgentShutdown:
		// Agent shutdown is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeSyncStarted:
		return s.handleSyncStarted(ctx, agent, msg)
	case TypeSyncCompleted:
		return s.handleSyncCompleted(ctx, agent, msg)
	case TypeSyncFailed:
		return s.handleSyncFailed(ctx, agent, msg)
	case TypeSyncProgress:
		return s.handleSyncProgress(ctx, agent, msg)
	case TypeSyncStatus:
		// File sync status (file_sync_status) is handled in the handler layer (handleSyncStatus)
		// Just update heartbeat here - return nil to avoid "unknown message type" error
		return nil
	case TypeDebugStatusReport:
		// Debug status report is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeDebugToggleAck:
		// Debug toggle ack is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeLogData:
		// Log data is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeLogStatusResponse:
		// Log status response is handled in the handler layer
		// Just update heartbeat here
		return nil
	case TypeLogPurgeAck:
		// Log purge ack is handled in the handler layer
		// Just update heartbeat here
		return nil
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

// updateLastSeen updates the last seen timestamp for an agent
func (s *Service) updateLastSeen(agentID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, ok := s.clients[agentID]; ok {
		client.LastSeen = time.Now()
	}
}

// GetLastSeen returns when an agent was last seen
func (s *Service) GetLastSeen(agentID int) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if client, ok := s.clients[agentID]; ok {
		return client.LastSeen
	}
	return time.Time{}
}

// handleHeartbeat processes heartbeat messages
func (s *Service) handleHeartbeat(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload HeartbeatPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal heartbeat: %w", err)
	}

	// Update agent status in database
	if err := s.agentService.UpdateAgentStatus(ctx, agent.ID, models.AgentStatusActive, nil); err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	s.updateLastSeen(agent.ID)
	return nil
}


// handleTaskStatus processes task status messages
func (s *Service) handleTaskStatus(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload TaskStatusPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal task status: %w", err)
	}

	// TODO: Update task status in task service
	return nil
}

// handleAgentStatus processes agent status messages
func (s *Service) handleAgentStatus(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload AgentStatusPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal agent status: %w", err)
	}

	// Update agent status in database
	var lastError *string
	if payload.LastError != "" {
		lastError = &payload.LastError
	}

	if err := s.agentService.UpdateAgentStatus(ctx, agent.ID, payload.Status, lastError); err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	// Update agent version if provided
	if payload.Version != "" {
		if err := s.agentService.UpdateAgentVersion(ctx, agent.ID, payload.Version); err != nil {
			// Log error but don't fail the status update
			debug.Error("Failed to update agent version: %v", err)
		} else {
			debug.Info("Updated agent %d version to %s", agent.ID, payload.Version)
		}
	}

	// Update OS info if provided
	if payload.OSInfo != nil && len(payload.OSInfo) > 0 {
		if err := s.agentService.UpdateAgentOSInfo(ctx, agent.ID, payload.OSInfo); err != nil {
			// Log error but don't fail the status update
			debug.Error("Failed to update agent OS info: %v", err)
		}
	}

	return nil
}

// handleErrorReport processes error report messages
func (s *Service) handleErrorReport(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload ErrorReportPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal error report: %w", err)
	}

	// Update agent status with error
	if err := s.agentService.UpdateAgentStatus(ctx, agent.ID, "error", &payload.Error); err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	// Dispatch agent error notification to agent owner
	go s.dispatchAgentErrorNotification(ctx, agent, &payload)

	return nil
}

// dispatchAgentErrorNotification sends an agent error notification to the agent owner
func (s *Service) dispatchAgentErrorNotification(ctx context.Context, agent *models.Agent, payload *ErrorReportPayload) {
	dispatcher := services.GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping agent error notification")
		return
	}

	// Check if agent has an owner
	if agent.OwnerID == nil {
		debug.Warning("Agent %d has no owner, skipping error notification", agent.ID)
		return
	}

	params := models.NotificationDispatchParams{
		UserID:  *agent.OwnerID,
		Type:    models.NotificationTypeAgentError,
		Title:   fmt.Sprintf("Agent '%s' Error", agent.Name),
		Message: fmt.Sprintf("Agent reported an error: %s", payload.Error),
		Data: map[string]interface{}{
			"AgentID":    agent.ID,
			"AgentName":  agent.Name,
			"Error":      payload.Error,
			"StackTrace": payload.Stack,
			"Context":    payload.Context,
			"ReportedAt": payload.ReportedAt.Format(time.RFC3339),
		},
		SourceType: "agent",
		SourceID:   uuid.New().String(), // Unique per error event - each error is distinct
	}

	if err := dispatcher.Dispatch(ctx, params); err != nil {
		debug.Error("Failed to dispatch agent error notification: %v", err)
	} else {
		debug.Log("Agent error notification dispatched", map[string]interface{}{
			"agent_id":   agent.ID,
			"agent_name": agent.Name,
		})
	}
}

// handleHardwareInfo processes hardware information messages
func (s *Service) handleHardwareInfo(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If HardwareInfo is not directly populated, try to unmarshal from Payload
	var hardware models.Hardware
	if err := json.Unmarshal(msg.Payload, &hardware); err != nil {
		return fmt.Errorf("failed to unmarshal hardware info: %w", err)
	}

	// Update agent's hardware information in the database
	agent.Hardware = hardware
	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent hardware info: %w", err)
	}

	return nil
}

// handleSyncRequest processes file sync request messages
func (s *Service) handleSyncRequest(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload FileSyncRequestPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal file sync request: %w", err)
	}

	// Log the request
	debug.Debug("Received file sync request from agent %d: %+v", agent.ID, payload)

	// This function should just acknowledge receipt of the request
	// The actual file comparison happens in the WebSocket handler

	// Update agent metadata to indicate sync is in progress
	if agent.Metadata == nil {
		agent.Metadata = make(map[string]string)
	}
	agent.Metadata["sync_request_id"] = payload.RequestID
	agent.Metadata["sync_status"] = "requested"
	agent.Metadata["sync_timestamp"] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent metadata for sync request: %w", err)
	}

	return nil
}

// handleSyncCommand processes file sync command messages
func (s *Service) handleSyncCommand(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload FileSyncCommandPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal file sync command: %w", err)
	}

	// Log the command
	fmt.Printf("Received file sync command for agent %d: action=%s, files=%d\n",
		agent.ID, payload.Action, len(payload.Files))

	// Update agent metadata to indicate sync command sent
	if agent.Metadata == nil {
		agent.Metadata = make(map[string]string)
	}
	agent.Metadata["sync_request_id"] = payload.RequestID
	agent.Metadata["sync_status"] = "command_received"
	agent.Metadata["sync_action"] = payload.Action
	agent.Metadata["sync_files_count"] = fmt.Sprintf("%d", len(payload.Files))
	agent.Metadata["sync_timestamp"] = fmt.Sprintf("%d", time.Now().Unix())

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent metadata for sync command: %w", err)
	}

	return nil
}

// handleJobProgress processes job progress messages from agents
func (s *Service) handleJobProgress(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If no job handler is set, just log and ignore
	if s.jobHandler == nil {
		fmt.Printf("Received job progress from agent %d but no job handler set\n", agent.ID)
		return nil
	}

	// Process job progress asynchronously to avoid blocking the read loop
	go func() {
		// Create a new context with timeout for the async operation
		asyncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.jobHandler.ProcessJobProgress(asyncCtx, agent.ID, msg.Payload); err != nil {
			debug.Error("Failed to process job progress from agent %d: %v", agent.ID, err)
		}
	}()

	return nil
}

// handleJobStatus processes job status messages from agents (status-only, synchronous)
func (s *Service) handleJobStatus(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If no job handler is set, just log and ignore
	if s.jobHandler == nil {
		debug.Debug("Received job status from agent %d but no job handler set", agent.ID)
		return nil
	}

	// Process job status synchronously (small, frequent messages)
	// This is the same as job progress handler since they both update the same data
	if err := s.jobHandler.ProcessJobProgress(ctx, agent.ID, msg.Payload); err != nil {
		debug.Error("Failed to process job status from agent %d: %v", agent.ID, err)
		return err
	}

	return nil
}

// handleCrackBatch processes crack batch messages from agents (cracks-only, asynchronous)
func (s *Service) handleCrackBatch(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If no job handler is set, just log and ignore
	if s.jobHandler == nil {
		debug.Debug("Received crack batch from agent %d but no job handler set", agent.ID)
		return nil
	}

	// Increment wait group before spawning goroutine
	s.crackBatchWg.Add(1)

	// Process crack batch asynchronously to avoid blocking the read loop
	// Use semaphore to limit concurrent processing and prevent database overload
	go func() {
		defer s.crackBatchWg.Done()

		// Acquire semaphore (blocks if at capacity)
		s.crackBatchSem <- struct{}{}
		defer func() { <-s.crackBatchSem }() // Release semaphore when done

		// Create a new context with timeout for the async operation
		// Extended timeout for large batches (10k cracks can take time to insert)
		asyncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		debug.Debug("Processing crack batch from agent %d (semaphore acquired)", agent.ID)
		if err := s.jobHandler.ProcessCrackBatch(asyncCtx, agent.ID, msg.Payload); err != nil {
			debug.Error("Failed to process crack batch from agent %d: %v", agent.ID, err)
		} else {
			debug.Debug("Successfully processed crack batch from agent %d", agent.ID)
		}
	}()

	return nil
}

// handleCrackBatchesComplete processes crack_batches_complete signal from agents
func (s *Service) handleCrackBatchesComplete(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If no job handler is set, just log and ignore
	if s.jobHandler == nil {
		debug.Debug("Received crack_batches_complete from agent %d but no job handler set", agent.ID)
		return nil
	}

	// Forward to job handler (which will parse and handle the message)
	return s.jobHandler.ProcessCrackBatchesComplete(ctx, agent.ID, msg.Payload)
}

// handleBenchmarkResult processes benchmark result messages from agents
func (s *Service) handleBenchmarkResult(ctx context.Context, agent *models.Agent, msg *Message) error {
	// If no job handler is set, just log and ignore
	if s.jobHandler == nil {
		fmt.Printf("Received benchmark result from agent %d but no job handler set\n", agent.ID)
		return nil
	}

	// Forward to job handler
	return s.jobHandler.ProcessBenchmarkResult(ctx, agent.ID, msg.Payload)
}

// handleHashcatOutput processes hashcat output messages from agents
func (s *Service) handleHashcatOutput(ctx context.Context, agent *models.Agent, msg *Message) error {
	// Process hashcat output asynchronously to avoid blocking the read loop
	go func() {
		var payload struct {
			TaskID    string    `json:"task_id"`
			Output    string    `json:"output"`
			IsError   bool      `json:"is_error"`
			Timestamp time.Time `json:"timestamp"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			debug.Error("Failed to unmarshal hashcat output from agent %d: %v", agent.ID, err)
			return
		}

		// Log the output for debugging
		if payload.IsError {
			fmt.Printf("[Agent %d][Task %s][ERROR] %s\n", agent.ID, payload.TaskID, payload.Output)
		} else {
			fmt.Printf("[Agent %d][Task %s] %s\n", agent.ID, payload.TaskID, payload.Output)
		}

		// TODO: Store output in database or forward to interested parties via SSE
	}()

	return nil
}

// HandleAgentDisconnection handles when an agent disconnects unexpectedly
func (s *Service) HandleAgentDisconnection(ctx context.Context, agentID int) error {
	// Check if we have a job handler
	if s.jobHandler == nil {
		debug.Warning("Agent %d disconnected but no job handler available to mark tasks", agentID)
		return nil
	}
	
	// Call the job handler to mark tasks as reconnect_pending
	// We use a type assertion to check if the handler supports disconnection handling
	type disconnectionHandler interface {
		HandleAgentDisconnection(ctx context.Context, agentID int) error
	}
	
	if handler, ok := s.jobHandler.(disconnectionHandler); ok {
		return handler.HandleAgentDisconnection(ctx, agentID)
	}
	
	debug.Warning("Job handler does not support disconnection handling")
	return nil
}

// handleSyncStarted processes sync started messages from agents
func (s *Service) handleSyncStarted(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload SyncStartedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal sync started: %w", err)
	}

	// Update agent sync status
	agent.SyncStatus = models.AgentSyncStatusInProgress
	agent.SyncStartedAt = sql.NullTime{Time: time.Now(), Valid: true}
	agent.FilesToSync = payload.FilesToSync
	agent.FilesSynced = 0
	agent.SyncError = sql.NullString{Valid: false}

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent sync status: %w", err)
	}

	debug.Info("Agent %d started file sync with %d files", agent.ID, payload.FilesToSync)
	return nil
}

// handleSyncCompleted processes sync completed messages from agents
func (s *Service) handleSyncCompleted(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload SyncCompletedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal sync completed: %w", err)
	}

	// Update agent sync status
	agent.SyncStatus = models.AgentSyncStatusCompleted
	agent.SyncCompletedAt = sql.NullTime{Time: time.Now(), Valid: true}
	agent.FilesSynced = payload.FilesSynced
	agent.SyncError = sql.NullString{Valid: false}

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent sync status: %w", err)
	}

	debug.Info("Agent %d completed file sync with %d files", agent.ID, payload.FilesSynced)
	return nil
}

// handleSyncFailed processes sync failed messages from agents
func (s *Service) handleSyncFailed(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload SyncFailedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal sync failed: %w", err)
	}

	// Update agent sync status
	agent.SyncStatus = models.AgentSyncStatusFailed
	agent.SyncError = sql.NullString{String: payload.Error, Valid: true}

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent sync status: %w", err)
	}

	debug.Error("Agent %d failed file sync: %s", agent.ID, payload.Error)
	return nil
}

// handleSyncProgress processes sync progress messages from agents
func (s *Service) handleSyncProgress(ctx context.Context, agent *models.Agent, msg *Message) error {
	var payload SyncProgressPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal sync progress: %w", err)
	}

	// Update agent sync progress
	agent.FilesToSync = payload.FilesToSync
	agent.FilesSynced = payload.FilesSynced

	if err := s.agentService.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent sync progress: %w", err)
	}

	debug.Info("Agent %d sync progress: %d/%d files (%d%%)",
		agent.ID, payload.FilesSynced, payload.FilesToSync, payload.Percentage)
	return nil
}
