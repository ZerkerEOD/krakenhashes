package agent

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Handler handles agent registration and API key distribution
type Handler struct {
	agentService *services.AgentService
	config       *config.Config
}

// NewHandler creates a new instance of Handler
func NewHandler(agentService *services.AgentService, config *config.Config) *Handler {
	return &Handler{
		agentService: agentService,
		config:       config,
	}
}

// HandleRegistration handles agent registration requests
func (h *Handler) HandleRegistration(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received agent registration request")

	if r.Method != http.MethodPost {
		debug.Error("Invalid request method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req handlers.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode registration request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	debug.Debug("Registration request - Claim Code: %s, Hostname: %s, Version: %s", req.ClaimCode, req.Hostname, req.Version)

	// Register agent with version if provided
	agent, err := h.agentService.RegisterAgentWithVersion(r.Context(), req.ClaimCode, req.Hostname, req.Version, req.StorageTier, req.NetworkShareMountPath)
	if err != nil {
		debug.Error("Agent registration failed: %v", err)
		http.Error(w, "Registration failed", http.StatusBadRequest)
		return
	}
	debug.Info("Successfully registered agent with ID: %d", agent.ID)

	// Prepare response
	resp := handlers.RegistrationResponse{
		AgentID: agent.ID,
		APIKey:  agent.APIKey.String,
		Endpoints: map[string]string{
			"websocket": h.config.GetWSEndpoint(),
			"api":       h.config.GetAPIEndpoint(),
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		debug.Error("Failed to encode registration response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	debug.Info("Successfully sent registration response")
}
