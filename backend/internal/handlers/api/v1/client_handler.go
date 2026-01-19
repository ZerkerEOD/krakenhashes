package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ClientHandler handles User API v1 client-related requests
type ClientHandler struct {
	clientRepo         *repository.ClientRepository
	hashlistRepo       *repository.HashListRepository
	clientSettingsRepo *repository.ClientSettingsRepository
	db                 *db.DB
}

// NewClientHandler creates a new client handler instance
func NewClientHandler(clientRepo *repository.ClientRepository, hashlistRepo *repository.HashListRepository, clientSettingsRepo *repository.ClientSettingsRepository, database *db.DB) *ClientHandler {
	return &ClientHandler{
		clientRepo:         clientRepo,
		hashlistRepo:       hashlistRepo,
		clientSettingsRepo: clientSettingsRepo,
		db:                 database,
	}
}

// ClientResponse represents the response for client operations
type ClientResponse struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Description         *string   `json:"description,omitempty"`
	Domain              *string   `json:"domain,omitempty"`
	DataRetentionMonths *int      `json:"data_retention_months,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ClientListResponse represents the paginated list response
type ClientListResponse struct {
	Clients  []ClientResponse `json:"clients"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// CreateClientRequest represents the request body for creating a client
type CreateClientRequest struct {
	Name                string  `json:"name"`
	Description         *string `json:"description,omitempty"`
	Domain              *string `json:"domain,omitempty"`
	DataRetentionMonths *int    `json:"data_retention_months,omitempty"`
}

// UpdateClientRequest represents the request body for updating a client
type UpdateClientRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Domain      *string `json:"domain,omitempty"`
}

// sendAPIError sends a standardized API error response
func sendAPIError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Code:    code,
		Message: message,
	})
}

// getUserID extracts the user UUID from the request context
func getUserID(r *http.Request) (uuid.UUID, error) {
	userID, ok := r.Context().Value("user_uuid").(uuid.UUID)
	if !ok {
		return uuid.Nil, fmt.Errorf("user_uuid not found in context")
	}
	return userID, nil
}

// hasHashlists checks if a client has any associated hashlists
func (h *ClientHandler) hasHashlists(ctx context.Context, clientID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM hashlists WHERE client_id = $1)`
	var exists bool
	err := h.db.QueryRowContext(ctx, query, clientID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check hashlists: %w", err)
	}
	return exists, nil
}

// toClientResponse converts a models.Client to ClientResponse
func toClientResponse(client *models.Client) ClientResponse {
	// Use ContactInfo as Domain for API response
	return ClientResponse{
		ID:                  client.ID,
		Name:                client.Name,
		Description:         client.Description,
		Domain:              client.ContactInfo, // Map ContactInfo to Domain
		DataRetentionMonths: client.DataRetentionMonths,
		CreatedAt:           client.CreatedAt,
		UpdatedAt:           client.UpdatedAt,
	}
}

// CreateClient handles POST /api/v1/clients
func (h *ClientHandler) CreateClient(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserID(r)
	if err != nil {
		debug.Error("Failed to get user ID: %v", err)
		sendAPIError(w, "Authentication required", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}

	var req CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIError(w, "Invalid request payload", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		sendAPIError(w, "Client name is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Determine data retention months
	var retentionMonths *int
	if req.DataRetentionMonths != nil {
		// User explicitly provided a value - use it (0 = keep forever, >0 = months)
		retentionMonths = req.DataRetentionMonths
	} else {
		// User omitted the field - fetch and apply default from client_settings
		defaultSetting, settingErr := h.clientSettingsRepo.GetSetting(r.Context(), "default_data_retention_months")
		if settingErr == nil && defaultSetting.Value != nil {
			if val, parseErr := strconv.Atoi(*defaultSetting.Value); parseErr == nil {
				retentionMonths = &val
			}
		}
	}

	// Create client
	client := &models.Client{
		ID:                  uuid.New(),
		Name:                req.Name,
		Description:         req.Description,
		ContactInfo:         req.Domain, // Map Domain to ContactInfo
		DataRetentionMonths: retentionMonths,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	err = h.clientRepo.Create(r.Context(), client)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateRecord) {
			sendAPIError(w, fmt.Sprintf("Client with name '%s' already exists", req.Name), "VALIDATION_ERROR", http.StatusConflict)
		} else {
			debug.Error("Failed to create client: %v", err)
			sendAPIError(w, "Failed to create client", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	debug.Info("User %s created new client with ID: %s", userID.String(), client.ID.String())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toClientResponse(client))
}

// ListClients handles GET /api/v1/clients
func (h *ClientHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	// Note: Authentication is handled by middleware, clients are global resources

	// Parse pagination parameters
	page := 1
	pageSize := 25

	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr := r.URL.Query().Get("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// Get all clients (clients are global resources)
	allClients, err := h.clientRepo.List(r.Context())
	if err != nil {
		debug.Error("Failed to list clients: %v", err)
		sendAPIError(w, "Failed to retrieve clients", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	// Apply pagination in handler
	total := len(allClients)
	offset := (page - 1) * pageSize
	end := offset + pageSize
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	clients := allClients[offset:end]

	// Convert to response format
	clientResponses := make([]ClientResponse, len(clients))
	for i, client := range clients {
		clientResponses[i] = toClientResponse(&client)
	}

	response := ClientListResponse{
		Clients:  clientResponses,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetClient handles GET /api/v1/clients/{id}
func (h *ClientHandler) GetClient(w http.ResponseWriter, r *http.Request) {
	// Note: Authentication is handled by middleware, clients are global resources
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid client ID format", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Get client (clients are global resources - no ownership check needed)
	client, err := h.clientRepo.GetByID(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			sendAPIError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		} else {
			debug.Error("Failed to get client: %v", err)
			sendAPIError(w, "Failed to retrieve client", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(toClientResponse(client))
}

// UpdateClient handles PATCH /api/v1/clients/{id}
func (h *ClientHandler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	// Note: Authentication is handled by middleware, clients are global resources
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid client ID format", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	var req UpdateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIError(w, "Invalid request payload", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Get existing client
	client, err := h.clientRepo.GetByID(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			sendAPIError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		} else {
			debug.Error("Failed to get client: %v", err)
			sendAPIError(w, "Failed to update client", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	// Apply updates
	if req.Name != nil {
		if *req.Name == "" {
			sendAPIError(w, "Client name cannot be empty", "VALIDATION_ERROR", http.StatusBadRequest)
			return
		}
		client.Name = *req.Name
	}
	if req.Description != nil {
		client.Description = req.Description
	}
	if req.Domain != nil {
		client.ContactInfo = req.Domain // Map Domain to ContactInfo
	}

	// Update client
	err = h.clientRepo.Update(r.Context(), client)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateRecord) {
			sendAPIError(w, fmt.Sprintf("Client with name '%s' already exists", client.Name), "VALIDATION_ERROR", http.StatusConflict)
		} else if errors.Is(err, repository.ErrNotFound) {
			sendAPIError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		} else {
			debug.Error("Failed to update client: %v", err)
			sendAPIError(w, "Failed to update client", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	debug.Info("Client updated with ID: %s", client.ID.String())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(toClientResponse(client))
}

// DeleteClient handles DELETE /api/v1/clients/{id}
func (h *ClientHandler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	// Note: Authentication is handled by middleware, clients are global resources
	vars := mux.Vars(r)
	clientID, err := uuid.Parse(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid client ID format", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Check if client has hashlists (prevent deletion if hashlists exist)
	hasHashlists, err := h.hasHashlists(r.Context(), clientID)
	if err != nil {
		debug.Error("Failed to check hashlists: %v", err)
		sendAPIError(w, "Failed to delete client", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	if hasHashlists {
		sendAPIError(w, "Cannot delete client with associated hashlists", "CLIENT_HAS_HASHLISTS", http.StatusConflict)
		return
	}

	// Delete client
	err = h.clientRepo.Delete(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			sendAPIError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		} else {
			debug.Error("Failed to delete client: %v", err)
			sendAPIError(w, "Failed to delete client", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	debug.Info("Client deleted: %s", clientID.String())

	w.WriteHeader(http.StatusNoContent)
}
