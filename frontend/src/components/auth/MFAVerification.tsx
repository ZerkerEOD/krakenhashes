import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Button,
  TextField,
  Typography,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  CircularProgress,
  Card,
  CardContent,
  Alert,
} from '@mui/material';
import KeyIcon from '@mui/icons-material/Key';
import { verifyMFA, beginPasskeyAuthentication, finishPasskeyAuthentication } from '../../services/auth';
import {
  isWebAuthnSupported,
  authenticateWithPasskey,
  getWebAuthnErrorMessage
} from '../../utils/webauthn';

interface MFAVerificationProps {
  sessionToken: string;
  mfaType: string[];  // Changed to string[] to match backend type
  preferredMethod: string;
  onSuccess: (token: string) => void;
  onError: (error: string) => void;
  expiresAt?: string;
}

const MFAVerification: React.FC<MFAVerificationProps> = ({
  sessionToken,
  mfaType,
  preferredMethod,
  onSuccess,
  onError,
  expiresAt,
}) => {
  const [code, setCode] = useState('');
  const [method, setMethod] = useState(preferredMethod);
  const [loading, setLoading] = useState(false);
  const [remainingAttempts, setRemainingAttempts] = useState<number>(3);
  const [passkeyError, setPasskeyError] = useState<string | null>(null);

  // Check if WebAuthn is supported for passkey method
  const webAuthnSupported = isWebAuthnSupported();

  // Get available methods including backup and passkey if they exist in mfaType
  const getAvailableMethods = useCallback(() => {
    // Filter out any invalid or unavailable methods
    const validMethods = mfaType.filter(m => {
      // Always include authenticator and backup if present
      if (m === 'authenticator' || m === 'backup') return true;
      // Only include email if it's in the mfaType (backend should have already filtered based on provider)
      if (m === 'email') return true;
      // Include passkey if WebAuthn is supported
      if (m === 'passkey') return webAuthnSupported;
      // Filter out any unknown methods
      return false;
    });

    // Separate backup from other methods for proper ordering
    const nonBackupMethods = validMethods.filter(m => m !== 'backup');
    const hasBackup = validMethods.includes('backup');

    return nonBackupMethods.concat(hasBackup ? ['backup'] : []);
  }, [mfaType, webAuthnSupported]);

  // Handle passkey authentication
  const handlePasskeyAuth = async () => {
    setLoading(true);
    setPasskeyError(null);

    try {
      // Begin authentication to get WebAuthn options from server
      const options = await beginPasskeyAuthentication(sessionToken);

      // Create the credential using WebAuthn API
      const credential = await authenticateWithPasskey(options);

      // Finish authentication by sending credential to server
      const response = await finishPasskeyAuthentication(sessionToken, credential);

      if (response.success) {
        onSuccess(response.token);
      } else {
        setRemainingAttempts(response.remainingAttempts ?? remainingAttempts - 1);
        const errorMessage = response.message || 'Passkey authentication failed';
        setPasskeyError(errorMessage);
        onError(errorMessage);
      }
    } catch (error) {
      const errorMessage = getWebAuthnErrorMessage(error);
      setPasskeyError(errorMessage);
      onError(errorMessage);
    } finally {
      setLoading(false);
    }
  };

  const handleMethodChange = async (newMethod: string) => {
    setCode(''); // Clear code when changing methods
    setPasskeyError(null); // Clear passkey error
    setMethod(newMethod);

    // Auto-start passkey authentication when selecting passkey
    if (newMethod === 'passkey') {
      // Don't auto-start, let user click the button
      return;
    }

    // Request email code when switching to email method
    if (newMethod === 'email') {
      try {
        setLoading(true);
        const response = await verifyMFA(sessionToken, '', 'request_email');
        if (!response.success) {
          onError(response.message || 'Failed to send email code');
        }
      } catch (error) {
        onError(error instanceof Error ? error.message : 'Failed to send email code');
      } finally {
        setLoading(false);
      }
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // For passkey, use the passkey flow
    if (method === 'passkey') {
      await handlePasskeyAuth();
      return;
    }

    setLoading(true);

    try {
      const response = await verifyMFA(sessionToken, code, method);
      if (response.success) {
        onSuccess(response.token);
      } else {
        setRemainingAttempts(response.remainingAttempts ?? remainingAttempts - 1);
        onError(response.message || `Invalid code. ${response.remainingAttempts} attempts remaining.`);
        setCode(''); // Clear code on failed attempt
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Verification failed';
      onError(message);
      if (message.includes('No backup codes available')) {
        // Remove backup from available methods if no codes are available
        const updatedMethods = getAvailableMethods().filter(m => m !== 'backup');
        if (method === 'backup') {
          setMethod(preferredMethod); // Switch back to preferred method
        }
        setAvailableMethods(updatedMethods);
      }
    } finally {
      setLoading(false);
    }
  };

  // State for available methods
  const [availableMethods, setAvailableMethods] = useState<string[]>(getAvailableMethods());

  // Update available methods when mfaType changes
  useEffect(() => {
    setAvailableMethods(getAvailableMethods());
  }, [mfaType, getAvailableMethods]);

  // Request email code on initial load if email is the selected method
  useEffect(() => {
    // Only request email code on mount if email is selected but NOT the preferred method
    // This prevents duplicate emails when email is preferred (since backend already sent it)
    if (method === 'email' && method !== preferredMethod && !loading) {
      handleMethodChange('email');
    }
  }, []); // Only run on initial mount

  const getMethodInstructions = () => {
    switch (method) {
      case 'email':
        return (
          <Alert severity="info" sx={{ mb: 2 }}>
            Please enter the verification code sent to your email.
            {expiresAt && (
              <Typography variant="body2" sx={{ mt: 1 }}>
                Code expires at: {new Date(expiresAt).toLocaleTimeString()}
              </Typography>
            )}
          </Alert>
        );
      case 'authenticator':
        return (
          <Alert severity="info" sx={{ mb: 2 }}>
            Please enter the code from your authenticator app.
          </Alert>
        );
      case 'backup':
        return (
          <Alert severity="warning" sx={{ mb: 2 }}>
            Please enter one of your backup codes. Note that each backup code can only be used once.
          </Alert>
        );
      case 'passkey':
        return (
          <Alert severity="info" sx={{ mb: 2 }}>
            Use your passkey (security key, fingerprint, or device) to authenticate.
            {!webAuthnSupported && (
              <Typography variant="body2" color="error" sx={{ mt: 1 }}>
                WebAuthn is not supported in this browser. Please use a different method.
              </Typography>
            )}
          </Alert>
        );
      default:
        return null;
    }
  };

  const getMethodDisplayName = (m: string) => {
    switch (m) {
      case 'email':
        return 'Email Code';
      case 'authenticator':
        return 'Authenticator App';
      case 'backup':
        return 'Backup Code';
      case 'passkey':
        return 'Passkey';
      default:
        return m;
    }
  };

  // Render passkey-specific UI
  const renderPasskeyUI = () => (
    <Box sx={{ textAlign: 'center', mt: 2 }}>
      {passkeyError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {passkeyError}
        </Alert>
      )}
      <Button
        variant="contained"
        size="large"
        startIcon={<KeyIcon />}
        onClick={handlePasskeyAuth}
        disabled={loading || !webAuthnSupported}
        sx={{ mt: 2, mb: 2, py: 1.5, px: 4 }}
      >
        {loading ? <CircularProgress size={24} /> : 'Authenticate with Passkey'}
      </Button>
      <Typography variant="body2" color="text.secondary">
        Your browser will prompt you to use your passkey.
      </Typography>
    </Box>
  );

  // Render code input UI
  const renderCodeUI = () => (
    <>
      <TextField
        margin="normal"
        required
        fullWidth
        label={method === 'backup' ? 'Backup Code' : 'Verification Code'}
        value={code}
        onChange={(e) => setCode(e.target.value)}
        disabled={loading}
        autoFocus
        placeholder={method === 'backup' ? 'Enter 8-character backup code' : 'Enter verification code'}
        inputProps={{
          maxLength: method === 'backup' ? 8 : 6,
          pattern: '[0-9]*'
        }}
      />

      <Typography
        color={remainingAttempts <= 1 ? "error" : "warning"}
        sx={{ mt: 1 }}
      >
        {remainingAttempts} {remainingAttempts === 1 ? 'attempt' : 'attempts'} remaining
      </Typography>

      <Button
        type="submit"
        fullWidth
        variant="contained"
        sx={{ mt: 3, mb: 2 }}
        disabled={loading || !code || (method === 'backup' ? code.length !== 8 : code.length !== 6)}
        onClick={handleSubmit}
      >
        {loading ? <CircularProgress size={24} /> : 'Verify'}
      </Button>
    </>
  );

  return (
    <Card>
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Two-Factor Authentication Required
        </Typography>

        {getMethodInstructions()}

        {availableMethods.length > 1 && (
          <FormControl fullWidth margin="normal">
            <InputLabel>Authentication Method</InputLabel>
            <Select
              value={method}
              onChange={(e) => handleMethodChange(e.target.value)}
              label="Authentication Method"
            >
              {availableMethods.map((m) => (
                <MenuItem key={m} value={m}>
                  {getMethodDisplayName(m)}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        {method === 'passkey' ? renderPasskeyUI() : renderCodeUI()}
      </CardContent>
    </Card>
  );
};

export default MFAVerification;
