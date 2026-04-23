package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// CustomCharsetHandler handles user-facing charset CRUD.
type CustomCharsetHandler struct {
	charsetService *services.CustomCharsetService
}

// NewCustomCharsetHandler creates a new user charset handler.
func NewCustomCharsetHandler(charsetService *services.CustomCharsetService) *CustomCharsetHandler {
	return &CustomCharsetHandler{charsetService: charsetService}
}

// ListAccessibleCharsets returns all charsets accessible to the current user.
func (h *CustomCharsetHandler) ListAccessibleCharsets(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	// TODO: When teams feature is enabled, pass team IDs from context
	var teamIDs []uuid.UUID

	charsets, err := h.charsetService.ListAccessible(r.Context(), userID, teamIDs)
	if err != nil {
		debug.Error("Failed to list accessible charsets: %v", err)
		http.Error(w, "Failed to list charsets", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(charsets)
}

// CreateUserCharset creates a personal charset for the current user.
func (h *CustomCharsetHandler) CreateUserCharset(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Definition  string `json:"definition"`
		IsHex       bool   `json:"is_hex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Definition == "" {
		http.Error(w, "Name and definition are required", http.StatusBadRequest)
		return
	}

	charset, err := h.charsetService.CreateUserCharset(r.Context(), userID, req.Name, req.Description, req.Definition, req.IsHex)
	if err != nil {
		debug.Error("Failed to create user charset: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charset)
}

// UpdateOwnCharset updates a charset owned by the current user.
func (h *CustomCharsetHandler) UpdateOwnCharset(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid charset ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Definition  string `json:"definition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	isAdmin, _ := r.Context().Value("user_role").(string)

	charset, err := h.charsetService.UpdateCharset(r.Context(), id, req.Name, req.Description, req.Definition, userID, isAdmin == "admin")
	if err != nil {
		debug.Error("Failed to update charset %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(charset)
}

// UploadUserCharsetFile handles uploading a .hcchr charset file as a personal charset.
func (h *CustomCharsetHandler) UploadUserCharsetFile(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	// Parse multipart form (limit to 2MB — charset files are max 1024 bytes)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	if ext := strings.ToLower(filepath.Ext(header.Filename)); ext != ".hcchr" {
		http.Error(w, "Only .hcchr files are accepted", http.StatusBadRequest)
		return
	}

	// Save file to disk
	relPath, absPath, err := h.charsetService.SaveUploadedCharsetFile(file)
	if err != nil {
		debug.Error("Failed to save charset file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Validate the file content
	fileInfo, err := h.charsetService.ValidateHCCHRFile(absPath)
	if err != nil {
		os.Remove(absPath) // Cleanup on validation failure
		debug.Error("Charset file validation failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create DB record
	charset, err := h.charsetService.CreateUserFileCharset(r.Context(), userID, name, description, fileInfo, relPath)
	if err != nil {
		os.Remove(absPath) // Cleanup on DB failure
		debug.Error("Failed to create user file charset: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charset)
}

// DeleteOwnCharset deletes a charset owned by the current user.
func (h *CustomCharsetHandler) DeleteOwnCharset(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid charset ID", http.StatusBadRequest)
		return
	}

	isAdmin, _ := r.Context().Value("user_role").(string)

	if err := h.charsetService.DeleteCharset(r.Context(), id, userID, isAdmin == "admin"); err != nil {
		debug.Error("Failed to delete charset %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
