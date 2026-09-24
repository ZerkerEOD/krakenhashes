package scheduler

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * The dispatch filter that gives cloud_instances.state its meaning.
 *
 * The budget ladder's drain rung wrote state = 'draining' and nothing read it:
 * getIdleAgents had no ci.state filter, so an instance at >=99% of its client's
 * cap kept being handed fresh chunks until the 100% rung killed it mid-chunk.
 * These tests are the contract for the one predicate that changes that.
 */

// stubSender offers a fixed agent list. getIdleAgents is the only thing under
// test here, so everything else is the permissive answer.
type stubSender struct{ connected []int }

func (s *stubSender) SendMessage(int, *wsservice.Message) error { return nil }
func (s *stubSender) GetConnectedAgents() []int                 { return s.connected }
func (s *stubSender) IsShuttingDown(int) bool                   { return false }
func (s *stubSender) WasRecentlyRejected(int) bool              { return false }
func (s *stubSender) IsFileMapReady(int) bool                   { return true }

// drainCycle builds a Cycle wired only far enough for getIdleAgents.
func drainCycle(database *db.DB, agentIDs []int) *Cycle {
	return &Cycle{db: database, wsSender: &stubSender{connected: agentIDs}}
}

// cloudAgent creates a cloud instance in the given state plus its agent row,
// and returns the agent ID and instance ID.
func cloudAgent(t *testing.T, database *db.DB, state string) (int, uuid.UUID) {
	t.Helper()
	owner := testutil.CreateTestUser(t, database,
		"drain-"+uuid.NewString()[:8], "drain-"+uuid.NewString()[:8]+"@test.local", "pw", "admin")
	configID := testutil.CreateTestCloudProviderConfig(t, database, "mock",
		"drain-"+uuid.NewString()[:8], testutil.ProviderConfigOpts{})
	clientID := testutil.CreateTestClient(t, database, "drain-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, ProviderAllowlist: []string{"mock"}})
	instID, _ := testutil.CreateTestCloudInstance(t, database, configID, testutil.InstanceOpts{
		State: state, ClientID: &clientID, HourlyRateCents: 100,
	})
	agentID := testutil.CreateTestAgent(t, database, owner.ID, &instID)
	return agentID, instID
}

func idleAgentIDs(t *testing.T, c *Cycle) map[int]bool {
	t.Helper()
	infos, err := c.getIdleAgents(context.Background())
	if err != nil {
		t.Fatalf("getIdleAgents: %v", err)
	}
	out := make(map[int]bool, len(infos))
	for _, a := range infos {
		out[a.ID] = true
	}
	return out
}

/*
 * TestGetIdleAgents_ExcludesDrainingCloudAgent.
 *
 * Asserted as a before/after on the same agent rather than a single negative,
 * so it cannot pass vacuously because the fixture was never eligible.
 */
func TestGetIdleAgents_ExcludesDrainingCloudAgent(t *testing.T) {
	database := requireSchedulerTestDB(t)
	agentID, instID := cloudAgent(t, database, "running")
	c := drainCycle(database, []int{agentID})

	if !idleAgentIDs(t, c)[agentID] {
		t.Fatal("a running cloud agent should be offered for dispatch")
	}

	if _, err := database.Exec(
		`UPDATE cloud_instances SET state = 'draining' WHERE id = $1`, instID); err != nil {
		t.Fatalf("mark draining: %v", err)
	}

	if idleAgentIDs(t, c)[agentID] {
		t.Error("a draining cloud agent was still offered new work — this is the whole " +
			"reason the drain rung was cosmetic")
	}
}

// TestGetIdleAgents_ExcludesTerminatingCloudAgent: ListLive still returns
// 'terminating', so an instance whose Destroy call failed can sit there
// indefinitely. Dispatching a chunk to it is pure waste.
func TestGetIdleAgents_ExcludesTerminatingCloudAgent(t *testing.T) {
	database := requireSchedulerTestDB(t)
	agentID, instID := cloudAgent(t, database, "running")
	c := drainCycle(database, []int{agentID})

	if _, err := database.Exec(
		`UPDATE cloud_instances SET state = 'terminating' WHERE id = $1`, instID); err != nil {
		t.Fatalf("mark terminating: %v", err)
	}

	if idleAgentIDs(t, c)[agentID] {
		t.Error("a terminating cloud agent was offered new work")
	}
}

/*
 * TestGetIdleAgents_OnPremAgentUnaffected.
 *
 * The catastrophic version of this change is a bare `ci.state <> 'draining'`,
 * which is NULL for every on-prem agent and would therefore drop the entire
 * non-cloud fleet from dispatch. This pins the ci.id IS NULL arm.
 */
func TestGetIdleAgents_OnPremAgentUnaffected(t *testing.T) {
	database := requireSchedulerTestDB(t)
	owner := testutil.CreateTestUser(t, database,
		"onprem-"+uuid.NewString()[:8], "onprem-"+uuid.NewString()[:8]+"@test.local", "pw", "admin")
	agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)
	c := drainCycle(database, []int{agentID})

	if !idleAgentIDs(t, c)[agentID] {
		t.Error("an on-prem agent was dropped by the cloud drain filter; the LEFT JOIN " +
			"must stay a no-op for the non-cloud fleet")
	}
}

/*
 * TestGetIdleAgents_SyncingCloudAgentStillOffered.
 *
 * Pins the exclusion-list choice against a future "positive filter" refactor.
 * An agent registers and can accept a task while its instance row still says
 * provisioning/syncing, so gating on state = 'running' would strand every cold
 * cloud agent — failing closed on the way UP, which is the wrong direction.
 */
func TestGetIdleAgents_SyncingCloudAgentStillOffered(t *testing.T) {
	database := requireSchedulerTestDB(t)
	for _, state := range []string{"provisioning", "syncing"} {
		t.Run(state, func(t *testing.T) {
			agentID, _ := cloudAgent(t, database, state)
			c := drainCycle(database, []int{agentID})
			if !idleAgentIDs(t, c)[agentID] {
				t.Errorf("a cloud agent in state %q was not offered work; cold cloud "+
					"agents would never receive their first task", state)
			}
		})
	}
}
