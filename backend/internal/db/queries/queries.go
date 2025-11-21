package queries

// Agent queries
const (
	CreateAgent = `
		INSERT INTO agents (
			name, status, last_heartbeat, version, hardware,
			os_info, created_by_id, created_at, updated_at, api_key,
			api_key_created_at, api_key_last_used, last_error, metadata, owner_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		) RETURNING id`

	GetAgentByID = `
		SELECT
			a.id, a.name, a.status, a.last_error, a.last_heartbeat,
			a.version, a.hardware, a.os_info, a.created_by_id, a.created_at,
			a.updated_at, a.api_key, a.api_key_created_at,
			a.api_key_last_used, a.metadata, a.owner_id, a.extra_parameters, a.is_enabled,
			a.consecutive_failures, a.scheduling_enabled, a.schedule_timezone,
			a.sync_status, a.sync_started_at, a.sync_completed_at, a.files_to_sync, a.files_synced, a.sync_error,
			a.binary_version_id, a.binary_override,
			u.id, u.username, u.email, u.role
		FROM agents a
		LEFT JOIN users u ON a.created_by_id = u.id
		WHERE a.id = $1`

	ListAgents = `
		SELECT
			a.id, a.name, a.status, a.last_error, a.last_heartbeat,
			a.version, a.hardware, a.os_info, a.created_by_id, a.created_at,
			a.updated_at, a.api_key, a.api_key_created_at,
			a.api_key_last_used, a.metadata, a.owner_id, a.extra_parameters, a.is_enabled,
			a.consecutive_failures, a.scheduling_enabled, a.schedule_timezone,
			a.sync_status, a.sync_started_at, a.sync_completed_at, a.files_to_sync, a.files_synced, a.sync_error,
			a.binary_version_id, a.binary_override,
			u.id, u.username, u.email, u.role
		FROM agents a
		LEFT JOIN users u ON a.created_by_id = u.id
		WHERE ($1::text IS NULL OR a.status = $1)
		ORDER BY a.created_at DESC`

	UpdateAgent = `
		UPDATE agents SET
			name = $2,
			status = $3,
			last_error = $4,
			last_heartbeat = $5,
			version = $6,
			hardware = $7,
			os_info = $8,
			updated_at = $9,
			api_key = $10,
			api_key_created_at = $11,
			api_key_last_used = $12,
			metadata = $13,
			sync_status = $14,
			sync_completed_at = $15,
			sync_started_at = $16,
			sync_error = $17,
			files_to_sync = $18,
			files_synced = $19
		WHERE id = $1`

	UpdateAgentStatus = `
		UPDATE agents SET
			status = $2,
			last_error = $3,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	UpdateAgentHeartbeat = `
		UPDATE agents SET
			last_heartbeat = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	UpdateAgentMetadata = `
		UPDATE agents SET
			metadata = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	GetAgentByAPIKey = `
		SELECT
			a.id, a.name, a.status, a.last_error, a.last_heartbeat,
			a.version, a.hardware, a.os_info, a.created_by_id, a.created_at,
			a.updated_at, a.api_key, a.api_key_created_at,
			a.api_key_last_used, a.metadata, a.owner_id, a.extra_parameters, a.is_enabled,
			a.consecutive_failures, a.scheduling_enabled, a.schedule_timezone,
			a.binary_version_id, a.binary_override,
			u.id, u.username, u.email, u.role
		FROM agents a
		LEFT JOIN users u ON a.created_by_id = u.id
		WHERE a.api_key = $1`
)

// User queries
const (
	CreateUser = `
		INSERT INTO users (
			id, username, email, password_hash, role,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		) RETURNING id`

	GetUserByID = `
		SELECT
			id, username, email, password_hash, role,
			created_at, updated_at, mfa_enabled, mfa_type,
			mfa_secret, backup_codes, preferred_mfa_method,
			last_password_change, failed_login_attempts,
			last_failed_attempt, account_locked,
			account_locked_until, account_enabled,
			last_login, disabled_reason, disabled_at,
			disabled_by, notify_on_job_completion,
			api_key, api_key_created_at, api_key_last_used
		FROM users
		WHERE id = $1`

	GetUserByEmail = `
		SELECT id, username, email, password_hash, role,
			created_at, updated_at
		FROM users
		WHERE email = $1`

	UpdateUser = `
		UPDATE users SET
			username = $2,
			email = $3,
			password_hash = $4,
			role = $5,
			updated_at = $6
		WHERE id = $1`

	DeleteUser = `DELETE FROM users WHERE id = $1`
)

// Team queries
const (
	CreateTeam = `
		INSERT INTO teams (
			id, name, description, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5
		) RETURNING id`

	GetTeamByID = `
		SELECT id, name, description, created_at, updated_at
		FROM teams
		WHERE id = $1`

	UpdateTeam = `
		UPDATE teams SET
			name = $2,
			description = $3,
			updated_at = $4
		WHERE id = $1`

	DeleteTeam = `DELETE FROM teams WHERE id = $1`

	AddUserToTeam = `
		INSERT INTO user_teams (user_id, team_id)
		VALUES ($1, $2)`

	RemoveUserFromTeam = `
		DELETE FROM user_teams
		WHERE user_id = $1 AND team_id = $2`

	AddAgentToTeam = `
		INSERT INTO agent_teams (agent_id, team_id)
		VALUES ($1, $2)`

	RemoveAgentFromTeam = `
		DELETE FROM agent_teams
		WHERE agent_id = $1 AND team_id = $2`
)

// ClaimVoucher queries
const (
	CreateClaimVoucher = `
		INSERT INTO claim_vouchers (
			code, is_active, is_continuous,
			created_by_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6
		) RETURNING code`

	GetClaimVoucherByCode = `
		SELECT 
			v.code, v.is_active, v.is_continuous,
			v.created_by_id, v.used_by_agent_id, v.used_at, v.created_at, v.updated_at,
			u1.id, u1.username, u1.email, u1.role,
			a.id, a.name, a.status
		FROM claim_vouchers v
		LEFT JOIN users u1 ON v.created_by_id = u1.id
		LEFT JOIN agents a ON v.used_by_agent_id = a.id
		WHERE v.code = $1`

	ListActiveVouchers = `
		SELECT 
			v.code, v.is_active, v.is_continuous,
			v.created_by_id, v.used_by_agent_id, v.used_at, v.created_at, v.updated_at,
			u1.id, u1.username, u1.email, u1.role,
			a.id, a.name, a.status
		FROM claim_vouchers v
		LEFT JOIN users u1 ON v.created_by_id = u1.id
		LEFT JOIN agents a ON v.used_by_agent_id = a.id
		WHERE v.is_active = true
		ORDER BY v.created_at DESC`

	UseClaimVoucherByAgent = `
		UPDATE claim_vouchers SET
			used_by_agent_id = $2,
			used_at = $3,
			updated_at = $3
		WHERE code = $1 AND is_active = true
		AND (is_continuous = true OR used_by_agent_id IS NULL)`

	DeactivateClaimVoucher = `
		UPDATE claim_vouchers SET
			is_active = false,
			updated_at = NOW()
		WHERE code = $1`
)

// Email Queries
const (
	// EmailConfigQueries handles email provider configuration
	EmailConfigExists = `
		SELECT EXISTS (SELECT 1 FROM email_config WHERE provider_type = $1)
	`

	EmailConfigUpdate = `
		UPDATE email_config
		SET api_key = $1,
			additional_config = $2,
			monthly_limit = $3,
			reset_date = $4,
			is_active = $5,
			updated_at = NOW()
		WHERE provider_type = $6
	`

	EmailConfigInsert = `
		INSERT INTO email_config (
			provider_type, api_key, additional_config, monthly_limit,
			reset_date, is_active
		) VALUES ($1, $2, $3, $4, $5, $6)
	`

	EmailConfigGet = `
		SELECT id, provider_type, api_key, additional_config, monthly_limit,
			   reset_date, is_active, created_at, updated_at
		FROM email_config
		WHERE is_active = true
		LIMIT 1
	`

	// EmailTemplateQueries handles email templates
	EmailTemplateInsert = `
		INSERT INTO email_templates (
			template_type, name, subject, html_content,
			text_content, last_modified_by
		) VALUES ($1, $2, $3, $4, $5, $6)
	`

	EmailTemplateUpdate = `
		UPDATE email_templates
		SET template_type = $1,
			name = $2,
			subject = $3,
			html_content = $4,
			text_content = $5,
			last_modified_by = $6,
			updated_at = NOW()
		WHERE id = $7
	`

	EmailTemplateGet = `
		SELECT id, template_type, name, subject, html_content,
			   text_content, created_at, updated_at, last_modified_by
		FROM email_templates
		WHERE id = $1
	`

	EmailTemplateList = `
		SELECT id, template_type, name, subject, html_content,
			   text_content, created_at, updated_at, last_modified_by
		FROM email_templates
		%s
		ORDER BY name
	`

	EmailTemplateDelete = `
		DELETE FROM email_templates WHERE id = $1
	`

	// EmailUsageQueries handles email usage tracking
	EmailUsageGetMonthlyLimit = `
		SELECT monthly_limit FROM email_config WHERE is_active = true
	`

	EmailUsageUpsert = `
		INSERT INTO email_usage (month_year, count, last_reset)
		VALUES ($1, 1, NOW())
		ON CONFLICT (month_year)
		DO UPDATE SET
			count = email_usage.count + 1,
			last_reset = CASE
				WHEN email_usage.last_reset < $1 THEN NOW()
				ELSE email_usage.last_reset
			END
	`

	EmailUsageGetCount = `
		SELECT count FROM email_usage WHERE month_year = $1
	`
)

// MFA queries
const (
	GetPendingMFASetup = `
		SELECT secret FROM pending_mfa_setup
		WHERE user_id = $1`

	ClearPendingMFASetup = `
		DELETE FROM pending_mfa_setup
		WHERE user_id = $1`

	GetMFAVerifyAttempts = `
		SELECT attempts FROM mfa_verify_attempts
		WHERE user_id = $1`

	IncrementMFAVerifyAttempts = `
		INSERT INTO mfa_verify_attempts (user_id, attempts)
		VALUES ($1, 1)
		ON CONFLICT (user_id) DO UPDATE 
		SET attempts = mfa_verify_attempts.attempts + 1
		RETURNING attempts`

	ClearMFAVerifyAttempts = `
		DELETE FROM mfa_verify_attempts
		WHERE user_id = $1`

	ValidateEmailMFACode = `
		SELECT EXISTS (
			SELECT 1 FROM email_mfa_codes
			WHERE user_id = $1 AND code = $2 AND expires_at > NOW()
		)`

	EnableMFA = `
		UPDATE users
		SET mfa_enabled = true,
			mfa_type = $2,
			mfa_secret = $3
		WHERE id = $1`
)

// ClearPendingMFASetupQuery removes a pending MFA setup for a user
const ClearPendingMFASetupQuery = `
DELETE FROM pending_mfa_setup 
WHERE user_id = $1`

// Bulk update users' MFA settings when global MFA is enabled
const BulkEnableMFAQuery = `
	UPDATE users 
	SET mfa_enabled = true,
		mfa_type = ARRAY['email']::text[],
		preferred_mfa_method = 'email',
		updated_at = NOW()
	WHERE mfa_enabled = false
		AND account_enabled = true
		AND account_locked = false;
`
