package pot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type Handler struct {
	hashRepo     *repository.HashRepository
	hashlistRepo *repository.HashListRepository
	clientRepo   *repository.ClientRepository
}

func NewHandler(
	hashRepo *repository.HashRepository,
	hashlistRepo *repository.HashListRepository,
	clientRepo *repository.ClientRepository,
) *Handler {
	return &Handler{
		hashRepo:     hashRepo,
		hashlistRepo: hashlistRepo,
		clientRepo:   clientRepo,
	}
}

type CrackedHashResponse struct {
	ID           uuid.UUID `json:"id"`
	OriginalHash string    `json:"original_hash"`
	Password     string    `json:"password"`
	HashTypeID   int       `json:"hash_type_id"`
	Username     *string   `json:"username,omitempty"`
}

type PotResponse struct {
	Hashes     []CrackedHashResponse `json:"hashes"`
	TotalCount int64                 `json:"total_count"`
	Limit      int                   `json:"limit"`
	Offset     int                   `json:"offset"`
}

func (h *Handler) HandleListCrackedHashes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	params := repository.CrackedHashParams{
		Limit:  500,
		Offset: 0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}
	
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			params.Offset = offset
		}
	}

	debug.Info("Fetching cracked hashes with limit=%d, offset=%d", params.Limit, params.Offset)

	hashes, totalCount, err := h.hashRepo.GetCrackedHashes(ctx, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}

	response := h.formatPotResponse(hashes, totalCount, params.Limit, params.Offset)
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) HandleListCrackedHashesByHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	hashlistIDStr := vars["id"]
	
	hashlistID, err := strconv.ParseInt(hashlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid hashlist ID: %v", err)
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}

	params := repository.CrackedHashParams{
		Limit:  500,
		Offset: 0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}
	
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			params.Offset = offset
		}
	}

	debug.Info("Fetching cracked hashes for hashlist=%d with limit=%d, offset=%d", hashlistID, params.Limit, params.Offset)

	hashes, totalCount, err := h.hashRepo.GetCrackedHashesByHashlist(ctx, hashlistID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes by hashlist: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}

	response := h.formatPotResponse(hashes, totalCount, params.Limit, params.Offset)
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) HandleListCrackedHashesByClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	clientIDStr := vars["id"]
	
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		debug.Error("Invalid client ID: %v", err)
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}

	params := repository.CrackedHashParams{
		Limit:  500,
		Offset: 0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}
	
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			params.Offset = offset
		}
	}

	debug.Info("Fetching cracked hashes for client=%s with limit=%d, offset=%d", clientID, params.Limit, params.Offset)

	hashes, totalCount, err := h.hashRepo.GetCrackedHashesByClient(ctx, clientID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes by client: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}

	response := h.formatPotResponse(hashes, totalCount, params.Limit, params.Offset)
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debug.Error("Failed to encode response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) formatPotResponse(hashes []*models.Hash, totalCount int64, limit, offset int) PotResponse {
	crackedHashes := make([]CrackedHashResponse, 0, len(hashes))
	
	for _, hash := range hashes {
		displayHash := hash.HashValue
		if hash.OriginalHash != "" {
			displayHash = hash.OriginalHash
		}
		
		if hash.HashTypeID == 1000 && hash.OriginalHash != "" {
			displayHash = hash.OriginalHash
		}
		
		crackedHashes = append(crackedHashes, CrackedHashResponse{
			ID:           hash.ID,
			OriginalHash: displayHash,
			Password:     hash.Password,
			HashTypeID:   hash.HashTypeID,
			Username:     hash.Username,
		})
	}
	
	return PotResponse{
		Hashes:     crackedHashes,
		TotalCount: totalCount,
		Limit:      limit,
		Offset:     offset,
	}
}

// Download handlers for all cracked hashes

func (h *Handler) HandleDownloadHashPass(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Get all cracked hashes (no pagination)
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashes(ctx, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeHashPassFormat(w, hashes, "master")
}

func (h *Handler) HandleDownloadUserPass(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashes(ctx, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserPassFormat(w, hashes, "master")
}

func (h *Handler) HandleDownloadUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashes(ctx, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserFormat(w, hashes, "master")
}

func (h *Handler) HandleDownloadPass(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashes(ctx, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writePassFormat(w, hashes, "master")
}

// Download handlers for hashlist-specific cracked hashes

func (h *Handler) HandleDownloadHashPassByHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	hashlistIDStr := vars["id"]
	
	hashlistID, err := strconv.ParseInt(hashlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid hashlist ID: %v", err)
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}
	
	// Get hashlist name for filename
	hashlist, err := h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist: %v", err)
		http.Error(w, "Failed to retrieve hashlist", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByHashlist(ctx, hashlistID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeHashPassFormat(w, hashes, sanitizeFilename(hashlist.Name))
}

func (h *Handler) HandleDownloadUserPassByHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	hashlistIDStr := vars["id"]
	
	hashlistID, err := strconv.ParseInt(hashlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid hashlist ID: %v", err)
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}
	
	hashlist, err := h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist: %v", err)
		http.Error(w, "Failed to retrieve hashlist", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByHashlist(ctx, hashlistID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserPassFormat(w, hashes, sanitizeFilename(hashlist.Name))
}

func (h *Handler) HandleDownloadUserByHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	hashlistIDStr := vars["id"]
	
	hashlistID, err := strconv.ParseInt(hashlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid hashlist ID: %v", err)
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}
	
	hashlist, err := h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist: %v", err)
		http.Error(w, "Failed to retrieve hashlist", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByHashlist(ctx, hashlistID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserFormat(w, hashes, sanitizeFilename(hashlist.Name))
}

func (h *Handler) HandleDownloadPassByHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	hashlistIDStr := vars["id"]
	
	hashlistID, err := strconv.ParseInt(hashlistIDStr, 10, 64)
	if err != nil {
		debug.Error("Invalid hashlist ID: %v", err)
		http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
		return
	}
	
	hashlist, err := h.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Error("Failed to get hashlist: %v", err)
		http.Error(w, "Failed to retrieve hashlist", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByHashlist(ctx, hashlistID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writePassFormat(w, hashes, sanitizeFilename(hashlist.Name))
}

// Download handlers for client-specific cracked hashes

func (h *Handler) HandleDownloadHashPassByClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	clientIDStr := vars["id"]
	
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		debug.Error("Invalid client ID: %v", err)
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}
	
	// Get client name for filename
	client, err := h.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		debug.Error("Failed to get client: %v", err)
		http.Error(w, "Failed to retrieve client", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByClient(ctx, clientID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeHashPassFormat(w, hashes, sanitizeFilename(client.Name))
}

func (h *Handler) HandleDownloadUserPassByClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	clientIDStr := vars["id"]
	
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		debug.Error("Invalid client ID: %v", err)
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}
	
	client, err := h.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		debug.Error("Failed to get client: %v", err)
		http.Error(w, "Failed to retrieve client", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByClient(ctx, clientID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserPassFormat(w, hashes, sanitizeFilename(client.Name))
}

func (h *Handler) HandleDownloadUserByClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	clientIDStr := vars["id"]
	
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		debug.Error("Invalid client ID: %v", err)
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}
	
	client, err := h.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		debug.Error("Failed to get client: %v", err)
		http.Error(w, "Failed to retrieve client", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByClient(ctx, clientID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writeUserFormat(w, hashes, sanitizeFilename(client.Name))
}

func (h *Handler) HandleDownloadPassByClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	clientIDStr := vars["id"]
	
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		debug.Error("Invalid client ID: %v", err)
		http.Error(w, "Invalid client ID", http.StatusBadRequest)
		return
	}
	
	client, err := h.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		debug.Error("Failed to get client: %v", err)
		http.Error(w, "Failed to retrieve client", http.StatusInternalServerError)
		return
	}
	
	params := repository.CrackedHashParams{
		Limit:  999999,
		Offset: 0,
	}
	
	hashes, _, err := h.hashRepo.GetCrackedHashesByClient(ctx, clientID, params)
	if err != nil {
		debug.Error("Failed to get cracked hashes for download: %v", err)
		http.Error(w, "Failed to retrieve cracked hashes", http.StatusInternalServerError)
		return
	}
	
	h.writePassFormat(w, hashes, sanitizeFilename(client.Name))
}

// Helper functions for writing different formats

func (h *Handler) writeHashPassFormat(w http.ResponseWriter, hashes []*models.Hash, context string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-h-p.lst\"", context))
	
	for _, hash := range hashes {
		displayHash := hash.HashValue
		if hash.OriginalHash != "" {
			displayHash = hash.OriginalHash
		}
		if hash.HashTypeID == 1000 && hash.OriginalHash != "" {
			displayHash = hash.OriginalHash
		}
		
		fmt.Fprintf(w, "%s:%s\n", displayHash, hash.Password)
	}
}

func (h *Handler) writeUserPassFormat(w http.ResponseWriter, hashes []*models.Hash, context string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-u-p.lst\"", context))
	
	for _, hash := range hashes {
		if hash.Username != nil && *hash.Username != "" {
			fmt.Fprintf(w, "%s:%s\n", *hash.Username, hash.Password)
		}
	}
}

func (h *Handler) writeUserFormat(w http.ResponseWriter, hashes []*models.Hash, context string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-u.lst\"", context))
	
	for _, hash := range hashes {
		if hash.Username != nil && *hash.Username != "" {
			fmt.Fprintf(w, "%s\n", *hash.Username)
		}
	}
}

func (h *Handler) writePassFormat(w http.ResponseWriter, hashes []*models.Hash, context string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-p.lst\"", context))
	
	for _, hash := range hashes {
		fmt.Fprintf(w, "%s\n", hash.Password)
	}
}

// sanitizeFilename removes or replaces characters that are problematic in filenames
func sanitizeFilename(name string) string {
	// Replace spaces with underscores
	name = strings.ReplaceAll(name, " ", "_")
	// Remove or replace other problematic characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
		"\n", "-",
		"\r", "-",
		"\t", "-",
	)
	return replacer.Replace(name)
}