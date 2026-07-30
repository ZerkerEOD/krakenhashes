package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/analytics/bhupload"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/analytics/pdf"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/bloodhound"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
)

// AnalyticsHandler exposes the Analytics & Reporting API on the User API-key surface (/api/v1). It
// shares the same repository, queue poller, team service and PDF generator as the JWT app surface.
type AnalyticsHandler struct {
	repo         *repository.AnalyticsRepository
	queueService *services.AnalyticsQueueService
	teamService  *services.TeamService
	clientRepo   *repository.ClientRepository
	userRepo     *repository.UserRepository
	auditRepo    *repository.AuditLogRepository
	pdfGen       *pdf.Generator
	db           *db.DB
}

// NewAnalyticsHandler creates the User API analytics handler. queueService is the singleton poller
// started in main; this handler only reads its status and never starts a second poller.
func NewAnalyticsHandler(database *db.DB, queueService *services.AnalyticsQueueService, teamService *services.TeamService) *AnalyticsHandler {
	return &AnalyticsHandler{
		repo:         repository.NewAnalyticsRepository(database),
		queueService: queueService,
		teamService:  teamService,
		clientRepo:   repository.NewClientRepository(database),
		userRepo:     repository.NewUserRepository(database),
		auditRepo:    repository.NewAuditLogRepository(database),
		pdfGen:       pdf.NewGenerator(),
		db:           database,
	}
}

// checkClientAccess mirrors the client handler's team-scoping guard for the API-key surface.
func (h *AnalyticsHandler) checkClientAccess(r *http.Request, clientID uuid.UUID) (int, string, string) {
	ctx := r.Context()
	if h.teamService == nil || !h.teamService.IsTeamsEnabled(ctx) {
		return 0, "", ""
	}
	userID, err := getUserID(r)
	if err != nil {
		return http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication required"
	}
	canAccess, err := h.teamService.CanUserAccessClient(ctx, userID, clientID, false)
	if err != nil {
		debug.Error("v1 analytics: failed to check client access for %s: %v", clientID, err)
		return http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to verify client access"
	}
	if !canAccess {
		return http.StatusNotFound, "RESOURCE_NOT_FOUND", "Client not found"
	}
	return 0, "", ""
}

func (h *AnalyticsHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debug.Error("v1 analytics: failed to encode response: %v", err)
	}
}

// CreateReport queues a new analytics report (no BloodHound dump).
// POST /api/v1/analytics/reports
func (h *AnalyticsHandler) CreateReport(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserID(r)
	if err != nil {
		sendAPIError(w, "Authentication required", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}
	var req models.CreateAnalyticsReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendAPIError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}
	if req.ClientID == uuid.Nil {
		sendAPIError(w, "client_id is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if req.EndDate.IsZero() {
		req.EndDate = time.Now()
	}
	if req.StartDate.IsZero() {
		req.StartDate = req.EndDate
	}
	if req.EndDate.Before(req.StartDate) {
		sendAPIError(w, "end_date must be after start_date", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if status, code, msg := h.checkClientAccess(r, req.ClientID); status != 0 {
		sendAPIError(w, msg, code, status)
		return
	}

	queuePos, err := h.repo.GetNextQueuePosition(r.Context())
	if err != nil {
		sendAPIError(w, "Failed to queue report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	report := &models.AnalyticsReport{
		ID:             uuid.New(),
		ClientID:       req.ClientID,
		UserID:         userID,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		Status:         "queued",
		CustomPatterns: req.CustomPatterns,
		HashlistIDs:    pq.Int64Array(req.HashlistIDs),
		QueuePosition:  &queuePos,
		CreatedAt:      time.Now(),
	}
	if err := h.repo.Create(r.Context(), report); err != nil {
		debug.Error("v1 analytics: create report failed: %v", err)
		sendAPIError(w, "Failed to create report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusCreated, report)
}

// CreateReportWithBloodhound queues a report enriched with an uploaded BloodHound dump. The dump is
// parsed in memory only; the derived context is persisted atomically with the report and cleared
// after generation. See the JWT handler for the multipart contract.
// POST /api/v1/analytics/reports/bloodhound
func (h *AnalyticsHandler) CreateReportWithBloodhound(w http.ResponseWriter, r *http.Request) {
	lim := bloodhound.DefaultLimits()
	r.Body = http.MaxBytesReader(w, r.Body, lim.MaxRequestBytes)

	userID, err := getUserID(r)
	if err != nil {
		sendAPIError(w, "Authentication required", "AUTH_REQUIRED", http.StatusUnauthorized)
		return
	}
	parsed, err := bhupload.Parse(r, lim)
	if err != nil {
		debug.Error("v1 analytics: bloodhound upload parse failed: %v", err)
		sendAPIError(w, fmt.Sprintf("Failed to process upload: %v", err), "INVALID_REQUEST", http.StatusBadRequest)
		return
	}
	clientID, err := uuid.Parse(parsed.Fields.ClientID)
	if err != nil {
		sendAPIError(w, "Invalid or missing client_id", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if len(parsed.Fields.HashlistIDs) == 0 {
		sendAPIError(w, "hashlist_ids is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if parsed.FileCount == 0 {
		sendAPIError(w, "No BloodHound file uploaded (field 'file')", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if status, code, msg := h.checkClientAccess(r, clientID); status != 0 {
		sendAPIError(w, msg, code, status)
		return
	}

	end := parsed.Fields.EndDate
	if !parsed.Fields.HasEndDate {
		end = time.Now()
	}
	start := parsed.Fields.StartDate
	if !parsed.Fields.HasStartDate {
		start = end
	}
	if end.Before(start) {
		sendAPIError(w, "end_date must be after start_date", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	refs, err := h.repo.GetHashlistAccountRefs(r.Context(), parsed.Fields.HashlistIDs)
	if err != nil {
		sendAPIError(w, "Failed to scope report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	parsed.Collector.SetInScope(bhupload.ScopeFromRefs(refs))
	dc := parsed.Collector.Resolve()

	queuePos, err := h.repo.GetNextQueuePosition(r.Context())
	if err != nil {
		sendAPIError(w, "Failed to queue report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	report := &models.AnalyticsReport{
		ID:             uuid.New(),
		ClientID:       clientID,
		UserID:         userID,
		StartDate:      start,
		EndDate:        end,
		Status:         "queued",
		CustomPatterns: parsed.Fields.CustomPatterns,
		HashlistIDs:    pq.Int64Array(parsed.Fields.HashlistIDs),
		QueuePosition:  &queuePos,
		CreatedAt:      time.Now(),
	}
	if err := h.repo.CreateWithBloodhound(r.Context(), report, dc); err != nil {
		debug.Error("v1 analytics: create report with bloodhound failed: %v", err)
		sendAPIError(w, "Failed to create report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	debug.Info("v1 analytics: created BloodHound-enriched report %s (%d in-scope accounts, paths_skipped=%v)",
		report.ID, len(dc.Accounts), dc.PathsSkipped)
	h.writeJSON(w, http.StatusCreated, report)
}

// ListReports lists reports for a client (client_id query param is required and team-scoped).
// GET /api/v1/analytics/reports?client_id=UUID
func (h *AnalyticsHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	clientIDStr := r.URL.Query().Get("client_id")
	if clientIDStr == "" {
		sendAPIError(w, "client_id query parameter is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		sendAPIError(w, "Invalid client_id", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if status, code, msg := h.checkClientAccess(r, clientID); status != 0 {
		sendAPIError(w, msg, code, status)
		return
	}
	reports, err := h.repo.GetByClient(r.Context(), clientID)
	if err != nil {
		debug.Error("v1 analytics: list reports failed: %v", err)
		sendAPIError(w, "Failed to retrieve reports", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]interface{}{"reports": reports, "total": len(reports)})
}

// GetReport returns a report (including its analytics_data metrics).
// GET /api/v1/analytics/reports/{id}
func (h *AnalyticsHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	report, done := h.loadAccessibleReport(w, r)
	if done {
		return
	}
	h.writeJSON(w, http.StatusOK, report)
}

// DeleteReport deletes a report.
// DELETE /api/v1/analytics/reports/{id}
func (h *AnalyticsHandler) DeleteReport(w http.ResponseWriter, r *http.Request) {
	report, done := h.loadAccessibleReport(w, r)
	if done {
		return
	}
	if err := h.repo.Delete(r.Context(), report.ID); err != nil {
		debug.Error("v1 analytics: delete report failed: %v", err)
		sendAPIError(w, "Failed to delete report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RetryReport re-queues a failed report.
// POST /api/v1/analytics/reports/{id}/retry
func (h *AnalyticsHandler) RetryReport(w http.ResponseWriter, r *http.Request) {
	report, done := h.loadAccessibleReport(w, r)
	if done {
		return
	}
	if report.Status != "failed" {
		sendAPIError(w, "Can only retry failed reports", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if err := h.repo.UpdateStatus(r.Context(), report.ID, "queued"); err != nil {
		debug.Error("v1 analytics: retry report failed: %v", err)
		sendAPIError(w, "Failed to retry report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	report.Status = "queued"
	report.ErrorMessage = nil
	h.writeJSON(w, http.StatusOK, report)
}

// ExportReport renders a completed report as a PDF (internal = full, external = redacted).
// GET /api/v1/analytics/reports/{id}/export?type=internal|external
func (h *AnalyticsHandler) ExportReport(w http.ResponseWriter, r *http.Request) {
	var class pdf.Classification
	switch strings.ToLower(r.URL.Query().Get("type")) {
	case "internal":
		class = pdf.Internal
	case "external":
		class = pdf.External
	default:
		sendAPIError(w, "Query parameter 'type' must be 'internal' or 'external'", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if format := strings.ToLower(r.URL.Query().Get("format")); format != "" && format != "pdf" {
		sendAPIError(w, "Query parameter 'format' must be 'pdf'", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	report, done := h.loadAccessibleReport(w, r)
	if done {
		return
	}
	if report.Status != "completed" || report.AnalyticsData == nil {
		sendAPIError(w, "Only completed reports can be exported", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	client, err := h.clientRepo.GetByID(r.Context(), report.ClientID)
	if err != nil {
		client = nil
	}
	data := report.AnalyticsData
	if class == pdf.External {
		data = pdf.BuildExternalAnalytics(report.AnalyticsData)
	}
	pdfBytes, err := h.pdfGen.Generate(report, client, data, class)
	if err != nil {
		debug.Error("v1 analytics: pdf generation failed: %v", err)
		sendAPIError(w, "Failed to generate PDF", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	h.auditExport(r, report, client, class)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportFilename(client, class, report.ID)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(pdfBytes); err != nil {
		debug.Error("v1 analytics: failed to write PDF response: %v", err)
	}
}

// GetQueueStatus reports the analytics queue depth and processing state.
// GET /api/v1/analytics/queue-status
func (h *AnalyticsHandler) GetQueueStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.queueService.GetQueueStatus(r.Context())
	if err != nil {
		sendAPIError(w, "Failed to get queue status", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

// GetHashlistsForReport lists hashlists selectable for a report. client_id is required; start_date /
// end_date are optional RFC3339 filters (default: all time up to now).
// GET /api/v1/analytics/hashlists?client_id=UUID[&start_date=&end_date=]
func (h *AnalyticsHandler) GetHashlistsForReport(w http.ResponseWriter, r *http.Request) {
	clientIDStr := r.URL.Query().Get("client_id")
	if clientIDStr == "" {
		sendAPIError(w, "client_id is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		sendAPIError(w, "Invalid client_id", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if status, code, msg := h.checkClientAccess(r, clientID); status != 0 {
		sendAPIError(w, msg, code, status)
		return
	}
	start := time.Unix(0, 0)
	end := time.Now()
	if s := r.URL.Query().Get("start_date"); s != "" {
		if start, err = time.Parse(time.RFC3339, s); err != nil {
			sendAPIError(w, "Invalid start_date format (use RFC3339)", "VALIDATION_ERROR", http.StatusBadRequest)
			return
		}
	}
	if e := r.URL.Query().Get("end_date"); e != "" {
		if end, err = time.Parse(time.RFC3339, e); err != nil {
			sendAPIError(w, "Invalid end_date format (use RFC3339)", "VALIDATION_ERROR", http.StatusBadRequest)
			return
		}
	}
	summaries, err := h.repo.GetHashlistSummariesByClientAndDateRange(r.Context(), clientID, start, end)
	if err != nil {
		debug.Error("v1 analytics: get hashlists failed: %v", err)
		sendAPIError(w, "Failed to retrieve hashlists", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []models.HashlistSummary{}
	}
	h.writeJSON(w, http.StatusOK, summaries)
}

// loadAccessibleReport parses {id}, loads the report, and enforces team access. It writes the error
// response and returns done=true when the caller should stop.
func (h *AnalyticsHandler) loadAccessibleReport(w http.ResponseWriter, r *http.Request) (*models.AnalyticsReport, bool) {
	reportID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		sendAPIError(w, "Invalid report ID", "VALIDATION_ERROR", http.StatusBadRequest)
		return nil, true
	}
	report, err := h.repo.GetByID(r.Context(), reportID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			sendAPIError(w, "Report not found", "RESOURCE_NOT_FOUND", http.StatusNotFound)
			return nil, true
		}
		debug.Error("v1 analytics: get report failed: %v", err)
		sendAPIError(w, "Failed to retrieve report", "INTERNAL_ERROR", http.StatusInternalServerError)
		return nil, true
	}
	if status, code, msg := h.checkClientAccess(r, report.ClientID); status != 0 {
		sendAPIError(w, msg, code, status)
		return nil, true
	}
	return report, false
}

// auditExport records an audit-log entry for a User-API PDF export (metadata only, never contents).
func (h *AnalyticsHandler) auditExport(r *http.Request, report *models.AnalyticsReport, client *models.Client, class pdf.Classification) {
	severity := models.AuditSeverityInfo
	if class == pdf.Internal {
		severity = models.AuditSeverityWarning
	}
	var userIDPtr *uuid.UUID
	username, email := "", ""
	if uid, err := getUserID(r); err == nil {
		userIDPtr = &uid
		if u, uerr := h.userRepo.GetByID(r.Context(), uid); uerr == nil && u != nil {
			username = u.Username
			email = u.Email
		}
	}
	clientName := report.ClientID.String()
	if client != nil && client.Name != "" {
		clientName = client.Name
	}
	title := fmt.Sprintf("Analytics report exported via API (%s)", class)
	message := fmt.Sprintf("Exported the %s analytics PDF for client %q (report %s) via the User API.", class, clientName, report.ID)
	log := models.NewAuditLog(models.NotificationTypeAnalyticsExport, severity, title, message).
		WithSource("analytics_report", report.ID.String()).
		WithData(map[string]interface{}{
			"export_type": string(class),
			"client_id":   report.ClientID.String(),
			"api":         "v1",
		})
	if userIDPtr != nil {
		log.WithUser(*userIDPtr, username, email)
	}
	ip, ua := auth.GetClientInfo(r)
	log.WithRequestContext(ip, ua)
	if err := h.auditRepo.Create(r.Context(), log); err != nil {
		debug.Error("v1 analytics: failed to write export audit log: %v", err)
	}
}

func exportFilename(client *models.Client, class pdf.Classification, reportID uuid.UUID) string {
	name := "client"
	if client != nil && client.Name != "" {
		name = client.Name
	}
	short := reportID.String()
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("analytics_%s_%s_%s.pdf", sanitizeFilePart(name), class, short)
}

func sanitizeFilePart(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
			b.WriteRune(ch)
		case ch == ' ' || ch == '.':
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "client"
	}
	return out
}
