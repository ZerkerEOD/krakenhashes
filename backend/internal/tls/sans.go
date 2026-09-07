package tls

import (
	"crypto/x509"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// DesiredSANs is the administrator-controlled portion of the SAN set: the two
// lists managed in Admin -> Settings, or (before they have ever been set) the
// legacy KH_ADDITIONAL_* environment variables.
type DesiredSANs struct {
	DNSNames    []string
	IPAddresses []string
}

// SANSet is a fully resolved, normalised, de-duplicated and deterministically
// ordered SAN set for the server leaf certificate.
type SANSet struct {
	DNSNames    []string
	IPAddresses []net.IP
}

// BuildServerSANs composes the SAN set for the server certificate from the fixed
// defaults, the configured host, and the administrator's additions.
//
// Determinism is a correctness requirement, not a nicety: the startup drift check
// compares this against the SANs on the live certificate, and an unstable
// ordering would report drift on every boot and reissue in a loop.
//
// Two entries that older versions emitted are deliberately excluded:
//
//   - The unspecified addresses 0.0.0.0 and ::. These are bind addresses. No
//     client ever dials them, so they can never satisfy VerifyHostname, and
//     carrying them made certificates look better-covered than they were.
//   - The CA common name ("KrakenHashes Root CA") as a dNSName. It is not a valid
//     DNS name, can never match a client's target, and some strict TLS stacks
//     object to a malformed dNSName.
//
// Both omissions mean an existing deployment reports drift once on upgrade and
// reissues. That reissue is harmless: same CA, so enrolled agents are unaffected.
func BuildServerSANs(cfg *ProviderConfig, extra DesiredSANs) SANSet {
	dnsSeen := make(map[string]struct{})
	ipSeen := make(map[string]struct{})

	var dnsNames []string
	var ipAddresses []net.IP

	addDNS := func(name string) {
		normalised, err := ValidateDNSName(name)
		if err != nil {
			debug.Warning("Skipping invalid DNS name in certificate configuration: %v", err)
			return
		}
		if _, dup := dnsSeen[normalised]; dup {
			return
		}
		dnsSeen[normalised] = struct{}{}
		dnsNames = append(dnsNames, normalised)
	}

	addIP := func(raw string) {
		// Dropping the unspecified address is deliberate, not a misconfiguration:
		// KH_HOST defaults to 0.0.0.0 and older entrypoints put it in the
		// additional-IP list, so warning about it would fire on every normal
		// startup.
		if parsed := net.ParseIP(strings.Trim(strings.TrimSpace(raw), "[]")); parsed != nil && parsed.IsUnspecified() {
			debug.Debug("Omitting bind address %s from certificate SANs; no client connects to it", raw)
			return
		}

		ip, err := ValidateInternalIP(raw)
		if err != nil {
			debug.Warning("Skipping invalid IP address in certificate configuration: %v", err)
			return
		}
		key := ip.String()
		if _, dup := ipSeen[key]; dup {
			return
		}
		ipSeen[key] = struct{}{}
		ipAddresses = append(ipAddresses, ip)
	}

	// Always present. localhost and 127.0.0.1 are load-bearing: nginx proxies to
	// https://localhost:31337 and the container health check uses loopback.
	addDNS("localhost")
	addIP("127.0.0.1")

	// The configured host, as an IP when it parses as one and as a DNS name
	// otherwise. An unspecified address (the common KH_HOST=0.0.0.0) is dropped
	// by ValidateInternalIP.
	if host := strings.TrimSpace(cfg.Host); host != "" {
		if net.ParseIP(strings.Trim(host, "[]")) != nil {
			addIP(host)
		} else {
			addDNS(host)
		}
	}

	for _, name := range extra.DNSNames {
		addDNS(name)
	}
	for _, ip := range extra.IPAddresses {
		addIP(ip)
	}

	sort.Strings(dnsNames)
	sort.Slice(ipAddresses, func(i, j int) bool {
		return ipAddresses[i].String() < ipAddresses[j].String()
	})

	return SANSet{DNSNames: dnsNames, IPAddresses: ipAddresses}
}

// IPStrings renders the IP list for logging, API responses, and the UI.
func (s SANSet) IPStrings() []string {
	out := make([]string, 0, len(s.IPAddresses))
	for _, ip := range s.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// Equal reports whether cert already carries exactly this SAN set.
//
// Order-independent, and IPs are compared with net.IP.Equal so that the 4-byte
// and 16-byte encodings of the same address are not mistaken for drift -- x509
// parsing does not guarantee which form comes back.
func (s SANSet) Equal(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	if len(s.DNSNames) != len(cert.DNSNames) || len(s.IPAddresses) != len(cert.IPAddresses) {
		return false
	}

	// Both sides are duplicate-free -- SANSet by construction, and the certificate
	// because we are the only thing that issues it -- so equal lengths plus
	// one-directional containment is sufficient.
	wantDNS := make(map[string]struct{}, len(s.DNSNames))
	for _, n := range s.DNSNames {
		wantDNS[n] = struct{}{}
	}
	for _, n := range cert.DNSNames {
		if _, ok := wantDNS[strings.ToLower(n)]; !ok {
			return false
		}
	}

	for _, wantIP := range s.IPAddresses {
		found := false
		for _, haveIP := range cert.IPAddresses {
			if wantIP.Equal(haveIP) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// Diff returns human-readable "IP:10.0.0.5" / "DNS:kraken.lan" tokens describing
// what this set adds to and removes from cert. Used for the startup drift log and
// the admin API's drift report.
func (s SANSet) Diff(cert *x509.Certificate) (added, removed []string) {
	var certDNS []string
	var certIPs []net.IP
	if cert != nil {
		certDNS = cert.DNSNames
		certIPs = cert.IPAddresses
	}

	haveDNS := make(map[string]struct{}, len(certDNS))
	for _, n := range certDNS {
		haveDNS[strings.ToLower(n)] = struct{}{}
	}
	wantDNS := make(map[string]struct{}, len(s.DNSNames))
	for _, n := range s.DNSNames {
		wantDNS[n] = struct{}{}
	}

	for _, n := range s.DNSNames {
		if _, ok := haveDNS[n]; !ok {
			added = append(added, "DNS:"+n)
		}
	}
	for _, n := range certDNS {
		if _, ok := wantDNS[strings.ToLower(n)]; !ok {
			removed = append(removed, "DNS:"+n)
		}
	}

	containsIP := func(list []net.IP, target net.IP) bool {
		for _, ip := range list {
			if ip.Equal(target) {
				return true
			}
		}
		return false
	}
	for _, ip := range s.IPAddresses {
		if !containsIP(certIPs, ip) {
			added = append(added, "IP:"+ip.String())
		}
	}
	for _, ip := range certIPs {
		if !containsIP(s.IPAddresses, ip) {
			removed = append(removed, "IP:"+ip.String())
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// String renders the set the way it is logged at startup, so `docker logs`
// answers "what is my certificate actually good for?" without reaching for
// openssl.
func (s SANSet) String() string {
	return fmt.Sprintf("DNS=%v IP=%v", s.DNSNames, s.IPStrings())
}
