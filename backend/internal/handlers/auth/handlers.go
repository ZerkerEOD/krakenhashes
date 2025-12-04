package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// LoginRequest represents the expected JSON structure for login attempts
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Helper function to extract client IP address and User-Agent from request
func getClientInfo(r *http.Request) (ipAddress string, userAgent string) {
	// Try to get real IP from X-Forwarded-For header (for proxied requests)
	ipAddress = r.Header.Get("X-Forwarded-For")
	if ipAddress != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if idx := strings.Index(ipAddress, ","); idx != -1 {
			ipAddress = strings.TrimSpace(ipAddress[:idx])
		}
	}

	// Fallback to X-Real-IP header
	if ipAddress == "" {
		ipAddress = r.Header.Get("X-Real-IP")
	}

	// Fallback to RemoteAddr
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
		// Remove port if present
		if idx := strings.LastIndex(ipAddress, ":"); idx != -1 {
			ipAddress = ipAddress[:idx]
		}
	}

	// Get User-Agent
	userAgent = r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "Unknown"
	}

	return ipAddress, userAgent
}

// Helper function to get cookie domain from request host
func getCookieDomain(host string) string {
	debug.Debug("Getting cookie domain from host: %s", host)

	// Always strip port number since frontend and backend are on different ports
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	// For development environments (localhost/127.0.0.1), don't set domain
	if host == "localhost" || host == "127.0.0.1" {
		debug.Debug("Development environment detected, not setting cookie domain")
		return ""
	}

	debug.Debug("Using cookie domain: %s", host)
	return host
}

// Helper function to set auth cookie
func setAuthCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	debug.Debug("[COOKIE] Setting auth cookie - MaxAge: %d", maxAge)

	// Check if this is a development environment
	isDevelopment := strings.Contains(r.Host, "localhost") || strings.Contains(r.Host, "127.0.0.1")

	// For cross-port development (frontend:3000, backend:31337) we need special handling
	var sameSite http.SameSite
	var secure bool

	if isDevelopment {
		// For localhost development with HTTPS, use Lax for better compatibility
		sameSite = http.SameSiteLaxMode
		secure = true // We're using HTTPS even in development
		debug.Info("[COOKIE] Development environment: using SameSite=Lax, Secure=true for HTTPS localhost")
	} else {
		// Production settings
		sameSite = http.SameSiteLaxMode
		secure = true
		debug.Debug("[COOKIE] Production environment: using SameSite=Lax, Secure=true")
	}

	cookie := &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Path:     "/",
		MaxAge:   maxAge,
	}

	// For development, don't set domain to allow cross-port cookie sharing
	domain := getCookieDomain(r.Host)
	if domain != "" {
		cookie.Domain = domain
		debug.Debug("[COOKIE] Setting cookie domain: %s", domain)
	} else {
		debug.Info("[COOKIE] No domain set for cookie (allows cross-port sharing in development)")
	}

	// Log the complete cookie configuration for debugging
	debug.Info("[COOKIE] Cookie configuration: name=%s, secure=%v, sameSite=%v, httpOnly=%v, path=%s, domain=%s, maxAge=%d",
		cookie.Name, cookie.Secure, cookie.SameSite, cookie.HttpOnly, cookie.Path, cookie.Domain, cookie.MaxAge)

	http.SetCookie(w, cookie)
	debug.Info("[COOKIE] Auth cookie set successfully")
}

// generateAuthToken creates a new JWT token for the user
func (h *Handler) generateAuthToken(user *models.User, expiryMinutes int) (string, error) {
	return jwt.GenerateToken(user.ID.String(), user.Role, expiryMinutes)
}

/*
 * LoginHandler processes user login requests.
 * It validates credentials, generates a JWT token, and sets a secure cookie.
 *
 * Request body expects JSON:
 * {
 *   "username": "string",
 *   "password": "string"
 * }
 *
 * Responses:
 *   - 200: Successfully logged in, sets auth cookie
 *   - 400: Invalid request format
 *   - 401: Invalid credentials
 *   - 500: Server error (token generation/storage)
 */
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Processing login request")

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Failed to decode login request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	debug.Debug("Login request decoded for user: %s", req.Username)

	// Prevent login with system user
	if req.Username == "system" {
		debug.Warning("Attempted login with system user account")
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}
	
	// Get auth settings once at the beginning
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	user, err := h.db.GetUserByUsername(req.Username)
	if err != nil {
		debug.Info("Failed login attempt for user '%s': %v", req.Username, err)

		// Log failed login attempt (user not found)
		ipAddress, userAgent := getClientInfo(r)
		loginAttempt := &models.LoginAttempt{
			Username:      req.Username,
			IPAddress:     ipAddress,
			UserAgent:     userAgent,
			Success:       false,
			FailureReason: "user_not_found",
		}
		if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
			debug.Error("Failed to log login attempt: %v", err)
		}

		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Prevent login with system user by ID
	if user.ID.String() == "00000000-0000-0000-0000-000000000000" || user.Role == "system" {
		debug.Warning("Attempted login with system user account")

		// Log failed login attempt (system user)
		ipAddress, userAgent := getClientInfo(r)
		loginAttempt := &models.LoginAttempt{
			UserID:        &user.ID,
			Username:      req.Username,
			IPAddress:     ipAddress,
			UserAgent:     userAgent,
			Success:       false,
			FailureReason: "system_user_login_blocked",
		}
		if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
			debug.Error("Failed to log login attempt: %v", err)
		}

		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Check if account is disabled
	if !user.AccountEnabled {
		debug.Warning("Login attempt for disabled account: %s", req.Username)

		// Log failed login attempt (account disabled)
		ipAddress, userAgent := getClientInfo(r)
		loginAttempt := &models.LoginAttempt{
			UserID:        &user.ID,
			Username:      req.Username,
			IPAddress:     ipAddress,
			UserAgent:     userAgent,
			Success:       false,
			FailureReason: "account_disabled",
		}
		if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
			debug.Error("Failed to log login attempt: %v", err)
		}

		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Check if account is locked
	if user.AccountLocked {
		// Check if lock has expired
		if user.AccountLockedUntil != nil && time.Now().After(*user.AccountLockedUntil) {
			// Lock has expired, unlock the account
			debug.Info("Account lock expired for user: %s, unlocking", req.Username)
			err = h.db.ResetFailedAttempts(user.ID)
			if err != nil {
				debug.Error("Failed to unlock account: %v", err)
			}
			user.AccountLocked = false
			user.AccountLockedUntil = nil
		} else {
			debug.Warning("Login attempt for locked account: %s", req.Username)

			// Log failed login attempt (account locked)
			ipAddress, userAgent := getClientInfo(r)
			loginAttempt := &models.LoginAttempt{
				UserID:        &user.ID,
				Username:      req.Username,
				IPAddress:     ipAddress,
				UserAgent:     userAgent,
				Success:       false,
				FailureReason: "account_locked",
			}
			if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
				debug.Error("Failed to log login attempt: %v", err)
			}

			http.Error(w, "Account temporarily locked due to multiple failed login attempts", http.StatusUnauthorized)
			return
		}
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		debug.Info("Invalid password for user '%s'", req.Username)

		// Increment failed login attempts
		attempts, err := h.db.IncrementFailedAttempts(user.ID)
		if err != nil {
			debug.Error("Failed to increment login attempts: %v", err)
		} else if attempts >= authSettings.MaxFailedAttempts {
			// Lock the account
			err = h.db.LockUserAccount(user.ID, authSettings.LockoutDurationMinutes)
			if err != nil {
				debug.Error("Failed to lock account: %v", err)
			} else {
				debug.Warning("Account locked after %d failed attempts: %s", attempts, req.Username)
			}
		}

		// Log failed login attempt (invalid password)
		ipAddress, userAgent := getClientInfo(r)
		loginAttempt := &models.LoginAttempt{
			UserID:        &user.ID,
			Username:      req.Username,
			IPAddress:     ipAddress,
			UserAgent:     userAgent,
			Success:       false,
			FailureReason: "invalid_password",
		}
		if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
			debug.Error("Failed to log login attempt: %v", err)
		}

		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}
	
	// Reset failed attempts on successful password check
	if user.FailedLoginAttempts > 0 {
		err = h.db.ResetFailedAttempts(user.ID)
		if err != nil {
			debug.Error("Failed to reset login attempts: %v", err)
		}
	}

	// Check user's MFA settings and global MFA requirement
	mfaSettings, err := h.db.GetUserMFASettings(user.ID.String())
	if err != nil {
		debug.Error("error checking user MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if MFA is globally required
	globalMFARequired, err := h.db.IsMFARequired()
	if err != nil {
		debug.Error("error checking global MFA requirement: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if email provider is configured
	hasEmailProvider, err := h.db.HasActiveEmailProvider()
	if err != nil {
		debug.Error("error checking email provider: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// If either user has MFA enabled or it's globally required
	if mfaSettings.MFAEnabled || globalMFARequired {
		// Create MFA session
		sessionToken := uuid.New().String()
		session, err := h.db.CreateMFASession(user.ID.String(), sessionToken)
		if err != nil {
			debug.Error("error creating MFA session: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// If email is the preferred method and it's available, send the code
		if mfaSettings.PreferredMFAMethod == "email" && contains(mfaSettings.MFAType, "email") && hasEmailProvider {
			code, err := generateEmailCode()
			if err != nil {
				debug.Error("error generating email code: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			err = h.db.StoreEmailMFACode(user.ID.String(), code)
			if err != nil {
				debug.Error("error storing email code: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Send email synchronously
			err = h.emailService.SendMFACode(r.Context(), user.Email, code)
			if err != nil {
				debug.Error("error sending MFA email: %v", err)
				http.Error(w, "Failed to send verification code", http.StatusInternalServerError)
				return
			}
		}

		// Filter MFA types based on email provider availability
		filteredMFATypes := make([]string, 0, len(mfaSettings.MFAType))
		for _, method := range mfaSettings.MFAType {
			// Only include email if email provider is configured
			if method == "email" && !hasEmailProvider {
				continue
			}
			filteredMFATypes = append(filteredMFATypes, method)
		}

		// Adjust preferred method if email is not available
		preferredMethod := mfaSettings.PreferredMFAMethod
		if preferredMethod == "email" && !hasEmailProvider {
			// Fall back to authenticator or passkey if available
			if contains(filteredMFATypes, "authenticator") {
				preferredMethod = "authenticator"
			} else if contains(filteredMFATypes, "passkey") {
				preferredMethod = "passkey"
			} else if len(filteredMFATypes) > 0 {
				preferredMethod = filteredMFATypes[0]
			}
		}

		// Return MFA required response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"mfa_required":     true,
			"session_token":    sessionToken,
			"mfa_type":         filteredMFATypes,
			"preferred_method": preferredMethod,
			"expires_at":       session.ExpiresAt.Format(time.RFC3339),
		})
		return
	}

	// If no MFA required, proceed with normal login
	token, err := h.generateAuthToken(user, authSettings.JWTExpiryMinutes)
	if err != nil {
		debug.Error("Failed to generate token: %v", err)
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
	session := &models.ActiveSession{
		UserID:    user.ID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		TokenID:   &tokenID,
	}
	if err := h.db.CreateSession(session); err != nil {
		debug.Error("Failed to create session: %v", err)
		// Don't fail the login for this
	}

	// Log successful login attempt
	loginAttempt := &models.LoginAttempt{
		UserID:    &user.ID,
		Username:  req.Username,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Success:   true,
	}
	if err := h.db.CreateLoginAttempt(loginAttempt); err != nil {
		debug.Error("Failed to log login attempt: %v", err)
		// Don't fail the login for this
	}

	setAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds
	debug.Info("User '%s' successfully logged in", req.Username)

	json.NewEncoder(w).Encode(models.LoginResponse{
		Success: true,
		Token:   token,
	})
}

/*
 * LogoutHandler processes user logout requests.
 * It removes the token from the database and invalidates the auth cookie.
 *
 * Responses:
 *   - 200: Successfully logged out
 *   - 500: Error removing token from database
 */
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Processing logout request")

	cookie, err := r.Cookie("token")
	if err == nil {
		debug.Debug("Found token cookie, removing from database")
		if err := h.db.RemoveTokenByString(cookie.Value); err != nil {
			debug.Error("Failed to remove token from database: %v", err)
			http.Error(w, "Error removing token", http.StatusInternalServerError)
			return
		}
		debug.Debug("Token removed from database successfully")
	} else {
		debug.Debug("No token cookie found during logout: %v", err)
	}

	setAuthCookie(w, r, "", -1) // Expire the cookie
	debug.Info("User successfully logged out")

	w.WriteHeader(http.StatusOK)
}

/*
 * RefreshTokenHandler generates a new JWT token for the authenticated user.
 * This extends the session without requiring re-login.
 *
 * This handler now:
 * - Deletes the old token (CASCADE deletes linked session)
 * - Checks concurrent session limits and revokes oldest if needed
 * - Checks absolute session timeout
 * - Creates new token and session while preserving session_started_at
 *
 * Responses:
 *   - 200: New token generated and cookie set
 *   - 401: Authentication required or session expired
 *   - 500: Internal server error
 */
func (h *Handler) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Refreshing authentication token")

	// Get user ID from middleware context
	userID := r.Context().Value("user_id")
	if userID == nil {
		debug.Warning("RefreshToken called without user context")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get user role from middleware context
	userRole := r.Context().Value("user_role")
	if userRole == nil {
		debug.Warning("RefreshToken called without user role context")
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get current token from cookie
	cookie, err := r.Cookie("token")
	if err != nil {
		debug.Warning("No token cookie found during refresh: %v", err)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	oldToken := cookie.Value

	// Get JWT expiry from auth settings
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get old session to preserve session_started_at
	oldSession, err := h.db.GetSessionByToken(oldToken)
	if err != nil {
		debug.Warning("Could not find session for token during refresh: %v", err)
		// Continue anyway, will create new session with current time
		oldSession = nil
	}

	// Check absolute session timeout if configured
	if authSettings.SessionAbsoluteTimeoutHours > 0 && oldSession != nil {
		sessionAge := time.Since(oldSession.SessionStartedAt).Hours()
		if sessionAge >= float64(authSettings.SessionAbsoluteTimeoutHours) {
			debug.Info("Session exceeded absolute timeout (%d hours) for user: %s",
				authSettings.SessionAbsoluteTimeoutHours, userID)
			// Delete old token (will CASCADE delete session)
			h.db.RemoveTokenByString(oldToken)
			http.Error(w, "Session expired - please log in again", http.StatusUnauthorized)
			return
		}
		debug.Debug("Session age: %.2f hours, limit: %d hours", sessionAge, authSettings.SessionAbsoluteTimeoutHours)
	}

	// Parse userID to UUID for session queries
	userUUID, err := uuid.Parse(userID.(string))
	if err != nil {
		debug.Error("Failed to parse user ID: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check concurrent session limit if configured
	if authSettings.MaxConcurrentSessions > 0 {
		userSessions, err := h.db.GetUserSessions(userUUID)
		if err != nil {
			debug.Error("Failed to get user sessions: %v", err)
			// Continue anyway, don't fail the refresh
		} else if len(userSessions) >= authSettings.MaxConcurrentSessions {
			// Find oldest session (by created_at)
			var oldestSession *models.ActiveSession
			for _, s := range userSessions {
				if oldestSession == nil || s.CreatedAt.Before(oldestSession.CreatedAt) {
					oldestSession = s
				}
			}

			// Revoke oldest session's token
			if oldestSession != nil && oldestSession.TokenID != nil {
				debug.Info("Revoking oldest session (token: %s) due to max concurrent sessions limit for user: %s",
					oldestSession.TokenID, userID)
				if err := h.db.RevokeToken(*oldestSession.TokenID, "max_sessions_exceeded"); err != nil {
					debug.Error("Failed to revoke oldest token: %v", err)
				}
			}
		}
	}

	// Delete old token (CASCADE deletes linked session)
	debug.Debug("Removing old token during refresh")
	if err := h.db.RemoveTokenByString(oldToken); err != nil {
		debug.Error("Failed to remove old token: %v", err)
		// Continue anyway, we'll create the new token
	}

	// Generate new token
	token, err := jwt.GenerateToken(userID.(string), userRole.(string), authSettings.JWTExpiryMinutes)
	if err != nil {
		debug.Error("Failed to generate refresh token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store new token in database
	tokenID, err := h.db.StoreToken(userID.(string), token)
	if err != nil {
		debug.Error("Failed to store refresh token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get client info for new session
	ipAddress, userAgent := getClientInfo(r)

	// Create new session, preserving session_started_at from old session
	sessionStartedAt := time.Now()
	if oldSession != nil {
		sessionStartedAt = oldSession.SessionStartedAt
		debug.Debug("Preserving session_started_at: %s", sessionStartedAt)
	}

	session := &models.ActiveSession{
		UserID:           userUUID,
		IPAddress:        ipAddress,
		UserAgent:        userAgent,
		TokenID:          &tokenID,
		SessionStartedAt: sessionStartedAt,
	}
	if err := h.db.CreateSession(session); err != nil {
		debug.Error("Failed to create session during refresh: %v", err)
		// Don't fail the refresh for this
	}

	// Set new auth cookie
	setAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds
	debug.Info("Token refreshed successfully for user: %s", userID)

	json.NewEncoder(w).Encode(models.LoginResponse{
		Success: true,
		Token:   token,
	})
}

/*
 * CheckAuthHandler verifies if the current request has valid authentication.
 * It checks for the presence of a valid JWT token in the cookies and verifies
 * it exists in the database.
 *
 * Responses:
 *   - 200: JSON response indicating authentication status
 *     {
 *       "authenticated": boolean
 *     }
 */
func (h *Handler) CheckAuthHandler(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Checking authentication status")

	cookie, err := r.Cookie("token")
	if err != nil {
		debug.Debug("No auth token found in cookies")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          nil,
		})
		return
	}

	// Validate token cryptographically
	userID, err := jwt.ValidateJWT(cookie.Value)
	if err != nil {
		debug.Info("Invalid token: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          nil,
		})
		return
	}

	// Verify token exists in database
	exists, err := h.db.TokenExists(cookie.Value)
	if err != nil {
		debug.Error("Error checking token in database: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          nil,
		})
		return
	}
	if !exists {
		debug.Warning("Token not found in database for user ID: %s", userID)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          nil,
		})
		return
	}

	// Get user's role from token
	role, err := jwt.GetUserRole(cookie.Value)
	if err != nil {
		debug.Error("Error getting user role: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"role":          nil,
		})
		return
	}

	debug.Info("Valid authentication found for user ID: %s with role: %s", userID, role)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"role":          role,
	})
}

// Helper function to check if a string is in a slice
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
