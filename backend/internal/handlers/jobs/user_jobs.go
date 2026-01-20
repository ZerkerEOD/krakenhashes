package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// UserJobsHandler handles job-related requests from users
type UserJobsHandler struct {
	jobExecRepo           *repository.JobExecutionRepository
	jobTaskRepo           *repository.JobTaskRepository
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	presetJobRepo         repository.PresetJobRepository
	hashlistRepo          *repository.HashListRepository
	clientRepo            *repository.ClientRepository
	workflowRepo          repository.JobWorkflowRepository
	hashTypeRepo          *repository.HashTypeRepository
	wordlistStore         *wordlist.Store
	ruleStore             *rule.Store
	binaryStore           binary.Store
	jobExecutionService   *services.JobExecutionService
	systemSettingsRepo    *repository.SystemSettingsRepository
	assocWordlistRepo     *repository.AssociationWordlistRepository
	wsHandler             WSHandler
}

// WSHandler interface for WebSocket operations
type WSHandler interface {
	SendMessage(agentID int, msg interface{}) error
}

// SetWSHandler sets the WebSocket handler after creation
func (h *UserJobsHandler) SetWSHandler(wsHandler WSHandler) {
	h.wsHandler = wsHandler
}

// NewUserJobsHandler creates a new user jobs handler
func NewUserJobsHandler(
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
	presetJobRepo repository.PresetJobRepository,
	hashlistRepo *repository.HashListRepository,
	clientRepo *repository.ClientRepository,
	workflowRepo repository.JobWorkflowRepository,
	hashTypeRepo *repository.HashTypeRepository,
	wordlistStore *wordlist.Store,
	ruleStore *rule.Store,
	binaryStore binary.Store,
	jobExecutionService *services.JobExecutionService,
	systemSettingsRepo *repository.SystemSettingsRepository,
	assocWordlistRepo *repository.AssociationWordlistRepository,
) *UserJobsHandler {
	return &UserJobsHandler{
		jobExecRepo:           jobExecRepo,
		jobTaskRepo:           jobTaskRepo,
		jobIncrementLayerRepo: jobIncrementLayerRepo,
		presetJobRepo:         presetJobRepo,
		hashlistRepo:          hashlistRepo,
		clientRepo:            clientRepo,
		workflowRepo:          workflowRepo,
		hashTypeRepo:          hashTypeRepo,
		wordlistStore:         wordlistStore,
		ruleStore:             ruleStore,
		binaryStore:           binaryStore,
		jobExecutionService:   jobExecutionService,
		systemSettingsRepo:    systemSettingsRepo,
		assocWordlistRepo:     assocWordlistRepo,
		wsHandler:             nil, // Will be set later via SetWSHandler
	}
}

// JobSummary represents a job summary for the UI
type JobSummary struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	HashlistID             int64   `json:"hashlist_id"`
	HashlistName           string  `json:"hashlist_name"`
	Status                 string  `json:"status"`
	Priority               int     `json:"priority"`
	MaxAgents              int     `json:"max_agents"`
	DispatchedPercent      float64 `json:"dispatched_percent"`
	SearchedPercent        float64 `json:"searched_percent"`
	CrackedCount           int     `json:"cracked_count"`
	AgentCount             int     `json:"agent_count"`
	TotalSpeed             int64   `json:"total_speed"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
	CompletedAt            *string `json:"completed_at,omitempty"`
	CreatedByUsername      *string `json:"created_by_username,omitempty"`
	ErrorMessage           *string `json:"error_message,omitempty"`
	TotalKeyspace          *int64  `json:"total_keyspace,omitempty"`
	EffectiveKeyspace      *int64  `json:"effective_keyspace,omitempty"`
	MultiplicationFactor   int     `json:"multiplication_factor,omitempty"`
	UsesRuleSplitting      bool    `json:"uses_rule_splitting"`
	ProcessedKeyspace      *int64  `json:"processed_keyspace,omitempty"`
	DispatchedKeyspace     *int64  `json:"dispatched_keyspace,omitempty"`
	OverallProgressPercent float64 `json:"overall_progress_percent"`
}

// ListJobs handles GET /api/jobs with pagination and filtering
func (h *UserJobsHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	// Parse filters
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

	// Create filter
	filter := repository.JobFilter{
		Status:   &status,
		Priority: priority,
		Search:   &search,
	}

	// Get jobs with filters and user information
	jobsWithUser, err := h.jobExecRepo.ListWithFiltersAndUser(ctx, pageSize, (page-1)*pageSize, filter)
	if err != nil {
		debug.Error("Failed to list jobs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get total count with filters
	total, err := h.jobExecRepo.GetFilteredCount(ctx, filter)
	if err != nil {
		debug.Error("Failed to get job count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get status counts
	statusCounts, err := h.jobExecRepo.GetStatusCounts(ctx)
	if err != nil {
		debug.Error("Failed to get status counts: %v", err)
		// Don't fail the request, just log the error
		statusCounts = make(map[string]int)
	}

	// Convert to job summaries
	summaries := make([]JobSummary, 0, len(jobsWithUser))
	for _, jobWithUser := range jobsWithUser {
		job := jobWithUser.JobExecution
		// Get hashlist details including cracked count
		hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
		if err != nil {
			debug.Error("Failed to get hashlist %d: %v", job.HashlistID, err)
			continue
		}

		// Get task statistics
		tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
		if err != nil {
			debug.Error("Failed to get tasks for job %s: %v", job.ID, err)
			tasks = []models.JobTask{}
		}

		// Calculate metrics
		var agentCount int
		var totalSpeed int64
		var crackedCount int
		var keyspaceSearched int64
		// Note: keyspaceDispatched removed - use job.DispatchedKeyspace instead (calculated by job_progress_calculation_service)

		for _, task := range tasks {
			if task.Status == models.JobTaskStatusRunning {
				agentCount++
				if task.BenchmarkSpeed != nil {
					totalSpeed += *task.BenchmarkSpeed
				}
			}
			crackedCount += task.CrackCount
			keyspaceSearched += task.KeyspaceProcessed
		}

		// Calculate percentages using effective keyspace when available
		dispatchedPercent := 0.0
		searchedPercent := 0.0
		overallProgressPercent := 0.0

		// Use effective keyspace if available, otherwise fall back to total keyspace
		var keyspaceForProgress int64
		if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
			keyspaceForProgress = *job.EffectiveKeyspace
		} else if job.TotalKeyspace != nil && *job.TotalKeyspace > 0 {
			keyspaceForProgress = *job.TotalKeyspace
		} else {
			keyspaceForProgress = 0
		}

		if keyspaceForProgress > 0 {
			// For dispatched percentage, we need to consider rule splitting
			if job.UsesRuleSplitting {
				// For rule split jobs, calculate based on effective keyspace
				var totalEffectiveDispatched int64 = 0
				var totalEffectiveSearched int64 = 0

				for _, task := range tasks {
					// Calculate dispatched effective keyspace for non-pending tasks
					if task.Status != models.JobTaskStatusPending {
						if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
							totalEffectiveDispatched += (*task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart)
						}
					}
					
					// Calculate searched effective keyspace from all tasks
					if task.EffectiveKeyspaceProcessed != nil {
						totalEffectiveSearched += *task.EffectiveKeyspaceProcessed
					}
				}

				// Both percentages are relative to total effective keyspace
				if keyspaceForProgress > 0 {
					searchedPercent = float64(totalEffectiveSearched) / float64(keyspaceForProgress) * 100
					dispatchedPercent = float64(totalEffectiveDispatched) / float64(keyspaceForProgress) * 100
				}
			} else {
				// For keyspace-based jobs (including keyspace-split jobs)
				// Use the pre-calculated dispatched_keyspace from job_progress_calculation_service
				// which correctly handles effective keyspace for keyspace-split jobs
				searchedPercent = float64(job.ProcessedKeyspace) / float64(keyspaceForProgress) * 100
				dispatchedPercent = float64(job.DispatchedKeyspace) / float64(keyspaceForProgress) * 100
			}

			// Cap percentages at 100%
			if dispatchedPercent > 100 {
				dispatchedPercent = 100
			}
			if searchedPercent > 100 {
				searchedPercent = 100
			}

			// Overall progress is the searched percentage
			overallProgressPercent = searchedPercent
		}

		// Use the backend-calculated overall progress if available and more accurate
		if job.OverallProgressPercent > 0 {
			overallProgressPercent = job.OverallProgressPercent
			// For consistency, use this for searched percentage too
			searchedPercent = overallProgressPercent
		}

		summary := JobSummary{
			ID:                     job.ID.String(),
			Name:                   getJobName(job, hashlist),
			HashlistID:             job.HashlistID,
			HashlistName:           hashlist.Name,
			Status:                 string(job.Status),
			Priority:               job.Priority,
			MaxAgents:              job.MaxAgents,
			DispatchedPercent:      dispatchedPercent,
			SearchedPercent:        searchedPercent,
			CrackedCount:           crackedCount,
			AgentCount:             agentCount,
			TotalSpeed:             totalSpeed,
			CreatedAt:              job.CreatedAt.Format(time.RFC3339),
			UpdatedAt:              job.UpdatedAt.Format(time.RFC3339),
			CreatedByUsername:      jobWithUser.CreatedByUsername,
			TotalKeyspace:          job.TotalKeyspace,
			EffectiveKeyspace:      job.EffectiveKeyspace,
			MultiplicationFactor:   job.MultiplicationFactor,
			UsesRuleSplitting:      job.UsesRuleSplitting,
			ProcessedKeyspace:      &job.ProcessedKeyspace,
			DispatchedKeyspace:     &job.DispatchedKeyspace,
			OverallProgressPercent: overallProgressPercent,
		}

		// Add completed time if present
		if job.CompletedAt != nil {
			completedAtStr := job.CompletedAt.Format(time.RFC3339)
			summary.CompletedAt = &completedAtStr
		}

		// Add error message if present
		if job.ErrorMessage != nil && *job.ErrorMessage != "" {
			summary.ErrorMessage = job.ErrorMessage
		}

		summaries = append(summaries, summary)
	}

	// Prepare response
	response := map[string]interface{}{
		"jobs": summaries,
		"pagination": map[string]interface{}{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
		"status_counts": statusCounts,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// getJobName generates a display name for a job
func getJobName(job models.JobExecution, hashlist *models.HashList) string {
	// Job name should always be set during creation now
	if job.Name != "" {
		return job.Name
	}
	// This is a fallback for legacy jobs without names
	return hashlist.Name
}

// JobNameConfig holds all data needed to generate a job name
type JobNameConfig struct {
	Client                   *models.Client
	AttackMode               models.AttackMode
	WordlistNames            []string
	RuleNames                []string
	Mask                     string
	IncrementMode            string // "off", "increment", "increment_inverse"
	HashTypeID               int
	CustomName               string
	AssociationWordlistName  string // For association attack (mode 9)
}

// resolveWordlistNames converts wordlist IDs to their names
func (h *UserJobsHandler) resolveWordlistNames(ctx context.Context, wordlistIDs []string) []string {
	names := make([]string, 0, len(wordlistIDs))
	for _, idStr := range wordlistIDs {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		wl, err := h.wordlistStore.GetWordlist(ctx, id)
		if err != nil || wl == nil {
			continue
		}
		names = append(names, wl.Name)
	}
	return names
}

// resolveRuleNames converts rule IDs to their names
func (h *UserJobsHandler) resolveRuleNames(ctx context.Context, ruleIDs []string) []string {
	names := make([]string, 0, len(ruleIDs))
	for _, idStr := range ruleIDs {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		rl, err := h.ruleStore.GetRule(ctx, id)
		if err != nil || rl == nil {
			continue
		}
		names = append(names, rl.Name)
	}
	return names
}

// generateJobName creates a job name based on the attack configuration
func generateJobName(config JobNameConfig) string {
	// User-provided custom name takes priority
	if config.CustomName != "" {
		return config.CustomName
	}

	var parts []string

	// Add client name (skip if empty - don't use "Unknown")
	if config.Client != nil && config.Client.Name != "" {
		parts = append(parts, config.Client.Name)
	}

	// Build attack-specific portion
	attackPart := buildAttackPart(config)
	if attackPart != "" {
		parts = append(parts, attackPart)
	}

	// Add increment indicator for mask-based attacks
	if config.IncrementMode == "increment" {
		parts = append(parts, "inc-L-R")
	} else if config.IncrementMode == "increment_inverse" {
		parts = append(parts, "inc-R-L")
	}

	// Always add hash mode at the end
	hashModeStr := strconv.Itoa(config.HashTypeID)
	parts = append(parts, hashModeStr)

	// Join with dashes
	name := strings.Join(parts, "-")

	// Truncate if exceeds 255 characters
	if len(name) > 255 {
		name = truncateJobName(name, hashModeStr, 255)
	}

	return name
}

// buildAttackPart generates the attack-specific portion of the job name
func buildAttackPart(config JobNameConfig) string {
	switch config.AttackMode {
	case models.AttackModeStraight: // Mode 0: Dictionary
		var attackParts []string
		if len(config.WordlistNames) > 0 {
			attackParts = append(attackParts, config.WordlistNames[0])
		}
		if len(config.RuleNames) > 0 {
			attackParts = append(attackParts, strings.Join(config.RuleNames, ","))
		}
		return strings.Join(attackParts, "-")

	case models.AttackModeCombination: // Mode 1: Combination (2 wordlists)
		if len(config.WordlistNames) >= 2 {
			if config.WordlistNames[0] == config.WordlistNames[1] {
				return fmt.Sprintf("%s(x2)", config.WordlistNames[0])
			}
			return fmt.Sprintf("%s,%s", config.WordlistNames[0], config.WordlistNames[1])
		}
		return ""

	case models.AttackModeBruteForce: // Mode 3: Mask
		return config.Mask

	case models.AttackModeHybridWordlistMask: // Mode 6: Wordlist + Mask
		var attackParts []string
		if len(config.WordlistNames) > 0 {
			attackParts = append(attackParts, config.WordlistNames[0])
		}
		if config.Mask != "" {
			attackParts = append(attackParts, config.Mask)
		}
		return strings.Join(attackParts, "-")

	case models.AttackModeHybridMaskWordlist: // Mode 7: Mask + Wordlist
		var attackParts []string
		if config.Mask != "" {
			attackParts = append(attackParts, config.Mask)
		}
		if len(config.WordlistNames) > 0 {
			attackParts = append(attackParts, config.WordlistNames[0])
		}
		return strings.Join(attackParts, "-")

	case models.AttackModeAssociation: // Mode 9: Association
		if config.AssociationWordlistName != "" {
			return fmt.Sprintf("assoc-%s", config.AssociationWordlistName)
		}
		return "assoc"

	default:
		return ""
	}
}

// truncateJobName truncates the job name while preserving the hash mode
func truncateJobName(name string, hashMode string, maxLen int) string {
	suffix := "-" + hashMode
	availableLen := maxLen - len(suffix)

	if availableLen <= 0 {
		return hashMode
	}

	prefixEnd := len(name) - len(suffix)
	if prefixEnd <= 0 {
		return name
	}
	prefix := name[:prefixEnd]

	if len(prefix) > availableLen {
		prefix = prefix[:availableLen-3] + "..."
	}

	return prefix + suffix
}

// CreateJobFromHashlist handles POST /api/hashlists/{id}/create-job
func (h *UserJobsHandler) CreateJobFromHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	// Get user ID from context
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse user ID to UUID
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}

	hashlistID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}

	// Parse the request body to determine job type
	var rawReq json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Determine the job type
	var jobType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rawReq, &jobType); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Verify the hashlist exists and get its details
	hashlist, err := h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", hashlistID, err)
		http.Error(w, "Hashlist not found", http.StatusNotFound)
		return
	}
	
	// Get client info if available
	var client *models.Client
	if hashlist.ClientID != uuid.Nil {
		client, err = h.clientRepo.GetByID(ctx, hashlist.ClientID)
		if err != nil {
			debug.Warning("Failed to get client %s: %v", hashlist.ClientID, err)
			// Don't fail, just continue without client info
			client = nil
		}
	}

	var createdJobs []string

	switch jobType.Type {
	case "preset":
		// Handle preset jobs
		var req struct {
			Type          string   `json:"type"`
			PresetJobIDs  []string `json:"preset_job_ids"`
			CustomJobName string   `json:"custom_job_name"`
		}
		if err := json.Unmarshal(rawReq, &req); err != nil {
			http.Error(w, "Invalid preset job request", http.StatusBadRequest)
			return
		}

		// Create a job execution for each selected preset job
		for _, presetJobIDStr := range req.PresetJobIDs {
			presetJobID, err := uuid.Parse(presetJobIDStr)
			if err != nil {
				debug.Error("Invalid preset job ID: %s", presetJobIDStr)
				continue
			}

			// Get the preset job to verify it exists and get its configuration
			presetJob, err := h.presetJobRepo.GetByID(ctx, presetJobID)
			if err != nil {
				debug.Error("Failed to get preset job %s: %v", presetJobID, err)
				continue
			}

			// Resolve wordlist and rule names from preset
			wordlistNames := h.resolveWordlistNames(ctx, presetJob.WordlistIDs)
			ruleNames := h.resolveRuleNames(ctx, presetJob.RuleIDs)

			// Generate job name based on attack configuration
			jobName := generateJobName(JobNameConfig{
				Client:        client,
				AttackMode:    presetJob.AttackMode,
				WordlistNames: wordlistNames,
				RuleNames:     ruleNames,
				Mask:          presetJob.Mask,
				IncrementMode: presetJob.IncrementMode,
				HashTypeID:    hashlist.HashTypeID,
				CustomName:    req.CustomJobName,
			})

			// Use CreateJobExecution to create job with keyspace calculation
			jobExecution, err := h.jobExecutionService.CreateJobExecution(ctx, presetJobID, hashlistID, &userID, jobName)
			if err != nil {
				debug.Error("Failed to create job execution for preset %s: %v", presetJobID, err)
				continue
			}

			createdJobs = append(createdJobs, jobExecution.ID.String())
		}

	case "workflow":
		// Handle workflows
		var req struct {
			Type          string   `json:"type"`
			WorkflowIDs   []string `json:"workflow_ids"`
			CustomJobName string   `json:"custom_job_name"`
		}
		if err := json.Unmarshal(rawReq, &req); err != nil {
			http.Error(w, "Invalid workflow request", http.StatusBadRequest)
			return
		}

		// For each workflow, create jobs for all its steps
		for _, workflowIDStr := range req.WorkflowIDs {
			workflowID, err := uuid.Parse(workflowIDStr)
			if err != nil {
				debug.Error("Invalid workflow ID: %s", workflowIDStr)
				continue
			}

			// Get workflow steps
			steps, err := h.workflowRepo.GetWorkflowSteps(ctx, workflowID)
			if err != nil {
				debug.Error("Failed to get workflow steps for %s: %v", workflowID, err)
				continue
			}

			// Create a job for each step in order
			for _, step := range steps {
				// Verify the preset job exists and get its configuration
				presetJob, err := h.presetJobRepo.GetByID(ctx, step.PresetJobID)
				if err != nil {
					debug.Error("Failed to get preset job %s for workflow step: %v", step.PresetJobID, err)
					continue
				}

				// Resolve wordlist and rule names from preset
				wordlistNames := h.resolveWordlistNames(ctx, presetJob.WordlistIDs)
				ruleNames := h.resolveRuleNames(ctx, presetJob.RuleIDs)

				// Generate job name for workflow step based on attack configuration
				jobName := generateJobName(JobNameConfig{
					Client:        client,
					AttackMode:    presetJob.AttackMode,
					WordlistNames: wordlistNames,
					RuleNames:     ruleNames,
					Mask:          presetJob.Mask,
					IncrementMode: presetJob.IncrementMode,
					HashTypeID:    hashlist.HashTypeID,
					CustomName:    req.CustomJobName,
				})

				// Use CreateJobExecution to create job with keyspace calculation
				jobExecution, err := h.jobExecutionService.CreateJobExecution(ctx, step.PresetJobID, hashlistID, &userID, jobName)
				if err != nil {
					debug.Error("Failed to create job execution for workflow step: %v", err)
					continue
				}

				createdJobs = append(createdJobs, jobExecution.ID.String())
			}
		}

	case "custom":
		// Handle custom job
		var req struct {
			Type          string `json:"type"`
			CustomJobName string `json:"custom_job_name"`
			CustomJob struct {
				Name                      string   `json:"name"`
				AttackMode                int      `json:"attack_mode"`
				WordlistIDs               []string `json:"wordlist_ids"`
				RuleIDs                   []string `json:"rule_ids"`
				Mask                      string   `json:"mask"`
				Priority                  int      `json:"priority"`
				MaxAgents                 int      `json:"max_agents"`
				BinaryVersion             string   `json:"binary_version"`
				AllowHighPriorityOverride bool     `json:"allow_high_priority_override"`
				ChunkSizeSeconds          int      `json:"chunk_size_seconds"`
				IncrementMode             string   `json:"increment_mode"`
				IncrementMin              *int     `json:"increment_min"`
				IncrementMax              *int     `json:"increment_max"`
				AssociationWordlistID     *string  `json:"association_wordlist_id"`
			} `json:"custom_job"`
		}
		if err := json.Unmarshal(rawReq, &req); err != nil {
			http.Error(w, "Invalid custom job request", http.StatusBadRequest)
			return
		}

		// Validate association attack mode (mode 9)
		if models.AttackMode(req.CustomJob.AttackMode) == models.AttackModeAssociation {
			// Block if hashlist has mixed work factors
			if hashlist.HasMixedWorkFactors {
				http.Error(w, "Association attacks cannot be run on hashlists with mixed work factors (e.g., bcrypt with varying costs). All hashes must have the same work factor.", http.StatusBadRequest)
				return
			}

			// Require association wordlist ID for mode 9
			if req.CustomJob.AssociationWordlistID == nil || *req.CustomJob.AssociationWordlistID == "" {
				http.Error(w, "Association attacks require an association wordlist. Please upload one for this hashlist first.", http.StatusBadRequest)
				return
			}

			// Parse and validate the association wordlist
			assocWordlistUUID, err := uuid.Parse(*req.CustomJob.AssociationWordlistID)
			if err != nil {
				http.Error(w, "Invalid association wordlist ID format", http.StatusBadRequest)
				return
			}

			// Get the association wordlist to validate it exists and belongs to this hashlist
			assocWordlist, err := h.assocWordlistRepo.GetByID(ctx, assocWordlistUUID)
			if err != nil {
				debug.Error("Failed to get association wordlist %s: %v", assocWordlistUUID, err)
				http.Error(w, "Association wordlist not found", http.StatusNotFound)
				return
			}

			// Verify the wordlist belongs to this hashlist
			if assocWordlist.HashlistID != hashlistID {
				http.Error(w, "Association wordlist does not belong to this hashlist", http.StatusBadRequest)
				return
			}

			// Validate line count matches
			if assocWordlist.LineCount != int64(hashlist.TotalHashes) {
				http.Error(w, fmt.Sprintf("Association wordlist line count (%d) does not match hashlist hash count (%d). Association attacks require exact 1:1 mapping.", assocWordlist.LineCount, hashlist.TotalHashes), http.StatusBadRequest)
				return
			}
		}

		// Debug logging for increment mode
		debug.Info("Received custom job request with increment settings", map[string]interface{}{
			"increment_mode": req.CustomJob.IncrementMode,
			"increment_min":  req.CustomJob.IncrementMin,
			"increment_max":  req.CustomJob.IncrementMax,
			"mask":           req.CustomJob.Mask,
			"attack_mode":    req.CustomJob.AttackMode,
		})

		// Create custom job configuration (NO preset job creation)
		config := services.CustomJobConfig{
			Name:                      req.CustomJob.Name,
			AttackMode:                models.AttackMode(req.CustomJob.AttackMode),
			WordlistIDs:               models.IDArray(req.CustomJob.WordlistIDs),
			RuleIDs:                   models.IDArray(req.CustomJob.RuleIDs),
			Mask:                      req.CustomJob.Mask,
			Priority:                  req.CustomJob.Priority,
			MaxAgents:                 req.CustomJob.MaxAgents,
			BinaryVersion:             req.CustomJob.BinaryVersion,
			AllowHighPriorityOverride: req.CustomJob.AllowHighPriorityOverride,
			ChunkSizeSeconds:          req.CustomJob.ChunkSizeSeconds,
			IncrementMode:             req.CustomJob.IncrementMode,
			IncrementMin:              req.CustomJob.IncrementMin,
			IncrementMax:              req.CustomJob.IncrementMax,
		}

		// Add association wordlist ID for mode 9
		if req.CustomJob.AssociationWordlistID != nil && *req.CustomJob.AssociationWordlistID != "" {
			assocUUID, _ := uuid.Parse(*req.CustomJob.AssociationWordlistID)
			config.AssociationWordlistID = &assocUUID
		}

		// Resolve wordlist and rule names for custom job
		wordlistNames := h.resolveWordlistNames(ctx, req.CustomJob.WordlistIDs)
		ruleNames := h.resolveRuleNames(ctx, req.CustomJob.RuleIDs)

		// Get association wordlist name if present
		var assocWordlistName string
		if req.CustomJob.AssociationWordlistID != nil && *req.CustomJob.AssociationWordlistID != "" {
			assocUUID, err := uuid.Parse(*req.CustomJob.AssociationWordlistID)
			if err == nil {
				if assocWL, err := h.assocWordlistRepo.GetByID(ctx, assocUUID); err == nil {
					assocWordlistName = assocWL.FileName
				}
			}
		}

		// Generate job name for custom job based on attack configuration
		jobName := generateJobName(JobNameConfig{
			Client:                  client,
			AttackMode:              models.AttackMode(req.CustomJob.AttackMode),
			WordlistNames:           wordlistNames,
			RuleNames:               ruleNames,
			Mask:                    req.CustomJob.Mask,
			IncrementMode:           req.CustomJob.IncrementMode,
			HashTypeID:              hashlist.HashTypeID,
			CustomName:              req.CustomJobName,
			AssociationWordlistName: assocWordlistName,
		})
		
		// Create job execution directly without saving preset
		jobExecution, err := h.jobExecutionService.CreateCustomJobExecution(ctx, config, hashlistID, &userID, jobName)
		if err != nil {
			debug.Error("Failed to create custom job execution: %v", err)

			// Propagate meaningful error messages for keyspace-related errors
			errMsg := err.Error()
			if strings.Contains(errMsg, "overflow") || strings.Contains(errMsg, "exceeds") ||
				strings.Contains(errMsg, "too large") || strings.Contains(errMsg, "keyspace") {
				http.Error(w, fmt.Sprintf("Keyspace error: %s", errMsg), http.StatusBadRequest)
				return
			}

			http.Error(w, fmt.Sprintf("Failed to create job: %s", errMsg), http.StatusInternalServerError)
			return
		}

		createdJobs = append(createdJobs, jobExecution.ID.String())

	default:
		http.Error(w, "Invalid job type", http.StatusBadRequest)
		return
	}

	if len(createdJobs) == 0 {
		http.Error(w, "No jobs were created", http.StatusInternalServerError)
		return
	}

	// Return the created jobs
	response := map[string]interface{}{
		"ids":     createdJobs,
		"message": fmt.Sprintf("%d job(s) created successfully", len(createdJobs)),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// GetJobDetail handles GET /api/jobs/{id}
func (h *UserJobsHandler) GetJobDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get job execution
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get job %s: %v", jobID, err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Get hashlist
	hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", job.HashlistID, err)
		http.Error(w, "Hashlist not found", http.StatusNotFound)
		return
	}

	// Get hash type name and format as "HashName (Mode)"
	var formattedHashType string
	if job.HashType > 0 {
		hashType, err := h.hashTypeRepo.GetByID(ctx, job.HashType)
		if err != nil {
			debug.Warning("Failed to get hash type %d for job %s: %v", job.HashType, jobID, err)
			formattedHashType = fmt.Sprintf("Unknown (%d)", job.HashType)
		} else {
			formattedHashType = fmt.Sprintf("%s (%d)", hashType.Name, job.HashType)
		}
	}

	// Resolve wordlist IDs to names
	wordlistNames := make([]string, 0, len(job.WordlistIDs))
	for _, idStr := range job.WordlistIDs {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			debug.Warning("Invalid wordlist ID %s for job %s: %v", idStr, jobID, err)
			wordlistNames = append(wordlistNames, fmt.Sprintf("Unknown (ID: %s)", idStr))
			continue
		}
		wl, err := h.wordlistStore.GetWordlist(ctx, id)
		if err != nil || wl == nil {
			debug.Warning("Failed to get wordlist %d for job %s: %v", id, jobID, err)
			wordlistNames = append(wordlistNames, fmt.Sprintf("Unknown (ID: %d)", id))
			continue
		}
		wordlistNames = append(wordlistNames, wl.Name)
	}

	// Resolve rule IDs to names
	ruleNames := make([]string, 0, len(job.RuleIDs))
	for _, idStr := range job.RuleIDs {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			debug.Warning("Invalid rule ID %s for job %s: %v", idStr, jobID, err)
			ruleNames = append(ruleNames, fmt.Sprintf("Unknown (ID: %s)", idStr))
			continue
		}
		rl, err := h.ruleStore.GetRule(ctx, id)
		if err != nil || rl == nil {
			debug.Warning("Failed to get rule %d for job %s: %v", id, jobID, err)
			ruleNames = append(ruleNames, fmt.Sprintf("Unknown (ID: %d)", id))
			continue
		}
		ruleNames = append(ruleNames, rl.Name)
	}

	// Get ALL tasks for this job execution (no pagination)
	// Frontend will handle filtering and client-side pagination
	tasks, err := h.jobTaskRepo.GetAllTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for job %s: %v", jobID, err)
		tasks = []models.JobTask{}
	}

	// Total task count is simply the length of all tasks
	totalTasks := len(tasks)

	// Calculate metrics
	var agentCount int
	var totalSpeed int64
	var crackedCount int
	var keyspaceSearched int64
	activeAgents := make(map[int]bool)

	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning {
			if task.AgentID != nil {
				activeAgents[*task.AgentID] = true
			}
			if task.BenchmarkSpeed != nil {
				totalSpeed += *task.BenchmarkSpeed
			}
		}
		crackedCount += task.CrackCount
		keyspaceSearched += task.KeyspaceProcessed
	}
	agentCount = len(activeAgents)

	// Calculate percentages
	dispatchedPercent := 0.0
	searchedPercent := 0.0
	
	// For jobs with rules, use effective keyspace as the denominator
	totalKeyspace := job.TotalKeyspace
	if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
		totalKeyspace = job.EffectiveKeyspace
	}
	
	if totalKeyspace != nil && *totalKeyspace > 0 {
		// Dispatched: Use the tracked dispatched_keyspace field
		dispatchedPercent = float64(job.DispatchedKeyspace) / float64(*totalKeyspace) * 100
		// Searched: Use the processed_keyspace from the job execution
		searchedPercent = float64(job.ProcessedKeyspace) / float64(*totalKeyspace) * 100
		
		// Validation: Log if searched exceeds dispatched
		if searchedPercent > dispatchedPercent {
			debug.Warning("Searched percentage (%.3f%%) exceeds dispatched percentage (%.3f%%) for job %s",
				searchedPercent, dispatchedPercent, job.ID)
		}
		
		// Cap percentages at 100%
		if dispatchedPercent > 100 {
			debug.Warning("Dispatched percentage exceeds 100%% (%.3f%%) for job %s, capping at 100%%",
				dispatchedPercent, job.ID)
			dispatchedPercent = 100
		}
		if searchedPercent > 100 {
			debug.Warning("Searched percentage exceeds 100%% (%.3f%%) for job %s, capping at 100%%", 
				searchedPercent, job.ID)
			searchedPercent = 100
		}
	}

	// Prepare task summaries
	taskSummaries := make([]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		// Calculate task progress percentage based on effective or regular keyspace
		taskProgressPercent := task.ProgressPercent
		if taskProgressPercent == 0 {
			// Fallback calculation if not set in database
			if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil && task.EffectiveKeyspaceProcessed != nil {
				effectiveSize := *task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart
				if effectiveSize > 0 {
					taskProgressPercent = float64(*task.EffectiveKeyspaceProcessed) / float64(effectiveSize) * 100
				}
			} else {
				taskKeyspaceSize := task.KeyspaceEnd - task.KeyspaceStart
				if taskKeyspaceSize > 0 {
					taskProgressPercent = float64(task.KeyspaceProcessed) / float64(taskKeyspaceSize) * 100
				}
			}
			if taskProgressPercent > 100 {
				taskProgressPercent = 100
			}
		}

		taskSummary := map[string]interface{}{
			"id":                           task.ID.String(),
			"agent_id":                     task.AgentID,
			"status":                       string(task.Status),
			"keyspace_start":               task.KeyspaceStart,
			"keyspace_end":                 task.KeyspaceEnd,
			"keyspace_processed":           task.KeyspaceProcessed,
			"effective_keyspace_start":     task.EffectiveKeyspaceStart,
			"effective_keyspace_end":       task.EffectiveKeyspaceEnd,
			"effective_keyspace_processed": task.EffectiveKeyspaceProcessed,
			"rule_start_index":             task.RuleStartIndex,
			"rule_end_index":               task.RuleEndIndex,
			"is_rule_split_task":           task.IsRuleSplitTask,
			"progress_percent":             taskProgressPercent,
			"crack_count":                  task.CrackCount,
			"retry_count":                  task.RetryCount,
			"detailed_status":              task.DetailedStatus,
			"error_message":                task.ErrorMessage,
			"created_at":                   task.CreatedAt.Format(time.RFC3339),
		}

		if task.StartedAt != nil {
			taskSummary["started_at"] = task.StartedAt.Format(time.RFC3339)
		}
		if task.CompletedAt != nil {
			taskSummary["completed_at"] = task.CompletedAt.Format(time.RFC3339)
		}
		if task.BenchmarkSpeed != nil {
			taskSummary["benchmark_speed"] = *task.BenchmarkSpeed
		}
		if task.AverageSpeed != nil {
			taskSummary["average_speed"] = *task.AverageSpeed
		}
		if task.CrackingCompletedAt != nil {
			taskSummary["cracking_completed_at"] = task.CrackingCompletedAt.Format(time.RFC3339)
		}
		taskSummary["expected_crack_count"] = task.ExpectedCrackCount
		taskSummary["received_crack_count"] = task.ReceivedCrackCount

		taskSummaries = append(taskSummaries, taskSummary)
	}

	// No need for separate active tasks - frontend will filter from all tasks

	// Calculate overall progress percentage
	overallProgressPercent := 0.0
	if totalKeyspace != nil && *totalKeyspace > 0 {
		overallProgressPercent = float64(job.ProcessedKeyspace) / float64(*totalKeyspace) * 100
		if overallProgressPercent > 100 {
			overallProgressPercent = 100
		}
	}

	// Prepare response
	response := map[string]interface{}{
		"id":                        jobID.String(),
		"name":                      getJobName(*job, hashlist),
		"hashlist_id":               job.HashlistID,
		"hashlist_name":             hashlist.Name,
		"status":                    string(job.Status),
		"priority":                  job.Priority,
		"max_agents":                job.MaxAgents,
		"chunk_size_seconds":        job.ChunkSizeSeconds,
		"attack_mode":               job.AttackMode,
		"hash_type":                 formattedHashType,
		"total_keyspace":            job.TotalKeyspace,
		"effective_keyspace":        job.EffectiveKeyspace,
		"base_keyspace":             job.BaseKeyspace,
		"processed_keyspace":        job.ProcessedKeyspace,
		"dispatched_keyspace":       job.DispatchedKeyspace,
		"dispatched_percent":        dispatchedPercent,
		"searched_percent":          searchedPercent,
		"overall_progress_percent":  overallProgressPercent,
		"multiplication_factor":     job.MultiplicationFactor,
		"uses_rule_splitting":       job.UsesRuleSplitting,
		"increment_mode":            job.IncrementMode,
		"increment_min":             job.IncrementMin,
		"increment_max":             job.IncrementMax,
		"cracked_count":             crackedCount,
		"agent_count":               agentCount,
		"total_speed":               totalSpeed,
		"created_at":                job.CreatedAt.Format(time.RFC3339),
		"updated_at":                job.UpdatedAt.Format(time.RFC3339),
		"tasks":                     taskSummaries,
		"total_tasks":               totalTasks,
		"wordlist_ids":              job.WordlistIDs,
		"wordlist_names":            wordlistNames,
		"rule_ids":                  job.RuleIDs,
		"rule_names":                ruleNames,
		"mask":                      job.Mask,
	}

	if job.StartedAt != nil {
		response["started_at"] = job.StartedAt.Format(time.RFC3339)
	}
	if job.CompletedAt != nil {
		response["completed_at"] = job.CompletedAt.Format(time.RFC3339)
	}
	if job.CrackingCompletedAt != nil {
		response["cracking_completed_at"] = job.CrackingCompletedAt.Format(time.RFC3339)
	}
	if job.ErrorMessage != nil {
		response["error_message"] = *job.ErrorMessage
	}

	// Add preset job details if available
	if job.PresetJobID != nil {
		presetJob, err := h.presetJobRepo.GetByID(ctx, *job.PresetJobID)
		if err == nil {
			response["preset_job"] = map[string]interface{}{
				"id":   presetJob.ID.String(),
				"name": presetJob.Name,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// UpdateJob handles PATCH /api/jobs/{id}
func (h *UserJobsHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	var update struct {
		Priority         *int `json:"priority"`
		MaxAgents        *int `json:"max_agents"`
		ChunkSizeSeconds *int `json:"chunk_size_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Track what was updated for the response message
	var updatedFields []string

	// Update job execution
	if update.Priority != nil {
		// Validate priority against system settings
		maxPriority, err := h.systemSettingsRepo.GetMaxJobPriority(ctx)
		if err != nil {
			debug.Error("Failed to get max priority setting: %v", err)
			http.Error(w, "Failed to validate priority", http.StatusInternalServerError)
			return
		}

		if *update.Priority < 0 || *update.Priority > maxPriority {
			http.Error(w, fmt.Sprintf("Priority must be between 0 and %d", maxPriority), http.StatusBadRequest)
			return
		}

		if err := h.jobExecRepo.UpdatePriority(ctx, jobID, *update.Priority); err != nil {
			debug.Error("Failed to update job priority: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		updatedFields = append(updatedFields, "priority")
	}

	if update.MaxAgents != nil {
		if err := h.jobExecRepo.UpdateMaxAgents(ctx, jobID, *update.MaxAgents); err != nil {
			debug.Error("Failed to update job max agents: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		updatedFields = append(updatedFields, "max agents")
	}

	if update.ChunkSizeSeconds != nil {
		// Validate chunk size
		if *update.ChunkSizeSeconds < 5 {
			http.Error(w, "Chunk size must be at least 5 seconds", http.StatusBadRequest)
			return
		}
		if *update.ChunkSizeSeconds > 86400 {
			http.Error(w, "Chunk size cannot exceed 24 hours (86400 seconds)", http.StatusBadRequest)
			return
		}

		if err := h.jobExecRepo.UpdateChunkSizeSeconds(ctx, jobID, *update.ChunkSizeSeconds); err != nil {
			debug.Error("Failed to update job chunk size: %v", err)
			http.Error(w, "Failed to update chunk size", http.StatusInternalServerError)
			return
		}
		updatedFields = append(updatedFields, "chunk size")
	}

	responseMessage := "Job updated successfully"
	if len(updatedFields) > 0 && update.ChunkSizeSeconds != nil {
		responseMessage = "Job updated successfully. Chunk size changes will take effect on next task creation."
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": responseMessage,
	})
}

// RetryJob handles POST /api/jobs/{id}/retry
func (h *UserJobsHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get job %s: %v", jobID, err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Check if job can be retried
	if job.Status != models.JobExecutionStatusFailed &&
		job.Status != models.JobExecutionStatusCancelled {
		http.Error(w, "Job can only be retried if it's failed or cancelled", http.StatusBadRequest)
		return
	}

	// Reset the job to pending status
	if err := h.jobExecRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending); err != nil {
		debug.Error("Failed to reset job status: %v", err)
		http.Error(w, "Failed to retry job", http.StatusInternalServerError)
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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Job retry initiated successfully",
	})
}

// RetryTask handles POST /api/jobs/{id}/tasks/{taskId}/retry
func (h *UserJobsHandler) RetryTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(vars["taskId"])
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}

	// Verify the task belongs to this job
	task, err := h.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		debug.Error("Failed to get task %s: %v", taskID, err)
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	if task.JobExecutionID != jobID {
		http.Error(w, "Task does not belong to this job", http.StatusBadRequest)
		return
	}

	// Check if task is in a state that can be retried
	if task.Status != models.JobTaskStatusFailed {
		http.Error(w, "Only failed tasks can be retried", http.StatusBadRequest)
		return
	}

	// Use the proper ResetTaskForRetry method which handles everything correctly:
	// - Sets status to 'pending' and clears agent assignment
	// - Clears error_message
	// - Increments retry_count
	// - Resets progress fields
	// - Adjusts keyspace in transaction
	if err := h.jobTaskRepo.ResetTaskForRetry(ctx, taskID); err != nil {
		debug.Error("Failed to reset task for retry: %v", err)
		http.Error(w, "Failed to retry task", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Task retry initiated successfully",
		"task_id": taskID.String(),
	})
}

// ForceCompleteJob handles POST /api/jobs/{id}/force-complete
// This is a temporary endpoint to force complete jobs that are stuck
func (h *UserJobsHandler) ForceCompleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job to verify it exists
	_, err = h.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get job %s: %v", jobID, err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Get all tasks for this job
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for job %s: %v", jobID, err)
		http.Error(w, "Failed to get tasks", http.StatusInternalServerError)
		return
	}

	// Mark all non-completed/non-failed tasks as completed
	completedCount := 0
	for _, task := range tasks {
		if task.Status != models.JobTaskStatusCompleted && task.Status != models.JobTaskStatusFailed {
			if err := h.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCompleted); err != nil {
				debug.Error("Failed to update task %s status: %v", task.ID, err)
				continue
			}
			completedCount++
		}
	}

	// Update job status to completed
	if err := h.jobExecRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusCompleted); err != nil {
		debug.Error("Failed to update job status: %v", err)
		http.Error(w, "Failed to complete job", http.StatusInternalServerError)
		return
	}

	debug.Info("Force completed job %s: %d tasks marked as completed", jobID, completedCount)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":         fmt.Sprintf("Job force completed successfully. %d tasks marked as completed.", completedCount),
		"tasks_completed": completedCount,
	})
}

// stopAgentTasks sends stop signals to all agents working on tasks for a job
func (h *UserJobsHandler) stopAgentTasks(ctx context.Context, jobID uuid.UUID) error {
	// Get all tasks for this job
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for job %s: %v", jobID, err)
		return err
	}

	// Send stop signals to agents working on active tasks
	stoppedCount := 0
	for _, task := range tasks {
		// Only send stop signals for running or assigned tasks
		if task.AgentID != nil && (task.Status == models.JobTaskStatusRunning || task.Status == models.JobTaskStatusAssigned) {
			// Create stop message payload
			stopPayload := map[string]string{
				"task_id": task.ID.String(),
			}
			payloadJSON, err := json.Marshal(stopPayload)
			if err != nil {
				debug.Error("Failed to marshal stop payload for task %s: %v", task.ID, err)
				continue
			}

			// Create the WebSocket message
			stopMsg := map[string]interface{}{
				"type":    "job_stop",
				"payload": json.RawMessage(payloadJSON),
			}

			// Send stop signal to the agent
			if h.wsHandler != nil {
				if err := h.wsHandler.SendMessage(*task.AgentID, stopMsg); err != nil {
					debug.Error("Failed to send stop signal to agent %d for task %s: %v", *task.AgentID, task.ID, err)
				} else {
					debug.Info("Sent stop signal to agent %d for task %s", *task.AgentID, task.ID)
					stoppedCount++
				}
			} else {
				debug.Warning("WebSocket handler not available, cannot send stop signal to agent %d", *task.AgentID)
			}

			// Update task status to cancelled
			if err := h.jobTaskRepo.UpdateStatus(ctx, task.ID, models.JobTaskStatusCancelled); err != nil {
				debug.Error("Failed to update task %s status to cancelled: %v", task.ID, err)
			}
		}
	}

	debug.Info("Sent stop signals for %d tasks of job %s", stoppedCount, jobID)
	return nil
}

// DeleteJob handles DELETE /api/jobs/{id}
func (h *UserJobsHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Stop all agents working on this job's tasks
	if err := h.stopAgentTasks(ctx, jobID); err != nil {
		debug.Error("Failed to stop agent tasks for job %s: %v", jobID, err)
		// Continue with deletion even if we couldn't stop all tasks
	}

	// Delete job execution record (cascade deletes tasks)
	if err := h.jobExecRepo.Delete(ctx, jobID); err != nil {
		debug.Error("Failed to delete job %s: %v", jobID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully deleted job %s", jobID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Job deleted successfully",
	})
}

// DeleteFinishedJobs handles DELETE /api/jobs/finished
func (h *UserJobsHandler) DeleteFinishedJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Delete all completed jobs
	deletedCount, err := h.jobExecRepo.DeleteFinished(ctx)
	if err != nil {
		debug.Error("Failed to delete finished jobs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       "Finished jobs deleted successfully",
		"deleted_count": deletedCount,
	})
}

// GetAvailablePresetJobs handles GET /api/hashlists/{id}/available-jobs
func (h *UserJobsHandler) GetAvailablePresetJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	hashlistID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}

	// Verify the hashlist exists
	_, err = h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", hashlistID, err)
		http.Error(w, "Hashlist not found", http.StatusNotFound)
		return
	}

	// Get all preset jobs
	presetJobs, err := h.presetJobRepo.List(ctx)
	if err != nil {
		debug.Error("Failed to list preset jobs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get all workflows
	workflows, err := h.workflowRepo.ListWorkflows(ctx)
	if err != nil {
		debug.Error("Failed to list workflows: %v", err)
		// Don't fail, just log and continue with empty workflows
		workflows = []models.JobWorkflow{}
	}

	// Get wordlists, rules, and binary versions for form data
	wordlists, err := h.wordlistStore.ListWordlists(ctx, map[string]interface{}{})
	if err != nil {
		debug.Error("Failed to list wordlists: %v", err)
		wordlists = []*models.Wordlist{}
	}

	rules, err := h.ruleStore.ListRules(ctx, nil)
	if err != nil {
		debug.Error("Failed to list rules: %v", err)
		rules = []*models.Rule{}
	}

	binaries, err := h.binaryStore.ListVersions(ctx, map[string]interface{}{"is_active": true})
	if err != nil {
		debug.Error("Failed to list binaries: %v", err)
		binaries = []*binary.BinaryVersion{}
	}

	// Format preset jobs
	availableJobs := make([]map[string]interface{}, 0)
	for _, job := range presetJobs {
		availableJobs = append(availableJobs, map[string]interface{}{
			"id":                           job.ID.String(),
			"name":                         job.Name,
			"priority":                     job.Priority,
			"attack_mode":                  job.AttackMode,
			"wordlist_ids":                 job.WordlistIDs,
			"rule_ids":                     job.RuleIDs,
			"mask":                         job.Mask,
			"allow_high_priority_override": job.AllowHighPriorityOverride,
		})
	}

	// Format workflows with their steps
	formattedWorkflows := make([]map[string]interface{}, 0)
	for _, workflow := range workflows {
		// Get workflow steps
		steps, err := h.workflowRepo.GetWorkflowSteps(ctx, workflow.ID)
		if err != nil {
			debug.Error("Failed to get workflow steps for %s: %v", workflow.ID, err)
			steps = []models.JobWorkflowStep{}
		}

		// Format steps with preset job names and check for high priority override
		formattedSteps := make([]map[string]interface{}, 0)
		hasHighPriorityOverride := false
		for _, step := range steps {
			// Get preset job for this step
			presetJob, err := h.presetJobRepo.GetByID(ctx, step.PresetJobID)
			stepData := map[string]interface{}{
				"id":            step.ID,
				"preset_job_id": step.PresetJobID.String(),
				"step_order":    step.StepOrder,
			}
			if err == nil && presetJob != nil {
				stepData["preset_job_name"] = presetJob.Name
				stepData["allow_high_priority_override"] = presetJob.AllowHighPriorityOverride
				// Check if this preset job has high priority override
				if presetJob.AllowHighPriorityOverride {
					hasHighPriorityOverride = true
				}
			}
			formattedSteps = append(formattedSteps, stepData)
		}

		formattedWorkflows = append(formattedWorkflows, map[string]interface{}{
			"id":                        workflow.ID.String(),
			"name":                      workflow.Name,
			"steps":                     formattedSteps,
			"has_high_priority_override": hasHighPriorityOverride,
		})
	}

	// Format form data
	formData := map[string]interface{}{
		"wordlists":       formatWordlists(wordlists),
		"rules":           formatRules(rules),
		"binary_versions": formatBinaries(binaries),
	}

	// Return the expected structure
	response := map[string]interface{}{
		"preset_jobs": availableJobs,
		"workflows":   formattedWorkflows,
		"form_data":   formData,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper functions to format data
func formatWordlists(wordlists []*models.Wordlist) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(wordlists))
	for _, w := range wordlists {
		result = append(result, map[string]interface{}{
			"id":        w.ID,
			"name":      w.Name,
			"file_size": w.FileSize,
		})
	}
	return result
}

func formatRules(rules []*models.Rule) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(rules))
	for _, r := range rules {
		result = append(result, map[string]interface{}{
			"id":         r.ID,
			"name":       r.Name,
			"rule_count": r.RuleCount,
		})
	}
	return result
}

func formatBinaries(binaries []*binary.BinaryVersion) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(binaries))
	for _, b := range binaries {
		result = append(result, map[string]interface{}{
			"id":      b.ID,
			"version": b.FileName,
			"type":    string(b.BinaryType),
		})
	}
	return result
}

// ListUserJobs handles GET /api/user/jobs with pagination and filtering (filtered by authenticated user)
func (h *UserJobsHandler) ListUserJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, ok := ctx.Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 25
	}

	// Parse filters
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
	filter := repository.JobFilter{
		Status:   &status,
		Priority: priority,
		Search:   &search,
		UserID:   &userID,
	}

	// Get jobs with filters and user information
	jobsWithUser, err := h.jobExecRepo.ListWithFiltersAndUser(ctx, pageSize, (page-1)*pageSize, filter)
	if err != nil {
		debug.Error("Failed to list user jobs: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get total count with filters
	total, err := h.jobExecRepo.GetFilteredCount(ctx, filter)
	if err != nil {
		debug.Error("Failed to get user job count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get status counts for this user
	statusCounts, err := h.jobExecRepo.GetStatusCountsForUser(ctx, userID)
	if err != nil {
		debug.Error("Failed to get user status counts: %v", err)
		// Don't fail the request, just log the error
		statusCounts = make(map[string]int)
	}

	// Convert to job summaries (reuse the same logic as ListJobs)
	summaries := make([]JobSummary, 0, len(jobsWithUser))
	for _, jobWithUser := range jobsWithUser {
		job := jobWithUser.JobExecution
		// Get hashlist details including cracked count
		hashlist, err := h.hashlistRepo.GetByID(ctx, job.HashlistID)
		if err != nil {
			debug.Error("Failed to get hashlist %d: %v", job.HashlistID, err)
			continue
		}

		// Get task statistics
		tasks, err := h.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
		if err != nil {
			debug.Error("Failed to get tasks for job %s: %v", job.ID, err)
			tasks = []models.JobTask{}
		}

		// Calculate metrics
		var agentCount int
		var totalSpeed int64
		var crackedCount int
		var keyspaceSearched int64
		// Note: keyspaceDispatched removed - use job.DispatchedKeyspace instead (calculated by job_progress_calculation_service)

		for _, task := range tasks {
			if task.Status == models.JobTaskStatusRunning {
				agentCount++
				if task.BenchmarkSpeed != nil {
					totalSpeed += *task.BenchmarkSpeed
				}
			}
			crackedCount += task.CrackCount
			keyspaceSearched += task.KeyspaceProcessed
		}

		// Calculate percentages using effective keyspace when available
		dispatchedPercent := 0.0
		searchedPercent := 0.0
		overallProgressPercent := 0.0

		// Use effective keyspace if available, otherwise fall back to total keyspace
		var keyspaceForProgress int64
		if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
			keyspaceForProgress = *job.EffectiveKeyspace
		} else if job.TotalKeyspace != nil && *job.TotalKeyspace > 0 {
			keyspaceForProgress = *job.TotalKeyspace
		} else {
			keyspaceForProgress = 0
		}

		if keyspaceForProgress > 0 {
			// For dispatched percentage, we need to consider rule splitting
			if job.UsesRuleSplitting {
				// For rule split jobs, calculate based on effective keyspace
				var totalEffectiveDispatched int64 = 0
				var totalEffectiveSearched int64 = 0

				for _, task := range tasks {
					// Calculate dispatched effective keyspace for non-pending tasks
					if task.Status != models.JobTaskStatusPending {
						if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
							totalEffectiveDispatched += (*task.EffectiveKeyspaceEnd - *task.EffectiveKeyspaceStart)
						}
					}
					
					// Calculate searched effective keyspace from all tasks
					if task.EffectiveKeyspaceProcessed != nil {
						totalEffectiveSearched += *task.EffectiveKeyspaceProcessed
					}
				}

				// Both percentages are relative to total effective keyspace
				if keyspaceForProgress > 0 {
					searchedPercent = float64(totalEffectiveSearched) / float64(keyspaceForProgress) * 100
					dispatchedPercent = float64(totalEffectiveDispatched) / float64(keyspaceForProgress) * 100
				}
			} else {
				// For keyspace-based jobs (including keyspace-split jobs)
				// Use the pre-calculated dispatched_keyspace from job_progress_calculation_service
				// which correctly handles effective keyspace for keyspace-split jobs
				searchedPercent = float64(job.ProcessedKeyspace) / float64(keyspaceForProgress) * 100
				dispatchedPercent = float64(job.DispatchedKeyspace) / float64(keyspaceForProgress) * 100
			}

			// Cap percentages at 100%
			if dispatchedPercent > 100 {
				dispatchedPercent = 100
			}
			if searchedPercent > 100 {
				searchedPercent = 100
			}

			// Overall progress is the searched percentage
			overallProgressPercent = searchedPercent
		}

		// Use the backend-calculated overall progress if available and more accurate
		if job.OverallProgressPercent > 0 {
			overallProgressPercent = job.OverallProgressPercent
			// For consistency, use this for searched percentage too
			searchedPercent = overallProgressPercent
		}

		summary := JobSummary{
			ID:                     job.ID.String(),
			Name:                   getJobName(job, hashlist),
			HashlistID:             job.HashlistID,
			HashlistName:           hashlist.Name,
			Status:                 string(job.Status),
			Priority:               job.Priority,
			MaxAgents:              job.MaxAgents,
			DispatchedPercent:      dispatchedPercent,
			SearchedPercent:        searchedPercent,
			CrackedCount:           crackedCount,
			AgentCount:             agentCount,
			TotalSpeed:             totalSpeed,
			CreatedAt:              job.CreatedAt.Format(time.RFC3339),
			UpdatedAt:              job.UpdatedAt.Format(time.RFC3339),
			ErrorMessage:           job.ErrorMessage,
			CreatedByUsername:      jobWithUser.CreatedByUsername,
			TotalKeyspace:          job.TotalKeyspace,
			EffectiveKeyspace:      job.EffectiveKeyspace,
			MultiplicationFactor:   job.MultiplicationFactor,
			UsesRuleSplitting:      job.UsesRuleSplitting,
			ProcessedKeyspace:      &job.ProcessedKeyspace,
			DispatchedKeyspace:     &job.DispatchedKeyspace,
			OverallProgressPercent: overallProgressPercent,
		}

		// Add completed time if present
		if job.CompletedAt != nil {
			completedAtStr := job.CompletedAt.Format(time.RFC3339)
			summary.CompletedAt = &completedAtStr
		}

		summaries = append(summaries, summary)
	}

	// Create response
	response := map[string]interface{}{
		"jobs":          summaries,
		"total":         total,
		"page":          page,
		"page_size":     pageSize,
		"total_pages":   (total + pageSize - 1) / pageSize,
		"status_counts": statusCounts,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// GetJobLayers returns all increment layers for a job with statistics
func (h *UserJobsHandler) GetJobLayers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobIDStr := vars["id"]

	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	// Get the job to verify it exists and user has access
	job, err := h.jobExecRepo.GetByID(r.Context(), jobID)
	if err != nil {
		debug.Error("Failed to get job: %v", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Check if this is an increment mode job
	if job.IncrementMode == "" || job.IncrementMode == "off" {
		http.Error(w, "Job is not an increment mode job", http.StatusBadRequest)
		return
	}

	// Get layers with stats
	layers, err := h.jobIncrementLayerRepo.GetLayersWithStats(r.Context(), jobID)
	if err != nil {
		debug.Error("Failed to get job layers: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(layers); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// GetJobLayerTasks returns all tasks for a specific increment layer
func (h *UserJobsHandler) GetJobLayerTasks(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobIDStr := vars["id"]
	layerIDStr := vars["layer_id"]

	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	layerID, err := uuid.Parse(layerIDStr)
	if err != nil {
		http.Error(w, "Invalid layer ID", http.StatusBadRequest)
		return
	}

	// Verify the layer belongs to this job
	layer, err := h.jobIncrementLayerRepo.GetByID(r.Context(), layerID)
	if err != nil {
		debug.Error("Failed to get layer: %v", err)
		http.Error(w, "Layer not found", http.StatusNotFound)
		return
	}

	if layer.JobExecutionID != jobID {
		http.Error(w, "Layer does not belong to this job", http.StatusBadRequest)
		return
	}

	// Get all tasks for the job
	tasks, err := h.jobTaskRepo.GetTasksByJobExecution(r.Context(), jobID)
	if err != nil {
		debug.Error("Failed to get tasks: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
	if err := json.NewEncoder(w).Encode(layerTasks); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
