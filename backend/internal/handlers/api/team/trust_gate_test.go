package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

/*
 * GET /api/teams/{id}/trust is gated on team membership like GetTeam and
 * ListTeamAgents: a non-member must get the enumeration-safe 404.
 *
 * Shares the test database: run under `go test -p 1`, no t.Parallel().
 */
func doTrust(router http.Handler, u *models.User, teamID uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/teams/"+teamID.String()+"/trust", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestListTrustedTeams_RequiresMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, true)

	admin := testutil.CreateTestUser(t, database, "trust-admin", "trust-admin@test.local", testutil.DefaultTestPassword, "admin")
	userA := testutil.CreateTestUser(t, database, "trust-usera", "trust-usera@test.local", testutil.DefaultTestPassword, "user")
	userB := testutil.CreateTestUser(t, database, "trust-userb", "trust-userb@test.local", testutil.DefaultTestPassword, "user")
	teamA := testutil.CreateTestTeam(t, database, "Trust Team A")
	teamB := testutil.CreateTestTeam(t, database, "Trust Team B")
	testutil.AddUserToTeam(t, database, teamA, userA.ID)
	testutil.AddUserToTeam(t, database, teamB, userB.ID)

	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	h := NewTeamHandler(teamSvc)
	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	api.HandleFunc("/teams/{id}/trust", h.ListTrustedTeams).Methods(http.MethodGet)

	if rec := doTrust(root, userB, teamA); rec.Code != http.StatusNotFound {
		t.Fatalf("non-member: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTrust(root, userA, teamA); rec.Code != http.StatusOK {
		t.Fatalf("member: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTrust(root, admin, teamA); rec.Code != http.StatusOK {
		t.Fatalf("admin: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}
