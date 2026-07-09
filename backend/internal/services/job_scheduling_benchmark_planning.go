package services

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/errorclass"
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
	LayerID    *uuid.UUID // For increment layers, the specific layer needing benchmark
	Mask       string     // For increment layers, the specific mask to benchmark
	HashType   int
	AttackMode models.AttackMode
	Priority   int
	SaltCount  *int // For salted hash types, the number of remaining hashes
}

// AgentBenchmarkTask represents an agent speed benchmark
type AgentBenchmarkTask struct {
	AgentID    int
	JobID      uuid.UUID // Representative job for parameters
	HashType   int
	AttackMode models.AttackMode
	SaltCount  *int // For salted hash types, the number of remaining hashes
}

// JobHashTypeInfo contains hash type information for a job
type JobHashTypeInfo struct {
	JobID                uuid.UUID
	LayerID              *uuid.UUID // For increment layers
	Mask                 string     // For increment layers, the specific mask
	LayerIndex           int        // For increment layers, the ordering
	HashType             int
	AttackMode           models.AttackMode
	Priority             int
	CreatedAt            time.Time
	NeedsForcedBenchmark bool
	SaltCount            *int   // For salted hash types, the number of remaining hashes (each = 1 salt)
	IsSalted             bool   // Whether this hash type uses per-hash salts
	BinaryVersion        string // Binary version pattern for compatibility filtering
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

	// 3b. Pre-compute blocklist membership for every (agent, job, combo) pair
	//     the allocator might consider. Pulling this up front avoids O(N*M)
	//     DB calls inside the nested selection loops and keeps allocate*
	//     functions pure.
	blocklist, err := s.buildBenchmarkBlocklistSet(ctx, availableAgents, jobHashInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build benchmark blocklist set: %w", err)
	}

	// 3c. Same shape as the blocklist, but for the per-tuple hard-failure cap.
	//     An over-cap tuple is one we've already decided to fail the job over;
	//     don't waste an agent slot re-requesting that benchmark.
	overCap, err := s.buildBenchmarkOverCapSet(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build benchmark over-cap set: %w", err)
	}

	// 4. Allocate forced benchmarks (for new jobs only)
	forcedTasks, usedAgents := s.allocateForcedBenchmarks(jobHashInfo, availableAgents, agentBenchmarkStatus, blocklist, overCap)

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
	agentTasks := s.allocateAgentBenchmarks(availableAgents, usedAgents, uniqueHashTypes, agentBenchmarkStatus, blocklist, overCap)

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

		// Check if hash type is salted and get salt count (remaining hash count)
		var saltCount *int
		isSalted := false
		hashType, htErr := s.jobExecutionService.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
		if htErr == nil && hashType != nil && hashType.IsSalted {
			isSalted = true
			// For salted hash types, salt count = remaining (uncracked) hash count
			uncrackedCount, err := s.jobExecutionService.hashlistRepo.GetUncrackedHashCount(ctx, job.HashlistID)
			if err == nil && uncrackedCount > 0 {
				saltCount = &uncrackedCount
			}
		}

		// Check if job has increment layers
		if job.IncrementMode != "" && job.IncrementMode != "off" {
			// Check if this is an expanded layer entry (job.ID is actually a layer ID)
			layer, err := s.jobExecutionService.jobIncrementLayerRepo.GetByID(ctx, job.ID)
			if err == nil && layer != nil {
				// This is a layer entry - benchmark this specific layer if needed
				if !layer.IsAccurateKeyspace {
					jobHashInfo = append(jobHashInfo, JobHashTypeInfo{
						JobID:                layer.JobExecutionID, // Use parent job ID
						LayerID:              &layer.ID,            // This specific layer
						Mask:                 layer.Mask,           // Layer's mask
						LayerIndex:           layer.LayerIndex,
						HashType:             hashlist.HashTypeID,
						AttackMode:           job.AttackMode,
						Priority:             job.Priority,
						CreatedAt:            job.CreatedAt,
						NeedsForcedBenchmark: true,
						SaltCount:            saltCount,
						IsSalted:             isSalted,
						BinaryVersion:        job.BinaryVersion,
					})

					debug.Log("Added layer entry for benchmarking", map[string]interface{}{
						"parent_job_id": layer.JobExecutionID,
						"layer_id":      layer.ID,
						"layer_index":   layer.LayerIndex,
						"layer_mask":    layer.Mask,
						"is_salted":     isSalted,
						"salt_count":    saltCount,
					})
				}
			} else {
				// Not a layer entry - this is a regular increment job
				// Get all layers for this job
				layers, err := s.jobExecutionService.jobIncrementLayerRepo.GetByJobExecutionID(ctx, job.ID)
				if err != nil {
					debug.Warning("Failed to get increment layers for job %s: %v", job.ID, err)
					continue
				}

				// Add each layer that needs benchmarking
				for _, layer := range layers {
					if !layer.IsAccurateKeyspace {
						jobHashInfo = append(jobHashInfo, JobHashTypeInfo{
							JobID:                job.ID,
							LayerID:              &layer.ID,
							Mask:                 layer.Mask,
							LayerIndex:           layer.LayerIndex,
							HashType:             hashlist.HashTypeID,
							AttackMode:           job.AttackMode,
							Priority:             job.Priority,
							CreatedAt:            job.CreatedAt,
							NeedsForcedBenchmark: true,
							SaltCount:            saltCount,
							IsSalted:             isSalted,
							BinaryVersion:        job.BinaryVersion,
						})
					}
				}

				debug.Log("Found increment layers needing benchmarks", map[string]interface{}{
					"job_id":      job.ID,
					"layer_count": len(layers),
					"is_salted":   isSalted,
					"salt_count":  saltCount,
				})
			}
		} else {
			// Regular (non-increment) job - use existing logic
			taskCount, err := s.jobExecutionService.jobTaskRepo.GetTaskCountForJob(ctx, job.ID)
			if err != nil {
				debug.Warning("Failed to get task count for job %s: %v", job.ID, err)
				taskCount = 1 // Assume has tasks if can't determine
			}

			needsForcedBenchmark := (taskCount == 0 && !job.IsAccurateKeyspace)

			jobHashInfo = append(jobHashInfo, JobHashTypeInfo{
				JobID:                job.ID,
				LayerID:              nil, // Regular job, no layer
				Mask:                 "",
				LayerIndex:           0,
				HashType:             hashlist.HashTypeID,
				AttackMode:           job.AttackMode,
				Priority:             job.Priority,
				CreatedAt:            job.CreatedAt,
				NeedsForcedBenchmark: needsForcedBenchmark,
				SaltCount:            saltCount,
				IsSalted:             isSalted,
				BinaryVersion:        job.BinaryVersion,
			})
		}
	}

	return jobHashInfo, nil
}

// buildAgentBenchmarkStatus queries which benchmarks each agent has that are still valid
// Cache key format: "attackMode_hashType_saltCount" where saltCount is nil for non-salted hash types
func (s *JobSchedulingService) buildAgentBenchmarkStatus(
	ctx context.Context,
	availableAgents []models.Agent,
	jobHashInfo []JobHashTypeInfo,
	cacheDuration time.Duration,
) (map[int]map[string]bool, error) {
	agentBenchmarkStatus := make(map[int]map[string]bool)

	for _, agent := range availableAgents {
		agentBenchmarkStatus[agent.ID] = make(map[string]bool)

		// Check each unique (attackMode, hashType, saltCount) combination
		checkedCombos := make(map[string]bool)

		for _, jobInfo := range jobHashInfo {
			// Include salt count in cache key for salted hash types
			key := buildBenchmarkCacheKey(jobInfo.AttackMode, jobInfo.HashType, jobInfo.SaltCount)

			if checkedCombos[key] {
				continue // Already checked this combo for this agent
			}
			checkedCombos[key] = true

			// Check if agent has recent valid benchmark (including salt count)
			isRecent, err := s.jobExecutionService.benchmarkRepo.IsRecentBenchmark(
				ctx,
				agent.ID,
				jobInfo.AttackMode,
				jobInfo.HashType,
				jobInfo.SaltCount, // Pass salt count for salt-aware lookup
				cacheDuration,
			)

			if err != nil {
				// Error checking - treat as not recent
				agentBenchmarkStatus[agent.ID][key] = false
				debug.Warning("IsRecentBenchmark failed (agent=%d, key=%s): %v — treating combo as needing a benchmark this cycle",
					agent.ID, key, err)
				continue
			}

			agentBenchmarkStatus[agent.ID][key] = isRecent
			if !isRecent {
				// A miss here is the proximate cause of "successful benchmark, then re-issued"
				// loops. Logging the combo + cache duration makes the next dump diagnosable
				// without having to query the DB.
				debug.Log("Benchmark cache miss — will request benchmark", map[string]interface{}{
					"agent_id":         agent.ID,
					"cache_key":        key,
					"attack_mode":      int(jobInfo.AttackMode),
					"hash_type":        jobInfo.HashType,
					"salt_count":       jobInfo.SaltCount,
					"cache_duration":   cacheDuration.String(),
					"job_execution_id": jobInfo.JobID,
				})
			}
		}
	}

	return agentBenchmarkStatus, nil
}

// buildBenchmarkCacheKey creates a cache key for benchmark lookups
// Format: "attackMode_hashType_saltCount" where saltCount is "nil" for non-salted hash types
func buildBenchmarkCacheKey(attackMode models.AttackMode, hashType int, saltCount *int) string {
	if saltCount == nil {
		return fmt.Sprintf("%d_%d_nil", attackMode, hashType)
	}
	return fmt.Sprintf("%d_%d_%d", attackMode, hashType, *saltCount)
}

// benchmarkBlocklistSet is a pre-computed lookup of active blocklist entries.
// Keys: jobBlocklistKey (agent_id, job_execution_id, attack_mode, hash_type) for
// job-scoped entries, and globalBlocklistKey (agent_id, attack_mode, hash_type)
// for global entries (job_execution_id IS NULL). An agent is considered
// blocklisted for a (job, combo) if either key is present.
type benchmarkBlocklistSet struct {
	jobScoped map[string]struct{}
	global    map[string]struct{}
}

func jobBlocklistKey(agentID int, jobID uuid.UUID, attackMode models.AttackMode, hashType int) string {
	return fmt.Sprintf("%d|%s|%d|%d", agentID, jobID.String(), int(attackMode), hashType)
}

func globalBlocklistKey(agentID int, attackMode models.AttackMode, hashType int) string {
	return fmt.Sprintf("%d|%d|%d", agentID, int(attackMode), hashType)
}

func (b *benchmarkBlocklistSet) isBlocklisted(agentID int, jobID uuid.UUID, attackMode models.AttackMode, hashType int) bool {
	if b == nil {
		return false
	}
	if _, ok := b.global[globalBlocklistKey(agentID, attackMode, hashType)]; ok {
		return true
	}
	if _, ok := b.jobScoped[jobBlocklistKey(agentID, jobID, attackMode, hashType)]; ok {
		return true
	}
	return false
}

// benchmarkOverCapSet is the per-(agent, job, attack_mode, hash_type) set of
// tuples whose failure_count has already reached the hard cap. The forced-
// benchmark planner consults it to avoid re-asking an agent for a benchmark
// the system is about to fail the job over. Distinct from the blocklist
// because the cap can be reached without a successful blocklist insert (and
// after Fix 1 the blocklist is reliable, but defense in depth is still cheap).
type benchmarkOverCapSet struct {
	m map[string]struct{}
}

func (o *benchmarkOverCapSet) isOverCap(agentID int, jobID uuid.UUID, attackMode models.AttackMode, hashType int) bool {
	if o == nil {
		return false
	}
	_, ok := o.m[jobBlocklistKey(agentID, jobID, attackMode, hashType)]
	return ok
}

// buildBenchmarkOverCapSet loads every (agent, job, mode, hash_type) tuple
// where failure_count >= cap. Empty set when the cap is disabled (<= 0).
func (s *JobSchedulingService) buildBenchmarkOverCapSet(ctx context.Context) (*benchmarkOverCapSet, error) {
	set := &benchmarkOverCapSet{m: make(map[string]struct{})}
	cap := s.benchmarkHardFailureCap(ctx)
	if cap <= 0 {
		return set, nil
	}
	tuples, err := s.jobExecutionService.benchmarkRepo.GetOverCapTuples(ctx, cap)
	if err != nil {
		return nil, fmt.Errorf("get over-cap tuples: %w", err)
	}
	for _, t := range tuples {
		set.m[jobBlocklistKey(t.AgentID, t.JobExecutionID, t.AttackMode, t.HashType)] = struct{}{}
	}
	return set, nil
}

// rotationKey is the lookup key for lastForcedBenchmarkAgent. Same combo
// shape as jobBlocklistKey but without the agent — the *value* is the agent.
func rotationKey(jobID uuid.UUID, attackMode models.AttackMode, hashType int) string {
	return fmt.Sprintf("%s|%d|%d", jobID.String(), int(attackMode), hashType)
}

// buildBenchmarkBlocklistSet loads every active blocklist row that could
// matter for the current allocation cycle — i.e., the row's (agent, job?,
// attack_mode, hash_type) overlaps with at least one candidate. This is a
// single SELECT so the allocator loops stay O(1) per check.
func (s *JobSchedulingService) buildBenchmarkBlocklistSet(
	ctx context.Context,
	availableAgents []models.Agent,
	jobHashInfo []JobHashTypeInfo,
) (*benchmarkBlocklistSet, error) {
	set := &benchmarkBlocklistSet{
		jobScoped: make(map[string]struct{}),
		global:    make(map[string]struct{}),
	}
	if len(availableAgents) == 0 || len(jobHashInfo) == 0 {
		return set, nil
	}

	// Collect candidate combos and agent IDs.
	agentIDs := make([]int, 0, len(availableAgents))
	for _, a := range availableAgents {
		agentIDs = append(agentIDs, a.ID)
	}

	// Distinct (jobID, attackMode, hashType) across candidate jobs.
	type comboKey struct {
		jobID      uuid.UUID
		attackMode models.AttackMode
		hashType   int
	}
	seen := make(map[comboKey]struct{})
	jobIDs := make([]uuid.UUID, 0, len(jobHashInfo))
	attackModes := make([]int, 0, len(jobHashInfo))
	hashTypes := make([]int, 0, len(jobHashInfo))
	for _, info := range jobHashInfo {
		k := comboKey{jobID: info.JobID, attackMode: info.AttackMode, hashType: info.HashType}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		jobIDs = append(jobIDs, info.JobID)
		attackModes = append(attackModes, int(info.AttackMode))
		hashTypes = append(hashTypes, info.HashType)
	}
	if len(jobIDs) == 0 {
		return set, nil
	}

	// Single wide SELECT. Postgres ANY($1::<type>[]) handles the array
	// parameters cleanly and lets us filter on every dimension at once.
	query := `
		SELECT agent_id, job_execution_id, attack_mode, hash_type
		FROM agent_benchmark_blocklist
		WHERE cleared_at IS NULL
		  AND expires_at > CURRENT_TIMESTAMP
		  AND agent_id = ANY($1::int[])
		  AND attack_mode = ANY($2::int[])
		  AND hash_type = ANY($3::int[])
		  AND (job_execution_id IS NULL OR job_execution_id = ANY($4::uuid[]))`

	rows, err := s.jobExecutionService.db.QueryContext(ctx, query,
		intSliceAsPGArray(agentIDs),
		intSliceAsPGArray(attackModes),
		intSliceAsPGArray(hashTypes),
		uuidSliceAsPGArray(jobIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("query blocklist: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			agentID    int
			jobID      sql.NullString
			attackMode int
			hashType   int
		)
		if err := rows.Scan(&agentID, &jobID, &attackMode, &hashType); err != nil {
			debug.Warning("scan blocklist row: %v", err)
			continue
		}
		mode := models.AttackMode(attackMode)
		if !jobID.Valid {
			set.global[globalBlocklistKey(agentID, mode, hashType)] = struct{}{}
			continue
		}
		parsed, parseErr := uuid.Parse(jobID.String)
		if parseErr != nil {
			debug.Warning("bad job_execution_id %q in blocklist: %v", jobID.String, parseErr)
			continue
		}
		set.jobScoped[jobBlocklistKey(agentID, parsed, mode, hashType)] = struct{}{}
	}
	return set, nil
}

// intSliceAsPGArray returns a textual pg array literal for a slice of ints.
// Using the literal form keeps the single query above portable across the
// lib/pq driver without pulling in the pq.Array helper type.
func intSliceAsPGArray(vals []int) string {
	if len(vals) == 0 {
		return "{}"
	}
	b := strings.Builder{}
	b.WriteByte('{')
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(v))
	}
	b.WriteByte('}')
	return b.String()
}

func uuidSliceAsPGArray(vals []uuid.UUID) string {
	if len(vals) == 0 {
		return "{}"
	}
	b := strings.Builder{}
	b.WriteByte('{')
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(v.String())
	}
	b.WriteByte('}')
	return b.String()
}

// allocateForcedBenchmarks assigns agents to jobs needing forced benchmarks.
// Skip conditions, in order: binary-version incompat, already used this cycle,
// blocklisted, over-cap. Within the eligible set, agents we asked last cycle
// for the same (job, mode, hash_type) sink to the back of the candidate list
// so multi-agent fleets converge on failJobIfAllAgentsBlocklisted faster
// instead of pounding the same box every cycle.
func (s *JobSchedulingService) allocateForcedBenchmarks(
	jobHashInfo []JobHashTypeInfo,
	availableAgents []models.Agent,
	agentBenchmarkStatus map[int]map[string]bool,
	blocklist *benchmarkBlocklistSet,
	overCap *benchmarkOverCapSet,
) ([]ForcedBenchmarkTask, map[int]uuid.UUID) {
	// Filter and sort jobs needing forced benchmarks
	var jobsNeedingForced []JobHashTypeInfo
	for _, job := range jobHashInfo {
		if job.NeedsForcedBenchmark {
			jobsNeedingForced = append(jobsNeedingForced, job)
		}
	}

	// Sort by priority DESC, then created_at ASC, then layer_index ASC (for increment layers)
	for i := 0; i < len(jobsNeedingForced); i++ {
		for j := i + 1; j < len(jobsNeedingForced); j++ {
			shouldSwap := false

			// Primary: Priority (higher first)
			if jobsNeedingForced[i].Priority < jobsNeedingForced[j].Priority {
				shouldSwap = true
			} else if jobsNeedingForced[i].Priority == jobsNeedingForced[j].Priority {
				// Secondary: Created time (older first)
				if jobsNeedingForced[i].CreatedAt.After(jobsNeedingForced[j].CreatedAt) {
					shouldSwap = true
				} else if jobsNeedingForced[i].CreatedAt.Equal(jobsNeedingForced[j].CreatedAt) {
					// Tertiary: Layer index (lower index first) - for same job's layers
					if jobsNeedingForced[i].JobID == jobsNeedingForced[j].JobID {
						if jobsNeedingForced[i].LayerIndex > jobsNeedingForced[j].LayerIndex {
							shouldSwap = true
						}
					}
				}
			}

			if shouldSwap {
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
		// Prefer agents WITHOUT valid benchmark for this hash type (including salt count)
		// MUST be compatible with job's binary version
		key := buildBenchmarkCacheKey(job.AttackMode, job.HashType, job.SaltCount)

		// Determine last-picked agent for this combo so we can deprioritize it.
		// Pure heuristic — no behavior change when only one agent qualifies.
		var lastPickedID int
		if v, ok := s.lastForcedBenchmarkAgent.Load(rotationKey(job.JobID, job.AttackMode, job.HashType)); ok {
			if id, isInt := v.(int); isInt {
				lastPickedID = id
			}
		}

		// eligible returns true if availableAgents[i] is a viable candidate.
		eligible := func(i int) bool {
			a := &availableAgents[i]
			if usedAgents[a.ID] != uuid.Nil {
				return false
			}
			if !version.IsCompatibleStr(a.BinaryVersion, job.BinaryVersion) {
				return false
			}
			if blocklist.isBlocklisted(a.ID, job.JobID, job.AttackMode, job.HashType) {
				debug.Log("Skipping blocklisted agent for forced benchmark", map[string]interface{}{
					"agent_id": a.ID, "job_id": job.JobID,
					"attack_mode": int(job.AttackMode), "hash_type": job.HashType,
				})
				return false
			}
			if overCap.isOverCap(a.ID, job.JobID, job.AttackMode, job.HashType) {
				debug.Log("Skipping over-cap agent for forced benchmark", map[string]interface{}{
					"agent_id": a.ID, "job_id": job.JobID,
					"attack_mode": int(job.AttackMode), "hash_type": job.HashType,
				})
				return false
			}
			return true
		}

		// pickFirstMatching iterates eligible agents twice — once skipping the
		// last-picked agent, once including it — so any "new blood" wins over
		// re-asking the same box, but a singleton candidate isn't excluded.
		pickFirstMatching := func(want func(i int) bool) *models.Agent {
			for skipLast := true; ; skipLast = false {
				for i := range availableAgents {
					if !eligible(i) {
						continue
					}
					if skipLast && availableAgents[i].ID == lastPickedID {
						continue
					}
					if want(i) {
						return &availableAgents[i]
					}
				}
				if !skipLast {
					return nil
				}
			}
		}

		// First pass: agent without a valid benchmark for this combo.
		bestAgent := pickFirstMatching(func(i int) bool {
			return !agentBenchmarkStatus[availableAgents[i].ID][key]
		})
		// Second pass: any eligible agent.
		if bestAgent == nil {
			bestAgent = pickFirstMatching(func(i int) bool { return true })
		}

		if bestAgent == nil {
			// No compatible agents available for this job - skip it
			debug.Log("No compatible agents for forced benchmark job", map[string]interface{}{
				"job_id":         job.JobID,
				"binary_version": job.BinaryVersion,
			})
			continue
		}

		forcedTasks = append(forcedTasks, ForcedBenchmarkTask{
			AgentID:    bestAgent.ID,
			JobID:      job.JobID,
			LayerID:    job.LayerID,
			Mask:       job.Mask,
			HashType:   job.HashType,
			AttackMode: job.AttackMode,
			Priority:   job.Priority,
			SaltCount:  job.SaltCount,
		})

		usedAgents[bestAgent.ID] = job.JobID
		// Remember whom we picked so the next cycle can prefer someone else.
		s.lastForcedBenchmarkAgent.Store(rotationKey(job.JobID, job.AttackMode, job.HashType), bestAgent.ID)
	}

	return forcedTasks, usedAgents
}

// buildUniqueHashTypeList creates a deduplicated list of hash types from all jobs
func (s *JobSchedulingService) buildUniqueHashTypeList(
	jobHashInfo []JobHashTypeInfo,
) []JobHashTypeInfo {
	// Map key: "attackMode_hashType_saltCount" -> highest priority job with that combo
	uniqueMap := make(map[string]JobHashTypeInfo)

	for _, job := range jobHashInfo {
		key := buildBenchmarkCacheKey(job.AttackMode, job.HashType, job.SaltCount)

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
	blocklist *benchmarkBlocklistSet,
	overCap *benchmarkOverCapSet,
) []AgentBenchmarkTask {
	if len(uniqueHashTypes) == 0 {
		return []AgentBenchmarkTask{}
	}

	// Build map of which agents need which hash types (including salt count)
	// Only includes agents that are compatible with the job's binary version
	hashTypeToAgentsNeeding := make(map[string][]int)

	for _, agent := range availableAgents {
		if usedAgents[agent.ID] != uuid.Nil {
			continue // Skip agents doing forced benchmarks
		}

		for _, htInfo := range uniqueHashTypes {
			// Check binary version compatibility first
			if !version.IsCompatibleStr(agent.BinaryVersion, htInfo.BinaryVersion) {
				continue // Agent not compatible with this job's binary version
			}

			// Skip agent speed benchmarks when the agent is blocklisted for
			// this (job, hash_type, attack_mode). Speed benchmarks reuse the
			// same hashcat speedtest flow as forced benchmarks, so an agent
			// that keeps failing the speedtest is just as likely to fail here.
			if blocklist.isBlocklisted(agent.ID, htInfo.JobID, htInfo.AttackMode, htInfo.HashType) {
				continue
			}
			// Same rationale for the per-tuple hard cap: don't reuse this
			// agent for a combo we've already given up on.
			if overCap.isOverCap(agent.ID, htInfo.JobID, htInfo.AttackMode, htInfo.HashType) {
				continue
			}

			key := buildBenchmarkCacheKey(htInfo.AttackMode, htInfo.HashType, htInfo.SaltCount)

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
		key := buildBenchmarkCacheKey(htInfo.AttackMode, htInfo.HashType, htInfo.SaltCount)

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
					SaltCount:  htInfo.SaltCount,
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

// checkAndSyncAgentsForBenchmarks checks file availability for AVAILABLE agents only.
// Busy agents (running tasks) are not checked - would slow down their current work.
// Returns filtered lists of benchmark tasks that are ready (agent has all files).
func (s *JobSchedulingService) checkAndSyncAgentsForBenchmarks(
	ctx context.Context,
	plan *BenchmarkPlan,
) (*BenchmarkPlan, error) {
	if plan == nil || (len(plan.ForcedBenchmarks) == 0 && len(plan.AgentBenchmarks) == 0) {
		return plan, nil
	}

	// Step 1: Get currently available agents (not busy with tasks)
	availableAgents, err := s.jobExecutionService.GetAvailableAgents(ctx)
	if err != nil {
		debug.Warning("Failed to get available agents for file check: %v", err)
		return plan, nil // Proceed with original plan on error
	}

	// Build set of available agent IDs for O(1) lookup
	availableAgentIDs := make(map[int]bool)
	for _, agent := range availableAgents {
		availableAgentIDs[agent.ID] = true
	}

	debug.Info("Pre-benchmark file check: %d available agents, %d forced benchmarks, %d agent benchmarks",
		len(availableAgents), len(plan.ForcedBenchmarks), len(plan.AgentBenchmarks))

	// Step 2: Filter and check forced benchmarks
	// Timeout increased to 30s to allow agents with large wordlists (e.g., 15GB crackstation.txt)
	// to complete file scanning. First scan hashes all files, subsequent scans use cache (<1s).
	const inventoryTimeout = 30 * time.Second
	var readyForcedBenchmarks []ForcedBenchmarkTask
	var readyAgentBenchmarks []AgentBenchmarkTask
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Check forced benchmarks in parallel (only for available agents)
	for _, task := range plan.ForcedBenchmarks {
		if !availableAgentIDs[task.AgentID] {
			debug.Info("Skipping file check for agent %d (busy with task)", task.AgentID)
			continue
		}

		wg.Add(1)
		go func(t ForcedBenchmarkTask) {
			defer wg.Done()

			// Get job execution for this task
			job, err := s.jobExecutionService.GetJobExecutionByID(ctx, t.JobID)
			if err != nil {
				debug.Warning("Failed to get job %s for file check: %v", t.JobID, err)
				return // Skip this benchmark
			}

			// Check if agent has all required files
			ready, err := s.wsIntegration.CheckAgentFilesForJob(ctx, t.AgentID, job, inventoryTimeout)
			if err != nil {
				debug.Warning("File check failed for agent %d: %v", t.AgentID, err)
				return // Skip this agent
			}

			if ready {
				mu.Lock()
				readyForcedBenchmarks = append(readyForcedBenchmarks, t)
				mu.Unlock()
			} else {
				debug.Info("Agent %d not ready for forced benchmark (syncing), will retry next cycle", t.AgentID)
			}
		}(task)
	}

	// Check agent benchmarks in parallel (only for available agents)
	for _, task := range plan.AgentBenchmarks {
		if !availableAgentIDs[task.AgentID] {
			debug.Info("Skipping file check for agent %d (busy with task)", task.AgentID)
			continue
		}

		wg.Add(1)
		go func(t AgentBenchmarkTask) {
			defer wg.Done()

			// Get job execution for this task
			job, err := s.jobExecutionService.GetJobExecutionByID(ctx, t.JobID)
			if err != nil {
				debug.Warning("Failed to get job %s for file check: %v", t.JobID, err)
				return // Skip this benchmark
			}

			// Check if agent has all required files
			ready, err := s.wsIntegration.CheckAgentFilesForJob(ctx, t.AgentID, job, inventoryTimeout)
			if err != nil {
				debug.Warning("File check failed for agent %d: %v", t.AgentID, err)
				return // Skip this agent
			}

			if ready {
				mu.Lock()
				readyAgentBenchmarks = append(readyAgentBenchmarks, t)
				mu.Unlock()
			} else {
				debug.Info("Agent %d not ready for agent benchmark (syncing), will retry next cycle", t.AgentID)
			}
		}(task)
	}

	wg.Wait()

	debug.Info("Pre-benchmark file check complete: %d/%d forced benchmarks ready, %d/%d agent benchmarks ready",
		len(readyForcedBenchmarks), len(plan.ForcedBenchmarks),
		len(readyAgentBenchmarks), len(plan.AgentBenchmarks))

	// Return filtered plan with only ready benchmarks
	return &BenchmarkPlan{
		ForcedBenchmarks:         readyForcedBenchmarks,
		AgentBenchmarks:          readyAgentBenchmarks,
		ForcedBenchmarkAgentJobs: plan.ForcedBenchmarkAgentJobs,
	}, nil
}

// ExecuteBenchmarkPlan sends all benchmark requests in parallel
// Returns the filtered plan containing only benchmarks that were actually sent
func (s *JobSchedulingService) ExecuteBenchmarkPlan(
	ctx context.Context,
	plan *BenchmarkPlan,
) (*BenchmarkPlan, error) {
	if plan == nil || (len(plan.ForcedBenchmarks) == 0 && len(plan.AgentBenchmarks) == 0) {
		return nil, nil // Nothing to do
	}

	// First, check file availability for available agents and filter to ready-only benchmarks
	filteredPlan, err := s.checkAndSyncAgentsForBenchmarks(ctx, plan)
	if err != nil {
		debug.Warning("Pre-benchmark file check failed: %v", err)
		// Continue with original plan on error
		filteredPlan = plan
	}

	if len(filteredPlan.ForcedBenchmarks) == 0 && len(filteredPlan.AgentBenchmarks) == 0 {
		debug.Info("No agents ready for benchmarks this cycle (all syncing or busy)")
		return nil, nil // Return nil plan to indicate no benchmarks were sent
	}

	debug.Info("Executing benchmark plan", map[string]interface{}{
		"forced_benchmarks": len(filteredPlan.ForcedBenchmarks),
		"agent_benchmarks":  len(filteredPlan.AgentBenchmarks),
		"total_benchmarks":  len(filteredPlan.ForcedBenchmarks) + len(filteredPlan.AgentBenchmarks),
	})

	// Use filtered plan for execution
	plan = filteredPlan

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

	// Return the filtered plan so caller knows which benchmarks were actually sent
	return filteredPlan, nil
}

// executeForcedBenchmark sends a forced benchmark request for a specific job
func (s *JobSchedulingService) executeForcedBenchmark(ctx context.Context, task ForcedBenchmarkTask) error {
	// Last-second busy check: agent state can change between planning (GetAvailableAgents in
	// CreateBenchmarkPlan) and this send. If the agent picked up a task in between, sending
	// a benchmark now would force the agent to wait for the running task in
	// waitForActiveProcesses and time out 30s later. Skip and let the next scheduler tick retry.
	if activeTasks, atErr := s.jobTaskRepo.GetActiveTasksByAgent(ctx, task.AgentID); atErr == nil && len(activeTasks) > 0 {
		debug.Info("Agent %d became busy between planning and send; skipping forced benchmark this cycle (active_tasks=%d)", task.AgentID, len(activeTasks))
		return nil
	}

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

	// Mark agent as having pending benchmark
	// Use LAYER ID if this is a layer benchmark, otherwise job ID
	benchmarkEntityID := task.JobID.String()
	if task.LayerID != nil {
		benchmarkEntityID = task.LayerID.String()
	}

	if agent.Metadata == nil {
		agent.Metadata = make(map[string]string)
	}
	agent.Metadata["pending_benchmark_job"] = benchmarkEntityID // Can be layer or job ID
	agent.Metadata["benchmark_requested_at"] = time.Now().Format(time.RFC3339)
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		debug.Warning("Failed to update agent metadata for benchmark: %v", err)
	}

	// Send benchmark request via WebSocket
	if s.wsIntegration == nil {
		return fmt.Errorf("WebSocket integration not available")
	}

	// Pass layer information if this is a layer benchmark
	err = s.wsIntegration.RequestAgentBenchmark(ctx, task.AgentID, job, task.LayerID, task.Mask)
	if err != nil {
		// Clear metadata on failure
		if agent.Metadata != nil {
			delete(agent.Metadata, "pending_benchmark_job")
			delete(agent.Metadata, "benchmark_requested_at")
			s.agentRepo.Update(ctx, agent)
		}
		return fmt.Errorf("failed to send benchmark request: %w", err)
	}

	logFields := map[string]interface{}{
		"agent_id": task.AgentID,
		"job_id":   task.JobID,
	}
	if task.LayerID != nil {
		logFields["layer_id"] = task.LayerID
		logFields["layer_mask"] = task.Mask
	}
	debug.Info("Sent forced benchmark request", logFields)

	return nil
}

// executeAgentBenchmark sends an agent speed benchmark request
func (s *JobSchedulingService) executeAgentBenchmark(ctx context.Context, task AgentBenchmarkTask) error {
	// Last-second busy check (see executeForcedBenchmark for the full rationale).
	if activeTasks, atErr := s.jobTaskRepo.GetActiveTasksByAgent(ctx, task.AgentID); atErr == nil && len(activeTasks) > 0 {
		debug.Info("Agent %d became busy between planning and send; skipping agent benchmark this cycle (active_tasks=%d)", task.AgentID, len(activeTasks))
		return nil
	}

	// Get job execution
	job, err := s.jobExecutionService.GetJobExecutionByID(ctx, task.JobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// Send benchmark request via WebSocket
	if s.wsIntegration == nil {
		return fmt.Errorf("WebSocket integration not available")
	}

	// Pass nil for layer parameters (this is a regular agent benchmark, not layer-specific)
	err = s.wsIntegration.RequestAgentBenchmark(ctx, task.AgentID, job, nil, "")
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
	// Fixed base timeout: the removed speedtest_timeout_seconds setting no longer
	// configures this. The scheduler-v2 speed-test path uses the compressed /
	// uncompressed timeouts (see scheduler/speedtest.go) instead.
	baseTimeout := 180 * time.Second // 3 minutes

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

// MarkTimedOutBenchmarksAsFailed marks any incomplete benchmarks as failed and
// routes each timed-out row through AttributeBenchmarkFailure so the agent-
// reported and server-timeout paths apply the same blocklist/attribution
// policy. Called when WaitForBenchmarks times out.
func (s *JobSchedulingService) MarkTimedOutBenchmarksAsFailed(ctx context.Context) error {
	// Capture the list of timed-out (agent, job, mode, hash) tuples BEFORE the
	// bulk update — AttributeBenchmarkFailure's own WHERE completed_at IS NULL
	// clause would otherwise miss rows the bulk update just marked complete.
	rows, err := s.jobExecutionService.db.QueryContext(ctx, `
		SELECT agent_id, job_execution_id, attack_mode, hash_type
		FROM benchmark_requests
		WHERE completed_at IS NULL
	`)
	if err != nil {
		return fmt.Errorf("failed to query timed-out benchmarks: %w", err)
	}
	type timedOutRow struct {
		agentID    int
		jobID      uuid.UUID
		attackMode models.AttackMode
		hashType   int
	}
	var timedOut []timedOutRow
	for rows.Next() {
		var (
			agentID    int
			jobID      uuid.UUID
			attackMode string
			hashType   int
		)
		if err := rows.Scan(&agentID, &jobID, &attackMode, &hashType); err != nil {
			debug.Warning("Failed to scan timed-out benchmark row: %v", err)
			continue
		}
		modeInt, parseErr := strconv.Atoi(attackMode)
		if parseErr != nil {
			debug.Warning("Failed to parse attack_mode %q on timed-out benchmark: %v", attackMode, parseErr)
			continue
		}
		debug.Warning("Benchmark timed out for agent %d, job %s, attack_mode %d, hash_type %d",
			agentID, jobID, modeInt, hashType)
		timedOut = append(timedOut, timedOutRow{
			agentID:    agentID,
			jobID:      jobID,
			attackMode: models.AttackMode(modeInt),
			hashType:   hashType,
		})
	}
	_ = rows.Close()

	if len(timedOut) == 0 {
		return nil
	}

	// Bulk-mark all remaining incomplete rows as failed. This is the closest
	// equivalent to the original behavior; per-row attribution follows below.
	if _, err := s.jobExecutionService.db.ExecContext(ctx, `
		UPDATE benchmark_requests
		SET completed_at = CURRENT_TIMESTAMP,
		    success = false,
		    error_message = 'Benchmark timed out waiting for agent response'
		WHERE completed_at IS NULL
	`); err != nil {
		return fmt.Errorf("failed to mark timed-out benchmarks as failed: %w", err)
	}

	// Per-row attribution: clear agent metadata, count failures, decide blocklist,
	// and (if applicable) fail the job. This is the only place that can observe
	// multiple agents timing out on the same job in one cycle, so the job-fail
	// check fires at the right moment.
	const timeoutErrMsg = "Benchmark timed out waiting for agent response"
	attributed := make(map[uuid.UUID]bool, len(timedOut))
	for _, row := range timedOut {
		if err := s.AttributeBenchmarkFailure(
			ctx, row.agentID, row.attackMode, row.hashType,
			row.jobID.String(), timeoutErrMsg,
		); err != nil {
			debug.Warning("AttributeBenchmarkFailure for timed-out agent %d, job %s: %v",
				row.agentID, row.jobID, err)
		}
		attributed[row.jobID] = true
	}

	// Keep the historical "job error_message = timed out" behavior for visibility
	// in the UI. This only sets the message when empty; AttributeBenchmarkFailure
	// may have already overwritten it with a more specific reason when marking
	// the job failed.
	for jobID := range attributed {
		if _, err := s.jobExecutionService.db.ExecContext(ctx, `
			UPDATE job_executions
			SET error_message = $2
			WHERE id = $1 AND error_message IS NULL
		`, jobID, timeoutErrMsg); err != nil {
			debug.Warning("Failed to update job %s error message: %v", jobID, err)
		}
	}

	debug.Warning("Marked %d timed-out benchmark(s) as failed across %d job(s)",
		len(timedOut), len(attributed))
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

// AttributeBenchmarkFailure is the single shared path for recording a failed
// benchmark attempt. Both the agent-reported failure (HandleBenchmarkResult)
// and the server-side timeout (MarkTimedOutBenchmarksAsFailed) route through
// here so blocklist/attribution policy stays uniform.
//
// Pipeline (each step is independent — a failure in any step is logged but
// does not short-circuit subsequent steps, so notifications + job-fail still
// fire in degraded conditions):
//
//  1. Clear pending_benchmark_job + benchmark_requested_at agent metadata
//     so the scheduler does not immediately re-select the same benchmark.
//  2. Mark matching benchmark_requests rows complete with success=false
//     (no-op for the timeout path, which has already bulk-updated them).
//  3. Resolve layer → parent job_execution_id (attribution keys on parent).
//  4. Upsert benchmark_failure_attempts (per-(agent, job, mode, hash_type)).
//  5. Update per-agent benchmark health counters. If they cross either the
//     streak or distinct-combos threshold, quarantine the agent
//     (is_enabled=false) and emit an agent_error notification. This rotates
//     work away from a sick box without waiting for combo-wide attribution.
//  6. If the per-tuple failure_count has reached the hard cap (default 10),
//     mark the job failed immediately — regardless of other evidence — so a
//     deadlocked combo cannot starve the queue indefinitely.
//  7. Apply combo-wide blocklist threshold (existing logic): 1 failure if
//     other agents have evidence the job works; N failures (default 3)
//     otherwise.
//  8. Insert cooldown blocklist entry (idempotent thanks to ON CONFLICT).
//  9. If every eligible agent is now blocklisted for this combo on this
//     job, mark the job failed and dispatch job_failed.
//  10. If the cumulative failure count for this job within the storm window
//     crosses the storm threshold, dispatch a benchmark_storm advisory
//     notification (admins only). Soft alert — does not change state.
//
// Returns non-nil only for unexpected repository errors that prevented any
// step from running; callers log but do not propagate (the surface error is
// the original benchmark failure).
func (s *JobSchedulingService) AttributeBenchmarkFailure(
	ctx context.Context,
	agentID int,
	attackMode models.AttackMode,
	hashType int,
	entityID string, // layer ID or job execution ID (as sent by the agent)
	errMsg string,
) error {
	// 1. Clear agent metadata. This is the immediate loop-breaker.
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("get agent %d: %w", agentID, err)
	}
	if agent.Metadata != nil {
		if _, had := agent.Metadata["pending_benchmark_job"]; had {
			delete(agent.Metadata, "pending_benchmark_job")
			delete(agent.Metadata, "benchmark_requested_at")
			if err := s.agentRepo.Update(ctx, agent); err != nil {
				debug.Warning("Failed to clear agent %d pending_benchmark metadata: %v", agentID, err)
			} else {
				debug.Info("Cleared pending_benchmark metadata for agent %d after benchmark failure", agentID)
			}
		}
	}

	// 2. Mark matching benchmark_requests row complete. WHERE completed_at IS
	//    NULL makes this a no-op when the timeout path already did the bulk
	//    update, so it's safe to call from either caller.
	if _, err := s.jobExecutionService.db.ExecContext(ctx, `
		UPDATE benchmark_requests
		SET completed_at = CURRENT_TIMESTAMP,
		    success = false,
		    error_message = $1
		WHERE agent_id = $2
		  AND attack_mode = $3
		  AND hash_type = $4
		  AND completed_at IS NULL
	`, errMsg, agentID, int(attackMode), hashType); err != nil {
		debug.Warning("Failed to update benchmark_requests on failure: %v", err)
	}

	// 3. Resolve layer → parent job if applicable. Attribution keys on parent.
	if entityID == "" {
		debug.Warning("AttributeBenchmarkFailure: empty entity_id, skipping attribution (agent_id=%d)", agentID)
		return nil
	}
	parsedID, parseErr := uuid.Parse(entityID)
	if parseErr != nil {
		debug.Warning("AttributeBenchmarkFailure: invalid entity_id %q: %v", entityID, parseErr)
		return nil
	}

	jobExecutionID := parsedID
	if layer, err := s.jobExecutionService.jobIncrementLayerRepo.GetByID(ctx, parsedID); err == nil && layer != nil {
		jobExecutionID = layer.JobExecutionID
		debug.Info("AttributeBenchmarkFailure: entity %s is a layer; attributing to parent job %s",
			parsedID, jobExecutionID)
	}

	// Classify the failure once; every downstream decision branches on this.
	// Shared with the task-failure path (job_websocket_integration.go), so the
	// taxonomy is consistent for benchmark and task failures alike.
	category := errorclass.Classify(errMsg)

	// HASHLIST_FATAL: hashcat rejected the hashlist for this hash type — every
	// line is wrong, so no agent can run it. Retrying doesn't help. Fail the job
	// and every other non-terminal job on the same hashlist (cascade), set the
	// hashlist to needs-attention, and add a global blocklist so the benchmark
	// phase doesn't immediately re-fire. Recovery is to change the hash type
	// (PATCH /hashlists/{id}/hash-type), which re-queues the failed jobs.
	if category == errorclass.CategoryHashlistFatal {
		reason := fmt.Sprintf(
			"hashlist content invalid for hash type %d: hashcat rejected every line (agent %d). Verify the hashlist contains valid hashes for the selected mode, or change the hash type.",
			hashType, agentID,
		)
		debug.Warning("Fast-failing job %s (hashlist_fatal): %s", jobExecutionID, reason)
		jobExecRepo := repository.NewJobExecutionRepository(s.jobExecutionService.db)
		// Cascade: fail every non-terminal job on this hashlist, not just the
		// reporting one — they would all fail for the same reason. Best-effort.
		if jobExec, err := s.jobExecutionService.GetJobExecutionByID(ctx, jobExecutionID); err == nil && jobExec != nil {
			if n, cErr := jobExecRepo.FailJobsByHashlistID(ctx, jobExec.HashlistID, reason); cErr != nil {
				debug.Warning("FailJobsByHashlistID(%d) for hashlist_fatal: %v", jobExec.HashlistID, cErr)
				// Fall back to failing just the reporting job.
				_ = jobExecRepo.UpdateErrorMessage(ctx, jobExecutionID, reason)
				_ = jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed)
			} else {
				debug.Info("hashlist_fatal: failed %d job(s) on hashlist %d", n, jobExec.HashlistID)
			}
			// Mark the hashlist itself as in error so the user sees the problem
			// on the hashlist view (not only on the failed jobs), and so the
			// recovery endpoint's "clear the cascade error state" is meaningful.
			// Recovery (PATCH /hashlists/{id}/hash-type) resets this to ready.
			if uErr := s.jobExecutionService.hashlistRepo.UpdateStatus(ctx, jobExec.HashlistID, models.HashListStatusError, reason); uErr != nil {
				debug.Warning("hashlist_fatal: set hashlist %d status=error: %v", jobExec.HashlistID, uErr)
			}
			s.jobExecutionService.dispatchJobFailedNotification(ctx, jobExec, reason)
		} else {
			_ = jobExecRepo.UpdateErrorMessage(ctx, jobExecutionID, reason)
			_ = jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed)
		}
		expiresAt := time.Now().Add(24 * time.Hour)
		if _, err := s.jobExecutionService.benchmarkRepo.AddBlocklistEntry(
			ctx, agentID, nil, attackMode, hashType, "HASHLIST_FATAL", expiresAt,
		); err != nil {
			debug.Warning("AddBlocklistEntry(hashlist_fatal, global) for agent %d: %v", agentID, err)
		}
		return nil
	}

	// JOB_CONFIG: the job's own attack config is invalid (bad mask/charset/rule).
	// No agent can run it as configured, but it is NOT the agent's fault and NOT
	// a hashlist problem — fail just this job with an actionable reason; do not
	// blocklist the agent or touch sibling jobs.
	if category == errorclass.CategoryJobConfig {
		reason := fmt.Sprintf(
			"job configuration rejected by hashcat (agent %d): %s. Fix the mask/charset/rules and recreate the job.",
			agentID, errMsg,
		)
		debug.Warning("Fast-failing job %s (job_config): %s", jobExecutionID, reason)
		jobExecRepo := repository.NewJobExecutionRepository(s.jobExecutionService.db)
		if err := jobExecRepo.UpdateErrorMessage(ctx, jobExecutionID, reason); err != nil {
			debug.Warning("UpdateErrorMessage(job_config) for job %s: %v", jobExecutionID, err)
		}
		if err := jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed); err != nil {
			debug.Warning("UpdateStatus(job_config) for job %s: %v", jobExecutionID, err)
		}
		if jobExec, err := s.jobExecutionService.GetJobExecutionByID(ctx, jobExecutionID); err == nil && jobExec != nil {
			s.jobExecutionService.dispatchJobFailedNotification(ctx, jobExec, reason)
		}
		return nil
	}

	// Transient (or unknown) failures must NOT be treated as agent-specific
	// evidence: no auto-quarantine and no otherAgentsSucceeded/runningTasks
	// blocklist after a single hit (a single cold-cache BENCHMARK_TIMEOUT, OOM,
	// or watchdog blip should never sideline a healthy agent). The per-tuple
	// threshold/hard-cap valves still apply so a combo that keeps failing can't
	// loop forever. Persistent agent faults (driver/no-device/self-test) fall
	// through to the corroborated quarantine/blocklist path below.
	isTransient := category.IsTransient()

	benchmarkRepo := s.jobExecutionService.benchmarkRepo

	// 4. Upsert failure counter.
	attempt, err := benchmarkRepo.RecordFailureAttempt(
		ctx, agentID, jobExecutionID, attackMode, hashType, errMsg,
	)
	if err != nil {
		return fmt.Errorf("record failure attempt: %w", err)
	}
	debug.Log("Recorded benchmark failure attempt", map[string]interface{}{
		"agent_id":         agentID,
		"job_execution_id": jobExecutionID,
		"attack_mode":      int(attackMode),
		"hash_type":        hashType,
		"failure_count":    attempt.FailureCount,
	})

	// 5. Per-agent benchmark health → quarantine ONLY when thresholds trip
	//    AND cross-agent evidence proves the failure is agent-specific. A
	//    threshold alone is ambiguous: a streak on one combo, or distinct
	//    failures across several combos, could just as easily mean the
	//    user is feeding bad hashlists / unsupported modes as it could
	//    mean the agent is broken. We only disable the agent when other
	//    agents are succeeding on the same (attack_mode, hash_type) combo
	//    that just failed here — that's the cross-agent signal the user
	//    asked for ("typically means an agent issue not a job or hashlist
	//    issue"). Without that signal, blocklisting (step 7) and the
	//    per-tuple hard cap (step 6) are the right tools.
	streakReset := s.benchmarkStreakResetWindow(ctx)
	cacheDuration := s.benchmarkCacheDurationForAttribution(ctx)
	otherAgentsSucceeded, oasErr := benchmarkRepo.CountAgentsWithRecentBenchmark(
		ctx, agentID, attackMode, hashType, cacheDuration,
	)
	if oasErr != nil {
		debug.Warning("CountAgentsWithRecentBenchmark: %v", oasErr)
		otherAgentsSucceeded = 0
	}
	if health, err := benchmarkRepo.RecordAgentBenchmarkFailure(ctx, agentID, streakReset); err != nil {
		debug.Warning("RecordAgentBenchmarkFailure(agent=%d): %v", agentID, err)
	} else {
		streakCap := s.agentBenchmarkQuarantineStreak(ctx)
		distinctCap := s.agentBenchmarkQuarantineDistinct(ctx)
		thresholdTripped := (streakCap > 0 && health.Streak >= streakCap) ||
			(distinctCap > 0 && health.DistinctCombosFailed >= distinctCap)
		switch {
		case thresholdTripped && isTransient:
			// Thresholds tripped but the failures are transient (timeout, OOM,
			// watchdog, disk, network) or unknown — don't quarantine a
			// likely-healthy agent. Cooldown/threshold blocklist (step 7) still
			// gives the job a way to make progress.
			debug.Warning(
				"agent %d benchmark thresholds tripped (streak=%d/%d, distinct=%d/%d within %s) but failures are transient/unknown (likely cold kernels, OOM, watchdog, or short timeout); not quarantining",
				agentID, health.Streak, streakCap, health.DistinctCombosFailed, distinctCap, streakReset,
			)
		case thresholdTripped && otherAgentsSucceeded > 0:
			reason := fmt.Sprintf(
				"auto-quarantine: benchmark streak=%d (cap=%d), distinct_combos=%d (cap=%d) within %s; %d other agent(s) succeeded on (attack_mode=%d, hash_type=%d)",
				health.Streak, streakCap, health.DistinctCombosFailed, distinctCap, streakReset,
				otherAgentsSucceeded, int(attackMode), hashType,
			)
			if qErr := benchmarkRepo.QuarantineAgent(ctx, agentID, reason); qErr != nil {
				debug.Warning("QuarantineAgent(agent=%d): %v", agentID, qErr)
			} else {
				debug.Warning("Quarantined agent %d: %s", agentID, reason)
				// Reload agent to capture is_enabled=false + last_error for the notification.
				if refreshed, refErr := s.agentRepo.GetByID(ctx, agentID); refErr == nil {
					s.jobExecutionService.DispatchAgentErrorNotification(ctx, refreshed, reason, map[string]interface{}{
						"streak":                 health.Streak,
						"streak_cap":             streakCap,
						"distinct_combos":        health.DistinctCombosFailed,
						"distinct_cap":           distinctCap,
						"other_agents_succeeded": otherAgentsSucceeded,
						"reset_window":           streakReset.String(),
						"job_execution_id":       jobExecutionID.String(),
						"hash_type":              hashType,
						"attack_mode":            int(attackMode),
					})
				}
			}
		case thresholdTripped:
			// Thresholds tripped, but no cross-agent evidence. Could be a
			// bad job/hashlist rather than a bad agent — defer to the
			// per-tuple hard cap (which fails the JOB, not the agent).
			debug.Warning(
				"agent %d benchmark thresholds tripped (streak=%d/%d, distinct=%d/%d within %s) but no other agent has a recent successful (attack_mode=%d, hash_type=%d) benchmark; deferring quarantine — letting per-tuple hard cap fail the job instead",
				agentID, health.Streak, streakCap, health.DistinctCombosFailed, distinctCap, streakReset,
				int(attackMode), hashType,
			)
		}
	}

	// 6. Per-tuple hard cap — fail the job once any (agent, job, mode, type)
	//    has failed `cap` times, regardless of attribution evidence. This is
	//    the catch-all that breaks loops single-agent deployments could
	//    otherwise stay stuck in forever.
	hardCap := s.benchmarkHardFailureCap(ctx)
	if hardCap > 0 && attempt.FailureCount >= hardCap {
		reason := fmt.Sprintf(
			"per-tuple hard cap reached: benchmark failed %d times on agent %d for (hash_type=%d, attack_mode=%d); cap=%d",
			attempt.FailureCount, agentID, hashType, int(attackMode), hardCap,
		)
		debug.Warning("Marking job %s failed via hard cap: %s", jobExecutionID, reason)
		jobExecRepo := repository.NewJobExecutionRepository(s.jobExecutionService.db)
		if err := jobExecRepo.UpdateErrorMessage(ctx, jobExecutionID, reason); err != nil {
			debug.Warning("UpdateErrorMessage(hard cap) for job %s: %v", jobExecutionID, err)
		}
		if err := jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed); err != nil {
			debug.Warning("UpdateStatus(hard cap) for job %s: %v", jobExecutionID, err)
		}
		if jobExec, err := s.jobExecutionService.GetJobExecutionByID(ctx, jobExecutionID); err == nil && jobExec != nil {
			s.jobExecutionService.dispatchJobFailedNotification(ctx, jobExec, reason)
		}
		// Storm check still runs below; don't return early.
	}

	// 7. Decide whether to blocklist yet (combo-wide policy).
	// otherAgentsSucceeded and cacheDuration were computed in step 5;
	// reuse them here.
	threshold := s.benchmarkFailureThreshold(ctx)
	activeAgentCount := 0
	if n, err := s.jobTaskRepo.GetActiveAgentCountByJob(ctx, jobExecutionID); err == nil {
		activeAgentCount = n
	} else {
		debug.Warning("GetActiveAgentCountByJob: %v", err)
	}
	hasRunningTasksOnOthers := activeAgentCount > 0

	blocklistNow := false
	blockReason := ""
	switch {
	case isTransient && attempt.FailureCount >= threshold:
		// Repeated transient/unknown failures on the same combo: blocklist with a
		// cooldown so the job can progress on other agents, but DON'T treat it as
		// agent-specific (no quarantine — see step 5). A single blip never reaches
		// here.
		blocklistNow = true
		blockReason = fmt.Sprintf(
			"benchmark failed (transient) %d times on agent %d for (hash_type=%d, attack_mode=%d); threshold=%d — cooldown only",
			attempt.FailureCount, agentID, hashType, int(attackMode), threshold,
		)
	case isTransient:
		// Below threshold: a transient/unknown failure is not evidence the agent
		// is broken. Skip the agent-specific blocklist branches entirely so a good
		// agent isn't sidelined for 24h after one cold-cache timeout/OOM/watchdog.
		debug.Info(
			"agent %d benchmark transient failure (%d/%d) for hash_type=%d attack_mode=%d; not blocklisting",
			agentID, attempt.FailureCount, threshold, hashType, int(attackMode),
		)
	case otherAgentsSucceeded > 0:
		blocklistNow = true
		blockReason = fmt.Sprintf(
			"benchmark failed on agent %d while %d other agent(s) have a recent successful benchmark for hash_type=%d attack_mode=%d — treating as agent-specific",
			agentID, otherAgentsSucceeded, hashType, int(attackMode),
		)
	case hasRunningTasksOnOthers:
		blocklistNow = true
		blockReason = fmt.Sprintf(
			"benchmark failed on agent %d while other agents are running tasks for this job — treating as agent-specific",
			agentID,
		)
	case attempt.FailureCount >= threshold:
		blocklistNow = true
		blockReason = fmt.Sprintf(
			"benchmark failed %d times on agent %d for this (hash_type=%d, attack_mode=%d); threshold=%d",
			attempt.FailureCount, agentID, hashType, int(attackMode), threshold,
		)
	}

	if blocklistNow {
		// 8. Insert cooldown blocklist entry (job-scoped). AddBlocklistEntry
		//    is now idempotent via ON CONFLICT DO UPDATE; an error here is a
		//    genuine DB problem and is logged-not-returned so steps 9/10 run.
		cooldown := s.benchmarkBlocklistCooldown(ctx)
		expiresAt := time.Now().Add(cooldown)
		jobScoped := jobExecutionID
		if _, err := benchmarkRepo.AddBlocklistEntry(
			ctx, agentID, &jobScoped, attackMode, hashType, blockReason, expiresAt,
		); err != nil {
			debug.Warning("AddBlocklistEntry(agent=%d, job=%s): %v — continuing", agentID, jobExecutionID, err)
		} else {
			debug.Info("Blocklisted agent %d for job %s (hash_type=%d, attack_mode=%d) until %s: %s",
				agentID, jobExecutionID, hashType, int(attackMode), expiresAt.Format(time.RFC3339), blockReason)
		}

		// 9. If every eligible agent is now blocklisted for this combo on
		//    this job, fail the job. Always runs after a blocklist attempt.
		if err := s.failJobIfAllAgentsBlocklisted(ctx, jobExecutionID, attackMode, hashType); err != nil {
			debug.Warning("failJobIfAllAgentsBlocklisted for job %s: %v", jobExecutionID, err)
		}
	}

	// 10. Storm advisory (admins only, soft alert).
	stormThreshold := s.benchmarkStormThreshold(ctx)
	stormWindow := s.benchmarkStormWindow(ctx)
	if stormThreshold > 0 && stormWindow > 0 {
		if n, err := benchmarkRepo.CountRecentBenchmarkFailuresForJob(ctx, jobExecutionID, stormWindow); err == nil && n >= stormThreshold {
			if jobExec, jErr := s.jobExecutionService.GetJobExecutionByID(ctx, jobExecutionID); jErr == nil && jobExec != nil {
				// Only fire while the job is still running — avoid spamming after a failed job is already marked.
				if jobExec.Status == models.JobExecutionStatusRunning || jobExec.Status == models.JobExecutionStatusPending {
					s.jobExecutionService.DispatchBenchmarkStormNotification(ctx, jobExec, n, int(stormWindow/time.Minute))
				}
			}
		}
	}

	return nil
}

// benchmarkFailureThreshold returns the failure count threshold before
// blocklisting a (agent, job) combination when attribution evidence is weak.
func (s *JobSchedulingService) benchmarkFailureThreshold(ctx context.Context) int {
	const defaultThreshold = 3
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_failure_threshold")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultThreshold
	}
	if n, err := strconv.Atoi(*setting.Value); err == nil && n > 0 {
		return n
	}
	return defaultThreshold
}

// benchmarkBlocklistCooldown returns the cooldown duration for new blocklist
// entries.
func (s *JobSchedulingService) benchmarkBlocklistCooldown(ctx context.Context) time.Duration {
	const defaultCooldown = 24 * time.Hour
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_blocklist_cooldown_hours")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultCooldown
	}
	if h, err := strconv.Atoi(*setting.Value); err == nil && h > 0 {
		return time.Duration(h) * time.Hour
	}
	return defaultCooldown
}

// benchmarkCacheDurationForAttribution mirrors the TTL used by
// CreateBenchmarkPlan so the "other agent has a recent successful benchmark"
// check is consistent with the cache the scheduler would otherwise use.
func (s *JobSchedulingService) benchmarkCacheDurationForAttribution(ctx context.Context) time.Duration {
	const defaultCache = 168 * time.Hour
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_cache_duration_hours")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultCache
	}
	if h, err := strconv.Atoi(*setting.Value); err == nil && h > 0 {
		return time.Duration(h) * time.Hour
	}
	return defaultCache
}

// benchmarkHardFailureCap is the per-(agent, job, attack_mode, hash_type)
// ceiling. When failure_count crosses it, the job is auto-marked failed
// regardless of any other attribution evidence. Default 10. Set to 0 to
// disable.
func (s *JobSchedulingService) benchmarkHardFailureCap(ctx context.Context) int {
	const defaultCap = 10
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_hard_failure_cap")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultCap
	}
	if n, err := strconv.Atoi(*setting.Value); err == nil && n >= 0 {
		return n
	}
	return defaultCap
}

// agentBenchmarkQuarantineStreak is the per-agent consecutive benchmark
// failure count that trips auto-quarantine. Default 15. Set to 0 to disable.
func (s *JobSchedulingService) agentBenchmarkQuarantineStreak(ctx context.Context) int {
	const defaultStreak = 15
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_benchmark_quarantine_streak")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultStreak
	}
	if n, err := strconv.Atoi(*setting.Value); err == nil && n >= 0 {
		return n
	}
	return defaultStreak
}

// agentBenchmarkQuarantineDistinct is the per-agent number of DISTINCT
// (hash_type, attack_mode) tuples that must fail within the streak reset
// window before auto-quarantine. Default 3. Set to 0 to disable.
func (s *JobSchedulingService) agentBenchmarkQuarantineDistinct(ctx context.Context) int {
	const defaultDistinct = 3
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_benchmark_quarantine_distinct")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultDistinct
	}
	if n, err := strconv.Atoi(*setting.Value); err == nil && n >= 0 {
		return n
	}
	return defaultDistinct
}

// benchmarkStreakResetWindow is the inactivity period after which the
// per-agent benchmark failure streak resets. Default 60 minutes.
func (s *JobSchedulingService) benchmarkStreakResetWindow(ctx context.Context) time.Duration {
	const defaultMinutes = 60
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_benchmark_streak_reset_minutes")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultMinutes * time.Minute
	}
	if m, err := strconv.Atoi(*setting.Value); err == nil && m > 0 {
		return time.Duration(m) * time.Minute
	}
	return defaultMinutes * time.Minute
}

// benchmarkStormThreshold is the minimum cumulative benchmark failure count
// on a single job within the storm window before a benchmark_storm advisory
// fires. Default 5. Set to 0 to disable storm notifications entirely.
func (s *JobSchedulingService) benchmarkStormThreshold(ctx context.Context) int {
	const defaultThreshold = 5
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_storm_threshold")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultThreshold
	}
	if n, err := strconv.Atoi(*setting.Value); err == nil && n >= 0 {
		return n
	}
	return defaultThreshold
}

// benchmarkStormWindow is the rolling window over which storm failures are
// counted. Default 15 minutes.
func (s *JobSchedulingService) benchmarkStormWindow(ctx context.Context) time.Duration {
	const defaultMinutes = 15
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_storm_window_minutes")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultMinutes * time.Minute
	}
	if m, err := strconv.Atoi(*setting.Value); err == nil && m > 0 {
		return time.Duration(m) * time.Minute
	}
	return defaultMinutes * time.Minute
}

// failJobIfAllAgentsBlocklisted marks the job failed if every eligible agent
// is currently blocklisted for this (hash_type, attack_mode) combination. This
// prevents the scheduler from idling forever on a job no one can benchmark.
func (s *JobSchedulingService) failJobIfAllAgentsBlocklisted(
	ctx context.Context,
	jobExecutionID uuid.UUID,
	attackMode models.AttackMode,
	hashType int,
) error {
	agents, err := s.agentRepo.List(ctx, nil)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	eligible := 0
	blocklisted := 0
	var blockedAgentNames []string
	benchmarkRepo := s.jobExecutionService.benchmarkRepo
	for i := range agents {
		a := &agents[i]
		// Eligibility is is_enabled ONLY — NOT online/active/sync-completed.
		// An offline-but-enabled agent could come back online later and try
		// the job, so we should keep the job alive for it. Failing the job
		// just because the currently-connected fleet is all blocklisted
		// would prematurely close work that another agent could rescue.
		// This matches the user-stated "multi-agent = any agent with
		// is_enabled = true" definition.
		if !a.IsEnabled {
			continue
		}
		eligible++
		blocked, err := benchmarkRepo.IsBlocklisted(ctx, a.ID, jobExecutionID, attackMode, hashType)
		if err != nil {
			debug.Warning("IsBlocklisted(agent=%d, job=%s): %v", a.ID, jobExecutionID, err)
			continue
		}
		if blocked {
			blocklisted++
			blockedAgentNames = append(blockedAgentNames, fmt.Sprintf("%d", a.ID))
		}
	}

	if eligible == 0 || blocklisted < eligible {
		return nil
	}

	reason := fmt.Sprintf(
		"benchmark failed on all %d eligible agents (ids: %s) for hash_type=%d attack_mode=%d; see per-agent blocklist entries under this job for details",
		eligible, strings.Join(blockedAgentNames, ","), hashType, int(attackMode),
	)
	debug.Warning("Marking job %s failed — all eligible agents blocklisted: %s", jobExecutionID, reason)

	jobExecRepo := repository.NewJobExecutionRepository(s.jobExecutionService.db)
	if err := jobExecRepo.UpdateErrorMessage(ctx, jobExecutionID, reason); err != nil {
		debug.Warning("UpdateErrorMessage on all-blocklisted job %s: %v", jobExecutionID, err)
	}
	if err := jobExecRepo.UpdateStatus(ctx, jobExecutionID, models.JobExecutionStatusFailed); err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}

	// Fire the standard job-failed notification so operators see it in the UI.
	if jobExec, err := s.jobExecutionService.GetJobExecutionByID(ctx, jobExecutionID); err == nil && jobExec != nil {
		s.jobExecutionService.dispatchJobFailedNotification(ctx, jobExec, reason)
	}
	return nil
}
