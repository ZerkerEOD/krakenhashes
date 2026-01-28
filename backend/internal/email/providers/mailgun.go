package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	emailtypes "github.com/ZerkerEOD/krakenhashes/backend/pkg/email"
	"github.com/mailgun/mailgun-go/v4"
)

// MailgunConfig represents Mailgun-specific configuration
type MailgunConfig struct {
	Domain    string `json:"domain"`
	FromEmail string `json:"from_email"`
	FromName  string `json:"from_name"`
	Region    string `json:"region"` // "us" (default) or "eu"
}

// mailgunProvider implements the Provider interface for Mailgun
type mailgunProvider struct {
	mg        *mailgun.MailgunImpl
	domain    string
	fromName  string
	fromEmail string
}

// init registers the Mailgun provider
func init() {
	Register(emailtypes.ProviderMailgun, func() Provider {
		return &mailgunProvider{}
	})
}

// Initialize sets up the Mailgun client
func (p *mailgunProvider) Initialize(cfg *emailtypes.Config) error {
	if cfg.APIKey == "" {
		debug.Error("mailgun API key not provided")
		return ErrProviderNotConfigured
	}

	var mgConfig MailgunConfig
	if err := json.Unmarshal(cfg.AdditionalConfig, &mgConfig); err != nil {
		debug.Error("failed to parse mailgun config: %v", err)
		return fmt.Errorf("invalid mailgun configuration: %w", err)
	}

	if mgConfig.Domain == "" {
		debug.Error("mailgun domain not provided")
		return errors.New("mailgun domain is required")
	}

	if mgConfig.FromEmail == "" {
		debug.Error("mailgun from_email not provided")
		return errors.New("mailgun from_email is required")
	}

	if mgConfig.FromName == "" {
		debug.Error("mailgun from_name not provided")
		return errors.New("mailgun from_name is required")
	}

	p.mg = mailgun.NewMailgun(mgConfig.Domain, cfg.APIKey)

	// Set API base URL based on region (EU accounts use different endpoint)
	if strings.ToLower(mgConfig.Region) == "eu" {
		p.mg.SetAPIBase(mailgun.APIBaseEU)
		debug.Info("using Mailgun EU region endpoint")
	}

	p.domain = mgConfig.Domain
	p.fromName = mgConfig.FromName
	p.fromEmail = mgConfig.FromEmail
	debug.Info("initialized mailgun client for domain: %s with sender: %s <%s>", mgConfig.Domain, mgConfig.FromName, mgConfig.FromEmail)
	return nil
}

// ValidateConfig validates the Mailgun configuration
func (p *mailgunProvider) ValidateConfig(cfg *emailtypes.Config) error {
	if cfg.APIKey == "" {
		debug.Error("mailgun API key not provided")
		return errors.New("mailgun API key is required")
	}

	var mgConfig MailgunConfig
	if err := json.Unmarshal(cfg.AdditionalConfig, &mgConfig); err != nil {
		debug.Error("failed to parse mailgun config: %v", err)
		return fmt.Errorf("invalid mailgun configuration: %w", err)
	}

	if mgConfig.Domain == "" {
		debug.Error("mailgun domain not provided")
		return errors.New("mailgun domain is required")
	}

	if mgConfig.FromEmail == "" {
		debug.Error("mailgun from_email not provided")
		return errors.New("mailgun from_email is required")
	}

	if mgConfig.FromName == "" {
		debug.Error("mailgun from_name not provided")
		return errors.New("mailgun from_name is required")
	}

	debug.Info("validated mailgun configuration for domain: %s with sender: %s <%s>", mgConfig.Domain, mgConfig.FromName, mgConfig.FromEmail)
	return nil
}

// Send sends an email using Mailgun
func (p *mailgunProvider) Send(ctx context.Context, data *emailtypes.EmailData) error {
	if p.mg == nil {
		debug.Error("mailgun client not initialized")
		return ErrProviderNotConfigured
	}

	if data.Template == nil {
		debug.Error("email template not provided")
		return ErrInvalidTemplate
	}

	var textContent, htmlContent string

	// Process template variables
	if len(data.Variables) > 0 {
		debug.Info("processing template variables for email")
		// Create template for both HTML and text content
		htmlTmpl, err := template.New("email_html").Parse(data.Template.HTMLContent)
		if err != nil {
			debug.Error("failed to parse HTML template: %v", err)
			return fmt.Errorf("failed to parse HTML template: %w", err)
		}

		textTmpl, err := template.New("email_text").Parse(data.Template.TextContent)
		if err != nil {
			debug.Error("failed to parse text template: %v", err)
			return fmt.Errorf("failed to parse text template: %w", err)
		}

		// Execute templates with variables
		if err := executeTemplate(htmlTmpl, data.Variables, &htmlContent); err != nil {
			debug.Error("failed to execute HTML template: %v", err)
			return fmt.Errorf("failed to execute HTML template: %w", err)
		}

		if err := executeTemplate(textTmpl, data.Variables, &textContent); err != nil {
			debug.Error("failed to execute text template: %v", err)
			return fmt.Errorf("failed to execute text template: %w", err)
		}
	} else {
		debug.Info("using template content without variables")
		htmlContent = data.Template.HTMLContent
		textContent = data.Template.TextContent
	}

	// Create a new message with text content
	from := fmt.Sprintf("%s <%s>", p.fromName, p.fromEmail)
	message := p.mg.NewMessage(
		from,
		data.Subject,
		textContent,
		data.To...,
	)

	// Set HTML content
	message.SetHTML(htmlContent)

	debug.Info("sending email from %s to %v", from, data.To)

	// Send the message
	_, id, err := p.mg.Send(ctx, message)
	if err != nil {
		debug.Error("failed to send email: %v", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	debug.Info("successfully sent email with ID: %s", id)
	return nil
}

// TestConnection tests the connection to Mailgun
func (p *mailgunProvider) TestConnection(ctx context.Context, testEmail string) error {
	if p.mg == nil {
		debug.Error("mailgun client not initialized")
		return ErrProviderNotConfigured
	}

	from := fmt.Sprintf("%s <%s>", p.fromName, p.fromEmail)
	debug.Info("testing mailgun connection with test email to: %s", testEmail)

	// Create a test message
	message := p.mg.NewMessage(
		from,
		"KrakenHashes Email Test",
		"This is a test email from KrakenHashes.",
		testEmail,
	)

	// Attempt to send the test message
	_, id, err := p.mg.Send(ctx, message)
	if err != nil {
		debug.Error("mailgun test failed: %v", err)
		return fmt.Errorf("mailgun test failed: %w", err)
	}

	debug.Info("successfully sent test email with ID: %s", id)
	return nil
}

// executeTemplate executes a template with the given variables
func executeTemplate(tmpl *template.Template, vars map[string]string, result *string) error {
	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars); err != nil {
		return err
	}
	*result = buf.String()
	return nil
}
