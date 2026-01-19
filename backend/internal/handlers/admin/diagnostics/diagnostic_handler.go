package diagnostics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/diagnostic"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// DiagnosticHandler handles diagnostic-related requests
type DiagnosticHandler struct {
	diagnosticService *diagnostic.DiagnosticService
	wsHandler         *wshandler.Handler
}

// NewDiagnosticHandler creates a new diagnostic handler
func NewDiagnosticHandler(diagnosticService *diagnostic.DiagnosticService, wsHandler *wshandler.Handler) *DiagnosticHandler {
	return &DiagnosticHandler{
		diagnosticService: diagnosticService,
		wsHandler:         wsHandler,
	}
}

// AgentDebugStatusResponse represents the debug status response for all agents
type AgentDebugStatusResponse struct {
	Agents []wshandler.AgentDebugStatus `json:"agents"`
	Count  int                          `json:"count"`
}

// DownloadDiagnostics handles requests to download a diagnostic package
func (h *DiagnosticHandler) DownloadDiagnostics(w http.ResponseWriter, r *http.Request) {
	debug.Info("Diagnostic download requested")

	// Check for include_agent_logs query param
	includeAgentLogs := r.URL.Query().Get("include_agent_logs") == "true"

	// Check for include_nginx_logs query param (contains sensitive data)
	includeNginxLogs := r.URL.Query().Get("include_nginx_logs") == "true"

	// Check for include_postgres_logs query param (contains sensitive data)
	includePostgresLogs := r.URL.Query().Get("include_postgres_logs") == "true"

	// Check for hours_back query param (default: 1 hour)
	hoursBack := 1
	if hb := r.URL.Query().Get("hours_back"); hb != "" {
		if val, err := strconv.Atoi(hb); err == nil && val > 0 {
			hoursBack = val
		}
	}

	// Generate the diagnostic package
	ctx := r.Context()
	zipData, err := h.diagnosticService.PackageDiagnostics(ctx, includeAgentLogs, includeNginxLogs, includePostgresLogs, hoursBack)
	if err != nil {
		debug.Error("Failed to generate diagnostic package: %v", err)
		http.Error(w, "Failed to generate diagnostic package: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate filename with timestamp
	filename := fmt.Sprintf("krakenhashes-diagnostics-%s.zip", time.Now().Format("20060102-150405"))

	// Set headers for file download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(zipData)))

	// Write the ZIP data
	if _, err := w.Write(zipData); err != nil {
		debug.Error("Failed to write diagnostic package: %v", err)
		return
	}

	debug.Info("Diagnostic package downloaded: %s (%d bytes)", filename, len(zipData))
}

// GetSystemInfo returns system diagnostic information (lightweight, for page display)
func (h *DiagnosticHandler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	sysInfo, err := h.diagnosticService.GetSystemInfoOnly(r.Context())
	if err != nil {
		debug.Error("Failed to collect system info: %v", err)
		http.Error(w, "Failed to collect system info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"system_info":    sysInfo,
		"generated_at":   time.Now(),
		"version":        "1.0.0",
		"errors":         []string{},
		"agent_statuses": len(wshandler.GetAllAgentDebugStatuses()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetServerDebugStatus returns the current server debug configuration
func (h *DiagnosticHandler) GetServerDebugStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"enabled": debug.IsDebugEnabled(),
		"level":   debug.GetLogLevelName(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ToggleServerDebug toggles server debug mode
func (h *DiagnosticHandler) ToggleServerDebug(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enable bool   `json:"enable"`
		Level  string `json:"level,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Apply changes immediately (hot-reload)
	debug.SetEnabled(req.Enable)
	if req.Level != "" {
		if level, ok := debug.ParseLevel(req.Level); ok {
			debug.SetLogLevel(level)
		}
	}

	// Log the change (will only show if debug is enabled)
	if req.Enable {
		debug.Info("Server debug mode enabled via admin panel, level: %s", debug.GetLogLevelName())
	}

	response := map[string]interface{}{
		"success": true,
		"enabled": debug.IsDebugEnabled(),
		"level":   debug.GetLogLevelName(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAgentDebugStatuses returns debug status for all agents
func (h *DiagnosticHandler) GetAgentDebugStatuses(w http.ResponseWriter, r *http.Request) {
	statuses := wshandler.GetAllAgentDebugStatuses()

	// Convert map to slice for easier frontend consumption
	agents := make([]wshandler.AgentDebugStatus, 0, len(statuses))
	for _, status := range statuses {
		if status != nil {
			agents = append(agents, *status)
		}
	}

	response := AgentDebugStatusResponse{
		Agents: agents,
		Count:  len(agents),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAgentDebugStatus returns debug status for a specific agent
func (h *DiagnosticHandler) GetAgentDebugStatus(w http.ResponseWriter, r *http.Request) {
	agentIDStr := mux.Vars(r)["id"]
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	status := wshandler.GetAgentDebugStatus(agentID)
	if status == nil {
		http.Error(w, "Agent debug status not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// ToggleAgentDebug toggles debug mode for a specific agent
func (h *DiagnosticHandler) ToggleAgentDebug(w http.ResponseWriter, r *http.Request) {
	agentIDStr := mux.Vars(r)["id"]
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Send debug toggle command
	if err := h.wsHandler.SendDebugToggle(agentID, req.Enable); err != nil {
		debug.Error("Failed to toggle debug for agent %d: %v", agentID, err)
		http.Error(w, "Failed to toggle debug: "+err.Error(), http.StatusInternalServerError)
		return
	}

	debug.Info("Debug toggle sent to agent %d: enable=%v", agentID, req.Enable)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"message":  fmt.Sprintf("Debug toggle command sent to agent %d", agentID),
		"enable":   req.Enable,
		"agent_id": agentID,
	})
}

// RequestAgentLogs requests logs from a specific agent
func (h *DiagnosticHandler) RequestAgentLogs(w http.ResponseWriter, r *http.Request) {
	agentIDStr := mux.Vars(r)["id"]
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Parse optional query params
	hoursBack := 24
	if hb := r.URL.Query().Get("hours_back"); hb != "" {
		if val, err := strconv.Atoi(hb); err == nil {
			hoursBack = val
		}
	}
	includeAll := r.URL.Query().Get("include_all") == "true"

	requestID := uuid.New().String()

	// Register callback for response
	responseCh := wshandler.RegisterLogDataCallback(requestID)
	defer wshandler.UnregisterLogDataCallback(requestID)

	// Send log request
	if err := h.wsHandler.SendLogRequest(agentID, requestID, hoursBack, includeAll); err != nil {
		debug.Error("Failed to request logs from agent %d: %v", agentID, err)
		http.Error(w, "Failed to request logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case response := <-responseCh:
		if response == nil {
			http.Error(w, "No response received from agent", http.StatusInternalServerError)
			return
		}
		if response.Error != "" {
			http.Error(w, "Agent error: "+response.Error, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	case <-time.After(30 * time.Second):
		http.Error(w, "Request timed out", http.StatusGatewayTimeout)
	case <-r.Context().Done():
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
	}
}

// PurgeAgentLogs requests log purge from a specific agent
func (h *DiagnosticHandler) PurgeAgentLogs(w http.ResponseWriter, r *http.Request) {
	agentIDStr := mux.Vars(r)["id"]
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	requestID := uuid.New().String()

	// Register callback for response
	responseCh := wshandler.RegisterLogPurgeCallback(requestID)
	defer wshandler.UnregisterLogPurgeCallback(requestID)

	// Send log purge command
	if err := h.wsHandler.SendLogPurge(agentID, requestID); err != nil {
		debug.Error("Failed to send log purge to agent %d: %v", agentID, err)
		http.Error(w, "Failed to send log purge: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case response := <-responseCh:
		if response == nil {
			http.Error(w, "No response received from agent", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	case <-time.After(10 * time.Second):
		// Still return success - the command was sent
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "pending",
			"message":    "Log purge command sent, response pending",
			"request_id": requestID,
		})
	case <-r.Context().Done():
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
	}
}

// ToggleAllAgentsDebug toggles debug mode for all connected agents
func (h *DiagnosticHandler) ToggleAllAgentsDebug(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get all connected agents
	connectedAgents := h.wsHandler.GetConnectedAgents()
	if len(connectedAgents) == 0 {
		http.Error(w, "No connected agents", http.StatusNotFound)
		return
	}

	// Send debug toggle to all agents
	var succeeded, failed int
	for _, agentID := range connectedAgents {
		if err := h.wsHandler.SendDebugToggle(agentID, req.Enable); err != nil {
			debug.Warning("Failed to toggle debug for agent %d: %v", agentID, err)
			failed++
		} else {
			succeeded++
		}
	}

	debug.Info("Debug toggle sent to all agents: enable=%v, succeeded=%d, failed=%d",
		req.Enable, succeeded, failed)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"message":   fmt.Sprintf("Debug toggle sent to %d agents", succeeded),
		"enable":    req.Enable,
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// GetLogStats returns statistics for all server log directories
func (h *DiagnosticHandler) GetLogStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.diagnosticService.GetLogStats()
	if err != nil {
		debug.Error("Failed to get log stats: %v", err)
		http.Error(w, "Failed to get log stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// PurgeServerLogs deletes all log files in the specified directory
func (h *DiagnosticHandler) PurgeServerLogs(w http.ResponseWriter, r *http.Request) {
	directory := mux.Vars(r)["directory"]

	// Validate directory parameter
	validDirs := map[string]bool{"backend": true, "nginx": true, "postgres": true, "all": true}
	if !validDirs[directory] {
		http.Error(w, "Invalid directory. Must be one of: backend, nginx, postgres, all", http.StatusBadRequest)
		return
	}

	debug.Info("Purging server logs for directory: %s", directory)

	if err := h.diagnosticService.PurgeLogs(directory); err != nil {
		debug.Error("Failed to purge logs for %s: %v", directory, err)
		http.Error(w, "Failed to purge logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   fmt.Sprintf("Purged logs in %s directory", directory),
		"directory": directory,
	})
}

// CheckPostgresLogsExist checks if PostgreSQL log files exist
func (h *DiagnosticHandler) CheckPostgresLogsExist(w http.ResponseWriter, r *http.Request) {
	exists := h.diagnosticService.CheckPostgresLogsExist()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exists": exists,
	})
}

// CheckNginxLogsExist checks if Nginx log files exist
func (h *DiagnosticHandler) CheckNginxLogsExist(w http.ResponseWriter, r *http.Request) {
	exists := h.diagnosticService.CheckNginxLogsExist()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exists": exists,
	})
}

// ReloadNginx triggers a hot-reload of nginx configuration
func (h *DiagnosticHandler) ReloadNginx(w http.ResponseWriter, r *http.Request) {
	debug.Info("Nginx reload requested via admin panel")

	if err := h.diagnosticService.ReloadNginx(); err != nil {
		debug.Error("Failed to reload nginx: %v", err)
		http.Error(w, "Failed to reload nginx: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Nginx configuration reloaded successfully",
	})
}
