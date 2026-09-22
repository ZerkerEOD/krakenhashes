package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
Retry has to be able to reopen a terminal job.

UpdateStatus enforces "terminal is terminal" — it refuses to move a job out of
completed/failed/cancelled and, when the guard bites, returns nil. Both RetryJob
handlers called it anyway, so from 2026-05-22 every "Retry" click cleared the
job's error message, reset its tasks and answered 200 while the job stayed
'failed'. The scheduler only ever selects `WHERE status = 'pending'`, so nothing
restarted. On a production deployment three jobs sat failed through that whole
window with a retry button that did nothing.

ResetToPendingForRetry is the one sanctioned terminal -> pending transition.
These tests pin both halves: that it reopens what it should, and that it refuses
— loudly, never silently — everything else.
*/

// createRetryTestHashlist makes a hashlist to hang job rows off.
func createRetryTestHashlist(t *testing.T, database *db.DB) int64 {
	t.Helper()
	suffix := uuid.NewString()[:8]
	user := testutil.CreateTestUser(t, database,
		"retry-"+suffix, "retry-"+suffix+"@test.local",
		testutil.DefaultTestPassword, "user")

	var hashlistID int64
	err := database.QueryRow(`
		INSERT INTO hashlists (name, user_id, hash_type_id, status)
		VALUES ($1, $2, 0, $3)
		RETURNING id`,
		"retry-test-hashlist-"+suffix, user.ID, models.HashListStatusReady,
	).Scan(&hashlistID)
	require.NoError(t, err, "failed to create test hashlist")
	return hashlistID
}

// insertRetryTestJob creates a job in the given status. Terminal statuses get a
// completed_at and an error_message so the reset can be shown to clear both.
func insertRetryTestJob(t *testing.T, database *db.DB, hashlistID int64, status string) uuid.UUID {
	t.Helper()
	jobID := uuid.New()

	var completedAt interface{}
	var errMsg interface{}
	if status == "completed" || status == "failed" || status == "cancelled" {
		completedAt = time.Now().Add(-time.Hour)
		errMsg = "per-tuple hard cap reached: benchmark failed 10 times on agent 1 for (hash_type=5600, attack_mode=0); cap=10"
	}

	_, err := database.Exec(`
		INSERT INTO job_executions (id, hashlist_id, attack_mode, priority, status, name, completed_at, error_message)
		VALUES ($1, $2, 0, 5, $3, $4, $5, $6)`,
		jobID, hashlistID, status, "retry-test-"+status, completedAt, errMsg)
	require.NoError(t, err, "failed to create %s job execution", status)
	return jobID
}

// readRetryTestJob returns the fields the reset is responsible for.
func readRetryTestJob(t *testing.T, database *db.DB, jobID uuid.UUID) (status string, completedAt *time.Time, errMsg *string) {
	t.Helper()
	err := database.QueryRow(
		`SELECT status, completed_at, error_message FROM job_executions WHERE id = $1`, jobID,
	).Scan(&status, &completedAt, &errMsg)
	require.NoError(t, err, "failed to read job execution back")
	return status, completedAt, errMsg
}

// TestResetToPendingForRetry_ReopensTerminalJobs is the core regression: a failed
// or cancelled job must come back as 'pending' with its terminal bookkeeping
// cleared, because `WHERE status = 'pending'` is the only thing the scheduler
// looks at.
func TestResetToPendingForRetry_ReopensTerminalJobs(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := NewJobExecutionRepository(database)
	hashlistID := createRetryTestHashlist(t, database)

	for _, status := range []string{"failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			jobID := insertRetryTestJob(t, database, hashlistID, status)

			require.NoError(t, repo.ResetToPendingForRetry(ctx, jobID),
				"a %s job is retryable and must reopen", status)

			got, completedAt, errMsg := readRetryTestJob(t, database, jobID)
			assert.Equal(t, "pending", got,
				"job must be 'pending' or the scheduler will never select it")
			assert.Nil(t, completedAt,
				"completed_at must be cleared: the progress loop's terminal grace window keys off it")
			assert.Nil(t, errMsg,
				"error_message must be cleared so the UI stops showing the old failure")
		})
	}
}

// TestResetToPendingForRetry_RefusesNonRetryableJobs keeps the exception narrow.
// A completed job finished — reopening it would re-dispatch keyspace that was
// legitimately exhausted — and a live job has nothing to retry. Every refusal
// must be an error, never a silent success.
func TestResetToPendingForRetry_RefusesNonRetryableJobs(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := NewJobExecutionRepository(database)
	hashlistID := createRetryTestHashlist(t, database)

	for _, status := range []string{"completed", "running", "pending", "paused"} {
		t.Run(status, func(t *testing.T) {
			jobID := insertRetryTestJob(t, database, hashlistID, status)

			err := repo.ResetToPendingForRetry(ctx, jobID)
			require.Error(t, err, "a %s job must not be reopened by retry", status)
			assert.ErrorIs(t, err, ErrJobNotRetryable,
				"the handler needs ErrJobNotRetryable to answer 400 rather than 500")
			assert.Contains(t, err.Error(), status,
				"the error should name the status that blocked the retry")

			got, _, _ := readRetryTestJob(t, database, jobID)
			assert.Equal(t, status, got, "the job's status must be untouched")
		})
	}
}

// TestResetToPendingForRetry_MissingJobIsNotFound separates "no such job" (404)
// from "that job cannot be retried" (400). Returning one for the other sends the
// operator looking in the wrong place.
func TestResetToPendingForRetry_MissingJobIsNotFound(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobExecutionRepository(database)

	err := repo.ResetToPendingForRetry(context.Background(), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound,
		"a job that does not exist is ErrNotFound, not ErrJobNotRetryable")
}

// TestUpdateStatusStillRefusesToReopenTerminalJobs documents WHY the dedicated
// method exists, and fails if someone "simplifies" retry back onto UpdateStatus.
//
// The guard itself is correct and must stay: it is what stopped the 2026-05-17
// runaway where a late hard-cap re-marked an already-completed job as failed.
// The defect was never the guard — it was routing retry through a method that
// silently swallows the refusal.
func TestUpdateStatusStillRefusesToReopenTerminalJobs(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := NewJobExecutionRepository(database)
	hashlistID := createRetryTestHashlist(t, database)
	jobID := insertRetryTestJob(t, database, hashlistID, "failed")

	// Returns nil — this is the silent no-op that hid the bug for months.
	err := repo.UpdateStatus(ctx, jobID, models.JobExecutionStatusPending)
	assert.NoError(t, err, "the guard reports success; that is precisely the trap")

	got, _, _ := readRetryTestJob(t, database, jobID)
	assert.Equal(t, "failed", got,
		"UpdateStatus must NOT reopen a terminal job — use ResetToPendingForRetry")
}
