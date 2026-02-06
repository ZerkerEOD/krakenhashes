package team

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// AdminHandler handles admin team management
type AdminHandler struct {
	teamService *services.TeamService
}

// NewAdminTeamHandler creates a new admin team handler
func NewAdminTeamHandler(teamService *services.TeamService) *AdminHandler {
	return &AdminHandler{
		teamService: teamService,
	}
}

// TeamResponse represents a team in admin responses
type TeamResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MemberCount int    `json:"member_count,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListAllTeams returns all teams (admin only)
// GET /api/admin/teams
func (h *AdminHandler) ListAllTeams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Admin sees all teams - use ListTeams with isAdmin=true
	teams, err := h.teamService.ListTeams(ctx, uuid.Nil, true)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Convert to response format
	response := make([]TeamResponse, len(teams))
	for i, t := range teams {
		response[i] = TeamResponse{
			ID:          t.ID.String(),
			Name:        t.Name,
			Description: t.Description,
			CreatedAt:   t.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}

// CreateTeam creates a new team (admin only)
// POST /api/admin/teams
func (h *AdminHandler) CreateTeam(w http.ResponseWriter, r *http.Request) {
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

	// Get the admin's user ID as creator
	creatorID, _ := middleware.GetUserIDFromContext(ctx)

	team, err := h.teamService.CreateTeam(ctx, req.Name, req.Description, creatorID)
	if err != nil {
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

// UpdateTeam updates a team (admin only)
// PUT /api/admin/teams/{id}
func (h *AdminHandler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
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

	adminUserID, _ := middleware.GetUserIDFromContext(ctx)

	err = h.teamService.UpdateTeam(ctx, teamID, req.Name, req.Description, adminUserID, true)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Team updated successfully"})
}

// DeleteTeam deletes a team (admin only)
// DELETE /api/admin/teams/{id}
func (h *AdminHandler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teamIDStr := mux.Vars(r)["id"]
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid team ID")
		return
	}

	err = h.teamService.DeleteTeam(ctx, teamID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ToggleTeamsEnabled enables or disables multi-team mode
// PUT /api/admin/settings/teams_enabled
func (h *AdminHandler) ToggleTeamsEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Call the OnTeamsEnabledChanged handler which updates the setting and handles migration
	if err := h.teamService.OnTeamsEnabledChanged(ctx, req.Enabled); err != nil {
		log.Printf("Warning: OnTeamsEnabledChanged failed: %v", err)
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Invalidate cache
	h.teamService.InvalidateTeamsEnabledCache()

	respondWithJSON(w, http.StatusOK, map[string]bool{"teams_enabled": req.Enabled})
}

// GetTeamsEnabled returns the current teams_enabled status (admin endpoint)
// GET /api/admin/settings/teams_enabled
func (h *AdminHandler) GetTeamsEnabled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	enabled := h.teamService.IsTeamsEnabled(ctx)

	respondWithJSON(w, http.StatusOK, map[string]bool{"teams_enabled": enabled})
}

// =============================================================================
// Helper Methods
// =============================================================================

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
