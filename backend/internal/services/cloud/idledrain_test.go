package cloud

import (
	"context"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * Idle drain (C8).
 *
 * Every other teardown tier fires on something being WRONG: a failed launch, an
 * expired TTL, an exhausted budget. None of them fires on the ordinary happy
 * ending — a job that finishes at 14:02 on an instance rented until 18:00. That
 * is four hours of GPU billing with nothing to do, on every successful job, and
 * cloud_idle_drain_minutes was the setting that claimed to prevent it while
 * being read by nothing at all.
 */

// idleFixture builds a reaper with a real job and a registered agent, so the
// "does this instance still have work?" query has something to answer about.
type idleFixture struct {
	*reaperFixture
	job     testutil.CloudJob
	agentID int
}

func newIdleFixture(t *testing.T, idleDrain time.Duration) *idleFixture {
	t.Helper()
	return newIdleFixtureWithCap(t, idleDrain, 100_000)
}

// newIdleFixtureWithCap is newIdleFixture with a controllable budget ceiling,
// so the drain tests can park a client at a chosen percentage of its cap.
func newIdleFixtureWithCap(t *testing.T, idleDrain time.Duration, capCents int64) *idleFixture {
	t.Helper()
	f := newReaperFixture(t, capCents)
	f.reaper.IdleDrain = idleDrain

	job := testutil.CreateCloudJob(t, f.database, f.clientID, true)
	var owner uuid.UUID
	if err := f.database.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&owner); err != nil {
		u := testutil.CreateTestUser(t, f.database, "idle-"+uuid.NewString()[:8],
			"idle-"+uuid.NewString()[:8]+"@test.local", "pw", "admin")
		owner = u.ID
	}
	agentID := testutil.CreateTestAgent(t, f.database, owner, nil)

	return &idleFixture{reaperFixture: f, job: job, agentID: agentID}
}

// liveInstance creates a running instance attached to the fixture's job/agent.
func (f *idleFixture) liveInstance(t *testing.T, readyAgo time.Duration) uuid.UUID {
	t.Helper()
	id, _ := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		State:              "running",
		ClientID:           &f.clientID,
		JobExecutionID:     &f.job.JobID,
		AgentID:            &f.agentID,
		HourlyRateCents:    100,
		ReservedCents:      1000,
		ProviderInstanceID: "prov-" + uuid.NewString()[:8],
		TTLEpoch:           ahead(4 * time.Hour),
		ReadyDeadlineAt:    ahead(3 * time.Hour),
		LaunchDeadlineAt:   ahead(3 * time.Hour),
	})
	// ready_at is not settable through InstanceOpts and is what an
	// instance-that-never-worked is measured from.
	if _, err := f.database.Exec(
		`UPDATE cloud_instances SET ready_at = NOW() - ($2 || ' seconds')::interval WHERE id = $1`,
		id, int(readyAgo.Seconds())); err != nil {
		t.Fatalf("set ready_at: %v", err)
	}
	return id
}

func (f *idleFixture) setJobStatus(t *testing.T, status string) {
	t.Helper()
	if _, err := f.database.Exec(
		`UPDATE job_executions SET status = $2 WHERE id = $1`, f.job.JobID, status); err != nil {
		t.Fatalf("set job status: %v", err)
	}
}

// recordTask inserts a job_task for the fixture's agent, finished `agoDur` ago.
func (f *idleFixture) recordTask(t *testing.T, agoDur time.Duration) {
	t.Helper()
	_, err := f.database.Exec(`
		INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
		                       keyspace_start, keyspace_end, chunk_duration,
		                       assigned_at, started_at, completed_at)
		VALUES ($1, $2, $3, 'completed', 0, 100, 60,
		        NOW() - ($4 || ' seconds')::interval,
		        NOW() - ($4 || ' seconds')::interval,
		        NOW() - ($4 || ' seconds')::interval)`,
		uuid.New(), f.job.JobID, f.agentID, int(agoDur.Seconds()))
	if err != nil {
		t.Fatalf("insert job task: %v", err)
	}
}

/*
 * TestIdleDrain_FinishedJobIsTornDownImmediately.
 *
 * No grace period here on purpose. A terminal job cannot produce more work, so
 * every second the instance stays up is billed for nothing, and unlike the
 * between-chunks case there is no risk of throwing away work in flight.
 */
func TestIdleDrain_FinishedJobIsTornDownImmediately(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	id := f.liveInstance(t, time.Minute)
	f.setJobStatus(t, "completed")
	// A task that finished seconds ago: the instance is not idle by time, only
	// by the job being over.
	f.recordTask(t, 10*time.Second)

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got != models.CloudInstanceTerminated {
		t.Fatalf("instance state = %q after its job completed, want terminated; a "+
			"rented GPU outlives every successful job until its TTL otherwise", got)
	}
}

// TestIdleDrain_RunningJobKeepsItsInstance: the common case must not regress
// into tearing down instances that are working.
func TestIdleDrain_RunningJobKeepsItsInstance(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	id := f.liveInstance(t, time.Minute)
	f.setJobStatus(t, "running")
	f.recordTask(t, 10*time.Second)

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got == models.CloudInstanceTerminated {
		t.Fatal("destroyed an instance whose agent finished a task 10 seconds ago")
	}
}

/*
 * TestIdleDrain_FiresAfterTheGrace: a genuinely idle instance on a live job.
 * The job may still have keyspace left but this agent is getting none of it.
 */
func TestIdleDrain_FiresAfterTheGrace(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	id := f.liveInstance(t, time.Hour)
	f.setJobStatus(t, "running")
	f.recordTask(t, 30*time.Minute)

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got != models.CloudInstanceTerminated {
		t.Fatalf("instance state = %q after 30 minutes with no task and a 5 minute "+
			"drain, want terminated", got)
	}
}

/*
 * TestIdleDrain_BetweenChunksIsNotIdle.
 *
 * The grace period exists for exactly this: a gap between chunks is normal, and
 * tearing down during one throws away the launch that was just paid for.
 */
func TestIdleDrain_BetweenChunksIsNotIdle(t *testing.T) {
	f := newIdleFixture(t, 30*time.Minute)
	id := f.liveInstance(t, time.Hour)
	f.setJobStatus(t, "running")
	f.recordTask(t, 2*time.Minute)

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got == models.CloudInstanceTerminated {
		t.Fatal("destroyed an instance two minutes into a gap between chunks, with a " +
			"30 minute drain configured")
	}
}

/*
 * TestCommissioning_NeverWorkedIsMeasuredFromReady.
 *
 * An instance with no task history has no "last activity" at all, so it is
 * judged by CommissioningGrace measured from ready_at — NOT by IdleDrain.
 * Measuring from zero would make every instance look infinitely idle the moment
 * it registered and destroy it before its first chunk ever arrived.
 *
 * IdleDrain is set absurdly low here on purpose: it must have no effect on an
 * instance that has never worked. Before the split, that 1-minute value would
 * have destroyed the fresh instance.
 */
func TestCommissioning_NeverWorkedIsMeasuredFromReady(t *testing.T) {
	f := newIdleFixture(t, time.Minute)
	f.reaper.CommissioningGrace = 30 * time.Minute
	f.setJobStatus(t, "running")

	fresh := f.liveInstance(t, 15*time.Minute)
	f.reaper.SweepOnce(context.Background())
	if got := f.state(t, fresh); got == models.CloudInstanceTerminated {
		t.Fatal("destroyed an instance 15 minutes into commissioning with a 30 minute " +
			"grace — this is the cold-start kill that produced the rent/kill/rent loop")
	}

	stale := f.liveInstance(t, 45*time.Minute)
	f.reaper.SweepOnce(context.Background())
	if got := f.state(t, stale); got != models.CloudInstanceTerminated {
		t.Fatalf("instance ready 45 minutes ago with a 30 minute commissioning grace and "+
			"no task ever assigned: state = %q, want terminated", got)
	}
}

/*
 * TestCommissioning_WedgedInstanceStillDies.
 *
 * The ceiling is absolute. An agent stuck in sync_status='in_progress' — the
 * state nothing else clears if it dies in a way readPump misses — must still be
 * destroyed, because it bills every minute. This is why the clock is measured
 * from ready_at, which is written once, rather than from any signal the agent
 * can refresh.
 */
func TestCommissioning_WedgedInstanceStillDies(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	f.reaper.CommissioningGrace = 30 * time.Minute
	f.setJobStatus(t, "running")

	id := f.liveInstance(t, 35*time.Minute)
	if _, err := f.database.Exec(
		`UPDATE agents SET sync_status = 'in_progress', sync_started_at = NOW() WHERE id = $1`,
		f.agentID); err != nil {
		t.Fatalf("failed to wedge the agent in sync: %v", err)
	}

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got != models.CloudInstanceTerminated {
		t.Fatalf("an instance wedged in sync past its commissioning grace survived "+
			"(state = %q); a freshly-stamped sync_started_at must not buy more time, "+
			"or a wedged agent bills forever", got)
	}
}

// TestIdleDrain_DisabledByZero: the setting's 0 must switch the behaviour off,
// not switch it to "destroy everything immediately".
//
// The instance is given a task so this exercises the idle-drain path. Before
// the commissioning split this test used an instance that had never worked,
// which now belongs to CommissioningGrace instead — see
// TestCommissioning_DisabledByZero.
func TestIdleDrain_DisabledByZero(t *testing.T) {
	f := newIdleFixture(t, 0)
	id := f.liveInstance(t, 6*time.Hour)
	f.setJobStatus(t, "running")
	f.recordTask(t, 6*time.Hour)

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got == models.CloudInstanceTerminated {
		t.Fatal("idle drain is disabled (0) but an idle instance was destroyed anyway")
	}
}

// TestCommissioning_DisabledByZero mirrors the above for the new clock, so an
// operator who deliberately turns commissioning teardown off gets that, rather
// than immediate destruction.
func TestCommissioning_DisabledByZero(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	f.reaper.CommissioningGrace = 0
	f.setJobStatus(t, "running")

	id := f.liveInstance(t, 6*time.Hour)
	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got == models.CloudInstanceTerminated {
		t.Fatal("commissioning teardown is disabled (0) but a never-commissioned " +
			"instance was destroyed anyway")
	}
}

/*
 * TestIdleDrain_MissingJobIsTornDown: a job row that no longer exists cannot
 * ever need this instance, and nothing else in the ladder notices.
 */
func TestIdleDrain_MissingJobIsTornDown(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	id := f.liveInstance(t, time.Minute)

	// Detach the instance from the job, then delete the job.
	if _, err := f.database.Exec(
		`UPDATE job_tasks SET agent_id = NULL WHERE job_execution_id = $1`, f.job.JobID); err != nil {
		t.Fatalf("detach tasks: %v", err)
	}
	if _, err := f.database.Exec(`DELETE FROM job_tasks WHERE job_execution_id = $1`, f.job.JobID); err != nil {
		t.Fatalf("delete tasks: %v", err)
	}
	if _, err := f.database.Exec(`DELETE FROM job_executions WHERE id = $1`, f.job.JobID); err != nil {
		t.Skipf("job row cannot be deleted in this schema (%v); the missing-job branch "+
			"is unreachable here", err)
	}

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got != models.CloudInstanceTerminated {
		t.Fatalf("instance state = %q after its job row vanished, want terminated", got)
	}
}
