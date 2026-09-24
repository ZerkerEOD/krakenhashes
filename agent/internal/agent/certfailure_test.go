package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"testing"
)

// TestClassifyCertFailureHostnameMismatch is the case this whole feature exists
// for: the server's certificate carries no name matching the address the agent
// dialled.
//
// Before typed classification, this was lumped in with client-certificate
// problems, sending the agent through a renewal that cannot help and reporting
// "connection failed after certificate renewal" -- which points the operator at
// the agent when the fault is entirely server-side.
func TestClassifyCertFailureHostnameMismatch(t *testing.T) {
	serverCert := &x509.Certificate{
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	err := x509.HostnameError{Certificate: serverCert, Host: "192.168.1.50"}

	kind, got := classifyCertFailure(err)
	if kind != certFailureHostnameMismatch {
		t.Fatalf("kind = %v, want certFailureHostnameMismatch", kind)
	}
	if got != serverCert {
		t.Fatal("the server certificate was not carried out of the error")
	}
}

// The x509 error is normally wrapped in tls.CertificateVerificationError, which
// implements Unwrap. errors.As must reach through it.
func TestClassifyCertFailureThroughVerificationError(t *testing.T) {
	serverCert := &x509.Certificate{DNSNames: []string{"localhost"}}
	wrapped := &tls.CertificateVerificationError{
		UnverifiedCertificates: []*x509.Certificate{serverCert},
		Err:                    x509.HostnameError{Certificate: serverCert, Host: "10.0.0.5"},
	}

	kind, got := classifyCertFailure(wrapped)
	if kind != certFailureHostnameMismatch {
		t.Fatalf("kind = %v, want certFailureHostnameMismatch through the wrapper", kind)
	}
	if got != serverCert {
		t.Fatal("the server certificate was not carried through the wrapper")
	}
}

// And through an additional fmt.Errorf %w layer, which is how it arrives from
// the dialer in practice.
func TestClassifyCertFailureThroughFmtWrap(t *testing.T) {
	inner := x509.HostnameError{Certificate: &x509.Certificate{}, Host: "10.0.0.5"}
	err := fmt.Errorf("failed to connect: %w", inner)

	if kind, _ := classifyCertFailure(err); kind != certFailureHostnameMismatch {
		t.Fatalf("kind = %v, want certFailureHostnameMismatch through fmt.Errorf", kind)
	}
}

func TestClassifyCertFailureUnknownAuthority(t *testing.T) {
	cert := &x509.Certificate{}
	kind, got := classifyCertFailure(x509.UnknownAuthorityError{Cert: cert})
	if kind != certFailureUnknownAuthority {
		t.Fatalf("kind = %v, want certFailureUnknownAuthority", kind)
	}
	if got != cert {
		t.Fatal("the certificate was not carried out of the error")
	}
}

func TestClassifyCertFailureExpired(t *testing.T) {
	kind, _ := classifyCertFailure(x509.CertificateInvalidError{
		Cert:   &x509.Certificate{},
		Reason: x509.Expired,
	})
	if kind != certFailureExpired {
		t.Fatalf("kind = %v, want certFailureExpired", kind)
	}
}

func TestClassifyCertFailureNilAndUnrelated(t *testing.T) {
	if kind, _ := classifyCertFailure(nil); kind != certFailureNone {
		t.Fatalf("classifyCertFailure(nil) = %v, want certFailureNone", kind)
	}
	// A plain network failure must not be dressed up as a certificate problem,
	// or the operator gets certificate guidance for an unplugged cable.
	if kind, _ := classifyCertFailure(errors.New("dial tcp: connection refused")); kind != certFailureNone {
		t.Fatalf("classifyCertFailure(connection refused) = %v, want certFailureNone", kind)
	}
}

func TestDescribeCertSANs(t *testing.T) {
	cert := &x509.Certificate{
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	got := describeCertSANs(cert)
	if len(got) != 2 || got[0] != "IP:127.0.0.1" || got[1] != "DNS:localhost" {
		t.Fatalf("describeCertSANs = %v, want [IP:127.0.0.1 DNS:localhost]", got)
	}
	if describeCertSANs(nil) != nil {
		t.Fatal("describeCertSANs(nil) should be nil so callers fall back to generic guidance")
	}
}

// A failure must be reported once, not on every 30-second reconnect attempt.
// The key includes the certificate fingerprint so that a STILL-failing agent
// reports again immediately after a reissue rather than waiting out the cooldown.
func TestReporterSuppressesDuplicates(t *testing.T) {
	var r tlsFailureReporter

	if !r.shouldReport("192.168.1.50:31337|hostname_mismatch|aabb") {
		t.Fatal("the first report was suppressed")
	}
	if r.shouldReport("192.168.1.50:31337|hostname_mismatch|aabb") {
		t.Fatal("an identical repeat report was not suppressed")
	}
	if !r.shouldReport("192.168.1.50:31337|hostname_mismatch|ccdd") {
		t.Fatal("a report after the certificate changed was suppressed")
	}
}

func TestReporterShowsGuidanceOnce(t *testing.T) {
	var r tlsFailureReporter

	if !r.shouldShowGuidance() {
		t.Fatal("guidance was suppressed on the first failure")
	}
	if r.shouldShowGuidance() {
		t.Fatal("guidance repeated; it would be printed on every reconnect attempt")
	}

	// A successful connection resets it, so a later recurrence is explained again.
	r.reset()
	if !r.shouldShowGuidance() {
		t.Fatal("guidance stayed suppressed after a successful reconnect")
	}
}

func TestReporterStopsAgainstAnOlderBackend(t *testing.T) {
	var r tlsFailureReporter
	r.markUnsupported()
	if r.shouldReport("anything") {
		t.Fatal("reports continued against a backend with no reporting endpoint")
	}
}

func TestPortNumber(t *testing.T) {
	cases := map[string]int{"31337": 31337, "": 0, "not-a-port": 0}
	for input, want := range cases {
		if got := portNumber(input); got != want {
			t.Fatalf("portNumber(%q) = %d, want %d", input, got, want)
		}
	}
}
