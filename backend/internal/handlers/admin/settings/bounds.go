package settings

import (
	"fmt"
	"strconv"
	"strings"
)

/*
Server-side range checking for numeric system settings.

Until this existed, every bound in the admin settings UI lived in a React prop
and nothing on the server had an opinion about any value. Two consequences,
both of which happened:

  - `inputProps: {min, max}` on <input type="number"> is an advisory hint.
    Browsers accept a typed value outside it and only fail constraint
    validation, which the form never checked. A deployment ended up with
    keyspace_calculation_timeout_minutes = 1200 -- twenty hours -- in a field
    whose max was 60.

  - The bulk PUT /api/admin/settings/job-execution endpoint writes every key it
    knows about, unvalidated, from a decoded struct. Any API client, or a
    frontend a version behind, could put anything in any of them.

The map is intentionally partial: a key that is not listed is not checked, so
adding coverage is incremental and a new setting is never accidentally
rejected because nobody remembered to register it.

IMPORTANT: bounds are in the units the value is STORED in, which is not always
what the UI shows. default_chunk_duration is entered in minutes (1..1440) and
stored in seconds, so it appears here as 60..86400. When changing a bound,
change it in both places -- frontend/src/components/admin/JobExecutionSettings.tsx
is the other one.
*/

type settingBound struct {
	min int
	max int
}

// numericSettingBounds mirrors the min/max declared on each NumberSetting in
// JobExecutionSettings.tsx, converted to stored units where the field converts.
var numericSettingBounds = map[string]settingBound{
	// Chunking
	"default_chunk_duration":          {60, 86400}, // shown 1..1440 minutes
	"min_chunk_seconds":               {1, 300},
	"chunk_overrun_tolerance_percent": {0, 500},

	// Keyspace & benchmark
	"keyspace_calculation_timeout_minutes":    {1, 60},
	"speed_test_timeout_seconds_uncompressed": {30, 3600},
	"speed_test_timeout_seconds_compressed":   {30, 7200},
	"speed_test_min_status_updates":           {1, 20},
	"benchmark_cache_duration_hours":          {1, 8760},
	"benchmark_history_retention_days":        {1, 3650},
	"benchmark_failure_threshold":             {1, 50},
	"benchmark_hard_failure_cap":              {1, 100},
	"benchmark_blocklist_cooldown_hours":      {1, 720},
	"benchmark_storm_threshold":               {1, 100},
	"benchmark_storm_window_minutes":          {1, 1440},
	"agent_benchmark_streak_reset_minutes":    {1, 1440},
	"agent_benchmark_quarantine_streak":       {1, 200},
	"agent_benchmark_quarantine_distinct":     {1, 50},

	// Agent & task timing
	"max_concurrent_jobs_per_agent":  {1, 10},
	"task_heartbeat_timeout_seconds": {30, 3600},
	"task_startup_grace_seconds":     {30, 3600},
	"network_grace_seconds":          {0, 600},
	"reconnect_grace_period_minutes": {1, 120},
	"agent_offline_buffer_minutes":   {1, 120},
	"max_chunk_retry_attempts":       {0, 10},
	"progress_reporting_interval":    {1, 60},
	"agent_hashlist_retention_hours": {1, 8760},

	// Jobs & potfile
	"loopback_max_rounds":    {1, 100},
	"potfile_max_batch_size": {1000, 1000000},
	"potfile_batch_interval": {1, 300},

	// Keys owned by a dedicated endpoint.
	//
	// These are written by PUT /admin/settings/agent-updates,
	// /admin/settings/agent-downloads and /admin/settings/monitoring, which
	// validate and (for the first two) write their group in one transaction.
	// They are registered here as well because the generic key/value endpoint
	// accepts any key: without this, agent_update_max_concurrent could be set to
	// 9999 through the generic route while its own endpoint rejects anything
	// outside 1..10. Bounds mirror the dedicated handlers exactly --
	// agent_settings.go:119-130 -- so neither route is the lenient one.
	"agent_update_max_concurrent":              {1, 10},
	"agent_update_health_timeout_seconds":      {60, 3600},
	"agent_update_max_attempts":                {1, 10},
	"agent_max_concurrent_downloads":           {1, 10},
	"agent_download_timeout_minutes":           {1, 1440},
	"agent_download_retry_attempts":            {0, 10},
	"agent_download_progress_interval_seconds": {1, 300},
	"agent_download_chunk_size_mb":             {1, 100},

	// 0 means "keep forever" for the retention keys, so the floor is 0 and not 1.
	"metrics_retention_realtime_days": {0, 3650},
	"metrics_retention_daily_days":    {0, 3650},
	"metrics_retention_weekly_days":   {0, 3650},
}

// ValidateSettingValue checks value against the registered range for key.
//
// Returns nil for any key with no registered bound, so unregistered settings
// keep their previous write-anything behaviour rather than failing closed on a
// key nobody has characterised yet.
func ValidateSettingValue(key, value string) error {
	bound, registered := numericSettingBounds[key]
	if !registered {
		return nil
	}

	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s must be a whole number, got %q", key, value)
	}

	if n < bound.min || n > bound.max {
		return fmt.Errorf("%s must be between %d and %d, got %d", key, bound.min, bound.max, n)
	}

	return nil
}
