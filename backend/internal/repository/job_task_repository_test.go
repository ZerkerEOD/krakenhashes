package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers -----------------------------------------------------------------

// guardTestTask describes the job_tasks row a terminal-guard test wants.
//
// Only the columns the guarded UPDATEs read or write are configurable. Note
// what is NOT here: agent_id is always NULL. None of these statements need an
// agent row (CompleteTaskAndClearAgentStatus' agents UPDATE simply matches
// nothing), and leaving it out keeps the fixture to one INSERT.
type guardTestTask struct {
	// Status is job_tasks.status. Empty means 'running'. The CHECK constraint
	// valid_task_status permits pending, assigned, reconnect_pending, running,
	// processing, completed, failed and cancelled — and nothing else.
	Status string
	// DetailedStatus is what the UI reads. Empty means 'running'.
	DetailedStatus string
	// CrackCount drives the completed_with_cracks / completed_no_cracks CASE
	// that CompleteTask and CompleteTaskAndClearAgentStatus now derive in SQL.
	CrackCount int
	// ReceivedCrackCount seeds the counter SetReceivedCrackCount merges into.
	ReceivedCrackCount int
	// ExpectedCrackCount seeds the counter SetTaskProcessing overwrites.
	ExpectedCrackCount int
	// CrackingCompletedAt seeds the forensic column. SetTaskProcessing is the
	// only statement that stamps it, so a value that jumps to ~now is direct
	// evidence that statement ran.
	CrackingCompletedAt *time.Time
}

// insertGuardTestTask inserts one job_tasks row matching spec and returns its ID.
func insertGuardTestTask(t *testing.T, database *db.DB, jobExecutionID uuid.UUID, spec guardTestTask) uuid.UUID {
	t.Helper()

	status := spec.Status
	if status == "" {
		status = "running"
	}
	detailedStatus := spec.DetailedStatus
	if detailedStatus == "" {
		detailedStatus = "running"
	}

	taskID := uuid.New()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO job_tasks (
			id, job_execution_id, agent_id, status, detailed_status,
			keyspace_start, keyspace_end, chunk_duration,
			crack_count, received_crack_count, expected_crack_count,
			cracking_completed_at
		) VALUES ($1, $2, NULL, $3, $4, 0, 100, 60, $5, $6, $7, $8)
	`, taskID, jobExecutionID, status, detailedStatus,
		spec.CrackCount, spec.ReceivedCrackCount, spec.ExpectedCrackCount,
		spec.CrackingCompletedAt)
	require.NoError(t, err, "failed to insert guard test task")

	return taskID
}

// guardTaskState is the subset of job_tasks these assertions read back.
type guardTaskState struct {
	Status              string
	DetailedStatus      string
	ProgressPercent     float64
	CrackCount          int
	ReceivedCrackCount  int
	ExpectedCrackCount  int
	CompletedAt         sql.NullTime
	CrackingCompletedAt sql.NullTime
}

func readGuardTestTask(t *testing.T, database *db.DB, taskID uuid.UUID) guardTaskState {
	t.Helper()

	var st guardTaskState
	err := database.QueryRowContext(context.Background(), `
		SELECT status,
		       COALESCE(detailed_status, ''),
		       COALESCE(progress_percent, 0),
		       COALESCE(crack_count, 0),
		       COALESCE(received_crack_count, 0),
		       COALESCE(expected_crack_count, 0),
		       completed_at,
		       cracking_completed_at
		FROM job_tasks WHERE id = $1
	`, taskID).Scan(&st.Status, &st.DetailedStatus, &st.ProgressPercent,
		&st.CrackCount, &st.ReceivedCrackCount, &st.ExpectedCrackCount,
		&st.CompletedAt, &st.CrackingCompletedAt)
	require.NoError(t, err, "failed to read task %s", taskID)

	return st
}

// taskCompleter names one of the two guarded completion entry points.
type taskCompleter struct {
	name     string
	complete func(uuid.UUID) error
}

// completerCases returns both completion entry points behind one signature, so
// every assertion about the guard and about the detailed_status CASE is made
// against both. They carry the same statement; they must not drift.
func completerCases(ctx context.Context, repo *JobTaskRepository) []taskCompleter {
	return []taskCompleter{
		{
			name:     "CompleteTask",
			complete: func(id uuid.UUID) error { return repo.CompleteTask(ctx, id) },
		},
		{
			// agentID 0 never matches a row in agents (the sequence starts at
			// 1), which is fine: the agents UPDATE is unguarded and matching
			// nothing is the same no-op as clearing an already-idle agent.
			name:     "CompleteTaskAndClearAgentStatus",
			complete: func(id uuid.UUID) error { return repo.CompleteTaskAndClearAgentStatus(ctx, id, 0) },
		},
	}
}

// --- SetTaskProcessing -------------------------------------------------------

// TestJobTaskRepository_SetTaskProcessing_RefusesTerminalTask is the guard that
// stops a task being dragged backwards out of a terminal status.
//
// The incident: HashlistCompletionService.completeJob reconciled a sibling task
// to 'cancelled', and a job_progress message carrying hashcat status 6 (all
// hashes cracked) landed about a second later on its own goroutine — with no
// ordering against that reconciliation — and pulled the row back to
// 'processing'. Nothing ever moved it out again, so it sat there until the
// agent disconnected and it was written off as 'failed'.
//
// cracking_completed_at is asserted specifically because this statement is the
// ONLY writer of that column: a fresh timestamp on a terminal row is the
// fingerprint that identified this UPDATE as the resurrector.
func TestJobTaskRepository_SetTaskProcessing_RefusesTerminalTask(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)

	// Seeded an hour in the past, so any write would jump it to ~now and no
	// clock-skew tolerance can hide the difference.
	seeded := time.Now().Add(-time.Hour)
	taskID := insertGuardTestTask(t, database, jobID, guardTestTask{
		Status:              "cancelled",
		DetailedStatus:      "cancelled",
		CrackingCompletedAt: &seeded,
	})

	before := readGuardTestTask(t, database, taskID)
	require.True(t, before.CrackingCompletedAt.Valid, "fixture must seed cracking_completed_at")

	err := repo.SetTaskProcessing(ctx, taskID, 7)
	require.Error(t, err, "a cancelled task must not be moved to processing")
	require.ErrorIs(t, err, ErrTaskTerminal,
		"callers distinguish 'lost a race' from 'unknown task' with errors.Is; this must be ErrTaskTerminal")
	assert.False(t, errors.Is(err, ErrNotFound),
		"the row exists — reporting ErrNotFound would tell the caller to give up quietly for the wrong reason")

	after := readGuardTestTask(t, database, taskID)
	assert.Equal(t, "cancelled", after.Status, "status must not move backwards out of a terminal state")
	assert.Equal(t, "cancelled", after.DetailedStatus, "detailed_status must not move either")
	assert.Equal(t, 0, after.ExpectedCrackCount, "expected_crack_count must not be written on a terminal row")
	require.True(t, after.CrackingCompletedAt.Valid, "cracking_completed_at must not be cleared")
	assert.WithinDuration(t, before.CrackingCompletedAt.Time, after.CrackingCompletedAt.Time, time.Second,
		"cracking_completed_at moved — this statement wrote to a terminal row, which is exactly the resurrection bug")
}

// TestJobTaskRepository_SetTaskProcessing_MissingTaskIsNotFound keeps the other
// half of the disambiguation honest: an unknown task ID is ErrNotFound, never
// the terminal sentinel.
func TestJobTaskRepository_SetTaskProcessing_MissingTaskIsNotFound(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)

	err := repo.SetTaskProcessing(context.Background(), uuid.New(), 3)
	require.ErrorIs(t, err, ErrNotFound, "an unknown task ID must report ErrNotFound")
	assert.False(t, errors.Is(err, ErrTaskTerminal),
		"there is no row to be terminal — the caller must be told to give up, not that it lost a race")
}

// TestJobTaskRepository_SetTaskProcessing_AdvancesRunningTask proves the guard
// only blocks backwards moves: the normal running -> processing transition
// still works and still records the handshake's expected crack count.
func TestJobTaskRepository_SetTaskProcessing_AdvancesRunningTask(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)
	taskID := insertGuardTestTask(t, database, jobID, guardTestTask{Status: "running"})

	require.NoError(t, repo.SetTaskProcessing(ctx, taskID, 4))

	st := readGuardTestTask(t, database, taskID)
	assert.Equal(t, "processing", st.Status)
	assert.Equal(t, 4, st.ExpectedCrackCount, "the crack handshake needs the expected count recorded")
	assert.True(t, st.CrackingCompletedAt.Valid, "cracking_completed_at is stamped when hashcat finishes")
}

// --- CompleteTask / CompleteTaskAndClearAgentStatus --------------------------

// TestJobTaskRepository_CompleteTask_RefusesTerminalTask covers both completion
// entry points against an already-cancelled row. The operator-stop path cancels
// the task before the agent's final messages arrive, so this race is routine —
// and completing a cancelled task would re-run the once-only side effects the
// caller gates on a nil error.
func TestJobTaskRepository_CompleteTask_RefusesTerminalTask(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)

	for _, tc := range completerCases(ctx, repo) {
		t.Run(tc.name, func(t *testing.T) {
			seeded := time.Now().Add(-time.Hour)
			taskID := insertGuardTestTask(t, database, jobID, guardTestTask{
				Status:              "cancelled",
				DetailedStatus:      "cancelled",
				CrackingCompletedAt: &seeded,
			})

			before := readGuardTestTask(t, database, taskID)

			err := tc.complete(taskID)
			require.Error(t, err, "a cancelled task must not be completed")
			require.ErrorIs(t, err, ErrTaskTerminal)
			assert.False(t, errors.Is(err, ErrNotFound), "the row exists; this is a lost race, not a missing task")

			after := readGuardTestTask(t, database, taskID)
			assert.Equal(t, "cancelled", after.Status, "recovery already finalised this row")
			assert.Equal(t, "cancelled", after.DetailedStatus)
			assert.Equal(t, before.ProgressPercent, after.ProgressPercent, "progress_percent must not be forced to 100")
			assert.Equal(t, before.CompletedAt.Valid, after.CompletedAt.Valid, "completed_at must not be stamped")
			require.True(t, after.CrackingCompletedAt.Valid)
			assert.WithinDuration(t, before.CrackingCompletedAt.Time, after.CrackingCompletedAt.Time, time.Second,
				"cracking_completed_at moved — the guarded UPDATE wrote to a terminal row")
		})
	}
}

// TestJobTaskRepository_CompleteTask_DetailedStatusFollowsCrackCount pins the
// CASE that replaced a hardcoded "completed_no_cracks".
//
// The caller cannot know the final crack count — crack batches land on their
// own goroutines and may bump crack_count after the caller's snapshot — so the
// label has to be derived in the same statement that reads the row. Before
// this, every task that actually cracked something was labelled as having
// cracked nothing.
func TestJobTaskRepository_CompleteTask_DetailedStatusFollowsCrackCount(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)

	crackCases := []struct {
		name       string
		crackCount int
		want       string
	}{
		{name: "with cracks", crackCount: 3, want: "completed_with_cracks"},
		{name: "no cracks", crackCount: 0, want: "completed_no_cracks"},
	}

	for _, completer := range completerCases(ctx, repo) {
		for _, tc := range crackCases {
			t.Run(completer.name+"/"+tc.name, func(t *testing.T) {
				taskID := insertGuardTestTask(t, database, jobID, guardTestTask{
					Status:     "processing",
					CrackCount: tc.crackCount,
				})

				require.NoError(t, completer.complete(taskID))

				st := readGuardTestTask(t, database, taskID)
				assert.Equal(t, "completed", st.Status)
				assert.Equal(t, tc.want, st.DetailedStatus,
					"detailed_status must be derived from crack_count, not hardcoded")
				assert.Equal(t, 100.0, st.ProgressPercent)
				assert.True(t, st.CompletedAt.Valid, "completed_at must be stamped on a real completion")
			})
		}
	}
}

// TestJobTaskRepository_CompleteTask_CompletesExactlyOnceUnderConcurrency is
// the assertion the whole "exactly once" design rests on.
//
// TryFinalizeTask can be reached from several independently scheduled agent
// messages, each of which may conclude the crack handshake is satisfied. Under
// READ COMMITTED the loser's UPDATE blocks on the winner's row lock and then
// re-evaluates its WHERE against the newly committed tuple, so it matches
// nothing and reports ErrTaskTerminal. That — not a mutex, and not a
// SELECT ... FOR UPDATE — is what stops the once-only side effects (benchmark
// EMA update, completion notification, job-completion cascade) running twice.
func TestJobTaskRepository_CompleteTask_CompletesExactlyOnceUnderConcurrency(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)
	taskID := insertGuardTestTask(t, database, jobID, guardTestTask{
		Status:             "processing",
		CrackCount:         1,
		ExpectedCrackCount: 1,
		ReceivedCrackCount: 1,
	})

	const racers = 2

	// ready + start line the goroutines up so both are inside the call before
	// either does any work; done collects them. The outcome does not actually
	// depend on the overlap (a fully serialised pair produces the same
	// one-winner result), which is what keeps this deterministic rather than
	// timing-dependent.
	var ready, done sync.WaitGroup
	ready.Add(racers)
	done.Add(racers)
	start := make(chan struct{})
	errs := make([]error, racers)

	for i := 0; i < racers; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			errs[i] = repo.CompleteTask(ctx, taskID)
		}(i)
	}

	ready.Wait()
	close(start)
	done.Wait()

	succeeded, terminal := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrTaskTerminal):
			terminal++
		default:
			t.Errorf("racer %d returned an unexpected error: %v", i, err)
		}
	}

	assert.Equal(t, 1, succeeded, "exactly one caller may be told it completed the task")
	assert.Equal(t, 1, terminal,
		"the loser must get ErrTaskTerminal — a nil error would re-run the once-only completion side effects")

	st := readGuardTestTask(t, database, taskID)
	assert.Equal(t, "completed", st.Status)
	assert.Equal(t, "completed_with_cracks", st.DetailedStatus)
}

// --- SetReceivedCrackCount ---------------------------------------------------

// TestJobTaskRepository_SetReceivedCrackCount_IsMonotonic pins the GREATEST
// merge. The retransmit path reconciles this counter to an absolute value it
// computed from the whole outfile, but a normal-path crack batch may be in
// flight on another goroutine and may already have incremented past it —
// lowering the counter would re-open a handshake that is genuinely satisfied.
func TestJobTaskRepository_SetReceivedCrackCount_IsMonotonic(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewJobTaskRepository(database)
	ctx := context.Background()

	jobID := createSchedulerV2Prereqs(t, database)
	taskID := insertGuardTestTask(t, database, jobID, guardTestTask{
		Status:             "processing",
		ReceivedCrackCount: 5,
	})

	require.NoError(t, repo.SetReceivedCrackCount(ctx, taskID, 2))
	assert.Equal(t, 5, readGuardTestTask(t, database, taskID).ReceivedCrackCount,
		"a lower reconciliation must never lower the counter")

	require.NoError(t, repo.SetReceivedCrackCount(ctx, taskID, 9))
	assert.Equal(t, 9, readGuardTestTask(t, database, taskID).ReceivedCrackCount,
		"a higher reconciliation must raise the counter, or a retransmit-recovered task never completes")

	err := repo.SetReceivedCrackCount(ctx, uuid.New(), 1)
	require.ErrorIs(t, err, ErrNotFound, "an unknown task ID must report ErrNotFound")
}
