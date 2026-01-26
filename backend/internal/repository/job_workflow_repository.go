package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// JobWorkflowRepository defines the interface for interacting with job workflows and steps.
type JobWorkflowRepository interface {
	CreateWorkflow(ctx context.Context, name string) (*models.JobWorkflow, error)
	GetWorkflowByID(ctx context.Context, id uuid.UUID) (*models.JobWorkflow, error)
	GetWorkflowByName(ctx context.Context, name string) (*models.JobWorkflow, error)
	ListWorkflows(ctx context.Context) ([]models.JobWorkflow, error)
	UpdateWorkflow(ctx context.Context, id uuid.UUID, name string) (*models.JobWorkflow, error)
	DeleteWorkflow(ctx context.Context, id uuid.UUID) error

	CreateWorkflowStep(ctx context.Context, workflowID, presetJobID uuid.UUID, stepOrder int) (*models.JobWorkflowStep, error)
	GetWorkflowSteps(ctx context.Context, workflowID uuid.UUID) ([]models.JobWorkflowStep, error)
	DeleteWorkflowSteps(ctx context.Context, workflowID uuid.UUID) error
	CheckPresetJobsExist(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)

	// Deletion impact methods
	GetStepsByPresetJobIDs(ctx context.Context, presetJobIDs []uuid.UUID) ([]models.DeletionImpactWorkflowStep, error)
	DeleteStepsByPresetJobIDs(ctx context.Context, presetJobIDs []uuid.UUID) error
	GetEmptyWorkflows(ctx context.Context) ([]models.JobWorkflow, error)
	DeleteEmptyWorkflows(ctx context.Context) (int64, error)
	GetWorkflowsAffectedByPresetJobDeletion(ctx context.Context, presetJobIDs []uuid.UUID) ([]models.DeletionImpactWorkflow, error)

	// Transactional methods (optional, can be handled in service layer)
	// ReplaceWorkflowStepsTx(ctx context.Context, tx *sql.Tx, workflowID uuid.UUID, steps []models.JobWorkflowStep) error
}

// jobWorkflowRepository implements JobWorkflowRepository.
type jobWorkflowRepository struct {
	db *sql.DB
}

// NewJobWorkflowRepository creates a new repository for job workflows.
func NewJobWorkflowRepository(db *sql.DB) JobWorkflowRepository {
	return &jobWorkflowRepository{db: db}
}

// CreateWorkflow inserts a new job workflow.
func (r *jobWorkflowRepository) CreateWorkflow(ctx context.Context, name string) (*models.JobWorkflow, error) {
	query := `INSERT INTO job_workflows (name) VALUES ($1) RETURNING id, name, created_at, updated_at`
	row := r.db.QueryRowContext(ctx, query, name)

	var wf models.JobWorkflow
	err := row.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt)
	if err != nil {
		// TODO: Handle potential unique constraint violation error (e.g., convert pq error)
		debug.Error("Error creating job workflow: %v", err)
		return nil, fmt.Errorf("error creating job workflow: %w", err)
	}
	return &wf, nil
}

// GetWorkflowByID retrieves a job workflow by ID, including its steps.
func (r *jobWorkflowRepository) GetWorkflowByID(ctx context.Context, id uuid.UUID) (*models.JobWorkflow, error) {
	query := `SELECT id, name, created_at, updated_at FROM job_workflows WHERE id = $1 LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, id)

	var wf models.JobWorkflow
	err := row.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job workflow not found: %w", ErrNotFound)
		}
		debug.Error("Error getting job workflow by ID %s: %v", id, err)
		return nil, fmt.Errorf("error getting job workflow by ID: %w", err)
	}

	// Fetch associated steps
	steps, err := r.GetWorkflowSteps(ctx, wf.ID)
	if err != nil {
		// Log error but return the workflow info we already have
		debug.Error("Error fetching steps for workflow %s: %v", id, err)
	} else {
		wf.Steps = steps
	}

	return &wf, nil
}

// GetWorkflowByName retrieves a job workflow by name.
func (r *jobWorkflowRepository) GetWorkflowByName(ctx context.Context, name string) (*models.JobWorkflow, error) {
	query := `SELECT id, name, created_at, updated_at FROM job_workflows WHERE name = $1 LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, name)

	var wf models.JobWorkflow
	err := row.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job workflow not found: %w", ErrNotFound)
		}
		debug.Error("Error getting job workflow by name %s: %v", name, err)
		return nil, fmt.Errorf("error getting job workflow by name: %w", err)
	}
	// Note: Steps are not fetched by default in GetByName, only basic workflow info.
	return &wf, nil
}

// ListWorkflows retrieves all job workflows.
func (r *jobWorkflowRepository) ListWorkflows(ctx context.Context) ([]models.JobWorkflow, error) {
	query := `
		SELECT w.id, w.name, w.created_at, w.updated_at, COUNT(s.id) as step_count
		FROM job_workflows w
		LEFT JOIN job_workflow_steps s ON w.id = s.job_workflow_id
		GROUP BY w.id, w.name, w.created_at, w.updated_at
		ORDER BY w.name
	` // TODO: Pagination
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		debug.Error("Error listing job workflows: %v", err)
		return nil, fmt.Errorf("error listing job workflows: %w", err)
	}
	defer rows.Close()

	workflows := []models.JobWorkflow{}
	for rows.Next() {
		var wf models.JobWorkflow
		var stepCount int
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt, &stepCount); err != nil {
			debug.Error("Error scanning job workflow row: %v", err)
			return nil, fmt.Errorf("error scanning job workflow row: %w", err)
		}

		// Create empty steps slice with the correct count
		wf.Steps = make([]models.JobWorkflowStep, stepCount)

		workflows = append(workflows, wf)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating job workflow rows: %v", err)
		return nil, fmt.Errorf("error iterating job workflow rows: %w", err)
	}

	return workflows, nil
}

// UpdateWorkflow updates a job workflow's name.
func (r *jobWorkflowRepository) UpdateWorkflow(ctx context.Context, id uuid.UUID, name string) (*models.JobWorkflow, error) {
	query := `UPDATE job_workflows SET name = $2, updated_at = NOW() WHERE id = $1 RETURNING id, name, created_at, updated_at`
	row := r.db.QueryRowContext(ctx, query, id, name)

	var wf models.JobWorkflow
	err := row.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job workflow not found for update: %w", ErrNotFound)
		}
		// TODO: Handle potential unique constraint violation error
		debug.Error("Error updating job workflow %s: %v", id, err)
		return nil, fmt.Errorf("error updating job workflow: %w", err)
	}
	return &wf, nil
}

// DeleteWorkflow removes a job workflow and its steps (due to ON DELETE CASCADE).
func (r *jobWorkflowRepository) DeleteWorkflow(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM job_workflows WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		debug.Error("Error deleting job workflow %s: %v", id, err)
		return fmt.Errorf("error deleting job workflow: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting job workflow %s: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("job workflow not found for deletion: %w", ErrNotFound)
	}
	return nil
}

// --- Workflow Step Methods ---

// CreateWorkflowStep inserts a single step for a workflow.
func (r *jobWorkflowRepository) CreateWorkflowStep(ctx context.Context, workflowID, presetJobID uuid.UUID, stepOrder int) (*models.JobWorkflowStep, error) {
	query := `
		INSERT INTO job_workflow_steps (job_workflow_id, preset_job_id, step_order)
		VALUES ($1, $2, $3)
		RETURNING id, job_workflow_id, preset_job_id, step_order`
	row := r.db.QueryRowContext(ctx, query, workflowID, presetJobID, stepOrder)

	var step models.JobWorkflowStep
	err := row.Scan(&step.ID, &step.JobWorkflowID, &step.PresetJobID, &step.StepOrder)
	if err != nil {
		// TODO: Handle potential unique constraint (workflowID, stepOrder) violation
		// TODO: Handle potential foreign key constraint violation (presetJobID)
		debug.Error("Error creating job workflow step: %v", err)
		return nil, fmt.Errorf("error creating job workflow step: %w", err)
	}
	return &step, nil
}

// GetWorkflowSteps retrieves all steps for a given workflow, ordered by priority, including preset job details.
func (r *jobWorkflowRepository) GetWorkflowSteps(ctx context.Context, workflowID uuid.UUID) ([]models.JobWorkflowStep, error) {
	query := `
		SELECT
			jws.id, jws.job_workflow_id, jws.preset_job_id, jws.step_order,
			pj.name as preset_job_name,
			pj.attack_mode as preset_job_attack_mode,
			pj.priority as preset_job_priority,
			pj.binary_version as preset_job_binary_version,
			pj.binary_version as preset_job_binary_name,
			pj.wordlist_ids as preset_job_wordlist_ids,
			pj.rule_ids as preset_job_rule_ids
		FROM job_workflow_steps jws
		JOIN preset_jobs pj ON jws.preset_job_id = pj.id
		WHERE jws.job_workflow_id = $1
		ORDER BY pj.priority DESC, jws.step_order`

	rows, err := r.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		debug.Error("Error getting steps for workflow %s: %v", workflowID, err)
		return nil, fmt.Errorf("error getting workflow steps: %w", err)
	}
	defer rows.Close()

	steps := []models.JobWorkflowStep{}
	for rows.Next() {
		var step models.JobWorkflowStep
		if err := rows.Scan(
			&step.ID, &step.JobWorkflowID, &step.PresetJobID, &step.StepOrder,
			&step.PresetJobName, &step.PresetJobAttackMode, &step.PresetJobPriority,
			&step.PresetJobBinaryVersion, &step.PresetJobBinaryName, &step.PresetJobWordlistIDs, &step.PresetJobRuleIDs,
		); err != nil {
			debug.Error("Error scanning job workflow step row: %v", err)
			return nil, fmt.Errorf("error scanning job workflow step row: %w", err)
		}
		steps = append(steps, step)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating job workflow step rows: %v", err)
		return nil, fmt.Errorf("error iterating job workflow step rows: %w", err)
	}

	return steps, nil
}

// DeleteWorkflowSteps removes all steps associated with a workflow ID.
// Often used within a transaction before inserting new steps.
func (r *jobWorkflowRepository) DeleteWorkflowSteps(ctx context.Context, workflowID uuid.UUID) error {
	query := `DELETE FROM job_workflow_steps WHERE job_workflow_id = $1`
	_, err := r.db.ExecContext(ctx, query, workflowID)
	if err != nil {
		debug.Error("Error deleting steps for workflow %s: %v", workflowID, err)
		return fmt.Errorf("error deleting workflow steps: %w", err)
	}
	// We don't check RowsAffected here, as deleting 0 rows isn't necessarily an error.
	return nil
}

// CheckPresetJobsExist checks if a list of preset job IDs exist in the database.
// Returns a list of IDs that *do* exist.
func (r *jobWorkflowRepository) CheckPresetJobsExist(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}
	query := `SELECT id FROM preset_jobs WHERE id = ANY($1::uuid[])`
	rows, err := r.db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		debug.Error("Error checking preset job existence: %v", err)
		return nil, fmt.Errorf("error checking preset job existence: %w", err)
	}
	defer rows.Close()

	existingIDs := make([]uuid.UUID, 0, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			debug.Error("Error scanning existing preset job ID: %v", err)
			return nil, fmt.Errorf("error scanning existing preset job ID: %w", err)
		}
		existingIDs = append(existingIDs, id)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating existing preset job IDs: %v", err)
		return nil, fmt.Errorf("error iterating existing preset job IDs: %w", err)
	}

	return existingIDs, nil
}

// GetStepsByPresetJobIDs retrieves all workflow steps that reference any of the given preset job IDs.
func (r *jobWorkflowRepository) GetStepsByPresetJobIDs(ctx context.Context, presetJobIDs []uuid.UUID) ([]models.DeletionImpactWorkflowStep, error) {
	if len(presetJobIDs) == 0 {
		return []models.DeletionImpactWorkflowStep{}, nil
	}

	query := `
		SELECT jws.job_workflow_id, jw.name, jws.step_order, jws.preset_job_id, pj.name
		FROM job_workflow_steps jws
		JOIN job_workflows jw ON jw.id = jws.job_workflow_id
		JOIN preset_jobs pj ON pj.id = jws.preset_job_id
		WHERE jws.preset_job_id = ANY($1::uuid[])`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(presetJobIDs))
	if err != nil {
		debug.Error("Error getting workflow steps by preset job IDs: %v", err)
		return nil, fmt.Errorf("error getting workflow steps by preset job IDs: %w", err)
	}
	defer rows.Close()

	steps := []models.DeletionImpactWorkflowStep{}
	for rows.Next() {
		var step models.DeletionImpactWorkflowStep
		if err := rows.Scan(&step.WorkflowID, &step.WorkflowName, &step.StepOrder, &step.PresetJobID, &step.PresetJobName); err != nil {
			debug.Error("Error scanning workflow step row: %v", err)
			return nil, fmt.Errorf("error scanning workflow step row: %w", err)
		}
		steps = append(steps, step)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating workflow step rows: %v", err)
		return nil, fmt.Errorf("error iterating workflow step rows: %w", err)
	}

	return steps, nil
}

// DeleteStepsByPresetJobIDs deletes all workflow steps that reference any of the given preset job IDs.
func (r *jobWorkflowRepository) DeleteStepsByPresetJobIDs(ctx context.Context, presetJobIDs []uuid.UUID) error {
	if len(presetJobIDs) == 0 {
		return nil
	}

	query := `DELETE FROM job_workflow_steps WHERE preset_job_id = ANY($1::uuid[])`
	result, err := r.db.ExecContext(ctx, query, pq.Array(presetJobIDs))
	if err != nil {
		debug.Error("Error deleting workflow steps by preset job IDs: %v", err)
		return fmt.Errorf("error deleting workflow steps: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting workflow steps: %v", err)
	} else {
		debug.Info("Deleted %d workflow steps", rowsAffected)
	}

	return nil
}

// GetEmptyWorkflows retrieves all workflows that have no steps.
func (r *jobWorkflowRepository) GetEmptyWorkflows(ctx context.Context) ([]models.JobWorkflow, error) {
	query := `
		SELECT jw.id, jw.name, jw.created_at, jw.updated_at
		FROM job_workflows jw
		WHERE NOT EXISTS (
			SELECT 1 FROM job_workflow_steps jws
			WHERE jws.job_workflow_id = jw.id
		)`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		debug.Error("Error getting empty workflows: %v", err)
		return nil, fmt.Errorf("error getting empty workflows: %w", err)
	}
	defer rows.Close()

	workflows := []models.JobWorkflow{}
	for rows.Next() {
		var wf models.JobWorkflow
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
			debug.Error("Error scanning empty workflow row: %v", err)
			return nil, fmt.Errorf("error scanning empty workflow row: %w", err)
		}
		workflows = append(workflows, wf)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating empty workflow rows: %v", err)
		return nil, fmt.Errorf("error iterating empty workflow rows: %w", err)
	}

	return workflows, nil
}

// DeleteEmptyWorkflows deletes all workflows that have no steps.
func (r *jobWorkflowRepository) DeleteEmptyWorkflows(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM job_workflows jw
		WHERE NOT EXISTS (
			SELECT 1 FROM job_workflow_steps jws
			WHERE jws.job_workflow_id = jw.id
		)`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		debug.Error("Error deleting empty workflows: %v", err)
		return 0, fmt.Errorf("error deleting empty workflows: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting empty workflows: %v", err)
		return 0, nil
	}

	debug.Info("Deleted %d empty workflows", rowsAffected)
	return rowsAffected, nil
}

// GetWorkflowsAffectedByPresetJobDeletion finds workflows that would become empty after deleting the given preset jobs.
func (r *jobWorkflowRepository) GetWorkflowsAffectedByPresetJobDeletion(ctx context.Context, presetJobIDs []uuid.UUID) ([]models.DeletionImpactWorkflow, error) {
	if len(presetJobIDs) == 0 {
		return []models.DeletionImpactWorkflow{}, nil
	}

	// Find workflows where ALL steps reference preset jobs being deleted
	// Also count the total steps in each workflow
	query := `
		SELECT jw.id, jw.name,
			(SELECT COUNT(*) FROM job_workflow_steps jws2 WHERE jws2.job_workflow_id = jw.id) as step_count
		FROM job_workflows jw
		WHERE EXISTS (
			SELECT 1 FROM job_workflow_steps jws
			WHERE jws.job_workflow_id = jw.id
			AND jws.preset_job_id = ANY($1::uuid[])
		)
		AND NOT EXISTS (
			SELECT 1 FROM job_workflow_steps jws
			WHERE jws.job_workflow_id = jw.id
			AND jws.preset_job_id != ALL($1::uuid[])
		)`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(presetJobIDs))
	if err != nil {
		debug.Error("Error getting workflows affected by preset job deletion: %v", err)
		return nil, fmt.Errorf("error getting affected workflows: %w", err)
	}
	defer rows.Close()

	workflows := []models.DeletionImpactWorkflow{}
	for rows.Next() {
		var wf models.DeletionImpactWorkflow
		if err := rows.Scan(&wf.ID, &wf.Name, &wf.StepCount); err != nil {
			debug.Error("Error scanning affected workflow row: %v", err)
			return nil, fmt.Errorf("error scanning affected workflow row: %w", err)
		}
		wf.Description = "all steps will be removed"
		workflows = append(workflows, wf)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating affected workflow rows: %v", err)
		return nil, fmt.Errorf("error iterating affected workflow rows: %w", err)
	}

	return workflows, nil
}
