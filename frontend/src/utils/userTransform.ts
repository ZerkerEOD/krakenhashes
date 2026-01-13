import { User, LoginAttempt, ActiveSession } from '../types/user';

// Transform snake_case backend response to camelCase frontend type
export const transformUserResponse = (backendUser: any): User => {
  return {
    id: backendUser.id,
    username: backendUser.username,
    email: backendUser.email,
    role: backendUser.role,
    firstName: backendUser.first_name,
    lastName: backendUser.last_name,
    createdAt: backendUser.created_at,
    updatedAt: backendUser.updated_at,
    
    // Account status - these are the fields causing the issue
    accountEnabled: backendUser.account_enabled,
    accountLocked: backendUser.account_locked,
    accountLockedUntil: backendUser.account_locked_until,
    
    // MFA settings
    mfaEnabled: backendUser.mfa_enabled,
    mfaType: backendUser.mfa_type || [],
    preferredMFAMethod: backendUser.preferred_mfa_method,
    
    // Login information
    lastLogin: backendUser.last_login,
    lastPasswordChange: backendUser.last_password_change,
    failedLoginAttempts: backendUser.failed_login_attempts || 0,
    lastFailedAttempt: backendUser.last_failed_attempt,
    
    // Disable information
    disabledReason: backendUser.disabled_reason,
    disabledAt: backendUser.disabled_at,
    disabledBy: backendUser.disabled_by,
    
    // Teams (if applicable)
    teams: backendUser.teams || []
  };
};

export const transformUserListResponse = (response: any): User[] => {
  if (!response.data || !Array.isArray(response.data)) {
    return [];
  }
  return response.data.map(transformUserResponse);
};

// Transform login attempt - pass through snake_case fields to match API response
export const transformLoginAttempt = (backendAttempt: any): LoginAttempt => {
  return {
    id: backendAttempt.id,
    user_id: backendAttempt.user_id,
    username: backendAttempt.username,
    ip_address: backendAttempt.ip_address,
    user_agent: backendAttempt.user_agent,
    success: backendAttempt.success,
    failure_reason: backendAttempt.failure_reason,
    attempted_at: backendAttempt.attempted_at,
    notified: backendAttempt.notified,
    // SSO provider info
    provider_id: backendAttempt.provider_id,
    provider_type: backendAttempt.provider_type,
  };
};

// Transform active session from snake_case to camelCase
export const transformActiveSession = (backendSession: any): ActiveSession => {
  return {
    id: backendSession.id,
    userId: backendSession.user_id,
    ipAddress: backendSession.ip_address,
    userAgent: backendSession.user_agent,
    createdAt: backendSession.created_at,
    lastActiveAt: backendSession.last_active_at,
  };
};