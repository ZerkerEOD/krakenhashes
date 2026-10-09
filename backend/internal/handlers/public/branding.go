package public

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/branding"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// BrandingHandler serves the unauthenticated branding endpoints the frontend
// needs before login: the effective branding JSON and the inline logo/favicon.
type BrandingHandler struct {
	svc *branding.Service
}

// NewBrandingHandler creates the handler. svc may be nil (stock branding).
func NewBrandingHandler(svc *branding.Service) *BrandingHandler {
	return &BrandingHandler{svc: svc}
}

// PublicBranding is the wire shape of GET /api/branding.
type PublicBranding struct {
	AppName        string  `json:"app_name"`
	PageTitle      string  `json:"page_title"`
	PoweredBy      string  `json:"powered_by"`
	Branded        bool    `json:"branded"`
	PrimaryColor   string  `json:"primary_color"`
	SecondaryColor *string `json:"secondary_color"`
	LogoURL        *string `json:"logo_url"`
	FaviconURL     *string `json:"favicon_url"`
	Version        int64   `json:"version"`
}

// ToPublic converts resolved branding to the wire shape. Asset URLs carry the
// version as a cache-busting query so browsers refetch after an upload.
func ToPublic(b branding.Branding) PublicBranding {
	out := PublicBranding{
		AppName:      b.AppName,
		PageTitle:    b.PageTitle,
		PoweredBy:    b.PoweredBy,
		Branded:      b.Branded,
		PrimaryColor: b.PrimaryColor,
		Version:      b.Version,
	}
	if b.SecondaryColor != "" {
		c := b.SecondaryColor
		out.SecondaryColor = &c
	}
	if b.LogoFile != "" {
		u := fmt.Sprintf("/api/branding/logo?v=%d", b.Version)
		out.LogoURL = &u
	}
	if b.FaviconFile != "" {
		u := fmt.Sprintf("/api/branding/favicon?v=%d", b.Version)
		out.FaviconURL = &u
	}
	return out
}

// Get returns the effective branding. It always answers 200: on a store error
// the stock defaults are returned so the UI can render.
func (h *BrandingHandler) Get(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.Resolve(r.Context())
	if err != nil {
		debug.Warning("branding: serving defaults after resolve error: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	if err := json.NewEncoder(w).Encode(ToPublic(b)); err != nil {
		debug.Error("branding: failed to encode response: %v", err)
	}
}

// Logo serves the uploaded logo inline, or 404 when none is configured.
func (h *BrandingHandler) Logo(w http.ResponseWriter, r *http.Request) {
	h.serveAsset(w, r, branding.Logo)
}

// Favicon serves the uploaded favicon inline, or 404 when none is configured.
func (h *BrandingHandler) Favicon(w http.ResponseWriter, r *http.Request) {
	h.serveAsset(w, r, branding.Favicon)
}

func (h *BrandingHandler) serveAsset(w http.ResponseWriter, r *http.Request, kind branding.AssetKind) {
	b, err := h.svc.Resolve(r.Context())
	if err != nil {
		http.Error(w, "Branding unavailable", http.StatusServiceUnavailable)
		return
	}
	name := b.LogoFile
	if kind == branding.Favicon {
		name = b.FaviconFile
	}
	abs, contentType, err := h.svc.AssetPath(kind, name)
	if err != nil {
		if !errors.Is(err, branding.ErrNoAsset) {
			debug.Warning("branding: refusing to serve %s %q: %v", kind, name, err)
		}
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		debug.Warning("branding: configured %s missing on disk: %v", kind, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+name+`"`)
	// ServeFile honours If-None-Match against the ETag set above (304) and
	// never lists directories because abs is a confined, validated file path.
	http.ServeFile(w, r, abs)
}
