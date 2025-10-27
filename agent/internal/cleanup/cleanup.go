package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// CleanupService handles automatic cleanup of old files on the agent
type CleanupService struct {
	dataDirs       *config.DataDirs
	retentionDays  int
	ticker         *time.Ticker
	stop           chan struct{}
	wg             sync.WaitGroup
	mu             sync.Mutex
	lastCleanup    time.Time
	cleanupRunning bool
}

// NewCleanupService creates a new cleanup service
func NewCleanupService(dataDirs *config.DataDirs) *CleanupService {
	return &CleanupService{
		dataDirs:      dataDirs,
		retentionDays: 3, // 3-day retention policy
		stop:          make(chan struct{}),
	}
}

// Start begins the periodic cleanup process
func (cs *CleanupService) Start(ctx context.Context) {
	// Run cleanup every 6 hours
	cs.ticker = time.NewTicker(6 * time.Hour)

	cs.wg.Add(1)
	go func() {
		defer cs.wg.Done()

		// Run initial cleanup after a short delay
		time.Sleep(1 * time.Minute)
		cs.performCleanup(ctx)

		// Then run periodically
		for {
			select {
			case <-ctx.Done():
				debug.Info("Cleanup service stopping due to context cancellation")
				return
			case <-cs.stop:
				debug.Info("Cleanup service stopping")
				return
			case <-cs.ticker.C:
				cs.performCleanup(ctx)
			}
		}
	}()

	debug.Info("Cleanup service started with %d day retention policy", cs.retentionDays)
}

// Stop halts the cleanup service
func (cs *CleanupService) Stop() {
	if cs.ticker != nil {
		cs.ticker.Stop()
	}
	close(cs.stop)
	cs.wg.Wait()
	debug.Info("Cleanup service stopped")
}

// performCleanup executes the actual cleanup process
func (cs *CleanupService) performCleanup(ctx context.Context) {
	cs.mu.Lock()
	if cs.cleanupRunning {
		cs.mu.Unlock()
		debug.Debug("Cleanup already running, skipping")
		return
	}
	cs.cleanupRunning = true
	cs.mu.Unlock()

	defer func() {
		cs.mu.Lock()
		cs.cleanupRunning = false
		cs.lastCleanup = time.Now()
		cs.mu.Unlock()
	}()

	debug.Info("Starting cleanup of old files...")

	totalDeleted := 0
	totalSize := int64(0)

	// Clean hashlists
	deleted, size := cs.cleanupHashlists()
	totalDeleted += deleted
	totalSize += size

	// Clean rule chunks
	deleted, size = cs.cleanupRuleChunks()
	totalDeleted += deleted
	totalSize += size

	// Clean orphaned chunk ID files
	deleted, size = cs.cleanupChunkIDFiles()
	totalDeleted += deleted
	totalSize += size

	// Clean orphaned outfiles
	deleted, size = cs.cleanupOutfiles()
	totalDeleted += deleted
	totalSize += size

	if totalDeleted > 0 {
		debug.Info("Cleanup completed: deleted %d files, freed %s", totalDeleted, formatBytes(totalSize))
	} else {
		debug.Debug("Cleanup completed: no files to delete")
	}
}

// cleanupHashlists removes hashlist files older than retention period
func (cs *CleanupService) cleanupHashlists() (int, int64) {
	hashlistDir := cs.dataDirs.Hashlists
	if hashlistDir == "" {
		debug.Warning("Hashlist directory not configured")
		return 0, 0
	}

	return cs.cleanupDirectory(hashlistDir, []string{".txt", ".hash", ".lst", ".hashlist"}, "hashlist")
}

// cleanupRuleChunks removes temporary rule chunks older than retention period
func (cs *CleanupService) cleanupRuleChunks() (int, int64) {
	rulesDir := cs.dataDirs.Rules
	if rulesDir == "" {
		debug.Warning("Rules directory not configured")
		return 0, 0
	}

	// Rule chunks typically have patterns like "chunk_*" or contain "temp" in the name
	// We pass an empty pattern to check all files, then filter by chunk/temp in the function
	return cs.cleanupDirectoryWithPattern(rulesDir, "", "rule chunk")
}

// cleanupChunkIDFiles removes chunk ID tracking files when all chunks are gone
func (cs *CleanupService) cleanupChunkIDFiles() (int, int64) {
	rulesDir := cs.dataDirs.Rules
	if rulesDir == "" {
		return 0, 0
	}

	deleted := 0
	totalSize := int64(0)
	cutoffTime := time.Now().Add(-time.Duration(cs.retentionDays) * 24 * time.Hour)

	// Look for chunk ID files (typically named like "chunkid_*" or "*.chunkid")
	err := filepath.Walk(rulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Debug("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if this is a chunk ID file
		name := strings.ToLower(info.Name())
		if !strings.Contains(name, "chunkid") && !strings.HasSuffix(name, ".id") {
			return nil
		}

		// Check if file is older than retention period
		if info.ModTime().After(cutoffTime) {
			return nil // File is still within retention period
		}

		// Check if there are any associated chunks still present
		baseName := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		hasChunks := false

		chunkPattern := filepath.Join(filepath.Dir(path), baseName+"_chunk*")
		chunks, _ := filepath.Glob(chunkPattern)
		if len(chunks) > 0 {
			// Check if any chunks are within retention period
			for _, chunkPath := range chunks {
				chunkInfo, err := os.Stat(chunkPath)
				if err == nil && chunkInfo.ModTime().After(cutoffTime) {
					hasChunks = true
					break
				}
			}
		}

		// Delete the chunk ID file if no associated chunks exist
		if !hasChunks {
			size := info.Size()
			if err := os.Remove(path); err != nil {
				debug.Error("Failed to delete chunk ID file %s: %v", path, err)
			} else {
				debug.Debug("Deleted old chunk ID file: %s (age: %s, size: %d bytes)",
					path, time.Since(info.ModTime()), size)
				deleted++
				totalSize += size
			}
		}

		return nil
	})

	if err != nil {
		debug.Error("Error walking rules directory for chunk ID cleanup: %v", err)
	}

	return deleted, totalSize
}

// cleanupDirectory removes files in a directory older than retention period
func (cs *CleanupService) cleanupDirectory(dir string, extensions []string, fileType string) (int, int64) {
	deleted := 0
	totalSize := int64(0)
	cutoffTime := time.Now().Add(-time.Duration(cs.retentionDays) * 24 * time.Hour)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Debug("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check file extension if specified
		if len(extensions) > 0 {
			hasValidExt := false
			ext := strings.ToLower(filepath.Ext(info.Name()))
			for _, validExt := range extensions {
				if ext == validExt {
					hasValidExt = true
					break
				}
			}
			if !hasValidExt {
				return nil
			}
		}

		// Check if file is older than retention period based on modification time
		if info.ModTime().After(cutoffTime) {
			return nil // File is still within retention period
		}

		// Delete the file
		size := info.Size()
		if err := os.Remove(path); err != nil {
			debug.Error("Failed to delete %s %s: %v", fileType, path, err)
		} else {
			debug.Debug("Deleted old %s: %s (age: %s, size: %d bytes)",
				fileType, path, time.Since(info.ModTime()), size)
			deleted++
			totalSize += size
		}

		return nil
	})

	if err != nil {
		debug.Error("Error walking %s directory: %v", fileType, err)
	}

	return deleted, totalSize
}

// cleanupDirectoryWithPattern removes files matching a pattern older than retention period
func (cs *CleanupService) cleanupDirectoryWithPattern(dir string, pattern string, fileType string) (int, int64) {
	deleted := 0
	totalSize := int64(0)
	cutoffTime := time.Now().Add(-time.Duration(cs.retentionDays) * 24 * time.Hour)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Debug("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// If pattern is specified, check if filename contains it
		if pattern != "" && !strings.Contains(strings.ToLower(info.Name()), pattern) {
			return nil
		}

		// For rule chunks: only delete files that contain "chunk" or "temp"
		// This prevents deletion of base rule files
		if pattern == "" { // This is for rule chunks cleanup
			if !strings.Contains(strings.ToLower(info.Name()), "chunk") && !strings.Contains(strings.ToLower(info.Name()), "temp") {
				return nil
			}
		}

		// Check if file is older than retention period
		if info.ModTime().After(cutoffTime) {
			return nil // File is still within retention period
		}

		// Delete the file
		size := info.Size()
		if err := os.Remove(path); err != nil {
			debug.Error("Failed to delete %s %s: %v", fileType, path, err)
		} else {
			debug.Debug("Deleted old %s: %s (age: %s, size: %d bytes)",
				fileType, path, time.Since(info.ModTime()), size)
			deleted++
			totalSize += size
		}

		return nil
	})

	if err != nil {
		debug.Error("Error walking directory for %s cleanup: %v", fileType, err)
	}

	return deleted, totalSize
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetLastCleanupTime returns the time of the last cleanup
func (cs *CleanupService) GetLastCleanupTime() time.Time {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.lastCleanup
}

// ForceCleanup triggers an immediate cleanup
func (cs *CleanupService) ForceCleanup(ctx context.Context) {
	go cs.performCleanup(ctx)
}

// cleanupOutfiles removes orphaned hashcat outfiles older than 24 hours
func (cs *CleanupService) cleanupOutfiles() (int, int64) {
	// Get base directory from binaries path (parent directory)
	baseDir := filepath.Dir(cs.dataDirs.Binaries)
	outfileDir := filepath.Join(baseDir, "outfile")
	if _, err := os.Stat(outfileDir); os.IsNotExist(err) {
		return 0, 0 // Directory doesn't exist yet
	}

	// Clean outfiles older than 1 day (much shorter than other files)
	// These should be deleted immediately after task completion
	deleted := 0
	totalSize := int64(0)
	cutoffTime := time.Now().Add(-24 * time.Hour)

	err := filepath.Walk(outfileDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Debug("Error accessing path %s: %v", path, err)
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Only delete .txt files (our outfile format)
		if filepath.Ext(info.Name()) != ".txt" {
			return nil
		}

		// Check if older than 24 hours
		if info.ModTime().After(cutoffTime) {
			return nil
		}

		// Delete orphaned outfile
		size := info.Size()
		if err := os.Remove(path); err != nil {
			debug.Error("Failed to delete orphaned outfile %s: %v", path, err)
		} else {
			debug.Debug("Deleted orphaned outfile: %s (age: %s, size: %d bytes)",
				path, time.Since(info.ModTime()), size)
			deleted++
			totalSize += size
		}

		return nil
	})

	if err != nil {
		debug.Error("Error walking outfile directory for cleanup: %v", err)
	}

	return deleted, totalSize
}