/*
 * Package agent implements agent registration and management functionality
 * for the KrakenHashes agent service.
 */
package agent

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/version"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// Add file lock management
var (
	fileLocks     = make(map[string]*sync.Mutex)
	fileLocksLock sync.Mutex
	lockTimeout   = 5 * time.Minute
	lastUsed      = make(map[string]time.Time)
)

// FileWithChecksum represents a file with its checksum
type FileWithChecksum struct {
	Data     []byte
	Checksum string
}

// getFileLock returns a mutex for the given file path
func getFileLock(path string) *sync.Mutex {
	fileLocksLock.Lock()
	defer fileLocksLock.Unlock()

	// Cleanup stale locks first
	cleanupStaleLocks()

	if lock, exists := fileLocks[path]; exists {
		lastUsed[path] = time.Now()
		return lock
	}

	lock := &sync.Mutex{}
	fileLocks[path] = lock
	lastUsed[path] = time.Now()
	return lock
}

// calculateChecksum generates a SHA-256 hash of the data
func calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// verifyChecksum validates the data against its checksum
func verifyChecksum(data []byte, expectedChecksum string) bool {
	actualChecksum := calculateChecksum(data)
	return actualChecksum == expectedChecksum
}

// RegistrationRequest represents the data sent to register an agent
type RegistrationRequest struct {
	ClaimCode string `json:"claim_code"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version"` // Agent version
}

// RegistrationResponse represents the server's response to registration
type RegistrationResponse struct {
	AgentID       int               `json:"agent_id"`
	DownloadToken string            `json:"download_token"`
	Endpoints     map[string]string `json:"endpoints"`
	APIKey        string            `json:"api_key"`
	Certificate   string            `json:"certificate"`    // PEM-encoded client certificate
	PrivateKey    string            `json:"private_key"`    // PEM-encoded private key
	CACertificate string            `json:"ca_certificate"` // PEM-encoded CA certificate
}

// getHostname retrieves the system hostname for agent identification
func getHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		debug.Error("Failed to get system hostname: %v", err)
		return "", err
	}

	if hostname == "" {
		debug.Error("System returned empty hostname")
		return "", fmt.Errorf("empty hostname")
	}

	debug.Info("Retrieved hostname: %s", hostname)
	return hostname, nil
}

// storeCredentials stores the agent ID, API key, and certificates
func storeCredentials(agentID int, apiKey string, cert, key, caCert []byte) error {
	debug.Info("Storing agent credentials")

	// Get config directory
	configDir := config.GetConfigDir()
	if err := os.MkdirAll(configDir, 0755); err != nil {
		debug.Error("Failed to create config directory: %v", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Store agent ID and API key using auth package
	if err := auth.SaveAgentKey(configDir, apiKey, fmt.Sprintf("%d", agentID)); err != nil {
		debug.Error("Failed to save agent key: %v", err)
		return fmt.Errorf("failed to save agent key: %w", err)
	}

	// Store CA certificate
	caCertPath := filepath.Join(configDir, "ca.crt")
	caCertLock := getFileLock(caCertPath)
	caCertLock.Lock()
	defer caCertLock.Unlock()

	if err := os.WriteFile(caCertPath, caCert, 0644); err != nil {
		debug.Error("Failed to write CA certificate: %v", err)
		return fmt.Errorf("failed to write CA certificate: %w", err)
	}

	// Store client certificate
	certPath := filepath.Join(configDir, "client.crt")
	certLock := getFileLock(certPath)
	certLock.Lock()
	defer certLock.Unlock()

	if err := os.WriteFile(certPath, cert, 0644); err != nil {
		debug.Error("Failed to write client certificate: %v", err)
		return fmt.Errorf("failed to write client certificate: %w", err)
	}

	// Store private key
	keyPath := filepath.Join(configDir, "client.key")
	keyLock := getFileLock(keyPath)
	keyLock.Lock()
	defer keyLock.Unlock()

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		debug.Error("Failed to write private key: %v", err)
		return fmt.Errorf("failed to write private key: %w", err)
	}

	debug.Info("Successfully stored agent credentials")
	return nil
}

// LoadCredentials loads the agent ID, API key, and certificates from disk
func LoadCredentials() (string, string, error) {
	debug.Info("Loading agent credentials")

	// Get config directory
	configDir := config.GetConfigDir()

	// Print current working directory for debugging
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get current working directory: %v", err)
	} else {
		debug.Info("Current working directory: %s", cwd)
	}

	// Check if config directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		debug.Error("Config directory does not exist: %s", configDir)
	} else {
		debug.Info("Config directory exists: %s", configDir)
		// List files in config directory
		files, err := os.ReadDir(configDir)
		if err != nil {
			debug.Error("Failed to read config directory: %v", err)
		} else {
			debug.Info("Files in config directory:")
			for _, file := range files {
				debug.Info("- %s", file.Name())
			}
		}
	}

	// Load credentials from agent.key file using auth package
	debug.Info("Loading credentials from agent.key file")
	apiKey, agentID, err := auth.LoadAgentKey(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			debug.Info("No existing credentials found")
			return "", "", nil
		}
		debug.Error("Failed to load agent key: %v", err)
		return "", "", fmt.Errorf("failed to load agent key: %w", err)
	}

	// Check if certificates exist
	certPath := filepath.Join(configDir, "client.crt")
	keyPath := filepath.Join(configDir, "client.key")
	debug.Info("Looking for client certificate at: %s", certPath)
	debug.Info("Looking for private key at: %s", keyPath)

	// Check if certificate files exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		debug.Error("Client certificate file does not exist: %s", certPath)
	} else {
		debug.Info("Client certificate file exists: %s", certPath)
	}

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		debug.Error("Private key file does not exist: %s", keyPath)
	} else {
		debug.Info("Private key file exists: %s", keyPath)
	}

	if _, err := os.Stat(certPath); err != nil {
		debug.Error("Client certificate not found: %v", err)
		return "", "", fmt.Errorf("client certificate not found: %w", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		debug.Error("Private key not found: %v", err)
		return "", "", fmt.Errorf("private key not found: %w", err)
	}

	debug.Info("Successfully loaded agent credentials")
	return agentID, apiKey, nil
}

// downloadCACertificate downloads the CA certificate from the HTTP endpoint
func downloadCACertificate(urlConfig *config.URLConfig) error {
	debug.Info("Downloading CA certificate")

	// Create HTTP client
	client := &http.Client{}

	// Get CA certificate URL
	caURL := urlConfig.GetCACertURL()
	debug.Info("Downloading CA certificate from: %s", caURL)

	// Make request
	resp, err := client.Get(caURL)
	if err != nil {
		debug.Error("Failed to download CA certificate: %v", err)
		return fmt.Errorf("failed to download CA certificate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		debug.Error("Server returned non-200 status: %d", resp.StatusCode)
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// Read response body
	caCert, err := io.ReadAll(resp.Body)
	if err != nil {
		debug.Error("Failed to read CA certificate: %v", err)
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	// Create config directory if it doesn't exist
	configDir := config.GetConfigDir()
	if err := os.MkdirAll(configDir, 0755); err != nil {
		debug.Error("Failed to create config directory: %v", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Save CA certificate
	caCertPath := filepath.Join(configDir, "ca.crt")
	if err := os.WriteFile(caCertPath, caCert, 0644); err != nil {
		debug.Error("Failed to write CA certificate: %v", err)
		return fmt.Errorf("failed to write CA certificate: %w", err)
	}

	debug.Info("Successfully downloaded and saved CA certificate")
	return nil
}

// sendRegistrationRequest sends the registration request to the server
func sendRegistrationRequest(urlConfig *config.URLConfig, req *RegistrationRequest) (*http.Response, error) {
	// Download CA certificate first
	debug.Info("Downloading CA certificate before registration")
	console.Status("Downloading CA certificate...")
	if err := downloadCACertificate(urlConfig); err != nil {
		debug.Error("Failed to download CA certificate: %v", err)
		console.Error("Failed to download CA certificate: %v", err)
		return nil, fmt.Errorf("failed to download CA certificate: %w", err)
	}
	console.Success("CA certificate downloaded")

	// Now load the downloaded CA certificate
	debug.Info("Loading CA certificate for registration request")
	certPool, err := loadCACertificate(urlConfig)
	if err != nil {
		debug.Error("Failed to load CA certificate: %v", err)
		return nil, fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Prepare request body
	debug.Debug("Preparing registration request body")
	body, err := json.Marshal(req)
	if err != nil {
		debug.Error("Failed to marshal registration request: %v", err)
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	// Create request
	url := urlConfig.GetRegistrationURL()
	debug.Info("Sending registration request to %s", url)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		debug.Error("Failed to create registration request: %v", err)
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Create HTTP client with TLS config
	//
	// Proxy is explicit because a custom Transport defaults to a nil Proxy.
	// Cloud agents register through a userspace VPN's local SOCKS5 proxy.
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		},
	}

	// Send request
	resp, err := client.Do(httpReq)
	if err != nil {
		debug.Error("Registration request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		debug.Error("Registration request returned non-200 status: %d", resp.StatusCode)
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	debug.Info("Registration request successful")
	return resp, nil
}

// RegisterAgent handles the agent registration process using a claim code.
// It will:
// 1. Send the claim code to the server
// 2. Receive agent ID and API key
// 3. Store the credentials locally
func RegisterAgent(claimCode string, urlConfig *config.URLConfig) error {
	debug.Info("Starting agent registration process")

	// Prepare registration request
	debug.Info("Preparing registration request")
	hostname, err := getHostname()
	if err != nil {
		hostname = "unknown"
		debug.Warning("Could not get hostname, using 'unknown': %v", err)
	}

	// Send registration request with version
	agentVersion := version.GetVersion()
	debug.Info("Sending registration request for hostname: %s with version: %s", hostname, agentVersion)
	resp, err := sendRegistrationRequest(urlConfig, &RegistrationRequest{
		ClaimCode: claimCode,
		Hostname:  hostname,
		Version:   agentVersion,
	})
	if err != nil {
		debug.Error("Registration request failed: %v", err)
		return fmt.Errorf("registration request failed: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	debug.Debug("Parsing registration response")
	var regResp RegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		debug.Error("Failed to decode registration response: %v", err)
		return fmt.Errorf("failed to decode registration response: %v", err)
	}

	// Decode certificates and private key
	debug.Debug("Decoding certificates from response")
	certBlock, _ := pem.Decode([]byte(regResp.Certificate))
	if certBlock == nil {
		debug.Error("Failed to decode client certificate PEM")
		return fmt.Errorf("failed to decode certificate PEM")
	}

	keyBlock, _ := pem.Decode([]byte(regResp.PrivateKey))
	if keyBlock == nil {
		debug.Error("Failed to decode private key PEM")
		return fmt.Errorf("failed to decode private key PEM")
	}

	caCertBlock, _ := pem.Decode([]byte(regResp.CACertificate))
	if caCertBlock == nil {
		debug.Error("Failed to decode CA certificate PEM")
		return fmt.Errorf("failed to decode CA certificate PEM")
	}

	// Store credentials and certificates
	debug.Info("Storing agent credentials and certificates")
	console.Status("Storing credentials...")
	if err := storeCredentials(regResp.AgentID, regResp.APIKey, []byte(regResp.Certificate), []byte(regResp.PrivateKey), []byte(regResp.CACertificate)); err != nil {
		debug.Error("Failed to store agent credentials: %v", err)
		console.Error("Failed to store credentials: %v", err)
		return fmt.Errorf("failed to store credentials: %v", err)
	}
	console.Success("Credentials stored successfully")

	// Initialize data directories (just create them, don't populate yet)
	debug.Info("Initializing data directories")
	console.Status("Initializing data directories...")
	_, err = config.GetDataDirs()
	if err != nil {
		debug.Error("Failed to initialize data directories: %v", err)
		console.Error("Failed to initialize data directories: %v", err)
		return fmt.Errorf("failed to initialize data directories: %w", err)
	}

	// File synchronization will be handled by the WebSocket connection
	// The backend will request the agent's file list and send commands to download missing files
	debug.Info("File synchronization will be handled by the WebSocket connection")
	console.Info("File synchronization will occur after connection is established")

	debug.Info("Agent registration completed successfully")
	return nil
}

// cleanupStaleLocks removes locks that haven't been used for a while
func cleanupStaleLocks() {
	now := time.Now()
	for path, lastUse := range lastUsed {
		if now.Sub(lastUse) > lockTimeout {
			delete(fileLocks, path)
			delete(lastUsed, path)
		}
	}
}
