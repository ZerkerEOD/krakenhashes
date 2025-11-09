package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// AgentDevice represents a compute device on an agent
type AgentDevice struct {
	ID              int            `json:"id" db:"id"`
	AgentID         int            `json:"agent_id" db:"agent_id"`
	DeviceID        int            `json:"device_id" db:"device_id"` // Physical device index (0-based)
	DeviceName      string         `json:"device_name" db:"device_name"`
	DeviceType      string         `json:"device_type" db:"device_type"` // "GPU" or "CPU"
	Enabled         bool           `json:"enabled" db:"enabled"`
	RuntimeOptions  RuntimeOptions `json:"runtime_options" db:"runtime_options"` // JSONB
	SelectedRuntime string         `json:"selected_runtime" db:"selected_runtime"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
}

// GetHashcatDeviceID returns the hashcat device ID for the selected runtime
func (d *AgentDevice) GetHashcatDeviceID() int {
	for _, opt := range d.RuntimeOptions {
		if opt.Backend == d.SelectedRuntime {
			return opt.DeviceID
		}
	}
	// Fallback to first available option if selected runtime not found
	if len(d.RuntimeOptions) > 0 {
		return d.RuntimeOptions[0].DeviceID
	}
	return 0
}

// RuntimeOptions is a slice of RuntimeOption that implements sql Scanner and driver Valuer for JSONB
type RuntimeOptions []RuntimeOption

// Scan implements the sql.Scanner interface for reading from database
func (r *RuntimeOptions) Scan(value interface{}) error {
	if value == nil {
		*r = []RuntimeOption{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan RuntimeOptions: expected []byte, got %T", value)
	}

	if len(bytes) == 0 {
		*r = []RuntimeOption{}
		return nil
	}

	return json.Unmarshal(bytes, r)
}

// Value implements the driver.Valuer interface for writing to database
func (r RuntimeOptions) Value() (driver.Value, error) {
	if r == nil || len(r) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(r)
}

// RuntimeOption represents one backend's view of the device
type RuntimeOption struct {
	Backend     string `json:"backend"`       // "CUDA", "HIP", "OpenCL"
	DeviceID    int    `json:"device_id"`     // Hashcat device ID for this backend
	Processors  int    `json:"processors"`
	Clock       int    `json:"clock"`         // MHz
	MemoryTotal int64  `json:"memory_total"`  // MB
	MemoryFree  int64  `json:"memory_free"`   // MB
	PCIAddress  string `json:"pci_address"`
}

// DeviceDetectionResult represents the result from agent device detection (legacy)
type DeviceDetectionResult struct {
	Devices []Device `json:"devices"`
	Error   string   `json:"error,omitempty"`
}

// PhysicalDeviceDetectionResult represents the result from physical device detection
type PhysicalDeviceDetectionResult struct {
	Devices []PhysicalDevice `json:"devices"`
	Error   string           `json:"error,omitempty"`
}

// PhysicalDevice represents one physical GPU with multiple runtime options
type PhysicalDevice struct {
	Index           int              `json:"index"`            // 0-based position (stable ID)
	Name            string           `json:"name"`             // "GTX 1080 Ti"
	Type            string           `json:"type"`             // "GPU" or "CPU"
	Enabled         bool             `json:"enabled"`          // Overall enable/disable
	RuntimeOptions  []RuntimeOption  `json:"runtime_options"`  // Available backends
	SelectedRuntime string           `json:"selected_runtime"` // "CUDA", "HIP", or "OpenCL"
}

// Device represents a compute device detected by hashcat (legacy)
type Device struct {
	ID      int    `json:"device_id"`
	Name    string `json:"device_name"`
	Type    string `json:"device_type"` // "GPU" or "CPU"
	Enabled bool   `json:"enabled"`

	// Additional properties from hashcat output
	Processors  int    `json:"processors,omitempty"`
	Clock       int    `json:"clock,omitempty"`        // MHz
	MemoryTotal int64  `json:"memory_total,omitempty"` // MB
	MemoryFree  int64  `json:"memory_free,omitempty"`  // MB
	PCIAddress  string `json:"pci_address,omitempty"`

	// Backend information
	Backend string `json:"backend,omitempty"` // "HIP", "OpenCL", "CUDA", etc.
	IsAlias bool   `json:"is_alias,omitempty"`
	AliasOf int    `json:"alias_of,omitempty"` // Device ID this is an alias of
}

// DeviceUpdate represents a device update request
type DeviceUpdate struct {
	DeviceID int  `json:"device_id"`
	Enabled  bool `json:"enabled"`
}

// RuntimeSelectionUpdate represents a runtime selection update request
type RuntimeSelectionUpdate struct {
	Runtime string `json:"runtime"`
}

// AgentWithDevices represents an agent with its devices
type AgentWithDevices struct {
	Agent
	Devices []AgentDevice `json:"devices"`
}
