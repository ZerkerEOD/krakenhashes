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

	// No preset layers cached → fall through to per-job initialization.
	if len(presetLayers) == 0 {
		debug.Log("No preset increment layers found, will calculate from scratch", map[string]interface{}{
			"preset_job_id":    presetJobID,
			"job_execution_id": jobExecution.ID,
		})
		return false, nil
	}

	// Stale-detection: every layer must have an accurate effective_keyspace recorded.
	// Pre-fix presets have rows with is_accurate_keyspace=FALSE (migration default) and
	// possibly wrong effective_keyspace (e.g. file-charset slots evaluated as 26).
	// On stale detection we skip the cache for THIS job and fire a single-flight
	// background refresh so the NEXT job creation hits the fast path.
	stale := false
	for _, pl := range presetLayers {
		if !pl.IsAccurateKeyspace || pl.EffectiveKeyspace == nil || !pl.EffectiveKeyspace.IsPositive() {
			stale = true
			break
		}
	}
	if stale {
		debug.Log("Preset increment layers are stale; refreshing inline before copy", map[string]interface{}{
			"preset_job_id":    presetJobID,
			"job_execution_id": jobExecution.ID,
		})
		if err := s.refreshPresetIncrementLayerCache(ctx, presetJobID); err != nil {
			// Refresh failed — fall through to per-job initializeIncrementLayers so the
			// caller can still produce a job. The preset cache stays stale; next job
			// creation will try again.
			debug.Warning("Inline preset layer refresh failed for %s: %v; falling back to per-job init", presetJobID, err)
			return false, nil
		}
		// Re-fetch the refreshed layers and fall through to the fast-copy path.
		presetLayers, err = s.presetIncrementLayerRepo.GetByPresetJobID(ctx, presetJobID)
		if err != nil {
			debug.Warning("Failed to re-fetch preset layers after refresh: %v; falling back to per-job init", err)
			return false, nil
		}
		if len(presetLayers) == 0 {
			// Should not happen if refresh succeeded, but defend against it.
			return false, nil
		}
	}

	debug.Log("Copying preset increment layers to job", map[string]interface{}{
		"preset_job_id":    presetJobID,
		"job_execution_id": jobExecution.ID,
		"layer_count":      len(presetLayers),
	})

	// Fast path: every preset layer is accurate. Clone into job_increment_layers verbatim.
	var totalBaseKeyspace int64 = 0
	var totalEffectiveKeyspace models.BigInt // effective: base × rules × salts can exceed int64
	allAccurate := true
	for _, presetLayer := range presetLayers {
		jobLayer := &models.JobIncrementLayer{
			JobExecutionID:         jobExecution.ID,
			LayerIndex:             presetLayer.LayerIndex,
			Mask:                   presetLayer.Mask,
			Status:                 models.JobIncrementLayerStatusPending,
			BaseKeyspace:           presetLayer.BaseKeyspace,
			EffectiveKeyspace:      presetLayer.EffectiveKeyspace,
			ProcessedKeyspace:      models.NewBigInt(0),
			DispatchedKeyspace:     models.NewBigInt(0),
			IsAccurateKeyspace:     presetLayer.IsAccurateKeyspace,
			OverallProgressPercent: 0.0,
		}
		if err := s.jobIncrementLayerRepo.Create(ctx, jobLayer); err != nil {
			return false, fmt.Errorf("failed to create job increment layer %d from preset cache: %w", presetLayer.LayerIndex, err)
		}
		if presetLayer.BaseKeyspace != nil {
			totalBaseKeyspace += *presetLayer.BaseKeyspace
		}
		if presetLayer.EffectiveKeyspace != nil {
			totalEffectiveKeyspace = totalEffectiveKeyspace.Add(*presetLayer.EffectiveKeyspace)
		}
		if !presetLayer.IsAccurateKeyspace {
			allAccurate = false
		}
	}

	jobExecution.BaseKeyspace = &totalBaseKeyspace
	jobExecution.EffectiveKeyspace = models.BigIntPtrFromBig(totalEffectiveKeyspace.Big())
	jobExecution.IsAccurateKeyspace = allAccurate

	if err := s.jobExecRepo.UpdateEffectiveKeyspace(ctx, jobExecution.ID, totalEffectiveKeyspace); err != nil {
		debug.Warning("Failed to update job effective_keyspace after preset copy: %v", err)
	}
	if err := s.jobExecRepo.UpdateBaseKeyspace(ctx, jobExecution.ID, totalBaseKeyspace); err != nil {
		debug.Warning("Failed to update job base_keyspace after preset copy: %v", err)
	}
	if err := s.jobExecRepo.SetIsAccurateKeyspace(ctx, jobExecution.ID, allAccurate); err != nil {
		debug.Warning("Failed to update job is_accurate_keyspace after preset copy: %v", err)
	}

	debug.Log("Successfully copied preset increment layers to job", map[string]interface{}{
		"job_execution_id":         jobExecution.ID,
		"layer_count":              len(presetLayers),
		"total_base_keyspace":      totalBaseKeyspace,
		"total_effective_keyspace": totalEffectiveKeyspace,
	})

	return true, nil
}

// refreshPresetIncrementLayerCache recomputes and rewrites preset_increment_layers for a preset.
// Uses the same mode-aware --keyspace + --total-candidates helpers as job-side init.
// DELETE-then-INSERT semantics; on partial failure, leaves the cache empty (next job will
// recalculate again).
func (s *JobExecutionService) refreshPresetIncrementLayerCache(ctx context.Context, presetJobID uuid.UUID) error {
	preset, err := s.presetJobRepo.GetByID(ctx, presetJobID)
	if err != nil {
		return fmt.Errorf("failed to load preset for refresh: %w", err)
	}
	if preset == nil {
		return fmt.Errorf("preset %s not found", presetJobID)
	}
	if preset.IncrementMode == "" || preset.IncrementMode == "off" {
		// Increment was disabled after the refresh was queued — clear stale rows and stop.
		_ = s.presetIncrementLayerRepo.DeleteByPresetJobID(ctx, presetJobID)
		return nil
	}

	maskLength, err := utils.GetMaskLength(preset.Mask)
	if err != nil {
		return fmt.Errorf("failed to parse preset mask: %w", err)
	}

	incrementMin := 1
	if preset.IncrementMin != nil {
		incrementMin = *preset.IncrementMin
	}
	incrementMax := maskLength
	if preset.IncrementMax != nil {
		incrementMax = *preset.IncrementMax
	}
	if incrementMin < 1 {
		incrementMin = 1
	}
	if incrementMax > maskLength {
		incrementMax = maskLength
	}
	if incrementMin > maskLength || incrementMax < incrementMin {
		return fmt.Errorf("invalid increment range min=%d max=%d for mask length %d", incrementMin, incrementMax, maskLength)
	}

	isInverse := preset.IncrementMode == "increment_inverse"
	layerMasks, err := utils.GenerateIncrementLayers(preset.Mask, incrementMin, incrementMax, isInverse)
	if err != nil {
		return fmt.Errorf("failed to generate preset layer masks: %w", err)
	}

	binaryVersionID, err := s.resolveBinaryVersionPattern(ctx, preset.BinaryVersion)
	if err != nil {
		return fmt.Errorf("failed to resolve preset binary version: %w", err)
	}
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, binaryVersionID)
	if err != nil {
		return fmt.Errorf("failed to get hashcat binary path for preset refresh: %w", err)
	}

	attackMode := preset.AttackMode
	wordlistPath := ""
	var wordlistLines int64 = 0
	if attackMode == models.AttackModeHybridWordlistMask || attackMode == models.AttackModeHybridMaskWordlist {
		if len(preset.WordlistIDs) == 0 {
			return fmt.Errorf("hybrid preset has no wordlist; cannot compute keyspace")
		}
		wordlistPath, err = s.resolveWordlistPath(ctx, preset.WordlistIDs[0])
		if err != nil {
			return fmt.Errorf("failed to resolve preset wordlist path: %w", err)
		}
		wordlistLines = s.getWordlistWordCount(ctx, preset.WordlistIDs[0])
	}

	// Compute fresh values for every layer BEFORE touching the cache so partial-failure
	// scenarios don't leave the cache half-populated.
	type pendingLayer struct {
		mask              string
		baseKeyspace      int64
		effectiveKeyspace int64
		isAccurate        bool
	}
	pending := make([]pendingLayer, 0, len(layerMasks))
	for i, layerMask := range layerMasks {
		baseKeyspace, err := s.calculateMaskKeyspace(ctx, hashcatPath, attackMode, layerMask, wordlistPath, preset.CustomCharsets, preset.CustomCharsetFiles, preset.HexCharset)
		if err != nil {
			return fmt.Errorf("preset refresh layer %d --keyspace failed: %w", i+1, err)
		}

		baseArgs := s.buildMaskKeyspaceBaseArgs(attackMode, layerMask, wordlistPath, preset.CustomCharsets, preset.CustomCharsetFiles, preset.HexCharset)
		effectiveKeyspace, isAccurate, err := s.calculateMaskTotalCandidates(ctx, hashcatPath, baseArgs, presetJobID.String())
		if err != nil {
			debug.Warning("Preset refresh layer %d --total-candidates returned error, falling back: %v", i+1, err)
			isAccurate = false
		}
		if !isAccurate || effectiveKeyspace <= 0 {
			est, estErr := utils.CalculateEffectiveKeyspace(layerMask, preset.CustomCharsets, preset.CustomCharsetFiles, wordlistLines)
			if estErr != nil {
				effectiveKeyspace = baseKeyspace
			} else {
				effectiveKeyspace = est
			}
			isAccurate = false
		}
		pending = append(pending, pendingLayer{
			mask:              layerMask,
			baseKeyspace:      baseKeyspace,
			effectiveKeyspace: effectiveKeyspace,
			isAccurate:        isAccurate,
		})
	}

	// Atomically swap the cache: DELETE old rows, then INSERT all new ones. On INSERT failure,
	// best-effort clean up so we don't leave a partial cache.
	if err := s.presetIncrementLayerRepo.DeleteByPresetJobID(ctx, presetJobID); err != nil {
		return fmt.Errorf("preset refresh: failed to delete old layers: %w", err)
	}
	for i, p := range pending {
		layer := &models.PresetIncrementLayer{
			PresetJobID:        presetJobID,
			LayerIndex:         i + 1,
			Mask:               p.mask,
			BaseKeyspace:       &p.baseKeyspace,
			EffectiveKeyspace:  models.NewBigIntPtr(p.effectiveKeyspace),
			IsAccurateKeyspace: p.isAccurate,
		}
		if err := s.presetIncrementLayerRepo.Create(ctx, layer); err != nil {
			_ = s.presetIncrementLayerRepo.DeleteByPresetJobID(ctx, presetJobID)
			return fmt.Errorf("preset refresh: failed to create layer %d: %w", i+1, err)
		}
	}

	return nil
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

	// For hybrid attacks (modes 6 and 7), --keyspace and --total-candidates need the wordlist
	// so hashcat can report wordlist_lines × mask_candidates instead of mask-only candidates.
	attackMode := jobExecution.AttackMode
	wordlistPath := ""
	var wordlistLines int64 = 0
	if attackMode == models.AttackModeHybridWordlistMask || attackMode == models.AttackModeHybridMaskWordlist {
		if len(presetJob.WordlistIDs) == 0 {
			return fmt.Errorf("hybrid attack mode %d requires a wordlist for increment-layer keyspace calculation", attackMode)
		}
		wordlistPath, err = s.resolveWordlistPath(ctx, presetJob.WordlistIDs[0])
		if err != nil {
			return fmt.Errorf("failed to resolve wordlist path for increment layer keyspace: %w", err)
		}
		wordlistLines = s.getWordlistWordCount(ctx, presetJob.WordlistIDs[0])
	}

	// Create layers with base_keyspace calculation
	// Track both base (from hashcat --keyspace) and effective (calculated) totals.
	// effective sum is BigInt: base × rules × salts can exceed int64.
	var totalBaseKeyspace int64 = 0
	var totalEffectiveKeyspace models.BigInt
	allLayersAccurate := true
	for i, layerMask := range layerMasks {
		// Calculate base_keyspace using hashcat --keyspace
		baseKeyspace, err := s.calculateMaskKeyspace(ctx, hashcatPath, attackMode, layerMask, wordlistPath, jobExecution.CustomCharsets, jobExecution.CustomCharsetFiles, jobExecution.HexCharset)
		if err != nil {
			return fmt.Errorf("failed to calculate keyspace for layer %d mask %s: %w", i+1, layerMask, err)
		}

		// Try to get accurate effective keyspace via hashcat --total-candidates.
		// If it succeeds, the layer is marked accurate; otherwise fall back to the mask-math estimate.
		baseArgs := s.buildMaskKeyspaceBaseArgs(attackMode, layerMask, wordlistPath, jobExecution.CustomCharsets, jobExecution.CustomCharsetFiles, jobExecution.HexCharset)
		effectiveKeyspace, isAccurate, err := s.calculateMaskTotalCandidates(ctx, hashcatPath, baseArgs, jobExecution.ID.String())
		if err != nil {
			debug.Warning("Layer %d --total-candidates returned error, falling back: %v", i+1, err)
			isAccurate = false
		}

		if !isAccurate || effectiveKeyspace <= 0 {
			// Fall back to mask-math estimate (with wordlist multiplier for hybrid modes)
			est, estErr := utils.CalculateEffectiveKeyspace(layerMask, jobExecution.CustomCharsets, jobExecution.CustomCharsetFiles, wordlistLines)
			if estErr != nil {
				debug.Warning("Failed to calculate effective keyspace for mask %s: %v, falling back to base", layerMask, estErr)
				effectiveKeyspace = baseKeyspace
			} else {
				effectiveKeyspace = est
			}
			allLayersAccurate = false
		}

		debug.Log("Calculated layer effective keyspace", map[string]interface{}{
			"layer_index":        i + 1,
			"mask":               layerMask,
			"base_keyspace":      baseKeyspace,
			"effective_keyspace": effectiveKeyspace,
			"is_accurate":        isAccurate,
		})

		// Create layer record
		layer := &models.JobIncrementLayer{
			JobExecutionID:         jobExecution.ID,
			LayerIndex:             i + 1, // 1-indexed
			Mask:                   layerMask,
			Status:                 models.JobIncrementLayerStatusPending,
			BaseKeyspace:           &baseKeyspace,
			EffectiveKeyspace:      models.NewBigIntPtr(effectiveKeyspace),
			ProcessedKeyspace:      models.NewBigInt(0),
			DispatchedKeyspace:     models.NewBigInt(0),
			IsAccurateKeyspace:     isAccurate,
			OverallProgressPercent: 0.0,
		}

		err = s.jobIncrementLayerRepo.Create(ctx, layer)
		if err != nil {
			return fmt.Errorf("failed to create increment layer %d: %w", i+1, err)
		}

		// Track both totals for the job
		totalBaseKeyspace += baseKeyspace
		totalEffectiveKeyspace = totalEffectiveKeyspace.AddInt64(effectiveKeyspace)

		debug.Log("Created increment layer", map[string]interface{}{
			"job_execution_id":     jobExecution.ID,
			"layer_index":          layer.LayerIndex,
			"mask":                 layer.Mask,
			"base_keyspace":        baseKeyspace,
			"effective_keyspace":   effectiveKeyspace,
			"is_accurate_keyspace": isAccurate,
		})
	}

	// Mark the job's keyspace as accurate only when every layer reported an accurate value.
	jobExecution.IsAccurateKeyspace = allLayersAccurate

	// Update job's keyspace values - both base and effective
	// base_keyspace = sum of layer base_keyspaces (from hashcat --keyspace)
	// effective_keyspace = sum of layer effective_keyspaces (calculated)
	jobExecution.BaseKeyspace = &totalBaseKeyspace
	jobExecution.EffectiveKeyspace = models.BigIntPtrFromBig(totalEffectiveKeyspace.Big())

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

	// Persist whether every layer reported accurate effective keyspace from --total-candidates
	if err := s.jobExecRepo.SetIsAccurateKeyspace(ctx, jobExecution.ID, allLayersAccurate); err != nil {
		debug.Warning("Failed to update job is_accurate_keyspace: %v", err)
	}

	debug.Log("Increment layers initialized successfully", map[string]interface{}{
		"job_execution_id":         jobExecution.ID,
		"layer_count":              len(layerMasks),
		"total_base_keyspace":      totalBaseKeyspace,
		"total_effective_keyspace": totalEffectiveKeyspace,
		"all_layers_accurate":      allLayersAccurate,
	})

	return nil
}

// buildMaskKeyspaceBaseArgs builds the argv prefix shared by --keyspace and --total-candidates
// invocations for a single layer mask. Returns args containing the attack-mode flag, charsets,
// and the positional arguments in the correct order for the requested attack mode; callers
// append the flag (--keyspace or --total-candidates) plus session/output flags.
//
// For attack mode 3 (brute-force mask) the wordlist path is unused. For modes 6 and 7 (hybrid)
// the wordlist path is required so hashcat measures wordlist_lines × mask_candidates.
func (s *JobExecutionService) buildMaskKeyspaceBaseArgs(attackMode models.AttackMode, mask string, wordlistPath string, customCharsets models.CustomCharsets, charsetFiles models.CustomCharsetFiles, hexCharset bool) []string {
	args := []string{"-a", strconv.Itoa(int(attackMode))}

	// Add --hex-charset ONLY if hex mode AND there is at least one inline charset definition
	// (file charsets are unaffected by --hex-charset; without any inline -1/-2/-3/-4 defs
	// hashcat rejects --hex-charset by interpreting the mask as hex)
	if hexCharset {
		hasInlineCharset := false
		for _, slot := range []string{"1", "2", "3", "4"} {
			if _, isFile := charsetFiles[slot]; isFile {
				continue
			}
			if def, ok := customCharsets[slot]; ok && def != "" {
				hasInlineCharset = true
				break
			}
		}
		if hasInlineCharset {
			args = append(args, "--hex-charset")
		}
	}

	// Add custom charset flags before the mask/wordlist positional args
	// File charsets take priority over inline definitions (same slot can't have both)
	for _, slot := range []string{"1", "2", "3", "4"} {
		if cf, ok := charsetFiles[slot]; ok && cf.FilePath != "" {
			charsetPath := filepath.Join(s.dataDirectory, cf.FilePath)
			args = append(args, "-"+slot, charsetPath)
		} else if def, ok := customCharsets[slot]; ok && def != "" {
			args = append(args, "-"+slot, def)
		}
	}

	switch attackMode {
	case models.AttackModeHybridWordlistMask: // -a 6: wordlist + mask
		args = append(args, wordlistPath, mask)
	case models.AttackModeHybridMaskWordlist: // -a 7: mask + wordlist
		args = append(args, mask, wordlistPath)
	default: // -a 3 (brute-force mask) and anything else falls back to mask-only
		args = append(args, mask)
	}
	return args
}

// calculateMaskKeyspace runs hashcat --keyspace for a specific layer mask under the given attack mode.
// For hybrid modes (6, 7), wordlistPath must be the absolute path to the wordlist used in the job.
func (s *JobExecutionService) calculateMaskKeyspace(ctx context.Context, hashcatPath string, attackMode models.AttackMode, mask string, wordlistPath string, customCharsets models.CustomCharsets, charsetFiles models.CustomCharsetFiles, hexCharset bool) (int64, error) {
	baseArgs := s.buildMaskKeyspaceBaseArgs(attackMode, mask, wordlistPath, customCharsets, charsetFiles, hexCharset)

	args := append([]string{}, baseArgs...)
	args = append(args, "--keyspace", "--restore-disable", "--quiet")

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

// calculateMaskTotalCandidates runs hashcat --total-candidates for a single mask to get the
// accurate effective keyspace (candidate count). Returns (effectiveKeyspace, isAccurate, error).
// On failure or timeout, returns (0, false, nil) so the caller can fall back to the estimate.
// Mirrors adminPresetJobService.calculateTotalCandidates but takes already-built base args.
func (s *JobExecutionService) calculateMaskTotalCandidates(ctx context.Context, hashcatPath string, baseArgs []string, jobID string) (int64, bool, error) {
	const maxRetries = 3
	const retryDelay = 5 * time.Second

	args := append([]string{}, baseArgs...)
	args = append(args, "--total-candidates", "--restore-disable", "--quiet")
	sessionID := fmt.Sprintf("layer_total_candidates_%s_%d", jobID, time.Now().UnixNano())
	args = append(args, "--session", sessionID)

	keyspaceTimeout := s.getKeyspaceTimeout(ctx)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			debug.Warning("Retrying layer --total-candidates (attempt %d/%d) after %v delay: %v",
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

		sessionFiles := []string{
			filepath.Join(s.dataDirectory, sessionID+".log"),
			filepath.Join(s.dataDirectory, sessionID+".potfile"),
		}
		for _, file := range sessionFiles {
			_ = os.Remove(file)
		}

		if err != nil {
			stderrStr := stderr.String()
			if execCtx.Err() == context.DeadlineExceeded {
				debug.Warning("layer --total-candidates timed out after %v for job %s — consider increasing keyspace_calculation_timeout_minutes in Admin Settings", keyspaceTimeout, jobID)
				return 0, false, nil
			}
			if strings.Contains(stderrStr, "Already an instance") ||
				strings.Contains(stderrStr, "already running") {
				lastErr = fmt.Errorf("hashcat busy: %s", stderrStr)
				continue
			}
			debug.Warning("layer --total-candidates failed: %v, stderr: %s", err, stderrStr)
			return 0, false, nil
		}

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
			debug.Warning("Failed to parse layer --total-candidates output '%s': %v", keyspaceStr, parseErr)
			return 0, false, nil
		}

		debug.Log("Calculated layer total candidates successfully", map[string]interface{}{
			"job_id":             jobID,
			"effective_keyspace": effectiveKeyspace,
		})

		return effectiveKeyspace, true, nil
	}

	debug.Warning("layer --total-candidates exhausted retries for job %s: %v", jobID, lastErr)
	return 0, false, nil
}
