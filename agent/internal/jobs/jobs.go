package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	filesync "github.com/ZerkerEOD/krakenhashes/agent/internal/sync"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// JobManager manages job execution on the agent
type JobManager struct {
	executor                     ExecutorInterface
	config                       *config.Config
	statusCallback               func(*JobStatus)             // Callback for status updates (synchronous)
	crackCallback                func(*CrackBatch)            // Callback for crack batches (asynchronous)
	crackBatchesCompleteCallback func(*CrackBatchesComplete)  // Callback to signal all crack batches sent
	progressCallback             func(*JobProgress)           // Legacy callback (deprecated, use statusCallback/crackCallback)
	outputCallback               func(taskID string, output string, isError bool) // Callback for sending output via websocket
	fileSync                     *filesync.FileSync
	hwMonitor                    HardwareMonitor // Interface for hardware monitor

	// ACK waiting callback for completion acknowledgment (GH Issue #12)
	// Parameters: taskID, resend function
	// Returns: true if ACK received, false if timeout/retries exhausted
	ackWaitCallback func(taskID string, resendFunc func() error) bool

	// Job state
	mutex             sync.RWMutex
	activeJobs        map[string]*JobExecution
	benchmarkCache    map[string]*BenchmarkResult
	lastCompletedTask *CompletedTaskInfo // Cache last completed task for reconnection

	// Explicit state machine for reliable state tracking
	// This prevents race conditions from deferred cleanup
	stateManager *TaskStateManager
}

// HardwareMonitor interface for device management
type HardwareMonitor interface {
	GetEnabledDeviceFlags() string
	HasEnabledDevices() bool
}

// JobExecution represents an active job execution
type JobExecution struct {
	Assignment      *JobTaskAssignment
	Process         *HashcatProcess
	StartTime       time.Time
	LastProgress    *JobProgress
	Status          string
}

// BenchmarkResult stores benchmark results
type BenchmarkResult struct {
	HashType    int
	AttackMode  int
	Speed       int64
	Timestamp   time.Time
}

// CompletedTaskInfo stores all progress data needed for reconnection
// Used to report completion status if agent reconnects after task finished
type CompletedTaskInfo struct {
	TaskID                 string
	JobID                  string
	// Progress fields - copy from LastProgress
	KeyspaceProcessed      int64
	EffectiveProgress      int64
	ProgressPercent        float64
	TotalEffectiveKeyspace *int64
	HashRate               int64
	CrackedCount           int
	AllHashesCracked       bool
	// Status fields
	Status                 string // "completed", "failed", or "running"
	ErrorMessage           string
	CompletedAt            time.Time
}

// ExecutorInterface defines the methods needed by JobManager
type ExecutorInterface interface {
	SetOutputCallback(callback func(taskID string, output string, isError bool))
	SetDeviceFlagsCallback(callback func() string)
	SetAgentExtraParams(params string)
	ExecuteTask(ctx context.Context, assignment *JobTaskAssignment) (*HashcatProcess, error)
	StopTask(taskID string) error
	GetTaskProgress(taskID string) (*JobProgress, error)
	GetActiveTaskIDs() []string
	ForceCleanup() error
	RunSpeedTest(ctx context.Context, assignment *JobTaskAssignment, testDuration int) (int64, []DeviceSpeed, int64, error)
	// Outfile acknowledgment protocol methods
	RetransmitOutfile(taskID string) ([]CrackedHash, error)
	DeleteOutfile(taskID string) error
	GetPendingOutfiles() (taskIDs []string, currentTaskID string, err error)
	GetOutfileLineCount(taskID string) (int64, error)
}

// NewJobManager creates a new job manager
func NewJobManager(cfg *config.Config, progressCallback func(*JobProgress), hwMonitor HardwareMonitor) *JobManager {
	return NewJobManagerWithExecutor(cfg, progressCallback, hwMonitor, false)
}

// NewJobManagerWithExecutor creates a new job manager with a specific executor type
func NewJobManagerWithExecutor(cfg *config.Config, progressCallback func(*JobProgress), hwMonitor HardwareMonitor, testMode bool) *JobManager {
	dataDir := cfg.DataDirectory

	var executor ExecutorInterface

	if testMode {
		// Create mock executor for testing
		mockExec := NewMockHashcatExecutor(dataDir)
		mockExec.SetAgentExtraParams(cfg.HashcatExtraParams)
		if hwMonitor != nil {
			mockExec.SetDeviceFlagsCallback(func() string {
				return hwMonitor.GetEnabledDeviceFlags()
			})
		}
		executor = mockExec
	} else {
		// Create real hashcat executor
		hashcatExec := NewHashcatExecutor(dataDir)
		hashcatExec.SetAgentExtraParams(cfg.HashcatExtraParams)
		if hwMonitor != nil {
			hashcatExec.SetDeviceFlagsCallback(func() string {
				return hwMonitor.GetEnabledDeviceFlags()
			})
		}
		executor = hashcatExec
	}

	return &JobManager{
		executor:         executor,
		config:           cfg,
		progressCallback: progressCallback,
		hwMonitor:        hwMonitor,
		activeJobs:       make(map[string]*JobExecution),
		benchmarkCache:   make(map[string]*BenchmarkResult),
		stateManager:     NewTaskStateManager(),
	}
}

// SetFileSync sets the file sync handler for downloading hashlists
func (jm *JobManager) SetFileSync(fileSync *filesync.FileSync) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.fileSync = fileSync
}

// SetOutputCallback sets the callback for sending output via websocket
func (jm *JobManager) SetOutputCallback(callback func(taskID string, output string, isError bool)) {
	jm.outputCallback = callback
	// Pass it through to the executor
	jm.executor.SetOutputCallback(callback)
}

// GetCurrentTaskStatus returns information about the currently running or completed task
// Returns nil if there is no active task and no cached completion
// Uses explicit state machine for reliable state tracking
func (jm *JobManager) GetCurrentTaskStatus() *CompletedTaskInfo {
	// Check state machine first - this is the source of truth
	state, taskID := jm.stateManager.GetState()

	// If state is idle, check for cached completion or pending completion
	if state == TaskStateIdle {
		// Check for pending completion that needs to be resolved
		if pending, pendingID := jm.stateManager.GetCompletionPending(); pending {
			debug.Info("GetCurrentTaskStatus: returning pending completion for task %s", pendingID)
			jm.mutex.RLock()
			defer jm.mutex.RUnlock()
			if jm.lastCompletedTask != nil && jm.lastCompletedTask.TaskID == pendingID {
				return jm.lastCompletedTask
			}
			// Return minimal pending completion info
			return &CompletedTaskInfo{
				TaskID: pendingID,
				Status: "completed",
			}
		}

		// Return cached completion if exists (no timeout - cache indefinitely until cleared)
		jm.mutex.RLock()
		defer jm.mutex.RUnlock()
		return jm.lastCompletedTask // nil if no cached task
	}

	// State is not idle - we have an active task
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	// Get execution from activeJobs using the task ID from state manager
	if execution, exists := jm.activeJobs[taskID]; exists {
		info := &CompletedTaskInfo{
			TaskID: taskID,
			Status: state.String(),
		}
		if execution.Assignment != nil {
			info.JobID = execution.Assignment.JobExecutionID
		}
		if execution.LastProgress != nil {
			info.KeyspaceProcessed = execution.LastProgress.KeyspaceProcessed
			info.EffectiveProgress = execution.LastProgress.EffectiveProgress
			info.ProgressPercent = execution.LastProgress.ProgressPercent
			info.TotalEffectiveKeyspace = execution.LastProgress.TotalEffectiveKeyspace
			info.HashRate = execution.LastProgress.HashRate
			info.CrackedCount = execution.LastProgress.CrackedCount
			info.AllHashesCracked = execution.LastProgress.AllHashesCracked
		}
		return info
	}

	// State says we have a task but it's not in activeJobs - inconsistent state
	// This shouldn't happen with proper state management, log warning
	debug.Warning("GetCurrentTaskStatus: state=%s taskID=%s but not in activeJobs", state, taskID)
	return nil
}

// ClearLastCompletedTask clears the last completed task info
// Called after backend acknowledges receipt of completion status
func (jm *JobManager) ClearLastCompletedTask() {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.lastCompletedTask = nil
	// Also clear any pending completion flag
	jm.stateManager.ClearCompletionPending()
}

// GetState returns the current task state and task ID
func (jm *JobManager) GetState() (TaskState, string) {
	return jm.stateManager.GetState()
}

// GetStateInfo returns full state information including timing
func (jm *JobManager) GetStateInfo() (TaskState, string, time.Time) {
	return jm.stateManager.GetStateInfo()
}

// SetCompletionPending marks a task completion as pending ACK resolution
func (jm *JobManager) SetCompletionPending(taskID string) {
	jm.stateManager.SetCompletionPending(taskID)
}

// GetCompletionPending returns the pending completion status
func (jm *JobManager) GetCompletionPending() (bool, string) {
	return jm.stateManager.GetCompletionPending()
}

// TransitionToIdle forces transition to idle state (for recovery scenarios)
func (jm *JobManager) TransitionToIdle() {
	jm.stateManager.TransitionTo(TaskStateIdle, "")
}

// SetProgressCallback sets the progress callback function (deprecated)
func (jm *JobManager) SetProgressCallback(callback func(*JobProgress)) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.progressCallback = callback
}

// SetStatusCallback sets the status callback function
func (jm *JobManager) SetStatusCallback(callback func(*JobStatus)) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.statusCallback = callback
}

// SetCrackCallback sets the crack callback function
func (jm *JobManager) SetCrackCallback(callback func(*CrackBatch)) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.crackCallback = callback
}

// SetCrackBatchesCompleteCallback sets the callback for crack batches complete signal
func (jm *JobManager) SetCrackBatchesCompleteCallback(callback func(*CrackBatchesComplete)) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.crackBatchesCompleteCallback = callback
}

// SetAckWaitCallback sets the callback for waiting for completion ACK (GH Issue #12)
func (jm *JobManager) SetAckWaitCallback(callback func(taskID string, resendFunc func() error) bool) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()
	jm.ackWaitCallback = callback
}

// ProcessJobAssignment processes a job assignment from the backend
func (jm *JobManager) ProcessJobAssignment(ctx context.Context, assignmentData []byte) error {
	// DEBUG: Log raw JSON received
	debug.Info("Received task assignment JSON: %s", string(assignmentData))

	var assignment JobTaskAssignment
	err := json.Unmarshal(assignmentData, &assignment)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job assignment: %w", err)
	}

	// DEBUG: Log increment values after unmarshaling
	debug.Info("Unmarshaled increment values - Mode: %s, Min: %v, Max: %v",
		assignment.IncrementMode, assignment.IncrementMin, assignment.IncrementMax)

	// Processing is already shown by "Task received" message
	debug.Info("Hashlist ID: %d, Hashlist Path: %s", assignment.HashlistID, assignment.HashlistPath)
	debug.Info("Wordlist paths: %v", assignment.WordlistPaths)
	debug.Info("Rule paths: %v", assignment.RulePaths)
	debug.Info("Attack mode: %d, Hash type: %d", assignment.AttackMode, assignment.HashType)

	// Check if any task is already running (agent runs one task at a time)
	// This is defense in depth - prevents concurrent task execution even if server has a bug
	jm.mutex.RLock()
	if len(jm.activeJobs) > 0 {
		// Get info about existing task for logging
		var existingTaskID string
		for taskID := range jm.activeJobs {
			existingTaskID = taskID
			break
		}
		jm.mutex.RUnlock()
		return fmt.Errorf("cannot accept task %s: already running task %s", assignment.TaskID, existingTaskID)
	}
	jm.mutex.RUnlock()

	// Ensure hashlist is available before proceeding
	err = jm.ensureHashlist(ctx, &assignment)
	if err != nil {
		return fmt.Errorf("failed to ensure hashlist: %w", err)
	}
	
	// Ensure rule chunks are available if this job uses rule chunks
	err = jm.ensureRuleChunks(ctx, &assignment)
	if err != nil {
		return fmt.Errorf("failed to ensure rule chunks: %w", err)
	}

	// Ensure association files are available if this is an association attack (mode 9)
	err = jm.ensureAssociationFiles(ctx, &assignment)
	if err != nil {
		return fmt.Errorf("failed to ensure association files: %w", err)
	}

	// Run benchmark if needed
	err = jm.ensureBenchmark(ctx, &assignment)
	if err != nil {
		console.Warning("Benchmark failed for task %s: %v", assignment.TaskID, err)
		// Continue without benchmark - use estimated values
	}

	// Start job execution
	process, err := jm.executor.ExecuteTask(ctx, &assignment)
	if err != nil {
		return fmt.Errorf("failed to start task execution: %w", err)
	}

	// Create job execution record
	jobExecution := &JobExecution{
		Assignment:   &assignment,
		Process:      process,
		StartTime:    time.Now(),
		Status:       "running",
	}

	jm.mutex.Lock()
	jm.activeJobs[assignment.TaskID] = jobExecution
	jm.mutex.Unlock()

	// Transition state machine to running
	jm.stateManager.TransitionTo(TaskStateRunning, assignment.TaskID)
	debug.Info("State transition: idle -> running (task: %s)", assignment.TaskID)

	// Start progress monitoring
	go jm.monitorJobProgress(ctx, jobExecution)

	// Job start is already shown by "Starting hashcat execution" message
	return nil
}

// ensureHashlist ensures the hashlist file is available locally
func (jm *JobManager) ensureHashlist(ctx context.Context, assignment *JobTaskAssignment) error {
	if jm.fileSync == nil {
		debug.Error("File sync is not initialized in job manager")
		return fmt.Errorf("file sync not initialized")
	}

	// Backend sends same path for all modes: hashlists/{id}.hash
	// The download function picks the correct endpoint based on attack mode
	localPath := filepath.Join(jm.config.DataDirectory, assignment.HashlistPath)
	hashlistFileName := filepath.Base(assignment.HashlistPath)

	debug.Info("Ensuring hashlist is available: %s (attack_mode: %d)", assignment.HashlistPath, assignment.AttackMode)
	debug.Info("Expected local path: %s", localPath)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create hashlist directory: %w", err)
	}

	// Always re-download hashlist for each task to ensure we have a fresh copy
	// This prevents issues with stale/modified hashlists from previous tasks
	if _, err := os.Stat(localPath); err == nil {
		debug.Info("Removing existing hashlist to download fresh copy: %s", localPath)
		if err := os.Remove(localPath); err != nil {
			debug.Warning("Failed to remove existing hashlist (will overwrite): %v", err)
			// Continue anyway - download will overwrite
		}
	}

	debug.Info("Downloading hashlist: %s", hashlistFileName)

	// Create FileInfo for download
	// AttackMode is passed through - download function picks right endpoint (mode 9 = original file)
	fileInfo := &filesync.FileInfo{
		Name:       hashlistFileName,
		FileType:   "hashlist",
		ID:         int(assignment.HashlistID),
		MD5Hash:    "", // Empty hash means skip verification
		AttackMode: assignment.AttackMode,
	}

	// Download the hashlist file
	if err := jm.fileSync.DownloadFileFromInfo(ctx, fileInfo); err != nil {
		debug.Error("Failed to download hashlist: %v", err)
		return fmt.Errorf("failed to download hashlist: %w", err)
	}

	// Verify the file was created
	if info, err := os.Stat(localPath); err == nil {
		debug.Info("Successfully downloaded hashlist: %s (size: %d bytes)", hashlistFileName, info.Size())
	} else {
		debug.Error("Hashlist file not found after download: %s", localPath)
		return fmt.Errorf("hashlist file not found after download")
	}

	return nil
}

// ensureRuleChunks downloads rule chunk files if the job uses rule splitting
func (jm *JobManager) ensureRuleChunks(ctx context.Context, assignment *JobTaskAssignment) error {
	if jm.fileSync == nil {
		debug.Warning("File sync not initialized, skipping rule chunk download")
		return nil
	}
	
	// Check if this job has rule chunks (rule paths that contain "chunks/")
	hasRuleChunks := false
	for _, rulePath := range assignment.RulePaths {
		if strings.Contains(rulePath, "rules/chunks/") {
			hasRuleChunks = true
			break
		}
	}
	
	if !hasRuleChunks {
		// No rule chunks to download
		return nil
	}
	
	debug.Info("Job uses rule chunks, ensuring they are downloaded")
	
	// Process each rule chunk
	for _, rulePath := range assignment.RulePaths {
		if !strings.HasPrefix(rulePath, "rules/chunks/") {
			continue // Skip non-chunk rules
		}
		
		// Extract the chunk filename and job directory
		// Format: rules/chunks/job_<ID>/chunk_<N>.rule
		parts := strings.Split(rulePath, "/")
		if len(parts) < 3 {
			debug.Error("Invalid rule chunk path format: %s", rulePath)
			continue
		}
		
		var jobDir string
		var chunkFile string
		
		// Check if path includes job directory
		if len(parts) == 4 && strings.HasPrefix(parts[2], "job_") {
			// Format: rules/chunks/job_<ID>/chunk_<N>.rule
			jobDir = parts[2]
			chunkFile = parts[3]
		} else if len(parts) == 3 {
			// Format: rules/chunks/chunk_<N>.rule (legacy)
			chunkFile = parts[2]
		}
		
		// Check if chunk already exists locally
		localPath := filepath.Join(jm.config.DataDirectory, rulePath)
		if _, err := os.Stat(localPath); err == nil {
			debug.Info("Rule chunk already exists locally: %s", localPath)
			continue
		}
		
		// Create directory structure if needed
		localDir := filepath.Dir(localPath)
		if err := os.MkdirAll(localDir, 0755); err != nil {
			debug.Error("Failed to create rule chunk directory %s: %v", localDir, err)
			return fmt.Errorf("failed to create rule chunk directory: %w", err)
		}
		
		// Prepare file info for download
		// The backend serves chunks at /api/files/rule/chunks/<filename> or /api/files/rule/chunks/<jobDir>/<filename>
		var fileInfo *filesync.FileInfo
		if jobDir != "" {
			fileInfo = &filesync.FileInfo{
				Name:     fmt.Sprintf("%s/%s", jobDir, chunkFile),
				FileType: "rule",
				Category: "chunks",
			}
		} else {
			fileInfo = &filesync.FileInfo{
				Name:     chunkFile,
				FileType: "rule",
				Category: "chunks",
			}
		}
		
		debug.Info("Downloading rule chunk: %s", fileInfo.Name)
		if err := jm.fileSync.DownloadFileFromInfo(ctx, fileInfo); err != nil {
			debug.Error("Failed to download rule chunk %s: %v", fileInfo.Name, err)
			return fmt.Errorf("failed to download rule chunk %s: %w", fileInfo.Name, err)
		}
		
		// Verify the file was created
		if fileInfo, err := os.Stat(localPath); err == nil {
			debug.Info("Successfully downloaded rule chunk: %s (size: %d bytes)", chunkFile, fileInfo.Size())
		} else {
			debug.Error("Rule chunk file not found after download: %s", localPath)
			return fmt.Errorf("rule chunk file not found after download")
		}
	}
	
	return nil
}

// ensureAssociationFiles downloads the association wordlist for mode 9 attacks
// Note: The original hashlist is handled by ensureHashlist - backend sends correct path
func (jm *JobManager) ensureAssociationFiles(ctx context.Context, assignment *JobTaskAssignment) error {
	// Only needed for association attacks (mode 9)
	if assignment.AttackMode != 9 {
		return nil
	}

	if jm.fileSync == nil {
		debug.Error("File sync is not initialized in job manager")
		return fmt.Errorf("file sync not initialized")
	}

	// For mode 9, the association wordlist is in WordlistPaths[0]
	// Path format: wordlists/association/{hashlistID}_{filename}
	if len(assignment.WordlistPaths) == 0 {
		debug.Error("No wordlist path provided for association attack")
		return fmt.Errorf("association attack requires wordlist in WordlistPaths[0]")
	}

	assocWordlistPath := assignment.WordlistPaths[0]
	localPath := filepath.Join(jm.config.DataDirectory, assocWordlistPath)

	debug.Info("Ensuring association wordlist is available: %s", assocWordlistPath)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for association wordlist: %w", err)
	}

	// Check if already exists
	if _, err := os.Stat(localPath); err == nil {
		debug.Info("Association wordlist already exists: %s", localPath)
		return nil
	}

	// Extract category and filename from path
	// Path format: wordlists/association/{hashlistID}_{filename}
	parts := strings.Split(assocWordlistPath, "/")
	if len(parts) < 3 {
		debug.Error("Invalid association wordlist path format: %s", assocWordlistPath)
		return fmt.Errorf("invalid association wordlist path format")
	}

	category := parts[1] // "association"
	filename := parts[2] // "{hashlistID}_{filename}"

	debug.Info("Downloading association wordlist: %s (category: %s)", filename, category)

	fileInfo := &filesync.FileInfo{
		Name:     filename,
		FileType: "wordlist",
		Category: category,
	}

	if err := jm.fileSync.DownloadFileFromInfo(ctx, fileInfo); err != nil {
		debug.Error("Failed to download association wordlist: %v", err)
		return fmt.Errorf("failed to download association wordlist: %w", err)
	}

	// Verify the file was created
	if info, err := os.Stat(localPath); err == nil {
		debug.Info("Successfully downloaded association wordlist: %s (size: %d bytes)", filename, info.Size())
	} else {
		debug.Error("Association wordlist file not found after download: %s", localPath)
		return fmt.Errorf("association wordlist file not found after download")
	}

	return nil
}

// cleanupAssociationFiles removes association wordlist after task completion
// Note: Hashlist is NOT cleaned up - it may be reused by other tasks
func (jm *JobManager) cleanupAssociationFiles(assignment *JobTaskAssignment) {
	// Only clean up for association attacks (mode 9)
	if assignment.AttackMode != 9 {
		return
	}

	// Clean up association wordlist from WordlistPaths[0]
	if len(assignment.WordlistPaths) > 0 {
		assocPath := filepath.Join(jm.config.DataDirectory, assignment.WordlistPaths[0])
		if err := os.Remove(assocPath); err != nil && !os.IsNotExist(err) {
			debug.Warning("Failed to remove association wordlist file %s: %v", assocPath, err)
		} else if err == nil {
			debug.Info("Removed association wordlist file: %s", assocPath)
		}
	}
}

// ensureBenchmark runs a benchmark if needed for the job
func (jm *JobManager) ensureBenchmark(ctx context.Context, assignment *JobTaskAssignment) error {
	// We no longer run benchmarks here - the backend will request speed tests
	// through the WebSocket benchmark request message when needed
	debug.Info("Skipping local benchmark - speed tests are now requested by backend")
	return nil
}

// cleanupCompletedTask performs synchronous cleanup of a completed task
// This MUST be called BEFORE logging completion to prevent race conditions
// where GetCurrentTaskStatus() sees stale data in activeJobs
func (jm *JobManager) cleanupCompletedTask(jobExecution *JobExecution, finalStatus string) {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	taskID := jobExecution.Assignment.TaskID

	// Cache completion info before deleting from activeJobs
	if exec, exists := jm.activeJobs[taskID]; exists {
		info := &CompletedTaskInfo{
			TaskID:      taskID,
			JobID:       jobExecution.Assignment.JobExecutionID,
			Status:      finalStatus,
			CompletedAt: time.Now(),
		}

		if exec.LastProgress != nil {
			info.KeyspaceProcessed = exec.LastProgress.KeyspaceProcessed
			info.EffectiveProgress = exec.LastProgress.EffectiveProgress
			info.ProgressPercent = exec.LastProgress.ProgressPercent
			info.TotalEffectiveKeyspace = exec.LastProgress.TotalEffectiveKeyspace
			info.HashRate = exec.LastProgress.HashRate
			info.CrackedCount = exec.LastProgress.CrackedCount
			info.AllHashesCracked = exec.LastProgress.AllHashesCracked
			info.ErrorMessage = exec.LastProgress.ErrorMessage
		}

		jm.lastCompletedTask = info
		debug.Info("Cached completion for task %s with status %s, progress %.2f%%, keyspace %d",
			info.TaskID, info.Status, info.ProgressPercent, info.KeyspaceProcessed)

		// Clean up association attack files after task completion
		jm.cleanupAssociationFiles(exec.Assignment)
	}

	// Remove from activeJobs - this MUST happen synchronously before logging
	delete(jm.activeJobs, taskID)
	debug.Info("Removed task %s from activeJobs (synchronous cleanup)", taskID)
}

// monitorJobProgress monitors job progress and sends updates
func (jm *JobManager) monitorJobProgress(ctx context.Context, jobExecution *JobExecution) {
	// NOTE: We no longer use deferred cleanup - cleanup is done synchronously
	// before logging completion to prevent race conditions

	// Track retry attempts for "already running" errors
	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - cleanup and transition to stopped state
			debug.Info("Context cancelled for task %s, performing cleanup", jobExecution.Assignment.TaskID)
			jm.cleanupCompletedTask(jobExecution, "stopped")
			jm.stateManager.TransitionTo(TaskStateStopped, "")
			debug.Info("State transition: running -> stopped (task: %s, context cancelled)", jobExecution.Assignment.TaskID)
			return
		case progress, ok := <-jobExecution.Process.ProgressChannel:
			if !ok {
				// Channel closed, job finished - cleanup with last known status
				debug.Info("Job progress monitoring ended for task %s (channel closed)", jobExecution.Assignment.TaskID)
				finalStatus := "completed"
				if jobExecution.LastProgress != nil && jobExecution.LastProgress.Status == "failed" {
					finalStatus = "failed"
				}
				jm.cleanupCompletedTask(jobExecution, finalStatus)
				if finalStatus == "completed" {
					jm.stateManager.TransitionTo(TaskStateIdle, "")
				} else {
					jm.stateManager.TransitionTo(TaskStateFailed, "")
				}
				debug.Info("State transition: running -> %s (task: %s, channel closed)", finalStatus, jobExecution.Assignment.TaskID)
				return
			}

			if progress != nil {
				jobExecution.LastProgress = progress

				// Show console progress for running tasks
				if progress.Status == "" || progress.Status == "running" {
					// Calculate total keyspace
					totalKeyspace := jobExecution.Assignment.KeyspaceEnd - jobExecution.Assignment.KeyspaceStart

					// Format and display task progress
					taskProgress := console.TaskProgress{
						TaskID:            progress.TaskID,
						ProgressPercent:   progress.ProgressPercent,
						HashRate:          progress.HashRate,
						TimeRemaining:     0,
						Status:            "running",
						KeyspaceProcessed: progress.KeyspaceProcessed,
						TotalKeyspace:     totalKeyspace,
					}
					if progress.TimeRemaining != nil {
						taskProgress.TimeRemaining = *progress.TimeRemaining
					}
					console.Progress(console.FormatTaskProgress(taskProgress))
				} else if progress.Status == "completed" {
					// NOTE: Success message is now logged AFTER cleanup to prevent race condition
					// See the cleanup section below where console.Success is called
				} else if progress.Status == "failed" {
					console.Error("Task %s failed: %s", progress.TaskID, progress.ErrorMessage)
				}

				// Check if this is a failure due to "already running" error
				if progress.Status == "failed" && jobExecution.Process.AlreadyRunningError && retryCount < MaxHashcatRetries {
					retryCount++
					debug.Info("Task %s failed with 'already running' error, attempting retry %d/%d",
						progress.TaskID, retryCount, MaxHashcatRetries)
					
					// Remove from active jobs
					jm.mutex.Lock()
					delete(jm.activeJobs, jobExecution.Assignment.TaskID)
					jm.mutex.Unlock()
					
					// Wait before retry
					select {
					case <-ctx.Done():
						// Context cancelled during retry wait - task already removed from activeJobs above
						jm.stateManager.TransitionTo(TaskStateStopped, "")
						debug.Info("State transition: running -> stopped (task: %s, context cancelled during retry)", jobExecution.Assignment.TaskID)
						return
					case <-time.After(HashcatRetryDelay):
						// Continue with retry
					}

					// Attempt to restart the job
					newProcess, err := jm.executor.ExecuteTask(ctx, jobExecution.Assignment)
					if err != nil {
						console.Error("Failed to restart task %s on retry %d: %v",
							jobExecution.Assignment.TaskID, retryCount, err)
						// Send final error to backend
						if jm.progressCallback != nil {
							errorProgress := &JobProgress{
								TaskID:       jobExecution.Assignment.TaskID,
								Status:       "failed",
								ErrorMessage: fmt.Sprintf("Failed to restart after %d retries: %v", retryCount, err),
							}
							jm.progressCallback(errorProgress)
						}
						// Task was already removed from activeJobs above, just transition state
						jm.stateManager.TransitionTo(TaskStateFailed, "")
						debug.Info("State transition: running -> failed (task: %s, retry failed)", jobExecution.Assignment.TaskID)
						return
					}
					
					// Update the job execution with new process
					jobExecution.Process = newProcess
					
					// Re-add to active jobs
					jm.mutex.Lock()
					jm.activeJobs[jobExecution.Assignment.TaskID] = jobExecution
					jm.mutex.Unlock()
					
					// Continue monitoring the new process
					continue
				}
				
				// Send to backend via dual callbacks (new approach)
				jm.mutex.RLock()
				hasStatusCallback := jm.statusCallback != nil
				hasCrackCallback := jm.crackCallback != nil
				hasLegacyCallback := jm.progressCallback != nil
				jm.mutex.RUnlock()

				// New dual-channel approach: separate status from cracks
				if hasStatusCallback || hasCrackCallback {
					// Send status update (JobStatus without crack data)
					// IMPORTANT: Skip status updates for crack-only messages (status=="cracked")
					// This prevents zero values from overwriting real progress in the database
					if hasStatusCallback && progress.Status != "cracked" {
						status := &JobStatus{
							TaskID:                 progress.TaskID,
							KeyspaceProcessed:      progress.KeyspaceProcessed,
							EffectiveProgress:      progress.EffectiveProgress,
							ProgressPercent:        progress.ProgressPercent,
							TotalEffectiveKeyspace: progress.TotalEffectiveKeyspace,
							IsFirstUpdate:          progress.IsFirstUpdate,
							HashRate:               progress.HashRate,
							TimeRemaining:          progress.TimeRemaining,
							CrackedCount:           progress.CrackedCount,
							Status:                 progress.Status,
							ErrorMessage:           progress.ErrorMessage,
							DeviceMetrics:          progress.DeviceMetrics,
							AllHashesCracked:       progress.AllHashesCracked,
						}
						jm.statusCallback(status)
					}

					// Send crack batch if there are cracks (CrackBatch with only cracks)
					if hasCrackCallback && len(progress.CrackedHashes) > 0 {
						batch := &CrackBatch{
							TaskID:        progress.TaskID,
							CrackedHashes: progress.CrackedHashes,
						}
						jm.crackCallback(batch)
					}
				} else if hasLegacyCallback {
					// Fallback to legacy callback
					jm.progressCallback(progress)
				}

				// Log any cracked hashes with console output
				if progress.CrackedCount > 0 {
					console.Info("Found %d cracked hashes", progress.CrackedCount)
				}

				// If this was a final status (completed or failed), drain remaining messages then exit
				if progress.Status == "completed" || progress.Status == "failed" {
					// Drain any remaining crack batches from the channel before exiting
					debug.Info("Job %s finished with status %s, draining remaining crack batches", progress.TaskID, progress.Status)

					// Track batches processed during drain to ensure we don't exit prematurely
					drainedBatches := 0
					drainedCracks := 0

				drainLoop:
					for {
						select {
						case remaining, ok := <-jobExecution.Process.ProgressChannel:
							if !ok {
								break drainLoop // Channel closed
							}

							// Process any remaining crack batches
							if remaining != nil && len(remaining.CrackedHashes) > 0 {
								drainedBatches++
								drainedCracks += len(remaining.CrackedHashes)

								debug.Info("Processing %d remaining cracks after job completion (batch %d, total drained: %d)",
									len(remaining.CrackedHashes), drainedBatches, drainedCracks)

								if hasCrackCallback {
									batch := &CrackBatch{
										TaskID:        remaining.TaskID,
										CrackedHashes: remaining.CrackedHashes,
									}
									jm.crackCallback(batch)
								}

								console.Info("Found %d cracked hashes", remaining.CrackedCount)
							}
						case <-time.After(30 * time.Second):
							// No more messages after 30s, safe to exit
							// Increased from 500ms to 30s to handle large crack bursts at job completion
							debug.Info("No more messages in channel after 30s, exiting monitoring for task %s (drained %d batches, %d cracks)",
								progress.TaskID, drainedBatches, drainedCracks)
							break drainLoop
						}
					}

					debug.Info("Drain complete for task %s: processed %d batches with %d total cracks",
						progress.TaskID, drainedBatches, drainedCracks)

					// Send crack_batches_complete signal to backend
					if jm.crackBatchesCompleteCallback != nil {
						completionSignal := &CrackBatchesComplete{
							TaskID: progress.TaskID,
						}
						debug.Info("Sending crack_batches_complete signal for task %s", progress.TaskID)
						jm.crackBatchesCompleteCallback(completionSignal)
					} else {
						debug.Warning("No crack_batches_complete callback set for task %s", progress.TaskID)
					}

					// CRITICAL: Synchronous cleanup BEFORE logging success
					// This prevents race condition where GetCurrentTaskStatus() sees stale data
					finalStatus := progress.Status
					jm.cleanupCompletedTask(jobExecution, finalStatus)

					// Transition to completing state while waiting for ACK (GH Issue #12)
					jm.stateManager.TransitionTo(TaskStateCompleting, progress.TaskID)
					debug.Info("State transition: running -> completing (task: %s)", progress.TaskID)

					// Wait for completion ACK from backend (GH Issue #12)
					// This ensures backend has processed the completion before agent moves on
					jm.mutex.RLock()
					hasAckWaitCallback := jm.ackWaitCallback != nil
					hasStatusCallback := jm.statusCallback != nil
					jm.mutex.RUnlock()

					ackReceived := true // Default to true if no ACK waiting
					if hasAckWaitCallback && finalStatus == "completed" {
						// Create resend function for retries
						resendFunc := func() error {
							if hasStatusCallback {
								status := &JobStatus{
									TaskID:                 progress.TaskID,
									KeyspaceProcessed:      progress.KeyspaceProcessed,
									EffectiveProgress:      progress.EffectiveProgress,
									ProgressPercent:        progress.ProgressPercent,
									TotalEffectiveKeyspace: progress.TotalEffectiveKeyspace,
									HashRate:               progress.HashRate,
									CrackedCount:           progress.CrackedCount,
									Status:                 progress.Status,
									AllHashesCracked:       progress.AllHashesCracked,
								}
								jm.statusCallback(status)
							}
							return nil
						}
						ackReceived = jm.ackWaitCallback(progress.TaskID, resendFunc)
					}

					// Transition state machine to idle (or failed)
					if finalStatus == "completed" {
						if !ackReceived {
							// ACK not received after retries - set completion_pending flag
							jm.stateManager.SetCompletionPending(progress.TaskID)
							debug.Warning("Completion ACK not received for task %s, setting completion_pending flag", progress.TaskID)
						}
						jm.stateManager.TransitionTo(TaskStateIdle, "")
						debug.Info("State transition: completing -> idle (task: %s completed, ack_received=%v)", progress.TaskID, ackReceived)
						// NOW log success - after cleanup is complete
						console.Success("Task %s completed successfully", progress.TaskID)
						if progress.CrackedCount > 0 {
							console.Success("Found %d cracked hashes", progress.CrackedCount)
						}
					} else {
						jm.stateManager.TransitionTo(TaskStateFailed, "")
						debug.Info("State transition: completing -> failed (task: %s)", progress.TaskID)
					}

					return
				}
			}
		}
	}
}

// StopJob stops a running job
func (jm *JobManager) StopJob(taskID string) error {
	jm.mutex.RLock()
	jobExecution, exists := jm.activeJobs[taskID]
	jm.mutex.RUnlock()

	if !exists {
		// Check if we're in a state where we think we should have this task
		state, stateTaskID := jm.stateManager.GetState()
		if stateTaskID == taskID {
			debug.Warning("StopJob: task %s not in activeJobs but state says %s", taskID, state)
		}
		return fmt.Errorf("job %s not found", taskID)
	}

	// Stopping message is already shown by main.go shutdown

	err := jm.executor.StopTask(taskID)
	if err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	// Update job status
	jobExecution.Status = "stopped"

	// Perform synchronous cleanup
	jm.cleanupCompletedTask(jobExecution, "stopped")

	// Transition state to stopped
	jm.stateManager.TransitionTo(TaskStateStopped, "")
	debug.Info("State transition: running -> stopped (task: %s manually stopped)", taskID)

	debug.Info("Job stopped: Task ID %s", taskID)
	return nil
}

// GetJobStatus returns the status of a specific job
func (jm *JobManager) GetJobStatus(taskID string) (*JobExecution, error) {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	jobExecution, exists := jm.activeJobs[taskID]
	if !exists {
		return nil, fmt.Errorf("job %s not found", taskID)
	}

	return jobExecution, nil
}

// GetActiveJobs returns a list of currently active jobs
func (jm *JobManager) GetActiveJobs() map[string]*JobExecution {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	// Return a copy to avoid concurrent access issues
	activeJobs := make(map[string]*JobExecution)
	for taskID, job := range jm.activeJobs {
		activeJobs[taskID] = job
	}

	return activeJobs
}

// ForceCleanup forces cleanup of all active jobs and hashcat processes
func (jm *JobManager) ForceCleanup() error {
	console.Status("Forcing cleanup of all active jobs")
	
	// Stop all active jobs
	jm.mutex.Lock()
	for taskID := range jm.activeJobs {
		debug.Info("Stopping active job: %s", taskID)
	}
	// Clear the active jobs map
	jm.activeJobs = make(map[string]*JobExecution)
	jm.mutex.Unlock()
	
	// Force cleanup in the executor
	return jm.executor.ForceCleanup()
}

// GetBenchmarkResults returns cached benchmark results
func (jm *JobManager) GetBenchmarkResults() map[string]*BenchmarkResult {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()

	// Return a copy to avoid concurrent access issues
	benchmarks := make(map[string]*BenchmarkResult)
	for key, result := range jm.benchmarkCache {
		benchmarks[key] = result
	}

	return benchmarks
}

// RunManualBenchmark runs a benchmark manually for testing purposes
func (jm *JobManager) RunManualBenchmark(ctx context.Context, binaryPath string, hashType int, attackMode int) (*BenchmarkResult, error) {
	// Manual benchmarks are no longer supported - use speed tests through WebSocket
	// The backend should send a benchmark request with full job configuration
	return nil, fmt.Errorf("manual benchmarks are deprecated - use speed tests through WebSocket benchmark requests")
}

// GetExecutor returns the executor for direct access
// Note: Returns ExecutorInterface which could be real or mock
func (jm *JobManager) GetExecutor() ExecutorInterface {
	return jm.executor
}

// Shutdown gracefully shuts down the job manager
func (jm *JobManager) Shutdown(ctx context.Context) error {
	// Shutdown message is already shown by main.go

	jm.mutex.RLock()
	activeTaskIDs := make([]string, 0, len(jm.activeJobs))
	for taskID := range jm.activeJobs {
		activeTaskIDs = append(activeTaskIDs, taskID)
	}
	jm.mutex.RUnlock()

	// Stop all active jobs
	for _, taskID := range activeTaskIDs {
		err := jm.StopJob(taskID)
		if err != nil {
			debug.Error("Error stopping job %s during shutdown: %v", taskID, err)
		}
	}

	// Wait for jobs to stop (with timeout)
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-shutdownCtx.Done():
			log.Println("Job manager shutdown timeout reached")
			return shutdownCtx.Err()
		case <-ticker.C:
			jm.mutex.RLock()
			activeCount := len(jm.activeJobs)
			jm.mutex.RUnlock()

			if activeCount == 0 {
				// Shutdown completion is shown by main.go
				return nil
			}

			debug.Info("Waiting for %d jobs to stop...", activeCount)
		}
	}
}

// Legacy function for compatibility
func ProcessJobs() {
	log.Println("ProcessJobs called - this is now handled by JobManager")
}

// RetransmitOutfile reads an outfile and returns all cracks for retransmission
func (jm *JobManager) RetransmitOutfile(taskID string) ([]CrackedHash, error) {
	return jm.executor.RetransmitOutfile(taskID)
}

// DeleteOutfile removes the outfile for a task after backend confirmation
func (jm *JobManager) DeleteOutfile(taskID string) error {
	return jm.executor.DeleteOutfile(taskID)
}

// GetPendingOutfiles returns all task IDs with unacknowledged outfiles
func (jm *JobManager) GetPendingOutfiles() (taskIDs []string, currentTaskID string, err error) {
	return jm.executor.GetPendingOutfiles()
}

// GetOutfileLineCount returns the number of lines in the outfile for a task
func (jm *JobManager) GetOutfileLineCount(taskID string) (int64, error) {
	return jm.executor.GetOutfileLineCount(taskID)
}

// StuckDetectionTimeout is the maximum time to stay in Completing state before force recovery
const StuckDetectionTimeout = 2 * time.Minute

// StuckCheckInterval is how often to check for stuck states
const StuckCheckInterval = 30 * time.Second

// StartStuckDetection starts a background goroutine that monitors for stuck states (GH Issue #12)
// This is a safety net - if agent gets stuck in Completing state for too long, force recovery
func (jm *JobManager) StartStuckDetection(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(StuckCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				debug.Debug("Stuck detection loop stopping (context cancelled)")
				return
			case <-ticker.C:
				jm.checkForStuckState()
			}
		}
	}()
	debug.Info("Started stuck state detection (check interval: %v, timeout: %v)", StuckCheckInterval, StuckDetectionTimeout)
}

// checkForStuckState checks if the agent is stuck in a non-terminal state
func (jm *JobManager) checkForStuckState() {
	state, taskID, stateChangedAt := jm.stateManager.GetStateInfo()
	stuckDuration := time.Since(stateChangedAt)

	// Only check Completing state for now - Running state is handled by hashcat timeout
	if state == TaskStateCompleting && stuckDuration > StuckDetectionTimeout {
		debug.Warning("Force recovering from stuck Completing state: task %s stuck for %v (GH Issue #12)",
			taskID, stuckDuration)
		jm.forceRecovery(taskID)
	}
}

// forceRecovery forces the agent back to idle state when stuck
func (jm *JobManager) forceRecovery(taskID string) {
	jm.mutex.Lock()
	// Remove from activeJobs if still present
	if _, exists := jm.activeJobs[taskID]; exists {
		delete(jm.activeJobs, taskID)
		debug.Info("Force removed stuck task %s from activeJobs", taskID)
	}
	jm.mutex.Unlock()

	// Set completion pending so it can be resolved on next state sync
	jm.stateManager.SetCompletionPending(taskID)

	// Transition to idle
	jm.stateManager.TransitionTo(TaskStateIdle, "")
	debug.Info("Force recovered from stuck state: task %s, now idle with completion_pending flag", taskID)
}
