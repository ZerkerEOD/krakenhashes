package pot

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/hashutils"
)

// Cracked passwords are now stored decoded (GH #90). Potfile export must still
// re-encode the ones hashcat cannot read raw, and the encoding must decode back
// to the same password.
func TestPotExportRoundTripsDecodedPasswords(t *testing.T) {
	tests := []struct {
		password   string
		wantEncode bool
	}{
		{"Pass:word:123", true}, // separator in the plain
		{"tab\there", true},     // control byte
		{"Summer2026!", false},
		{"päss", false},
	}
	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			if got := needsHexEncoding(tt.password); got != tt.wantEncode {
				t.Fatalf("needsHexEncoding(%q) = %v, want %v", tt.password, got, tt.wantEncode)
			}
			if !tt.wantEncode {
				return
			}
			encoded := hexEncodePassword(tt.password)
			if back := hashutils.DecodeHexPlain(encoded); back != tt.password {
				t.Errorf("round trip %q -> %q -> %q", tt.password, encoded, back)
			}
		})
	}
}
