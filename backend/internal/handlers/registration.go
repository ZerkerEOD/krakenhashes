package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// RegistrationRequest represents the data sent by the agent during registration
type RegistrationRequest struct {
	ClaimCode string `json:"claim_code"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version,omitempty"` // Agent version (optional for backward compatibility)
	// Seed-only storage config (WS9): the agent's locally-configured storage
	// tier + read-only mount path. Recorded as this agent's initial values; the
	// admin UI is authoritative afterwards. Both optional for backward
	// compatibility (older agents omit them → the server defaults to full_cache).
	StorageTier           string `json:"storage_tier,omitempty"`
	NetworkShareMountPath string `json:"network_share_mount_path,omitempty"`
}

// RegistrationResponse represents the data sent back to the agent after successful registration
type RegistrationResponse struct {
	AgentID       int               `json:"agent_id"`
	APIKey        string            `json:"api_key"`
	Endpoints     map[string]string `json:"endpoints"`
	Certificate   string            `json:"certificate"`    // PEM-encoded client certificate
	PrivateKey    string            `json:"private_key"`    // PEM-encoded private key
	CACertificate string            `json:"ca_certificate"` // PEM-encoded CA certificate
}

// RegistrationHandler handles agent registration and API key distribution
type RegistrationHandler struct {
	agentService *services.AgentService
	config       *config.Config
	tlsProvider  tls.Provider
}

// NewRegistrationHandler creates a new instance of RegistrationHandler
func NewRegistrationHandler(agentService *services.AgentService, config *config.Config, tlsProvider tls.Provider) *RegistrationHandler {
	return &RegistrationHandler{
		agentService: agentService,
		config:       config,
		tlsProvider:  tlsProvider,
	}
}

// HandleRegistration handles agent registration requests
func (h *RegistrationHandler) HandleRegistration(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received agent registration request")

	// Parse registration request
	var req RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode registration request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	debug.Debug("Registration request - Claim Code: %s, Hostname: %s, Version: %s, StorageTier: %s", req.ClaimCode, req.Hostname, req.Version, req.StorageTier)

	// Register agent with version if provided
	agent, err := h.agentService.RegisterAgentWithVersion(r.Context(), req.ClaimCode, req.Hostname, req.Version, req.StorageTier, req.NetworkShareMountPath)
	if err != nil {
		debug.Error("Failed to register agent: %v", err)
		http.Error(w, fmt.Sprintf("Failed to register agent: %v", err), http.StatusBadRequest)
		return
	}

	debug.Info("Successfully registered agent with ID: %d", agent.ID)

	// Get client certificate
	debug.Info("Getting client certificate for agent %d", agent.ID)
	certPEM, keyPEM, err := h.tlsProvider.GetClientCertificate()
	if err != nil {
		debug.Error("Failed to get client certificate: %v", err)
		// Delete the agent since certificate retrieval failed
		if err := h.agentService.DeleteAgent(r.Context(), agent.ID); err != nil {
			debug.Error("Failed to delete agent after certificate retrieval failure: %v", err)
		}
		http.Error(w, "Failed to get client certificate", http.StatusInternalServerError)
		return
	}

	// Get CA certificate
	debug.Info("Getting CA certificate for agent %d", agent.ID)
	caCertPEM, err := h.tlsProvider.ExportCACertificate()
	if err != nil {
		debug.Error("Failed to get CA certificate: %v", err)
		// Delete the agent since CA certificate retrieval failed
		if err := h.agentService.DeleteAgent(r.Context(), agent.ID); err != nil {
			debug.Error("Failed to delete agent after CA certificate retrieval failure: %v", err)
		}
		http.Error(w, "Failed to get CA certificate", http.StatusInternalServerError)
		return
	}

	// Mark claim code as used
	if err := h.agentService.MarkClaimCodeUsed(r.Context(), req.ClaimCode, agent.ID); err != nil {
		debug.Error("Failed to mark claim code as used: %v", err)
		// Continue since we already have the certificates
		debug.Warning("Continuing despite failure to mark claim code as used")
	}

	// Prepare response
	resp := RegistrationResponse{
		AgentID: agent.ID,
		APIKey:  agent.APIKey.String,
		Endpoints: map[string]string{
			"websocket": h.config.GetWSEndpoint(),
			"api":       h.config.GetAPIEndpoint(),
		},
		Certificate:   string(certPEM),
		PrivateKey:    string(keyPEM),
		CACertificate: string(caCertPEM),
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		debug.Error("Failed to encode registration response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully completed registration for agent %d", agent.ID)
}
