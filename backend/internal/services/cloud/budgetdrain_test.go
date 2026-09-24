package cloud

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * The budget ladder's drain rung.
 *
 * As shipped, drain_pct wrote cloud_instances.state = 'draining' and stopped
 * there. Nothing read the state — getIdleAgents had no ci.state filter — so a
 * draining instance kept receiving fresh chunks right up until the 100% rung
 * hard-killed it mid-chunk. drain_timeout_seconds was stored, validated, and
 * read by no service. The ladder was 95% stop-new-launches -> 100% kill, with a
 * cosmetic rung in between.
 *
 * IdleDrain is set to an hour in these fixtures so that any destruction is
 * unambiguously attributable to the drain rung rather than the idle tier.
 */

// drainFixture parks a client just over its drain threshold.
type drainFixture struct {
	*idleFixture
}

// newDrainFixture builds a client at usedPct of a 100-cent cap. The seeded
// default policy is 80/95/99/100 with a 300s drain timeout, so 99 lands exactly
// on the drain rung and 100 on hard stop.
func newDrainFixture(t *testing.T, usedCents int64) *drainFixture {
	t.Helper()
	f := newIdleFixtureWithCap(t, time.Hour, 100)
	testutil.InsertLedgerEntry(t, f.database, f.clientID, nil, usedCents, "reservation", time.Now())
	return &drainFixture{idleFixture: f}
}

// runningTask inserts an in-flight task for the fixture's agent.
func (f *drainFixture) runningTask(t *testing.T, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.database.Exec(`
		INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
		                       keyspace_start, keyspace_end, chunk_duration,
		                       assigned_at, started_at)
		VALUES ($1, $2, $3, $4, 0, 100, 60, NOW(), NOW())`,
		id, f.job.JobID, f.agentID, status)
	if err != nil {
		t.Fatalf("insert %s task: %v", status, err)
	}
	return id
}

func (f *drainFixture) drainStartedAt(t *testing.T, id uuid.UUID) *time.Time {
	t.Helper()
	var at *time.Time
	if err := f.database.QueryRow(
		`SELECT drain_started_at FROM cloud_instances WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatalf("read drain_started_at: %v", err)
	}
	return at
}

func (f *drainFixture) terminationReason(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s *string
	if err := f.database.QueryRow(
		`SELECT termination_reason FROM cloud_instances WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read termination_reason: %v", err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func (f *drainFixture) setDrainTimeout(t *testing.T, seconds int) {
	t.Helper()
	if _, err := f.database.Exec(
		`UPDATE cloud_budget_policies SET drain_timeout_seconds = $1 WHERE client_id IS NULL`,
		seconds); err != nil {
		t.Fatalf("set drain timeout: %v", err)
	}
}

// TestDrain_MarksAndStampsTheClock: reaching drain_pct with work in flight
// marks the instance and starts the clock, without destroying it.
func TestDrain_MarksAndStampsTheClock(t *testing.T) {
	f := newDrainFixture(t, 99)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got != models.CloudInstanceDraining {
		t.Fatalf("state = %s, want draining", got)
	}
	if f.drainStartedAt(t, inst) == nil {
		t.Error("drain_started_at is NULL — drain_timeout_seconds has nothing to measure from")
	}
}

/*
 * TestDrain_InFlightWorkIsNotDestroyed.
 *
 * Two sweeps, asserting drain_started_at does NOT move. The reaper re-evaluates
 * the ladder every sweep, so an unconditional NOW() here would restart the
 * timeout every 60 seconds and it could never fire — the instance would drain
 * forever at >=99% of cap.
 */
func TestDrain_InFlightWorkIsNotDestroyed(t *testing.T) {
	f := newDrainFixture(t, 99)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	first := f.drainStartedAt(t, inst)
	if first == nil {
		t.Fatal("drain_started_at not stamped on the first sweep")
	}

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got != models.CloudInstanceDraining {
		t.Errorf("state = %s, want the instance to still be draining with work in flight", got)
	}
	second := f.drainStartedAt(t, inst)
	if second == nil || !second.Equal(*first) {
		t.Errorf("drain_started_at moved (%v -> %v); the timeout would never fire", first, second)
	}
}

// TestDrain_DestroysOnceWorkFinishes: the rung's actual promise — let in-flight
// work finish, then go.
func TestDrain_DestroysOnceWorkFinishes(t *testing.T) {
	f := newDrainFixture(t, 99)
	inst := f.liveInstance(t, 2*time.Hour)
	task := f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	if got := f.state(t, inst); got != models.CloudInstanceDraining {
		t.Fatalf("state = %s, want draining after the first sweep", got)
	}

	if _, err := f.database.Exec(
		`UPDATE job_tasks SET status = 'completed', completed_at = NOW() WHERE id = $1`, task); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got != models.CloudInstanceTerminated {
		t.Fatalf("state = %s, want terminated once nothing is in flight", got)
	}
	// Names the drain path specifically, so this cannot pass on an idle-drain
	// or TTL teardown that happened to fire.
	if reason := f.terminationReason(t, inst); !strings.Contains(reason, "drained") {
		t.Errorf("termination_reason = %q, want it to name the drain rung", reason)
	}
}

// TestDrain_TimeoutDestroysDespiteWorkInFlight: drain_timeout_seconds is the
// bound on how long "let it finish" is allowed to cost.
func TestDrain_TimeoutDestroysDespiteWorkInFlight(t *testing.T) {
	f := newDrainFixture(t, 99)
	f.setDrainTimeout(t, 60)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	if got := f.state(t, inst); got != models.CloudInstanceDraining {
		t.Fatalf("state = %s, want draining", got)
	}

	// Backdate the clock past the timeout; the task stays running.
	if _, err := f.database.Exec(
		`UPDATE cloud_instances SET drain_started_at = NOW() - INTERVAL '5 minutes' WHERE id = $1`,
		inst); err != nil {
		t.Fatalf("backdate drain clock: %v", err)
	}
	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got != models.CloudInstanceTerminated {
		t.Fatalf("state = %s, want terminated once the drain timeout elapsed", got)
	}
	if reason := f.terminationReason(t, inst); !strings.Contains(reason, "drain timeout") {
		t.Errorf("termination_reason = %q, want it to name the timeout", reason)
	}
}

/*
 * TestDrain_ProcessingTaskCountsAsInFlight.
 *
 * 'processing' means the agent is still uploading crack batches. Destroying the
 * VM there loses cracks it has already found — the one outcome strictly worse
 * than overspending by a minute.
 */
func TestDrain_ProcessingTaskCountsAsInFlight(t *testing.T) {
	f := newDrainFixture(t, 99)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "processing")

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got == models.CloudInstanceTerminated {
		t.Error("destroyed an instance still uploading crack batches; those cracks are lost")
	}
}

// TestDrain_ZeroTimeoutWaitsForInFlightWork pins the "0 disables" convention
// that every other cloud grace knob follows.
func TestDrain_ZeroTimeoutWaitsForInFlightWork(t *testing.T) {
	f := newDrainFixture(t, 99)
	f.setDrainTimeout(t, 0)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	if _, err := f.database.Exec(
		`UPDATE cloud_instances SET drain_started_at = NOW() - INTERVAL '1 hour' WHERE id = $1`,
		inst); err != nil {
		t.Fatalf("backdate drain clock: %v", err)
	}
	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got == models.CloudInstanceTerminated {
		t.Error("a zero drain timeout must not destroy work in flight")
	}
}

/*
 * TestDrain_ResumesWhenSpendFallsBack.
 *
 * A raised cap, a new budget period or a released reservation puts the client
 * back under the rung. Without resume, a transient spike costs a whole rental:
 * paid for, excluded from dispatch, then killed by the timeout without ever
 * doing the work it was rented for.
 */
func TestDrain_ResumesWhenSpendFallsBack(t *testing.T) {
	f := newDrainFixture(t, 99)
	inst := f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	if got := f.state(t, inst); got != models.CloudInstanceDraining {
		t.Fatalf("state = %s, want draining", got)
	}

	// Release most of the reservation: spend drops well under every threshold.
	testutil.InsertLedgerEntry(t, f.database, f.clientID, nil, -95, "release", time.Now())
	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, inst); got != models.CloudInstanceRunning {
		t.Errorf("state = %s, want running again once spend fell back", got)
	}
	if at := f.drainStartedAt(t, inst); at != nil {
		t.Errorf("drain_started_at = %v, want NULL after resume — a stale clock would "+
			"destroy the instance immediately on the next drain", at)
	}
}

// TestNotify_FiresOncePerRung: CloudBudgetThreshold was fully built and called
// by nothing. It must alert on a rung change, and not on every sweep.
func TestNotify_FiresOncePerRung(t *testing.T) {
	f := newDrainFixture(t, 96) // stop_provision_pct is 95
	f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())
	f.reaper.SweepOnce(context.Background())

	alerts := f.notifier.budgetAlerts()
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts across two sweeps at the same rung, want 1: %v", len(alerts), alerts)
	}
	if alerts[0] != BudgetActionStopProvisioning {
		t.Errorf("alert = %v, want StopProvisioning — a silent 95%% rung is when an "+
			"operator most needs to be told", alerts[0])
	}

	// Cross to the drain rung: a NEW rung must alert again.
	testutil.InsertLedgerEntry(t, f.database, f.clientID, nil, 3, "reservation", time.Now())
	f.reaper.SweepOnce(context.Background())

	alerts = f.notifier.budgetAlerts()
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts after crossing to a new rung, want 2: %v", len(alerts), alerts)
	}
	if alerts[1] != BudgetActionDrain {
		t.Errorf("second alert = %v, want Drain", alerts[1])
	}
}

/*
 * TestNotify_IsPerClientNotPerInstance.
 *
 * The ladder is assessed once per INSTANCE per sweep. Without dedup, a client
 * with several rented boxes pages admins once per box every 60 seconds — which
 * is the difference between a usable alert and one everybody mutes.
 */
func TestNotify_IsPerClientNotPerInstance(t *testing.T) {
	f := newDrainFixture(t, 96)
	f.liveInstance(t, 2*time.Hour)
	f.liveInstance(t, 2*time.Hour)
	f.runningTask(t, "running")

	f.reaper.SweepOnce(context.Background())

	if alerts := f.notifier.budgetAlerts(); len(alerts) != 1 {
		t.Errorf("got %d alerts for one over-threshold client with two instances, want 1: %v",
			len(alerts), alerts)
	}
}
