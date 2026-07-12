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
	clientSettingsRepo *repository.ClientSettingsRepository
}

// NewJobSettingsHandler creates a new job settings handler
func NewJobSettingsHandler(systemSettingsRepo *repository.SystemSettingsRepository, clientSettingsRepo *repository.ClientSettingsRepository) *JobSettingsHandler {
	return &JobSettingsHandler{
		systemSettingsRepo: systemSettingsRepo,
		clientSettingsRepo: clientSettingsRepo,
	}
}

// JobExecutionSettings represents all job execution related settings
type JobExecutionSettings struct {
	DefaultChunkDuration             int    `json:"default_chunk_duration"`
	AgentHashlistRetentionHours      int    `json:"agent_hashlist_retention_hours"`
	ProgressReportingInterval        int    `json:"progress_reporting_interval"`
	MaxConcurrentJobsPerAgent        int    `json:"max_concurrent_jobs_per_agent"`
	JobInterruptionEnabled           bool   `json:"job_interruption_enabled"`
	BenchmarkCacheDurationHours      int    `json:"benchmark_cache_duration_hours"`
	EnableRealtimeCrackNotifications bool   `json:"enable_realtime_crack_notifications"`
	JobRefreshIntervalSeconds        int    `json:"job_refresh_interval_seconds"`
	MaxChunkRetryAttempts            int    `json:"max_chunk_retry_attempts"`
	JobsPerPageDefault               int    `json:"jobs_per_page_default"`
	ReconnectGracePeriodMinutes      int    `json:"reconnect_grace_period_minutes"`
	// Scheduler-v2 tuning knobs
	MinChunkSeconds              int  `json:"min_chunk_seconds"`
	TaskHeartbeatTimeoutSeconds  int  `json:"task_heartbeat_timeout_seconds"`
	TaskStartupGraceSeconds      int  `json:"task_startup_grace_seconds"`
	NetworkGraceSeconds          int  `json:"network_grace_seconds"`
	ChunkOverrunGuardEnabled     bool `json:"chunk_overrun_guard_enabled"`
	ChunkOverrunTolerancePercent int  `json:"chunk_overrun_tolerance_percent"`
	// Keyspace calculation settings
	KeyspaceCalculationTimeoutMinutes int `json:"keyspace_calculation_timeout_minutes"`
	// Potfile settings
	PotfileEnabled       bool `json:"potfile_enabled"`
	PotfileMaxBatchSize  int  `json:"potfile_max_batch_size"`
	PotfileBatchInterval int  `json:"potfile_batch_interval"`
	// Client potfile settings
	ClientPotfilesEnabled                          bool `json:"client_potfiles_enabled"`
	RemoveFromGlobalPotfileOnHashlistDeleteDefault bool `json:"remove_from_global_potfile_on_hashlist_delete_default"`
	RemoveFromClientPotfileOnHashlistDeleteDefault bool `json:"remove_from_client_potfile_on_hashlist_delete_default"`
	// Benchmark history settings
	BenchmarkHistoryRetentionDays int `json:"benchmark_history_retention_days"`
	// Analytics settings
	AnalyticsDefaultDateRangeMonths int `json:"analytics_default_date_range_months"`
}

// GetJobExecutionSettings returns all job execution settings
func (h *JobSettingsHandler) GetJobExecutionSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Getting job execution settings", nil)

	// Define all setting keys we need to retrieve
	settingKeys := []string{
		"default_chunk_duration",
		"agent_hashlist_retention_hours",
		"progress_reporting_interval",
		"max_concurrent_jobs_per_agent",
		"job_interruption_enabled",
		"benchmark_cache_duration_hours",
		"enable_realtime_crack_notifications",
		"job_refresh_interval_seconds",
		"max_chunk_retry_attempts",
		"jobs_per_page_default",
		"reconnect_grace_period_minutes",
		// Scheduler-v2 tuning knobs
		"min_chunk_seconds",
		"task_heartbeat_timeout_seconds",
		"task_startup_grace_seconds",
		"network_grace_seconds",
		"chunk_overrun_guard_enabled",
		"chunk_overrun_tolerance_percent",
		// Keyspace calculation settings
		"keyspace_calculation_timeout_minutes",
		// Potfile settings
		"potfile_enabled",
		"potfile_max_batch_size",
		"potfile_batch_interval",
		// Client potfile settings
		"client_potfiles_enabled",
		"remove_from_global_potfile_on_hashlist_delete_default",
		"remove_from_client_potfile_on_hashlist_delete_default",
		// Benchmark history settings
		"benchmark_history_retention_days",
		// Analytics settings
		"analytics_default_date_range_months",
	}

	settings := JobExecutionSettings{
		// Set defaults in case settings don't exist
		DefaultChunkDuration:             1200, // 20 minutes
		AgentHashlistRetentionHours:      24,
		ProgressReportingInterval:        5,
		MaxConcurrentJobsPerAgent:        1,
		JobInterruptionEnabled:           true,
		BenchmarkCacheDurationHours:      168, // 7 days
		EnableRealtimeCrackNotifications: true,
		JobRefreshIntervalSeconds:        5,
		MaxChunkRetryAttempts:            3,
		JobsPerPageDefault:               25,
		ReconnectGracePeriodMinutes:      5, // 5 minutes default
		// Scheduler-v2 tuning defaults (mirror migrations 000149 / 000164)
		MinChunkSeconds:              5,
		TaskHeartbeatTimeoutSeconds:  120,
		TaskStartupGraceSeconds:      600,
		NetworkGraceSeconds:          30,
		ChunkOverrunGuardEnabled:     true,
		ChunkOverrunTolerancePercent: 20,
		// Keyspace calculation defaults
		KeyspaceCalculationTimeoutMinutes: 4, // 4 minutes
		// Potfile defaults
		PotfileEnabled:       true,
		PotfileMaxBatchSize:  1000,
		PotfileBatchInterval: 60,
		// Client potfile defaults
		ClientPotfilesEnabled:                          true,
		RemoveFromGlobalPotfileOnHashlistDeleteDefault: false,
		RemoveFromClientPotfileOnHashlistDeleteDefault: false,
		// Benchmark history defaults
		BenchmarkHistoryRetentionDays: 365,
		// Analytics defaults
		AnalyticsDefaultDateRangeMonths: 12,
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
			case "reconnect_grace_period_minutes":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.ReconnectGracePeriodMinutes = val
				}
			case "min_chunk_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.MinChunkSeconds = val
				}
			case "task_heartbeat_timeout_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.TaskHeartbeatTimeoutSeconds = val
				}
			case "task_startup_grace_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.TaskStartupGraceSeconds = val
				}
			case "network_grace_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.NetworkGraceSeconds = val
				}
			case "chunk_overrun_guard_enabled":
				settings.ChunkOverrunGuardEnabled = *setting.Value == "true"
			case "chunk_overrun_tolerance_percent":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.ChunkOverrunTolerancePercent = val
				}
			case "keyspace_calculation_timeout_minutes":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.KeyspaceCalculationTimeoutMinutes = val
				}
			case "potfile_enabled":
				settings.PotfileEnabled = *setting.Value == "true"
			case "potfile_max_batch_size":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.PotfileMaxBatchSize = val
				}
			case "potfile_batch_interval":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.PotfileBatchInterval = val
				}
			case "client_potfiles_enabled":
				settings.ClientPotfilesEnabled = *setting.Value == "true"
			case "remove_from_global_potfile_on_hashlist_delete_default":
				settings.RemoveFromGlobalPotfileOnHashlistDeleteDefault = *setting.Value == "true"
			case "remove_from_client_potfile_on_hashlist_delete_default":
				settings.RemoveFromClientPotfileOnHashlistDeleteDefault = *setting.Value == "true"
			case "benchmark_history_retention_days":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.BenchmarkHistoryRetentionDays = val
				}
			case "analytics_default_date_range_months":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.AnalyticsDefaultDateRangeMonths = val
				}
			}
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, settings)
}

// UserJobDefaults represents the subset of job execution settings
// that non-admin authenticated users need for job creation forms.
type UserJobDefaults struct {
	DefaultChunkDuration            int  `json:"default_chunk_duration"`
	PotfileEnabled                  bool `json:"potfile_enabled"`
	DefaultDataRetentionMonths      *int `json:"default_data_retention_months"`
	AnalyticsDefaultDateRangeMonths int  `json:"analytics_default_date_range_months"`
}

// GetJobDefaultsForUsers returns user-relevant job defaults (non-admin, read-only).
func (h *JobSettingsHandler) GetJobDefaultsForUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debug.Log("Getting job defaults for users", nil)

	defaults := UserJobDefaults{
		DefaultChunkDuration:            1200, // fallback: 20 minutes
		PotfileEnabled:                  true, // fallback: enabled
		AnalyticsDefaultDateRangeMonths: 12,   // fallback: 12 months
	}

	if setting, err := h.systemSettingsRepo.GetSetting(ctx, "default_chunk_duration"); err == nil && setting.Value != nil {
		if val, parseErr := strconv.Atoi(*setting.Value); parseErr == nil {
			defaults.DefaultChunkDuration = val
		}
	}

	if setting, err := h.systemSettingsRepo.GetSetting(ctx, "potfile_enabled"); err == nil && setting.Value != nil {
		defaults.PotfileEnabled = *setting.Value == "true"
	}

	// Fetch default data retention from client_settings
	if h.clientSettingsRepo != nil {
		if setting, err := h.clientSettingsRepo.GetSetting(ctx, "default_data_retention_months"); err == nil && setting.Value != nil {
			if val, parseErr := strconv.Atoi(*setting.Value); parseErr == nil {
				defaults.DefaultDataRetentionMonths = &val
			}
		}
	}

	// Fetch analytics default date range
	if setting, err := h.systemSettingsRepo.GetSetting(ctx, "analytics_default_date_range_months"); err == nil && setting.Value != nil {
		if val, parseErr := strconv.Atoi(*setting.Value); parseErr == nil {
			defaults.AnalyticsDefaultDateRangeMonths = val
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, defaults)
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
		"default_chunk_duration":              strconv.Itoa(settings.DefaultChunkDuration),
		"agent_hashlist_retention_hours":      strconv.Itoa(settings.AgentHashlistRetentionHours),
		"progress_reporting_interval":         strconv.Itoa(settings.ProgressReportingInterval),
		"max_concurrent_jobs_per_agent":       strconv.Itoa(settings.MaxConcurrentJobsPerAgent),
		"job_interruption_enabled":            strconv.FormatBool(settings.JobInterruptionEnabled),
		"benchmark_cache_duration_hours":      strconv.Itoa(settings.BenchmarkCacheDurationHours),
		"enable_realtime_crack_notifications": strconv.FormatBool(settings.EnableRealtimeCrackNotifications),
		"job_refresh_interval_seconds":        strconv.Itoa(settings.JobRefreshIntervalSeconds),
		"max_chunk_retry_attempts":            strconv.Itoa(settings.MaxChunkRetryAttempts),
		"jobs_per_page_default":               strconv.Itoa(settings.JobsPerPageDefault),
		"reconnect_grace_period_minutes":      strconv.Itoa(settings.ReconnectGracePeriodMinutes),
		// Scheduler-v2 tuning knobs
		"min_chunk_seconds":               strconv.Itoa(settings.MinChunkSeconds),
		"task_heartbeat_timeout_seconds":  strconv.Itoa(settings.TaskHeartbeatTimeoutSeconds),
		"task_startup_grace_seconds":      strconv.Itoa(settings.TaskStartupGraceSeconds),
		"network_grace_seconds":           strconv.Itoa(settings.NetworkGraceSeconds),
		"chunk_overrun_guard_enabled":     strconv.FormatBool(settings.ChunkOverrunGuardEnabled),
		"chunk_overrun_tolerance_percent": strconv.Itoa(settings.ChunkOverrunTolerancePercent),
		// Keyspace calculation settings
		"keyspace_calculation_timeout_minutes": strconv.Itoa(settings.KeyspaceCalculationTimeoutMinutes),
		// Potfile settings
		"potfile_enabled":        strconv.FormatBool(settings.PotfileEnabled),
		"potfile_max_batch_size": strconv.Itoa(settings.PotfileMaxBatchSize),
		"potfile_batch_interval": strconv.Itoa(settings.PotfileBatchInterval),
		// Client potfile settings
		"client_potfiles_enabled":                              strconv.FormatBool(settings.ClientPotfilesEnabled),
		"remove_from_global_potfile_on_hashlist_delete_default": strconv.FormatBool(settings.RemoveFromGlobalPotfileOnHashlistDeleteDefault),
		"remove_from_client_potfile_on_hashlist_delete_default": strconv.FormatBool(settings.RemoveFromClientPotfileOnHashlistDeleteDefault),
		// Benchmark history settings
		"benchmark_history_retention_days": strconv.Itoa(settings.BenchmarkHistoryRetentionDays),
		// Analytics settings
		"analytics_default_date_range_months": strconv.Itoa(settings.AnalyticsDefaultDateRangeMonths),
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
		"message": "Job execution settings updated successfully",
	})
}
