package user

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// APIKeyAdminHandler handles admin operations for user API keys
type APIKeyAdminHandler struct {
	userAPIService *services.UserAPIService
}

// NewAPIKeyAdminHandler creates a new admin API key handler
func NewAPIKeyAdminHandler(userAPIService *services.UserAPIService) *APIKeyAdminHandler {
	return &APIKeyAdminHandler{
		userAPIService: userAPIService,
	}
}

// GetUserAPIKeyInfo returns metadata about a user's API key (admin only)
// GET /api/admin/users/{id}/api-key/info
func (h *APIKeyAdminHandler) GetUserAPIKeyInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get user ID from URL parameter
	vars := mux.Vars(r)
	userIDStr := vars["id"]
	if userIDStr == "" {
		http.Error(w, "User ID required", http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	debug.Debug("Admin getting API key info for user %s", userID.String())

	// Get API key info
	keyInfo, err := h.userAPIService.GetAPIKeyInfo(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get API key info for user %s: %v", userID.String(), err)
		http.Error(w, "Failed to get API key info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keyInfo)
}

// RevokeUserAPIKey revokes a user's API key (admin only)
// DELETE /api/admin/users/{id}/api-key
func (h *APIKeyAdminHandler) RevokeUserAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get user ID from URL parameter
	vars := mux.Vars(r)
	userIDStr := vars["id"]
	if userIDStr == "" {
		http.Error(w, "User ID required", http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Get admin user ID from context for audit logging
	adminIDStr := r.Context().Value("user_id").(string)
	debug.Info("Admin %s revoking API key for user %s", adminIDStr, userID.String())

	// Revoke the API key
	if err := h.userAPIService.RevokeAPIKey(r.Context(), userID); err != nil {
		debug.Error("Failed to revoke API key for user %s: %v", userID.String(), err)
		http.Error(w, "Failed to revoke API key", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	debug.Info("Admin successfully revoked API key for user %s", userID.String())
}
