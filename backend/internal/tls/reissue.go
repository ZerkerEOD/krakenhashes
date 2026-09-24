package tls

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// leafRenewWindow is how close to expiry a leaf may get before it is reissued at
// startup.
//
// Deliberately a constant rather than a setting: the server and client leaves
// inherit a 365-day validity and nothing else renews them, so a deployment left
// running for a year would otherwise present an expired certificate to every
// agent. Two rows of SAN configuration is the whole settings surface this feature
// should add.
const leafRenewWindow = 30 * 24 * time.Hour

// Reasons reported in ReissueResult.
const (
	ReasonInSync     = "in-sync"
	ReasonSANDrift   = "san-drift"
	ReasonForced     = "forced"
	ReasonExpiring   = "expiring"
	ReasonCARotation = "ca-rotation"
)

// ReissueOptions controls a reissue request.
type ReissueOptions struct {
	// Force reissues even when the current certificate already matches.
	Force bool
	// IncludeClientLeaf also reissues the shared agent client certificate.
	//
	// Defaults to false for drift-driven reissue: the client leaf carries no
	// SANs, so a SAN change cannot affect it, and replacing it desynchronises
	// what registration hands out from what already-enrolled agents hold for no
	// benefit. It exists as the manual escape hatch for an expiring client leaf.
	IncludeClientLeaf bool
}

// ReissueResult describes what a reissue actually did.
type ReissueResult struct {
	Reissued           bool      `json:"reissued"`
	Reason             string    `json:"reason"`
	AddedSANs          []string  `json:"added_sans,omitempty"`
	RemovedSANs        []string  `json:"removed_sans,omitempty"`
	Serial             string    `json:"serial,omitempty"`
	NotBefore          time.Time `json:"not_before,omitempty"`
	NotAfter           time.Time `json:"not_after,omitempty"`
	CARotated          bool      `json:"ca_rotated"`
	ClientLeafReissued bool      `json:"client_leaf_reissued"`
	// BackupDir is set by RotateCA: the directory holding the pre-rotation
	// material, named in the API response so a failed rotation has a documented
	// manual recovery path.
	BackupDir string `json:"backup_dir,omitempty"`
}

// ProviderConfig exposes the immutable provider configuration so callers that
// build a SAN set (which needs Host and the CA subject) do not have to duplicate
// the environment loading in tls.LoadProviderConfig.
func (p *SelfSignedProvider) ProviderConfig() *ProviderConfig {
	return p.config
}

// ServerCertificate returns a copy of the leaf currently being served, for the
// admin API's read model and for the startup drift check.
func (p *SelfSignedProvider) ServerCertificate() *x509.Certificate {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cert
}

// CACertificate returns the CA currently signing leaves.
func (p *SelfSignedProvider) CACertificate() *x509.Certificate {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ca
}

// EnsureServerCertificate reissues the server leaf under the EXISTING certificate
// authority when its SANs no longer match the desired set, when it is close to
// expiry, or when forced.
//
// The CA is deliberately untouched. Subject alternative names live only on the
// leaf -- a CA carries none -- so reissuing the leaf alone fixes an
// address-coverage problem with zero disruption: every enrolled agent keeps
// validating the new certificate with the ca.crt it already has, and no agent
// needs to do anything but reconnect.
//
// Nothing is written to disk and nothing is swapped into the live listener until
// the new certificate has been parsed back, matched against its key, verified to
// chain to the CA, and confirmed to actually carry every requested name. On any
// failure the previous certificate keeps serving.
func (p *SelfSignedProvider) EnsureServerCertificate(sans SANSet, opts ReissueOptions) (*ReissueResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ca == nil || p.caKey == nil {
		return nil, fmt.Errorf("certificate authority is not loaded; cannot reissue")
	}
	if p.cert == nil {
		return nil, fmt.Errorf("no server certificate is loaded; cannot reissue")
	}

	added, removed := sans.Diff(p.cert)

	reason := ""
	switch {
	case opts.Force:
		reason = ReasonForced
	case !sans.Equal(p.cert):
		reason = ReasonSANDrift
	case time.Until(p.cert.NotAfter) < leafRenewWindow:
		reason = ReasonExpiring
	default:
		return &ReissueResult{
			Reissued:  false,
			Reason:    ReasonInSync,
			Serial:    p.cert.SerialNumber.String(),
			NotBefore: p.cert.NotBefore,
			NotAfter:  p.cert.NotAfter,
		}, nil
	}

	debug.Warning("Reissuing server certificate (%s). Adding %v; removing %v.", reason, added, removed)
	debug.Warning("Reissuing under the EXISTING certificate authority - enrolled agents are unaffected.")

	newCert, newKey, err := p.issueServerLeaf(p.ca, p.caKey, sans)
	if err != nil {
		return nil, err
	}

	if err := p.validateServerLeaf(newCert, newKey, sans); err != nil {
		// Nothing has been written and nothing swapped: the old certificate is
		// still on disk and still being served.
		return nil, fmt.Errorf("refusing to install the reissued certificate: %w", err)
	}

	var newClientCert *x509.Certificate
	var newClientKey *rsa.PrivateKey
	if opts.IncludeClientLeaf {
		newClientCert, newClientKey, err = p.issueClientLeaf(p.ca, p.caKey)
		if err != nil {
			return nil, err
		}
	}

	if err := p.persistServerLeaf(newCert, newKey); err != nil {
		return nil, err
	}

	if opts.IncludeClientLeaf {
		if err := p.persistClientLeaf(newClientCert, newClientKey); err != nil {
			// The server leaf is already installed and valid; report the client
			// leaf failure rather than pretending the whole reissue failed.
			return nil, fmt.Errorf("server certificate reissued, but the client certificate could not be written: %w", err)
		}
		p.clientCert, p.clientKey = newClientCert, newClientKey
	}

	// Disk first, then memory. If the write fails the process keeps serving the
	// old certificate AND a restart reproduces that same state. Updating memory
	// first would let a restart silently revert a reissue the admin was told
	// had succeeded.
	p.cert, p.certKey = newCert, newKey
	p.storeCurrentLeaf()

	debug.Info("Reissued server certificate: serial=%s notAfter=%s %s",
		newCert.SerialNumber.String(), newCert.NotAfter.Format(time.RFC3339), sans.String())

	return &ReissueResult{
		Reissued:           true,
		Reason:             reason,
		AddedSANs:          added,
		RemovedSANs:        removed,
		Serial:             newCert.SerialNumber.String(),
		NotBefore:          newCert.NotBefore,
		NotAfter:           newCert.NotAfter,
		ClientLeafReissued: opts.IncludeClientLeaf,
	}, nil
}

// RotateCA regenerates the certificate authority and every leaf beneath it.
//
// This is the destructive path. Agents recover on their own -- a TLS failure
// drives them to re-download ca.crt over the plain-HTTP channel -- but every
// browser and operating system trust store that installed the old CA must be
// updated by hand, and the CA fingerprint changes.
func (p *SelfSignedProvider) RotateCA(sans SANSet) (*ReissueResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	debug.Warning("Rotating the KrakenHashes certificate authority. Every agent must re-fetch ca.crt.")

	backupDir, err := p.backupCertsDir()
	if err != nil {
		return nil, fmt.Errorf("refusing to rotate the CA without a backup: %w", err)
	}
	debug.Info("Existing certificate material backed up to %s", backupDir)

	// Build everything in memory first. A rotation touches six files and cannot
	// be made atomic as a group, so the goal is to have nothing left to fail
	// once the first byte is written.
	newCA, newCAKey, err := p.issueCA()
	if err != nil {
		return nil, err
	}
	newCert, newKey, err := p.issueServerLeaf(newCA, newCAKey, sans)
	if err != nil {
		return nil, err
	}
	newClientCert, newClientKey, err := p.issueClientLeaf(newCA, newCAKey)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	pool.AddCert(newCA)
	if err := p.validateLeafAgainst(newCert, newKey, pool, sans, x509.ExtKeyUsageServerAuth); err != nil {
		return nil, fmt.Errorf("refusing to install the rotated certificate: %w", err)
	}

	// ca.crt is written LAST. It is what /ca.crt serves and what agents fetch, so
	// publishing it last guarantees an agent that reads it mid-rotation always
	// receives a CA that already signs the leaf on disk.
	certsDir := p.config.CertsDir
	writes := []struct {
		path      string
		blockType string
		der       []byte
		mode      os.FileMode
	}{
		{p.config.KeyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(newKey), keyFileMode},
		{p.config.CertFile, "CERTIFICATE", newCert.Raw, certFileMode},
		{filepath.Join(certsDir, "client.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(newClientKey), keyFileMode},
		{filepath.Join(certsDir, "client.crt"), "CERTIFICATE", newClientCert.Raw, certFileMode},
		{filepath.Join(certsDir, "ca.key"), "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(newCAKey), keyFileMode},
		{p.config.CAFile, "CERTIFICATE", newCA.Raw, certFileMode},
	}
	for _, w := range writes {
		if err := writePEMAtomic(w.path, w.blockType, w.der, w.mode); err != nil {
			return nil, fmt.Errorf("CA rotation failed while writing %s (previous material is in %s): %w",
				w.path, backupDir, err)
		}
	}

	added, removed := sans.Diff(p.cert)

	p.ca, p.caKey = newCA, newCAKey
	p.cert, p.certKey = newCert, newKey
	p.clientCert, p.clientKey = newClientCert, newClientKey
	p.caCertPool = pool
	p.storeCurrentLeaf()

	debug.Warning("Certificate authority rotated. New CA serial=%s, server serial=%s",
		newCA.SerialNumber.String(), newCert.SerialNumber.String())

	return &ReissueResult{
		Reissued:           true,
		Reason:             ReasonCARotation,
		AddedSANs:          added,
		RemovedSANs:        removed,
		Serial:             newCert.SerialNumber.String(),
		NotBefore:          newCert.NotBefore,
		NotAfter:           newCert.NotAfter,
		CARotated:          true,
		ClientLeafReissued: true,
		BackupDir:          backupDir,
	}, nil
}

// validateServerLeaf checks a freshly-issued leaf against the current CA.
func (p *SelfSignedProvider) validateServerLeaf(cert *x509.Certificate, key *rsa.PrivateKey, sans SANSet) error {
	pool := x509.NewCertPool()
	pool.AddCert(p.ca)
	return p.validateLeafAgainst(cert, key, pool, sans, x509.ExtKeyUsageServerAuth)
}

// validateLeafAgainst proves a new certificate is usable BEFORE it is allowed to
// replace a working one.
//
// The VerifyHostname pass is the one that matters most here: it is the same check
// an agent performs, so it turns "we asked for this address" into "a client
// dialling this address will actually succeed", which is precisely the guarantee
// this whole feature exists to provide.
func (p *SelfSignedProvider) validateLeafAgainst(
	cert *x509.Certificate,
	key *rsa.PrivateKey,
	roots *x509.CertPool,
	sans SANSet,
	usage x509.ExtKeyUsage,
) error {
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("reissued certificate does not carry an RSA public key")
	}
	if !pub.Equal(&key.PublicKey) {
		return fmt.Errorf("reissued certificate does not match its private key")
	}

	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{usage},
	}); err != nil {
		return fmt.Errorf("reissued certificate does not chain to the certificate authority: %w", err)
	}

	for _, name := range sans.DNSNames {
		if err := cert.VerifyHostname(name); err != nil {
			return fmt.Errorf("reissued certificate does not cover %q: %w", name, err)
		}
	}
	for _, ip := range sans.IPAddresses {
		if err := cert.VerifyHostname(ip.String()); err != nil {
			return fmt.Errorf("reissued certificate does not cover %q: %w", ip.String(), err)
		}
	}

	// Round-trip the exact bytes about to be written, so a PEM encoding problem
	// surfaces here rather than at the next handshake.
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("reissued certificate and key do not form a usable TLS pair: %w", err)
	}

	return nil
}

// persistServerLeaf writes the new server certificate and key, taking .bak copies
// first and restoring them if the second write fails.
//
// The key is written before the certificate. That ordering means the only
// observable half-state is "new key, old certificate", and nothing in the system
// reads the key without the certificate, so no reader can act on it.
func (p *SelfSignedProvider) persistServerLeaf(cert *x509.Certificate, key *rsa.PrivateKey) error {
	certBak := p.config.CertFile + ".bak"
	keyBak := p.config.KeyFile + ".bak"

	if err := copyFile(p.config.CertFile, certBak, certFileMode); err != nil {
		return fmt.Errorf("failed to back up the current certificate: %w", err)
	}
	if err := copyFile(p.config.KeyFile, keyBak, keyFileMode); err != nil {
		return fmt.Errorf("failed to back up the current private key: %w", err)
	}

	if err := writePEMAtomic(p.config.KeyFile, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), keyFileMode); err != nil {
		return fmt.Errorf("failed to write the reissued private key: %w", err)
	}

	if err := writePEMAtomic(p.config.CertFile, "CERTIFICATE", cert.Raw, certFileMode); err != nil {
		// Put the old key back so disk stays a matched pair. In-memory state was
		// never touched, so the running server is unaffected either way.
		if restoreErr := copyFile(keyBak, p.config.KeyFile, keyFileMode); restoreErr != nil {
			debug.Error("Failed to restore the previous private key from %s: %v", keyBak, restoreErr)
		}
		return fmt.Errorf("failed to write the reissued certificate: %w", err)
	}

	return nil
}

// persistClientLeaf writes the shared agent client certificate and key.
func (p *SelfSignedProvider) persistClientLeaf(cert *x509.Certificate, key *rsa.PrivateKey) error {
	certPath := filepath.Join(p.config.CertsDir, "client.crt")
	keyPath := filepath.Join(p.config.CertsDir, "client.key")

	if err := writePEMAtomic(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), keyFileMode); err != nil {
		return err
	}
	return writePEMAtomic(certPath, "CERTIFICATE", cert.Raw, certFileMode)
}

// backupCertsDir snapshots the whole certs directory before a CA rotation.
func (p *SelfSignedProvider) backupCertsDir() (string, error) {
	dir := filepath.Join(p.config.CertsDir, "backup-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup directory %s: %w", dir, err)
	}

	// Uses the configured paths rather than assumed filenames so a deployment
	// that overrode KH_CERT_FILE / KH_KEY_FILE / KH_CA_FILE still gets a complete
	// backup.
	sources := []string{
		p.config.CAFile,
		p.config.CertFile,
		p.config.KeyFile,
		filepath.Join(p.config.CertsDir, "ca.key"),
		filepath.Join(p.config.CertsDir, "client.crt"),
		filepath.Join(p.config.CertsDir, "client.key"),
	}
	for _, src := range sources {
		if !fileExists(src) {
			continue
		}
		mode := certFileMode
		if filepath.Ext(src) == ".key" {
			mode = keyFileMode
		}
		if err := copyFile(src, filepath.Join(dir, filepath.Base(src)), mode); err != nil {
			return "", err
		}
	}

	return dir, nil
}
