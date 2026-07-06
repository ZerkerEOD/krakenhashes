package hashutils

import "testing"

// derefDomain returns a printable representation of a *string domain for messages.
func derefDomain(d *string) string {
	if d == nil {
		return "<nil>"
	}
	return *d
}

// TestExtractUsernameAndDomain_DomainExtraction is the regression suite for the
// "domain manufactured from username" bug: only authoritative formats
// (DOMAIN\user, NetNTLM ::domain:, Kerberos user@REALM) should yield a domain.
// An '@' in a generic/email-style username must NOT become a domain.
func TestExtractUsernameAndDomain_DomainExtraction(t *testing.T) {
	tests := []struct {
		name         string
		rawHash      string
		hashTypeID   int
		wantUsername string
		wantDomain   *string // nil means "no domain expected"
	}{
		{
			name:         "LastPass 6800 email username yields no domain",
			rawHash:      "abcdef0123456789abcdef0123456789:100100:user@company.com",
			hashTypeID:   6800,
			wantUsername: "user@company.com",
			wantDomain:   nil,
		},
		{
			name:         "DCC 1100 email username yields no domain",
			rawHash:      "5a4761123456789abcdef0123456789a:user@corp.local",
			hashTypeID:   1100,
			wantUsername: "user@corp.local",
			wantDomain:   nil,
		},
		{
			name:         "PostgreSQL 12 rule path email username yields no domain",
			rawHash:      "md5a6343a68d964ca596d9752250d54bb8a:user@example.com",
			hashTypeID:   12,
			wantUsername: "user@example.com",
			wantDomain:   nil,
		},
		{
			name:         "Generic heuristic email username yields no domain",
			rawHash:      "bob@example.com:5f4dcc3b5aa765d61d8327deb882cf99",
			hashTypeID:   0, // raw MD5 — no custom extractor, no rule
			wantUsername: "bob@example.com",
			wantDomain:   nil,
		},
		{
			name:         "NetNTLMv2 5600 extracts domain from :: field",
			rawHash:      "user::CORP:1122334455667788:1122334455667788112233445566778811:0101000000000000",
			hashTypeID:   5600,
			wantUsername: "user",
			wantDomain:   strptr("CORP"),
		},
		{
			name:         "Kerberos 18200 extracts realm from @",
			rawHash:      "$krb5asrep$23$user@REALM.LOCAL:1122334455667788abcdef0123456789",
			hashTypeID:   18200,
			wantUsername: "user",
			wantDomain:   strptr("REALM.LOCAL"),
		},
		{
			name:         "NTLM pwdump 1000 extracts domain from backslash",
			rawHash:      `CORP\user:1001:aad3b435b51404eeaad3b435b51404ee:8846f7eaee8fb117ad06bdd830b7586c:::`,
			hashTypeID:   1000,
			wantUsername: "user",
			wantDomain:   strptr("CORP"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUsernameAndDomain(tt.rawHash, tt.hashTypeID)
			if got == nil {
				t.Fatalf("ExtractUsernameAndDomain returned nil; want username=%q domain=%s",
					tt.wantUsername, derefDomain(tt.wantDomain))
			}
			if got.Username == nil || *got.Username != tt.wantUsername {
				t.Errorf("username = %s, want %q", derefDomain(got.Username), tt.wantUsername)
			}
			switch {
			case tt.wantDomain == nil && got.Domain != nil:
				t.Errorf("domain = %q, want <nil> (domain must not be derived from username)", *got.Domain)
			case tt.wantDomain != nil && got.Domain == nil:
				t.Errorf("domain = <nil>, want %q", *tt.wantDomain)
			case tt.wantDomain != nil && got.Domain != nil && *got.Domain != *tt.wantDomain:
				t.Errorf("domain = %q, want %q", *got.Domain, *tt.wantDomain)
			}
		})
	}
}

func TestParseDomainFromBackslash(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantUsername string
		wantDomain   *string
	}{
		{"backslash domain", `CORP\user`, "user", strptr("CORP")},
		{"email is not a domain", "user@company.com", "user@company.com", nil},
		{"plain username", "user", "user", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotDomain := ParseDomainFromBackslash(tt.raw)
			if gotUser != tt.wantUsername {
				t.Errorf("username = %q, want %q", gotUser, tt.wantUsername)
			}
			if (tt.wantDomain == nil) != (gotDomain == nil) {
				t.Fatalf("domain = %s, want %s", derefDomain(gotDomain), derefDomain(tt.wantDomain))
			}
			if tt.wantDomain != nil && *gotDomain != *tt.wantDomain {
				t.Errorf("domain = %q, want %q", *gotDomain, *tt.wantDomain)
			}
		})
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want *string
	}{
		{"nil stays nil", nil, nil},
		{"empty becomes nil", strptr(""), nil},
		{"whitespace becomes nil", strptr("   "), nil},
		{"trim and lowercase", strptr("  CORP.LOCAL  "), strptr("corp.local")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeDomain(tt.in)
			if (tt.want == nil) != (got == nil) {
				t.Fatalf("NormalizeDomain = %s, want %s", derefDomain(got), derefDomain(tt.want))
			}
			if tt.want != nil && *got != *tt.want {
				t.Errorf("NormalizeDomain = %q, want %q", *got, *tt.want)
			}
		})
	}
}

func strptr(s string) *string { return &s }
