package agent

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// CertRefreshPayload is the server's request to re-pull trust material.
type CertRefreshPayload struct {
	RequestID     string `json:"request_id"`
	Reason        string `json:"reason"`
	CAFingerprint string `json:"ca_fingerprint"`
}

// CertRefreshAckPayload reports the outcome back to the server.
type CertRefreshAckPayload struct {
	RequestID     string `json:"request_id"`
	Success       bool   `json:"success"`
	Message       string `json:"message,omitempty"`
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
}

// handleCertRefresh re-downloads the CA and client certificate after the server
// has rotated its certificate authority.
//
// Note this is a convenience, not the recovery mechanism. An agent whose TLS is
// already broken has no WebSocket to receive this on; it recovers through its own
// reconnect loop. What this avoids is a connected agent carrying a stale CA until
// its next reconnect, which after a rotation would mean an avoidable outage.
func (c *Connection) handleCertRefresh(rawPayload json.RawMessage) {
	var payload CertRefreshPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		debug.Error("Failed to parse cert refresh payload: %v", err)
		c.sendCertRefreshAck(payload.RequestID, false, "invalid payload", "")
		return
	}

	// If the CA on disk already matches, do nothing. A broadcast to a large
	// fleet would otherwise cause every agent to re-download identical material.
	current := localCAFingerprint()
	if payload.CAFingerprint != "" && current == payload.CAFingerprint {
		debug.Info("Certificate refresh requested but the CA is already current; nothing to do")
		c.sendCertRefreshAck(payload.RequestID, true, "already current", current)
		return
	}

	debug.Info("Refreshing certificates (reason: %s)", payload.Reason)
	console.Info("The server rotated its certificate authority; refreshing this agent's certificates...")

	if err := RenewCertificates(c.urlConfig); err != nil {
		debug.Error("Certificate refresh failed: %v", err)
		console.Warning("Certificate refresh failed: %v. This agent will retry on its next reconnect.", err)
		c.sendCertRefreshAck(payload.RequestID, false, err.Error(), current)
		return
	}

	// Load the refreshed material into the live TLS configuration so the next
	// dial uses it. The current WebSocket session is unaffected: it was
	// negotiated under the old material and stays valid until it drops.
	certPool, err := loadCACertificate(c.urlConfig)
	if err != nil {
		debug.Error("Failed to reload the CA certificate after refresh: %v", err)
		c.sendCertRefreshAck(payload.RequestID, false, err.Error(), localCAFingerprint())
		return
	}
	clientCert, err := loadClientCertificate()
	if err != nil {
		debug.Error("Failed to reload the client certificate after refresh: %v", err)
		c.sendCertRefreshAck(payload.RequestID, false, err.Error(), localCAFingerprint())
		return
	}

	c.tlsConfig.RootCAs = certPool
	c.tlsConfig.Certificates = []tls.Certificate{clientCert}

	updated := localCAFingerprint()
	debug.Info("Certificates refreshed successfully (CA %s)", updated)
	console.Success("Certificates refreshed.")
	c.sendCertRefreshAck(payload.RequestID, true, "", updated)
}

// sendCertRefreshAck reports the outcome, mirroring the log-purge ack pattern.
func (c *Connection) sendCertRefreshAck(requestID string, success bool, message, fingerprint string) {
	if !c.isConnected.Load() {
		return
	}

	payload := CertRefreshAckPayload{
		RequestID:     requestID,
		Success:       success,
		Message:       message,
		CAFingerprint: fingerprint,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal cert refresh ack: %v", err)
		return
	}

	msg := &WSMessage{
		Type:      WSTypeCertRefreshAck,
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	if c.safeSendMessage(msg, 5000) {
		debug.Debug("Sent cert refresh ack (request_id: %s, success: %v)", requestID, success)
	} else {
		debug.Warning("Failed to send cert refresh ack")
	}
}

// localCAFingerprint returns the SHA-256 of the CA certificate on disk, in the
// same lowercase hex form the server sends. Empty when there is no readable CA.
func localCAFingerprint() string {
	data, err := os.ReadFile(filepath.Join(config.GetConfigDir(), "ca.crt"))
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:])
}
