package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrPasskeyNotFound       = errors.New("passkey not found")
	ErrPasskeyAlreadyExists  = errors.New("passkey already exists")
	ErrChallengeExpired      = errors.New("challenge has expired")
	ErrChallengeNotFound     = errors.New("challenge not found")
	ErrSignCountMismatch     = errors.New("sign count validation failed - possible cloned authenticator")
	ErrWebAuthnNotConfigured = errors.New("WebAuthn is not configured")
)

// CreatePasskey creates a new passkey credential for a user
func (db *DB) CreatePasskey(passkey *models.UserPasskey) error {
	err := db.QueryRow(
		queries.CreatePasskeyQuery,
		passkey.UserID,
		passkey.CredentialID,
		passkey.PublicKey,
		passkey.AAGUID,
		passkey.SignCount,
		pq.Array(passkey.Transports),
		passkey.Name,
		passkey.BackupEligible,
		passkey.BackupState,
	).Scan(&passkey.ID, &passkey.CreatedAt)

	if err != nil {
		debug.Error("Failed to create passkey: %v", err)
		return err
	}
	return nil
}

// GetUserPasskeys retrieves all passkeys for a user
func (db *DB) GetUserPasskeys(userID uuid.UUID) ([]*models.UserPasskey, error) {
	rows, err := db.Query(queries.GetUserPasskeysQuery, userID)
	if err != nil {
		debug.Error("Failed to get user passkeys: %v", err)
		return nil, err
	}
	defer rows.Close()

	var passkeys []*models.UserPasskey
	for rows.Next() {
		p := &models.UserPasskey{}
		var transports pq.StringArray
		err := rows.Scan(
			&p.ID,
			&p.UserID,
			&p.CredentialID,
			&p.PublicKey,
			&p.AAGUID,
			&p.SignCount,
			&transports,
			&p.Name,
			&p.BackupEligible,
			&p.BackupState,
			&p.CreatedAt,
			&p.LastUsedAt,
		)
		if err != nil {
			debug.Error("Failed to scan passkey: %v", err)
			return nil, err
		}
		p.Transports = []string(transports)
		passkeys = append(passkeys, p)
	}

	return passkeys, rows.Err()
}

// GetPasskeyByCredentialID retrieves a passkey by its credential ID
func (db *DB) GetPasskeyByCredentialID(credentialID []byte) (*models.UserPasskey, error) {
	p := &models.UserPasskey{}
	var transports pq.StringArray
	err := db.QueryRow(queries.GetPasskeyByCredentialIDQuery, credentialID).Scan(
		&p.ID,
		&p.UserID,
		&p.CredentialID,
		&p.PublicKey,
		&p.AAGUID,
		&p.SignCount,
		&transports,
		&p.Name,
		&p.BackupEligible,
		&p.BackupState,
		&p.CreatedAt,
		&p.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrPasskeyNotFound
	}
	if err != nil {
		debug.Error("Failed to get passkey by credential ID: %v", err)
		return nil, err
	}
	p.Transports = []string(transports)
	return p, nil
}

// GetPasskeyByID retrieves a passkey by ID and user ID (for ownership verification)
func (db *DB) GetPasskeyByID(passkeyID, userID uuid.UUID) (*models.UserPasskey, error) {
	p := &models.UserPasskey{}
	var transports pq.StringArray
	err := db.QueryRow(queries.GetPasskeyByIDAndUserQuery, passkeyID, userID).Scan(
		&p.ID,
		&p.UserID,
		&p.CredentialID,
		&p.PublicKey,
		&p.AAGUID,
		&p.SignCount,
		&transports,
		&p.Name,
		&p.BackupEligible,
		&p.BackupState,
		&p.CreatedAt,
		&p.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrPasskeyNotFound
	}
	if err != nil {
		debug.Error("Failed to get passkey by ID: %v", err)
		return nil, err
	}
	p.Transports = []string(transports)
	return p, nil
}

// UpdatePasskeySignCount updates the sign count after successful authentication
func (db *DB) UpdatePasskeySignCount(passkeyID uuid.UUID, newSignCount uint32) error {
	_, err := db.Exec(queries.UpdatePasskeySignCountQuery, passkeyID, newSignCount)
	if err != nil {
		debug.Error("Failed to update passkey sign count: %v", err)
		return err
	}
	return nil
}

// DeletePasskey deletes a passkey by ID and user ID
func (db *DB) DeletePasskey(passkeyID, userID uuid.UUID) error {
	result, err := db.Exec(queries.DeletePasskeyQuery, passkeyID, userID)
	if err != nil {
		debug.Error("Failed to delete passkey: %v", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}

// RenamePasskey renames a passkey
func (db *DB) RenamePasskey(passkeyID, userID uuid.UUID, name string) error {
	result, err := db.Exec(queries.RenamePasskeyQuery, passkeyID, userID, name)
	if err != nil {
		debug.Error("Failed to rename passkey: %v", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}

// GetPasskeyCount returns the number of passkeys for a user
func (db *DB) GetPasskeyCount(userID uuid.UUID) (int, error) {
	var count int
	err := db.QueryRow(queries.GetPasskeyCountQuery, userID).Scan(&count)
	if err != nil {
		debug.Error("Failed to get passkey count: %v", err)
		return 0, err
	}
	return count, nil
}

// UserHasPasskeys checks if a user has any passkeys registered
func (db *DB) UserHasPasskeys(userID uuid.UUID) (bool, error) {
	var hasPasskeys bool
	err := db.QueryRow(queries.UserHasPasskeysQuery, userID).Scan(&hasPasskeys)
	if err != nil {
		debug.Error("Failed to check if user has passkeys: %v", err)
		return false, err
	}
	return hasPasskeys, nil
}

// GetUserCredentialIDs returns all credential IDs for a user (for excludeCredentials)
func (db *DB) GetUserCredentialIDs(userID uuid.UUID) ([][]byte, [][]string, error) {
	rows, err := db.Query(queries.GetUserCredentialIDsQuery, userID)
	if err != nil {
		debug.Error("Failed to get user credential IDs: %v", err)
		return nil, nil, err
	}
	defer rows.Close()

	var credentialIDs [][]byte
	var transports [][]string
	for rows.Next() {
		var credID []byte
		var trans pq.StringArray
		if err := rows.Scan(&credID, &trans); err != nil {
			debug.Error("Failed to scan credential ID: %v", err)
			return nil, nil, err
		}
		credentialIDs = append(credentialIDs, credID)
		transports = append(transports, []string(trans))
	}

	return credentialIDs, transports, rows.Err()
}

// StorePendingPasskeyRegistration stores a pending passkey registration challenge
func (db *DB) StorePendingPasskeyRegistration(userID uuid.UUID, challenge, sessionData []byte) error {
	_, err := db.Exec(queries.StorePendingPasskeyRegistrationQuery, userID, challenge, sessionData)
	if err != nil {
		debug.Error("Failed to store pending passkey registration: %v", err)
		return err
	}
	return nil
}

// GetPendingPasskeyRegistration retrieves a pending passkey registration challenge
func (db *DB) GetPendingPasskeyRegistration(userID uuid.UUID) ([]byte, []byte, error) {
	var challenge, sessionData []byte
	err := db.QueryRow(queries.GetPendingPasskeyRegistrationQuery, userID).Scan(&challenge, &sessionData)
	if err == sql.ErrNoRows {
		return nil, nil, ErrChallengeNotFound
	}
	if err != nil {
		debug.Error("Failed to get pending passkey registration: %v", err)
		return nil, nil, err
	}
	return challenge, sessionData, nil
}

// DeletePendingPasskeyRegistration deletes a pending passkey registration
func (db *DB) DeletePendingPasskeyRegistration(userID uuid.UUID) error {
	_, err := db.Exec(queries.DeletePendingPasskeyRegistrationQuery, userID)
	if err != nil {
		debug.Error("Failed to delete pending passkey registration: %v", err)
		return err
	}
	return nil
}

// StorePendingPasskeyAuthentication stores a pending passkey authentication challenge
func (db *DB) StorePendingPasskeyAuthentication(sessionToken string, userID uuid.UUID, challenge, sessionData []byte) error {
	_, err := db.Exec(queries.StorePendingPasskeyAuthenticationQuery, sessionToken, userID, challenge, sessionData)
	if err != nil {
		debug.Error("Failed to store pending passkey authentication: %v", err)
		return err
	}
	return nil
}

// GetPendingPasskeyAuthentication retrieves a pending passkey authentication challenge
func (db *DB) GetPendingPasskeyAuthentication(sessionToken string) (uuid.UUID, []byte, []byte, error) {
	var userID uuid.UUID
	var challenge, sessionData []byte
	err := db.QueryRow(queries.GetPendingPasskeyAuthenticationQuery, sessionToken).Scan(&userID, &challenge, &sessionData)
	if err == sql.ErrNoRows {
		return uuid.Nil, nil, nil, ErrChallengeNotFound
	}
	if err != nil {
		debug.Error("Failed to get pending passkey authentication: %v", err)
		return uuid.Nil, nil, nil, err
	}
	return userID, challenge, sessionData, nil
}

// DeletePendingPasskeyAuthentication deletes a pending passkey authentication
func (db *DB) DeletePendingPasskeyAuthentication(sessionToken string) error {
	_, err := db.Exec(queries.DeletePendingPasskeyAuthenticationQuery, sessionToken)
	if err != nil {
		debug.Error("Failed to delete pending passkey authentication: %v", err)
		return err
	}
	return nil
}

// GetWebAuthnSettings retrieves WebAuthn settings from auth_settings
func (db *DB) GetWebAuthnSettings() (*models.WebAuthnSettings, error) {
	settings := &models.WebAuthnSettings{}
	var rpOrigins pq.StringArray
	err := db.QueryRow(queries.GetWebAuthnSettingsQuery).Scan(
		&settings.RPID,
		&rpOrigins,
		&settings.RPDisplayName,
	)
	if err != nil {
		debug.Error("Failed to get WebAuthn settings: %v", err)
		return nil, err
	}
	settings.RPOrigins = []string(rpOrigins)
	return settings, nil
}

// UpdateWebAuthnSettings updates WebAuthn settings in auth_settings
func (db *DB) UpdateWebAuthnSettings(settings *models.WebAuthnSettings) error {
	_, err := db.Exec(queries.UpdateWebAuthnSettingsQuery,
		settings.RPID,
		pq.Array(settings.RPOrigins),
		settings.RPDisplayName,
	)
	if err != nil {
		debug.Error("Failed to update WebAuthn settings: %v", err)
		return err
	}
	return nil
}

// CleanupExpiredPasskeyChallenges removes expired passkey challenges
func (db *DB) CleanupExpiredPasskeyChallenges() error {
	_, err := db.Exec(queries.CleanupExpiredPasskeyChallengesQuery)
	if err != nil {
		debug.Error("Failed to cleanup expired passkey challenges: %v", err)
		return err
	}
	return nil
}

// ValidateAndUpdateSignCount validates the sign count and updates it
// Returns error if sign count validation fails (possible cloned authenticator)
func (db *DB) ValidateAndUpdateSignCount(passkeyID uuid.UUID, storedSignCount, newSignCount uint32) error {
	// Sign count must always increase (or be 0 for authenticators that don't support it)
	if storedSignCount > 0 && newSignCount <= storedSignCount {
		debug.Warning("Sign count validation failed for passkey %s: stored=%d, received=%d",
			passkeyID, storedSignCount, newSignCount)
		return ErrSignCountMismatch
	}

	return db.UpdatePasskeySignCount(passkeyID, newSignCount)
}

// AddPasskeyToUserMFATypes adds passkey to the user's mfa_type array if not already present
func (db *DB) AddPasskeyToUserMFATypes(userID uuid.UUID) error {
	_, err := db.Exec(`
		UPDATE users
		SET mfa_type = CASE
			WHEN NOT 'passkey' = ANY(mfa_type) THEN array_append(mfa_type, 'passkey')
			ELSE mfa_type
		END,
		mfa_enabled = true,
		updated_at = NOW()
		WHERE id = $1
	`, userID)
	if err != nil {
		debug.Error("Failed to add passkey to user MFA types: %v", err)
		return err
	}
	return nil
}

// RemovePasskeyFromUserMFATypes removes passkey from user's mfa_type array if they have no passkeys left
func (db *DB) RemovePasskeyFromUserMFATypes(userID uuid.UUID) error {
	// Check if user has any remaining passkeys
	hasPasskeys, err := db.UserHasPasskeys(userID)
	if err != nil {
		return err
	}

	if !hasPasskeys {
		_, err := db.Exec(`
			UPDATE users
			SET mfa_type = array_remove(mfa_type, 'passkey'),
				preferred_mfa_method = CASE
					WHEN preferred_mfa_method = 'passkey' THEN 'email'
					ELSE preferred_mfa_method
				END,
				updated_at = NOW()
			WHERE id = $1
		`, userID)
		if err != nil {
			debug.Error("Failed to remove passkey from user MFA types: %v", err)
			return err
		}
	}
	return nil
}

// PasskeyCredentialInfo holds basic credential info for WebAuthn operations
type PasskeyCredentialInfo struct {
	CredentialID []byte
	PublicKey    []byte
	AAGUID       []byte
	SignCount    uint32
	Transports   []string
	LastUsedAt   *time.Time
}
