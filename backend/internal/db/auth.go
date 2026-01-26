package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// GetUserByUsername retrieves a user by their username
func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	user := &models.User{}
	var accountLockedUntil, lastFailedAttempt sql.NullTime
	var localAuthOverride, ssoAuthOverride sql.NullBool
	var authOverrideNotes sql.NullString

	err := db.QueryRow(queries.GetUserByUsername, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.AccountEnabled,
		&user.AccountLocked,
		&accountLockedUntil,
		&user.FailedLoginAttempts,
		&lastFailedAttempt,
		&localAuthOverride,
		&ssoAuthOverride,
		&authOverrideNotes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		debug.Error("Failed to get user by username: %v", err)
		return nil, err
	}

	// Handle nullable time fields
	if accountLockedUntil.Valid {
		user.AccountLockedUntil = &accountLockedUntil.Time
	}
	if lastFailedAttempt.Valid {
		user.LastFailedAttempt = &lastFailedAttempt.Time
	}

	// Handle nullable SSO override fields
	if localAuthOverride.Valid {
		user.LocalAuthOverride = &localAuthOverride.Bool
	}
	if ssoAuthOverride.Valid {
		user.SSOAuthOverride = &ssoAuthOverride.Bool
	}
	if authOverrideNotes.Valid {
		user.AuthOverrideNotes = &authOverrideNotes.String
	}

	return user, nil
}

// StoreToken stores a JWT token for a user and returns the token ID
func (db *DB) StoreToken(userID, token string) (uuid.UUID, error) {
	// Get JWT expiry from auth settings
	authSettings, err := db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings for token storage: %v", err)
		return uuid.Nil, err
	}

	// Calculate expiration time based on JWT expiry setting
	expiresAt := time.Now().Add(time.Duration(authSettings.JWTExpiryMinutes) * time.Minute)

	var tokenID uuid.UUID
	err = db.QueryRow(`
		INSERT INTO tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
	`, userID, token, expiresAt).Scan(&tokenID)

	if err != nil {
		debug.Error("Failed to store token: %v", err)
		return uuid.Nil, err
	}
	return tokenID, nil
}

// RemoveToken removes a JWT token from storage by token string
func (db *DB) RemoveToken(tokenID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM tokens WHERE id = $1`, tokenID)
	if err != nil {
		debug.Error("Failed to remove token: %v", err)
		return err
	}
	return nil
}

// RemoveTokenByString removes a JWT token from storage by token string
func (db *DB) RemoveTokenByString(token string) error {
	_, err := db.Exec(queries.RemoveToken, token)
	if err != nil {
		debug.Error("Failed to remove token: %v", err)
		return err
	}
	return nil
}

// TokenExists checks if a JWT token exists in storage
func (db *DB) TokenExists(token string) (bool, error) {
	var exists bool
	err := db.QueryRow(queries.TokenExists, token).Scan(&exists)
	if err != nil {
		debug.Error("Failed to check token existence: %v", err)
		return false, err
	}
	return exists, nil
}

// UpdateTokenActivity updates the last_activity timestamp for a token
func (db *DB) UpdateTokenActivity(token string) error {
	_, err := db.Exec(queries.UpdateTokenActivity, token)
	if err != nil {
		debug.Error("Failed to update token activity: %v", err)
		return err
	}
	return nil
}

// IsTokenExpiredByIdleTimeout checks if a token has exceeded the idle timeout
func (db *DB) IsTokenExpiredByIdleTimeout(token string) (bool, error) {
	var expired bool
	err := db.QueryRow(queries.IsTokenExpiredByIdleTimeout, token).Scan(&expired)
	if err != nil {
		debug.Error("Failed to check token idle timeout: %v", err)
		return false, err
	}
	return expired, nil
}

// CreateLoginAttempt records a login attempt
func (db *DB) CreateLoginAttempt(attempt *models.LoginAttempt) error {
	_, err := db.Exec(queries.CreateLoginAttempt,
		attempt.UserID,
		attempt.Username,
		attempt.IPAddress,
		attempt.UserAgent,
		attempt.Success,
		attempt.FailureReason)
	return err
}

// GetUserLoginAttempts retrieves login attempts for a user
func (db *DB) GetUserLoginAttempts(userID uuid.UUID, limit int) ([]*models.LoginAttempt, error) {
	rows, err := db.Query(queries.GetUserLoginAttempts, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []*models.LoginAttempt
	for rows.Next() {
		attempt := &models.LoginAttempt{}
		var username, failureReason, providerType sql.NullString
		err := rows.Scan(
			&attempt.ID,
			&attempt.UserID,
			&username,
			&attempt.IPAddress,
			&attempt.UserAgent,
			&attempt.Success,
			&failureReason,
			&attempt.AttemptedAt,
			&attempt.Notified,
			&attempt.ProviderID,
			&providerType,
		)
		if err != nil {
			return nil, err
		}
		if username.Valid {
			attempt.Username = username.String
		}
		if failureReason.Valid {
			attempt.FailureReason = failureReason.String
		}
		if providerType.Valid {
			attempt.ProviderType = providerType.String
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// GetUnnotifiedFailedAttempts retrieves unnotified failed login attempts
func (db *DB) GetUnnotifiedFailedAttempts(since string) ([]*models.LoginAttempt, error) {
	rows, err := db.Query(queries.GetUnnotifiedFailedAttempts, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []*models.LoginAttempt
	for rows.Next() {
		attempt := &models.LoginAttempt{}
		var username, failureReason, providerType sql.NullString
		err := rows.Scan(
			&attempt.ID,
			&attempt.UserID,
			&username,
			&attempt.IPAddress,
			&attempt.UserAgent,
			&attempt.Success,
			&failureReason,
			&attempt.AttemptedAt,
			&attempt.Notified,
			&attempt.ProviderID,
			&providerType,
		)
		if err != nil {
			return nil, err
		}
		if username.Valid {
			attempt.Username = username.String
		}
		if failureReason.Valid {
			attempt.FailureReason = failureReason.String
		}
		if providerType.Valid {
			attempt.ProviderType = providerType.String
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// MarkAttemptsAsNotified marks login attempts as notified
func (db *DB) MarkAttemptsAsNotified(ids []uuid.UUID) error {
	_, err := db.Exec(queries.MarkAttemptsAsNotified, ids)
	return err
}

// CreateSession creates a new active session linked to a token
func (db *DB) CreateSession(session *models.ActiveSession) error {
	// If SessionStartedAt is not set, use current time
	if session.SessionStartedAt.IsZero() {
		session.SessionStartedAt = time.Now()
	}

	_, err := db.Exec(`
		INSERT INTO active_sessions (user_id, ip_address, user_agent, token_id, session_started_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.UserID, session.IPAddress, session.UserAgent, session.TokenID, session.SessionStartedAt)
	return err
}

// UpdateSessionActivity updates the last active time for a session
func (db *DB) UpdateSessionActivity(sessionID uuid.UUID) error {
	_, err := db.Exec(queries.UpdateSessionActivity, sessionID)
	return err
}

// DeleteSession deletes a specific session
func (db *DB) DeleteSession(sessionID uuid.UUID) error {
	_, err := db.Exec(queries.DeleteSession, sessionID)
	return err
}

// DeleteUserSessions deletes all sessions for a user
func (db *DB) DeleteUserSessions(userID uuid.UUID) error {
	_, err := db.Exec(queries.DeleteUserSessions, userID)
	return err
}

// GetUserSessions retrieves all active sessions for a user
func (db *DB) GetUserSessions(userID uuid.UUID) ([]*models.ActiveSession, error) {
	rows, err := db.Query(queries.GetUserSessions, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*models.ActiveSession
	for rows.Next() {
		session := &models.ActiveSession{}
		err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.IPAddress,
			&session.UserAgent,
			&session.CreatedAt,
			&session.LastActiveAt,
			&session.TokenID,
			&session.SessionStartedAt,
		)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// UpdateUserAuthInfo updates a user's authentication information
func (db *DB) UpdateUserAuthInfo(userID uuid.UUID, info *models.UserAuthInfo) error {
	_, err := db.Exec(queries.UpdateUserAuthInfo,
		info.MFAEnabled,
		info.MFAType,
		info.MFASecret,
		info.BackupCodes,
		userID)
	return err
}

// IncrementFailedAttempts increments the failed login attempts for a user
func (db *DB) IncrementFailedAttempts(userID uuid.UUID) (int, error) {
	var attempts int
	err := db.QueryRow(queries.IncrementFailedAttempts, userID).Scan(&attempts)
	return attempts, err
}

// ResetFailedAttempts resets the failed login attempts for a user
func (db *DB) ResetFailedAttempts(userID uuid.UUID) error {
	_, err := db.Exec(queries.ResetFailedAttempts, userID)
	return err
}

// LockUserAccount locks a user account
func (db *DB) LockUserAccount(userID uuid.UUID, durationMinutes int) error {
	_, err := db.Exec(queries.LockUserAccount, durationMinutes, userID)
	return err
}

// DisableUserAccount disables a user account
func (db *DB) DisableUserAccount(userID uuid.UUID, reason string, disabledBy uuid.UUID) error {
	_, err := db.Exec(queries.DisableUserAccount, reason, disabledBy, userID)
	return err
}

// EnableUserAccount enables a user account
func (db *DB) EnableUserAccount(userID uuid.UUID) error {
	_, err := db.Exec(queries.EnableUserAccount, userID)
	return err
}

// UpdateLastLogin updates the last login time for a user
func (db *DB) UpdateLastLogin(userID uuid.UUID) error {
	_, err := db.Exec(queries.UpdateLastLogin, userID)
	return err
}

// GetUserByID retrieves a user by their ID, excluding sensitive MFA data
func (db *DB) GetUserByID(userID string) (*models.User, error) {
	var user models.User
	var skipMFASecret, skipBackupCodes, disabledReason sql.NullString
	var lastFailedAttempt, accountLockedUntil, lastLogin, disabledAt sql.NullTime
	var disabledBy sql.NullString
	var mfaType []string
	var apiKey sql.NullString
	var apiKeyCreatedAt, apiKeyLastUsed sql.NullTime

	err := db.QueryRow(queries.GetUserByID, userID).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.MFAEnabled,
		pq.Array(&mfaType),
		&skipMFASecret,
		&skipBackupCodes,
		&user.PreferredMFAMethod,
		&user.LastPasswordChange,
		&user.FailedLoginAttempts,
		&lastFailedAttempt,
		&user.AccountLocked,
		&accountLockedUntil,
		&user.AccountEnabled,
		&lastLogin,
		&disabledReason,
		&disabledAt,
		&disabledBy,
		&user.NotifyOnJobCompletion,
		&apiKey,
		&apiKeyCreatedAt,
		&apiKeyLastUsed,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		debug.Error("Failed to get user by ID: %v", err)
		return nil, err
	}

	// Set the scanned mfa_type array
	user.MFAType = mfaType

	// Handle nullable fields
	if lastFailedAttempt.Valid {
		user.LastFailedAttempt = &lastFailedAttempt.Time
	}
	if accountLockedUntil.Valid {
		user.AccountLockedUntil = &accountLockedUntil.Time
	}
	if lastLogin.Valid {
		user.LastLogin = &lastLogin.Time
	}
	if disabledAt.Valid {
		user.DisabledAt = &disabledAt.Time
	}
	if disabledReason.Valid {
		user.DisabledReason = &disabledReason.String
	}
	if disabledBy.Valid {
		id, err := uuid.Parse(disabledBy.String)
		if err == nil {
			user.DisabledBy = &id
		}
	}
	if apiKey.Valid {
		user.APIKey = apiKey.String
	}
	if apiKeyCreatedAt.Valid {
		user.APIKeyCreatedAt = &apiKeyCreatedAt.Time
	}
	if apiKeyLastUsed.Valid {
		user.APIKeyLastUsed = &apiKeyLastUsed.Time
	}

	return &user, nil
}

// GetUserWithMFAData gets a user with sensitive MFA data
func (db *DB) GetUserWithMFAData(userID string) (*models.UserMFAData, error) {
	var user models.UserMFAData
	var backupCodes []string

	query := `
		SELECT 
			id,
			mfa_enabled,
			mfa_secret,
			backup_codes,
			preferred_mfa_method
		FROM users 
		WHERE id = $1
	`

	err := db.QueryRow(query, userID).Scan(
		&user.ID,
		&user.MFAEnabled,
		&user.MFASecret,
		pq.Array(&backupCodes),
		&user.PreferredMFAMethod,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get user MFA data: %w", err)
	}

	user.BackupCodes = backupCodes
	return &user, nil
}

// SetPreferredMFAMethod sets the user's preferred MFA method
func (db *DB) SetPreferredMFAMethod(userID string, method string) error {
	_, err := db.Exec(queries.SetPreferredMFAMethodQuery, userID, method)
	return err
}

// GetUserMFASettings gets a user's MFA settings
func (db *DB) GetUserMFASettings(userID string) (*models.UserMFAData, error) {
	var settings models.UserMFAData
	var mfaType []string
	var backupCodes []string
	var mfaSecret sql.NullString
	var preferredMethod sql.NullString

	err := db.QueryRow(queries.GetUserMFASettingsQuery, userID).Scan(
		&settings.MFAEnabled,
		pq.Array(&mfaType),
		&preferredMethod,
		&mfaSecret,
		pq.Array(&backupCodes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user MFA settings: %w", err)
	}

	settings.MFAType = mfaType
	settings.BackupCodes = backupCodes
	settings.ID = userID

	// Handle NULL values
	if mfaSecret.Valid {
		settings.MFASecret = mfaSecret.String
	}
	if preferredMethod.Valid {
		settings.PreferredMFAMethod = preferredMethod.String
	} else {
		settings.PreferredMFAMethod = "email" // Default to email if not set
	}

	return &settings, nil
}

// GetUserIDFromMFASession retrieves the user ID associated with an MFA session token
func (db *DB) GetUserIDFromMFASession(sessionToken string) (string, error) {
	var userID string
	err := db.QueryRow(`
		SELECT user_id 
		FROM mfa_sessions 
		WHERE session_token = $1 
		AND expires_at > NOW()`,
		sessionToken,
	).Scan(&userID)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("invalid or expired session token")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get user ID from MFA session: %w", err)
	}

	return userID, nil
}

// ClearMFASession removes an MFA session after successful verification
func (db *DB) ClearMFASession(sessionToken string) error {
	_, err := db.Exec(`
		DELETE FROM mfa_sessions
		WHERE session_token = $1`,
		sessionToken,
	)
	if err != nil {
		return fmt.Errorf("failed to clear MFA session: %w", err)
	}
	return nil
}

// RevokeToken marks a token as revoked with a reason
func (db *DB) RevokeToken(tokenID uuid.UUID, reason string) error {
	_, err := db.Exec(`
		UPDATE tokens
		SET revoked = true,
			revoked_at = CURRENT_TIMESTAMP,
			revoked_reason = $1
		WHERE id = $2`,
		reason, tokenID,
	)
	if err != nil {
		debug.Error("Failed to revoke token: %v", err)
		return err
	}
	return nil
}

// GetSessionByToken retrieves a session by its token value
func (db *DB) GetSessionByToken(token string) (*models.ActiveSession, error) {
	session := &models.ActiveSession{}
	err := db.QueryRow(`
		SELECT s.id, s.user_id, s.ip_address, s.user_agent,
			   s.created_at, s.last_active_at, s.token_id, s.session_started_at
		FROM active_sessions s
		JOIN tokens t ON s.token_id = t.id
		WHERE t.token = $1`,
		token,
	).Scan(
		&session.ID,
		&session.UserID,
		&session.IPAddress,
		&session.UserAgent,
		&session.CreatedAt,
		&session.LastActiveAt,
		&session.TokenID,
		&session.SessionStartedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found for token")
		}
		debug.Error("Failed to get session by token: %v", err)
		return nil, err
	}
	return session, nil
}

// GetTokenByValue retrieves a token ID by its value
func (db *DB) GetTokenByValue(token string) (uuid.UUID, error) {
	var tokenID uuid.UUID
	err := db.QueryRow(`
		SELECT id FROM tokens WHERE token = $1`,
		token,
	).Scan(&tokenID)
	if err != nil {
		if err == sql.ErrNoRows {
			return uuid.Nil, fmt.Errorf("token not found")
		}
		debug.Error("Failed to get token by value: %v", err)
		return uuid.Nil, err
	}
	return tokenID, nil
}

// TokenDetails holds token information for swap operations
type TokenDetails struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// IsTokenRefreshable checks if a token has passed the refresh threshold (1/3 of session time)
// Returns true if token is eligible for refresh (past 1/3 of session duration and not superseded)
func (db *DB) IsTokenRefreshable(token string) (bool, error) {
	var refreshable bool
	err := db.QueryRow(queries.IsTokenRefreshable, token).Scan(&refreshable)
	if err != nil {
		debug.Error("Failed to check if token is refreshable: %v", err)
		return false, err
	}
	return refreshable, nil
}

// GetTokenDetails retrieves token details for swap operations
func (db *DB) GetTokenDetails(token string) (*TokenDetails, error) {
	details := &TokenDetails{}
	err := db.QueryRow(queries.GetTokenDetails, token).Scan(
		&details.ID,
		&details.UserID,
		&details.CreatedAt,
		&details.ExpiresAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("token not found")
		}
		debug.Error("Failed to get token details: %v", err)
		return nil, err
	}
	return details, nil
}

// SwapTokenWithGrace atomically swaps an old token with a new one, preserving session linkage
// The old token is marked as superseded (not deleted) to allow a grace period for concurrent requests
func (db *DB) SwapTokenWithGrace(oldToken, newToken string, userID uuid.UUID, expiryMinutes int) (uuid.UUID, error) {
	tx, err := db.Begin()
	if err != nil {
		debug.Error("Failed to begin transaction for token swap: %v", err)
		return uuid.Nil, err
	}
	defer tx.Rollback()

	// Get old token details
	var oldTokenID uuid.UUID
	err = tx.QueryRow(`SELECT id FROM tokens WHERE token = $1 AND revoked = false`, oldToken).Scan(&oldTokenID)
	if err != nil {
		debug.Error("Failed to get old token ID: %v", err)
		return uuid.Nil, err
	}

	// Get the session linked to the old token to preserve session_started_at
	var sessionID uuid.UUID
	var sessionStartedAt time.Time
	var ipAddress, userAgent string
	err = tx.QueryRow(`
		SELECT id, session_started_at, ip_address, user_agent
		FROM active_sessions
		WHERE token_id = $1`, oldTokenID).Scan(&sessionID, &sessionStartedAt, &ipAddress, &userAgent)
	if err != nil && err != sql.ErrNoRows {
		debug.Error("Failed to get session for token: %v", err)
		return uuid.Nil, err
	}
	sessionExists := err != sql.ErrNoRows

	// Insert new token
	expiresAt := time.Now().Add(time.Duration(expiryMinutes) * time.Minute)
	var newTokenID uuid.UUID
	err = tx.QueryRow(`
		INSERT INTO tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id`, userID, newToken, expiresAt).Scan(&newTokenID)
	if err != nil {
		debug.Error("Failed to insert new token: %v", err)
		return uuid.Nil, err
	}

	// Mark old token as superseded (not deleted - allows grace period)
	_, err = tx.Exec(queries.SupersedeToken, oldToken, newTokenID)
	if err != nil {
		debug.Error("Failed to supersede old token: %v", err)
		return uuid.Nil, err
	}

	// Update session to point to new token, preserving session_started_at
	if sessionExists {
		_, err = tx.Exec(`
			UPDATE active_sessions
			SET token_id = $1, last_active_at = CURRENT_TIMESTAMP
			WHERE id = $2`, newTokenID, sessionID)
		if err != nil {
			debug.Error("Failed to update session token: %v", err)
			return uuid.Nil, err
		}
	} else {
		// Create session if it doesn't exist (shouldn't happen in normal flow)
		_, err = tx.Exec(`
			INSERT INTO active_sessions (user_id, ip_address, user_agent, token_id, session_started_at)
			VALUES ($1, $2, $3, $4, $5)`,
			userID, ipAddress, userAgent, newTokenID, time.Now())
		if err != nil {
			debug.Error("Failed to create session for new token: %v", err)
			return uuid.Nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		debug.Error("Failed to commit token swap transaction: %v", err)
		return uuid.Nil, err
	}

	debug.Info("Successfully swapped token for user %s, old token superseded with grace period", userID)
	return newTokenID, nil
}
