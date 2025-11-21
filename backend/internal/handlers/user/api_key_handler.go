package user

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// APIKeyHandler handles user API key operations
type APIKeyHandler struct {
	userAPIService *services.UserAPIService
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(userAPIService *services.UserAPIService) *APIKeyHandler {
	return &APIKeyHandler{
		userAPIService: userAPIService,
	}
}

// GenerateAPIKey generates a new API key for the authenticated user
// POST /api/user/api-key
func (h *APIKeyHandler) GenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get user ID from context (set by JWT middleware)
	userIDStr := r.Context().Value("user_id").(string)
	if userIDStr == "" {
		debug.Warning("No user ID found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	debug.Info("Generating API key for user %s", userID.String())

	// Generate the API key
	apiKey, err := h.userAPIService.GenerateAPIKey(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to generate API key for user %s: %v", userID.String(), err)
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	// Get the key info (with created_at timestamp)
	keyInfo, err := h.userAPIService.GetAPIKeyInfo(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get API key info: %v", err)
		// Don't fail the request, just log the error
	}

	// Return the plaintext API key (only time it will be shown)
	response := map[string]interface{}{
		"api_key": apiKey,
	}
	if keyInfo != nil && keyInfo.CreatedAt != nil {
		response["created_at"] = keyInfo.CreatedAt
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)

	debug.Info("Successfully generated API key for user %s", userID.String())
}

// GetAPIKeyInfo returns metadata about the user's API key
// GET /api/user/api-key/info
func (h *APIKeyHandler) GetAPIKeyInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get user ID from context
	userIDStr := r.Context().Value("user_id").(string)
	if userIDStr == "" {
		debug.Warning("No user ID found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	debug.Debug("Getting API key info for user %s", userID.String())

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

// RevokeAPIKey revokes the user's API key
// DELETE /api/user/api-key
func (h *APIKeyHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get user ID from context
	userIDStr := r.Context().Value("user_id").(string)
	if userIDStr == "" {
		debug.Warning("No user ID found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	debug.Info("Revoking API key for user %s", userID.String())

	// Revoke the API key
	if err := h.userAPIService.RevokeAPIKey(r.Context(), userID); err != nil {
		debug.Error("Failed to revoke API key for user %s: %v", userID.String(), err)
		http.Error(w, "Failed to revoke API key", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	debug.Info("Successfully revoked API key for user %s", userID.String())
}
