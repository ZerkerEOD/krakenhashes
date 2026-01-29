import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Grid,
  Alert,
  CircularProgress,
  Divider,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  Chip,
  Link,
} from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import WarningIcon from '@mui/icons-material/Warning';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import SecurityIcon from '@mui/icons-material/Security';
import DownloadIcon from '@mui/icons-material/Download';
import { useAuth } from '../../contexts/AuthContext';
import { updateUserProfile, ProfileUpdate } from '../../services/user';
import { getPasswordPolicy } from '../../services/auth';
import { PasswordPolicy } from '../../types/auth';
import PasswordValidation from '../../components/common/PasswordValidation';
import MFACard from '../../components/settings/MFACard';
import NotificationCard from '../../components/settings/NotificationCard';
import LinkedAccountsCard from '../../components/settings/LinkedAccountsCard';
import { usePasswordConfirm } from '../../hooks/usePasswordConfirm';
import { generateApiKey, getApiKeyInfo, revokeApiKey } from '../../services/api';
import { ApiKeyInfo } from '../../types/user';

interface PasswordChangeForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

const ProfileSettings: React.FC = () => {
  const { t } = useTranslation('settings');
  const { user, setUser } = useAuth();
  const { showPasswordConfirm, PasswordConfirmDialog } = usePasswordConfirm();

  // Email update state
  const [email, setEmail] = useState(user?.email || '');
  const [emailLoading, setEmailLoading] = useState(false);
  const [emailError, setEmailError] = useState<string | null>(null);
  const [emailSuccess, setEmailSuccess] = useState<string | null>(null);

  // Password change state
  const [passwordForm, setPasswordForm] = useState<PasswordChangeForm>({
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
  });
  const [passwordLoading, setPasswordLoading] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null);

  const [policy, setPolicy] = useState<PasswordPolicy | null>(null);

  // API Key state
  const [apiKeyInfo, setApiKeyInfo] = useState<ApiKeyInfo | null>(null);
  const [apiKeyLoading, setApiKeyLoading] = useState(false);
  const [apiKeyError, setApiKeyError] = useState<string | null>(null);
  const [apiKeySuccess, setApiKeySuccess] = useState<string | null>(null);
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false);
  const [generatedApiKey, setGeneratedApiKey] = useState<string>('');
  const [showConfirmRevoke, setShowConfirmRevoke] = useState(false);
  const [showConfirmRegenerate, setShowConfirmRegenerate] = useState(false);

  useEffect(() => {
    const loadPolicy = async () => {
      try {
        const policyData = await getPasswordPolicy();
        setPolicy(policyData);
      } catch (error) {
        console.error('Failed to load password policy:', error);
      }
    };
    loadPolicy();

    const loadApiKeyInfo = async () => {
      try {
        const response = await getApiKeyInfo();
        setApiKeyInfo(response.data.data);
      } catch (error) {
        console.error('Failed to load API key info:', error);
      }
    };
    loadApiKeyInfo();
  }, []);

  const validatePassword = (password: string): boolean => {
    if (!policy) return false;

    const validation = {
      length: password.length >= (policy.minPasswordLength || 15),
      uppercase: !policy.requireUppercase || /[A-Z]/.test(password),
      lowercase: !policy.requireLowercase || /[a-z]/.test(password),
      numbers: !policy.requireNumbers || /[0-9]/.test(password),
      specialChars: !policy.requireSpecialChars || /[!@#$%^&*(),.?":{}|<>]/.test(password),
    };

    return Object.values(validation).every(Boolean);
  };

  const handlePasswordChange = (field: keyof PasswordChangeForm) => (event: React.ChangeEvent<HTMLInputElement>) => {
    setPasswordForm(prev => ({ ...prev, [field]: event.target.value }));
  };

  const handleEmailUpdate = async () => {
    if (!email || email === user?.email) {
      setEmailError(t('account.errors.noChanges') as string);
      return;
    }

    // Basic email validation
    if (!email.includes('@')) {
      setEmailError(t('account.errors.invalidEmail') as string);
      return;
    }

    setEmailError(null);
    setEmailSuccess(null);

    // Show password confirmation dialog
    const password = await showPasswordConfirm(
      t('account.confirmEmailUpdate.title') as string,
      t('account.confirmEmailUpdate.message') as string
    );

    if (!password) {
      return; // User cancelled
    }

    setEmailLoading(true);

    try {
      const updates: ProfileUpdate = {
        email: email,
        currentPassword: password,
      };

      await updateUserProfile(updates);
      setEmailSuccess(t('account.success.emailUpdated') as string);

      if (setUser && user) {
        setUser({ ...user, email: email });
      }
    } catch (error: any) {
      const errorMessage = error?.response || error?.message || t('account.errors.updateFailed') as string;
      if (errorMessage.includes('password')) {
        setEmailError(t('account.errors.incorrectPassword') as string);
      } else {
        setEmailError(errorMessage);
      }
    } finally {
      setEmailLoading(false);
    }
  };

  const handlePasswordUpdate = async (event: React.FormEvent) => {
    event.preventDefault();
    setPasswordLoading(true);
    setPasswordError(null);
    setPasswordSuccess(null);

    try {
      if (!passwordForm.currentPassword) {
        throw new Error(t('password.errors.currentRequired') as string);
      }

      if (!passwordForm.newPassword) {
        throw new Error(t('password.errors.newRequired') as string);
      }

      if (passwordForm.newPassword !== passwordForm.confirmPassword) {
        throw new Error(t('password.errors.mismatch') as string);
      }

      if (!validatePassword(passwordForm.newPassword)) {
        throw new Error(t('password.errors.requirements') as string);
      }

      const updates: ProfileUpdate = {
        currentPassword: passwordForm.currentPassword,
        newPassword: passwordForm.newPassword,
      };

      await updateUserProfile(updates);
      setPasswordSuccess(t('password.success.changed') as string);

      // Clear password fields after successful update
      setPasswordForm({
        currentPassword: '',
        newPassword: '',
        confirmPassword: '',
      });
    } catch (error: any) {
      const errorMessage = error?.response || error?.message || t('password.errors.updateFailed') as string;
      setPasswordError(errorMessage);
    } finally {
      setPasswordLoading(false);
    }
  };

  const handleGenerateApiKey = async () => {
    setApiKeyLoading(true);
    setApiKeyError(null);
    setApiKeySuccess(null);

    try {
      const response = await generateApiKey();
      setGeneratedApiKey(response.data.data.apiKey);
      setShowApiKeyDialog(true);
      setShowConfirmRegenerate(false);

      // Refresh API key info
      const infoResponse = await getApiKeyInfo();
      setApiKeyInfo(infoResponse.data.data);
    } catch (error: any) {
      const errorMessage = error?.response?.data?.error || error?.message || t('apiKey.errors.generateFailed') as string;
      setApiKeyError(errorMessage);
    } finally {
      setApiKeyLoading(false);
    }
  };

  const handleRevokeApiKey = async () => {
    setApiKeyLoading(true);
    setApiKeyError(null);
    setApiKeySuccess(null);

    try {
      await revokeApiKey();
      setApiKeySuccess(t('apiKey.success.revoked') as string);
      setShowConfirmRevoke(false);

      // Refresh API key info
      const infoResponse = await getApiKeyInfo();
      setApiKeyInfo(infoResponse.data.data);
    } catch (error: any) {
      const errorMessage = error?.response?.data?.error || error?.message || t('apiKey.errors.revokeFailed') as string;
      setApiKeyError(errorMessage);
    } finally {
      setApiKeyLoading(false);
    }
  };

  const handleCopyApiKey = () => {
    navigator.clipboard.writeText(generatedApiKey);
    setApiKeySuccess(t('apiKey.success.copied') as string);
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return t('common.never') as string;
    try {
      return new Date(dateString).toLocaleString();
    } catch {
      return t('common.invalidDate') as string;
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h4" gutterBottom>
        {t('profileSettings.title') as string}
      </Typography>

      <PasswordConfirmDialog />

      {/* Account Information Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t('account.title') as string}
          </Typography>

          {emailError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {emailError}
            </Alert>
          )}

          {emailSuccess && (
            <Alert severity="success" sx={{ mb: 2 }}>
              {emailSuccess}
            </Alert>
          )}

          <Grid container spacing={2}>
            <Grid item xs={12} sm={6}>
              <TextField
                fullWidth
                label={t('account.username') as string}
                value={user?.username || ''}
                disabled
                margin="normal"
                helperText={t('account.usernameCannotChange') as string}
              />
            </Grid>
            <Grid item xs={12} sm={6}>
              <TextField
                fullWidth
                label={t('account.email') as string}
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  setEmailError(null);
                  setEmailSuccess(null);
                }}
                type="email"
                margin="normal"
              />
            </Grid>
          </Grid>

          <Box sx={{ mt: 2, display: 'flex', justifyContent: 'flex-end' }}>
            <Button
              variant="contained"
              color="primary"
              onClick={handleEmailUpdate}
              disabled={emailLoading || email === user?.email}
              startIcon={emailLoading && <CircularProgress size={20} color="inherit" />}
            >
              {emailLoading ? t('account.saving') as string : t('account.saveEmail') as string}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {/* Change Password Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t('password.title') as string}
          </Typography>

          {passwordError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {passwordError}
            </Alert>
          )}

          {passwordSuccess && (
            <Alert severity="success" sx={{ mb: 2 }}>
              {passwordSuccess}
            </Alert>
          )}

          <form onSubmit={handlePasswordUpdate}>
            <Grid container spacing={2}>
              <Grid item xs={12}>
                <TextField
                  fullWidth
                  label={t('password.currentPassword') as string}
                  value={passwordForm.currentPassword}
                  onChange={handlePasswordChange('currentPassword')}
                  type="password"
                  margin="normal"
                />
              </Grid>
              <Grid item xs={12} sm={6}>
                <TextField
                  fullWidth
                  label={t('password.newPassword') as string}
                  value={passwordForm.newPassword}
                  onChange={handlePasswordChange('newPassword')}
                  type="password"
                  margin="normal"
                  disabled={!passwordForm.currentPassword}
                  helperText={!passwordForm.currentPassword ? t('password.enterCurrentFirst') as string : ""}
                />
                {passwordForm.newPassword && (
                  <PasswordValidation password={passwordForm.newPassword} />
                )}
              </Grid>
              <Grid item xs={12} sm={6}>
                <TextField
                  fullWidth
                  label={t('password.confirmNewPassword') as string}
                  value={passwordForm.confirmPassword}
                  onChange={handlePasswordChange('confirmPassword')}
                  type="password"
                  margin="normal"
                  disabled={!passwordForm.currentPassword}
                  error={passwordForm.newPassword !== passwordForm.confirmPassword && passwordForm.confirmPassword !== ''}
                  helperText={
                    !passwordForm.currentPassword ? t('password.enterCurrentFirst') as string :
                    passwordForm.confirmPassword !== '' && (
                      passwordForm.newPassword !== passwordForm.confirmPassword
                        ? t('password.errors.mismatch') as string
                        : passwordForm.newPassword === passwordForm.confirmPassword
                          ? t('password.passwordsMatch') as string
                          : ''
                    )
                  }
                  FormHelperTextProps={{
                    sx: {
                      color: passwordForm.confirmPassword !== '' && passwordForm.newPassword === passwordForm.confirmPassword
                        ? 'success.main'
                        : 'error.main'
                    }
                  }}
                />
              </Grid>
            </Grid>

            <Box sx={{ mt: 3, display: 'flex', justifyContent: 'flex-end' }}>
              <Button
                variant="contained"
                color="primary"
                type="submit"
                disabled={passwordLoading}
                startIcon={passwordLoading && <CircularProgress size={20} color="inherit" />}
              >
                {passwordLoading ? t('password.changing') as string : t('password.changePassword') as string}
              </Button>
            </Box>
          </form>
        </CardContent>
      </Card>

      <MFACard onMFAChange={() => {
        // Refresh user data when MFA settings change
        if (setUser && user) {
          setUser({ ...user });
        }
      }} />

      <LinkedAccountsCard />

      <NotificationCard onNotificationChange={() => {
        // You can add any refresh logic here if needed
        console.log('Notification preferences updated');
      }} />

      {/* API Keys Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t('apiKey.title') as string}
          </Typography>

          {apiKeyError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {apiKeyError}
            </Alert>
          )}

          {apiKeySuccess && (
            <Alert severity="success" sx={{ mb: 2 }}>
              {apiKeySuccess}
            </Alert>
          )}

          <Box sx={{ mb: 3 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
              <VpnKeyIcon color={apiKeyInfo?.hasKey ? 'success' : 'disabled'} />
              <Typography variant="body1">
                {t('apiKey.status') as string}:{' '}
                {apiKeyInfo?.hasKey ? (
                  <Chip label={t('apiKey.active') as string} color="success" size="small" icon={<CheckCircleIcon />} />
                ) : (
                  <Chip label={t('apiKey.noKeyGenerated') as string} size="small" />
                )}
              </Typography>
            </Box>

            {apiKeyInfo?.hasKey && (
              <Box sx={{ ml: 4 }}>
                <Typography variant="body2" color="text.secondary">
                  {t('apiKey.created') as string}: {formatDate(apiKeyInfo.createdAt)}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {t('apiKey.lastUsed') as string}: {formatDate(apiKeyInfo.lastUsed)}
                </Typography>
              </Box>
            )}
          </Box>

          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
            <Button
              variant="contained"
              startIcon={<VpnKeyIcon />}
              onClick={() => {
                if (apiKeyInfo?.hasKey) {
                  setShowConfirmRegenerate(true);
                } else {
                  handleGenerateApiKey();
                }
              }}
              disabled={apiKeyLoading}
            >
              {apiKeyInfo?.hasKey ? t('apiKey.regenerate') as string : t('apiKey.generate') as string}
            </Button>

            {apiKeyInfo?.hasKey && (
              <Button
                variant="outlined"
                color="error"
                onClick={() => setShowConfirmRevoke(true)}
                disabled={apiKeyLoading}
              >
                {t('apiKey.revoke') as string}
              </Button>
            )}

            <Button
              variant="text"
              component={Link}
              href="https://zerkereod.github.io/krakenhashes/user-api/"
              target="_blank"
              rel="noopener noreferrer"
            >
              {t('apiKey.viewDocs') as string}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {/* Security Settings Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t('security.title') as string}
          </Typography>

          <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 2, mb: 2 }}>
            <SecurityIcon color="primary" sx={{ mt: 0.5 }} />
            <Box>
              <Typography variant="subtitle1" gutterBottom>
                {t('security.caCertificate.title') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                {t('security.caCertificate.description') as string}
              </Typography>
            </Box>
          </Box>

          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
            <Button
              variant="contained"
              startIcon={<DownloadIcon />}
              onClick={() => {
                const httpPort = 1337;
                const downloadUrl = `http://${window.location.hostname}:${httpPort}/ca.crt`;
                window.open(downloadUrl, '_blank');
              }}
            >
              {t('security.caCertificate.download') as string}
            </Button>
            <Button
              variant="text"
              component={Link}
              href="https://zerkereod.github.io/krakenhashes/admin-guide/system-setup/ssl-tls/"
              target="_blank"
              rel="noopener noreferrer"
            >
              {t('security.caCertificate.docsLink') as string}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {/* API Key Display Dialog */}
      <Dialog
        open={showApiKeyDialog}
        onClose={() => {
          setShowApiKeyDialog(false);
          setGeneratedApiKey('');
        }}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <VpnKeyIcon color="primary" />
            {t('apiKey.dialog.title') as string}
          </Box>
        </DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 3 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <WarningIcon />
              <Typography variant="body2">
                <strong>{t('apiKey.dialog.saveNow') as string}</strong> {t('apiKey.dialog.cantSeeAgain') as string}
              </Typography>
            </Box>
          </Alert>

          <Box sx={{ position: 'relative' }}>
            <TextField
              fullWidth
              multiline
              rows={3}
              value={generatedApiKey}
              InputProps={{
                readOnly: true,
                sx: {
                  fontFamily: 'monospace',
                  fontSize: '0.9rem',
                },
                endAdornment: (
                  <IconButton onClick={handleCopyApiKey} sx={{ position: 'absolute', right: 8, top: 8 }}>
                    <ContentCopyIcon />
                  </IconButton>
                ),
              }}
            />
          </Box>

          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            {t('apiKey.dialog.instructions') as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button
            variant="contained"
            onClick={() => {
              setShowApiKeyDialog(false);
              setGeneratedApiKey('');
            }}
          >
            {t('apiKey.dialog.saved') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Confirm Regenerate Dialog */}
      <Dialog
        open={showConfirmRegenerate}
        onClose={() => setShowConfirmRegenerate(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('apiKey.confirmRegenerate.title') as string}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            {t('apiKey.confirmRegenerate.warning') as string}
          </Alert>
          <Typography>{t('apiKey.confirmRegenerate.confirm') as string}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowConfirmRegenerate(false)}>{t('common.cancel') as string}</Button>
          <Button
            variant="contained"
            color="warning"
            onClick={handleGenerateApiKey}
            disabled={apiKeyLoading}
          >
            {t('apiKey.regenerate') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Confirm Revoke Dialog */}
      <Dialog
        open={showConfirmRevoke}
        onClose={() => setShowConfirmRevoke(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('apiKey.confirmRevoke.title') as string}</DialogTitle>
        <DialogContent>
          <Alert severity="error" sx={{ mb: 2 }}>
            {t('apiKey.confirmRevoke.warning') as string}
          </Alert>
          <Typography>{t('apiKey.confirmRevoke.confirm') as string}</Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowConfirmRevoke(false)}>{t('common.cancel') as string}</Button>
          <Button
            variant="contained"
            color="error"
            onClick={handleRevokeApiKey}
            disabled={apiKeyLoading}
          >
            {t('apiKey.revoke') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default ProfileSettings; 