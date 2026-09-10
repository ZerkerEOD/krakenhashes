package cloud

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * The crack-drain grace, and the activity signal it sits on top of.
 *
 * Two bugs, found by watching a rented instance in the $0 mock rehearsal:
 *
 *  1. LastActivityAt was GREATEST over assigned_at/started_at/completed_at, none
 *     of which move during a chunk. With cloud chunks floored at 3600s and the
 *     idle drain at 5 minutes, EVERY rented instance died five minutes into its
 *     first chunk. Seen on real AWS hardware and on the mock provider.
 *  2. The job-finished rung destroyed with zero grace, on the ordinary
 *     successful path, while the agent was still uploading cracks. Those cracks
 *     are unrecoverable: the outfile dies with the disk and applyRecovery books
 *     the range as covered so it is never re-run.
 */

/*
 * inFlightTask inserts a non-terminal task whose last write was `quiet` ago.
 *
 * Inserted stale rather than backdated afterwards: update_job_tasks_updated_at
 * is a BEFORE UPDATE trigger (migration 000026) that overwrites updated_at with
 * NOW(), so any attempt to age a row with an UPDATE silently makes it fresh
 * again and the test passes for the wrong reason.
 */
func (f *idleFixture) inFlightTask(t *testing.T, status string, startedAgo, quiet time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.database.Exec(`
		INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
		                       keyspace_start, keyspace_end, chunk_duration,
		                       assigned_at, started_at, last_activity_at, updated_at)
		VALUES ($1, $2, $3, $4, 0, 100, 3600,
		        NOW() - ($5 || ' seconds')::interval,
		        NOW() - ($5 || ' seconds')::interval,
		        NOW() - ($6 || ' seconds')::interval,
		        NOW() - ($6 || ' seconds')::interval)`,
		id, f.job.JobID, f.agentID, status,
		int(startedAgo.Seconds()), int(quiet.Seconds()))
	if err != nil {
		t.Fatalf("insert %s task: %v", status, err)
	}
	return id
}

func (f *idleFixture) instanceState(t *testing.T, id uuid.UUID) models.CloudInstanceState {
	t.Helper()
	return f.state(t, id)
}

/*
 * TestCrackDrain_LongRunningChunkIsNotIdle.
 *
 * Pins bug 1. A cloud chunk is floored at 3600s while the idle drain is 5
 * minutes, so without last_activity_at in the activity signal this instance is
 * destroyed mid-chunk every time — which is what made cloud provisioning unable
 * to finish any chunk at all.
 */
func TestCrackDrain_LongRunningChunkIsNotIdle(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	inst := f.liveInstance(t, 2*time.Hour)
	// Started 45 minutes ago, but reported progress 5 seconds ago.
	f.inFlightTask(t, "running", 45*time.Minute, 5*time.Second)

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got == models.CloudInstanceTerminated {
		t.Error("destroyed an instance whose agent reported progress 5 seconds ago; " +
			"a rented GPU cannot finish a 3600s chunk under a 5-minute idle drain")
	}
}

/*
 * TestCrackDrain_ProcessingTaskSuppressesJobFinishedTeardown.
 *
 * Pins bug 2, and it is the exact incident observed: the job completes off the
 * all-hashes-cracked signal while the triggering task is still draining crack
 * batches, and the reaper destroyed the disk holding them with no grace at all.
 */
func TestCrackDrain_ProcessingTaskSuppressesJobFinishedTeardown(t *testing.T) {
	f := newIdleFixture(t, time.Hour) // idle drain out of the way
	inst := f.liveInstance(t, 2*time.Hour)
	f.inFlightTask(t, "processing", 10*time.Minute, 5*time.Second)
	f.setJobStatus(t, "completed")

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got == models.CloudInstanceTerminated {
		t.Error("destroyed an instance mid-crack-upload because the job was marked complete; " +
			"those cracks cannot be retransmitted from a machine that no longer exists")
	}
}

// TestCrackDrain_ProcessingTaskSuppressesIdleDrain: the same protection on the
// idle rung, for the window after hashcat exits when only crack batches move.
func TestCrackDrain_ProcessingTaskSuppressesIdleDrain(t *testing.T) {
	f := newIdleFixture(t, time.Minute)
	inst := f.liveInstance(t, 2*time.Hour)
	// last_activity_at is old (hashcat exited), but updated_at is fresh
	// because crack batches are still landing.
	id := f.inFlightTask(t, "processing", 30*time.Minute, 30*time.Minute)
	if _, err := f.database.Exec(
		`UPDATE job_tasks SET received_crack_count = received_crack_count + 1 WHERE id = $1`, id); err != nil {
		t.Fatalf("simulate crack batch arriving: %v", err)
	}

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got == models.CloudInstanceTerminated {
		t.Error("idle drain destroyed an instance that was still receiving crack batches")
	}
}

/*
 * TestCrackDrain_BoundEventuallyFires.
 *
 * The hold must not be indefinite. A wedged task that has produced no write for
 * longer than the grace stops protecting the instance, so a stuck handshake
 * cannot hold a rented GPU to its TTL.
 */
func TestCrackDrain_BoundEventuallyFires(t *testing.T) {
	f := newIdleFixture(t, time.Hour)
	f.reaper.CrackDrainGrace = 10 * time.Minute
	diag := &fakeDiag{}
	f.reaper.Diagnostics = diag
	inst := f.liveInstance(t, 2*time.Hour)
	f.inFlightTask(t, "processing", 30*time.Minute, 20*time.Minute) // silent 20m > 10m grace
	f.setJobStatus(t, "completed")

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got != models.CloudInstanceTerminated {
		t.Fatalf("state = %s, want terminated once the crack-drain grace expired", got)
	}
	// The operator sees a completed job with missing cracks and no other clue,
	// so the give-up path must say so.
	if !diag.has(models.DiagReasonCloudCracksLost) {
		t.Errorf("no %s diagnostic recorded; the loss would be entirely invisible",
			models.DiagReasonCloudCracksLost)
	}
}

// TestCrackDrain_IdleAgentIsStillDestroyed: no regression of the feature's
// purpose. An agent holding nothing must still be reaped promptly.
func TestCrackDrain_IdleAgentIsStillDestroyed(t *testing.T) {
	f := newIdleFixture(t, 5*time.Minute)
	inst := f.liveInstance(t, 2*time.Hour)
	f.recordTask(t, 30*time.Minute) // completed 30 minutes ago

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got != models.CloudInstanceTerminated {
		t.Errorf("state = %s, want terminated — a genuinely idle rented GPU must still be reaped", got)
	}
}

/*
 * TestCrackDrain_StaleForeignTaskDoesNotPinTheInstance.
 *
 * The in-flight aggregate is agent-scoped, so a non-terminal row from ANOTHER
 * job on the same agent is visible to it. The freshness test is what stops that
 * becoming indefinite suppression — this asserts it does.
 */
func TestCrackDrain_StaleForeignTaskDoesNotPinTheInstance(t *testing.T) {
	f := newIdleFixture(t, time.Hour)
	f.reaper.CrackDrainGrace = 10 * time.Minute
	inst := f.liveInstance(t, 2*time.Hour)

	// A second job for the same client, with a long-dead processing row on the
	// SAME agent.
	other := testutil.CreateCloudJob(t, f.database, f.clientID, true)
	if _, err := f.database.Exec(`
		INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
		                       keyspace_start, keyspace_end, chunk_duration,
		                       assigned_at, started_at, last_activity_at, updated_at)
		VALUES ($1, $2, $3, 'processing', 0, 100, 3600,
		        NOW() - INTERVAL '40 minutes', NOW() - INTERVAL '40 minutes',
		        NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '30 minutes')`,
		uuid.New(), other.JobID, f.agentID); err != nil {
		t.Fatalf("insert foreign stale task: %v", err)
	}
	f.setJobStatus(t, "completed")

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got != models.CloudInstanceTerminated {
		t.Errorf("state = %s, want terminated — a stale row from another job must not "+
			"hold a rented GPU to its TTL", got)
	}
}

// TestCrackDrain_DisabledByZero pins the "0 disables" convention every other
// cloud grace knob follows.
func TestCrackDrain_DisabledByZero(t *testing.T) {
	f := newIdleFixture(t, time.Hour)
	f.reaper.CrackDrainGrace = 0
	inst := f.liveInstance(t, 2*time.Hour)
	f.inFlightTask(t, "processing", 10*time.Minute, 5*time.Second)
	f.setJobStatus(t, "completed")

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got != models.CloudInstanceTerminated {
		t.Errorf("state = %s, want terminated with the grace disabled", got)
	}
}

/*
 * TestCrackDrain_TTLStaysUnconditional.
 *
 * The same epoch is armed inside the guest, which will poweroff regardless. A
 * backend-side grace here would be a promise the guest does not honour, so this
 * rung must ignore in-flight work.
 */
func TestCrackDrain_TTLStaysUnconditional(t *testing.T) {
	f := newIdleFixture(t, time.Hour)
	inst := f.liveInstance(t, 2*time.Hour)
	f.inFlightTask(t, "processing", time.Minute, 5*time.Second)
	if _, err := f.database.Exec(
		`UPDATE cloud_instances SET ttl_epoch = NOW() - INTERVAL '1 minute' WHERE id = $1`, inst); err != nil {
		t.Fatalf("expire ttl: %v", err)
	}

	f.reaper.SweepOnce(context.Background())

	if got := f.instanceState(t, inst); got != models.CloudInstanceTerminated {
		t.Errorf("state = %s, want terminated — TTL must stay unconditional", got)
	}
}

// TestCrackDrainGraceBounds asserts the two invariants that keep the grace
// wedged between the rung below it and the sweep above it. Pure, no DB.
func TestCrackDrainGraceBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		idle  time.Duration
		crack time.Duration
	}{
		{"DefaultSettings", DefaultSettings().IdleDrain, DefaultSettings().CrackDrainGrace},
		{"NewReaper", NewReaper(nil, nil, nil, nil).IdleDrain, NewReaper(nil, nil, nil, nil).CrackDrainGrace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.crack <= tc.idle {
				t.Errorf("CrackDrainGrace (%s) must exceed IdleDrain (%s): an agent holding work "+
					"deserves strictly more patience than one holding nothing", tc.crack, tc.idle)
			}
			if tc.crack >= services.StaleProcessingTimeout {
				t.Errorf("CrackDrainGrace (%s) must be under StaleProcessingTimeout (%s), or the "+
					"reaper holds a rented GPU waiting for a handshake the cleanup sweep has "+
					"already abandoned", tc.crack, services.StaleProcessingTimeout)
			}
		})
	}
}

// helper: assert a reason code was recorded.
func (d *fakeDiag) has(reason string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.recorded {
		if strings.Contains(r, reason) {
			return true
		}
	}
	return false
}
