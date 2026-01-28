package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeJobStarted             NotificationType = "job_started"
	NotificationTypeJobCompleted           NotificationType = "job_completed"
	NotificationTypeJobFailed              NotificationType = "job_failed"
	NotificationTypeFirstCrack             NotificationType = "first_crack"
	NotificationTypeTaskCompletedWithCracks NotificationType = "task_completed_with_cracks"
	NotificationTypeAgentOffline           NotificationType = "agent_offline"
	NotificationTypeAgentError             NotificationType = "agent_error"
	NotificationTypeSecuritySuspiciousLogin NotificationType = "security_suspicious_login"
	NotificationTypeSecurityMFADisabled    NotificationType = "security_mfa_disabled"
	NotificationTypeSecurityPasswordChanged NotificationType = "security_password_changed"
	NotificationTypeWebhookFailure         NotificationType = "webhook_failure"
)

// AllNotificationTypes returns all valid notification types
func AllNotificationTypes() []NotificationType {
	return []NotificationType{
		NotificationTypeJobStarted,
		NotificationTypeJobCompleted,
		NotificationTypeJobFailed,
		NotificationTypeFirstCrack,
		NotificationTypeTaskCompletedWithCracks,
		NotificationTypeAgentOffline,
		NotificationTypeAgentError,
		NotificationTypeSecuritySuspiciousLogin,
		NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged,
		NotificationTypeWebhookFailure,
	}
}

// IsValid checks if the notification type is valid
func (t NotificationType) IsValid() bool {
	switch t {
	case NotificationTypeJobStarted,
		NotificationTypeJobCompleted,
		NotificationTypeJobFailed,
		NotificationTypeFirstCrack,
		NotificationTypeTaskCompletedWithCracks,
		NotificationTypeAgentOffline,
		NotificationTypeAgentError,
		NotificationTypeSecuritySuspiciousLogin,
		NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged,
		NotificationTypeWebhookFailure:
		return true
	}
	return false
}

// IsMandatory returns true if this notification type cannot be disabled
func (t NotificationType) IsMandatory() bool {
	switch t {
	case NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged:
		return true
	}
	return false
}

// Category returns the category for this notification type
func (t NotificationType) Category() string {
	switch t {
	case NotificationTypeJobStarted,
		NotificationTypeJobCompleted,
		NotificationTypeJobFailed,
		NotificationTypeFirstCrack,
		NotificationTypeTaskCompletedWithCracks:
		return "job"
	case NotificationTypeAgentOffline,
		NotificationTypeAgentError:
		return "agent"
	case NotificationTypeSecuritySuspiciousLogin,
		NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged:
		return "security"
	case NotificationTypeWebhookFailure:
		return "system"
	}
	return "system"
}

// NotificationChannel represents delivery channel
type NotificationChannel string

const (
	ChannelInApp   NotificationChannel = "in_app"
	ChannelEmail   NotificationChannel = "email"
	ChannelWebhook NotificationChannel = "webhook"
)

// TaskReportMode represents when to send task completion notifications
type TaskReportMode string

const (
	TaskReportModeOnlyIfCracks TaskReportMode = "only_if_cracks"
	TaskReportModeAlways       TaskReportMode = "always"
)

// Notification represents a notification record
type Notification struct {
	ID               uuid.UUID        `json:"id" db:"id"`
	UserID           uuid.UUID        `json:"user_id" db:"user_id"`
	NotificationType NotificationType `json:"notification_type" db:"notification_type"`
	Title            string           `json:"title" db:"title"`
	Message          string           `json:"message" db:"message"`
	Data             JSONMap          `json:"data" db:"data"`

	// In-app delivery tracking
	InAppRead   bool       `json:"in_app_read" db:"in_app_read"`
	InAppReadAt *time.Time `json:"in_app_read_at,omitempty" db:"in_app_read_at"`

	// Email delivery tracking
	EmailSent   bool       `json:"email_sent" db:"email_sent"`
	EmailSentAt *time.Time `json:"email_sent_at,omitempty" db:"email_sent_at"`
	EmailError  *string    `json:"email_error,omitempty" db:"email_error"`

	// Webhook delivery tracking
	WebhookSent   bool       `json:"webhook_sent" db:"webhook_sent"`
	WebhookSentAt *time.Time `json:"webhook_sent_at,omitempty" db:"webhook_sent_at"`
	WebhookError  *string    `json:"webhook_error,omitempty" db:"webhook_error"`

	// Source tracking for navigation
	SourceType string `json:"source_type,omitempty" db:"source_type"`
	SourceID   string `json:"source_id,omitempty" db:"source_id"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// NewNotification creates a new notification with a generated UUID
func NewNotification(userID uuid.UUID, notificationType NotificationType, title, message string) *Notification {
	return &Notification{
		ID:               uuid.New(),
		UserID:           userID,
		NotificationType: notificationType,
		Title:            title,
		Message:          message,
		Data:             make(JSONMap),
		CreatedAt:        time.Now(),
	}
}

// WithSource sets the source type and ID for the notification
func (n *Notification) WithSource(sourceType, sourceID string) *Notification {
	n.SourceType = sourceType
	n.SourceID = sourceID
	return n
}

// WithData sets additional data for the notification
func (n *Notification) WithData(data map[string]interface{}) *Notification {
	n.Data = data
	return n
}

// UserNotificationPreference represents per-type settings for a user
type UserNotificationPreference struct {
	ID               uuid.UUID        `json:"id" db:"id"`
	UserID           uuid.UUID        `json:"user_id" db:"user_id"`
	NotificationType NotificationType `json:"notification_type" db:"notification_type"`
	InAppEnabled     bool             `json:"in_app_enabled" db:"in_app_enabled"`
	EmailEnabled     bool             `json:"email_enabled" db:"email_enabled"`
	WebhookEnabled   bool             `json:"webhook_enabled" db:"webhook_enabled"`
	Settings         JSONMap          `json:"settings" db:"settings"`
	CreatedAt        time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at" db:"updated_at"`
}

// GetTaskReportMode returns the task report mode from settings
func (p *UserNotificationPreference) GetTaskReportMode() TaskReportMode {
	if p.Settings == nil {
		return TaskReportModeOnlyIfCracks
	}
	if mode, ok := p.Settings["mode"].(string); ok {
		return TaskReportMode(mode)
	}
	return TaskReportModeOnlyIfCracks
}

// SetTaskReportMode sets the task report mode in settings
func (p *UserNotificationPreference) SetTaskReportMode(mode TaskReportMode) {
	if p.Settings == nil {
		p.Settings = make(JSONMap)
	}
	p.Settings["mode"] = string(mode)
}

// UserNotificationPreferencesExtended is the full preferences model for API responses
type UserNotificationPreferencesExtended struct {
	// Legacy field for backward compatibility
	NotifyOnJobCompletion bool `json:"notifyOnJobCompletion"`
	EmailConfigured       bool `json:"emailConfigured"`

	// Per-type preferences
	TypePreferences map[NotificationType]TypeChannelPreference `json:"typePreferences"`

	// Webhook configuration summary
	WebhooksConfigured int  `json:"webhooksConfigured"`
	WebhooksActive     int  `json:"webhooksActive"`
}

// TypeChannelPreference represents channel settings for a notification type
type TypeChannelPreference struct {
	Enabled        bool           `json:"enabled"`
	InAppEnabled   bool           `json:"inAppEnabled"`
	EmailEnabled   bool           `json:"emailEnabled"`
	WebhookEnabled bool           `json:"webhookEnabled"`
	Settings       JSONMap        `json:"settings,omitempty"`
}

// UserWebhook represents a user's webhook configuration
type UserWebhook struct {
	ID                uuid.UUID          `json:"id" db:"id"`
	UserID            uuid.UUID          `json:"user_id" db:"user_id"`
	Name              string             `json:"name" db:"name"`
	URL               string             `json:"url" db:"url"`
	Secret            *string            `json:"-" db:"secret"` // Never expose in JSON
	IsActive          bool               `json:"is_active" db:"is_active"`
	NotificationTypes pq.StringArray     `json:"notification_types" db:"notification_types"`
	CustomHeaders     JSONMap            `json:"custom_headers" db:"custom_headers"`
	RetryCount        int                `json:"retry_count" db:"retry_count"`
	TimeoutSeconds    int                `json:"timeout_seconds" db:"timeout_seconds"`
	LastTriggeredAt   *time.Time         `json:"last_triggered_at,omitempty" db:"last_triggered_at"`
	LastSuccessAt     *time.Time         `json:"last_success_at,omitempty" db:"last_success_at"`
	LastError         *string            `json:"last_error,omitempty" db:"last_error"`
	TotalSent         int                `json:"total_sent" db:"total_sent"`
	TotalFailed       int                `json:"total_failed" db:"total_failed"`
	CreatedAt         time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at" db:"updated_at"`
}

// NewUserWebhook creates a new user webhook with defaults
func NewUserWebhook(userID uuid.UUID, name, url string) *UserWebhook {
	return &UserWebhook{
		ID:             uuid.New(),
		UserID:         userID,
		Name:           name,
		URL:            url,
		IsActive:       true,
		CustomHeaders:  make(JSONMap),
		RetryCount:     3,
		TimeoutSeconds: 30,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// MatchesType checks if this webhook should receive a notification type
// Returns true if notification_types is NULL/empty (all types) or contains the type
func (w *UserWebhook) MatchesType(notificationType NotificationType) bool {
	if len(w.NotificationTypes) == 0 {
		return true
	}
	for _, t := range w.NotificationTypes {
		if t == string(notificationType) {
			return true
		}
	}
	return false
}

// HasSecret returns true if a secret is configured
func (w *UserWebhook) HasSecret() bool {
	return w.Secret != nil && *w.Secret != ""
}

// UserWebhookAdmin is the admin view of a user's webhook (includes user info)
type UserWebhookAdmin struct {
	UserWebhook
	Username string `json:"username" db:"username"`
	Email    string `json:"email" db:"email"`
}

// AgentOfflineBuffer tracks agents pending offline notification
type AgentOfflineBuffer struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	AgentID            int        `json:"agent_id" db:"agent_id"`
	DisconnectedAt     time.Time  `json:"disconnected_at" db:"disconnected_at"`
	NotificationDueAt  time.Time  `json:"notification_due_at" db:"notification_due_at"`
	NotificationSent   bool       `json:"notification_sent" db:"notification_sent"`
	NotificationSentAt *time.Time `json:"notification_sent_at,omitempty" db:"notification_sent_at"`
	Reconnected        bool       `json:"reconnected" db:"reconnected"`
	ReconnectedAt      *time.Time `json:"reconnected_at,omitempty" db:"reconnected_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
}

// NewAgentOfflineBuffer creates a new offline buffer entry
func NewAgentOfflineBuffer(agentID int, bufferMinutes int) *AgentOfflineBuffer {
	now := time.Now()
	return &AgentOfflineBuffer{
		ID:                uuid.New(),
		AgentID:           agentID,
		DisconnectedAt:    now,
		NotificationDueAt: now.Add(time.Duration(bufferMinutes) * time.Minute),
		CreatedAt:         now,
	}
}

// WebhookPayload is the standard payload sent to webhooks
type WebhookPayload struct {
	Event     string                 `json:"event"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// NewWebhookPayload creates a new webhook payload
func NewWebhookPayload(notification *Notification) *WebhookPayload {
	return &WebhookPayload{
		Event:     fmt.Sprintf("notification.%s", notification.NotificationType),
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"notification_id":   notification.ID,
			"notification_type": notification.NotificationType,
			"title":             notification.Title,
			"message":           notification.Message,
			"data":              notification.Data,
			"source_type":       notification.SourceType,
			"source_id":         notification.SourceID,
			"created_at":        notification.CreatedAt,
		},
	}
}

// NewWebhookPayloadWithUser creates a webhook payload with user context
// This is useful for system webhooks where admins need to know which user triggered the notification
func NewWebhookPayloadWithUser(notification *Notification, username, email string) *WebhookPayload {
	payload := NewWebhookPayload(notification)
	// Add user identification to the payload
	payload.Data["user_id"] = notification.UserID.String()
	payload.Data["username"] = username
	payload.Data["user_email"] = email
	return payload
}

// NotificationListParams represents query parameters for listing notifications
type NotificationListParams struct {
	UserID    uuid.UUID
	Category  string           // Filter by category (job, agent, security, system)
	Type      NotificationType // Filter by specific type
	ReadOnly  *bool            // true = only read, false = only unread, nil = all
	Limit     int
	Offset    int
}

// NotificationListResponse represents a paginated list of notifications
type NotificationListResponse struct {
	Notifications []Notification `json:"notifications"`
	Total         int            `json:"total"`
	Limit         int            `json:"limit"`
	Offset        int            `json:"offset"`
}

// NotificationDispatchParams represents parameters for dispatching a notification
type NotificationDispatchParams struct {
	UserID     uuid.UUID
	Type       NotificationType
	Title      string
	Message    string
	Data       map[string]interface{}
	SourceType string
	SourceID   string
}

// GlobalWebhookConfig represents system-wide webhook configuration
type GlobalWebhookConfig struct {
	URL           string            `json:"url"`
	Secret        string            `json:"-"` // Never expose in JSON
	Enabled       bool              `json:"enabled"`
	CustomHeaders map[string]string `json:"custom_headers"`
}

// JSONMap is a helper type for JSONB columns
type JSONMap map[string]interface{}

// Scan implements the sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONMap)
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("failed to unmarshal JSONMap value: %v", value)
	}

	if len(bytes) == 0 {
		*j = make(JSONMap)
		return nil
	}

	return json.Unmarshal(bytes, j)
}

// Value implements the driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	return json.Marshal(j)
}
