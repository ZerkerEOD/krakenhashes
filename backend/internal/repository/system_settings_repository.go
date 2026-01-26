package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SystemSettingsRepository handles database operations for system settings.
type SystemSettingsRepository struct {
	db *db.DB
}

// NewSystemSettingsRepository creates a new instance of SystemSettingsRepository.
func NewSystemSettingsRepository(database *db.DB) *SystemSettingsRepository {
	return &SystemSettingsRepository{db: database}
}

// GetSetting retrieves a specific setting by its key.
func (r *SystemSettingsRepository) GetSetting(ctx context.Context, key string) (*models.SystemSetting, error) {
	query := `
		SELECT key, value, description, data_type, updated_at
		FROM system_settings
		WHERE key = $1`

	row := r.db.QueryRowContext(ctx, query, key)
	var setting models.SystemSetting
	err := row.Scan(
		&setting.Key,
		&setting.Value,
		&setting.Description,
		&setting.DataType,
		&setting.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("system setting with key '%s' not found: %w", key, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get system setting by key '%s': %w", key, err)
	}
	return &setting, nil
}

// SetSetting updates a specific setting's value by its key.
func (r *SystemSettingsRepository) SetSetting(ctx context.Context, key string, value *string) error {
	now := time.Now()
	query := `
		UPDATE system_settings
		SET value = $1, updated_at = $2
		WHERE key = $3`

	result, err := r.db.ExecContext(ctx, query, value, now, key)
	if err != nil {
		return fmt.Errorf("failed to set system setting '%s': %w", key, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after updating system setting %s: %v", key, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("system setting with key '%s' not found for update: %w", key, ErrNotFound)
	}

	return nil
}

// GetAllSettings retrieves all system settings.
func (r *SystemSettingsRepository) GetAllSettings(ctx context.Context) ([]models.SystemSetting, error) {
	query := `
		SELECT key, value, description, data_type, updated_at
		FROM system_settings
		ORDER BY key ASC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list system settings: %w", err)
	}
	defer rows.Close()

	var settings []models.SystemSetting
	for rows.Next() {
		var setting models.SystemSetting
		if err := rows.Scan(
			&setting.Key,
			&setting.Value,
			&setting.Description,
			&setting.DataType,
			&setting.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan system setting row: %w", err)
		}
		settings = append(settings, setting)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating system setting rows: %w", err)
	}

	return settings, nil
}

// GetMaxJobPriority retrieves the maximum job priority setting as an integer.
func (r *SystemSettingsRepository) GetMaxJobPriority(ctx context.Context) (int, error) {
	setting, err := r.GetSetting(ctx, "max_job_priority")
	if err != nil {
		// Return default value if setting not found
		if err == ErrNotFound {
			return 1000, nil // Default max priority
		}
		return 0, err
	}

	if setting.Value == nil {
		return 1000, nil // Default if value is null
	}

	maxPriority, err := strconv.Atoi(*setting.Value)
	if err != nil {
		debug.Error("Invalid max_job_priority value in database: %s", *setting.Value)
		return 1000, nil // Return default on invalid value
	}

	return maxPriority, nil
}

// SetMaxJobPriority updates the maximum job priority setting.
func (r *SystemSettingsRepository) SetMaxJobPriority(ctx context.Context, maxPriority int) error {
	value := strconv.Itoa(maxPriority)
	return r.SetSetting(ctx, "max_job_priority", &value)
}

// UpdateSetting updates a specific setting's value by its key (alias for SetSetting with string value).
func (r *SystemSettingsRepository) UpdateSetting(ctx context.Context, key string, value string) error {
	return r.SetSetting(ctx, key, &value)
}

// GetAgentDownloadSettings retrieves all agent download settings
func (r *SystemSettingsRepository) GetAgentDownloadSettings(ctx context.Context) (*models.AgentDownloadSettings, error) {
	query := `
		SELECT key, value
		FROM system_settings
		WHERE key LIKE 'agent_%'`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent download settings: %w", err)
	}
	defer rows.Close()

	settings := models.GetDefaultAgentDownloadSettings()
	for rows.Next() {
		var key string
		var value *string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan agent setting row: %w", err)
		}

		if value == nil {
			continue
		}

		switch key {
		case "agent_max_concurrent_downloads":
			if val, err := strconv.Atoi(*value); err == nil {
				settings.MaxConcurrentDownloads = val
			}
		case "agent_download_timeout_minutes":
			if val, err := strconv.Atoi(*value); err == nil {
				settings.DownloadTimeoutMinutes = val
			}
		case "agent_download_retry_attempts":
			if val, err := strconv.Atoi(*value); err == nil {
				settings.DownloadRetryAttempts = val
			}
		case "agent_download_progress_interval_seconds":
			if val, err := strconv.Atoi(*value); err == nil {
				settings.ProgressIntervalSeconds = val
			}
		case "agent_download_chunk_size_mb":
			if val, err := strconv.Atoi(*value); err == nil {
				settings.ChunkSizeMB = val
			}
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agent setting rows: %w", err)
	}

	return &settings, nil
}

// GetHashlistBulkBatchSize retrieves the bulk batch size setting for hashlist uploads.
func (r *SystemSettingsRepository) GetHashlistBulkBatchSize(ctx context.Context) (int, error) {
	setting, err := r.GetSetting(ctx, "hashlist_bulk_batch_size")
	if err != nil {
		// Return default value if setting not found
		if err == ErrNotFound {
			return 100000, nil
		}
		return 0, err
	}

	if setting.Value == nil {
		return 100000, nil // Default if value is null
	}

	batchSize, err := strconv.Atoi(*setting.Value)
	if err != nil {
		debug.Error("Invalid hashlist_bulk_batch_size value in database: %s", *setting.Value)
		return 100000, nil // Return default on invalid value
	}

	return batchSize, nil
}

// UpdateAgentDownloadSettings updates all agent download settings
func (r *SystemSettingsRepository) UpdateAgentDownloadSettings(ctx context.Context, settings *models.AgentDownloadSettings) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update each setting
	settingMap := map[string]string{
		"agent_max_concurrent_downloads":      strconv.Itoa(settings.MaxConcurrentDownloads),
		"agent_download_timeout_minutes":      strconv.Itoa(settings.DownloadTimeoutMinutes),
		"agent_download_retry_attempts":       strconv.Itoa(settings.DownloadRetryAttempts),
		"agent_download_progress_interval_seconds": strconv.Itoa(settings.ProgressIntervalSeconds),
		"agent_download_chunk_size_mb":        strconv.Itoa(settings.ChunkSizeMB),
	}

	now := time.Now()
	query := `
		UPDATE system_settings
		SET value = $1, updated_at = $2
		WHERE key = $3`

	for key, value := range settingMap {
		if _, err := tx.ExecContext(ctx, query, value, now, key); err != nil {
			return fmt.Errorf("failed to update setting %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
