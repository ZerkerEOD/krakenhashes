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
	executor         ExecutorInterface
	config           *config.Config
	statusCallback   func(*JobStatus)   // Callback for status updates (synchronous)
	crackCallback    func(*CrackBatch)  // Callback for crack batches (asynchronous)
	progressCallback func(*JobProgress) // Legacy callback (deprecated, use statusCallback/crackCallback)
	outputCallback   func(taskID string, output string, isError bool) // Callback for sending output via websocket
	fileSync         *filesync.FileSync
	hwMonitor        HardwareMonitor // Interface for hardware monitor

	// Job state
	mutex           sync.RWMutex
	activeJobs      map[string]*JobExecution
	benchmarkCache  map[string]*BenchmarkResult
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

// GetCurrentTaskStatus returns information about the currently running task
func (jm *JobManager) GetCurrentTaskStatus() (hasTask bool, taskID string, jobID string, keyspaceProcessed int64) {
	jm.mutex.RLock()
	defer jm.mutex.RUnlock()
	
	if len(jm.activeJobs) == 0 {
		return false, "", "", 0
	}
	
	// Return the first active job (agents typically run one job at a time)
	for taskID, execution := range jm.activeJobs {
		if execution.Assignment != nil {
			jobID = execution.Assignment.JobExecutionID
		}
		if execution.LastProgress != nil {
			keyspaceProcessed = execution.LastProgress.KeyspaceProcessed
		}
		return true, taskID, jobID, keyspaceProcessed
	}
	
	return false, "", "", 0
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

// ProcessJobAssignment processes a job assignment from the backend
func (jm *JobManager) ProcessJobAssignment(ctx context.Context, assignmentData []byte) error {
	var assignment JobTaskAssignment
	err := json.Unmarshal(assignmentData, &assignment)
	if err != nil {
		return fmt.Errorf("failed to unmarshal job assignment: %w", err)
	}

	// Processing is already shown by "Task received" message
	debug.Info("Hashlist ID: %d, Hashlist Path: %s", assignment.HashlistID, assignment.HashlistPath)
	debug.Info("Wordlist paths: %v", assignment.WordlistPaths)
	debug.Info("Rule paths: %v", assignment.RulePaths)
	debug.Info("Attack mode: %d, Hash type: %d", assignment.AttackMode, assignment.HashType)

	// Check if task is already running
	jm.mutex.RLock()
	if _, exists := jm.activeJobs[assignment.TaskID]; exists {
		jm.mutex.RUnlock()
		return fmt.Errorf("task %s is already running", assignment.TaskID)
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

	// Build the expected local path
	hashlistFileName := fmt.Sprintf("%d.hash", assignment.HashlistID)
	localPath := filepath.Join(jm.config.DataDirectory, "hashlists", hashlistFileName)
	
	debug.Info("Ensuring hashlist %d is available", assignment.HashlistID)
	debug.Info("Expected local path: %s", localPath)
	debug.Info("Data directory: %s", jm.config.DataDirectory)

	// Always re-download hashlist for each task to ensure we have a fresh copy
	// This prevents issues with stale/modified hashlists from previous tasks
	if _, err := os.Stat(localPath); err == nil {
		debug.Info("Removing existing hashlist to download fresh copy: %s", localPath)
		if err := os.Remove(localPath); err != nil {
			debug.Warning("Failed to remove existing hashlist (will overwrite): %v", err)
			// Continue anyway - download will overwrite
		}
	}
	
	debug.Info("Hashlist file not found locally, need to download from backend")
	debug.Info("Creating FileInfo for download - Name: %s, Type: hashlist, ID: %d", hashlistFileName, assignment.HashlistID)
	
	// Create FileInfo for download
	// Note: For hashlists, we don't have the MD5 hash upfront, so we'll download without hash verification
	fileInfo := &filesync.FileInfo{
		Name:     hashlistFileName,
		FileType: "hashlist",
		ID:       int(assignment.HashlistID),
		MD5Hash:  "", // Empty hash means skip verification
	}
	
	// Download the hashlist file
	debug.Info("Starting download of hashlist %d", assignment.HashlistID)
	if err := jm.fileSync.DownloadFileFromInfo(ctx, fileInfo); err != nil {
		debug.Error("Failed to download hashlist %d: %v", assignment.HashlistID, err)
		return fmt.Errorf("failed to download hashlist %d: %w", assignment.HashlistID, err)
	}
	
	// Verify the file was created
	if fileInfo, err := os.Stat(localPath); err == nil {
		debug.Info("Successfully downloaded hashlist file: %s (size: %d bytes)", hashlistFileName, fileInfo.Size())
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

// ensureBenchmark runs a benchmark if needed for the job
func (jm *JobManager) ensureBenchmark(ctx context.Context, assignment *JobTaskAssignment) error {
	// We no longer run benchmarks here - the backend will request speed tests
	// through the WebSocket benchmark request message when needed
	debug.Info("Skipping local benchmark - speed tests are now requested by backend")
	return nil
}

// monitorJobProgress monitors job progress and sends updates
func (jm *JobManager) monitorJobProgress(ctx context.Context, jobExecution *JobExecution) {
	defer func() {
		jm.mutex.Lock()
		delete(jm.activeJobs, jobExecution.Assignment.TaskID)
		jm.mutex.Unlock()
	}()

	// Track retry attempts for "already running" errors
	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case progress, ok := <-jobExecution.Process.ProgressChannel:
			if !ok {
				// Channel closed, job finished
				debug.Info("Job progress monitoring ended for task %s", jobExecution.Assignment.TaskID)
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
					console.Success("Task %s completed successfully", progress.TaskID)
					if progress.CrackedCount > 0 {
						console.Success("Found %d cracked hashes", progress.CrackedCount)
					}
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
		return fmt.Errorf("job %s not found", taskID)
	}

	// Stopping message is already shown by main.go shutdown
	
	err := jm.executor.StopTask(taskID)
	if err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	// Update job status
	jobExecution.Status = "stopped"
	
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
