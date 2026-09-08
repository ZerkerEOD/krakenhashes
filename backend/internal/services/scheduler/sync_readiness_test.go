package scheduler

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * agentFileSyncInProgress is the benchmark readiness gate. Its whole design
 * hinges on gating 'in_progress' but NOT 'pending', so these tests pin that
 * distinction rather than merely exercising the query.
 *
 * A global sync_status gate was deliberately removed from getIdleAgents because
 * 'completed' means "holds the entire corpus", which cloud agents never do --
 * AgentSyncRecovery excludes them for exactly that reason, calling "stuck at
 * pending" their normal state. Re-introducing a gate that trips on 'pending'
 * would strand every rented agent permanently, so that case is tested
 * explicitly.
 */

func TestAgentFileSyncInProgress(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	owner := testutil.CreateTestUser(t, database, "sync-gate-"+uuid.NewString()[:8],
		"sync-gate-"+uuid.NewString()[:8]+"@test.local", testutil.DefaultTestPassword, "user")
	agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)

	// Stamped in SQL rather than from Go's clock, so the test is independent of
	// the host's timezone. Writing a Go time.Time here is what exposed the
	// tz-naive column bug that migration 20260908130000 fixes: the value landed
	// an hour in the future and the gate read it as fresh.
	set := func(status string, startedAgoSeconds int, nullStarted bool) {
		t.Helper()
		query := `UPDATE agents SET sync_status = $2::agent_sync_status, ` +
			`sync_started_at = NOW() - ($3 * INTERVAL '1 second') WHERE id = $1`
		args := []interface{}{agentID, status, startedAgoSeconds}
		if nullStarted {
			query = `UPDATE agents SET sync_status = $2::agent_sync_status, sync_started_at = NULL WHERE id = $1`
			args = args[:2]
		}
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("set sync state %s: %v", status, err)
		}
	}

	t.Run("pending must NOT gate — it is a cloud agent's resting state", func(t *testing.T) {
		set("pending", 0, true)
		syncing, err := agentFileSyncInProgress(ctx, database, agentID, syncInProgressGrace)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if syncing {
			t.Fatal("gating on 'pending' would permanently strand every cloud agent — " +
				"they never run a full-corpus sync, so pending is where they live")
		}
	})

	t.Run("in_progress gates", func(t *testing.T) {
		set("in_progress", 30, false)
		syncing, err := agentFileSyncInProgress(ctx, database, agentID, syncInProgressGrace)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !syncing {
			t.Fatal("an agent actively downloading must not be handed a benchmark")
		}
	})

	t.Run("stale in_progress fails open", func(t *testing.T) {
		set("in_progress", int(syncInProgressGrace.Seconds())+60, false)
		syncing, err := agentFileSyncInProgress(ctx, database, agentID, syncInProgressGrace)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if syncing {
			t.Fatal("an agent stuck in_progress past the grace must fail OPEN; nothing clears " +
				"that state if the agent died in a way readPump did not catch, and an " +
				"unbounded gate would exclude it from benchmarks forever")
		}
	})

	t.Run("completed does not gate", func(t *testing.T) {
		set("completed", 30, false)
		syncing, err := agentFileSyncInProgress(ctx, database, agentID, syncInProgressGrace)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if syncing {
			t.Fatal("a completed sync must not gate")
		}
	})
}
