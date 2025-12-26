package providers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	emailtypes "github.com/ZerkerEOD/krakenhashes/backend/pkg/email"
	"github.com/google/uuid"
)

// SMTPConfig represents SMTP-specific configuration
type SMTPConfig struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	FromEmail     string `json:"from_email"`
	FromName      string `json:"from_name"`
	Encryption    string `json:"encryption"`     // "none", "starttls", or "tls"
	SkipTLSVerify bool   `json:"skip_tls_verify,omitempty"`
}

// smtpProvider implements the Provider interface for SMTP
type smtpProvider struct {
	config   SMTPConfig
	password string // from APIKey field
}

// init registers the SMTP provider
func init() {
	Register(emailtypes.ProviderSMTP, func() Provider {
		return &smtpProvider{}
	})
}

// Initialize sets up the SMTP provider
func (p *smtpProvider) Initialize(cfg *emailtypes.Config) error {
	if cfg.APIKey == "" {
		debug.Error("smtp password not provided")
		return ErrProviderNotConfigured
	}

	var smtpConfig SMTPConfig
	if err := json.Unmarshal(cfg.AdditionalConfig, &smtpConfig); err != nil {
		debug.Error("failed to parse smtp config: %v", err)
		return fmt.Errorf("invalid smtp configuration: %w", err)
	}

	// Validate required fields
	if smtpConfig.Host == "" {
		debug.Error("smtp host not provided")
		return errors.New("smtp host is required")
	}

	if smtpConfig.Username == "" {
		debug.Error("smtp username not provided")
		return errors.New("smtp username is required")
	}

	if smtpConfig.FromEmail == "" {
		debug.Error("smtp from_email not provided")
		return errors.New("smtp from_email is required")
	}

	if smtpConfig.FromName == "" {
		debug.Error("smtp from_name not provided")
		return errors.New("smtp from_name is required")
	}

	// Validate encryption mode
	if smtpConfig.Encryption != "none" && smtpConfig.Encryption != "starttls" && smtpConfig.Encryption != "tls" {
		debug.Error("invalid smtp encryption mode: %s", smtpConfig.Encryption)
		return errors.New("smtp encryption must be 'none', 'starttls', or 'tls'")
	}

	// Set default ports if not specified
	if smtpConfig.Port == 0 {
		switch smtpConfig.Encryption {
		case "none":
			smtpConfig.Port = 25
		case "starttls":
			smtpConfig.Port = 587
		case "tls":
			smtpConfig.Port = 465
		}
		debug.Info("smtp port not specified, using default port %d for encryption mode %s", smtpConfig.Port, smtpConfig.Encryption)
	}

	p.config = smtpConfig
	p.password = cfg.APIKey

	debug.Info("initialized smtp provider for host %s:%d with encryption %s, sender: %s <%s>",
		smtpConfig.Host, smtpConfig.Port, smtpConfig.Encryption, smtpConfig.FromName, smtpConfig.FromEmail)

	return nil
}

// ValidateConfig validates the SMTP configuration
func (p *smtpProvider) ValidateConfig(cfg *emailtypes.Config) error {
	if cfg.APIKey == "" {
		debug.Error("smtp password not provided")
		return errors.New("smtp password is required")
	}

	var smtpConfig SMTPConfig
	if err := json.Unmarshal(cfg.AdditionalConfig, &smtpConfig); err != nil {
		debug.Error("failed to parse smtp config: %v", err)
		return fmt.Errorf("invalid smtp configuration: %w", err)
	}

	if smtpConfig.Host == "" {
		debug.Error("smtp host not provided")
		return errors.New("smtp host is required")
	}

	if smtpConfig.Username == "" {
		debug.Error("smtp username not provided")
		return errors.New("smtp username is required")
	}

	if smtpConfig.FromEmail == "" {
		debug.Error("smtp from_email not provided")
		return errors.New("smtp from_email is required")
	}

	if smtpConfig.FromName == "" {
		debug.Error("smtp from_name not provided")
		return errors.New("smtp from_name is required")
	}

	if smtpConfig.Encryption != "none" && smtpConfig.Encryption != "starttls" && smtpConfig.Encryption != "tls" {
		debug.Error("invalid smtp encryption mode: %s", smtpConfig.Encryption)
		return errors.New("smtp encryption must be 'none', 'starttls', or 'tls'")
	}

	debug.Info("validated smtp configuration for host %s:%d with encryption %s", smtpConfig.Host, smtpConfig.Port, smtpConfig.Encryption)
	return nil
}

// Send sends an email using SMTP
func (p *smtpProvider) Send(ctx context.Context, data *emailtypes.EmailData) error {
	if p.config.Host == "" {
		debug.Error("smtp provider not initialized")
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

	// Build the email message
	from := fmt.Sprintf("%s <%s>", p.config.FromName, p.config.FromEmail)
	message := p.buildMessage(from, data.To, data.Subject, textContent, htmlContent)

	debug.Info("sending email from %s to %v via SMTP %s:%d", from, data.To, p.config.Host, p.config.Port)

	// Send based on encryption mode
	var err error
	switch p.config.Encryption {
	case "none":
		err = p.sendPlain(message, data.To)
	case "starttls":
		err = p.sendSTARTTLS(message, data.To)
	case "tls":
		err = p.sendTLS(message, data.To)
	default:
		return fmt.Errorf("unsupported encryption mode: %s", p.config.Encryption)
	}

	if err != nil {
		debug.Error("failed to send email: %v", err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	debug.Info("successfully sent email via SMTP")
	return nil
}

// TestConnection tests the connection to the SMTP server
func (p *smtpProvider) TestConnection(ctx context.Context, testEmail string) error {
	if p.config.Host == "" {
		debug.Error("smtp provider not initialized")
		return ErrProviderNotConfigured
	}

	from := fmt.Sprintf("%s <%s>", p.config.FromName, p.config.FromEmail)
	subject := "KrakenHashes Email Test"
	textContent := "This is a test email from KrakenHashes."
	htmlContent := "<p>This is a test email from KrakenHashes.</p>"

	message := p.buildMessage(from, []string{testEmail}, subject, textContent, htmlContent)

	debug.Info("testing smtp connection to %s:%d with encryption %s", p.config.Host, p.config.Port, p.config.Encryption)

	var err error
	switch p.config.Encryption {
	case "none":
		err = p.sendPlain(message, []string{testEmail})
	case "starttls":
		err = p.sendSTARTTLS(message, []string{testEmail})
	case "tls":
		err = p.sendTLS(message, []string{testEmail})
	default:
		return fmt.Errorf("unsupported encryption mode: %s", p.config.Encryption)
	}

	if err != nil {
		debug.Error("smtp test failed: %v", err)
		return fmt.Errorf("smtp test failed: %w", err)
	}

	debug.Info("successfully sent test email to: %s", testEmail)
	return nil
}

// buildMessage constructs an RFC 5322 compliant email message
func (p *smtpProvider) buildMessage(from string, to []string, subject, textContent, htmlContent string) []byte {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	// Date header (RFC 5322 required)
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	// Message-ID header (RFC 5322 recommended)
	domain := p.config.FromEmail[strings.LastIndex(p.config.FromEmail, "@")+1:]
	buf.WriteString(fmt.Sprintf("Message-ID: <%s@%s>\r\n", uuid.New().String(), domain))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/alternative; boundary=\"boundary-krakenhashes\"\r\n")
	buf.WriteString("\r\n")

	// Plain text part
	buf.WriteString("--boundary-krakenhashes\r\n")
	buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(textContent)
	buf.WriteString("\r\n")

	// HTML part
	buf.WriteString("--boundary-krakenhashes\r\n")
	buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(htmlContent)
	buf.WriteString("\r\n")

	buf.WriteString("--boundary-krakenhashes--\r\n")

	return []byte(buf.String())
}

// sendPlain sends email without encryption
func (p *smtpProvider) sendPlain(message []byte, to []string) error {
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)

	// Dial with timeout
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Mail(p.config.FromEmail); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to start data command: %w", err)
	}
	defer wc.Close()

	if _, err := wc.Write(message); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// sendSTARTTLS sends email using STARTTLS (upgrade plain connection to TLS)
func (p *smtpProvider) sendSTARTTLS(message []byte, to []string) error {
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)

	// Dial with timeout
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Start TLS
	tlsConfig := &tls.Config{
		ServerName:         p.config.Host,
		InsecureSkipVerify: p.config.SkipTLSVerify,
	}

	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("failed to start TLS: %w", err)
	}

	// Authenticate
	auth := smtp.PlainAuth("", p.config.Username, p.password, p.config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if err := client.Mail(p.config.FromEmail); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to start data command: %w", err)
	}
	defer wc.Close()

	if _, err := wc.Write(message); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// sendTLS sends email using direct TLS connection (implicit TLS)
func (p *smtpProvider) sendTLS(message []byte, to []string) error {
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)

	// Establish TLS connection with timeout
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	tlsConfig := &tls.Config{
		ServerName:         p.config.Host,
		InsecureSkipVerify: p.config.SkipTLSVerify,
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to establish TLS connection: %w", err)
	}
	defer conn.Close()

	// Create SMTP client on top of TLS connection
	client, err := smtp.NewClient(conn, p.config.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Authenticate
	auth := smtp.PlainAuth("", p.config.Username, p.password, p.config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if err := client.Mail(p.config.FromEmail); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to start data command: %w", err)
	}
	defer wc.Close()

	if _, err := wc.Write(message); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}
