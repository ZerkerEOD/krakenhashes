package repository

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// ListWithPagination retrieves job executions with pagination, ordered by priority and creation time
func (r *JobExecutionRepository) ListWithPagination(ctx context.Context, limit, offset int) ([]models.JobExecution, error) {
	query := `
		SELECT 
			id, preset_job_id, hashlist_id, status, priority, COALESCE(max_agents, 0) as max_agents,
			total_keyspace, processed_keyspace, attack_mode, created_by,
			created_at, started_at, completed_at, error_message, interrupted_by, updated_at
		FROM job_executions
		ORDER BY 
			-- Active jobs first (pending, running, paused)
			CASE 
				WHEN status IN ('pending', 'running', 'paused') THEN 0
				ELSE 1
			END,
			-- Within active jobs: by priority DESC, created_at ASC
			CASE 
				WHEN status IN ('pending', 'running', 'paused') THEN priority
				ELSE NULL
			END DESC,
			CASE 
				WHEN status IN ('pending', 'running', 'paused') THEN created_at
				ELSE NULL
			END ASC,
			-- Within completed jobs: by completed_at DESC (most recent first)
			-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
			CASE
				WHEN status NOT IN ('pending', 'running', 'paused')
				THEN COALESCE(completed_at, updated_at, created_at)
				ELSE NULL
			END DESC NULLS LAST
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list job executions: %w", err)
	}
	defer rows.Close()

	var executions []models.JobExecution
	for rows.Next() {
		var exec models.JobExecution
		err := rows.Scan(
			&exec.ID, &exec.PresetJobID, &exec.HashlistID, &exec.Status, &exec.Priority, &exec.MaxAgents,
			&exec.TotalKeyspace, &exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
			&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution: %w", err)
		}
		executions = append(executions, exec)
	}

	return executions, nil
}

// JobFilter contains filter criteria for job queries
type JobFilter struct {
	Status   *string
	Priority *int
	Search   *string
	UserID   *string
}

// JobExecutionWithUser represents a job execution with user information
type JobExecutionWithUser struct {
	models.JobExecution
	CreatedByUsername *string `db:"created_by_username"`
}

// ListWithFilters retrieves job executions with pagination and filters
func (r *JobExecutionRepository) ListWithFilters(ctx context.Context, limit, offset int, filter JobFilter) ([]models.JobExecution, error) {
	query := `
		SELECT 
			je.id, je.preset_job_id, je.hashlist_id, je.status, je.priority, COALESCE(je.max_agents, 0) as max_agents,
			je.total_keyspace, je.processed_keyspace, je.attack_mode, je.created_by,
			je.created_at, je.started_at, je.completed_at, je.error_message, je.interrupted_by, je.updated_at
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN users u ON je.created_by = u.id
		WHERE 1=1`

	args := []interface{}{}
	argCount := 0

	// Apply status filter
	if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}

	// Apply priority filter
	if filter.Priority != nil {
		argCount++
		query += fmt.Sprintf(" AND je.priority = $%d", argCount)
		args = append(args, *filter.Priority)
	}

	// Apply search filter (search in job name, preset job name and hashlist name)
	if filter.Search != nil && *filter.Search != "" {
		argCount++
		query += fmt.Sprintf(" AND (je.name ILIKE $%d OR pj.name ILIKE $%d OR h.name ILIKE $%d)", argCount, argCount, argCount)
		searchPattern := "%" + *filter.Search + "%"
		args = append(args, searchPattern)
	}

	// Apply user filter - filter by job creator, not hashlist owner
	if filter.UserID != nil && *filter.UserID != "" {
		argCount++
		query += fmt.Sprintf(" AND je.created_by = $%d", argCount)
		args = append(args, *filter.UserID)
	}

	// Add ordering
	query += ` ORDER BY
		-- Active jobs first (pending, running, paused)
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN 0
			ELSE 1
		END,
		-- Within active jobs: by priority DESC, created_at ASC
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN je.priority
			ELSE NULL
		END DESC,
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN je.created_at
			ELSE NULL
		END ASC,
		-- Within completed jobs: by completed_at DESC (most recent first)
		-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
		CASE
			WHEN je.status NOT IN ('pending', 'running', 'paused')
			THEN COALESCE(je.completed_at, je.updated_at, je.created_at)
			ELSE NULL
		END DESC NULLS LAST`

	// Add pagination
	argCount++
	query += fmt.Sprintf(" LIMIT $%d", argCount)
	args = append(args, limit)

	argCount++
	query += fmt.Sprintf(" OFFSET $%d", argCount)
	args = append(args, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list job executions with filters: %w", err)
	}
	defer rows.Close()

	var executions []models.JobExecution
	for rows.Next() {
		var exec models.JobExecution
		err := rows.Scan(
			&exec.ID, &exec.PresetJobID, &exec.HashlistID, &exec.Status, &exec.Priority, &exec.MaxAgents,
			&exec.TotalKeyspace, &exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
			&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution: %w", err)
		}
		executions = append(executions, exec)
	}

	return executions, nil
}

// GetTotalCount returns the total number of job executions
func (r *JobExecutionRepository) GetTotalCount(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM job_executions`
	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total job execution count: %w", err)
	}
	return count, nil
}

// GetFilteredCount returns the number of job executions matching the filter
func (r *JobExecutionRepository) GetFilteredCount(ctx context.Context, filter JobFilter) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		WHERE 1=1`

	args := []interface{}{}
	argCount := 0

	// Apply status filter
	if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}

	// Apply priority filter
	if filter.Priority != nil {
		argCount++
		query += fmt.Sprintf(" AND je.priority = $%d", argCount)
		args = append(args, *filter.Priority)
	}

	// Apply search filter
	if filter.Search != nil && *filter.Search != "" {
		argCount++
		query += fmt.Sprintf(" AND (pj.name ILIKE $%d OR h.name ILIKE $%d)", argCount, argCount)
		searchPattern := "%" + *filter.Search + "%"
		args = append(args, searchPattern)
	}

	// Apply user filter - filter by job creator, not hashlist owner
	if filter.UserID != nil && *filter.UserID != "" {
		argCount++
		query += fmt.Sprintf(" AND je.created_by = $%d", argCount)
		args = append(args, *filter.UserID)
	}

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get filtered job execution count: %w", err)
	}
	return count, nil
}

// GetStatusCounts returns counts of jobs grouped by status
func (r *JobExecutionRepository) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM job_executions
		GROUP BY status`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get status counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan status count: %w", err)
		}
		counts[status] = count
	}

	return counts, nil
}

// GetStatusCountsForUser returns counts of jobs grouped by status for a specific user
func (r *JobExecutionRepository) GetStatusCountsForUser(ctx context.Context, userID string) (map[string]int, error) {
	query := `
		SELECT je.status, COUNT(*) as count
		FROM job_executions je
		WHERE je.created_by = $1
		GROUP BY je.status`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get status counts for user: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan status count: %w", err)
		}
		counts[status] = count
	}

	return counts, nil
}

// UpdatePriority updates the priority of a job execution
func (r *JobExecutionRepository) UpdatePriority(ctx context.Context, id uuid.UUID, priority int) error {
	query := `UPDATE job_executions SET priority = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, priority, id)
	if err != nil {
		return fmt.Errorf("failed to update job execution priority: %w", err)
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

// UpdateMaxAgents updates the max agents for a job execution
func (r *JobExecutionRepository) UpdateMaxAgents(ctx context.Context, id uuid.UUID, maxAgents int) error {
	query := `UPDATE job_executions SET max_agents = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, maxAgents, id)
	if err != nil {
		return fmt.Errorf("failed to update job execution max agents: %w", err)
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

// UpdateChunkSizeSeconds updates the chunk size (in seconds) for a job execution
func (r *JobExecutionRepository) UpdateChunkSizeSeconds(ctx context.Context, id uuid.UUID, chunkSizeSeconds int) error {
	query := `UPDATE job_executions SET chunk_size_seconds = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, chunkSizeSeconds, id)
	if err != nil {
		return fmt.Errorf("failed to update job execution chunk size: %w", err)
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

// Delete deletes a job execution and related tasks
func (r *JobExecutionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear any references to this job in the interrupted_by column
	_, err = tx.ExecContext(ctx, `UPDATE job_executions SET interrupted_by = NULL WHERE interrupted_by = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to clear interrupted_by references: %w", err)
	}

	// Delete related performance metrics
	_, err = tx.ExecContext(ctx, `DELETE FROM job_performance_metrics WHERE job_execution_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete related performance metrics: %w", err)
	}

	// Delete related tasks
	_, err = tx.ExecContext(ctx, `DELETE FROM job_tasks WHERE job_execution_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete related job tasks: %w", err)
	}

	// Delete job execution
	result, err := tx.ExecContext(ctx, `DELETE FROM job_executions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete job execution: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// DeleteFinished deletes all completed job executions
func (r *JobExecutionRepository) DeleteFinished(ctx context.Context) (int, error) {
	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear any references to finished jobs in the interrupted_by column
	_, err = tx.ExecContext(ctx, `
		UPDATE job_executions 
		SET interrupted_by = NULL 
		WHERE interrupted_by IN (
			SELECT id FROM job_executions 
			WHERE status IN ('completed', 'failed', 'cancelled')
		)`)
	if err != nil {
		return 0, fmt.Errorf("failed to clear interrupted_by references: %w", err)
	}

	// Delete related performance metrics
	_, err = tx.ExecContext(ctx, `
		DELETE FROM job_performance_metrics 
		WHERE job_execution_id IN (
			SELECT id FROM job_executions 
			WHERE status IN ('completed', 'failed', 'cancelled')
		)`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete related performance metrics: %w", err)
	}

	// Delete related tasks
	_, err = tx.ExecContext(ctx, `
		DELETE FROM job_tasks 
		WHERE job_execution_id IN (
			SELECT id FROM job_executions 
			WHERE status IN ('completed', 'failed', 'cancelled')
		)`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete related job tasks: %w", err)
	}

	// Delete finished job executions
	result, err := tx.ExecContext(ctx, `
		DELETE FROM job_executions 
		WHERE status IN ('completed', 'failed', 'cancelled')`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete finished job executions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return int(rowsAffected), nil
}

// ListWithFiltersAndUser retrieves job executions with user information
func (r *JobExecutionRepository) ListWithFiltersAndUser(ctx context.Context, limit, offset int, filter JobFilter) ([]JobExecutionWithUser, error) {
	query := `
		SELECT 
			je.id, je.preset_job_id, je.hashlist_id, je.status, je.priority, COALESCE(je.max_agents, 0) as max_agents,
			je.total_keyspace, je.processed_keyspace, je.attack_mode, je.created_by,
			je.created_at, je.started_at, je.completed_at, je.error_message, je.interrupted_by, je.updated_at,
			je.base_keyspace, je.effective_keyspace, je.multiplication_factor, je.uses_rule_splitting,
			je.rule_split_count, je.overall_progress_percent, je.dispatched_keyspace,
			je.name,
			u.username as created_by_username
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN users u ON je.created_by = u.id
		WHERE 1=1`

	args := []interface{}{}
	argCount := 0

	// Apply status filter
	if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}

	// Apply priority filter
	if filter.Priority != nil {
		argCount++
		query += fmt.Sprintf(" AND je.priority = $%d", argCount)
		args = append(args, *filter.Priority)
	}

	// Apply search filter (search in job name, preset job name and hashlist name)
	if filter.Search != nil && *filter.Search != "" {
		argCount++
		query += fmt.Sprintf(" AND (je.name ILIKE $%d OR pj.name ILIKE $%d OR h.name ILIKE $%d)", argCount, argCount, argCount)
		searchPattern := "%" + *filter.Search + "%"
		args = append(args, searchPattern)
	}

	// Apply user filter - filter by job creator, not hashlist owner
	if filter.UserID != nil && *filter.UserID != "" {
		argCount++
		query += fmt.Sprintf(" AND je.created_by = $%d", argCount)
		args = append(args, *filter.UserID)
	}

	// Add ordering
	query += ` ORDER BY
		-- Active jobs first (pending, running, paused)
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN 0
			ELSE 1
		END,
		-- Within active jobs: by priority DESC, created_at ASC
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN je.priority
			ELSE NULL
		END DESC,
		CASE
			WHEN je.status IN ('pending', 'running', 'paused') THEN je.created_at
			ELSE NULL
		END ASC,
		-- Within completed jobs: by completed_at DESC (most recent first)
		-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
		CASE
			WHEN je.status NOT IN ('pending', 'running', 'paused')
			THEN COALESCE(je.completed_at, je.updated_at, je.created_at)
			ELSE NULL
		END DESC NULLS LAST`

	// Add pagination
	argCount++
	query += fmt.Sprintf(" LIMIT $%d", argCount)
	args = append(args, limit)

	argCount++
	query += fmt.Sprintf(" OFFSET $%d", argCount)
	args = append(args, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list job executions with user: %w", err)
	}
	defer rows.Close()

	var executions []JobExecutionWithUser
	for rows.Next() {
		var exec JobExecutionWithUser
		err := rows.Scan(
			&exec.ID, &exec.PresetJobID, &exec.HashlistID, &exec.Status, &exec.Priority, &exec.MaxAgents,
			&exec.TotalKeyspace, &exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
			&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
			&exec.BaseKeyspace, &exec.EffectiveKeyspace, &exec.MultiplicationFactor, &exec.UsesRuleSplitting,
			&exec.RuleSplitCount, &exec.OverallProgressPercent, &exec.DispatchedKeyspace,
			&exec.Name,
			&exec.CreatedByUsername,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution with user: %w", err)
		}
		executions = append(executions, exec)
	}

	return executions, nil
}
