package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// JobChunkingService handles job chunking based on time and agent performance
type JobChunkingService struct {
	benchmarkRepo      *repository.BenchmarkRepository
	jobTaskRepo        *repository.JobTaskRepository
	systemSettingsRepo *repository.SystemSettingsRepository
}

// NewJobChunkingService creates a new job chunking service
func NewJobChunkingService(
	benchmarkRepo *repository.BenchmarkRepository,
	jobTaskRepo *repository.JobTaskRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *JobChunkingService {
	return &JobChunkingService{
		benchmarkRepo:      benchmarkRepo,
		jobTaskRepo:        jobTaskRepo,
		systemSettingsRepo: systemSettingsRepo,
	}
}

// ChunkCalculationRequest contains the parameters needed for chunk calculation
type ChunkCalculationRequest struct {
	JobExecution  *models.JobExecution
	Agent         *models.Agent
	AttackMode    models.AttackMode
	HashType      int
	ChunkDuration int  // Desired chunk duration in seconds
	IsSalted      bool // Whether the hash type uses per-hash salts
	TotalHashes   int  // Total hashes in the hashlist
	CrackedHashes int  // Number of cracked hashes
}

// ChunkCalculationResult contains the calculated chunk parameters
type ChunkCalculationResult struct {
	KeyspaceStart  int64
	KeyspaceEnd    int64
	BenchmarkSpeed *int64
	ActualDuration int // Estimated actual duration in seconds
	IsLastChunk    bool
}

// CalculateNextChunk calculates the next chunk for an agent based on benchmarks and time constraints
func (s *JobChunkingService) CalculateNextChunk(ctx context.Context, req ChunkCalculationRequest) (*ChunkCalculationResult, error) {
	debug.Log("Calculating next chunk", map[string]interface{}{
		"job_execution_id": req.JobExecution.ID,
		"agent_id":         req.Agent.ID,
		"attack_mode":      req.AttackMode,
		"hash_type":        req.HashType,
		"chunk_duration":   req.ChunkDuration,
	})

	// Get the next available keyspace start position
	keyspaceStart, _, err := s.jobTaskRepo.GetNextKeyspaceRange(ctx, req.JobExecution.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next keyspace range: %w", err)
	}

	debug.Log("Retrieved keyspace start position", map[string]interface{}{
		"job_execution_id": req.JobExecution.ID,
		"keyspace_start":   keyspaceStart,
	})

	// If job has no base keyspace, we can't calculate chunks properly
	// BaseKeyspace is used for --skip/--limit which operates on wordlist positions
	if req.JobExecution.BaseKeyspace == nil {
		debug.Log("Job has no base keyspace, using alternative calculation", map[string]interface{}{
			"job_execution_id": req.JobExecution.ID,
		})
		return s.calculateChunkWithoutKeyspace(ctx, req, keyspaceStart)
	}

	baseKeyspace := *req.JobExecution.BaseKeyspace
	remainingKeyspace := baseKeyspace - keyspaceStart

	debug.Log("Calculated remaining keyspace", map[string]interface{}{
		"job_execution_id":   req.JobExecution.ID,
		"base_keyspace":      baseKeyspace,
		"keyspace_start":     keyspaceStart,
		"remaining_keyspace": remainingKeyspace,
	})

	if remainingKeyspace <= 0 {
		return nil, fmt.Errorf("no remaining keyspace for job")
	}

	// Calculate salt count for salted hash types (used for benchmark lookup)
	var saltCount *int
	if req.IsSalted {
		remaining := req.TotalHashes - req.CrackedHashes
		if remaining > 0 {
			saltCount = &remaining
		}
	}

	// Get agent benchmark for this attack mode and hash type (salt-aware lookup)
	benchmarkSpeed, err := s.GetOrEstimateBenchmark(ctx, req.Agent.ID, req.AttackMode, req.HashType, saltCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get benchmark: %w", err)
	}

	debug.Log("Retrieved benchmark speed for chunking", map[string]interface{}{
		"agent_id":        req.Agent.ID,
		"attack_mode":     req.AttackMode,
		"hash_type":       req.HashType,
		"benchmark_speed": benchmarkSpeed,
		"chunk_duration":  req.ChunkDuration,
	})

	// Validate benchmark speed
	if benchmarkSpeed <= 0 {
		debug.Log("Invalid benchmark speed detected, using default", map[string]interface{}{
			"agent_id":        req.Agent.ID,
			"benchmark_speed": benchmarkSpeed,
		})
		benchmarkSpeed = s.getDefaultBenchmarkEstimate(req.AttackMode, req.HashType)
	}

	// For salted hash types, adjust the benchmark speed to get true candidate rate
	// Hashcat reports speed as hash_ops/sec (candidate_rate × salt_count) for salted hashes
	// We need to divide by remaining hash count (≈ salt count) to get candidate rate
	candidateSpeed := benchmarkSpeed
	if req.IsSalted {
		remainingHashes := int64(req.TotalHashes - req.CrackedHashes)
		if remainingHashes > 0 {
			candidateSpeed = benchmarkSpeed / remainingHashes
			debug.Log("Applied salt adjustment to benchmark speed", map[string]interface{}{
				"is_salted":         req.IsSalted,
				"total_hashes":      req.TotalHashes,
				"cracked_hashes":    req.CrackedHashes,
				"remaining_hashes":  remainingHashes,
				"original_speed":    benchmarkSpeed,
				"adjusted_speed":    candidateSpeed,
			})
		}
	}

	// Calculate chunk size based on adjusted speed and desired duration
	desiredChunkSize := int64(req.ChunkDuration) * candidateSpeed

	debug.Log("Calculated desired chunk size", map[string]interface{}{
		"chunk_duration":     req.ChunkDuration,
		"benchmark_speed":    benchmarkSpeed,
		"candidate_speed":    candidateSpeed,
		"is_salted":          req.IsSalted,
		"desired_chunk_size": desiredChunkSize,
	})

	// Get fluctuation percentage setting
	fluctuationSetting, err := s.systemSettingsRepo.GetSetting(ctx, "chunk_fluctuation_percentage")
	if err != nil {
		return nil, fmt.Errorf("failed to get fluctuation setting: %w", err)
	}

	fluctuationPercentage := 20 // Default
	if fluctuationSetting != nil {
		if fluctuationSetting.Value != nil {
			if parsed, parseErr := parseIntValue(*fluctuationSetting.Value); parseErr == nil {
				fluctuationPercentage = parsed
			}
		}
	}

	// Check if this would be the last chunk
	keyspaceEnd := keyspaceStart + desiredChunkSize
	isLastChunk := false
	actualDuration := req.ChunkDuration

	debug.Log("Initial keyspace calculation", map[string]interface{}{
		"keyspace_start":     keyspaceStart,
		"desired_chunk_size": desiredChunkSize,
		"keyspace_end":       keyspaceEnd,
		"base_keyspace":      baseKeyspace,
	})

	if keyspaceEnd >= baseKeyspace {
		// This is the last chunk
		keyspaceEnd = baseKeyspace
		isLastChunk = true
		actualDuration = int((baseKeyspace - keyspaceStart) / candidateSpeed)

		debug.Log("Adjusted to last chunk", map[string]interface{}{
			"reason":           "keyspace_end >= base_keyspace",
			"keyspace_end":     keyspaceEnd,
			"actual_duration":  actualDuration,
		})
	} else {
		// Check if the remaining keyspace after this chunk would be too small
		remainingAfterChunk := baseKeyspace - keyspaceEnd
		fluctuationThreshold := int64(float64(desiredChunkSize) * float64(fluctuationPercentage) / 100.0)

		if remainingAfterChunk <= fluctuationThreshold {
			// Merge the final small chunk into this one
			keyspaceEnd = baseKeyspace
			isLastChunk = true
			actualDuration = int((baseKeyspace - keyspaceStart) / candidateSpeed)

			debug.Log("Merging final chunk to avoid small remainder", map[string]interface{}{
				"remaining_after_chunk": remainingAfterChunk,
				"fluctuation_threshold": fluctuationThreshold,
				"adjusted_keyspace_end": keyspaceEnd,
				"adjusted_duration":     actualDuration,
			})
		}
	}

	result := &ChunkCalculationResult{
		KeyspaceStart:  keyspaceStart,
		KeyspaceEnd:    keyspaceEnd,
		BenchmarkSpeed: &benchmarkSpeed,
		ActualDuration: actualDuration,
		IsLastChunk:    isLastChunk,
	}

	debug.Log("Chunk calculated - Final result", map[string]interface{}{
		"keyspace_start":  result.KeyspaceStart,
		"keyspace_end":    result.KeyspaceEnd,
		"benchmark_speed": benchmarkSpeed,
		"benchmark_ptr":   result.BenchmarkSpeed,
		"actual_duration": result.ActualDuration,
		"is_last_chunk":   result.IsLastChunk,
		"chunk_size":      result.KeyspaceEnd - result.KeyspaceStart,
		"job_id":          req.JobExecution.ID,
	})

	return result, nil
}

// calculateChunkWithoutKeyspace handles chunk calculation for attacks that don't support keyspace
func (s *JobChunkingService) calculateChunkWithoutKeyspace(ctx context.Context, req ChunkCalculationRequest, keyspaceStart int64) (*ChunkCalculationResult, error) {
	// We no longer support chunk calculation without keyspace
	// All jobs must have a calculated keyspace for proper distributed workload management
	return nil, fmt.Errorf("keyspace calculation is required for all job types - attack mode %d does not support chunking without keyspace", req.AttackMode)
}

// GetOrEstimateBenchmark gets the benchmark for an agent or estimates one if not available
// saltCount is used for salt-aware benchmark lookup (nil for non-salted hash types)
func (s *JobChunkingService) GetOrEstimateBenchmark(ctx context.Context, agentID int, attackMode models.AttackMode, hashType int, saltCount *int) (int64, error) {
	// Try to get existing benchmark (salt-aware lookup)
	benchmark, err := s.benchmarkRepo.GetAgentBenchmark(ctx, agentID, attackMode, hashType, saltCount)
	if err == nil {
		// Check if benchmark is recent enough
		cacheDurationSetting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_cache_duration_hours")
		if err != nil {
			return benchmark.Speed, nil // Use existing benchmark if we can't check cache duration
		}

		cacheDurationHours := 168 // Default 7 days
		if cacheDurationSetting.Value != nil {
			if parsed, parseErr := parseIntValue(*cacheDurationSetting.Value); parseErr == nil {
				cacheDurationHours = parsed
			}
		}

		cacheDuration := time.Duration(cacheDurationHours) * time.Hour
		isRecent, err := s.benchmarkRepo.IsRecentBenchmark(ctx, agentID, attackMode, hashType, saltCount, cacheDuration)
		if err == nil && isRecent {
			return benchmark.Speed, nil
		}
	}

	// No recent benchmark found, estimate based on similar benchmarks
	agentBenchmarks, err := s.benchmarkRepo.GetAgentBenchmarks(ctx, agentID)
	if err != nil || len(agentBenchmarks) == 0 {
		// No benchmarks available, use a conservative estimate
		return s.getDefaultBenchmarkEstimate(attackMode, hashType), nil
	}

	// Calculate average speed from existing benchmarks
	var totalSpeed int64
	var count int
	for _, bench := range agentBenchmarks {
		totalSpeed += bench.Speed
		count++
	}

	if count == 0 {
		return s.getDefaultBenchmarkEstimate(attackMode, hashType), nil
	}

	averageSpeed := totalSpeed / int64(count)

	// Apply attack mode modifier to the average
	modifier := s.getAttackModeSpeedModifier(attackMode)
	estimatedSpeed := int64(float64(averageSpeed) * modifier)

	debug.Log("Estimated benchmark speed", map[string]interface{}{
		"agent_id":        agentID,
		"attack_mode":     attackMode,
		"hash_type":       hashType,
		"average_speed":   averageSpeed,
		"modifier":        modifier,
		"estimated_speed": estimatedSpeed,
	})

	return estimatedSpeed, nil
}

// getDefaultBenchmarkEstimate provides conservative default benchmark estimates
func (s *JobChunkingService) getDefaultBenchmarkEstimate(attackMode models.AttackMode, hashType int) int64 {
	baseSpeed := int64(1000000) // 1M hashes/sec baseline

	// Adjust based on attack mode complexity
	switch attackMode {
	case models.AttackModeStraight:
		return baseSpeed * 2 // Dictionary attacks are typically faster
	case models.AttackModeCombination:
		return baseSpeed // Combination attacks are moderate
	case models.AttackModeBruteForce:
		return baseSpeed / 2 // Brute force is slower
	case models.AttackModeHybridWordlistMask, models.AttackModeHybridMaskWordlist:
		return baseSpeed / 3 // Hybrid attacks are slower
	default:
		return baseSpeed / 10 // Very conservative for unknown modes
	}
}

// getAttackModeSpeedModifier returns a speed modifier for different attack modes
func (s *JobChunkingService) getAttackModeSpeedModifier(attackMode models.AttackMode) float64 {
	switch attackMode {
	case models.AttackModeStraight:
		return 1.2 // Dictionary attacks are typically faster
	case models.AttackModeCombination:
		return 1.0 // Baseline
	case models.AttackModeBruteForce:
		return 0.8 // Brute force is slower
	case models.AttackModeHybridWordlistMask, models.AttackModeHybridMaskWordlist:
		return 0.6 // Hybrid attacks are slower
	default:
		return 0.5 // Conservative for unknown modes
	}
}

// JobCompletionEstimateRequest contains parameters for job completion estimation
type JobCompletionEstimateRequest struct {
	JobExecution  *models.JobExecution
	IsSalted      bool // Whether the hash type uses per-hash salts
	TotalHashes   int  // Total hashes in the hashlist
	CrackedHashes int  // Number of cracked hashes
}

// EstimateJobCompletion estimates when a job will complete based on current progress
func (s *JobChunkingService) EstimateJobCompletion(ctx context.Context, req JobCompletionEstimateRequest) (*time.Time, error) {
	// Use EffectiveKeyspace for completion estimation (total candidates including rules and salts)
	if req.JobExecution.EffectiveKeyspace == nil || *req.JobExecution.EffectiveKeyspace == 0 {
		return nil, fmt.Errorf("cannot estimate completion without effective keyspace")
	}

	effectiveKeyspace := *req.JobExecution.EffectiveKeyspace
	remainingKeyspace := effectiveKeyspace - req.JobExecution.ProcessedKeyspace

	if remainingKeyspace <= 0 {
		// Job is already complete
		now := time.Now()
		return &now, nil
	}

	// Get active tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, req.JobExecution.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job tasks: %w", err)
	}

	// Calculate average speed from running tasks
	var totalSpeed int64
	var runningTasks int
	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning && task.BenchmarkSpeed != nil {
			totalSpeed += *task.BenchmarkSpeed
			runningTasks++
		}
	}

	if runningTasks == 0 {
		return nil, fmt.Errorf("no running tasks to estimate completion")
	}

	// Calculate average speed and adjust for salted hashes
	avgSpeed := totalSpeed / int64(runningTasks)

	// For salted hash types, adjust the speed to get true candidate rate
	candidateSpeed := avgSpeed
	if req.IsSalted {
		remainingHashes := int64(req.TotalHashes - req.CrackedHashes)
		if remainingHashes > 0 {
			candidateSpeed = avgSpeed / remainingHashes
			debug.Log("Applied salt adjustment to completion estimate", map[string]interface{}{
				"is_salted":        req.IsSalted,
				"remaining_hashes": remainingHashes,
				"original_speed":   avgSpeed,
				"adjusted_speed":   candidateSpeed,
			})
		}
	}

	// Calculate estimated completion time
	estimatedSeconds := remainingKeyspace / candidateSpeed
	estimatedCompletion := time.Now().Add(time.Duration(estimatedSeconds) * time.Second)

	debug.Log("Job completion estimated", map[string]interface{}{
		"job_execution_id":     req.JobExecution.ID,
		"remaining_keyspace":   remainingKeyspace,
		"average_speed":        avgSpeed,
		"candidate_speed":      candidateSpeed,
		"is_salted":            req.IsSalted,
		"estimated_seconds":    estimatedSeconds,
		"estimated_completion": estimatedCompletion,
	})

	return &estimatedCompletion, nil
}

// parseIntValue safely parses an integer value with error handling
func parseIntValue(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty value")
	}

	result := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("invalid integer: %s", value)
		}
		result = result*10 + int(char-'0')
	}
	return result, nil
}

// CreateInitialChunks creates the initial set of chunks for a job execution
// This method handles rule splitting integration when needed
func (s *JobChunkingService) CreateInitialChunks(ctx context.Context, job *models.JobExecution, presetJob *models.PresetJob, jobExecService *JobExecutionService) error {
	debug.Log("Creating initial chunks for job", map[string]interface{}{
		"job_execution_id":    job.ID,
		"uses_rule_splitting": job.UsesRuleSplitting,
	})

	// Get available agents
	availableAgents, err := jobExecService.GetAvailableAgents(ctx)
	if err != nil {
		return fmt.Errorf("failed to get available agents: %w", err)
	}

	if len(availableAgents) == 0 {
		debug.Log("No available agents for job", map[string]interface{}{
			"job_execution_id": job.ID,
		})
		return nil
	}

	// Get benchmark speed estimate for the first available agent
	// In production, we might want to get speeds for all agents
	agent := availableAgents[0]
	hashType := 0 // This should come from the preset job
	if presetJob != nil {
		hashType = presetJob.HashType
	}

	// Determine salt count for salted hash types (used for benchmark lookup)
	// For salted hashes, we need the correct benchmark speed for the current salt count
	var saltCount *int
	if job.HashlistID > 0 {
		hashlist, hlErr := jobExecService.hashlistRepo.GetByID(ctx, job.HashlistID)
		if hlErr == nil && hashlist != nil {
			hashTypeInfo, htErr := jobExecService.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
			if htErr == nil && hashTypeInfo != nil && hashTypeInfo.IsSalted {
				uncrackedCount, countErr := jobExecService.hashlistRepo.GetUncrackedHashCount(ctx, job.HashlistID)
				if countErr == nil && uncrackedCount > 0 {
					saltCount = &uncrackedCount
					debug.Log("Using salt-aware benchmark for rule splitting", map[string]interface{}{
						"job_execution_id": job.ID,
						"salt_count":       uncrackedCount,
						"hash_type":        hashlist.HashTypeID,
					})
				}
			}
		}
	}

	// Get benchmark with salt count for accurate rule splitting calculation
	benchmarkSpeed, err := s.GetOrEstimateBenchmark(ctx, agent.ID, job.AttackMode, hashType, saltCount)
	if err != nil {
		debug.Log("Failed to get benchmark, using default", map[string]interface{}{
			"error": err.Error(),
		})
		benchmarkSpeed = 1000000 // 1M H/s default
	}

	// Check if we should use rule splitting
	decision, err := jobExecService.analyzeForRuleSplitting(ctx, job, presetJob, float64(benchmarkSpeed))
	if err != nil {
		return fmt.Errorf("failed to analyze for rule splitting: %w", err)
	}

	if decision.ShouldSplit {
		debug.Log("Using rule splitting for job", map[string]interface{}{
			"job_execution_id": job.ID,
			"num_splits":       decision.NumSplits,
			"total_rules":      decision.TotalRules,
		})

		// Create tasks with rule splitting
		err = jobExecService.createJobTasksWithRuleSplitting(ctx, job, presetJob, decision)
		if err != nil {
			return fmt.Errorf("failed to create tasks with rule splitting: %w", err)
		}
	} else {
		debug.Log("Using standard chunking for job", map[string]interface{}{
			"job_execution_id": job.ID,
		})

		// Standard chunking - this will be handled by the existing chunking logic
		// The job scheduling service will create chunks as agents become available
	}

	return nil
}
