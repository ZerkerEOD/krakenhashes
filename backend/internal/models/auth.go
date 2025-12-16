package models

import (
	"time"

	"github.com/google/uuid"
)

// AuthSettings represents the system-wide authentication settings
type AuthSettings struct {
	ID                             uuid.UUID `json:"id" db:"id"`
	MinPasswordLength              int       `json:"min_password_length" db:"min_password_length"`
	RequireUppercase               bool      `json:"require_uppercase" db:"require_uppercase"`
	RequireLowercase               bool      `json:"require_lowercase" db:"require_lowercase"`
	RequireNumbers                 bool      `json:"require_numbers" db:"require_numbers"`
	RequireSpecialChars            bool      `json:"require_special_chars" db:"require_special_chars"`
	MaxFailedAttempts              int       `json:"max_failed_attempts" db:"max_failed_attempts"`
	LockoutDurationMinutes         int       `json:"lockout_duration_minutes" db:"lockout_duration_minutes"`
	RequireMFA                     bool      `json:"require_mfa" db:"require_mfa"`
	JWTExpiryMinutes               int       `json:"jwt_expiry_minutes" db:"jwt_expiry_minutes"`
	DisplayTimezone                string    `json:"display_timezone" db:"display_timezone"`
	NotificationAggregationMinutes int       `json:"notification_aggregation_minutes" db:"notification_aggregation_minutes"`
	TokenCleanupIntervalSeconds    int       `json:"token_cleanup_interval_seconds" db:"token_cleanup_interval_seconds"`
	MaxConcurrentSessions          int       `json:"max_concurrent_sessions" db:"max_concurrent_sessions"`
	SessionAbsoluteTimeoutHours    int       `json:"session_absolute_timeout_hours" db:"session_absolute_timeout_hours"`
	// WebAuthn/Passkey settings
	WebAuthnRPID          *string  `json:"webauthn_rp_id" db:"webauthn_rp_id"`
	WebAuthnRPOrigins     []string `json:"webauthn_rp_origins" db:"webauthn_rp_origins"`
	WebAuthnRPDisplayName *string  `json:"webauthn_rp_display_name" db:"webauthn_rp_display_name"`
}

// LoginAttempt represents a user login attempt
type LoginAttempt struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	UserID        *uuid.UUID `json:"user_id" db:"user_id"`
	Username      string     `json:"username" db:"username"`
	IPAddress     string     `json:"ip_address" db:"ip_address"`
	UserAgent     string     `json:"user_agent" db:"user_agent"`
	Success       bool       `json:"success" db:"success"`
	FailureReason string     `json:"failure_reason" db:"failure_reason"`
	AttemptedAt   time.Time  `json:"attempted_at" db:"attempted_at"`
	Notified      bool       `json:"notified" db:"notified"`
}

// ActiveSession represents an active user session
type ActiveSession struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	UserID           uuid.UUID  `json:"user_id" db:"user_id"`
	IPAddress        string     `json:"ip_address" db:"ip_address"`
	UserAgent        string     `json:"user_agent" db:"user_agent"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	LastActiveAt     time.Time  `json:"last_active_at" db:"last_active_at"`
	TokenID          *uuid.UUID `json:"token_id,omitempty" db:"token_id"`      // Nullable for backwards compatibility
	SessionStartedAt time.Time  `json:"session_started_at" db:"session_started_at"` // Original session start time, persists across token refreshes
}

// MFAType represents the type of MFA enabled for a user
type MFAType string

const (
	MFATypeEmail         MFAType = "email"
	MFATypeAuthenticator MFAType = "authenticator"
	MFATypeBackup        MFAType = "backup"
	MFATypePasskey       MFAType = "passkey"
)

// ValidMFATypes returns all valid MFA types
func ValidMFATypes() []MFAType {
	return []MFAType{
		MFATypeEmail,
		MFATypeAuthenticator,
		MFATypeBackup,
		MFATypePasskey,
	}
}

// IsValidMFAType checks if a given string is a valid MFA type
func IsValidMFAType(t string) bool {
	for _, valid := range ValidMFATypes() {
		if string(valid) == t {
			return true
		}
	}
	return false
}

// IsValidPreferredMFAType checks if a given string is a valid preferred MFA type
// Note: backup codes cannot be set as preferred method
func IsValidPreferredMFAType(t string) bool {
	return t == string(MFATypeEmail) || t == string(MFATypeAuthenticator) || t == string(MFATypePasskey)
}

// MFASettings represents the MFA configuration
type MFASettings struct {
	RequireMFA               bool     `json:"requireMfa" db:"require_mfa"`
	AllowedMFAMethods        []string `json:"allowedMfaMethods" db:"allowed_mfa_methods"`
	EmailCodeValidityMinutes int      `json:"emailCodeValidity" db:"email_code_validity_minutes"`
	BackupCodesCount         int      `json:"backupCodesCount" db:"backup_codes_count"`
	MFACodeCooldownMinutes   int      `json:"mfaCodeCooldownMinutes" db:"mfa_code_cooldown_minutes"`
	MFACodeExpiryMinutes     int      `json:"mfaCodeExpiryMinutes" db:"mfa_code_expiry_minutes"`
	MFAMaxAttempts           int      `json:"mfaMaxAttempts" db:"mfa_max_attempts"`
}

// UserAuthInfo represents the authentication-related fields for a user
type UserAuthInfo struct {
	MFAEnabled          bool       `json:"mfa_enabled" db:"mfa_enabled"`
	MFAType             []string   `json:"mfa_type" db:"mfa_type"`
	MFASecret           string     `json:"-" db:"mfa_secret"`
	BackupCodes         []string   `json:"-" db:"backup_codes"`
	PreferredMFAMethod  string     `json:"preferred_mfa_method" db:"preferred_mfa_method"`
	LastPasswordChange  time.Time  `json:"last_password_change" db:"last_password_change"`
	FailedLoginAttempts int        `json:"failed_login_attempts" db:"failed_login_attempts"`
	LastFailedAttempt   *time.Time `json:"last_failed_attempt" db:"last_failed_attempt"`
	AccountLocked       bool       `json:"account_locked" db:"account_locked"`
	AccountLockedUntil  *time.Time `json:"account_locked_until" db:"account_locked_until"`
	AccountEnabled      bool       `json:"account_enabled" db:"account_enabled"`
	LastLogin           *time.Time `json:"last_login" db:"last_login"`
	DisabledReason      *string    `json:"disabled_reason" db:"disabled_reason"`
	DisabledAt          *time.Time `json:"disabled_at" db:"disabled_at"`
	DisabledBy          *uuid.UUID `json:"disabled_by" db:"disabled_by"`
}

// LoginResponse represents the response for a login attempt
type LoginResponse struct {
	Success            bool     `json:"success"`
	Message            string   `json:"message,omitempty"`
	Token              string   `json:"token,omitempty"`
	RequiresMFA        bool     `json:"requiresMfa,omitempty"`
	SessionToken       string   `json:"sessionToken,omitempty"`
	MFAType            []string `json:"mfaType,omitempty"`
	PreferredMFAMethod string   `json:"preferredMfaMethod,omitempty"`
	ExpiresAt          string   `json:"expiresAt,omitempty"`
}

// MFASession represents a temporary session during MFA verification
type MFASession struct {
	ID           string    `json:"id" db:"id"`
	UserID       string    `json:"user_id" db:"user_id"`
	SessionToken string    `json:"session_token" db:"session_token"`
	ExpiresAt    time.Time `json:"expires_at" db:"expires_at"`
	Attempts     int       `json:"attempts" db:"attempts"`
}

// MFAVerifyRequest represents an MFA verification request
type MFAVerifyRequest struct {
	SessionToken string `json:"sessionToken"`
	Code         string `json:"code"`
	Method       string `json:"method"`
}

// MFAVerifyResponse represents the response for an MFA verification attempt
type MFAVerifyResponse struct {
	Success           bool   `json:"success"`
	Message           string `json:"message,omitempty"`
	Token             string `json:"token,omitempty"`
	RemainingAttempts int    `json:"remainingAttempts,omitempty"`
}
