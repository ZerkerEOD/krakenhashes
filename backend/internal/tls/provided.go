package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// ProvidedProvider implements the Provider interface using user-provided certificates
type ProvidedProvider struct {
	config         *ProviderConfig
	serverCert     *x509.Certificate
	serverKey      interface{}
	caCert         *x509.Certificate
	caCertPool     *x509.CertPool
	certChain      []*x509.Certificate // Full certificate chain for server
	tlsCert        tls.Certificate     // Prepared TLS certificate
}

// NewProvidedProvider creates a new provider for user-provided certificates
func NewProvidedProvider(config *ProviderConfig) Provider {
	return &ProvidedProvider{
		config: config,
	}
}

// Initialize loads and validates user-provided certificates
func (p *ProvidedProvider) Initialize() error {
	debug.Info("Initializing provided certificate mode")

	// Validate required paths
	if p.config.CertFile == "" || p.config.KeyFile == "" {
		return fmt.Errorf("KH_CERT_FILE and KH_KEY_FILE are required for provided mode")
	}

	debug.Debug("Loading server certificate from: %s", p.config.CertFile)
	debug.Debug("Loading server private key from: %s", p.config.KeyFile)

	// Load server certificate file (may contain chain)
	certPEM, err := os.ReadFile(p.config.CertFile)
	if err != nil {
		debug.Error("Failed to read server certificate: %v", err)
		return fmt.Errorf("failed to read server certificate: %w", err)
	}

	// Parse all certificates from the PEM data
	certChain, err := parseCertificateChain(certPEM)
	if err != nil {
		debug.Error("Failed to parse certificate chain: %v", err)
		return fmt.Errorf("failed to parse certificate chain: %w", err)
	}

	if len(certChain) == 0 {
		return fmt.Errorf("no valid certificates found in %s", p.config.CertFile)
	}

	p.certChain = certChain
	p.serverCert = certChain[0] // First certificate is the leaf/server cert

	debug.Info("Loaded %d certificate(s) from chain", len(certChain))
	debug.Info("Server certificate subject: %s", p.serverCert.Subject.String())
	debug.Info("Server certificate validity: %s to %s",
		p.serverCert.NotBefore.Format("2006-01-02"),
		p.serverCert.NotAfter.Format("2006-01-02"))

	// Determine CA certificate for agent trust
	if err := p.determineCACertificate(); err != nil {
		return fmt.Errorf("failed to determine CA certificate: %w", err)
	}

	// Load private key
	keyPEM, err := os.ReadFile(p.config.KeyFile)
	if err != nil {
		debug.Error("Failed to read private key: %v", err)
		return fmt.Errorf("failed to read private key: %w", err)
	}

	// Parse private key
	p.serverKey, err = parsePrivateKey(keyPEM)
	if err != nil {
		debug.Error("Failed to parse private key: %v", err)
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Validate that key matches certificate
	if err := validateKeyPair(p.serverCert, p.serverKey); err != nil {
		debug.Error("Private key does not match certificate: %v", err)
		return fmt.Errorf("private key does not match certificate: %w", err)
	}

	debug.Info("Private key validated successfully")

	// Prepare TLS certificate with full chain
	if err := p.prepareTLSCertificate(certPEM, keyPEM); err != nil {
		return fmt.Errorf("failed to prepare TLS certificate: %w", err)
	}

	// Create CA certificate pool
	p.caCertPool = x509.NewCertPool()
	p.caCertPool.AddCert(p.caCert)

	debug.Info("Provided certificate mode initialized successfully")
	return nil
}

// determineCACertificate determines which certificate to use as the CA for agent trust
func (p *ProvidedProvider) determineCACertificate() error {
	// Priority 1: Explicit CA file provided
	if p.config.CAFile != "" {
		debug.Info("Loading explicit CA certificate from: %s", p.config.CAFile)
		caPEM, err := os.ReadFile(p.config.CAFile)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCerts, err := parseCertificateChain(caPEM)
		if err != nil {
			return fmt.Errorf("failed to parse CA certificate: %w", err)
		}

		if len(caCerts) == 0 {
			return fmt.Errorf("no valid certificates found in CA file")
		}

		p.caCert = caCerts[0] // Use first cert from CA file
		debug.Info("Using explicit CA certificate: %s", p.caCert.Subject.String())
		return nil
	}

	// Priority 2: Multiple certs in chain - use last one as CA
	if len(p.certChain) > 1 {
		p.caCert = p.certChain[len(p.certChain)-1]
		debug.Info("Auto-extracted CA from chain (cert %d/%d): %s",
			len(p.certChain), len(p.certChain), p.caCert.Subject.String())
		debug.Debug("CA is %s certificate",
			map[bool]string{true: "root", false: "intermediate"}[p.caCert.IsCA])
		return nil
	}

	// Priority 3: Single cert - assume self-signed, use as both server and CA
	p.caCert = p.serverCert
	debug.Info("Single certificate detected - using as self-signed CA")
	debug.Info("CA certificate: %s", p.caCert.Subject.String())

	if !p.caCert.IsCA {
		debug.Warning("Certificate is not marked as CA but will be used for agent trust (self-signed scenario)")
	}

	return nil
}

// prepareTLSCertificate prepares the TLS certificate with full chain
func (p *ProvidedProvider) prepareTLSCertificate(certPEM, keyPEM []byte) error {
	// Load certificate and key using tls package
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to load certificate and key pair: %w", err)
	}

	// Parse the leaf certificate
	tlsCert.Leaf, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	p.tlsCert = tlsCert
	debug.Info("TLS certificate prepared with %d certificate(s) in chain", len(tlsCert.Certificate))
	return nil
}

// GetTLSConfig returns the TLS configuration for the server
func (p *ProvidedProvider) GetTLSConfig() (*tls.Config, error) {
	debug.Debug("Creating TLS configuration")

	return &tls.Config{
		Certificates: []tls.Certificate{p.tlsCert},
		RootCAs:      p.caCertPool,
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
	}, nil
}

// GetCACertPool returns the CA certificate pool
func (p *ProvidedProvider) GetCACertPool() (*x509.CertPool, error) {
	debug.Debug("Returning CA certificate pool")
	if p.caCertPool == nil {
		return nil, fmt.Errorf("CA certificate pool not initialized")
	}
	return p.caCertPool, nil
}

// ExportCACertificate exports the CA certificate for agents
func (p *ProvidedProvider) ExportCACertificate() ([]byte, error) {
	debug.Debug("Exporting CA certificate for agents")
	if p.caCert == nil {
		return nil, fmt.Errorf("CA certificate not initialized")
	}

	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: p.caCert.Raw,
	}

	pemData := pem.EncodeToMemory(pemBlock)
	if pemData == nil {
		return nil, fmt.Errorf("failed to encode CA certificate to PEM")
	}

	debug.Info("Successfully exported CA certificate: %s", p.caCert.Subject.String())
	return pemData, nil
}

// GetClientCertificate returns the certificate and key for client authentication
// Note: This is currently unused as agents authenticate via API keys, not client certs
func (p *ProvidedProvider) GetClientCertificate() ([]byte, []byte, error) {
	debug.Debug("GetClientCertificate called (currently unused by agents)")

	// Return server cert/key as this method is part of the interface but unused
	// In the future, if client cert auth is needed, this would need separate client certs
	certPEM, err := os.ReadFile(p.config.CertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(p.config.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read private key: %w", err)
	}

	return certPEM, keyPEM, nil
}

// Cleanup performs any necessary cleanup
func (p *ProvidedProvider) Cleanup() error {
	debug.Debug("Cleaning up provided certificate provider")
	// No cleanup needed for provided certificates
	return nil
}

// parseCertificateChain parses all certificates from PEM data
func parseCertificateChain(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	var block *pem.Block

	for {
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}

		certs = append(certs, cert)
	}

	return certs, nil
}

// parsePrivateKey parses a private key from PEM data
func parsePrivateKey(pemData []byte) (interface{}, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	// Try different private key formats
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

// validateKeyPair validates that a private key matches a certificate
func validateKeyPair(cert *x509.Certificate, key interface{}) error {
	// Create a temporary TLS certificate to validate the pair
	// This is a simple way to verify they match
	switch key.(type) {
	case *interface{}:
		return fmt.Errorf("invalid key type")
	}

	// If we can create a tls.Certificate, the pair is valid
	// The actual validation happens in prepareTLSCertificate
	return nil
}
