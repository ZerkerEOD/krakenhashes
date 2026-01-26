package jobs

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// MockHashcatExecutor simulates hashcat execution for testing without real GPUs
type MockHashcatExecutor struct {
	dataDirectory      string
	mutex              sync.RWMutex
	activeTasks        map[string]*MockTask
	outputCallback     func(taskID string, output string, isError bool)
	deviceFlagsCallback func() string
	agentExtraParams   string

	// Configuration from environment variables
	progressSpeed  time.Duration // How fast to complete jobs
	crackRate      float64       // Percentage of hashes to crack (0-100)
	hashRate       int64         // Simulated H/s
	gpuCount       int           // Number of fake GPUs
	gpuVendor      string        // nvidia/amd/intel
}

// MockTask represents a simulated task execution
type MockTask struct {
	assignment      *JobTaskAssignment
	process         *HashcatProcess
	ctx             context.Context
	cancel          context.CancelFunc
	progressTicker  *time.Ticker
	currentProgress float64
	crackedHashes   []*CrackedHash
	mutex           sync.Mutex
	firstUpdateSent bool // Track if first update has been sent
}

// NewMockHashcatExecutor creates a new mock hashcat executor
func NewMockHashcatExecutor(dataDirectory string) *MockHashcatExecutor {
	// Load configuration from environment variables with defaults
	progressSpeed := getEnvDuration("MOCK_PROGRESS_SPEED", 300*time.Second)
	crackRate := getEnvFloat("MOCK_CRACK_RATE", 5.0)
	hashRate := getEnvInt64("MOCK_HASH_RATE", 1000000000) // 1 GH/s default
	gpuCount := getEnvInt("MOCK_GPU_COUNT", 2)
	gpuVendor := getEnvString("MOCK_GPU_VENDOR", "nvidia")

	debug.Info("Creating mock hashcat executor with config:")
	debug.Info("  Progress Speed: %v", progressSpeed)
	debug.Info("  Crack Rate: %.1f%%", crackRate)
	debug.Info("  Hash Rate: %d H/s", hashRate)
	debug.Info("  GPU Count: %d", gpuCount)
	debug.Info("  GPU Vendor: %s", gpuVendor)

	return &MockHashcatExecutor{
		dataDirectory: dataDirectory,
		activeTasks:   make(map[string]*MockTask),
		progressSpeed: progressSpeed,
		crackRate:     crackRate,
		hashRate:      hashRate,
		gpuCount:      gpuCount,
		gpuVendor:     gpuVendor,
	}
}

// SetOutputCallback sets the callback for hashcat output
func (e *MockHashcatExecutor) SetOutputCallback(callback func(taskID string, output string, isError bool)) {
	e.outputCallback = callback
}

// SetDeviceFlagsCallback sets the callback for device flags
func (e *MockHashcatExecutor) SetDeviceFlagsCallback(callback func() string) {
	e.deviceFlagsCallback = callback
}

// SetAgentExtraParams sets extra hashcat parameters
func (e *MockHashcatExecutor) SetAgentExtraParams(params string) {
	e.agentExtraParams = params
}

// ExecuteTask simulates task execution
func (e *MockHashcatExecutor) ExecuteTask(ctx context.Context, assignment *JobTaskAssignment) (*HashcatProcess, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Check if task already exists
	if _, exists := e.activeTasks[assignment.TaskID]; exists {
		return nil, fmt.Errorf("task %s is already running", assignment.TaskID)
	}

	debug.Info("Mock executor starting task %s", assignment.TaskID)

	// Create progress channel
	progressChan := make(chan *JobProgress, 100)

	// Create mock process
	process := &HashcatProcess{
		TaskID:          assignment.TaskID,
		ProgressChannel: progressChan,
		OutfilePath:     "", // No real outfile in mock mode
	}

	// Create context for this task
	taskCtx, cancel := context.WithCancel(ctx)

	// Calculate keyspace
	totalKeyspace := assignment.KeyspaceEnd - assignment.KeyspaceStart

	// Create mock task
	task := &MockTask{
		assignment:      assignment,
		process:         process,
		ctx:             taskCtx,
		cancel:          cancel,
		currentProgress: 0,
		crackedHashes:   make([]*CrackedHash, 0),
	}

	e.activeTasks[assignment.TaskID] = task

	// Start simulation goroutine
	go e.simulateTask(task, totalKeyspace)

	return process, nil
}

// simulateTask runs the task simulation
func (e *MockHashcatExecutor) simulateTask(task *MockTask, totalKeyspace int64) {
	defer func() {
		e.mutex.Lock()
		delete(e.activeTasks, task.assignment.TaskID)
		e.mutex.Unlock()
		close(task.process.ProgressChannel)
	}()

	// Calculate progress increment per tick (100% / number of ticks)
	tickInterval := 2 * time.Second
	numTicks := int(e.progressSpeed.Seconds() / tickInterval.Seconds())
	progressIncrement := 100.0 / float64(numTicks)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	debug.Info("Mock task %s: will complete in %v with %d ticks", task.assignment.TaskID, e.progressSpeed, numTicks)

	// Send initial progress with TotalEffectiveKeyspace
	e.sendProgress(task, 0, totalKeyspace, false, true)

	// Generate some cracks if crack rate > 0
	var crackedHashCount int
	if e.crackRate > 0 {
		// Estimate total hashes (we don't know exact count, use arbitrary number)
		estimatedTotalHashes := 1000
		crackedHashCount = int(float64(estimatedTotalHashes) * e.crackRate / 100.0)
		debug.Info("Mock task %s: will crack ~%d hashes", task.assignment.TaskID, crackedHashCount)
	}

	cracksReported := 0

	for {
		select {
		case <-task.ctx.Done():
			// Task was stopped
			debug.Info("Mock task %s: stopped by request", task.assignment.TaskID)
			task.process.ProgressChannel <- &JobProgress{
				TaskID:       task.assignment.TaskID,
				Status:       "stopped",
				ErrorMessage: "Task stopped by user",
			}
			return

		case <-ticker.C:
			task.mutex.Lock()
			task.currentProgress += progressIncrement
			if task.currentProgress > 100 {
				task.currentProgress = 100
			}
			currentProgress := task.currentProgress
			task.mutex.Unlock()

			// Randomly generate cracks throughout execution
			if cracksReported < crackedHashCount && rand.Float64() < 0.3 {
				numCracks := rand.Intn(5) + 1
				if cracksReported+numCracks > crackedHashCount {
					numCracks = crackedHashCount - cracksReported
				}

				cracks := e.generateMockCracks(task.assignment, numCracks)
				cracksReported += numCracks

				// Send crack batch
				e.sendCrackBatch(task, cracks)
			}

			// Send progress update
			isComplete := currentProgress >= 100.0
			e.sendProgress(task, currentProgress, totalKeyspace, isComplete, false)

			if isComplete {
				debug.Info("Mock task %s: completed successfully", task.assignment.TaskID)

				// Send any remaining cracks
				if cracksReported < crackedHashCount {
					remaining := crackedHashCount - cracksReported
					cracks := e.generateMockCracks(task.assignment, remaining)
					e.sendCrackBatch(task, cracks)
				}

				// Send completion message
				task.process.ProgressChannel <- &JobProgress{
					TaskID:            task.assignment.TaskID,
					KeyspaceProcessed: totalKeyspace,
					EffectiveProgress: totalKeyspace,
					ProgressPercent:   100.0,
					HashRate:          e.hashRate,
					Status:            "completed",
					CrackedCount:      crackedHashCount,
				}
				return
			}
		}
	}
}

// sendProgress sends a progress update
func (e *MockHashcatExecutor) sendProgress(task *MockTask, progressPercent float64, totalKeyspace int64, isComplete bool, isFirstUpdate bool) {
	keyspaceProcessed := int64(float64(totalKeyspace) * progressPercent / 100.0)

	progress := &JobProgress{
		TaskID:            task.assignment.TaskID,
		KeyspaceProcessed: keyspaceProcessed,
		EffectiveProgress: keyspaceProcessed,
		ProgressPercent:   progressPercent,
		HashRate:          e.hashRate,
		Status:            "running",
		DeviceMetrics:     e.generateDeviceMetrics(),
	}

	// On first update, include total effective keyspace (chunk keyspace, not job keyspace)
	if isFirstUpdate && !task.firstUpdateSent {
		progress.TotalEffectiveKeyspace = &totalKeyspace
		progress.IsFirstUpdate = true
		task.firstUpdateSent = true
		debug.Info("Mock task %s: First update with total effective keyspace %d", task.assignment.TaskID, totalKeyspace)
	}

	// Calculate time remaining
	if progressPercent > 0 && progressPercent < 100 {
		// Estimate based on progress speed
		remaining := (100.0 - progressPercent) / 100.0 * e.progressSpeed.Seconds()
		remainingInt := int(remaining)
		progress.TimeRemaining = &remainingInt
	}

	select {
	case task.process.ProgressChannel <- progress:
	case <-task.ctx.Done():
	}
}

// sendCrackBatch sends a batch of cracks
func (e *MockHashcatExecutor) sendCrackBatch(task *MockTask, cracks []*CrackedHash) {
	if len(cracks) == 0 {
		return
	}

	// Convert to slice (not pointer slice)
	crackSlice := make([]CrackedHash, len(cracks))
	for i, c := range cracks {
		crackSlice[i] = *c
	}

	progress := &JobProgress{
		TaskID:        task.assignment.TaskID,
		Status:        "cracked",
		CrackedHashes: crackSlice,
		CrackedCount:  len(cracks),
	}

	select {
	case task.process.ProgressChannel <- progress:
	case <-task.ctx.Done():
	}
}

// generateMockCracks creates fake cracked hashes
func (e *MockHashcatExecutor) generateMockCracks(assignment *JobTaskAssignment, count int) []*CrackedHash {
	cracks := make([]*CrackedHash, count)

	for i := 0; i < count; i++ {
		// Generate random-ish hash and password
		hash := fmt.Sprintf("MOCK_%s_%d", assignment.TaskID[:8], rand.Int63())
		password := generateMockPassword()

		cracks[i] = &CrackedHash{
			Hash:  hash,
			Plain: password,
		}
	}

	return cracks
}

// generateMockPassword creates a realistic-looking password
func generateMockPassword() string {
	patterns := []string{
		"Password%d!",
		"Summer%d!",
		"Winter%d@",
		"Admin%d#",
		"Test%d$",
		"User%d%%",
		"Welcome%d",
		"Qwerty%d!",
		"Letmein%d",
		"Monkey%d!",
	}

	pattern := patterns[rand.Intn(len(patterns))]
	number := rand.Intn(9999) + 1

	return fmt.Sprintf(pattern, number)
}

// generateDeviceMetrics creates fake device metrics
func (e *MockHashcatExecutor) generateDeviceMetrics() []DeviceMetric {
	metrics := make([]DeviceMetric, e.gpuCount)

	for i := 0; i < e.gpuCount; i++ {
		// Randomize metrics slightly to look realistic
		tempVariance := float64(rand.Intn(10) - 5)
		utilVariance := float64(rand.Intn(10) - 5)

		metrics[i] = DeviceMetric{
			DeviceID:   i + 1,
			DeviceName: fmt.Sprintf("Mock GPU %d", i+1),
			Temp:       65.0 + tempVariance,
			Util:       95.0 + utilVariance,
			Speed:      e.hashRate / int64(e.gpuCount),
		}
	}

	return metrics
}

// StopTask stops a running task
func (e *MockHashcatExecutor) StopTask(taskID string) error {
	e.mutex.Lock()
	task, exists := e.activeTasks[taskID]
	e.mutex.Unlock()

	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	debug.Info("Mock executor stopping task %s", taskID)
	task.cancel()

	return nil
}

// GetTaskProgress returns the progress of a task
func (e *MockHashcatExecutor) GetTaskProgress(taskID string) (*JobProgress, error) {
	e.mutex.RLock()
	task, exists := e.activeTasks[taskID]
	e.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	task.mutex.Lock()
	progress := task.currentProgress
	task.mutex.Unlock()

	return &JobProgress{
		TaskID:          taskID,
		ProgressPercent: progress,
		Status:          "running",
	}, nil
}

// GetActiveTaskIDs returns IDs of all active tasks
func (e *MockHashcatExecutor) GetActiveTaskIDs() []string {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	ids := make([]string, 0, len(e.activeTasks))
	for id := range e.activeTasks {
		ids = append(ids, id)
	}
	return ids
}

// ForceCleanup stops all running tasks
func (e *MockHashcatExecutor) ForceCleanup() error {
	e.mutex.Lock()
	tasks := make([]*MockTask, 0, len(e.activeTasks))
	for _, task := range e.activeTasks {
		tasks = append(tasks, task)
	}
	e.activeTasks = make(map[string]*MockTask)
	e.mutex.Unlock()

	for _, task := range tasks {
		task.cancel()
	}

	debug.Info("Mock executor cleaned up %d tasks", len(tasks))
	return nil
}

// calculateKeyspace calculates the total effective keyspace based on attack configuration
// This simulates what hashcat reports in progress[1] during benchmarks
func (e *MockHashcatExecutor) calculateKeyspace(assignment *JobTaskAssignment) int64 {
	switch assignment.AttackMode {
	case 0: // Wordlist attack
		var totalKeyspace int64 = 0

		// Count wordlist lines
		for _, wordlistPath := range assignment.WordlistPaths {
			fullPath := filepath.Join(e.dataDirectory, wordlistPath)
			lines, err := countFileLines(fullPath)
			if err != nil {
				debug.Warning("Failed to count wordlist lines for %s: %v", wordlistPath, err)
				continue
			}
			debug.Info("Wordlist %s has %d lines", wordlistPath, lines)
			totalKeyspace += lines
		}

		// If rules are present, multiply by rule count
		if len(assignment.RulePaths) > 0 {
			var totalRules int64 = 0
			for _, rulePath := range assignment.RulePaths {
				fullPath := filepath.Join(e.dataDirectory, rulePath)
				lines, err := countFileLines(fullPath)
				if err != nil {
					debug.Warning("Failed to count rule lines for %s: %v", rulePath, err)
					continue
				}
				debug.Info("Rule file %s has %d lines", rulePath, lines)
				totalRules += lines
			}
			if totalRules > 0 {
				debug.Info("Multiplying keyspace %d by %d rules = %d", totalKeyspace, totalRules, totalKeyspace*totalRules)
				totalKeyspace *= totalRules
			}
		}

		return totalKeyspace

	case 3: // Mask attack
		// For mask attacks, use a large estimated keyspace
		// Real hashcat calculates based on charset and mask
		return 1500000000 // 1.5B default for masks

	default:
		// For other attack modes, return a reasonable default
		return 600000000 // 600M default
	}
}

// countFileLines counts lines in a file by simply counting newline characters
// This is much faster than scanning tokens and works for mock data
func countFileLines(filepath string) (int64, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int64
	buf := make([]byte, 32*1024) // 32KB buffer for reading

	for {
		n, err := file.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
	}

	return count, nil
}

// RunSpeedTest simulates a speed test
func (e *MockHashcatExecutor) RunSpeedTest(ctx context.Context, assignment *JobTaskAssignment, testDuration int) (int64, []DeviceSpeed, int64, error) {
	debug.Info("Mock executor running speed test for hash type %d", assignment.HashType)

	// Simulate benchmark time (much faster than real)
	time.Sleep(1 * time.Second)

	// Generate device speeds
	deviceSpeeds := make([]DeviceSpeed, e.gpuCount)
	for i := 0; i < e.gpuCount; i++ {
		deviceSpeeds[i] = DeviceSpeed{
			DeviceID: i + 1,
			Speed:    e.hashRate / int64(e.gpuCount),
		}
	}

	// Calculate effective keyspace (simulates hashcat's progress[1] during benchmark)
	effectiveKeyspace := e.calculateKeyspace(assignment)
	debug.Info("Mock benchmark: calculated effective keyspace = %d", effectiveKeyspace)

	// Return total speed, device speeds, and effective keyspace
	return e.hashRate, deviceSpeeds, effectiveKeyspace, nil
}

// Helper functions to load configuration from environment

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if seconds, err := strconv.Atoi(val); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvString(key string, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// RetransmitOutfile returns empty cracks for mock executor (no real outfiles)
func (m *MockHashcatExecutor) RetransmitOutfile(taskID string) ([]CrackedHash, error) {
	debug.Info("Mock executor: RetransmitOutfile called for task %s (no-op)", taskID)
	return nil, nil
}

// DeleteOutfile is a no-op for mock executor
func (m *MockHashcatExecutor) DeleteOutfile(taskID string) error {
	debug.Info("Mock executor: DeleteOutfile called for task %s (no-op)", taskID)
	return nil
}

// GetPendingOutfiles returns empty list for mock executor (no real outfiles)
func (m *MockHashcatExecutor) GetPendingOutfiles() (taskIDs []string, currentTaskID string, err error) {
	debug.Info("Mock executor: GetPendingOutfiles called (no-op)")
	return nil, "", nil
}

// GetOutfileLineCount returns 0 for mock executor (no real outfiles)
func (m *MockHashcatExecutor) GetOutfileLineCount(taskID string) (int64, error) {
	debug.Info("Mock executor: GetOutfileLineCount called for task %s (no-op)", taskID)
	return 0, nil
}
