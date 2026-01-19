package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

const (
	// DefaultConfigDir is the default directory name for agent configuration
	// This will be created in the same directory as the executable
	DefaultConfigDir = "config"
	// DefaultDataDir is the default directory name for agent data
	// This will be created in the same directory as the executable
	DefaultDataDir = "data"
)

// Config represents the agent configuration
type Config struct {
	DataDirectory      string
	HashcatExtraParams string // Extra parameters to pass to hashcat (e.g., "-O -w 3")
}

// NewConfig creates a new agent configuration
func NewConfig() *Config {
	dataDirs, err := GetDataDirs()
	if err != nil {
		debug.Error("Failed to get data directories: %v", err)
		return &Config{
			DataDirectory:      "data",
			HashcatExtraParams: os.Getenv("HASHCAT_EXTRA_PARAMS"),
		}
	}
	
	// Use the base data directory
	baseDataDir := filepath.Dir(dataDirs.Binaries)
	
	return &Config{
		DataDirectory:      baseDataDir,
		HashcatExtraParams: os.Getenv("HASHCAT_EXTRA_PARAMS"),
	}
}

// DataDirs represents the data directories used by the agent
type DataDirs struct {
	// Base directories
	Binaries  string
	Wordlists string
	Rules     string
	Hashlists string

	// Wordlist type subdirectories
	WordlistGeneral     string
	WordlistSpecialized string
	WordlistTargeted    string
	WordlistCustom      string

	// Rule type subdirectories
	RuleHashcat string
	RuleJohn    string
	RuleCustom  string
}

// GetDataDirs returns the paths to the agent's data directories
// It checks KH_DATA_DIR environment variable first, then falls back to default
// The directories will be created if they don't exist
func GetDataDirs() (*DataDirs, error) {
	var baseDataDir string

	// Print current working directory for debugging
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get current working directory: %v", err)
		cwd = "."
	} else {
		// Set base path for log sanitization before logging anything else
		// This converts absolute paths to relative paths in all subsequent log messages
		debug.SetBasePath(cwd)
		debug.Info("Current working directory in GetDataDirs: %s", cwd)
	}

	// Check environment variable first (this is the preferred method)
	if envDir := os.Getenv("KH_DATA_DIR"); envDir != "" {
		debug.Info("Using data directory from environment: %s", envDir)

		// Check if the path is relative or absolute
		if filepath.IsAbs(envDir) {
			debug.Info("Data directory path is absolute")
			baseDataDir = envDir
		} else {
			debug.Info("Data directory path is relative, resolving from current directory")
			absPath, err := filepath.Abs(envDir)
			if err != nil {
				debug.Error("Failed to resolve absolute path: %v", err)
				baseDataDir = envDir
			} else {
				debug.Info("Resolved absolute path: %s", absPath)
				baseDataDir = absPath
			}
		}
	} else {
		// For development/go run: use current working directory
		// For production: fall back to executable directory
		debug.Info("KH_DATA_DIR not set, determining best directory to use")
		
		// Check if we're running via go run (executable is in temp directory)
		execPath, err := os.Executable()
		if err == nil && strings.Contains(execPath, "go-build") {
			// We're running via `go run`, use current working directory
			baseDataDir = filepath.Join(cwd, DefaultDataDir)
			debug.Info("Detected go run execution, using data directory relative to CWD: %s", baseDataDir)
		} else if err != nil {
			// Can't get executable path, use current directory
			debug.Error("Could not get executable path: %v", err)
			debug.Info("Using current directory with default data dir: %s", DefaultDataDir)
			baseDataDir = filepath.Join(cwd, DefaultDataDir)
		} else {
			// Production build - use the data directory relative to the executable
			execDir := filepath.Dir(execPath)
			baseDataDir = filepath.Join(execDir, DefaultDataDir)
			debug.Info("Using data directory relative to executable: %s", baseDataDir)
		}
	}

	// Create base data directory if it doesn't exist
	if err := os.MkdirAll(baseDataDir, 0750); err != nil {
		debug.Error("Failed to create base data directory %s: %v", baseDataDir, err)
		return nil, fmt.Errorf("failed to create base data directory: %w", err)
	}

	// Create data directories structure
	dirs := &DataDirs{
		// Base directories
		Binaries:  filepath.Join(baseDataDir, "binaries"),
		Wordlists: filepath.Join(baseDataDir, "wordlists"),
		Rules:     filepath.Join(baseDataDir, "rules"),
		Hashlists: filepath.Join(baseDataDir, "hashlists"),

		// Wordlist type subdirectories
		WordlistGeneral:     filepath.Join(baseDataDir, "wordlists", "general"),
		WordlistSpecialized: filepath.Join(baseDataDir, "wordlists", "specialized"),
		WordlistTargeted:    filepath.Join(baseDataDir, "wordlists", "targeted"),
		WordlistCustom:      filepath.Join(baseDataDir, "wordlists", "custom"),

		// Rule type subdirectories
		RuleHashcat: filepath.Join(baseDataDir, "rules", "hashcat"),
		RuleJohn:    filepath.Join(baseDataDir, "rules", "john"),
		RuleCustom:  filepath.Join(baseDataDir, "rules", "custom"),
	}

	// Create each directory with appropriate permissions
	for name, dir := range map[string]string{
		"binaries":  dirs.Binaries,
		"wordlists": dirs.Wordlists,
		"rules":     dirs.Rules,
		"hashlists": dirs.Hashlists,
	} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create %s directory %s: %v", name, dir, err)
			return nil, fmt.Errorf("failed to create %s directory: %w", name, err)
		}
		debug.Info("Created %s directory: %s", name, dir)
	}

	// Create subdirectories for wordlists based on type
	wordlistSubdirs := []string{
		"general",
		"specialized",
		"targeted",
		"custom",
	}

	for _, subdir := range wordlistSubdirs {
		dir := filepath.Join(dirs.Wordlists, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create wordlist type directory %s: %v", dir, err)
			return nil, fmt.Errorf("failed to create wordlist type directory: %w", err)
		}
		debug.Info("Created wordlist type directory: %s", dir)
	}

	// Create subdirectories for rules based on type
	ruleSubdirs := []string{
		"hashcat",
		"john",
		"custom",
	}

	for _, subdir := range ruleSubdirs {
		dir := filepath.Join(dirs.Rules, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			debug.Error("Failed to create rule type directory %s: %v", dir, err)
			return nil, fmt.Errorf("failed to create rule type directory: %w", err)
		}
		debug.Info("Created rule type directory: %s", dir)
	}

	return dirs, nil
}

// GetConfigDir returns the path to the agent's configuration directory
// It checks KH_CONFIG_DIR environment variable first, then falls back to default
// The directory will be created if it doesn't exist
func GetConfigDir() string {
	var configDir string

	// Print current working directory for debugging
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get current working directory: %v", err)
	} else {
		debug.Info("Current working directory in GetConfigDir: %s", cwd)
	}

	// Check environment variable first (this is the preferred method)
	if envDir := os.Getenv("KH_CONFIG_DIR"); envDir != "" {
		debug.Info("Using config directory from environment: %s", envDir)

		// Check if the path is relative or absolute
		if filepath.IsAbs(envDir) {
			debug.Info("Config directory path is absolute")
			configDir = envDir
		} else {
			debug.Info("Config directory path is relative, resolving from current directory")
			absPath, err := filepath.Abs(envDir)
			if err != nil {
				debug.Error("Failed to resolve absolute path: %v", err)
				configDir = envDir
			} else {
				debug.Info("Resolved absolute path: %s", absPath)
				configDir = absPath
			}
		}
	} else {
		// For development/go run: use current working directory
		// For production: fall back to executable directory
		debug.Info("KH_CONFIG_DIR not set, determining best directory to use")
		
		// Check if we're running via go run (executable is in temp directory)
		execPath, err := os.Executable()
		if err == nil && strings.Contains(execPath, "go-build") {
			// We're running via `go run`, use current working directory
			configDir = filepath.Join(cwd, DefaultConfigDir)
			debug.Info("Detected go run execution, using config directory relative to CWD: %s", configDir)
		} else if err != nil {
			// Can't get executable path, use current directory
			debug.Error("Could not get executable path: %v", err)
			debug.Info("Using current directory with default config dir: %s", DefaultConfigDir)
			configDir = filepath.Join(cwd, DefaultConfigDir)
		} else {
			// Production build - use the config directory relative to the executable
			execDir := filepath.Dir(execPath)
			configDir = filepath.Join(execDir, DefaultConfigDir)
			debug.Info("Using config directory relative to executable: %s", configDir)
		}
	}

	// Create the directory if it doesn't exist
	debug.Debug("Creating config directory if it doesn't exist: %s", configDir)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		debug.Error("Failed to create config directory %s: %v", configDir, err)
		// Fall back to current directory if we can't create the intended directory
		configDir = DefaultConfigDir
		debug.Warning("Falling back to default config directory: %s", configDir)
		if err := os.MkdirAll(configDir, 0700); err != nil {
			debug.Error("Failed to create fallback config directory: %v", err)
		}
	}

	debug.Info("Using config directory: %s", configDir)
	return configDir
}
