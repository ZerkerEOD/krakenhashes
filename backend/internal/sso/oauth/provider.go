package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// Provider implements OAuth2/OIDC authentication
type Provider struct {
	*sso.BaseProvider
	config       *models.OAuthConfig
	oauth2Config *oauth2.Config
	oidcVerifier *oidc.IDTokenVerifier
	oidcProvider *oidc.Provider
	repo         sso.Repository
}

// NewProvider creates a new OAuth/OIDC provider
func NewProvider(providerConfig *sso.ProviderConfig, repo sso.Repository) (sso.Provider, error) {
	if providerConfig.OAuthConfig == nil {
		return nil, fmt.Errorf("OAuth config is required")
	}

	p := &Provider{
		BaseProvider: sso.NewBaseProvider(providerConfig.Provider),
		config:       providerConfig.OAuthConfig,
		repo:         repo,
	}

	// Initialize OAuth2/OIDC configuration
	if err := p.initializeConfig(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize OAuth config: %w", err)
	}

	return p, nil
}

// initializeConfig sets up OAuth2 config and OIDC verifier
func (p *Provider) initializeConfig(ctx context.Context) error {
	// Decrypt client secret
	clientSecret, err := p.DecryptSecret(p.config.ClientSecretEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt client secret: %w", err)
	}

	// If OIDC with discovery URL, use it to auto-configure
	if p.config.IsOIDC && p.config.DiscoveryURL != "" {
		// Normalize issuer URL - ensure trailing slash for compatibility with most OIDC providers
		// (Authentik, Keycloak, Okta, etc. return issuer URLs with trailing slashes)
		issuerURL := strings.TrimSuffix(p.config.DiscoveryURL, "/.well-known/openid-configuration")
		if !strings.HasSuffix(issuerURL, "/") {
			issuerURL += "/"
		}
		provider, err := oidc.NewProvider(ctx, issuerURL)
		if err != nil {
			return fmt.Errorf("failed to create OIDC provider from discovery: %w", err)
		}
		p.oidcProvider = provider
		p.oidcVerifier = provider.Verifier(&oidc.Config{
			ClientID: p.config.ClientID,
		})

		p.oauth2Config = &oauth2.Config{
			ClientID:     p.config.ClientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			Scopes:       p.config.Scopes,
		}
	} else {
		// Manual configuration
		p.oauth2Config = &oauth2.Config{
			ClientID:     p.config.ClientID,
			ClientSecret: clientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  p.config.AuthorizationURL,
				TokenURL: p.config.TokenURL,
			},
			Scopes: p.config.Scopes,
		}

		// Set up OIDC verifier for manual OIDC config
		if p.config.IsOIDC && p.config.JWKSURL != "" {
			keySet := oidc.NewRemoteKeySet(ctx, p.config.JWKSURL)
			p.oidcVerifier = oidc.NewVerifier("", keySet, &oidc.Config{
				ClientID:        p.config.ClientID,
				SkipIssuerCheck: true, // We'll verify manually if needed
			})
		}
	}

	return nil
}

// Type returns the provider type
func (p *Provider) Type() models.ProviderType {
	if p.config.IsOIDC {
		return models.ProviderTypeOIDC
	}
	return models.ProviderTypeOAuth2
}

// Authenticate is not used directly - use GetStartURL and HandleCallback
func (p *Provider) Authenticate(ctx context.Context, req *sso.AuthRequest) (*models.AuthResult, error) {
	return nil, fmt.Errorf("OAuth provider requires redirect-based authentication")
}

// GetStartURL generates the OAuth authorization URL
func (p *Provider) GetStartURL(ctx context.Context, redirectURI string) (string, error) {
	// Generate state and PKCE verifier
	state, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := generateRandomString(64)
	if err != nil {
		return "", fmt.Errorf("failed to generate code verifier: %w", err)
	}

	var nonce string
	if p.config.IsOIDC {
		nonce, err = generateRandomString(32)
		if err != nil {
			return "", fmt.Errorf("failed to generate nonce: %w", err)
		}
	}

	// Store pending authentication state
	pending := &models.PendingOAuthAuthentication{
		State:        state,
		ProviderID:   p.ProviderID(),
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
		Nonce:        nonce,
	}
	if err := p.repo.StorePendingOAuth(ctx, pending); err != nil {
		return "", fmt.Errorf("failed to store pending OAuth: %w", err)
	}

	// Generate code challenge for PKCE
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Build authorization URL with PKCE
	oauth2Config := *p.oauth2Config
	oauth2Config.RedirectURL = redirectURI

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}

	if nonce != "" {
		opts = append(opts, oauth2.SetAuthURLParam("nonce", nonce))
	}

	authURL := oauth2Config.AuthCodeURL(state, opts...)

	debug.Info("Generated OAuth authorization URL for provider %s", p.Name())
	return authURL, nil
}

// HandleCallback processes the OAuth callback
func (p *Provider) HandleCallback(ctx context.Context, req *sso.CallbackRequest) (*models.AuthResult, error) {
	if req.Error != "" {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("OAuth error: %s", req.Error),
			ErrorCode:    "oauth_error",
		}, nil
	}

	if req.State == "" || req.Code == "" {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Missing state or code parameter",
			ErrorCode:    "missing_params",
		}, nil
	}

	// Retrieve and validate pending state
	pending, err := p.repo.GetPendingOAuth(ctx, req.State)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending OAuth: %w", err)
	}
	if pending == nil {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Invalid or expired state parameter",
			ErrorCode:    "invalid_state",
		}, nil
	}

	// Delete pending state (it's single-use)
	defer p.repo.DeletePendingOAuth(ctx, req.State)

	// Exchange code for tokens with PKCE
	oauth2Config := *p.oauth2Config
	oauth2Config.RedirectURL = pending.RedirectURI

	token, err := oauth2Config.Exchange(ctx, req.Code,
		oauth2.SetAuthURLParam("code_verifier", pending.CodeVerifier),
	)
	if err != nil {
		debug.Error("OAuth token exchange failed: %v", err)
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Failed to exchange authorization code",
			ErrorCode:    "token_exchange_failed",
		}, nil
	}

	// Get user identity
	var identity *models.ExternalIdentity
	var mfaVerified bool

	if p.config.IsOIDC {
		// Extract ID token for OIDC
		idToken, err := p.extractIDToken(ctx, token, pending.Nonce)
		if err != nil {
			debug.Error("Failed to extract ID token: %v", err)
			return &models.AuthResult{
				Success:      false,
				ErrorMessage: "Failed to validate ID token",
				ErrorCode:    "invalid_id_token",
			}, nil
		}
		identity, mfaVerified = p.buildIdentityFromIDToken(idToken)
	} else {
		// Fetch userinfo for OAuth2
		identity, err = p.fetchUserInfo(ctx, token)
		if err != nil {
			debug.Error("Failed to fetch user info: %v", err)
			return &models.AuthResult{
				Success:      false,
				ErrorMessage: "Failed to get user information",
				ErrorCode:    "userinfo_failed",
			}, nil
		}
	}

	identity.MFAVerified = mfaVerified
	identity.ProviderID = p.ProviderID()
	identity.ProviderType = p.Type()

	debug.Info("OAuth authentication successful for user %s via provider %s", identity.Email, p.Name())

	return &models.AuthResult{
		Success:     true,
		Identity:    identity,
		RequiresMFA: false, // OAuth/OIDC trusts provider's MFA
	}, nil
}

// extractIDToken extracts and validates the ID token from OAuth response
func (p *Provider) extractIDToken(ctx context.Context, token *oauth2.Token, expectedNonce string) (*oidc.IDToken, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	if p.oidcVerifier == nil {
		return nil, fmt.Errorf("OIDC verifier not configured")
	}

	idToken, err := p.oidcVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("ID token verification failed: %w", err)
	}

	// Verify nonce if set
	if expectedNonce != "" {
		var claims struct {
			Nonce string `json:"nonce"`
		}
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("failed to parse claims: %w", err)
		}
		if claims.Nonce != expectedNonce {
			return nil, fmt.Errorf("nonce mismatch")
		}
	}

	return idToken, nil
}

// buildIdentityFromIDToken creates an ExternalIdentity from OIDC ID token
func (p *Provider) buildIdentityFromIDToken(idToken *oidc.IDToken) (*models.ExternalIdentity, bool) {
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		debug.Warning("Failed to parse ID token claims: %v", err)
		claims = make(map[string]interface{})
	}

	identity := &models.ExternalIdentity{
		ExternalID: idToken.Subject,
		Metadata:   claims,
	}

	// Extract standard claims
	if email, ok := getClaimString(claims, p.config.EmailAttribute); ok {
		identity.Email = email
	}
	if username, ok := getClaimString(claims, p.config.UsernameAttribute); ok {
		identity.Username = username
	}
	if displayName, ok := getClaimString(claims, p.config.DisplayNameAttribute); ok {
		identity.DisplayName = displayName
	}

	// Check for MFA indicators
	mfaVerified := false
	if acr, ok := claims["acr"].(string); ok {
		// Check for MFA acr values
		mfaACRValues := []string{
			"http://schemas.openid.net/pape/policies/2007/06/multi-factor",
			"urn:oasis:names:tc:SAML:2.0:ac:classes:MobileTwoFactorUnregistered",
			"urn:oasis:names:tc:SAML:2.0:ac:classes:MobileTwoFactorContract",
		}
		for _, mfaACR := range mfaACRValues {
			if acr == mfaACR {
				mfaVerified = true
				break
			}
		}
	}
	if amr, ok := claims["amr"].([]interface{}); ok {
		// Check for MFA amr values
		for _, method := range amr {
			if m, ok := method.(string); ok {
				if m == "mfa" || m == "otp" || m == "hwk" || m == "swk" {
					mfaVerified = true
					break
				}
			}
		}
	}

	return identity, mfaVerified
}

// fetchUserInfo fetches user information from the userinfo endpoint (OAuth2)
func (p *Provider) fetchUserInfo(ctx context.Context, token *oauth2.Token) (*models.ExternalIdentity, error) {
	if p.config.UserinfoURL == "" {
		return nil, fmt.Errorf("userinfo URL not configured")
	}

	client := p.oauth2Config.Client(ctx, token)
	resp, err := client.Get(p.config.UserinfoURL)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned status %d: %s", resp.StatusCode, string(body))
	}

	var claims map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	identity := &models.ExternalIdentity{
		Metadata: claims,
	}

	// Extract external ID
	if extID, ok := getClaimString(claims, p.config.ExternalIDAttribute); ok {
		identity.ExternalID = extID
	} else if id, ok := claims["id"]; ok {
		identity.ExternalID = fmt.Sprintf("%v", id)
	} else {
		return nil, fmt.Errorf("could not find external ID in userinfo response")
	}

	// Extract other attributes
	if email, ok := getClaimString(claims, p.config.EmailAttribute); ok {
		identity.Email = email
	}
	if username, ok := getClaimString(claims, p.config.UsernameAttribute); ok {
		identity.Username = username
	}
	if displayName, ok := getClaimString(claims, p.config.DisplayNameAttribute); ok {
		identity.DisplayName = displayName
	}

	return identity, nil
}

// TestConnection tests the OAuth provider configuration
func (p *Provider) TestConnection(ctx context.Context) error {
	// For OIDC with discovery, verify the provider is reachable
	if p.config.IsOIDC && p.config.DiscoveryURL != "" {
		// Same URL normalization as initializeConfig
		issuerURL := strings.TrimSuffix(p.config.DiscoveryURL, "/.well-known/openid-configuration")
		if !strings.HasSuffix(issuerURL, "/") {
			issuerURL += "/"
		}
		_, err := oidc.NewProvider(ctx, issuerURL)
		if err != nil {
			return fmt.Errorf("OIDC discovery failed: %w", err)
		}
	}

	// Verify client secret can be decrypted
	_, err := p.DecryptSecret(p.config.ClientSecretEncrypted)
	if err != nil {
		return fmt.Errorf("client secret decryption failed: %w", err)
	}

	debug.Info("OAuth connection test successful for provider %s", p.Name())
	return nil
}

// Helper functions

func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func getClaimString(claims map[string]interface{}, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	if val, ok := claims[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// Factory creates OAuth provider instances
func Factory(repo sso.Repository) sso.ProviderFactory {
	return func(config *sso.ProviderConfig) (sso.Provider, error) {
		return NewProvider(config, repo)
	}
}

// ValidateConfig validates the OAuth configuration
func ValidateConfig(config *models.OAuthConfigInput) error {
	if config.ClientID == "" {
		return fmt.Errorf("client ID is required")
	}
	if config.ClientSecret == "" {
		return fmt.Errorf("client secret is required")
	}

	if config.IsOIDC {
		// OIDC requires either discovery URL or manual URLs
		if config.DiscoveryURL == "" && config.AuthorizationURL == "" {
			return fmt.Errorf("either discovery URL or authorization URL is required for OIDC")
		}
	} else {
		// OAuth2 requires all URLs
		if config.AuthorizationURL == "" {
			return fmt.Errorf("authorization URL is required")
		}
		if config.TokenURL == "" {
			return fmt.Errorf("token URL is required")
		}
		if config.UserinfoURL == "" {
			return fmt.Errorf("userinfo URL is required for OAuth2")
		}
	}

	if config.ExternalIDAttribute == "" {
		return fmt.Errorf("external ID attribute is required")
	}
	if config.EmailAttribute == "" {
		return fmt.Errorf("email attribute is required")
	}

	return nil
}

// NewOAuthConfig creates a new OAuth config with defaults
func NewOAuthConfig(providerID uuid.UUID, isOIDC bool) *models.OAuthConfig {
	scopes := []string{"openid", "email", "profile"}
	if !isOIDC {
		scopes = []string{"user:email", "read:user"} // GitHub-style
	}

	return &models.OAuthConfig{
		ID:                   uuid.New(),
		ProviderID:           providerID,
		IsOIDC:               isOIDC,
		Scopes:               scopes,
		EmailAttribute:       "email",
		UsernameAttribute:    "preferred_username",
		DisplayNameAttribute: "name",
		ExternalIDAttribute:  "sub",
	}
}
