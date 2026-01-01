package queries

const (
	// Existing Auth Queries
	GetUserByUsername = `
		SELECT id, username, email, password_hash, role,
			created_at, updated_at,
			account_enabled, account_locked, account_locked_until,
			failed_login_attempts, last_failed_attempt
		FROM users
		WHERE username = $1`

	StoreToken = `
		INSERT INTO tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)`

	RemoveToken = `
		DELETE FROM tokens
		WHERE token = $1`

	TokenExists = `
		SELECT EXISTS(
			SELECT 1 FROM tokens
			WHERE token = $1
			AND revoked = false
			AND (superseded_at IS NULL OR superseded_at > CURRENT_TIMESTAMP - INTERVAL '5 minutes')
		)`

	// Check if token is eligible for refresh (past 1/3 of session time)
	IsTokenRefreshable = `
		SELECT EXISTS(
			SELECT 1 FROM tokens t
			JOIN auth_settings a ON true
			WHERE t.token = $1
			AND t.superseded_at IS NULL
			AND t.revoked = false
			AND t.created_at < CURRENT_TIMESTAMP - INTERVAL '1 minute' * (a.jwt_expiry_minutes / 3)
		)`

	// Mark old token as superseded (for grace period)
	SupersedeToken = `
		UPDATE tokens
		SET superseded_at = CURRENT_TIMESTAMP,
			superseded_by = $2
		WHERE token = $1
		AND superseded_at IS NULL`

	// Get token details for swap operation
	GetTokenDetails = `
		SELECT id, user_id, created_at, expires_at
		FROM tokens
		WHERE token = $1
		AND revoked = false`

	// Update last activity for a token (for non-auto-refresh requests)
	UpdateTokenActivity = `
		UPDATE tokens
		SET last_activity = CURRENT_TIMESTAMP
		WHERE token = $1`

	// Check if token is expired based on idle timeout
	IsTokenExpiredByIdleTimeout = `
		SELECT EXISTS(
			SELECT 1 FROM tokens t
			JOIN auth_settings a
			ON true
			WHERE t.token = $1
			AND t.last_activity < CURRENT_TIMESTAMP - INTERVAL '1 minute' * a.jwt_expiry_minutes
		)`

	// User MFA Data Query
	GetUserWithMFAData = `
		SELECT mfa_enabled, mfa_type, mfa_secret, backup_codes,
			last_password_change, failed_login_attempts,
			last_failed_attempt, account_locked, account_locked_until,
			account_enabled, last_login, disabled_reason,
			disabled_at, disabled_by
		FROM users
		WHERE id = $1`

	// Auth Settings Queries
	GetAuthSettings = `
		SELECT id, min_password_length, require_uppercase, require_lowercase,
			require_numbers, require_special_chars, max_failed_attempts,
			lockout_duration_minutes, require_mfa, jwt_expiry_minutes,
			display_timezone, notification_aggregation_minutes
		FROM auth_settings LIMIT 1
	`

	// Login Attempts Queries
	CreateLoginAttempt = `
		INSERT INTO login_attempts (
			user_id, username, ip_address, user_agent,
			success, failure_reason
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING *
	`

	GetUserLoginAttempts = `
		SELECT * FROM login_attempts
		WHERE user_id = $1
		ORDER BY attempted_at DESC
		LIMIT $2
	`

	GetUnnotifiedFailedAttempts = `
		SELECT * FROM login_attempts
		WHERE success = false
		AND notified = false
		AND attempted_at >= $1
		ORDER BY attempted_at DESC
	`

	MarkAttemptsAsNotified = `
		UPDATE login_attempts
		SET notified = true
		WHERE id = ANY($1)
	`

	// Active Sessions Queries
	CreateSession = `
		INSERT INTO active_sessions (
			user_id, ip_address, user_agent
		) VALUES ($1, $2, $3)
		RETURNING *
	`

	UpdateSessionActivity = `
		UPDATE active_sessions
		SET last_active_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING *
	`

	DeleteSession = `
		DELETE FROM active_sessions
		WHERE id = $1
	`

	DeleteUserSessions = `
		DELETE FROM active_sessions
		WHERE user_id = $1
	`

	GetUserSessions = `
		SELECT * FROM active_sessions
		WHERE user_id = $1
		ORDER BY last_active_at DESC
	`

	// User Auth Info Queries
	UpdateUserAuthInfo = `
		UPDATE users
		SET mfa_enabled = $1,
			mfa_type = $2,
			mfa_secret = $3,
			backup_codes = $4,
			last_password_change = CURRENT_TIMESTAMP
		WHERE id = $5
		RETURNING *
	`

	IncrementFailedAttempts = `
		UPDATE users
		SET failed_login_attempts = failed_login_attempts + 1,
			last_failed_attempt = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING failed_login_attempts
	`

	ResetFailedAttempts = `
		UPDATE users
		SET failed_login_attempts = 0,
			last_failed_attempt = NULL,
			account_locked = false,
			account_locked_until = NULL
		WHERE id = $1
	`

	LockUserAccount = `
		UPDATE users
		SET account_locked = true,
			account_locked_until = CURRENT_TIMESTAMP + ($1 || ' minutes')::interval
		WHERE id = $2
	`

	// Commented out - duplicate in admin_user_queries.go
	// DisableUserAccount = `
	// 	UPDATE users
	// 	SET account_enabled = false,
	// 		disabled_reason = $1,
	// 		disabled_at = CURRENT_TIMESTAMP,
	// 		disabled_by = $2
	// 	WHERE id = $3
	// `

	// EnableUserAccount = `
	// 	UPDATE users
	// 	SET account_enabled = true,
	// 		disabled_reason = NULL,
	// 		disabled_at = NULL,
	// 		disabled_by = NULL
	// 	WHERE id = $1
	// `

	UpdateLastLogin = `
		UPDATE users
		SET last_login = CURRENT_TIMESTAMP
		WHERE id = $1
	`
)
