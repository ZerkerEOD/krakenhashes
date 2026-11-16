package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestAccumulatedRunSeconds guards issue #138 end to end:
//   - total running time survives running -> pending -> running cycles instead
//     of resetting to the latest segment (accumulated_run_seconds);
//   - the job's started_at is the FIRST start and never moves on re-dispatch,
//     while running_since tracks the current segment.
//
// It drives the real repository methods and reads values back through GetByID
// (exercising the new column scans). The accumulator is maintained by the
// trg_accumulate_job_run_seconds trigger (migration 20261115120000).
//
// Timings are deterministic without sleeps: running_since is backdated with an
// explicit interval, and the trigger folds NOW() - running_since at the instant
// the job leaves running (consecutive statements run well under a second apart).
func TestAccumulatedRunSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	ctx := context.Background()

	repo := NewJobExecutionRepository(database)

	user := testutil.CreateTestUser(t, database, "i138-user", "i138-user@test.local", testutil.DefaultTestPassword, "user")
	client := testutil.CreateTestClient(t, database, "I138 Client", testutil.ClientCloudOpts{})
	hl := testutil.CreateTestHashlist(t, database, user.ID, client, "i138-hl")

	job := testutil.CreateTestJobExecution(t, database, hl, user.ID, "pending")
	// A separate job stands in as the interrupter (interrupted_by references it).
	interrupter := testutil.CreateTestJobExecution(t, database, hl, user.ID, "running")

	// backdate the CURRENT segment start so the next exit folds ~seconds.
	backdateSegment := func(seconds int) {
		t.Helper()
		if _, err := database.Exec(
			`UPDATE job_executions SET running_since = NOW() - make_interval(secs => $2) WHERE id = $1`,
			job, seconds); err != nil {
			t.Fatalf("backdate running_since: %v", err)
		}
	}
	get := func() *models.JobExecution {
		t.Helper()
		got, err := repo.GetByID(ctx, job)
		require.NoError(t, err)
		return got
	}

	// Fresh pending job: nothing accumulated, not running.
	require.Equal(t, int64(0), get().AccumulatedRunSeconds)

	// Segment 1: start. started_at and running_since are stamped.
	require.NoError(t, repo.StartExecution(ctx, job))
	started := get()
	require.NotNil(t, started.StartedAt, "first start stamps started_at")
	require.NotNil(t, started.RunningSince, "entering running stamps running_since")
	firstStart := *started.StartedAt

	// Run ~10s, then get interrupted back to pending.
	backdateSegment(10)
	require.NoError(t, repo.InterruptExecution(ctx, job, interrupter))
	after1 := get()
	require.Equal(t, models.JobExecutionStatusPending, after1.Status, "interrupt returns the job to pending")
	require.GreaterOrEqual(t, after1.AccumulatedRunSeconds, int64(10), "segment 1 (~10s) folded into accumulator")
	require.LessOrEqual(t, after1.AccumulatedRunSeconds, int64(12))
	require.Nil(t, after1.RunningSince, "running_since cleared when leaving running")
	require.WithinDuration(t, firstStart, *after1.StartedAt, time.Second, "started_at must NOT move on interrupt")

	// Resume: started_at is unchanged (immutable first start); running_since is a
	// fresh segment; the accumulator is untouched on entry (no double-count).
	require.NoError(t, repo.StartExecution(ctx, job))
	resumed := get()
	require.WithinDuration(t, firstStart, *resumed.StartedAt, time.Second, "started_at stays the first start across re-dispatch")
	require.Equal(t, after1.AccumulatedRunSeconds, resumed.AccumulatedRunSeconds, "resume must not change the accumulator")
	require.NotNil(t, resumed.RunningSince)
	require.Less(t, time.Since(*resumed.RunningSince), 5*time.Second, "resume opens a fresh segment")

	// Segment 2: run ~5s, then finish. Total ~15s (both segments), not just 5s.
	backdateSegment(5)
	require.NoError(t, repo.UpdateStatus(ctx, job, models.JobExecutionStatusCompleted))
	final := get()
	require.Equal(t, models.JobExecutionStatusCompleted, final.Status)
	require.GreaterOrEqual(t, final.AccumulatedRunSeconds, int64(15), "both segments (~10s + ~5s) counted, surviving the interrupt")
	require.LessOrEqual(t, final.AccumulatedRunSeconds, int64(18))
	require.WithinDuration(t, firstStart, *final.StartedAt, time.Second, "started_at is still the first start at completion")
}
