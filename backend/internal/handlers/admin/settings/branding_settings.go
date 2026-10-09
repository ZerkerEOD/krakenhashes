package settings

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/public"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/branding"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/gorilla/mux"
)

// BrandingSettingsHandler serves the admin branding API (GitHub issue #41).
type BrandingSettingsHandler struct {
	svc *branding.Service
}

// NewBrandingSettingsHandler creates the handler.
func NewBrandingSettingsHandler(svc *branding.Service) *BrandingSettingsHandler {
	return &BrandingSettingsHandler{svc: svc}
}

// RegisterRoutes mounts the API under /admin/settings/branding. It MUST be
// called before the generic /settings/{key} routes are registered, because
// gorilla/mux matches in registration order.
func (h *BrandingSettingsHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/settings/branding", h.Get).Methods(http.MethodGet, http.MethodOptions)
	r.HandleFunc("/settings/branding", h.Update).Methods(http.MethodPut, http.MethodOptions)
	r.HandleFunc("/settings/branding/logo", h.uploadAsset(branding.Logo)).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/settings/branding/logo", h.removeAsset(branding.Logo)).Methods(http.MethodDelete, http.MethodOptions)
	r.HandleFunc("/settings/branding/favicon", h.uploadAsset(branding.Favicon)).Methods(http.MethodPost, http.MethodOptions)
	r.HandleFunc("/settings/branding/favicon", h.removeAsset(branding.Favicon)).Methods(http.MethodDelete, http.MethodOptions)
}

// AdminBranding is the wire shape returned by every admin branding call.
type AdminBranding struct {
	Settings   branding.Settings     `json:"settings"`
	HasLogo    bool                  `json:"has_logo"`
	HasFavicon bool                  `json:"has_favicon"`
	Effective  public.PublicBranding `json:"effective"`
}

func (h *BrandingSettingsHandler) respond(w http.ResponseWriter, r *http.Request) {
	raw, eff, err := h.svc.RawSettings(r.Context())
	if err != nil {
		debug.Error("branding: failed to read settings: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to read branding settings")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, AdminBranding{
		Settings:   raw,
		HasLogo:    eff.LogoFile != "",
		HasFavicon: eff.FaviconFile != "",
		Effective:  public.ToPublic(eff),
	})
}

// Get returns the stored values plus the effective branding.
func (h *BrandingSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r)
}

// Update validates and stores the text settings.
func (h *BrandingSettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var in branding.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if _, err := h.svc.Update(r.Context(), in); err != nil {
		if branding.IsValidation(err) {
			httputil.RespondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		debug.Error("branding: failed to update settings: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to save branding settings")
		return
	}
	debug.Info("Branding settings updated")
	h.respond(w, r)
}

func (h *BrandingSettingsHandler) uploadAsset(kind branding.AssetKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := int64(kind.MaxBytes())
		// Multipart framing adds a little overhead on top of the file itself.
		r.Body = http.MaxBytesReader(w, r.Body, limit+64<<10)
		if err := r.ParseMultipartForm(limit); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				httputil.RespondWithError(w, http.StatusRequestEntityTooLarge, "Upload exceeds the size limit")
			} else {
				httputil.RespondWithError(w, http.StatusBadRequest, "Invalid multipart upload")
			}
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			httputil.RespondWithError(w, http.StatusBadRequest, "Missing form field \"file\"")
			return
		}
		defer file.Close()

		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil {
			httputil.RespondWithError(w, http.StatusBadRequest, "Failed to read upload")
			return
		}
		if int64(len(data)) > limit {
			httputil.RespondWithError(w, http.StatusRequestEntityTooLarge, "Upload exceeds the size limit")
			return
		}

		if _, err := h.svc.SaveAsset(r.Context(), kind, data); err != nil {
			switch {
			case errors.Is(err, branding.ErrImageTooLarge):
				httputil.RespondWithError(w, http.StatusRequestEntityTooLarge, err.Error())
			case errors.Is(err, branding.ErrUnsupportedImage):
				httputil.RespondWithError(w, http.StatusBadRequest, err.Error())
			default:
				debug.Error("branding: failed to save %s: %v", kind, err)
				httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to store the uploaded image")
			}
			return
		}
		debug.Info("Branding %s uploaded (%d bytes)", kind, len(data))
		h.respond(w, r)
	}
}

func (h *BrandingSettingsHandler) removeAsset(kind branding.AssetKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := h.svc.RemoveAsset(r.Context(), kind); err != nil {
			debug.Error("branding: failed to remove %s: %v", kind, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to remove the image")
			return
		}
		debug.Info("Branding %s removed", kind)
		h.respond(w, r)
	}
}
