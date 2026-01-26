package v1

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// AgentHandler handles User API requests for agent management
type AgentHandler struct {
	agentRepo         *repository.AgentRepository
	voucherService    *services.ClaimVoucherService
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(agentRepo *repository.AgentRepository, voucherService *services.ClaimVoucherService) *AgentHandler {
	return &AgentHandler{
		agentRepo:      agentRepo,
		voucherService: voucherService,
	}
}

// GenerateVoucherRequest represents the request to generate a voucher
type GenerateVoucherRequest struct {
	ExpiresIn    int64 `json:"expires_in"`    // Duration in seconds
	IsContinuous bool  `json:"is_continuous"` // Whether the voucher can be used multiple times
}

// GenerateVoucherResponse represents the response containing a voucher
type GenerateVoucherResponse struct {
	Code         string    `json:"code"`
	IsActive     bool      `json:"is_active"`
	IsContinuous bool      `json:"is_continuous"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// UpdateAgentRequest represents the request to update an agent
type UpdateAgentRequest struct {
	Name            *string `json:"name,omitempty"`
	ExtraParameters *string `json:"extra_parameters,omitempty"`
	IsEnabled       *bool   `json:"is_enabled,omitempty"`
}

// GenerateVoucher generates a registration voucher for the user
// POST /api/v1/agents/vouchers
func (h *AgentHandler) GenerateVoucher(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_uuid").(uuid.UUID)

	var req GenerateVoucherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Default to 7 days if not specified
	if req.ExpiresIn <= 0 {
		req.ExpiresIn = 7 * 24 * 60 * 60 // 7 days in seconds
	}

	// Create the voucher
	voucher, err := h.voucherService.CreateTempVoucher(r.Context(), userID.String(), time.Duration(req.ExpiresIn)*time.Second, req.IsContinuous)
	if err != nil {
		sendAPIError(w, "Failed to generate voucher", "VOUCHER_GENERATION_FAILED", http.StatusInternalServerError)
		return
	}

	// Calculate expiration time
	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		expiry := voucher.CreatedAt.Add(time.Duration(req.ExpiresIn) * time.Second)
		expiresAt = &expiry
	}

	response := GenerateVoucherResponse{
		Code:         voucher.Code,
		IsActive:     voucher.IsActive,
		IsContinuous: voucher.IsContinuous,
		CreatedAt:    voucher.CreatedAt,
		ExpiresAt:    expiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// ListAgents lists all agents owned by the user
// GET /api/v1/agents?page=1&page_size=20&status=active
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_uuid").(uuid.UUID)

	// Parse pagination parameters
	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	// Get agents owned by user
	agents, err := h.agentRepo.GetByOwnerID(r.Context(), userID)
	if err != nil {
		sendAPIError(w, "Failed to retrieve agents", "AGENTS_RETRIEVAL_FAILED", http.StatusInternalServerError)
		return
	}

	// Filter by status if provided
	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" {
		var filtered []models.Agent
		for _, agent := range agents {
			if agent.Status == statusFilter {
				filtered = append(filtered, agent)
			}
		}
		agents = filtered
	}

	// Apply pagination
	total := len(agents)
	start := (page - 1) * pageSize
	end := start + pageSize

	if start >= total {
		agents = []models.Agent{}
	} else {
		if end > total {
			end = total
		}
		agents = agents[start:end]
	}

	response := map[string]interface{}{
		"agents":    agents,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAgent retrieves a specific agent by ID
// GET /api/v1/agents/{id}
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_uuid").(uuid.UUID)
	vars := mux.Vars(r)

	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid agent ID", "INVALID_AGENT_ID", http.StatusBadRequest)
		return
	}

	// Get agent
	agent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify ownership
	if agent.OwnerID == nil || *agent.OwnerID != userID {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent)
}

// UpdateAgent updates an agent's settings
// PATCH /api/v1/agents/{id}
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_uuid").(uuid.UUID)
	vars := mux.Vars(r)

	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid agent ID", "INVALID_AGENT_ID", http.StatusBadRequest)
		return
	}

	// Get agent to verify ownership
	agent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify ownership
	if agent.OwnerID == nil || *agent.OwnerID != userID {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Parse update request
	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Update fields if provided
	if req.Name != nil {
		agent.Name = *req.Name
	}
	if req.ExtraParameters != nil {
		agent.ExtraParameters = *req.ExtraParameters
	}
	if req.IsEnabled != nil {
		agent.IsEnabled = *req.IsEnabled
	}

	// Update agent
	if err := h.agentRepo.Update(r.Context(), agent); err != nil {
		sendAPIError(w, "Failed to update agent", "AGENT_UPDATE_FAILED", http.StatusInternalServerError)
		return
	}

	// Fetch updated agent to return
	updatedAgent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		sendAPIError(w, "Failed to retrieve updated agent", "AGENT_RETRIEVAL_FAILED", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedAgent)
}

// DeleteAgent removes an agent (disables it)
// DELETE /api/v1/agents/{id}
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_uuid").(uuid.UUID)
	vars := mux.Vars(r)

	agentID, err := strconv.Atoi(vars["id"])
	if err != nil {
		sendAPIError(w, "Invalid agent ID", "INVALID_AGENT_ID", http.StatusBadRequest)
		return
	}

	// Get agent to verify ownership
	agent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify ownership
	if agent.OwnerID == nil || *agent.OwnerID != userID {
		sendAPIError(w, "Agent not found", "AGENT_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Disable the agent instead of deleting (agents cannot be fully deleted due to foreign key constraints)
	agent.IsEnabled = false
	agent.Status = models.AgentStatusDisabled

	if err := h.agentRepo.Update(r.Context(), agent); err != nil {
		sendAPIError(w, "Failed to delete agent", "AGENT_DELETE_FAILED", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
