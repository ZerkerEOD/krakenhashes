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

// makeUnit creates one scheduling_unit with the given effective_keyspace
// and returns its ID. Used by every test that needs intervals.
func makeUnit(t *testing.T, repo *SchedulingUnitRepository, parentJobID uuid.UUID, effective int64) uuid.UUID {
	t.Helper()
	u := newTestSchedulingUnit(parentJobID)
	u.EffectiveKeyspace = effective
	require.NoError(t, repo.Create(context.Background(), u))
	return u.ID
}

func TestKeyspaceIntervalRepository_InsertAndGetByID(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	iv := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       100,
		RangeEnd:         200,
		Status:           models.KeyspaceIntervalStatusAssigned,
	}
	require.NoError(t, intRepo.Insert(ctx, iv))
	assert.NotEqual(t, uuid.Nil, iv.ID)

	got, err := intRepo.GetByID(ctx, iv.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.RangeStart)
	assert.Equal(t, int64(200), got.RangeEnd)
	assert.Equal(t, models.KeyspaceIntervalStatusAssigned, got.Status)
}

func TestKeyspaceIntervalRepository_OverlapRejected(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	first := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       0,
		RangeEnd:         100,
		Status:           models.KeyspaceIntervalStatusCompleted,
	}
	require.NoError(t, intRepo.Insert(ctx, first))

	overlapping := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       50,
		RangeEnd:         150,
		Status:           models.KeyspaceIntervalStatusAssigned,
	}
	err := intRepo.Insert(ctx, overlapping)
	assert.Error(t, err, "overlapping non-failed insert should fail")
}

func TestKeyspaceIntervalRepository_FailedIntervalMayOverlap(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	// Completed [0,100)
	first := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       0,
		RangeEnd:         100,
		Status:           models.KeyspaceIntervalStatusCompleted,
	}
	require.NoError(t, intRepo.Insert(ctx, first))

	// Failed [50,150) should be allowed because the exclusion constraint
	// has WHERE status <> 'failed'.
	failed := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       50,
		RangeEnd:         150,
		Status:           models.KeyspaceIntervalStatusFailed,
	}
	assert.NoError(t, intRepo.Insert(ctx, failed))
}

func TestKeyspaceIntervalRepository_UndispatchedRanges_EmptyUnit(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	gaps, err := intRepo.UndispatchedRanges(ctx, unitID)
	require.NoError(t, err)
	require.Len(t, gaps, 1, "empty unit should return one gap covering the whole keyspace")
	assert.Equal(t, int64(0), gaps[0].Start)
	assert.Equal(t, int64(1000), gaps[0].End)
}

func TestKeyspaceIntervalRepository_UndispatchedRanges_FullyCovered(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	// One interval covering [0,1000) entirely.
	require.NoError(t, intRepo.Insert(ctx, &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       0,
		RangeEnd:         1000,
		Status:           models.KeyspaceIntervalStatusCompleted,
	}))

	gaps, err := intRepo.UndispatchedRanges(ctx, unitID)
	require.NoError(t, err)
	assert.Empty(t, gaps, "fully-covered unit should have zero gaps")
}

// The user's #6 example: A did [0,100), B did [200,300), C truncated at
// 150 leaving [150,200) as a gap. Should return [100,150), [150,200),
// [300,1000).
//
// (Note: the test uses [0,100) instead of [1,100] from the user's prose to
// match the dispatcher's 0-indexed half-open convention.)
func TestKeyspaceIntervalRepository_UndispatchedRanges_MidGap(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	intervals := []*models.KeyspaceInterval{
		{SchedulingUnitID: unitID, RangeStart: 0, RangeEnd: 100, Status: models.KeyspaceIntervalStatusCompleted},
		{SchedulingUnitID: unitID, RangeStart: 100, RangeEnd: 150, Status: models.KeyspaceIntervalStatusCompleted},
		{SchedulingUnitID: unitID, RangeStart: 200, RangeEnd: 300, Status: models.KeyspaceIntervalStatusCompleted},
	}
	for _, iv := range intervals {
		require.NoError(t, intRepo.Insert(ctx, iv))
	}

	gaps, err := intRepo.UndispatchedRanges(ctx, unitID)
	require.NoError(t, err)
	require.Len(t, gaps, 2, "expected mid-gap and tail-gap")
	assert.Equal(t, int64(150), gaps[0].Start, "first gap starts where coverage ends")
	assert.Equal(t, int64(200), gaps[0].End, "first gap ends where next coverage starts")
	assert.Equal(t, int64(300), gaps[1].Start, "tail gap starts after last interval")
	assert.Equal(t, int64(1000), gaps[1].End, "tail gap ends at effective_keyspace")
}

func TestKeyspaceIntervalRepository_UndispatchedRanges_FailedNotCounted(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	// One completed at [0,500), one failed at [500,1000).
	require.NoError(t, intRepo.Insert(ctx, &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       0,
		RangeEnd:         500,
		Status:           models.KeyspaceIntervalStatusCompleted,
	}))
	require.NoError(t, intRepo.Insert(ctx, &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       500,
		RangeEnd:         1000,
		Status:           models.KeyspaceIntervalStatusFailed,
	}))

	gaps, err := intRepo.UndispatchedRanges(ctx, unitID)
	require.NoError(t, err)
	require.Len(t, gaps, 1, "failed interval should not count as coverage")
	assert.Equal(t, int64(500), gaps[0].Start)
	assert.Equal(t, int64(1000), gaps[0].End)
}

func TestKeyspaceIntervalRepository_GetByUnitID_Order(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	// Insert in non-ascending order.
	starts := []int64{500, 0, 200}
	for _, s := range starts {
		require.NoError(t, intRepo.Insert(ctx, &models.KeyspaceInterval{
			SchedulingUnitID: unitID,
			RangeStart:       s,
			RangeEnd:         s + 50,
			Status:           models.KeyspaceIntervalStatusCompleted,
		}))
	}

	got, err := intRepo.GetByUnitID(ctx, unitID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, int64(0), got[0].RangeStart, "results should be sorted by range_start")
	assert.Equal(t, int64(200), got[1].RangeStart)
	assert.Equal(t, int64(500), got[2].RangeStart)
}

func TestKeyspaceIntervalRepository_UpdateStatus(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	iv := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       0,
		RangeEnd:         100,
		Status:           models.KeyspaceIntervalStatusAssigned,
	}
	require.NoError(t, intRepo.Insert(ctx, iv))

	require.NoError(t, intRepo.UpdateStatus(ctx, iv.ID, models.KeyspaceIntervalStatusRunning))
	got, err := intRepo.GetByID(ctx, iv.ID)
	require.NoError(t, err)
	assert.Equal(t, models.KeyspaceIntervalStatusRunning, got.Status)

	err = intRepo.UpdateStatus(ctx, uuid.New(), models.KeyspaceIntervalStatusFailed)
	assert.True(t, errors.Is(err, sql.ErrNoRows))
}

// Truncate is the §8.2 split-and-gap primitive. An assigned/running
// interval at [S, E) with progress R becomes [S, R) completed, and the
// rest [R, E) becomes a gap automatically (no row covers it).
func TestKeyspaceIntervalRepository_Truncate_SplitsCorrectly(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	iv := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       100,
		RangeEnd:         200,
		Status:           models.KeyspaceIntervalStatusRunning,
	}
	require.NoError(t, intRepo.Insert(ctx, iv))

	// Agent disconnected at restore_point=150 -> truncate to 150.
	require.NoError(t, intRepo.Truncate(ctx, iv.ID, 150))

	got, err := intRepo.GetByID(ctx, iv.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.RangeStart, "start unchanged")
	assert.Equal(t, int64(150), got.RangeEnd, "end truncated to restore_point")
	assert.Equal(t, models.KeyspaceIntervalStatusCompleted, got.Status, "marked completed")

	// And the [150,200) range should now show up as an undispatched gap
	// because no row covers it.
	gaps, err := intRepo.UndispatchedRanges(ctx, unitID)
	require.NoError(t, err)
	foundMidGap := false
	for _, g := range gaps {
		if g.Start == 150 && g.End == 200 {
			foundMidGap = true
			break
		}
	}
	assert.True(t, foundMidGap, "split should produce a [150,200) gap")
}

func TestKeyspaceIntervalRepository_Truncate_RejectsBadInputs(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitRepo := NewSchedulingUnitRepository(database)
	intRepo := NewKeyspaceIntervalRepository(database)
	parentJobID := createSchedulerV2Prereqs(t, database)
	unitID := makeUnit(t, unitRepo, parentJobID, 1000)
	ctx := context.Background()

	iv := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       100,
		RangeEnd:         200,
		Status:           models.KeyspaceIntervalStatusRunning,
	}
	require.NoError(t, intRepo.Insert(ctx, iv))

	// newEnd <= range_start: rejected (would produce zero-or-negative range).
	assert.Error(t, intRepo.Truncate(ctx, iv.ID, 100), "newEnd == range_start must be rejected")
	assert.Error(t, intRepo.Truncate(ctx, iv.ID, 50), "newEnd < range_start must be rejected")

	// newEnd > range_end: rejected (would expand the interval).
	assert.Error(t, intRepo.Truncate(ctx, iv.ID, 250), "newEnd > range_end must be rejected")

	// Completed intervals can't be truncated.
	completed := &models.KeyspaceInterval{
		SchedulingUnitID: unitID,
		RangeStart:       400,
		RangeEnd:         500,
		Status:           models.KeyspaceIntervalStatusCompleted,
	}
	require.NoError(t, intRepo.Insert(ctx, completed))
	assert.Error(t, intRepo.Truncate(ctx, completed.ID, 450), "completed intervals cannot be truncated")
}
