export interface User {
  id: string;
  username: string;
  email: string;
  role: string;
  firstName?: string;
  lastName?: string;
  createdAt: string;
  updatedAt: string;
  
  // Account status
  accountEnabled: boolean;
  accountLocked: boolean;
  accountLockedUntil?: string;
  
  // MFA settings
  mfaEnabled: boolean;
  mfaType: string[];
  preferredMFAMethod?: string;
  
  // Login information
  lastLogin?: string;
  lastPasswordChange?: string;
  failedLoginAttempts: number;
  lastFailedAttempt?: string;
  
  // Disable information
  disabledReason?: string;
  disabledAt?: string;
  disabledBy?: string;
  
  // Teams (if applicable)
  teams?: Team[];

  // API Key status
  hasApiKey?: boolean;
  apiKeyCreatedAt?: string;
  apiKeyLastUsed?: string;
}

export interface Team {
  id: string;
  name: string;
  description?: string;
  createdAt: string;
  updatedAt: string;
}

export interface UserUpdateRequest {
  username?: string;
  email?: string;
  role?: string;
}

export interface DisableUserRequest {
  reason: string;
}

export interface ResetPasswordRequest {
  password?: string;
  temporary?: boolean;
}

export interface UserListResponse {
  data: User[];
}

export interface UserDetailResponse {
  data: User;
}

export interface ProfileUpdate {
  email?: string;
  currentPassword?: string;
  newPassword?: string;
}

export interface NotificationPreferences {
  notifyOnJobCompletion: boolean;
  emailConfigured: boolean;
}

export interface LoginAttempt {
  id: string;
  userId?: string;
  username: string;
  ipAddress: string;
  userAgent: string;
  success: boolean;
  failureReason?: string;
  attemptedAt: string;
  notified: boolean;
}

export interface ActiveSession {
  id: string;
  userId: string;
  ipAddress: string;
  userAgent: string;
  createdAt: string;
  lastActiveAt: string;
}

export interface LoginAttemptsResponse {
  data: LoginAttempt[];
}

export interface ActiveSessionsResponse {
  data: ActiveSession[];
}

export interface TerminateSessionResponse {
  data: {
    message: string;
  };
}

export interface TerminateAllSessionsResponse {
  data: {
    message: string;
    count: number;
  };
}

// API Key Management
export interface ApiKeyInfo {
  hasKey: boolean;
  createdAt?: string;
  lastUsed?: string;
}

export interface ApiKeyGenerateResponse {
  apiKey: string;
  createdAt: string;
}

export interface ApiKeyGenerateRequest {
  // No fields needed - backend generates the key
}

export interface ApiKeyInfoResponse {
  data: ApiKeyInfo;
}

export interface ApiKeyResponse {
  data: ApiKeyGenerateResponse;
}