export interface AuthState {
  isAuth: boolean;
  authChecked: boolean;
}

export interface LoginResponse {
  success?: boolean;
  message?: string;
  token?: string;
  mfa_required?: boolean;
  session_token?: string;
  mfa_type?: string[];
  preferred_method?: string;
  expires_at?: string;
}

export interface AuthCheckResponse {
  authenticated: boolean;
  role?: string;
}

export interface User {
  id: string;
  username: string;
  email: string;
  role?: string;
  mfaEnabled: boolean;
  mfaType?: 'email' | 'authenticator' | 'passkey';
  createdAt?: string;
  updatedAt?: string;
}

export interface LoginCredentials {
  username: string;
  password: string;
}

export interface LoginProps {
  setIsAuth: (isAuth: boolean) => void;
  onSuccess?: () => void;
  onError?: (error: Error) => void;
}

export interface AuthSettingsResponse {
  id: string;
  min_password_length: number;
  require_uppercase: boolean;
  require_lowercase: boolean;
  require_numbers: boolean;
  require_special_chars: boolean;
  max_failed_attempts: number;
  lockout_duration_minutes: number;
  require_mfa: boolean;
  jwt_expiry_minutes: number;
  display_timezone: string;
  notification_aggregation_minutes: number;
}

export interface AuthSettings {
  id: string;
  minPasswordLength: number;
  requireUppercase: boolean;
  requireLowercase: boolean;
  requireNumbers: boolean;
  requireSpecialChars: boolean;
  maxFailedAttempts: number;
  lockoutDurationMinutes: number;
  requireMFA: boolean;
  jwtExpiryMinutes: number;
  displayTimezone: string;
  notificationAggregationMinutes: number;
}

export interface MFASettings {
  requireMfa: boolean;
  allowedMfaMethods: string[];
  emailCodeValidity: number | '';
  backupCodesCount: number | '';
  mfaCodeCooldownMinutes: number | '';
  mfaCodeExpiryMinutes: number | '';
  mfaMaxAttempts: number | '';
  mfaEnabled: boolean;
  mfaType?: string[];
  remainingBackupCodes?: number;
  preferredMethod?: string;
}

export type MFAMethod = 'email' | 'authenticator' | 'passkey';

export interface PasswordPolicy {
  minPasswordLength: number | '';
  requireUppercase: boolean;
  requireLowercase: boolean;
  requireNumbers: boolean;
  requireSpecialChars: boolean;
}

export interface AccountSecurity {
  maxFailedAttempts: number | '';
  lockoutDuration: number | '';
  jwtExpiryMinutes: number | '';
  notificationAggregationMinutes: number | '';
  tokenCleanupIntervalSeconds?: number | '';
  maxConcurrentSessions?: number | '';
  sessionAbsoluteTimeoutHours?: number | '';
}

export interface AuthSettingsUpdate {
  minPasswordLength: number;
  requireUppercase: boolean;
  requireLowercase: boolean;
  requireNumbers: boolean;
  requireSpecialChars: boolean;
  maxFailedAttempts: number;
  lockoutDuration: number;
  jwtExpiryMinutes: number;
  notificationAggregationMinutes: number;
  tokenCleanupIntervalSeconds?: number;
  maxConcurrentSessions?: number;
  sessionAbsoluteTimeoutHours?: number;
}

export interface MFAVerifyRequest {
  sessionToken: string;
  code: string;
  method: string;
}

export interface MFAVerifyResponse {
  success: boolean;
  token: string;
  message?: string;
  remainingAttempts: number;
}

// Passkey/WebAuthn types
export interface Passkey {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

export interface PasskeyRegistrationBeginResponse {
  options: PublicKeyCredentialCreationOptionsJSON;
}

export interface PasskeyRegistrationFinishResponse {
  success: boolean;
  passkey: Passkey;
}

export interface PasskeyAuthenticationBeginResponse {
  options: PublicKeyCredentialRequestOptionsJSON;
}

export interface PasskeyAuthenticationFinishResponse {
  success: boolean;
  token: string;
}

export interface PasskeyListResponse {
  passkeys: Passkey[];
}

export interface WebAuthnSettings {
  rpId: string;
  rpOrigins: string[];
  rpDisplayName: string;
  configured: boolean;
}

// JSON representations from server
export interface PublicKeyCredentialCreationOptionsJSON {
  publicKey: {
    rp: {
      name: string;
      id?: string;
    };
    user: {
      id: string;
      name: string;
      displayName: string;
    };
    challenge: string;
    pubKeyCredParams: Array<{
      type: 'public-key';
      alg: number;
    }>;
    timeout?: number;
    excludeCredentials?: Array<{
      type: 'public-key';
      id: string;
      transports?: AuthenticatorTransport[];
    }>;
    authenticatorSelection?: {
      authenticatorAttachment?: AuthenticatorAttachment;
      residentKey?: ResidentKeyRequirement;
      userVerification?: UserVerificationRequirement;
    };
    attestation?: AttestationConveyancePreference;
  };
}

export interface PublicKeyCredentialRequestOptionsJSON {
  publicKey: {
    challenge: string;
    timeout?: number;
    rpId?: string;
    allowCredentials?: Array<{
      type: 'public-key';
      id: string;
      transports?: AuthenticatorTransport[];
    }>;
    userVerification?: UserVerificationRequirement;
  };
}