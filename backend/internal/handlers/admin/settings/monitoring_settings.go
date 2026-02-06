package settings

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
)

// MonitoringSettingsHandler handles monitoring and metrics settings for admin
type MonitoringSettingsHandler struct {
	systemSettingsRepo *repository.SystemSettingsRepository
}

// NewMonitoringSettingsHandler creates a new monitoring settings handler
func NewMonitoringSettingsHandler(systemSettingsRepo *repository.SystemSettingsRepository) *MonitoringSettingsHandler {
	return &MonitoringSettingsHandler{
		systemSettingsRepo: systemSettingsRepo,
	}
}

// MonitoringSettings represents all monitoring and metrics related settings
type MonitoringSettings struct {
	// Retention periods for cascading aggregation
	MetricsRetentionRealtimeDays int `json:"metrics_retention_realtime_days"`
	MetricsRetentionDailyDays    int `json:"metrics_retention_daily_days"`
	MetricsRetentionWeeklyDays   int `json:"metrics_retention_weekly_days"`

	// Aggregation configuration
	EnableAggregation   bool   `json:"enable_aggregation"`
	AggregationInterval string `json:"aggregation_interval"`
}

// GetMonitoringSettings returns all monitoring settings
func (h *MonitoringSettingsHandler) GetMonitoringSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Getting monitoring settings", nil)

	// Define all setting keys we need to retrieve
	settingKeys := []string{
		"metrics_retention_realtime_days",
		"metrics_retention_daily_days",
		"metrics_retention_weekly_days",
		"enable_aggregation",
		"aggregation_interval",
	}

	settings := MonitoringSettings{
		// Set defaults in case settings don't exist
		MetricsRetentionRealtimeDays: 7,    // 7 days of real-time data
		MetricsRetentionDailyDays:    30,   // 30 days of daily aggregates
		MetricsRetentionWeeklyDays:   365,  // 1 year of weekly aggregates
		EnableAggregation:             true,
		AggregationInterval:           "daily",
	}

	// Retrieve each setting
	for _, key := range settingKeys {
		setting, err := h.systemSettingsRepo.GetSetting(ctx, key)
		if err != nil {
			debug.Log("Setting not found, using default", map[string]interface{}{
				"key":   key,
				"error": err.Error(),
			})
			continue
		}

		// Parse and assign values based on key
		if setting.Value != nil {
			switch key {
			case "metrics_retention_realtime_days":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MetricsRetentionRealtimeDays = val
				}
			case "metrics_retention_daily_days":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MetricsRetentionDailyDays = val
				}
			case "metrics_retention_weekly_days":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MetricsRetentionWeeklyDays = val
				}
			case "enable_aggregation":
				settings.EnableAggregation = *setting.Value == "true"
			case "aggregation_interval":
				settings.AggregationInterval = *setting.Value
			}
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, settings)
}

// UpdateMonitoringSettings updates monitoring settings
func (h *MonitoringSettingsHandler) UpdateMonitoringSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Updating monitoring settings", nil)

	var settings MonitoringSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate retention periods
	if settings.MetricsRetentionRealtimeDays < 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "Real-time retention days cannot be negative")
		return
	}
	if settings.MetricsRetentionDailyDays < 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "Daily retention days cannot be negative")
		return
	}
	if settings.MetricsRetentionWeeklyDays < 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, "Weekly retention days cannot be negative")
		return
	}

	// Validate aggregation interval
	validIntervals := map[string]bool{"hourly": true, "daily": true, "weekly": true}
	if !validIntervals[settings.AggregationInterval] {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid aggregation interval. Must be hourly, daily, or weekly")
		return
	}

	// Update each setting
	updates := map[string]string{
		"metrics_retention_realtime_days": strconv.Itoa(settings.MetricsRetentionRealtimeDays),
		"metrics_retention_daily_days":    strconv.Itoa(settings.MetricsRetentionDailyDays),
		"metrics_retention_weekly_days":   strconv.Itoa(settings.MetricsRetentionWeeklyDays),
		"enable_aggregation":               strconv.FormatBool(settings.EnableAggregation),
		"aggregation_interval":             settings.AggregationInterval,
	}

	var failedKeys []string
	failedErrors := make(map[string]string)

	for key, value := range updates {
		if err := h.systemSettingsRepo.SetSetting(ctx, key, &value); err != nil {
			debug.Error("Failed to update setting %s: %v", key, err)
			failedKeys = append(failedKeys, key)
			failedErrors[key] = err.Error()
		}
	}

	if len(failedKeys) > 0 {
		debug.Warning("Partial settings update: %d of %d settings failed", len(failedKeys), len(updates))
		httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"message":     "Some settings failed to update",
			"failed_keys": failedKeys,
			"errors":      failedErrors,
		})
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Monitoring settings updated successfully",
	})
}