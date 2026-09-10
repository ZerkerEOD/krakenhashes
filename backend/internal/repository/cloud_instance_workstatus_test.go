package repository

import (
	"context"
	"testing"
	"time"

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

	/*
	 * TestWorkStatus_LastActivityFollowsProgressReports.
	 *
	 * The bug this pins: LastActivityAt was GREATEST over
	 * assigned_at/started_at/completed_at, and completed_at is NULL while a task
	 * runs — so the whole expression collapsed to started_at and never moved for
	 * the duration of the chunk. Cloud chunks are floored at 3600s and the idle
	 * drain defaults to 5 minutes, so every rented instance was destroyed five
	 * minutes into its first chunk regardless of what the agent was doing.
	 */
	t.Run("progress reports keep a long chunk fresh", func(t *testing.T) {
		agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)
		_, err := database.Exec(`
			INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
			                       keyspace_start, keyspace_end, chunk_duration,
			                       assigned_at, started_at, last_activity_at)
			VALUES ($1, $2, $3, 'running', 0, 100, 3600,
			        NOW() - INTERVAL '45 minutes',
			        NOW() - INTERVAL '45 minutes',
			        NOW() - INTERVAL '5 seconds')`,
			uuid.New(), job.JobID, agentID)
		require.NoError(t, err)

		got, err := repo.WorkStatus(ctx, job.JobID, &agentID)
		require.NoError(t, err)
		require.True(t, got.LastActivityAt.Valid)
		require.WithinDuration(t, time.Now(), got.LastActivityAt.Time, time.Minute,
			"a task that reported progress 5s ago must not look 45 minutes idle")
		require.True(t, got.InFlight)
		require.True(t, got.InFlightActivityAt.Valid,
			"an in-flight task must carry a freshness stamp for the crack-drain grace")
	})

	/*
	 * Terminal rows must not populate the in-flight freshness stamp, or a
	 * just-completed task would suppress idle drain for a full grace period on
	 * an agent that is genuinely finished.
	 */
	t.Run("terminal rows do not populate InFlightActivityAt", func(t *testing.T) {
		agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)
		_, err := database.Exec(`
			INSERT INTO job_tasks (id, job_execution_id, agent_id, status,
			                       keyspace_start, keyspace_end, chunk_duration,
			                       assigned_at, started_at, completed_at, last_activity_at)
			VALUES ($1, $2, $3, 'completed', 0, 100, 3600,
			        NOW(), NOW(), NOW(), NOW())`,
			uuid.New(), job.JobID, agentID)
		require.NoError(t, err)

		got, err := repo.WorkStatus(ctx, job.JobID, &agentID)
		require.NoError(t, err)
		require.False(t, got.InFlight)
		require.False(t, got.InFlightActivityAt.Valid)
	})

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
