package services

import (
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// ProcessingProgress represents the current state of a hashlist processing operation.
type ProcessingProgress struct {
	HashlistID     int64     `json:"hashlist_id"`
	Status         string    `json:"status"` // "pending", "processing", "completed", "failed"
	ProcessedLines int64     `json:"processed_lines"`
	TotalLines     int64     `json:"total_lines"` // Estimated from file size
	InsertedHashes int64     `json:"inserted_hashes"`
	StartedAt      time.Time `json:"started_at"`
	LastUpdateAt   time.Time `json:"last_update_at"`
	LinesPerSecond float64   `json:"lines_per_second"` // For ETA calculation
	Error          string    `json:"error,omitempty"`
}

// ProcessingProgressService manages hashlist processing progress tracking.
type ProcessingProgressService struct {
	progress sync.Map // map[int64]*ProcessingProgress
	mu       sync.Mutex
}

// NewProcessingProgressService creates a new ProcessingProgressService.
func NewProcessingProgressService() *ProcessingProgressService {
	svc := &ProcessingProgressService{}

	// Start background cleanup goroutine
	go svc.cleanupLoop()

	return svc
}

// StartProcessing initializes progress tracking for a hashlist.
func (s *ProcessingProgressService) StartProcessing(hashlistID int64, estimatedLines int64) {
	progress := &ProcessingProgress{
		HashlistID:   hashlistID,
		Status:       "pending",
		TotalLines:   estimatedLines,
		StartedAt:    time.Now(),
		LastUpdateAt: time.Now(),
	}
	s.progress.Store(hashlistID, progress)
	debug.Info("[ProcessingProgress] Started tracking for hashlist %d (estimated %d lines)", hashlistID, estimatedLines)
}

// UpdateProgress updates the progress for a hashlist being processed.
func (s *ProcessingProgressService) UpdateProgress(hashlistID int64, processedLines, insertedHashes int64) {
	if val, ok := s.progress.Load(hashlistID); ok {
		prog := val.(*ProcessingProgress)
		prog.Status = "processing"
		prog.ProcessedLines = processedLines
		prog.InsertedHashes = insertedHashes
		prog.LastUpdateAt = time.Now()

		// Calculate speed
		elapsed := time.Since(prog.StartedAt).Seconds()
		if elapsed > 0 {
			prog.LinesPerSecond = float64(processedLines) / elapsed
		}

		s.progress.Store(hashlistID, prog)
		debug.Debug("[ProcessingProgress] Hashlist %d: %d/%d lines (%.0f lines/sec)",
			hashlistID, processedLines, prog.TotalLines, prog.LinesPerSecond)
	}
}

// CompleteProcessing marks processing as complete.
func (s *ProcessingProgressService) CompleteProcessing(hashlistID int64, totalInserted int64) {
	if val, ok := s.progress.Load(hashlistID); ok {
		prog := val.(*ProcessingProgress)
		prog.Status = "completed"
		prog.InsertedHashes = totalInserted
		prog.ProcessedLines = prog.TotalLines // Ensure 100%
		prog.LastUpdateAt = time.Now()
		s.progress.Store(hashlistID, prog)
		debug.Info("[ProcessingProgress] Hashlist %d processing completed: %d hashes inserted", hashlistID, totalInserted)
	}
}

// FailProcessing marks processing as failed with an error message.
func (s *ProcessingProgressService) FailProcessing(hashlistID int64, errorMsg string) {
	if val, ok := s.progress.Load(hashlistID); ok {
		prog := val.(*ProcessingProgress)
		prog.Status = "failed"
		prog.Error = errorMsg
		prog.LastUpdateAt = time.Now()
		s.progress.Store(hashlistID, prog)
		debug.Error("[ProcessingProgress] Hashlist %d processing failed: %s", hashlistID, errorMsg)
	}
}

// GetProgress returns the current progress for a hashlist.
// Returns nil if no processing is in progress or recently completed.
func (s *ProcessingProgressService) GetProgress(hashlistID int64) *ProcessingProgress {
	if val, ok := s.progress.Load(hashlistID); ok {
		return val.(*ProcessingProgress)
	}
	return nil
}

// cleanupLoop periodically removes old completed/failed entries from the map.
func (s *ProcessingProgressService) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupOldEntries()
	}
}

// cleanupOldEntries removes entries that completed more than 10 minutes ago.
func (s *ProcessingProgressService) cleanupOldEntries() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	var toDelete []int64

	s.progress.Range(func(key, value interface{}) bool {
		hashlistID := key.(int64)
		prog := value.(*ProcessingProgress)

		// Only cleanup completed or failed entries older than 10 minutes
		if (prog.Status == "completed" || prog.Status == "failed") &&
			!prog.LastUpdateAt.IsZero() && prog.LastUpdateAt.Before(cutoff) {
			toDelete = append(toDelete, hashlistID)
		}
		return true
	})

	for _, id := range toDelete {
		s.progress.Delete(id)
		debug.Debug("[ProcessingProgress] Cleaned up old progress entry for hashlist %d", id)
	}
}
