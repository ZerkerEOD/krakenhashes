package services

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)



// HashlistSyncService handles hashlist distribution and cleanup for agents
type HashlistSyncService struct {
	agentHashlistRepo  *repository.AgentHashlistRepository
	hashlistRepo       *repository.HashListRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	dataDirectory      string
}

// NewHashlistSyncService creates a new hashlist sync service
func NewHashlistSyncService(
	agentHashlistRepo *repository.AgentHashlistRepository,
	hashlistRepo *repository.HashListRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	dataDirectory string,
) *HashlistSyncService {
	return &HashlistSyncService{
		agentHashlistRepo:  agentHashlistRepo,
		hashlistRepo:       hashlistRepo,
		systemSettingsRepo: systemSettingsRepo,
		dataDirectory:      dataDirectory,
	}
}

// HashlistSyncRequest contains information for syncing a hashlist to an agent
type HashlistSyncRequest struct {
	AgentID        int
	HashlistID     int64
	ForceUpdate    bool
	TargetFilePath string // Path where agent should store the file
}

// HashlistSyncResult contains the result of a hashlist sync operation
type HashlistSyncResult struct {
	SyncRequired   bool
	FilePath       string
	FileHash       string
	FileSize       int64
	UpdateRequired bool
}

// EnsureHashlistOnAgent ensures that an agent has the current version of a hashlist
func (s *HashlistSyncService) EnsureHashlistOnAgent(ctx context.Context, agentID int, hashlistID int64) error {
	debug.Log("Ensuring hashlist on agent", map[string]interface{}{
		"agent_id":    agentID,
		"hashlist_id": hashlistID,
	})

	// Get hashlist information
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Get current hashlist file path and hash
	hashlistFilePath := filepath.Join(s.dataDirectory, "hashlists", fmt.Sprintf("%s.hash", hashlistID))
	currentFileHash, err := s.calculateFileHash(hashlistFilePath)
	if err != nil {
		return fmt.Errorf("failed to calculate hashlist file hash: %w", err)
	}

	// Check if agent already has current version
	isCurrentOnAgent, err := s.agentHashlistRepo.IsHashlistCurrentForAgent(ctx, agentID, hashlistID, currentFileHash)
	if err != nil {
		return fmt.Errorf("failed to check hashlist currency on agent: %w", err)
	}

	if isCurrentOnAgent {
		// Update last used timestamp
		err = s.agentHashlistRepo.UpdateLastUsed(ctx, agentID, hashlistID)
		if err != nil {
			debug.Log("Failed to update last used timestamp", map[string]interface{}{
				"agent_id":    agentID,
				"hashlist_id": hashlistID,
				"error":       err.Error(),
			})
		}
		
		debug.Log("Agent already has current hashlist", map[string]interface{}{
			"agent_id":    agentID,
			"hashlist_id": hashlistID,
		})
		return nil
	}

	// Create or update agent hashlist record
	targetFilePath := fmt.Sprintf("hashlists/%s.hash", hashlistID)
	agentHashlist := &models.AgentHashlist{
		AgentID:    agentID,
		HashlistID: hashlistID,
		FilePath:   targetFilePath,
		FileHash:   &currentFileHash,
	}

	err = s.agentHashlistRepo.CreateOrUpdate(ctx, agentHashlist)
	if err != nil {
		return fmt.Errorf("failed to create or update agent hashlist record: %w", err)
	}

	debug.Log("Hashlist sync required for agent", map[string]interface{}{
		"agent_id":         agentID,
		"hashlist_id":      hashlistID,
		"hashlist_name":    hashlist.Name,
		"target_file_path": targetFilePath,
		"file_hash":        currentFileHash,
	})

	// The actual file transfer will be handled by the WebSocket file sync mechanism
	// This service just manages the tracking and ensures the agent knows it needs the file

	return nil
}

// GetHashlistSyncInfo returns information needed for agent to sync a hashlist
func (s *HashlistSyncService) GetHashlistSyncInfo(ctx context.Context, agentID int, hashlistID int64) (*HashlistSyncResult, error) {
	// Get hashlist information
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Get file path and calculate hash
	hashlistFilePath := filepath.Join(s.dataDirectory, "hashlists", fmt.Sprintf("%s.hash", hashlistID))
	fileHash, err := s.calculateFileHash(hashlistFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate file hash: %w", err)
	}

	// Get file size
	fileInfo, err := os.Stat(hashlistFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Check if agent needs update
	isCurrentOnAgent, err := s.agentHashlistRepo.IsHashlistCurrentForAgent(ctx, agentID, hashlistID, fileHash)
	if err != nil {
		return nil, fmt.Errorf("failed to check hashlist currency: %w", err)
	}

	syncRequired := !isCurrentOnAgent
	targetFilePath := fmt.Sprintf("hashlists/%s.hash", hashlistID)

	result := &HashlistSyncResult{
		SyncRequired:   syncRequired,
		FilePath:       targetFilePath,
		FileHash:       fileHash,
		FileSize:       fileInfo.Size(),
		UpdateRequired: syncRequired,
	}

	debug.Log("Hashlist sync info", map[string]interface{}{
		"agent_id":        agentID,
		"hashlist_id":     hashlistID,
		"hashlist_name":   hashlist.Name,
		"sync_required":   syncRequired,
		"file_size":       fileInfo.Size(),
	})

	return result, nil
}

// CleanupOldHashlists removes old hashlists from agents based on retention settings
func (s *HashlistSyncService) CleanupOldHashlists(ctx context.Context) error {
	debug.Log("Starting hashlist cleanup", nil)

	// Get retention period setting
	retentionSetting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_hashlist_retention_hours")
	if err != nil {
		return fmt.Errorf("failed to get retention setting: %w", err)
	}

	retentionHours := 24 // Default
	if retentionSetting.Value != nil {
		if parsed, parseErr := strconv.Atoi(*retentionSetting.Value); parseErr == nil {
			retentionHours = parsed
		}
	}

	retentionPeriod := time.Duration(retentionHours) * time.Hour

	// Get old hashlists to cleanup
	oldHashlists, err := s.agentHashlistRepo.CleanupOldHashlists(ctx, retentionPeriod)
	if err != nil {
		return fmt.Errorf("failed to cleanup old hashlists: %w", err)
	}

	if len(oldHashlists) > 0 {
		debug.Log("Cleaned up old hashlists", map[string]interface{}{
			"count":            len(oldHashlists),
			"retention_hours":  retentionHours,
		})

		// Log details of cleaned up hashlists
		for _, hashlist := range oldHashlists {
			debug.Log("Cleaned up hashlist", map[string]interface{}{
				"agent_id":      hashlist.AgentID,
				"hashlist_id":   hashlist.HashlistID,
				"file_path":     hashlist.FilePath,
				"last_used_at":  hashlist.LastUsedAt,
			})
		}
	}

	return nil
}

// CleanupAgentHashlists removes all hashlists for a specific agent (when agent is removed)
func (s *HashlistSyncService) CleanupAgentHashlists(ctx context.Context, agentID int) error {
	debug.Log("Cleaning up hashlists for agent", map[string]interface{}{
		"agent_id": agentID,
	})

	deletedHashlists, err := s.agentHashlistRepo.CleanupAgentHashlists(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to cleanup agent hashlists: %w", err)
	}

	debug.Log("Cleaned up agent hashlists", map[string]interface{}{
		"agent_id": agentID,
		"count":    len(deletedHashlists),
	})

	return nil
}

// GetHashlistDistribution returns which agents have a specific hashlist
func (s *HashlistSyncService) GetHashlistDistribution(ctx context.Context, hashlistID int64) ([]models.AgentHashlist, error) {
	distribution, err := s.agentHashlistRepo.GetHashlistDistribution(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist distribution: %w", err)
	}

	return distribution, nil
}

// GetAgentHashlists returns all hashlists for a specific agent
func (s *HashlistSyncService) GetAgentHashlists(ctx context.Context, agentID int) ([]models.AgentHashlist, error) {
	hashlists, err := s.agentHashlistRepo.GetHashlistsByAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent hashlists: %w", err)
	}

	return hashlists, nil
}

// UpdateHashlistAfterCracks updates the hashlist file after hashes are cracked
func (s *HashlistSyncService) UpdateHashlistAfterCracks(ctx context.Context, hashlistID int64, crackedHashes []string) error {
	debug.Log("Updating hashlist after cracks", map[string]interface{}{
		"hashlist_id":    hashlistID,
		"cracked_count":  len(crackedHashes),
	})

	// This would typically involve:
	// 1. Reading the current hashlist file
	// 2. Removing the cracked hashes
	// 3. Writing the updated file
	// 4. Updating the file hash
	// 5. Marking all agent copies as outdated

	hashlistFilePath := filepath.Join(s.dataDirectory, "hashlists", fmt.Sprintf("%s.hash", hashlistID))
	
	// For now, we'll just recalculate the file hash and mark all agent copies as outdated
	// The actual file update logic would need to be implemented based on the hashlist format
	
	newFileHash, err := s.calculateFileHash(hashlistFilePath)
	if err != nil {
		return fmt.Errorf("failed to calculate updated file hash: %w", err)
	}

	// Get all agents that have this hashlist
	distribution, err := s.agentHashlistRepo.GetHashlistDistribution(ctx, hashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist distribution: %w", err)
	}

	// Update file hash for all agents (this will mark them as needing updates)
	for _, agentHashlist := range distribution {
		agentHashlist.FileHash = &newFileHash
		err = s.agentHashlistRepo.CreateOrUpdate(ctx, &agentHashlist)
		if err != nil {
			debug.Log("Failed to update agent hashlist hash", map[string]interface{}{
				"agent_id":    agentHashlist.AgentID,
				"hashlist_id": hashlistID,
				"error":       err.Error(),
			})
		}
	}

	debug.Log("Hashlist updated after cracks", map[string]interface{}{
		"hashlist_id":      hashlistID,
		"new_file_hash":    newFileHash,
		"affected_agents":  len(distribution),
	})

	return nil
}

// StartHashlistCleanupScheduler starts the periodic hashlist cleanup
func (s *HashlistSyncService) StartHashlistCleanupScheduler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	debug.Log("Hashlist cleanup scheduler started", map[string]interface{}{
		"interval": interval,
	})

	for {
		select {
		case <-ctx.Done():
			debug.Log("Hashlist cleanup scheduler stopped", nil)
			return
		case <-ticker.C:
			err := s.CleanupOldHashlists(ctx)
			if err != nil {
				debug.Log("Hashlist cleanup failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}
}

// calculateFileHash calculates MD5 hash of a file
func (s *HashlistSyncService) calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}