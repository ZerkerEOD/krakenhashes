package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AttackMode represents the Hashcat attack mode.
// Valid values: 0, 1, 3, 6, 7, 9.
type AttackMode int

const (
	AttackModeStraight           AttackMode = 0 // Straight
	AttackModeCombination        AttackMode = 1 // Combination
	AttackModeBruteForce         AttackMode = 3 // Brute-force
	AttackModeHybridWordlistMask AttackMode = 6 // Hybrid Wordlist + Mask
	AttackModeHybridMaskWordlist AttackMode = 7 // Hybrid Mask + Wordlist
	AttackModeAssociation        AttackMode = 9 // Association
)

// IDArray is a custom type for handling arrays of IDs stored as JSONB in PostgreSQL
type IDArray []string

// Value implements the driver.Valuer interface
func (a IDArray) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// Scan implements the sql.Scanner interface
func (a *IDArray) Scan(value interface{}) error {
	if value == nil {
		*a = IDArray{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("unsupported type for IDArray: %T", value)
	}

	return json.Unmarshal(bytes, a)
}

// PresetJob mirrors the preset_jobs table structure.
// It defines a pre-configured set of parameters for a cracking job.
type PresetJob struct {
	ID                        uuid.UUID  `json:"id" db:"id"`
	Name                      string     `json:"name" db:"name"`
	WordlistIDs               IDArray    `json:"wordlist_ids" db:"wordlist_ids"` // Stores numeric IDs as strings in JSONB
	RuleIDs                   IDArray    `json:"rule_ids" db:"rule_ids"`         // Stores numeric IDs as strings in JSONB
	AttackMode                AttackMode `json:"attack_mode" db:"attack_mode"`
	HashType                  int        `json:"hash_type" db:"hash_type"` // Hashcat hash type number
	Priority                  int        `json:"priority" db:"priority"`
	ChunkSizeSeconds          int        `json:"chunk_size_seconds" db:"chunk_size_seconds"`
	StatusUpdatesEnabled      bool       `json:"status_updates_enabled" db:"status_updates_enabled"`
	AllowHighPriorityOverride bool       `json:"allow_high_priority_override" db:"allow_high_priority_override"`
	BinaryVersionID           int        `json:"binary_version_id" db:"binary_version_id"`       // References binary_versions.id
	Mask                      string     `json:"mask,omitempty" db:"mask"`                       // For mask-based attack modes
	AdditionalArgs            *string    `json:"additional_args,omitempty" db:"additional_args"` // Additional hashcat arguments
	Keyspace                  *int64     `json:"keyspace,omitempty" db:"keyspace"`               // Pre-calculated keyspace for this preset
	MaxAgents                 int        `json:"max_agents" db:"max_agents"`                     // Max agents allowed (0 = unlimited)
	IncrementMode             string     `json:"increment_mode,omitempty" db:"increment_mode"`   // Mask increment mode: off, increment, increment_inverse
	IncrementMin              *int       `json:"increment_min,omitempty" db:"increment_min"`     // Starting mask length for increment mode
	IncrementMax              *int       `json:"increment_max,omitempty" db:"increment_max"`     // Maximum mask length for increment mode
	CreatedAt                 time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at" db:"updated_at"`

	// Fields potentially populated by JOINs in specific queries
	BinaryVersionName string `json:"binary_version_name,omitempty" db:"binary_version_name"` // Example: Populated when listing
}

// JobWorkflow mirrors the job_workflows table structure.
// It represents a named sequence of preset jobs.
type JobWorkflow struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`

	// Populated field holding the ordered steps
	Steps []JobWorkflowStep `json:"steps,omitempty"`
}

// JobWorkflowStep mirrors the job_workflow_steps table structure.
// It links a JobWorkflow to a PresetJob at a specific order.
type JobWorkflowStep struct {
	ID            int64     `json:"id" db:"id"` // Using int64 for BIGSERIAL
	JobWorkflowID uuid.UUID `json:"job_workflow_id" db:"job_workflow_id"`
	PresetJobID   uuid.UUID `json:"preset_job_id" db:"preset_job_id"`
	StepOrder     int       `json:"step_order" db:"step_order"`

	// Fields potentially populated by JOINs with preset_jobs
	PresetJobName        string     `json:"preset_job_name,omitempty" db:"preset_job_name"`
	PresetJobAttackMode  AttackMode `json:"preset_job_attack_mode,omitempty" db:"preset_job_attack_mode"`
	PresetJobPriority    int        `json:"preset_job_priority,omitempty" db:"preset_job_priority"`
	PresetJobBinaryID    int        `json:"preset_job_binary_id,omitempty" db:"preset_job_binary_id"`
	PresetJobBinaryName  string     `json:"preset_job_binary_name,omitempty" db:"preset_job_binary_name"`
	PresetJobWordlistIDs IDArray    `json:"preset_job_wordlist_ids,omitempty" db:"preset_job_wordlist_ids"`
	PresetJobRuleIDs     IDArray    `json:"preset_job_rule_ids,omitempty" db:"preset_job_rule_ids"`
}

// PresetJobBasic represents minimal information about a preset job
// Used for dropdowns and selection interfaces
type PresetJobBasic struct {
	ID                        uuid.UUID `json:"id" db:"id"`
	Name                      string    `json:"name" db:"name"`
	AllowHighPriorityOverride bool      `json:"allow_high_priority_override" db:"allow_high_priority_override"`
}

// JobExecutionStatus represents the status of a job execution
type JobExecutionStatus string

const (
	JobExecutionStatusPending    JobExecutionStatus = "pending"
	JobExecutionStatusRunning    JobExecutionStatus = "running"
	JobExecutionStatusPaused     JobExecutionStatus = "paused"
	JobExecutionStatusProcessing JobExecutionStatus = "processing" // Waiting for crack batches to be processed
	JobExecutionStatusCompleted  JobExecutionStatus = "completed"
	JobExecutionStatusFailed     JobExecutionStatus = "failed"
	JobExecutionStatusCancelled  JobExecutionStatus = "cancelled"
)

// JobExecution represents an actual running instance of a preset job
type JobExecution struct {
	ID                  uuid.UUID          `json:"id" db:"id"`
	PresetJobID         *uuid.UUID         `json:"preset_job_id" db:"preset_job_id"` // Nullable for custom jobs
	HashlistID          int64              `json:"hashlist_id" db:"hashlist_id"`
	Status              JobExecutionStatus `json:"status" db:"status"`
	Priority            int                `json:"priority" db:"priority"`
	MaxAgents           int                `json:"max_agents" db:"max_agents"`
	TotalKeyspace       *int64             `json:"total_keyspace" db:"total_keyspace"`
	ProcessedKeyspace   int64              `json:"processed_keyspace" db:"processed_keyspace"`
	AttackMode          AttackMode         `json:"attack_mode" db:"attack_mode"`
	CreatedBy           *uuid.UUID         `json:"created_by" db:"created_by"`
	CreatedAt           time.Time          `json:"created_at" db:"created_at"`
	StartedAt           *time.Time         `json:"started_at" db:"started_at"`
	CompletedAt         *time.Time         `json:"completed_at" db:"completed_at"`
	UpdatedAt           time.Time          `json:"updated_at" db:"updated_at"`
	ErrorMessage        *string            `json:"error_message" db:"error_message"`
	InterruptedBy       *uuid.UUID         `json:"interrupted_by" db:"interrupted_by"`
	ConsecutiveFailures int                `json:"consecutive_failures" db:"consecutive_failures"` // Track consecutive task failures

	// Self-contained configuration fields (no need to look up preset)
	Name                      string  `json:"name" db:"name"`
	WordlistIDs               IDArray `json:"wordlist_ids" db:"wordlist_ids"`
	RuleIDs                   IDArray `json:"rule_ids" db:"rule_ids"`
	HashType                  int     `json:"hash_type" db:"hash_type"`
	ChunkSizeSeconds          int     `json:"chunk_size_seconds" db:"chunk_size_seconds"`
	StatusUpdatesEnabled      bool    `json:"status_updates_enabled" db:"status_updates_enabled"`
	AllowHighPriorityOverride bool    `json:"allow_high_priority_override" db:"allow_high_priority_override"`
	BinaryVersionID           int     `json:"binary_version_id" db:"binary_version_id"`
	Mask                      string  `json:"mask,omitempty" db:"mask"`
	AdditionalArgs            *string `json:"additional_args,omitempty" db:"additional_args"`
	IncrementMode             string  `json:"increment_mode,omitempty" db:"increment_mode"` // Mask increment mode: off, increment, increment_inverse
	IncrementMin              *int    `json:"increment_min,omitempty" db:"increment_min"`   // Starting mask length for increment mode
	IncrementMax              *int    `json:"increment_max,omitempty" db:"increment_max"`   // Maximum mask length for increment mode

	// Enhanced chunking fields
	BaseKeyspace         *int64   `json:"base_keyspace" db:"base_keyspace"`                 // Wordlist-only keyspace
	EffectiveKeyspace    *int64   `json:"effective_keyspace" db:"effective_keyspace"`       // Base × multiplication factor (or from hashcat progress[1])
	MultiplicationFactor int      `json:"multiplication_factor" db:"multiplication_factor"` // Rules count or second wordlist size
	AvgRuleMultiplier    *float64 `json:"avg_rule_multiplier" db:"avg_rule_multiplier"`     // Actual effectiveness from hashcat: effective/base/rules
	IsAccurateKeyspace   bool     `json:"is_accurate_keyspace" db:"is_accurate_keyspace"`   // TRUE if effective_keyspace from hashcat progress[1]
	UsesRuleSplitting    bool     `json:"uses_rule_splitting" db:"uses_rule_splitting"`     // Whether this job uses rule splitting
	RuleSplitCount       int      `json:"rule_split_count" db:"rule_split_count"`           // Number of rule chunks created

	// Progress tracking
	OverallProgressPercent float64    `json:"overall_progress_percent" db:"overall_progress_percent"` // Overall job progress (0-100)
	LastProgressUpdate     *time.Time `json:"last_progress_update" db:"last_progress_update"`         // Last time progress was updated
	DispatchedKeyspace     int64      `json:"dispatched_keyspace" db:"dispatched_keyspace"`           // Total keyspace dispatched to tasks

	// Email notification tracking
	CompletionEmailSent   bool       `json:"completion_email_sent" db:"completion_email_sent"`
	CompletionEmailSentAt *time.Time `json:"completion_email_sent_at" db:"completion_email_sent_at"`
	CompletionEmailError  *string    `json:"completion_email_error" db:"completion_email_error"`
}

// JobTaskStatus represents the status of a job task
type JobTaskStatus string

const (
	JobTaskStatusPending          JobTaskStatus = "pending"
	JobTaskStatusAssigned         JobTaskStatus = "assigned"
	JobTaskStatusReconnectPending JobTaskStatus = "reconnect_pending"
	JobTaskStatusRunning          JobTaskStatus = "running"
	JobTaskStatusProcessing       JobTaskStatus = "processing" // Waiting for crack batches to be processed
	JobTaskStatusCompleted        JobTaskStatus = "completed"
	JobTaskStatusFailed           JobTaskStatus = "failed"
	JobTaskStatusCancelled        JobTaskStatus = "cancelled"
)

// JobTask represents a chunk of work assigned to an agent
type JobTask struct {
	ID                uuid.UUID     `json:"id" db:"id"`
	JobExecutionID    uuid.UUID     `json:"job_execution_id" db:"job_execution_id"`
	IncrementLayerID  *uuid.UUID    `json:"increment_layer_id" db:"increment_layer_id"` // References job_increment_layers if task belongs to a layer
	AgentID           *int          `json:"agent_id" db:"agent_id"`
	Status            JobTaskStatus `json:"status" db:"status"`
	Priority          int           `json:"priority" db:"priority"`     // Task priority (inherited from job)
	AttackCmd         string        `json:"attack_cmd" db:"attack_cmd"` // Full hashcat command for this task
	KeyspaceStart           int64   `json:"keyspace_start" db:"keyspace_start"`
	KeyspaceEnd             int64   `json:"keyspace_end" db:"keyspace_end"`
	KeyspaceProcessed       int64   `json:"keyspace_processed" db:"keyspace_processed"`
	EffectiveKeyspaceStart     *int64  `json:"effective_keyspace_start" db:"effective_keyspace_start"`         // For rule splitting: base_keyspace * rule_start_index (estimate or actual)
	EffectiveKeyspaceEnd       *int64  `json:"effective_keyspace_end" db:"effective_keyspace_end"`             // For rule splitting: base_keyspace * rule_end_index (estimate or actual)
	EffectiveKeyspaceProcessed *int64  `json:"effective_keyspace_processed" db:"effective_keyspace_processed"` // Actual effective progress
	IsActualKeyspace           bool    `json:"is_actual_keyspace" db:"is_actual_keyspace"`                     // TRUE if effective ranges from hashcat progress[1]
	ChunkActualKeyspace        *int64  `json:"chunk_actual_keyspace" db:"chunk_actual_keyspace"`               // Actual keyspace SIZE for this chunk from hashcat progress[1]
	ProgressPercent            float64 `json:"progress_percent" db:"progress_percent"`                         // Task progress percentage (0-100)
	BenchmarkSpeed    *int64        `json:"benchmark_speed" db:"benchmark_speed"`   // hashes per second (current/last reported)
	AverageSpeed      *int64        `json:"average_speed" db:"average_speed"`       // time-weighted average hashes per second
	ChunkDuration     int           `json:"chunk_duration" db:"chunk_duration"`     // seconds
	CreatedAt         time.Time     `json:"created_at" db:"created_at"`
	AssignedAt        *time.Time    `json:"assigned_at" db:"assigned_at"`
	StartedAt         *time.Time    `json:"started_at" db:"started_at"`
	CompletedAt       *time.Time    `json:"completed_at" db:"completed_at"`
	UpdatedAt         time.Time     `json:"updated_at" db:"updated_at"`
	LastCheckpoint    *time.Time    `json:"last_checkpoint" db:"last_checkpoint"`
	ErrorMessage      *string       `json:"error_message" db:"error_message"`

	// Enhanced fields for detailed chunk tracking
	CrackCount              int  `json:"crack_count" db:"crack_count"`
	ExpectedCrackCount      int  `json:"expected_crack_count" db:"expected_crack_count"`           // Expected cracks from progress message
	ReceivedCrackCount      int  `json:"received_crack_count" db:"received_crack_count"`           // Cracks received via batches
	BatchesCompleteSignaled bool `json:"batches_complete_signaled" db:"batches_complete_signaled"` // Agent signaled batches done
	DetailedStatus          string `json:"detailed_status" db:"detailed_status"`
	RetryCount              int    `json:"retry_count" db:"retry_count"`

	// Rule splitting fields
	RuleStartIndex  *int    `json:"rule_start_index" db:"rule_start_index"`     // Starting rule index for this chunk
	RuleEndIndex    *int    `json:"rule_end_index" db:"rule_end_index"`         // Ending rule index for this chunk
	RuleChunkPath   *string `json:"rule_chunk_path" db:"rule_chunk_path"`       // Path to temporary rule chunk file
	IsRuleSplitTask bool    `json:"is_rule_split_task" db:"is_rule_split_task"` // Whether this is a rule-split task
	IsKeyspaceSplit bool    `json:"is_keyspace_split" db:"is_keyspace_split"`   // Whether this task uses keyspace splitting (--skip/--limit)

	// Chunk tracking
	ChunkNumber int `json:"chunk_number" db:"chunk_number"` // Sequential chunk number within this job (1, 2, 3...)

	// Populated fields from JOINs
	AgentName *string `json:"agent_name,omitempty" db:"agent_name"`
}

// AgentBenchmark stores benchmark results for an agent
type AgentBenchmark struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	AgentID    int        `json:"agent_id" db:"agent_id"`
	AttackMode AttackMode `json:"attack_mode" db:"attack_mode"`
	HashType   int        `json:"hash_type" db:"hash_type"`
	Speed      int64      `json:"speed" db:"speed"` // hashes per second
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

// MetricType represents the type of metric being tracked
type MetricType string

const (
	MetricTypeHashRate    MetricType = "hash_rate"
	MetricTypeUtilization MetricType = "utilization"
	MetricTypeTemperature MetricType = "temperature"
	MetricTypePowerUsage  MetricType = "power_usage"
)

// JobMetricType represents job-specific metric types
type JobMetricType string

const (
	JobMetricTypeHashRate        JobMetricType = "hash_rate"
	JobMetricTypeProgressPercent JobMetricType = "progress_percentage"
	JobMetricTypeCracksFound     JobMetricType = "cracks_found"
)

// AggregationLevel represents the level of metric aggregation
type AggregationLevel string

const (
	AggregationLevelRealtime AggregationLevel = "realtime"
	AggregationLevelDaily    AggregationLevel = "daily"
	AggregationLevelWeekly   AggregationLevel = "weekly"
)

// AgentPerformanceMetric stores performance metrics for agents
type AgentPerformanceMetric struct {
	ID               uuid.UUID        `json:"id" db:"id"`
	AgentID          int              `json:"agent_id" db:"agent_id"`
	MetricType       MetricType       `json:"metric_type" db:"metric_type"`
	Value            float64          `json:"value" db:"value"`
	Timestamp        time.Time        `json:"timestamp" db:"timestamp"`
	AggregationLevel AggregationLevel `json:"aggregation_level" db:"aggregation_level"`
	PeriodStart      *time.Time       `json:"period_start" db:"period_start"`
	PeriodEnd        *time.Time       `json:"period_end" db:"period_end"`
	
	// Device tracking fields
	DeviceID   *int       `json:"device_id" db:"device_id"`     // Device ID from hashcat
	DeviceName *string    `json:"device_name" db:"device_name"` // Human-readable device name
	TaskID     *uuid.UUID `json:"task_id" db:"task_id"`         // Link to job task
	AttackMode *int       `json:"attack_mode" db:"attack_mode"` // Hashcat attack mode
}

// JobPerformanceMetric stores performance metrics for job executions
type JobPerformanceMetric struct {
	ID               uuid.UUID        `json:"id" db:"id"`
	JobExecutionID   uuid.UUID        `json:"job_execution_id" db:"job_execution_id"`
	MetricType       JobMetricType    `json:"metric_type" db:"metric_type"`
	Value            float64          `json:"value" db:"value"`
	Timestamp        time.Time        `json:"timestamp" db:"timestamp"`
	AggregationLevel AggregationLevel `json:"aggregation_level" db:"aggregation_level"`
	PeriodStart      *time.Time       `json:"period_start" db:"period_start"`
	PeriodEnd        *time.Time       `json:"period_end" db:"period_end"`
}

// AgentHashlist tracks hashlist distribution to agents
type AgentHashlist struct {
	ID           uuid.UUID `json:"id" db:"id"`
	AgentID      int       `json:"agent_id" db:"agent_id"`
	HashlistID   int64     `json:"hashlist_id" db:"hashlist_id"`
	FilePath     string    `json:"file_path" db:"file_path"`
	DownloadedAt time.Time `json:"downloaded_at" db:"downloaded_at"`
	LastUsedAt   time.Time `json:"last_used_at" db:"last_used_at"`
	FileHash     *string   `json:"file_hash" db:"file_hash"` // MD5 hash for verification
}

// JobTaskAssignment contains the information sent to an agent to execute a task
type JobTaskAssignment struct {
	TaskID         uuid.UUID  `json:"task_id"`
	JobExecutionID uuid.UUID  `json:"job_execution_id"`
	HashlistID     int64      `json:"hashlist_id"`
	HashlistPath   string     `json:"hashlist_path"` // Path where agent should download hashlist
	AttackMode     AttackMode `json:"attack_mode"`
	HashType       int        `json:"hash_type"`
	KeyspaceStart  int64      `json:"keyspace_start"`
	KeyspaceEnd    int64      `json:"keyspace_end"`
	WordlistPaths  []string   `json:"wordlist_paths"`  // Local paths on agent
	RulePaths      []string   `json:"rule_paths"`      // Local paths on agent
	Mask           string     `json:"mask,omitempty"`  // For mask-based attacks
	BinaryPath     string     `json:"binary_path"`     // Hashcat binary to use
	ChunkDuration  int        `json:"chunk_duration"`  // Expected duration in seconds
	ReportInterval int        `json:"report_interval"` // Progress reporting interval
	OutputFormat   string     `json:"output_format"`   // Hashcat output format
	IncrementMode  string     `json:"increment_mode,omitempty"` // Mask increment mode: off, increment, increment_inverse
	IncrementMin   *int       `json:"increment_min,omitempty"`  // Starting mask length for increment mode
	IncrementMax   *int       `json:"increment_max,omitempty"`  // Maximum mask length for increment mode
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

// JobProgress represents a progress update from an agent
type JobProgress struct {
	TaskID                 uuid.UUID      `json:"task_id"`
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

// CrackBatch represents crack-only message from agent (asynchronous, dual-channel)
type CrackBatch struct {
	TaskID        uuid.UUID     `json:"task_id"`
	CrackedHashes []CrackedHash `json:"cracked_hashes"`
}

// CrackBatchesComplete signals that agent has finished sending all crack batches for a task
type CrackBatchesComplete struct {
	TaskID uuid.UUID `json:"task_id"`
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

// BenchmarkRequest represents a request to test speed for a specific job configuration
// Now enhanced to include full job configuration for real-world speed testing
type BenchmarkRequest struct {
	RequestID       string     `json:"request_id"`
	TaskID          uuid.UUID  `json:"task_id"`
	HashlistID      int64      `json:"hashlist_id"`
	HashlistPath    string     `json:"hashlist_path"`
	AttackMode      AttackMode `json:"attack_mode"`
	HashType        int        `json:"hash_type"`
	WordlistPaths   []string   `json:"wordlist_paths"`
	RulePaths       []string   `json:"rule_paths"`
	Mask            string     `json:"mask,omitempty"`
	BinaryPath      string     `json:"binary_path"`
	TestDuration    int        `json:"test_duration"`    // How long to run test (seconds)
	TimeoutDuration int        `json:"timeout_duration"` // Maximum time to wait for speedtest (seconds)
}

// BenchmarkResult represents the result of a speed test
type BenchmarkResult struct {
	RequestID    string        `json:"request_id"`
	TaskID       uuid.UUID     `json:"task_id"`
	TotalSpeed   int64         `json:"total_speed"` // Total H/s across all devices
	DeviceSpeeds []DeviceSpeed `json:"device_speeds"`
	Success      bool          `json:"success"`
	ErrorMessage string        `json:"error_message,omitempty"`
}

// DeviceSpeed represents speed for a single device
type DeviceSpeed struct {
	DeviceID   int    `json:"device_id"`
	DeviceName string `json:"device_name"`
	Speed      int64  `json:"speed"` // H/s for this device
}

// JobExecutionWithWork extends JobExecution with work status information
type JobExecutionWithWork struct {
	JobExecution
	ActiveAgents int `db:"active_agents" json:"active_agents"`
	PendingWork  int `db:"pending_work" json:"pending_work"`
}

// JobIncrementLayerStatus represents the status of an increment layer
// Uses same statuses as JobExecutionStatus since layers are jobs in their own right
type JobIncrementLayerStatus string

const (
	JobIncrementLayerStatusPending   JobIncrementLayerStatus = "pending"
	JobIncrementLayerStatusRunning   JobIncrementLayerStatus = "running"
	JobIncrementLayerStatusPaused    JobIncrementLayerStatus = "paused"
	JobIncrementLayerStatusCompleted JobIncrementLayerStatus = "completed"
	JobIncrementLayerStatusFailed    JobIncrementLayerStatus = "failed"
	JobIncrementLayerStatusCancelled JobIncrementLayerStatus = "cancelled"
)

// JobIncrementLayer represents a sub-layer for increment mode jobs
// Each layer corresponds to one mask length (e.g., ?l?l for length 2)
type JobIncrementLayer struct {
	ID               uuid.UUID               `json:"id" db:"id"`
	JobExecutionID   uuid.UUID               `json:"job_execution_id" db:"job_execution_id"`
	LayerIndex       int                     `json:"layer_index" db:"layer_index"`             // Ordering based on increment mode
	Mask             string                  `json:"mask" db:"mask"`                           // Specific mask for this layer
	Status           JobIncrementLayerStatus `json:"status" db:"status"`

	// Keyspace tracking
	BaseKeyspace         *int64  `json:"base_keyspace" db:"base_keyspace"`                 // From --keyspace command
	EffectiveKeyspace    *int64  `json:"effective_keyspace" db:"effective_keyspace"`       // From benchmark progress[1]
	ProcessedKeyspace    int64   `json:"processed_keyspace" db:"processed_keyspace"`       // Sum from tasks
	DispatchedKeyspace   int64   `json:"dispatched_keyspace" db:"dispatched_keyspace"`     // Total keyspace dispatched
	IsAccurateKeyspace   bool    `json:"is_accurate_keyspace" db:"is_accurate_keyspace"`   // TRUE after benchmark

	// Progress tracking
	OverallProgressPercent float64    `json:"overall_progress_percent" db:"overall_progress_percent"`
	LastProgressUpdate     *time.Time `json:"last_progress_update" db:"last_progress_update"`

	// Timing
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	StartedAt   *time.Time `json:"started_at" db:"started_at"`
	CompletedAt *time.Time `json:"completed_at" db:"completed_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`

	// Error handling
	ErrorMessage *string `json:"error_message" db:"error_message"`
}

// JobIncrementLayerWithStats extends JobIncrementLayer with aggregated statistics
type JobIncrementLayerWithStats struct {
	JobIncrementLayer
	TotalTasks     int `db:"total_tasks" json:"total_tasks"`         // Total tasks created for this layer
	RunningTasks   int `db:"running_tasks" json:"running_tasks"`     // Tasks currently running/assigned/processing
	CompletedTasks int `db:"completed_tasks" json:"completed_tasks"` // Tasks that completed successfully
	FailedTasks    int `db:"failed_tasks" json:"failed_tasks"`       // Tasks that failed
	CrackCount     int `db:"crack_count" json:"crack_count"`         // Total cracks from this layer's tasks
}
