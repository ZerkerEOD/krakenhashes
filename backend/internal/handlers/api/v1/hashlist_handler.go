package v1

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/processor"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HashlistHandler handles User API v1 hashlist operations
type HashlistHandler struct {
	hashlistRepo *repository.HashListRepository
	clientRepo   *repository.ClientRepository
	hashTypeRepo *repository.HashTypeRepository
	processor    *processor.HashlistDBProcessor
	dataDir      string
}

// NewHashlistHandler creates a new hashlist handler for User API v1
func NewHashlistHandler(
	hashlistRepo *repository.HashListRepository,
	clientRepo *repository.ClientRepository,
	hashTypeRepo *repository.HashTypeRepository,
	processor *processor.HashlistDBProcessor,
	dataDir string,
) *HashlistHandler {
	return &HashlistHandler{
		hashlistRepo: hashlistRepo,
		clientRepo:   clientRepo,
		hashTypeRepo: hashTypeRepo,
		processor:    processor,
		dataDir:      dataDir,
	}
}

// CreateHashlist handles POST /api/v1/hashlists
func (h *HashlistHandler) CreateHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context (set by UserAPIKeyMiddleware)
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		sendError(w, "Unauthorized", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}

	// Parse multipart form (100MB max)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		debug.Error("Failed to parse multipart form: %v", err)
		sendError(w, "Failed to parse form data", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Extract form fields
	name := r.FormValue("name")
	clientIDStr := r.FormValue("client_id")
	hashTypeIDStr := r.FormValue("hash_type_id")
	description := r.FormValue("description")
	_ = description // suppress unused warning for now

	// Validate required fields
	if name == "" {
		sendError(w, "Name is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	if clientIDStr == "" {
		sendError(w, "Client ID is required", "CLIENT_REQUIRED", http.StatusBadRequest)
		return
	}

	if hashTypeIDStr == "" {
		sendError(w, "Hash type ID is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Parse and validate client_id (UUID format)
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		sendError(w, "Invalid client_id format (must be UUID)", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Parse and validate hash_type_id
	hashTypeID, err := strconv.Atoi(hashTypeIDStr)
	if err != nil {
		sendError(w, "Invalid hash_type_id format", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Verify hash type exists and is enabled
	hashType, err := h.hashTypeRepo.GetByID(ctx, hashTypeID)
	if err != nil || hashType == nil || !hashType.IsEnabled {
		debug.Error("Invalid or disabled hash type ID %d: %v", hashTypeID, err)
		sendError(w, "Invalid or disabled hash type", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Verify client exists
	client, err := h.clientRepo.GetByID(ctx, clientID)
	if err != nil {
		debug.Error("Failed to get client %s: %v", clientID.String(), err)
		sendError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		return
	}

	if client == nil {
		sendError(w, "Client not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Note: Clients are global (no user ownership), all authenticated users can use any client

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		debug.Error("Failed to get file from form: %v", err)
		sendError(w, "File is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Create hashlist database entry
	now := time.Now()
	hashlist := &models.HashList{
		Name:               name,
		UserID:             userID,
		ClientID:           client.ID,
		HashTypeID:         hashTypeID,
		Status:             models.HashListStatusUploading,
		ExcludeFromPotfile: false, // default value
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := h.hashlistRepo.Create(ctx, hashlist); err != nil {
		debug.Error("Failed to create hashlist: %v", err)
		sendError(w, "Failed to create hashlist", "HASHLIST_UPLOAD_FAILED", http.StatusBadRequest)
		return
	}

	// Ensure hashlist.ID is populated
	if hashlist.ID == 0 {
		debug.Error("Hashlist ID is 0 after creation")
		sendError(w, "Failed to retrieve hashlist ID", "HASHLIST_UPLOAD_FAILED", http.StatusBadRequest)
		return
	}

	// Save uploaded file
	filename := fmt.Sprintf("%d_%s%s",
		hashlist.ID,
		sanitizeFilename(strings.ReplaceAll(strings.ToLower(name), " ", "_")),
		filepath.Ext(header.Filename),
	)
	hashlistPath := filepath.Join(h.dataDir, filename)

	dst, err := os.Create(hashlistPath)
	if err != nil {
		debug.Error("Failed to create file %s: %v", hashlistPath, err)
		h.hashlistRepo.UpdateStatus(ctx, hashlist.ID, models.HashListStatusError, "Failed to save file")
		sendError(w, "Failed to save uploaded file", "HASHLIST_UPLOAD_FAILED", http.StatusBadRequest)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		debug.Error("Failed to copy file data: %v", err)
		h.hashlistRepo.UpdateStatus(ctx, hashlist.ID, models.HashListStatusError, "Failed to save file")
		os.Remove(hashlistPath)
		sendError(w, "Failed to save uploaded file", "HASHLIST_UPLOAD_FAILED", http.StatusBadRequest)
		return
	}

	// Update status to processing
	if err := h.hashlistRepo.UpdateStatus(ctx, hashlist.ID, models.HashListStatusProcessing, ""); err != nil {
		debug.Error("Failed to update hashlist status: %v", err)
		os.Remove(hashlistPath)
		sendError(w, "Failed to process hashlist", "HASHLIST_UPLOAD_FAILED", http.StatusBadRequest)
		return
	}

	// Start background processing
	go h.processor.SubmitHashlistForProcessing(hashlist.ID, hashlistPath)

	debug.Info("Hashlist %d uploaded successfully by user %s", hashlist.ID, userID.String())

	// Prepare response
	response := map[string]interface{}{
		"id":            hashlist.ID,
		"name":          hashlist.Name,
		"hash_count":    0, // Will be updated during processing
		"cracked_count": 0,
		"hash_type":     hashType.Name,
		"client_id":     clientID.String(),
		"created_at":    hashlist.CreatedAt,
	}

	httputil.RespondWithJSON(w, http.StatusCreated, response)
}

// ListHashlists handles GET /api/v1/hashlists
func (h *HashlistHandler) ListHashlists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		sendError(w, "Unauthorized", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	page := httputil.GetIntQueryParam(r, "page", 1)
	pageSize := httputil.GetIntQueryParam(r, "page_size", 25)
	clientIDStr := httputil.GetQueryParam(r, "client_id")
	search := httputil.GetQueryParam(r, "search")

	// Calculate offset
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	// Build filter parameters
	params := repository.ListHashlistsParams{
		Limit:  pageSize,
		Offset: offset,
		UserID: &userID, // Filter by user
	}

	// Add client filter if provided
	if clientIDStr != "" {
		clientID, err := uuid.Parse(clientIDStr)
		if err == nil {
			params.ClientID = &clientID
		} else {
			debug.Warning("Invalid client_id filter format: %s", clientIDStr)
		}
	}

	// Add search filter if provided
	if search != "" {
		params.NameLike = &search
	}

	// Fetch hashlists
	hashlists, total, err := h.hashlistRepo.List(ctx, params)
	if err != nil {
		debug.Error("Failed to list hashlists for user %s: %v", userID.String(), err)
		sendError(w, "Failed to retrieve hashlists", "RESOURCE_NOT_FOUND", http.StatusInternalServerError)
		return
	}

	// Prepare response
	response := map[string]interface{}{
		"hashlists": hashlists,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// GetHashlist handles GET /api/v1/hashlists/{id}
func (h *HashlistHandler) GetHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		sendError(w, "Unauthorized", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}

	// Get hashlist ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		sendError(w, "Invalid hashlist ID", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Get hashlist
	hashlist, err := h.hashlistRepo.GetByID(ctx, id)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", id, err)
		sendError(w, "Hashlist not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify ownership
	if hashlist.UserID != userID {
		debug.Warning("User %s attempted to access hashlist %d owned by %s",
			userID.String(), id, hashlist.UserID.String())
		sendError(w, "Access denied", "RESOURCE_ACCESS_DENIED", http.StatusForbidden)
		return
	}

	// Calculate progress
	progress := 0.0
	if hashlist.TotalHashes > 0 {
		progress = float64(hashlist.CrackedHashes) / float64(hashlist.TotalHashes) * 100.0
	}

	// Get hash type name
	hashType, err := h.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
	hashTypeName := "Unknown"
	if err == nil && hashType != nil {
		hashTypeName = hashType.Name
	}

	// Prepare response
	response := map[string]interface{}{
		"id":            hashlist.ID,
		"name":          hashlist.Name,
		"hash_count":    hashlist.TotalHashes,
		"cracked_count": hashlist.CrackedHashes,
		"progress":      progress,
		"hash_type":     hashTypeName,
		"hash_type_id":  hashlist.HashTypeID,
		"client_id":     hashlist.ClientID,
		"status":        hashlist.Status,
		"created_at":    hashlist.CreatedAt,
		"updated_at":    hashlist.UpdatedAt,
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// DeleteHashlist handles DELETE /api/v1/hashlists/{id}
func (h *HashlistHandler) DeleteHashlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		sendError(w, "Unauthorized", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}

	// Get hashlist ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		sendError(w, "Invalid hashlist ID", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Get hashlist to verify ownership and check for active jobs
	hashlist, err := h.hashlistRepo.GetByID(ctx, id)
	if err != nil {
		debug.Error("Failed to get hashlist %d: %v", id, err)
		sendError(w, "Hashlist not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify ownership
	if hashlist.UserID != userID {
		debug.Warning("User %s attempted to delete hashlist %d owned by %s",
			userID.String(), id, hashlist.UserID.String())
		sendError(w, "Access denied", "RESOURCE_ACCESS_DENIED", http.StatusForbidden)
		return
	}

	// TODO: Check for active jobs using this hashlist
	// For now, we'll allow deletion (the repository may have FK constraints)

	// Delete hashlist (associations handled by CASCADE)
	if err := h.hashlistRepo.Delete(ctx, id); err != nil {
		// Check if it's a foreign key constraint error (active jobs)
		if strings.Contains(err.Error(), "foreign key") || strings.Contains(err.Error(), "referenced") {
			sendError(w, "Cannot delete hashlist with active jobs", "HASHLIST_HAS_ACTIVE_JOBS", http.StatusConflict)
			return
		}
		debug.Error("Failed to delete hashlist %d: %v", id, err)
		sendError(w, "Failed to delete hashlist", "RESOURCE_NOT_FOUND", http.StatusInternalServerError)
		return
	}

	debug.Info("Hashlist %d deleted by user %s", id, userID.String())

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// Helper functions

// getUserIDFromContext extracts user UUID from context
func getUserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value("user_uuid").(uuid.UUID)
	if !ok {
		return uuid.Nil, fmt.Errorf("user_uuid not found in context")
	}
	return userID, nil
}

// sendError sends a standardized error response
func sendError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf(`{"error":"%s","code":"%s"}`, message, code)))
}

// sanitizeFilename removes problematic characters from filenames
func sanitizeFilename(filename string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(filename)
}
