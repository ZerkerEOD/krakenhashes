package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestAccumulatedRunSeconds guards issue #138: a job's total running time must
// survive running -> pending -> running interrupt/resume cycles instead of
// resetting to the latest segment. The accumulator is maintained by the
// trg_accumulate_job_run_seconds trigger (migration 20261115120000), so this
// drives the real repository methods and reads the value back through GetByID
// (which also exercises the new column's scan).
//
// Timings are made deterministic without sleeps by backdating started_at with
// an explicit interval: the trigger folds NOW() - started_at at the moment the
// job leaves running, and consecutive statements run well under a second apart.
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

	backdate := func(seconds int) {
		t.Helper()
		// status stays 'running' so the trigger leaves started_at as set here.
		if _, err := database.Exec(
			`UPDATE job_executions SET started_at = NOW() - make_interval(secs => $2) WHERE id = $1`,
			job, seconds); err != nil {
			t.Fatalf("backdate started_at: %v", err)
		}
	}
	accumulated := func() int64 {
		t.Helper()
		got, err := repo.GetByID(ctx, job)
		require.NoError(t, err)
		return got.AccumulatedRunSeconds
	}

	// Fresh pending job: nothing accumulated yet.
	require.Equal(t, int64(0), accumulated())

	// Segment 1: start, run for ~10s, then get interrupted back to pending.
	require.NoError(t, repo.StartExecution(ctx, job))
	backdate(10)
	require.NoError(t, repo.InterruptExecution(ctx, job, interrupter))

	after1, err := repo.GetByID(ctx, job)
	require.NoError(t, err)
	require.Equal(t, models.JobExecutionStatusPending, after1.Status, "interrupt returns the job to pending")
	require.GreaterOrEqual(t, after1.AccumulatedRunSeconds, int64(10), "segment 1 (~10s) folded into accumulator")
	require.LessOrEqual(t, after1.AccumulatedRunSeconds, int64(12))

	// Resume: re-dispatch resets started_at to a fresh segment start, so the
	// accumulator is unchanged (the prior segment was already folded) and we do
	// not re-count the stale start.
	require.NoError(t, repo.StartExecution(ctx, job))
	resumed, err := repo.GetByID(ctx, job)
	require.NoError(t, err)
	require.Equal(t, after1.AccumulatedRunSeconds, resumed.AccumulatedRunSeconds, "resume must not change the accumulator")
	// started_at must be reset to the new segment start, not the stale ~10s-ago
	// value; otherwise the next fold would re-count the already-folded segment.
	require.NotNil(t, resumed.StartedAt)
	require.Less(t, time.Since(*resumed.StartedAt), 5*time.Second, "resume resets started_at to the fresh segment start")

	// Segment 2: run for ~5s, then finish. Total must be ~15s, not just 5s.
	backdate(5)
	require.NoError(t, repo.UpdateStatus(ctx, job, models.JobExecutionStatusCompleted))

	final, err := repo.GetByID(ctx, job)
	require.NoError(t, err)
	require.Equal(t, models.JobExecutionStatusCompleted, final.Status)
	require.GreaterOrEqual(t, final.AccumulatedRunSeconds, int64(15), "both segments (~10s + ~5s) counted, surviving the interrupt")
	require.LessOrEqual(t, final.AccumulatedRunSeconds, int64(18))
}
