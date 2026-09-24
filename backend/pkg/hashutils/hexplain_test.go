package hashutils

import "testing"

func TestDecodeHexPlain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain password untouched", "Summer2026!", "Summer2026!"},
		{"empty untouched", "", ""},
		{"ascii decodes", "$HEX[50617373776f7264]", "Password"},
		{"colon password decodes", "$HEX[506173733a776f72643a313233]", "Pass:word:123"},
		{"uppercase hex decodes", "$HEX[50415353]", "PASS"},
		{"utf-8 decodes", "$HEX[70c3a47373]", "päss"},
		{"empty token decodes to empty", "$HEX[]", ""},
		{"invalid utf-8 kept literal", "$HEX[ff41]", "$HEX[ff41]"},
		{"NUL kept literal", "$HEX[410042]", "$HEX[410042]"},
		{"newline kept literal", "$HEX[410a42]", "$HEX[410a42]"},
		{"carriage return kept literal", "$HEX[410d42]", "$HEX[410d42]"},
		{"odd length kept literal", "$HEX[414]", "$HEX[414]"},
		{"non-hex kept literal", "$HEX[zz]", "$HEX[zz]"},
		{"missing close bracket untouched", "$HEX[4142", "$HEX[4142"},
		{"prefix inside password untouched", "my$HEX[4142]", "my$HEX[4142]"},
		{"trailing text untouched", "$HEX[4142]x", "$HEX[4142]x"},
		{"lowercase tag untouched", "$hex[4142]", "$hex[4142]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeHexPlain(tt.in); got != tt.want {
				t.Errorf("DecodeHexPlain(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Decoding must be idempotent so the backfill can safely run over rows that
// were already decoded on ingest.
func TestDecodeHexPlain_Idempotent(t *testing.T) {
	for _, in := range []string{"$HEX[506173733a776f7264]", "$HEX[ff41]", "Password"} {
		once := DecodeHexPlain(in)
		if twice := DecodeHexPlain(once); twice != once {
			t.Errorf("DecodeHexPlain not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}
