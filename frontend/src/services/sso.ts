import { api } from './api';
import {
  SSOSettings,
  SSOSettingsUpdate,
  SSOProvider,
  SSOProviderWithConfig,
  CreateSSOProviderRequest,
  UpdateSSOProviderRequest,
  UserIdentity,
  LDAPLoginRequest,
  SSOLoginResponse,
  ProviderTestResult,
  EnabledProvidersResponse,
  SSOProviderDisplay,
  LDAPConfig,
  SAMLConfig,
  OAuthConfig
} from '../types/sso';

// ============================================================================
// Public SSO API (for login page)
// ============================================================================

/**
 * Get list of enabled SSO providers for the login page
 */
export const getEnabledProviders = async (): Promise<EnabledProvidersResponse> => {
  try {
    const response = await api.get<EnabledProvidersResponse>('/api/auth/providers');
    return response.data;
  } catch (error: any) {
    console.error('Get Enabled Providers Error:', error.response?.data);
    // Return default with local auth enabled if API fails
    return { local_auth_enabled: true, providers: [] };
  }
};

/**
 * Login via LDAP provider
 */
export const ldapLogin = async (
  providerId: string,
  credentials: LDAPLoginRequest
): Promise<SSOLoginResponse> => {
  try {
    const response = await api.post<SSOLoginResponse>(
      `/api/auth/ldap/${providerId}`,
      credentials
    );
    return response.data;
  } catch (error: any) {
    console.error('LDAP Login Error:', error.response?.data);
    return {
      success: false,
      message: error.response?.data?.message || 'LDAP authentication failed'
    };
  }
};

/**
 * Start OAuth/OIDC flow - redirects to IdP
 */
export const startOAuthFlow = (providerId: string): void => {
  // Redirect browser to OAuth start endpoint
  window.location.href = `${api.defaults.baseURL}/api/auth/oauth/${providerId}/start`;
};

/**
 * Start SAML flow - redirects to IdP
 */
export const startSAMLFlow = (providerId: string): void => {
  // Redirect browser to SAML start endpoint
  window.location.href = `${api.defaults.baseURL}/api/auth/saml/${providerId}/start`;
};

/**
 * Get SAML metadata URL for a provider
 */
export const getSAMLMetadataURL = (providerId: string): string => {
  return `${api.defaults.baseURL}/api/auth/saml/${providerId}/metadata`;
};

// ============================================================================
// Admin SSO API
// ============================================================================

/**
 * Get SSO settings (admin)
 */
export const getSSOSettings = async (): Promise<SSOSettings> => {
  try {
    const response = await api.get<SSOSettings>('/api/admin/sso/settings');
    return response.data;
  } catch (error: any) {
    console.error('Get SSO Settings Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to get SSO settings');
  }
};

/**
 * Update SSO settings (admin)
 */
export const updateSSOSettings = async (settings: SSOSettingsUpdate): Promise<void> => {
  try {
    await api.put('/api/admin/sso/settings', settings);
  } catch (error: any) {
    console.error('Update SSO Settings Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to update SSO settings');
  }
};

/**
 * List all SSO providers (admin)
 */
export const listSSOProviders = async (): Promise<SSOProvider[]> => {
  try {
    const response = await api.get<{ providers: SSOProvider[] }>('/api/admin/sso/providers');
    return response.data.providers || [];
  } catch (error: any) {
    console.error('List SSO Providers Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to list SSO providers');
  }
};

/**
 * Get a single SSO provider with config (admin)
 */
export const getSSOProvider = async (providerId: string): Promise<SSOProviderWithConfig> => {
  try {
    // Backend returns nested structure: { provider: {...}, oauth_config?: {...}, ... }
    // We need to flatten it for the frontend
    const response = await api.get<{
      provider: SSOProvider;
      ldap_config?: LDAPConfig;
      saml_config?: SAMLConfig;
      oauth_config?: OAuthConfig;
    }>(`/api/admin/sso/providers/${providerId}`);

    return {
      ...response.data.provider,
      ldap_config: response.data.ldap_config,
      saml_config: response.data.saml_config,
      oauth_config: response.data.oauth_config,
    };
  } catch (error: any) {
    console.error('Get SSO Provider Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to get SSO provider');
  }
};

/**
 * Create a new SSO provider (admin)
 */
export const createSSOProvider = async (
  data: CreateSSOProviderRequest
): Promise<SSOProvider> => {
  try {
    const response = await api.post<SSOProvider>('/api/admin/sso/providers', data);
    return response.data;
  } catch (error: any) {
    console.error('Create SSO Provider Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to create SSO provider');
  }
};

/**
 * Update an SSO provider (admin)
 */
export const updateSSOProvider = async (
  providerId: string,
  data: UpdateSSOProviderRequest
): Promise<SSOProvider> => {
  try {
    const response = await api.put<SSOProvider>(`/api/admin/sso/providers/${providerId}`, data);
    return response.data;
  } catch (error: any) {
    console.error('Update SSO Provider Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to update SSO provider');
  }
};

/**
 * Delete an SSO provider (admin)
 */
export const deleteSSOProvider = async (providerId: string): Promise<void> => {
  try {
    await api.delete(`/api/admin/sso/providers/${providerId}`);
  } catch (error: any) {
    console.error('Delete SSO Provider Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to delete SSO provider');
  }
};

/**
 * Test an SSO provider connection (admin)
 */
export const testSSOProvider = async (providerId: string): Promise<ProviderTestResult> => {
  try {
    const response = await api.post<ProviderTestResult>(
      `/api/admin/sso/providers/${providerId}/test`
    );
    return response.data;
  } catch (error: any) {
    console.error('Test SSO Provider Error:', error.response?.data);
    return {
      success: false,
      message: error.response?.data?.message || 'Connection test failed'
    };
  }
};

/**
 * Get user's SSO identities (admin)
 */
export const getUserIdentities = async (userId: string): Promise<UserIdentity[]> => {
  try {
    const response = await api.get<{ identities: UserIdentity[] }>(
      `/api/admin/sso/users/${userId}/identities`
    );
    return response.data.identities || [];
  } catch (error: any) {
    console.error('Get User Identities Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to get user identities');
  }
};

/**
 * Unlink an SSO identity from a user (admin)
 */
export const unlinkIdentity = async (identityId: string): Promise<void> => {
  try {
    await api.delete(`/api/admin/sso/identities/${identityId}`);
  } catch (error: any) {
    console.error('Unlink Identity Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to unlink identity');
  }
};

// ============================================================================
// User SSO API (for linked accounts in settings)
// ============================================================================

/**
 * Get current user's linked SSO identities
 */
export const getMyIdentities = async (): Promise<UserIdentity[]> => {
  try {
    const response = await api.get<{ identities: UserIdentity[] }>('/api/user/sso/identities');
    return response.data.identities || [];
  } catch (error: any) {
    console.error('Get My Identities Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to get linked accounts');
  }
};

/**
 * Unlink an SSO identity from current user
 */
export const unlinkMyIdentity = async (identityId: string): Promise<void> => {
  try {
    await api.delete(`/api/user/sso/identities/${identityId}`);
  } catch (error: any) {
    console.error('Unlink My Identity Error:', error.response?.data);
    throw new Error(error.response?.data?.message || 'Failed to unlink account');
  }
};

// ============================================================================
// Helper functions
// ============================================================================

/**
 * Get human-readable provider type label
 */
export const getProviderTypeLabel = (type: string): string => {
  switch (type) {
    case 'ldap':
      return 'LDAP / Active Directory';
    case 'saml':
      return 'SAML 2.0';
    case 'oidc':
      return 'OpenID Connect';
    case 'oauth2':
      return 'OAuth 2.0';
    default:
      return type.toUpperCase();
  }
};

/**
 * Get icon name for provider type (for MUI icons)
 */
export const getProviderTypeIcon = (type: string): string => {
  switch (type) {
    case 'ldap':
      return 'AccountTree'; // or 'Business'
    case 'saml':
      return 'Security';
    case 'oidc':
    case 'oauth2':
      return 'Key';
    default:
      return 'Login';
  }
};

/**
 * Check if a provider type uses redirect flow
 */
export const isRedirectProvider = (type: string): boolean => {
  return type === 'saml' || type === 'oidc' || type === 'oauth2';
};

/**
 * Start SSO login flow for a provider
 */
export const startSSOLogin = async (provider: SSOProviderDisplay): Promise<void> => {
  if (provider.provider_type === 'ldap') {
    // LDAP requires username/password, handled by form
    throw new Error('LDAP login requires credentials');
  } else if (provider.provider_type === 'saml') {
    startSAMLFlow(provider.id);
  } else {
    startOAuthFlow(provider.id);
  }
};
