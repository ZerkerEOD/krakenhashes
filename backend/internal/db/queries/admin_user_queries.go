package queries

const (
	// ListAllUsers retrieves all users with their basic information
	ListAllUsers = `
		SELECT id, username, email, role,
			account_enabled, account_locked, account_locked_until,
			mfa_enabled, mfa_type, preferred_mfa_method,
			created_at, updated_at, last_login,
			disabled_reason, disabled_at, disabled_by,
			(SELECT provider_type FROM login_attempts la
			 WHERE la.user_id = users.id AND la.success = true
			 ORDER BY la.attempted_at DESC LIMIT 1) as last_auth_provider
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC`

	// GetUserDetails retrieves detailed user information for admin view
	GetUserDetails = `
		SELECT id, username, email, role, 
			account_enabled, account_locked, account_locked_until,
			mfa_enabled, mfa_type, preferred_mfa_method,
			created_at, updated_at, last_login, last_password_change,
			failed_login_attempts, last_failed_attempt,
			disabled_reason, disabled_at, disabled_by
		FROM users
		WHERE id = $1`

	// DisableUserAccount disables a user account with reason and admin ID
	DisableUserAccount = `
		UPDATE users
		SET account_enabled = false,
			disabled_reason = $2,
			disabled_at = CURRENT_TIMESTAMP,
			disabled_by = $3,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// EnableUserAccount re-enables a user account
	EnableUserAccount = `
		UPDATE users
		SET account_enabled = true,
			disabled_reason = NULL,
			disabled_at = NULL,
			disabled_by = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// AdminResetUserPassword updates a user's password (admin action)
	AdminResetUserPassword = `
		UPDATE users
		SET password_hash = $2,
			last_password_change = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// DisableUserMFA disables MFA for a user
	DisableUserMFA = `
		UPDATE users
		SET mfa_enabled = false,
			mfa_secret = NULL,
			backup_codes = NULL,
			preferred_mfa_method = 'email',
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// UpdateUserDetails updates username and/or email and/or role
	UpdateUserDetails = `
		UPDATE users
		SET username = COALESCE($2, username),
			email = COALESCE($3, email),
			role = COALESCE($4, role),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// UnlockUserAccount unlocks a locked account
	UnlockUserAccount = `
		UPDATE users
		SET account_locked = false,
			account_locked_until = NULL,
			failed_login_attempts = 0,
			last_failed_attempt = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	// CheckUsernameExists checks if a username already exists (excluding a specific user)
	CheckUsernameExists = `
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE username = $1 AND id != $2
		)`

	// CheckEmailExists checks if an email already exists (excluding a specific user)
	CheckEmailExists = `
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE email = $1 AND id != $2
		)`
)
