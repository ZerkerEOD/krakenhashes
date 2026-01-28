package services

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// NotificationService handles notification operations
type NotificationService struct {
	db       *db.DB
	userRepo *repository.UserRepository
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(dbConn *sql.DB) *NotificationService {
	database := &db.DB{DB: dbConn}
	return &NotificationService{
		db:       database,
		userRepo: repository.NewUserRepository(database),
	}
}

// GetUserNotificationPreferences retrieves the notification preferences for a user
func (s *NotificationService) GetUserNotificationPreferences(ctx context.Context, userID uuid.UUID) (*models.UserNotificationPreferencesExtended, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Check if email provider is configured
	hasEmailProvider, err := s.db.HasActiveEmailProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to check email provider: %w", err)
	}

	// Get per-type preferences from repository
	prefRepo := repository.NewNotificationPreferenceRepository(s.db)
	typePrefsMap, err := prefRepo.GetAllByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get type preferences: %w", err)
	}

	// Convert to frontend-expected format
	typePreferences := make(map[models.NotificationType]models.TypeChannelPreference)
	for notifType, pref := range typePrefsMap {
		typePreferences[notifType] = models.TypeChannelPreference{
			Enabled:        true,
			InAppEnabled:   pref.InAppEnabled,
			EmailEnabled:   pref.EmailEnabled,
			WebhookEnabled: pref.WebhookEnabled,
			Settings:       pref.Settings,
		}
	}

	// Get webhook counts
	webhookRepo := repository.NewUserWebhookRepository(s.db)
	totalWebhooks, activeWebhooks, err := webhookRepo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to count webhooks: %w", err)
	}

	prefs := &models.UserNotificationPreferencesExtended{
		NotifyOnJobCompletion: user.NotifyOnJobCompletion,
		EmailConfigured:       hasEmailProvider,
		TypePreferences:       typePreferences,
		WebhooksConfigured:    totalWebhooks,
		WebhooksActive:        activeWebhooks,
	}

	debug.Log("Retrieved notification preferences", map[string]interface{}{
		"user_id":              userID,
		"notify_on_completion": prefs.NotifyOnJobCompletion,
		"email_configured":     prefs.EmailConfigured,
		"type_prefs_count":     len(typePreferences),
		"webhooks_configured":  totalWebhooks,
		"webhooks_active":      activeWebhooks,
	})

	return prefs, nil
}

// UpdateUserNotificationPreferences updates the notification preferences for a user
func (s *NotificationService) UpdateUserNotificationPreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) error {
	// Check if email provider is configured when enabling notifications
	if prefs.NotifyOnJobCompletion {
		hasEmailProvider, err := s.db.HasActiveEmailProvider()
		if err != nil {
			return fmt.Errorf("failed to check email provider: %w", err)
		}
		if !hasEmailProvider {
			return fmt.Errorf("email notifications require an email gateway to be configured")
		}
	}

	debug.Log("Updating notification preferences", map[string]interface{}{
		"user_id":              userID,
		"notify_on_completion": prefs.NotifyOnJobCompletion,
		"type_prefs_count":     len(prefs.TypePreferences),
	})

	// Update legacy user preference (notify_on_job_completion)
	err := s.userRepo.UpdateNotificationPreferences(ctx, userID, prefs.NotifyOnJobCompletion)
	if err != nil {
		return fmt.Errorf("failed to update notification preferences: %w", err)
	}

	// Save per-type preferences if provided
	if len(prefs.TypePreferences) > 0 {
		prefRepo := repository.NewNotificationPreferenceRepository(s.db)

		// Fetch all existing preferences to merge with partial updates
		existingPrefs, err := prefRepo.GetAllByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get existing preferences: %w", err)
		}

		for notifType, typePref := range prefs.TypePreferences {
			// Start with existing preference (or defaults)
			existing := existingPrefs[notifType]
			if existing == nil {
				existing = &models.UserNotificationPreference{
					UserID:           userID,
					NotificationType: notifType,
					InAppEnabled:     true,  // default
					EmailEnabled:     false, // default
					WebhookEnabled:   false, // default
					Settings:         make(models.JSONMap),
				}
			}

			// Merge partial update - only update fields that were explicitly set
			// The frontend sends partial updates, so we check if the value differs from default
			// to determine if it was explicitly set
			pref := &models.UserNotificationPreference{
				UserID:           userID,
				NotificationType: notifType,
				InAppEnabled:     existing.InAppEnabled,
				EmailEnabled:     existing.EmailEnabled,
				WebhookEnabled:   existing.WebhookEnabled,
				Settings:         existing.Settings,
			}

			// Apply the partial update - check each field
			// Since Go defaults bools to false, we need to always apply the incoming value
			// The frontend should send all three values when updating
			pref.InAppEnabled = typePref.InAppEnabled
			pref.EmailEnabled = typePref.EmailEnabled
			pref.WebhookEnabled = typePref.WebhookEnabled
			if typePref.Settings != nil {
				pref.Settings = typePref.Settings
			}

			if err := prefRepo.Upsert(ctx, pref); err != nil {
				debug.Error("Failed to save preference for type %s: %v", notifType, err)
				return fmt.Errorf("failed to save preference for type %s: %w", notifType, err)
			}

			debug.Log("Saved notification preference", map[string]interface{}{
				"user_id":           userID,
				"notification_type": notifType,
				"in_app_enabled":    pref.InAppEnabled,
				"email_enabled":     pref.EmailEnabled,
				"webhook_enabled":   pref.WebhookEnabled,
			})
		}
	}

	debug.Log("Successfully updated notification preferences", map[string]interface{}{
		"user_id":          userID,
		"type_prefs_saved": len(prefs.TypePreferences),
	})

	return nil
}