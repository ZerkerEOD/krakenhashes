package settings

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
)

// JobSettingsHandler handles job execution settings for admin
type JobSettingsHandler struct {
	systemSettingsRepo *repository.SystemSettingsRepository
}

// NewJobSettingsHandler creates a new job settings handler
func NewJobSettingsHandler(systemSettingsRepo *repository.SystemSettingsRepository) *JobSettingsHandler {
	return &JobSettingsHandler{
		systemSettingsRepo: systemSettingsRepo,
	}
}

// JobExecutionSettings represents all job execution related settings
type JobExecutionSettings struct {
	DefaultChunkDuration         int     `json:"default_chunk_duration"`
	ChunkFluctuationPercentage   int     `json:"chunk_fluctuation_percentage"`
	AgentHashlistRetentionHours  int     `json:"agent_hashlist_retention_hours"`
	ProgressReportingInterval    int     `json:"progress_reporting_interval"`
	MaxConcurrentJobsPerAgent    int     `json:"max_concurrent_jobs_per_agent"`
	JobInterruptionEnabled       bool    `json:"job_interruption_enabled"`
	BenchmarkCacheDurationHours  int     `json:"benchmark_cache_duration_hours"`
	EnableRealtimeCrackNotifications bool `json:"enable_realtime_crack_notifications"`
	MetricsRetentionRealtimeDays int     `json:"metrics_retention_realtime_days"`
	MetricsRetentionDailyDays    int     `json:"metrics_retention_daily_days"`
	MetricsRetentionWeeklyDays   int     `json:"metrics_retention_weekly_days"`
	JobRefreshIntervalSeconds    int     `json:"job_refresh_interval_seconds"`
	MaxChunkRetryAttempts        int     `json:"max_chunk_retry_attempts"`
	JobsPerPageDefault           int     `json:"jobs_per_page_default"`
	// Rule splitting settings
	RuleSplitEnabled   bool    `json:"rule_split_enabled"`
	RuleSplitThreshold float64 `json:"rule_split_threshold"`
	RuleSplitMinRules  int     `json:"rule_split_min_rules"`
	RuleSplitMaxChunks int     `json:"rule_split_max_chunks"`
	RuleChunkTempDir   string  `json:"rule_chunk_temp_dir"`
}

// GetJobExecutionSettings returns all job execution settings
func (h *JobSettingsHandler) GetJobExecutionSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Getting job execution settings", nil)

	// Define all setting keys we need to retrieve
	settingKeys := []string{
		"default_chunk_duration",
		"chunk_fluctuation_percentage",
		"agent_hashlist_retention_hours",
		"progress_reporting_interval",
		"max_concurrent_jobs_per_agent",
		"job_interruption_enabled",
		"benchmark_cache_duration_hours",
		"enable_realtime_crack_notifications",
		"metrics_retention_realtime_days",
		"metrics_retention_daily_days",
		"metrics_retention_weekly_days",
		"job_refresh_interval_seconds",
		"max_chunk_retry_attempts",
		"jobs_per_page_default",
		// Rule splitting settings
		"rule_split_enabled",
		"rule_split_threshold",
		"rule_split_min_rules",
		"rule_split_max_chunks",
		"rule_chunk_temp_dir",
	}

	settings := JobExecutionSettings{
		// Set defaults in case settings don't exist
		DefaultChunkDuration:         1200, // 20 minutes
		ChunkFluctuationPercentage:   20,
		AgentHashlistRetentionHours:  24,
		ProgressReportingInterval:    5,
		MaxConcurrentJobsPerAgent:    1,
		JobInterruptionEnabled:       true,
		BenchmarkCacheDurationHours:  168, // 7 days
		EnableRealtimeCrackNotifications: true,
		MetricsRetentionRealtimeDays: 7,
		MetricsRetentionDailyDays:    30,
		MetricsRetentionWeeklyDays:   365,
		JobRefreshIntervalSeconds:    5,
		MaxChunkRetryAttempts:        3,
		JobsPerPageDefault:           25,
		// Rule splitting defaults
		RuleSplitEnabled:   true,
		RuleSplitThreshold: 2.0,
		RuleSplitMinRules:  100,
		RuleSplitMaxChunks: 1000,
		RuleChunkTempDir:   "/data/krakenhashes/temp/rule_chunks",
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
			case "default_chunk_duration":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.DefaultChunkDuration = val
				}
			case "chunk_fluctuation_percentage":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.ChunkFluctuationPercentage = val
				}
			case "agent_hashlist_retention_hours":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.AgentHashlistRetentionHours = val
				}
			case "progress_reporting_interval":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.ProgressReportingInterval = val
				}
			case "max_concurrent_jobs_per_agent":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MaxConcurrentJobsPerAgent = val
				}
			case "job_interruption_enabled":
				settings.JobInterruptionEnabled = *setting.Value == "true"
			case "benchmark_cache_duration_hours":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.BenchmarkCacheDurationHours = val
				}
			case "enable_realtime_crack_notifications":
				settings.EnableRealtimeCrackNotifications = *setting.Value == "true"
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
			case "job_refresh_interval_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.JobRefreshIntervalSeconds = val
				}
			case "max_chunk_retry_attempts":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MaxChunkRetryAttempts = val
				}
			case "jobs_per_page_default":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.JobsPerPageDefault = val
				}
			case "rule_split_enabled":
				settings.RuleSplitEnabled = *setting.Value == "true"
			case "rule_split_threshold":
				if val, err := strconv.ParseFloat(*setting.Value, 64); err == nil {
					settings.RuleSplitThreshold = val
				}
			case "rule_split_min_rules":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.RuleSplitMinRules = val
				}
			case "rule_split_max_chunks":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.RuleSplitMaxChunks = val
				}
			case "rule_chunk_temp_dir":
				settings.RuleChunkTempDir = *setting.Value
			}
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, settings)
}

// UpdateJobExecutionSettings updates job execution settings
func (h *JobSettingsHandler) UpdateJobExecutionSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Updating job execution settings", nil)

	var settings JobExecutionSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update each setting
	updates := map[string]string{
		"default_chunk_duration":               strconv.Itoa(settings.DefaultChunkDuration),
		"chunk_fluctuation_percentage":         strconv.Itoa(settings.ChunkFluctuationPercentage),
		"agent_hashlist_retention_hours":       strconv.Itoa(settings.AgentHashlistRetentionHours),
		"progress_reporting_interval":          strconv.Itoa(settings.ProgressReportingInterval),
		"max_concurrent_jobs_per_agent":        strconv.Itoa(settings.MaxConcurrentJobsPerAgent),
		"job_interruption_enabled":             strconv.FormatBool(settings.JobInterruptionEnabled),
		"benchmark_cache_duration_hours":       strconv.Itoa(settings.BenchmarkCacheDurationHours),
		"enable_realtime_crack_notifications":  strconv.FormatBool(settings.EnableRealtimeCrackNotifications),
		"metrics_retention_realtime_days":      strconv.Itoa(settings.MetricsRetentionRealtimeDays),
		"metrics_retention_daily_days":         strconv.Itoa(settings.MetricsRetentionDailyDays),
		"metrics_retention_weekly_days":        strconv.Itoa(settings.MetricsRetentionWeeklyDays),
		"job_refresh_interval_seconds":         strconv.Itoa(settings.JobRefreshIntervalSeconds),
		"max_chunk_retry_attempts":             strconv.Itoa(settings.MaxChunkRetryAttempts),
		"jobs_per_page_default":                strconv.Itoa(settings.JobsPerPageDefault),
		// Rule splitting settings
		"rule_split_enabled":                   strconv.FormatBool(settings.RuleSplitEnabled),
		"rule_split_threshold":                 strconv.FormatFloat(settings.RuleSplitThreshold, 'f', 1, 64),
		"rule_split_min_rules":                 strconv.Itoa(settings.RuleSplitMinRules),
		"rule_split_max_chunks":                strconv.Itoa(settings.RuleSplitMaxChunks),
		"rule_chunk_temp_dir":                  settings.RuleChunkTempDir,
	}

	for key, value := range updates {
		if err := h.systemSettingsRepo.SetSetting(ctx, key, &value); err != nil {
			debug.Error("Failed to update setting %s: %v", key, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update settings")
			return
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Job execution settings updated successfully",
	})
}