package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/agent"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/cleanup"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/jobs"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/metrics"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/version"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
	"github.com/joho/godotenv"
)

// agentConfig holds the agent's runtime configuration
type agentConfig struct {
	host               string // Host of the backend server (e.g., localhost:8080)
	useTLS             bool   // Whether to use TLS (HTTPS/WSS)
	listenInterface    string // Network interface to bind to
	heartbeatInterval  int    // Interval between heartbeats in seconds
	claimCode          string // Unique code for agent registration
	debug              bool   // Enable debug logging
	hashcatExtraParams string // Extra parameters to pass to hashcat (e.g., "-O -w 3")
	configDir          string // Configuration directory for certificates and credentials
	dataDir            string // Data directory for binaries, wordlists, rules, and hashlists
	testMode           bool   // Enable test mode (simulate GPU work without real hardware)
}

/*
 * loadConfig processes configuration from multiple sources in the following order:
 * 1. Command line flags (already parsed in main)
 * 2. .env file values (NOT environment variables to avoid conflicts with backend)
 *
 * If a required configuration value is not found, the function will exit with an error.
 * The function will create or update the .env file with any missing values.
 *
 * Parameters:
 *   - cfg: Pre-populated configuration from command-line flags
 *
 * Returns:
 *   - config: Populated configuration struct
 *
 * Required Configuration:
 *   - Backend Host
 */
func loadConfig(cfg agentConfig) agentConfig {
	// Load existing .env file values into a map
	envMap := make(map[string]string)
	envFileExists := false
	
	if _, err := os.Stat(".env"); err == nil {
		envFileExists = true
		// Read .env file
		envFile, err := godotenv.Read(".env")
		if err == nil {
			envMap = envFile
		}
	}

	// Apply values from .env file if command-line flags weren't provided
	// Priority: command-line flags > .env file > defaults
	
	// Host configuration
	if cfg.host == "" && envFileExists {
		host := envMap["KH_HOST"]
		port := envMap["KH_PORT"]
		if host != "" {
			if port != "" {
				cfg.host = fmt.Sprintf("%s:%s", host, port)
			} else {
				cfg.host = fmt.Sprintf("%s:31337", host)
			}
		}
	}
	
	// TLS setting
	if envFileExists && envMap["USE_TLS"] != "" {
		// Only override if not set by command line (check if it's still the default)
		if cfg.useTLS == true && !isFlagPassed("tls") {
			cfg.useTLS = envMap["USE_TLS"] == "true"
		}
	}
	
	// Listen interface
	if cfg.listenInterface == "" && envFileExists {
		cfg.listenInterface = envMap["LISTEN_INTERFACE"]
	}
	
	// Heartbeat interval
	if cfg.heartbeatInterval == 0 && envFileExists {
		if i, err := strconv.Atoi(envMap["HEARTBEAT_INTERVAL"]); err == nil && i > 0 {
			cfg.heartbeatInterval = i
		} else {
			cfg.heartbeatInterval = 5 // default to 5 seconds
		}
	} else if cfg.heartbeatInterval == 0 {
		cfg.heartbeatInterval = 5
	}
	
	// Claim code
	if cfg.claimCode == "" && envFileExists {
		cfg.claimCode = envMap["KH_CLAIM_CODE"]
	}
	
	// Debug setting
	if !cfg.debug && envFileExists {
		cfg.debug = envMap["DEBUG"] == "true"
	}

	// Test mode setting
	if !cfg.testMode && envFileExists {
		cfg.testMode = envMap["TEST_MODE"] == "true"
	}
	
	// Hashcat extra params
	if cfg.hashcatExtraParams == "" && envFileExists {
		cfg.hashcatExtraParams = envMap["HASHCAT_EXTRA_PARAMS"]
		// Clean up any accidental comment that might have been included in the value
		if strings.Contains(cfg.hashcatExtraParams, "#") {
			parts := strings.Split(cfg.hashcatExtraParams, "#")
			cfg.hashcatExtraParams = strings.TrimSpace(parts[0])
		}
	}
	
	// Directory configuration
	cwd, _ := os.Getwd()
	
	// Config directory
	if cfg.configDir == "" && envFileExists {
		cfg.configDir = envMap["KH_CONFIG_DIR"]
	}
	if cfg.configDir == "" {
		cfg.configDir = filepath.Join(cwd, "config")
	}
	
	// Data directory
	if cfg.dataDir == "" && envFileExists {
		cfg.dataDir = envMap["KH_DATA_DIR"]
	}
	if cfg.dataDir == "" {
		cfg.dataDir = filepath.Join(cwd, "data")
	}

	// Reinitialize debug after loading configuration
	if cfg.debug {
		os.Setenv("DEBUG", "true")
		os.Setenv("LOG_LEVEL", "DEBUG")
	}
	debug.Reinitialize()

	// Validate required configuration
	if cfg.host == "" {
		log.Fatal("Backend host must be provided via --host flag or KH_HOST/KH_PORT in .env file")
	}

	// Update or create .env file with current configuration
	updateEnvFile(cfg, envMap, envFileExists)
	
	// Set environment variables from resolved configuration
	// This ensures the config package uses our values instead of system environment
	os.Setenv("KH_CONFIG_DIR", cfg.configDir)
	os.Setenv("KH_DATA_DIR", cfg.dataDir)
	os.Setenv("KH_HOST", cfg.host)
	os.Setenv("USE_TLS", fmt.Sprintf("%t", cfg.useTLS))
	os.Setenv("HEARTBEAT_INTERVAL", fmt.Sprintf("%d", cfg.heartbeatInterval))
	
	debug.Info("Set KH_CONFIG_DIR to: %s", cfg.configDir)
	debug.Info("Set KH_DATA_DIR to: %s", cfg.dataDir)

	return cfg
}

// updateEnvFile creates or updates the .env file with current configuration
func updateEnvFile(cfg agentConfig, existingEnv map[string]string, fileExists bool) {
	// Split host and port for .env file
	host, port, err := net.SplitHostPort(cfg.host)
	if err != nil {
		host = cfg.host
		port = "31337" // Default port if not specified
	}

	// Prepare the configuration values
	newEnv := map[string]string{
		"KH_HOST":                     host,
		"KH_PORT":                     port,
		"USE_TLS":                     fmt.Sprintf("%t", cfg.useTLS),
		"LISTEN_INTERFACE":            cfg.listenInterface,
		"HEARTBEAT_INTERVAL":          fmt.Sprintf("%d", cfg.heartbeatInterval),
		"KH_CLAIM_CODE":               cfg.claimCode,
		"KH_CONFIG_DIR":               cfg.configDir,
		"KH_DATA_DIR":                 cfg.dataDir,
		"HASHCAT_EXTRA_PARAMS":        cfg.hashcatExtraParams,
		"DEBUG":                       fmt.Sprintf("%t", cfg.debug),
		"TEST_MODE":                   fmt.Sprintf("%t", cfg.testMode),
		"LOG_LEVEL":                   "DEBUG",
		"KH_MAX_CONCURRENT_DOWNLOADS": "3",
		"KH_DOWNLOAD_TIMEOUT":         "1h",
	}

	// Merge with existing values (existing values take precedence for non-command-line settings)
	finalEnv := make(map[string]string)
	if fileExists {
		// Start with existing values
		for k, v := range existingEnv {
			finalEnv[k] = v
		}
		// Add any missing keys from newEnv
		for k, v := range newEnv {
			if _, exists := finalEnv[k]; !exists {
				finalEnv[k] = v
			}
		}
		// Override with command-line values if they were explicitly set
		// (This ensures command-line flags take precedence)
		if isFlagPassed("host") {
			finalEnv["KH_HOST"] = host
			finalEnv["KH_PORT"] = port
		}
		if isFlagPassed("tls") {
			finalEnv["USE_TLS"] = fmt.Sprintf("%t", cfg.useTLS)
		}
		if isFlagPassed("interface") {
			finalEnv["LISTEN_INTERFACE"] = cfg.listenInterface
		}
		if isFlagPassed("heartbeat") {
			finalEnv["HEARTBEAT_INTERVAL"] = fmt.Sprintf("%d", cfg.heartbeatInterval)
		}
		if isFlagPassed("claim") {
			finalEnv["KH_CLAIM_CODE"] = cfg.claimCode
		}
		if isFlagPassed("debug") {
			finalEnv["DEBUG"] = fmt.Sprintf("%t", cfg.debug)
		}
		if isFlagPassed("test-mode") {
			finalEnv["TEST_MODE"] = fmt.Sprintf("%t", cfg.testMode)
		}
		if isFlagPassed("hashcat-params") {
			finalEnv["HASHCAT_EXTRA_PARAMS"] = cfg.hashcatExtraParams
		}
		if isFlagPassed("config-dir") {
			finalEnv["KH_CONFIG_DIR"] = cfg.configDir
		}
		if isFlagPassed("data-dir") {
			finalEnv["KH_DATA_DIR"] = cfg.dataDir
		}
	} else {
		// New file, use all values from config
		finalEnv = newEnv
	}

	// Generate .env file content
	env := fmt.Sprintf(`# KrakenHashes Agent Configuration
# Generated on: %s

# Server Configuration
KH_HOST=%s  # Backend server hostname
KH_PORT=%s  # Backend server port
USE_TLS=%s       # Use TLS for secure communication (wss:// and https://)
LISTEN_INTERFACE=%s
HEARTBEAT_INTERVAL=%s

# Agent Configuration
%s

# Directory Configuration
KH_CONFIG_DIR=%s  # Configuration directory for certificates and credentials
KH_DATA_DIR=%s    # Data directory for binaries, wordlists, rules, and hashlists

# WebSocket Timing Configuration
KH_WRITE_WAIT=10s   # Timeout for writing messages to WebSocket
KH_PONG_WAIT=60s    # Timeout for receiving pong from server
KH_PING_PERIOD=54s  # Interval for sending ping to server (must be less than pong wait)

# File Transfer Configuration
KH_MAX_CONCURRENT_DOWNLOADS=%s  # Maximum number of concurrent file downloads
KH_DOWNLOAD_TIMEOUT=%s        # Timeout for large file downloads

# Hashcat Configuration
# Extra parameters to pass to hashcat (e.g., "-O -w 3" for optimized kernels and high workload)
HASHCAT_EXTRA_PARAMS=%s

# Logging Configuration
DEBUG=%s
LOG_LEVEL=%s

# Test Mode Configuration
TEST_MODE=%s  # Enable mock mode for testing without GPUs
`,
		time.Now().Format(time.RFC3339),
		finalEnv["KH_HOST"],
		finalEnv["KH_PORT"],
		finalEnv["USE_TLS"],
		finalEnv["LISTEN_INTERFACE"],
		finalEnv["HEARTBEAT_INTERVAL"],
		formatClaimCode(finalEnv["KH_CLAIM_CODE"]),
		finalEnv["KH_CONFIG_DIR"],
		finalEnv["KH_DATA_DIR"],
		getEnvOrDefault(finalEnv, "KH_MAX_CONCURRENT_DOWNLOADS", "3"),
		getEnvOrDefault(finalEnv, "KH_DOWNLOAD_TIMEOUT", "1h"),
		finalEnv["HASHCAT_EXTRA_PARAMS"],
		finalEnv["DEBUG"],
		getEnvOrDefault(finalEnv, "LOG_LEVEL", "DEBUG"),
		finalEnv["TEST_MODE"])

	// Preserve any extra variables that exist in the original .env but aren't in the template
	// This includes MOCK_* variables and any other custom configuration
	templateKeys := map[string]bool{
		"KH_HOST": true, "KH_PORT": true, "USE_TLS": true, "LISTEN_INTERFACE": true,
		"HEARTBEAT_INTERVAL": true, "KH_CLAIM_CODE": true, "KH_CONFIG_DIR": true,
		"KH_DATA_DIR": true, "KH_WRITE_WAIT": true, "KH_PONG_WAIT": true,
		"KH_PING_PERIOD": true, "KH_MAX_CONCURRENT_DOWNLOADS": true,
		"KH_DOWNLOAD_TIMEOUT": true, "HASHCAT_EXTRA_PARAMS": true,
		"DEBUG": true, "LOG_LEVEL": true, "TEST_MODE": true,
	}

	if fileExists && len(existingEnv) > 0 {
		var extraVars strings.Builder
		for key, value := range existingEnv {
			if !templateKeys[key] {
				// Quote values that contain spaces
				if strings.Contains(value, " ") {
					extraVars.WriteString(fmt.Sprintf("%s=\"%s\"\n", key, value))
				} else {
					extraVars.WriteString(fmt.Sprintf("%s=%s\n", key, value))
				}
			}
		}
		if extraVars.Len() > 0 {
			env += "\n# Additional Configuration (preserved from existing .env)\n"
			env += extraVars.String()
		}
	}

	if err := os.WriteFile(".env", []byte(env), 0644); err != nil {
		log.Printf("Warning: Could not save configuration to .env file: %v", err)
	}
}

// formatClaimCode formats the claim code line, commenting it out if already used
func formatClaimCode(claimCode string) string {
	if claimCode == "" {
		return "# KH_CLAIM_CODE=  # Add claim code for first-time registration"
	}
	// Check if claim code starts with # (already commented)
	if strings.HasPrefix(claimCode, "#") {
		return claimCode
	}
	return fmt.Sprintf("KH_CLAIM_CODE=%s", claimCode)
}

// getEnvOrDefault returns the value from the map or a default if not present
func getEnvOrDefault(envMap map[string]string, key, defaultValue string) string {
	if val, exists := envMap[key]; exists && val != "" {
		return val
	}
	return defaultValue
}

// isFlagPassed checks if a specific flag was passed on the command line
func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// commentOutClaimCode comments out the CLAIM_CODE line in the .env file
// after successful registration
func commentOutClaimCode() error {
	envFile := ".env"

	// Read the current .env file
	content, err := os.ReadFile(envFile)
	if err != nil {
		return fmt.Errorf("failed to read .env file: %w", err)
	}

	// Create a backup of the original file
	backupFile := envFile + ".bak"
	if err := os.WriteFile(backupFile, content, 0644); err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}

	// Split into lines and modify the KH_CLAIM_CODE line
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "KH_CLAIM_CODE=") {
			lines[i] = "# " + line + " # Commented out after successful registration"
		}
	}

	// Write the modified content back to the file
	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(envFile, []byte(newContent), 0644); err != nil {
		// If writing fails, try to restore from backup
		os.WriteFile(envFile, content, 0644)
		return fmt.Errorf("failed to write modified .env file: %w", err)
	}

	// Remove the backup file
	os.Remove(backupFile)
	return nil
}

/*
 * main is the entry point for the KrakenHashes agent.
 *
 * It performs the following operations:
 * 1. Loads and validates configuration
 * 2. Establishes connection with the backend
 * 3. Starts the heartbeat mechanism
 * 4. Begins processing jobs
 *
 * The agent will continue running until terminated or a fatal error occurs.
 */
func main() {
	// Parse command-line flags FIRST before anything else
	// This ensures debug flag is processed before any logging
	cfg := agentConfig{}
	flag.StringVar(&cfg.host, "host", "", "Backend server host (e.g., localhost:31337)")
	flag.BoolVar(&cfg.useTLS, "tls", true, "Use TLS for secure communication (default: true)")
	flag.StringVar(&cfg.listenInterface, "interface", "", "Network interface to listen on (optional)")
	flag.IntVar(&cfg.heartbeatInterval, "heartbeat", 0, "Heartbeat interval in seconds (default: 5)")
	flag.StringVar(&cfg.claimCode, "claim", "", "Agent claim code (required only for first-time registration)")
	flag.BoolVar(&cfg.debug, "debug", false, "Enable debug logging (default: false)")
	flag.BoolVar(&cfg.testMode, "test-mode", false, "Enable test mode (simulate GPU work without real hardware)")
	flag.StringVar(&cfg.hashcatExtraParams, "hashcat-params", "", "Extra parameters to pass to hashcat (e.g., '-O -w 3')")
	flag.StringVar(&cfg.configDir, "config-dir", "", "Configuration directory for certificates and credentials")
	flag.StringVar(&cfg.dataDir, "data-dir", "", "Data directory for binaries, wordlists, rules, and hashlists")
	flag.Parse()

	// Set debug environment variable if debug flag is set
	if cfg.debug {
		os.Setenv("DEBUG", "true")
		os.Setenv("LOG_LEVEL", "DEBUG")
	}

	// Set test mode environment variable if test mode flag is set
	if cfg.testMode {
		os.Setenv("TEST_MODE", "true")
	}

	// Initialize debug package with settings from flags/environment
	debug.Reinitialize()
	debug.Info("Debug logging initialized - Debug enabled: %v", cfg.debug || os.Getenv("DEBUG") == "true")

	// Show startup message to console
	agentVersion := version.GetVersion()
	if cfg.testMode {
		console.Info("Starting KrakenHashes Agent %s (TEST MODE - Simulated GPU Execution)", agentVersion)
	} else {
		console.Info("Starting KrakenHashes Agent %s", agentVersion)
	}

	// Get and log current working directory
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get working directory: %v", err)
		console.Error("Failed to get working directory: %v", err)
		os.Exit(1)
	}
	debug.Info("Current working directory: %s", cwd)
	console.Status("Working directory: %s", cwd)

	// Log executable path
	execPath, execErr := os.Executable()
	if execErr != nil {
		debug.Warning("Failed to get executable path: %v", execErr)
	} else {
		debug.Info("Executable path: %s", execPath)
		debug.Info("Executable directory: %s", filepath.Dir(execPath))
	}

	// Flag to track if .env file was loaded successfully
	envLoaded := false

	// Check if KH_ENV_FILE environment variable is set
	envFilePath := os.Getenv("KH_ENV_FILE")
	if envFilePath != "" {
		debug.Info("KH_ENV_FILE environment variable is set to: %s", envFilePath)
		absEnvFilePath, _ := filepath.Abs(envFilePath)
		debug.Info("Attempting to load .env from specified path: %s (absolute: %s)", envFilePath, absEnvFilePath)
		err = godotenv.Load(envFilePath)
		if err != nil {
			debug.Error("Failed to load .env file from specified path: %v", err)
			debug.Warning("Will try other locations...")
		} else {
			debug.Info("Successfully loaded .env file from specified path")
			envLoaded = true
		}
	} else {
		debug.Info("KH_ENV_FILE environment variable is not set, will try default locations")
	}

	// Only try other locations if .env hasn't been loaded yet
	if !envLoaded {
		// Try to load .env file from current directory
		cwdEnvPath := filepath.Join(cwd, ".env")
		debug.Info("Attempting to load .env from current directory: %s", cwdEnvPath)
		err = godotenv.Load(cwdEnvPath)
		if err != nil {
			debug.Warning("Failed to load .env file from current directory: %v", err)

			// Try to load from project root
			projectRootEnvPath := filepath.Join(cwd, "../../.env")
			absProjectRootEnvPath, _ := filepath.Abs(projectRootEnvPath)
			debug.Info("Attempting to load .env from project root: %s (absolute: %s)", projectRootEnvPath, absProjectRootEnvPath)
			err = godotenv.Load(projectRootEnvPath)
			if err != nil {
				debug.Warning("Failed to load .env file from project root: %v", err)

				// Try to load from executable directory
				if execErr == nil {
					execDirEnvPath := filepath.Join(filepath.Dir(execPath), ".env")
					debug.Info("Attempting to load .env from executable directory: %s", execDirEnvPath)
					err = godotenv.Load(execDirEnvPath)
					if err != nil {
						debug.Warning("Failed to load .env file from executable directory: %v", err)
					} else {
						debug.Info("Successfully loaded .env file from executable directory")
						envLoaded = true
					}
				}

				// If all attempts failed, log but continue
				// The loadConfig() function will create a .env file if needed
				if !envLoaded {
					debug.Info("No .env file found in any location, will create one from command-line flags")
					debug.Info("Searched the following locations:")
					if envFilePath != "" {
						debug.Info("1. Specified path (KH_ENV_FILE): %s", envFilePath)
						debug.Info("2. Current directory: %s", cwdEnvPath)
						debug.Info("3. Project root: %s", absProjectRootEnvPath)
						if execErr == nil {
							debug.Info("4. Executable directory: %s", filepath.Join(filepath.Dir(execPath), ".env"))
						}
					} else {
						debug.Info("1. Current directory: %s", cwdEnvPath)
						debug.Info("2. Project root: %s", absProjectRootEnvPath)
						if execErr == nil {
							debug.Info("3. Executable directory: %s", filepath.Join(filepath.Dir(execPath), ".env"))
						}
					}
					// Don't exit - allow loadConfig() to create .env from flags
				}
			} else {
				debug.Info("Successfully loaded .env file from project root")
				envLoaded = true
			}
		} else {
			debug.Info("Successfully loaded .env file from current directory")
			envLoaded = true
		}
	}

	// Reinitialize debug package with loaded environment variables
	debug.Reinitialize()
	debug.Info("Debug logging reinitialized with environment variables")

	// Load configuration (pass the pre-parsed cfg)
	debug.Info("Loading agent configuration...")
	console.Status("Loading agent configuration...")
	cfg = loadConfig(cfg)
	debug.Info("Agent configuration loaded successfully")
	console.Success("Configuration loaded successfully")

	// Set environment variables from config
	host, port, err := net.SplitHostPort(cfg.host)
	if err != nil {
		host = cfg.host
		port = "31337" // Default port if not specified
	}
	os.Setenv("KH_HOST", host)
	os.Setenv("KH_PORT", port)
	os.Setenv("USE_TLS", strconv.FormatBool(cfg.useTLS))

	// Create URL configuration
	urlConfig := config.NewURLConfig()
	debug.Info("URL Configuration:")
	debug.Info("- Base URL: %s", urlConfig.GetAPIBaseURL())
	debug.Info("- WebSocket URL: %s", urlConfig.GetWebSocketURL())
	console.Status("Connecting to backend at %s", cfg.host)

	// Initialize data directories early in the process
	debug.Info("Initializing data directories...")
	console.Status("Initializing data directories...")
	dataDirs, err := config.GetDataDirs()
	if err != nil {
		debug.Error("Failed to initialize data directories: %v", err)
		console.Error("Failed to initialize data directories: %v", err)
		os.Exit(1)
	}
	debug.Info("Data directories initialized successfully at %s", dataDirs.Binaries)

	// Create metrics collector
	collector, err := metrics.New(metrics.Config{
		CollectionInterval: time.Duration(cfg.heartbeatInterval) * time.Second,
		EnableGPU:          true,
	})
	if err != nil {
		debug.Error("Failed to create metrics collector: %v", err)
		os.Exit(1)
	}
	defer collector.Close()

	// Check for existing certificates
	debug.Info("Checking for existing certificates...")
	console.Status("Checking for existing credentials...")
	agentID, cert, err := agent.LoadCredentials()
	if err != nil {
		debug.Error("Failed to load credentials: %v", err)

		// Check if we have an API key but missing certificates
		apiKey, agentIDFromKey, keyErr := auth.LoadAgentKey(config.GetConfigDir())
		if keyErr == nil && apiKey != "" && agentIDFromKey != "" {
			debug.Info("Found API key but missing certificates - attempting certificate renewal")
			console.Warning("Found API key but missing certificates - renewing...")
			if renewErr := agent.RenewCertificates(urlConfig); renewErr != nil {
				debug.Error("Failed to renew certificates: %v", renewErr)
				console.Error("Failed to renew certificates: %v", renewErr)
				os.Exit(1)
			}
			// Reload credentials after renewal
			agentID, cert, err = agent.LoadCredentials()
			if err != nil {
				debug.Error("Failed to load credentials after renewal: %v", err)
				console.Error("Failed to load credentials after renewal: %v", err)
				os.Exit(1)
			}
			console.Success("Certificates renewed successfully")
		} else if cfg.claimCode == "" {
			debug.Error("Claim code required for first-time registration")
			console.Error("No existing credentials found. Please provide a claim code with --claim flag for first-time registration")
			os.Exit(1)
		} else {
			debug.Info("Starting registration process with claim code")
			console.Status("Registering agent with claim code...")

			// Attempt registration
			if err := agent.RegisterAgent(cfg.claimCode, urlConfig); err != nil {
				debug.Error("Failed to register agent: %v", err)
				console.Error("Failed to register agent: %v", err)
				os.Exit(1)
			}

			// Reload credentials after registration
			debug.Info("Reloading credentials after registration...")
			agentID, cert, err = agent.LoadCredentials()
			if err != nil {
				debug.Error("Failed to load credentials after registration: %v", err)
				console.Error("Failed to load credentials after registration: %v", err)
				os.Exit(1)
			}

			// Comment out claim code after successful registration
			if err := commentOutClaimCode(); err != nil {
				debug.Warning("Failed to comment out claim code: %v", err)
			}
			console.Success("Agent registered successfully (ID: %s)", agentID)
		}
	} else if agentID == "" || cert == "" {
		debug.Error("Loaded credentials are empty - Agent ID: %v, Certificate: %v", agentID != "", cert != "")
		if cfg.claimCode == "" {
			debug.Error("Claim code required for first-time registration")
			console.Error("Invalid credentials found. Please provide a claim code with --claim flag to re-register")
			os.Exit(1)
		}
		debug.Info("Starting registration process with claim code")
		console.Status("Re-registering agent with claim code...")

		// Attempt registration
		if err := agent.RegisterAgent(cfg.claimCode, urlConfig); err != nil {
			debug.Error("Failed to register agent: %v", err)
			console.Error("Failed to register agent: %v", err)
			os.Exit(1)
		}

		// Reload credentials after registration
		debug.Info("Reloading credentials after registration...")
		agentID, cert, err = agent.LoadCredentials()
		if err != nil {
			debug.Error("Failed to load credentials after registration: %v", err)
			console.Error("Failed to load credentials after registration: %v", err)
			os.Exit(1)
		}

		// Comment out claim code after successful registration
		if err := commentOutClaimCode(); err != nil {
			debug.Warning("Failed to comment out claim code: %v", err)
		}
		console.Success("Agent registered successfully (ID: %s)", agentID)
	} else {
		debug.Info("Found existing credentials, proceeding with WebSocket connection")
		debug.Debug("Agent ID: %s", agentID)
		debug.Debug("Certificate length: %d bytes", len(cert))
		console.Success("Credentials loaded (Agent ID: %s)", agentID)
	}

	// Create job manager before establishing connection
	debug.Info("Creating job manager...")
	agentConfig := config.NewConfig()

	// Job manager will be created after connection is established
	// so we can pass the hardware monitor
	var jobManager *jobs.JobManager
	debug.Info("Job manager will be created after connection")

	// Create connection with retry
	debug.Info("Starting WebSocket connection process")
	console.Status("Establishing WebSocket connection...")
	var lastError error
	var conn *agent.Connection
	for i := 0; i < 3; i++ {
		debug.Info("Connection attempt %d of 3", i+1)
		if i > 0 {
			console.Warning("Connection attempt %d of 3...", i+1)
		}
		conn, err = agent.NewConnection(urlConfig)
		if err != nil {
			lastError = err
			debug.Warning("Failed to create connection on attempt %d: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}

		// Create job manager with hardware monitor from connection
		hwMonitor := conn.GetHardwareMonitor()
		jobManager = jobs.NewJobManagerWithExecutor(agentConfig, nil, hwMonitor, cfg.testMode)
		if cfg.testMode {
			debug.Info("Job manager created with MOCK executor (test mode)")
		} else {
			debug.Info("Job manager created successfully with hardware monitor")
		}

		// Set the job manager in the connection
		conn.SetJobManager(jobManager)

		if err := conn.Start(); err != nil {
			lastError = err
			debug.Warning("Connection attempt %d failed: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		debug.Info("Connection attempt %d successful", i+1)
		console.Success("Connected to backend successfully")

		// Device detection is triggered by config_update message from backend
		// This ensures the preferred binary version is set before detection runs
		// Running detection here would cause a race condition with config_update
		
		// Set up dual callbacks for status and cracks
		statusCallback := func(status *jobs.JobStatus) {
			debug.Debug("Job status: Task %s, Progress %.2f%%, Hash rate %d H/s",
				status.TaskID, status.ProgressPercent, status.HashRate)

			// Send status to backend via WebSocket (synchronous, must be delivered)
			if err := conn.SendJobStatus(status); err != nil {
				debug.Error("Failed to send job status to backend: %v", err)
			}
		}
		jobManager.SetStatusCallback(statusCallback)
		debug.Info("Status callback configured to send updates to backend")

		crackCallback := func(batch *jobs.CrackBatch) {
			debug.Info("Crack batch: Task %s, %d cracks", batch.TaskID, len(batch.CrackedHashes))

			// Send crack batch to backend via WebSocket (asynchronous, can drop)
			if err := conn.SendCrackBatchAsync(batch); err != nil {
				debug.Warning("Failed to send crack batch to backend: %v (will recover from outfile)", err)
			}
		}
		jobManager.SetCrackCallback(crackCallback)
		debug.Info("Crack callback configured to send batches to backend")
		
		// Set up output callback to send hashcat output via websocket
		outputCallback := func(taskID string, output string, isError bool) {
			// Send output to backend via WebSocket
			if err := conn.SendHashcatOutput(taskID, output, isError); err != nil {
				debug.Error("Failed to send hashcat output to backend: %v", err)
			}
		}
		jobManager.SetOutputCallback(outputCallback)
		debug.Info("Output callback configured to send hashcat output to backend")
		
		lastError = nil
		break
	}

	if lastError != nil {
		debug.Error("Failed to establish connection after 3 attempts: %v", lastError)
		console.Error("Failed to establish connection after 3 attempts: %v", lastError)
		os.Exit(1)
	}

	// Start the cleanup service for automatic file cleanup
	cleanupService := cleanup.NewCleanupService(dataDirs)
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	cleanupService.Start(cleanupCtx)
	debug.Info("File cleanup service started with 3-day retention policy")

	console.Success("Heartbeat active (interval: %ds)", cfg.heartbeatInterval)
	console.Info("Agent running, press Ctrl+C to exit")
	debug.Info("Agent running, press Ctrl+C to exit")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill)
	<-sigChan

	console.Info("Shutting down agent...")
	debug.Info("Shutting down agent...")

	// Stop the cleanup service
	debug.Info("Stopping cleanup service...")
	cleanupCancel()
	cleanupService.Stop()

	// Capture current task status BEFORE stopping the job manager
	var hasTask bool
	var taskID, jobID string
	if jobManager != nil {
		hasTask, taskID, jobID, _ = jobManager.GetCurrentTaskStatus()
		if hasTask {
			debug.Info("Capturing task status before shutdown - TaskID: %s, JobID: %s", taskID, jobID)
			console.Status("Stopping active task: %s", taskID)
		}
	}

	// Now stop the job manager to cleanly stop all running jobs
	if jobManager != nil {
		debug.Info("Stopping job manager and all running tasks...")
		console.Status("Stopping active tasks...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := jobManager.Shutdown(ctx); err != nil {
			debug.Error("Error during job manager shutdown: %v", err)
		} else {
			debug.Info("Job manager shutdown complete")
		}
	}

	// Send shutdown notification to server before closing connection
	if conn != nil {
		debug.Info("Sending shutdown notification to server...")
		conn.SendShutdownNotification(hasTask, taskID, jobID)
		time.Sleep(500 * time.Millisecond) // Give time for the message to be sent

		debug.Info("Stopping connection...")
		console.Status("Disconnecting from backend...")
		conn.Stop() // Stop the active connection and maintenance routines
	}
	time.Sleep(time.Second) // Give connections time to close gracefully

	console.Success("Agent shutdown complete")
	debug.Info("Agent shutdown complete")
}
