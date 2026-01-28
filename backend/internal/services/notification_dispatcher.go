package services

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	emailPkg "github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// globalDispatcher holds the global dispatcher instance for access from other packages
// This avoids circular imports (e.g., handlers/auth can't import routes)
var globalDispatcher *NotificationDispatcher

// SetGlobalDispatcher sets the global dispatcher instance
// Called from routes.SetupNotificationRoutes after dispatcher is created
func SetGlobalDispatcher(d *NotificationDispatcher) {
	globalDispatcher = d
}

// GetGlobalDispatcher returns the global dispatcher instance
// Returns nil if not initialized yet
func GetGlobalDispatcher() *NotificationDispatcher {
	return globalDispatcher
}

// NotificationDispatcher orchestrates notification delivery across all channels
type NotificationDispatcher struct {
	db                 *db.DB
	notificationRepo   *repository.NotificationRepository
	preferenceRepo     *repository.NotificationPreferenceRepository
	webhookRepo        *repository.UserWebhookRepository
	userRepo           *repository.UserRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	auditLogRepo       *repository.AuditLogRepository
	emailService       *emailPkg.Service
	webhookService     *NotificationWebhookService
	userHub            *UserNotificationHub
}

// NewNotificationDispatcher creates a new notification dispatcher
func NewNotificationDispatcher(
	dbConn *sql.DB,
	emailService *emailPkg.Service,
	webhookService *NotificationWebhookService,
	userHub *UserNotificationHub,
) *NotificationDispatcher {
	database := &db.DB{DB: dbConn}
	return &NotificationDispatcher{
		db:                 database,
		notificationRepo:   repository.NewNotificationRepository(database),
		preferenceRepo:     repository.NewNotificationPreferenceRepository(database),
		webhookRepo:        repository.NewUserWebhookRepository(database),
		userRepo:           repository.NewUserRepository(database),
		systemSettingsRepo: repository.NewSystemSettingsRepository(database),
		auditLogRepo:       repository.NewAuditLogRepository(database),
		emailService:       emailService,
		webhookService:     webhookService,
		userHub:            userHub,
	}
}

// Dispatch creates and delivers a notification to all enabled channels
func (d *NotificationDispatcher) Dispatch(ctx context.Context, params models.NotificationDispatchParams) error {
	debug.Log("Dispatching notification", map[string]interface{}{
		"user_id": params.UserID,
		"type":    params.Type,
		"title":   params.Title,
	})

	// DEDUPLICATION CHECK: Skip if notification already exists for this source
	// This prevents duplicate notifications when agents reconnect after server restart
	if params.SourceType != "" && params.SourceID != "" {
		exists, err := d.notificationRepo.ExistsBySourceAndType(ctx, params.SourceType, params.SourceID, params.Type)
		if err != nil {
			debug.Warning("Failed to check for duplicate notification: %v", err)
			// Continue anyway - better to potentially send duplicate than miss notification
		} else if exists {
			debug.Log("Notification already exists, skipping duplicate", map[string]interface{}{
				"source_type": params.SourceType,
				"source_id":   params.SourceID,
				"type":        params.Type,
			})
			return nil
		}
	}

	// Get user preferences for this notification type
	pref, err := d.preferenceRepo.GetByUserAndType(ctx, params.UserID, params.Type)
	if err != nil {
		return fmt.Errorf("failed to get user preferences: %w", err)
	}

	// Check if notification type is mandatory (security events that can't be disabled)
	isMandatory := params.Type.IsMandatory()

	// If not mandatory and all channels disabled, skip
	if !isMandatory && !pref.InAppEnabled && !pref.EmailEnabled && !pref.WebhookEnabled {
		debug.Log("Notification skipped - all channels disabled", map[string]interface{}{
			"user_id": params.UserID,
			"type":    params.Type,
		})
		return nil
	}

	// Create notification record
	notification := models.NewNotification(params.UserID, params.Type, params.Title, params.Message)
	notification.WithSource(params.SourceType, params.SourceID)
	notification.WithData(params.Data)

	// Save to database
	if err := d.notificationRepo.Create(ctx, notification); err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	// Log to audit log if this is an auditable event (security/critical events)
	if models.IsAuditableEventType(params.Type) {
		go d.logToAuditLog(ctx, notification, params)
		go d.broadcastAdminAlert(ctx, notification)
	}

	// Deliver to enabled channels in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, 3)

	// In-app delivery (always enabled for mandatory, otherwise check preference)
	if isMandatory || pref.InAppEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.deliverInApp(ctx, notification); err != nil {
				debug.Error("Failed to deliver in-app notification: %v", err)
				errChan <- err
			}
		}()
	}

	// Email delivery
	if isMandatory || pref.EmailEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.deliverEmail(ctx, notification); err != nil {
				debug.Error("Failed to deliver email notification: %v", err)
				errChan <- err
			}
		}()
	}

	// Webhook delivery
	if pref.WebhookEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.deliverWebhook(ctx, notification); err != nil {
				debug.Error("Failed to deliver webhook notification: %v", err)
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect any errors (don't fail the whole operation for delivery failures)
	var deliveryErrors []error
	for err := range errChan {
		deliveryErrors = append(deliveryErrors, err)
	}

	if len(deliveryErrors) > 0 {
		debug.Warning("Some notification deliveries failed", map[string]interface{}{
			"notification_id": notification.ID,
			"error_count":     len(deliveryErrors),
		})
	}

	debug.Log("Notification dispatched successfully", map[string]interface{}{
		"notification_id": notification.ID,
		"user_id":         params.UserID,
		"type":            params.Type,
	})

	return nil
}

// DispatchToMany dispatches the same notification to multiple users
func (d *NotificationDispatcher) DispatchToMany(ctx context.Context, userIDs []uuid.UUID, params models.NotificationDispatchParams) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(userIDs))

	for _, userID := range userIDs {
		wg.Add(1)
		go func(uid uuid.UUID) {
			defer wg.Done()
			p := params
			p.UserID = uid
			if err := d.Dispatch(ctx, p); err != nil {
				errChan <- fmt.Errorf("failed to dispatch to user %s: %w", uid, err)
			}
		}(userID)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to dispatch to %d users", len(errors))
	}

	return nil
}

// DispatchToAdmins dispatches a notification to all admin users
func (d *NotificationDispatcher) DispatchToAdmins(ctx context.Context, params models.NotificationDispatchParams) error {
	// Get all admin users
	admins, err := d.userRepo.GetByRole(ctx, "admin")
	if err != nil {
		return fmt.Errorf("failed to get admin users: %w", err)
	}

	var userIDs []uuid.UUID
	for _, admin := range admins {
		userIDs = append(userIDs, admin.ID)
	}

	if len(userIDs) == 0 {
		debug.Warning("No admin users found for notification dispatch")
		return nil
	}

	return d.DispatchToMany(ctx, userIDs, params)
}

// deliverInApp pushes notification via WebSocket to user's browser
func (d *NotificationDispatcher) deliverInApp(ctx context.Context, notification *models.Notification) error {
	// Push via UserNotificationHub WebSocket if available
	if d.userHub != nil {
		d.userHub.SendToUser(notification.UserID, notification)
	}

	debug.Log("In-app notification delivered", map[string]interface{}{
		"notification_id": notification.ID,
		"user_id":         notification.UserID,
	})

	return nil
}

// deliverEmail sends notification via email
func (d *NotificationDispatcher) deliverEmail(ctx context.Context, notification *models.Notification) error {
	// Check if email provider is configured
	hasEmailProvider, err := d.db.HasActiveEmailProvider()
	if err != nil {
		return fmt.Errorf("failed to check email provider: %w", err)
	}
	if !hasEmailProvider {
		debug.Warning("No active email provider configured, skipping email notification")
		return nil
	}

	// Get user email
	user, err := d.userRepo.GetByID(ctx, notification.UserID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	// Get email template for this notification type
	// Map notification types to email template types (some have different names)
	templateType := mapNotificationTypeToTemplateType(notification.NotificationType)
	tmpl, err := d.emailService.GetTemplateByType(ctx, templateType)
	if err != nil {
		debug.Warning("Email template not found for type %s (mapped from %s), using generic", templateType, notification.NotificationType)
		// Fall back to sending without template
		return d.sendGenericEmail(ctx, notification, user.Email)
	}

	// Prepare template data from notification data
	// Add both original keys and PascalCase versions for template compatibility
	templateData := make(map[string]interface{})
	for k, v := range notification.Data {
		templateData[k] = v
		// Also add PascalCase version for templates that use {{ .JobName }} style
		pascalKey := snakeToPascal(k)
		if pascalKey != k {
			templateData[pascalKey] = v
		}
	}

	// Add common fields
	templateData["Title"] = notification.Title
	templateData["Message"] = notification.Message
	templateData["NotificationType"] = string(notification.NotificationType)

	// Add user info for templates that use {{ .Username }} and {{ .Email }}
	templateData["Username"] = user.Username
	templateData["Email"] = user.Email

	// Send the email
	err = d.emailService.SendTemplatedEmail(ctx, user.Email, tmpl.ID, templateData)
	now := time.Now()
	if err != nil {
		errorMsg := err.Error()
		d.notificationRepo.UpdateEmailStatus(ctx, notification.ID, false, nil, &errorMsg)
		return fmt.Errorf("failed to send email: %w", err)
	}

	d.notificationRepo.UpdateEmailStatus(ctx, notification.ID, true, &now, nil)

	debug.Log("Email notification delivered", map[string]interface{}{
		"notification_id": notification.ID,
		"user_id":         notification.UserID,
		"email":           user.Email,
	})

	return nil
}

// sendGenericEmail sends a generic email without a template
func (d *NotificationDispatcher) sendGenericEmail(ctx context.Context, notification *models.Notification, email string) error {
	// For now, just log that we would send a generic email
	// In production, you'd want to implement a generic email template
	debug.Warning("Generic email would be sent to %s for notification %s", email, notification.ID)
	return nil
}

// deliverWebhook sends notification to BOTH system and user webhooks
func (d *NotificationDispatcher) deliverWebhook(ctx context.Context, notification *models.Notification) error {
	// Get user info for webhook payload - system webhooks need to identify which user triggered the notification
	var username, email string
	user, err := d.userRepo.GetByID(ctx, notification.UserID)
	if err == nil && user != nil {
		username = user.Username
		email = user.Email
	}

	var wg sync.WaitGroup
	var systemErr, userErr error

	// 1. Send to system webhook if configured
	wg.Add(1)
	go func() {
		defer wg.Done()
		systemErr = d.deliverSystemWebhook(ctx, notification, username, email)
	}()

	// 2. Send to all active user webhooks
	wg.Add(1)
	go func() {
		defer wg.Done()
		userErr = d.deliverUserWebhooks(ctx, notification, username, email)
	}()

	wg.Wait()

	// Update notification status
	now := time.Now()
	if systemErr != nil || userErr != nil {
		errorMsg := ""
		if systemErr != nil {
			errorMsg += fmt.Sprintf("system: %v; ", systemErr)
		}
		if userErr != nil {
			errorMsg += fmt.Sprintf("user: %v", userErr)
		}
		d.notificationRepo.UpdateWebhookStatus(ctx, notification.ID, false, &now, &errorMsg)
	} else {
		d.notificationRepo.UpdateWebhookStatus(ctx, notification.ID, true, &now, nil)
	}

	return nil
}

// deliverSystemWebhook sends to the system-wide webhook
func (d *NotificationDispatcher) deliverSystemWebhook(ctx context.Context, notification *models.Notification, username, email string) error {
	// Get global webhook config from system settings
	globalEnabled, err := d.systemSettingsRepo.GetSetting(ctx, "global_webhook_enabled")
	if err != nil || globalEnabled.Value == nil || *globalEnabled.Value != "true" {
		return nil // System webhook not enabled
	}

	globalURL, err := d.systemSettingsRepo.GetSetting(ctx, "global_webhook_url")
	if err != nil || globalURL.Value == nil || *globalURL.Value == "" {
		return nil // No URL configured
	}

	globalSecret, _ := d.systemSettingsRepo.GetSetting(ctx, "global_webhook_secret")
	var secret *string
	if globalSecret.Value != nil && *globalSecret.Value != "" {
		secret = globalSecret.Value
	}

	// Create payload with user context - system webhooks need to know which user triggered the notification
	payload := models.NewWebhookPayloadWithUser(notification, username, email)

	// Send webhook
	err = d.webhookService.Send(ctx, *globalURL.Value, payload, secret, nil, 3, 30)
	if err != nil {
		// Notify all admins about system webhook failure
		d.notifyWebhookFailure(ctx, notification.UserID, "System Webhook", *globalURL.Value, err.Error(), true)
		return err
	}

	debug.Log("System webhook delivered", map[string]interface{}{
		"notification_id": notification.ID,
		"url":             *globalURL.Value,
	})

	return nil
}

// deliverUserWebhooks sends to all user's active webhooks
func (d *NotificationDispatcher) deliverUserWebhooks(ctx context.Context, notification *models.Notification, username, email string) error {
	webhooks, err := d.webhookRepo.GetActiveByUserID(ctx, notification.UserID)
	if err != nil {
		return fmt.Errorf("failed to get user webhooks: %w", err)
	}

	if len(webhooks) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, webhook := range webhooks {
		// Check if webhook is configured for this notification type
		if !webhook.MatchesType(notification.NotificationType) {
			continue
		}

		wg.Add(1)
		go func(w *models.UserWebhook) {
			defer wg.Done()

			// Create payload with user context for consistency
			payload := models.NewWebhookPayloadWithUser(notification, username, email)

			err := d.webhookService.Send(ctx, w.URL, payload, w.Secret, w.CustomHeaders, w.RetryCount, w.TimeoutSeconds)

			// Update webhook stats
			if err != nil {
				errorMsg := err.Error()
				d.webhookRepo.UpdateStats(ctx, w.ID, false, &errorMsg)
				// Notify user about webhook failure
				d.notifyWebhookFailure(ctx, notification.UserID, w.Name, w.URL, err.Error(), false)
			} else {
				d.webhookRepo.UpdateStats(ctx, w.ID, true, nil)
				debug.Log("User webhook delivered", map[string]interface{}{
					"notification_id": notification.ID,
					"webhook_id":      w.ID,
					"webhook_name":    w.Name,
				})
			}
		}(webhook)
	}

	wg.Wait()
	return nil
}

// notifyWebhookFailure creates an in-app notification about a webhook delivery failure
func (d *NotificationDispatcher) notifyWebhookFailure(ctx context.Context, userID uuid.UUID, webhookName, webhookURL, errorMsg string, isSystemWebhook bool) {
	// Build notification params
	params := models.NotificationDispatchParams{
		Type:    models.NotificationTypeWebhookFailure,
		Title:   fmt.Sprintf("Webhook Delivery Failed: %s", webhookName),
		Message: fmt.Sprintf("Failed to deliver notification to webhook. Error: %s", errorMsg),
		Data: map[string]interface{}{
			"webhook_name": webhookName,
			"webhook_url":  webhookURL,
			"error":        errorMsg,
			"is_system":    isSystemWebhook,
		},
		SourceType: "webhook",
		SourceID:   uuid.New().String(), // Unique per failure event - each failure is distinct
	}

	if isSystemWebhook {
		// System webhook error: notify all admins
		go func() {
			if err := d.DispatchToAdmins(context.Background(), params); err != nil {
				debug.Error("Failed to notify admins of system webhook failure: %v", err)
			}
		}()
	} else {
		// User webhook error: notify that user only
		params.UserID = userID
		go func() {
			// Create notification directly to avoid infinite loop
			notification := models.NewNotification(userID, params.Type, params.Title, params.Message)
			notification.WithSource(params.SourceType, params.SourceID)
			notification.WithData(params.Data)

			if err := d.notificationRepo.Create(context.Background(), notification); err != nil {
				debug.Error("Failed to create webhook failure notification: %v", err)
				return
			}

			// Only deliver in-app (no email/webhook to avoid loops)
			d.deliverInApp(context.Background(), notification)
		}()
	}
}

// GetUserNotifications retrieves notifications for a user
func (d *NotificationDispatcher) GetUserNotifications(ctx context.Context, params models.NotificationListParams) (*models.NotificationListResponse, error) {
	return d.notificationRepo.List(ctx, params)
}

// GetUnreadCount returns the unread notification count for a user
func (d *NotificationDispatcher) GetUnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	return d.notificationRepo.GetUnreadCount(ctx, userID)
}

// MarkAsRead marks a notification as read
func (d *NotificationDispatcher) MarkAsRead(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	return d.notificationRepo.MarkAsRead(ctx, id, userID)
}

// MarkAllAsRead marks all notifications as read for a user
func (d *NotificationDispatcher) MarkAllAsRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	return d.notificationRepo.MarkAllAsRead(ctx, userID)
}

// DeleteNotifications deletes notifications
func (d *NotificationDispatcher) DeleteNotifications(ctx context.Context, ids []uuid.UUID, userID uuid.UUID) (int64, error) {
	return d.notificationRepo.DeleteMany(ctx, ids, userID)
}

// GetRecentNotifications returns the most recent notifications for a user
func (d *NotificationDispatcher) GetRecentNotifications(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	return d.notificationRepo.GetRecent(ctx, userID, limit)
}

// mapNotificationTypeToTemplateType maps notification types to email template types
// Some notification types have different names in the email_templates table
func mapNotificationTypeToTemplateType(notificationType models.NotificationType) string {
	switch notificationType {
	case models.NotificationTypeJobCompleted:
		return "job_completion" // Legacy template name
	case models.NotificationTypeTaskCompletedWithCracks:
		return "task_completed" // Shorter template name
	default:
		return string(notificationType)
	}
}

// commonAcronyms maps lowercase acronyms to their uppercase form for PascalCase conversion
// These are common technical acronyms that should remain fully uppercase in PascalCase
var commonAcronyms = map[string]string{
	"ip": "IP", "url": "URL", "id": "ID", "api": "API",
	"http": "HTTP", "https": "HTTPS", "json": "JSON", "xml": "XML",
	"uuid": "UUID", "mfa": "MFA", "jwt": "JWT", "tls": "TLS",
	"ssl": "SSL", "gpu": "GPU", "cpu": "CPU", "ram": "RAM",
	"sql": "SQL", "ssh": "SSH", "html": "HTML", "css": "CSS",
}

// snakeToPascal converts snake_case strings to PascalCase
// Example: "job_name" -> "JobName", "ip_address" -> "IPAddress"
// Handles common acronyms like IP, URL, ID, API, etc.
func snakeToPascal(s string) string {
	words := strings.Split(s, "_")
	for i, word := range words {
		if len(word) > 0 {
			lowerWord := strings.ToLower(word)
			if acronym, ok := commonAcronyms[lowerWord]; ok {
				words[i] = acronym
			} else {
				words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
			}
		}
	}
	return strings.Join(words, "")
}

// ShouldSendTaskReport checks if a task completion report should be sent
// based on user preferences and the crack count.
// Returns true if the notification should be sent, false if it should be skipped.
func (d *NotificationDispatcher) ShouldSendTaskReport(ctx context.Context, userID uuid.UUID, crackCount int64) bool {
	pref, err := d.preferenceRepo.GetByUserAndType(ctx, userID, models.NotificationTypeTaskCompletedWithCracks)
	if err != nil {
		debug.Warning("Failed to get task report preference: %v", err)
		// Default to only_if_cracks behavior on error
		return crackCount > 0
	}

	// Get the task report mode from user settings
	mode := pref.GetTaskReportMode()

	debug.Debug("Task report mode check: mode=%s, crackCount=%d", mode, crackCount)

	switch mode {
	case models.TaskReportModeAlways:
		// Always send regardless of crack count
		return true
	case models.TaskReportModeOnlyIfCracks:
		// Only send if there were cracks
		return crackCount > 0
	default:
		// Default to only_if_cracks
		return crackCount > 0
	}
}

// logToAuditLog creates an entry in the admin audit log for security/critical events
func (d *NotificationDispatcher) logToAuditLog(ctx context.Context, notification *models.Notification, params models.NotificationDispatchParams) {
	// Get user information for the audit log
	user, err := d.userRepo.GetByID(ctx, notification.UserID)
	if err != nil {
		debug.Warning("Failed to get user for audit log: %v", err)
		return
	}

	username := "Unknown"
	email := ""
	if user != nil {
		username = user.Username
		email = user.Email
	}

	// Create audit log entry
	severity := models.GetAuditSeverity(notification.NotificationType)
	auditLog := models.NewAuditLog(notification.NotificationType, severity, notification.Title, notification.Message)
	auditLog.WithUser(notification.UserID, username, email)
	auditLog.WithSource(notification.SourceType, notification.SourceID)
	auditLog.WithData(params.Data)

	// Extract IP address and user agent from notification data if present
	if params.Data != nil {
		if ipAddr, ok := params.Data["ip_address"].(string); ok {
			auditLog.WithRequestContext(ipAddr, "")
		}
		if userAgent, ok := params.Data["user_agent"].(string); ok {
			if auditLog.UserAgent == "" {
				auditLog.UserAgent = userAgent
			}
		}
	}

	// Save to database
	if err := d.auditLogRepo.Create(ctx, auditLog); err != nil {
		debug.Error("Failed to create audit log entry: %v", err)
	} else {
		debug.Log("Audit log entry created", map[string]interface{}{
			"event_type": notification.NotificationType,
			"severity":   severity,
			"user_id":    notification.UserID,
		})
	}
}

// broadcastAdminAlert sends a real-time alert to all connected admin users
func (d *NotificationDispatcher) broadcastAdminAlert(ctx context.Context, notification *models.Notification) {
	if d.userHub == nil {
		return
	}

	// Get user information for the alert
	user, err := d.userRepo.GetByID(ctx, notification.UserID)
	if err != nil {
		debug.Warning("Failed to get user for admin alert: %v", err)
		return
	}

	username := "Unknown"
	if user != nil {
		username = user.Username
	}

	// Create system alert
	alert := models.NewSystemAlert(notification, username)

	// Broadcast to all connected admins
	d.userHub.BroadcastSystemAlert(alert)
}
