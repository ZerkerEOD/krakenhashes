package services

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// CustomCharsetService handles business logic for custom charset management.
type CustomCharsetService struct {
	charsetRepo repository.CustomCharsetRepository
	dataDir     string
}

// NewCustomCharsetService creates a new service for custom charsets.
func NewCustomCharsetService(charsetRepo repository.CustomCharsetRepository, dataDir string) *CustomCharsetService {
	return &CustomCharsetService{
		charsetRepo: charsetRepo,
		dataDir:     dataDir,
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

// --- Inline charset creation ---

// CreateGlobalCharset creates a new admin-managed global charset.
func (s *CustomCharsetService) CreateGlobalCharset(ctx context.Context, name, description, definition string, isHex bool, createdBy uuid.UUID) (*models.CustomCharset, error) {
	if isHex {
		if err := s.validateHexCharsetDefinition(definition); err != nil {
			return nil, err
		}
	} else {
		if err := s.validateCharsetDefinition(definition); err != nil {
			return nil, err
		}
	}

	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CharsetType: models.CustomCharsetTypeInline,
		Definition:  &definition,
		IsHex:       isHex,
		Scope:       models.CustomCharsetScopeGlobal,
		OwnerID:     nil,
		CreatedBy:   &createdBy,
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
func (s *CustomCharsetService) CreateUserCharset(ctx context.Context, userID uuid.UUID, name, description, definition string, isHex bool) (*models.CustomCharset, error) {
	if isHex {
		if err := s.validateHexCharsetDefinition(definition); err != nil {
			return nil, err
		}
	} else {
		if err := s.validateCharsetDefinition(definition); err != nil {
			return nil, err
		}
	}

	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CharsetType: models.CustomCharsetTypeInline,
		Definition:  &definition,
		IsHex:       isHex,
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
func (s *CustomCharsetService) CreateTeamCharset(ctx context.Context, teamID uuid.UUID, name, description, definition string, isHex bool, createdBy uuid.UUID) (*models.CustomCharset, error) {
	if isHex {
		if err := s.validateHexCharsetDefinition(definition); err != nil {
			return nil, err
		}
	} else {
		if err := s.validateCharsetDefinition(definition); err != nil {
			return nil, err
		}
	}

	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CharsetType: models.CustomCharsetTypeInline,
		Definition:  &definition,
		IsHex:       isHex,
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

// --- File charset creation ---

// HCCHRFileInfo contains validated metadata about a .hcchr charset file.
type HCCHRFileInfo struct {
	ByteCount int
	MD5       string
	FileSize  int64
}

// ValidateHCCHRFile validates a .hcchr charset file and returns its metadata.
// Checks: file size ≤ 1023 bytes (hashcat reads into char[1024] with fread(..., sizeof-1)),
// counts unique bytes (max 256).
func (s *CustomCharsetService) ValidateHCCHRFile(filePath string) (*HCCHRFileInfo, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat charset file: %w", err)
	}

	if info.Size() > 1023 {
		return nil, fmt.Errorf("charset file too large: %d bytes (max 1023 bytes, hashcat read buffer limit)", info.Size())
	}

	if info.Size() == 0 {
		return nil, fmt.Errorf("charset file is empty")
	}

	// Read all bytes
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read charset file: %w", err)
	}

	// Count unique bytes
	seen := make(map[byte]bool)
	for _, b := range data {
		seen[b] = true
	}

	// Calculate MD5
	hash := md5.Sum(data)
	md5Str := hex.EncodeToString(hash[:])

	return &HCCHRFileInfo{
		ByteCount: len(seen),
		MD5:       md5Str,
		FileSize:  info.Size(),
	}, nil
}

// CreateGlobalFileCharset creates a new admin-managed global file charset.
func (s *CustomCharsetService) CreateGlobalFileCharset(ctx context.Context, name, description string, fileInfo *HCCHRFileInfo, relFilePath string, createdBy uuid.UUID) (*models.CustomCharset, error) {
	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CharsetType: models.CustomCharsetTypeFile,
		FilePath:    &relFilePath,
		FileMD5:     &fileInfo.MD5,
		FileSize:    &fileInfo.FileSize,
		ByteCount:   &fileInfo.ByteCount,
		Scope:       models.CustomCharsetScopeGlobal,
		OwnerID:     nil,
		CreatedBy:   &createdBy,
	}

	created, err := s.charsetRepo.Create(ctx, charset)
	if err != nil {
		return nil, fmt.Errorf("failed to create global file charset: %w", err)
	}

	debug.Log("Created global file charset", map[string]interface{}{
		"id":         created.ID,
		"name":       created.Name,
		"byte_count": fileInfo.ByteCount,
		"file_path":  relFilePath,
		"created_by": createdBy,
	})

	return created, nil
}

// CreateUserFileCharset creates a personal file charset for a specific user.
func (s *CustomCharsetService) CreateUserFileCharset(ctx context.Context, userID uuid.UUID, name, description string, fileInfo *HCCHRFileInfo, relFilePath string) (*models.CustomCharset, error) {
	charset := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CharsetType: models.CustomCharsetTypeFile,
		FilePath:    &relFilePath,
		FileMD5:     &fileInfo.MD5,
		FileSize:    &fileInfo.FileSize,
		ByteCount:   &fileInfo.ByteCount,
		Scope:       models.CustomCharsetScopeUser,
		OwnerID:     &userID,
		CreatedBy:   &userID,
	}

	created, err := s.charsetRepo.Create(ctx, charset)
	if err != nil {
		return nil, fmt.Errorf("failed to create user file charset: %w", err)
	}

	debug.Log("Created user file charset", map[string]interface{}{
		"id":         created.ID,
		"name":       created.Name,
		"byte_count": fileInfo.ByteCount,
		"user_id":    userID,
	})

	return created, nil
}

// --- Update and Delete ---

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

	// For file charsets, definition cannot be changed (only name/description)
	var defPtr *string
	if existing.CharsetType == models.CustomCharsetTypeFile {
		defPtr = existing.Definition // Keep original nil
	} else {
		if definition != "" {
			if err := s.validateCharsetDefinition(definition); err != nil {
				return nil, err
			}
		}
		defPtr = &definition
	}

	update := &models.CustomCharset{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Definition:  defPtr,
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
// For file charsets, also deletes the physical file from disk.
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

	// Clean up physical file for file charsets
	if existing.CharsetType == models.CustomCharsetTypeFile && existing.FilePath != nil {
		absPath := filepath.Join(s.dataDir, *existing.FilePath)
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			debug.Warning("Failed to delete charset file %s: %v", absPath, err)
			// Don't fail the operation — DB record is already deleted
		} else if err == nil {
			debug.Info("Deleted charset file: %s", absPath)
		}
	}

	debug.Log("Deleted custom charset", map[string]interface{}{
		"id":      id,
		"scope":   existing.Scope,
		"type":    existing.CharsetType,
		"user_id": userID,
	})

	return nil
}

// --- DES charset seeding ---

// SeedDESCharset creates the DES_full.hcchr file on disk and ensures the DB record exists.
// Called on backend startup. The file contains all 256 byte values (0x00-0xFF).
func (s *CustomCharsetService) SeedDESCharset(ctx context.Context) error {
	relPath := "charsets/des_full.hcchr"
	absPath := filepath.Join(s.dataDir, relPath)

	// Check if file already exists with correct content
	if info, err := os.Stat(absPath); err == nil && info.Size() == 256 {
		debug.Info("DES_full.hcchr already exists at %s", absPath)
		return nil
	}

	// Create the directory if needed
	if err := os.MkdirAll(filepath.Dir(absPath), 0750); err != nil {
		return fmt.Errorf("failed to create charsets directory: %w", err)
	}

	// Generate all 256 byte values
	data := make([]byte, 256)
	for i := 0; i < 256; i++ {
		data[i] = byte(i)
	}

	// Write the file
	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write DES_full.hcchr: %w", err)
	}

	debug.Info("Created DES_full.hcchr at %s (256 bytes)", absPath)
	return nil
}

// --- Validation ---

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

// validateHexCharsetDefinition checks that a charset definition is valid hex byte pairs.
// Valid: only hex characters [0-9a-fA-F], even length (each pair = one byte).
func (s *CustomCharsetService) validateHexCharsetDefinition(definition string) error {
	if definition == "" {
		return fmt.Errorf("hex charset definition cannot be empty")
	}
	if len(definition)%2 != 0 {
		return fmt.Errorf("hex charset definition must have even length (byte pairs), got %d characters", len(definition))
	}
	for i, c := range definition {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("hex charset definition contains non-hex character %q at position %d", string(c), i)
		}
	}
	return nil
}

// --- File path helper ---

// SaveUploadedCharsetFile saves an uploaded file to the charsets directory and returns the relative path.
// The file is saved as {uuid}.hcchr to avoid name collisions.
func (s *CustomCharsetService) SaveUploadedCharsetFile(src io.Reader) (relPath string, absPath string, err error) {
	fileID := uuid.New()
	filename := fileID.String() + ".hcchr"
	relPath = filepath.Join("charsets", filename)
	absPath = filepath.Join(s.dataDir, relPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(absPath), 0750); err != nil {
		return "", "", fmt.Errorf("failed to create charsets directory: %w", err)
	}

	// Write the file
	dst, err := os.Create(absPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create charset file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(absPath) // Cleanup on error
		return "", "", fmt.Errorf("failed to write charset file: %w", err)
	}

	return relPath, absPath, nil
}
