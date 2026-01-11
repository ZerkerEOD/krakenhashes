package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/go-ldap/ldap/v3"
	"github.com/google/uuid"
)

// Provider implements LDAP authentication
type Provider struct {
	*sso.BaseProvider
	config *models.LDAPConfig
}

// NewProvider creates a new LDAP provider
func NewProvider(providerConfig *sso.ProviderConfig) (sso.Provider, error) {
	if providerConfig.LDAPConfig == nil {
		return nil, fmt.Errorf("LDAP config is required")
	}

	return &Provider{
		BaseProvider: sso.NewBaseProvider(providerConfig.Provider),
		config:       providerConfig.LDAPConfig,
	}, nil
}

// Type returns the provider type
func (p *Provider) Type() models.ProviderType {
	return models.ProviderTypeLDAP
}

// Authenticate handles LDAP bind authentication
func (p *Provider) Authenticate(ctx context.Context, req *sso.AuthRequest) (*models.AuthResult, error) {
	if req.Username == "" || req.Password == "" {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Username and password are required",
			ErrorCode:    "missing_credentials",
		}, nil
	}

	conn, err := p.connect()
	if err != nil {
		debug.Error("LDAP connection failed for provider %s: %v", p.Name(), err)
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Failed to connect to directory server",
			ErrorCode:    "connection_failed",
		}, nil
	}
	defer conn.Close()

	// Bind with service account to search for user
	if p.config.BindDN != "" {
		bindPassword, err := p.DecryptSecret(p.config.BindPasswordEncrypted)
		if err != nil {
			debug.Error("Failed to decrypt bind password: %v", err)
			return &models.AuthResult{
				Success:      false,
				ErrorMessage: "Internal configuration error",
				ErrorCode:    "config_error",
			}, nil
		}

		if err := conn.Bind(p.config.BindDN, bindPassword); err != nil {
			debug.Error("LDAP service account bind failed: %v", err)
			return &models.AuthResult{
				Success:      false,
				ErrorMessage: "Directory service authentication failed",
				ErrorCode:    "service_bind_failed",
			}, nil
		}
	}

	// Search for user
	userDN, attributes, err := p.searchUser(conn, req.Username)
	if err != nil {
		debug.Warning("LDAP user search failed for %s: %v", req.Username, err)
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "User not found",
			ErrorCode:    "user_not_found",
		}, nil
	}

	// Bind as user to verify password
	if err := conn.Bind(userDN, req.Password); err != nil {
		debug.Warning("LDAP user bind failed for %s: %v", req.Username, err)
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Invalid credentials",
			ErrorCode:    "invalid_credentials",
		}, nil
	}

	// Build external identity from attributes
	identity := p.buildIdentity(userDN, attributes)

	debug.Info("LDAP authentication successful for user %s via provider %s", req.Username, p.Name())

	return &models.AuthResult{
		Success:     true,
		Identity:    identity,
		RequiresMFA: true, // LDAP always requires local MFA
	}, nil
}

// GetStartURL is not applicable for LDAP (direct auth only)
func (p *Provider) GetStartURL(ctx context.Context, redirectURI string) (string, error) {
	return "", fmt.Errorf("LDAP provider does not support redirect-based authentication")
}

// HandleCallback is not applicable for LDAP (direct auth only)
func (p *Provider) HandleCallback(ctx context.Context, req *sso.CallbackRequest) (*models.AuthResult, error) {
	return nil, fmt.Errorf("LDAP provider does not support callback-based authentication")
}

// TestConnection tests the LDAP connection and optionally the bind credentials
func (p *Provider) TestConnection(ctx context.Context) error {
	conn, err := p.connect()
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	// Test bind if credentials are configured
	if p.config.BindDN != "" {
		bindPassword, err := p.DecryptSecret(p.config.BindPasswordEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt bind password: %w", err)
		}

		if err := conn.Bind(p.config.BindDN, bindPassword); err != nil {
			return fmt.Errorf("bind failed: %w", err)
		}
	}

	debug.Info("LDAP connection test successful for provider %s", p.Name())
	return nil
}

// connect establishes a connection to the LDAP server
func (p *Provider) connect() (*ldap.Conn, error) {
	var conn *ldap.Conn
	var err error

	serverURL := p.config.ServerURL
	timeout := time.Duration(p.config.ConnectionTimeoutSeconds) * time.Second

	// Parse URL to determine connection method
	if strings.HasPrefix(serverURL, "ldaps://") {
		// LDAPS connection
		tlsConfig := p.buildTLSConfig()
		conn, err = ldap.DialURL(serverURL, ldap.DialWithTLSConfig(tlsConfig))
	} else if strings.HasPrefix(serverURL, "ldap://") {
		// LDAP connection (optionally with StartTLS)
		conn, err = ldap.DialURL(serverURL)
		if err == nil && p.config.UseStartTLS {
			tlsConfig := p.buildTLSConfig()
			if err = conn.StartTLS(tlsConfig); err != nil {
				conn.Close()
				return nil, fmt.Errorf("StartTLS failed: %w", err)
			}
		}
	} else {
		return nil, fmt.Errorf("invalid LDAP URL scheme: must start with ldap:// or ldaps://")
	}

	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	// Set timeout
	conn.SetTimeout(timeout)

	return conn, nil
}

// buildTLSConfig creates TLS configuration for LDAP connections
func (p *Provider) buildTLSConfig() *tls.Config {
	config := &tls.Config{
		InsecureSkipVerify: p.config.SkipCertVerify,
	}

	// Add custom CA if provided
	if p.config.CACertificate != "" {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM([]byte(p.config.CACertificate)) {
			config.RootCAs = pool
		} else {
			debug.Warning("Failed to parse custom CA certificate")
		}
	}

	return config
}

// searchUser searches for a user by username and returns their DN and attributes
func (p *Provider) searchUser(conn *ldap.Conn, username string) (string, map[string][]string, error) {
	// Build search filter with username placeholder
	filter := strings.ReplaceAll(p.config.UserSearchFilter, "{{username}}", ldap.EscapeFilter(username))

	// Attributes to retrieve
	attributes := []string{
		"dn",
		p.config.EmailAttribute,
	}
	if p.config.UsernameAttribute != "" {
		attributes = append(attributes, p.config.UsernameAttribute)
	}
	if p.config.DisplayNameAttribute != "" {
		attributes = append(attributes, p.config.DisplayNameAttribute)
	}

	searchRequest := ldap.NewSearchRequest(
		p.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1, // Size limit (only need 1 result)
		p.config.ConnectionTimeoutSeconds,
		false, // Types only
		filter,
		attributes,
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return "", nil, fmt.Errorf("search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return "", nil, fmt.Errorf("user not found")
	}

	if len(result.Entries) > 1 {
		return "", nil, fmt.Errorf("multiple users found with filter")
	}

	entry := result.Entries[0]
	attrs := make(map[string][]string)
	for _, attr := range entry.Attributes {
		attrs[attr.Name] = attr.Values
	}

	return entry.DN, attrs, nil
}

// buildIdentity creates an ExternalIdentity from LDAP attributes
func (p *Provider) buildIdentity(userDN string, attributes map[string][]string) *models.ExternalIdentity {
	identity := &models.ExternalIdentity{
		ExternalID:   userDN,
		ProviderID:   p.ProviderID(),
		ProviderType: models.ProviderTypeLDAP,
		MFAVerified:  false, // LDAP doesn't verify MFA
		Metadata:     make(map[string]interface{}),
	}

	// Extract email
	if emails, ok := attributes[p.config.EmailAttribute]; ok && len(emails) > 0 {
		identity.Email = emails[0]
	}

	// Extract username
	if p.config.UsernameAttribute != "" {
		if usernames, ok := attributes[p.config.UsernameAttribute]; ok && len(usernames) > 0 {
			identity.Username = usernames[0]
		}
	}

	// Extract display name
	if p.config.DisplayNameAttribute != "" {
		if displayNames, ok := attributes[p.config.DisplayNameAttribute]; ok && len(displayNames) > 0 {
			identity.DisplayName = displayNames[0]
		}
	}

	// Store all attributes in metadata
	for key, values := range attributes {
		if len(values) == 1 {
			identity.Metadata[key] = values[0]
		} else {
			identity.Metadata[key] = values
		}
	}

	return identity
}

// Factory creates LDAP provider instances
func Factory(config *sso.ProviderConfig) (sso.Provider, error) {
	return NewProvider(config)
}

// init registers the LDAP factory
func init() {
	// Factory is registered via Manager.RegisterFactory in main initialization
}

// GetConfig returns the LDAP configuration
func (p *Provider) GetConfig() *models.LDAPConfig {
	return p.config
}

// ValidateConfig validates the LDAP configuration
func ValidateConfig(config *models.LDAPConfigInput) error {
	if config.ServerURL == "" {
		return fmt.Errorf("server URL is required")
	}
	if !strings.HasPrefix(config.ServerURL, "ldap://") && !strings.HasPrefix(config.ServerURL, "ldaps://") {
		return fmt.Errorf("server URL must start with ldap:// or ldaps://")
	}
	if config.BaseDN == "" {
		return fmt.Errorf("base DN is required")
	}
	if config.UserSearchFilter == "" {
		return fmt.Errorf("user search filter is required")
	}
	if !strings.Contains(config.UserSearchFilter, "{{username}}") {
		return fmt.Errorf("user search filter must contain {{username}} placeholder")
	}
	if config.EmailAttribute == "" {
		return fmt.Errorf("email attribute is required")
	}
	// Warn about security issues
	if strings.HasPrefix(config.ServerURL, "ldap://") && !config.UseStartTLS {
		debug.Warning("LDAP connection is not encrypted. Consider using ldaps:// or enabling StartTLS")
	}
	if config.SkipCertVerify {
		debug.Warning("Certificate verification is disabled. This is insecure and should only be used for testing")
	}
	return nil
}

// NewLDAPConfig creates a new LDAP config with defaults
func NewLDAPConfig(providerID uuid.UUID) *models.LDAPConfig {
	return &models.LDAPConfig{
		ID:                       uuid.New(),
		ProviderID:               providerID,
		UserSearchFilter:         "(sAMAccountName={{username}})",
		EmailAttribute:           "mail",
		DisplayNameAttribute:     "displayName",
		UsernameAttribute:        "sAMAccountName",
		ConnectionTimeoutSeconds: 10,
	}
}
