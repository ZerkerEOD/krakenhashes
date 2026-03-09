package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// CustomCharsetService handles business logic for custom charset management.
type CustomCharsetService struct {
	charsetRepo repository.CustomCharsetRepository
}

// NewCustomCharsetService creates a new service for custom charsets.
func NewCustomCharsetService(charsetRepo repository.CustomCharsetRepository) *CustomCharsetService {
	return &CustomCharsetService{
		charsetRepo: charsetRepo,
	}
}

// GetByID retrieves a custom charset by its ID.
func (s *CustomCharsetService) GetByID(ctx context.Context, id uuid.UUID) (*models.CustomCharset, error) {
	return s.charsetRepo.GetByID(ctx, id)
}

// ListGlobal returns all global charsets.
func (s *CustomCharsetService) ListGlobal(ctx context.Context) ([]models.CustomCharset, error) {
	return s.charsetRepo.ListGlobal(ctx)
}

// ListAccessible returns all charsets accessible to a user (global + user's own + team).
func (s *CustomCharsetService) ListAccessible(ctx context.Context, userID uuid.UUID, teamIDs []uuid.UUID) ([]models.CustomCharset, error) {
	return s.charsetRepo.ListAccessible(ctx, userID, teamIDs)
}

// ListByTeam returns all charsets for a specific team.
func (s *CustomCharsetService) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]models.CustomCharset, error) {
	return s.charsetRepo.ListByTeam(ctx, teamID)
}

// CreateGlobalCharset creates a new admin-managed global charset.
func (s *CustomCharsetService) CreateGlobalCharset(ctx context.Context, name, description, definition string, createdBy uuid.UUID) (*models.CustomCharset, error) {
	if err := s.validateCharsetDefinition(definition); err != nil {
		return nil, err
	}

	charset := &models.CustomCharset{
		Name:       strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Definition: definition,
		Scope:      models.CustomCharsetScopeGlobal,
		OwnerID:    nil,
		CreatedBy:  &createdBy,
	}

	created, err := s.charsetRepo.Create(ctx, charset)
	if err != nil {
		return nil, fmt.Errorf("failed to create global charset: %w", err)
	}

	debug.Log("Created global custom charset", map[string]interface{}{
		"id":         created.ID,
		"name":       created.Name,
		"created_by": createdBy,
	})

	return created, nil
}

// CreateUserCharset creates a personal charset for a specific user.
func (s *CustomCharsetService) CreateUserCharset(ctx context.Context, userID uuid.UUID, name, description, definition string) (*models.CustomCharset, error) {
	if err := s.validateCharsetDefinition(definition); err != nil {
		return nil, err
	}

	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Definition:  definition,
		Scope:       models.CustomCharsetScopeUser,
		OwnerID:     &userID,
		CreatedBy:   &userID,
	}

	created, err := s.charsetRepo.Create(ctx, charset)
	if err != nil {
		return nil, fmt.Errorf("failed to create user charset: %w", err)
	}

	debug.Log("Created user custom charset", map[string]interface{}{
		"id":      created.ID,
		"name":    created.Name,
		"user_id": userID,
	})

	return created, nil
}

// CreateTeamCharset creates a team-scoped charset.
func (s *CustomCharsetService) CreateTeamCharset(ctx context.Context, teamID uuid.UUID, name, description, definition string, createdBy uuid.UUID) (*models.CustomCharset, error) {
	if err := s.validateCharsetDefinition(definition); err != nil {
		return nil, err
	}

	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Definition:  definition,
		Scope:       models.CustomCharsetScopeTeam,
		OwnerID:     &teamID,
		CreatedBy:   &createdBy,
	}

	created, err := s.charsetRepo.Create(ctx, charset)
	if err != nil {
		return nil, fmt.Errorf("failed to create team charset: %w", err)
	}

	debug.Log("Created team custom charset", map[string]interface{}{
		"id":         created.ID,
		"name":       created.Name,
		"team_id":    teamID,
		"created_by": createdBy,
	})

	return created, nil
}

// UpdateCharset updates an existing charset with permission checks.
func (s *CustomCharsetService) UpdateCharset(ctx context.Context, id uuid.UUID, name, description, definition string, userID uuid.UUID, isAdmin bool) (*models.CustomCharset, error) {
	existing, err := s.charsetRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Permission check: admins can edit global/team charsets, users can edit their own
	if !isAdmin {
		if existing.Scope != models.CustomCharsetScopeUser || existing.OwnerID == nil || *existing.OwnerID != userID {
			return nil, fmt.Errorf("permission denied: you can only edit your own charsets")
		}
	}

	if definition != "" {
		if err := s.validateCharsetDefinition(definition); err != nil {
			return nil, err
		}
	}

	update := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Definition:  definition,
	}

	updated, err := s.charsetRepo.Update(ctx, id, update)
	if err != nil {
		return nil, fmt.Errorf("failed to update charset: %w", err)
	}

	debug.Log("Updated custom charset", map[string]interface{}{
		"id":      id,
		"name":    updated.Name,
		"user_id": userID,
	})

	return updated, nil
}

// DeleteCharset deletes a charset with permission checks.
func (s *CustomCharsetService) DeleteCharset(ctx context.Context, id uuid.UUID, userID uuid.UUID, isAdmin bool) error {
	existing, err := s.charsetRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Permission check
	if !isAdmin {
		if existing.Scope != models.CustomCharsetScopeUser || existing.OwnerID == nil || *existing.OwnerID != userID {
			return fmt.Errorf("permission denied: you can only delete your own charsets")
		}
	}

	if err := s.charsetRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete charset: %w", err)
	}

	debug.Log("Deleted custom charset", map[string]interface{}{
		"id":      id,
		"scope":   existing.Scope,
		"user_id": userID,
	})

	return nil
}

// validateCharsetDefinition checks that a charset definition contains valid hashcat syntax.
// Valid elements: built-in placeholders (?l, ?u, ?d, ?s, ?a, ?b, ?h, ?H),
// custom charset references (?1-?4), and literal characters.
func (s *CustomCharsetService) validateCharsetDefinition(definition string) error {
	if definition == "" {
		return fmt.Errorf("charset definition cannot be empty")
	}

	i := 0
	for i < len(definition) {
		if definition[i] == '?' {
			if i+1 >= len(definition) {
				return fmt.Errorf("incomplete placeholder at end of charset definition")
			}
			second := definition[i+1]
			switch second {
			case 'l', 'u', 'd', 's', 'a', 'b', 'h', 'H', '1', '2', '3', '4':
				// Valid placeholder
			default:
				return fmt.Errorf("invalid placeholder ?%c in charset definition", second)
			}
			i += 2
		} else {
			// Literal character - always valid
			i++
		}
	}

	return nil
}
