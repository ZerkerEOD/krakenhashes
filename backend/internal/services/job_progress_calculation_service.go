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
	db                    *db.DB
	jobExecRepo           *repository.JobExecutionRepository
	jobTaskRepo           *repository.JobTaskRepository
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	mutex                 sync.Mutex
	running               bool
	stopChan              chan bool
}

// NewJobProgressCalculationService creates a new job progress calculation service
func NewJobProgressCalculationService(
	db *db.DB,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
) *JobProgressCalculationService {
	return &JobProgressCalculationService{
		db:                    db,
		jobExecRepo:           jobExecRepo,
		jobTaskRepo:           jobTaskRepo,
		jobIncrementLayerRepo: jobIncrementLayerRepo,
		stopChan:              make(chan bool),
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
			overall_progress_percent, uses_rule_splitting, status, completed_at,
			avg_rule_multiplier, base_keyspace, increment_mode
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
			&job.AvgRuleMultiplier,
			&job.BaseKeyspace,
			&job.IncrementMode,
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
	// Check if this is an increment mode job
	if job.IncrementMode != "" && job.IncrementMode != "off" {
		return s.calculateIncrementJobProgress(ctx, job)
	}

	// Regular job - aggregate from tasks directly
	return s.calculateRegularJobProgress(ctx, job)
}

// calculateIncrementJobProgress calculates progress for increment mode jobs (aggregate layers → job)
func (s *JobProgressCalculationService) calculateIncrementJobProgress(ctx context.Context, job models.JobExecution) (*JobProgressUpdate, error) {
	// Get all layers for this job
	layers, err := s.jobIncrementLayerRepo.GetByJobExecutionID(ctx, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get layers for job %s: %w", job.ID, err)
	}

	// First, update progress for each layer
	for i := range layers {
		layer := &layers[i]
		err := s.calculateAndUpdateLayerProgress(ctx, layer)
		if err != nil {
			debug.Warning("Failed to update layer %s progress: %v", layer.ID, err)
			// Continue with other layers
		}
	}

	// Then aggregate layer progress to job level
	var totalProcessedKeyspace int64
	var totalDispatchedKeyspace int64
	var totalEffectiveKeyspace int64

	for _, layer := range layers {
		totalProcessedKeyspace += layer.ProcessedKeyspace
		totalDispatchedKeyspace += layer.DispatchedKeyspace
		if layer.EffectiveKeyspace != nil {
			totalEffectiveKeyspace += *layer.EffectiveKeyspace
		} else if layer.BaseKeyspace != nil {
			totalEffectiveKeyspace += *layer.BaseKeyspace
		}
	}

	// Calculate overall progress percentage
	progressPercent := 0.0
	if totalEffectiveKeyspace > 0 {
		progressPercent = float64(totalProcessedKeyspace) / float64(totalEffectiveKeyspace) * 100
		if progressPercent > 100 {
			debug.Warning("Job %s progress exceeds 100%% (%.3f%%), capping at 100%%. Processed: %d, Effective: %d",
				job.ID, progressPercent, totalProcessedKeyspace, totalEffectiveKeyspace)
			progressPercent = 100
		}
	}

	// Aggregate dispatched keyspace from all layers
	return &JobProgressUpdate{
		ProcessedKeyspace:      totalProcessedKeyspace,
		DispatchedKeyspace:     totalDispatchedKeyspace,
		OverallProgressPercent: progressPercent,
	}, nil
}

// calculateRegularJobProgress calculates progress for regular jobs (aggregate tasks → job)
func (s *JobProgressCalculationService) calculateRegularJobProgress(ctx context.Context, job models.JobExecution) (*JobProgressUpdate, error) {
	// Get all tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks for job %s: %w", job.ID, err)
	}

	var processedKeyspace int64
	var dispatchedKeyspace int64

	// Determine if this is a keyspace-split job and get the multiplier
	// For keyspace-split jobs, we need to use consistent calculations for both
	// processed and dispatched to avoid drift
	multiplier := float64(1)
	isKeyspaceSplitJob := false
	if job.AvgRuleMultiplier != nil && *job.AvgRuleMultiplier > 0 {
		multiplier = *job.AvgRuleMultiplier
		// A job is a keyspace-split job if it has a multiplier AND is NOT a rule-splitting job
		// Rule-splitting jobs have multiplier but use different tracking
		isKeyspaceSplitJob = !job.UsesRuleSplitting && multiplier > 0
	}

	for _, task := range tasks {
		// Calculate processed keyspace
		// PRIORITY: Use effective_keyspace_processed when available (most accurate from hashcat)
		// FALLBACK: For keyspace-split jobs without effective values, estimate using multiplier
		if task.EffectiveKeyspaceProcessed != nil && *task.EffectiveKeyspaceProcessed > 0 {
			// Use effective keyspace directly - this is the actual value reported by hashcat
			processedKeyspace += *task.EffectiveKeyspaceProcessed
		} else if isKeyspaceSplitJob {
			// Fallback for keyspace-split tasks without effective values
			// For keyspace-split tasks, KeyspaceProcessed storage is INCONSISTENT:
			// - COMPLETED tasks: relative (chunk size)
			// - RUNNING tasks: absolute (restore_point from hashcat)
			// We detect absolute values by checking if KeyspaceProcessed >= KeyspaceStart
			var relativeProcessed int64
			// Check if keyspace_processed is absolute (for running tasks with keyspace splits)
			// Absolute values are >= keyspace_start when keyspace_start > 0
			if task.KeyspaceStart > 0 && task.KeyspaceProcessed >= task.KeyspaceStart {
				// keyspace_processed is absolute - convert to relative
				relativeProcessed = task.KeyspaceProcessed - task.KeyspaceStart
			} else {
				// keyspace_processed is already relative (or KeyspaceStart=0)
				relativeProcessed = task.KeyspaceProcessed
			}
			processedKeyspace += int64(float64(relativeProcessed) * multiplier)
		} else {
			// Fallback to regular keyspace processed
			processedKeyspace += task.KeyspaceProcessed
		}

		// Calculate dispatched keyspace for ALL tasks with defined ranges
		// This includes pending, failed, and cancelled tasks because "dispatched"
		// means work was allocated, showing total coverage (including gaps)
		// PRIORITY: Use effective keyspace range when available
		// FALLBACK: For keyspace-split jobs, estimate using multiplier
		if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
			// Use effective keyspace range if available (from hashcat or estimates)
			dispatchedKeyspace += (*task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart)
		} else if isKeyspaceSplitJob {
			// Fallback: Keyspace-split task without effective values
			// Calculate base chunk × multiplier for consistency
			chunkSize := task.KeyspaceEnd - task.KeyspaceStart
			if chunkSize > 0 {
				dispatchedKeyspace += int64(float64(chunkSize) * multiplier)
			}
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

// calculateAndUpdateLayerProgress aggregates task progress to layer and updates the database
func (s *JobProgressCalculationService) calculateAndUpdateLayerProgress(ctx context.Context, layer *models.JobIncrementLayer) error {
	// Get all tasks for this layer
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, layer.JobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get tasks for layer %s: %w", layer.ID, err)
	}

	// Filter tasks belonging to this layer
	var layerTasks []models.JobTask
	for _, task := range tasks {
		if task.IncrementLayerID != nil && *task.IncrementLayerID == layer.ID {
			layerTasks = append(layerTasks, task)
		}
	}

	// Calculate multiplier for keyspace-split tasks
	// Multiplier = EffectiveKeyspace / BaseKeyspace for the layer
	multiplier := float64(1)
	isKeyspaceSplitLayer := false
	if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 && layer.EffectiveKeyspace != nil && *layer.EffectiveKeyspace > 0 {
		multiplier = float64(*layer.EffectiveKeyspace) / float64(*layer.BaseKeyspace)
		// Consider it a keyspace-split layer if multiplier is significantly different from 1
		isKeyspaceSplitLayer = multiplier > 1.01 || multiplier < 0.99
	}

	// Aggregate processed and dispatched keyspace from layer tasks
	var processedKeyspace int64
	var dispatchedKeyspace int64
	for _, task := range layerTasks {
		// Calculate chunk size for this task (dispatched keyspace in BASE units)
		chunkSize := task.KeyspaceEnd - task.KeyspaceStart

		// For keyspace-split tasks, KeyspaceProcessed storage is INCONSISTENT:
		// - COMPLETED tasks: relative (chunk size)
		// - RUNNING tasks: absolute (restore_point from hashcat)
		// We detect absolute values by checking if KeyspaceProcessed >= KeyspaceStart
		if isKeyspaceSplitLayer {
			var relativeProcessed int64
			if task.KeyspaceStart > 0 && task.KeyspaceProcessed >= task.KeyspaceStart {
				// keyspace_processed is absolute - convert to relative
				relativeProcessed = task.KeyspaceProcessed - task.KeyspaceStart
			} else {
				// keyspace_processed is already relative (or KeyspaceStart=0)
				relativeProcessed = task.KeyspaceProcessed
			}
			processedKeyspace += int64(float64(relativeProcessed) * multiplier)

			// Dispatched keyspace: convert BASE chunk size to EFFECTIVE units
			if chunkSize > 0 {
				dispatchedKeyspace += int64(float64(chunkSize) * multiplier)
			}
		} else if task.EffectiveKeyspaceProcessed != nil && *task.EffectiveKeyspaceProcessed > 0 {
			processedKeyspace += *task.EffectiveKeyspaceProcessed

			// Use effective keyspace range if available
			if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
				dispatchedKeyspace += (*task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart)
			} else if chunkSize > 0 {
				dispatchedKeyspace += chunkSize
			}
		} else {
			processedKeyspace += task.KeyspaceProcessed
			dispatchedKeyspace += chunkSize
		}
	}

	// Calculate layer progress percentage
	progressPercent := 0.0
	if layer.EffectiveKeyspace != nil && *layer.EffectiveKeyspace > 0 {
		progressPercent = float64(processedKeyspace) / float64(*layer.EffectiveKeyspace) * 100
		if progressPercent > 100 {
			progressPercent = 100
		}
	} else if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 {
		progressPercent = float64(processedKeyspace) / float64(*layer.BaseKeyspace) * 100
		if progressPercent > 100 {
			progressPercent = 100
		}
	}

	// Update layer progress in database
	err = s.jobIncrementLayerRepo.UpdateProgress(ctx, layer.ID, processedKeyspace, progressPercent)
	if err != nil {
		return fmt.Errorf("failed to update layer progress: %w", err)
	}

	// Update in-memory layer object for aggregation by calculateIncrementJobProgress
	// Note: DispatchedKeyspace is calculated in EFFECTIVE units here for display purposes
	// The database stores dispatched_keyspace in BASE units (from task assignments)
	// but we override the in-memory value with EFFECTIVE units for correct aggregation
	layer.ProcessedKeyspace = processedKeyspace
	layer.DispatchedKeyspace = dispatchedKeyspace
	layer.OverallProgressPercent = progressPercent

	return nil
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

	// FEEDBACK LOOP: Check if any updated jobs should complete.
	// This addresses the issue where task completion triggers ProcessJobCompletion() with
	// stale dispatched_keyspace values. Now that we've updated progress, re-check completion.
	s.checkJobsForCompletion(ctx, updates)

	return nil
}

// checkJobsForCompletion checks if any updated jobs should be marked complete.
// This provides the feedback loop that was missing - after progress is updated,
// we re-verify if jobs with all tasks complete should now be completed.
func (s *JobProgressCalculationService) checkJobsForCompletion(ctx context.Context, updates map[uuid.UUID]*JobProgressUpdate) {
	for jobID := range updates {
		// Get fresh job data
		job, err := s.jobExecRepo.GetByID(ctx, jobID)
		if err != nil {
			continue
		}

		// Skip if already completed
		if job.Status == "completed" {
			continue
		}

		// Check if all tasks are complete (including no remaining work)
		allComplete, err := s.jobTaskRepo.AreAllTasksComplete(ctx, jobID)
		if err != nil {
			debug.Warning("Progress service: failed to check if all tasks complete for job %s: %v", jobID, err)
			continue
		}

		if !allComplete {
			continue
		}

		// Use structural check to verify job should complete
		if s.shouldJobComplete(ctx, job) {
			debug.Log("Progress service triggering job completion (feedback loop)", map[string]interface{}{
				"job_id":              jobID,
				"dispatched_keyspace": job.DispatchedKeyspace,
				"effective_keyspace":  job.EffectiveKeyspace,
			})

			// Complete the job
			err = s.jobExecRepo.CompleteExecution(ctx, jobID)
			if err != nil {
				debug.Error("Progress service failed to complete job %s: %v", jobID, err)
			} else {
				debug.Info("Progress service completed stuck job %s via feedback loop", jobID)
			}
		}
	}
}

// shouldJobComplete performs structural checks to determine if a job should be marked complete.
// This is called after AreAllTasksComplete() returns true, so dispatch validation is already done.
func (s *JobProgressCalculationService) shouldJobComplete(ctx context.Context, job *models.JobExecution) bool {
	// For increment mode, check layer status
	if job.IncrementMode != "" && job.IncrementMode != "off" {
		layers, err := s.jobIncrementLayerRepo.GetByJobExecutionID(ctx, job.ID)
		if err != nil {
			return false
		}
		for _, layer := range layers {
			if layer.Status != models.JobIncrementLayerStatusCompleted {
				return false
			}
		}
		return true
	}

	// For rule-split jobs, check max rule index
	if job.UsesRuleSplitting {
		maxRuleEnd, err := s.jobTaskRepo.GetMaxRuleEndIndex(ctx, job.ID)
		if err != nil {
			return false
		}
		return maxRuleEnd != nil && *maxRuleEnd >= job.MultiplicationFactor
	}

	// For non-rule-split jobs, AreAllTasksComplete() already validates dispatch
	// using effective_keyspace comparison. No additional structural check needed.
	// Note: We intentionally do NOT compare base_keyspace here because base_keyspace
	// (wordlist size) and effective_keyspace (total candidates) are different dimensions.
	return true
}
