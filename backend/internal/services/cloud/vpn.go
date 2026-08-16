package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// VPNCredential is what gets injected into one instance.
type VPNCredential struct {
	AuthKey     string
	LoginServer string
	Tag         string
	// Ref identifies the credential for later revocation. Empty for static
	// configs, which cannot be revoked per-instance.
	Ref string
}

/*
 * VPNMinter issues a per-instance enrollment credential.
 *
 * Preference order, and why:
 *
 *   oauth / pat   Mint a ONE-OFF, EPHEMERAL, pre-authorized credential per
 *                 instance. Blast radius if the rented host reads it: the key
 *                 is already consumed, the node is tag-scoped by ACL, and it
 *                 evaporates minutes after the instance dies.
 *   reusable_key  One key shared by every instance. Tailscale caps auth-key
 *                 lifetime at 90 days, so this is a scheduled outage as well
 *                 as a larger blast radius. Expiry is tracked and provisioning
 *                 is REFUSED once it lapses, rather than launching an instance
 *                 that can never connect.
 *   static_config WireGuard. No per-instance credential and no automatic
 *                 deregistration; documented as the least-safe option.
 *
 * Every path FAILS CLOSED. An instance that cannot join the VPN can never
 * reach the backend, so launching one would burn money until its watchdog
 * fired — strictly worse than not launching at all.
 */
type VPNMinter struct {
	client *http.Client
}

// NewVPNMinter creates a minter.
func NewVPNMinter() *VPNMinter {
	return &VPNMinter{client: &http.Client{Timeout: 30 * time.Second}}
}

// Mint issues a credential for one instance. decrypted is the plaintext VPN
// credential from the provider config.
func (m *VPNMinter) Mint(ctx context.Context, cfg *models.CloudProviderConfig, decrypted string, ttl time.Duration) (*VPNCredential, error) {
	if cfg.VPNProvider == "" {
		return nil, fmt.Errorf("no VPN provider configured; refusing to launch an instance that cannot reach the backend")
	}
	if decrypted == "" {
		return nil, fmt.Errorf("VPN credential for %s is empty", cfg.VPNProvider)
	}

	// A lapsed reusable key means every instance launched from now on would be
	// unreachable. Refuse before spending, not after.
	if cfg.VPNCredentialKind == models.VPNCredentialReusableKey &&
		cfg.VPNCredentialExpiresAt.Valid &&
		time.Now().After(cfg.VPNCredentialExpiresAt.Time) {
		return nil, fmt.Errorf("the reusable %s key expired at %s; rotate it before provisioning",
			cfg.VPNProvider, cfg.VPNCredentialExpiresAt.Time.Format(time.RFC3339))
	}

	switch cfg.VPNProvider {
	case models.VPNProviderTailscale:
		if cfg.VPNCredentialKind == models.VPNCredentialOAuth {
			return m.mintTailscale(ctx, cfg, decrypted, ttl)
		}
		return &VPNCredential{AuthKey: decrypted, Tag: cfg.VPNTagOrGroup, LoginServer: tailscaleLoginServer(cfg)}, nil

	case models.VPNProviderNetBird:
		if cfg.VPNCredentialKind == models.VPNCredentialPAT {
			return m.mintNetBird(ctx, cfg, decrypted, ttl)
		}
		return &VPNCredential{AuthKey: decrypted, Tag: cfg.VPNTagOrGroup, LoginServer: netbirdManagementURL(cfg)}, nil

	case models.VPNProviderWireGuard:
		// Static peer config. We have no access to the operator's WireGuard
		// server to add a peer, so there is nothing to mint and nothing to
		// revoke when the instance dies.
		return &VPNCredential{AuthKey: decrypted}, nil

	default:
		return nil, fmt.Errorf("unsupported VPN provider %q", cfg.VPNProvider)
	}
}

func tailscaleLoginServer(cfg *models.CloudProviderConfig) string {
	if v, ok := cfg.Settings["tailscale_login_server"].(string); ok {
		return v
	}
	return ""
}

func netbirdManagementURL(cfg *models.CloudProviderConfig) string {
	if v, ok := cfg.Settings["netbird_management_url"].(string); ok {
		return v
	}
	return ""
}

/*
 * mintTailscale exchanges an OAuth client for a single-use ephemeral auth key.
 *
 * `decrypted` is "client_id:client_secret".
 *
 * The key is created reusable:false, ephemeral:true, preauthorized:true and
 * tagged. Ephemeral means Tailscale removes the node automatically 30-60
 * minutes after it goes offline, so a churn of rented instances does not leave
 * a graveyard of dead nodes in the tailnet.
 *
 * The OAuth client must hold the auth_keys scope AND have the tag selected, or
 * key creation fails — keys minted via OAuth are always tagged.
 */
func (m *VPNMinter) mintTailscale(ctx context.Context, cfg *models.CloudProviderConfig, decrypted string, ttl time.Duration) (*VPNCredential, error) {
	parts := strings.SplitN(decrypted, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("tailscale OAuth credential must be 'client_id:client_secret'")
	}
	if cfg.VPNTagOrGroup == "" {
		return nil, fmt.Errorf("tailscale OAuth requires a tag (e.g. tag:kraken-worker); OAuth-minted keys are always tagged")
	}

	form := url.Values{"client_id": {parts[0]}, "client_secret": {parts[1]}}
	tokReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tailscale.com/api/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := m.doJSON(tokReq, &tok); err != nil {
		return nil, fmt.Errorf("tailscale OAuth token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("tailscale OAuth returned no access token")
	}

	// Short expiry: the key only has to survive long enough for the instance
	// to boot and register.
	expirySeconds := int(ttl.Seconds())
	if expirySeconds > 900 || expirySeconds <= 0 {
		expirySeconds = 900
	}

	body := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"devices": map[string]interface{}{
				"create": map[string]interface{}{
					"reusable":      false,
					"ephemeral":     true,
					"preauthorized": true,
					"tags":          []string{cfg.VPNTagOrGroup},
				},
			},
		},
		"expirySeconds": expirySeconds,
		"description":   "krakenhashes cloud agent",
	}
	encoded, _ := json.Marshal(body)

	keyReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tailscale.com/api/v2/tailnet/-/keys", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	keyReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	keyReq.Header.Set("Content-Type", "application/json")

	var keyResp struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := m.doJSON(keyReq, &keyResp); err != nil {
		return nil, fmt.Errorf("tailscale create auth key: %w", err)
	}
	if keyResp.Key == "" {
		return nil, fmt.Errorf("tailscale returned an empty auth key")
	}

	debug.Info("Minted ephemeral Tailscale auth key %s (expires in %ds)", keyResp.ID, expirySeconds)
	return &VPNCredential{
		AuthKey:     keyResp.Key,
		Tag:         cfg.VPNTagOrGroup,
		LoginServer: tailscaleLoginServer(cfg),
		Ref:         keyResp.ID,
	}, nil
}

/*
 * mintNetBird creates a one-off ephemeral setup key.
 *
 * Note expires_in has a documented MINIMUM of 86400 seconds (1 day) — a
 * 15-minute key is not possible here. What actually bounds the credential is
 * usage_limit:1 plus ephemeral:true, which removes the peer ~10 minutes after
 * it goes offline.
 */
func (m *VPNMinter) mintNetBird(ctx context.Context, cfg *models.CloudProviderConfig, token string, ttl time.Duration) (*VPNCredential, error) {
	base := netbirdManagementURL(cfg)
	if base == "" {
		base = "https://api.netbird.io"
	}

	body := map[string]interface{}{
		"name":        "krakenhashes-cloud-agent",
		"type":        "one-off",
		"expires_in":  86400,
		"usage_limit": 1,
		"ephemeral":   true,
	}
	if cfg.VPNTagOrGroup != "" {
		body["auto_groups"] = []string{cfg.VPNTagOrGroup}
	}
	encoded, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/api/setup-keys", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", "application/json")

	var resp struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := m.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("netbird create setup key: %w", err)
	}
	if resp.Key == "" {
		return nil, fmt.Errorf("netbird returned an empty setup key")
	}

	debug.Info("Minted ephemeral NetBird setup key %s", resp.ID)
	return &VPNCredential{
		AuthKey:     resp.Key,
		Tag:         cfg.VPNTagOrGroup,
		LoginServer: base,
		Ref:         resp.ID,
	}, nil
}

func (m *VPNMinter) doJSON(req *http.Request, out interface{}) error {
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out != nil && len(payload) > 0 {
		return json.Unmarshal(payload, out)
	}
	return nil
}
