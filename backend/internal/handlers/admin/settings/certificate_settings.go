package settings

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/certs"
	tlspkg "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// CertificateHandler serves the server-certificate admin API.
type CertificateHandler struct {
	svc *certs.Service
}

func NewCertificateHandler(svc *certs.Service) *CertificateHandler {
	return &CertificateHandler{svc: svc}
}

// RegisterRoutes mounts the API under /admin/tls.
//
// Deliberately NOT under /admin/settings. The admin router registers
// /settings/{key} catch-alls, and gorilla/mux matches in registration order --
// because these routes are attached after SetupRoutes has already run, a path
// like /admin/settings/certificate would be swallowed by the generic
// single-key handler and return "setting not found".
func (h *CertificateHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/tls/certificate", h.GetStatus).Methods("GET")
	r.HandleFunc("/tls/discovered", h.ListDiscovered).Methods("GET")
	r.HandleFunc("/tls/discovered/{id}", h.DismissDiscovered).Methods("DELETE")
	r.HandleFunc("/tls/san-failures", h.GetSANFailures).Methods("GET")
	r.HandleFunc("/tls/sans", h.UpdateSANs).Methods("PUT")
	r.HandleFunc("/tls/reissue", h.Reissue).Methods("POST")
	r.HandleFunc("/tls/rotate-ca", h.RotateCA).Methods("POST")
}

// GetStatus returns the live certificate, the configured names, and the drift
// between them.
func (h *CertificateHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		debug.Error("Failed to build certificate status: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to read certificate status")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, status)
}

// DiscoveredAddress is one grouped candidate, with its status relative to the
// live certificate and the pending configuration.
type DiscoveredAddress struct {
	ID              int64      `json:"id"`
	Address         string     `json:"address"`
	Kind            string     `json:"kind"`
	Sources         []string   `json:"sources"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	HitCount        int64      `json:"hit_count"`
	LastAgentID     *int       `json:"last_agent_id,omitempty"`
	LastAgentName   *string    `json:"last_agent_name,omitempty"`
	LastUserAgent   *string    `json:"last_user_agent,omitempty"`
	LastPort        *int       `json:"last_port,omitempty"`
	Status          string     `json:"status"`
	Allowed         bool       `json:"allowed"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
	DismissedAt     *time.Time `json:"dismissed_at,omitempty"`
}

// ListDiscovered returns observed addresses the certificate does not cover.
func (h *CertificateHandler) ListDiscovered(w http.ResponseWriter, r *http.Request) {
	repo := h.svc.Candidates()
	if repo == nil {
		httputil.RespondWithJSON(w, http.StatusOK, []DiscoveredAddress{})
		return
	}

	rows, err := repo.List(r.Context(), false)
	if err != nil {
		debug.Error("Failed to list SAN candidates: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list discovered addresses")
		return
	}

	status, err := h.svc.Status(r.Context())
	if err != nil {
		debug.Error("Failed to read certificate status while listing candidates: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list discovered addresses")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, groupCandidates(rows, status))
}

// groupCandidates collapses per-source rows into one entry per address and
// classifies each against the live certificate.
//
// Classification happens here, at read time, rather than being filtered out at
// write time: a candidate that stops being relevant when an address is added
// becomes relevant again if that address is later removed, and discarding it on
// ingest would lose it permanently.
func groupCandidates(rows []repository.TLSSANCandidate, status *certs.CertificateStatus) []DiscoveredAddress {
	inCert := make(map[string]struct{})
	pending := make(map[string]struct{})

	if status != nil {
		if status.Certificate != nil {
			for _, n := range status.Certificate.DNSNames {
				inCert[n] = struct{}{}
			}
			for _, ip := range status.Certificate.IPAddresses {
				inCert[ip] = struct{}{}
			}
		}
		if status.EffectiveSANs != nil {
			for _, n := range status.EffectiveSANs.DNSNames {
				pending[n] = struct{}{}
			}
			for _, ip := range status.EffectiveSANs.IPAddresses {
				pending[ip] = struct{}{}
			}
		}
	}

	byAddress := make(map[string]*DiscoveredAddress)
	order := make([]string, 0, len(rows))

	for _, row := range rows {
		existing, seen := byAddress[row.Address]
		if !seen {
			entry := &DiscoveredAddress{
				ID:            row.ID,
				Address:       row.Address,
				Kind:          row.Kind,
				Sources:       []string{row.Source},
				FirstSeenAt:   row.FirstSeenAt,
				LastSeenAt:    row.LastSeenAt,
				HitCount:      row.HitCount,
				LastAgentID:   row.LastAgentID,
				LastAgentName: row.LastAgentName,
				LastUserAgent: row.LastUserAgent,
				LastPort:      row.LastPort,
				DismissedAt:   row.DismissedAt,
			}
			entry.Status, entry.Allowed, entry.RejectionReason = classifyCandidate(row, inCert, pending)
			byAddress[row.Address] = entry
			order = append(order, row.Address)
			continue
		}

		existing.Sources = append(existing.Sources, row.Source)
		existing.HitCount += row.HitCount
		if row.LastSeenAt.After(existing.LastSeenAt) {
			existing.LastSeenAt = row.LastSeenAt
		}
		if row.FirstSeenAt.Before(existing.FirstSeenAt) {
			existing.FirstSeenAt = row.FirstSeenAt
		}
		// Agent attribution wins over a passive sighting: an operator needs to
		// know WHICH agent cannot connect.
		if row.LastAgentID != nil {
			existing.LastAgentID = row.LastAgentID
			existing.LastAgentName = row.LastAgentName
		}
		if row.LastPort != nil {
			existing.LastPort = row.LastPort
		}
	}

	out := make([]DiscoveredAddress, 0, len(order))
	for _, address := range order {
		out = append(out, *byAddress[address])
	}
	return out
}

// classifyCandidate decides how one address should be presented.
func classifyCandidate(row repository.TLSSANCandidate, inCert, pending map[string]struct{}) (status string, allowed bool, reason string) {
	// Re-validate rather than trusting the stored row. The policy is the
	// server's to enforce, and a value that was acceptable when observed must
	// still be acceptable now.
	if row.Kind == "ip" {
		if _, err := tlspkg.ValidateInternalIP(row.Address); err != nil {
			return "new", false, err.Error()
		}
	} else if _, err := tlspkg.ValidateDNSName(row.Address); err != nil {
		return "new", false, err.Error()
	}

	if _, ok := inCert[row.Address]; ok {
		return "in_certificate", true, ""
	}
	if _, ok := pending[row.Address]; ok {
		return "pending_reissue", true, ""
	}
	return "new", true, ""
}

// DismissDiscovered hides a candidate from the default list.
func (h *CertificateHandler) DismissDiscovered(w http.ResponseWriter, r *http.Request) {
	repo := h.svc.Candidates()
	if repo == nil {
		httputil.RespondWithError(w, http.StatusServiceUnavailable, "Address discovery is not available")
		return
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid candidate id")
		return
	}

	userID, _ := r.Context().Value("user_id").(string)
	parsedUser, err := uuid.Parse(userID)
	if err != nil {
		httputil.RespondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := repo.Dismiss(r.Context(), id, parsedUser); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondWithError(w, http.StatusNotFound, "Discovered address not found")
			return
		}
		debug.Error("Failed to dismiss SAN candidate %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to dismiss address")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// sanFailureWindow is how recently an agent must have reported for the banner to
// show.
//
// Combined with the agent's 30-minute re-report heartbeat, this means the banner
// self-clears within 15 minutes of an agent going away -- but the "not in the
// certificate" check below clears it IMMEDIATELY on a successful reissue, which
// is the case that actually matters.
const sanFailureWindow = 15 * time.Minute

// SANFailureSummary drives the "an agent cannot verify this certificate" banner.
type SANFailureSummary struct {
	Active            bool             `json:"active"`
	DistinctAddresses int              `json:"distinct_addresses"`
	AgentCount        int              `json:"agent_count"`
	LastReportAt      *time.Time       `json:"last_report_at,omitempty"`
	Addresses         []SANFailureItem `json:"addresses"`
}

type SANFailureItem struct {
	Address    string    `json:"address"`
	AgentNames []string  `json:"agent_names"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// GetSANFailures reports agents that currently cannot verify this server.
func (h *CertificateHandler) GetSANFailures(w http.ResponseWriter, r *http.Request) {
	summary := SANFailureSummary{Addresses: []SANFailureItem{}}

	repo := h.svc.Candidates()
	if repo == nil {
		httputil.RespondWithJSON(w, http.StatusOK, summary)
		return
	}

	rows, err := repo.ListRecentAgentFailures(r.Context(), sanFailureWindow)
	if err != nil {
		debug.Error("Failed to list recent agent TLS failures: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to read TLS failures")
		return
	}

	status, err := h.svc.Status(r.Context())
	if err != nil {
		debug.Error("Failed to read certificate status for the failure banner: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to read TLS failures")
		return
	}

	covered := make(map[string]struct{})
	if status.Certificate != nil {
		for _, n := range status.Certificate.DNSNames {
			covered[n] = struct{}{}
		}
		for _, ip := range status.Certificate.IPAddresses {
			covered[ip] = struct{}{}
		}
	}

	agents := make(map[string]struct{})
	byAddress := make(map[string]*SANFailureItem)
	var order []string

	for _, row := range rows {
		// A reissue that added the address clears the banner at once, without
		// waiting for the reporting window to lapse.
		if _, ok := covered[row.Address]; ok {
			continue
		}

		item, seen := byAddress[row.Address]
		if !seen {
			item = &SANFailureItem{Address: row.Address, LastSeenAt: row.LastSeenAt}
			byAddress[row.Address] = item
			order = append(order, row.Address)
		}
		if row.LastSeenAt.After(item.LastSeenAt) {
			item.LastSeenAt = row.LastSeenAt
		}
		if row.LastAgentName != nil && *row.LastAgentName != "" {
			item.AgentNames = append(item.AgentNames, *row.LastAgentName)
			agents[*row.LastAgentName] = struct{}{}
		}
		if summary.LastReportAt == nil || row.LastSeenAt.After(*summary.LastReportAt) {
			t := row.LastSeenAt
			summary.LastReportAt = &t
		}
	}

	for _, address := range order {
		summary.Addresses = append(summary.Addresses, *byAddress[address])
	}
	summary.DistinctAddresses = len(summary.Addresses)
	summary.AgentCount = len(agents)
	summary.Active = summary.DistinctAddresses > 0

	httputil.RespondWithJSON(w, http.StatusOK, summary)
}

// UpdateSANsRequest is the PUT /tls/sans body.
type UpdateSANsRequest struct {
	AdditionalIPAddresses []string `json:"additional_ip_addresses"`
	AdditionalDNSNames    []string `json:"additional_dns_names"`
	// Apply chains straight into a reissue, which is what the UI's single
	// "Apply & Reissue" button does.
	Apply bool `json:"apply"`
}

// UpdateSANs validates and persists both lists, optionally reissuing.
func (h *CertificateHandler) UpdateSANs(w http.ResponseWriter, r *http.Request) {
	var req UpdateSANsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	result, err := h.svc.UpdateSANs(r.Context(), req.AdditionalIPAddresses, req.AdditionalDNSNames)
	if err != nil {
		h.respondOperationError(w, err, "Failed to save certificate names")
		return
	}

	if len(result.Rejected) > 0 {
		httputil.RespondWithJSON(w, http.StatusBadRequest, map[string]any{
			"error":    "one or more entries were rejected",
			"rejected": result.Rejected,
		})
		return
	}

	if !req.Apply {
		status, err := h.svc.Status(r.Context())
		if err != nil {
			debug.Error("Failed to read status after saving certificate names: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Saved, but failed to read status")
			return
		}
		httputil.RespondWithJSON(w, http.StatusOK, status)
		return
	}

	report, err := h.svc.Reissue(r.Context(), tlspkg.ReissueOptions{})
	if err != nil {
		h.respondOperationError(w, err, "Names were saved, but the certificate could not be reissued")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, report)
}

// ReissueRequest is the POST /tls/reissue body.
type ReissueRequest struct {
	Force bool `json:"force"`
	// IncludeClientLeaf is the manual escape hatch for an expiring shared agent
	// certificate. Off by default: the client leaf carries no names, so a name
	// change never requires touching it.
	IncludeClientLeaf bool `json:"include_client_leaf"`
}

// Reissue regenerates the server leaf under the existing CA.
func (h *CertificateHandler) Reissue(w http.ResponseWriter, r *http.Request) {
	var req ReissueRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req)
	}

	report, err := h.svc.Reissue(r.Context(), tlspkg.ReissueOptions{
		Force:             req.Force,
		IncludeClientLeaf: req.IncludeClientLeaf,
	})
	if err != nil {
		h.respondOperationError(w, err, "Failed to reissue the certificate")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, report)
}

// rotateCAConfirmation is the exact phrase required to rotate the CA.
//
// Not translated: a type-to-confirm guard only works if the string is stable.
const rotateCAConfirmation = "ROTATE CA"

// RotateCARequest is the POST /tls/rotate-ca body.
type RotateCARequest struct {
	Confirm string `json:"confirm"`
}

// RotateCA regenerates the certificate authority and every leaf beneath it.
func (h *CertificateHandler) RotateCA(w http.ResponseWriter, r *http.Request) {
	var req RotateCARequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Confirm != rotateCAConfirmation {
		httputil.RespondWithError(w, http.StatusBadRequest,
			"Rotating the certificate authority requires explicit confirmation")
		return
	}

	report, err := h.svc.RotateCA(r.Context())
	if err != nil {
		h.respondOperationError(w, err, "Failed to rotate the certificate authority")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, report)
}

// respondOperationError maps a service error onto a status code.
//
// An unmanaged TLS mode is 409 rather than 404: the resource exists and is
// readable, it just cannot be changed from here, and the reason says where to
// change it instead.
func (h *CertificateHandler) respondOperationError(w http.ResponseWriter, err error, fallback string) {
	var unmanaged certs.ErrUnmanaged
	if errors.As(err, &unmanaged) {
		httputil.RespondWithError(w, http.StatusConflict, unmanaged.Reason)
		return
	}
	debug.Error("%s: %v", fallback, err)
	httputil.RespondWithError(w, http.StatusInternalServerError, fallback)
}
