package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZerkerEOD/hashdom-backend/internal/db"
	"github.com/ZerkerEOD/hashdom-backend/internal/db/queries"
	"github.com/ZerkerEOD/hashdom-backend/internal/models"
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
	if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
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

	// Delete agent metrics
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_metrics WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent metrics: %w", err)
	}

	// Delete agent team associations
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_teams WHERE agent_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent team associations: %w", err)
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
		if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

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
	if err := json.Unmarshal(metadataJSON, &agent.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	agent.CreatedBy = &createdByUser
	return agent, nil
}

// UpdateAPIKeyLastUsed updates the last used timestamp for an API key
func (r *AgentRepository) UpdateAPIKeyLastUsed(ctx context.Context, apiKey string, lastUsed time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agents 
		SET api_key_last_used = $2
		WHERE api_key = $1`,
		apiKey, lastUsed)

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