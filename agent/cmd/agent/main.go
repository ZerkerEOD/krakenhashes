package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/hashdom/agent/internal/agent"
	"github.com/ZerkerEOD/hashdom/agent/internal/config"
	"github.com/ZerkerEOD/hashdom/agent/internal/metrics"
	"github.com/ZerkerEOD/hashdom/agent/pkg/debug"
	"github.com/joho/godotenv"
)

// agentConfig holds the agent's runtime configuration
type agentConfig struct {
	host              string // Host of the backend server (e.g., localhost:8080)
	useTLS            bool   // Whether to use TLS (HTTPS/WSS)
	listenInterface   string // Network interface to bind to
	heartbeatInterval int    // Interval between heartbeats in seconds
	claimCode         string // Unique code for agent registration
	debug             bool   // Enable debug logging
}

/*
 * loadConfig processes configuration from multiple sources in the following order:
 * 1. Command line flags
 * 2. Environment variables
 * 3. .env file
 *
 * If a required configuration value is not found, the function will exit with an error.
 * When running for the first time, it saves the configuration to a .env file for future use.
 *
 * Returns:
 *   - config: Populated configuration struct
 *
 * Required Configuration:
 *   - Backend Host
 */
func loadConfig() agentConfig {
	cfg := agentConfig{}

	// Define command line flags with usage documentation
	flag.StringVar(&cfg.host, "host", "", "Backend server host (e.g., localhost:8080)")
	flag.BoolVar(&cfg.useTLS, "tls", false, "Use TLS for secure communication")
	flag.StringVar(&cfg.listenInterface, "interface", "", "Network interface to listen on (optional)")
	flag.IntVar(&cfg.heartbeatInterval, "heartbeat", 0, "Heartbeat interval in seconds (default: 5)")
	flag.StringVar(&cfg.claimCode, "claim", "", "Agent claim code (required only for first-time registration)")
	flag.BoolVar(&cfg.debug, "debug", false, "Enable debug logging (default: false)")
	flag.Parse()

	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using command line flags and environment variables")
	}

	// Reinitialize debug after loading .env
	debug.Reinitialize()

	// Override with environment variables if not set by flags
	if cfg.host == "" {
		host := os.Getenv("HASHDOM_HOST")
		port := os.Getenv("HASHDOM_PORT")
		if host != "" {
			if port != "" {
				cfg.host = fmt.Sprintf("%s:%s", host, port)
			} else {
				cfg.host = fmt.Sprintf("%s:8080", host)
			}
		}
	}
	if !cfg.useTLS {
		cfg.useTLS = os.Getenv("USE_TLS") == "true"
	}
	if cfg.listenInterface == "" {
		cfg.listenInterface = os.Getenv("LISTEN_INTERFACE")
	}
	if cfg.heartbeatInterval == 0 {
		if i, err := strconv.Atoi(os.Getenv("HEARTBEAT_INTERVAL")); err == nil {
			cfg.heartbeatInterval = i
		} else {
			cfg.heartbeatInterval = 5 // default to 5 seconds
		}
	}
	if cfg.claimCode == "" {
		cfg.claimCode = os.Getenv("HASHDOM_CLAIM_CODE")
	}
	if !cfg.debug {
		cfg.debug = os.Getenv("DEBUG") == "true"
	}

	// Validate required configuration
	if cfg.host == "" {
		log.Fatal("Backend host must be provided via flag or BACKEND_HOST environment variable")
	}

	// Save configuration to .env file if it doesn't exist
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		// Split host and port for .env file
		host, port, err := net.SplitHostPort(cfg.host)
		if err != nil {
			host = cfg.host
			port = "8080" // Default port if not specified
		}

		env := fmt.Sprintf(`# HashDom Agent Configuration
# Generated on: %s

# Server Configuration
HASHDOM_HOST=%s  # Backend server hostname
HASHDOM_PORT=%s  # Backend server port
USE_TLS=%t       # Use TLS for secure communication (wss:// and https://)
LISTEN_INTERFACE=%s
HEARTBEAT_INTERVAL=%d

# Agent Configuration
HASHDOM_CLAIM_CODE=%s

# Logging Configuration
DEBUG=%t
LOG_LEVEL=%s
`, time.Now().Format(time.RFC3339), host, port, cfg.useTLS, cfg.listenInterface, cfg.heartbeatInterval, cfg.claimCode, cfg.debug, "DEBUG")

		if err := os.WriteFile(".env", []byte(env), 0644); err != nil {
			log.Printf("Warning: Could not save configuration to .env file: %v", err)
		}
	}

	return cfg
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

	// Split into lines and modify the HASHDOM_CLAIM_CODE line
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "HASHDOM_CLAIM_CODE=") {
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
 * main is the entry point for the HashDom agent.
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
	// Initialize debug package first with default settings
	debug.Reinitialize()
	debug.Info("Debug logging initialized with default settings")

	// Get and log current working directory
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get working directory: %v", err)
		os.Exit(1)
	}
	debug.Info("Current working directory: %s", cwd)

	// Load .env file
	err = godotenv.Load()
	if err != nil {
		debug.Info("Attempting to load .env from current directory: %s", cwd)
		debug.Warning("Failed to load .env file from current directory: %v", err)

		debug.Info("Attempting to load .env from project root")
		err = godotenv.Load("../../.env")
		if err != nil {
			debug.Error("Failed to load .env file from project root: %v", err)
			os.Exit(1)
		}
		debug.Info("Successfully loaded .env file from project root")
	} else {
		debug.Info("Successfully loaded .env file from current directory")
	}

	// Reinitialize debug package with loaded environment variables
	debug.Reinitialize()
	debug.Info("Debug logging reinitialized with environment variables")

	// Load configuration
	debug.Info("Loading agent configuration...")
	cfg := loadConfig()
	debug.Info("Agent configuration loaded successfully")

	// Set environment variables from config
	host, port, err := net.SplitHostPort(cfg.host)
	if err != nil {
		host = cfg.host
		port = "8080" // Default port if not specified
	}
	os.Setenv("HASHDOM_HOST", host)
	os.Setenv("HASHDOM_PORT", port)
	os.Setenv("USE_TLS", strconv.FormatBool(cfg.useTLS))

	// Create URL configuration
	urlConfig := config.NewURLConfig()
	debug.Info("URL Configuration:")
	debug.Info("- Base URL: %s", urlConfig.GetAPIBaseURL())
	debug.Info("- WebSocket URL: %s", urlConfig.GetWebSocketURL())

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
	agentID, cert, err := agent.LoadCredentials()
	if err != nil {
		debug.Error("Failed to load credentials: %v", err)
		if cfg.claimCode == "" {
			debug.Error("Claim code required for first-time registration")
			os.Exit(1)
		}
		debug.Info("Starting registration process with claim code")

		// Attempt registration
		if err := agent.RegisterAgent(cfg.claimCode, urlConfig); err != nil {
			debug.Error("Failed to register agent: %v", err)
			os.Exit(1)
		}

		// Reload credentials after registration
		debug.Info("Reloading credentials after registration...")
		agentID, cert, err = agent.LoadCredentials()
		if err != nil {
			debug.Error("Failed to load credentials after registration: %v", err)
			os.Exit(1)
		}

		// Comment out claim code after successful registration
		if err := commentOutClaimCode(); err != nil {
			debug.Warning("Failed to comment out claim code: %v", err)
		}
	} else if agentID == "" || cert == "" {
		debug.Error("Loaded credentials are empty - Agent ID: %v, Certificate: %v", agentID != "", cert != "")
		if cfg.claimCode == "" {
			debug.Error("Claim code required for first-time registration")
			os.Exit(1)
		}
		debug.Info("Starting registration process with claim code")

		// Attempt registration
		if err := agent.RegisterAgent(cfg.claimCode, urlConfig); err != nil {
			debug.Error("Failed to register agent: %v", err)
			os.Exit(1)
		}

		// Reload credentials after registration
		debug.Info("Reloading credentials after registration...")
		agentID, cert, err = agent.LoadCredentials()
		if err != nil {
			debug.Error("Failed to load credentials after registration: %v", err)
			os.Exit(1)
		}

		// Comment out claim code after successful registration
		if err := commentOutClaimCode(); err != nil {
			debug.Warning("Failed to comment out claim code: %v", err)
		}
	} else {
		debug.Info("Found existing credentials, proceeding with WebSocket connection")
		debug.Debug("Agent ID: %s", agentID)
		debug.Debug("Certificate length: %d bytes", len(cert))
	}

	// Create connection with retry
	debug.Info("Starting WebSocket connection process")
	var lastError error
	var conn *agent.Connection
	for i := 0; i < 3; i++ {
		debug.Info("Connection attempt %d of 3", i+1)
		conn, err = agent.NewConnection(urlConfig)
		if err != nil {
			lastError = err
			debug.Warning("Failed to create connection on attempt %d: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		if err := conn.Start(); err != nil {
			lastError = err
			debug.Warning("Connection attempt %d failed: %v", i+1, err)
			time.Sleep(time.Second * time.Duration(i+1))
			continue
		}
		debug.Info("Connection attempt %d successful", i+1)
		lastError = nil
		break
	}

	if lastError != nil {
		debug.Error("Failed to establish connection after 3 attempts: %v", lastError)
		os.Exit(1)
	}

	debug.Info("Agent running, press Ctrl+C to exit")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill)
	<-sigChan

	debug.Info("Shutting down agent...")
	if conn != nil {
		conn.Stop() // Stop the active connection and maintenance routines
	}
	time.Sleep(time.Second) // Give connections time to close gracefully

	debug.Info("Agent shutdown complete")
}