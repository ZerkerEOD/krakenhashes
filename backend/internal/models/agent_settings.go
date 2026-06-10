package models

// AgentDownloadSettings represents the configuration for agent file downloads
type AgentDownloadSettings struct {
	MaxConcurrentDownloads      int `json:"max_concurrent_downloads"`
	DownloadTimeoutMinutes      int `json:"download_timeout_minutes"`
	DownloadRetryAttempts       int `json:"download_retry_attempts"`
	ProgressIntervalSeconds     int `json:"progress_interval_seconds"`
	ChunkSizeMB                 int `json:"chunk_size_mb"`
}

// GetDefaultAgentDownloadSettings returns the default download settings
func GetDefaultAgentDownloadSettings() AgentDownloadSettings {
	return AgentDownloadSettings{
		MaxConcurrentDownloads:      3,
		DownloadTimeoutMinutes:      60,
		DownloadRetryAttempts:       3,
		ProgressIntervalSeconds:     10,
		ChunkSizeMB:                 10,
	}
}

// AgentUpdateSettings represents the configuration for agent binary auto-updates.
type AgentUpdateSettings struct {
	AutoUpdateEnabled    bool `json:"agent_auto_update_enabled"`
	MaxConcurrent        int  `json:"agent_update_max_concurrent"`
	HealthTimeoutSeconds int  `json:"agent_update_health_timeout_seconds"`
	MaxAttempts          int  `json:"agent_update_max_attempts"`
}

// GetDefaultAgentUpdateSettings returns the default auto-update settings
// (mirrors migration 000163).
func GetDefaultAgentUpdateSettings() AgentUpdateSettings {
	return AgentUpdateSettings{
		AutoUpdateEnabled:    true,
		MaxConcurrent:        2,
		HealthTimeoutSeconds: 300,
		MaxAttempts:          3,
	}
}