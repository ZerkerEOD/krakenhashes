package hardware

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/hardware/types"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// MockMonitor simulates hardware monitoring for testing
type MockMonitor struct {
	mu          sync.RWMutex
	devices     []types.Device
	gpuCount    int
	gpuVendor   string
	gpuModel    string
	gpuMemoryMB int64
}

// NewMockMonitor creates a new mock hardware monitor
func NewMockMonitor() *MockMonitor {
	// Load configuration from environment variables
	gpuCount := getEnvInt("MOCK_GPU_COUNT", 2)
	gpuVendor := getEnvString("MOCK_GPU_VENDOR", "nvidia")
	gpuModel := getEnvString("MOCK_GPU_MODEL", "")
	gpuMemoryMB := getEnvInt64("MOCK_GPU_MEMORY_MB", 24576) // 24GB default

	// Set default model based on vendor if not specified
	if gpuModel == "" {
		switch strings.ToLower(gpuVendor) {
		case "nvidia":
			gpuModel = "NVIDIA GeForce RTX 4090"
		case "amd":
			gpuModel = "AMD Radeon RX 7900 XTX"
		case "intel":
			gpuModel = "Intel Arc A770"
		default:
			gpuModel = "Generic GPU"
		}
	}

	debug.Info("Creating mock hardware monitor:")
	debug.Info("  GPU Count: %d", gpuCount)
	debug.Info("  GPU Vendor: %s", gpuVendor)
	debug.Info("  GPU Model: %s", gpuModel)
	debug.Info("  GPU Memory: %d MB", gpuMemoryMB)

	m := &MockMonitor{
		devices:     make([]types.Device, 0),
		gpuCount:    gpuCount,
		gpuVendor:   gpuVendor,
		gpuModel:    gpuModel,
		gpuMemoryMB: gpuMemoryMB,
	}

	// Initialize mock devices
	m.initializeMockDevices()

	return m
}

// initializeMockDevices creates fake GPU devices
func (m *MockMonitor) initializeMockDevices() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.devices = make([]types.Device, m.gpuCount)

	for i := 0; i < m.gpuCount; i++ {
		m.devices[i] = types.Device{
			ID:          i + 1,
			Name:        fmt.Sprintf("%s #%d", m.gpuModel, i+1),
			Type:        "GPU",
			Enabled:     true,
			MemoryTotal: m.gpuMemoryMB,
			Backend:     strings.ToUpper(m.gpuVendor),
		}
	}

	debug.Info("Initialized %d mock GPU devices", m.gpuCount)
}

// SetPreferredBinaryVersion sets the preferred binary version (no-op for mock)
func (m *MockMonitor) SetPreferredBinaryVersion(version int64) {
	debug.Info("Mock monitor: setting preferred binary version to %d (no-op)", version)
}

// Cleanup releases monitor resources (no-op for mock)
func (m *MockMonitor) Cleanup() error {
	debug.Info("Mock monitor cleanup (no-op)")
	return nil
}

// DetectDevices returns mock device detection results
func (m *MockMonitor) DetectDevices() (*types.DeviceDetectionResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	debug.Info("Mock monitor: detecting %d devices", len(m.devices))

	// Return a copy of devices
	devices := make([]types.Device, len(m.devices))
	copy(devices, m.devices)

	result := &types.DeviceDetectionResult{
		Devices: devices,
	}

	return result, nil
}

// DetectPhysicalDevices returns mock physical device detection results
func (m *MockMonitor) DetectPhysicalDevices() (*types.PhysicalDeviceDetectionResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	debug.Info("Mock monitor: detecting physical devices")

	// Create physical devices (one per GPU for simplicity)
	physicalDevices := make([]types.PhysicalDevice, m.gpuCount)

	for i := 0; i < m.gpuCount; i++ {
		// Each physical device has one runtime
		runtimes := []types.RuntimeOption{
			{
				Backend:     strings.ToUpper(m.gpuVendor),
				DeviceID:    i + 1,
				MemoryTotal: m.gpuMemoryMB,
			},
		}

		physicalDevices[i] = types.PhysicalDevice{
			Index:           i,
			Name:            fmt.Sprintf("%s #%d", m.gpuModel, i+1),
			Type:            "GPU",
			Enabled:         true,
			RuntimeOptions:  runtimes,
			SelectedRuntime: strings.ToUpper(m.gpuVendor),
		}
	}

	result := &types.PhysicalDeviceDetectionResult{
		Devices: physicalDevices,
	}

	return result, nil
}

// HasBinary always returns true for mock (simulates binary available)
func (m *MockMonitor) HasBinary() bool {
	return true
}

// HasPreferredBinary always returns true for mock
func (m *MockMonitor) HasPreferredBinary() bool {
	return true
}

// GetDevices returns the mock devices
func (m *MockMonitor) GetDevices() []types.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	devices := make([]types.Device, len(m.devices))
	copy(devices, m.devices)

	return devices
}

// UpdateDeviceStatus updates the enabled status of a device
func (m *MockMonitor) UpdateDeviceStatus(deviceID int, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	for i := range m.devices {
		if m.devices[i].ID == deviceID {
			m.devices[i].Enabled = enabled
			found = true
			debug.Info("Mock monitor: updated device %d enabled status to %v", deviceID, enabled)
			break
		}
	}

	if !found {
		return fmt.Errorf("device with ID %d not found", deviceID)
	}

	return nil
}

// GetEnabledDeviceFlags returns device flags for enabled devices
func (m *MockMonitor) GetEnabledDeviceFlags() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var enabledIDs []string
	for _, device := range m.devices {
		if device.Enabled {
			enabledIDs = append(enabledIDs, strconv.Itoa(device.ID))
		}
	}

	if len(enabledIDs) == 0 {
		return ""
	}

	return strings.Join(enabledIDs, ",")
}

// HasEnabledDevices returns true if any devices are enabled
func (m *MockMonitor) HasEnabledDevices() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, device := range m.devices {
		if device.Enabled {
			return true
		}
	}
	return false
}

// Helper functions to load configuration from environment

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
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

func getEnvString(key string, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
