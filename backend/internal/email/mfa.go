package email

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	// MFATemplateID is the ID of the MFA code email template
	MFATemplateID = 4 // This should match the ID in the database
)

// SendMFACode sends an MFA verification code via email
func (s *Service) SendMFACode(ctx context.Context, to string, code string) error {
	debug.Debug("Sending MFA code to %s", to)

	// Get MFA code expiry from settings
	var expiryMinutes int
	err := s.db.QueryRowContext(ctx, queries.GetMFACodeExpiryMinutesQuery).Scan(&expiryMinutes)
	if err != nil {
		debug.Error("Failed to get MFA code expiry minutes: %v", err)
		expiryMinutes = 5 // Default to 5 minutes if setting not found
	}

	data := map[string]interface{}{
		"Code":          code,
		"ExpiryMinutes": expiryMinutes,
	}

	if err := s.SendTemplatedEmail(ctx, to, MFATemplateID, data); err != nil {
		debug.Error("Failed to send MFA code email: %v", err)
		return fmt.Errorf("failed to send MFA code: %w", err)
	}

	debug.Info("MFA code sent successfully to %s", to)
	return nil
}
