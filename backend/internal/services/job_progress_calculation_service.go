package services

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobProgressUpdate represents calculated progress values for a job
type JobProgressUpdate struct {
	ProcessedKeyspace      int64
	DispatchedKeyspace     int64
	OverallProgressPercent float64
}

// JobProgressCalculationService periodically recalculates job progress from task data
// This eliminates the need for increment/decrement tracking and ensures accuracy
type JobProgressCalculationService struct {
	db          *db.DB
	jobExecRepo *repository.JobExecutionRepository
	jobTaskRepo *repository.JobTaskRepository
	mutex       sync.Mutex
	running     bool
	stopChan    chan bool
}

// NewJobProgressCalculationService creates a new job progress calculation service
func NewJobProgressCalculationService(
	db *db.DB,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
) *JobProgressCalculationService {
	return &JobProgressCalculationService{
		db:          db,
		jobExecRepo: jobExecRepo,
		jobTaskRepo: jobTaskRepo,
		stopChan:    make(chan bool),
	}
}

// Start begins the polling loop that recalculates job progress every 2 seconds
func (s *JobProgressCalculationService) Start() {
	s.running = true
	debug.Info("Starting job progress calculation service (polling every 2 seconds)")
	go s.runPollingLoop()
}

// Stop gracefully stops the polling loop
func (s *JobProgressCalculationService) Stop() {
	if s.running {
		debug.Info("Stopping job progress calculation service")
		s.running = false
		s.stopChan <- true
	}
}

// runPollingLoop executes the progress calculation every 2 seconds
func (s *JobProgressCalculationService) runPollingLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.updateJobProgress()
		case <-s.stopChan:
			debug.Info("Job progress calculation service stopped")
			return
		}
	}
}

// updateJobProgress calculates and updates progress for all active jobs
func (s *JobProgressCalculationService) updateJobProgress() {
	// Prevent concurrent execution using TryLock
	if !s.mutex.TryLock() {
		debug.Debug("Progress calculation already running, skipping this cycle")
		return
	}
	defer s.mutex.Unlock()

	// Create context with timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	startTime := time.Now()

	// Query jobs that need updates
	jobs, err := s.getJobsNeedingUpdate(ctx)
	if err != nil {
		debug.Error("Failed to get jobs for progress update: %v", err)
		return
	}

	if len(jobs) == 0 {
		debug.Debug("No jobs need progress updates")
		return
	}

	debug.Debug("Calculating progress for %d jobs", len(jobs))

	// Calculate progress for each job
	updates := make(map[uuid.UUID]*JobProgressUpdate)
	for _, job := range jobs {
		update, err := s.calculateJobProgress(ctx, job)
		if err != nil {
			debug.Error("Failed to calculate progress for job %s: %v", job.ID, err)
			continue
		}

		// Only include if values changed
		if s.hasChanged(job, update) {
			updates[job.ID] = update
		}
	}

	// Batch update all changed jobs
	if len(updates) > 0 {
		debug.Debug("Updating progress for %d jobs (out of %d checked)", len(updates), len(jobs))
		err = s.batchUpdateProgress(ctx, updates)
		if err != nil {
			debug.Error("Failed to batch update job progress: %v", err)
		}
	}

	elapsed := time.Since(startTime)
	debug.Debug("Progress calculation completed in %v", elapsed)
}

// getJobsNeedingUpdate retrieves all jobs that need progress recalculation
func (s *JobProgressCalculationService) getJobsNeedingUpdate(ctx context.Context) ([]models.JobExecution, error) {
	query := `
		SELECT
			id, effective_keyspace, processed_keyspace, dispatched_keyspace,
			overall_progress_percent, uses_rule_splitting, status, completed_at
		FROM job_executions
		WHERE
			-- Active jobs that may have changing progress
			(status IN ('pending', 'running', 'paused', 'failed'))
			OR
			-- Recently completed jobs (within last 15 seconds)
			(status = 'completed' AND completed_at > NOW() - INTERVAL '15 seconds')
		ORDER BY status, id
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs needing update: %w", err)
	}
	defer rows.Close()

	var jobs []models.JobExecution
	for rows.Next() {
		var job models.JobExecution
		var completedAt sql.NullTime

		err := rows.Scan(
			&job.ID,
			&job.EffectiveKeyspace,
			&job.ProcessedKeyspace,
			&job.DispatchedKeyspace,
			&job.OverallProgressPercent,
			&job.UsesRuleSplitting,
			&job.Status,
			&completedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}

		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// calculateJobProgress computes progress values from actual task data
func (s *JobProgressCalculationService) calculateJobProgress(ctx context.Context, job models.JobExecution) (*JobProgressUpdate, error) {
	// Get all tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks for job %s: %w", job.ID, err)
	}

	var processedKeyspace int64
	var dispatchedKeyspace int64

	for _, task := range tasks {
		// Calculate processed keyspace
		// Prefer effective keyspace (from hashcat progress[1]) over regular keyspace
		if task.EffectiveKeyspaceProcessed != nil && *task.EffectiveKeyspaceProcessed > 0 {
			processedKeyspace += *task.EffectiveKeyspaceProcessed
		} else {
			// Fallback to regular keyspace processed
			processedKeyspace += task.KeyspaceProcessed
		}

		// Calculate dispatched keyspace for ALL tasks with defined ranges
		// This includes pending, failed, and cancelled tasks because "dispatched"
		// means work was allocated, showing total coverage (including gaps)
		if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
			// Use effective keyspace range if available (from hashcat or estimates)
			dispatchedKeyspace += (*task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart)
		} else if task.KeyspaceEnd > task.KeyspaceStart {
			// Fallback to regular keyspace range for tasks without effective keyspace
			dispatchedKeyspace += (task.KeyspaceEnd - task.KeyspaceStart)
		}
	}

	// Calculate overall progress percentage
	progressPercent := 0.0
	if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
		progressPercent = float64(processedKeyspace) / float64(*job.EffectiveKeyspace) * 100

		// Cap at 100% but log if it exceeds (indicates a calculation issue)
		if progressPercent > 100 {
			debug.Warning("Job %s progress exceeds 100%% (%.3f%%), capping at 100%%. Processed: %d, Effective: %d",
				job.ID, progressPercent, processedKeyspace, *job.EffectiveKeyspace)
			progressPercent = 100
		}
	}

	return &JobProgressUpdate{
		ProcessedKeyspace:      processedKeyspace,
		DispatchedKeyspace:     dispatchedKeyspace,
		OverallProgressPercent: progressPercent,
	}, nil
}

// hasChanged checks if the calculated values differ from stored values
func (s *JobProgressCalculationService) hasChanged(job models.JobExecution, update *JobProgressUpdate) bool {
	// Check if any value has changed
	processedChanged := job.ProcessedKeyspace != update.ProcessedKeyspace
	dispatchedChanged := job.DispatchedKeyspace != update.DispatchedKeyspace

	// Use a small threshold for percentage to avoid floating point precision issues
	percentChanged := math.Abs(job.OverallProgressPercent-update.OverallProgressPercent) > 0.01

	return processedChanged || dispatchedChanged || percentChanged
}

// batchUpdateProgress atomically updates all changed jobs in a single transaction
func (s *JobProgressCalculationService) batchUpdateProgress(ctx context.Context, updates map[uuid.UUID]*JobProgressUpdate) error {
	// Use a transaction for atomic batch update
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statement for efficiency
	stmt, err := tx.PrepareContext(ctx, `
		UPDATE job_executions
		SET processed_keyspace = $1,
		    dispatched_keyspace = $2,
		    overall_progress_percent = $3,
		    last_progress_update = NOW(),
		    updated_at = NOW()
		WHERE id = $4
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	// Execute batch updates
	successCount := 0
	for jobID, update := range updates {
		_, err := stmt.ExecContext(ctx,
			update.ProcessedKeyspace,
			update.DispatchedKeyspace,
			update.OverallProgressPercent,
			jobID,
		)
		if err != nil {
			debug.Error("Failed to update job %s: %v", jobID, err)
			// Continue with other updates instead of failing the entire batch
		} else {
			successCount++
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	debug.Debug("Successfully updated %d/%d jobs", successCount, len(updates))
	return nil
}
