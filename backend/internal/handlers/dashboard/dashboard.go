// Package dashboard serves the personal dashboard: headline stats, the recent
// cracks feed and the agent-health panel.
//
// Every endpoint takes ?scope=mine|teams|all (default mine); see resolveScope.
// The team rules mirror the job list (handlers/jobs ListJobs): a non-admin only
// ever sees teams they belong to, and only admins get "all" when teams are on.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// Handler exposes the dashboard endpoints.
type Handler struct {
	repo      *repository.DashboardRepository
	agentRepo *repository.AgentRepository
}

func NewHandler(repo *repository.DashboardRepository, agentRepo *repository.AgentRepository) *Handler {
	return &Handler{repo: repo, agentRepo: agentRepo}
}

// View names accepted by the ?scope= parameter.
const (
	ViewMine  = "mine"
	ViewTeams = "teams"
	ViewAll   = "all"
)

// errAllForbidden is returned when a non-admin asks for ?scope=all while
// teams are enabled.
var errAllForbidden = errors.New("the all view requires an administrator when teams are enabled")

// resolveScope turns ?scope=mine|teams|all (default mine) and ?team_id= into
// the query scope:
//
//   - mine:  jobs the caller created, hashlists/cracks they uploaded, agents
//     they own. With teams enabled it is also limited to the caller's teams
//     (all of them, or the selected one), like /api/user/jobs.
//   - teams: everything in the caller's teams, or the selected team. Admins
//     use their own memberships here too. With teams disabled there is only
//     one team, so this is the same as all.
//   - all:   unrestricted. With teams enabled only admins may use it.
func (h *Handler) resolveScope(ctx context.Context, r *http.Request) (repository.DashboardScope, error) {
	scope := repository.DashboardScope{}
	view := r.URL.Query().Get("scope")
	switch view {
	case ViewMine, ViewTeams, ViewAll:
	default:
		view = ViewMine
	}
	userID, hasUser := middleware.GetUserIDFromContext(ctx)
	teamsEnabled := middleware.IsTeamsEnabledFromContext(ctx)
	isAdmin := middleware.IsAdminFromContext(ctx)

	if view == ViewAll || (view == ViewTeams && !teamsEnabled) {
		if teamsEnabled && !isAdmin {
			return scope, errAllForbidden
		}
		return scope, nil
	}

	if view == ViewMine {
		if !hasUser {
			// No identity: show nothing rather than everything.
			scope.TeamIDs = []uuid.UUID{}
			scope.AgentIDs = []int{}
			return scope, nil
		}
		scope.OwnerUserID = &userID
		if teamsEnabled {
			// Admins keep their own data across all teams unless one is selected.
			scope.TeamIDs = h.teamIDs(ctx, r, isAdmin, isAdmin)
		}
		owned, err := h.repo.ListOwnedAgentIDs(ctx, userID)
		if err != nil {
			debug.Warning("dashboard: owned agents for %s: %v", userID, err)
			owned = []int{}
		}
		scope.AgentIDs = owned
		return scope, nil
	}

	// teams view, teams enabled.
	scope.TeamIDs = h.teamIDs(ctx, r, isAdmin, false)
	if scope.TeamIDs == nil {
		scope.TeamIDs = []uuid.UUID{}
	}

	// Agents are not client-scoped. A team sees the agents resolved for it
	// (team assignment, member ownership, system agents) plus the caller's own.
	agentIDs := map[int]struct{}{}
	for _, teamID := range scope.TeamIDs {
		agents, err := h.agentRepo.GetAgentsForTeam(ctx, teamID)
		if err != nil {
			debug.Warning("dashboard: agents for team %s: %v", teamID, err)
			continue
		}
		for _, a := range agents {
			agentIDs[a.ID] = struct{}{}
		}
	}
	if hasUser {
		owned, err := h.repo.ListOwnedAgentIDs(ctx, userID)
		if err != nil {
			debug.Warning("dashboard: owned agents for %s: %v", userID, err)
		}
		for _, id := range owned {
			agentIDs[id] = struct{}{}
		}
	}
	ids := make([]int, 0, len(agentIDs))
	for id := range agentIDs {
		ids = append(ids, id)
	}
	scope.AgentIDs = ids
	return scope, nil
}

// teamIDs returns the teams the request is limited to: the ?team_id= team when
// the caller may see it, otherwise every team the caller belongs to (an empty,
// non-nil slice when none). With unrestrictedDefault and no team selected it
// returns nil (no team restriction).
func (h *Handler) teamIDs(ctx context.Context, r *http.Request, isAdmin, unrestrictedDefault bool) []uuid.UUID {
	if param := r.URL.Query().Get("team_id"); param != "" {
		if teamID, err := uuid.Parse(param); err == nil && (isAdmin || middleware.IsUserInTeamFromContext(ctx, teamID)) {
			return []uuid.UUID{teamID}
		}
	}
	if unrestrictedDefault {
		return nil
	}
	ids := middleware.GetUserTeamIDsFromContext(ctx)
	if ids == nil {
		ids = []uuid.UUID{} // fail-closed
	}
	return ids
}

// scopeOrError resolves the scope, writing a 403 when the view is not allowed.
func (h *Handler) scopeOrError(w http.ResponseWriter, r *http.Request) (repository.DashboardScope, bool) {
	scope, err := h.resolveScope(r.Context(), r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return scope, false
	}
	return scope, true
}

// StatsResponse is GET /api/dashboard/stats.
type StatsResponse struct {
	Jobs        repository.DashboardJobCounts      `json:"jobs"`
	Agents      AgentCounts                        `json:"agents"`
	Cracks      repository.DashboardCrackCounts    `json:"cracks"`
	HashRate    int64                              `json:"hash_rate"`
	Hashlists   repository.DashboardHashlistTotals `json:"hashlists"`
	GeneratedAt time.Time                          `json:"generated_at"`
}

// AgentCounts summarises agent statuses ("online" = active).
type AgentCounts struct {
	Online   int `json:"online"`
	Total    int `json:"total"`
	Error    int `json:"error"`
	Updating int `json:"updating"`
	Disabled int `json:"disabled"`
	Offline  int `json:"offline"`
}

// GetStats handles GET /api/dashboard/stats.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope, ok := h.scopeOrError(w, r)
	if !ok {
		return
	}

	jobs, err := h.repo.GetJobCounts(ctx, scope)
	if err != nil {
		debug.Error("dashboard stats: %v", err)
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}
	hashlists, err := h.repo.GetHashlistTotals(ctx, scope)
	if err != nil {
		debug.Error("dashboard stats: %v", err)
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}
	cracks, err := h.repo.GetCrackCounts(ctx, scope)
	if err != nil {
		debug.Error("dashboard stats: %v", err)
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}
	rate, err := h.repo.GetAggregateHashRate(ctx, scope)
	if err != nil {
		debug.Error("dashboard stats: %v", err)
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}
	agents, err := h.repo.ListAgentHealth(ctx, scope)
	if err != nil {
		debug.Error("dashboard stats: %v", err)
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}
	var counts AgentCounts
	for _, a := range agents {
		counts.Total++
		switch a.Status {
		case "active":
			counts.Online++
		case "error":
			counts.Error++
		case "updating":
			counts.Updating++
		case "disabled":
			counts.Disabled++
		case "inactive":
			counts.Offline++
		}
	}

	writeJSON(w, StatsResponse{
		Jobs:        jobs,
		Agents:      counts,
		Cracks:      cracks,
		HashRate:    rate,
		Hashlists:   hashlists,
		GeneratedAt: time.Now().UTC(),
	})
}

// GetRecentCracks handles GET /api/dashboard/recent-cracks?limit=.
func (h *Handler) GetRecentCracks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope, ok := h.scopeOrError(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cracks, err := h.repo.ListRecentCracks(ctx, scope, limit)
	if err != nil {
		debug.Error("dashboard recent cracks: %v", err)
		http.Error(w, "Failed to load recent cracks", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"cracks": cracks})
}

// GetAgentHealth handles GET /api/dashboard/agent-health.
func (h *Handler) GetAgentHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope, ok := h.scopeOrError(w, r)
	if !ok {
		return
	}
	agents, err := h.repo.ListAgentHealth(ctx, scope)
	if err != nil {
		debug.Error("dashboard agent health: %v", err)
		http.Error(w, "Failed to load agent health", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"agents": agents})
}

// GetAttention handles GET /api/dashboard/attention.
func (h *Handler) GetAttention(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scopeOrError(w, r)
	if !ok {
		return
	}
	items, err := h.repo.ListAttention(r.Context(), scope)
	if err != nil {
		debug.Error("dashboard attention: %v", err)
		http.Error(w, "Failed to load attention items", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// GetCrackTrend handles GET /api/dashboard/crack-trend?days=.
func (h *Handler) GetCrackTrend(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scopeOrError(w, r)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	trend, err := h.repo.GetCrackTrend(r.Context(), scope, days)
	if err != nil {
		debug.Error("dashboard crack trend: %v", err)
		http.Error(w, "Failed to load crack trend", http.StatusInternalServerError)
		return
	}
	writeJSON(w, trend)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debug.Error("dashboard: encode response: %v", err)
	}
}
