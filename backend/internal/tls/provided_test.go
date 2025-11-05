package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseCertificateChain tests parsing of certificate chains from PEM data
func TestParseCertificateChain(t *testing.T) {
	// Create test certificates
	caKey, caTemplate, caCert := generateTestCAWithKey(t)
	serverCert := generateTestServerCertWithCA(t, caTemplate, caCert, caKey)

	// Create PEM data with multiple certificates
	var pemData []byte
	pemData = append(pemData, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCert.Raw,
	})...)
	pemData = append(pemData, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCert.Raw,
	})...)

	// Parse chain
	certs, err := parseCertificateChain(pemData)
	if err != nil {
		t.Fatalf("Failed to parse certificate chain: %v", err)
	}

	// Verify we got 2 certificates
	if len(certs) != 2 {
		t.Errorf("Expected 2 certificates, got %d", len(certs))
	}

	// Verify order (server cert first, then CA)
	if !certs[0].Equal(serverCert) {
		t.Error("First certificate should be server cert")
	}
	if !certs[1].Equal(caCert) {
		t.Error("Second certificate should be CA cert")
	}
}

// TestProvidedProviderSingleCert tests provided mode with a single self-signed certificate
func TestProvidedProviderSingleCert(t *testing.T) {
	// Create temporary directory for test certificates
	tmpDir := t.TempDir()

	// Generate self-signed certificate
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true, // Self-signed
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Write certificate and key to files
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}), 0644); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}

	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	// Create provider
	config := &ProviderConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
		CertsDir: tmpDir,
	}

	provider := NewProvidedProvider(config)

	// Initialize provider
	if err := provider.Initialize(); err != nil {
		t.Fatalf("Failed to initialize provider: %v", err)
	}

	// Verify CA certificate (should be same as server cert for self-signed)
	p := provider.(*ProvidedProvider)
	if p.caCert == nil {
		t.Fatal("CA certificate is nil")
	}
	if p.serverCert == nil {
		t.Fatal("Server certificate is nil")
	}

	// For self-signed, CA and server should be the same
	if !p.caCert.Equal(p.serverCert) {
		t.Error("For self-signed cert, CA and server cert should be equal")
	}
}

// Helper function to generate a test CA certificate with key
func generateTestCAWithKey(t *testing.T) (*rsa.PrivateKey, *x509.Certificate, *x509.Certificate) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	return key, template, cert
}

// Helper function to generate a test server certificate
func generateTestServerCertWithCA(t *testing.T, caTemplate, caCert *x509.Certificate, caKey *rsa.PrivateKey) *x509.Certificate {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate server key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse server certificate: %v", err)
	}

	return cert
}
