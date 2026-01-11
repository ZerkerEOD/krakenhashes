package user

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// UserSSOHandler handles user SSO-related requests
type UserSSOHandler struct {
	ssoRepo *repository.SSORepository
}

// NewUserSSOHandler creates a new user SSO handler
func NewUserSSOHandler(ssoRepo *repository.SSORepository) *UserSSOHandler {
	return &UserSSOHandler{
		ssoRepo: ssoRepo,
	}
}

// GetMyIdentities returns the linked SSO identities for the current user
func (h *UserSSOHandler) GetMyIdentities(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by RequireAuth middleware)
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok || userIDStr == "" {
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

	// Get user's linked identities
	identities, err := h.ssoRepo.GetUserIdentities(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get user identities: %v", err)
		http.Error(w, "Failed to get linked accounts", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	type IdentityResponse struct {
		ID                  string  `json:"id"`
		ProviderID          string  `json:"provider_id"`
		ProviderName        string  `json:"provider_name"`
		ProviderType        string  `json:"provider_type"`
		ExternalEmail       string  `json:"external_email,omitempty"`
		ExternalUsername    string  `json:"external_username,omitempty"`
		ExternalDisplayName string  `json:"external_display_name,omitempty"`
		LastLoginAt         *string `json:"last_login_at,omitempty"`
		CreatedAt           string  `json:"created_at"`
	}

	identityList := make([]IdentityResponse, 0, len(identities))
	for _, identity := range identities {
		resp := IdentityResponse{
			ID:                  identity.ID.String(),
			ProviderID:          identity.ProviderID.String(),
			ProviderName:        identity.ProviderName,
			ProviderType:        string(identity.ProviderType),
			ExternalEmail:       identity.ExternalEmail,
			ExternalUsername:    identity.ExternalUsername,
			ExternalDisplayName: identity.ExternalDisplayName,
			CreatedAt:           identity.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if identity.LastLoginAt != nil {
			formatted := identity.LastLoginAt.Format("2006-01-02T15:04:05Z")
			resp.LastLoginAt = &formatted
		}
		identityList = append(identityList, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"identities": identityList,
	})
}

// UnlinkMyIdentity removes a linked SSO identity from the current user
func (h *UserSSOHandler) UnlinkMyIdentity(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by RequireAuth middleware)
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok || userIDStr == "" {
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

	// Get identity ID from URL
	vars := mux.Vars(r)
	identityIDStr := vars["identityId"]
	identityID, err := uuid.Parse(identityIDStr)
	if err != nil {
		debug.Error("Invalid identity ID format: %v", err)
		http.Error(w, "Invalid identity ID", http.StatusBadRequest)
		return
	}

	// Get the identity to verify ownership
	identity, err := h.ssoRepo.GetUserIdentityByID(r.Context(), identityID)
	if err != nil {
		debug.Error("Failed to get identity: %v", err)
		http.Error(w, "Failed to get identity", http.StatusInternalServerError)
		return
	}

	if identity == nil {
		http.Error(w, "Identity not found", http.StatusNotFound)
		return
	}

	// Verify the identity belongs to the current user
	if identity.UserID != userID {
		debug.Warning("User %s attempted to unlink identity %s belonging to user %s",
			userID, identityID, identity.UserID)
		http.Error(w, "Identity not found", http.StatusNotFound)
		return
	}

	// Delete the identity
	if err := h.ssoRepo.DeleteUserIdentity(r.Context(), identityID); err != nil {
		debug.Error("Failed to unlink identity: %v", err)
		http.Error(w, "Failed to unlink account", http.StatusInternalServerError)
		return
	}

	debug.Info("User %s unlinked SSO identity %s (provider: %s)",
		userID, identityID, identity.ProviderID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Account unlinked successfully",
	})
}
