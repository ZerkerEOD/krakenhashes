/**
 * WebAuthn utilities for passkey authentication
 */

// Check if WebAuthn is supported in the current browser
export const isWebAuthnSupported = (): boolean => {
  return !!(
    window.PublicKeyCredential &&
    typeof window.PublicKeyCredential === 'function'
  );
};

// Check if platform authenticator (e.g., Touch ID, Windows Hello) is available
export const isPlatformAuthenticatorAvailable = async (): Promise<boolean> => {
  if (!isWebAuthnSupported()) {
    return false;
  }
  try {
    return await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable();
  } catch {
    return false;
  }
};

// Convert base64url to ArrayBuffer
export const base64urlToArrayBuffer = (base64url: string): ArrayBuffer => {
  // Replace base64url characters with base64 characters
  const base64 = base64url
    .replace(/-/g, '+')
    .replace(/_/g, '/');
  // Pad with '=' to make length a multiple of 4
  const paddedBase64 = base64 + '='.repeat((4 - (base64.length % 4)) % 4);
  const binary = atob(paddedBase64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
};

// Convert ArrayBuffer to base64url
export const arrayBufferToBase64url = (buffer: ArrayBuffer): string => {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  const base64 = btoa(binary);
  // Convert to base64url
  return base64
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
};

// Types for WebAuthn responses from the server
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

// Convert server options to WebAuthn API format for registration
export const prepareRegistrationOptions = (
  options: PublicKeyCredentialCreationOptionsJSON
): CredentialCreationOptions => {
  const publicKey = options.publicKey;

  return {
    publicKey: {
      rp: publicKey.rp,
      user: {
        id: base64urlToArrayBuffer(publicKey.user.id),
        name: publicKey.user.name,
        displayName: publicKey.user.displayName,
      },
      challenge: base64urlToArrayBuffer(publicKey.challenge),
      pubKeyCredParams: publicKey.pubKeyCredParams,
      timeout: publicKey.timeout,
      excludeCredentials: publicKey.excludeCredentials?.map((cred) => ({
        type: cred.type,
        id: base64urlToArrayBuffer(cred.id),
        transports: cred.transports,
      })),
      authenticatorSelection: publicKey.authenticatorSelection,
      attestation: publicKey.attestation,
    },
  };
};

// Convert server options to WebAuthn API format for authentication
export const prepareAuthenticationOptions = (
  options: PublicKeyCredentialRequestOptionsJSON
): CredentialRequestOptions => {
  const publicKey = options.publicKey;

  return {
    publicKey: {
      challenge: base64urlToArrayBuffer(publicKey.challenge),
      timeout: publicKey.timeout,
      rpId: publicKey.rpId,
      allowCredentials: publicKey.allowCredentials?.map((cred) => ({
        type: cred.type,
        id: base64urlToArrayBuffer(cred.id),
        transports: cred.transports,
      })),
      userVerification: publicKey.userVerification,
    },
  };
};

// Convert registration credential to JSON for server
export const serializeRegistrationCredential = (
  credential: PublicKeyCredential
): object => {
  const response = credential.response as AuthenticatorAttestationResponse;

  return {
    id: credential.id,
    rawId: arrayBufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: arrayBufferToBase64url(response.clientDataJSON),
      attestationObject: arrayBufferToBase64url(response.attestationObject),
      transports: response.getTransports?.() || [],
    },
  };
};

// Convert authentication credential to JSON for server
export const serializeAuthenticationCredential = (
  credential: PublicKeyCredential
): object => {
  const response = credential.response as AuthenticatorAssertionResponse;

  return {
    id: credential.id,
    rawId: arrayBufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: arrayBufferToBase64url(response.clientDataJSON),
      authenticatorData: arrayBufferToBase64url(response.authenticatorData),
      signature: arrayBufferToBase64url(response.signature),
      userHandle: response.userHandle
        ? arrayBufferToBase64url(response.userHandle)
        : null,
    },
  };
};

// Create a new passkey (registration)
export const createPasskey = async (
  options: PublicKeyCredentialCreationOptionsJSON
): Promise<object> => {
  if (!isWebAuthnSupported()) {
    throw new Error('WebAuthn is not supported in this browser');
  }

  const preparedOptions = prepareRegistrationOptions(options);

  const credential = await navigator.credentials.create(preparedOptions);

  if (!credential || !(credential instanceof PublicKeyCredential)) {
    throw new Error('Failed to create credential');
  }

  return serializeRegistrationCredential(credential);
};

// Authenticate with a passkey
export const authenticateWithPasskey = async (
  options: PublicKeyCredentialRequestOptionsJSON
): Promise<object> => {
  if (!isWebAuthnSupported()) {
    throw new Error('WebAuthn is not supported in this browser');
  }

  const preparedOptions = prepareAuthenticationOptions(options);

  const credential = await navigator.credentials.get(preparedOptions);

  if (!credential || !(credential instanceof PublicKeyCredential)) {
    throw new Error('Failed to get credential');
  }

  return serializeAuthenticationCredential(credential);
};

// Get human-readable error message for WebAuthn errors
export const getWebAuthnErrorMessage = (error: unknown): string => {
  if (error instanceof DOMException) {
    switch (error.name) {
      case 'NotAllowedError':
        return 'The operation was cancelled or timed out. Please try again.';
      case 'SecurityError':
        return 'The operation is not allowed in the current security context.';
      case 'NotSupportedError':
        return 'This authenticator is not supported.';
      case 'InvalidStateError':
        return 'The authenticator is already registered with this account.';
      case 'AbortError':
        return 'The operation was aborted.';
      case 'ConstraintError':
        return 'The authenticator does not meet the required constraints.';
      case 'UnknownError':
        return 'An unknown error occurred with the authenticator.';
      default:
        return error.message || 'An error occurred during authentication.';
    }
  }

  if (error instanceof Error) {
    return error.message;
  }

  return 'An unexpected error occurred.';
};
