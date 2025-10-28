package jobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// AttackMode represents hashcat attack modes
type AttackMode int

const (
	AttackModeStraight           AttackMode = 0 // Dictionary attack
	AttackModeCombination        AttackMode = 1 // Combination attack
	AttackModeBruteForce         AttackMode = 3 // Brute-force attack
	AttackModeHybridWordlistMask AttackMode = 6 // Hybrid Wordlist + Mask
	AttackModeHybridMaskWordlist AttackMode = 7 // Hybrid Mask + Wordlist
	
	// PID file for tracking hashcat processes
	hashcatPIDFile = "/tmp/krakenhashes-hashcat.pid"
	
	// Retry configuration for "already running" errors
	MaxHashcatRetries = 5
	HashcatRetryDelay = 5 * time.Second
)

// JobTaskAssignment represents a task assignment from the backend
type JobTaskAssignment struct {
	TaskID          string      `json:"task_id"`
	JobExecutionID  string      `json:"job_execution_id"`
	HashlistID      int64       `json:"hashlist_id"`
	HashlistPath    string      `json:"hashlist_path"`    // Local path on agent
	AttackMode      int         `json:"attack_mode"`
	HashType        int         `json:"hash_type"`
	KeyspaceStart   int64       `json:"keyspace_start"`
	KeyspaceEnd     int64       `json:"keyspace_end"`
	WordlistPaths   []string    `json:"wordlist_paths"`   // Local paths on agent
	RulePaths       []string    `json:"rule_paths"`       // Local paths on agent
	Mask            string      `json:"mask,omitempty"`   // For mask attacks
	BinaryPath      string      `json:"binary_path"`      // Hashcat binary to use
	ChunkDuration   int         `json:"chunk_duration"`   // Expected duration in seconds
	ReportInterval  int         `json:"report_interval"`  // Progress reporting interval
	OutputFormat    string      `json:"output_format"`    // Hashcat output format
	ExtraParameters string      `json:"extra_parameters,omitempty"` // Agent-specific hashcat parameters
	EnabledDevices  []int       `json:"enabled_devices,omitempty"`  // List of enabled device IDs
}

// DeviceMetric represents metrics for a single device
type DeviceMetric struct {
	DeviceID   int     `json:"device_id"`   // Device ID from hashcat
	DeviceName string  `json:"device_name"` // Human-readable device name
	Speed      int64   `json:"speed"`       // Hash rate for this device (H/s)
	Temp       float64 `json:"temp"`        // Temperature in Celsius
	Util       float64 `json:"util"`        // Utilization percentage (0-100)
	FanSpeed   float64 `json:"fan_speed"`   // Fan speed percentage (0-100)
}

// JobProgress represents progress updates sent to backend
type JobProgress struct {
	TaskID                 string         `json:"task_id"`
	KeyspaceProcessed      int64          `json:"keyspace_processed"`                   // Restore point (position in wordlist)
	EffectiveProgress      int64          `json:"effective_progress"`                   // Actual effective progress (words × rules processed)
	ProgressPercent        float64        `json:"progress_percent"`                     // Actual progress percentage (0-100)
	TotalEffectiveKeyspace *int64         `json:"total_effective_keyspace,omitempty"`   // Only sent on first update - hashcat progress[1]
	IsFirstUpdate          bool           `json:"is_first_update"`                      // Flag indicating this is the first progress update
	HashRate               int64          `json:"hash_rate"`                            // Current hashes per second
	Temperature            *float64       `json:"temperature"`                          // GPU temperature (deprecated, use DeviceMetrics)
	Utilization            *float64       `json:"utilization"`                          // GPU utilization percentage (deprecated, use DeviceMetrics)
	TimeRemaining          *int           `json:"time_remaining"`                       // Estimated seconds remaining
	CrackedCount           int            `json:"cracked_count"`                        // Number of hashes cracked in this update
	CrackedHashes          []CrackedHash  `json:"cracked_hashes"`                       // Detailed crack information
	Status                 string         `json:"status,omitempty"`                     // Task status (running, completed, failed)
	ErrorMessage           string         `json:"error_message,omitempty"`              // Error message if status is failed
	DeviceMetrics          []DeviceMetric `json:"device_metrics,omitempty"`              // Per-device metrics
	AllHashesCracked       bool           `json:"all_hashes_cracked,omitempty"`         // Flag indicating all hashes in hashlist were cracked (exit code 6)
}

// JobStatus represents status-only message (synchronous, no crack data)
type JobStatus struct {
	TaskID                 string   `json:"task_id"`
	KeyspaceProcessed      int64    `json:"keyspace_processed"`
	EffectiveProgress      int64    `json:"effective_progress"`
	ProgressPercent        float64  `json:"progress_percent"`
	TotalEffectiveKeyspace *int64   `json:"total_effective_keyspace,omitempty"`
	IsFirstUpdate          bool     `json:"is_first_update"`
	HashRate               int64    `json:"hash_rate"`
	TimeRemaining          *int     `json:"time_remaining,omitempty"`
	CrackedCount           int      `json:"cracked_count"` // Total count only, not actual hashes
	Status                 string   `json:"status,omitempty"`
	ErrorMessage           string   `json:"error_message,omitempty"`
	DeviceMetrics          []DeviceMetric `json:"device_metrics,omitempty"`
	AllHashesCracked       bool     `json:"all_hashes_cracked,omitempty"`
}

// CrackBatch represents crack-only message (asynchronous)
type CrackBatch struct {
	TaskID        string         `json:"task_id"`
	CrackedHashes []CrackedHash  `json:"cracked_hashes"`
}

// CrackedHash represents a cracked hash with all available information
type CrackedHash struct {
	Hash         string `json:"hash"`          // The original hash
	Salt         string `json:"salt"`          // Salt (if applicable)
	Plain        string `json:"plain"`         // Plain text password
	HexPlain     string `json:"hex_plain"`     // Hex representation of plain
	CrackPos     string `json:"crack_pos"`     // Position in keyspace where found
	FullLine     string `json:"full_line"`     // Full output line for reference
}

// DeviceSpeed represents speed for a single device
type DeviceSpeed struct {
	DeviceID   int    `json:"device_id"`
	DeviceName string `json:"device_name"`
	Speed      int64  `json:"speed"` // H/s for this device
}

// HashcatExecutor handles hashcat process execution and monitoring
type HashcatExecutor struct {
	dataDirectory string

	// Process management
	mutex           sync.RWMutex
	activeProcesses map[string]*HashcatProcess

	// Output callback for sending output via websocket
	outputCallback  func(taskID string, output string, isError bool)

	// Device flags callback - returns device flags for hashcat (-d flag)
	deviceFlagsCallback func() string

	// Agent's default extra parameters for hashcat
	agentExtraParams string

	// Crack batching - reduces message flood when many hashes crack simultaneously
	crackBatchMutex    sync.Mutex
	crackBatchBuffers  map[string][]CrackedHash // Buffer per task ID
	crackBatchTimers   map[string]*time.Timer   // Timer per task ID
	crackBatchInterval time.Duration            // Batch window duration (100ms)
}

// HashcatProcess represents an active hashcat process
type HashcatProcess struct {
	TaskID          string
	Assignment      *JobTaskAssignment
	Cmd             *exec.Cmd
	Cancel          context.CancelFunc
	ProgressChannel chan *JobProgress
	StatusFile      string
	PotFile         string
	OutputFile      string
	StdinPipe       io.WriteCloser

	// Process state
	IsRunning       bool
	StartTime       time.Time
	LastProgress    *JobProgress
	LastCheckpoint  time.Time

	// Hashlist tracking for crack parsing
	HashlistContent []string  // Store the hashes we're cracking

	// Outfile tracking for reliable crack capture
	OutfilePath       string            // Path to hashcat --outfile
	OutfileSentHashes map[string]bool   // Track sent hash:password lines (deduplication)
	OutfileMutex      sync.Mutex        // Protect outfile state
	OutfileOffset     int64             // Current read position in outfile

	// Error tracking
	AlreadyRunningError bool
	mutex              sync.Mutex
}


// NewHashcatExecutor creates a new hashcat executor
func NewHashcatExecutor(dataDirectory string) *HashcatExecutor {
	// We don't use a work directory since we're capturing output from stdout
	// with --potfile-disable and no output files
	
	executor := &HashcatExecutor{
		dataDirectory:      dataDirectory,
		activeProcesses:    make(map[string]*HashcatProcess),
		crackBatchBuffers:  make(map[string][]CrackedHash),
		crackBatchTimers:   make(map[string]*time.Timer),
		crackBatchInterval: 500 * time.Millisecond, // 500ms batching window (reduced frequency)
	}
	
	// Clean up any orphaned processes on startup
	if err := executor.cleanOrphanedProcesses(); err != nil {
		debug.Warning("Failed to clean orphaned processes on startup: %v", err)
	}
	
	return executor
}

// checkAndKillExistingHashcat checks if a hashcat process is already running and kills it
func (e *HashcatExecutor) checkAndKillExistingHashcat() error {
	// First check our PID file
	if pid, err := e.readPIDFile(); err == nil && pid > 0 {
		if e.isProcessRunning(pid) {
			debug.Warning("Found existing hashcat process with PID %d, attempting to kill", pid)
			if err := e.killProcess(pid); err != nil {
				return fmt.Errorf("failed to kill existing hashcat process (PID %d): %w", pid, err)
			}
			debug.Info("Successfully killed existing hashcat process (PID %d)", pid)
		}
		// Clean up the PID file
		os.Remove(hashcatPIDFile)
	}
	
	// Also check using pgrep for any hashcat processes
	cmd := exec.Command("pgrep", "-f", "hashcat")
	output, _ := cmd.Output()
	if len(output) > 0 {
		pids := strings.Fields(string(output))
		for _, pidStr := range pids {
			if pid, err := strconv.Atoi(pidStr); err == nil {
				// Skip our own process
				if pid == os.Getpid() {
					continue
				}
				debug.Warning("Found hashcat process with PID %d via pgrep, attempting to kill", pid)
				e.killProcess(pid)
			}
		}
	}
	
	return nil
}

// cleanOrphanedProcesses cleans up any orphaned hashcat processes
func (e *HashcatExecutor) cleanOrphanedProcesses() error {
	return e.checkAndKillExistingHashcat()
}

// writePIDFile writes the PID to the PID file
func (e *HashcatExecutor) writePIDFile(pid int) error {
	return ioutil.WriteFile(hashcatPIDFile, []byte(strconv.Itoa(pid)), 0644)
}

// readPIDFile reads the PID from the PID file
func (e *HashcatExecutor) readPIDFile() (int, error) {
	data, err := ioutil.ReadFile(hashcatPIDFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// isProcessRunning checks if a process with the given PID is running
func (e *HashcatExecutor) isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	
	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// killProcess kills a process with the given PID
func (e *HashcatExecutor) killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	
	// Try graceful termination first
	if err := process.Signal(syscall.SIGTERM); err == nil {
		// Wait a bit for graceful shutdown
		time.Sleep(2 * time.Second)
		
		// Check if still running
		if !e.isProcessRunning(pid) {
			return nil
		}
	}
	
	// Force kill if still running
	return process.Kill()
}

// SetOutputCallback sets the callback for sending output via websocket
func (e *HashcatExecutor) SetOutputCallback(callback func(taskID string, output string, isError bool)) {
	e.outputCallback = callback
}

// SetDeviceFlagsCallback sets the callback for getting device flags
func (e *HashcatExecutor) SetDeviceFlagsCallback(callback func() string) {
	e.deviceFlagsCallback = callback
}

// SetAgentExtraParams sets the agent's default extra parameters for hashcat
func (e *HashcatExecutor) SetAgentExtraParams(params string) {
	e.agentExtraParams = params
}

// ExecuteTask starts execution of a hashcat task
func (e *HashcatExecutor) ExecuteTask(ctx context.Context, assignment *JobTaskAssignment) (*HashcatProcess, error) {
	// For now, directly call executeTaskInternal without retry logic
	// The retry logic will be handled by the job manager monitoring the process
	// and detecting AlreadyRunningError failures
	return e.executeTaskInternal(ctx, assignment)
}

// executeTaskInternal is the internal implementation of ExecuteTask without retry logic
func (e *HashcatExecutor) executeTaskInternal(ctx context.Context, assignment *JobTaskAssignment) (*HashcatProcess, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Check if task is already running
	if _, exists := e.activeProcesses[assignment.TaskID]; exists {
		return nil, fmt.Errorf("task %s is already running", assignment.TaskID)
	}
	
	// Don't kill existing processes - we'll let hashcat gracefully shut down
	// and retry if needed

	// Create process context with cancellation
	processCtx, cancel := context.WithCancel(ctx)

	// Create outfile directory if not exists
	outfileDir := filepath.Join(e.dataDirectory, "outfile")
	if err := os.MkdirAll(outfileDir, 0755); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create outfile directory: %w", err)
	}

	// Build hashcat command
	command, statusFile, potFile, outputFile, err := e.buildHashcatCommand(assignment)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to build hashcat command: %w", err)
	}

	// Set command context - no specific directory needed
	command.Env = os.Environ()
	
	// Set up stdin pipe for sending commands to hashcat
	stdinPipe, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	// Set up stdout pipe for capturing output
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	// Set up stderr pipe for error messages
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Load the hashlist content for crack parsing
	hashlistPath := filepath.Join(e.dataDirectory, assignment.HashlistPath)
	hashlistContent, err := e.loadHashlist(hashlistPath)
	if err != nil {
		debug.Warning("Failed to load hashlist for crack parsing: %v", err)
		// Continue anyway - we'll fall back to old parsing if needed
		hashlistContent = []string{}
	}

	// Create process structure
	process := &HashcatProcess{
		TaskID:            assignment.TaskID,
		Assignment:        assignment,
		Cmd:               command,
		Cancel:            cancel,
		ProgressChannel:   make(chan *JobProgress, 100),
		StatusFile:        statusFile,
		PotFile:           potFile,
		OutputFile:        outputFile,
		StdinPipe:         stdinPipe,
		IsRunning:         false,
		StartTime:         time.Now(),
		HashlistContent:   hashlistContent,
		OutfilePath:       outputFile, // Set outfile path for monitoring
		OutfileSentHashes: make(map[string]bool), // Initialize deduplication map
		OutfileOffset:     0, // Start reading from beginning
	}

	// Store process
	e.activeProcesses[assignment.TaskID] = process

	// Show console message about starting execution
	console.Status("Starting hashcat execution for task %s", assignment.TaskID)
	debug.Info("Starting hashcat execution for task %s", assignment.TaskID)

	// Start the process in a goroutine
	go e.runHashcatProcess(processCtx, process, stdoutPipe, stderrPipe)

	return process, nil
}

// buildHashcatCommand builds the hashcat command line arguments
func (e *HashcatExecutor) buildHashcatCommand(assignment *JobTaskAssignment) (*exec.Cmd, string, string, string, error) {
	return e.buildHashcatCommandWithOptions(assignment, false)
}

// buildHashcatCommandWithOptions builds the hashcat command line arguments with options
func (e *HashcatExecutor) buildHashcatCommandWithOptions(assignment *JobTaskAssignment, isBenchmark bool) (*exec.Cmd, string, string, string, error) {
	debug.Info("Building hashcat command for task %s", assignment.TaskID)
	debug.Info("Data directory: %s", e.dataDirectory)
	debug.Info("Binary path from assignment: %s", assignment.BinaryPath)
	debug.Info("Hashlist path from assignment: %s", assignment.HashlistPath)
	
	// Since we're running distributed with --potfile-disable and capturing output from stdout,
	// we don't need to create any work files. Just return empty paths.
	statusFile := ""
	potFile := ""
	outputFile := ""

	// Base arguments
	args := []string{
		"-m", strconv.Itoa(assignment.HashType),     // Hash type
		"-a", strconv.Itoa(int(assignment.AttackMode)), // Attack mode
		"--status",                                   // Enable status output
		"--status-json",                              // Output status in JSON format
		"--status-timer", strconv.Itoa(assignment.ReportInterval), // Status update interval
		"--quiet",                                    // Reduce verbose output
		"--potfile-disable",                          // Disable potfile
		"--restore-disable",                          // Disable restore files (we handle restore via keyspace)
	}
	
	// Add device flags if specified
	// Only add -d flag if some devices are disabled (i.e., we have a specific list)
	if len(assignment.EnabledDevices) > 0 {
		// Convert device IDs to comma-separated string
		deviceIDs := make([]string, len(assignment.EnabledDevices))
		for i, id := range assignment.EnabledDevices {
			deviceIDs[i] = strconv.Itoa(id)
		}
		deviceFlags := strings.Join(deviceIDs, ",")
		debug.Info("Adding device flags to hashcat command: -d %s", deviceFlags)
		args = append(args, "-d", deviceFlags)
	}
	// If no devices specified, hashcat will use all available devices
	
	// Add extra parameters - prefer task-specific over agent defaults
	extraParams := assignment.ExtraParameters
	if extraParams == "" && e.agentExtraParams != "" {
		extraParams = e.agentExtraParams
	}
	
	if extraParams != "" {
		debug.Info("Adding extra parameters: %s", extraParams)
		// Split the extra parameters by space and append them
		extraParamsList := strings.Fields(extraParams)
		args = append(args, extraParamsList...)
	}
	
	// Only add --outfile for actual job execution, not benchmarks
	// Note: --remove flag removed as it's not needed for distributed cracking
	// The backend tracks which hashes are cracked via the database
	if !isBenchmark {
		// Add outfile for reliable crack capture
		outfileDir := filepath.Join(e.dataDirectory, "outfile")
		if err := os.MkdirAll(outfileDir, 0755); err != nil {
			return nil, "", "", "", fmt.Errorf("failed to create outfile directory: %w", err)
		}

		outfilePath := filepath.Join(outfileDir, fmt.Sprintf("%s.txt", assignment.TaskID))
		args = append(args, "--outfile", outfilePath)
		args = append(args, "--outfile-format", "1,2") // Format 1,2: hash:plain (colon-separated)
		outputFile = outfilePath // Store for return value
		debug.Info("Outfile configured: %s", outfilePath)
	}

	// Add skip and limit for keyspace distribution
	// Skip this for rule-split tasks (detected by rule chunk paths)
	isRuleSplitTask := false
	for _, rulePath := range assignment.RulePaths {
		if strings.Contains(rulePath, "chunks/job_") {
			isRuleSplitTask = true
			break
		}
	}
	
	if !isRuleSplitTask {
		if assignment.KeyspaceStart > 0 {
			args = append(args, "--skip", strconv.FormatInt(assignment.KeyspaceStart, 10))
		}
		
		if assignment.KeyspaceEnd > assignment.KeyspaceStart {
			keyspaceRange := assignment.KeyspaceEnd - assignment.KeyspaceStart
			args = append(args, "--limit", strconv.FormatInt(keyspaceRange, 10))
		}
	}

	// Add hashlist file
	hashlistPath := filepath.Join(e.dataDirectory, assignment.HashlistPath)
	
	// Debug: Check if hashlist file exists
	if _, err := os.Stat(hashlistPath); os.IsNotExist(err) {
		debug.Error("Hashlist file does not exist: %s", hashlistPath)
		return nil, "", "", "", fmt.Errorf("hashlist file not found: %s", hashlistPath)
	}
	
	args = append(args, hashlistPath)

	// Add attack-mode specific arguments
	switch assignment.AttackMode {
	case int(AttackModeStraight): // Dictionary attack
		// Add wordlists
		debug.Info("Adding wordlists to hashcat command: %v", assignment.WordlistPaths)
		for _, wordlistPath := range assignment.WordlistPaths {
			fullPath := filepath.Join(e.dataDirectory, wordlistPath)
			debug.Info("Adding wordlist: %s (full path: %s)", wordlistPath, fullPath)
			args = append(args, fullPath)
		}
		// Add rules
		debug.Info("Adding rules to hashcat command: %v", assignment.RulePaths)
		for _, rulePath := range assignment.RulePaths {
			fullPath := filepath.Join(e.dataDirectory, rulePath)
			debug.Info("Adding rule: %s (full path: %s)", rulePath, fullPath)
			args = append(args, "-r", fullPath)
		}

	case int(AttackModeCombination): // Combination attack
		if len(assignment.WordlistPaths) >= 2 {
			wordlist1 := filepath.Join(e.dataDirectory, assignment.WordlistPaths[0])
			wordlist2 := filepath.Join(e.dataDirectory, assignment.WordlistPaths[1])
			args = append(args, wordlist1, wordlist2)
		}

	case int(AttackModeBruteForce): // Mask attack
		if assignment.Mask != "" {
			args = append(args, assignment.Mask)
		}

	case int(AttackModeHybridWordlistMask): // Hybrid Wordlist + Mask
		if len(assignment.WordlistPaths) > 0 && assignment.Mask != "" {
			wordlistPath := filepath.Join(e.dataDirectory, assignment.WordlistPaths[0])
			args = append(args, wordlistPath, assignment.Mask)
		}

	case int(AttackModeHybridMaskWordlist): // Hybrid Mask + Wordlist
		if assignment.Mask != "" && len(assignment.WordlistPaths) > 0 {
			wordlistPath := filepath.Join(e.dataDirectory, assignment.WordlistPaths[0])
			args = append(args, assignment.Mask, wordlistPath)
		}

	default:
		return nil, "", "", "", fmt.Errorf("unsupported attack mode: %d", assignment.AttackMode)
	}

	// Resolve the hashcat binary path
	hashcatBinary, err := e.resolveHashcatBinary(assignment.BinaryPath)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("failed to resolve hashcat binary: %w", err)
	}
	
	debug.Info("Using hashcat binary: %s", hashcatBinary)
	debug.Info("Full hashcat command: %s %s", hashcatBinary, strings.Join(args, " "))
	
	// Create command
	cmd := exec.Command(hashcatBinary, args...)
	
	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(hashcatBinary)
	debug.Info("Setting working directory to: %s", cmd.Dir)

	return cmd, statusFile, potFile, outputFile, nil
}

// runHashcatProcess executes and monitors a hashcat process
func (e *HashcatExecutor) runHashcatProcess(ctx context.Context, process *HashcatProcess, stdoutPipe, stderrPipe io.ReadCloser) {
	defer func() {
		// Final outfile read to catch any remaining cracks
		if process.OutfilePath != "" {
			debug.Info("Performing final outfile read for task %s", process.TaskID)
			e.readNewOutfileLines(process)

			// Flush any pending crack batches immediately
			debug.Info("Flushing pending crack batches for task %s", process.TaskID)
			e.cleanupBatchState(process)

			// Wait briefly for any batch timers that just fired to complete
			// This prevents race condition where timer fires after final read but before cleanup
			time.Sleep(150 * time.Millisecond)
			debug.Info("Batch flush wait completed for task %s", process.TaskID)

			// Don't delete outfile here - let monitorOutfile handle it after confirming all batches sent
			// This prevents premature deletion when processing large numbers of cracks (e.g., 1.75M)
			// if err := os.Remove(process.OutfilePath); err != nil {
			// 	debug.Warning("Failed to delete outfile %s: %v", process.OutfilePath, err)
			// } else {
			// 	debug.Info("Deleted outfile artifact: %s", process.OutfilePath)
			// }
		}

		e.mutex.Lock()
		delete(e.activeProcesses, process.TaskID)
		e.mutex.Unlock()
		close(process.ProgressChannel)
		if process.StdinPipe != nil {
			process.StdinPipe.Close()
		}

		// Clean up PID file
		os.Remove(hashcatPIDFile)

		// Ensure the process is killed if still running
		if process.Cmd != nil && process.Cmd.Process != nil {
			// Send SIGTERM first
			process.Cmd.Process.Signal(syscall.SIGTERM)
			// Give it a moment to exit gracefully
			time.Sleep(100 * time.Millisecond)
			// Force kill if needed
			process.Cmd.Process.Kill()
		}
	}()

	// Start output readers before starting the process
	outputDone := make(chan bool, 2)
	
	// Read stdout for JSON status and cracked hashes
	go func() {
		defer func() {
			debug.Info("[Hashcat stdout reader] Goroutine exiting for task %s", process.TaskID)
			// Send completion signal safely
			select {
			case outputDone <- true:
			default:
			}
		}()
		scanner := bufio.NewScanner(stdoutPipe)
		// Increase buffer size to 1MB to handle large JSON status outputs
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		
		debug.Info("[Hashcat stdout reader] Starting for task %s", process.TaskID)
		lineCount := 0
		
		for scanner.Scan() {
			line := scanner.Text()
			lineCount++
			debug.Debug("[Hashcat stdout raw] %s", line)

			// Store original line for outputCallback
			originalLine := line

			// Pre-check: Is this a standalone crack line (not JSON, contains colon, not "Skipping")?
			// We need to detect this BEFORE calling outputCallback
			if strings.Contains(line, ":") && !strings.HasPrefix(line, "{") && !strings.Contains(line, "Skipping") && !strings.Contains(line, "\"status\"") {
				// Try to parse as crack
				cracked := e.parseCrackedHash(line, process.HashlistContent)
				if cracked != nil {
					// This is a crack line - skip outputCallback and add to batch
					e.addCrackToBatch(process, cracked)
					debug.Info("[Hashcat cracked] Hash: %s, Plain: %s",
						cracked.Hash, cracked.Plain)
					// Skip the rest of processing for this line
					continue
				}
			}

			// Sometimes hashcat outputs crack result and JSON on same line
			// Check if line contains both crack and JSON
			if strings.Contains(line, ":") && strings.Contains(line, "{") && strings.Contains(line, "\"status\"") {
				// Split at the JSON start
				jsonStart := strings.Index(line, "{")
				crackPart := strings.TrimSpace(line[:jsonStart])
				jsonPart := line[jsonStart:]

				// Process crack part first
				if len(crackPart) > 0 {
					cracked := e.parseCrackedHash(crackPart, process.HashlistContent)
					if cracked != nil {
						// Add crack to batch instead of sending immediately
						e.addCrackToBatch(process, cracked)
						debug.Info("[Hashcat cracked] Hash: %s, Plain: %s",
							cracked.Hash, cracked.Plain)
						// For combined lines, still send the JSON part via outputCallback
					}
				}

				// Now process JSON part
				line = jsonPart
			}

			// Send output via websocket if callback is set
			// (Standalone crack lines already handled above with continue)
			if e.outputCallback != nil {
				e.outputCallback(process.TaskID, originalLine, false)
			}
			
			// Check if this is a JSON status line
			if strings.HasPrefix(line, "{") && strings.Contains(line, "\"status\"") {
				// This is a JSON status update
				// Fix hashcat's invalid JSON - it outputs device_id with leading zeros like 01, 02
				fixedLine := line
				re := regexp.MustCompile(`"device_id":\s*0+(\d+)`)
				fixedLine = re.ReplaceAllString(fixedLine, `"device_id": $1`)
				
				var status map[string]interface{}
				if err := json.Unmarshal([]byte(fixedLine), &status); err == nil {
					// Check if this is a final status update and detect if all hashes are cracked
					var allHashesCracked bool
					if statusCode, ok := status["status"].(float64); ok {
						debug.Info("[Hashcat status] Status code: %d (3=Running, 5=Exhausted, 6=Cracked)", int(statusCode))

						// Status code 6 means all hashes cracked with --remove flag
						if int(statusCode) == 6 {
							debug.Info("[Hashcat] Status code 6 detected - all hashes in hashlist are cracked")
							allHashesCracked = true
						}

						// Status codes: 3=Running, 5=Exhausted, 6=Cracked, 7=Aborted, etc.
						if int(statusCode) != 3 {
							debug.Info("[Hashcat] Final status detected: %d", int(statusCode))
							// This is a final status, make sure to process it
						}
					}

					// Extract key metrics from JSON
					if progressArr, ok := status["progress"].([]interface{}); ok && len(progressArr) >= 2 {
						// Extract restore point for resume capability (position in wordlist)
						var keyspaceProcessed int64
						if restorePoint, ok := status["restore_point"].(float64); ok {
							keyspaceProcessed = int64(restorePoint)
						}

						// Extract progress values for percentage calculation
						var currentProgress, totalProgress int64
						if current, ok := progressArr[0].(float64); ok {
							currentProgress = int64(current)  // Current position (words * rules processed)
						}
						if total, ok := progressArr[1].(float64); ok {
							totalProgress = int64(total)  // Total to process (total words * total rules) - this is progress[1]
						}

						// Calculate progress percentage
						var progressPercent float64
						if totalProgress > 0 {
							progressPercent = (float64(currentProgress) / float64(totalProgress)) * 100
						}

						// Determine if this is the first progress update
						isFirstUpdate := process.LastProgress == nil || process.LastProgress.EffectiveProgress == 0

						progress := &JobProgress{
							TaskID:            process.TaskID,
							KeyspaceProcessed: keyspaceProcessed,  // Restore point (word position)
							EffectiveProgress: currentProgress,     // Actual effective progress
							ProgressPercent:   progressPercent,     // Actual progress percentage
							IsFirstUpdate:     isFirstUpdate,       // Flag indicating first update
							AllHashesCracked:  allHashesCracked,    // Flag when status code 6 detected
						}

						// Always include total effective keyspace from hashcat
						if totalProgress > 0 {
							progress.TotalEffectiveKeyspace = &totalProgress  // Hashcat's progress[1]
						}
						
						// Extract metrics from all devices
						var totalSpeed int64
						var deviceMetrics []DeviceMetric
						if devices, ok := status["devices"].([]interface{}); ok {
							for i, dev := range devices {
								if device, ok := dev.(map[string]interface{}); ok {
									metric := DeviceMetric{}
									
									// Extract device ID
									if deviceID, ok := device["device_id"].(float64); ok {
										metric.DeviceID = int(deviceID)
									}
									
									// Extract device name
									if deviceName, ok := device["device_name"].(string); ok {
										metric.DeviceName = deviceName
									}
									
									// Extract speed
									if speed, ok := device["speed"].(float64); ok {
										metric.Speed = int64(speed)
										totalSpeed += int64(speed)
									}
									
									// Extract temperature
									if temp, ok := device["temp"].(float64); ok {
										metric.Temp = temp
										// Keep backward compatibility - use first device for legacy fields
										if i == 0 {
											progress.Temperature = &temp
										}
									}
									
									// Extract utilization
									if util, ok := device["util"].(float64); ok {
										metric.Util = util
										// Keep backward compatibility - use first device for legacy fields
										if i == 0 {
											progress.Utilization = &util
										}
									}
									
									// Extract fan speed
									if fanspeed, ok := device["fanspeed"].(float64); ok {
										metric.FanSpeed = fanspeed
									}
									
									deviceMetrics = append(deviceMetrics, metric)
								}
							}
						}
						progress.HashRate = totalSpeed
						progress.DeviceMetrics = deviceMetrics
						
						// Calculate time remaining based on actual progress
						if totalProgress > 0 && currentProgress < totalProgress && progress.HashRate > 0 {
							remaining := totalProgress - currentProgress
							if remaining > 0 {
								timeRemaining := int(remaining / progress.HashRate)
								progress.TimeRemaining = &timeRemaining
							}
						}
						
						e.sendProgressUpdate(process, progress, "running")
						// Update last progress and checkpoint on the process
						process.LastProgress = progress
						process.LastCheckpoint = time.Now()
					}
				} else {
					debug.Warning("[Hashcat] Failed to parse JSON status: %v", err)
				}
			} else {
				// Not JSON - could be informational output
				// (Crack lines are already handled at the beginning of the loop)
				debug.Debug("[Hashcat stdout] %s", line)
			}
		}
		
		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			debug.Error("[Hashcat stdout reader] Scanner error after %d lines: %v", lineCount, err)
			e.sendErrorProgress(process, fmt.Sprintf("Output reading failed: %v", err))
		} else {
			debug.Info("[Hashcat stdout reader] Finished reading %d lines without error", lineCount)
		}
	}()
	
	// Read stderr for errors and warnings
	go func() {
		defer func() {
			debug.Info("[Hashcat stderr reader] Goroutine exiting for task %s", process.TaskID)
			// Send completion signal safely
			select {
			case outputDone <- true:
			default:
			}
		}()
		scanner := bufio.NewScanner(stderrPipe)
		// Increase buffer size to 1MB
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		
		debug.Info("[Hashcat stderr reader] Starting for task %s", process.TaskID)
		lineCount := 0
		
		alreadyRunningDetected := false
		var alreadyRunningPID string
		for scanner.Scan() {
			line := scanner.Text()
			lineCount++
			debug.Debug("[Hashcat stderr] %s", line)
			
			// Check for "Already an instance" error
			// Example: "Already an instance C:\Users\Aaron Sullivan\Desktop\KrakenHashes\data\binaries\2\hashcat.exe running on pid 50444"
			if strings.Contains(line, "Already an instance") && strings.Contains(line, "running on pid") {
				alreadyRunningDetected = true
				
				// Try to extract the PID
				pidMatch := regexp.MustCompile(`running on pid (\d+)`).FindStringSubmatch(line)
				if len(pidMatch) > 1 {
					alreadyRunningPID = pidMatch[1]
					debug.Error("Detected 'Already an instance' error for task %s - existing PID: %s", process.TaskID, alreadyRunningPID)
				} else {
					debug.Error("Detected 'Already an instance' error for task %s", process.TaskID)
				}
			}
			
			// Send error output via websocket if callback is set
			if e.outputCallback != nil {
				e.outputCallback(process.TaskID, line, true)
			}
		}
		
		// If we detected the "already running" error, store it
		if alreadyRunningDetected {
			process.mutex.Lock()
			process.AlreadyRunningError = true
			process.mutex.Unlock()
			
			// Log additional details for debugging
			if alreadyRunningPID != "" {
				debug.Info("Hashcat process %s blocked by existing instance with PID %s", process.TaskID, alreadyRunningPID)
			}
		}
		
		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			debug.Error("[Hashcat stderr reader] Scanner error after %d lines: %v", lineCount, err)
		} else {
			debug.Info("[Hashcat stderr reader] Finished reading %d lines without error", lineCount)
		}
	}()


	// Mark as running
	process.IsRunning = true

	// Start the command
	debug.Info("Starting hashcat process for task %s", process.TaskID)
	debug.Info("Command: %s", process.Cmd.Path)
	debug.Info("Args: %v", process.Cmd.Args)
	
	err := process.Cmd.Start()
	if err != nil {
		debug.Error("Failed to start hashcat process: %v", err)
		e.sendErrorProgress(process, fmt.Sprintf("Failed to start hashcat: %v", err))
		return
	}
	
	debug.Info("Hashcat process started successfully with PID: %d", process.Cmd.Process.Pid)

	// Start outfile monitoring goroutine
	if process.OutfilePath != "" {
		outfileCtx, outfileCancel := context.WithCancel(ctx)
		defer outfileCancel()
		go e.monitorOutfile(outfileCtx, process)
		debug.Info("Started outfile monitor for task %s", process.TaskID)
	}

	// Write PID to file for tracking
	if err := e.writePIDFile(process.Cmd.Process.Pid); err != nil {
		debug.Warning("Failed to write PID file: %v", err)
	}

	// Wait for completion or cancellation
	done := make(chan error, 1)
	go func() {
		debug.Info("Starting process wait for task %s", process.TaskID)
		waitErr := process.Cmd.Wait()
		debug.Info("Process wait completed for task %s, error: %v", process.TaskID, waitErr)
		done <- waitErr
	}()

	debug.Info("Entering main select loop for task %s", process.TaskID)
	select {
	case <-ctx.Done():
		// Context cancelled, kill the process
		debug.Info("Context cancelled for task %s, killing process", process.TaskID)
		if process.Cmd.Process != nil {
			process.Cmd.Process.Kill()
		}
		e.sendProgressUpdate(process, &JobProgress{
			TaskID: process.TaskID,
		}, "cancelled")

	case err := <-done:
		// Process completed
		debug.Info("Process completed for task %s, error: %v", process.TaskID, err)
		process.IsRunning = false
		
		// Wait for output goroutines to complete with increased timeout
		debug.Info("Waiting for output goroutines to complete for task %s", process.TaskID)
		for i := 0; i < 2; i++ {
			select {
			case <-outputDone:
				debug.Info("Output goroutine %d/2 completed for task %s", i+1, process.TaskID)
			case <-time.After(30 * time.Second):
				debug.Warning("Timeout waiting for output goroutine %d/2 to complete for task %s (waited 30s)", i+1, process.TaskID)
			}
		}
		debug.Info("All output goroutines finished for task %s", process.TaskID)
		
		if err != nil {
			// Check if it's just a non-zero exit code (hashcat uses different exit codes)
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode := exitErr.ExitCode()
				debug.Info("Hashcat exited with code: %d for task %s", exitCode, process.TaskID)
				
				// Hashcat exit codes:
				// 0 = OK/cracked
				// 1 = exhausted (normal completion, no more work)
				// 2 = aborted
				// 3 = aborted by checkpoint
				// 4 = aborted by runtime
				// 5 = aborted by finish
				// -1 = error
				// -2 = gpu-watchdog alarm
				// ... other negative codes are backend errors
				
				switch exitCode {
				case 0:
					// OK/cracked - normal completion
					debug.Info("Hashcat completed with OK/cracked status for task %s", process.TaskID)
					// Use the last progress percentage if available, otherwise 100%
					progressPercent := 100.0
					var effectiveProgress int64
					var totalEffectiveKeyspace *int64
					if process.LastProgress != nil {
						if process.LastProgress.ProgressPercent > 0 {
							progressPercent = process.LastProgress.ProgressPercent
						}
						effectiveProgress = process.LastProgress.EffectiveProgress
						// Include the total effective keyspace from last hashcat status
						// This ensures the backend can adjust dispatched_keyspace even if this is the first/only message
						if process.LastProgress.TotalEffectiveKeyspace != nil {
							totalEffectiveKeyspace = process.LastProgress.TotalEffectiveKeyspace
						}
					}
					finalProgress := &JobProgress{
						TaskID:                 process.TaskID,
						KeyspaceProcessed:      process.Assignment.KeyspaceEnd - process.Assignment.KeyspaceStart,
						EffectiveProgress:      effectiveProgress,
						ProgressPercent:        progressPercent,
						TotalEffectiveKeyspace: totalEffectiveKeyspace, // Always include for backend adjustment
					}
					e.sendProgressUpdate(process, finalProgress, "completed")
					
				case 1:
					// Exhausted - normal completion, keyspace fully processed
					debug.Info("Hashcat exhausted keyspace for task %s", process.TaskID)
					// Exhausted means 100% complete
					var effectiveProgress int64
					var totalEffectiveKeyspace *int64
					if process.LastProgress != nil {
						effectiveProgress = process.LastProgress.EffectiveProgress
						// Include the total effective keyspace from last hashcat status
						// This ensures the backend can adjust dispatched_keyspace even if this is the first/only message
						if process.LastProgress.TotalEffectiveKeyspace != nil {
							totalEffectiveKeyspace = process.LastProgress.TotalEffectiveKeyspace
						}
					}
					finalProgress := &JobProgress{
						TaskID:                 process.TaskID,
						KeyspaceProcessed:      process.Assignment.KeyspaceEnd - process.Assignment.KeyspaceStart,
						EffectiveProgress:      effectiveProgress,
						ProgressPercent:        100.0, // Keyspace exhausted = 100% complete
						TotalEffectiveKeyspace: totalEffectiveKeyspace, // Always include for backend adjustment
					}
					e.sendProgressUpdate(process, finalProgress, "completed")

				case 2, 3, 4, 5:
					// Various abort conditions
					debug.Warning("Hashcat was aborted (exit code %d) for task %s", exitCode, process.TaskID)
					e.sendErrorProgress(process, fmt.Sprintf("Hashcat aborted with exit code %d", exitCode))
					
				case -2:
					// GPU watchdog alarm
					debug.Error("GPU watchdog alarm triggered for task %s", process.TaskID)
					e.sendErrorProgress(process, "GPU watchdog alarm - possible GPU hang or temperature issue")
					
				case 255, -1:
					// Exit code 255 or -1 (4294967295 as unsigned) often means another instance is running
					process.mutex.Lock()
					alreadyRunning := process.AlreadyRunningError
					process.mutex.Unlock()
					
					if alreadyRunning {
						debug.Error("Hashcat exit code %d for task %s - confirmed another instance is running", exitCode, process.TaskID)
						e.sendErrorProgress(process, "Hashcat failed to start - another instance is already running")
					} else {
						debug.Error("Hashcat exit code %d for task %s - unknown error", exitCode, process.TaskID)
						e.sendErrorProgress(process, fmt.Sprintf("Hashcat failed with exit code %d", exitCode))
					}
					
				default:
					// Other errors
					if exitCode < 0 {
						debug.Error("Hashcat backend error (exit code %d) for task %s", exitCode, process.TaskID)
						e.sendErrorProgress(process, fmt.Sprintf("Hashcat backend error with exit code %d", exitCode))
					} else {
						debug.Warning("Hashcat unexpected exit code %d for task %s", exitCode, process.TaskID)
						e.sendErrorProgress(process, fmt.Sprintf("Hashcat exited with unexpected code %d", exitCode))
					}
				}
			} else {
				e.sendErrorProgress(process, fmt.Sprintf("Hashcat process failed: %v", err))
			}
		} else {
			// Process completed successfully with exit code 0
			debug.Info("Hashcat completed successfully with exit code 0 (OK/cracked) for task %s", process.TaskID)
			// Use the last progress percentage if available, otherwise 100%
			progressPercent := 100.0
			var effectiveProgress int64
			if process.LastProgress != nil {
				if process.LastProgress.ProgressPercent > 0 {
					progressPercent = process.LastProgress.ProgressPercent
				}
				effectiveProgress = process.LastProgress.EffectiveProgress
			}
			finalProgress := &JobProgress{
				TaskID:            process.TaskID,
				KeyspaceProcessed: process.Assignment.KeyspaceEnd - process.Assignment.KeyspaceStart,
				EffectiveProgress: effectiveProgress,
				ProgressPercent:   progressPercent,
			}
			e.sendProgressUpdate(process, finalProgress, "completed")
		}
	}

	// Clean up batch state when task ends
	e.cleanupBatchState(process)
}

// sendProgressUpdate sends a progress update through the channel
func (e *HashcatExecutor) sendProgressUpdate(process *HashcatProcess, progress *JobProgress, status string) {
	// Set the status in the progress update
	progress.Status = status
	
	select {
	case process.ProgressChannel <- progress:
		// Progress sent successfully
	default:
		// Channel full, log warning but don't block
		debug.Warning("Progress channel full for task %s, dropping update", process.TaskID)
	}
}

// sendErrorProgress sends an error progress update
func (e *HashcatExecutor) sendErrorProgress(process *HashcatProcess, errorMsg string) {
	progress := &JobProgress{
		TaskID:       process.TaskID,
		Status:       "failed",
		ErrorMessage: errorMsg,
	}

	e.sendProgressUpdate(process, progress, "failed")
}

// addCrackToBatch adds a cracked hash to the batch buffer for the given task.
// Batches are flushed after 500ms timer OR when buffer reaches 10k cracks (WebSocket limit).
// This balances efficiency with WebSocket message size constraints.
func (e *HashcatExecutor) addCrackToBatch(process *HashcatProcess, cracked *CrackedHash) {
	e.crackBatchMutex.Lock()
	defer e.crackBatchMutex.Unlock()

	taskID := process.TaskID

	// Initialize buffer if needed (10k capacity to stay under WebSocket message size)
	if e.crackBatchBuffers[taskID] == nil {
		e.crackBatchBuffers[taskID] = make([]CrackedHash, 0, 10000)
	}

	// Add crack to buffer
	e.crackBatchBuffers[taskID] = append(e.crackBatchBuffers[taskID], *cracked)

	// Flush immediately if buffer reaches 10k cracks (WebSocket message size limit)
	// Otherwise let the timer handle batching naturally
	if len(e.crackBatchBuffers[taskID]) >= 10000 {
		debug.Info("Crack batch buffer reached size limit for task %s (%d cracks), flushing immediately",
			taskID, len(e.crackBatchBuffers[taskID]))
		e.flushCrackBatchLocked(process)
		return
	}

	// Start or reset the batch timer (will send everything after 500ms)
	e.startBatchTimerLocked(process)
}

// flushCrackBatch flushes the crack batch buffer for the given task with mutex protection.
func (e *HashcatExecutor) flushCrackBatch(process *HashcatProcess) {
	e.crackBatchMutex.Lock()
	defer e.crackBatchMutex.Unlock()
	e.flushCrackBatchLocked(process)
}

// flushCrackBatchLocked flushes the crack batch buffer without acquiring the mutex (caller must hold it).
func (e *HashcatExecutor) flushCrackBatchLocked(process *HashcatProcess) {
	taskID := process.TaskID

	// Get the buffer
	buffer := e.crackBatchBuffers[taskID]
	if len(buffer) == 0 {
		return // Nothing to flush
	}

	debug.Info("Flushing crack batch for task %s with %d cracks", taskID, len(buffer))

	// Send the batched progress update
	progress := &JobProgress{
		TaskID:        taskID,
		CrackedCount:  len(buffer),
		CrackedHashes: buffer,
	}
	e.sendProgressUpdate(process, progress, "cracked")

	// Clear the buffer (reset to 10k capacity for next batch)
	e.crackBatchBuffers[taskID] = make([]CrackedHash, 0, 10000)

	// Stop and clear the timer
	if timer := e.crackBatchTimers[taskID]; timer != nil {
		timer.Stop()
		delete(e.crackBatchTimers, taskID)
	}
}

// startBatchTimerLocked starts or resets the batch timer for the given task (caller must hold mutex).
func (e *HashcatExecutor) startBatchTimerLocked(process *HashcatProcess) {
	taskID := process.TaskID

	// Stop existing timer if present
	if timer := e.crackBatchTimers[taskID]; timer != nil {
		timer.Stop()
	}

	// Create new timer
	e.crackBatchTimers[taskID] = time.AfterFunc(e.crackBatchInterval, func() {
		// When timer fires, flush the batch
		e.crackBatchMutex.Lock()
		defer e.crackBatchMutex.Unlock()

		// Verify buffer still exists (task might have completed)
		if e.crackBatchBuffers[taskID] != nil && len(e.crackBatchBuffers[taskID]) > 0 {
			debug.Debug("Batch timer expired for task %s, flushing %d cracks",
				taskID, len(e.crackBatchBuffers[taskID]))
			e.flushCrackBatchLocked(process)
		}
	})
}

// cleanupBatchState cleans up the batching state for a task (called when task completes/fails).
func (e *HashcatExecutor) cleanupBatchState(process *HashcatProcess) {
	e.crackBatchMutex.Lock()
	defer e.crackBatchMutex.Unlock()

	taskID := process.TaskID

	// Flush any remaining cracks
	e.flushCrackBatchLocked(process)

	// Clean up maps
	delete(e.crackBatchBuffers, taskID)
	if timer := e.crackBatchTimers[taskID]; timer != nil {
		timer.Stop()
		delete(e.crackBatchTimers, taskID)
	}
}

// StopTask stops a running task
func (e *HashcatExecutor) StopTask(taskID string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	process, exists := e.activeProcesses[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Cancel the context to stop the process
	process.Cancel()
	return nil
}

// GetTaskProgress returns the current progress of a task
func (e *HashcatExecutor) GetTaskProgress(taskID string) (*JobProgress, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	process, exists := e.activeProcesses[taskID]
	if !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	return process.LastProgress, nil
}

// GetActiveTaskIDs returns a list of currently active task IDs
func (e *HashcatExecutor) GetActiveTaskIDs() []string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	var taskIDs []string
	for taskID := range e.activeProcesses {
		taskIDs = append(taskIDs, taskID)
	}

	return taskIDs
}

// ForceCleanup forces cleanup of all hashcat processes
func (e *HashcatExecutor) ForceCleanup() error {
	debug.Info("Forcing cleanup of all hashcat processes")
	
	// First, stop all tracked processes
	e.mutex.Lock()
	for taskID, process := range e.activeProcesses {
		debug.Info("Cancelling task %s", taskID)
		process.Cancel()
	}
	// Clear the map
	e.activeProcesses = make(map[string]*HashcatProcess)
	e.mutex.Unlock()
	
	// Then kill any remaining hashcat processes
	if err := e.checkAndKillExistingHashcat(); err != nil {
		debug.Warning("Error during force cleanup: %v", err)
		return err
	}
	
	// Clean up PID file
	os.Remove(hashcatPIDFile)
	
	debug.Info("Force cleanup completed")
	return nil
}

// RunSpeedTest runs a real-world speed test with actual job configuration
// Returns: totalSpeed (H/s), deviceSpeeds, totalEffectiveKeyspace (progress[1]), error
func (e *HashcatExecutor) RunSpeedTest(ctx context.Context, assignment *JobTaskAssignment, testDuration int) (int64, []DeviceSpeed, int64, error) {
	debug.Info("Running speed test for hash type %d, attack mode %d, duration %d seconds", 
		assignment.HashType, assignment.AttackMode, testDuration)
	
	// Build command similar to real job but without skip/limit and without --remove
	cmd, _, _, _, err := e.buildHashcatCommandWithOptions(assignment, true)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("failed to build command: %w", err)
	}
	
	// Get the original args
	originalArgs := cmd.Args[1:] // Skip the command itself
	
	// Remove --skip and --limit arguments for speed test
	filteredArgs := []string{}
	skipNext := false
	for _, arg := range originalArgs {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--skip" || arg == "--limit" {
			skipNext = true
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
	
	debug.Info("Starting speed test with command: %s %s", cmd.Path, strings.Join(filteredArgs, " "))
	
	// Create new command with filtered args
	cmd = exec.CommandContext(ctx, cmd.Path, filteredArgs...)
	
	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(cmd.Path)
	debug.Info("Setting speed test working directory to: %s", cmd.Dir)
	
	// Set up pipes for stdout/stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, nil, 0, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return 0, nil, 0, fmt.Errorf("failed to start hashcat: %w", err)
	}
	
	// Channel to collect status updates
	statusChan := make(chan string, 10)
	stopReading := make(chan bool)
	
	// Read stdout in goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-stopReading:
				debug.Debug("[Speed test] Stopping stdout reader")
				return
			default:
				line := scanner.Text()
				if strings.TrimSpace(line) != "" {
					debug.Debug("[Speed test stdout raw] %s", line)
					// Sometimes hashcat outputs crack result and JSON on same line
					// First check if line contains both crack and JSON
					if strings.Contains(line, ":") && strings.Contains(line, "{") && strings.Contains(line, "\"status\"") {
						// Split at the JSON start
						jsonStart := strings.Index(line, "{")
						jsonPart := line[jsonStart:]
						debug.Debug("[Speed test] Found mixed output, extracted JSON: %s", jsonPart)
						// Just use the JSON part for speed test
						select {
						case statusChan <- jsonPart:
						case <-stopReading:
							return
						}
					} else if strings.HasPrefix(line, "{") && strings.Contains(line, "\"status\"") {
						// Pure JSON status line
						debug.Debug("[Speed test] Found pure JSON status")
						select {
						case statusChan <- line:
						case <-stopReading:
							return
						}
					}
				}
			}
		}
	}()
	
	// Read stderr in goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			debug.Debug("[Hashcat stderr] %s", line)
		}
	}()
	
	// Timer to stop after timeout - use context deadline if available, otherwise 3 minutes
	var timer *time.Timer
	if deadline, ok := ctx.Deadline(); ok {
		// Use remaining time from context
		timer = time.NewTimer(time.Until(deadline))
	} else {
		// Fallback to 3 minutes if no deadline set
		timer = time.NewTimer(3 * time.Minute)
	}
	
	// Collect status updates
	var statusUpdates []string
	var lastValidSpeed int64
	var lastDeviceSpeeds []DeviceSpeed
	var lastTotalEffectiveKeyspace int64
	statusCollected := make(chan bool)

	go func() {
		for {
			select {
			case status := <-statusChan:
				debug.Debug("[Speed test] Received status update %d", len(statusUpdates)+1)
				// Try to parse this status update immediately
				speed, devSpeeds, totalEffective, err := e.parseSpeedFromJSON(status)
				if err == nil && speed > 0 {
					lastValidSpeed = speed
					lastDeviceSpeeds = devSpeeds
					lastTotalEffectiveKeyspace = totalEffective
					debug.Info("[Speed test] Parsed valid speed: %d H/s, effective keyspace: %d from update %d", speed, totalEffective, len(statusUpdates)+1)
				} else if err != nil {
					debug.Warning("[Speed test] Failed to parse update %d: %v", len(statusUpdates)+1, err)
				}
				
				statusUpdates = append(statusUpdates, status)
				
				// Check if hashcat has completed
				// Status codes: 1=init, 2=running, 3=paused, 4=bypassed, 5=exhausted, 6=cracked, 7=aborted, 8=quit, 9=error
				// Parse the status field to see if the job is done
				var statusCheck struct {
					Status int `json:"status"`
				}
				if err := json.Unmarshal([]byte(status), &statusCheck); err == nil {
					// Status 5 (exhausted) or 6 (all cracked) means the job is complete
					if statusCheck.Status == 5 || statusCheck.Status == 6 {
						debug.Info("[Speed test] Hashcat completed (status %d) after %d updates, stopping collection", statusCheck.Status, len(statusUpdates))
						timer.Stop()
						close(statusCollected)
						return
					}
				}
				
				// We want to get at least 3 updates for stability
				if len(statusUpdates) >= 3 {
					debug.Info("[Speed test] Collected 3 updates, stopping collection")
					timer.Stop()
					close(statusCollected)
					return
				}
			case <-timer.C:
				debug.Info("[Speed test] Timer expired after %d updates", len(statusUpdates))
				close(statusCollected)
				return
			}
		}
	}()
	
	// Wait for status collection to complete
	<-statusCollected
	
	// Stop reading stdout/stderr
	close(stopReading)
	
	// Kill the process and wait for it to exit
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
	cmd.Wait() // Clean up the process
	
	// Check if we got any valid speed readings
	if lastValidSpeed == 0 {
		debug.Warning("[Speed test] No valid speed parsed during collection, checking stored updates")
		if len(statusUpdates) == 0 {
			return 0, nil, 0, fmt.Errorf("no status updates received during speed test")
		}

		// Try to parse from the best available update
		// Use 3rd update if available, otherwise use the last update
		statusIndex := len(statusUpdates) - 1
		if len(statusUpdates) >= 3 {
			statusIndex = 2 // Third update (0-indexed) for stability
		}

		debug.Debug("[Speed test] Attempting to parse update %d of %d: %s", statusIndex+1, len(statusUpdates), statusUpdates[statusIndex])
		totalSpeed, deviceSpeeds, totalEffective, err := e.parseSpeedFromJSON(statusUpdates[statusIndex])
		if err != nil {
			// Log the actual content that failed to parse
			debug.Error("[Speed test] Failed to parse JSON from update %d. Content: %s", statusIndex+1, statusUpdates[statusIndex])
			return 0, nil, 0, fmt.Errorf("failed to parse speed from status: %w", err)
		}

		if totalSpeed == 0 {
			// For exhausted jobs with only 1 update, this might be expected
			debug.Warning("[Speed test] Speed is 0 H/s after %d updates - job may have exhausted immediately", len(statusUpdates))
			// Still try to return if we have device speeds
			if len(deviceSpeeds) > 0 {
				debug.Info("[Speed test] Using device speeds even though total is 0")
			}
		}

		lastValidSpeed = totalSpeed
		lastDeviceSpeeds = deviceSpeeds
		lastTotalEffectiveKeyspace = totalEffective
	}

	debug.Info("Speed test completed: %d H/s total, effective keyspace: %d from %d updates", lastValidSpeed, lastTotalEffectiveKeyspace, len(statusUpdates))
	return lastValidSpeed, lastDeviceSpeeds, lastTotalEffectiveKeyspace, nil
}

// parseSpeedFromJSON parses device speeds and effective keyspace from hashcat JSON status output
func (e *HashcatExecutor) parseSpeedFromJSON(jsonStr string) (int64, []DeviceSpeed, int64, error) {
	// Fix hashcat's invalid JSON - it outputs device_id with leading zeros like 01, 02
	// which is invalid JSON. We need to fix these to be valid numbers.
	fixedJSON := jsonStr

	// Use regex to fix device_id values with leading zeros
	// This will convert "device_id": 01 to "device_id": 1
	re := regexp.MustCompile(`"device_id":\s*0+(\d+)`)
	fixedJSON = re.ReplaceAllString(fixedJSON, `"device_id": $1`)

	var status struct {
		Devices []struct {
			DeviceID   int    `json:"device_id"`
			DeviceName string `json:"device_name"`
			Speed      int64  `json:"speed"`
		} `json:"devices"`
		Progress [2]int64 `json:"progress"` // [current, total] - total is progress[1]
	}

	if err := json.Unmarshal([]byte(fixedJSON), &status); err != nil {
		return 0, nil, 0, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var totalSpeed int64
	var deviceSpeeds []DeviceSpeed

	for _, device := range status.Devices {
		totalSpeed += device.Speed
		deviceSpeeds = append(deviceSpeeds, DeviceSpeed{
			DeviceID:   device.DeviceID,
			DeviceName: device.DeviceName,
			Speed:      device.Speed,
		})
	}

	// Extract total effective keyspace from progress[1]
	totalEffectiveKeyspace := status.Progress[1]

	return totalSpeed, deviceSpeeds, totalEffectiveKeyspace, nil
}


// resolveHashcatBinary resolves the hashcat binary path from the assignment
func (e *HashcatExecutor) resolveHashcatBinary(binaryPath string) (string, error) {
	debug.Info("Resolving hashcat binary from path: %s", binaryPath)
	
	// The binaryPath might come in different formats:
	// - "binaries/hashcat_2" (old format)
	// - "binaries/8" (new format, just the ID)
	// We need to resolve this to the actual executable
	
	var binaryDir string
	
	// Check if it's the old format
	if strings.HasPrefix(binaryPath, "binaries/hashcat_") {
		binaryID := strings.TrimPrefix(binaryPath, "binaries/hashcat_")
		binaryDir = filepath.Join(e.dataDirectory, "binaries", binaryID)
	} else if strings.HasPrefix(binaryPath, "binaries/") {
		// New format - just the binary ID
		binaryID := strings.TrimPrefix(binaryPath, "binaries/")
		binaryDir = filepath.Join(e.dataDirectory, "binaries", binaryID)
	} else {
		// Direct path or other format
		// Check if it's already a full path
		if _, err := os.Stat(binaryPath); err == nil {
			return binaryPath, nil
		}
		// Try in data directory
		fullPath := filepath.Join(e.dataDirectory, binaryPath)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath, nil
		}
		return "", fmt.Errorf("invalid binary path format: %s", binaryPath)
	}
	
	if binaryDir != "" {
		
		// Look for hashcat executable in the binary directory
		// The binary should have been extracted from the .7z archive
		var possiblePaths []string
		
		// Prioritize OS-specific binaries
		switch runtime.GOOS {
		case "windows":
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat.exe"),  // Windows primary
				filepath.Join(binaryDir, "hashcat"),      // Windows fallback
			}
		case "linux":
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat.bin"),  // Linux primary
				filepath.Join(binaryDir, "hashcat"),      // Linux fallback
			}
		case "darwin":
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat"),      // macOS primary
				filepath.Join(binaryDir, "hashcat.bin"),  // macOS fallback
			}
		default:
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat"),      // Default Unix-like
				filepath.Join(binaryDir, "hashcat.bin"),  // Alternative
			}
		}
		
		for _, path := range possiblePaths {
			if fileInfo, err := os.Stat(path); err == nil {
				// Check if it's the right type of executable for this OS
				isExecutable := false
				
				if runtime.GOOS == "windows" {
					// On Windows, .exe files are executable
					isExecutable = strings.HasSuffix(path, ".exe") || fileInfo.Mode()&0111 != 0
				} else {
					// On Unix-like systems, check execute permission and skip .exe files
					isExecutable = !strings.HasSuffix(path, ".exe") && fileInfo.Mode()&0111 != 0
				}
				
				if isExecutable {
					debug.Info("Found hashcat binary for %s at: %s", runtime.GOOS, path)
					return path, nil
				}
			}
		}
		
		// Check if the .7z archive exists but hasn't been extracted
		archivePath := filepath.Join(binaryDir, "hashcat-6.2.6+1017.7z")
		if _, err := os.Stat(archivePath); err == nil {
			return "", fmt.Errorf("hashcat archive found at %s but not extracted. Please ensure file sync extracts binaries", archivePath)
		}
		
		return "", fmt.Errorf("hashcat binary not found in directory %s. Checked paths: %v", binaryDir, possiblePaths)
	}
	
	// If it's a direct path, check if it exists
	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}
	
	// Try in data directory
	fullPath := filepath.Join(e.dataDirectory, binaryPath)
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath, nil
	}
	
	return "", fmt.Errorf("hashcat binary not found: %s", binaryPath)
}

// loadHashlist loads the hashlist file and returns the hash values
func (e *HashcatExecutor) loadHashlist(hashlistPath string) ([]string, error) {
	file, err := os.Open(hashlistPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open hashlist file: %w", err)
	}
	defer file.Close()

	var hashes []string
	scanner := bufio.NewScanner(file)
	// Increase buffer size for large hashes like NTLMv2
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			// Store the full hash line
			hashes = append(hashes, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading hashlist file: %w", err)
	}

	debug.Info("Loaded %d hashes from hashlist file", len(hashes))
	return hashes, nil
}

// parseCrackedHash parses a cracked hash output line using hashlist knowledge
func (e *HashcatExecutor) parseCrackedHash(line string, hashlistContent []string) *CrackedHash {
	// First try to match against known hashes from the hashlist
	// Use case-insensitive matching since hashcat may output different case than stored
	lineLower := strings.ToLower(line)

	for _, knownHash := range hashlistContent {
		// Check if the line starts with this known hash (case-insensitive)
		knownHashLower := strings.ToLower(knownHash)
		if strings.HasPrefix(lineLower, knownHashLower) {
			// The format should be: knownHash:password
			// Everything after the known hash and colon is the password
			expectedPrefixLower := knownHashLower + ":"
			if strings.HasPrefix(lineLower, expectedPrefixLower) {
				// Extract password from the original line (preserving case)
				// Find where the password starts in the original line
				passwordStart := len(knownHash) + 1  // +1 for the colon
				if passwordStart <= len(line) {
					password := line[passwordStart:]

					// Create the cracked hash structure
					cracked := &CrackedHash{
						Hash:     knownHash,  // Use original hash from hashlist (lowercase as stored in DB)
						Plain:    password,   // Password with original case
						FullLine: line,       // Keep the full line for reference
					}

					debug.Debug("[Crack Parser] Matched hash: %s, Password: %s", knownHash, password)
					return cracked
				}
			}
		}
	}

	// Fallback: If no exact match found (shouldn't happen with proper hashlist),
	// use the old simple parsing as a safety net
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)  // Split only on first colon
		if len(parts) == 2 {
			// For unknown formats, send the whole first part as hash
			cracked := &CrackedHash{
				Hash:     parts[0],
				Plain:    parts[1],
				FullLine: line,
			}

			// Only return if it looks like a valid hash
			if len(cracked.Hash) >= 16 && !strings.Contains(cracked.Hash, " ") {
				debug.Warning("[Crack Parser] Using fallback parsing for: %s", line)
				return cracked
			}
		}
	}

	return nil
}

// monitorOutfile monitors the hashcat outfile for new cracks
func (e *HashcatExecutor) monitorOutfile(ctx context.Context, process *HashcatProcess) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final read before exit
			e.readNewOutfileLines(process)
			return

		case <-ticker.C:
			e.readNewOutfileLines(process)
		}
	}
}

// readNewOutfileLines reads new lines from the outfile and sends them as crack batches
func (e *HashcatExecutor) readNewOutfileLines(process *HashcatProcess) {
	// Open file
	file, err := os.Open(process.OutfilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			debug.Error("Failed to open outfile %s: %v", process.OutfilePath, err)
		}
		return
	}
	defer file.Close()

	// Seek to last read position
	process.OutfileMutex.Lock()
	offset := process.OutfileOffset
	process.OutfileMutex.Unlock()

	file.Seek(offset, 0)
	reader := bufio.NewReader(file)

	var newCracks []CrackedHash
	var newOffset int64 = offset

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break // No more complete lines
		}
		if err != nil {
			debug.Error("Error reading outfile: %v", err)
			return
		}

		// Parse format 1,2: hash:password (colon-separated)
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}

		hash := parts[0]
		password := parts[1]
		lineKey := line // Use full line as key (includes newline for uniqueness)

		// Check if already sent
		process.OutfileMutex.Lock()
		if process.OutfileSentHashes == nil {
			process.OutfileSentHashes = make(map[string]bool)
		}

		if !process.OutfileSentHashes[lineKey] {
			process.OutfileSentHashes[lineKey] = true
			newCracks = append(newCracks, CrackedHash{
				Hash:  hash,
				Plain: password,
			})
		}
		process.OutfileMutex.Unlock()

		// Update offset after each line
		newOffset, _ = file.Seek(0, io.SeekCurrent)
	}

	// Update stored offset
	process.OutfileMutex.Lock()
	process.OutfileOffset = newOffset
	process.OutfileMutex.Unlock()

	// Send batch if any new cracks
	if len(newCracks) > 0 {
		debug.Info("Outfile monitor found %d new cracks for task %s", len(newCracks), process.TaskID)
		// Add each crack to the batch (uses existing batching logic)
		for _, crack := range newCracks {
			e.addCrackToBatch(process, &crack)
		}
	}
}