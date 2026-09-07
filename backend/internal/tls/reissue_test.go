package tls

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

// newTestProvider builds a fully initialised self-signed provider in a temp dir.
func newTestProvider(t *testing.T, host string, extra DesiredSANs) *SelfSignedProvider {
	t.Helper()

	dir := t.TempDir()
	cfg := &ProviderConfig{
		Mode:     ModeSelfSigned,
		CertsDir: dir,
		CertFile: filepath.Join(dir, "server.crt"),
		KeyFile:  filepath.Join(dir, "server.key"),
		CAFile:   filepath.Join(dir, "ca.crt"),
		Host:     host,
		CADetails: &CertificateAuthority{
			Country:            "US",
			Organization:       "KrakenHashes",
			OrganizationalUnit: "KrakenHashes CA",
			CommonName:         "KrakenHashes Root CA",
		},
		// 2048 keeps the test fast; the logic under test is key-size agnostic.
		KeySize:               2048,
		AdditionalDNSNames:    extra.DNSNames,
		AdditionalIPAddresses: extra.IPAddresses,
	}
	cfg.Validity.Server = 365
	cfg.Validity.CA = 3650

	p := NewSelfSignedProvider(cfg)
	if err := p.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p
}

func TestReissueKeepsCertificateAuthority(t *testing.T) {
	// The central promise of this feature: adding an address reissues only the
	// leaf, so every enrolled agent keeps working with the ca.crt it already
	// holds. If the CA serial changes here, the whole fleet breaks.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	caBefore := p.CACertificate().SerialNumber.String()
	leafBefore := p.ServerCertificate().SerialNumber.String()

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	result, err := p.EnsureServerCertificate(sans, ReissueOptions{})
	if err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}
	if !result.Reissued {
		t.Fatalf("Reissued = false (reason %q), want a reissue for the added address", result.Reason)
	}
	if result.Reason != ReasonSANDrift {
		t.Fatalf("Reason = %q, want %q", result.Reason, ReasonSANDrift)
	}

	if got := p.CACertificate().SerialNumber.String(); got != caBefore {
		t.Fatalf("CA serial changed from %s to %s; the CA must not be regenerated", caBefore, got)
	}
	if got := p.ServerCertificate().SerialNumber.String(); got == leafBefore {
		t.Fatal("server certificate serial unchanged; the leaf was not actually reissued")
	}
}

func TestReissuedLeafValidatesAgainstTheOriginalCA(t *testing.T) {
	// Simulates an already-enrolled agent: it holds the ORIGINAL ca.crt and must
	// be able to verify the reissued certificate without fetching anything.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	agentPool := x509.NewCertPool()
	agentPool.AddCert(p.CACertificate())

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	if _, err := p.EnsureServerCertificate(sans, ReissueOptions{}); err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}

	leaf := p.ServerCertificate()
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     agentPool,
		DNSName:   "192.168.1.50",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("an agent holding the original CA could not verify the reissued leaf: %v", err)
	}
}

func TestReissueCoversTheRequestedAddress(t *testing.T) {
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{
		IPAddresses: []string{"192.168.1.50", "100.64.1.7"},
		DNSNames:    []string{"kraken.internal"},
	})
	if _, err := p.EnsureServerCertificate(sans, ReissueOptions{}); err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}

	leaf := p.ServerCertificate()
	for _, name := range []string{"192.168.1.50", "100.64.1.7", "kraken.internal", "127.0.0.1", "localhost"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Fatalf("reissued certificate does not cover %q: %v", name, err)
		}
	}
}

func TestReissueUpdatesTheServedCertificate(t *testing.T) {
	// The hot-swap contract: a reissue must be visible to the next handshake
	// without a restart. If this fails, an admin is told the certificate was
	// reissued while :31337 keeps serving the old one.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	cfg, err := p.GetTLSConfig()
	if err != nil {
		t.Fatalf("GetTLSConfig: %v", err)
	}

	// Certificates must be empty. crypto/tls only consults GetCertificate when
	// it is, or when the ClientHello carries SNI -- and agents dialling a bare
	// IP send no SNI at all.
	if len(cfg.Certificates) != 0 {
		t.Fatal("tls.Config.Certificates is populated; GetCertificate would be skipped for bare-IP clients")
	}
	if cfg.GetCertificate == nil {
		t.Fatal("tls.Config.GetCertificate is nil; reissue could never take effect without a restart")
	}

	before, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	if _, err := p.EnsureServerCertificate(sans, ReissueOptions{}); err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}

	after, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate after reissue: %v", err)
	}
	if after.Leaf.SerialNumber.Cmp(before.Leaf.SerialNumber) == 0 {
		t.Fatal("the served certificate did not change after a reissue")
	}
	if err := after.Leaf.VerifyHostname("192.168.1.50"); err != nil {
		t.Fatalf("the served certificate does not cover the new address: %v", err)
	}
}

func TestReissueIsIdempotent(t *testing.T) {
	// Running the drift check twice must not reissue twice, or every boot would
	// churn the certificate.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{IPAddresses: []string{"192.168.1.50"}})

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})

	first, err := p.EnsureServerCertificate(sans, ReissueOptions{})
	if err != nil {
		t.Fatalf("first EnsureServerCertificate: %v", err)
	}
	if first.Reissued {
		t.Fatalf("reissued on a certificate that already matches (reason %q)", first.Reason)
	}

	second, err := p.EnsureServerCertificate(sans, ReissueOptions{})
	if err != nil {
		t.Fatalf("second EnsureServerCertificate: %v", err)
	}
	if second.Reissued || second.Reason != ReasonInSync {
		t.Fatalf("second call reissued = %v reason %q, want in-sync", second.Reissued, second.Reason)
	}
}

func TestReissueWritesCorrectFileModes(t *testing.T) {
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	if _, err := p.EnsureServerCertificate(sans, ReissueOptions{}); err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}

	certInfo, err := os.Stat(p.ProviderConfig().CertFile)
	if err != nil {
		t.Fatalf("stat certificate: %v", err)
	}
	if got := certInfo.Mode().Perm(); got != certFileMode {
		t.Fatalf("certificate mode = %o, want %o", got, certFileMode)
	}

	keyInfo, err := os.Stat(p.ProviderConfig().KeyFile)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if got := keyInfo.Mode().Perm(); got != keyFileMode {
		t.Fatalf("private key mode = %o, want %o -- a private key must never be group or world readable", got, keyFileMode)
	}
}

func TestReissueLeavesClientLeafAloneByDefault(t *testing.T) {
	// The client certificate carries no SANs, so a name change cannot affect it.
	// Replacing it would desynchronise what registration hands out from what
	// already-enrolled agents hold, for no benefit.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	clientBefore, _, err := p.GetClientCertificate()
	if err != nil {
		t.Fatalf("GetClientCertificate: %v", err)
	}

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	result, err := p.EnsureServerCertificate(sans, ReissueOptions{})
	if err != nil {
		t.Fatalf("EnsureServerCertificate: %v", err)
	}
	if result.ClientLeafReissued {
		t.Fatal("ClientLeafReissued = true, want false for a name-only change")
	}

	clientAfter, _, err := p.GetClientCertificate()
	if err != nil {
		t.Fatalf("GetClientCertificate after reissue: %v", err)
	}
	if string(clientBefore) != string(clientAfter) {
		t.Fatal("the shared client certificate changed during a server-leaf reissue")
	}
}

func TestReissueFailureLeavesTheOldCertificateServing(t *testing.T) {
	// Failure injection: make the certs directory unwritable partway through.
	// The old certificate must keep serving, and the atomic pointer must be
	// untouched, so the running server is never left without a usable leaf.
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	before := p.ServerCertificate().SerialNumber.String()
	dir := p.ProviderConfig().CertsDir

	if err := os.Chmod(dir, 0500); err != nil {
		t.Skipf("cannot make the certs directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	// Running as root defeats a read-only directory, so skip rather than
	// report a false pass.
	if f, err := os.CreateTemp(dir, "probe-*"); err == nil {
		name := f.Name()
		f.Close()
		os.Remove(name)
		t.Skip("the certs directory is still writable (running as root?)")
	}

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	if _, err := p.EnsureServerCertificate(sans, ReissueOptions{}); err == nil {
		t.Fatal("EnsureServerCertificate succeeded with an unwritable certs directory")
	}

	if got := p.ServerCertificate().SerialNumber.String(); got != before {
		t.Fatalf("in-memory certificate changed after a failed reissue: %s -> %s", before, got)
	}

	cfg, err := p.GetTLSConfig()
	if err != nil {
		t.Fatalf("GetTLSConfig after a failed reissue: %v", err)
	}
	served, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate after a failed reissue: %v", err)
	}
	if served.Leaf.SerialNumber.String() != before {
		t.Fatal("the served certificate changed after a failed reissue")
	}
}

func TestRotateCAReplacesEverything(t *testing.T) {
	p := newTestProvider(t, "0.0.0.0", DesiredSANs{})

	caBefore := p.CACertificate().SerialNumber.String()
	oldPool := x509.NewCertPool()
	oldPool.AddCert(p.CACertificate())

	sans := BuildServerSANs(p.ProviderConfig(), DesiredSANs{IPAddresses: []string{"192.168.1.50"}})
	result, err := p.RotateCA(sans)
	if err != nil {
		t.Fatalf("RotateCA: %v", err)
	}
	if !result.CARotated || !result.ClientLeafReissued {
		t.Fatalf("result = %+v, want both the CA and the client leaf replaced", result)
	}
	if result.BackupDir == "" {
		t.Fatal("BackupDir is empty; a rotation must leave a documented recovery path")
	}
	if _, err := os.Stat(result.BackupDir); err != nil {
		t.Fatalf("backup directory %s is missing: %v", result.BackupDir, err)
	}

	if got := p.CACertificate().SerialNumber.String(); got == caBefore {
		t.Fatal("the CA serial did not change during a rotation")
	}

	// The old CA must no longer validate the new leaf; that is precisely why
	// rotation is a separate, explicitly-confirmed action.
	if _, err := p.ServerCertificate().Verify(x509.VerifyOptions{
		Roots:     oldPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Fatal("the rotated leaf still validates against the old CA")
	}
}
