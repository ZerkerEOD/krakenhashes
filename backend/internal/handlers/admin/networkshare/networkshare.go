// Package networkshare exposes the admin API for the network-share storage
// configuration (feature/network-share-storage, WS3).
//
// Routes live under /api/admin/settings/network-share and are registered
// BEFORE the generic /settings/{key} catch-all (see routes/admin.go), so the
// key-value settings handler never shadows them.
//
// These are typed endpoints rather than generic settings keys because the
// config drives where every wordlist and rule is read from and, later, a
// data-migration that takes the server offline — far too consequential for the
// unvalidated generic {key} writer.
package networkshare

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Handler serves the network-share admin API.
type Handler struct {
	svc    *services.NetworkShareService
	engine *services.MigrationEngine
}

// NewHandler creates the handler.
func NewHandler(svc *services.NetworkShareService, engine *services.MigrationEngine) *Handler {
	return &Handler{svc: svc, engine: engine}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		if err := json.NewEncoder(w).Encode(body); err != nil {
			debug.Warning("network share handler: encode response: %v", err)
		}
	}
}

// GetConfig returns the current share configuration plus a live health flag.
// GET /api/admin/settings/network-share
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.Get(r.Context())
	if err != nil {
		debug.Error("network share: get config: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load network share config"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config": cfg,
		"health": h.svc.Health(r.Context()),
		// share_dir is the server's compose-mounted path (KH_SHARE_DIR) — shown
		// read-only so the panel reflects what the server actually reads, not the
		// agent-command coordinate fields.
		"share_dir": h.svc.ShareDir(),
	})
}

// UpdateConfig upserts the non-secret config fields.
// PUT /api/admin/settings/network-share
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var in services.NetworkShareConfigInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	cfg, err := h.svc.UpsertConfig(r.Context(), in)
	if err != nil {
		debug.Warning("network share: update config: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"config": cfg})
}

// Validate exercises the server-side mount and reports the result.
// POST /api/admin/settings/network-share/validate
func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Validate(r.Context())
	if err != nil {
		debug.Error("network share: validate: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "validation failed to run"})
		return
	}
	// A failed validation is a 200 with ok=false + error detail: the request
	// itself succeeded, the mount just isn't ready. The UI reads res.ok.
	writeJSON(w, http.StatusOK, res)
}

// StartMigration launches an in-process maintenance migration in the requested
// direction ("to_share" or "to_local").
// POST /api/admin/settings/network-share/migrate  {"direction": "..."}
func (h *Handler) StartMigration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Direction string `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if err := h.engine.Start(r.Context(), body.Direction); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, h.engine.Status())
}

// MigrationStatus returns the live migration progress snapshot for the
// maintenance UI / landing page to poll.
// GET /api/admin/settings/network-share/migration
func (h *Handler) MigrationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.engine.Status())
}

// CancelMigration aborts an in-progress migration (e.g. one wedged on a slow or
// hung share). The engine unwinds at its next checkpoint, leaving storage on
// its original backend and resuming normal operation. Returns 200 with the
// post-cancel status snapshot when a run was aborted, 409 when nothing was
// running.
// DELETE /api/admin/settings/network-share/migration
func (h *Handler) CancelMigration(w http.ResponseWriter, r *http.Request) {
	if !h.engine.Cancel() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no migration is in progress"})
		return
	}
	writeJSON(w, http.StatusOK, h.engine.Status())
}
