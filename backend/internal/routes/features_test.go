package routes

import (
	"net/http"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/gorilla/mux"
)

// POST /api/agents/{id}/force-cleanup must be admin-only (GH #100). The package-level
// JobIntegrationManager is nil in tests, so an admin reaches the 503 "integration not
// available" branch, which proves the role gate runs before the integration lookup.
func TestForceCleanup_AdminOnly(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedIsolationFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()

	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	SetupAgentRoutes(api, nil, database)

	rec := do(root, f.userA, "POST", "/api/agents/1/force-cleanup", "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin force-cleanup: want 403, got %d %s", rec.Code, rec.Body.String())
	}

	rec = do(root, f.admin, "POST", "/api/agents/1/force-cleanup", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin force-cleanup with no integration: want 503, got %d %s", rec.Code, rec.Body.String())
	}
}
