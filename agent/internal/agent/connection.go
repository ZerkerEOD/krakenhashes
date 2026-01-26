package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/buffer"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/hardware"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/hardware/types"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/jobs"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/logbuffer"
	filesync "github.com/ZerkerEOD/krakenhashes/agent/internal/sync"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/version"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
	"github.com/gorilla/websocket"
)

// WSMessageType represents different types of WebSocket messages
type WSMessageType string

const (
	WSTypeHardwareInfo WSMessageType = "hardware_info"
	WSTypeMetrics      WSMessageType = "metrics"
	WSTypeHeartbeat    WSMessageType = "heartbeat"
	WSTypeAgentStatus  WSMessageType = "agent_status"

	// Configuration message types
	WSTypeConfigUpdate WSMessageType = "config_update"

	// File synchronization message types
	WSTypeFileSyncRequest  WSMessageType = "file_sync_request"
	WSTypeFileSyncResponse WSMessageType = "file_sync_response"
	WSTypeFileSyncCommand  WSMessageType = "file_sync_command"
	WSTypeFileSyncStatus   WSMessageType = "file_sync_status"

	// Job execution message types
	WSTypeTaskAssignment        WSMessageType = "task_assignment"
	WSTypeJobProgress           WSMessageType = "job_progress"
	WSTypeJobStatus             WSMessageType = "job_status"              // Status-only (synchronous)
	WSTypeCrackBatch            WSMessageType = "crack_batch"             // Cracks-only (asynchronous)
	WSTypeCrackBatchesComplete  WSMessageType = "crack_batches_complete" // Signal all batches sent
	WSTypeJobStop               WSMessageType = "job_stop"
	WSTypeBenchmarkRequest      WSMessageType = "benchmark_request"
	WSTypeBenchmarkResult       WSMessageType = "benchmark_result"
	WSTypeHashcatOutput         WSMessageType = "hashcat_output"
	WSTypeForceCleanup          WSMessageType = "force_cleanup"
	WSTypeCurrentTaskStatus     WSMessageType = "current_task_status"

	// Device detection message types
	WSTypeDeviceDetection         WSMessageType = "device_detection"
	WSTypePhysicalDeviceDetection WSMessageType = "physical_device_detection"
	WSTypeDeviceUpdate            WSMessageType = "device_update"

	// Buffer-related message types
	WSTypeBufferedMessages WSMessageType = "buffered_messages"
	WSTypeBufferAck        WSMessageType = "buffer_ack"

	// Shutdown message type
	WSTypeAgentShutdown WSMessageType = "agent_shutdown"

	// Outfile acknowledgment protocol message types
	WSTypePendingOutfiles        WSMessageType = "pending_outfiles"         // Agent -> Server: report tasks with pending outfiles
	WSTypeRequestCrackRetransmit WSMessageType = "request_crack_retransmit" // Server -> Agent: request full outfile retransmission
	WSTypeOutfileDeleteApproved  WSMessageType = "outfile_delete_approved"  // Server -> Agent: safe to delete outfile
	WSTypeOutfileDeleteRejected  WSMessageType = "outfile_delete_rejected"  // Agent -> Server: reject deletion (line count mismatch)

	// Task state synchronization message types (GH Issue #12)
	WSTypeTaskCompleteAck   WSMessageType = "task_complete_ack"   // Server -> Agent: acknowledge task completion
	WSTypeTaskStopAck       WSMessageType = "task_stop_ack"       // Agent -> Server: acknowledge stop command
	WSTypeStateSyncRequest  WSMessageType = "state_sync_request"  // Server -> Agent: request agent state
	WSTypeStateSyncResponse WSMessageType = "state_sync_response" // Agent -> Server: report current state

	// Diagnostics message types (GH Issue #23)
	WSTypeDebugStatusReport WSMessageType = "debug_status_report" // Agent -> Server: report debug state
	WSTypeDebugToggle       WSMessageType = "debug_toggle"        // Server -> Agent: toggle debug mode
	WSTypeDebugToggleAck    WSMessageType = "debug_toggle_ack"    // Agent -> Server: acknowledge toggle
	WSTypeLogRequest        WSMessageType = "log_request"         // Server -> Agent: request logs
	WSTypeLogData           WSMessageType = "log_data"            // Agent -> Server: send log data
	WSTypeLogStatusRequest  WSMessageType = "log_status_request"  // Server -> Agent: request log file status
	WSTypeLogStatusResponse WSMessageType = "log_status_response" // Agent -> Server: report log file info
	WSTypeLogPurge          WSMessageType = "log_purge"           // Server -> Agent: delete log files
	WSTypeLogPurgeAck       WSMessageType = "log_purge_ack"       // Agent -> Server: confirm purge
)

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type         WSMessageType   `json:"type"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Metrics      *MetricsData    `json:"metrics,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
}

// FileSyncRequestPayload represents a request for the agent to report its current files
type FileSyncRequestPayload struct {
	FileTypes []string `json:"file_types"` // "wordlist", "rule", "binary"
}

// FileInfo represents information about a file for synchronization
type FileInfo = filesync.FileInfo

// FileSyncResponsePayload represents the agent's response with its current files
type FileSyncResponsePayload struct {
	AgentID int        `json:"agent_id"`
	Files   []FileInfo `json:"files"`
}

// FileSyncCommandPayload represents a command to download specific files
type FileSyncCommandPayload struct {
	Files []FileInfo `json:"files"`
}

// CurrentTaskStatusPayload represents the agent's current task status
// Includes all progress fields needed for offline task completion handling
type CurrentTaskStatusPayload struct {
	AgentID                int     `json:"agent_id"`
	HasRunningTask         bool    `json:"has_running_task"`
	TaskID                 string  `json:"task_id,omitempty"`
	JobID                  string  `json:"job_id,omitempty"`
	KeyspaceProcessed      int64   `json:"keyspace_processed,omitempty"`
	EffectiveProgress      int64   `json:"effective_progress,omitempty"`
	ProgressPercent        float64 `json:"progress_percent,omitempty"`
	TotalEffectiveKeyspace *int64  `json:"total_effective_keyspace,omitempty"`
	HashRate               int64   `json:"hash_rate,omitempty"`
	CrackedCount           int     `json:"cracked_count,omitempty"`
	AllHashesCracked       bool    `json:"all_hashes_cracked,omitempty"`
	Status                 string  `json:"status,omitempty"`
	ErrorMessage           string  `json:"error_message,omitempty"`
}

// BenchmarkRequest represents a request to test speed for a specific job configuration
type BenchmarkRequest struct {
	RequestID       string             `json:"request_id"`
	JobExecutionID  string             `json:"job_execution_id"` // Job execution ID for tracking results
	TaskID          string             `json:"task_id"`
	HashlistID      int64              `json:"hashlist_id"`
	HashlistPath    string             `json:"hashlist_path"`
	AttackMode      int                `json:"attack_mode"`
	HashType        int                `json:"hash_type"`
	WordlistPaths   []string           `json:"wordlist_paths"`
	RulePaths       []string           `json:"rule_paths"`
	Mask            string             `json:"mask,omitempty"`
	BinaryPath      string             `json:"binary_path"`
	TestDuration    int                `json:"test_duration"`    // How long to run test (seconds)
	TimeoutDuration int                `json:"timeout_duration"` // Maximum time to wait for speedtest (seconds)
	ExtraParameters         string   `json:"extra_parameters,omitempty"`          // Agent-specific hashcat parameters
	EnabledDevices          []int    `json:"enabled_devices,omitempty"`           // List of enabled device IDs
	AssociationWordlistPath string   `json:"association_wordlist_path,omitempty"` // For mode 9 association attacks
}

// BenchmarkResult represents the result of a speed test
type BenchmarkResult struct {
	RequestID      string              `json:"request_id"`
	JobExecutionID string              `json:"job_execution_id"`  // Job execution ID to match with request
	TaskID         string              `json:"task_id"`
	TotalSpeed     int64               `json:"total_speed"` // Total H/s across all devices
	DeviceSpeeds   []jobs.DeviceSpeed  `json:"device_speeds"`
	Success        bool                `json:"success"`
	ErrorMessage   string              `json:"error_message,omitempty"`
}

// TaskCompleteAckPayload is received from backend acknowledging task completion (GH Issue #12)
type TaskCompleteAckPayload struct {
	TaskID    string `json:"task_id"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message,omitempty"`
}

// TaskStopAckPayload is sent to acknowledge a stop command (GH Issue #12)
type TaskStopAckPayload struct {
	TaskID    string `json:"task_id"`
	StopID    string `json:"stop_id"`
	Stopped   bool   `json:"stopped"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message,omitempty"`
}

// StateSyncRequestPayload is received from backend requesting state sync (GH Issue #12)
type StateSyncRequestPayload struct {
	RequestID string `json:"request_id"`
	AgentID   int    `json:"agent_id"`
}

// StateSyncResponsePayload is sent in response to state sync request (GH Issue #12)
type StateSyncResponsePayload struct {
	RequestID          string   `json:"request_id"`
	HasRunningTask     bool     `json:"has_running_task"`
	TaskID             string   `json:"task_id,omitempty"`
	JobID              string   `json:"job_id,omitempty"`
	Status             string   `json:"status"` // idle, running, completing
	PendingCompletions []string `json:"pending_completions,omitempty"`
}

// DebugStatusReportPayload is sent to report debug state on connection (GH Issue #23)
type DebugStatusReportPayload struct {
	Enabled            bool   `json:"enabled"`
	Level              string `json:"level"`
	FileLoggingEnabled bool   `json:"file_logging_enabled"`
	LogFilePath        string `json:"log_file_path,omitempty"`
	LogFileExists      bool   `json:"log_file_exists"`
	LogFileSize        int64  `json:"log_file_size"`
	LogFileModified    int64  `json:"log_file_modified,omitempty"` // Unix timestamp
	BufferCount        int    `json:"buffer_count"`
	BufferCapacity     int    `json:"buffer_capacity"`
}

// DebugTogglePayload is received from backend to toggle debug mode (GH Issue #23)
type DebugTogglePayload struct {
	Enable bool `json:"enable"`
}

// DebugToggleAckPayload is sent to acknowledge debug toggle (GH Issue #23)
type DebugToggleAckPayload struct {
	Success         bool   `json:"success"`
	Enabled         bool   `json:"enabled"`
	RestartRequired bool   `json:"restart_required"`
	Message         string `json:"message,omitempty"`
}

// LogRequestPayload is received from backend to request logs (GH Issue #23)
type LogRequestPayload struct {
	RequestID  string `json:"request_id"`
	HoursBack  int    `json:"hours_back"`
	IncludeAll bool   `json:"include_all"` // Include all logs regardless of time
}

// LogDataPayload is sent to deliver log data (GH Issue #23)
type LogDataPayload struct {
	RequestID   string                `json:"request_id"`
	AgentID     int                   `json:"agent_id"`
	Entries     []LogEntryPayload     `json:"entries,omitempty"`
	FileContent string                `json:"file_content,omitempty"` // Raw log file content if available
	TotalCount  int                   `json:"total_count"`
	Truncated   bool                  `json:"truncated"`
	Error       string                `json:"error,omitempty"`
}

// LogEntryPayload represents a single log entry for transmission (GH Issue #23)
type LogEntryPayload struct {
	Timestamp int64  `json:"timestamp"` // Unix timestamp with milliseconds
	Level     string `json:"level"`
	Message   string `json:"message"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Function  string `json:"function,omitempty"`
}

// LogStatusRequestPayload is received from backend to check log file status (GH Issue #23)
type LogStatusRequestPayload struct {
	RequestID string `json:"request_id"`
}

// LogStatusResponsePayload is sent to report log file status (GH Issue #23)
type LogStatusResponsePayload struct {
	RequestID       string `json:"request_id"`
	LogFileExists   bool   `json:"log_file_exists"`
	LogFilePath     string `json:"log_file_path,omitempty"`
	LogFileSize     int64  `json:"log_file_size"`
	LogFileModified int64  `json:"log_file_modified,omitempty"` // Unix timestamp
	DebugEnabled    bool   `json:"debug_enabled"`
	BufferCount     int    `json:"buffer_count"`
}

// LogPurgePayload is received from backend to delete logs (GH Issue #23)
type LogPurgePayload struct {
	RequestID string `json:"request_id"`
}

// LogPurgeAckPayload is sent to confirm log purge (GH Issue #23)
type LogPurgeAckPayload struct {
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

// JobStopPayload represents a job stop command (updated for GH Issue #12)
type JobStopPayload struct {
	TaskID         string `json:"task_id"`
	JobExecutionID string `json:"job_execution_id"`
	Reason         string `json:"reason"`
	StopID         string `json:"stop_id,omitempty"` // Unique ID for tracking ACK
}

// MetricsData represents the metrics data sent to the server
type MetricsData struct {
	AgentID     int                `json:"agent_id"`
	CollectedAt time.Time          `json:"collected_at"`
	CPUs        []CPUMetrics       `json:"cpus"`
	GPUs        []GPUMetrics       `json:"gpus"`
	Memory      MemoryMetrics      `json:"memory"`
	Disk        []DiskMetrics      `json:"disk"`
	Network     []NetworkMetrics   `json:"network"`
	Process     []ProcessMetrics   `json:"process"`
	Custom      map[string]float64 `json:"custom,omitempty"`
}

// CPUMetrics represents CPU performance metrics
type CPUMetrics struct {
	Index       int     `json:"index"`
	Usage       float64 `json:"usage"`
	Temperature float64 `json:"temperature"`
	Frequency   float64 `json:"frequency"`
}

// GPUMetrics represents GPU performance metrics
type GPUMetrics struct {
	Index       int     `json:"index"`
	Usage       float64 `json:"usage"`
	Temperature float64 `json:"temperature"`
	Memory      float64 `json:"memory"`
	PowerUsage  float64 `json:"power_usage"`
}

// MemoryMetrics represents memory usage metrics
type MemoryMetrics struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Free      uint64  `json:"free"`
	UsagePerc float64 `json:"usage_perc"`
}

// DiskMetrics represents disk usage metrics
type DiskMetrics struct {
	Path      string  `json:"path"`
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Free      uint64  `json:"free"`
	UsagePerc float64 `json:"usage_perc"`
}

// NetworkMetrics represents network interface metrics
type NetworkMetrics struct {
	Interface string `json:"interface"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
}

// ProcessMetrics represents process metrics
type ProcessMetrics struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	CPUUsage   float64 `json:"cpu_usage"`
	MemoryUsed uint64  `json:"memory_used"`
}

// Default connection timing values
const (
	defaultWriteWait  = 10 * time.Second
	defaultPongWait   = 60 * time.Second
	defaultPingPeriod = 54 * time.Second
	maxMessageSize    = 512 * 1024 // 512KB
)

// Connection timing configuration
var (
	writeWait  time.Duration
	pongWait   time.Duration
	pingPeriod time.Duration
)

// BackendConfig represents the configuration received from the backend
type BackendConfig struct {
	WebSocket struct {
		WriteWait  string `json:"write_wait"`
		PongWait   string `json:"pong_wait"`
		PingPeriod string `json:"ping_period"`
	} `json:"websocket"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
	ServerVersion     string `json:"server_version"`
}

// getEnvDuration gets a duration from an environment variable with a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	debug.Info("Attempting to load environment variable: %s", key)
	value := os.Getenv(key)
	debug.Info("Environment variable %s value: %q", key, value)

	if value != "" {
		duration, err := time.ParseDuration(value)
		if err == nil {
			debug.Info("Successfully parsed %s: %v", key, duration)
			return duration
		}
		debug.Warning("Invalid %s value: %s, using default: %v", key, value, defaultValue)
	}
	debug.Info("No %s environment variable found, using default: %v", key, defaultValue)
	return defaultValue
}

// fetchBackendConfig fetches WebSocket configuration from the backend
func fetchBackendConfig(urlConfig *config.URLConfig) (*BackendConfig, error) {
	debug.Info("Fetching backend configuration from %s", urlConfig.GetAPIBaseURL())
	
	// Create the request
	url := fmt.Sprintf("%s/agent/config", urlConfig.GetAPIBaseURL())
	debug.Debug("Fetching config from: %s", url)
	
	// Create HTTP client with TLS configuration
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Skip verification for self-signed certs
		},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}
	
	resp, err := client.Get(url)
	if err != nil {
		debug.Error("Failed to fetch backend configuration: %v", err)
		return nil, fmt.Errorf("failed to fetch backend configuration: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		debug.Error("Backend returned non-OK status: %d", resp.StatusCode)
		return nil, fmt.Errorf("backend returned status %d", resp.StatusCode)
	}
	
	var config BackendConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		debug.Error("Failed to decode backend configuration: %v", err)
		return nil, fmt.Errorf("failed to decode backend configuration: %w", err)
	}
	
	debug.Info("Successfully fetched backend configuration:")
	debug.Info("- WebSocket WriteWait: %s", config.WebSocket.WriteWait)
	debug.Info("- WebSocket PongWait: %s", config.WebSocket.PongWait)
	debug.Info("- WebSocket PingPeriod: %s", config.WebSocket.PingPeriod)
	debug.Info("- Heartbeat Interval: %d", config.HeartbeatInterval)
	debug.Info("- Server Version: %s", config.ServerVersion)
	
	return &config, nil
}

// initTimingConfig initializes the timing configuration from backend config or defaults
func initTimingConfig(backendConfig *BackendConfig) {
	debug.Info("Initializing WebSocket timing configuration")
	
	if backendConfig != nil {
		// Parse timing from backend config
		var err error
		writeWait, err = time.ParseDuration(backendConfig.WebSocket.WriteWait)
		if err != nil {
			debug.Warning("Failed to parse WriteWait from backend: %v, using default", err)
			writeWait = defaultWriteWait
		}
		
		pongWait, err = time.ParseDuration(backendConfig.WebSocket.PongWait)
		if err != nil {
			debug.Warning("Failed to parse PongWait from backend: %v, using default", err)
			pongWait = defaultPongWait
		}
		
		pingPeriod, err = time.ParseDuration(backendConfig.WebSocket.PingPeriod)
		if err != nil {
			debug.Warning("Failed to parse PingPeriod from backend: %v, using default", err)
			pingPeriod = defaultPingPeriod
		}
		
		debug.Info("Using backend WebSocket configuration")
	} else {
		// Fall back to defaults if no backend config
		debug.Warning("No backend configuration available, using defaults")
		writeWait = defaultWriteWait
		pongWait = defaultPongWait
		pingPeriod = defaultPingPeriod
	}
	
	debug.Info("WebSocket timing configuration initialized:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)
}

// Connection represents a WebSocket connection to the server
type Connection struct {
	// The WebSocket connection
	ws *websocket.Conn

	// URL configuration for the connection
	urlConfig *config.URLConfig

	// Hardware monitor
	hwMonitor *hardware.Monitor

	// Channel for all outbound messages
	outbound chan *WSMessage

	// Channel to signal connection closure
	done chan struct{}

	// Atomic flag to track connection status
	isConnected atomic.Bool

	// TLS configuration
	tlsConfig *tls.Config

	// File synchronization
	fileSync *filesync.FileSync

	// Download manager for file downloads
	downloadManager *filesync.DownloadManager

	// Sync status tracking
	syncStatus      string
	syncMutex       sync.RWMutex
	// Note: File download tracking now handled by downloadManager.GetDownloadStats()

	// Job manager - initialized externally and set via SetJobManager
	jobManager JobManager

	// Preferred binary version for device detection and operations
	preferredBinaryVersion int64
	binaryMutex            sync.RWMutex

	// Mutex for write synchronization
	writeMux sync.Mutex

	// Once for ensuring single close
	closeOnce sync.Once

	// Atomic flag to track if outbound channel is closed
	channelClosed atomic.Bool

	// Message buffer for handling disconnections
	messageBuffer *buffer.MessageBuffer
	
	// Agent ID for buffer identification
	agentID int

	// Device detection tracking
	devicesDetected       bool
	detectionInProgress   bool
	deviceMutex           sync.Mutex

	// Task completion ACK tracking (GH Issue #12)
	completionAckChan   chan *TaskCompleteAckPayload
	completionAckMu     sync.RWMutex
	pendingCompletionID string
}

// JobManager interface defines the methods required for job management
type JobManager interface {
	ProcessJobAssignment(ctx context.Context, assignmentData []byte) error
	StopJob(taskID string) error
	RunManualBenchmark(ctx context.Context, binaryPath string, hashType int, attackMode int) (*jobs.BenchmarkResult, error)
	ForceCleanup() error
	// GH Issue #12: State sync support
	GetState() (jobs.TaskState, string)
	GetJobStatus(taskID string) (*jobs.JobExecution, error)
	GetCompletionPending() (bool, string)
}

// isCertificateError checks if an error is related to certificate verification
func isCertificateError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := err.Error()
	certErrorPatterns := []string{
		"x509:",
		"certificate",
		"unknown authority",
		"certificate verify failed",
		"tls:",
		"bad certificate",
		"certificate required",
		"unknown certificate authority",
		"certificate has expired",
		"certificate is not valid",
	}
	
	for _, pattern := range certErrorPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}
	
	// Check nested errors
	if urlErr, ok := err.(*url.Error); ok && urlErr.Err != nil {
		return isCertificateError(urlErr.Err)
	}
	
	return false
}

// certificatesExist checks if all required certificates exist
func certificatesExist() bool {
	caPath := filepath.Join(config.GetConfigDir(), "ca.crt")
	clientCertPath := filepath.Join(config.GetConfigDir(), "client.crt")
	clientKeyPath := filepath.Join(config.GetConfigDir(), "client.key")
	
	if _, err := os.Stat(caPath); os.IsNotExist(err) {
		debug.Info("CA certificate not found")
		return false
	}
	if _, err := os.Stat(clientCertPath); os.IsNotExist(err) {
		debug.Info("Client certificate not found")
		return false
	}
	if _, err := os.Stat(clientKeyPath); os.IsNotExist(err) {
		debug.Info("Client key not found")
		return false
	}
	
	return true
}


// RenewCertificates downloads new certificates using the API key
func RenewCertificates(urlConfig *config.URLConfig) error {
	debug.Info("Starting certificate renewal process")
	
	// First, download the latest CA certificate
	if err := downloadCACertificate(urlConfig); err != nil {
		return fmt.Errorf("failed to download CA certificate: %w", err)
	}
	
	// Load API key and agent ID
	apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
	if err != nil {
		debug.Error("Failed to load API key for certificate renewal: %v", err)
		return fmt.Errorf("failed to load API key: %w", err)
	}
	
	// Request new client certificates
	// Parse base URL to get host
	parsedURL, err := url.Parse(urlConfig.BaseURL)
	if err != nil {
		debug.Error("Failed to parse base URL: %v", err)
		return fmt.Errorf("failed to parse base URL: %w", err)
	}
	host := parsedURL.Hostname()
	
	renewURL := fmt.Sprintf("http://%s:%s/api/agent/renew-certificates", host, urlConfig.HTTPPort)
	debug.Info("Requesting new client certificates from: %s", renewURL)
	
	req, err := http.NewRequest("POST", renewURL, nil)
	if err != nil {
		debug.Error("Failed to create certificate renewal request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	resp, err := client.Do(req)
	if err != nil {
		debug.Error("Failed to request certificate renewal: %v", err)
		return fmt.Errorf("failed to request certificate renewal: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debug.Error("Certificate renewal failed: status %d, body: %s", resp.StatusCode, string(body))
		return fmt.Errorf("certificate renewal failed: status %d", resp.StatusCode)
	}
	
	// Parse response
	var renewalResp struct {
		ClientCertificate string `json:"client_certificate"`
		ClientKey         string `json:"client_key"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&renewalResp); err != nil {
		debug.Error("Failed to decode certificate renewal response: %v", err)
		return fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Save client certificate
	clientCertPath := filepath.Join(config.GetConfigDir(), "client.crt")
	if err := os.WriteFile(clientCertPath, []byte(renewalResp.ClientCertificate), 0644); err != nil {
		debug.Error("Failed to save client certificate: %v", err)
		return fmt.Errorf("failed to save client certificate: %w", err)
	}
	
	// Save client key
	clientKeyPath := filepath.Join(config.GetConfigDir(), "client.key")
	if err := os.WriteFile(clientKeyPath, []byte(renewalResp.ClientKey), 0600); err != nil {
		debug.Error("Failed to save client key: %v", err)
		return fmt.Errorf("failed to save client key: %w", err)
	}
	
	debug.Info("Successfully renewed and saved certificates")
	return nil
}

// loadCACertificate loads the CA certificate from disk
func loadCACertificate(urlConfig *config.URLConfig) (*x509.CertPool, error) {
	debug.Info("Loading CA certificate")
	certPool := x509.NewCertPool()

	// Try to load from disk
	certPath := filepath.Join(config.GetConfigDir(), "ca.crt")
	if _, err := os.Stat(certPath); err == nil {
		debug.Info("Found existing CA certificate at: %s", certPath)
		certData, err := os.ReadFile(certPath)
		if err != nil {
			debug.Error("Failed to read CA certificate: %v", err)
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		if !certPool.AppendCertsFromPEM(certData) {
			debug.Error("Failed to parse CA certificate")
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		debug.Info("Successfully loaded CA certificate from disk")
		return certPool, nil
	}

	debug.Error("CA certificate not found at: %s", certPath)
	return nil, fmt.Errorf("CA certificate not found")
}

// loadClientCertificate loads the client certificate and key from disk
func loadClientCertificate() (tls.Certificate, error) {
	debug.Info("Loading client certificate")
	certPath := filepath.Join(config.GetConfigDir(), "client.crt")
	keyPath := filepath.Join(config.GetConfigDir(), "client.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		debug.Error("Failed to load client certificate: %v", err)
		return tls.Certificate{}, fmt.Errorf("failed to load client certificate: %w", err)
	}

	debug.Info("Successfully loaded client certificate")
	return cert, nil
}

// NewConnection creates a new WebSocket connection instance
func NewConnection(urlConfig *config.URLConfig) (*Connection, error) {
	debug.Info("Creating new WebSocket connection")

	// Fetch backend configuration for WebSocket timing
	backendConfig, err := fetchBackendConfig(urlConfig)
	if err != nil {
		debug.Warning("Failed to fetch backend configuration: %v, will use defaults", err)
		// Continue with defaults if fetch fails
	}

	// Initialize timing configuration with backend config or defaults
	initTimingConfig(backendConfig)

	// Get data directory for hardware monitor
	cfg := config.NewConfig()

	// Initialize hardware monitor (real or mock based on TEST_MODE)
	var hwMonitor *hardware.Monitor
	if os.Getenv("TEST_MODE") == "true" {
		debug.Info("TEST_MODE enabled, using mock hardware monitor")
		hwMonitor = hardware.NewMonitorFromMock(hardware.NewMockMonitor())
	} else {
		var monitorErr error
		hwMonitor, monitorErr = hardware.NewMonitor(cfg.DataDirectory)
		if monitorErr != nil {
			debug.Error("Failed to create hardware monitor: %v", monitorErr)
			return nil, fmt.Errorf("failed to create hardware monitor: %w", monitorErr)
		}
	}

	// Check if certificates exist, if not try to renew them
	if !certificatesExist() {
		debug.Info("Certificates missing, attempting to renew")
		if err := RenewCertificates(urlConfig); err != nil {
			debug.Error("Failed to renew certificates: %v", err)
			return nil, fmt.Errorf("failed to renew certificates: %w", err)
		}
	}

	// Load CA certificate
	certPool, err := loadCACertificate(urlConfig)
	if err != nil {
		debug.Error("Failed to load CA certificate: %v", err)
		return nil, fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Load client certificate
	clientCert, err := loadClientCertificate()
	if err != nil {
		debug.Error("Failed to load client certificate: %v", err)
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Create TLS configuration
	tlsConfig := &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	conn := &Connection{
		urlConfig:         urlConfig,
		hwMonitor:         hwMonitor,
		outbound:          make(chan *WSMessage, 4096),
		done:              make(chan struct{}),
		tlsConfig:         tlsConfig,
		syncStatus:        "pending",
		completionAckChan: make(chan *TaskCompleteAckPayload, 1), // Buffer 1 ACK (GH Issue #12)
	}

	// Download manager will be initialized when file sync is set up
	return conn, nil
}

// connect establishes a WebSocket connection to the server
func (c *Connection) connect() error {
	debug.Info("Starting WebSocket connection attempt")

	// Refetch backend configuration on each connection attempt
	// This ensures we always have the latest WebSocket timing
	backendConfig, err := fetchBackendConfig(c.urlConfig)
	if err != nil {
		debug.Warning("Failed to fetch backend configuration on reconnect: %v, using existing timing", err)
		// Continue with existing timing if fetch fails
	} else {
		// Update timing configuration with fresh backend config
		initTimingConfig(backendConfig)
		debug.Info("Updated WebSocket timing from backend for reconnection")
	}

	// Load API key and agent ID
	apiKey, agentIDStr, err := auth.LoadAgentKey(config.GetConfigDir())
	if err != nil {
		debug.Error("Failed to load API key: %v", err)
		return fmt.Errorf("failed to load API key: %w", err)
	}
	debug.Info("Successfully loaded API key")
	
	// Convert agent ID to int for internal use
	agentIDInt := 0
	if agentIDStr != "" {
		if _, err := fmt.Sscanf(agentIDStr, "%d", &agentIDInt); err != nil {
			debug.Warning("Failed to parse agent ID as integer: %v", err)
		}
	}
	c.agentID = agentIDInt
	
	// Initialize message buffer if not already initialized
	if c.messageBuffer == nil && c.agentID > 0 {
		cfg := config.NewConfig()
		if mb, err := buffer.NewMessageBuffer(cfg.DataDirectory, c.agentID); err != nil {
			debug.Error("Failed to create message buffer: %v", err)
		} else {
			c.messageBuffer = mb
			debug.Info("Message buffer initialized for agent %d", c.agentID)
			
			// Send any buffered messages from previous sessions
			c.sendBufferedMessages()
		}
	}

	// Get WebSocket URL from config
	wsURL := c.urlConfig.GetWebSocketURL()
	debug.Info("Generated WebSocket URL: %s", wsURL)

	// Parse server URL
	u, err := url.Parse(wsURL)
	if err != nil {
		debug.Error("Invalid server URL: %v", err)
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// Add agent ID to query parameters
	q := u.Query()
	u.RawQuery = q.Encode()
	debug.Info("Attempting WebSocket connection to: %s", u.String())
	debug.Debug("Connection details - Protocol: %s, Host: %s, Path: %s, Query: %s",
		u.Scheme, u.Host, u.Path, u.RawQuery)

	// Setup headers with API key
	header := http.Header{}
	header.Set("X-API-Key", apiKey)
	header.Set("X-Agent-ID", agentIDStr)

	// Configure WebSocket dialer with TLS
	dialer := websocket.Dialer{
		WriteBufferSize:  maxMessageSize,
		ReadBufferSize:   maxMessageSize,
		HandshakeTimeout: writeWait,
		TLSClientConfig:  c.tlsConfig,
	}

	debug.Info("Initiating WebSocket connection with timing configuration:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)
	debug.Info("- TLS Enabled: %v", c.tlsConfig != nil)
	if c.tlsConfig != nil {
		debug.Debug("TLS Configuration:")
		debug.Debug("- Client Certificates: %d", len(c.tlsConfig.Certificates))
		debug.Debug("- RootCAs: %v", c.tlsConfig.RootCAs != nil)
	}

	ws, resp, err := dialer.Dial(u.String(), header)
	if err != nil {
		if resp != nil {
			debug.Error("WebSocket connection failed with status: %d", resp.StatusCode)
			debug.Debug("Response headers: %v", resp.Header)
			body, _ := io.ReadAll(resp.Body)
			debug.Debug("Response body: %s", string(body))
			resp.Body.Close()
		} else {
			debug.Error("WebSocket connection failed with no response: %v", err)
			debug.Debug("Error type: %T", err)
			
			// Check if this is a certificate verification error
			if isCertificateError(err) {
				debug.Info("Certificate verification error detected, attempting to renew certificates")
				if renewErr := RenewCertificates(c.urlConfig); renewErr != nil {
					debug.Error("Failed to renew certificates: %v", renewErr)
					return fmt.Errorf("certificate renewal failed: %w", renewErr)
				}
				
				// Reload certificates after renewal
				debug.Info("Reloading certificates after renewal")
				certPool, loadErr := loadCACertificate(c.urlConfig)
				if loadErr != nil {
					debug.Error("Failed to reload CA certificate: %v", loadErr)
					return fmt.Errorf("failed to reload CA certificate: %w", loadErr)
				}
				
				clientCert, loadErr := loadClientCertificate()
				if loadErr != nil {
					debug.Error("Failed to reload client certificate: %v", loadErr)
					return fmt.Errorf("failed to reload client certificate: %w", loadErr)
				}
				
				// Update TLS configuration
				c.tlsConfig.RootCAs = certPool
				c.tlsConfig.Certificates = []tls.Certificate{clientCert}
				
				// Update dialer with new TLS config
				dialer.TLSClientConfig = c.tlsConfig
				
				// Retry connection with new certificates
				debug.Info("Retrying connection with renewed certificates")
				ws, resp, err = dialer.Dial(u.String(), header)
				if err != nil {
					if resp != nil {
						debug.Error("WebSocket connection still failed after renewal with status: %d", resp.StatusCode)
						body, _ := io.ReadAll(resp.Body)
						debug.Debug("Response body: %s", string(body))
						resp.Body.Close()
					}
					return fmt.Errorf("connection failed after certificate renewal: %w", err)
				}
				// Connection successful after renewal
				debug.Info("Successfully connected after certificate renewal")
			} else {
				// Not a certificate error
				return fmt.Errorf("failed to connect to WebSocket server: %w", err)
			}
		}
		
		if err != nil {
			return fmt.Errorf("failed to connect to WebSocket server: %w", err)
		}
	}

	c.ws = ws
	debug.Info("Successfully established WebSocket connection")
	console.Success("WebSocket connection established")
	c.isConnected.Store(true)
	
	// Device detection is done at agent startup, not after connection
	// This prevents running hashcat -I during active jobs after reconnections
	
	return nil
}

// maintainConnection maintains the WebSocket connection with exponential backoff
func (c *Connection) maintainConnection() {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second // Capped at 30 seconds for faster reconnection
	multiplier := 2.0
	attempt := 1

	debug.Info("Starting connection maintenance routine")

	for {
		select {
		case <-c.done:
			debug.Info("Connection maintenance stopped")
			return
		default:
			if !c.isConnected.Load() {
				debug.Info("Connection state: disconnected")
				debug.Info("Reconnection attempt %d - Waiting %v before retry", attempt, backoff)
				if attempt == 1 {
					console.Warning("Connection lost, reconnecting...")
				} else if attempt % 5 == 0 {
					console.Warning("Still trying to reconnect (attempt %d)...", attempt)
				}
				time.Sleep(backoff)

				if err := c.connect(); err != nil {
					debug.Error("Reconnection attempt %d failed: %v", attempt, err)
					nextBackoff := time.Duration(float64(backoff) * multiplier)
					if nextBackoff > maxBackoff {
						nextBackoff = maxBackoff
					}
					debug.Info("Increasing backoff from %v to %v (max: %v)", backoff, nextBackoff, maxBackoff)
					backoff = nextBackoff
					attempt++
				} else {
					debug.Info("Reconnection successful after %d attempts - Resetting backoff", attempt)
					console.Success("Reconnected to backend successfully")
					backoff = 1 * time.Second
					attempt = 1
					
					// Reinitialize channels before starting pumps
					c.reinitializeChannels()
					
					debug.Info("Starting read and write pumps")
					go c.readPump()
					go c.writePump()

					// Send current task status after reconnection
					go c.sendCurrentTaskStatus()
					// Also send debug status report (GH Issue #23)
					go c.sendDebugStatusReport()
				}
			} else {
				// debug.Debug("Connection state: connected") // Commented out to reduce log spam
			}
			time.Sleep(time.Second)
		}
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Connection) readPump() {
	defer func() {
		debug.Info("ReadPump closing, marking connection as disconnected")
		c.isConnected.Store(false)
		c.Close()
	}()

	debug.Info("Starting readPump with timing configuration:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)

	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))

	// Set handlers for ping/pong
	c.ws.SetPingHandler(func(appData string) error {
		debug.Info("Received ping from server, sending pong")
		err := c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			debug.Error("Failed to set read deadline: %v", err)
			return err
		}
		// Send pong response immediately
		err = c.ws.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(writeWait))
		if err != nil {
			debug.Error("Failed to send pong: %v", err)
			c.isConnected.Store(false)
			return err
		}
		debug.Info("Successfully sent pong response")
		return nil
	})

	c.ws.SetPongHandler(func(string) error {
		debug.Info("Received pong from server")
		err := c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			debug.Error("Failed to set read deadline: %v", err)
			c.isConnected.Store(false)
			return err
		}
		debug.Info("Successfully updated read deadline after pong")
		return nil
	})

	debug.Info("Ping/Pong handlers configured")

	for {
		var msg WSMessage
		err := c.ws.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				debug.Error("Unexpected WebSocket close error: %v", err)
			} else {
				debug.Info("WebSocket connection closed: %v", err)
			}
			c.isConnected.Store(false)
			break
		}

		// Handle different message types
		switch msg.Type {
		case WSTypeHeartbeat:
			// Send heartbeat response
			response := WSMessage{
				Type:      WSTypeHeartbeat,
				Timestamp: time.Now(),
			}
			if err := c.ws.WriteJSON(response); err != nil {
				debug.Error("Failed to send heartbeat response: %v", err)
			}
		case WSTypeMetrics:
			// Server requested metrics update
			// TODO: Implement metrics collection and sending
			// This will be implemented later when we add the metrics collection functionality
			debug.Info("Metrics update requested but not yet implemented")
		case WSTypeHardwareInfo:
			// Server requested hardware info update
			// Detect devices and send the result
			detectionResult, err := c.hwMonitor.DetectDevices()
			if err != nil {
				debug.Error("Failed to detect devices: %v", err)
				continue
			}

			// Marshal hardware info to JSON for the payload
			hwInfoJSON, err := json.Marshal(detectionResult)
			if err != nil {
				debug.Error("Failed to marshal hardware info: %v", err)
				continue
			}

			response := WSMessage{
				Type:      WSTypeHardwareInfo,
				Payload:   hwInfoJSON,
				Timestamp: time.Now(),
			}
			if err := c.ws.WriteJSON(response); err != nil {
				debug.Error("Failed to send hardware info: %v", err)
			}
		case WSTypeConfigUpdate:
			// Server sent configuration update
			debug.Info("Received configuration update")

			// Parse the configuration payload
			var configPayload map[string]interface{}
			if err := json.Unmarshal(msg.Payload, &configPayload); err != nil {
				debug.Error("Failed to parse configuration update: %v", err)
				continue
			}

			// Check for preferred_binary_version
			if preferredBinary, ok := configPayload["preferred_binary_version"]; ok {
				if binaryID, ok := preferredBinary.(float64); ok { // JSON numbers decode as float64
					c.SetPreferredBinaryVersion(int64(binaryID))
					// Also set it on the hardware monitor for device detection
					if c.hwMonitor != nil {
						c.hwMonitor.SetPreferredBinaryVersion(int64(binaryID))
					}
					debug.Info("Set preferred binary version to %d", int64(binaryID))

					// Only trigger detection if the preferred binary is already available
					// If not, detection will be triggered after file sync downloads it
					if c.hwMonitor != nil && c.hwMonitor.HasPreferredBinary() {
						debug.Info("Preferred binary version %d is available, triggering device detection", int64(binaryID))
						c.TryDetectDevicesIfNeeded()
					} else {
						debug.Info("Preferred binary version %d not yet available, skipping detection (will run after download)", int64(binaryID))
					}
				}
			}

			debug.Info("Configuration update processed successfully")
		case WSTypeFileSyncRequest:
			// Server requested file list
			debug.Info("Received file sync request")

			// Parse the request payload
			var requestPayload FileSyncRequestPayload
			if err := json.Unmarshal(msg.Payload, &requestPayload); err != nil {
				debug.Error("Failed to parse file sync request: %v", err)
				continue
			}

			// Handle file sync asynchronously to avoid blocking the read pump
			go c.handleFileSyncAsync(requestPayload)
			debug.Info("Started async file sync operation")

		case WSTypeFileSyncCommand:
			// Server sent file sync command
			debug.Info("Received file sync command")

			// Parse the command payload
			var commandPayload FileSyncCommandPayload
			if err := json.Unmarshal(msg.Payload, &commandPayload); err != nil {
				debug.Error("Failed to parse file sync command: %v", err)
				continue
			}

			// Show console message about file sync
			if len(commandPayload.Files) > 0 {
				console.Status("Starting file synchronization (%d files)...", len(commandPayload.Files))
			}

			// Initialize file sync if not already done
			if c.fileSync == nil {
				// Get credentials from the same place we use for WebSocket connection
				apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
				if err != nil {
					debug.Error("Failed to load agent credentials: %v", err)
					continue
				}

				// Store agent ID for later use
				c.agentID = auth.ParseAgentID(agentID)

				// Initialize file sync and download manager
				if err := c.initializeFileSync(apiKey, agentID); err != nil {
					debug.Error("Failed to initialize file sync: %v", err)
					continue
				}
			}

			// Ensure download manager is initialized even if fileSync already exists
			if c.downloadManager == nil && c.fileSync != nil {
				debug.Info("Initializing download manager with existing file sync")
				c.downloadManager = filesync.NewDownloadManager(c.fileSync, 3)
				go c.monitorDownloadProgress()
			}

			// Pre-check: Look for binary archives that need extraction
			// This ensures we extract any archives that were downloaded but not extracted
			if err := c.checkAndExtractBinaryArchives(); err != nil {
				debug.Error("Error during pre-sync binary archive check: %v", err)
				// Continue anyway, this is just a pre-check
			}

			// Check if binaries are being downloaded
			hasBinaries := false
			for _, file := range commandPayload.Files {
				if file.FileType == "binary" {
					hasBinaries = true
					break
				}
			}
			
			// Send sync started message
			c.sendSyncStarted(len(commandPayload.Files))

			// Queue downloads using the download manager
			// Note: Download manager tracks all file states internally
			ctx := context.Background()
			if c.downloadManager != nil {
				for _, file := range commandPayload.Files {
					// Check if already downloading to prevent duplicates
					if c.downloadManager.IsDownloading(file) {
						debug.Info("File %s is already downloading, skipping duplicate", file.Name)
						continue
					}

					debug.Info("Queueing download for file: %s (%s)", file.Name, file.FileType)
					if err := c.downloadManager.QueueDownload(ctx, file); err != nil {
						debug.Error("Failed to queue download for %s: %v", file.Name, err)
					}
				}
			} else {
				debug.Error("Download manager is not initialized, cannot queue downloads")
			}

			debug.Info("Queued %d files for download", len(commandPayload.Files))

			// Check if all files were already available (no new downloads needed)
			// This happens when download manager verified files exist on disk
			if c.downloadManager != nil {
				total, pending, downloading, _, _ := c.downloadManager.GetDownloadStats()
				if pending == 0 && downloading == 0 && total > 0 {
					// All files were already synced - immediately complete sync
					debug.Info("All %d files already synced (verified on disk), sending sync_completed immediately", total)
					c.sendSyncCompleted()
				}
			}

			// If binaries were downloaded, trigger device detection after downloads complete
			if hasBinaries && c.downloadManager != nil {
				go func() {
					// Wait for download manager to complete all downloads
					c.downloadManager.Wait()
					debug.Info("Binary downloads complete, checking if device detection is needed")
					c.TryDetectDevicesIfNeeded()
				}()
			}

		case WSTypeTaskAssignment:
			// Server sent a job task assignment
			debug.Info("Received task assignment")

			// Try to extract task ID for console message
			var taskInfo struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(msg.Payload, &taskInfo); err == nil && taskInfo.TaskID != "" {
				console.Status("Task received: %s", taskInfo.TaskID)
			} else {
				console.Status("Task received")
			}

			if c.jobManager == nil {
				debug.Error("Job manager not initialized, cannot process task assignment")
				continue
			}

			// Ensure file sync is initialized before processing job
			if c.fileSync == nil {
				// Get credentials from the same place we use for WebSocket connection
				apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
				if err != nil {
					debug.Error("Failed to load agent credentials: %v", err)
					continue
				}

				// Store agent ID for later use
				c.agentID = auth.ParseAgentID(agentID)

				// Initialize file sync and download manager
				if err := c.initializeFileSync(apiKey, agentID); err != nil {
					debug.Error("Failed to initialize file sync: %v", err)
					continue
				}
			}

			// Ensure download manager is initialized even if fileSync already exists
			if c.downloadManager == nil && c.fileSync != nil {
				debug.Info("Initializing download manager with existing file sync")
				c.downloadManager = filesync.NewDownloadManager(c.fileSync, 3)
				go c.monitorDownloadProgress()
			}

			// Set the file sync in job manager
			if jobMgr, ok := c.jobManager.(*jobs.JobManager); ok {
				jobMgr.SetFileSync(c.fileSync)
			}

			// Process job assignment asynchronously to prevent blocking readPump
			// This is critical: blocking readPump during long downloads (72+ seconds)
			// prevents the agent from responding to server pings, causing WebSocket timeout
			payload := msg.Payload // Capture payload for goroutine
			go func() {
				// Use context without timeout for job execution
				// Jobs should run until completion, not be limited by arbitrary timeouts
				ctx := context.Background()

				if err := c.jobManager.ProcessJobAssignment(ctx, payload); err != nil {
					debug.Error("Failed to process job assignment: %v", err)

					// Extract task ID from the assignment payload to report failure to backend
					var assignment struct {
						TaskID string `json:"task_id"`
					}
					if unmarshalErr := json.Unmarshal(payload, &assignment); unmarshalErr == nil && assignment.TaskID != "" {
						// Send failure status to backend so it can update task state
						failureStatus := &jobs.JobStatus{
							TaskID:       assignment.TaskID,
							Status:       "failed",
							ErrorMessage: fmt.Sprintf("Failed to prepare task: %v", err),
						}
						if sendErr := c.SendJobStatus(failureStatus); sendErr != nil {
							debug.Error("Failed to send task failure status to backend: %v", sendErr)
						} else {
							debug.Info("Sent task failure status to backend for task %s", assignment.TaskID)
						}
					} else {
						debug.Error("Could not extract task ID from failed assignment to report failure")
					}
				} else {
					debug.Info("Successfully processed job assignment")
				}
			}()

		case WSTypeJobStop:
			// Server requested to stop a job
			debug.Info("Received job stop command")

			if c.jobManager == nil {
				debug.Error("Job manager not initialized, cannot process job stop")
				continue
			}

			var stopPayload JobStopPayload
			if err := json.Unmarshal(msg.Payload, &stopPayload); err != nil {
				debug.Error("Failed to parse job stop payload: %v", err)
				continue
			}

			// Display user-visible notification about task being stopped
			if stopPayload.Reason != "" {
				console.Warning("Task stopped by server: %s (Reason: %s)", stopPayload.TaskID, stopPayload.Reason)
			} else {
				console.Warning("Task stopped by server: %s", stopPayload.TaskID)
			}

			var stopped bool
			var stopMessage string
			if err := c.jobManager.StopJob(stopPayload.TaskID); err != nil {
				debug.Error("Failed to stop job %s: %v", stopPayload.TaskID, err)
				console.Error("Failed to stop task %s: %v", stopPayload.TaskID, err)
				stopped = false
				stopMessage = err.Error()
			} else {
				debug.Info("Successfully stopped job %s", stopPayload.TaskID)
				console.Success("Task %s stopped successfully", stopPayload.TaskID)
				stopped = true
			}

			// Send stop ACK back to backend (GH Issue #12)
			if stopPayload.StopID != "" {
				c.sendTaskStopAck(stopPayload.TaskID, stopPayload.StopID, stopped, stopMessage)
			}

		case WSTypeTaskCompleteAck:
			// Server acknowledged task completion (GH Issue #12)
			debug.Info("Received task complete ACK")

			var ackPayload TaskCompleteAckPayload
			if err := json.Unmarshal(msg.Payload, &ackPayload); err != nil {
				debug.Error("Failed to parse task complete ACK: %v", err)
				continue
			}

			debug.Debug("Task completion acknowledged by backend: task=%s success=%v message=%s",
				ackPayload.TaskID, ackPayload.Success, ackPayload.Message)

			// Send to ACK channel if we're waiting for it
			c.completionAckMu.RLock()
			pendingID := c.pendingCompletionID
			c.completionAckMu.RUnlock()

			if pendingID == ackPayload.TaskID {
				select {
				case c.completionAckChan <- &ackPayload:
					debug.Debug("Delivered ACK to waiting goroutine for task %s", ackPayload.TaskID)
				default:
					debug.Warning("ACK channel full or not waiting for task %s", ackPayload.TaskID)
				}
			} else {
				debug.Debug("Received ACK for task %s but waiting for %s (or not waiting)", ackPayload.TaskID, pendingID)
			}

		case WSTypeForceCleanup:
			// Server requested to force cleanup all hashcat processes
			debug.Info("Received force cleanup command")
			
			if c.jobManager == nil {
				debug.Error("Job manager not initialized, cannot process force cleanup")
				continue
			}
			
			// Force cleanup all hashcat processes
			if err := c.jobManager.ForceCleanup(); err != nil {
				debug.Error("Failed to force cleanup: %v", err)
			} else {
				debug.Info("Successfully completed force cleanup")
			}

		case WSTypeBenchmarkRequest:
			// Server requested a benchmark (now with full job configuration for real-world speed test)
			debug.Info("Received benchmark request")

			if c.jobManager == nil {
				debug.Error("Job manager not initialized, cannot process benchmark request")
				continue
			}

			var benchmarkPayload BenchmarkRequest
			if err := json.Unmarshal(msg.Payload, &benchmarkPayload); err != nil {
				debug.Error("Failed to parse benchmark request: %v", err)
				continue
			}

			// Run benchmark in a goroutine to not block message processing
			go func() {
				debug.Info("Running speed test for task %s, hash type %d, attack mode %d", 
					benchmarkPayload.TaskID, benchmarkPayload.HashType, benchmarkPayload.AttackMode)

				// Ensure file sync is initialized before processing benchmark
				if c.fileSync == nil {
					dataDirs, err := config.GetDataDirs()
					if err != nil {
						debug.Error("Failed to get data directories: %v", err)
						// Send failure result
						resultPayload := map[string]interface{}{
							"job_execution_id": benchmarkPayload.JobExecutionID,
							"attack_mode":      benchmarkPayload.AttackMode,
							"hash_type":        benchmarkPayload.HashType,
							"speed":            int64(0),
							"device_speeds":    []jobs.DeviceSpeed{},
							"success":          false,
							"error":            fmt.Sprintf("Failed to get data directories: %v", err),
						}
						payloadBytes, _ := json.Marshal(resultPayload)
						response := WSMessage{
							Type:      WSTypeBenchmarkResult,
							Payload:   payloadBytes,
							Timestamp: time.Now(),
						}
						if err := c.ws.WriteJSON(response); err != nil {
							debug.Error("Failed to send benchmark failure result: %v", err)
						}
						return
					}

					// Get credentials from the same place we use for WebSocket connection
					apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
					if err != nil {
						debug.Error("Failed to load agent credentials: %v", err)
						// Send failure result
						resultPayload := map[string]interface{}{
							"job_execution_id": benchmarkPayload.JobExecutionID,
							"attack_mode":      benchmarkPayload.AttackMode,
							"hash_type":        benchmarkPayload.HashType,
							"speed":            int64(0),
							"device_speeds":    []jobs.DeviceSpeed{},
							"success":          false,
							"error":            fmt.Sprintf("Failed to load agent credentials: %v", err),
						}
						payloadBytes, _ := json.Marshal(resultPayload)
						response := WSMessage{
							Type:      WSTypeBenchmarkResult,
							Payload:   payloadBytes,
							Timestamp: time.Now(),
						}
						if err := c.ws.WriteJSON(response); err != nil {
							debug.Error("Failed to send benchmark failure result: %v", err)
						}
						return
					}

					c.fileSync, err = filesync.NewFileSync(c.urlConfig, dataDirs, apiKey, agentID)
					if err != nil {
						debug.Error("Failed to initialize file sync: %v", err)
						// Send failure result
						resultPayload := map[string]interface{}{
							"job_execution_id": benchmarkPayload.JobExecutionID,
							"attack_mode":      benchmarkPayload.AttackMode,
							"hash_type":        benchmarkPayload.HashType,
							"speed":            int64(0),
							"device_speeds":    []jobs.DeviceSpeed{},
							"success":          false,
							"error":            fmt.Sprintf("Failed to initialize file sync: %v", err),
						}
						payloadBytes, _ := json.Marshal(resultPayload)
						response := WSMessage{
							Type:      WSTypeBenchmarkResult,
							Payload:   payloadBytes,
							Timestamp: time.Now(),
						}
						if err := c.ws.WriteJSON(response); err != nil {
							debug.Error("Failed to send benchmark failure result: %v", err)
						}
						return
					}
				}

				// Always re-download hashlist for benchmarks to ensure fresh data after cracks
				if benchmarkPayload.HashlistID > 0 {
					hashlistFileName := fmt.Sprintf("%d.hash", benchmarkPayload.HashlistID)
					dataDirs, _ := config.GetDataDirs()
					localPath := filepath.Join(dataDirs.Hashlists, hashlistFileName)

					// Remove existing hashlist if it exists to force fresh download
					if _, err := os.Stat(localPath); err == nil {
						debug.Info("Removing existing hashlist %d to download fresh copy for benchmark", benchmarkPayload.HashlistID)
						if err := os.Remove(localPath); err != nil {
							debug.Warning("Failed to remove existing hashlist: %v", err)
						}
					}

					// Always download for benchmarks to get current hash count
					debug.Info("Downloading hashlist %d for benchmark...", benchmarkPayload.HashlistID)

					// Create FileInfo for download
					// AttackMode is passed through - download function picks right endpoint (mode 9 = original file)
					fileInfo := &filesync.FileInfo{
						Name:       hashlistFileName,
						FileType:   "hashlist",
						ID:         int(benchmarkPayload.HashlistID),
						MD5Hash:    "", // Empty hash means skip verification
						AttackMode: benchmarkPayload.AttackMode,
					}

					// Download with timeout
					downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer downloadCancel()

					if err := c.fileSync.DownloadFileFromInfo(downloadCtx, fileInfo); err != nil {
					debug.Error("Failed to download hashlist for benchmark: %v", err)
					// Send failure result
					resultPayload := map[string]interface{}{
						"job_execution_id": benchmarkPayload.JobExecutionID,
						"attack_mode":      benchmarkPayload.AttackMode,
						"hash_type":        benchmarkPayload.HashType,
						"speed":            int64(0),
						"device_speeds":    []jobs.DeviceSpeed{},
						"success":          false,
						"error":            fmt.Sprintf("Failed to download hashlist: %v", err),
					}
					payloadBytes, _ := json.Marshal(resultPayload)
					response := WSMessage{
						Type:      WSTypeBenchmarkResult,
						Payload:   payloadBytes,
						Timestamp: time.Now(),
					}
					if err := c.ws.WriteJSON(response); err != nil {
						debug.Error("Failed to send benchmark failure result: %v", err)
					}
					return
						}
						
						// Verify the file was downloaded
						if _, err := os.Stat(localPath); err != nil {
					debug.Error("Hashlist file not found after download: %s", localPath)
					// Send failure result
					resultPayload := map[string]interface{}{
						"job_execution_id": benchmarkPayload.JobExecutionID,
						"attack_mode":      benchmarkPayload.AttackMode,
						"hash_type":        benchmarkPayload.HashType,
						"speed":            int64(0),
						"device_speeds":    []jobs.DeviceSpeed{},
						"success":          false,
						"error":            "Hashlist file not found after download",
					}
					payloadBytes, _ := json.Marshal(resultPayload)
					response := WSMessage{
						Type:      WSTypeBenchmarkResult,
						Payload:   payloadBytes,
						Timestamp: time.Now(),
					}
					if err := c.ws.WriteJSON(response); err != nil {
						debug.Error("Failed to send benchmark failure result: %v", err)
					}
					return
						}
						
						debug.Info("Successfully downloaded hashlist %d for benchmark", benchmarkPayload.HashlistID)
					}

				// Create a JobTaskAssignment from benchmark request
				assignment := &jobs.JobTaskAssignment{
					TaskID:                  benchmarkPayload.TaskID,
					HashlistID:              benchmarkPayload.HashlistID,
					HashlistPath:            benchmarkPayload.HashlistPath,
					AttackMode:              benchmarkPayload.AttackMode,
					HashType:                benchmarkPayload.HashType,
					WordlistPaths:           benchmarkPayload.WordlistPaths,
					RulePaths:               benchmarkPayload.RulePaths,
					Mask:                    benchmarkPayload.Mask,
					BinaryPath:              benchmarkPayload.BinaryPath,
					ReportInterval:          5, // Default status interval
					ExtraParameters:         benchmarkPayload.ExtraParameters,         // Agent-specific parameters
					EnabledDevices:          benchmarkPayload.EnabledDevices,           // Device list
					AssociationWordlistPath: benchmarkPayload.AssociationWordlistPath, // For mode 9
				}

				// Default test duration to 16 seconds if not specified
				testDuration := benchmarkPayload.TestDuration
				if testDuration == 0 {
					testDuration = 16
				}

				// Use configurable timeout duration, default to 180 seconds (3 minutes)
				timeoutDuration := benchmarkPayload.TimeoutDuration
				if timeoutDuration == 0 {
					timeoutDuration = 180
				}

				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutDuration)*time.Second)
				defer cancel()

				// Get the executor from job manager
				executor := c.jobManager.(*jobs.JobManager).GetExecutor()
				totalSpeed, deviceSpeeds, totalEffectiveKeyspace, err := executor.RunSpeedTest(ctx, assignment, testDuration)

				if err != nil {
					debug.Error("Speed test failed: %v", err)
					// Send failure result in the format the backend expects
					resultPayload := map[string]interface{}{
						"job_execution_id":          benchmarkPayload.JobExecutionID,
						"attack_mode":               benchmarkPayload.AttackMode,
						"hash_type":                 benchmarkPayload.HashType,
						"speed":                     int64(0),
						"device_speeds":             []jobs.DeviceSpeed{},
						"total_effective_keyspace":  int64(0),
						"success":                   false,
						"error":                     err.Error(), // Backend expects "error" not "error_message"
					}

					payloadBytes, _ := json.Marshal(resultPayload)
					response := WSMessage{
						Type:      WSTypeBenchmarkResult,
						Payload:   payloadBytes,
						Timestamp: time.Now(),
					}
					if err := c.ws.WriteJSON(response); err != nil {
						debug.Error("Failed to send benchmark failure result: %v", err)
					}
					return
				}

				// Send success result in the format the backend expects
				// The backend expects BenchmarkResultPayload which has different field names
				resultPayload := map[string]interface{}{
					"job_execution_id":          benchmarkPayload.JobExecutionID, // Include job ID for tracking
					"attack_mode":               benchmarkPayload.AttackMode,
					"hash_type":                 benchmarkPayload.HashType,
					"speed":                     totalSpeed, // Backend expects "speed" not "total_speed"
					"device_speeds":             deviceSpeeds,
					"total_effective_keyspace":  totalEffectiveKeyspace, // Hashcat's progress[1]
					"success":                   true,
				}

				payloadBytes, _ := json.Marshal(resultPayload)
				response := WSMessage{
					Type:      WSTypeBenchmarkResult,
					Payload:   payloadBytes,
					Timestamp: time.Now(),
				}
				if err := c.ws.WriteJSON(response); err != nil {
					debug.Error("Failed to send benchmark result: %v", err)
				} else {
					debug.Info("Successfully sent benchmark result: %d H/s total, effective keyspace: %d", totalSpeed, totalEffectiveKeyspace)
				}
			}()
			
		case WSTypeDeviceUpdate:
			// Server requested device update (enable/disable)
			debug.Info("Received device update request")
			
			var updatePayload types.DeviceUpdate
			if err := json.Unmarshal(msg.Payload, &updatePayload); err != nil {
				debug.Error("Failed to parse device update: %v", err)
				continue
			}
			
			// Update device status
			if err := c.hwMonitor.UpdateDeviceStatus(updatePayload.DeviceID, updatePayload.Enabled); err != nil {
				debug.Error("Failed to update device status: %v", err)
				// Send error response
				errorPayload := map[string]interface{}{
					"device_id": updatePayload.DeviceID,
					"error": err.Error(),
					"success": false,
				}
				errorJSON, _ := json.Marshal(errorPayload)
				response := WSMessage{
					Type:      WSTypeDeviceUpdate,
					Payload:   errorJSON,
					Timestamp: time.Now(),
				}
				if writeErr := c.ws.WriteJSON(response); writeErr != nil {
					debug.Error("Failed to send device update error: %v", writeErr)
				}
				continue
			}
			
			// Send success response
			successPayload := map[string]interface{}{
				"device_id": updatePayload.DeviceID,
				"enabled": updatePayload.Enabled,
				"success": true,
			}
			successJSON, _ := json.Marshal(successPayload)
			response := WSMessage{
				Type:      WSTypeDeviceUpdate,
				Payload:   successJSON,
				Timestamp: time.Now(),
			}
			if err := c.ws.WriteJSON(response); err != nil {
				debug.Error("Failed to send device update success: %v", err)
			} else {
				debug.Info("Successfully updated device %d to enabled=%v", updatePayload.DeviceID, updatePayload.Enabled)
			}

		case WSTypeBufferAck:
			// Server acknowledged buffered messages
			debug.Info("Received buffer acknowledgment")
			c.handleBufferAck(msg.Payload)

		case WSTypeRequestCrackRetransmit:
			// Server requested retransmission of outfile cracks
			debug.Info("Received request for crack retransmission")
			c.handleCrackRetransmitRequest(msg.Payload)

		case WSTypeOutfileDeleteApproved:
			// Server approved deletion of outfile
			debug.Info("Received outfile delete approval")
			c.handleOutfileDeleteApproval(msg.Payload)

		case WSTypeStateSyncRequest:
			// Server requesting state sync (GH Issue #12)
			debug.Info("Received state sync request from backend")
			c.handleStateSyncRequest(msg.Payload)

		// Diagnostics message handlers (GH Issue #23)
		case WSTypeDebugToggle:
			debug.Info("Received debug toggle command")
			c.handleDebugToggle(msg.Payload)

		case WSTypeLogRequest:
			debug.Info("Received log request")
			c.handleLogRequest(msg.Payload)

		case WSTypeLogStatusRequest:
			debug.Info("Received log status request")
			c.handleLogStatusRequest(msg.Payload)

		case WSTypeLogPurge:
			debug.Info("Received log purge command")
			c.handleLogPurge(msg.Payload)

		default:
			debug.Warning("Received unknown message type: %s", msg.Type)
		}
	}
}

// handleFileSyncAsync performs file synchronization in a separate goroutine
func (c *Connection) handleFileSyncAsync(requestPayload FileSyncRequestPayload) {
	debug.Info("Starting async file sync operation")
	startTime := time.Now()

	// Create a context with timeout for the entire operation
	// 5 minute timeout allows hashing of large wordlist files (50GB+)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Initialize file sync if not already done
	if c.fileSync == nil {
		dataDirs, err := config.GetDataDirs()
		if err != nil {
			debug.Error("Failed to get data directories: %v", err)
			return
		}

		// Get credentials from the same place we use for WebSocket connection
		apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
		if err != nil {
			debug.Error("Failed to load agent credentials: %v", err)
			return
		}

		c.fileSync, err = filesync.NewFileSync(c.urlConfig, dataDirs, apiKey, agentID)
		if err != nil {
			debug.Error("Failed to initialize file sync: %v", err)
			return
		}
		debug.Info("FileSync initialized with hash caching enabled")
	}

	// Send progress update
	progressMsg := &WSMessage{
		Type:      WSTypeFileSyncResponse,
		Payload:   json.RawMessage(`{"status":"scanning","message":"Starting directory scan..."}`),
		Timestamp: time.Now(),
	}
	if c.safeSendMessage(progressMsg, 0) {
		debug.Info("Sent file sync progress update")
	}

	// Scan directories for files
	filesByType := make(map[string][]filesync.FileInfo)
	totalFiles := 0
	
	for i, fileType := range requestPayload.FileTypes {
		// Send progress for each directory
		progressData := map[string]interface{}{
			"status": "scanning",
			"message": fmt.Sprintf("Scanning %s directory (%d/%d)...", fileType, i+1, len(requestPayload.FileTypes)),
			"progress": float64(i) / float64(len(requestPayload.FileTypes)) * 100,
		}
		progressBytes, _ := json.Marshal(progressData)
		progressMsg := &WSMessage{
			Type:      WSTypeFileSyncResponse,
			Payload:   progressBytes,
			Timestamp: time.Now(),
		}
		c.safeSendMessage(progressMsg, 0)

		// Check if context is cancelled before scanning
		select {
		case <-ctx.Done():
			debug.Warning("File sync operation timed out during scan")
			break
		default:
		}

		files, err := c.fileSync.ScanDirectory(fileType)
		if err != nil {
			debug.Error("Failed to scan %s directory: %v", fileType, err)
			continue
		}
		filesByType[fileType] = files
		totalFiles += len(files)
		debug.Info("Scanned %s directory: found %d files", fileType, len(files))
	}

	// Flatten the file list
	var allFiles []filesync.FileInfo
	for _, files := range filesByType {
		allFiles = append(allFiles, files...)
	}

	// Get agent ID
	agentID, err := GetAgentID()
	if err != nil {
		debug.Error("Failed to get agent ID: %v", err)
		return
	}

	// Prepare response
	responsePayload := FileSyncResponsePayload{
		AgentID: agentID,
		Files:   allFiles,
	}

	// Marshal response payload
	payloadBytes, err := json.Marshal(responsePayload)
	if err != nil {
		debug.Error("Failed to marshal file sync response: %v", err)
		return
	}

	// Send final response
	response := WSMessage{
		Type:      WSTypeFileSyncResponse,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	// Log payload size to monitor buffer usage
	payloadSize := len(payloadBytes)
	debug.Info("File sync response payload size: %d bytes (%.2f KB)", payloadSize, float64(payloadSize)/1024)
	if payloadSize > maxMessageSize/2 {
		debug.Warning("File sync response is large (%d bytes), approaching buffer limit of %d", payloadSize, maxMessageSize)
	}

	// Use safe send method for the response
	if !c.safeSendMessage(&response, 5000) { // 5 second timeout
		debug.Error("Failed to send file sync response: channel blocked or closed")
	} else {
		debug.Info("File sync completed in %v, sent response with %d files", time.Since(startTime), len(allFiles))
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	// Add a status update ticker that runs every minute
	statusTicker := time.NewTicker(1 * time.Minute)
	defer func() {
		debug.Info("WritePump closing, marking connection as disconnected")
		ticker.Stop()
		statusTicker.Stop()
		c.isConnected.Store(false)
		c.Close()
	}()

	debug.Info("Starting writePump with timing configuration:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)
	debug.Info("- Status Update Period: 1m")

	// Send initial status update
	if statusMsg, err := c.createAgentStatusMessage(); err != nil {
		debug.Error("Failed to create initial status update: %v", err)
	} else {
		c.writeMux.Lock()
		c.ws.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.ws.WriteJSON(statusMsg); err != nil {
			debug.Error("Failed to send initial status update: %v", err)
		}
		c.writeMux.Unlock()
	}

	for {
		select {
		case message, ok := <-c.outbound:
			if !ok {
				debug.Info("Outbound channel closed, marking as disconnected")
				c.isConnected.Store(false)
				c.writeMux.Lock()
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				c.writeMux.Unlock()
				return
			}

			// Write the message with mutex protection
			c.writeMux.Lock()
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteJSON(message); err != nil {
				debug.Error("Failed to send message type %s: %v", message.Type, err)
				c.writeMux.Unlock()
				
				// Buffer critical messages on send failure
				if c.messageBuffer != nil && c.shouldBufferMessage(message) {
					if bufferErr := c.bufferMessage(message); bufferErr != nil {
						debug.Error("Failed to buffer message: %v", bufferErr)
					} else {
						debug.Info("Buffered message type %s for later delivery", message.Type)
					}
				}
				
				c.isConnected.Store(false)
				return
			}
			c.writeMux.Unlock()
			debug.Debug("Successfully sent message type: %s", message.Type)

		case <-ticker.C:
			debug.Info("Local ticker triggered, sending ping to server")
			c.writeMux.Lock()
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				debug.Error("Failed to send ping: %v", err)
				c.writeMux.Unlock()
				c.isConnected.Store(false)
				return
			}
			c.writeMux.Unlock()
			debug.Info("Successfully sent ping to server")

		case <-statusTicker.C:
			debug.Info("Status ticker triggered, creating agent status update")
			if statusMsg, err := c.createAgentStatusMessage(); err != nil {
				debug.Error("Failed to create agent status update: %v", err)
			} else {
				// Send via safeSendMessage to avoid panic on closed channel
				if c.safeSendMessage(statusMsg, 1000) {
					debug.Info("Queued agent status update")
				} else {
					debug.Warning("Failed to queue status update: channel blocked or closed")
				}
			}

		case <-c.done:
			debug.Info("WritePump received done signal")
			return
		}
	}
}

// SendJobProgress sends job progress update to the server
func (c *Connection) SendJobProgress(progress *jobs.JobProgress) error {
	if !c.isConnected.Load() {
		return fmt.Errorf("not connected")
	}

	// Marshal progress payload to JSON
	progressJSON, err := json.Marshal(progress)
	if err != nil {
		debug.Error("Failed to marshal job progress: %v", err)
		return fmt.Errorf("failed to marshal job progress: %w", err)
	}

	// Create and send progress message
	msg := &WSMessage{
		Type:      WSTypeJobProgress,
		Payload:   progressJSON,
		Timestamp: time.Now(),
	}

	// Send via safeSendMessage with panic recovery
	if !c.safeSendMessage(msg, 5000) {
		debug.Error("Failed to queue job progress update: channel blocked or closed")
		return fmt.Errorf("failed to queue job progress update: channel blocked or closed")
	}
	debug.Debug("Queued job progress update for task %s: %d keyspace processed, %d H/s",
		progress.TaskID, progress.KeyspaceProcessed, progress.HashRate)
	return nil
}

// SendJobStatus sends job status update synchronously (blocking until sent)
func (c *Connection) SendJobStatus(status *jobs.JobStatus) error {
	if !c.isConnected.Load() {
		return fmt.Errorf("not connected")
	}

	// Marshal status payload to JSON
	statusJSON, err := json.Marshal(status)
	if err != nil {
		debug.Error("Failed to marshal job status: %v", err)
		return fmt.Errorf("failed to marshal job status: %w", err)
	}

	// Create and send status message
	msg := &WSMessage{
		Type:      WSTypeJobStatus,
		Payload:   statusJSON,
		Timestamp: time.Now(),
	}

	// Send via safeSendMessage with timeout (must be delivered)
	if !c.safeSendMessage(msg, 5000) {
		debug.Error("Failed to send job status: channel blocked or closed")
		return fmt.Errorf("failed to send status: channel blocked")
	}

	debug.Debug("Sent job status for task %s: %.2f%% complete",
		status.TaskID, status.ProgressPercent)
	return nil
}

// WaitForCompletionAck waits for a completion ACK from the backend with retry logic (GH Issue #12)
// Returns true if ACK received, false if all retries exhausted
// The taskID should be set before calling this method
func (c *Connection) WaitForCompletionAck(taskID string, timeout time.Duration, maxRetries int, resendFunc func() error) bool {
	// Set pending completion ID so ACK handler knows what we're waiting for
	c.completionAckMu.Lock()
	c.pendingCompletionID = taskID
	c.completionAckMu.Unlock()

	// Ensure we clear pending ID when done
	defer func() {
		c.completionAckMu.Lock()
		c.pendingCompletionID = ""
		c.completionAckMu.Unlock()
	}()

	// Drain any stale ACKs from channel first
	select {
	case <-c.completionAckChan:
		debug.Debug("Drained stale ACK from channel")
	default:
	}

	for retry := 0; retry <= maxRetries; retry++ {
		if retry > 0 {
			debug.Info("Retrying completion for task %s (attempt %d/%d)", taskID, retry+1, maxRetries+1)
			// Resend the completion message
			if resendFunc != nil {
				if err := resendFunc(); err != nil {
					debug.Error("Failed to resend completion for task %s: %v", taskID, err)
				}
			}
		}

		// Wait for ACK with timeout
		select {
		case ack := <-c.completionAckChan:
			if ack != nil && ack.TaskID == taskID {
				debug.Info("Received completion ACK for task %s (success=%v)", taskID, ack.Success)
				return true
			}
			debug.Warning("Received ACK for wrong task: expected %s, got %s", taskID, ack.TaskID)
		case <-time.After(timeout):
			debug.Warning("Timeout waiting for completion ACK for task %s (attempt %d/%d)",
				taskID, retry+1, maxRetries+1)
		}
	}

	debug.Warning("All retries exhausted for completion ACK of task %s, proceeding with completion_pending flag", taskID)
	return false
}

// SetCompletionPending sets the completion pending flag on the job manager (GH Issue #12)
func (c *Connection) SetCompletionPending(taskID string) {
	if jobMgr, ok := c.jobManager.(*jobs.JobManager); ok {
		jobMgr.SetCompletionPending(taskID)
		debug.Info("Set completion_pending flag for task %s", taskID)
	}
}

// sendTaskStopAck sends a stop acknowledgment back to the backend (GH Issue #12)
func (c *Connection) sendTaskStopAck(taskID, stopID string, stopped bool, message string) {
	if !c.isConnected.Load() {
		debug.Warning("Cannot send stop ACK - not connected")
		return
	}

	ackPayload := TaskStopAckPayload{
		TaskID:    taskID,
		StopID:    stopID,
		Stopped:   stopped,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}

	payloadBytes, err := json.Marshal(ackPayload)
	if err != nil {
		debug.Error("Failed to marshal stop ACK: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeTaskStopAck,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Info("Sent stop ACK for task %s (stop_id=%s, stopped=%v)", taskID, stopID, stopped)
	} else {
		debug.Warning("Failed to send stop ACK for task %s", taskID)
	}
}

// handleStateSyncRequest handles state sync requests from backend (GH Issue #12)
func (c *Connection) handleStateSyncRequest(payload json.RawMessage) {
	var request StateSyncRequestPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal state sync request: %v", err)
		return
	}

	debug.Info("Processing state sync request (request_id: %s)", request.RequestID)

	// Get current state from job manager
	response := StateSyncResponsePayload{
		RequestID:          request.RequestID,
		HasRunningTask:     false,
		TaskID:             "",
		JobID:              "",
		Status:             "idle",
		PendingCompletions: []string{},
	}

	if c.jobManager != nil {
		state, taskID := c.jobManager.GetState()
		switch state {
		case jobs.TaskStateRunning:
			response.HasRunningTask = true
			response.TaskID = taskID
			response.Status = "running"
			// Try to get job ID from the active job
			if jobStatus, err := c.jobManager.GetJobStatus(taskID); err == nil && jobStatus != nil && jobStatus.Assignment != nil {
				response.JobID = jobStatus.Assignment.JobExecutionID
			}
		case jobs.TaskStateCompleting:
			response.HasRunningTask = false
			response.TaskID = taskID
			response.Status = "completing"
		case jobs.TaskStateIdle:
			response.HasRunningTask = false
			response.Status = "idle"
		default:
			response.Status = "unknown"
		}

		// Get pending completions
		if hasPending, pendingTaskID := c.jobManager.GetCompletionPending(); hasPending && pendingTaskID != "" {
			response.PendingCompletions = []string{pendingTaskID}
		}
	}

	debug.Info("Sending state sync response (request_id: %s, status: %s, has_task: %v, pending: %v)",
		request.RequestID, response.Status, response.HasRunningTask, response.PendingCompletions)

	// Send response
	c.sendStateSyncResponse(&response)
}

// sendStateSyncResponse sends state sync response to backend
func (c *Connection) sendStateSyncResponse(response *StateSyncResponsePayload) {
	if !c.isConnected.Load() {
		debug.Warning("Cannot send state sync response - not connected")
		return
	}

	payloadBytes, err := json.Marshal(response)
	if err != nil {
		debug.Error("Failed to marshal state sync response: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeStateSyncResponse,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Debug("Sent state sync response (request_id: %s)", response.RequestID)
	} else {
		debug.Warning("Failed to send state sync response")
	}
}

// SendCrackBatchAsync sends crack batch asynchronously (non-blocking)
func (c *Connection) SendCrackBatchAsync(batch *jobs.CrackBatch) error {
	// Panic recovery (send on closed channel)
	defer func() {
		if r := recover(); r != nil {
			debug.Error("Panic in SendCrackBatchAsync: %v", r)
		}
	}()

	if !c.isConnected.Load() {
		return fmt.Errorf("not connected")
	}

	// Marshal batch payload to JSON
	batchJSON, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal crack batch: %w", err)
	}

	msg := &WSMessage{
		Type:      WSTypeCrackBatch,
		Payload:   batchJSON,
		Timestamp: time.Now(),
	}

	// Non-blocking send - drop if channel full (recovered from outfile)
	select {
	case c.outbound <- msg:
		debug.Debug("Queued crack batch: %d cracks for task %s", len(batch.CrackedHashes), batch.TaskID)
		return nil
	default:
		debug.Warning("Crack batch dropped (channel full): %d cracks for task %s - will recover from outfile",
			len(batch.CrackedHashes), batch.TaskID)
		return fmt.Errorf("channel full, cracks dropped")
	}
}

// SendCrackBatchesComplete sends signal that all crack batches have been sent for a task
func (c *Connection) SendCrackBatchesComplete(signal *jobs.CrackBatchesComplete) error {
	// Panic recovery (send on closed channel)
	defer func() {
		if r := recover(); r != nil {
			debug.Error("Panic in SendCrackBatchesComplete: %v", r)
		}
	}()

	if !c.isConnected.Load() {
		return fmt.Errorf("not connected")
	}

	// Marshal signal payload to JSON
	signalJSON, err := json.Marshal(signal)
	if err != nil {
		return fmt.Errorf("failed to marshal crack_batches_complete signal: %w", err)
	}

	msg := &WSMessage{
		Type:      WSTypeCrackBatchesComplete,
		Payload:   signalJSON,
		Timestamp: time.Now(),
	}

	// Blocking send - this signal must be delivered
	select {
	case c.outbound <- msg:
		debug.Debug("Queued crack_batches_complete signal for task %s", signal.TaskID)
		return nil
	case <-time.After(5 * time.Second):
		debug.Error("Timeout sending crack_batches_complete signal for task %s", signal.TaskID)
		return fmt.Errorf("timeout sending crack_batches_complete signal")
	}
}

// SendHashcatOutput sends hashcat output to the server
func (c *Connection) SendHashcatOutput(taskID string, output string, isError bool) error {
	if !c.isConnected.Load() {
		return fmt.Errorf("not connected")
	}

	// Create output payload
	outputPayload := map[string]interface{}{
		"task_id":  taskID,
		"output":   output,
		"is_error": isError,
		"timestamp": time.Now(),
	}

	// Marshal payload to JSON
	payloadJSON, err := json.Marshal(outputPayload)
	if err != nil {
		debug.Error("Failed to marshal hashcat output: %v", err)
		return fmt.Errorf("failed to marshal hashcat output: %w", err)
	}

	// Create and send message
	msg := &WSMessage{
		Type:      WSTypeHashcatOutput,
		Payload:   payloadJSON,
		Timestamp: time.Now(),
	}

	// Send via safeSendMessage with panic recovery
	if !c.safeSendMessage(msg, 5000) {
		debug.Error("Failed to queue hashcat output: channel blocked or closed")
		return fmt.Errorf("failed to queue hashcat output: channel blocked or closed")
	}
	return nil
}

// getDetailedOSInfo returns detailed OS information
func getDetailedOSInfo() map[string]interface{} {
	hostname, _ := os.Hostname()
	osInfo := map[string]interface{}{
		"platform": runtime.GOOS,
		"arch":     runtime.GOARCH,
		"hostname": hostname,
	}

	// Try to get more detailed info on Linux
	if runtime.GOOS == "linux" {
		// Try to read /etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
					
					switch key {
					case "NAME":
						osInfo["os_name"] = value
					case "VERSION":
						osInfo["os_version"] = value
					case "ID":
						osInfo["os_id"] = value
					case "VERSION_ID":
						osInfo["os_version_id"] = value
					case "PRETTY_NAME":
						osInfo["os_pretty_name"] = value
					}
				}
			}
		}
		
		// Try to get kernel version
		if data, err := os.ReadFile("/proc/version"); err == nil {
			osInfo["kernel_version"] = strings.TrimSpace(string(data))
		}
	}
	
	// Add Go version
	osInfo["go_version"] = runtime.Version()
	
	return osInfo
}

// createAgentStatusMessage creates an agent status update message
func (c *Connection) createAgentStatusMessage() (*WSMessage, error) {
	// Get hostname
	hostname, _ := os.Hostname()
	
	// Get detailed OS information
	osInfo := getDetailedOSInfo()
	
	// Create status payload
	statusPayload := map[string]interface{}{
		"status":      "active",
		"version":     version.GetVersion(),
		"updated_at":  time.Now(),
		"environment": map[string]string{
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
			"hostname": hostname,
		},
		"os_info": osInfo,
	}

	// Marshal status payload to JSON
	statusJSON, err := json.Marshal(statusPayload)
	if err != nil {
		debug.Error("Failed to marshal agent status: %v", err)
		return nil, fmt.Errorf("failed to marshal agent status: %w", err)
	}

	// Create and return status message
	msg := &WSMessage{
		Type:      WSTypeAgentStatus,
		Payload:   statusJSON,
		Timestamp: time.Now(),
	}

	return msg, nil
}

// Close closes the WebSocket connection
func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		debug.Info("Closing connection")
		c.isConnected.Store(false)

		// Close the outbound channel to signal writePump to exit
		// Use atomic flag to prevent double-close panic
		if !c.channelClosed.Load() {
			c.channelClosed.Store(true)
			close(c.outbound)
			debug.Debug("Outbound channel closed")
		} else {
			debug.Debug("Outbound channel already closed, skipping")
		}

		// Close the websocket connection
		if c.ws != nil {
			debug.Debug("Closing WebSocket connection")
			c.writeMux.Lock()
			c.ws.Close()
			c.writeMux.Unlock()
		}
	})
}

// Stop completely stops the connection and maintenance routines
func (c *Connection) Stop() {
	debug.Info("Stopping connection and maintenance")
	select {
	case <-c.done:
		debug.Debug("Connection already stopped")
	default:
		debug.Debug("Closing done channel")
		close(c.done)
	}
	c.Close()
}

// reinitializeChannels recreates closed channels after reconnection
func (c *Connection) reinitializeChannels() {
	c.writeMux.Lock()
	defer c.writeMux.Unlock()
	
	debug.Info("Reinitializing connection channels")
	
	// Check if outbound channel needs to be recreated
	// A closed channel will immediately return from a receive operation
	select {
	case _, ok := <-c.outbound:
		if !ok {
			// Channel is closed, create new one
			debug.Info("Outbound channel was closed, creating new channel")
			c.outbound = make(chan *WSMessage, 4096)
			// Reset channel closed flag for the new channel
			c.channelClosed.Store(false)
		}
	default:
		// Channel is still open and has no messages, which is fine
		debug.Debug("Outbound channel is still open")
	}

	// Reset closeOnce for next disconnection
	c.closeOnce = sync.Once{}
	debug.Info("Reset closeOnce for future disconnections")
}

// safeSendMessage safely sends a message to the outbound channel with panic recovery
func (c *Connection) safeSendMessage(msg *WSMessage, timeoutMs int) (sent bool) {
	// Recover from any panic (e.g., sending on closed channel)
	defer func() {
		if r := recover(); r != nil {
			debug.Error("Panic recovered in safeSendMessage: %v", r)
			sent = false
		}
	}()
	
	// Check if connected
	if !c.isConnected.Load() {
		debug.Debug("Not connected, skipping message send")
		return false
	}
	
	// Create timeout if specified
	if timeoutMs > 0 {
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()
		
		select {
		case c.outbound <- msg:
			return true
		case <-timer.C:
			debug.Warning("Timeout sending message of type %s", msg.Type)
			return false
		}
	}

	// Monitor channel fullness
	channelLen := len(c.outbound)
	channelCap := cap(c.outbound)
	if channelCap > 0 {
		fullnessPercent := float64(channelLen) / float64(channelCap) * 100
		if fullnessPercent >= 90 {
			debug.Error("Outbound channel critically full: %d/%d (%.1f%%) - message type: %s",
				channelLen, channelCap, fullnessPercent, msg.Type)
		} else if fullnessPercent >= 75 {
			debug.Warning("Outbound channel high: %d/%d (%.1f%%) - message type: %s",
				channelLen, channelCap, fullnessPercent, msg.Type)
		}
	}

	// Non-blocking send
	select {
	case c.outbound <- msg:
		return true
	default:
		channelLen := len(c.outbound)
		channelCap := cap(c.outbound)
		fullnessPercent := float64(channelLen) / float64(channelCap) * 100
		debug.Warning("Outbound channel full (%d/%d, %.1f%%), dropping message of type %s",
			channelLen, channelCap, fullnessPercent, msg.Type)
		return false
	}
}

// Start starts the WebSocket connection
func (c *Connection) Start() error {
	debug.Info("Starting WebSocket connection")

	if err := c.connect(); err != nil {
		debug.Error("Initial connection failed: %v", err)
		return err
	}

	go c.maintainConnection()
	go c.readPump()
	go c.writePump()
	
	// Send current task status after initial connection
	// This ensures the backend knows if we have any running tasks
	// Important for crash recovery: if agent restarts, it will report no tasks
	// and backend can immediately reset any reconnect_pending tasks
	go func() {
		// Small delay to ensure connection is fully established
		time.Sleep(2 * time.Second)
		c.sendCurrentTaskStatus()
		// Also send debug status report so backend knows current debug state (GH Issue #23)
		c.sendDebugStatusReport()
	}()

	return nil
}

// Connect establishes a WebSocket connection to the server
func (c *Connection) Connect() error {
	return c.connect()
}

// SetJobManager sets the job manager for handling job assignments
func (c *Connection) SetJobManager(jm JobManager) {
	c.jobManager = jm
}

// SendShutdownNotification sends a notification to the backend that the agent is shutting down gracefully
func (c *Connection) SendShutdownNotification(hasTask bool, taskID string, jobID string) {
	debug.Info("Sending shutdown notification to backend")

	// Check if connected
	if !c.isConnected.Load() {
		debug.Warning("Not connected, cannot send shutdown notification")
		return
	}

	// Create shutdown payload with provided task status
	shutdownPayload := struct {
		AgentID        int    `json:"agent_id"`
		Reason         string `json:"reason"`
		HasRunningTask bool   `json:"has_running_task"`
		TaskID         string `json:"task_id,omitempty"`
		JobID          string `json:"job_id,omitempty"`
	}{
		Reason:         "graceful_shutdown",
		HasRunningTask: hasTask,
		TaskID:         taskID,
		JobID:          jobID,
	}

	// Try to get agent ID from config
	configDir := config.GetConfigDir()
	agentIDPath := filepath.Join(configDir, "agent_id")
	agentIDBytes, err := os.ReadFile(agentIDPath)
	if err == nil {
		agentIDStr := strings.TrimSpace(string(agentIDBytes))
		if agentID, err := strconv.Atoi(agentIDStr); err == nil {
			shutdownPayload.AgentID = agentID
		}
	}
	
	// Marshal the payload
	payloadBytes, err := json.Marshal(shutdownPayload)
	if err != nil {
		debug.Error("Failed to marshal shutdown payload: %v", err)
		return
	}
	
	// Create the message
	msg := &WSMessage{
		Type:      WSTypeAgentShutdown,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}
	
	// Send with a short timeout since we're shutting down
	if c.safeSendMessage(msg, 2000) {
		debug.Info("Successfully sent shutdown notification - HasTask: %v, TaskID: %s", 
			shutdownPayload.HasRunningTask, shutdownPayload.TaskID)
	} else {
		debug.Warning("Failed to send shutdown notification (timeout or channel blocked)")
	}
}

// sendCurrentTaskStatus sends the current task status to the backend
func (c *Connection) sendCurrentTaskStatus() {
	debug.Info("Sending current task status to backend")
	
	// Check if we have a job manager
	if c.jobManager == nil {
		debug.Warning("No job manager available, sending empty task status")
		// Send empty status to indicate no running tasks
		// This is important for crash recovery
		var statusPayload CurrentTaskStatusPayload
		
		// Try to get agent ID from config
		configDir := config.GetConfigDir()
		agentIDPath := filepath.Join(configDir, "agent_id")
		agentIDBytes, err := os.ReadFile(agentIDPath)
		if err == nil {
			agentIDStr := strings.TrimSpace(string(agentIDBytes))
			if agentID, err := strconv.Atoi(agentIDStr); err == nil {
				statusPayload.AgentID = agentID
			}
		}
		
		statusPayload.HasRunningTask = false
		statusPayload.Status = "idle"
		
		// Marshal the payload
		payloadBytes, err := json.Marshal(statusPayload)
		if err != nil {
			debug.Error("Failed to marshal empty task status payload: %v", err)
			return
		}
		
		// Create and send the message
		msg := &WSMessage{
			Type:      WSTypeCurrentTaskStatus,
			Payload:   payloadBytes,
			Timestamp: time.Now(),
		}
		
		if c.safeSendMessage(msg, 5000) {
			debug.Info("Successfully sent empty task status (no job manager)")
		} else {
			debug.Error("Failed to send empty task status")
		}
		return
	}
	
	// Get current task status from job manager
	var statusPayload CurrentTaskStatusPayload
	
	// Try to get agent ID from config
	configDir := config.GetConfigDir()
	agentIDPath := filepath.Join(configDir, "agent_id")
	agentIDBytes, err := os.ReadFile(agentIDPath)
	if err == nil {
		agentIDStr := strings.TrimSpace(string(agentIDBytes))
		if agentID, err := strconv.Atoi(agentIDStr); err == nil {
			statusPayload.AgentID = agentID
		}
	}
	
	// Get task status from job manager if it's the concrete type
	if jm, ok := c.jobManager.(*jobs.JobManager); ok {
		taskInfo := jm.GetCurrentTaskStatus()
		if taskInfo != nil {
			statusPayload.HasRunningTask = true
			statusPayload.TaskID = taskInfo.TaskID
			statusPayload.JobID = taskInfo.JobID
			statusPayload.KeyspaceProcessed = taskInfo.KeyspaceProcessed
			statusPayload.EffectiveProgress = taskInfo.EffectiveProgress
			statusPayload.ProgressPercent = taskInfo.ProgressPercent
			statusPayload.TotalEffectiveKeyspace = taskInfo.TotalEffectiveKeyspace
			statusPayload.HashRate = taskInfo.HashRate
			statusPayload.CrackedCount = taskInfo.CrackedCount
			statusPayload.AllHashesCracked = taskInfo.AllHashesCracked
			statusPayload.Status = taskInfo.Status
			statusPayload.ErrorMessage = taskInfo.ErrorMessage

			// If we reported a completed/failed task, clear the cache
			// The backend will process this status and we don't need to report it again
			if taskInfo.Status == "completed" || taskInfo.Status == "failed" {
				debug.Info("Reporting cached completion for task %s with status %s, progress %.2f%%",
					taskInfo.TaskID, taskInfo.Status, taskInfo.ProgressPercent)
				jm.ClearLastCompletedTask()
			}
		} else {
			statusPayload.HasRunningTask = false
			statusPayload.Status = "idle"
		}
	}
	
	// Marshal the payload
	payloadBytes, err := json.Marshal(statusPayload)
	if err != nil {
		debug.Error("Failed to marshal task status payload: %v", err)
		return
	}
	
	// Create the message
	msg := &WSMessage{
		Type:      WSTypeCurrentTaskStatus,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}
	
	// Send the message
	if c.safeSendMessage(msg, 5000) {
		debug.Info("Successfully sent current task status - HasTask: %v, TaskID: %s, JobID: %s",
			statusPayload.HasRunningTask, statusPayload.TaskID, statusPayload.JobID)

		// After sending task status, also send pending outfiles for the acknowledgment protocol
		c.sendPendingOutfiles()
	} else {
		debug.Error("Failed to send current task status")
	}
}

// GetHardwareMonitor returns the hardware monitor for device management
func (c *Connection) GetHardwareMonitor() *hardware.Monitor {
	return c.hwMonitor
}

// SetPreferredBinaryVersion sets the preferred binary version for device detection
func (c *Connection) SetPreferredBinaryVersion(version int64) {
	c.binaryMutex.Lock()
	defer c.binaryMutex.Unlock()
	c.preferredBinaryVersion = version
}

// GetPreferredBinaryVersion gets the preferred binary version for device detection
func (c *Connection) GetPreferredBinaryVersion() int64 {
	c.binaryMutex.RLock()
	defer c.binaryMutex.RUnlock()
	return c.preferredBinaryVersion
}

// checkAndExtractBinaryArchives checks all binary directories for .7z files without extracted executables
// initializeFileSync initializes the file sync and download manager
func (c *Connection) initializeFileSync(apiKey, agentID string) error {
	// Get data directory paths
	dataDirs, err := config.GetDataDirs()
	if err != nil {
		return fmt.Errorf("failed to get data directories: %w", err)
	}

	// Initialize file sync
	c.fileSync, err = filesync.NewFileSync(c.urlConfig, dataDirs, apiKey, agentID)
	if err != nil {
		return fmt.Errorf("failed to initialize file sync: %w", err)
	}

	// Initialize download manager with file sync
	c.downloadManager = filesync.NewDownloadManager(c.fileSync, 3)

	// Start monitoring download progress
	go c.monitorDownloadProgress()

	return nil
}

// monitorDownloadProgress monitors download progress and sends updates
func (c *Connection) monitorDownloadProgress() {
	if c.downloadManager == nil {
		return
	}

	progressChan := c.downloadManager.GetProgressChannel()
	for range progressChan {
		// Query download manager for actual state (single source of truth)
		total, pending, downloading, completed, failed := c.downloadManager.GetDownloadStats()

		// Log progress for debugging
		debug.Info("Download progress: %d completed, %d failed, %d pending, %d downloading (total: %d)",
			completed, failed, pending, downloading, total)

		// Check if all downloads are resolved (no active downloads remaining)
		if pending == 0 && downloading == 0 && total > 0 {
			// All downloads finished (either completed or failed)
			if failed > 0 {
				debug.Warning("File sync completed with %d failures out of %d total files", failed, total)
			}
			c.sendSyncCompleted()
		}
	}
}

// sendSyncStarted sends sync started message to backend
func (c *Connection) sendSyncStarted(filesToSync int) {
	c.syncMutex.Lock()
	c.syncStatus = "in_progress"
	c.syncMutex.Unlock()

	payload, _ := json.Marshal(map[string]interface{}{
		"agent_id":      c.agentID,
		"files_to_sync": filesToSync,
	})

	message := WSMessage{
		Type:      "sync_started",
		Payload:   payload,
		Timestamp: time.Now(),
	}

	select {
	case c.outbound <- &message:
		debug.Info("Sent sync started message with %d files", filesToSync)
	default:
		debug.Warning("Failed to send sync started message: outbound channel full")
	}
}

// sendSyncCompleted sends sync completed message to backend
func (c *Connection) sendSyncCompleted() {
	c.syncMutex.Lock()
	if c.syncStatus == "completed" {
		c.syncMutex.Unlock()
		return // Already sent
	}
	c.syncStatus = "completed"
	c.syncMutex.Unlock()

	// Get final stats from download manager (single source of truth)
	total, _, _, completed, failed := c.downloadManager.GetDownloadStats()

	// Send status message in the format the backend expects
	statusMessage := "File sync completed successfully"
	if failed > 0 {
		statusMessage = fmt.Sprintf("File sync completed with %d failures out of %d files", failed, total)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"status":   "completed",
		"progress": 100,
		"message":  statusMessage,
	})

	message := WSMessage{
		Type:      WSTypeFileSyncStatus,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	select {
	case c.outbound <- &message:
		debug.Info("Sent sync status completed message: %d succeeded, %d failed out of %d total", completed, failed, total)
		if failed > 0 {
			console.Warning("File synchronization complete with issues (%d/%d files downloaded, %d failed)", completed, total, failed)
		} else {
			console.Success("File synchronization complete (%d/%d files downloaded)", completed, total)
		}
	default:
		debug.Warning("Failed to send sync completed message: outbound channel full")
	}
}

// sendSyncFailed sends sync failed message to backend
func (c *Connection) sendSyncFailed(err error) {
	c.syncMutex.Lock()
	c.syncStatus = "failed"
	c.syncMutex.Unlock()

	payload, _ := json.Marshal(map[string]interface{}{
		"agent_id": c.agentID,
		"error":    err.Error(),
	})

	message := WSMessage{
		Type:      "sync_failed",
		Payload:   payload,
		Timestamp: time.Now(),
	}

	select {
	case c.outbound <- &message:
		debug.Error("Sent sync failed message: %v", err)
	default:
		debug.Warning("Failed to send sync failed message: outbound channel full")
	}
}

func (c *Connection) checkAndExtractBinaryArchives() error {
	if c.fileSync == nil {
		return fmt.Errorf("file sync not initialized")
	}

	// Get the binaries directory
	binaryDir, err := c.fileSync.GetFileTypeDir("binary")
	if err != nil {
		return fmt.Errorf("failed to get binary directory: %w", err)
	}

	// List all binary ID directories
	entries, err := os.ReadDir(binaryDir)
	if err != nil {
		return fmt.Errorf("failed to read binary directory: %w", err)
	}

	debug.Info("Checking binary directories for archives without extracted executables")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Skip non-directories
		}

		// Each directory represents a binary ID
		binaryIDDir := filepath.Join(binaryDir, entry.Name())

		// Check for .7z files in this directory
		archiveFiles, err := filepath.Glob(filepath.Join(binaryIDDir, "*.7z"))
		if err != nil {
			debug.Error("Failed to search for archives in %s: %v", binaryIDDir, err)
			continue
		}

		if len(archiveFiles) == 0 {
			continue // No archives in this directory
		}

		// Check if any executables exist
		execFiles, err := c.fileSync.FindExtractedExecutables(binaryIDDir)
		if err != nil {
			debug.Error("Failed to search for executables in %s: %v", binaryIDDir, err)
			continue
		}

		// If we have archives but no executables, extract them
		if len(execFiles) == 0 && len(archiveFiles) > 0 {
			debug.Info("Found binary directory %s with archives but no executables, extracting...", entry.Name())

			// Extract each archive
			for _, archivePath := range archiveFiles {
				archiveFilename := filepath.Base(archivePath)
				debug.Info("Extracting binary archive %s during pre-sync check", archiveFilename)
				console.Status("Extracting binary archive %s...", archiveFilename)

				if err := c.fileSync.ExtractBinary7z(archivePath, binaryIDDir); err != nil {
					debug.Error("Failed to extract binary archive %s: %v", archiveFilename, err)
					console.Error("Failed to extract binary archive %s: %v", archiveFilename, err)
					continue
				}

				debug.Info("Successfully extracted binary archive %s during pre-sync check", archiveFilename)
				console.Success("Binary archive %s extracted successfully", archiveFilename)
			}
		}
	}

	return nil
}

// DetectAndSendDevices detects available compute devices and sends them to the server
// This is exported so it can be called from main.go at startup
func (c *Connection) DetectAndSendDevices() error {
	debug.Info("Starting physical device detection using hashcat")

	// Detect physical devices using hashcat (grouped by physical GPU)
	result, err := c.hwMonitor.DetectPhysicalDevices()
	if err != nil {
		debug.Error("Failed to detect physical devices: %v", err)
		// Send error status to server
		errorPayload := map[string]interface{}{
			"error": err.Error(),
			"status": "error",
		}
		errorJSON, _ := json.Marshal(errorPayload)

		msg := &WSMessage{
			Type:      WSTypePhysicalDeviceDetection,
			Payload:   errorJSON,
			Timestamp: time.Now(),
		}

		// Use safeSendMessage to avoid concurrent writes
		if !c.safeSendMessage(msg, 5000) {
			debug.Error("Failed to send physical device detection error")
		}

		return err
	}

	// Marshal physical device detection result
	devicesJSON, err := json.Marshal(result)
	if err != nil {
		debug.Error("Failed to marshal physical device detection result: %v", err)
		return fmt.Errorf("failed to marshal physical device detection result: %w", err)
	}

	// Send physical device information to server
	msg := &WSMessage{
		Type:      WSTypePhysicalDeviceDetection,
		Payload:   devicesJSON,
		Timestamp: time.Now(),
	}

	// Use safeSendMessage to avoid concurrent writes
	if !c.safeSendMessage(msg, 5000) {
		debug.Error("Failed to send physical device detection result")
		return fmt.Errorf("failed to send physical device detection result: channel blocked or timeout")
	}

	debug.Info("Successfully sent physical device detection result with %d devices", len(result.Devices))

	// Mark devices as detected
	c.deviceMutex.Lock()
	c.devicesDetected = true
	c.deviceMutex.Unlock()

	return nil
}

// TryDetectDevicesIfNeeded attempts to detect devices if they haven't been detected yet and a binary is available
func (c *Connection) TryDetectDevicesIfNeeded() {
	// Atomically check and set detection status to prevent race conditions
	c.deviceMutex.Lock()

	// If already detected, skip
	if c.devicesDetected {
		c.deviceMutex.Unlock()
		debug.Info("Devices already detected, skipping detection")
		return
	}

	// If detection is in progress, skip to avoid concurrent hashcat processes
	if c.detectionInProgress {
		c.deviceMutex.Unlock()
		debug.Info("Device detection already in progress, skipping duplicate detection")
		return
	}

	// Set detection in progress flag
	c.detectionInProgress = true
	c.deviceMutex.Unlock()

	// Ensure we clear the in-progress flag when done
	defer func() {
		c.deviceMutex.Lock()
		c.detectionInProgress = false
		c.deviceMutex.Unlock()
	}()

	// Check if hashcat binary is available
	if !c.hwMonitor.HasBinary() {
		debug.Info("No hashcat binary available yet, skipping device detection")
		return
	}

	// Attempt device detection
	debug.Info("Hashcat binary available, attempting device detection")
	if err := c.DetectAndSendDevices(); err != nil {
		debug.Error("Failed to detect devices: %v", err)
	}
}

// shouldBufferMessage determines if a message should be buffered
func (c *Connection) shouldBufferMessage(msg *WSMessage) bool {
	switch msg.Type {
	case WSTypeJobProgress, WSTypeCrackBatch, WSTypeHashcatOutput, WSTypeBenchmarkResult:
		// Check if message contains crack information
		if msg.Type == WSTypeJobProgress || msg.Type == WSTypeCrackBatch || msg.Type == WSTypeHashcatOutput {
			return buffer.HasCrackedHashes(msg.Payload)
		}
		return true
	default:
		return false
	}
}

// bufferMessage adds a message to the buffer
func (c *Connection) bufferMessage(msg *WSMessage) error {
	if c.messageBuffer == nil {
		return fmt.Errorf("message buffer not initialized")
	}
	
	return c.messageBuffer.Add(buffer.MessageType(msg.Type), msg.Payload)
}

// sendBufferedMessages sends all buffered messages to the server
func (c *Connection) sendBufferedMessages() {
	if c.messageBuffer == nil || c.messageBuffer.Count() == 0 {
		return
	}
	
	debug.Info("Sending %d buffered messages", c.messageBuffer.Count())
	
	// Get all buffered messages
	messages := c.messageBuffer.GetAll()
	
	// Create payload with all buffered messages
	payload, err := json.Marshal(map[string]interface{}{
		"messages": messages,
		"agent_id": c.agentID,
	})
	if err != nil {
		debug.Error("Failed to marshal buffered messages: %v", err)
		return
	}
	
	// Send buffered messages
	msg := WSMessage{
		Type:      WSTypeBufferedMessages,
		Payload:   payload,
		Timestamp: time.Now(),
	}
	
	// Use safeSendMessage to avoid blocking
	if c.safeSendMessage(&msg, 10000) { // 10 second timeout for buffered messages
		debug.Info("Successfully sent buffered messages, waiting for ACK")
		// Note: Buffer will be cleared when we receive the ACK
	} else {
		debug.Error("Failed to send buffered messages - will retry on next connection")
	}
}

// handleBufferAck processes acknowledgment from server for buffered messages
func (c *Connection) handleBufferAck(payload json.RawMessage) {
	var ack struct {
		MessageIDs []string `json:"message_ids"`
	}

	if err := json.Unmarshal(payload, &ack); err != nil {
		debug.Error("Failed to unmarshal buffer ACK: %v", err)
		return
	}

	if c.messageBuffer == nil {
		return
	}

	// Remove acknowledged messages from buffer
	if err := c.messageBuffer.RemoveMessages(ack.MessageIDs); err != nil {
		debug.Error("Failed to remove acknowledged messages from buffer: %v", err)
	} else {
		debug.Info("Removed %d acknowledged messages from buffer", len(ack.MessageIDs))
	}
}

// handleCrackRetransmitRequest processes a request from the backend to retransmit cracks from an outfile
func (c *Connection) handleCrackRetransmitRequest(payload json.RawMessage) {
	var request struct {
		TaskID        string `json:"task_id"`
		ExpectedCount int    `json:"expected_count"`
	}

	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal crack retransmit request: %v", err)
		return
	}

	debug.Info("Processing crack retransmit request for task %s (expected %d cracks)", request.TaskID, request.ExpectedCount)

	// Get job manager
	jm, ok := c.jobManager.(*jobs.JobManager)
	if !ok || jm == nil {
		debug.Error("Job manager not available for retransmit")
		return
	}

	// Read all cracks from the outfile
	cracks, err := jm.RetransmitOutfile(request.TaskID)
	if err != nil {
		debug.Error("Failed to retransmit outfile for task %s: %v", request.TaskID, err)
		return
	}

	if len(cracks) == 0 {
		debug.Warning("No cracks found in outfile for task %s", request.TaskID)
		// Still send crack_batches_complete with 0 count to signal completion
		c.SendCrackBatchesComplete(&jobs.CrackBatchesComplete{TaskID: request.TaskID, IsRetransmit: true})
		return
	}

	// Send in batches of 10,000 (same as regular crack transmission)
	const retransmitBatchSize = 10000
	totalCracks := len(cracks)
	batchesSent := 0

	for start := 0; start < totalCracks; start += retransmitBatchSize {
		end := start + retransmitBatchSize
		if end > totalCracks {
			end = totalCracks
		}

		batch := jobs.CrackBatch{
			TaskID:        request.TaskID,
			CrackedHashes: cracks[start:end],
			IsRetransmit:  true,
		}

		batchBytes, err := json.Marshal(batch)
		if err != nil {
			debug.Error("Failed to marshal retransmit crack batch: %v", err)
			return
		}

		msg := &WSMessage{
			Type:      WSTypeCrackBatch,
			Payload:   batchBytes,
			Timestamp: time.Now(),
		}

		if !c.safeSendMessage(msg, 5000) {
			debug.Error("Failed to send retransmit crack batch %d for task %s", batchesSent+1, request.TaskID)
			return
		}
		batchesSent++

		debug.Debug("Sent retransmit batch %d with %d cracks for task %s",
			batchesSent, end-start, request.TaskID)
	}

	debug.Info("Completed retransmission: sent %d batches with %d total cracks for task %s",
		batchesSent, totalCracks, request.TaskID)

	// Send crack_batches_complete to signal all batches sent (marked as retransmit)
	if err := c.SendCrackBatchesComplete(&jobs.CrackBatchesComplete{TaskID: request.TaskID, IsRetransmit: true}); err != nil {
		debug.Error("Failed to send crack_batches_complete for retransmit task %s: %v", request.TaskID, err)
	}
}

// handleOutfileDeleteApproval processes approval from the backend to delete an outfile
func (c *Connection) handleOutfileDeleteApproval(payload json.RawMessage) {
	var approval struct {
		TaskID            string `json:"task_id"`
		ExpectedLineCount int64  `json:"expected_line_count"`
		TaskExists        bool   `json:"task_exists"`
	}

	if err := json.Unmarshal(payload, &approval); err != nil {
		debug.Error("Failed to unmarshal outfile delete approval: %v", err)
		return
	}

	debug.Info("Processing outfile delete approval for task %s (expected %d lines, task_exists=%v)", approval.TaskID, approval.ExpectedLineCount, approval.TaskExists)

	// Get job manager
	jm, ok := c.jobManager.(*jobs.JobManager)
	if !ok || jm == nil {
		debug.Error("Job manager not available for outfile deletion")
		return
	}

	// SAFETY CHECK 1: Don't delete if currently working on this task
	// This prevents a race condition where:
	// 1. Agent had task, went offline, reconnects
	// 2. Backend requests retransmission of old outfile
	// 3. Agent gets reassigned the same task again
	// 4. Backend approves deletion of outfile (from retransmit)
	// 5. If we delete now, we lose the NEW cracks from the reassigned task
	taskInfo := jm.GetCurrentTaskStatus()
	if taskInfo != nil && taskInfo.TaskID == approval.TaskID {
		debug.Warning("Ignoring outfile delete approval for task %s - currently working on it (race condition prevented)", approval.TaskID)
		return
	}

	// If the backend says the task doesn't exist, delete unconditionally
	// This handles orphaned outfiles from deleted jobs where the cracks have already been processed
	if !approval.TaskExists {
		debug.Info("Task %s no longer exists in backend, deleting outfile unconditionally", approval.TaskID)
		if err := jm.DeleteOutfile(approval.TaskID); err != nil {
			if !os.IsNotExist(err) {
				debug.Error("Failed to delete orphaned outfile for task %s: %v", approval.TaskID, err)
			}
		} else {
			debug.Info("Successfully deleted orphaned outfile for task %s", approval.TaskID)
		}
		return
	}

	// SAFETY CHECK 2: Verify line count matches expected
	// This prevents data loss if outfile grew while retransmit was being processed
	actualCount, err := jm.GetOutfileLineCount(approval.TaskID)
	if err != nil {
		if os.IsNotExist(err) {
			debug.Info("Outfile already deleted for task %s", approval.TaskID)
			return
		}
		debug.Error("Failed to count outfile lines for task %s: %v", approval.TaskID, err)
		return
	}

	// Check 3: Verify line counts match
	if actualCount != approval.ExpectedLineCount {
		debug.Warning("Line count mismatch for task %s: expected %d, actual %d - rejecting delete",
			approval.TaskID, approval.ExpectedLineCount, actualCount)
		c.sendOutfileDeleteRejected(approval.TaskID, approval.ExpectedLineCount, actualCount)
		return
	}

	// Safe to delete - line counts verified
	debug.Info("Line count verified for task %s (%d lines), deleting outfile", approval.TaskID, actualCount)
	if err := jm.DeleteOutfile(approval.TaskID); err != nil {
		debug.Error("Failed to delete outfile for task %s: %v", approval.TaskID, err)
	} else {
		debug.Info("Successfully deleted outfile for task %s after backend approval", approval.TaskID)
	}
}

// sendOutfileDeleteRejected sends a rejection message to the backend when line count doesn't match
func (c *Connection) sendOutfileDeleteRejected(taskID string, expected, actual int64) {
	payload := struct {
		TaskID            string `json:"task_id"`
		ExpectedLineCount int64  `json:"expected_line_count"`
		ActualLineCount   int64  `json:"actual_line_count"`
		Reason            string `json:"reason"`
	}{
		TaskID:            taskID,
		ExpectedLineCount: expected,
		ActualLineCount:   actual,
		Reason:            "line_count_mismatch",
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal outfile delete rejection payload: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeOutfileDeleteRejected,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Info("Sent outfile delete rejection for task %s (expected %d, actual %d)", taskID, expected, actual)
	} else {
		debug.Error("Failed to send outfile delete rejection for task %s", taskID)
	}
}

// sendPendingOutfiles sends a list of pending outfiles to the backend on reconnect
func (c *Connection) sendPendingOutfiles() {
	// Get job manager
	jm, ok := c.jobManager.(*jobs.JobManager)
	if !ok || jm == nil {
		debug.Debug("Job manager not available, skipping pending outfiles check")
		return
	}

	// Get list of pending outfiles
	taskIDs, currentTaskID, err := jm.GetPendingOutfiles()
	if err != nil {
		debug.Error("Failed to get pending outfiles: %v", err)
		return
	}

	if len(taskIDs) == 0 {
		debug.Debug("No pending outfiles to report")
		return
	}

	debug.Info("Reporting %d pending outfiles to backend (current task: %s)", len(taskIDs), currentTaskID)

	// Create message payload
	pendingPayload := map[string]interface{}{
		"task_ids":        taskIDs,
		"current_task_id": currentTaskID,
	}

	payloadBytes, err := json.Marshal(pendingPayload)
	if err != nil {
		debug.Error("Failed to marshal pending outfiles payload: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypePendingOutfiles,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Info("Sent pending outfiles notification to backend")
	} else {
		debug.Error("Failed to send pending outfiles notification")
	}
}

// ============================================================================
// Diagnostics Handlers (GH Issue #23)
// ============================================================================

// sendDebugStatusReport sends the current debug status to the backend
func (c *Connection) sendDebugStatusReport() {
	if !c.isConnected.Load() {
		debug.Warning("Cannot send debug status report - not connected")
		return
	}

	status := debug.GetStatus()

	// Check log file info
	var logFileExists bool
	var logFileSize int64
	var logFileModified int64

	if status.LogFilePath != "" {
		if info, err := os.Stat(status.LogFilePath); err == nil {
			logFileExists = true
			logFileSize = info.Size()
			logFileModified = info.ModTime().Unix()
		}
	}

	payload := DebugStatusReportPayload{
		Enabled:            status.Enabled,
		Level:              status.Level,
		FileLoggingEnabled: status.FileLoggingEnabled,
		LogFilePath:        status.LogFilePath,
		LogFileExists:      logFileExists,
		LogFileSize:        logFileSize,
		LogFileModified:    logFileModified,
		BufferCount:        status.BufferCount,
		BufferCapacity:     status.BufferCapacity,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal debug status report: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeDebugStatusReport,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Debug("Sent debug status report to backend")
	} else {
		debug.Warning("Failed to send debug status report")
	}
}

// handleDebugToggle handles a request to toggle debug mode
func (c *Connection) handleDebugToggle(payload json.RawMessage) {
	var request DebugTogglePayload
	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal debug toggle request: %v", err)
		c.sendDebugToggleAck(false, false, false, "Failed to parse request")
		return
	}

	debug.Info("Processing debug toggle request: enable=%v", request.Enable)

	// Update runtime state immediately
	debug.SetEnabled(request.Enable)

	// If enabling, also enable file logging to the logs directory
	var logDir string
	if request.Enable {
		// Use config directory's parent + logs, or current directory + logs
		configDir := config.GetConfigDir()
		logDir = filepath.Join(filepath.Dir(configDir), "logs")
		if err := debug.EnableFileLogging(logDir); err != nil {
			debug.Error("Failed to enable file logging to %s: %v", logDir, err)
			// Continue anyway - runtime logging will still work
		}
	} else {
		// Disable file logging when debug is disabled
		if err := debug.DisableFileLogging(); err != nil {
			debug.Error("Failed to disable file logging: %v", err)
		}
	}

	// Update the .env file for persistence across restarts
	envUpdated := c.updateEnvFile(request.Enable, logDir)

	// If env update failed, still report success for runtime change
	// but indicate restart may be needed for full persistence
	restartRequired := !envUpdated

	c.sendDebugToggleAck(true, request.Enable, restartRequired, "")
	debug.Info("Debug mode toggled: enabled=%v, restart_required=%v", request.Enable, restartRequired)

	// Send updated debug status
	c.sendDebugStatusReport()
}

// updateEnvFile updates the .env file with debug settings
func (c *Connection) updateEnvFile(enable bool, logDir string) bool {
	// Find the .env file - check config directory first, then working directory
	envPaths := []string{
		filepath.Join(config.GetConfigDir(), ".env"),
		".env",
	}

	var envPath string
	var existingContent []byte
	var err error

	for _, p := range envPaths {
		if _, statErr := os.Stat(p); statErr == nil {
			envPath = p
			existingContent, err = os.ReadFile(p)
			if err != nil {
				debug.Warning("Failed to read existing .env file at %s: %v", p, err)
				existingContent = nil
			}
			break
		}
	}

	// If no .env file found, create one in config directory
	if envPath == "" {
		envPath = filepath.Join(config.GetConfigDir(), ".env")
		existingContent = nil
	}

	// Parse existing content and update DEBUG and LOG_DIR lines
	lines := strings.Split(string(existingContent), "\n")
	newLines := make([]string, 0, len(lines)+2)
	foundDebug := false
	foundLogDir := false
	foundLogLevel := false

	debugValue := "false"
	if enable {
		debugValue = "true"
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "DEBUG=") {
			newLines = append(newLines, fmt.Sprintf("DEBUG=%s", debugValue))
			foundDebug = true
		} else if strings.HasPrefix(trimmed, "LOG_DIR=") {
			if enable && logDir != "" {
				newLines = append(newLines, fmt.Sprintf("LOG_DIR=%s", logDir))
			} else {
				newLines = append(newLines, "LOG_DIR=")
			}
			foundLogDir = true
		} else if strings.HasPrefix(trimmed, "LOG_LEVEL=") {
			newLines = append(newLines, line)
			foundLogLevel = true
		} else if trimmed != "" || len(newLines) > 0 {
			// Keep non-empty lines and preserve trailing empty lines only if we have content
			newLines = append(newLines, line)
		}
	}

	// Add missing entries
	if !foundDebug {
		newLines = append(newLines, fmt.Sprintf("DEBUG=%s", debugValue))
	}
	if !foundLogDir && enable && logDir != "" {
		newLines = append(newLines, fmt.Sprintf("LOG_DIR=%s", logDir))
	}
	if !foundLogLevel {
		newLines = append(newLines, "LOG_LEVEL=DEBUG")
	}

	// Write updated content
	newContent := strings.Join(newLines, "\n")
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	if err := os.WriteFile(envPath, []byte(newContent), 0644); err != nil {
		debug.Error("Failed to write .env file at %s: %v", envPath, err)
		return false
	}

	debug.Info("Updated .env file at %s with DEBUG=%s", envPath, debugValue)
	return true
}

// sendDebugToggleAck sends acknowledgment for debug toggle
func (c *Connection) sendDebugToggleAck(success, enabled, restartRequired bool, message string) {
	if !c.isConnected.Load() {
		return
	}

	payload := DebugToggleAckPayload{
		Success:         success,
		Enabled:         enabled,
		RestartRequired: restartRequired,
		Message:         message,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal debug toggle ack: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeDebugToggleAck,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	c.safeSendMessage(msg, 5000)
}

// handleLogRequest handles a request to retrieve logs
func (c *Connection) handleLogRequest(payload json.RawMessage) {
	var request LogRequestPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal log request: %v", err)
		c.sendLogData(request.RequestID, nil, "", 0, false, "Failed to parse request")
		return
	}

	debug.Info("Processing log request (request_id: %s, hours_back: %d, include_all: %v)",
		request.RequestID, request.HoursBack, request.IncludeAll)

	// Calculate the since time
	var since time.Time
	if !request.IncludeAll && request.HoursBack > 0 {
		since = time.Now().Add(-time.Duration(request.HoursBack) * time.Hour)
	}

	// Get buffered logs
	var entries []LogEntryPayload
	var bufferedLogs []logbuffer.LogEntry

	if request.IncludeAll {
		bufferedLogs = debug.GetAllBufferedLogs()
	} else {
		bufferedLogs = debug.GetBufferedLogs(since)
	}

	// Convert to payload format
	for _, entry := range bufferedLogs {
		entries = append(entries, LogEntryPayload{
			Timestamp: entry.Timestamp.UnixMilli(),
			Level:     entry.Level,
			Message:   entry.Message,
			File:      entry.File,
			Line:      entry.Line,
			Function:  entry.Function,
		})
	}

	// Also try to read log file if it exists
	var fileContent string
	status := debug.GetStatus()
	if status.LogFilePath != "" {
		if data, err := os.ReadFile(status.LogFilePath); err == nil {
			// Limit file content to 1MB
			const maxFileSize = 1024 * 1024
			if len(data) > maxFileSize {
				// Take the last 1MB
				data = data[len(data)-maxFileSize:]
				fileContent = "... [truncated] ...\n" + string(data)
			} else {
				fileContent = string(data)
			}
		}
	}

	totalCount := len(entries)
	truncated := false

	// Limit entries to prevent huge messages
	const maxEntries = 500
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
		truncated = true
	}

	c.sendLogData(request.RequestID, entries, fileContent, totalCount, truncated, "")
}

// sendLogData sends log data to the backend
func (c *Connection) sendLogData(requestID string, entries []LogEntryPayload, fileContent string, totalCount int, truncated bool, errMsg string) {
	if !c.isConnected.Load() {
		return
	}

	payload := LogDataPayload{
		RequestID:   requestID,
		AgentID:     c.agentID,
		Entries:     entries,
		FileContent: fileContent,
		TotalCount:  totalCount,
		Truncated:   truncated,
		Error:       errMsg,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal log data: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeLogData,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 10000) { // Longer timeout for potentially large payload
		debug.Debug("Sent log data (request_id: %s, entries: %d, truncated: %v)", requestID, len(entries), truncated)
	} else {
		debug.Warning("Failed to send log data")
	}
}

// handleLogStatusRequest handles a request for log file status
func (c *Connection) handleLogStatusRequest(payload json.RawMessage) {
	var request LogStatusRequestPayload
	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal log status request: %v", err)
		return
	}

	debug.Debug("Processing log status request (request_id: %s)", request.RequestID)

	status := debug.GetStatus()

	// Check log file info
	var logFileExists bool
	var logFileSize int64
	var logFileModified int64

	if status.LogFilePath != "" {
		if info, err := os.Stat(status.LogFilePath); err == nil {
			logFileExists = true
			logFileSize = info.Size()
			logFileModified = info.ModTime().Unix()
		}
	}

	response := LogStatusResponsePayload{
		RequestID:       request.RequestID,
		LogFileExists:   logFileExists,
		LogFilePath:     status.LogFilePath,
		LogFileSize:     logFileSize,
		LogFileModified: logFileModified,
		DebugEnabled:    status.Enabled,
		BufferCount:     status.BufferCount,
	}

	c.sendLogStatusResponse(&response)
}

// sendLogStatusResponse sends log status response to backend
func (c *Connection) sendLogStatusResponse(response *LogStatusResponsePayload) {
	if !c.isConnected.Load() {
		return
	}

	payloadBytes, err := json.Marshal(response)
	if err != nil {
		debug.Error("Failed to marshal log status response: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeLogStatusResponse,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Debug("Sent log status response (request_id: %s)", response.RequestID)
	} else {
		debug.Warning("Failed to send log status response")
	}
}

// handleLogPurge handles a request to delete log files
func (c *Connection) handleLogPurge(payload json.RawMessage) {
	var request LogPurgePayload
	if err := json.Unmarshal(payload, &request); err != nil {
		debug.Error("Failed to unmarshal log purge request: %v", err)
		c.sendLogPurgeAck(request.RequestID, false, "Failed to parse request")
		return
	}

	debug.Info("Processing log purge request (request_id: %s)", request.RequestID)

	// Clear the in-memory buffer
	debug.ClearLogBuffer()

	// Delete the log file if it exists
	status := debug.GetStatus()
	if status.LogFilePath != "" {
		if err := os.Remove(status.LogFilePath); err != nil && !os.IsNotExist(err) {
			debug.Warning("Failed to delete log file %s: %v", status.LogFilePath, err)
			c.sendLogPurgeAck(request.RequestID, false, fmt.Sprintf("Failed to delete log file: %v", err))
			return
		}
		debug.Info("Deleted log file: %s", status.LogFilePath)
	}

	c.sendLogPurgeAck(request.RequestID, true, "")
	debug.Info("Log purge completed (request_id: %s)", request.RequestID)
}

// sendLogPurgeAck sends log purge acknowledgment to backend
func (c *Connection) sendLogPurgeAck(requestID string, success bool, message string) {
	if !c.isConnected.Load() {
		return
	}

	payload := LogPurgeAckPayload{
		RequestID: requestID,
		Success:   success,
		Message:   message,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal log purge ack: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeLogPurgeAck,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Debug("Sent log purge ack (request_id: %s, success: %v)", requestID, success)
	} else {
		debug.Warning("Failed to send log purge ack")
	}
}
