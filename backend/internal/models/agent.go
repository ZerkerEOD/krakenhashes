package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Agent status constants
const (
	AgentStatusPending  = "pending"
	AgentStatusActive   = "active"
	AgentStatusInactive = "inactive"
	AgentStatusError    = "error"
	AgentStatusDisabled = "disabled"
)

// Agent sync status constants
const (
	AgentSyncStatusPending    = "pending"
	AgentSyncStatusInProgress = "in_progress"
	AgentSyncStatusCompleted  = "completed"
	AgentSyncStatusFailed     = "failed"
)

// AgentWithTask represents an agent with its current task information
type AgentWithTask struct {
	Agent
	CurrentTask  *JobTask      `json:"currentTask,omitempty"`
	JobExecution *JobExecution `json:"jobExecution,omitempty"`
}

// MarshalJSON implements custom JSON marshalling for AgentWithTask.
// This is necessary because Agent has a custom MarshalJSON that would otherwise
// shadow the AgentWithTask's additional fields (CurrentTask, JobExecution).
func (a AgentWithTask) MarshalJSON() ([]byte, error) {
	// First, marshal the embedded Agent to get its JSON representation
	agentJSON, err := a.Agent.MarshalJSON()
	if err != nil {
		return nil, err
	}

	// If no task info, just return the agent JSON
	if a.CurrentTask == nil && a.JobExecution == nil {
		return agentJSON, nil
	}

	// Unmarshal agent JSON into a map so we can add additional fields
	var result map[string]interface{}
	if err := json.Unmarshal(agentJSON, &result); err != nil {
		return nil, err
	}

	// Add CurrentTask if present
	if a.CurrentTask != nil {
		result["currentTask"] = a.CurrentTask
	}

	// Add JobExecution if present
	if a.JobExecution != nil {
		result["jobExecution"] = a.JobExecution
	}

	return json.Marshal(result)
}

// Agent represents a registered agent in the system
type Agent struct {
	ID                  int               `json:"id"`
	Name                string            `json:"name"`
	Status              string            `json:"status"`
	LastError           sql.NullString    `json:"lastError"`
	LastSeen            time.Time         `json:"lastSeen"`
	LastHeartbeat       time.Time         `json:"lastHeartbeat"`
	Version             string            `json:"version"`
	Hardware            Hardware          `json:"hardware"`
	OSInfo              json.RawMessage   `json:"os_info"`
	CreatedByID         uuid.UUID         `json:"createdById"`
	CreatedBy           *User             `json:"createdBy,omitempty"`
	Teams               []Team            `json:"teams,omitempty"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
	APIKey              sql.NullString    `json:"-"`
	APIKeyCreatedAt     sql.NullTime      `json:"-"`
	APIKeyLastUsed      sql.NullTime      `json:"-"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	OwnerID             *uuid.UUID        `json:"ownerId,omitempty"`
	ExtraParameters     string            `json:"extraParameters"`
	IsEnabled           bool              `json:"isEnabled"`
	ConsecutiveFailures int               `json:"consecutiveFailures"` // Track consecutive task failures
	SchedulingEnabled   bool              `json:"schedulingEnabled"`
	ScheduleTimezone    string            `json:"scheduleTimezone"`
	SyncStatus          string            `json:"syncStatus"`
	SyncCompletedAt     sql.NullTime      `json:"syncCompletedAt"`
	SyncStartedAt       sql.NullTime      `json:"syncStartedAt"`
	SyncError           sql.NullString    `json:"syncError"`
	FilesToSync         int               `json:"filesToSync"`
	FilesSynced         int               `json:"filesSynced"`
	BinaryVersionID     sql.NullInt64     `json:"binaryVersionId,omitempty"` // Optional override binary version
	BinaryOverride      bool              `json:"binaryOverride"`            // Whether binary_version_id is manually set
}

// Hardware represents the hardware configuration of an agent
type Hardware struct {
	CPUs              []CPU              `json:"cpus"`
	GPUs              []GPU              `json:"gpus"`
	NetworkInterfaces []NetworkInterface `json:"network_interfaces"`
}

// CPU represents a CPU in the agent's hardware
type CPU struct {
	Model       string  `json:"model"`
	Cores       int     `json:"cores"`
	Threads     int     `json:"threads"`
	Frequency   float64 `json:"frequency"`
	Temperature float64 `json:"temperature"`
}

// GPU represents a GPU in the agent's hardware
type GPU struct {
	Vendor      string  `json:"vendor"`
	Model       string  `json:"model"`
	Memory      int64   `json:"memory"`
	Driver      string  `json:"driver"`
	Temperature float64 `json:"temperature"`
	PowerUsage  float64 `json:"powerUsage"`
	Utilization float64 `json:"utilization"`
}

// NetworkInterface represents a network interface in the agent's hardware
type NetworkInterface struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
}

// AgentMetrics represents metrics collected from an agent
type AgentMetrics struct {
	ID             int             `json:"id"`
	AgentID        int             `json:"agent_id"`
	CPUUsage       float64         `json:"cpu_usage"`
	MemoryUsage    float64         `json:"memory_usage"`
	GPUUtilization float64         `json:"gpu_utilization"`
	GPUTemp        float64         `json:"gpu_temp"`
	GPUMetrics     json.RawMessage `json:"gpu_metrics"`
	Timestamp      time.Time       `json:"timestamp"`
}

// ScanHardware scans a JSON-encoded hardware string into the Hardware struct
func (a *Agent) ScanHardware(value interface{}) error {
	if value == nil {
		a.Hardware = Hardware{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, &a.Hardware)
	case string:
		return json.Unmarshal([]byte(v), &a.Hardware)
	default:
		return fmt.Errorf("unsupported type for hardware: %T", value)
	}
}

// Value returns the JSON encoding of Hardware for database storage
func (h Hardware) Value() (driver.Value, error) {
	return json.Marshal(h)
}

// MarshalJSON implements custom JSON marshalling for Agent to handle sql.NullInt64
func (a Agent) MarshalJSON() ([]byte, error) {
	// Create a struct with explicit fields to avoid embedding issues
	type AgentJSON struct {
		ID                  int               `json:"id"`
		Name                string            `json:"name"`
		Status              string            `json:"status"`
		LastError           sql.NullString    `json:"lastError"`
		LastSeen            time.Time         `json:"lastSeen"`
		LastHeartbeat       time.Time         `json:"lastHeartbeat"`
		Version             string            `json:"version"`
		Hardware            Hardware          `json:"hardware"`
		OSInfo              json.RawMessage   `json:"os_info"`
		CreatedByID         uuid.UUID         `json:"createdById"`
		CreatedBy           *User             `json:"createdBy,omitempty"`
		Teams               []Team            `json:"teams,omitempty"`
		CreatedAt           time.Time         `json:"createdAt"`
		UpdatedAt           time.Time         `json:"updatedAt"`
		Metadata            map[string]string `json:"metadata,omitempty"`
		OwnerID             *uuid.UUID        `json:"ownerId,omitempty"`
		ExtraParameters     string            `json:"extraParameters"`
		IsEnabled           bool              `json:"isEnabled"`
		ConsecutiveFailures int               `json:"consecutiveFailures"`
		SchedulingEnabled   bool              `json:"schedulingEnabled"`
		ScheduleTimezone    string            `json:"scheduleTimezone"`
		SyncStatus          string            `json:"syncStatus"`
		SyncCompletedAt     sql.NullTime      `json:"syncCompletedAt"`
		SyncStartedAt       sql.NullTime      `json:"syncStartedAt"`
		SyncError           sql.NullString    `json:"syncError"`
		FilesToSync         int               `json:"filesToSync"`
		FilesSynced         int               `json:"filesSynced"`
		BinaryVersionID     *int64            `json:"binaryVersionId,omitempty"` // Custom handling for sql.NullInt64
		BinaryOverride      bool              `json:"binaryOverride"`
	}

	temp := AgentJSON{
		ID:                  a.ID,
		Name:                a.Name,
		Status:              a.Status,
		LastError:           a.LastError,
		LastSeen:            a.LastSeen,
		LastHeartbeat:       a.LastHeartbeat,
		Version:             a.Version,
		Hardware:            a.Hardware,
		OSInfo:              a.OSInfo,
		CreatedByID:         a.CreatedByID,
		CreatedBy:           a.CreatedBy,
		Teams:               a.Teams,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
		Metadata:            a.Metadata,
		OwnerID:             a.OwnerID,
		ExtraParameters:     a.ExtraParameters,
		IsEnabled:           a.IsEnabled,
		ConsecutiveFailures: a.ConsecutiveFailures,
		SchedulingEnabled:   a.SchedulingEnabled,
		ScheduleTimezone:    a.ScheduleTimezone,
		SyncStatus:          a.SyncStatus,
		SyncCompletedAt:     a.SyncCompletedAt,
		SyncStartedAt:       a.SyncStartedAt,
		SyncError:           a.SyncError,
		FilesToSync:         a.FilesToSync,
		FilesSynced:         a.FilesSynced,
		BinaryOverride:      a.BinaryOverride,
	}

	// Convert sql.NullInt64 to *int64 for proper JSON marshalling
	if a.BinaryVersionID.Valid && a.BinaryVersionID.Int64 > 0 {
		temp.BinaryVersionID = &a.BinaryVersionID.Int64
	} else {
		temp.BinaryVersionID = nil
	}

	return json.Marshal(temp)
}
