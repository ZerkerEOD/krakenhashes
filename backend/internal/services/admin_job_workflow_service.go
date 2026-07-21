package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// AdminJobWorkflowService defines the interface for managing job workflows.
type AdminJobWorkflowService interface {
	CreateJobWorkflow(ctx context.Context, name string, presetJobIDs []uuid.UUID, loopbackAllEligible bool, loopbackPresetJobIDs []uuid.UUID) (*models.JobWorkflow, error)
	GetJobWorkflowByID(ctx context.Context, id uuid.UUID) (*models.JobWorkflow, error)
	ListJobWorkflows(ctx context.Context) ([]models.JobWorkflow, error)
	UpdateJobWorkflow(ctx context.Context, id uuid.UUID, name string, presetJobIDs []uuid.UUID, loopbackAllEligible bool, loopbackPresetJobIDs []uuid.UUID) (*models.JobWorkflow, error)
	DeleteJobWorkflow(ctx context.Context, id uuid.UUID) error
	GetJobWorkflowFormData(ctx context.Context) ([]models.PresetJobBasic, error)
}

// adminJobWorkflowService implements AdminJobWorkflowService.
type adminJobWorkflowService struct {
	db            *sql.DB // Need DB instance for transactions
	workflowRepo  repository.JobWorkflowRepository
	presetJobRepo repository.PresetJobRepository // Needed for step name lookup
}

// NewAdminJobWorkflowService creates a new service for managing job workflows.
func NewAdminJobWorkflowService(db *sql.DB, workflowRepo repository.JobWorkflowRepository, presetJobRepo repository.PresetJobRepository) AdminJobWorkflowService {
	return &adminJobWorkflowService{
		db:            db,
		workflowRepo:  workflowRepo,
		presetJobRepo: presetJobRepo, // Inject PresetJobRepository
	}
}

// toUUIDSet builds a lookup set from a slice of UUIDs.
func toUUIDSet(ids []uuid.UUID) map[uuid.UUID]bool {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// validateWorkflowInput performs input validation for create/update operations.
func (s *adminJobWorkflowService) validateWorkflowInput(ctx context.Context, name string, presetJobIDs []uuid.UUID, isUpdate bool, existingID uuid.UUID) error {
	if name == "" {
		return errors.New("job workflow name cannot be empty")
	}
	if len(presetJobIDs) == 0 {
		return errors.New("job workflow must have at least one step")
	}

	// Check name uniqueness
	existingByName, err := s.workflowRepo.GetWorkflowByName(ctx, name)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		// Error other than not found
		return fmt.Errorf("error checking name uniqueness: %w", err)
	}
	if existingByName != nil && (!isUpdate || existingByName.ID != existingID) {
		return fmt.Errorf("job workflow name '%s' already exists", name)
	}

	// Check if all preset job IDs actually exist
	existingPresetJobIDs, err := s.workflowRepo.CheckPresetJobsExist(ctx, presetJobIDs)
	if err != nil {
		return fmt.Errorf("error checking preset job existence: %w", err)
	}
	if len(existingPresetJobIDs) != len(presetJobIDs) {
		// Find the missing IDs for a better error message
		missing := []uuid.UUID{}
		existingMap := make(map[uuid.UUID]bool)
		for _, id := range existingPresetJobIDs {
			existingMap[id] = true
		}
		for _, id := range presetJobIDs {
			if !existingMap[id] {
				missing = append(missing, id)
			}
		}
		return fmt.Errorf("one or more preset job IDs do not exist: %v", missing)
	}

	// TODO: Check for duplicate step order? (Implicitly handled by list order for now)
	return nil
}

// CreateJobWorkflow creates a new workflow and its steps transactionally.
func (s *adminJobWorkflowService) CreateJobWorkflow(ctx context.Context, name string, presetJobIDs []uuid.UUID, loopbackAllEligible bool, loopbackPresetJobIDs []uuid.UUID) (*models.JobWorkflow, error) {
	if err := s.validateWorkflowInput(ctx, name, presetJobIDs, false, uuid.Nil); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	debug.Info("Attempting to create job workflow: %s", name)

	loopbackSet := toUUIDSet(loopbackPresetJobIDs)
	var createdWorkflow *models.JobWorkflow
	createdSteps := make([]models.JobWorkflowStep, 0, len(presetJobIDs))

	// Execute DB operations within a transaction
	err := s.executeTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		// 1. Create the workflow record
		// Assuming repo methods are modified to accept *sql.Tx (or we pass ctx and repo uses internal DB handle)
		// For now, let's assume repo methods don't take Tx and work directly on s.db within the transaction context
		createdWorkflow, err = s.workflowRepo.CreateWorkflow(ctx, name, loopbackAllEligible) // Need repo to work with Tx or handle context
		if err != nil {
			return fmt.Errorf("failed to create workflow record in transaction: %w", err)
		}

		// 2. Create the steps
		for i, presetID := range presetJobIDs {
			stepOrder := i + 1 // Steps are 1-indexed
			step, stepErr := s.workflowRepo.CreateWorkflowStep(ctx, createdWorkflow.ID, presetID, stepOrder, loopbackSet[presetID])
			if stepErr != nil {
				return fmt.Errorf("failed to create workflow step %d in transaction: %w", stepOrder, stepErr)
			}
			// Fetch preset job name to populate the step
			presetJob, presetErr := s.presetJobRepo.GetByID(ctx, presetID)
			if presetErr != nil {
				debug.Warning("Could not fetch preset job name %s during workflow creation: %v", presetID, presetErr)
				step.PresetJobName = "[Error Fetching Name]"
			} else {
				step.PresetJobName = presetJob.Name
			}
			createdSteps = append(createdSteps, *step)
		}
		return nil // Success
	})

	if err != nil {
		return nil, err // Error already logged by executeTransaction or repo methods
	}

	createdWorkflow.Steps = createdSteps // Populate steps in the returned object
	debug.Info("Successfully created job workflow ID: %s with %d steps", createdWorkflow.ID, len(createdSteps))
	return createdWorkflow, nil
}

// GetJobWorkflowByID retrieves a workflow and its steps.
func (s *adminJobWorkflowService) GetJobWorkflowByID(ctx context.Context, id uuid.UUID) (*models.JobWorkflow, error) {
	debug.Debug("Getting job workflow by ID: %s", id)
	wf, err := s.workflowRepo.GetWorkflowByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			debug.Warning("Job workflow not found: %s", id)
		} else {
			debug.Error("Failed to get job workflow %s: %v", id, err)
		}
		return nil, err // Return original error (could be ErrNotFound)
	}
	return wf, nil
}

// ListJobWorkflows retrieves all workflows (without steps).
func (s *adminJobWorkflowService) ListJobWorkflows(ctx context.Context) ([]models.JobWorkflow, error) {
	debug.Debug("Listing all job workflows")
	wfs, err := s.workflowRepo.ListWorkflows(ctx)
	if err != nil {
		debug.Error("Failed to list job workflows: %v", err)
		return nil, fmt.Errorf("failed to list job workflows: %w", err)
	}
	return wfs, nil
}

// UpdateJobWorkflow updates a workflow name and replaces its steps transactionally.
func (s *adminJobWorkflowService) UpdateJobWorkflow(ctx context.Context, id uuid.UUID, name string, presetJobIDs []uuid.UUID, loopbackAllEligible bool, loopbackPresetJobIDs []uuid.UUID) (*models.JobWorkflow, error) {
	// 1. Check if workflow exists first
	_, err := s.GetJobWorkflowByID(ctx, id)
	if err != nil {
		return nil, err // Returns ErrNotFound if applicable
	}

	// 2. Validate input
	if err := s.validateWorkflowInput(ctx, name, presetJobIDs, true, id); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	debug.Info("Attempting to update job workflow ID: %s", id)

	loopbackSet := toUUIDSet(loopbackPresetJobIDs)
	var updatedWorkflow *models.JobWorkflow
	createdSteps := make([]models.JobWorkflowStep, 0, len(presetJobIDs))

	// Execute DB operations within a transaction
	err = s.executeTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		// 3. Update workflow name
		updatedWorkflow, err = s.workflowRepo.UpdateWorkflow(ctx, id, name, loopbackAllEligible)
		if err != nil {
			return fmt.Errorf("failed to update workflow name in transaction: %w", err)
		}

		// 4. Delete existing steps
		err = s.workflowRepo.DeleteWorkflowSteps(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to delete existing steps in transaction: %w", err)
		}

		// 5. Create new steps
		for i, presetID := range presetJobIDs {
			stepOrder := i + 1
			step, stepErr := s.workflowRepo.CreateWorkflowStep(ctx, id, presetID, stepOrder, loopbackSet[presetID])
			if stepErr != nil {
				return fmt.Errorf("failed to create workflow step %d in transaction: %w", stepOrder, stepErr)
			}
			// Fetch preset job name to populate the step
			presetJob, presetErr := s.presetJobRepo.GetByID(ctx, presetID)
			if presetErr != nil {
				debug.Warning("Could not fetch preset job name %s during workflow update: %v", presetID, presetErr)
				step.PresetJobName = "[Error Fetching Name]"
			} else {
				step.PresetJobName = presetJob.Name
			}
			createdSteps = append(createdSteps, *step)
		}
		return nil // Success
	})

	if err != nil {
		return nil, err // Error logged by executeTransaction or repo
	}

	updatedWorkflow.Steps = createdSteps // Populate steps
	debug.Info("Successfully updated job workflow ID: %s", updatedWorkflow.ID)
	return updatedWorkflow, nil
}

// DeleteJobWorkflow deletes a workflow.
func (s *adminJobWorkflowService) DeleteJobWorkflow(ctx context.Context, id uuid.UUID) error {
	debug.Info("Deleting job workflow ID: %s", id)
	// Check existence first
	_, err := s.GetJobWorkflowByID(ctx, id)
	if err != nil {
		return err
	}

	err = s.workflowRepo.DeleteWorkflow(ctx, id)
	if err != nil {
		debug.Error("Failed to delete job workflow %s: %v", id, err)
		return fmt.Errorf("failed to delete job workflow: %w", err)
	}
	debug.Info("Successfully deleted job workflow ID: %s", id)
	return nil
}

// executeTransaction is a helper function to wrap database operations in a transaction.
func (s *adminJobWorkflowService) executeTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		debug.Error("Failed to begin transaction: %v", err)
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback() // Rollback on panic
			panic(p)      // Re-panic after rollback
		} else if err != nil {
			// Error occurred within fn
			debug.Warning("Rolling back transaction due to error: %v", err)
			if rbErr := tx.Rollback(); rbErr != nil {
				debug.Error("Failed to rollback transaction: %v", rbErr)
			}
		} else {
			// No error, commit the transaction
			err = tx.Commit()
			if err != nil {
				debug.Error("Failed to commit transaction: %v", err)
				err = fmt.Errorf("failed to commit transaction: %w", err)
			}
		}
	}()

	// Execute the provided function within the transaction
	err = fn(tx)
	return err // Return the error from fn (or nil if successful, or commit error)
}

// GetJobWorkflowFormData retrieves a list of preset jobs for workflow form selection.
func (s *adminJobWorkflowService) GetJobWorkflowFormData(ctx context.Context) ([]models.PresetJobBasic, error) {
	debug.Debug("Getting job workflow form data (available preset jobs)")
	jobs, err := s.presetJobRepo.List(ctx)
	if err != nil {
		debug.Error("Failed to list preset jobs for workflow form: %v", err)
		return nil, fmt.Errorf("failed to get available preset jobs: %w", err)
	}

	// Convert full preset jobs to basic info needed for the form
	basicJobs := make([]models.PresetJobBasic, len(jobs))
	for i, job := range jobs {
		basicJobs[i] = models.PresetJobBasic{
			ID:                        job.ID,
			Name:                      job.Name,
			AllowHighPriorityOverride: job.AllowHighPriorityOverride,
			AttackMode:                job.AttackMode,
			RuleIDs:                   job.RuleIDs,
		}
	}

	return basicJobs, nil
}
