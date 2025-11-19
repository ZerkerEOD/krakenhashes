package hardware

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	
	"github.com/ZerkerEOD/krakenhashes/agent/internal/hardware/types"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// HashcatDetector detects devices using hashcat -I command
type HashcatDetector struct {
	dataDirectory string
}

// NewHashcatDetector creates a new hashcat-based device detector
func NewHashcatDetector(dataDirectory string) *HashcatDetector {
	return &HashcatDetector{
		dataDirectory: dataDirectory,
	}
}

// DetectDevices detects all available compute devices using hashcat (DEPRECATED - use DetectPhysicalDevices)
// If preferredVersion is > 0, it will use that specific binary version instead of the latest
func (d *HashcatDetector) DetectDevices(preferredVersion ...int64) (*types.DeviceDetectionResult, error) {
	debug.Info("Starting hashcat device detection")

	var binaryPath string
	var binaryVersion string
	var err error

	// Check if a preferred version was specified
	if len(preferredVersion) > 0 && preferredVersion[0] > 0 {
		binaryPath, err = d.findSpecificHashcatBinary(preferredVersion[0])
		if err != nil {
			debug.Warning("Failed to find preferred binary version %d: %v, falling back to latest", preferredVersion[0], err)
			binaryPath, err = d.findLatestHashcatBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to find hashcat binary: %w", err)
			}
			binaryVersion = "latest (preferred not found)"
		} else {
			binaryVersion = fmt.Sprintf("%d (preferred)", preferredVersion[0])
			debug.Info("Using preferred hashcat binary version %d", preferredVersion[0])
		}
	} else {
		// Find the most recent hashcat binary
		binaryPath, err = d.findLatestHashcatBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to find hashcat binary: %w", err)
		}
		binaryVersion = "latest (no preference set)"
	}

	debug.Info("Using hashcat binary version %s: %s", binaryVersion, binaryPath)

	// Run hashcat -I command
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "-I")
	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(binaryPath)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Log any errors as warnings - hashcat may return non-zero exit codes for warnings
	if err != nil {
		debug.Warning("hashcat -I returned error (may be just warnings): %v", err)
		// Continue to try parsing the output even if there was an error
	}

	// Log the output regardless of error status
	debug.Info("Raw hashcat -I output:\n%s", outputStr)

	// Parse the output
	devices, parseErr := d.ParseHashcatOutput(outputStr)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse hashcat output: %w", parseErr)
	}

	// Only fail if we got an error AND no devices were found
	if len(devices) == 0 && err != nil {
		return nil, fmt.Errorf("failed to detect devices: hashcat returned error and no devices found: %w", err)
	}

	debug.Info("Parsed %d devices before filtering", len(devices))

	// Filter out aliases
	filteredDevices := d.FilterAliases(devices)

	debug.Info("Detected %d devices (filtered from %d total)", len(filteredDevices), len(devices))

	return &types.DeviceDetectionResult{
		Devices: filteredDevices,
	}, nil
}

// DetectPhysicalDevices detects all available compute devices and groups them by physical GPU
// If preferredVersion is > 0, it will use that specific binary version instead of the latest
func (d *HashcatDetector) DetectPhysicalDevices(preferredVersion ...int64) (*types.PhysicalDeviceDetectionResult, error) {
	debug.Info("Starting hashcat physical device detection")

	var binaryPath string
	var binaryVersion string
	var err error

	// Check if a preferred version was specified
	if len(preferredVersion) > 0 && preferredVersion[0] > 0 {
		binaryPath, err = d.findSpecificHashcatBinary(preferredVersion[0])
		if err != nil {
			debug.Warning("Failed to find preferred binary version %d: %v, falling back to latest", preferredVersion[0], err)
			binaryPath, err = d.findLatestHashcatBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to find hashcat binary: %w", err)
			}
			binaryVersion = "latest (preferred not found)"
		} else {
			binaryVersion = fmt.Sprintf("%d (preferred)", preferredVersion[0])
			debug.Info("Using preferred hashcat binary version %d", preferredVersion[0])
		}
	} else {
		// Find the most recent hashcat binary
		binaryPath, err = d.findLatestHashcatBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to find hashcat binary: %w", err)
		}
		binaryVersion = "latest (no preference set)"
	}

	debug.Info("Using hashcat binary version %s: %s", binaryVersion, binaryPath)

	// Run hashcat -I command
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "-I")
	// Set working directory to the hashcat binary directory so it can find relative dependencies like OpenCL
	cmd.Dir = filepath.Dir(binaryPath)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Log any errors as warnings - hashcat may return non-zero exit codes for warnings
	if err != nil {
		debug.Warning("hashcat -I returned error (may be just warnings): %v", err)
		// Continue to try parsing the output even if there was an error
	}

	// Log the output regardless of error status
	debug.Info("Raw hashcat -I output:\n%s", outputStr)

	// Parse the output
	devices, parseErr := d.ParseHashcatOutput(outputStr)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse hashcat output: %w", parseErr)
	}

	// Only fail if we got an error AND no devices were found
	if len(devices) == 0 && err != nil {
		return nil, fmt.Errorf("failed to detect devices: hashcat returned error and no devices found: %w", err)
	}

	debug.Info("Parsed %d individual devices before grouping", len(devices))

	// Group devices by physical GPU
	physicalDevices := d.GroupPhysicalDevices(devices)

	debug.Info("Detected %d physical devices (grouped from %d total)", len(physicalDevices), len(devices))

	return &types.PhysicalDeviceDetectionResult{
		Devices: physicalDevices,
	}, nil
}

// findLatestHashcatBinary finds the most recent hashcat binary in the binaries directory
func (d *HashcatDetector) findLatestHashcatBinary() (string, error) {
	binariesDir := filepath.Join(d.dataDirectory, "binaries")
	
	// Look for the latest version directory
	entries, err := os.ReadDir(binariesDir)
	if err != nil {
		return "", fmt.Errorf("failed to read binaries directory: %w", err)
	}
	
	var latestVersion int
	var latestDir string
	
	for _, entry := range entries {
		if entry.IsDir() {
			// Try to parse directory name as version number
			version, err := strconv.Atoi(entry.Name())
			if err == nil && version > latestVersion {
				latestVersion = version
				latestDir = entry.Name()
			}
		}
	}
	
	if latestDir == "" {
		return "", fmt.Errorf("no hashcat binary versions found")
	}
	
	// Determine binary extension based on OS
	var binaryName string
	if runtime.GOOS == "windows" {
		binaryName = "hashcat.exe"
	} else {
		binaryName = "hashcat.bin"
	}
	
	binaryPath := filepath.Join(binariesDir, latestDir, binaryName)
	
	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("hashcat binary not found at %s", binaryPath)
	}
	
	return binaryPath, nil
}

// findSpecificHashcatBinary finds a specific hashcat binary version
func (d *HashcatDetector) findSpecificHashcatBinary(version int64) (string, error) {
	binariesDir := filepath.Join(d.dataDirectory, "binaries")

	// Construct path to the specific version directory
	versionDir := filepath.Join(binariesDir, fmt.Sprintf("%d", version))

	// Check if version directory exists
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return "", fmt.Errorf("binary version %d directory not found", version)
	}

	// Determine binary extension based on OS
	var binaryName string
	if runtime.GOOS == "windows" {
		binaryName = "hashcat.exe"
	} else {
		binaryName = "hashcat.bin"
	}

	binaryPath := filepath.Join(versionDir, binaryName)

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("hashcat binary not found at %s", binaryPath)
	}

	return binaryPath, nil
}

// HasHashcatBinary checks if any hashcat binary is available
func (d *HashcatDetector) HasHashcatBinary() bool {
	_, err := d.findLatestHashcatBinary()
	return err == nil
}

// HasSpecificBinary checks if a specific hashcat binary version is available
func (d *HashcatDetector) HasSpecificBinary(version int64) bool {
	_, err := d.findSpecificHashcatBinary(version)
	return err == nil
}

// ParseHashcatOutput parses the output of hashcat -I command (exported for testing)
func (d *HashcatDetector) ParseHashcatOutput(output string) ([]types.Device, error) {
	var devices []types.Device
	aliasMap := make(map[int]int) // Maps device ID to its alias ID
	scanner := bufio.NewScanner(strings.NewReader(output))
	
	var currentDevice *types.Device
	var currentBackend string
	
	// Regular expressions for parsing
	backendRe := regexp.MustCompile(`^(HIP|OpenCL|CUDA) Info:`)
	platformRe := regexp.MustCompile(`^\s*(OpenCL|CUDA|HIP) Platform ID #\d+`)
	deviceIDRe := regexp.MustCompile(`^\s*Backend Device ID #(\d+)(?:\s+\(Alias:\s+#(\d+)\))?`)
	nameRe := regexp.MustCompile(`^\s*Name\.+:\s+(.+)`)
	typeRe := regexp.MustCompile(`^\s*Type\.+:\s+(.+)`)
	processorsRe := regexp.MustCompile(`^\s*Processor\(s\)\.+:\s+(\d+)`)
	clockRe := regexp.MustCompile(`^\s*Clock\.+:\s+(\d+)`)
	memoryTotalRe := regexp.MustCompile(`^\s*Memory\.Total\.+:\s+(\d+)\s+MB`)
	memoryFreeRe := regexp.MustCompile(`^\s*Memory\.Free\.+:\s+(\d+)\s+MB`)
	pciAddrRe := regexp.MustCompile(`^\s*PCI\.Addr\.(BDF|BDFe)\.+:\s+(.+)`)
	
	// First pass: collect alias information
	tempScanner := bufio.NewScanner(strings.NewReader(output))
	for tempScanner.Scan() {
		line := tempScanner.Text()
		if matches := deviceIDRe.FindStringSubmatch(line); matches != nil {
			if len(matches) > 2 && matches[2] != "" {
				deviceID, _ := strconv.Atoi(matches[1])
				aliasID, _ := strconv.Atoi(matches[2])
				aliasMap[deviceID] = aliasID
				debug.Info("Device #%d declares #%d as its alias", deviceID, aliasID)
			}
		}
	}
	
	// Parse all devices - we'll handle alias filtering later
	inPlatformSection := false
	for scanner.Scan() {
		line := scanner.Text()
		
		// Check for backend section
		if matches := backendRe.FindStringSubmatch(line); matches != nil {
			currentBackend = matches[1]
			inPlatformSection = false
			continue
		}
		
		// Check for platform section (we want to skip platform entries)
		if platformRe.MatchString(line) {
			inPlatformSection = true
			// If we were parsing a device, save it
			if currentDevice != nil {
				devices = append(devices, *currentDevice)
				currentDevice = nil
			}
			continue
		}
		
		// Check for device ID
		if matches := deviceIDRe.FindStringSubmatch(line); matches != nil {
			inPlatformSection = false
			// Save previous device if any
			if currentDevice != nil {
				devices = append(devices, *currentDevice)
			}
			
			// Start new device
			deviceID, _ := strconv.Atoi(matches[1])
			currentDevice = &types.Device{
				ID:       deviceID,
				Backend:  currentBackend,
				Type:     "GPU", // Default to GPU type
				Enabled:  true, // Default to enabled
				IsAlias:  false, // We'll set this based on alias map
			}
			
			// Store alias information with the device
			if aliasID, hasAlias := aliasMap[deviceID]; hasAlias {
				currentDevice.AliasOf = aliasID
			}
			
			continue
		}
		
		// Parse device properties (but skip if we're in a platform section)
		if currentDevice != nil && !inPlatformSection {
			if matches := nameRe.FindStringSubmatch(line); matches != nil {
				currentDevice.Name = strings.TrimSpace(matches[1])
			} else if matches := typeRe.FindStringSubmatch(line); matches != nil {
				currentDevice.Type = strings.TrimSpace(matches[1])
			} else if matches := processorsRe.FindStringSubmatch(line); matches != nil {
				currentDevice.Processors, _ = strconv.Atoi(matches[1])
			} else if matches := clockRe.FindStringSubmatch(line); matches != nil {
				currentDevice.Clock, _ = strconv.Atoi(matches[1])
			} else if matches := memoryTotalRe.FindStringSubmatch(line); matches != nil {
				currentDevice.MemoryTotal, _ = strconv.ParseInt(matches[1], 10, 64)
			} else if matches := memoryFreeRe.FindStringSubmatch(line); matches != nil {
				currentDevice.MemoryFree, _ = strconv.ParseInt(matches[1], 10, 64)
			} else if matches := pciAddrRe.FindStringSubmatch(line); matches != nil {
				currentDevice.PCIAddress = strings.TrimSpace(matches[2])
			}
		}
	}
	
	// Don't forget the last device
	if currentDevice != nil {
		devices = append(devices, *currentDevice)
	}
	
	if len(devices) == 0 {
		return nil, fmt.Errorf("no devices found in hashcat output")
	}
	
	return devices, nil
}

// GroupPhysicalDevices groups devices by physical GPU, creating one entry per physical device
// with multiple runtime options (exported for testing)
func (d *HashcatDetector) GroupPhysicalDevices(devices []types.Device) []types.PhysicalDevice {
	// Backend priority for auto-selection: CUDA > HIP > OpenCL
	backendPriority := map[string]int{
		"CUDA":   3,
		"HIP":    2,
		"OpenCL": 1,
	}

	// Separate devices by backend
	devicesByBackend := make(map[string][]types.Device)
	for _, device := range devices {
		if device.Backend != "" && device.Name != "" {
			devicesByBackend[device.Backend] = append(devicesByBackend[device.Backend], device)
		}
	}

	// Sort devices within each backend by device ID
	for backend := range devicesByBackend {
		sort.Slice(devicesByBackend[backend], func(i, j int) bool {
			return devicesByBackend[backend][i].ID < devicesByBackend[backend][j].ID
		})
	}

	// Build alias map for newer hashcat versions
	aliasMap := make(map[int]int) // device ID -> alias ID
	hasAnyAliases := false
	for _, device := range devices {
		if device.AliasOf > 0 {
			aliasMap[device.ID] = device.AliasOf
			hasAnyAliases = true
			debug.Info("Device #%d has alias #%d", device.ID, device.AliasOf)
		}
	}

	var physicalDevices []types.PhysicalDevice

	if hasAnyAliases {
		// Use alias-based grouping for versions 6.2.6+
		debug.Info("Using alias-based device grouping")
		physicalDevices = d.groupByAliases(devices, aliasMap, backendPriority)
	} else {
		// Use positional matching for older versions
		debug.Info("Using positional device grouping (no aliases detected)")
		physicalDevices = d.groupByPosition(devicesByBackend, backendPriority)
	}

	debug.Info("Grouped %d devices into %d physical devices", len(devices), len(physicalDevices))
	return physicalDevices
}

// groupByAliases groups devices using explicit alias relationships (hashcat 6.2.6+)
func (d *HashcatDetector) groupByAliases(devices []types.Device, aliasMap map[int]int, backendPriority map[string]int) []types.PhysicalDevice {
	// Build map of device ID -> device
	deviceMap := make(map[int]types.Device)
	for _, dev := range devices {
		deviceMap[dev.ID] = dev
	}

	// Track which devices we've already grouped
	processed := make(map[int]bool)
	var physicalDevices []types.PhysicalDevice

	// Group devices by their alias relationships
	for _, device := range devices {
		if processed[device.ID] {
			continue
		}

		// Start a new physical device
		physicalDev := types.PhysicalDevice{
			Index:          len(physicalDevices),
			Name:           device.Name,
			Type:           device.Type,
			Enabled:        true,
			RuntimeOptions: []types.RuntimeOption{},
		}

		// Add this device as a runtime option
		physicalDev.RuntimeOptions = append(physicalDev.RuntimeOptions, types.RuntimeOption{
			Backend:     device.Backend,
			DeviceID:    device.ID,
			Processors:  device.Processors,
			Clock:       device.Clock,
			MemoryTotal: device.MemoryTotal,
			MemoryFree:  device.MemoryFree,
			PCIAddress:  device.PCIAddress,
		})
		processed[device.ID] = true

		// Find aliased devices
		if aliasID, hasAlias := aliasMap[device.ID]; hasAlias {
			if aliasDevice, exists := deviceMap[aliasID]; exists && !processed[aliasID] {
				physicalDev.RuntimeOptions = append(physicalDev.RuntimeOptions, types.RuntimeOption{
					Backend:     aliasDevice.Backend,
					DeviceID:    aliasDevice.ID,
					Processors:  aliasDevice.Processors,
					Clock:       aliasDevice.Clock,
					MemoryTotal: aliasDevice.MemoryTotal,
					MemoryFree:  aliasDevice.MemoryFree,
					PCIAddress:  aliasDevice.PCIAddress,
				})
				processed[aliasID] = true
				debug.Info("Grouped device #%d (%s) with alias #%d (%s) as physical device %d",
					device.ID, device.Backend, aliasID, aliasDevice.Backend, physicalDev.Index)
			}
		}

		// Select highest priority runtime
		physicalDev.SelectedRuntime = d.selectDefaultRuntime(physicalDev.RuntimeOptions, backendPriority)
		physicalDevices = append(physicalDevices, physicalDev)
	}

	return physicalDevices
}

// groupByPosition groups devices using positional matching (hashcat < 6.2.6)
func (d *HashcatDetector) groupByPosition(devicesByBackend map[string][]types.Device, backendPriority map[string]int) []types.PhysicalDevice {
	// Find the backend with the most devices (use as reference)
	var backends []string
	maxDevices := 0
	var referenceBackend string

	for backend, devs := range devicesByBackend {
		backends = append(backends, backend)
		if len(devs) > maxDevices {
			maxDevices = len(devs)
			referenceBackend = backend
		}
	}

	if referenceBackend == "" {
		debug.Warning("No devices found in any backend")
		return []types.PhysicalDevice{}
	}

	debug.Info("Using %s as reference backend with %d devices", referenceBackend, maxDevices)

	// Create physical devices based on reference backend positions
	var physicalDevices []types.PhysicalDevice
	for i := 0; i < maxDevices; i++ {
		refDevice := devicesByBackend[referenceBackend][i]
		physicalDev := types.PhysicalDevice{
			Index:          i,
			Name:           refDevice.Name,
			Type:           refDevice.Type,
			Enabled:        true,
			RuntimeOptions: []types.RuntimeOption{},
		}

		// Add runtime options from all backends at this position
		for _, backend := range backends {
			backendDevices := devicesByBackend[backend]
			if i < len(backendDevices) {
				device := backendDevices[i]
				physicalDev.RuntimeOptions = append(physicalDev.RuntimeOptions, types.RuntimeOption{
					Backend:     device.Backend,
					DeviceID:    device.ID,
					Processors:  device.Processors,
					Clock:       device.Clock,
					MemoryTotal: device.MemoryTotal,
					MemoryFree:  device.MemoryFree,
					PCIAddress:  device.PCIAddress,
				})

				debug.Info("Matched %s device #%d at position %d for physical device %d (%s)",
					backend, device.ID, i, physicalDev.Index, device.Name)
			}
		}

		// Select highest priority runtime
		physicalDev.SelectedRuntime = d.selectDefaultRuntime(physicalDev.RuntimeOptions, backendPriority)
		physicalDevices = append(physicalDevices, physicalDev)
	}

	return physicalDevices
}

// selectDefaultRuntime chooses the highest priority runtime from available options
func (d *HashcatDetector) selectDefaultRuntime(options []types.RuntimeOption, backendPriority map[string]int) string {
	if len(options) == 0 {
		return ""
	}

	bestRuntime := options[0].Backend
	bestPriority := backendPriority[bestRuntime]

	for _, opt := range options {
		if priority := backendPriority[opt.Backend]; priority > bestPriority {
			bestRuntime = opt.Backend
			bestPriority = priority
		}
	}

	debug.Info("Selected %s as default runtime (priority: %d)", bestRuntime, bestPriority)
	return bestRuntime
}

// FilterAliases removes aliased devices from the list using hashcat's alias information (DEPRECATED - use GroupPhysicalDevices)
func (d *HashcatDetector) FilterAliases(devices []types.Device) []types.Device {
	// Build a map of device ID to device for easy lookup
	deviceMap := make(map[int]*types.Device)
	for i := range devices {
		deviceMap[devices[i].ID] = &devices[i]
	}

	// For devices with circular aliases, we need to determine which to keep
	// Priority: HIP > CUDA > OpenCL
	backendPriority := map[string]int{
		"HIP":    3,
		"CUDA":   2,
		"OpenCL": 1,
	}

	// Track which devices to keep
	keepDevice := make(map[int]bool)
	processedPairs := make(map[string]bool)

	for _, device := range devices {
		if device.AliasOf > 0 {
			// Check if we've already processed this pair
			pairKey := fmt.Sprintf("%d-%d", min(device.ID, device.AliasOf), max(device.ID, device.AliasOf))
			if processedPairs[pairKey] {
				continue
			}
			processedPairs[pairKey] = true

			// Get the aliased device
			aliasDevice, exists := deviceMap[device.AliasOf]
			if !exists {
				// If alias doesn't exist, keep this device
				keepDevice[device.ID] = true
				continue
			}

			// Check if it's a circular alias
			if aliasDevice.AliasOf == device.ID {
				debug.Info("Circular alias detected: #%d (%s) <-> #%d (%s)",
					device.ID, device.Backend, aliasDevice.ID, aliasDevice.Backend)

				// Choose based on backend priority
				devicePriority := backendPriority[device.Backend]
				aliasPriority := backendPriority[aliasDevice.Backend]

				if devicePriority > aliasPriority {
					keepDevice[device.ID] = true
					debug.Info("Keeping device #%d (%s) over #%d (%s) based on backend priority",
						device.ID, device.Backend, aliasDevice.ID, aliasDevice.Backend)
				} else if aliasPriority > devicePriority {
					keepDevice[aliasDevice.ID] = true
					debug.Info("Keeping device #%d (%s) over #%d (%s) based on backend priority",
						aliasDevice.ID, aliasDevice.Backend, device.ID, device.Backend)
				} else {
					// Same priority, keep the one with lower ID
					if device.ID < aliasDevice.ID {
						keepDevice[device.ID] = true
					} else {
						keepDevice[aliasDevice.ID] = true
					}
				}
			} else {
				// Not circular, this device's alias should be filtered
				keepDevice[device.ID] = true
				debug.Info("Device #%d declares #%d as alias (not circular)",
					device.ID, device.AliasOf)
			}
		} else {
			// No alias declared, keep it unless another device declares it as alias
			keepDevice[device.ID] = true
		}
	}

	// Build final filtered list
	var filtered []types.Device
	for _, device := range devices {
		// Skip if explicitly not to keep
		if keep, exists := keepDevice[device.ID]; exists && !keep {
			continue
		}

		// Check if another device declares this as its alias (and we're keeping that device)
		isAlias := false
		for _, otherDevice := range devices {
			if otherDevice.AliasOf == device.ID && keepDevice[otherDevice.ID] {
				isAlias = true
				debug.Info("Filtering out device #%d (%s, %s backend) - it's declared as alias by #%d",
					device.ID, device.Name, device.Backend, otherDevice.ID)
				break
			}
		}

		if isAlias {
			continue
		}

		// Also skip devices with no name or zero processors
		if device.Name == "" || device.Processors == 0 {
			debug.Info("Skipping invalid device #%d: name='%s', processors=%d",
				device.ID, device.Name, device.Processors)
			continue
		}

		filtered = append(filtered, device)
		debug.Info("Keeping device #%d: %s (%s backend)", device.ID, device.Name, device.Backend)
	}

	// Sort by device ID for consistent ordering
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	return filtered
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BuildDeviceFlags builds the -d flag for hashcat based on enabled devices (DEPRECATED - use BuildDeviceFlagsFromPhysical)
func BuildDeviceFlags(devices []types.Device) string {
	var enabledIDs []string
	allEnabled := true

	for _, device := range devices {
		if device.Enabled {
			enabledIDs = append(enabledIDs, strconv.Itoa(device.ID))
		} else {
			allEnabled = false
		}
	}

	// If all devices are enabled, no need for -d flag
	if allEnabled || len(enabledIDs) == len(devices) {
		return ""
	}

	// If no devices are enabled, this is an error condition
	if len(enabledIDs) == 0 {
		return ""
	}

	// Return comma-separated list of enabled device IDs
	return strings.Join(enabledIDs, ",")
}

// BuildDeviceFlagsFromPhysical builds the -d flag for hashcat based on enabled physical devices
// It uses each device's selected runtime to determine the correct hashcat device ID
func BuildDeviceFlagsFromPhysical(devices []types.PhysicalDevice) string {
	var enabledIDs []string
	allEnabled := true

	for _, device := range devices {
		if device.Enabled {
			// Find the hashcat device ID for the selected runtime
			for _, opt := range device.RuntimeOptions {
				if opt.Backend == device.SelectedRuntime {
					enabledIDs = append(enabledIDs, strconv.Itoa(opt.DeviceID))
					break
				}
			}
		} else {
			allEnabled = false
		}
	}

	// If all devices are enabled, no need for -d flag
	if allEnabled || len(enabledIDs) == len(devices) {
		return ""
	}

	// If no devices are enabled, this is an error condition
	if len(enabledIDs) == 0 {
		return ""
	}

	// Return comma-separated list of enabled device IDs
	return strings.Join(enabledIDs, ",")
}