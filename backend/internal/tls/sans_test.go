package tls

import (
	"crypto/x509"
	"net"
	"reflect"
	"testing"
)

func testProviderConfig(host string) *ProviderConfig {
	return &ProviderConfig{
		Host: host,
		CADetails: &CertificateAuthority{
			Country:            "US",
			Organization:       "KrakenHashes",
			OrganizationalUnit: "KrakenHashes CA",
			CommonName:         "KrakenHashes Root CA",
		},
	}
}

func TestBuildServerSANsAlwaysIncludesLoopback(t *testing.T) {
	// nginx proxies to https://localhost:31337 and the container health check
	// uses loopback, so these must survive any configuration.
	sans := BuildServerSANs(testProviderConfig("0.0.0.0"), DesiredSANs{})

	if !contains(sans.DNSNames, "localhost") {
		t.Fatalf("DNSNames = %v, want localhost", sans.DNSNames)
	}
	if !contains(sans.IPStrings(), "127.0.0.1") {
		t.Fatalf("IPAddresses = %v, want 127.0.0.1", sans.IPStrings())
	}
}

func TestBuildServerSANsDropsBindAddress(t *testing.T) {
	// KH_HOST defaults to 0.0.0.0. It is a bind address: no client dials it, so
	// it can never satisfy VerifyHostname and only makes the certificate look
	// better covered than it is.
	sans := BuildServerSANs(testProviderConfig("0.0.0.0"), DesiredSANs{
		IPAddresses: []string{"0.0.0.0", "::", "192.168.1.50"},
	})

	for _, ip := range sans.IPStrings() {
		if ip == "0.0.0.0" || ip == "::" {
			t.Fatalf("IPAddresses = %v, want no unspecified address", sans.IPStrings())
		}
	}
	if !contains(sans.IPStrings(), "192.168.1.50") {
		t.Fatalf("IPAddresses = %v, want 192.168.1.50", sans.IPStrings())
	}
	if contains(sans.DNSNames, "0.0.0.0") {
		t.Fatalf("DNSNames = %v, want no bind address as a DNS name", sans.DNSNames)
	}
}

func TestBuildServerSANsDropsCACommonName(t *testing.T) {
	// "KrakenHashes Root CA" is not a valid dNSName and can never match a
	// client's target, but older versions put it in every certificate.
	sans := BuildServerSANs(testProviderConfig("0.0.0.0"), DesiredSANs{})
	if contains(sans.DNSNames, "krakenhashes root ca") || contains(sans.DNSNames, "KrakenHashes Root CA") {
		t.Fatalf("DNSNames = %v, want no CA common name", sans.DNSNames)
	}
}

func TestBuildServerSANsHostAsDNSOrIP(t *testing.T) {
	ipHost := BuildServerSANs(testProviderConfig("10.1.2.3"), DesiredSANs{})
	if !contains(ipHost.IPStrings(), "10.1.2.3") {
		t.Fatalf("IPAddresses = %v, want the host as an IP", ipHost.IPStrings())
	}

	nameHost := BuildServerSANs(testProviderConfig("kraken.internal"), DesiredSANs{})
	if !contains(nameHost.DNSNames, "kraken.internal") {
		t.Fatalf("DNSNames = %v, want the host as a DNS name", nameHost.DNSNames)
	}
}

func TestBuildServerSANsIsDeterministic(t *testing.T) {
	// This is the loop-prevention property. The startup drift check compares
	// this against the live certificate on every boot; if the ordering were
	// unstable the server would report drift and reissue on every start.
	desired := DesiredSANs{
		IPAddresses: []string{"192.168.1.50", "10.0.0.5", "100.64.1.7"},
		DNSNames:    []string{"zeta.internal", "alpha.internal"},
	}
	cfg := testProviderConfig("kraken.internal")

	first := BuildServerSANs(cfg, desired)
	second := BuildServerSANs(cfg, desired)

	if !reflect.DeepEqual(first.DNSNames, second.DNSNames) {
		t.Fatalf("DNS ordering unstable: %v vs %v", first.DNSNames, second.DNSNames)
	}
	if !reflect.DeepEqual(first.IPStrings(), second.IPStrings()) {
		t.Fatalf("IP ordering unstable: %v vs %v", first.IPStrings(), second.IPStrings())
	}
}

func TestBuildServerSANsDeduplicates(t *testing.T) {
	sans := BuildServerSANs(testProviderConfig("10.0.0.5"), DesiredSANs{
		IPAddresses: []string{"10.0.0.5", "10.0.0.5", "127.0.0.1"},
		DNSNames:    []string{"localhost", "Localhost", "kraken.internal"},
	})

	if countOf(sans.IPStrings(), "10.0.0.5") != 1 {
		t.Fatalf("IPAddresses = %v, want one 10.0.0.5", sans.IPStrings())
	}
	if countOf(sans.IPStrings(), "127.0.0.1") != 1 {
		t.Fatalf("IPAddresses = %v, want one 127.0.0.1", sans.IPStrings())
	}
	if countOf(sans.DNSNames, "localhost") != 1 {
		t.Fatalf("DNSNames = %v, want one localhost", sans.DNSNames)
	}
}

func TestBuildServerSANsSkipsPublicAddresses(t *testing.T) {
	sans := BuildServerSANs(testProviderConfig("0.0.0.0"), DesiredSANs{
		IPAddresses: []string{"8.8.8.8", "10.0.0.5"},
	})
	if contains(sans.IPStrings(), "8.8.8.8") {
		t.Fatalf("IPAddresses = %v, want no public address", sans.IPStrings())
	}
}

func TestSANSetEqualMatchesGeneratedCertificate(t *testing.T) {
	// The property the drift check depends on: a certificate built FROM a set
	// must compare equal TO that set, or the server reissues forever.
	sans := BuildServerSANs(testProviderConfig("kraken.internal"), DesiredSANs{
		IPAddresses: []string{"192.168.1.50", "10.0.0.5"},
		DNSNames:    []string{"kraken.lan"},
	})

	cert := &x509.Certificate{DNSNames: sans.DNSNames, IPAddresses: sans.IPAddresses}
	if !sans.Equal(cert) {
		t.Fatalf("SANSet does not match a certificate built from itself: %s", sans.String())
	}

	added, removed := sans.Diff(cert)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("Diff = added %v, removed %v, want both empty", added, removed)
	}
}

func TestSANSetEqualIgnoresIPEncoding(t *testing.T) {
	// x509 parsing does not guarantee whether an IPv4 address comes back as 4
	// or 16 bytes; treating those as different would be spurious drift.
	sans := SANSet{
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1").To4()},
	}
	cert := &x509.Certificate{
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1").To16()},
	}
	if !sans.Equal(cert) {
		t.Fatal("Equal treated the 4-byte and 16-byte forms of 127.0.0.1 as different")
	}
}

func TestSANSetDiffReportsBothDirections(t *testing.T) {
	sans := SANSet{
		DNSNames:    []string{"kraken.lan"},
		IPAddresses: []net.IP{net.ParseIP("10.0.0.5").To4()},
	}
	cert := &x509.Certificate{
		DNSNames:    []string{"old.lan"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1").To4()},
	}

	added, removed := sans.Diff(cert)
	if !contains(added, "IP:10.0.0.5") || !contains(added, "DNS:kraken.lan") {
		t.Fatalf("added = %v, want the new entries", added)
	}
	if !contains(removed, "IP:127.0.0.1") || !contains(removed, "DNS:old.lan") {
		t.Fatalf("removed = %v, want the dropped entries", removed)
	}
}

func TestSANSetEqualAgainstNilCertificate(t *testing.T) {
	sans := BuildServerSANs(testProviderConfig("10.0.0.5"), DesiredSANs{})
	if sans.Equal(nil) {
		t.Fatal("Equal(nil) = true, want false so a missing certificate reads as drift")
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func countOf(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}
