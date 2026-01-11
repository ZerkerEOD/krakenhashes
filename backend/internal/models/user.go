package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User represents a user in the system
type User struct {
	ID                  uuid.UUID  `json:"id" db:"id"`
	Username            string     `json:"username" db:"username"`
	FirstName           string     `json:"firstName,omitempty"`
	LastName            string     `json:"lastName,omitempty"`
	Email               string     `json:"email" db:"email"`
	PasswordHash        string     `json:"-" db:"password_hash"`
	Role                string     `json:"role" db:"role"`
	Teams               []Team     `json:"teams,omitempty"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
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
	DisabledReason         *string    `json:"disabled_reason" db:"disabled_reason"`
	DisabledAt             *time.Time `json:"disabled_at" db:"disabled_at"`
	DisabledBy             *uuid.UUID `json:"disabled_by" db:"disabled_by"`
	NotifyOnJobCompletion  bool       `json:"notify_on_job_completion" db:"notify_on_job_completion"`
	APIKey                 string     `json:"-" db:"api_key"`
	APIKeyCreatedAt        *time.Time `json:"api_key_created_at,omitempty" db:"api_key_created_at"`
	APIKeyLastUsed         *time.Time `json:"api_key_last_used,omitempty" db:"api_key_last_used"`
	// SSO auth overrides (NULL = use global setting, true/false = override)
	LocalAuthOverride  *bool   `json:"local_auth_override,omitempty" db:"local_auth_override"`
	SSOAuthOverride    *bool   `json:"sso_auth_override,omitempty" db:"sso_auth_override"`
	AuthOverrideNotes  *string `json:"auth_override_notes,omitempty" db:"auth_override_notes"`
}

// NotificationPreferences represents user notification settings
type NotificationPreferences struct {
	NotifyOnJobCompletion bool    `json:"notifyOnJobCompletion"`
	EmailConfigured       bool    `json:"emailConfigured"`
}

// UserMFAData represents sensitive MFA data for a user
type UserMFAData struct {
	ID                 string   `json:"id" db:"id"`
	MFAEnabled         bool     `json:"mfa_enabled" db:"mfa_enabled"`
	MFAType            []string `json:"mfa_type" db:"mfa_type"`
	MFASecret          string   `json:"mfa_secret" db:"mfa_secret"`
	BackupCodes        []string `json:"backup_codes" db:"backup_codes"`
	PreferredMFAMethod string   `json:"preferred_mfa_method" db:"preferred_mfa_method"`
}

// SetPassword hashes and sets the user's password
func (u *User) SetPassword(password string) error {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hashedBytes)
	return nil
}

// CheckPassword verifies if the provided password matches the user's hashed password
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}

// NewUser creates a new user with a generated UUID
func NewUser(username, email string) *User {
	return &User{
		ID:        uuid.New(),
		Username:  username,
		Email:     email,
		Role:      "user", // Default role
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// ScanTeams scans a JSON-encoded teams string into the Teams slice
func (u *User) ScanTeams(value interface{}) error {
	if value == nil {
		u.Teams = []Team{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, &u.Teams)
	case string:
		return json.Unmarshal([]byte(v), &u.Teams)
	default:
		return fmt.Errorf("unsupported type for teams: %T", value)
	}
}

// Value returns the JSON encoding of Teams for database storage
func (u User) TeamsValue() (driver.Value, error) {
	if len(u.Teams) == 0 {
		return nil, nil
	}
	return json.Marshal(u.Teams)
}

// ScanBackupCodes scans a backup codes array from the database
func (u *User) ScanBackupCodes(value interface{}) error {
	if value == nil {
		u.BackupCodes = []string{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		// Try parsing as JSON array first
		if err := json.Unmarshal(v, &u.BackupCodes); err == nil {
			return nil
		}
		// If JSON parsing fails, try parsing as Postgres array string
		str := string(v)
		if str[0] == '{' && str[len(str)-1] == '}' {
			// Remove the curly braces and split by comma
			str = str[1 : len(str)-1]
			if str == "" {
				u.BackupCodes = []string{}
				return nil
			}
			u.BackupCodes = strings.Split(str, ",")
			return nil
		}
		return fmt.Errorf("invalid backup codes format: %s", str)
	case string:
		// Try parsing as JSON array first
		if err := json.Unmarshal([]byte(v), &u.BackupCodes); err == nil {
			return nil
		}
		// If JSON parsing fails, try parsing as Postgres array string
		if v[0] == '{' && v[len(v)-1] == '}' {
			// Remove the curly braces and split by comma
			v = v[1 : len(v)-1]
			if v == "" {
				u.BackupCodes = []string{}
				return nil
			}
			u.BackupCodes = strings.Split(v, ",")
			return nil
		}
		return fmt.Errorf("invalid backup codes format: %s", v)
	case []string:
		u.BackupCodes = v
		return nil
	default:
		return fmt.Errorf("unsupported type for backup codes: %T", value)
	}
}

// BackupCodesValue returns the backup codes in a format suitable for database storage
func (u User) BackupCodesValue() (driver.Value, error) {
	if len(u.BackupCodes) == 0 {
		return nil, nil
	}
	return u.BackupCodes, nil
}

// ScanMFAType scans a text array from the database into MFAType
func (u *User) ScanMFAType(value interface{}) error {
	if value == nil {
		u.MFAType = []string{"email"} // Default to email
		return nil
	}

	switch v := value.(type) {
	case []byte:
		// Try parsing as JSON array first
		if err := json.Unmarshal(v, &u.MFAType); err == nil {
			return nil
		}
		// If JSON parsing fails, try parsing as Postgres array string
		str := string(v)
		if str[0] == '{' && str[len(str)-1] == '}' {
			// Remove the curly braces and split by comma
			str = str[1 : len(str)-1]
			if str == "" {
				u.MFAType = []string{"email"}
				return nil
			}
			u.MFAType = strings.Split(str, ",")
			return nil
		}
		return fmt.Errorf("invalid MFA type format: %s", str)
	case string:
		// Try parsing as JSON array first
		if err := json.Unmarshal([]byte(v), &u.MFAType); err == nil {
			return nil
		}
		// If JSON parsing fails, try parsing as Postgres array string
		if v[0] == '{' && v[len(v)-1] == '}' {
			// Remove the curly braces and split by comma
			v = v[1 : len(v)-1]
			if v == "" {
				u.MFAType = []string{"email"}
				return nil
			}
			u.MFAType = strings.Split(v, ",")
			return nil
		}
		return fmt.Errorf("invalid MFA type format: %s", v)
	case []string:
		u.MFAType = v
		return nil
	default:
		return fmt.Errorf("unsupported type for MFA type: %T", value)
	}
}

// MFATypeValue returns the MFA types in a format suitable for database storage
func (u User) MFATypeValue() (driver.Value, error) {
	if len(u.MFAType) == 0 {
		return []string{"email"}, nil // Default to email
	}
	return u.MFAType, nil
}

// HasMFAMethod checks if a specific MFA method is enabled for the user
func (u *User) HasMFAMethod(method string) bool {
	for _, m := range u.MFAType {
		if m == method {
			return true
		}
	}
	return false
}

// AddMFAMethod adds a new MFA method if it doesn't exist
func (u *User) AddMFAMethod(method string) {
	if !u.HasMFAMethod(method) {
		u.MFAType = append(u.MFAType, method)
	}
}

// RemoveMFAMethod removes an MFA method if it exists
// Note: Cannot remove 'email' as it's required
func (u *User) RemoveMFAMethod(method string) error {
	if method == "email" {
		return fmt.Errorf("cannot remove email MFA method as it is required")
	}

	newTypes := make([]string, 0)
	for _, m := range u.MFAType {
		if m != method {
			newTypes = append(newTypes, m)
		}
	}
	u.MFAType = newTypes

	// If removed method was preferred, fall back to email
	if u.PreferredMFAMethod == method {
		u.PreferredMFAMethod = "email"
	}

	return nil
}

// CanUseLocalAuth checks if user can use local (password) authentication
// Takes the global setting and returns whether local auth is allowed for this user
func (u *User) CanUseLocalAuth(globalEnabled bool) bool {
	if u.LocalAuthOverride != nil {
		return *u.LocalAuthOverride
	}
	return globalEnabled
}

// CanUseSSOAuth checks if user can use SSO authentication
// Takes the global setting and returns whether SSO auth is allowed for this user
func (u *User) CanUseSSOAuth(globalEnabled bool) bool {
	if u.SSOAuthOverride != nil {
		return *u.SSOAuthOverride
	}
	return globalEnabled
}

// HasPasswordSet returns true if the user has a password hash set
// SSO-only users may not have a password
func (u *User) HasPasswordSet() bool {
	return u.PasswordHash != "" && u.PasswordHash != "SSO_USER_NO_PASSWORD"
}
