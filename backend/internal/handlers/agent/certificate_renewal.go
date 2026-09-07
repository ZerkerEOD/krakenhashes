package agent

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// CertificateRenewalHandler handles agent certificate renewal requests
type CertificateRenewalHandler struct {
	tlsProvider tls.Provider
	agentRepo   *repository.AgentRepository
}

// NewCertificateRenewalHandler creates a new certificate renewal handler
func NewCertificateRenewalHandler(tlsProvider tls.Provider, agentRepo *repository.AgentRepository) *CertificateRenewalHandler {
	return &CertificateRenewalHandler{
		tlsProvider: tlsProvider,
		agentRepo:   agentRepo,
	}
}

// CertificateRenewalResponse contains the renewed certificates
type CertificateRenewalResponse struct {
	ClientCertificate string `json:"client_certificate"`
	ClientKey         string `json:"client_key"`
}

// HandleCertificateRenewal handles certificate renewal requests from agents
func (h *CertificateRenewalHandler) HandleCertificateRenewal(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received certificate renewal request")

	// Get API key and agent ID from headers
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		debug.Error("Missing API key in certificate renewal request")
		http.Error(w, "Missing API key", http.StatusUnauthorized)
		return
	}

	agentIDStr := r.Header.Get("X-Agent-ID")
	if agentIDStr == "" {
		debug.Error("Missing agent ID in certificate renewal request")
		http.Error(w, "Missing agent ID", http.StatusUnauthorized)
		return
	}

	// Convert agent ID to int
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		debug.Error("Invalid agent ID format: %v", err)
		http.Error(w, "Invalid agent ID", http.StatusBadRequest)
		return
	}

	// Get agent by ID.
	//
	// Both failure paths below return an identical 401. Returning 404 for an
	// unknown agent and 401 for a bad key made this an agent-ID oracle for an
	// unauthenticated caller, and this endpoint is served over plain HTTP.
	agent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		debug.Debug("Certificate renewal for unknown agent %d", agentID)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Validate API key in constant time. A plain != leaks key material through
	// timing to anyone who can reach this port.
	if subtle.ConstantTimeCompare([]byte(agent.APIKey.String), []byte(apiKey)) != 1 {
		debug.Warning("Invalid API key presented for agent %d", agentID)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	debug.Info("Certificate renewal requested by agent %d", agent.ID)

	// Get client certificate and key
	certPEM, keyPEM, err := h.tlsProvider.GetClientCertificate()
	if err != nil {
		debug.Error("Failed to get client certificate: %v", err)
		http.Error(w, "Failed to generate client certificate", http.StatusInternalServerError)
		return
	}

	// Prepare response
	response := CertificateRenewalResponse{
		ClientCertificate: string(certPEM),
		ClientKey:         string(keyPEM),
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode certificate renewal response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully renewed certificates for agent %d", agent.ID)
}