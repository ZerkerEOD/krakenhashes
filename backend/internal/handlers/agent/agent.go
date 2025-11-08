package agent

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

type AgentHandler struct {
	service *services.AgentService
}

func NewAgentHandler(service *services.AgentService) *AgentHandler {
	return &AgentHandler{service: service}
}

// ListAgents handles listing all agents
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	debug.Info("Listing agents")

	// Parse query parameters into filters
	filters := make(map[string]interface{})
	if status := r.URL.Query().Get("status"); status != "" {
		filters["status"] = status
	}
	if team := r.URL.Query().Get("team"); team != "" {
		filters["team"] = team
	}

	agents, err := h.service.ListAgents(r.Context(), filters)
	if err != nil {
		debug.Error("failed to list agents: %v", err)
		http.Error(w, "Failed to list agents", http.StatusInternalServerError)
		return
	}

	// Set content type and encode response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agents); err != nil {
		debug.Error("failed to encode agents: %v", err)
		http.Error(w, "Failed to encode agents", http.StatusInternalServerError)
		return
	}
}

// GetAgent retrieves an agent by ID
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse agent ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Get agent
	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// DeleteAgent deletes an agent
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse agent ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Delete agent
	if err := h.service.DeleteAgent(r.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to delete agent %d: %v", id, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RegisterAgent handles the initial registration of an agent using a claim code
func (h *AgentHandler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode registration request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate claim code and register agent with version if provided
	agent, err := h.service.RegisterAgentWithVersion(r.Context(), req.ClaimCode, req.Hostname, req.Version)
	if err != nil {
		debug.Error("Failed to register agent: %v", err)
		http.Error(w, "Registration failed", http.StatusUnauthorized)
		return
	}

	// Generate a temporary download token
	downloadToken, err := h.service.CreateDownloadToken(r.Context(), agent.ID)
	if err != nil {
		debug.Error("Failed to create download token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := RegistrationResponse{
		AgentID:       agent.ID,
		DownloadToken: downloadToken,
		Endpoints: map[string]string{
			"cert":      "/api/agent/cert",
			"config":    "/api/agent/config",
			"websocket": "/ws/agent",
			"upload":    "/api/agent/upload",
			"download":  "/api/agent/download",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAgentDevices retrieves devices for a specific agent
func (h *AgentHandler) GetAgentDevices(w http.ResponseWriter, r *http.Request) {
	debug.Info("Getting agent devices")

	// Parse agent ID from URL
	vars := mux.Vars(r)
	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Get devices for agent
	devices, err := h.service.GetAgentDevices(agentID)
	if err != nil {
		debug.Error("Failed to get agent devices: %v", err)
		http.Error(w, "Failed to get agent devices", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(devices); err != nil {
		debug.Error("Failed to encode devices: %v", err)
		http.Error(w, "Failed to encode devices", http.StatusInternalServerError)
		return
	}
}

// UpdateDeviceStatus updates the enabled status of a device
func (h *AgentHandler) UpdateDeviceStatus(w http.ResponseWriter, r *http.Request) {
	debug.Info("Updating device status")

	// Parse agent ID and device ID from URL
	vars := mux.Vars(r)
	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	deviceID, err := strconv.Atoi(vars["deviceId"])
	if err != nil {
		http.Error(w, "Invalid device ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update device status in database
	if err := h.service.UpdateDeviceStatus(agentID, deviceID, req.Enabled); err != nil {
		debug.Error("Failed to update device status: %v", err)
		http.Error(w, "Failed to update device status", http.StatusInternalServerError)
		return
	}

	// TODO: Send device update message to agent via WebSocket
	// The WebSocket service needs to be injected into the handler
	// to send the device update message to the connected agent

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"device_id": deviceID,
		"enabled":   req.Enabled,
	})
}

// GetAgentWithDevices retrieves an agent with its devices
func (h *AgentHandler) GetAgentWithDevices(w http.ResponseWriter, r *http.Request) {
	debug.Info("Getting agent with devices")

	// Parse agent ID from URL
	vars := mux.Vars(r)
	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Get agent
	agent, err := h.service.GetAgent(r.Context(), agentID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get agent: %v", err)
		http.Error(w, "Failed to get agent", http.StatusInternalServerError)
		return
	}

	// Get devices
	devices, err := h.service.GetAgentDevices(agentID)
	if err != nil {
		debug.Error("Failed to get agent devices: %v", err)
		http.Error(w, "Failed to get agent devices", http.StatusInternalServerError)
		return
	}

	// Create response with agent and devices
	response := map[string]interface{}{
		"agent":   agent,
		"devices": devices,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// GetAgentMetrics retrieves performance metrics for an agent
func (h *AgentHandler) GetAgentMetrics(w http.ResponseWriter, r *http.Request) {
	debug.Info("Getting agent metrics")
	
	// Parse agent ID from URL
	vars := mux.Vars(r)
	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}
	
	// Parse query parameters
	timeRange := r.URL.Query().Get("timeRange")
	if timeRange == "" {
		timeRange = "1h" // Default to 1 hour
	}
	
	metricsParam := r.URL.Query().Get("metrics")
	if metricsParam == "" {
		metricsParam = "temperature,utilization,fanspeed,hashrate"
	}
	
	// Get metrics from service
	metrics, err := h.service.GetAgentDeviceMetrics(r.Context(), agentID, timeRange, metricsParam)
	if err != nil {
		debug.Error("Failed to get agent metrics: %v", err)
		http.Error(w, "Failed to get agent metrics", http.StatusInternalServerError)
		return
	}
	
	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		debug.Error("Failed to encode metrics: %v", err)
		http.Error(w, "Failed to encode metrics", http.StatusInternalServerError)
		return
	}
}

// UpdateAgent updates agent settings
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse agent ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		IsEnabled       bool    `json:"isEnabled"`
		OwnerID         *string `json:"ownerId"`
		ExtraParameters string  `json:"extraParameters"`
		BinaryVersionID *int64  `json:"binaryVersionId"`
		BinaryOverride  bool    `json:"binaryOverride"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update agent
	if err := h.service.UpdateAgent(r.Context(), id, req.IsEnabled, req.OwnerID, req.ExtraParameters, req.BinaryVersionID, req.BinaryOverride); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to update agent: %v", err)
		http.Error(w, "Failed to update agent", http.StatusInternalServerError)
		return
	}

	// Send success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      id,
	})
}

// GetUserAgents retrieves agents owned by the authenticated user with their current task info
func (h *AgentHandler) GetUserAgents(w http.ResponseWriter, r *http.Request) {
	debug.Info("Getting user agents with task info")

	// Get user ID from context
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		debug.Error("No user ID in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get agents with current task info for the user
	agents, err := h.service.GetUserAgentsWithTasks(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get user agents with tasks: %v", err)
		http.Error(w, "Failed to get agents", http.StatusInternalServerError)
		return
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agents); err != nil {
		debug.Error("Failed to encode agents: %v", err)
		http.Error(w, "Failed to encode agents", http.StatusInternalServerError)
		return
	}
}

// ClearBusyStatus manually clears the busy_status metadata for an agent
// This is a manual override for when agents get stuck in busy state
func (h *AgentHandler) ClearBusyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse agent ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	debug.Info("Clearing busy status for agent %d", id)

	// Clear the agent's busy status
	if err := h.service.ClearAgentBusyStatus(r.Context(), id); err != nil {
		debug.Error("Failed to clear agent busy status: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Agent busy status cleared successfully",
	})
}
