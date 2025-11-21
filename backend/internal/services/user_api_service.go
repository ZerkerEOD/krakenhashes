package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	// APIKeyLength is the length of the API key in bytes (32 bytes = 64 hex characters)
	APIKeyLength = 32
)

// UserAPIService handles user API key operations
type UserAPIService struct {
	userRepo *repository.UserRepository
}

// NewUserAPIService creates a new user API service
func NewUserAPIService(userRepo *repository.UserRepository) *UserAPIService {
	return &UserAPIService{
		userRepo: userRepo,
	}
}

// GenerateAPIKey generates a new API key for a user
// Returns the plaintext API key (shown once) and stores the bcrypt hash
func (s *UserAPIService) GenerateAPIKey(ctx context.Context, userID uuid.UUID) (string, error) {
	debug.Info("Generating API key for user %s", userID.String())

	// Generate random bytes for API key
	bytes := make([]byte, APIKeyLength)
	if _, err := rand.Read(bytes); err != nil {
		debug.Error("Failed to generate random bytes for API key: %v", err)
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	// Convert to hex string (64 characters)
	apiKey := hex.EncodeToString(bytes)
	debug.Debug("Generated API key (length: %d)", len(apiKey))

	// Hash the API key using bcrypt
	hashedKey, err := bcrypt.GenerateFromPassword([]byte(apiKey), bcrypt.DefaultCost)
	if err != nil {
		debug.Error("Failed to hash API key: %v", err)
		return "", fmt.Errorf("failed to hash API key: %w", err)
	}

	// Store the hashed key in the database
	if err := s.userRepo.UpdateAPIKey(ctx, userID, string(hashedKey)); err != nil {
		debug.Error("Failed to store API key for user %s: %v", userID.String(), err)
		return "", fmt.Errorf("failed to store API key: %w", err)
	}

	debug.Info("Successfully generated and stored API key for user %s", userID.String())
	return apiKey, nil
}

// ValidateAPIKey validates an API key for a user
// Returns the user UUID if valid, error otherwise
func (s *UserAPIService) ValidateAPIKey(ctx context.Context, email, apiKey string) (uuid.UUID, error) {
	debug.Debug("Validating API key for email: %s", email)

	// Get user by email
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		debug.Error("Failed to get user by email %s: %v", email, err)
		return uuid.Nil, fmt.Errorf("invalid credentials")
	}

	if user == nil {
		debug.Warning("User not found for email: %s", email)
		return uuid.Nil, fmt.Errorf("invalid credentials")
	}

	// Check if user has an API key
	if user.APIKey == "" {
		debug.Warning("User %s has no API key", user.ID.String())
		return uuid.Nil, fmt.Errorf("no API key configured")
	}

	// Verify the API key against the stored hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.APIKey), []byte(apiKey)); err != nil {
		debug.Warning("Invalid API key for user %s", user.ID.String())
		return uuid.Nil, fmt.Errorf("invalid credentials")
	}

	// Update last used timestamp
	if err := s.userRepo.UpdateAPIKeyLastUsed(ctx, user.ID); err != nil {
		debug.Error("Failed to update API key last used for user %s: %v", user.ID.String(), err)
		// Don't fail the request, just log the error
	}

	debug.Info("Successfully validated API key for user %s", user.ID.String())
	return user.ID, nil
}

// RevokeAPIKey revokes (nullifies) the API key for a user
func (s *UserAPIService) RevokeAPIKey(ctx context.Context, userID uuid.UUID) error {
	debug.Info("Revoking API key for user %s", userID.String())

	if err := s.userRepo.RevokeAPIKey(ctx, userID); err != nil {
		debug.Error("Failed to revoke API key for user %s: %v", userID.String(), err)
		return fmt.Errorf("failed to revoke API key: %w", err)
	}

	debug.Info("Successfully revoked API key for user %s", userID.String())
	return nil
}

// GetAPIKeyInfo returns metadata about a user's API key (not the key itself)
func (s *UserAPIService) GetAPIKeyInfo(ctx context.Context, userID uuid.UUID) (*APIKeyInfo, error) {
	debug.Debug("Getting API key info for user %s", userID.String())

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		debug.Error("Failed to get user %s: %v", userID.String(), err)
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	info := &APIKeyInfo{
		HasKey: user.APIKey != "",
	}

	if user.APIKeyCreatedAt != nil {
		info.CreatedAt = user.APIKeyCreatedAt
	}

	if user.APIKeyLastUsed != nil {
		info.LastUsed = user.APIKeyLastUsed
	}

	return info, nil
}

// APIKeyInfo contains metadata about a user's API key
type APIKeyInfo struct {
	HasKey    bool       `json:"has_key"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}
