package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// AgentRepository handles database operations for agents
type AgentRepository struct {
	db *db.DB
}

// NewAgentRepository creates a new agent repository
func NewAgentRepository(db *db.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

// Create creates a new agent
func (r *AgentRepository) Create(ctx context.Context, agent *models.Agent) error {
	// Convert hardware to JSON
	hardwareJSON, err := json.Marshal(agent.Hardware)
	if err != nil {
		return fmt.Errorf("failed to marshal hardware: %w", err)
	}

	// Convert OS info to JSON
	osInfoJSON, err := json.Marshal(agent.OSInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal OS info: %w", err)
	}

	// Convert metadata to JSON
	metadataJSON, err := json.Marshal(agent.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = r.db.QueryRowContext(ctx, queries.CreateAgent,
		agent.Name,
		agent.Status,
		agent.LastHeartbeat,
		agent.Version,
		hardwareJSON,
		osInfoJSON,
		agent.CreatedByID,
		agent.CreatedAt,
		agent.UpdatedAt,
		agent.APIKey,
		agent.APIKeyCreatedAt,
		agent.APIKeyLastUsed,
		agent.LastError,
		metadataJSON,
		agent.OwnerID,
	).Scan(&agent.ID)

	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	return nil
}

// GetByID retrieves an agent by ID
func (r *AgentRepository) GetByID(ctx context.Context, id int) (*models.Agent, error) {
	agent := &models.Agent{}
	var hardwareJSON, osInfoJSON, metadataJSON []byte
	var createdByUser models.User
	var ownerID sql.NullString

	err := r.db.QueryRowContext(ctx, queries.GetAgentByID, id).Scan(
		&agent.ID,
		&agent.Name,
		&agent.Status,
		&agent.LastError,
		&agent.LastHeartbeat,
		&agent.Version,
		&hardwareJSON,
		&osInfoJSON,
		&agent.CreatedByID,
		&agent.CreatedAt,
		&agent.UpdatedAt,
		&agent.APIKey,
		&agent.APIKeyCreatedAt,
		&agent.APIKeyLastUsed,
		&metadataJSON,
		&ownerID,
		&agent.ExtraParameters,
		&agent.IsEnabled,
		&agent.ConsecutiveFailures,
		&agent.SchedulingEnabled,
		&agent.ScheduleTimezone,
		&agent.SyncStatus,
		&agent.SyncStartedAt,
		&agent.SyncCompletedAt,
		&agent.FilesToSync,
		&agent.FilesSynced,
		&agent.SyncError,
		&agent.BinaryVersion,
		&agent.UpdatePending,
		&agent.TargetVersion,
		&agent.UpdateStartedAt,
		&agent.UpdateAttempts,
		&agent.UpdateError,
		&agent.UpdateLastAttemptAt,
		&createdByUser.ID,
		&createdByUser.Username,
		&createdByUser.Email,
		&createdByUser.Role,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %d", id)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	// Unmarshal hardware JSON
	if err := json.Unmarshal(hardwareJSON, &agent.Hardware); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hardware: %w", err)
	}

	// Unmarshal OS info JSON
	if err := json.Unmarshal(osInfoJSON, &agent.OSInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OS info: %w", err)
	}

	// Unmarshal metadata JSON
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	} else {
		// Initialize empty map for NULL metadata
		agent.Metadata = make(map[string]string)
	}

	// Convert ownerID if not null
	if ownerID.Valid {
		ownerUUID, err := uuid.Parse(ownerID.String)
		if err == nil {
			agent.OwnerID = &ownerUUID
		}
	}

	agent.CreatedBy = &createdByUser
	return agent, nil
}

// ExistsByName checks if an agent exists with the given name
func (r *AgentRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE name = $1)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check agent existence: %w", err)
	}
	return exists, nil
}

// Update updates an agent
func (r *AgentRepository) Update(ctx context.Context, agent *models.Agent) error {
	// Convert hardware to JSON
	hardwareJSON, err := json.Marshal(agent.Hardware)
	if err != nil {
		return fmt.Errorf("failed to marshal hardware: %w", err)
	}

	// Convert OS info to JSON
	osInfoJSON, err := json.Marshal(agent.OSInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal OS info: %w", err)
	}

	// Convert metadata to JSON
	metadataJSON, err := json.Marshal(agent.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	result, err := r.db.ExecContext(ctx, queries.UpdateAgent,
		agent.ID,
		agent.Name,
		agent.Status,
		agent.LastError,
		agent.LastHeartbeat,
		agent.Version,
		hardwareJSON,
		osInfoJSON,
		agent.UpdatedAt,
		agent.APIKey,
		agent.APIKeyCreatedAt,
		agent.APIKeyLastUsed,
		metadataJSON,
		agent.SyncStatus,
		agent.SyncCompletedAt,
		agent.SyncStartedAt,
		agent.SyncError,
		agent.FilesToSync,
		agent.FilesSynced,
	)

	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %d", agent.ID)
	}

	return nil
}

// Delete deletes an agent and its related records
func (r *AgentRepository) Delete(ctx context.Context, id int) error {
	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete agent benchmarks
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_benchmarks WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent benchmarks: %w", err)
	}

	// Delete agent devices
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_devices WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent devices: %w", err)
	}

	// Delete agent hashlists
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_hashlists WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent hashlists: %w", err)
	}

	// Delete agent performance metrics
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_performance_metrics WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent performance metrics: %w", err)
	}

	// Delete agent schedules
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_schedules WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent schedules: %w", err)
	}

	// Delete agent team associations
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_teams WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent team associations: %w", err)
	}

	// Update job_tasks to remove reference to this agent (preserve task history)
	_, err = tx.ExecContext(ctx, `
		UPDATE job_tasks 
		SET agent_id = NULL 
		WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to update job tasks: %w", err)
	}

	// Update claim vouchers to remove reference to this agent
	_, err = tx.ExecContext(ctx, `
		UPDATE claim_vouchers 
		SET used_by_agent_id = NULL, used_at = NULL 
		WHERE used_by_agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to update claim vouchers: %w", err)
	}

	// Finally, delete the agent
	result, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %d", id)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// List retrieves all agents with optional filters
func (r *AgentRepository) List(ctx context.Context, filters map[string]interface{}) ([]models.Agent, error) {
	var status *string
	if s, ok := filters["status"].(string); ok {
		status = &s
	}

	rows, err := r.db.QueryContext(ctx, queries.ListAgents, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var agent models.Agent
		var hardwareJSON, osInfoJSON, metadataJSON []byte
		var createdByUser models.User
		var ownerID sql.NullString

		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.Status,
			&agent.LastError,
			&agent.LastHeartbeat,
			&agent.Version,
			&hardwareJSON,
			&osInfoJSON,
			&agent.CreatedByID,
			&agent.CreatedAt,
			&agent.UpdatedAt,
			&agent.APIKey,
			&agent.APIKeyCreatedAt,
			&agent.APIKeyLastUsed,
			&metadataJSON,
			&ownerID,
			&agent.ExtraParameters,
			&agent.IsEnabled,
			&agent.ConsecutiveFailures,
			&agent.SchedulingEnabled,
			&agent.ScheduleTimezone,
			&agent.SyncStatus,
			&agent.SyncStartedAt,
			&agent.SyncCompletedAt,
			&agent.FilesToSync,
			&agent.FilesSynced,
			&agent.SyncError,
			&agent.BinaryVersion,
			&agent.UpdatePending,
			&agent.TargetVersion,
			&agent.UpdateStartedAt,
			&agent.UpdateAttempts,
			&agent.UpdateError,
			&agent.UpdateLastAttemptAt,
			&createdByUser.ID,
			&createdByUser.Username,
			&createdByUser.Email,
			&createdByUser.Role,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}

		// Unmarshal hardware JSON
		if err := json.Unmarshal(hardwareJSON, &agent.Hardware); err != nil {
			return nil, fmt.Errorf("failed to unmarshal hardware: %w", err)
		}

		// Unmarshal OS info JSON
		if err := json.Unmarshal(osInfoJSON, &agent.OSInfo); err != nil {
			return nil, fmt.Errorf("failed to unmarshal OS info: %w", err)
		}

		// Unmarshal metadata JSON
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		} else {
			// Initialize empty map for NULL metadata
			agent.Metadata = make(map[string]string)
		}

		// Convert ownerID if not null
		if ownerID.Valid {
			ownerUUID, err := uuid.Parse(ownerID.String)
			if err == nil {
				agent.OwnerID = &ownerUUID
			}
		}

		agent.CreatedBy = &createdByUser
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// GetByOwnerID retrieves all agents owned by a specific user
func (r *AgentRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]models.Agent, error) {
	query := `
		SELECT 
			a.id, a.name, a.status, a.last_error, a.last_heartbeat, 
			a.version, a.hardware, a.os_info, a.created_by_id, a.created_at, 
			a.updated_at, a.api_key, a.api_key_created_at, a.api_key_last_used,
			a.metadata, a.owner_id, a.extra_parameters, a.is_enabled,
			a.consecutive_failures, a.scheduling_enabled, a.schedule_timezone,
			u.id, u.username, u.email, u.role
		FROM agents a
		JOIN users u ON a.created_by_id = u.id
		WHERE a.owner_id = $1 OR (a.owner_id IS NULL AND a.created_by_id = $1)
		ORDER BY a.name ASC`

	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agents by owner: %w", err)
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var agent models.Agent
		var hardwareJSON, osInfoJSON, metadataJSON []byte
		var createdByUser models.User
		var ownerIDStr sql.NullString

		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.Status,
			&agent.LastError,
			&agent.LastHeartbeat,
			&agent.Version,
			&hardwareJSON,
			&osInfoJSON,
			&agent.CreatedByID,
			&agent.CreatedAt,
			&agent.UpdatedAt,
			&agent.APIKey,
			&agent.APIKeyCreatedAt,
			&agent.APIKeyLastUsed,
			&metadataJSON,
			&ownerIDStr,
			&agent.ExtraParameters,
			&agent.IsEnabled,
			&agent.ConsecutiveFailures,
			&agent.SchedulingEnabled,
			&agent.ScheduleTimezone,
			&createdByUser.ID,
			&createdByUser.Username,
			&createdByUser.Email,
			&createdByUser.Role,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}

		// Unmarshal hardware JSON
		if err := json.Unmarshal(hardwareJSON, &agent.Hardware); err != nil {
			return nil, fmt.Errorf("failed to unmarshal hardware: %w", err)
		}

		// Unmarshal OS info JSON
		if err := json.Unmarshal(osInfoJSON, &agent.OSInfo); err != nil {
			return nil, fmt.Errorf("failed to unmarshal OS info: %w", err)
		}

		// Unmarshal metadata JSON
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		} else {
			agent.Metadata = make(map[string]string)
		}

		// Set owner ID
		agent.OwnerID = &ownerID
		agent.CreatedBy = &createdByUser
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// UpdateStatus updates an agent's status and last error
func (r *AgentRepository) UpdateStatus(ctx context.Context, id int, status string, lastError *string) error {
	var nullLastError sql.NullString
	if lastError != nil {
		nullLastError = sql.NullString{String: *lastError, Valid: true}
	}

	result, err := r.db.ExecContext(ctx, queries.UpdateAgentStatus, id, status, nullLastError)
	if err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %d", id)
	}

	return nil
}

// UpdateStatusUnlessUpdating updates an agent's status only when it is not
// currently mid auto-update. The heartbeat / periodic agent_status handlers use
// this so an agent's "active" report (sent just before it exits to update)
// cannot clobber the 'updating' marker that the scheduler and health-check
// sweeper rely on. A no-op (no error) when the agent is 'updating' or missing.
func (r *AgentRepository) UpdateStatusUnlessUpdating(ctx context.Context, id int, status string, lastError *string) error {
	var nullLastError sql.NullString
	if lastError != nil {
		nullLastError = sql.NullString{String: *lastError, Valid: true}
	}

	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET status = $2, last_error = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status <> 'updating'`,
		id, status, nullLastError)
	if err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}
	return nil
}

// UpdateHeartbeat updates an agent's heartbeat timestamp
func (r *AgentRepository) UpdateHeartbeat(ctx context.Context, id int) error {
	result, err := r.db.ExecContext(ctx, queries.UpdateAgentHeartbeat, id)
	if err != nil {
		return fmt.Errorf("failed to update agent heartbeat: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %d", id)
	}

	return nil
}

// SaveMetrics saves agent metrics
func (r *AgentRepository) SaveMetrics(ctx context.Context, metrics *models.AgentMetrics) error {
	// Convert GPU metrics to JSON
	gpuMetricsJSON, err := json.Marshal(metrics.GPUMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal GPU metrics: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agent_metrics (
			agent_id, cpu_usage, memory_usage, gpu_metrics, timestamp
		) VALUES ($1, $2, $3, $4, $5)`,
		metrics.AgentID,
		metrics.CPUUsage,
		metrics.MemoryUsage,
		gpuMetricsJSON,
		metrics.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to save agent metrics: %w", err)
	}

	return nil
}

// GetByAPIKey retrieves an agent by API key
func (r *AgentRepository) GetByAPIKey(ctx context.Context, apiKey string) (*models.Agent, error) {
	agent := &models.Agent{}
	var hardwareJSON, osInfoJSON, metadataJSON []byte
	var createdByUser models.User
	var ownerID sql.NullString

	err := r.db.QueryRowContext(ctx, queries.GetAgentByAPIKey, apiKey).Scan(
		&agent.ID,
		&agent.Name,
		&agent.Status,
		&agent.LastError,
		&agent.LastHeartbeat,
		&agent.Version,
		&hardwareJSON,
		&osInfoJSON,
		&agent.CreatedByID,
		&agent.CreatedAt,
		&agent.UpdatedAt,
		&agent.APIKey,
		&agent.APIKeyCreatedAt,
		&agent.APIKeyLastUsed,
		&metadataJSON,
		&ownerID,
		&agent.ExtraParameters,
		&agent.IsEnabled,
		&agent.ConsecutiveFailures,
		&agent.SchedulingEnabled,
		&agent.ScheduleTimezone,
		&agent.BinaryVersion,
		&createdByUser.ID,
		&createdByUser.Username,
		&createdByUser.Email,
		&createdByUser.Role,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found with API key")
	} else if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	// Unmarshal hardware JSON
	if err := json.Unmarshal(hardwareJSON, &agent.Hardware); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hardware: %w", err)
	}

	// Unmarshal OS info JSON
	if err := json.Unmarshal(osInfoJSON, &agent.OSInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OS info: %w", err)
	}

	// Unmarshal metadata JSON
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	} else {
		// Initialize empty map for NULL metadata
		agent.Metadata = make(map[string]string)
	}

	// Convert ownerID if not null
	if ownerID.Valid {
		ownerUUID, err := uuid.Parse(ownerID.String)
		if err == nil {
			agent.OwnerID = &ownerUUID
		}
	}

	agent.CreatedBy = &createdByUser
	return agent, nil
}

// UpdateAPIKeyLastUsed updates the last used timestamp for an API key
func (r *AgentRepository) UpdateAPIKeyLastUsed(ctx context.Context, apiKey string, lastUsed time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agents
		SET api_key_last_used = $1
		WHERE api_key = $2`,
		lastUsed, apiKey,
	)
	if err != nil {
		return fmt.Errorf("failed to update API key last used: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found with API key")
	}

	return nil
}

// GetDB returns the underlying database connection
func (r *AgentRepository) GetDB() *sql.DB {
	return r.db.DB
}

// UpdateAgentSettings updates agent settings including is_enabled, owner, extra parameters, and binary version
func (r *AgentRepository) UpdateAgentSettings(ctx context.Context, agentID int, isEnabled bool, ownerID *string, extraParameters string, binaryVersion string) error {
	query := `
		UPDATE agents
		SET is_enabled = $2,
		    owner_id = $3,
		    extra_parameters = $4,
		    binary_version = $5,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, agentID, isEnabled, ownerID, extraParameters, binaryVersion)
	if err != nil {
		return fmt.Errorf("failed to update agent settings: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateVersion updates the version field for an agent
func (r *AgentRepository) UpdateVersion(ctx context.Context, id int, version string) error {
	query := `UPDATE agents SET version = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, version)
	if err != nil {
		return fmt.Errorf("failed to update agent version: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found")
	}

	return nil
}

// UpdateMetadata updates an agent's metadata field
func (r *AgentRepository) UpdateMetadata(ctx context.Context, agentID int, metadata map[string]string) error {
	// Convert metadata to JSON
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	result, err := r.db.ExecContext(ctx, queries.UpdateAgentMetadata, agentID, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to update agent metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("agent not found")
	}

	return nil
}

// UpdateAPIKeyLastUsedByID updates the api_key_last_used timestamp for an agent by ID
func (r *AgentRepository) UpdateAPIKeyLastUsedByID(ctx context.Context, agentID int, lastUsed time.Time) error {
	query := `
		UPDATE agents
		SET api_key_last_used = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, agentID, lastUsed)
	if err != nil {
		return fmt.Errorf("failed to update agent API key last used: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("agent not found")
	}

	return nil
}

// UpdateOSInfo updates an agent's OS information
func (r *AgentRepository) UpdateOSInfo(ctx context.Context, agentID int, osInfo map[string]interface{}) error {
	// Convert OS info to JSON
	osInfoJSON, err := json.Marshal(osInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal OS info: %w", err)
	}

	query := `
		UPDATE agents
		SET os_info = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, agentID, osInfoJSON)
	if err != nil {
		return fmt.Errorf("failed to update agent OS info: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// TeamAgent is a lightweight agent representation for team listings
type TeamAgent struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Version       string     `json:"version"`
	OwnerID       *uuid.UUID `json:"owner_id,omitempty"`
	OwnerUsername string     `json:"owner_username"`
}

// GetAgentsForTeam retrieves all agents accessible to a specific team
// via the 4-path resolution (explicit teams, owner inheritance, system agents, ownerless agents)
func (r *AgentRepository) GetAgentsForTeam(ctx context.Context, teamID uuid.UUID) ([]TeamAgent, error) {
	rows, err := r.db.QueryContext(ctx, queries.GetAgentsForTeam, teamID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get agents for team: %w", err)
	}
	defer rows.Close()

	var agents []TeamAgent
	for rows.Next() {
		var agent TeamAgent
		var ownerIDStr sql.NullString

		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.Status,
			&agent.Version,
			&ownerIDStr,
			&agent.OwnerUsername,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team agent: %w", err)
		}

		if ownerIDStr.Valid {
			ownerUUID, parseErr := uuid.Parse(ownerIDStr.String)
			if parseErr == nil {
				agent.OwnerID = &ownerUUID
			}
		}

		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team agents: %w", err)
	}

	return agents, nil
}

// SetDisconnectGrace sets or clears the agent's disconnect_grace_expires_at
// timestamp. The scheduler-v2 sweeper uses it to decide when a hard-
// disconnected agent's tasks should be evicted and their keyspace ranges
// returned to the gap pool.
//
// Pass a non-nil expiresAt when an agent's WebSocket connection drops
// (typically NOW() + network_grace_seconds). Pass nil to clear the grace
// after the agent reconnects. This method is additive — it does not
// touch any other agent fields, so it composes cleanly with the existing
// disconnect / reconnect handling.
//
// Returns nil if the agent does not exist (treated as a no-op; the
// caller's context — agent reconnect or disconnect — usually means the
// row exists, but a missing row should not break the surrounding flow).
func (r *AgentRepository) SetDisconnectGrace(ctx context.Context, agentID int, expiresAt *time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET disconnect_grace_expires_at = $1 WHERE id = $2`,
		expiresAt, agentID)
	if err != nil {
		return fmt.Errorf("failed to update agent disconnect_grace_expires_at: %w", err)
	}
	return nil
}

// MarkUpdatePending flags a version-stale agent that is currently busy so it
// gets updated the moment it goes idle. The status='active' guard avoids
// stomping a disabled/error/already-updating agent. Idempotent.
func (r *AgentRepository) MarkUpdatePending(ctx context.Context, agentID int, targetVersion string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET update_pending = true, target_version = $2 WHERE id = $1 AND status = 'active'`,
		agentID, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to mark agent update pending: %w", err)
	}
	return nil
}

// ClearUpdatePending clears a stale update_pending flag (e.g. the agent is no
// longer version-stale, or auto-update was disabled). Leaves status untouched.
func (r *AgentRepository) ClearUpdatePending(ctx context.Context, agentID int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agents SET update_pending = false, target_version = NULL WHERE id = $1 AND status <> 'updating'`,
		agentID)
	if err != nil {
		return fmt.Errorf("failed to clear agent update pending: %w", err)
	}
	return nil
}

// BeginUpdate atomically transitions an idle, active agent into the 'updating'
// state. The NOT EXISTS clause is the race guard: if the scheduler dispatched a
// task to this agent in the same window, the in-flight job_tasks row makes the
// update fail (0 rows affected) and the caller must abort sending the update
// command — honoring "never interrupt a running job". Returns true only when
// exactly one row was updated (the caller now owns the in-flight update).
func (r *AgentRepository) BeginUpdate(ctx context.Context, agentID int, targetVersion string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
			status = 'updating',
			update_pending = false,
			target_version = $2,
			update_started_at = NOW(),
			update_attempts = update_attempts + 1,
			update_last_attempt_at = NOW(),
			update_error = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND status = 'active'
		  AND NOT EXISTS (
			  SELECT 1 FROM job_tasks t
			  WHERE t.agent_id = $1
				AND t.status IN ('assigned', 'running')
				AND t.scheduling_unit_id IS NOT NULL
		  )`,
		agentID, targetVersion)
	if err != nil {
		return false, fmt.Errorf("failed to begin agent update: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rows == 1, nil
}

// CompleteUpdate clears the 'updating' state once the agent has reconnected on
// the target version. Returns to 'active' and resets the attempt counter so a
// future version bump starts from a clean slate.
func (r *AgentRepository) CompleteUpdate(ctx context.Context, agentID int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
			status = 'active',
			update_pending = false,
			target_version = NULL,
			update_started_at = NULL,
			update_attempts = 0,
			update_last_attempt_at = NULL,
			update_error = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'updating'`,
		agentID)
	if err != nil {
		return fmt.Errorf("failed to complete agent update: %w", err)
	}
	return nil
}

// FailUpdate moves an agent out of 'updating' into 'error' with a message,
// preserving update_attempts (for the give-up policy) and target_version (for
// the UI). It sets update_pending so the sweeper's promote pass resumes the
// retry once the agent is active again (the promote pass gives up on its own
// when attempts are exhausted). Called by the health-timeout sweeper or on a
// send failure.
func (r *AgentRepository) FailUpdate(ctx context.Context, agentID int, errMsg string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
			status = 'error',
			update_pending = true,
			update_started_at = NULL,
			update_error = $2,
			last_error = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`,
		agentID, errMsg)
	if err != nil {
		return fmt.Errorf("failed to fail agent update: %w", err)
	}
	return nil
}

// ResetUpdateState clears all update tracking for an agent (manual retry or
// post-success cleanup). Used by the admin retry-update action.
func (r *AgentRepository) ResetUpdateState(ctx context.Context, agentID int, pending bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
			update_pending = $2,
			update_attempts = 0,
			update_error = NULL,
			update_started_at = NULL,
			update_last_attempt_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`,
		agentID, pending)
	if err != nil {
		return fmt.Errorf("failed to reset agent update state: %w", err)
	}
	return nil
}

// ResetStrandedUpdates clears agents left in the 'updating' state by a backend
// crash/restart: their WebSocket is gone so the update can't proceed. They drop
// to 'inactive' (the scheduler ignores them until they reconnect) but keep
// update_pending/update_attempts so the update resumes once they reconnect.
func (r *AgentRepository) ResetStrandedUpdates(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents SET
			status = 'inactive',
			update_pending = true,
			update_started_at = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE status = 'updating'`)
	if err != nil {
		return fmt.Errorf("failed to reset stranded updates: %w", err)
	}
	return nil
}

// CountUpdating returns how many agents are currently in the 'updating' state,
// used to enforce agent_update_max_concurrent.
func (r *AgentRepository) CountUpdating(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agents WHERE status = 'updating'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count updating agents: %w", err)
	}
	return count, nil
}

// ListTimedOutUpdates returns the IDs of agents that have been 'updating' for
// longer than timeoutSeconds without reconnecting on the target version — the
// health-check sweeper fails these.
func (r *AgentRepository) ListTimedOutUpdates(ctx context.Context, timeoutSeconds int) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM agents
		WHERE status = 'updating'
		  AND update_started_at IS NOT NULL
		  AND update_started_at < NOW() - ($1 || ' seconds')::INTERVAL`,
		timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to list timed-out updates: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan timed-out update id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListIdleActiveAgentVersions returns the version of every active, enabled
// agent that has no in-flight scheduler-v2 task — the sweeper checks each
// against the expected version to decide whether to update it. Returning the
// version inline lets the sweeper pre-filter stale agents cheaply before
// loading the full row. Mirrors the scheduler's idle predicate.
func (r *AgentRepository) ListIdleActiveAgentVersions(ctx context.Context) (map[int]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.version, '') FROM agents a
		WHERE a.status = 'active'
		  AND a.is_enabled = true
		  AND NOT EXISTS (
			  SELECT 1 FROM job_tasks t
			  WHERE t.agent_id = a.id
				AND t.status IN ('assigned', 'running')
				AND t.scheduling_unit_id IS NOT NULL
		  )`)
	if err != nil {
		return nil, fmt.Errorf("failed to list idle active agents: %w", err)
	}
	defer rows.Close()

	out := make(map[int]string)
	for rows.Next() {
		var id int
		var version string
		if err := rows.Scan(&id, &version); err != nil {
			return nil, fmt.Errorf("failed to scan idle active agent: %w", err)
		}
		out[id] = version
	}
	return out, rows.Err()
}

// ListIdleUpdatePending returns IDs of agents flagged update_pending that are
// now idle and schedulable — the sweeper's "promote pending" pass transitions
// these into 'updating'. Mirrors the scheduler's idle predicate so a pending
// agent is only promoted when it genuinely has no in-flight scheduler-v2 work.
func (r *AgentRepository) ListIdleUpdatePending(ctx context.Context) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id FROM agents a
		WHERE a.update_pending = true
		  AND a.status = 'active'
		  AND a.is_enabled = true
		  AND NOT EXISTS (
			  SELECT 1 FROM job_tasks t
			  WHERE t.agent_id = a.id
				AND t.status IN ('assigned', 'running')
				AND t.scheduling_unit_id IS NOT NULL
		  )`)
	if err != nil {
		return nil, fmt.Errorf("failed to list idle update-pending agents: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan idle update-pending id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
