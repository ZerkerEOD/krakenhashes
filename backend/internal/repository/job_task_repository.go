package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobTaskRepository handles database operations for job tasks
type JobTaskRepository struct {
	db *db.DB
}

// NewJobTaskRepository creates a new job task repository
func NewJobTaskRepository(db *db.DB) *JobTaskRepository {
	return &JobTaskRepository{db: db}
}

// GetPreviousChunksActualKeyspace returns the cumulative actual keyspace size from all previous chunks
func (r *JobTaskRepository) GetPreviousChunksActualKeyspace(ctx context.Context, jobExecutionID uuid.UUID, currentChunkNumber int) (int64, error) {
	query := `
		SELECT COALESCE(SUM(chunk_actual_keyspace), 0)
		FROM job_tasks
		WHERE job_execution_id = $1
		  AND chunk_number < $2
		  AND chunk_actual_keyspace IS NOT NULL`

	var cumulativeKeyspace int64
	err := r.db.QueryRowContext(ctx, query, jobExecutionID, currentChunkNumber).Scan(&cumulativeKeyspace)
	if err != nil {
		return 0, fmt.Errorf("failed to get previous chunks' actual keyspace: %w", err)
	}

	return cumulativeKeyspace, nil
}

// Create creates a new job task
func (r *JobTaskRepository) Create(ctx context.Context, task *models.JobTask) error {
	query := `
		INSERT INTO job_tasks (
			job_execution_id, agent_id, status, priority, attack_cmd,
			keyspace_start, keyspace_end, keyspace_processed,
			effective_keyspace_start, effective_keyspace_end, effective_keyspace_processed,
			benchmark_speed, chunk_duration,
			rule_start_index, rule_end_index, rule_chunk_path, is_rule_split_task,
			chunk_number, is_actual_keyspace
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		RETURNING id, assigned_at, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		task.JobExecutionID,
		task.AgentID,
		task.Status,
		task.Priority,
		task.AttackCmd,
		task.KeyspaceStart,
		task.KeyspaceEnd,
		task.KeyspaceProcessed,
		task.EffectiveKeyspaceStart,
		task.EffectiveKeyspaceEnd,
		task.EffectiveKeyspaceProcessed,
		task.BenchmarkSpeed,
		task.ChunkDuration,
		task.RuleStartIndex,
		task.RuleEndIndex,
		task.RuleChunkPath,
		task.IsRuleSplitTask,
		task.ChunkNumber,
		task.IsActualKeyspace,
	).Scan(&task.ID, &task.AssignedAt, &task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create job task: %w", err)
	}

	debug.Log("Created job task", map[string]interface{}{
		"task_id":            task.ID,
		"job_execution_id":   task.JobExecutionID,
		"status":             task.Status,
		"is_rule_split_task": task.IsRuleSplitTask,
	})

	return nil
}

// CreateWithRuleSplitting creates a new job task with rule splitting support
// This method should be used once migration 34 is applied
func (r *JobTaskRepository) CreateWithRuleSplitting(ctx context.Context, task *models.JobTask) error {
	query := `
		INSERT INTO job_tasks (
			job_execution_id, agent_id, status, priority, attack_cmd,
			keyspace_start, keyspace_end, keyspace_processed,
			effective_keyspace_start, effective_keyspace_end, effective_keyspace_processed,
			benchmark_speed, chunk_duration,
			rule_start_index, rule_end_index, rule_chunk_path, is_rule_split_task,
			chunk_number, is_actual_keyspace
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		RETURNING id, assigned_at, created_at, updated_at`

	err := r.db.QueryRowContext(ctx, query,
		task.JobExecutionID,
		task.AgentID,
		task.Status,
		task.Priority,
		task.AttackCmd,
		task.KeyspaceStart,
		task.KeyspaceEnd,
		task.KeyspaceProcessed,
		task.EffectiveKeyspaceStart,
		task.EffectiveKeyspaceEnd,
		task.EffectiveKeyspaceProcessed,
		task.BenchmarkSpeed,
		task.ChunkDuration,
		task.RuleStartIndex,
		task.RuleEndIndex,
		task.RuleChunkPath,
		task.IsRuleSplitTask,
		task.ChunkNumber,
		task.IsActualKeyspace,
	).Scan(&task.ID, &task.AssignedAt, &task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create job task with rule splitting: %w", err)
	}

	return nil
}

// GetByID retrieves a job task by ID
func (r *JobTaskRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.JobTask, error) {
	query := `
		SELECT
			jt.id, jt.job_execution_id, jt.agent_id, jt.status,
			jt.priority, jt.attack_cmd,
			jt.keyspace_start, jt.keyspace_end, jt.keyspace_processed,
			jt.effective_keyspace_start, jt.effective_keyspace_end, jt.effective_keyspace_processed,
			jt.benchmark_speed, jt.average_speed, jt.chunk_duration, jt.assigned_at,
			jt.started_at, jt.completed_at, jt.last_checkpoint, jt.error_message,
			jt.crack_count, jt.detailed_status, jt.retry_count,
			jt.rule_start_index, jt.rule_end_index, jt.rule_chunk_path, jt.is_rule_split_task,
			jt.progress_percent,
			a.name as agent_name
		FROM job_tasks jt
		JOIN agents a ON jt.agent_id = a.id
		WHERE jt.id = $1`

	var task models.JobTask
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
		&task.Priority, &task.AttackCmd,
		&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.EffectiveKeyspaceStart, &task.EffectiveKeyspaceEnd, &task.EffectiveKeyspaceProcessed,
		&task.BenchmarkSpeed, &task.AverageSpeed, &task.ChunkDuration, &task.AssignedAt,
		&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
		&task.CrackCount, &task.DetailedStatus, &task.RetryCount,
		&task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath, &task.IsRuleSplitTask,
		&task.ProgressPercent,
		&task.AgentName,
	)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job task: %w", err)
	}

	return &task, nil
}

// GetTasksByJobExecution retrieves all tasks for a job execution
func (r *JobTaskRepository) GetTasksByJobExecution(ctx context.Context, jobExecutionID uuid.UUID) ([]models.JobTask, error) {
	query := `
		SELECT
			jt.id, jt.job_execution_id, jt.agent_id, jt.status,
			jt.keyspace_start, jt.keyspace_end, jt.keyspace_processed,
			jt.effective_keyspace_start, jt.effective_keyspace_end, jt.effective_keyspace_processed,
			jt.benchmark_speed, jt.average_speed, jt.chunk_duration, jt.assigned_at,
			jt.started_at, jt.completed_at, jt.last_checkpoint, jt.error_message,
			jt.crack_count,
			jt.rule_start_index, jt.rule_end_index, jt.rule_chunk_path, jt.is_rule_split_task,
			jt.progress_percent,
			a.name as agent_name
		FROM job_tasks jt
		LEFT JOIN agents a ON jt.agent_id = a.id
		WHERE jt.job_execution_id = $1
		ORDER BY jt.keyspace_start ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks by job execution: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
			&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.EffectiveKeyspaceStart, &task.EffectiveKeyspaceEnd, &task.EffectiveKeyspaceProcessed,
			&task.BenchmarkSpeed, &task.AverageSpeed, &task.ChunkDuration, &task.AssignedAt,
			&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
			&task.CrackCount,
			&task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath, &task.IsRuleSplitTask,
			&task.ProgressPercent,
			&task.AgentName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetActiveTasksByAgent retrieves active tasks for an agent
func (r *JobTaskRepository) GetActiveTasksByAgent(ctx context.Context, agentID int) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status,
			keyspace_start, keyspace_end, keyspace_processed,
			benchmark_speed, chunk_duration, assigned_at,
			started_at, completed_at, last_checkpoint, error_message
		FROM job_tasks
		WHERE agent_id = $1 AND status IN ('assigned', 'running')
		ORDER BY assigned_at ASC`

	rows, err := r.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active tasks by agent: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
			&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.BenchmarkSpeed, &task.ChunkDuration, &task.AssignedAt,
			&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetStaleTasks retrieves tasks that are in assigned or running state
func (r *JobTaskRepository) GetStaleTasks(ctx context.Context) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status,
			keyspace_start, keyspace_end, keyspace_processed,
			benchmark_speed, chunk_duration, assigned_at,
			started_at, completed_at, last_checkpoint, error_message,
			created_at, updated_at
		FROM job_tasks
		WHERE status IN ('assigned', 'running')
		ORDER BY assigned_at ASC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get stale tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
			&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.BenchmarkSpeed, &task.ChunkDuration, &task.AssignedAt,
			&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
			&task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetTasksNotUpdatedSince retrieves tasks that haven't been updated since the given time
func (r *JobTaskRepository) GetTasksNotUpdatedSince(ctx context.Context, since time.Time) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status,
			keyspace_start, keyspace_end, keyspace_processed,
			benchmark_speed, chunk_duration, assigned_at,
			started_at, completed_at, last_checkpoint, error_message,
			crack_count, created_at, updated_at
		FROM job_tasks
		WHERE status IN ('assigned', 'running')
		  AND (last_checkpoint IS NULL OR last_checkpoint < $1)
		  AND updated_at < $1
		ORDER BY assigned_at ASC`

	rows, err := r.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks not updated since %v: %w", since, err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
			&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.BenchmarkSpeed, &task.ChunkDuration, &task.AssignedAt,
			&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
			&task.CrackCount, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// UpdateTaskError marks a task as failed with an error message
// NOTE: This function does NOT adjust dispatched_keyspace. Use MarkTaskFailedPermanently
// if the task should be permanently failed and dispatched keyspace needs to be decremented.
func (r *JobTaskRepository) UpdateTaskError(ctx context.Context, taskID uuid.UUID, errorMessage string) error {
	query := `
		UPDATE job_tasks
		SET status = $2,
		    error_message = $3,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`

	_, err := r.db.ExecContext(ctx, query, taskID, models.JobTaskStatusFailed, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to update task error: %w", err)
	}

	return nil
}

// MarkTaskFailedPermanently marks a task as permanently failed and decrements the job's dispatched keyspace
// This should be used when a task fails and will NOT be retried (e.g., max retries exceeded)
func (r *JobTaskRepository) MarkTaskFailedPermanently(ctx context.Context, taskID uuid.UUID, errorMessage string) error {
	// Get task details including keyspace ranges
	var jobExecutionID uuid.UUID
	var keyspaceProcessed int64
	var keyspaceStart int64
	var keyspaceEnd int64
	var effectiveKeyspaceStart sql.NullInt64
	var effectiveKeyspaceEnd sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		"SELECT job_execution_id, keyspace_processed, keyspace_start, keyspace_end, "+
		"effective_keyspace_start, effective_keyspace_end FROM job_tasks WHERE id = $1",
		taskID).Scan(&jobExecutionID, &keyspaceProcessed, &keyspaceStart, &keyspaceEnd,
		&effectiveKeyspaceStart, &effectiveKeyspaceEnd)
	if err != nil {
		return fmt.Errorf("failed to get task details: %w", err)
	}

	// Begin transaction to ensure atomicity
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Mark task as failed
	query := `
		UPDATE job_tasks
		SET status = $2,
		    error_message = $3,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`
	_, err = tx.ExecContext(ctx, query, taskID, models.JobTaskStatusFailed, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}

	// Subtract any processed keyspace from the job execution
	if keyspaceProcessed > 0 {
		updateJobQuery := `
			UPDATE job_executions
			SET processed_keyspace = processed_keyspace - $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2 AND processed_keyspace >= $1`
		_, err = tx.ExecContext(ctx, updateJobQuery, keyspaceProcessed, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to update job processed keyspace: %w", err)
		}
	}

	// Decrement the dispatched keyspace since this work will never complete
	// Use the EFFECTIVE chunk size that was originally dispatched
	var chunkSize int64
	if effectiveKeyspaceStart.Valid && effectiveKeyspaceEnd.Valid {
		chunkSize = effectiveKeyspaceEnd.Int64 - effectiveKeyspaceStart.Int64
	} else {
		// Fallback to base keyspace if effective not set
		chunkSize = keyspaceEnd - keyspaceStart
	}

	if chunkSize > 0 {
		decrementDispatchedQuery := `
			UPDATE job_executions
			SET dispatched_keyspace = GREATEST(dispatched_keyspace - $1, 0),
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2`
		_, err = tx.ExecContext(ctx, decrementDispatchedQuery, chunkSize, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to decrement job dispatched keyspace: %w", err)
		}

		debug.Log("Decremented dispatched keyspace for permanently failed task", map[string]interface{}{
			"task_id":                 taskID,
			"job_execution_id":        jobExecutionID,
			"chunk_size_decremented":  chunkSize,
			"used_effective_keyspace": effectiveKeyspaceStart.Valid && effectiveKeyspaceEnd.Valid,
			"error_message":           errorMessage,
		})
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateStatus updates the status of a job task
func (r *JobTaskRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.JobTaskStatus) error {
	// Map status to appropriate detailed_status to maintain database constraint consistency
	var detailedStatus string
	switch status {
	case models.JobTaskStatusPending:
		detailedStatus = "pending"
	case models.JobTaskStatusAssigned:
		detailedStatus = "dispatched"
	case models.JobTaskStatusRunning:
		detailedStatus = "running"
	case models.JobTaskStatusCompleted:
		detailedStatus = "completed_no_cracks"
	case models.JobTaskStatusFailed:
		detailedStatus = "failed"
	case models.JobTaskStatusCancelled:
		detailedStatus = "cancelled"
	case models.JobTaskStatusReconnectPending:
		// Keep the existing detailed_status for reconnect_pending
		// This is a temporary state and detailed_status should remain as it was
		query := `UPDATE job_tasks SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
		result, err := r.db.ExecContext(ctx, query, status, id)
		if err != nil {
			return fmt.Errorf("failed to update job task status: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}
		if rowsAffected == 0 {
			debug.Error("Task not found when updating status: task_id=%s, status=%s", id, status)
			return ErrNotFound
		}
		debug.Log("Updated task status to reconnect_pending", map[string]interface{}{
			"task_id":       id,
			"status":        status,
			"rows_affected": rowsAffected,
		})
		return nil
	default:
		detailedStatus = "pending" // Safe default
	}

	query := `UPDATE job_tasks SET status = $1, detailed_status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	result, err := r.db.ExecContext(ctx, query, status, detailedStatus, id)
	if err != nil {
		return fmt.Errorf("failed to update job task status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		debug.Error("Task not found when updating status: task_id=%s, status=%s", id, status)
		return ErrNotFound
	}

	debug.Log("Updated task status", map[string]interface{}{
		"task_id":         id,
		"status":          status,
		"detailed_status": detailedStatus,
		"rows_affected":   rowsAffected,
	})

	return nil
}

// StartTask marks a task as started
func (r *JobTaskRepository) StartTask(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `UPDATE job_tasks SET status = $1, started_at = $2 WHERE id = $3 AND status = 'assigned'`
	result, err := r.db.ExecContext(ctx, query, models.JobTaskStatusRunning, now, id)
	if err != nil {
		return fmt.Errorf("failed to start job task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateProgress updates the progress of a task
func (r *JobTaskRepository) UpdateProgress(ctx context.Context, id uuid.UUID, keyspaceProcessed int64, effectiveKeyspaceProcessed int64, benchmarkSpeed *int64, progressPercent float64) error {
	now := time.Now()
	query := `
		UPDATE job_tasks 
		SET keyspace_processed = $1, effective_keyspace_processed = $2, benchmark_speed = $3, last_checkpoint = $4, progress_percent = $5
		WHERE id = $6`

	result, err := r.db.ExecContext(ctx, query, keyspaceProcessed, effectiveKeyspaceProcessed, benchmarkSpeed, now, progressPercent, id)
	if err != nil {
		return fmt.Errorf("failed to update job task progress: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// CompleteTask marks a task as completed
func (r *JobTaskRepository) CompleteTask(ctx context.Context, id uuid.UUID) error {
	now := time.Now()

	// Calculate and store average speed before marking as complete
	err := r.CalculateAndStoreAverageSpeed(ctx, id)
	if err != nil {
		// Log error but don't fail the completion
		// Average speed is nice to have but not critical
		fmt.Printf("Warning: failed to calculate average speed for task %s: %v\n", id, err)
	}

	// Update agent benchmark with the average speed from this task
	task, err := r.GetByID(ctx, id)
	if err == nil && task.AverageSpeed != nil && *task.AverageSpeed > 0 && task.AgentID != nil {
		// Get job execution to find attack mode and hash type
		jobExecQuery := `SELECT attack_mode, hash_type FROM job_executions WHERE id = $1`
		var attackMode int
		var hashType int

		err = r.db.QueryRowContext(ctx, jobExecQuery, task.JobExecutionID).Scan(&attackMode, &hashType)
		if err == nil {
			// Update the existing benchmark (it must exist or task wouldn't have run)
			updateBenchmarkQuery := `
				UPDATE agent_benchmarks
				SET speed = $1, updated_at = NOW()
				WHERE agent_id = $2 AND attack_mode = $3 AND hash_type = $4`

			_, err = r.db.ExecContext(ctx, updateBenchmarkQuery,
				*task.AverageSpeed, *task.AgentID, attackMode, hashType)
			if err != nil {
				// Log but don't fail task completion
				fmt.Printf("Warning: failed to update benchmark for agent %d: %v\n", *task.AgentID, err)
			} else {
				fmt.Printf("Updated benchmark for agent %d, mode %d, type %d: %d H/s\n",
					*task.AgentID, attackMode, hashType, *task.AverageSpeed)
			}
		}
	}

	// Update both status and detailed_status to maintain database constraint consistency
	query := `UPDATE job_tasks SET status = $1, detailed_status = $2, completed_at = $3, progress_percent = 100 WHERE id = $4`
	result, err := r.db.ExecContext(ctx, query, models.JobTaskStatusCompleted, "completed_no_cracks", now, id)
	if err != nil {
		return fmt.Errorf("failed to complete job task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// FailTask marks a task as failed with an error message
func (r *JobTaskRepository) FailTask(ctx context.Context, id uuid.UUID, errorMessage string) error {
	now := time.Now()
	// Update both status and detailed_status to maintain database constraint consistency
	query := `UPDATE job_tasks SET status = $1, detailed_status = $2, completed_at = $3, error_message = $4 WHERE id = $5`
	result, err := r.db.ExecContext(ctx, query, models.JobTaskStatusFailed, "failed", now, errorMessage, id)
	if err != nil {
		return fmt.Errorf("failed to fail job task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// CancelTask marks a task as cancelled
func (r *JobTaskRepository) CancelTask(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	// Update both status and detailed_status to maintain database constraint consistency
	query := `UPDATE job_tasks SET status = $1, detailed_status = $2, completed_at = $3 WHERE id = $4 AND status IN ('pending', 'assigned', 'running')`
	result, err := r.db.ExecContext(ctx, query, models.JobTaskStatusCancelled, "cancelled", now, id)
	if err != nil {
		return fmt.Errorf("failed to cancel job task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// SetTaskPending sets a task back to pending status for reassignment
// This is used when interrupting a task for a higher priority job
func (r *JobTaskRepository) SetTaskPending(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE job_tasks 
		SET 
			status = 'pending',
			detailed_status = 'pending',
			agent_id = NULL,
			assigned_at = NULL,
			started_at = NULL,
			last_checkpoint = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status IN ('assigned', 'running')`
	
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to set task to pending: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// GetNextKeyspaceRange gets the next available keyspace range for a job
func (r *JobTaskRepository) GetNextKeyspaceRange(ctx context.Context, jobExecutionID uuid.UUID) (start int64, end int64, err error) {
	query := `
		SELECT COALESCE(MAX(keyspace_end), 0) as last_end
		FROM job_tasks
		WHERE job_execution_id = $1`

	var lastEnd int64
	err = r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&lastEnd)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get next keyspace range: %w", err)
	}

	return lastEnd, 0, nil // End will be calculated by the service based on chunk size
}

// GetIncompleteTasksCount returns the number of incomplete tasks for a job
func (r *JobTaskRepository) GetIncompleteTasksCount(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM job_tasks
		WHERE job_execution_id = $1 AND status NOT IN ('completed', 'cancelled')`

	var count int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get incomplete tasks count: %w", err)
	}

	return count, nil
}

// GetMaxKeyspaceEnd returns the maximum keyspace_end value for a job
func (r *JobTaskRepository) GetMaxKeyspaceEnd(ctx context.Context, jobExecutionID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(MAX(keyspace_end), 0)
		FROM job_tasks
		WHERE job_execution_id = $1`

	var maxEnd int64
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&maxEnd)
	if err != nil {
		return 0, fmt.Errorf("failed to get max keyspace end: %w", err)
	}

	return maxEnd, nil
}

// GetFailedTasksByJobExecution returns all failed tasks for a job execution
func (r *JobTaskRepository) GetFailedTasksByJobExecution(ctx context.Context, jobExecutionID uuid.UUID) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status, keyspace_start, keyspace_end,
			keyspace_processed, benchmark_speed, chunk_duration, assigned_at,
			started_at, completed_at, last_checkpoint, error_message,
			COALESCE(crack_count, 0) as crack_count,
			COALESCE(detailed_status, 'pending') as detailed_status,
			COALESCE(retry_count, 0) as retry_count
		FROM job_tasks
		WHERE job_execution_id = $1 AND status = 'failed'`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query failed tasks: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID,
			&task.JobExecutionID,
			&task.AgentID,
			&task.Status,
			&task.KeyspaceStart,
			&task.KeyspaceEnd,
			&task.KeyspaceProcessed,
			&task.BenchmarkSpeed,
			&task.ChunkDuration,
			&task.AssignedAt,
			&task.StartedAt,
			&task.CompletedAt,
			&task.LastCheckpoint,
			&task.ErrorMessage,
			&task.CrackCount,
			&task.DetailedStatus,
			&task.RetryCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// UpdateTaskStatus updates both status and detailed_status fields
func (r *JobTaskRepository) UpdateTaskStatus(ctx context.Context, id uuid.UUID, status, detailedStatus string) error {
	query := `
		UPDATE job_tasks 
		SET status = $2, detailed_status = $3
		WHERE id = $1`

	_, err := r.db.ExecContext(ctx, query, id, status, detailedStatus)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	return nil
}

// ResetTaskForRetry resets a task for retry by incrementing retry count and resetting status
func (r *JobTaskRepository) ResetTaskForRetry(ctx context.Context, id uuid.UUID) error {
	// First, get the current task details including both base and effective keyspace ranges
	var jobExecutionID uuid.UUID
	var keyspaceProcessed int64
	var keyspaceStart int64
	var keyspaceEnd int64
	var effectiveKeyspaceStart sql.NullInt64
	var effectiveKeyspaceEnd sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		"SELECT job_execution_id, keyspace_processed, keyspace_start, keyspace_end, "+
		"effective_keyspace_start, effective_keyspace_end FROM job_tasks WHERE id = $1",
		id).Scan(&jobExecutionID, &keyspaceProcessed, &keyspaceStart, &keyspaceEnd,
		&effectiveKeyspaceStart, &effectiveKeyspaceEnd)
	if err != nil {
		return fmt.Errorf("failed to get task details: %w", err)
	}

	// Begin transaction to ensure atomicity
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Reset the task
	query := `
		UPDATE job_tasks
		SET
			status = 'pending',
			detailed_status = 'pending',
			retry_count = retry_count + 1,
			started_at = NULL,
			completed_at = NULL,
			error_message = NULL,
			keyspace_processed = 0,
			effective_keyspace_processed = 0,
			progress_percent = 0,
			agent_id = NULL,
			last_checkpoint = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`
	_, err = tx.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to reset task for retry: %w", err)
	}

	// Subtract the processed keyspace from the job execution
	if keyspaceProcessed > 0 {
		updateJobQuery := `
			UPDATE job_executions
			SET processed_keyspace = processed_keyspace - $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2 AND processed_keyspace >= $1`
		_, err = tx.ExecContext(ctx, updateJobQuery, keyspaceProcessed, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to update job processed keyspace: %w", err)
		}
	}

	// Decrement the dispatched keyspace to return the work back to the pool
	// Use the EFFECTIVE chunk size that was originally dispatched, not the base chunk size
	// This matches what was added when the task was assigned
	var chunkSize int64
	if effectiveKeyspaceStart.Valid && effectiveKeyspaceEnd.Valid {
		chunkSize = effectiveKeyspaceEnd.Int64 - effectiveKeyspaceStart.Int64
	} else {
		// Fallback to base keyspace if effective not set (shouldn't happen with new code)
		chunkSize = keyspaceEnd - keyspaceStart
	}

	if chunkSize > 0 {
		decrementDispatchedQuery := `
			UPDATE job_executions
			SET dispatched_keyspace = GREATEST(dispatched_keyspace - $1, 0),
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2`
		_, err = tx.ExecContext(ctx, decrementDispatchedQuery, chunkSize, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to decrement job dispatched keyspace: %w", err)
		}

		debug.Log("Decremented dispatched keyspace for task retry", map[string]interface{}{
			"task_id": id,
			"job_execution_id": jobExecutionID,
			"chunk_size_decremented": chunkSize,
			"used_effective_keyspace": effectiveKeyspaceStart.Valid && effectiveKeyspaceEnd.Valid,
		})
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateTaskWithCracks updates a task with crack count and detailed status
func (r *JobTaskRepository) UpdateTaskWithCracks(ctx context.Context, id uuid.UUID, crackCount int, detailedStatus string) error {
	status := "completed"
	if crackCount > 0 {
		detailedStatus = "completed_with_cracks"
	} else if detailedStatus == "" {
		detailedStatus = "completed_no_cracks"
	}

	query := `
		UPDATE job_tasks 
		SET 
			status = $2,
			detailed_status = $3,
			crack_count = $4,
			completed_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	_, err := r.db.ExecContext(ctx, query, id, status, detailedStatus, crackCount)
	if err != nil {
		return fmt.Errorf("failed to update task with cracks: %w", err)
	}

	return nil
}

// UpdateCrackCount increments the crack count for a task
func (r *JobTaskRepository) UpdateCrackCount(ctx context.Context, id uuid.UUID, additionalCracks int) error {
	if additionalCracks <= 0 {
		return nil // Nothing to update
	}

	query := `
		UPDATE job_tasks 
		SET 
			crack_count = COALESCE(crack_count, 0) + $2,
			detailed_status = CASE 
				WHEN COALESCE(crack_count, 0) + $2 > 0 THEN 'running'
				ELSE detailed_status
			END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	_, err := r.db.ExecContext(ctx, query, id, additionalCracks)
	if err != nil {
		return fmt.Errorf("failed to update crack count: %w", err)
	}

	return nil
}

// UpdateCrackCountTx updates the crack count for a task within a transaction
func (r *JobTaskRepository) UpdateCrackCountTx(tx *sql.Tx, id uuid.UUID, additionalCracks int) error {
	if additionalCracks <= 0 {
		return nil // Nothing to update
	}

	query := `
		UPDATE job_tasks 
		SET 
			crack_count = COALESCE(crack_count, 0) + $2,
			detailed_status = CASE 
				WHEN COALESCE(crack_count, 0) + $2 > 0 THEN 'running'
				ELSE detailed_status
			END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	_, err := tx.ExecContext(context.Background(), query, id, additionalCracks)
	if err != nil {
		return fmt.Errorf("failed to update crack count: %w", err)
	}

	return nil
}

// IncrementPotfileEntriesAddedTx increments the potfile_entries_added counter for a task within a transaction
func (r *JobTaskRepository) IncrementPotfileEntriesAddedTx(tx *sql.Tx, id uuid.UUID) error {
	query := `
		UPDATE job_tasks 
		SET 
			potfile_entries_added = COALESCE(potfile_entries_added, 0) + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	_, err := tx.ExecContext(context.Background(), query, id)
	if err != nil {
		return fmt.Errorf("failed to increment potfile entries added: %w", err)
	}

	return nil
}

// GetActiveTaskForAgentAndHashlist finds the active task for a specific agent and hashlist
func (r *JobTaskRepository) GetActiveTaskForAgentAndHashlist(ctx context.Context, agentID int, hashlistID int64) (*models.JobTask, error) {
	query := `
		SELECT
			jt.id, jt.job_execution_id, jt.agent_id, jt.status, jt.priority,
			jt.attack_cmd, jt.keyspace_start, jt.keyspace_end, jt.keyspace_processed,
			jt.progress_percent, jt.benchmark_speed, jt.average_speed, jt.chunk_duration,
			jt.created_at, jt.assigned_at, jt.started_at, jt.completed_at, jt.updated_at,
			jt.last_checkpoint, jt.error_message, jt.crack_count, jt.detailed_status,
			jt.retry_count, jt.rule_start_index, jt.rule_end_index, jt.rule_chunk_path,
			jt.is_rule_split_task
		FROM job_tasks jt
		JOIN job_executions je ON jt.job_execution_id = je.id
		WHERE jt.agent_id = $1
		  AND je.hashlist_id = $2
		  AND jt.status IN ('running', 'assigned')
		ORDER BY jt.assigned_at DESC
		LIMIT 1`

	var task models.JobTask
	err := r.db.QueryRowContext(ctx, query, agentID, hashlistID).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
		&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.ProgressPercent, &task.BenchmarkSpeed, &task.AverageSpeed, &task.ChunkDuration,
		&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
		&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
		&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
		&task.IsRuleSplitTask,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No active task found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active task: %w", err)
	}

	return &task, nil
}

// GetActiveTaskForAgentAndHashlistTx finds the active task within a transaction
func (r *JobTaskRepository) GetActiveTaskForAgentAndHashlistTx(tx *sql.Tx, agentID int, hashlistID int64) (*models.JobTask, error) {
	query := `
		SELECT
			jt.id, jt.job_execution_id, jt.agent_id, jt.status, jt.priority,
			jt.attack_cmd, jt.keyspace_start, jt.keyspace_end, jt.keyspace_processed,
			jt.progress_percent, jt.benchmark_speed, jt.average_speed, jt.chunk_duration,
			jt.created_at, jt.assigned_at, jt.started_at, jt.completed_at, jt.updated_at,
			jt.last_checkpoint, jt.error_message, jt.crack_count, jt.detailed_status,
			jt.retry_count, jt.rule_start_index, jt.rule_end_index, jt.rule_chunk_path,
			jt.is_rule_split_task
		FROM job_tasks jt
		JOIN job_executions je ON jt.job_execution_id = je.id
		WHERE jt.agent_id = $1
		  AND je.hashlist_id = $2
		  AND jt.status IN ('running', 'assigned')
		ORDER BY jt.assigned_at DESC
		LIMIT 1`

	var task models.JobTask
	err := tx.QueryRowContext(context.Background(), query, agentID, hashlistID).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
		&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.ProgressPercent, &task.BenchmarkSpeed, &task.AverageSpeed, &task.ChunkDuration,
		&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
		&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
		&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
		&task.IsRuleSplitTask,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No active task found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active task: %w", err)
	}

	return &task, nil
}

// GetActiveAgentCountByJob returns the number of agents actively working on a job
func (r *JobTaskRepository) GetActiveAgentCountByJob(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(DISTINCT agent_id) 
		FROM job_tasks 
		WHERE job_execution_id = $1 
		  AND status IN ('running', 'assigned')
		  AND agent_id IS NOT NULL`

	var count int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get active agent count: %w", err)
	}

	return count, nil
}

// GetTaskCountByJobExecution returns the total number of tasks for a job execution
func (r *JobTaskRepository) GetTaskCountByJobExecution(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM job_tasks 
		WHERE job_execution_id = $1`

	var count int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get task count: %w", err)
	}

	return count, nil
}

// GetActiveTasksCount returns the number of active tasks (running or assigned) for a job
func (r *JobTaskRepository) GetActiveTasksCount(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM job_tasks 
		WHERE job_execution_id = $1 
		  AND status IN ('running', 'assigned')`

	var count int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get active tasks count: %w", err)
	}

	return count, nil
}

// GetTasksByStatuses retrieves tasks with specific statuses
func (r *JobTaskRepository) GetTasksByStatuses(ctx context.Context, statuses []string) ([]models.JobTask, error) {
	if len(statuses) == 0 {
		return []models.JobTask{}, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(statuses))
	args := make([]interface{}, len(statuses))
	for i, status := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = status
	}

	query := fmt.Sprintf(`
		SELECT 
			id, job_execution_id, agent_id, status,
			keyspace_start, keyspace_end, keyspace_processed,
			benchmark_speed, chunk_duration, assigned_at,
			started_at, completed_at, last_checkpoint, error_message,
			created_at, updated_at, retry_count
		FROM job_tasks
		WHERE status IN (%s)
		ORDER BY created_at ASC`, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks by statuses: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status,
			&task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.BenchmarkSpeed, &task.ChunkDuration, &task.AssignedAt,
			&task.StartedAt, &task.CompletedAt, &task.LastCheckpoint, &task.ErrorMessage,
			&task.CreatedAt, &task.UpdatedAt, &task.RetryCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetMaxRuleEndIndex returns the maximum rule_end_index for a job execution
// This is used to determine where to start the next rule chunk
func (r *JobTaskRepository) GetMaxRuleEndIndex(ctx context.Context, jobExecutionID uuid.UUID) (*int, error) {
	query := `
		SELECT MAX(rule_end_index)
		FROM job_tasks
		WHERE job_execution_id = $1 AND is_rule_split_task = true`

	var maxEnd sql.NullInt64
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&maxEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get max rule end index: %w", err)
	}

	if !maxEnd.Valid {
		return nil, nil // No rule split tasks exist yet
	}

	result := int(maxEnd.Int64)
	return &result, nil
}

// GetNextChunkNumber returns the next chunk number for a job execution
// Chunk numbers are sequential per job (1, 2, 3, ...)
func (r *JobTaskRepository) GetNextChunkNumber(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `
		SELECT COALESCE(MAX(chunk_number), 0) + 1
		FROM job_tasks
		WHERE job_execution_id = $1`

	var nextNumber int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&nextNumber)
	if err != nil {
		return 0, fmt.Errorf("failed to get next chunk number: %w", err)
	}

	return nextNumber, nil
}

// GetReconnectPendingTasksByAgent retrieves all reconnect_pending tasks for a specific agent
func (r *JobTaskRepository) GetReconnectPendingTasksByAgent(ctx context.Context, agentID int) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status, priority,
			attack_cmd, keyspace_start, keyspace_end, keyspace_processed,
			progress_percent, benchmark_speed, chunk_duration,
			created_at, assigned_at, started_at, completed_at, updated_at,
			last_checkpoint, error_message, crack_count, detailed_status,
			retry_count, rule_start_index, rule_end_index, rule_chunk_path,
			is_rule_split_task, chunk_number,
			effective_keyspace_start, effective_keyspace_end, effective_keyspace_processed
		FROM job_tasks
		WHERE agent_id = $1 AND status = $2
		ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, agentID, models.JobTaskStatusReconnectPending)
	if err != nil {
		return nil, fmt.Errorf("failed to get reconnect_pending tasks for agent: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
			&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.ProgressPercent, &task.BenchmarkSpeed, &task.ChunkDuration,
			&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
			&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
			&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
			&task.IsRuleSplitTask, &task.ChunkNumber,
			&task.EffectiveKeyspaceStart, &task.EffectiveKeyspaceEnd, &task.EffectiveKeyspaceProcessed,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetTasksByChunkNumber retrieves tasks by job execution ID and chunk number
// Useful for user queries like "show me chunk 5 for job X"
func (r *JobTaskRepository) GetTasksByChunkNumber(ctx context.Context, jobExecutionID uuid.UUID, chunkNumber int) ([]models.JobTask, error) {
	query := `
		SELECT 
			id, job_execution_id, agent_id, status, priority,
			attack_cmd, keyspace_start, keyspace_end, keyspace_processed,
			progress_percent, benchmark_speed, chunk_duration,
			created_at, assigned_at, started_at, completed_at, updated_at,
			last_checkpoint, error_message, crack_count, detailed_status,
			retry_count, rule_start_index, rule_end_index, rule_chunk_path,
			is_rule_split_task, chunk_number
		FROM job_tasks
		WHERE job_execution_id = $1 AND chunk_number = $2
		ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, jobExecutionID, chunkNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks by chunk number: %w", err)
	}
	defer rows.Close()

	var tasks []models.JobTask
	for rows.Next() {
		var task models.JobTask
		err := rows.Scan(
			&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
			&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
			&task.ProgressPercent, &task.BenchmarkSpeed, &task.ChunkDuration,
			&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
			&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
			&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
			&task.IsRuleSplitTask, &task.ChunkNumber,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// GetRetriableErrorTask gets a task in error state that can be retried
func (r *JobTaskRepository) GetRetriableErrorTask(ctx context.Context, jobExecutionID uuid.UUID, maxRetries int) (*models.JobTask, error) {
	query := `
		SELECT id, job_execution_id, agent_id, status, priority, attack_cmd,
			keyspace_start, keyspace_end, keyspace_processed, progress_percent,
			benchmark_speed, chunk_duration, created_at, assigned_at, started_at, 
			completed_at, updated_at, last_checkpoint, error_message, crack_count, 
			detailed_status, retry_count, rule_start_index, rule_end_index, 
			rule_chunk_path, is_rule_split_task, chunk_number
		FROM job_tasks
		WHERE job_execution_id = $1 
			AND status = 'error' 
			AND retry_count < $2
		ORDER BY created_at ASC
		LIMIT 1`

	var task models.JobTask
	err := r.db.QueryRowContext(ctx, query, jobExecutionID, maxRetries).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
		&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.ProgressPercent, &task.BenchmarkSpeed, &task.ChunkDuration,
		&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
		&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
		&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
		&task.IsRuleSplitTask, &task.ChunkNumber,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get retriable error task: %w", err)
	}

	return &task, nil
}

// GetStalePendingTask gets a pending task that hasn't been updated in the specified duration
func (r *JobTaskRepository) GetStalePendingTask(ctx context.Context, jobExecutionID uuid.UUID, staleDuration time.Duration) (*models.JobTask, error) {
	cutoffTime := time.Now().Add(-staleDuration)
	query := `
		SELECT id, job_execution_id, agent_id, status, priority, attack_cmd,
			keyspace_start, keyspace_end, keyspace_processed, progress_percent,
			benchmark_speed, chunk_duration, created_at, assigned_at, started_at, 
			completed_at, updated_at, last_checkpoint, error_message, crack_count, 
			detailed_status, retry_count, rule_start_index, rule_end_index, 
			rule_chunk_path, is_rule_split_task, chunk_number
		FROM job_tasks
		WHERE job_execution_id = $1 
			AND status IN ('pending', 'running')
			AND agent_id IS NOT NULL
			AND (last_checkpoint IS NULL OR last_checkpoint < $2)
			AND assigned_at < $2
		ORDER BY created_at ASC
		LIMIT 1`

	var task models.JobTask
	err := r.db.QueryRowContext(ctx, query, jobExecutionID, cutoffTime).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
		&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.ProgressPercent, &task.BenchmarkSpeed, &task.ChunkDuration,
		&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
		&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
		&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
		&task.IsRuleSplitTask, &task.ChunkNumber,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get stale pending task: %w", err)
	}

	return &task, nil
}

// GetUnassignedPendingTask gets a pending task that hasn't been assigned to any agent
func (r *JobTaskRepository) GetUnassignedPendingTask(ctx context.Context, jobExecutionID uuid.UUID) (*models.JobTask, error) {
	query := `
		SELECT id, job_execution_id, agent_id, status, priority, attack_cmd,
			keyspace_start, keyspace_end, keyspace_processed, progress_percent,
			benchmark_speed, chunk_duration, created_at, assigned_at, started_at, 
			completed_at, updated_at, last_checkpoint, error_message, crack_count, 
			detailed_status, retry_count, rule_start_index, rule_end_index, 
			rule_chunk_path, is_rule_split_task, chunk_number
		FROM job_tasks
		WHERE job_execution_id = $1 
			AND status = 'pending'
			AND agent_id IS NULL
		ORDER BY created_at ASC
		LIMIT 1`

	var task models.JobTask
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(
		&task.ID, &task.JobExecutionID, &task.AgentID, &task.Status, &task.Priority,
		&task.AttackCmd, &task.KeyspaceStart, &task.KeyspaceEnd, &task.KeyspaceProcessed,
		&task.ProgressPercent, &task.BenchmarkSpeed, &task.ChunkDuration,
		&task.CreatedAt, &task.AssignedAt, &task.StartedAt, &task.CompletedAt, &task.UpdatedAt,
		&task.LastCheckpoint, &task.ErrorMessage, &task.CrackCount, &task.DetailedStatus,
		&task.RetryCount, &task.RuleStartIndex, &task.RuleEndIndex, &task.RuleChunkPath,
		&task.IsRuleSplitTask, &task.ChunkNumber,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned pending task: %w", err)
	}

	return &task, nil
}

// AssignTaskToAgent assigns a pending task to an agent
func (r *JobTaskRepository) AssignTaskToAgent(ctx context.Context, taskID uuid.UUID, agentID int) error {
	query := `
		UPDATE job_tasks 
		SET status = 'assigned', 
		    agent_id = $2,
		    assigned_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'pending'`

	result, err := r.db.ExecContext(ctx, query, taskID, agentID)
	if err != nil {
		return fmt.Errorf("failed to assign task to agent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("task not found or not in pending status")
	}

	return nil
}

// GetTasksByAgentAndStatus gets all tasks for a specific agent with a given status
func (r *JobTaskRepository) GetTasksByAgentAndStatus(ctx context.Context, agentID int, status models.JobTaskStatus) ([]uuid.UUID, error) {
	query := `
		SELECT id
		FROM job_tasks
		WHERE agent_id = $1 AND status = $2
		ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, agentID, status)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks by agent and status: %w", err)
	}
	defer rows.Close()

	var taskIDs []uuid.UUID
	for rows.Next() {
		var taskID uuid.UUID
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("failed to scan task ID: %w", err)
		}
		taskIDs = append(taskIDs, taskID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return taskIDs, nil
}

// UpdateTaskEffectiveKeyspace updates the effective keyspace for a task with actual values from hashcat
func (r *JobTaskRepository) UpdateTaskEffectiveKeyspace(ctx context.Context, taskID uuid.UUID, effectiveKeyspaceStart, effectiveKeyspaceEnd int64) error {
	query := `
		UPDATE job_tasks
		SET effective_keyspace_start = $2,
		    effective_keyspace_end = $3,
		    is_actual_keyspace = true,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, taskID, effectiveKeyspaceStart, effectiveKeyspaceEnd)
	if err != nil {
		return fmt.Errorf("failed to update task effective keyspace: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateTaskEffectiveKeyspaceWithChunkSize updates effective keyspace values and stores the actual chunk size
// This enables self-correcting cascade updates for subsequent chunks
func (r *JobTaskRepository) UpdateTaskEffectiveKeyspaceWithChunkSize(ctx context.Context, taskID uuid.UUID, effectiveKeyspaceStart, effectiveKeyspaceEnd, chunkActualKeyspace int64) error {
	query := `
		UPDATE job_tasks
		SET effective_keyspace_start = $2,
		    effective_keyspace_end = $3,
		    chunk_actual_keyspace = $4,
		    is_actual_keyspace = true,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, taskID, effectiveKeyspaceStart, effectiveKeyspaceEnd, chunkActualKeyspace)
	if err != nil {
		return fmt.Errorf("failed to update task effective keyspace with chunk size: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// CalculateAndStoreAverageSpeed calculates the time-weighted average speed for a task and stores it
func (r *JobTaskRepository) CalculateAndStoreAverageSpeed(ctx context.Context, taskID uuid.UUID) error {
	// Query all speed metrics for this task from agent_performance_metrics
	query := `
		SELECT value, timestamp
		FROM agent_performance_metrics
		WHERE task_id = $1
		  AND metric_type = 'hash_rate'
		  AND aggregation_level = 'realtime'
		ORDER BY timestamp ASC`

	rows, err := r.db.QueryContext(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("failed to query speed metrics: %w", err)
	}
	defer rows.Close()

	var metrics []struct {
		Speed     float64
		Timestamp time.Time
	}

	for rows.Next() {
		var m struct {
			Speed     float64
			Timestamp time.Time
		}
		err := rows.Scan(&m.Speed, &m.Timestamp)
		if err != nil {
			return fmt.Errorf("failed to scan metric: %w", err)
		}
		metrics = append(metrics, m)
	}

	if len(metrics) == 0 {
		// No metrics recorded, cannot calculate average
		return nil
	}

	// Calculate time-weighted average
	var totalWeightedSpeed float64
	var totalDuration float64

	for i := 0; i < len(metrics)-1; i++ {
		duration := metrics[i+1].Timestamp.Sub(metrics[i].Timestamp).Seconds()
		totalWeightedSpeed += metrics[i].Speed * duration
		totalDuration += duration
	}

	// For the last metric, use a reasonable duration (e.g., 10 seconds) or until now
	if len(metrics) > 0 {
		lastDuration := 10.0 // Default 10 seconds for last measurement
		// Or use time until now if task just completed
		timeSinceLast := time.Since(metrics[len(metrics)-1].Timestamp).Seconds()
		if timeSinceLast < 60 { // If less than 60 seconds ago, use actual time
			lastDuration = timeSinceLast
		}
		totalWeightedSpeed += metrics[len(metrics)-1].Speed * lastDuration
		totalDuration += lastDuration
	}

	var averageSpeed int64
	if totalDuration > 0 {
		averageSpeed = int64(totalWeightedSpeed / totalDuration)
	}

	// Update the task with the calculated average speed
	updateQuery := `
		UPDATE job_tasks
		SET average_speed = $1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`

	_, err = r.db.ExecContext(ctx, updateQuery, averageSpeed, taskID)
	if err != nil {
		return fmt.Errorf("failed to update average speed: %w", err)
	}

	return nil
}

// GetTaskCountForJob returns the number of tasks (all statuses) for a job
func (r *JobTaskRepository) GetTaskCountForJob(ctx context.Context, jobExecutionID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM job_tasks WHERE job_execution_id = $1`

	var count int
	err := r.db.QueryRowContext(ctx, query, jobExecutionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get task count: %w", err)
	}

	return count, nil
}
