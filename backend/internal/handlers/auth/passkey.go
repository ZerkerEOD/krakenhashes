package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// getWebAuthnInstance creates a WebAuthn instance from current database settings
// This is created per-request to support hot-reloading of settings
func (h *Handler) getWebAuthnInstance() (*webauthn.WebAuthn, error) {
	settings, err := h.db.GetWebAuthnSettings()
	if err != nil {
		return nil, err
	}

	if !settings.IsConfigured() {
		return nil, db.ErrWebAuthnNotConfigured
	}

	displayName := settings.RPDisplayName
	if displayName == "" {
		displayName = "KrakenHashes"
	}

	config := &webauthn.Config{
		RPDisplayName: displayName,
		RPID:          settings.RPID,
		RPOrigins:     settings.RPOrigins,
		Timeouts: webauthn.TimeoutsConfig{
			Login: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    120000, // 2 minutes
				TimeoutUVD: 120000,
			},
			Registration: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    120000,
				TimeoutUVD: 120000,
			},
		},
		AttestationPreference: protocol.PreferNoAttestation,
	}

	return webauthn.New(config)
}

// buildWebAuthnUser creates a WebAuthnUser from a database user
func (h *Handler) buildWebAuthnUser(userID uuid.UUID, username, email string) (*models.WebAuthnUser, error) {
	passkeys, err := h.db.GetUserPasskeys(userID)
	if err != nil {
		return nil, err
	}

	credentials := make([]webauthn.Credential, len(passkeys))
	for i, p := range passkeys {
		transports := make([]protocol.AuthenticatorTransport, len(p.Transports))
		for j, t := range p.Transports {
			transports[j] = protocol.AuthenticatorTransport(t)
		}

		credentials[i] = webauthn.Credential{
			ID:              p.CredentialID,
			PublicKey:       p.PublicKey,
			AttestationType: "none",
			Authenticator: webauthn.Authenticator{
				AAGUID:    p.AAGUID,
				SignCount: p.SignCount,
			},
			Transport: transports,
			Flags: webauthn.CredentialFlags{
				BackupEligible: p.BackupEligible,
				BackupState:    p.BackupState,
			},
		}
	}

	return &models.WebAuthnUser{
		ID:          userID,
		Username:    username,
		Email:       email,
		Credentials: credentials,
	}, nil
}

// BeginPasskeyRegistration starts the passkey registration process
func (h *Handler) BeginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Context().Value("user_id").(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Get WebAuthn instance
	webAuthn, err := h.getWebAuthnInstance()
	if err != nil {
		if err == db.ErrWebAuthnNotConfigured {
			debug.Warning("WebAuthn not configured")
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error":   "WebAuthn not configured",
				"message": "Please configure WebAuthn settings in admin panel",
			})
			return
		}
		debug.Error("Failed to get WebAuthn instance: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get user info
	user, err := h.db.GetUserByID(userIDStr)
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Build WebAuthn user
	webAuthnUser, err := h.buildWebAuthnUser(userID, user.Username, user.Email)
	if err != nil {
		debug.Error("Failed to build WebAuthn user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get existing credentials to exclude them
	excludeCredentials := make([]protocol.CredentialDescriptor, len(webAuthnUser.Credentials))
	for i, cred := range webAuthnUser.Credentials {
		excludeCredentials[i] = protocol.CredentialDescriptor{
			Type:            protocol.PublicKeyCredentialType,
			CredentialID:    cred.ID,
			Transport:       cred.Transport,
		}
	}

	// Create registration options
	options, session, err := webAuthn.BeginRegistration(
		webAuthnUser,
		webauthn.WithExclusions(excludeCredentials),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			AuthenticatorAttachment: "",  // Allow any authenticator
			ResidentKey:            protocol.ResidentKeyRequirementDiscouraged,
			UserVerification:       protocol.VerificationDiscouraged,
		}),
	)
	if err != nil {
		debug.Error("Failed to begin registration: %v", err)
		http.Error(w, "Failed to begin registration", http.StatusInternalServerError)
		return
	}

	// Serialize session data
	sessionData, err := json.Marshal(session)
	if err != nil {
		debug.Error("Failed to marshal session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store pending registration
	if err := h.db.StorePendingPasskeyRegistration(userID, options.Response.Challenge, sessionData); err != nil {
		debug.Error("Failed to store pending registration: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("Passkey registration started for user %s", userID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"options": options,
	})
}

// FinishPasskeyRegistration completes the passkey registration process
func (h *Handler) FinishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Context().Value("user_id").(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Parse request
	var req struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		req.Name = "Passkey"
	}

	// Get WebAuthn instance
	webAuthn, err := h.getWebAuthnInstance()
	if err != nil {
		debug.Error("Failed to get WebAuthn instance: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get pending registration
	_, sessionData, err := h.db.GetPendingPasskeyRegistration(userID)
	if err != nil {
		if err == db.ErrChallengeNotFound {
			debug.Warning("No pending registration found for user %s", userID)
			http.Error(w, "No pending registration found or challenge expired", http.StatusBadRequest)
			return
		}
		debug.Error("Failed to get pending registration: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Unmarshal session
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		debug.Error("Failed to unmarshal session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get user info
	user, err := h.db.GetUserByID(userIDStr)
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Build WebAuthn user
	webAuthnUser, err := h.buildWebAuthnUser(userID, user.Username, user.Email)
	if err != nil {
		debug.Error("Failed to build WebAuthn user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse the credential response
	credentialResponse, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(req.Credential)))
	if err != nil {
		debug.Error("Failed to parse credential response: %v", err)
		http.Error(w, "Invalid credential response", http.StatusBadRequest)
		return
	}

	// Verify registration
	credential, err := webAuthn.CreateCredential(webAuthnUser, session, credentialResponse)
	if err != nil {
		debug.Error("Failed to create credential: %v", err)
		http.Error(w, "Failed to verify registration", http.StatusBadRequest)
		return
	}

	// Extract transports
	transports := make([]string, len(credential.Transport))
	for i, t := range credential.Transport {
		transports[i] = string(t)
	}

	// Store the passkey with backup flags
	passkey := &models.UserPasskey{
		UserID:         userID,
		CredentialID:   credential.ID,
		PublicKey:      credential.PublicKey,
		AAGUID:         credential.Authenticator.AAGUID,
		SignCount:      credential.Authenticator.SignCount,
		Transports:     transports,
		Name:           req.Name,
		BackupEligible: credential.Flags.BackupEligible,
		BackupState:    credential.Flags.BackupState,
	}

	if err := h.db.CreatePasskey(passkey); err != nil {
		debug.Error("Failed to store passkey: %v", err)
		http.Error(w, "Failed to store passkey", http.StatusInternalServerError)
		return
	}

	// Add passkey to user's MFA types
	if err := h.db.AddPasskeyToUserMFATypes(userID); err != nil {
		debug.Error("Failed to add passkey to user MFA types: %v", err)
		// Don't fail - passkey is already stored
	}

	// Delete pending registration
	h.db.DeletePendingPasskeyRegistration(userID)

	debug.Info("Passkey registered successfully for user %s: %s", userID, req.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"passkey": passkey.ToResponse(),
	})
}

// BeginPasskeyAuthentication starts the passkey authentication process (MFA flow)
func (h *Handler) BeginPasskeyAuthentication(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get MFA session to verify it's valid and get user ID
	mfaSession, err := h.db.GetMFASession(req.SessionToken)
	if err != nil {
		debug.Warning("Invalid or expired MFA session: %v", err)
		http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(mfaSession.UserID)
	if err != nil {
		debug.Error("Invalid user ID in session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get WebAuthn instance
	webAuthn, err := h.getWebAuthnInstance()
	if err != nil {
		if err == db.ErrWebAuthnNotConfigured {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error":   "WebAuthn not configured",
				"message": "Please configure WebAuthn settings in admin panel",
			})
			return
		}
		debug.Error("Failed to get WebAuthn instance: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get user info
	user, err := h.db.GetUserByID(mfaSession.UserID)
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Build WebAuthn user
	webAuthnUser, err := h.buildWebAuthnUser(userID, user.Username, user.Email)
	if err != nil {
		debug.Error("Failed to build WebAuthn user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if user has passkeys
	if len(webAuthnUser.Credentials) == 0 {
		debug.Warning("User %s has no passkeys registered", userID)
		http.Error(w, "No passkeys registered", http.StatusBadRequest)
		return
	}

	// Create authentication options
	options, session, err := webAuthn.BeginLogin(
		webAuthnUser,
		webauthn.WithUserVerification(protocol.VerificationDiscouraged),
	)
	if err != nil {
		debug.Error("Failed to begin authentication: %v", err)
		http.Error(w, "Failed to begin authentication", http.StatusInternalServerError)
		return
	}

	// Serialize session data
	sessionData, err := json.Marshal(session)
	if err != nil {
		debug.Error("Failed to marshal session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store pending authentication
	if err := h.db.StorePendingPasskeyAuthentication(req.SessionToken, userID, options.Response.Challenge, sessionData); err != nil {
		debug.Error("Failed to store pending authentication: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("Passkey authentication started for user %s", userID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"options": options,
	})
}

// FinishPasskeyAuthentication completes the passkey authentication and logs the user in
func (h *Handler) FinishPasskeyAuthentication(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionToken string          `json:"sessionToken"`
		Credential   json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get pending authentication
	userID, _, sessionData, err := h.db.GetPendingPasskeyAuthentication(req.SessionToken)
	if err != nil {
		if err == db.ErrChallengeNotFound {
			debug.Warning("No pending authentication found")
			http.Error(w, "No pending authentication or challenge expired", http.StatusBadRequest)
			return
		}
		debug.Error("Failed to get pending authentication: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get WebAuthn instance
	webAuthn, err := h.getWebAuthnInstance()
	if err != nil {
		debug.Error("Failed to get WebAuthn instance: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Unmarshal session
	var session webauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		debug.Error("Failed to unmarshal session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get user info
	user, err := h.db.GetUserByID(userID.String())
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Build WebAuthn user
	webAuthnUser, err := h.buildWebAuthnUser(userID, user.Username, user.Email)
	if err != nil {
		debug.Error("Failed to build WebAuthn user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse the credential response
	credentialResponse, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(req.Credential)))
	if err != nil {
		debug.Error("Failed to parse credential response: %v", err)
		http.Error(w, "Invalid credential response", http.StatusBadRequest)
		return
	}

	// Verify authentication
	credential, err := webAuthn.ValidateLogin(webAuthnUser, session, credentialResponse)
	if err != nil {
		debug.Error("Failed to validate login: %v", err)

		// Increment MFA attempts
		attempts, _ := h.db.IncrementMFASessionAttempts(req.SessionToken)
		maxAttempts := 3 // Default

		if authSettings, err := h.db.GetAuthSettings(); err == nil {
			if mfaSettings, err := h.db.GetMFASettings(); err == nil {
				maxAttempts = mfaSettings.MFAMaxAttempts
			}
			_ = authSettings // unused but needed for context
		}

		remaining := maxAttempts - attempts
		if remaining <= 0 {
			h.db.DeleteMFASession(req.SessionToken)
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"success":           false,
				"message":           "Maximum verification attempts exceeded",
				"remainingAttempts": 0,
			})
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success":           false,
			"message":           "Authentication failed",
			"remainingAttempts": remaining,
		})
		return
	}

	// Find the passkey that was used
	passkey, err := h.db.GetPasskeyByCredentialID(credential.ID)
	if err != nil {
		debug.Error("Failed to find passkey: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Validate and update sign count
	if err := h.db.ValidateAndUpdateSignCount(passkey.ID, passkey.SignCount, credential.Authenticator.SignCount); err != nil {
		if err == db.ErrSignCountMismatch {
			debug.Warning("Sign count mismatch for passkey %s - possible cloned authenticator", passkey.ID)
			// Log security event but still allow login (user should be warned)
		} else {
			debug.Error("Failed to update sign count: %v", err)
		}
	}

	// Delete pending authentication
	h.db.DeletePendingPasskeyAuthentication(req.SessionToken)

	// Delete MFA session
	h.db.DeleteMFASession(req.SessionToken)

	// Get auth settings for token expiry
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Generate auth token
	token, err := h.generateAuthToken(user, authSettings.JWTExpiryMinutes)
	if err != nil {
		debug.Error("Failed to generate auth token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store token and get token ID
	tokenID, err := h.db.StoreToken(user.ID.String(), token)
	if err != nil {
		debug.Error("Failed to store token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update last login timestamp
	if err := h.db.UpdateLastLogin(user.ID); err != nil {
		debug.Error("Failed to update last login: %v", err)
		// Don't fail the login for this
	}

	// Get client info for session and login attempt logging
	ipAddress, userAgent := getClientInfo(r)

	// Create active session linked to token
	activeSession := &models.ActiveSession{
		UserID:    user.ID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		TokenID:   &tokenID,
	}
	if err := h.db.CreateSession(activeSession); err != nil {
		debug.Error("Failed to create session: %v", err)
		// Don't fail the login for this
	}

	// Log successful login attempt
	loginAttempt := &models.LoginAttempt{
		UserID:    &user.ID,
		Username:  user.Username,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Success:   true,
	}
	if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
		debug.Error("Failed to log login attempt: %v", err)
		// Don't fail the login for this
	}

	// Set auth cookie
	setAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60)

	debug.Info("Passkey authentication successful for user %s", userID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"token":   token,
	})
}

// ListPasskeys returns all passkeys for the current user
func (h *Handler) ListPasskeys(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Context().Value("user_id").(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	passkeys, err := h.db.GetUserPasskeys(userID)
	if err != nil {
		debug.Error("Failed to get passkeys: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := make([]models.UserPasskeyResponse, len(passkeys))
	for i, p := range passkeys {
		response[i] = p.ToResponse()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"passkeys": response,
	})
}

// DeletePasskey removes a passkey for the current user
func (h *Handler) DeletePasskey(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Context().Value("user_id").(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	vars := mux.Vars(r)
	passkeyIDStr := vars["id"]
	passkeyID, err := uuid.Parse(passkeyIDStr)
	if err != nil {
		debug.Error("Invalid passkey ID: %v", err)
		http.Error(w, "Invalid passkey ID", http.StatusBadRequest)
		return
	}

	if err := h.db.DeletePasskey(passkeyID, userID); err != nil {
		if err == db.ErrPasskeyNotFound {
			http.Error(w, "Passkey not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to delete passkey: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Remove passkey from user's MFA types if they have no more passkeys
	if err := h.db.RemovePasskeyFromUserMFATypes(userID); err != nil {
		debug.Error("Failed to remove passkey from user MFA types: %v", err)
		// Don't fail - passkey is already deleted
	}

	debug.Info("Passkey %s deleted for user %s", passkeyID, userID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// RenamePasskey renames a passkey for the current user
func (h *Handler) RenamePasskey(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Context().Value("user_id").(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Invalid user ID: %v", err)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	vars := mux.Vars(r)
	passkeyIDStr := vars["id"]
	passkeyID, err := uuid.Parse(passkeyIDStr)
	if err != nil {
		debug.Error("Invalid passkey ID: %v", err)
		http.Error(w, "Invalid passkey ID", http.StatusBadRequest)
		return
	}

	var req models.PasskeyRenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Name cannot be empty", http.StatusBadRequest)
		return
	}

	if err := h.db.RenamePasskey(passkeyID, userID, req.Name); err != nil {
		if err == db.ErrPasskeyNotFound {
			http.Error(w, "Passkey not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to rename passkey: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("Passkey %s renamed for user %s", passkeyID, userID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// GetWebAuthnSettings returns the current WebAuthn settings (for admin)
func (h *Handler) GetWebAuthnSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.db.GetWebAuthnSettings()
	if err != nil {
		debug.Error("Failed to get WebAuthn settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rpId":          settings.RPID,
		"rpOrigins":     settings.RPOrigins,
		"rpDisplayName": settings.RPDisplayName,
		"configured":    settings.IsConfigured(),
	})
}

// UpdateWebAuthnSettings updates the WebAuthn settings (admin only)
func (h *Handler) UpdateWebAuthnSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RPID          string   `json:"rpId"`
		RPOrigins     []string `json:"rpOrigins"`
		RPDisplayName string   `json:"rpDisplayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	settings := &models.WebAuthnSettings{
		RPID:          req.RPID,
		RPOrigins:     req.RPOrigins,
		RPDisplayName: req.RPDisplayName,
	}

	if err := h.db.UpdateWebAuthnSettings(settings); err != nil {
		debug.Error("Failed to update WebAuthn settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("WebAuthn settings updated - RPID: %s, Origins: %v", req.RPID, req.RPOrigins)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// writeJSON is a helper function to write JSON responses
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
