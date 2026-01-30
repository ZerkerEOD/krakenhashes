package services

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// JobExecutionService handles job execution orchestration
type JobExecutionService struct {
	db                 *db.DB // Store db connection for notification service
	jobExecRepo        *repository.JobExecutionRepository
	jobTaskRepo        *repository.JobTaskRepository
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository
	presetIncrementLayerRepo *repository.PresetIncrementLayerRepository
	benchmarkRepo      *repository.BenchmarkRepository
	agentHashlistRepo  *repository.AgentHashlistRepository
	agentRepo          *repository.AgentRepository
	deviceRepo         *repository.AgentDeviceRepository
	presetJobRepo      repository.PresetJobRepository
	hashlistRepo       *repository.HashListRepository
	hashTypeRepo       *repository.HashTypeRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	fileRepo           *repository.FileRepository
	scheduleRepo       *repository.AgentScheduleRepository
	binaryManager      binary.Manager
	ruleSplitManager   *RuleSplitManager
	assocWordlistRepo  *repository.AssociationWordlistRepository

	// Configuration paths
	hashcatBinaryPath string
	dataDirectory     string
}

// NewJobExecutionService creates a new job execution service
func NewJobExecutionService(
	database *db.DB,
	jobExecRepo *repository.JobExecutionRepository,
	jobTaskRepo *repository.JobTaskRepository,
	jobIncrementLayerRepo *repository.JobIncrementLayerRepository,
	presetIncrementLayerRepo *repository.PresetIncrementLayerRepository,
	benchmarkRepo *repository.BenchmarkRepository,
	agentHashlistRepo *repository.AgentHashlistRepository,
	agentRepo *repository.AgentRepository,
	deviceRepo *repository.AgentDeviceRepository,
	presetJobRepo repository.PresetJobRepository,
	hashlistRepo *repository.HashListRepository,
	hashTypeRepo *repository.HashTypeRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	fileRepo *repository.FileRepository,
	scheduleRepo *repository.AgentScheduleRepository,
	binaryManager binary.Manager,
	assocWordlistRepo *repository.AssociationWordlistRepository,
	hashcatBinaryPath string,
	dataDirectory string,
) *JobExecutionService {
	debug.Log("Creating JobExecutionService", map[string]interface{}{
		"data_directory": dataDirectory,
		"is_absolute":    filepath.IsAbs(dataDirectory),
	})

	// Create rule split manager with temp directory
	ruleSplitDir := filepath.Join(dataDirectory, "temp", "rule_chunks")
	ruleSplitManager := NewRuleSplitManager(ruleSplitDir, fileRepo)

	return &JobExecutionService{
		db:                       database,
		jobExecRepo:              jobExecRepo,
		jobTaskRepo:              jobTaskRepo,
		jobIncrementLayerRepo:    jobIncrementLayerRepo,
		presetIncrementLayerRepo: presetIncrementLayerRepo,
		benchmarkRepo:            benchmarkRepo,
		agentHashlistRepo:        agentHashlistRepo,
		agentRepo:                agentRepo,
		deviceRepo:               deviceRepo,
		presetJobRepo:            presetJobRepo,
		hashlistRepo:             hashlistRepo,
		hashTypeRepo:             hashTypeRepo,
		systemSettingsRepo:       systemSettingsRepo,
		fileRepo:                 fileRepo,
		scheduleRepo:             scheduleRepo,
		binaryManager:            binaryManager,
		ruleSplitManager:         ruleSplitManager,
		assocWordlistRepo:        assocWordlistRepo,
		hashcatBinaryPath:        hashcatBinaryPath,
		dataDirectory:            dataDirectory,
	}
}

// GetFreshEffectiveKeyspace fetches the current effective_keyspace from the database.
// This is needed because in-memory JobExecution objects may be stale after benchmark updates.
func (s *JobExecutionService) GetFreshEffectiveKeyspace(ctx context.Context, jobID uuid.UUID, layerID *uuid.UUID) (int64, error) {
	if layerID != nil {
		// For increment layers, fetch from job_increment_layers table
		layer, err := s.jobIncrementLayerRepo.GetByID(ctx, *layerID)
		if err != nil {
			return 0, fmt.Errorf("failed to fetch increment layer: %w", err)
		}
		if layer != nil && layer.EffectiveKeyspace != nil {
			return *layer.EffectiveKeyspace, nil
		}
		return 0, nil
	}

	// For regular jobs, fetch from job_executions table
	job, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch job execution: %w", err)
	}
	if job != nil && job.EffectiveKeyspace != nil {
		return *job.EffectiveKeyspace, nil
	}
	return 0, nil
}

// GetHashTypeByID retrieves a hash type by its ID.
// Used for salt-aware chunk calculations.
func (s *JobExecutionService) GetHashTypeByID(ctx context.Context, hashTypeID int) (*models.HashType, error) {
	if s.hashTypeRepo == nil {
		return nil, fmt.Errorf("hash type repository not initialized")
	}
	return s.hashTypeRepo.GetByID(ctx, hashTypeID)
}

// binaryStoreAdapter adapts binary.Manager to version.BinaryStore interface
type binaryStoreAdapter struct {
	manager binary.Manager
}

func (a *binaryStoreAdapter) ListActive(ctx context.Context) ([]version.BinaryInfo, error) {
	versions, err := a.manager.ListVersions(ctx, map[string]interface{}{"is_active": true})
	if err != nil {
		return nil, err
	}

	result := make([]version.BinaryInfo, 0, len(versions))
	for _, v := range versions {
		versionStr := ""
		if v.Version != nil {
			versionStr = *v.Version
		} else {
			// Fallback: extract version from filename (e.g., "hashcat-7.1.2.7z" -> "7.1.2")
			versionStr = extractVersionFromFilename(v.FileName)
			if versionStr == "" {
				debug.Warning("Binary ID %d has no version info and version cannot be extracted from filename %q, skipping", v.ID, v.FileName)
				continue
			}
			debug.Warning("Binary ID %d has NULL version, extracted %q from filename", v.ID, versionStr)
		}
		result = append(result, version.BinaryInfo{
			ID:        v.ID,
			Version:   versionStr,
			IsDefault: v.IsDefault,
			IsActive:  v.IsActive,
		})
	}
	return result, nil
}

// extractVersionFromFilename extracts hashcat version from filename
// Examples: "hashcat-7.1.2.7z" -> "7.1.2", "hashcat-7.1.2+154-clang.7z" -> "7.1.2"
func extractVersionFromFilename(filename string) string {
	re := regexp.MustCompile(`hashcat-(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func (a *binaryStoreAdapter) GetDefault(ctx context.Context) (*version.BinaryInfo, error) {
	bv, err := a.manager.GetDefault(ctx, binary.BinaryTypeHashcat)
	if err != nil {
		return nil, err
	}
	if bv == nil || bv.Version == nil {
		return nil, nil
	}
	return &version.BinaryInfo{
		ID:        bv.ID,
		Version:   *bv.Version,
		IsDefault: bv.IsDefault,
		IsActive:  bv.IsActive,
	}, nil
}

// resolveBinaryVersionPattern resolves a version pattern string to an actual binary ID.
// For keyspace calculation and other operations that need a specific binary.
func (s *JobExecutionService) resolveBinaryVersionPattern(ctx context.Context, pattern string) (int64, error) {
	if pattern == "" {
		pattern = "default"
	}

	// If pattern is "default", just get the system default
	if pattern == "default" {
		defaultBinary, err := s.binaryManager.GetDefault(ctx, binary.BinaryTypeHashcat)
		if err != nil {
			return 0, fmt.Errorf("failed to get default binary: %w", err)
		}
		if defaultBinary == nil {
			return 0, fmt.Errorf("no default binary configured")
		}
		return defaultBinary.ID, nil
	}

	// For specific patterns, use the resolver
	adapter := &binaryStoreAdapter{manager: s.binaryManager}
	resolver := version.NewResolver(adapter)

	// Parse the pattern
	parsedPattern, err := version.Parse(pattern)
	if err != nil {
		return 0, fmt.Errorf("invalid version pattern %q: %w", pattern, err)
	}

	// Get matching binaries
	matching, err := resolver.GetMatchingBinaries(ctx, parsedPattern)
	if err != nil {
		return 0, fmt.Errorf("failed to find matching binaries: %w", err)
	}

	if len(matching) == 0 {
		return 0, fmt.Errorf("no binary matches pattern %q", pattern)
	}

	// Return the highest matching version (or default if in list)
	for _, b := range matching {
		if b.IsDefault {
			return b.ID, nil
		}
	}

	// Return highest version (matching is already sorted by version desc in GetMatchingBinaries)
	return matching[0].ID, nil
}

// CustomJobConfig contains the configuration for a custom job
type CustomJobConfig struct {
	Name                      string
	WordlistIDs               models.IDArray
	RuleIDs                   models.IDArray
	AttackMode                models.AttackMode
	Mask                      string
	Priority                  int
	MaxAgents                 int
	BinaryVersion             string // Version pattern (e.g., "default", "7.x", "7.1.2")
	AllowHighPriorityOverride bool
	ChunkSizeSeconds          int
	IncrementMode             string
	IncrementMin              *int
	IncrementMax              *int
	AssociationWordlistID     *uuid.UUID // For association attacks (-a 9)
}

// CreateJobExecution creates a new job execution from a preset job and hashlist
func (s *JobExecutionService) CreateJobExecution(ctx context.Context, presetJobID uuid.UUID, hashlistID int64, createdBy *uuid.UUID, customJobName string) (*models.JobExecution, error) {
	debug.Log("Creating job execution", map[string]interface{}{
		"preset_job_id": presetJobID,
		"hashlist_id":   hashlistID,
	})

	// Get the preset job
	presetJob, err := s.presetJobRepo.GetByID(ctx, presetJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get preset job: %w", err)
	}

	// Get the hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Use pre-calculated keyspace from preset job if available
	var totalKeyspace *int64
	var effectiveKeyspace *int64
	var isAccurateKeyspace bool
	var useRuleSplitting bool
	var multiplicationFactor int = 1

	if presetJob.Keyspace != nil && *presetJob.Keyspace > 0 {
		totalKeyspace = presetJob.Keyspace
		effectiveKeyspace = presetJob.EffectiveKeyspace
		isAccurateKeyspace = presetJob.IsAccurateKeyspace
		// NOTE: useRuleSplitting is determined at creation time for accurate keyspace jobs,
		// or at first task dispatch (after benchmark) for estimate-based jobs as fallback
		multiplicationFactor = presetJob.MultiplicationFactor
		debug.Log("Using pre-calculated keyspace from preset job", map[string]interface{}{
			"preset_job_id":         presetJobID,
			"keyspace":              *totalKeyspace,
			"effective_keyspace":    effectiveKeyspace,
			"is_accurate_keyspace":  isAccurateKeyspace,
			"multiplication_factor": multiplicationFactor,
		})
	} else {
		// Fallback to calculating keyspace if not pre-calculated
		debug.Warning("Preset job has no pre-calculated keyspace, calculating now")
		totalKeyspace, effectiveKeyspace, isAccurateKeyspace, err = s.calculateKeyspace(ctx, presetJob, hashlist)
		if err != nil {
			debug.Error("Failed to calculate keyspace: %v", err)
			return nil, fmt.Errorf("keyspace calculation is required for job execution: %w", err)
		}
		// NOTE: useRuleSplitting is determined at creation time for accurate keyspace jobs,
		// or at first task dispatch (after benchmark) for estimate-based jobs as fallback
		// Calculate multiplication factor from returned values
		if isAccurateKeyspace && totalKeyspace != nil && *totalKeyspace > 0 && effectiveKeyspace != nil && *effectiveKeyspace > 0 {
			multiplicationFactor = int(*effectiveKeyspace / *totalKeyspace)
			if multiplicationFactor < 1 {
				multiplicationFactor = 1
			}
		}
	}

	// For salted hash types, adjust effective_keyspace by salt count
	// Preset's --total-candidates = base × rules (no hashlist, no salts)
	// Job's effective_keyspace = base × rules × salts (to match progress[1])
	if effectiveKeyspace != nil && *effectiveKeyspace > 0 {
		hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
		if htErr == nil && hashType != nil && hashType.IsSalted {
			saltCount := int64(hashlist.TotalHashes)
			if saltCount > 0 {
				originalEffective := *effectiveKeyspace
				adjustedEffective := originalEffective * saltCount
				effectiveKeyspace = &adjustedEffective
				// Also adjust multiplication factor
				if totalKeyspace != nil && *totalKeyspace > 0 {
					multiplicationFactor = int(adjustedEffective / *totalKeyspace)
				}
				debug.Log("Applied salt adjustment to effective keyspace at job creation", map[string]interface{}{
					"preset_job_id":      presetJobID,
					"hash_type_id":       hashlist.HashTypeID,
					"is_salted":          true,
					"salt_count":         saltCount,
					"original_effective": originalEffective,
					"adjusted_effective": adjustedEffective,
				})
			}
		}
	}

	// Determine rule splitting at creation time for accurate keyspace jobs
	// This avoids race conditions and mid-job switching issues - can't change strategy after tasks are dispatched
	if isAccurateKeyspace &&
		(presetJob.AttackMode == models.AttackModeStraight || presetJob.AttackMode == models.AttackModeAssociation) &&
		len(presetJob.RuleIDs) > 0 {

		// Check if rule splitting is enabled
		ruleSplitEnabled, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_enabled")
		if err == nil && ruleSplitEnabled != nil && ruleSplitEnabled.Value != nil && *ruleSplitEnabled.Value == "true" {
			// Get minimum rules threshold
			minRulesSetting, _ := s.systemSettingsRepo.GetSetting(ctx, "rule_split_min_rules")
			minRules := 100 // default
			if minRulesSetting != nil && minRulesSetting.Value != nil {
				if parsed, parseErr := strconv.Atoi(*minRulesSetting.Value); parseErr == nil {
					minRules = parsed
				}
			}

			// Enable rule splitting if we have enough rules
			// Use actual rule count (not salt-adjusted multiplicationFactor) for minRules comparison
			actualRuleCount, ruleErr := s.GetTotalRuleCount(ctx, presetJob.RuleIDs)
			if ruleErr != nil {
				actualRuleCount = int64(multiplicationFactor) // Fallback to multiplicationFactor
			}
			if int(actualRuleCount) >= minRules {
				useRuleSplitting = true
				debug.Log("Rule splitting enabled at preset job creation", map[string]interface{}{
					"preset_job_id":          presetJobID,
					"actual_rule_count":      actualRuleCount,
					"multiplication_factor":  multiplicationFactor,
					"min_rules":              minRules,
					"is_accurate_keyspace":   isAccurateKeyspace,
				})
			}
		}
	}

	// Set avg_rule_multiplier for accurate keyspace jobs (used in progress calculations)
	// For accurate keyspace jobs, this is the same as multiplication_factor but as float64
	var avgRuleMultiplier *float64
	if isAccurateKeyspace && multiplicationFactor > 0 {
		v := float64(multiplicationFactor)
		avgRuleMultiplier = &v
	}

	// Create job execution with all configuration copied from preset
	jobExecution := &models.JobExecution{
		PresetJobID:       &presetJobID, // Keep reference for audit trail
		HashlistID:        hashlistID,
		Status:            models.JobExecutionStatusPending,
		Priority:          presetJob.Priority,
		ProcessedKeyspace: 0,
		AttackMode:        presetJob.AttackMode,
		MaxAgents:         presetJob.MaxAgents,
		CreatedBy:         createdBy,

		// Copy all configuration from preset to make job self-contained
		Name:                      customJobName, // Will be set after getting client info
		WordlistIDs:               presetJob.WordlistIDs,
		RuleIDs:                   presetJob.RuleIDs,
		HashType:                  hashlist.HashTypeID,
		ChunkSizeSeconds:          presetJob.ChunkSizeSeconds,
		StatusUpdatesEnabled:      presetJob.StatusUpdatesEnabled,
		AllowHighPriorityOverride: presetJob.AllowHighPriorityOverride,
		BinaryVersion:           presetJob.BinaryVersion,
		Mask:                      presetJob.Mask,
		AdditionalArgs:            presetJob.AdditionalArgs,
		IncrementMode:             presetJob.IncrementMode,
		IncrementMin:              presetJob.IncrementMin,
		IncrementMax:              presetJob.IncrementMax,

		// Keyspace values from preset or calculated
		BaseKeyspace:         totalKeyspace,
		EffectiveKeyspace:    effectiveKeyspace,
		MultiplicationFactor: multiplicationFactor,
		IsAccurateKeyspace:   isAccurateKeyspace,
		UsesRuleSplitting:    useRuleSplitting,
		AvgRuleMultiplier:    avgRuleMultiplier, // For progress calculations
	}

	err = s.jobExecRepo.Create(ctx, jobExecution)
	if err != nil {
		return nil, fmt.Errorf("failed to create job execution: %w", err)
	}

	// Initialize increment layers if increment mode is enabled
	// This must happen BEFORE calculateEffectiveKeyspace
	// First, try to copy layers from preset_increment_layers (pre-calculated)
	layersCopied, err := s.copyPresetIncrementLayers(ctx, jobExecution, presetJobID)
	if err != nil {
		debug.Warning("Failed to copy preset increment layers, will calculate from scratch: %v", err)
	}

	// If no layers were copied (preset didn't have them), calculate from scratch
	if !layersCopied {
		err = s.initializeIncrementLayers(ctx, jobExecution, presetJob)
		if err != nil {
			debug.Error("Failed to initialize increment layers: job_execution_id=%s, error=%v",
				jobExecution.ID, err)
			return nil, fmt.Errorf("failed to initialize increment layers: %w", err)
		}
	}

	// Calculate effective keyspace after creating the job
	// Skip for increment mode jobs - initializeIncrementLayers already sets both base_keyspace and effective_keyspace
	// Skip if we already have accurate keyspace from preset (--total-candidates succeeded)
	// calculateEffectiveKeyspace would incorrectly overwrite base_keyspace with effective_keyspace value
	if (jobExecution.IncrementMode == "" || jobExecution.IncrementMode == "off") && !jobExecution.IsAccurateKeyspace {
		err = s.calculateEffectiveKeyspace(ctx, jobExecution, presetJob)
		if err != nil {
			debug.Error("Failed to calculate effective keyspace: job_execution_id=%s, error=%v",
				jobExecution.ID, err)
			// Log the error but continue - we'll handle this in the scheduling logic
		}
	}

	// NOTE: Rule splitting determination is DEFERRED until after forced benchmark
	// The benchmark provides accurate effective keyspace from hashcat's progress[1]
	// See HandleBenchmarkResult() in job_websocket_integration.go for the actual decision
	debug.Log("Job execution created - rule split decision deferred to benchmark", map[string]interface{}{
		"job_execution_id":      jobExecution.ID,
		"base_keyspace":         totalKeyspace,
		"effective_keyspace":    jobExecution.EffectiveKeyspace,
		"multiplication_factor": jobExecution.MultiplicationFactor,
	})

	return jobExecution, nil
}

// CreateCustomJobExecution creates a new job execution directly from custom configuration
func (s *JobExecutionService) CreateCustomJobExecution(ctx context.Context, config CustomJobConfig, hashlistID int64, createdBy *uuid.UUID, customJobName string) (*models.JobExecution, error) {
	debug.Log("Creating custom job execution", map[string]interface{}{
		"name":        config.Name,
		"hashlist_id": hashlistID,
		"attack_mode": config.AttackMode,
	})

	// Get the hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Get chunk size from config or system settings
	chunkSize := config.ChunkSizeSeconds
	if chunkSize <= 0 {
		// Fetch from system settings if not provided
		defaultChunkSetting, err := s.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration")
		if err == nil && defaultChunkSetting != nil && defaultChunkSetting.Value != nil {
			if parsed, parseErr := parseIntValueFromString(*defaultChunkSetting.Value); parseErr == nil {
				chunkSize = parsed
			}
		}
		// Final fallback
		if chunkSize <= 0 {
			chunkSize = 900
		}
	}

	// Create a temporary preset job structure for keyspace calculation
	// This ensures we use EXACTLY the same calculation logic as preset jobs
	tempPreset := &models.PresetJob{
		Name:                      config.Name,
		WordlistIDs:               config.WordlistIDs,
		RuleIDs:                   config.RuleIDs,
		AttackMode:                config.AttackMode,
		HashType:                  hashlist.HashTypeID,
		BinaryVersion:           config.BinaryVersion,
		Mask:                      config.Mask,
		Priority:                  config.Priority,
		MaxAgents:                 config.MaxAgents,
		AllowHighPriorityOverride: config.AllowHighPriorityOverride,
		ChunkSizeSeconds:          chunkSize,
		StatusUpdatesEnabled:      true,
		IncrementMode:             config.IncrementMode,
		IncrementMin:              config.IncrementMin,
		IncrementMax:              config.IncrementMax,
	}

	// Set association wordlist ID for keyspace calculation (convert UUID to string)
	if config.AssociationWordlistID != nil {
		assocIDStr := config.AssociationWordlistID.String()
		tempPreset.AssociationWordlistID = &assocIDStr
	}

	// Use the same keyspace calculation as preset jobs
	totalKeyspace, effectiveKeyspace, isAccurateKeyspace, err := s.calculateKeyspace(ctx, tempPreset, hashlist)
	if err != nil {
		debug.Error("Failed to calculate keyspace for custom job: %v", err)
		return nil, fmt.Errorf("keyspace calculation is required for job execution: %w", err)
	}

	// NOTE: useRuleSplitting is determined at creation time for accurate keyspace jobs,
	// or at first task dispatch (after benchmark) for estimate-based jobs as fallback
	useRuleSplitting := false

	// Calculate multiplication factor from keyspace values
	multiplicationFactor := 1
	if isAccurateKeyspace && totalKeyspace != nil && *totalKeyspace > 0 && effectiveKeyspace != nil && *effectiveKeyspace > 0 {
		multiplicationFactor = int(*effectiveKeyspace / *totalKeyspace)
		if multiplicationFactor < 1 {
			multiplicationFactor = 1
		}
	}

	// For salted hash types, adjust effective_keyspace by salt count
	// calculateKeyspace's --total-candidates = base × rules (no hashlist when run)
	// Job's effective_keyspace = base × rules × salts (to match progress[1])
	if effectiveKeyspace != nil && *effectiveKeyspace > 0 {
		hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
		if htErr == nil && hashType != nil && hashType.IsSalted {
			saltCount := int64(hashlist.TotalHashes)
			if saltCount > 0 {
				originalEffective := *effectiveKeyspace
				adjustedEffective := originalEffective * saltCount
				effectiveKeyspace = &adjustedEffective
				// Also adjust multiplication factor
				if totalKeyspace != nil && *totalKeyspace > 0 {
					multiplicationFactor = int(adjustedEffective / *totalKeyspace)
				}
				debug.Log("Applied salt adjustment to effective keyspace at custom job creation", map[string]interface{}{
					"custom_job_name":    config.Name,
					"hash_type_id":       hashlist.HashTypeID,
					"is_salted":          true,
					"salt_count":         saltCount,
					"original_effective": originalEffective,
					"adjusted_effective": adjustedEffective,
				})
			}
		}
	}

	// Determine rule splitting at creation time for accurate keyspace jobs
	// This avoids race conditions and mid-job switching issues - can't change strategy after tasks are dispatched
	if isAccurateKeyspace &&
		(config.AttackMode == models.AttackModeStraight || config.AttackMode == models.AttackModeAssociation) &&
		len(config.RuleIDs) > 0 {

		// Check if rule splitting is enabled
		ruleSplitEnabled, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_enabled")
		if err == nil && ruleSplitEnabled != nil && ruleSplitEnabled.Value != nil && *ruleSplitEnabled.Value == "true" {
			// Get minimum rules threshold
			minRulesSetting, _ := s.systemSettingsRepo.GetSetting(ctx, "rule_split_min_rules")
			minRules := 100 // default
			if minRulesSetting != nil && minRulesSetting.Value != nil {
				if parsed, parseErr := strconv.Atoi(*minRulesSetting.Value); parseErr == nil {
					minRules = parsed
				}
			}

			// Enable rule splitting if we have enough rules
			// Use actual rule count (not salt-adjusted multiplicationFactor) for minRules comparison
			actualRuleCount, ruleErr := s.GetTotalRuleCount(ctx, config.RuleIDs)
			if ruleErr != nil {
				actualRuleCount = int64(multiplicationFactor) // Fallback to multiplicationFactor
			}
			if int(actualRuleCount) >= minRules {
				useRuleSplitting = true
				debug.Log("Rule splitting enabled at custom job creation", map[string]interface{}{
					"custom_job_name":       config.Name,
					"actual_rule_count":     actualRuleCount,
					"multiplication_factor": multiplicationFactor,
					"min_rules":             minRules,
					"is_accurate_keyspace":  isAccurateKeyspace,
				})
			}
		}
	}

	// Set avg_rule_multiplier for accurate keyspace jobs (used in progress calculations)
	var avgRuleMultiplier *float64
	if isAccurateKeyspace && multiplicationFactor > 0 {
		v := float64(multiplicationFactor)
		avgRuleMultiplier = &v
	}

	// Create self-contained job execution
	jobExecution := &models.JobExecution{
		PresetJobID:           nil, // NULL for custom jobs
		HashlistID:            hashlistID,
		AssociationWordlistID: config.AssociationWordlistID, // For association attacks (-a 9)
		Status:                models.JobExecutionStatusPending,
		Priority:              config.Priority,
		ProcessedKeyspace:     0,
		AttackMode:            config.AttackMode,
		MaxAgents:             config.MaxAgents,
		CreatedBy:             createdBy,

		// Direct configuration (not from preset)
		Name:                      customJobName, // Will be set with proper naming logic
		WordlistIDs:               config.WordlistIDs,
		RuleIDs:                   config.RuleIDs,
		HashType:                  hashlist.HashTypeID,
		ChunkSizeSeconds:          chunkSize,
		StatusUpdatesEnabled:      true,
		AllowHighPriorityOverride: config.AllowHighPriorityOverride,
		BinaryVersion:           config.BinaryVersion,
		Mask:                      config.Mask,
		AdditionalArgs:            nil,
		IncrementMode:             config.IncrementMode,
		IncrementMin:              config.IncrementMin,
		IncrementMax:              config.IncrementMax,

		// Keyspace values from calculation
		BaseKeyspace:         totalKeyspace,
		EffectiveKeyspace:    effectiveKeyspace,
		MultiplicationFactor: multiplicationFactor,
		IsAccurateKeyspace:   isAccurateKeyspace,
		UsesRuleSplitting:    useRuleSplitting,
		AvgRuleMultiplier:    avgRuleMultiplier, // For progress calculations
	}

	err = s.jobExecRepo.Create(ctx, jobExecution)
	if err != nil {
		return nil, fmt.Errorf("failed to create custom job execution: %w", err)
	}

	// Initialize increment layers if increment mode is enabled
	// This must happen BEFORE calculateEffectiveKeyspace
	err = s.initializeIncrementLayers(ctx, jobExecution, tempPreset)
	if err != nil {
		debug.Error("Failed to initialize increment layers: job_execution_id=%s, error=%v",
			jobExecution.ID, err)
		return nil, fmt.Errorf("failed to initialize increment layers: %w", err)
	}

	// Use the same effective keyspace calculation
	// Skip for increment mode jobs - initializeIncrementLayers already sets both base_keyspace and effective_keyspace
	// Skip if we already have accurate keyspace (--total-candidates succeeded)
	// calculateEffectiveKeyspace would incorrectly overwrite base_keyspace with effective_keyspace value
	if (jobExecution.IncrementMode == "" || jobExecution.IncrementMode == "off") && !jobExecution.IsAccurateKeyspace {
		err = s.calculateEffectiveKeyspace(ctx, jobExecution, tempPreset)
		if err != nil {
			debug.Error("Failed to calculate effective keyspace: job_execution_id=%s, error=%v",
				jobExecution.ID, err)
			// Log the error but continue - we'll handle this in the scheduling logic
		}
	}

	// NOTE: Rule splitting determination is DEFERRED until after forced benchmark
	// The benchmark provides accurate effective keyspace from hashcat's progress[1]
	// See HandleBenchmarkResult() in job_websocket_integration.go for the actual decision
	debug.Log("Custom job execution created - rule split decision deferred to benchmark", map[string]interface{}{
		"job_execution_id":      jobExecution.ID,
		"base_keyspace":         totalKeyspace,
		"effective_keyspace":    jobExecution.EffectiveKeyspace,
		"multiplication_factor": jobExecution.MultiplicationFactor,
	})

	return jobExecution, nil
}

// calculateKeyspace calculates the total keyspace for a job using hashcat --keyspace
// Returns: baseKeyspace, effectiveKeyspace, isAccurateKeyspace, error
// If --total-candidates succeeds, effectiveKeyspace will be accurate and isAccurateKeyspace=true
// Otherwise, effectiveKeyspace will be an estimate and isAccurateKeyspace=false
func (s *JobExecutionService) calculateKeyspace(ctx context.Context, presetJob *models.PresetJob, hashlist *models.HashList) (*int64, *int64, bool, error) {
	debug.Log("Starting keyspace calculation for job execution", map[string]interface{}{
		"preset_job_id":     presetJob.ID,
		"binary_version":    presetJob.BinaryVersion,
		"attack_mode":       presetJob.AttackMode,
		"hashlist_id":       hashlist.ID,
		"data_directory":    s.dataDirectory,
	})

	// Resolve binary version pattern to actual binary ID
	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, presetJob.BinaryVersion)
	if err != nil {
		debug.Error("Failed to resolve binary version pattern: pattern=%s, error=%v",
			presetJob.BinaryVersion, err)
		return nil, nil, false, fmt.Errorf("failed to resolve binary version pattern %q: %w", presetJob.BinaryVersion, err)
	}

	// Get the hashcat binary path from binary manager
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		debug.Error("Failed to get hashcat binary path: binary_version_id=%d, error=%v",
			binaryVersionID, err)
		return nil, nil, false, fmt.Errorf("failed to get hashcat binary path for version %d: %w", binaryVersionID, err)
	}

	// Verify the binary exists and is executable
	if fileInfo, err := os.Stat(hashcatPath); err != nil {
		debug.Error("Hashcat binary not found: path=%s, error=%v",
			hashcatPath, err)
		return nil, nil, false, fmt.Errorf("hashcat binary not found at %s: %w", hashcatPath, err)
	} else {
		debug.Log("Found hashcat binary", map[string]interface{}{
			"path": hashcatPath,
			"size": fileInfo.Size(),
			"mode": fileInfo.Mode().String(),
		})
	}

	// Build hashcat command for keyspace calculation
	// For keyspace calculation, we don't need -m (hash type) or the hash file
	// We only need the attack-specific inputs
	var args []string

	// Add attack mode flag - REQUIRED for hashcat to interpret arguments correctly
	args = append(args, "-a", strconv.Itoa(int(presetJob.AttackMode)))

	// Add attack-specific arguments
	switch presetJob.AttackMode {
	case models.AttackModeStraight: // Dictionary attack (-a 0)
		// For straight attack, only need wordlist(s) and optionally rules
		// The keyspace is the number of words in the wordlist (or with rules applied)
		for _, wordlistIDStr := range presetJob.WordlistIDs {
			wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDStr)
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath)
		}
		// Add rules if any (rules don't change the keyspace command, but hashcat will calculate accordingly)
		for _, ruleIDStr := range presetJob.RuleIDs {
			rulePath, err := s.resolveRulePath(ctx, ruleIDStr)
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve rule path: %w", err)
			}
			args = append(args, "-r", rulePath)
		}

	case models.AttackModeCombination: // Combinator attack
		if len(presetJob.WordlistIDs) >= 2 {
			wordlist1Path, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve wordlist1 path: %w", err)
			}
			wordlist2Path, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[1])
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve wordlist2 path: %w", err)
			}
			args = append(args, wordlist1Path, wordlist2Path)
		}

	case models.AttackModeBruteForce: // Mask attack
		if presetJob.Mask != "" {
			args = append(args, presetJob.Mask)
		}

	case models.AttackModeHybridWordlistMask: // Hybrid Wordlist + Mask
		if len(presetJob.WordlistIDs) > 0 && presetJob.Mask != "" {
			wordlistPath, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath, presetJob.Mask)
		}

	case models.AttackModeHybridMaskWordlist: // Hybrid Mask + Wordlist
		if presetJob.Mask != "" && len(presetJob.WordlistIDs) > 0 {
			wordlistPath, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
			if err != nil {
				return nil, nil, false, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, presetJob.Mask, wordlistPath)
		}

	case models.AttackModeAssociation: // Association attack (-a 9)
		// Mode 9 does NOT support --keyspace or --total-candidates flags
		// Use estimation based on wordlist line count and rule count instead

		if presetJob.AssociationWordlistID == nil || *presetJob.AssociationWordlistID == "" {
			return nil, nil, false, fmt.Errorf("association wordlist ID is required for attack mode 9")
		}

		// Get wordlist line count from database
		lineCount, err := s.getAssociationWordlistLineCount(ctx, *presetJob.AssociationWordlistID)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to get association wordlist line count: %w", err)
		}

		// Get rule count for effective keyspace calculation
		ruleCount, err := s.GetTotalRuleCount(ctx, presetJob.RuleIDs)
		if err != nil {
			debug.Warning("Failed to get rule count for mode 9: %v", err)
			ruleCount = 1
		}

		baseKeyspace := lineCount
		effectiveKeyspace := lineCount * ruleCount

		debug.Log("Mode 9 keyspace estimation", map[string]interface{}{
			"wordlist_line_count": lineCount,
			"rule_count":          ruleCount,
			"base_keyspace":       baseKeyspace,
			"effective_keyspace":  effectiveKeyspace,
		})

		// Return early - don't run hashcat --keyspace (not supported for mode 9)
		return &baseKeyspace, &effectiveKeyspace, false, nil // false = not accurate (estimation)

	default:
		return nil, nil, false, fmt.Errorf("unsupported attack mode for keyspace calculation: %d", presetJob.AttackMode)
	}

	// Save base args for reuse with --total-candidates
	baseArgs := make([]string, len(args))
	copy(baseArgs, args)

	// Add keyspace flag
	args = append(args, "--keyspace")

	// Add session management to prevent conflicts (like preset version)
	args = append(args, "--restore-disable")

	// Add a unique session ID to allow concurrent executions
	sessionID := fmt.Sprintf("job_keyspace_%s_%d", hashlist.ID, time.Now().UnixNano())
	args = append(args, "--session", sessionID)

	// Add --quiet flag to suppress unnecessary output
	args = append(args, "--quiet")

	debug.Log("Calculating keyspace", map[string]interface{}{
		"command":     hashcatPath,
		"args":        args,
		"attack_mode": presetJob.AttackMode,
		"session_id":  sessionID,
	})

	// Execute hashcat command with timeout
	// Increase timeout to 4 minutes to allow for large wordlist processing and --total-candidates
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	startTime := time.Now()
	cmd := exec.CommandContext(ctx, hashcatPath, args...)

	// Set working directory to data directory to ensure session files are created there
	cmd.Dir = s.dataDirectory

	debug.Log("Executing hashcat command", map[string]interface{}{
		"working_dir": s.dataDirectory,
		"command":     hashcatPath,
		"args":        args,
	})

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Clean up session files regardless of success/failure
	sessionFiles := []string{
		filepath.Join(s.dataDirectory, sessionID+".log"),
		filepath.Join(s.dataDirectory, sessionID+".potfile"),
	}
	for _, file := range sessionFiles {
		_ = os.Remove(file)
	}

	if err != nil {
		// Log the full output for debugging
		debug.Error("Hashcat keyspace calculation failed: error=%v, stdout=%s, stderr=%s, working_dir=%s, command=%s, args=%v",
			err, stdout.String(), stderr.String(), s.dataDirectory, hashcatPath, args)
		return nil, nil, false, fmt.Errorf("hashcat keyspace calculation failed: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	// Parse keyspace from output
	// The keyspace should be the last line of stdout (ignoring stderr warnings about invalid rules)
	outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(outputLines) == 0 {
		return nil, nil, false, fmt.Errorf("no output from hashcat keyspace calculation")
	}

	// Get the last non-empty line
	var keyspaceStr string
	for i := len(outputLines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(outputLines[i])
		if line != "" {
			keyspaceStr = line
			break
		}
	}

	keyspace, err := strconv.ParseInt(keyspaceStr, 10, 64)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to parse keyspace '%s': %w", keyspaceStr, err)
	}

	if keyspace <= 0 {
		return nil, nil, false, fmt.Errorf("invalid keyspace: %d", keyspace)
	}

	duration := time.Since(startTime)
	debug.Log("Base keyspace calculated successfully", map[string]interface{}{
		"keyspace":        keyspace,
		"duration":        duration.String(),
		"stderr_warnings": stderr.String(),
	})

	// Step 2: Calculate effective keyspace using --total-candidates
	// This accounts for rule effectiveness and gives the true candidate count
	effectiveKeyspace, isAccurate, err := s.calculateTotalCandidates(ctx, hashcatPath, baseArgs, strconv.FormatInt(hashlist.ID, 10))
	if err != nil {
		// Error is unexpected - log and fall back to estimation
		debug.Warning("Error calculating total candidates: %v, falling back to estimation", err)
		isAccurate = false
	}

	var effectiveKeyspacePtr *int64
	if isAccurate && effectiveKeyspace > 0 {
		// Use accurate value from --total-candidates
		effectiveKeyspacePtr = &effectiveKeyspace

		debug.Log("Using accurate effective keyspace from --total-candidates", map[string]interface{}{
			"hashlist_id":        hashlist.ID,
			"base_keyspace":      keyspace,
			"effective_keyspace": effectiveKeyspace,
		})
	} else {
		// Fall back to estimation: base * rule_count
		estimatedEffective := keyspace
		if len(presetJob.RuleIDs) > 0 {
			// For estimation, assume each rule file has approximately 1 rule on average
			// This is conservative - actual count may vary significantly
			ruleCount := int64(len(presetJob.RuleIDs))
			if ruleCount > 0 {
				estimatedEffective = keyspace * ruleCount
			}
		}
		effectiveKeyspacePtr = &estimatedEffective

		debug.Log("Using estimated effective keyspace (--total-candidates failed or unavailable)", map[string]interface{}{
			"hashlist_id":       hashlist.ID,
			"base_keyspace":     keyspace,
			"estimated_effective": estimatedEffective,
			"rule_count":        len(presetJob.RuleIDs),
		})
	}

	debug.Log("Keyspace calculation complete", map[string]interface{}{
		"hashlist_id":          hashlist.ID,
		"base_keyspace":        keyspace,
		"effective_keyspace":   effectiveKeyspacePtr,
		"is_accurate_keyspace": isAccurate,
	})

	return &keyspace, effectiveKeyspacePtr, isAccurate, nil
}

// calculateTotalCandidates runs hashcat --total-candidates with retry logic to get actual effective keyspace.
// This accounts for rule effectiveness and gives the true candidate count.
// Returns (effectiveKeyspace, isAccurate, error)
// On failure after retries, returns (0, false, nil) to allow fallback to estimation.
func (s *JobExecutionService) calculateTotalCandidates(
	ctx context.Context,
	hashcatPath string,
	baseArgs []string,
	jobID string,
) (int64, bool, error) {
	const maxRetries = 3
	const retryDelay = 5 * time.Second

	// Build args for --total-candidates (same as --keyspace but different flag)
	args := make([]string, len(baseArgs))
	copy(args, baseArgs)
	args = append(args, "--total-candidates")

	// Add session management
	args = append(args, "--restore-disable")
	sessionID := fmt.Sprintf("job_total_candidates_%s_%d", jobID, time.Now().UnixNano())
	args = append(args, "--session", sessionID)
	args = append(args, "--quiet")

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			debug.Warning("Retrying --total-candidates (attempt %d/%d) after %v delay: %v",
				attempt, maxRetries, retryDelay, lastErr)
			time.Sleep(retryDelay)
		}

		execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(execCtx, hashcatPath, args...)
		cmd.Dir = s.dataDirectory

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		cancel()

		// Clean up session files
		sessionFiles := []string{
			filepath.Join(s.dataDirectory, sessionID+".log"),
			filepath.Join(s.dataDirectory, sessionID+".potfile"),
		}
		for _, file := range sessionFiles {
			_ = os.Remove(file)
		}

		if err != nil {
			stderrStr := stderr.String()
			// Check if hashcat is busy (already running)
			if strings.Contains(stderrStr, "Already an instance") ||
				strings.Contains(stderrStr, "already running") {
				lastErr = fmt.Errorf("hashcat busy: %s", stderrStr)
				continue // Retry
			}
			// Other error - log and allow fallback
			debug.Warning("--total-candidates failed: %v, stderr: %s", err, stderrStr)
			return 0, false, nil // Allow fallback to estimation
		}

		// Parse result - get last non-empty line
		outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		var keyspaceStr string
		for i := len(outputLines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(outputLines[i])
			if line != "" {
				keyspaceStr = line
				break
			}
		}

		effectiveKeyspace, parseErr := strconv.ParseInt(keyspaceStr, 10, 64)
		if parseErr != nil {
			debug.Warning("Failed to parse --total-candidates output '%s': %v", keyspaceStr, parseErr)
			return 0, false, nil // Allow fallback
		}

		debug.Log("Calculated total candidates successfully", map[string]interface{}{
			"job_id":             jobID,
			"effective_keyspace": effectiveKeyspace,
			"method":             "--total-candidates",
		})

		return effectiveKeyspace, true, nil
	}

	debug.Warning("--total-candidates exhausted retries for job %s: %v", jobID, lastErr)
	return 0, false, nil // Allow fallback to estimation
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// parseAttackMode extracts the attack mode from a preset job
func (s *JobExecutionService) parseAttackMode(presetJob *models.PresetJob) int {
	return int(presetJob.AttackMode)
}

// extractRuleFiles returns the rule file paths from a preset job
func (s *JobExecutionService) extractRuleFiles(ctx context.Context, presetJob *models.PresetJob) ([]string, error) {
	var rulePaths []string
	for _, ruleIDStr := range presetJob.RuleIDs {
		rulePath, err := s.resolveRulePath(ctx, ruleIDStr)
		if err != nil {
			debug.Log("Failed to resolve rule path", map[string]interface{}{
				"rule_id": ruleIDStr,
				"error":   err.Error(),
			})
			continue // Skip invalid rules
		}
		rulePaths = append(rulePaths, rulePath)
	}
	return rulePaths, nil
}

// extractWordlists returns the wordlist file paths from a preset job
func (s *JobExecutionService) extractWordlists(ctx context.Context, presetJob *models.PresetJob) ([]string, error) {
	var wordlistPaths []string
	for _, wordlistIDStr := range presetJob.WordlistIDs {
		wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDStr)
		if err != nil {
			debug.Log("Failed to resolve wordlist path", map[string]interface{}{
				"wordlist_id": wordlistIDStr,
				"error":       err.Error(),
			})
			continue // Skip invalid wordlists
		}
		wordlistPaths = append(wordlistPaths, wordlistPath)
	}
	return wordlistPaths, nil
}

// countRulesInFile counts the number of rules in a rule file
func (s *JobExecutionService) countRulesInFile(ctx context.Context, rulePath string) (int, error) {
	// For now, we'll use a simple line count
	// In a real implementation, this might use a rule manager or more sophisticated parsing
	file, err := os.Open(rulePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open rule file: %w", err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read rule file: %w", err)
	}

	return count, nil
}

// calculateWordlistKeyspace calculates the keyspace for a single wordlist
func (s *JobExecutionService) calculateWordlistKeyspace(ctx context.Context, wordlistPath string) (int64, error) {
	// For a simple wordlist, the keyspace is the number of lines
	file, err := os.Open(wordlistPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open wordlist file: %w", err)
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read wordlist file: %w", err)
	}

	return count, nil
}

// calculateEffectiveKeyspace computes the true workload accounting for rules/combinations
func (s *JobExecutionService) calculateEffectiveKeyspace(ctx context.Context, job *models.JobExecution, presetJob *models.PresetJob) error {
	// Use existing base_keyspace (from hashcat --keyspace) as starting point
	if job.BaseKeyspace == nil {
		return fmt.Errorf("job has no base keyspace calculated")
	}

	baseKeyspace := *job.BaseKeyspace
	attackMode := s.parseAttackMode(presetJob)

	debug.Log("Calculating effective keyspace", map[string]interface{}{
		"job_id":        job.ID,
		"base_keyspace": baseKeyspace,
		"attack_mode":   attackMode,
		"rule_ids":      presetJob.RuleIDs,
		"data_directory": s.dataDirectory,
	})

	switch models.AttackMode(attackMode) {
	case models.AttackModeStraight: // Straight attack
		if len(presetJob.RuleIDs) > 0 {
			// Get all rules from database to get rule counts
			allRules, err := s.fileRepo.GetRules(ctx, "")
			if err != nil {
				return fmt.Errorf("failed to get rules from database: %w", err)
			}

			// Build a map of rule ID to rule info for quick lookup
			ruleMap := make(map[int]repository.FileInfo)
			for _, rule := range allRules {
				ruleMap[rule.ID] = rule
			}

			// Calculate total rule count (simple sum, no learned multipliers)
			// NOTE: We do NOT use estimated_keyspace_multiplier for initial estimates.
			// The benchmark will provide accurate effective keyspace from hashcat's progress[1].
			var totalRuleCount int64

			for _, ruleIDStr := range presetJob.RuleIDs {
				ruleID, err := strconv.Atoi(ruleIDStr)
				if err != nil {
					debug.Error("Invalid rule ID format: rule_id=%s, error=%v", ruleIDStr, err)
					continue
				}

				rule, ok := ruleMap[ruleID]
				if !ok {
					debug.Error("Rule not found in database: rule_id=%d", ruleID)
					continue
				}

				totalRuleCount += rule.RuleCount

				debug.Log("Processing rule for keyspace calculation", map[string]interface{}{
					"rule_id":    ruleID,
					"rule_count": rule.RuleCount,
				})
			}

			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = int(totalRuleCount)
			job.IsAccurateKeyspace = false // Will be set by first agent benchmark or --total-candidates

			// Estimate effective keyspace using simple formula
			// Formula: base_keyspace × rule_count
			// NOTE: hash_count is NOT part of keyspace - keyspace is about candidates to try,
			// not targets. This is an estimate; --total-candidates or benchmark provides accurate values.
			estimatedEffective := baseKeyspace * totalRuleCount
			job.EffectiveKeyspace = &estimatedEffective

			debug.Log("Straight attack with rules - estimated effective keyspace", map[string]interface{}{
				"rule_count":          totalRuleCount,
				"base_keyspace":       baseKeyspace,
				"estimated_effective": estimatedEffective,
			})
		} else {
			// No rules, effective = base
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = &baseKeyspace
		}

	case models.AttackModeCombination: // Combination attack
		wordlists, err := s.extractWordlists(ctx, presetJob)
		if err != nil {
			return fmt.Errorf("failed to extract wordlists: %w", err)
		}

		if len(wordlists) >= 2 {
			keyspace1, err := s.calculateWordlistKeyspace(ctx, wordlists[0])
			if err != nil {
				return fmt.Errorf("failed to calculate keyspace for wordlist 1: %w", err)
			}

			keyspace2, err := s.calculateWordlistKeyspace(ctx, wordlists[1])
			if err != nil {
				return fmt.Errorf("failed to calculate keyspace for wordlist 2: %w", err)
			}

			// The base keyspace from hashcat is the larger wordlist
			job.BaseKeyspace = &baseKeyspace

			// Multiplication factor is the smaller wordlist
			if keyspace1 > keyspace2 {
				job.MultiplicationFactor = int(keyspace2)
			} else {
				job.MultiplicationFactor = int(keyspace1)
			}

			job.IsAccurateKeyspace = false // Will be set by first agent benchmark

			// Estimate effective keyspace (will be updated to actual from hashcat benchmark)
			estimatedEffective := keyspace1 * keyspace2
			job.EffectiveKeyspace = &estimatedEffective

			debug.Log("Combination attack - using estimated effective keyspace", map[string]interface{}{
				"wordlist1_keyspace":  keyspace1,
				"wordlist2_keyspace":  keyspace2,
				"estimated_effective": estimatedEffective,
			})
		} else {
			// Not enough wordlists for combination
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = &baseKeyspace
		}

	case models.AttackModeAssociation: // Association attack
		// For association attack, use the wordlist line count from database
		// The association wordlist ID is stored in presetJob.AssociationWordlistID
		if presetJob.AssociationWordlistID != nil && *presetJob.AssociationWordlistID != "" {
			lineCount, err := s.getAssociationWordlistLineCount(ctx, *presetJob.AssociationWordlistID)
			if err != nil {
				debug.Warning("Failed to get association wordlist line count: %v", err)
				// Fall back to using baseKeyspace
				job.BaseKeyspace = &baseKeyspace
				job.MultiplicationFactor = 1
				job.EffectiveKeyspace = &baseKeyspace
			} else {
				ruleCount, err := s.GetTotalRuleCount(ctx, presetJob.RuleIDs)
				if err != nil {
					ruleCount = 1
				}

				job.BaseKeyspace = &lineCount
				job.MultiplicationFactor = int(ruleCount)
				// Mode 9 keyspace is estimated from wordlist line count × rule count
				// IsAccurateKeyspace = false triggers forced benchmark for speed measurement
				job.IsAccurateKeyspace = false

				effectiveKeyspace := lineCount * ruleCount
				job.EffectiveKeyspace = &effectiveKeyspace

				debug.Log("Association attack keyspace calculated", map[string]interface{}{
					"wordlist_line_count": lineCount,
					"rule_count":          ruleCount,
					"effective_keyspace":  effectiveKeyspace,
					"is_accurate":         false,
				})
			}
		} else {
			// No association wordlist - shouldn't happen but handle gracefully
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = &baseKeyspace
		}

	default: // Attacks 3, 6, 7 - hashcat calculates correctly
		job.BaseKeyspace = &baseKeyspace
		job.MultiplicationFactor = 1
		job.EffectiveKeyspace = &baseKeyspace

		debug.Log("Standard attack mode", map[string]interface{}{
			"attack_mode": attackMode,
			"keyspace":    baseKeyspace,
		})
	}

	// Apply salt adjustment for salted hash types (same pattern as CreateCustomJobExecution:542-567)
	// calculateEffectiveKeyspace calculates base × rules, but for salted hashes we need base × rules × salts
	if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace > 0 {
		hashlist, hlErr := s.hashlistRepo.GetByID(ctx, job.HashlistID)
		if hlErr == nil && hashlist != nil {
			hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
			if htErr == nil && hashType != nil && hashType.IsSalted {
				saltCount := int64(hashlist.TotalHashes)
				if saltCount > 0 {
					originalEffective := *job.EffectiveKeyspace
					adjustedEffective := originalEffective * saltCount
					job.EffectiveKeyspace = &adjustedEffective
					// Also adjust multiplication factor to reflect salts
					if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
						job.MultiplicationFactor = int(adjustedEffective / *job.BaseKeyspace)
					}
					debug.Log("Applied salt adjustment in calculateEffectiveKeyspace", map[string]interface{}{
						"job_id":             job.ID,
						"hash_type_id":       hashlist.HashTypeID,
						"is_salted":          true,
						"salt_count":         saltCount,
						"original_effective": originalEffective,
						"adjusted_effective": adjustedEffective,
					})
				}
			}
		}
	}

	// Update job in database
	return s.jobExecRepo.UpdateKeyspaceInfo(ctx, job)
}

// GetNextPendingJob returns the next job to be executed based on priority and FIFO
// DEPRECATED: Use GetNextJobWithWork instead
func (s *JobExecutionService) GetNextPendingJob(ctx context.Context) (*models.JobExecution, error) {
	debug.Log("Getting next pending job", nil)

	pendingJobs, err := s.jobExecRepo.GetPendingJobs(ctx)
	if err != nil {
		debug.Log("Failed to get pending jobs from repository", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to get pending jobs: %w", err)
	}

	debug.Log("Retrieved pending jobs", map[string]interface{}{
		"count": len(pendingJobs),
	})

	if len(pendingJobs) == 0 {
		return nil, nil // No pending jobs
	}

	// Jobs are already ordered by priority DESC, created_at ASC in the repository
	nextJob := &pendingJobs[0]
	debug.Log("Selected next job", map[string]interface{}{
		"job_id":      nextJob.ID,
		"priority":    nextJob.Priority,
		"job_name":    nextJob.Name,
		"hashlist_id": nextJob.HashlistID,
	})

	return nextJob, nil
}

// GetAllJobsWithPendingWork returns all jobs that have pending work
// Used by the priority-based scheduling algorithm
// Jobs are ordered by priority DESC, created_at ASC (FIFO for same priority)
func (s *JobExecutionService) GetAllJobsWithPendingWork(ctx context.Context) ([]models.JobExecutionWithWork, error) {
	return s.jobExecRepo.GetJobsWithPendingWork(ctx)
}

// GetNextJobWithWork returns the next job that has work available and isn't at max agent capacity
// Jobs are ordered by priority DESC, created_at ASC (FIFO for same priority)
func (s *JobExecutionService) GetNextJobWithWork(ctx context.Context) (*models.JobExecutionWithWork, error) {
	debug.Log("Getting next job with available work", nil)

	jobsWithWork, err := s.jobExecRepo.GetJobsWithPendingWork(ctx)
	if err != nil {
		debug.Log("Failed to get jobs with pending work", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("failed to get jobs with pending work: %w", err)
	}

	debug.Log("Retrieved jobs with pending work", map[string]interface{}{
		"count": len(jobsWithWork),
	})

	if len(jobsWithWork) == 0 {
		return nil, nil // No jobs with available work
	}

	// Jobs are already filtered and ordered correctly by the repository
	nextJob := &jobsWithWork[0]
	debug.Log("Selected next job with work", map[string]interface{}{
		"job_id":        nextJob.ID,
		"priority":      nextJob.Priority,
		"job_name":      nextJob.Name,
		"hashlist_id":   nextJob.HashlistID,
		"active_agents": nextJob.ActiveAgents,
		"max_agents":    nextJob.MaxAgents,
		"pending_work":  nextJob.PendingWork,
		"status":        nextJob.Status,
	})

	return nextJob, nil
}

// GetAvailableAgents returns agents that are available to take on new work
func (s *JobExecutionService) GetAvailableAgents(ctx context.Context) ([]models.Agent, error) {
	// Get max concurrent jobs per agent setting
	maxConcurrentSetting, err := s.systemSettingsRepo.GetSetting(ctx, "max_concurrent_jobs_per_agent")
	if err != nil {
		return nil, fmt.Errorf("failed to get max concurrent jobs setting: %w", err)
	}

	maxConcurrent := 1 // Default: one task per agent
	if maxConcurrentSetting.Value != nil {
		if parsed, parseErr := strconv.Atoi(*maxConcurrentSetting.Value); parseErr == nil {
			maxConcurrent = parsed
		}
	}

	// Get all active agents
	agents, err := s.agentRepo.List(ctx, map[string]interface{}{"status": models.AgentStatusActive})
	if err != nil {
		return nil, fmt.Errorf("failed to get active agents: %w", err)
	}

	debug.Log("Found active agents", map[string]interface{}{
		"agent_count": len(agents),
	})

	var availableAgents []models.Agent
	for _, agent := range agents {
		debug.Log("Checking agent availability", map[string]interface{}{
			"agent_id":   agent.ID,
			"status":     agent.Status,
			"is_enabled": agent.IsEnabled,
		})

		// Clean up stale busy states before checking availability
		if agent.Metadata != nil && agent.Metadata["busy_status"] == "true" {
			if taskIDStr, exists := agent.Metadata["current_task_id"]; exists && taskIDStr != "" {
				// Try to parse and verify the task
				taskUUID, err := uuid.Parse(taskIDStr)
				if err != nil {
					// Invalid task ID, clear stale busy status
					debug.Log("Clearing stale busy status with invalid task ID in GetAvailableAgents", map[string]interface{}{
						"agent_id":      agent.ID,
						"invalid_task": taskIDStr,
					})
					agent.Metadata["busy_status"] = "false"
					delete(agent.Metadata, "current_task_id")
					delete(agent.Metadata, "current_job_id")
					s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
				} else {
					// Valid UUID, check if task exists and is actually assigned to this agent
					task, err := s.jobTaskRepo.GetByID(ctx, taskUUID)
					if err != nil || task == nil {
						// Task doesn't exist
						debug.Log("Clearing stale busy status - task not found", map[string]interface{}{
							"agent_id":      agent.ID,
							"stale_task_id": taskIDStr,
						})
						agent.Metadata["busy_status"] = "false"
						delete(agent.Metadata, "current_task_id")
						delete(agent.Metadata, "current_job_id")
						s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
					} else if task.AgentID == nil || *task.AgentID != agent.ID {
						// Task not assigned to this agent
						debug.Log("Clearing stale busy status - task assigned to different agent", map[string]interface{}{
							"agent_id":      agent.ID,
							"stale_task_id": taskIDStr,
							"task_agent_id": task.AgentID,
						})
						agent.Metadata["busy_status"] = "false"
						delete(agent.Metadata, "current_task_id")
						delete(agent.Metadata, "current_job_id")
						s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
					} else if task.Status == models.JobTaskStatusCompleted ||
						task.Status == models.JobTaskStatusFailed ||
						task.Status == models.JobTaskStatusCancelled ||
						task.Status == models.JobTaskStatusProcessing {
						// Task is in terminal state for AGENT availability (agent is free to accept new work)
						// Note: Processing = hashcat done, backend still ingesting cracks - agent should be free
						// Agent should be free but busy_status wasn't cleared - this is the race condition fix
						debug.Warning("Clearing stale busy status - task in terminal state %s (GH Issue #12 recovery)",
							task.Status)
						debug.Log("Agent busy status recovery", map[string]interface{}{
							"agent_id":     agent.ID,
							"task_id":      taskIDStr,
							"task_status":  task.Status,
							"reason":       "task_in_terminal_state",
						})
						agent.Metadata["busy_status"] = "false"
						delete(agent.Metadata, "current_task_id")
						delete(agent.Metadata, "current_job_id")
						s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
					} else if task.Status != models.JobTaskStatusRunning && task.Status != models.JobTaskStatusAssigned {
						// Task in unexpected state
						debug.Log("Clearing stale busy status - task in unexpected state", map[string]interface{}{
							"agent_id":     agent.ID,
							"stale_task_id": taskIDStr,
							"task_status":  task.Status,
						})
						agent.Metadata["busy_status"] = "false"
						delete(agent.Metadata, "current_task_id")
						delete(agent.Metadata, "current_job_id")
						s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
					}
				}
			} else {
				// No task ID but marked as busy, clear it
				debug.Log("Clearing busy status with no task ID in GetAvailableAgents", map[string]interface{}{
					"agent_id": agent.ID,
				})
				agent.Metadata["busy_status"] = "false"
				delete(agent.Metadata, "current_task_id")
				delete(agent.Metadata, "current_job_id")
				s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
			}
		}

		// Skip disabled agents (maintenance mode)
		if !agent.IsEnabled {
			debug.Log("Agent is disabled (maintenance mode), skipping", map[string]interface{}{
				"agent_id": agent.ID,
			})
			continue
		}

		// Skip agents that haven't completed file sync
		if agent.SyncStatus != models.AgentSyncStatusCompleted {
			debug.Log("Agent has not completed file sync, skipping", map[string]interface{}{
				"agent_id":    agent.ID,
				"sync_status": agent.SyncStatus,
			})
			continue
		}

		// Count active tasks for this agent
		activeTasks, err := s.jobTaskRepo.GetActiveTasksByAgent(ctx, agent.ID)
		if err != nil {
			debug.Log("Failed to get active tasks for agent", map[string]interface{}{
				"agent_id": agent.ID,
				"error":    err.Error(),
			})
			continue
		}

		debug.Log("Agent task status", map[string]interface{}{
			"agent_id":       agent.ID,
			"active_tasks":   len(activeTasks),
			"max_concurrent": maxConcurrent,
			"is_available":   len(activeTasks) < maxConcurrent,
		})

		if len(activeTasks) < maxConcurrent {
			// Check if agent has enabled devices
			hasEnabledDevices, err := s.deviceRepo.HasEnabledDevices(agent.ID)
			if err != nil {
				debug.Log("Failed to check enabled devices for agent", map[string]interface{}{
					"agent_id": agent.ID,
					"error":    err.Error(),
				})
				continue
			}

			if hasEnabledDevices {
				// Check if scheduling is enabled for this agent
				if agent.SchedulingEnabled {
					// Check if scheduling system is enabled globally
					schedulingSetting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_scheduling_enabled")
					if err == nil && schedulingSetting.Value != nil && *schedulingSetting.Value == "true" {
						// Check if agent is scheduled for current UTC time
						isScheduled, err := s.scheduleRepo.IsAgentScheduledNow(ctx, agent.ID)
						if err != nil {
							debug.Log("Failed to check agent schedule", map[string]interface{}{
								"agent_id": agent.ID,
								"error":    err.Error(),
							})
							continue
						}
						if !isScheduled {
							debug.Log("Agent is not scheduled for current time, skipping", map[string]interface{}{
								"agent_id": agent.ID,
							})
							continue
						}
					}
				}
				
				availableAgents = append(availableAgents, agent)
			} else {
				debug.Log("Agent has no enabled devices, skipping", map[string]interface{}{
					"agent_id": agent.ID,
				})
			}
		}
	}

	return availableAgents, nil
}

// CreateJobTask creates a task chunk for an agent
func (s *JobExecutionService) CreateJobTask(ctx context.Context, jobExecution *models.JobExecution, agent *models.Agent, keyspaceStart, keyspaceEnd int64, benchmarkSpeed *int64, chunkDuration int) (*models.JobTask, error) {

	// Estimate effective keyspace for this task (will be updated to actual from hashcat progress[1])
	var effectiveStart, effectiveEnd int64

	if jobExecution.UsesRuleSplitting && jobExecution.BaseKeyspace != nil && *jobExecution.BaseKeyspace > 0 {
		// Rule-split task: estimate based on rule range for this chunk
		// This will be populated later when we have the actual rule indices
		// For now, use a placeholder that will be updated
		effectiveStart = 0
		effectiveEnd = keyspaceEnd - keyspaceStart // Just the chunk size for now

		debug.Log("Rule-split task - will calculate effective from rule indices", map[string]interface{}{
			"job_id":         jobExecution.ID,
			"keyspace_start": keyspaceStart,
			"keyspace_end":   keyspaceEnd,
		})
	} else if jobExecution.MultiplicationFactor > 1 && jobExecution.BaseKeyspace != nil && *jobExecution.BaseKeyspace > 0 {
		// Non-split task with rules: estimate total effective keyspace
		effectiveStart = 0
		effectiveEnd = *jobExecution.BaseKeyspace * int64(jobExecution.MultiplicationFactor)

		debug.Log("Non-split task with rules - estimated effective keyspace", map[string]interface{}{
			"job_id":              jobExecution.ID,
			"base_keyspace":       *jobExecution.BaseKeyspace,
			"multiplication_factor": jobExecution.MultiplicationFactor,
			"estimated_effective": effectiveEnd,
		})
	} else {
		// No rules: effective = base keyspace range
		effectiveStart = keyspaceStart
		effectiveEnd = keyspaceEnd

		debug.Log("No rules - effective equals base keyspace", map[string]interface{}{
			"job_id":         jobExecution.ID,
			"keyspace_start": keyspaceStart,
			"keyspace_end":   keyspaceEnd,
		})
	}

	// Data integrity check: validate that task's effective keyspace doesn't exceed job total (with 10% tolerance for estimates)
	if jobExecution.EffectiveKeyspace != nil && effectiveEnd > (*jobExecution.EffectiveKeyspace + (*jobExecution.EffectiveKeyspace / 10)) {
		debug.Warning("Task effective_keyspace_end exceeds job total (with tolerance): job_id=%s, task_effective_end=%d, job_effective_total=%d",
			jobExecution.ID, effectiveEnd, *jobExecution.EffectiveKeyspace)
	}

	effectiveProcessed := int64(0)
	jobTask := &models.JobTask{
		JobExecutionID:             jobExecution.ID,
		AgentID:                    &agent.ID,
		Status:                     models.JobTaskStatusPending,
		KeyspaceStart:              keyspaceStart,
		KeyspaceEnd:                keyspaceEnd,
		KeyspaceProcessed:          0,
		EffectiveKeyspaceStart:     &effectiveStart,
		EffectiveKeyspaceEnd:       &effectiveEnd,
		EffectiveKeyspaceProcessed: &effectiveProcessed,
		BenchmarkSpeed:             benchmarkSpeed,
		ChunkDuration:              chunkDuration,
		IsActualKeyspace:           false, // Will be updated from hashcat progress[1]
	}

	err := s.jobTaskRepo.Create(ctx, jobTask)
	if err != nil {
		return nil, fmt.Errorf("failed to create job task: %w", err)
	}

	// Update dispatched keyspace for the job execution
	// Use the EFFECTIVE keyspace estimate that we calculated and stored in the task
	// This ensures the adjustment logic has the correct baseline to work from
	effectiveChunkSize := effectiveEnd - effectiveStart
	baseChunkSize := keyspaceEnd - keyspaceStart

	debug.Log("Incrementing dispatched keyspace", map[string]interface{}{
		"job_id":               jobExecution.ID,
		"effective_chunk_size": effectiveChunkSize,
		"base_chunk_size":      baseChunkSize,
	})

	debug.Log("Job task created", map[string]interface{}{
		"task_id":               jobTask.ID,
		"agent_id":              agent.ID,
		"keyspace_start":        keyspaceStart,
		"keyspace_end":          keyspaceEnd,
		"chunk_duration":        chunkDuration,
		"base_chunk_size":       baseChunkSize,
		"effective_chunk_size":  effectiveChunkSize,
	})

	return jobTask, nil
}

// StartJobExecution marks a job execution as started
func (s *JobExecutionService) StartJobExecution(ctx context.Context, jobExecutionID uuid.UUID) error {
	err := s.jobExecRepo.StartExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to start job execution: %w", err)
	}

	debug.Log("Job execution started", map[string]interface{}{
		"job_execution_id": jobExecutionID,
	})

	// Dispatch job started notification
	jobExec, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		debug.Error("Failed to get job execution for notification: %v", err)
	} else if jobExec.CreatedBy != nil {
		s.dispatchJobStartedNotification(ctx, jobExec)
	}

	return nil
}

// CompleteJobExecution marks a job execution as completed
func (s *JobExecutionService) CompleteJobExecution(ctx context.Context, jobExecutionID uuid.UUID) error {
	// CRITICAL: Check for failed tasks - job should be marked failed, not completed
	hasFailed, err := s.jobTaskRepo.HasFailedTasks(ctx, jobExecutionID)
	if err != nil {
		debug.Error("Failed to check for failed tasks: %v", err)
		// Continue with completion check, but log the error
	} else if hasFailed {
		debug.Warning("Job %s has failed tasks - marking as failed, not completed", jobExecutionID)
		if failErr := s.jobExecRepo.FailExecution(ctx, jobExecutionID, "One or more tasks failed"); failErr != nil {
			return fmt.Errorf("failed to mark job as failed: %w", failErr)
		}
		// Dispatch job failed notification
		jobExec, getErr := s.jobExecRepo.GetByID(ctx, jobExecutionID)
		if getErr == nil && jobExec.CreatedBy != nil {
			s.dispatchJobFailedNotification(ctx, jobExec, "One or more tasks failed")
		}
		return nil
	}

	// KEYSPACE SYNC: Ensure effective_keyspace and dispatched_keyspace match actual work performed.
	// This prevents stuck job issues where effective_keyspace drifted due to wordlist/potfile updates.
	// We use the sum of chunk_actual_keyspace from completed tasks as the source of truth.
	actualKeyspace, err := s.jobTaskRepo.GetSumChunkActualKeyspace(ctx, jobExecutionID)
	if err != nil {
		debug.Warning("Failed to get sum of chunk actual keyspace: %v", err)
		// Continue with completion even if we can't get actual keyspace
	} else if actualKeyspace > 0 {
		job, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
		if err == nil {
			needsUpdate := false
			oldEffective := int64(0)
			if job.EffectiveKeyspace != nil {
				oldEffective = *job.EffectiveKeyspace
			}

			// Update if effective_keyspace differs from actual
			if job.EffectiveKeyspace == nil || *job.EffectiveKeyspace != actualKeyspace {
				needsUpdate = true
			}

			if needsUpdate {
				// Update effective_keyspace to match actual work
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, actualKeyspace); updateErr != nil {
					debug.Error("Failed to update effective keyspace at completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace at completion: %d -> %d (actual from tasks)", oldEffective, actualKeyspace)
				}

				// Also sync dispatched_keyspace to match (ensures 100% progress display)
				if updateErr := s.jobExecRepo.UpdateDispatchedKeyspace(ctx, jobExecutionID, actualKeyspace); updateErr != nil {
					debug.Error("Failed to update dispatched keyspace at completion: %v", updateErr)
				}
			}
		}
	}

	// Legacy check: If no chunk_actual_keyspace data, fall back to old method
	if actualKeyspace == 0 {
		tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobExecutionID)
		if err == nil {
			// Check if all tasks have actual keyspace values from hashcat
			allTasksHaveActualKeyspace := len(tasks) > 0
			for _, task := range tasks {
				if !task.IsActualKeyspace {
					allTasksHaveActualKeyspace = false
					break
				}
			}

			// If all tasks reported actual keyspace, update job's effective_keyspace to match processed
			if allTasksHaveActualKeyspace {
				job, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
				if err == nil && job.ProcessedKeyspace > 0 {
					if job.EffectiveKeyspace != nil && *job.EffectiveKeyspace != job.ProcessedKeyspace {
						oldEffective := *job.EffectiveKeyspace
						if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
							debug.Error("Failed to update effective keyspace (legacy): %v", updateErr)
						} else {
							debug.Info("Updated effective_keyspace from %d to %d (legacy method)", oldEffective, job.ProcessedKeyspace)
						}
					}
				}
			}
		}
	}

	// COMPLETION SYNC: Ensure effective_keyspace matches processed_keyspace for accurate 100% display.
	// Two cases:
	// 1. Early completion (processed < effective): hashcat code 6 when all hashes cracked early
	// 2. Over-completion (processed > effective): benchmark estimate was lower than actual keyspace
	// This runs AFTER the above task sync to ensure we capture the final state.
	{
		job, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
		if err == nil && job.ProcessedKeyspace > 0 {
			if job.EffectiveKeyspace != nil && job.ProcessedKeyspace < *job.EffectiveKeyspace {
				// Case 1: Early completion - sync effective DOWN to processed
				oldEffective := *job.EffectiveKeyspace
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync effective_keyspace for early completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace for early completion: %d -> %d", oldEffective, job.ProcessedKeyspace)
				}
				// Also sync dispatched_keyspace to match
				if updateErr := s.jobExecRepo.UpdateDispatchedKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync dispatched_keyspace for early completion: %v", updateErr)
				}
			} else if job.EffectiveKeyspace == nil || *job.EffectiveKeyspace < job.ProcessedKeyspace {
				// Case 2: Over-completion - benchmark gave lower estimate than actual work
				// Sync effective UP to processed to prevent >100% progress display
				oldEffective := int64(0)
				if job.EffectiveKeyspace != nil {
					oldEffective = *job.EffectiveKeyspace
				}
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync effective_keyspace for over-completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace for over-completion: %d -> %d", oldEffective, job.ProcessedKeyspace)
				}
			}
		}
	}

	// Mark the job as completed
	err = s.jobExecRepo.CompleteExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to complete job execution: %w", err)
	}

	// Get the job execution to find the user who created it
	jobExec, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		debug.Error("Failed to get job execution for notification: %v", err)
		// Don't fail the completion due to notification errors
	} else if jobExec.CreatedBy != nil {
		// Dispatch job completion notification via new dispatcher
		s.dispatchJobCompletedNotification(ctx, jobExec)
	}

	// Clean up resources for completed job
	if cleanupErr := s.CleanupJobResources(ctx, jobExecutionID); cleanupErr != nil {
		debug.Error("Failed to cleanup job resources: %v", cleanupErr)
		// Don't fail the completion due to cleanup errors
	}

	debug.Log("Job execution completed", map[string]interface{}{
		"job_execution_id": jobExecutionID,
	})

	return nil
}

// dispatchJobCompletedNotification sends a job completion notification via the dispatcher
func (s *JobExecutionService) dispatchJobCompletedNotification(ctx context.Context, jobExec *models.JobExecution) {
	dispatcher := GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping job completion notification")
		return
	}

	// Get hashlist info for the notification
	var hashlistName string
	var crackedCount, totalHashes int
	if jobExec.HashlistID > 0 {
		hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
		if err == nil && hashlist != nil {
			hashlistName = hashlist.Name
			crackedCount = hashlist.CrackedHashes
			totalHashes = hashlist.TotalHashes
		}
	}

	// Calculate success rate
	var successRate float64
	if totalHashes > 0 {
		successRate = float64(crackedCount) / float64(totalHashes) * 100
	}

	// Calculate duration
	var duration string
	if jobExec.StartedAt != nil && jobExec.CompletedAt != nil {
		d := jobExec.CompletedAt.Sub(*jobExec.StartedAt)
		duration = d.Round(time.Second).String()
	}

	params := models.NotificationDispatchParams{
		UserID:  *jobExec.CreatedBy,
		Type:    models.NotificationTypeJobCompleted,
		Title:   "Job Completed",
		Message: fmt.Sprintf("Job '%s' completed with %d hashes cracked (%.1f%%)", jobExec.Name, crackedCount, successRate),
		Data: map[string]interface{}{
			"job_id":           jobExec.ID.String(),
			"job_name":         jobExec.Name,
			"hashlist_name":    hashlistName,
			"duration":         duration,
			"cracked_count":    crackedCount,
			"total_hashes":     totalHashes,
			"hashes_processed": totalHashes, // Template uses {{ .HashesProcessed }}
			"success_rate":     fmt.Sprintf("%.1f", successRate), // No % - template adds it
		},
		SourceType: "job",
		SourceID:   jobExec.ID.String(),
	}

	if err := dispatcher.Dispatch(ctx, params); err != nil {
		debug.Error("Failed to dispatch job completion notification: %v", err)
	} else {
		debug.Log("Job completion notification dispatched", map[string]interface{}{
			"job_id":   jobExec.ID,
			"user_id":  jobExec.CreatedBy,
			"job_name": jobExec.Name,
		})
	}
}

// dispatchJobStartedNotification sends a job started notification via the dispatcher
func (s *JobExecutionService) dispatchJobStartedNotification(ctx context.Context, jobExec *models.JobExecution) {
	dispatcher := GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping job started notification")
		return
	}

	// Get hashlist info for the notification
	var hashlistName string
	var totalHashes int
	if jobExec.HashlistID > 0 {
		hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
		if err == nil && hashlist != nil {
			hashlistName = hashlist.Name
			totalHashes = hashlist.TotalHashes
		}
	}

	params := models.NotificationDispatchParams{
		UserID:  *jobExec.CreatedBy,
		Type:    models.NotificationTypeJobStarted,
		Title:   "Job Started",
		Message: fmt.Sprintf("Job '%s' has started processing", jobExec.Name),
		Data: map[string]interface{}{
			"job_id":        jobExec.ID.String(),
			"job_name":      jobExec.Name,
			"hashlist_name": hashlistName,
			"total_hashes":  totalHashes,
			"priority":      jobExec.Priority,
		},
		SourceType: "job",
		SourceID:   jobExec.ID.String(),
	}

	if err := dispatcher.Dispatch(ctx, params); err != nil {
		debug.Error("Failed to dispatch job started notification: %v", err)
	} else {
		debug.Log("Job started notification dispatched", map[string]interface{}{
			"job_id":   jobExec.ID,
			"user_id":  jobExec.CreatedBy,
			"job_name": jobExec.Name,
		})
	}
}

// dispatchJobFailedNotification sends a job failed notification via the dispatcher
func (s *JobExecutionService) dispatchJobFailedNotification(ctx context.Context, jobExec *models.JobExecution, errorMessage string) {
	dispatcher := GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping job failed notification")
		return
	}

	// Get hashlist info for the notification
	var hashlistName string
	if jobExec.HashlistID > 0 {
		hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
		if err == nil && hashlist != nil {
			hashlistName = hashlist.Name
		}
	}

	params := models.NotificationDispatchParams{
		UserID:  *jobExec.CreatedBy,
		Type:    models.NotificationTypeJobFailed,
		Title:   "Job Failed",
		Message: fmt.Sprintf("Job '%s' failed: %s", jobExec.Name, errorMessage),
		Data: map[string]interface{}{
			"job_id":        jobExec.ID.String(),
			"job_name":      jobExec.Name,
			"hashlist_name": hashlistName,
			"error_message": errorMessage,
			"failed_at":     time.Now().Format(time.RFC3339),
		},
		SourceType: "job",
		SourceID:   jobExec.ID.String(),
	}

	if err := dispatcher.Dispatch(ctx, params); err != nil {
		debug.Error("Failed to dispatch job failed notification: %v", err)
	} else {
		debug.Log("Job failed notification dispatched", map[string]interface{}{
			"job_id":        jobExec.ID,
			"user_id":       jobExec.CreatedBy,
			"job_name":      jobExec.Name,
			"error_message": errorMessage,
		})
	}
}

// UpdateTaskProgress updates the progress of a task accounting for rule splitting and keysplit tasks
func (s *JobExecutionService) UpdateTaskProgress(ctx context.Context, taskID uuid.UUID, keyspaceProcessed int64, effectiveProgress int64, hashRate *int64, progressPercent float64) error {
	// Get the task to check for keysplit
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task for progress update: %w", err)
	}

	// For keysplit tasks with EffectiveKeyspaceStart > 0, convert absolute to relative
	// Hashcat reports progress[0] as cumulative absolute position in the keyspace
	// For continuation tasks (keyspace_start > 0), we need to store the RELATIVE contribution
	// Example: Task 2 with effective_keyspace_start=492B reports progress[0]=735B at completion
	//          We should store 735B - 492B = 243B as the task's contribution
	effectiveKeyspaceProcessed := effectiveProgress
	if task.IsKeyspaceSplit && task.EffectiveKeyspaceStart != nil && *task.EffectiveKeyspaceStart > 0 {
		if effectiveProgress >= *task.EffectiveKeyspaceStart {
			effectiveKeyspaceProcessed = effectiveProgress - *task.EffectiveKeyspaceStart
			debug.Log("Converted absolute to relative effective keyspace for keysplit task", map[string]interface{}{
				"task_id":                   taskID,
				"effective_progress_raw":   effectiveProgress,
				"effective_keyspace_start": *task.EffectiveKeyspaceStart,
				"effective_keyspace_processed": effectiveKeyspaceProcessed,
			})
		}
		// If effectiveProgress < EffectiveKeyspaceStart, keep original (shouldn't happen but be safe)
	}

	// Update the task progress
	// Note: Job-level progress is now calculated by the polling service (JobProgressCalculationService)
	// which runs every 2 seconds and recalculates from task data
	err = s.jobTaskRepo.UpdateProgress(ctx, taskID, keyspaceProcessed, effectiveKeyspaceProcessed, hashRate, progressPercent)
	if err != nil {
		return fmt.Errorf("failed to update task progress: %w", err)
	}

	return nil
}

// UpdateRuleSplitting updates the rule splitting flag for a job execution
func (s *JobExecutionService) UpdateRuleSplitting(ctx context.Context, jobID uuid.UUID, usesRuleSplitting bool) error {
	job, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	job.UsesRuleSplitting = usesRuleSplitting
	return s.jobExecRepo.UpdateKeyspaceInfo(ctx, job)
}

// UpdateKeyspaceInfo updates the keyspace information for a job execution
func (s *JobExecutionService) UpdateKeyspaceInfo(ctx context.Context, job *models.JobExecution) error {
	return s.jobExecRepo.UpdateKeyspaceInfo(ctx, job)
}

// UpdateCrackedCount updates the total number of cracked hashes for a job execution
// DEPRECATED: This method is deprecated as cracked counts are now tracked at the hashlist level
func (s *JobExecutionService) UpdateCrackedCount(ctx context.Context, jobExecutionID uuid.UUID, additionalCracks int) error {
	// This method is deprecated and should not be used
	// Cracked counts are now tracked on the hashlists table, not job_executions
	debug.Log("WARNING: UpdateCrackedCount called on job execution service (deprecated)", map[string]interface{}{
		"job_id":            jobExecutionID,
		"additional_cracks": additionalCracks,
	})
	return nil
}

// CanInterruptJob checks if a job can be interrupted by a higher priority job
func (s *JobExecutionService) CanInterruptJob(ctx context.Context, newJobPriority int) ([]models.JobExecution, error) {
	// Check if job interruption is enabled
	interruptionSetting, err := s.systemSettingsRepo.GetSetting(ctx, "job_interruption_enabled")
	if err != nil {
		return nil, fmt.Errorf("failed to get interruption setting: %w", err)
	}

	if interruptionSetting.Value == nil || *interruptionSetting.Value != "true" {
		return []models.JobExecution{}, nil // Interruption disabled
	}

	// Get interruptible jobs with lower priority
	interruptibleJobs, err := s.jobExecRepo.GetInterruptibleJobs(ctx, newJobPriority)
	if err != nil {
		return nil, fmt.Errorf("failed to get interruptible jobs: %w", err)
	}

	return interruptibleJobs, nil
}

// InterruptJob interrupts a running job for a higher priority job
// If taskIDsToInterrupt is provided (non-empty), only those specific tasks will be interrupted.
// If taskIDsToInterrupt is nil or empty, ALL running tasks for the job will be interrupted.
// This uses SetTaskStopping to preserve agent_id until the agent acknowledges the stop.
func (s *JobExecutionService) InterruptJob(ctx context.Context, jobExecutionID, interruptingJobID uuid.UUID, taskIDsToInterrupt []uuid.UUID) error {
	err := s.jobExecRepo.InterruptExecution(ctx, jobExecutionID, interruptingJobID)
	if err != nil {
		return fmt.Errorf("failed to interrupt job: %w", err)
	}

	// Get tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get tasks for interrupted job: %w", err)
	}

	// Build a set of task IDs to interrupt for quick lookup
	interruptSet := make(map[uuid.UUID]bool)
	selectiveInterrupt := len(taskIDsToInterrupt) > 0
	for _, tid := range taskIDsToInterrupt {
		interruptSet[tid] = true
	}

	for _, task := range tasks {
		if task.Status == models.JobTaskStatusRunning || task.Status == models.JobTaskStatusAssigned {
			// If selective interrupt, only interrupt tasks in the list
			if selectiveInterrupt && !interruptSet[task.ID] {
				debug.Log("Skipping task not in interrupt list", map[string]interface{}{
					"task_id": task.ID,
					"job_id":  jobExecutionID,
				})
				continue
			}

			// Set task to stopping (keeps agent_id until stop ack is received)
			err = s.jobTaskRepo.SetTaskStopping(ctx, task.ID)
			if err != nil {
				debug.Log("Failed to set task to stopping", map[string]interface{}{
					"task_id": task.ID,
					"error":   err.Error(),
				})
			} else {
				debug.Log("Set task to stopping", map[string]interface{}{
					"task_id":  task.ID,
					"agent_id": task.AgentID,
				})
			}

			// Note: We do NOT clear agent busy status here anymore.
			// Agent busy status will be cleared in ClearStoppedTaskAgent after the agent
			// acknowledges the stop via task_stop_ack message.
		}
	}

	debug.Log("Job interrupted", map[string]interface{}{
		"job_execution_id":      jobExecutionID,
		"interrupting_job_id":   interruptingJobID,
		"selective_interrupt":   selectiveInterrupt,
		"tasks_to_interrupt":    len(taskIDsToInterrupt),
	})

	return nil
}

// GetSystemSetting retrieves a system setting by key (public method for integration)
func (s *JobExecutionService) GetSystemSetting(ctx context.Context, key string) (int, error) {
	setting, err := s.systemSettingsRepo.GetSetting(ctx, key)
	if err != nil {
		return 0, err
	}

	if setting.Value == nil {
		return 0, fmt.Errorf("setting value is null")
	}

	value, err := strconv.Atoi(*setting.Value)
	if err != nil {
		return 0, fmt.Errorf("invalid setting value: %w", err)
	}

	return value, nil
}

// GetJobExecutionByID retrieves a job execution by ID (public method for integration)
func (s *JobExecutionService) GetJobExecutionByID(ctx context.Context, id uuid.UUID) (*models.JobExecution, error) {
	return s.jobExecRepo.GetByID(ctx, id)
}

// RetryFailedChunk attempts to retry a failed job task chunk
func (s *JobExecutionService) RetryFailedChunk(ctx context.Context, taskID uuid.UUID) error {
	debug.Log("Attempting to retry failed chunk", map[string]interface{}{
		"task_id": taskID,
	})

	// Get the current task
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Get max retry attempts from system settings
	maxRetryAttempts, err := s.GetSystemSetting(ctx, "max_chunk_retry_attempts")
	if err != nil {
		debug.Log("Failed to get max retry attempts, using default", map[string]interface{}{
			"error": err.Error(),
		})
		maxRetryAttempts = 3 // Default fallback
	}

	// Check if we can retry
	if task.RetryCount >= maxRetryAttempts {
		debug.Log("Maximum retry attempts reached", map[string]interface{}{
			"task_id":     taskID,
			"retry_count": task.RetryCount,
			"max_retries": maxRetryAttempts,
		})

		// Mark as permanently failed
		err = s.jobTaskRepo.UpdateTaskStatus(ctx, taskID, "failed", "failed")
		if err != nil {
			return fmt.Errorf("failed to mark task as permanently failed: %w", err)
		}

		return fmt.Errorf("maximum retry attempts (%d) exceeded for task %s", maxRetryAttempts, taskID)
	}

	// Reset task for retry
	err = s.jobTaskRepo.ResetTaskForRetry(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to reset task for retry: %w", err)
	}

	debug.Log("Chunk reset for retry", map[string]interface{}{
		"task_id":     taskID,
		"retry_count": task.RetryCount + 1,
	})

	return nil
}

// ProcessFailedChunks automatically retries failed chunks based on system settings
func (s *JobExecutionService) ProcessFailedChunks(ctx context.Context, jobExecutionID uuid.UUID) error {
	debug.Log("Processing failed chunks for job", map[string]interface{}{
		"job_execution_id": jobExecutionID,
	})

	// Get all failed tasks for this job execution
	failedTasks, err := s.jobTaskRepo.GetFailedTasksByJobExecution(ctx, jobExecutionID)
	if err != nil {
		return fmt.Errorf("failed to get failed tasks: %w", err)
	}

	retriedCount := 0
	permanentFailureCount := 0

	for _, task := range failedTasks {
		err := s.RetryFailedChunk(ctx, task.ID)
		if err != nil {
			debug.Log("Failed to retry chunk", map[string]interface{}{
				"task_id": task.ID,
				"error":   err.Error(),
			})
			permanentFailureCount++
		} else {
			retriedCount++
		}
	}

	debug.Log("Completed failed chunk processing", map[string]interface{}{
		"job_execution_id":        jobExecutionID,
		"retried_count":           retriedCount,
		"permanent_failure_count": permanentFailureCount,
		"total_failed_tasks":      len(failedTasks),
	})

	return nil
}

// UpdateChunkStatusWithCracks updates a chunk's status and crack count
func (s *JobExecutionService) UpdateChunkStatusWithCracks(ctx context.Context, taskID uuid.UUID, crackCount int, detailedStatus string) error {
	debug.Log("Updating chunk status with crack information", map[string]interface{}{
		"task_id":         taskID,
		"crack_count":     crackCount,
		"detailed_status": detailedStatus,
	})

	err := s.jobTaskRepo.UpdateTaskWithCracks(ctx, taskID, crackCount, detailedStatus)
	if err != nil {
		return fmt.Errorf("failed to update task with cracks: %w", err)
	}

	return nil
}

// GetDynamicChunkSize calculates optimal chunk size based on agent benchmark data
func (s *JobExecutionService) GetDynamicChunkSize(ctx context.Context, agentID int, attackMode int, hashType int, defaultDurationSeconds int) (int64, error) {
	debug.Log("Calculating dynamic chunk size", map[string]interface{}{
		"agent_id":         agentID,
		"attack_mode":      attackMode,
		"hash_type":        hashType,
		"default_duration": defaultDurationSeconds,
	})

	// Get agent benchmark for this specific attack mode and hash type
	// Note: saltCount is nil here as this function doesn't have job context
	// For salt-aware lookups, use the chunk planning service which has full context
	benchmark, err := s.benchmarkRepo.GetAgentBenchmark(ctx, agentID, models.AttackMode(attackMode), hashType, nil)
	if err != nil {
		debug.Log("No benchmark found, using default chunk size", map[string]interface{}{
			"agent_id":    agentID,
			"attack_mode": attackMode,
			"hash_type":   hashType,
			"error":       err.Error(),
		})
		// Return a default chunk size (e.g., 1M keyspace)
		return 1000000, nil
	}

	// Calculate keyspace size for the default duration
	// keyspace = benchmark_speed * duration_seconds
	keyspaceSize := benchmark.Speed * int64(defaultDurationSeconds)

	debug.Log("Dynamic chunk size calculated", map[string]interface{}{
		"agent_id":        agentID,
		"benchmark_speed": benchmark.Speed,
		"duration":        defaultDurationSeconds,
		"keyspace_size":   keyspaceSize,
	})

	return keyspaceSize, nil
}

// resolveWordlistPath gets the actual file path for a wordlist ID
func (s *JobExecutionService) resolveWordlistPath(ctx context.Context, wordlistIDStr string) (string, error) {
	// Try to parse as integer ID first
	if wordlistID, err := strconv.Atoi(wordlistIDStr); err == nil {
		// Look up wordlist in database
		wordlists, err := s.fileRepo.GetWordlists(ctx, "")
		if err != nil {
			return "", fmt.Errorf("failed to get wordlists: %w", err)
		}

		for _, wl := range wordlists {
			if wl.ID == wordlistID {
				// The Name field already contains the relative path from wordlists directory
				// e.g., "general/crackstation.txt"
				path := filepath.Join(s.dataDirectory, "wordlists", wl.Name)

				debug.Log("Resolved wordlist path", map[string]interface{}{
					"wordlist_id": wordlistID,
					"category":    wl.Category,
					"name_field":  wl.Name,
					"path":        path,
				})
				return path, nil
			}
		}
		return "", fmt.Errorf("wordlist with ID %d not found", wordlistID)
	}

	// If not a numeric ID, treat as a filename
	path := filepath.Join(s.dataDirectory, "wordlists", wordlistIDStr)
	debug.Log("Resolved wordlist path from string", map[string]interface{}{
		"wordlist_str": wordlistIDStr,
		"path":         path,
	})
	return path, nil
}

// resolveRulePath gets the actual file path for a rule ID
func (s *JobExecutionService) resolveRulePath(ctx context.Context, ruleIDStr string) (string, error) {
	debug.Log("Resolving rule path", map[string]interface{}{
		"rule_id_str":    ruleIDStr,
		"data_directory": s.dataDirectory,
	})

	// Try to parse as integer ID first
	if ruleID, err := strconv.Atoi(ruleIDStr); err == nil {
		// Look up rule in database
		rules, err := s.fileRepo.GetRules(ctx, "")
		if err != nil {
			return "", fmt.Errorf("failed to get rules: %w", err)
		}

		debug.Log("Looking for rule in database", map[string]interface{}{
			"rule_id":     ruleID,
			"total_rules": len(rules),
		})

		for _, rule := range rules {
			if rule.ID == ruleID {
				// The Name field already contains the relative path from rules directory
				// e.g., "hashcat/_nsakey.v2.dive.rule"
				path := filepath.Join(s.dataDirectory, "rules", rule.Name)

				debug.Log("Resolved rule path", map[string]interface{}{
					"rule_id":     ruleID,
					"category":    rule.Category,
					"name_field":  rule.Name,
					"path":        path,
					"file_exists": fileExists(path),
				})
				return path, nil
			}
		}
		return "", fmt.Errorf("rule with ID %d not found", ruleID)
	}

	// If not a numeric ID, treat as a filename
	path := filepath.Join(s.dataDirectory, "rules", ruleIDStr)
	debug.Log("Resolved rule path from string", map[string]interface{}{
		"rule_str": ruleIDStr,
		"path":     path,
	})
	return path, nil
}

// resolveAssociationWordlistPath resolves an association wordlist ID string to its file path
func (s *JobExecutionService) resolveAssociationWordlistPath(ctx context.Context, assocWordlistIDStr string) (string, error) {
	assocWordlistID, err := uuid.Parse(assocWordlistIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid association wordlist ID: %w", err)
	}
	filePath, err := s.assocWordlistRepo.GetFilePath(ctx, assocWordlistID)
	if err != nil {
		return "", fmt.Errorf("failed to get association wordlist path: %w", err)
	}
	return filePath, nil
}

// getAssociationWordlistLineCount retrieves the line count for an association wordlist
func (s *JobExecutionService) getAssociationWordlistLineCount(ctx context.Context, assocWordlistIDStr string) (int64, error) {
	assocWordlistID, err := uuid.Parse(assocWordlistIDStr)
	if err != nil {
		return 0, fmt.Errorf("invalid association wordlist ID: %w", err)
	}
	wordlist, err := s.assocWordlistRepo.GetByID(ctx, assocWordlistID)
	if err != nil {
		return 0, fmt.Errorf("failed to get association wordlist: %w", err)
	}
	return wordlist.LineCount, nil
}

// GetTotalRuleCount retrieves the sum of rule counts for a list of rule IDs
func (s *JobExecutionService) GetTotalRuleCount(ctx context.Context, ruleIDs []string) (int64, error) {
	if len(ruleIDs) == 0 {
		return 1, nil // No rules = multiplier of 1
	}

	var totalCount int64 = 0
	for _, ruleIDStr := range ruleIDs {
		ruleID, err := strconv.Atoi(ruleIDStr)
		if err != nil {
			continue
		}
		var ruleCount int64
		err = s.db.QueryRowContext(ctx,
			"SELECT rule_count FROM rules WHERE id = $1", ruleID).Scan(&ruleCount)
		if err != nil {
			continue
		}
		totalCount += ruleCount
	}
	if totalCount == 0 {
		return 1, nil
	}
	return totalCount, nil
}

// RuleSplitDecision contains the decision information for rule splitting
type RuleSplitDecision struct {
	ShouldSplit     bool
	NumSplits       int
	RuleFileToSplit string
	RulesPerChunk   int
	TotalRules      int
}

// analyzeForRuleSplitting determines if rule splitting should be used for a job
func (s *JobExecutionService) analyzeForRuleSplitting(ctx context.Context, job *models.JobExecution, presetJob *models.PresetJob, benchmarkSpeed float64) (*RuleSplitDecision, error) {
	// Check if rule splitting is enabled
	ruleSplitEnabled, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_enabled")
	if err != nil || ruleSplitEnabled.Value == nil || *ruleSplitEnabled.Value != "true" {
		return &RuleSplitDecision{ShouldSplit: false}, nil
	}

	// Only applicable for attacks 0 and 9 with rules
	if job.AttackMode != models.AttackModeStraight && job.AttackMode != models.AttackModeAssociation {
		return &RuleSplitDecision{ShouldSplit: false}, nil
	}

	if job.MultiplicationFactor <= 1 {
		return &RuleSplitDecision{ShouldSplit: false}, nil
	}

	// For both attack modes 0 (straight) and 9 (association), check thresholds
	// Association attacks use the same threshold-based logic to respect system settings
	thresholdSetting, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_threshold")
	if err != nil {
		debug.Log("Failed to get rule split threshold, using default", map[string]interface{}{
			"error": err.Error(),
		})
	}
	threshold := 2.0 // Default
	if thresholdSetting != nil && thresholdSetting.Value != nil {
		if parsed, parseErr := strconv.ParseFloat(*thresholdSetting.Value, 64); parseErr == nil {
			threshold = parsed
		}
	}

	minRulesSetting, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_min_rules")
	if err != nil {
		debug.Log("Failed to get min rules setting, using default", map[string]interface{}{
			"error": err.Error(),
		})
	}
	minRules := 100 // Default
	if minRulesSetting != nil && minRulesSetting.Value != nil {
		if parsed, parseErr := strconv.Atoi(*minRulesSetting.Value); parseErr == nil {
			minRules = parsed
		}
	}

	// Calculate estimated time
	effectiveKeyspace := job.EffectiveKeyspace
	if effectiveKeyspace == nil {
		// Can't make rule split decision without effective keyspace
		return &RuleSplitDecision{ShouldSplit: false}, nil
	}

	estimatedTimeSeconds := float64(*effectiveKeyspace) / benchmarkSpeed

	chunkDurationSetting, err := s.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration")
	if err != nil {
		debug.Log("Failed to get chunk duration, using default", map[string]interface{}{
			"error": err.Error(),
		})
	}
	chunkDuration := 1200.0 // Default 20 minutes
	if chunkDurationSetting != nil && chunkDurationSetting.Value != nil {
		if parsed, parseErr := strconv.ParseFloat(*chunkDurationSetting.Value, 64); parseErr == nil {
			chunkDuration = parsed
		}
	}

	// Get actual rule count (not salt-adjusted) for minRules comparison
	actualRuleCount, ruleErr := s.GetTotalRuleCount(ctx, presetJob.RuleIDs)
	if ruleErr != nil {
		actualRuleCount = int64(job.MultiplicationFactor) // Fallback to multiplicationFactor
	}

	debug.Log("Analyzing for rule splitting", map[string]interface{}{
		"job_id":                job.ID,
		"attack_mode":           job.AttackMode,
		"actual_rule_count":     actualRuleCount,
		"multiplication_factor": job.MultiplicationFactor,
		"estimated_time":        estimatedTimeSeconds,
		"chunk_duration":        chunkDuration,
		"threshold":             threshold,
		"min_rules":             minRules,
	})

	if estimatedTimeSeconds > chunkDuration*threshold && int(actualRuleCount) >= minRules {
		return s.createSplitDecision(ctx, job, presetJob, benchmarkSpeed)
	}

	return &RuleSplitDecision{ShouldSplit: false}, nil
}

// createSplitDecision creates a rule split decision for a job
func (s *JobExecutionService) createSplitDecision(ctx context.Context, job *models.JobExecution, presetJob *models.PresetJob, benchmarkSpeed float64) (*RuleSplitDecision, error) {
	// Get rule files
	ruleFiles, err := s.extractRuleFiles(ctx, presetJob)
	if err != nil {
		return nil, fmt.Errorf("failed to extract rule files: %w", err)
	}

	if len(ruleFiles) == 0 {
		return &RuleSplitDecision{ShouldSplit: false}, nil
	}

	// For simplicity, we'll split the first rule file
	// In a more advanced implementation, we might split multiple files
	ruleFileToSplit := ruleFiles[0]

	// Count rules in the file
	totalRules, err := s.ruleSplitManager.CountRules(ctx, ruleFileToSplit)
	if err != nil {
		return nil, fmt.Errorf("failed to count rules: %w", err)
	}

	// Get max chunks setting
	maxChunksSetting, err := s.systemSettingsRepo.GetSetting(ctx, "rule_split_max_chunks")
	if err != nil {
		debug.Log("Failed to get max chunks setting, using default", map[string]interface{}{
			"error": err.Error(),
		})
	}
	maxChunks := 1000 // Default
	if maxChunksSetting != nil && maxChunksSetting.Value != nil {
		if parsed, parseErr := strconv.Atoi(*maxChunksSetting.Value); parseErr == nil {
			maxChunks = parsed
		}
	}

	// Calculate optimal number of splits
	chunkDurationSetting, err := s.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration")
	if err != nil {
		debug.Log("Failed to get chunk duration, using default", map[string]interface{}{
			"error": err.Error(),
		})
	}
	chunkDuration := 1200.0 // Default 20 minutes in seconds
	if chunkDurationSetting != nil && chunkDurationSetting.Value != nil {
		if parsed, parseErr := strconv.ParseFloat(*chunkDurationSetting.Value, 64); parseErr == nil {
			chunkDuration = parsed
		}
	}

	// Calculate how many rules we can process in chunk duration
	var baseKeyspace int64
	if job.BaseKeyspace != nil {
		baseKeyspace = *job.BaseKeyspace
	} else if job.EffectiveKeyspace != nil {
		baseKeyspace = *job.EffectiveKeyspace
	} else {
		baseKeyspace = 1000000 // Default fallback
	}

	// Rules we can process in chunk duration = (benchmark_speed * chunk_duration) / base_keyspace
	rulesPerChunkIdeal := int((benchmarkSpeed * chunkDuration) / float64(baseKeyspace))
	if rulesPerChunkIdeal < 1 {
		rulesPerChunkIdeal = 1
	}

	// Calculate number of splits needed
	numSplits := (totalRules + rulesPerChunkIdeal - 1) / rulesPerChunkIdeal
	if numSplits > maxChunks {
		numSplits = maxChunks
	}
	if numSplits < 1 {
		numSplits = 1
	}

	rulesPerChunk := (totalRules + numSplits - 1) / numSplits

	debug.Log("Created split decision", map[string]interface{}{
		"job_id":                job.ID,
		"rule_file":             ruleFileToSplit,
		"total_rules":           totalRules,
		"num_splits":            numSplits,
		"rules_per_chunk":       rulesPerChunk,
		"rules_per_chunk_ideal": rulesPerChunkIdeal,
		"base_keyspace":         baseKeyspace,
		"benchmark_speed":       benchmarkSpeed,
	})

	return &RuleSplitDecision{
		ShouldSplit:     true,
		NumSplits:       numSplits,
		RuleFileToSplit: ruleFileToSplit,
		RulesPerChunk:   rulesPerChunk,
		TotalRules:      totalRules,
	}, nil
}

// createJobTasksWithRuleSplitting creates job tasks with rule splitting if needed
func (s *JobExecutionService) createJobTasksWithRuleSplitting(ctx context.Context, job *models.JobExecution, presetJob *models.PresetJob, decision *RuleSplitDecision) error {
	if !decision.ShouldSplit {
		// Standard single task creation - this will be handled by JobChunkingService
		return nil
	}

	// Update job metadata to indicate rule splitting will be used
	job.UsesRuleSplitting = true
	job.RuleSplitCount = decision.TotalRules // Store total rules for progress tracking
	if err := s.jobExecRepo.UpdateKeyspaceInfo(ctx, job); err != nil {
		return fmt.Errorf("failed to update job metadata: %w", err)
	}

	debug.Log("Enabled rule splitting for job", map[string]interface{}{
		"job_id":          job.ID,
		"total_rules":     decision.TotalRules,
		"rule_file":       decision.RuleFileToSplit,
		"uses_splitting":  true,
	})

	// Tasks will be created dynamically by the scheduler as agents become available
	// No pre-chunking needed!
	return nil
}

// buildAttackCommand builds the hashcat attack command from a job execution
// Job executions are self-contained and no longer require preset lookups
// The presetJob parameter is deprecated and should always be nil
// layerMask can be provided for increment layer tasks - if set, it overrides job.Mask and skips increment flags
func (s *JobExecutionService) buildAttackCommand(ctx context.Context, presetJob *models.PresetJob, job *models.JobExecution, layerMask string) (string, error) {
	// Resolve binary version pattern to actual binary ID
	if job.BinaryVersion == "" {
		return "", fmt.Errorf("no binary version pattern available in job execution")
	}

	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, job.BinaryVersion)
	if err != nil {
		return "", fmt.Errorf("failed to resolve binary version pattern %q: %w", job.BinaryVersion, err)
	}

	// Get the hashcat binary path
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		return "", fmt.Errorf("failed to get hashcat binary path: %w", err)
	}

	// Construct the hashlist path where agent will store it locally
	// Agents download hashlists and store them as: data/hashlists/<hashlist_id>.hash
	hashlistPath := filepath.Join("hashlists", fmt.Sprintf("%d.hash", job.HashlistID))

	// Build the command
	var args []string

	// Use attack mode directly from job (0 is valid for AttackModeStraight)
	attackMode := job.AttackMode

	// Use hash type directly from job (job_executions are self-contained)
	hashType := job.HashType

	// Attack mode
	args = append(args, "-a", strconv.Itoa(int(attackMode)))

	// Hash type
	args = append(args, "-m", strconv.Itoa(hashType))

	// Hashlist
	args = append(args, hashlistPath)

	// Use wordlist and rule IDs directly from job (job_executions are self-contained)
	wordlistIDs := job.WordlistIDs
	ruleIDs := job.RuleIDs

	// Attack-specific arguments
	switch attackMode {
	case models.AttackModeStraight, models.AttackModeAssociation:
		// Add wordlists
		for _, wordlistIDStr := range wordlistIDs {
			wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDStr)
			if err != nil {
				return "", fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath)
		}
		// Add rules
		for _, ruleIDStr := range ruleIDs {
			rulePath, err := s.resolveRulePath(ctx, ruleIDStr)
			if err != nil {
				return "", fmt.Errorf("failed to resolve rule path: %w", err)
			}
			args = append(args, "-r", rulePath)
		}

	case models.AttackModeCombination:
		// Add two wordlists
		if len(wordlistIDs) >= 2 {
			wordlist1Path, err := s.resolveWordlistPath(ctx, wordlistIDs[0])
			if err != nil {
				return "", fmt.Errorf("failed to resolve wordlist1 path: %w", err)
			}
			wordlist2Path, err := s.resolveWordlistPath(ctx, wordlistIDs[1])
			if err != nil {
				return "", fmt.Errorf("failed to resolve wordlist2 path: %w", err)
			}
			args = append(args, wordlist1Path, wordlist2Path)
		}

	case models.AttackModeBruteForce:
		// Add mask - use layerMask if provided (for increment layers), otherwise use job.Mask
		mask := job.Mask
		if layerMask != "" {
			mask = layerMask
		}
		if mask != "" {
			args = append(args, mask)
		}

	case models.AttackModeHybridWordlistMask:
		// Add wordlist and mask - use layerMask if provided (for increment layers), otherwise use job.Mask
		mask := job.Mask
		if layerMask != "" {
			mask = layerMask
		}
		if len(wordlistIDs) > 0 && mask != "" {
			wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDs[0])
			if err != nil {
				return "", fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath, mask)
		}

	case models.AttackModeHybridMaskWordlist:
		// Add mask and wordlist - use layerMask if provided (for increment layers), otherwise use job.Mask
		mask := job.Mask
		if layerMask != "" {
			mask = layerMask
		}
		if mask != "" && len(wordlistIDs) > 0 {
			wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDs[0])
			if err != nil {
				return "", fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, mask, wordlistPath)
		}
	}

	// Add any additional arguments from job (job_executions are self-contained)
	if job.AdditionalArgs != nil && *job.AdditionalArgs != "" {
		additionalArgs := strings.Fields(*job.AdditionalArgs)
		args = append(args, additionalArgs...)
	}

	// Add increment flags for mask-based attacks
	// SKIP increment flags when layerMask is provided - the backend handles layer distribution
	if layerMask == "" && (job.IncrementMode == "increment" || job.IncrementMode == "increment_inverse") {
		if job.IncrementMode == "increment" {
			args = append(args, "--increment")
		} else if job.IncrementMode == "increment_inverse" {
			args = append(args, "--increment-inverse")
		}

		if job.IncrementMin != nil {
			args = append(args, "--increment-min", strconv.Itoa(*job.IncrementMin))
		}

		if job.IncrementMax != nil {
			args = append(args, "--increment-max", strconv.Itoa(*job.IncrementMax))
		}
	}

	// Join command
	fullCmd := hashcatPath + " " + strings.Join(args, " ")
	return fullCmd, nil
}

// cleanupTaskResources cleans up resources associated with a completed or failed task
func (s *JobExecutionService) cleanupTaskResources(ctx context.Context, task *models.JobTask) error {
	if !task.IsRuleSplitTask || task.RuleChunkPath == nil || *task.RuleChunkPath == "" {
		return nil
	}

	debug.Log("Cleaning up task resources", map[string]interface{}{
		"task_id":         task.ID,
		"rule_chunk_path": *task.RuleChunkPath,
	})

	// Remove rule chunk file from server
	if err := os.Remove(*task.RuleChunkPath); err != nil && !os.IsNotExist(err) {
		debug.Error("Failed to remove rule chunk file: %v", err)
		// Don't return error - continue with cleanup
	}

	// TODO: Send cleanup message to agent via WebSocket to remove the chunk file

	return nil
}

// CleanupJobResources cleans up all resources for a completed/failed/cancelled job
func (s *JobExecutionService) CleanupJobResources(ctx context.Context, jobID uuid.UUID) error {
	debug.Log("Cleaning up job resources", map[string]interface{}{
		"job_id": jobID,
	})

	// Get job execution
	job, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	// If this job uses rule splitting, clean up all chunks
	if job.UsesRuleSplitting {
		// Use the new UUID-based cleanup method
		err = s.ruleSplitManager.CleanupJobChunksUUID(jobID)
		if err != nil {
			debug.Error("Failed to cleanup rule chunks for job: %v", err)
			// Don't return error - continue with other cleanup
		}
	}

	// Get all tasks for this job
	tasks, err := s.jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		debug.Error("Failed to get tasks for cleanup: %v", err)
		return nil // Don't fail the entire cleanup
	}

	// Cleanup each task's resources
	for _, task := range tasks {
		if err := s.cleanupTaskResources(ctx, &task); err != nil {
			debug.Error("Failed to cleanup task resources: %v", err)
			// Continue with other tasks
		}
	}

	return nil
}

// HandleTaskCompletion handles cleanup when a task completes (success or failure)
func (s *JobExecutionService) HandleTaskCompletion(ctx context.Context, taskID uuid.UUID) error {
	debug.Log("HandleTaskCompletion called", map[string]interface{}{
		"task_id": taskID,
	})

	// Get task
	task, err := s.jobTaskRepo.GetByID(ctx, taskID)
	if err != nil {
		debug.Error("Failed to get task in HandleTaskCompletion: %v", err)
		return fmt.Errorf("failed to get task: %w", err)
	}

	debug.Log("Retrieved task for completion handling", map[string]interface{}{
		"task_id":             taskID,
		"increment_layer_id":  task.IncrementLayerID,
		"status":              task.Status,
		"job_execution_id":    task.JobExecutionID,
	})

	// Cleanup task resources
	if err := s.cleanupTaskResources(ctx, task); err != nil {
		debug.Error("Failed to cleanup task resources on completion: %v", err)
		// Don't fail the task completion
	}

	// If this task belongs to an increment layer, check if layer is complete
	if task.IncrementLayerID != nil {
		debug.Log("Task belongs to increment layer, checking completion status", map[string]interface{}{
			"task_id":  taskID,
			"layer_id": task.IncrementLayerID,
		})

		allLayerTasksComplete, err := s.jobTaskRepo.AreAllLayerTasksComplete(ctx, *task.IncrementLayerID)
		if err != nil {
			debug.Error("Failed to check if all layer tasks complete: %v", err)
		} else {
			debug.Log("Layer tasks completion check result", map[string]interface{}{
				"layer_id":     task.IncrementLayerID,
				"all_complete": allLayerTasksComplete,
			})

			if allLayerTasksComplete {
				// CRITICAL: Check for failed layer tasks - don't mark layer as completed if any tasks failed
				hasFailedLayerTasks, failErr := s.jobTaskRepo.HasFailedLayerTasks(ctx, *task.IncrementLayerID)
				if failErr != nil {
					debug.Error("Failed to check for failed layer tasks: %v", failErr)
				} else if hasFailedLayerTasks {
					debug.Warning("Layer %s has failed tasks - marking as failed, not completed", *task.IncrementLayerID)
					if updateErr := s.jobIncrementLayerRepo.UpdateStatus(ctx, *task.IncrementLayerID, models.JobIncrementLayerStatusFailed); updateErr != nil {
						debug.Error("Failed to mark layer as failed: %v", updateErr)
					}
					// Continue to check job completion (which will mark job as failed)
				} else {
					// Safety check: ensure all base keyspace is covered before marking layer complete
					// This prevents premature completion when keyspace splitting is in use
					layer, layerErr := s.jobIncrementLayerRepo.GetByID(ctx, *task.IncrementLayerID)
					if layerErr != nil {
						debug.Error("Failed to get layer for completion check: %v", layerErr)
					} else if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 {
						maxBaseDispatched, maxErr := s.jobTaskRepo.GetMaxKeyspaceEndByLayer(ctx, *task.IncrementLayerID)
						if maxErr != nil {
							debug.Error("Failed to get max keyspace end for layer: %v", maxErr)
						} else if maxBaseDispatched < *layer.BaseKeyspace {
							// NOT all keyspace covered - keep layer running so scheduler creates more tasks
							debug.Log("Layer has remaining work - not marking complete", map[string]interface{}{
								"layer_id":         *task.IncrementLayerID,
								"base_keyspace":    *layer.BaseKeyspace,
								"max_keyspace_end": maxBaseDispatched,
								"remaining":        *layer.BaseKeyspace - maxBaseDispatched,
							})
							return nil // Don't mark as completed - scheduler will create more tasks
						}
					}

					debug.Log("All layer tasks complete and keyspace covered, marking layer as completed", map[string]interface{}{
						"layer_id": task.IncrementLayerID,
					})

					// Mark layer as completed
					err = s.jobIncrementLayerRepo.UpdateStatus(ctx, *task.IncrementLayerID, models.JobIncrementLayerStatusCompleted)
					if err != nil {
						debug.Error("Failed to mark layer as completed: %v", err)
					} else {
						debug.Log("Layer marked as completed", map[string]interface{}{
							"layer_id": task.IncrementLayerID,
							"job_id":   task.JobExecutionID,
						})
					}
				}
			}
		}
	} else {
		debug.Log("Task does not belong to increment layer", map[string]interface{}{
			"task_id": taskID,
		})
	}

	// Check if all tasks for this job are complete
	allTasksComplete, err := s.jobTaskRepo.AreAllTasksComplete(ctx, task.JobExecutionID)
	if err != nil {
		debug.Error("Failed to check if all tasks complete: %v", err)
		return nil
	}

	if allTasksComplete {
		// Get job to check current status
		job, err := s.jobExecRepo.GetByID(ctx, task.JobExecutionID)
		if err != nil {
			debug.Error("Failed to get job for completion check: %v", err)
			return nil
		}

		// Don't overwrite failed/cancelled status with completed
		// This preserves the failed status set when a task fails
		if job.Status == models.JobExecutionStatusFailed || job.Status == models.JobExecutionStatusCancelled {
			debug.Log("Job already in terminal state - skipping completion", map[string]interface{}{
				"job_id": task.JobExecutionID,
				"status": job.Status,
			})
			// Still run cleanup for resources
			if err := s.CleanupJobResources(ctx, task.JobExecutionID); err != nil {
				debug.Error("Failed to cleanup job resources: %v", err)
			}
			return nil
		}

		// For increment jobs, check if more layers need processing before marking complete
		if job.IncrementMode != "" && job.IncrementMode != "off" {
			// Check if there are pending increment layers
			hasPendingLayers, err := s.jobIncrementLayerRepo.HasPendingLayers(ctx, task.JobExecutionID)
			if err != nil {
				debug.Error("Failed to check for pending increment layers: %v", err)
			} else if hasPendingLayers {
				debug.Log("Not completing job - pending increment layers exist", map[string]interface{}{
					"job_id": task.JobExecutionID,
				})
				return nil // Don't mark complete yet, more layers to process
			}
		}

		// Mark job as completed (only if not failed/cancelled and no pending layers)
		// Use CompleteJobExecution which checks for failed tasks and marks job as failed if any
		if err := s.CompleteJobExecution(ctx, task.JobExecutionID); err != nil {
			debug.Error("Failed to complete job execution: %v", err)
			// Don't fail the task completion, but log the error
		} else {
			debug.Log("Job completion processed", map[string]interface{}{
				"job_id": task.JobExecutionID,
			})
		}

		// Cleanup job-level resources
		if err := s.CleanupJobResources(ctx, task.JobExecutionID); err != nil {
			debug.Error("Failed to cleanup job resources: %v", err)
		}
	}

	return nil
}

// buildRuleSplitAttackCommand builds the hashcat command for a rule split task
func (s *JobExecutionService) buildRuleSplitAttackCommand(ctx context.Context, job *models.JobExecution, task *models.JobTask) (string, error) {
	// Get the preset job (if this job was created from a preset)
	if job.PresetJobID == nil {
		return "", fmt.Errorf("job was not created from a preset job")
	}
	presetJob, err := s.presetJobRepo.GetByID(ctx, *job.PresetJobID)
	if err != nil {
		return "", fmt.Errorf("failed to get preset job: %w", err)
	}

	// Get hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		return "", fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Build base command
	var cmdParts []string
	cmdParts = append(cmdParts, fmt.Sprintf("-m %d", hashlist.HashTypeID))
	cmdParts = append(cmdParts, "-a 0") // Attack mode 0

	// Add wordlists
	for _, wordlistIDStr := range presetJob.WordlistIDs {
		wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDStr)
		if err != nil {
			return "", fmt.Errorf("failed to resolve wordlist path: %w", err)
		}
		cmdParts = append(cmdParts, wordlistPath)
	}

	// Add the rule chunk file
	if task.RuleChunkPath != nil {
		cmdParts = append(cmdParts, "-r", *task.RuleChunkPath)
	}

	// Add limit to match the base keyspace
	if job.BaseKeyspace != nil {
		cmdParts = append(cmdParts, "--limit", fmt.Sprintf("%d", *job.BaseKeyspace))
	}

	return strings.Join(cmdParts, " "), nil
}

// parseIntValueFromString safely parses an integer value with error handling
func parseIntValueFromString(value string) (int, error) {
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

// GetPreviousChunksActualKeyspace returns the cumulative actual keyspace size from all previous chunks
// This is used when creating new chunks to ensure correct starting positions
func (s *JobExecutionService) GetPreviousChunksActualKeyspace(ctx context.Context, jobExecutionID uuid.UUID, currentChunkNumber int) (int64, error) {
	return s.jobTaskRepo.GetPreviousChunksActualKeyspace(ctx, jobExecutionID, currentChunkNumber)
}

// GetAgentPreferredBinary returns the preferred binary version for an agent
// This is used for device detection and other agent-level operations that don't have a specific job context
func (s *JobExecutionService) GetAgentPreferredBinary(ctx context.Context, agentID int) (int64, error) {
	// Check agent-level override first
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to get agent: %w", err)
	}

	// Resolve agent's binary version pattern to actual binary ID
	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, agent.BinaryVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve agent binary version pattern %q: %w", agent.BinaryVersion, err)
	}

	debug.Log("Resolved agent binary version for device detection", map[string]interface{}{
		"agent_id":          agentID,
		"binary_pattern":    agent.BinaryVersion,
		"binary_version_id": binaryVersionID,
	})
	return binaryVersionID, nil
}

// DetermineBinaryForTask implements the binary selection logic using version patterns.
// It uses the version resolver to find a binary compatible with both agent and job patterns.
// Returns the binary version ID to use for a given agent and job execution.
func (s *JobExecutionService) DetermineBinaryForTask(ctx context.Context, agentID int, jobExecutionID uuid.UUID) (int64, error) {
	// Get agent info
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to get agent: %w", err)
	}

	// Get job execution info
	jobExecution, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get job execution: %w", err)
	}

	// Parse agent and job patterns
	agentPattern := agent.BinaryVersion
	if agentPattern == "" {
		agentPattern = "default"
	}

	jobPattern := jobExecution.BinaryVersion
	if jobPattern == "" {
		jobPattern = "default"
	}

	// Create adapter and resolver
	adapter := &binaryStoreAdapter{manager: s.binaryManager}
	resolver := version.NewResolver(adapter)

	// Use the resolver to find a compatible binary
	binaryID, err := resolver.ResolveForTaskStr(ctx, agentPattern, jobPattern)
	if err != nil {
		return 0, fmt.Errorf("no compatible binary for agent pattern %q and job pattern %q: %w", agentPattern, jobPattern, err)
	}

	debug.Log("Resolved binary for task", map[string]interface{}{
		"agent_id":         agentID,
		"job_execution_id": jobExecutionID,
		"agent_pattern":    agentPattern,
		"job_pattern":      jobPattern,
		"binary_id":        binaryID,
	})

	return binaryID, nil
}
