package services

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// BenchmarkPlan represents the complete benchmark execution plan for a scheduling cycle
type BenchmarkPlan struct {
	ForcedBenchmarks         []ForcedBenchmarkTask
	AgentBenchmarks          []AgentBenchmarkTask
	ForcedBenchmarkAgentJobs map[int]uuid.UUID // agentID -> jobID for prioritization
}

// ForcedBenchmarkTask represents a forced benchmark for a new job needing accurate keyspace
type ForcedBenchmarkTask struct {
	AgentID    int
	JobID      uuid.UUID
	HashType   int
	AttackMode models.AttackMode
	Priority   int
}

// AgentBenchmarkTask represents an agent speed benchmark
type AgentBenchmarkTask struct {
	AgentID    int
	JobID      uuid.UUID // Representative job for parameters
	HashType   int
	AttackMode models.AttackMode
}

// JobHashTypeInfo contains hash type information for a job
type JobHashTypeInfo struct {
	JobID                uuid.UUID
	HashType             int
	AttackMode           models.AttackMode
	Priority             int
	CreatedAt            time.Time
	NeedsForcedBenchmark bool
}

// CreateBenchmarkPlan analyzes the system state and creates an intelligent benchmark execution plan
func (s *JobSchedulingService) CreateBenchmarkPlan(
	ctx context.Context,
	availableAgents []models.Agent,
	jobsWithWork []models.JobExecutionWithWork,
) (*BenchmarkPlan, error) {
	debug.Log("Creating benchmark plan", map[string]interface{}{
		"available_agents": len(availableAgents),
		"jobs_with_work":   len(jobsWithWork),
	})

	// 1. Get benchmark cache duration from system settings
	cacheDuration := 168 * time.Hour // Default 7 days
	if setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_cache_duration_hours"); err == nil && setting.Value != nil {
		if hours, err := strconv.Atoi(*setting.Value); err == nil {
			cacheDuration = time.Duration(hours) * time.Hour
		}
	}

	// 2. Collect hash type info for all jobs with pending work
	jobHashInfo, err := s.collectJobHashTypeInfo(ctx, jobsWithWork)
	if err != nil {
		return nil, fmt.Errorf("failed to collect job hash type info: %w", err)
	}

	if len(jobHashInfo) == 0 {
		debug.Log("No jobs with hash type info, returning empty plan", nil)
		return &BenchmarkPlan{
			ForcedBenchmarks:         []ForcedBenchmarkTask{},
			AgentBenchmarks:          []AgentBenchmarkTask{},
			ForcedBenchmarkAgentJobs: make(map[int]uuid.UUID),
		}, nil
	}

	// 3. Query valid benchmark status for all agents
	agentBenchmarkStatus, err := s.buildAgentBenchmarkStatus(ctx, availableAgents, jobHashInfo, cacheDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent benchmark status: %w", err)
	}

	// 4. Allocate forced benchmarks (for new jobs only)
	forcedTasks, usedAgents := s.allocateForcedBenchmarks(jobHashInfo, availableAgents, agentBenchmarkStatus)

	debug.Log("Allocated forced benchmarks", map[string]interface{}{
		"forced_benchmark_count": len(forcedTasks),
		"agents_used":            len(usedAgents),
	})

	// 5. Build unique hash type list from ALL jobs (including forced ones)
	uniqueHashTypes := s.buildUniqueHashTypeList(jobHashInfo)

	debug.Log("Built unique hash type list", map[string]interface{}{
		"unique_hash_types": len(uniqueHashTypes),
	})

	// 6. Round-robin allocate agent speed benchmarks for remaining agents
	agentTasks := s.allocateAgentBenchmarks(availableAgents, usedAgents, uniqueHashTypes, agentBenchmarkStatus)

	debug.Log("Allocated agent speed benchmarks", map[string]interface{}{
		"agent_benchmark_count": len(agentTasks),
	})

	return &BenchmarkPlan{
		ForcedBenchmarks:         forcedTasks,
		AgentBenchmarks:          agentTasks,
		ForcedBenchmarkAgentJobs: usedAgents,
	}, nil
}

// collectJobHashTypeInfo gathers hash type information for all jobs with pending work
func (s *JobSchedulingService) collectJobHashTypeInfo(
	ctx context.Context,
	jobsWithWork []models.JobExecutionWithWork,
) ([]JobHashTypeInfo, error) {
	var jobHashInfo []JobHashTypeInfo

	for _, job := range jobsWithWork {
		// Get hashlist to find hash type
		hashlist, err := s.jobExecutionService.hashlistRepo.GetByID(ctx, job.HashlistID)
		if err != nil {
			debug.Warning("Failed to get hashlist %d for job %s: %v", job.HashlistID, job.ID, err)
			continue
		}
		if hashlist == nil {
			debug.Warning("Hashlist %d not found for job %s", job.HashlistID, job.ID)
			continue
		}

		// Check if job needs forced benchmark
		taskCount, err := s.jobExecutionService.jobTaskRepo.GetTaskCountForJob(ctx, job.ID)
		if err != nil {
			debug.Warning("Failed to get task count for job %s: %v", job.ID, err)
			taskCount = 1 // Assume has tasks if can't determine
		}

		needsForcedBenchmark := (taskCount == 0 && !job.IsAccurateKeyspace)

		jobHashInfo = append(jobHashInfo, JobHashTypeInfo{
			JobID:                job.ID,
			HashType:             hashlist.HashTypeID,
			AttackMode:           job.AttackMode,
			Priority:             job.Priority,
			CreatedAt:            job.CreatedAt,
			NeedsForcedBenchmark: needsForcedBenchmark,
		})
	}

	return jobHashInfo, nil
}

// buildAgentBenchmarkStatus queries which benchmarks each agent has that are still valid
func (s *JobSchedulingService) buildAgentBenchmarkStatus(
	ctx context.Context,
	availableAgents []models.Agent,
	jobHashInfo []JobHashTypeInfo,
	cacheDuration time.Duration,
) (map[int]map[string]bool, error) {
	agentBenchmarkStatus := make(map[int]map[string]bool)

	for _, agent := range availableAgents {
		agentBenchmarkStatus[agent.ID] = make(map[string]bool)

		// Check each unique (attackMode, hashType) combination
		checkedCombos := make(map[string]bool)

		for _, jobInfo := range jobHashInfo {
			key := fmt.Sprintf("%d_%d", jobInfo.AttackMode, jobInfo.HashType)

			if checkedCombos[key] {
				continue // Already checked this combo for this agent
			}
			checkedCombos[key] = true

			// Check if agent has recent valid benchmark
			isRecent, err := s.jobExecutionService.benchmarkRepo.IsRecentBenchmark(
				ctx,
				agent.ID,
				jobInfo.AttackMode,
				jobInfo.HashType,
				cacheDuration,
			)

			if err != nil {
				// Error checking - treat as not recent
				agentBenchmarkStatus[agent.ID][key] = false
				continue
			}

			agentBenchmarkStatus[agent.ID][key] = isRecent
		}
	}

	return agentBenchmarkStatus, nil
}

// allocateForcedBenchmarks assigns agents to jobs needing forced benchmarks
func (s *JobSchedulingService) allocateForcedBenchmarks(
	jobHashInfo []JobHashTypeInfo,
	availableAgents []models.Agent,
	agentBenchmarkStatus map[int]map[string]bool,
) ([]ForcedBenchmarkTask, map[int]uuid.UUID) {
	// Filter and sort jobs needing forced benchmarks
	var jobsNeedingForced []JobHashTypeInfo
	for _, job := range jobHashInfo {
		if job.NeedsForcedBenchmark {
			jobsNeedingForced = append(jobsNeedingForced, job)
		}
	}

	// Sort by priority DESC, then created_at ASC
	for i := 0; i < len(jobsNeedingForced); i++ {
		for j := i + 1; j < len(jobsNeedingForced); j++ {
			if jobsNeedingForced[i].Priority < jobsNeedingForced[j].Priority ||
				(jobsNeedingForced[i].Priority == jobsNeedingForced[j].Priority &&
					jobsNeedingForced[i].CreatedAt.After(jobsNeedingForced[j].CreatedAt)) {
				jobsNeedingForced[i], jobsNeedingForced[j] = jobsNeedingForced[j], jobsNeedingForced[i]
			}
		}
	}

	var forcedTasks []ForcedBenchmarkTask
	usedAgents := make(map[int]uuid.UUID)

	for _, job := range jobsNeedingForced {
		if len(usedAgents) >= len(availableAgents) {
			break // No more agents available
		}

		// Find best agent for this job
		// Prefer agents WITHOUT valid benchmark for this hash type
		key := fmt.Sprintf("%d_%d", job.AttackMode, job.HashType)
		var bestAgent *models.Agent

		// First pass: look for agent without this benchmark
		for i := range availableAgents {
			if usedAgents[availableAgents[i].ID] != uuid.Nil {
				continue // Already used
			}

			if !agentBenchmarkStatus[availableAgents[i].ID][key] {
				bestAgent = &availableAgents[i]
				break
			}
		}

		// Second pass: if all have it, just pick first available
		if bestAgent == nil {
			for i := range availableAgents {
				if usedAgents[availableAgents[i].ID] == uuid.Nil {
					bestAgent = &availableAgents[i]
					break
				}
			}
		}

		if bestAgent == nil {
			break // No agents available (shouldn't happen)
		}

		forcedTasks = append(forcedTasks, ForcedBenchmarkTask{
			AgentID:    bestAgent.ID,
			JobID:      job.JobID,
			HashType:   job.HashType,
			AttackMode: job.AttackMode,
			Priority:   job.Priority,
		})

		usedAgents[bestAgent.ID] = job.JobID
	}

	return forcedTasks, usedAgents
}

// buildUniqueHashTypeList creates a deduplicated list of hash types from all jobs
func (s *JobSchedulingService) buildUniqueHashTypeList(
	jobHashInfo []JobHashTypeInfo,
) []JobHashTypeInfo {
	// Map key: "attackMode_hashType" -> highest priority job with that combo
	uniqueMap := make(map[string]JobHashTypeInfo)

	for _, job := range jobHashInfo {
		key := fmt.Sprintf("%d_%d", job.AttackMode, job.HashType)

		existing, exists := uniqueMap[key]
		if !exists || job.Priority > existing.Priority {
			uniqueMap[key] = job
		}
	}

	// Convert map to slice and sort by priority DESC
	var uniqueList []JobHashTypeInfo
	for _, job := range uniqueMap {
		uniqueList = append(uniqueList, job)
	}

	// Sort by priority DESC
	for i := 0; i < len(uniqueList); i++ {
		for j := i + 1; j < len(uniqueList); j++ {
			if uniqueList[i].Priority < uniqueList[j].Priority {
				uniqueList[i], uniqueList[j] = uniqueList[j], uniqueList[i]
			}
		}
	}

	return uniqueList
}

// allocateAgentBenchmarks assigns agent speed benchmarks using round-robin distribution
func (s *JobSchedulingService) allocateAgentBenchmarks(
	availableAgents []models.Agent,
	usedAgents map[int]uuid.UUID,
	uniqueHashTypes []JobHashTypeInfo,
	agentBenchmarkStatus map[int]map[string]bool,
) []AgentBenchmarkTask {
	if len(uniqueHashTypes) == 0 {
		return []AgentBenchmarkTask{}
	}

	// Build map of which agents need which hash types
	hashTypeToAgentsNeeding := make(map[string][]int)

	for _, agent := range availableAgents {
		if usedAgents[agent.ID] != uuid.Nil {
			continue // Skip agents doing forced benchmarks
		}

		for _, htInfo := range uniqueHashTypes {
			key := fmt.Sprintf("%d_%d", htInfo.AttackMode, htInfo.HashType)

			// Check if agent has valid benchmark
			if !agentBenchmarkStatus[agent.ID][key] {
				hashTypeToAgentsNeeding[key] = append(hashTypeToAgentsNeeding[key], agent.ID)
			}
		}
	}

	// Round-robin assignment
	var agentTasks []AgentBenchmarkTask
	assignedAgents := make(map[int]bool)
	hashTypeIndex := 0
	maxIterations := len(availableAgents) * len(uniqueHashTypes) * 2 // Safety limit

	for iteration := 0; iteration < maxIterations; iteration++ {
		if len(assignedAgents) >= len(availableAgents)-len(usedAgents) {
			break // All remaining agents assigned
		}

		htInfo := uniqueHashTypes[hashTypeIndex%len(uniqueHashTypes)]
		key := fmt.Sprintf("%d_%d", htInfo.AttackMode, htInfo.HashType)

		agentsNeedingThis := hashTypeToAgentsNeeding[key]

		// Find first unassigned agent needing this hash type
		foundAgent := false
		for _, agentID := range agentsNeedingThis {
			if !assignedAgents[agentID] {
				agentTasks = append(agentTasks, AgentBenchmarkTask{
					AgentID:    agentID,
					JobID:      htInfo.JobID,
					HashType:   htInfo.HashType,
					AttackMode: htInfo.AttackMode,
				})
				assignedAgents[agentID] = true
				foundAgent = true
				break
			}
		}

		hashTypeIndex++

		// If we've gone through all hash types without finding an agent, stop
		if !foundAgent && hashTypeIndex%len(uniqueHashTypes) == 0 {
			break
		}
	}

	return agentTasks
}

// ExecuteBenchmarkPlan sends all benchmark requests in parallel
func (s *JobSchedulingService) ExecuteBenchmarkPlan(
	ctx context.Context,
	plan *BenchmarkPlan,
) error {
	if len(plan.ForcedBenchmarks) == 0 && len(plan.AgentBenchmarks) == 0 {
		return nil // Nothing to do
	}

	debug.Info("Executing benchmark plan", map[string]interface{}{
		"forced_benchmarks": len(plan.ForcedBenchmarks),
		"agent_benchmarks":  len(plan.AgentBenchmarks),
		"total_benchmarks":  len(plan.ForcedBenchmarks) + len(plan.AgentBenchmarks),
	})

	var wg sync.WaitGroup
	errChan := make(chan error, len(plan.ForcedBenchmarks)+len(plan.AgentBenchmarks))

	// Execute forced benchmarks in parallel
	for _, task := range plan.ForcedBenchmarks {
		wg.Add(1)
		go func(t ForcedBenchmarkTask) {
			defer wg.Done()

			if err := s.executeForcedBenchmark(ctx, t); err != nil {
				errChan <- fmt.Errorf("forced benchmark failed for agent %d: %w", t.AgentID, err)
			}
		}(task)
	}

	// Execute agent speed benchmarks in parallel
	for _, task := range plan.AgentBenchmarks {
		wg.Add(1)
		go func(t AgentBenchmarkTask) {
			defer wg.Done()

			if err := s.executeAgentBenchmark(ctx, t); err != nil {
				errChan <- fmt.Errorf("agent benchmark failed for agent %d: %w", t.AgentID, err)
			}
		}(task)
	}

	wg.Wait()
	close(errChan)

	// Log any errors but don't fail the whole operation
	for err := range errChan {
		debug.Error("Benchmark execution error: %v", err)
	}

	return nil
}

// executeForcedBenchmark sends a forced benchmark request for a specific job
func (s *JobSchedulingService) executeForcedBenchmark(ctx context.Context, task ForcedBenchmarkTask) error {
	// Get job execution
	job, err := s.jobExecutionService.GetJobExecutionByID(ctx, task.JobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// Get agent for metadata update
	agent, err := s.agentRepo.GetByID(ctx, task.AgentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Mark agent as having pending benchmark for this job
	if agent.Metadata == nil {
		agent.Metadata = make(map[string]string)
	}
	agent.Metadata["pending_benchmark_job"] = task.JobID.String()
	agent.Metadata["benchmark_requested_at"] = time.Now().Format(time.RFC3339)
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		debug.Warning("Failed to update agent metadata for benchmark: %v", err)
	}

	// Send benchmark request via WebSocket
	if s.wsIntegration == nil {
		return fmt.Errorf("WebSocket integration not available")
	}

	err = s.wsIntegration.RequestAgentBenchmark(ctx, task.AgentID, job)
	if err != nil {
		// Clear metadata on failure
		if agent.Metadata != nil {
			delete(agent.Metadata, "pending_benchmark_job")
			delete(agent.Metadata, "benchmark_requested_at")
			s.agentRepo.Update(ctx, agent)
		}
		return fmt.Errorf("failed to send benchmark request: %w", err)
	}

	debug.Info("Sent forced benchmark request", map[string]interface{}{
		"agent_id": task.AgentID,
		"job_id":   task.JobID,
	})

	return nil
}

// executeAgentBenchmark sends an agent speed benchmark request
func (s *JobSchedulingService) executeAgentBenchmark(ctx context.Context, task AgentBenchmarkTask) error {
	// Get job execution
	job, err := s.jobExecutionService.GetJobExecutionByID(ctx, task.JobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// Send benchmark request via WebSocket
	if s.wsIntegration == nil {
		return fmt.Errorf("WebSocket integration not available")
	}

	err = s.wsIntegration.RequestAgentBenchmark(ctx, task.AgentID, job)
	if err != nil {
		return fmt.Errorf("failed to send benchmark request: %w", err)
	}

	debug.Log("Sent agent speed benchmark request", map[string]interface{}{
		"agent_id":  task.AgentID,
		"job_id":    task.JobID,
		"hash_type": task.HashType,
	})

	return nil
}

// WaitForBenchmarks blocks until all benchmarks complete or timeout is reached
func (s *JobSchedulingService) WaitForBenchmarks(ctx context.Context) bool {
	// Get timeout from system settings
	baseTimeout := 180 * time.Second // Default 3 minutes
	if setting, err := s.systemSettingsRepo.GetSetting(ctx, "speedtest_timeout_seconds"); err == nil && setting.Value != nil {
		if seconds, err := strconv.Atoi(*setting.Value); err == nil {
			baseTimeout = time.Duration(seconds) * time.Second
		}
	}

	// Add 5 second buffer to ensure agents time out before we do
	timeout := baseTimeout + (5 * time.Second)

	deadline := time.Now().Add(timeout)
	checkInterval := 500 * time.Millisecond
	lastLogTime := time.Now()

	debug.Info("Waiting for benchmarks to complete", map[string]interface{}{
		"timeout_seconds": timeout.Seconds(),
	})

	for time.Now().Before(deadline) {
		var pendingCount int
		err := s.jobExecutionService.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM benchmark_requests
			WHERE completed_at IS NULL
		`).Scan(&pendingCount)

		if err != nil {
			debug.Error("Failed to check pending benchmarks: %v", err)
			return false
		}

		if pendingCount == 0 {
			elapsed := time.Since(deadline.Add(-timeout))
			debug.Info("All benchmarks completed", map[string]interface{}{
				"elapsed_seconds": elapsed.Seconds(),
			})
			return true
		}

		// Log progress every 5 seconds
		if time.Since(lastLogTime) >= 5*time.Second {
			debug.Log("Benchmarks in progress", map[string]interface{}{
				"pending_count":   pendingCount,
				"elapsed_seconds": time.Since(deadline.Add(-timeout)).Seconds(),
				"timeout_seconds": timeout.Seconds(),
			})
			lastLogTime = time.Now()
		}

		time.Sleep(checkInterval)
	}

	// Timeout reached - log which agents failed to respond
	rows, err := s.jobExecutionService.db.QueryContext(ctx, `
		SELECT agent_id, hash_type, attack_mode
		FROM benchmark_requests
		WHERE completed_at IS NULL
	`)
	if err == nil {
		defer rows.Close()
		var timedOutAgents []int
		for rows.Next() {
			var agentID, hashType int
			var attackMode string
			if err := rows.Scan(&agentID, &hashType, &attackMode); err == nil {
				timedOutAgents = append(timedOutAgents, agentID)
				debug.Warning("Agent %d benchmark timed out (hash_type=%d, attack_mode=%s)",
					agentID, hashType, attackMode)
			}
		}

		debug.Error("Benchmark timeout reached", map[string]interface{}{
			"timeout_seconds":  timeout.Seconds(),
			"timed_out_agents": timedOutAgents,
		})
	}

	return false
}

// InsertBenchmarkRequests inserts benchmark request records into the tracking table
func (s *JobSchedulingService) InsertBenchmarkRequests(ctx context.Context, plan *BenchmarkPlan) error {
	if len(plan.ForcedBenchmarks) == 0 && len(plan.AgentBenchmarks) == 0 {
		return nil
	}

	// Insert forced benchmarks
	for _, task := range plan.ForcedBenchmarks {
		_, err := s.jobExecutionService.db.ExecContext(ctx, `
			INSERT INTO benchmark_requests (agent_id, job_execution_id, attack_mode, hash_type, request_type)
			VALUES ($1, $2, $3, $4, 'forced')
			ON CONFLICT (agent_id, attack_mode, hash_type) DO NOTHING
		`, task.AgentID, task.JobID, task.AttackMode, task.HashType)

		if err != nil {
			debug.Error("Failed to insert forced benchmark request: %v", err)
		}
	}

	// Insert agent benchmarks
	for _, task := range plan.AgentBenchmarks {
		_, err := s.jobExecutionService.db.ExecContext(ctx, `
			INSERT INTO benchmark_requests (agent_id, job_execution_id, attack_mode, hash_type, request_type)
			VALUES ($1, $2, $3, $4, 'agent_speed')
			ON CONFLICT (agent_id, attack_mode, hash_type) DO NOTHING
		`, task.AgentID, task.JobID, task.AttackMode, task.HashType)

		if err != nil {
			debug.Error("Failed to insert agent benchmark request: %v", err)
		}
	}

	debug.Log("Inserted benchmark request records", map[string]interface{}{
		"forced_count": len(plan.ForcedBenchmarks),
		"agent_count":  len(plan.AgentBenchmarks),
	})

	return nil
}

// ClearBenchmarkRequests removes all benchmark request records
func (s *JobSchedulingService) ClearBenchmarkRequests(ctx context.Context) error {
	_, err := s.jobExecutionService.db.ExecContext(ctx, `DELETE FROM benchmark_requests`)
	if err != nil {
		return fmt.Errorf("failed to clear benchmark requests: %w", err)
	}

	debug.Log("Cleared benchmark requests table", nil)
	return nil
}

// PrioritizeForcedBenchmarkAgents checks agent metadata and prioritizes agents for their forced benchmark jobs
func (s *JobSchedulingService) PrioritizeForcedBenchmarkAgents(
	ctx context.Context,
	agents []models.Agent,
	jobs []models.JobExecutionWithWork,
) {
	for i := range agents {
		agent := &agents[i]
		if agent.Metadata == nil {
			continue
		}

		// Check if this agent completed a forced benchmark
		if jobIDStr, exists := agent.Metadata["forced_benchmark_completed_for_job"]; exists && jobIDStr != "" {
			jobID, err := uuid.Parse(jobIDStr)
			if err != nil {
				continue
			}

			// Find the job and check if it's the first task
			taskCount, err := s.jobExecutionService.jobTaskRepo.GetTaskCountForJob(ctx, jobID)
			if err != nil || taskCount != 1 {
				continue // Not first task or error
			}

			// This agent should get priority for this job
			// We'll handle prioritization in allocation by checking this metadata
			debug.Info("Agent %d should get priority for job %s (completed forced benchmark)", agent.ID, jobID)

			// Clear the metadata flag after noting it
			delete(agent.Metadata, "forced_benchmark_completed_for_job")
			s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
		}
	}
}
