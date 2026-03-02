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
	presetJobRepo      repository.PresetJobRepository
	jobExecRepo        *repository.JobExecutionRepository
	jobTaskRepo        *repository.JobTaskRepository
	updateMutex        sync.RWMutex
	jobLocks           sync.Map
	isSystemUpdating   bool
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

	// Get all jobs that use this rule
	jobs, err := s.jobExecRepo.GetJobsByRuleID(ctx, ruleID)
	if err != nil {
		return fmt.Errorf("failed to get jobs by rule ID: %w", err)
	}

	for _, job := range jobs {
		// Skip non-rule-splitting jobs
		if !job.UsesRuleSplitting {
			continue
		}

		// Lock this specific job
		s.lockJob(job.ID.String())
		defer s.unlockJob(job.ID.String())

		// Check if job has any tasks
		taskCount, err := s.jobTaskRepo.GetTaskCountByJobExecution(ctx, job.ID)
		if err != nil {
			debug.Error("Failed to get task count for job %s: %v", job.ID, err)
			continue
		}

		if taskCount == 0 {
			// No tasks yet - simple recalculation
			s.recalculateJobKeyspace(ctx, &job, newCount)
		} else {
			// Has tasks - adjust for remaining work only
			s.adjustRemainingKeyspace(ctx, &job, oldCount, newCount)
		}
	}

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

		// Update base keyspace
		err = s.jobExecRepo.UpdateBaseKeyspace(ctx, job.ID, newLines)
		if err != nil {
			debug.Error("Failed to update base keyspace for job %s: %v", job.ID, err)
			continue
		}

		// For rule-splitting jobs, recalculate effective keyspace accounting for missed work
		if job.UsesRuleSplitting && job.MultiplicationFactor > 0 {
			// Calculate the theoretical new effective keyspace
			theoreticalNewEffective := newLines * job.MultiplicationFactor

			// Calculate how many words were added
			wordsDifference := newLines - oldLines

			// Get the highest rule chunk that's been dispatched
			maxRuleEnd, err := s.jobTaskRepo.GetMaxRuleEndIndex(ctx, job.ID)
			if err != nil {
				debug.Error("Failed to get max rule end for job %s: %v", job.ID, err)
				// Fall back to simple update if we can't determine dispatched rules
				err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, job.ID, theoreticalNewEffective)
				if err != nil {
					debug.Error("Failed to update effective keyspace for job %s: %v", job.ID, err)
				}
				continue
			}

			// Calculate the "missed" keyspace (words added × rules already dispatched)
			missedKeyspace := int64(0)
			if maxRuleEnd != nil && *maxRuleEnd > 0 {
				missedKeyspace = wordsDifference * int64(*maxRuleEnd)
			}

			// Actual effective keyspace = theoretical - missed
			actualEffective := theoreticalNewEffective - missedKeyspace

			err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, job.ID, actualEffective)
			if err != nil {
				debug.Error("Failed to update effective keyspace for job %s: %v", job.ID, err)
			}

			debug.Log("Updated job keyspace for wordlist change", map[string]interface{}{
				"job_id":                job.ID,
				"new_base_keyspace":     newLines,
				"old_base_keyspace":     oldLines,
				"words_added":           wordsDifference,
				"multiplication_factor": job.MultiplicationFactor,
				"rules_dispatched":      maxRuleEnd,
				"missed_keyspace":       missedKeyspace,
				"theoretical_effective": theoreticalNewEffective,
				"actual_effective":      actualEffective,
			})
		} else {
			// Non-rule-splitting job
			var newEffective int64
			if job.MultiplicationFactor > 0 {
				// Job has rules but doesn't use rule splitting
				newEffective = newLines * job.MultiplicationFactor
			} else {
				// Pure wordlist job without rules
				newEffective = newLines
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
	}

	return nil
}

// recalculateJobKeyspace recalculates keyspace for jobs with no tasks
func (s *JobUpdateService) recalculateJobKeyspace(ctx context.Context, job *models.JobExecution, newRuleCount int64) {
	if job.BaseKeyspace == nil {
		debug.Warning("Job %s has no base keyspace, skipping recalculation", job.ID)
		return
	}

	newMultFactor := newRuleCount
	newEffective := *job.BaseKeyspace * newMultFactor

	updates := map[string]interface{}{
		"multiplication_factor": newMultFactor,
		"effective_keyspace":   newEffective,
	}

	err := s.jobExecRepo.UpdateKeyspaceMetrics(ctx, job.ID, updates)
	if err != nil {
		debug.Error("Failed to update keyspace metrics for job %s: %v", job.ID, err)
		return
	}

	debug.Log("Recalculated job keyspace (no tasks)", map[string]interface{}{
		"job_id":                job.ID,
		"new_multiplication":    newMultFactor,
		"new_effective":        newEffective,
	})
}

// adjustRemainingKeyspace adjusts keyspace for jobs with existing tasks
func (s *JobUpdateService) adjustRemainingKeyspace(ctx context.Context, job *models.JobExecution, oldRules, newRules int64) {
	// Get the highest rule that's been dispatched
	maxRuleEnd, err := s.jobTaskRepo.GetMaxRuleEndIndex(ctx, job.ID)
	if err != nil {
		debug.Error("Failed to get max rule end for job %s: %v", job.ID, err)
		return
	}

	var maxRuleEndVal int64
	if maxRuleEnd != nil {
		maxRuleEndVal = int64(*maxRuleEnd)
	}

	if newRules <= maxRuleEndVal {
		// All new rules already covered - update to reflect reality
		err = s.jobExecRepo.UpdateMultiplicationFactor(ctx, job.ID, maxRuleEndVal)
		if err != nil {
			debug.Error("Failed to update multiplication factor for job %s: %v", job.ID, err)
		}
		debug.Log("Rules shrunk below dispatched range, job effectively complete", map[string]interface{}{
			"job_id":      job.ID,
			"max_rule":    maxRuleEndVal,
			"new_rules":   newRules,
		})
	} else {
		// Update total to reflect current reality
		err = s.jobExecRepo.UpdateMultiplicationFactor(ctx, job.ID, newRules)
		if err != nil {
			debug.Error("Failed to update multiplication factor for job %s: %v", job.ID, err)
			return
		}

		// Recalculate effective keyspace
		if job.BaseKeyspace != nil {
			newEffective := *job.BaseKeyspace * newRules
			err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, job.ID, newEffective)
			if err != nil {
				debug.Error("Failed to update effective keyspace for job %s: %v", job.ID, err)
			}
		}

		debug.Log("Adjusted remaining keyspace for job with tasks", map[string]interface{}{
			"job_id":             job.ID,
			"max_dispatched":     maxRuleEndVal,
			"new_total_rules":    newRules,
			"rules_to_process":   newRules - maxRuleEndVal,
		})
	}
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