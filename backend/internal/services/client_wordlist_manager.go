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

// ClientWordlistManager handles upload, validation, and storage of client-specific wordlists.
type ClientWordlistManager struct {
	clientWordlistRepo *repository.ClientWordlistRepository
	clientRepo         *repository.ClientRepository
	basePath           string // Base path for client files (e.g., data/wordlists/clients)
}

// NewClientWordlistManager creates a new ClientWordlistManager.
func NewClientWordlistManager(
	clientWordlistRepo *repository.ClientWordlistRepository,
	clientRepo *repository.ClientRepository,
	basePath string,
) *ClientWordlistManager {
	return &ClientWordlistManager{
		clientWordlistRepo: clientWordlistRepo,
		clientRepo:         clientRepo,
		basePath:           basePath,
	}
}

// ClientWordlistUploadResult contains the result of a client wordlist upload.
type ClientWordlistUploadResult struct {
	Wordlist  *models.ClientWordlist
	LineCount int64
}

// ReservedPotfileName is the reserved filename for client potfiles.
// Uploads with this filename are rejected to prevent conflicts.
const ReservedPotfileName = "potfile.txt"

// Upload handles the upload of a client-specific wordlist.
func (m *ClientWordlistManager) Upload(ctx context.Context, clientID uuid.UUID, filename string, tempFilePath string) (*ClientWordlistUploadResult, error) {
	debug.Info("Uploading client wordlist '%s' for client %s", filename, clientID)

	// Check for reserved filename
	sanitizedFilename := m.sanitizeFilename(filename)
	if strings.EqualFold(sanitizedFilename, ReservedPotfileName) {
		return nil, fmt.Errorf("filename '%s' is reserved for client potfiles", ReservedPotfileName)
	}

	// Verify client exists
	client, err := m.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client %s: %w", clientID, err)
	}
	if client == nil {
		return nil, fmt.Errorf("client %s not found", clientID)
	}

	// Count lines in the wordlist
	lineCount, err := m.countLines(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to count lines in wordlist: %w", err)
	}
	debug.Info("Client wordlist '%s' has %d lines", filename, lineCount)

	// Create the destination path
	destPath := m.getWordlistPath(clientID, filename)

	// Ensure the directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Move the temp file to the destination
	if err := os.Rename(tempFilePath, destPath); err != nil {
		// If rename fails (cross-device), copy and delete
		if err := m.copyFile(tempFilePath, destPath); err != nil {
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
	md5Hash, err := m.calculateMD5(destPath)
	if err != nil {
		debug.Warning("Failed to calculate MD5 hash for %s: %v", destPath, err)
		md5Hash = "" // Continue without MD5
	}

	// Create the wordlist record
	wordlist := &models.ClientWordlist{
		ClientID:  clientID,
		FilePath:  destPath,
		FileName:  filename,
		FileSize:  fileInfo.Size(),
		LineCount: lineCount,
		MD5Hash:   md5Hash,
	}

	if err := m.clientWordlistRepo.Create(ctx, wordlist); err != nil {
		// Clean up file on DB error
		os.Remove(destPath)
		return nil, fmt.Errorf("failed to create client wordlist record: %w", err)
	}

	result := &ClientWordlistUploadResult{
		Wordlist:  wordlist,
		LineCount: lineCount,
	}

	debug.Info("Successfully uploaded client wordlist %s for client %s", wordlist.ID, clientID)
	return result, nil
}

// List returns all wordlists for a given client.
func (m *ClientWordlistManager) List(ctx context.Context, clientID uuid.UUID) ([]models.ClientWordlist, error) {
	return m.clientWordlistRepo.ListByClientID(ctx, clientID)
}

// Get retrieves a specific client wordlist by ID.
func (m *ClientWordlistManager) Get(ctx context.Context, id uuid.UUID) (*models.ClientWordlist, error) {
	return m.clientWordlistRepo.GetByID(ctx, id)
}

// Delete removes a client wordlist and its file.
func (m *ClientWordlistManager) Delete(ctx context.Context, id uuid.UUID) error {
	// Delete from database (returns file path)
	filePath, err := m.clientWordlistRepo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to delete client wordlist file %s: %v", filePath, err)
		// Don't return error - DB record is already deleted
	}

	debug.Info("Deleted client wordlist %s and file %s", id, filePath)
	return nil
}

// DeleteAllForClient removes all wordlists and their files for a client.
func (m *ClientWordlistManager) DeleteAllForClient(ctx context.Context, clientID uuid.UUID) error {
	// Get all file paths and delete records
	filePaths, err := m.clientWordlistRepo.DeleteByClientID(ctx, clientID)
	if err != nil {
		return err
	}

	// Delete all files
	for _, filePath := range filePaths {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			debug.Warning("Failed to delete client wordlist file %s: %v", filePath, err)
			// Continue deleting other files
		}
	}

	// Also delete the client's wordlist directory if empty
	clientDir := filepath.Join(m.basePath, clientID.String())
	os.Remove(clientDir) // Will fail if not empty, which is fine

	debug.Info("Deleted %d client wordlists for client %s", len(filePaths), clientID)
	return nil
}

// GetFilePath retrieves the file path for serving to agents.
func (m *ClientWordlistManager) GetFilePath(ctx context.Context, id uuid.UUID) (string, error) {
	return m.clientWordlistRepo.GetFilePath(ctx, id)
}

// getWordlistPath generates the storage path for a client wordlist.
func (m *ClientWordlistManager) getWordlistPath(clientID uuid.UUID, filename string) string {
	// Sanitize filename
	safeFilename := m.sanitizeFilename(filename)
	return filepath.Join(m.basePath, clientID.String(), safeFilename)
}

// sanitizeFilename removes potentially dangerous characters from filenames.
func (m *ClientWordlistManager) sanitizeFilename(filename string) string {
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
func (m *ClientWordlistManager) countLines(filePath string) (int64, error) {
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
func (m *ClientWordlistManager) calculateMD5(filePath string) (string, error) {
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
func (m *ClientWordlistManager) copyFile(src, dst string) error {
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

// Initialize ensures the base directory exists.
func (m *ClientWordlistManager) Initialize() error {
	if err := os.MkdirAll(m.basePath, 0755); err != nil {
		return fmt.Errorf("failed to create client wordlists directory: %w", err)
	}
	debug.Info("Initialized client wordlist manager at %s", m.basePath)
	return nil
}
