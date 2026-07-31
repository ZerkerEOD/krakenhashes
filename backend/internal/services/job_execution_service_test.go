//go:build unit
// +build unit

package services

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

// Mock repositories for testing
type MockJobExecutionRepository struct {
	mock.Mock
}

func (m *MockJobExecutionRepository) Create(ctx context.Context, exec *models.JobExecution) error {
	args := m.Called(ctx, exec)
	// Set a test ID and timestamp
	exec.ID = uuid.New()
	exec.CreatedAt = time.Now()
	return args.Error(0)
}

func (m *MockJobExecutionRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.JobExecution, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*models.JobExecution), args.Error(1)
}

func (m *MockJobExecutionRepository) GetPendingJobs(ctx context.Context) ([]models.JobExecution, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.JobExecution), args.Error(1)
}

func (m *MockJobExecutionRepository) StartExecution(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) UpdateProgress(ctx context.Context, id uuid.UUID, processedKeyspace int64) error {
	args := m.Called(ctx, id, processedKeyspace)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) GetRunningJobs(ctx context.Context) ([]models.JobExecution, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.JobExecution), args.Error(1)
}

func (m *MockJobExecutionRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.JobExecutionStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) CompleteExecution(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) FailExecution(ctx context.Context, id uuid.UUID, errorMessage string) error {
	args := m.Called(ctx, id, errorMessage)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) InterruptExecution(ctx context.Context, id uuid.UUID, interruptingJobID uuid.UUID) error {
	args := m.Called(ctx, id, interruptingJobID)
	return args.Error(0)
}

func (m *MockJobExecutionRepository) GetInterruptibleJobs(ctx context.Context, priority int) ([]models.JobExecution, error) {
	args := m.Called(ctx, priority)
	return args.Get(0).([]models.JobExecution), args.Error(1)
}

type MockPresetJobRepository struct {
	mock.Mock
}

func (m *MockPresetJobRepository) Create(ctx context.Context, params models.PresetJob) (*models.PresetJob, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(*models.PresetJob), args.Error(1)
}

func (m *MockPresetJobRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.PresetJob, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*models.PresetJob), args.Error(1)
}

func (m *MockPresetJobRepository) GetByName(ctx context.Context, name string) (*models.PresetJob, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PresetJob), args.Error(1)
}

func (m *MockPresetJobRepository) List(ctx context.Context) ([]models.PresetJob, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.PresetJob), args.Error(1)
}

func (m *MockPresetJobRepository) Update(ctx context.Context, id uuid.UUID, params models.PresetJob) (*models.PresetJob, error) {
	args := m.Called(ctx, id, params)
	return args.Get(0).(*models.PresetJob), args.Error(1)
}

func (m *MockPresetJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockPresetJobRepository) ListFormData(ctx context.Context) (*repository.PresetJobFormData, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.PresetJobFormData), args.Error(1)
}

type MockHashlistRepository struct {
	mock.Mock
}

func (m *MockHashlistRepository) GetByID(ctx context.Context, id int64) (*models.HashList, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*models.HashList), args.Error(1)
}

type MockSystemSettingsRepository struct {
	mock.Mock
}

func (m *MockSystemSettingsRepository) GetByKey(ctx context.Context, key string) (*models.SystemSetting, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.SystemSetting), args.Error(1)
}

func TestJobExecutionService_CreateJobExecution(t *testing.T) {
	// NOTE: This test demonstrates the mock pattern but cannot actually instantiate
	// JobExecutionService since it requires concrete repository types, not mocks.
	// This is a limitation of the current architecture.
	t.Skip("Skipping due to architecture limitation: service requires concrete repository types")
}

func TestJobExecutionService_GetNextPendingJob(t *testing.T) {
	t.Skip("Skipping due to architecture limitation: service requires concrete repository types")
}

func TestJobExecutionService_GetNextPendingJob_NoPendingJobs(t *testing.T) {
	t.Skip("Skipping due to architecture limitation: service requires concrete repository types")
}

func TestJobExecutionService_CanInterruptJob(t *testing.T) {
	t.Skip("Skipping due to architecture limitation: service requires concrete repository types")
}

func TestJobExecutionService_CanInterruptJob_Disabled(t *testing.T) {
	t.Skip("Skipping due to architecture limitation: service requires concrete repository types")
}

// TestChunkLocalProgressPercent covers the base-unit per-task progress
// calculation. It is a regression guard for the bug where running tasks on
// salted hash types all displayed a static 99.99%: the old calc divided the
// agent's salt-free progress[0] by a salt-adjusted (base x rules x salts) chunk
// span, so the ratio exceeded 100% for every running chunk and was clamped to
// the reserved 99.99. Computing in BASE units removes the salt/rule drift.
func TestChunkLocalProgressPercent(t *testing.T) {
	const (
		b       = int64(1_000_000_000) // 1B
		epsilon = 0.01
	)

	cases := []struct {
		name              string
		keyspaceProcessed int64
		keyspaceStart     int64
		keyspaceEnd       int64
		agentPercent      float64
		wantPercent       float64
		wantBaseProc      int64
	}{
		{
			// The reported bug: a salted mid-keyspace chunk 25% into its own
			// span. Pre-fix this rendered 99.99; it must now read ~25%.
			// restore_point is ABSOLUTE while running: 140B + 25%*(30B) = 147.5B.
			name:              "salted mid-keyspace chunk reads real progress",
			keyspaceProcessed: 147_500 * (b / 1000), // 147.5B
			keyspaceStart:     140 * b,
			keyspaceEnd:       170 * b,
			wantPercent:       25.0,
			wantBaseProc:      7_500 * (b / 1000), // 7.5B
		},
		{
			// First chunk: KeyspaceStart == 0, restore point is already
			// chunk-relative (absolute == relative).
			name:              "first chunk (start=0) halfway",
			keyspaceProcessed: 16 * b,
			keyspaceStart:     0,
			keyspaceEnd:       32 * b,
			wantPercent:       50.0,
			wantBaseProc:      16 * b,
		},
		{
			// A chunk that has processed its entire span reads 99.99, not 100
			// (100 is reserved for terminal completion writes).
			name:              "full chunk caps at 99.99",
			keyspaceProcessed: 170 * b,
			keyspaceStart:     140 * b,
			keyspaceEnd:       170 * b,
			wantPercent:       99.99,
			wantBaseProc:      30 * b,
		},
		{
			// Restore point overshoots the chunk end: baseProc clamps to the
			// chunk size, percent caps at 99.99.
			name:              "overshoot clamps to chunk size",
			keyspaceProcessed: 175 * b,
			keyspaceStart:     140 * b,
			keyspaceEnd:       170 * b,
			wantPercent:       99.99,
			wantBaseProc:      30 * b,
		},
		{
			// Degenerate task without a base chunk span falls back to the
			// agent's already chunk-local ratio.
			name:              "no base span falls back to agent percent",
			keyspaceProcessed: 5 * b,
			keyspaceStart:     100 * b,
			keyspaceEnd:       100 * b,
			agentPercent:      42.5,
			wantPercent:       42.5,
			wantBaseProc:      5 * b,
		},
		{
			// Fallback still honors the terminal-write cap.
			name:              "no base span with >=100 agent percent caps at 99.99",
			keyspaceProcessed: 0,
			keyspaceStart:     100 * b,
			keyspaceEnd:       100 * b,
			agentPercent:      100.0,
			wantPercent:       99.99,
			wantBaseProc:      0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercent, gotBaseProc := chunkLocalProgressPercent(
				tc.keyspaceProcessed, tc.keyspaceStart, tc.keyspaceEnd, tc.agentPercent)

			diff := gotPercent - tc.wantPercent
			if diff < 0 {
				diff = -diff
			}
			if diff > epsilon {
				t.Errorf("percent = %.4f, want %.4f", gotPercent, tc.wantPercent)
			}
			if gotBaseProc != tc.wantBaseProc {
				t.Errorf("baseProc = %d, want %d", gotBaseProc, tc.wantBaseProc)
			}
		})
	}
}
