package services

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// HashlistSyncService handles hashlist distribution and cleanup for agents
type HashlistSyncService struct {
	agentHashlistRepo  *repository.AgentHashlistRepository
	hashlistRepo       *repository.HashListRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	jobExecutionRepo   *repository.JobExecutionRepository
	dataDirectory      string
	db                 *sql.DB
}

// NewHashlistSyncService creates a new hashlist sync service
func NewHashlistSyncService(
	agentHashlistRepo *repository.AgentHashlistRepository,
	hashlistRepo *repository.HashListRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	jobExecutionRepo *repository.JobExecutionRepository,
	dataDirectory string,
	sqlDB *sql.DB,
) *HashlistSyncService {
	return &HashlistSyncService{
		agentHashlistRepo:  agentHashlistRepo,
		hashlistRepo:       hashlistRepo,
		systemSettingsRepo: systemSettingsRepo,
		jobExecutionRepo:   jobExecutionRepo,
		dataDirectory:      dataDirectory,
		db:                 sqlDB,
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

	// Hashlists are now generated on-demand from the database, not static files
	// Agents download them directly via /api/agent/hashlists/{id}/download
	// We use an empty hash to signal dynamic content (no MD5 verification needed)
	currentFileHash := ""

	// Check if agent already has a hashlist record
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

		debug.Log("Agent already has hashlist metadata", map[string]interface{}{
			"agent_id":    agentID,
			"hashlist_id": hashlistID,
		})
		return nil
	}

	// Create or update agent hashlist record
	// Agent will download the actual content on-demand when needed
	targetFilePath := fmt.Sprintf("hashlists/%d.hash", hashlistID)
	agentHashlist := &models.AgentHashlist{
		AgentID:    agentID,
		HashlistID: hashlistID,
		FilePath:   targetFilePath,
		FileHash:   &currentFileHash, // Empty hash = dynamic content, no verification
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
	hashlistFilePath := filepath.Join(s.dataDirectory, "hashlists", fmt.Sprintf("%d.hash", hashlistID))
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
	targetFilePath := fmt.Sprintf("hashlists/%d.hash", hashlistID)

	result := &HashlistSyncResult{
		SyncRequired:   syncRequired,
		FilePath:       targetFilePath,
		FileHash:       fileHash,
		FileSize:       fileInfo.Size(),
		UpdateRequired: syncRequired,
	}

	debug.Log("Hashlist sync info", map[string]interface{}{
		"agent_id":      agentID,
		"hashlist_id":   hashlistID,
		"hashlist_name": hashlist.Name,
		"sync_required": syncRequired,
		"file_size":     fileInfo.Size(),
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
			"count":           len(oldHashlists),
			"retention_hours": retentionHours,
		})

		// Log details of cleaned up hashlists
		for _, hashlist := range oldHashlists {
			debug.Log("Cleaned up hashlist", map[string]interface{}{
				"agent_id":     hashlist.AgentID,
				"hashlist_id":  hashlist.HashlistID,
				"file_path":    hashlist.FilePath,
				"last_used_at": hashlist.LastUsedAt,
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

// SyncJobFiles synchronizes all required files for a job task to an agent
func (s *HashlistSyncService) SyncJobFiles(ctx context.Context, agentID int, task *models.JobTask) error {
	debug.Log("Syncing job files to agent", map[string]interface{}{
		"agent_id":           agentID,
		"task_id":            task.ID,
		"is_rule_split_task": task.IsRuleSplitTask,
	})

	// First sync the hashlist
	jobExecution, err := s.getJobExecution(ctx, task.JobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	err = s.EnsureHashlistOnAgent(ctx, agentID, jobExecution.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to sync hashlist: %w", err)
	}

	// If this is a rule split task, sync the rule chunk
	if task.IsRuleSplitTask && task.RuleChunkPath != nil && *task.RuleChunkPath != "" {
		err = s.syncRuleChunk(ctx, agentID, task)
		if err != nil {
			return fmt.Errorf("failed to sync rule chunk: %w", err)
		}
	}

	return nil
}

// syncRuleChunk synchronizes a rule chunk file to an agent
func (s *HashlistSyncService) syncRuleChunk(ctx context.Context, agentID int, task *models.JobTask) error {
	if task.RuleChunkPath == nil || *task.RuleChunkPath == "" {
		return nil
	}

	debug.Log("Syncing rule chunk to agent", map[string]interface{}{
		"agent_id":        agentID,
		"task_id":         task.ID,
		"rule_chunk_path": *task.RuleChunkPath,
	})

	// Extract job ID from the rule chunk path
	// Path format: /path/to/temp/rule_chunks/job_<ID>/chunk_<N>.rule
	pathParts := strings.Split(*task.RuleChunkPath, string(filepath.Separator))
	var jobDirName string
	chunkFilename := filepath.Base(*task.RuleChunkPath)

	// Find the job directory name
	for i, part := range pathParts {
		if strings.HasPrefix(part, "job_") && i < len(pathParts)-1 {
			jobDirName = part
			break
		}
	}

	// Create target path with job directory to avoid conflicts
	var targetPath string
	if jobDirName != "" {
		targetPath = fmt.Sprintf("rules/chunks/%s/%s", jobDirName, chunkFilename)
	} else {
		// Fallback to just chunk filename
		targetPath = fmt.Sprintf("rules/chunks/%s", chunkFilename)
	}

	// Calculate file hash
	fileHash, err := s.calculateFileHash(*task.RuleChunkPath)
	if err != nil {
		return fmt.Errorf("failed to calculate rule chunk hash: %w", err)
	}

	// Get file info
	fileInfo, err := os.Stat(*task.RuleChunkPath)
	if err != nil {
		return fmt.Errorf("failed to get rule chunk file info: %w", err)
	}

	debug.Log("Rule chunk sync info", map[string]interface{}{
		"agent_id":    agentID,
		"source_path": *task.RuleChunkPath,
		"target_path": targetPath,
		"file_size":   fileInfo.Size(),
		"file_hash":   fileHash,
		"job_dir":     jobDirName,
	})

	// The actual file transfer will be notified through the job assignment
	// The agent will download the chunk when it receives the task with the chunk path
	// We've already updated the task.RuleChunkPath to include the proper path for download

	return nil
}

// CleanupTaskRuleChunks removes rule chunk files for a completed/failed task
func (s *HashlistSyncService) CleanupTaskRuleChunks(ctx context.Context, task *models.JobTask, agentID int) error {
	if !task.IsRuleSplitTask || task.RuleChunkPath == nil || *task.RuleChunkPath == "" {
		return nil
	}

	debug.Log("Cleaning up rule chunks for task", map[string]interface{}{
		"task_id":         task.ID,
		"agent_id":        agentID,
		"rule_chunk_path": *task.RuleChunkPath,
	})

	// Remove chunk file from server
	if err := os.Remove(*task.RuleChunkPath); err != nil && !os.IsNotExist(err) {
		debug.Error("Failed to remove rule chunk from server: %v", err)
	}

	// Note: Agent cleanup would need to be handled via WebSocket message
	// to instruct the agent to remove the file

	return nil
}

// getJobExecution helper to fetch job execution details
func (s *HashlistSyncService) getJobExecution(ctx context.Context, jobExecutionID uuid.UUID) (*models.JobExecution, error) {
	return s.jobExecutionRepo.GetByID(ctx, jobExecutionID)
}
