package agent

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// certFailureKind classifies a TLS failure precisely enough to give the operator
// a correct instruction.
//
// The distinction that matters is between a problem with THIS agent's own
// certificates, which renewing can fix, and a problem with the SERVER's
// certificate, which nothing on this side can fix. Conflating the two is what
// made an unreachable address look like an agent credential fault and sent
// operators chasing the wrong thing.
type certFailureKind int

const (
	certFailureNone certFailureKind = iota
	// certFailureHostnameMismatch: the server's certificate carries no name
	// matching the address we dialled. Server-side; renewal cannot help.
	certFailureHostnameMismatch
	// certFailureUnknownAuthority: our ca.crt does not sign the server's
	// certificate. Usually a rotated CA; refreshing our copy fixes it.
	certFailureUnknownAuthority
	// certFailureExpired: a certificate in the chain is outside its validity.
	certFailureExpired
	// certFailureClientCertRejected: the server rejected OUR certificate.
	certFailureClientCertRejected
	// certFailureOther: a TLS failure we cannot attribute.
	certFailureOther
)

func (k certFailureKind) String() string {
	switch k {
	case certFailureHostnameMismatch:
		return "hostname_mismatch"
	case certFailureUnknownAuthority:
		return "unknown_authority"
	case certFailureExpired:
		return "expired"
	case certFailureClientCertRejected:
		return "client_cert_rejected"
	case certFailureOther:
		return "other"
	default:
		return "none"
	}
}

// classifyCertFailure identifies why a TLS dial failed, and returns the server
// certificate involved when the error carries one.
//
// Typed errors, not string matching. Go wraps verification failures in
// *tls.CertificateVerificationError, which implements Unwrap, so errors.As
// reaches the underlying x509 error. x509.HostnameError in particular carries the
// offending certificate, which is what lets us tell the operator exactly which
// names the server does have.
func classifyCertFailure(err error) (certFailureKind, *x509.Certificate) {
	if err == nil {
		return certFailureNone, nil
	}

	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return certFailureHostnameMismatch, hostErr.Certificate
	}

	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		return certFailureUnknownAuthority, authErr.Cert
	}

	var invalidErr x509.CertificateInvalidError
	if errors.As(err, &invalidErr) {
		if invalidErr.Reason == x509.Expired {
			return certFailureExpired, invalidErr.Cert
		}
		return certFailureOther, invalidErr.Cert
	}

	// An alert from the peer means the SERVER is complaining about US -- the one
	// case that renewing this agent's client certificate can actually fix.
	var alertErr *tls.CertificateVerificationError
	if errors.As(err, &alertErr) {
		return certFailureOther, firstCert(alertErr.UnverifiedCertificates)
	}
	if strings.Contains(err.Error(), "remote error: tls:") {
		return certFailureClientCertRejected, nil
	}

	if isCertificateError(err) {
		return certFailureOther, nil
	}
	return certFailureNone, nil
}

// LastFailureWasUncoveredAddress reports whether the most recent connection
// attempt failed because the server's certificate carries no name matching the
// address this agent dials.
//
// Callers use it to stop retrying: the condition is server-side and cannot
// resolve without an administrator changing the certificate.
func (c *Connection) LastFailureWasUncoveredAddress() bool {
	return certFailureKind(c.lastCertFailure.Load()) == certFailureHostnameMismatch
}

// portNumber parses a URL port, returning 0 when it is absent or malformed.
func portNumber(port string) int {
	if port == "" {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
}

func firstCert(certs []*x509.Certificate) *x509.Certificate {
	if len(certs) == 0 {
		return nil
	}
	return certs[0]
}

// describeCertSANs renders what a certificate actually covers, in the same
// IP:/DNS: form the server logs use.
func describeCertSANs(cert *x509.Certificate) []string {
	if cert == nil {
		return nil
	}
	out := make([]string, 0, len(cert.DNSNames)+len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		out = append(out, "IP:"+ip.String())
	}
	for _, name := range cert.DNSNames {
		out = append(out, "DNS:"+name)
	}
	return out
}

// tlsFailureReporter suppresses duplicate reports and duplicate console guidance.
type tlsFailureReporter struct {
	mu sync.Mutex
	// lastKey is host:port + kind + certificate fingerprint. Including the
	// fingerprint means that after the administrator reissues, a STILL-failing
	// agent reports again immediately instead of waiting out the cooldown --
	// which is the feedback loop for "you added the wrong address".
	lastKey       string
	lastReportAt  time.Time
	guidanceShown bool
	// unsupported is set when the backend has no reporting endpoint (an older
	// server), so a new agent against an old backend does not log-spam.
	unsupported bool
}

// reportInterval is the floor for re-reporting an unchanged failure. Short enough
// that the admin banner stays fresh while the problem persists.
const reportInterval = 30 * time.Minute

// tlsFailureReport is the payload sent to the backend.
type tlsFailureReport struct {
	AttemptedHost string   `json:"attempted_host"`
	AttemptedPort int      `json:"attempted_port"`
	FailureKind   string   `json:"failure_kind"`
	CertSANs      []string `json:"cert_sans,omitempty"`
	CertSubject   string   `json:"cert_subject,omitempty"`
	Error         string   `json:"error,omitempty"`
	AgentVersion  string   `json:"agent_version,omitempty"`
}

// shouldReport decides whether this failure is new enough to send.
func (r *tlsFailureReporter) shouldReport(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.unsupported {
		return false
	}
	if key == r.lastKey && time.Since(r.lastReportAt) < reportInterval {
		return false
	}
	r.lastKey = key
	r.lastReportAt = time.Now()
	return true
}

func (r *tlsFailureReporter) markUnsupported() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unsupported = true
}

// shouldShowGuidance reports whether the operator has already been told.
func (r *tlsFailureReporter) shouldShowGuidance() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.guidanceShown {
		return false
	}
	r.guidanceShown = true
	return true
}

// reset clears the suppression state after a successful connection, so a
// recurrence is reported and explained again.
func (r *tlsFailureReporter) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastKey = ""
	r.guidanceShown = false
}

// reportTLSFailure tells the backend which address this agent could not verify.
//
// Sent over PLAIN HTTP on the bootstrap port, deliberately: the whole reason this
// exists is that the TLS channel cannot be trusted, so the report must not depend
// on it. Best-effort with no retry -- a failed report is not worth a retry storm
// on top of an already-failing reconnect loop.
func reportTLSFailure(
	reporter *tlsFailureReporter,
	urlConfig *config.URLConfig,
	kind certFailureKind,
	serverCert *x509.Certificate,
	host string,
	port int,
	cause error,
	agentVersion string,
) {
	fingerprint := "none"
	if serverCert != nil {
		sum := sha256.Sum256(serverCert.Raw)
		fingerprint = hex.EncodeToString(sum[:8])
	}
	key := fmt.Sprintf("%s:%d|%s|%s", host, port, kind, fingerprint)

	if !reporter.shouldReport(key) {
		return
	}

	apiKey, agentID, err := auth.LoadAgentKey(config.GetConfigDir())
	if err != nil {
		debug.Debug("Not reporting the TLS failure: this agent is not registered yet (%v)", err)
		return
	}

	payload := tlsFailureReport{
		AttemptedHost: host,
		AttemptedPort: port,
		FailureKind:   kind.String(),
		Error:         cause.Error(),
		AgentVersion:  agentVersion,
	}
	if serverCert != nil {
		payload.CertSANs = describeCertSANs(serverCert)
		payload.CertSubject = serverCert.Subject.String()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		debug.Warning("Failed to encode the TLS failure report: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, urlConfig.GetTLSFailureReportURL(), bytes.NewReader(body))
	if err != nil {
		debug.Warning("Failed to build the TLS failure report request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Agent-ID", agentID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		debug.Warning("Could not report the TLS failure to the server: %v", err)
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// Older backend without this endpoint. Treat as a no-op and stop trying.
		debug.Info("This server does not support TLS failure reporting; skipping further reports")
		reporter.markUnsupported()
	case resp.StatusCode >= 400:
		debug.Warning("The server rejected the TLS failure report: %s", resp.Status)
	default:
		debug.Info("Reported the TLS verification failure for %s:%d to the server", host, port)
	}
}

// printSANGuidance tells the operator, in plain terms, that this is a
// server-side configuration problem and exactly how to get it fixed.
//
// Uses console rather than debug because this has to reach whoever is watching
// the agent start, not just a log file nobody opens.
func printSANGuidance(host string, port int, serverCert *x509.Certificate) {
	hostPort := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	covers := strings.Join(describeCertSANs(serverCert), ", ")
	if covers == "" {
		covers = "(the certificate lists no addresses at all)"
	}

	console.Error("Cannot verify the KrakenHashes server certificate for %s.", hostPort)
	console.Print("")
	console.Warning("The server's certificate does not list this address.")
	console.Print("  You are connecting to : %s", hostPort)
	console.Print("  The certificate covers: %s", covers)
	console.Print("")
	console.Warning("This is a server-side setting. Renewing this agent's certificates will not fix it.")
	console.Print("")
	console.Print("Ask a KrakenHashes administrator to:")
	console.Print("  1. Open  Admin -> Settings -> Server Certificate")
	console.Print("  2. Add   %s   to the certificate's address list", host)
	console.Print("  3. Click Apply & Reissue")
	console.Print("")
	console.Info("This agent has reported the problem to the server and will keep retrying.")
	console.Info("It will reconnect on its own within 30 seconds of the certificate being reissued.")
	console.Info("No agent restart is needed.")
}

// printGenericCertGuidance covers the failures that renewing this agent's own
// material can plausibly fix. Deliberately does NOT point at the certificate
// settings page: sending an operator there for an unrelated TLS fault wastes
// their time and teaches them to distrust the message.
func printGenericCertGuidance(hostPort string, kind certFailureKind, cause error) {
	switch kind {
	case certFailureUnknownAuthority:
		console.Warning("This agent does not trust the server's certificate authority.")
		console.Info("The server's CA may have been rotated. Refreshing the CA certificate...")
	case certFailureExpired:
		console.Warning("A certificate in the chain has expired.")
		console.Info("Refreshing this agent's certificates from the server...")
	default:
		console.Error("TLS error connecting to %s: %v", hostPort, cause)
		console.Warning("Attempting to refresh this agent's certificates from the server...")
	}
}
