package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/gorilla/mux"
)

/*
 * Team detail reads (/api/teams/{id}, /members, /clients, /agents) must be limited to members
 * of that team or system admins when Multi-Team Mode is on (GH #100).
 *
 * Shares the test database with other packages: run under `go test -p 1`, no t.Parallel().
 */

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

func get(router http.Handler, u *models.User, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestTeamIsolation_TeamDetailRoutes(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, true)

	admin := testutil.CreateTestUser(t, database, "tm-admin", "tm-admin@test.local", testutil.DefaultTestPassword, "admin")
	userA := testutil.CreateTestUser(t, database, "tm-usera", "tm-usera@test.local", testutil.DefaultTestPassword, "user")
	userB := testutil.CreateTestUser(t, database, "tm-userb", "tm-userb@test.local", testutil.DefaultTestPassword, "user")
	teamA := testutil.CreateTestTeam(t, database, "Team A")
	teamB := testutil.CreateTestTeam(t, database, "Team B")
	testutil.AddUserToTeam(t, database, teamA, userA.ID)
	testutil.AddUserToTeam(t, database, teamB, userB.ID)
	clientA := testutil.CreateTestClient(t, database, "Client A", testutil.ClientCloudOpts{})
	testutil.AssignClientToTeam(t, database, clientA, teamA)

	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	h := NewTeamHandler(teamSvc)

	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	api.HandleFunc("/teams/{id}", h.GetTeam).Methods("GET")
	api.HandleFunc("/teams/{id}/members", h.ListMembers).Methods("GET")
	api.HandleFunc("/teams/{id}/clients", h.ListTeamClients).Methods("GET")
	api.HandleFunc("/teams/{id}/agents", h.ListTeamAgents).Methods("GET")

	for _, path := range []string{
		"/api/teams/" + teamA.String(),
		"/api/teams/" + teamA.String() + "/members",
		"/api/teams/" + teamA.String() + "/clients",
		"/api/teams/" + teamA.String() + "/agents",
	} {
		if rec := get(root, userB, path); rec.Code != http.StatusNotFound {
			t.Fatalf("userB GET %s: want 404, got %d %s", path, rec.Code, rec.Body.String())
		}
		if rec := get(root, userA, path); rec.Code != http.StatusOK {
			t.Fatalf("userA (member) GET %s: want 200, got %d %s", path, rec.Code, rec.Body.String())
		}
		if rec := get(root, admin, path); rec.Code != http.StatusOK {
			t.Fatalf("admin GET %s: want 200, got %d %s", path, rec.Code, rec.Body.String())
		}
	}

	// Teams off: shared workspace, the non-member can read team details.
	testutil.SetTeamsEnabled(t, database, false)
	teamSvc.InvalidateTeamsEnabledCache()
	if rec := get(root, userB, "/api/teams/"+teamA.String()+"/clients"); rec.Code != http.StatusOK {
		t.Fatalf("teams off: userB GET team clients: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}
