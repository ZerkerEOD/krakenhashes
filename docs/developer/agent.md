# KrakenHashes Agent Development Guide

## Table of Contents

1. [Agent Architecture Overview](#agent-architecture-overview)
2. [Hardware Detection System](#hardware-detection-system)
3. [Job Execution Flow](#job-execution-flow)
4. [WebSocket Communication](#websocket-communication)
5. [File Synchronization](#file-synchronization)
6. [Metrics Collection](#metrics-collection)
7. [Adding New Features](#adding-new-features)
8. [Testing Agents](#testing-agents)

## Agent Architecture Overview

The KrakenHashes agent is a distributed compute node that executes password cracking jobs using hashcat. It's built with Go and designed to be cross-platform, supporting various GPU and CPU configurations.

### Core Components

```
agent/
├── cmd/agent/          # Entry point and initialization
├── internal/
│   ├── agent/         # Core agent logic and WebSocket connection
│   ├── auth/          # API key and certificate management
│   ├── config/        # Configuration management
│   ├── hardware/      # GPU/CPU detection using hashcat
│   ├── jobs/          # Job execution and hashcat management
│   ├── metrics/       # System metrics collection
│   ├── sync/          # File synchronization with backend
│   └── status/        # Agent status management
└── pkg/debug/         # Debug logging utilities
```

### Agent Lifecycle

```go
// From cmd/agent/main.go
func main() {
    // 1. Initialize debug logging
    debug.Reinitialize()
    
    // 2. Load configuration from .env
    cfg := loadConfig()
    
    // 3. Initialize data directories
    dataDirs, err := config.GetDataDirs()
    
    // 4. Create metrics collector
    collector, err := metrics.New(metrics.Config{
        CollectionInterval: time.Duration(cfg.heartbeatInterval) * time.Second,
        EnableGPU:          true,
    })
    
    // 5. Load or register agent credentials
    agentID, cert, err := agent.LoadCredentials()
    
    // 6. Create WebSocket connection
    conn, err := agent.NewConnection(urlConfig)
    
    // 7. Create job manager with hardware monitor
    hwMonitor := conn.GetHardwareMonitor()
    jobManager = jobs.NewJobManager(agentConfig, nil, hwMonitor)
    
    // 8. Start connection and maintenance
    conn.Start()
    
    // 9. Detect and send device information
    conn.DetectAndSendDevices()
}
```

## Hardware Detection System

The agent uses hashcat's built-in device detection to identify available compute devices (GPUs and CPUs).

### Device Detection Implementation

```go
// From internal/hardware/hashcat_detector.go
type HashcatDetector struct {
    binaryPath     string
    dataDirectory  string
}

func (d *HashcatDetector) DetectDevices() (*types.DeviceDetectionResult, error) {
    // Build hashcat command with device info flags
    args := []string{"-I", "--machine-readable", "--quiet"}
    
    cmd := exec.CommandContext(ctx, d.binaryPath, args...)
    output, err := cmd.Output()
    
    // Parse hashcat output
    devices, backends, err := d.parseHashcatOutput(string(output))
    
    return &types.DeviceDetectionResult{
        Devices:          devices,
        OpenCLBackends:   backends,
        DetectedAt:       time.Now(),
        HashcatVersion:   d.getHashcatVersion(),
    }, nil
}
```

### Device Structure

```go
// From internal/hardware/types/device.go
type Device struct {
    ID          int    `json:"id"`
    Type        string `json:"type"`        // "gpu" or "cpu"
    Brand       string `json:"brand"`       // "nvidia", "amd", "intel"
    Name        string `json:"name"`
    Processor   string `json:"processor"`
    Memory      int64  `json:"memory"`      // In bytes
    DriverVersion string `json:"driver_version"`
    
    // Performance characteristics
    PCIeBus     string `json:"pcie_bus"`
    CoreCount   int    `json:"core_count"`
    ClockSpeed  int    `json:"clock_speed"` // In MHz
    
    // Runtime state
    Enabled     bool   `json:"enabled"`
}
```

### Hardware Monitor

```go
// From internal/hardware/monitor.go
type Monitor struct {
    mu             sync.RWMutex
    devices        []types.Device
    hashcatDetector *HashcatDetector
}

// Detect devices using hashcat
func (m *Monitor) DetectDevices() (*types.DeviceDetectionResult, error) {
    result, err := m.hashcatDetector.DetectDevices()
    if err != nil {
        return nil, err
    }
    
    // Store devices in monitor
    m.mu.Lock()
    m.devices = result.Devices
    m.mu.Unlock()
    
    return result, nil
}

// Get enabled device flags for hashcat (-d parameter)
func (m *Monitor) GetEnabledDeviceFlags() string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    return BuildDeviceFlags(m.devices)
}
```

## Job Execution Flow

The agent executes hashcat jobs based on task assignments from the backend.

### Job Manager

```go
// From internal/jobs/jobs.go
type JobManager struct {
    executor         *HashcatExecutor
    config           *config.Config
    progressCallback func(*JobProgress)
    outputCallback   func(taskID string, output string, isError bool)
    fileSync         *filesync.FileSync
    hwMonitor        HardwareMonitor
    
    mutex           sync.RWMutex
    activeJobs      map[string]*JobExecution
}

// Process a job assignment from the backend
func (jm *JobManager) ProcessJobAssignment(ctx context.Context, assignmentData []byte) error {
    var assignment JobTaskAssignment
    err := json.Unmarshal(assignmentData, &assignment)
    
    // 1. Ensure hashlist is available
    err = jm.ensureHashlist(ctx, &assignment)
    
    // 2. Ensure rule chunks are available if needed
    err = jm.ensureRuleChunks(ctx, &assignment)
    
    // 3. Start job execution
    process, err := jm.executor.ExecuteTask(ctx, &assignment)
    
    // 4. Monitor job progress
    go jm.monitorJobProgress(ctx, jobExecution)
    
    return nil
}
```

### Hashcat Executor

```go
// From internal/jobs/hashcat_executor.go
type HashcatExecutor struct {
    dataDirectory      string
    activeProcesses    map[string]*HashcatProcess
    mu                 sync.RWMutex
    agentExtraParams   string
    deviceFlagsCallback func() string
}

// Execute a hashcat task
func (e *HashcatExecutor) ExecuteTask(ctx context.Context, assignment *JobTaskAssignment) (*HashcatProcess, error) {
    // 1. Build hashcat command
    args := e.buildHashcatCommand(assignment)
    
    // 2. Create process with output capture
    cmd := exec.CommandContext(ctx, binaryPath, args...)
    
    // 3. Set up output pipes
    stdout, _ := cmd.StdoutPipe()
    stderr, _ := cmd.StderrPipe()
    
    // 4. Start the process
    err := cmd.Start()
    
    // 5. Monitor output and parse progress
    go e.monitorOutput(process, stdout, stderr)
    
    return process, nil
}

// Build hashcat command with all parameters
func (e *HashcatExecutor) buildHashcatCommand(assignment *JobTaskAssignment) []string {
    args := []string{
        "-m", strconv.Itoa(assignment.HashType),
        "-a", strconv.Itoa(assignment.AttackMode),
        "--status",
        "--status-json",
        "--status-timer", strconv.Itoa(assignment.ReportInterval),
    }
    
    // Add device flags if callback is set
    if e.deviceFlagsCallback != nil {
        deviceFlags := e.deviceFlagsCallback()
        if deviceFlags != "" {
            args = append(args, "-d", deviceFlags)
        }
    }
    
    // Add skip and limit for keyspace splitting
    if assignment.KeyspaceStart > 0 {
        args = append(args, "-s", strconv.FormatInt(assignment.KeyspaceStart, 10))
    }
    if assignment.KeyspaceEnd > assignment.KeyspaceStart {
        limit := assignment.KeyspaceEnd - assignment.KeyspaceStart
        args = append(args, "-l", strconv.FormatInt(limit, 10))
    }
    
    return args
}
```

### Job Progress Tracking

```go
// From internal/jobs/types.go
type JobProgress struct {
    TaskID            string         `json:"task_id"`
    Status            string         `json:"status"`
    Progress          float64        `json:"progress"`
    Speed             int64          `json:"speed"`
    DeviceSpeeds      []DeviceSpeed  `json:"device_speeds"`
    TimeRemaining     int            `json:"time_remaining"`
    KeyspaceProcessed int64          `json:"keyspace_processed"`
    CrackedCount      int            `json:"cracked_count"`
    CrackedHashes     []CrackedHash  `json:"cracked_hashes,omitempty"`
    ErrorMessage      string         `json:"error_message,omitempty"`
}
```

## WebSocket Communication

The agent maintains a persistent WebSocket connection with the backend for real-time communication.

### Connection Management

```go
// From internal/agent/connection.go
type Connection struct {
    ws              *websocket.Conn
    urlConfig       *config.URLConfig
    hwMonitor       *hardware.Monitor
    outbound        chan *WSMessage
    done            chan struct{}
    isConnected     atomic.Bool
    tlsConfig       *tls.Config
    fileSync        *filesync.FileSync
    jobManager      JobManager
}

// WebSocket message types
type WSMessageType string

const (
    WSTypeHardwareInfo         WSMessageType = "hardware_info"
    WSTypeMetrics              WSMessageType = "metrics"
    WSTypeHeartbeat            WSMessageType = "heartbeat"
    WSTypeAgentStatus          WSMessageType = "agent_status"
    WSTypeFileSyncRequest      WSMessageType = "file_sync_request"
    WSTypeFileSyncResponse     WSMessageType = "file_sync_response"
    WSTypeTaskAssignment       WSMessageType = "task_assignment"
    WSTypeJobProgress          WSMessageType = "job_progress"
    WSTypeJobStop              WSMessageType = "job_stop"
    WSTypeBenchmarkRequest     WSMessageType = "benchmark_request"
    WSTypeBenchmarkResult      WSMessageType = "benchmark_result"
    WSTypeHashcatOutput        WSMessageType = "hashcat_output"
    WSTypeDeviceDetection      WSMessageType = "device_detection"
    WSTypeDeviceUpdate         WSMessageType = "device_update"
    WSTypeCrackBatch           WSMessageType = "crack_batch"
    WSTypeCrackBatchesComplete WSMessageType = "crack_batches_complete"  // NEW in v1.3.1
)
```

### Message Handling

```go
// From internal/agent/connection.go - readPump method
func (c *Connection) readPump() {
    for {
        var msg WSMessage
        err := c.ws.ReadJSON(&msg)
        
        switch msg.Type {
        case WSTypeTaskAssignment:
            // Process job assignment
            if err := c.jobManager.ProcessJobAssignment(ctx, msg.Payload); err != nil {
                debug.Error("Failed to process job assignment: %v", err)
            }
            
        case WSTypeFileSyncRequest:
            // Handle file sync request
            go c.handleFileSyncAsync(requestPayload)
            
        case WSTypeBenchmarkRequest:
            // Run speed test
            go func() {
                totalSpeed, deviceSpeeds, err := executor.RunSpeedTest(ctx, assignment, testDuration)
                // Send results back
            }()
            
        case WSTypeDeviceUpdate:
            // Update device enabled/disabled state
            c.hwMonitor.UpdateDeviceStatus(updatePayload.DeviceID, updatePayload.Enabled)
        }
    }
}
```

### Sending Updates

```go
// Send job progress to backend
func (c *Connection) SendJobProgress(progress *jobs.JobProgress) error {
    progressJSON, err := json.Marshal(progress)
    
    msg := &WSMessage{
        Type:      WSTypeJobProgress,
        Payload:   progressJSON,
        Timestamp: time.Now(),
    }
    
    select {
    case c.outbound <- msg:
        return nil
    case <-time.After(5 * time.Second):
        return fmt.Errorf("failed to queue job progress: channel blocked")
    }
}
```

### Crack Batch Workflow

The agent sends cracked passwords in batches and signals completion to enable the backend's processing status workflow.

**Message Flow:**

1. **Task Completes**: Agent sends final `job_progress` with `Status="completed"` and `CrackedCount` field
2. **Send Crack Batches**: Agent sends one or more `crack_batch` messages with cracked passwords
3. **Signal Completion**: Agent sends `crack_batches_complete` to signal all batches sent
4. **Agent Available**: Agent is immediately available for new work

**Implementation:**

```go
// Send final progress with crack count
progress := &JobProgress{
    TaskID:          taskID,
    Status:          "completed",
    ProgressPercent: 100.0,
    CrackedCount:    totalCracks,  // Expected number of cracks
}
c.SendJobProgress(progress)

// Send crack batches (batched during execution)
for _, batch := range crackBatches {
    crackBatch := &CrackBatch{
        TaskID:        taskID,
        CrackedHashes: batch,
    }
    c.SendCrackBatch(crackBatch)
}

// Signal all batches sent
completion := &CrackBatchesComplete{
    TaskID: taskID,
}
c.SendCrackBatchesComplete(completion)
```

**Message Structures:**

```go
// Final progress message
type JobProgress struct {
    TaskID          string  `json:"task_id"`
    Status          string  `json:"status"`           // "completed"
    ProgressPercent float64 `json:"progress_percent"` // 100.0
    CrackedCount    int     `json:"cracked_count"`    // Expected cracks
    // ... other fields
}

// Crack batch message
type CrackBatch struct {
    TaskID        string         `json:"task_id"`
    CrackedHashes []CrackedHash  `json:"cracked_hashes"`
}

type CrackedHash struct {
    Hash         string `json:"hash"`
    Plaintext    string `json:"plaintext"`
    OriginalLine string `json:"original_line"`
}

// Completion signal message
type CrackBatchesComplete struct {
    TaskID string `json:"task_id"`
}
```

**Backend Processing:**

When backend receives `crack_batches_complete`:
1. Task transitions from `processing` to `completed`
2. Backend verifies: `received_crack_count >= expected_crack_count`
3. Agent busy status cleared
4. Agent can immediately accept new tasks

See [Crack Batching System](../reference/architecture/crack-batching-system.md) for full details on batching logic and performance optimization.

## File Synchronization

The agent synchronizes wordlists, rules, and binaries with the backend.

### File Sync Implementation

```go
// From internal/sync/sync.go
type FileSync struct {
    client     *http.Client
    urlConfig  *config.URLConfig
    dataDirs   *config.DataDirs
    sem        chan struct{} // Semaphore for concurrent downloads
    maxRetries int
    apiKey     string
    agentID    string
}

// Download a file from the backend
func (fs *FileSync) DownloadFileFromInfo(ctx context.Context, fileInfo *FileInfo) error {
    // 1. Check if file already exists with correct hash
    if fs.fileExistsWithHash(fileInfo) {
        return nil
    }
    
    // 2. Build download URL
    url := fs.buildDownloadURL(fileInfo)
    
    // 3. Create authenticated request
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("X-API-Key", fs.apiKey)
    req.Header.Set("X-Agent-ID", fs.agentID)
    
    // 4. Download to temporary file
    tempPath := finalPath + ".tmp"
    err := fs.downloadToFile(req, tempPath)
    
    // 5. Verify hash
    if !fs.verifyFileHash(tempPath, fileInfo.MD5Hash) {
        return fmt.Errorf("hash mismatch")
    }
    
    // 6. Move to final location
    os.Rename(tempPath, finalPath)
    
    // 7. Extract if it's a binary archive
    if fileInfo.FileType == "binary" && strings.HasSuffix(fileInfo.Name, ".7z") {
        fs.ExtractBinary7z(finalPath, targetDir)
    }
    
    return nil
}
```

### Directory Structure

```go
// From internal/config/dirs.go
type DataDirs struct {
    Binaries   string // /path/to/data/binaries
    Wordlists  string // /path/to/data/wordlists
    Rules      string // /path/to/data/rules
    Hashlists  string // /path/to/data/hashlists
}

// Wordlist categories:
// - general/     # Common wordlists
// - specialized/ # Domain-specific lists
// - targeted/    # Custom lists for specific targets
// - custom/      # User-uploaded lists

// Rule categories:
// - hashcat/     # Hashcat rule files
// - john/        # John the Ripper rules
// - custom/      # User-created rules
// - chunks/      # Split rule files for distributed processing
```

## Metrics Collection

The agent collects system metrics for monitoring and optimization.

### Metrics Collector

```go
// From internal/metrics/collector.go
type Collector struct {
    interval   time.Duration
    gpuEnabled bool
}

// Collect system metrics
func (c *Collector) Collect() (*SystemMetrics, error) {
    metrics := &SystemMetrics{}
    
    // CPU metrics using gopsutil
    percentage, _ := cpu.Percent(time.Second, false)
    metrics.CPUUsage = percentage[0]
    
    // Memory metrics
    vmem, _ := mem.VirtualMemory()
    metrics.MemoryUsage = vmem.UsedPercent
    
    // GPU metrics come from hashcat JSON status
    // during job execution
    
    return metrics, nil
}
```

### Metrics Data Structure

```go
// From internal/agent/connection.go
type MetricsData struct {
    AgentID     int               `json:"agent_id"`
    CollectedAt time.Time         `json:"collected_at"`
    CPUs        []CPUMetrics      `json:"cpus"`
    GPUs        []GPUMetrics      `json:"gpus"`
    Memory      MemoryMetrics     `json:"memory"`
    Disk        []DiskMetrics     `json:"disk"`
    Network     []NetworkMetrics  `json:"network"`
    Process     []ProcessMetrics  `json:"process"`
}
```

## Adding New Features

### 1. Adding a New WebSocket Message Type

```go
// 1. Define the message type in connection.go
const WSTypeNewFeature WSMessageType = "new_feature"

// 2. Create payload structures
type NewFeatureRequest struct {
    Parameter1 string `json:"parameter1"`
    Parameter2 int    `json:"parameter2"`
}

// 3. Add handler in readPump
case WSTypeNewFeature:
    var payload NewFeatureRequest
    if err := json.Unmarshal(msg.Payload, &payload); err != nil {
        debug.Error("Failed to parse new feature: %v", err)
        continue
    }
    
    // Handle the feature
    go c.handleNewFeature(payload)

// 4. Implement the handler
func (c *Connection) handleNewFeature(payload NewFeatureRequest) {
    // Implementation
}
```

### 2. Adding Hardware Support

```go
// 1. Update device detection in hashcat_detector.go
func (d *HashcatDetector) parseHashcatOutput(output string) ([]types.Device, []types.OpenCLBackend, error) {
    // Add parsing for new hardware types
    
    // Example: Detect new GPU vendor
    if strings.Contains(line, "NewVendor") {
        device.Brand = "newvendor"
        device.Type = "gpu"
    }
}

// 2. Add device-specific handling
func BuildDeviceFlags(devices []types.Device) string {
    // Add logic for new device types
}
```

### 3. Adding Job Features

```go
// 1. Update JobTaskAssignment structure
type JobTaskAssignment struct {
    // Existing fields...
    
    NewFeature string `json:"new_feature"`
}

// 2. Update hashcat command building
func (e *HashcatExecutor) buildHashcatCommand(assignment *JobTaskAssignment) []string {
    // Add new hashcat parameters
    if assignment.NewFeature != "" {
        args = append(args, "--new-feature", assignment.NewFeature)
    }
}

// 3. Update progress monitoring if needed
func (e *HashcatExecutor) parseHashcatStatus(status map[string]interface{}) *JobProgress {
    // Parse new status fields
}
```

## Testing Agents

### Unit Testing

```go
// Example from agent_test.go
func TestGetAgentID(t *testing.T) {
    tests := []struct {
        name        string
        setupFunc   func(configDir string) error
        expectedID  int
        wantErr     bool
    }{
        {
            name: "successful ID retrieval",
            setupFunc: func(configDir string) error {
                return auth.SaveAgentKey(configDir, "test-api-key", "456")
            },
            expectedID: 456,
            wantErr:    false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tempDir := t.TempDir()
            t.Setenv("KH_CONFIG_DIR", tempDir)
            
            if tt.setupFunc != nil {
                err := tt.setupFunc(tempDir)
                require.NoError(t, err)
            }
            
            id, err := GetAgentID()
            
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expectedID, id)
            }
        })
    }
}
```

### Integration Testing

```go
// Test WebSocket connection
func TestWebSocketConnection(t *testing.T) {
    // Create test server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        upgrader := websocket.Upgrader{}
        conn, _ := upgrader.Upgrade(w, r, nil)
        defer conn.Close()
        
        // Test message exchange
        var msg WSMessage
        conn.ReadJSON(&msg)
        assert.Equal(t, WSTypeHeartbeat, msg.Type)
        
        // Send response
        conn.WriteJSON(WSMessage{
            Type: WSTypeHeartbeat,
            Timestamp: time.Now(),
        })
    }))
    defer server.Close()
    
    // Test connection
    conn, err := NewConnection(urlConfig)
    assert.NoError(t, err)
    
    err = conn.Start()
    assert.NoError(t, err)
}
```

### Mock Testing

```go
// From internal/mocks/
type MockJobManager struct {
    mock.Mock
}

func (m *MockJobManager) ProcessJobAssignment(ctx context.Context, data []byte) error {
    args := m.Called(ctx, data)
    return args.Error(0)
}

// Use in tests
func TestJobProcessing(t *testing.T) {
    mockJM := new(MockJobManager)
    mockJM.On("ProcessJobAssignment", mock.Anything, mock.Anything).Return(nil)
    
    conn := &Connection{
        jobManager: mockJM,
    }
    
    // Test job processing
    msg := WSMessage{
        Type: WSTypeTaskAssignment,
        Payload: json.RawMessage(`{"task_id": "test-123"}`),
    }
    
    // Process message
    // Assert expectations
    mockJM.AssertExpectations(t)
}
```

### Performance Testing

```go
func BenchmarkHashcatCommandBuilding(b *testing.B) {
    executor := NewHashcatExecutor("/data")
    assignment := &JobTaskAssignment{
        TaskID:     "bench-task",
        HashType:   0,
        AttackMode: 0,
        WordlistPaths: []string{"wordlist1.txt", "wordlist2.txt"},
        RulePaths:     []string{"rule1.rule", "rule2.rule"},
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = executor.buildHashcatCommand(assignment)
    }
}
```

### Manual Testing

```bash
# Build and run agent locally
cd agent
go build -o krakenhashes-agent cmd/agent/main.go

# Run with test configuration
./krakenhashes-agent -host localhost:8080 -claim TEST-CLAIM-CODE -debug

# Test specific components
# Device detection
./krakenhashes-agent -test-devices

# File sync
./krakenhashes-agent -test-sync wordlists

# Benchmark
./krakenhashes-agent -test-benchmark -m 0 -a 0
```

## Best Practices

1. **Error Handling**: Always use the debug package for logging
   ```go
   debug.Error("Operation failed: %v", err)
   debug.Info("Operation completed successfully")
   ```

2. **Resource Management**: Always clean up resources
   ```go
   defer func() {
     if conn != nil {
         conn.Close()
     }
   }()
   ```

3. **Concurrent Operations**: Use proper synchronization
   ```go
   type SafeMap struct {
       mu   sync.RWMutex
       data map[string]interface{}
   }
   ```

4. **Context Usage**: Respect context cancellation
   ```go
   select {
   case <-ctx.Done():
       return ctx.Err()
   case result := <-resultChan:
       return result
   }
   ```

5. **Configuration**: Use environment variables
   ```go
   value := os.Getenv("KH_SETTING")
   if value == "" {
       value = "default"
   }
   ```

## Troubleshooting

Common issues and solutions:

1. **Certificate Errors**: Agent will attempt to renew certificates automatically
2. **Connection Drops**: Automatic reconnection with exponential backoff
3. **File Sync Failures**: Automatic retry with hash verification
4. **Hashcat Errors**: Check device permissions and driver installation
5. **Memory Issues**: Monitor system resources during large jobs

For debugging, enable debug mode:
```bash
export DEBUG=true
export LOG_LEVEL=DEBUG
```