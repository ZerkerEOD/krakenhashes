package queries

// Auth settings related queries
const (
	GetAuthSettingsQuery = `
		SELECT min_password_length, require_uppercase, require_lowercase,
			require_numbers, require_special_chars, max_failed_attempts,
			lockout_duration_minutes, require_mfa, jwt_expiry_minutes,
			display_timezone, notification_aggregation_minutes
		FROM auth_settings
		LIMIT 1`

	UpdateAuthSettingsQuery = `
		UPDATE auth_settings
		SET min_password_length = $1,
			require_uppercase = $2,
			require_lowercase = $3,
			require_numbers = $4,
			require_special_chars = $5,
			max_failed_attempts = $6,
			lockout_duration_minutes = $7,
			require_mfa = $8,
			jwt_expiry_minutes = $9,
			display_timezone = $10,
			notification_aggregation_minutes = $11
		WHERE id = (SELECT id FROM auth_settings LIMIT 1)`

	// Additional MFA settings queries
	UpdateMFASettingsQuery = `
		UPDATE auth_settings
		SET require_mfa = $1,
			allowed_mfa_methods = $2::jsonb,
			email_code_validity_minutes = $3,
			backup_codes_count = $4,
			mfa_code_cooldown_minutes = $5,
			mfa_code_expiry_minutes = $6,
			mfa_max_attempts = $7
		WHERE id = (SELECT id FROM auth_settings LIMIT 1)`

	GetMFASettingsQuery = `
		SELECT require_mfa, allowed_mfa_methods::text,
			email_code_validity_minutes, backup_codes_count,
			mfa_code_cooldown_minutes, mfa_code_expiry_minutes,
			mfa_max_attempts
		FROM auth_settings
		LIMIT 1`

	// Get MFA code expiry minutes
	GetMFACodeExpiryMinutesQuery = `
		SELECT mfa_code_expiry_minutes FROM auth_settings LIMIT 1;
	`
)
