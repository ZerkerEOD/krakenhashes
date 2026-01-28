package models

import (
	"net"
	"time"

	"github.com/google/uuid"
)

// AuditLogSeverity represents the severity level of an audit log entry
type AuditLogSeverity string

const (
	AuditSeverityInfo     AuditLogSeverity = "info"
	AuditSeverityWarning  AuditLogSeverity = "warning"
	AuditSeverityCritical AuditLogSeverity = "critical"
)

// IsValid checks if the severity level is valid
func (s AuditLogSeverity) IsValid() bool {
	switch s {
	case AuditSeverityInfo, AuditSeverityWarning, AuditSeverityCritical:
		return true
	}
	return false
}

// AuditLog represents an entry in the admin audit log
// This is used to track security and critical events across all users
type AuditLog struct {
	ID        uuid.UUID        `json:"id" db:"id"`
	EventType NotificationType `json:"event_type" db:"event_type"`
	Severity  AuditLogSeverity `json:"severity" db:"severity"`

	// User context (who the event happened to)
	UserID    *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Username  string     `json:"username" db:"username"`
	UserEmail string     `json:"user_email" db:"user_email"`

	// Event details
	Title   string  `json:"title" db:"title"`
	Message string  `json:"message" db:"message"`
	Data    JSONMap `json:"data" db:"data"`

	// Source tracking
	SourceType string `json:"source_type,omitempty" db:"source_type"`
	SourceID   string `json:"source_id,omitempty" db:"source_id"`

	// Request context
	IPAddress *net.IP `json:"ip_address,omitempty" db:"ip_address"`
	UserAgent string  `json:"user_agent,omitempty" db:"user_agent"`

	// Timestamp
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// NewAuditLog creates a new audit log entry
func NewAuditLog(eventType NotificationType, severity AuditLogSeverity, title, message string) *AuditLog {
	return &AuditLog{
		ID:        uuid.New(),
		EventType: eventType,
		Severity:  severity,
		Title:     title,
		Message:   message,
		Data:      make(JSONMap),
		CreatedAt: time.Now(),
	}
}

// WithUser sets user information for the audit log entry
func (a *AuditLog) WithUser(userID uuid.UUID, username, email string) *AuditLog {
	a.UserID = &userID
	a.Username = username
	a.UserEmail = email
	return a
}

// WithSource sets the source type and ID
func (a *AuditLog) WithSource(sourceType, sourceID string) *AuditLog {
	a.SourceType = sourceType
	a.SourceID = sourceID
	return a
}

// WithData sets additional data
func (a *AuditLog) WithData(data map[string]interface{}) *AuditLog {
	a.Data = data
	return a
}

// WithRequestContext sets IP address and user agent
func (a *AuditLog) WithRequestContext(ipAddress, userAgent string) *AuditLog {
	if ipAddress != "" {
		ip := net.ParseIP(ipAddress)
		if ip != nil {
			a.IPAddress = &ip
		}
	}
	a.UserAgent = userAgent
	return a
}

// IsAuditableEventType returns true if the notification type should be logged to the audit log
// These are security events and critical system events that admins need visibility into
func IsAuditableEventType(t NotificationType) bool {
	switch t {
	// Security events (critical severity)
	case NotificationTypeSecuritySuspiciousLogin,
		NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged:
		return true
	// Critical system events (warning severity)
	case NotificationTypeJobFailed,
		NotificationTypeAgentError,
		NotificationTypeAgentOffline,
		NotificationTypeWebhookFailure:
		return true
	}
	return false
}

// GetAuditSeverity returns the appropriate audit severity for a notification type
func GetAuditSeverity(t NotificationType) AuditLogSeverity {
	switch t {
	// Security events are critical
	case NotificationTypeSecuritySuspiciousLogin,
		NotificationTypeSecurityMFADisabled,
		NotificationTypeSecurityPasswordChanged:
		return AuditSeverityCritical
	// System errors are warnings
	case NotificationTypeJobFailed,
		NotificationTypeAgentError,
		NotificationTypeAgentOffline,
		NotificationTypeWebhookFailure:
		return AuditSeverityWarning
	}
	return AuditSeverityInfo
}

// AuditLogListParams represents query parameters for listing audit logs
type AuditLogListParams struct {
	EventTypes []NotificationType // Filter by one or more event types
	UserID     *uuid.UUID         // Filter by specific user
	Severity   *AuditLogSeverity  // Filter by severity level
	StartDate  *time.Time         // Filter by start date
	EndDate    *time.Time         // Filter by end date
	Limit      int
	Offset     int
}

// AuditLogListResponse represents a paginated list of audit logs
type AuditLogListResponse struct {
	AuditLogs []AuditLog `json:"audit_logs"`
	Total     int        `json:"total"`
	Limit     int        `json:"limit"`
	Offset    int        `json:"offset"`
}

// SystemAlert represents a real-time alert sent to admins via WebSocket
// This is ephemeral and not stored in the database
type SystemAlert struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"` // Always "system_alert"
	EventType    NotificationType       `json:"event_type"`
	Severity     AuditLogSeverity       `json:"severity"`
	Title        string                 `json:"title"`
	Message      string                 `json:"message"`
	AffectedUser string                 `json:"affected_user"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Timestamp    int64                  `json:"timestamp"`
}

// NewSystemAlert creates a new system alert from a notification
func NewSystemAlert(notification *Notification, username string) *SystemAlert {
	return &SystemAlert{
		ID:           uuid.New().String(),
		Type:         "system_alert",
		EventType:    notification.NotificationType,
		Severity:     GetAuditSeverity(notification.NotificationType),
		Title:        notification.Title,
		Message:      notification.Message,
		AffectedUser: username,
		Data:         notification.Data,
		Timestamp:    time.Now().Unix(),
	}
}
