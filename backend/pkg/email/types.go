package email

import (
	"encoding/json"
	"time"
)

// ProviderType represents supported email providers
type ProviderType string

const (
	ProviderMailgun  ProviderType = "mailgun"
	ProviderSendGrid ProviderType = "sendgrid"
	ProviderSMTP     ProviderType = "smtp"
	// Potential Future providers to be added in v2.0:
	// - Gmail
	// - Mailchimp
)

// TemplateType represents different types of email templates
type TemplateType string

const (
	TemplateSecurityEvent TemplateType = "security_event"
	TemplateJobCompletion TemplateType = "job_completion"
	TemplateAdminError    TemplateType = "admin_error"
	TemplateMFACode       TemplateType = "mfa_code"

	// Notification email templates
	TemplateSecurityPasswordChanged TemplateType = "security_password_changed"
	TemplateSecurityMFADisabled     TemplateType = "security_mfa_disabled"
	TemplateSecuritySuspiciousLogin TemplateType = "security_suspicious_login"
	TemplateJobStarted              TemplateType = "job_started"
	TemplateJobFailed               TemplateType = "job_failed"
	TemplateFirstCrack              TemplateType = "first_crack"
	TemplateTaskCompleted           TemplateType = "task_completed"
	TemplateAgentOffline            TemplateType = "agent_offline"
	TemplateAgentError              TemplateType = "agent_error"
	TemplateWebhookFailure          TemplateType = "webhook_failure"
)

// Config represents email provider configuration
type Config struct {
	ID               int             `json:"id" db:"id"`
	ProviderType     ProviderType    `json:"provider_type" db:"provider_type"`
	APIKey           string          `json:"api_key" db:"api_key"`
	AdditionalConfig json.RawMessage `json:"additional_config,omitempty" db:"additional_config"`
	MonthlyLimit     *int            `json:"monthly_limit,omitempty" db:"monthly_limit"`
	ResetDate        *time.Time      `json:"reset_date,omitempty" db:"reset_date"`
	IsActive         bool            `json:"is_active" db:"is_active"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

// TestConfig represents configuration for testing email
type TestConfig struct {
	Config
	TestEmail string `json:"test_email"`
}

// Template represents an email template
type Template struct {
	ID             int          `json:"id" db:"id"`
	TemplateType   TemplateType `json:"template_type" db:"template_type"`
	Name           string       `json:"name" db:"name"`
	Subject        string       `json:"subject" db:"subject"`
	HTMLContent    string       `json:"html_content" db:"html_content"`
	TextContent    string       `json:"text_content" db:"text_content"`
	CreatedAt      time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at" db:"updated_at"`
	LastModifiedBy *string      `json:"last_modified_by,omitempty" db:"last_modified_by"`
}

// Usage represents email usage tracking
type Usage struct {
	ID        int       `json:"id" db:"id"`
	MonthYear time.Time `json:"month_year" db:"month_year"`
	Count     int       `json:"count" db:"count"`
	LastReset time.Time `json:"last_reset" db:"last_reset"`
}

// EmailData represents the data needed to send an email
type EmailData struct {
	To         []string          `json:"to"`
	Subject    string            `json:"subject"`
	Variables  map[string]string `json:"variables,omitempty"`
	TemplateID int               `json:"template_id"`
	Template   *Template         `json:"-"` // Used internally, not serialized
}
