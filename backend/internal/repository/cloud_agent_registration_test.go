package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * Registering a rented agent has to establish two pointers in two tables, and
 * every consumer of "is this a cloud agent?" reads one or the other:
 *
 *   agents.cloud_instance_id  -> suppresses the full-corpus file sync
 *                                (which would ship every client potfile to a
 *                                machine the operator does not control), and
 *                                exempts the agent from the offline monitor.
 *   cloud_instances.agent_id  -> LoadAgentJobLocks builds the ENTIRE dispatch
 *                                isolation map from this column, and the reaper
 *                                reads a NULL as "never registered" and
 *                                destroys the instance at its ready deadline.
 *
 * Either one alone is a live defect, so these tests are about atomicity as much
 * as correctness.
 */

func newTestAgent(name string, cloudInstanceID *uuid.UUID, ownerID uuid.UUID) *models.Agent {
	now := time.Now()
	return &models.Agent{
		Name:            name,
		Status:          models.AgentStatusPending,
		CreatedByID:     ownerID,
		OwnerID:         &ownerID,
		CreatedAt:       now,
		UpdatedAt:       now,
		LastHeartbeat:   now,
		Version:         "0.0.0-test",
		APIKey:          sql.NullString{String: uuid.NewString(), Valid: true},
		APIKeyCreatedAt: sql.NullTime{Time: now, Valid: true},
		CloudInstanceID: cloudInstanceID,
	}
}

// agentOwner mints the user an agent is registered under. TruncateAll empties
// the users table, so every test creates its own rather than assuming one.
func agentOwner(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	suffix := uuid.NewString()[:8]
	u := testutil.CreateTestUser(t, database, "owner-"+suffix, "owner-"+suffix+"@test.local", "pw", "admin")
	return u.ID
}

/*
 * TestAgentCreate_CloudAgentSetsBothPointers is the core wiring assertion.
 */
func TestAgentCreate_CloudAgentSetsBothPointers(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()

	owner := agentOwner(t, database)
	client := testutil.CreateTestClient(t, database, "cloud-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})
	job := testutil.CreateCloudJob(t, database, client, true)
	cfg := testutil.CreateTestCloudProviderConfig(t, database, "mock", "mock-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{Enabled: true})
	instanceID, _ := testutil.CreateTestCloudInstance(t, database, cfg, testutil.InstanceOpts{
		State:          "provisioning",
		ClientID:       &client,
		JobExecutionID: &job.JobID,
	})

	repo := NewAgentRepository(database)
	agent := newTestAgent("cloud-agent-"+uuid.NewString()[:8], &instanceID, owner)
	if err := repo.Create(ctx, agent); err != nil {
		t.Fatalf("register cloud agent: %v", err)
	}

	var storedInstance uuid.NullUUID
	if err := database.QueryRow(
		`SELECT cloud_instance_id FROM agents WHERE id = $1`, agent.ID).Scan(&storedInstance); err != nil {
		t.Fatalf("read back agent: %v", err)
	}
	if !storedInstance.Valid || storedInstance.UUID != instanceID {
		t.Errorf("agents.cloud_instance_id = %v, want %s — without it the agent "+
			"receives a full-corpus file sync onto rented hardware", storedInstance, instanceID)
	}

	var storedAgent sql.NullInt64
	var readyAt sql.NullTime
	if err := database.QueryRow(
		`SELECT agent_id, ready_at FROM cloud_instances WHERE id = $1`, instanceID).
		Scan(&storedAgent, &readyAt); err != nil {
		t.Fatalf("read back instance: %v", err)
	}
	if !storedAgent.Valid || int(storedAgent.Int64) != agent.ID {
		t.Errorf("cloud_instances.agent_id = %v, want %d — without it LoadAgentJobLocks "+
			"returns an empty map and dispatch isolation degrades to a pass-through",
			storedAgent, agent.ID)
	}
	if !readyAt.Valid {
		t.Error("ready_at was not stamped; the reaper reads an unset ready_at plus a " +
			"passed ready deadline as 'never registered' and destroys the instance")
	}
}

/*
 * TestAgentCreate_LockMapIsPopulated closes the loop: the registration above is
 * only useful if it produces the map the scheduler actually consumes.
 */
func TestAgentCreate_LockMapIsPopulated(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()

	owner := agentOwner(t, database)
	client := testutil.CreateTestClient(t, database, "cloud-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})
	job := testutil.CreateCloudJob(t, database, client, true)
	cfg := testutil.CreateTestCloudProviderConfig(t, database, "mock", "mock-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{Enabled: true})
	instanceID, _ := testutil.CreateTestCloudInstance(t, database, cfg, testutil.InstanceOpts{
		State:          "running",
		ClientID:       &client,
		JobExecutionID: &job.JobID,
	})

	agent := newTestAgent("cloud-agent-"+uuid.NewString()[:8], &instanceID, owner)
	if err := NewAgentRepository(database).Create(ctx, agent); err != nil {
		t.Fatalf("register cloud agent: %v", err)
	}

	locks, err := NewCloudInstanceRepository(database).LoadAgentJobLocks(ctx)
	if err != nil {
		t.Fatalf("LoadAgentJobLocks: %v", err)
	}
	got, ok := locks[agent.ID]
	if !ok {
		t.Fatalf("agent %d is missing from the dispatch lock map; the isolation "+
			"wrapper would treat it as an ordinary agent and offer it any client's job", agent.ID)
	}
	if got != job.JobID {
		t.Errorf("agent %d is locked to job %s, want %s", agent.ID, got, job.JobID)
	}
}

/*
 * TestAgentCreate_OnPremAgentIsUnchanged.
 *
 * The cloud path adds a transaction and a second write. On-prem registration —
 * every agent in every existing deployment — must not acquire either.
 */
func TestAgentCreate_OnPremAgentIsUnchanged(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()

	owner := agentOwner(t, database)
	agent := newTestAgent("onprem-"+uuid.NewString()[:8], nil, owner)
	if err := NewAgentRepository(database).Create(ctx, agent); err != nil {
		t.Fatalf("register on-prem agent: %v", err)
	}
	if agent.ID == 0 {
		t.Fatal("no id was returned for the new agent")
	}

	var storedInstance uuid.NullUUID
	if err := database.QueryRow(
		`SELECT cloud_instance_id FROM agents WHERE id = $1`, agent.ID).Scan(&storedInstance); err != nil {
		t.Fatalf("read back agent: %v", err)
	}
	if storedInstance.Valid {
		t.Errorf("an on-prem agent was marked as cloud (instance %s): it would be "+
			"pinned to one job and exempted from the offline monitor", storedInstance.UUID)
	}

	locks, err := NewCloudInstanceRepository(database).LoadAgentJobLocks(ctx)
	if err != nil {
		t.Fatalf("LoadAgentJobLocks: %v", err)
	}
	if _, found := locks[agent.ID]; found {
		t.Error("an on-prem agent appeared in the cloud dispatch lock map")
	}
}

/*
 * TestAgentCreate_UnknownInstanceRegistersNothing.
 *
 * Half-registering is worse than failing: an agent marked ephemeral that no
 * instance claims is invisible to teardown, so nothing destroys the box it runs
 * on and it bills until its own in-guest watchdog fires — assuming that watchdog
 * is working, which is exactly the assumption teardown exists not to make.
 */
func TestAgentCreate_UnknownInstanceRegistersNothing(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()

	owner := agentOwner(t, database)
	ghost := uuid.New()
	name := "ghost-agent-" + uuid.NewString()[:8]

	agent := newTestAgent(name, &ghost, owner)
	err := NewAgentRepository(database).Create(ctx, agent)
	if err == nil {
		t.Fatal("registering a cloud agent against a nonexistent instance succeeded")
	}

	var n int
	if qerr := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE name = $1`, name).Scan(&n); qerr != nil {
		t.Fatalf("count agents: %v", qerr)
	}
	if n != 0 {
		t.Fatalf("%d agent row(s) survived a failed cloud registration; the INSERT "+
			"must roll back with the instance update", n)
	}
}
