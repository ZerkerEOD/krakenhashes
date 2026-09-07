package tls

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// SANKind distinguishes the two lists an administrator can edit.
type SANKind string

const (
	SANKindIP  SANKind = "ip"
	SANKindDNS SANKind = "dns"
)

// ErrPublicAddress is returned for any address outside the internal allowlist.
//
// There is no override of any kind, by design. KrakenHashes holds cracked
// credentials and is an internal-only tool; naming an internet-routable address
// in its certificate is always a configuration error.
//
// This deliberately refuses some addresses that are, in their operator's own
// network, genuinely private: a VPN can be configured with any range, and a
// self-hosted mesh handing out space that is public by IANA allocation will be
// rejected here. That is the intended outcome. The fix is to renumber the VPN
// onto RFC 1918 or CGNAT space, not to teach this tool to accept globally
// assigned addresses -- an overlay built on someone else's address space also
// stops every peer on it from reaching the real hosts there.
var ErrPublicAddress = errors.New("public IP addresses are not permitted in KrakenHashes certificates")

// SANRejection explains why one entry in a submitted list was refused, so the
// API can point the administrator at the exact value rather than failing the
// whole list with one opaque message.
type SANRejection struct {
	Kind   SANKind `json:"kind"`
	Value  string  `json:"value"`
	Reason string  `json:"reason"`
}

// internalRanges is the complete and only set of address ranges a KrakenHashes
// certificate may name.
//
// 100.64.0.0/10 is Carrier-Grade NAT space, which is what a correctly configured
// VPN mesh uses: Tailscale allocates from it, and NetBird's management server
// picks its default account network as a random /16 inside it.
//
// A VPN can be pointed at any range, including space assigned to someone else on
// the public internet. Such an address is refused here, and that is deliberate --
// see ErrPublicAddress. Renumber the VPN rather than widening this list.
var internalRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"100.64.0.0/10",  // RFC 6598 CGNAT -- Tailscale, NetBird
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fd00::/8",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			// Unreachable: the list above is a compile-time constant. Panicking
			// here beats silently shipping a policy with a hole in it.
			panic(fmt.Sprintf("tls: invalid internal CIDR %q: %v", c, err))
		}
		nets = append(nets, n)
	}
	return nets
}()

// publicAddressMessage is the operator-facing rejection text.
//
// It names the permitted ranges, and says what to do when the address belongs to
// a VPN: renumber the VPN. An operator hitting this on their own overlay has a
// misconfigured VPN, not a KrakenHashes limitation, and telling them so is more
// useful than implying there is a way through.
func publicAddressMessage(value string) string {
	return fmt.Sprintf(
		"%s is a public, internet-routable address. KrakenHashes is an internal-only tool "+
			"and its certificate must never name a public address. Use a private address "+
			"(10.x, 172.16-31.x, 192.168.x), a CGNAT address (100.64-127.x), or a link-local "+
			"address. If this is your VPN's address, the VPN is configured on address space "+
			"that belongs to someone else on the internet - change its network range to "+
			"RFC1918 or 100.64.0.0/10.", value)
}

// InternalRanges returns the permitted ranges, so the admin UI can state the
// policy up front instead of leaving an operator to discover it by rejection.
func InternalRanges() []string {
	out := make([]string, 0, len(internalRanges))
	for _, n := range internalRanges {
		out = append(out, n.String())
	}
	return out
}

// IsInternalIP reports whether ip falls inside the allowlist.
//
// The caller must pass an already-parsed address. Normalisation of IPv4-mapped
// IPv6 forms happens in ValidateInternalIP; calling this directly with a raw
// 16-byte ::ffff:8.8.8.8 would test the IPv6 ranges and wrongly reject rather
// than wrongly accept, but ValidateInternalIP is the supported entry point.
func IsInternalIP(ip net.IP) bool {
	for _, n := range internalRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateInternalIP parses s and accepts it only if it is an internal address.
//
// Returns the normalised address: IPv4 and IPv4-mapped IPv6 both collapse to the
// 4-byte form, so that "::ffff:192.168.1.5" and "192.168.1.5" cannot end up as
// two separate entries in the same list -- and, more importantly, so that
// "::ffff:8.8.8.8" is tested against the IPv4 ranges and correctly rejected
// instead of slipping past as an unmatched IPv6 address.
func ValidateInternalIP(s string) (net.IP, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, errors.New("empty address")
	}

	// Accept a bracketed IPv6 literal, since that is how an admin copying an
	// address out of a URL will have it.
	trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")

	ip := net.ParseIP(trimmed)
	if ip == nil {
		return nil, fmt.Errorf("%q is not a valid IP address", s)
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	if ip.IsUnspecified() {
		return nil, fmt.Errorf(
			"%s is a bind address, not a reachable one. No client ever connects to it, so it "+
				"cannot appear in a certificate. Use the address agents actually dial.", trimmed)
	}
	if ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return nil, fmt.Errorf("%s is a multicast address and cannot identify a server", trimmed)
	}
	if !IsInternalIP(ip) {
		return nil, fmt.Errorf("%w: %s", ErrPublicAddress, publicAddressMessage(trimmed))
	}

	return ip, nil
}

// ValidateDNSName enforces RFC 1123 hostname syntax.
//
// Rejects wildcards deliberately: a wildcard certificate for an internal tool
// widens the blast radius of the private key for no operational gain, and the
// self-signed CA here is not constrained by name.
func ValidateDNSName(s string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	name = strings.TrimSuffix(name, ".") // tolerate a fully-qualified trailing dot

	if name == "" {
		return "", errors.New("empty DNS name")
	}
	if len(name) > 253 {
		return "", fmt.Errorf("%q is longer than the 253-character limit for a DNS name", s)
	}
	if strings.Contains(name, "*") {
		return "", fmt.Errorf("%q is a wildcard; wildcard names are not supported", s)
	}
	if strings.Contains(name, "://") || strings.ContainsAny(name, "/\\ ") {
		return "", fmt.Errorf("%q looks like a URL. Enter only the hostname, with no scheme or path", s)
	}
	if strings.Contains(name, ":") {
		return "", fmt.Errorf("%q includes a port. Enter only the hostname", s)
	}
	// An IP typed into the DNS list is a common slip and the error should say so
	// rather than complaining about label syntax.
	if net.ParseIP(name) != nil {
		return "", fmt.Errorf("%q is an IP address. Add it to the IP address list instead", s)
	}

	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return "", fmt.Errorf("%q contains an empty label", s)
		}
		if len(label) > 63 {
			return "", fmt.Errorf("%q contains a label longer than 63 characters", s)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%q has a label starting or ending with '-'", s)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isAlnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
			if !isAlnum && c != '-' {
				return "", fmt.Errorf("%q contains an invalid character %q", s, string(c))
			}
		}
	}

	return name, nil
}

// ParseSANList splits a comma-separated setting value and validates each entry.
//
// Entries are reported individually rather than failing the list as a whole, so
// the API can tell the administrator exactly which value was refused and why.
// Accepted values come back normalised and de-duplicated, in input order.
func ParseSANList(raw string, kind SANKind) (accepted []string, rejected []SANRejection) {
	seen := make(map[string]struct{})

	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		var normalised string
		var err error
		switch kind {
		case SANKindIP:
			var ip net.IP
			ip, err = ValidateInternalIP(entry)
			if err == nil {
				normalised = ip.String()
			}
		case SANKindDNS:
			normalised, err = ValidateDNSName(entry)
		default:
			err = fmt.Errorf("unknown SAN kind %q", kind)
		}

		if err != nil {
			rejected = append(rejected, SANRejection{Kind: kind, Value: entry, Reason: err.Error()})
			continue
		}
		if _, dup := seen[normalised]; dup {
			continue
		}
		seen[normalised] = struct{}{}
		accepted = append(accepted, normalised)
	}

	return accepted, rejected
}
