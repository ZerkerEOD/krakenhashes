package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers -----------------------------------------------------------------

// createLoopbackTestUser creates a user with a collision-free username/email so several
// users can coexist inside one test.
func createLoopbackTestUser(t *testing.T, database *db.DB, label string) uuid.UUID {
	t.Helper()
	suffix := uuid.NewString()[:8]
	user := testutil.CreateTestUser(t, database,
		"loopback-"+label+"-"+suffix,
		"loopback-"+label+"-"+suffix+"@test.local",
		testutil.DefaultTestPassword, "user")
	return user.ID
}

// createLoopbackClient creates a client row. clients.name is UNIQUE and the table is not
// truncated between tests, so the name carries a random suffix.
func createLoopbackClient(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.QueryRow(
		`INSERT INTO clients (name) VALUES ($1) RETURNING id`,
		"loopback-client-"+uuid.NewString(),
	).Scan(&id)
	require.NoError(t, err, "failed to create test client")
	return id
}

// createLoopbackTeam creates a team row (teams.name is UNIQUE, same caveat as clients).
func createLoopbackTeam(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.QueryRow(
		`INSERT INTO teams (name) VALUES ($1) RETURNING id`,
		"loopback-team-"+uuid.NewString(),
	).Scan(&id)
	require.NoError(t, err, "failed to create test team")
	return id
}

// assignClientToTeam links a client to a team (the path the team filter traverses).
func assignClientToTeam(t *testing.T, database *db.DB, clientID, teamID uuid.UUID) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO client_teams (client_id, team_id) VALUES ($1, $2)`,
		clientID, teamID,
	)
	require.NoError(t, err, "failed to assign client to team")
}

// createLoopbackHashlist creates a hashlist owned by userID. Pass a nil clientID to make a
// client-less ("legacy") hashlist.
func createLoopbackHashlist(t *testing.T, database *db.DB, userID uuid.UUID, clientID *uuid.UUID) int64 {
	t.Helper()
	var hashlistID int64
	var client interface{}
	if clientID != nil {
		client = *clientID
	}
	err := database.QueryRow(`
		INSERT INTO hashlists (name, user_id, client_id, hash_type_id, status)
		VALUES ($1, $2, $3, 0, $4)
		RETURNING id`,
		"loopback-test-hashlist", userID, client, models.HashListStatusReady,
	).Scan(&hashlistID)
	require.NoError(t, err, "failed to create test hashlist")
	return hashlistID
}

// insertLoopbackSession inserts a session directly so a terminal status can be set up
// without driving the whole controller.
func insertLoopbackSession(t *testing.T, database *db.DB, hashlistID int64, createdBy uuid.UUID, status models.LoopbackSessionStatus) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO loopback_sessions (hashlist_id, source_type, name, status, created_by)
		VALUES ($1, 'custom', $2, $3, $4)
		RETURNING id`,
		hashlistID, "session-"+string(status), status, createdBy,
	).Scan(&id)
	require.NoError(t, err, "failed to insert loopback session")
	return id
}

// sessionIDSet turns a result slice into a set for order-independent assertions.
func sessionIDSet(sessions []*models.LoopbackSession) map[uuid.UUID]models.LoopbackSessionStatus {
	out := make(map[uuid.UUID]models.LoopbackSessionStatus, len(sessions))
	for _, s := range sessions {
		out[s.ID] = s.Status
	}
	return out
}

// --- tests -------------------------------------------------------------------

// TestLoopbackRepository_ListSessions_InFlightOnly is the GH #79 regression test: the
// Loopback panel is a live view, so a finished session must not come back from the query
// that backs it. Finished sessions are never deleted (nothing deletes them), which is
// exactly why the read has to filter.
func TestLoopbackRepository_ListSessions_InFlightOnly(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewLoopbackRepository(database)
	ctx := context.Background()

	userID := createLoopbackTestUser(t, database, "owner")
	hashlistID := createLoopbackHashlist(t, database, userID, nil)

	allStatuses := []models.LoopbackSessionStatus{
		models.LoopbackSessionStatusWaiting,
		models.LoopbackSessionStatusActive,
		models.LoopbackSessionStatusCompleted,
		models.LoopbackSessionStatusFailed,
		models.LoopbackSessionStatusCancelled,
	}
	ids := make(map[models.LoopbackSessionStatus]uuid.UUID, len(allStatuses))
	for _, st := range allStatuses {
		ids[st] = insertLoopbackSession(t, database, hashlistID, userID, st)
	}

	t.Run("in-flight only returns waiting and active", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Len(t, got, 2, "only waiting/active sessions should be in flight")
		assert.Contains(t, got, ids[models.LoopbackSessionStatusWaiting])
		assert.Contains(t, got, ids[models.LoopbackSessionStatusActive])

		// The regression: finished sessions must be gone from the panel's query.
		assert.NotContains(t, got, ids[models.LoopbackSessionStatusCompleted])
		assert.NotContains(t, got, ids[models.LoopbackSessionStatusFailed])
		assert.NotContains(t, got, ids[models.LoopbackSessionStatusCancelled])
	})

	t.Run("without the filter every session is returned", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Len(t, got, len(allStatuses))
		for _, st := range allStatuses {
			assert.Contains(t, got, ids[st], "status %s should be listed when InFlightOnly is false", st)
		}
	})

	t.Run("results are newest first", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{})
		require.NoError(t, err)
		require.NotEmpty(t, sessions)
		for i := 1; i < len(sessions); i++ {
			assert.False(t, sessions[i-1].CreatedAt.Before(sessions[i].CreatedAt),
				"sessions should be ordered created_at DESC")
		}
	})
}

// TestLoopbackRepository_ListSessions_CreatedBy covers the "mine" scope.
func TestLoopbackRepository_ListSessions_CreatedBy(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewLoopbackRepository(database)
	ctx := context.Background()

	alice := createLoopbackTestUser(t, database, "alice")
	bob := createLoopbackTestUser(t, database, "bob")
	hashlistID := createLoopbackHashlist(t, database, alice, nil)

	aliceSession := insertLoopbackSession(t, database, hashlistID, alice, models.LoopbackSessionStatusWaiting)
	bobSession := insertLoopbackSession(t, database, hashlistID, bob, models.LoopbackSessionStatusActive)

	t.Run("scoped to a creator", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{CreatedBy: &alice, InFlightOnly: true})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Len(t, got, 1)
		assert.Contains(t, got, aliceSession)
		assert.NotContains(t, got, bobSession)
	})

	t.Run("nil creator sees both", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Contains(t, got, aliceSession)
		assert.Contains(t, got, bobSession)
	})
}

// TestLoopbackRepository_ListSessions_TeamFilter is the cross-team leak regression test:
// before this filter existed the endpoint scoped by created_by only, so a user removed
// from a team kept seeing their old sessions (including the joined job names).
func TestLoopbackRepository_ListSessions_TeamFilter(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewLoopbackRepository(database)
	ctx := context.Background()

	userID := createLoopbackTestUser(t, database, "teamuser")

	teamA := createLoopbackTeam(t, database)
	teamB := createLoopbackTeam(t, database)

	clientA := createLoopbackClient(t, database)
	clientB := createLoopbackClient(t, database)
	assignClientToTeam(t, database, clientA, teamA)
	assignClientToTeam(t, database, clientB, teamB)

	hashlistA := createLoopbackHashlist(t, database, userID, &clientA)
	hashlistB := createLoopbackHashlist(t, database, userID, &clientB)
	hashlistNoClient := createLoopbackHashlist(t, database, userID, nil)

	sessionA := insertLoopbackSession(t, database, hashlistA, userID, models.LoopbackSessionStatusWaiting)
	sessionB := insertLoopbackSession(t, database, hashlistB, userID, models.LoopbackSessionStatusWaiting)
	sessionNoClient := insertLoopbackSession(t, database, hashlistNoClient, userID, models.LoopbackSessionStatusWaiting)

	t.Run("includes own team, excludes other team", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{
			InFlightOnly: true,
			TeamsEnabled: true,
			TeamIDs:      []uuid.UUID{teamA},
		})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Contains(t, got, sessionA, "session on a client in the user's team should be visible")
		assert.NotContains(t, got, sessionB, "session on another team's client must not leak")
	})

	t.Run("client-less hashlists are excluded under a team filter", func(t *testing.T) {
		// hashlists.client_id is nullable and `NULL IN (...)` is never true, so legacy
		// client-less hashlists drop out of team-filtered results. This matches the Jobs
		// list exactly and is intentional.
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{
			InFlightOnly: true,
			TeamsEnabled: true,
			TeamIDs:      []uuid.UUID{teamA},
		})
		require.NoError(t, err)
		assert.NotContains(t, sessionIDSet(sessions), sessionNoClient)
	})

	t.Run("multiple teams union", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{
			InFlightOnly: true,
			TeamsEnabled: true,
			TeamIDs:      []uuid.UUID{teamA, teamB},
		})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Contains(t, got, sessionA)
		assert.Contains(t, got, sessionB)
	})

	t.Run("teams enabled with no teams fails closed", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{
			InFlightOnly: true,
			TeamsEnabled: true,
			TeamIDs:      nil,
		})
		require.NoError(t, err)
		assert.Empty(t, sessions, "a user with no teams must see nothing, not everything")
	})

	t.Run("teams disabled ignores team membership", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true})
		require.NoError(t, err)

		got := sessionIDSet(sessions)
		assert.Contains(t, got, sessionA)
		assert.Contains(t, got, sessionB)
		assert.Contains(t, got, sessionNoClient)
	})
}

// TestLoopbackRepository_ListSessions_Limit checks the cap and its zero-value default.
func TestLoopbackRepository_ListSessions_Limit(t *testing.T) {
	database := testutil.SetupTestDB(t)
	repo := NewLoopbackRepository(database)
	ctx := context.Background()

	userID := createLoopbackTestUser(t, database, "limit")
	hashlistID := createLoopbackHashlist(t, database, userID, nil)
	for i := 0; i < 3; i++ {
		insertLoopbackSession(t, database, hashlistID, userID, models.LoopbackSessionStatusWaiting)
	}

	t.Run("explicit limit is applied", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true, Limit: 2})
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})

	t.Run("zero limit falls back to the default", func(t *testing.T) {
		sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true, Limit: 0})
		require.NoError(t, err)
		assert.Len(t, sessions, 3, "a zero limit must not mean zero rows")
	})
}

// TestLoopbackRepository_DeleteFinishedJobs_LeavesSessionRow pins the mechanism behind the
// symptom reported in GH #79: "Delete Finished Jobs" has no loopback carve-out, so a
// session's finished job_executions really are deleted and their loopback_session_jobs
// links cascade away — but the loopback_sessions row itself survives (nothing deletes it).
// That orphan row is what made the panel look like the jobs were never deleted, and is why
// the fix is a read-side filter rather than a purge.
func TestLoopbackRepository_DeleteFinishedJobs_LeavesSessionRow(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := createLoopbackTestUser(t, database, "cascade")
	hashlistID := createLoopbackHashlist(t, database, userID, nil)
	sessionID := insertLoopbackSession(t, database, hashlistID, userID, models.LoopbackSessionStatusCompleted)

	// A finished job belonging to the session.
	jobID := uuid.New()
	_, err := database.ExecContext(ctx, `
		INSERT INTO job_executions (id, hashlist_id, attack_mode, priority, status, name)
		VALUES ($1, $2, 0, 5, 'completed', 'loopback cascade job')`,
		jobID, hashlistID)
	require.NoError(t, err, "failed to create finished job execution")

	_, err = database.ExecContext(ctx, `
		INSERT INTO loopback_session_jobs (session_id, job_execution_id, round, role, is_mutatable)
		VALUES ($1, $2, 0, 'original', true)`,
		sessionID, jobID)
	require.NoError(t, err, "failed to link job to session")

	deleted, err := NewJobExecutionRepository(database).DeleteFinished(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, 1, "the finished loopback job should be deleted like any other")

	// The link row cascaded away with the job_execution...
	var linkCount int
	require.NoError(t, database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM loopback_session_jobs WHERE session_id = $1`, sessionID,
	).Scan(&linkCount))
	assert.Zero(t, linkCount, "loopback_session_jobs should cascade with the deleted job_executions")

	// ...but the session row is still there, now with zero jobs. Nothing in the backend
	// deletes it, so the panel must not render it.
	var sessionCount int
	require.NoError(t, database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM loopback_sessions WHERE id = $1`, sessionID,
	).Scan(&sessionCount))
	assert.Equal(t, 1, sessionCount, "the loopback_sessions row survives a finished-job purge")

	// And the in-flight filter is what keeps that orphan off the panel.
	repo := NewLoopbackRepository(database)
	sessions, err := repo.ListSessions(ctx, LoopbackSessionFilter{InFlightOnly: true})
	require.NoError(t, err)
	assert.NotContains(t, sessionIDSet(sessions), sessionID)
}
