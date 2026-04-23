package admin

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

// CustomCharsetHandler handles admin CRUD for global custom charsets.
type CustomCharsetHandler struct {
	charsetService *services.CustomCharsetService
}

// NewCustomCharsetHandler creates a new admin charset handler.
func NewCustomCharsetHandler(charsetService *services.CustomCharsetService) *CustomCharsetHandler {
	return &CustomCharsetHandler{charsetService: charsetService}
}

// ListGlobalCharsets returns all global charsets.
func (h *CustomCharsetHandler) ListGlobalCharsets(w http.ResponseWriter, r *http.Request) {
	charsets, err := h.charsetService.ListGlobal(r.Context())
	if err != nil {
		debug.Error("Failed to list global charsets: %v", err)
		http.Error(w, "Failed to list charsets", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(charsets)
}

// CreateGlobalCharset creates a new global charset.
func (h *CustomCharsetHandler) CreateGlobalCharset(w http.ResponseWriter, r *http.Request) {
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

	userID, _ := r.Context().Value("user_uuid").(uuid.UUID)

	charset, err := h.charsetService.CreateGlobalCharset(r.Context(), req.Name, req.Description, req.Definition, req.IsHex, userID)
	if err != nil {
		debug.Error("Failed to create global charset: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charset)
}

// UpdateGlobalCharset updates a global charset.
func (h *CustomCharsetHandler) UpdateGlobalCharset(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid charset ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Name       string `json:"name"`
		Description string `json:"description"`
		Definition string `json:"definition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	userID, _ := r.Context().Value("user_uuid").(uuid.UUID)

	charset, err := h.charsetService.UpdateCharset(r.Context(), id, req.Name, req.Description, req.Definition, userID, true)
	if err != nil {
		debug.Error("Failed to update global charset %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(charset)
}

// UploadGlobalCharsetFile handles uploading a .hcchr charset file as a global charset.
func (h *CustomCharsetHandler) UploadGlobalCharsetFile(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value("user_uuid").(uuid.UUID)

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
	charset, err := h.charsetService.CreateGlobalFileCharset(r.Context(), name, description, fileInfo, relPath, userID)
	if err != nil {
		os.Remove(absPath) // Cleanup on DB failure
		debug.Error("Failed to create global file charset: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(charset)
}

// DeleteGlobalCharset deletes a global charset.
func (h *CustomCharsetHandler) DeleteGlobalCharset(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid charset ID", http.StatusBadRequest)
		return
	}

	userID, _ := r.Context().Value("user_uuid").(uuid.UUID)

	if err := h.charsetService.DeleteCharset(r.Context(), id, userID, true); err != nil {
		debug.Error("Failed to delete global charset %s: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
