package wordlist

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/fsutil"
	"github.com/google/uuid"
)

// EphemeralFilenamePrefix marks the filename of a job-scoped (ephemeral) filtered
// wordlist (GH #40). Agents delete their local copy by this prefix once the owning job
// finishes, and the DirectoryMonitor must skip these files so an orphaned ephemeral file
// is never re-imported as a standalone regular wordlist.
const EphemeralFilenamePrefix = "__eph__"

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

	// Filtered wordlists (GH #40)
	CreateFilteredWordlistRecord(ctx context.Context, parentID int, name, description string, filter models.WordlistFilter, ephemeral bool, ownerJobID *uuid.UUID, userID uuid.UUID) (*models.Wordlist, error)
	GenerateFilteredWordlist(ctx context.Context, wordlistID int) error
	RegenerateFilteredWordlist(ctx context.Context, wordlistID int) error
	RegenerateFilteredWordlistFull(ctx context.Context, wordlistID int) error
	PreviewFilter(ctx context.Context, parentID int, filter models.WordlistFilter, sampleLines int64) (*models.FilterPreviewResponse, error)
	GetFilteredChildren(ctx context.Context, parentID int) ([]*models.Wordlist, error)
	GetEphemeralWordlistsByJob(ctx context.Context, jobID uuid.UUID) ([]*models.Wordlist, error)
	SetWordlistOwnerJob(ctx context.Context, wordlistID int, jobID uuid.UUID) error
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

	// Filtered wordlist operations (GH #40)
	GetFilteredChildren(ctx context.Context, parentID int) ([]*models.Wordlist, error)
	GetEphemeralByJob(ctx context.Context, jobID uuid.UUID) ([]*models.Wordlist, error)
	MarkChildrenStale(ctx context.Context, parentID int, currentParentMD5 string) error
	ClearStale(ctx context.Context, id int) error
	ClearFilteredIndex(ctx context.Context, id int) error
	UpdateFilteredParentMD5(ctx context.Context, id int, parentMD5 string) error
	UpdateFilteredIndex(ctx context.Context, id int, parentMD5 string, parentOffset *int64, anchorMD5 *string) error
	SetWordlistOwnerJob(ctx context.Context, wordlistID int, jobID uuid.UUID) error

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
	if err := m.store.UpdateWordlistFileInfo(ctx, id, md5Hash, fileSize); err != nil {
		return err
	}
	// If this wordlist is a parent of filtered wordlists, flag stale children (GH #40).
	if err := m.store.MarkChildrenStale(ctx, id, md5Hash); err != nil {
		debug.Warning("Failed to flag filtered children stale for wordlist %d: %v", id, err)
	}
	return nil
}

// UpdateWordlistComplete updates a wordlist's complete file information (MD5 hash, file size, and word count)
func (m *manager) UpdateWordlistComplete(ctx context.Context, id int, md5Hash string, fileSize int64, wordCount int64) error {
	if err := m.store.UpdateWordlistComplete(ctx, id, md5Hash, fileSize, wordCount); err != nil {
		return err
	}
	// If this wordlist is a parent of filtered wordlists, flag stale children (GH #40).
	if err := m.store.MarkChildrenStale(ctx, id, md5Hash); err != nil {
		debug.Warning("Failed to flag filtered children stale for wordlist %d: %v", id, err)
	}
	return nil
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

// multiReadCloser couples a reader with the closers that must be released when
// the reader is done (e.g. a gzip reader plus its underlying file).
type multiReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (m *multiReadCloser) Close() error {
	var firstErr error
	// Close in reverse order (innermost reader first).
	for i := len(m.closers) - 1; i >= 0; i-- {
		if err := m.closers[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// openWordlistReader opens a wordlist for streaming reads, transparently
// decompressing .gz and .zip sources. This is the single place archive handling
// lives so counting, filtering, and previewing all read the same way (GH #40).
func openWordlistReader(filePath string) (io.ReadCloser, error) {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".gz":
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, err
		}
		return &multiReadCloser{Reader: gzReader, closers: []io.Closer{file, gzReader}}, nil
	case ".zip":
		zipReader, err := zip.OpenReader(filePath)
		if err != nil {
			return nil, err
		}
		var targetFile *zip.File
		for _, f := range zipReader.File {
			if !f.FileInfo().IsDir() {
				targetFile = f
				break
			}
		}
		if targetFile == nil {
			zipReader.Close()
			return nil, fmt.Errorf("no files found in zip archive")
		}
		rc, err := targetFile.Open()
		if err != nil {
			zipReader.Close()
			return nil, err
		}
		return &multiReadCloser{Reader: rc, closers: []io.Closer{rc, zipReader}}, nil
	default:
		return os.Open(filePath)
	}
}

// countLinesFromReader counts newline-delimited lines from a reader using a
// 16MB buffer for throughput.
func countLinesFromReader(r io.Reader) (int64, error) {
	var count int64
	const bufferSize = 16 * 1024 * 1024
	buf := make([]byte, bufferSize)
	for {
		n, err := r.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

// countLinesInGzip counts lines in a gzip-compressed wordlist.
func countLinesInGzip(filePath string) (int64, error) {
	debug.Info("countLinesInGzip: Starting streaming line count for: %s", filePath)
	reader, err := openWordlistReader(filePath)
	if err != nil {
		debug.Error("countLinesInGzip: Failed to open file: %v", err)
		return 0, err
	}
	defer reader.Close()

	count, err := countLinesFromReader(reader)
	if err != nil {
		debug.Error("countLinesInGzip: Error reading: %v", err)
		return 0, err
	}
	debug.Info("countLinesInGzip: Counted %d lines", count)
	return count, nil
}

// countLinesInZip counts lines in a zip archive containing a wordlist.
// Expects a single text file inside the archive (standard for hashcat wordlists).
func countLinesInZip(filePath string) (int64, error) {
	debug.Info("countLinesInZip: Starting streaming line count for: %s", filePath)
	reader, err := openWordlistReader(filePath)
	if err != nil {
		debug.Error("countLinesInZip: Failed to open zip: %v", err)
		return 0, err
	}
	defer reader.Close()

	count, err := countLinesFromReader(reader)
	if err != nil {
		debug.Error("countLinesInZip: Error reading: %v", err)
		return 0, err
	}
	debug.Info("countLinesInZip: Counted %d lines", count)
	return count, nil
}

// ---------------------------------------------------------------------------
// Wordlist pre-filtering engine (GH #40)
// ---------------------------------------------------------------------------

// compiledFilter is a WordlistFilter prepared for fast per-line evaluation.
type compiledFilter struct {
	minLen     *int
	maxLen     *int
	reqUpper   bool
	reqLower   bool
	reqDigit   bool
	reqSpecial bool
	minClasses *int
	re         *regexp.Regexp
}

func compileFilter(f models.WordlistFilter) (*compiledFilter, error) {
	cf := &compiledFilter{
		minLen:     f.MinLength,
		maxLen:     f.MaxLength,
		reqUpper:   f.RequireUpper,
		reqLower:   f.RequireLower,
		reqDigit:   f.RequireDigit,
		reqSpecial: f.RequireSpecial,
		minClasses: f.MinClasses,
	}
	if f.Regex != "" {
		re, err := regexp.Compile(f.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		cf.re = re
	}
	return cf, nil
}

// isSpecialChar reports whether a rune is a printable ASCII non-alphanumeric
// character (the "special" password class).
func isSpecialChar(r rune) bool {
	return r > 0x20 && r < 0x7f && !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// match reports whether a single line (newline already stripped) passes the filter.
func (c *compiledFilter) match(s string) bool {
	if c.minLen != nil || c.maxLen != nil {
		length := utf8.RuneCountInString(s)
		if c.minLen != nil && length < *c.minLen {
			return false
		}
		if c.maxLen != nil && length > *c.maxLen {
			return false
		}
	}

	if c.reqUpper || c.reqLower || c.reqDigit || c.reqSpecial || c.minClasses != nil {
		var hasU, hasL, hasD, hasS bool
		for _, r := range s {
			switch {
			case unicode.IsUpper(r):
				hasU = true
			case unicode.IsLower(r):
				hasL = true
			case unicode.IsDigit(r):
				hasD = true
			case isSpecialChar(r):
				hasS = true
			}
		}
		if c.reqUpper && !hasU {
			return false
		}
		if c.reqLower && !hasL {
			return false
		}
		if c.reqDigit && !hasD {
			return false
		}
		if c.reqSpecial && !hasS {
			return false
		}
		if c.minClasses != nil {
			n := boolToInt(hasU) + boolToInt(hasL) + boolToInt(hasD) + boolToInt(hasS)
			if n < *c.minClasses {
				return false
			}
		}
	}

	if c.re != nil && !c.re.MatchString(s) {
		return false
	}
	return true
}

// freeDiskSpace returns the bytes available to a non-privileged user on the
// filesystem holding dir. Backend runs on Linux, so syscall.Statfs is used.
func freeDiskSpace(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func isNoSpaceErr(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}

// anchorWindow is the size of the parent tail window whose MD5 is stored as the
// incremental-regeneration anchor (GH #40 follow-up). On a later parent change we
// re-hash the new parent's bytes in this same window; if it still matches and the
// file only grew, the change is an append and we can filter just the new tail.
const anchorWindow int64 = 1 << 20 // 1 MiB

// isCompressedPath reports whether a wordlist file is a compressed archive whose
// decompressed byte offsets are not seekable — such parents always full-rebuild.
func isCompressedPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".gz", ".zip":
		return true
	default:
		return false
	}
}

// FilterWordlist streams srcPath through the filter and materializes matching
// lines (normalized to '\n' line endings) at dstPath. It returns the number of
// matching lines, the MD5 of the produced file, and the source byte offset up to
// the last COMPLETE line consumed (used as the incremental-regeneration index for
// plaintext parents). A partial .tmp is removed on any error, including running
// out of disk space.
func (m *manager) FilterWordlist(ctx context.Context, srcPath, dstPath string, f models.WordlistFilter) (int64, string, int64, error) {
	cf, err := compileFilter(f)
	if err != nil {
		return 0, "", 0, err
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return 0, "", 0, err
	}

	// Best-effort free-space pre-check: require at least the source size free.
	if info, statErr := os.Stat(srcPath); statErr == nil {
		if free, derr := freeDiskSpace(filepath.Dir(dstPath)); derr == nil && free < uint64(info.Size()) {
			return 0, "", 0, fmt.Errorf("insufficient disk space to filter wordlist: need ~%d bytes free, have %d", info.Size(), free)
		}
	}

	reader, err := openWordlistReader(srcPath)
	if err != nil {
		return 0, "", 0, err
	}
	defer reader.Close()

	// Stage to a hidden dotfile (".<name>.tmp") in the same directory so the
	// directory monitor's hidden-file skip ignores the in-progress file and
	// doesn't register it as an orphan wordlist mid-generation (GH #40). Same
	// directory keeps the final os.Rename atomic.
	tmpPath := filepath.Join(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".tmp")
	out, err := os.Create(tmpPath)
	if err != nil {
		return 0, "", 0, err
	}

	hash := md5.New()
	writer := bufio.NewWriterSize(io.MultiWriter(out, hash), 4*1024*1024)
	br := bufio.NewReaderSize(reader, 16*1024*1024)

	var count int64
	// srcBytes tracks every byte consumed from the source. Because the parent is
	// stable when we filter it, the consumed offset is the full byte count (every
	// line read — including a final line with no trailing '\n' — is written), so the
	// child holds exactly the matches for source bytes [0, srcBytes) and the next
	// append resumes cleanly at srcBytes. For plaintext sources this is a file
	// offset; for compressed sources the caller ignores it.
	var srcBytes int64
	var genErr error
	for {
		if ctx.Err() != nil {
			genErr = ctx.Err()
			break
		}
		line, rerr := br.ReadString('\n')
		if len(line) > 0 {
			srcBytes += int64(len(line))
			trimmed := strings.TrimRight(line, "\r\n")
			if cf.match(trimmed) {
				if _, werr := writer.WriteString(trimmed); werr != nil {
					genErr = werr
					break
				}
				if werr := writer.WriteByte('\n'); werr != nil {
					genErr = werr
					break
				}
				count++
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			genErr = rerr
			break
		}
	}

	if genErr == nil {
		if ferr := writer.Flush(); ferr != nil {
			genErr = ferr
		}
	}
	if cerr := out.Close(); cerr != nil && genErr == nil {
		genErr = cerr
	}

	if genErr != nil {
		os.Remove(tmpPath)
		if isNoSpaceErr(genErr) {
			return 0, "", 0, fmt.Errorf("ran out of disk space while filtering wordlist: %w", genErr)
		}
		return 0, "", 0, genErr
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		return 0, "", 0, err
	}

	return count, hex.EncodeToString(hash.Sum(nil)), srcBytes, nil
}

// appendFilterWordlist filters only the parent's tail starting at startOffset and
// APPENDS matching lines to the existing child file (GH #40 follow-up). It is the
// fast path for append-only parent growth: order is preserved (existing matches
// stay, new matches are appended) so dispatch/keyspace ordering is unaffected.
// Returns the number of newly appended matches and the new source offset up to the
// last complete line. Only valid for plaintext parents (byte-seekable).
func (m *manager) appendFilterWordlist(ctx context.Context, srcPath, dstPath string, f models.WordlistFilter, startOffset int64) (int64, int64, error) {
	cf, err := compileFilter(f)
	if err != nil {
		return 0, 0, err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return 0, 0, err
	}
	defer src.Close()
	if _, err := src.Seek(startOffset, io.SeekStart); err != nil {
		return 0, 0, err
	}

	out, err := os.OpenFile(dstPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, 0, err
	}

	writer := bufio.NewWriterSize(out, 4*1024*1024)
	br := bufio.NewReaderSize(src, 16*1024*1024)

	var newCount int64
	var srcBytes int64
	var genErr error
	for {
		if ctx.Err() != nil {
			genErr = ctx.Err()
			break
		}
		line, rerr := br.ReadString('\n')
		if len(line) > 0 {
			srcBytes += int64(len(line))
			trimmed := strings.TrimRight(line, "\r\n")
			if cf.match(trimmed) {
				if _, werr := writer.WriteString(trimmed); werr != nil {
					genErr = werr
					break
				}
				if werr := writer.WriteByte('\n'); werr != nil {
					genErr = werr
					break
				}
				newCount++
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			genErr = rerr
			break
		}
	}
	consumed := startOffset + srcBytes

	if genErr == nil {
		if ferr := writer.Flush(); ferr != nil {
			genErr = ferr
		}
	}
	// fsync the appended bytes before the caller updates the DB so a crash can't
	// leave the on-disk file ahead of the recorded word_count/offset undetected.
	if genErr == nil {
		if serr := out.Sync(); serr != nil {
			genErr = serr
		}
	}
	if cerr := out.Close(); cerr != nil && genErr == nil {
		genErr = cerr
	}

	if genErr != nil {
		if isNoSpaceErr(genErr) {
			return 0, 0, fmt.Errorf("ran out of disk space while appending to filtered wordlist: %w", genErr)
		}
		return 0, 0, genErr
	}
	return newCount, consumed, nil
}

// computeAnchorMD5 returns the MD5 of the file bytes in the window
// [offset-anchorWindow, offset). It is the cheap "did the prefix shift?" probe for
// incremental regeneration. Returns nil when offset is 0 (no window).
func computeAnchorMD5(filePath string, offset int64) (*string, error) {
	if offset <= 0 {
		return nil, nil
	}
	start := offset - anchorWindow
	if start < 0 {
		start = 0
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	h := md5.New()
	if _, err := io.CopyN(h, file, offset-start); err != nil {
		return nil, err
	}
	s := hex.EncodeToString(h.Sum(nil))
	return &s, nil
}

// hashFileMD5 returns the MD5 and size of a file (used to refresh a child's
// md5_hash/file_size after an incremental append).
func hashFileMD5(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	h := md5.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// PreviewFilter estimates how many candidates a filter keeps by sampling the
// first sampleLines of the parent wordlist and extrapolating to its full count.
func (m *manager) PreviewFilter(ctx context.Context, parentID int, f models.WordlistFilter, sampleLines int64) (*models.FilterPreviewResponse, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	parent, err := m.store.GetWordlist(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, fmt.Errorf("parent wordlist not found")
	}
	cf, err := compileFilter(f)
	if err != nil {
		return nil, err
	}
	if sampleLines <= 0 {
		sampleLines = 1000000
	}

	srcPath := m.GetWordlistPath(parent.FileName, parent.WordlistType)
	reader, err := openWordlistReader(srcPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	br := bufio.NewReaderSize(reader, 16*1024*1024)
	var sampled, matched int64
	for sampled < sampleLines {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		line, rerr := br.ReadString('\n')
		if len(line) > 0 {
			sampled++
			if cf.match(strings.TrimRight(line, "\r\n")) {
				matched++
			}
		}
		if rerr != nil {
			break
		}
	}

	rate := 0.0
	if sampled > 0 {
		rate = float64(matched) / float64(sampled)
	}
	est := int64(rate * float64(parent.WordCount))
	return &models.FilterPreviewResponse{
		EstimatedCount:  est,
		SampledLines:    sampled,
		MatchedInSample: matched,
		MatchRate:       rate,
		ParentCount:     parent.WordCount,
	}, nil
}

// CreateFilteredWordlistRecord validates a filter and creates a pending filtered
// wordlist row referencing its parent. Generation is performed separately via
// GenerateFilteredWordlist. When ownerJobID is set the wordlist is ephemeral
// (job-scoped) and hidden from listings.
func (m *manager) CreateFilteredWordlistRecord(ctx context.Context, parentID int, name, description string, filter models.WordlistFilter, ephemeral bool, ownerJobID *uuid.UUID, userID uuid.UUID) (*models.Wordlist, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	parent, err := m.store.GetWordlist(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, fmt.Errorf("parent wordlist not found")
	}
	if parent.ParentWordlistID != nil {
		return nil, fmt.Errorf("cannot filter an already-filtered wordlist")
	}

	if name == "" {
		name = parent.Name + " (filtered)"
	}
	base := fsutil.SanitizeFilename(name)
	if base == "" {
		base = "filtered"
	}
	// Ephemeral (job-scoped) filtered wordlists carry a recognizable filename
	// prefix so agents can delete their local copy once the job finishes (GH #40).
	// Keep them under custom/ so existing file-serving/sync routing is unchanged.
	namePrefix := ""
	if ephemeral {
		namePrefix = EphemeralFilenamePrefix
	}
	fileName := filepath.Join("custom", fmt.Sprintf("%s%s_%s.txt", namePrefix, base, uuid.New().String()[:8]))

	filterCopy := filter
	wl := &models.Wordlist{
		Name:               name,
		Description:        description,
		WordlistType:       "custom",
		Format:             string(models.WordlistFormatPlaintext),
		FileName:           fileName,
		WordCount:          0,
		CreatedBy:          userID,
		VerificationStatus: "pending",
		ParentWordlistID:   &parentID,
		FilterSpec:         &filterCopy,
		ParentMD5:          parent.MD5Hash,
		IsEphemeral:        ephemeral,
		OwnerJobID:         ownerJobID,
	}
	if err := m.store.CreateWordlist(ctx, wl); err != nil {
		return nil, err
	}
	return wl, nil
}

// SetWordlistOwnerJob attaches an ephemeral filtered wordlist to its owning job
// (used for cleanup once the job ends).
func (m *manager) SetWordlistOwnerJob(ctx context.Context, wordlistID int, jobID uuid.UUID) error {
	return m.store.SetWordlistOwnerJob(ctx, wordlistID, jobID)
}

// GenerateFilteredWordlist (re)materializes a filtered wordlist from its parent
// and updates its status to verified (with counts) or failed. When the parent only
// grew append-only since the last generation (verified via the stored
// offset/anchor index), it filters and appends just the new tail (incremental);
// otherwise it does a full rebuild. Both paths refresh the index. Used by both the
// manual Regenerate button and the monitor's auto-regeneration.
func (m *manager) GenerateFilteredWordlist(ctx context.Context, wordlistID int) error {
	wl, err := m.store.GetWordlist(ctx, wordlistID)
	if err != nil {
		return err
	}
	if wl == nil {
		return fmt.Errorf("wordlist not found")
	}
	if wl.ParentWordlistID == nil || wl.FilterSpec == nil {
		return fmt.Errorf("wordlist %d is not a filtered wordlist", wordlistID)
	}

	parent, err := m.store.GetWordlist(ctx, *wl.ParentWordlistID)
	if err != nil {
		return err
	}
	if parent == nil {
		_ = m.store.UpdateWordlistVerification(ctx, wordlistID, "failed", nil)
		return fmt.Errorf("parent wordlist %d not found", *wl.ParentWordlistID)
	}

	srcPath := m.GetWordlistPath(parent.FileName, parent.WordlistType)
	dstPath := m.GetWordlistPath(wl.FileName, wl.WordlistType)

	if m.canAppendRegenerate(wl, srcPath, dstPath) {
		if err := m.generateIncremental(ctx, wl, parent, srcPath, dstPath); err != nil {
			// An incremental append failed (and may have written partial bytes); a
			// full rebuild writes a fresh file via tmp+rename and self-corrects.
			debug.Warning("Incremental regeneration of filtered wordlist %d failed (%v); falling back to full rebuild", wordlistID, err)
		} else {
			return nil
		}
	}
	return m.generateFull(ctx, wl, parent, srcPath, dstPath)
}

// canAppendRegenerate reports whether the parent changed in an append-only way that
// lets us regenerate the child by filtering just the new tail (GH #40 follow-up).
func (m *manager) canAppendRegenerate(wl *models.Wordlist, srcPath, dstPath string) bool {
	// Require a prior plaintext index and a seekable (non-compressed) parent.
	if wl.ParentOffset == nil || wl.ParentAnchorMD5 == nil || isCompressedPath(srcPath) {
		return false
	}
	// The child must exist and match the recorded size; a mismatch means it was
	// modified out-of-band or a prior append crashed mid-write → full rebuild.
	childInfo, err := os.Stat(dstPath)
	if err != nil || childInfo.Size() != wl.FileSize {
		return false
	}
	// The parent must have grown (append-only). Equal or smaller size with a
	// changed MD5 means content within the prefix changed → full rebuild.
	parentInfo, err := os.Stat(srcPath)
	if err != nil || parentInfo.Size() <= *wl.ParentOffset {
		return false
	}
	// Cheap prefix-shift probe: the parent's window ending at the old offset must
	// still hash to the stored anchor.
	anchor, err := computeAnchorMD5(srcPath, *wl.ParentOffset)
	if err != nil || anchor == nil || *anchor != *wl.ParentAnchorMD5 {
		return false
	}
	return true
}

// generateFull rebuilds the whole filtered child from the parent.
func (m *manager) generateFull(ctx context.Context, wl, parent *models.Wordlist, srcPath, dstPath string) error {
	count, md5hex, consumed, ferr := m.FilterWordlist(ctx, srcPath, dstPath, *wl.FilterSpec)
	if ferr != nil {
		debug.Error("Failed to generate filtered wordlist %d: %v", wl.ID, ferr)
		_ = m.store.UpdateWordlistVerification(ctx, wl.ID, "failed", nil)
		return ferr
	}
	if count == 0 {
		os.Remove(dstPath)
		_ = m.store.UpdateWordlistVerification(ctx, wl.ID, "failed", nil)
		return fmt.Errorf("filter matched 0 candidates: no word in %q satisfied all criteria (length/character-class/regex are combined with AND) — loosen the filter and try again", parent.Name)
	}

	var size int64
	if info, statErr := os.Stat(dstPath); statErr == nil {
		size = info.Size()
	}
	if err := m.store.UpdateWordlistComplete(ctx, wl.ID, md5hex, size, count); err != nil {
		return err
	}

	// Capture the incremental index (plaintext parents only; compressed parents
	// store NULL offset/anchor, forcing a full rebuild next time).
	var offsetPtr *int64
	var anchorPtr *string
	if !isCompressedPath(srcPath) {
		offsetPtr = &consumed
		if a, aerr := computeAnchorMD5(srcPath, consumed); aerr == nil {
			anchorPtr = a
		}
	}
	if err := m.store.UpdateFilteredIndex(ctx, wl.ID, parent.MD5Hash, offsetPtr, anchorPtr); err != nil {
		debug.Warning("Failed to record filtered index for wordlist %d: %v", wl.ID, err)
	}
	if err := m.store.ClearStale(ctx, wl.ID); err != nil {
		debug.Warning("Failed to clear stale flag for filtered wordlist %d: %v", wl.ID, err)
	}
	debug.Info("Generated filtered wordlist %d (full): %d candidates", wl.ID, count)
	return m.store.UpdateWordlistVerification(ctx, wl.ID, "verified", &count)
}

// generateIncremental filters only the parent's new tail and appends matches to the
// existing child, preserving order so keyspace/dispatch ordering is unaffected.
func (m *manager) generateIncremental(ctx context.Context, wl, parent *models.Wordlist, srcPath, dstPath string) error {
	newCount, consumed, aerr := m.appendFilterWordlist(ctx, srcPath, dstPath, *wl.FilterSpec, *wl.ParentOffset)
	if aerr != nil {
		return aerr
	}

	// Refresh md5/size from the (now larger) child file. A zero-new-match append is
	// NOT a failure — the child still holds every prior match.
	md5hex, size, herr := hashFileMD5(dstPath)
	if herr != nil {
		return herr
	}
	total := wl.WordCount + newCount
	if err := m.store.UpdateWordlistComplete(ctx, wl.ID, md5hex, size, total); err != nil {
		return err
	}
	anchorPtr, _ := computeAnchorMD5(srcPath, consumed)
	if err := m.store.UpdateFilteredIndex(ctx, wl.ID, parent.MD5Hash, &consumed, anchorPtr); err != nil {
		debug.Warning("Failed to record filtered index for wordlist %d: %v", wl.ID, err)
	}
	if err := m.store.ClearStale(ctx, wl.ID); err != nil {
		debug.Warning("Failed to clear stale flag for filtered wordlist %d: %v", wl.ID, err)
	}
	debug.Info("Regenerated filtered wordlist %d (incremental): +%d candidates (total %d)", wl.ID, newCount, total)
	return m.store.UpdateWordlistVerification(ctx, wl.ID, "verified", &total)
}

// RegenerateFilteredWordlist re-runs generation for an existing filtered
// wordlist (e.g. after its parent changed), refreshing the parent MD5 baseline.
func (m *manager) RegenerateFilteredWordlist(ctx context.Context, wordlistID int) error {
	wl, err := m.store.GetWordlist(ctx, wordlistID)
	if err != nil {
		return err
	}
	if wl == nil {
		return fmt.Errorf("wordlist not found")
	}
	if wl.ParentWordlistID == nil {
		return fmt.Errorf("wordlist %d is not a filtered wordlist", wordlistID)
	}
	if err := m.store.UpdateWordlistVerification(ctx, wordlistID, "pending", nil); err != nil {
		return err
	}
	return m.GenerateFilteredWordlist(ctx, wordlistID)
}

// RegenerateFilteredWordlistFull forces a FULL rebuild of a filtered wordlist,
// bypassing the incremental append fast-path (GH #40 follow-up). It drops the
// incremental index first so the next generation re-streams the entire parent. This
// is the manual "Regenerate" action — the escape hatch for when the parent changed
// in a non-append way (reorder/rewrite) or a prior regeneration failed; automatic
// regeneration on parent change stays incremental.
func (m *manager) RegenerateFilteredWordlistFull(ctx context.Context, wordlistID int) error {
	wl, err := m.store.GetWordlist(ctx, wordlistID)
	if err != nil {
		return err
	}
	if wl == nil {
		return fmt.Errorf("wordlist not found")
	}
	if wl.ParentWordlistID == nil {
		return fmt.Errorf("wordlist %d is not a filtered wordlist", wordlistID)
	}
	if err := m.store.ClearFilteredIndex(ctx, wordlistID); err != nil {
		debug.Warning("Failed to clear filtered index for wordlist %d: %v", wordlistID, err)
	}
	if err := m.store.UpdateWordlistVerification(ctx, wordlistID, "pending", nil); err != nil {
		return err
	}
	return m.GenerateFilteredWordlist(ctx, wordlistID)
}

// GetFilteredChildren returns filtered wordlists derived from a parent.
func (m *manager) GetFilteredChildren(ctx context.Context, parentID int) ([]*models.Wordlist, error) {
	return m.store.GetFilteredChildren(ctx, parentID)
}

// GetEphemeralWordlistsByJob returns ephemeral filtered wordlists owned by a job.
func (m *manager) GetEphemeralWordlistsByJob(ctx context.Context, jobID uuid.UUID) ([]*models.Wordlist, error) {
	return m.store.GetEphemeralByJob(ctx, jobID)
}
