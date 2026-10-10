package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ListWithPagination retrieves job executions with pagination, ordered by priority and creation time
func (r *JobExecutionRepository) ListWithPagination(ctx context.Context, limit, offset int) ([]models.JobExecution, error) {
	query := `
		SELECT
			id, preset_job_id, hashlist_id, status, priority, COALESCE(max_agents, 0) as max_agents,
			processed_keyspace, attack_mode, created_by,
			created_at, started_at, completed_at, error_message, interrupted_by, updated_at
		FROM job_executions
		ORDER BY
			-- Active jobs first (pending, running, paused)
			CASE
				WHEN status IN ('preparing', 'pending', 'running', 'paused') THEN 0
				ELSE 1
			END,
			-- Within active jobs: by priority DESC, created_at ASC (FIFO: oldest first, matching scheduler run order)
			CASE
				WHEN status IN ('preparing', 'pending', 'running', 'paused') THEN priority
				ELSE NULL
			END DESC,
			CASE
				WHEN status IN ('preparing', 'pending', 'running', 'paused') THEN created_at
				ELSE NULL
			END ASC,
			-- Within completed jobs: by completed_at DESC (most recent first)
			-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
			CASE
				WHEN status NOT IN ('preparing', 'pending', 'running', 'paused')
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
			&exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
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
	Status          *string
	Statuses        []string // Multi-status filter (status = ANY); takes precedence over Status when set
	Priority        *int
	Search          *string
	UserID          *string
	HashlistID      *int64      // Only jobs against this hashlist
	ClientID        *uuid.UUID  // Only jobs whose hashlist belongs to this client
	TeamsEnabled    bool        // Whether team filtering is active (fail-closed when true + empty TeamIDs)
	TeamIDs         []uuid.UUID // When set, filter jobs by team access (via hashlist → client → client_teams)
	IncludeArchived bool        // When false (default), exclude archived jobs
}

// JobExecutionWithUser represents a job execution with user information
type JobExecutionWithUser struct {
	models.JobExecution
	CreatedByUsername *string    `db:"created_by_username"`
	PresetJobName     *string    `db:"preset_job_name"` // nil for custom jobs (no preset)
	ClientID          *uuid.UUID `db:"client_id"`       // hashlist's client (nil when unassigned)
	ClientName        *string    `db:"client_name"`
	WorkflowID        *uuid.UUID `db:"workflow_id"` // workflow that created the job (nil for preset/custom)
	WorkflowName      *string    `db:"workflow_name"`
}

// jobEntityFilters appends the hashlist/client/multi-status predicates shared by
// the list, count and status-count queries. `h` must alias hashlists.
func jobEntityFilters(query string, args []interface{}, argCount int, filter JobFilter) (string, []interface{}, int) {
	if filter.HashlistID != nil {
		argCount++
		query += fmt.Sprintf(" AND je.hashlist_id = $%d", argCount)
		args = append(args, *filter.HashlistID)
	}
	if filter.ClientID != nil {
		argCount++
		query += fmt.Sprintf(" AND h.client_id = $%d", argCount)
		args = append(args, *filter.ClientID)
	}
	return query, args, argCount
}

// GetByIDWithUser loads one job execution with the creator, preset, client and
// workflow names resolved, for the job detail page.
func (r *JobExecutionRepository) GetByIDWithUser(ctx context.Context, id uuid.UUID) (*JobExecutionWithUser, error) {
	query := `
		SELECT
			je.id, je.preset_job_id, je.hashlist_id, je.status, je.priority, COALESCE(je.max_agents, 0) as max_agents,
			je.processed_keyspace, je.attack_mode, je.created_by,
			je.created_at, je.started_at, je.completed_at, je.error_message, je.interrupted_by, je.updated_at,
			je.base_keyspace, je.effective_keyspace, je.multiplication_factor,
			je.overall_progress_percent, je.dispatched_keyspace,
			je.name, je.archived_at,
			u.username as created_by_username,
			pj.name as preset_job_name,
			h.client_id, c.name as client_name,
			je.workflow_id, jw.name as workflow_name
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN clients c ON c.id = h.client_id
		LEFT JOIN job_workflows jw ON jw.id = je.workflow_id
		LEFT JOIN users u ON je.created_by = u.id
		WHERE je.id = $1`
	var exec JobExecutionWithUser
	var clientID sql.Null[uuid.UUID]
	var workflowID sql.Null[uuid.UUID]
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&exec.ID, &exec.PresetJobID, &exec.HashlistID, &exec.Status, &exec.Priority, &exec.MaxAgents,
		&exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
		&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
		&exec.BaseKeyspace, &exec.EffectiveKeyspace, &exec.MultiplicationFactor,
		&exec.OverallProgressPercent, &exec.DispatchedKeyspace,
		&exec.Name, &exec.ArchivedAt,
		&exec.CreatedByUsername,
		&exec.PresetJobName,
		&clientID, &exec.ClientName,
		&workflowID, &exec.WorkflowName,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job execution with user: %w", err)
	}
	if clientID.Valid {
		v := clientID.V
		exec.ClientID = &v
	}
	if workflowID.Valid {
		v := workflowID.V
		exec.WorkflowID = &v
	}
	return &exec, nil
}

// SetWorkflowID records the workflow that created a job (linking only).
func (r *JobExecutionRepository) SetWorkflowID(ctx context.Context, id, workflowID uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE job_executions SET workflow_id = $1 WHERE id = $2`, workflowID, id); err != nil {
		return fmt.Errorf("failed to set workflow id: %w", err)
	}
	return nil
}

// ListWithFilters retrieves job executions with pagination and filters
func (r *JobExecutionRepository) ListWithFilters(ctx context.Context, limit, offset int, filter JobFilter) ([]models.JobExecution, error) {
	query := `
		SELECT
			je.id, je.preset_job_id, je.hashlist_id, je.status, je.priority, COALESCE(je.max_agents, 0) as max_agents,
			je.processed_keyspace, je.attack_mode, je.created_by,
			je.created_at, je.started_at, je.completed_at, je.error_message, je.interrupted_by, je.updated_at,
			je.archived_at
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN users u ON je.created_by = u.id
		WHERE 1=1`

	args := []interface{}{}
	argCount := 0

	// Apply archive filter
	if !filter.IncludeArchived {
		query += " AND je.archived_at IS NULL"
	}

	// Apply status filter (multi-status takes precedence)
	if len(filter.Statuses) > 0 {
		argCount++
		query += fmt.Sprintf(" AND je.status = ANY($%d::text[])", argCount)
		args = append(args, pq.Array(filter.Statuses))
	} else if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}
	query, args, argCount = jobEntityFilters(query, args, argCount, filter)

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
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN 0
			ELSE 1
		END,
		-- Within active jobs: by priority DESC, created_at ASC (FIFO: oldest first, matching scheduler run order)
		CASE
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN je.priority
			ELSE NULL
		END DESC,
		CASE
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN je.created_at
			ELSE NULL
		END ASC,
		-- Within completed jobs: by completed_at DESC (most recent first)
		-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
		CASE
			WHEN je.status NOT IN ('preparing', 'pending', 'running', 'paused')
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
			&exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
			&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
			&exec.ArchivedAt,
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

	// Apply archive filter
	if !filter.IncludeArchived {
		query += " AND je.archived_at IS NULL"
	}

	// Apply status filter (multi-status takes precedence)
	if len(filter.Statuses) > 0 {
		argCount++
		query += fmt.Sprintf(" AND je.status = ANY($%d::text[])", argCount)
		args = append(args, pq.Array(filter.Statuses))
	} else if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}
	query, args, argCount = jobEntityFilters(query, args, argCount, filter)

	// Apply priority filter
	if filter.Priority != nil {
		argCount++
		query += fmt.Sprintf(" AND je.priority = $%d", argCount)
		args = append(args, *filter.Priority)
	}

	// Apply search filter
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

	// Apply team filter - filter by team access via hashlist → client → client_teams
	if filter.TeamsEnabled && len(filter.TeamIDs) == 0 {
		// Teams enabled but no teams — fail-closed: no results
		return 0, nil
	}
	if len(filter.TeamIDs) > 0 {
		teamIDStrs := make([]string, len(filter.TeamIDs))
		for i, id := range filter.TeamIDs {
			teamIDStrs[i] = id.String()
		}
		argCount++
		query += fmt.Sprintf(" AND h.client_id IN (SELECT ct.client_id FROM client_teams ct WHERE ct.team_id = ANY($%d::uuid[]))", argCount)
		args = append(args, pq.Array(teamIDStrs))
	}

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get filtered job execution count: %w", err)
	}
	return count, nil
}

// GetStatusCounts returns counts of jobs grouped by status (excludes archived)
func (r *JobExecutionRepository) GetStatusCounts(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM job_executions
		WHERE archived_at IS NULL
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

// GetStatusCountsForUser returns counts of jobs grouped by status for a specific user (excludes archived)
func (r *JobExecutionRepository) GetStatusCountsForUser(ctx context.Context, userID string) (map[string]int, error) {
	query := `
		SELECT je.status, COUNT(*) as count
		FROM job_executions je
		WHERE je.created_by = $1 AND je.archived_at IS NULL
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

// GetStatusCountsFiltered returns counts of jobs grouped by status, respecting filters (excludes archived)
func (r *JobExecutionRepository) GetStatusCountsFiltered(ctx context.Context, filter JobFilter) (map[string]int, error) {
	query := `
		SELECT je.status, COUNT(*) as count
		FROM job_executions je
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		WHERE je.archived_at IS NULL`

	args := []interface{}{}
	argCount := 0

	// Apply user filter
	if filter.UserID != nil && *filter.UserID != "" {
		argCount++
		query += fmt.Sprintf(" AND je.created_by = $%d", argCount)
		args = append(args, *filter.UserID)
	}

	query, args, argCount = jobEntityFilters(query, args, argCount, filter)

	// Teams enabled but no teams — fail-closed: no results (mirror GetFilteredCount).
	if filter.TeamsEnabled && len(filter.TeamIDs) == 0 {
		return map[string]int{}, nil
	}

	// Apply team filter
	if len(filter.TeamIDs) > 0 {
		teamIDStrs := make([]string, len(filter.TeamIDs))
		for i, id := range filter.TeamIDs {
			teamIDStrs[i] = id.String()
		}
		argCount++
		query += fmt.Sprintf(" AND h.client_id IN (SELECT ct.client_id FROM client_teams ct WHERE ct.team_id = ANY($%d::uuid[]))", argCount)
		args = append(args, pq.Array(teamIDStrs))
	}

	query += " GROUP BY je.status"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get filtered status counts: %w", err)
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

// DeleteFinished deletes all finished (completed/failed/cancelled), non-archived job
// executions regardless of team. Used when Multi-Team Mode is off or the caller is an admin.
func (r *JobExecutionRepository) DeleteFinished(ctx context.Context) (int, error) {
	return r.deleteFinished(ctx, nil)
}

// DeleteFinishedForTeams deletes finished, non-archived job executions whose hashlist's client is
// assigned to at least one of teamIDs. An empty team list deletes nothing (fail closed), so a
// non-admin with no team membership cannot clear anyone's jobs (GH #100).
func (r *JobExecutionRepository) DeleteFinishedForTeams(ctx context.Context, teamIDs []uuid.UUID) (int, error) {
	if len(teamIDs) == 0 {
		return 0, nil
	}
	return r.deleteFinished(ctx, teamIDs)
}

// deleteFinished is the shared implementation. When teamIDs is nil the scope is global; otherwise
// the finished-job subquery is additionally restricted to hashlists reachable through client_teams.
func (r *JobExecutionRepository) deleteFinished(ctx context.Context, teamIDs []uuid.UUID) (int, error) {
	finishedSub := `
		SELECT je.id FROM job_executions je
		WHERE je.status IN ('completed', 'failed', 'cancelled') AND je.archived_at IS NULL`
	var args []interface{}
	if teamIDs != nil {
		ids := make([]string, len(teamIDs))
		for i, id := range teamIDs {
			ids[i] = id.String()
		}
		finishedSub += `
		  AND je.hashlist_id IN (
			SELECT h.id FROM hashlists h
			JOIN client_teams ct ON ct.client_id = h.client_id
			WHERE ct.team_id = ANY($1::uuid[])
		  )`
		args = append(args, pq.Array(ids))
	}

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
		WHERE interrupted_by IN (`+finishedSub+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to clear interrupted_by references: %w", err)
	}

	// Delete related performance metrics
	_, err = tx.ExecContext(ctx, `
		DELETE FROM job_performance_metrics
		WHERE job_execution_id IN (`+finishedSub+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete related performance metrics: %w", err)
	}

	// Delete related tasks
	_, err = tx.ExecContext(ctx, `
		DELETE FROM job_tasks
		WHERE job_execution_id IN (`+finishedSub+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete related job tasks: %w", err)
	}

	// Delete finished job executions
	result, err := tx.ExecContext(ctx, `
		DELETE FROM job_executions
		WHERE id IN (`+finishedSub+`)`, args...)
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
			je.processed_keyspace, je.attack_mode, je.created_by,
			je.created_at, je.started_at, je.completed_at, je.error_message, je.interrupted_by, je.updated_at,
			je.base_keyspace, je.effective_keyspace, je.multiplication_factor,
			je.overall_progress_percent, je.dispatched_keyspace,
			je.name, je.archived_at,
			u.username as created_by_username,
			pj.name as preset_job_name,
			h.client_id, c.name as client_name,
			je.workflow_id, jw.name as workflow_name
		FROM job_executions je
		LEFT JOIN preset_jobs pj ON je.preset_job_id = pj.id
		JOIN hashlists h ON je.hashlist_id = h.id
		LEFT JOIN clients c ON c.id = h.client_id
		LEFT JOIN job_workflows jw ON jw.id = je.workflow_id
		LEFT JOIN users u ON je.created_by = u.id
		WHERE 1=1`

	args := []interface{}{}
	argCount := 0

	// Apply archive filter
	if !filter.IncludeArchived {
		query += " AND je.archived_at IS NULL"
	}

	// Apply status filter (multi-status takes precedence)
	if len(filter.Statuses) > 0 {
		argCount++
		query += fmt.Sprintf(" AND je.status = ANY($%d::text[])", argCount)
		args = append(args, pq.Array(filter.Statuses))
	} else if filter.Status != nil && *filter.Status != "" {
		argCount++
		query += fmt.Sprintf(" AND je.status = $%d", argCount)
		args = append(args, *filter.Status)
	}
	query, args, argCount = jobEntityFilters(query, args, argCount, filter)

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

	// Apply team filter - filter by team access via hashlist → client → client_teams
	if filter.TeamsEnabled && len(filter.TeamIDs) == 0 {
		// Teams enabled but no teams — fail-closed: no results
		return nil, nil
	}
	if len(filter.TeamIDs) > 0 {
		teamIDStrs := make([]string, len(filter.TeamIDs))
		for i, id := range filter.TeamIDs {
			teamIDStrs[i] = id.String()
		}
		argCount++
		query += fmt.Sprintf(" AND h.client_id IN (SELECT ct.client_id FROM client_teams ct WHERE ct.team_id = ANY($%d::uuid[]))", argCount)
		args = append(args, pq.Array(teamIDStrs))
	}

	// Add ordering
	query += ` ORDER BY
		-- Active jobs first (pending, running, paused)
		CASE
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN 0
			ELSE 1
		END,
		-- Within active jobs: by priority DESC, created_at ASC (FIFO: oldest first, matching scheduler run order)
		CASE
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN je.priority
			ELSE NULL
		END DESC,
		CASE
			WHEN je.status IN ('preparing', 'pending', 'running', 'paused') THEN je.created_at
			ELSE NULL
		END ASC,
		-- Within completed jobs: by completed_at DESC (most recent first)
		-- Use COALESCE to handle NULL completed_at (fallback to updated_at, then created_at)
		CASE
			WHEN je.status NOT IN ('preparing', 'pending', 'running', 'paused')
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
		var clientID sql.Null[uuid.UUID]
		var workflowID sql.Null[uuid.UUID]
		err := rows.Scan(
			&exec.ID, &exec.PresetJobID, &exec.HashlistID, &exec.Status, &exec.Priority, &exec.MaxAgents,
			&exec.ProcessedKeyspace, &exec.AttackMode, &exec.CreatedBy,
			&exec.CreatedAt, &exec.StartedAt, &exec.CompletedAt, &exec.ErrorMessage, &exec.InterruptedBy, &exec.UpdatedAt,
			&exec.BaseKeyspace, &exec.EffectiveKeyspace, &exec.MultiplicationFactor,
			&exec.OverallProgressPercent, &exec.DispatchedKeyspace,
			&exec.Name, &exec.ArchivedAt,
			&exec.CreatedByUsername,
			&exec.PresetJobName,
			&clientID, &exec.ClientName,
			&workflowID, &exec.WorkflowName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution with user: %w", err)
		}
		if clientID.Valid {
			v := clientID.V
			exec.ClientID = &v
		}
		if workflowID.Valid {
			v := workflowID.V
			exec.WorkflowID = &v
		}
		executions = append(executions, exec)
	}

	return executions, nil
}

// ArchiveJob sets the archived_at timestamp for a job execution
func (r *JobExecutionRepository) ArchiveJob(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE job_executions SET archived_at = NOW(), updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND archived_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to archive job execution: %w", err)
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

// UnarchiveJob clears the archived_at timestamp for a job execution
func (r *JobExecutionRepository) UnarchiveJob(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE job_executions SET archived_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND archived_at IS NOT NULL`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to unarchive job execution: %w", err)
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

// HasActiveTasks checks if a job execution has any active (pending/running/dispatched) tasks
func (r *JobExecutionRepository) HasActiveTasks(ctx context.Context, jobID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM job_tasks WHERE job_execution_id = $1 AND status IN ('pending', 'running', 'dispatched'))`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, jobID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check active tasks: %w", err)
	}
	return exists, nil
}
