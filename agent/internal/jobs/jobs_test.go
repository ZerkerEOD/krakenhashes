package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobManager_Creation(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		DataDirectory: tempDir,
	}

	var progressUpdates []*JobProgress
	progressCallback := func(progress *JobProgress) {
		progressUpdates = append(progressUpdates, progress)
	}

	jobManager := NewJobManager(cfg, progressCallback, nil)

	assert.NotNil(t, jobManager)
	assert.Equal(t, cfg, jobManager.config)
	assert.NotNil(t, jobManager.executor)
	assert.NotNil(t, jobManager.activeJobs)
	assert.NotNil(t, jobManager.benchmarkCache)
}

func TestJobManager_JobAssignmentParsing(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		DataDirectory: tempDir,
	}

	jobManager := NewJobManager(cfg, nil, nil)
	// The startup MD5 map gate defaults closed; mark it ready so this test
	// exercises the assignment flow rather than the "map not ready" refusal.
	jobManager.MarkFileMapReady()

	// Create test job assignment
	assignment := JobTaskAssignment{
		TaskID:          "test-task-123",
		JobExecutionID:  "job-exec-456",
		HashlistID:      789,
		HashlistPath:    "hashlists/test.hash",
		AttackMode:      int(AttackModeStraight),
		HashType:        0, // MD5
		KeyspaceStart:   0,
		KeyspaceEnd:     1000000,
		WordlistPaths:   []string{"wordlists/rockyou.txt"},
		RulePaths:       []string{"rules/best64.rule"},
		BinaryPath:      "/usr/bin/hashcat",
		ChunkDuration:   1200, // 20 minutes
		ReportInterval:  5,    // 5 seconds
		OutputFormat:    "3",  // hash:plain
	}

	// Marshal to JSON
	assignmentData, err := json.Marshal(assignment)
	require.NoError(t, err)

	// Create required directories and files for the test
	hashlistDir := filepath.Join(tempDir, "hashlists")
	err = os.MkdirAll(hashlistDir, 0755)
	require.NoError(t, err)

	wordlistDir := filepath.Join(tempDir, "wordlists")
	err = os.MkdirAll(wordlistDir, 0755)
	require.NoError(t, err)

	rulesDir := filepath.Join(tempDir, "rules")
	err = os.MkdirAll(rulesDir, 0755)
	require.NoError(t, err)

	// Create test files
	hashlistFile := filepath.Join(hashlistDir, "test.hash")
	err = os.WriteFile(hashlistFile, []byte("5d41402abc4b2a76b9719d911017c592\n"), 0644)
	require.NoError(t, err)

	wordlistFile := filepath.Join(wordlistDir, "rockyou.txt")
	err = os.WriteFile(wordlistFile, []byte("password\n123456\nadmin\n"), 0644)
	require.NoError(t, err)

	rulesFile := filepath.Join(rulesDir, "best64.rule")
	err = os.WriteFile(rulesFile, []byte(":\nc\nu\n"), 0644)
	require.NoError(t, err)

	// Test that we can parse the assignment (but not execute since we don't have hashcat)
	ctx := context.Background()
	
	// This should fail since hashcat binary doesn't exist, but we can test the parsing
	err = jobManager.ProcessJobAssignment(ctx, assignmentData)
	assert.Error(t, err) // Expected to fail due to missing hashcat binary
	assert.Contains(t, err.Error(), "failed to start task execution")

	// Verify the assignment was parsed correctly before execution failure
	// We can't easily test this without mocking, but the error should be about execution, not parsing
}

func TestJobManager_BenchmarkCaching(t *testing.T) {
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		DataDirectory: tempDir,
	}

	jobManager := NewJobManager(cfg, nil, nil)

	// Add a benchmark result manually
	benchmarkKey := "0_0" // MD5, dictionary attack
	result := &BenchmarkResult{
		HashType:   0,
		AttackMode: int(AttackModeStraight),
		Speed:      1000000, // 1MH/s
		Timestamp:  time.Now(),
	}

	jobManager.benchmarkCache[benchmarkKey] = result

	// Test retrieval
	benchmarks := jobManager.GetBenchmarkResults()
	assert.Len(t, benchmarks, 1)
	assert.Contains(t, benchmarks, benchmarkKey)
	assert.Equal(t, int64(1000000), benchmarks[benchmarkKey].Speed)
}

func TestJobManager_ActiveJobsTracking(t *testing.T) {
	tempDir := t.TempDir()
	
	cfg := &config.Config{
		DataDirectory: tempDir,
	}

	jobManager := NewJobManager(cfg, nil, nil)

	// Initially no active jobs
	activeJobs := jobManager.GetActiveJobs()
	assert.Len(t, activeJobs, 0)

	// Add a job manually (simulating an active job)
	assignment := &JobTaskAssignment{
		TaskID:         "test-task-123",
		JobExecutionID: "job-exec-456",
		AttackMode:     int(AttackModeStraight),
		HashType:       0,
	}

	jobExecution := &JobExecution{
		Assignment: assignment,
		StartTime:  time.Now(),
		Status:     "running",
	}

	jobManager.activeJobs[assignment.TaskID] = jobExecution

	// Now we should have one active job
	activeJobs = jobManager.GetActiveJobs()
	assert.Len(t, activeJobs, 1)
	assert.Contains(t, activeJobs, assignment.TaskID)

	// Test job status retrieval
	status, err := jobManager.GetJobStatus(assignment.TaskID)
	assert.NoError(t, err)
	assert.Equal(t, "running", status.Status)
	assert.Equal(t, assignment.TaskID, status.Assignment.TaskID)

	// Test non-existent job
	_, err = jobManager.GetJobStatus("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJobTaskAssignment_JSONMarshaling(t *testing.T) {
	assignment := JobTaskAssignment{
		TaskID:          "task-123",
		JobExecutionID:  "job-456",
		HashlistID:      789,
		HashlistPath:    "hashlists/test.hash",
		AttackMode:      int(AttackModeStraight),
		HashType:        0,
		KeyspaceStart:   0,
		KeyspaceEnd:     1000000,
		WordlistPaths:   []string{"wordlists/rockyou.txt"},
		RulePaths:       []string{"rules/best64.rule"},
		BinaryPath:      "/usr/bin/hashcat",
		ChunkDuration:   1200,
		ReportInterval:  5,
		OutputFormat:    "3",
	}

	// Test JSON marshaling
	data, err := json.Marshal(assignment)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test JSON unmarshaling
	var parsed JobTaskAssignment
	err = json.Unmarshal(data, &parsed)
	assert.NoError(t, err)

	// Verify all fields were preserved
	assert.Equal(t, assignment.TaskID, parsed.TaskID)
	assert.Equal(t, assignment.JobExecutionID, parsed.JobExecutionID)
	assert.Equal(t, assignment.HashlistID, parsed.HashlistID)
	assert.Equal(t, assignment.AttackMode, parsed.AttackMode)
	assert.Equal(t, assignment.HashType, parsed.HashType)
	assert.Equal(t, assignment.KeyspaceStart, parsed.KeyspaceStart)
	assert.Equal(t, assignment.KeyspaceEnd, parsed.KeyspaceEnd)
	assert.Equal(t, assignment.WordlistPaths, parsed.WordlistPaths)
	assert.Equal(t, assignment.RulePaths, parsed.RulePaths)
	assert.Equal(t, assignment.BinaryPath, parsed.BinaryPath)
}

func TestJobProgress_JSONMarshaling(t *testing.T) {
	temperature := 65.5
	utilization := 95.2
	timeRemaining := 300

	progress := JobProgress{
		TaskID:            "task-123",
		KeyspaceProcessed: 500000,
		HashRate:          1250000,
		Temperature:       &temperature,
		Utilization:       &utilization,
		TimeRemaining:     &timeRemaining,
		CrackedCount:      5,
		CrackedHashes:     []CrackedHash{
			{Hash: "hash1", Plain: "plain1", FullLine: "hash1:plain1"},
			{Hash: "hash2", Plain: "plain2", FullLine: "hash2:plain2"},
		},
	}

	// Test JSON marshaling
	data, err := json.Marshal(progress)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test JSON unmarshaling
	var parsed JobProgress
	err = json.Unmarshal(data, &parsed)
	assert.NoError(t, err)

	// Verify all fields were preserved
	assert.Equal(t, progress.TaskID, parsed.TaskID)
	assert.Equal(t, progress.KeyspaceProcessed, parsed.KeyspaceProcessed)
	assert.Equal(t, progress.HashRate, parsed.HashRate)
	assert.Equal(t, *progress.Temperature, *parsed.Temperature)
	assert.Equal(t, *progress.Utilization, *parsed.Utilization)
	assert.Equal(t, *progress.TimeRemaining, *parsed.TimeRemaining)
	assert.Equal(t, progress.CrackedCount, parsed.CrackedCount)
	assert.Equal(t, progress.CrackedHashes, parsed.CrackedHashes)
}

func TestAttackMode_Values(t *testing.T) {
	// Test that attack mode constants have correct values
	assert.Equal(t, AttackMode(0), AttackModeStraight)
	assert.Equal(t, AttackMode(1), AttackModeCombination)
	assert.Equal(t, AttackMode(3), AttackModeBruteForce)
	assert.Equal(t, AttackMode(6), AttackModeHybridWordlistMask)
	assert.Equal(t, AttackMode(7), AttackModeHybridMaskWordlist)
}