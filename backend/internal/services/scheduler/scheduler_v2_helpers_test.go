package scheduler

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// defaultTestDatabaseURL mirrors the fallback inside testutil.SetupTestDB so
// the reachability probe below targets exactly the server the harness will use.
const defaultTestDatabaseURL = "postgres://krakenhashes:krakenhashes@localhost:5432/krakenhashes_test?sslmode=disable"

// requireSchedulerTestDB returns a migrated test database, or skips the test
// when no Postgres is reachable.
//
// The repository package's DB-backed tests hard-fail in that situation, but
// every other test in this package is a pure unit test that must keep passing
// on a machine with no database — so these skip instead of failing the whole
// package. Set TEST_DATABASE_URL (or run a Postgres matching the default DSN)
// to actually exercise them.
func requireSchedulerTestDB(t *testing.T) *db.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("DB-backed scheduler test skipped in -short mode")
	}

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = defaultTestDatabaseURL
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

// createTestJobExecution builds the minimum row chain a scheduler-v2 test needs
// before it can create scheduling_units, tasks and intervals: a user, a
// hashlist, a preset job and a job_execution. Returns the job_execution ID for
// use as parent_job_id.
//
// The repository package has a fuller equivalent
// (createSchedulerV2PrereqsWithPriority) but it is unexported and lives in
// package repository, so this is the minimal in-package version. Each call
// makes a distinct user/hashlist/preset/job chain so several jobs can coexist
// within one test.
func createTestJobExecution(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	user := testutil.CreateTestUser(t, database, "scheduler-test-"+suffix,
		"scheduler-"+suffix+"@test.local", testutil.DefaultTestPassword, "user")

	var hashlistID int64
	err := database.QueryRowContext(ctx, `
		INSERT INTO hashlists (name, user_id, hash_type_id, status)
		VALUES ('scheduler-test', $1, 0, $2)
		RETURNING id
	`, user.ID, models.HashListStatusReady).Scan(&hashlistID)
	if err != nil {
		t.Fatalf("failed to create test hashlist: %v", err)
	}

	presetJobID := uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO preset_jobs (id, name, attack_mode, priority, chunk_size_seconds)
		VALUES ($1, 'scheduler-test', 0, 0, 60)
	`, presetJobID)
	if err != nil {
		t.Fatalf("failed to create test preset_job: %v", err)
	}

	jobExecutionID := uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO job_executions (id, preset_job_id, hashlist_id, attack_mode, priority)
		VALUES ($1, $2, $3, 0, 0)
	`, jobExecutionID, presetJobID, hashlistID)
	if err != nil {
		t.Fatalf("failed to create test job_execution: %v", err)
	}

	return jobExecutionID
}

// createTestUnit inserts one scheduling_unit. base_keyspace is what firstGap
// uses as the tail bound, so it must be set — a NULL makes the gap query
// return no rows at all.
func createTestUnit(t *testing.T, database *db.DB, parentJobID uuid.UUID, baseKeyspace int64) uuid.UUID {
	t.Helper()

	unitID := uuid.New()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO scheduling_units (
			id, parent_job_id, layer_index, status, attack_mode,
			effective_keyspace, base_keyspace, is_accurate_keyspace
		) VALUES ($1, $2, 0, 'running', 0, $3, $3, true)
	`, unitID, parentJobID, baseKeyspace)
	if err != nil {
		t.Fatalf("failed to create test scheduling_unit: %v", err)
	}
	return unitID
}

// insertTestInterval inserts one job_keyspace_intervals row over the half-open
// range [start, end). taskID may be nil for gap-query tests that don't care
// which task produced the coverage.
func insertTestInterval(t *testing.T, database *db.DB, unitID uuid.UUID, taskID *uuid.UUID, start, end int64, status string) uuid.UUID {
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

// testTask describes the job_tasks row a test wants. Only the fields the
// recovery and sweeper paths actually read are configurable; everything else
// takes a harmless default.
type testTask struct {
	// Status is job_tasks.status. Empty means 'running'.
	Status string
	// RangeStart / RangeEnd are the half-open task range in BASE units. They
	// are mirrored into the legacy keyspace_start / keyspace_end columns, the
	// same way the dispatcher does it.
	RangeStart int64
	RangeEnd   int64
	// RestorePoint is hashcat's restore point. nil means "never reported",
	// which recovery treats as no progress.
	RestorePoint *int64
	// CrackCount drives which completed_* detailed_status the truncate branch
	// writes.
	CrackCount int
	// AgentID nil is the unassigned shape — what ClearTaskAgentAndSetPending
	// left behind and what the stranded-task sweep looks for.
	AgentID *int
	// UpdatedAt seeds job_tasks.updated_at at INSERT time. Zero means now.
	UpdatedAt time.Time
}

// insertTestTask inserts a scheduler-v2 job_tasks row matching spec and returns
// its ID.
//
// updated_at is written by the INSERT on purpose: job_tasks carries a BEFORE
// UPDATE trigger (migration 000026) that stamps NOW() on every UPDATE, so a
// row's age can only be backdated at insert time — which the stranded-task age
// guard needs.
func insertTestTask(t *testing.T, database *db.DB, jobID, unitID uuid.UUID, spec testTask) uuid.UUID {
	t.Helper()

	status := spec.Status
	if status == "" {
		status = "running"
	}
	updatedAt := spec.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	taskID := uuid.New()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO job_tasks (
			id, job_execution_id, scheduling_unit_id, agent_id, status,
			keyspace_start, keyspace_end, chunk_duration,
			range_start, range_end, restore_point,
			effective_keyspace_start, effective_keyspace_end,
			crack_count, is_keyspace_split, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, 60,
			$6, $7, $8,
			$9, $10,
			$11, true, $12, $12
		)
	`, taskID, jobID, unitID, spec.AgentID, status,
		spec.RangeStart, spec.RangeEnd, spec.RestorePoint,
		spec.RangeStart, spec.RangeEnd,
		spec.CrackCount, updatedAt)
	if err != nil {
		t.Fatalf("failed to create test job_task [%d,%d) %s: %v", spec.RangeStart, spec.RangeEnd, status, err)
	}
	return taskID
}

// int64Ptr is the usual addressable-literal helper, used for testTask's
// optional RestorePoint.
func int64Ptr(v int64) *int64 { return &v }

// taskState is the subset of job_tasks the recovery assertions read back.
type taskState struct {
	Status            string
	DetailedStatus    string
	RangeEnd          sql.NullInt64
	KeyspaceEnd       int64
	RestorePoint      sql.NullInt64
	KeyspaceProcessed sql.NullInt64
	ProgressPercent   float64
	FailureReason     sql.NullString
}

func readTaskState(t *testing.T, database *db.DB, taskID uuid.UUID) taskState {
	t.Helper()

	var st taskState
	err := database.QueryRowContext(context.Background(), `
		SELECT status, COALESCE(detailed_status, ''), range_end, keyspace_end,
		       restore_point, keyspace_processed, COALESCE(progress_percent, 0), failure_reason
		FROM job_tasks WHERE id = $1
	`, taskID).Scan(&st.Status, &st.DetailedStatus, &st.RangeEnd, &st.KeyspaceEnd,
		&st.RestorePoint, &st.KeyspaceProcessed, &st.ProgressPercent, &st.FailureReason)
	if err != nil {
		t.Fatalf("failed to read task %s: %v", taskID, err)
	}
	return st
}

// intervalState is the subset of job_keyspace_intervals the assertions read.
type intervalState struct {
	RangeStart int64
	RangeEnd   int64
	Status     string
}

func readIntervalState(t *testing.T, database *db.DB, intervalID uuid.UUID) intervalState {
	t.Helper()

	var iv intervalState
	err := database.QueryRowContext(context.Background(), `
		SELECT range_start, range_end, status FROM job_keyspace_intervals WHERE id = $1
	`, intervalID).Scan(&iv.RangeStart, &iv.RangeEnd, &iv.Status)
	if err != nil {
		t.Fatalf("failed to read interval %s: %v", intervalID, err)
	}
	return iv
}

// assertNoStrandedIntervals is the GH #77 invariant, checked after every
// recovery path: a task that is pending or terminal must never leave its
// keyspace interval live. A 'pending' task holding an 'assigned' interval is
// precisely the stranding — firstGap counts the range as covered while no agent
// is working it, so the unit looks fully tiled forever and the job never
// completes.
func assertNoStrandedIntervals(t *testing.T, database *db.DB) {
	t.Helper()

	var n int
	err := database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM job_tasks t
		JOIN job_keyspace_intervals i ON i.task_id = t.id
		WHERE t.status IN ('pending', 'failed', 'cancelled')
		  AND i.status IN ('assigned', 'running')
	`).Scan(&n)
	if err != nil {
		t.Fatalf("stranded-interval invariant query failed: %v", err)
	}
	if n != 0 {
		t.Errorf("stranded-interval invariant violated: %d task/interval pair(s) where a pending-or-terminal task still holds a live interval", n)
	}
}
