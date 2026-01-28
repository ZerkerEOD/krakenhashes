package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	sharedAuth "github.com/ZerkerEOD/krakenhashes/backend/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
)

const (
	totpIssuer        = "KrakenHashes"
	totpDigits        = 6
	totpPeriod        = 30
	totpSkew          = 1 // Accept one period before/after
	emailCodeLength   = 6
	maxVerifyAttempts = 3
	emailCodeValidity = 5 * time.Minute
)

type MFASetupRequest struct {
	Method string `json:"method"` // "email" or "authenticator"
}

type MFAVerifyRequest struct {
	Method       string `json:"method"`
	Code         string `json:"code"`
	SessionToken string `json:"sessionToken"`
}

type MFASetupResponse struct {
	Secret    string `json:"secret,omitempty"`    // For authenticator
	QRCode    string `json:"qrCode,omitempty"`    // For authenticator
	CodeSent  bool   `json:"codeSent,omitempty"`  // For email
	ExpiresAt string `json:"expiresAt,omitempty"` // For email
}

// MFAHandler handles MFA-related requests
type MFAHandler struct {
	db           *db.DB
	emailService EmailService
}

// NewMFAHandler creates a new MFA handler
func NewMFAHandler(db *db.DB, emailService EmailService) *MFAHandler {
	return &MFAHandler{
		db:           db,
		emailService: emailService,
	}
}

// generateTOTPSecret creates a new TOTP secret
func generateTOTPSecret() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base32.StdEncoding.EncodeToString(bytes), nil
}

// generateEmailCode creates a secure random code for email verification
func generateEmailCode() (string, error) {
	const charset = "0123456789"
	code := make([]byte, emailCodeLength)
	bytes := make([]byte, emailCodeLength)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i, b := range bytes {
		code[i] = charset[int(b)%len(charset)]
	}
	return string(code), nil
}

// SetupMFAHandler initiates MFA setup for a user
func (h *Handler) SetupMFAHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)

	var req MFASetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode MFA setup request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	switch strings.ToLower(req.Method) {
	case "authenticator":
		secret, err := generateTOTPSecret()
		if err != nil {
			debug.Error("Failed to generate TOTP secret: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get user's email for the key label - no sensitive data needed here
		user, err := h.db.GetUserByID(userID)
		if err != nil {
			debug.Error("Failed to get user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      totpIssuer,
			AccountName: user.Email,
			Secret:      []byte(secret),
			Digits:      totpDigits,
			Period:      totpPeriod,
			Algorithm:   otp.AlgorithmSHA512,
		})
		if err != nil {
			debug.Error("Failed to generate TOTP key: %v", err)
			http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
			return
		}

		// Store pending MFA setup
		if err := h.db.StorePendingMFASetup(userID, "authenticator", secret); err != nil {
			debug.Error("Failed to store pending MFA setup: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Generate QR code
		key, err = totp.Generate(totp.GenerateOpts{
			Issuer:      totpIssuer,
			AccountName: user.Email,
			Secret:      []byte(secret),
			Digits:      totpDigits,
			Period:      totpPeriod,
			Algorithm:   otp.AlgorithmSHA512,
		})
		if err != nil {
			debug.Error("Failed to generate TOTP key: %v", err)
			http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
			return
		}

		// Generate QR code PNG from the TOTP URL
		qr, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
		if err != nil {
			debug.Error("Failed to generate QR code: %v", err)
			http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
			return
		}

		response := struct {
			Secret string `json:"secret"`
			QRCode string `json:"qrCode"`
		}{
			Secret: secret,
			QRCode: base64.StdEncoding.EncodeToString(qr),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			debug.Error("Failed to encode response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case "email":
		code, err := generateEmailCode()
		if err != nil {
			debug.Error("Failed to generate email code: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get user's email - no sensitive data needed here
		user, err := h.db.GetUserByID(userID)
		if err != nil {
			debug.Error("Failed to get user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Send verification email
		if err := h.emailService.SendMFACode(r.Context(), user.Email, code); err != nil {
			debug.Error("Failed to send MFA code email: %v", err)
			http.Error(w, "Failed to send verification code", http.StatusInternalServerError)
			return
		}

		// Store the code
		if err := h.db.StoreEmailMFACode(userID, code); err != nil {
			debug.Error("Failed to store email MFA code: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(MFASetupResponse{
			CodeSent:  true,
			ExpiresAt: time.Now().Add(emailCodeValidity).Format(time.RFC3339),
		})

	default:
		http.Error(w, "Invalid MFA method", http.StatusBadRequest)
	}
}

// VerifyMFAHandler verifies MFA setup or login
func (h *Handler) VerifyMFAHandler(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Verifying MFA code")

	var req MFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var userID string
	var isLoginFlow bool

	// Check if this is a login flow (session token provided) or setup flow (user context)
	if req.SessionToken != "" {
		// Login flow - get user ID from session
		var err error
		userID, err = h.db.GetUserIDFromMFASession(req.SessionToken)
		if err != nil {
			debug.Error("Failed to get user ID from MFA session: %v", err)
			http.Error(w, "Invalid session", http.StatusUnauthorized)
			return
		}
		isLoginFlow = true
	} else {
		// Setup flow - get user ID from context
		if id := r.Context().Value("user_id"); id != nil {
			userID = id.(string)
		} else {
			debug.Error("No user ID in context and no session token provided")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Get attempts count and max attempts from settings
	attempts, err := h.db.GetMFAVerifyAttempts(req.SessionToken)
	if err != nil {
		debug.Error("Failed to get verify attempts: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	maxAttempts := settings.MFAMaxAttempts
	if maxAttempts < 1 {
		maxAttempts = maxVerifyAttempts // Use default if not set
	}

	// Calculate remaining attempts
	remainingAttempts := maxAttempts - attempts
	if remainingAttempts <= 0 {
		debug.Warning("Max MFA verify attempts reached for user %s", userID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           false,
			"message":           "Too many verification attempts",
			"remainingAttempts": 0,
		})
		return
	}

	// Calculate remaining attempts for use in error responses
	currentRemainingAttempts := remainingAttempts - 1 // Subtract 1 since this attempt will count if it fails
	if currentRemainingAttempts < 0 {
		currentRemainingAttempts = 0
	}

	switch strings.ToLower(req.Method) {
	case "request_email":
		// Get user's email
		user, err := h.db.GetUserByID(userID)
		if err != nil {
			debug.Error("Failed to get user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Generate a random 6-digit code
		code, err := generateEmailCode()
		if err != nil {
			debug.Error("Failed to generate email code: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Send verification email
		if err := h.emailService.SendMFACode(r.Context(), user.Email, code); err != nil {
			debug.Error("Failed to send MFA code email: %v", err)
			http.Error(w, "Failed to send verification code", http.StatusInternalServerError)
			return
		}

		// Store the code
		if err := h.db.StoreEmailMFACode(userID, code); err != nil {
			debug.Error("Failed to store email MFA code: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return success response with remaining attempts
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           true,
			"message":           "Email code sent",
			"remainingAttempts": remainingAttempts,
		})
		return

	case "authenticator":
		// Get user data for verification
		authInfo, err := h.db.GetUserWithMFAData(userID)
		if err != nil {
			debug.Error("Failed to get user MFA data: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// For login flow, use the user's stored secret
		if isLoginFlow {
			debug.Debug("Validating TOTP code for login flow")
			debug.Debug("User MFA secret length: %d, code length: %d", len(authInfo.MFASecret), len(req.Code))
			debug.Debug("TOTP validation parameters: Algorithm=SHA512, Digits=%d, Period=%d, Skew=%d", totpDigits, totpPeriod, totpSkew)

			valid, err := totp.ValidateCustom(
				req.Code,
				authInfo.MFASecret, // Secret is already in base32 format from database
				time.Now().UTC(),
				totp.ValidateOpts{
					Algorithm: otp.AlgorithmSHA512,
					Digits:    totpDigits,
					Period:    totpPeriod,
					Skew:      totpSkew,
				},
			)
			if err != nil {
				debug.Error("Failed to validate TOTP code: %v", err)
				if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
					debug.Error("Failed to increment verify attempts: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":           false,
					"message":           "Failed to validate code",
					"remainingAttempts": currentRemainingAttempts,
				})
				return
			}
			if !valid {
				expectedCode, err := totp.GenerateCode(authInfo.MFASecret, time.Now().UTC())
				if err != nil {
					debug.Error("Failed to generate expected code: %v", err)
				} else {
					debug.Debug("Invalid TOTP code. Expected code for secret at current time: %s", expectedCode)
				}
				if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
					debug.Error("Failed to increment verify attempts: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":           false,
					"message":           "Invalid verification code",
					"remainingAttempts": currentRemainingAttempts,
				})
				return
			}

			// Get user data for token generation
			user, err := h.db.GetUserByID(userID)
			if err != nil {
				debug.Error("Failed to get user data: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Get JWT expiry from auth settings
			authSettings, err := h.db.GetAuthSettings()
			if err != nil {
				debug.Error("Failed to get auth settings: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Generate auth token after successful verification
			token, err := h.generateAuthToken(user, authSettings.JWTExpiryMinutes)
			if err != nil {
				debug.Error("Failed to generate auth token: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Store the token and get token ID
			tokenID, err := h.db.StoreToken(userID, token)
			if err != nil {
				debug.Error("Failed to store token: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Clear MFA session
			if err := h.db.ClearMFASession(req.SessionToken); err != nil {
				debug.Error("Failed to clear MFA session: %v", err)
				// Don't return error as token is already generated
			}

			// Update last login timestamp
			userUUID, _ := uuid.Parse(userID)
			if err := h.db.UpdateLastLogin(userUUID); err != nil {
				debug.Error("Failed to update last login: %v", err)
				// Don't fail the login for this
			}

			// Get client info for session and login attempt logging
			ipAddress, userAgent := sharedAuth.GetClientInfo(r)

			// Create active session linked to token
			session := &models.ActiveSession{
				UserID:    userUUID,
				IPAddress: ipAddress,
				UserAgent: userAgent,
				TokenID:   &tokenID,
			}
			if err := h.db.CreateSession(session); err != nil {
				debug.Error("Failed to create session: %v", err)
				// Don't fail the login for this
			}

			// Log successful login attempt (MFA authenticator)
			loginAttempt := &models.LoginAttempt{
				UserID:    &userUUID,
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
			sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds

			// Return success with token
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"token":   token,
			})
			return
		}

		// For setup flow, use the pending setup secret
		secret, err := h.db.GetPendingMFASetup(userID)
		if err != nil {
			debug.Error("Failed to get pending MFA setup: %v", err)
			http.Error(w, "No pending MFA setup", http.StatusBadRequest)
			return
		}

		debug.Debug("Validating TOTP code for user %s with secret length: %d", userID, len(secret))
		debug.Debug("TOTP validation parameters: Algorithm=SHA512, Digits=%d, Period=%d, Skew=%d", totpDigits, totpPeriod, totpSkew)

		// Verify TOTP code using the stored base32 secret directly
		valid, err := totp.ValidateCustom(
			req.Code,
			secret, // Use the base32 secret directly, no need to decode/re-encode
			time.Now().UTC(),
			totp.ValidateOpts{
				Algorithm: otp.AlgorithmSHA512,
				Digits:    totpDigits,
				Period:    totpPeriod,
				Skew:      totpSkew,
			},
		)
		if err != nil {
			debug.Error("Failed to validate TOTP code: %v", err)
			http.Error(w, "Failed to verify setup", http.StatusInternalServerError)
			return
		}
		if !valid {
			debug.Error("Invalid TOTP code provided during setup")
			http.Error(w, "Invalid verification code", http.StatusBadRequest)
			return
		}

		// Enable MFA
		if err := h.db.EnableMFA(userID, "authenticator", secret); err != nil {
			debug.Error("Failed to enable MFA: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Clear pending setup and attempts
		if err := h.db.ClearPendingMFASetup(userID); err != nil {
			debug.Error("Failed to clear pending MFA setup: %v", err)
		}
		if err := h.db.ClearMFAVerifyAttempts(userID); err != nil {
			debug.Error("Failed to clear verify attempts: %v", err)
		}

		// If this is a login flow, generate a new auth token
		if isLoginFlow {
			// Get user for token generation
			user, err := h.db.GetUserByID(userID)
			if err != nil {
				debug.Error("Failed to get user: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Get JWT expiry from auth settings
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

			// Store the token (ignore token ID - this is just for MFA initiation)
			_, err = h.db.StoreToken(userID, token)
			if err != nil {
				debug.Error("Failed to store token: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Clear MFA session
			if err := h.db.ClearMFASession(req.SessionToken); err != nil {
				debug.Error("Failed to clear MFA session: %v", err)
				// Don't return error as token is already generated
			}

			// Set auth cookie
			sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds

			// Return success with token
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"token":   token,
			})
			return
		} else {
			// Setup flow - just return success
			w.WriteHeader(http.StatusOK)
		}

	case "email":
		// Verify email code
		err = h.db.VerifyEmailMFACode(userID, req.Code)
		if err != nil {
			if err == db.ErrInvalidCode {
				if err := h.db.IncrementMFAVerifyAttempts(userID); err != nil {
					debug.Error("Failed to increment verify attempts: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":           false,
					"message":           "Invalid verification code",
					"remainingAttempts": currentRemainingAttempts,
				})
				return
			}
			debug.Error("Failed to verify email MFA code: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// If this is a login flow, generate a new auth token
		if isLoginFlow {
			// Get user for token generation
			user, err := h.db.GetUserByID(userID)
			if err != nil {
				debug.Error("Failed to get user: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Get JWT expiry from auth settings
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

			// Store the token and get token ID
			tokenID, err := h.db.StoreToken(userID, token)
			if err != nil {
				debug.Error("Failed to store token: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Clear MFA session
			if err := h.db.ClearMFASession(req.SessionToken); err != nil {
				debug.Error("Failed to clear MFA session: %v", err)
				// Don't return error as token is already generated
			}

			// Update last login timestamp
			userUUID, _ := uuid.Parse(userID)
			if err := h.db.UpdateLastLogin(userUUID); err != nil {
				debug.Error("Failed to update last login: %v", err)
				// Don't fail the login for this
			}

			// Get client info for session and login attempt logging
			ipAddress, userAgent := sharedAuth.GetClientInfo(r)

			// Create active session linked to token
			session := &models.ActiveSession{
				UserID:    userUUID,
				IPAddress: ipAddress,
				UserAgent: userAgent,
				TokenID:   &tokenID,
			}
			if err := h.db.CreateSession(session); err != nil {
				debug.Error("Failed to create session: %v", err)
				// Don't fail the login for this
			}

			// Log successful login attempt (MFA email)
			loginAttempt := &models.LoginAttempt{
				UserID:    &userUUID,
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
			sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds

			// Return success with token
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"token":   token,
			})
			return
		} else {
			// Enable MFA for setup flow
			if err := h.db.EnableMFA(userID, "email", ""); err != nil {
				debug.Error("Failed to enable MFA: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Clear attempts
			if err := h.db.ClearMFAVerifyAttempts(userID); err != nil {
				debug.Error("Failed to clear verify attempts: %v", err)
			}

			w.WriteHeader(http.StatusOK)
		}

	case "backup":
		// Get user's backup codes
		userID, err := h.db.GetUserIDFromMFASession(req.SessionToken)
		if err != nil {
			debug.Error("Failed to get user ID from MFA session: %v", err)
			http.Error(w, "Invalid session", http.StatusUnauthorized)
			return
		}

		// Get MFA settings for max attempts
		settings, err := h.db.GetMFASettings()
		if err != nil {
			debug.Error("Failed to get MFA settings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get current attempts
		attempts, err := h.db.GetMFAVerifyAttempts(req.SessionToken)
		if err != nil {
			debug.Error("Failed to get verify attempts: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Calculate remaining attempts
		maxAttempts := settings.MFAMaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 3 // Default if not set
		}
		remainingAttempts := maxAttempts - attempts

		if remainingAttempts <= 0 {
			debug.Warning("Max MFA verify attempts reached for user %s", userID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           false,
				"message":           "Too many verification attempts",
				"remainingAttempts": 0,
			})
			return
		}

		// Get user data to check backup codes
		user, err := h.db.GetUserByID(userID)
		if err != nil {
			debug.Error("Failed to get user: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Validate and use backup code
		valid, err := h.db.ValidateAndUseBackupCode(userID, req.Code)
		if err != nil {
			debug.Error("Failed to validate backup code: %v", err)
			http.Error(w, "Failed to validate code", http.StatusInternalServerError)
			return
		}
		if !valid {
			debug.Warning("Invalid backup code provided by user %s", userID)
			if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
				debug.Error("Failed to increment verify attempts: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           false,
				"message":           "Invalid backup code",
				"remainingAttempts": remainingAttempts - 1,
			})
			return
		}

		// Get JWT expiry from auth settings
		authSettings, err := h.db.GetAuthSettings()
		if err != nil {
			debug.Error("Failed to get auth settings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Generate auth token after successful verification
		token, err := h.generateAuthToken(user, authSettings.JWTExpiryMinutes)
		if err != nil {
			debug.Error("Failed to generate auth token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Store the token and get token ID
		tokenID, err := h.db.StoreToken(userID, token)
		if err != nil {
			debug.Error("Failed to store token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Clear MFA session
		if err := h.db.ClearMFASession(req.SessionToken); err != nil {
			debug.Error("Failed to clear MFA session: %v", err)
			// Don't return error as token is already generated
		}

		// Update last login timestamp
		userUUID, _ := uuid.Parse(userID)
		if err := h.db.UpdateLastLogin(userUUID); err != nil {
			debug.Error("Failed to update last login: %v", err)
			// Don't fail the login for this
		}

		// Get client info for session and login attempt logging
		ipAddress, userAgent := sharedAuth.GetClientInfo(r)

		// Create active session linked to token
		session := &models.ActiveSession{
			UserID:    userUUID,
			IPAddress: ipAddress,
			UserAgent: userAgent,
			TokenID:   &tokenID,
		}
		if err := h.db.CreateSession(session); err != nil {
			debug.Error("Failed to create session: %v", err)
			// Don't fail the login for this
		}

		// Log successful login attempt (MFA backup code)
		loginAttempt := &models.LoginAttempt{
			UserID:    &userUUID,
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
		sharedAuth.SetAuthCookie(w, r, token, authSettings.JWTExpiryMinutes*60) // Convert minutes to seconds

		// Return success with token
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"token":   token,
		})
		return

	default:
		debug.Warning("Invalid MFA method requested by user %s: %s", userID, req.Method)
		http.Error(w, "Invalid MFA method", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// EnableMFA enables MFA for a user
func (h *MFAHandler) EnableMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req MFASetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get global MFA settings
	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// If MFA is required, check if the method is allowed
	if settings.RequireMFA {
		methodAllowed := false
		for _, method := range settings.AllowedMFAMethods {
			if method == req.Method {
				methodAllowed = true
				break
			}
		}

		if !methodAllowed {
			debug.Error("MFA method %s is not allowed when MFA is required", req.Method)
			http.Error(w, "Selected MFA method is not allowed", http.StatusBadRequest)
			return
		}
	}

	// Get user data for setup
	basicUser, err := h.db.GetUserByID(userID)
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if email provider is configured
	hasEmailProvider, err := h.db.HasActiveEmailProvider()
	if err != nil {
		debug.Error("Failed to check email provider: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// If no email provider and method is email, return error
	if !hasEmailProvider && req.Method == "email" {
		debug.Error("Email MFA requested but no email provider configured")
		http.Error(w, "Email MFA requires email configuration. Please use authenticator method instead.", http.StatusBadRequest)
		return
	}

	switch req.Method {
	case "email":
		// Email MFA is enabled by default when MFA is enabled
		if err := h.db.EnableMFA(userID, "email", ""); err != nil {
			debug.Error("Failed to enable email MFA: %v", err)
			http.Error(w, "Failed to enable MFA", http.StatusInternalServerError)
			return
		}

		// Set email as preferred method
		if err := h.db.SetPreferredMFAMethod(userID, "email"); err != nil {
			debug.Error("Failed to set preferred MFA method: %v", err)
			// Don't return error as MFA is still enabled
		}

		w.WriteHeader(http.StatusOK)

	case "authenticator":
		// Generate TOTP secret
		secret := make([]byte, 20)
		if _, err := rand.Read(secret); err != nil {
			debug.Error("Failed to generate secret: %v", err)
			http.Error(w, "Failed to generate secret", http.StatusInternalServerError)
			return
		}

		debug.Debug("Generated raw secret of length: %d bytes", len(secret))
		secretBase32 := base32.StdEncoding.EncodeToString(secret)
		debug.Debug("Encoded secret to base32 of length: %d chars", len(secretBase32))

		// Store pending MFA setup
		if err := h.db.StorePendingMFASetup(userID, "authenticator", secretBase32); err != nil {
			debug.Error("Failed to store pending MFA setup: %v", err)
			http.Error(w, "Failed to store MFA setup", http.StatusInternalServerError)
			return
		}

		// Generate QR code
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      totpIssuer,
			AccountName: basicUser.Email,
			Secret:      secret, // Use the raw bytes for QR code generation
			Digits:      totpDigits,
			Period:      totpPeriod,
			Algorithm:   otp.AlgorithmSHA512,
		})
		if err != nil {
			debug.Error("Failed to generate TOTP key: %v", err)
			http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
			return
		}

		debug.Debug("Generated TOTP URL: %s", key.URL())

		// Generate QR code PNG from the TOTP URL
		qr, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
		if err != nil {
			debug.Error("Failed to generate QR code: %v", err)
			http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
			return
		}

		response := struct {
			Secret string `json:"secret"`
			QRCode string `json:"qrCode"`
		}{
			Secret: secretBase32,
			QRCode: base64.StdEncoding.EncodeToString(qr),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			debug.Error("Failed to encode response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case "passkey":
		// Verify user has at least one passkey registered
		userIDUUID, err := uuid.Parse(userID)
		if err != nil {
			debug.Error("Failed to parse user ID: %v", err)
			http.Error(w, "Invalid user ID", http.StatusBadRequest)
			return
		}

		passkeys, err := h.db.GetUserPasskeys(userIDUUID)
		if err != nil {
			debug.Error("Failed to get user passkeys: %v", err)
			http.Error(w, "Failed to verify passkey setup", http.StatusInternalServerError)
			return
		}

		if len(passkeys) == 0 {
			debug.Error("User %s has no registered passkeys", userID)
			http.Error(w, "Please register a passkey first", http.StatusBadRequest)
			return
		}

		// Enable MFA with passkey method (empty secret - passkeys use credential IDs)
		if err := h.db.EnableMFA(userID, "passkey", ""); err != nil {
			debug.Error("Failed to enable passkey MFA: %v", err)
			http.Error(w, "Failed to enable MFA", http.StatusInternalServerError)
			return
		}

		// Set passkey as preferred method
		if err := h.db.SetPreferredMFAMethod(userID, "passkey"); err != nil {
			debug.Error("Failed to set preferred MFA method: %v", err)
			// Don't fail - MFA is still enabled
		}

		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Invalid MFA method", http.StatusBadRequest)
	}
}

// VerifyMFASetup verifies the setup of authenticator-based MFA
func (h *MFAHandler) VerifyMFASetup(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	debug.Debug("Starting MFA setup verification for user: %s", userID)

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	debug.Debug("Received verification code of length %d", len(req.Code))

	// Get pending setup
	secret, err := h.db.GetPendingMFASetup(userID)
	if err != nil {
		debug.Error("Failed to get pending MFA setup: %v", err)
		http.Error(w, "Failed to verify setup", http.StatusInternalServerError)
		return
	}

	debug.Debug("Retrieved secret of length %d for verification", len(secret))
	debug.Debug("TOTP validation parameters: Algorithm=SHA512, Digits=%d, Period=%d, Skew=%d", totpDigits, totpPeriod, totpSkew)

	// Verify TOTP code using the stored base32 secret directly
	valid, err := totp.ValidateCustom(
		req.Code,
		secret, // Use the base32 secret directly, no need to decode/re-encode
		time.Now().UTC(),
		totp.ValidateOpts{
			Algorithm: otp.AlgorithmSHA512,
			Digits:    totpDigits,
			Period:    totpPeriod,
			Skew:      totpSkew,
		},
	)
	if err != nil {
		debug.Error("TOTP validation error: %v", err)
		if err := h.db.IncrementMFAVerifyAttempts(userID); err != nil {
			debug.Error("Failed to increment verify attempts: %v", err)
		}
		http.Error(w, "Failed to validate code", http.StatusInternalServerError)
		return
	}
	if !valid {
		debug.Warning("Invalid TOTP code provided for user %s", userID)
		if err := h.db.IncrementMFAVerifyAttempts(userID); err != nil {
			debug.Error("Failed to increment verify attempts: %v", err)
		}
		http.Error(w, "Invalid verification code", http.StatusBadRequest)
		return
	}

	// Start transaction for atomic MFA setup
	tx, err := h.db.Begin()
	if err != nil {
		debug.Error("Failed to start transaction: %v", err)
		http.Error(w, "Failed to enable MFA", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Enable authenticator MFA
	if _, err = tx.Exec(queries.EnableMFAQuery, userID, "authenticator", secret); err != nil {
		debug.Error("Failed to enable authenticator MFA: %v", err)
		http.Error(w, "Failed to enable MFA", http.StatusInternalServerError)
		return
	}

	// Get backup code count from settings
	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	// Use configured count, fallback to 8 if not set
	codeCount := settings.BackupCodesCount
	if codeCount < 1 {
		codeCount = 8
	}

	// Generate backup codes
	codes := make([]string, codeCount)
	for i := range codes {
		// Generate 5 random bytes for 8 characters when base32 encoded
		bytes := make([]byte, 5)
		if _, err = rand.Read(bytes); err != nil {
			debug.Error("Failed to generate backup code: %v", err)
			http.Error(w, "Failed to generate backup codes", http.StatusInternalServerError)
			return
		}
		// Convert to base32 and take first 8 characters
		codes[i] = strings.ToUpper(base32.StdEncoding.EncodeToString(bytes)[:8])
	}

	// Store the codes directly without hashing
	if _, err = tx.Exec(queries.StoreBackupCodesQuery, userID, pq.Array(codes)); err != nil {
		debug.Error("Failed to store backup codes: %v", err)
		http.Error(w, "Failed to store backup codes", http.StatusInternalServerError)
		return
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		debug.Error("Failed to commit MFA setup transaction: %v", err)
		http.Error(w, "Failed to complete MFA setup", http.StatusInternalServerError)
		return
	}

	// Return success with backup codes
	response := struct {
		BackupCodes []string `json:"backupCodes"`
	}{
		BackupCodes: codes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DisableMFA handles the request to disable MFA for a user
func (h *MFAHandler) DisableMFA(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if MFA is required by policy
	required, err := h.db.IsMFARequired()
	if err != nil {
		debug.Error("Failed to check MFA requirement: %v", err)
		http.Error(w, "Failed to check MFA requirement", http.StatusInternalServerError)
		return
	}

	if required {
		http.Error(w, "MFA is required by organization policy", http.StatusForbidden)
		return
	}

	// Get user's current MFA method before disabling (for notification)
	mfaSettings, _ := h.db.GetUserMFASettings(userID)
	disabledMethod := "Unknown"
	if mfaSettings != nil && len(mfaSettings.MFAType) > 0 {
		disabledMethod = strings.Join(mfaSettings.MFAType, ", ")
	}

	if err := h.db.DisableMFA(userID); err != nil {
		debug.Error("Failed to disable MFA: %v", err)
		http.Error(w, "Failed to disable MFA", http.StatusInternalServerError)
		return
	}

	// Dispatch MFA disabled notification (mandatory security notification)
	go dispatchMFADisabledNotification(r, userID, disabledMethod)

	w.WriteHeader(http.StatusOK)
}

// GenerateBackupCodes generates new backup codes for a user
func (h *MFAHandler) GenerateBackupCodes(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get backup code count from settings
	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	// Use configured count, fallback to 8 if not set
	codeCount := settings.BackupCodesCount
	if codeCount < 1 {
		codeCount = 8
	}

	// Generate backup codes
	codes := make([]string, codeCount)
	for i := range codes {
		// Generate 5 random bytes for 8 characters when base32 encoded
		bytes := make([]byte, 5)
		if _, err := rand.Read(bytes); err != nil {
			debug.Error("Failed to generate backup code: %v", err)
			http.Error(w, "Failed to generate backup codes", http.StatusInternalServerError)
			return
		}
		// Convert to base32 and take first 8 characters
		codes[i] = strings.ToUpper(base32.StdEncoding.EncodeToString(bytes)[:8])
	}

	// Store the codes directly without hashing
	if err := h.db.StoreBackupCodes(userID, codes); err != nil {
		debug.Error("Failed to store backup codes: %v", err)
		http.Error(w, "Failed to store backup codes", http.StatusInternalServerError)
		return
	}

	// Return success with backup codes
	response := struct {
		BackupCodes []string `json:"backupCodes"`
	}{
		BackupCodes: codes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SendEmailMFACode generates and sends a new email MFA code
func (h *MFAHandler) SendEmailMFACode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user ID from session token
	userID, err := h.db.GetUserIDFromMFASession(req.SessionToken)
	if err != nil {
		debug.Error("Failed to get user ID from MFA session: %v", err)
		http.Error(w, "Invalid session", http.StatusUnauthorized)
		return
	}

	// Get MFA settings for max attempts
	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get current attempts
	attempts, err := h.db.GetMFAVerifyAttempts(req.SessionToken)
	if err != nil {
		debug.Error("Failed to get verify attempts: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Calculate remaining attempts
	remainingAttempts := settings.MFAMaxAttempts - attempts
	if remainingAttempts <= 0 {
		debug.Warning("Max MFA verify attempts reached for user %s", userID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           false,
			"message":           "Too many verification attempts",
			"remainingAttempts": 0,
		})
		return
	}

	debug.Debug("Generating email MFA code for user %s", userID)

	// Get user's email
	user, err := h.db.GetUserByID(userID)
	if err != nil {
		debug.Error("Failed to get user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Generate a random 6-digit code
	code, err := generateEmailCode()
	if err != nil {
		debug.Error("Failed to generate email code: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store the code
	err = h.db.StoreEmailMFACode(userID, code)
	if err != nil {
		if err == db.ErrMFACooldown {
			debug.Info("MFA code request on cooldown for user %s", userID)
			http.Error(w, "Please wait before requesting a new code", http.StatusTooManyRequests)
			return
		}
		debug.Error("Failed to store email MFA code for user %s: %v", userID, err)
		http.Error(w, "Failed to generate code", http.StatusInternalServerError)
		return
	}

	debug.Debug("Successfully stored email MFA code for user %s", userID)

	// Send the code via email
	if err := h.emailService.SendMFACode(r.Context(), user.Email, code); err != nil {
		debug.Error("Failed to send email MFA code: %v", err)
		http.Error(w, "Failed to send verification code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":           true,
		"remainingAttempts": remainingAttempts,
	})

	debug.Info("Successfully sent email MFA code for user %s", userID)
}

// VerifyMFACode verifies a provided MFA code during login
func (h *MFAHandler) VerifyMFACode(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		debug.Warning("No user ID found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	debug.Debug("Verifying MFA code for user %s", userID)

	var req MFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Invalid request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get attempts count
	attempts, err := h.db.GetMFAVerifyAttempts(req.SessionToken)
	if err != nil {
		debug.Error("Failed to get verify attempts: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Calculate remaining attempts
	remainingAttempts := maxVerifyAttempts - attempts
	if remainingAttempts <= 0 {
		debug.Warning("Max MFA verify attempts reached for user %s", userID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           false,
			"message":           "Too many verification attempts",
			"remainingAttempts": 0,
		})
		return
	}

	// Get user data for verification
	authInfo, err := h.db.GetUserWithMFAData(userID)
	if err != nil {
		debug.Error("Failed to get user MFA data: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	switch req.Method {
	case "authenticator":
		debug.Debug("Validating TOTP code for user %s", userID)
		valid, err := totp.ValidateCustom(
			req.Code,
			authInfo.MFASecret,
			time.Now().UTC(),
			totp.ValidateOpts{
				Algorithm: otp.AlgorithmSHA512,
				Digits:    totpDigits,
				Period:    totpPeriod,
				Skew:      totpSkew,
			},
		)
		if err != nil {
			debug.Error("Failed to validate TOTP code for user %s: %v", userID, err)
			if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
				debug.Error("Failed to increment verify attempts: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           false,
				"message":           "Failed to validate code",
				"remainingAttempts": remainingAttempts - 1,
			})
			return
		}
		if !valid {
			expectedCode, err := totp.GenerateCode(authInfo.MFASecret, time.Now().UTC())
			if err != nil {
				debug.Error("Failed to generate expected code: %v", err)
			} else {
				debug.Debug("Invalid TOTP code. Expected code for secret at current time: %s", expectedCode)
			}
			if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
				debug.Error("Failed to increment verify attempts: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           false,
				"message":           "Invalid verification code",
				"remainingAttempts": remainingAttempts - 1,
			})
			return
		}
		debug.Info("Successfully verified TOTP code for user %s", userID)

	case "backup":
		if len(authInfo.BackupCodes) == 0 {
			debug.Warning("User %s attempted to use backup code but none are available", userID)
			http.Error(w, "No backup codes available", http.StatusBadRequest)
			return
		}

		// Validate and use backup code
		valid, err := h.db.ValidateAndUseBackupCode(userID, req.Code)
		if err != nil {
			debug.Error("Failed to validate backup code for user %s: %v", userID, err)
			http.Error(w, "Failed to validate code", http.StatusInternalServerError)
			return
		}
		if !valid {
			debug.Warning("Invalid backup code provided by user %s", userID)
			if err := h.db.IncrementMFAVerifyAttempts(req.SessionToken); err != nil {
				debug.Error("Failed to increment attempts: %v", err)
			}
			attempts, err := h.db.GetMFAVerifyAttempts(req.SessionToken)
			if err != nil {
				debug.Error("Failed to get attempts: %v", err)
				attempts = maxVerifyAttempts // Default to max attempts on error
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":           false,
				"message":           "Invalid backup code",
				"remainingAttempts": maxVerifyAttempts - attempts,
			})
			return
		}
		debug.Info("Successfully verified backup code for user %s", userID)

	default:
		debug.Warning("Invalid MFA method requested by user %s: %s", userID, req.Method)
		http.Error(w, "Invalid MFA method", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUserMFASettings returns the MFA settings for the authenticated user
func (h *MFAHandler) GetUserMFASettings(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		debug.Warning("No user ID found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	debug.Debug("Getting MFA settings for user: %s", userID)

	// Get global MFA settings
	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if email provider is configured
	hasEmailProvider, err := h.db.HasActiveEmailProvider()
	if err != nil {
		debug.Error("Failed to check email provider: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Filter allowed methods based on actual availability
	allowedMethods := make([]string, 0, len(settings.AllowedMFAMethods))
	for _, method := range settings.AllowedMFAMethods {
		// Only include email if email provider is configured
		if method == "email" && !hasEmailProvider {
			continue
		}
		allowedMethods = append(allowedMethods, method)
	}

	// Get user's MFA settings
	mfaSettings, err := h.db.GetUserMFASettings(userID)
	if err != nil {
		debug.Error("Failed to get user MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get remaining backup codes count
	remainingCodes := 0
	if mfaSettings.BackupCodes != nil {
		remainingCodes = len(mfaSettings.BackupCodes)
	}

	response := struct {
		RequireMFA           bool     `json:"requireMfa"`
		AllowedMFAMethods    []string `json:"allowedMfaMethods"`
		MfaEnabled           bool     `json:"mfaEnabled"`
		MfaType              []string `json:"mfaType,omitempty"`
		PreferredMethod      string   `json:"preferredMethod,omitempty"`
		RemainingBackupCodes int      `json:"remainingBackupCodes"`
	}{
		RequireMFA:           settings.RequireMFA,
		AllowedMFAMethods:    allowedMethods,
		MfaEnabled:           mfaSettings.MFAEnabled,
		MfaType:              mfaSettings.MFAType,
		PreferredMethod:      mfaSettings.PreferredMFAMethod,
		RemainingBackupCodes: remainingCodes,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response for user %s: %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// GetMFASettings returns the MFA settings for a user
func (h *MFAHandler) GetMFASettings(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Failed to get MFA settings", http.StatusInternalServerError)
		return
	}

	mfaSettings, err := h.db.GetUserMFASettings(userID)
	if err != nil {
		debug.Error("Failed to get user MFA settings: %v", err)
		http.Error(w, "Failed to get user MFA settings", http.StatusInternalServerError)
		return
	}

	response := struct {
		RequireMFA        bool     `json:"requireMfa"`
		AllowedMFAMethods []string `json:"allowedMfaMethods"`
		MfaEnabled        bool     `json:"mfaEnabled"`
		MfaType           []string `json:"mfaType,omitempty"`
	}{
		RequireMFA:        settings.RequireMFA,
		AllowedMFAMethods: settings.AllowedMFAMethods,
		MfaEnabled:        mfaSettings.MFAEnabled,
		MfaType:           mfaSettings.MFAType,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response for user %s: %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// UpdatePreferredMFAMethod updates the user's preferred MFA method
func (h *MFAHandler) UpdatePreferredMFAMethod(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Error("Failed to decode request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate method
	if !models.IsValidPreferredMFAType(req.Method) {
		debug.Error("Invalid MFA method requested: %s", req.Method)
		http.Error(w, "Invalid MFA method", http.StatusBadRequest)
		return
	}

	// Get user's current MFA settings
	mfaSettings, err := h.db.GetUserMFASettings(userID)
	if err != nil {
		debug.Error("Failed to get user MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if user has the requested method enabled
	if !mfaSettings.MFAEnabled || !contains(mfaSettings.MFAType, req.Method) {
		debug.Error("User %s attempted to set preferred method %s but it's not enabled", userID, req.Method)
		http.Error(w, "Requested MFA method is not enabled for your account", http.StatusBadRequest)
		return
	}

	// Update preferred method
	if err := h.db.SetPreferredMFAMethod(userID, req.Method); err != nil {
		debug.Error("Failed to update preferred MFA method: %v", err)
		http.Error(w, "Failed to update preferred method", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DisableAuthenticator handles the request to disable authenticator MFA for a user
func (h *MFAHandler) DisableAuthenticator(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user's current MFA settings
	mfaSettings, err := h.db.GetUserMFASettings(userID)
	if err != nil {
		debug.Error("Failed to get user MFA settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Check if authenticator is enabled
	if !contains(mfaSettings.MFAType, "authenticator") {
		debug.Warning("User %s attempted to disable authenticator but it's not enabled", userID)
		http.Error(w, "Authenticator is not enabled", http.StatusBadRequest)
		return
	}

	// Execute the query to remove the authenticator method
	_, err = h.db.Exec(queries.RemoveMFAMethodQuery, userID, "authenticator")
	if err != nil {
		debug.Error("Failed to remove authenticator method: %v", err)
		http.Error(w, "Failed to disable authenticator", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// dispatchMFADisabledNotification sends a security notification when MFA is disabled
// This is a mandatory notification that cannot be disabled by user preferences
func dispatchMFADisabledNotification(r *http.Request, userID string, disabledMethod string) {
	dispatcher := services.GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping MFA disabled notification")
		return
	}

	// Parse user ID
	uid, err := uuid.Parse(userID)
	if err != nil {
		debug.Error("Failed to parse user ID for notification: %v", err)
		return
	}

	// Get client info
	ipAddress := getClientIPFromRequest(r)
	userAgent := r.UserAgent()

	params := models.NotificationDispatchParams{
		UserID:  uid,
		Type:    models.NotificationTypeSecurityMFADisabled,
		Title:   "Two-Factor Authentication Disabled",
		Message: "Two-factor authentication has been disabled on your account. If you did not make this change, please re-enable MFA immediately and contact support.",
		Data: map[string]interface{}{
			"disabled_method": disabledMethod,
			"ip_address":      ipAddress,
			"user_agent":      userAgent,
			"timestamp":       time.Now().Format(time.RFC3339),
		},
		SourceType: "security",
		SourceID:   uuid.New().String(), // Unique per event - security events should not deduplicate
	}

	if err := dispatcher.Dispatch(context.Background(), params); err != nil {
		debug.Error("Failed to dispatch MFA disabled notification: %v", err)
	} else {
		debug.Log("MFA disabled notification dispatched", map[string]interface{}{
			"user_id":         userID,
			"disabled_method": disabledMethod,
		})
	}
}

// getClientIPFromRequest extracts the client IP address from the request
func getClientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	// RemoteAddr is in the format "IP:port", so we need to extract just the IP
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
