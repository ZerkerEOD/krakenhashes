package repository

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// GetJobsByRuleID retrieves all job executions that use a specific rule
func (r *JobExecutionRepository) GetJobsByRuleID(ctx context.Context, ruleID int) ([]models.JobExecution, error) {
	query := `
		SELECT
			id, preset_job_id, hashlist_id, status, priority,
			COALESCE(max_agents, 0) as max_agents,
			processed_keyspace, attack_mode, created_by,
			created_at, started_at, completed_at, error_message,
			interrupted_by, updated_at, name,
			base_keyspace, effective_keyspace, multiplication_factor,
			uses_rule_splitting, rule_split_count, overall_progress_percent,
			dispatched_keyspace, chunk_size_seconds, rule_ids,
			wordlist_ids
		FROM job_executions
		WHERE rule_ids @> to_jsonb(ARRAY[$1::text]) AND status IN ('pending', 'running', 'paused')`

	rows, err := r.db.QueryContext(ctx, query, ruleID)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs by rule ID: %w", err)
	}
	defer rows.Close()

	var jobs []models.JobExecution
	for rows.Next() {
		var job models.JobExecution
		err := rows.Scan(
			&job.ID, &job.PresetJobID, &job.HashlistID, &job.Status, &job.Priority,
			&job.MaxAgents, &job.ProcessedKeyspace, &job.AttackMode,
			&job.CreatedBy, &job.CreatedAt, &job.StartedAt, &job.CompletedAt,
			&job.ErrorMessage, &job.InterruptedBy, &job.UpdatedAt, &job.Name,
			&job.BaseKeyspace, &job.EffectiveKeyspace,
			&job.MultiplicationFactor, &job.UsesRuleSplitting, &job.RuleSplitCount,
			&job.OverallProgressPercent, &job.DispatchedKeyspace, &job.ChunkSizeSeconds,
			&job.RuleIDs, &job.WordlistIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution: %w", err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetJobsByWordlistID retrieves all job executions that use a specific wordlist
func (r *JobExecutionRepository) GetJobsByWordlistID(ctx context.Context, wordlistID int) ([]models.JobExecution, error) {
	query := `
		SELECT
			id, preset_job_id, hashlist_id, status, priority,
			COALESCE(max_agents, 0) as max_agents,
			processed_keyspace, attack_mode, created_by,
			created_at, started_at, completed_at, error_message,
			interrupted_by, updated_at, name,
			base_keyspace, effective_keyspace, multiplication_factor,
			uses_rule_splitting, rule_split_count, overall_progress_percent,
			dispatched_keyspace, chunk_size_seconds, rule_ids,
			wordlist_ids
		FROM job_executions
		WHERE wordlist_ids @> to_jsonb(ARRAY[$1::text]) AND status IN ('pending', 'running', 'paused')`

	rows, err := r.db.QueryContext(ctx, query, wordlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs by wordlist ID: %w", err)
	}
	defer rows.Close()

	var jobs []models.JobExecution
	for rows.Next() {
		var job models.JobExecution
		err := rows.Scan(
			&job.ID, &job.PresetJobID, &job.HashlistID, &job.Status, &job.Priority,
			&job.MaxAgents, &job.ProcessedKeyspace, &job.AttackMode,
			&job.CreatedBy, &job.CreatedAt, &job.StartedAt, &job.CompletedAt,
			&job.ErrorMessage, &job.InterruptedBy, &job.UpdatedAt, &job.Name,
			&job.BaseKeyspace, &job.EffectiveKeyspace,
			&job.MultiplicationFactor, &job.UsesRuleSplitting, &job.RuleSplitCount,
			&job.OverallProgressPercent, &job.DispatchedKeyspace, &job.ChunkSizeSeconds,
			&job.RuleIDs, &job.WordlistIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job execution: %w", err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// UpdateBaseKeyspace updates the base keyspace for a job execution
func (r *JobExecutionRepository) UpdateBaseKeyspace(ctx context.Context, jobID uuid.UUID, baseKeyspace int64) error {
	query := `
		UPDATE job_executions
		SET base_keyspace = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`

	result, err := r.db.ExecContext(ctx, query, baseKeyspace, jobID)
	if err != nil {
		return fmt.Errorf("failed to update base keyspace: %w", err)
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

// UpdateEffectiveKeyspace updates the effective keyspace for a job execution
func (r *JobExecutionRepository) UpdateEffectiveKeyspace(ctx context.Context, jobID uuid.UUID, effectiveKeyspace int64) error {
	query := `
		UPDATE job_executions
		SET effective_keyspace = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`

	result, err := r.db.ExecContext(ctx, query, effectiveKeyspace, jobID)
	if err != nil {
		return fmt.Errorf("failed to update effective keyspace: %w", err)
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

// UpdateMultiplicationFactor updates the multiplication factor for a job execution
func (r *JobExecutionRepository) UpdateMultiplicationFactor(ctx context.Context, jobID uuid.UUID, multiplicationFactor int) error {
	query := `
		UPDATE job_executions
		SET multiplication_factor = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`

	result, err := r.db.ExecContext(ctx, query, multiplicationFactor, jobID)
	if err != nil {
		return fmt.Errorf("failed to update multiplication factor: %w", err)
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

// UpdateKeyspaceMetrics updates multiple keyspace-related metrics for a job execution
func (r *JobExecutionRepository) UpdateKeyspaceMetrics(ctx context.Context, jobID uuid.UUID, updates map[string]interface{}) error {
	// Build dynamic query based on provided updates
	setClause := ""
	args := []interface{}{}
	argCount := 0

	for field, value := range updates {
		if argCount > 0 {
			setClause += ", "
		}
		argCount++
		setClause += fmt.Sprintf("%s = $%d", field, argCount)
		args = append(args, value)
	}

	// Add job ID as last argument
	argCount++
	args = append(args, jobID)

	query := fmt.Sprintf(`
		UPDATE job_executions
		SET %s, updated_at = CURRENT_TIMESTAMP
		WHERE id = $%d`, setClause, argCount)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update keyspace metrics: %w", err)
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