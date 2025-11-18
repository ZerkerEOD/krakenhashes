package hardware

import (
	"fmt"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/hardware/types"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// Monitor manages hardware monitoring
type Monitor struct {
	mu                     sync.RWMutex
	devices                []types.Device
	hashcatDetector        *HashcatDetector
	dataDirectory          string
	preferredBinaryVersion int64
}

// NewMonitor creates a new hardware monitor
func NewMonitor(dataDirectory string) (*Monitor, error) {
	m := &Monitor{
		hashcatDetector: NewHashcatDetector(dataDirectory),
		dataDirectory:   dataDirectory,
		devices:         []types.Device{},
	}

	return m, nil
}

// NewMonitorFromMock creates a Monitor wrapper around a MockMonitor
// This allows mock monitors to be used where *Monitor is expected
func NewMonitorFromMock(mock *MockMonitor) *Monitor {
	// Get mock devices
	mockDevices := mock.GetDevices()

	return &Monitor{
		hashcatDetector:        nil, // No real detector in mock mode
		dataDirectory:          "",
		devices:                mockDevices,
		preferredBinaryVersion: 0,
	}
}

// SetPreferredBinaryVersion sets the preferred binary version for device detection
func (m *Monitor) SetPreferredBinaryVersion(version int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preferredBinaryVersion = version
}

// Cleanup releases monitor resources
func (m *Monitor) Cleanup() error {
	debug.Info("Cleaning up hardware monitor")
	m.mu.Lock()
	defer m.mu.Unlock()

	// Nothing to cleanup anymore
	return nil
}

// DetectDevices uses hashcat to detect available compute devices
func (m *Monitor) DetectDevices() (*types.DeviceDetectionResult, error) {
	// If no detector (mock mode), return mock devices
	if m.hashcatDetector == nil {
		m.mu.RLock()
		devices := make([]types.Device, len(m.devices))
		copy(devices, m.devices)
		m.mu.RUnlock()

		result := &types.DeviceDetectionResult{
			Devices: devices,
		}
		return result, nil
	}

	// Get preferred binary version
	m.mu.RLock()
	preferredVersion := m.preferredBinaryVersion
	m.mu.RUnlock()

	// Pass preferred version to detector
	var result *types.DeviceDetectionResult
	var err error
	if preferredVersion > 0 {
		result, err = m.hashcatDetector.DetectDevices(preferredVersion)
	} else {
		result, err = m.hashcatDetector.DetectDevices()
	}

	if err != nil {
		return nil, err
	}

	// Store devices in monitor
	m.mu.Lock()
	m.devices = result.Devices
	m.mu.Unlock()

	return result, nil
}

// DetectPhysicalDevices detects and groups devices by physical GPU
func (m *Monitor) DetectPhysicalDevices() (*types.PhysicalDeviceDetectionResult, error) {
	// If no detector (mock mode), return mock physical devices
	if m.hashcatDetector == nil {
		m.mu.RLock()
		deviceCount := len(m.devices)
		m.mu.RUnlock()

		// Create mock physical devices (one per GPU)
		physicalDevices := make([]types.PhysicalDevice, deviceCount)
		for i := 0; i < deviceCount; i++ {
			m.mu.RLock()
			device := m.devices[i]
			m.mu.RUnlock()

			runtimes := []types.RuntimeOption{
				{
					Backend:     device.Backend,
					DeviceID:    device.ID,
					MemoryTotal: device.MemoryTotal,
				},
			}
			physicalDevices[i] = types.PhysicalDevice{
				Index:           i,
				Name:            device.Name,
				Type:            device.Type,
				Enabled:         device.Enabled,
				RuntimeOptions:  runtimes,
				SelectedRuntime: device.Backend,
			}
		}

		return &types.PhysicalDeviceDetectionResult{
			Devices: physicalDevices,
		}, nil
	}

	// Get preferred binary version
	m.mu.RLock()
	preferredVersion := m.preferredBinaryVersion
	m.mu.RUnlock()

	// Pass preferred version to detector
	var result *types.PhysicalDeviceDetectionResult
	var err error
	if preferredVersion > 0 {
		result, err = m.hashcatDetector.DetectPhysicalDevices(preferredVersion)
	} else {
		result, err = m.hashcatDetector.DetectPhysicalDevices()
	}

	if err != nil {
		return nil, err
	}

	// Note: We don't store physical devices in the monitor's devices field
	// since they have a different structure. The monitor still uses the
	// old devices field for backward compatibility with existing code.

	return result, nil
}

// HasBinary checks if any hashcat binary is available
func (m *Monitor) HasBinary() bool {
	if m.hashcatDetector == nil {
		return true // Mock mode always has "binary"
	}
	return m.hashcatDetector.HasHashcatBinary()
}

// HasPreferredBinary checks if the preferred binary version is available
func (m *Monitor) HasPreferredBinary() bool {
	if m.hashcatDetector == nil {
		return true // Mock mode always has "binary"
	}

	m.mu.RLock()
	preferredVersion := m.preferredBinaryVersion
	m.mu.RUnlock()

	// If no preferred version set, check for any binary
	if preferredVersion == 0 {
		return m.HasBinary()
	}

	// Check if the specific preferred version exists
	return m.hashcatDetector.HasSpecificBinary(preferredVersion)
}

// GetDevices returns the currently detected devices
func (m *Monitor) GetDevices() []types.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Return a copy to prevent concurrent modification
	devices := make([]types.Device, len(m.devices))
	copy(devices, m.devices)
	
	return devices
}

// UpdateDeviceStatus updates the enabled status of a device
func (m *Monitor) UpdateDeviceStatus(deviceID int, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	found := false
	for i := range m.devices {
		if m.devices[i].ID == deviceID {
			m.devices[i].Enabled = enabled
			found = true
			break
		}
	}
	
	if !found {
		return fmt.Errorf("device with ID %d not found", deviceID)
	}
	
	return nil
}

// GetEnabledDeviceFlags returns the -d flag value for hashcat based on enabled devices
func (m *Monitor) GetEnabledDeviceFlags() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return BuildDeviceFlags(m.devices)
}

// HasEnabledDevices returns true if at least one device is enabled
func (m *Monitor) HasEnabledDevices() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, device := range m.devices {
		if device.Enabled {
			return true
		}
	}
	
	return false
}
