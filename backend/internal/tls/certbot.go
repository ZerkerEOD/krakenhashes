package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// CertbotProvider implements the Provider interface using Let's Encrypt certificates via certbot
type CertbotProvider struct {
	config *ProviderConfig
}

// NewCertbotProvider creates a new certbot-based TLS provider
func NewCertbotProvider(config *ProviderConfig) Provider {
	return &CertbotProvider{
		config: config,
	}
}

// Initialize sets up the certbot provider and obtains certificates if needed
func (p *CertbotProvider) Initialize() error {
	debug.Info("Initializing certbot TLS provider")

	// Validate certbot configuration
	if p.config.CertbotConfig == nil {
		return fmt.Errorf("certbot configuration is required")
	}

	if p.config.CertbotConfig.Domain == "" || p.config.CertbotConfig.Email == "" {
		return fmt.Errorf("domain and email are required for certbot")
	}

	// Check if certbot is installed
	if _, err := exec.LookPath("certbot"); err != nil {
		return fmt.Errorf("certbot is not installed: %w", err)
	}

	// Create Cloudflare credentials file
	if err := p.createCloudflareCredentials(); err != nil {
		return fmt.Errorf("failed to create Cloudflare credentials: %w", err)
	}

	// Check if certificates already exist
	certPath := filepath.Join(p.config.CertsDir, "live", p.config.CertbotConfig.Domain, "fullchain.pem")
	keyPath := filepath.Join(p.config.CertsDir, "live", p.config.CertbotConfig.Domain, "privkey.pem")

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		debug.Info("Certificates not found, obtaining new certificates from Let's Encrypt")
		if err := p.obtainCertificates(); err != nil {
			return fmt.Errorf("failed to obtain certificates: %w", err)
		}
	} else {
		debug.Info("Existing certificates found at %s", certPath)
		// Check if renewal is needed
		if p.shouldRenew() {
			debug.Info("Certificate renewal needed")
			if err := p.renewCertificates(); err != nil {
				debug.Error("Failed to renew certificates: %v", err)
				// Don't fail initialization if renewal fails - use existing certs
			}
		}
	}

	// Update config paths to point to Let's Encrypt certificates
	p.config.CertFile = certPath
	p.config.KeyFile = keyPath
	p.config.CAFile = filepath.Join(p.config.CertsDir, "live", p.config.CertbotConfig.Domain, "chain.pem")

	debug.Info("Certbot provider initialized successfully")
	return nil
}

// createCloudflareCredentials creates the Cloudflare API credentials file
func (p *CertbotProvider) createCloudflareCredentials() error {
	apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	if apiToken == "" {
		return fmt.Errorf("CLOUDFLARE_API_TOKEN environment variable is required")
	}

	credPath := filepath.Join(p.config.CertsDir, "cloudflare.ini")
	content := fmt.Sprintf("dns_cloudflare_api_token = %s\n", apiToken)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(p.config.CertsDir, 0755); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Write credentials file with restricted permissions
	if err := os.WriteFile(credPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write Cloudflare credentials: %w", err)
	}

	debug.Debug("Created Cloudflare credentials at %s", credPath)
	return nil
}

// obtainCertificates uses certbot to obtain new certificates
func (p *CertbotProvider) obtainCertificates() error {
	debug.Info("Obtaining certificates for domain: %s", p.config.CertbotConfig.Domain)

	args := []string{
		"certonly",
		"--non-interactive",
		"--agree-tos",
		"--email", p.config.CertbotConfig.Email,
		"--dns-cloudflare",
		"--dns-cloudflare-credentials", filepath.Join(p.config.CertsDir, "cloudflare.ini"),
		"--config-dir", p.config.CertsDir,
		"--work-dir", filepath.Join(p.config.CertsDir, "work"),
		"--logs-dir", filepath.Join(p.config.CertsDir, "logs"),
		"-d", p.config.CertbotConfig.Domain,
	}

	// Add custom ACME server if specified
	if p.config.CertbotConfig.Server != "" {
		debug.Info("Using custom ACME server: %s", p.config.CertbotConfig.Server)
		args = append(args, "--server", p.config.CertbotConfig.Server)
	}

	// Add staging flag if configured (only if no custom server specified)
	if p.config.CertbotConfig.Staging && p.config.CertbotConfig.Server == "" {
		debug.Info("Using Let's Encrypt staging environment")
		args = append(args, "--staging")
	}

	// Add additional domains if specified
	for _, domain := range p.config.AdditionalDNSNames {
		if domain != "" && domain != p.config.CertbotConfig.Domain {
			args = append(args, "-d", domain)
		}
	}

	// Add extra arguments for advanced configuration
	if p.config.CertbotConfig.ExtraArgs != "" {
		debug.Info("Adding extra certbot arguments: %s", p.config.CertbotConfig.ExtraArgs)
		extraArgs := strings.Fields(p.config.CertbotConfig.ExtraArgs)
		args = append(args, extraArgs...)
	}

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	debug.Debug("Running certbot command: certbot %s", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("certbot failed: %w", err)
	}

	debug.Info("Successfully obtained certificates")
	return nil
}

// renewCertificates attempts to renew existing certificates
func (p *CertbotProvider) renewCertificates() error {
	debug.Info("Attempting to renew certificates")

	args := []string{
		"renew",
		"--non-interactive",
		"--dns-cloudflare",
		"--dns-cloudflare-credentials", filepath.Join(p.config.CertsDir, "cloudflare.ini"),
		"--config-dir", p.config.CertsDir,
		"--work-dir", filepath.Join(p.config.CertsDir, "work"),
		"--logs-dir", filepath.Join(p.config.CertsDir, "logs"),
	}

	// Add custom ACME server if specified
	if p.config.CertbotConfig.Server != "" {
		args = append(args, "--server", p.config.CertbotConfig.Server)
	}

	// Add staging flag if configured (only if no custom server specified)
	if p.config.CertbotConfig.Staging && p.config.CertbotConfig.Server == "" {
		args = append(args, "--staging")
	}

	// Add renewal hook if specified
	if p.config.CertbotConfig.RenewHook != "" {
		args = append(args, "--deploy-hook", p.config.CertbotConfig.RenewHook)
	}

	// Add extra arguments for advanced configuration
	if p.config.CertbotConfig.ExtraArgs != "" {
		extraArgs := strings.Fields(p.config.CertbotConfig.ExtraArgs)
		args = append(args, extraArgs...)
	}

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("certbot renewal failed: %w", err)
	}

	debug.Info("Certificate renewal completed")
	return nil
}

// shouldRenew checks if certificates should be renewed (30 days before expiry)
func (p *CertbotProvider) shouldRenew() bool {
	certPath := filepath.Join(p.config.CertsDir, "live", p.config.CertbotConfig.Domain, "fullchain.pem")
	
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		debug.Error("Failed to read certificate for renewal check: %v", err)
		return false
	}

	block, _ := decodePEMBlock(certPEM)
	if block == nil {
		debug.Error("Failed to decode certificate PEM")
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		debug.Error("Failed to parse certificate: %v", err)
		return false
	}

	// Renew if less than 30 days until expiry
	daysUntilExpiry := time.Until(cert.NotAfter).Hours() / 24
	shouldRenew := daysUntilExpiry < 30

	debug.Debug("Certificate expires in %.0f days, renewal needed: %v", daysUntilExpiry, shouldRenew)
	return shouldRenew
}

// GetTLSConfig returns the TLS configuration using Let's Encrypt certificates
func (p *CertbotProvider) GetTLSConfig() (*tls.Config, error) {
	debug.Debug("Loading TLS configuration from Let's Encrypt certificates")

	cert, err := tls.LoadX509KeyPair(p.config.CertFile, p.config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate and key: %w", err)
	}

	// Load CA certificate pool
	caCertPool, err := p.GetCACertPool()
	if err != nil {
		debug.Warning("Failed to load CA certificate pool: %v", err)
		// Continue without CA pool - not critical for server operation
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		PreferServerCipherSuites: true,
	}

	debug.Debug("TLS configuration loaded successfully")
	return tlsConfig, nil
}

// GetCACertPool returns the CA certificate pool (Let's Encrypt intermediate certificate)
func (p *CertbotProvider) GetCACertPool() (*x509.CertPool, error) {
	debug.Debug("Loading CA certificate pool")

	if p.config.CAFile == "" {
		debug.Debug("No CA file specified")
		return nil, nil
	}

	caCert, err := os.ReadFile(p.config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	debug.Debug("CA certificate pool loaded successfully")
	return caCertPool, nil
}

// ExportCACertificate exports the CA certificate (Let's Encrypt intermediate)
func (p *CertbotProvider) ExportCACertificate() ([]byte, error) {
	debug.Debug("Exporting CA certificate")

	if p.config.CAFile == "" {
		return nil, fmt.Errorf("no CA certificate available")
	}

	caCert, err := os.ReadFile(p.config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	return caCert, nil
}

// GetClientCertificate returns the certificate and key for client authentication
func (p *CertbotProvider) GetClientCertificate() ([]byte, []byte, error) {
	debug.Debug("Loading client certificate and key")

	cert, err := os.ReadFile(p.config.CertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	key, err := os.ReadFile(p.config.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read private key: %w", err)
	}

	return cert, key, nil
}

// Cleanup performs cleanup operations
func (p *CertbotProvider) Cleanup() error {
	debug.Debug("Cleaning up certbot provider")
	
	// Remove Cloudflare credentials file
	credPath := filepath.Join(p.config.CertsDir, "cloudflare.ini")
	if err := os.Remove(credPath); err != nil && !os.IsNotExist(err) {
		debug.Warning("Failed to remove Cloudflare credentials: %v", err)
	}
	
	return nil
}

// StartAutoRenewal starts a goroutine to check for certificate renewal periodically
func (p *CertbotProvider) StartAutoRenewal() {
	if !p.config.CertbotConfig.AutoRenew {
		debug.Info("Auto-renewal is disabled")
		return
	}

	debug.Info("Starting auto-renewal goroutine")
	go func() {
		// Check twice daily
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if p.shouldRenew() {
				debug.Info("Auto-renewal check: renewal needed")
				if err := p.renewCertificates(); err != nil {
					debug.Error("Auto-renewal failed: %v", err)
				} else {
					debug.Info("Auto-renewal completed successfully")
				}
			} else {
				debug.Debug("Auto-renewal check: no renewal needed")
			}
		}
	}()
}

// decodePEMBlock decodes the first PEM block from the given data
func decodePEMBlock(data []byte) (*pem.Block, []byte) {
	return pem.Decode(data)
}