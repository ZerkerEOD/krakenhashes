package websocket

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"

	khtls "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
)

func stateWithCN(cn string) *tls.ConnectionState {
	return &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{
			{Subject: pkix.Name{CommonName: cn}, SerialNumber: big.NewInt(0xbeef)},
		}},
	}
}

func TestMTLSIdentity(t *testing.T) {
	tests := []struct {
		name     string
		state    *tls.ConnectionState
		wantID   int
		wantOK   bool
		wantErr  bool
		wantSerl string
	}{
		{name: "plain HTTP (no TLS state)", state: nil},
		{name: "TLS without client cert", state: &tls.ConnectionState{}},
		{name: "per-agent cert", state: stateWithCN("42"), wantID: 42, wantOK: true, wantSerl: "beef"},
		// A 2.x agent presents the shared bootstrap cert on every connection.
		// It must fall through to API-key auth, not be rejected.
		{name: "legacy shared bootstrap cert", state: stateWithCN(khtls.LegacySharedClientCN)},
		{name: "unrelated non-numeric CN", state: stateWithCN("evil"), wantErr: true},
		{name: "empty CN", state: stateWithCN(""), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, serial, ok, err := mtlsIdentity(tc.state)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if ok != tc.wantOK || id != tc.wantID || serial != tc.wantSerl {
				t.Fatalf("got (id=%d serial=%q ok=%v), want (id=%d serial=%q ok=%v)",
					id, serial, ok, tc.wantID, tc.wantSerl, tc.wantOK)
			}
		})
	}
}
