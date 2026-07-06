package routes

import (
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	adminteam "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/team"
	teamapi "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/api/team"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupTeamRoutes registers team routes on the JWT-protected router
func SetupTeamRoutes(jwtRouter *mux.Router, teamService *services.TeamService, database *db.DB) {
	debug.Info("Setting up team routes")

	// Create handler
	handler := teamapi.NewTeamHandler(teamService)

	// Literal path routes MUST be registered before parameterized routes
	// (gorilla/mux is first-match-wins, so /teams/{id} would capture "names")
	jwtRouter.HandleFunc("/teams/names", handler.ListAllTeamNames).Methods(http.MethodGet, http.MethodOptions)

	// Team routes (authenticated users)
	jwtRouter.HandleFunc("/teams", handler.ListUserTeams).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/teams", handler.CreateTeam).Methods(http.MethodPost, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}", handler.GetTeam).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}", handler.UpdateTeam).Methods(http.MethodPut, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/members", handler.ListMembers).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/clients", handler.ListTeamClients).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/agents", handler.ListTeamAgents).Methods(http.MethodGet, http.MethodOptions)

	// Team manager operations (require team admin role - enforced by service layer)
	jwtRouter.HandleFunc("/teams/{id}/members", handler.AddMember).Methods(http.MethodPost, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/members/{userId}", handler.RemoveMember).Methods(http.MethodDelete, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/members/{userId}", handler.UpdateMemberRole).Methods(http.MethodPut, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/clients/{clientId}", handler.AssignClient).Methods(http.MethodPost, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/clients/{clientId}", handler.RemoveClient).Methods(http.MethodDelete, http.MethodOptions)

	// Team trust management (team admins manage own team's trust; system admins manage any)
	jwtRouter.HandleFunc("/teams/{id}/trust", handler.ListTrustedTeams).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/trust/{trustedTeamId}", handler.AddTrust).Methods(http.MethodPost, http.MethodOptions)
	jwtRouter.HandleFunc("/teams/{id}/trust/{trustedTeamId}", handler.RemoveTrust).Methods(http.MethodDelete, http.MethodOptions)

	// User search for adding members
	jwtRouter.HandleFunc("/users/search", handler.SearchUsers).Methods(http.MethodGet, http.MethodOptions)

	// Non-admin teams_enabled setting endpoint
	jwtRouter.HandleFunc("/settings/teams_enabled", handler.GetTeamsEnabled).Methods(http.MethodGet, http.MethodOptions)

	debug.Info("Team routes configured: /api/teams/*, /api/users/search, /api/settings/teams_enabled")
}

// SetupAdminTeamRoutes registers admin team routes on the admin router
func SetupAdminTeamRoutes(adminRouter *mux.Router, teamService *services.TeamService) {
	debug.Info("Setting up admin team routes")

	handler := adminteam.NewAdminTeamHandler(teamService)

	// Admin team CRUD operations
	adminRouter.HandleFunc("/teams", handler.ListAllTeams).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/teams", handler.CreateTeam).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/teams/{id}", handler.UpdateTeam).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/teams/{id}", handler.DeleteTeam).Methods(http.MethodDelete, http.MethodOptions)

	// Admin settings for teams_enabled toggle
	adminRouter.HandleFunc("/settings/teams_enabled", handler.GetTeamsEnabled).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/teams_enabled", handler.ToggleTeamsEnabled).Methods(http.MethodPut, http.MethodOptions)

	debug.Info("Admin team routes configured: /api/admin/teams/*, /api/admin/settings/teams_enabled")
}
