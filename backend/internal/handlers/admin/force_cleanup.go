package admin

import (
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/integration"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// ForceCleanupHandler handles force cleanup requests for agents
type ForceCleanupHandler struct {
	wsIntegration *integration.JobWebSocketIntegration
}

// NewForceCleanupHandler creates a new force cleanup handler
func NewForceCleanupHandler(wsIntegration *integration.JobWebSocketIntegration) *ForceCleanupHandler {
	return &ForceCleanupHandler{
		wsIntegration: wsIntegration,
	}
}

// ForceCleanup sends a force cleanup command to an agent
func (h *ForceCleanupHandler) ForceCleanup(w http.ResponseWriter, r *http.Request) {
	// Get agent ID from URL parameter
	agentIDStr := mux.Vars(r)["id"]
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		debug.Error("Invalid agent ID: %s, error: %v", agentIDStr, err)
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Send force cleanup command
	err = h.wsIntegration.SendForceCleanup(r.Context(), agentID)
	if err != nil {
		debug.Error("Failed to send force cleanup to agent %d: %v", agentID, err)
		http.Error(w, "Failed to send force cleanup: "+err.Error(), http.StatusInternalServerError)
		return
	}

	debug.Log("Force cleanup sent successfully", map[string]interface{}{
		"agent_id": agentID,
	})

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success","message":"Force cleanup command sent"}`))
}
