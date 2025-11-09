package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// AgentDeviceRepository handles database operations for agent devices
type AgentDeviceRepository struct {
	db *db.DB
}

// NewAgentDeviceRepository creates a new agent device repository
func NewAgentDeviceRepository(db *db.DB) *AgentDeviceRepository {
	return &AgentDeviceRepository{db: db}
}

// GetByAgentID retrieves all devices for a specific agent
func (r *AgentDeviceRepository) GetByAgentID(agentID int) ([]models.AgentDevice, error) {
	query := `
		SELECT id, agent_id, device_id, device_name, device_type, enabled,
		       runtime_options, selected_runtime, created_at, updated_at
		FROM agent_devices
		WHERE agent_id = $1
		ORDER BY device_id`

	rows, err := r.db.Query(query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices for agent %d: %w", agentID, err)
	}
	defer rows.Close()

	var devices []models.AgentDevice
	for rows.Next() {
		var device models.AgentDevice
		var selectedRuntime sql.NullString
		err := rows.Scan(
			&device.ID,
			&device.AgentID,
			&device.DeviceID,
			&device.DeviceName,
			&device.DeviceType,
			&device.Enabled,
			&device.RuntimeOptions,
			&selectedRuntime,
			&device.CreatedAt,
			&device.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan device row: %w", err)
		}
		if selectedRuntime.Valid {
			device.SelectedRuntime = selectedRuntime.String
		}
		devices = append(devices, device)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating device rows: %w", err)
	}

	return devices, nil
}

// UpsertDevices inserts or updates devices for an agent
func (r *AgentDeviceRepository) UpsertDevices(agentID int, devices []models.Device) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing devices that are not in the new list
	deviceIDs := make([]int, len(devices))
	for i, device := range devices {
		deviceIDs[i] = device.ID
	}

	query := `DELETE FROM agent_devices WHERE agent_id = $1`
	args := []interface{}{agentID}

	if len(deviceIDs) > 0 {
		query += ` AND device_id NOT IN (`
		for i := range deviceIDs {
			if i > 0 {
				query += ", "
			}
			query += fmt.Sprintf("$%d", i+2)
			args = append(args, deviceIDs[i])
		}
		query += `)`
	}

	_, err = tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete removed devices: %w", err)
	}

	// Upsert devices
	for _, device := range devices {
		upsertQuery := `
			INSERT INTO agent_devices (agent_id, device_id, device_name, device_type, enabled, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (agent_id, device_id) 
			DO UPDATE SET 
				device_name = EXCLUDED.device_name,
				device_type = EXCLUDED.device_type,
				updated_at = EXCLUDED.updated_at`

		_, err = tx.Exec(upsertQuery, agentID, device.ID, device.Name, device.Type, device.Enabled, time.Now())
		if err != nil {
			return fmt.Errorf("failed to upsert device %d: %w", device.ID, err)
		}
	}

	return tx.Commit()
}

// UpdateDeviceStatus updates the enabled status of a specific device
func (r *AgentDeviceRepository) UpdateDeviceStatus(agentID int, deviceID int, enabled bool) error {
	query := `
		UPDATE agent_devices 
		SET enabled = $1, updated_at = $2
		WHERE agent_id = $3 AND device_id = $4`

	result, err := r.db.Exec(query, enabled, time.Now(), agentID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to update device status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("device not found")
	}

	return nil
}

// GetEnabledDevicesByAgentID retrieves only enabled devices for an agent
func (r *AgentDeviceRepository) GetEnabledDevicesByAgentID(agentID int) ([]models.AgentDevice, error) {
	query := `
		SELECT id, agent_id, device_id, device_name, device_type, enabled,
		       runtime_options, selected_runtime, created_at, updated_at
		FROM agent_devices
		WHERE agent_id = $1 AND enabled = true
		ORDER BY device_id`

	rows, err := r.db.Query(query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled devices for agent %d: %w", agentID, err)
	}
	defer rows.Close()

	var devices []models.AgentDevice
	for rows.Next() {
		var device models.AgentDevice
		var selectedRuntime sql.NullString
		err := rows.Scan(
			&device.ID,
			&device.AgentID,
			&device.DeviceID,
			&device.DeviceName,
			&device.DeviceType,
			&device.Enabled,
			&device.RuntimeOptions,
			&selectedRuntime,
			&device.CreatedAt,
			&device.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan enabled device row: %w", err)
		}
		if selectedRuntime.Valid {
			device.SelectedRuntime = selectedRuntime.String
		}
		devices = append(devices, device)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled device rows: %w", err)
	}

	return devices, nil
}

// HasEnabledDevices checks if an agent has at least one enabled device
func (r *AgentDeviceRepository) HasEnabledDevices(agentID int) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*) 
		FROM agent_devices 
		WHERE agent_id = $1 AND enabled = true`

	err := r.db.QueryRow(query, agentID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count enabled devices: %w", err)
	}

	return count > 0, nil
}

// UpdateAgentDeviceDetectionStatus updates the device detection status for an agent
func (r *AgentDeviceRepository) UpdateAgentDeviceDetectionStatus(agentID int, status string, errorMsg *string) error {
	query := `
		UPDATE agents
		SET device_detection_status = $1,
		    device_detection_error = $2,
		    device_detection_at = $3,
		    updated_at = $4
		WHERE id = $5`

	var errorValue sql.NullString
	if errorMsg != nil {
		errorValue = sql.NullString{String: *errorMsg, Valid: true}
	}

	_, err := r.db.Exec(query, status, errorValue, time.Now(), time.Now(), agentID)
	if err != nil {
		return fmt.Errorf("failed to update device detection status: %w", err)
	}

	return nil
}

// UpsertPhysicalDevices inserts or updates physical devices for an agent
func (r *AgentDeviceRepository) UpsertPhysicalDevices(agentID int, devices []models.PhysicalDevice) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing devices that are not in the new list
	deviceIndices := make([]int, len(devices))
	for i, device := range devices {
		deviceIndices[i] = device.Index
	}

	query := `DELETE FROM agent_devices WHERE agent_id = $1`
	args := []interface{}{agentID}

	if len(deviceIndices) > 0 {
		query += ` AND device_id NOT IN (`
		for i := range deviceIndices {
			if i > 0 {
				query += ", "
			}
			query += fmt.Sprintf("$%d", i+2)
			args = append(args, deviceIndices[i])
		}
		query += `)`
	}

	_, err = tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete removed devices: %w", err)
	}

	// Upsert physical devices
	for _, device := range devices {
		// Convert RuntimeOptions to models.RuntimeOptions
		runtimeOpts := make(models.RuntimeOptions, len(device.RuntimeOptions))
		for i, opt := range device.RuntimeOptions {
			runtimeOpts[i] = models.RuntimeOption{
				Backend:     opt.Backend,
				DeviceID:    opt.DeviceID,
				Processors:  opt.Processors,
				Clock:       opt.Clock,
				MemoryTotal: opt.MemoryTotal,
				MemoryFree:  opt.MemoryFree,
				PCIAddress:  opt.PCIAddress,
			}
		}

		upsertQuery := `
			INSERT INTO agent_devices (
				agent_id, device_id, device_name, device_type, enabled,
				runtime_options, selected_runtime, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (agent_id, device_id)
			DO UPDATE SET
				device_name = EXCLUDED.device_name,
				device_type = EXCLUDED.device_type,
				runtime_options = EXCLUDED.runtime_options,
				selected_runtime = EXCLUDED.selected_runtime,
				updated_at = EXCLUDED.updated_at`

		_, err = tx.Exec(
			upsertQuery,
			agentID,
			device.Index,
			device.Name,
			device.Type,
			device.Enabled,
			runtimeOpts,
			device.SelectedRuntime,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to upsert physical device %d: %w", device.Index, err)
		}
	}

	return tx.Commit()
}

// UpdateDeviceRuntime updates the selected runtime for a specific device
func (r *AgentDeviceRepository) UpdateDeviceRuntime(agentID int, deviceID int, runtime string) error {
	query := `
		UPDATE agent_devices
		SET selected_runtime = $1, updated_at = $2
		WHERE agent_id = $3 AND device_id = $4`

	result, err := r.db.Exec(query, runtime, time.Now(), agentID, deviceID)
	if err != nil {
		return fmt.Errorf("failed to update device runtime: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("device not found")
	}

	return nil
}
