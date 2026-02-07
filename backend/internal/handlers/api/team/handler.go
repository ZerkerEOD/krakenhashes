package team

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Handler handles team-related HTTP requests
type Handler struct {
	teamService *services.TeamService
}

// NewTeamHandler creates a new team handler
func NewTeamHandler(teamService *services.TeamService) *Handler {
	return &Handler{
		teamService: teamService,
	}
}

// =============================================================================
// Response/Request Types
// =============================================================================

type TeamResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	UserRole      string `json:"user_role,omitempty"`
	MemberCount   int    `json:"member_count"`
	ClientCount   int    `json:"client_count"`
	HashlistCount int    `json:"hashlist_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type TeamMemberResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"` // "member" or "admin"
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role"`
}

type UserSearchResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// =============================================================================
// Team Endpoints
// =============================================================================

// CreateTeam creates a new team (any authenticated user)
// POST /api/teams
func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		respondWithError(w, http.StatusBadRequest, "Team name is required")
		return
	}

	if len(req.Name) > 100 {
		respondWithError(w, http.StatusBadRequest, "Team name must be 100 characters or less")
		return
	}

	creatorID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	team, err := h.teamService.CreateTeam(ctx, req.Name, req.Description, creatorID)
	if err != nil {
		log.Printf("Failed to create team: %v", err)
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := TeamResponse{
		ID:          team.ID.String(),
		Name:        team.Name,
		Description: team.Description,
		CreatedAt:   team.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   team.UpdatedAt.Format(time.RFC3339),
	}

	respondWithJSON(w, http.StatusCreated, response)
}

// UpdateTeam updates a team (team admins or system admins)
// PUT /api/teams/{id}
func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		respondWithError(w, http.StatusBadRequest, "Team name is required")
		return
	}

	if len(req.Name) > 100 {
		respondWithError(w, http.StatusBadRequest, "Team name must be 100 characters or less")
		return
	}

	userID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.UpdateTeam(ctx, teamID, req.Name, req.Description, userID, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Team updated successfully"})
}

// ListUserTeams returns teams the current user belongs to
// GET /api/teams
func (h *Handler) ListUserTeams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := middleware.GetUserIDFromContext(ctx)
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	teams, err := h.teamService.GetUserTeams(ctx, userID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Convert to response format
	response := make([]TeamResponse, len(teams))
	for i, t := range teams {
		response[i] = TeamResponse{
			ID:            t.ID.String(),
			Name:          t.Name,
			Description:   t.Description,
			UserRole:      t.UserRole,
			MemberCount:   t.MemberCount,
			ClientCount:   t.ClientCount,
			HashlistCount: t.HashlistCount,
			CreatedAt:     t.CreatedAt.Format(time.RFC3339),
			UpdatedAt:     t.UpdatedAt.Format(time.RFC3339),
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetTeam returns a specific team
// GET /api/teams/{id}
func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	// Get team details directly
	team, err := h.teamService.GetTeam(ctx, teamID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Team not found")
		return
	}

	// Check if user is a member (to get their role)
	userID, ok := middleware.GetUserIDFromContext(ctx)
	var userRole string
	if ok {
		isMember, _ := h.teamService.IsUserInTeam(ctx, userID, teamID)
		if isMember {
			isAdmin, _ := h.teamService.IsUserTeamAdmin(ctx, userID, teamID)
			if isAdmin {
				userRole = models.TeamRoleAdmin
			} else {
				userRole = models.TeamRoleMember
			}
		}
	}

	// Get member count
	members, _ := h.teamService.GetTeamMembers(ctx, teamID)

	response := TeamResponse{
		ID:          team.ID.String(),
		Name:        team.Name,
		Description: team.Description,
		UserRole:    userRole,
		MemberCount: len(members),
		CreatedAt:   team.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   team.UpdatedAt.Format(time.RFC3339),
	}

	respondWithJSON(w, http.StatusOK, response)
}

// ListMembers returns all members of a team
// GET /api/teams/{id}/members
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	members, err := h.teamService.GetTeamMembers(ctx, teamID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := make([]TeamMemberResponse, len(members))
	for i, m := range members {
		response[i] = TeamMemberResponse{
			UserID:   m.UserID.String(),
			Username: m.Username,
			Email:    m.Email,
			Role:     m.Role,
			JoinedAt: m.JoinedAt.Format(time.RFC3339),
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

// ListTeamClients returns all clients assigned to a team
// GET /api/teams/{id}/clients
func (h *Handler) ListTeamClients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	clients, err := h.teamService.GetClientsForTeam(ctx, teamID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, clients)
}

// =============================================================================
// Team Manager Operations (require team admin role)
// =============================================================================

// AddMember adds a user to a team
// POST /api/teams/{id}/members
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	targetUserID, err := uuid.Parse(req.UserID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Default to member role
	role := req.Role
	if role == "" {
		role = models.TeamRoleMember
	}

	// Validate role
	if role != models.TeamRoleMember && role != models.TeamRoleAdmin {
		respondWithError(w, http.StatusBadRequest, "Role must be 'member' or 'admin'")
		return
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.AddUserToTeam(ctx, teamID, targetUserID, actingUserID, role, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, map[string]string{"message": "User added to team"})
}

// RemoveMember removes a user from a team
// DELETE /api/teams/{id}/members/{userId}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	teamIDStr := vars["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	targetUserIDStr := vars["userId"]
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.RemoveUserFromTeam(ctx, teamID, targetUserID, actingUserID, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateMemberRole updates a user's role in a team
// PUT /api/teams/{id}/members/{userId}
func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	teamIDStr := vars["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	targetUserIDStr := vars["userId"]
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate role
	if req.Role != models.TeamRoleMember && req.Role != models.TeamRoleAdmin {
		respondWithError(w, http.StatusBadRequest, "Role must be 'member' or 'admin'")
		return
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.UpdateUserTeamRole(ctx, teamID, targetUserID, actingUserID, req.Role, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Role updated"})
}

// AssignClient assigns a client to a team
// POST /api/teams/{id}/clients/{clientId}
func (h *Handler) AssignClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	teamIDStr := vars["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	clientIDStr := vars["clientId"]
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid client ID")
		return
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.AssignClientToTeam(ctx, clientID, teamID, actingUserID, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondWithJSON(w, http.StatusCreated, map[string]string{"message": "Client assigned to team"})
}

// RemoveClient removes a client from a team
// DELETE /api/teams/{id}/clients/{clientId}
func (h *Handler) RemoveClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	teamIDStr := vars["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	clientIDStr := vars["clientId"]
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid client ID")
		return
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)
	isSystemAdmin := middleware.IsAdminFromContext(ctx)

	err = h.teamService.RemoveClientFromTeam(ctx, clientID, teamID, actingUserID, isSystemAdmin)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// User Search Endpoint
// =============================================================================

// SearchUsers searches for users to add to a team
// GET /api/users/search?q={query}&team_id={teamID}&limit={limit}&offset={offset}
func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := r.URL.Query().Get("q")
	if len(query) < 2 {
		respondWithError(w, http.StatusBadRequest, "Search query must be at least 2 characters")
		return
	}

	teamIDStr := r.URL.Query().Get("team_id")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Team ID is required")
		return
	}

	// Parse pagination params with safe defaults
	limit := 50 // Default and max limit to prevent unbounded results
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 50 {
		limit = 50 // Cap at 50 to prevent unbounded results
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	users, err := h.teamService.SearchUsersForTeam(ctx, teamID, query, limit, offset)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := make([]UserSearchResponse, len(users))
	for i, u := range users {
		response[i] = UserSearchResponse{
			ID:       u.ID.String(),
			Username: u.Username,
			Email:    u.Email,
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetTeamsEnabled returns the current teams_enabled status (non-admin endpoint)
// GET /api/settings/teams_enabled
func (h *Handler) GetTeamsEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	enabled := h.teamService.IsTeamsEnabled(ctx)

	respondWithJSON(w, http.StatusOK, map[string]bool{"teams_enabled": enabled})
}

// =============================================================================
// Helper Methods
// =============================================================================

// respondWithJSON writes a JSON response with the given status code
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

// respondWithError writes a JSON error response with the given status code
func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
