package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// AdminJobsHandler holds handlers for admin job-related operations.
type AdminJobsHandler struct {
	presetJobService services.AdminPresetJobService
	workflowService  services.AdminJobWorkflowService
}

// NewAdminJobsHandler creates a new handler for admin job routes.
func NewAdminJobsHandler(presetJobService services.AdminPresetJobService, workflowService services.AdminJobWorkflowService) *AdminJobsHandler {
	return &AdminJobsHandler{
		presetJobService: presetJobService,
		workflowService:  workflowService,
	}
}

// --- Preset Job Handlers ---

func (h *AdminJobsHandler) CreatePresetJob(w http.ResponseWriter, r *http.Request) {
	var job models.PresetJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	createdJob, err := h.presetJobService.CreatePresetJob(r.Context(), job)
	if err != nil {
		// Basic error handling, could check for specific validation errors
		debug.Error("Error creating preset job: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create preset job: %v", err))
		return
	}

	httputil.RespondWithJSON(w, http.StatusCreated, createdJob)
}

func (h *AdminJobsHandler) ListPresetJobs(w http.ResponseWriter, r *http.Request) {
	// TODO: Add pagination/sorting based on query params
	jobs, err := h.presetJobService.ListPresetJobs(r.Context())
	if err != nil {
		debug.Error("Error listing preset jobs: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list preset jobs")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, jobs)
}

func (h *AdminJobsHandler) GetPresetJobFormData(w http.ResponseWriter, r *http.Request) {
	formData, err := h.presetJobService.GetPresetJobFormData(r.Context())
	if err != nil {
		debug.Error("Error getting preset job form data: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get form data")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, formData)
}

func (h *AdminJobsHandler) GetPresetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["preset_job_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing preset job ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid preset job ID format")
		return
	}

	job, err := h.presetJobService.GetPresetJobByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Preset job not found")
		} else {
			debug.Error("Error getting preset job %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get preset job")
		}
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, job)
}

func (h *AdminJobsHandler) UpdatePresetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["preset_job_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing preset job ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid preset job ID format")
		return
	}

	var job models.PresetJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	updatedJob, err := h.presetJobService.UpdatePresetJob(r.Context(), id, job)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Preset job not found")
		} else {
			debug.Error("Error updating preset job %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update preset job: %v", err))
		}
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, updatedJob)
}

func (h *AdminJobsHandler) DeletePresetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["preset_job_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing preset job ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid preset job ID format")
		return
	}

	err = h.presetJobService.DeletePresetJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Preset job not found")
		} else {
			debug.Error("Error deleting preset job %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete preset job")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminJobsHandler) RecalculatePresetJobKeyspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["preset_job_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing preset job ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid preset job ID format")
		return
	}

	debug.Info("Received request to recalculate keyspace for preset job %s", id)

	// Get the preset job
	job, err := h.presetJobService.GetPresetJobByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Preset job not found")
		} else {
			debug.Error("Error getting preset job %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get preset job")
		}
		return
	}

	// Calculate keyspace
	keyspace, err := h.presetJobService.CalculateKeyspaceForPresetJob(r.Context(), job)
	if err != nil {
		debug.Error("Error calculating keyspace for preset job %s: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to calculate keyspace: %v", err))
		return
	}

	// Update the job with the new keyspace
	job.Keyspace = keyspace

	// Log the keyspace we're about to update
	debug.Info("Updating preset job %s with calculated keyspace: %v", id, keyspace)

	updatedJob, err := h.presetJobService.UpdatePresetJob(r.Context(), id, *job)
	if err != nil {
		debug.Error("Error updating preset job %s with keyspace: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update preset job with keyspace")
		return
	}

	// Verify the update
	if updatedJob.Keyspace == nil || (keyspace != nil && *updatedJob.Keyspace != *keyspace) {
		debug.Error("Keyspace was not properly updated for preset job %s. Expected: %v, Got: %v", id, keyspace, updatedJob.Keyspace)
	}

	httputil.RespondWithJSON(w, http.StatusOK, updatedJob)
}

func (h *AdminJobsHandler) RecalculateAllMissingKeyspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all preset jobs
	jobs, err := h.presetJobService.ListPresetJobs(ctx)
	if err != nil {
		debug.Error("Error listing preset jobs: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list preset jobs")
		return
	}

	var updated, failed, skipped int
	var errors []string

	debug.Info("Starting batch keyspace recalculation for %d preset jobs", len(jobs))

	for _, job := range jobs {
		// Skip if keyspace is already calculated
		if job.Keyspace != nil && *job.Keyspace > 0 {
			skipped++
			debug.Info("Skipping preset job %s - already has keyspace: %d", job.ID, *job.Keyspace)
			continue
		}

		debug.Info("Processing preset job %s - calculating keyspace", job.ID)

		// Calculate keyspace
		keyspace, err := h.presetJobService.CalculateKeyspaceForPresetJob(ctx, &job)
		if err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("%s: %v", job.ID, err))
			debug.Warning("Failed to calculate keyspace for preset job %s: %v", job.ID, err)
			continue
		}

		// Update the job with the new keyspace
		job.Keyspace = keyspace
		updatedJob, err := h.presetJobService.UpdatePresetJob(ctx, job.ID, job)
		if err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("%s: failed to update: %v", job.Name, err))
			debug.Warning("Failed to update preset job %s with keyspace: %v", job.ID, err)
			continue
		}

		updated++
		debug.Info("Successfully updated preset job %s with keyspace %d", job.ID, *keyspace)

		// Log the updated job details to ensure it was saved
		if updatedJob != nil && updatedJob.Keyspace != nil {
			debug.Info("Verified preset job %s now has keyspace %d in database", updatedJob.ID, *updatedJob.Keyspace)
		}
	}

	response := map[string]interface{}{
		"total":   len(jobs),
		"updated": updated,
		"failed":  failed,
		"skipped": skipped,
		"errors":  errors,
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// --- Job Workflow Handlers ---

// Define request/response structs specific to handlers if needed
type CreateWorkflowRequest struct {
	Name         string      `json:"name"`
	PresetJobIDs []uuid.UUID `json:"preset_job_ids"`
}

type UpdateWorkflowRequest struct {
	Name         string      `json:"name"`
	PresetJobIDs []uuid.UUID `json:"preset_job_ids"`
}

func (h *AdminJobsHandler) CreateJobWorkflow(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	createdWorkflow, err := h.workflowService.CreateJobWorkflow(r.Context(), req.Name, req.PresetJobIDs)
	if err != nil {
		debug.Error("Error creating job workflow: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create job workflow: %v", err))
		return
	}

	httputil.RespondWithJSON(w, http.StatusCreated, createdWorkflow)
}

func (h *AdminJobsHandler) ListJobWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workflows, err := h.workflowService.ListJobWorkflows(ctx)
	if err != nil {
		debug.Error("Error listing job workflows: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list job workflows")
		return
	}

	// Enhance workflows with has_high_priority_override flag
	enhancedWorkflows := make([]map[string]interface{}, 0, len(workflows))
	for _, workflow := range workflows {
		// Get workflow steps to check for high priority override
		steps, err := h.workflowService.GetJobWorkflowByID(ctx, workflow.ID)
		hasHighPriorityOverride := false
		
		if err == nil && steps != nil && steps.Steps != nil {
			// Check each step's preset job for high priority override
			for _, step := range steps.Steps {
				// The step should have preset job details populated
				// We need to check if the preset job has allow_high_priority_override
				// Since we don't have direct access to preset job details here,
				// we'll need to get them from the preset job service
				presetJob, err := h.presetJobService.GetPresetJobByID(ctx, step.PresetJobID)
				if err == nil && presetJob.AllowHighPriorityOverride {
					hasHighPriorityOverride = true
					break
				}
			}
		}
		
		enhancedWorkflow := map[string]interface{}{
			"id":                        workflow.ID,
			"name":                      workflow.Name,
			"created_at":                workflow.CreatedAt,
			"updated_at":                workflow.UpdatedAt,
			"has_high_priority_override": hasHighPriorityOverride,
		}
		
		// Include steps if they exist
		if workflow.Steps != nil {
			enhancedWorkflow["steps"] = workflow.Steps
		}
		
		enhancedWorkflows = append(enhancedWorkflows, enhancedWorkflow)
	}

	httputil.RespondWithJSON(w, http.StatusOK, enhancedWorkflows)
}

func (h *AdminJobsHandler) GetJobWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["job_workflow_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing job workflow ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid job workflow ID format")
		return
	}

	workflow, err := h.workflowService.GetJobWorkflowByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Job workflow not found")
		} else {
			debug.Error("Error getting job workflow %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get job workflow")
		}
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, workflow)
}

func (h *AdminJobsHandler) UpdateJobWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["job_workflow_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing job workflow ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid job workflow ID format")
		return
	}

	var req UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	updatedWorkflow, err := h.workflowService.UpdateJobWorkflow(r.Context(), id, req.Name, req.PresetJobIDs)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Job workflow not found")
		} else {
			debug.Error("Error updating job workflow %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update job workflow: %v", err))
		}
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, updatedWorkflow)
}

func (h *AdminJobsHandler) DeleteJobWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["job_workflow_id"]
	if !ok {
		httputil.RespondWithError(w, http.StatusBadRequest, "Missing job workflow ID")
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid job workflow ID format")
		return
	}

	err = h.workflowService.DeleteJobWorkflow(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Job workflow not found")
		} else {
			debug.Error("Error deleting job workflow %s: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete job workflow")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminJobsHandler) GetJobWorkflowFormData(w http.ResponseWriter, r *http.Request) {
	presetJobs, err := h.workflowService.GetJobWorkflowFormData(r.Context())
	if err != nil {
		debug.Error("Error getting job workflow form data: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get workflow form data")
		return
	}

	// Create a response structure with the preset jobs for selection
	response := struct {
		PresetJobs []models.PresetJobBasic `json:"preset_jobs"`
	}{
		PresetJobs: presetJobs,
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}
