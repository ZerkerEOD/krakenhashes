package repository

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// HasActiveJobsUsingWordlist checks if there are any active jobs using the specified wordlist
func (r *JobExecutionRepository) HasActiveJobsUsingWordlist(ctx context.Context, wordlistID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM job_executions
			WHERE status NOT IN ('completed', 'cancelled', 'failed')
			AND wordlist_ids ? $1
		)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, wordlistID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check active jobs using wordlist: %w", err)
	}

	return exists, nil
}

// HasActiveJobsUsingRule checks if there are any active jobs using the specified rule
func (r *JobExecutionRepository) HasActiveJobsUsingRule(ctx context.Context, ruleID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM job_executions
			WHERE status NOT IN ('completed', 'cancelled', 'failed')
			AND rule_ids ? $1
		)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, ruleID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check active jobs using rule: %w", err)
	}

	return exists, nil
}

// GetNonCompletedJobsUsingWordlist retrieves all non-completed jobs using the specified wordlist
func (r *JobExecutionRepository) GetNonCompletedJobsUsingWordlist(ctx context.Context, wordlistID string) ([]models.DeletionImpactJob, error) {
	query := `
		SELECT je.id, je.name, je.status, COALESCE(h.name, '') as hashlist_name
		FROM job_executions je
		LEFT JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.status IN ('pending', 'running', 'failed')
		AND je.wordlist_ids ? $1`

	rows, err := r.db.QueryContext(ctx, query, wordlistID)
	if err != nil {
		debug.Error("Error getting non-completed jobs using wordlist %s: %v", wordlistID, err)
		return nil, fmt.Errorf("failed to get jobs using wordlist: %w", err)
	}
	defer rows.Close()

	jobs := []models.DeletionImpactJob{}
	for rows.Next() {
		var job models.DeletionImpactJob
		if err := rows.Scan(&job.ID, &job.Name, &job.Status, &job.HashlistName); err != nil {
			debug.Error("Error scanning job row: %v", err)
			return nil, fmt.Errorf("failed to scan job row: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating job rows: %v", err)
		return nil, fmt.Errorf("failed to iterate job rows: %w", err)
	}

	return jobs, nil
}

// GetNonCompletedJobsUsingRule retrieves all non-completed jobs using the specified rule
func (r *JobExecutionRepository) GetNonCompletedJobsUsingRule(ctx context.Context, ruleID string) ([]models.DeletionImpactJob, error) {
	query := `
		SELECT je.id, je.name, je.status, COALESCE(h.name, '') as hashlist_name
		FROM job_executions je
		LEFT JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.status IN ('pending', 'running', 'failed')
		AND je.rule_ids ? $1`

	rows, err := r.db.QueryContext(ctx, query, ruleID)
	if err != nil {
		debug.Error("Error getting non-completed jobs using rule %s: %v", ruleID, err)
		return nil, fmt.Errorf("failed to get jobs using rule: %w", err)
	}
	defer rows.Close()

	jobs := []models.DeletionImpactJob{}
	for rows.Next() {
		var job models.DeletionImpactJob
		if err := rows.Scan(&job.ID, &job.Name, &job.Status, &job.HashlistName); err != nil {
			debug.Error("Error scanning job row: %v", err)
			return nil, fmt.Errorf("failed to scan job row: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating job rows: %v", err)
		return nil, fmt.Errorf("failed to iterate job rows: %w", err)
	}

	return jobs, nil
}

// DeleteJobsByIDs deletes job executions by their IDs (also deletes associated job_tasks via CASCADE)
func (r *JobExecutionRepository) DeleteJobsByIDs(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	query := `DELETE FROM job_executions WHERE id = ANY($1::uuid[])`
	result, err := r.db.ExecContext(ctx, query, pq.Array(ids))
	if err != nil {
		debug.Error("Error deleting job executions by IDs: %v", err)
		return fmt.Errorf("failed to delete job executions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting job executions: %v", err)
	} else {
		debug.Info("Deleted %d job executions", rowsAffected)
	}

	return nil
}