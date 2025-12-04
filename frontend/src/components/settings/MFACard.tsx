import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Switch,
  FormControlLabel,
  Button,
  Alert,
  CircularProgress,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListItemSecondaryAction,
  IconButton,
  Tooltip,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Chip,
  Divider,
} from '@mui/material';
import {
  Email as EmailIcon,
  Key as KeyIcon,
  QrCode2 as QrCodeIcon,
  ContentCopy as CopyIcon,
  Check as CheckIcon,
  Warning as WarningIcon,
  Fingerprint as FingerprintIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  Add as AddIcon,
} from '@mui/icons-material';
import { useAuth } from '../../contexts/AuthContext';
import {
  getUserMFASettings,
  enableMFA,
  disableMFA,
  verifyMFASetup,
  generateBackupCodes,
  updatePreferredMFAMethod,
  disableAuthenticator,
  getPasskeys,
  beginPasskeyRegistration,
  finishPasskeyRegistration,
  deletePasskey,
  renamePasskey,
} from '../../services/auth';
import { MFASettings, Passkey } from '../../types/auth';
import {
  isWebAuthnSupported,
  createPasskey,
  getWebAuthnErrorMessage,
} from '../../utils/webauthn';
import MFAMethodSelectionDialog from './MFAMethodSelectionDialog';

interface MFACardProps {
  onMFAChange?: () => void;
}

const MFACard: React.FC<MFACardProps> = ({ onMFAChange }): JSX.Element => {
  const { user, setUser } = useAuth();
  const [loading, setLoading] = useState(true);
  const [mfaSettings, setMFASettings] = useState<MFASettings | null>(null);
  const [showQRDialog, setShowQRDialog] = useState(false);
  const [showBackupCodes, setShowBackupCodes] = useState(false);
  const [showRegenerateWarning, setShowRegenerateWarning] = useState(false);
  const [showDisableAuthWarning, setShowDisableAuthWarning] = useState(false);
  const [verificationCode, setVerificationCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null);
  const [qrCode, setQrCode] = useState<string | null>(null);
  const [secret, setSecret] = useState<string | null>(null);

  // Passkey state
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [showAddPasskeyDialog, setShowAddPasskeyDialog] = useState(false);
  const [showDeletePasskeyDialog, setShowDeletePasskeyDialog] = useState(false);
  const [showRenamePasskeyDialog, setShowRenamePasskeyDialog] = useState(false);
  const [selectedPasskey, setSelectedPasskey] = useState<Passkey | null>(null);
  const [passkeyName, setPasskeyName] = useState('');
  const [passkeyLoading, setPasskeyLoading] = useState(false);
  const webAuthnSupported = isWebAuthnSupported();

  // MFA method selection state
  const [showMethodSelection, setShowMethodSelection] = useState(false);
  const [showPasskeyNameForMFA, setShowPasskeyNameForMFA] = useState(false);
  const [mfaPasskeyName, setMfaPasskeyName] = useState('');

  useEffect(() => {
    loadMFASettings();
    loadPasskeys();
  }, []);

  const loadMFASettings = async () => {
    try {
      const settings = await getUserMFASettings();
      setMFASettings(settings);
      setError(null);
    } catch (err) {
      setError('Failed to load MFA settings');
      console.error('Failed to load MFA settings:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadPasskeys = async () => {
    try {
      const passkeyList = await getPasskeys();
      setPasskeys(passkeyList);
    } catch (err) {
      console.error('Failed to load passkeys:', err);
      // Don't show error for passkeys as they may not be configured
    }
  };

  const handleMFAToggle = async () => {
    try {
      setLoading(true);
      setError(null);

      if (mfaSettings?.mfaEnabled) {
        // Disable MFA
        await disableMFA();
        setSuccess('MFA disabled successfully');
        await loadMFASettings();
        if (onMFAChange) {
          onMFAChange();
        }
        return;
      }

      // Calculate available methods for enabling MFA
      const hasEmail = mfaSettings?.allowedMfaMethods.includes('email');
      const hasAuthenticator = mfaSettings?.allowedMfaMethods.includes('authenticator');
      const hasPasskey = mfaSettings?.allowedMfaMethods.includes('passkey') && webAuthnSupported;
      const hasPasskeyMethodEnabled = mfaSettings?.allowedMfaMethods.includes('passkey');

      // Build list of available non-email methods
      const availableNonEmailMethods: ('authenticator' | 'passkey')[] = [];
      if (hasAuthenticator) availableNonEmailMethods.push('authenticator');
      if (hasPasskey) availableNonEmailMethods.push('passkey');

      // Decision logic based on available methods
      if (availableNonEmailMethods.length >= 2) {
        // Multiple non-email methods available - show selection dialog
        setShowMethodSelection(true);
        setLoading(false);
        return;
      } else if (availableNonEmailMethods.length === 1) {
        // Single non-email method available
        if (hasAuthenticator) {
          await handleAuthenticatorSetup();
          return;
        } else if (hasPasskey) {
          // Show passkey name dialog for MFA setup
          setMfaPasskeyName('');
          setShowPasskeyNameForMFA(true);
          setLoading(false);
          return;
        }
      } else if (hasPasskeyMethodEnabled && !webAuthnSupported) {
        // Passkey is the only non-email method but browser doesn't support WebAuthn
        setError(
          'Passkey authentication is required but your browser does not support WebAuthn. ' +
          'Please use a modern browser like Chrome, Firefox, Safari, or Edge to enable MFA.'
        );
        setLoading(false);
        return;
      } else if (hasEmail) {
        // Only email available - enable with email (no auto backup codes)
        await enableMFA('email');
        if (setUser && user) {
          setUser({ ...user, mfaEnabled: true, mfaType: 'email' });
        }
        setSuccess('MFA enabled with email authentication');
        await loadMFASettings();
        if (onMFAChange) {
          onMFAChange();
        }
        return;
      } else {
        // No MFA methods available
        throw new Error('No MFA methods are available. Please contact your administrator.');
      }
    } catch (err) {
      console.error('MFA toggle failed:', err);
      setError(err instanceof Error ? err.message : 'Failed to toggle MFA');
    } finally {
      setLoading(false);
    }
  };

  const handleMethodSelection = (method: 'authenticator' | 'passkey') => {
    setShowMethodSelection(false);
    if (method === 'authenticator') {
      handleAuthenticatorSetup();
    } else if (method === 'passkey') {
      setMfaPasskeyName('');
      setShowPasskeyNameForMFA(true);
    }
  };

  const handlePasskeySetupForMFA = async () => {
    if (!mfaPasskeyName.trim()) {
      setError('Please enter a name for the passkey');
      return;
    }

    setPasskeyLoading(true);
    setError(null);

    try {
      // Begin registration to get WebAuthn options from server
      const options = await beginPasskeyRegistration();

      // Create the credential using WebAuthn API
      const credential = await createPasskey(options);

      // Finish registration by sending credential to server
      const newPasskey = await finishPasskeyRegistration(mfaPasskeyName, credential);

      // Update passkeys list
      setPasskeys([...passkeys, newPasskey]);

      // Now enable MFA with passkey as the primary method
      await enableMFA('passkey');

      // Update user state
      if (setUser && user) {
        setUser({ ...user, mfaEnabled: true, mfaType: 'passkey' });
      }

      // Close the dialog and reset state
      setShowPasskeyNameForMFA(false);
      setMfaPasskeyName('');

      // Generate backup codes automatically for passkey setup
      try {
        const codes = await generateBackupCodes();
        setBackupCodes(codes);
        setShowBackupCodes(true);
        setSuccess('Passkey registered successfully! Please save your backup codes.');
      } catch (backupErr) {
        // If backup code generation fails, still show success for passkey
        setSuccess('Passkey registered successfully. You can generate backup codes from the MFA settings.');
      }

      // Reload MFA settings to reflect new passkey and MFA status
      await loadMFASettings();

      if (onMFAChange) {
        onMFAChange();
      }
    } catch (err) {
      const errorMessage = getWebAuthnErrorMessage(err);
      setError(errorMessage);
    } finally {
      setPasskeyLoading(false);
    }
  };

  const handleAuthenticatorSetup = async () => {
    try {
      setError(null);
      const response = await enableMFA('authenticator');
      if (response.qrCode && response.secret) {
        setQrCode(response.qrCode);
        setSecret(response.secret);
        setShowQRDialog(true);
        // Update user state to reflect pending authenticator setup
        if (setUser && user) {
          setUser({ ...user, mfaEnabled: false, mfaType: 'authenticator' });
        }
        // Reload MFA settings to get the latest state
        await loadMFASettings();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to setup authenticator');
      setLoading(false);
    }
  };

  const handleVerifyCode = async () => {
    if (!verificationCode || !user || !setUser) return;

    try {
      // Clear any existing errors
      setError(null);
      setSuccess(null);

      const response = await verifyMFASetup(verificationCode);

      // Update user state
      setUser({ ...user, mfaEnabled: true, mfaType: 'authenticator' });

      // Reload MFA settings to get the latest state
      await loadMFASettings();

      setSuccess('Authenticator app has been set up successfully');
      setShowQRDialog(false);
      setVerificationCode('');
      setQrCode(null);
      setSecret(null);

      // Handle backup codes if they were returned
      if (response?.backupCodes) {
        setBackupCodes(response.backupCodes);
        setShowBackupCodes(true);
      }

      if (onMFAChange) {
        onMFAChange();
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to verify code';
      setError(errorMessage);
      // Don't close dialog on error so user can try again
      setVerificationCode('');
    }
  };

  const handleGenerateBackupCodes = async () => {
    try {
      setError(null);
      const codes = await generateBackupCodes();
      setBackupCodes(codes);
      setShowBackupCodes(true);
      setSuccess('New backup codes have been generated');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to generate backup codes');
    }
  };

  const handleCopyCode = (code: string, index: number) => {
    navigator.clipboard.writeText(code);
    setCopiedIndex(index);
    setTimeout(() => setCopiedIndex(null), 2000);
  };

  const handlePreferredMethodChange = async (event: any) => {
    try {
      setError(null);
      const newMethod = event.target.value;
      await updatePreferredMFAMethod(newMethod);
      setSuccess('Preferred MFA method updated successfully');
      await loadMFASettings();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update preferred MFA method');
    }
  };

  const handleDisableAuthenticator = async () => {
    try {
      setError(null);
      setShowDisableAuthWarning(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to disable authenticator');
    }
  };

  const handleConfirmDisableAuth = async () => {
    try {
      setError(null);
      setLoading(true);
      await disableAuthenticator();
      setShowDisableAuthWarning(false);
      setSuccess('Authenticator has been disabled successfully');
      await loadMFASettings();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to disable authenticator');
    } finally {
      setLoading(false);
    }
  };

  const handleRegenerateBackupCodes = async () => {
    try {
      setError(null);
      setShowRegenerateWarning(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to generate new backup codes');
    }
  };

  const handleConfirmRegenerate = async () => {
    try {
      setError(null);
      const newCodes = await generateBackupCodes();
      if (Array.isArray(newCodes)) {
        setBackupCodes(newCodes);
        setShowBackupCodes(true);
        setShowRegenerateWarning(false);
        setSuccess('New backup codes have been generated successfully');
      } else {
        throw new Error('Invalid response format from server');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to generate new backup codes');
    }
  };

  // Passkey handlers
  const handleAddPasskey = async () => {
    if (!passkeyName.trim()) {
      setError('Please enter a name for the passkey');
      return;
    }

    setPasskeyLoading(true);
    setError(null);

    try {
      // Begin registration to get WebAuthn options from server
      const options = await beginPasskeyRegistration();

      // Create the credential using WebAuthn API
      const credential = await createPasskey(options);

      // Finish registration by sending credential to server
      const newPasskey = await finishPasskeyRegistration(passkeyName, credential);

      // Update passkeys list
      setPasskeys([...passkeys, newPasskey]);
      setShowAddPasskeyDialog(false);
      setPasskeyName('');
      setSuccess('Passkey registered successfully');

      // Reload MFA settings to reflect new passkey
      await loadMFASettings();

      if (onMFAChange) {
        onMFAChange();
      }
    } catch (err) {
      const errorMessage = getWebAuthnErrorMessage(err);
      setError(errorMessage);
    } finally {
      setPasskeyLoading(false);
    }
  };

  const handleDeletePasskey = async () => {
    if (!selectedPasskey) return;

    setPasskeyLoading(true);
    setError(null);

    try {
      await deletePasskey(selectedPasskey.id);
      setPasskeys(passkeys.filter((p) => p.id !== selectedPasskey.id));
      setShowDeletePasskeyDialog(false);
      setSelectedPasskey(null);
      setSuccess('Passkey deleted successfully');

      // Reload MFA settings to reflect removed passkey
      await loadMFASettings();

      if (onMFAChange) {
        onMFAChange();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete passkey');
    } finally {
      setPasskeyLoading(false);
    }
  };

  const handleRenamePasskey = async () => {
    if (!selectedPasskey || !passkeyName.trim()) return;

    setPasskeyLoading(true);
    setError(null);

    try {
      await renamePasskey(selectedPasskey.id, passkeyName);
      setPasskeys(
        passkeys.map((p) =>
          p.id === selectedPasskey.id ? { ...p, name: passkeyName } : p
        )
      );
      setShowRenamePasskeyDialog(false);
      setSelectedPasskey(null);
      setPasskeyName('');
      setSuccess('Passkey renamed successfully');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rename passkey');
    } finally {
      setPasskeyLoading(false);
    }
  };

  const openRenameDialog = (passkey: Passkey) => {
    setSelectedPasskey(passkey);
    setPasskeyName(passkey.name);
    setShowRenamePasskeyDialog(true);
  };

  const openDeleteDialog = (passkey: Passkey) => {
    setSelectedPasskey(passkey);
    setShowDeletePasskeyDialog(true);
  };

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" p={4}>
        <CircularProgress />
      </Box>
    );
  }

  const isEmailRequired = mfaSettings?.mfaEnabled && mfaSettings?.allowedMfaMethods.includes('email');
  const hasPasskeySupport = mfaSettings?.allowedMfaMethods?.includes('passkey') && webAuthnSupported;

  return (
    <Card sx={{ mb: 3 }}>
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Multi-Factor Authentication
        </Typography>

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {success && (
          <Alert severity="success" sx={{ mb: 2 }}>
            {success}
          </Alert>
        )}

        {mfaSettings?.requireMfa && (
          <Alert severity="info" sx={{ mb: 2 }}>
            MFA is required by your organization's security policy
          </Alert>
        )}

        <FormControlLabel
          control={
            <Switch
              checked={mfaSettings?.mfaEnabled || false}
              onChange={handleMFAToggle}
              disabled={mfaSettings?.requireMfa}
            />
          }
          label="Enable Multi-Factor Authentication"
        />

        {mfaSettings?.mfaEnabled && (
          <Box sx={{ mt: 2 }}>
            <FormControl fullWidth sx={{ mb: 2 }}>
              <InputLabel id="preferred-mfa-method-label">Preferred MFA Method</InputLabel>
              <Select
                labelId="preferred-mfa-method-label"
                value={mfaSettings.preferredMethod}
                onChange={handlePreferredMethodChange}
                label="Preferred MFA Method"
              >
                {(Array.isArray(mfaSettings?.mfaType) ? mfaSettings.mfaType : [])
                  .filter(method => method !== 'backup')  // Filter out backup from preferred methods
                  .map((method: string) => (
                    <MenuItem key={method} value={method}>
                      {method === 'passkey' ? 'Passkey' : method.charAt(0).toUpperCase() + method.slice(1)}
                    </MenuItem>
                  ))}
              </Select>
            </FormControl>

            <List>
              {/* Email MFA Status */}
              <ListItem>
                <ListItemIcon>
                  <EmailIcon color={isEmailRequired ? "primary" : "disabled"} />
                </ListItemIcon>
                <ListItemText
                  primary="Email Authentication"
                  secondary={isEmailRequired ? "Required for account security" : "Optional"}
                />
                {isEmailRequired && (
                  <Tooltip title="Required">
                    <WarningIcon color="info" />
                  </Tooltip>
                )}
              </ListItem>

              {/* Authenticator App Status */}
              <ListItem>
                <ListItemIcon>
                  <KeyIcon color={mfaSettings?.mfaType?.includes('authenticator') ? "primary" : "disabled"} />
                </ListItemIcon>
                <ListItemText
                  primary="Authenticator App"
                  secondary={mfaSettings?.mfaType?.includes('authenticator') ? "Configured" : "Not configured"}
                />
                {mfaSettings?.allowedMfaMethods?.includes('authenticator') && (
                  mfaSettings?.mfaType?.includes('authenticator') ? (
                    <Button
                      variant="outlined"
                      color="error"
                      onClick={handleDisableAuthenticator}
                    >
                      Disable
                    </Button>
                  ) : (
                    <Button
                      variant="outlined"
                      onClick={handleAuthenticatorSetup}
                      startIcon={<QrCodeIcon />}
                    >
                      Setup
                    </Button>
                  )
                )}
              </ListItem>

              {/* Passkeys Section */}
              {hasPasskeySupport && (
                <>
                  <Divider sx={{ my: 2 }} />
                  <ListItem>
                    <ListItemIcon>
                      <FingerprintIcon color={passkeys.length > 0 ? "primary" : "disabled"} />
                    </ListItemIcon>
                    <ListItemText
                      primary="Passkeys"
                      secondary={`${passkeys.length} passkey${passkeys.length !== 1 ? 's' : ''} registered`}
                    />
                    <Button
                      variant="outlined"
                      startIcon={<AddIcon />}
                      onClick={() => {
                        setPasskeyName('');
                        setShowAddPasskeyDialog(true);
                      }}
                    >
                      Add Passkey
                    </Button>
                  </ListItem>

                  {passkeys.length > 0 && (
                    <Box sx={{ pl: 7, pr: 2 }}>
                      {passkeys.map((passkey) => (
                        <Box
                          key={passkey.id}
                          sx={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            py: 1,
                            borderBottom: '1px solid',
                            borderColor: 'divider',
                          }}
                        >
                          <Box>
                            <Typography variant="body2">{passkey.name}</Typography>
                            <Typography variant="caption" color="text.secondary">
                              Added: {new Date(passkey.createdAt).toLocaleDateString()}
                              {passkey.lastUsedAt && (
                                <> | Last used: {new Date(passkey.lastUsedAt).toLocaleDateString()}</>
                              )}
                            </Typography>
                          </Box>
                          <Box>
                            <Tooltip title="Rename">
                              <IconButton
                                size="small"
                                onClick={() => openRenameDialog(passkey)}
                              >
                                <EditIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                            <Tooltip title="Delete">
                              <IconButton
                                size="small"
                                color="error"
                                onClick={() => openDeleteDialog(passkey)}
                              >
                                <DeleteIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          </Box>
                        </Box>
                      ))}
                    </Box>
                  )}
                </>
              )}

              {/* WebAuthn not supported warning */}
              {mfaSettings?.allowedMfaMethods?.includes('passkey') && !webAuthnSupported && (
                <Alert severity="warning" sx={{ mt: 2 }}>
                  Passkeys are not supported in this browser. Please use a modern browser to enable passkey authentication.
                </Alert>
              )}

              <Divider sx={{ my: 2 }} />

              {/* Backup Codes */}
              <ListItem>
                <ListItemIcon>
                  <KeyIcon color={(mfaSettings?.remainingBackupCodes ?? 0) > 0 ? "primary" : "disabled"} />
                </ListItemIcon>
                <ListItemText
                  primary="Backup Codes"
                  secondary={mfaSettings?.remainingBackupCodes
                    ? `${mfaSettings.remainingBackupCodes} backup ${mfaSettings.remainingBackupCodes === 1 ? 'code' : 'codes'} remaining`
                    : "No backup codes available"}
                />
                {mfaSettings?.mfaEnabled && (
                  <Button
                    variant="outlined"
                    onClick={handleRegenerateBackupCodes}
                  >
                    {mfaSettings?.remainingBackupCodes ? 'Regenerate' : 'Generate'}
                  </Button>
                )}
              </ListItem>
            </List>
          </Box>
        )}

        {/* QR Code Dialog */}
        <Dialog open={showQRDialog} onClose={() => setShowQRDialog(false)}>
          <DialogTitle>Setup Authenticator App</DialogTitle>
          <DialogContent>
            <Box sx={{ p: 2, textAlign: 'center' }}>
              {error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                  {error}
                </Alert>
              )}
              {qrCode && (
                <Box
                  component="img"
                  src={`data:image/png;base64,${qrCode}`}
                  alt="QR Code"
                  sx={{
                    width: 200,
                    height: 200,
                    mb: 2,
                  }}
                />
              )}
              {secret && (
                <Typography variant="body2" sx={{ mb: 2 }}>
                  If you can't scan the QR code, enter this code manually: <strong>{secret}</strong>
                </Typography>
              )}
              <Typography variant="body2" sx={{ mb: 2 }}>
                Scan this QR code with your authenticator app (e.g., Google Authenticator, Authy)
              </Typography>
              <TextField
                fullWidth
                label="Verification Code"
                value={verificationCode}
                onChange={(e) => setVerificationCode(e.target.value)}
                margin="normal"
                autoComplete="off"
                placeholder="Enter the 6-digit code"
                inputProps={{
                  maxLength: 6,
                  pattern: '[0-9]*',
                }}
              />
            </Box>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => {
              setShowQRDialog(false);
              setQrCode(null);
              setSecret(null);
              setVerificationCode('');
            }}>
              Cancel
            </Button>
            <Button
              onClick={handleVerifyCode}
              variant="contained"
              disabled={!verificationCode || verificationCode.length !== 6}
            >
              Verify
            </Button>
          </DialogActions>
        </Dialog>

        {/* Backup Codes Dialog */}
        <Dialog
          open={showBackupCodes}
          onClose={() => setShowBackupCodes(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>Backup Codes</DialogTitle>
          <DialogContent>
            {backupCodes.length === 0 ? (
              <Box sx={{ textAlign: 'center', py: 2 }}>
                <Typography variant="body2" sx={{ mb: 2 }}>
                  Generate backup codes to use when you can't access your primary authentication method
                </Typography>
                <Button
                  variant="contained"
                  onClick={handleGenerateBackupCodes}
                >
                  Generate Backup Codes
                </Button>
              </Box>
            ) : (
              <Box>
                <Typography variant="body2" color="warning.main" sx={{ mb: 2 }}>
                  Save these codes in a secure location. They will not be shown again!
                </Typography>
                <Box
                  sx={{
                    fontFamily: 'monospace',
                    fontSize: '1.1rem',
                    mb: 3,
                    pl: 2
                  }}
                >
                  {backupCodes.map((code) => (
                    <Typography key={code} sx={{ mb: 1 }}>
                      {code}
                    </Typography>
                  ))}
                </Box>
                <Button
                  fullWidth
                  variant="contained"
                  color="error"
                  startIcon={copiedIndex === -1 ? <CheckIcon /> : <CopyIcon />}
                  onClick={() => {
                    navigator.clipboard.writeText(backupCodes.join('\n'));
                    setCopiedIndex(-1);
                    setTimeout(() => setCopiedIndex(null), 2000);
                  }}
                >
                  {copiedIndex === -1 ? 'Copied!' : 'COPY ALL CODES'}
                </Button>
              </Box>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowBackupCodes(false)}>Close</Button>
          </DialogActions>
        </Dialog>

        {/* Regenerate Warning Dialog */}
        <Dialog
          open={showRegenerateWarning}
          onClose={() => setShowRegenerateWarning(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>
            <Box display="flex" alignItems="center" gap={1}>
              <WarningIcon color="warning" />
              <Typography>Warning</Typography>
            </Box>
          </DialogTitle>
          <DialogContent>
            <Typography>
              This will invalidate all your existing backup codes. Are you sure you want to generate new ones?
            </Typography>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowRegenerateWarning(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleConfirmRegenerate}
              variant="contained"
              color="warning"
            >
              Generate New Codes
            </Button>
          </DialogActions>
        </Dialog>

        {/* Disable Authenticator Warning Dialog */}
        <Dialog
          open={showDisableAuthWarning}
          onClose={() => setShowDisableAuthWarning(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>
            <Box display="flex" alignItems="center" gap={1}>
              <WarningIcon color="warning" />
              <Typography>Warning</Typography>
            </Box>
          </DialogTitle>
          <DialogContent>
            <Typography>
              Are you sure you want to disable the authenticator? This will remove it from your account and you will need to set it up again if you want to use it in the future.
            </Typography>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowDisableAuthWarning(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleConfirmDisableAuth}
              variant="contained"
              color="warning"
            >
              Disable Authenticator
            </Button>
          </DialogActions>
        </Dialog>

        {/* Add Passkey Dialog */}
        <Dialog
          open={showAddPasskeyDialog}
          onClose={() => setShowAddPasskeyDialog(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>Add Passkey</DialogTitle>
          <DialogContent>
            <Typography variant="body2" sx={{ mb: 2 }}>
              Register a new passkey (security key, fingerprint, or device) for two-factor authentication.
            </Typography>
            <TextField
              fullWidth
              label="Passkey Name"
              value={passkeyName}
              onChange={(e) => setPasskeyName(e.target.value)}
              placeholder="e.g., YubiKey, MacBook Touch ID, Bitwarden"
              margin="normal"
              autoFocus
            />
            {!webAuthnSupported && (
              <Alert severity="error" sx={{ mt: 2 }}>
                WebAuthn is not supported in this browser. Please use a modern browser.
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowAddPasskeyDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleAddPasskey}
              variant="contained"
              disabled={passkeyLoading || !passkeyName.trim() || !webAuthnSupported}
              startIcon={passkeyLoading ? <CircularProgress size={16} /> : <FingerprintIcon />}
            >
              {passkeyLoading ? 'Registering...' : 'Register Passkey'}
            </Button>
          </DialogActions>
        </Dialog>

        {/* Rename Passkey Dialog */}
        <Dialog
          open={showRenamePasskeyDialog}
          onClose={() => setShowRenamePasskeyDialog(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>Rename Passkey</DialogTitle>
          <DialogContent>
            <TextField
              fullWidth
              label="Passkey Name"
              value={passkeyName}
              onChange={(e) => setPasskeyName(e.target.value)}
              margin="normal"
              autoFocus
            />
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowRenamePasskeyDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleRenamePasskey}
              variant="contained"
              disabled={passkeyLoading || !passkeyName.trim()}
            >
              {passkeyLoading ? 'Saving...' : 'Save'}
            </Button>
          </DialogActions>
        </Dialog>

        {/* Delete Passkey Dialog */}
        <Dialog
          open={showDeletePasskeyDialog}
          onClose={() => setShowDeletePasskeyDialog(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>
            <Box display="flex" alignItems="center" gap={1}>
              <WarningIcon color="warning" />
              <Typography>Delete Passkey</Typography>
            </Box>
          </DialogTitle>
          <DialogContent>
            <Typography>
              Are you sure you want to delete the passkey "{selectedPasskey?.name}"? This action cannot be undone.
            </Typography>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowDeletePasskeyDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleDeletePasskey}
              variant="contained"
              color="error"
              disabled={passkeyLoading}
            >
              {passkeyLoading ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogActions>
        </Dialog>

        {/* MFA Method Selection Dialog */}
        <MFAMethodSelectionDialog
          open={showMethodSelection}
          onClose={() => setShowMethodSelection(false)}
          onSelectMethod={handleMethodSelection}
          availableMethods={
            [
              ...(mfaSettings?.allowedMfaMethods.includes('authenticator') ? ['authenticator' as const] : []),
              ...(mfaSettings?.allowedMfaMethods.includes('passkey') && webAuthnSupported ? ['passkey' as const] : []),
            ]
          }
        />

        {/* Passkey Name Dialog for MFA Setup */}
        <Dialog
          open={showPasskeyNameForMFA}
          onClose={() => setShowPasskeyNameForMFA(false)}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>Setup Passkey for MFA</DialogTitle>
          <DialogContent>
            <Typography variant="body2" sx={{ mb: 2 }}>
              Register a passkey to enable multi-factor authentication. Use a security key, fingerprint, face recognition, or device PIN.
            </Typography>
            <TextField
              fullWidth
              label="Passkey Name"
              value={mfaPasskeyName}
              onChange={(e) => setMfaPasskeyName(e.target.value)}
              placeholder="e.g., YubiKey, MacBook Touch ID, Bitwarden"
              margin="normal"
              autoFocus
            />
            {!webAuthnSupported && (
              <Alert severity="error" sx={{ mt: 2 }}>
                WebAuthn is not supported in this browser. Please use a modern browser like Chrome, Firefox, Safari, or Edge.
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setShowPasskeyNameForMFA(false)}>
              Cancel
            </Button>
            <Button
              onClick={handlePasskeySetupForMFA}
              variant="contained"
              disabled={passkeyLoading || !mfaPasskeyName.trim() || !webAuthnSupported}
              startIcon={passkeyLoading ? <CircularProgress size={16} /> : <FingerprintIcon />}
            >
              {passkeyLoading ? 'Registering...' : 'Register & Enable MFA'}
            </Button>
          </DialogActions>
        </Dialog>
      </CardContent>
    </Card>
  );
};

export default MFACard;
