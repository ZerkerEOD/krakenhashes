package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// ConfigResponse represents the configuration sent to agents
type ConfigResponse struct {
	WebSocket struct {
		WriteWait  string `json:"write_wait"`
		PongWait   string `json:"pong_wait"`
		PingPeriod string `json:"ping_period"`
	} `json:"websocket"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
	ServerVersion     string `json:"server_version"`
	// MTLSEnabled tells the agent whether the server verifies per-agent client
	// certificates on the HTTPS listener; the agent uses it to decide whether to
	// migrate to a per-agent certificate.
	MTLSEnabled bool `json:"mtls_enabled"`
}

// mtlsEnabled records whether agent mTLS is active (set once at startup).
var mtlsEnabled bool

// SetMTLSEnabled records whether the server has agent mTLS active. Called once
// at startup; read by GetConfig so agents learn to migrate to a per-agent cert.
func SetMTLSEnabled(v bool) { mtlsEnabled = v }

// GetConfig returns the current server configuration for agents
// This is a public endpoint (no authentication required) so agents
// can fetch configuration before establishing WebSocket connections
func GetConfig(w http.ResponseWriter, r *http.Request) {
	debug.Info("Agent configuration request from %s", r.RemoteAddr)

	// Get WebSocket timing from environment variables
	writeWait := os.Getenv("KH_WRITE_WAIT")
	if writeWait == "" {
		writeWait = "45s" // Default
	}

	pongWait := os.Getenv("KH_PONG_WAIT")
	if pongWait == "" {
		pongWait = "60s" // Default
	}

	pingPeriod := os.Getenv("KH_PING_PERIOD")
	if pingPeriod == "" {
		pingPeriod = "54s" // Default (90% of pong wait)
	}

	// Get heartbeat interval
	heartbeatInterval := 5 // Default to 5 seconds
	if envVal := os.Getenv("HEARTBEAT_INTERVAL"); envVal != "" {
		// Try to parse the environment variable
		var interval int
		if _, err := fmt.Sscanf(envVal, "%d", &interval); err == nil && interval > 0 {
			heartbeatInterval = interval
		}
	}

	// Get server version
	serverVersion := os.Getenv("SERVER_VERSION")
	if serverVersion == "" {
		serverVersion = "0.11.0" // Default to current version
	}

	// Build response
	response := ConfigResponse{
		HeartbeatInterval: heartbeatInterval,
		ServerVersion:     serverVersion,
		MTLSEnabled:       mtlsEnabled,
	}
	response.WebSocket.WriteWait = writeWait
	response.WebSocket.PongWait = pongWait
	response.WebSocket.PingPeriod = pingPeriod

	// Log the configuration being sent
	debug.Debug("Sending agent configuration: WriteWait=%s, PongWait=%s, PingPeriod=%s, HeartbeatInterval=%d",
		writeWait, pongWait, pingPeriod, heartbeatInterval)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode agent configuration response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
