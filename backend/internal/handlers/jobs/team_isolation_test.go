package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

/*
 * Multi-Team isolation for the job routes touched by GH #100:
 *   POST   /api/jobs/{id}/archive
 *   POST   /api/jobs/{id}/unarchive
 *   DELETE /api/jobs/finished
 *   POST   /api/jobs/{id}/benchmark-blocklist/{entryID}/clear
 *
 * Shares the test database with other packages: run under `go test -p 1`, no t.Parallel().
 */

type jobFixtures struct {
	admin, userA, userB *models.User
	teamA, teamB        uuid.UUID
	clientA, clientB    uuid.UUID
	hlA, hlB            int64
	jobA, jobB          uuid.UUID // completed jobs, one per team
	agentA              int
}

func requireTestDB(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
}

func newTeamService(database *db.DB) *services.TeamService {
	return services.NewTeamService(
		database,
		repository.NewTeamRepository(database),
		repository.NewClientTeamRepository(database),
		repository.NewHashListRepository(database),
		repository.NewJobExecutionRepository(database),
		repository.NewAgentRepository(database),
		repository.NewSystemSettingsRepository(database),
		repository.NewTeamAgentTrustRepository(database),
	)
}

func seedJobFixtures(t *testing.T, database *db.DB, teamsEnabled bool) *jobFixtures {
	t.Helper()
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, teamsEnabled)

	f := &jobFixtures{}
	f.admin = testutil.CreateTestUser(t, database, "job-admin", "job-admin@test.local", testutil.DefaultTestPassword, "admin")
	f.userA = testutil.CreateTestUser(t, database, "job-usera", "job-usera@test.local", testutil.DefaultTestPassword, "user")
	f.userB = testutil.CreateTestUser(t, database, "job-userb", "job-userb@test.local", testutil.DefaultTestPassword, "user")

	f.teamA = testutil.CreateTestTeam(t, database, "Team A")
	f.teamB = testutil.CreateTestTeam(t, database, "Team B")
	testutil.AddUserToTeam(t, database, f.teamA, f.userA.ID)
	testutil.AddUserToTeam(t, database, f.teamB, f.userB.ID)

	f.clientA = testutil.CreateTestClient(t, database, "Client A", testutil.ClientCloudOpts{})
	f.clientB = testutil.CreateTestClient(t, database, "Client B", testutil.ClientCloudOpts{})
	testutil.AssignClientToTeam(t, database, f.clientA, f.teamA)
	testutil.AssignClientToTeam(t, database, f.clientB, f.teamB)

	f.hlA = testutil.CreateTestHashlist(t, database, f.userA.ID, f.clientA, "hl-a")
	f.hlB = testutil.CreateTestHashlist(t, database, f.userB.ID, f.clientB, "hl-b")
	f.jobA = testutil.CreateTestJobExecution(t, database, f.hlA, f.userA.ID, "completed")
	f.jobB = testutil.CreateTestJobExecution(t, database, f.hlB, f.userB.ID, "completed")

	f.agentA = testutil.CreateTestAgent(t, database, f.userA.ID, nil)
	return f
}

// newJobRouter mounts the four routes under test exactly as routes/user.go does, behind the
// production TeamAccessMiddleware. Dependencies the routes do not touch are nil.
func newJobRouter(database *db.DB, teamSvc *services.TeamService) http.Handler {
	h := NewUserJobsHandler(
		repository.NewJobExecutionRepository(database),
		nil, nil, nil,
		repository.NewHashListRepository(database),
		nil, nil, nil, nil, nil, nil, nil,
		nil, // jobExecutionService: DeleteFinishedJobs nil-guards the ephemeral sweep
		nil, nil, nil, nil,
		teamSvc,
		nil,
		repository.NewBenchmarkRepository(database),
		nil,
	)
	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	api.HandleFunc("/jobs/finished", h.DeleteFinishedJobs).Methods("DELETE")
	api.HandleFunc("/jobs/{id}/archive", h.ArchiveJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/unarchive", h.UnarchiveJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/benchmark-blocklist/{entryID}/clear", h.ClearBenchmarkBlocklistEntry).Methods("POST")
	return root
}

func do(router http.Handler, u *models.User, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestTeamIsolation_ArchiveUnarchive(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedJobFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newJobRouter(database, teamSvc)

	// Cross-team archive is denied with the enumeration-safe 404.
	if rec := do(router, f.userB, "POST", "/api/jobs/"+f.jobA.String()+"/archive"); rec.Code != http.StatusNotFound {
		t.Fatalf("userB archive jobA: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// Owner can archive, cross-team cannot unarchive, owner can unarchive.
	if rec := do(router, f.userA, "POST", "/api/jobs/"+f.jobA.String()+"/archive"); rec.Code != http.StatusOK {
		t.Fatalf("userA archive jobA: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(router, f.userB, "POST", "/api/jobs/"+f.jobA.String()+"/unarchive"); rec.Code != http.StatusNotFound {
		t.Fatalf("userB unarchive jobA: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(router, f.userA, "POST", "/api/jobs/"+f.jobA.String()+"/unarchive"); rec.Code != http.StatusOK {
		t.Fatalf("userA unarchive jobA: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	// Admin bypass.
	if rec := do(router, f.admin, "POST", "/api/jobs/"+f.jobB.String()+"/archive"); rec.Code != http.StatusOK {
		t.Fatalf("admin archive jobB: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func deletedCount(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var resp struct {
		DeletedCount int `json:"deleted_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode delete-finished response %q: %v", rec.Body.String(), err)
	}
	return resp.DeletedCount
}

func TestTeamIsolation_DeleteFinishedJobs(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedJobFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newJobRouter(database, teamSvc)

	// userB clears finished jobs: only Team B's job goes; Team A's survives.
	rec := do(router, f.userB, "DELETE", "/api/jobs/finished")
	if rec.Code != http.StatusOK {
		t.Fatalf("userB delete finished: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if n := deletedCount(t, rec); n != 1 {
		t.Fatalf("userB delete finished: want deleted_count 1, got %d", n)
	}
	if n := testutil.CountJobExecutions(t, database, f.hlA); n != 1 {
		t.Fatalf("Team A job should survive userB's bulk delete, found %d job(s)", n)
	}
	if n := testutil.CountJobExecutions(t, database, f.hlB); n != 0 {
		t.Fatalf("Team B job should be deleted, found %d job(s)", n)
	}

	// Admin clears everything that remains.
	rec = do(router, f.admin, "DELETE", "/api/jobs/finished")
	if rec.Code != http.StatusOK || deletedCount(t, rec) != 1 {
		t.Fatalf("admin delete finished: want 200 with deleted_count 1, got %d %s", rec.Code, rec.Body.String())
	}
}

// A non-admin with no team membership must delete nothing (fail closed).
func TestTeamIsolation_DeleteFinishedJobs_NoTeams(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedJobFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newJobRouter(database, teamSvc)

	loner := testutil.CreateTestUser(t, database, "job-loner", "job-loner@test.local", testutil.DefaultTestPassword, "user")
	rec := do(router, loner, "DELETE", "/api/jobs/finished")
	if rec.Code != http.StatusOK || deletedCount(t, rec) != 0 {
		t.Fatalf("teamless user delete finished: want 200 with deleted_count 0, got %d %s", rec.Code, rec.Body.String())
	}
	if testutil.CountJobExecutions(t, database, f.hlA)+testutil.CountJobExecutions(t, database, f.hlB) != 2 {
		t.Fatal("teamless user must not delete any jobs")
	}
}

// Teams off: shared workspace, any user clears all finished jobs (pre-fix behaviour).
func TestTeamIsolation_DeleteFinishedJobs_TeamsDisabled(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedJobFixtures(t, database, false)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newJobRouter(database, teamSvc)

	rec := do(router, f.userB, "DELETE", "/api/jobs/finished")
	if rec.Code != http.StatusOK || deletedCount(t, rec) != 2 {
		t.Fatalf("teams off: userB delete finished: want 200 with deleted_count 2, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestTeamIsolation_ClearBenchmarkBlocklistEntry(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedJobFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newJobRouter(database, teamSvc)

	entryA := testutil.CreateBlocklistEntry(t, database, f.agentA, &f.jobA)
	entryGlobal := testutil.CreateBlocklistEntry(t, database, f.agentA, nil)

	clear := func(u *models.User, job, entry uuid.UUID) *httptest.ResponseRecorder {
		return do(router, u, "POST", "/api/jobs/"+job.String()+"/benchmark-blocklist/"+entry.String()+"/clear")
	}

	// userB is authorised for jobB but must not be able to clear jobA's entry through it.
	if rec := clear(f.userB, f.jobB, entryA); rec.Code != http.StatusNotFound {
		t.Fatalf("userB clear jobA entry via jobB: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// userB cannot even address jobA.
	if rec := clear(f.userB, f.jobA, entryA); rec.Code != http.StatusNotFound {
		t.Fatalf("userB clear via jobA: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// Global entries need admin.
	if rec := clear(f.userA, f.jobA, entryGlobal); rec.Code != http.StatusNotFound {
		t.Fatalf("userA clear global entry: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// Owner clears their own job's entry.
	if rec := clear(f.userA, f.jobA, entryA); rec.Code != http.StatusNoContent {
		t.Fatalf("userA clear own entry: want 204, got %d %s", rec.Code, rec.Body.String())
	}
	// Admin clears the global entry.
	if rec := clear(f.admin, f.jobA, entryGlobal); rec.Code != http.StatusNoContent {
		t.Fatalf("admin clear global entry: want 204, got %d %s", rec.Code, rec.Body.String())
	}
}
