package analytics

import (
	"bytes"
	"context"
	"fmt"
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
 * POST /api/analytics/reports must reject hashlist_ids that do not belong to the request's
 * client (GH #100). The client itself is already team-checked; this closes the door where a
 * user named their own client and listed another team's hashlist IDs.
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

func postReport(router http.Handler, u *models.User, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/analytics/reports", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestTeamIsolation_CreateReportHashlistOwnership(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, true)

	admin := testutil.CreateTestUser(t, database, "an-admin", "an-admin@test.local", testutil.DefaultTestPassword, "admin")
	userA := testutil.CreateTestUser(t, database, "an-usera", "an-usera@test.local", testutil.DefaultTestPassword, "user")
	userB := testutil.CreateTestUser(t, database, "an-userb", "an-userb@test.local", testutil.DefaultTestPassword, "user")
	teamA := testutil.CreateTestTeam(t, database, "Team A")
	teamB := testutil.CreateTestTeam(t, database, "Team B")
	testutil.AddUserToTeam(t, database, teamA, userA.ID)
	testutil.AddUserToTeam(t, database, teamB, userB.ID)
	clientA := testutil.CreateTestClient(t, database, "Client A", testutil.ClientCloudOpts{})
	clientB := testutil.CreateTestClient(t, database, "Client B", testutil.ClientCloudOpts{})
	testutil.AssignClientToTeam(t, database, clientA, teamA)
	testutil.AssignClientToTeam(t, database, clientB, teamB)
	hlA := testutil.CreateTestHashlist(t, database, userA.ID, clientA, "hl-a")
	hlB := testutil.CreateTestHashlist(t, database, userB.ID, clientB, "hl-b")

	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	h := NewHandler(database, nil, teamSvc, repository.NewClientTeamRepository(database))

	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	api.HandleFunc("/analytics/reports", h.CreateReport).Methods("POST")

	body := func(client uuid.UUID, ids ...int64) string {
		idList := ""
		for i, id := range ids {
			if i > 0 {
				idList += ","
			}
			idList += fmt.Sprint(id)
		}
		return fmt.Sprintf(`{"client_id":"%s","start_date":"2026-01-01T00:00:00Z","end_date":"2026-12-31T00:00:00Z","hashlist_ids":[%s]}`, client, idList)
	}

	// Own client + another team's hashlist: rejected as a bad request.
	if rec := postReport(root, userA, body(clientA, hlB)); rec.Code != http.StatusBadRequest {
		t.Fatalf("userA report on clientA with hlB: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Mixed list: still rejected (no partial acceptance).
	if rec := postReport(root, userA, body(clientA, hlA, hlB)); rec.Code != http.StatusBadRequest {
		t.Fatalf("userA report on clientA with [hlA,hlB]: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Nonexistent ID gets the same answer as a foreign one.
	if rec := postReport(root, userA, body(clientA, 999999)); rec.Code != http.StatusBadRequest {
		t.Fatalf("userA report with nonexistent hashlist: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Legitimate request still works (duplicates tolerated).
	if rec := postReport(root, userA, body(clientA, hlA, hlA)); rec.Code != http.StatusCreated {
		t.Fatalf("userA report on clientA with hlA: want 201, got %d %s", rec.Code, rec.Body.String())
	}
	// Empty hashlist list keeps its existing meaning.
	if rec := postReport(root, userA, body(clientA)); rec.Code != http.StatusCreated {
		t.Fatalf("userA report with no hashlist_ids: want 201, got %d %s", rec.Code, rec.Body.String())
	}
	// Cross-team client is still the existing 403.
	if rec := postReport(root, userB, body(clientA, hlA)); rec.Code != http.StatusForbidden {
		t.Fatalf("userB report on clientA: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	// Admin bypasses the client check but the client/hashlist invariant still holds.
	if rec := postReport(root, admin, body(clientA, hlB)); rec.Code != http.StatusBadRequest {
		t.Fatalf("admin report on clientA with hlB: want 400, got %d %s", rec.Code, rec.Body.String())
	}
}
