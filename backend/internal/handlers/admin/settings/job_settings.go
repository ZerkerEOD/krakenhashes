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
	DefaultChunkDuration             int    `json:"default_chunk_duration"`
	ChunkFluctuationPercentage       int    `json:"chunk_fluctuation_percentage"`
	AgentHashlistRetentionHours      int    `json:"agent_hashlist_retention_hours"`
	ProgressReportingInterval        int    `json:"progress_reporting_interval"`
	MaxConcurrentJobsPerAgent        int    `json:"max_concurrent_jobs_per_agent"`
	JobInterruptionEnabled           bool   `json:"job_interruption_enabled"`
	BenchmarkCacheDurationHours      int    `json:"benchmark_cache_duration_hours"`
	EnableRealtimeCrackNotifications bool   `json:"enable_realtime_crack_notifications"`
	JobRefreshIntervalSeconds        int    `json:"job_refresh_interval_seconds"`
	MaxChunkRetryAttempts            int    `json:"max_chunk_retry_attempts"`
	JobsPerPageDefault               int    `json:"jobs_per_page_default"`
	SpeedtestTimeoutSeconds          int    `json:"speedtest_timeout_seconds"`
	ReconnectGracePeriodMinutes      int    `json:"reconnect_grace_period_minutes"`
	// Rule splitting settings
	RuleSplitEnabled   bool    `json:"rule_split_enabled"`
	RuleSplitThreshold float64 `json:"rule_split_threshold"`
	RuleSplitMinRules  int     `json:"rule_split_min_rules"`
	RuleSplitMaxChunks int     `json:"rule_split_max_chunks"`
	RuleChunkTempDir   string  `json:"rule_chunk_temp_dir"`
	// Keyspace calculation settings
	KeyspaceCalculationTimeoutMinutes int `json:"keyspace_calculation_timeout_minutes"`
	// Potfile settings
	PotfileEnabled bool `json:"potfile_enabled"`
	// Client potfile settings
	ClientPotfilesEnabled                          bool `json:"client_potfiles_enabled"`
	RemoveFromGlobalPotfileOnHashlistDeleteDefault bool `json:"remove_from_global_potfile_on_hashlist_delete_default"`
	RemoveFromClientPotfileOnHashlistDeleteDefault bool `json:"remove_from_client_potfile_on_hashlist_delete_default"`
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
		"job_refresh_interval_seconds",
		"max_chunk_retry_attempts",
		"jobs_per_page_default",
		"speedtest_timeout_seconds",
		"reconnect_grace_period_minutes",
		// Rule splitting settings
		"rule_split_enabled",
		"rule_split_threshold",
		"rule_split_min_rules",
		"rule_split_max_chunks",
		"rule_chunk_temp_dir",
		// Keyspace calculation settings
		"keyspace_calculation_timeout_minutes",
		// Potfile settings
		"potfile_enabled",
		// Client potfile settings
		"client_potfiles_enabled",
		"remove_from_global_potfile_on_hashlist_delete_default",
		"remove_from_client_potfile_on_hashlist_delete_default",
	}

	settings := JobExecutionSettings{
		// Set defaults in case settings don't exist
		DefaultChunkDuration:             1200, // 20 minutes
		ChunkFluctuationPercentage:       20,
		AgentHashlistRetentionHours:      24,
		ProgressReportingInterval:        5,
		MaxConcurrentJobsPerAgent:        1,
		JobInterruptionEnabled:           true,
		BenchmarkCacheDurationHours:      168, // 7 days
		EnableRealtimeCrackNotifications: true,
		JobRefreshIntervalSeconds:        5,
		MaxChunkRetryAttempts:            3,
		JobsPerPageDefault:               25,
		SpeedtestTimeoutSeconds:          30,
		ReconnectGracePeriodMinutes:      5, // 5 minutes default
		// Rule splitting defaults
		RuleSplitEnabled:   true,
		RuleSplitThreshold: 2.0,
		RuleSplitMinRules:  100,
		RuleSplitMaxChunks: 1000,
		RuleChunkTempDir:   "/data/krakenhashes/temp/rule_chunks",
		// Keyspace calculation defaults
		KeyspaceCalculationTimeoutMinutes: 4, // 4 minutes
		// Potfile defaults
		PotfileEnabled: true,
		// Client potfile defaults
		ClientPotfilesEnabled:                          true,
		RemoveFromGlobalPotfileOnHashlistDeleteDefault: false,
		RemoveFromClientPotfileOnHashlistDeleteDefault: false,
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
			case "speedtest_timeout_seconds":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.SpeedtestTimeoutSeconds = val
				}
			case "reconnect_grace_period_minutes":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.ReconnectGracePeriodMinutes = val
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
			case "keyspace_calculation_timeout_minutes":
				if val, err := strconv.Atoi(*setting.Value); err == nil {
					settings.KeyspaceCalculationTimeoutMinutes = val
				}
			case "potfile_enabled":
				settings.PotfileEnabled = *setting.Value == "true"
			case "client_potfiles_enabled":
				settings.ClientPotfilesEnabled = *setting.Value == "true"
			case "remove_from_global_potfile_on_hashlist_delete_default":
				settings.RemoveFromGlobalPotfileOnHashlistDeleteDefault = *setting.Value == "true"
			case "remove_from_client_potfile_on_hashlist_delete_default":
				settings.RemoveFromClientPotfileOnHashlistDeleteDefault = *setting.Value == "true"
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
		"default_chunk_duration":              strconv.Itoa(settings.DefaultChunkDuration),
		"chunk_fluctuation_percentage":        strconv.Itoa(settings.ChunkFluctuationPercentage),
		"agent_hashlist_retention_hours":      strconv.Itoa(settings.AgentHashlistRetentionHours),
		"progress_reporting_interval":         strconv.Itoa(settings.ProgressReportingInterval),
		"max_concurrent_jobs_per_agent":       strconv.Itoa(settings.MaxConcurrentJobsPerAgent),
		"job_interruption_enabled":            strconv.FormatBool(settings.JobInterruptionEnabled),
		"benchmark_cache_duration_hours":      strconv.Itoa(settings.BenchmarkCacheDurationHours),
		"enable_realtime_crack_notifications": strconv.FormatBool(settings.EnableRealtimeCrackNotifications),
		"job_refresh_interval_seconds":        strconv.Itoa(settings.JobRefreshIntervalSeconds),
		"max_chunk_retry_attempts":            strconv.Itoa(settings.MaxChunkRetryAttempts),
		"jobs_per_page_default":               strconv.Itoa(settings.JobsPerPageDefault),
		"speedtest_timeout_seconds":           strconv.Itoa(settings.SpeedtestTimeoutSeconds),
		"reconnect_grace_period_minutes":      strconv.Itoa(settings.ReconnectGracePeriodMinutes),
		// Rule splitting settings
		"rule_split_enabled":    strconv.FormatBool(settings.RuleSplitEnabled),
		"rule_split_threshold":  strconv.FormatFloat(settings.RuleSplitThreshold, 'f', 1, 64),
		"rule_split_min_rules":  strconv.Itoa(settings.RuleSplitMinRules),
		"rule_split_max_chunks": strconv.Itoa(settings.RuleSplitMaxChunks),
		"rule_chunk_temp_dir":   settings.RuleChunkTempDir,
		// Keyspace calculation settings
		"keyspace_calculation_timeout_minutes": strconv.Itoa(settings.KeyspaceCalculationTimeoutMinutes),
		// Potfile settings
		"potfile_enabled": strconv.FormatBool(settings.PotfileEnabled),
		// Client potfile settings
		"client_potfiles_enabled":                              strconv.FormatBool(settings.ClientPotfilesEnabled),
		"remove_from_global_potfile_on_hashlist_delete_default": strconv.FormatBool(settings.RemoveFromGlobalPotfileOnHashlistDeleteDefault),
		"remove_from_client_potfile_on_hashlist_delete_default": strconv.FormatBool(settings.RemoveFromClientPotfileOnHashlistDeleteDefault),
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
