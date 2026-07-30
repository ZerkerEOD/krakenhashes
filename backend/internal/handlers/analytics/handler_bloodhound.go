package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/analytics/bhupload"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/bloodhound"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// CreateReportWithBloodhound creates an analytics report enriched with an uploaded BloodHound
// collection dump. The dump is parsed entirely in memory; only the compact derived AD-privilege
// context is persisted (atomically with the report row), and the raw dump is discarded when this
// request returns. Re-analysis requires re-uploading the dump.
//
// POST /api/analytics/reports/bloodhound   (multipart/form-data)
//
//	client_id       (required) UUID of the client
//	hashlist_ids    (required) JSON array or CSV of hashlist IDs to analyze
//	start_date      (optional) RFC3339; defaults to end_date
//	end_date        (optional) RFC3339; defaults to now
//	custom_patterns (optional) JSON array or CSV
//	file            (required) SharpHound .zip OR a single BloodHound .json (repeatable / "files")
func (h *Handler) CreateReportWithBloodhound(w http.ResponseWriter, r *http.Request) {
	lim := bloodhound.DefaultLimits()
	r.Body = http.MaxBytesReader(w, r.Body, lim.MaxRequestBytes)

	userIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	parsed, err := bhupload.Parse(r, lim)
	if err != nil {
		debug.Error("BloodHound upload parse failed: %v", err)
		http.Error(w, fmt.Sprintf("Failed to process upload: %v", err), http.StatusBadRequest)
		return
	}

	clientID, err := uuid.Parse(parsed.Fields.ClientID)
	if err != nil {
		http.Error(w, "Invalid or missing client_id", http.StatusBadRequest)
		return
	}
	if len(parsed.Fields.HashlistIDs) == 0 {
		http.Error(w, "hashlist_ids is required", http.StatusBadRequest)
		return
	}
	if parsed.FileCount == 0 {
		http.Error(w, "No BloodHound file uploaded (field 'file')", http.StatusBadRequest)
		return
	}

	if !h.checkClientAccess(r.Context(), clientID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	start, end := resolveReportDates(parsed.Fields)
	if end.Before(start) {
		http.Error(w, "end_date must be after start_date", http.StatusBadRequest)
		return
	}

	// Seed in-scope accounts from the report's hashlists, then resolve the compact context.
	refs, err := h.repo.GetHashlistAccountRefs(r.Context(), parsed.Fields.HashlistIDs)
	if err != nil {
		debug.Error("Failed to load hashlist account refs: %v", err)
		http.Error(w, "Failed to scope report", http.StatusInternalServerError)
		return
	}
	parsed.Collector.SetInScope(bhupload.ScopeFromRefs(refs))
	dc := parsed.Collector.Resolve()

	queuePos, err := h.repo.GetNextQueuePosition(r.Context())
	if err != nil {
		debug.Error("Failed to get next queue position: %v", err)
		http.Error(w, "Failed to queue report", http.StatusInternalServerError)
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
		debug.Error("Failed to create analytics report with BloodHound context: %v", err)
		http.Error(w, "Failed to create report", http.StatusInternalServerError)
		return
	}

	debug.Info("Created BloodHound-enriched analytics report %s for client %s (%d in-scope accounts, paths_skipped=%v)",
		report.ID, report.ClientID, len(dc.Accounts), dc.PathsSkipped)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(report)
}

// resolveReportDates applies defaults: end defaults to now, start defaults to end. Hashlist-scoped
// reports do not use the date range for selection, so these are stored only for display.
func resolveReportDates(f bhupload.Fields) (time.Time, time.Time) {
	end := f.EndDate
	if !f.HasEndDate {
		end = time.Now()
	}
	start := f.StartDate
	if !f.HasStartDate {
		start = end
	}
	return start, end
}
