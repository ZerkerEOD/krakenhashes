package services

import (
	"bufio"
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

// AssociationWordlistManager handles upload, validation, and storage of association wordlists.
type AssociationWordlistManager struct {
	assocWordlistRepo *repository.AssociationWordlistRepository
	hashlistRepo      *repository.HashListRepository
	basePath          string // Base path for association wordlists (e.g., data/wordlists/association)
}

// NewAssociationWordlistManager creates a new AssociationWordlistManager.
func NewAssociationWordlistManager(
	assocWordlistRepo *repository.AssociationWordlistRepository,
	hashlistRepo *repository.HashListRepository,
	basePath string,
) *AssociationWordlistManager {
	return &AssociationWordlistManager{
		assocWordlistRepo: assocWordlistRepo,
		hashlistRepo:      hashlistRepo,
		basePath:          basePath,
	}
}

// UploadResult contains the result of an association wordlist upload.
type UploadResult struct {
	Wordlist       *models.AssociationWordlist
	LineCountMatch bool
	HashlistLines  int64
	WordlistLines  int64
	Warning        string
}

// Upload handles the upload and validation of an association wordlist.
// It validates that the wordlist line count matches the hashlist's total hashes.
func (m *AssociationWordlistManager) Upload(ctx context.Context, hashlistID int64, filename string, tempFilePath string) (*UploadResult, error) {
	debug.Info("Uploading association wordlist '%s' for hashlist %d", filename, hashlistID)

	// Get the hashlist to verify it exists and get the total hashes
	hashlist, err := m.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist %d: %w", hashlistID, err)
	}

	// Count lines in the wordlist
	lineCount, err := countLines(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to count lines in wordlist: %w", err)
	}
	debug.Info("Association wordlist '%s' has %d lines, hashlist %d has %d hashes", filename, lineCount, hashlistID, hashlist.TotalHashes)

	// Create the destination path
	destPath := m.getWordlistPath(hashlistID, filename)

	// Ensure the directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Move the temp file to the destination
	if err := os.Rename(tempFilePath, destPath); err != nil {
		// If rename fails (cross-device), copy and delete
		if err := copyFile(tempFilePath, destPath); err != nil {
			return nil, fmt.Errorf("failed to move/copy wordlist to destination: %w", err)
		}
		os.Remove(tempFilePath) // Clean up temp file
	}

	// Get file size
	fileInfo, err := os.Stat(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat wordlist file: %w", err)
	}

	// Calculate MD5 hash
	md5Hash, err := calculateMD5(destPath)
	if err != nil {
		debug.Warning("Failed to calculate MD5 hash for %s: %v", destPath, err)
		md5Hash = "" // Continue without MD5
	}

	// Create the wordlist record
	wordlist := &models.AssociationWordlist{
		HashlistID: hashlistID,
		FilePath:   destPath,
		FileName:   filename,
		FileSize:   fileInfo.Size(),
		LineCount:  lineCount,
		MD5Hash:    md5Hash,
	}

	if err := m.assocWordlistRepo.Create(ctx, wordlist); err != nil {
		// Clean up file on DB error
		os.Remove(destPath)
		return nil, fmt.Errorf("failed to create association wordlist record: %w", err)
	}

	// Check if line counts match
	lineCountMatch := lineCount == int64(hashlist.TotalHashes)
	var warning string
	if !lineCountMatch {
		warning = fmt.Sprintf("Line count mismatch: wordlist has %d lines but hashlist has %d hashes. Association attacks require exact line count match.", lineCount, hashlist.TotalHashes)
		debug.Warning("Association wordlist %s: %s", wordlist.ID, warning)
	}

	result := &UploadResult{
		Wordlist:       wordlist,
		LineCountMatch: lineCountMatch,
		HashlistLines:  int64(hashlist.TotalHashes),
		WordlistLines:  lineCount,
		Warning:        warning,
	}

	debug.Info("Successfully uploaded association wordlist %s for hashlist %d (lines match: %v)", wordlist.ID, hashlistID, lineCountMatch)
	return result, nil
}

// List returns all association wordlists for a given hashlist.
func (m *AssociationWordlistManager) List(ctx context.Context, hashlistID int64) ([]models.AssociationWordlist, error) {
	return m.assocWordlistRepo.ListByHashlistID(ctx, hashlistID)
}

// Get retrieves a specific association wordlist by ID.
func (m *AssociationWordlistManager) Get(ctx context.Context, id uuid.UUID) (*models.AssociationWordlist, error) {
	return m.assocWordlistRepo.GetByID(ctx, id)
}

// Delete removes an association wordlist and its file.
func (m *AssociationWordlistManager) Delete(ctx context.Context, id uuid.UUID) error {
	// Get the wordlist to get the file path
	wordlist, err := m.assocWordlistRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Delete the database record
	if err := m.assocWordlistRepo.Delete(ctx, id); err != nil {
		return err
	}

	// Delete the file
	if err := os.Remove(wordlist.FilePath); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to delete association wordlist file %s: %v", wordlist.FilePath, err)
		// Don't return error - DB record is already deleted
	}

	debug.Info("Deleted association wordlist %s and file %s", id, wordlist.FilePath)
	return nil
}

// DeleteAllForHashlist removes all association wordlists and their files for a hashlist.
func (m *AssociationWordlistManager) DeleteAllForHashlist(ctx context.Context, hashlistID int64) error {
	// Get all file paths and delete records
	filePaths, err := m.assocWordlistRepo.DeleteByHashlistID(ctx, hashlistID)
	if err != nil {
		return err
	}

	// Delete all files
	for _, filePath := range filePaths {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			debug.Warning("Failed to delete association wordlist file %s: %v", filePath, err)
			// Continue deleting other files
		}
	}

	debug.Info("Deleted %d association wordlists for hashlist %d", len(filePaths), hashlistID)
	return nil
}

// GetFilePath retrieves the file path for serving to agents.
func (m *AssociationWordlistManager) GetFilePath(ctx context.Context, id uuid.UUID) (string, error) {
	return m.assocWordlistRepo.GetFilePath(ctx, id)
}

// ValidateLineCount checks if the wordlist line count matches the hashlist's total hashes.
func (m *AssociationWordlistManager) ValidateLineCount(ctx context.Context, wordlistID uuid.UUID, hashlistID int64) (bool, error) {
	wordlist, err := m.assocWordlistRepo.GetByID(ctx, wordlistID)
	if err != nil {
		return false, err
	}

	hashlist, err := m.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return false, err
	}

	return wordlist.LineCount == int64(hashlist.TotalHashes), nil
}

// getWordlistPath generates the storage path for an association wordlist.
func (m *AssociationWordlistManager) getWordlistPath(hashlistID int64, filename string) string {
	// Sanitize filename
	safeFilename := sanitizeFilename(filename)
	return filepath.Join(m.basePath, fmt.Sprintf("%d_%s", hashlistID, safeFilename))
}

// sanitizeFilename removes potentially dangerous characters from filenames.
func sanitizeFilename(filename string) string {
	// Remove path separators and null bytes
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Trim spaces and dots from beginning/end
	filename = strings.Trim(filename, " .")

	if filename == "" {
		filename = "wordlist"
	}

	return filename
}

// countLines counts the number of lines in a file efficiently.
func countLines(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	// Use a larger buffer for performance
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, err
	}

	return count, nil
}

// calculateMD5 calculates the MD5 hash of a file.
func calculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
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

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}
