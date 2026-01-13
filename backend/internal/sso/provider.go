package sso

import (
	"context"
	"errors"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// Common errors for SSO providers
var (
	ErrProviderNotFound      = errors.New("SSO provider not found")
	ErrProviderDisabled      = errors.New("SSO provider is disabled")
	ErrProviderTypeDisabled  = errors.New("SSO provider type is disabled globally")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrUserNotFound          = errors.New("user not found in directory")
	ErrConnectionFailed      = errors.New("failed to connect to provider")
	ErrInvalidConfiguration  = errors.New("invalid provider configuration")
	ErrAuthenticationFailed  = errors.New("authentication failed")
	ErrInvalidState          = errors.New("invalid state parameter")
	ErrInvalidAssertion      = errors.New("invalid SAML assertion")
	ErrReplayAttack          = errors.New("replay attack detected")
	ErrMissingRequiredClaim  = errors.New("missing required claim from provider")
	ErrLocalAuthDisabled     = errors.New("local authentication is disabled")
	ErrSSOAuthDisabled       = errors.New("SSO authentication is disabled for this user")
)

// Provider defines the interface that all SSO providers must implement
type Provider interface {
	// Type returns the provider type (ldap, saml, oidc, oauth2)
	Type() models.ProviderType

	// ProviderID returns the unique identifier of this provider
	ProviderID() uuid.UUID

	// Name returns the display name of this provider
	Name() string

	// Authenticate handles the authentication flow
	// For LDAP: takes AuthRequest with username/password, returns identity directly
	// For SAML/OAuth: returns redirect URL for initial call, processes callback for subsequent
	Authenticate(ctx context.Context, req *AuthRequest) (*models.AuthResult, error)

	// TestConnection validates the provider configuration
	TestConnection(ctx context.Context) error

	// GetStartURL returns the URL to redirect the user to for authentication
	// Only applicable for SAML and OAuth providers
	GetStartURL(ctx context.Context, redirectURI string) (string, error)

	// HandleCallback processes the callback from the identity provider
	// Only applicable for SAML and OAuth providers
	HandleCallback(ctx context.Context, req *CallbackRequest) (*models.AuthResult, error)
}

// AuthRequest represents an authentication request to an SSO provider
type AuthRequest struct {
	// For LDAP
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// For OAuth/SAML callbacks (flow continuation)
	IsCallback bool   `json:"is_callback,omitempty"`
	State      string `json:"state,omitempty"`
	Code       string `json:"code,omitempty"`

	// Request metadata
	IPAddress string `json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// CallbackRequest represents the callback from an OAuth/SAML provider
type CallbackRequest struct {
	// For OAuth
	State string `json:"state,omitempty"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`

	// For SAML
	SAMLResponse      string `json:"saml_response,omitempty"`
	RelayState        string `json:"relay_state,omitempty"`
	IsRedirectBinding bool   `json:"is_redirect_binding,omitempty"` // true for HTTP-Redirect (GET), false for HTTP-POST (POST)

	// Request metadata
	IPAddress string `json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
}

// ProviderConfig holds the combined configuration for a provider
type ProviderConfig struct {
	Provider    *models.SSOProvider
	LDAPConfig  *models.LDAPConfig
	SAMLConfig  *models.SAMLConfig
	OAuthConfig *models.OAuthConfig
}

// BaseProvider provides common functionality for all providers
type BaseProvider struct {
	provider    *models.SSOProvider
	encryption  *EncryptionService
}

// NewBaseProvider creates a new base provider
func NewBaseProvider(provider *models.SSOProvider) *BaseProvider {
	return &BaseProvider{
		provider:   provider,
		encryption: GetEncryptionService(),
	}
}

// Type returns the provider type
func (b *BaseProvider) Type() models.ProviderType {
	return b.provider.ProviderType
}

// ProviderID returns the provider ID
func (b *BaseProvider) ProviderID() uuid.UUID {
	return b.provider.ID
}

// Name returns the provider name
func (b *BaseProvider) Name() string {
	return b.provider.Name
}

// IsEnabled returns whether the provider is enabled
func (b *BaseProvider) IsEnabled() bool {
	return b.provider.Enabled
}

// EncryptSecret encrypts a secret using the encryption service
func (b *BaseProvider) EncryptSecret(plaintext string) (string, error) {
	return b.encryption.Encrypt(plaintext)
}

// DecryptSecret decrypts a secret using the encryption service
func (b *BaseProvider) DecryptSecret(ciphertext string) (string, error) {
	return b.encryption.Decrypt(ciphertext)
}
