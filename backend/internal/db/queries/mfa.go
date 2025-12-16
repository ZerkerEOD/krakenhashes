package queries

// MFA-related queries
const (
	// Check if MFA is required by policy
	IsMFARequiredQuery = `
		SELECT require_mfa FROM auth_settings LIMIT 1;
	`

	// Enable MFA for a user with a specific method
	EnableMFAQuery = `
		UPDATE users
		SET mfa_enabled = true,
			mfa_type = CASE
				WHEN $2 = ANY(mfa_type) THEN mfa_type
				ELSE array_append(mfa_type, $2)
			END,
			mfa_secret = $3,
			preferred_mfa_method = $2,
			updated_at = NOW()
		WHERE id = $1;
	`

	// Disable MFA for a user
	DisableMFAQuery = `
		UPDATE users
		SET mfa_enabled = false,
			mfa_type = ARRAY['email']::text[],
			mfa_secret = NULL,
			backup_codes = NULL,
			preferred_mfa_method = 'email',
			updated_at = NOW()
		WHERE id = $1;
	`

	// Store pending MFA setup
	StorePendingMFASetupQuery = `
		INSERT INTO pending_mfa_setup (user_id, method, secret)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id)
		DO UPDATE SET method = $2, secret = $3, created_at = NOW();
	`

	// Get pending MFA setup
	GetPendingMFASetupQuery = `
		SELECT secret
		FROM pending_mfa_setup
		WHERE user_id = $1 AND created_at > NOW() - INTERVAL '5 minutes';
	`

	// Store MFA secret
	StoreMFASecretQuery = `
		UPDATE users
		SET mfa_secret = $2, updated_at = NOW()
		WHERE id = $1;
	`

	// Store backup codes
	StoreBackupCodesQuery = `
		UPDATE users
		SET backup_codes = $2,
			mfa_type = CASE
				WHEN NOT 'backup' = ANY(mfa_type) AND array_length($2::text[], 1) > 0
				THEN array_append(mfa_type, 'backup')
				WHEN array_length($2::text[], 1) = 0
				THEN array_remove(mfa_type, 'backup')
				ELSE mfa_type
			END,
			updated_at = NOW()
		WHERE id = $1;
	`

	// Get user's MFA settings
	GetUserMFASettingsQuery = `
		SELECT
			mfa_enabled,
			mfa_type,
			preferred_mfa_method,
			mfa_secret,
			backup_codes
		FROM users
		WHERE id = $1;
	`

	// Delete expired pending MFA setups
	CleanupPendingMFASetupQuery = `
		DELETE FROM pending_mfa_setup
		WHERE created_at < NOW() - INTERVAL '5 minutes';
	`

	// Store email MFA code
	StoreEmailMFACodeQuery = `
		WITH settings AS (
			SELECT mfa_code_expiry_minutes FROM auth_settings LIMIT 1
		)
		INSERT INTO email_mfa_codes (user_id, code, expires_at)
		VALUES (
			$1,
			$2,
			NOW() + ((SELECT mfa_code_expiry_minutes FROM settings) || ' minutes')::INTERVAL
		)
		ON CONFLICT (user_id)
		DO UPDATE SET
			code = $2,
			expires_at = NOW() + ((SELECT mfa_code_expiry_minutes FROM settings) || ' minutes')::INTERVAL,
			attempts = 0;
	`

	// Verify email MFA code
	VerifyEmailMFACodeQuery = `
		WITH updated AS (
			UPDATE email_mfa_codes
			SET attempts = attempts + 1
			WHERE user_id = $1
				AND code = $2
				AND expires_at > NOW()
				AND attempts < (SELECT mfa_max_attempts FROM auth_settings)
			RETURNING true AS success
		)
		SELECT COALESCE((SELECT success FROM updated), false);
	`

	// Delete used/expired email MFA codes
	CleanupEmailMFACodesQuery = `
		DELETE FROM email_mfa_codes
		WHERE expires_at < NOW()
			OR attempts >= (SELECT mfa_max_attempts FROM auth_settings);
	`

	// Check cooldown period for email MFA codes
	CheckEmailMFACooldownQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM email_mfa_codes
			WHERE user_id = $1
				AND created_at > NOW() - ((SELECT mfa_code_cooldown_minutes FROM auth_settings LIMIT 1) || ' minutes')::INTERVAL
		);
	`

	GetMFAVerifyAttemptsQuery = `
		SELECT attempts
		FROM mfa_sessions
		WHERE session_token = $1
		AND expires_at > NOW()`

	IncrementMFAVerifyAttemptsQuery = `
		UPDATE mfa_sessions
		SET attempts = attempts + 1
		WHERE session_token = $1
		AND expires_at > NOW()
		RETURNING attempts`

	ClearMFAVerifyAttemptsQuery = `
		UPDATE mfa_sessions
		SET attempts = 0
		WHERE session_token = $1
		AND expires_at > NOW()`

	// Backup codes queries
	GetUnusedBackupCodesCountQuery = `
		SELECT array_length(backup_codes, 1)
		FROM users
		WHERE id = $1`

	ClearBackupCodesQuery = `
		UPDATE users
		SET backup_codes = ARRAY[]::text[],
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// Set preferred MFA method
	SetPreferredMFAMethodQuery = `
		UPDATE users
		SET preferred_mfa_method = $2,
			updated_at = NOW()
		WHERE id = $1
		AND $2 = ANY(mfa_type)  -- Ensure method exists in mfa_type
		AND $2 != 'backup';     -- Prevent setting backup as preferred
	`

	// Get count of remaining backup codes
	GetRemainingBackupCodesCountQuery = `
		SELECT COALESCE(ARRAY_LENGTH(backup_codes, 1), 0)
		FROM users
		WHERE id = $1;
	`

	// Validate and consume a backup code
	ValidateAndConsumeBackupCodeQuery = `
		UPDATE users
		SET backup_codes = array_remove(backup_codes, $2),
			updated_at = NOW()
		WHERE id = $1
		AND backup_codes IS NOT NULL
		AND array_length(backup_codes, 1) > 0
		AND $2 = ANY(backup_codes)
		RETURNING id;
	`

	// Store new backup codes
	StoreNewBackupCodesQuery = `
		UPDATE users
		SET backup_codes = $2,
			updated_at = NOW()
		WHERE id = $1;
	`

	// MFA Session queries
	// Create MFA session
	CreateMFASessionQuery = `
		INSERT INTO mfa_sessions (user_id, session_token, expires_at, attempts)
		VALUES ($1, $2, NOW() + INTERVAL '5 minutes', 0)
		RETURNING id, expires_at`

	// Get MFA session
	GetMFASessionQuery = `
		SELECT user_id, attempts, expires_at
		FROM mfa_sessions
		WHERE session_token = $1 AND expires_at > NOW();
	`

	// Increment MFA session attempts
	IncrementMFASessionAttemptsQuery = `
		UPDATE mfa_sessions
		SET attempts = attempts + 1
		WHERE session_token = $1 AND expires_at > NOW()
		RETURNING attempts;
	`

	// Delete MFA session
	DeleteMFASessionQuery = `
		DELETE FROM mfa_sessions
		WHERE session_token = $1;
	`

	// Delete expired MFA sessions
	DeleteExpiredMFASessionsQuery = `
		DELETE FROM mfa_sessions
		WHERE expires_at < NOW();
	`

	// Check if user requires MFA
	CheckMFARequiredQuery = `
		SELECT
			CASE
				WHEN (SELECT require_mfa FROM auth_settings LIMIT 1) THEN true
				WHEN u.mfa_enabled THEN true
				ELSE false
			END as requires_mfa,
			u.mfa_type,
			u.preferred_mfa_method
		FROM users u
		WHERE u.id = $1;
	`

	// Remove specific MFA method
	RemoveMFAMethodQuery = `
		UPDATE users
		SET mfa_type = array_remove(mfa_type, $2),
			preferred_mfa_method = CASE
				WHEN preferred_mfa_method = $2 THEN 'email'
				ELSE preferred_mfa_method
			END,
			updated_at = NOW()
		WHERE id = $1
		AND $2 != 'email';  -- Prevent removal of email method
	`

	// Add MFA method
	AddMFAMethodQuery = `
		UPDATE users
		SET mfa_type = array_append(CASE WHEN NOT $2 = ANY(mfa_type) THEN mfa_type ELSE mfa_type END, $2),
			updated_at = NOW()
		WHERE id = $1;
	`

	// Alias for backward compatibility
	ValidateAndUseBackupCodeQuery = ValidateAndConsumeBackupCodeQuery
)
