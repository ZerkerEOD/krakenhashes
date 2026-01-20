package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/utils"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// AdminPresetJobService defines the interface for managing preset jobs.
type AdminPresetJobService interface {
	CreatePresetJob(ctx context.Context, params models.PresetJob) (*models.PresetJob, error)
	GetPresetJobByID(ctx context.Context, id uuid.UUID) (*models.PresetJob, error)
	ListPresetJobs(ctx context.Context) ([]models.PresetJob, error)
	UpdatePresetJob(ctx context.Context, id uuid.UUID, params models.PresetJob) (*models.PresetJob, error)
	DeletePresetJob(ctx context.Context, id uuid.UUID) error
	GetPresetJobFormData(ctx context.Context) (*repository.PresetJobFormData, error)
	CalculateKeyspaceForPresetJob(ctx context.Context, presetJob *models.PresetJob) (*int64, error)
	RecalculateKeyspacesForWordlist(ctx context.Context, wordlistID string) error
	RecalculateKeyspacesForRule(ctx context.Context, ruleID string) error
}

// adminPresetJobService implements AdminPresetJobService.
type adminPresetJobService struct {
	presetJobRepo            repository.PresetJobRepository
	presetIncrementLayerRepo *repository.PresetIncrementLayerRepository
	systemSettingsRepo       *repository.SystemSettingsRepository
	binaryManager            binary.Manager
	fileRepo                 *repository.FileRepository
	dataDirectory            string
}

// NewAdminPresetJobService creates a new service for managing preset jobs.
func NewAdminPresetJobService(
	presetJobRepo repository.PresetJobRepository,
	presetIncrementLayerRepo *repository.PresetIncrementLayerRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	binaryManager binary.Manager,
	fileRepo *repository.FileRepository,
	dataDirectory string,
) AdminPresetJobService {
	return &adminPresetJobService{
		presetJobRepo:            presetJobRepo,
		presetIncrementLayerRepo: presetIncrementLayerRepo,
		systemSettingsRepo:       systemSettingsRepo,
		binaryManager:            binaryManager,
		fileRepo:                 fileRepo,
		dataDirectory:            dataDirectory,
	}
}

// presetJobBinaryStoreAdapter adapts binary.Manager to version.BinaryStore interface
type presetJobBinaryStoreAdapter struct {
	manager binary.Manager
}

func (a *presetJobBinaryStoreAdapter) ListActive(ctx context.Context) ([]version.BinaryInfo, error) {
	versions, err := a.manager.ListVersions(ctx, map[string]interface{}{"is_active": true})
	if err != nil {
		return nil, err
	}

	result := make([]version.BinaryInfo, 0, len(versions))
	for _, v := range versions {
		if v.Version == nil {
			continue
		}
		result = append(result, version.BinaryInfo{
			ID:        v.ID,
			Version:   *v.Version,
			IsDefault: v.IsDefault,
			IsActive:  v.IsActive,
		})
	}
	return result, nil
}

func (a *presetJobBinaryStoreAdapter) GetDefault(ctx context.Context) (*version.BinaryInfo, error) {
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
func (s *adminPresetJobService) resolveBinaryVersionPattern(ctx context.Context, pattern string) (int64, error) {
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
	adapter := &presetJobBinaryStoreAdapter{manager: s.binaryManager}
	resolver := version.NewResolver(adapter)

	parsedPattern, err := version.Parse(pattern)
	if err != nil {
		return 0, fmt.Errorf("invalid version pattern %q: %w", pattern, err)
	}

	matching, err := resolver.GetMatchingBinaries(ctx, parsedPattern)
	if err != nil {
		return 0, fmt.Errorf("failed to find matching binaries: %w", err)
	}

	if len(matching) == 0 {
		return 0, fmt.Errorf("no binary matches pattern %q", pattern)
	}

	for _, b := range matching {
		if b.IsDefault {
			return b.ID, nil
		}
	}

	return matching[0].ID, nil
}

// isValidAttackMode checks if the provided integer corresponds to a defined AttackMode.
func isValidAttackMode(mode models.AttackMode) bool {
	switch mode {
	case models.AttackModeStraight,
		models.AttackModeCombination,
		models.AttackModeBruteForce,
		models.AttackModeHybridWordlistMask,
		models.AttackModeHybridMaskWordlist,
		models.AttackModeAssociation:
		return true
	default:
		return false
	}
}

// validatePresetJob performs input validation for create/update operations.
func (s *adminPresetJobService) validatePresetJob(ctx context.Context, params models.PresetJob, isUpdate bool, existingID uuid.UUID) error {
	if params.Name == "" {
		return errors.New("preset job name cannot be empty")
	}

	// Check name uniqueness
	existingByName, err := s.presetJobRepo.GetByName(ctx, params.Name)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		// Error other than not found
		return fmt.Errorf("error checking name uniqueness: %w", err)
	}
	if existingByName != nil && (!isUpdate || existingByName.ID != existingID) {
		return fmt.Errorf("preset job name '%s' already exists", params.Name)
	}

	if params.Priority < 0 {
		return errors.New("priority cannot be negative")
	}

	// Check priority against system maximum
	maxPriority, err := s.systemSettingsRepo.GetMaxJobPriority(ctx)
	if err != nil {
		debug.Warning("Failed to get max priority setting, using default: %v", err)
		maxPriority = 1000 // Default fallback
	}

	if params.Priority > maxPriority {
		return fmt.Errorf("priority %d exceeds the maximum allowed priority of %d", params.Priority, maxPriority)
	}

	if params.ChunkSizeSeconds <= 0 {
		return errors.New("chunk size must be positive")
	}

	if !isValidAttackMode(params.AttackMode) {
		return fmt.Errorf("invalid attack mode: %d", params.AttackMode)
	}

	// Basic validation for ID strings in arrays (ensures they are valid integers)
	for _, idStr := range params.WordlistIDs {
		// Wordlist IDs are numeric IDs stored as strings in the database
		// Check that they can be parsed as integers
		if _, err := strconv.Atoi(idStr); err != nil {
			return fmt.Errorf("invalid wordlist ID format: %s", idStr)
		}
	}
	for _, idStr := range params.RuleIDs {
		// Rule IDs are numeric IDs stored as strings in the database
		// Check that they can be parsed as integers
		if _, err := strconv.Atoi(idStr); err != nil {
			return fmt.Errorf("invalid rule ID format: %s", idStr)
		}
	}

	// Attack mode specific validation
	switch params.AttackMode {
	case models.AttackModeStraight:
		if len(params.WordlistIDs) != 1 {
			return errors.New("straight attack mode requires exactly one wordlist")
		}
		// Rules are optional, but limited to at most 1 (backend scheduling only handles single rule file)
		if len(params.RuleIDs) > 1 {
			return errors.New("straight attack mode supports at most one rule file")
		}

	case models.AttackModeCombination:
		if len(params.WordlistIDs) != 2 {
			return errors.New("combination attack mode requires exactly two wordlists")
		}
		if len(params.RuleIDs) > 0 {
			return errors.New("rules are not supported in combination attack mode")
		}

	case models.AttackModeBruteForce:
		if len(params.WordlistIDs) > 0 {
			return errors.New("wordlists are not used in brute force attack mode")
		}
		if len(params.RuleIDs) > 0 {
			return errors.New("rules are not supported in brute force attack mode")
		}
		if params.Mask == "" {
			return errors.New("mask is required for brute force attack mode")
		}
		if !validateMaskPattern(params.Mask) {
			return errors.New("invalid mask pattern format")
		}

	case models.AttackModeHybridWordlistMask, models.AttackModeHybridMaskWordlist:
		if len(params.WordlistIDs) != 1 {
			return errors.New("hybrid attack modes require exactly one wordlist")
		}
		if len(params.RuleIDs) > 0 {
			return errors.New("rules are not supported in hybrid attack modes")
		}
		if params.Mask == "" {
			return errors.New("mask is required for hybrid attack modes")
		}
		if !validateMaskPattern(params.Mask) {
			return errors.New("invalid mask pattern format")
		}

	case models.AttackModeAssociation:
		return errors.New("association attack mode is not currently implemented")
	}

	// TODO: Add deeper validation if necessary:
	// - Check if BinaryVersion actually exists in binary_versions table.
	// - Check if all WordlistIDs/RuleIDs exist (might require fetching all valid IDs).
	//   For now, we rely on the frontend using data from GetPresetJobFormData
	//   and potentially database foreign key constraints where applicable.

	return nil
}

// validateMaskPattern validates that the mask follows the expected pattern for hashcat.
// Simple validation to check for valid character sets: ?u, ?l, ?d, ?s, ?a, ?b
// and length requirements.
func validateMaskPattern(mask string) bool {
	if mask == "" {
		return false
	}

	// Pattern should consist of character set specifiers
	// Each valid specifier is two characters: ? followed by a character class
	validSpecifiers := map[string]bool{
		"?u": true, // uppercase
		"?l": true, // lowercase
		"?d": true, // digit
		"?s": true, // special
		"?a": true, // all (uppercase, lowercase, digit, special)
		"?b": true, // binary (0x00 - 0xff)
		"?h": true, // lowercase hex
		"?H": true, // uppercase hex
	}

	i := 0
	for i < len(mask) {
		// If we encounter a literal character (not part of a specifier)
		if mask[i] != '?' {
			i++
			continue
		}

		// Check if we have enough characters for a complete specifier
		if i+1 >= len(mask) {
			return false // Incomplete specifier at end of mask
		}

		// Check if the specifier is valid
		specifier := mask[i : i+2]
		if !validSpecifiers[specifier] {
			return false // Invalid specifier
		}

		i += 2 // Move past this specifier
	}

	// Ensure mask isn't empty after validation
	return true
}

// CreatePresetJob creates a new preset job after validation.
func (s *adminPresetJobService) CreatePresetJob(ctx context.Context, params models.PresetJob) (*models.PresetJob, error) {
	// Set default values if not provided
	if params.ChunkSizeSeconds == 0 {
		params.ChunkSizeSeconds = 300 // 5 minutes default
	}
	if params.Priority == 0 {
		params.Priority = 10 // Default priority
	}
	// StatusUpdatesEnabled defaults to false if not set
	// IsSmallJob defaults to false if not set
	// AllowHighPriorityOverride defaults to false if not set
	// MaxAgents defaults to 0 (unlimited) if not set

	if err := s.validatePresetJob(ctx, params, false, uuid.Nil); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	debug.Info("Creating preset job")

	// Calculate keyspace for the preset job
	// For increment mode jobs, this will be updated after layer initialization
	keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &params)
	if err != nil {
		debug.Error("Failed to calculate keyspace for preset job: %v", err)
		return nil, fmt.Errorf("failed to calculate keyspace: %w", err)
	}
	if keyspace == nil {
		debug.Error("Keyspace calculation returned nil for preset job")
		return nil, fmt.Errorf("keyspace calculation failed: no keyspace value returned")
	}
	params.Keyspace = keyspace

	createdJob, err := s.presetJobRepo.Create(ctx, params)
	if err != nil {
		debug.Error("Failed to create preset job in repository: %v", err)
		// TODO: Handle specific DB errors like unique constraint violations more gracefully
		return nil, fmt.Errorf("failed to create preset job: %w", err)
	}

	// Initialize increment layers if increment mode is enabled
	if s.hasIncrementMode(&params) {
		totalKeyspace, err := s.initializePresetIncrementLayers(ctx, createdJob)
		if err != nil {
			// Log but don't fail - the preset is created, layers can be recalculated later
			debug.Warning("Failed to initialize increment layers for preset job %s: %v", createdJob.ID, err)
		} else {
			// Update the keyspace with the sum of all layer keyspaces
			createdJob.Keyspace = &totalKeyspace
			_, updateErr := s.presetJobRepo.Update(ctx, createdJob.ID, *createdJob)
			if updateErr != nil {
				debug.Warning("Failed to update keyspace after layer initialization: %v", updateErr)
			}
		}
	}

	debug.Info("Successfully created preset job ID: %s with keyspace: %v", createdJob.ID, createdJob.Keyspace)
	return createdJob, nil
}

// GetPresetJobByID retrieves a single preset job.
func (s *adminPresetJobService) GetPresetJobByID(ctx context.Context, id uuid.UUID) (*models.PresetJob, error) {
	debug.Debug("Getting preset job by ID: %s", id)
	job, err := s.presetJobRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			debug.Warning("Preset job not found: %s", id)
			return nil, err // Return the specific ErrNotFound
		}
		debug.Error("Failed to get preset job by ID %s: %v", id, err)
		return nil, fmt.Errorf("failed to get preset job: %w", err)
	}
	return job, nil
}

// ListPresetJobs retrieves all preset jobs.
func (s *adminPresetJobService) ListPresetJobs(ctx context.Context) ([]models.PresetJob, error) {
	debug.Debug("Listing all preset jobs")
	jobs, err := s.presetJobRepo.List(ctx)
	if err != nil {
		debug.Error("Failed to list preset jobs: %v", err)
		return nil, fmt.Errorf("failed to list preset jobs: %w", err)
	}
	return jobs, nil
}

// UpdatePresetJob updates an existing preset job after validation.
func (s *adminPresetJobService) UpdatePresetJob(ctx context.Context, id uuid.UUID, params models.PresetJob) (*models.PresetJob, error) {
	// Ensure the job exists before validating/updating
	existingJob, err := s.GetPresetJobByID(ctx, id)
	if err != nil {
		return nil, err // Returns ErrNotFound if applicable
	}

	if err := s.validatePresetJob(ctx, params, true, id); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	debug.Info("Updating preset job ID: %s", id)

	// Check if increment settings changed (requires re-initializing layers)
	incrementSettingsChanged := s.incrementSettingsChanged(existingJob, &params)

	// Check if keyspace was explicitly provided (from recalculation endpoint)
	if params.Keyspace != nil {
		// Keyspace was explicitly set, use it
		// But also keep other keyspace-related fields if not set
		if params.EffectiveKeyspace == nil {
			params.EffectiveKeyspace = existingJob.EffectiveKeyspace
		}
		if !params.IsAccurateKeyspace && existingJob.IsAccurateKeyspace {
			params.IsAccurateKeyspace = existingJob.IsAccurateKeyspace
		}
		if !params.UseRuleSplitting && existingJob.UseRuleSplitting {
			params.UseRuleSplitting = existingJob.UseRuleSplitting
		}
		if params.MultiplicationFactor == 0 {
			params.MultiplicationFactor = existingJob.MultiplicationFactor
		}
		debug.Info("Using explicitly provided keyspace for preset job %s: %v", id, params.Keyspace)
	} else if s.needsKeyspaceRecalculation(existingJob, &params) || incrementSettingsChanged {
		// Recalculate keyspace - CalculateKeyspaceForPresetJob sets all fields on params
		keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &params)
		if err != nil {
			// Log the error but don't fail update - keyspace can be calculated later
			debug.Warning("Failed to calculate keyspace for preset job: %v", err)
		}
		params.Keyspace = keyspace
	} else {
		// Keep ALL existing keyspace-related fields if no changes affecting them
		params.Keyspace = existingJob.Keyspace
		params.EffectiveKeyspace = existingJob.EffectiveKeyspace
		params.IsAccurateKeyspace = existingJob.IsAccurateKeyspace
		params.UseRuleSplitting = existingJob.UseRuleSplitting
		params.MultiplicationFactor = existingJob.MultiplicationFactor
	}

	updatedJob, err := s.presetJobRepo.Update(ctx, id, params)
	if err != nil {
		debug.Error("Failed to update preset job %s: %v", id, err)
		// TODO: Handle specific DB errors
		return nil, fmt.Errorf("failed to update preset job: %w", err)
	}

	// Re-initialize increment layers if increment settings changed
	if incrementSettingsChanged {
		// Delete existing layers
		if err := s.presetIncrementLayerRepo.DeleteByPresetJobID(ctx, id); err != nil {
			debug.Warning("Failed to delete existing preset increment layers: %v", err)
		}

		// Create new layers if increment mode is now enabled
		if s.hasIncrementMode(&params) {
			totalKeyspace, err := s.initializePresetIncrementLayers(ctx, updatedJob)
			if err != nil {
				debug.Warning("Failed to re-initialize increment layers for preset job %s: %v", id, err)
			} else {
				// Update the keyspace with the sum of all layer keyspaces
				updatedJob.Keyspace = &totalKeyspace
				_, updateErr := s.presetJobRepo.Update(ctx, id, *updatedJob)
				if updateErr != nil {
					debug.Warning("Failed to update keyspace after layer re-initialization: %v", updateErr)
				}
			}
		}
	}

	debug.Info("Successfully updated preset job ID: %s with keyspace: %v", updatedJob.ID, updatedJob.Keyspace)
	return updatedJob, nil
}

// DeletePresetJob deletes a preset job.
func (s *adminPresetJobService) DeletePresetJob(ctx context.Context, id uuid.UUID) error {
	debug.Info("Deleting preset job ID: %s", id)
	// Check existence first (optional, repo delete also checks)
	_, err := s.GetPresetJobByID(ctx, id)
	if err != nil {
		return err
	}

	err = s.presetJobRepo.Delete(ctx, id)
	if err != nil {
		debug.Error("Failed to delete preset job %s: %v", id, err)
		// Consider if FK constraints could cause errors here if not handled by DB
		return fmt.Errorf("failed to delete preset job: %w", err)
	}
	debug.Info("Successfully deleted preset job ID: %s", id)
	return nil
}

// GetPresetJobFormData retrieves lists needed for UI forms.
func (s *adminPresetJobService) GetPresetJobFormData(ctx context.Context) (*repository.PresetJobFormData, error) {
	debug.Debug("Getting preset job form data")
	formData, err := s.presetJobRepo.ListFormData(ctx)
	if err != nil {
		debug.Error("Failed to get preset job form data: %v", err)
		return nil, fmt.Errorf("failed to get preset job form data: %w", err)
	}
	return formData, nil
}

// needsKeyspaceRecalculation checks if any fields that affect keyspace calculation have changed
func (s *adminPresetJobService) needsKeyspaceRecalculation(existing, updated *models.PresetJob) bool {
	// Check if attack mode changed
	if existing.AttackMode != updated.AttackMode {
		return true
	}

	// Check if wordlists changed
	if len(existing.WordlistIDs) != len(updated.WordlistIDs) {
		return true
	}
	for i, id := range existing.WordlistIDs {
		if i >= len(updated.WordlistIDs) || id != updated.WordlistIDs[i] {
			return true
		}
	}

	// Check if rules changed (only affects straight mode)
	if existing.AttackMode == models.AttackModeStraight {
		if len(existing.RuleIDs) != len(updated.RuleIDs) {
			return true
		}
		for i, id := range existing.RuleIDs {
			if i >= len(updated.RuleIDs) || id != updated.RuleIDs[i] {
				return true
			}
		}
	}

	// Check if mask changed (for mask-based modes)
	if existing.Mask != updated.Mask {
		return true
	}

	// Check if binary version changed
	if existing.BinaryVersion != updated.BinaryVersion {
		return true
	}

	return false
}

// CalculateKeyspaceForPresetJob calculates the total keyspace for a preset job using hashcat --keyspace
func (s *adminPresetJobService) CalculateKeyspaceForPresetJob(ctx context.Context, presetJob *models.PresetJob) (*int64, error) {
	debug.Log("Starting keyspace calculation for preset job", map[string]interface{}{
		"preset_job_id":    presetJob.ID,
		"binary_version":   presetJob.BinaryVersion,
		"attack_mode":      presetJob.AttackMode,
		"data_directory":   s.dataDirectory,
	})

	// Resolve binary version pattern to actual binary ID
	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, presetJob.BinaryVersion)
	if err != nil {
		debug.Error("Failed to resolve binary version pattern: pattern=%s, error=%v",
			presetJob.BinaryVersion, err)
		return nil, fmt.Errorf("failed to resolve binary version pattern %q: %w", presetJob.BinaryVersion, err)
	}

	// Get the hashcat binary path from binary manager
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		debug.Error("Failed to get hashcat binary path: binary_version_id=%d, error=%v",
			binaryVersionID, err)
		return nil, fmt.Errorf("failed to get hashcat binary path for version %d: %w", binaryVersionID, err)
	}
	
	// Verify the binary exists and is executable
	if fileInfo, err := os.Stat(hashcatPath); err != nil {
		debug.Error("Hashcat binary not found: path=%s, error=%v", hashcatPath, err)
		return nil, fmt.Errorf("hashcat binary not found at %s: %w", hashcatPath, err)
	} else {
		debug.Log("Found hashcat binary", map[string]interface{}{
			"path": hashcatPath,
			"size": fileInfo.Size(),
			"mode": fileInfo.Mode().String(),
		})
	}

	// Build hashcat command for keyspace calculation
	var args []string

	// Add attack mode flag
	args = append(args, "-a", fmt.Sprintf("%d", presetJob.AttackMode))

	// Add attack-specific arguments
	switch presetJob.AttackMode {
	case models.AttackModeStraight: // Dictionary attack (-a 0)
		for _, wordlistIDStr := range presetJob.WordlistIDs {
			wordlistPath, err := s.resolveWordlistPath(ctx, wordlistIDStr)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath)
		}
		// Add rules if any
		for _, ruleIDStr := range presetJob.RuleIDs {
			rulePath, err := s.resolveRulePath(ctx, ruleIDStr)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve rule path: %w", err)
			}
			args = append(args, "-r", rulePath)
		}

	case models.AttackModeCombination: // Combinator attack
		if len(presetJob.WordlistIDs) >= 2 {
			wordlist1Path, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
			if err != nil {
				return nil, fmt.Errorf("failed to resolve wordlist1 path: %w", err)
			}
			wordlist2Path, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[1])
			if err != nil {
				return nil, fmt.Errorf("failed to resolve wordlist2 path: %w", err)
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
				return nil, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, wordlistPath, presetJob.Mask)
		}

	case models.AttackModeHybridMaskWordlist: // Hybrid Mask + Wordlist
		if presetJob.Mask != "" && len(presetJob.WordlistIDs) > 0 {
			wordlistPath, err := s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
			if err != nil {
				return nil, fmt.Errorf("failed to resolve wordlist path: %w", err)
			}
			args = append(args, presetJob.Mask, wordlistPath)
		}

	default:
		return nil, fmt.Errorf("unsupported attack mode for keyspace calculation: %d", presetJob.AttackMode)
	}

	// Save base args for reuse with --total-candidates
	baseArgs := make([]string, len(args))
	copy(baseArgs, args)

	// Add keyspace flag
	args = append(args, "--keyspace")

	// Add --restore-disable to prevent creating restore files for keyspace calculation
	args = append(args, "--restore-disable")

	// Add a unique session ID to allow concurrent executions
	sessionID := fmt.Sprintf("keyspace_%s_%d", presetJob.ID, time.Now().UnixNano())
	args = append(args, "--session", sessionID)

	// Add --quiet flag to suppress unnecessary output
	args = append(args, "--quiet")

	debug.Log("Calculating keyspace for preset job", map[string]interface{}{
		"preset_job_id":  presetJob.ID,
		"command":        hashcatPath,
		"args":           args,
		"attack_mode":    presetJob.AttackMode,
		"session_id":     sessionID,
		"working_dir":    s.dataDirectory,
		"full_command":   fmt.Sprintf("%s %s", hashcatPath, strings.Join(args, " ")),
	})

	// Execute hashcat command with timeout
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Set working directory to data directory to ensure session files are created there
	cmd := exec.CommandContext(ctx, hashcatPath, args...)
	cmd.Dir = s.dataDirectory
	
	// Log environment
	debug.Log("Executing hashcat command", map[string]interface{}{
		"working_directory": cmd.Dir,
		"path_env":         os.Getenv("PATH"),
	})

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Clean up session files regardless of success/failure
	// Note: With --restore-disable, .restore files won't be created
	sessionFiles := []string{
		filepath.Join(s.dataDirectory, sessionID+".log"),
		filepath.Join(s.dataDirectory, sessionID+".potfile"),
		filepath.Join(s.dataDirectory, sessionID+".induct"),
		filepath.Join(s.dataDirectory, sessionID+".outfile"),
		// Also check in binary directory in case hashcat creates files there
		filepath.Join(filepath.Dir(hashcatPath), sessionID+".log"),
		filepath.Join(filepath.Dir(hashcatPath), sessionID+".potfile"),
	}
	for _, file := range sessionFiles {
		_ = os.Remove(file) // Ignore errors for non-existent files
	}

	if err != nil {
		// Check for specific error conditions
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "Already an instance") {
			// This shouldn't happen with unique sessions, but handle it gracefully
			return nil, fmt.Errorf("hashcat instance conflict (this should not happen with session IDs): %s", stderrStr)
		}

		debug.Error("Hashcat keyspace calculation failed: error=%v, exit_code=%d, stdout=%s, stderr=%s, command=%s, args=%v, session_id=%s, working_dir=%s",
			err, cmd.ProcessState.ExitCode(), stdout.String(), stderrStr, hashcatPath, args, sessionID, cmd.Dir)
		return nil, fmt.Errorf("hashcat keyspace calculation failed (exit code %d): %w\nstderr: %s\nstdout: %s", 
			cmd.ProcessState.ExitCode(), err, stderrStr, stdout.String())
	}

	// Parse keyspace from output
	outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(outputLines) == 0 {
		return nil, fmt.Errorf("no output from hashcat keyspace calculation")
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
		return nil, fmt.Errorf("failed to parse keyspace '%s': %w", keyspaceStr, err)
	}

	if keyspace <= 0 {
		return nil, fmt.Errorf("invalid keyspace: %d", keyspace)
	}

	debug.Log("Base keyspace calculated successfully", map[string]interface{}{
		"preset_job_id": presetJob.ID,
		"keyspace":      keyspace,
		"session_id":    sessionID,
		"stdout_lines":  len(outputLines),
		"keyspace_str":  keyspaceStr,
	})

	// Step 2: Calculate effective keyspace using --total-candidates
	// This accounts for rule effectiveness and gives the true candidate count
	effectiveKeyspace, isAccurate, err := s.calculateTotalCandidates(ctx, hashcatPath, baseArgs, presetJob.ID.String())
	if err != nil {
		// Error is unexpected - log and fall back to estimation
		debug.Warning("Error calculating total candidates: %v, falling back to estimation", err)
		isAccurate = false
	}

	if isAccurate && effectiveKeyspace > 0 {
		// Use accurate value from --total-candidates
		presetJob.EffectiveKeyspace = &effectiveKeyspace
		presetJob.IsAccurateKeyspace = true

		// Calculate multiplication factor from accurate keyspace
		if keyspace > 0 {
			presetJob.MultiplicationFactor = int(effectiveKeyspace / keyspace)
			if presetJob.MultiplicationFactor < 1 {
				presetJob.MultiplicationFactor = 1
			}
		} else {
			presetJob.MultiplicationFactor = 1
		}

		debug.Log("Using accurate effective keyspace from --total-candidates", map[string]interface{}{
			"preset_job_id":         presetJob.ID,
			"base_keyspace":         keyspace,
			"effective_keyspace":    effectiveKeyspace,
			"multiplication_factor": presetJob.MultiplicationFactor,
		})
	} else {
		// Fall back to estimation: base * rule_count
		var estimatedEffective int64 = keyspace
		if len(presetJob.RuleIDs) > 0 {
			// For estimation, assume each rule file has approximately 1 rule on average
			// This is conservative - actual count may vary significantly
			// With accurate keyspace, this won't be used anyway
			ruleCount := int64(len(presetJob.RuleIDs))
			if ruleCount > 0 {
				estimatedEffective = keyspace * ruleCount
			}
			presetJob.MultiplicationFactor = int(ruleCount)
		} else {
			presetJob.MultiplicationFactor = 1
		}
		presetJob.EffectiveKeyspace = &estimatedEffective
		presetJob.IsAccurateKeyspace = false

		debug.Log("Using estimated effective keyspace (--total-candidates failed or unavailable)", map[string]interface{}{
			"preset_job_id":         presetJob.ID,
			"base_keyspace":         keyspace,
			"estimated_effective":   estimatedEffective,
			"rule_count":            len(presetJob.RuleIDs),
			"multiplication_factor": presetJob.MultiplicationFactor,
		})
	}

	// Step 3: Determine if rule splitting should be used
	// Rule splitting is beneficial when:
	// 1. We have rules (attack mode 0 with rules)
	// 2. The effective keyspace is large enough to benefit from splitting
	// For now, set to true if we have rules and accurate keyspace
	presetJob.UseRuleSplitting = len(presetJob.RuleIDs) > 0 && presetJob.IsAccurateKeyspace

	debug.Log("Keyspace calculation complete", map[string]interface{}{
		"preset_job_id":         presetJob.ID,
		"base_keyspace":         keyspace,
		"effective_keyspace":    presetJob.EffectiveKeyspace,
		"is_accurate_keyspace":  presetJob.IsAccurateKeyspace,
		"use_rule_splitting":    presetJob.UseRuleSplitting,
		"multiplication_factor": presetJob.MultiplicationFactor,
	})

	return &keyspace, nil
}

// resolveWordlistPath resolves the full path for a wordlist ID
func (s *adminPresetJobService) resolveWordlistPath(ctx context.Context, wordlistIDStr string) (string, error) {
	debug.Log("Resolving wordlist path", map[string]interface{}{
		"wordlist_id_str": wordlistIDStr,
		"data_directory":  s.dataDirectory,
	})
	
	wordlistID, err := strconv.ParseInt(wordlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid wordlist ID format: wordlist_id=%s, error=%v", wordlistIDStr, err)
		return "", fmt.Errorf("invalid wordlist ID: %s", wordlistIDStr)
	}

	// Look up wordlist in database
	wordlists, err := s.fileRepo.GetWordlists(ctx, "")
	if err != nil {
		debug.Error("Failed to get wordlists from database: error=%v", err)
		return "", fmt.Errorf("failed to get wordlists: %w", err)
	}

	for _, wl := range wordlists {
		if wl.ID == int(wordlistID) {
			// The Name field already contains the relative path from wordlists directory
			// e.g., "general/crackstation.txt"
			path := filepath.Join(s.dataDirectory, "wordlists", wl.Name)

			debug.Log("Found wordlist in database", map[string]interface{}{
				"wordlist_id": wordlistID,
				"category":    wl.Category,
				"name_field":  wl.Name,
				"path":        path,
			})
			
			// Verify the file exists
			if fileInfo, err := os.Stat(path); err != nil {
				debug.Error("Wordlist file not found: path=%s, error=%v", path, err)
				return "", fmt.Errorf("wordlist file not found at %s: %w", path, err)
			} else {
				debug.Log("Wordlist file verified", map[string]interface{}{
					"path": path,
					"size": fileInfo.Size(),
				})
			}

			return path, nil
		}
	}

	return "", fmt.Errorf("wordlist with ID %d not found", wordlistID)
}

// resolveRulePath resolves the full path for a rule ID
func (s *adminPresetJobService) resolveRulePath(ctx context.Context, ruleIDStr string) (string, error) {
	debug.Log("Resolving rule path", map[string]interface{}{
		"rule_id_str":    ruleIDStr,
		"data_directory": s.dataDirectory,
	})
	
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid rule ID format: rule_id=%s, error=%v", ruleIDStr, err)
		return "", fmt.Errorf("invalid rule ID: %s", ruleIDStr)
	}

	// Look up rule in database
	rules, err := s.fileRepo.GetRules(ctx, "")
	if err != nil {
		debug.Error("Failed to get rules from database: error=%v", err)
		return "", fmt.Errorf("failed to get rules: %w", err)
	}

	for _, rule := range rules {
		if rule.ID == int(ruleID) {
			// The Name field already contains the relative path from rules directory
			// e.g., "hashcat/_nsakey.v2.dive.rule"
			path := filepath.Join(s.dataDirectory, "rules", rule.Name)

			debug.Log("Found rule in database", map[string]interface{}{
				"rule_id":    ruleID,
				"category":   rule.Category,
				"name_field": rule.Name,
				"path":       path,
			})
			
			// Verify the file exists
			if fileInfo, err := os.Stat(path); err != nil {
				debug.Error("Rule file not found: path=%s, error=%v", path, err)
				return "", fmt.Errorf("rule file not found at %s: %w", path, err)
			} else {
				debug.Log("Rule file verified", map[string]interface{}{
					"path": path,
					"size": fileInfo.Size(),
				})
			}

			return path, nil
		}
	}

	return "", fmt.Errorf("rule with ID %d not found", ruleID)
}

// RecalculateKeyspacesForWordlist recalculates keyspaces for all preset jobs using the specified wordlist
func (s *adminPresetJobService) RecalculateKeyspacesForWordlist(ctx context.Context, wordlistID string) error {
	// Get all preset jobs that use this wordlist
	allJobs, err := s.presetJobRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list preset jobs: %w", err)
	}

	for _, job := range allJobs {
		// Check if this job uses the wordlist
		usesWordlist := false
		for _, wID := range job.WordlistIDs {
			if wID == wordlistID {
				usesWordlist = true
				break
			}
		}

		if usesWordlist {
			// Recalculate keyspace
			keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &job)
			if err != nil {
				debug.Warning("Failed to recalculate keyspace for preset job %s: %v", job.ID, err)
				continue
			}

			// Update the job with new keyspace
			job.Keyspace = keyspace
			_, err = s.presetJobRepo.Update(ctx, job.ID, job)
			if err != nil {
				debug.Warning("Failed to update keyspace for preset job %s: %v", job.ID, err)
			}
		}
	}

	return nil
}

// RecalculateKeyspacesForRule recalculates keyspaces for all preset jobs using the specified rule
func (s *adminPresetJobService) RecalculateKeyspacesForRule(ctx context.Context, ruleID string) error {
	// Get all preset jobs that use this rule
	allJobs, err := s.presetJobRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list preset jobs: %w", err)
	}

	for _, job := range allJobs {
		// Check if this job uses the rule (only straight mode uses rules)
		if job.AttackMode != models.AttackModeStraight {
			continue
		}

		usesRule := false
		for _, rID := range job.RuleIDs {
			if rID == ruleID {
				usesRule = true
				break
			}
		}

		if usesRule {
			// Recalculate keyspace
			keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &job)
			if err != nil {
				debug.Warning("Failed to recalculate keyspace for preset job %s: %v", job.ID, err)
				continue
			}

			// Update the job with new keyspace
			job.Keyspace = keyspace
			_, err = s.presetJobRepo.Update(ctx, job.ID, job)
			if err != nil {
				debug.Warning("Failed to update keyspace for preset job %s: %v", job.ID, err)
			}
		}
	}

	return nil
}

// hasIncrementMode checks if a preset job has increment mode enabled
func (s *adminPresetJobService) hasIncrementMode(presetJob *models.PresetJob) bool {
	return presetJob.IncrementMode != "" && presetJob.IncrementMode != "off"
}

// incrementSettingsChanged checks if increment-related settings have changed between two preset jobs
func (s *adminPresetJobService) incrementSettingsChanged(existing, updated *models.PresetJob) bool {
	// Check if increment mode changed
	if existing.IncrementMode != updated.IncrementMode {
		return true
	}

	// Check if increment min changed
	existingMin := 0
	updatedMin := 0
	if existing.IncrementMin != nil {
		existingMin = *existing.IncrementMin
	}
	if updated.IncrementMin != nil {
		updatedMin = *updated.IncrementMin
	}
	if existingMin != updatedMin {
		return true
	}

	// Check if increment max changed
	existingMax := 0
	updatedMax := 0
	if existing.IncrementMax != nil {
		existingMax = *existing.IncrementMax
	}
	if updated.IncrementMax != nil {
		updatedMax = *updated.IncrementMax
	}
	if existingMax != updatedMax {
		return true
	}

	// Check if mask changed (affects layer generation)
	if existing.Mask != updated.Mask {
		return true
	}

	return false
}

// initializePresetIncrementLayers creates increment layers for a preset job with increment mode enabled
// Returns the total effective keyspace (sum of all layer keyspaces)
func (s *adminPresetJobService) initializePresetIncrementLayers(ctx context.Context, presetJob *models.PresetJob) (int64, error) {
	// Only initialize layers if increment mode is enabled
	if !s.hasIncrementMode(presetJob) {
		return 0, nil
	}

	// Step 1: Get mask length (needed for applying defaults)
	maskLength, err := utils.GetMaskLength(presetJob.Mask)
	if err != nil {
		return 0, fmt.Errorf("failed to parse mask: %w", err)
	}

	// Step 2: Apply sensible defaults if min/max are not specified
	// This matches hashcat's behavior where:
	// - --increment-min defaults to 1 if not specified
	// - --increment-max defaults to mask length if not specified
	incrementMin := 1
	if presetJob.IncrementMin != nil {
		incrementMin = *presetJob.IncrementMin
	}
	incrementMax := maskLength
	if presetJob.IncrementMax != nil {
		incrementMax = *presetJob.IncrementMax
	}

	// Clamp values to valid range
	if incrementMin < 1 {
		incrementMin = 1
	}
	if incrementMax > maskLength {
		incrementMax = maskLength
	}

	// Validate constraints
	if incrementMin > maskLength {
		return 0, fmt.Errorf("increment_min (%d) exceeds mask length (%d)", incrementMin, maskLength)
	}
	if incrementMax < incrementMin {
		return 0, fmt.Errorf("increment_max (%d) must be >= increment_min (%d)", incrementMax, incrementMin)
	}

	debug.Log("Initializing preset increment layers", map[string]interface{}{
		"preset_job_id":  presetJob.ID,
		"increment_mode": presetJob.IncrementMode,
		"increment_min":  incrementMin,
		"increment_max":  incrementMax,
		"mask":           presetJob.Mask,
		"mask_length":    maskLength,
	})

	// Generate layer masks
	isInverse := presetJob.IncrementMode == "increment_inverse"
	layerMasks, err := utils.GenerateIncrementLayers(presetJob.Mask, incrementMin, incrementMax, isInverse)
	if err != nil {
		return 0, fmt.Errorf("failed to generate layer masks: %w", err)
	}

	debug.Log("Generated preset increment layer masks", map[string]interface{}{
		"preset_job_id": presetJob.ID,
		"layer_count":   len(layerMasks),
		"masks":         layerMasks,
	})

	// Resolve binary version pattern to actual binary ID
	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, presetJob.BinaryVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve binary version pattern %q: %w", presetJob.BinaryVersion, err)
	}

	// Get hashcat binary path
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get hashcat binary path: %w", err)
	}

	// Create layers with keyspace calculation
	var totalEffectiveKeyspace int64 = 0
	for i, layerMask := range layerMasks {
		// Calculate base_keyspace using hashcat --keyspace
		baseKeyspace, err := s.calculateMaskKeyspace(ctx, hashcatPath, layerMask)
		if err != nil {
			return 0, fmt.Errorf("failed to calculate keyspace for layer %d mask %s: %w", i+1, layerMask, err)
		}

		// Calculate effective keyspace from mask
		effectiveKeyspace, err := utils.CalculateEffectiveKeyspace(layerMask)
		if err != nil {
			debug.Warning("Failed to calculate effective keyspace for mask %s: %v, falling back to base", layerMask, err)
			effectiveKeyspace = baseKeyspace
		}

		debug.Log("Calculated keyspace for preset layer", map[string]interface{}{
			"layer_index":        i + 1,
			"mask":               layerMask,
			"base_keyspace":      baseKeyspace,
			"effective_keyspace": effectiveKeyspace,
		})

		// Create layer record
		layer := &models.PresetIncrementLayer{
			PresetJobID:       presetJob.ID,
			LayerIndex:        i + 1, // 1-indexed
			Mask:              layerMask,
			BaseKeyspace:      &baseKeyspace,
			EffectiveKeyspace: &effectiveKeyspace,
		}

		err = s.presetIncrementLayerRepo.Create(ctx, layer)
		if err != nil {
			return 0, fmt.Errorf("failed to create preset increment layer %d: %w", i+1, err)
		}

		totalEffectiveKeyspace += effectiveKeyspace

		debug.Log("Created preset increment layer", map[string]interface{}{
			"preset_job_id":      presetJob.ID,
			"layer_index":        layer.LayerIndex,
			"mask":               layer.Mask,
			"base_keyspace":      baseKeyspace,
			"effective_keyspace": effectiveKeyspace,
		})
	}

	debug.Log("Preset increment layers initialized successfully", map[string]interface{}{
		"preset_job_id":           presetJob.ID,
		"layer_count":             len(layerMasks),
		"total_effective_keyspace": totalEffectiveKeyspace,
	})

	return totalEffectiveKeyspace, nil
}

// calculateMaskKeyspace runs hashcat --keyspace to get the keyspace for a specific mask
func (s *adminPresetJobService) calculateMaskKeyspace(ctx context.Context, hashcatPath string, mask string) (int64, error) {
	// Build command: hashcat -a 3 <mask> --keyspace
	args := []string{"-a", "3", mask, "--keyspace", "--restore-disable", "--quiet"}

	// Add a unique session ID to allow concurrent executions
	sessionID := fmt.Sprintf("preset_keyspace_%d", time.Now().UnixNano())
	args = append(args, "--session", sessionID)

	debug.Log("Calculating keyspace for mask", map[string]interface{}{
		"mask":         mask,
		"hashcat_path": hashcatPath,
	})

	// Execute with timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, hashcatPath, args...)
	cmd.Dir = s.dataDirectory

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Clean up session files
	sessionFiles := []string{
		filepath.Join(s.dataDirectory, sessionID+".log"),
		filepath.Join(s.dataDirectory, sessionID+".potfile"),
	}
	for _, file := range sessionFiles {
		_ = os.Remove(file)
	}

	if err != nil {
		return 0, fmt.Errorf("hashcat --keyspace command failed: %w (stderr: %s)", err, stderr.String())
	}

	// Parse output
	keyspaceStr := strings.TrimSpace(stdout.String())
	// Get the last non-empty line
	lines := strings.Split(keyspaceStr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			keyspaceStr = line
			break
		}
	}

	keyspace, err := strconv.ParseInt(keyspaceStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse keyspace output '%s': %w", keyspaceStr, err)
	}

	debug.Log("Calculated mask keyspace", map[string]interface{}{
		"mask":     mask,
		"keyspace": keyspace,
	})

	return keyspace, nil
}

// calculateTotalCandidates runs hashcat --total-candidates with retry logic to get actual effective keyspace.
// This accounts for rule effectiveness and gives the true candidate count.
// Returns (effectiveKeyspace, isAccurate, error)
// On failure after retries, returns (0, false, nil) to allow fallback to estimation.
func (s *adminPresetJobService) calculateTotalCandidates(
	ctx context.Context,
	hashcatPath string,
	baseArgs []string,
	presetJobID string,
) (int64, bool, error) {
	const maxRetries = 3
	const retryDelay = 5 * time.Second

	// Build args for --total-candidates (same as --keyspace but different flag)
	args := make([]string, len(baseArgs))
	copy(args, baseArgs)
	args = append(args, "--total-candidates")

	// Add session management
	args = append(args, "--restore-disable")
	sessionID := fmt.Sprintf("total_candidates_%s_%d", presetJobID, time.Now().UnixNano())
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
			"preset_job_id":      presetJobID,
			"effective_keyspace": effectiveKeyspace,
			"method":             "--total-candidates",
		})

		return effectiveKeyspace, true, nil
	}

	debug.Warning("--total-candidates exhausted retries for preset %s: %v", presetJobID, lastErr)
	return 0, false, nil // Allow fallback to estimation
}
