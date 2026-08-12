package services

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// --- harness -----------------------------------------------------------------

// hcsDefaultTestDatabaseURL mirrors the fallback inside testutil.SetupTestDB so
// the reachability probe below targets exactly the server the harness will use.
const hcsDefaultTestDatabaseURL = "postgres://krakenhashes:krakenhashes@localhost:5432/krakenhashes_test?sslmode=disable"

// requireCompletionTestDB returns a migrated test database, or skips when no
// Postgres is reachable.
//
// Skip rather than fail: every other test in package services is a pure unit
// test (they are behind the `unit` build tag but the package must still be
// runnable untagged on a machine with no database). Same shape as
// requireSchedulerTestDB in services/scheduler.
func requireCompletionTestDB(t *testing.T) *db.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("DB-backed completion-service test skipped in -short mode")
	}

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = hcsDefaultTestDatabaseURL
	}

	// Probe first: sql.Open is lazy, so without an explicit Ping the failure
	// would surface deep inside SetupTestDB as a t.Fatalf on the migration run.
	probe, err := sql.Open("postgres", url)
	if err != nil {
		t.Skipf("no test database available (%v); set TEST_DATABASE_URL to run", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if perr := probe.PingContext(ctx); perr != nil {
		probe.Close()
		t.Skipf("no test database reachable at %s (%v); set TEST_DATABASE_URL to run", url, perr)
	}
	probe.Close()

	return testutil.SetupTestDB(t)
}

// newCompletionServiceForTest wires the service against real repositories. All
// of its dependencies except WSHandler are concrete structs over *db.DB, so a
// real test database is the only way to exercise it — and it is also the only
// honest way, since the two behaviours under test ARE raw SQL statements.
//
// wsHandler is nil: every path below is guarded by `if s.wsHandler != nil`, and
// none of the fixture tasks has an agent to signal.
func newCompletionServiceForTest(database *db.DB) *HashlistCompletionService {
	return NewHashlistCompletionService(
		database,
		repository.NewJobExecutionRepository(database),
		repository.NewJobTaskRepository(database),
		repository.NewJobIncrementLayerRepository(database),
		repository.NewHashListRepository(database),
		nil,
	)
}

// createCompletionTestJob builds the row chain the service needs — user,
// hashlist, preset job, job_execution, scheduling unit — and returns the
// job_execution and scheduling_unit IDs.
func createCompletionTestJob(t *testing.T, database *db.DB) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	user := testutil.CreateTestUser(t, database, "completion-test-"+suffix,
		"completion-"+suffix+"@test.local", testutil.DefaultTestPassword, "user")

	var hashlistID int64
	if err := database.QueryRowContext(ctx, `
		INSERT INTO hashlists (name, user_id, hash_type_id, status)
		VALUES ('completion-test', $1, 0, $2)
		RETURNING id
	`, user.ID, models.HashListStatusReady).Scan(&hashlistID); err != nil {
		t.Fatalf("failed to create test hashlist: %v", err)
	}

	presetJobID := uuid.New()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO preset_jobs (id, name, attack_mode, priority, chunk_size_seconds)
		VALUES ($1, 'completion-test', 0, 0, 60)
	`, presetJobID); err != nil {
		t.Fatalf("failed to create test preset_job: %v", err)
	}

	jobID := uuid.New()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO job_executions (id, preset_job_id, hashlist_id, attack_mode, priority, status)
		VALUES ($1, $2, $3, 0, 0, 'running')
	`, jobID, presetJobID, hashlistID); err != nil {
		t.Fatalf("failed to create test job_execution: %v", err)
	}

	unitID := uuid.New()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO scheduling_units (
			id, parent_job_id, layer_index, status, attack_mode,
			effective_keyspace, base_keyspace, is_accurate_keyspace
		) VALUES ($1, $2, 0, 'running', 0, 1000, 1000, true)
	`, unitID, jobID); err != nil {
		t.Fatalf("failed to create test scheduling_unit: %v", err)
	}

	return jobID, unitID
}

// insertCompletionTestTask inserts an agent-less job_tasks row. Agent-less is
// deliberate: it keeps the fixture to one INSERT and steers stopJobTasks away
// from its WebSocket branch, which is not what these tests are about.
func insertCompletionTestTask(t *testing.T, database *db.DB, jobID, unitID uuid.UUID, status string, start, end int64) uuid.UUID {
	t.Helper()

	taskID := uuid.New()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO job_tasks (
			id, job_execution_id, scheduling_unit_id, agent_id, status, detailed_status,
			keyspace_start, keyspace_end, chunk_duration, range_start, range_end
		) VALUES ($1, $2, $3, NULL, $4, 'running', $5, $6, 60, $5, $6)
	`, taskID, jobID, unitID, status, start, end)
	if err != nil {
		t.Fatalf("failed to create test job_task (%s): %v", status, err)
	}
	return taskID
}

func insertCompletionTestInterval(t *testing.T, database *db.DB, unitID, taskID uuid.UUID, start, end int64, status string) uuid.UUID {
	t.Helper()

	intervalID := uuid.New()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO job_keyspace_intervals (
			id, scheduling_unit_id, range_start, range_end, status, task_id
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, intervalID, unitID, start, end, status, taskID)
	if err != nil {
		t.Fatalf("failed to create test interval [%d,%d) %s: %v", start, end, status, err)
	}
	return intervalID
}

func readCompletionTaskStatus(t *testing.T, database *db.DB, taskID uuid.UUID) (string, string) {
	t.Helper()

	var status, detailedStatus string
	err := database.QueryRowContext(context.Background(), `
		SELECT status, COALESCE(detailed_status, '') FROM job_tasks WHERE id = $1
	`, taskID).Scan(&status, &detailedStatus)
	if err != nil {
		t.Fatalf("failed to read task %s: %v", taskID, err)
	}
	return status, detailedStatus
}

func readCompletionIntervalStatus(t *testing.T, database *db.DB, intervalID uuid.UUID) string {
	t.Helper()

	var status string
	err := database.QueryRowContext(context.Background(), `
		SELECT status FROM job_keyspace_intervals WHERE id = $1
	`, intervalID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to read interval %s: %v", intervalID, err)
	}
	return status
}

func readCompletionJobStatus(t *testing.T, database *db.DB, jobID uuid.UUID) string {
	t.Helper()

	var status string
	if err := database.QueryRowContext(context.Background(), `
		SELECT status FROM job_executions WHERE id = $1
	`, jobID).Scan(&status); err != nil {
		t.Fatalf("failed to read job %s: %v", jobID, err)
	}
	return status
}

// --- completeJob -------------------------------------------------------------

// TestHashlistCompletionService_CompleteJobSparesTriggeringTask covers the
// deliberately downgraded GH #62 invariant.
//
// The triggering task is the one whose hashcat status-6 report started this
// path. By the time completeJob runs it is in 'processing', streaming us the
// very cracks that prove the hashlist is finished. Cancelling it out from under
// that handshake is what produced the incident: the row went 'processing' ->
// 'cancelled' here, a (then unguarded) SetTaskProcessing pulled it back to
// 'processing', and nothing was left to move it out again.
//
// Its siblings still get reconciled — including detailed_status, whose absence
// is why the incident row displayed status='failed' with detailed_status='running'.
func TestHashlistCompletionService_CompleteJobSparesTriggeringTask(t *testing.T) {
	database := requireCompletionTestDB(t)
	svc := newCompletionServiceForTest(database)
	ctx := context.Background()

	t.Run("triggering task survives, siblings are cancelled", func(t *testing.T) {
		jobID, unitID := createCompletionTestJob(t, database)
		triggerID := insertCompletionTestTask(t, database, jobID, unitID, "processing", 0, 100)
		siblingID := insertCompletionTestTask(t, database, jobID, unitID, "running", 100, 200)

		if err := svc.completeJob(ctx, &models.JobExecution{ID: jobID}, &triggerID); err != nil {
			t.Fatalf("completeJob: %v", err)
		}

		status, _ := readCompletionTaskStatus(t, database, triggerID)
		if status != "processing" {
			t.Errorf("triggering task status = %q, want processing — cancelling it mid-handshake is the incident", status)
		}

		status, detailed := readCompletionTaskStatus(t, database, siblingID)
		if status != "cancelled" {
			t.Errorf("sibling status = %q, want cancelled", status)
		}
		if detailed != "cancelled" {
			t.Errorf("sibling detailed_status = %q, want cancelled — leaving it stale is how a finished task kept displaying 'running'", detailed)
		}

		// Job completion is explicitly NOT held up waiting for the handshake.
		if jobStatus := readCompletionJobStatus(t, database, jobID); jobStatus != "completed" {
			t.Errorf("job status = %q, want completed", jobStatus)
		}
	})

	t.Run("no triggering task reconciles everything", func(t *testing.T) {
		jobID, unitID := createCompletionTestJob(t, database)
		taskA := insertCompletionTestTask(t, database, jobID, unitID, "processing", 0, 100)
		taskB := insertCompletionTestTask(t, database, jobID, unitID, "running", 100, 200)

		if err := svc.completeJob(ctx, &models.JobExecution{ID: jobID}, nil); err != nil {
			t.Fatalf("completeJob: %v", err)
		}

		// The exclusion is the ONLY thing sparing the triggering task above:
		// with no ID to exclude, the same statement terminalises both.
		for _, taskID := range []uuid.UUID{taskA, taskB} {
			status, detailed := readCompletionTaskStatus(t, database, taskID)
			if status != "cancelled" || detailed != "cancelled" {
				t.Errorf("task %s = %q/%q, want cancelled/cancelled", taskID, status, detailed)
			}
		}
	})
}

// --- stopJobTasks ------------------------------------------------------------

// TestHashlistCompletionService_StopJobTasksLeavesLiveTaskIntervalOpen is the
// scope fix on the interval cascade.
//
// The cascade used to be job-wide and blind to task status, so it promoted the
// still-'processing' triggering task's own interval to 'completed'. applyRecovery
// in services/scheduler uses interval status as its progress oracle: a live task
// whose interval already reads 'completed' has no open interval left to truncate,
// which is exactly what made agent-disconnect recovery mislabel the incident
// task. The invariant restored here is "an interval is only 'completed' when its
// task is terminal".
func TestHashlistCompletionService_StopJobTasksLeavesLiveTaskIntervalOpen(t *testing.T) {
	database := requireCompletionTestDB(t)
	svc := newCompletionServiceForTest(database)
	ctx := context.Background()

	jobID, unitID := createCompletionTestJob(t, database)

	// Still mid crack handshake: non-terminal, so its coverage is not yet
	// anybody's to book.
	liveID := insertCompletionTestTask(t, database, jobID, unitID, "processing", 0, 100)
	liveIntervalID := insertCompletionTestInterval(t, database, unitID, liveID, 0, 100, "assigned")

	// Already terminal: closing its interval is the UI-honesty fix the cascade
	// exists for, and is safe precisely because the task is done.
	doneID := insertCompletionTestTask(t, database, jobID, unitID, "completed", 100, 200)
	doneIntervalID := insertCompletionTestInterval(t, database, unitID, doneID, 100, 200, "running")

	// Never dispatched: terminalised by the loop, so the cascade that runs
	// afterwards in the same call may — and must — close its interval too.
	orphanID := insertCompletionTestTask(t, database, jobID, unitID, "pending", 200, 300)
	orphanIntervalID := insertCompletionTestInterval(t, database, unitID, orphanID, 200, 300, "assigned")

	if _, err := svc.StopJobTasks(ctx, jobID); err != nil {
		t.Fatalf("StopJobTasks: %v", err)
	}

	if status := readCompletionIntervalStatus(t, database, liveIntervalID); status != "assigned" {
		t.Errorf("live task's interval status = %q, want assigned — closing it strands recovery with no interval to truncate", status)
	}
	if status, _ := readCompletionTaskStatus(t, database, liveID); status != "processing" {
		t.Errorf("live task status = %q, want processing — stopJobTasks has no business terminalising an agent-less processing task", status)
	}

	if status := readCompletionIntervalStatus(t, database, doneIntervalID); status != "completed" {
		t.Errorf("completed task's interval status = %q, want completed — the cascade's UI-honesty purpose must survive the scope fix", status)
	}

	if status, _ := readCompletionTaskStatus(t, database, orphanID); status != "cancelled" {
		t.Errorf("orphan task status = %q, want cancelled", status)
	}
	if status := readCompletionIntervalStatus(t, database, orphanIntervalID); status != "completed" {
		t.Errorf("orphan task's interval status = %q, want completed — it was terminalised earlier in this same call", status)
	}
}
