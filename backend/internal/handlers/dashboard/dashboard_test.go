package dashboard

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
 * Team scoping for the dashboard endpoints:
 *   GET /api/dashboard/stats
 *   GET /api/dashboard/recent-cracks
 *   GET /api/dashboard/agent-health
 *
 * Default view is "mine" (own jobs/hashlists/cracks/agents). userA (Team A)
 * must only ever see Team A's data; a foreign team_id is ignored; only admins
 * get ?scope=all when teams are enabled; with teams disabled "all" is open to
 * everyone (one shared workspace).
 *
 * Shares the test database with other packages: run under `go test -p 1`, no t.Parallel().
 */

type dashFixtures struct {
	admin, userA, userB *models.User
	teamA, teamB        uuid.UUID
	clientA, clientB    uuid.UUID
	hlA, hlB            int64
	jobA, jobB          uuid.UUID
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

func seed(t *testing.T, database *db.DB, teamsEnabled bool) *dashFixtures {
	t.Helper()
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, teamsEnabled)

	f := &dashFixtures{}
	f.admin = testutil.CreateTestUser(t, database, "dash-admin", "dash-admin@test.local", testutil.DefaultTestPassword, "admin")
	f.userA = testutil.CreateTestUser(t, database, "dash-usera", "dash-usera@test.local", testutil.DefaultTestPassword, "user")
	f.userB = testutil.CreateTestUser(t, database, "dash-userb", "dash-userb@test.local", testutil.DefaultTestPassword, "user")

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
	f.jobA = testutil.CreateTestJobExecution(t, database, f.hlA, f.userA.ID, "running")
	f.jobB = testutil.CreateTestJobExecution(t, database, f.hlB, f.userB.ID, "running")
	testutil.CreateTestJobExecution(t, database, f.hlA, f.userA.ID, "completed")

	f.agentA = testutil.CreateTestAgent(t, database, f.userA.ID, nil)

	// One cracked hash in hashlist A.
	var hashID uuid.UUID
	if err := database.QueryRow(`
		INSERT INTO hashes (hash_value, original_hash, hash_type_id, is_cracked, password, last_updated)
		VALUES ('5f4dcc3b5aa765d61d8327deb882cf99', '5f4dcc3b5aa765d61d8327deb882cf99', 0, true, 'password', NOW())
		RETURNING id`).Scan(&hashID); err != nil {
		t.Fatalf("insert cracked hash: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO hashlist_hashes (hashlist_id, hash_id) VALUES ($1, $2)`, f.hlA, hashID); err != nil {
		t.Fatalf("link cracked hash: %v", err)
	}
	return f
}

func newRouter(database *db.DB, teamSvc *services.TeamService) http.Handler {
	h := NewHandler(repository.NewDashboardRepository(database), repository.NewAgentRepository(database))
	root := mux.NewRouter()
	api := root.PathPrefix("/api").Subrouter()
	api.Use(middleware.TeamAccessMiddleware(teamSvc))
	api.HandleFunc("/dashboard/stats", h.GetStats).Methods("GET")
	api.HandleFunc("/dashboard/recent-cracks", h.GetRecentCracks).Methods("GET")
	api.HandleFunc("/dashboard/agent-health", h.GetAgentHealth).Methods("GET")
	api.HandleFunc("/dashboard/attention", h.GetAttention).Methods("GET")
	api.HandleFunc("/dashboard/crack-trend", h.GetCrackTrend).Methods("GET")
	return root
}

func do(router http.Handler, u *models.User, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, u.ID.String())
	ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, u.Role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func stats(t *testing.T, router http.Handler, u *models.User, path string) StatsResponse {
	t.Helper()
	rec := do(router, u, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s as %s: want 200, got %d %s", path, u.Username, rec.Code, rec.Body.String())
	}
	var out StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	return out
}

func TestDashboard_StatsTeamScoped(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	a := stats(t, router, f.userA, "/api/dashboard/stats")
	if a.Jobs.Running != 1 || a.Jobs.Completed24h != 1 || a.Hashlists.Active != 1 {
		t.Fatalf("userA: want running=1 completed24h=1 active=1, got %+v %+v", a.Jobs, a.Hashlists)
	}
	if a.Cracks.Last24h != 1 {
		t.Fatalf("userA: want 1 crack in 24h, got %d", a.Cracks.Last24h)
	}
	if a.Agents.Total != 1 || a.Agents.Online != 1 {
		t.Fatalf("userA: want 1 online agent (owned), got %+v", a.Agents)
	}

	b := stats(t, router, f.userB, "/api/dashboard/stats")
	if b.Jobs.Running != 1 || b.Jobs.Completed24h != 0 || b.Hashlists.Active != 1 || b.Cracks.Last24h != 0 {
		t.Fatalf("userB: want running=1 completed24h=0 active=1 cracks=0, got %+v %+v %+v", b.Jobs, b.Hashlists, b.Cracks)
	}
	if b.Agents.Total != 0 {
		t.Fatalf("userB: want no agents, got %+v", b.Agents)
	}

	adm := stats(t, router, f.admin, "/api/dashboard/stats?scope=all")
	if adm.Jobs.Running != 2 || adm.Hashlists.Active != 2 || adm.Agents.Total != 1 {
		t.Fatalf("admin: want running=2 active=2 agents=1, got %+v %+v %+v", adm.Jobs, adm.Hashlists, adm.Agents)
	}

	// A foreign team_id is ignored for a non-member: userB still sees Team B only.
	bForeign := stats(t, router, f.userB, "/api/dashboard/stats?team_id="+f.teamA.String())
	if bForeign.Jobs.Completed24h != 0 || bForeign.Cracks.Last24h != 0 {
		t.Fatalf("userB with Team A id: must not see Team A data, got %+v %+v", bForeign.Jobs, bForeign.Cracks)
	}
	// An admin can narrow to a team.
	admA := stats(t, router, f.admin, "/api/dashboard/stats?scope=teams&team_id="+f.teamA.String())
	if admA.Jobs.Running != 1 || admA.Hashlists.Active != 1 {
		t.Fatalf("admin scoped to Team A: want running=1 active=1, got %+v %+v", admA.Jobs, admA.Hashlists)
	}
}

func TestDashboard_TeamsDisabledIsGlobal(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, false)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	b := stats(t, router, f.userB, "/api/dashboard/stats?scope=all")
	if b.Jobs.Running != 2 || b.Hashlists.Active != 2 || b.Cracks.Last24h != 1 || b.Agents.Total != 1 {
		t.Fatalf("teams disabled: userB should see everything, got %+v %+v %+v %+v", b.Jobs, b.Hashlists, b.Cracks, b.Agents)
	}
}

func TestDashboard_RecentCracksScoped(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	type resp struct {
		Cracks []repository.RecentCrack `json:"cracks"`
	}
	get := func(u *models.User, view string) resp {
		rec := do(router, u, "/api/dashboard/recent-cracks?limit=10&scope="+view)
		if rec.Code != http.StatusOK {
			t.Fatalf("recent-cracks as %s: %d %s", u.Username, rec.Code, rec.Body.String())
		}
		var out resp
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	a := get(f.userA, "mine")
	if len(a.Cracks) != 1 {
		t.Fatalf("userA: want 1 recent crack, got %d", len(a.Cracks))
	}
	if a.Cracks[0].Plaintext != "password" || a.Cracks[0].HashlistID == nil || *a.Cracks[0].HashlistID != f.hlA {
		t.Fatalf("userA: crack should carry plaintext and hashlist A, got %+v", a.Cracks[0])
	}
	if b := get(f.userB, "teams"); len(b.Cracks) != 0 {
		t.Fatalf("userB: want no cracks from Team A, got %d", len(b.Cracks))
	}
	if adm := get(f.admin, "mine"); len(adm.Cracks) != 0 {
		t.Fatalf("admin mine: owns no hashlists, want 0 cracks, got %d", len(adm.Cracks))
	}
	if adm := get(f.admin, "all"); len(adm.Cracks) != 1 {
		t.Fatalf("admin: want 1 crack, got %d", len(adm.Cracks))
	}
}

func TestDashboard_AgentHealthScoped(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	type resp struct {
		Agents []repository.AgentHealth `json:"agents"`
	}
	get := func(u *models.User, view string) resp {
		rec := do(router, u, "/api/dashboard/agent-health?scope="+view)
		if rec.Code != http.StatusOK {
			t.Fatalf("agent-health as %s: %d %s", u.Username, rec.Code, rec.Body.String())
		}
		var out resp
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	a := get(f.userA, "mine")
	if len(a.Agents) != 1 || a.Agents[0].ID != f.agentA || a.Agents[0].Status != "active" {
		t.Fatalf("userA: want own agent %d active, got %+v", f.agentA, a.Agents)
	}
	if a.Agents[0].Warnings == nil {
		t.Fatalf("warnings must be an array, not null")
	}
	if b := get(f.userB, "teams"); len(b.Agents) != 0 {
		t.Fatalf("userB: want no agents, got %+v", b.Agents)
	}
	if adm := get(f.admin, "all"); len(adm.Agents) != 1 {
		t.Fatalf("admin: want 1 agent, got %+v", adm.Agents)
	}
}

// Mine vs Teams vs All with teams enabled: a teammate's job shows up under
// "teams" but not "mine", and neither shows the teammate's agent under "mine";
// "all" is admin-only.
func TestDashboard_ScopeViewsTeamsEnabled(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	mate := testutil.CreateTestUser(t, database, "dash-mate", "dash-mate@test.local", testutil.DefaultTestPassword, "user")
	testutil.AddUserToTeam(t, database, f.teamA, mate.ID)
	testutil.CreateTestJobExecution(t, database, f.hlA, mate.ID, "running")
	testutil.CreateTestAgent(t, database, mate.ID, nil)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	mine := stats(t, router, f.userA, "/api/dashboard/stats?scope=mine")
	if mine.Jobs.Running != 1 || mine.Agents.Total != 1 {
		t.Fatalf("userA mine: want running=1 agents=1 (own only), got %+v %+v", mine.Jobs, mine.Agents)
	}
	team := stats(t, router, f.userA, "/api/dashboard/stats?scope=teams")
	// Team agents follow the existing team-resolution rules (GetAgentsForTeam);
	// the caller's own agent is always included.
	if team.Jobs.Running != 2 || team.Agents.Total < 1 || team.Hashlists.Active != 1 {
		t.Fatalf("userA teams: want running=2 agents>=1 hashlists=1 (Team A only), got %+v %+v %+v", team.Jobs, team.Agents, team.Hashlists)
	}
	if def := stats(t, router, f.userA, "/api/dashboard/stats"); def.Jobs.Running != 1 {
		t.Fatalf("default view must be mine: want running=1, got %+v", def.Jobs)
	}

	for _, path := range []string{"/api/dashboard/stats?scope=all", "/api/dashboard/recent-cracks?scope=all", "/api/dashboard/agent-health?scope=all"} {
		if rec := do(router, f.userA, path); rec.Code != http.StatusForbidden {
			t.Fatalf("%s as non-admin: want 403, got %d", path, rec.Code)
		}
	}

	all := stats(t, router, f.admin, "/api/dashboard/stats?scope=all")
	if all.Jobs.Running != 3 || all.Hashlists.Active != 2 {
		t.Fatalf("admin all: want running=3 hashlists=2, got %+v %+v", all.Jobs, all.Hashlists)
	}
	// The admin belongs to no team: "teams" is empty, not everything.
	admTeams := stats(t, router, f.admin, "/api/dashboard/stats?scope=teams")
	if admTeams.Jobs.Running != 0 || admTeams.Hashlists.Active != 0 {
		t.Fatalf("admin teams without memberships: want empty, got %+v %+v", admTeams.Jobs, admTeams.Hashlists)
	}
	// userB's mine/teams never include Team A, even with Team A's id.
	bTeams := stats(t, router, f.userB, "/api/dashboard/stats?scope=teams&team_id="+f.teamA.String())
	if bTeams.Jobs.Running != 1 || bTeams.Cracks.Last24h != 0 {
		t.Fatalf("userB teams with Team A id: want Team B only, got %+v %+v", bTeams.Jobs, bTeams.Cracks)
	}
}

// With teams disabled there is one shared workspace: mine is still personal,
// teams and all show everything to everyone.
func TestDashboard_ScopeViewsTeamsDisabled(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, false)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	mine := stats(t, router, f.userB, "/api/dashboard/stats")
	if mine.Jobs.Running != 1 || mine.Hashlists.Active != 1 || mine.Cracks.Last24h != 0 || mine.Agents.Total != 0 {
		t.Fatalf("userB mine: want own data only, got %+v %+v %+v %+v", mine.Jobs, mine.Hashlists, mine.Cracks, mine.Agents)
	}
	for _, view := range []string{"all", "teams"} {
		got := stats(t, router, f.userB, "/api/dashboard/stats?scope="+view)
		if got.Jobs.Running != 2 || got.Hashlists.Active != 2 || got.Agents.Total != 1 {
			t.Fatalf("userB %s: want everything, got %+v %+v %+v", view, got.Jobs, got.Hashlists, got.Agents)
		}
	}
}

func TestDashboard_AttentionScoped(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	failed := testutil.CreateTestJobExecution(t, database, f.hlA, f.userA.ID, "failed")
	if _, err := database.Exec(`UPDATE job_executions SET error_message = 'boom', completed_at = NOW() WHERE id = $1`, failed); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}
	if _, err := database.Exec(`UPDATE hashlists SET status = 'awaiting_validation_decision' WHERE id = $1`, f.hlB); err != nil {
		t.Fatalf("hashlist status: %v", err)
	}
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	type resp struct {
		Items []repository.AttentionItem `json:"items"`
	}
	get := func(u *models.User, view string) []repository.AttentionItem {
		rec := do(router, u, "/api/dashboard/attention?scope="+view)
		if rec.Code != http.StatusOK {
			t.Fatalf("attention %s as %s: %d %s", view, u.Username, rec.Code, rec.Body.String())
		}
		var out resp
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out.Items
	}
	kinds := func(items []repository.AttentionItem) map[string]string {
		m := map[string]string{}
		for _, i := range items {
			m[i.Kind] = i.EntityID
		}
		return m
	}

	a := kinds(get(f.userA, "mine"))
	if a["job_failed"] != failed.String() {
		t.Fatalf("userA mine: want failed job %s, got %v", failed, a)
	}
	if _, ok := a["hashlist_awaiting_decision"]; ok {
		t.Fatalf("userA must not see userB's hashlist, got %v", a)
	}
	b := kinds(get(f.userB, "teams"))
	if _, ok := b["job_failed"]; ok {
		t.Fatalf("userB must not see Team A's failed job, got %v", b)
	}
	if b["hashlist_awaiting_decision"] == "" {
		t.Fatalf("userB: want own hashlist awaiting decision, got %v", b)
	}
	adm := kinds(get(f.admin, "all"))
	if adm["job_failed"] == "" || adm["hashlist_awaiting_decision"] == "" {
		t.Fatalf("admin all: want both items, got %v", adm)
	}
	if items := get(f.userA, "mine"); len(items) > 0 && items[0].Severity != "error" {
		t.Fatalf("errors must sort first, got %+v", items)
	}
	if rec := do(router, f.userA, "/api/dashboard/attention?scope=all"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin all: want 403, got %d", rec.Code)
	}
}

func TestDashboard_CrackTrendScoped(t *testing.T) {
	requireTestDB(t)
	database := testutil.SetupTestDB(t)
	f := seed(t, database, true)
	teamSvc := newTeamService(database)
	teamSvc.InvalidateTeamsEnabledCache()
	router := newRouter(database, teamSvc)

	get := func(u *models.User, query string) repository.CrackTrend {
		rec := do(router, u, "/api/dashboard/crack-trend"+query)
		if rec.Code != http.StatusOK {
			t.Fatalf("crack-trend%s as %s: %d %s", query, u.Username, rec.Code, rec.Body.String())
		}
		var out repository.CrackTrend
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	sum := func(tr repository.CrackTrend) int64 {
		var n int64
		for _, d := range tr.Days {
			n += d.Count
		}
		return n
	}

	a := get(f.userA, "?scope=mine&days=14")
	if len(a.Days) != 14 {
		t.Fatalf("want 14 days, got %d", len(a.Days))
	}
	if a.Days[13].Count != 1 || sum(a) != 1 {
		t.Fatalf("userA: want the seeded crack on the last day, got %+v", a.Days)
	}
	if len(a.TopHashlists) != 1 || a.TopHashlists[0].ID != f.hlA || a.TopHashlists[0].Count != 1 {
		t.Fatalf("userA: want hashlist A as top hashlist, got %+v", a.TopHashlists)
	}
	b := get(f.userB, "?scope=teams&days=7")
	if len(b.Days) != 7 || sum(b) != 0 || len(b.TopHashlists) != 0 {
		t.Fatalf("userB: want 7 empty days and no hashlists, got %+v", b)
	}
	if adm := get(f.admin, "?scope=all"); len(adm.Days) != 14 || sum(adm) != 1 {
		t.Fatalf("admin all: want 14 days with 1 crack, got %+v", adm.Days)
	}
	if rec := do(router, f.userA, "/api/dashboard/crack-trend?scope=all"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin all: want 403, got %d", rec.Code)
	}
}
