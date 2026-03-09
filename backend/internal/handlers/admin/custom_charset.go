package admin

import (
	"encoding/json"
	"net/http"

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
		Name       string `json:"name"`
		Description string `json:"description"`
		Definition string `json:"definition"`
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

	charset, err := h.charsetService.CreateGlobalCharset(r.Context(), req.Name, req.Description, req.Definition, userID)
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
