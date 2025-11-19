package types

// Device represents a compute device detected by hashcat (used during parsing)
type Device struct {
	ID       int    `json:"device_id"`
	Name     string `json:"device_name"`
	Type     string `json:"device_type"` // "GPU" or "CPU"
	Enabled  bool   `json:"enabled"`

	// Additional properties from hashcat output
	Processors  int    `json:"processors,omitempty"`
	Clock       int    `json:"clock,omitempty"`       // MHz
	MemoryTotal int64  `json:"memory_total,omitempty"` // MB
	MemoryFree  int64  `json:"memory_free,omitempty"`  // MB
	PCIAddress  string `json:"pci_address,omitempty"`

	// Backend information
	Backend     string `json:"backend,omitempty"`      // "HIP", "OpenCL", "CUDA", etc.
	IsAlias     bool   `json:"is_alias,omitempty"`
	AliasOf     int    `json:"alias_of,omitempty"`     // Device ID this is an alias of
}

// PhysicalDevice represents one physical GPU with multiple runtime options
type PhysicalDevice struct {
	Index           int             `json:"index"`            // 0-based position (stable ID)
	Name            string          `json:"name"`             // "GTX 1080 Ti"
	Type            string          `json:"type"`             // "GPU" or "CPU"
	Enabled         bool            `json:"enabled"`          // Overall enable/disable
	RuntimeOptions  []RuntimeOption `json:"runtime_options"`  // Available backends
	SelectedRuntime string          `json:"selected_runtime"` // "CUDA", "HIP", or "OpenCL"
}

// RuntimeOption represents one backend's view of the device
type RuntimeOption struct {
	Backend     string `json:"backend"`       // "CUDA", "HIP", "OpenCL"
	DeviceID    int    `json:"device_id"`     // Hashcat device ID for this backend
	Processors  int    `json:"processors"`
	Clock       int    `json:"clock"`
	MemoryTotal int64  `json:"memory_total"`
	MemoryFree  int64  `json:"memory_free"`
	PCIAddress  string `json:"pci_address"`
}

// DeviceDetectionResult represents the result of device detection (legacy)
type DeviceDetectionResult struct {
	Devices []Device `json:"devices"`
	Error   string   `json:"error,omitempty"`
}

// PhysicalDeviceDetectionResult represents the result of physical device detection
type PhysicalDeviceDetectionResult struct {
	Devices []PhysicalDevice `json:"devices"`
	Error   string           `json:"error,omitempty"`
}

// DeviceUpdate represents a device update request
type DeviceUpdate struct {
	DeviceID int  `json:"device_id"`
	Enabled  bool `json:"enabled"`
}

// RuntimeUpdate represents a runtime selection update request
type RuntimeUpdate struct {
	DeviceID int    `json:"device_id"`
	Runtime  string `json:"runtime"`
}