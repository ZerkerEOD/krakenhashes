// SSO Provider types
export type SSOProviderType = 'ldap' | 'saml' | 'oidc' | 'oauth2';

export interface SSOProvider {
  id: string;
  name: string;
  provider_type: SSOProviderType;
  enabled: boolean;
  display_order: number;
  auto_create_users?: boolean;
  auto_enable_users?: boolean;
  created_at: string;
  updated_at: string;
}

export interface SSOProviderDisplay {
  id: string;
  name: string;
  provider_type: SSOProviderType;
  display_order: number;
}

// SSO Settings
export interface SSOSettings {
  local_auth_enabled: boolean;
  ldap_auth_enabled: boolean;
  saml_auth_enabled: boolean;
  oauth_auth_enabled: boolean;
  sso_auto_create_users: boolean;
  sso_auto_enable_users: boolean;
}

export interface SSOSettingsUpdate {
  local_auth_enabled?: boolean;
  ldap_auth_enabled?: boolean;
  saml_auth_enabled?: boolean;
  oauth_auth_enabled?: boolean;
  sso_auto_create_users?: boolean;
  sso_auto_enable_users?: boolean;
}

// LDAP Configuration
export interface LDAPConfig {
  server_url: string;
  base_dn: string;
  user_search_filter: string;
  bind_dn?: string;
  bind_password?: string;
  use_start_tls: boolean;
  skip_cert_verify: boolean;
  ca_certificate?: string;
  email_attribute: string;
  display_name_attribute?: string;
  username_attribute?: string;
  connection_timeout_seconds: number;
}

// SAML Configuration
export interface SAMLConfig {
  sp_entity_id: string;
  idp_entity_id: string;
  idp_sso_url: string;
  idp_slo_url?: string;
  idp_certificate: string;
  sp_private_key?: string;
  sp_certificate?: string;
  sign_requests: boolean;
  require_signed_assertions: boolean;
  require_encrypted_assertions: boolean;
  name_id_format?: string;
  email_attribute: string;
  username_attribute?: string;
  display_name_attribute?: string;
}

// OAuth/OIDC Configuration
export interface OAuthConfig {
  is_oidc: boolean;
  client_id: string;
  client_secret?: string;
  discovery_url?: string;
  authorization_url?: string;
  token_url?: string;
  userinfo_url?: string;
  jwks_url?: string;
  scopes: string[];
  email_attribute: string;
  username_attribute?: string;
  display_name_attribute?: string;
  external_id_attribute: string;
}

// Combined provider with config
export interface SSOProviderWithConfig extends SSOProvider {
  ldap_config?: LDAPConfig;
  saml_config?: SAMLConfig;
  oauth_config?: OAuthConfig;
}

// Create/Update provider requests
export interface CreateSSOProviderRequest {
  name: string;
  provider_type: SSOProviderType;
  enabled: boolean;
  display_order?: number;
  auto_create_users?: boolean;
  auto_enable_users?: boolean;
  ldap_config?: LDAPConfig;
  saml_config?: SAMLConfig;
  oauth_config?: OAuthConfig;
}

export interface UpdateSSOProviderRequest {
  name?: string;
  enabled?: boolean;
  display_order?: number;
  auto_create_users?: boolean;
  auto_enable_users?: boolean;
  ldap_config?: LDAPConfig;
  saml_config?: SAMLConfig;
  oauth_config?: OAuthConfig;
}

// User Identity (linked SSO accounts)
export interface UserIdentity {
  id: string;
  user_id: string;
  provider_id: string;
  provider_name?: string;
  provider_type: SSOProviderType;
  external_id: string;
  external_email?: string;
  external_username?: string;
  external_display_name?: string;
  last_login_at?: string;
  created_at: string;
  updated_at: string;
}

// LDAP Login Request
export interface LDAPLoginRequest {
  username: string;
  password: string;
}

// SSO Login Response (from LDAP or callback)
export interface SSOLoginResponse {
  success: boolean;
  token?: string;
  message?: string;
  mfa_required?: boolean;
  session_token?: string;
  mfa_type?: string[];
  preferred_method?: string;
  expires_at?: string;
  // For new user pending approval
  pending_approval?: boolean;
  user_created?: boolean;
}

// Provider test result
export interface ProviderTestResult {
  success: boolean;
  message: string;
  details?: string;
}

// Get enabled providers response (for login page)
export interface EnabledProvidersResponse {
  local_auth_enabled: boolean;
  providers: SSOProviderDisplay[];
}
