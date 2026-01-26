package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SSORepository handles database operations for SSO providers and identities
type SSORepository struct {
	db *db.DB
}

// NewSSORepository creates a new SSO repository
func NewSSORepository(db *db.DB) *SSORepository {
	return &SSORepository{db: db}
}

// ============================================================================
// SSO Settings Operations
// ============================================================================

// GetSSOSettings retrieves the global SSO settings from auth_settings
func (r *SSORepository) GetSSOSettings(ctx context.Context) (*models.SSOAuthSettings, error) {
	settings := &models.SSOAuthSettings{}
	query := `
		SELECT local_auth_enabled, ldap_auth_enabled, saml_auth_enabled,
		       oauth_auth_enabled, sso_auto_create_users, sso_auto_enable_users
		FROM auth_settings
		LIMIT 1
	`
	err := r.db.QueryRowContext(ctx, query).Scan(
		&settings.LocalAuthEnabled,
		&settings.LDAPAuthEnabled,
		&settings.SAMLAuthEnabled,
		&settings.OAuthAuthEnabled,
		&settings.SSOAutoCreateUsers,
		&settings.SSOAutoEnableUsers,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSO settings: %w", err)
	}
	return settings, nil
}

// UpdateSSOSettings updates the global SSO settings in auth_settings
func (r *SSORepository) UpdateSSOSettings(ctx context.Context, settings *models.SSOAuthSettings) error {
	query := `
		UPDATE auth_settings
		SET local_auth_enabled = $1,
		    ldap_auth_enabled = $2,
		    saml_auth_enabled = $3,
		    oauth_auth_enabled = $4,
		    sso_auto_create_users = $5,
		    sso_auto_enable_users = $6
	`
	_, err := r.db.ExecContext(ctx, query,
		settings.LocalAuthEnabled,
		settings.LDAPAuthEnabled,
		settings.SAMLAuthEnabled,
		settings.OAuthAuthEnabled,
		settings.SSOAutoCreateUsers,
		settings.SSOAutoEnableUsers,
	)
	if err != nil {
		return fmt.Errorf("failed to update SSO settings: %w", err)
	}
	return nil
}

// ============================================================================
// SSO Provider Operations
// ============================================================================

// GetProvider retrieves an SSO provider by ID
func (r *SSORepository) GetProvider(ctx context.Context, id uuid.UUID) (*models.SSOProvider, error) {
	provider := &models.SSOProvider{}
	var autoCreate, autoEnable sql.NullBool

	query := `
		SELECT id, name, provider_type, enabled, display_order,
		       auto_create_users, auto_enable_users, created_at, updated_at
		FROM sso_providers
		WHERE id = $1
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&provider.ID,
		&provider.Name,
		&provider.ProviderType,
		&provider.Enabled,
		&provider.DisplayOrder,
		&autoCreate,
		&autoEnable,
		&provider.CreatedAt,
		&provider.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	if autoCreate.Valid {
		provider.AutoCreateUsers = &autoCreate.Bool
	}
	if autoEnable.Valid {
		provider.AutoEnableUsers = &autoEnable.Bool
	}

	return provider, nil
}

// GetProviderByType retrieves all SSO providers of a specific type
func (r *SSORepository) GetProviderByType(ctx context.Context, providerType models.ProviderType) ([]*models.SSOProvider, error) {
	query := `
		SELECT id, name, provider_type, enabled, display_order,
		       auto_create_users, auto_enable_users, created_at, updated_at
		FROM sso_providers
		WHERE provider_type = $1
		ORDER BY display_order, name
	`
	rows, err := r.db.QueryContext(ctx, query, providerType)
	if err != nil {
		return nil, fmt.Errorf("failed to get providers by type: %w", err)
	}
	defer rows.Close()

	return r.scanProviders(rows)
}

// GetEnabledProviders retrieves all enabled SSO providers for the login page
func (r *SSORepository) GetEnabledProviders(ctx context.Context) ([]*models.EnabledSSOProvider, error) {
	query := `
		SELECT id, name, provider_type, display_order
		FROM sso_providers
		WHERE enabled = true
		ORDER BY display_order, name
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled providers: %w", err)
	}
	defer rows.Close()

	var providers []*models.EnabledSSOProvider
	for rows.Next() {
		p := &models.EnabledSSOProvider{}
		if err := rows.Scan(&p.ID, &p.Name, &p.ProviderType, &p.DisplayOrder); err != nil {
			return nil, fmt.Errorf("failed to scan enabled provider: %w", err)
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// ListProviders retrieves all SSO providers
func (r *SSORepository) ListProviders(ctx context.Context) ([]*models.SSOProvider, error) {
	query := `
		SELECT id, name, provider_type, enabled, display_order,
		       auto_create_users, auto_enable_users, created_at, updated_at
		FROM sso_providers
		ORDER BY display_order, name
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	defer rows.Close()

	return r.scanProviders(rows)
}

func (r *SSORepository) scanProviders(rows *sql.Rows) ([]*models.SSOProvider, error) {
	var providers []*models.SSOProvider
	for rows.Next() {
		p := &models.SSOProvider{}
		var autoCreate, autoEnable sql.NullBool

		if err := rows.Scan(
			&p.ID, &p.Name, &p.ProviderType, &p.Enabled, &p.DisplayOrder,
			&autoCreate, &autoEnable, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan provider: %w", err)
		}

		if autoCreate.Valid {
			p.AutoCreateUsers = &autoCreate.Bool
		}
		if autoEnable.Valid {
			p.AutoEnableUsers = &autoEnable.Bool
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// CreateProvider creates a new SSO provider
func (r *SSORepository) CreateProvider(ctx context.Context, provider *models.SSOProvider) error {
	query := `
		INSERT INTO sso_providers (id, name, provider_type, enabled, display_order,
		                           auto_create_users, auto_enable_users, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.ExecContext(ctx, query,
		provider.ID,
		provider.Name,
		provider.ProviderType,
		provider.Enabled,
		provider.DisplayOrder,
		provider.AutoCreateUsers,
		provider.AutoEnableUsers,
		provider.CreatedAt,
		provider.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}
	return nil
}

// UpdateProvider updates an existing SSO provider
func (r *SSORepository) UpdateProvider(ctx context.Context, provider *models.SSOProvider) error {
	query := `
		UPDATE sso_providers
		SET name = $2, enabled = $3, display_order = $4,
		    auto_create_users = $5, auto_enable_users = $6, updated_at = $7
		WHERE id = $1
	`
	provider.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		provider.ID,
		provider.Name,
		provider.Enabled,
		provider.DisplayOrder,
		provider.AutoCreateUsers,
		provider.AutoEnableUsers,
		provider.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update provider: %w", err)
	}
	return nil
}

// DeleteProvider deletes an SSO provider and its config
func (r *SSORepository) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM sso_providers WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}
	return nil
}

// ============================================================================
// LDAP Config Operations
// ============================================================================

// GetLDAPConfig retrieves the LDAP configuration for a provider
func (r *SSORepository) GetLDAPConfig(ctx context.Context, providerID uuid.UUID) (*models.LDAPConfig, error) {
	config := &models.LDAPConfig{}
	var bindDN, bindPwd, caCert, displayName, username sql.NullString

	query := `
		SELECT id, provider_id, server_url, base_dn, user_search_filter,
		       bind_dn, bind_password_encrypted, use_start_tls, skip_cert_verify,
		       ca_certificate, email_attribute, display_name_attribute, username_attribute,
		       connection_timeout_seconds
		FROM ldap_configs
		WHERE provider_id = $1
	`
	err := r.db.QueryRowContext(ctx, query, providerID).Scan(
		&config.ID,
		&config.ProviderID,
		&config.ServerURL,
		&config.BaseDN,
		&config.UserSearchFilter,
		&bindDN,
		&bindPwd,
		&config.UseStartTLS,
		&config.SkipCertVerify,
		&caCert,
		&config.EmailAttribute,
		&displayName,
		&username,
		&config.ConnectionTimeoutSeconds,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get LDAP config: %w", err)
	}

	if bindDN.Valid {
		config.BindDN = bindDN.String
	}
	if bindPwd.Valid {
		config.BindPasswordEncrypted = bindPwd.String
	}
	if caCert.Valid {
		config.CACertificate = caCert.String
	}
	if displayName.Valid {
		config.DisplayNameAttribute = displayName.String
	}
	if username.Valid {
		config.UsernameAttribute = username.String
	}

	return config, nil
}

// CreateLDAPConfig creates a new LDAP configuration
func (r *SSORepository) CreateLDAPConfig(ctx context.Context, config *models.LDAPConfig) error {
	query := `
		INSERT INTO ldap_configs (id, provider_id, server_url, base_dn, user_search_filter,
		                          bind_dn, bind_password_encrypted, use_start_tls, skip_cert_verify,
		                          ca_certificate, email_attribute, display_name_attribute,
		                          username_attribute, connection_timeout_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ID,
		config.ProviderID,
		config.ServerURL,
		config.BaseDN,
		config.UserSearchFilter,
		nullString(config.BindDN),
		nullString(config.BindPasswordEncrypted),
		config.UseStartTLS,
		config.SkipCertVerify,
		nullString(config.CACertificate),
		config.EmailAttribute,
		nullString(config.DisplayNameAttribute),
		nullString(config.UsernameAttribute),
		config.ConnectionTimeoutSeconds,
	)
	if err != nil {
		return fmt.Errorf("failed to create LDAP config: %w", err)
	}
	return nil
}

// UpdateLDAPConfig updates an existing LDAP configuration
func (r *SSORepository) UpdateLDAPConfig(ctx context.Context, config *models.LDAPConfig) error {
	query := `
		UPDATE ldap_configs
		SET server_url = $2, base_dn = $3, user_search_filter = $4,
		    bind_dn = $5, bind_password_encrypted = $6, use_start_tls = $7,
		    skip_cert_verify = $8, ca_certificate = $9, email_attribute = $10,
		    display_name_attribute = $11, username_attribute = $12,
		    connection_timeout_seconds = $13
		WHERE provider_id = $1
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ProviderID,
		config.ServerURL,
		config.BaseDN,
		config.UserSearchFilter,
		nullString(config.BindDN),
		nullString(config.BindPasswordEncrypted),
		config.UseStartTLS,
		config.SkipCertVerify,
		nullString(config.CACertificate),
		config.EmailAttribute,
		nullString(config.DisplayNameAttribute),
		nullString(config.UsernameAttribute),
		config.ConnectionTimeoutSeconds,
	)
	if err != nil {
		return fmt.Errorf("failed to update LDAP config: %w", err)
	}
	return nil
}

// ============================================================================
// SAML Config Operations
// ============================================================================

// GetSAMLConfig retrieves the SAML configuration for a provider
func (r *SSORepository) GetSAMLConfig(ctx context.Context, providerID uuid.UUID) (*models.SAMLConfig, error) {
	config := &models.SAMLConfig{}
	var sloURL, spPrivateKey, spCert, usernameAttr, displayNameAttr sql.NullString

	query := `
		SELECT id, provider_id, sp_entity_id, idp_entity_id, idp_sso_url, idp_slo_url,
		       idp_certificate, sp_private_key_encrypted, sp_certificate,
		       sign_requests, require_signed_assertions, require_encrypted_assertions,
		       name_id_format, email_attribute, username_attribute, display_name_attribute
		FROM saml_configs
		WHERE provider_id = $1
	`
	err := r.db.QueryRowContext(ctx, query, providerID).Scan(
		&config.ID,
		&config.ProviderID,
		&config.SPEntityID,
		&config.IDPEntityID,
		&config.IDPSSOURL,
		&sloURL,
		&config.IDPCertificate,
		&spPrivateKey,
		&spCert,
		&config.SignRequests,
		&config.RequireSignedAssertions,
		&config.RequireEncryptedAssertions,
		&config.NameIDFormat,
		&config.EmailAttribute,
		&usernameAttr,
		&displayNameAttr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get SAML config: %w", err)
	}

	if sloURL.Valid {
		config.IDPSLOURL = sloURL.String
	}
	if spPrivateKey.Valid {
		config.SPPrivateKeyEncrypted = spPrivateKey.String
	}
	if spCert.Valid {
		config.SPCertificate = spCert.String
	}
	if usernameAttr.Valid {
		config.UsernameAttribute = usernameAttr.String
	}
	if displayNameAttr.Valid {
		config.DisplayNameAttribute = displayNameAttr.String
	}

	return config, nil
}

// CreateSAMLConfig creates a new SAML configuration
func (r *SSORepository) CreateSAMLConfig(ctx context.Context, config *models.SAMLConfig) error {
	query := `
		INSERT INTO saml_configs (id, provider_id, sp_entity_id, idp_entity_id, idp_sso_url,
		                          idp_slo_url, idp_certificate, sp_private_key_encrypted,
		                          sp_certificate, sign_requests, require_signed_assertions,
		                          require_encrypted_assertions, name_id_format, email_attribute,
		                          username_attribute, display_name_attribute)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ID,
		config.ProviderID,
		config.SPEntityID,
		config.IDPEntityID,
		config.IDPSSOURL,
		nullString(config.IDPSLOURL),
		config.IDPCertificate,
		nullString(config.SPPrivateKeyEncrypted),
		nullString(config.SPCertificate),
		config.SignRequests,
		config.RequireSignedAssertions,
		config.RequireEncryptedAssertions,
		config.NameIDFormat,
		config.EmailAttribute,
		nullString(config.UsernameAttribute),
		nullString(config.DisplayNameAttribute),
	)
	if err != nil {
		return fmt.Errorf("failed to create SAML config: %w", err)
	}
	return nil
}

// UpdateSAMLConfig updates an existing SAML configuration
func (r *SSORepository) UpdateSAMLConfig(ctx context.Context, config *models.SAMLConfig) error {
	query := `
		UPDATE saml_configs
		SET sp_entity_id = $2, idp_entity_id = $3, idp_sso_url = $4, idp_slo_url = $5,
		    idp_certificate = $6, sp_private_key_encrypted = $7, sp_certificate = $8,
		    sign_requests = $9, require_signed_assertions = $10, require_encrypted_assertions = $11,
		    name_id_format = $12, email_attribute = $13, username_attribute = $14,
		    display_name_attribute = $15
		WHERE provider_id = $1
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ProviderID,
		config.SPEntityID,
		config.IDPEntityID,
		config.IDPSSOURL,
		nullString(config.IDPSLOURL),
		config.IDPCertificate,
		nullString(config.SPPrivateKeyEncrypted),
		nullString(config.SPCertificate),
		config.SignRequests,
		config.RequireSignedAssertions,
		config.RequireEncryptedAssertions,
		config.NameIDFormat,
		config.EmailAttribute,
		nullString(config.UsernameAttribute),
		nullString(config.DisplayNameAttribute),
	)
	if err != nil {
		return fmt.Errorf("failed to update SAML config: %w", err)
	}
	return nil
}

// ============================================================================
// OAuth Config Operations
// ============================================================================

// GetOAuthConfig retrieves the OAuth configuration for a provider
func (r *SSORepository) GetOAuthConfig(ctx context.Context, providerID uuid.UUID) (*models.OAuthConfig, error) {
	config := &models.OAuthConfig{}
	var discoveryURL, authURL, tokenURL, userinfoURL, jwksURL sql.NullString
	var usernameAttr, displayNameAttr sql.NullString
	var scopes interface{}

	query := `
		SELECT id, provider_id, is_oidc, client_id, client_secret_encrypted,
		       discovery_url, authorization_url, token_url, userinfo_url, jwks_url,
		       scopes, email_attribute, username_attribute, display_name_attribute,
		       external_id_attribute
		FROM oauth_configs
		WHERE provider_id = $1
	`
	err := r.db.QueryRowContext(ctx, query, providerID).Scan(
		&config.ID,
		&config.ProviderID,
		&config.IsOIDC,
		&config.ClientID,
		&config.ClientSecretEncrypted,
		&discoveryURL,
		&authURL,
		&tokenURL,
		&userinfoURL,
		&jwksURL,
		&scopes,
		&config.EmailAttribute,
		&usernameAttr,
		&displayNameAttr,
		&config.ExternalIDAttribute,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth config: %w", err)
	}

	if discoveryURL.Valid {
		config.DiscoveryURL = discoveryURL.String
	}
	if authURL.Valid {
		config.AuthorizationURL = authURL.String
	}
	if tokenURL.Valid {
		config.TokenURL = tokenURL.String
	}
	if userinfoURL.Valid {
		config.UserinfoURL = userinfoURL.String
	}
	if jwksURL.Valid {
		config.JWKSURL = jwksURL.String
	}
	if usernameAttr.Valid {
		config.UsernameAttribute = usernameAttr.String
	}
	if displayNameAttr.Valid {
		config.DisplayNameAttribute = displayNameAttr.String
	}

	// Parse scopes
	if err := config.ScanScopes(scopes); err != nil {
		return nil, fmt.Errorf("failed to parse scopes: %w", err)
	}

	return config, nil
}

// CreateOAuthConfig creates a new OAuth configuration
func (r *SSORepository) CreateOAuthConfig(ctx context.Context, config *models.OAuthConfig) error {
	query := `
		INSERT INTO oauth_configs (id, provider_id, is_oidc, client_id, client_secret_encrypted,
		                           discovery_url, authorization_url, token_url, userinfo_url,
		                           jwks_url, scopes, email_attribute, username_attribute,
		                           display_name_attribute, external_id_attribute)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ID,
		config.ProviderID,
		config.IsOIDC,
		config.ClientID,
		config.ClientSecretEncrypted,
		nullString(config.DiscoveryURL),
		nullString(config.AuthorizationURL),
		nullString(config.TokenURL),
		nullString(config.UserinfoURL),
		nullString(config.JWKSURL),
		pq.Array(config.Scopes),
		config.EmailAttribute,
		nullString(config.UsernameAttribute),
		nullString(config.DisplayNameAttribute),
		config.ExternalIDAttribute,
	)
	if err != nil {
		return fmt.Errorf("failed to create OAuth config: %w", err)
	}
	return nil
}

// UpdateOAuthConfig updates an existing OAuth configuration
func (r *SSORepository) UpdateOAuthConfig(ctx context.Context, config *models.OAuthConfig) error {
	query := `
		UPDATE oauth_configs
		SET is_oidc = $2, client_id = $3, client_secret_encrypted = $4,
		    discovery_url = $5, authorization_url = $6, token_url = $7,
		    userinfo_url = $8, jwks_url = $9, scopes = $10, email_attribute = $11,
		    username_attribute = $12, display_name_attribute = $13, external_id_attribute = $14
		WHERE provider_id = $1
	`
	_, err := r.db.ExecContext(ctx, query,
		config.ProviderID,
		config.IsOIDC,
		config.ClientID,
		config.ClientSecretEncrypted,
		nullString(config.DiscoveryURL),
		nullString(config.AuthorizationURL),
		nullString(config.TokenURL),
		nullString(config.UserinfoURL),
		nullString(config.JWKSURL),
		pq.Array(config.Scopes),
		config.EmailAttribute,
		nullString(config.UsernameAttribute),
		nullString(config.DisplayNameAttribute),
		config.ExternalIDAttribute,
	)
	if err != nil {
		return fmt.Errorf("failed to update OAuth config: %w", err)
	}
	return nil
}

// ============================================================================
// User Identity Operations
// ============================================================================

// GetUserIdentityByID retrieves a user identity by its ID
func (r *SSORepository) GetUserIdentityByID(ctx context.Context, id uuid.UUID) (*models.UserIdentity, error) {
	identity := &models.UserIdentity{}
	var email, username, displayName sql.NullString
	var lastLogin sql.NullTime
	var metadata interface{}

	query := `
		SELECT id, user_id, provider_id, external_id, external_email, external_username,
		       external_display_name, last_login_at, metadata, created_at, updated_at
		FROM user_identities
		WHERE id = $1
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&identity.ID,
		&identity.UserID,
		&identity.ProviderID,
		&identity.ExternalID,
		&email,
		&username,
		&displayName,
		&lastLogin,
		&metadata,
		&identity.CreatedAt,
		&identity.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user identity by ID: %w", err)
	}

	if email.Valid {
		identity.ExternalEmail = email.String
	}
	if username.Valid {
		identity.ExternalUsername = username.String
	}
	if displayName.Valid {
		identity.ExternalDisplayName = displayName.String
	}
	if lastLogin.Valid {
		identity.LastLoginAt = &lastLogin.Time
	}
	if err := identity.ScanMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return identity, nil
}

// GetUserIdentity retrieves a user identity by provider and external ID
func (r *SSORepository) GetUserIdentity(ctx context.Context, providerID uuid.UUID, externalID string) (*models.UserIdentity, error) {
	identity := &models.UserIdentity{}
	var email, username, displayName sql.NullString
	var lastLogin sql.NullTime
	var metadata interface{}

	query := `
		SELECT id, user_id, provider_id, external_id, external_email, external_username,
		       external_display_name, last_login_at, metadata, created_at, updated_at
		FROM user_identities
		WHERE provider_id = $1 AND external_id = $2
	`
	err := r.db.QueryRowContext(ctx, query, providerID, externalID).Scan(
		&identity.ID,
		&identity.UserID,
		&identity.ProviderID,
		&identity.ExternalID,
		&email,
		&username,
		&displayName,
		&lastLogin,
		&metadata,
		&identity.CreatedAt,
		&identity.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user identity: %w", err)
	}

	if email.Valid {
		identity.ExternalEmail = email.String
	}
	if username.Valid {
		identity.ExternalUsername = username.String
	}
	if displayName.Valid {
		identity.ExternalDisplayName = displayName.String
	}
	if lastLogin.Valid {
		identity.LastLoginAt = &lastLogin.Time
	}
	if err := identity.ScanMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return identity, nil
}

// GetUserIdentityByEmail retrieves a user identity by provider and external email
func (r *SSORepository) GetUserIdentityByEmail(ctx context.Context, providerID uuid.UUID, email string) (*models.UserIdentity, error) {
	identity := &models.UserIdentity{}
	var extEmail, username, displayName sql.NullString
	var lastLogin sql.NullTime
	var metadata interface{}

	query := `
		SELECT id, user_id, provider_id, external_id, external_email, external_username,
		       external_display_name, last_login_at, metadata, created_at, updated_at
		FROM user_identities
		WHERE provider_id = $1 AND LOWER(external_email) = LOWER($2)
	`
	err := r.db.QueryRowContext(ctx, query, providerID, email).Scan(
		&identity.ID,
		&identity.UserID,
		&identity.ProviderID,
		&identity.ExternalID,
		&extEmail,
		&username,
		&displayName,
		&lastLogin,
		&metadata,
		&identity.CreatedAt,
		&identity.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user identity by email: %w", err)
	}

	if extEmail.Valid {
		identity.ExternalEmail = extEmail.String
	}
	if username.Valid {
		identity.ExternalUsername = username.String
	}
	if displayName.Valid {
		identity.ExternalDisplayName = displayName.String
	}
	if lastLogin.Valid {
		identity.LastLoginAt = &lastLogin.Time
	}
	if err := identity.ScanMetadata(metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return identity, nil
}

// GetUserIdentities retrieves all identities for a user with provider info
func (r *SSORepository) GetUserIdentities(ctx context.Context, userID uuid.UUID) ([]*models.UserIdentityWithProvider, error) {
	query := `
		SELECT ui.id, ui.user_id, ui.provider_id, ui.external_id, ui.external_email,
		       ui.external_username, ui.external_display_name, ui.last_login_at,
		       ui.metadata, ui.created_at, ui.updated_at,
		       sp.name as provider_name, sp.provider_type
		FROM user_identities ui
		JOIN sso_providers sp ON ui.provider_id = sp.id
		WHERE ui.user_id = $1
		ORDER BY ui.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user identities: %w", err)
	}
	defer rows.Close()

	var identities []*models.UserIdentityWithProvider
	for rows.Next() {
		i := &models.UserIdentityWithProvider{}
		var email, username, displayName sql.NullString
		var lastLogin sql.NullTime
		var metadata interface{}

		if err := rows.Scan(
			&i.ID, &i.UserID, &i.ProviderID, &i.ExternalID, &email,
			&username, &displayName, &lastLogin, &metadata,
			&i.CreatedAt, &i.UpdatedAt, &i.ProviderName, &i.ProviderType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan identity: %w", err)
		}

		if email.Valid {
			i.ExternalEmail = email.String
		}
		if username.Valid {
			i.ExternalUsername = username.String
		}
		if displayName.Valid {
			i.ExternalDisplayName = displayName.String
		}
		if lastLogin.Valid {
			i.LastLoginAt = &lastLogin.Time
		}
		if err := i.ScanMetadata(metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}

		identities = append(identities, i)
	}
	return identities, rows.Err()
}

// CreateUserIdentity creates a new user identity link
func (r *SSORepository) CreateUserIdentity(ctx context.Context, identity *models.UserIdentity) error {
	var metadataJSON []byte
	var err error
	if identity.Metadata != nil {
		metadataJSON, err = json.Marshal(identity.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	query := `
		INSERT INTO user_identities (id, user_id, provider_id, external_id, external_email,
		                             external_username, external_display_name, metadata,
		                             created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	identity.CreatedAt = time.Now()
	identity.UpdatedAt = identity.CreatedAt

	_, err = r.db.ExecContext(ctx, query,
		identity.ID,
		identity.UserID,
		identity.ProviderID,
		identity.ExternalID,
		nullString(identity.ExternalEmail),
		nullString(identity.ExternalUsername),
		nullString(identity.ExternalDisplayName),
		metadataJSON,
		identity.CreatedAt,
		identity.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user identity: %w", err)
	}
	return nil
}

// UpdateUserIdentity updates a user identity (e.g., last login time)
func (r *SSORepository) UpdateUserIdentity(ctx context.Context, identity *models.UserIdentity) error {
	var metadataJSON []byte
	var err error
	if identity.Metadata != nil {
		metadataJSON, err = json.Marshal(identity.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	query := `
		UPDATE user_identities
		SET external_email = $2, external_username = $3, external_display_name = $4,
		    last_login_at = $5, metadata = $6, updated_at = $7
		WHERE id = $1
	`
	identity.UpdatedAt = time.Now()

	_, err = r.db.ExecContext(ctx, query,
		identity.ID,
		nullString(identity.ExternalEmail),
		nullString(identity.ExternalUsername),
		nullString(identity.ExternalDisplayName),
		identity.LastLoginAt,
		metadataJSON,
		identity.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update user identity: %w", err)
	}
	return nil
}

// DeleteUserIdentity deletes a user identity link
func (r *SSORepository) DeleteUserIdentity(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM user_identities WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user identity: %w", err)
	}
	return nil
}

// ============================================================================
// Pending Authentication State Operations
// ============================================================================

// StorePendingOAuth stores OAuth state for the redirect flow
func (r *SSORepository) StorePendingOAuth(ctx context.Context, pending *models.PendingOAuthAuthentication) error {
	query := `
		INSERT INTO pending_oauth_authentication (state, provider_id, code_verifier,
		                                          redirect_uri, nonce, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	pending.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		pending.State,
		pending.ProviderID,
		pending.CodeVerifier,
		pending.RedirectURI,
		nullString(pending.Nonce),
		pending.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to store pending OAuth: %w", err)
	}
	return nil
}

// GetPendingOAuth retrieves pending OAuth state by state parameter
func (r *SSORepository) GetPendingOAuth(ctx context.Context, state string) (*models.PendingOAuthAuthentication, error) {
	pending := &models.PendingOAuthAuthentication{}
	var nonce sql.NullString

	query := `
		SELECT state, provider_id, code_verifier, redirect_uri, nonce, created_at
		FROM pending_oauth_authentication
		WHERE state = $1
	`
	err := r.db.QueryRowContext(ctx, query, state).Scan(
		&pending.State,
		&pending.ProviderID,
		&pending.CodeVerifier,
		&pending.RedirectURI,
		&nonce,
		&pending.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending OAuth: %w", err)
	}

	if nonce.Valid {
		pending.Nonce = nonce.String
	}

	return pending, nil
}

// DeletePendingOAuth deletes pending OAuth state
func (r *SSORepository) DeletePendingOAuth(ctx context.Context, state string) error {
	query := `DELETE FROM pending_oauth_authentication WHERE state = $1`
	_, err := r.db.ExecContext(ctx, query, state)
	if err != nil {
		return fmt.Errorf("failed to delete pending OAuth: %w", err)
	}
	return nil
}

// StorePendingSAML stores SAML request state
func (r *SSORepository) StorePendingSAML(ctx context.Context, pending *models.PendingSAMLAuthentication) error {
	query := `
		INSERT INTO pending_saml_authentication (request_id, provider_id, relay_state, created_at)
		VALUES ($1, $2, $3, $4)
	`
	pending.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		pending.RequestID,
		pending.ProviderID,
		nullString(pending.RelayState),
		pending.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to store pending SAML: %w", err)
	}
	return nil
}

// GetPendingSAML retrieves pending SAML state by request ID
func (r *SSORepository) GetPendingSAML(ctx context.Context, requestID string) (*models.PendingSAMLAuthentication, error) {
	pending := &models.PendingSAMLAuthentication{}
	var relayState sql.NullString

	query := `
		SELECT request_id, provider_id, relay_state, created_at
		FROM pending_saml_authentication
		WHERE request_id = $1
	`
	err := r.db.QueryRowContext(ctx, query, requestID).Scan(
		&pending.RequestID,
		&pending.ProviderID,
		&relayState,
		&pending.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending SAML: %w", err)
	}

	if relayState.Valid {
		pending.RelayState = relayState.String
	}

	return pending, nil
}

// DeletePendingSAML deletes pending SAML state
func (r *SSORepository) DeletePendingSAML(ctx context.Context, requestID string) error {
	query := `DELETE FROM pending_saml_authentication WHERE request_id = $1`
	_, err := r.db.ExecContext(ctx, query, requestID)
	if err != nil {
		return fmt.Errorf("failed to delete pending SAML: %w", err)
	}
	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// nullString returns a sql.NullString for empty strings
func nullString(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// FindUserByEmail finds a user by email (for SSO linking)
func (r *SSORepository) FindUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}
	var lastFailedAttempt, accountLockedUntil, lastLogin, disabledAt sql.NullTime
	var disabledReason sql.NullString
	var localAuthOverride, ssoAuthOverride sql.NullBool
	var authOverrideNotes sql.NullString

	query := `
		SELECT id, username, email, password_hash, role, account_enabled,
		       account_locked, account_locked_until, last_login,
		       failed_login_attempts, last_failed_attempt,
		       disabled_reason, disabled_at,
		       local_auth_override, sso_auth_override, auth_override_notes
		FROM users
		WHERE LOWER(email) = LOWER($1)
	`
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.AccountEnabled,
		&user.AccountLocked,
		&accountLockedUntil,
		&lastLogin,
		&user.FailedLoginAttempts,
		&lastFailedAttempt,
		&disabledReason,
		&disabledAt,
		&localAuthOverride,
		&ssoAuthOverride,
		&authOverrideNotes,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}

	if accountLockedUntil.Valid {
		user.AccountLockedUntil = &accountLockedUntil.Time
	}
	if lastLogin.Valid {
		user.LastLogin = &lastLogin.Time
	}
	if lastFailedAttempt.Valid {
		user.LastFailedAttempt = &lastFailedAttempt.Time
	}
	if disabledReason.Valid {
		user.DisabledReason = &disabledReason.String
	}
	if disabledAt.Valid {
		user.DisabledAt = &disabledAt.Time
	}
	if localAuthOverride.Valid {
		user.LocalAuthOverride = &localAuthOverride.Bool
	}
	if ssoAuthOverride.Valid {
		user.SSOAuthOverride = &ssoAuthOverride.Bool
	}
	if authOverrideNotes.Valid {
		user.AuthOverrideNotes = &authOverrideNotes.String
	}

	return user, nil
}

// ============================================================================
// Login Attempt Operations (with SSO support)
// ============================================================================

// CreateSSOLoginAttempt records a login attempt with SSO provider information
func (r *SSORepository) CreateSSOLoginAttempt(ctx context.Context, attempt *models.LoginAttempt) error {
	query := `
		INSERT INTO login_attempts (
			user_id, username, ip_address, user_agent,
			success, failure_reason, provider_id, provider_type
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.ExecContext(ctx, query,
		attempt.UserID,
		nullString(attempt.Username),
		attempt.IPAddress,
		attempt.UserAgent,
		attempt.Success,
		nullString(attempt.FailureReason),
		attempt.ProviderID,
		nullString(attempt.ProviderType),
	)
	if err != nil {
		return fmt.Errorf("failed to create SSO login attempt: %w", err)
	}
	return nil
}
