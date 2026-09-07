// Package cloud exposes the admin API for cloud GPU provisioning.
//
// Routes live under /api/admin/cloud/..., a distinct prefix from
// /api/admin/settings, so the registration-order constraint documented in
// routes/admin.go (specific /settings/<name> routes before the catch-all
// /settings/{key}) does not apply here.
//
// These are deliberately typed endpoints rather than generic settings keys:
// the generic /settings/{key} route accepts any key and any value with no
// validation whatsoever, and everything here moves money.
package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/crypto"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	cloudsvc "github.com/ZerkerEOD/krakenhashes/backend/internal/services/cloud"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Handler serves the cloud admin API.
type Handler struct {
	providers  *repository.CloudProviderRepository
	instances  *repository.CloudInstanceRepository
	budgetRepo *repository.CloudBudgetRepository
	budget     *cloudsvc.BudgetEngine
	estimator  *cloudsvc.Estimator
	// rules is the admin provisioning rails (when spending may happen). May be
	// nil, in which case the rules routes report 503 rather than panicking —
	// same reasoning as provisionForJob below.
	rules *repository.CloudProvisioningRulesRepository
	// providerFor resolves a configured provider for preflight and manual
	// teardown.
	providerFor func(ctx context.Context, providerConfigID uuid.UUID) (cloudsvc.Provider, error)
	// invalidateProvider drops the cached provider client after its config
	// changes. Without it a credential rotation has no effect until restart.
	invalidateProvider func(providerConfigID uuid.UUID)
	// provisionForJob rents exactly one instance for a job. May be nil, in
	// which case the manual provision route reports 503 rather than 404 — the
	// distinction matters when debugging, because "not wired" and "no such
	// route" call for very different fixes.
	provisionForJob func(ctx context.Context, jobID uuid.UUID) error
}

// NewHandler creates the cloud admin handler.
func NewHandler(
	providers *repository.CloudProviderRepository,
	instances *repository.CloudInstanceRepository,
	budgetRepo *repository.CloudBudgetRepository,
	budget *cloudsvc.BudgetEngine,
	estimator *cloudsvc.Estimator,
	rules *repository.CloudProvisioningRulesRepository,
	providerFor func(ctx context.Context, providerConfigID uuid.UUID) (cloudsvc.Provider, error),
	invalidateProvider func(providerConfigID uuid.UUID),
	provisionForJob func(ctx context.Context, jobID uuid.UUID) error,
) *Handler {
	return &Handler{
		providers:          providers,
		instances:          instances,
		budgetRepo:         budgetRepo,
		budget:             budget,
		estimator:          estimator,
		rules:              rules,
		providerFor:        providerFor,
		invalidateProvider: invalidateProvider,
		provisionForJob:    provisionForJob,
	}
}

// RegisterRoutes wires the cloud admin API onto an admin-scoped subrouter.
func (h *Handler) RegisterRoutes(r *mux.Router) {
	s := r.PathPrefix("/cloud").Subrouter()

	s.HandleFunc("/providers", h.ListProviders).Methods("GET", "OPTIONS")
	s.HandleFunc("/providers", h.CreateProvider).Methods("POST", "OPTIONS")
	// Literal sub-paths are registered before /providers/{id} so "preflight"
	// and "acknowledge" are never captured as an id.
	s.HandleFunc("/providers/{id}/preflight", h.Preflight).Methods("POST", "OPTIONS")
	s.HandleFunc("/providers/{id}/acknowledge", h.AcknowledgeProvider).Methods("POST", "OPTIONS")
	s.HandleFunc("/providers/{id}", h.UpdateProvider).Methods("PUT", "OPTIONS")
	s.HandleFunc("/providers/{id}", h.DeleteProvider).Methods("DELETE", "OPTIONS")

	s.HandleFunc("/instances", h.ListInstances).Methods("GET", "OPTIONS")
	s.HandleFunc("/instances/{id}", h.DestroyInstance).Methods("DELETE", "OPTIONS")

	s.HandleFunc("/clients", h.ListClientSettings).Methods("GET", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/budget", h.ClientBudget).Methods("GET", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/settings", h.GetClientSettings).Methods("GET", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/settings", h.UpdateClientSettings).Methods("PUT", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/policy", h.UpdateClientPolicy).Methods("PUT", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/policy", h.DeleteClientPolicy).Methods("DELETE", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/acknowledge", h.AcknowledgeClientProvider).Methods("POST", "OPTIONS")

	s.HandleFunc("/policy", h.GetDefaultPolicy).Methods("GET", "OPTIONS")
	s.HandleFunc("/policy", h.UpdateDefaultPolicy).Methods("PUT", "OPTIONS")

	// Provisioning rules: WHEN spending may happen, as opposed to /policy's
	// HOW MUCH. The client route returns the MERGED view; /rules returns the
	// system default unmerged. See rules.go for why that asymmetry matters.
	s.HandleFunc("/rules", h.GetDefaultRules).Methods("GET", "OPTIONS")
	s.HandleFunc("/rules", h.UpdateDefaultRules).Methods("PUT", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/rules", h.GetClientRules).Methods("GET", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/rules", h.UpdateClientRules).Methods("PUT", "OPTIONS")
	s.HandleFunc("/clients/{clientId}/rules", h.DeleteClientRules).Methods("DELETE", "OPTIONS")

	s.HandleFunc("/jobs/{jobId}/projection", h.JobProjection).Methods("GET", "OPTIONS")
	s.HandleFunc("/jobs/{jobId}/provision", h.ProvisionForJob).Methods("POST", "OPTIONS")
}

// actingUser resolves the authenticated admin. Acknowledgements and credential
// changes are attributed to this, never to anything in the request body.
func actingUser(r *http.Request) (uuid.UUID, error) {
	// Handles both middleware paths (user_uuid as uuid.UUID, user_id as string).
	id, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		return uuid.Nil, fmt.Errorf("no authenticated user on request")
	}
	return id, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debug.Error("cloud admin: encode response: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ListProviders returns every configured provider. Secrets never leave the
// backend — CloudProviderConfig.MarshalJSON emits has_credentials booleans
// instead of the ciphertext.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	configs, err := h.providers.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if configs == nil {
		configs = []*models.CloudProviderConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": configs,
		// An ephemeral key means every stored secret is unreadable after the
		// next restart. Surfacing it here matches the SSO admin API.
		"encryption_key_ephemeral": crypto.GetEncryptionService().IsEphemeral(),
	})
}

// CreateProvider stores a new provider configuration.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	h.saveProvider(w, r, uuid.Nil)
}

// UpdateProvider edits an existing provider configuration.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid provider config id")
		return
	}
	h.saveProvider(w, r, id)
}

/*
 * saveProvider is the shared create/update path.
 *
 * Secrets follow the SSO pattern: an empty `credentials` or `vpn_credential`
 * means "leave what is stored alone", so an admin toggling `enabled` does not
 * have to re-enter an API key — and cannot accidentally blank one.
 *
 * Enabling a provider is refused unless a VPN provider is configured. The
 * backend is never exposed to the internet, so an instance with no VPN
 * credential can never reach it: it would boot, fail to connect, and bill until
 * its watchdog fired.
 */
func (h *Handler) saveProvider(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var in models.CloudProviderConfigInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if !in.Provider.IsValid() {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("unsupported provider %q", in.Provider))
		return
	}
	if in.MaxInstanceHourlyCents < 0 || in.MaxConcurrentInstances < 0 {
		writeErr(w, http.StatusBadRequest, "instance limits must not be negative")
		return
	}

	cfg := &models.CloudProviderConfig{
		ID:                     id,
		Provider:               in.Provider,
		Name:                   in.Name,
		Enabled:                in.Enabled,
		Settings:               in.Settings,
		MaxConcurrentInstances: in.MaxConcurrentInstances,
		MaxInstanceHourlyCents: in.MaxInstanceHourlyCents,
		VPNProvider:            in.VPNProvider,
		VPNCredentialKind:      in.VPNCredentialKind,
		VPNTagOrGroup:          in.VPNTagOrGroup,
		BackendVPNHost:         in.BackendVPNHost,
	}

	// Carry forward what the update omits, and know whether a secret already
	// exists before deciding an enable is safe.
	var existing *models.CloudProviderConfig
	if id != uuid.Nil {
		var err error
		existing, err = h.providers.GetByID(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		cfg.VPNCredentialExpiresAt = existing.VPNCredentialExpiresAt
	}

	if in.Enabled {
		if err := validateEnable(&in, existing); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		/*
		 * Peer providers put client data on machines the operator does not
		 * control, so enabling one is a separate, attributed action.
		 *
		 * Routed through the predicate rather than naming vastai: AWS and
		 * RunPod Secure must keep enabling in ONE step with no acknowledgement
		 * dance, and the surest way to lose that is for someone adding a
		 * provider to extend this condition with an ||.
		 */
		if in.Provider.RequiresThirdPartyAck() &&
			(existing == nil || !existing.ThirdPartyAckAt.Valid) {
			writeErr(w, http.StatusBadRequest,
				providerAckPrompt(in.Provider))
			return
		}
	}

	enc := crypto.GetEncryptionService()
	if in.Credentials != "" {
		ciphertext, err := enc.Encrypt(in.Credentials)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to encrypt credentials: "+err.Error())
			return
		}
		cfg.CredentialsEncrypted = ciphertext
	}
	if in.VPNCredential != "" {
		ciphertext, err := enc.Encrypt(in.VPNCredential)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to encrypt VPN credential: "+err.Error())
			return
		}
		cfg.VPNCredentialEncrypted = ciphertext
		// A freshly supplied reusable key restarts its own clock; the caller
		// supplies the expiry it was issued with.
		cfg.VPNCredentialExpiresAt = parseExpiry(in.Settings)
	}

	if err := h.providers.Upsert(r.Context(), cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Drop the cached client so the next call uses the new configuration.
	// Without this a rotated credential — or an edited setting — keeps taking
	// effect only after a restart, which for a compromised key is the
	// difference between revoked and not.
	if h.invalidateProvider != nil {
		h.invalidateProvider(cfg.ID)
	}

	status := http.StatusOK
	if id == uuid.Nil {
		status = http.StatusCreated
	}
	writeJSON(w, status, cfg)
}

// validateEnable refuses configurations that would launch instances unable to
// reach the backend or unable to authenticate to the provider.
func validateEnable(in *models.CloudProviderConfigInput, existing *models.CloudProviderConfig) error {
	hasCreds := in.Credentials != "" || (existing != nil && existing.CredentialsEncrypted != "")
	// The mock provider drives local `agent --test-mode` processes and has
	// nothing to authenticate against.
	if !hasCreds && in.Provider != models.CloudProviderMock {
		return fmt.Errorf("provider credentials are required before enabling %s", in.Provider)
	}

	if in.VPNProvider == "" {
		return fmt.Errorf(
			"a VPN provider is required: the backend is not internet-exposed, so an instance " +
				"that cannot join your VPN would bill until its watchdog fired")
	}
	switch in.VPNProvider {
	case models.VPNProviderTailscale, models.VPNProviderNetBird, models.VPNProviderWireGuard:
	default:
		return fmt.Errorf("unsupported VPN provider %q (OpenVPN is not supported: it needs "+
			"/dev/net/tun and CAP_NET_ADMIN, which Vast.ai's unprivileged containers do not have)",
			in.VPNProvider)
	}

	hasVPNCred := in.VPNCredential != "" || (existing != nil && existing.VPNCredentialEncrypted != "")
	if !hasVPNCred {
		return fmt.Errorf("a %s credential is required before enabling this provider", in.VPNProvider)
	}
	if in.VPNProvider == models.VPNProviderTailscale &&
		in.VPNCredentialKind == models.VPNCredentialOAuth && in.VPNTagOrGroup == "" {
		return fmt.Errorf("Tailscale OAuth requires a tag (e.g. tag:kraken-worker); OAuth-minted keys are always tagged")
	}
	if in.BackendVPNHost == "" {
		return fmt.Errorf(
			"the backend's address on your VPN is required, and the server certificate must " +
				"already cover it or agents will fail TLS verification. Add it under " +
				"Admin -> Settings -> Server Certificate and click Apply & Reissue")
	}
	return nil
}

// parseExpiry reads an operator-supplied credential expiry out of settings.
// Reusable keys expire (Tailscale caps auth keys at 90 days) and a lapsed key
// strands every future launch, so the UI counts down against this.
func parseExpiry(settings models.JSONMap) sql.NullTime {
	raw, ok := settings["vpn_credential_expires_at"].(string)
	if !ok || raw == "" {
		return sql.NullTime{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// DeleteProvider removes a provider configuration. The FK from cloud_instances
// is RESTRICT, so this fails while rented hardware still references it.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid provider config id")
		return
	}
	if err := h.providers.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AcknowledgeProvider records that an admin accepted a provider's data-exposure
// terms. Attributed to the authenticated caller and required before Vast.ai can
// be enabled.
func (h *Handler) AcknowledgeProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid provider config id")
		return
	}
	userID, err := actingUser(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := h.providers.RecordThirdPartyAck(r.Context(), id, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	debug.Info("Cloud provider %s third-party data exposure acknowledged by %s", id, userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// ListClientSettings returns every client that could spend money — cloud
// enabled, or funded but currently disabled.
func (h *Handler) ListClientSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.budgetRepo.ListClientCloudSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if settings == nil {
		settings = []*models.ClientCloudSettings{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": settings})
}

// GetClientSettings returns one client's cloud burst configuration.
func (h *Handler) GetClientSettings(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	settings, err := h.budgetRepo.GetClientCloudSettings(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

/*
 * UpdateClientSettings edits a client's budget, TTL ceiling and provider
 * allowlist.
 *
 * Allowlisting a provider that exposes data to third parties requires that
 * client's own acknowledgement to already be on file. The provider-level
 * acknowledgement is not enough: it says the operator understands Vast.ai, not
 * that this particular engagement's data may go there.
 */
func (h *Handler) UpdateClientSettings(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}

	var in models.ClientCloudSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	current, err := h.budgetRepo.GetClientCloudSettings(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	for _, p := range in.ProviderAllowlist {
		if !models.CloudProvider(p).RequiresThirdPartyAck() {
			continue
		}
		if _, acked := current.ProviderAck[p]; !acked {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf(
				"acknowledge %s third-party data exposure for this client before allowing it",
				providerDisplayName(models.CloudProvider(p))))
			return
		}
	}

	if err := h.budgetRepo.UpdateClientCloudSettings(r.Context(), clientID, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := h.budgetRepo.GetClientCloudSettings(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	debug.Info("Cloud settings updated for client %s (enabled=%v, providers=%v)",
		clientID, updated.Enabled, updated.ProviderAllowlist)
	writeJSON(w, http.StatusOK, updated)
}

// AcknowledgeClientProvider records this client's per-provider data-exposure
// acknowledgement, attributed to the authenticated admin.
func (h *Handler) AcknowledgeClientProvider(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	userID, err := actingUser(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}

	var body struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := h.budgetRepo.RecordClientProviderAck(r.Context(), clientID, body.Provider, userID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	debug.Info("Client %s acknowledged %s data exposure (by %s)", clientID, body.Provider, userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// GetDefaultPolicy returns the system-default threshold ladder.
func (h *Handler) GetDefaultPolicy(w http.ResponseWriter, r *http.Request) {
	// uuid.Nil matches no client, so GetPolicy falls through to the default row.
	policy, err := h.budgetRepo.GetPolicy(r.Context(), uuid.Nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// UpdateDefaultPolicy edits the system-default threshold ladder.
func (h *Handler) UpdateDefaultPolicy(w http.ResponseWriter, r *http.Request) {
	h.savePolicy(w, r, nil)
}

// UpdateClientPolicy creates or edits a client's threshold override.
func (h *Handler) UpdateClientPolicy(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	h.savePolicy(w, r, &clientID)
}

func (h *Handler) savePolicy(w http.ResponseWriter, r *http.Request, clientID *uuid.UUID) {
	var policy models.CloudBudgetPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	// The path owns the scope; a body claiming a different client is ignored.
	policy.ClientID = clientID

	if err := h.budgetRepo.UpsertPolicy(r.Context(), &policy); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

// DeleteClientPolicy drops a client override so the client falls back to the
// system default.
func (h *Handler) DeleteClientPolicy(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	if err := h.budgetRepo.DeletePolicy(r.Context(), clientID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListInstances returns every live rented instance.
func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.instances.ListLive(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if instances == nil {
		instances = []*models.CloudInstance{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": instances})
}

/*
 * DestroyInstance is the operator's manual kill switch.
 *
 * Deliberately calls the provider directly rather than only flagging the row
 * for the reaper: when an admin reaches for this, something is already wrong
 * and the next reaper tick is up to a minute of billing away.
 */
func (h *Handler) DestroyInstance(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid instance id")
		return
	}

	inst, err := h.instances.GetByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}

	provider, err := h.providerFor(r.Context(), inst.ProviderConfigID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("no provider for instance: %v", err))
		return
	}

	if err := h.instances.SetState(r.Context(), id, models.CloudInstanceTerminating, "destroyed by admin"); err != nil {
		debug.Error("cloud admin: failed to mark terminating: %v", err)
	}

	if inst.ProviderInstanceID != "" {
		if err := provider.Destroy(r.Context(), inst.ProviderInstanceID); err != nil {
			attempts, _ := h.instances.RecordTerminateFailure(r.Context(), id, err.Error())
			// 502, not 500: the failure is at the provider, and the instance
			// is still billing. The row stays live so it keeps accruing and
			// keeps blocking new provisioning for that client.
			writeErr(w, http.StatusBadGateway,
				fmt.Sprintf("provider refused to destroy instance (attempt %d): %v", attempts, err))
			return
		}
	}

	if err := h.budget.SettleInstance(r.Context(), inst); err != nil {
		debug.Error("cloud admin: failed to settle budget: %v", err)
	}
	if err := h.instances.SetState(r.Context(), id, models.CloudInstanceTerminated, "destroyed by admin"); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "terminated"})
}

// Preflight reports whether a provider could actually launch anything.
func (h *Handler) Preflight(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid provider config id")
		return
	}
	provider, err := h.providerFor(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	report, err := provider.Preflight(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Always 200: a failing preflight is a successful diagnosis, and the
	// report body is the actionable part.
	writeJSON(w, http.StatusOK, report)
}

// ClientBudget returns the spend picture and the action it implies.
func (h *Handler) ClientBudget(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	assessment, err := h.budget.Assess(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state":  assessment.State,
		"policy": assessment.Policy,
		"action": assessment.Action.String(),
		"reason": assessment.Reason,
	})
}

/*
 * JobProjection answers "will this finish before the budget runs out?".
 *
 * Returns the NPK-style coverage percentage. Below 100 the UI must require an
 * explicit confirmation rather than silently launching — NPK's equivalent
 * warning was commented out, which turned their cap into a silent killer.
 */
func (h *Handler) JobProjection(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(mux.Vars(r)["jobId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid job id")
		return
	}

	q := r.URL.Query()
	var extraSpeed int64
	fmt.Sscanf(q.Get("cloud_speed"), "%d", &extraSpeed)
	var rateCents int
	fmt.Sscanf(q.Get("hourly_rate_cents"), "%d", &rateCents)

	// Resolve the paying client. Falling back to the job's own hashlist matters:
	// with no client the available budget is zero, which renders as "0% covered"
	// and reads as a genuine shortfall rather than a missing argument.
	var clientID uuid.UUID
	if raw := q.Get("client_id"); raw != "" {
		if parsed, perr := uuid.Parse(raw); perr == nil {
			clientID = parsed
		}
	}
	if clientID == uuid.Nil {
		if resolved, rerr := h.budgetRepo.ClientForJob(r.Context(), jobID); rerr == nil {
			clientID = resolved
		} else {
			debug.Warning("cloud admin: could not resolve client for job %s: %v", jobID, rerr)
		}
	}

	var available int64
	if clientID != uuid.Nil {
		if a, aerr := h.budget.Assess(r.Context(), clientID); aerr == nil {
			available = a.State.AvailableCents
		} else {
			debug.Warning("cloud admin: budget assessment failed for client %s: %v", clientID, aerr)
		}
	}

	projection, err := h.estimator.Project(r.Context(), jobID, extraSpeed, rateCents, available)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projection)
}

/*
 * ProvisionForJob rents exactly one instance for a job, on demand.
 *
 * This exists alongside the autoscaler rather than instead of it. The
 * autoscaler decides *when* to spend; this decides *now*, which is what makes
 * it the deterministic entry point for testing the provisioning path and the
 * first thing to reach for when the autoscaler is not doing what you expect.
 *
 * One instance per call, deliberately. Provisioning commits real money before
 * anything has proven it can even register, so a burst is something an operator
 * should have to ask for repeatedly rather than something a single click can
 * trigger.
 *
 * Runs on a detached context. The request context dies when the admin's browser
 * navigates away, and a cancellation landing between "the provider created the
 * instance" and "we recorded it" produces an instance nobody owns, billing until
 * the orphan sweep notices. A rented instance must outlive the HTTP request that
 * asked for it.
 */
func (h *Handler) ProvisionForJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(mux.Vars(r)["jobId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid job id")
		return
	}
	if h.provisionForJob == nil {
		writeErr(w, http.StatusServiceUnavailable, "cloud provisioning is not wired on this server")
		return
	}

	actor, err := actingUser(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	debug.Info("cloud admin: user %s requested manual provisioning for job %s", actor, jobID)

	// Long enough for an image-heavy provider to answer, short enough that a
	// wedged provider API cannot pin a goroutine forever.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := h.provisionForJob(ctx, jobID); err != nil {
		// Every refusal inside ProvisionForJob is a precondition failure the
		// operator can act on — no budget, no eligible provider, no VPN
		// credential, cap reached — so the message is surfaced rather than
		// flattened into "internal error".
		debug.Error("cloud admin: manual provisioning failed for job %s: %v", jobID, err)
		writeErr(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "provisioning",
		"job_id": jobID.String(),
	})
}

// providerDisplayName is the operator-facing name for a provider kind. The two
// RunPod tiers must never both render as "RunPod": an admin acknowledging
// third-party data exposure needs to see that it is the Community tier they are
// consenting to, not the SOC 2 one they probably think they bought.
func providerDisplayName(p models.CloudProvider) string {
	switch p {
	case models.CloudProviderVastAI:
		return "Vast.ai"
	case models.CloudProviderAWS:
		return "AWS"
	case models.CloudProviderRunPod:
		return "RunPod Secure Cloud"
	case models.CloudProviderRunPodCommunity:
		return "RunPod Community Cloud"
	case models.CloudProviderMock:
		return "Mock"
	default:
		return string(p)
	}
}

// providerAckPrompt explains what is being acknowledged. The threat is the same
// for both peer providers — the host's owner has root over the container — so
// the wording is deliberately the same; only the reason the tier qualifies
// differs.
func providerAckPrompt(p models.CloudProvider) string {
	switch p {
	case models.CloudProviderRunPodCommunity:
		return "RunPod Community Cloud runs on peer-operated machines whose owners have root over " +
			"the container, and RunPod's SOC 2, ISO 27001 and PCI DSS attestations cover only Secure " +
			"Cloud, not this tier. Acknowledge the data-exposure terms before enabling it."
	default:
		return providerDisplayName(p) + " runs GPUs on third-party machines whose owners have root " +
			"over the container. Acknowledge the data-exposure terms before enabling it."
	}
}
