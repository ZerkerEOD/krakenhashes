package binary

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	maxDownloadAttempts = 3
	retryDelay          = 5 * time.Second
)

// manager implements the Manager interface
type manager struct {
	store  Store
	config Config
}

// NewManager creates a new binary manager instance
func NewManager(store Store, config Config) (Manager, error) {
	debug.Info("NewManager called with DataDir: %s", config.DataDir)

	// The data directory should already exist from config initialization
	// Just ensure the local subdirectory exists
	localDir := filepath.Join(config.DataDir, "local")
	if err := os.MkdirAll(localDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create local directory: %w", err)
	}

	debug.Info("Binary manager initialized with data directory: %s", config.DataDir)

	return &manager{
		store:  store,
		config: config,
	}, nil
}

// AddVersion implements Manager.AddVersion
func (m *manager) AddVersion(ctx context.Context, version *BinaryVersion) error {
	// Set initial verification status
	version.VerificationStatus = VerificationStatusPending

	// Create the version record
	if err := m.store.CreateVersion(ctx, version); err != nil {
		return fmt.Errorf("failed to create version record: %w", err)
	}

	// Try downloading the binary with retries
	var lastErr error
	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		debug.Info("Attempting to download binary (attempt %d/%d)", attempt, maxDownloadAttempts)

		if err := m.DownloadBinary(ctx, version); err != nil {
			lastErr = err
			debug.Error("Download attempt %d failed: %v", attempt, err)

			// Check if context is cancelled before retrying
			if ctx.Err() != nil {
				return fmt.Errorf("operation cancelled: %w", ctx.Err())
			}

			if attempt < maxDownloadAttempts {
				debug.Info("Waiting %v before next attempt", retryDelay)
				time.Sleep(retryDelay)
				continue
			}
			break
		}

		// Download successful
		lastErr = nil
		break
	}

	if lastErr != nil {
		// Update version status to failed
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("failed to update version status for version %d: %v",
				version.ID, updateErr)
		}
		return fmt.Errorf("failed to download binary after %d attempts: %w", maxDownloadAttempts, lastErr)
	}

	// Get file info to set the file size
	filePath := m.getBinaryPath(version)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	version.FileSize = fileInfo.Size()

	// Calculate hash and verify the binary
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open binary file: %w", err)
	}
	defer file.Close()

	// Calculate initial hash
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	calculatedHash := hex.EncodeToString(hash.Sum(nil))
	version.MD5Hash = calculatedHash
	version.VerificationStatus = VerificationStatusVerified
	now := time.Now()
	version.LastVerifiedAt = &now

	// Update version with hash, file size, and verification status
	if err := m.store.UpdateVersion(ctx, version); err != nil {
		return fmt.Errorf("failed to update version with calculated hash: %w", err)
	}

	// Verify the binary to ensure integrity
	if err := m.VerifyVersion(ctx, version.ID); err != nil {
		return fmt.Errorf("failed to verify binary integrity: %w", err)
	}

	// Extract the binary for server-side use
	if err := m.ExtractBinary(ctx, version.ID); err != nil {
		debug.Warning("Failed to extract binary version %d: %v", version.ID, err)
		// Don't fail the entire operation if extraction fails
		// It can be extracted on-demand when needed
	} else {
		debug.Info("Successfully extracted binary version %d", version.ID)
	}

	debug.Info("Successfully added and verified binary version %d with hash %s", version.ID, calculatedHash)
	return nil
}

// AddVersionFromUpload implements Manager.AddVersionFromUpload
// It handles binary uploads by saving the file directly instead of downloading from URL
func (m *manager) AddVersionFromUpload(ctx context.Context, version *BinaryVersion, file io.Reader, fileSize int64) error {
	// Set upload-specific defaults
	version.SourceType = SourceTypeUpload
	version.VerificationStatus = VerificationStatusPending
	version.IsActive = true

	// Create version record to get ID
	if err := m.store.CreateVersion(ctx, version); err != nil {
		return fmt.Errorf("failed to create version record: %w", err)
	}

	// Create directory: binaries/{id}/
	binaryDir := filepath.Join(m.config.DataDir, fmt.Sprintf("%d", version.ID))
	if err := os.MkdirAll(binaryDir, 0750); err != nil {
		return fmt.Errorf("failed to create binary directory: %w", err)
	}

	// Save file and calculate MD5 hash
	destPath := filepath.Join(binaryDir, version.FileName)
	hash := md5.New()
	destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, 0750)
	if err != nil {
		return fmt.Errorf("failed to create binary file: %w", err)
	}

	// Write to both file and hash calculator
	written, err := io.Copy(io.MultiWriter(destFile, hash), file)
	destFile.Close() // Close before checking error to ensure data is flushed
	if err != nil {
		os.Remove(destPath) // Clean up on error
		return fmt.Errorf("failed to save binary file: %w", err)
	}

	// Update version with hash and size
	version.MD5Hash = hex.EncodeToString(hash.Sum(nil))
	version.FileSize = written
	version.VerificationStatus = VerificationStatusVerified
	now := time.Now()
	version.LastVerifiedAt = &now

	// Update database record with hash and file size
	if err := m.store.UpdateVersion(ctx, version); err != nil {
		os.Remove(destPath) // Clean up on error
		return fmt.Errorf("failed to update version record: %w", err)
	}

	// Extract the binary for server-side use
	if err := m.ExtractBinary(ctx, version.ID); err != nil {
		debug.Warning("Failed to extract binary version %d: %v", version.ID, err)
		// Don't fail the entire operation if extraction fails
		// It can be extracted on-demand when needed
	} else {
		debug.Info("Successfully extracted binary version %d", version.ID)
	}

	debug.Info("Successfully uploaded and verified binary version %d with hash %s", version.ID, version.MD5Hash)
	return nil
}

// GetVersion implements Manager.GetVersion
func (m *manager) GetVersion(ctx context.Context, id int64) (*BinaryVersion, error) {
	return m.store.GetVersion(ctx, id)
}

// ListVersions implements Manager.ListVersions
func (m *manager) ListVersions(ctx context.Context, filters map[string]interface{}) ([]*BinaryVersion, error) {
	return m.store.ListVersions(ctx, filters)
}

// VerifyVersion implements Manager.VerifyVersion
func (m *manager) VerifyVersion(ctx context.Context, id int64) error {
	version, err := m.store.GetVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	// Skip verification for deleted versions
	if version.VerificationStatus == VerificationStatusDeleted {
		return fmt.Errorf("cannot verify deleted version")
	}

	// Check if binary file exists
	filePath := m.getBinaryPath(version)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		debug.Error("Binary file does not exist: %s", filePath)
		version.VerificationStatus = VerificationStatusDeleted
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("binary file does not exist")
	}

	// Open the binary file
	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("Failed to open binary file: %v", err)
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("failed to open binary file: %w", err)
	}
	defer file.Close()

	// Calculate MD5 hash
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		debug.Error("Failed to calculate hash: %v", err)
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	calculatedHash := hex.EncodeToString(hash.Sum(nil))

	// Get file info for size verification
	fileInfo, err := file.Stat()
	if err != nil {
		debug.Error("Failed to get file info: %v", err)
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Verify file size matches
	if fileInfo.Size() != version.FileSize {
		debug.Error("File size mismatch: expected %d, got %d", version.FileSize, fileInfo.Size())
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("file size mismatch: expected %d, got %d", version.FileSize, fileInfo.Size())
	}

	// Verify hash matches
	if calculatedHash != version.MD5Hash {
		debug.Error("Hash mismatch: expected %s, got %s", version.MD5Hash, calculatedHash)
		version.VerificationStatus = VerificationStatusFailed
		if updateErr := m.store.UpdateVersion(ctx, version); updateErr != nil {
			debug.Error("Failed to update version status: %v", updateErr)
		}
		return fmt.Errorf("hash mismatch: expected %s, got %s", version.MD5Hash, calculatedHash)
	}

	// Update verification status
	now := time.Now()
	version.LastVerifiedAt = &now
	version.VerificationStatus = VerificationStatusVerified
	if err := m.store.UpdateVersion(ctx, version); err != nil {
		return fmt.Errorf("failed to update version status: %w", err)
	}

	debug.Info("Successfully verified binary version %d", id)
	return nil
}

// DownloadBinary implements Manager.DownloadBinary
func (m *manager) DownloadBinary(ctx context.Context, version *BinaryVersion) error {
	// Check if file already exists
	filePath := m.getBinaryPath(version)
	debug.Info("Downloading binary to path: %s (DataDir: %s)", filePath, m.config.DataDir)

	if _, err := os.Stat(filePath); err == nil {
		debug.Info("Binary file already exists at %s, skipping download", filePath)
		return nil
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Minute, // Long timeout for large files
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, version.SourceURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Ensure binary directory exists
	binDir := filepath.Dir(filePath)
	debug.Info("Creating binary directory: %s", binDir)
	if err := os.MkdirAll(binDir, 0750); err != nil {
		return fmt.Errorf("failed to create binary directory %s: %w", binDir, err)
	}

	// Create binary file with restricted permissions
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0750)
	if err != nil {
		return fmt.Errorf("failed to create binary file: %w", err)
	}
	defer file.Close()

	// Calculate hash while downloading
	hash := md5.New()
	writer := io.MultiWriter(file, hash)

	// Copy data
	written, err := io.Copy(writer, resp.Body)
	if err != nil {
		// Clean up file on error
		os.Remove(filePath)
		return fmt.Errorf("failed to save binary: %w", err)
	}

	// Update file size
	version.FileSize = written

	// Calculate and store hash
	calculatedHash := hex.EncodeToString(hash.Sum(nil))
	version.MD5Hash = calculatedHash
	debug.Info("Downloaded binary and calculated hash: %s", calculatedHash)

	return nil
}

// DeleteVersion implements Manager.DeleteVersion with fallback protection
func (m *manager) DeleteVersion(ctx context.Context, id int64) error {
	// Get version first to get details
	version, err := m.store.GetVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	// Prevent deletion of the default binary
	if version.IsDefault {
		return fmt.Errorf("cannot delete the default binary; set another binary as default first")
	}

	// Check if this is the last active binary of its type
	count, err := m.store.CountActiveBinaries(ctx, version.BinaryType)
	if err != nil {
		return fmt.Errorf("failed to count active binaries: %w", err)
	}

	// Prevent deletion of the last binary
	if count <= 1 {
		return fmt.Errorf("cannot delete the only remaining binary of type %s", version.BinaryType)
	}

	// Delete binary file
	filePath := m.getBinaryPath(version)
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete binary file: %w", err)
		}
		// If file doesn't exist, just log it
		debug.Warning("Binary file already deleted: %s", filePath)
	}

	// Clean up the version directory if it's empty
	versionDir := filepath.Dir(filePath)
	if err := os.Remove(versionDir); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to remove empty version directory: %v", err)
	}

	// Clean up the local extraction directory
	localDir := m.getLocalBinaryDir(version)
	if err := os.RemoveAll(localDir); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to remove local extraction directory: %v", err)
	}

	// Update version status to deleted and inactive
	version.IsActive = false
	version.IsDefault = false
	version.VerificationStatus = VerificationStatusDeleted
	version.LastVerifiedAt = nil // Clear last verification time
	if err := m.store.UpdateVersion(ctx, version); err != nil {
		return fmt.Errorf("failed to update version status: %w", err)
	}

	// Create audit log entry
	auditLog := &BinaryAuditLog{
		BinaryVersionID: id,
		Action:          "deleted",
		PerformedBy:     version.CreatedBy, // TODO: Get actual user from context
		Details: map[string]any{
			"binary_type": version.BinaryType,
			"version":     version.Version,
		},
	}
	if err := m.store.CreateAuditLog(ctx, auditLog); err != nil {
		debug.Warning("Failed to create audit log: %v", err)
	}

	debug.Info("Successfully deleted binary version %d", id)
	return nil
}

// SetDefaultVersion implements Manager.SetDefaultVersion
func (m *manager) SetDefaultVersion(ctx context.Context, id int64) error {
	// Get version to verify it exists and is active
	version, err := m.store.GetVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	// Verify version is active and verified
	if !version.IsActive {
		return fmt.Errorf("cannot set inactive version as default")
	}
	if version.VerificationStatus != VerificationStatusVerified {
		return fmt.Errorf("cannot set unverified version as default")
	}

	// Set as default
	if err := m.store.SetDefault(ctx, id); err != nil {
		return fmt.Errorf("failed to set default: %w", err)
	}

	// Create audit log entry
	auditLog := &BinaryAuditLog{
		BinaryVersionID: id,
		Action:          "set_default",
		PerformedBy:     version.CreatedBy, // TODO: Get actual user from context
		Details: map[string]any{
			"binary_type": version.BinaryType,
		},
	}
	if err := m.store.CreateAuditLog(ctx, auditLog); err != nil {
		debug.Warning("Failed to create audit log: %v", err)
	}

	debug.Info("Successfully set binary version %d as default for type %s", id, version.BinaryType)
	return nil
}

// GetLatestActive implements Manager.GetLatestActive
func (m *manager) GetLatestActive(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error) {
	return m.store.GetLatestActive(ctx, binaryType)
}

// GetDefault implements Manager.GetDefault
func (m *manager) GetDefault(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error) {
	return m.store.GetDefault(ctx, binaryType)
}

// getBinaryPath returns the full path for a binary version
func (m *manager) getBinaryPath(version *BinaryVersion) string {
	path := filepath.Join(m.config.DataDir, fmt.Sprintf("%d", version.ID), version.FileName)
	debug.Debug("getBinaryPath: DataDir=%s, ID=%d, FileName=%s, Result=%s",
		m.config.DataDir, version.ID, version.FileName, path)
	return path
}

// ExtractBinary implements Manager.ExtractBinary
func (m *manager) ExtractBinary(ctx context.Context, id int64) error {
	// Get version details
	version, err := m.store.GetVersion(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	// Check if already extracted
	localDir := m.getLocalBinaryDir(version)
	hashcatPath := m.getHashcatExecutablePath(localDir)
	if _, err := os.Stat(hashcatPath); err == nil {
		debug.Info("Binary already extracted at %s", hashcatPath)
		return nil
	}

	// Get archive path
	archivePath := m.getBinaryPath(version)
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("binary archive not found at %s: %w", archivePath, err)
	}

	// Create local extraction directory
	if err := os.MkdirAll(localDir, 0750); err != nil {
		return fmt.Errorf("failed to create local directory: %w", err)
	}

	// Extract based on compression type
	switch version.CompressionType {
	case CompressionType7z:
		if err := m.extract7z(ctx, archivePath, localDir); err != nil {
			return fmt.Errorf("failed to extract 7z archive: %w", err)
		}
	case CompressionTypeZip:
		if err := m.extractZip(ctx, archivePath, localDir); err != nil {
			return fmt.Errorf("failed to extract zip archive: %w", err)
		}
	case CompressionTypeTarGz, CompressionTypeTarXz:
		if err := m.extractTar(ctx, archivePath, localDir); err != nil {
			return fmt.Errorf("failed to extract tar archive: %w", err)
		}
	default:
		return fmt.Errorf("unsupported compression type: %s", version.CompressionType)
	}

	// After extraction, the hashcat binary should be directly in localDir
	// thanks to our extraction logic that strips common directories
	hashcatPath = m.getHashcatExecutablePath(localDir)
	if _, err := os.Stat(hashcatPath); err != nil {
		return fmt.Errorf("hashcat binary not found after extraction at %s: %w", hashcatPath, err)
	}

	// Ensure hashcat binary is executable
	if err := os.Chmod(hashcatPath, 0750); err != nil {
		debug.Warning("Failed to set executable permissions on %s: %v", hashcatPath, err)
	}

	debug.Info("Successfully extracted binary to %s with hashcat at %s", localDir, hashcatPath)
	return nil
}

// GetLocalBinaryPath implements Manager.GetLocalBinaryPath
func (m *manager) GetLocalBinaryPath(ctx context.Context, id int64) (string, error) {
	// Get version details
	version, err := m.store.GetVersion(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	// Get the local directory
	localDir := m.getLocalBinaryDir(version)

	// Check if already extracted
	hashcatPath := m.getHashcatExecutablePath(localDir)
	if _, err := os.Stat(hashcatPath); err == nil {
		return hashcatPath, nil
	}

	// Not found, try to extract it
	if extractErr := m.ExtractBinary(ctx, id); extractErr != nil {
		return "", fmt.Errorf("binary not extracted and extraction failed: %w", extractErr)
	}

	// After extraction, the binary should be there
	hashcatPath = m.getHashcatExecutablePath(localDir)
	if _, err := os.Stat(hashcatPath); err != nil {
		return "", fmt.Errorf("binary not found after extraction: %w", err)
	}

	return hashcatPath, nil
}

// getLocalBinaryDir returns the local extraction directory for a binary version
func (m *manager) getLocalBinaryDir(version *BinaryVersion) string {
	return filepath.Join(m.config.DataDir, "local", fmt.Sprintf("%d", version.ID))
}

// getHashcatExecutablePath returns the expected path for the hashcat executable
func (m *manager) getHashcatExecutablePath(localDir string) string {
	// Determine the executable name based on OS
	execName := "hashcat"
	if runtime.GOOS == "windows" {
		execName = "hashcat.exe"
	} else if runtime.GOOS == "linux" {
		// Check for both hashcat and hashcat.bin
		if _, err := os.Stat(filepath.Join(localDir, "hashcat.bin")); err == nil {
			execName = "hashcat.bin"
		}
	}
	return filepath.Join(localDir, execName)
}

// extract7z extracts a 7z archive
func (m *manager) extract7z(ctx context.Context, archivePath, destDir string) error {
	// Create a temporary extraction directory
	tempDir := filepath.Join(destDir, ".tmp_extract")
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp dir

	// Extract to temporary directory
	cmd := exec.CommandContext(ctx, "7z", "x", "-y", fmt.Sprintf("-o%s", tempDir), archivePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z extraction failed: %w\nOutput: %s", err, string(output))
	}

	// Check what was extracted
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	// If there's only one directory in the temp dir, move its contents
	if len(entries) == 1 && entries[0].IsDir() {
		// All files were in a subdirectory, move them up
		subDir := filepath.Join(tempDir, entries[0].Name())
		debug.Info("Archive contains single directory %s, moving contents to target", entries[0].Name())

		// Move all files from subdir to destDir
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			return fmt.Errorf("failed to read subdirectory: %w", err)
		}

		for _, entry := range subEntries {
			src := filepath.Join(subDir, entry.Name())
			dst := filepath.Join(destDir, entry.Name())
			if err := os.Rename(src, dst); err != nil {
				// If rename fails (cross-device), try copy
				if err := m.copyRecursive(src, dst); err != nil {
					return fmt.Errorf("failed to move %s: %w", entry.Name(), err)
				}
			}
		}
	} else {
		// Files are at root level, move everything
		for _, entry := range entries {
			src := filepath.Join(tempDir, entry.Name())
			dst := filepath.Join(destDir, entry.Name())
			if err := os.Rename(src, dst); err != nil {
				// If rename fails (cross-device), try copy
				if err := m.copyRecursive(src, dst); err != nil {
					return fmt.Errorf("failed to move %s: %w", entry.Name(), err)
				}
			}
		}
	}

	return nil
}

// copyRecursive copies a file or directory recursively
func (m *manager) copyRecursive(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}

		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())
			if err := m.copyRecursive(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}

	// Copy file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return os.Chmod(dst, info.Mode())
}

// extractZip extracts a zip archive
func (m *manager) extractZip(ctx context.Context, archivePath, destDir string) error {
	cmd := exec.CommandContext(ctx, "unzip", "-o", archivePath, "-d", destDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("unzip failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// extractTar extracts a tar archive (supports gz and xz)
func (m *manager) extractTar(ctx context.Context, archivePath, destDir string) error {
	args := []string{"-xf", archivePath, "-C", destDir}

	// Auto-detect compression based on file extension
	if strings.HasSuffix(archivePath, ".gz") {
		args = append([]string{"-z"}, args...)
	} else if strings.HasSuffix(archivePath, ".xz") {
		args = append([]string{"-J"}, args...)
	}

	cmd := exec.CommandContext(ctx, "tar", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar extraction failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}
