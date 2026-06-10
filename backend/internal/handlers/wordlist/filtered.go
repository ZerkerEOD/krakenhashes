package wordlist

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// userIDFromContext extracts the authenticated user's UUID from the request context.
func userIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok || userIDStr == "" {
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return uuid.Nil, false
	}
	return userID, true
}

// HandlePreviewFilter estimates how many candidates a filter would keep by
// sampling the start of the parent wordlist. Used by the UI live preview.
func (h *Handler) HandlePreviewFilter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req models.FilterPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ParentWordlistID == 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "parent_wordlist_id is required")
		return
	}
	if err := req.Filter.Validate(); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Sample the first million lines for a fast estimate.
	resp, err := h.manager.PreviewFilter(ctx, req.ParentWordlistID, req.Filter, 1000000)
	if err != nil {
		debug.Error("Failed to preview filter: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to preview filter: "+err.Error())
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, resp)
}

// HandleCreateFilteredWordlist creates a permanent filtered wordlist and kicks
// off background generation. Returns 202 with the pending wordlist row.
func (h *Handler) HandleCreateFilteredWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := userIDFromContext(ctx)
	if !ok {
		httputil.RespondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	var req models.CreateFilteredWordlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ParentWordlistID == 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "parent_wordlist_id is required")
		return
	}
	if err := req.Filter.Validate(); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Permanent filtered wordlist (not ephemeral, no owner job).
	wl, err := h.manager.CreateFilteredWordlistRecord(ctx, req.ParentWordlistID, req.Name, req.Description, req.Filter, false, nil, userID)
	if err != nil {
		debug.Error("Failed to create filtered wordlist: %v", err)
		httputil.RespondWithError(w, http.StatusBadRequest, "Failed to create filtered wordlist: "+err.Error())
		return
	}

	// Generate in the background so the request returns immediately; the row's
	// verification_status transitions pending -> verified/failed.
	go func(id int) {
		if err := h.manager.GenerateFilteredWordlist(context.Background(), id); err != nil {
			debug.Error("Background generation of filtered wordlist %d failed: %v", id, err)
		}
	}(wl.ID)

	httputil.RespondWithJSON(w, http.StatusAccepted, wl)
}

// HandleRegenerateFilteredWordlist runs a manual FULL regeneration of a filtered
// wordlist. Automatic regeneration on parent change is incremental; this manual
// action forces a full rebuild (the escape hatch for non-append parent edits or a
// failed prior regeneration). Returns 202.
func (h *Handler) HandleRegenerateFilteredWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	wl, err := h.manager.GetWordlist(ctx, id)
	if err != nil {
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get wordlist")
		return
	}
	if wl == nil {
		httputil.RespondWithError(w, http.StatusNotFound, "Wordlist not found")
		return
	}
	if wl.ParentWordlistID == nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Wordlist is not a filtered wordlist")
		return
	}

	go func(id int) {
		if err := h.manager.RegenerateFilteredWordlistFull(context.Background(), id); err != nil {
			debug.Error("Background full regeneration of filtered wordlist %d failed: %v", id, err)
		}
	}(id)

	httputil.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "regenerating"})
}

// HandleListWordlistsForAgent lists wordlists for agents, including ephemeral
// (job-scoped) filtered wordlists so agents can sync them for their tasks and
// don't treat them as orphans during reconciliation (GH #40).
func (h *Handler) HandleListWordlistsForAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filters := make(map[string]interface{})
	if wordlistType := r.URL.Query().Get("type"); wordlistType != "" {
		filters["wordlist_type"] = wordlistType
	}
	if format := r.URL.Query().Get("format"); format != "" {
		filters["format"] = format
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tag"] = tag
	}
	filters["include_ephemeral"] = true

	wordlists, err := h.manager.ListWordlists(ctx, filters)
	if err != nil {
		debug.Error("Failed to list wordlists for agent: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list wordlists")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, wordlists)
}

// HandleListFilteredChildren lists the filtered wordlists derived from a parent.
func (h *Handler) HandleListFilteredChildren(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	children, err := h.manager.GetFilteredChildren(ctx, id)
	if err != nil {
		debug.Error("Failed to list filtered children for wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list filtered wordlists")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, children)
}
