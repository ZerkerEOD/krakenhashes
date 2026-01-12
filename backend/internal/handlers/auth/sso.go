package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/crypto/bcrypt"

	sharedAuth "github.com/ZerkerEOD/krakenhashes/backend/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SSOHandler handles SSO authentication requests
type SSOHandler struct {
	db         *db.DB
	ssoManager *sso.Manager
	ssoRepo    *repository.SSORepository
}

// NewSSOHandler creates a new SSO handler
func NewSSOHandler(database *db.DB, ssoManager *sso.Manager, ssoRepo *repository.SSORepository) *SSOHandler {
	return &SSOHandler{
		db:         database,
		ssoManager: ssoManager,
		ssoRepo:    ssoRepo,
	}
}

// GetEnabledProviders returns the list of enabled SSO providers for the login page
func (h *SSOHandler) GetEnabledProviders(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting enabled SSO providers")

	providers, err := h.ssoManager.GetEnabledProviders(r.Context())
	if err != nil {
		debug.Error("Failed to get enabled providers: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Also get SSO settings to inform frontend about local auth status
	settings, err := h.ssoManager.GetSSOSettings(r.Context())
	if err != nil {
		debug.Error("Failed to get SSO settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"providers":          providers,
		"local_auth_enabled": settings.LocalAuthEnabled,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// LDAPLoginRequest represents the LDAP login request body
type LDAPLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LDAPLogin handles LDAP authentication
func (h *SSOHandler) LDAPLogin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	var req LDAPLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Failed to decode LDAP login request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Authenticate with LDAP
	authReq := &sso.AuthRequest{
		Username:  req.Username,
		Password:  req.Password,
		IPAddress: sharedAuth.GetClientIP(r),
		UserAgent: r.UserAgent(),
	}

	result, err := h.ssoManager.Authenticate(r.Context(), providerID, authReq)
	if err != nil {
		debug.Error("LDAP authentication failed: %v", err)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeLDAP, false, err.Error())
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	if !result.Success {
		debug.Info("LDAP authentication unsuccessful: %s", result.ErrorMessage)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeLDAP, false, result.ErrorMessage)
		http.Error(w, result.ErrorMessage, http.StatusUnauthorized)
		return
	}

	// Process authentication result
	h.handleSSOResult(w, r, result, providerID)
}

// OAuthStart initiates the OAuth flow
func (h *SSOHandler) OAuthStart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	// Get redirect URI from query params or use default
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		// Build default callback URL
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		host := r.Host
		redirectURI = scheme + "://" + host + "/api/auth/oauth/" + providerIDStr + "/callback"
	}

	// Get authorization URL
	authURL, err := h.ssoManager.GetStartURL(r.Context(), providerID, redirectURI)
	if err != nil {
		debug.Error("Failed to generate OAuth URL: %v", err)
		http.Error(w, "Failed to initiate authentication", http.StatusInternalServerError)
		return
	}

	debug.Info("Redirecting to OAuth provider: %s", authURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OAuthCallback handles the OAuth callback
func (h *SSOHandler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		h.ssoErrorRedirect(w, r, "Invalid provider")
		return
	}

	// Get callback parameters
	callbackReq := &sso.CallbackRequest{
		State:     r.URL.Query().Get("state"),
		Code:      r.URL.Query().Get("code"),
		Error:     r.URL.Query().Get("error"),
		IPAddress: sharedAuth.GetClientIP(r),
		UserAgent: r.UserAgent(),
	}

	if callbackReq.Error != "" {
		debug.Warning("OAuth callback error: %s", callbackReq.Error)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeOIDC, false, callbackReq.Error)
		h.ssoErrorRedirect(w, r, "Authentication failed: "+callbackReq.Error)
		return
	}

	// Process callback
	result, err := h.ssoManager.HandleCallback(r.Context(), providerID, callbackReq)
	if err != nil {
		debug.Error("OAuth callback processing failed: %v", err)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeOIDC, false, err.Error())
		h.ssoErrorRedirect(w, r, "Authentication processing failed")
		return
	}

	if !result.Success {
		debug.Info("OAuth authentication unsuccessful: %s", result.ErrorMessage)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeOIDC, false, result.ErrorMessage)
		h.ssoErrorRedirect(w, r, result.ErrorMessage)
		return
	}

	// Process authentication result (with redirect to frontend)
	h.handleSSOResultWithRedirect(w, r, result, providerID)
}

// SAMLStart initiates the SAML flow
func (h *SSOHandler) SAMLStart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	// Get redirect URI from query params or use default
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		// Build default ACS URL
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		host := r.Host
		redirectURI = scheme + "://" + host + "/api/auth/saml/" + providerIDStr + "/acs"
	}

	// Get authorization URL
	authURL, err := h.ssoManager.GetStartURL(r.Context(), providerID, redirectURI)
	if err != nil {
		debug.Error("Failed to generate SAML URL: %v", err)
		http.Error(w, "Failed to initiate authentication", http.StatusInternalServerError)
		return
	}

	debug.Info("Redirecting to SAML provider: %s", authURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// SAMLACS handles the SAML Assertion Consumer Service
func (h *SSOHandler) SAMLACS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		h.ssoErrorRedirect(w, r, "Invalid provider")
		return
	}

	// Parse form data (SAML responses are typically POST form data)
	if err := r.ParseForm(); err != nil {
		debug.Warning("Failed to parse SAML form: %v", err)
		h.ssoErrorRedirect(w, r, "Invalid request")
		return
	}

	callbackReq := &sso.CallbackRequest{
		SAMLResponse: r.FormValue("SAMLResponse"),
		RelayState:   r.FormValue("RelayState"),
		IPAddress:    sharedAuth.GetClientIP(r),
		UserAgent:    r.UserAgent(),
	}

	if callbackReq.SAMLResponse == "" {
		debug.Warning("Missing SAML response")
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeSAML, false, "missing_saml_response")
		h.ssoErrorRedirect(w, r, "Missing SAML response")
		return
	}

	// Process SAML response
	result, err := h.ssoManager.HandleCallback(r.Context(), providerID, callbackReq)
	if err != nil {
		debug.Error("SAML processing failed: %v", err)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeSAML, false, err.Error())
		h.ssoErrorRedirect(w, r, "Authentication processing failed")
		return
	}

	if !result.Success {
		debug.Info("SAML authentication unsuccessful: %s", result.ErrorMessage)
		h.logSSOLoginAttempt(r, nil, providerID, models.ProviderTypeSAML, false, result.ErrorMessage)
		h.ssoErrorRedirect(w, r, result.ErrorMessage)
		return
	}

	// Process authentication result
	h.handleSSOResultWithRedirect(w, r, result, providerID)
}

// SAMLMetadata returns the SAML SP metadata
func (h *SSOHandler) SAMLMetadata(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		debug.Warning("Invalid provider ID: %s", providerIDStr)
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	// Get the SAML provider
	provider, err := h.ssoManager.GetProvider(providerID)
	if err != nil {
		debug.Error("Provider not found: %v", err)
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	// Check if it's a SAML provider
	if provider.Type() != models.ProviderTypeSAML {
		http.Error(w, "Provider is not a SAML provider", http.StatusBadRequest)
		return
	}

	// Get SAML provider and generate metadata
	samlProvider, ok := provider.(interface {
		GenerateMetadata(acsURL string) ([]byte, error)
	})
	if !ok {
		http.Error(w, "Provider does not support metadata generation", http.StatusInternalServerError)
		return
	}

	// Build ACS URL
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	acsURL := scheme + "://" + r.Host + "/api/auth/saml/" + providerIDStr + "/acs"

	metadata, err := samlProvider.GenerateMetadata(acsURL)
	if err != nil {
		debug.Error("Failed to generate metadata: %v", err)
		http.Error(w, "Failed to generate metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Write(metadata)
}

// handleSSOResult processes the SSO result and creates a session (for API responses)
func (h *SSOHandler) handleSSOResult(w http.ResponseWriter, r *http.Request, result *models.AuthResult, providerID uuid.UUID) {
	ctx := r.Context()

	// Find or create user from identity
	user, isNewUser, err := h.findOrCreateUser(ctx, result.Identity, providerID)
	if err != nil {
		debug.Error("Failed to find/create user: %v", err)
		http.Error(w, "Account processing failed", http.StatusInternalServerError)
		return
	}

	// Check if account is enabled
	if !user.AccountEnabled {
		debug.Warning("SSO login attempt for disabled account: %s", user.Email)
		h.logSSOLoginAttempt(r, &user.ID, providerID, result.Identity.ProviderType, false, "account_disabled")

		if isNewUser {
			// Inform user that account needs approval
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Your account has been created but requires administrator approval before you can log in.",
				"pending": true,
			})
			return
		}

		http.Error(w, "Account is disabled", http.StatusForbidden)
		return
	}

	// Check if MFA is required (user setting or global requirement)
	// This follows the same pattern as local auth in handlers.go
	mfaSettings, err := h.db.GetUserMFASettings(user.ID.String())
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	globalMFARequired, err := h.db.IsMFARequired()
	if err != nil {
		debug.Error("Failed to check global MFA requirement: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Only require MFA if user has it enabled OR it's globally required
	if mfaSettings.MFAEnabled || globalMFARequired {
		// Create MFA session and return MFA required response
		sessionToken := uuid.New().String()
		_, err := h.db.CreateMFASession(user.ID.String(), sessionToken)
		if err != nil {
			debug.Error("Failed to create MFA session: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		h.logSSOLoginAttempt(r, &user.ID, providerID, result.Identity.ProviderType, true, "mfa_required")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":          true,
			"mfa_required":     true,
			"session_token":    sessionToken,
			"mfa_type":         mfaSettings.MFAType,
			"preferred_method": mfaSettings.PreferredMFAMethod,
		})
		return
	}

	// Generate JWT and complete login
	h.completeLogin(w, r, user, providerID, result.Identity.ProviderType)
}

// handleSSOResultWithRedirect processes the SSO result and redirects to frontend
func (h *SSOHandler) handleSSOResultWithRedirect(w http.ResponseWriter, r *http.Request, result *models.AuthResult, providerID uuid.UUID) {
	ctx := r.Context()

	// Find or create user from identity
	user, isNewUser, err := h.findOrCreateUser(ctx, result.Identity, providerID)
	if err != nil {
		debug.Error("Failed to find/create user: %v", err)
		h.ssoErrorRedirect(w, r, "Account processing failed")
		return
	}

	// Check if account is enabled
	if !user.AccountEnabled {
		debug.Warning("SSO login attempt for disabled account: %s", user.Email)
		h.logSSOLoginAttempt(r, &user.ID, providerID, result.Identity.ProviderType, false, "account_disabled")

		if isNewUser {
			h.ssoRedirect(w, r, "/login?sso_error=pending_approval")
			return
		}

		h.ssoRedirect(w, r, "/login?sso_error=account_disabled")
		return
	}

	// For OAuth/SAML, we trust provider's MFA (RequiresMFA should be false)
	// Generate JWT and complete login
	h.completeLoginWithRedirect(w, r, user, providerID, result.Identity.ProviderType)
}

// findOrCreateUser finds an existing user or creates a new one from SSO identity
func (h *SSOHandler) findOrCreateUser(ctx context.Context, identity *models.ExternalIdentity, providerID uuid.UUID) (*models.User, bool, error) {
	// First, try to find by linked identity
	existingIdentity, err := h.ssoRepo.GetUserIdentity(ctx, providerID, identity.ExternalID)
	if err == nil && existingIdentity != nil {
		// User already linked, get the user
		user, err := h.db.GetUserByID(existingIdentity.UserID.String())
		if err != nil {
			return nil, false, err
		}

		// Update identity with latest info
		existingIdentity.ExternalEmail = identity.Email
		existingIdentity.ExternalUsername = identity.Username
		existingIdentity.ExternalDisplayName = identity.DisplayName
		existingIdentity.Metadata = identity.Metadata
		now := time.Now()
		existingIdentity.LastLoginAt = &now
		h.ssoRepo.UpdateUserIdentity(ctx, existingIdentity)

		return user, false, nil
	}

	// Try to find by email match
	if identity.Email != "" {
		user, err := h.getUserByEmail(identity.Email)
		if err == nil && user != nil {
			// Found existing user, link identity
			ui := &models.UserIdentity{
				ID:                  uuid.New(),
				UserID:              user.ID,
				ProviderID:          providerID,
				ExternalID:          identity.ExternalID,
				ExternalEmail:       identity.Email,
				ExternalUsername:    identity.Username,
				ExternalDisplayName: identity.DisplayName,
				Metadata:            identity.Metadata,
			}
			now := time.Now()
			ui.LastLoginAt = &now
			if err := h.ssoRepo.CreateUserIdentity(ctx, ui); err != nil {
				debug.Warning("Failed to link identity: %v", err)
			}

			return user, false, nil
		}
	}

	// Get SSO settings to check if auto-create is enabled
	settings, err := h.ssoRepo.GetSSOSettings(ctx)
	if err != nil {
		return nil, false, err
	}

	// Get provider settings for auto-create override
	provider, err := h.ssoRepo.GetProvider(ctx, providerID)
	if err != nil {
		return nil, false, err
	}

	// Check auto-create setting (provider override > global)
	autoCreate := settings.SSOAutoCreateUsers
	if provider.AutoCreateUsers != nil {
		autoCreate = *provider.AutoCreateUsers
	}

	if !autoCreate {
		return nil, false, sso.ErrUserNotFound
	}

	// Create new user
	autoEnable := settings.SSOAutoEnableUsers
	if provider.AutoEnableUsers != nil {
		autoEnable = *provider.AutoEnableUsers
	}

	username := identity.Username
	if username == "" {
		username = identity.Email
	}

	newUser := &models.User{
		ID:             uuid.New(),
		Username:       username,
		Email:          identity.Email,
		Role:           "user",
		AccountEnabled: autoEnable,
	}

	if err := h.createSSOUser(newUser); err != nil {
		return nil, false, err
	}

	// Link the identity
	ui := &models.UserIdentity{
		ID:                  uuid.New(),
		UserID:              newUser.ID,
		ProviderID:          providerID,
		ExternalID:          identity.ExternalID,
		ExternalEmail:       identity.Email,
		ExternalUsername:    identity.Username,
		ExternalDisplayName: identity.DisplayName,
		Metadata:            identity.Metadata,
	}
	now := time.Now()
	ui.LastLoginAt = &now
	if err := h.ssoRepo.CreateUserIdentity(ctx, ui); err != nil {
		debug.Warning("Failed to link identity for new user: %v", err)
	}

	debug.Info("Created new SSO user: %s (enabled: %v)", newUser.Email, autoEnable)
	return newUser, true, nil
}

// getUserByEmail retrieves a user by email
func (h *SSOHandler) getUserByEmail(email string) (*models.User, error) {
	user := &models.User{}
	err := h.db.QueryRow(`
		SELECT id, username, email, role, account_enabled, account_locked, created_at, updated_at
		FROM users
		WHERE email = $1`,
		email,
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Role,
		&user.AccountEnabled,
		&user.AccountLocked,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return user, nil
}

// createSSOUser creates a new user with an unguessable password (for SSO)
// The password is randomly generated and unknown, so local login is impossible
// unless an admin explicitly resets it later.
func (h *SSOHandler) createSSOUser(user *models.User) error {
	// Generate a random unguessable password (48 bytes -> 64 chars base64)
	// Note: bcrypt has a 72-byte max, so 64 chars is safely under the limit
	randomBytes := make([]byte, 48)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("failed to generate random password: %w", err)
	}
	randomPassword := base64.StdEncoding.EncodeToString(randomBytes)

	// Hash with bcrypt - this password is unknown and unguessable
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = h.db.Exec(`
		INSERT INTO users (id, username, email, password_hash, role, account_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		user.ID,
		user.Username,
		user.Email,
		string(passwordHash),
		user.Role,
		user.AccountEnabled,
	)
	return err
}

// completeLogin generates JWT and sets cookies (for API response)
func (h *SSOHandler) completeLogin(w http.ResponseWriter, r *http.Request, user *models.User, providerID uuid.UUID, providerType models.ProviderType) {
	// Get auth settings for JWT expiry
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	token, err := jwt.GenerateToken(user.ID.String(), user.Role, authSettings.JWTExpiryMinutes)
	if err != nil {
		debug.Error("Failed to generate token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store token
	tokenID, err := h.db.StoreToken(user.ID.String(), token)
	if err != nil {
		debug.Error("Failed to store token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Log successful login
	h.logSSOLoginAttempt(r, &user.ID, providerID, providerType, true, "")

	// Update last login
	h.db.UpdateLastLogin(user.ID)

	// Create active session
	ipAddress, userAgent := sharedAuth.GetClientInfo(r)
	session := &models.ActiveSession{
		UserID:    user.ID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		TokenID:   &tokenID,
	}
	if err := h.db.CreateSession(session); err != nil {
		debug.Error("Failed to create session: %v", err)
	}

	// Set auth cookie
	sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60)

	debug.Info("SSO login successful for user: %s via provider %s", user.Email, providerType)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.LoginResponse{
		Success: true,
		Token:   token,
	})
}

// completeLoginWithRedirect generates JWT and redirects to frontend
func (h *SSOHandler) completeLoginWithRedirect(w http.ResponseWriter, r *http.Request, user *models.User, providerID uuid.UUID, providerType models.ProviderType) {
	// Get auth settings for JWT expiry
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		h.ssoErrorRedirect(w, r, "Internal server error")
		return
	}

	token, err := jwt.GenerateToken(user.ID.String(), user.Role, authSettings.JWTExpiryMinutes)
	if err != nil {
		debug.Error("Failed to generate token: %v", err)
		h.ssoErrorRedirect(w, r, "Internal server error")
		return
	}

	// Store token
	tokenID, err := h.db.StoreToken(user.ID.String(), token)
	if err != nil {
		debug.Error("Failed to store token: %v", err)
		h.ssoErrorRedirect(w, r, "Internal server error")
		return
	}

	// Log successful login
	h.logSSOLoginAttempt(r, &user.ID, providerID, providerType, true, "")

	// Update last login
	h.db.UpdateLastLogin(user.ID)

	// Create active session
	ipAddress, userAgent := sharedAuth.GetClientInfo(r)
	session := &models.ActiveSession{
		UserID:    user.ID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		TokenID:   &tokenID,
	}
	if err := h.db.CreateSession(session); err != nil {
		debug.Error("Failed to create session: %v", err)
	}

	// Set auth cookie
	sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60)

	debug.Info("SSO login successful for user: %s via provider %s", user.Email, providerType)

	// Redirect to frontend
	h.ssoRedirect(w, r, "/")
}

// logSSOLoginAttempt logs an SSO login attempt
func (h *SSOHandler) logSSOLoginAttempt(r *http.Request, userID *uuid.UUID, providerID uuid.UUID, providerType models.ProviderType, success bool, failureReason string) {
	ipAddress, userAgent := sharedAuth.GetClientInfo(r)

	// Use the existing CreateLoginAttempt but with extra fields
	// For now, create directly since the SSO repository has the full query
	if err := h.ssoRepo.CreateSSOLoginAttempt(r.Context(), &models.LoginAttempt{
		UserID:        userID,
		IPAddress:     ipAddress,
		UserAgent:     userAgent,
		Success:       success,
		FailureReason: failureReason,
		ProviderID:    &providerID,
		ProviderType:  string(providerType),
	}); err != nil {
		debug.Error("Failed to log SSO login attempt: %v", err)
	}
}

// ssoRedirect redirects to frontend with optional message
func (h *SSOHandler) ssoRedirect(w http.ResponseWriter, r *http.Request, path string) {
	// Get the frontend URL from the origin or use default
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Fall back to the request host
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		origin = scheme + "://" + r.Host
	}

	redirectURL := origin + path
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// ssoErrorRedirect redirects to frontend with error message
func (h *SSOHandler) ssoErrorRedirect(w http.ResponseWriter, r *http.Request, errorMsg string) {
	h.ssoRedirect(w, r, "/login?sso_error="+url.QueryEscape(errorMsg))
}
