package services

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobProgressUpdate represents calculated progress values for a job.
// ProcessedKeyspace/DispatchedKeyspace are EFFECTIVE-keyspace values (base × rules × salts
// can exceed int64), so they are tracked as models.BigInt.
type JobProgressUpdate struct {
	ProcessedKeyspace      models.BigInt
	DispatchedKeyspace     models.BigInt
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
			overall_progress_percent, status, completed_at,
			base_keyspace, increment_mode
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
			&job.Status,
			&completedAt,
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
	var totalProcessedKeyspace models.BigInt
	var totalDispatchedKeyspace models.BigInt
	var totalEffectiveKeyspace models.BigInt

	for _, layer := range layers {
		totalProcessedKeyspace = totalProcessedKeyspace.Add(layer.ProcessedKeyspace)
		totalDispatchedKeyspace = totalDispatchedKeyspace.Add(layer.DispatchedKeyspace)
		if layer.EffectiveKeyspace != nil {
			totalEffectiveKeyspace = totalEffectiveKeyspace.Add(*layer.EffectiveKeyspace)
		} else if layer.BaseKeyspace != nil {
			totalEffectiveKeyspace = totalEffectiveKeyspace.AddInt64(*layer.BaseKeyspace)
		}
	}

	// Calculate overall progress percentage
	progressPercent := 0.0
	if totalEffectiveKeyspace.IsPositive() {
		progressPercent = float64(totalProcessedKeyspace.Int64()) / float64(totalEffectiveKeyspace.Int64()) * 100
		if progressPercent > 100 {
			debug.Warning("Job %s progress exceeds 100%% (%.3f%%), capping at 100%%. Processed: %s, Effective: %s",
				job.ID, progressPercent, totalProcessedKeyspace.String(), totalEffectiveKeyspace.String())
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

	// OVERLAP DETECTION: Sort tasks by KeyspaceStart and detect overlapping ranges
	// This is a safety net to catch keyspace assignment bugs
	if len(tasks) > 1 {
		// Make a copy for sorting to avoid modifying original slice
		sortedTasks := make([]models.JobTask, len(tasks))
		copy(sortedTasks, tasks)
		sort.Slice(sortedTasks, func(i, j int) bool {
			return sortedTasks[i].KeyspaceStart < sortedTasks[j].KeyspaceStart
		})

		var lastEnd int64 = 0
		for _, task := range sortedTasks {
			if task.KeyspaceStart < lastEnd && task.KeyspaceEnd > 0 {
				debug.Error("OVERLAP DETECTED in job %s: task %s starts at %d but previous task ended at %d (overlap: %d)",
					job.ID, task.ID, task.KeyspaceStart, lastEnd, lastEnd-task.KeyspaceStart)
			}
			if task.KeyspaceEnd > lastEnd {
				lastEnd = task.KeyspaceEnd
			}
		}
	}

	var processedKeyspace models.BigInt
	var dispatchedKeyspace models.BigInt
	// processedBaseKeyspace tracks progress in BASE (wordlist) units. For
	// non-rule-split jobs the displayed percentage is driven off this, not off
	// effective keyspace: effective_keyspace = base × rules but does NOT track
	// the salt count, while hashcat's effective_keyspace_processed DOES — so on
	// salted modes (e.g. NetNTLMv2/5600) the effective ratio runs well past 100%
	// (584% was observed). Base coverage is immune to salt/rule drift and is
	// structurally bounded by base_keyspace.
	var processedBaseKeyspace int64

	// Determine if this is a keyspace-split job and get the effective/base ratio.
	// The ratio is applied as a big.Int multiply-then-divide (effStart × N / base)
	// — never a pre-divided float — so it's overflow- and precision-safe. It is
	// used ONLY to estimate a task's effective span when hashcat hasn't reported
	// one yet (legacy tasks with NULL effective coords). A job "splits" the
	// keyspace when effective > base (rules/salts multiply the base wordlist).
	isKeyspaceSplitJob := false
	var effKS, baseKS models.BigInt
	if job.EffectiveKeyspace != nil && job.EffectiveKeyspace.IsPositive() &&
		job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
		effKS = *job.EffectiveKeyspace
		baseKS = models.NewBigInt(*job.BaseKeyspace)
		isKeyspaceSplitJob = effKS.Cmp(baseKS) > 0
	}

	for _, task := range tasks {
		// Accumulate BASE-unit progress (used for the drift-free percentage of
		// non-rule-split jobs). Completed tasks count their full chunk; active
		// tasks count their partial restore_point (converted from absolute to
		// relative when needed), clamped to the chunk size; failed/cancelled
		// ranges reopened as gaps count for nothing.
		if task.Status != models.JobTaskStatusFailed && task.Status != models.JobTaskStatusCancelled {
			chunkSize := task.KeyspaceEnd - task.KeyspaceStart
			if task.Status == models.JobTaskStatusCompleted {
				if chunkSize > 0 {
					processedBaseKeyspace += chunkSize
				}
			} else {
				var baseProc int64
				if task.KeyspaceStart > 0 && task.KeyspaceProcessed >= task.KeyspaceStart {
					baseProc = task.KeyspaceProcessed - task.KeyspaceStart
				} else {
					baseProc = task.KeyspaceProcessed
				}
				if baseProc < 0 {
					baseProc = 0
				}
				if chunkSize > 0 && baseProc > chunkSize {
					baseProc = chunkSize
				}
				processedBaseKeyspace += baseProc
			}
		}

		// Calculate processed keyspace
		// PRIORITY: Use effective_keyspace_processed when available (most accurate from hashcat)
		// FALLBACK: For keyspace-split jobs without effective values, estimate using multiplier
		if task.EffectiveKeyspaceProcessed != nil && task.EffectiveKeyspaceProcessed.IsPositive() {
			// Use effective keyspace directly - this is the actual value reported by hashcat
			processedKeyspace = processedKeyspace.Add(*task.EffectiveKeyspaceProcessed)
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
			processedKeyspace = processedKeyspace.Add(models.NewBigInt(relativeProcessed).Mul(effKS).Div(baseKS))
		} else {
			// Fallback to regular keyspace processed
			processedKeyspace = processedKeyspace.AddInt64(task.KeyspaceProcessed)
		}

		// Calculate dispatched keyspace ONLY for tasks whose ranges are
		// still part of the active coverage set. In scheduler-v2 a failed
		// task has its interval marked 'failed' which reopens the range as
		// a gap — counting that range as dispatched would over-state
		// coverage and let `dispatched_percent` exceed reality. Same for
		// 'cancelled' (operator-initiated stop). Pending tasks DO count:
		// the dispatcher created an 'assigned' interval covering them.
		if task.Status == models.JobTaskStatusFailed || task.Status == models.JobTaskStatusCancelled {
			continue
		}
		// PRIORITY: Use effective keyspace range when available
		// FALLBACK: For keyspace-split jobs, estimate using multiplier
		if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
			// Use effective keyspace range if available (from hashcat or estimates)
			dispatchedKeyspace = dispatchedKeyspace.Add(task.EffectiveKeyspaceEnd.Sub(*task.EffectiveKeyspaceStart))
		} else if isKeyspaceSplitJob {
			// Fallback: Keyspace-split task without effective values
			// Calculate base chunk × (effective/base) for consistency
			chunkSize := task.KeyspaceEnd - task.KeyspaceStart
			if chunkSize > 0 {
				dispatchedKeyspace = dispatchedKeyspace.Add(models.NewBigInt(chunkSize).Mul(effKS).Div(baseKS))
			}
		} else if task.KeyspaceEnd > task.KeyspaceStart {
			// Fallback to regular keyspace range for tasks without effective keyspace
			dispatchedKeyspace = dispatchedKeyspace.AddInt64(task.KeyspaceEnd - task.KeyspaceStart)
		}
	}

	// Calculate overall progress percentage.
	progressPercent := 0.0
	if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
		// Base-driven: immune to salt/rule drift and structurally <= 100%.
		progressPercent = float64(processedBaseKeyspace) / float64(*job.BaseKeyspace) * 100
		if progressPercent > 100 {
			// Can still nudge over 100 from a transient in-flight restore_point
			// overshoot; clamp quietly (no over-count warning — base coverage
			// can't genuinely exceed the wordlist).
			progressPercent = 100
		}
	} else if job.EffectiveKeyspace != nil && job.EffectiveKeyspace.IsPositive() {
		// No-base-keyspace fallback (effective units).
		progressPercent = float64(processedKeyspace.Int64()) / float64(job.EffectiveKeyspace.Int64()) * 100
		if progressPercent > 100 {
			debug.Warning("Job %s progress exceeds 100%% (%.3f%%), capping at 100%%. Processed: %s, Effective: %s",
				job.ID, progressPercent, processedKeyspace.String(), job.EffectiveKeyspace.String())
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

	// OVERLAP DETECTION: Sort layer tasks by KeyspaceStart and detect overlapping ranges
	if len(layerTasks) > 1 {
		sort.Slice(layerTasks, func(i, j int) bool {
			return layerTasks[i].KeyspaceStart < layerTasks[j].KeyspaceStart
		})

		var lastEnd int64 = 0
		for _, task := range layerTasks {
			if task.KeyspaceStart < lastEnd && task.KeyspaceEnd > 0 {
				debug.Error("OVERLAP DETECTED in layer %s (job %s): task %s starts at %d but previous task ended at %d (overlap: %d)",
					layer.ID, layer.JobExecutionID, task.ID, task.KeyspaceStart, lastEnd, lastEnd-task.KeyspaceStart)
			}
			if task.KeyspaceEnd > lastEnd {
				lastEnd = task.KeyspaceEnd
			}
		}
	}

	// effective/base ratio for the layer, applied as a big.Int multiply-then-
	// divide (chunk × effective / base) so a large effective keyspace neither
	// overflows int64 nor loses float precision. A layer "splits" the keyspace
	// when effective differs from base (rules/salts multiply the base).
	isKeyspaceSplitLayer := false
	var effKS, baseKS models.BigInt
	if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 && layer.EffectiveKeyspace != nil && layer.EffectiveKeyspace.IsPositive() {
		effKS = *layer.EffectiveKeyspace
		baseKS = models.NewBigInt(*layer.BaseKeyspace)
		isKeyspaceSplitLayer = effKS.Cmp(baseKS) != 0
	}

	// Aggregate processed and dispatched keyspace from layer tasks.
	//
	// Unit contract:
	//   task.EffectiveKeyspaceProcessed → effective units (candidate count) — always preferred when set
	//   task.KeyspaceProcessed          → mode-dependent: base mask units for -a 3, wordlist offset for -a 0/6/7
	//   task.EffectiveKeyspace{Start,End} → effective-unit chunk boundaries
	//   chunkSize (= KeyspaceEnd - KeyspaceStart) → base units
	//
	// Priority order matches the regular (non-layer) path in calculateRegularJobProgress:
	// 1) Use EffectiveKeyspaceProcessed/EffectiveKeyspaceEnd when available (most accurate)
	// 2) Fall back to KeyspaceProcessed × multiplier ONLY for keyspace-split layers (legacy data)
	// 3) Fall back to raw KeyspaceProcessed for non-split layers
	var processedKeyspace models.BigInt
	var dispatchedKeyspace models.BigInt
	for _, task := range layerTasks {
		chunkSize := task.KeyspaceEnd - task.KeyspaceStart

		if task.EffectiveKeyspaceProcessed != nil && task.EffectiveKeyspaceProcessed.IsPositive() {
			// Preferred path — agent reported effective progress (unambiguous candidate count).
			processedKeyspace = processedKeyspace.Add(*task.EffectiveKeyspaceProcessed)

			if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
				dispatchedKeyspace = dispatchedKeyspace.Add(task.EffectiveKeyspaceEnd.Sub(*task.EffectiveKeyspaceStart))
			} else if isKeyspaceSplitLayer && chunkSize > 0 {
				dispatchedKeyspace = dispatchedKeyspace.Add(models.NewBigInt(chunkSize).Mul(effKS).Div(baseKS))
			} else if chunkSize > 0 {
				dispatchedKeyspace = dispatchedKeyspace.AddInt64(chunkSize)
			}
		} else if isKeyspaceSplitLayer {
			// Legacy / pre-effective-tracking fallback. KeyspaceProcessed storage is INCONSISTENT:
			// COMPLETED tasks store the relative chunk size, RUNNING tasks store the absolute
			// restore_point from hashcat. Detect absolute by comparing to KeyspaceStart.
			var relativeProcessed int64
			if task.KeyspaceStart > 0 && task.KeyspaceProcessed >= task.KeyspaceStart {
				relativeProcessed = task.KeyspaceProcessed - task.KeyspaceStart
			} else {
				relativeProcessed = task.KeyspaceProcessed
			}
			processedKeyspace = processedKeyspace.Add(models.NewBigInt(relativeProcessed).Mul(effKS).Div(baseKS))
			if chunkSize > 0 {
				dispatchedKeyspace = dispatchedKeyspace.Add(models.NewBigInt(chunkSize).Mul(effKS).Div(baseKS))
			}
		} else {
			processedKeyspace = processedKeyspace.AddInt64(task.KeyspaceProcessed)
			dispatchedKeyspace = dispatchedKeyspace.AddInt64(chunkSize)
		}
	}

	// Calculate layer progress percentage
	progressPercent := 0.0
	if layer.EffectiveKeyspace != nil && layer.EffectiveKeyspace.IsPositive() {
		progressPercent = float64(processedKeyspace.Int64()) / float64(layer.EffectiveKeyspace.Int64()) * 100
		if progressPercent > 100 {
			progressPercent = 100
		}
	} else if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 {
		progressPercent = float64(processedKeyspace.Int64()) / float64(*layer.BaseKeyspace) * 100
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
	processedChanged := job.ProcessedKeyspace.Cmp(update.ProcessedKeyspace) != 0
	dispatchedChanged := job.DispatchedKeyspace.Cmp(update.DispatchedKeyspace) != 0

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
			// CRITICAL: Check for failed tasks before completing
			hasFailed, failErr := s.jobTaskRepo.HasFailedTasks(ctx, jobID)
			if failErr != nil {
				debug.Warning("Progress service: failed to check for failed tasks for job %s: %v", jobID, failErr)
				continue
			}
			if hasFailed {
				debug.Log("Progress service: Job has failed tasks - marking as failed", map[string]interface{}{
					"job_id": jobID,
				})
				if failExecErr := s.jobExecRepo.FailExecution(ctx, jobID, "One or more tasks failed"); failExecErr != nil {
					debug.Error("Progress service failed to mark job %s as failed: %v", jobID, failExecErr)
				} else {
					debug.Info("Progress service marked job %s as failed due to failed tasks", jobID)
				}
				continue
			}

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

				// Sync effective_keyspace to processed_keyspace to ensure 100% display
				// This prevents >100% progress when effective_keyspace from benchmark was lower
				// (similar to the AllHashesCracked handler in job_websocket_integration.go)
				if job.ProcessedKeyspace.IsPositive() {
					if job.EffectiveKeyspace == nil || job.EffectiveKeyspace.Cmp(job.ProcessedKeyspace) < 0 {
						syncErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobID, job.ProcessedKeyspace)
						if syncErr != nil {
							debug.Warning("Failed to sync effective_keyspace on completion for job %s: %v", jobID, syncErr)
						} else {
							debug.Info("Synced effective_keyspace to %s on job %s completion", job.ProcessedKeyspace.String(), jobID)
						}
					}
				}
			}
		}
	}
}

// shouldJobComplete performs structural checks to determine if a job should be marked complete.
// This is called after AreAllTasksComplete() returns true, so dispatch validation is already done.
func (s *JobProgressCalculationService) shouldJobComplete(ctx context.Context, job *models.JobExecution) bool {
	// Scheduler-v2 base-keyspace gap check: if ANY scheduling_unit for this
	// job still has undispatched base-keyspace gaps, the job is NOT complete
	// — no matter what AreAllTasksComplete says about in-flight tasks.
	//
	// This prevents the premature-completion bug where an operator kills the
	// only agent mid-job: the in-flight task gets marked failed (recovery),
	// AreAllTasksComplete returns true (no active tasks left), and the old
	// code would auto-flip the job to 'completed' with most of the keyspace
	// untouched. With this guard the job stays in its current state waiting
	// for an agent to come back online.
	//
	// Gap arithmetic is in BASE keyspace (= invariant chunkable dimension,
	// matching hashcat's --skip/--limit). Per the user-stated model, base
	// is the source of truth for coverage; effective is derived for display.
	var gapsRemaining int
	gapErr := s.db.QueryRowContext(ctx, `
		WITH unit_gaps AS (
			SELECT su.id AS unit_id, su.base_keyspace,
				COALESCE(SUM(jki.range_end - jki.range_start) FILTER (WHERE jki.status <> 'failed'), 0) AS covered
			FROM scheduling_units su
			LEFT JOIN job_keyspace_intervals jki ON jki.scheduling_unit_id = su.id
			WHERE su.parent_job_id = $1
			  AND su.base_keyspace IS NOT NULL
			GROUP BY su.id, su.base_keyspace
		)
		SELECT COUNT(*) FROM unit_gaps WHERE covered < base_keyspace
	`, job.ID).Scan(&gapsRemaining)
	if gapErr == nil && gapsRemaining > 0 {
		debug.Log("Job has uncovered base-keyspace gaps; not completing", map[string]interface{}{
			"job_id":         job.ID,
			"units_with_gap": gapsRemaining,
		})
		return false
	}

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

	// Without gap remaining, completion is valid.
	return true
}
