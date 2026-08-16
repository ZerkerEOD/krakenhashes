package cloud

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * VPN credential minting must FAIL CLOSED.
 *
 * An instance that cannot join the operator's VPN can never reach the backend:
 * it boots, fails every connection, and bills at GPU rates until its watchdog
 * fires. Refusing to launch is strictly cheaper than launching something
 * unreachable, so every one of these paths must return an error rather than an
 * empty credential.
 *
 * None of these cases may make a network call — a test that reaches
 * api.tailscale.com is both flaky and wrong.
 */
func TestVPNMint_FailsClosed(t *testing.T) {
	minter := NewVPNMinter()
	ctx := context.Background()

	tests := []struct {
		name      string
		cfg       *models.CloudProviderConfig
		decrypted string
		wantErr   string
	}{
		{
			name:      "no VPN provider configured",
			cfg:       &models.CloudProviderConfig{},
			decrypted: "something",
			wantErr:   "no VPN provider configured",
		},
		{
			name:      "empty credential",
			cfg:       &models.CloudProviderConfig{VPNProvider: models.VPNProviderTailscale},
			decrypted: "",
			wantErr:   "empty",
		},
		{
			// A lapsed reusable key means every instance launched from now on
			// is unreachable. Refuse before spending, not after.
			name: "expired reusable key",
			cfg: &models.CloudProviderConfig{
				VPNProvider:            models.VPNProviderTailscale,
				VPNCredentialKind:      models.VPNCredentialReusableKey,
				VPNCredentialExpiresAt: sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true},
			},
			decrypted: "tskey-expired",
			wantErr:   "expired",
		},
		{
			name: "unsupported provider",
			cfg: &models.CloudProviderConfig{
				VPNProvider: models.VPNProvider("openvpn"),
			},
			decrypted: "config",
			wantErr:   "unsupported VPN provider",
		},
		{
			// OAuth-minted Tailscale keys are always tagged, so a missing tag
			// fails at key creation — catch it before the API call.
			name: "tailscale OAuth without a tag",
			cfg: &models.CloudProviderConfig{
				VPNProvider:       models.VPNProviderTailscale,
				VPNCredentialKind: models.VPNCredentialOAuth,
			},
			decrypted: "client_id:client_secret",
			wantErr:   "tag",
		},
		{
			name: "tailscale OAuth credential missing the secret half",
			cfg: &models.CloudProviderConfig{
				VPNProvider:       models.VPNProviderTailscale,
				VPNCredentialKind: models.VPNCredentialOAuth,
				VPNTagOrGroup:     "tag:kraken-worker",
			},
			decrypted: "client_id_only",
			wantErr:   "client_id:client_secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cred, err := minter.Mint(ctx, tc.cfg, tc.decrypted, 15*time.Minute)
			if err == nil {
				t.Fatalf("expected a failure, got credential %+v — an instance that cannot "+
					"join the VPN must never be launched", cred)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
			if cred != nil {
				t.Error("a failed mint must return a nil credential, never a partial one")
			}
		})
	}
}

// TestVPNMint_StaticPathsMakeNoNetworkCall covers the two branches that are
// expected to succeed offline. WireGuard in particular must not reach out:
// there is no per-instance credential to mint.
func TestVPNMint_StaticPathsMakeNoNetworkCall(t *testing.T) {
	minter := NewVPNMinter()
	ctx := context.Background()

	tests := []struct {
		name    string
		cfg     *models.CloudProviderConfig
		plain   string
		wantRef bool
	}{
		{
			name: "wireguard returns the operator config verbatim",
			cfg: &models.CloudProviderConfig{
				VPNProvider:       models.VPNProviderWireGuard,
				VPNCredentialKind: models.VPNCredentialStaticConfig,
			},
			plain:   "[Interface]\nPrivateKey = abc\n",
			wantRef: false,
		},
		{
			name: "tailscale reusable key is passed through",
			cfg: &models.CloudProviderConfig{
				VPNProvider:       models.VPNProviderTailscale,
				VPNCredentialKind: models.VPNCredentialReusableKey,
				VPNTagOrGroup:     "tag:kraken-worker",
			},
			plain:   "tskey-auth-valid",
			wantRef: false,
		},
		{
			name: "netbird reusable setup key is passed through",
			cfg: &models.CloudProviderConfig{
				VPNProvider:       models.VPNProviderNetBird,
				VPNCredentialKind: models.VPNCredentialReusableKey,
			},
			plain:   "nb-setup-key",
			wantRef: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cred, err := minter.Mint(ctx, tc.cfg, tc.plain, 15*time.Minute)
			if err != nil {
				t.Fatalf("static credential path must succeed offline: %v", err)
			}
			if cred.AuthKey != tc.plain {
				t.Errorf("AuthKey = %q, want the stored credential %q", cred.AuthKey, tc.plain)
			}
			// Ref identifies a minted credential for later revocation. Static
			// credentials have nothing to revoke, so it must stay empty —
			// a non-empty Ref would make teardown try to delete a key that was
			// never created.
			if (cred.Ref != "") != tc.wantRef {
				t.Errorf("Ref = %q, wantRef = %v", cred.Ref, tc.wantRef)
			}
		})
	}
}

// TestVPNMint_NotExpiredReusableKeyIsAccepted guards the boundary: only a key
// whose expiry has actually passed is refused.
func TestVPNMint_NotExpiredReusableKeyIsAccepted(t *testing.T) {
	minter := NewVPNMinter()
	cfg := &models.CloudProviderConfig{
		VPNProvider:            models.VPNProviderTailscale,
		VPNCredentialKind:      models.VPNCredentialReusableKey,
		VPNCredentialExpiresAt: sql.NullTime{Time: time.Now().Add(24 * time.Hour), Valid: true},
		VPNTagOrGroup:          "tag:kraken-worker",
	}
	cred, err := minter.Mint(context.Background(), cfg, "tskey-still-valid", 15*time.Minute)
	if err != nil {
		t.Fatalf("a key expiring in 24h must still be usable: %v", err)
	}
	if cred.AuthKey != "tskey-still-valid" {
		t.Errorf("AuthKey = %q", cred.AuthKey)
	}
}
