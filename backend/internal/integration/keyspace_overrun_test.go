package integration

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover resolveEstimatedKeyspaceOverrun, which only ever runs on the
// degraded path where hashcat's --keyspace pre-flight timed out and
// base_keyspace came from the wordlist's stored word_count instead.
//
// That count is an UPPER bound (hashcat skips blank and over-length lines), so
// the tail chunk can address words hashcat does not index; hashcat exits 0
// having tested nothing and the agent reports AGENT_NO_WORK. Without this
// handling the tail gap never closes and the job hangs just short of 100% — the
// same failure class as GH #62.
//
// The risk in the other direction is worse: AGENT_NO_WORK is also how a device
// that never ran reports itself (kernel autotune skip). Swallowing that would
// hide a real fault, so most of what follows asserts the guard does NOT fire.

// overrunFixture builds a job + scheduling unit + task + interval and returns
// the unit and task IDs. baseKeyspace is the (possibly over-estimated) unit
// base; the task covers [rangeStart, rangeEnd).
func overrunFixture(t *testing.T, database *db.DB, estimated bool, baseKeyspace, rangeStart, rangeEnd int64) (unitID, taskID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	user := testutil.CreateTestUser(t, database, "overrun-"+suffix, "overrun-"+suffix+"@test.local", testutil.DefaultTestPassword, "user")

	var hashlistID int64
	require.NoError(t, database.QueryRowContext(ctx, `
		INSERT INTO hashlists (name, user_id, hash_type_id, status)
		VALUES ('overrun-test', $1, 0, $2)
		RETURNING id
	`, user.ID, models.HashListStatusReady).Scan(&hashlistID))

	jobID := uuid.New()
	_, err := database.ExecContext(ctx, `
		INSERT INTO job_executions (id, hashlist_id, attack_mode, priority, base_keyspace, base_keyspace_estimated)
		VALUES ($1, $2, 0, 0, $3, $4)
	`, jobID, hashlistID, baseKeyspace, estimated)
	require.NoError(t, err)

	unitID = uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO scheduling_units (id, parent_job_id, layer_index, status, attack_mode,
		                              effective_keyspace, base_keyspace, base_keyspace_estimated)
		VALUES ($1, $2, 0, 'pending', 0, $3, $4, $5)
	`, unitID, jobID, baseKeyspace, baseKeyspace, estimated)
	require.NoError(t, err)

	taskID = uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO job_tasks (id, job_execution_id, scheduling_unit_id, keyspace_start, keyspace_end,
		                       range_start, range_end, chunk_duration, status)
		VALUES ($1, $2, $3, $4, $5, $4, $5, 60, 'running')
	`, taskID, jobID, unitID, rangeStart, rangeEnd)
	require.NoError(t, err)

	_, err = database.ExecContext(ctx, `
		INSERT INTO job_keyspace_intervals (id, scheduling_unit_id, range_start, range_end, status, task_id)
		VALUES ($1, $2, $3, $4, 'running', $5)
	`, uuid.New(), unitID, rangeStart, rangeEnd, taskID)
	require.NoError(t, err)

	return unitID, taskID
}

func unitBase(t *testing.T, database *db.DB, unitID uuid.UUID) (base int64, estimated bool) {
	t.Helper()
	require.NoError(t, database.QueryRowContext(context.Background(),
		`SELECT base_keyspace, base_keyspace_estimated FROM scheduling_units WHERE id = $1`,
		unitID).Scan(&base, &estimated))
	return
}

func rowCount(t *testing.T, database *db.DB, query string, arg interface{}) int {
	t.Helper()
	var n int
	require.NoError(t, database.QueryRowContext(context.Background(), query, arg).Scan(&n))
	return n
}

const noWorkErr = "AGENT_NO_WORK: hashcat exited 0 without processing any candidates (no status ever reported)"

// The real case: word_count said 14,344,391 but hashcat only indexes
// 14,344,384, so the last 7-word chunk tests nothing.
func TestResolveEstimatedKeyspaceOverrun_ShrinksTailAndRetiresRange(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, true, 14344391, 14344384, 14344391)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, noWorkErr)
	require.NoError(t, err)
	assert.True(t, handled, "an AGENT_NO_WORK tail failure on an estimated base should be handled")

	base, estimated := unitBase(t, database, unitID)
	assert.Equal(t, int64(14344384), base, "base_keyspace should shrink to where the dead chunk started")
	assert.False(t, estimated, "base is now measured, not estimated, so the flag should clear")

	assert.Zero(t, rowCount(t, database, `SELECT count(*) FROM job_tasks WHERE id = $1`, taskID),
		"the impossible task should be removed, not left as a failed row")
	assert.Zero(t, rowCount(t, database, `SELECT count(*) FROM job_keyspace_intervals WHERE task_id = $1`, taskID),
		"its interval should go too, so the tail gap does not reopen")
}

// The parent job must be corrected as well — the units are denormalized from it,
// so leaving the job's own base stale would resurface on any repopulate.
func TestResolveEstimatedKeyspaceOverrun_AlsoCorrectsParentJob(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, true, 1000, 990, 1000)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, noWorkErr)
	require.NoError(t, err)
	require.True(t, handled)

	var jobBase int64
	var jobEstimated bool
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT je.base_keyspace, je.base_keyspace_estimated
		FROM job_executions je JOIN scheduling_units su ON su.parent_job_id = je.id
		WHERE su.id = $1
	`, unitID).Scan(&jobBase, &jobEstimated))

	assert.Equal(t, int64(990), jobBase)
	assert.False(t, jobEstimated)
}

// An exact base keyspace means the words really are there, so AGENT_NO_WORK is a
// device that never ran. This is the autotune-skip case and must stay a failure.
func TestResolveEstimatedKeyspaceOverrun_IgnoresExactBaseKeyspace(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, false, 1000, 990, 1000)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, noWorkErr)
	require.NoError(t, err)
	assert.False(t, handled, "must not swallow AGENT_NO_WORK when the base keyspace is exact")

	base, _ := unitBase(t, database, unitID)
	assert.Equal(t, int64(1000), base, "base must be left alone")
	assert.Equal(t, 1, rowCount(t, database, `SELECT count(*) FROM job_tasks WHERE id = $1`, taskID),
		"the task must survive so normal failure handling can run")
}

// A no-work failure in the middle of the keyspace is not an overrun: the words
// exist, so something else went wrong and it must be reported.
func TestResolveEstimatedKeyspaceOverrun_IgnoresMidKeyspaceFailure(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, true, 1000, 400, 500)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, noWorkErr)
	require.NoError(t, err)
	assert.False(t, handled, "only a range reaching the tail can be an overrun")

	base, estimated := unitBase(t, database, unitID)
	assert.Equal(t, int64(1000), base)
	assert.True(t, estimated, "the estimate flag should still stand")
}

// Shrinking to zero would mark a job complete having tested nothing at all, so a
// first chunk that reports no work is treated as a real failure.
func TestResolveEstimatedKeyspaceOverrun_RefusesToShrinkToZero(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, true, 1000, 0, 1000)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, noWorkErr)
	require.NoError(t, err)
	assert.False(t, handled, "a unit that never produced work must not be silently completed")

	base, _ := unitBase(t, database, unitID)
	assert.Equal(t, int64(1000), base)
}

// Any other failure is somebody else's to handle, even on an estimated unit.
func TestResolveEstimatedKeyspaceOverrun_IgnoresUnrelatedErrors(t *testing.T) {
	database := testutil.SetupTestDB(t)
	unitID, taskID := overrunFixture(t, database, true, 1000, 990, 1000)

	for _, msg := range []string{
		"AGENT_AUTOTUNE: kernel autotune failure skipped the device",
		"hashcat exited with status 255",
		"",
	} {
		handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, taskID, msg)
		require.NoError(t, err)
		assert.Falsef(t, handled, "must not fire on %q", msg)
	}

	base, _ := unitBase(t, database, unitID)
	assert.Equal(t, int64(1000), base)
}

// Legacy tasks predate scheduling units; the join finds nothing and the guard
// must decline rather than error.
func TestResolveEstimatedKeyspaceOverrun_IgnoresUnknownTask(t *testing.T) {
	database := testutil.SetupTestDB(t)

	handled, err := resolveEstimatedKeyspaceOverrun(context.Background(), database.DB, uuid.New(), noWorkErr)
	require.NoError(t, err, "an unknown task is not an error condition")
	assert.False(t, handled)
}
