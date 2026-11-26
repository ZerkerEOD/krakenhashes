package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewOIDCProviderFromDiscovery verifies discovery works whether or not the
// IdP's published issuer ends with a trailing slash.
func TestNewOIDCProviderFromDiscovery(t *testing.T) {
	tests := []struct {
		name       string
		basePath   string // where ".well-known/openid-configuration" is served (no trailing slash)
		issuerPath string // issuer published in the discovery document
	}{
		{"authentik-like (issuer with trailing slash)", "/application/o/kh", "/application/o/kh/"},
		{"azure entra id v2.0 (no trailing slash)", "/tenant/v2.0", "/tenant/v2.0"},
		{"google (root, no trailing slash)", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var srv *httptest.Server
			mux := http.NewServeMux()
			mux.HandleFunc(tt.basePath+"/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{
					"issuer":                 srv.URL + tt.issuerPath,
					"authorization_endpoint": srv.URL + "/auth",
					"token_endpoint":         srv.URL + "/token",
					"jwks_uri":               srv.URL + "/keys",
				})
			})
			srv = httptest.NewServer(mux)
			defer srv.Close()

			discovery := srv.URL + tt.basePath + "/.well-known/openid-configuration"
			if _, err := newOIDCProviderFromDiscovery(context.Background(), discovery); err != nil {
				t.Fatalf("discovery failed for %s: %v", discovery, err)
			}
		})
	}
}

func TestNewOIDCProviderFromDiscoveryIssuerMismatch(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": "https://evil.example.com", "jwks_uri": srv.URL + "/keys"})
	}))
	defer srv.Close()
	if _, err := newOIDCProviderFromDiscovery(context.Background(), srv.URL+"/.well-known/openid-configuration"); err == nil {
		t.Fatal("expected an error when the published issuer does not match the discovery URL")
	}
}
