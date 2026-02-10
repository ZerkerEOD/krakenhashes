package services

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// ClaimVoucherService handles business logic for claim vouchers
type ClaimVoucherService struct {
	repo *repository.ClaimVoucherRepository
}

// NewClaimVoucherService creates a new claim voucher service
func NewClaimVoucherService(repo *repository.ClaimVoucherRepository) *ClaimVoucherService {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())
	return &ClaimVoucherService{repo: repo}
}

// generateClaimCode generates a random 20-character alphanumeric claim code with hyphens for readability
func generateClaimCode() string {
	// Define the character set (0-9 and A-Z)
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	code := make([]byte, 20)
	for i := range code {
		code[i] = chars[rand.Intn(len(chars))]
	}

	// Format with hyphens for display (XXXXX-XXXXX-XXXXX-XXXXX)
	return fmt.Sprintf("%s-%s-%s-%s",
		string(code[0:5]),
		string(code[5:10]),
		string(code[10:15]),
		string(code[15:20]))
}

// normalizeClaimCode removes hyphens from a claim code and converts to uppercase
func normalizeClaimCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(code, "-", ""))
}

// formatClaimCode adds hyphens to a claim code for display
func formatClaimCode(code string) string {
	code = normalizeClaimCode(code)
	if len(code) != 20 {
		return code
	}
	return fmt.Sprintf("%s-%s-%s-%s",
		code[0:5],
		code[5:10],
		code[10:15],
		code[15:20])
}

// CreateTempVoucher creates a temporary claim voucher.
// When isSystem is true, the voucher is created with the system user as owner,
// making any agent registered with it a system agent (universal, serves all teams).
func (s *ClaimVoucherService) CreateTempVoucher(ctx context.Context, userID string, expiresIn time.Duration, isContinuous bool, isSystem bool) (*models.ClaimVoucher, error) {
	// Determine the creator ID
	var creatorID uuid.UUID
	if isSystem {
		creatorID = models.SystemUserID
	} else {
		var err error
		creatorID, err = uuid.Parse(userID)
		if err != nil {
			debug.Error("failed to parse user ID: %v", err)
			return nil, fmt.Errorf("invalid user ID: %w", err)
		}
	}

	// Create voucher with normalized code for storage
	code := generateClaimCode()
	voucher := &models.ClaimVoucher{
		Code:         normalizeClaimCode(code),
		IsActive:     true,
		IsContinuous: isContinuous,
		CreatedByID:  creatorID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Save voucher
	if err := s.repo.Create(ctx, voucher); err != nil {
		debug.Error("failed to create voucher: %v", err)
		return nil, fmt.Errorf("failed to create voucher: %w", err)
	}

	// Format code for display before returning
	voucher.Code = formatClaimCode(voucher.Code)
	return voucher, nil
}

// ListVouchers retrieves all active vouchers
func (s *ClaimVoucherService) ListVouchers(ctx context.Context) ([]models.ClaimVoucher, error) {
	vouchers, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	// Format codes for display
	for i := range vouchers {
		vouchers[i].Code = formatClaimCode(vouchers[i].Code)
	}
	return vouchers, nil
}

// ListVouchersByUser retrieves active vouchers created by a specific user
func (s *ClaimVoucherService) ListVouchersByUser(ctx context.Context, userID uuid.UUID) ([]models.ClaimVoucher, error) {
	vouchers, err := s.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Format codes for display
	for i := range vouchers {
		vouchers[i].Code = formatClaimCode(vouchers[i].Code)
	}
	return vouchers, nil
}

// DisableVoucher disables a claim voucher
func (s *ClaimVoucherService) DisableVoucher(ctx context.Context, code string) error {
	// Normalize code before disabling
	normalizedCode := normalizeClaimCode(code)
	return s.repo.Deactivate(ctx, normalizedCode)
}

// GetVoucher retrieves a voucher by code
func (s *ClaimVoucherService) GetVoucher(ctx context.Context, code string) (*models.ClaimVoucher, error) {
	// Normalize code before lookup
	normalizedCode := normalizeClaimCode(code)
	voucher, err := s.repo.GetByCode(ctx, normalizedCode)
	if err != nil {
		return nil, err
	}

	// Format code for display before returning
	voucher.Code = formatClaimCode(voucher.Code)
	return voucher, nil
}

// UseVoucher validates a voucher without marking it as used
func (s *ClaimVoucherService) UseVoucher(ctx context.Context, code string, agentID int) error {
	// Normalize code before using
	normalizedCode := normalizeClaimCode(code)

	// Get voucher to check if it's valid
	voucher, err := s.repo.GetByCode(ctx, normalizedCode)
	if err != nil {
		return fmt.Errorf("invalid claim code")
	}

	if !voucher.IsValid() {
		return fmt.Errorf("claim code is not active")
	}

	return nil
}

// MarkVoucherAsUsed marks a voucher as used by an agent after successful connection
func (s *ClaimVoucherService) MarkVoucherAsUsed(ctx context.Context, code string, agentID int) error {
	// Normalize code before using
	normalizedCode := normalizeClaimCode(code)

	// Get voucher to check if it's valid
	voucher, err := s.repo.GetByCode(ctx, normalizedCode)
	if err != nil {
		return fmt.Errorf("invalid claim code")
	}

	if !voucher.IsValid() {
		return fmt.Errorf("claim code is not active")
	}

	// Mark voucher as used by the agent
	if err := s.repo.UseByAgent(ctx, normalizedCode, agentID); err != nil {
		return fmt.Errorf("failed to mark voucher as used: %w", err)
	}

	// For single-use vouchers, deactivate after use
	if !voucher.IsContinuous {
		if err := s.repo.Deactivate(ctx, normalizedCode); err != nil {
			return fmt.Errorf("failed to deactivate voucher: %w", err)
		}
	}

	return nil
}

// ValidateClaimCode validates a claim code
func (s *ClaimVoucherService) ValidateClaimCode(ctx context.Context, claimCode string) error {
	// Normalize claim code by removing hyphens and converting to uppercase
	normalizedCode := normalizeClaimCode(claimCode)

	voucher, err := s.repo.GetByCode(ctx, normalizedCode)
	if err != nil {
		debug.Error("Failed to get voucher: %v", err)
		return fmt.Errorf("invalid claim code")
	}

	if !voucher.IsActive {
		debug.Error("Claim code is not active")
		return fmt.Errorf("claim code is not active")
	}

	if !voucher.IsValid() {
		debug.Error("Claim code is not valid")
		return fmt.Errorf("claim code is not valid")
	}

	debug.Info("Claim code validated successfully")
	return nil
}
