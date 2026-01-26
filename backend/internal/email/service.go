package email

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"text/template"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/email/providers"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	emailtypes "github.com/ZerkerEOD/krakenhashes/backend/pkg/email"
)

var (
	ErrConfigExists       = errors.New("email configuration already exists")
	ErrConfigNotFound     = errors.New("email configuration not found")
	ErrTemplateNotFound   = errors.New("email template not found")
	ErrInvalidProvider    = errors.New("invalid email provider")
	ErrTemplateValidation = errors.New("template validation failed")
)

// Service handles email configuration and template management
type Service struct {
	db *sql.DB
}

// NewService creates a new email service
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// ConfigureProvider sets up or updates the email provider configuration
func (s *Service) ConfigureProvider(ctx context.Context, cfg *emailtypes.Config) error {
	// Check if APIKey is empty or redacted, indicating we should preserve the existing password
	preservePassword := cfg.APIKey == "" || cfg.APIKey == "[REDACTED]"

	// If we need to preserve the password, fetch the existing config
	if preservePassword {
		existingCfg, err := s.GetConfig(ctx)
		if err != nil && err != ErrConfigNotFound {
			debug.Error("failed to get existing config: %v", err)
			return err
		}

		// If an existing config was found, use its API key
		if err == nil && existingCfg != nil {
			debug.Info("preserving existing API key/password for provider: %s", cfg.ProviderType)
			cfg.APIKey = existingCfg.APIKey
		} else if preservePassword && err == ErrConfigNotFound {
			// New config creation requires a password
			debug.Error("cannot create new config without API key/password")
			return errors.New("API key/password is required for new configuration")
		}
	}

	provider, err := providers.New(cfg.ProviderType)
	if err != nil {
		debug.Error("failed to create provider: %v", err)
		return ErrInvalidProvider
	}

	if err := provider.ValidateConfig(cfg); err != nil {
		debug.Error("failed to validate provider config: %v", err)
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		debug.Error("failed to begin transaction: %v", err)
		return err
	}
	defer tx.Rollback()

	// First, deactivate all existing configurations
	_, err = tx.ExecContext(ctx, `UPDATE email_config SET is_active = false, updated_at = NOW()`)
	if err != nil {
		debug.Error("failed to deactivate existing configs: %v", err)
		return err
	}

	var exists bool
	err = tx.QueryRowContext(ctx, queries.EmailConfigExists, cfg.ProviderType).Scan(&exists)
	if err != nil {
		debug.Error("failed to check if config exists: %v", err)
		return err
	}

	// Always set is_active to true for the new/updated config
	cfg.IsActive = true

	if exists {
		_, err = tx.ExecContext(ctx, queries.EmailConfigUpdate,
			cfg.APIKey, cfg.AdditionalConfig, cfg.MonthlyLimit,
			cfg.ResetDate, cfg.IsActive, cfg.ProviderType)
		if err != nil {
			debug.Error("failed to update config: %v", err)
			return err
		}
		debug.Info("updated email configuration for provider: %s", cfg.ProviderType)
	} else {
		_, err = tx.ExecContext(ctx, queries.EmailConfigInsert,
			cfg.ProviderType, cfg.APIKey, cfg.AdditionalConfig,
			cfg.MonthlyLimit, cfg.ResetDate, cfg.IsActive)
		if err != nil {
			debug.Error("failed to insert config: %v", err)
			return err
		}
		debug.Info("created new email configuration for provider: %s", cfg.ProviderType)
	}

	return tx.Commit()
}

// GetConfig retrieves the current email configuration
func (s *Service) GetConfig(ctx context.Context) (*emailtypes.Config, error) {
	var cfg emailtypes.Config
	err := s.db.QueryRowContext(ctx, queries.EmailConfigGet).Scan(
		&cfg.ID, &cfg.ProviderType, &cfg.APIKey, &cfg.AdditionalConfig,
		&cfg.MonthlyLimit, &cfg.ResetDate, &cfg.IsActive, &cfg.CreatedAt,
		&cfg.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrConfigNotFound
	}
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// TestConnection tests the connection to the configured email provider
func (s *Service) TestConnection(ctx context.Context, testEmail string) error {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}

	provider, err := providers.New(cfg.ProviderType)
	if err != nil {
		return err
	}

	if err := provider.Initialize(cfg); err != nil {
		return err
	}

	return provider.TestConnection(ctx, testEmail)
}

// TestConnectionWithConfig tests the connection using a provided configuration without saving it
func (s *Service) TestConnectionWithConfig(ctx context.Context, cfg *emailtypes.Config, testEmail string) error {
	provider, err := providers.New(cfg.ProviderType)
	if err != nil {
		return fmt.Errorf("invalid provider: %w", err)
	}

	if err := provider.Initialize(cfg); err != nil {
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	return provider.TestConnection(ctx, testEmail)
}

// CreateTemplate creates a new email template
func (s *Service) CreateTemplate(ctx context.Context, template *emailtypes.Template, userID string) error {
	debug.Info("[EmailService] Creating new template for user: %s", userID)
	debug.Debug("[EmailService] Template data: %+v", template)
	debug.Debug("[EmailService] SQL Query: %s", queries.EmailTemplateInsert)
	debug.Debug("[EmailService] SQL Args: [%s, %s, %s, <html_content>, <text_content>, %s]",
		template.TemplateType, template.Name, template.Subject, userID)

	if err := validateTemplate(template); err != nil {
		debug.Error("[EmailService] Template validation failed: %v", err)
		return err
	}

	result, err := s.db.ExecContext(ctx, queries.EmailTemplateInsert,
		template.TemplateType, template.Name, template.Subject,
		template.HTMLContent, template.TextContent, userID,
	)
	if err != nil {
		debug.Error("[EmailService] SQL query failed: %v", err)
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		debug.Debug("[EmailService] Could not get last insert ID: %v", err)
	} else {
		debug.Info("[EmailService] Created template with ID: %d", id)
	}

	return nil
}

// UpdateTemplate updates an existing email template
func (s *Service) UpdateTemplate(ctx context.Context, template *emailtypes.Template, userID string) error {
	debug.Info("[EmailService] Updating template ID %d for user: %s", template.ID, userID)
	debug.Debug("[EmailService] Template update data: %+v", template)
	debug.Debug("[EmailService] SQL Query: %s", queries.EmailTemplateUpdate)
	debug.Debug("[EmailService] SQL Args: [%s, %s, %s, <html_content>, <text_content>, %s, %d]",
		template.TemplateType, template.Name, template.Subject, userID, template.ID)

	if err := validateTemplate(template); err != nil {
		debug.Error("[EmailService] Template validation failed: %v", err)
		return err
	}

	result, err := s.db.ExecContext(ctx, queries.EmailTemplateUpdate,
		template.TemplateType, template.Name, template.Subject,
		template.HTMLContent, template.TextContent, userID,
		template.ID,
	)
	if err != nil {
		debug.Error("[EmailService] SQL query failed: %v", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		debug.Error("[EmailService] Failed to get rows affected: %v", err)
		return err
	}
	if rows == 0 {
		debug.Info("[EmailService] No template found with ID: %d", template.ID)
		return ErrTemplateNotFound
	}

	debug.Info("[EmailService] Successfully updated template")
	return nil
}

// GetTemplate retrieves a template by ID
func (s *Service) GetTemplate(ctx context.Context, id int) (*emailtypes.Template, error) {
	debug.Info("[EmailService] Getting template with ID: %d", id)
	debug.Debug("[EmailService] SQL Query: %s", queries.EmailTemplateGet)
	debug.Debug("[EmailService] SQL Args: [%d]", id)

	var template emailtypes.Template
	err := s.db.QueryRowContext(ctx, queries.EmailTemplateGet, id).Scan(
		&template.ID, &template.TemplateType, &template.Name,
		&template.Subject, &template.HTMLContent, &template.TextContent,
		&template.CreatedAt, &template.UpdatedAt, &template.LastModifiedBy,
	)

	if err == sql.ErrNoRows {
		debug.Info("[EmailService] No template found with ID: %d", id)
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		debug.Error("[EmailService] SQL query failed: %v", err)
		debug.Debug("[EmailService] Row scan failed for columns: id, template_type, name, subject, html_content, text_content, created_at, updated_at, last_modified_by")
		return nil, err
	}

	debug.Debug("[EmailService] Retrieved template: %+v", template)
	return &template, nil
}

// ListTemplates retrieves all templates of a specific type
func (s *Service) ListTemplates(ctx context.Context, templateType *emailtypes.TemplateType) ([]emailtypes.Template, error) {
	debug.Info("[EmailService] Listing templates")
	if templateType != nil {
		debug.Info("[EmailService] Filtering by type: %s", *templateType)
	}

	whereClause := ""
	args := []interface{}{}

	if templateType != nil {
		whereClause = "WHERE template_type = $1"
		args = append(args, *templateType)
	}

	query := fmt.Sprintf(queries.EmailTemplateList, whereClause)
	debug.Debug("[EmailService] SQL Query: %s", query)
	debug.Debug("[EmailService] SQL Args: %+v", args)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		debug.Error("[EmailService] SQL query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var templates []emailtypes.Template
	for rows.Next() {
		var template emailtypes.Template
		err := rows.Scan(
			&template.ID, &template.TemplateType, &template.Name,
			&template.Subject, &template.HTMLContent, &template.TextContent,
			&template.CreatedAt, &template.UpdatedAt, &template.LastModifiedBy,
		)
		if err != nil {
			debug.Error("[EmailService] Failed to scan row: %v", err)
			debug.Debug("[EmailService] Row scan failed for columns: id, template_type, name, subject, html_content, text_content, created_at, updated_at, last_modified_by")
			return nil, err
		}
		debug.Debug("[EmailService] Scanned template: %+v", template)
		templates = append(templates, template)
	}

	if err := rows.Err(); err != nil {
		debug.Error("[EmailService] Error after scanning rows: %v", err)
		return nil, err
	}

	debug.Info("[EmailService] Retrieved %d templates", len(templates))
	for i, t := range templates {
		debug.Debug("[EmailService] Template %d: ID=%d, Type=%s, Name=%s", i+1, t.ID, t.TemplateType, t.Name)
	}
	return templates, nil
}

// GetTemplateByType retrieves a template by its type
func (s *Service) GetTemplateByType(ctx context.Context, templateType string) (*emailtypes.Template, error) {
	debug.Info("[EmailService] Getting template by type: %s", templateType)
	
	var template emailtypes.Template
	query := `SELECT id, template_type, name, subject, html_content, text_content, created_at, updated_at, last_modified_by 
	          FROM email_templates WHERE template_type = $1 LIMIT 1`
	
	err := s.db.QueryRowContext(ctx, query, templateType).Scan(
		&template.ID, &template.TemplateType, &template.Name,
		&template.Subject, &template.HTMLContent, &template.TextContent,
		&template.CreatedAt, &template.UpdatedAt, &template.LastModifiedBy,
	)
	
	if err == sql.ErrNoRows {
		debug.Info("[EmailService] No template found with type: %s", templateType)
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		debug.Error("[EmailService] Failed to get template by type: %v", err)
		return nil, err
	}
	
	return &template, nil
}

// DeleteTemplate deletes a template by ID
func (s *Service) DeleteTemplate(ctx context.Context, id int) error {
	debug.Info("[EmailService] Deleting template with ID: %d", id)
	debug.Debug("[EmailService] SQL Query: %s", queries.EmailTemplateDelete)
	debug.Debug("[EmailService] SQL Args: [%d]", id)

	result, err := s.db.ExecContext(ctx, queries.EmailTemplateDelete, id)
	if err != nil {
		debug.Error("[EmailService] SQL query failed: %v", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		debug.Error("[EmailService] Failed to get rows affected: %v", err)
		return err
	}
	if rows == 0 {
		debug.Info("[EmailService] No template found with ID: %d", id)
		return ErrTemplateNotFound
	}

	debug.Info("[EmailService] Successfully deleted template")
	return nil
}

// validateTemplate performs basic validation on a template
func validateTemplate(template *emailtypes.Template) error {
	debug.Debug("[EmailService] Validating template: %+v", template)

	if template.Name == "" || template.Subject == "" ||
		template.HTMLContent == "" || template.TextContent == "" {
		debug.Error("[EmailService] Template validation failed: missing required fields")
		return ErrTemplateValidation
	}

	debug.Debug("[EmailService] Template validation successful")
	return nil
}

// TrackEmailUsage increments the email usage count for the current month
func (s *Service) TrackEmailUsage(ctx context.Context) error {
	currentDate := time.Now()
	monthStart := time.Date(currentDate.Year(), currentDate.Month(), 1, 0, 0, 0, 0, time.UTC)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var monthlyLimit *int
	err = tx.QueryRowContext(ctx, queries.EmailUsageGetMonthlyLimit).Scan(&monthlyLimit)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	_, err = tx.ExecContext(ctx, queries.EmailUsageUpsert, monthStart)
	if err != nil {
		return err
	}

	if monthlyLimit != nil {
		var currentCount int
		err = tx.QueryRowContext(ctx, queries.EmailUsageGetCount, monthStart).Scan(&currentCount)
		if err != nil {
			return err
		}

		if currentCount > *monthlyLimit {
			return providers.ErrMonthlyLimitExceeded
		}
	}

	return tx.Commit()
}

// SendEmail sends an email using the configured provider
func (s *Service) SendEmail(ctx context.Context, data *emailtypes.EmailData) error {
	// Get the template
	template, err := s.GetTemplate(ctx, data.TemplateID)
	if err != nil {
		debug.Error("failed to get template: %v", err)
		return fmt.Errorf("failed to get template: %w", err)
	}

	// Get the provider configuration
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		debug.Error("failed to get email configuration: %v", err)
		return fmt.Errorf("failed to get email configuration: %w", err)
	}

	// Initialize the provider
	provider, err := providers.New(cfg.ProviderType)
	if err != nil {
		debug.Error("failed to create provider: %v", err)
		return fmt.Errorf("failed to create provider: %w", err)
	}

	if err := provider.Initialize(cfg); err != nil {
		debug.Error("failed to initialize provider: %v", err)
		return fmt.Errorf("failed to initialize provider: %w", err)
	}

	// Track email usage
	if err := s.TrackEmailUsage(ctx); err != nil {
		debug.Error("failed to track email usage: %v", err)
		return fmt.Errorf("failed to track email usage: %w", err)
	}

	// Create email data with template content
	emailData := &emailtypes.EmailData{
		To:        data.To,
		Subject:   data.Subject,
		Variables: data.Variables,
		Template:  template,
	}

	debug.Info("sending email to %v using template %s", data.To, template.Name)

	// Send the email
	if err := provider.Send(ctx, emailData); err != nil {
		debug.Error("failed to send email: %v", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	debug.Info("successfully sent email to %v", data.To)
	return nil
}

// SendTemplatedEmail sends an email using a template
func (s *Service) SendTemplatedEmail(ctx context.Context, to string, templateID int, data map[string]interface{}) error {
	template, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return fmt.Errorf("failed to get template: %w", err)
	}

	// Parse template
	subject, err := s.parseTemplate(template.Subject, data)
	if err != nil {
		return fmt.Errorf("failed to parse subject template: %w", err)
	}

	// Convert data to string map
	variables := make(map[string]string)
	for k, v := range data {
		variables[k] = fmt.Sprintf("%v", v)
	}

	// Send email
	emailData := &emailtypes.EmailData{
		To:         []string{to},
		Subject:    subject,
		Variables:  variables,
		TemplateID: templateID,
		Template:   template,
	}
	return s.SendEmail(ctx, emailData)
}

// parseTemplate parses a template string with the given data
func (s *Service) parseTemplate(tmpl string, data map[string]interface{}) (string, error) {
	t := template.New("email")
	parsed, err := t.Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
