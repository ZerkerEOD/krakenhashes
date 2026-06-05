package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulingUnitRepository_Create(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	unit := newTestSchedulingUnit(parentJobID)
	unit.EffectiveKeyspace = 42_000

	err := repo.Create(ctx, unit)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, unit.ID, "Create should assign an ID")
	assert.False(t, unit.CreatedAt.IsZero(), "Create should set created_at")
	assert.False(t, unit.UpdatedAt.IsZero(), "Create should set updated_at")

	retrieved, err := repo.GetByID(ctx, unit.ID)
	require.NoError(t, err)
	assert.Equal(t, unit.ID, retrieved.ID)
	// priority / max_agents are no longer denormalized onto the unit
	// (migration 000153) — they live on job_executions and are read live by
	// the scheduler's buildUnitInfos.
	assert.Equal(t, int64(42_000), retrieved.EffectiveKeyspace)
	assert.Equal(t, 5, retrieved.RetryBudgetRemaining, "default retry budget should be 5")
}

func TestSchedulingUnitRepository_GetByID_NotFound(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)

	_, err := repo.GetByID(context.Background(), uuid.New())
	assert.True(t, errors.Is(err, sql.ErrNoRows), "missing unit should return sql.ErrNoRows")
}

func TestSchedulingUnitRepository_GetByParentJobID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	// Create three units at layer indexes 0, 1, 2 simulating an --increment 1-3 job.
	for i := 0; i < 3; i++ {
		u := newTestSchedulingUnit(parentJobID)
		u.LayerIndex = i
		u.EffectiveKeyspace = int64((i + 1) * 100)
		require.NoError(t, repo.Create(ctx, u))
	}

	units, err := repo.GetByParentJobID(ctx, parentJobID)
	require.NoError(t, err)
	require.Len(t, units, 3)

	for i, u := range units {
		assert.Equal(t, i, u.LayerIndex, "units should come back in layer_index order")
	}
}

func TestSchedulingUnitRepository_GetSchedulable_FiltersByStatus(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	// Unit A: pending, accurate keyspace -> schedulable
	uA := newTestSchedulingUnit(parentJobID)
	uA.LayerIndex = 0
	require.NoError(t, repo.Create(ctx, uA))

	// Unit B: pending, NOT accurate -> STILL schedulable. GetSchedulable no
	// longer filters on is_accurate_keyspace; the scheduler cycle handles
	// inaccurate units by dispatching a benchmark before a chunk (see
	// GetSchedulable docs and scheduler/cycle.go classification).
	uB := newTestSchedulingUnit(parentJobID)
	uB.LayerIndex = 1
	uB.IsAccurateKeyspace = false
	require.NoError(t, repo.Create(ctx, uB))

	// Unit C: completed -> NOT schedulable
	uC := newTestSchedulingUnit(parentJobID)
	uC.LayerIndex = 2
	require.NoError(t, repo.Create(ctx, uC))
	require.NoError(t, repo.UpdateStatus(ctx, uC.ID, models.SchedulingUnitStatusCompleted))

	// Unit D: running, accurate -> schedulable
	uD := newTestSchedulingUnit(parentJobID)
	uD.LayerIndex = 3
	require.NoError(t, repo.Create(ctx, uD))
	require.NoError(t, repo.UpdateStatus(ctx, uD.ID, models.SchedulingUnitStatusRunning))

	units, err := repo.GetSchedulable(ctx)
	require.NoError(t, err)

	gotIDs := make(map[uuid.UUID]bool)
	for _, u := range units {
		gotIDs[u.ID] = true
	}
	assert.True(t, gotIDs[uA.ID], "pending+accurate unit A should be schedulable")
	assert.True(t, gotIDs[uB.ID], "pending+inaccurate unit B should still be schedulable (benchmark bootstraps it)")
	assert.False(t, gotIDs[uC.ID], "completed unit C should be excluded")
	assert.True(t, gotIDs[uD.ID], "running+accurate unit D should be schedulable")
}

func TestSchedulingUnitRepository_GetSchedulable_OrdersByPriorityThenCreatedAt(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	ctx := context.Background()

	// Priority lives on job_executions (migration 000153), so each unit gets
	// its own parent job at a distinct priority. GetSchedulable orders by
	// je.priority DESC, so expected unit order is the 300, 200, 100 jobs.
	jobP100 := createSchedulerV2PrereqsWithPriority(t, database, 100)
	jobP300 := createSchedulerV2PrereqsWithPriority(t, database, 300)
	jobP200 := createSchedulerV2PrereqsWithPriority(t, database, 200)

	unitByJob := make(map[uuid.UUID]uuid.UUID, 3)
	for _, jobID := range []uuid.UUID{jobP100, jobP300, jobP200} {
		u := newTestSchedulingUnit(jobID)
		require.NoError(t, repo.Create(ctx, u))
		unitByJob[jobID] = u.ID
	}

	units, err := repo.GetSchedulable(ctx)
	require.NoError(t, err)
	require.Len(t, units, 3)

	assert.Equal(t, unitByJob[jobP300], units[0].ID, "highest-priority job's unit first")
	assert.Equal(t, unitByJob[jobP200], units[1].ID, "middle-priority job's unit second")
	assert.Equal(t, unitByJob[jobP100], units[2].ID, "lowest-priority job's unit last")
}

func TestSchedulingUnitRepository_UpdateStatus(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	u := newTestSchedulingUnit(parentJobID)
	require.NoError(t, repo.Create(ctx, u))

	require.NoError(t, repo.UpdateStatus(ctx, u.ID, models.SchedulingUnitStatusRunning))
	got, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, models.SchedulingUnitStatusRunning, got.Status)

	// Updating a non-existent unit returns sql.ErrNoRows.
	err = repo.UpdateStatus(ctx, uuid.New(), models.SchedulingUnitStatusRunning)
	assert.True(t, errors.Is(err, sql.ErrNoRows))
}

func TestSchedulingUnitRepository_UpdateEffectiveKeyspace(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	u := newTestSchedulingUnit(parentJobID)
	u.IsAccurateKeyspace = false
	u.EffectiveKeyspace = 1000
	require.NoError(t, repo.Create(ctx, u))

	require.NoError(t, repo.UpdateEffectiveKeyspace(ctx, u.ID, 1234, true))
	got, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), got.EffectiveKeyspace)
	assert.True(t, got.IsAccurateKeyspace, "is_accurate flag should be upgraded")
}

func TestSchedulingUnitRepository_DecrementRetryBudget(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewSchedulingUnitRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	ctx := context.Background()

	u := newTestSchedulingUnit(parentJobID)
	require.NoError(t, repo.Create(ctx, u))

	// Default budget is 5.
	remaining, err := repo.DecrementRetryBudget(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, remaining)

	// Drain it.
	for i := 0; i < 10; i++ {
		remaining, err = repo.DecrementRetryBudget(ctx, u.ID)
		require.NoError(t, err)
	}
	assert.Equal(t, 0, remaining, "budget should clamp at zero, not go negative")
}
