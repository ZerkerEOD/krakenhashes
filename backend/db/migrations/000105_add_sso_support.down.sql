-- Rollback: Remove SSO (LDAP, SAML, OAuth/OIDC) Support

-- Remove login_attempts extensions
DROP INDEX IF EXISTS idx_login_attempts_provider_type;
ALTER TABLE login_attempts
DROP COLUMN IF EXISTS provider_id,
DROP COLUMN IF EXISTS provider_type;

-- Drop pending authentication tables
DROP TRIGGER IF EXISTS trigger_cleanup_saml_requests ON pending_saml_authentication;
DROP FUNCTION IF EXISTS cleanup_expired_saml_requests();
DROP TABLE IF EXISTS pending_saml_authentication;

DROP TRIGGER IF EXISTS trigger_cleanup_oauth_states ON pending_oauth_authentication;
DROP FUNCTION IF EXISTS cleanup_expired_oauth_states();
DROP TABLE IF EXISTS pending_oauth_authentication;

-- Drop user identities
DROP TRIGGER IF EXISTS update_user_identities_updated_at ON user_identities;
DROP TABLE IF EXISTS user_identities;

-- Drop provider config tables
DROP TABLE IF EXISTS oauth_configs;
DROP TABLE IF EXISTS saml_configs;
DROP TABLE IF EXISTS ldap_configs;

-- Drop SSO providers
DROP TRIGGER IF EXISTS update_sso_providers_updated_at ON sso_providers;
DROP TABLE IF EXISTS sso_providers;

-- Remove user auth override columns
ALTER TABLE users
DROP COLUMN IF EXISTS local_auth_override,
DROP COLUMN IF EXISTS sso_auth_override,
DROP COLUMN IF EXISTS auth_override_notes;

-- Remove auth_settings SSO columns
ALTER TABLE auth_settings
DROP COLUMN IF EXISTS local_auth_enabled,
DROP COLUMN IF EXISTS ldap_auth_enabled,
DROP COLUMN IF EXISTS saml_auth_enabled,
DROP COLUMN IF EXISTS oauth_auth_enabled,
DROP COLUMN IF EXISTS sso_auto_create_users,
DROP COLUMN IF EXISTS sso_auto_enable_users;
