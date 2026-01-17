package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

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

	// Rule splitting (calculated ranges, file created in Phase 2)
	IsRuleSplit      bool
	IsKeyspaceSplit  bool   // Whether task uses keyspace splitting (--skip/--limit)
	RuleStartIndex   int
	RuleEndIndex     int
	RuleFilePath     string // Source rule file path
	ChunkNumber      int

	// Increment layer support
	IncrementLayerID *uuid.UUID // If set, task belongs to this increment layer
	LayerMask        string      // The specific mask for this layer (overrides job mask)

	// Effective keyspace
	EffectiveKeyspaceStart int64
	EffectiveKeyspaceEnd   int64

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
	DispatchedKeyspace  int64
	NextRuleIndex       int
	TotalRules          int
	RuleFilePath        string
	IsExhausted         bool
	ChunkNumber         int
	BaseKeyspace        int64

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

		// Get base keyspace for rule splitting calculations
		if job.BaseKeyspace != nil {
			state.BaseKeyspace = *job.BaseKeyspace
		}

		// Initialize BASE keyspace tracking from DB ONCE per job (not per agent)
		// This prevents the race condition where all agents get the same starting value
		// because they all query DB before any tasks are created.
		if !job.UsesRuleSplitting {
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
		}

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
				"job_id":                        actualJobID,
				"layer_id":                      specificLayer.ID,
				"layer_index":                   specificLayer.LayerIndex,
				"layer_mask":                    specificLayer.Mask,
				"effective_keyspace":            specificLayer.EffectiveKeyspace,
				"dispatched_keyspace":           specificLayer.DispatchedKeyspace,
				"layer_dispatched_base_keyspace": maxLayerBaseEnd,
			})
		}

		// Get next rule index for rule splitting jobs
		if job.UsesRuleSplitting {
			maxRuleEnd, err := s.jobExecutionService.jobTaskRepo.GetMaxRuleEndIndex(ctx, job.ID)
			if err == nil && maxRuleEnd != nil {
				state.NextRuleIndex = *maxRuleEnd
			}

			// Get total rules
			if len(job.RuleIDs) > 0 {
				rulePath, err := s.jobExecutionService.resolveRulePath(ctx, job.RuleIDs[0])
				if err == nil {
					totalRules, err := s.jobExecutionService.ruleSplitManager.CountRules(ctx, rulePath)
					if err == nil {
						state.TotalRules = totalRules
						state.RuleFilePath = rulePath
					}
				}
			}
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
			"dispatched_keyspace": state.DispatchedKeyspace,
			"next_rule_index":     state.NextRuleIndex,
			"total_rules":         state.TotalRules,
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
			effectiveStart := int64(0)
			if pendingTask.EffectiveKeyspaceStart != nil {
				effectiveStart = *pendingTask.EffectiveKeyspaceStart
			}
			effectiveEnd := int64(0)
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
					effectiveRange := *pendingTask.EffectiveKeyspaceEnd - *pendingTask.EffectiveKeyspaceStart
					multiplier := float64(effectiveRange) / float64(baseRange)

					// New effective start = original effective start + (processed base * multiplier)
					processedBase := pendingTask.KeyspaceProcessed - pendingTask.KeyspaceStart
					recoveryEffectiveStart = *pendingTask.EffectiveKeyspaceStart + int64(float64(processedBase)*multiplier)
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
				AttackCmd:              pendingTask.AttackCmd,
				ChunkNumber:            pendingTask.ChunkNumber,

				// Salt-aware chunk calculation
				IsSalted:      isSalted,
				TotalHashes:   hashlist.TotalHashes,
				CrackedHashes: hashlist.CrackedHashes,
			}

			// For rule-split tasks, copy rule fields and get source rule path
			if pendingTask.IsRuleSplitTask {
				plan.IsRuleSplit = true

				// Handle nullable RuleIndex fields - should always be set for rule-split tasks
				if pendingTask.RuleStartIndex != nil && pendingTask.RuleEndIndex != nil {
					plan.RuleStartIndex = *pendingTask.RuleStartIndex
					plan.RuleEndIndex = *pendingTask.RuleEndIndex
				} else {
					debug.Warning("Pending rule-split task missing rule index fields", map[string]interface{}{
						"task_id": pendingTask.ID,
					})
					// Skip this task - it's corrupted
					// Fall through to new chunk logic
					goto createNewChunk
				}

				// Get source rule file path from job
				if len(currentState.JobExecution.RuleIDs) > 0 {
					rulePath, err := s.jobExecutionService.resolveRulePath(ctx, currentState.JobExecution.RuleIDs[0])
					if err == nil {
						plan.RuleFilePath = rulePath
					} else {
						debug.Warning("Failed to resolve rule path for pending task: %v", err)
					}
				}

				debug.Info("Reassigning pending rule-split task", map[string]interface{}{
					"task_id":    pendingTask.ID,
					"rule_start": plan.RuleStartIndex,
					"rule_end":   plan.RuleEndIndex,
				})
			}

			return plan, nil
		}
	}

createNewChunk:
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

	if currentState.JobExecution.UsesRuleSplitting {
		// Rule splitting chunk calculation
		err = s.calculateRuleSplitChunk(ctx, plan, currentState, hashlist)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate rule split chunk: %w", err)
		}
	} else {
		// Regular keyspace chunking
		err = s.calculateKeyspaceChunk(ctx, plan, currentState)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate keyspace chunk: %w", err)
		}
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

	// Step 1: Create rule chunk file if needed (BEFORE file sync)
	var ruleChunkPath string
	if plan.IsRuleSplit {
		chunk, err := s.jobExecutionService.ruleSplitManager.CreateSingleRuleChunk(
			ctx,
			plan.JobExecution.ID,
			plan.RuleFilePath,
			plan.RuleStartIndex,
			plan.RuleEndIndex-plan.RuleStartIndex,
		)
		if err != nil {
			result.Error = fmt.Errorf("failed to create rule chunk: %w", err)
			return result
		}
		ruleChunkPath = chunk.Path

		// Replace rule path in attack command
		plan.AttackCmd = replaceRulePath(plan.AttackCmd, plan.RuleFilePath, ruleChunkPath)

		debug.Log("Created rule chunk file", map[string]interface{}{
			"agent_id":    plan.AgentID,
			"chunk_path":  ruleChunkPath,
			"rule_start":  plan.RuleStartIndex,
			"rule_end":    plan.RuleEndIndex,
		})
	}

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
		task.AttackCmd = plan.AttackCmd // Updated with new chunk path if rule-split
		now := time.Now()
		task.AssignedAt = &now

		// Apply recovery optimization - update keyspace fields from checkpoint
		task.KeyspaceStart = plan.KeyspaceStart     // May be updated from keyspace_processed
		task.IsKeyspaceSplit = plan.IsKeyspaceSplit // Ensure split flag is set
		task.EffectiveKeyspaceStart = &plan.EffectiveKeyspaceStart
		task.EffectiveKeyspaceEnd = &plan.EffectiveKeyspaceEnd

		// Update rule chunk path if this is a rule-split task
		if plan.IsRuleSplit && ruleChunkPath != "" {
			task.RuleChunkPath = &ruleChunkPath
		}

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
			AttackCmd:              plan.AttackCmd,
			KeyspaceStart:          plan.KeyspaceStart,
			KeyspaceEnd:            plan.KeyspaceEnd,
			KeyspaceProcessed:      0,
			EffectiveKeyspaceStart: &plan.EffectiveKeyspaceStart,
			EffectiveKeyspaceEnd:   &plan.EffectiveKeyspaceEnd,
			ChunkNumber:            plan.ChunkNumber,
			ChunkDuration:          plan.ChunkDuration,
			BenchmarkSpeed:         &plan.BenchmarkSpeed,
			IncrementLayerID:       plan.IncrementLayerID, // Set layer ID for increment mode tasks
		}

		// Add rule splitting fields if applicable
		if plan.IsRuleSplit {
			task.IsRuleSplitTask = true
			task.RuleStartIndex = &plan.RuleStartIndex
			task.RuleEndIndex = &plan.RuleEndIndex
			task.RuleChunkPath = &ruleChunkPath
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
				dispatchedAmount,
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

// replaceRulePath replaces the rule file path in the attack command with the chunk path
func replaceRulePath(attackCmd, oldPath, newPath string) string {
	// Replace first occurrence only (attack command should only have one rule file)
	return strings.Replace(attackCmd, oldPath, newPath, 1)
}

// calculateRuleSplitChunk calculates rule range for a rule splitting job
func (s *JobSchedulingService) calculateRuleSplitChunk(
	ctx context.Context,
	plan *TaskAssignmentPlan,
	state *JobPlanningState,
	hashlist interface{},
) error {
	// Check if all rules have been dispatched
	if state.TotalRules > 0 && state.NextRuleIndex >= state.TotalRules {
		state.IsExhausted = true
		plan.SkipAssignment = true
		plan.SkipReason = fmt.Sprintf("All rules dispatched for job %s", plan.JobExecution.ID)
		return nil
	}

	// Calculate how many rules this agent can process in the chunk duration
	// Using effective_keyspace (which includes salt multiplication from benchmark) for accurate timing
	// keyspacePerRule = effective_keyspace / total_rules
	// chunkKeyspace = benchmarkSpeed * chunkDuration
	// rulesPerChunk = chunkKeyspace / keyspacePerRule
	rulesPerChunk := 100 // Default if calculation fails
	if state.JobExecution.EffectiveKeyspace != nil && *state.JobExecution.EffectiveKeyspace > 0 && plan.BenchmarkSpeed > 0 {
		totalRules := state.JobExecution.MultiplicationFactor
		if totalRules == 0 {
			totalRules = 1
		}

		// Keyspace per rule (base calculation without salts)
		keyspacePerRule := float64(*state.JobExecution.EffectiveKeyspace) / float64(totalRules)

		// For salted hash types, multiply keyspace by remaining hashes (each hash = 1 salt)
		// This aligns with the salt-aware benchmark speed which already accounts for salt count
		if plan.IsSalted {
			remainingHashes := plan.TotalHashes - plan.CrackedHashes
			if remainingHashes > 0 {
				originalKeyspacePerRule := keyspacePerRule
				keyspacePerRule *= float64(remainingHashes)
				debug.Log("Adjusted keyspace_per_rule for salted hash type", map[string]interface{}{
					"original_keyspace_per_rule": originalKeyspacePerRule,
					"salt_count":                 remainingHashes,
					"adjusted_keyspace_per_rule": keyspacePerRule,
				})
			}
		}

		// Keyspace we can process in target duration
		chunkKeyspace := float64(plan.BenchmarkSpeed) * float64(plan.ChunkDuration)

		// Rules per chunk
		rulesPerChunk = int(chunkKeyspace / keyspacePerRule)
		if rulesPerChunk < 1 {
			rulesPerChunk = 1 // At least one rule per chunk
		}

		debug.Log("Rule chunk calculation", map[string]interface{}{
			"effective_keyspace": *state.JobExecution.EffectiveKeyspace,
			"total_rules":        totalRules,
			"keyspace_per_rule":  keyspacePerRule,
			"benchmark_speed":    plan.BenchmarkSpeed,
			"chunk_duration":     plan.ChunkDuration,
			"rules_per_chunk":    rulesPerChunk,
		})
	}

	// Get fluctuation settings
	fluctuationSetting, _ := s.systemSettingsRepo.GetSetting(ctx, "chunk_fluctuation_percentage")
	fluctuationPercent := 20 // Default 20%
	if fluctuationSetting != nil && fluctuationSetting.Value != nil {
		var parsed int
		_, err := fmt.Sscanf(*fluctuationSetting.Value, "%d", &parsed)
		if err == nil {
			fluctuationPercent = parsed
		}
	}

	fluctuationThreshold := int(float64(rulesPerChunk) * float64(fluctuationPercent) / 100.0)
	ruleStart := state.NextRuleIndex
	ruleEnd := ruleStart + rulesPerChunk

	if ruleEnd >= state.TotalRules {
		ruleEnd = state.TotalRules
	} else {
		// Check if remaining rules would be too small
		remainingAfterChunk := state.TotalRules - ruleEnd
		if remainingAfterChunk <= fluctuationThreshold {
			// Merge the final small chunk
			ruleEnd = state.TotalRules
			debug.Log("Merging final rule chunk to avoid small remainder", map[string]interface{}{
				"normal_chunk_size":   rulesPerChunk,
				"remaining_rules":     remainingAfterChunk,
				"threshold":           fluctuationThreshold,
				"merged_chunk_size":   ruleEnd - ruleStart,
				"percent_over_normal": float64(ruleEnd-ruleStart-rulesPerChunk) / float64(rulesPerChunk) * 100,
			})
		}
	}

	// Build attack command (use plan.LayerMask if this is an increment layer task)
	attackCmd, err := s.jobExecutionService.buildAttackCommand(ctx, nil, plan.JobExecution, plan.LayerMask)
	if err != nil {
		return fmt.Errorf("failed to build attack command: %w", err)
	}

	// Calculate effective keyspace
	effectiveKeyspaceStart := int64(0)
	previousChunksActual, err := s.jobExecutionService.GetPreviousChunksActualKeyspace(ctx, plan.JobExecution.ID, state.ChunkNumber)
	if err == nil && previousChunksActual > 0 {
		effectiveKeyspaceStart = previousChunksActual
	} else {
		// Fall back to estimated based on base keyspace
		effectiveKeyspaceStart = state.BaseKeyspace * int64(ruleStart)
	}

	rulesInChunk := ruleEnd - ruleStart
	estimatedChunkKeyspace := state.BaseKeyspace * int64(rulesInChunk)
	effectiveKeyspaceEnd := effectiveKeyspaceStart + estimatedChunkKeyspace

	// Update plan
	plan.IsRuleSplit = true
	plan.RuleStartIndex = ruleStart
	plan.RuleEndIndex = ruleEnd
	plan.RuleFilePath = state.RuleFilePath
	plan.ChunkNumber = state.ChunkNumber
	plan.KeyspaceStart = 0
	plan.KeyspaceEnd = state.BaseKeyspace
	plan.EffectiveKeyspaceStart = effectiveKeyspaceStart
	plan.EffectiveKeyspaceEnd = effectiveKeyspaceEnd
	plan.AttackCmd = attackCmd // Will replace rule path during execution

	// Update state for next agent
	state.NextRuleIndex = ruleEnd
	state.ChunkNumber++
	state.DispatchedKeyspace += estimatedChunkKeyspace

	debug.Log("Calculated rule split chunk", map[string]interface{}{
		"agent_id":         plan.AgentID,
		"job_id":           plan.JobExecution.ID,
		"chunk_number":     plan.ChunkNumber,
		"rule_start":       ruleStart,
		"rule_end":         ruleEnd,
		"rules_in_chunk":   rulesInChunk,
		"effective_start":  effectiveKeyspaceStart,
		"effective_end":    effectiveKeyspaceEnd,
	})

	return nil
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
			// Fallback to effective if base not available (shouldn't happen normally)
			totalKeyspace = *state.CurrentLayer.EffectiveKeyspace
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
		// BaseKeyspace represents actual password candidates that hashcat will test
		if plan.JobExecution.BaseKeyspace != nil {
			totalKeyspace = *plan.JobExecution.BaseKeyspace
		} else if plan.JobExecution.TotalKeyspace != nil {
			// Fallback to TotalKeyspace if BaseKeyspace not set
			totalKeyspace = *plan.JobExecution.TotalKeyspace
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
	var effectiveKeyspace int64
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
	if effectiveKeyspace > 0 && totalKeyspace > 0 {
		// multiplier = effective_keyspace / base_keyspace
		// For mask attacks with many hashes, multiplier ≈ hash_count
		// For salted hashes, effective_keyspace already includes salt factor (applied at job creation)
		// So multiplier = (base × rules × salts) / base = rules × salts
		multiplier := float64(effectiveKeyspace) / float64(totalKeyspace)

		// Convert benchmark speed (effective H/s) to base keyspace per second
		// base_per_second = effective_per_second / multiplier
		basePerSecond := float64(plan.BenchmarkSpeed) / multiplier

		// Calculate desired base keyspace chunk
		desiredChunkSize = int64(float64(plan.ChunkDuration) * basePerSecond)

		debug.Log("Converted benchmark speed to base keyspace rate", map[string]interface{}{
			"job_id":              plan.JobExecution.ID,
			"effective_keyspace":  effectiveKeyspace,
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

	// Get fluctuation percentage setting
	fluctuationSetting, _ := s.systemSettingsRepo.GetSetting(ctx, "chunk_fluctuation_percentage")
	fluctuationPercentage := 20 // Default
	if fluctuationSetting != nil && fluctuationSetting.Value != nil {
		var parsed int
		_, err := fmt.Sscanf(*fluctuationSetting.Value, "%d", &parsed)
		if err == nil {
			fluctuationPercentage = parsed
		}
	}

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
	if effectiveKeyspace > 0 && totalKeyspace > 0 {
		// Scale effective keyspace proportionally to base keyspace chunk
		// effective_chunk_start = (base_start / base_total) * effective_total
		// effective_chunk_end = (base_end / base_total) * effective_total
		plan.EffectiveKeyspaceStart = int64(float64(keyspaceStart) * float64(effectiveKeyspace) / float64(totalKeyspace))
		plan.EffectiveKeyspaceEnd = int64(float64(keyspaceEnd) * float64(effectiveKeyspace) / float64(totalKeyspace))

		// For increment layer tasks with LayerIndex > 1, add cumulative offset from previous layers
		// This ensures effective_keyspace_start/end are GLOBAL job positions, not layer-relative
		// which is required for the progress bar visualization to work correctly
		if state.CurrentLayer != nil && state.CurrentLayer.LayerIndex > 1 {
			cumulativeOffset, err := s.jobExecutionService.jobIncrementLayerRepo.GetCumulativeEffectiveKeyspace(
				ctx, state.CurrentLayer.JobExecutionID, state.CurrentLayer.LayerIndex)
			if err != nil {
				debug.Warning("Failed to get cumulative effective keyspace for layer %d: %v", state.CurrentLayer.LayerIndex, err)
				// Continue without offset - visualization may be slightly off but job will still work
			} else if cumulativeOffset > 0 {
				plan.EffectiveKeyspaceStart += cumulativeOffset
				plan.EffectiveKeyspaceEnd += cumulativeOffset
				debug.Log("Added cumulative layer offset to effective keyspace", map[string]interface{}{
					"layer_id":              state.CurrentLayer.ID,
					"layer_index":           state.CurrentLayer.LayerIndex,
					"cumulative_offset":     cumulativeOffset,
					"adjusted_start":        plan.EffectiveKeyspaceStart,
					"adjusted_end":          plan.EffectiveKeyspaceEnd,
				})
			}
		}

		debug.Log("Calculated proportional effective keyspace for chunk", map[string]interface{}{
			"job_id":                 plan.JobExecution.ID,
			"base_chunk_start":       keyspaceStart,
			"base_chunk_end":         keyspaceEnd,
			"effective_chunk_start":  plan.EffectiveKeyspaceStart,
			"effective_chunk_end":    plan.EffectiveKeyspaceEnd,
			"total_base_keyspace":    totalKeyspace,
			"total_effective_keyspace": effectiveKeyspace,
		})
	}

	// Update state for next agent (in-memory tracking during planning)
	dispatchedAmount := keyspaceEnd - keyspaceStart
	if state.CurrentLayer != nil {
		// Update layer's in-memory dispatched keyspace (EFFECTIVE units)
		state.CurrentLayer.DispatchedKeyspace += dispatchedAmount
		// Update layer's BASE keyspace tracking (for --skip/--limit)
		// This is the KEY FIX: track the END position so next agent gets correct start
		state.LayerDispatchedBaseKeyspace[state.CurrentLayer.ID] = keyspaceEnd
	} else {
		// Update job's in-memory dispatched keyspace (EFFECTIVE units)
		state.DispatchedKeyspace += dispatchedAmount
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
		"total_keyspace":             totalKeyspace,
		"is_keyspace_split":          plan.IsKeyspaceSplit,
		"benchmark_speed":            plan.BenchmarkSpeed,
		"updated_base_keyspace_end":  keyspaceEnd,
	})

	return nil
}
