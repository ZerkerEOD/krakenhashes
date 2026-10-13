package tls

import (
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"
)

func newMTLSTestProvider(t *testing.T) *SelfSignedProvider {
	t.Helper()
	dir := t.TempDir()
	cfg := &ProviderConfig{
		Mode:     ModeSelfSigned,
		CertsDir: dir,
		CertFile: filepath.Join(dir, "server.crt"),
		KeyFile:  filepath.Join(dir, "server.key"),
		CAFile:   filepath.Join(dir, "ca.crt"),
		Host:     "127.0.0.1",
		CADetails: &CertificateAuthority{
			Country:            "US",
			Organization:       "KrakenHashes",
			OrganizationalUnit: "KrakenHashes CA",
			CommonName:         "KrakenHashes Root CA",
		},
		// 2048 keeps the test fast; the logic under test is key-size agnostic.
		KeySize: 2048,
	}
	cfg.Validity.Server = 365
	cfg.Validity.CA = 3650

	p := NewSelfSignedProvider(cfg)
	if err := p.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p
}

// A per-agent certificate must carry the agent ID as its CommonName, chain to
// the internal CA (what the mTLS ClientCAs pool verifies), be usable for client
// auth, and hand back the serial recorded for revocation. Distinct issuances
// must get distinct serials so revocation targets one cert/agent.
func TestGenerateAgentCertificateIdentityAndChain(t *testing.T) {
	p := newMTLSTestProvider(t)

	if !p.SupportsMTLS() {
		t.Fatal("self-signed provider should support mTLS")
	}

	certPEM, keyPEM, serial, err := p.GenerateAgentCertificate("42")
	if err != nil {
		t.Fatalf("GenerateAgentCertificate: %v", err)
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		t.Fatal("empty cert or key PEM")
	}
	if serial == "" {
		t.Fatal("empty serial")
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("cert PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	if cert.Subject.CommonName != "42" {
		t.Fatalf("CN = %q, want 42", cert.Subject.CommonName)
	}
	if got := cert.SerialNumber.Text(16); got != serial {
		t.Fatalf("serial mismatch: cert=%s returned=%s", got, serial)
	}

	foundClientAuth := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			foundClientAuth = true
		}
	}
	if !foundClientAuth {
		t.Fatal("missing ExtKeyUsageClientAuth")
	}

	pool, err := p.GetCACertPool()
	if err != nil {
		t.Fatalf("GetCACertPool: %v", err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("agent cert does not chain to internal CA: %v", err)
	}

	_, _, serial2, err := p.GenerateAgentCertificate("43")
	if err != nil {
		t.Fatalf("second GenerateAgentCertificate: %v", err)
	}
	if serial2 == serial {
		t.Fatal("expected distinct serials for distinct issuances")
	}
}
