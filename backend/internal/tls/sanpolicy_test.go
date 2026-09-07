package tls

import (
	"errors"
	"testing"
)

func TestValidateInternalIP(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantOK  bool
		wantErr error
	}{
		// RFC 1918 boundaries. The address immediately below each range must be
		// rejected and the first address inside it accepted, because an
		// off-by-one in the mask is the failure mode that silently admits a
		// public address.
		{"below 10/8", "9.255.255.255", false, ErrPublicAddress},
		{"start of 10/8", "10.0.0.0", true, nil},
		{"inside 10/8", "10.1.2.3", true, nil},
		{"end of 10/8", "10.255.255.255", true, nil},
		{"below 172.16/12", "172.15.255.255", false, ErrPublicAddress},
		{"start of 172.16/12", "172.16.0.0", true, nil},
		{"end of 172.16/12", "172.31.255.255", true, nil},
		{"above 172.16/12", "172.32.0.0", false, ErrPublicAddress},
		{"start of 192.168/16", "192.168.0.0", true, nil},
		{"above 192.168/16", "192.169.0.0", false, ErrPublicAddress},

		// CGNAT. This is the Tailscale and NetBird case and the reason the
		// range is in the allowlist at all.
		{"below 100.64/10", "100.63.255.255", false, ErrPublicAddress},
		{"start of CGNAT", "100.64.0.0", true, nil},
		{"tailscale address", "100.113.129.115", true, nil},
		{"end of CGNAT", "100.127.255.255", true, nil},
		{"above CGNAT", "100.128.0.0", false, ErrPublicAddress},

		{"loopback", "127.0.0.1", true, nil},
		{"link local", "169.254.1.1", true, nil},
		{"ipv6 loopback", "::1", true, nil},
		{"ipv6 ULA", "fd7a:115c:a1e0::1", true, nil},
		{"ipv6 link local", "fe80::1", true, nil},
		{"bracketed ipv6", "[fd00::5]", true, nil},

		// An IPv4-mapped IPv6 form of a public address must be normalised to
		// its v4 form BEFORE the allowlist test, or it slips past the v4 ranges
		// unmatched and is only caught by the v6 rules -- which it also does
		// not match, so the result depends on ordering rather than policy.
		{"ipv4-mapped public", "::ffff:8.8.8.8", false, ErrPublicAddress},
		{"ipv4-mapped private", "::ffff:192.168.1.5", true, nil},

		{"public v4", "8.8.8.8", false, ErrPublicAddress},
		{"public v6", "2001:4860:4860::8888", false, ErrPublicAddress},

		{"unspecified v4", "0.0.0.0", false, nil},
		{"unspecified v6", "::", false, nil},
		{"multicast", "224.0.0.1", false, nil},

		{"not an address", "not-an-ip", false, nil},
		{"empty", "", false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateInternalIP(tc.input)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("ValidateInternalIP(%q) = error %v, want accepted", tc.input, err)
				}
				if got == nil {
					t.Fatalf("ValidateInternalIP(%q) returned a nil address", tc.input)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateInternalIP(%q) accepted, want rejected", tc.input)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateInternalIP(%q) = %v, want wrapping %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestValidateInternalIPNormalises(t *testing.T) {
	// "::ffff:192.168.1.5" and "192.168.1.5" must collapse to the same string,
	// or the same address can appear twice in one list.
	mapped, err := ValidateInternalIP("::ffff:192.168.1.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plain, err := ValidateInternalIP("192.168.1.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mapped.String() != plain.String() {
		t.Fatalf("normalisation mismatch: %q vs %q", mapped.String(), plain.String())
	}
}

func TestValidateDNSName(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"simple", "kraken", "kraken", true},
		{"fqdn", "kraken.internal", "kraken.internal", true},
		{"uppercase is normalised", "Kraken.Internal", "kraken.internal", true},
		{"trailing dot is stripped", "kraken.internal.", "kraken.internal", true},
		{"tailscale name", "kraken.tailnet-abcd.ts.net", "kraken.tailnet-abcd.ts.net", true},
		{"hyphenated", "gpu-node-01.lan", "gpu-node-01.lan", true},

		{"wildcard", "*.kraken.internal", "", false},
		{"with scheme", "https://kraken.internal", "", false},
		{"with port", "kraken.internal:31337", "", false},
		{"with path", "kraken.internal/api", "", false},
		{"an IP", "192.168.1.5", "", false},
		{"empty label", "kraken..internal", "", false},
		{"leading hyphen", "-kraken.internal", "", false},
		{"trailing hyphen", "kraken-.internal", "", false},
		{"underscore", "kraken_node.internal", "", false},
		{"empty", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateDNSName(tc.input)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("ValidateDNSName(%q) = error %v, want %q", tc.input, err, tc.want)
				}
				if got != tc.want {
					t.Fatalf("ValidateDNSName(%q) = %q, want %q", tc.input, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateDNSName(%q) = %q, want rejected", tc.input, got)
			}
		})
	}
}

func TestParseSANListReportsEachEntry(t *testing.T) {
	// One bad entry must not discard the good ones: the API reports rejections
	// individually so the admin can see exactly which value is wrong.
	accepted, rejected := ParseSANList("10.0.0.5, 8.8.8.8 ,192.168.1.50,", SANKindIP)

	if len(accepted) != 2 {
		t.Fatalf("accepted = %v, want 2 entries", accepted)
	}
	if accepted[0] != "10.0.0.5" || accepted[1] != "192.168.1.50" {
		t.Fatalf("accepted = %v, want [10.0.0.5 192.168.1.50]", accepted)
	}
	if len(rejected) != 1 || rejected[0].Value != "8.8.8.8" {
		t.Fatalf("rejected = %+v, want one entry for 8.8.8.8", rejected)
	}
}

func TestParseSANListDeduplicates(t *testing.T) {
	accepted, rejected := ParseSANList("10.0.0.5,10.0.0.5,::ffff:10.0.0.5", SANKindIP)
	if len(rejected) != 0 {
		t.Fatalf("rejected = %+v, want none", rejected)
	}
	if len(accepted) != 1 {
		t.Fatalf("accepted = %v, want a single de-duplicated entry", accepted)
	}
}
