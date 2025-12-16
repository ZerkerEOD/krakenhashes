package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/lib/pq"
)

var (
	ErrMFACooldown = errors.New("MFA code request is on cooldown")
	ErrInvalidCode = errors.New("invalid MFA code")
	ErrCodeExpired = errors.New("MFA code has expired")
	ErrMaxAttempts = errors.New("maximum verification attempts exceeded")
)

// IsMFARequired checks if MFA is required by policy
func (db *DB) IsMFARequired() (bool, error) {
	var required bool
	err := db.QueryRow(queries.IsMFARequiredQuery).Scan(&required)
	if err != nil {
		debug.Error("Failed to check MFA requirement: %v", err)
		return false, err
	}
	return required, nil
}

// EnableMFA enables MFA for a user with the specified method
func (db *DB) EnableMFA(userID, method, secret string) error {
	_, err := db.Exec(queries.EnableMFAQuery, userID, method, secret)
	if err != nil {
		debug.Error("Failed to enable MFA: %v", err)
		return err
	}
	return nil
}

// DisableMFA disables MFA for a user
func (db *DB) DisableMFA(userID string) error {
	_, err := db.Exec(queries.DisableMFAQuery, userID)
	if err != nil {
		debug.Error("Failed to disable MFA: %v", err)
		return err
	}
	return nil
}

// StorePendingMFASetup stores a pending MFA setup for a user
func (db *DB) StorePendingMFASetup(userID, method, secret string) error {
	_, err := db.Exec(queries.StorePendingMFASetupQuery, userID, method, secret)
	if err != nil {
		debug.Error("Failed to store pending MFA setup: %v", err)
		return err
	}
	return nil
}

// GetPendingMFASetup retrieves a pending MFA setup for a user
func (db *DB) GetPendingMFASetup(userID string) (string, error) {
	var secret string
	err := db.QueryRow(queries.GetPendingMFASetupQuery, userID).Scan(&secret)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		debug.Error("Failed to get pending MFA setup: %v", err)
		return "", err
	}
	return secret, nil
}

// ClearPendingMFASetup removes a pending MFA setup
func (db *DB) ClearPendingMFASetup(userID string) error {
	_, err := db.Exec(queries.ClearPendingMFASetupQuery, userID)
	if err != nil {
		debug.Error("Failed to clear pending MFA setup: %v", err)
		return err
	}
	return nil
}

// StoreBackupCodes stores backup codes for a user
func (db *DB) StoreBackupCodes(userID string, codes []string) error {
	_, err := db.Exec(queries.StoreBackupCodesQuery, userID, pq.Array(codes))
	if err != nil {
		debug.Error("Failed to store backup codes: %v", err)
		return err
	}
	return nil
}

// StoreEmailMFACode stores an email MFA code for a user
func (db *DB) StoreEmailMFACode(userID, code string) error {
	// Check cooldown period
	var onCooldown bool
	err := db.QueryRow(queries.CheckEmailMFACooldownQuery, userID).Scan(&onCooldown)
	if err != nil {
		debug.Error("Failed to check email MFA cooldown: %v", err)
		return err
	}
	if onCooldown {
		return ErrMFACooldown
	}

	_, err = db.Exec(queries.StoreEmailMFACodeQuery, userID, code)
	if err != nil {
		debug.Error("Failed to store email MFA code: %v", err)
		return err
	}
	return nil
}

// VerifyEmailMFACode verifies an email MFA code
func (db *DB) VerifyEmailMFACode(userID, code string) error {
	var success bool
	err := db.QueryRow(queries.VerifyEmailMFACodeQuery, userID, code).Scan(&success)
	if err != nil {
		debug.Error("Failed to verify email MFA code: %v", err)
		return err
	}

	if !success {
		return ErrInvalidCode
	}

	return nil
}

// GetMFAVerifyAttempts gets the number of verification attempts for a session
func (db *DB) GetMFAVerifyAttempts(sessionToken string) (int, error) {
	var attempts int
	err := db.QueryRow(queries.GetMFAVerifyAttemptsQuery, sessionToken).Scan(&attempts)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		debug.Error("Failed to get MFA verify attempts: %v", err)
		return 0, err
	}
	return attempts, nil
}

// IncrementMFAVerifyAttempts increments the verification attempts counter for a session
func (db *DB) IncrementMFAVerifyAttempts(sessionToken string) error {
	var attempts int
	err := db.QueryRow(queries.IncrementMFAVerifyAttemptsQuery, sessionToken).Scan(&attempts)
	if err != nil {
		debug.Error("Failed to increment MFA verify attempts: %v", err)
		return err
	}
	return nil
}

// ClearMFAVerifyAttempts resets the verification attempts counter for a session
func (db *DB) ClearMFAVerifyAttempts(sessionToken string) error {
	_, err := db.Exec(queries.ClearMFAVerifyAttemptsQuery, sessionToken)
	if err != nil {
		debug.Error("Failed to clear MFA verify attempts: %v", err)
		return err
	}
	return nil
}

// CleanupMFAData removes expired MFA data
func (db *DB) CleanupMFAData() error {
	tx, err := db.Begin()
	if err != nil {
		debug.Error("Failed to begin transaction: %v", err)
		return err
	}
	defer tx.Rollback()

	// Cleanup pending MFA setups
	_, err = tx.Exec(queries.CleanupPendingMFASetupQuery)
	if err != nil {
		debug.Error("Failed to cleanup pending MFA setups: %v", err)
		return err
	}

	// Cleanup email MFA codes
	_, err = tx.Exec(queries.CleanupEmailMFACodesQuery)
	if err != nil {
		debug.Error("Failed to cleanup email MFA codes: %v", err)
		return err
	}

	return tx.Commit()
}

// ValidateAndUseBackupCode validates a backup code and removes it from the user's backup codes
func (db *DB) ValidateAndUseBackupCode(userID string, code string) (bool, error) {
	var id string
	err := db.QueryRow(queries.ValidateAndUseBackupCodeQuery, userID, code).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetRemainingBackupCodesCount returns the number of remaining backup codes for a user
func (db *DB) GetRemainingBackupCodesCount(userID string) (int, error) {
	var count int
	err := db.QueryRow(queries.GetRemainingBackupCodesCountQuery, userID).Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get remaining backup codes count: %w", err)
	}
	return count, nil
}

// CreateMFASession creates a new MFA session
func (db *DB) CreateMFASession(userID, sessionToken string) (*models.MFASession, error) {
	session := &models.MFASession{}
	err := db.QueryRow(queries.CreateMFASessionQuery, userID, sessionToken).Scan(&session.ID, &session.ExpiresAt)
	if err != nil {
		debug.Error("Failed to create MFA session: %v", err)
		return nil, err
	}
	session.UserID = userID
	session.SessionToken = sessionToken
	session.Attempts = 0
	return session, nil
}

// GetMFASession retrieves an MFA session by token
func (db *DB) GetMFASession(sessionToken string) (*models.MFASession, error) {
	session := &models.MFASession{SessionToken: sessionToken}
	err := db.QueryRow(queries.GetMFASessionQuery, sessionToken).Scan(&session.UserID, &session.Attempts, &session.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		debug.Error("Failed to get MFA session: %v", err)
		return nil, err
	}
	return session, nil
}

// IncrementMFASessionAttempts increments the attempts counter for an MFA session
func (db *DB) IncrementMFASessionAttempts(sessionToken string) (int, error) {
	var attempts int
	err := db.QueryRow(queries.IncrementMFASessionAttemptsQuery, sessionToken).Scan(&attempts)
	if err != nil {
		debug.Error("Failed to increment MFA session attempts: %v", err)
		return 0, err
	}
	return attempts, nil
}

// DeleteMFASession deletes an MFA session
func (db *DB) DeleteMFASession(sessionToken string) error {
	_, err := db.Exec(queries.DeleteMFASessionQuery, sessionToken)
	if err != nil {
		debug.Error("Failed to delete MFA session: %v", err)
		return err
	}
	return nil
}
