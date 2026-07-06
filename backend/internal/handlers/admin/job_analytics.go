package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// JobAnalyticsHandler handles job performance analytics endpoints
type JobAnalyticsHandler struct {
	service *services.JobAnalyticsService
}

// NewJobAnalyticsHandler creates a new job analytics handler
func NewJobAnalyticsHandler(service *services.JobAnalyticsService) *JobAnalyticsHandler {
	return &JobAnalyticsHandler{service: service}
}

// GetFilters returns available filter values for the UI
func (h *JobAnalyticsHandler) GetFilters(w http.ResponseWriter, r *http.Request) {
	opts, err := h.service.GetFilterOptions(r.Context())
	if err != nil {
		debug.Error("Failed to get filter options: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get filter options")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, opts)
}

// GetSummary returns aggregate statistics for filtered jobs
func (h *JobAnalyticsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	filter := parseJobAnalyticsFilter(r)

	summary, err := h.service.GetSummary(r.Context(), filter)
	if err != nil {
		debug.Error("Failed to get job analytics summary: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get summary")
		return
	}
	httputil.RespondWithJSON(w, http.StatusOK, summary)
}

// GetJobs returns paginated job list with performance metrics
func (h *JobAnalyticsHandler) GetJobs(w http.ResponseWriter, r *http.Request) {
	filter := parseJobAnalyticsFilter(r)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "started_at"
	}
	sortOrder := r.URL.Query().Get("sort_order")

	jobs, total, err := h.service.GetJobsList(r.Context(), filter, page, pageSize, sortBy, sortOrder)
	if err != nil {
		debug.Error("Failed to get job analytics list: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get jobs")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
		"pagination": map[string]interface{}{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

// GetTimeline returns hash rate time series data for charting
func (h *JobAnalyticsHandler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	filter := parseJobAnalyticsFilter(r)
	resolution := r.URL.Query().Get("resolution")
	if resolution == "" {
		resolution = "daily"
	}

	points, err := h.service.GetTimeline(r.Context(), filter, resolution)
	if err != nil {
		debug.Error("Failed to get job analytics timeline: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get timeline")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"points": points,
	})
}

// GetJobTimeline returns detailed timeline for a single job
func (h *JobAnalyticsHandler) GetJobTimeline(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID, err := uuid.Parse(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid job ID")
		return
	}

	points, tasks, err := h.service.GetJobTimeline(r.Context(), jobID)
	if err != nil {
		debug.Error("Failed to get job timeline: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get job timeline")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"metrics": points,
		"tasks":   tasks,
	})
}

// GetBenchmarkHistory returns paginated benchmark history
func (h *JobAnalyticsHandler) GetBenchmarkHistory(w http.ResponseWriter, r *http.Request) {
	var agentID *int
	if v := r.URL.Query().Get("agent_id"); v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			agentID = &id
		}
	}

	var hashType *int
	if v := r.URL.Query().Get("hash_type"); v != "" {
		if ht, err := strconv.Atoi(v); err == nil {
			hashType = &ht
		}
	}

	var attackMode *int
	if v := r.URL.Query().Get("attack_mode"); v != "" {
		if am, err := strconv.Atoi(v); err == nil {
			attackMode = &am
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if limit < 1 || limit > 100 {
		limit = 25
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	entries, total, err := h.service.GetBenchmarkHistory(r.Context(), agentID, hashType, attackMode, limit, offset)
	if err != nil {
		debug.Error("Failed to get benchmark history: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get benchmark history")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"items": entries,
		"pagination": map[string]interface{}{
			"page":        page,
			"page_size":   limit,
			"total":       total,
			"total_pages": (total + limit - 1) / limit,
		},
	})
}

// GetBenchmarkTrends returns benchmark speed trend data for charting
func (h *JobAnalyticsHandler) GetBenchmarkTrends(w http.ResponseWriter, r *http.Request) {
	agentIDStr := r.URL.Query().Get("agent_id")
	if agentIDStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid agent_id")
		return
	}

	var hashType *int
	if v := r.URL.Query().Get("hash_type"); v != "" {
		if ht, err := strconv.Atoi(v); err == nil {
			hashType = &ht
		}
	}

	var attackMode *int
	if v := r.URL.Query().Get("attack_mode"); v != "" {
		if am, err := strconv.Atoi(v); err == nil {
			attackMode = &am
		}
	}

	// Parse time range
	since := time.Now().AddDate(0, -3, 0) // Default 90 days
	if tr := r.URL.Query().Get("time_range"); tr != "" {
		switch tr {
		case "90d":
			since = time.Now().AddDate(0, -3, 0)
		case "365d":
			since = time.Now().AddDate(-1, 0, 0)
		case "all":
			since = time.Time{} // Zero time = no filter
		}
	}

	trends, err := h.service.GetBenchmarkTrends(r.Context(), agentID, hashType, attackMode, since)
	if err != nil {
		debug.Error("Failed to get benchmark trends: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get benchmark trends")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"points": trends,
	})
}

// GetSuccessRates returns success rate analytics grouped by job configuration
func (h *JobAnalyticsHandler) GetSuccessRates(w http.ResponseWriter, r *http.Request) {
	filter := parseJobAnalyticsFilter(r)

	entries, err := h.service.GetSuccessRates(r.Context(), filter)
	if err != nil {
		debug.Error("Failed to get success rates: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get success rates")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
	})
}

// parseJobAnalyticsFilter extracts filter parameters from query string
func parseJobAnalyticsFilter(r *http.Request) *repository.JobAnalyticsFilter {
	filter := &repository.JobAnalyticsFilter{}

	if v := r.URL.Query().Get("date_start"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.DateStart = &t
		}
	}
	if v := r.URL.Query().Get("date_end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.DateEnd = &t
		}
	}
	if v := r.URL.Query().Get("attack_mode"); v != "" {
		if am, err := strconv.Atoi(v); err == nil {
			filter.AttackMode = &am
		}
	}
	if v := r.URL.Query().Get("hash_type"); v != "" {
		if ht, err := strconv.Atoi(v); err == nil {
			filter.HashType = &ht
		}
	}
	if v := r.URL.Query().Get("agent_id"); v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			filter.AgentID = &id
		}
	}
	if v := r.URL.Query().Get("hashlist_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.HashlistID = &id
		}
	}
	if v := r.URL.Query().Get("min_keyspace"); v != "" {
		if ks, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.MinKeyspace = &ks
		}
	}
	if v := r.URL.Query().Get("max_keyspace"); v != "" {
		if ks, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.MaxKeyspace = &ks
		}
	}
	if statuses := r.URL.Query()["status"]; len(statuses) > 0 {
		filter.Status = statuses
	}

	return filter
}
