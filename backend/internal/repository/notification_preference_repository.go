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

// NotificationPreferenceRepository handles database operations for user notification preferences
type NotificationPreferenceRepository struct {
	db *db.DB
}

// NewNotificationPreferenceRepository creates a new notification preference repository
func NewNotificationPreferenceRepository(db *db.DB) *NotificationPreferenceRepository {
	return &NotificationPreferenceRepository{db: db}
}

// GetByUserAndType retrieves a user's preference for a specific notification type
func (r *NotificationPreferenceRepository) GetByUserAndType(ctx context.Context, userID uuid.UUID, notificationType models.NotificationType) (*models.UserNotificationPreference, error) {
	pref := &models.UserNotificationPreference{}

	query := `
		SELECT id, user_id, notification_type, in_app_enabled, email_enabled, webhook_enabled,
		       settings, created_at, updated_at
		FROM user_notification_preferences
		WHERE user_id = $1 AND notification_type = $2
	`

	err := r.db.QueryRowContext(ctx, query, userID, notificationType).Scan(
		&pref.ID,
		&pref.UserID,
		&pref.NotificationType,
		&pref.InAppEnabled,
		&pref.EmailEnabled,
		&pref.WebhookEnabled,
		&pref.Settings,
		&pref.CreatedAt,
		&pref.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Return default preferences if not explicitly set
		return r.getDefaultPreference(userID, notificationType), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get notification preference: %w", err)
	}

	return pref, nil
}

// GetAllByUser retrieves all preferences for a user
func (r *NotificationPreferenceRepository) GetAllByUser(ctx context.Context, userID uuid.UUID) (map[models.NotificationType]*models.UserNotificationPreference, error) {
	query := `
		SELECT id, user_id, notification_type, in_app_enabled, email_enabled, webhook_enabled,
		       settings, created_at, updated_at
		FROM user_notification_preferences
		WHERE user_id = $1
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user preferences: %w", err)
	}
	defer rows.Close()

	// Start with defaults for all types
	result := make(map[models.NotificationType]*models.UserNotificationPreference)
	for _, t := range models.AllNotificationTypes() {
		result[t] = r.getDefaultPreference(userID, t)
	}

	// Override with stored preferences
	for rows.Next() {
		pref := &models.UserNotificationPreference{}
		err := rows.Scan(
			&pref.ID,
			&pref.UserID,
			&pref.NotificationType,
			&pref.InAppEnabled,
			&pref.EmailEnabled,
			&pref.WebhookEnabled,
			&pref.Settings,
			&pref.CreatedAt,
			&pref.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan preference: %w", err)
		}
		result[pref.NotificationType] = pref
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating preferences: %w", err)
	}

	return result, nil
}

// Upsert creates or updates a notification preference
func (r *NotificationPreferenceRepository) Upsert(ctx context.Context, pref *models.UserNotificationPreference) error {
	query := `
		INSERT INTO user_notification_preferences (
			id, user_id, notification_type, in_app_enabled, email_enabled, webhook_enabled,
			settings, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, notification_type) DO UPDATE SET
			in_app_enabled = EXCLUDED.in_app_enabled,
			email_enabled = EXCLUDED.email_enabled,
			webhook_enabled = EXCLUDED.webhook_enabled,
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
		RETURNING id
	`

	if pref.ID == uuid.Nil {
		pref.ID = uuid.New()
	}
	now := time.Now()
	if pref.CreatedAt.IsZero() {
		pref.CreatedAt = now
	}
	pref.UpdatedAt = now

	err := r.db.QueryRowContext(ctx, query,
		pref.ID,
		pref.UserID,
		pref.NotificationType,
		pref.InAppEnabled,
		pref.EmailEnabled,
		pref.WebhookEnabled,
		pref.Settings,
		pref.CreatedAt,
		pref.UpdatedAt,
	).Scan(&pref.ID)

	if err != nil {
		return fmt.Errorf("failed to upsert notification preference: %w", err)
	}

	return nil
}

// UpsertMany creates or updates multiple preferences at once
func (r *NotificationPreferenceRepository) UpsertMany(ctx context.Context, prefs []*models.UserNotificationPreference) error {
	if len(prefs) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO user_notification_preferences (
			id, user_id, notification_type, in_app_enabled, email_enabled, webhook_enabled,
			settings, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, notification_type) DO UPDATE SET
			in_app_enabled = EXCLUDED.in_app_enabled,
			email_enabled = EXCLUDED.email_enabled,
			webhook_enabled = EXCLUDED.webhook_enabled,
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, pref := range prefs {
		if pref.ID == uuid.Nil {
			pref.ID = uuid.New()
		}
		if pref.CreatedAt.IsZero() {
			pref.CreatedAt = now
		}
		pref.UpdatedAt = now

		_, err := stmt.ExecContext(ctx,
			pref.ID,
			pref.UserID,
			pref.NotificationType,
			pref.InAppEnabled,
			pref.EmailEnabled,
			pref.WebhookEnabled,
			pref.Settings,
			pref.CreatedAt,
			pref.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to upsert preference for type %s: %w", pref.NotificationType, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Delete deletes a user's preference for a specific type
func (r *NotificationPreferenceRepository) Delete(ctx context.Context, userID uuid.UUID, notificationType models.NotificationType) error {
	query := `DELETE FROM user_notification_preferences WHERE user_id = $1 AND notification_type = $2`

	_, err := r.db.ExecContext(ctx, query, userID, notificationType)
	if err != nil {
		return fmt.Errorf("failed to delete preference: %w", err)
	}

	return nil
}

// DeleteAllForUser deletes all preferences for a user
func (r *NotificationPreferenceRepository) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	query := `DELETE FROM user_notification_preferences WHERE user_id = $1`

	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user preferences: %w", err)
	}

	return nil
}

// GetUsersWithPreference returns user IDs who have a specific notification type enabled for a channel
func (r *NotificationPreferenceRepository) GetUsersWithPreference(ctx context.Context, notificationType models.NotificationType, channel models.NotificationChannel) ([]uuid.UUID, error) {
	var columnName string
	switch channel {
	case models.ChannelInApp:
		columnName = "in_app_enabled"
	case models.ChannelEmail:
		columnName = "email_enabled"
	case models.ChannelWebhook:
		columnName = "webhook_enabled"
	default:
		return nil, fmt.Errorf("invalid channel: %s", channel)
	}

	query := fmt.Sprintf(`
		SELECT user_id FROM user_notification_preferences
		WHERE notification_type = $1 AND %s = true
	`, columnName)

	rows, err := r.db.QueryContext(ctx, query, notificationType)
	if err != nil {
		return nil, fmt.Errorf("failed to get users with preference: %w", err)
	}
	defer rows.Close()

	var userIDs []uuid.UUID
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("failed to scan user ID: %w", err)
		}
		userIDs = append(userIDs, userID)
	}

	return userIDs, nil
}

// getDefaultPreference returns the default preference for a notification type
func (r *NotificationPreferenceRepository) getDefaultPreference(userID uuid.UUID, notificationType models.NotificationType) *models.UserNotificationPreference {
	pref := &models.UserNotificationPreference{
		ID:               uuid.New(),
		UserID:           userID,
		NotificationType: notificationType,
		InAppEnabled:     true,  // In-app enabled by default
		EmailEnabled:     false, // Email disabled by default
		WebhookEnabled:   false, // Webhook disabled by default
		Settings:         make(models.JSONMap),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	// Set type-specific defaults
	switch notificationType {
	case models.NotificationTypeJobCompleted:
		pref.EmailEnabled = true // Job completion emails enabled by default (matching legacy behavior)
	case models.NotificationTypeFirstCrack:
		pref.EmailEnabled = true // First crack is important
	case models.NotificationTypeJobFailed:
		pref.EmailEnabled = true // Failures are important
	case models.NotificationTypeAgentOffline, models.NotificationTypeAgentError:
		pref.EmailEnabled = true // Agent issues need attention
	case models.NotificationTypeSecurityMFADisabled, models.NotificationTypeSecurityPasswordChanged:
		pref.InAppEnabled = true
		pref.EmailEnabled = true // Security events always have email enabled
	case models.NotificationTypeTaskCompletedWithCracks:
		// Default to only_if_cracks mode
		pref.Settings["mode"] = string(models.TaskReportModeOnlyIfCracks)
	}

	return pref
}
