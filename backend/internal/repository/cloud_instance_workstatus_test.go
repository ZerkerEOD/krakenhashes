package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

/*
 * InstanceWorkStatus.InFlight is the predicate the budget ladder's drain rung
 * hangs on: "is this rented machine still doing something we must not
 * interrupt?". Getting the status set wrong is expensive in both directions —
 * too broad and a drained instance never terminates, too narrow and the reaper
 * destroys a VM that is still uploading cracks it has already found.
 *
 * The SQL is the logic, so this is DB-backed.
 */
func TestWorkStatus_InFlight(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewCloudInstanceRepository(database)
	ctx := context.Background()

	owner := testutil.CreateTestUser(t, database,
		"wf-"+uuid.NewString()[:8], "wf-"+uuid.NewString()[:8]+"@test.local", "pw", "admin")
	clientID := testutil.CreateTestClient(t, database, "wf-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true})
	job := testutil.CreateCloudJob(t, database, clientID, true)

	cases := []struct {
		status       string
		wantInFlight bool
		why          string
	}{
		{"assigned", true, "handed out but not started is still owned by the agent"},
		{"running", true, "obviously in flight"},
		{"processing", true, "still uploading crack batches; destroying here LOSES cracks"},
		{"completed", false, "terminal"},
		{"failed", false, "terminal"},
		{"cancelled", false, "terminal"},
		{"reconnect_pending", false, "the agent is not connected, so there is nothing to preserve"},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			// A fresh agent per case so statuses cannot bleed between them.
			agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)
			_, err := database.Exec(`
				INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
				                       keyspace_start, keyspace_end, chunk_duration, assigned_at)
				VALUES ($1, $2, $3, $4, 0, 100, 60, NOW())`,
				uuid.New(), job.JobID, agentID, tc.status)
			require.NoError(t, err)

			got, err := repo.WorkStatus(ctx, job.JobID, &agentID)
			require.NoError(t, err)
			require.Equal(t, tc.wantInFlight, got.InFlight, tc.why)
		})
	}

	t.Run("agent with no tasks at all", func(t *testing.T) {
		agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)
		got, err := repo.WorkStatus(ctx, job.JobID, &agentID)
		// Both aggregates are total, so this yields a row of (NULL, false)
		// rather than no row — the ErrNoRows path must stay unreachable here.
		require.NoError(t, err)
		require.False(t, got.InFlight)
		require.False(t, got.LastActivityAt.Valid,
			"never-worked must stay distinguishable from idle-since-X; the commissioning "+
				"grace depends on it")
	})
}
