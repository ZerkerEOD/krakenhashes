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
	db                       *db.DB // Store db connection for notification service
	jobExecRepo              *repository.JobExecutionRepository
	jobTaskRepo              *repository.JobTaskRepository
	jobIncrementLayerRepo    *repository.JobIncrementLayerRepository
	presetIncrementLayerRepo *repository.PresetIncrementLayerRepository
	benchmarkRepo            *repository.BenchmarkRepository
	agentHashlistRepo        *repository.AgentHashlistRepository
	agentRepo                *repository.AgentRepository
	deviceRepo               *repository.AgentDeviceRepository
	presetJobRepo            repository.PresetJobRepository
	hashlistRepo             *repository.HashListRepository
	hashTypeRepo             *repository.HashTypeRepository
	systemSettingsRepo       *repository.SystemSettingsRepository
	fileRepo                 *repository.FileRepository
	scheduleRepo             *repository.AgentScheduleRepository
	binaryManager            binary.Manager
	assocWordlistRepo        *repository.AssociationWordlistRepository
	clientWordlistRepo       *repository.ClientWordlistRepository
	clientPotfileRepo        *repository.ClientPotfileRepository

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
	clientWordlistRepo *repository.ClientWordlistRepository,
	clientPotfileRepo *repository.ClientPotfileRepository,
	hashcatBinaryPath string,
	dataDirectory string,
) *JobExecutionService {
	debug.Log("Creating JobExecutionService", map[string]interface{}{
		"data_directory": dataDirectory,
		"is_absolute":    filepath.IsAbs(dataDirectory),
	})

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
		assocWordlistRepo:        assocWordlistRepo,
		clientWordlistRepo:       clientWordlistRepo,
		clientPotfileRepo:        clientPotfileRepo,
		hashcatBinaryPath:        hashcatBinaryPath,
		dataDirectory:            dataDirectory,
	}
}

// GetFreshEffectiveKeyspace fetches the current effective_keyspace from the database.
// This is needed because in-memory JobExecution objects may be stale after benchmark updates.
func (s *JobExecutionService) GetFreshEffectiveKeyspace(ctx context.Context, jobID uuid.UUID, layerID *uuid.UUID) (models.BigInt, error) {
	if layerID != nil {
		// For increment layers, fetch from job_increment_layers table
		layer, err := s.jobIncrementLayerRepo.GetByID(ctx, *layerID)
		if err != nil {
			return models.BigInt{}, fmt.Errorf("failed to fetch increment layer: %w", err)
		}
		if layer != nil && layer.EffectiveKeyspace != nil {
			return *layer.EffectiveKeyspace, nil
		}
		return models.BigInt{}, nil
	}

	// For regular jobs, fetch from job_executions table
	job, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return models.BigInt{}, fmt.Errorf("failed to fetch job execution: %w", err)
	}
	if job != nil && job.EffectiveKeyspace != nil {
		return *job.EffectiveKeyspace, nil
	}
	return models.BigInt{}, nil
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
	CustomCharsets            models.CustomCharsets
	CustomCharsetFiles        models.CustomCharsetFiles
	HexCharset                bool
	Priority                  int
	MaxAgents                 int
	BinaryVersion             string // Version pattern (e.g., "default", "7.x", "7.1.2")
	AllowHighPriorityOverride bool
	ChunkSizeSeconds          int
	IncrementMode             string
	IncrementMin              *int
	IncrementMax              *int
	AssociationWordlistID     *uuid.UUID // For association attacks (-a 9)
	AdditionalArgs            *string    // Additional hashcat arguments
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

	// Refresh the preset's keyspace if stale BEFORE reading its pre-calculated
	// values below. This runs hashcat --keyspace/--total-candidates only when
	// the cached values are missing/inaccurate or a referenced wordlist/rule
	// changed since the preset was last computed, so the created job inherits an
	// accurate keyspace and the scheduler-v2 dispatch gate opens immediately —
	// no agent benchmark needed just for keyspace. Best-effort: on failure we
	// proceed with whatever the preset already had (the scheduler's benchmark
	// phase remains the accuracy backstop). Skipped for increment-mode presets
	// inside EnsurePresetKeyspaceFresh.
	presetSvc := NewAdminPresetJobService(s.presetJobRepo, s.presetIncrementLayerRepo, s.systemSettingsRepo, s.binaryManager, s.fileRepo, s.dataDirectory)
	if fresh, ferr := presetSvc.EnsurePresetKeyspaceFresh(ctx, presetJob); ferr != nil {
		debug.Warning("CreateJobExecution: preset keyspace refresh failed for %s: %v", presetJobID, ferr)
	} else if fresh != nil {
		presetJob = fresh
	}

	// Get the hashlist
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}

	// Use pre-calculated keyspace from preset job if available
	var totalKeyspace *int64
	var effectiveKeyspace *models.BigInt
	var isAccurateKeyspace bool
	var multiplicationFactor int64 = 1

	if presetJob.Keyspace != nil && *presetJob.Keyspace > 0 {
		totalKeyspace = presetJob.Keyspace
		effectiveKeyspace = presetJob.EffectiveKeyspace
		isAccurateKeyspace = presetJob.IsAccurateKeyspace
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
		// Calculate multiplication factor from returned values.
		// Rounded (not truncated) so 70923768/23641330 = 2.9999… → 3, not 2.
		// This is a display-only value; correctness paths derive the ratio from
		// effective/base directly in big.Int (see dispatcher / progress calc).
		if isAccurateKeyspace && totalKeyspace != nil && *totalKeyspace > 0 && effectiveKeyspace != nil && effectiveKeyspace.IsPositive() {
			multiplicationFactor = effectiveKeyspace.DivRoundInt64(*totalKeyspace).Int64()
			if multiplicationFactor < 1 {
				multiplicationFactor = 1
			}
		}
	}

	// For salted hash types, adjust effective_keyspace by salt count
	// Preset's --total-candidates = base × rules (no hashlist, no salts)
	// Job's effective_keyspace = base × rules × salts (to match progress[1])
	if effectiveKeyspace != nil && effectiveKeyspace.IsPositive() {
		hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
		if htErr == nil && hashType != nil && hashType.IsSalted {
			saltCount := int64(hashlist.TotalHashes)
			if saltCount > 0 {
				originalEffective := *effectiveKeyspace
				adjustedEffective := originalEffective.MulInt64(saltCount)
				effectiveKeyspace = &adjustedEffective
				// Also adjust multiplication factor (rounded, display-only).
				if totalKeyspace != nil && *totalKeyspace > 0 {
					multiplicationFactor = adjustedEffective.DivRoundInt64(*totalKeyspace).Int64()
				}
				debug.Log("Applied salt adjustment to effective keyspace at job creation", map[string]interface{}{
					"preset_job_id":      presetJobID,
					"hash_type_id":       hashlist.HashTypeID,
					"is_salted":          true,
					"salt_count":         saltCount,
					"original_effective": originalEffective.String(),
					"adjusted_effective": adjustedEffective.String(),
				})
			}
		}
	}

	// Create job execution with all configuration copied from preset
	jobExecution := &models.JobExecution{
		PresetJobID:       &presetJobID, // Keep reference for audit trail
		HashlistID:        hashlistID,
		Status:            models.JobExecutionStatusPending,
		Priority:          presetJob.Priority,
		ProcessedKeyspace: models.NewBigInt(0),
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
		BinaryVersion:             presetJob.BinaryVersion,
		Mask:                      presetJob.Mask,
		CustomCharsets:            presetJob.CustomCharsets,
		CustomCharsetFiles:        presetJob.CustomCharsetFiles,
		HexCharset:                presetJob.HexCharset,
		AdditionalArgs:            presetJob.AdditionalArgs,
		IncrementMode:             presetJob.IncrementMode,
		IncrementMin:              presetJob.IncrementMin,
		IncrementMax:              presetJob.IncrementMax,

		// Keyspace values from preset or calculated
		BaseKeyspace:         totalKeyspace,
		EffectiveKeyspace:    effectiveKeyspace,
		MultiplicationFactor: multiplicationFactor,
		IsAccurateKeyspace:   isAccurateKeyspace,
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

	s.populateSchedulingUnitsIfEnabled(ctx, jobExecution)

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

	// Validate mask pattern for attack modes that use masks
	if config.Mask != "" {
		switch config.AttackMode {
		case models.AttackModeBruteForce, models.AttackModeHybridWordlistMask, models.AttackModeHybridMaskWordlist:
			if !validateMaskPattern(config.Mask) {
				return nil, fmt.Errorf("invalid mask pattern format")
			}
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
		BinaryVersion:             config.BinaryVersion,
		Mask:                      config.Mask,
		CustomCharsets:            config.CustomCharsets,
		CustomCharsetFiles:        config.CustomCharsetFiles,
		HexCharset:                config.HexCharset,
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

	// Compute keyspace + dispatch strategy (shared with the preparing/finalize path).
	ks, err := s.computeKeyspaceStrategy(ctx, tempPreset, hashlist)
	if err != nil {
		return nil, err
	}
	totalKeyspace, effectiveKeyspace, isAccurateKeyspace := ks.base, ks.effective, ks.isAccurate
	multiplicationFactor := ks.multiplicationFactor

	// Create self-contained job execution
	jobExecution := &models.JobExecution{
		PresetJobID:           nil, // NULL for custom jobs
		HashlistID:            hashlistID,
		AssociationWordlistID: config.AssociationWordlistID, // For association attacks (-a 9)
		Status:                models.JobExecutionStatusPending,
		Priority:              config.Priority,
		ProcessedKeyspace:     models.NewBigInt(0),
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
		BinaryVersion:             config.BinaryVersion,
		Mask:                      config.Mask,
		CustomCharsets:            config.CustomCharsets,
		CustomCharsetFiles:        config.CustomCharsetFiles,
		HexCharset:                config.HexCharset,
		AdditionalArgs:            config.AdditionalArgs,
		IncrementMode:             config.IncrementMode,
		IncrementMin:              config.IncrementMin,
		IncrementMax:              config.IncrementMax,

		// Keyspace values from calculation
		BaseKeyspace:         totalKeyspace,
		EffectiveKeyspace:    effectiveKeyspace,
		MultiplicationFactor: multiplicationFactor,
		IsAccurateKeyspace:   isAccurateKeyspace,
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

	// Phase E hook — same as CreateJobExecution; see comment there.
	s.populateSchedulingUnitsIfEnabled(ctx, jobExecution)

	return jobExecution, nil
}

// keyspaceStrategy holds the keyspace + dispatch-strategy values computed for a
// custom job, shared by CreateCustomJobExecution and FinalizeFilterJob (GH #40).
type keyspaceStrategy struct {
	base                 *int64
	effective            *models.BigInt
	isAccurate           bool
	multiplicationFactor int64
}

// computeKeyspaceStrategy runs hashcat keyspace calculation and derives the
// salt-adjusted effective keyspace and (display-only) multiplication factor.
// Extracted verbatim from the original inline block in CreateCustomJobExecution
// so both the synchronous and preparing/finalize paths stay consistent.
func (s *JobExecutionService) computeKeyspaceStrategy(ctx context.Context, tempPreset *models.PresetJob, hashlist *models.HashList) (keyspaceStrategy, error) {
	totalKeyspace, effectiveKeyspace, isAccurateKeyspace, err := s.calculateKeyspace(ctx, tempPreset, hashlist)
	if err != nil {
		debug.Error("Failed to calculate keyspace for custom job: %v", err)
		return keyspaceStrategy{}, fmt.Errorf("keyspace calculation is required for job execution: %w", err)
	}

	// Calculate multiplication factor from keyspace values (rounded, display-only).
	var multiplicationFactor int64 = 1
	if isAccurateKeyspace && totalKeyspace != nil && *totalKeyspace > 0 && effectiveKeyspace != nil && effectiveKeyspace.IsPositive() {
		multiplicationFactor = effectiveKeyspace.DivRoundInt64(*totalKeyspace).Int64()
		if multiplicationFactor < 1 {
			multiplicationFactor = 1
		}
	}

	// For salted hash types, adjust effective_keyspace by salt count
	// calculateKeyspace's --total-candidates = base × rules (no hashlist when run)
	// Job's effective_keyspace = base × rules × salts (to match progress[1])
	if effectiveKeyspace != nil && effectiveKeyspace.IsPositive() {
		hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
		if htErr == nil && hashType != nil && hashType.IsSalted {
			saltCount := int64(hashlist.TotalHashes)
			if saltCount > 0 {
				originalEffective := *effectiveKeyspace
				adjustedEffective := originalEffective.MulInt64(saltCount)
				effectiveKeyspace = &adjustedEffective
				// Also adjust multiplication factor (rounded, display-only).
				if totalKeyspace != nil && *totalKeyspace > 0 {
					multiplicationFactor = adjustedEffective.DivRoundInt64(*totalKeyspace).Int64()
				}
				debug.Log("Applied salt adjustment to effective keyspace at custom job creation", map[string]interface{}{
					"custom_job_name":    tempPreset.Name,
					"hash_type_id":       hashlist.HashTypeID,
					"is_salted":          true,
					"salt_count":         saltCount,
					"original_effective": originalEffective.String(),
					"adjusted_effective": adjustedEffective.String(),
				})
			}
		}
	}

	return keyspaceStrategy{
		base:                 totalKeyspace,
		effective:            effectiveKeyspace,
		isAccurate:           isAccurateKeyspace,
		multiplicationFactor: multiplicationFactor,
	}, nil
}

// resolveChunkSize resolves the chunk duration from the request or system settings.
func (s *JobExecutionService) resolveChunkSize(ctx context.Context, requested int) int {
	chunkSize := requested
	if chunkSize <= 0 {
		if setting, err := s.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration"); err == nil && setting != nil && setting.Value != nil {
			if parsed, parseErr := parseIntValueFromString(*setting.Value); parseErr == nil {
				chunkSize = parsed
			}
		}
		if chunkSize <= 0 {
			chunkSize = 900
		}
	}
	return chunkSize
}

// buildTempPresetFromConfig builds the preset-shaped struct used for keyspace
// calculation from a custom-job config.
func buildTempPresetFromConfig(config CustomJobConfig, hashTypeID, chunkSize int) *models.PresetJob {
	tempPreset := &models.PresetJob{
		Name:                      config.Name,
		WordlistIDs:               config.WordlistIDs,
		RuleIDs:                   config.RuleIDs,
		AttackMode:                config.AttackMode,
		HashType:                  hashTypeID,
		BinaryVersion:             config.BinaryVersion,
		Mask:                      config.Mask,
		CustomCharsets:            config.CustomCharsets,
		CustomCharsetFiles:        config.CustomCharsetFiles,
		HexCharset:                config.HexCharset,
		Priority:                  config.Priority,
		MaxAgents:                 config.MaxAgents,
		AllowHighPriorityOverride: config.AllowHighPriorityOverride,
		ChunkSizeSeconds:          chunkSize,
		StatusUpdatesEnabled:      true,
		IncrementMode:             config.IncrementMode,
		IncrementMin:              config.IncrementMin,
		IncrementMax:              config.IncrementMax,
	}
	if config.AssociationWordlistID != nil {
		assocIDStr := config.AssociationWordlistID.String()
		tempPreset.AssociationWordlistID = &assocIDStr
	}
	return tempPreset
}

// CreatePreparingFilterJob creates a job_executions row in the "preparing" state
// for a custom job whose ephemeral filtered wordlist is still generating (GH #40).
// It carries the user's chosen config (so the jobs table shows real details) but
// no keyspace and no scheduling_units, so the scheduler ignores it. Call
// FinalizeFilterJob once the wordlist is ready, or FailJob on error.
func (s *JobExecutionService) CreatePreparingFilterJob(ctx context.Context, config CustomJobConfig, hashlistID int64, createdBy *uuid.UUID, name string) (*models.JobExecution, error) {
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hashlist: %w", err)
	}
	chunkSize := s.resolveChunkSize(ctx, config.ChunkSizeSeconds)

	jobExecution := &models.JobExecution{
		PresetJobID:               nil,
		HashlistID:                hashlistID,
		AssociationWordlistID:     config.AssociationWordlistID,
		Status:                    models.JobExecutionStatusPreparing,
		Priority:                  config.Priority,
		AttackMode:                config.AttackMode,
		MaxAgents:                 config.MaxAgents,
		CreatedBy:                 createdBy,
		Name:                      name,
		WordlistIDs:               config.WordlistIDs, // user's selection (display only until finalize)
		RuleIDs:                   config.RuleIDs,
		HashType:                  hashlist.HashTypeID,
		ChunkSizeSeconds:          chunkSize,
		StatusUpdatesEnabled:      true,
		AllowHighPriorityOverride: config.AllowHighPriorityOverride,
		BinaryVersion:             config.BinaryVersion,
		Mask:                      config.Mask,
		CustomCharsets:            config.CustomCharsets,
		CustomCharsetFiles:        config.CustomCharsetFiles,
		HexCharset:                config.HexCharset,
		AdditionalArgs:            config.AdditionalArgs,
		IncrementMode:             config.IncrementMode,
		IncrementMin:              config.IncrementMin,
		IncrementMax:              config.IncrementMax,
		MultiplicationFactor:      1,
	}
	if err := s.jobExecRepo.Create(ctx, jobExecution); err != nil {
		return nil, fmt.Errorf("failed to create preparing job: %w", err)
	}
	return jobExecution, nil
}

// FinalizeFilterJob completes a preparing job once its filtered wordlist(s) exist:
// config.WordlistIDs must already point at the generated (filtered) wordlists. It
// computes keyspace, persists the swapped wordlist IDs + keyspace, creates
// scheduling units, and flips the job to pending so the scheduler can dispatch it.
func (s *JobExecutionService) FinalizeFilterJob(ctx context.Context, jobID uuid.UUID, config CustomJobConfig) error {
	job, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to load preparing job: %w", err)
	}
	hashlist, err := s.hashlistRepo.GetByID(ctx, job.HashlistID)
	if err != nil {
		return fmt.Errorf("failed to get hashlist: %w", err)
	}

	tempPreset := buildTempPresetFromConfig(config, hashlist.HashTypeID, job.ChunkSizeSeconds)

	ks, err := s.computeKeyspaceStrategy(ctx, tempPreset, hashlist)
	if err != nil {
		return err
	}

	// Persist the swapped (filtered) wordlist IDs and keyspace onto the existing row.
	job.WordlistIDs = config.WordlistIDs
	job.BaseKeyspace = ks.base
	job.EffectiveKeyspace = ks.effective
	job.MultiplicationFactor = ks.multiplicationFactor
	job.IsAccurateKeyspace = ks.isAccurate

	if err := s.jobExecRepo.UpdateWordlistIDs(ctx, jobID, config.WordlistIDs); err != nil {
		return fmt.Errorf("failed to update wordlist ids: %w", err)
	}
	if err := s.jobExecRepo.UpdateKeyspaceInfo(ctx, job); err != nil {
		return fmt.Errorf("failed to update keyspace info: %w", err)
	}

	// Mirror CreateCustomJobExecution's post-keyspace steps.
	if err := s.initializeIncrementLayers(ctx, job, tempPreset); err != nil {
		return fmt.Errorf("failed to initialize increment layers: %w", err)
	}
	if (job.IncrementMode == "" || job.IncrementMode == "off") && !job.IsAccurateKeyspace {
		if err := s.calculateEffectiveKeyspace(ctx, job, tempPreset); err != nil {
			debug.Error("Failed to calculate effective keyspace for finalized job %s: %v", jobID, err)
		}
	}
	s.populateSchedulingUnitsIfEnabled(ctx, job)

	// Open the dispatch gate.
	if err := s.jobExecRepo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending); err != nil {
		return fmt.Errorf("failed to set job pending: %w", err)
	}
	debug.Info("Finalized filtered custom job %s (preparing -> pending)", jobID)
	return nil
}

// FailJob marks a job failed with a reason and dispatches the job-failed
// notification. Used to surface preparation failures (e.g. a 0-match filter or a
// full disk) to the user as a failed job in the table (GH #40).
func (s *JobExecutionService) FailJob(ctx context.Context, jobID uuid.UUID, reason string) error {
	if err := s.jobExecRepo.FailExecution(ctx, jobID, reason); err != nil {
		return fmt.Errorf("failed to mark job %s failed: %w", jobID, err)
	}
	if jobExec, err := s.jobExecRepo.GetByID(ctx, jobID); err == nil && jobExec.CreatedBy != nil {
		s.dispatchJobFailedNotification(ctx, jobExec, reason)
	}
	return nil
}

// calculateKeyspace calculates the total keyspace for a job using hashcat --keyspace
// Returns: baseKeyspace, effectiveKeyspace, isAccurateKeyspace, error
// If --total-candidates succeeds, effectiveKeyspace will be accurate and isAccurateKeyspace=true
// Otherwise, effectiveKeyspace will be an estimate and isAccurateKeyspace=false
func (s *JobExecutionService) calculateKeyspace(ctx context.Context, presetJob *models.PresetJob, hashlist *models.HashList) (*int64, *models.BigInt, bool, error) {
	debug.Log("Starting keyspace calculation for job execution", map[string]interface{}{
		"preset_job_id":  presetJob.ID,
		"binary_version": presetJob.BinaryVersion,
		"attack_mode":    presetJob.AttackMode,
		"hashlist_id":    hashlist.ID,
		"data_directory": s.dataDirectory,
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
		// Effective keyspace (base × rules) can exceed int64 for large wordlists,
		// so compute it in big.Int.
		effectiveKeyspace := models.NewBigInt(lineCount).MulInt64(ruleCount)

		debug.Log("Mode 9 keyspace estimation", map[string]interface{}{
			"wordlist_line_count": lineCount,
			"rule_count":          ruleCount,
			"base_keyspace":       baseKeyspace,
			"effective_keyspace":  effectiveKeyspace.String(),
		})

		// Return early - don't run hashcat --keyspace (not supported for mode 9)
		return &baseKeyspace, &effectiveKeyspace, false, nil // false = not accurate (estimation)

	default:
		return nil, nil, false, fmt.Errorf("unsupported attack mode for keyspace calculation: %d", presetJob.AttackMode)
	}

	// Add --hex-charset ONLY if job uses hex mode AND has inline charset definitions
	// (file charsets are unaffected by --hex-charset, and without any -1/-2/-3/-4 inline defs
	// hashcat will reject --hex-charset as it tries to interpret the mask as hex)
	if presetJob.HexCharset {
		hasInlineCharset := false
		for _, slot := range []string{"1", "2", "3", "4"} {
			if _, isFile := presetJob.CustomCharsetFiles[slot]; isFile {
				continue
			}
			if def, ok := presetJob.CustomCharsets[slot]; ok && def != "" {
				hasInlineCharset = true
				break
			}
		}
		if hasInlineCharset {
			args = append(args, "--hex-charset")
		}
	}

	// Add custom charset flags (-1 through -4) before keyspace calculation
	// File charsets take priority over inline definitions (same slot can't have both)
	for _, slot := range []string{"1", "2", "3", "4"} {
		if cf, ok := presetJob.CustomCharsetFiles[slot]; ok && cf.FilePath != "" {
			// File charset — use the actual backend file path so hashcat can read it for keyspace
			charsetPath := filepath.Join(s.dataDirectory, cf.FilePath)
			args = append(args, "-"+slot, charsetPath)
		} else if def, ok := presetJob.CustomCharsets[slot]; ok && def != "" {
			args = append(args, "-"+slot, def)
		}
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

	// Execute hashcat command with configurable timeout
	keyspaceTimeout := s.getKeyspaceTimeout(ctx)
	ctx, cancel := context.WithTimeout(ctx, keyspaceTimeout)
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
		// Check if the error was caused by a timeout
		if ctx.Err() == context.DeadlineExceeded {
			debug.Error("Hashcat keyspace calculation timed out after %v", keyspaceTimeout)
			return nil, nil, false, fmt.Errorf("keyspace calculation timed out after %v — an administrator can increase this limit in Admin Settings > Job Execution > Keyspace Calculation Timeout", keyspaceTimeout)
		}
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

	var effectiveKeyspacePtr *models.BigInt
	if isAccurate && effectiveKeyspace > 0 {
		// Use accurate value from --total-candidates
		effectiveKeyspacePtr = models.NewBigIntPtr(effectiveKeyspace)

		debug.Log("Using accurate effective keyspace from --total-candidates", map[string]interface{}{
			"hashlist_id":        hashlist.ID,
			"base_keyspace":      keyspace,
			"effective_keyspace": effectiveKeyspace,
		})
	} else {
		// Fall back to estimation: base * rule_count.
		// base × rules can exceed int64 for large wordlists, so use big.Int.
		estimatedEffective := models.NewBigInt(keyspace)
		if len(presetJob.RuleIDs) > 0 {
			// For estimation, assume each rule file has approximately 1 rule on average
			// This is conservative - actual count may vary significantly
			ruleCount := int64(len(presetJob.RuleIDs))
			if ruleCount > 0 {
				estimatedEffective = models.NewBigInt(keyspace).MulInt64(ruleCount)
			}
		}
		effectiveKeyspacePtr = &estimatedEffective

		debug.Log("Using estimated effective keyspace (--total-candidates failed or unavailable)", map[string]interface{}{
			"hashlist_id":         hashlist.ID,
			"base_keyspace":       keyspace,
			"estimated_effective": estimatedEffective.String(),
			"rule_count":          len(presetJob.RuleIDs),
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

	keyspaceTimeout := s.getKeyspaceTimeout(ctx)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			debug.Warning("Retrying --total-candidates (attempt %d/%d) after %v delay: %v",
				attempt, maxRetries, retryDelay, lastErr)
			time.Sleep(retryDelay)
		}

		execCtx, cancel := context.WithTimeout(ctx, keyspaceTimeout)
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
			// Check if timed out
			if execCtx.Err() == context.DeadlineExceeded {
				debug.Warning("--total-candidates timed out after %v for job %s — consider increasing keyspace_calculation_timeout_minutes in Admin Settings", keyspaceTimeout, jobID)
				return 0, false, nil // Allow fallback to estimation
			}
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
		"job_id":         job.ID,
		"base_keyspace":  baseKeyspace,
		"attack_mode":    attackMode,
		"rule_ids":       presetJob.RuleIDs,
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
			job.MultiplicationFactor = totalRuleCount
			job.IsAccurateKeyspace = false // Will be set by first agent benchmark or --total-candidates

			// Estimate effective keyspace using simple formula
			// Formula: base_keyspace × rule_count
			// NOTE: hash_count is NOT part of keyspace - keyspace is about candidates to try,
			// not targets. This is an estimate; --total-candidates or benchmark provides accurate values.
			// base × rules can exceed int64 for large wordlists, so use big.Int.
			estimatedEffective := models.NewBigInt(baseKeyspace).MulInt64(totalRuleCount)
			job.EffectiveKeyspace = &estimatedEffective

			debug.Log("Straight attack with rules - estimated effective keyspace", map[string]interface{}{
				"rule_count":          totalRuleCount,
				"base_keyspace":       baseKeyspace,
				"estimated_effective": estimatedEffective.String(),
			})
		} else {
			// No rules, effective = base
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = models.NewBigIntPtr(baseKeyspace)
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
				job.MultiplicationFactor = keyspace2
			} else {
				job.MultiplicationFactor = keyspace1
			}

			job.IsAccurateKeyspace = false // Will be set by first agent benchmark

			// Estimate effective keyspace (will be updated to actual from hashcat benchmark).
			// keyspace1 × keyspace2 can exceed int64, so use big.Int.
			estimatedEffective := models.NewBigInt(keyspace1).MulInt64(keyspace2)
			job.EffectiveKeyspace = &estimatedEffective

			debug.Log("Combination attack - using estimated effective keyspace", map[string]interface{}{
				"wordlist1_keyspace":  keyspace1,
				"wordlist2_keyspace":  keyspace2,
				"estimated_effective": estimatedEffective.String(),
			})
		} else {
			// Not enough wordlists for combination
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = models.NewBigIntPtr(baseKeyspace)
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
				job.EffectiveKeyspace = models.NewBigIntPtr(baseKeyspace)
			} else {
				ruleCount, err := s.GetTotalRuleCount(ctx, presetJob.RuleIDs)
				if err != nil {
					ruleCount = 1
				}

				job.BaseKeyspace = &lineCount
				job.MultiplicationFactor = ruleCount
				// Mode 9 keyspace is estimated from wordlist line count × rule count
				// IsAccurateKeyspace = false triggers forced benchmark for speed measurement
				job.IsAccurateKeyspace = false

				// lineCount × ruleCount can exceed int64, so use big.Int.
				effectiveKeyspace := models.NewBigInt(lineCount).MulInt64(ruleCount)
				job.EffectiveKeyspace = &effectiveKeyspace

				debug.Log("Association attack keyspace calculated", map[string]interface{}{
					"wordlist_line_count": lineCount,
					"rule_count":          ruleCount,
					"effective_keyspace":  effectiveKeyspace.String(),
					"is_accurate":         false,
				})
			}
		} else {
			// No association wordlist - shouldn't happen but handle gracefully
			job.BaseKeyspace = &baseKeyspace
			job.MultiplicationFactor = 1
			job.EffectiveKeyspace = models.NewBigIntPtr(baseKeyspace)
		}

	default: // Attacks 3, 6, 7 - hashcat calculates correctly
		job.BaseKeyspace = &baseKeyspace
		job.MultiplicationFactor = 1
		job.EffectiveKeyspace = models.NewBigIntPtr(baseKeyspace)

		debug.Log("Standard attack mode", map[string]interface{}{
			"attack_mode": attackMode,
			"keyspace":    baseKeyspace,
		})
	}

	// Apply salt adjustment for salted hash types (same pattern as CreateCustomJobExecution:542-567)
	// calculateEffectiveKeyspace calculates base × rules, but for salted hashes we need base × rules × salts
	if job.EffectiveKeyspace != nil && job.EffectiveKeyspace.IsPositive() {
		hashlist, hlErr := s.hashlistRepo.GetByID(ctx, job.HashlistID)
		if hlErr == nil && hashlist != nil {
			hashType, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
			if htErr == nil && hashType != nil && hashType.IsSalted {
				saltCount := int64(hashlist.TotalHashes)
				if saltCount > 0 {
					originalEffective := *job.EffectiveKeyspace
					adjustedEffective := originalEffective.MulInt64(saltCount)
					job.EffectiveKeyspace = &adjustedEffective
					// Also adjust multiplication factor to reflect salts (rounded, display-only).
					if job.BaseKeyspace != nil && *job.BaseKeyspace > 0 {
						job.MultiplicationFactor = adjustedEffective.DivRoundInt64(*job.BaseKeyspace).Int64()
					}
					debug.Log("Applied salt adjustment in calculateEffectiveKeyspace", map[string]interface{}{
						"job_id":             job.ID,
						"hash_type_id":       hashlist.HashTypeID,
						"is_salted":          true,
						"salt_count":         saltCount,
						"original_effective": originalEffective.String(),
						"adjusted_effective": adjustedEffective.String(),
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
						"agent_id":     agent.ID,
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
							"agent_id":    agent.ID,
							"task_id":     taskIDStr,
							"task_status": task.Status,
							"reason":      "task_in_terminal_state",
						})
						agent.Metadata["busy_status"] = "false"
						delete(agent.Metadata, "current_task_id")
						delete(agent.Metadata, "current_job_id")
						s.agentRepo.UpdateMetadata(ctx, agent.ID, agent.Metadata)
					} else if task.Status != models.JobTaskStatusRunning && task.Status != models.JobTaskStatusAssigned {
						// Task in unexpected state
						debug.Log("Clearing stale busy status - task in unexpected state", map[string]interface{}{
							"agent_id":      agent.ID,
							"stale_task_id": taskIDStr,
							"task_status":   task.Status,
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
	var effectiveStart, effectiveEnd models.BigInt

	if jobExecution.MultiplicationFactor > 1 && jobExecution.BaseKeyspace != nil && *jobExecution.BaseKeyspace > 0 {
		// Task with rules: estimate total effective keyspace.
		// base × rules can exceed int64, so use big.Int.
		effectiveStart = models.NewBigInt(0)
		effectiveEnd = models.NewBigInt(*jobExecution.BaseKeyspace).MulInt64(jobExecution.MultiplicationFactor)

		debug.Log("Non-split task with rules - estimated effective keyspace", map[string]interface{}{
			"job_id":                jobExecution.ID,
			"base_keyspace":         *jobExecution.BaseKeyspace,
			"multiplication_factor": jobExecution.MultiplicationFactor,
			"estimated_effective":   effectiveEnd.String(),
		})
	} else {
		// No rules: effective = base keyspace range
		effectiveStart = models.NewBigInt(keyspaceStart)
		effectiveEnd = models.NewBigInt(keyspaceEnd)

		debug.Log("No rules - effective equals base keyspace", map[string]interface{}{
			"job_id":         jobExecution.ID,
			"keyspace_start": keyspaceStart,
			"keyspace_end":   keyspaceEnd,
		})
	}

	// Data integrity check: validate that task's effective keyspace doesn't exceed job total (with 10% tolerance for estimates)
	if jobExecution.EffectiveKeyspace != nil {
		tolerance := jobExecution.EffectiveKeyspace.Add(jobExecution.EffectiveKeyspace.DivInt64(10))
		if effectiveEnd.Cmp(tolerance) > 0 {
			debug.Warning("Task effective_keyspace_end exceeds job total (with tolerance): job_id=%s, task_effective_end=%s, job_effective_total=%s",
				jobExecution.ID, effectiveEnd.String(), jobExecution.EffectiveKeyspace.String())
		}
	}

	effectiveProcessed := models.NewBigInt(0)
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
	effectiveChunkSize := effectiveEnd.Sub(effectiveStart)
	baseChunkSize := keyspaceEnd - keyspaceStart

	debug.Log("Incrementing dispatched keyspace", map[string]interface{}{
		"job_id":               jobExecution.ID,
		"effective_chunk_size": effectiveChunkSize.String(),
		"base_chunk_size":      baseChunkSize,
	})

	debug.Log("Job task created", map[string]interface{}{
		"task_id":              jobTask.ID,
		"agent_id":             agent.ID,
		"keyspace_start":       keyspaceStart,
		"keyspace_end":         keyspaceEnd,
		"chunk_duration":       chunkDuration,
		"base_chunk_size":      baseChunkSize,
		"effective_chunk_size": effectiveChunkSize.String(),
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
		// This branch returns before the completion path's CleanupJobResources, so
		// sweep ephemeral filtered wordlists here too — otherwise a failed ephemeral
		// job's __eph__ wordlist lingers until the next completed job (GH #40).
		if err := s.sweepEphemeralWordlists(ctx); err != nil {
			debug.Error("Failed to sweep ephemeral filtered wordlists on job failure: %v", err)
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
	} else if actualKeyspace.IsPositive() {
		job, err := s.jobExecRepo.GetByID(ctx, jobExecutionID)
		if err == nil {
			needsUpdate := false
			oldEffective := models.NewBigInt(0)
			if job.EffectiveKeyspace != nil {
				oldEffective = *job.EffectiveKeyspace
			}

			// Update if effective_keyspace differs from actual
			if job.EffectiveKeyspace == nil || job.EffectiveKeyspace.Cmp(actualKeyspace) != 0 {
				needsUpdate = true
			}

			if needsUpdate {
				// Update effective_keyspace to match actual work
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, actualKeyspace); updateErr != nil {
					debug.Error("Failed to update effective keyspace at completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace at completion: %s -> %s (actual from tasks)", oldEffective.String(), actualKeyspace.String())
				}

				// Also sync dispatched_keyspace to match (ensures 100% progress display)
				if updateErr := s.jobExecRepo.UpdateDispatchedKeyspace(ctx, jobExecutionID, actualKeyspace); updateErr != nil {
					debug.Error("Failed to update dispatched keyspace at completion: %v", updateErr)
				}
			}
		}
	}

	// Legacy check: If no chunk_actual_keyspace data, fall back to old method
	if actualKeyspace.IsZero() {
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
				if err == nil && job.ProcessedKeyspace.IsPositive() {
					if job.EffectiveKeyspace != nil && job.EffectiveKeyspace.Cmp(job.ProcessedKeyspace) != 0 {
						oldEffective := *job.EffectiveKeyspace
						if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
							debug.Error("Failed to update effective keyspace (legacy): %v", updateErr)
						} else {
							debug.Info("Updated effective_keyspace from %s to %s (legacy method)", oldEffective.String(), job.ProcessedKeyspace.String())
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
		if err == nil && job.ProcessedKeyspace.IsPositive() {
			if job.EffectiveKeyspace != nil && job.ProcessedKeyspace.Cmp(*job.EffectiveKeyspace) < 0 {
				// Case 1: Early completion - sync effective DOWN to processed
				oldEffective := *job.EffectiveKeyspace
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync effective_keyspace for early completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace for early completion: %s -> %s", oldEffective.String(), job.ProcessedKeyspace.String())
				}
				// Also sync dispatched_keyspace to match
				if updateErr := s.jobExecRepo.UpdateDispatchedKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync dispatched_keyspace for early completion: %v", updateErr)
				}
			} else if job.EffectiveKeyspace == nil || job.EffectiveKeyspace.Cmp(job.ProcessedKeyspace) < 0 {
				// Case 2: Over-completion - benchmark gave lower estimate than actual work
				// Sync effective UP to processed to prevent >100% progress display
				oldEffective := models.NewBigInt(0)
				if job.EffectiveKeyspace != nil {
					oldEffective = *job.EffectiveKeyspace
				}
				if updateErr := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecutionID, job.ProcessedKeyspace); updateErr != nil {
					debug.Error("Failed to sync effective_keyspace for over-completion: %v", updateErr)
				} else {
					debug.Info("Synced effective_keyspace for over-completion: %s -> %s", oldEffective.String(), job.ProcessedKeyspace.String())
				}
			}
		}
	}

	// GH #62 defense-in-depth: reconcile any still-non-terminal task for this
	// job to 'cancelled' before completing. This covers the code-6 bypass in
	// ProcessJobCompletion (job_scheduling_service.go), which races the async
	// HandleHashlistFullyCracked goroutine and can reach here while a sibling
	// task is still running/assigned/processing/pending. Invariant: the DB is
	// NEVER job=completed with a non-terminal task. 'cancelled' (NOT 'failed')
	// keeps HasFailedTasks from flipping the job to 'failed'. Best-effort:
	// log on error, don't abort completion.
	if res, reconErr := s.db.ExecContext(ctx, `
		UPDATE job_tasks
		SET status = 'cancelled', completed_at = NOW()
		WHERE job_execution_id = $1
		  AND status IN ('assigned', 'running', 'processing', 'pending')
	`, jobExecutionID); reconErr != nil {
		debug.Error("Failed to reconcile non-terminal tasks to cancelled for job %s: %v", jobExecutionID, reconErr)
	} else if affected, _ := res.RowsAffected(); affected > 0 {
		debug.Warning("Reconciled %d non-terminal task(s) to 'cancelled' for job %s before completion — a WS stop signal was likely lost", affected, jobExecutionID)
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
			"hashes_processed": totalHashes,                      // Template uses {{ .HashesProcessed }}
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

// adminUserIDs returns the IDs of every enabled, non-deleted admin user.
// Used as the fan-out target for system-level notifications
// (agent_error, benchmark_storm, hashlist_malformed). Best-effort: returns
// nil on failure so the caller can keep going.
func (s *JobExecutionService) adminUserIDs(ctx context.Context) []uuid.UUID {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM users
		WHERE role = 'admin' AND account_enabled = true AND deleted_at IS NULL`)
	if err != nil {
		debug.Warning("adminUserIDs query: %v", err)
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			debug.Warning("adminUserIDs scan: %v", err)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// fanout dispatches the same notification template to a deduplicated set of
// recipients. The dispatcher honors each user's per-channel preferences, so
// callers don't need to think about email/webhook/in-app selection here.
func (s *JobExecutionService) fanout(
	ctx context.Context,
	recipients []uuid.UUID,
	template models.NotificationDispatchParams,
) {
	dispatcher := GetGlobalDispatcher()
	if dispatcher == nil {
		debug.Warning("Notification dispatcher not available, skipping %s fan-out", template.Type)
		return
	}
	seen := make(map[uuid.UUID]struct{}, len(recipients))
	for _, uid := range recipients {
		if uid == uuid.Nil {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		params := template
		params.UserID = uid
		if err := dispatcher.Dispatch(ctx, params); err != nil {
			debug.Warning("Dispatch %s to %s: %v", template.Type, uid, err)
		}
	}
}

// DispatchAgentErrorNotification informs the agent owner and admins that an
// agent has been auto-quarantined or otherwise entered an unhealthy state.
// Exported because it's also fired from the scheduling layer (after the
// per-agent benchmark health thresholds trip).
func (s *JobExecutionService) DispatchAgentErrorNotification(
	ctx context.Context,
	agent *models.Agent,
	reason string,
	details map[string]interface{},
) {
	if agent == nil {
		return
	}
	recipients := s.adminUserIDs(ctx)
	if agent.OwnerID != nil {
		recipients = append(recipients, *agent.OwnerID)
	}
	if len(recipients) == 0 {
		debug.Warning("No recipients for agent_error notification (agent %d)", agent.ID)
		return
	}
	data := map[string]interface{}{
		"agent_id":   agent.ID,
		"agent_name": agent.Name,
		"reason":     reason,
		"fired_at":   time.Now().Format(time.RFC3339),
	}
	for k, v := range details {
		if _, dup := data[k]; !dup {
			data[k] = v
		}
	}
	s.fanout(ctx, recipients, models.NotificationDispatchParams{
		Type:       models.NotificationTypeAgentError,
		Title:      "Agent Quarantined",
		Message:    fmt.Sprintf("Agent %q (id=%d) auto-quarantined: %s", agent.Name, agent.ID, reason),
		Data:       data,
		SourceType: "agent",
		SourceID:   fmt.Sprintf("%d", agent.ID),
	})
}

// DispatchHashlistMalformedNotification informs the hashlist owner and admins
// that a hashlist could not be parsed. The reason is operator-facing and
// should include enough context to fix the file (line number, hash mode, etc).
func (s *JobExecutionService) DispatchHashlistMalformedNotification(
	ctx context.Context,
	hashlistID int64,
	hashlistName string,
	ownerID uuid.UUID,
	reason string,
	details map[string]interface{},
) {
	recipients := s.adminUserIDs(ctx)
	if ownerID != uuid.Nil {
		recipients = append(recipients, ownerID)
	}
	if len(recipients) == 0 {
		return
	}
	data := map[string]interface{}{
		"hashlist_id":   hashlistID,
		"hashlist_name": hashlistName,
		"reason":        reason,
		"fired_at":      time.Now().Format(time.RFC3339),
	}
	for k, v := range details {
		if _, dup := data[k]; !dup {
			data[k] = v
		}
	}
	s.fanout(ctx, recipients, models.NotificationDispatchParams{
		Type:       models.NotificationTypeHashlistMalformed,
		Title:      "Hashlist Malformed",
		Message:    fmt.Sprintf("Hashlist %q (id=%d) could not be parsed: %s", hashlistName, hashlistID, reason),
		Data:       data,
		SourceType: "hashlist",
		SourceID:   fmt.Sprintf("%d", hashlistID),
	})
}

// DispatchBenchmarkStormNotification fires an admin-only soft alert when a
// single job has accumulated an unusual number of benchmark failures within
// the storm window — before the per-tuple hard cap auto-fails the job. Lets
// an operator step in early.
func (s *JobExecutionService) DispatchBenchmarkStormNotification(
	ctx context.Context,
	jobExec *models.JobExecution,
	failureCount int,
	windowMinutes int,
) {
	if jobExec == nil {
		return
	}
	recipients := s.adminUserIDs(ctx)
	if len(recipients) == 0 {
		return
	}
	s.fanout(ctx, recipients, models.NotificationDispatchParams{
		Type:    models.NotificationTypeBenchmarkStorm,
		Title:   "Benchmark Storm Detected",
		Message: fmt.Sprintf("Job %q saw %d benchmark failures in the last %d minutes — investigate before it auto-fails.", jobExec.Name, failureCount, windowMinutes),
		Data: map[string]interface{}{
			"job_id":         jobExec.ID.String(),
			"job_name":       jobExec.Name,
			"failure_count":  failureCount,
			"window_minutes": windowMinutes,
			"fired_at":       time.Now().Format(time.RFC3339),
		},
		SourceType: "job",
		SourceID:   jobExec.ID.String(),
	})
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
	if task.IsKeyspaceSplit && task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceStart.IsPositive() {
		if task.EffectiveKeyspaceStart.CmpInt64(effectiveProgress) <= 0 {
			effectiveKeyspaceProcessed = effectiveProgress - task.EffectiveKeyspaceStart.Int64()
			debug.Log("Converted absolute to relative effective keyspace for keysplit task", map[string]interface{}{
				"task_id":                      taskID,
				"effective_progress_raw":       effectiveProgress,
				"effective_keyspace_start":     task.EffectiveKeyspaceStart.String(),
				"effective_keyspace_processed": effectiveKeyspaceProcessed,
			})
		}
		// If effectiveProgress < EffectiveKeyspaceStart, keep original (shouldn't happen but be safe)
	}

	// Step 11r: recalculate progress_percent as a CHUNK-local fraction.
	// The agent sends progress_percent as hashcat's absolute ratio
	// (progress[0]/progress[1]). For chunks dispatched at --skip > 0,
	// this starts high (e.g., 51% baseline for a chunk at job midpoint)
	// — confusing display because the chunk hasn't done that much
	// work; it's just sitting at a high absolute coordinate.
	//
	// User-expected behavior: a single chunk reads 0% → 100% over its
	// own lifetime. Use the chunk-relative values we just computed
	// above: chunk_percent = effective_processed / chunk_effective_size × 100.
	if task.EffectiveKeyspaceStart != nil && task.EffectiveKeyspaceEnd != nil {
		chunkEff := task.EffectiveKeyspaceEnd.Sub(*task.EffectiveKeyspaceStart)
		if chunkEff.IsPositive() {
			localPercent := float64(effectiveKeyspaceProcessed) / float64(chunkEff.Int64()) * 100.0
			if localPercent >= 100.0 {
				localPercent = 99.99 // reserve 100.0 for terminal completion writes (CompleteTask / code-6 path)
			}
			if localPercent < 0 {
				localPercent = 0
			}
			progressPercent = localPercent
		}
	}

	// Update the task progress
	// Note: Job-level progress is now calculated by the polling service (JobProgressCalculationService)
	// which runs every 2 seconds and recalculates from task data
	err = s.jobTaskRepo.UpdateProgress(ctx, taskID, keyspaceProcessed, models.NewBigInt(effectiveKeyspaceProcessed), hashRate, progressPercent)
	if err != nil {
		return fmt.Errorf("failed to update task progress: %w", err)
	}

	return nil
}

// UpdateKeyspaceInfo updates the keyspace information for a job execution
func (s *JobExecutionService) UpdateKeyspaceInfo(ctx context.Context, job *models.JobExecution) error {
	return s.jobExecRepo.UpdateKeyspaceInfo(ctx, job)
}

// RepairPendingJobKeyspaces is a boot-time safety net. It finds pending jobs
// that never started (no processed/dispatched keyspace) and whose keyspace is
// not accurate, then recomputes an accurate keyspace via hashcat
// --keyspace/--total-candidates from the job's OWN attack params and persists it
// (both the job_executions row and its scheduler-v2 scheduling_units). This
// self-heals jobs left inaccurate by an older build — e.g. the scheduler-v2
// bootstrap deadlock that stranded pending jobs — without waiting on an agent
// benchmark, for the common non-optimized case.
//
// Skipped (left to the scheduler's agent-benchmark path):
//   - association (-a 9): hashcat rejects --keyspace/--total-candidates
//   - increment-mode jobs: keyspace is tracked per layer, not job-wide
//   - jobs whose --total-candidates can't produce an accurate value (e.g. it
//     timed out): the agent benchmark remains the backstop
//
// Best-effort: per-job errors are logged and skipped. Returns how many were
// repaired. Intended to run once at server start; safe to run repeatedly (a
// job already accurate or started is skipped, so it's a cheap no-op afterward).
func (s *JobExecutionService) RepairPendingJobKeyspaces(ctx context.Context) (int, error) {
	jobs, err := s.jobExecRepo.GetPendingJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list pending jobs: %w", err)
	}
	unitRepo := repository.NewSchedulingUnitRepository(s.db)
	repaired := 0
	for i := range jobs {
		job := jobs[i]
		if job.IsAccurateKeyspace {
			continue
		}
		if job.ProcessedKeyspace.IsPositive() || job.DispatchedKeyspace.IsPositive() {
			continue // already started — don't disturb in-flight accounting
		}
		if job.AttackMode == models.AttackModeAssociation {
			continue
		}
		if job.IncrementMode != "" && job.IncrementMode != "off" {
			continue
		}

		hashlist, hlErr := s.hashlistRepo.GetByID(ctx, job.HashlistID)
		if hlErr != nil || hashlist == nil {
			debug.Warning("RepairPendingJobKeyspaces: job %s hashlist %d: %v", job.ID, job.HashlistID, hlErr)
			continue
		}

		// Rebuild a preset-shaped struct from the job's own params so we reuse
		// the exact same calculation path as creation (calculateKeyspace).
		tempPreset := &models.PresetJob{
			Name:               job.Name,
			WordlistIDs:        job.WordlistIDs,
			RuleIDs:            job.RuleIDs,
			AttackMode:         job.AttackMode,
			HashType:           hashlist.HashTypeID,
			BinaryVersion:      job.BinaryVersion,
			Mask:               job.Mask,
			CustomCharsets:     job.CustomCharsets,
			CustomCharsetFiles: job.CustomCharsetFiles,
			HexCharset:         job.HexCharset,
		}

		base, effective, accurate, kErr := s.calculateKeyspace(ctx, tempPreset, hashlist)
		if kErr != nil {
			debug.Warning("RepairPendingJobKeyspaces: job %s keyspace calc failed: %v", job.ID, kErr)
			continue
		}
		if !accurate || base == nil || effective == nil || *base <= 0 || !effective.IsPositive() {
			continue // couldn't measure accurately; leave for the agent benchmark
		}

		// Salt adjustment: effective = base × rules × salts (matches hashcat's
		// progress[1] and the creation-time logic). --total-candidates is
		// hashlist-independent, so salts are applied here.
		eff := *effective
		if ht, htErr := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID); htErr == nil && ht != nil && ht.IsSalted {
			if salt := int64(hashlist.TotalHashes); salt > 0 {
				eff = eff.MulInt64(salt)
			}
		}
		mf := eff.DivRoundInt64(*base).Int64() // rounded, display-only
		if mf < 1 {
			mf = 1
		}

		job.BaseKeyspace = base
		job.EffectiveKeyspace = &eff
		job.IsAccurateKeyspace = true
		job.MultiplicationFactor = mf
		if err := s.jobExecRepo.UpdateKeyspaceInfo(ctx, &job); err != nil {
			debug.Warning("RepairPendingJobKeyspaces: persist job %s: %v", job.ID, err)
			continue
		}

		// Propagate to scheduler-v2 units so the dispatch gate opens without an
		// agent benchmark. Non-increment jobs have exactly one unit.
		if units, uErr := unitRepo.GetByParentJobID(ctx, job.ID); uErr == nil {
			for _, u := range units {
				if err := unitRepo.UpdateEffectiveKeyspace(ctx, u.ID, eff, true); err != nil {
					debug.Warning("RepairPendingJobKeyspaces: unit %s: %v", u.ID, err)
				}
			}
		} else {
			debug.Warning("RepairPendingJobKeyspaces: get units for job %s: %v", job.ID, uErr)
		}

		repaired++
		debug.Info("RepairPendingJobKeyspaces: job %s now accurate (base=%d effective=%s)", job.ID, *base, eff.String())
	}
	if repaired > 0 {
		debug.Info("RepairPendingJobKeyspaces: repaired %d pending job(s)", repaired)
	}
	return repaired, nil
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
		"job_execution_id":    jobExecutionID,
		"interrupting_job_id": interruptingJobID,
		"selective_interrupt": selectiveInterrupt,
		"tasks_to_interrupt":  len(taskIDsToInterrupt),
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

// getKeyspaceTimeout returns the configured timeout for keyspace/total-candidates calculations.
func (s *JobExecutionService) getKeyspaceTimeout(ctx context.Context) time.Duration {
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "keyspace_calculation_timeout_minutes")
	if err == nil && setting.Value != nil {
		if val, err := strconv.Atoi(*setting.Value); err == nil && val > 0 {
			return time.Duration(val) * time.Minute
		}
	}
	return 4 * time.Minute // default
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

// getWordlistWordCount returns the recorded word_count for a wordlist ID, or 0 on any error.
// Used to supply a wordlist multiplier to utils.CalculateEffectiveKeyspace for hybrid modes.
// Returning 0 is safe — the estimator treats values < 2 as "no multiplier".
// Skips client-specific wordlist prefixes ("client:" / "potfile:") since those line counts
// live in different repositories; hybrid jobs use regular wordlists via numeric IDs.
func (s *JobExecutionService) getWordlistWordCount(ctx context.Context, wordlistIDStr string) int64 {
	if strings.HasPrefix(wordlistIDStr, "client:") || strings.HasPrefix(wordlistIDStr, "potfile:") {
		return 0
	}
	wordlistID, err := strconv.ParseInt(wordlistIDStr, 10, 64)
	if err != nil {
		return 0
	}
	wordlists, err := s.fileRepo.GetWordlists(ctx, "")
	if err != nil {
		return 0
	}
	for _, wl := range wordlists {
		if wl.ID == int(wordlistID) {
			return wl.WordCount
		}
	}
	return 0
}

// resolveWordlistPath gets the actual file path for a wordlist ID
func (s *JobExecutionService) resolveWordlistPath(ctx context.Context, wordlistIDStr string) (string, error) {
	// Check for client-specific wordlist prefix "client:UUID"
	if strings.HasPrefix(wordlistIDStr, "client:") {
		uuidStr := strings.TrimPrefix(wordlistIDStr, "client:")
		clientWordlistID, err := uuid.Parse(uuidStr)
		if err != nil {
			return "", fmt.Errorf("invalid client wordlist ID format: %w", err)
		}
		if s.clientWordlistRepo == nil {
			return "", fmt.Errorf("client wordlist repository not configured")
		}
		wordlist, err := s.clientWordlistRepo.GetByID(ctx, clientWordlistID)
		if err != nil {
			return "", fmt.Errorf("client wordlist not found: %w", err)
		}
		debug.Log("Resolved client wordlist path", map[string]interface{}{
			"client_wordlist_id": clientWordlistID,
			"file_path":          wordlist.FilePath,
		})
		return wordlist.FilePath, nil
	}

	// Check for client potfile prefix "potfile:ID"
	if strings.HasPrefix(wordlistIDStr, "potfile:") {
		idStr := strings.TrimPrefix(wordlistIDStr, "potfile:")
		potfileID, err := strconv.Atoi(idStr)
		if err != nil {
			return "", fmt.Errorf("invalid potfile ID format: %w", err)
		}
		if s.clientPotfileRepo == nil {
			return "", fmt.Errorf("client potfile repository not configured")
		}
		potfile, err := s.clientPotfileRepo.GetByID(ctx, potfileID)
		if err != nil {
			return "", fmt.Errorf("client potfile not found: %w", err)
		}
		debug.Log("Resolved client potfile path", map[string]interface{}{
			"potfile_id": potfileID,
			"file_path":  potfile.FilePath,
		})
		return potfile.FilePath, nil
	}

	// Try to parse as integer ID first (global wordlist)
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

// CleanupJobResources cleans up all resources for a completed/failed/cancelled job
func (s *JobExecutionService) CleanupJobResources(ctx context.Context, jobID uuid.UUID) error {
	debug.Log("Cleaning up job resources", map[string]interface{}{
		"job_id": jobID,
	})

	// Self-healing sweep of ephemeral filtered wordlists (GH #40) owned by any
	// terminal job. Running it here means every completed job also clears
	// stragglers left by failed/cancelled jobs that didn't reach this path.
	if err := s.sweepEphemeralWordlists(ctx); err != nil {
		debug.Error("Failed to sweep ephemeral filtered wordlists: %v", err)
	}

	return nil
}

// sweepEphemeralWordlists deletes ephemeral (job-scoped) filtered wordlists whose
// owning job has reached a terminal state, removing both the file and the row
// (GH #40). It is best-effort and safe to run repeatedly.
func (s *JobExecutionService) sweepEphemeralWordlists(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id, w.file_name
		FROM wordlists w
		JOIN job_executions j ON j.id = w.owner_job_id
		WHERE w.is_ephemeral = true
		  AND j.status IN ('completed', 'failed', 'cancelled')`)
	if err != nil {
		return fmt.Errorf("query ephemeral wordlists: %w", err)
	}
	defer rows.Close()

	type ephemeral struct {
		id       int
		fileName string
	}
	var toDelete []ephemeral
	for rows.Next() {
		var e ephemeral
		if err := rows.Scan(&e.id, &e.fileName); err != nil {
			debug.Error("Failed to scan ephemeral wordlist row: %v", err)
			continue
		}
		toDelete = append(toDelete, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range toDelete {
		filePath := filepath.Join(s.dataDirectory, "wordlists", e.fileName)
		if rmErr := os.Remove(filePath); rmErr != nil && !os.IsNotExist(rmErr) {
			debug.Error("Failed to remove ephemeral wordlist file %s: %v", filePath, rmErr)
		}
		if _, delErr := s.db.ExecContext(ctx, "DELETE FROM wordlist_tags WHERE wordlist_id = $1", e.id); delErr != nil {
			debug.Error("Failed to delete tags for ephemeral wordlist %d: %v", e.id, delErr)
		}
		if _, delErr := s.db.ExecContext(ctx, "DELETE FROM wordlists WHERE id = $1", e.id); delErr != nil {
			debug.Error("Failed to delete ephemeral wordlist row %d: %v", e.id, delErr)
			continue
		}
		debug.Info("Swept ephemeral filtered wordlist %d (%s)", e.id, e.fileName)
	}
	return nil
}

// SweepEphemeralWordlists is the exported wrapper around sweepEphemeralWordlists so
// other callers (e.g. bulk job deletion) can clear stragglers owned by terminal jobs.
func (s *JobExecutionService) SweepEphemeralWordlists(ctx context.Context) error {
	return s.sweepEphemeralWordlists(ctx)
}

// CleanupEphemeralWordlistsForJob removes the file and DB row for every ephemeral
// (job-scoped) filtered wordlist owned by jobID, regardless of the job's status
// (GH #40). This must run on explicit job DELETE *before* the job row is removed:
// the wordlists.owner_job_id FK is ON DELETE CASCADE, so deleting the job first drops
// the wordlist row and leaves its file orphaned on disk with nothing pointing at it
// (sweepEphemeralWordlists keys off owner_job_id and could never find it again).
// Best-effort and safe to call for jobs that own no ephemeral wordlists.
func (s *JobExecutionService) CleanupEphemeralWordlistsForJob(ctx context.Context, jobID uuid.UUID) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_name
		FROM wordlists
		WHERE is_ephemeral = true AND owner_job_id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("query ephemeral wordlists for job %s: %w", jobID, err)
	}
	defer rows.Close()

	type ephemeral struct {
		id       int
		fileName string
	}
	var toDelete []ephemeral
	for rows.Next() {
		var e ephemeral
		if err := rows.Scan(&e.id, &e.fileName); err != nil {
			debug.Error("Failed to scan ephemeral wordlist row for job %s: %v", jobID, err)
			continue
		}
		toDelete = append(toDelete, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range toDelete {
		filePath := filepath.Join(s.dataDirectory, "wordlists", e.fileName)
		if rmErr := os.Remove(filePath); rmErr != nil && !os.IsNotExist(rmErr) {
			debug.Error("Failed to remove ephemeral wordlist file %s: %v", filePath, rmErr)
		}
		if _, delErr := s.db.ExecContext(ctx, "DELETE FROM wordlist_tags WHERE wordlist_id = $1", e.id); delErr != nil {
			debug.Error("Failed to delete tags for ephemeral wordlist %d: %v", e.id, delErr)
		}
		if _, delErr := s.db.ExecContext(ctx, "DELETE FROM wordlists WHERE id = $1", e.id); delErr != nil {
			debug.Error("Failed to delete ephemeral wordlist row %d: %v", e.id, delErr)
			continue
		}
		debug.Info("Deleted ephemeral filtered wordlist %d (%s) on job %s deletion", e.id, e.fileName, jobID)
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
		"task_id":            taskID,
		"increment_layer_id": task.IncrementLayerID,
		"status":             task.Status,
		"job_execution_id":   task.JobExecutionID,
	})

	// Scheduler-v2 cascade: when a v2 task completes normally, mark its
	// keyspace interval 'completed' too. Without this, intervals stay
	// 'assigned' forever for normally-completed tasks (only RecoverTaskByID's
	// truncate path was updating intervals previously). Legacy tasks have
	// no row in job_keyspace_intervals, so the UPDATE is a safe no-op for
	// them. Status guard makes repeat calls safe. Best-effort: log on
	// error rather than failing task completion.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE job_keyspace_intervals
		SET status = 'completed'
		WHERE task_id = $1 AND status IN ('assigned', 'running')
	`, taskID); err != nil {
		debug.Warning("Failed to cascade interval->completed for task %s: %v", taskID, err)
	}

	// Step 11b: cascade scheduling_units.status -> completed when the
	// unit's non-failed/non-cancelled intervals fully cover its
	// base_keyspace. Each layer is its own "job" — once its keyspace is
	// fully covered, the unit transitions to 'completed' and drops out
	// of the scheduler's candidate set. Intervals don't overlap (no_overlap_per_unit
	// constraint), so SUM(range_end - range_start) >= base_keyspace
	// exactly when coverage is complete.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE scheduling_units su
		SET status = 'completed', updated_at = NOW()
		WHERE su.id = (SELECT scheduling_unit_id FROM job_tasks WHERE id = $1)
		  AND su.status IN ('pending', 'running')
		  AND su.base_keyspace IS NOT NULL
		  AND (
			SELECT COALESCE(SUM(range_end - range_start), 0)
			FROM job_keyspace_intervals jki
			WHERE jki.scheduling_unit_id = su.id
			  AND jki.status NOT IN ('failed', 'cancelled')
		  ) >= su.base_keyspace
	`, taskID); err != nil {
		debug.Warning("Failed to cascade unit->completed for task %s: %v", taskID, err)
	}

	// Step 11n: cascade scheduling_units.status -> pending when the
	// unit has REMAINING work (incomplete coverage) AND no other tasks
	// are currently in flight for it. The dispatcher's idempotent
	// pending->running transition (Step 11e) flips it back to running
	// on the next successful dispatch. Without this, the unit would
	// stay 'running' forever after the last task fails/completes if no
	// agent picks it up — misleading UI status.
	//
	// Gated by:
	//   - unit currently 'running' (don't downgrade other states)
	//   - has gaps (coverage < base_keyspace)
	//   - no active tasks (assigned/running/processing) for this unit
	if _, err := s.db.ExecContext(ctx, `
		UPDATE scheduling_units su
		SET status = 'pending', updated_at = NOW()
		WHERE su.id = (SELECT scheduling_unit_id FROM job_tasks WHERE id = $1)
		  AND su.status = 'running'
		  AND su.base_keyspace IS NOT NULL
		  AND (
			SELECT COALESCE(SUM(range_end - range_start), 0)
			FROM job_keyspace_intervals jki
			WHERE jki.scheduling_unit_id = su.id
			  AND jki.status NOT IN ('failed', 'cancelled')
		  ) < su.base_keyspace
		  AND NOT EXISTS (
			SELECT 1 FROM job_tasks t
			WHERE t.scheduling_unit_id = su.id
			  AND t.status IN ('assigned', 'running', 'processing')
		  )
	`, taskID); err != nil {
		debug.Warning("Failed to cascade unit->pending for task %s: %v", taskID, err)
	}

	// Step 11n (cont.): mirror cascade for the increment layer.
	// If its corresponding unit just transitioned to 'pending', the
	// layer should follow. The increment_layer_id is on the task row.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE job_increment_layers l
		SET status = 'pending', updated_at = NOW()
		FROM job_tasks t, scheduling_units su
		WHERE t.id = $1
		  AND t.increment_layer_id = l.id
		  AND su.id = t.scheduling_unit_id
		  AND l.status = 'running'
		  AND su.status = 'pending'
	`, taskID); err != nil {
		debug.Warning("Failed to cascade layer->pending for task %s: %v", taskID, err)
	}

	// Step 11n (cont.): mirror cascade for the parent job. Goes
	// pending only if NO tasks anywhere on this job are active AND
	// not every unit is already completed (which is the completion
	// path handled by JobProgressCalculationService.checkJobsForCompletion).
	//
	// Also accepts 'processing' as a starting state — checkJobProcessingStatus
	// can prematurely flip a v2 job to 'processing' if its v2-aware check
	// somehow fails. Without recovering from 'processing' here, the job
	// would stay stuck even after every task drained.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE job_executions je
		SET status = 'pending', updated_at = NOW()
		WHERE je.id = (SELECT job_execution_id FROM job_tasks WHERE id = $1)
		  AND je.status IN ('running', 'processing')
		  AND NOT EXISTS (
			SELECT 1 FROM job_tasks t
			WHERE t.job_execution_id = je.id
			  AND t.status IN ('assigned', 'running', 'processing')
		  )
		  AND EXISTS (
			-- At least one unit is still incomplete; otherwise the
			-- completion check should mark the JOB completed, not pending.
			SELECT 1 FROM scheduling_units su
			WHERE su.parent_job_id = je.id AND su.status <> 'completed'
		  )
	`, taskID); err != nil {
		debug.Warning("Failed to cascade job->pending for task %s: %v", taskID, err)
	}

	// Self-heal the cached benchmark from this task's observed speed so a
	// pessimistic cold-start benchmark stops permanently under-sizing future
	// tasks for this (agent, hash_type, attack_mode). Non-fatal on error.
	if err := s.updateBenchmarkFromTaskCompletion(ctx, task); err != nil {
		debug.Warning("Benchmark EMA update from task %s failed: %v", taskID, err)
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
				// V2 layer-cascade (Step 11j): use coverage check, not
				// task-failure presence. A v2 task that fails has its
				// interval reverted to a gap by RecoverTaskByID; the gap
				// gets re-issued and a later task fills it. So a single
				// failed task does NOT mean the layer is unrecoverable —
				// what matters is whether the layer's intervals (non-failed,
				// non-cancelled) fully cover its base_keyspace.
				//
				// The OLD `HasFailedLayerTasks → mark layer failed` cascade
				// was v1-era logic (where a failed task was terminal). In v2
				// it incorrectly poisoned layers whose coverage was actually
				// complete via re-attempted tasks. A layer goes 'failed'
				// only via external policy (e.g., AttributeBenchmarkFailure
				// exhausting all eligible agents) — not from transient
				// per-task failures.
				layer, layerErr := s.jobIncrementLayerRepo.GetByID(ctx, *task.IncrementLayerID)
				if layerErr != nil {
					debug.Error("Failed to get layer for completion check: %v", layerErr)
				} else if layer.BaseKeyspace != nil && *layer.BaseKeyspace > 0 {
					var covered int64
					if cErr := s.db.QueryRowContext(ctx, `
						SELECT COALESCE(SUM(jki.range_end - jki.range_start), 0)
						FROM job_keyspace_intervals jki
						JOIN scheduling_units su ON su.id = jki.scheduling_unit_id
						WHERE su.parent_job_id = $1
						  AND su.layer_index = $2
						  AND jki.status NOT IN ('failed', 'cancelled')
					`, task.JobExecutionID, layer.LayerIndex).Scan(&covered); cErr != nil {
						debug.Error("Failed to compute layer coverage: %v", cErr)
					} else if covered < *layer.BaseKeyspace {
						// Gaps remain — leave layer 'running' so the scheduler
						// continues filling. The dispatcher's gap query
						// already excludes failed/cancelled intervals.
						debug.Log("Layer has uncovered range — staying running", map[string]interface{}{
							"layer_id":      *task.IncrementLayerID,
							"base_keyspace": *layer.BaseKeyspace,
							"covered":       covered,
							"remaining":     *layer.BaseKeyspace - covered,
						})
						return nil
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
func (s *JobExecutionService) GetPreviousChunksActualKeyspace(ctx context.Context, jobExecutionID uuid.UUID, currentChunkNumber int) (models.BigInt, error) {
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

// updateBenchmarkFromTaskCompletion applies an exponential moving average to
// the cached agent benchmark speed using this task's observed hashrate. The
// purpose is self-healing: a cold-start benchmark that ran subpar should not
// permanently under-size future tasks for the same (agent, hash_type,
// attack_mode). Non-fatal on any error — this path must never block task
// completion.
//
// Guardrails:
//   - Task must have an assigned agent and a wall time >= 30s.
//   - We derive the observation from task.AverageSpeed when hashcat reported it;
//     otherwise compute base_span (effective_span × base/effective) / wall_time.
//   - Salted hash types are keyed on salt_count = hashlist.total_hashes (the
//     same key the dispatcher's lookupHashTypeAndSalt/readAgentSpeeds uses), so
//     the EMA updates the exact cache row a future dispatch will read. A salted
//     task without a hashcat-reported average speed is skipped (the derived
//     base-span fallback is wrong-unit for salts).
//   - Skip if observed speed is >10x or <0.1x of the cached speed — that
//     indicates a bug or a very short sample, not drift; the huge delta would
//     poison the cache.
func (s *JobExecutionService) updateBenchmarkFromTaskCompletion(
	ctx context.Context,
	task *models.JobTask,
) error {
	if task == nil || task.AgentID == nil {
		return nil
	}
	if task.StartedAt == nil || task.CompletedAt == nil {
		return nil
	}
	wallTime := task.CompletedAt.Sub(*task.StartedAt).Seconds()
	if wallTime < 30 {
		return nil
	}

	jobExec, err := s.GetJobExecutionByID(ctx, task.JobExecutionID)
	if err != nil || jobExec == nil {
		return fmt.Errorf("load job execution: %w", err)
	}

	// Skip salted hash types (see comment above).
	hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
	if err != nil || hashlist == nil {
		return fmt.Errorf("load hashlist: %w", err)
	}
	hashType, err := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
	if err != nil || hashType == nil {
		return fmt.Errorf("load hash_type: %w", err)
	}
	// agent_benchmarks is salt-aware: salted hash types are keyed on
	// salt_count = hashlist.total_hashes (the same key the dispatcher's
	// lookupHashTypeAndSalt/readAgentSpeeds uses), non-salted on NULL. Use that
	// key so the EMA lands on exactly the row a future dispatch will read.
	var saltCount *int
	if hashType.IsSalted && hashlist.TotalHashes > 0 {
		sc := hashlist.TotalHashes
		saltCount = &sc
	}

	// Prefer the task's hashcat-reported average speed (same effective-h/s unit
	// as agent_benchmarks.speed). The derived fallback converts the effective
	// span back to a base candidate rate (effective→base via the keyspace ratio),
	// which matches the cache semantics for rules but NOT for salts, so use it
	// only for non-salted types; a salted task with no reported average is
	// skipped rather than risk a wrong-unit write.
	var observedSpeed int64
	if task.AverageSpeed != nil && *task.AverageSpeed > 0 {
		observedSpeed = *task.AverageSpeed
	} else if !hashType.IsSalted {
		// Derive the agent's base candidate rate from this task's effective span.
		// base_span = effective_span × base_keyspace / effective_keyspace divides
		// out the rule multiplier, computed in big.Int (overflow/precision-safe)
		// rather than via a stored float ratio.
		effSpan := models.NewBigInt(task.KeyspaceEnd - task.KeyspaceStart)
		if task.EffectiveKeyspaceEnd != nil && task.EffectiveKeyspaceStart != nil {
			effSpan = task.EffectiveKeyspaceEnd.Sub(*task.EffectiveKeyspaceStart)
		}
		if !effSpan.IsPositive() {
			return nil
		}
		baseSpan := effSpan
		if jobExec.EffectiveKeyspace != nil && jobExec.EffectiveKeyspace.IsPositive() &&
			jobExec.BaseKeyspace != nil && *jobExec.BaseKeyspace > 0 {
			baseSpan = effSpan.MulInt64(*jobExec.BaseKeyspace).Div(*jobExec.EffectiveKeyspace)
		}
		observedSpeed = int64(float64(baseSpan.Int64()) / wallTime)
	} else {
		return nil
	}
	if observedSpeed <= 0 {
		return nil
	}

	return s.recordObservedSpeed(ctx, *task.AgentID, jobExec.AttackMode, hashType.ID, saltCount, observedSpeed, "task_completion", task.ID)
}

// recordObservedSpeed applies a sanity-checked EMA of observedSpeed into the
// (agent, attack_mode, hash_type, salt_count) benchmark row. Shared by the
// task-completion feedback and the chunk-overrun guard so a stale/optimistic
// benchmark self-heals toward the agent's real sustained speed. Non-fatal.
func (s *JobExecutionService) recordObservedSpeed(
	ctx context.Context,
	agentID int,
	attackMode models.AttackMode,
	hashTypeID int,
	saltCount *int,
	observedSpeed int64,
	source string,
	taskID uuid.UUID,
) error {
	if observedSpeed <= 0 {
		return nil
	}

	// Sanity-check against any current cached value to avoid EMA poisoning
	// from outlier observations.
	cached, err := s.benchmarkRepo.GetAgentBenchmark(ctx, agentID, attackMode, hashTypeID, saltCount)
	if err != nil && err != repository.ErrNotFound {
		return fmt.Errorf("load existing benchmark: %w", err)
	}
	if cached != nil && cached.Speed > 0 {
		ratio := float64(observedSpeed) / float64(cached.Speed)
		if ratio > 10 || ratio < 0.1 {
			debug.Warning(
				"EMA skipped: observed speed %d is %.2fx cached %d (task %s, agent %d, hash_type %d, source %s) — outside sanity window",
				observedSpeed, ratio, cached.Speed, taskID, agentID, hashTypeID, source,
			)
			return nil
		}
	}

	alpha := s.benchmarkObservationAlpha(ctx)
	oldSpeed, newSpeed, err := s.benchmarkRepo.UpdateSpeedEMA(
		ctx, agentID, attackMode, hashTypeID, saltCount, observedSpeed, alpha,
	)
	if err != nil {
		return fmt.Errorf("apply EMA: %w", err)
	}
	debug.Log("Benchmark EMA update", map[string]interface{}{
		"source":         source,
		"task_id":        taskID,
		"agent_id":       agentID,
		"hash_type":      hashTypeID,
		"attack_mode":    int(attackMode),
		"observed_speed": observedSpeed,
		"old_speed":      oldSpeed,
		"new_speed":      newSpeed,
		"alpha":          alpha,
	})
	return nil
}

// RecordRunningTaskObservedSpeed feeds a still-running task's measured speed
// back into the benchmark cache. Used by the chunk-overrun guard: when a task
// is stopped for exceeding its chunk-time budget, recording the agent's real
// (slower) speed means the re-dispatched remainder is sized to fit and does not
// immediately overrun again. Non-fatal — must never block the guard.
func (s *JobExecutionService) RecordRunningTaskObservedSpeed(ctx context.Context, task *models.JobTask) error {
	if task == nil || task.AgentID == nil {
		return nil
	}
	// The progress handler maintains job_tasks.benchmark_speed as a live EWMA of
	// the agent's reported hashrate; prefer it, then any computed average.
	var observed int64
	if task.BenchmarkSpeed != nil && *task.BenchmarkSpeed > 0 {
		observed = *task.BenchmarkSpeed
	} else if task.AverageSpeed != nil && *task.AverageSpeed > 0 {
		observed = *task.AverageSpeed
	}
	if observed <= 0 {
		return nil
	}

	jobExec, err := s.GetJobExecutionByID(ctx, task.JobExecutionID)
	if err != nil || jobExec == nil {
		return fmt.Errorf("load job execution: %w", err)
	}
	hashlist, err := s.hashlistRepo.GetByID(ctx, jobExec.HashlistID)
	if err != nil || hashlist == nil {
		return fmt.Errorf("load hashlist: %w", err)
	}
	hashType, err := s.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
	if err != nil || hashType == nil {
		return fmt.Errorf("load hash_type: %w", err)
	}
	var saltCount *int
	if hashType.IsSalted && hashlist.TotalHashes > 0 {
		sc := hashlist.TotalHashes
		saltCount = &sc
	}
	return s.recordObservedSpeed(ctx, *task.AgentID, jobExec.AttackMode, hashType.ID, saltCount, observed, "chunk_overrun", task.ID)
}

// benchmarkObservationAlpha returns the EMA weight for a single observation.
// Default 0.3 biases toward the existing cached value while allowing drift.
func (s *JobExecutionService) benchmarkObservationAlpha(ctx context.Context) float64 {
	const defaultAlpha = 0.3
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "benchmark_observation_ema_alpha")
	if err != nil || setting == nil || setting.Value == nil {
		return defaultAlpha
	}
	if f, err := strconv.ParseFloat(*setting.Value, 64); err == nil && f > 0 && f <= 1 {
		return f
	}
	return defaultAlpha
}
