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
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"

	// Go library for archive extraction
	"github.com/bodgit/sevenzip"

	// Cross-platform disk usage (linux/mac/windows)
	"github.com/shirou/gopsutil/disk"
)

// freeDiskSpace returns the bytes available on the filesystem holding dir.
// Cross-platform via gopsutil. The bool is false if it could not be determined,
// in which case callers should proceed without blocking the download.
func freeDiskSpace(dir string) (uint64, bool) {
	usage, err := disk.Usage(dir)
	if err != nil || usage == nil {
		return 0, false
	}
	return usage.Free, true
}

// CachedFileInfo stores file metadata and hash to avoid recalculation
// Hash is only recalculated if mtime or size changes
type CachedFileInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
	MD5Hash string
}

// FileSync handles synchronization of files between the agent and backend
type FileSync struct {
	client     *http.Client
	urlConfig  *config.URLConfig
	dataDirs   *config.DataDirs
	sem        chan struct{} // Semaphore for limiting concurrent downloads
	maxRetries int           // Maximum number of retries for downloads
	apiKey     string        // API key for authentication
	agentID    string        // Agent ID for authentication

	// Progress tracking
	progressCallback func(fileName string, bytesReceived, totalBytes int64)
	multiProgress    *console.MultiProgress

	// File hash cache: key = absolute path, value = cached info
	// Avoids recalculating MD5 for unchanged files (major performance improvement for large wordlists)
	hashCache     map[string]CachedFileInfo
	hashCacheLock sync.RWMutex
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
	ID         int   `json:"id,omitempty"`          // ID in the backend database
	Timestamp  int64 `json:"timestamp,omitempty"`   // Last modified time
	AttackMode int   `json:"attack_mode,omitempty"` // For hashlists: determines download endpoint (9=original file)
}

// progressReader wraps an io.Reader and reports progress
type progressReader struct {
	reader        io.Reader
	fileName      string
	bytesRead     int64
	totalBytes    int64
	lastReported  int64
	lastTime      time.Time
	callback      func(fileName string, bytesReceived, totalBytes int64)
	multiProgress *console.MultiProgress
}

// newProgressReader creates a new progress reader
func newProgressReader(r io.Reader, fileName string, totalBytes int64, callback func(string, int64, int64), mp *console.MultiProgress) *progressReader {
	return &progressReader{
		reader:        r,
		fileName:      fileName,
		totalBytes:    totalBytes,
		lastTime:      time.Now(),
		callback:      callback,
		multiProgress: mp,
	}
}

// Read implements io.Reader interface with progress reporting
func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.bytesRead += int64(n)

	// Calculate speed
	now := time.Now()
	elapsed := now.Sub(pr.lastTime).Seconds()
	var speed int64
	if elapsed > 0 && pr.bytesRead > pr.lastReported {
		speed = int64(float64(pr.bytesRead-pr.lastReported) / elapsed)
	}

	// Report progress every 100KB or when complete
	if pr.bytesRead-pr.lastReported > 102400 || err == io.EOF {
		if pr.callback != nil {
			pr.callback(pr.fileName, pr.bytesRead, pr.totalBytes)
		}

		// Update multi-progress display
		if pr.multiProgress != nil {
			progress := console.DownloadProgress{
				FileName:      pr.fileName,
				BytesReceived: pr.bytesRead,
				TotalBytes:    pr.totalBytes,
				BytesPerSec:   speed,
			}
			pr.multiProgress.Update(pr.fileName, progress)
		}

		pr.lastReported = pr.bytesRead
		pr.lastTime = now
	}

	// Clear progress when complete
	if err == io.EOF && pr.multiProgress != nil {
		pr.multiProgress.Remove(pr.fileName)
	}

	return n, err
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

	// Create HTTP client with properly configured transport
	// This configuration ensures large file downloads don't timeout prematurely
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		// Proxy is explicit because a custom Transport defaults to a nil
		// Proxy. Cloud agents pull files through a userspace VPN's local
		// SOCKS5 proxy; without this, bulk file transfer would bypass the
		// tunnel while the WebSocket used it.
		Proxy: http.ProxyFromEnvironment,
		// Connection pool settings
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		MaxConnsPerHost:     5,
		// Timeout settings - these are for connection establishment, not transfer
		IdleConnTimeout:       90 * time.Second, // How long idle connections are kept
		TLSHandshakeTimeout:   10 * time.Second, // TLS handshake timeout
		ExpectContinueTimeout: 1 * time.Second,  // Timeout for 100-continue response
		// Disable HTTP/2 to avoid potential protocol issues with large downloads
		ForceAttemptHTTP2: false,
		// Keep-alive settings
		DisableKeepAlives: false,
		// Don't set ResponseHeaderTimeout as it would timeout slow downloads
		// The client-level timeout handles the overall request timeout
	}

	client := &http.Client{
		Timeout:   timeout, // This is the overall timeout for the entire request/response
		Transport: transport,
	}

	return &FileSync{
		client:        client,
		urlConfig:     urlConfig,
		dataDirs:      dataDirs,
		sem:           make(chan struct{}, maxDownloads),
		maxRetries:    maxRetries,
		apiKey:        apiKey,
		agentID:       agentID,
		multiProgress: console.NewMultiProgress(),
		hashCache:     make(map[string]CachedFileInfo),
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

			/*
			 * Report, do not extract.
			 *
			 * This function is an INVENTORY scan. It used to perform a
			 * multi-minute extraction inline, on two paths that are otherwise
			 * read-only: the async file-sync handler, which runs under a
			 * five-minute context, and PopulateHashCache on the BuildFileMap
			 * goroutine -- whose completion is what unblocks job acceptance. A
			 * slow disk here delayed the readiness gate or blew the budget
			 * outright.
			 *
			 * An unextracted archive is now healed a few seconds later by the
			 * async pre-check, or on demand by ensureBinary, both of which go
			 * through EnsureBinaryExtracted and are serialized.
			 */
			if IsBinaryExtracted(binaryIDDir) {
				debug.Info("Binary ID %s is extracted and usable", entry.Name())
			} else if len(archiveFiles) > 0 {
				debug.Info("Binary ID %s has an archive but is not usable yet; leaving it for "+
					"the extraction path rather than extracting inside a directory scan", entry.Name())
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

			// Normalize path separators to forward slashes for cross-platform compatibility
			// This ensures Windows paths like "general\file.txt" become "general/file.txt"
			normalizedPath := strings.ReplaceAll(relPath, "\\", "/")

			// Add file info to list
			files = append(files, FileInfo{
				Name:     normalizedPath,
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

// FindExtractedExecutables lists the executables under a binary directory.
//
// NOT a completion check -- use IsBinaryExtracted for that. A name appears here
// as soon as the file is created, which happens before its contents are
// written, so a non-empty result says nothing about whether the tree is usable.
func (fs *FileSync) FindExtractedExecutables(binaryDir string) ([]string, error) {
	// Look for .bin or .exe files recursively
	var execFiles []string

	// Walk the directory tree
	err := filepath.WalkDir(binaryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors and continue
		}

		if d.IsDir() {
			// Never descend into an in-flight or set-aside extraction. Those
			// hold files that are not published and may still be being
			// written; reporting them as the agent's executables would be a
			// slower-motion version of the partial-tree bug this whole path
			// exists to remove.
			name := d.Name()
			if path != binaryDir &&
				(strings.HasPrefix(name, extractTempPrefix) || strings.HasPrefix(name, extractOldPrefix)) {
				return filepath.SkipDir
			}
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

// CalculateFileHash calculates or retrieves cached MD5 hash of a file
// Uses a cache keyed by file path to avoid recalculating hashes for unchanged files.
// Cache hit is determined by matching both mtime and size - if either changed, hash is recalculated.
// This is a major performance improvement for large wordlists (e.g., 15GB crackstation.txt)
func (fs *FileSync) CalculateFileHash(filePath string) (string, error) {
	// Get current file info to check if cached hash is still valid
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Check cache first (read lock for performance)
	fs.hashCacheLock.RLock()
	cached, exists := fs.hashCache[filePath]
	fs.hashCacheLock.RUnlock()

	// If cached and file hasn't changed (same mtime and size), use cached hash
	if exists && cached.ModTime.Equal(fileInfo.ModTime()) && cached.Size == fileInfo.Size() {
		debug.Debug("Using cached hash for %s", filePath)
		return cached.MD5Hash, nil
	}

	// File is new or modified - calculate hash
	debug.Info("Calculating hash for %s (%.2f MB)", filePath, float64(fileInfo.Size())/1024/1024)

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	hashStr := hex.EncodeToString(hash.Sum(nil))

	// Update cache (write lock).
	//
	// The nil check matters because FileSync is not always built by
	// NewFileSync: tests construct bare literals, and a nil map assignment
	// panics rather than simply missing the cache. Hashing still returns the
	// right answer without a cache, so degrading to uncached is correct.
	fs.hashCacheLock.Lock()
	if fs.hashCache == nil {
		fs.hashCache = make(map[string]CachedFileInfo)
	}
	fs.hashCache[filePath] = CachedFileInfo{
		Path:    filePath,
		ModTime: fileInfo.ModTime(),
		Size:    fileInfo.Size(),
		MD5Hash: hashStr,
	}
	fs.hashCacheLock.Unlock()

	return hashStr, nil
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

// PopulateHashCache pre-populates the hash cache for all files in wordlist, rule, and binary directories.
// This should be called on agent startup to avoid slow first scan when the backend requests file inventory.
// The first scan will still take time to read and hash all files, but subsequent scans will be instant.
func (fs *FileSync) PopulateHashCache() error {
	debug.Info("Pre-populating file hash cache...")
	start := time.Now()

	fileTypes := []string{"wordlist", "rule", "binary", "charset"}
	var totalFiles int

	for _, fileType := range fileTypes {
		files, err := fs.ScanDirectory(fileType)
		if err != nil {
			debug.Warning("Failed to cache %s files: %v", fileType, err)
			continue
		}
		totalFiles += len(files)
		debug.Info("Cached %d %s files", len(files), fileType)
	}

	fs.hashCacheLock.RLock()
	cacheSize := len(fs.hashCache)
	fs.hashCacheLock.RUnlock()

	debug.Info("Hash cache populated with %d entries for %d files in %v", cacheSize, totalFiles, time.Since(start))
	return nil
}

// GetHashCacheSize returns the number of entries in the hash cache (for debugging/logging)
func (fs *FileSync) GetHashCacheSize() int {
	fs.hashCacheLock.RLock()
	defer fs.hashCacheLock.RUnlock()
	return len(fs.hashCache)
}

// DownloadFileFromInfo downloads a file using information from the FileInfo struct
// This ensures we can use the ID field for creating proper directory structures for binaries
func (fs *FileSync) DownloadFileFromInfo(ctx context.Context, fileInfo *FileInfo) error {
	// For binary files, check if we already have the executables extracted
	if fileInfo.FileType == "binary" && strings.HasSuffix(strings.ToLower(fileInfo.Name), ".7z") {
		binaryDir := filepath.Join(fs.dataDirs.Binaries, fmt.Sprintf("%d", fileInfo.ID))
		archivePath := filepath.Join(binaryDir, fileInfo.Name)

		// Archive already on disk with the right bytes: nothing to download,
		// and EnsureBinaryExtracted decides cheaply whether anything still
		// needs extracting. Delegating rather than deciding here is the point
		// -- this was one of the five places that answered "is it extracted?"
		// by looking for a filename, which a half-written tree satisfies.
		if _, err := os.Stat(archivePath); err == nil {
			hash, err := fs.CalculateFileHash(archivePath)
			if err == nil && hash == fileInfo.MD5Hash {
				if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
					debug.Error("Failed to extract existing binary archive %s: %v", fileInfo.Name, err)
					console.Error("Failed to extract binary archive %s: %v", fileInfo.Name, err)
					return fmt.Errorf("failed to extract existing binary archive: %w", err)
				}
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

	// Acquire a download slot BEFORE any retryOrFailInfo can run, so that helper
	// can always safely release/re-acquire the slot around its backoff (a
	// repeatedly-failing or slow download must not pin a slot and starve
	// task-critical downloads like the hashlist). Held for the whole attempt and
	// released on every return by the defer below.
	//
	// A nil sem means this FileSync was not built by NewFileSync, which is only
	// true of test literals. Sending on a nil channel blocks FOREVER, so
	// without this guard such a caller hangs rather than failing — and it did,
	// invisibly, because a panic in an earlier test was killing the binary
	// before this one ran. Treat "no semaphore" as "no configured limit".
	if fs.sem != nil {
		select {
		case fs.sem <- struct{}{}:
			defer func() { <-fs.sem }()
		case <-ctx.Done():
			debug.Error("Context cancelled while waiting for download slot: %v", ctx.Err())
			return ctx.Err()
		}
	}

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

		// If we have an explicit category field, it takes precedence
		if fileInfo.Category != "" {
			// Check if the name already starts with the category to avoid double directories
			if strings.HasPrefix(fileInfo.Name, fileInfo.Category+"/") {
				// Name already includes the category prefix, use as-is
				finalPath = filepath.Join(targetDir, fileInfo.Name)
				debug.Info("Rule download - Name already has category prefix: %s -> %s", fileInfo.Name, finalPath)
			} else {
				// Use category field and append the name (which may include subdirectories)
				finalPath = filepath.Join(targetDir, fileInfo.Category, fileInfo.Name)
				debug.Info("Rule download - Using category field: %s/%s -> %s", fileInfo.Category, fileInfo.Name, finalPath)
			}
		} else if strings.Contains(fileInfo.Name, "/") {
			// Name includes category path, use it as-is
			finalPath = filepath.Join(targetDir, fileInfo.Name)
			debug.Info("Rule download - Name includes category: %s -> %s", fileInfo.Name, finalPath)
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
	case "charset":
		targetDir = fs.dataDirs.Charsets
		finalPath = filepath.Join(targetDir, fileInfo.Name)
		debug.Info("Charset download - Target dir: %s, Final path: %s", targetDir, finalPath)
	case "client_potfile":
		// Client potfile: stored in wordlists/clients/{client_id}/potfile.txt
		// Category contains the client UUID
		targetDir = fs.dataDirs.Wordlists
		finalPath = filepath.Join(targetDir, "clients", fileInfo.Category, "potfile.txt")
		debug.Info("Client potfile download - Client: %s, Final path: %s", fileInfo.Category, finalPath)
	case "client_wordlist":
		// Client wordlist: stored in wordlists/clients/{client_id}/{filename}
		// Category contains the client UUID, Name contains the filename
		targetDir = fs.dataDirs.Wordlists
		finalPath = filepath.Join(targetDir, "clients", fileInfo.Category, fileInfo.Name)
		debug.Info("Client wordlist download - Client: %s, Name: %s, Final path: %s", fileInfo.Category, fileInfo.Name, finalPath)
	default:
		debug.Error("Unsupported file type: %s", fileInfo.FileType)
		return fmt.Errorf("unsupported file type: %s", fileInfo.FileType)
	}

	// Create parent directory for the final file path
	parentDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		debug.Error("Failed to create parent directory %s: %v", parentDir, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to create parent directory: %w", err))
	}

	// Disk-full guard (GH #40): when the expected size is known, fail fast with a
	// clear message instead of partially writing a file we can't finish. This is
	// a terminal failure (no retry) since the disk won't free up on its own.
	if fileInfo.Size > 0 {
		if free, ok := freeDiskSpace(parentDir); ok && free < uint64(fileInfo.Size) {
			debug.Error("Insufficient disk space for %s: need %d bytes, only %d free in %s",
				fileInfo.Name, fileInfo.Size, free, parentDir)
			return fmt.Errorf("insufficient disk space to download %s: need %d bytes, only %d available",
				fileInfo.Name, fileInfo.Size, free)
		}
	}

	tempPath := finalPath + ".tmp"

	debug.Info("Starting download of %s to %s (attempt %d/%d)",
		fileInfo.Name, finalPath, retryCount+1, fs.maxRetries+1)

	// Create temporary file
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		debug.Error("Failed to create temporary file %s: %v", tempPath, err)
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to create temporary file: %w", err))
	}
	defer os.Remove(tempPath) // Clean up temp file on error

	// Create download URL
	var url string
	if fileInfo.FileType == "hashlist" && fileInfo.ID > 0 {
		// Mode 9 (association attack) needs original file to preserve line order for 1:1 mapping
		// Other modes use DB streaming (deduped, removes cracked)
		if fileInfo.AttackMode == 9 {
			url = fmt.Sprintf("%s/api/agent/hashlists/%d/original", fs.urlConfig.BaseURL, fileInfo.ID)
		} else {
			url = fmt.Sprintf("%s/api/agent/hashlists/%d/download", fs.urlConfig.BaseURL, fileInfo.ID)
		}
	} else if fileInfo.FileType == "client_potfile" && fileInfo.Category != "" {
		// Client potfile: Category contains the client UUID
		url = fmt.Sprintf("%s/api/agent/client-potfiles/%s", fs.urlConfig.BaseURL, fileInfo.Category)
	} else if fileInfo.FileType == "client_wordlist" && fileInfo.Category != "" {
		// Client wordlist: Category contains the wordlist UUID
		url = fmt.Sprintf("%s/api/agent/client-wordlists/%s", fs.urlConfig.BaseURL, fileInfo.Category)
	} else {
		// For wordlists/rules, Name often contains the full path (e.g., "general/file.txt")
		// The Category field represents the classification enum (wordlist_type/rule_type) which
		// may not match the actual directory path in the filesystem.
		// We prioritize the path information in Name over the Category enum to avoid mismatches.
		//
		// The "rule chunks" special case from the old scheduler is gone. The
		// rewrite stacks whole rule files (-r r1.txt -r r2.txt) but never
		// splits one file. If a Category=="chunks" rule path arrives here
		// it's a legacy artifact and will fall through the normal name/path
		// dispatch below — likely 404'ing, which is the right signal that
		// something on the backend is sending stale data.
		if strings.Contains(fileInfo.Name, "/") {
			// Name contains path separator - use fallback route which extracts category from path
			// This handles cases where Category enum may not match the directory path
			// Example: file_name="general/file.txt", wordlist_type="custom" -> use path from Name
			// This prevents double-path issues like "general/general/file.txt"
			url = fmt.Sprintf("%s/api/files/%s/%s", fs.urlConfig.BaseURL, fileInfo.FileType, fileInfo.Name)
		} else if fileInfo.Category != "" {
			// Name is just a filename without path - use Category to build the path
			// This handles legacy files or files where category determines the directory
			url = fmt.Sprintf("%s/api/files/%s/%s/%s", fs.urlConfig.BaseURL, fileInfo.FileType, fileInfo.Category, fileInfo.Name)
		} else {
			// No category and no path in name - use direct path
			url = fmt.Sprintf("%s/api/files/%s/%s", fs.urlConfig.BaseURL, fileInfo.FileType, fileInfo.Name)
		}
	}
	// The global potfile is continuously appended, so its recorded md5 only covers
	// the prefix [0, Size). Ask the server for exactly that prefix (?bytes=N) so the
	// downloaded bytes verify against fileInfo.MD5Hash (see the backend's bounded
	// serve) instead of the larger current file — keeping verification meaningful
	// without ever accepting a stale copy. Immutable files set Size == on-disk size,
	// so bounding them is a harmless no-op.
	if fileInfo.FileType == "wordlist" && strings.HasSuffix(fileInfo.Name, "potfile.txt") && fileInfo.Size > 0 {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url = fmt.Sprintf("%s%sbytes=%d", url, sep, fileInfo.Size)
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

	// Get content length for progress tracking
	contentLength := resp.ContentLength
	if contentLength < 0 && fileInfo.Size > 0 {
		contentLength = fileInfo.Size
	}

	// Create progress reader to track download progress
	progressReader := newProgressReader(resp.Body, fileInfo.Name, contentLength, fs.progressCallback, fs.multiProgress)

	// Create hash writer to verify MD5
	h := md5.New()
	writer := io.MultiWriter(tempFile, h)

	// Copy response body to file and hash writer with progress tracking
	size, err := io.Copy(writer, progressReader)
	if err != nil {
		debug.Error("Failed to write file %s: %v", tempPath, err)
		// Clear progress on error
		if fs.multiProgress != nil {
			fs.multiProgress.Remove(fileInfo.Name)
		}
		// If the disk filled up mid-write, fail fast with a clear message rather
		// than retrying (the temp file is removed by the deferred cleanup).
		if free, ok := freeDiskSpace(parentDir); ok && free < 1024*1024 {
			return fmt.Errorf("ran out of disk space while downloading %s: %w", fileInfo.Name, err)
		}
		return fs.retryOrFailInfo(ctx, fileInfo, retryCount,
			fmt.Errorf("failed to write file: %w", err))
	}

	// Clear progress when complete
	if fs.multiProgress != nil {
		fs.multiProgress.Remove(fileInfo.Name)
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

	// Harden permissions for sensitive file types (owner-only read/write)
	switch fileInfo.FileType {
	case "hashlist", "client_potfile", "client_wordlist":
		if err := os.Chmod(finalPath, 0600); err != nil {
			debug.Warning("Failed to set restricted permissions on %s: %v", finalPath, err)
		}
	}

	// For binary files, extract if it's a 7z archive. Through
	// EnsureBinaryExtracted so this cannot race a concurrent extraction of the
	// same directory, and so the completion marker is written.
	if fileInfo.FileType == "binary" && strings.HasSuffix(strings.ToLower(fileInfo.Name), ".7z") {
		debug.Info("Extracting 7z binary archive: %s", finalPath)
		if err := fs.EnsureBinaryExtracted(finalPath, targetDir); err != nil {
			debug.Error("Failed to extract binary archive %s: %v", fileInfo.Name, err)
			console.Error("Failed to extract binary archive %s: %v", fileInfo.Name, err)
			return fmt.Errorf("failed to extract binary archive: %w", err)
		}
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

	// Release the download slot for the duration of the backoff + next attempt.
	// This function is always reached while the caller (DownloadFileWithInfoRetry)
	// holds one fs.sem slot; without this, a repeatedly-failing or slow download
	// would pin that slot across every backoff sleep and nested retry, starving
	// task-critical downloads (e.g. the hashlist) that share the same pool. The
	// recursive DownloadFileWithInfoRetry re-acquires its own slot for the retry;
	// we then re-acquire one here so the caller's deferred `<-fs.sem` stays balanced.
	// Mirrors the nil guard on acquisition: with no semaphore there is no slot
	// to release or re-take, and both operations would block forever.
	reacquire := func() {}
	if fs.sem != nil {
		<-fs.sem
		reacquire = func() { fs.sem <- struct{}{} }
	}

	select {
	case <-time.After(delay):
		err := fs.DownloadFileWithInfoRetry(ctx, fileInfo, retryCount+1)
		reacquire()
		return err
	case <-ctx.Done():
		debug.Error("Context cancelled while waiting for retry: %v", ctx.Err())
		reacquire()
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

			// NOTE: do NOT acquire fs.sem here. DownloadFileFromInfo ->
			// DownloadFileWithInfoRetry already acquires a slot for the actual
			// transfer. Acquiring here too made every download hold TWO slots,
			// which both halved effective concurrency and could deadlock (with a
			// pool of N, N goroutines holding the outer slot would all block
			// forever on the inner acquire). The inner semaphore is the real
			// concurrency bound; excess goroutines simply park on it.

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
	case "hashlist_original":
		// Original hashlists are stored in hashlists/original/ subdirectory
		return filepath.Join(fs.dataDirs.Hashlists, "original"), nil
	case "binary":
		return fs.dataDirs.Binaries, nil
	case "charset":
		return fs.dataDirs.Charsets, nil
	default:
		return "", fmt.Errorf("unsupported file type: %s", fileType)
	}
}

// GetFilePath returns the full path where a file would be stored on disk.
// This mirrors the path construction logic in DownloadFileWithInfoRetry.
func (fs *FileSync) GetFilePath(fileType, category, name string) string {
	switch fileType {
	case "wordlist":
		if strings.Contains(name, "/") {
			// Name includes category path
			return filepath.Join(fs.dataDirs.Wordlists, name)
		} else if category != "" {
			return filepath.Join(fs.dataDirs.Wordlists, category, name)
		}
		return filepath.Join(fs.dataDirs.Wordlists, name)
	case "rule":
		if category != "" {
			if strings.HasPrefix(name, category+"/") {
				return filepath.Join(fs.dataDirs.Rules, name)
			}
			return filepath.Join(fs.dataDirs.Rules, category, name)
		} else if strings.Contains(name, "/") {
			return filepath.Join(fs.dataDirs.Rules, name)
		}
		return filepath.Join(fs.dataDirs.Rules, name)
	case "hashlist":
		return filepath.Join(fs.dataDirs.Hashlists, name)
	default:
		// For other types, just use the base directory
		baseDir, err := fs.GetFileTypeDir(fileType)
		if err != nil {
			return ""
		}
		return filepath.Join(baseDir, name)
	}
}

// ExtractBinary7z extracts a 7z binary archive to the given directory
/*
 * ExtractBinary7z extracts an archive directly into targetDir.
 *
 * DEPRECATED for callers: use EnsureBinaryExtracted instead. This writes in
 * place with no lock and no completion marker, so a concurrent caller can
 * interleave writes into the same files and every "is it extracted?" probe can
 * observe a half-written tree. It survives only as the mechanism
 * EnsureBinaryExtracted drives against a private staging directory.
 */
func (fs *FileSync) ExtractBinary7z(archivePath, targetDir string) error {
	return fs.extractTo(archivePath, targetDir)
}

// extractTo writes every file in the archive under targetDir. The caller is
// responsible for exclusion and for making the result visible atomically.
func (fs *FileSync) extractTo(archivePath, targetDir string) error {
	debug.Info("Extracting 7z archive %s to %s", archivePath, targetDir)

	r, err := os.Open(archivePath)
	if err != nil {
		debug.Error("Failed to open archive file: %v", err)
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer r.Close()

	fi, err := r.Stat()
	if err != nil {
		debug.Error("Failed to get archive file stats: %v", err)
		return fmt.Errorf("failed to get archive file stats: %w", err)
	}

	sz, err := sevenzip.NewReader(r, fi.Size())
	if err != nil {
		debug.Error("Failed to create 7z reader: %v", err)
		return fmt.Errorf("failed to create 7z reader: %w", err)
	}

	commonPrefix, hasCommonPrefix := archiveCommonPrefix(sz)

	for _, file := range sz.File {
		if file.FileInfo().IsDir() {
			continue
		}

		outPath := filepath.Join(targetDir, archiveRelPath(file.Name, commonPrefix, hasCommonPrefix))

		if err := os.MkdirAll(filepath.Dir(outPath), 0750); err != nil {
			debug.Error("Failed to create directory for %s: %v", outPath, err)
			return fmt.Errorf("failed to create directory: %w", err)
		}

		rc, err := file.Open()
		if err != nil {
			debug.Error("Failed to open file in archive: %v", err)
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		/*
		 * The mode is chosen HERE rather than chmod'd after the copy.
		 *
		 * A post-copy chmod leaves a window in which the file is complete but
		 * not yet executable, and more importantly file.Mode() from a
		 * Windows-produced 7z is frequently 0666 -- so the executable bit was
		 * doing real work and cannot simply be dropped.
		 *
		 * O_EXCL is correct now that the target is a private staging directory:
		 * a collision means a duplicate entry in the archive, which should be
		 * surfaced rather than silently interleaved.
		 */
		mode := file.Mode()
		if isLikelyExecutable(file.Name, file.FileInfo().IsDir()) {
			mode = 0755
		}

		outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, mode)
		if err != nil {
			rc.Close()
			debug.Error("Failed to create output file %s: %v", outPath, err)
			return fmt.Errorf("failed to create output file: %w", err)
		}

		_, copyErr := io.Copy(outFile, rc)
		if copyErr == nil {
			// Flush before the rename that publishes this name, so a crash
			// cannot leave a visible file with unwritten contents.
			copyErr = outFile.Sync()
		}
		outFile.Close()
		rc.Close()

		if copyErr != nil {
			debug.Error("Failed to extract file %s: %v", file.Name, copyErr)
			return fmt.Errorf("failed to extract file: %w", copyErr)
		}
	}

	debug.Info("Extraction completed successfully for %s", archivePath)
	return nil
}

// isLikelyExecutable mirrors the heuristic the extractor has always used.
func isLikelyExecutable(name string, isDir bool) bool {
	baseName := filepath.Base(name)
	return strings.HasPrefix(baseName, "hashcat") ||
		strings.HasSuffix(name, ".bin") ||
		strings.HasSuffix(name, ".exe") ||
		(!strings.Contains(baseName, ".") && !isDir)
}

// archiveCommonPrefix reports the single top-level directory every entry sits
// under, if there is one, so it can be stripped on extraction.
func archiveCommonPrefix(sz *sevenzip.Reader) (string, bool) {
	if len(sz.File) == 0 {
		return "", false
	}

	var dirNames []string
	for _, file := range sz.File {
		normalizedName := strings.ReplaceAll(file.Name, "\\", "/")
		dirPath := filepath.ToSlash(filepath.Dir(normalizedName))
		if dirPath != "." {
			dirNames = append(dirNames, dirPath)
		}
	}
	if len(dirNames) == 0 {
		return "", false
	}

	parts := strings.Split(dirNames[0], "/")
	if len(parts) == 0 {
		return "", false
	}
	topDir := parts[0]
	for _, dirName := range dirNames {
		p := strings.Split(dirName, "/")
		if len(p) == 0 || p[0] != topDir {
			return "", false
		}
	}
	debug.Info("All files in archive share common top directory: %s", topDir)
	return topDir, true
}
