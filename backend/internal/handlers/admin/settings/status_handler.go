package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/certs"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
GET /api/admin/settings/status

One round trip that tells the admin settings hub what needs attention. Each
probe is independent: a failing probe marks its own card "error" instead of
failing the whole response, because an unreachable network share must not hide
a certificate that expires tomorrow.
*/

type EmailStatus struct {
	Configured bool   `json:"configured"`
	Error      string `json:"error,omitempty"`
}

type CertificateStatusSummary struct {
	Managed       bool       `json:"managed"`
	TLSMode       string     `json:"tls_mode"`
	DaysRemaining *int       `json:"days_remaining,omitempty"`
	NotAfter      *time.Time `json:"not_after,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type SSOStatus struct {
	EphemeralKey     bool   `json:"ephemeral_key"`
	ProvidersEnabled int    `json:"providers_enabled"`
	ProvidersTotal   int    `json:"providers_total"`
	Error            string `json:"error,omitempty"`
}

type BinariesStatus struct {
	Total          int    `json:"total"`
	VerifiedActive int    `json:"verified_active"`
	Error          string `json:"error,omitempty"`
}

type CloudStatus struct {
	Providers       int    `json:"providers"`
	MonthlyCapCents int64  `json:"monthly_cap_cents"`
	Error           string `json:"error,omitempty"`
}

type StorageStatus struct {
	Enabled        bool   `json:"enabled"`
	Reachable      bool   `json:"reachable"`
	Backend        string `json:"backend"`
	MigrationState string `json:"migration_state"`
	Error          string `json:"error,omitempty"`
}

type WebhookStatus struct {
	Enabled   bool   `json:"enabled"`
	HasURL    bool   `json:"has_url"`
	HasSecret bool   `json:"has_secret"`
	Error     string `json:"error,omitempty"`
}

type SettingsStatusResponse struct {
	Email       EmailStatus              `json:"email"`
	Certificate CertificateStatusSummary `json:"certificate"`
	SSO         SSOStatus                `json:"sso"`
	Binaries    BinariesStatus           `json:"binaries"`
	Cloud       CloudStatus              `json:"cloud"`
	Storage     StorageStatus            `json:"storage"`
	Webhook     WebhookStatus            `json:"webhook"`
	GeneratedAt time.Time                `json:"generated_at"`
}

// StatusDeps are the probes. Any may be nil; its card then reports "unavailable".
type StatusDeps struct {
	DB                 *db.DB
	Certs              *certs.Service
	SSORepo            *repository.SSORepository
	Binaries           binary.Manager
	CloudProviders     *repository.CloudProviderRepository
	SystemSettingsRepo *repository.SystemSettingsRepository
	NetworkShare       *services.NetworkShareService
}

type StatusHandler struct {
	deps StatusDeps
}

func NewStatusHandler(deps StatusDeps) *StatusHandler {
	return &StatusHandler{deps: deps}
}

// RegisterRoutes attaches GET /settings/status. Registered as a static path
// so it is matched before the generic /settings/{key} route.
// GetStatus serves GET /api/admin/settings/status. The route is reserved in
// routes.SetupAdminRoutes (it must precede /settings/{key}); main.go installs
// this handler with routes.SetSettingsStatusHandler.
func (h *StatusHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := SettingsStatusResponse{
		Email:       h.email(),
		Certificate: h.certificate(ctx),
		SSO:         h.sso(ctx),
		Binaries:    h.binaries(ctx),
		Cloud:       h.cloud(ctx),
		Storage:     h.storage(ctx),
		Webhook:     h.webhook(ctx),
		GeneratedAt: time.Now().UTC(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		debug.Error("settings status: encode: %v", err)
	}
}

const unavailable = "unavailable"

func (h *StatusHandler) email() EmailStatus {
	if h.deps.DB == nil {
		return EmailStatus{Error: unavailable}
	}
	ok, err := h.deps.DB.HasActiveEmailProvider()
	if err != nil {
		return EmailStatus{Error: err.Error()}
	}
	return EmailStatus{Configured: ok}
}

func (h *StatusHandler) certificate(ctx context.Context) CertificateStatusSummary {
	if h.deps.Certs == nil {
		return CertificateStatusSummary{Error: unavailable}
	}
	st, err := h.deps.Certs.Status(ctx)
	if err != nil {
		return CertificateStatusSummary{Error: err.Error()}
	}
	out := CertificateStatusSummary{Managed: st.Managed, TLSMode: st.TLSMode}
	if st.Certificate != nil {
		days := st.Certificate.DaysRemaining
		out.DaysRemaining = &days
		notAfter := st.Certificate.NotAfter
		out.NotAfter = &notAfter
	}
	return out
}

func (h *StatusHandler) sso(ctx context.Context) SSOStatus {
	out := SSOStatus{EphemeralKey: sso.GetEncryptionService().IsEphemeral()}
	if h.deps.SSORepo == nil {
		out.Error = unavailable
		return out
	}
	providers, err := h.deps.SSORepo.ListProviders(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.ProvidersTotal = len(providers)
	for _, p := range providers {
		if p != nil && p.Enabled {
			out.ProvidersEnabled++
		}
	}
	return out
}

func (h *StatusHandler) binaries(ctx context.Context) BinariesStatus {
	if h.deps.Binaries == nil {
		return BinariesStatus{Error: unavailable}
	}
	versions, err := h.deps.Binaries.ListVersions(ctx, map[string]interface{}{})
	if err != nil {
		return BinariesStatus{Error: err.Error()}
	}
	out := BinariesStatus{Total: len(versions)}
	for _, v := range versions {
		if v != nil && v.IsActive && v.VerificationStatus == binary.VerificationStatusVerified {
			out.VerifiedActive++
		}
	}
	return out
}

func (h *StatusHandler) cloud(ctx context.Context) CloudStatus {
	out := CloudStatus{}
	if h.deps.CloudProviders != nil {
		providers, err := h.deps.CloudProviders.List(ctx)
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Providers = len(providers)
		}
	} else {
		out.Error = unavailable
	}
	if h.deps.SystemSettingsRepo != nil {
		if s, err := h.deps.SystemSettingsRepo.GetSetting(ctx, "cloud_global_monthly_cap_cents"); err == nil && s != nil && s.Value != nil {
			if v, perr := strconv.ParseInt(*s.Value, 10, 64); perr == nil {
				out.MonthlyCapCents = v
			}
		}
	}
	return out
}

func (h *StatusHandler) storage(ctx context.Context) StorageStatus {
	if h.deps.NetworkShare == nil {
		return StorageStatus{Error: unavailable}
	}
	cfg, err := h.deps.NetworkShare.Get(ctx)
	if err != nil {
		return StorageStatus{Error: err.Error()}
	}
	out := StorageStatus{}
	if cfg != nil {
		out.Enabled = cfg.Enabled
		out.Backend = cfg.StorageBackend
		out.MigrationState = cfg.MigrationState
		if cfg.Enabled {
			out.Reachable = h.deps.NetworkShare.Health(ctx)
		}
	}
	return out
}

func (h *StatusHandler) webhook(ctx context.Context) WebhookStatus {
	if h.deps.SystemSettingsRepo == nil {
		return WebhookStatus{Error: unavailable}
	}
	get := func(key string) string {
		s, err := h.deps.SystemSettingsRepo.GetSetting(ctx, key)
		if err != nil || s == nil || s.Value == nil {
			return ""
		}
		return *s.Value
	}
	return WebhookStatus{
		Enabled:   get("global_webhook_enabled") == "true",
		HasURL:    get("global_webhook_url") != "",
		HasSecret: get("global_webhook_secret") != "",
	}
}
