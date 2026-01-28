package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// AuditLogHandler handles admin audit log requests
type AuditLogHandler struct {
	auditLogRepo *repository.AuditLogRepository
}

// NewAuditLogHandler creates a new audit log handler
func NewAuditLogHandler(dbConn *sql.DB) *AuditLogHandler {
	database := &db.DB{DB: dbConn}
	return &AuditLogHandler{
		auditLogRepo: repository.NewAuditLogRepository(database),
	}
}

// GetAuditLogs returns paginated audit logs with filtering
// GET /api/admin/audit-logs
// Query params:
//   - event_type[] - filter by one or more event types
//   - user_id - filter by specific user
//   - severity - filter by severity level (info, warning, critical)
//   - start_date - filter by start date (ISO 8601)
//   - end_date - filter by end date (ISO 8601)
//   - limit - pagination limit (default 50, max 200)
//   - offset - pagination offset
func (h *AuditLogHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	params := models.AuditLogListParams{
		Limit:  50,
		Offset: 0,
	}

	// Parse event types (can have multiple)
	eventTypes := r.URL.Query()["event_type[]"]
	if len(eventTypes) == 0 {
		// Also try without [] suffix
		eventTypes = r.URL.Query()["event_type"]
	}
	for _, et := range eventTypes {
		// Handle comma-separated values
		for _, t := range strings.Split(et, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				notifType := models.NotificationType(t)
				if notifType.IsValid() {
					params.EventTypes = append(params.EventTypes, notifType)
				}
			}
		}
	}

	// Parse user_id
	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		if userID, err := uuid.Parse(userIDStr); err == nil {
			params.UserID = &userID
		} else {
			http.Error(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}
	}

	// Parse severity
	if severityStr := r.URL.Query().Get("severity"); severityStr != "" {
		severity := models.AuditLogSeverity(severityStr)
		if severity.IsValid() {
			params.Severity = &severity
		} else {
			http.Error(w, "Invalid severity value. Must be: info, warning, or critical", http.StatusBadRequest)
			return
		}
	}

	// Parse start_date
	if startDateStr := r.URL.Query().Get("start_date"); startDateStr != "" {
		startDate, err := time.Parse(time.RFC3339, startDateStr)
		if err != nil {
			// Try parsing without timezone
			startDate, err = time.Parse("2006-01-02", startDateStr)
			if err != nil {
				http.Error(w, "Invalid start_date format. Use ISO 8601 (YYYY-MM-DD or full RFC3339)", http.StatusBadRequest)
				return
			}
		}
		params.StartDate = &startDate
	}

	// Parse end_date
	if endDateStr := r.URL.Query().Get("end_date"); endDateStr != "" {
		endDate, err := time.Parse(time.RFC3339, endDateStr)
		if err != nil {
			// Try parsing without timezone
			endDate, err = time.Parse("2006-01-02", endDateStr)
			if err != nil {
				http.Error(w, "Invalid end_date format. Use ISO 8601 (YYYY-MM-DD or full RFC3339)", http.StatusBadRequest)
				return
			}
			// End of day for date-only format
			endDate = endDate.Add(24*time.Hour - time.Second)
		}
		params.EndDate = &endDate
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			http.Error(w, "Invalid limit value", http.StatusBadRequest)
			return
		}
		params.Limit = limit
	}

	// Parse offset
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			http.Error(w, "Invalid offset value", http.StatusBadRequest)
			return
		}
		params.Offset = offset
	}

	// Query audit logs
	response, err := h.auditLogRepo.List(ctx, params)
	if err != nil {
		debug.Error("Failed to list audit logs: %v", err)
		http.Error(w, "Failed to retrieve audit logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetAuditLog returns a single audit log entry by ID
// GET /api/admin/audit-logs/{id}
func (h *AuditLogHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL path
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 1 {
		http.Error(w, "Missing audit log ID", http.StatusBadRequest)
		return
	}

	idStr := pathParts[len(pathParts)-1]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid audit log ID format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	log, err := h.auditLogRepo.GetByID(ctx, id)
	if err != nil {
		debug.Error("Failed to get audit log: %v", err)
		http.Error(w, "Failed to retrieve audit log", http.StatusInternalServerError)
		return
	}

	if log == nil {
		http.Error(w, "Audit log not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(log)
}

// GetAuditableEventTypes returns the list of event types that are audited
// GET /api/admin/audit-logs/event-types
func (h *AuditLogHandler) GetAuditableEventTypes(w http.ResponseWriter, r *http.Request) {
	eventTypes := []map[string]interface{}{
		// Security events
		{"type": "security_suspicious_login", "category": "security", "severity": "critical", "description": "Suspicious login activity detected"},
		{"type": "security_mfa_disabled", "category": "security", "severity": "critical", "description": "Two-factor authentication was disabled"},
		{"type": "security_password_changed", "category": "security", "severity": "critical", "description": "User password was changed"},
		// Critical system events
		{"type": "job_failed", "category": "job", "severity": "warning", "description": "A job execution failed"},
		{"type": "agent_error", "category": "agent", "severity": "warning", "description": "An agent reported an error"},
		{"type": "agent_offline", "category": "agent", "severity": "warning", "description": "An agent went offline"},
		{"type": "webhook_failure", "category": "system", "severity": "warning", "description": "A webhook delivery failed"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"event_types": eventTypes,
	})
}
