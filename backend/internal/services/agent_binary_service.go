package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// BinaryInfo contains information about an agent binary
type BinaryInfo struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Path        string `json:"path"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
	Version     string `json:"version"`
	DisplayName string `json:"display_name"`
	FileName    string `json:"file_name"`
	DownloadURL string `json:"download_url"`
}

// AgentBinaryService manages agent and launcher binaries
type AgentBinaryService struct {
	mu              sync.RWMutex
	binaryPath      string                // /usr/share/krakenhashes/agents
	launcherPath    string                // /usr/share/krakenhashes/launcher
	agentVersion    string                // From versions.json ("agent")
	launcherVersion string                // From versions.json ("launcher")
	checksums       map[string]string     // "os_arch" -> SHA-256 (agent)
	binaries        map[string]BinaryInfo // "os_arch" -> BinaryInfo (agent)
	launchers       map[string]BinaryInfo // "os_arch" -> BinaryInfo (launcher)
}

// NewAgentBinaryService creates an AgentBinaryService configured with root paths for agent and launcher binaries and initialized in-memory indexes.
// It prefers fixed container-installation paths (/usr/share/krakenhashes/agents and /usr/share/krakenhashes/launcher) and falls back to the provided dataDir (dataDir/agents and dataDir/launcher) when those fixed paths do not exist.
// The returned service has empty checksum, binaries, and launchers maps ready for population.
func NewAgentBinaryService(dataDir string) *AgentBinaryService {
	// Use a fixed path for agents that's not in the volume-mounted data directory
	// This ensures agents built into the Docker image are accessible
	binaryPath := "/usr/share/krakenhashes/agents"
	launcherPath := "/usr/share/krakenhashes/launcher"

	// In development (non-Docker), fall back to data directory
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		binaryPath = filepath.Join(dataDir, "agents")
	}
	if _, err := os.Stat(launcherPath); os.IsNotExist(err) {
		launcherPath = filepath.Join(dataDir, "launcher")
	}

	return &AgentBinaryService{
		binaryPath:   binaryPath,
		launcherPath: launcherPath,
		checksums:    make(map[string]string),
		binaries:     make(map[string]BinaryInfo),
		launchers:    make(map[string]BinaryInfo),
	}
}

// Initialize scans and indexes available binaries
func (s *AgentBinaryService) Initialize() error {
	debug.Info("Initializing agent binary service")

	// Read agent + launcher versions from versions.json
	if err := s.readAgentVersion(); err != nil {
		return fmt.Errorf("failed to read agent version: %w", err)
	}

	// Scan binary directory
	if err := s.scanBinaries(); err != nil {
		return fmt.Errorf("failed to scan binaries: %w", err)
	}

	// Scan launcher directory (absence is not an error — launcher binaries may
	// not be built/baked yet in development).
	if err := s.scanLaunchers(); err != nil {
		debug.Warning("failed to scan launcher binaries: %v", err)
	}

	debug.Info("Agent binary service initialized with agent version %s (%d binaries), launcher version %s (%d binaries)",
		s.agentVersion, len(s.binaries), s.launcherVersion, len(s.launchers))
	return nil
}

// readAgentVersion reads the agent version from versions.json
func (s *AgentBinaryService) readAgentVersion() error {
	// Try to read from the standard location in the container
	versionsPath := "/usr/local/share/krakenhashes/versions.json"

	// Fallback to local development path if not in container
	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
		versionsPath = "versions.json"
	}

	data, err := os.ReadFile(versionsPath)
	if err != nil {
		return fmt.Errorf("failed to read versions.json: %w", err)
	}

	var versions map[string]string
	if err := json.Unmarshal(data, &versions); err != nil {
		return fmt.Errorf("failed to parse versions.json: %w", err)
	}

	agentVersion, ok := versions["agent"]
	if !ok {
		return fmt.Errorf("agent version not found in versions.json")
	}

	s.agentVersion = agentVersion
	// Launcher version is optional (it may not be tracked yet); default to the
	// agent version so downloads still advertise something sensible.
	if launcherVersion, ok := versions["launcher"]; ok {
		s.launcherVersion = launcherVersion
	} else {
		s.launcherVersion = agentVersion
	}
	return nil
}

// scanLaunchers indexes launcher binaries under launcherPath/{os}/{arch}/.
func (s *AgentBinaryService) scanLaunchers() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.launcherPath); os.IsNotExist(err) {
		debug.Warning("Launcher binary directory does not exist: %s", s.launcherPath)
		return nil
	}

	osDirs, err := os.ReadDir(s.launcherPath)
	if err != nil {
		return fmt.Errorf("failed to read launcher directory: %w", err)
	}

	for _, osDir := range osDirs {
		if !osDir.IsDir() {
			continue
		}
		osName := osDir.Name()
		osPath := filepath.Join(s.launcherPath, osName)

		archDirs, err := os.ReadDir(osPath)
		if err != nil {
			debug.Error("Failed to read launcher OS directory %s: %v", osPath, err)
			continue
		}

		for _, archDir := range archDirs {
			if !archDir.IsDir() {
				continue
			}
			arch := archDir.Name()

			binaryName := "krakenhashes-launcher"
			if osName == "windows" {
				binaryName += ".exe"
			}

			binaryPath := filepath.Join(osPath, arch, binaryName)
			info, err := os.Stat(binaryPath)
			if err != nil {
				continue
			}
			checksum, err := s.calculateChecksum(binaryPath)
			if err != nil {
				debug.Error("Failed to calculate launcher checksum for %s: %v", binaryPath, err)
				continue
			}

			key := fmt.Sprintf("%s_%s", osName, arch)
			s.launchers[key] = BinaryInfo{
				OS:          osName,
				Arch:        arch,
				Path:        binaryPath,
				Checksum:    checksum,
				Size:        info.Size(),
				Version:     s.launcherVersion,
				DisplayName: s.getDisplayName(osName, arch),
				FileName:    binaryName,
				DownloadURL: fmt.Sprintf("/api/public/agent/launcher/download/%s/%s", osName, arch),
			}
			debug.Info("Indexed launcher: %s/%s (size: %d)", osName, arch, info.Size())
		}
	}

	return nil
}

// scanBinaries scans the binary directory and indexes available binaries
func (s *AgentBinaryService) scanBinaries() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if binary directory exists
	if _, err := os.Stat(s.binaryPath); os.IsNotExist(err) {
		debug.Warning("Agent binary directory does not exist: %s", s.binaryPath)
		return nil // Not an error in development
	}

	// Iterate through OS directories
	osDirs, err := os.ReadDir(s.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read binary directory: %w", err)
	}

	for _, osDir := range osDirs {
		if !osDir.IsDir() {
			continue
		}

		osName := osDir.Name()
		osPath := filepath.Join(s.binaryPath, osName)

		// Iterate through architecture directories
		archDirs, err := os.ReadDir(osPath)
		if err != nil {
			debug.Error("Failed to read OS directory %s: %v", osPath, err)
			continue
		}

		for _, archDir := range archDirs {
			if !archDir.IsDir() {
				continue
			}

			arch := archDir.Name()
			archPath := filepath.Join(osPath, arch)

			// Find the binary file
			binaryName := "krakenhashes-agent"
			if osName == "windows" {
				binaryName += ".exe"
			}

			binaryPath := filepath.Join(archPath, binaryName)
			if info, err := os.Stat(binaryPath); err == nil {
				// Calculate checksum
				checksum, err := s.calculateChecksum(binaryPath)
				if err != nil {
					debug.Error("Failed to calculate checksum for %s: %v", binaryPath, err)
					continue
				}

				key := fmt.Sprintf("%s_%s", osName, arch)
				s.checksums[key] = checksum
				s.binaries[key] = BinaryInfo{
					OS:          osName,
					Arch:        arch,
					Path:        binaryPath,
					Checksum:    checksum,
					Size:        info.Size(),
					Version:     s.agentVersion,
					DisplayName: s.getDisplayName(osName, arch),
					FileName:    binaryName,
					DownloadURL: fmt.Sprintf("/api/public/agent/download/%s/%s", osName, arch),
				}

				debug.Info("Indexed binary: %s/%s (size: %d, checksum: %s...)",
					osName, arch, info.Size(), checksum[:8])
			}
		}
	}

	return nil
}

// calculateChecksum calculates SHA-256 checksum of a file
func (s *AgentBinaryService) calculateChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// getDisplayName returns a user-friendly display name for the platform
func (s *AgentBinaryService) getDisplayName(os, arch string) string {
	displayNames := map[string]string{
		"linux_amd64":   "64-bit",
		"linux_386":     "32-bit",
		"linux_arm64":   "ARM64",
		"linux_arm":     "ARM",
		"windows_amd64": "64-bit",
		"windows_386":   "32-bit",
		"windows_arm64": "ARM64",
		"darwin_amd64":  "Intel",
		"darwin_arm64":  "Apple Silicon",
	}

	key := fmt.Sprintf("%s_%s", os, arch)
	if name, ok := displayNames[key]; ok {
		return name
	}
	return arch
}

// GetBinary returns information about a specific binary
func (s *AgentBinaryService) GetBinary(os, arch string) (*BinaryInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s_%s", os, arch)
	if binary, ok := s.binaries[key]; ok {
		return &binary, nil
	}

	return nil, fmt.Errorf("binary not found for %s/%s", os, arch)
}

// GetAllBinaries returns information about all available binaries
func (s *AgentBinaryService) GetAllBinaries() []BinaryInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Initialize with empty slice instead of nil to ensure JSON encoding returns [] not null
	result := make([]BinaryInfo, 0)
	for _, binary := range s.binaries {
		result = append(result, binary)
	}
	return result
}

// GetVersion returns the current agent version
func (s *AgentBinaryService) GetVersion() string {
	return s.agentVersion
}

// GetLauncherVersion returns the current launcher version.
func (s *AgentBinaryService) GetLauncherVersion() string {
	return s.launcherVersion
}

// GetLauncherBinary returns info about a specific launcher binary.
func (s *AgentBinaryService) GetLauncherBinary(os, arch string) (*BinaryInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s_%s", os, arch)
	if binary, ok := s.launchers[key]; ok {
		return &binary, nil
	}
	return nil, fmt.Errorf("launcher binary not found for %s/%s", os, arch)
}

// GetAllLauncherBinaries returns info about all available launcher binaries.
func (s *AgentBinaryService) GetAllLauncherBinaries() []BinaryInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]BinaryInfo, 0)
	for _, binary := range s.launchers {
		result = append(result, binary)
	}
	return result
}

// GetChecksums returns all checksums
func (s *AgentBinaryService) GetChecksums() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for k, v := range s.checksums {
		result[k] = v
	}
	return result
}

// NeedsUpdate checks if an agent needs an update
func (s *AgentBinaryService) NeedsUpdate(currentVersion string) bool {
	return CompareVersions(currentVersion, s.agentVersion) < 0
}

// CompareVersions compares two semantic versions
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func CompareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	// Simple string comparison for now (works for x.y.z format)
	// TODO: Implement proper semver comparison
	if v1 < v2 {
		return -1
	} else if v1 > v2 {
		return 1
	}
	return 0
}

// GetBinaryPath returns the full path to a binary file
func (s *AgentBinaryService) GetBinaryPath(os, arch string) (string, error) {
	binary, err := s.GetBinary(os, arch)
	if err != nil {
		return "", err
	}
	return binary.Path, nil
}

// GetPlatformBinary returns binary info for the current platform (for testing)
func (s *AgentBinaryService) GetPlatformBinary() (*BinaryInfo, error) {
	return s.GetBinary(runtime.GOOS, runtime.GOARCH)
}