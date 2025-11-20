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
	IsRuleSplit     bool
	RuleStartIndex  int
	RuleEndIndex    int
	RuleFilePath    string // Source rule file path
	ChunkNumber     int

	// Effective keyspace
	EffectiveKeyspaceStart int64
	EffectiveKeyspaceEnd   int64

	AttackCmd string // Pre-built, will need rule path replacement for rule splits

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

	// Create job lookup map
	jobMap := make(map[uuid.UUID]*models.JobExecutionWithWork)
	for i := range jobsWithWork {
		jobMap[jobsWithWork[i].ID] = &jobsWithWork[i]
	}

	// Track planning state for each job
	jobStates := make(map[uuid.UUID]*JobPlanningState)

	// Process jobs in priority order (jobs are already sorted)
	for _, job := range jobsWithWork {
		// Initialize job state
		state := &JobPlanningState{
			JobExecution:       &job.JobExecution,
			DispatchedKeyspace: job.DispatchedKeyspace,
			IsExhausted:        false,
		}

		// Get base keyspace for rule splitting calculations
		if job.BaseKeyspace != nil {
			state.BaseKeyspace = *job.BaseKeyspace
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

		jobStates[job.ID] = state

		debug.Log("Initialized job planning state", map[string]interface{}{
			"job_id":              job.ID,
			"dispatched_keyspace": state.DispatchedKeyspace,
			"next_rule_index":     state.NextRuleIndex,
			"total_rules":         state.TotalRules,
			"chunk_number":        state.ChunkNumber,
		})
	}

	// Process agents by job priority
	for _, job := range jobsWithWork {
		state := jobStates[job.ID]
		if state.IsExhausted {
			debug.Log("Job already exhausted, skipping", map[string]interface{}{
				"job_id": job.ID,
			})
			continue
		}

		// Get agents reserved for this job
		var agentsForJob []int
		for agentID, jobID := range reservedAgents {
			if jobID == job.ID {
				agentsForJob = append(agentsForJob, agentID)
			}
		}

		debug.Log("Processing agents for job", map[string]interface{}{
			"job_id":      job.ID,
			"agent_count": len(agentsForJob),
		})

		// Process each agent for this job
		for _, agentID := range agentsForJob {
			if state.IsExhausted {
				debug.Log("Job exhausted during planning, skipping remaining agents", map[string]interface{}{
					"job_id": job.ID,
				})
				break
			}

			plan, err := s.createSingleTaskPlan(ctx, agentID, state, jobsWithWork, jobStates)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to create plan for agent %d: %w", agentID, err))
				continue
			}

			if plan != nil {
				plans = append(plans, *plan)
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

		// Verify agent has benchmark for this job before reassigning
		benchmark, err := s.jobExecutionService.benchmarkRepo.GetAgentBenchmark(
			ctx, agentID, currentState.JobExecution.AttackMode, hashlist.HashTypeID)

		if err != nil || benchmark == nil {
			debug.Log("Agent missing benchmark for pending task job, skipping reassignment", map[string]interface{}{
				"agent_id":  agentID,
				"job_id":    currentState.JobExecution.ID,
				"hash_type": hashlist.HashTypeID,
			})
			// Don't reassign - continue to new chunk logic below
		} else {
			// Create plan from existing pending task
			plan := &TaskAssignmentPlan{
				AgentID:        agentID,
				Agent:          agent,
				JobExecution:   currentState.JobExecution,
				ChunkDuration:  pendingTask.ChunkDuration,
				BenchmarkSpeed: *pendingTask.BenchmarkSpeed,
				ExistingTask:   pendingTask, // Mark as pending task reassignment

				// Copy keyspace fields from pending task
				KeyspaceStart:          pendingTask.KeyspaceStart,
				KeyspaceEnd:            pendingTask.KeyspaceEnd,
				EffectiveKeyspaceStart: *pendingTask.EffectiveKeyspaceStart,
				EffectiveKeyspaceEnd:   *pendingTask.EffectiveKeyspaceEnd,
				AttackCmd:              pendingTask.AttackCmd,
				ChunkNumber:            pendingTask.ChunkNumber,
			}

			// For rule-split tasks, copy rule fields and get source rule path
			if pendingTask.IsRuleSplitTask {
				plan.IsRuleSplit = true
				plan.RuleStartIndex = *pendingTask.RuleStartIndex
				plan.RuleEndIndex = *pendingTask.RuleEndIndex

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

	// PRIORITY 2: No pending tasks OR agent lacks benchmark - create NEW chunk
	// Check if agent has valid benchmark for this job
	benchmark, err := s.jobExecutionService.benchmarkRepo.GetAgentBenchmark(
		ctx, agentID, currentState.JobExecution.AttackMode, hashlist.HashTypeID)

	// If no benchmark, try to find another job this agent can work on
	targetJob := currentState.JobExecution
	targetState := currentState

	if err != nil || benchmark == nil {
		debug.Log("Agent missing benchmark for assigned job, checking alternatives", map[string]interface{}{
			"agent_id":   agentID,
			"job_id":     currentState.JobExecution.ID,
			"hash_type":  hashlist.HashTypeID,
		})

		// Try to find another job with valid benchmark
		foundAlternative := false
		for _, otherJob := range allJobs {
			if otherJob.ID == currentState.JobExecution.ID {
				continue // Skip current job
			}

			otherState := jobStates[otherJob.ID]
			if otherState.IsExhausted {
				continue // Skip exhausted jobs
			}

			// Get hashlist for other job
			otherHashlist, err := s.jobExecutionService.hashlistRepo.GetByID(ctx, otherJob.HashlistID)
			if err != nil {
				continue
			}

			// Check if agent has benchmark for this job
			otherBenchmark, err := s.jobExecutionService.benchmarkRepo.GetAgentBenchmark(
				ctx, agentID, otherJob.AttackMode, otherHashlist.HashTypeID)

			if err == nil && otherBenchmark != nil {
				// Found a job this agent can work on
				targetJob = &otherJob.JobExecution
				targetState = otherState
				benchmark = otherBenchmark
				hashlist = otherHashlist
				foundAlternative = true

				debug.Info("Reassigned agent to alternative job", map[string]interface{}{
					"agent_id":       agentID,
					"original_job":   currentState.JobExecution.ID,
					"alternative_job": otherJob.ID,
					"hash_type":      otherHashlist.HashTypeID,
				})
				break
			}
		}

		if !foundAlternative {
			// No valid benchmark for any job
			return &TaskAssignmentPlan{
				AgentID:        agentID,
				SkipAssignment: true,
				SkipReason:     fmt.Sprintf("No valid benchmark for any available job (agent_id=%d)", agentID),
			}, nil
		}
	}

	// Get chunk duration
	chunkDuration := 1200 // Default 20 minutes
	if duration, err := s.getChunkDuration(ctx, targetJob); err == nil {
		chunkDuration = duration
	}

	// Calculate chunk based on job type
	plan := &TaskAssignmentPlan{
		AgentID:        agentID,
		Agent:          agent,
		JobExecution:   targetJob,
		ChunkDuration:  chunkDuration,
		BenchmarkSpeed: benchmark.Speed,
	}

	if targetJob.UsesRuleSplitting {
		// Rule splitting chunk calculation
		err = s.calculateRuleSplitChunk(ctx, plan, targetState, hashlist)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate rule split chunk: %w", err)
		}
	} else {
		// Regular keyspace chunking
		err = s.calculateKeyspaceChunk(ctx, plan, targetState)
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
		}

		// Add rule splitting fields if applicable
		if plan.IsRuleSplit {
			task.IsRuleSplitTask = true
			task.RuleStartIndex = &plan.RuleStartIndex
			task.RuleEndIndex = &plan.RuleEndIndex
			task.RuleChunkPath = &ruleChunkPath
		}

		err = s.jobExecutionService.jobTaskRepo.Create(ctx, task)
		if err != nil {
			result.Error = fmt.Errorf("failed to create task: %w", err)
			return result
		}

		debug.Log("Created new task in database", map[string]interface{}{
			"agent_id": plan.AgentID,
			"task_id":  task.ID,
			"job_id":   plan.JobExecution.ID,
		})
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
	// rulesPerSecond = benchmarkSpeed / baseKeyspace (how many complete wordlist passes per second)
	// rulesPerChunk = rulesPerSecond * chunkDuration
	rulesPerChunk := 100 // Default if calculation fails
	if state.BaseKeyspace > 0 && plan.BenchmarkSpeed > 0 {
		rulesPerSecond := float64(plan.BenchmarkSpeed) / float64(state.BaseKeyspace)
		rulesPerChunk = int(rulesPerSecond * float64(plan.ChunkDuration))
		if rulesPerChunk < 1 {
			rulesPerChunk = 1 // At least one rule per chunk
		}
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

	// Build attack command
	attackCmd, err := s.jobExecutionService.buildAttackCommand(ctx, nil, plan.JobExecution)
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
func (s *JobSchedulingService) calculateKeyspaceChunk(
	ctx context.Context,
	plan *TaskAssignmentPlan,
	state *JobPlanningState,
) error {
	// Check if job has total keyspace
	if plan.JobExecution.TotalKeyspace == nil {
		return fmt.Errorf("job has no total keyspace")
	}

	totalKeyspace := *plan.JobExecution.TotalKeyspace
	remainingKeyspace := totalKeyspace - state.DispatchedKeyspace

	if remainingKeyspace <= 0 {
		state.IsExhausted = true
		plan.SkipAssignment = true
		plan.SkipReason = fmt.Sprintf("No remaining keyspace for job %s", plan.JobExecution.ID)
		return nil
	}

	// Calculate chunk size based on benchmark and desired duration
	desiredChunkSize := int64(plan.ChunkDuration) * plan.BenchmarkSpeed

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

	keyspaceStart := state.DispatchedKeyspace
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

	// Build attack command
	attackCmd, err := s.jobExecutionService.buildAttackCommand(ctx, nil, plan.JobExecution)
	if err != nil {
		return fmt.Errorf("failed to build attack command: %w", err)
	}

	// Update plan
	plan.KeyspaceStart = keyspaceStart
	plan.KeyspaceEnd = keyspaceEnd
	plan.AttackCmd = attackCmd
	plan.ChunkNumber = state.ChunkNumber

	// Update state for next agent
	dispatchedAmount := keyspaceEnd - keyspaceStart
	state.DispatchedKeyspace += dispatchedAmount
	state.ChunkNumber++

	debug.Log("Calculated keyspace chunk", map[string]interface{}{
		"agent_id":      plan.AgentID,
		"job_id":        plan.JobExecution.ID,
		"chunk_number":  plan.ChunkNumber,
		"keyspace_start": keyspaceStart,
		"keyspace_end":  keyspaceEnd,
		"chunk_size":    dispatchedAmount,
	})

	return nil
}
