package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/google/uuid"
	"time"
)

// ClientPotfileService provides client potfile operations.
// NOTE: Staging and background processing have been moved to the unified PotfileService.
// This service is now a thin compatibility wrapper that delegates most operations to PotfileService.
type ClientPotfileService struct {
	dataDir           string
	clientPotfileRepo *repository.ClientPotfileRepository
	potfileService    *PotfileService // Reference to unified PotfileService for delegation
}

// clientBloomEntry holds a bloom filter and its last access time for LRU eviction.
// Shared with PotfileService.
type clientBloomEntry struct {
	filter     *bloom.BloomFilter
	lastAccess time.Time
}

// NewClientPotfileService creates a new client potfile service.
func NewClientPotfileService(
	dataDir string,
	clientPotfileRepo *repository.ClientPotfileRepository,
	potfileService *PotfileService,
) *ClientPotfileService {
	return &ClientPotfileService{
		dataDir:           dataDir,
		clientPotfileRepo: clientPotfileRepo,
		potfileService:    potfileService,
	}
}

// Start initializes the client potfile service.
// NOTE: Background processing is now handled by the unified PotfileService.
func (s *ClientPotfileService) Start(ctx context.Context) error {
	debug.Info("Starting client potfile service (staging delegated to unified PotfileService)...")

	// Ensure base directory exists
	baseDir := s.getBaseDir()
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create client potfiles directory: %w", err)
	}

	debug.Info("Client potfile service started")
	return nil
}

// Stop is a no-op since background processing is now in PotfileService.
func (s *ClientPotfileService) Stop() {
	debug.Info("Client potfile service stopped")
}

// GetClientPotfilePath returns the path to a client's potfile.
func (s *ClientPotfileService) GetClientPotfilePath(clientID uuid.UUID) string {
	if s.potfileService != nil {
		return s.potfileService.GetClientPotfilePath(clientID)
	}
	return filepath.Join(s.getBaseDir(), clientID.String(), "potfile.txt")
}

// getBaseDir returns the base directory for client files (potfiles and wordlists).
func (s *ClientPotfileService) getBaseDir() string {
	return filepath.Join(s.dataDir, "wordlists", "clients")
}

// GetClientPotfileInfo returns information about a client's potfile.
func (s *ClientPotfileService) GetClientPotfileInfo(ctx context.Context, clientID uuid.UUID) (*models.ClientPotfile, error) {
	if s.potfileService != nil {
		return s.potfileService.GetClientPotfileInfo(ctx, clientID)
	}
	if s.clientPotfileRepo != nil {
		return s.clientPotfileRepo.GetByClientID(ctx, clientID)
	}
	return nil, fmt.Errorf("no repository configured")
}

// DeleteClientPotfile removes a client's potfile and associated data.
func (s *ClientPotfileService) DeleteClientPotfile(ctx context.Context, clientID uuid.UUID) error {
	if s.potfileService != nil {
		return s.potfileService.DeleteClientPotfile(ctx, clientID)
	}

	// Fallback implementation
	if s.clientPotfileRepo != nil {
		if err := s.clientPotfileRepo.Delete(ctx, clientID); err != nil {
			return fmt.Errorf("failed to delete potfile record: %w", err)
		}
	}

	// Remove file
	potfilePath := s.GetClientPotfilePath(clientID)
	if err := os.RemoveAll(filepath.Dir(potfilePath)); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to remove potfile directory: %v", err)
	}

	debug.Info("Deleted potfile for client %s", clientID)
	return nil
}

// RegenerateClientPotfile rebuilds a client's potfile from database plaintexts.
// This is called when a hashlist is deleted and password removal is requested.
func (s *ClientPotfileService) RegenerateClientPotfile(ctx context.Context, clientID uuid.UUID) error {
	if s.potfileService != nil {
		return s.potfileService.RegenerateClientPotfile(ctx, clientID)
	}
	return fmt.Errorf("potfile service not configured for regeneration")
}
