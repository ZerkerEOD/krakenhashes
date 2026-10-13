package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestListAgentHealth_JobNameTeamScoped guards issue #113: a shared agent's
// current-job name/id/progress must only reach a scope that owns the job (its
// hashlist's client team, or an unrestricted/admin scope). A team that merely
// sees the agent must not see another team's job name.
func TestListAgentHealth_JobNameTeamScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	ctx := context.Background()

	repo := NewDashboardRepository(database)

	userA := testutil.CreateTestUser(t, database, "d113-usera", "d113-usera@test.local", testutil.DefaultTestPassword, "user")
	teamA := testutil.CreateTestTeam(t, database, "D113 Team A")
	teamB := testutil.CreateTestTeam(t, database, "D113 Team B")
	testutil.AddUserToTeam(t, database, teamA, userA.ID)

	clientA := testutil.CreateTestClient(t, database, "D113 Client A", testutil.ClientCloudOpts{})
	testutil.AssignClientToTeam(t, database, clientA, teamA)

	hlA := testutil.CreateTestHashlist(t, database, userA.ID, clientA, "d113-hl-a")
	jobA := testutil.CreateTestJobExecution(t, database, hlA, userA.ID, "running")

	agentA := testutil.CreateTestAgent(t, database, userA.ID, nil)

	// Link the agent to Team A's running job via a running task.
	if _, err := database.Exec(`
		INSERT INTO job_tasks (job_execution_id, agent_id, status, keyspace_start, keyspace_end, chunk_duration, started_at)
		VALUES ($1, $2, 'running', 0, 1000, 60, NOW())`, jobA, agentA); err != nil {
		t.Fatalf("insert job_task: %v", err)
	}

	jobName := func(t *testing.T, scope DashboardScope) *string {
		t.Helper()
		agents, err := repo.ListAgentHealth(ctx, scope)
		require.NoError(t, err)
		require.Len(t, agents, 1, "the agent itself is always listed, regardless of job scope")
		require.Equal(t, agentA, agents[0].ID)
		return agents[0].CurrentJobName
	}

	// Team A owns the job → name visible.
	require.NotNil(t, jobName(t, DashboardScope{AgentIDs: []int{agentA}, TeamIDs: []uuid.UUID{teamA}}),
		"Team A (job owner) must see the current job name")
	// Admin / unrestricted (nil TeamIDs) → name visible.
	require.NotNil(t, jobName(t, DashboardScope{AgentIDs: []int{agentA}}),
		"unrestricted scope must see the current job name")

	// Team B sees the shared agent but NOT Team A's job name/id/progress (#113).
	agents, err := repo.ListAgentHealth(ctx, DashboardScope{AgentIDs: []int{agentA}, TeamIDs: []uuid.UUID{teamB}})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Nil(t, agents[0].CurrentJobName, "#113: job name must not leak to a non-owning team")
	require.Nil(t, agents[0].CurrentJobID, "#113: job id must not leak to a non-owning team")
	require.Nil(t, agents[0].CurrentProgress, "#113: job progress must not leak to a non-owning team")
}
