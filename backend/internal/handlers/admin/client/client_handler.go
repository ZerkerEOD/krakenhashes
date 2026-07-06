package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	clientsvc "github.com/ZerkerEOD/krakenhashes/backend/internal/services/client"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ClientHandler handles API requests for admin client management.
type ClientHandler struct {
	clientRepo     *repository.ClientRepository
	clientSvc      *clientsvc.ClientService
	teamService    *services.TeamService
	clientTeamRepo *repository.ClientTeamRepository
}

// NewClientHandler creates a new handler instance.
func NewClientHandler(cr *repository.ClientRepository, cs *clientsvc.ClientService, ts *services.TeamService, ctr *repository.ClientTeamRepository) *ClientHandler {
	return &ClientHandler{
		clientRepo:     cr,
		clientSvc:      cs,
		teamService:    ts,
		clientTeamRepo: ctr,
	}
}

// ListClients godoc
// @Summary List all clients
// @Description Retrieves a list of all clients in the system with their cracked hash counts.
// @Tags Admin Clients
// @Produce json
// @Success 200 {object} httputil.SuccessResponse{data=[]models.Client}
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/clients [get]
// @Security ApiKeyAuth
func (h *ClientHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var clients []models.Client
	var err error

	// Filter by team access when teams enabled and user is not admin
	if h.teamService != nil && h.teamService.IsTeamsEnabled(ctx) && !middleware.IsAdminFromContext(ctx) {
		teamIDs := middleware.GetUserTeamIDsFromContext(ctx)
		if teamIDs != nil && len(teamIDs) > 0 {
			clients, err = h.clientRepo.ListWithCrackedCountsByTeamIDs(ctx, teamIDs)
		} else {
			// Fail closed: no teams or middleware error = empty result
			clients = []models.Client{}
		}
	} else {
		clients, err = h.clientRepo.ListWithCrackedCounts(ctx)
	}

	if err != nil {
		debug.Error("Failed to list clients with cracked counts: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve clients")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"data": clients})
}

// CreateClient godoc
// @Summary Create a new client
// @Description Adds a new client to the system.
// @Tags Admin Clients
// @Accept json
// @Produce json
// @Param client body models.Client true "Client object to create (ID, CreatedAt, UpdatedAt ignored)"
// @Success 201 {object} httputil.SuccessResponse{data=models.Client}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 409 {object} httputil.ErrorResponse // Duplicate name
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/clients [post]
// @Security ApiKeyAuth
func (h *ClientHandler) CreateClient(w http.ResponseWriter, r *http.Request) {
	// Parse raw JSON to extract team_id alongside client fields
	var rawBody json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	var newClient models.Client
	if err := json.Unmarshal(rawBody, &newClient); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Extract team_id from the same body
	var teamPayload struct {
		TeamID *string `json:"team_id"`
	}
	json.Unmarshal(rawBody, &teamPayload) // ignore error, team_id is optional

	// Basic validation
	if newClient.Name == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Client name is required")
		return
	}
	// Ensure retention is non-negative if provided
	if newClient.DataRetentionMonths != nil && *newClient.DataRetentionMonths < 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "Data retention must be non-negative")
		return
	}

	// Check if teams are enabled and validate team_id
	ctx := r.Context()
	teamsEnabled := h.teamService != nil && h.teamService.IsTeamsEnabled(ctx)
	var teamID uuid.UUID
	if teamsEnabled {
		if teamPayload.TeamID == nil || *teamPayload.TeamID == "" {
			httputil.RespondWithError(w, http.StatusBadRequest, "Team selection is required when teams are enabled")
			return
		}
		var parseErr error
		teamID, parseErr = uuid.Parse(*teamPayload.TeamID)
		if parseErr != nil {
			httputil.RespondWithError(w, http.StatusBadRequest, "Invalid team ID")
			return
		}

		// Get the acting user ID from context
		userIDStr, _ := ctx.Value("user_id").(string)
		if userIDStr == "" {
			httputil.RespondWithError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		actingUserID, parseErr := uuid.Parse(userIDStr)
		if parseErr != nil {
			httputil.RespondWithError(w, http.StatusUnauthorized, "Invalid user ID")
			return
		}

		// Admins can assign to any team; non-admins must be a member
		isAdmin := middleware.IsAdminFromContext(ctx)
		if !isAdmin {
			isMember, memberErr := h.teamService.IsUserInTeam(ctx, actingUserID, teamID)
			if memberErr != nil {
				debug.Error("Failed to check team membership: %v", memberErr)
				httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to verify team membership")
				return
			}
			if !isMember {
				httputil.RespondWithError(w, http.StatusForbidden, "You are not a member of the selected team")
				return
			}
		}
	}

	// Set server-side fields
	newClient.ID = uuid.New() // Generate new ID
	// CreatedAt/UpdatedAt set by repository

	err := h.clientRepo.Create(r.Context(), &newClient)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateRecord) {
			httputil.RespondWithError(w, http.StatusConflict, fmt.Sprintf("Client with name '%s' already exists", newClient.Name))
		} else {
			debug.Error("Failed to create client: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to create client")
		}
		return
	}

	// Auto-assign to team when teams are enabled
	if teamsEnabled && h.clientTeamRepo != nil {
		userIDStr, _ := ctx.Value("user_id").(string)
		assignedBy, _ := uuid.Parse(userIDStr)
		if assignErr := h.clientTeamRepo.AssignClientToTeam(ctx, newClient.ID, teamID, &assignedBy); assignErr != nil {
			debug.Error("Failed to assign new client %s to team %s: %v", newClient.ID, teamID, assignErr)
			// Client was created but team assignment failed — don't fail the whole request
		}
	}

	debug.Info("Created new client with ID: %s", newClient.ID)
	httputil.RespondWithJSON(w, http.StatusCreated, map[string]interface{}{"data": newClient})
}

// GetClient godoc
// @Summary Get a single client
// @Description Retrieves details for a specific client by ID.
// @Tags Admin Clients
// @Produce json
// @Param id path string true "Client ID (UUID)"
// @Success 200 {object} httputil.SuccessResponse{data=models.Client}
// @Failure 400 {object} httputil.ErrorResponse // Invalid ID format
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/clients/{id} [get]
// @Security ApiKeyAuth
func (h *ClientHandler) GetClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid client ID format")
		return
	}

	client, err := h.clientRepo.GetByID(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Client not found")
		} else {
			debug.Error("Failed to get client %s: %v", clientID, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve client")
		}
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"data": client})
}

// UpdateClient godoc
// @Summary Update an existing client
// @Description Modifies details of an existing client.
// @Tags Admin Clients
// @Accept json
// @Produce json
// @Param id path string true "Client ID (UUID)"
// @Param client body models.Client true "Client object with updated fields (ID, CreatedAt ignored)"
// @Success 200 {object} httputil.SuccessResponse{data=models.Client}
// @Failure 400 {object} httputil.ErrorResponse // Invalid ID or payload
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 409 {object} httputil.ErrorResponse // Duplicate name
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/clients/{id} [put]
// @Security ApiKeyAuth
func (h *ClientHandler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid client ID format")
		return
	}

	var updates models.Client
	if err := httputil.ParseJSONBody(r, &updates); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Basic validation
	if updates.Name == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Client name cannot be empty")
		return
	}
	// Ensure retention is non-negative if provided
	if updates.DataRetentionMonths != nil && *updates.DataRetentionMonths < 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "Data retention must be non-negative")
		return
	}

	// Get existing client to preserve fields not being updated
	// Note: Repo Update only changes specified fields in its query, but returning the full updated object is good practice.
	// We need the full object anyway to return it.
	client, err := h.clientRepo.GetByID(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Client not found")
		} else {
			debug.Error("Failed to get client %s for update: %v", clientID, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve client before update")
		}
		return
	}

	// Apply updates
	client.Name = updates.Name
	client.Description = updates.Description
	client.ContactInfo = updates.ContactInfo
	client.DataRetentionMonths = updates.DataRetentionMonths // Will be handled correctly by repo (sets NULL if pointer is nil)
	client.ExcludeFromPotfile = updates.ExcludeFromPotfile
	// Client potfile settings
	client.ExcludeFromClientPotfile = updates.ExcludeFromClientPotfile
	client.RemoveFromGlobalPotfileOnHashlistDelete = updates.RemoveFromGlobalPotfileOnHashlistDelete
	client.RemoveFromClientPotfileOnHashlistDelete = updates.RemoveFromClientPotfileOnHashlistDelete
	// UpdatedAt will be set by repository

	err = h.clientRepo.Update(r.Context(), client)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateRecord) {
			httputil.RespondWithError(w, http.StatusConflict, fmt.Sprintf("Client with name '%s' already exists", client.Name))
		} else if errors.Is(err, repository.ErrNotFound) { // Should not happen if GetByID succeeded, but check anyway
			httputil.RespondWithError(w, http.StatusNotFound, "Client not found for update")
		} else {
			debug.Error("Failed to update client %s: %v", clientID, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update client")
		}
		return
	}

	// Fetch the updated client again to get the latest UpdatedAt timestamp
	updatedClient, _ := h.clientRepo.GetByID(r.Context(), clientID)

	debug.Info("Admin updated client with ID: %s", client.ID)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"data": updatedClient})
}

// DeleteClient godoc
// @Summary Delete a client
// @Description Removes a client and handles associated hashlists based on retention policy.
// @Tags Admin Clients
// @Produce json
// @Param id path string true "Client ID (UUID)"
// @Success 200 {object} httputil.SuccessResponse
// @Failure 400 {object} httputil.ErrorResponse // Invalid ID format
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/clients/{id} [delete]
// @Security ApiKeyAuth
func (h *ClientHandler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid client ID format")
		return
	}

	// Use the ClientService for complex deletion logic
	err = h.clientSvc.DeleteClient(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Client not found")
		} else {
			// Service layer logs details
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete client")
		}
		return
	}

	debug.Info("Admin deleted client: %s", clientID)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Client deleted successfully"})
}

// BulkAssignTeam assigns multiple clients to a single team.
// POST /api/clients/bulk-assign-team
func (h *ClientHandler) BulkAssignTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !middleware.IsAdminFromContext(ctx) {
		httputil.RespondWithError(w, http.StatusForbidden, "Only administrators can bulk assign clients")
		return
	}

	var req struct {
		ClientIDs []string `json:"client_ids"`
		TeamID    string   `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.ClientIDs) == 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "At least one client ID is required")
		return
	}
	if req.TeamID == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Team ID is required")
		return
	}

	teamID, err := uuid.Parse(req.TeamID)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid team ID format")
		return
	}

	clientIDs := make([]uuid.UUID, 0, len(req.ClientIDs))
	for _, idStr := range req.ClientIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			httputil.RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid client ID format: %s", idStr))
			return
		}
		clientIDs = append(clientIDs, id)
	}

	actingUserID, _ := middleware.GetUserIDFromContext(ctx)

	assigned, alreadyAssigned, err := h.teamService.BulkAssignClientsToTeam(ctx, clientIDs, teamID, actingUserID, true)
	if err != nil {
		debug.Error("Bulk assign clients to team failed: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to assign clients to team")
		return
	}

	debug.Info("Admin bulk assigned %d clients to team %s (%d new, %d already assigned)", len(clientIDs), teamID, assigned, alreadyAssigned)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]int{
		"assigned":         assigned,
		"already_assigned": alreadyAssigned,
		"total":            len(clientIDs),
	})
}
