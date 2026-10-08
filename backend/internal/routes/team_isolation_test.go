package routes

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
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
 * Multi-Team isolation for the hashlist / client / wordlist / potfile routes (GH #100).
 *
 * Every route registered by registerHashlistRoutes that takes a hashlist, client, or
 * wordlist ID is exercised as:
 *   - userB (a member of Team B, no relationship to Team A): must be denied with the
 *     enumeration-safe response (404, or the handler's existing 403).
 *   - userA (owner, member of Team A): must NOT be denied.
 *   - admin: must NOT be denied.
 * A second test flips teams_enabled off and asserts userB is no longer denied, so the
 * shared-workspace semantics are preserved.
 *
 * These tests share one Postgres database with other packages and truncate it, so the
 * package must run under `go test -p 1` (see .github/workflows/test.yml) and never uses
 * t.Parallel().
 */

// stubJobsHandler stands in for the real jobs handler, which needs a 21-dependency
// JobExecutionService and starts goroutines. The real delegates perform no team check,
// so the gate under test lives in the hashlistHandler wrappers.
type stubJobsHandler struct{}

func (stubJobsHandler) GetAvailablePresetJobs(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (stubJobsHandler) CreateJobFromHashlist(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
func (stubJobsHandler) JobExecutionService() *services.JobExecutionService { return nil }

type isoFixtures struct {
	dataDir string

	admin, userA, userB *models.User
	teamA, teamB        uuid.UUID
	clientA, clientB    uuid.UUID

	// Team A resources (the targets userB must not reach)
	hlA          int64 // general-purpose, read-only rows
	hlAArchived  int64 // pre-archived, for /unarchive
	hlAArchiveMe int64 // for /archive (mutates)
	hlADeleteMe  int64 // for DELETE /{id} (destructive)
	assocA       uuid.UUID
	assocADelete uuid.UUID
	clientWLA    uuid.UUID
	clientWLADel uuid.UUID
	potfilePathA string
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

// newIsolationRouter wires the production hashlist routes behind the production
// TeamAccessMiddleware so teams_enabled / user_team_ids come from real code, not the test.
func newIsolationRouter(t *testing.T, database *db.DB, dataDir string, teamSvc *services.TeamService) http.Handler {
	t.Helper()
	cfg := &config.Config{
		DataDir:       dataDir,
		HashUploadDir: filepath.Join(dataDir, "hashlist_uploads"),
	}
	clientPotfileSvc := services.NewClientPotfileService(dataDir, repository.NewClientPotfileRepository(database), nil)

	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	registerHashlistRoutes(api, database.DB, cfg, nil, clientPotfileSvc, nil, stubJobsHandler{}, teamSvc)
	return root
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedIsolationFixtures(t *testing.T, database *db.DB, teamsEnabled bool) *isoFixtures {
	t.Helper()
	ctx := context.Background()
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, teamsEnabled)

	f := &isoFixtures{dataDir: t.TempDir()}
	f.admin = testutil.CreateTestUser(t, database, "iso-admin", "iso-admin@test.local", testutil.DefaultTestPassword, "admin")
	f.userA = testutil.CreateTestUser(t, database, "iso-usera", "iso-usera@test.local", testutil.DefaultTestPassword, "user")
	f.userB = testutil.CreateTestUser(t, database, "iso-userb", "iso-userb@test.local", testutil.DefaultTestPassword, "user")

	f.teamA = testutil.CreateTestTeam(t, database, "Team A")
	f.teamB = testutil.CreateTestTeam(t, database, "Team B")
	testutil.AddUserToTeam(t, database, f.teamA, f.userA.ID)
	testutil.AddUserToTeam(t, database, f.teamB, f.userB.ID)

	f.clientA = testutil.CreateTestClient(t, database, "Client A", testutil.ClientCloudOpts{})
	f.clientB = testutil.CreateTestClient(t, database, "Client B", testutil.ClientCloudOpts{})
	testutil.AssignClientToTeam(t, database, f.clientA, f.teamA)
	testutil.AssignClientToTeam(t, database, f.clientB, f.teamB)

	f.hlA = testutil.CreateTestHashlist(t, database, f.userA.ID, f.clientA, "hl-a")
	f.hlAArchived = testutil.CreateTestHashlist(t, database, f.userA.ID, f.clientA, "hl-a-archived")
	testutil.ArchiveTestHashlist(t, database, f.hlAArchived)
	f.hlAArchiveMe = testutil.CreateTestHashlist(t, database, f.userA.ID, f.clientA, "hl-a-archive-me")
	f.hlADeleteMe = testutil.CreateTestHashlist(t, database, f.userA.ID, f.clientA, "hl-a-delete-me")

	assocRepo := repository.NewAssociationWordlistRepository(database)
	for _, target := range []*uuid.UUID{&f.assocA, &f.assocADelete} {
		path := filepath.Join(f.dataDir, "wordlists", "association", fmt.Sprint(f.hlA), uuid.New().String()+".txt")
		writeFixtureFile(t, path, "candidate\n")
		wl := &models.AssociationWordlist{HashlistID: f.hlA, FilePath: path, FileName: "assoc.txt", FileSize: 10, LineCount: 1, MD5Hash: "x"}
		if err := assocRepo.Create(ctx, wl); err != nil {
			t.Fatalf("create association wordlist: %v", err)
		}
		*target = wl.ID
	}

	cwRepo := repository.NewClientWordlistRepository(database)
	for _, target := range []*uuid.UUID{&f.clientWLA, &f.clientWLADel} {
		path := filepath.Join(f.dataDir, "wordlists", "clients", f.clientA.String(), uuid.New().String()+".txt")
		writeFixtureFile(t, path, "candidate\n")
		wl := &models.ClientWordlist{ClientID: f.clientA, FilePath: path, FileName: "client.txt", FileSize: 10, LineCount: 1}
		if err := cwRepo.Create(ctx, wl); err != nil {
			t.Fatalf("create client wordlist: %v", err)
		}
		*target = wl.ID
	}

	// Client potfile: the nil-PotfileService fallback path used by handleDownloadClientPotfile.
	f.potfilePathA = filepath.Join(f.dataDir, "wordlists", "clients", f.clientA.String(), "potfile.txt")
	writeFixtureFile(t, f.potfilePathA, "hash:password\n")

	return f
}

// asUser injects what RequireAuth would: "user_id" (string) and "user_role".
func asUser(req *http.Request, u *models.User) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	return req.WithContext(ctx)
}

func do(router http.Handler, u *models.User, method, path string, body string, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, asUser(req, u))
	return rec
}

// denial is the exact response the team gate writes. Matching on the exact JSON body (not a
// substring) distinguishes the gate from handler-level 404s such as
// {"error":"Hashlist not found or not archived"}.
type denial struct {
	code int
	body string
}

var (
	denyHashlist = denial{http.StatusNotFound, `{"error":"Hashlist not found"}`}
	denyClient   = denial{http.StatusNotFound, `{"error":"Client not found"}`}
	// handleUpdateHashlistClient predates the 404 convention and keeps its 403.
	denyUpdateClient = denial{http.StatusForbidden, `{"error":"You do not have access to this hashlist"}`}
)

func (d denial) matches(rec *httptest.ResponseRecorder) bool {
	return rec.Code == d.code && strings.TrimSpace(rec.Body.String()) == d.body
}

type isoRoute struct {
	name      string
	method    string
	path      func(f *isoFixtures) string
	body      string
	ctype     string
	deny      denial
	skipAdmin bool // destructive rows: the owner consumes the fixture
}

func isolationRoutes() []isoRoute {
	hl := func(suffix string, id func(f *isoFixtures) int64) func(f *isoFixtures) string {
		return func(f *isoFixtures) string { return fmt.Sprintf("/api/hashlists/%d%s", id(f), suffix) }
	}
	hlA := func(f *isoFixtures) int64 { return f.hlA }
	client := func(suffix string) func(f *isoFixtures) string {
		return func(f *isoFixtures) string { return "/api/clients/" + f.clientA.String() + suffix }
	}
	j := "application/json"

	return []isoRoute{
		// --- hashlist-keyed (read) ---
		{name: "GET hashlist", method: "GET", path: hl("", hlA), deny: denyHashlist},
		{name: "GET hashes", method: "GET", path: hl("/hashes", hlA), deny: denyHashlist},
		{name: "GET download", method: "GET", path: hl("/download", hlA), deny: denyHashlist},
		{name: "GET deletion-progress", method: "GET", path: hl("/deletion-progress", hlA), deny: denyHashlist},
		{name: "GET processing-progress", method: "GET", path: hl("/processing-progress", hlA), deny: denyHashlist},
		{name: "GET invalid-hashes", method: "GET", path: hl("/invalid-hashes", hlA), deny: denyHashlist},
		{name: "GET available-jobs", method: "GET", path: hl("/available-jobs", hlA), deny: denyHashlist},
		{name: "GET association-wordlists", method: "GET", path: hl("/association-wordlists", hlA), deny: denyHashlist},
		// --- hashlist-keyed (mutating, non-destructive for the owner) ---
		{name: "POST create-job", method: "POST", path: hl("/create-job", hlA), body: `{}`, ctype: j, deny: denyHashlist},
		{name: "POST association-wordlists upload", method: "POST", path: hl("/association-wordlists", hlA), body: ``, ctype: "multipart/form-data; boundary=x", deny: denyHashlist},
		{name: "POST confirm", method: "POST", path: hl("/confirm", hlA), body: `{"action":"proceed"}`, ctype: j, deny: denyHashlist},
		{name: "PUT revalidate", method: "PUT", path: hl("/revalidate", hlA), body: `{"hash_type_id":0}`, ctype: j, deny: denyHashlist},
		{name: "PATCH hash-type", method: "PATCH", path: hl("/hash-type", hlA), body: `{"hash_type_id":0}`, ctype: j, deny: denyHashlist},
		{name: "PATCH client", method: "PATCH", path: hl("/client", hlA), body: func() string { return "" }(), ctype: j, deny: denyUpdateClient},
		// --- association wordlists keyed by wordlist id ---
		{name: "GET association-wordlist", method: "GET", path: func(f *isoFixtures) string { return "/api/association-wordlists/" + f.assocA.String() }, deny: denyHashlist},
		{name: "GET association-wordlist download", method: "GET", path: func(f *isoFixtures) string { return "/api/association-wordlists/" + f.assocA.String() + "/download" }, deny: denyHashlist},
		// --- client-keyed ---
		{name: "GET client wordlists", method: "GET", path: client("/wordlists"), deny: denyClient},
		{name: "POST client wordlist upload", method: "POST", path: client("/wordlists"), body: ``, ctype: "multipart/form-data; boundary=x", deny: denyClient},
		{name: "GET client wordlist", method: "GET", path: func(f *isoFixtures) string {
			return "/api/clients/" + f.clientA.String() + "/wordlists/" + f.clientWLA.String()
		}, deny: denyClient},
		{name: "GET client wordlist download", method: "GET", path: func(f *isoFixtures) string {
			return "/api/clients/" + f.clientA.String() + "/wordlists/" + f.clientWLA.String() + "/download"
		}, deny: denyClient},
		{name: "GET client potfile", method: "GET", path: client("/potfile"), deny: denyClient},
		{name: "GET client potfile download", method: "GET", path: client("/potfile/download"), deny: denyClient},
		{name: "GET client association-wordlists", method: "GET", path: client("/association-wordlists"), deny: denyClient},
		// --- destructive rows last; the owner consumes the fixture so admin is skipped ---
		{name: "POST archive", method: "POST", path: hl("/archive", func(f *isoFixtures) int64 { return f.hlAArchiveMe }), deny: denyHashlist, skipAdmin: true},
		{name: "POST unarchive", method: "POST", path: hl("/unarchive", func(f *isoFixtures) int64 { return f.hlAArchived }), deny: denyHashlist, skipAdmin: true},
		{name: "DELETE association-wordlist", method: "DELETE", path: func(f *isoFixtures) string { return "/api/association-wordlists/" + f.assocADelete.String() }, deny: denyHashlist, skipAdmin: true},
		{name: "DELETE client wordlist", method: "DELETE", path: func(f *isoFixtures) string {
			return "/api/clients/" + f.clientA.String() + "/wordlists/" + f.clientWLADel.String()
		}, deny: denyClient, skipAdmin: true},
		{name: "DELETE hashlist", method: "DELETE", path: hl("", func(f *isoFixtures) int64 { return f.hlADeleteMe }), deny: denyHashlist, skipAdmin: true},
	}
}

func TestTeamIsolation_HashlistRoutes(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedIsolationFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newIsolationRouter(t, database, f.dataDir, teamSvc)

	for _, rt := range isolationRoutes() {
		rt := rt
		body := rt.body
		if rt.name == "PATCH client" {
			body = `{"client_id":"` + f.clientA.String() + `"}`
		}
		t.Run(rt.name, func(t *testing.T) {
			// Cross-team user: must be denied with the enumeration-safe response.
			rec := do(router, f.userB, rt.method, rt.path(f), body, rt.ctype)
			if !rt.deny.matches(rec) {
				t.Fatalf("userB %s %s: want %d %s, got %d %s", rt.method, rt.path(f), rt.deny.code, rt.deny.body, rec.Code, strings.TrimSpace(rec.Body.String()))
			}

			// Owner: must not be denied (any non-auth status is acceptable; several handlers
			// legitimately return 400/409 for an empty fixture).
			rec = do(router, f.userA, rt.method, rt.path(f), body, rt.ctype)
			if rt.deny.matches(rec) || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("userA (owner) %s %s: unexpectedly denied: %d %s", rt.method, rt.path(f), rec.Code, strings.TrimSpace(rec.Body.String()))
			}

			if rt.skipAdmin {
				return
			}
			rec = do(router, f.admin, rt.method, rt.path(f), body, rt.ctype)
			if rt.deny.matches(rec) || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("admin %s %s: unexpectedly denied: %d %s", rt.method, rt.path(f), rec.Code, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

// With Multi-Team Mode off the instance is a shared workspace: the cross-team user must
// reach every route exactly as before the fix.
func TestTeamIsolation_HashlistRoutes_TeamsDisabled(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedIsolationFixtures(t, database, false)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newIsolationRouter(t, database, f.dataDir, teamSvc)

	for _, rt := range isolationRoutes() {
		rt := rt
		body := rt.body
		if rt.name == "PATCH client" {
			body = `{"client_id":"` + f.clientA.String() + `"}`
		}
		t.Run(rt.name, func(t *testing.T) {
			rec := do(router, f.userB, rt.method, rt.path(f), body, rt.ctype)
			if rt.deny.matches(rec) || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("teams off: userB %s %s: unexpectedly denied: %d %s", rt.method, rt.path(f), rec.Code, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

// Sanity check that the fixture actually exercises the vulnerable path: with teams on,
// userB's denial must come from the gate, not from the hashlist being missing.
func TestTeamIsolation_FixtureSanity(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seedIsolationFixtures(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newIsolationRouter(t, database, f.dataDir, teamSvc)

	rec := do(router, f.userA, "GET", fmt.Sprintf("/api/hashlists/%d", f.hlA), "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner GET hashlist: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	rec = do(router, f.userA, "GET", "/api/clients/"+f.clientA.String()+"/potfile/download", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hash:password") {
		t.Fatalf("owner potfile download: want 200 with fixture content, got %d %q", rec.Code, rec.Body.String())
	}
}
