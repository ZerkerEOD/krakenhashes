package sync

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// DownloadStatus represents the status of a download
type DownloadStatus string

const (
	DownloadStatusPending     DownloadStatus = "pending"
	DownloadStatusDownloading DownloadStatus = "downloading"
	DownloadStatusCompleted   DownloadStatus = "completed"
	DownloadStatusFailed      DownloadStatus = "failed"
)

// DownloadTask represents a file download task
type DownloadTask struct {
	FileInfo     FileInfo
	Status       DownloadStatus
	Progress     int64
	TotalSize    int64
	Error        error
	StartTime    time.Time
	CompletedAt  time.Time
	RetryCount   int
	CancelFunc   context.CancelFunc
}

// DownloadManager manages file downloads with deduplication and progress tracking
type DownloadManager struct {
	fileSync      *FileSync
	downloads     map[string]*DownloadTask // Map of file key to download task
	mu            sync.RWMutex
	maxConcurrent int
	semaphore     chan struct{}
	wg            sync.WaitGroup
	progressChan  chan DownloadProgress
}

// DownloadProgress represents download progress information
type DownloadProgress struct {
	FileName   string
	Progress   int64
	TotalSize  int64
	Percentage int
	Status     DownloadStatus
	Error      error
}

// NewDownloadManager creates a new download manager
func NewDownloadManager(fileSync *FileSync, maxConcurrent int) *DownloadManager {
	if maxConcurrent <= 0 {
		maxConcurrent = 3 // Default to 3 concurrent downloads
	}

	return &DownloadManager{
		fileSync:      fileSync,
		downloads:     make(map[string]*DownloadTask),
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
		progressChan:  make(chan DownloadProgress, 100),
	}
}

// GetProgressChannel returns the channel for receiving download progress updates
func (dm *DownloadManager) GetProgressChannel() <-chan DownloadProgress {
	return dm.progressChan
}

// generateFileKey generates a unique key for a file
func (dm *DownloadManager) generateFileKey(fileInfo FileInfo) string {
	// Create unique key based on file type, category, and name
	if fileInfo.Category != "" {
		return fmt.Sprintf("%s/%s/%s", fileInfo.FileType, fileInfo.Category, fileInfo.Name)
	}
	return fmt.Sprintf("%s/%s", fileInfo.FileType, fileInfo.Name)
}

// IsDownloading checks if a file is currently being downloaded
func (dm *DownloadManager) IsDownloading(fileInfo FileInfo) bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	key := dm.generateFileKey(fileInfo)
	task, exists := dm.downloads[key]
	if !exists {
		return false
	}

	// Check if download is active
	return task.Status == DownloadStatusPending || task.Status == DownloadStatusDownloading
}

// GetDownloadStatus returns the status of a download
func (dm *DownloadManager) GetDownloadStatus(fileInfo FileInfo) (DownloadStatus, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	key := dm.generateFileKey(fileInfo)
	task, exists := dm.downloads[key]
	if !exists {
		return "", fmt.Errorf("download not found")
	}

	return task.Status, task.Error
}

// QueueDownload queues a file for download with deduplication
func (dm *DownloadManager) QueueDownload(ctx context.Context, fileInfo FileInfo) error {
	key := dm.generateFileKey(fileInfo)

	dm.mu.Lock()
	// Check if already downloading or queued
	if existingTask, exists := dm.downloads[key]; exists {
		// If the download is active, don't queue again
		if existingTask.Status == DownloadStatusPending || existingTask.Status == DownloadStatusDownloading {
			dm.mu.Unlock()
			debug.Info("Download already in progress for %s, skipping duplicate", key)
			return nil
		}

		// If previous download failed and this is a retry, reset the task
		if existingTask.Status == DownloadStatusFailed {
			debug.Info("Retrying failed download for %s", key)
			existingTask.Status = DownloadStatusPending
			existingTask.RetryCount++
			existingTask.Error = nil
		} else if existingTask.Status == DownloadStatusCompleted {
			// Verify the file actually exists on disk before claiming "already downloaded"
			filePath := dm.fileSync.GetFilePath(fileInfo.FileType, fileInfo.Category, fileInfo.Name)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				// File was deleted - reset task for fresh download
				debug.Info("File %s was previously downloaded but no longer exists on disk, re-downloading", key)
				existingTask.Status = DownloadStatusPending
				existingTask.Error = nil
				existingTask.StartTime = time.Now()

				// Create new context for this download
				downloadCtx, cancel := context.WithCancel(ctx)
				existingTask.CancelFunc = cancel

				dm.wg.Add(1)
				go dm.downloadWorker(downloadCtx, key, existingTask)
				dm.mu.Unlock()
				return nil
			}

			// File exists - truly already downloaded
			dm.mu.Unlock()
			debug.Info("File %s already downloaded successfully (verified on disk)", key)
			return nil
		}
	} else {
		// Create new download task
		downloadCtx, cancel := context.WithCancel(ctx)
		task := &DownloadTask{
			FileInfo:   fileInfo,
			Status:     DownloadStatusPending,
			StartTime:  time.Now(),
			CancelFunc: cancel,
		}
		dm.downloads[key] = task

		// Start download goroutine
		dm.wg.Add(1)
		go dm.downloadWorker(downloadCtx, key, task)
	}
	dm.mu.Unlock()

	return nil
}

// downloadWorker performs the actual download
func (dm *DownloadManager) downloadWorker(ctx context.Context, key string, task *DownloadTask) {
	defer dm.wg.Done()

	// Acquire semaphore to limit concurrent downloads
	select {
	case dm.semaphore <- struct{}{}:
		defer func() { <-dm.semaphore }()
	case <-ctx.Done():
		dm.updateTaskStatus(key, DownloadStatusFailed, fmt.Errorf("context cancelled"))
		return
	}

	// Update status to downloading
	dm.updateTaskStatus(key, DownloadStatusDownloading, nil)

	// Send initial progress update
	dm.sendProgress(task.FileInfo.Name, 0, task.FileInfo.Size, 0, DownloadStatusDownloading, nil)

	// Show console status for this download
	console.Status("Downloading %s (%s)...", task.FileInfo.Name, console.FormatBytes(task.FileInfo.Size))

	// Perform the download using existing FileSync logic
	err := dm.fileSync.DownloadFileWithInfoRetry(ctx, &task.FileInfo, 0)

	if err != nil {
		dm.updateTaskStatus(key, DownloadStatusFailed, err)
		dm.sendProgress(task.FileInfo.Name, 0, task.FileInfo.Size, 0, DownloadStatusFailed, err)
		debug.Error("Failed to download %s: %v", key, err)
		console.Error("Failed to download %s: %v", task.FileInfo.Name, err)
	} else {
		task.CompletedAt = time.Now()
		dm.updateTaskStatus(key, DownloadStatusCompleted, nil)
		dm.sendProgress(task.FileInfo.Name, task.FileInfo.Size, task.FileInfo.Size, 100, DownloadStatusCompleted, nil)
		debug.Info("Successfully downloaded %s", key)
		// Success message will be shown by file sync completion
	}
}

// updateTaskStatus updates the status of a download task
func (dm *DownloadManager) updateTaskStatus(key string, status DownloadStatus, err error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if task, exists := dm.downloads[key]; exists {
		task.Status = status
		task.Error = err
		if status == DownloadStatusCompleted {
			task.CompletedAt = time.Now()
		}
	}
}

// sendProgress sends a progress update
func (dm *DownloadManager) sendProgress(fileName string, progress, total int64, percentage int, status DownloadStatus, err error) {
	select {
	case dm.progressChan <- DownloadProgress{
		FileName:   fileName,
		Progress:   progress,
		TotalSize:  total,
		Percentage: percentage,
		Status:     status,
		Error:      err,
	}:
	default:
		// Channel full, skip this update
	}
}

// CancelDownload cancels a specific download
func (dm *DownloadManager) CancelDownload(fileInfo FileInfo) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	key := dm.generateFileKey(fileInfo)
	task, exists := dm.downloads[key]
	if !exists {
		return fmt.Errorf("download not found")
	}

	if task.CancelFunc != nil {
		task.CancelFunc()
	}
	task.Status = DownloadStatusFailed
	task.Error = fmt.Errorf("cancelled by user")

	return nil
}

// CancelAll cancels all active downloads
func (dm *DownloadManager) CancelAll() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for _, task := range dm.downloads {
		if task.CancelFunc != nil && (task.Status == DownloadStatusPending || task.Status == DownloadStatusDownloading) {
			task.CancelFunc()
		}
	}
}

// Wait waits for all downloads to complete
func (dm *DownloadManager) Wait() {
	dm.wg.Wait()
}

// GetActiveDownloads returns the number of active downloads
func (dm *DownloadManager) GetActiveDownloads() int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	count := 0
	for _, task := range dm.downloads {
		if task.Status == DownloadStatusPending || task.Status == DownloadStatusDownloading {
			count++
		}
	}
	return count
}

// GetDownloadStats returns statistics about downloads
func (dm *DownloadManager) GetDownloadStats() (total, pending, downloading, completed, failed int) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	total = len(dm.downloads)
	for _, task := range dm.downloads {
		switch task.Status {
		case DownloadStatusPending:
			pending++
		case DownloadStatusDownloading:
			downloading++
		case DownloadStatusCompleted:
			completed++
		case DownloadStatusFailed:
			failed++
		}
	}
	return
}

// ClearCompleted removes completed downloads from tracking
func (dm *DownloadManager) ClearCompleted() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for key, task := range dm.downloads {
		if task.Status == DownloadStatusCompleted {
			delete(dm.downloads, key)
		}
	}
}

// ResetFailed resets failed downloads to pending status for retry
func (dm *DownloadManager) ResetFailed() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for _, task := range dm.downloads {
		if task.Status == DownloadStatusFailed {
			task.Status = DownloadStatusPending
			task.Error = nil
			task.RetryCount = 0
		}
	}
}