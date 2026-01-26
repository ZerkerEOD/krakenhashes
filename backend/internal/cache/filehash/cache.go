package filehash

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CachedFileInfo stores file metadata and hash to avoid recalculation
type CachedFileInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
	MD5Hash string
}

// Cache provides thread-safe file hash caching
type Cache struct {
	entries map[string]CachedFileInfo
	mu      sync.RWMutex
}

// New creates a new file hash cache
func New() *Cache {
	return &Cache{
		entries: make(map[string]CachedFileInfo),
	}
}

// GetOrCalculate returns cached hash if valid, otherwise calculates and caches
func (c *Cache) GetOrCalculate(filePath string) (string, error) {
	// Get current file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Check cache (read lock)
	c.mu.RLock()
	cached, exists := c.entries[filePath]
	c.mu.RUnlock()

	// Cache hit: modTime and size unchanged
	if exists && cached.ModTime.Equal(fileInfo.ModTime()) && cached.Size == fileInfo.Size() {
		return cached.MD5Hash, nil
	}

	// Cache miss: calculate hash
	hash, err := calculateMD5(filePath)
	if err != nil {
		return "", err
	}

	// Update cache (write lock)
	c.mu.Lock()
	c.entries[filePath] = CachedFileInfo{
		Path:    filePath,
		ModTime: fileInfo.ModTime(),
		Size:    fileInfo.Size(),
		MD5Hash: hash,
	}
	c.mu.Unlock()

	return hash, nil
}

// Set manually updates a cache entry (used after file uploads)
func (c *Cache) Set(filePath string, modTime time.Time, size int64, md5Hash string) {
	c.mu.Lock()
	c.entries[filePath] = CachedFileInfo{
		Path:    filePath,
		ModTime: modTime,
		Size:    size,
		MD5Hash: md5Hash,
	}
	c.mu.Unlock()
}

// Invalidate removes an entry from cache
func (c *Cache) Invalidate(filePath string) {
	c.mu.Lock()
	delete(c.entries, filePath)
	c.mu.Unlock()
}

// Size returns number of cached entries
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// PopulateAsync starts background cache population for directories
func (c *Cache) PopulateAsync(directories []string, skipPatterns []string) {
	go func() {
		for _, dir := range directories {
			c.populateDirectory(dir, skipPatterns)
		}
	}()
}

func (c *Cache) populateDirectory(dir string, skipPatterns []string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		// Skip patterns (e.g., potfile, association/)
		for _, pattern := range skipPatterns {
			if strings.Contains(path, pattern) {
				return nil
			}
		}
		c.GetOrCalculate(path) // Populates cache
		return nil
	})
}

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
