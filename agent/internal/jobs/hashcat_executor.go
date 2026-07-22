package jobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// Speed-test failure sentinels. The benchmark handler in connection.go uses
// errors.Is to set error_code on the result so the backend can format an
// admin-friendly message. Keep in sync with backend wsservice.BenchmarkError*.
var (
	ErrBenchmarkTimeout        = fmt.Errorf("BENCHMARK_TIMEOUT")
	ErrBenchmarkZeroSpeed      = fmt.Errorf("BENCHMARK_ZERO_SPEED")
	ErrBenchmarkNoHashesLoaded = fmt.Errorf("BENCHMARK_NO_HASHES_LOADED")
)

// Task runtime error codes. When hashcat fails mid-task, the agent prefixes the
// failure ErrorMessage with one of these so the backend's errorclass.Classify
// maps it to the right category (agent-persistent vs transient) WITHOUT having
// to parse hashcat's English. Keep these strings in sync with the markers in
// backend/internal/services/errorclass/classify.go.
const (
	taskErrNoDevice = "AGENT_NO_DEVICE" // no GPU/accelerator visible
	taskErrDriver   = "AGENT_DRIVER"    // missing/incompatible CUDA/HIP/OpenCL runtime
	taskErrOOM      = "AGENT_OOM"       // out of (GPU) memory
	taskErrDiskFull = "AGENT_DISK_FULL" // no space left on device
	taskErrWatchdog = "GPU_WATCHDOG"    // GPU watchdog / thermal alarm
	taskErrAutotune = "AGENT_AUTOTUNE"  // kernel autotune failed → device skipped, nothing ran (retryable with --force)
	taskErrNoWork   = "AGENT_NO_WORK"   // hashcat exited 0 but never processed any candidates
)

// isAutotuneSkip reports whether a hashcat output line indicates a kernel
// autotune failure that skips the device. On some GPUs a tiny job trips
// "Kernel minimum runtime larger than default TDR" → "Aborting session due to
// kernel autotune failures, for all active devices." → "Device #N: skipped, due
// to kernel autotune failure (-4)", after which hashcat exits 0 having tested
// nothing. We detect this to avoid mis-reporting the task completed and to retry
// with --force (which bypasses autotune). Checked on both stdout and stderr
// because hashcat may route these lines to either.
func isAutotuneSkip(line string) bool {
	l := strings.ToLower(line)
	return strings.Contains(l, "kernel autotune failure") ||
		strings.Contains(l, "aborting session due to kernel autotune failures")
}

// classifyHashcatStderr maps a single hashcat stderr/stdout line to a task error
// code, or "" if the line carries no recognized fault. Checked most-specific
// first so e.g. an allocation failure isn't mistaken for a generic device error.
// Mirrors the agent-side markers in the backend classifier.
func classifyHashcatStderr(line string) string {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "no space left on device") || strings.Contains(l, "enospc"):
		return taskErrDiskFull
	case strings.Contains(l, "out of memory") ||
		strings.Contains(l, "cl_out_of_resources") ||
		strings.Contains(l, "cl_mem_object_allocation_failure") ||
		strings.Contains(l, "cudaerrormemoryallocation") ||
		strings.Contains(l, "out_of_memory"):
		return taskErrOOM
	case strings.Contains(l, "no devices found") ||
		strings.Contains(l, "no devices left") ||
		strings.Contains(l, "cl_device_not_found") ||
		strings.Contains(l, "clgetdeviceids"):
		return taskErrNoDevice
	case strings.Contains(l, "no opencl") ||
		strings.Contains(l, "compatible platform found") ||
		strings.Contains(l, "clgetplatformids") ||
		strings.Contains(l, "cuinit") ||
		strings.Contains(l, "no cuda-capable device"):
		return taskErrDriver
	case strings.Contains(l, "watchdog") ||
		strings.Contains(l, "temperature limit"):
		// GPU watchdog alarm or a thermal-limit abort ("Temperature limit on GPU
		// #N reached, aborting"). Both stop the device mid-run; hashcat may then
		// exit 1 ("exhausted") despite not finishing — see DeviceAbort handling.
		return taskErrWatchdog
	case strings.Contains(l, "memory access fault") ||
		strings.Contains(l, "page not present or supervisor privilege"):
		// A hard GPU driver crash mid-run — e.g. AMD ROCm/amdgpu "Memory access
		// fault by GPU node-N (...) on address (nil). Reason: Page not present or
		// supervisor privilege." This is a device crash, not thermal, but it's
		// still a device abort: the GPU stops and hashcat may exit 1 ("exhausted")
		// with progress[0] < progress[1]. Reuse taskErrWatchdog so it (a) sets
		// DeviceAbort → the exit handler fails-not-completes and the unsearched
		// remainder re-dispatches from the recovery point instead of counting as
		// done, and (b) classifies transient so the agent is re-dispatched, not
		// quarantined. NB "memory access fault" is distinct from OOM ("out of
		// memory") above, so it lands here, not there.
		return taskErrWatchdog
	case isAutotuneSkip(line):
		return taskErrAutotune
	}
	return ""
}

// noteStderr classifies one hashcat output line and, on the first recognized
// agent-local fault, records the typed code + line so the exit handler can tag
// the failure. First match wins (the earliest fault is usually the root cause).
func (p *HashcatProcess) noteStderr(line string) {
	code := classifyHashcatStderr(line)
	if code == "" {
		return
	}
	if code == taskErrAutotune {
		// Separate, dedicated signal: the exit-code handler consults this to
		// avoid fake-completing a run where the device was skipped, and the
		// executor uses it to add --force to subsequent runs.
		p.AutotuneSkipped.Store(true)
	}
	if code == taskErrWatchdog {
		// A thermal/watchdog abort stops the device mid-run; hashcat can still
		// exit 1 ("exhausted") with progress[0] < progress[1]. The exit-code
		// handler consults this to fail-not-complete so the unsearched remainder
		// re-dispatches from the recovery point instead of counting as complete.
		p.DeviceAbort.Store(true)
	}
	p.mutex.Lock()
	if p.detectedErrorCode == "" {
		p.detectedErrorCode = code
		p.detectedErrorLine = strings.TrimSpace(line)
	}
	p.mutex.Unlock()
}

// taskFailureMessage builds the failure ErrorMessage for a non-completion exit.
// If the stderr scanner classified an agent-local fault, the message is prefixed
// with the typed code (AGENT_OOM, AGENT_NO_DEVICE, ...) so the backend routes it
// correctly without parsing hashcat's English; otherwise `generic` is used.
func (p *HashcatProcess) taskFailureMessage(exitCode int, generic string) string {
	p.mutex.Lock()
	code, line := p.detectedErrorCode, p.detectedErrorLine
	p.mutex.Unlock()
	if code == "" {
		return generic
	}
	if line != "" {
		return fmt.Sprintf("%s: %s (hashcat exit %d)", code, line, exitCode)
	}
	return fmt.Sprintf("%s (hashcat exit %d)", code, exitCode)
}

// keyspaceExhausted reports whether the last captured hashcat status confirms the
// task processed its ENTIRE assigned keyspace — progress[0] reached progress[1].
// A genuine "exhausted" exit has them equal; a device that aborted mid-run
// (thermal/watchdog) leaves progress[0] < progress[1]. Returns false when no
// status carrying the total was ever captured (nothing is confirmed). Read
// without a lock: the exit-code handler runs only after the output-reader
// goroutines have joined, so LastProgress is stable by then (same as the other
// LastProgress reads in that handler).
func (p *HashcatProcess) keyspaceExhausted() bool {
	lp := p.LastProgress
	if lp == nil || lp.TotalEffectiveKeyspace == nil {
		return false
	}
	return lp.EffectiveProgress >= *lp.TotalEffectiveKeyspace
}

// AttackMode represents hashcat attack modes
type AttackMode int

const (
	AttackModeStraight           AttackMode = 0 // Dictionary attack
	AttackModeCombination        AttackMode = 1 // Combination attack
	AttackModeBruteForce         AttackMode = 3 // Brute-force attack
	AttackModeHybridWordlistMask AttackMode = 6 // Hybrid Wordlist + Mask
	AttackModeHybridMaskWordlist AttackMode = 7 // Hybrid Mask + Wordlist
	AttackModeAssociation        AttackMode = 9 // Association attack (1:1 hash:wordlist mapping)

	// PID file for tracking hashcat processes
	hashcatPIDFile = "/tmp/krakenhashes-hashcat.pid"

	// Retry configuration for "already running" errors
	MaxHashcatRetries = 5
	HashcatRetryDelay = 5 * time.Second
)

// CharsetFileInfo describes a charset file referenced by a task assignment
type CharsetFileInfo struct {
	Name      string `json:"name"`
	MD5Hash   string `json:"md5_hash"`
	Size      int64  `json:"size"`
	ByteCount int    `json:"byte_count"`
}

// JobTaskAssignment represents a task assignment from the backend
type JobTaskAssignment struct {
	TaskID            string                     `json:"task_id"`
	JobExecutionID    string                     `json:"job_execution_id"`
	HashlistID        int64                      `json:"hashlist_id"`
	HashlistPath      string                     `json:"hashlist_path"` // Local path on agent
	AttackMode        int                        `json:"attack_mode"`
	HashType          int                        `json:"hash_type"`
	KeyspaceStart     int64                      `json:"keyspace_start"`
	KeyspaceEnd       int64                      `json:"keyspace_end"`
	WordlistPaths     []string                   `json:"wordlist_paths"`                // Local paths on agent
	RulePaths         []string                   `json:"rule_paths"`                    // Local paths on agent
	Mask              string                     `json:"mask,omitempty"`                // For mask attacks
	CustomCharsets    map[string]string          `json:"custom_charsets,omitempty"`     // Custom charsets: {"1": "?u?d", "3": "?s"}
	CharsetFiles      map[string]CharsetFileInfo `json:"charset_files,omitempty"`       // File-based charsets: {"1": {name: "file.hcchr", ...}}
	HexCharset        bool                       `json:"hex_charset,omitempty"`         // When true, auto-inject --hex-charset flag
	BinaryPath        string                     `json:"binary_path"`                   // Hashcat binary to use
	ChunkDuration     int                        `json:"chunk_duration"`                // Expected duration in seconds
	ReportInterval    int                        `json:"report_interval"`               // Progress reporting interval
	OutputFormat      string                     `json:"output_format"`                 // Hashcat output format
	ExtraParameters   string                     `json:"extra_parameters,omitempty"`    // Agent-specific hashcat parameters
	JobAdditionalArgs string                     `json:"job_additional_args,omitempty"` // Job-level hashcat parameters (merged with agent params)
	EnabledDevices    []int                      `json:"enabled_devices,omitempty"`     // List of enabled device IDs
	IncrementMode     string                     `json:"increment_mode,omitempty"`      // Mask increment mode: off, increment, increment_inverse
	IncrementMin      *int                       `json:"increment_min,omitempty"`       // Starting mask length for increment mode
	IncrementMax      *int                       `json:"increment_max,omitempty"`       // Maximum mask length for increment mode
	IsKeyspaceSplit   bool                       `json:"is_keyspace_split"`             // Whether this task uses keyspace splitting (--skip/--limit)
	Slow              bool                       `json:"slow,omitempty"`                // Hash type is slow (iterated) — add hashcat -S for wordlist attacks so host-side candidate generation keeps the GPU saturated under small --limit chunks
	BaseKeyspace      int64                      `json:"base_keyspace,omitempty"`       // Server's base keyspace for --skip/--limit coordinate conversion
	// Effective-keyspace range (base × rule/salt multipliers) for this
	// task. The real hashcat executor reads effective progress from
	// hashcat's progress[0]/[1] and ignores these. They exist so the
	// mock executor (and any future non-hashcat executor) can report
	// EffectiveProgress / TotalEffectiveKeyspace without re-deriving the
	// multiplier the scheduler already computed. Zero when the backend
	// couldn't compute them (overflow / unset).
	EffectiveKeyspaceStart int64 `json:"effective_keyspace_start,omitempty"`
	EffectiveKeyspaceEnd   int64 `json:"effective_keyspace_end,omitempty"`
	// Association attack fields (mode 9)
	AssociationWordlistPath string `json:"association_wordlist_path,omitempty"` // Path to the association wordlist
	OriginalHashlistPath    string `json:"original_hashlist_path,omitempty"`    // Path to the original hashlist file (preserves order)

	// Client-specific wordlists (potfile and uploaded wordlists)
	ClientID            string   `json:"client_id,omitempty"`             // Client UUID for this hashlist
	ClientPotfilePath   string   `json:"client_potfile_path,omitempty"`   // Path to client potfile (treated as wordlist)
	ClientWordlistPaths []string `json:"client_wordlist_paths,omitempty"` // Paths to client-specific wordlists
	ClientWordlistIDs   []string `json:"client_wordlist_ids,omitempty"`   // IDs for downloading client wordlists

	// Expected on-server MD5s for the files this task references, keyed by the
	// exact wire path above (e.g. "rules/hashcat/best64.rule"). ensureRules /
	// ensureWordlists verify each referenced file against these and re-download
	// any that are missing or stale, so a running agent picks up files changed
	// on the server after it connected (GH #61). A path absent from the map
	// falls back to a plain existence check. BinaryMD5 is informational only —
	// binaries are immutable by version id.
	WordlistMD5s map[string]string `json:"wordlist_md5s,omitempty"`
	RuleMD5s     map[string]string `json:"rule_md5s,omitempty"`
	BinaryMD5    string            `json:"binary_md5,omitempty"`
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
	KeyspaceProcessed      int64          `json:"keyspace_processed"`                 // Restore point (position in wordlist)
	EffectiveProgress      int64          `json:"effective_progress"`                 // Actual effective progress (words × rules processed)
	ProgressPercent        float64        `json:"progress_percent"`                   // Actual progress percentage (0-100)
	TotalEffectiveKeyspace *int64         `json:"total_effective_keyspace,omitempty"` // Only sent on first update - hashcat progress[1]
	IsFirstUpdate          bool           `json:"is_first_update"`                    // Flag indicating this is the first progress update
	HashRate               int64          `json:"hash_rate"`                          // Current hashes per second
	Temperature            *float64       `json:"temperature"`                        // GPU temperature (deprecated, use DeviceMetrics)
	Utilization            *float64       `json:"utilization"`                        // GPU utilization percentage (deprecated, use DeviceMetrics)
	TimeRemaining          *int           `json:"time_remaining"`                     // Estimated seconds remaining
	CrackedCount           int            `json:"cracked_count"`                      // Number of hashes cracked in this update
	CrackedHashes          []CrackedHash  `json:"cracked_hashes"`                     // Detailed crack information
	Status                 string         `json:"status,omitempty"`                   // Task status (running, completed, failed)
	ErrorMessage           string         `json:"error_message,omitempty"`            // Error message if status is failed
	DeviceMetrics          []DeviceMetric `json:"device_metrics,omitempty"`           // Per-device metrics
	AllHashesCracked       bool           `json:"all_hashes_cracked,omitempty"`       // Flag indicating all hashes in hashlist were cracked (exit code 6)
}

// JobStatus represents status-only message (synchronous, no crack data)
type JobStatus struct {
	TaskID                 string         `json:"task_id"`
	KeyspaceProcessed      int64          `json:"keyspace_processed"`
	EffectiveProgress      int64          `json:"effective_progress"`
	ProgressPercent        float64        `json:"progress_percent"`
	TotalEffectiveKeyspace *int64         `json:"total_effective_keyspace,omitempty"`
	IsFirstUpdate          bool           `json:"is_first_update"`
	HashRate               int64          `json:"hash_rate"`
	TimeRemaining          *int           `json:"time_remaining,omitempty"`
	CrackedCount           int            `json:"cracked_count"` // Total count only, not actual hashes
	Status                 string         `json:"status,omitempty"`
	ErrorMessage           string         `json:"error_message,omitempty"`
	DeviceMetrics          []DeviceMetric `json:"device_metrics,omitempty"`
	AllHashesCracked       bool           `json:"all_hashes_cracked,omitempty"`
}

// CrackBatch represents crack-only message (asynchronous)
type CrackBatch struct {
	TaskID        string        `json:"task_id"`
	CrackedHashes []CrackedHash `json:"cracked_hashes"`
	IsRetransmit  bool          `json:"is_retransmit,omitempty"` // Marks this as a retransmission for deduplication
}

// CrackBatchesComplete signals that all crack batches have been sent
type CrackBatchesComplete struct {
	TaskID       string `json:"task_id"`
	IsRetransmit bool   `json:"is_retransmit,omitempty"` // True if this is from a retransmission
}

// CrackedHash represents a cracked hash with all available information
type CrackedHash struct {
	Hash     string `json:"hash"`      // The original hash
	Salt     string `json:"salt"`      // Salt (if applicable)
	Plain    string `json:"plain"`     // Plain text password
	HexPlain string `json:"hex_plain"` // Hex representation of plain
	CrackPos string `json:"crack_pos"` // Position in keyspace where found
	FullLine string `json:"full_line"` // Full output line for reference
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
	outputCallback func(taskID string, output string, isError bool)

	// Orphan callback fires when hashcat refuses to start because another
	// (foreign-to-us) hashcat process is already holding the session lock.
	// The executor has already SIGKILLed the orphan PID by the time the
	// callback runs; the callback's job is to notify the backend so it can
	// audit / reconcile. attemptedTaskID is the task whose start was blocked
	// (may be empty for benchmark runs that don't have a real task ID).
	orphanCallback func(pid int, attemptedTaskID string, fromOurAgent bool)

	// Device flags callback - returns device flags for hashcat (-d flag)
	deviceFlagsCallback func() string

	// Agent's default extra parameters for hashcat
	agentExtraParams string

	// forceKernel is set true (sticky, agent-session-wide) the first time any
	// task is skipped by a kernel autotune failure. Once set, every subsequent
	// hashcat run (tasks and speed tests, which share the command builder) is
	// launched with --force to bypass autotune, so tiny jobs stop re-failing.
	// --force also suppresses hashcat's self-test, so we only enable it AFTER an
	// observed autotune failure — never speculatively.
	forceKernel atomic.Bool

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
	IsRunning      bool
	StartTime      time.Time
	LastProgress   *JobProgress
	LastCheckpoint time.Time

	// Hashlist tracking for crack parsing
	HashlistContent []string          // Store the hashes we're cracking (kept for special hash types)
	HashlistMap     map[string]string // Key: lowercase hash, Value: original hash (for O(1) lookups)

	// Outfile tracking for reliable crack capture
	OutfilePath       string          // Path to hashcat --outfile
	OutfileSentHashes map[string]bool // Track sent hash:password lines (deduplication)
	OutfileMutex      sync.Mutex      // Protect outfile state
	OutfileOffset     int64           // Current read position in outfile

	// Keyspace coordinate conversion
	KeyspaceRatio float64 // Ratio for coordinate conversion (agent_base / server_base), 0 = no conversion

	// Step 11s: chunk-local progress baseline.
	// Hashcat's progress[0] is reported in absolute effective coords
	// — for a chunk with --skip > 0, the first reading is already at
	// the skip-equivalent baseline, not 0. Capture that baseline on
	// the first non-zero status report so the terminal progress bar
	// can display chunk-local % (0 → 100 over THIS chunk's lifetime)
	// instead of the absolute ratio (which starts at e.g. 51% baseline).
	InitialEffectiveProgress int64
	InitialProgressCaptured  bool

	// Error tracking
	AlreadyRunningError     bool
	HashcatRejectedHashlist atomic.Bool  // set when stderr/stdout indicates "No hashes loaded" / "Hash parsing error" / etc. → fast-fail the job
	AutotuneSkipped         atomic.Bool  // set when hashcat skipped the device on kernel autotune failure (session aborted, nothing ran) → fail-not-complete + retry with --force
	DeviceAbort             atomic.Bool  // set when a device aborted mid-run (thermal/watchdog). hashcat may then exit 1 ("exhausted") with progress[0] < progress[1]; treat as a failure (not completion) so the unsearched remainder re-dispatches from the recovery point
	cracksReported          atomic.Int64 // cumulative cracks captured for this task; a nonzero count proves the run did real work even if no final status JSON was captured
	// detectedErrorCode/Line capture the first recognized agent-local fault
	// (OOM, no-device, driver, disk, watchdog) seen in hashcat output, so the
	// exit-code handler can tag the failure with a typed code for the backend.
	// Guarded by mutex.
	detectedErrorCode string
	detectedErrorLine string
	mutex             sync.Mutex

	// Cleanup coordination
	CleanupInProgress atomic.Bool // Flag to prevent timer creation during cleanup
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

// SetOrphanCallback sets the callback fired when an orphaned hashcat PID is
// detected and SIGKILLed by the stderr "Already an instance" handler.
func (e *HashcatExecutor) SetOrphanCallback(callback func(pid int, attemptedTaskID string, fromOurAgent bool)) {
	e.orphanCallback = callback
}

// lookupActiveTaskByPID scans activeProcesses for a process whose hashcat PID
// matches the supplied pid. Returns the owning task ID and true on a hit,
// "" and false otherwise. Caller must not be holding e.mutex.
func (e *HashcatExecutor) lookupActiveTaskByPID(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	for taskID, p := range e.activeProcesses {
		if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
			continue
		}
		if p.Cmd.Process.Pid == pid {
			return taskID, true
		}
	}
	return "", false
}

// reconcileOrphanHashcat is invoked when hashcat refuses to start because
// another instance owns the session lock. It distinguishes a self-collision
// (the conflicting PID is one of ours, so the existing retry path will sort
// itself out as the running task finishes) from a true orphan (the PID is
// NOT in our activeProcesses map, e.g. left over from a crashed previous
// agent run, a leaked speed test, or a stale lock with a recycled PID). In
// the orphan case the executor SIGKILLs the PID directly and notifies the
// backend so it can mark any task that was running on this agent as
// unrecoverable.
func (e *HashcatExecutor) reconcileOrphanHashcat(pid int, attemptedTaskID string) {
	if pid <= 0 {
		return
	}

	if owningTask, ours := e.lookupActiveTaskByPID(pid); ours {
		debug.Warning("Hashcat 'Already an instance' on PID %d collided with our own task %s; not killing. The retry path will pick up after that task finishes.", pid, owningTask)
		if e.orphanCallback != nil {
			e.orphanCallback(pid, attemptedTaskID, true)
		}
		return
	}

	// Foreign PID: best-effort kill. We can't re-attach to its stdout to
	// recover progress, so the only useful action is to free the session
	// lock and let our retry succeed. os.FindProcess + Process.Kill is
	// portable: SIGKILL on Linux/macOS, TerminateProcess on Windows. Either
	// way an already-exited PID just produces a benign error which we log.
	proc, ferr := os.FindProcess(pid)
	if ferr != nil {
		debug.Warning("os.FindProcess(%d) failed (orphan may have already exited): %v", pid, ferr)
	} else if kerr := proc.Kill(); kerr != nil {
		debug.Warning("Failed to kill orphaned hashcat PID %d: %v (it may have already exited)", pid, kerr)
	} else {
		debug.Info("Killed orphaned hashcat PID %d (was blocking task %s)", pid, attemptedTaskID)
	}

	if e.orphanCallback != nil {
		e.orphanCallback(pid, attemptedTaskID, false)
	}
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
	if err := os.MkdirAll(outfileDir, 0700); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create outfile directory: %w", err)
	}

	// Build hashcat command
	command, statusFile, potFile, outputFile, keyspaceRatio, err := e.buildHashcatCommand(assignment)
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

	// Load the hashlist content and map for crack parsing
	hashlistPath := filepath.Join(e.dataDirectory, assignment.HashlistPath)
	hashlistContent, hashlistMap, err := e.loadHashlist(hashlistPath)
	if err != nil {
		debug.Warning("Failed to load hashlist for crack parsing: %v", err)
		// Continue anyway - we'll fall back to old parsing if needed
		hashlistContent = []string{}
		hashlistMap = make(map[string]string)
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
		KeyspaceRatio:     keyspaceRatio, // For reverse conversion of restore_point
		HashlistContent:   hashlistContent,
		HashlistMap:       hashlistMap,           // O(1) lookup map for crack parsing
		OutfilePath:       outputFile,            // Set outfile path for monitoring
		OutfileSentHashes: make(map[string]bool), // Initialize deduplication map
		OutfileOffset:     0,                     // Start reading from beginning
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

// wantsSlowCandidates reports whether hashcat -S (--slow-candidates) should be added for
// this assignment: a slow (iterated) hash type running a wordlist attack that carries an
// amplifier — rules on -a 0, or a hybrid mask on -a 6/-a 7. -S moves candidate generation
// to the host so the GPU stays saturated when a keyspace-split --limit makes the base
// wordlist small. It is deliberately NOT applied to fast hashes (host generation would
// starve the GPU) or to modes without an amplifier (nothing to relocate).
func (a *JobTaskAssignment) wantsSlowCandidates() bool {
	if !a.Slow {
		return false
	}
	switch a.AttackMode {
	case int(AttackModeStraight):
		return len(a.RulePaths) > 0
	case int(AttackModeHybridWordlistMask), int(AttackModeHybridMaskWordlist):
		return true
	default:
		return false
	}
}

// hasSlowCandidatesFlag reports whether -S / --slow-candidates is already present in args
// (e.g. supplied via the job's or agent's extra parameters), so we don't add it twice.
func hasSlowCandidatesFlag(args []string) bool {
	for _, a := range args {
		if a == "-S" || a == "--slow-candidates" {
			return true
		}
	}
	return false
}

// buildHashcatCommand builds the hashcat command line arguments
func (e *HashcatExecutor) buildHashcatCommand(assignment *JobTaskAssignment) (*exec.Cmd, string, string, string, float64, error) {
	return e.buildHashcatCommandWithOptions(assignment, false)
}

// buildHashcatCommandWithOptions builds the hashcat command line arguments with options
// Returns: cmd, statusFile, potFile, outputFile, keyspaceRatio, error
// keyspaceRatio is the agent_base/server_base ratio (>1 when -O changes kernel split), 0 if no conversion
func (e *HashcatExecutor) buildHashcatCommandWithOptions(assignment *JobTaskAssignment, isBenchmark bool) (*exec.Cmd, string, string, string, float64, error) {
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
		"-m", strconv.Itoa(assignment.HashType), // Hash type
		"-a", strconv.Itoa(int(assignment.AttackMode)), // Attack mode
		"--status",                                                // Enable status output
		"--status-json",                                           // Output status in JSON format
		"--status-timer", strconv.Itoa(assignment.ReportInterval), // Status update interval
		"--quiet",           // Reduce verbose output
		"--potfile-disable", // Disable potfile
		"--restore-disable", // Disable restore files (we handle restore via keyspace)
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

	// Merge job-level args with agent-level args (agent wins on conflicts)
	agentArgs := assignment.ExtraParameters
	if agentArgs == "" {
		agentArgs = e.agentExtraParams
	}
	mergedArgs := MergeHashcatArgs(assignment.JobAdditionalArgs, agentArgs)
	if mergedArgs != "" {
		debug.Info("Adding merged extra parameters: %s (job: %s, agent: %s)", mergedArgs, assignment.JobAdditionalArgs, agentArgs)
		args = append(args, strings.Fields(mergedArgs)...)
	}

	// Slow-hash candidate mode: add hashcat -S (--slow-candidates) so host-side candidate
	// generation keeps the GPU saturated for a slow hash whose keyspace-split --limit
	// makes the per-chunk base wordlist small (otherwise the fast-candidate GPU path
	// starves on a slow hash + few base words — observed: phpass + a 610k-rule file
	// collapsing a 5090 from ~16 MH/s to ~131 KH/s as chunks shrank). Applied to
	// benchmarks too so the measured speed matches the run. See wantsSlowCandidates for
	// the (slow AND amplified) gate. MUST be mirrored in getAgentKeyspace so the
	// --keyspace probe matches the run and the --skip/--limit coordinate conversion stays
	// correct (-S can change the keyspace unit hashcat reports).
	if assignment.wantsSlowCandidates() && !hasSlowCandidatesFlag(args) {
		args = append(args, "-S")
		debug.Info("Adding -S (--slow-candidates): slow hash type %d, attack mode %d — keeps the GPU fed under small --limit chunks", assignment.HashType, assignment.AttackMode)
	}

	// Surgical autotune remediation: once any task on this agent has been
	// skipped by a kernel autotune failure, carry --force on every subsequent
	// hashcat run so tiny jobs stop autotune-aborting. Only added after such a
	// failure is observed (forceKernel is set in the exit handler), never
	// speculatively, because --force also disables hashcat's correctness
	// self-test. Skip if the user already supplied --force to avoid a duplicate.
	if e.forceKernel.Load() {
		hasForce := false
		for _, a := range args {
			if a == "--force" {
				hasForce = true
				break
			}
		}
		if !hasForce {
			args = append(args, "--force")
			debug.Info("Adding --force (agent previously detected a kernel autotune skip)")
		}
	}

	// Add increment flags for mask-based attacks
	if assignment.IncrementMode == "increment" || assignment.IncrementMode == "increment_inverse" {
		if assignment.IncrementMode == "increment" {
			args = append(args, "--increment")
			debug.Info("Adding --increment flag for left-to-right mask increment")
		} else if assignment.IncrementMode == "increment_inverse" {
			args = append(args, "--increment-inverse")
			debug.Info("Adding --increment-inverse flag for right-to-left mask increment")
		}

		if assignment.IncrementMin != nil {
			args = append(args, "--increment-min", strconv.Itoa(*assignment.IncrementMin))
			debug.Info("Adding --increment-min %d", *assignment.IncrementMin)
		}

		if assignment.IncrementMax != nil {
			args = append(args, "--increment-max", strconv.Itoa(*assignment.IncrementMax))
			debug.Info("Adding --increment-max %d", *assignment.IncrementMax)
		}
	}

	// Only add --outfile for actual job execution, not benchmarks
	// Note: --remove flag removed as it's not needed for distributed cracking
	// The backend tracks which hashes are cracked via the database
	if !isBenchmark {
		// Add outfile for reliable crack capture
		outfileDir := filepath.Join(e.dataDirectory, "outfile")
		if err := os.MkdirAll(outfileDir, 0755); err != nil {
			return nil, "", "", "", 0, fmt.Errorf("failed to create outfile directory: %w", err)
		}

		outfilePath := filepath.Join(outfileDir, fmt.Sprintf("%s.txt", assignment.TaskID))
		args = append(args, "--outfile", outfilePath)
		args = append(args, "--outfile-format", "1,2") // Format 1,2: hash:plain (colon-separated)
		outputFile = outfilePath                       // Store for return value
		debug.Info("Outfile configured: %s", outfilePath)
	}

	// Only use --skip/--limit when explicitly doing keyspace splitting
	// Do NOT use for rule chunking or increment mode
	var keyspaceRatio float64
	if assignment.IsKeyspaceSplit {
		skip := assignment.KeyspaceStart
		limit := assignment.KeyspaceEnd - assignment.KeyspaceStart

		// Agent-side coordinate conversion: if the server's base_keyspace differs
		// from this agent's outer-loop keyspace (e.g., due to -O flag changing the
		// kernel split for mask attacks), convert --skip/--limit to the agent's space
		if assignment.BaseKeyspace > 0 {
			agentBase, err := e.getAgentKeyspace(assignment)
			if err != nil {
				debug.Warning("Failed to get agent keyspace for coordinate conversion, using server coordinates: %v", err)
			} else if agentBase > 0 && agentBase != assignment.BaseKeyspace {
				keyspaceRatio = float64(agentBase) / float64(assignment.BaseKeyspace)
				skip = int64(float64(skip) * keyspaceRatio)
				limit = int64(math.Ceil(float64(limit) * keyspaceRatio))
				debug.Info("Keyspace coordinate conversion: ratio=%.2f, server_base=%d, agent_base=%d, skip=%d, limit=%d",
					keyspaceRatio, assignment.BaseKeyspace, agentBase, skip, limit)
			}
		}

		if skip > 0 {
			args = append(args, "--skip", strconv.FormatInt(skip, 10))
		}
		if limit > 0 {
			args = append(args, "--limit", strconv.FormatInt(limit, 10))
		}
	}

	// Add hashlist file
	// Backend sends the correct hashlist path for all modes:
	// - Mode 9 (association): original hashlist with preserved order
	// - Other modes: processed (deduplicated) hashlist
	hashlistPath := filepath.Join(e.dataDirectory, assignment.HashlistPath)

	// Debug: Check if hashlist file exists
	if _, err := os.Stat(hashlistPath); os.IsNotExist(err) {
		debug.Error("Hashlist file does not exist: %s", hashlistPath)
		return nil, "", "", "", 0, fmt.Errorf("hashlist file not found: %s", hashlistPath)
	}

	args = append(args, hashlistPath)

	// Auto-inject --hex-charset ONLY when the job has hex mode AND inline charset definitions
	// (file charsets are unaffected by --hex-charset, and without any -1/-2/-3/-4 inline defs
	// hashcat will reject --hex-charset as it tries to interpret the mask as hex)
	if assignment.HexCharset {
		hasInlineCharset := false
		for _, slot := range []string{"1", "2", "3", "4"} {
			if _, isFile := assignment.CharsetFiles[slot]; isFile {
				continue
			}
			if def, ok := assignment.CustomCharsets[slot]; ok && def != "" {
				hasInlineCharset = true
				break
			}
		}
		if hasInlineCharset {
			args = append(args, "--hex-charset")
			debug.Info("Adding --hex-charset flag (job has hex-encoded inline charsets)")
		}
	}

	// Add custom charset flags (-1 through -4) before attack-specific args
	for _, slot := range []string{"1", "2", "3", "4"} {
		if cf, ok := assignment.CharsetFiles[slot]; ok && cf.Name != "" {
			// File charset — use local file path
			charsetPath := filepath.Join(e.dataDirectory, "charsets", cf.Name)
			args = append(args, "-"+slot, charsetPath)
		} else if def, ok := assignment.CustomCharsets[slot]; ok && def != "" {
			args = append(args, "-"+slot, def)
		}
	}

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

		// Add client potfile as a wordlist if specified
		// Client potfile is a wordlist of previously cracked passwords for this client
		if assignment.ClientPotfilePath != "" {
			fullPath := filepath.Join(e.dataDirectory, assignment.ClientPotfilePath)
			if _, err := os.Stat(fullPath); err == nil {
				debug.Info("Adding client potfile as wordlist: %s", fullPath)
				args = append(args, fullPath)
			} else {
				debug.Warning("Client potfile not found, skipping: %s", fullPath)
			}
		}

		// Add client-specific wordlists if specified
		if len(assignment.ClientWordlistPaths) > 0 {
			debug.Info("Adding client wordlists to hashcat command: %v", assignment.ClientWordlistPaths)
			for _, wordlistPath := range assignment.ClientWordlistPaths {
				fullPath := filepath.Join(e.dataDirectory, wordlistPath)
				if _, err := os.Stat(fullPath); err == nil {
					debug.Info("Adding client wordlist: %s", fullPath)
					args = append(args, fullPath)
				} else {
					debug.Warning("Client wordlist not found, skipping: %s", fullPath)
				}
			}
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

	case int(AttackModeAssociation): // Association attack (mode 9)
		// Association attack requires:
		// - Original hashlist file (backend sends correct path in HashlistPath)
		// - Association wordlist (backend sends it in WordlistPaths[0])
		// - Optional rules
		if len(assignment.WordlistPaths) == 0 {
			return nil, "", "", "", 0, fmt.Errorf("association attack requires wordlist")
		}

		// Use first wordlist as the association wordlist
		assocWordlistPath := filepath.Join(e.dataDirectory, assignment.WordlistPaths[0])
		debug.Info("Adding association wordlist: %s", assocWordlistPath)

		// Verify association wordlist exists
		if _, err := os.Stat(assocWordlistPath); os.IsNotExist(err) {
			return nil, "", "", "", 0, fmt.Errorf("association wordlist not found: %s", assocWordlistPath)
		}

		args = append(args, assocWordlistPath)

		// Add rules if specified
		for _, rulePath := range assignment.RulePaths {
			fullPath := filepath.Join(e.dataDirectory, rulePath)
			debug.Info("Adding rule for association attack: %s", fullPath)
			args = append(args, "-r", fullPath)
		}

	default:
		return nil, "", "", "", 0, fmt.Errorf("unsupported attack mode: %d", assignment.AttackMode)
	}

	// Resolve the hashcat binary path
	hashcatBinary, err := e.resolveHashcatBinary(assignment.BinaryPath)
	if err != nil {
		return nil, "", "", "", 0, fmt.Errorf("failed to resolve hashcat binary: %w", err)
	}

	debug.Info("Using hashcat binary: %s", hashcatBinary)
	debug.Info("Full hashcat command: %s %s", hashcatBinary, strings.Join(args, " "))

	// Create command
	cmd := exec.Command(hashcatBinary, args...)

	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(hashcatBinary)
	debug.Info("Setting working directory to: %s", cmd.Dir)

	return cmd, statusFile, potFile, outputFile, keyspaceRatio, nil
}

// getAgentKeyspace runs "hashcat --keyspace" with the agent's own flags to determine
// this agent's outer-loop keyspace. This may differ from the server's base_keyspace
// when the agent uses -O (optimized kernels), which changes the kernel split for mask attacks.
func (e *HashcatExecutor) getAgentKeyspace(assignment *JobTaskAssignment) (int64, error) {
	// Resolve hashcat binary
	hashcatBinary, err := e.resolveHashcatBinary(assignment.BinaryPath)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve hashcat binary: %w", err)
	}

	// Build minimal command for --keyspace
	args := []string{
		"-m", strconv.Itoa(assignment.HashType),
		"-a", strconv.Itoa(assignment.AttackMode),
		"--keyspace",
		"--quiet",
		"--restore-disable",
	}

	// Merge job-level args with agent-level args (may include -O which affects the kernel split)
	agentArgs := assignment.ExtraParameters
	if agentArgs == "" {
		agentArgs = e.agentExtraParams
	}
	mergedArgs := MergeHashcatArgs(assignment.JobAdditionalArgs, agentArgs)
	if mergedArgs != "" {
		args = append(args, strings.Fields(mergedArgs)...)
	}

	// Mirror the run's -S (--slow-candidates): the --keyspace probe must measure the SAME
	// outer-loop keyspace hashcat will use for --skip/--limit, because -S can change that
	// unit (base words vs base×amplifier). If the probe and the run disagree, the
	// skip/limit coordinate conversion in buildHashcatCommandWithOptions uses the wrong
	// ratio. (This is the same reason -O is already reflected here via the merged args.)
	if assignment.wantsSlowCandidates() && !hasSlowCandidatesFlag(args) {
		args = append(args, "-S")
	}

	// Auto-inject --hex-charset ONLY when the job has hex mode AND inline charset definitions
	if assignment.HexCharset {
		hasInlineCharset := false
		for _, slot := range []string{"1", "2", "3", "4"} {
			if _, isFile := assignment.CharsetFiles[slot]; isFile {
				continue
			}
			if def, ok := assignment.CustomCharsets[slot]; ok && def != "" {
				hasInlineCharset = true
				break
			}
		}
		if hasInlineCharset {
			args = append(args, "--hex-charset")
		}
	}

	// Add custom charset flags (-1 through -4) before attack-specific args
	for _, slot := range []string{"1", "2", "3", "4"} {
		if cf, ok := assignment.CharsetFiles[slot]; ok && cf.Name != "" {
			// File charset — use local file path
			charsetPath := filepath.Join(e.dataDirectory, "charsets", cf.Name)
			args = append(args, "-"+slot, charsetPath)
		} else if def, ok := assignment.CustomCharsets[slot]; ok && def != "" {
			args = append(args, "-"+slot, def)
		}
	}

	// Add attack-mode specific arguments (hashcat needs these to compute keyspace)
	switch assignment.AttackMode {
	case int(AttackModeStraight): // Dictionary attack
		for _, wordlistPath := range assignment.WordlistPaths {
			args = append(args, filepath.Join(e.dataDirectory, wordlistPath))
		}
		for _, rulePath := range assignment.RulePaths {
			args = append(args, "-r", filepath.Join(e.dataDirectory, rulePath))
		}
	case int(AttackModeCombination):
		if len(assignment.WordlistPaths) >= 2 {
			args = append(args, filepath.Join(e.dataDirectory, assignment.WordlistPaths[0]))
			args = append(args, filepath.Join(e.dataDirectory, assignment.WordlistPaths[1]))
		}
	case int(AttackModeBruteForce): // Mask attack
		if assignment.Mask != "" {
			args = append(args, assignment.Mask)
		}
	case int(AttackModeHybridWordlistMask):
		if len(assignment.WordlistPaths) > 0 && assignment.Mask != "" {
			args = append(args, filepath.Join(e.dataDirectory, assignment.WordlistPaths[0]))
			args = append(args, assignment.Mask)
		}
	case int(AttackModeHybridMaskWordlist):
		if assignment.Mask != "" && len(assignment.WordlistPaths) > 0 {
			args = append(args, assignment.Mask)
			args = append(args, filepath.Join(e.dataDirectory, assignment.WordlistPaths[0]))
		}
	case int(AttackModeAssociation):
		if len(assignment.WordlistPaths) > 0 {
			args = append(args, filepath.Join(e.dataDirectory, assignment.WordlistPaths[0]))
		}
		for _, rulePath := range assignment.RulePaths {
			args = append(args, "-r", filepath.Join(e.dataDirectory, rulePath))
		}
	}

	debug.Info("Running hashcat --keyspace: %s %s", hashcatBinary, strings.Join(args, " "))

	cmd := exec.Command(hashcatBinary, args...)
	cmd.Dir = filepath.Dir(hashcatBinary)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("hashcat --keyspace failed: %w", err)
	}

	keyspaceStr := strings.TrimSpace(string(output))
	keyspace, err := strconv.ParseInt(keyspaceStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse keyspace output %q: %w", keyspaceStr, err)
	}

	debug.Info("Agent keyspace for hash_type=%d, attack_mode=%d: %d", assignment.HashType, assignment.AttackMode, keyspace)
	return keyspace, nil
}

// runHashcatProcess executes and monitors a hashcat process
func (e *HashcatExecutor) runHashcatProcess(ctx context.Context, process *HashcatProcess, stdoutPipe, stderrPipe io.ReadCloser) {
	defer func() {
		// Cleanup only - outfile reading and batch flushing is now done explicitly
		// BEFORE sending completion status (see the synchronization block after output goroutines)
		if process.OutfilePath != "" {
			// Set cleanup flag to prevent new timers during cleanup
			process.CleanupInProgress.Store(true)

			// Stop all existing timers (should already be stopped, but defensive)
			e.crackBatchMutex.Lock()
			if timer := e.crackBatchTimers[process.TaskID]; timer != nil {
				timer.Stop()
				delete(e.crackBatchTimers, process.TaskID)
			}
			e.crackBatchMutex.Unlock()

			debug.Info("Defer cleanup completed for task %s (outfile read already done before completion)", process.TaskID)
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

			// Autotune-skip can surface on stdout (the stderr reader also watches
			// for it). Setting this flag lets the exit handler avoid mis-reporting
			// a skipped run as completed and trips the --force retry.
			if isAutotuneSkip(line) {
				process.AutotuneSkipped.Store(true)
			}

			// Store original line for outputCallback
			originalLine := line

			// Pre-check: Is this a standalone crack line (not JSON, contains colon, not "Skipping")?
			// We need to detect this BEFORE calling outputCallback
			if strings.Contains(line, ":") && !strings.HasPrefix(line, "{") && !strings.Contains(line, "Skipping") && !strings.Contains(line, "\"status\"") {
				// Try to parse as crack (uses O(1) HashMap lookup)
				cracked := e.parseCrackedHash(line, process.HashlistContent, process.HashlistMap, process.Assignment.HashType)
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
					cracked := e.parseCrackedHash(crackPart, process.HashlistContent, process.HashlistMap, process.Assignment.HashType)
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
							// Convert restore_point from agent base coords back to server
							// base coords whenever the two differ. The previous guard
							// `ratio > 1.0` only handled the -O kernel-split case (server
							// space larger than agent's); it missed ratio < 1.0 which
							// happened on increment-mode jobs pre-Step-11h (server sent
							// the JOB's combined base ~245M while agent's hashcat reported
							// per-layer base ~81M → ratio 0.33). Conversion math
							// `hashcat_value / ratio` is correct in both directions.
							// Step 11h normalizes ratio to 1.0 for increment dispatches,
							// but this conversion stays robust against any future
							// ratio != 1.0 case.
							if process.KeyspaceRatio > 0 && process.KeyspaceRatio != 1.0 {
								keyspaceProcessed = int64(float64(keyspaceProcessed) / process.KeyspaceRatio)
							}
						}

						// Extract progress values for percentage calculation
						var currentProgress, totalProgress int64
						if current, ok := progressArr[0].(float64); ok {
							currentProgress = int64(current) // Current position (words * rules processed)
						}
						if total, ok := progressArr[1].(float64); ok {
							totalProgress = int64(total) // Total to process (total words * total rules) - this is progress[1]
						}

						// Step 11s: capture baseline on first non-zero progress reading.
						// For chunks with --skip > 0, hashcat's progress[0] starts at
						// the skip-equivalent position (not 0). The terminal progress
						// bar should display chunk-local % (0 → 100 over this chunk's
						// run), not the absolute hashcat ratio (which would start at
						// e.g. 51% baseline for a chunk dispatched at job midpoint).
						if !process.InitialProgressCaptured && currentProgress > 0 {
							process.InitialEffectiveProgress = currentProgress
							process.InitialProgressCaptured = true
						}

						// Calculate chunk-local progress percentage by subtracting
						// the baseline from both numerator and denominator.
						var progressPercent float64
						if totalProgress > process.InitialEffectiveProgress {
							progressPercent = (float64(currentProgress-process.InitialEffectiveProgress) /
								float64(totalProgress-process.InitialEffectiveProgress)) * 100
							if progressPercent < 0 {
								progressPercent = 0
							}
							if progressPercent > 100 {
								progressPercent = 100
							}
						} else if totalProgress > 0 {
							// Fallback when baseline wasn't captured (e.g., first status
							// arrived before any work was done with currentProgress=0).
							progressPercent = (float64(currentProgress) / float64(totalProgress)) * 100
						}

						// Determine if this is the first progress update
						isFirstUpdate := process.LastProgress == nil || process.LastProgress.EffectiveProgress == 0

						// Always extract recovered hashes count for backend tracking
						var crackedCount int
						if recoveredHashes, ok := status["recovered_hashes"].([]interface{}); ok && len(recoveredHashes) >= 2 {
							if recovered, ok := recoveredHashes[0].(float64); ok {
								crackedCount = int(recovered)
								debug.Info("[Hashcat] Extracted recovered_hashes: %d out of %d total (allHashesCracked=%v)",
									int(recovered), int(recoveredHashes[1].(float64)), allHashesCracked)
							}
						}

						progress := &JobProgress{
							TaskID:            process.TaskID,
							KeyspaceProcessed: keyspaceProcessed, // Restore point (word position)
							EffectiveProgress: currentProgress,   // Actual effective progress
							ProgressPercent:   progressPercent,   // Actual progress percentage
							IsFirstUpdate:     isFirstUpdate,     // Flag indicating first update
							AllHashesCracked:  allHashesCracked,  // Flag when status code 6 detected
							CrackedCount:      crackedCount,      // Number of hashes cracked (from recovered_hashes)
						}

						// Always include total effective keyspace from hashcat
						if totalProgress > 0 {
							progress.TotalEffectiveKeyspace = &totalProgress // Hashcat's progress[1]
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

						// Streaming status updates are always sent as "running".
						// Even when hashcat reports status code 6 (all hashes
						// cracked), the task is NOT completed until the hashcat
						// process actually exits — the AllHashesCracked flag is
						// still propagated so the backend can trigger
						// hashlist-completion handling and 100% display, but the
						// real "completed" status is emitted from the process-exit
						// handler below.
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

			// Detect "no usable hashes" patterns so the backend can
			// fast-fail the job instead of cycling through the per-tuple
			// retry cap. Same patterns the benchmark path watches. When
			// any of these fire, the task's exit-code handler tags the
			// error message with BENCHMARK_NO_HASHES_LOADED, which the
			// backend's AttributeBenchmarkFailure recognizes as the
			// fast-fail sentinel.
			if strings.Contains(line, "No hashes loaded") ||
				strings.Contains(line, "Hash parsing error") ||
				strings.Contains(line, "Token length exception") ||
				strings.Contains(line, "Separator unmatched") {
				process.HashcatRejectedHashlist.Store(true)
			}

			// Classify agent-local runtime faults (OOM, no-device, driver,
			// disk-full, watchdog) so the exit handler can tag the failure with
			// a typed code the backend routes correctly. First match wins.
			process.noteStderr(line)

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

		// If we detected the "already running" error, store it and try to
		// reconcile. reconcileOrphanHashcat distinguishes a self-collision
		// (legitimate race against one of our own running tasks) from an
		// orphan (foreign PID; SIGKILL it so the retry path can take over).
		if alreadyRunningDetected {
			process.mutex.Lock()
			process.AlreadyRunningError = true
			process.mutex.Unlock()

			if alreadyRunningPID != "" {
				debug.Info("Hashcat process %s blocked by existing instance with PID %s", process.TaskID, alreadyRunningPID)
				if pid, parseErr := strconv.Atoi(alreadyRunningPID); parseErr == nil {
					e.reconcileOrphanHashcat(pid, process.TaskID)
				} else {
					debug.Warning("Could not parse 'Already an instance' PID %q: %v", alreadyRunningPID, parseErr)
				}
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

	// Start outfile monitoring goroutine with completion tracking
	var outfileDone chan struct{}
	var outfileCancel context.CancelFunc
	if process.OutfilePath != "" {
		var outfileCtx context.Context
		outfileCtx, outfileCancel = context.WithCancel(ctx)
		outfileDone = make(chan struct{})
		go func() {
			defer close(outfileDone)
			e.monitorOutfile(outfileCtx, process)
		}()
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
		// Context cancelled (operator Ctrl+C, StopJob, agent shutdown).
		// Kill hashcat and send a final status that PRESERVES the
		// last-known progress. The previous code sent an empty
		// JobProgress (all zeros), which the backend then wrote on
		// top of the real progress, displaying 0.00% / N/A speed for
		// a task that was actually 88% done.
		//
		// New behavior: status="stopped" (distinct from "completed"
		// or "failed") carries the last KeyspaceProcessed and
		// EffectiveProgress so the backend can truncate the
		// task/interval at that point and the remaining range gets
		// re-dispatched, with no loss of completed work.
		debug.Info("Context cancelled for task %s, killing process", process.TaskID)
		if process.Cmd.Process != nil {
			process.Cmd.Process.Kill()
		}
		stoppedProgress := &JobProgress{
			TaskID: process.TaskID,
		}
		if process.LastProgress != nil {
			stoppedProgress.KeyspaceProcessed = process.LastProgress.KeyspaceProcessed
			stoppedProgress.EffectiveProgress = process.LastProgress.EffectiveProgress
			stoppedProgress.ProgressPercent = process.LastProgress.ProgressPercent
			stoppedProgress.HashRate = process.LastProgress.HashRate
			stoppedProgress.TotalEffectiveKeyspace = process.LastProgress.TotalEffectiveKeyspace
			stoppedProgress.CrackedCount = process.LastProgress.CrackedCount
		}
		e.sendProgressUpdate(process, stoppedProgress, "stopped")

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

		// CRITICAL: Wait for outfile monitor to complete BEFORE sending any completion status
		// This ensures all cracks are captured and flushed before task transitions
		if outfileDone != nil {
			debug.Info("Stopping outfile monitor for task %s", process.TaskID)
			outfileCancel() // Signal the monitor to stop
			select {
			case <-outfileDone:
				debug.Info("Outfile monitor completed for task %s", process.TaskID)
			case <-time.After(30 * time.Second):
				debug.Warning("Timeout waiting for outfile monitor to complete for task %s (waited 30s)", process.TaskID)
			}
			// Flush any remaining crack batches BEFORE sending completion status
			debug.Info("Flushing remaining crack batches before completion for task %s", process.TaskID)
			e.flushCrackBatch(process)
		}

		// If any device was skipped by a kernel autotune failure this run, switch
		// the agent to --force for every subsequent run (and this task's retry).
		// Centralized here so it fires no matter which exit path we take —
		// autotune can surface as exit 0 (handled by reportNoWorkIfIdle) or as an
		// abort exit code (2-5), and both must set the flag for the monitor's
		// --force retry to actually help.
		if process.AutotuneSkipped.Load() {
			if e.forceKernel.CompareAndSwap(false, true) {
				debug.Warning("Kernel autotune skip detected for task %s — enabling --force for all subsequent hashcat runs on this agent", process.TaskID)
			}
		}

		if err != nil {
			// Step 11t: detect signal-killed hashcat (SIGINT from Ctrl+C
			// propagating through the process group, SIGTERM/SIGKILL from
			// graceful kill) and report as "stopped" instead of "failed".
			// Without this, the user's Ctrl+C produces "Hashcat failed
			// with exit code -1" — the work hashcat actually did is lost
			// because the failed-status path doesn't truncate-and-complete.
			//
			// This check fires BEFORE the exit-code switch because Go's
			// exec.ExitError.ExitCode() returns -1 for signal kills, which
			// would otherwise fall into the "unknown error" branch below.
			// The cached LastProgress is valid; the backend's status="stopped"
			// handler (Step 10c-2) calls IngestProgressV2 + RecoverTaskByID
			// to truncate the interval at the last known progress.
			errStr := err.Error()
			if strings.Contains(errStr, "signal: interrupt") ||
				strings.Contains(errStr, "signal: terminated") ||
				strings.Contains(errStr, "signal: killed") {
				debug.Info("Hashcat process killed by signal (%s) for task %s — treating as stopped, preserving progress", errStr, process.TaskID)
				stoppedProgress := &JobProgress{TaskID: process.TaskID}
				if process.LastProgress != nil {
					stoppedProgress.KeyspaceProcessed = process.LastProgress.KeyspaceProcessed
					stoppedProgress.EffectiveProgress = process.LastProgress.EffectiveProgress
					stoppedProgress.ProgressPercent = process.LastProgress.ProgressPercent
					stoppedProgress.HashRate = process.LastProgress.HashRate
					stoppedProgress.TotalEffectiveKeyspace = process.LastProgress.TotalEffectiveKeyspace
					stoppedProgress.CrackedCount = process.LastProgress.CrackedCount
				}
				e.sendProgressUpdate(process, stoppedProgress, "stopped")
				// Skip the exit-code switch — signal-kill is fully handled here.
			} else if exitErr, ok := err.(*exec.ExitError); ok {
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
					// OK/cracked - normal completion. But a clean exit with no
					// status ever captured means no device processed candidates
					// (e.g. a kernel autotune abort that still exits 0). Fail
					// instead of fake-completing so the work isn't lost.
					if e.reportNoWorkIfIdle(process, exitCode) {
						break
					}
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
					// Exit 1 = "exhausted". But a device that aborted mid-run
					// (thermal/watchdog) also exits 1 while its restore_point /
					// progress[0] never reached progress[1]. Do NOT fake-complete
					// that — it would count unsearched keyspace as done and lose
					// work (and cracks). Fail it (transient) instead so the backend
					// closes out coverage up to the last recovery point and
					// re-dispatches the remainder from there. A genuine exhaustion
					// (progress[0] == progress[1]) still completes below.
					if process.DeviceAbort.Load() && !process.keyspaceExhausted() {
						debug.Warning("Task %s: hashcat exit 1 (exhausted) after a device abort but the keyspace was NOT confirmed complete — failing so the unsearched remainder re-dispatches from the recovery point", process.TaskID)
						e.sendErrorProgress(process, process.taskFailureMessage(exitCode, taskErrWatchdog+": device aborted before exhausting keyspace (thermal/watchdog)"))
						break
					}
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
						ProgressPercent:        100.0,                  // Keyspace exhausted = 100% complete
						TotalEffectiveKeyspace: totalEffectiveKeyspace, // Always include for backend adjustment
					}
					e.sendProgressUpdate(process, finalProgress, "completed")

				case 2, 3, 4, 5:
					// Various abort conditions. A detected agent-local fault
					// (e.g. OOM forcing an abort) takes precedence over the
					// generic "aborted" message.
					debug.Warning("Hashcat was aborted (exit code %d) for task %s", exitCode, process.TaskID)
					e.sendErrorProgress(process, process.taskFailureMessage(exitCode, fmt.Sprintf("Hashcat aborted with exit code %d", exitCode)))

				case -2:
					// GPU watchdog alarm (thermal/hang). Tag with GPU_WATCHDOG so
					// the backend treats it as transient (retry/cooldown).
					debug.Error("GPU watchdog alarm triggered for task %s", process.TaskID)
					e.sendErrorProgress(process, process.taskFailureMessage(exitCode, taskErrWatchdog+": GPU watchdog alarm - possible GPU hang or temperature issue"))

				case 255, -1:
					// Exit code 255 or -1 (4294967295 as unsigned) often means another instance is running
					process.mutex.Lock()
					alreadyRunning := process.AlreadyRunningError
					process.mutex.Unlock()

					if alreadyRunning {
						debug.Error("Hashcat exit code %d for task %s - confirmed another instance is running", exitCode, process.TaskID)
						e.sendErrorProgress(process, "Hashcat failed to start - another instance is already running")
					} else if process.HashcatRejectedHashlist.Load() {
						// Hashcat parsed the hashlist and rejected every line — wrong
						// hash type for these hashes, or the file is corrupt. Tag the
						// error with the BENCHMARK_NO_HASHES_LOADED sentinel so the
						// backend's AttributeBenchmarkFailure fast-fails the job
						// (no point retrying — every agent will hit the same
						// rejection).
						msg := fmt.Sprintf("BENCHMARK_NO_HASHES_LOADED: hashcat rejected all hashes (exit %d) — verify the hashlist contains valid hashes for hash type %d", exitCode, process.Assignment.HashType)
						debug.Error("[Task %s] %s", process.TaskID, msg)
						e.sendErrorProgress(process, msg)
					} else {
						// Unknown error — prefer a typed code if the stderr
						// scanner classified an agent-local fault (OOM, driver,
						// no-device, disk), which commonly surface as exit -1/255.
						debug.Error("Hashcat exit code %d for task %s - unknown error", exitCode, process.TaskID)
						e.sendErrorProgress(process, process.taskFailureMessage(exitCode, fmt.Sprintf("Hashcat failed with exit code %d", exitCode)))
					}

				default:
					// Other errors
					if exitCode < 0 {
						debug.Error("Hashcat backend error (exit code %d) for task %s", exitCode, process.TaskID)
						e.sendErrorProgress(process, process.taskFailureMessage(exitCode, fmt.Sprintf("Hashcat backend error with exit code %d", exitCode)))
					} else {
						debug.Warning("Hashcat unexpected exit code %d for task %s", exitCode, process.TaskID)
						e.sendErrorProgress(process, process.taskFailureMessage(exitCode, fmt.Sprintf("Hashcat exited with unexpected code %d", exitCode)))
					}
				}
			} else {
				e.sendErrorProgress(process, fmt.Sprintf("Hashcat process failed: %v", err))
			}
		} else if !e.reportNoWorkIfIdle(process, 0) {
			// Process completed successfully with exit code 0.
			// reportNoWorkIfIdle returned false, so a device actually ran and
			// reported progress — report the genuine completion. (A clean exit
			// with no status ever captured is failed there, not fake-completed.)
			debug.Info("Hashcat completed successfully with exit code 0 (OK/cracked) for task %s", process.TaskID)
			// Use the last progress values if available
			progressPercent := 100.0
			var effectiveProgress int64
			var crackedCount int
			if process.LastProgress != nil {
				if process.LastProgress.ProgressPercent > 0 {
					progressPercent = process.LastProgress.ProgressPercent
				}
				effectiveProgress = process.LastProgress.EffectiveProgress
				crackedCount = process.LastProgress.CrackedCount
			}
			debug.Info("Hashcat final progress for task %s: CrackedCount=%d", process.TaskID, crackedCount)
			finalProgress := &JobProgress{
				TaskID:            process.TaskID,
				KeyspaceProcessed: process.Assignment.KeyspaceEnd - process.Assignment.KeyspaceStart,
				EffectiveProgress: effectiveProgress,
				ProgressPercent:   progressPercent,
				CrackedCount:      crackedCount,
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

// reportNoWorkIfIdle guards the "hashcat exited cleanly but no device processed
// any candidates" case. hashcat can exit 0 (OK/cracked) yet never emit a single
// status line when the session was aborted before running — most commonly a
// kernel autotune failure that skips the device. Reporting such a run as
// completed would silently under-crack the job (this truncated a loopback chain
// one hash short during testing). When LastProgress is nil we therefore fail the
// task instead: if an autotune skip was detected we flag the executor to add
// --force to future runs and tag AGENT_AUTOTUNE so the monitor retries with
// --force; otherwise we tag AGENT_NO_WORK so the backend re-dispatches.
//
// Returns true if it handled (failed) the task; false if the run actually
// produced progress and the caller should report it completed. On a multi-GPU
// agent where one device autotune-skipped but another ran to completion,
// LastProgress is non-nil (real work happened) so the task completes normally,
// but forceKernel is still set so the skipped device participates next time.
func (e *HashcatExecutor) reportNoWorkIfIdle(process *HashcatProcess, exitCode int) bool {
	if process.LastProgress != nil || process.cracksReported.Load() > 0 {
		// A device ran and reported at least one status, or the run captured
		// cracks — either way it did real work, so let the caller complete it.
		return false
	}
	// forceKernel was already set upstream (centralized autotune check) if this
	// run tripped autotune; here we only choose the failure message.
	if process.AutotuneSkipped.Load() {
		msg := process.taskFailureMessage(exitCode, taskErrAutotune+": kernel autotune failure skipped the device before any candidates were tested")
		debug.Error("[Task %s] %s — failing task; subsequent runs on this agent will use --force", process.TaskID, msg)
		e.sendErrorProgress(process, msg)
		return true
	}
	msg := fmt.Sprintf("%s: hashcat exited %d without processing any candidates (no status ever reported)", taskErrNoWork, exitCode)
	debug.Error("[Task %s] %s — failing task instead of reporting a no-op completion", process.TaskID, msg)
	e.sendErrorProgress(process, msg)
	return true
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

	// Count it: a nonzero total proves this task did real work, so the exit
	// handler won't mistake a fast crack-then-exit (that raced past its final
	// status line) for a no-op run.
	process.cracksReported.Add(1)

	// Flush immediately if buffer reaches 10k cracks (WebSocket message size limit)
	// Otherwise let the timer handle batching naturally
	if len(e.crackBatchBuffers[taskID]) >= 10000 {
		debug.Info("Crack batch buffer reached size limit for task %s (%d cracks), flushing immediately",
			taskID, len(e.crackBatchBuffers[taskID]))
		e.flushCrackBatchLocked(process)
		return
	}

	// Start or reset the batch timer (will send everything after 500ms)
	// Skip timer creation if cleanup is in progress - cracks will be flushed explicitly
	if !process.CleanupInProgress.Load() {
		e.startBatchTimerLocked(process)
	}
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

// waitForActiveProcesses waits for all active hashcat processes to complete.
// This is critical before starting benchmarks because hashcat can only run one instance at a time.
// Times out after 30 seconds to prevent indefinite blocking.
func (e *HashcatExecutor) waitForActiveProcesses(ctx context.Context) error {
	const maxWait = 30 * time.Second
	const checkInterval = 100 * time.Millisecond

	startTime := time.Now()

	for {
		// Check if we've exceeded max wait time
		if time.Since(startTime) > maxWait {
			e.mutex.RLock()
			count := len(e.activeProcesses)
			e.mutex.RUnlock()
			return fmt.Errorf("timeout waiting for %d active processes to complete after %v", count, maxWait)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if any processes are still active
		e.mutex.RLock()
		count := len(e.activeProcesses)
		e.mutex.RUnlock()

		if count == 0 {
			debug.Debug("No active processes, safe to proceed")
			return nil
		}

		debug.Debug("Waiting for %d active processes to complete before speed test...", count)
		time.Sleep(checkInterval)
	}
}

// RunSpeedTest runs a real-world speed test with actual job configuration.
// testDuration bounds how long we collect status updates (seconds, must be >0).
// minStatusUpdates is the minimum number of hashcat --status-json ticks to
// collect before returning a result; <=0 falls back to a sane default.
// Returns: totalSpeed (H/s), deviceSpeeds, totalEffectiveKeyspace (progress[1]),
// agentBaseKeyspace, error. On benchmark-side failures, the returned error
// wraps ErrBenchmarkTimeout or ErrBenchmarkZeroSpeed so callers can label it.
func (e *HashcatExecutor) RunSpeedTest(ctx context.Context, assignment *JobTaskAssignment, testDuration int, minStatusUpdates int) (int64, []DeviceSpeed, int64, int64, error) {
	if minStatusUpdates < 1 {
		minStatusUpdates = 3
	}
	if testDuration < 1 {
		testDuration = 120
	}
	debug.Info("Running speed test for hash type %d, attack mode %d, duration %d seconds, min_status_updates %d",
		assignment.HashType, assignment.AttackMode, testDuration, minStatusUpdates)

	// Part 9: Wait for any active hashcat processes to complete before starting benchmark.
	// Hashcat can only run one instance at a time. If a task just completed (status=5 Exhausted),
	// there's a ~1.3 second delay before the process fully exits and is removed from activeProcesses.
	// Without this wait, the benchmark would fail with "Already an instance running" error.
	if err := e.waitForActiveProcesses(ctx); err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed waiting for active processes: %w", err)
	}

	// Build command similar to real job but without skip/limit and without --remove
	cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(assignment, true)
	if err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed to build command: %w", err)
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

	// Add outfile to redirect crack output away from stdout during benchmark
	// This prevents log noise from thousands of benchmark cracks
	benchmarkOutfile := filepath.Join(e.dataDirectory, "benchmark.out")
	filteredArgs = append(filteredArgs, "--outfile", benchmarkOutfile)

	debug.Info("Starting speed test with command: %s %s", cmd.Path, strings.Join(filteredArgs, " "))

	// Create new command with filtered args
	cmd = exec.CommandContext(ctx, cmd.Path, filteredArgs...)

	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(cmd.Path)
	debug.Info("Setting speed test working directory to: %s", cmd.Dir)

	// Set up pipes for stdout/stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return 0, nil, 0, 0, fmt.Errorf("failed to start hashcat: %w", err)
	}

	// Channel to collect status updates
	statusChan := make(chan string, 10)
	stopReading := make(chan bool)

	// readersWG lets cleanup know when both reader goroutines have exited so we
	// can safely tear down the process without leaking goroutines.
	var readersWG sync.WaitGroup
	readersWG.Add(2)

	// hashcatRejectedHashlist flips true when either reader sees one of the
	// known "no usable hashes in this file" stderr/stdout patterns. The
	// timeout path consults it to return a typed ErrBenchmarkNoHashesLoaded
	// instead of the generic ErrBenchmarkTimeout, which lets the backend
	// fast-fail the job instead of cycling through the per-tuple retry cap.
	var hashcatRejectedHashlist atomic.Bool

	// Read stdout in goroutine. The scanner unblocks when cleanup closes stdout
	// (defer below), which makes Scan() return false; sends to statusChan are
	// guarded with stopReading so a slow consumer can't deadlock the reader.
	go func() {
		defer readersWG.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			debug.Debug("[Speed test stdout raw] %s", line)
			// Hashcat reports per-line parsing failures on stdout
			// ("Hash parsing error in hashfile: ...") followed by a
			// summary ("* Token length exception: N/M hashes"). Any
			// of these means the hashlist contents are wrong for
			// the chosen hash mode; flag the run for fast-fail.
			if strings.Contains(line, "Hash parsing error") ||
				strings.Contains(line, "Token length exception") ||
				strings.Contains(line, "Separator unmatched") {
				hashcatRejectedHashlist.Store(true)
			}
			// A benchmark can also trip a kernel autotune skip on tiny/flaky
			// devices. Set the sticky force flag so a retry of this benchmark —
			// and every subsequent run — is launched with --force to bypass it.
			if isAutotuneSkip(line) {
				e.forceKernel.Store(true)
			}
			var jsonPart string
			if strings.Contains(line, ":") && strings.Contains(line, "{") && strings.Contains(line, "\"status\"") {
				jsonStart := strings.Index(line, "{")
				jsonPart = line[jsonStart:]
				debug.Debug("[Speed test] Found mixed output, extracted JSON: %s", jsonPart)
			} else if strings.HasPrefix(line, "{") && strings.Contains(line, "\"status\"") {
				jsonPart = line
				debug.Debug("[Speed test] Found pure JSON status")
			} else {
				continue
			}
			select {
			case statusChan <- jsonPart:
			case <-stopReading:
				return
			}
		}
	}()

	// Read stderr in goroutine. Unblocks the same way (cleanup closes stderr).
	go func() {
		defer readersWG.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			debug.Debug("[Hashcat stderr] %s", line)
			// "No hashes loaded." is hashcat's final verdict when
			// every input line was rejected. Same signal as the
			// per-line errors on stdout; both flip the flag so the
			// timeout path returns the typed sentinel.
			if strings.Contains(line, "No hashes loaded") {
				hashcatRejectedHashlist.Store(true)
			}
			// See the stdout reader: a kernel autotune skip during the benchmark
			// sets the sticky force flag so the retry uses --force.
			if isAutotuneSkip(line) {
				e.forceKernel.Store(true)
			}
		}
	}()

	// Cleanup: close pipes (unblocks readers), kill the process, wait with a
	// hard deadline so a stuck hashcat never leaves us with a zombie. This
	// runs on every return path including errors.
	cleanedUp := false
	cleanup := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true

		// Signal readers' send-paths to abort. Safe to close once.
		select {
		case <-stopReading:
		default:
			close(stopReading)
		}

		// Best-effort SIGKILL. Capture the error so we know if it failed.
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
				debug.Warning("[Speed test] Kill PID %d failed: %v", cmd.Process.Pid, err)
			}
		}

		// Force readers to exit by closing the pipes; cmd.Wait would close
		// them eventually, but only after the process actually dies.
		if stdout != nil {
			_ = stdout.Close()
		}
		if stderr != nil {
			_ = stderr.Close()
		}

		// Wait for the process to exit, but bound it. If hashcat is stuck in
		// a long syscall (e.g. decompressing a huge .gz wordlist), SIGKILL
		// may take time to land; we don't want to block here forever.
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			pid := -1
			if cmd.Process != nil {
				pid = cmd.Process.Pid
				_ = cmd.Process.Release()
			}
			debug.Warning("[Speed test] cmd.Wait did not return within 5s for PID %d; released process handle. Hashcat may still be alive and the next task on this agent could collide.", pid)
		}

		// Drain reader goroutines (stdout/stderr are now closed so they exit).
		readersWG.Wait()
	}
	defer cleanup()

	// Stop collecting after testDuration seconds, but never longer than the
	// context deadline (the agent caller wraps us in a context with a slightly
	// larger TimeoutDuration for belt-and-suspenders).
	collectFor := time.Duration(testDuration) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < collectFor {
			collectFor = remaining
		}
	}
	timer := time.NewTimer(collectFor)

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

				// Check if hashcat has completed.
				// Hashcat status codes (from upstream inc_types.h): 0=init, 1=autotune,
				// 2=selftest, 3=running, 4=paused, 5=exhausted, 6=cracked, 7=aborted,
				// 8=quit, 9=bypass. 5/6 are terminal; everything else means hashcat is
				// still working and a 0 H/s reading just means the GPUs aren't crunching
				// yet (init/autotune/decompress).
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

				// We want at least minStatusUpdates updates for stability.
				if len(statusUpdates) >= minStatusUpdates {
					debug.Info("[Speed test] Collected %d updates (min=%d), stopping collection", len(statusUpdates), minStatusUpdates)
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

	// Wait for status collection to complete, then run cleanup synchronously
	// so the process is reaped before we evaluate the result. The deferred
	// cleanup remains as a safety net for error paths.
	<-statusCollected
	cleanup()

	// Clean up benchmark outfile (best-effort, next benchmark will overwrite anyway)
	if err := os.Remove(benchmarkOutfile); err != nil && !os.IsNotExist(err) {
		debug.Debug("Failed to remove benchmark outfile (will be overwritten next run): %v", err)
	}

	// Check if we got any valid speed readings
	if lastValidSpeed == 0 {
		debug.Warning("[Speed test] No valid speed parsed during collection, checking stored updates")
		if len(statusUpdates) == 0 {
			// If stderr/stdout flagged that hashcat rejected the
			// hashlist content, surface the actionable failure mode
			// instead of the generic timeout. The backend uses this
			// code to fail the job immediately rather than cycling
			// through the per-tuple retry cap — retries are pointless
			// when the hashlist itself is wrong for the chosen mode.
			if hashcatRejectedHashlist.Load() {
				return 0, nil, 0, 0, fmt.Errorf("%w: hashcat rejected all hashes in the hashlist for hash type %d", ErrBenchmarkNoHashesLoaded, assignment.HashType)
			}
			// No JSON ever arrived: hashcat is still in autotune/init/decompress
			// when the timer fired. Backend reads this code as
			// BENCHMARK_TIMEOUT and asks the admin to bump the timeout.
			return 0, nil, 0, 0, fmt.Errorf("%w: no status updates received during %ds speed test", ErrBenchmarkTimeout, testDuration)
		}

		// Try to parse from the best available update.
		// Use the third update if available (more stable), otherwise the last.
		statusIndex := len(statusUpdates) - 1
		if len(statusUpdates) >= 3 {
			statusIndex = 2
		}

		debug.Debug("[Speed test] Attempting to parse update %d of %d: %s", statusIndex+1, len(statusUpdates), statusUpdates[statusIndex])
		totalSpeed, deviceSpeeds, totalEffective, err := e.parseSpeedFromJSON(statusUpdates[statusIndex])
		if err != nil {
			debug.Error("[Speed test] Failed to parse JSON from update %d. Content: %s", statusIndex+1, statusUpdates[statusIndex])
			return 0, nil, 0, 0, fmt.Errorf("failed to parse speed from status: %w", err)
		}

		if totalSpeed == 0 {
			// We collected updates but the GPUs reported 0 H/s on every one.
			// This was the user-visible failure mode in agent_6.log: the
			// backend's chunk math degenerated to a 1-candidate task. Refuse
			// the result here so the backend can attribute the failure and
			// surface an actionable error on the job.
			debug.Warning("[Speed test] Speed is 0 H/s after %d updates - returning typed failure", len(statusUpdates))
			return 0, nil, 0, 0, fmt.Errorf("%w: %d status updates, all 0 H/s", ErrBenchmarkZeroSpeed, len(statusUpdates))
		}

		lastValidSpeed = totalSpeed
		lastDeviceSpeeds = deviceSpeeds
		lastTotalEffectiveKeyspace = totalEffective
	}

	debug.Info("Speed test completed: %d H/s total, effective keyspace: %d from %d updates", lastValidSpeed, lastTotalEffectiveKeyspace, len(statusUpdates))

	// Previously we ran `hashcat --keyspace` here for every speed test and
	// shipped the result back as `agent_base_keyspace`. That was a holdover
	// from when coordinate conversion for -O kernel splits was unconditional.
	// It's now handled at task-execution time in buildHashcatCommandWithOptions
	// (guarded by IsKeyspaceSplit && BaseKeyspace > 0), so this call became
	// dead pre-task latency — ~49 s on a 26 GB .gz wordlist with a 264 k rule
	// file. Backend only logged the value, never used it; the wire field is
	// kept (sent as 0) to avoid a protocol bump.
	return lastValidSpeed, lastDeviceSpeeds, lastTotalEffectiveKeyspace, 0, nil
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
				filepath.Join(binaryDir, "hashcat.exe"), // Windows primary
				filepath.Join(binaryDir, "hashcat"),     // Windows fallback
			}
		case "linux":
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat.bin"), // Linux primary
				filepath.Join(binaryDir, "hashcat"),     // Linux fallback
			}
		case "darwin":
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat"),     // macOS primary
				filepath.Join(binaryDir, "hashcat.bin"), // macOS fallback
			}
		default:
			possiblePaths = []string{
				filepath.Join(binaryDir, "hashcat"),     // Default Unix-like
				filepath.Join(binaryDir, "hashcat.bin"), // Alternative
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

// loadHashlist loads the hashlist file and returns both the hash values slice and a lookup map
// The map provides O(1) lookups for crack parsing (key: lowercase hash, value: original hash)
func (e *HashcatExecutor) loadHashlist(hashlistPath string) ([]string, map[string]string, error) {
	file, err := os.Open(hashlistPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open hashlist file: %w", err)
	}
	defer file.Close()

	var hashes []string
	hashMap := make(map[string]string)
	scanner := bufio.NewScanner(file)
	// Increase buffer size for large hashes like NTLMv2
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			// Store the full hash line
			hashes = append(hashes, line)
			// Build lookup map: lowercase key -> original value for O(1) crack matching
			hashMap[strings.ToLower(line)] = line
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading hashlist file: %w", err)
	}

	debug.Info("Loaded %d hashes from hashlist file (map size: %d)", len(hashes), len(hashMap))
	return hashes, hashMap, nil
}

// parseWPAHash parses WPA/WPA2/WPA3 hashes where output format differs from input
// Input format: WPA*01*PMK*MAC_AP*MAC_STA*ESSID_HEX***
// Output format: PMK:MAC_AP:MAC_STA:ESSID:PASSWORD
func (e *HashcatExecutor) parseWPAHash(line string, hashlistContent []string) *CrackedHash {
	// Split the outfile line by colons
	parts := strings.Split(line, ":")
	if len(parts) < 5 {
		// Not enough parts for WPA format
		return nil
	}

	// Extract components from outfile
	pmk := strings.ToLower(parts[0])    // PMK hash (e.g., 4d4fe7aac3a2cecab195321ceb99a7d0)
	macAP := strings.ToLower(parts[1])  // MAC AP (e.g., fc690c158264)
	macSTA := strings.ToLower(parts[2]) // MAC STA (e.g., f4747f87f9f4)
	// parts[3] is ESSID (decoded from hex in original)
	password := parts[len(parts)-1] // Last part is always the password

	debug.Debug("[WPA Parser] Parsed outfile - PMK: %s, MAC_AP: %s, MAC_STA: %s, Password: %s",
		pmk, macAP, macSTA, password)

	// Search hashlist for a hash containing these components
	for _, knownHash := range hashlistContent {
		knownHashLower := strings.ToLower(knownHash)

		// WPA hash format: WPA*01*PMK*MAC_AP*MAC_STA*ESSID_HEX***
		// Check if this hash contains the PMK and MAC addresses
		if strings.Contains(knownHashLower, pmk) &&
			strings.Contains(knownHashLower, macAP) &&
			strings.Contains(knownHashLower, macSTA) {

			debug.Info("[WPA Parser] Matched WPA hash via PMK+MAC matching")
			return &CrackedHash{
				Hash:     knownHash, // Return original full format
				Plain:    password,
				FullLine: line,
			}
		}
	}

	debug.Warning("[WPA Parser] Could not match WPA hash - PMK: %s", pmk)
	return nil
}

// parseNetNTLMv2Hash parses NetNTLMv2 hashes where output format differs from input
// Input format: USER::DOMAIN:SERVER_CHALLENGE:NTPROOFSTR:BLOB
// Output format: Same as input with :PASSWORD appended
func (e *HashcatExecutor) parseNetNTLMv2Hash(line string, hashlistContent []string) *CrackedHash {
	// NetNTLMv2 hashes maintain their format but append :PASSWORD
	// The hashlist already contains the full hash, so we need to find where the password starts

	// Try to match the line prefix against hashlist entries
	for _, knownHash := range hashlistContent {
		knownHashLower := strings.ToLower(knownHash)
		lineLower := strings.ToLower(line)

		// Check if the line starts with the known hash
		if strings.HasPrefix(lineLower, knownHashLower) {
			// Extract password - it comes after the known hash and a colon
			if len(line) > len(knownHash)+1 && line[len(knownHash)] == ':' {
				password := line[len(knownHash)+1:]

				debug.Info("[NetNTLMv2 Parser] Matched NetNTLMv2 hash via prefix matching")
				return &CrackedHash{
					Hash:     knownHash,
					Plain:    password,
					FullLine: line,
				}
			}
		}
	}

	debug.Warning("[NetNTLMv2 Parser] Could not match NetNTLMv2 hash")
	return nil
}

// parseCrackedHash parses a cracked hash output line using hashlist knowledge
// Now with hash-type-aware parsing and O(1) HashMap lookup for standard types
func (e *HashcatExecutor) parseCrackedHash(line string, hashlistContent []string, hashlistMap map[string]string, hashType int) *CrackedHash {
	// First, try hash-type-specific parsers for known problematic types
	// These need special parsing because output format differs from input
	switch hashType {
	case 22000, 22001: // WPA-PBKDF2-PMKID+EAPOL, WPA-PMK-PMKID+EAPOL
		if cracked := e.parseWPAHash(line, hashlistContent); cracked != nil {
			return cracked
		}
	case 2500: // WPA-EAPOL-PBKDF2 (deprecated but may still be used)
		if cracked := e.parseWPAHash(line, hashlistContent); cracked != nil {
			return cracked
		}
	case 5600: // NetNTLMv2
		if cracked := e.parseNetNTLMv2Hash(line, hashlistContent); cracked != nil {
			return cracked
		}
	}

	// OPTIMIZED: O(1) HashMap lookup for standard hash types
	// Format is always: hash:password (hash may contain colons for some types)
	// We use LastIndex to handle hashes that contain colons (like SHA512CRYPT)
	lastColonIdx := strings.LastIndex(line, ":")
	if lastColonIdx == -1 {
		return nil
	}

	hashPart := line[:lastColonIdx]
	password := line[lastColonIdx+1:]
	hashPartLower := strings.ToLower(hashPart)

	// O(1) lookup in the pre-built map
	if originalHash, exists := hashlistMap[hashPartLower]; exists {
		return &CrackedHash{
			Hash:     originalHash, // Use original hash from hashlist (preserving case as stored in DB)
			Plain:    password,     // Password with original case
			FullLine: line,         // Keep the full line for reference
		}
	}

	// Fallback for edge cases (hash not in map but looks valid)
	// This can happen if hash was modified by hashcat output formatting
	if len(hashPart) >= 16 && !strings.Contains(hashPart, " ") {
		debug.Warning("[Crack Parser] Using fallback for unmatched hash: %s", hashPart)
		return &CrackedHash{
			Hash:     hashPart,
			Plain:    password,
			FullLine: line,
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

		// Parse using hash-type-aware parser with O(1) lookup (same as stdout reader)
		lineStr := strings.TrimSpace(line)
		cracked := e.parseCrackedHash(lineStr, process.HashlistContent, process.HashlistMap, process.Assignment.HashType)
		if cracked == nil {
			continue
		}

		lineKey := line // Use full line as key (includes newline for uniqueness)

		// Check if already sent
		process.OutfileMutex.Lock()
		if process.OutfileSentHashes == nil {
			process.OutfileSentHashes = make(map[string]bool)
		}

		if !process.OutfileSentHashes[lineKey] {
			process.OutfileSentHashes[lineKey] = true
			newCracks = append(newCracks, *cracked)
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

// RetransmitOutfile reads an entire outfile and returns all cracks for retransmission
func (e *HashcatExecutor) RetransmitOutfile(taskID string) ([]CrackedHash, error) {
	outfileDir := filepath.Join(e.dataDirectory, "outfile")
	outfilePath := filepath.Join(outfileDir, fmt.Sprintf("%s.txt", taskID))

	// Check if file exists
	if _, err := os.Stat(outfilePath); os.IsNotExist(err) {
		debug.Warning("Outfile does not exist for task %s: %s", taskID, outfilePath)
		return nil, nil // No error, just no cracks to retransmit
	}

	// Open and read the file
	file, err := os.Open(outfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open outfile for retransmit: %w", err)
	}
	defer file.Close()

	var cracks []CrackedHash
	scanner := bufio.NewScanner(file)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max line length

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse the line - format is hash:plain (format 1,2)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			debug.Warning("Invalid outfile line format: %s", line)
			continue
		}

		crack := CrackedHash{
			Hash:  parts[0],
			Plain: parts[1],
		}
		cracks = append(cracks, crack)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading outfile: %w", err)
	}

	debug.Info("Read %d cracks from outfile for task %s retransmission", len(cracks), taskID)
	return cracks, nil
}

// DeleteOutfile removes the outfile for a task after backend confirmation
func (e *HashcatExecutor) DeleteOutfile(taskID string) error {
	outfileDir := filepath.Join(e.dataDirectory, "outfile")
	outfilePath := filepath.Join(outfileDir, fmt.Sprintf("%s.txt", taskID))

	// Check if file exists
	if _, err := os.Stat(outfilePath); os.IsNotExist(err) {
		debug.Info("Outfile already deleted for task %s", taskID)
		return nil // Not an error if already gone
	}

	if err := os.Remove(outfilePath); err != nil {
		return fmt.Errorf("failed to delete outfile: %w", err)
	}

	debug.Info("Successfully deleted outfile for task %s", taskID)
	return nil
}

// GetPendingOutfiles returns all task IDs with unacknowledged outfiles
func (e *HashcatExecutor) GetPendingOutfiles() (taskIDs []string, currentTaskID string, err error) {
	outfileDir := filepath.Join(e.dataDirectory, "outfile")

	// Check if outfile directory exists
	if _, err := os.Stat(outfileDir); os.IsNotExist(err) {
		return nil, "", nil // No outfiles directory means no pending outfiles
	}

	// Read all files in the outfile directory
	entries, err := os.ReadDir(outfileDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read outfile directory: %w", err)
	}

	// Get currently running task ID
	e.mutex.RLock()
	for _, process := range e.activeProcesses {
		if process.IsRunning {
			currentTaskID = process.TaskID
			break
		}
	}
	e.mutex.RUnlock()

	// Collect all task IDs from outfile names
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Outfiles are named {task_id}.txt
		if strings.HasSuffix(name, ".txt") {
			taskID := strings.TrimSuffix(name, ".txt")
			// Validate it looks like a UUID
			if len(taskID) == 36 && strings.Count(taskID, "-") == 4 {
				taskIDs = append(taskIDs, taskID)
			}
		}
	}

	debug.Info("Found %d pending outfiles (current task: %s)", len(taskIDs), currentTaskID)
	return taskIDs, currentTaskID, nil
}

// GetOutfileLineCount returns the number of lines in the outfile for a task
func (e *HashcatExecutor) GetOutfileLineCount(taskID string) (int64, error) {
	outfileDir := filepath.Join(e.dataDirectory, "outfile")
	outfilePath := filepath.Join(outfileDir, fmt.Sprintf("%s.txt", taskID))

	file, err := os.Open(outfilePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	// Set larger buffer for files with long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error scanning outfile: %w", err)
	}

	return count, nil
}
