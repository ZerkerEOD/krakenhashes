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

	// Job execution message types
	WSTypeTaskAssignment   WSMessageType = "task_assignment"
	WSTypeJobProgress      WSMessageType = "job_progress"
	WSTypeJobStatus        WSMessageType = "job_status"        // Status-only (synchronous)
	WSTypeCrackBatch       WSMessageType = "crack_batch"       // Cracks-only (asynchronous)
	WSTypeJobStop          WSMessageType = "job_stop"
	WSTypeBenchmarkRequest WSMessageType = "benchmark_request"
	WSTypeBenchmarkResult  WSMessageType = "benchmark_result"
	WSTypeHashcatOutput    WSMessageType = "hashcat_output"
	WSTypeForceCleanup     WSMessageType = "force_cleanup"
	WSTypeCurrentTaskStatus WSMessageType = "current_task_status"

	// Device detection message types
	WSTypeDeviceDetection  WSMessageType = "device_detection"
	WSTypeDeviceUpdate     WSMessageType = "device_update"

	// Buffer-related message types
	WSTypeBufferedMessages WSMessageType = "buffered_messages"
	WSTypeBufferAck        WSMessageType = "buffer_ack"

	// Shutdown message type
	WSTypeAgentShutdown    WSMessageType = "agent_shutdown"
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
type CurrentTaskStatusPayload struct {
	AgentID           int    `json:"agent_id"`
	HasRunningTask    bool   `json:"has_running_task"`
	TaskID            string `json:"task_id,omitempty"`
	JobID             string `json:"job_id,omitempty"`
	KeyspaceProcessed int64  `json:"keyspace_processed,omitempty"`
	Status            string `json:"status,omitempty"`
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
	ExtraParameters string             `json:"extra_parameters,omitempty"` // Agent-specific hashcat parameters
	EnabledDevices  []int              `json:"enabled_devices,omitempty"`  // List of enabled device IDs
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
	devicesDetected bool
	deviceMutex     sync.Mutex
}

// JobManager interface defines the methods required for job management
type JobManager interface {
	ProcessJobAssignment(ctx context.Context, assignmentData []byte) error
	StopJob(taskID string) error
	RunManualBenchmark(ctx context.Context, binaryPath string, hashType int, attackMode int) (*jobs.BenchmarkResult, error)
	ForceCleanup() error
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
	
	// Initialize hardware monitor
	hwMonitor, err := hardware.NewMonitor(cfg.DataDirectory)
	if err != nil {
		debug.Error("Failed to create hardware monitor: %v", err)
		return nil, fmt.Errorf("failed to create hardware monitor: %w", err)
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
		urlConfig:  urlConfig,
		hwMonitor:  hwMonitor,
		outbound:   make(chan *WSMessage, 4096),
		done:       make(chan struct{}),
		tlsConfig:  tlsConfig,
		syncStatus: "pending",
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

			// Use context without timeout for job execution
			// Jobs should run until completion, not be limited by arbitrary timeouts
			ctx := context.Background()

			if err := c.jobManager.ProcessJobAssignment(ctx, msg.Payload); err != nil {
				debug.Error("Failed to process job assignment: %v", err)

				// Extract task ID from the assignment payload to report failure to backend
				var assignment struct {
					TaskID string `json:"task_id"`
				}
				if unmarshalErr := json.Unmarshal(msg.Payload, &assignment); unmarshalErr == nil && assignment.TaskID != "" {
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

		case WSTypeJobStop:
			// Server requested to stop a job
			debug.Info("Received job stop command")

			if c.jobManager == nil {
				debug.Error("Job manager not initialized, cannot process job stop")
				continue
			}

			var stopPayload struct {
				TaskID string `json:"task_id"`
				Reason string `json:"reason,omitempty"`
			}
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

			if err := c.jobManager.StopJob(stopPayload.TaskID); err != nil {
				debug.Error("Failed to stop job %s: %v", stopPayload.TaskID, err)
				console.Error("Failed to stop task %s: %v", stopPayload.TaskID, err)
			} else {
				debug.Info("Successfully stopped job %s", stopPayload.TaskID)
				console.Success("Task %s stopped successfully", stopPayload.TaskID)
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
					fileInfo := &filesync.FileInfo{
						Name:     hashlistFileName,
						FileType: "hashlist",
						ID:       int(benchmarkPayload.HashlistID),
						MD5Hash:  "", // Empty hash means skip verification
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
					TaskID:          benchmarkPayload.TaskID,
					HashlistID:      benchmarkPayload.HashlistID,
					HashlistPath:    benchmarkPayload.HashlistPath,
					AttackMode:      benchmarkPayload.AttackMode,
					HashType:        benchmarkPayload.HashType,
					WordlistPaths:   benchmarkPayload.WordlistPaths,
					RulePaths:       benchmarkPayload.RulePaths,
					Mask:            benchmarkPayload.Mask,
					BinaryPath:      benchmarkPayload.BinaryPath,
					ReportInterval:  5, // Default status interval
					ExtraParameters: benchmarkPayload.ExtraParameters, // Agent-specific parameters
					EnabledDevices:  benchmarkPayload.EnabledDevices,   // Device list
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

				// Get the hashcat executor from job manager
				executor := c.jobManager.(*jobs.JobManager).GetHashcatExecutor()
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		hasTask, taskID, jobID, keyspaceProcessed := jm.GetCurrentTaskStatus()
		statusPayload.HasRunningTask = hasTask
		statusPayload.TaskID = taskID
		statusPayload.JobID = jobID
		statusPayload.KeyspaceProcessed = keyspaceProcessed
		if hasTask {
			statusPayload.Status = "running"
		} else {
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

	payload, _ := json.Marshal(map[string]interface{}{
		"agent_id":     c.agentID,
		"files_synced": completed,
		"files_failed": failed,
		"files_total":  total,
	})

	message := WSMessage{
		Type:      "sync_completed",
		Payload:   payload,
		Timestamp: time.Now(),
	}

	select {
	case c.outbound <- &message:
		debug.Info("Sent sync completed message: %d succeeded, %d failed out of %d total", completed, failed, total)
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
	debug.Info("Starting device detection using hashcat")
	
	// Detect devices using hashcat
	result, err := c.hwMonitor.DetectDevices()
	if err != nil {
		debug.Error("Failed to detect devices: %v", err)
		// Send error status to server
		errorPayload := map[string]interface{}{
			"error": err.Error(),
			"status": "error",
		}
		errorJSON, _ := json.Marshal(errorPayload)
		
		msg := &WSMessage{
			Type:      WSTypeDeviceDetection,
			Payload:   errorJSON,
			Timestamp: time.Now(),
		}
		
		// Use safeSendMessage to avoid concurrent writes
		if !c.safeSendMessage(msg, 5000) {
			debug.Error("Failed to send device detection error")
		}
		
		return err
	}
	
	// Marshal device detection result
	devicesJSON, err := json.Marshal(result)
	if err != nil {
		debug.Error("Failed to marshal device detection result: %v", err)
		return fmt.Errorf("failed to marshal device detection result: %w", err)
	}
	
	// Send device information to server
	msg := &WSMessage{
		Type:      WSTypeDeviceDetection,
		Payload:   devicesJSON,
		Timestamp: time.Now(),
	}
	
	// Use safeSendMessage to avoid concurrent writes
	if !c.safeSendMessage(msg, 5000) {
		debug.Error("Failed to send device detection result")
		return fmt.Errorf("failed to send device detection result: channel blocked or timeout")
	}
	
	debug.Info("Successfully sent device detection result with %d devices", len(result.Devices))

	// Mark devices as detected
	c.deviceMutex.Lock()
	c.devicesDetected = true
	c.deviceMutex.Unlock()

	return nil
}

// TryDetectDevicesIfNeeded attempts to detect devices if they haven't been detected yet and a binary is available
func (c *Connection) TryDetectDevicesIfNeeded() {
	// Check if we've already detected devices
	c.deviceMutex.Lock()
	alreadyDetected := c.devicesDetected
	c.deviceMutex.Unlock()

	if alreadyDetected {
		debug.Info("Devices already detected, skipping detection")
		return
	}

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
