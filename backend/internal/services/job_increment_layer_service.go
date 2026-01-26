package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/utils"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// copyPresetIncrementLayers copies pre-calculated layers from preset_increment_layers to job_increment_layers
// Returns true if layers were successfully copied, false if no preset layers exist
func (s *JobExecutionService) copyPresetIncrementLayers(ctx context.Context, jobExecution *models.JobExecution, presetJobID uuid.UUID) (bool, error) {
	// Only copy if increment mode is enabled
	if jobExecution.IncrementMode == "" || jobExecution.IncrementMode == "off" {
		return false, nil
	}

	// Check if presetIncrementLayerRepo is available
	if s.presetIncrementLayerRepo == nil {
		debug.Warning("presetIncrementLayerRepo not available, cannot copy preset layers")
		return false, nil
	}

	// Fetch preset increment layers
	presetLayers, err := s.presetIncrementLayerRepo.GetByPresetJobID(ctx, presetJobID)
	if err != nil {
		return false, fmt.Errorf("failed to fetch preset increment layers: %w", err)
	}

	// If no preset layers exist, return false to trigger calculation from scratch
	if len(presetLayers) == 0 {
		debug.Log("No preset increment layers found, will calculate from scratch", map[string]interface{}{
			"preset_job_id":    presetJobID,
			"job_execution_id": jobExecution.ID,
		})
		return false, nil
	}

	debug.Log("Copying preset increment layers to job", map[string]interface{}{
		"preset_job_id":    presetJobID,
		"job_execution_id": jobExecution.ID,
		"layer_count":      len(presetLayers),
	})

	// Copy each preset layer to job_increment_layers
	var totalBaseKeyspace int64 = 0
	var totalEffectiveKeyspace int64 = 0

	for _, presetLayer := range presetLayers {
		// Create job increment layer from preset layer
		jobLayer := &models.JobIncrementLayer{
			JobExecutionID:         jobExecution.ID,
			LayerIndex:             presetLayer.LayerIndex,
			Mask:                   presetLayer.Mask,
			Status:                 models.JobIncrementLayerStatusPending,
			BaseKeyspace:           presetLayer.BaseKeyspace,
			EffectiveKeyspace:      presetLayer.EffectiveKeyspace,
			ProcessedKeyspace:      0,
			DispatchedKeyspace:     0,
			IsAccurateKeyspace:     false, // Will be updated by benchmark
			OverallProgressPercent: 0.0,
		}

		err = s.jobIncrementLayerRepo.Create(ctx, jobLayer)
		if err != nil {
			return false, fmt.Errorf("failed to create job increment layer %d: %w", presetLayer.LayerIndex, err)
		}

		// Track totals
		if presetLayer.BaseKeyspace != nil {
			totalBaseKeyspace += *presetLayer.BaseKeyspace
		}
		if presetLayer.EffectiveKeyspace != nil {
			totalEffectiveKeyspace += *presetLayer.EffectiveKeyspace
		}

		debug.Log("Copied preset layer to job", map[string]interface{}{
			"job_execution_id":   jobExecution.ID,
			"layer_index":        jobLayer.LayerIndex,
			"mask":               jobLayer.Mask,
			"base_keyspace":      presetLayer.BaseKeyspace,
			"effective_keyspace": presetLayer.EffectiveKeyspace,
		})
	}

	// Update job's keyspace values
	jobExecution.BaseKeyspace = &totalBaseKeyspace
	jobExecution.EffectiveKeyspace = &totalEffectiveKeyspace

	// Persist the keyspace values to the database
	err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecution.ID, totalEffectiveKeyspace)
	if err != nil {
		debug.Warning("Failed to update job effective_keyspace: %v", err)
	}

	err = s.jobExecRepo.UpdateBaseKeyspace(ctx, jobExecution.ID, totalBaseKeyspace)
	if err != nil {
		debug.Warning("Failed to update job base_keyspace: %v", err)
	}

	debug.Log("Successfully copied preset increment layers", map[string]interface{}{
		"job_execution_id":          jobExecution.ID,
		"layer_count":               len(presetLayers),
		"total_base_keyspace":       totalBaseKeyspace,
		"total_effective_keyspace":  totalEffectiveKeyspace,
	})

	return true, nil
}

// initializeIncrementLayers creates increment layers for a job with increment mode enabled
// This runs during job creation to:
// 1. Generate layer masks from increment settings
// 2. Calculate base_keyspace for each layer using --keyspace command
// 3. Create job_increment_layers records
func (s *JobExecutionService) initializeIncrementLayers(ctx context.Context, jobExecution *models.JobExecution, presetJob *models.PresetJob) error {
	// Only initialize layers if increment mode is enabled
	if jobExecution.IncrementMode == "" || jobExecution.IncrementMode == "off" {
		return nil
	}

	// Step 1: Get mask length FIRST (needed for applying defaults)
	maskLength, err := utils.GetMaskLength(jobExecution.Mask)
	if err != nil {
		return fmt.Errorf("failed to parse mask: %w", err)
	}

	// Step 2: Apply sensible defaults if min/max are not specified
	// This matches hashcat's behavior where:
	// - --increment-min defaults to 1 if not specified
	// - --increment-max defaults to mask length if not specified
	defaultsApplied := false
	if jobExecution.IncrementMin == nil {
		defaultMin := 1
		jobExecution.IncrementMin = &defaultMin
		defaultsApplied = true
	}
	if jobExecution.IncrementMax == nil {
		defaultMax := maskLength
		jobExecution.IncrementMax = &defaultMax
		defaultsApplied = true
	}

	// Step 3: Clamp values to valid range
	if *jobExecution.IncrementMin < 1 {
		*jobExecution.IncrementMin = 1
		defaultsApplied = true
	}
	if *jobExecution.IncrementMax > maskLength {
		*jobExecution.IncrementMax = maskLength
		defaultsApplied = true
	}

	// Step 4: Validate constraints that should still fail
	if *jobExecution.IncrementMin > maskLength {
		return fmt.Errorf("increment_min (%d) exceeds mask length (%d)", *jobExecution.IncrementMin, maskLength)
	}
	if *jobExecution.IncrementMax < *jobExecution.IncrementMin {
		return fmt.Errorf("increment_max (%d) must be >= increment_min (%d)", *jobExecution.IncrementMax, *jobExecution.IncrementMin)
	}

	// Log with resolved values
	debug.Log("Initializing increment layers", map[string]interface{}{
		"job_execution_id": jobExecution.ID,
		"increment_mode":   jobExecution.IncrementMode,
		"increment_min":    *jobExecution.IncrementMin,
		"increment_max":    *jobExecution.IncrementMax,
		"mask":             jobExecution.Mask,
		"mask_length":      maskLength,
		"defaults_applied": defaultsApplied,
	})

	// Step 5: Persist resolved values to database if defaults were applied
	if defaultsApplied {
		err = s.jobExecRepo.UpdateIncrementSettings(ctx, jobExecution.ID, *jobExecution.IncrementMin, *jobExecution.IncrementMax)
		if err != nil {
			debug.Warning("Failed to persist increment settings defaults: %v", err)
			// Continue anyway - the layers will still be created correctly
		}
	}

	// Generate layer masks
	isInverse := jobExecution.IncrementMode == "increment_inverse"
	layerMasks, err := utils.GenerateIncrementLayers(jobExecution.Mask, *jobExecution.IncrementMin, *jobExecution.IncrementMax, isInverse)
	if err != nil {
		return fmt.Errorf("failed to generate layer masks: %w", err)
	}

	debug.Log("Generated increment layer masks", map[string]interface{}{
		"job_execution_id": jobExecution.ID,
		"layer_count":      len(layerMasks),
		"masks":            layerMasks,
	})

	// Resolve binary version pattern to actual binary ID
	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, jobExecution.BinaryVersion)
	if err != nil {
		return fmt.Errorf("failed to resolve binary version pattern %q: %w", jobExecution.BinaryVersion, err)
	}

	// Get hashcat binary path
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		return fmt.Errorf("failed to get hashcat binary path: %w", err)
	}

	// Create layers with base_keyspace calculation
	// Track both base (from hashcat --keyspace) and effective (calculated) totals
	var totalBaseKeyspace int64 = 0
	var totalEffectiveKeyspace int64 = 0
	for i, layerMask := range layerMasks {
		// Calculate base_keyspace using hashcat --keyspace
		baseKeyspace, err := s.calculateMaskKeyspace(ctx, hashcatPath, layerMask)
		if err != nil {
			return fmt.Errorf("failed to calculate keyspace for layer %d mask %s: %w", i+1, layerMask, err)
		}

		// Calculate estimated effective keyspace from mask
		// This multiplies charset sizes to get actual candidate count
		// For example: ?l?l = 26*26 = 676, ?l?l?l = 26*26*26 = 17576
		estimatedEffective, err := utils.CalculateEffectiveKeyspace(layerMask)
		if err != nil {
			debug.Warning("Failed to calculate effective keyspace for mask %s: %v, falling back to base", layerMask, err)
			estimatedEffective = baseKeyspace
		}

		debug.Log("Calculated estimated effective keyspace for layer", map[string]interface{}{
			"layer_index":         i + 1,
			"mask":                layerMask,
			"base_keyspace":       baseKeyspace,
			"estimated_effective": estimatedEffective,
		})

		// Create layer record
		layer := &models.JobIncrementLayer{
			JobExecutionID:         jobExecution.ID,
			LayerIndex:             i + 1, // 1-indexed
			Mask:                   layerMask,
			Status:                 models.JobIncrementLayerStatusPending,
			BaseKeyspace:           &baseKeyspace,
			EffectiveKeyspace:      &estimatedEffective, // Set estimated value (will be updated by benchmark)
			ProcessedKeyspace:      0,
			DispatchedKeyspace:     0,
			IsAccurateKeyspace:     false, // Mark as estimate
			OverallProgressPercent: 0.0,
		}

		err = s.jobIncrementLayerRepo.Create(ctx, layer)
		if err != nil {
			return fmt.Errorf("failed to create increment layer %d: %w", i+1, err)
		}

		// Track both totals for the job
		totalBaseKeyspace += baseKeyspace
		totalEffectiveKeyspace += estimatedEffective

		debug.Log("Created increment layer", map[string]interface{}{
			"job_execution_id":     jobExecution.ID,
			"layer_index":          layer.LayerIndex,
			"mask":                 layer.Mask,
			"base_keyspace":        baseKeyspace,
			"estimated_effective":  estimatedEffective,
			"is_accurate_keyspace": false,
		})
	}

	// Update job's keyspace values - both base and effective
	// base_keyspace = sum of layer base_keyspaces (from hashcat --keyspace)
	// effective_keyspace = sum of layer effective_keyspaces (calculated)
	jobExecution.BaseKeyspace = &totalBaseKeyspace
	jobExecution.EffectiveKeyspace = &totalEffectiveKeyspace

	// Update effective_keyspace
	err = s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecution.ID, totalEffectiveKeyspace)
	if err != nil {
		debug.Warning("Failed to update job effective_keyspace: %v", err)
	}

	// Update base_keyspace separately
	err = s.jobExecRepo.UpdateBaseKeyspace(ctx, jobExecution.ID, totalBaseKeyspace)
	if err != nil {
		debug.Warning("Failed to update job base_keyspace: %v", err)
	}

	debug.Log("Increment layers initialized successfully", map[string]interface{}{
		"job_execution_id":       jobExecution.ID,
		"layer_count":            len(layerMasks),
		"total_base_keyspace":    totalBaseKeyspace,
		"total_effective_keyspace": totalEffectiveKeyspace,
	})

	return nil
}

// calculateMaskKeyspace runs hashcat --keyspace to get the keyspace for a specific mask
func (s *JobExecutionService) calculateMaskKeyspace(ctx context.Context, hashcatPath string, mask string) (int64, error) {
	// Build command: hashcat -a 3 <mask> --keyspace
	args := []string{"-a", "3", mask, "--keyspace", "--restore-disable", "--quiet"}

	// Add a unique session ID to allow concurrent executions
	sessionID := fmt.Sprintf("layer_keyspace_%d", time.Now().UnixNano())
	args = append(args, "--session", sessionID)

	debug.Log("Calculating keyspace for mask", map[string]interface{}{
		"mask":         mask,
		"hashcat_path": hashcatPath,
		"session_id":   sessionID,
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

	// Parse output - get the last non-empty line
	keyspaceStr := strings.TrimSpace(stdout.String())
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
