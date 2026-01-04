package wordlist

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/fsutil"
	"github.com/google/uuid"
)

// Manager handles wordlist operations
type Manager interface {
	ListWordlists(ctx context.Context, filters map[string]interface{}) ([]*models.Wordlist, error)
	GetWordlist(ctx context.Context, id int) (*models.Wordlist, error)
	GetWordlistByFilename(ctx context.Context, filename string) (*models.Wordlist, error)
	GetWordlistByMD5Hash(ctx context.Context, md5Hash string) (*models.Wordlist, error)
	AddWordlist(ctx context.Context, req *models.WordlistAddRequest, userID uuid.UUID) (*models.Wordlist, error)
	UpdateWordlist(ctx context.Context, id int, req *models.WordlistUpdateRequest, userID uuid.UUID) (*models.Wordlist, error)
	DeleteWordlist(ctx context.Context, id int, confirmID *int) error
	GetDeletionImpact(ctx context.Context, id int) (*models.DeletionImpact, error)
	VerifyWordlist(ctx context.Context, id int, req *models.WordlistVerifyRequest) error
	UpdateWordlistFileInfo(ctx context.Context, id int, md5Hash string, fileSize int64) error
	UpdateWordlistComplete(ctx context.Context, id int, md5Hash string, fileSize int64, wordCount int64) error
	AddWordlistTag(ctx context.Context, id int, tag string, userID uuid.UUID) error
	DeleteWordlistTag(ctx context.Context, id int, tag string) error
	GetWordlistPath(filename string, wordlistType string) string
	CountWordsInFile(filepath string) (int64, error)
	CalculateFileMD5(filepath string) (string, error)
}

// Store defines the interface for wordlist data storage operations
type WordlistStore interface {
	// Wordlist operations
	ListWordlists(ctx context.Context, filters map[string]interface{}) ([]*models.Wordlist, error)
	GetWordlist(ctx context.Context, id int) (*models.Wordlist, error)
	GetWordlistByFilename(ctx context.Context, filename string) (*models.Wordlist, error)
	GetWordlistByMD5Hash(ctx context.Context, md5Hash string) (*models.Wordlist, error)
	CreateWordlist(ctx context.Context, wordlist *models.Wordlist) error
	UpdateWordlist(ctx context.Context, wordlist *models.Wordlist) error
	DeleteWordlist(ctx context.Context, id int) error
	UpdateWordlistVerification(ctx context.Context, id int, status string, wordCount *int64) error
	UpdateWordlistFileInfo(ctx context.Context, id int, md5Hash string, fileSize int64) error
	UpdateWordlistComplete(ctx context.Context, id int, md5Hash string, fileSize int64, wordCount int64) error

	// Tag operations
	GetWordlistTags(ctx context.Context, id int) ([]string, error)
	AddWordlistTag(ctx context.Context, id int, tag string, userID uuid.UUID) error
	DeleteWordlistTag(ctx context.Context, id int, tag string) error
}

type manager struct {
	store            WordlistStore
	wordlistsDir     string
	maxUploadSize    int64
	allowedFormats   []string
	allowedMimeTypes []string
	jobExecRepo      *repository.JobExecutionRepository
	presetJobRepo    repository.PresetJobRepository
	workflowRepo     repository.JobWorkflowRepository
}

// NewManager creates a new wordlist manager
func NewManager(store WordlistStore, wordlistsDir string, maxUploadSize int64, allowedFormats, allowedMimeTypes []string, jobExecRepo *repository.JobExecutionRepository, presetJobRepo repository.PresetJobRepository, workflowRepo repository.JobWorkflowRepository) Manager {
	// Ensure wordlists directory exists
	if err := os.MkdirAll(wordlistsDir, 0755); err != nil {
		debug.Error("Failed to create wordlists directory: %v", err)
		panic(err)
	}

	return &manager{
		store:            store,
		wordlistsDir:     wordlistsDir,
		maxUploadSize:    maxUploadSize,
		allowedFormats:   allowedFormats,
		allowedMimeTypes: allowedMimeTypes,
		jobExecRepo:      jobExecRepo,
		presetJobRepo:    presetJobRepo,
		workflowRepo:     workflowRepo,
	}
}

// ListWordlists retrieves all wordlists with optional filtering
func (m *manager) ListWordlists(ctx context.Context, filters map[string]interface{}) ([]*models.Wordlist, error) {
	return m.store.ListWordlists(ctx, filters)
}

// GetWordlist retrieves a wordlist by ID
func (m *manager) GetWordlist(ctx context.Context, id int) (*models.Wordlist, error) {
	return m.store.GetWordlist(ctx, id)
}

// GetWordlistByFilename retrieves a wordlist by filename
func (m *manager) GetWordlistByFilename(ctx context.Context, filename string) (*models.Wordlist, error) {
	return m.store.GetWordlistByFilename(ctx, filename)
}

// GetWordlistByMD5Hash retrieves a wordlist by MD5 hash
func (m *manager) GetWordlistByMD5Hash(ctx context.Context, md5Hash string) (*models.Wordlist, error) {
	return m.store.GetWordlistByMD5Hash(ctx, md5Hash)
}

// AddWordlist adds a new wordlist
func (m *manager) AddWordlist(ctx context.Context, req *models.WordlistAddRequest, userID uuid.UUID) (*models.Wordlist, error) {
	// Create wordlist model
	wordlist := &models.Wordlist{
		Name:               req.Name,
		Description:        req.Description,
		WordlistType:       req.WordlistType,
		Format:             req.Format,
		FileName:           req.FileName,
		MD5Hash:            req.MD5Hash,
		FileSize:           req.FileSize,
		WordCount:          req.WordCount,
		CreatedBy:          userID,
		VerificationStatus: "pending",
		Tags:               req.Tags,
	}

	// Create wordlist in database
	if err := m.store.CreateWordlist(ctx, wordlist); err != nil {
		return nil, err
	}

	return wordlist, nil
}

// UpdateWordlist updates an existing wordlist
func (m *manager) UpdateWordlist(ctx context.Context, id int, req *models.WordlistUpdateRequest, userID uuid.UUID) (*models.Wordlist, error) {
	// Get existing wordlist
	wordlist, err := m.store.GetWordlist(ctx, id)
	if err != nil {
		return nil, err
	}
	if wordlist == nil {
		return nil, fmt.Errorf("wordlist not found")
	}

	// Update fields
	wordlist.Name = req.Name
	wordlist.Description = req.Description
	wordlist.WordlistType = req.WordlistType

	// Only update format if provided
	if req.Format != "" {
		wordlist.Format = req.Format
	}

	wordlist.UpdatedBy = userID

	// Update in database
	if err := m.store.UpdateWordlist(ctx, wordlist); err != nil {
		return nil, err
	}

	// Handle tags
	if req.Tags != nil {
		// Get current tags
		currentTags, err := m.store.GetWordlistTags(ctx, id)
		if err != nil {
			return nil, err
		}

		// Add new tags
		for _, tag := range req.Tags {
			found := false
			for _, currentTag := range currentTags {
				if tag == currentTag {
					found = true
					break
				}
			}
			if !found {
				if err := m.store.AddWordlistTag(ctx, id, tag, userID); err != nil {
					return nil, err
				}
			}
		}

		// Remove tags that are no longer present
		for _, currentTag := range currentTags {
			found := false
			for _, tag := range req.Tags {
				if currentTag == tag {
					found = true
					break
				}
			}
			if !found {
				if err := m.store.DeleteWordlistTag(ctx, id, currentTag); err != nil {
					return nil, err
				}
			}
		}

		// Update tags in wordlist object
		wordlist.Tags = req.Tags
	}

	return wordlist, nil
}

// GetDeletionImpact returns the impact of deleting a wordlist
func (m *manager) GetDeletionImpact(ctx context.Context, id int) (*models.DeletionImpact, error) {
	// Check if wordlist exists
	wordlist, err := m.store.GetWordlist(ctx, id)
	if err != nil {
		return nil, err
	}
	if wordlist == nil {
		return nil, fmt.Errorf("wordlist not found")
	}

	impact := &models.DeletionImpact{
		ResourceID:   id,
		ResourceType: "wordlist",
		CanDelete:    true,
		Impact: models.DeletionImpactDetails{
			Jobs:              []models.DeletionImpactJob{},
			PresetJobs:        []models.DeletionImpactPresetJob{},
			WorkflowSteps:     []models.DeletionImpactWorkflowStep{},
			WorkflowsToDelete: []models.DeletionImpactWorkflow{},
		},
	}

	wordlistIDStr := strconv.Itoa(id)

	// Get non-completed jobs using this wordlist
	if m.jobExecRepo != nil {
		jobs, err := m.jobExecRepo.GetNonCompletedJobsUsingWordlist(ctx, wordlistIDStr)
		if err != nil {
			debug.Error("Failed to get jobs using wordlist %d: %v", id, err)
			return nil, fmt.Errorf("failed to get jobs using wordlist: %w", err)
		}
		impact.Impact.Jobs = jobs
	}

	// Get preset jobs using this wordlist
	if m.presetJobRepo != nil {
		presetJobs, err := m.presetJobRepo.GetByWordlistID(ctx, wordlistIDStr)
		if err != nil {
			debug.Error("Failed to get preset jobs using wordlist %d: %v", id, err)
			return nil, fmt.Errorf("failed to get preset jobs using wordlist: %w", err)
		}
		for _, pj := range presetJobs {
			impact.Impact.PresetJobs = append(impact.Impact.PresetJobs, models.DeletionImpactPresetJob{
				ID:         pj.ID,
				Name:       pj.Name,
				AttackMode: strconv.Itoa(int(pj.AttackMode)),
			})
		}

		// Get workflow steps that use these preset jobs
		if m.workflowRepo != nil && len(presetJobs) > 0 {
			presetJobIDs := make([]uuid.UUID, len(presetJobs))
			for i, pj := range presetJobs {
				presetJobIDs[i] = pj.ID
			}

			steps, err := m.workflowRepo.GetStepsByPresetJobIDs(ctx, presetJobIDs)
			if err != nil {
				debug.Error("Failed to get workflow steps: %v", err)
				return nil, fmt.Errorf("failed to get workflow steps: %w", err)
			}
			impact.Impact.WorkflowSteps = steps

			// Get workflows that would become empty
			workflowsToDelete, err := m.workflowRepo.GetWorkflowsAffectedByPresetJobDeletion(ctx, presetJobIDs)
			if err != nil {
				debug.Error("Failed to get affected workflows: %v", err)
				return nil, fmt.Errorf("failed to get affected workflows: %w", err)
			}
			impact.Impact.WorkflowsToDelete = workflowsToDelete
		}
	}

	// Calculate summary
	impact.Summary = models.DeletionImpactSummary{
		TotalJobs:              len(impact.Impact.Jobs),
		TotalPresetJobs:        len(impact.Impact.PresetJobs),
		TotalWorkflowSteps:     len(impact.Impact.WorkflowSteps),
		TotalWorkflowsToDelete: len(impact.Impact.WorkflowsToDelete),
	}

	// Has cascading impact if any references exist
	impact.HasCascadingImpact = impact.Summary.TotalJobs > 0 ||
		impact.Summary.TotalPresetJobs > 0 ||
		impact.Summary.TotalWorkflowSteps > 0

	return impact, nil
}

// DeleteWordlist deletes a wordlist with optional cascade deletion
func (m *manager) DeleteWordlist(ctx context.Context, id int, confirmID *int) error {
	// Get deletion impact first
	impact, err := m.GetDeletionImpact(ctx, id)
	if err != nil {
		return err
	}

	// If there's cascading impact, require confirmation
	if impact.HasCascadingImpact {
		if confirmID == nil || *confirmID != id {
			return models.ErrResourceInUse
		}

		// Perform cascade deletion
		debug.Info("Starting cascade deletion for wordlist %d", id)

		// 1. Delete non-completed jobs using this wordlist
		if len(impact.Impact.Jobs) > 0 && m.jobExecRepo != nil {
			jobIDs := make([]uuid.UUID, len(impact.Impact.Jobs))
			for i, job := range impact.Impact.Jobs {
				jobIDs[i] = job.ID
			}
			if err := m.jobExecRepo.DeleteJobsByIDs(ctx, jobIDs); err != nil {
				debug.Error("Failed to delete jobs: %v", err)
				return fmt.Errorf("failed to delete jobs: %w", err)
			}
			debug.Info("Deleted %d jobs using wordlist %d", len(jobIDs), id)
		}

		// 2. Delete workflow steps referencing affected preset jobs
		if len(impact.Impact.PresetJobs) > 0 && m.workflowRepo != nil {
			presetJobIDs := make([]uuid.UUID, len(impact.Impact.PresetJobs))
			for i, pj := range impact.Impact.PresetJobs {
				presetJobIDs[i] = pj.ID
			}
			if err := m.workflowRepo.DeleteStepsByPresetJobIDs(ctx, presetJobIDs); err != nil {
				debug.Error("Failed to delete workflow steps: %v", err)
				return fmt.Errorf("failed to delete workflow steps: %w", err)
			}
			debug.Info("Deleted workflow steps for %d preset jobs", len(presetJobIDs))

			// 3. Delete preset jobs
			if m.presetJobRepo != nil {
				if err := m.presetJobRepo.DeleteByIDs(ctx, presetJobIDs); err != nil {
					debug.Error("Failed to delete preset jobs: %v", err)
					return fmt.Errorf("failed to delete preset jobs: %w", err)
				}
				debug.Info("Deleted %d preset jobs using wordlist %d", len(presetJobIDs), id)
			}

			// 4. Delete empty workflows
			if deletedCount, err := m.workflowRepo.DeleteEmptyWorkflows(ctx); err != nil {
				debug.Error("Failed to delete empty workflows: %v", err)
				return fmt.Errorf("failed to delete empty workflows: %w", err)
			} else if deletedCount > 0 {
				debug.Info("Deleted %d empty workflows", deletedCount)
			}
		}
	}

	// Get wordlist to find filename
	wordlist, err := m.store.GetWordlist(ctx, id)
	if err != nil {
		return err
	}
	if wordlist == nil {
		return fmt.Errorf("wordlist not found")
	}

	// Delete from database
	if err := m.store.DeleteWordlist(ctx, id); err != nil {
		return err
	}

	// Delete file
	filePath := filepath.Join(m.wordlistsDir, wordlist.FileName)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		debug.Error("Failed to delete wordlist file %s: %v", filePath, err)
		// Don't return error, as the database entry is already deleted
	}

	debug.Info("Successfully deleted wordlist %d", id)
	return nil
}

// VerifyWordlist updates a wordlist's verification status
func (m *manager) VerifyWordlist(ctx context.Context, id int, req *models.WordlistVerifyRequest) error {
	// Get wordlist
	wordlist, err := m.store.GetWordlist(ctx, id)
	if err != nil {
		return err
	}
	if wordlist == nil {
		return fmt.Errorf("wordlist not found")
	}

	// If status is "verified" and word count is not provided, calculate it
	if req.Status == "verified" && req.WordCount == nil {
		filePath := filepath.Join(m.wordlistsDir, wordlist.FileName)
		wordCount, err := m.CountWordsInFile(filePath)
		if err != nil {
			debug.Error("Failed to count words in file %s: %v", filePath, err)
			return err
		}
		req.WordCount = &wordCount
	}

	// Update verification status
	return m.store.UpdateWordlistVerification(ctx, id, req.Status, req.WordCount)
}

// UpdateWordlistFileInfo updates a wordlist's file information (MD5 hash and file size)
func (m *manager) UpdateWordlistFileInfo(ctx context.Context, id int, md5Hash string, fileSize int64) error {
	return m.store.UpdateWordlistFileInfo(ctx, id, md5Hash, fileSize)
}

// UpdateWordlistComplete updates a wordlist's complete file information (MD5 hash, file size, and word count)
func (m *manager) UpdateWordlistComplete(ctx context.Context, id int, md5Hash string, fileSize int64, wordCount int64) error {
	return m.store.UpdateWordlistComplete(ctx, id, md5Hash, fileSize, wordCount)
}

// AddWordlistTag adds a tag to a wordlist
func (m *manager) AddWordlistTag(ctx context.Context, id int, tag string, userID uuid.UUID) error {
	return m.store.AddWordlistTag(ctx, id, tag, userID)
}

// DeleteWordlistTag deletes a tag from a wordlist
func (m *manager) DeleteWordlistTag(ctx context.Context, id int, tag string) error {
	return m.store.DeleteWordlistTag(ctx, id, tag)
}

// GetWordlistPath returns the full path to a wordlist file
func (m *manager) GetWordlistPath(filename string, wordlistType string) string {
	// Check if the filename already contains a subdirectory
	if strings.Contains(filename, string(filepath.Separator)) {
		return filepath.Join(m.wordlistsDir, filename)
	}

	// If no wordlist type is provided, use a default
	if wordlistType == "" {
		wordlistType = "general" // Default type
	} else {
		// Normalize wordlist type
		wordlistType = strings.ToLower(wordlistType)
		// Ensure it's one of the valid types
		switch wordlistType {
		case "general", "specialized", "targeted", "custom":
			// Valid type, use as is
		default:
			// Invalid type, use default
			wordlistType = "general"
		}
	}

	// Place in appropriate subdirectory
	return filepath.Join(m.wordlistsDir, wordlistType, filename)
}

// CountWordsInFile counts the number of words in a file
func (m *manager) CountWordsInFile(filepath string) (int64, error) {
	debug.Info("CountWordsInFile: Starting word count for file: %s", filepath)

	// Get file info for size
	fileInfo, err := os.Stat(filepath)
	if err != nil {
		debug.Error("CountWordsInFile: Failed to get file info: %v", err)
		return 0, err
	}

	// Check if the file is compressed and use streaming decompression for accurate count
	ext := strings.ToLower(path.Ext(filepath))
	switch ext {
	case ".gz":
		debug.Info("CountWordsInFile: Detected gzip file, using streaming decompression")
		return countLinesInGzip(filepath)
	case ".zip":
		debug.Info("CountWordsInFile: Detected zip file, using streaming decompression")
		return countLinesInZip(filepath)
	}

	// For large text files (over 1GB), use a more efficient counting method
	if fileInfo.Size() > 1024*1024*1024 {
		debug.Info("CountWordsInFile: Large text file detected (%d bytes), using optimized counting method",
			fileInfo.Size())

		// Use a buffered reader with a large buffer size for better performance
		file, err := os.Open(filepath)
		if err != nil {
			debug.Error("CountWordsInFile: Failed to open file: %v", err)
			return 0, err
		}
		defer file.Close()

		// Use a 16MB buffer for large files
		const bufferSize = 16 * 1024 * 1024
		reader := bufio.NewReaderSize(file, bufferSize)

		var count int64
		var buf [4096]byte

		for {
			c, err := reader.Read(buf[:])
			if err != nil {
				if err == io.EOF {
					break
				}
				debug.Error("CountWordsInFile: Error reading file: %v", err)
				return 0, err
			}

			// Count newlines in the buffer
			for i := 0; i < c; i++ {
				if buf[i] == '\n' {
					count++
				}
			}
		}

		// Add 1 if the file doesn't end with a newline
		if count > 0 {
			lastByte := make([]byte, 1)
			if _, err := file.ReadAt(lastByte, fileInfo.Size()-1); err == nil {
				if lastByte[0] != '\n' {
					count++
				}
			}
		}

		debug.Info("CountWordsInFile: Counted %d lines in large text file", count)
		return count, nil
	}

	// For regular text files, use the standard line counting method
	debug.Info("CountWordsInFile: Counting lines in text file")
	return fsutil.CountLinesInFile(filepath)
}

// CalculateFileMD5 calculates the MD5 hash of a file
func (m *manager) CalculateFileMD5(filepath string) (string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// countLinesInGzip counts lines in a gzip-compressed file using streaming decompression.
// Uses a 16MB buffer for performance consistency with large uncompressed file handling.
func countLinesInGzip(filePath string) (int64, error) {
	debug.Info("countLinesInGzip: Starting streaming line count for: %s", filePath)

	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("countLinesInGzip: Failed to open file: %v", err)
		return 0, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		debug.Error("countLinesInGzip: Failed to create gzip reader: %v", err)
		return 0, err
	}
	defer gzReader.Close()

	var count int64
	const bufferSize = 16 * 1024 * 1024 // 16MB buffer for performance
	buf := make([]byte, bufferSize)

	for {
		n, err := gzReader.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			debug.Error("countLinesInGzip: Error reading: %v", err)
			return 0, err
		}
	}

	debug.Info("countLinesInGzip: Counted %d lines", count)
	return count, nil
}

// countLinesInZip counts lines in a zip archive containing a wordlist.
// Expects a single text file inside the archive (standard for hashcat wordlists).
// Uses a 16MB buffer for performance consistency with large uncompressed file handling.
func countLinesInZip(filePath string) (int64, error) {
	debug.Info("countLinesInZip: Starting streaming line count for: %s", filePath)

	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		debug.Error("countLinesInZip: Failed to open zip: %v", err)
		return 0, err
	}
	defer zipReader.Close()

	// Find first non-directory file (the wordlist)
	var targetFile *zip.File
	for _, f := range zipReader.File {
		if !f.FileInfo().IsDir() {
			targetFile = f
			break
		}
	}

	if targetFile == nil {
		debug.Error("countLinesInZip: No files found in zip archive")
		return 0, fmt.Errorf("no files found in zip archive")
	}

	debug.Info("countLinesInZip: Reading file from archive: %s", targetFile.Name)

	rc, err := targetFile.Open()
	if err != nil {
		debug.Error("countLinesInZip: Failed to open file in archive: %v", err)
		return 0, err
	}
	defer rc.Close()

	var count int64
	const bufferSize = 16 * 1024 * 1024 // 16MB buffer for performance
	buf := make([]byte, bufferSize)

	for {
		n, err := rc.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			debug.Error("countLinesInZip: Error reading: %v", err)
			return 0, err
		}
	}

	debug.Info("countLinesInZip: Counted %d lines", count)
	return count, nil
}
