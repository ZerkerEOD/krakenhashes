package db

import (
	"encoding/json"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// GetAuthSettings retrieves the current authentication settings
func (db *DB) GetAuthSettings() (*models.AuthSettings, error) {
	var settings models.AuthSettings
	err := db.QueryRow(queries.GetAuthSettingsQuery).Scan(
		&settings.MinPasswordLength,
		&settings.RequireUppercase,
		&settings.RequireLowercase,
		&settings.RequireNumbers,
		&settings.RequireSpecialChars,
		&settings.MaxFailedAttempts,
		&settings.LockoutDurationMinutes,
		&settings.RequireMFA,
		&settings.JWTExpiryMinutes,
		&settings.DisplayTimezone,
		&settings.NotificationAggregationMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth settings: %w", err)
	}
	return &settings, nil
}

// UpdateAuthSettings updates the authentication settings
func (db *DB) UpdateAuthSettings(settings *models.AuthSettings) error {
	_, err := db.Exec(queries.UpdateAuthSettingsQuery,
		settings.MinPasswordLength,
		settings.RequireUppercase,
		settings.RequireLowercase,
		settings.RequireNumbers,
		settings.RequireSpecialChars,
		settings.MaxFailedAttempts,
		settings.LockoutDurationMinutes,
		settings.RequireMFA,
		settings.JWTExpiryMinutes,
		settings.DisplayTimezone,
		settings.NotificationAggregationMinutes,
	)
	if err != nil {
		return fmt.Errorf("failed to update auth settings: %w", err)
	}
	return nil
}

// GetMFASettings retrieves the current MFA settings
func (db *DB) GetMFASettings() (*models.MFASettings, error) {
	var settings models.MFASettings
	var methodsJSON string

	err := db.QueryRow(queries.GetMFASettingsQuery).Scan(
		&settings.RequireMFA,
		&methodsJSON,
		&settings.EmailCodeValidityMinutes,
		&settings.BackupCodesCount,
		&settings.MFACodeCooldownMinutes,
		&settings.MFACodeExpiryMinutes,
		&settings.MFAMaxAttempts,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get MFA settings: %w", err)
	}

	// Parse the JSON array of allowed methods
	if err := json.Unmarshal([]byte(methodsJSON), &settings.AllowedMFAMethods); err != nil {
		return nil, fmt.Errorf("failed to unmarshal allowed methods: %w", err)
	}

	return &settings, nil
}

// UpdateMFASettings updates the MFA settings in the database
func (db *DB) UpdateMFASettings(requireMFA bool, allowedMethods []string, emailValidity, backupCodes, cooldown, expiry, maxAttempts int) error {
	debug.Info("Updating MFA settings: require=%v, methods=%v, emailValidity=%d, backupCodes=%d, cooldown=%d, expiry=%d, maxAttempts=%d",
		requireMFA, allowedMethods, emailValidity, backupCodes, cooldown, expiry, maxAttempts)

	methodsJSON, err := json.Marshal(allowedMethods)
	if err != nil {
		return fmt.Errorf("failed to marshal allowed methods: %w", err)
	}

	_, err = db.Exec(queries.UpdateMFASettingsQuery,
		requireMFA,
		methodsJSON,
		emailValidity,
		backupCodes,
		cooldown,
		expiry,
		maxAttempts,
	)
	if err != nil {
		return fmt.Errorf("failed to update MFA settings: %w", err)
	}

	return nil
}

// BulkEnableMFA enables MFA for all active users who don't have it enabled yet
func (db *DB) BulkEnableMFA() error {
	debug.Info("Bulk enabling MFA for all active users")
	result, err := db.Exec(queries.BulkEnableMFAQuery)
	if err != nil {
		debug.Error("Failed to bulk enable MFA: %v", err)
		return fmt.Errorf("failed to bulk enable MFA: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		debug.Error("Failed to get affected rows count: %v", err)
		return nil // Not returning error as the update was successful
	}
	debug.Info("Enabled MFA for %d users", affected)
	return nil
}

// IsLocalAuthEnabled checks if local (password) authentication is globally enabled
func (db *DB) IsLocalAuthEnabled() (bool, error) {
	var enabled bool
	err := db.QueryRow("SELECT local_auth_enabled FROM auth_settings LIMIT 1").Scan(&enabled)
	if err != nil {
		return true, fmt.Errorf("failed to check local auth status: %w", err)
	}
	return enabled, nil
}
