-- Migration: Add SSO (LDAP, SAML, OAuth/OIDC) Support
-- This migration adds tables and columns for external authentication providers

-- ============================================================================
-- EXTEND EXISTING TABLES
-- ============================================================================

-- Add SSO global toggles to auth_settings
ALTER TABLE auth_settings
ADD COLUMN local_auth_enabled BOOLEAN NOT NULL DEFAULT true,
ADD COLUMN ldap_auth_enabled BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN saml_auth_enabled BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN oauth_auth_enabled BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN sso_auto_create_users BOOLEAN NOT NULL DEFAULT true,
ADD COLUMN sso_auto_enable_users BOOLEAN NOT NULL DEFAULT false;

-- Add per-user auth overrides to users table
ALTER TABLE users
ADD COLUMN local_auth_override BOOLEAN,
ADD COLUMN sso_auth_override BOOLEAN,
ADD COLUMN auth_override_notes TEXT;

COMMENT ON COLUMN users.local_auth_override IS 'NULL = use global setting, true/false = override';
COMMENT ON COLUMN users.sso_auth_override IS 'NULL = use global setting, true/false = override';
COMMENT ON COLUMN users.auth_override_notes IS 'Admin notes explaining the override reason';

-- ============================================================================
-- SSO PROVIDER TABLES
-- ============================================================================

-- Base table for all SSO providers
CREATE TABLE sso_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    provider_type VARCHAR(50) NOT NULL CHECK (provider_type IN ('ldap', 'saml', 'oidc', 'oauth2')),
    enabled BOOLEAN NOT NULL DEFAULT false,
    display_order INT NOT NULL DEFAULT 0,
    auto_create_users BOOLEAN,
    auto_enable_users BOOLEAN,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sso_providers_enabled ON sso_providers(enabled);
CREATE INDEX idx_sso_providers_type ON sso_providers(provider_type);

COMMENT ON TABLE sso_providers IS 'Base configuration for all SSO providers';
COMMENT ON COLUMN sso_providers.auto_create_users IS 'NULL = use global default from auth_settings';
COMMENT ON COLUMN sso_providers.auto_enable_users IS 'NULL = use global default from auth_settings';

-- Trigger to update updated_at on sso_providers
CREATE TRIGGER update_sso_providers_updated_at
    BEFORE UPDATE ON sso_providers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- LDAP CONFIGURATION
-- ============================================================================

CREATE TABLE ldap_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    server_url VARCHAR(500) NOT NULL,
    base_dn VARCHAR(500) NOT NULL,
    user_search_filter VARCHAR(500) NOT NULL DEFAULT '(sAMAccountName={{username}})',
    bind_dn VARCHAR(500),
    bind_password_encrypted TEXT,
    use_start_tls BOOLEAN NOT NULL DEFAULT false,
    skip_cert_verify BOOLEAN NOT NULL DEFAULT false,
    ca_certificate TEXT,
    email_attribute VARCHAR(100) NOT NULL DEFAULT 'mail',
    display_name_attribute VARCHAR(100) DEFAULT 'displayName',
    username_attribute VARCHAR(100) DEFAULT 'sAMAccountName',
    connection_timeout_seconds INT NOT NULL DEFAULT 10,
    UNIQUE(provider_id)
);

COMMENT ON TABLE ldap_configs IS 'LDAP-specific configuration for SSO providers';
COMMENT ON COLUMN ldap_configs.server_url IS 'LDAP server URL (ldaps:// recommended)';
COMMENT ON COLUMN ldap_configs.user_search_filter IS 'Filter template with {{username}} placeholder';
COMMENT ON COLUMN ldap_configs.bind_password_encrypted IS 'AES-256-GCM encrypted bind password';
COMMENT ON COLUMN ldap_configs.skip_cert_verify IS 'Disable cert verification (testing only)';

-- ============================================================================
-- SAML CONFIGURATION
-- ============================================================================

CREATE TABLE saml_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    sp_entity_id VARCHAR(500) NOT NULL,
    idp_entity_id VARCHAR(500) NOT NULL,
    idp_sso_url VARCHAR(500) NOT NULL,
    idp_slo_url VARCHAR(500),
    idp_certificate TEXT NOT NULL,
    sp_private_key_encrypted TEXT,
    sp_certificate TEXT,
    sign_requests BOOLEAN NOT NULL DEFAULT false,
    require_signed_assertions BOOLEAN NOT NULL DEFAULT true,
    require_encrypted_assertions BOOLEAN NOT NULL DEFAULT false,
    name_id_format VARCHAR(200) DEFAULT 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress',
    email_attribute VARCHAR(100) NOT NULL DEFAULT 'email',
    username_attribute VARCHAR(100) DEFAULT 'username',
    display_name_attribute VARCHAR(100) DEFAULT 'displayName',
    UNIQUE(provider_id)
);

COMMENT ON TABLE saml_configs IS 'SAML-specific configuration for SSO providers';
COMMENT ON COLUMN saml_configs.sp_entity_id IS 'Service Provider entity ID (our identifier)';
COMMENT ON COLUMN saml_configs.idp_certificate IS 'IdP certificate for signature validation';
COMMENT ON COLUMN saml_configs.sp_private_key_encrypted IS 'AES-256-GCM encrypted SP private key for request signing';

-- ============================================================================
-- OAUTH/OIDC CONFIGURATION
-- ============================================================================

CREATE TABLE oauth_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    is_oidc BOOLEAN NOT NULL DEFAULT true,
    client_id VARCHAR(500) NOT NULL,
    client_secret_encrypted TEXT NOT NULL,
    discovery_url VARCHAR(500),
    authorization_url VARCHAR(500),
    token_url VARCHAR(500),
    userinfo_url VARCHAR(500),
    jwks_url VARCHAR(500),
    scopes TEXT[] NOT NULL DEFAULT ARRAY['openid', 'email', 'profile'],
    email_attribute VARCHAR(100) NOT NULL DEFAULT 'email',
    username_attribute VARCHAR(100) DEFAULT 'preferred_username',
    display_name_attribute VARCHAR(100) DEFAULT 'name',
    external_id_attribute VARCHAR(100) NOT NULL DEFAULT 'sub',
    UNIQUE(provider_id)
);

COMMENT ON TABLE oauth_configs IS 'OAuth2/OIDC configuration for SSO providers';
COMMENT ON COLUMN oauth_configs.is_oidc IS 'true for OIDC (with ID token), false for OAuth2-only';
COMMENT ON COLUMN oauth_configs.discovery_url IS 'OIDC discovery endpoint (.well-known/openid-configuration)';
COMMENT ON COLUMN oauth_configs.client_secret_encrypted IS 'AES-256-GCM encrypted client secret';

-- ============================================================================
-- USER IDENTITY LINKING
-- ============================================================================

CREATE TABLE user_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    external_id VARCHAR(500) NOT NULL,
    external_email VARCHAR(255),
    external_username VARCHAR(255),
    external_display_name VARCHAR(255),
    last_login_at TIMESTAMP WITH TIME ZONE,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(provider_id, external_id)
);

CREATE INDEX idx_user_identities_user_id ON user_identities(user_id);
CREATE INDEX idx_user_identities_external_email ON user_identities(external_email);

COMMENT ON TABLE user_identities IS 'Links external SSO identities to local user accounts';
COMMENT ON COLUMN user_identities.external_id IS 'Unique ID from external provider (sub, DN, NameID)';
COMMENT ON COLUMN user_identities.metadata IS 'Additional claims/attributes from provider';

-- Trigger to update updated_at on user_identities
CREATE TRIGGER update_user_identities_updated_at
    BEFORE UPDATE ON user_identities
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- PENDING AUTHENTICATION STATE (OAuth)
-- ============================================================================

CREATE TABLE pending_oauth_authentication (
    state VARCHAR(64) PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    code_verifier VARCHAR(128) NOT NULL,
    redirect_uri VARCHAR(500) NOT NULL,
    nonce VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pending_oauth_created ON pending_oauth_authentication(created_at);

COMMENT ON TABLE pending_oauth_authentication IS 'Stores OAuth state during redirect flow (auto-cleaned)';

-- Cleanup function for expired OAuth states
CREATE OR REPLACE FUNCTION cleanup_expired_oauth_states()
RETURNS trigger AS $$
BEGIN
    DELETE FROM pending_oauth_authentication WHERE created_at < NOW() - INTERVAL '10 minutes';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_cleanup_oauth_states
    AFTER INSERT ON pending_oauth_authentication
    EXECUTE FUNCTION cleanup_expired_oauth_states();

-- ============================================================================
-- PENDING AUTHENTICATION STATE (SAML)
-- ============================================================================

CREATE TABLE pending_saml_authentication (
    request_id VARCHAR(64) PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    relay_state VARCHAR(500),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pending_saml_created ON pending_saml_authentication(created_at);

COMMENT ON TABLE pending_saml_authentication IS 'Stores SAML request state during redirect flow (auto-cleaned)';

-- Cleanup function for expired SAML requests
CREATE OR REPLACE FUNCTION cleanup_expired_saml_requests()
RETURNS trigger AS $$
BEGIN
    DELETE FROM pending_saml_authentication WHERE created_at < NOW() - INTERVAL '10 minutes';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_cleanup_saml_requests
    AFTER INSERT ON pending_saml_authentication
    EXECUTE FUNCTION cleanup_expired_saml_requests();

-- ============================================================================
-- EXTEND LOGIN ATTEMPTS FOR SSO AUDIT
-- ============================================================================

-- Add provider tracking to login_attempts (sso_providers must exist first)
ALTER TABLE login_attempts
ADD COLUMN provider_id UUID REFERENCES sso_providers(id),
ADD COLUMN provider_type VARCHAR(50) DEFAULT 'local';

CREATE INDEX idx_login_attempts_provider_type ON login_attempts(provider_type);

COMMENT ON COLUMN login_attempts.provider_type IS 'Authentication method: local, ldap, saml, oidc, oauth2';

-- Update existing rows to have provider_type = 'local'
UPDATE login_attempts SET provider_type = 'local' WHERE provider_type IS NULL;
