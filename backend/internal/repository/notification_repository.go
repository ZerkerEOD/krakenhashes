package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// NotificationRepository handles database operations for notifications
type NotificationRepository struct {
	db *db.DB
}

// NewNotificationRepository creates a new notification repository
func NewNotificationRepository(db *db.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Create creates a new notification
func (r *NotificationRepository) Create(ctx context.Context, notification *models.Notification) error {
	query := `
		INSERT INTO notifications (
			id, user_id, notification_type, title, message, data,
			in_app_read, email_sent, webhook_sent,
			source_type, source_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`

	err := r.db.QueryRowContext(ctx, query,
		notification.ID,
		notification.UserID,
		notification.NotificationType,
		notification.Title,
		notification.Message,
		notification.Data,
		notification.InAppRead,
		notification.EmailSent,
		notification.WebhookSent,
		notification.SourceType,
		notification.SourceID,
		notification.CreatedAt,
	).Scan(&notification.ID)

	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	return nil
}

// GetByID retrieves a notification by ID
func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Notification, error) {
	notification := &models.Notification{}
	var inAppReadAt, emailSentAt, webhookSentAt sql.NullTime
	var emailError, webhookError, sourceType, sourceID sql.NullString

	query := `
		SELECT id, user_id, notification_type, title, message, data,
		       in_app_read, in_app_read_at, email_sent, email_sent_at, email_error,
		       webhook_sent, webhook_sent_at, webhook_error, source_type, source_id, created_at
		FROM notifications
		WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&notification.ID,
		&notification.UserID,
		&notification.NotificationType,
		&notification.Title,
		&notification.Message,
		&notification.Data,
		&notification.InAppRead,
		&inAppReadAt,
		&notification.EmailSent,
		&emailSentAt,
		&emailError,
		&notification.WebhookSent,
		&webhookSentAt,
		&webhookError,
		&sourceType,
		&sourceID,
		&notification.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get notification: %w", err)
	}

	// Handle nullable fields
	if inAppReadAt.Valid {
		notification.InAppReadAt = &inAppReadAt.Time
	}
	if emailSentAt.Valid {
		notification.EmailSentAt = &emailSentAt.Time
	}
	if emailError.Valid {
		notification.EmailError = &emailError.String
	}
	if webhookSentAt.Valid {
		notification.WebhookSentAt = &webhookSentAt.Time
	}
	if webhookError.Valid {
		notification.WebhookError = &webhookError.String
	}
	if sourceType.Valid {
		notification.SourceType = sourceType.String
	}
	if sourceID.Valid {
		notification.SourceID = sourceID.String
	}

	return notification, nil
}

// List retrieves notifications with pagination and filtering
func (r *NotificationRepository) List(ctx context.Context, params models.NotificationListParams) (*models.NotificationListResponse, error) {
	var whereConditions []string
	var args []interface{}
	argIndex := 1

	whereConditions = append(whereConditions, fmt.Sprintf("user_id = $%d", argIndex))
	args = append(args, params.UserID)
	argIndex++

	// Filter by category
	if params.Category != "" {
		types := getTypesForCategory(params.Category)
		if len(types) > 0 {
			placeholders := make([]string, len(types))
			for i, t := range types {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, t)
				argIndex++
			}
			whereConditions = append(whereConditions, fmt.Sprintf("notification_type IN (%s)", strings.Join(placeholders, ", ")))
		}
	}

	// Filter by specific type
	if params.Type != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("notification_type = $%d", argIndex))
		args = append(args, params.Type)
		argIndex++
	}

	// Filter by read status
	if params.ReadOnly != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("in_app_read = $%d", argIndex))
		args = append(args, *params.ReadOnly)
		argIndex++
	}

	whereClause := strings.Join(whereConditions, " AND ")

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM notifications WHERE %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count notifications: %w", err)
	}

	// Set default limit
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Get notifications
	query := fmt.Sprintf(`
		SELECT id, user_id, notification_type, title, message, data,
		       in_app_read, in_app_read_at, email_sent, email_sent_at, email_error,
		       webhook_sent, webhook_sent_at, webhook_error, source_type, source_id, created_at
		FROM notifications
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, limit, params.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list notifications: %w", err)
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		var inAppReadAt, emailSentAt, webhookSentAt sql.NullTime
		var emailError, webhookError, sourceType, sourceID sql.NullString

		err := rows.Scan(
			&n.ID,
			&n.UserID,
			&n.NotificationType,
			&n.Title,
			&n.Message,
			&n.Data,
			&n.InAppRead,
			&inAppReadAt,
			&n.EmailSent,
			&emailSentAt,
			&emailError,
			&n.WebhookSent,
			&webhookSentAt,
			&webhookError,
			&sourceType,
			&sourceID,
			&n.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}

		// Handle nullable fields
		if inAppReadAt.Valid {
			n.InAppReadAt = &inAppReadAt.Time
		}
		if emailSentAt.Valid {
			n.EmailSentAt = &emailSentAt.Time
		}
		if emailError.Valid {
			n.EmailError = &emailError.String
		}
		if webhookSentAt.Valid {
			n.WebhookSentAt = &webhookSentAt.Time
		}
		if webhookError.Valid {
			n.WebhookError = &webhookError.String
		}
		if sourceType.Valid {
			n.SourceType = sourceType.String
		}
		if sourceID.Valid {
			n.SourceID = sourceID.String
		}

		notifications = append(notifications, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notifications: %w", err)
	}

	return &models.NotificationListResponse{
		Notifications: notifications,
		Total:         total,
		Limit:         limit,
		Offset:        params.Offset,
	}, nil
}

// GetUnreadCount returns the number of unread notifications for a user
func (r *NotificationRepository) GetUnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND in_app_read = false`

	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}

	return count, nil
}

// MarkAsRead marks a notification as read
func (r *NotificationRepository) MarkAsRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE notifications
		SET in_app_read = true, in_app_read_at = $1
		WHERE id = $2 AND user_id = $3
	`

	result, err := r.db.ExecContext(ctx, query, now, id, userID)
	if err != nil {
		return fmt.Errorf("failed to mark notification as read: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("notification not found or not owned by user")
	}

	return nil
}

// MarkAllAsRead marks all notifications as read for a user
func (r *NotificationRepository) MarkAllAsRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	now := time.Now()
	query := `
		UPDATE notifications
		SET in_app_read = true, in_app_read_at = $1
		WHERE user_id = $2 AND in_app_read = false
	`

	result, err := r.db.ExecContext(ctx, query, now, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to mark all as read: %w", err)
	}

	return result.RowsAffected()
}

// Delete deletes a notification
func (r *NotificationRepository) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM notifications WHERE id = $1 AND user_id = $2`

	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete notification: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("notification not found or not owned by user")
	}

	return nil
}

// DeleteMany deletes multiple notifications
func (r *NotificationRepository) DeleteMany(ctx context.Context, ids []uuid.UUID, userID uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = userID

	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		DELETE FROM notifications
		WHERE user_id = $1 AND id IN (%s)
	`, strings.Join(placeholders, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete notifications: %w", err)
	}

	return result.RowsAffected()
}

// UpdateEmailStatus updates the email delivery status
func (r *NotificationRepository) UpdateEmailStatus(ctx context.Context, id uuid.UUID, sent bool, sentAt *time.Time, errorMsg *string) error {
	query := `
		UPDATE notifications
		SET email_sent = $1, email_sent_at = $2, email_error = $3
		WHERE id = $4
	`

	_, err := r.db.ExecContext(ctx, query, sent, sentAt, errorMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update email status: %w", err)
	}

	return nil
}

// UpdateWebhookStatus updates the webhook delivery status
func (r *NotificationRepository) UpdateWebhookStatus(ctx context.Context, id uuid.UUID, sent bool, sentAt *time.Time, errorMsg *string) error {
	query := `
		UPDATE notifications
		SET webhook_sent = $1, webhook_sent_at = $2, webhook_error = $3
		WHERE id = $4
	`

	_, err := r.db.ExecContext(ctx, query, sent, sentAt, errorMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update webhook status: %w", err)
	}

	return nil
}

// GetRecent returns the most recent notifications for a user (for dropdown)
func (r *NotificationRepository) GetRecent(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	if limit <= 0 {
		limit = 5
	}

	query := `
		SELECT id, user_id, notification_type, title, message, data,
		       in_app_read, in_app_read_at, source_type, source_id, created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent notifications: %w", err)
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		var inAppReadAt sql.NullTime
		var sourceType, sourceID sql.NullString

		err := rows.Scan(
			&n.ID,
			&n.UserID,
			&n.NotificationType,
			&n.Title,
			&n.Message,
			&n.Data,
			&n.InAppRead,
			&inAppReadAt,
			&sourceType,
			&sourceID,
			&n.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}

		if inAppReadAt.Valid {
			n.InAppReadAt = &inAppReadAt.Time
		}
		if sourceType.Valid {
			n.SourceType = sourceType.String
		}
		if sourceID.Valid {
			n.SourceID = sourceID.String
		}

		notifications = append(notifications, n)
	}

	return notifications, nil
}

// ExistsBySourceAndType checks if a notification already exists for a given source and type
// This is used for deduplication to prevent duplicate notifications (e.g., on server restart)
func (r *NotificationRepository) ExistsBySourceAndType(ctx context.Context, sourceType, sourceID string, notificationType models.NotificationType) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM notifications
			WHERE source_type = $1 AND source_id = $2 AND notification_type = $3
		)
	`

	err := r.db.QueryRowContext(ctx, query, sourceType, sourceID, notificationType).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check notification existence: %w", err)
	}

	return exists, nil
}

// getTypesForCategory returns notification types for a category
func getTypesForCategory(category string) []models.NotificationType {
	switch category {
	case "job":
		return []models.NotificationType{
			models.NotificationTypeJobStarted,
			models.NotificationTypeJobCompleted,
			models.NotificationTypeJobFailed,
			models.NotificationTypeFirstCrack,
			models.NotificationTypeTaskCompletedWithCracks,
		}
	case "agent":
		return []models.NotificationType{
			models.NotificationTypeAgentOffline,
			models.NotificationTypeAgentError,
		}
	case "security":
		return []models.NotificationType{
			models.NotificationTypeSecuritySuspiciousLogin,
			models.NotificationTypeSecurityMFADisabled,
			models.NotificationTypeSecurityPasswordChanged,
		}
	case "system":
		return []models.NotificationType{
			models.NotificationTypeWebhookFailure,
		}
	default:
		return nil
	}
}
