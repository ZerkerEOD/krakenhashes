package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// derefString returns the pointed-to string or "" for nil. Used at the
// boundary between *string fields on models.JobTask and the local
// non-pointer struct fields below (the legacy scheduler always populates
// AttackCmd/ChunkNumber, so the local types stay non-nullable for
// readability; the model exposes them as pointers because scheduler-v2
// tasks legitimately omit them).
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt is the int counterpart to derefString.
func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// ptrString wraps a string into a *string for assignment to a nullable
// JobTask field.
func ptrString(s string) *string {
	return &s
}

// ptrInt wraps an int into a *int for assignment to a nullable JobTask
// field.
func ptrInt(i int) *int {
	return &i
}

// bigIntPtrLog renders a nullable *models.BigInt for debug logs: the decimal
// string, or "<nil>" when the pointer is nil (NULL effective keyspace).
func bigIntPtrLog(b *models.BigInt) string {
	if b == nil {
		return "<nil>"
	}
	return b.String()
}

// TaskAssignmentPlan contains all pre-calculated data for assigning a task to an agent
type TaskAssignmentPlan struct {
	AgentID     int
	Agent       *models.Agent
	JobExecution *models.JobExecution
	ChunkDuration int
	BenchmarkSpeed int64

	// Keyspace chunking
	KeyspaceStart int64
	KeyspaceEnd   int64

	IsKeyspaceSplit bool // Whether task uses keyspace splitting (--skip/--limit)
	ChunkNumber     int

	// Increment layer support
	IncrementLayerID *uuid.UUID // If set, task belongs to this increment layer
	LayerMask        string      // The specific mask for this layer (overrides job mask)

	// Effective keyspace (EFFECTIVE units → BigInt; base × rules × salts can exceed int64)
	EffectiveKeyspaceStart models.BigInt
	EffectiveKeyspaceEnd   models.BigInt

	AttackCmd string // Pre-built, will need rule path replacement for rule splits

	// Salt-aware chunk calculation
	IsSalted      bool // Whether the hash type uses per-hash salts
	TotalHashes   int  // Total hashes in the hashlist
	CrackedHashes int  // Number of cracked hashes

	// Flags
	SkipAssignment bool // If no valid benchmark or job exhausted
	SkipReason     string

	// Existing pending task (if reassigning instead of creating new)
	ExistingTask *models.JobTask
}

// TaskAssignmentResult contains the result of a task assignment
type TaskAssignmentResult struct {
	AgentID int
	TaskID  uuid.UUID
	Success bool
	Error   error
}

// JobPlanningState tracks the state of a job during planning
type JobPlanningState struct {
	JobExecution        *models.JobExecution
	DispatchedKeyspace  models.BigInt // EFFECTIVE units (base × rules × salts can exceed int64)
	IsExhausted         bool
	ChunkNumber         int

	// BASE keyspace tracking for --skip/--limit (in password candidate units, not EFFECTIVE hash operations)
	// This is initialized once from DB at the start of planning and updated in-memory after each chunk.
	// Using this prevents the race condition where all agents query DB before any tasks exist.
	DispatchedBaseKeyspace int64

	// Increment layer support
	CurrentLayer        *models.JobIncrementLayer // The layer currently being worked on
	AvailableLayers     []models.JobIncrementLayer // All layers needing work

	// Per-layer BASE keyspace tracking (for increment mode)
	LayerDispatchedBaseKeyspace map[uuid.UUID]int64
}

// CreateTaskAssignmentPlans calculates chunk assignments for all reserved agents sequentially
// This ensures no overlapping keyspace or rule ranges
func (s *JobSchedulingService) CreateTaskAssignmentPlans(
	ctx context.Context,
	reservedAgents map[int]uuid.UUID,
	jobsWithWork []models.JobExecutionWithWork,
) ([]TaskAssignmentPlan, []error) {
	debug.Info("Creating task assignment plans", map[string]interface{}{
		"reserved_agent_count": len(reservedAgents),
		"jobs_with_work":       len(jobsWithWork),
	})

	var plans []TaskAssignmentPlan
	var errors []error

	// Create job lookup map (will be populated as we process jobs)
	jobMap := make(map[uuid.UUID]*models.JobExecutionWithWork)

	// Track planning state for each job
	jobStates := make(map[uuid.UUID]*JobPlanningState)

	// Track layer ID to parent job ID mapping for lookups
	layerToJobMap := make(map[uuid.UUID]uuid.UUID)

	// Track in-cycle agent assignments per job for max_agents enforcement
	// Key: job entry ID (may be layer ID), Value: number of agents assigned in this planning cycle
	planningAssignments := make(map[uuid.UUID]int)

	// Track the original entry ID for each job (may be layer ID or job ID)
	// This is needed because job.JobExecution.ID may change during layer processing
	// due to Go struct embedding (JobExecutionWithWork embeds JobExecution, so
	// job.ID accesses job.JobExecution.ID which gets overwritten when we copy parent job data)
	entryIDs := make([]uuid.UUID, len(jobsWithWork))
	for i := range jobsWithWork {
		entryIDs[i] = jobsWithWork[i].ID
	}

	// Process jobs in priority order (jobs are already sorted)
	for i := range jobsWithWork {
		job := &jobsWithWork[i]
		originalID := entryIDs[i] // Use pre-captured entry ID (might be layer ID)

		// Check if this entry represents an increment layer (job.ID is actually layer.ID)
		var actualJobID uuid.UUID
		var specificLayer *models.JobIncrementLayer

		if job.IncrementMode != "" && job.IncrementMode != "off" {
			// Try to load as a layer first
			layer, err := s.jobExecutionService.jobIncrementLayerRepo.GetByID(ctx, job.ID)
			if err == nil && layer != nil {
				// This is a layer entry
				actualJobID = layer.JobExecutionID
				specificLayer = layer

				// Load the actual parent job execution
				parentJob, err := s.jobExecutionService.jobExecRepo.GetByID(ctx, actualJobID)
				if err != nil {
					debug.Warning("Failed to load parent job for layer %s: %v", job.ID, err)
					continue
				}

				// Update job reference to use parent job data
				// NOTE: This copies parent job's ID into job.JobExecution.ID (due to struct embedding)
				// We use originalID (from entryIDs) for reservation lookups instead
				job.JobExecution = *parentJob

				// Track layer to job mapping (original layer ID -> parent job ID)
				layerToJobMap[originalID] = actualJobID

				debug.Log("Processing layer entry", map[string]interface{}{
					"layer_id":      specificLayer.ID,
					"layer_index":   specificLayer.LayerIndex,
					"layer_mask":    specificLayer.Mask,
					"parent_job_id": actualJobID,
				})
			} else {
				// Not a layer - this is a parent job entry kept for benchmark planning
				// Skip task creation to avoid increment_layer_id = NULL bug
				debug.Log("Skipping increment parent job - no specific layer (waiting for benchmark)", map[string]interface{}{
					"job_id": job.ID,
				})
				continue
			}
		} else {
			actualJobID = job.ID
		}

		// Keep job.ID as original for agent reservation lookup
		// But use actualJobID for job execution operations
		jobMap[originalID] = job

		// Initialize job state
		state := &JobPlanningState{
			JobExecution:                &job.JobExecution,
			DispatchedKeyspace:          job.DispatchedKeyspace,
			IsExhausted:                 false,
			LayerDispatchedBaseKeyspace: make(map[uuid.UUID]int64),
		}

		// Initialize BASE keyspace tracking from DB ONCE per job (not per agent)
		// This prevents the race condition where all agents get the same starting value
		// because they all query DB before any tasks are created.
		maxBaseEnd, err := s.jobExecutionService.jobTaskRepo.GetMaxKeyspaceEnd(ctx, actualJobID)
		if err != nil {
			debug.Warning("Failed to get max keyspace end for job %s: %v, starting from 0", actualJobID, err)
			maxBaseEnd = 0
		}
		state.DispatchedBaseKeyspace = maxBaseEnd
		debug.Log("Initialized BASE keyspace tracking for job", map[string]interface{}{
			"job_id":                   actualJobID,
			"dispatched_base_keyspace": maxBaseEnd,
		})

		// If this is a specific layer entry, set it as the current layer
		if specificLayer != nil {
			state.CurrentLayer = specificLayer
			state.AvailableLayers = []models.JobIncrementLayer{*specificLayer}

			// Initialize per-layer BASE keyspace tracking
			maxLayerBaseEnd, err := s.jobExecutionService.jobTaskRepo.GetMaxKeyspaceEndByLayer(ctx, specificLayer.ID)
			if err != nil {
				debug.Warning("Failed to get max keyspace end for layer %s: %v, starting from 0", specificLayer.ID, err)
				maxLayerBaseEnd = 0
			}
			state.LayerDispatchedBaseKeyspace[specificLayer.ID] = maxLayerBaseEnd

			debug.Log("Using specific layer for task assignment", map[string]interface{}{
				"job_id":                         actualJobID,
				"layer_id":                       specificLayer.ID,
				"layer_index":                    specificLayer.LayerIndex,
				"layer_mask":                     specificLayer.Mask,
				"effective_keyspace":             bigIntPtrLog(specificLayer.EffectiveKeyspace),
				"dispatched_keyspace":            specificLayer.DispatchedKeyspace.String(),
				"layer_dispatched_base_keyspace": maxLayerBaseEnd,
			})
		}

		// Get next chunk number
		chunkNum, err := s.jobExecutionService.jobTaskRepo.GetNextChunkNumber(ctx, job.ID)
		if err != nil {
			debug.Warning("Failed to get next chunk number for job %s: %v", job.ID, err)
			chunkNum = 1
		}
		state.ChunkNumber = chunkNum

		// Key state by originalID (layer ID for layers, job ID for regular jobs)
		// This matches what reserveAgentsForJobs uses for reservation lookup
		jobStates[originalID] = state

		debug.Log("Initialized job planning state", map[string]interface{}{
			"entry_id":            originalID,
			"job_execution_id":    job.JobExecution.ID,
			"dispatched_keyspace": state.DispatchedKeyspace.String(),
			"chunk_number":        state.ChunkNumber,
		})
	}

	// Process agents by job priority
	for i := range jobsWithWork {
		job := &jobsWithWork[i]
		entryID := entryIDs[i] // Use pre-captured entry ID for lookups

		state := jobStates[entryID]
		if state == nil || state.IsExhausted {
			debug.Log("Job already exhausted or no state, skipping", map[string]interface{}{
				"entry_id": entryID,
			})
			continue
		}

		// Get agents reserved for this job entry
		// entryID matches what reserveAgentsForJobs used (layer ID for layers, job ID for regular jobs)
		var agentsForJob []int
		for agentID, reservedID := range reservedAgents {
			if reservedID == entryID {
				agentsForJob = append(agentsForJob, agentID)
			}
		}

		debug.Log("Processing agents for job", map[string]interface{}{
			"entry_id":         entryID,
			"job_execution_id": job.JobExecution.ID,
			"parent_job":       layerToJobMap[entryID],
			"is_layer":         layerToJobMap[entryID] != uuid.Nil,
			"agent_count":      len(agentsForJob),
		})

		// Process each agent for this job
		for _, agentID := range agentsForJob {
			if state.IsExhausted {
				debug.Log("Job exhausted during planning, skipping remaining agents", map[string]interface{}{
					"entry_id": entryID,
				})
				break
			}

			plan, err := s.createSingleTaskPlan(ctx, agentID, state, jobsWithWork, jobStates, planningAssignments)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to create plan for agent %d: %w", agentID, err))
				continue
			}

			if plan != nil {
				plans = append(plans, *plan)
				// Track assignment for max_agents enforcement
				if plan.JobExecution != nil && !plan.SkipAssignment {
					planningAssignments[plan.JobExecution.ID]++
				}
			}
		}
	}

	debug.Info("Task assignment planning complete", map[string]interface{}{
		"total_plans": len(plans),
		"errors":      len(errors),
	})

	return plans, errors
}

// createSingleTaskPlan creates a task assignment plan for a single agent
func (s *JobSchedulingService) createSingleTaskPlan(
	ctx context.Context,
	agentID int,
	currentState *JobPlanningState,
	allJobs []models.JobExecutionWithWork,
	jobStates map[uuid.UUID]*JobPlanningState,
	planningAssignments map[uuid.UUID]int,
) (*TaskAssignmentPlan, error) {
	// Get agent details
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	// DEFENSIVE CHECK: Verify agent is compatible with job's binary version
	// This is a safety net - compatibility should have been checked during allocation
	if !version.IsCompatibleStr(agent.BinaryVersion, currentState.JobExecution.BinaryVersion) {
		debug.Warning("Agent %d not compatible with job %s binary version (agent: %s, job: %s) - skipping task assignment",
			agentID, currentState.JobExecution.ID, agent.BinaryVersion, currentState.JobExecution.BinaryVersion)
		return nil, fmt.Errorf("agent not compatible with job binary version")
	}

	// Get hashlist for job
	hashlist, err := s.jobExecutionService.hashlistRepo.GetByID(ctx, currentState.JobExecution.HashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Get hash type for salt-aware chunk calculations
	hashType, err := s.jobExecutionService.GetHashTypeByID(ctx, hashlist.HashTypeID)
	if err != nil {
		debug.Warning("Failed to get hash type for salt adjustment: %v (will use default behavior)", err)
		// Continue without salt adjustment - not a fatal error
	}
	isSalted := hashType != nil && hashType.IsSalted

	// Determine salt count for salted hash types (used for benchmark lookup)
	var saltCount *int
	if isSalted {
		uncrackedCount, countErr := s.jobExecutionService.hashlistRepo.GetUncrackedHashCount(ctx, currentState.JobExecution.HashlistID)
		if countErr == nil && uncrackedCount > 0 {
			saltCount = &uncrackedCount
		}
	}

	// PRIORITY 1: Check for existing pending tasks FIRST - prioritize reassignment over new chunks
	pendingTasks, err := s.jobExecutionService.jobTaskRepo.GetPendingTasksByJobExecution(ctx, currentState.JobExecution.ID)
	if err == nil && len(pendingTasks) > 0 {
		pendingTask := &pendingTasks[0] // Take oldest pending task (ORDER BY created_at ASC)

		debug.Info("Found pending task for job, reassigning to agent", map[string]interface{}{
			"job_id":       currentState.JobExecution.ID,
			"agent_id":     agentID,
			"task_id":      pendingTask.ID,
			"chunk_number": pendingTask.ChunkNumber,
		})

		// Verify agent has benchmark for this job before reassigning (salt-aware lookup)
		benchmark, err := s.jobExecutionService.benchmarkRepo.GetAgentBenchmark(
			ctx, agentID, currentState.JobExecution.AttackMode, hashlist.HashTypeID, saltCount)

		if err != nil || benchmark == nil {
			debug.Log("Agent missing benchmark for pending task job, skipping reassignment", map[string]interface{}{
				"agent_id":  agentID,
				"job_id":    currentState.JobExecution.ID,
				"hash_type": hashlist.HashTypeID,
			})
			// Don't reassign - continue to new chunk logic below
		} else {
			// Create plan from existing pending task

			// Handle nullable BenchmarkSpeed - use fresh benchmark if nil
			benchmarkSpeed := benchmark.Speed
			if pendingTask.BenchmarkSpeed != nil {
				benchmarkSpeed = *pendingTask.BenchmarkSpeed
			}

			// Handle nullable EffectiveKeyspace fields - use 0 if nil (will be set when task starts)
			effectiveStart := models.NewBigInt(0)
			if pendingTask.EffectiveKeyspaceStart != nil {
				effectiveStart = *pendingTask.EffectiveKeyspaceStart
			}
			effectiveEnd := models.NewBigInt(0)
			if pendingTask.EffectiveKeyspaceEnd != nil {
				effectiveEnd = *pendingTask.EffectiveKeyspaceEnd
			}

			// Recovery optimization - use checkpoint to skip already-processed keyspace
			recoveryKeyspaceStart := pendingTask.KeyspaceStart
			recoveryIsKeyspaceSplit := pendingTask.IsKeyspaceSplit
			recoveryEffectiveStart := effectiveStart

			if pendingTask.KeyspaceProcessed > pendingTask.KeyspaceStart {
				// Partial progress was made - resume from checkpoint
				recoveryKeyspaceStart = pendingTask.KeyspaceProcessed
				recoveryIsKeyspaceSplit = true // Force --skip/--limit flags

				// Calculate new effective keyspace start proportionally
				if pendingTask.EffectiveKeyspaceStart != nil && pendingTask.EffectiveKeyspaceEnd != nil &&
					pendingTask.KeyspaceEnd > pendingTask.KeyspaceStart {
					// multiplier = effective range / base range
					baseRange := pendingTask.KeyspaceEnd - pendingTask.KeyspaceStart
					effectiveRange := (*pendingTask.EffectiveKeyspaceEnd).Sub(*pendingTask.EffectiveKeyspaceStart)
					multiplier := float64(effectiveRange.Int64()) / float64(baseRange)

					// New effective start = original effective start + (processed base * multiplier)
					processedBase := pendingTask.KeyspaceProcessed - pendingTask.KeyspaceStart
					recoveryEffectiveStart = (*pendingTask.EffectiveKeyspaceStart).AddInt64(int64(float64(processedBase) * multiplier))
				}

				debug.Info("Task recovery: resuming from checkpoint", map[string]interface{}{
					"task_id":            pendingTask.ID,
					"original_start":     pendingTask.KeyspaceStart,
					"recovery_start":     recoveryKeyspaceStart,
					"keyspace_processed": pendingTask.KeyspaceProcessed,
					"remaining_work":     pendingTask.KeyspaceEnd - recoveryKeyspaceStart,
				})
			}

			plan := &TaskAssignmentPlan{
				AgentID:        agentID,
				Agent:          agent,
				JobExecution:   currentState.JobExecution,
				ChunkDuration:  pendingTask.ChunkDuration,
				BenchmarkSpeed: benchmarkSpeed,
				ExistingTask:   pendingTask, // Mark as pending task reassignment

				// Copy keyspace fields - may be updated for recovery
				KeyspaceStart:          recoveryKeyspaceStart,
				KeyspaceEnd:            pendingTask.KeyspaceEnd,
				EffectiveKeyspaceStart: recoveryEffectiveStart,
				EffectiveKeyspaceEnd:   effectiveEnd,
				IsKeyspaceSplit:        recoveryIsKeyspaceSplit,
				// AttackCmd / ChunkNumber on JobTask are *string / *int after
				// the scheduler-v2 refactor; the local plan struct keeps
				// non-pointer types because the legacy path always populates
				// them. Coerce here at the boundary.
				AttackCmd:   derefString(pendingTask.AttackCmd),
				ChunkNumber: derefInt(pendingTask.ChunkNumber),

				// Salt-aware chunk calculation
				IsSalted:      isSalted,
				TotalHashes:   hashlist.TotalHashes,
				CrackedHashes: hashlist.CrackedHashes,
			}

			return plan, nil
		}
	}

	// PRIORITY 2: No pending tasks OR agent lacks benchmark - create NEW chunk
	// Check if agent has valid benchmark for this job (salt-aware lookup)
	benchmark, err := s.jobExecutionService.benchmarkRepo.GetAgentBenchmark(
		ctx, agentID, currentState.JobExecution.AttackMode, hashlist.HashTypeID, saltCount)

	if err != nil || benchmark == nil {
		// Agent lacks benchmark for reserved job - skip assignment to preserve FIFO order
		// Agent will get benchmark via round-robin in the next scheduling cycle
		// DO NOT fall back to other jobs - this would break FIFO ordering
		debug.Log("Agent lacks benchmark for reserved job, skipping assignment (FIFO preserved)", map[string]interface{}{
			"agent_id":  agentID,
			"job_id":    currentState.JobExecution.ID,
			"hash_type": hashlist.HashTypeID,
		})
		return &TaskAssignmentPlan{
			AgentID:        agentID,
			SkipAssignment: true,
			SkipReason:     fmt.Sprintf("Agent %d lacks benchmark for reserved job %s (hash_type=%d) - will get benchmark in next cycle", agentID, currentState.JobExecution.ID, hashlist.HashTypeID),
		}, nil
	}

	// Block dispatch if job lacks accurate keyspace - must wait for benchmark to complete
	// This prevents dispatching tasks when the forced benchmark timed out
	if !currentState.JobExecution.IsAccurateKeyspace {
		debug.Log("Job lacks accurate keyspace, skipping task dispatch", map[string]interface{}{
			"job_id":              currentState.JobExecution.ID,
			"is_accurate_keyspace": currentState.JobExecution.IsAccurateKeyspace,
			"agent_id":            agentID,
		})
		return &TaskAssignmentPlan{
			AgentID:        agentID,
			SkipAssignment: true,
			SkipReason:     fmt.Sprintf("Job %s lacks accurate keyspace - waiting for benchmark to complete", currentState.JobExecution.ID),
		}, nil
	}

	// Get chunk duration
	chunkDuration := 1200 // Default 20 minutes
	if duration, err := s.getChunkDuration(ctx, currentState.JobExecution); err == nil {
		chunkDuration = duration
	}

	// Calculate chunk based on job type
	plan := &TaskAssignmentPlan{
		AgentID:        agentID,
		Agent:          agent,
		JobExecution:   currentState.JobExecution,
		ChunkDuration:  chunkDuration,
		BenchmarkSpeed: benchmark.Speed,

		// Salt-aware chunk calculation
		IsSalted:      isSalted,
		TotalHashes:   hashlist.TotalHashes,
		CrackedHashes: hashlist.CrackedHashes,
	}

	// Add increment layer information if this is an increment mode job
	if currentState.CurrentLayer != nil {
		plan.IncrementLayerID = &currentState.CurrentLayer.ID
		plan.LayerMask = currentState.CurrentLayer.Mask
		debug.Log("Added increment layer to task plan", map[string]interface{}{
			"layer_id":    plan.IncrementLayerID,
			"layer_mask":  plan.LayerMask,
			"layer_index": currentState.CurrentLayer.LayerIndex,
		})
	}

	// Regular keyspace chunking
	err = s.calculateKeyspaceChunk(ctx, plan, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate keyspace chunk: %w", err)
	}

	return plan, nil
}

// ExecuteTaskAssignmentPlans executes task assignments in parallel
func (s *JobSchedulingService) ExecuteTaskAssignmentPlans(
	ctx context.Context,
	plans []TaskAssignmentPlan,
) ([]TaskAssignmentResult, []error) {
	debug.Info("Executing task assignment plans in parallel", map[string]interface{}{
		"total_plans": len(plans),
	})

	// Filter out skipped assignments
	var validPlans []TaskAssignmentPlan
	for _, plan := range plans {
		if !plan.SkipAssignment {
			validPlans = append(validPlans, plan)
		}
	}

	debug.Info("Filtered valid plans", map[string]interface{}{
		"total_plans":  len(plans),
		"valid_plans":  len(validPlans),
		"skipped":      len(plans) - len(validPlans),
	})

	if len(validPlans) == 0 {
		return []TaskAssignmentResult{}, nil
	}

	// Create channels for results
	resultsChan := make(chan TaskAssignmentResult, len(validPlans))
	var wg sync.WaitGroup

	// Launch goroutines for each valid plan
	for i := range validPlans {
		wg.Add(1)
		go func(plan TaskAssignmentPlan) {
			defer wg.Done()
			result := s.executeTaskAssignment(ctx, &plan)
			resultsChan <- result
		}(validPlans[i])
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(resultsChan)

	// Collect results
	var results []TaskAssignmentResult
	var errors []error

	for result := range resultsChan {
		results = append(results, result)
		if !result.Success && result.Error != nil {
			errors = append(errors, result.Error)
		}
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	debug.Info("Task assignment execution complete", map[string]interface{}{
		"total_executed": len(results),
		"successful":     successCount,
		"failed":         len(results) - successCount,
		"errors":         len(errors),
	})

	return results, errors
}

// executeTaskAssignment executes a single task assignment (runs in goroutine)
func (s *JobSchedulingService) executeTaskAssignment(
	ctx context.Context,
	plan *TaskAssignmentPlan,
) TaskAssignmentResult {
	result := TaskAssignmentResult{
		AgentID: plan.AgentID,
		Success: false,
	}

	debug.Log("Executing task assignment", map[string]interface{}{
		"agent_id": plan.AgentID,
		"job_id":   plan.JobExecution.ID,
	})

	// Step 2: Sync hashlist (includes latest cracks)
	err := s.hashlistSyncService.EnsureHashlistOnAgent(ctx, plan.AgentID, plan.JobExecution.HashlistID)
	if err != nil {
		result.Error = fmt.Errorf("failed to sync hashlist: %w", err)
		return result
	}

	// Step 3: Sync files (5 minute blocking - includes rule chunks)
	// Extended timeout allows agents to hash large wordlist files (50GB+)
	if s.wsIntegration != nil {
		syncTimeout := 5 * time.Minute
		err = s.wsIntegration.SyncAgentFiles(ctx, plan.AgentID, syncTimeout)
		if err != nil {
			result.Error = fmt.Errorf("failed to sync files: %w", err)
			return result
		}
	}

	// Step 4: Create or update task in database
	var task *models.JobTask

	if plan.ExistingTask != nil {
		// PENDING TASK PATH: Update existing task with new agent assignment
		task = plan.ExistingTask
		task.AgentID = &plan.AgentID
		task.Status = models.JobTaskStatusAssigned
		task.AttackCmd = ptrString(plan.AttackCmd) // Updated with new chunk path if rule-split
		now := time.Now()
		task.AssignedAt = &now

		// Apply recovery optimization - update keyspace fields from checkpoint
		task.KeyspaceStart = plan.KeyspaceStart     // May be updated from keyspace_processed
		task.IsKeyspaceSplit = plan.IsKeyspaceSplit // Ensure split flag is set
		task.EffectiveKeyspaceStart = &plan.EffectiveKeyspaceStart
		task.EffectiveKeyspaceEnd = &plan.EffectiveKeyspaceEnd

		err = s.jobExecutionService.jobTaskRepo.Update(ctx, task)
		if err != nil {
			result.Error = fmt.Errorf("failed to update pending task: %w", err)
			return result
		}

		debug.Info("Reassigned pending task to agent", map[string]interface{}{
			"agent_id":     plan.AgentID,
			"task_id":      task.ID,
			"job_id":       plan.JobExecution.ID,
			"chunk_number": task.ChunkNumber,
		})
	} else {
		// NEW TASK PATH: Create new task
		task = &models.JobTask{
			JobExecutionID:         plan.JobExecution.ID,
			AgentID:                &plan.AgentID,
			Status:                 models.JobTaskStatusPending,
			Priority:               plan.JobExecution.Priority,
			AttackCmd:              ptrString(plan.AttackCmd),
			KeyspaceStart:          plan.KeyspaceStart,
			KeyspaceEnd:            plan.KeyspaceEnd,
			KeyspaceProcessed:      0,
			EffectiveKeyspaceStart: &plan.EffectiveKeyspaceStart,
			EffectiveKeyspaceEnd:   &plan.EffectiveKeyspaceEnd,
			ChunkNumber:            ptrInt(plan.ChunkNumber),
			ChunkDuration:          plan.ChunkDuration,
			BenchmarkSpeed:         &plan.BenchmarkSpeed,
			IncrementLayerID:       plan.IncrementLayerID, // Set layer ID for increment mode tasks
		}

		// Set keyspace split flag
		task.IsKeyspaceSplit = plan.IsKeyspaceSplit

		err = s.jobExecutionService.jobTaskRepo.Create(ctx, task)
		if err != nil {
			debug.Error("Failed to create task in database", map[string]interface{}{
				"error":              err.Error(),
				"job_execution_id":   task.JobExecutionID,
				"increment_layer_id": task.IncrementLayerID,
				"agent_id":           task.AgentID,
				"keyspace_start":     task.KeyspaceStart,
				"keyspace_end":       task.KeyspaceEnd,
			})
			result.Error = fmt.Errorf("failed to create task: %w", err)
			return result
		}

		debug.Log("Created new task in database", map[string]interface{}{
			"agent_id": plan.AgentID,
			"task_id":  task.ID,
			"job_id":   plan.JobExecution.ID,
		})

		// Update layer dispatched_keyspace if this is an increment layer task
		if plan.IncrementLayerID != nil {
			dispatchedAmount := plan.KeyspaceEnd - plan.KeyspaceStart
			err = s.jobExecutionService.jobIncrementLayerRepo.IncrementDispatchedKeyspace(
				ctx,
				*plan.IncrementLayerID,
				models.NewBigInt(dispatchedAmount),
			)
			if err != nil {
				debug.Warning("Failed to increment layer dispatched keyspace: %v", err)
				// Don't fail the task assignment for this
			} else {
				debug.Log("Incremented layer dispatched keyspace", map[string]interface{}{
					"layer_id":         plan.IncrementLayerID,
					"dispatched_amount": dispatchedAmount,
				})
			}
		}
	}

	// Step 5: Start job execution if in pending status
	if plan.JobExecution.Status == models.JobExecutionStatusPending {
		err = s.jobExecutionService.StartJobExecution(ctx, plan.JobExecution.ID)
		if err != nil {
			debug.Warning("Failed to start job execution: %v", err)
			// Don't fail the task assignment for this
		}
	}

	// Step 6: Send WebSocket task assignment (agent starts immediately)
	if s.wsIntegration != nil {
		err = s.wsIntegration.SendJobAssignment(ctx, task, plan.JobExecution)
		if err != nil {
			result.Error = fmt.Errorf("failed to send task via WebSocket: %w", err)
			return result
		}
	}

	debug.Info("Task assignment complete", map[string]interface{}{
		"agent_id": plan.AgentID,
		"task_id":  task.ID,
		"job_id":   plan.JobExecution.ID,
	})

	result.TaskID = task.ID
	result.Success = true
	return result
}

// calculateKeyspaceChunk calculates keyspace range for a regular job
// IMPORTANT: This function calculates keyspace for --skip/--limit which operate on
// BASE keyspace (password candidates), not effective keyspace. We must use BaseKeyspace
// here because hashcat's --skip and --limit parameters expect password candidate counts.
func (s *JobSchedulingService) calculateKeyspaceChunk(
	ctx context.Context,
	plan *TaskAssignmentPlan,
	state *JobPlanningState,
) error {
	// For increment layer tasks, use layer-specific keyspace
	var totalKeyspace int64
	var dispatchedKeyspace int64

	if state.CurrentLayer != nil {
		// Use layer's BASE keyspace for --skip/--limit (password candidates)
		// BaseKeyspace is the number of password candidates (e.g., 95^6 for ?a?a?a?a?a?a)
		// EffectiveKeyspace may include hash count or other multipliers which don't apply to --skip/--limit
		if state.CurrentLayer.BaseKeyspace != nil {
			totalKeyspace = *state.CurrentLayer.BaseKeyspace
		} else if state.CurrentLayer.EffectiveKeyspace != nil {
			// Fallback to effective if base not available (shouldn't happen normally).
			// totalKeyspace is a BASE (--skip/--limit) sink → coerce BigInt to int64.
			totalKeyspace = state.CurrentLayer.EffectiveKeyspace.Int64()
			debug.Warning("Layer %s has no BaseKeyspace, falling back to EffectiveKeyspace for --skip/--limit", state.CurrentLayer.ID)
		} else {
			return fmt.Errorf("layer has no keyspace information")
		}

		// Use in-memory BASE keyspace tracking (initialized once from DB at the start of planning)
		// This fixes the race condition where all agents query DB before any tasks exist.
		dispatchedKeyspace = state.LayerDispatchedBaseKeyspace[state.CurrentLayer.ID]

		debug.Log("Using in-memory base keyspace_end for layer", map[string]interface{}{
			"layer_id":              state.CurrentLayer.ID,
			"layer_index":           state.CurrentLayer.LayerIndex,
			"base_keyspace_end":     dispatchedKeyspace,
			"total_base_keyspace":   totalKeyspace,
		})
	} else {
		// Regular job - use BASE keyspace for --skip/--limit
		// BaseKeyspace represents wordlist positions that hashcat uses for --skip/--limit
		if plan.JobExecution.BaseKeyspace != nil {
			totalKeyspace = *plan.JobExecution.BaseKeyspace
		} else if plan.JobExecution.EffectiveKeyspace != nil {
			// Fallback to EffectiveKeyspace if BaseKeyspace not set.
			// totalKeyspace is a BASE (--skip/--limit) sink → coerce BigInt to int64.
			totalKeyspace = plan.JobExecution.EffectiveKeyspace.Int64()
		} else {
			return fmt.Errorf("job has no keyspace information")
		}

		// Use in-memory BASE keyspace tracking (initialized once from DB at the start of planning)
		// This fixes the race condition where all agents query DB before any tasks exist.
		dispatchedKeyspace = state.DispatchedBaseKeyspace

		debug.Log("Using in-memory base keyspace_end for job", map[string]interface{}{
			"job_id":            plan.JobExecution.ID,
			"base_keyspace_end": dispatchedKeyspace,
			"total_base_keyspace": totalKeyspace,
		})
	}

	remainingKeyspace := totalKeyspace - dispatchedKeyspace

	if remainingKeyspace <= 0 {
		state.IsExhausted = true
		plan.SkipAssignment = true
		if state.CurrentLayer != nil {
			plan.SkipReason = fmt.Sprintf("No remaining keyspace for layer %d of job %s", state.CurrentLayer.LayerIndex, plan.JobExecution.ID)
		} else {
			plan.SkipReason = fmt.Sprintf("No remaining keyspace for job %s", plan.JobExecution.ID)
		}
		return nil
	}

	// Calculate chunk size in BASE keyspace units
	// BenchmarkSpeed is in effective H/s (hash operations per second), but --skip/--limit use base keyspace
	// (password candidates). We need to convert the speed to base keyspace rate.
	var desiredChunkSize int64

	// Get FRESH effective keyspace from DB - in-memory value may be stale
	// (Benchmark results update DB but not the in-memory JobExecution object)
	// EFFECTIVE value → BigInt.
	var effectiveKeyspace models.BigInt
	var layerID *uuid.UUID
	if state.CurrentLayer != nil {
		layerID = &state.CurrentLayer.ID
	}
	freshEffective, err := s.jobExecutionService.GetFreshEffectiveKeyspace(ctx, plan.JobExecution.ID, layerID)
	if err != nil {
		debug.Warning("Failed to fetch fresh effective keyspace: %v, falling back to in-memory", err)
		// Fallback to in-memory values
		if state.CurrentLayer != nil && state.CurrentLayer.EffectiveKeyspace != nil {
			effectiveKeyspace = *state.CurrentLayer.EffectiveKeyspace
		} else if plan.JobExecution.EffectiveKeyspace != nil {
			effectiveKeyspace = *plan.JobExecution.EffectiveKeyspace
		}
	} else {
		effectiveKeyspace = freshEffective
	}

	// Calculate multiplier (effective / base) and convert speed to base units
	if effectiveKeyspace.IsPositive() && totalKeyspace > 0 {
		// multiplier = effective_keyspace / base_keyspace
		// For mask attacks with many hashes, multiplier ≈ hash_count
		// For salted hashes, effective_keyspace already includes salt factor (applied at job creation)
		// So multiplier = (base × rules × salts) / base = rules × salts
		multiplier := float64(effectiveKeyspace.Int64()) / float64(totalKeyspace)

		// Convert benchmark speed (effective H/s) to base keyspace per second
		// base_per_second = effective_per_second / multiplier
		basePerSecond := float64(plan.BenchmarkSpeed) / multiplier

		// Calculate desired base keyspace chunk
		desiredChunkSize = int64(float64(plan.ChunkDuration) * basePerSecond)

		debug.Log("Converted benchmark speed to base keyspace rate", map[string]interface{}{
			"job_id":              plan.JobExecution.ID,
			"effective_keyspace":  effectiveKeyspace.String(),
			"base_keyspace":       totalKeyspace,
			"multiplier":          multiplier,
			"benchmark_speed_eff": plan.BenchmarkSpeed,
			"base_per_second":     basePerSecond,
			"chunk_duration":      plan.ChunkDuration,
			"desired_base_chunk":  desiredChunkSize,
		})
	} else {
		// Fallback: no effective keyspace available
		// For salted hashes, we MUST apply salt adjustment or chunks will be way too large
		candidateSpeed := plan.BenchmarkSpeed
		if plan.IsSalted {
			remainingHashes := int64(plan.TotalHashes - plan.CrackedHashes)
			if remainingHashes > 0 {
				candidateSpeed = plan.BenchmarkSpeed / remainingHashes
				debug.Log("Applied salt adjustment in fallback chunk calculation", map[string]interface{}{
					"is_salted":         plan.IsSalted,
					"total_hashes":      plan.TotalHashes,
					"cracked_hashes":    plan.CrackedHashes,
					"remaining_hashes":  remainingHashes,
					"original_speed":    plan.BenchmarkSpeed,
					"adjusted_speed":    candidateSpeed,
				})
			}
		}
		desiredChunkSize = int64(plan.ChunkDuration) * candidateSpeed
		debug.Warning("No effective keyspace available for job %s, using %s for chunk calculation",
			plan.JobExecution.ID, func() string {
				if plan.IsSalted {
					return "salt-adjusted speed"
				}
				return "1:1 ratio"
			}())
	}

	// Ensure at least 1 keyspace unit per chunk
	if desiredChunkSize < 1 {
		desiredChunkSize = 1
	}

	// Fluctuation percentage for merging small final chunks (previously a system setting)
	fluctuationPercentage := 20 // Default

	keyspaceStart := dispatchedKeyspace
	keyspaceEnd := keyspaceStart + desiredChunkSize

	if keyspaceEnd >= totalKeyspace {
		// This is the last chunk
		keyspaceEnd = totalKeyspace
	} else {
		// Check if the remaining keyspace after this chunk would be too small
		remainingAfterChunk := totalKeyspace - keyspaceEnd
		fluctuationThreshold := int64(float64(desiredChunkSize) * float64(fluctuationPercentage) / 100.0)

		if remainingAfterChunk <= fluctuationThreshold {
			// Merge the final small chunk into this one
			keyspaceEnd = totalKeyspace
			debug.Log("Merging final keyspace chunk to avoid small remainder", map[string]interface{}{
				"remaining_after_chunk": remainingAfterChunk,
				"fluctuation_threshold": fluctuationThreshold,
				"adjusted_keyspace_end": keyspaceEnd,
			})
		}
	}

	// Build attack command (use plan.LayerMask if this is an increment layer task)
	attackCmd, err := s.jobExecutionService.buildAttackCommand(ctx, nil, plan.JobExecution, plan.LayerMask)
	if err != nil {
		return fmt.Errorf("failed to build attack command: %w", err)
	}

	// Update plan
	plan.KeyspaceStart = keyspaceStart
	plan.KeyspaceEnd = keyspaceEnd
	plan.AttackCmd = attackCmd
	plan.ChunkNumber = state.ChunkNumber
	// Mark as keyspace split if we're assigning a partial chunk (either starting past 0, or ending before total)
	// This ensures continuation tasks (where keyspaceStart > 0) also get --skip and --limit parameters
	plan.IsKeyspaceSplit = (keyspaceStart > 0 || keyspaceEnd < totalKeyspace)

	// Calculate proportional effective keyspace for this chunk
	// For keyspace-split jobs, progress[1] from hashcat reports the ENTIRE job's effective keyspace,
	// not the chunk's. We must calculate the proportional effective keyspace based on the base chunk.
	if effectiveKeyspace.IsPositive() && totalKeyspace > 0 {
		// Scale effective keyspace proportionally to base keyspace chunk
		// effective_chunk_start = (base_start / base_total) * effective_total
		// effective_chunk_end = (base_end / base_total) * effective_total
		effFloat := float64(effectiveKeyspace.Int64())
		plan.EffectiveKeyspaceStart = models.NewBigInt(int64(float64(keyspaceStart) * effFloat / float64(totalKeyspace)))
		plan.EffectiveKeyspaceEnd = models.NewBigInt(int64(float64(keyspaceEnd) * effFloat / float64(totalKeyspace)))

		// For increment layer tasks with LayerIndex > 1, add cumulative offset from previous layers
		// This ensures effective_keyspace_start/end are GLOBAL job positions, not layer-relative
		// which is required for the progress bar visualization to work correctly
		if state.CurrentLayer != nil && state.CurrentLayer.LayerIndex > 1 {
			cumulativeOffset, err := s.jobExecutionService.jobIncrementLayerRepo.GetCumulativeEffectiveKeyspace(
				ctx, state.CurrentLayer.JobExecutionID, state.CurrentLayer.LayerIndex)
			if err != nil {
				debug.Warning("Failed to get cumulative effective keyspace for layer %d: %v", state.CurrentLayer.LayerIndex, err)
				// Continue without offset - visualization may be slightly off but job will still work
			} else if cumulativeOffset.IsPositive() {
				plan.EffectiveKeyspaceStart = plan.EffectiveKeyspaceStart.Add(cumulativeOffset)
				plan.EffectiveKeyspaceEnd = plan.EffectiveKeyspaceEnd.Add(cumulativeOffset)
				debug.Log("Added cumulative layer offset to effective keyspace", map[string]interface{}{
					"layer_id":          state.CurrentLayer.ID,
					"layer_index":       state.CurrentLayer.LayerIndex,
					"cumulative_offset": cumulativeOffset.String(),
					"adjusted_start":    plan.EffectiveKeyspaceStart.String(),
					"adjusted_end":      plan.EffectiveKeyspaceEnd.String(),
				})
			}
		}

		debug.Log("Calculated proportional effective keyspace for chunk", map[string]interface{}{
			"job_id":                   plan.JobExecution.ID,
			"base_chunk_start":         keyspaceStart,
			"base_chunk_end":           keyspaceEnd,
			"effective_chunk_start":    plan.EffectiveKeyspaceStart.String(),
			"effective_chunk_end":      plan.EffectiveKeyspaceEnd.String(),
			"total_base_keyspace":      totalKeyspace,
			"total_effective_keyspace": effectiveKeyspace.String(),
		})
	}

	// Update state for next agent (in-memory tracking during planning)
	dispatchedAmount := keyspaceEnd - keyspaceStart
	if state.CurrentLayer != nil {
		// Update layer's in-memory dispatched keyspace (EFFECTIVE units)
		state.CurrentLayer.DispatchedKeyspace = state.CurrentLayer.DispatchedKeyspace.AddInt64(dispatchedAmount)
		// Update layer's BASE keyspace tracking (for --skip/--limit)
		// This is the KEY FIX: track the END position so next agent gets correct start
		state.LayerDispatchedBaseKeyspace[state.CurrentLayer.ID] = keyspaceEnd
	} else {
		// Update job's in-memory dispatched keyspace (EFFECTIVE units)
		state.DispatchedKeyspace = state.DispatchedKeyspace.AddInt64(dispatchedAmount)
		// Update job's BASE keyspace tracking (for --skip/--limit)
		// This is the KEY FIX: track the END position so next agent gets correct start
		state.DispatchedBaseKeyspace = keyspaceEnd
	}
	state.ChunkNumber++

	debug.Log("Calculated keyspace chunk (base keyspace for --skip/--limit)", map[string]interface{}{
		"agent_id":                   plan.AgentID,
		"job_id":                     plan.JobExecution.ID,
		"chunk_number":               plan.ChunkNumber,
		"keyspace_start":             keyspaceStart,
		"keyspace_end":               keyspaceEnd,
		"chunk_size":                 dispatchedAmount,
		"base_keyspace":              totalKeyspace,
		"is_keyspace_split":          plan.IsKeyspaceSplit,
		"benchmark_speed":            plan.BenchmarkSpeed,
		"updated_base_keyspace_end":  keyspaceEnd,
	})

	return nil
}
