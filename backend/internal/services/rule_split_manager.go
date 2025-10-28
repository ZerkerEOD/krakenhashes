package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// RuleChunk represents a chunk of rules split from a larger rule file
type RuleChunk struct {
	Path       string // Path to the chunk file
	StartIndex int    // Starting rule index in the original file
	EndIndex   int    // Ending rule index in the original file
	RuleCount  int    // Number of rules in this chunk
}

// RuleSplitManager handles splitting rule files into smaller chunks
type RuleSplitManager struct {
	tempDir  string
	fileRepo *repository.FileRepository
}

// NewRuleSplitManager creates a new rule split manager
func NewRuleSplitManager(tempDir string, fileRepo *repository.FileRepository) *RuleSplitManager {
	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		debug.Error("Failed to create rule chunk temp directory: %v", err)
	}

	return &RuleSplitManager{
		tempDir:  tempDir,
		fileRepo: fileRepo,
	}
}

// CountRules counts the number of rules in a rule file
func (m *RuleSplitManager) CountRules(ctx context.Context, rulePath string) (int, error) {
	file, err := os.Open(rulePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("rule file not found: %s", rulePath)
		}
		return 0, fmt.Errorf("failed to open rule file %s: %w", rulePath, err)
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

// SplitRuleFile splits a rule file into multiple chunks
func (m *RuleSplitManager) SplitRuleFile(ctx context.Context, jobID int64, ruleFile string, numSplits int) ([]RuleChunk, error) {
	// Read all rules from file
	rules, err := m.readRuleFile(ruleFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read rule file: %w", err)
	}

	if len(rules) == 0 {
		return nil, fmt.Errorf("no rules found in file %s", ruleFile)
	}

	if numSplits <= 0 {
		numSplits = 1
	}

	// Create job-specific directory
	jobDir := filepath.Join(m.tempDir, fmt.Sprintf("job_%d", jobID))
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create job directory: %w", err)
	}

	debug.Log("Created job directory for rule chunks", map[string]interface{}{
		"job_id":      jobID,
		"job_dir":     jobDir,
		"rule_file":   ruleFile,
		"total_rules": len(rules),
		"num_splits":  numSplits,
	})

	// Calculate rules per split
	rulesPerSplit := (len(rules) + numSplits - 1) / numSplits
	chunks := make([]RuleChunk, 0, numSplits)

	for i := 0; i < numSplits; i++ {
		start := i * rulesPerSplit
		end := min((i+1)*rulesPerSplit, len(rules))

		if start >= len(rules) {
			break
		}

		// Create chunk file in job-specific directory
		chunkPath := filepath.Join(jobDir, fmt.Sprintf("chunk_%d.rule", i))
		if err := m.writeRuleChunk(chunkPath, rules[start:end]); err != nil {
			// Cleanup on error - remove entire job directory
			os.RemoveAll(jobDir)
			return nil, fmt.Errorf("failed to write rule chunk %d: %w", i, err)
		}

		chunks = append(chunks, RuleChunk{
			Path:       chunkPath,
			StartIndex: start,
			EndIndex:   end,
			RuleCount:  end - start,
		})

		debug.Log("Created rule chunk", map[string]interface{}{
			"chunk_index": i,
			"path":        chunkPath,
			"start_index": start,
			"end_index":   end,
			"rule_count":  end - start,
		})
	}

	return chunks, nil
}

// readRuleFile reads all rules from a file
func (m *RuleSplitManager) readRuleFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("rule file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to open rule file %s: %w", path, err)
	}
	defer file.Close()

	var rules []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Include all lines, even comments and empty lines, to preserve rule indices
		rules = append(rules, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read rule file: %w", err)
	}

	return rules, nil
}

// writeRuleChunk writes a chunk of rules to a file
func (m *RuleSplitManager) writeRuleChunk(path string, rules []string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create chunk file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, rule := range rules {
		if _, err := writer.WriteString(rule + "\n"); err != nil {
			return fmt.Errorf("failed to write rule: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// cleanupChunks removes chunk files
func (m *RuleSplitManager) cleanupChunks(chunks []RuleChunk) {
	for _, chunk := range chunks {
		if err := os.Remove(chunk.Path); err != nil && !os.IsNotExist(err) {
			debug.Error("Failed to remove chunk file %s: %v", chunk.Path, err)
		}
	}
}

// CleanupJobChunks removes all chunk files for a specific job
func (m *RuleSplitManager) CleanupJobChunks(jobID int64) error {
	jobDir := filepath.Join(m.tempDir, fmt.Sprintf("job_%d", jobID))

	// Check if directory exists
	if _, err := os.Stat(jobDir); os.IsNotExist(err) {
		debug.Log("Job directory does not exist, nothing to clean", map[string]interface{}{
			"job_id":  jobID,
			"job_dir": jobDir,
		})
		return nil
	}

	// Count files before removal for logging
	files, _ := filepath.Glob(filepath.Join(jobDir, "*.rule"))
	fileCount := len(files)

	// Remove the entire job directory
	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("failed to remove job directory %s: %w", jobDir, err)
	}

	debug.Log("Cleaned up rule chunks for job", map[string]interface{}{
		"job_id":     jobID,
		"job_dir":    jobDir,
		"file_count": fileCount,
	})

	return nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CreateSingleRuleChunk creates a single rule chunk on-demand for dynamic chunking
// This is used when assigning work to agents dynamically instead of pre-splitting all chunks
func (m *RuleSplitManager) CreateSingleRuleChunk(ctx context.Context, jobID uuid.UUID, ruleFile string, startIndex int, numRules int) (*RuleChunk, error) {
	// Open the rule file
	file, err := os.Open(ruleFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("rule file not found: %s", ruleFile)
		}
		return nil, fmt.Errorf("failed to open rule file %s: %w", ruleFile, err)
	}
	defer file.Close()

	// Create job-specific directory
	jobDir := filepath.Join(m.tempDir, fmt.Sprintf("job_%s", jobID.String()))
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create job directory: %w", err)
	}

	// Generate unique chunk filename using timestamp
	chunkPath := filepath.Join(jobDir, fmt.Sprintf("chunk_%d_%d.rule", startIndex, startIndex+numRules))

	// Create chunk file
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create chunk file: %w", err)
	}
	defer chunkFile.Close()

	writer := bufio.NewWriter(chunkFile)
	scanner := bufio.NewScanner(file)

	// Skip to startIndex (counting only actual rules, not empty lines/comments)
	ruleIndex := 0
	for ruleIndex < startIndex {
		if !scanner.Scan() {
			break // End of file reached before startIndex
		}
		line := strings.TrimSpace(scanner.Text())
		// Only count non-empty, non-comment lines
		if line != "" && !strings.HasPrefix(line, "#") {
			ruleIndex++
		}
	}

	// Write numRules actual rules to chunk file (skipping empty lines and comments)
	rulesWritten := 0
	for scanner.Scan() && rulesWritten < numRules {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if _, err := writer.WriteString(line + "\n"); err != nil {
			os.Remove(chunkPath) // Clean up on error
			return nil, fmt.Errorf("failed to write rule to chunk: %w", err)
		}
		rulesWritten++
	}

	if err := writer.Flush(); err != nil {
		os.Remove(chunkPath) // Clean up on error
		return nil, fmt.Errorf("failed to flush chunk file: %w", err)
	}

	if err := scanner.Err(); err != nil {
		os.Remove(chunkPath) // Clean up on error
		return nil, fmt.Errorf("failed to read rule file: %w", err)
	}

	chunk := &RuleChunk{
		Path:       chunkPath,
		StartIndex: startIndex,
		EndIndex:   startIndex + rulesWritten,
		RuleCount:  rulesWritten,
	}

	debug.Log("Created single rule chunk", map[string]interface{}{
		"job_id":      jobID,
		"chunk_path":  chunkPath,
		"start_index": startIndex,
		"end_index":   chunk.EndIndex,
		"rule_count":  rulesWritten,
	})

	return chunk, nil
}

// CleanupJobChunksUUID removes all chunk files for a specific job with UUID
func (m *RuleSplitManager) CleanupJobChunksUUID(jobID uuid.UUID) error {
	jobDir := filepath.Join(m.tempDir, fmt.Sprintf("job_%s", jobID.String()))

	// Check if directory exists
	if _, err := os.Stat(jobDir); os.IsNotExist(err) {
		debug.Log("Job directory does not exist, nothing to clean", map[string]interface{}{
			"job_id":  jobID,
			"job_dir": jobDir,
		})
		return nil
	}

	// Count files before removal for logging
	files, _ := filepath.Glob(filepath.Join(jobDir, "*.rule"))
	fileCount := len(files)

	// Remove the entire job directory
	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("failed to remove job directory %s: %w", jobDir, err)
	}

	debug.Log("Cleaned up rule chunks for job", map[string]interface{}{
		"job_id":     jobID,
		"job_dir":    jobDir,
		"file_count": fileCount,
	})

	return nil
}
