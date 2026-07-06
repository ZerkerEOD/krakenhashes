package hashvalidator

import (
	"strings"
	"testing"
)

func TestValidateVendoredRegex(t *testing.T) {
	v := New()

	cases := []struct {
		name      string
		mode      int
		hash      string
		wantValid bool
	}{
		// MD5 — vendored regex ^[a-f0-9]{32}$
		{"md5_valid", 0, "8743b52063cd84097a65d1633f5c74f5", true},
		{"md5_upper", 0, "8743B52063CD84097A65D1633F5C74F5", true},
		{"md5_short_31", 0, "8743b52063cd84097a65d1633f5c74f", false},
		{"md5_long_33", 0, "8743b52063cd84097a65d1633f5c74f55", false},
		{"md5_non_hex", 0, "8743b52063cd84097a65d1633f5c74fz", false},

		// SHA-1
		{"sha1_valid", 100, "b89eaac7e61417341b710b727768294d0e6a277b", true},
		{"sha1_short", 100, "b89eaac7e61417341b710b727768294d0e6a277", false},

		// sha512crypt
		{"sha512crypt_valid", 1800,
			"$6$52450745$k5ka2p8bFuSmoVT1tzOyyuaREkkKBcCNqoDKzYiJL9RaE8yMnPgh2XzzF0NDrUhgrcLwg78xs1w5pJiypEdFX/", true},
		{"sha512crypt_wrong_prefix", 1800, "$5$52450745$abc", false},

		// bcrypt
		{"bcrypt_2a_valid", 3200, "$2a$05$LhayLxezLhK1LhWvKxCyLOj0j1u.Kj0jZ0pEmm134uzrQlFvQJLF6", true},
		{"bcrypt_2y_valid", 3200, "$2y$10$LhayLxezLhK1LhWvKxCyLOj0j1u.Kj0jZ0pEmm134uzrQlFvQJLF6", true},
		{"bcrypt_wrong_cost", 3200, "$2a$05$tooShort", false},

		// NetNTLMv2
		{"netntlmv2_valid", 5600,
			"admin::N46iSNekpT:08ca45b7d7ea58ee:88dcbe4446168966a153a0064958dac6:5c7830315c7830310000000000000b45c67103d07d7b95acd12ffa11230e0000000052920b85f78d013c31cdb3b92f5d765c783030",
			true},
		{"netntlmv2_truncated", 5600, "admin::N46iSNekpT:08ca", false},

		// Kerberos 5 TGS-REP — keep it short, just check the example
		{"krb5tgs_valid", 13100,
			"$krb5tgs$23$*user$realm$test/spn*$63386d22d359fe42230300d56852c9eb$891ad31d09ab89c6b3b8c5e5de6c06a7f49fd559d7a9a3c32576c8fedf705376cea582ab5938f7fc8bc741acf05c5990741b36ef4311fe3562a41b70a4ec6ecba849905f2385bb3799d92499909658c7287c49160276bca0006c350b0db4fd387adc27c01e9e9ad0c20ed53a7e6356dee2452e35eca2a6a1d1432796fc5c19d068978df74d3d0baf35c77de12456bf1144b6a750d11f55805f5a16ece2975246e2d026dce997fba34ac8757312e9e4e6272de35e20d52fb668c5ed",
			true},
		{"krb5tgs_invalid", 13100, "$krb5tgs$NOPE", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(tc.mode, tc.hash)
			if got.Valid != tc.wantValid {
				t.Fatalf("Validate(%d, %q) = %+v, want Valid=%v", tc.mode, tc.hash, got, tc.wantValid)
			}
			if got.Unvalidated {
				t.Fatalf("Validate(%d, %q) returned Unvalidated unexpectedly", tc.mode, tc.hash)
			}
			if !got.Valid && got.Reason == "" {
				t.Fatalf("Validate(%d, %q) reported invalid with empty Reason", tc.mode, tc.hash)
			}
		})
	}
}

func TestStructuralNTLM(t *testing.T) {
	v := New(DefaultStructural()...)

	cases := []struct {
		name      string
		hash      string
		wantValid bool
	}{
		{"bare_lower", "b4b9b02e6f09a9bd760f388b67351e2b", true},
		{"bare_upper", "B4B9B02E6F09A9BD760F388B67351E2B", true},
		{"dollar_nt_prefix", "$NT$b4b9b02e6f09a9bd760f388b67351e2b", true},
		{"lm_nt_format", "aad3b435b51404eeaad3b435b51404ee:b4b9b02e6f09a9bd760f388b67351e2b", true},
		{"pwdump_format", "Administrator:500:aad3b435b51404eeaad3b435b51404ee:b4b9b02e6f09a9bd760f388b67351e2b:::", true},
		{"too_short", "b4b9b02e6f09a9bd760f388b67351e2", false},
		{"non_hex", "b4b9b02e6f09a9bd760f388b67351e2X", false},
		{"empty_after_prefix", "$NT$", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(1000, tc.hash)
			if got.Valid != tc.wantValid {
				t.Fatalf("Validate(1000, %q) = %+v, want Valid=%v", tc.hash, got, tc.wantValid)
			}
		})
	}
}

func TestStructuralLM(t *testing.T) {
	v := New(DefaultStructural()...)

	cases := []struct {
		name      string
		hash      string
		wantValid bool
	}{
		{"bare", "299bd128c1101fd6aad3b435b51404ee", true},
		{"lm_nt_format", "299bd128c1101fd6aad3b435b51404ee:abcdef0123456789abcdef0123456789", true},
		{"pwdump", "User:500:299bd128c1101fd6aad3b435b51404ee:abcdef0123456789abcdef0123456789:::", true},
		{"blank_lm_pwdump", "User:500:aad3b435b51404eeaad3b435b51404ee:abcdef0123456789abcdef0123456789:::", true},
		{"too_short", "299bd128c1101fd6aad3b435b51404e", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(3000, tc.hash)
			if got.Valid != tc.wantValid {
				t.Fatalf("Validate(3000, %q) = %+v, want Valid=%v", tc.hash, got, tc.wantValid)
			}
		})
	}
}

func TestStructuralJWT(t *testing.T) {
	v := New(DefaultStructural()...)
	const valid = "eyJhbGciOiJIUzI1NiJ9.eyIzNDM2MzQyMCI6NTc2ODc1NDd9.f1nXZ3V_Hrr6ee-AFCTLaHRnrkiKmio2t3JqwL32guY"
	cases := []struct {
		name      string
		hash      string
		wantValid bool
	}{
		{"valid_jwt", valid, true},
		{"two_segments", "eyJhbGciOiJIUzI1NiJ9.eyIzNDM2MzQyMCI6NTc2ODc1NDd9", false},
		{"empty_middle", "eyJhbGciOiJIUzI1NiJ9..f1nXZ3V_Hrr6ee-AFCTLaHRnrkiKmio2t3JqwL32guY", false},
		{"invalid_base64", "!!!.!!!.!!!", false},
		{"non_json_header", "Zm9v.Zm9v.Zm9v", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(16500, tc.hash)
			if got.Valid != tc.wantValid {
				t.Fatalf("Validate(16500, %q) = %+v, want Valid=%v", tc.hash, got, tc.wantValid)
			}
		})
	}
}

func TestStructuralWPA(t *testing.T) {
	v := New(DefaultStructural()...)
	cases := []struct {
		name      string
		hash      string
		wantValid bool
	}{
		{"valid_pmkid", "WPA*01*4d4fe7aac3a2cecab195321ceb99a7d0*fc690c158264*f4747f87f9f4*686173686361742d6573736964***", true},
		{"valid_eapol", "WPA*02*4d4fe7aac3a2cecab195321ceb99a7d0*fc690c158264*f4747f87f9f4*686173686361742d6573736964*aaaa*bbbb*00", true},
		{"not_wpa_prefix", "EAPOL*01*abc*def*ghi*jkl*mno*pqr*stu", false},
		{"wrong_version", "WPA*03*4d4fe7aac3a2cecab195321ceb99a7d0*fc690c158264*f4747f87f9f4*essid***", false},
		{"too_few_fields", "WPA*01*abc*def", false},
		{"bad_mac", "WPA*01*4d4fe7aac3a2cecab195321ceb99a7d0*XXXXXX*f4747f87f9f4*essid***", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(22000, tc.hash)
			if got.Valid != tc.wantValid {
				t.Fatalf("Validate(22000, %q) = %+v, want Valid=%v", tc.hash, got, tc.wantValid)
			}
		})
	}
}

func TestUnvalidatedMode(t *testing.T) {
	v := New()
	// 99999 is well past any defined hashcat mode.
	got := v.Validate(99999, "anything goes here")
	if !got.Valid || !got.Unvalidated {
		t.Fatalf("expected Valid+Unvalidated for unknown mode, got %+v", got)
	}
	if v.HasValidator(99999) {
		t.Fatalf("HasValidator(99999) returned true unexpectedly")
	}
}

func TestExampleFallback(t *testing.T) {
	// Synthesize a fake hashcat mode whose example is plain hex (24 chars).
	const fakeMode = 99001
	v := New(WithExamples(map[int]string{fakeMode: "0123456789abcdef01234567"}))

	if !v.HasValidator(fakeMode) {
		t.Fatalf("expected HasValidator(%d) to be true after WithExamples", fakeMode)
	}
	if r := v.Validate(fakeMode, "0123456789abcdef01234567"); !r.Valid || r.Unvalidated {
		t.Fatalf("expected valid hex match, got %+v", r)
	}
	if r := v.Validate(fakeMode, "0123456789abcdef0123456"); r.Valid {
		t.Fatalf("expected invalid for short hex, got %+v", r)
	}
	if r := v.Validate(fakeMode, "0123456789abcdef0123456z"); r.Valid {
		t.Fatalf("expected invalid for non-hex, got %+v", r)
	}
}

func TestExampleFallbackSkipsStructuralExamples(t *testing.T) {
	// Examples with `$` etc. are too complex for the fallback — should remain Unvalidated.
	const fakeMode = 99002
	v := New(WithExamples(map[int]string{fakeMode: "$2a$05$LhayLxezLhK1LhWvKxCyLO"}))

	if v.HasValidator(fakeMode) {
		t.Fatalf("structural example should not produce a fallback validator")
	}
	r := v.Validate(fakeMode, "anything")
	if !r.Valid || !r.Unvalidated {
		t.Fatalf("expected Valid+Unvalidated, got %+v", r)
	}
}

func TestBatch(t *testing.T) {
	v := New(DefaultStructural()...)
	hashes := []string{
		"8743b52063cd84097a65d1633f5c74f5", // valid
		"8743b52063cd84097a65d1633f5c74f",  // invalid (31 chars)
		"8743b52063cd84097a65d1633f5c74f5", // valid
	}
	res := v.ValidateBatch(0, hashes)
	if res.AllUnvalidated {
		t.Fatalf("AllUnvalidated true unexpectedly")
	}
	if res.ValidCount != 2 || res.InvalidCount != 1 {
		t.Fatalf("got ValidCount=%d InvalidCount=%d, want 2/1", res.ValidCount, res.InvalidCount)
	}
}

func TestBatchAllUnvalidated(t *testing.T) {
	v := New()
	res := v.ValidateBatch(99999, []string{"anything", "goes", "here"})
	if !res.AllUnvalidated {
		t.Fatalf("expected AllUnvalidated=true for unknown mode")
	}
	if res.ValidCount != 0 || res.InvalidCount != 0 {
		t.Fatalf("expected counts 0/0 for unvalidated, got %d/%d", res.ValidCount, res.InvalidCount)
	}
}

func TestEmptyHashRejected(t *testing.T) {
	v := New()
	if got := v.Validate(0, ""); got.Valid {
		t.Fatalf("empty hash should be invalid, got %+v", got)
	}
	if got := v.Validate(0, "   "); got.Valid {
		t.Fatalf("whitespace-only hash should be invalid, got %+v", got)
	}
}

func TestTypeNameFallback(t *testing.T) {
	v := New()
	name := v.TypeName(0)
	if !strings.Contains(strings.ToLower(name), "md5") {
		t.Fatalf("expected MD5 in name, got %q", name)
	}
	if v.TypeName(99999) == "" {
		t.Fatalf("TypeName for unknown mode should return a non-empty fallback")
	}
}
