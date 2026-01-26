package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// AuthSettingsResponse represents the authentication settings response
type AuthSettingsResponse struct {
	// Password Policy
	MinPasswordLength   int  `json:"minPasswordLength"`
	RequireUppercase    bool `json:"requireUppercase"`
	RequireLowercase    bool `json:"requireLowercase"`
	RequireNumbers      bool `json:"requireNumbers"`
	RequireSpecialChars bool `json:"requireSpecialChars"`

	// MFA Settings
	RequireMFA        bool     `json:"requireMfa"`
	AllowedMFAMethods []string `json:"allowedMfaMethods"`
	EmailCodeValidity int      `json:"emailCodeValidity"` // in minutes
	BackupCodesCount  int      `json:"backupCodesCount"`

	// Account Security
	MaxFailedAttempts int `json:"maxFailedAttempts"`
	LockoutDuration   int `json:"lockoutDuration"` // in minutes
	SessionTimeout    int `json:"sessionTimeout"`  // in minutes
	JWTExpiryMinutes  int `json:"jwtExpiryMinutes"`
}

// AuthSettingsHandler handles authentication settings requests
type AuthSettingsHandler struct {
	db *db.DB
}

// NewAuthSettingsHandler creates a new auth settings handler
func NewAuthSettingsHandler(db *db.DB) *AuthSettingsHandler {
	return &AuthSettingsHandler{db: db}
}

// GetSettings retrieves the current authentication settings
func (h *AuthSettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting authentication settings")

	settings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	// Convert DB settings to response format
	response := AuthSettingsResponse{
		MinPasswordLength:   settings.MinPasswordLength,
		RequireUppercase:    settings.RequireUppercase,
		RequireLowercase:    settings.RequireLowercase,
		RequireNumbers:      settings.RequireNumbers,
		RequireSpecialChars: settings.RequireSpecialChars,
		RequireMFA:          settings.RequireMFA,
		AllowedMFAMethods:   []string{"email", "authenticator"}, // Default supported methods
		EmailCodeValidity:   5,                                  // Default 5 minutes
		BackupCodesCount:    8,                                  // Default 8 codes
		MaxFailedAttempts:   settings.MaxFailedAttempts,
		LockoutDuration:     settings.LockoutDurationMinutes,
		SessionTimeout:      60, // Default 60 minutes
		JWTExpiryMinutes:    settings.JWTExpiryMinutes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateSettings updates the authentication settings
func (h *AuthSettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received request to update auth settings")

	var settings struct {
		MinPasswordLength              int    `json:"min_password_length"`
		RequireUppercase               bool   `json:"require_uppercase"`
		RequireLowercase               bool   `json:"require_lowercase"`
		RequireNumbers                 bool   `json:"require_numbers"`
		RequireSpecialChars            bool   `json:"require_special_chars"`
		MaxFailedAttempts              int    `json:"max_failed_attempts"`
		LockoutDurationMinutes         int    `json:"lockout_duration_minutes"`
		JWTExpiryMinutes               int    `json:"jwt_expiry_minutes"`
		DisplayTimezone                string `json:"display_timezone"`
		NotificationAggregationMinutes int    `json:"notification_aggregation_minutes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		debug.Error("Failed to decode settings: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	debug.Info("Decoded settings: %+v", settings)

	// Create model settings
	modelSettings := &models.AuthSettings{
		MinPasswordLength:              settings.MinPasswordLength,
		RequireUppercase:               settings.RequireUppercase,
		RequireLowercase:               settings.RequireLowercase,
		RequireNumbers:                 settings.RequireNumbers,
		RequireSpecialChars:            settings.RequireSpecialChars,
		MaxFailedAttempts:              settings.MaxFailedAttempts,
		LockoutDurationMinutes:         settings.LockoutDurationMinutes,
		JWTExpiryMinutes:               settings.JWTExpiryMinutes,
		DisplayTimezone:                settings.DisplayTimezone,
		NotificationAggregationMinutes: settings.NotificationAggregationMinutes,
	}

	// Update database settings
	if err := h.db.UpdateAuthSettings(modelSettings); err != nil {
		debug.Error("Failed to update auth settings: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully updated auth settings")
	w.WriteHeader(http.StatusOK)
}

// validateSettings checks if the settings are valid
func validateSettings(s *AuthSettingsResponse) error {
	// Add validation logic here
	// For example:
	// - Minimum password length >= 8
	// - At least one MFA method enabled if MFA is required
	// - Valid ranges for timeouts and attempts
	return nil
}

// GetMFASettings retrieves the current MFA settings
func (h *AuthSettingsHandler) GetMFASettings(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting MFA settings")

	settings, err := h.db.GetMFASettings()
	if err != nil {
		debug.Error("Failed to get MFA settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	response := struct {
		RequireMFA             bool     `json:"requireMfa"`
		AllowedMFAMethods      []string `json:"allowedMfaMethods"`
		EmailCodeValidity      int      `json:"emailCodeValidity"`
		BackupCodesCount       int      `json:"backupCodesCount"`
		MFACodeCooldownMinutes int      `json:"mfaCodeCooldownMinutes"`
		MFACodeExpiryMinutes   int      `json:"mfaCodeExpiryMinutes"`
		MFAMaxAttempts         int      `json:"mfaMaxAttempts"`
	}{
		RequireMFA:             settings.RequireMFA,
		AllowedMFAMethods:      settings.AllowedMFAMethods,
		EmailCodeValidity:      settings.EmailCodeValidityMinutes,
		BackupCodesCount:       settings.BackupCodesCount,
		MFACodeCooldownMinutes: settings.MFACodeCooldownMinutes,
		MFACodeExpiryMinutes:   settings.MFACodeExpiryMinutes,
		MFAMaxAttempts:         settings.MFAMaxAttempts,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateMFASettings updates the MFA settings
func (h *AuthSettingsHandler) UpdateMFASettings(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received request to update MFA settings")

	var settings struct {
		RequireMFA             bool     `json:"requireMfa"`
		AllowedMFAMethods      []string `json:"allowedMfaMethods"`
		EmailCodeValidity      int      `json:"emailCodeValidity"`
		BackupCodesCount       int      `json:"backupCodesCount"`
		MFACodeCooldownMinutes int      `json:"mfaCodeCooldownMinutes"`
		MFACodeExpiryMinutes   int      `json:"mfaCodeExpiryMinutes"`
		MFAMaxAttempts         int      `json:"mfaMaxAttempts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		debug.Error("Failed to decode MFA settings: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	debug.Info("Decoded MFA settings: %+v", settings)

	// Check if trying to enable global MFA
	if settings.RequireMFA {
		// Check if email provider is configured
		hasEmailProvider, err := h.db.HasActiveEmailProvider()
		if err != nil {
			debug.Error("Failed to check email provider: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if !hasEmailProvider {
			debug.Error("Cannot enable global MFA: no active email provider configured")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "An email gateway must be configured before enabling global MFA. Please configure an email gateway in the Email Settings page first.",
				"code":  "EMAIL_GATEWAY_REQUIRED",
			})
			return
		}
	}

	// Validate settings
	if err := validateMFASettings(&settings); err != nil {
		debug.Error("Invalid MFA settings: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update database settings
	if err := h.db.UpdateMFASettings(
		settings.RequireMFA,
		settings.AllowedMFAMethods,
		settings.EmailCodeValidity,
		settings.BackupCodesCount,
		settings.MFACodeCooldownMinutes,
		settings.MFACodeExpiryMinutes,
		settings.MFAMaxAttempts,
	); err != nil {
		debug.Error("Failed to update MFA settings: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// If global MFA is being enabled, enable it for all active users
	if settings.RequireMFA {
		if err := h.db.BulkEnableMFA(); err != nil {
			debug.Error("Failed to bulk enable MFA: %v", err)
			http.Error(w, "Failed to enable MFA for all users", http.StatusInternalServerError)
			return
		}
	}

	debug.Info("Successfully updated MFA settings")
	w.WriteHeader(http.StatusOK)
}

// validateMFASettings checks if the MFA settings are valid
func validateMFASettings(s *struct {
	RequireMFA             bool     `json:"requireMfa"`
	AllowedMFAMethods      []string `json:"allowedMfaMethods"`
	EmailCodeValidity      int      `json:"emailCodeValidity"`
	BackupCodesCount       int      `json:"backupCodesCount"`
	MFACodeCooldownMinutes int      `json:"mfaCodeCooldownMinutes"`
	MFACodeExpiryMinutes   int      `json:"mfaCodeExpiryMinutes"`
	MFAMaxAttempts         int      `json:"mfaMaxAttempts"`
}) error {
	if s.RequireMFA && len(s.AllowedMFAMethods) == 0 {
		return fmt.Errorf("at least one MFA method must be enabled when MFA is required")
	}

	if s.EmailCodeValidity < 1 {
		return fmt.Errorf("email code validity must be at least 1 minute")
	}

	if s.BackupCodesCount < 1 {
		return fmt.Errorf("backup codes count must be at least 1")
	}

	if s.MFACodeCooldownMinutes < 1 {
		return fmt.Errorf("MFA code cooldown must be at least 1 minute")
	}

	if s.MFACodeExpiryMinutes < 1 {
		return fmt.Errorf("MFA code expiry must be at least 1 minute")
	}

	if s.MFAMaxAttempts < 1 {
		return fmt.Errorf("maximum attempts must be at least 1")
	}

	// Validate allowed MFA methods
	validMethods := map[string]bool{
		"email":         true,
		"authenticator": true,
		"passkey":       true,
	}

	for _, method := range s.AllowedMFAMethods {
		if !validMethods[method] {
			return fmt.Errorf("invalid MFA method: %s", method)
		}
	}

	return nil
}

// GetPasswordPolicy retrieves the current password policy settings
func (h *AuthSettingsHandler) GetPasswordPolicy(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting password policy settings")

	settings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get password policy settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	response := struct {
		MinPasswordLength   int  `json:"minPasswordLength"`
		RequireUppercase    bool `json:"requireUppercase"`
		RequireLowercase    bool `json:"requireLowercase"`
		RequireNumbers      bool `json:"requireNumbers"`
		RequireSpecialChars bool `json:"requireSpecialChars"`
	}{
		MinPasswordLength:   settings.MinPasswordLength,
		RequireUppercase:    settings.RequireUppercase,
		RequireLowercase:    settings.RequireLowercase,
		RequireNumbers:      settings.RequireNumbers,
		RequireSpecialChars: settings.RequireSpecialChars,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAccountSecurity retrieves the current account security settings
func (h *AuthSettingsHandler) GetAccountSecurity(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting account security settings")

	settings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get account security settings: %v", err)
		http.Error(w, "Failed to get settings", http.StatusInternalServerError)
		return
	}

	response := struct {
		MaxFailedAttempts              int `json:"maxFailedAttempts"`
		LockoutDuration                int `json:"lockoutDuration"`
		JWTExpiryMinutes               int `json:"jwtExpiryMinutes"`
		NotificationAggregationMinutes int `json:"notificationAggregationMinutes"`
	}{
		MaxFailedAttempts:              settings.MaxFailedAttempts,
		LockoutDuration:                settings.LockoutDurationMinutes,
		JWTExpiryMinutes:               settings.JWTExpiryMinutes,
		NotificationAggregationMinutes: settings.NotificationAggregationMinutes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
