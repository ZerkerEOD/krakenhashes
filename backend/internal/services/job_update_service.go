package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// JobUpdateService handles updates to jobs when files change
type JobUpdateService struct {
	presetJobRepo    repository.PresetJobRepository
	jobExecRepo      *repository.JobExecutionRepository
	jobTaskRepo      *repository.JobTaskRepository
	updateMutex      sync.RWMutex
	jobLocks         sync.Map
	isSystemUpdating bool
}

// NewJobUpdateService creates a new job update service
func NewJobUpdateService(
	presetJobRepo repository.PresetJobRepository,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
) *JobUpdateService {
	return &JobUpdateService{
		presetJobRepo: presetJobRepo,
		jobExecRepo:   jobExecRepo,
		jobTaskRepo:   jobTaskRepo,
	}
}

// StartUpdate marks the system as updating
func (s *JobUpdateService) StartUpdate(ctx context.Context) {
	s.updateMutex.Lock()
	s.isSystemUpdating = true
	debug.Log("Job update service: System update started", nil)
}

// FinishUpdate marks the system update as complete
func (s *JobUpdateService) FinishUpdate(ctx context.Context) {
	s.isSystemUpdating = false
	s.updateMutex.Unlock()
	debug.Log("Job update service: System update finished", nil)
}

// IsUpdating returns whether the system is currently updating
func (s *JobUpdateService) IsUpdating() bool {
	s.updateMutex.RLock()
	defer s.updateMutex.RUnlock()
	return s.isSystemUpdating
}

// HandleRuleUpdate handles updates when a rule file changes
func (s *JobUpdateService) HandleRuleUpdate(ctx context.Context, ruleID int, oldCount, newCount int64) error {
	debug.Log("Handling rule update", map[string]interface{}{
		"rule_id":   ruleID,
		"old_count": oldCount,
		"new_count": newCount,
	})

	// Rule splitting has been removed. Scheduler-v2 keyspace-splits every job and
	// derives keyspace from the job's own attack params, so a rule-file line-count
	// change no longer drives a per-job keyspace adjustment from this path
	// (matching the prior behavior for non-rule-split jobs, which were skipped).
	return nil
}

// HandleWordlistUpdate handles updates when a wordlist file changes
func (s *JobUpdateService) HandleWordlistUpdate(ctx context.Context, wordlistID int, oldLines, newLines int64) error {
	debug.Log("Handling wordlist update", map[string]interface{}{
		"wordlist_id": wordlistID,
		"old_lines":   oldLines,
		"new_lines":   newLines,
	})

	// Get all jobs that use this wordlist
	jobs, err := s.jobExecRepo.GetJobsByWordlistID(ctx, wordlistID)
	if err != nil {
		return fmt.Errorf("failed to get jobs by wordlist ID: %w", err)
	}

	for _, job := range jobs {
		// Lock this specific job
		s.lockJob(job.ID.String())
		defer s.unlockJob(job.ID.String())

		// Increment-mode jobs track keyspace PER LAYER; the job-level
		// base_keyspace/effective_keyspace are a derived sum maintained by the
		// progress calculator (calculateIncrementJobProgress). Rewriting them here
		// with a single wordlist line-count would clobber the layer sums with a
		// single-mask value and never touch the layers — exactly the drift that
		// made an increment job's effective_keyspace (denominator) diverge from
		// Σ layer effective. Leave increment jobs to their per-layer accounting.
		if job.IncrementMode != "" && job.IncrementMode != "off" {
			debug.Info("Wordlist update: job %s is increment-mode; keyspace is tracked per layer — skipping job-level keyspace rewrite", job.ID)
			continue
		}

		// Forward-only guard: if every word of the CURRENT base keyspace has
		// already been dispatched (max(keyspace_end) across non-failed tasks >=
		// base_keyspace), the job has no undispatched work left. Appending to the
		// wordlist must NOT resurrect a finished job — growing its keyspace here is
		// exactly what stranded a completed job in 'pending' forever (the
		// manufactured remainder never got a chunk). Skip it entirely.
		if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
			hasWork, hwErr := s.jobTaskRepo.HasUndispatchedBaseKeyspace(ctx, job.ID)
			if hwErr != nil {
				debug.Warning("Wordlist update: HasUndispatchedBaseKeyspace(job=%s): %v — proceeding with update", job.ID, hwErr)
			} else if !hasWork {
				debug.Info("Wordlist update: job %s already dispatched all %d base keyspace; skipping keyspace growth (forward-only)",
					job.ID, *job.BaseKeyspace)
				continue
			}
		}

		// Update base keyspace
		err = s.jobExecRepo.UpdateBaseKeyspace(ctx, job.ID, newLines)
		if err != nil {
			debug.Error("Failed to update base keyspace for job %s: %v", job.ID, err)
			continue
		}

		// Recalculate effective keyspace preserving the ACTUAL effective/base
		// ratio: newEffective = oldEffective × newBase / oldBase (big.Int,
		// overflow-safe). Using the exact ratio rather than the rounded,
		// display-only multiplication_factor keeps rule/salt keyspaces precise
		// across a wordlist change (a rounded ×2 vs a true ×2.9999 would strand
		// or over-dispatch work). job.EffectiveKeyspace/BaseKeyspace still hold
		// the pre-update values here (UpdateBaseKeyspace only touched the row).
		var newEffective models.BigInt
		switch {
		case job.EffectiveKeyspace != nil && job.EffectiveKeyspace.IsPositive() &&
			job.BaseKeyspace != nil && *job.BaseKeyspace > 0:
			newEffective = job.EffectiveKeyspace.MulInt64(newLines).DivInt64(*job.BaseKeyspace)
		case job.MultiplicationFactor > 0:
			// No prior effective/base pair to scale; fall back to base × rounded
			// multiplier (product can exceed int64).
			newEffective = models.NewBigInt(newLines).MulInt64(job.MultiplicationFactor)
		default:
			// Pure wordlist job without rules
			newEffective = models.NewBigInt(newLines)
		}

		err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, job.ID, newEffective)
		if err != nil {
			debug.Error("Failed to update effective keyspace for job %s: %v", job.ID, err)
		}

		debug.Log("Updated job keyspace for wordlist change", map[string]interface{}{
			"job_id":                job.ID,
			"new_base_keyspace":     newLines,
			"new_effective":         newEffective,
			"multiplication_factor": job.MultiplicationFactor,
		})
	}

	return nil
}

// lockJob locks a specific job for updates
func (s *JobUpdateService) lockJob(jobID string) {
	mu := &sync.Mutex{}
	actual, _ := s.jobLocks.LoadOrStore(jobID, mu)
	actual.(*sync.Mutex).Lock()
}

// unlockJob unlocks a specific job
func (s *JobUpdateService) unlockJob(jobID string) {
	if mu, ok := s.jobLocks.Load(jobID); ok {
		mu.(*sync.Mutex).Unlock()
	}
}
