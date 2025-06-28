package sync

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"

	// Go library for archive extraction
	"github.com/bodgit/sevenzip"
)

// FileSync handles synchronization of files between the agent and backend
type FileSync struct {
	client     *http.Client
	urlConfig  *config.URLConfig
	dataDirs   *config.DataDirs
	sem        chan struct{} // Semaphore for limiting concurrent downloads
	maxRetries int           // Maximum number of retries for downloads
	apiKey     string        // API key for authentication
	agentID    string        // Agent ID for authentication
}

// Config holds configuration for file synchronization
type Config struct {
	MaxConcurrentDownloads int
	DownloadTimeout        time.Duration
	MaxRetries             int
}

// FileInfo represents information about a file for synchronization
type FileInfo struct {
	Name     string `json:"name"`
	MD5Hash  string `json:"md5_hash"` // MD5 hash used for synchronization
	Size     int64  `json:"size"`
	FileType string `json:"file_type"`          // "wordlist", "rule", "binary", "hashlist"
	Category string `json:"category,omitempty"` // For wordlists: "general", "specialized", "targeted", "custom"
	// For rules: "hashcat", "john", "custom"
	ID        int   `json:"id,omitempty"`        // ID in the backend database
	Timestamp int64 `json:"timestamp,omitempty"` // Last modified time
}

// NewFileSync creates a new file synchronization handler
func NewFileSync(urlConfig *config.URLConfig, dataDirs *config.DataDirs, apiKey, agentID string) (*FileSync, error) {
	maxDownloads, _ := strconv.Atoi(getEnvOrDefault("KH_MAX_CONCURRENT_DOWNLOADS", "3"))
	timeout, _ := time.ParseDuration(getEnvOrDefault("KH_DOWNLOAD_TIMEOUT", "1h"))
	maxRetries, _ := strconv.Atoi(getEnvOrDefault("KH_MAX_DOWNLOAD_RETRIES", "3"))

	debug.Info("Initializing file sync with max downloads: %d, timeout: %s, max retries: %d",
		maxDownloads, timeout, maxRetries)

	// Load CA certificate for TLS
	certPool, err := loadCACertificate()
	if err != nil {
		debug.Error("Failed to load CA certificate for file sync: %v", err)
		return nil, fmt.Errorf("failed to load CA certificate: %w", err)
	}

	// Create TLS configuration
	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
	}

	// Create HTTP client with TLS config
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &FileSync{
		client:     client,
		urlConfig:  urlConfig,
		dataDirs:   dataDirs,
		sem:        make(chan struct{}, maxDownloads),
		maxRetries: maxRetries,
		apiKey:     apiKey,
		agentID:    agentID,
	}, nil
}

// loadCACertificate loads the CA certificate from disk
func loadCACertificate() (*x509.CertPool, error) {
	debug.Info("Loading CA certificate for HTTP client")
	certPool := x509.NewCertPool()

	// Try to load from disk
	certPath := filepath.Join(config.GetConfigDir(), "ca.crt")
	if _, err := os.Stat(certPath); err == nil {
		debug.Info("Found existing CA certificate at: %s", certPath)
		certData, err := os.ReadFile(certPath)
		if err != nil {
			debug.Error("Failed to read CA certificate: %v", err)
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		if !certPool.AppendCertsFromPEM(certData) {
			debug.Error("Failed to parse CA certificate")
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		debug.Info("Successfully loaded CA certificate from disk for file sync")
		return certPool, nil
	}

	debug.Error("CA certificate not found at: %s", certPath)
	return nil, fmt.Errorf("CA certificate not found")
}

// getEnvOrDefault returns the value of an environment variable or a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ScanDirectory scans a directory and returns information about all files
func (fs *FileSync) ScanDirectory(fileType string) ([]FileInfo, error) {
	dir, err := fs.GetFileTypeDir(fileType)
	if err != nil {
		return nil, err
	}

	debug.Info("Scanning directory %s for %s files", dir, fileType)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0750); err != nil {
		debug.Error("Failed to create directory %s: %v", dir, err)
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	var files []FileInfo

	// Special handling for binary directories which have subdirectories by ID
	if fileType == "binary" {
		// List all binary ID directories
		entries, err := os.ReadDir(dir)
		if err != nil {
			debug.Error("Error reading binary directory %s: %v", dir, err)
			return nil, fmt.Errorf("error reading binary directory: %w", err)
		}

		// For each subdirectory (binary ID)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue // Skip non-directories
			}

			// Each directory represents a binary ID
			binaryIDDir := filepath.Join(dir, entry.Name())
			debug.Info("Scanning binary ID directory: %s", binaryIDDir)

			// Check for archive files (.7z) in this directory
			archiveFiles, err := filepath.Glob(filepath.Join(binaryIDDir, "*.7z"))
			if err != nil {
				debug.Error("Error searching for archive files in %s: %v", binaryIDDir, err)
				continue
			}

			// Report each archive file
			for _, archivePath := range archiveFiles {
				archiveFilename := filepath.Base(archivePath)

				// Get file info
				fileInfo, err := os.Stat(archivePath)
				if err != nil {
					debug.Error("Error getting file info for %s: %v", archivePath, err)
					continue
				}

				// Calculate hash
				hash, err := fs.CalculateFileHash(archivePath)
				if err != nil {
					debug.Error("Error calculating hash for %s: %v", archivePath, err)
					continue
				}

				// Add archive file info to list
				files = append(files, FileInfo{
					Name:     archiveFilename,
					MD5Hash:  hash,
					Size:     fileInfo.Size(),
					FileType: fileType,
					ID:       fs.getBinaryIDFromPath(binaryIDDir),
				})

				debug.Info("Found binary archive: %s with ID %d", archiveFilename, fs.getBinaryIDFromPath(binaryIDDir))
			}

			// Check if this binary has already been extracted by looking for executable files
			extractedFiles, err := fs.FindExtractedExecutables(binaryIDDir)
			if err != nil {
				debug.Error("Error searching for extracted executables in %s: %v", binaryIDDir, err)
				continue
			}

			if len(extractedFiles) > 0 {
				debug.Info("Binary ID %s has %d extracted executable files", entry.Name(), len(extractedFiles))
			} else if len(archiveFiles) > 0 {
				// If we have archives but no extracted executables, extract them now
				debug.Info("Binary ID %s has archives but no executables, extracting during scan...", entry.Name())

				// Extract the first archive we find (usually there's just one)
				archivePath := archiveFiles[0]
				debug.Info("Extracting archive during scan: %s", filepath.Base(archivePath))

				if err := fs.ExtractBinary7z(archivePath, binaryIDDir); err != nil {
					debug.Error("Failed to extract binary archive during scan: %v", err)
				} else {
					debug.Info("Successfully extracted archive during scan")
				}
			}
		}
	} else {
		// Standard handling for non-binary files (wordlists, rules, etc.)
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				debug.Error("Error accessing path %s: %v", path, err)
				return nil // Continue walking despite errors
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Get file info
			info, err := d.Info()
			if err != nil {
				debug.Error("Error getting file info for %s: %v", path, err)
				return nil // Continue walking despite errors
			}

			// Calculate hash
			hash, err := fs.CalculateFileHash(path)
			if err != nil {
				debug.Error("Error calculating hash for %s: %v", path, err)
				return nil // Continue walking despite errors
			}

			// Extract relative path from the base directory for proper file reporting
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				relPath = d.Name() // Fallback to just the filename if we can't get relative path
			}

			// Add file info to list
			files = append(files, FileInfo{
				Name:     relPath,
				MD5Hash:  hash,
				Size:     info.Size(),
				FileType: fileType,
			})

			return nil
		})

		if err != nil {
			debug.Error("Error walking directory %s: %v", dir, err)
			return nil, fmt.Errorf("error scanning directory: %w", err)
		}
	}

	debug.Info("Found %d files in %s directory", len(files), fileType)
	return files, nil
}

// getBinaryIDFromPath extracts the binary ID from a path
func (fs *FileSync) getBinaryIDFromPath(path string) int {
	// Extract the last directory name which should be the ID
	dirName := filepath.Base(path)
	id, err := strconv.Atoi(dirName)
	if err != nil {
		debug.Error("Failed to parse binary ID from path %s: %v", path, err)
		return 0
	}
	return id
}

// FindExtractedExecutables checks if a binary has been extracted by looking for .bin or .exe files
func (fs *FileSync) FindExtractedExecutables(binaryDir string) ([]string, error) {
	// Look for .bin or .exe files recursively
	var execFiles []string

	// Walk the directory tree
	err := filepath.WalkDir(binaryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors and continue
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Check for executable extensions
		if strings.HasSuffix(strings.ToLower(d.Name()), ".bin") ||
			strings.HasSuffix(strings.ToLower(d.Name()), ".exe") {
			execFiles = append(execFiles, path)
		}

		return nil
	})

	return execFiles, err
}

// CalculateFileHash calculates the MD5 hash of a file (renamed from SHA-256)
func (fs *FileSync) CalculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ScanAllDirectories scans all data directories and returns information about all files
func (fs *FileSync) ScanAllDirectories(fileTypes []string) (map[string][]FileInfo, error) {
	result := make(map[string][]FileInfo)

	for _, fileType := range fileTypes {
		files, err := fs.ScanDirectory(fileType)
		if err != nil {
			debug.Error("Error scanning %s directory: %v", fileType, err)
			continue // Continue with other directories despite errors
		}
		result[fileType] = files
	}

	return result, nil
}

// DownloadFileFromInfo downloads a file using information from the FileInfo struct
// This ensures we can use the ID field for creating proper directory structures for binaries
func (fs *FileSync) DownloadFileFromInfo(ctx context.Context, fileInfo *FileInfo) error {
	// For binary files, check if we already have the executables extracted
	if fileInfo.FileType == "binary" && strings.HasSuffix(strings.ToLower(fileInfo.Name), ".7z") {
		binaryDir := filepath.Join(fs.dataDirs.Binaries, fmt.Sprintf("%d", fileInfo.ID))
		archivePath := filepath.Join(binaryDir, fileInfo.Name)

		// Check if the archive already exists and has the correct hash
		if _, err := os.Stat(archivePath); err == nil {
			// Archive exists, check hash
			hash, err := fs.CalculateFileHash(archivePath)
			if err == nil && hash == fileInfo.MD5Hash {
				// Hash matches, check if already extracted
				execFiles, err := fs.FindExtractedExecutables(binaryDir)
				if err == nil && len(execFiles) > 0 {
					debug.Info("Binary archive %s already extracted with %d executable files, skipping download and extraction",
						fileInfo.Name, len(execFiles))
					return nil
				}

				// Archive exists with correct hash but not extracted, extract it
				debug.Info("Binary archive %s exists with correct hash but executables not found, extracting...", fileInfo.Name)
				if err := fs.ExtractBinary7z(archivePath, binaryDir); err != nil {
					debug.Error("Failed to extract existing binary archive %s: %v", fileInfo.Name, err)
					return fmt.Errorf("failed to extract existing binary archive: %w", err)
				}
				debug.Info("Successfully extracted existing binary archive %s", fileInfo.Name)
				return nil
			}
		}
	}

	// Standard download flow for files that need to be downloaded
	return fs.DownloadFileWithInfoRetry(ctx, fileInfo, 0)
}

// DownloadFileWithInfoRetry downloads a file with retry logic using FileInfo struct
func (fs *FileSync) DownloadFileWithInfoRetry(ctx context.Context, fileInfo *FileInfo, retryCount int) error {
	// Note: Empty MD5Hash means skip verification (used for hashlists)

	// Get target directory based on file type
	var targetDir string
	var finalPath string

	switch fileInfo.FileType {
	case "wordlist":
		// The backend includes the category in the Name field (e.g., "general/file.txt")
		// We need to preserve this structure for proper organization
		targetDir = fs.dataDirs.Wordlists
		
		// Check if the name includes a category path
		if strings.Contains(fileInfo.Name, "/") {
			// Name includes category, use it as-is
			finalPath = filepath.Join(targetDir, fileInfo.Name)
			debug.Info("Wordlist download - Name includes category: %s -> %s", fileInfo.Name, finalPath)
		} else if fileInfo.Category != "" {
			// Use category field if available
			finalPath = filepath.Join(targetDir, fileInfo.Category, fileInfo.Name)
			debug.Info("Wordlist download - Using category field: %s/%s -> %s", fileInfo.Category, fileInfo.Name, finalPath)
		} else {
			// No category, save to root wordlists directory
			finalPath = filepath.Join(targetDir, fileInfo.Name)
			debug.Info("Wordlist download - No category: %s -> %s", fileInfo.Name, finalPath)
		}
	case "rule":
		// The backend includes the category in the Name field (e.g., "hashcat/file.rule")
		// We need to preserve this structure for proper organization
		targetDir = fs.dataDirs.Rules
		
		// Check if the name includes a category path
		if strings.Contains(fileInfo.Name, "/") {
			// Name includes category, use it as-is
			finalPath = filepath.Join(targetDir, fileInfo.Name)
			debug.Info("Rule download - Name includes category: %s -> %s", fileInfo.Name, finalPath)
		} else if fileInfo.Category != "" {
			// Use category field if available
			finalPath = filepath.Join(targetDir, fileInfo.Category, fileInfo.Name)
			debug.Info("Rule download - Using category field: %s/%s -> %s", fileInfo.Category, fileInfo.Name, finalPath)
		} else {
			// No category, save to root rules directory
			finalPath = filepath.Join(targetDir, fileInfo.Name)
			debug.Info("Rule download - No category: %s -> %s", fileInfo.Name, finalPath)
		}
	case "binary":
		// For binaries, create a directory structure using the binary ID
		if fileInfo.ID <= 0 {
			debug.Error("Binary download requires an ID but none was provided for %s", fileInfo.Name)
			return fmt.Errorf("binary download requires a valid ID")
		}

		// Create a directory named after the binary ID
		binaryDir := filepath.Join(fs.dataDirs.Binaries, fmt.Sprintf("%d", fileInfo.ID))
		targetDir = binaryDir
		finalPath = filepath.Join(binaryDir, fileInfo.Name)

		// Create the binary-specific directory
		if err := os.MkdirAll(binaryDir, 0750); err != nil {
			debug.Error("Failed to create binary directory %s: %v", binaryDir, err)
			return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
				fmt.Errorf("failed to create binary directory: %w", err))
		}
	case "hashlist":
		// Use the main hashlists directory
		targetDir = fs.dataDirs.Hashlists
		finalPath = filepath.Join(targetDir, fileInfo.Name)
		debug.Info("Hashlist download - Target dir: %s, Final path: %s", targetDir, finalPath)
	default:
		debug.Error("Unsupported file type: %s", fileInfo.FileType)
		return fmt.Errorf("unsupported file type: %s", fileInfo.FileType)
	}

	// Acquire semaphore slot
	select {
	case fs.sem <- struct{}{}:
		defer func() { <-fs.sem }()
	case <-ctx.Done():
		debug.Error("Context cancelled while waiting for download slot: %v", ctx.Err())
		return ctx.Err()
	}

	// Create parent directory for the final file path
	parentDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		debug.Error("Failed to create parent directory %s: %v", parentDir, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to create parent directory: %w", err))
	}

	tempPath := finalPath + ".tmp"

	debug.Info("Starting download of %s to %s (attempt %d/%d)",
		fileInfo.Name, finalPath, retryCount+1, fs.maxRetries+1)

	// Create temporary file
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		debug.Error("Failed to create temporary file %s: %v", tempPath, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to create temporary file: %w", err))
	}
	defer os.Remove(tempPath) // Clean up temp file on error

	// Create download URL
	var url string
	if fileInfo.FileType == "hashlist" && fileInfo.ID > 0 {
		// Hashlists use a different endpoint that requires the ID
		url = fmt.Sprintf("%s/api/agent/hashlists/%d/download", fs.urlConfig.BaseURL, fileInfo.ID)
	} else {
		// Other file types use the generic file endpoint
		url = fmt.Sprintf("%s/api/files/%s/%s", fs.urlConfig.BaseURL, fileInfo.FileType, fileInfo.Name)
	}
	debug.Info("Downloading file from %s", url)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		debug.Error("Failed to create request for %s: %v", url, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to create request: %w", err))
	}

	// Add authentication headers
	req.Header.Set("X-API-Key", fs.apiKey)
	req.Header.Set("X-Agent-ID", fs.agentID)

	// Send request
	resp, err := fs.client.Do(req)
	if err != nil {
		debug.Error("Failed to download file %s: %v", url, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("download failed: %w", err))
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		debug.Error("Download failed with status %d: %s", resp.StatusCode, body)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("download failed with status %d: %s", resp.StatusCode, body))
	}

	// Create hash writer to verify MD5
	h := md5.New()
	writer := io.MultiWriter(tempFile, h)

	// Copy response body to file and hash writer
	size, err := io.Copy(writer, resp.Body)
	if err != nil {
		debug.Error("Failed to write file %s: %v", tempPath, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to write file: %w", err))
	}

	// Close file before checking hash and moving
	if err := tempFile.Close(); err != nil {
		debug.Error("Failed to close temporary file %s: %v", tempPath, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to close temporary file: %w", err))
	}

	// Verify MD5 hash if provided
	if fileInfo.MD5Hash != "" {
		downloadedHash := fmt.Sprintf("%x", h.Sum(nil))
		if downloadedHash != fileInfo.MD5Hash {
			debug.Error("MD5 hash mismatch for %s: expected %s, got %s",
				fileInfo.Name, fileInfo.MD5Hash, downloadedHash)
			return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
				fmt.Errorf("md5 hash mismatch: expected %s, got %s", fileInfo.MD5Hash, downloadedHash))
		}
		debug.Info("MD5 hash verified for %s", fileInfo.Name)
	} else {
		debug.Info("Skipping MD5 verification for %s (no hash provided)", fileInfo.Name)
	}

	// Move temporary file to final location
	if err := os.Rename(tempPath, finalPath); err != nil {
		debug.Error("Failed to move file from %s to %s: %v", tempPath, finalPath, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to move temporary file: %w", err))
	}

	// For binary files, extract if it's a 7z archive
	if fileInfo.FileType == "binary" && strings.HasSuffix(strings.ToLower(fileInfo.Name), ".7z") {
		debug.Info("Extracting 7z binary archive: %s", finalPath)
		if err := fs.ExtractBinary7z(finalPath, targetDir); err != nil {
			debug.Error("Failed to extract binary archive %s: %v", fileInfo.Name, err)
			return fmt.Errorf("failed to extract binary archive: %w", err)
		}
		debug.Info("Successfully extracted binary archive %s", fileInfo.Name)
	}

	debug.Info("Successfully downloaded %s (%d bytes)", fileInfo.Name, size)
	return nil
}

// retryOrFailInfo handles retries for the FileInfo based download
func (fs *FileSync) retryOrFailInfo(ctx context.Context, fileInfo *FileInfo, retryCount int, err error) error {
	if retryCount >= fs.maxRetries {
		debug.Error("Max retries reached for %s: %v", fileInfo.Name, err)
		return fmt.Errorf("download failed after %d retries: %w", retryCount+1, err)
	}

	// Exponential backoff with jitter
	backoff := time.Duration(math.Pow(2, float64(retryCount))) * time.Second
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
	delay := backoff + jitter

	debug.Warning("Retrying download of %s in %v (attempt %d/%d): %v",
		fileInfo.Name, delay, retryCount+2, fs.maxRetries+1, err)

	select {
	case <-time.After(delay):
		return fs.DownloadFileWithInfoRetry(ctx, fileInfo, retryCount+1)
	case <-ctx.Done():
		debug.Error("Context cancelled while waiting for retry: %v", ctx.Err())
		return ctx.Err()
	}
}

// SyncDirectory synchronizes all files of a given type with the backend
func (fs *FileSync) SyncDirectory(ctx context.Context, fileType string) error {
	url := fmt.Sprintf("%s/api/files/%s/list", fs.urlConfig.BaseURL, fileType)
	debug.Info("Fetching file list from %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		debug.Error("Failed to create request for file list: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := fs.client.Do(req)
	if err != nil {
		debug.Error("Failed to fetch file list from %s: %v", url, err)
		return fmt.Errorf("failed to fetch file list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		debug.Error("Server returned non-200 status %d when fetching file list from %s", resp.StatusCode, url)
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	type FileListEntry struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
	}

	var files []FileListEntry
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		debug.Error("Failed to decode file list response: %v", err)
		return fmt.Errorf("failed to decode file list: %w", err)
	}

	debug.Info("Found %d files to sync for type %s", len(files), fileType)

	var wg sync.WaitGroup
	errMu := sync.Mutex{}
	errs := []error{}

	for _, file := range files {
		wg.Add(1)
		go func(file FileListEntry) {
			defer wg.Done()

			// Acquire semaphore slot
			fs.sem <- struct{}{}
			defer func() { <-fs.sem }()

			debug.Info("Starting download for file: %s", file.Name)
			fileInfo := &FileInfo{
				Name:     file.Name,
				MD5Hash:  file.Hash,
				FileType: fileType,
			}
			if err := fs.DownloadFileFromInfo(ctx, fileInfo); err != nil {
				debug.Error("Failed to download file %s: %v", file.Name, err)
				errMu.Lock()
				errs = append(errs, fmt.Errorf("failed to download %s: %w", file.Name, err))
				errMu.Unlock()
			} else {
				debug.Info("Successfully downloaded file: %s", file.Name)
			}
		}(file)
	}

	// Wait for all downloads to complete
	wg.Wait()

	// Collect any errors
	if len(errs) > 0 {
		debug.Error("Encountered %d errors while syncing %s directory", len(errs), fileType)
		for _, err := range errs {
			debug.Error("Sync error: %v", err)
		}
		return fmt.Errorf("encountered %d errors during sync", len(errs))
	}

	debug.Info("Successfully synchronized %s directory", fileType)
	return nil
}

// GetFileTypeDir returns the directory path for a given file type
func (fs *FileSync) GetFileTypeDir(fileType string) (string, error) {
	switch fileType {
	case "wordlist":
		return fs.dataDirs.Wordlists, nil
	case "rule":
		return fs.dataDirs.Rules, nil
	case "hashlist":
		return fs.dataDirs.Hashlists, nil
	case "binary":
		return fs.dataDirs.Binaries, nil
	default:
		return "", fmt.Errorf("unsupported file type: %s", fileType)
	}
}

// ExtractBinary7z extracts a 7z binary archive to the given directory
func (fs *FileSync) ExtractBinary7z(archivePath, targetDir string) error {
	debug.Info("Extracting 7z archive %s to %s", archivePath, targetDir)

	// Open the 7z archive using bodgit/sevenzip
	r, err := os.Open(archivePath)
	if err != nil {
		debug.Error("Failed to open archive file: %v", err)
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer r.Close()

	// Get file info for size
	fi, err := r.Stat()
	if err != nil {
		debug.Error("Failed to get archive file stats: %v", err)
		return fmt.Errorf("failed to get archive file stats: %w", err)
	}

	// Create a sevenzip reader
	sz, err := sevenzip.NewReader(r, fi.Size())
	if err != nil {
		debug.Error("Failed to create 7z reader: %v", err)
		return fmt.Errorf("failed to create 7z reader: %w", err)
	}

	// First, check if all files are inside a single top directory
	// If so, we'll strip that directory from the paths
	var commonPrefix string
	var hasCommonPrefix bool

	if len(sz.File) > 0 {
		// Gather all directory names
		var dirNames []string
		for _, file := range sz.File {
			dirPath := filepath.Dir(file.Name)
			if dirPath != "." {
				dirNames = append(dirNames, dirPath)
			}
		}

		// Check if all files share the same top-level directory
		if len(dirNames) > 0 {
			parts := strings.Split(dirNames[0], string(filepath.Separator))
			if len(parts) > 0 {
				topDir := parts[0]
				allInSameDir := true

				for _, dirName := range dirNames {
					parts := strings.Split(dirName, string(filepath.Separator))
					if len(parts) == 0 || parts[0] != topDir {
						allInSameDir = false
						break
					}
				}

				if allInSameDir {
					commonPrefix = topDir
					hasCommonPrefix = true
					debug.Info("All files in archive share common top directory: %s", commonPrefix)
				}
			}
		}
	}

	// Process each file in the archive
	for _, file := range sz.File {
		// Skip directories, they will be created as needed
		if file.FileInfo().IsDir() {
			continue
		}

		// Create output path, stripping common prefix if needed
		var outPath string
		if hasCommonPrefix {
			// Strip the common directory prefix if present
			relativePath := file.Name
			if strings.HasPrefix(relativePath, commonPrefix+string(filepath.Separator)) {
				relativePath = relativePath[len(commonPrefix)+1:]
				debug.Info("Stripping prefix from %s: result is %s", file.Name, relativePath)
			}
			outPath = filepath.Join(targetDir, relativePath)
		} else {
			outPath = filepath.Join(targetDir, file.Name)
		}

		debug.Info("Extracting file: %s to %s", file.Name, outPath)

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(outPath), 0750); err != nil {
			debug.Error("Failed to create directory for %s: %v", outPath, err)
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Open the file from the archive
		rc, err := file.Open()
		if err != nil {
			debug.Error("Failed to open file in archive: %v", err)
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		// Create the output file
		outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY, file.Mode())
		if err != nil {
			rc.Close()
			debug.Error("Failed to create output file %s: %v", outPath, err)
			return fmt.Errorf("failed to create output file: %w", err)
		}

		// Copy the content
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			debug.Error("Failed to extract file %s: %v", file.Name, err)
			return fmt.Errorf("failed to extract file: %w", err)
		}

		// Set executable permissions for binary files
		// Check if this is likely an executable (hashcat, hashcat.exe, hashcat.bin, etc.)
		baseName := filepath.Base(file.Name)
		isExecutable := strings.HasPrefix(baseName, "hashcat") || 
			strings.HasSuffix(file.Name, ".bin") || 
			strings.HasSuffix(file.Name, ".exe") ||
			(!strings.Contains(baseName, ".") && !file.FileInfo().IsDir())
		
		if isExecutable {
			debug.Info("Setting executable permissions for %s", outPath)
			if err := os.Chmod(outPath, 0755); err != nil {
				debug.Warning("Failed to set executable permissions for %s: %v", outPath, err)
				// Continue despite this error
			}
		}
	}

	debug.Info("Extraction completed successfully for %s", archivePath)
	return nil
}
