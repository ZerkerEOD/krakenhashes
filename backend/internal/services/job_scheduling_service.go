package services

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobWebSocketIntegration interface for WebSocket integration
type JobWebSocketIntegration interface {
	SendJobAssignment(ctx context.Context, task *models.JobTask, jobExecution *models.JobExecution) error
	RequestAgentBenchmark(ctx context.Context, agentID int, jobExecution *models.JobExecution, layerID *uuid.UUID, layerMask string) error
	SendJobStop(ctx context.Context, taskID uuid.UUID, reason string) error
	SyncAgentFiles(ctx context.Context, agentID int, timeout time.Duration) error
	// CheckAgentFilesForJob checks if agent has required files for a job and triggers download if missing.
	// Returns true if agent has all files (ready for benchmark), false if agent needs to download.
	// This is non-blocking - if files are missing, it triggers async download and returns false.
	CheckAgentFilesForJob(ctx context.Context, agentID int, jobExecution *models.JobExecution, timeout time.Duration) (bool, error)
}

// JobSchedulingService handles the assignment of jobs to agents
type JobSchedulingService struct {
	jobExecutionService *JobExecutionService
	jobChunkingService  *JobChunkingService
	hashlistSyncService *HashlistSyncService
	agentRepo           *repository.AgentRepository
	jobTaskRepo         *repository.JobTaskRepository
	systemSettingsRepo  *repository.SystemSettingsRepository
	wsIntegration       JobWebSocketIntegration

	// Scheduling state
	schedulingMutex sync.Mutex
	isScheduling    bool

	// Agent reservation system
	reservedAgents   map[int]uuid.UUID // agentID -> jobID
	reservationMutex sync.RWMutex
}

// NewJobSchedulingService creates a new job scheduling service
func NewJobSchedulingService(
	jobExecutionService *JobExecutionService,
	jobChunkingService *JobChunkingService,
	hashlistSyncService *HashlistSyncService,
	agentRepo *repository.AgentRepository,
	jobTaskRepo *repository.JobTaskRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *JobSchedulingService {
	return &JobSchedulingService{
		jobExecutionService: jobExecutionService,
		jobChunkingService:  jobChunkingService,
		hashlistSyncService: hashlistSyncService,
		agentRepo:           agentRepo,
		jobTaskRepo:         jobTaskRepo,
		systemSettingsRepo:  systemSettingsRepo,
		reservedAgents:      make(map[int]uuid.UUID),
	}
}

// ScheduleJobsResult contains the result of a scheduling operation
type ScheduleJobsResult struct {
	AssignedTasks   []models.JobTask
	InterruptedJobs []uuid.UUID
	Errors          []error
}

// JobAllocation represents the allocation decision for a job
type JobAllocation struct {
	JobID        uuid.UUID
	AgentCount   int
	ActiveAgents int
	MaxAgents    int
	Priority     int
}

// JobCompatibilityInfo tracks which agents can run a specific job
type JobCompatibilityInfo struct {
	JobID            uuid.UUID
	BinaryVersion    string
	CompatibleAgents []int // Agent IDs that can run this job
	ConstraintScore  int   // Lower = more constrained (fewer compatible agents)
}

// AgentCompatibilityInfo tracks which jobs an agent can run
type AgentCompatibilityInfo struct {
	AgentID          int
	BinaryVersion    string
	CompatibleJobs   []uuid.UUID // Job IDs this agent can run
	FlexibilityScore int         // Higher = more flexible (more compatible jobs)
}

// CompatibilityMatrix holds the bidirectional compatibility mappings
type CompatibilityMatrix struct {
	JobInfo   map[uuid.UUID]*JobCompatibilityInfo
	AgentInfo map[int]*AgentCompatibilityInfo
}

// buildCompatibilityMatrix creates the compatibility mappings between agents and jobs
// Uses version.IsCompatibleStr(agentPattern, jobPattern) to determine compatibility
func buildCompatibilityMatrix(
	agents []models.Agent,
	jobs []models.JobExecutionWithWork,
) *CompatibilityMatrix {
	matrix := &CompatibilityMatrix{
		JobInfo:   make(map[uuid.UUID]*JobCompatibilityInfo),
		AgentInfo: make(map[int]*AgentCompatibilityInfo),
	}

	// Initialize job info
	for _, job := range jobs {
		matrix.JobInfo[job.ID] = &JobCompatibilityInfo{
			JobID:            job.ID,
			BinaryVersion:    job.BinaryVersion,
			CompatibleAgents: []int{},
			ConstraintScore:  0,
		}
	}

	// Initialize agent info
	for _, agent := range agents {
		matrix.AgentInfo[agent.ID] = &AgentCompatibilityInfo{
			AgentID:          agent.ID,
			BinaryVersion:    agent.BinaryVersion,
			CompatibleJobs:   []uuid.UUID{},
			FlexibilityScore: 0,
		}
	}

	// Build compatibility mappings
	for _, agent := range agents {
		for _, job := range jobs {
			if version.IsCompatibleStr(agent.BinaryVersion, job.BinaryVersion) {
				// Add to job's compatible agents
				matrix.JobInfo[job.ID].CompatibleAgents = append(
					matrix.JobInfo[job.ID].CompatibleAgents,
					agent.ID,
				)
				// Add to agent's compatible jobs
				matrix.AgentInfo[agent.ID].CompatibleJobs = append(
					matrix.AgentInfo[agent.ID].CompatibleJobs,
					job.ID,
				)
			}
		}
	}

	// Calculate scores
	for jobID, info := range matrix.JobInfo {
		info.ConstraintScore = len(info.CompatibleAgents)
		matrix.JobInfo[jobID] = info
	}
	for agentID, info := range matrix.AgentInfo {
		info.FlexibilityScore = len(info.CompatibleJobs)
		matrix.AgentInfo[agentID] = info
	}

	debug.Log("Built compatibility matrix", map[string]interface{}{
		"total_agents": len(agents),
		"total_jobs":   len(jobs),
	})

	return matrix
}

// getCompatibleAgentCount returns the number of compatible agents for a job from the remaining pool
func (m *CompatibilityMatrix) getCompatibleAgentCount(jobID uuid.UUID, remainingAgents map[int]bool) int {
	jobInfo, exists := m.JobInfo[jobID]
	if !exists {
		return 0
	}

	count := 0
	for _, agentID := range jobInfo.CompatibleAgents {
		if remainingAgents[agentID] {
			count++
		}
	}
	return count
}

// getCompatibleAgents returns compatible agents for a job from the remaining pool, sorted by flexibility (specialists first)
func (m *CompatibilityMatrix) getCompatibleAgents(jobID uuid.UUID, remainingAgents map[int]bool) []int {
	jobInfo, exists := m.JobInfo[jobID]
	if !exists {
		return []int{}
	}

	compatible := []int{}
	for _, agentID := range jobInfo.CompatibleAgents {
		if remainingAgents[agentID] {
			compatible = append(compatible, agentID)
		}
	}

	// Sort by flexibility score ASC (specialists first - agents with fewer compatible jobs)
	sort.Slice(compatible, func(i, j int) bool {
		return m.AgentInfo[compatible[i]].FlexibilityScore < m.AgentInfo[compatible[j]].FlexibilityScore
	})

	return compatible
}

// expandIncrementJobsIntoLayers converts increment jobs into multiple entries (one per layer)
// while preserving parent job's priority and max_agents for shared allocation
func (s *JobSchedulingService) expandIncrementJobsIntoLayers(
	ctx context.Context,
	jobs []models.JobExecutionWithWork,
) []models.JobExecutionWithWork {
	expanded := []models.JobExecutionWithWork{}

	for _, job := range jobs {
		// Check if this is an increment mode job
		if job.IncrementMode != "" && job.IncrementMode != "off" {
			// Get layers with pending work
			layers, err := s.jobExecutionService.jobIncrementLayerRepo.GetLayersWithPendingWork(ctx, job.ID)
			if err != nil || len(layers) == 0 {
				// No layers or error - keep original job
				expanded = append(expanded, job)
				continue
			}

			// Create separate entry for each layer with pending work
			layerCount := 0
			for _, layer := range layers {
				if layer.Status == models.JobIncrementLayerStatusCompleted {
					continue
				}

				// Skip layers without accurate keyspace - need benchmark first
				if !layer.IsAccurateKeyspace {
					debug.Log("Skipping layer - needs benchmark first", map[string]interface{}{
						"layer_id":      layer.ID,
						"layer_index":   layer.LayerIndex,
						"parent_job_id": job.ID,
					})
					continue
				}

				// Check if layer has undispatched work
				// Use BaseKeyspace for comparison since DispatchedKeyspace tracks base keyspace (for --skip/--limit)
				var layerBaseKeyspace *int64
				if layer.BaseKeyspace != nil {
					layerBaseKeyspace = layer.BaseKeyspace
				} else if layer.EffectiveKeyspace != nil {
					layerBaseKeyspace = layer.EffectiveKeyspace
				}
				if layerBaseKeyspace != nil && layer.DispatchedKeyspace < *layerBaseKeyspace {
					// Create virtual job entry for this layer
					layerJob := job // Copy parent job

					// Override with layer-specific values
					layerJob.ID = layer.ID // Use layer ID as "job" ID for allocation map
					layerJob.BaseKeyspace = layerBaseKeyspace
					layerJob.DispatchedKeyspace = layer.DispatchedKeyspace

					// Keep parent's priority, max_agents, created_at for correct allocation
					// These are already in layerJob from the copy

					expanded = append(expanded, layerJob)
					layerCount++

					debug.Log("Expanded increment layer as schedulable unit", map[string]interface{}{
						"parent_job_id": job.ID,
						"layer_id":      layer.ID,
						"layer_index":   layer.LayerIndex,
						"layer_mask":    layer.Mask,
						"priority":      layerJob.Priority,
						"max_agents":    layerJob.MaxAgents,
					})
				}
			}

			// If no layers were added, keep the original job for benchmark planning
			// Task creation will be blocked in CreateTaskAssignmentPlans for increment jobs
			// without a specific layer target (defense against increment_layer_id = NULL bug)
			if layerCount == 0 {
				debug.Log("Increment job has no schedulable layers - keeping for benchmark planning", map[string]interface{}{
					"job_id":      job.ID,
					"layer_count": len(layers),
				})
				expanded = append(expanded, job)
			}
		} else {
			// Regular job - keep as-is
			expanded = append(expanded, job)
		}
	}

	debug.Log("Layer expansion complete", map[string]interface{}{
		"original_count": len(jobs),
		"expanded_count": len(expanded),
	})

	return expanded
}

// CalculateAgentAllocation determines which jobs should receive which agents
// based on priority-aware max_agents rules with binary version compatibility:
// 1. Higher priority jobs get ALL available COMPATIBLE agents (max_agents ignored)
// 2. Same priority jobs respect max_agents up to their limit (compatible agents only)
// 3. Overflow agents at same priority use configurable mode (FIFO or round-robin)
// 4. Jobs with no compatible agents get 0 allocation and stay pending
func (s *JobSchedulingService) CalculateAgentAllocation(
	ctx context.Context,
	availableAgents []models.Agent,
	jobsWithWork []models.JobExecutionWithWork,
) (map[uuid.UUID]int, *CompatibilityMatrix, error) {

	allocation := make(map[uuid.UUID]int)
	if len(availableAgents) == 0 || len(jobsWithWork) == 0 {
		return allocation, nil, nil
	}

	// Build compatibility matrix for filtering
	matrix := buildCompatibilityMatrix(availableAgents, jobsWithWork)

	// Track remaining agents as a map for efficient lookup and compatibility filtering
	remainingAgentMap := make(map[int]bool)
	for _, agent := range availableAgents {
		remainingAgentMap[agent.ID] = true
	}

	// Jobs are already sorted by priority DESC, created_at ASC from the SQL query
	// Group by priority level
	priorityGroups := make(map[int][]models.JobExecutionWithWork)
	priorities := []int{}

	for _, job := range jobsWithWork {
		if _, exists := priorityGroups[job.Priority]; !exists {
			priorities = append(priorities, job.Priority)
		}
		priorityGroups[job.Priority] = append(priorityGroups[job.Priority], job)
	}

	// Sort priorities in descending order (highest first)
	for i := 0; i < len(priorities); i++ {
		for j := i + 1; j < len(priorities); j++ {
			if priorities[i] < priorities[j] {
				priorities[i], priorities[j] = priorities[j], priorities[i]
			}
		}
	}

	debug.Log("Agent allocation: processing priority groups", map[string]interface{}{
		"available_agents": len(availableAgents),
		"total_jobs":       len(jobsWithWork),
		"priority_levels":  len(priorities),
	})

	// Helper to count remaining agents
	countRemainingAgents := func() int {
		count := 0
		for _, available := range remainingAgentMap {
			if available {
				count++
			}
		}
		return count
	}

	// Process each priority level from highest to lowest
	for _, priority := range priorities {
		jobs := priorityGroups[priority]

		remainingCount := countRemainingAgents()
		if remainingCount == 0 {
			break
		}

		debug.Log("Processing priority level", map[string]interface{}{
			"priority":         priority,
			"jobs_at_priority": len(jobs),
			"remaining_agents": remainingCount,
		})

		// Phase 1: Allocate up to max_agents for each job (only counting COMPATIBLE agents)
		// Track active agents by parent job for increment layers (they share max_agents)
		parentActiveAgents := make(map[uuid.UUID]int)

		for _, job := range jobs {
			// Determine if this is an increment layer entry and find parent job ID
			parentJobID := job.ID // default: self
			isIncrementLayer := false

			if job.IncrementMode != "" && job.IncrementMode != "off" {
				// This entry might represent a layer - check if ID is a layer ID
				layer, err := s.jobExecutionService.jobIncrementLayerRepo.GetByID(ctx, job.ID)
				if err == nil && layer != nil {
					parentJobID = layer.JobExecutionID
					isIncrementLayer = true
				}
			}

			// Get current active agents (shared across layers of same parent)
			currentActive := 0
			if isIncrementLayer {
				if tracked, exists := parentActiveAgents[parentJobID]; exists {
					// Use tracked count (includes allocations made in this cycle)
					currentActive = tracked
				} else {
					// First time seeing this parent - query actual active count from database
					// Active agents = count of tasks with status 'running' or 'assigned'
					activeCount, err := s.jobExecutionService.jobTaskRepo.GetActiveAgentCountByJob(ctx, parentJobID)
					if err == nil {
						currentActive = activeCount
						parentActiveAgents[parentJobID] = currentActive
					}
				}
			} else {
				currentActive = job.ActiveAgents
			}

			maxAllowed := job.MaxAgents
			if maxAllowed == 0 {
				maxAllowed = 999 // unlimited
			}

			needed := maxAllowed - currentActive

			// Count COMPATIBLE agents only for this job
			compatibleCount := matrix.getCompatibleAgentCount(job.ID, remainingAgentMap)

			// Allocate if job has undispatched keyspace and compatible agents available
			if needed > 0 && compatibleCount > 0 && s.hasUndispatchedWork(ctx, &job) {
				toAllocate := needed
				if toAllocate > compatibleCount {
					toAllocate = compatibleCount
				}

				allocation[job.ID] = toAllocate

				// Mark allocated agents as used (we'll do actual assignment in reserveAgentsForJobs)
				// For now, we just decrement the count - actual agent selection happens in reservation
				// Note: We DON'T remove agents from remainingAgentMap here because
				// the actual assignment happens in reserveAgentsForJobs with smart selection

				// Track allocation against parent for increment layers
				if isIncrementLayer {
					parentActiveAgents[parentJobID] += toAllocate
				}

				debug.Log("Allocated agents to job (phase 1)", map[string]interface{}{
					"job_id":           job.ID,
					"parent_job_id":    parentJobID,
					"is_layer":         isIncrementLayer,
					"job_name":         job.Name,
					"priority":         job.Priority,
					"active_agents":    currentActive,
					"max_agents":       maxAllowed,
					"compatible_count": compatibleCount,
					"allocated":        toAllocate,
				})
			} else if needed > 0 && compatibleCount == 0 && s.hasUndispatchedWork(ctx, &job) {
				// Job has work but no compatible agents - log and skip
				debug.Log("Job has no compatible agents - skipping", map[string]interface{}{
					"job_id":         job.ID,
					"job_name":       job.Name,
					"binary_version": job.BinaryVersion,
				})
			}
		}

		// Phase 2: Distribute overflow agents based on configured mode
		// Calculate total allocated at this priority vs total compatible agents available
		totalAllocatedThisPriority := 0
		for _, job := range jobs {
			totalAllocatedThisPriority += allocation[job.ID]
		}

		// Check if there are more agents than allocated (overflow situation)
		remainingCount = countRemainingAgents()
		if remainingCount > totalAllocatedThisPriority {
			overflowCount := remainingCount - totalAllocatedThisPriority
			s.distributeOverflowAgentsWithCompatibility(ctx, jobs, allocation, overflowCount, matrix, remainingAgentMap)

			// Priority override: If any job at this priority still has undispatched work
			// AND has compatible agents, don't process lower priorities
			hasWorkRemaining := false
			for _, job := range jobs {
				if s.hasUndispatchedWork(ctx, &job) {
					compatibleCount := matrix.getCompatibleAgentCount(job.ID, remainingAgentMap)
					if compatibleCount > 0 {
						hasWorkRemaining = true
						break
					}
				}
			}

			if hasWorkRemaining {
				debug.Log("Higher priority jobs have work with compatible agents - stopping allocation to lower priorities", map[string]interface{}{
					"priority":       priority,
					"jobs_with_work": len(jobs),
				})
				break // Don't process lower priority levels
			}
		}
	}

	debug.Log("Agent allocation completed", map[string]interface{}{
		"jobs_with_allocation": len(allocation),
		"unallocated_agents":   countRemainingAgents(),
	})

	return allocation, matrix, nil
}

// hasUndispatchedWork checks if a job still has keyspace/rules that haven't been dispatched
func (s *JobSchedulingService) hasUndispatchedWork(ctx context.Context, job *models.JobExecutionWithWork) bool {
	// Jobs in "pending" status always have work (they haven't started yet)
	if job.Status == "pending" {
		return true
	}

	if job.UsesRuleSplitting && job.EffectiveKeyspace != nil {
		// For rule chunking: check if dispatched keyspace < effective keyspace
		// This indicates more rule chunks need to be created
		return job.DispatchedKeyspace < *job.EffectiveKeyspace
	}

	// For keyspace splitting (--skip/--limit): query actual BASE keyspace dispatched from tasks
	// We can't use job.DispatchedKeyspace because it's tracked in EFFECTIVE units
	// which doesn't match the BASE keyspace units used for hashcat --skip/--limit
	if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
		// Query the maximum keyspace_end from tasks to get actual base keyspace dispatched
		maxBaseDispatched, err := s.jobTaskRepo.GetMaxKeyspaceEnd(ctx, job.ID)
		if err != nil {
			debug.Warning("Failed to get max keyspace_end for job %s: %v, assuming work remains", job.ID, err)
			return true // Fallback: assume work remains on error
		}

		hasWork := maxBaseDispatched < *job.BaseKeyspace
		debug.Log("Checked undispatched work for keyspace-split job", map[string]interface{}{
			"job_id":              job.ID,
			"max_base_dispatched": maxBaseDispatched,
			"base_keyspace":       *job.BaseKeyspace,
			"has_work":            hasWork,
		})
		return hasWork
	} else if job.EffectiveKeyspace != nil {
		// Fallback to EffectiveKeyspace for jobs without BaseKeyspace
		return job.DispatchedKeyspace < *job.EffectiveKeyspace
	}

	return false
}

// distributeOverflowAgents distributes extra agents beyond max_agents at the same priority level
func (s *JobSchedulingService) distributeOverflowAgents(
	ctx context.Context,
	jobs []models.JobExecutionWithWork,
	allocation map[uuid.UUID]int,
	remaining int,
) int {
	if remaining == 0 {
		return 0
	}

	// Get overflow mode from settings
	overflowMode := "fifo" // default
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_overflow_allocation_mode")
	if err == nil && setting.Value != nil {
		overflowMode = *setting.Value
	}

	debug.Log("Distributing overflow agents", map[string]interface{}{
		"mode":             overflowMode,
		"remaining_agents": remaining,
		"jobs_at_priority": len(jobs),
	})

	if overflowMode == "fifo" {
		// FIFO mode: Give all remaining agents to the oldest job with undispatched work
		// Jobs are already sorted by created_at ASC
		for _, job := range jobs {
			if s.hasUndispatchedWork(ctx, &job) {
				currentAllocation := allocation[job.ID]
				allocation[job.ID] = currentAllocation + remaining

				debug.Log("FIFO overflow: allocated all remaining to oldest job", map[string]interface{}{
					"job_id":             job.ID,
					"job_name":           job.Name,
					"previous_allocated": currentAllocation,
					"overflow_added":     remaining,
					"total_allocated":    allocation[job.ID],
				})

				return 0 // All agents allocated
			}
		}
	} else {
		// Round-robin mode: Distribute one agent at a time across all jobs with undispatched work
		for remaining > 0 {
			allocatedThisRound := false

			for _, job := range jobs {
				if s.hasUndispatchedWork(ctx, &job) && remaining > 0 {
					allocation[job.ID]++
					remaining--
					allocatedThisRound = true

					debug.Log("Round-robin overflow: allocated agent", map[string]interface{}{
						"job_id":         job.ID,
						"job_name":       job.Name,
						"total_allocated": allocation[job.ID],
						"remaining":      remaining,
					})

					if remaining == 0 {
						break
					}
				}
			}

			if !allocatedThisRound {
				// No job can take more agents
				debug.Log("Round-robin overflow: no jobs can accept more agents", map[string]interface{}{
					"remaining_agents": remaining,
				})
				break
			}
		}
	}

	return remaining
}

// distributeOverflowAgentsWithCompatibility distributes extra agents beyond max_agents
// with binary version compatibility filtering
func (s *JobSchedulingService) distributeOverflowAgentsWithCompatibility(
	ctx context.Context,
	jobs []models.JobExecutionWithWork,
	allocation map[uuid.UUID]int,
	overflowCount int,
	matrix *CompatibilityMatrix,
	remainingAgentMap map[int]bool,
) {
	if overflowCount == 0 {
		return
	}

	// Get overflow mode from settings
	overflowMode := "fifo" // default
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_overflow_allocation_mode")
	if err == nil && setting.Value != nil {
		overflowMode = *setting.Value
	}

	debug.Log("Distributing overflow agents with compatibility", map[string]interface{}{
		"mode":           overflowMode,
		"overflow_count": overflowCount,
		"jobs_at_priority": len(jobs),
	})

	if overflowMode == "fifo" {
		// FIFO mode: Give all overflow to the oldest job with undispatched work AND compatible agents
		// Jobs are already sorted by created_at ASC
		for _, job := range jobs {
			if s.hasUndispatchedWork(ctx, &job) {
				// Count compatible agents for this job
				compatibleCount := matrix.getCompatibleAgentCount(job.ID, remainingAgentMap)
				if compatibleCount > 0 {
					// Give all overflow to this job (up to compatible agent count)
					toAdd := overflowCount
					if toAdd > compatibleCount {
						toAdd = compatibleCount
					}

					currentAllocation := allocation[job.ID]
					allocation[job.ID] = currentAllocation + toAdd

					debug.Log("FIFO overflow with compatibility: allocated to oldest compatible job", map[string]interface{}{
						"job_id":             job.ID,
						"job_name":           job.Name,
						"previous_allocated": currentAllocation,
						"overflow_added":     toAdd,
						"total_allocated":    allocation[job.ID],
						"compatible_agents":  compatibleCount,
					})

					return // All overflow allocated
				}
				// Job has work but no compatible agents - skip to next job
				debug.Log("FIFO overflow: skipping job with no compatible agents", map[string]interface{}{
					"job_id":         job.ID,
					"job_name":       job.Name,
					"binary_version": job.BinaryVersion,
				})
			}
		}
	} else {
		// Round-robin mode: Distribute one agent at a time across jobs with compatible agents
		remaining := overflowCount
		for remaining > 0 {
			allocatedThisRound := false

			for _, job := range jobs {
				if s.hasUndispatchedWork(ctx, &job) && remaining > 0 {
					// Check if job has compatible agents
					compatibleCount := matrix.getCompatibleAgentCount(job.ID, remainingAgentMap)
					currentAllocation := allocation[job.ID]

					// Only allocate if there are enough compatible agents
					if compatibleCount > currentAllocation {
						allocation[job.ID]++
						remaining--
						allocatedThisRound = true

						debug.Log("Round-robin overflow with compatibility: allocated agent", map[string]interface{}{
							"job_id":            job.ID,
							"job_name":          job.Name,
							"total_allocated":   allocation[job.ID],
							"compatible_agents": compatibleCount,
							"remaining":         remaining,
						})

						if remaining == 0 {
							break
						}
					}
				}
			}

			if !allocatedThisRound {
				// No job can take more agents (either no work or no compatible agents)
				debug.Log("Round-robin overflow: no jobs can accept more compatible agents", map[string]interface{}{
					"remaining": remaining,
				})
				break
			}
		}
	}
}

// ScheduleJobs performs the main job scheduling logic
func (s *JobSchedulingService) ScheduleJobs(ctx context.Context) (*ScheduleJobsResult, error) {
	s.schedulingMutex.Lock()
	defer s.schedulingMutex.Unlock()

	if s.isScheduling {
		return nil, fmt.Errorf("scheduling already in progress")
	}

	s.isScheduling = true
	defer func() { s.isScheduling = false }()

	debug.Log("Starting job scheduling cycle", nil)

	result := &ScheduleJobsResult{
		AssignedTasks:   []models.JobTask{},
		InterruptedJobs: []uuid.UUID{},
		Errors:          []error{},
	}

	// Get available agents
	availableAgents, err := s.jobExecutionService.GetAvailableAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get available agents: %w", err)
	}

	if len(availableAgents) == 0 {
		debug.Log("No available agents for job scheduling", nil)

		// Check for high-priority jobs that can interrupt running jobs
		// This only happens when NO agents are available
		interruptedJobID, err := s.checkAndInterruptForHighPriority(ctx)
		if err != nil {
			debug.Log("Error checking for high-priority interruptions", map[string]interface{}{
				"error": err.Error(),
			})
		} else if interruptedJobID != nil {
			debug.Log("Interrupted job for high-priority override", map[string]interface{}{
				"interrupted_job_id": *interruptedJobID,
			})
			result.InterruptedJobs = append(result.InterruptedJobs, *interruptedJobID)

			// Re-get available agents after interruption
			// The interrupted task's agent should now be available
			availableAgents, err = s.jobExecutionService.GetAvailableAgents(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get available agents after interruption: %w", err)
			}

			debug.Log("Re-checked available agents after interruption", map[string]interface{}{
				"agent_count": len(availableAgents),
			})
		}

		// If still no agents available after interruption check, return
		if len(availableAgents) == 0 {
			return result, nil
		}
	}

	debug.Log("Found available agents", map[string]interface{}{
		"agent_count": len(availableAgents),
	})

	// Get all jobs with pending work (no longer filtered by max_agents)
	jobsWithWork, err := s.jobExecutionService.GetAllJobsWithPendingWork(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs with pending work: %w", err)
	}

	// Expand increment jobs into per-layer entries for independent scheduling
	jobsWithWork = s.expandIncrementJobsIntoLayers(ctx, jobsWithWork)

	debug.Log("Found jobs with pending work", map[string]interface{}{
		"job_count": len(jobsWithWork),
	})

	if len(jobsWithWork) == 0 {
		debug.Log("No jobs with pending work", nil)
		return result, nil
	}

	// NEW: Create benchmark plan
	benchmarkPlan, err := s.CreateBenchmarkPlan(ctx, availableAgents, jobsWithWork)
	if err != nil {
		return nil, fmt.Errorf("failed to create benchmark plan: %w", err)
	}

	// NEW: If ANY benchmarks needed, execute them and wait (blocking)
	if len(benchmarkPlan.ForcedBenchmarks) > 0 || len(benchmarkPlan.AgentBenchmarks) > 0 {
		debug.Info("Benchmarks needed, executing in parallel and waiting for completion", map[string]interface{}{
			"forced_benchmarks": len(benchmarkPlan.ForcedBenchmarks),
			"agent_benchmarks":  len(benchmarkPlan.AgentBenchmarks),
			"total_benchmarks":  len(benchmarkPlan.ForcedBenchmarks) + len(benchmarkPlan.AgentBenchmarks),
		})

		// Send all benchmark requests in parallel and get filtered plan (only benchmarks actually sent)
		// ExecuteBenchmarkPlan filters out agents that are syncing or busy
		filteredPlan, err := s.ExecuteBenchmarkPlan(ctx, benchmarkPlan)
		if err != nil {
			debug.Error("Failed to execute benchmark plan: %v", err)
		}

		// Only insert records and wait if benchmarks were actually sent
		// This prevents waiting forever for phantom benchmark records
		if filteredPlan != nil && (len(filteredPlan.ForcedBenchmarks) > 0 || len(filteredPlan.AgentBenchmarks) > 0) {
			// Insert benchmark request records for tracking (only for benchmarks that were sent)
			if err := s.InsertBenchmarkRequests(ctx, filteredPlan); err != nil {
				debug.Error("Failed to insert benchmark requests: %v", err)
			}

			// WAIT for benchmarks that were actually sent to complete or timeout
			allCompleted := s.WaitForBenchmarks(ctx)
			if !allCompleted {
				debug.Warning("Benchmark timeout reached, marking timed-out benchmarks as failed")
				// Mark timed-out benchmarks as failed and update job error messages
				if err := s.MarkTimedOutBenchmarksAsFailed(ctx); err != nil {
					debug.Error("Failed to mark timed-out benchmarks: %v", err)
				}
			}

			// Clear benchmark requests table for next cycle
			if err := s.ClearBenchmarkRequests(ctx); err != nil {
				debug.Error("Failed to clear benchmark requests: %v", err)
			}
		} else {
			debug.Info("No benchmarks were sent this cycle (agents not ready), skipping wait")
		}

		// Refresh available agents (exclude timed out ones)
		availableAgents, err = s.jobExecutionService.GetAvailableAgents(ctx)
		if err != nil {
			debug.Error("Failed to refresh available agents after benchmarks: %v", err)
		}

		// CRITICAL: Re-fetch and re-expand jobs after benchmarks complete
		// Benchmarks set is_accurate_keyspace=true on increment layers, which allows them
		// to be scheduled. Without re-fetching, we'd still have the parent job entry
		// which creates tasks without layer IDs (breaking hashcat with --skip/--limit + --increment)
		jobsWithWork, err = s.jobExecutionService.GetAllJobsWithPendingWork(ctx)
		if err != nil {
			debug.Error("Failed to refresh jobs after benchmarks: %v", err)
		} else {
			jobsWithWork = s.expandIncrementJobsIntoLayers(ctx, jobsWithWork)
			debug.Log("Re-expanded jobs after benchmarks", map[string]interface{}{
				"job_count": len(jobsWithWork),
			})
		}

		debug.Info("Benchmark phase complete, proceeding to task assignment", map[string]interface{}{
			"available_agents": len(availableAgents),
			"jobs_with_work":   len(jobsWithWork),
		})
	} else {
		debug.Log("No benchmarks needed, proceeding directly to task assignment", nil)
	}

	// NEW: Prioritize agents that completed forced benchmarks for their jobs
	s.PrioritizeForcedBenchmarkAgents(ctx, availableAgents, jobsWithWork)

	// Calculate priority-based agent allocation with binary version compatibility
	allocation, compatMatrix, err := s.CalculateAgentAllocation(ctx, availableAgents, jobsWithWork)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate agent allocation: %w", err)
	}

	// Reserve agents for jobs based on allocation with compatibility-aware assignment
	s.reserveAgentsForJobs(availableAgents, allocation, jobsWithWork, compatMatrix)

	// NEW: Create all task assignment plans sequentially (prevents overlapping ranges)
	debug.Info("Creating task assignment plans", map[string]interface{}{
		"total_reserved_agents": len(s.reservedAgents),
		"jobs_with_work":        len(jobsWithWork),
	})

	taskPlans, planErrors := s.CreateTaskAssignmentPlans(ctx, s.reservedAgents, jobsWithWork)
	if len(planErrors) > 0 {
		debug.Warning("Errors during task planning: %d errors", len(planErrors))
		result.Errors = append(result.Errors, planErrors...)
	}

	debug.Info("Task planning complete", map[string]interface{}{
		"total_plans":   len(taskPlans),
		"planning_errors": len(planErrors),
	})

	// NEW: Execute task assignments in parallel
	if len(taskPlans) > 0 {
		debug.Info("Executing task assignments in parallel", map[string]interface{}{
			"total_plans": len(taskPlans),
		})

		assignmentResults, execErrors := s.ExecuteTaskAssignmentPlans(ctx, taskPlans)
		if len(execErrors) > 0 {
			debug.Warning("Errors during task execution: %d errors", len(execErrors))
			result.Errors = append(result.Errors, execErrors...)
		}

		// Process results and count skipped assignments
		skippedCount := 0
		for _, plan := range taskPlans {
			if plan.SkipAssignment {
				skippedCount++
				debug.Info("Skipped agent assignment", map[string]interface{}{
					"agent_id": plan.AgentID,
					"reason":   plan.SkipReason,
				})
			}
		}

		// Count successful assignments and add tasks to result
		successCount := 0
		for _, assignResult := range assignmentResults {
			if assignResult.Success {
				successCount++
				// Add task to result for compatibility with existing code
				task := &models.JobTask{ID: assignResult.TaskID}
				result.AssignedTasks = append(result.AssignedTasks, *task)
			}
		}

		debug.Info("Task assignment phase complete", map[string]interface{}{
			"assigned": successCount,
			"skipped":  skippedCount,
			"failed":   len(assignmentResults) - successCount,
			"errors":   len(execErrors),
		})
	} else {
		debug.Info("No task plans to execute", nil)
	}

	// Release any unused reservations
	s.releaseUnusedReservations()

	debug.Log("Job scheduling cycle completed", map[string]interface{}{
		"assigned_tasks":   len(result.AssignedTasks),
		"interrupted_jobs": len(result.InterruptedJobs),
		"errors":           len(result.Errors),
	})

	return result, nil
}

// reserveAgentsForJobs populates the reservation map based on allocation decisions
func (s *JobSchedulingService) reserveAgentsForJobs(
	availableAgents []models.Agent,
	allocation map[uuid.UUID]int,
	jobsWithWork []models.JobExecutionWithWork,
	matrix *CompatibilityMatrix,
) {
	s.reservationMutex.Lock()
	defer s.reservationMutex.Unlock()

	// Clear existing reservations
	s.reservedAgents = make(map[int]uuid.UUID)

	// If no compatibility matrix, fall back to simple sequential assignment
	if matrix == nil {
		agentIndex := 0
		for _, job := range jobsWithWork {
			agentCount, exists := allocation[job.ID]
			if !exists || agentCount == 0 {
				continue
			}
			for i := 0; i < agentCount && agentIndex < len(availableAgents); i++ {
				agent := availableAgents[agentIndex]
				s.reservedAgents[agent.ID] = job.ID
				agentIndex++
			}
		}
		debug.Log("Agent reservation completed (no compatibility matrix)", map[string]interface{}{
			"total_reserved": len(s.reservedAgents),
		})
		return
	}

	// Track which agents are still available for assignment
	remainingAgents := make(map[int]bool)
	for _, agent := range availableAgents {
		remainingAgents[agent.ID] = true
	}

	// Group jobs by priority level (jobs are already sorted by priority DESC, created_at ASC)
	priorityGroups := make(map[int][]models.JobExecutionWithWork)
	priorities := []int{}

	for _, job := range jobsWithWork {
		agentCount, exists := allocation[job.ID]
		if !exists || agentCount == 0 {
			continue // Skip jobs with no allocation
		}

		if _, exists := priorityGroups[job.Priority]; !exists {
			priorities = append(priorities, job.Priority)
		}
		priorityGroups[job.Priority] = append(priorityGroups[job.Priority], job)
	}

	// Sort priorities descending
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))

	// Process each priority level
	for _, priority := range priorities {
		jobs := priorityGroups[priority]

		// Sort jobs within this priority by constraint score ASC (most constrained first)
		sort.Slice(jobs, func(i, j int) bool {
			iInfo := matrix.JobInfo[jobs[i].ID]
			jInfo := matrix.JobInfo[jobs[j].ID]
			iScore := 999999 // Default high score if not found
			jScore := 999999
			if iInfo != nil {
				iScore = iInfo.ConstraintScore
			}
			if jInfo != nil {
				jScore = jInfo.ConstraintScore
			}
			// Most constrained (lowest score) first
			// If tied, preserve FIFO order (jobs are already in created_at ASC order)
			return iScore < jScore
		})

		debug.Log("Processing priority level for reservation", map[string]interface{}{
			"priority":   priority,
			"job_count":  len(jobs),
		})

		// Assign agents to jobs in constrained-first order
		for _, job := range jobs {
			agentCount := allocation[job.ID]
			if agentCount == 0 {
				continue
			}

			// Get compatible agents sorted by flexibility (specialists first)
			compatibleAgents := matrix.getCompatibleAgents(job.ID, remainingAgents)

			debug.Log("Reserving agents for job (constrained-first)", map[string]interface{}{
				"job_id":            job.ID,
				"job_name":          job.Name,
				"priority":          job.Priority,
				"requested_count":   agentCount,
				"compatible_count":  len(compatibleAgents),
				"constraint_score":  matrix.JobInfo[job.ID].ConstraintScore,
			})

			// Reserve up to agentCount compatible agents
			reserved := 0
			for _, agentID := range compatibleAgents {
				if reserved >= agentCount {
					break
				}

				s.reservedAgents[agentID] = job.ID
				remainingAgents[agentID] = false // Mark as used
				reserved++

				debug.Log("Reserved agent for job", map[string]interface{}{
					"agent_id":         agentID,
					"job_id":           job.ID,
					"flexibility_score": matrix.AgentInfo[agentID].FlexibilityScore,
				})
			}

			if reserved < agentCount {
				debug.Log("Could not reserve all requested agents", map[string]interface{}{
					"job_id":    job.ID,
					"requested": agentCount,
					"reserved":  reserved,
				})
			}
		}
	}

	debug.Log("Agent reservation completed (compatibility-aware)", map[string]interface{}{
		"total_reserved": len(s.reservedAgents),
	})
}

// releaseUnusedReservations clears any remaining agent reservations
// This releases agents that were reserved but not assigned (e.g., job exhausted)
func (s *JobSchedulingService) releaseUnusedReservations() {
	s.reservationMutex.Lock()
	defer s.reservationMutex.Unlock()

	if len(s.reservedAgents) > 0 {
		debug.Log("Releasing unused agent reservations", map[string]interface{}{
			"count": len(s.reservedAgents),
		})

		// Clear all remaining reservations
		s.reservedAgents = make(map[int]uuid.UUID)
	}
}


// getChunkDuration gets the chunk duration for a job from preset job or settings
func (s *JobSchedulingService) getChunkDuration(ctx context.Context, jobExecution *models.JobExecution) (int, error) {
	// First try to get from job execution itself
	if jobExecution.ChunkSizeSeconds > 0 {
		return jobExecution.ChunkSizeSeconds, nil
	}

	// Fall back to system setting
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration")
	if err != nil {
		return 1200, nil // Default 20 minutes
	}

	chunkDuration := 1200 // Default 20 minutes
	if setting.Value != nil {
		if parsed, parseErr := strconv.Atoi(*setting.Value); parseErr == nil {
			chunkDuration = parsed
		}
	}

	return chunkDuration, nil
}

// HandleTaskSuccess handles successful task completion and resets consecutive failure counters
func (s *JobSchedulingService) HandleTaskSuccess(ctx context.Context, taskID uuid.UUID) error {
	// Get the task to find the job execution and agent
	task, err := s.jobExecutionService.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Reset job's consecutive failures
	jobExecution, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, task.JobExecutionID)
	if err == nil && jobExecution.ConsecutiveFailures > 0 {
		err = s.jobExecutionService.jobExecRepo.UpdateConsecutiveFailures(ctx, task.JobExecutionID, 0)
		if err != nil {
			debug.Log("Failed to reset job consecutive failures", map[string]interface{}{
				"job_execution_id": task.JobExecutionID,
				"error":            err.Error(),
			})
		}
	}

	// Reset agent's consecutive failures if assigned
	if task.AgentID != nil {
		agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
		if err == nil && agent.ConsecutiveFailures > 0 {
			err = s.agentRepo.UpdateConsecutiveFailures(ctx, *task.AgentID, 0)
			if err != nil {
				debug.Log("Failed to reset agent consecutive failures", map[string]interface{}{
					"agent_id": *task.AgentID,
					"error":    err.Error(),
				})
			}
		}
	}

	return nil
}

// StartScheduler starts the job scheduler with periodic scheduling
func (s *JobSchedulingService) StartScheduler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Cleanup ticker runs every 5 minutes
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	debug.Log("Job scheduler started", map[string]interface{}{
		"interval":         interval,
		"cleanup_interval": "5m",
	})

	// Recover stale jobs on startup
	if err := s.RecoverStaleJobs(ctx); err != nil {
		debug.Log("Failed to recover stale jobs on startup", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Run cleanup on startup
	if err := s.CleanupStaleAgentStatus(ctx); err != nil {
		debug.Log("Failed to cleanup stale agent status on startup", map[string]interface{}{
			"error": err.Error(),
		})
	}

	for {
		select {
		case <-ctx.Done():
			debug.Log("Job scheduler stopped", nil)
			return
		case <-ticker.C:
			result, err := s.ScheduleJobs(ctx)
			if err != nil {
				debug.Log("Scheduling cycle failed", map[string]interface{}{
					"error": err.Error(),
				})
				continue
			}

			// Log scheduling results
			if len(result.AssignedTasks) > 0 || len(result.InterruptedJobs) > 0 || len(result.Errors) > 0 {
				debug.Log("Scheduling cycle completed", map[string]interface{}{
					"assigned_tasks":   len(result.AssignedTasks),
					"interrupted_jobs": len(result.InterruptedJobs),
					"errors":           len(result.Errors),
				})
			}
		case <-cleanupTicker.C:
			// Run periodic cleanup of stale agent status
			if err := s.CleanupStaleAgentStatus(ctx); err != nil {
				debug.Log("Failed to cleanup stale agent status", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}
}

// checkAndInterruptForHighPriority checks if there are high-priority jobs waiting
// that should interrupt lower priority running jobs. This only runs when no agents are available.
func (s *JobSchedulingService) checkAndInterruptForHighPriority(ctx context.Context) (*uuid.UUID, error) {
	// Check if interruption is enabled
	interruptionSetting, err := s.systemSettingsRepo.GetSetting(ctx, "job_interruption_enabled")
	if err != nil {
		return nil, fmt.Errorf("failed to get interruption setting: %w", err)
	}

	if interruptionSetting.Value == nil || *interruptionSetting.Value != "true" {
		return nil, nil // Interruption disabled
	}

	// Get the highest priority pending job with allow_high_priority_override
	highPriorityJobs, err := s.jobExecutionService.jobExecRepo.GetPendingJobsWithHighPriorityOverride(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get high-priority jobs: %w", err)
	}

	if len(highPriorityJobs) == 0 {
		return nil, nil // No high-priority jobs waiting
	}

	// Get the highest priority job (first in the list since they're ordered by priority DESC)
	highPriorityJob := highPriorityJobs[0]

	// Check how many agents are already assigned to this high-priority job
	currentTasks, err := s.jobExecutionService.jobTaskRepo.GetTasksByJobExecution(ctx, highPriorityJob.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current tasks for high-priority job: %w", err)
	}
	
	// Count active agents for this job
	activeAgentCount := 0
	for _, task := range currentTasks {
		if task.AgentID != nil && (task.Status == models.JobTaskStatusRunning || task.Status == models.JobTaskStatusAssigned) {
			activeAgentCount++
		}
	}
	
	// Use the job's own max_agents setting (0 means unlimited)
	maxAgents := highPriorityJob.MaxAgents
	if maxAgents == 0 {
		maxAgents = 999 // Treat 0 as unlimited
	}
	
	// Don't interrupt if already at max agents
	if activeAgentCount >= maxAgents {
		debug.Log("High-priority job already at max agents, skipping interruption", map[string]interface{}{
			"job_id": highPriorityJob.ID,
			"active_agents": activeAgentCount,
			"max_agents": maxAgents,
		})
		return nil, nil
	}

	// Check if there are any interruptible jobs with lower priority
	interruptibleJobs, err := s.jobExecutionService.CanInterruptJob(ctx, highPriorityJob.Priority)
	if err != nil {
		return nil, fmt.Errorf("failed to check interruptible jobs: %w", err)
	}

	if len(interruptibleJobs) == 0 {
		return nil, nil // No jobs to interrupt
	}

	// Calculate how many agents we need to free up
	agentsNeeded := maxAgents - activeAgentCount
	if agentsNeeded <= 0 {
		return nil, nil // Already have enough agents
	}

	debug.Log("Smart interruption: need to free up agents", map[string]interface{}{
		"high_priority_job_id": highPriorityJob.ID,
		"high_priority":        highPriorityJob.Priority,
		"agents_needed":        agentsNeeded,
		"current_active":       activeAgentCount,
		"max_agents":           maxAgents,
	})

	// Smart interruption: interrupt only as many tasks as needed
	// Priority: lowest priority first, then newest jobs within same priority
	tasksToInterrupt := []models.JobTask{}
	// Map from job ID to list of task IDs to interrupt for that job
	interruptedJobTasks := make(map[uuid.UUID][]uuid.UUID)

	// Sort interruptible jobs by priority ASC (lowest first), then created_at DESC (newest first)
	// interruptibleJobs is already sorted by priority ASC from CanInterruptJob
	// Now we need to also sort by created_at DESC within same priority
	for i := 0; i < len(interruptibleJobs); i++ {
		for j := i + 1; j < len(interruptibleJobs); j++ {
			if interruptibleJobs[i].Priority == interruptibleJobs[j].Priority {
				// Same priority: sort by created_at DESC (newer first for interruption)
				if interruptibleJobs[i].CreatedAt.Before(interruptibleJobs[j].CreatedAt) {
					interruptibleJobs[i], interruptibleJobs[j] = interruptibleJobs[j], interruptibleJobs[i]
				}
			}
		}
	}

	// Collect tasks to interrupt
	for _, job := range interruptibleJobs {
		if len(tasksToInterrupt) >= agentsNeeded {
			break // We have enough agents
		}

		// Get running tasks for this job, sorted by created_at DESC (newest first)
		runningTasks, err := s.jobExecutionService.jobTaskRepo.GetTasksByJobExecution(ctx, job.ID)
		if err != nil {
			debug.Error("Failed to get running tasks for job %s: %v", job.ID, err)
			continue
		}

		// Filter to only running tasks and sort by created_at DESC
		var activeTasks []models.JobTask
		for _, task := range runningTasks {
			if task.AgentID != nil && task.Status == models.JobTaskStatusRunning {
				activeTasks = append(activeTasks, task)
			}
		}

		// Sort by created_at DESC (newest first)
		for i := 0; i < len(activeTasks); i++ {
			for j := i + 1; j < len(activeTasks); j++ {
				if activeTasks[i].CreatedAt.Before(activeTasks[j].CreatedAt) {
					activeTasks[i], activeTasks[j] = activeTasks[j], activeTasks[i]
				}
			}
		}

		// Add tasks to interrupt list
		for _, task := range activeTasks {
			if len(tasksToInterrupt) >= agentsNeeded {
				break
			}
			tasksToInterrupt = append(tasksToInterrupt, task)
			// Track which task IDs belong to which job
			interruptedJobTasks[job.ID] = append(interruptedJobTasks[job.ID], task.ID)
		}
	}

	if len(tasksToInterrupt) == 0 {
		debug.Log("No tasks available to interrupt", nil)
		return nil, nil
	}

	debug.Log("Interrupting tasks for high-priority override", map[string]interface{}{
		"tasks_to_interrupt": len(tasksToInterrupt),
		"jobs_affected":      len(interruptedJobTasks),
		"high_priority_job":  highPriorityJob.ID,
	})

	// Send stop commands to agents for each task
	for _, task := range tasksToInterrupt {
		debug.Log("Sending stop command to agent for task", map[string]interface{}{
			"task_id":  task.ID,
			"agent_id": *task.AgentID,
			"job_id":   task.JobExecutionID,
		})

		// Send stop command via WebSocket integration
		if s.wsIntegration != nil {
			stopErr := s.wsIntegration.SendJobStop(ctx, task.ID, fmt.Sprintf("Task interrupted by higher priority job %s", highPriorityJob.ID))
			if stopErr != nil {
				// Log the error but continue with interruption
				debug.Error("Failed to send stop command to agent %d for task %s: %v", *task.AgentID, task.ID, stopErr)
			}
		}
	}

	// Interrupt specific tasks for each affected job in the database
	// This only marks the specific tasks that were sent stop commands
	for jobID, taskIDs := range interruptedJobTasks {
		err = s.jobExecutionService.InterruptJob(ctx, jobID, highPriorityJob.ID, taskIDs)
		if err != nil {
			debug.Error("Failed to interrupt job %s: %v", jobID, err)
		}
	}

	// Return the first interrupted job ID for backwards compatibility
	for jobID := range interruptedJobTasks {
		return &jobID, nil
	}

	return nil, nil
}

// ProcessJobCompletion handles job completion and cleanup
func (s *JobSchedulingService) ProcessJobCompletion(ctx context.Context, jobExecutionID uuid.UUID) error {
	debug.Log("Processing job completion", map[string]interface{}{
		"job_execution_id": jobExecutionID,
	})

	// Get job details
	job, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// Check if already completed to avoid redundant updates
	if job.Status == models.JobExecutionStatusCompleted {
		debug.Log("Job already completed, skipping", map[string]interface{}{
			"job_id": jobExecutionID,
		})
		return nil
	}

	// CODE 6 EARLY COMPLETION: Check if job progress is 100% AND hashlist is fully cracked.
	// - Progress 100% is set by the AllHashesCracked handler (code 6 from hashcat)
	// - Hashlist check confirms all hashes are actually cracked (cracks recorded to DB)
	// When both conditions are met, complete immediately without keyspace checks.
	if job.OverallProgressPercent >= 100.0 {
		// Verify hashlist is fully cracked before bypassing keyspace checks
		hashlist, hashlistErr := s.jobExecutionService.hashlistRepo.GetByID(ctx, job.HashlistID)
		if hashlistErr == nil && hashlist.TotalHashes > 0 && hashlist.CrackedHashes >= hashlist.TotalHashes {
			// All hashes cracked - complete the job without keyspace validation
			err = s.jobExecutionService.CompleteJobExecution(ctx, jobExecutionID)
			if err != nil {
				return fmt.Errorf("failed to complete job execution (code 6 - all hashes cracked): %w", err)
			}
			debug.Log("Job completed via code 6 path (100% progress + hashlist fully cracked)", map[string]interface{}{
				"job_execution_id": jobExecutionID,
				"progress_percent": job.OverallProgressPercent,
				"hashlist_id":      job.HashlistID,
			})
			return nil
		}
		// Hashlist not fully cracked yet or error - fall through to normal checks
		// This handles race conditions where cracks haven't been recorded yet
	}

	// Check if all tasks for this job are completed
	incompleteTasks, err := s.jobExecutionService.jobTaskRepo.GetIncompleteTasksCount(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get incomplete tasks count: %w", err)
	}

	// For increment mode jobs, use layer-based completion instead of keyspace comparison
	// This is necessary because dispatched_keyspace is in BASE units but effective_keyspace
	// is in EFFECTIVE units, making direct comparison invalid for increment mode
	if job.IncrementMode != "" && job.IncrementMode != "off" && incompleteTasks == 0 {
		layers, err := s.jobExecutionService.jobIncrementLayerRepo.GetByJobExecutionID(ctx, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to get increment layers for completion check: %w", err)
		}

		allLayersComplete := true
		for _, layer := range layers {
			if layer.Status != models.JobIncrementLayerStatusCompleted {
				allLayersComplete = false
				debug.Log("Increment job has incomplete layer", map[string]interface{}{
					"job_id":       jobExecutionID,
					"layer_index":  layer.LayerIndex,
					"layer_status": string(layer.Status),
					"layer_mask":   layer.Mask,
				})
				break
			}
		}

		if allLayersComplete {
			// All layers complete and no pending tasks - mark job complete
			err = s.jobExecutionService.CompleteJobExecution(ctx, jobExecutionID)
			if err != nil {
				return fmt.Errorf("failed to complete increment job execution: %w", err)
			}
			debug.Log("Increment job completed - all layers done", map[string]interface{}{
				"job_id":      jobExecutionID,
				"layer_count": len(layers),
			})
		}
		return nil // Don't fall through to regular keyspace comparison for increment jobs
	}

	// For rule-split jobs, also check if all rules have been dispatched
	if job.UsesRuleSplitting && incompleteTasks == 0 {
		// Get total rules from the job's effective keyspace and base keyspace
		totalRules := job.MultiplicationFactor
		if totalRules == 0 && job.EffectiveKeyspace != nil && job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
			totalRules = int(*job.EffectiveKeyspace / *job.BaseKeyspace)
		}
		
		// Get the maximum rule end index from all tasks
		maxRuleEnd, err := s.jobExecutionService.jobTaskRepo.GetMaxRuleEndIndex(ctx, jobExecutionID)
		if err != nil {
			debug.Log("Failed to get max rule end index", map[string]interface{}{
				"job_execution_id": jobExecutionID,
				"error":            err.Error(),
			})
		}
		
		// Check if all rules have been processed
		allRulesProcessed := false
		if maxRuleEnd != nil && totalRules > 0 {
			allRulesProcessed = *maxRuleEnd >= totalRules
		}
		
		debug.Log("Rule-split job completion check", map[string]interface{}{
			"job_execution_id":   jobExecutionID,
			"incomplete_tasks":   incompleteTasks,
			"total_rules":        totalRules,
			"max_rule_end":       maxRuleEnd,
			"all_rules_processed": allRulesProcessed,
		})
		
		// Only complete if all tasks are done AND all rules have been dispatched
		if !allRulesProcessed {
			debug.Log("Rule-split job has completed tasks but not all rules dispatched", map[string]interface{}{
				"job_execution_id": jobExecutionID,
				"total_rules":      totalRules,
				"max_rule_end":     maxRuleEnd,
			})
			return nil
		}
	}

	if incompleteTasks == 0 {
		// For non-rule-splitting jobs, check if all work has been dispatched
		// using counter comparison. We use effective_keyspace as the target
		// since that represents the actual total candidates to process.
		// Note: base_keyspace is the wordlist size (different dimension) and
		// should NOT be used for completion comparison.
		if !job.UsesRuleSplitting {
			if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 && job.DispatchedKeyspace < *job.EffectiveKeyspace {
				// More effective keyspace needs to be dispatched
				debug.Log("Job not complete - effective_keyspace check", map[string]interface{}{
					"job_id":              jobExecutionID,
					"effective_keyspace":  *job.EffectiveKeyspace,
					"dispatched_keyspace": job.DispatchedKeyspace,
					"remaining":           *job.EffectiveKeyspace - job.DispatchedKeyspace,
					"percentage":          float64(job.DispatchedKeyspace) / float64(*job.EffectiveKeyspace) * 100,
				})
				return nil // Don't complete yet
			}
		}

		// CRITICAL SAFETY: Don't complete rule-split jobs if not all rules dispatched
		// This is a final safety net in case effective_keyspace was set incorrectly
		if job.UsesRuleSplitting {
			// Get actual rule count from file(s)
			totalRulesNeeded := 0
			for _, ruleID := range job.RuleIDs {
				rulePath, err := s.jobExecutionService.resolveRulePath(ctx, ruleID)
				if err == nil {
					ruleCount, err := s.jobExecutionService.ruleSplitManager.CountRules(ctx, rulePath)
					if err == nil {
						totalRulesNeeded += ruleCount
					}
				}
			}

			if totalRulesNeeded > 0 {
				maxRuleEnd, _ := s.jobExecutionService.jobTaskRepo.GetMaxRuleEndIndex(ctx, jobExecutionID)
				rulesDispatched := 0
				if maxRuleEnd != nil {
					rulesDispatched = *maxRuleEnd
				}

				if rulesDispatched < totalRulesNeeded {
					debug.Log("Rule-split job incomplete - safety check prevented premature completion", map[string]interface{}{
						"job_id": jobExecutionID,
						"rules_dispatched": rulesDispatched,
						"total_rules": totalRulesNeeded,
						"percent_complete": float64(rulesDispatched) / float64(totalRulesNeeded) * 100,
					})
					return nil // NOT DONE - more rules to dispatch
				}
			}
		}

		// All tasks are complete AND all keyspace has been dispatched, mark job as completed
		err = s.jobExecutionService.CompleteJobExecution(ctx, jobExecutionID)
		if err != nil {
			return fmt.Errorf("failed to complete job execution: %w", err)
		}

		debug.Log("Job execution completed - all tasks done and keyspace fully dispatched", map[string]interface{}{
			"job_execution_id": jobExecutionID,
			"effective_keyspace": job.EffectiveKeyspace,
			"dispatched_keyspace": job.DispatchedKeyspace,
			"incomplete_tasks": incompleteTasks,
		})
	} else {
		debug.Log("Job has incomplete tasks", map[string]interface{}{
			"job_execution_id": jobExecutionID,
			"incomplete_tasks": incompleteTasks,
		})
	}

	return nil
}

// ProcessTaskProgress handles task progress updates and job aggregation
func (s *JobSchedulingService) ProcessTaskProgress(ctx context.Context, taskID uuid.UUID, progress *models.JobProgress) error {
	// Use the enhanced progress tracking method from job execution service
	err := s.jobExecutionService.UpdateTaskProgress(ctx, taskID, progress.KeyspaceProcessed, progress.EffectiveProgress, &progress.HashRate, progress.ProgressPercent)
	if err != nil {
		return fmt.Errorf("failed to update task progress: %w", err)
	}

	// Get the task to find the job execution and agent ID
	task, err := s.jobExecutionService.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Store performance metrics
	if progress.HashRate > 0 {
		metric := &models.JobPerformanceMetric{
			JobExecutionID:   task.JobExecutionID,
			MetricType:       models.JobMetricTypeHashRate,
			Value:            float64(progress.HashRate),
			Timestamp:        time.Now(),
			AggregationLevel: models.AggregationLevelRealtime,
		}

		err = s.jobExecutionService.benchmarkRepo.CreateJobPerformanceMetric(ctx, metric)
		if err != nil {
			debug.Log("Failed to store job performance metric", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Store device-specific metrics if available
	if len(progress.DeviceMetrics) > 0 && task.AgentID != nil {
		// Get the job execution to get attack mode
		jobExec, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, task.JobExecutionID)
		if err != nil {
			debug.Log("Failed to get job execution for device metrics", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			attackMode := int(jobExec.AttackMode)
			
			// Store metrics for each device
			for _, device := range progress.DeviceMetrics {
				timestamp := time.Now()
				
				// Store temperature metric
				if device.Temp > 0 {
					tempMetric := &models.AgentPerformanceMetric{
						AgentID:          *task.AgentID,
						MetricType:       models.MetricTypeTemperature,
						Value:            device.Temp,
						Timestamp:        timestamp,
						AggregationLevel: models.AggregationLevelRealtime,
						DeviceID:         &device.DeviceID,
						DeviceName:       &device.DeviceName,
						TaskID:           &taskID,
						AttackMode:       &attackMode,
					}
					if err := s.jobExecutionService.benchmarkRepo.CreateAgentPerformanceMetric(ctx, tempMetric); err != nil {
						debug.Log("Failed to store temperature metric", map[string]interface{}{"error": err.Error()})
					}
				}

				// Store utilization metric
				if device.Util >= 0 {
					utilMetric := &models.AgentPerformanceMetric{
						AgentID:          *task.AgentID,
						MetricType:       models.MetricTypeUtilization,
						Value:            device.Util,
						Timestamp:        timestamp,
						AggregationLevel: models.AggregationLevelRealtime,
						DeviceID:         &device.DeviceID,
						DeviceName:       &device.DeviceName,
						TaskID:           &taskID,
						AttackMode:       &attackMode,
					}
					if err := s.jobExecutionService.benchmarkRepo.CreateAgentPerformanceMetric(ctx, utilMetric); err != nil {
						debug.Log("Failed to store utilization metric", map[string]interface{}{"error": err.Error()})
					}
				}

				// Store fan speed metric (custom metric type)
				if device.FanSpeed >= 0 {
					// Note: Using power_usage as a placeholder for fan speed since it's not in the enum
					fanMetric := &models.AgentPerformanceMetric{
						AgentID:          *task.AgentID,
						MetricType:       models.MetricTypePowerUsage, // TODO: Add MetricTypeFanSpeed to enum
						Value:            device.FanSpeed,
						Timestamp:        timestamp,
						AggregationLevel: models.AggregationLevelRealtime,
						DeviceID:         &device.DeviceID,
						DeviceName:       &device.DeviceName,
						TaskID:           &taskID,
						AttackMode:       &attackMode,
					}
					if err := s.jobExecutionService.benchmarkRepo.CreateAgentPerformanceMetric(ctx, fanMetric); err != nil {
						debug.Log("Failed to store fan speed metric", map[string]interface{}{"error": err.Error()})
					}
				}

				// Store hash rate metric per device
				if device.Speed > 0 {
					hashRateMetric := &models.AgentPerformanceMetric{
						AgentID:          *task.AgentID,
						MetricType:       models.MetricTypeHashRate,
						Value:            float64(device.Speed),
						Timestamp:        timestamp,
						AggregationLevel: models.AggregationLevelRealtime,
						DeviceID:         &device.DeviceID,
						DeviceName:       &device.DeviceName,
						TaskID:           &taskID,
						AttackMode:       &attackMode,
					}
					if err := s.jobExecutionService.benchmarkRepo.CreateAgentPerformanceMetric(ctx, hashRateMetric); err != nil {
						debug.Log("Failed to store hash rate metric", map[string]interface{}{"error": err.Error()})
					}
				}
			}
		}
	}

	return nil
}

// GetJobExecutionStatus returns the current status of a job execution
func (s *JobSchedulingService) GetJobExecutionStatus(ctx context.Context, jobExecutionID uuid.UUID) (*models.JobExecution, []models.JobTask, error) {
	jobExecution, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get job execution: %w", err)
	}

	tasks, err := s.jobExecutionService.jobTaskRepo.GetTasksByJobExecution(ctx, jobExecutionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get job tasks: %w", err)
	}

	return jobExecution, tasks, nil
}

// SetWebSocketIntegration sets the WebSocket integration for sending job assignments
func (s *JobSchedulingService) SetWebSocketIntegration(integration JobWebSocketIntegration) {
	s.wsIntegration = integration
}

// StopJob stops a running job execution and all its tasks
func (s *JobSchedulingService) StopJob(ctx context.Context, jobExecutionID uuid.UUID, reason string) error {
	// Update job execution status to cancelled
	err := s.jobExecutionService.jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusCancelled)
	if err != nil {
		return fmt.Errorf("failed to update job execution status: %w", err)
	}

	// Cancel all running tasks
	tasks, err := s.jobExecutionService.jobTaskRepo.GetTasksByJobExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get tasks: %w", err)
	}

	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning || task.Status == models.JobTaskStatusAssigned {
			err = s.jobExecutionService.jobTaskRepo.CancelTask(ctx, task.ID)
			if err != nil {
				debug.Log("Failed to cancel task", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
			}
		}
	}

	debug.Log("Job execution stopped", map[string]interface{}{
		"job_execution_id": jobExecutionID,
		"reason":           reason,
	})

	return nil
}

// RecoverStaleJobs recovers jobs that were assigned but not completed when server restarts
func (s *JobSchedulingService) RecoverStaleJobs(ctx context.Context) error {
	debug.Log("Starting stale job recovery", nil)

	// Get all tasks that are in 'assigned' or 'running' state
	staleStatuses := []string{"assigned", "running"}
	staleTasks, err := s.jobExecutionService.jobTaskRepo.GetTasksByStatuses(ctx, staleStatuses)
	if err != nil {
		return fmt.Errorf("failed to get stale tasks: %w", err)
	}

	if len(staleTasks) == 0 {
		debug.Log("No stale tasks found", nil)
		return nil
	}

	debug.Log("Found stale tasks to recover", map[string]interface{}{
		"task_count": len(staleTasks),
	})

	// Reset each stale task back to pending
	for _, task := range staleTasks {
		// Check if the agent is currently connected
		if task.AgentID == nil {
			// Task was never assigned to an agent, just reset it
			err = s.jobExecutionService.jobTaskRepo.ResetTaskForRetry(ctx, task.ID)
			if err != nil {
				debug.Log("Failed to reset unassigned stale task", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
			}
			continue
		}

		agent, err := s.agentRepo.GetByID(ctx, *task.AgentID)
		if err != nil {
			debug.Log("Failed to get agent for stale task", map[string]interface{}{
				"task_id":  task.ID,
				"agent_id": task.AgentID,
				"error":    err.Error(),
			})
			continue
		}

		// If agent is active and connected, we'll wait for it to report progress
		if agent.Status == "active" {
			// Check last checkpoint time
			if task.LastCheckpoint != nil {
				timeSinceCheckpoint := time.Since(*task.LastCheckpoint)
				// If checkpoint is recent (within 5 minutes), assume task is still running
				if timeSinceCheckpoint < 5*time.Minute {
					debug.Log("Task has recent checkpoint, assuming still running", map[string]interface{}{
						"task_id":               task.ID,
						"time_since_checkpoint": timeSinceCheckpoint,
					})
					continue
				}
			}
		}

		// Reset task to pending for reassignment
		err = s.jobExecutionService.jobTaskRepo.ResetTaskForRetry(ctx, task.ID)
		if err != nil {
			debug.Log("Failed to reset stale task", map[string]interface{}{
				"task_id": task.ID,
				"error":   err.Error(),
			})
			continue
		}

		debug.Log("Reset stale task to pending", map[string]interface{}{
			"task_id":          task.ID,
			"job_execution_id": task.JobExecutionID,
			"agent_id":         task.AgentID,
		})

		// Also update the job execution status if needed
		jobExec, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, task.JobExecutionID)
		if err == nil && jobExec.Status == models.JobExecutionStatusRunning {
			// Check if there are any other running tasks for this job
			activeTasks, err := s.jobExecutionService.jobTaskRepo.GetActiveTasksCount(ctx, task.JobExecutionID)
			if err == nil && activeTasks == 0 {
				// No active tasks, reset job to pending
				err = s.jobExecutionService.jobExecRepo.UpdateStatus(ctx, task.JobExecutionID, models.JobExecutionStatusPending)
				if err != nil {
					debug.Log("Failed to reset job execution status", map[string]interface{}{
						"job_execution_id": task.JobExecutionID,
						"error":            err.Error(),
					})
				}
			}
		}
	}

	debug.Log("Stale job recovery completed", map[string]interface{}{
		"total_tasks_recovered": len(staleTasks),
	})

	return nil
}

// CleanupStaleAgentStatus cleans up stale agent busy_status metadata
// This runs periodically to catch agents stuck in busy state when database updates failed
func (s *JobSchedulingService) CleanupStaleAgentStatus(ctx context.Context) error {
	debug.Log("Starting stale agent status cleanup", nil)

	// Get all agents with busy_status = "true"
	agents, err := s.agentRepo.List(ctx, map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to get agents: %w", err)
	}

	cleanedCount := 0
	for _, agent := range agents {
		// Skip if not marked as busy
		if agent.Metadata == nil || agent.Metadata["busy_status"] != "true" {
			continue
		}

		// Check if the agent has a valid running task
		taskIDStr, hasTaskID := agent.Metadata["current_task_id"]
		if !hasTaskID || taskIDStr == "" {
			// No task ID but marked as busy - clear it
			debug.Log("Cleanup: Clearing busy status with no task ID", map[string]interface{}{
				"agent_id": agent.ID,
			})
			agent.Metadata["busy_status"] = "false"
			delete(agent.Metadata, "current_task_id")
			delete(agent.Metadata, "current_job_id")
			if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
				debug.Error("Failed to clear stale agent status: %v", err)
				continue
			}
			cleanedCount++
			continue
		}

		// Parse and validate task ID
		taskUUID, err := uuid.Parse(taskIDStr)
		if err != nil {
			// Invalid task ID - clear it
			debug.Log("Cleanup: Clearing busy status with invalid task ID", map[string]interface{}{
				"agent_id":     agent.ID,
				"invalid_task": taskIDStr,
			})
			agent.Metadata["busy_status"] = "false"
			delete(agent.Metadata, "current_task_id")
			delete(agent.Metadata, "current_job_id")
			if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
				debug.Error("Failed to clear stale agent status: %v", err)
				continue
			}
			cleanedCount++
			continue
		}

		// Check if task exists and is actually running
		task, err := s.jobExecutionService.jobTaskRepo.GetByID(ctx, taskUUID)
		if err != nil || task == nil {
			// Task doesn't exist - clear busy status
			debug.Log("Cleanup: Clearing busy status for non-existent task", map[string]interface{}{
				"agent_id": agent.ID,
				"task_id":  taskIDStr,
			})
			agent.Metadata["busy_status"] = "false"
			delete(agent.Metadata, "current_task_id")
			delete(agent.Metadata, "current_job_id")
			if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
				debug.Error("Failed to clear stale agent status: %v", err)
				continue
			}
			cleanedCount++
			continue
		}

		// Check if task is assigned to this agent and in running state
		if task.AgentID == nil || *task.AgentID != agent.ID {
			debug.Log("Cleanup: Clearing busy status for task assigned to different agent", map[string]interface{}{
				"agent_id":         agent.ID,
				"task_id":          taskIDStr,
				"task_assigned_to": task.AgentID,
			})
			agent.Metadata["busy_status"] = "false"
			delete(agent.Metadata, "current_task_id")
			delete(agent.Metadata, "current_job_id")
			if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
				debug.Error("Failed to clear stale agent status: %v", err)
				continue
			}
			cleanedCount++
			continue
		}

		if task.Status != models.JobTaskStatusRunning && task.Status != models.JobTaskStatusAssigned {
			debug.Log("Cleanup: Clearing busy status for completed/failed task", map[string]interface{}{
				"agent_id":    agent.ID,
				"task_id":     taskIDStr,
				"task_status": task.Status,
			})
			agent.Metadata["busy_status"] = "false"
			delete(agent.Metadata, "current_task_id")
			delete(agent.Metadata, "current_job_id")
			if err := s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata); err != nil {
				debug.Error("Failed to clear stale agent status: %v", err)
				continue
			}
			cleanedCount++
			continue
		}

		// If we get here, the agent is legitimately busy
		debug.Log("Agent is legitimately busy", map[string]interface{}{
			"agent_id":    agent.ID,
			"task_id":     taskIDStr,
			"task_status": task.Status,
		})
	}

	if cleanedCount > 0 {
		debug.Log("Stale agent status cleanup completed", map[string]interface{}{
			"agents_cleaned": cleanedCount,
		})
	}

	return nil
}
