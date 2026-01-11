package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProviderType represents the type of SSO provider
type ProviderType string

const (
	ProviderTypeLDAP   ProviderType = "ldap"
	ProviderTypeSAML   ProviderType = "saml"
	ProviderTypeOIDC   ProviderType = "oidc"
	ProviderTypeOAuth2 ProviderType = "oauth2"
)

// Valid returns true if the provider type is valid
func (pt ProviderType) Valid() bool {
	switch pt {
	case ProviderTypeLDAP, ProviderTypeSAML, ProviderTypeOIDC, ProviderTypeOAuth2:
		return true
	default:
		return false
	}
}

// SSOProvider represents a configured SSO provider
type SSOProvider struct {
	ID              uuid.UUID    `json:"id" db:"id"`
	Name            string       `json:"name" db:"name"`
	ProviderType    ProviderType `json:"provider_type" db:"provider_type"`
	Enabled         bool         `json:"enabled" db:"enabled"`
	DisplayOrder    int          `json:"display_order" db:"display_order"`
	AutoCreateUsers *bool        `json:"auto_create_users,omitempty" db:"auto_create_users"`
	AutoEnableUsers *bool        `json:"auto_enable_users,omitempty" db:"auto_enable_users"`
	CreatedAt       time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at" db:"updated_at"`
}

// SSOProviderWithConfig combines provider with its type-specific config for API responses
type SSOProviderWithConfig struct {
	SSOProvider
	LDAPConfig  *LDAPConfig  `json:"ldap_config,omitempty"`
	SAMLConfig  *SAMLConfig  `json:"saml_config,omitempty"`
	OAuthConfig *OAuthConfig `json:"oauth_config,omitempty"`
}

// LDAPConfig holds LDAP-specific configuration
type LDAPConfig struct {
	ID                       uuid.UUID `json:"id" db:"id"`
	ProviderID               uuid.UUID `json:"provider_id" db:"provider_id"`
	ServerURL                string    `json:"server_url" db:"server_url"`
	BaseDN                   string    `json:"base_dn" db:"base_dn"`
	UserSearchFilter         string    `json:"user_search_filter" db:"user_search_filter"`
	BindDN                   string    `json:"bind_dn,omitempty" db:"bind_dn"`
	BindPasswordEncrypted    string    `json:"-" db:"bind_password_encrypted"`
	UseStartTLS              bool      `json:"use_start_tls" db:"use_start_tls"`
	SkipCertVerify           bool      `json:"skip_cert_verify" db:"skip_cert_verify"`
	CACertificate            string    `json:"ca_certificate,omitempty" db:"ca_certificate"`
	EmailAttribute           string    `json:"email_attribute" db:"email_attribute"`
	DisplayNameAttribute     string    `json:"display_name_attribute,omitempty" db:"display_name_attribute"`
	UsernameAttribute        string    `json:"username_attribute,omitempty" db:"username_attribute"`
	ConnectionTimeoutSeconds int       `json:"connection_timeout_seconds" db:"connection_timeout_seconds"`
}

// LDAPConfigInput is used for creating/updating LDAP configs (accepts plaintext password)
type LDAPConfigInput struct {
	ServerURL                string `json:"server_url"`
	BaseDN                   string `json:"base_dn"`
	UserSearchFilter         string `json:"user_search_filter"`
	BindDN                   string `json:"bind_dn,omitempty"`
	BindPassword             string `json:"bind_password,omitempty"` // Plaintext, will be encrypted
	UseStartTLS              bool   `json:"use_start_tls"`
	SkipCertVerify           bool   `json:"skip_cert_verify"`
	CACertificate            string `json:"ca_certificate,omitempty"`
	EmailAttribute           string `json:"email_attribute"`
	DisplayNameAttribute     string `json:"display_name_attribute,omitempty"`
	UsernameAttribute        string `json:"username_attribute,omitempty"`
	ConnectionTimeoutSeconds int    `json:"connection_timeout_seconds"`
}

// SAMLConfig holds SAML-specific configuration
type SAMLConfig struct {
	ID                          uuid.UUID `json:"id" db:"id"`
	ProviderID                  uuid.UUID `json:"provider_id" db:"provider_id"`
	SPEntityID                  string    `json:"sp_entity_id" db:"sp_entity_id"`
	IDPEntityID                 string    `json:"idp_entity_id" db:"idp_entity_id"`
	IDPSSOURL                   string    `json:"idp_sso_url" db:"idp_sso_url"`
	IDPSLOURL                   string    `json:"idp_slo_url,omitempty" db:"idp_slo_url"`
	IDPCertificate              string    `json:"idp_certificate" db:"idp_certificate"`
	SPPrivateKeyEncrypted       string    `json:"-" db:"sp_private_key_encrypted"`
	SPCertificate               string    `json:"sp_certificate,omitempty" db:"sp_certificate"`
	SignRequests                bool      `json:"sign_requests" db:"sign_requests"`
	RequireSignedAssertions     bool      `json:"require_signed_assertions" db:"require_signed_assertions"`
	RequireEncryptedAssertions  bool      `json:"require_encrypted_assertions" db:"require_encrypted_assertions"`
	NameIDFormat                string    `json:"name_id_format" db:"name_id_format"`
	EmailAttribute              string    `json:"email_attribute" db:"email_attribute"`
	UsernameAttribute           string    `json:"username_attribute,omitempty" db:"username_attribute"`
	DisplayNameAttribute        string    `json:"display_name_attribute,omitempty" db:"display_name_attribute"`
}

// SAMLConfigInput is used for creating/updating SAML configs
type SAMLConfigInput struct {
	SPEntityID                 string `json:"sp_entity_id"`
	IDPEntityID                string `json:"idp_entity_id"`
	IDPSSOURL                  string `json:"idp_sso_url"`
	IDPSLOURL                  string `json:"idp_slo_url,omitempty"`
	IDPCertificate             string `json:"idp_certificate"`
	SPPrivateKey               string `json:"sp_private_key,omitempty"` // Plaintext, will be encrypted
	SPCertificate              string `json:"sp_certificate,omitempty"`
	SignRequests               bool   `json:"sign_requests"`
	RequireSignedAssertions    bool   `json:"require_signed_assertions"`
	RequireEncryptedAssertions bool   `json:"require_encrypted_assertions"`
	NameIDFormat               string `json:"name_id_format"`
	EmailAttribute             string `json:"email_attribute"`
	UsernameAttribute          string `json:"username_attribute,omitempty"`
	DisplayNameAttribute       string `json:"display_name_attribute,omitempty"`
}

// OAuthConfig holds OAuth2/OIDC-specific configuration
type OAuthConfig struct {
	ID                    uuid.UUID `json:"id" db:"id"`
	ProviderID            uuid.UUID `json:"provider_id" db:"provider_id"`
	IsOIDC                bool      `json:"is_oidc" db:"is_oidc"`
	ClientID              string    `json:"client_id" db:"client_id"`
	ClientSecretEncrypted string    `json:"-" db:"client_secret_encrypted"`
	DiscoveryURL          string    `json:"discovery_url,omitempty" db:"discovery_url"`
	AuthorizationURL      string    `json:"authorization_url,omitempty" db:"authorization_url"`
	TokenURL              string    `json:"token_url,omitempty" db:"token_url"`
	UserinfoURL           string    `json:"userinfo_url,omitempty" db:"userinfo_url"`
	JWKSURL               string    `json:"jwks_url,omitempty" db:"jwks_url"`
	Scopes                []string  `json:"scopes" db:"scopes"`
	EmailAttribute        string    `json:"email_attribute" db:"email_attribute"`
	UsernameAttribute     string    `json:"username_attribute,omitempty" db:"username_attribute"`
	DisplayNameAttribute  string    `json:"display_name_attribute,omitempty" db:"display_name_attribute"`
	ExternalIDAttribute   string    `json:"external_id_attribute" db:"external_id_attribute"`
}

// OAuthConfigInput is used for creating/updating OAuth configs
type OAuthConfigInput struct {
	IsOIDC               bool     `json:"is_oidc"`
	ClientID             string   `json:"client_id"`
	ClientSecret         string   `json:"client_secret"` // Plaintext, will be encrypted
	DiscoveryURL         string   `json:"discovery_url,omitempty"`
	AuthorizationURL     string   `json:"authorization_url,omitempty"`
	TokenURL             string   `json:"token_url,omitempty"`
	UserinfoURL          string   `json:"userinfo_url,omitempty"`
	JWKSURL              string   `json:"jwks_url,omitempty"`
	Scopes               []string `json:"scopes"`
	EmailAttribute       string   `json:"email_attribute"`
	UsernameAttribute    string   `json:"username_attribute,omitempty"`
	DisplayNameAttribute string   `json:"display_name_attribute,omitempty"`
	ExternalIDAttribute  string   `json:"external_id_attribute"`
}

// ScanScopes scans a PostgreSQL text array into Scopes slice
func (c *OAuthConfig) ScanScopes(value interface{}) error {
	if value == nil {
		c.Scopes = []string{"openid", "email", "profile"}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		if err := json.Unmarshal(v, &c.Scopes); err == nil {
			return nil
		}
		str := string(v)
		if str[0] == '{' && str[len(str)-1] == '}' {
			str = str[1 : len(str)-1]
			if str == "" {
				c.Scopes = []string{}
				return nil
			}
			c.Scopes = strings.Split(str, ",")
			return nil
		}
		return fmt.Errorf("invalid scopes format: %s", str)
	case string:
		if err := json.Unmarshal([]byte(v), &c.Scopes); err == nil {
			return nil
		}
		if v[0] == '{' && v[len(v)-1] == '}' {
			v = v[1 : len(v)-1]
			if v == "" {
				c.Scopes = []string{}
				return nil
			}
			c.Scopes = strings.Split(v, ",")
			return nil
		}
		return fmt.Errorf("invalid scopes format: %s", v)
	case []string:
		c.Scopes = v
		return nil
	default:
		return fmt.Errorf("unsupported type for scopes: %T", value)
	}
}

// ScopesValue returns scopes in database format
func (c OAuthConfig) ScopesValue() (driver.Value, error) {
	if len(c.Scopes) == 0 {
		return []string{"openid", "email", "profile"}, nil
	}
	return c.Scopes, nil
}

// UserIdentity links an external SSO identity to a local user
type UserIdentity struct {
	ID                  uuid.UUID              `json:"id" db:"id"`
	UserID              uuid.UUID              `json:"user_id" db:"user_id"`
	ProviderID          uuid.UUID              `json:"provider_id" db:"provider_id"`
	ExternalID          string                 `json:"external_id" db:"external_id"`
	ExternalEmail       string                 `json:"external_email,omitempty" db:"external_email"`
	ExternalUsername    string                 `json:"external_username,omitempty" db:"external_username"`
	ExternalDisplayName string                 `json:"external_display_name,omitempty" db:"external_display_name"`
	LastLoginAt         *time.Time             `json:"last_login_at,omitempty" db:"last_login_at"`
	Metadata            map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
	CreatedAt           time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at" db:"updated_at"`
}

// UserIdentityWithProvider includes provider info for display
type UserIdentityWithProvider struct {
	UserIdentity
	ProviderName string       `json:"provider_name"`
	ProviderType ProviderType `json:"provider_type"`
}

// ScanMetadata scans JSONB metadata from database
func (ui *UserIdentity) ScanMetadata(value interface{}) error {
	if value == nil {
		ui.Metadata = make(map[string]interface{})
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, &ui.Metadata)
	case string:
		return json.Unmarshal([]byte(v), &ui.Metadata)
	default:
		return fmt.Errorf("unsupported type for metadata: %T", value)
	}
}

// MetadataValue returns metadata in database format
func (ui UserIdentity) MetadataValue() (driver.Value, error) {
	if ui.Metadata == nil || len(ui.Metadata) == 0 {
		return nil, nil
	}
	return json.Marshal(ui.Metadata)
}

// PendingOAuthAuthentication stores OAuth state during redirect flow
type PendingOAuthAuthentication struct {
	State        string    `json:"state" db:"state"`
	ProviderID   uuid.UUID `json:"provider_id" db:"provider_id"`
	CodeVerifier string    `json:"code_verifier" db:"code_verifier"`
	RedirectURI  string    `json:"redirect_uri" db:"redirect_uri"`
	Nonce        string    `json:"nonce,omitempty" db:"nonce"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// PendingSAMLAuthentication stores SAML request state during redirect flow
type PendingSAMLAuthentication struct {
	RequestID  string    `json:"request_id" db:"request_id"`
	ProviderID uuid.UUID `json:"provider_id" db:"provider_id"`
	RelayState string    `json:"relay_state,omitempty" db:"relay_state"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// SSOAuthSettings holds global SSO-related settings (extension of AuthSettings)
type SSOAuthSettings struct {
	LocalAuthEnabled   bool `json:"local_auth_enabled" db:"local_auth_enabled"`
	LDAPAuthEnabled    bool `json:"ldap_auth_enabled" db:"ldap_auth_enabled"`
	SAMLAuthEnabled    bool `json:"saml_auth_enabled" db:"saml_auth_enabled"`
	OAuthAuthEnabled   bool `json:"oauth_auth_enabled" db:"oauth_auth_enabled"`
	SSOAutoCreateUsers bool `json:"sso_auto_create_users" db:"sso_auto_create_users"`
	SSOAutoEnableUsers bool `json:"sso_auto_enable_users" db:"sso_auto_enable_users"`
}

// ExternalIdentity represents the identity returned from an SSO provider
type ExternalIdentity struct {
	ExternalID   string                 `json:"external_id"`
	Email        string                 `json:"email"`
	Username     string                 `json:"username,omitempty"`
	DisplayName  string                 `json:"display_name,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	MFAVerified  bool                   `json:"mfa_verified"` // True if provider asserts MFA was performed
	ProviderID   uuid.UUID              `json:"provider_id"`
	ProviderType ProviderType           `json:"provider_type"`
}

// AuthResult represents the result of an SSO authentication attempt
type AuthResult struct {
	Success      bool              `json:"success"`
	Identity     *ExternalIdentity `json:"identity,omitempty"`
	RedirectURL  string            `json:"redirect_url,omitempty"`
	SessionToken string            `json:"session_token,omitempty"`
	RequiresMFA  bool              `json:"requires_mfa"`
	ErrorMessage string            `json:"error,omitempty"`
	ErrorCode    string            `json:"error_code,omitempty"`
}

// SSOProviderInput is used for creating/updating SSO providers
type SSOProviderInput struct {
	Name            string       `json:"name"`
	ProviderType    ProviderType `json:"provider_type"`
	Enabled         bool         `json:"enabled"`
	DisplayOrder    int          `json:"display_order"`
	AutoCreateUsers *bool        `json:"auto_create_users,omitempty"`
	AutoEnableUsers *bool        `json:"auto_enable_users,omitempty"`
}

// SSOProviderCreateInput combines provider with config for creation
type SSOProviderCreateInput struct {
	Provider    SSOProviderInput  `json:"provider"`
	LDAPConfig  *LDAPConfigInput  `json:"ldap_config,omitempty"`
	SAMLConfig  *SAMLConfigInput  `json:"saml_config,omitempty"`
	OAuthConfig *OAuthConfigInput `json:"oauth_config,omitempty"`
}

// EnabledSSOProvider is a minimal provider info for login page display
type EnabledSSOProvider struct {
	ID           uuid.UUID    `json:"id"`
	Name         string       `json:"name"`
	ProviderType ProviderType `json:"provider_type"`
	DisplayOrder int          `json:"display_order"`
}

// NewSSOProvider creates a new SSO provider with a generated UUID
func NewSSOProvider(name string, providerType ProviderType) *SSOProvider {
	return &SSOProvider{
		ID:           uuid.New(),
		Name:         name,
		ProviderType: providerType,
		Enabled:      false,
		DisplayOrder: 0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}
