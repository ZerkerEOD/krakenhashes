package tls

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// CertificateInfo is a read-only description of a certificate, for the admin UI.
type CertificateInfo struct {
	Subject            string    `json:"subject"`
	Issuer             string    `json:"issuer"`
	Serial             string    `json:"serial"`
	NotBefore          time.Time `json:"not_before"`
	NotAfter           time.Time `json:"not_after"`
	DaysRemaining      int       `json:"days_remaining"`
	DNSNames           []string  `json:"dns_names"`
	IPAddresses        []string  `json:"ip_addresses"`
	SignatureAlgorithm string    `json:"signature_algorithm"`
	PublicKeyBits      int       `json:"public_key_bits"`
	FingerprintSHA256  string    `json:"fingerprint_sha256"`
}

// CertificateInspector is implemented by providers that can describe the leaf
// they are currently serving.
//
// Deliberately a separate optional interface rather than an addition to Provider:
// extending Provider would force every mock and test double to grow a method
// that most of them have no use for.
type CertificateInspector interface {
	DescribeServerCertificate() (*CertificateInfo, error)
	DescribeCACertificate() (*CertificateInfo, error)
}

// describeCertificate builds the read model for one parsed certificate.
func describeCertificate(cert *x509.Certificate) *CertificateInfo {
	if cert == nil {
		return nil
	}

	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	bits := 0
	if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
		bits = pub.N.BitLen()
	}

	sum := sha256.Sum256(cert.Raw)

	return &CertificateInfo{
		Subject:            cert.Subject.String(),
		Issuer:             cert.Issuer.String(),
		Serial:             cert.SerialNumber.String(),
		NotBefore:          cert.NotBefore,
		NotAfter:           cert.NotAfter,
		DaysRemaining:      int(time.Until(cert.NotAfter).Hours() / 24),
		DNSNames:           cert.DNSNames,
		IPAddresses:        ips,
		SignatureAlgorithm: cert.SignatureAlgorithm.String(),
		PublicKeyBits:      bits,
		FingerprintSHA256:  formatFingerprint(sum[:]),
	}
}

// formatFingerprint renders a digest the way openssl and browsers show it.
func formatFingerprint(sum []byte) string {
	out := make([]byte, 0, len(sum)*3)
	for i, b := range sum {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex.EncodeToString([]byte{b})...)
	}
	return string(out)
}

// parseCertificateFile reads and parses the first certificate in a PEM file.
func parseCertificateFile(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	// Walk the file so a fullchain.pem yields its leaf rather than failing on
	// the first non-certificate block.
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("no certificate found in %s", path)
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		data = rest
	}
}

// DescribeServerCertificate implements CertificateInspector.
func (p *SelfSignedProvider) DescribeServerCertificate() (*CertificateInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cert == nil {
		return nil, fmt.Errorf("no server certificate is loaded")
	}
	return describeCertificate(p.cert), nil
}

// DescribeCACertificate implements CertificateInspector.
func (p *SelfSignedProvider) DescribeCACertificate() (*CertificateInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ca == nil {
		return nil, fmt.Errorf("no certificate authority is loaded")
	}
	return describeCertificate(p.ca), nil
}

// DescribeServerCertificate implements CertificateInspector for operator-supplied
// certificates.
//
// Reads from disk rather than in-memory state so that expiry is reported
// accurately even though this provider never reloads: an admin in `provided` mode
// has no other surface that surfaces the expiry date at all.
func (p *ProvidedProvider) DescribeServerCertificate() (*CertificateInfo, error) {
	cert, err := parseCertificateFile(p.config.CertFile)
	if err != nil {
		return nil, err
	}
	return describeCertificate(cert), nil
}

// DescribeCACertificate implements CertificateInspector.
func (p *ProvidedProvider) DescribeCACertificate() (*CertificateInfo, error) {
	if p.config.CAFile == "" {
		return nil, fmt.Errorf("no CA file is configured")
	}
	cert, err := parseCertificateFile(p.config.CAFile)
	if err != nil {
		return nil, err
	}
	return describeCertificate(cert), nil
}

// DescribeServerCertificate implements CertificateInspector for certbot.
func (p *CertbotProvider) DescribeServerCertificate() (*CertificateInfo, error) {
	cert, err := parseCertificateFile(p.config.CertFile)
	if err != nil {
		return nil, err
	}
	return describeCertificate(cert), nil
}

// DescribeCACertificate implements CertificateInspector.
func (p *CertbotProvider) DescribeCACertificate() (*CertificateInfo, error) {
	if p.config.CAFile == "" {
		return nil, fmt.Errorf("no CA file is configured")
	}
	cert, err := parseCertificateFile(p.config.CAFile)
	if err != nil {
		return nil, err
	}
	return describeCertificate(cert), nil
}
