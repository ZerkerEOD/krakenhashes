package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SelfSignedProvider implements the Provider interface for self-signed certificates
type SelfSignedProvider struct {
	config *ProviderConfig

	// mu guards every certificate field below. ExportCACertificate and
	// GetClientCertificate are called per-request from live HTTP handlers, so a
	// reissue running concurrently would otherwise be a genuine data race.
	mu         sync.Mutex
	ca         *x509.Certificate
	caKey      *rsa.PrivateKey
	cert       *x509.Certificate
	certKey    *rsa.PrivateKey
	caCertPool *x509.CertPool
	// Add client certificate fields
	clientCert *x509.Certificate
	clientKey  *rsa.PrivateKey

	// current is the leaf handed to every new TLS handshake. Publishing through
	// an atomic pointer is what lets a certificate reissued hours after startup
	// take effect without restarting the process.
	current atomic.Pointer[tls.Certificate]
}

// NewSelfSignedProvider creates a new self-signed certificate provider
func NewSelfSignedProvider(config *ProviderConfig) *SelfSignedProvider {
	return &SelfSignedProvider{
		config: config,
	}
}

// Initialize sets up the self-signed certificate provider
func (p *SelfSignedProvider) Initialize() error {
	debug.Info("Initializing self-signed certificate provider")

	// Create certificates directory if it doesn't exist
	debug.Debug("Creating certificates directory: %s", p.config.CertsDir)
	if err := os.MkdirAll(p.config.CertsDir, 0755); err != nil {
		debug.Error("Failed to create certificates directory: %v", err)
		return fmt.Errorf("failed to create certificates directory: %w", err)
	}

	// Set default file paths if not provided
	if p.config.CertFile == "" {
		p.config.CertFile = filepath.Join(p.config.CertsDir, "server.crt")
		debug.Debug("Using default server certificate path: %s", p.config.CertFile)
	}
	if p.config.KeyFile == "" {
		p.config.KeyFile = filepath.Join(p.config.CertsDir, "server.key")
		debug.Debug("Using default server key path: %s", p.config.KeyFile)
	}
	if p.config.CAFile == "" {
		p.config.CAFile = filepath.Join(p.config.CertsDir, "ca.crt")
		debug.Debug("Using default CA certificate path: %s", p.config.CAFile)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if certificates already exist.
	//
	// ca.key is included in the gate even though only loadExistingCertificates
	// reads it: without it, a certs directory missing just the CA key took the
	// load path and aborted startup instead of regenerating.
	if fileExists(p.config.CertFile) && fileExists(p.config.KeyFile) && fileExists(p.config.CAFile) &&
		fileExists(filepath.Join(p.config.CertsDir, "ca.key")) &&
		fileExists(filepath.Join(p.config.CertsDir, "client.crt")) && fileExists(filepath.Join(p.config.CertsDir, "client.key")) {
		debug.Info("Found existing certificates, loading them")
		if err := p.loadExistingCertificates(); err != nil {
			return err
		}
		p.storeCurrentLeaf()
		p.logServerCertificateSANs()
		return nil
	}

	debug.Info("No existing certificates found, generating new ones")
	if err := p.generateNewCertificates(); err != nil {
		return err
	}
	p.storeCurrentLeaf()
	return nil
}

// logServerCertificateSANs states plainly what the loaded certificate actually
// covers.
//
// Historically the only way to answer "why can my agent not connect?" was to run
// openssl against the certs directory, because nothing logged the SAN list. That
// silence is a large part of why a certificate covering only 127.0.0.1 could sit
// in a deployment unnoticed.
func (p *SelfSignedProvider) logServerCertificateSANs() {
	if p.cert == nil {
		return
	}
	ips := make([]string, 0, len(p.cert.IPAddresses))
	for _, ip := range p.cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	debug.Info("Server certificate covers DNS=%v IP=%v (expires %s)",
		p.cert.DNSNames, ips, p.cert.NotAfter.Format(time.RFC3339))
}

// storeCurrentLeaf publishes the in-memory server leaf as the certificate served
// to new handshakes. Called at the end of both Initialize branches and at the end
// of every successful reissue.
//
// The caller must hold p.mu.
func (p *SelfSignedProvider) storeCurrentLeaf() {
	if p.cert == nil || p.certKey == nil || p.ca == nil {
		return
	}
	p.current.Store(&tls.Certificate{
		// The CA is included so the server presents a full chain; clients that
		// already trust the CA do not need it, but browsers importing the CA
		// mid-session and any intermediate-tolerant verifier do.
		Certificate: [][]byte{p.cert.Raw, p.ca.Raw},
		PrivateKey:  p.certKey,
		Leaf:        p.cert,
	})
}

// getCertificate is the tls.Config callback. Every new handshake reads the atomic
// pointer, so a reissue takes effect on the next connection with no restart.
func (p *SelfSignedProvider) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := p.current.Load()
	if cert == nil {
		return nil, fmt.Errorf("no server certificate is loaded")
	}
	return cert, nil
}

// GetTLSConfig returns the TLS configuration for the server
func (p *SelfSignedProvider) GetTLSConfig() (*tls.Config, error) {
	debug.Debug("Getting TLS configuration")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cert == nil || p.certKey == nil {
		debug.Error("Certificates not initialized")
		return nil, fmt.Errorf("certificates not initialized")
	}

	// Create CA certificate pool if not already created
	if p.caCertPool == nil {
		debug.Debug("Creating CA certificate pool")
		p.caCertPool = x509.NewCertPool()
		p.caCertPool.AddCert(p.ca)
	}

	p.storeCurrentLeaf()

	debug.Debug("Creating TLS configuration with secure defaults")
	return &tls.Config{
		// Certificates is deliberately left nil.
		//
		// crypto/tls consults GetCertificate only when Certificates is empty OR
		// the ClientHello carries SNI -- and a Go client dialling a bare IP such
		// as https://192.168.1.50:31337 sends no SNI at all, because IP literals
		// are stripped from the SNI extension. Populating both fields would make
		// the hot swap a silent no-op for exactly the IP-addressed agents this
		// callback exists to serve.
		//
		// net/http treats a non-nil GetCertificate as satisfying its
		// "config has a certificate" check, so ListenAndServeTLS("", "") still
		// works with no certificate file paths.
		GetCertificate: p.getCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
	}, nil
}

// GetCACertPool returns the CA certificate pool
func (p *SelfSignedProvider) GetCACertPool() (*x509.CertPool, error) {
	debug.Debug("Getting CA certificate pool")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.caCertPool == nil {
		debug.Error("CA certificate pool not initialized")
		return nil, fmt.Errorf("CA certificate pool not initialized")
	}
	return p.caCertPool, nil
}

// Cleanup performs any necessary cleanup
func (p *SelfSignedProvider) Cleanup() error {
	// No cleanup needed for self-signed certificates
	return nil
}

// loadExistingCertificates loads existing certificates from disk
func (p *SelfSignedProvider) loadExistingCertificates() error {
	debug.Info("Loading existing certificates")

	// Load CA certificate
	debug.Debug("Loading CA certificate from: %s", p.config.CAFile)
	caCertPEM, err := os.ReadFile(p.config.CAFile)
	if err != nil {
		debug.Error("Failed to read CA certificate: %v", err)
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		debug.Error("Failed to decode CA certificate PEM")
		return fmt.Errorf("failed to decode CA certificate PEM")
	}

	p.ca, err = x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse CA certificate: %v", err)
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Load CA private key
	debug.Debug("Loading CA private key from: %s", filepath.Join(p.config.CertsDir, "ca.key"))
	caKeyPEM, err := os.ReadFile(filepath.Join(p.config.CertsDir, "ca.key"))
	if err != nil {
		debug.Error("Failed to read CA private key: %v", err)
		return fmt.Errorf("failed to read CA private key: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		debug.Error("Failed to decode CA private key PEM")
		return fmt.Errorf("failed to decode CA private key PEM")
	}

	p.caKey, err = x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse CA private key: %v", err)
		return fmt.Errorf("failed to parse CA private key: %w", err)
	}

	// Verify CA key matches certificate
	if !p.ca.PublicKey.(*rsa.PublicKey).Equal(&p.caKey.PublicKey) {
		debug.Error("CA private key does not match certificate")
		return fmt.Errorf("CA private key does not match certificate")
	}

	debug.Info("Successfully loaded CA certificate and private key")

	// Load server certificate
	debug.Debug("Loading server certificate from: %s", p.config.CertFile)
	certPEM, err := os.ReadFile(p.config.CertFile)
	if err != nil {
		debug.Error("Failed to read server certificate: %v", err)
		return fmt.Errorf("failed to read server certificate: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		debug.Error("Failed to decode server certificate PEM")
		return fmt.Errorf("failed to decode server certificate PEM")
	}

	p.cert, err = x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse server certificate: %v", err)
		return fmt.Errorf("failed to parse server certificate: %w", err)
	}

	// Load server private key
	debug.Debug("Loading server private key from: %s", p.config.KeyFile)
	keyPEM, err := os.ReadFile(p.config.KeyFile)
	if err != nil {
		debug.Error("Failed to read server private key: %v", err)
		return fmt.Errorf("failed to read server private key: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		debug.Error("Failed to decode server private key PEM")
		return fmt.Errorf("failed to decode server private key PEM")
	}

	p.certKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse server private key: %v", err)
		return fmt.Errorf("failed to parse server private key: %w", err)
	}

	// Create CA certificate pool
	debug.Debug("Creating CA certificate pool")
	p.caCertPool = x509.NewCertPool()
	p.caCertPool.AddCert(p.ca)

	// Load client certificate
	debug.Debug("Loading client certificate from: %s", filepath.Join(p.config.CertsDir, "client.crt"))
	clientCertPEM, err := os.ReadFile(filepath.Join(p.config.CertsDir, "client.crt"))
	if err != nil {
		debug.Error("Failed to read client certificate: %v", err)
		return fmt.Errorf("failed to read client certificate: %w", err)
	}

	clientCertBlock, _ := pem.Decode(clientCertPEM)
	if clientCertBlock == nil {
		debug.Error("Failed to decode client certificate PEM")
		return fmt.Errorf("failed to decode client certificate PEM")
	}

	p.clientCert, err = x509.ParseCertificate(clientCertBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse client certificate: %v", err)
		return fmt.Errorf("failed to parse client certificate: %w", err)
	}

	// Load client private key
	debug.Debug("Loading client private key from: %s", filepath.Join(p.config.CertsDir, "client.key"))
	clientKeyPEM, err := os.ReadFile(filepath.Join(p.config.CertsDir, "client.key"))
	if err != nil {
		debug.Error("Failed to read client private key: %v", err)
		return fmt.Errorf("failed to read client private key: %w", err)
	}

	clientKeyBlock, _ := pem.Decode(clientKeyPEM)
	if clientKeyBlock == nil {
		debug.Error("Failed to decode client private key PEM")
		return fmt.Errorf("failed to decode client private key PEM")
	}

	p.clientKey, err = x509.ParsePKCS1PrivateKey(clientKeyBlock.Bytes)
	if err != nil {
		debug.Error("Failed to parse client private key: %v", err)
		return fmt.Errorf("failed to parse client private key: %w", err)
	}

	debug.Info("Successfully loaded existing certificates")
	return nil
}

// issueCA generates a new self-signed certificate authority.
//
// Shared by first generation and by CA rotation so the two cannot diverge.
// Returns without touching provider state; the caller decides when to adopt it.
func (p *SelfSignedProvider) issueCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	debug.Debug("Generating CA key pair with key size: %d", p.config.KeySize)
	caKey, err := rsa.GenerateKey(rand.Reader, p.config.KeySize)
	if err != nil {
		debug.Error("Failed to generate CA private key: %v", err)
		return nil, nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	caSubjectKeyID, err := generateSubjectKeyID(&caKey.PublicKey)
	if err != nil {
		debug.Error("Failed to generate CA subject key identifier: %v", err)
		return nil, nil, fmt.Errorf("failed to generate CA subject key identifier: %w", err)
	}

	caSerial, err := generateRandomSerial()
	if err != nil {
		debug.Error("Failed to generate CA serial number: %v", err)
		return nil, nil, fmt.Errorf("failed to generate CA serial number: %w", err)
	}

	debug.Debug("Creating CA certificate with validity: %d days", p.config.Validity.CA)
	caTemplate := &x509.Certificate{
		SerialNumber: caSerial,
		Subject: pkix.Name{
			Country:            []string{p.config.CADetails.Country},
			Organization:       []string{p.config.CADetails.Organization},
			OrganizationalUnit: []string{p.config.CADetails.OrganizationalUnit},
			CommonName:         p.config.CADetails.CommonName,
		},
		NotBefore:             time.Now().Add(-24 * time.Hour), // Valid from 24 hours ago to handle clock skew
		NotAfter:              time.Now().AddDate(0, 0, p.config.Validity.CA),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny}, // Allow any extended usage for CA
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            2, // Allow up to 2 intermediate CAs
		MaxPathLenZero:        false,
		SubjectKeyId:          caSubjectKeyID,
		AuthorityKeyId:        caSubjectKeyID, // Self-signed
	}

	debug.Debug("Self-signing CA certificate")
	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		debug.Error("Failed to create CA certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	ca, err := x509.ParseCertificate(caBytes)
	if err != nil {
		debug.Error("Failed to parse CA certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	return ca, caKey, nil
}

// generateNewCertificates generates new CA and server certificates
func (p *SelfSignedProvider) generateNewCertificates() error {
	debug.Info("Generating new certificates")

	ca, caKey, err := p.issueCA()
	if err != nil {
		return err
	}
	p.ca, p.caKey = ca, caKey

	// Generate the server leaf. SANs come from BuildServerSANs so that first
	// generation and later reissues can never produce a different set from the
	// same inputs -- the startup drift check compares the two.
	sans := BuildServerSANs(p.config, DesiredSANs{
		DNSNames:    p.config.AdditionalDNSNames,
		IPAddresses: p.config.AdditionalIPAddresses,
	})

	p.cert, p.certKey, err = p.issueServerLeaf(p.ca, caKey, sans)
	if err != nil {
		return err
	}

	p.clientCert, p.clientKey, err = p.issueClientLeaf(p.ca, caKey)
	if err != nil {
		return err
	}

	// Log certificate details for debugging
	debug.Info("CA Certificate Details:")
	debug.Info("  Subject: %s", p.ca.Subject.String())
	debug.Info("  Validity: %s to %s", p.ca.NotBefore.Format(time.RFC3339), p.ca.NotAfter.Format(time.RFC3339))
	debug.Info("  Serial: %s", p.ca.SerialNumber.String())
	debug.Info("  Is CA: %v", p.ca.IsCA)

	debug.Info("Server Certificate Details:")
	debug.Info("  Subject: %s", p.cert.Subject.String())
	debug.Info("  Validity: %s to %s", p.cert.NotBefore.Format(time.RFC3339), p.cert.NotAfter.Format(time.RFC3339))
	debug.Info("  DNS Names: %v", p.cert.DNSNames)
	debug.Info("  IP Addresses: %v", p.cert.IPAddresses)
	debug.Info("  Serial: %s", p.cert.SerialNumber.String())

	// Create the CA pool up front so GetCACertPool does not have to.
	p.caCertPool = x509.NewCertPool()
	p.caCertPool.AddCert(p.ca)

	// Save all certificates
	debug.Info("Saving certificates to disk")
	if err := p.saveCertificates(); err != nil {
		debug.Error("Failed to save certificates: %v", err)
		return fmt.Errorf("failed to save certificates: %w", err)
	}

	debug.Info("Successfully generated and saved all certificates")
	return nil
}

// issueServerLeaf builds and signs a server certificate under the supplied CA.
//
// Shared by first generation and by reissue so the two can never diverge in key
// usage, validity, or extension set. Returns the new certificate and key without
// touching provider state; the caller decides when to adopt them.
func (p *SelfSignedProvider) issueServerLeaf(ca *x509.Certificate, caKey *rsa.PrivateKey, sans SANSet) (*x509.Certificate, *rsa.PrivateKey, error) {
	debug.Debug("Generating server key pair with key size: %d", p.config.KeySize)
	serverKey, err := rsa.GenerateKey(rand.Reader, p.config.KeySize)
	if err != nil {
		debug.Error("Failed to generate server private key: %v", err)
		return nil, nil, fmt.Errorf("failed to generate server private key: %w", err)
	}

	debug.Debug("Creating server certificate with validity: %d days", p.config.Validity.Server)

	serverSubjectKeyID, err := generateSubjectKeyID(&serverKey.PublicKey)
	if err != nil {
		debug.Error("Failed to generate server subject key identifier: %v", err)
		return nil, nil, fmt.Errorf("failed to generate server subject key identifier: %w", err)
	}

	serverSerial, err := generateRandomSerial()
	if err != nil {
		debug.Error("Failed to generate server serial number: %v", err)
		return nil, nil, fmt.Errorf("failed to generate server serial number: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject: pkix.Name{
			Country:            []string{p.config.CADetails.Country},
			Organization:       []string{p.config.CADetails.Organization},
			OrganizationalUnit: []string{"KrakenHashes Server"},
			CommonName:         "KrakenHashes Server",
		},
		NotBefore:             time.Now().Add(-24 * time.Hour), // Valid from 24 hours ago to handle clock skew
		NotAfter:              time.Now().AddDate(0, 0, p.config.Validity.Server),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		MaxPathLen:            -1, // Not a CA
		DNSNames:              sans.DNSNames,
		IPAddresses:           sans.IPAddresses,
		SubjectKeyId:          serverSubjectKeyID,
		// Taken from the CA rather than a local variable: on the reissue path the
		// CA was loaded from disk and there is no freshly-computed key ID around.
		AuthorityKeyId: ca.SubjectKeyId,
	}

	debug.Debug("Signing server certificate with CA")
	serverBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		debug.Error("Failed to create server certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	serverCert, err := x509.ParseCertificate(serverBytes)
	if err != nil {
		debug.Error("Failed to parse server certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to parse server certificate: %w", err)
	}

	return serverCert, serverKey, nil
}

// issueClientLeaf builds and signs the shared agent client certificate.
//
// It carries no SANs, so a SAN change never requires reissuing it.
func (p *SelfSignedProvider) issueClientLeaf(ca *x509.Certificate, caKey *rsa.PrivateKey) (*x509.Certificate, *rsa.PrivateKey, error) {
	debug.Debug("Generating shared client key pair with key size: %d", p.config.KeySize)
	clientKey, err := rsa.GenerateKey(rand.Reader, p.config.KeySize)
	if err != nil {
		debug.Error("Failed to generate client private key: %v", err)
		return nil, nil, fmt.Errorf("failed to generate client private key: %w", err)
	}

	clientSubjectKeyID, err := generateSubjectKeyID(&clientKey.PublicKey)
	if err != nil {
		debug.Error("Failed to generate client subject key identifier: %v", err)
		return nil, nil, fmt.Errorf("failed to generate client subject key identifier: %w", err)
	}

	clientSerial, err := generateRandomSerial()
	if err != nil {
		debug.Error("Failed to generate client serial number: %v", err)
		return nil, nil, fmt.Errorf("failed to generate client serial number: %w", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: clientSerial,
		Subject: pkix.Name{
			Country:            []string{p.config.CADetails.Country},
			Organization:       []string{p.config.CADetails.Organization},
			OrganizationalUnit: []string{"KrakenHashes Client"},
			CommonName:         "KrakenHashes Client",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, p.config.Validity.Server),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		SubjectKeyId:          clientSubjectKeyID,
		AuthorityKeyId:        ca.SubjectKeyId,
	}

	debug.Debug("Signing client certificate with CA")
	clientBytes, err := x509.CreateCertificate(rand.Reader, clientTemplate, ca, &clientKey.PublicKey, caKey)
	if err != nil {
		debug.Error("Failed to create client certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	clientCert, err := x509.ParseCertificate(clientBytes)
	if err != nil {
		debug.Error("Failed to parse client certificate: %v", err)
		return nil, nil, fmt.Errorf("failed to parse client certificate: %w", err)
	}

	return clientCert, clientKey, nil
}

// saveCertificates saves the certificates to disk
//
// Uses the same atomic writer as the reissue path so that certificate file modes
// are explicit (0644 for certificates, 0600 for keys) rather than dependent on
// the process umask, and so a crash mid-write can never leave a truncated
// certificate behind.
func (p *SelfSignedProvider) saveCertificates() error {
	certsDir := p.config.CertsDir

	writes := []struct {
		path      string
		blockType string
		der       []byte
		mode      os.FileMode
	}{
		{p.config.CAFile, "CERTIFICATE", p.ca.Raw, certFileMode},
		{filepath.Join(certsDir, "ca.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(p.caKey), keyFileMode},
		{p.config.CertFile, "CERTIFICATE", p.cert.Raw, certFileMode},
		{p.config.KeyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(p.certKey), keyFileMode},
		{filepath.Join(certsDir, "client.crt"), "CERTIFICATE", p.clientCert.Raw, certFileMode},
		{filepath.Join(certsDir, "client.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(p.clientKey), keyFileMode},
	}

	for _, w := range writes {
		debug.Debug("Saving %s", w.path)
		if err := writePEMAtomic(w.path, w.blockType, w.der, w.mode); err != nil {
			debug.Error("Failed to save %s: %v", w.path, err)
			return fmt.Errorf("failed to save %s: %w", w.path, err)
		}
	}

	debug.Info("Successfully saved all certificates")
	return nil
}

// fileExists reports whether path is a readable existing file.
//
// Only a successful stat counts. The previous `!os.IsNotExist(err)` form returned
// true for any other error -- notably EACCES after a PUID/PGID remap -- which
// sent Initialize down the load path, where the read then failed and aborted
// startup instead of regenerating.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// generateRandomSerial generates a cryptographically random serial number for certificates
func generateRandomSerial() (*big.Int, error) {
	// Generate 16 bytes (128 bits) of random data
	serialBytes := make([]byte, 16)
	if _, err := rand.Read(serialBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random serial: %w", err)
	}
	
	// Ensure the serial number is positive by clearing the MSB
	serialBytes[0] &= 0x7F
	
	// Convert to big.Int
	serial := new(big.Int).SetBytes(serialBytes)
	return serial, nil
}

// generateSubjectKeyID generates a subject key identifier from public key
func generateSubjectKeyID(pub interface{}) ([]byte, error) {
	var pubBytes []byte
	switch pub := pub.(type) {
	case *rsa.PublicKey:
		pubDER, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, err
		}
		pubBytes = pubDER
	default:
		return nil, fmt.Errorf("unsupported public key type")
	}
	
	hash := sha1.Sum(pubBytes)
	return hash[:], nil
}

// ExportCACertificate exports the CA certificate in PEM format for browsers
//
// Served per-request from /ca.crt and from agent registration, so it is read
// under the lock: a CA rotation running concurrently would otherwise hand out a
// torn view of provider state.
func (p *SelfSignedProvider) ExportCACertificate() ([]byte, error) {
	debug.Info("Exporting CA certificate for browsers")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ca == nil {
		debug.Error("CA certificate not initialized")
		return nil, fmt.Errorf("CA certificate not initialized")
	}

	// Create PEM block
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: p.ca.Raw,
	}

	// Encode to PEM format
	pemData := pem.EncodeToMemory(pemBlock)
	if pemData == nil {
		debug.Error("Failed to encode CA certificate to PEM")
		return nil, fmt.Errorf("failed to encode CA certificate")
	}

	debug.Info("Successfully exported CA certificate")
	return pemData, nil
}

// ExportCACertificateToFile exports the CA certificate to a file for distribution
func (p *SelfSignedProvider) ExportCACertificateToFile(path string) error {
	debug.Info("Exporting CA certificate to file: %s", path)

	// Get PEM data
	pemData, err := p.ExportCACertificate()
	if err != nil {
		return fmt.Errorf("failed to export CA certificate: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		debug.Error("Failed to create directory for CA certificate: %v", err)
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, pemData, 0644); err != nil {
		debug.Error("Failed to write CA certificate to file: %v", err)
		return fmt.Errorf("failed to write CA certificate: %w", err)
	}

	debug.Info("Successfully exported CA certificate to file")
	return nil
}

// GenerateClientCertificate generates a client certificate signed by the CA
func (p *SelfSignedProvider) GenerateClientCertificate(commonName string) (*tls.Certificate, error) {
	debug.Info("Generating client certificate for: %s", commonName)

	// Ensure CA key is loaded
	if p.caKey == nil {
		debug.Error("CA private key not initialized")
		return nil, fmt.Errorf("CA private key not initialized")
	}

	// Generate client key pair
	debug.Debug("Generating client key pair with key size: %d", p.config.KeySize)
	clientKey, err := rsa.GenerateKey(rand.Reader, p.config.KeySize)
	if err != nil {
		debug.Error("Failed to generate client key pair: %v", err)
		return nil, fmt.Errorf("failed to generate client key pair: %w", err)
	}

	// Generate subject key identifier for client
	clientSubjectKeyID, err := generateSubjectKeyID(&clientKey.PublicKey)
	if err != nil {
		debug.Error("Failed to generate client subject key identifier: %v", err)
		return nil, fmt.Errorf("failed to generate client subject key identifier: %w", err)
	}

	// Get CA subject key identifier
	caSubjectKeyID := p.ca.SubjectKeyId

	// Create client certificate template
	debug.Debug("Creating client certificate template")
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Country:            []string{p.config.CADetails.Country},
			Organization:       []string{p.config.CADetails.Organization},
			OrganizationalUnit: []string{"KrakenHashes Agents"},
			CommonName:         commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, p.config.Validity.Server),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		SubjectKeyId:          clientSubjectKeyID,
		AuthorityKeyId:        caSubjectKeyID,
	}

	// Sign the client certificate with CA
	debug.Debug("Signing client certificate with CA")
	clientCertBytes, err := x509.CreateCertificate(rand.Reader, clientTemplate, p.ca, &clientKey.PublicKey, p.caKey)
	if err != nil {
		debug.Error("Failed to create client certificate: %v", err)
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	// Encode certificate to PEM
	debug.Debug("Encoding client certificate to PEM")
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: clientCertBytes,
	})
	if certPEM == nil {
		debug.Error("Failed to encode client certificate to PEM")
		return nil, fmt.Errorf("failed to encode client certificate")
	}

	// Encode private key to PEM
	debug.Debug("Encoding client private key to PEM")
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
	})
	if keyPEM == nil {
		debug.Error("Failed to encode client private key to PEM")
		return nil, fmt.Errorf("failed to encode client private key")
	}

	debug.Info("Successfully generated client certificate")
	return &tls.Certificate{
		Certificate: [][]byte{certPEM},
		PrivateKey:  keyPEM,
	}, nil
}

// GetClientCertificate returns the client certificate and private key in PEM format
//
// Served per-request from agent registration and certificate renewal, so it is
// read under the lock against a concurrent reissue.
func (p *SelfSignedProvider) GetClientCertificate() ([]byte, []byte, error) {
	debug.Info("Exporting client certificate and key")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.clientCert == nil || p.clientKey == nil {
		debug.Error("Client certificate not initialized")
		return nil, nil, fmt.Errorf("client certificate not initialized")
	}

	// Create certificate PEM block
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: p.clientCert.Raw,
	})
	if certPEM == nil {
		debug.Error("Failed to encode client certificate to PEM")
		return nil, nil, fmt.Errorf("failed to encode client certificate")
	}

	// Create private key PEM block
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(p.clientKey),
	})
	if keyPEM == nil {
		debug.Error("Failed to encode client private key to PEM")
		return nil, nil, fmt.Errorf("failed to encode client private key")
	}

	debug.Info("Successfully exported client certificate and key")
	return certPEM, keyPEM, nil
}
