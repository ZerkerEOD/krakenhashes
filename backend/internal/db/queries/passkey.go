package queries

// Passkey-related queries
const (
	// Create a new passkey credential
	CreatePasskeyQuery = `
		INSERT INTO user_passkeys (user_id, credential_id, public_key, aaguid, sign_count, transports, name, backup_eligible, backup_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at`

	// Get all passkeys for a user
	GetUserPasskeysQuery = `
		SELECT id, user_id, credential_id, public_key, aaguid, sign_count, transports, name, backup_eligible, backup_state, created_at, last_used_at
		FROM user_passkeys
		WHERE user_id = $1
		ORDER BY created_at DESC`

	// Get a passkey by its credential ID
	GetPasskeyByCredentialIDQuery = `
		SELECT id, user_id, credential_id, public_key, aaguid, sign_count, transports, name, backup_eligible, backup_state, created_at, last_used_at
		FROM user_passkeys
		WHERE credential_id = $1`

	// Get a passkey by ID and user ID (for ownership verification)
	GetPasskeyByIDAndUserQuery = `
		SELECT id, user_id, credential_id, public_key, aaguid, sign_count, transports, name, backup_eligible, backup_state, created_at, last_used_at
		FROM user_passkeys
		WHERE id = $1 AND user_id = $2`

	// Update passkey sign count after successful authentication
	UpdatePasskeySignCountQuery = `
		UPDATE user_passkeys
		SET sign_count = $2, last_used_at = NOW()
		WHERE id = $1`

	// Delete a passkey
	DeletePasskeyQuery = `
		DELETE FROM user_passkeys
		WHERE id = $1 AND user_id = $2`

	// Rename a passkey
	RenamePasskeyQuery = `
		UPDATE user_passkeys
		SET name = $3
		WHERE id = $1 AND user_id = $2`

	// Get passkey count for a user
	GetPasskeyCountQuery = `
		SELECT COUNT(*) FROM user_passkeys WHERE user_id = $1`

	// Store pending passkey registration challenge
	StorePendingPasskeyRegistrationQuery = `
		INSERT INTO pending_passkey_registration (user_id, challenge, session_data)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id)
		DO UPDATE SET challenge = $2, session_data = $3, created_at = NOW()`

	// Get pending passkey registration challenge
	GetPendingPasskeyRegistrationQuery = `
		SELECT challenge, session_data
		FROM pending_passkey_registration
		WHERE user_id = $1 AND created_at > NOW() - INTERVAL '5 minutes'`

	// Delete pending passkey registration
	DeletePendingPasskeyRegistrationQuery = `
		DELETE FROM pending_passkey_registration
		WHERE user_id = $1`

	// Store pending passkey authentication challenge
	StorePendingPasskeyAuthenticationQuery = `
		INSERT INTO pending_passkey_authentication (session_token, user_id, challenge, session_data)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (session_token)
		DO UPDATE SET user_id = $2, challenge = $3, session_data = $4, created_at = NOW()`

	// Get pending passkey authentication challenge
	GetPendingPasskeyAuthenticationQuery = `
		SELECT user_id, challenge, session_data
		FROM pending_passkey_authentication
		WHERE session_token = $1 AND created_at > NOW() - INTERVAL '5 minutes'`

	// Delete pending passkey authentication
	DeletePendingPasskeyAuthenticationQuery = `
		DELETE FROM pending_passkey_authentication
		WHERE session_token = $1`

	// Get WebAuthn settings from auth_settings
	GetWebAuthnSettingsQuery = `
		SELECT
			COALESCE(webauthn_rp_id, '') as webauthn_rp_id,
			COALESCE(webauthn_rp_origins, ARRAY[]::text[]) as webauthn_rp_origins,
			COALESCE(webauthn_rp_display_name, 'KrakenHashes') as webauthn_rp_display_name
		FROM auth_settings
		LIMIT 1`

	// Update WebAuthn settings
	UpdateWebAuthnSettingsQuery = `
		UPDATE auth_settings
		SET webauthn_rp_id = $1,
			webauthn_rp_origins = $2,
			webauthn_rp_display_name = $3`

	// Cleanup expired passkey challenges
	CleanupExpiredPasskeyChallengesQuery = `
		DELETE FROM pending_passkey_registration WHERE created_at < NOW() - INTERVAL '5 minutes';
		DELETE FROM pending_passkey_authentication WHERE created_at < NOW() - INTERVAL '5 minutes'`

	// Check if user has any passkeys
	UserHasPasskeysQuery = `
		SELECT EXISTS(SELECT 1 FROM user_passkeys WHERE user_id = $1)`

	// Get all credential IDs for a user (for WebAuthn excludeCredentials)
	GetUserCredentialIDsQuery = `
		SELECT credential_id, transports
		FROM user_passkeys
		WHERE user_id = $1`
)
