package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// JobHandler handles User API v1 job operations
type JobHandler struct {
	jobExecService        *services.JobExecutionService
	jobExecRepo           *repository.JobExecutionRepository
	jobTaskRepo           *repository.JobTaskRepository
	hashlistRepo          *repository.HashListRepository
	clientRepo            *repository.ClientRepository
	presetJobRepo         repository.PresetJobRepository
	workflowRepo          repository.JobWorkflowRepository
	schedulingService     *services.JobSchedulingService
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	systemSettingsRepo    *repository.SystemSettingsRepository
}

// NewJobHandler creates a new job handler for User API v1
func NewJobHandler(
	jobExecService *services.JobExecutionService,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	hashlistRepo *repository.HashListRepository,
	clientRepo *repository.ClientRepository,
	presetJobRepo repository.PresetJobRepository,
	workflowRepo repository.JobWorkflowRepository,
	schedulingService *services.JobSchedulingService,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *JobHandler {
	return &JobHandler{
		jobExecService:        jobExecService,
		jobExecRepo:           jobExecRepo,
		jobTaskRepo:           jobTaskRepo,
		hashlistRepo:          hashlistRepo,
		clientRepo:            clientRepo,
		presetJobRepo:         presetJobRepo,
		workflowRepo:          workflowRepo,
		schedulingService:     schedulingService,
		jobIncrementLayerRepo: jobIncrementLayerRepo,
		systemSettingsRepo:    systemSettingsRepo,
	}
}

// APIError represents a standardized API error response
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CreateJobRequest represents the request body for creating a job
type CreateJobRequest struct {
	Name         string     `json:"name"`
	HashlistID   int64      `json:"hashlist_id"`
	WorkflowID   *uuid.UUID `json:"workflow_id,omitempty"`
	PresetJobID  *uuid.UUID `json:"preset_job_id,omitempty"`
	Priority     int        `json:"priority"`
	MaxAgents    int        `json:"max_agents"`
}

// CreateJobResponse represents the response for job creation
type CreateJobResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

// JobStatusResponse represents a detailed job status for polling
type JobStatusResponse struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Status                 string     `json:"status"`
	Priority               int        `json:"priority"`
	MaxAgents              int        `json:"max_agents"`
	DispatchedPercent      float64    `json:"dispatched_percent"`
	SearchedPercent        float64    `json:"searched_percent"`
	CrackedCount           int        `json:"cracked_count"`
	AgentCount             int        `json:"agent_count"`
	TotalSpeed             int64      `json:"total_speed"`
	CreatedAt              time.Time  `json:"created_at"`
	StartedAt              *time.Time `json:"started_at,omitempty"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	ErrorMessage           *string    `json:"error_message,omitempty"`
	EffectiveKeyspace      *int64     `json:"effective_keyspace,omitempty"`
	ProcessedKeyspace      int64      `json:"processed_keyspace"`
	DispatchedKeyspace     int64      `json:"dispatched_keyspace"`
	OverallProgressPercent float64    `json:"overall_progress_percent"`
	// Increment mode fields
	IncrementMode string `json:"increment_mode,omitempty"`
	IncrementMin  *int   `json:"increment_min,omitempty"`
	IncrementMax  *int   `json:"increment_max,omitempty"`
}

// ListJobsResponse represents the response for listing jobs
type ListJobsResponse struct {
	Jobs         []JobSummary       `json:"jobs"`
	Total        int                `json:"total"`
	Page         int                `json:"page"`
	PageSize     int                `json:"page_size"`
	StatusCounts map[string]int     `json:"status_counts"`
}

// JobSummary represents a brief job summary for listing
type JobSummary struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	Priority          int       `json:"priority"`
	MaxAgents         int       `json:"max_agents"`
	DispatchedPercent float64   `json:"dispatched_percent"`
	SearchedPercent   float64   `json:"searched_percent"`
	CrackedCount      int       `json:"cracked_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// UpdateJobRequest represents the request body for updating a job
type UpdateJobRequest struct {
	Priority  *int `json:"priority,omitempty"`
	MaxAgents *int `json:"max_agents,omitempty"`
}

// CreateJob handles POST /api/v1/jobs
func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context (set by middleware)
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		h.sendError(w, "VALIDATION_ERROR", "Job name is required", http.StatusBadRequest)
		return
	}

	if req.HashlistID == 0 {
		h.sendError(w, "VALIDATION_ERROR", "Hashlist ID is required", http.StatusBadRequest)
		return
	}

	// Must specify either workflow_id or preset_job_id
	if req.WorkflowID == nil && req.PresetJobID == nil {
		h.sendError(w, "VALIDATION_ERROR", "Either workflow_id or preset_job_id is required", http.StatusBadRequest)
		return
	}

	if req.WorkflowID != nil && req.PresetJobID != nil {
		h.sendError(w, "VALIDATION_ERROR", "Cannot specify both workflow_id and preset_job_id", http.StatusBadRequest)
		return
	}

	// Validate priority against system setting
	maxPriority, err := h.systemSettingsRepo.GetMaxJobPriority(ctx)
	if err != nil {
		debug.Error("Failed to get max job priority setting: %v", err)
		maxPriority = 1000 // Default fallback
	}
	if req.Priority < 1 || req.Priority > maxPriority {
		h.sendError(w, "VALIDATION_ERROR", fmt.Sprintf("Priority must be between 1 and %d", maxPriority), http.StatusBadRequest)
		return
	}

	// Validate max_agents
	if req.MaxAgents < 0 {
		h.sendError(w, "VALIDATION_ERROR", "max_agents must be non-negative", http.StatusBadRequest)
		return
	}

	// Verify hashlist exists and user owns it
	hashlist, err := h.hashlistRepo.GetByID(ctx, req.HashlistID)
	if err != nil {
		if err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Hashlist not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get hashlist %d: %v", req.HashlistID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the hashlist
	if hashlist.UserID != userID {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this hashlist", http.StatusForbidden)
		return
	}

	var createdJobs []*models.JobExecution

	// Handle workflow-based job creation
	if req.WorkflowID != nil {
		// Verify workflow exists
		workflow, err := h.workflowRepo.GetWorkflowByID(ctx, *req.WorkflowID)
		if err != nil {
			if err == repository.ErrNotFound {
				h.sendError(w, "WORKFLOW_NOT_FOUND", "Workflow not found", http.StatusNotFound)
				return
			}
			debug.Error("Failed to get workflow %s: %v", *req.WorkflowID, err)
			h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get workflow steps
		steps, err := h.workflowRepo.GetWorkflowSteps(ctx, workflow.ID)
		if err != nil {
			debug.Error("Failed to get workflow steps for %s: %v", workflow.ID, err)
			h.sendError(w, "INTERNAL_ERROR", "Failed to retrieve workflow steps", http.StatusInternalServerError)
			return
		}

		if len(steps) == 0 {
			h.sendError(w, "VALIDATION_ERROR", "Workflow has no steps", http.StatusBadRequest)
			return
		}

		// Create a job for each preset in the workflow
		for _, step := range steps {
			jobName := fmt.Sprintf("%s - %s", req.Name, step.PresetJobName)
			jobExecution, err := h.jobExecService.CreateJobExecution(ctx, step.PresetJobID, req.HashlistID, &userID, jobName)
			if err != nil {
				debug.Error("Failed to create job execution for preset %s: %v", step.PresetJobID, err)
				h.sendError(w, "JOB_CREATION_FAILED", fmt.Sprintf("Failed to create job: %v", err), http.StatusBadRequest)
				return
			}

			// Update priority and max_agents if specified
			if req.Priority > 0 {
				if err := h.jobExecRepo.UpdatePriority(ctx, jobExecution.ID, req.Priority); err != nil {
					debug.Error("Failed to update job priority: %v", err)
				}
			}

			if req.MaxAgents > 0 {
				if err := h.jobExecRepo.UpdateMaxAgents(ctx, jobExecution.ID, req.MaxAgents); err != nil {
					debug.Error("Failed to update job max_agents: %v", err)
				}
			}

			// Reload job to get updated values
			jobExecution, _ = h.jobExecRepo.GetByID(ctx, jobExecution.ID)
			createdJobs = append(createdJobs, jobExecution)
		}
	}

	// Handle preset-based job creation
	if req.PresetJobID != nil {
		// Verify preset job exists
		presetJob, err := h.presetJobRepo.GetByID(ctx, *req.PresetJobID)
		if err != nil {
			if err == repository.ErrNotFound {
				h.sendError(w, "PRESET_JOB_NOT_FOUND", "Preset job not found", http.StatusNotFound)
				return
			}
			debug.Error("Failed to get preset job %s: %v", *req.PresetJobID, err)
			h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
			return
		}

		jobExecution, err := h.jobExecService.CreateJobExecution(ctx, presetJob.ID, req.HashlistID, &userID, req.Name)
		if err != nil {
			debug.Error("Failed to create job execution: %v", err)
			h.sendError(w, "JOB_CREATION_FAILED", fmt.Sprintf("Failed to create job: %v", err), http.StatusBadRequest)
			return
		}

		// Update priority and max_agents if specified
		if req.Priority > 0 {
			if err := h.jobExecRepo.UpdatePriority(ctx, jobExecution.ID, req.Priority); err != nil {
				debug.Error("Failed to update job priority: %v", err)
			}
		}

		if req.MaxAgents > 0 {
			if err := h.jobExecRepo.UpdateMaxAgents(ctx, jobExecution.ID, req.MaxAgents); err != nil {
				debug.Error("Failed to update job max_agents: %v", err)
			}
		}

		// Reload job to get updated values
		jobExecution, _ = h.jobExecRepo.GetByID(ctx, jobExecution.ID)
		createdJobs = append(createdJobs, jobExecution)
	}

	// If only one job created, return it directly
	if len(createdJobs) == 1 {
		response := CreateJobResponse{
			ID:        createdJobs[0].ID.String(),
			Status:    string(createdJobs[0].Status),
			Priority:  createdJobs[0].Priority,
			CreatedAt: createdJobs[0].CreatedAt,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Multiple jobs created (from workflow), return array
	var responses []CreateJobResponse
	for _, job := range createdJobs {
		responses = append(responses, CreateJobResponse{
			ID:        job.ID.String(),
			Status:    string(job.Status),
			Priority:  job.Priority,
			CreatedAt: job.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs": responses,
	})
}

// ListJobs handles GET /api/v1/jobs
func (h *JobHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	status := r.URL.Query().Get("status")
	priorityStr := r.URL.Query().Get("priority")
	search := r.URL.Query().Get("search")

	var priority *int
	if priorityStr != "" {
		p, err := strconv.Atoi(priorityStr)
		if err == nil && p >= 1 && p <= 10 {
			priority = &p
		}
	}

	// Create filter with user ID
	userIDStr := userID.String()
	filter := repository.JobFilter{
		Status:   &status,
		Priority: priority,
		Search:   &search,
		UserID:   &userIDStr,
	}

	// Get jobs with filters
	jobs, err := h.jobExecRepo.ListWithFilters(ctx, pageSize, (page-1)*pageSize, filter)
	if err != nil {
		debug.Error("Failed to list jobs: %v", err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get total count and status counts
	totalCount, err := h.jobExecRepo.GetFilteredCount(ctx, filter)
	if err != nil {
		debug.Error("Failed to count jobs: %v", err)
		totalCount = 0
	}

	statusCounts, err := h.jobExecRepo.GetStatusCountsForUser(ctx, userIDStr)
	if err != nil {
		debug.Error("Failed to count jobs by status: %v", err)
		statusCounts = map[string]int{}
	}

	// Convert to API response format
	summaries := make([]JobSummary, 0, len(jobs))
	for _, job := range jobs {
		// Get hashlist for this job
		hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
		if err != nil {
			debug.Error("Failed to get hashlist %d: %v", job.HashlistID, err)
			continue
		}

		// Calculate percentages
		dispatchedPercent := 0.0
		searchedPercent := 0.0

		if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
			dispatchedPercent = float64(job.DispatchedKeyspace) / float64(*job.EffectiveKeyspace) * 100
			searchedPercent = job.OverallProgressPercent
		}

		summaries = append(summaries, JobSummary{
			ID:                job.ID.String(),
			Name:              job.Name,
			Status:            string(job.Status),
			Priority:          job.Priority,
			MaxAgents:         job.MaxAgents,
			DispatchedPercent: dispatchedPercent,
			SearchedPercent:   searchedPercent,
			CrackedCount:      hashlist.CrackedHashes,
			CreatedAt:         job.CreatedAt,
			UpdatedAt:         job.UpdatedAt,
		})
	}

	response := ListJobsResponse{
		Jobs:         summaries,
		Total:        totalCount,
		Page:         page,
		PageSize:     pageSize,
		StatusCounts: statusCounts,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetJob handles GET /api/v1/jobs/{id}
func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job through hashlist ownership
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Get hashlist for cracked count
	hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", job.HashlistID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get active agent count for this job
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for job %s: %v", jobID, err)
	}

	agentCount := 0
	totalSpeed := int64(0)
	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning {
			agentCount++
			if task.BenchmarkSpeed != nil {
				totalSpeed += *task.BenchmarkSpeed
			}
		}
	}

	// Calculate percentages
	dispatchedPercent := 0.0
	searchedPercent := 0.0

	if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
		dispatchedPercent = float64(job.DispatchedKeyspace) / float64(*job.EffectiveKeyspace) * 100
		searchedPercent = job.OverallProgressPercent
	}

	response := JobStatusResponse{
		ID:                     job.ID.String(),
		Name:                   job.Name,
		Status:                 string(job.Status),
		Priority:               job.Priority,
		MaxAgents:              job.MaxAgents,
		DispatchedPercent:      dispatchedPercent,
		SearchedPercent:        searchedPercent,
		CrackedCount:           hashlist.CrackedHashes,
		AgentCount:             agentCount,
		TotalSpeed:             totalSpeed,
		CreatedAt:              job.CreatedAt,
		StartedAt:              job.StartedAt,
		CompletedAt:            job.CompletedAt,
		ErrorMessage:           job.ErrorMessage,
		EffectiveKeyspace:      job.EffectiveKeyspace,
		ProcessedKeyspace:      job.ProcessedKeyspace,
		DispatchedKeyspace:     job.DispatchedKeyspace,
		OverallProgressPercent: job.OverallProgressPercent,
		// Increment mode fields
		IncrementMode: job.IncrementMode,
		IncrementMin:  job.IncrementMin,
		IncrementMax:  job.IncrementMax,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UpdateJob handles PATCH /api/v1/jobs/{id}
func (h *JobHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	var req UpdateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Update priority if specified
	if req.Priority != nil {
		// Get max priority from system settings
		maxPriority, err := h.systemSettingsRepo.GetMaxJobPriority(ctx)
		if err != nil {
			debug.Error("Failed to get max job priority setting: %v", err)
			maxPriority = 1000 // Fallback to default
		}
		if *req.Priority < 1 || *req.Priority > maxPriority {
			h.sendError(w, "VALIDATION_ERROR", fmt.Sprintf("Priority must be between 1 and %d", maxPriority), http.StatusBadRequest)
			return
		}

		if err := h.jobExecRepo.UpdatePriority(ctx, jobID, *req.Priority); err != nil {
			debug.Error("Failed to update job priority: %v", err)
			h.sendError(w, "INTERNAL_ERROR", "Failed to update priority", http.StatusInternalServerError)
			return
		}
	}

	// Update max_agents if specified
	if req.MaxAgents != nil {
		if *req.MaxAgents < 0 {
			h.sendError(w, "VALIDATION_ERROR", "max_agents must be non-negative", http.StatusBadRequest)
			return
		}

		if err := h.jobExecRepo.UpdateMaxAgents(ctx, jobID, *req.MaxAgents); err != nil {
			debug.Error("Failed to update job max_agents: %v", err)
			h.sendError(w, "INTERNAL_ERROR", "Failed to update max_agents", http.StatusInternalServerError)
			return
		}
	}

	// Reload job to get updated values
	job, err = h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		debug.Error("Failed to reload job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return updated job
	response := CreateJobResponse{
		ID:        job.ID.String(),
		Status:    string(job.Status),
		Priority:  job.Priority,
		CreatedAt: job.CreatedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// DeleteJob handles DELETE /api/v1/jobs/{id}
func (h *JobHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Cancel the job first if it's running
	if job.Status == models.JobExecutionStatusRunning || job.Status == models.JobExecutionStatusPending {
		if err := h.schedulingService.StopJob(ctx, jobID, "User requested deletion"); err != nil {
			debug.Error("Failed to stop job %s: %v", jobID, err)
			// Continue with deletion even if stop fails
		}
	}

	// Delete the job (cascade deletes tasks)
	if err := h.jobExecRepo.Delete(ctx, jobID); err != nil {
		debug.Error("Failed to delete job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Failed to delete job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// StopJob handles POST /api/v1/jobs/{id}/stop
func (h *JobHandler) StopJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Check if job is in a stoppable state
	if job.Status != models.JobExecutionStatusRunning && job.Status != models.JobExecutionStatusPending {
		h.sendError(w, "VALIDATION_ERROR", "Job can only be stopped if it's running or pending", http.StatusBadRequest)
		return
	}

	// Stop the job
	if err := h.schedulingService.StopJob(ctx, jobID, "User requested stop"); err != nil {
		debug.Error("Failed to stop job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Failed to stop job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "stopped",
	})
}

// RetryJob handles POST /api/v1/jobs/{id}/retry
func (h *JobHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Check if job can be retried
	if job.Status != models.JobExecutionStatusFailed && job.Status != models.JobExecutionStatusCancelled {
		h.sendError(w, "VALIDATION_ERROR", "Job can only be retried if it's failed or cancelled", http.StatusBadRequest)
		return
	}

	// Reset the job to pending status
	if err := h.jobExecRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending); err != nil {
		debug.Error("Failed to reset job status: %v", err)
		h.sendError(w, "INTERNAL_ERROR", "Failed to retry job", http.StatusInternalServerError)
		return
	}

	// Clear error message
	if err := h.jobExecRepo.ClearError(ctx, jobID); err != nil {
		debug.Error("Failed to clear job error: %v", err)
		// Don't fail the request, just log the error
	}

	// Mark failed/cancelled tasks as pending so they can be retried
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err == nil {
		for _, task := range tasks {
			if task.Status == models.JobTaskStatusFailed || task.Status == models.JobTaskStatusCancelled {
				if err := h.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusPending); err != nil {
					debug.Error("Failed to reset task %s status: %v", task.ID, err)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "pending",
	})
}

// verifyJobOwnership checks if the user owns the job through hashlist ownership
func (h *JobHandler) verifyJobOwnership(ctx context.Context, job *models.JobExecution, userID uuid.UUID) error {
	// Get hashlist
	hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Check if user owns the hashlist
	if hashlist.UserID != userID {
		return fmt.Errorf("user does not own this job")
	}

	return nil
}

// sendError sends a standardized error response
func (h *JobHandler) sendError(w http.ResponseWriter, code, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIError{
		Code:    code,
		Message: message,
	})
}

// GetJobLayers returns all increment layers for a job with statistics
// GET /api/v1/jobs/{id}/layers
func (h *JobHandler) GetJobLayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job through hashlist ownership
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Check if this is an increment mode job
	if job.IncrementMode == "" || job.IncrementMode == "off" {
		h.sendError(w, "VALIDATION_ERROR", "Job is not an increment mode job", http.StatusBadRequest)
		return
	}

	// Get layers with stats
	layers, err := h.jobIncrementLayerRepo.GetLayersWithStats(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get job layers: %v", err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(layers)
}

// GetJobLayerTasks returns all tasks for a specific increment layer
// GET /api/v1/jobs/{id}/layers/{layer_id}/tasks
func (h *JobHandler) GetJobLayerTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		h.sendError(w, "AUTHENTICATION_REQUIRED", "User authentication required", http.StatusUnauthorized)
		return
	}

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid job ID", http.StatusBadRequest)
		return
	}

	layerID, err := uuid.Parse(vars["layer_id"])
	if err != nil {
		h.sendError(w, "VALIDATION_ERROR", "Invalid layer ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Job not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get job %s: %v", jobID, err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Verify user owns the job through hashlist ownership
	if err := h.verifyJobOwnership(ctx, job, userID); err != nil {
		h.sendError(w, "RESOURCE_ACCESS_DENIED", "You do not have access to this job", http.StatusForbidden)
		return
	}

	// Verify the layer belongs to this job
	layer, err := h.jobIncrementLayerRepo.GetByID(ctx, layerID)
	if err != nil {
		if err == repository.ErrNotFound || err == sql.ErrNoRows {
			h.sendError(w, "RESOURCE_NOT_FOUND", "Layer not found", http.StatusNotFound)
			return
		}
		debug.Error("Failed to get layer: %v", err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	if layer.JobExecutionID != jobID {
		h.sendError(w, "VALIDATION_ERROR", "Layer does not belong to this job", http.StatusBadRequest)
		return
	}

	// Get all tasks for the job
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks: %v", err)
		h.sendError(w, "INTERNAL_ERROR", "Internal server error", http.StatusInternalServerError)
		return
	}

	// Filter tasks for this layer
	var layerTasks []models.JobTask
	for _, task := range tasks {
		if task.IncrementLayerID != nil && *task.IncrementLayerID == layerID {
			layerTasks = append(layerTasks, task)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(layerTasks)
}
