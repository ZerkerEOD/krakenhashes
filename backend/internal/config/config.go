package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/env"
)

// Config holds the application configuration
type Config struct {
	Host          string
	HTTPPort      int    // Port for HTTP (CA certificate)
	HTTPSPort     int    // Port for HTTPS (API)
	ConfigDir     string // Base directory for config files (certs, etc.)
	DataDir       string // Base directory for mutable data (uploads, db?)
	MaxUploadSize int64  // Max size for file uploads in bytes
	HashUploadDir string // Directory within DataDir to store hashlist uploads
}

// NewConfig creates a new Config instance with values from environment variables
func NewConfig() *Config {
	httpsPort := 31337 // Default HTTPS port
	if portStr := os.Getenv("KH_HTTPS_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			httpsPort = p
		}
	}

	httpPort := 1337 // Default HTTP port
	if portStr := os.Getenv("KH_HTTP_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			httpPort = p
		}
	}

	host := os.Getenv("KH_HOST")
	if host == "" {
		// In Docker, bind to all interfaces
		if env.GetBool("KH_IN_DOCKER") {
			host = "0.0.0.0"
		} else {
			host = "localhost" // Default host for local development
		}
	}

	// Get config directory from environment or use default
	configDir := os.Getenv("KH_CONFIG_DIR")
	if configDir == "" {
		// Try to get user's home directory
		home, err := os.UserHomeDir()
		if err != nil {
			// Fallback to current directory
			configDir = ".krakenhashes"
		} else {
			configDir = filepath.Join(home, ".krakenhashes")
		}
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		debug.Error("Failed to create config directory: %v", err)
		// Fallback to current directory
		configDir = ".krakenhashes"
		if err := os.MkdirAll(configDir, 0755); err != nil {
			debug.Error("Failed to create fallback config directory: %v", err)
		}
	}

	debug.Info("Using config directory: %s", configDir)

	// Get data directory from environment or use default
	dataDir := os.Getenv("KH_DATA_DIR")
	if dataDir == "" {
		// Try to get user's home directory
		home, err := os.UserHomeDir()
		if err != nil {
			// Fallback to current directory
			dataDir = ".krakenhashes-data"
		} else {
			dataDir = filepath.Join(home, ".krakenhashes-data")
		}
	}

	// Convert to absolute path if it's relative
	if !filepath.IsAbs(dataDir) {
		absDataDir, err := filepath.Abs(dataDir)
		if err != nil {
			debug.Error("Failed to convert data directory to absolute path: %v", err)
		} else {
			debug.Info("Converting relative data directory '%s' to absolute path '%s'", dataDir, absDataDir)
			dataDir = absDataDir
		}
	}

	// Create data directory and its subdirectories if they don't exist
	subdirs := []string{"binaries", "wordlists", "rules", "hashlists"}
	for _, subdir := range subdirs {
		dir := filepath.Join(dataDir, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create data subdirectory %s: %v", subdir, err)
			// Fallback to current directory
			dataDir = ".krakenhashes-data"
			dir = filepath.Join(dataDir, subdir)
			if err := os.MkdirAll(dir, 0750); err != nil {
				debug.Error("Failed to create fallback data subdirectory %s: %v", subdir, err)
			}
		}
	}

	// Create subdirectories for wordlists and rules
	wordlistSubdirs := []string{
		filepath.Join("wordlists", "general"),
		filepath.Join("wordlists", "specialized"),
		filepath.Join("wordlists", "targeted"),
		filepath.Join("wordlists", "custom"),
	}

	ruleSubdirs := []string{
		filepath.Join("rules", "hashcat"),
		filepath.Join("rules", "john"),
		filepath.Join("rules", "custom"),
	}

	// Create all wordlist subdirectories
	for _, subdir := range wordlistSubdirs {
		dir := filepath.Join(dataDir, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create wordlist subdirectory %s: %v", subdir, err)
		} else {
			debug.Debug("Created wordlist subdirectory: %s", dir)
		}
	}

	// Create all rule subdirectories
	for _, subdir := range ruleSubdirs {
		dir := filepath.Join(dataDir, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create rule subdirectory %s: %v", subdir, err)
		} else {
			debug.Debug("Created rule subdirectory: %s", dir)
		}
	}

	debug.Info("Using data directory: %s", dataDir)

	// Get Max Upload Size
	maxUploadSize := int64(32 << 20) // Default 32 MiB
	if sizeStr := env.GetOrDefault("KH_MAX_UPLOAD_SIZE_MB", "32"); sizeStr != "" {
		if sizeMB, err := strconv.ParseInt(sizeStr, 10, 64); err == nil && sizeMB > 0 {
			maxUploadSize = sizeMB * 1024 * 1024
			debug.Info("Using Max Upload Size from environment: %d MiB", sizeMB)
		} else {
			debug.Warning("Invalid KH_MAX_UPLOAD_SIZE_MB value '%s', using default: 32 MiB", sizeStr)
		}
	} else {
		debug.Info("Using default Max Upload Size: 32 MiB")
	}

	// Determine Hash Upload Directory (within DataDir)
	hashUploadDir := filepath.Join(dataDir, "hashlist_uploads")
	if uploadDirEnv := os.Getenv("KH_HASH_UPLOAD_DIR"); uploadDirEnv != "" {
		// Allow overriding, potentially outside DataDir if absolute path is given
		if filepath.IsAbs(uploadDirEnv) {
			hashUploadDir = uploadDirEnv
		} else {
			hashUploadDir = filepath.Join(dataDir, uploadDirEnv) // Relative to dataDir
		}
	}
	// Ensure the upload directory exists
	if err := os.MkdirAll(hashUploadDir, 0750); err != nil {
		// Log error but proceed, file saving will fail later if it doesn't exist
		debug.Error("Failed to create hash upload directory '%s': %v", hashUploadDir, err)
	}
	debug.Info("Using Hash Upload directory: %s", hashUploadDir)

	return &Config{
		Host:          host,
		HTTPPort:      httpPort,
		HTTPSPort:     httpsPort,
		ConfigDir:     configDir,
		DataDir:       dataDir,
		MaxUploadSize: maxUploadSize,
		HashUploadDir: hashUploadDir,
	}
}

// GetHTTPAddress returns the address for the HTTP server
func (c *Config) GetHTTPAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.HTTPPort)
}

// GetHTTPSAddress returns the address for the HTTPS server
func (c *Config) GetHTTPSAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.HTTPSPort)
}

// GetWSEndpoint returns the WebSocket endpoint URL
func (c *Config) GetWSEndpoint() string {
	return fmt.Sprintf("ws://%s:%d/ws", c.Host, c.HTTPSPort)
}

// GetAPIEndpoint returns the API endpoint URL
func (c *Config) GetAPIEndpoint() string {
	return fmt.Sprintf("http://%s:%d/api", c.Host, c.HTTPSPort)
}

// GetAddress returns the full address for the server to listen on
func (c *Config) GetAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.HTTPSPort)
}

// GetCertsDir returns the path to the certificates directory
func (c *Config) GetCertsDir() string {
	// Check if running in Docker
	if env.GetBool("KH_IN_DOCKER") {
		return "/etc/krakenhashes/certs"
	}

	// For non-Docker installations, use the configured path
	certsDir := env.GetOrDefault("KH_CERTS_DIR", filepath.Join(c.ConfigDir, "certs"))

	// If the path is relative, make it absolute based on config directory
	if !filepath.IsAbs(certsDir) {
		certsDir = filepath.Join(c.ConfigDir, certsDir)
	}

	return certsDir
}

// GetAdditionalDNSNames returns a list of additional DNS names from environment variables
func GetAdditionalDNSNames() []string {
	// Get comma-separated list of DNS names from environment variable
	dnsNamesStr := env.GetOrDefault("KH_ADDITIONAL_DNS_NAMES", "")
	if dnsNamesStr == "" {
		return nil
	}

	// Split and trim whitespace
	dnsNames := strings.Split(dnsNamesStr, ",")
	for i := range dnsNames {
		dnsNames[i] = strings.TrimSpace(dnsNames[i])
	}

	return dnsNames
}

// GetAdditionalIPAddresses returns a list of additional IP addresses from environment variables
func GetAdditionalIPAddresses() []string {
	// Get comma-separated list of IP addresses from environment variable
	ipAddressesStr := env.GetOrDefault("KH_ADDITIONAL_IP_ADDRESSES", "")
	if ipAddressesStr == "" {
		return nil
	}

	// Split and trim whitespace
	ipAddresses := strings.Split(ipAddressesStr, ",")
	for i := range ipAddresses {
		ipAddresses[i] = strings.TrimSpace(ipAddresses[i])
	}

	return ipAddresses
}

// GetTLSConfig returns the TLS configuration including additional DNS names and IP addresses
func (c *Config) GetTLSConfig() ([]string, []string) {
	// Get additional DNS names
	dnsNames := GetAdditionalDNSNames()

	// Get additional IP addresses
	ipAddresses := GetAdditionalIPAddresses()

	return dnsNames, ipAddresses
}
