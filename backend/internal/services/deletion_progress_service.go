package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// DeletionProgress represents the current state of a hashlist deletion operation.
type DeletionProgress struct {
	HashlistID      int64     `json:"hashlist_id"`
	Status          string    `json:"status"` // "pending", "deleting_hashes", "clearing_references", "cleaning_orphans", "finalizing", "completed", "failed"
	Phase           string    `json:"phase"`  // Human-readable phase description
	Checked         int64     `json:"checked"`
	Total           int64     `json:"total"`
	Deleted         int64     `json:"deleted"`          // orphan hashes deleted
	RefsCleared     int64     `json:"refs_cleared"`     // cracked_by_task_id cleared count
	RefsTotal       int64     `json:"refs_total"`       // total refs to clear
	JobsDeleted     int64     `json:"jobs_deleted"`     // job_executions deleted
	SharedPreserved int64     `json:"shared_preserved"` // hashes preserved (in other lists)
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	Duration        string    `json:"duration,omitempty"` // human-readable duration
	Error           string    `json:"error,omitempty"`
}

// DeletionProgressService manages async hashlist deletions and tracks their progress.
type DeletionProgressService struct {
	progress     sync.Map // map[int64]*DeletionProgress
	hashlistRepo *repository.HashListRepository
	mu           sync.Mutex // protects cleanup operations
}

// NewDeletionProgressService creates a new DeletionProgressService.
func NewDeletionProgressService(hashlistRepo *repository.HashListRepository) *DeletionProgressService {
	svc := &DeletionProgressService{
		hashlistRepo: hashlistRepo,
	}

	// Start background cleanup goroutine
	go svc.cleanupLoop()

	return svc
}

// StartDeletion initiates an async deletion and returns immediately.
// Returns true if deletion was started, false if already in progress.
func (s *DeletionProgressService) StartDeletion(hashlistID int64) bool {
	// Check if already in progress
	if existing, ok := s.progress.Load(hashlistID); ok {
		prog := existing.(*DeletionProgress)
		// Check all active status values
		switch prog.Status {
		case "pending", "deleting_hashes", "clearing_references", "cleaning_orphans", "finalizing":
			debug.Warning("[DeletionProgress] Deletion for hashlist %d already in progress (status: %s)", hashlistID, prog.Status)
			return false
		}
	}

	// Create initial progress entry
	progress := &DeletionProgress{
		HashlistID: hashlistID,
		Status:     "pending",
		Checked:    0,
		Total:      0,
		Deleted:    0,
		StartedAt:  time.Now(),
	}
	s.progress.Store(hashlistID, progress)

	// Start deletion in background goroutine
	go s.runDeletion(hashlistID)

	debug.Info("[DeletionProgress] Started async deletion for hashlist %d", hashlistID)
	return true
}

// GetProgress returns the current progress for a hashlist deletion.
// Returns nil if no deletion is in progress or recently completed.
func (s *DeletionProgressService) GetProgress(hashlistID int64) *DeletionProgress {
	if val, ok := s.progress.Load(hashlistID); ok {
		return val.(*DeletionProgress)
	}
	return nil
}

// runDeletion performs the actual deletion with progress updates.
func (s *DeletionProgressService) runDeletion(hashlistID int64) {
	// Create a context with 2-hour timeout for very large hashlists
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	// Count jobs BEFORE deletion (they'll be CASCADE deleted)
	jobCount, err := s.hashlistRepo.CountJobExecutions(ctx, hashlistID)
	if err != nil {
		debug.Warning("[DeletionProgress] Could not count jobs for hashlist %d: %v", hashlistID, err)
		jobCount = 0 // Continue anyway, this is just for stats
	}
	debug.Info("[DeletionProgress] Hashlist %d has %d job_executions (will be CASCADE deleted)", hashlistID, jobCount)

	// Update status to deleting_hashes and store job count
	if val, ok := s.progress.Load(hashlistID); ok {
		prog := val.(*DeletionProgress)
		prog.Status = "deleting_hashes"
		prog.Phase = "Removing hashes"
		prog.JobsDeleted = jobCount // Store count before deletion
		s.progress.Store(hashlistID, prog)
	}

	// Define progress callback that handles phase updates
	onProgress := func(status, phase string, checked, total, deleted, refsCleared, refsTotal int64) {
		if val, ok := s.progress.Load(hashlistID); ok {
			prog := val.(*DeletionProgress)
			prog.Status = status
			prog.Phase = phase
			prog.Checked = checked
			prog.Total = total
			prog.Deleted = deleted
			prog.RefsCleared = refsCleared
			prog.RefsTotal = refsTotal
			s.progress.Store(hashlistID, prog)
			debug.Debug("[DeletionProgress] Hashlist %d [%s]: checked %d/%d, deleted %d, refs %d/%d",
				hashlistID, status, checked, total, deleted, refsCleared, refsTotal)
		}
	}

	// Perform deletion with progress callback using streaming method for low memory usage
	err = s.hashlistRepo.DeleteWithProgressStreaming(ctx, hashlistID, onProgress)

	// Update final status with stats
	if val, ok := s.progress.Load(hashlistID); ok {
		prog := val.(*DeletionProgress)
		prog.CompletedAt = time.Now()

		// Calculate duration
		duration := prog.CompletedAt.Sub(prog.StartedAt)
		prog.Duration = formatDuration(duration)

		// Calculate shared preserved (hashes that were in other hashlists)
		prog.SharedPreserved = prog.Total - prog.Deleted

		if err != nil {
			prog.Status = "failed"
			prog.Error = err.Error()
			debug.Error("[DeletionProgress] Hashlist %d deletion failed: %v", hashlistID, err)
		} else {
			prog.Status = "completed"
			debug.Info("[DeletionProgress] Hashlist %d deletion completed: %d total hashes, %d orphans deleted, %d shared preserved, %d jobs deleted, duration: %s",
				hashlistID, prog.Total, prog.Deleted, prog.SharedPreserved, prog.JobsDeleted, prog.Duration)
		}

		s.progress.Store(hashlistID, prog)
	}
}

// formatDuration converts a time.Duration into a human-readable string (e.g., "5m 32s")
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// cleanupLoop periodically removes old completed/failed entries from the map.
func (s *DeletionProgressService) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupOldEntries()
	}
}

// cleanupOldEntries removes entries that completed more than 5 minutes ago.
func (s *DeletionProgressService) cleanupOldEntries() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	var toDelete []int64

	s.progress.Range(func(key, value interface{}) bool {
		hashlistID := key.(int64)
		prog := value.(*DeletionProgress)

		// Only cleanup completed or failed entries older than 5 minutes
		if (prog.Status == "completed" || prog.Status == "failed") &&
			!prog.CompletedAt.IsZero() && prog.CompletedAt.Before(cutoff) {
			toDelete = append(toDelete, hashlistID)
		}
		return true
	})

	for _, id := range toDelete {
		s.progress.Delete(id)
		debug.Debug("[DeletionProgress] Cleaned up old progress entry for hashlist %d", id)
	}
}
