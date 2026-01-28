package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// UserWebhookRepository handles database operations for user webhooks
type UserWebhookRepository struct {
	db *db.DB
}

// NewUserWebhookRepository creates a new user webhook repository
func NewUserWebhookRepository(db *db.DB) *UserWebhookRepository {
	return &UserWebhookRepository{db: db}
}

// Create creates a new user webhook
func (r *UserWebhookRepository) Create(ctx context.Context, webhook *models.UserWebhook) error {
	query := `
		INSERT INTO user_webhooks (
			id, user_id, name, url, secret, is_active, notification_types,
			custom_headers, retry_count, timeout_seconds, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`

	if webhook.ID == uuid.Nil {
		webhook.ID = uuid.New()
	}
	now := time.Now()
	if webhook.CreatedAt.IsZero() {
		webhook.CreatedAt = now
	}
	webhook.UpdatedAt = now

	err := r.db.QueryRowContext(ctx, query,
		webhook.ID,
		webhook.UserID,
		webhook.Name,
		webhook.URL,
		webhook.Secret,
		webhook.IsActive,
		pq.Array(webhook.NotificationTypes),
		webhook.CustomHeaders,
		webhook.RetryCount,
		webhook.TimeoutSeconds,
		webhook.CreatedAt,
		webhook.UpdatedAt,
	).Scan(&webhook.ID)

	if err != nil {
		return fmt.Errorf("failed to create webhook: %w", err)
	}

	return nil
}

// GetByID retrieves a webhook by ID
func (r *UserWebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.UserWebhook, error) {
	webhook := &models.UserWebhook{}
	var secret sql.NullString
	var lastTriggeredAt, lastSuccessAt sql.NullTime
	var lastError sql.NullString

	query := `
		SELECT id, user_id, name, url, secret, is_active, notification_types,
		       custom_headers, retry_count, timeout_seconds, last_triggered_at,
		       last_success_at, last_error, total_sent, total_failed, created_at, updated_at
		FROM user_webhooks
		WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&webhook.ID,
		&webhook.UserID,
		&webhook.Name,
		&webhook.URL,
		&secret,
		&webhook.IsActive,
		pq.Array(&webhook.NotificationTypes),
		&webhook.CustomHeaders,
		&webhook.RetryCount,
		&webhook.TimeoutSeconds,
		&lastTriggeredAt,
		&lastSuccessAt,
		&lastError,
		&webhook.TotalSent,
		&webhook.TotalFailed,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}

	// Handle nullable fields
	if secret.Valid {
		webhook.Secret = &secret.String
	}
	if lastTriggeredAt.Valid {
		webhook.LastTriggeredAt = &lastTriggeredAt.Time
	}
	if lastSuccessAt.Valid {
		webhook.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastError.Valid {
		webhook.LastError = &lastError.String
	}

	return webhook, nil
}

// GetByUserID retrieves all webhooks for a user
func (r *UserWebhookRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*models.UserWebhook, error) {
	query := `
		SELECT id, user_id, name, url, secret, is_active, notification_types,
		       custom_headers, retry_count, timeout_seconds, last_triggered_at,
		       last_success_at, last_error, total_sent, total_failed, created_at, updated_at
		FROM user_webhooks
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []*models.UserWebhook
	for rows.Next() {
		webhook, err := r.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhooks: %w", err)
	}

	return webhooks, nil
}

// GetActiveByUserID retrieves only active webhooks for a user
func (r *UserWebhookRepository) GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]*models.UserWebhook, error) {
	query := `
		SELECT id, user_id, name, url, secret, is_active, notification_types,
		       custom_headers, retry_count, timeout_seconds, last_triggered_at,
		       last_success_at, last_error, total_sent, total_failed, created_at, updated_at
		FROM user_webhooks
		WHERE user_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active user webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []*models.UserWebhook
	for rows.Next() {
		webhook, err := r.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhooks: %w", err)
	}

	return webhooks, nil
}

// Update updates a webhook
func (r *UserWebhookRepository) Update(ctx context.Context, webhook *models.UserWebhook) error {
	webhook.UpdatedAt = time.Now()

	query := `
		UPDATE user_webhooks SET
			name = $1,
			url = $2,
			secret = $3,
			is_active = $4,
			notification_types = $5,
			custom_headers = $6,
			retry_count = $7,
			timeout_seconds = $8,
			updated_at = $9
		WHERE id = $10 AND user_id = $11
	`

	result, err := r.db.ExecContext(ctx, query,
		webhook.Name,
		webhook.URL,
		webhook.Secret,
		webhook.IsActive,
		pq.Array(webhook.NotificationTypes),
		webhook.CustomHeaders,
		webhook.RetryCount,
		webhook.TimeoutSeconds,
		webhook.UpdatedAt,
		webhook.ID,
		webhook.UserID,
	)
	if err != nil {
		return fmt.Errorf("failed to update webhook: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("webhook not found or not owned by user")
	}

	return nil
}

// Delete deletes a webhook
func (r *UserWebhookRepository) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM user_webhooks WHERE id = $1 AND user_id = $2`

	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete webhook: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("webhook not found or not owned by user")
	}

	return nil
}

// UpdateStats updates the webhook statistics after a delivery attempt
func (r *UserWebhookRepository) UpdateStats(ctx context.Context, id uuid.UUID, success bool, errorMsg *string) error {
	now := time.Now()

	var query string
	var args []interface{}

	if success {
		query = `
			UPDATE user_webhooks SET
				last_triggered_at = $1,
				last_success_at = $2,
				last_error = NULL,
				total_sent = total_sent + 1,
				updated_at = $3
			WHERE id = $4
		`
		args = []interface{}{now, now, now, id}
	} else {
		query = `
			UPDATE user_webhooks SET
				last_triggered_at = $1,
				last_error = $2,
				total_failed = total_failed + 1,
				updated_at = $3
			WHERE id = $4
		`
		args = []interface{}{now, errorMsg, now, id}
	}

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update webhook stats: %w", err)
	}

	return nil
}

// GetAllWebhooksAdmin retrieves all webhooks for admin view (includes user info)
func (r *UserWebhookRepository) GetAllWebhooksAdmin(ctx context.Context) ([]*models.UserWebhookAdmin, error) {
	query := `
		SELECT w.id, w.user_id, w.name, w.url, w.is_active, w.notification_types,
		       w.custom_headers, w.retry_count, w.timeout_seconds, w.last_triggered_at,
		       w.last_success_at, w.last_error, w.total_sent, w.total_failed,
		       w.created_at, w.updated_at, u.username, u.email
		FROM user_webhooks w
		JOIN users u ON w.user_id = u.id
		ORDER BY w.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []*models.UserWebhookAdmin
	for rows.Next() {
		webhook := &models.UserWebhookAdmin{}
		var lastTriggeredAt, lastSuccessAt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&webhook.ID,
			&webhook.UserID,
			&webhook.Name,
			&webhook.URL,
			&webhook.IsActive,
			pq.Array(&webhook.NotificationTypes),
			&webhook.CustomHeaders,
			&webhook.RetryCount,
			&webhook.TimeoutSeconds,
			&lastTriggeredAt,
			&lastSuccessAt,
			&lastError,
			&webhook.TotalSent,
			&webhook.TotalFailed,
			&webhook.CreatedAt,
			&webhook.UpdatedAt,
			&webhook.Username,
			&webhook.Email,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan webhook: %w", err)
		}

		if lastTriggeredAt.Valid {
			webhook.LastTriggeredAt = &lastTriggeredAt.Time
		}
		if lastSuccessAt.Valid {
			webhook.LastSuccessAt = &lastSuccessAt.Time
		}
		if lastError.Valid {
			webhook.LastError = &lastError.String
		}

		webhooks = append(webhooks, webhook)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhooks: %w", err)
	}

	return webhooks, nil
}

// CountByUserID returns the number of webhooks for a user
func (r *UserWebhookRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (total int, active int, err error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE is_active = true) as active
		FROM user_webhooks
		WHERE user_id = $1
	`

	err = r.db.QueryRowContext(ctx, query, userID).Scan(&total, &active)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count webhooks: %w", err)
	}

	return total, active, nil
}

// scanWebhook is a helper function to scan a webhook row
func (r *UserWebhookRepository) scanWebhook(rows *sql.Rows) (*models.UserWebhook, error) {
	webhook := &models.UserWebhook{}
	var secret sql.NullString
	var lastTriggeredAt, lastSuccessAt sql.NullTime
	var lastError sql.NullString

	err := rows.Scan(
		&webhook.ID,
		&webhook.UserID,
		&webhook.Name,
		&webhook.URL,
		&secret,
		&webhook.IsActive,
		pq.Array(&webhook.NotificationTypes),
		&webhook.CustomHeaders,
		&webhook.RetryCount,
		&webhook.TimeoutSeconds,
		&lastTriggeredAt,
		&lastSuccessAt,
		&lastError,
		&webhook.TotalSent,
		&webhook.TotalFailed,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan webhook: %w", err)
	}

	// Handle nullable fields
	if secret.Valid {
		webhook.Secret = &secret.String
	}
	if lastTriggeredAt.Valid {
		webhook.LastTriggeredAt = &lastTriggeredAt.Time
	}
	if lastSuccessAt.Valid {
		webhook.LastSuccessAt = &lastSuccessAt.Time
	}
	if lastError.Valid {
		webhook.LastError = &lastError.String
	}

	return webhook, nil
}
