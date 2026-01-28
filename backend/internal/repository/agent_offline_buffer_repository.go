package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// AgentOfflineBufferRepository handles database operations for agent offline buffering
type AgentOfflineBufferRepository struct {
	db *db.DB
}

// NewAgentOfflineBufferRepository creates a new agent offline buffer repository
func NewAgentOfflineBufferRepository(db *db.DB) *AgentOfflineBufferRepository {
	return &AgentOfflineBufferRepository{db: db}
}

// Create creates a new offline buffer entry
func (r *AgentOfflineBufferRepository) Create(ctx context.Context, buffer *models.AgentOfflineBuffer) error {
	query := `
		INSERT INTO agent_offline_buffer (
			id, agent_id, disconnected_at, notification_due_at,
			notification_sent, reconnected, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	if buffer.ID == uuid.Nil {
		buffer.ID = uuid.New()
	}
	if buffer.CreatedAt.IsZero() {
		buffer.CreatedAt = time.Now()
	}

	err := r.db.QueryRowContext(ctx, query,
		buffer.ID,
		buffer.AgentID,
		buffer.DisconnectedAt,
		buffer.NotificationDueAt,
		buffer.NotificationSent,
		buffer.Reconnected,
		buffer.CreatedAt,
	).Scan(&buffer.ID)

	if err != nil {
		return fmt.Errorf("failed to create offline buffer: %w", err)
	}

	return nil
}

// GetPendingDue returns all buffer entries that are due for notification
func (r *AgentOfflineBufferRepository) GetPendingDue(ctx context.Context, now time.Time) ([]*models.AgentOfflineBuffer, error) {
	query := `
		SELECT id, agent_id, disconnected_at, notification_due_at,
		       notification_sent, notification_sent_at, reconnected, reconnected_at, created_at
		FROM agent_offline_buffer
		WHERE notification_sent = false
		  AND reconnected = false
		  AND notification_due_at <= $1
		ORDER BY notification_due_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending due buffers: %w", err)
	}
	defer rows.Close()

	var buffers []*models.AgentOfflineBuffer
	for rows.Next() {
		buffer, err := r.scanBuffer(rows)
		if err != nil {
			return nil, err
		}
		buffers = append(buffers, buffer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating buffers: %w", err)
	}

	return buffers, nil
}

// MarkAsReconnected marks an agent's pending offline notification as reconnected (cancels it)
func (r *AgentOfflineBufferRepository) MarkAsReconnected(ctx context.Context, agentID int, reconnectedAt time.Time) error {
	query := `
		UPDATE agent_offline_buffer
		SET reconnected = true, reconnected_at = $1
		WHERE agent_id = $2
		  AND notification_sent = false
		  AND reconnected = false
	`

	_, err := r.db.ExecContext(ctx, query, reconnectedAt, agentID)
	if err != nil {
		return fmt.Errorf("failed to mark as reconnected: %w", err)
	}

	return nil
}

// MarkAsSent marks an offline notification as sent
func (r *AgentOfflineBufferRepository) MarkAsSent(ctx context.Context, id uuid.UUID, sentAt time.Time) error {
	query := `
		UPDATE agent_offline_buffer
		SET notification_sent = true, notification_sent_at = $1
		WHERE id = $2
	`

	result, err := r.db.ExecContext(ctx, query, sentAt, id)
	if err != nil {
		return fmt.Errorf("failed to mark as sent: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("buffer entry not found")
	}

	return nil
}

// GetByAgentID retrieves the most recent buffer entry for an agent
func (r *AgentOfflineBufferRepository) GetByAgentID(ctx context.Context, agentID int) (*models.AgentOfflineBuffer, error) {
	query := `
		SELECT id, agent_id, disconnected_at, notification_due_at,
		       notification_sent, notification_sent_at, reconnected, reconnected_at, created_at
		FROM agent_offline_buffer
		WHERE agent_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	buffer := &models.AgentOfflineBuffer{}
	var notificationSentAt, reconnectedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, agentID).Scan(
		&buffer.ID,
		&buffer.AgentID,
		&buffer.DisconnectedAt,
		&buffer.NotificationDueAt,
		&buffer.NotificationSent,
		&notificationSentAt,
		&buffer.Reconnected,
		&reconnectedAt,
		&buffer.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get buffer by agent ID: %w", err)
	}

	if notificationSentAt.Valid {
		buffer.NotificationSentAt = &notificationSentAt.Time
	}
	if reconnectedAt.Valid {
		buffer.ReconnectedAt = &reconnectedAt.Time
	}

	return buffer, nil
}

// GetPendingByAgentID returns any pending (not sent, not reconnected) buffer for an agent
func (r *AgentOfflineBufferRepository) GetPendingByAgentID(ctx context.Context, agentID int) (*models.AgentOfflineBuffer, error) {
	query := `
		SELECT id, agent_id, disconnected_at, notification_due_at,
		       notification_sent, notification_sent_at, reconnected, reconnected_at, created_at
		FROM agent_offline_buffer
		WHERE agent_id = $1
		  AND notification_sent = false
		  AND reconnected = false
		ORDER BY created_at DESC
		LIMIT 1
	`

	buffer := &models.AgentOfflineBuffer{}
	var notificationSentAt, reconnectedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, agentID).Scan(
		&buffer.ID,
		&buffer.AgentID,
		&buffer.DisconnectedAt,
		&buffer.NotificationDueAt,
		&buffer.NotificationSent,
		&notificationSentAt,
		&buffer.Reconnected,
		&reconnectedAt,
		&buffer.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending buffer: %w", err)
	}

	if notificationSentAt.Valid {
		buffer.NotificationSentAt = &notificationSentAt.Time
	}
	if reconnectedAt.Valid {
		buffer.ReconnectedAt = &reconnectedAt.Time
	}

	return buffer, nil
}

// DeleteOld deletes old buffer entries (older than specified duration)
func (r *AgentOfflineBufferRepository) DeleteOld(ctx context.Context, olderThan time.Time) (int64, error) {
	query := `
		DELETE FROM agent_offline_buffer
		WHERE created_at < $1
		  AND (notification_sent = true OR reconnected = true)
	`

	result, err := r.db.ExecContext(ctx, query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old buffers: %w", err)
	}

	return result.RowsAffected()
}

// scanBuffer is a helper function to scan a buffer row
func (r *AgentOfflineBufferRepository) scanBuffer(rows *sql.Rows) (*models.AgentOfflineBuffer, error) {
	buffer := &models.AgentOfflineBuffer{}
	var notificationSentAt, reconnectedAt sql.NullTime

	err := rows.Scan(
		&buffer.ID,
		&buffer.AgentID,
		&buffer.DisconnectedAt,
		&buffer.NotificationDueAt,
		&buffer.NotificationSent,
		&notificationSentAt,
		&buffer.Reconnected,
		&reconnectedAt,
		&buffer.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan buffer: %w", err)
	}

	if notificationSentAt.Valid {
		buffer.NotificationSentAt = &notificationSentAt.Time
	}
	if reconnectedAt.Valid {
		buffer.ReconnectedAt = &reconnectedAt.Time
	}

	return buffer, nil
}
