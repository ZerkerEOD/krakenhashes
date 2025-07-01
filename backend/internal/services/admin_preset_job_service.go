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
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
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
	presetJobRepo      repository.PresetJobRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	binaryManager      binary.Manager
	fileRepo           *repository.FileRepository
	dataDirectory      string
}

// NewAdminPresetJobService creates a new service for managing preset jobs.
func NewAdminPresetJobService(
	presetJobRepo repository.PresetJobRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	binaryManager binary.Manager,
	fileRepo *repository.FileRepository,
	dataDirectory string,
) AdminPresetJobService {
	return &adminPresetJobService{
		presetJobRepo:      presetJobRepo,
		systemSettingsRepo: systemSettingsRepo,
		binaryManager:      binaryManager,
		fileRepo:           fileRepo,
		dataDirectory:      dataDirectory,
	}
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
		// Rules are optional for straight mode

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
	// - Check if BinaryVersionID actually exists in binary_versions table.
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

	debug.Info("Creating preset job: %s", params.Name)
	
	// Calculate keyspace for the preset job
	keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &params)
	if err != nil {
		// Log the error but don't fail creation - keyspace can be calculated later
		debug.Warning("Failed to calculate keyspace for preset job: %v", err)
	}
	params.Keyspace = keyspace
	
	createdJob, err := s.presetJobRepo.Create(ctx, params)
	if err != nil {
		debug.Error("Failed to create preset job in repository: %v", err)
		// TODO: Handle specific DB errors like unique constraint violations more gracefully
		return nil, fmt.Errorf("failed to create preset job: %w", err)
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
	_, err := s.GetPresetJobByID(ctx, id)
	if err != nil {
		return nil, err // Returns ErrNotFound if applicable
	}

	if err := s.validatePresetJob(ctx, params, true, id); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	debug.Info("Updating preset job ID: %s", id)
	
	// Get the existing job to check if keyspace recalculation is needed
	existingJob, _ := s.presetJobRepo.GetByID(ctx, id)
	
	// Check if keyspace was explicitly provided (from recalculation endpoint)
	if params.Keyspace != nil {
		// Keyspace was explicitly set, use it
		debug.Info("Using explicitly provided keyspace for preset job %s: %v", id, params.Keyspace)
	} else if existingJob != nil && s.needsKeyspaceRecalculation(existingJob, &params) {
		// Check if keyspace-affecting fields have changed
		// Recalculate keyspace
		keyspace, err := s.CalculateKeyspaceForPresetJob(ctx, &params)
		if err != nil {
			// Log the error but don't fail update - keyspace can be calculated later
			debug.Warning("Failed to calculate keyspace for preset job: %v", err)
		}
		params.Keyspace = keyspace
	} else if existingJob != nil {
		// Keep existing keyspace if no changes affecting it
		params.Keyspace = existingJob.Keyspace
	}
	
	updatedJob, err := s.presetJobRepo.Update(ctx, id, params)
	if err != nil {
		debug.Error("Failed to update preset job %s: %v", id, err)
		// TODO: Handle specific DB errors
		return nil, fmt.Errorf("failed to update preset job: %w", err)
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
	if existing.BinaryVersionID != updated.BinaryVersionID {
		return true
	}
	
	return false
}

// CalculateKeyspaceForPresetJob calculates the total keyspace for a preset job using hashcat --keyspace
func (s *adminPresetJobService) CalculateKeyspaceForPresetJob(ctx context.Context, presetJob *models.PresetJob) (*int64, error) {
	// Get the hashcat binary path from binary manager
	hashcatPath, err := s.binaryManager.GetLocalBinaryPath(ctx, int64(presetJob.BinaryVersionID))
	if err != nil {
		return nil, fmt.Errorf("failed to get hashcat binary path: %w", err)
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
		"preset_job_id": presetJob.ID,
		"command": hashcatPath,
		"args":    args,
		"attack_mode": presetJob.AttackMode,
		"session_id": sessionID,
	})

	// Execute hashcat command with timeout
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Set working directory to data directory to ensure session files are created there
	cmd := exec.CommandContext(ctx, hashcatPath, args...)
	cmd.Dir = s.dataDirectory
	
	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	err = cmd.Run()
	
	// Clean up session files regardless of success/failure
	// Note: With --restore-disable, .restore files won't be created
	sessionFiles := []string{
		filepath.Join(s.dataDirectory, sessionID + ".log"),  
		filepath.Join(s.dataDirectory, sessionID + ".potfile"),
		filepath.Join(s.dataDirectory, sessionID + ".induct"),
		filepath.Join(s.dataDirectory, sessionID + ".outfile"),
		// Also check in binary directory in case hashcat creates files there
		filepath.Join(filepath.Dir(hashcatPath), sessionID + ".log"),
		filepath.Join(filepath.Dir(hashcatPath), sessionID + ".potfile"),
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
		
		debug.Error("Hashcat keyspace calculation failed", map[string]interface{}{
			"error":       err.Error(),
			"stdout":      stdout.String(),
			"stderr":      stderrStr,
			"command":     hashcatPath,
			"args":        args,
			"session_id":  sessionID,
		})
		return nil, fmt.Errorf("hashcat keyspace calculation failed: %w (stderr: %s)", err, stderrStr)
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
	
	debug.Log("Keyspace calculated successfully", map[string]interface{}{
		"preset_job_id": presetJob.ID,
		"keyspace": keyspace,
		"session_id": sessionID,
		"stdout_lines": len(outputLines),
		"keyspace_str": keyspaceStr,
	})

	return &keyspace, nil
}

// resolveWordlistPath resolves the full path for a wordlist ID
func (s *adminPresetJobService) resolveWordlistPath(ctx context.Context, wordlistIDStr string) (string, error) {
	wordlistID, err := strconv.ParseInt(wordlistIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid wordlist ID: %s", wordlistIDStr)
	}

	// Look up wordlist in database
	wordlists, err := s.fileRepo.GetWordlists(ctx, "")
	if err != nil {
		return "", fmt.Errorf("failed to get wordlists: %w", err)
	}
	
	for _, wl := range wordlists {
		if wl.ID == int(wordlistID) {
			// The Name field now contains category/filename (e.g., "general/crackstation.txt")
			// We need to use just the filename without duplicating the category
			filename := wl.Name
			
			// If the Name already contains the category path, extract just the filename
			if strings.Contains(wl.Name, "/") {
				filename = filepath.Base(wl.Name)
			}
			
			// Build absolute path using the data directory
			var path string
			if wl.Category != "" {
				path = filepath.Join(s.dataDirectory, "wordlists", wl.Category, filename)
			} else {
				path = filepath.Join(s.dataDirectory, "wordlists", filename)
			}
			
			// Verify the file exists
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("wordlist file not found at %s: %w", path, err)
			}
			
			debug.Log("Resolved wordlist path", map[string]interface{}{
				"wordlist_id": wordlistID,
				"category":    wl.Category,
				"name_field":  wl.Name,
				"filename":    filename,
				"path":        path,
			})
			return path, nil
		}
	}
	
	return "", fmt.Errorf("wordlist with ID %d not found", wordlistID)
}

// resolveRulePath resolves the full path for a rule ID
func (s *adminPresetJobService) resolveRulePath(ctx context.Context, ruleIDStr string) (string, error) {
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid rule ID: %s", ruleIDStr)
	}

	// Look up rule in database
	rules, err := s.fileRepo.GetRules(ctx, "")
	if err != nil {
		return "", fmt.Errorf("failed to get rules: %w", err)
	}
	
	for _, rule := range rules {
		if rule.ID == int(ruleID) {
			// The Name field now contains category/filename (e.g., "hashcat/wordlist_2f26acbe.txt")
			// We need to use just the filename without duplicating the category
			filename := rule.Name
			
			// If the Name already contains the category path, extract just the filename
			if strings.Contains(rule.Name, "/") {
				filename = filepath.Base(rule.Name)
			}
			
			// Build absolute path using the data directory
			var path string
			if rule.Category != "" {
				path = filepath.Join(s.dataDirectory, "rules", rule.Category, filename)
			} else {
				path = filepath.Join(s.dataDirectory, "rules", filename)
			}
			
			// Verify the file exists
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("rule file not found at %s: %w", path, err)
			}
			
			debug.Log("Resolved rule path", map[string]interface{}{
				"rule_id":    ruleID,
				"category":   rule.Category,
				"name_field": rule.Name,
				"filename":   filename,
				"path":       path,
			})
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
