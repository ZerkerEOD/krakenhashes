package cloud

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * These are DB-backed on purpose. Both behaviours under test live entirely in
 * SQL — a cloud-agent join and a NULL-safe salt_count predicate — so a stubbed
 * test would assert nothing about the thing that can actually be wrong.
 */

// seedRunningAgent attaches an agent to the job with a running task and a
// benchmark row, and returns the agent id. cloudInstance nil means on-prem.
func seedRunningAgent(t *testing.T, f *provisionFixture, cloudInstance *uuid.UUID, speed int64, saltCount *int) int {
	t.Helper()

	agentID := testutil.CreateTestAgent(t, f.db, f.job.UserID, cloudInstance)

	// CreateCloudJob builds user -> client -> hashlist -> job_execution but no
	// scheduling units, and the projection is entirely unit-driven. Create one
	// on first use so repeated calls share it.
	var unitID uuid.UUID
	err := f.db.QueryRow(
		`SELECT id FROM scheduling_units WHERE parent_job_id = $1 LIMIT 1`, f.job.JobID).Scan(&unitID)
	if err != nil {
		unitID = uuid.New()
		if _, insErr := f.db.Exec(`
			INSERT INTO scheduling_units (id, parent_job_id, layer_index, status, attack_mode,
			                              effective_keyspace, base_keyspace, is_accurate_keyspace)
			VALUES ($1, $2, 0, 'running', 0, 1000000, 1000000, true)`,
			unitID, f.job.JobID); insErr != nil {
			t.Fatalf("create scheduling unit: %v", insErr)
		}
	}

	var attackMode int
	var hashType int
	if err := f.db.QueryRow(`
		SELECT su.attack_mode, h.hash_type_id
		FROM scheduling_units su
		JOIN job_executions je ON je.id = su.parent_job_id
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE su.id = $1`, unitID).Scan(&attackMode, &hashType); err != nil {
		t.Fatalf("read unit combo: %v", err)
	}

	if _, err := f.db.Exec(`
		INSERT INTO job_tasks (id, job_execution_id, scheduling_unit_id, agent_id, status,
		                       keyspace_start, keyspace_end, chunk_duration)
		VALUES ($1, $2, $3, $4, 'running', 0, 100, 600)`,
		uuid.New(), f.job.JobID, unitID, agentID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}

	if _, err := f.db.Exec(`
		INSERT INTO agent_benchmarks (agent_id, attack_mode, hash_type, speed, salt_count)
		VALUES ($1, $2, $3, $4, $5)`,
		agentID, attackMode, hashType, speed, saltCount); err != nil {
		t.Fatalf("insert benchmark: %v", err)
	}
	return agentID
}

/*
 * TestProject_CountsRunningCloudSpeed.
 *
 * The projection used to read on-prem agents only. In a cloud-only deployment
 * that made the total zero, TimeToFinishKnown false, and
 * skip_if_finishing_within_seconds permanently unable to fire — removing one of
 * the two brakes that stop the autoscaler renting for a job that is about to
 * finish anyway.
 */
func TestProject_CountsRunningCloudSpeed(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	instID, _ := testutil.CreateTestCloudInstance(t, f.db, f.cfgID, testutil.InstanceOpts{
		State:           "running",
		ClientID:        &f.clientID,
		JobExecutionID:  &f.job.JobID,
		HourlyRateCents: 100,
	})
	seedRunningAgent(t, f, &instID, 5000, nil)

	p, err := NewEstimator(f.db).Project(ctx, f.job.JobID, 0, 100, 100_000)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	if p.CloudSpeed != 5000 {
		t.Errorf("CloudSpeed = %d, want 5000 from the running cloud agent. Zero here means "+
			"the finishing-soon rule can never fire in a cloud-only deployment", p.CloudSpeed)
	}
	if p.OnPremSpeed != 0 {
		t.Errorf("OnPremSpeed = %d, want 0 — a cloud agent must not be counted as on-prem", p.OnPremSpeed)
	}
}

// extraCloudSpeed is a hypothetical ("what if I add one more instance") from the
// admin estimate dialog. It must ADD to observed cloud speed, not replace it,
// and observed speed must not be folded into it twice.
func TestProject_ExtraCloudSpeedAddsToRunning(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	instID, _ := testutil.CreateTestCloudInstance(t, f.db, f.cfgID, testutil.InstanceOpts{
		State:           "running",
		ClientID:        &f.clientID,
		JobExecutionID:  &f.job.JobID,
		HourlyRateCents: 100,
	})
	seedRunningAgent(t, f, &instID, 5000, nil)

	p, err := NewEstimator(f.db).Project(ctx, f.job.JobID, 3000, 100, 100_000)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.CloudSpeed != 8000 {
		t.Errorf("CloudSpeed = %d, want 8000 (5000 running + 3000 hypothetical)", p.CloudSpeed)
	}
}

/*
 * TestProject_SaltedBenchmarksAreNotDoubleCounted.
 *
 * agent_benchmarks is unique on (agent, attack_mode, hash_type, salt_count), so
 * a salted hash type legitimately has several rows per agent — one per salt
 * count benchmarked. The join had no salt_count predicate, so SUM(speed) added
 * them all and reported a fleet several times faster than it is. Every
 * projection built on that is short, which makes the finishing-soon rule fire
 * when it should not.
 *
 * The fixture's hash type is unsalted, so the correct match is salt_count IS
 * NULL and the decoy rows below must be ignored entirely.
 */
func TestProject_SaltedBenchmarksAreNotDoubleCounted(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	instID, _ := testutil.CreateTestCloudInstance(t, f.db, f.cfgID, testutil.InstanceOpts{
		State:           "running",
		ClientID:        &f.clientID,
		JobExecutionID:  &f.job.JobID,
		HourlyRateCents: 100,
	})
	agentID := seedRunningAgent(t, f, &instID, 5000, nil)

	// Extra rows for the same (agent, attack_mode, hash_type) at other salt
	// counts. Without the predicate these are summed in as if the agent were
	// three times faster.
	var attackMode, hashType int
	if err := f.db.QueryRow(
		`SELECT attack_mode, hash_type FROM agent_benchmarks WHERE agent_id = $1 LIMIT 1`,
		agentID).Scan(&attackMode, &hashType); err != nil {
		t.Fatalf("read seeded benchmark: %v", err)
	}
	for _, sc := range []int{10, 250} {
		salt := sc
		if _, err := f.db.Exec(`
			INSERT INTO agent_benchmarks (agent_id, attack_mode, hash_type, speed, salt_count)
			VALUES ($1, $2, $3, 999999, $4)`, agentID, attackMode, hashType, salt); err != nil {
			t.Fatalf("insert decoy salted benchmark: %v", err)
		}
	}

	p, err := NewEstimator(f.db).Project(ctx, f.job.JobID, 0, 100, 100_000)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.CloudSpeed != 5000 {
		t.Errorf("CloudSpeed = %d, want 5000. Benchmarks recorded at other salt counts were "+
			"summed in, so the fleet looks faster than it is and every projection is short",
			p.CloudSpeed)
	}
}

// A terminated instance's agent must not contribute throughput — it is gone,
// and counting it would make a job look like it has capacity it does not.
func TestProject_IgnoresTerminatedCloudInstances(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	instID, _ := testutil.CreateTestCloudInstance(t, f.db, f.cfgID, testutil.InstanceOpts{
		State:           "terminated",
		ClientID:        &f.clientID,
		JobExecutionID:  &f.job.JobID,
		HourlyRateCents: 100,
	})
	seedRunningAgent(t, f, &instID, 5000, nil)

	p, err := NewEstimator(f.db).Project(ctx, f.job.JobID, 0, 100, 100_000)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.CloudSpeed != 0 {
		t.Errorf("CloudSpeed = %d, want 0 for a terminated instance", p.CloudSpeed)
	}
}
