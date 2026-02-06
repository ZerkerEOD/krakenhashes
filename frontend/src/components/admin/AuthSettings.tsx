import React, { useEffect, useState, useRef, useCallback } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Switch,
  FormControlLabel,
  FormGroup,
  TextField,
  Alert,
  CircularProgress,
  Grid,
  Checkbox,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useSnackbar } from 'notistack';
import {
  getPasswordPolicy,
  getAccountSecurity,
  getAdminMFASettings,
  updateAuthSettings,
  updateMFASettings,
  getWebAuthnSettings,
  updateWebAuthnSettings,
} from '../../services/auth';
import { getEmailConfig } from '../../services/api';
import { adminTeamsService } from '../../services/teams';
import { PasswordPolicy, AccountSecurity, MFASettings as MFASettingsType, WebAuthnSettings } from '../../types/auth';
import { isWebAuthnSupported } from '../../utils/webauthn';

const AuthSettingsForm: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();
  const [passwordPolicy, setPasswordPolicy] = useState<PasswordPolicy | null>(null);
  const [accountSecurity, setAccountSecurity] = useState<AccountSecurity | null>(null);
  const [mfaSettings, setMFASettings] = useState<MFASettingsType | null>(null);
  const [webauthnSettings, setWebauthnSettings] = useState<WebAuthnSettings | null>(null);
  const [loadingData, setLoadingData] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasEmailGateway, setHasEmailGateway] = useState(false);
  const [teamsEnabled, setTeamsEnabled] = useState(false);
  const webAuthnSupported = isWebAuthnSupported();

  // Refs to track latest state for blur handlers
  const passwordPolicyRef = useRef<PasswordPolicy | null>(null);
  const accountSecurityRef = useRef<AccountSecurity | null>(null);
  const mfaSettingsRef = useRef<MFASettingsType | null>(null);
  const webauthnSettingsRef = useRef<WebAuthnSettings | null>(null);

  useEffect(() => { passwordPolicyRef.current = passwordPolicy; }, [passwordPolicy]);
  useEffect(() => { accountSecurityRef.current = accountSecurity; }, [accountSecurity]);
  useEffect(() => { mfaSettingsRef.current = mfaSettings; }, [mfaSettings]);
  useEffect(() => { webauthnSettingsRef.current = webauthnSettings; }, [webauthnSettings]);

  useEffect(() => {
    loadSettings();
  }, []);

  const loadSettings = async () => {
    setLoadingData(true);
    try {
      // Check email gateway status
      try {
        const emailConfig = await getEmailConfig();
        setHasEmailGateway(!!emailConfig?.data?.provider_type && emailConfig?.data?.is_active !== false);
      } catch {
        setHasEmailGateway(false);
      }

      // Load WebAuthn settings
      try {
        const webauthn = await getWebAuthnSettings();
        setWebauthnSettings(webauthn);
      } catch {
        setWebauthnSettings(null);
      }

      // Load teams_enabled setting
      try {
        const enabled = await adminTeamsService.getTeamsEnabled();
        setTeamsEnabled(enabled);
      } catch {
        setTeamsEnabled(false);
      }

      // Fetch fresh data from API
      const [policyData, securityData, mfaData] = await Promise.all([
        getPasswordPolicy(),
        getAccountSecurity(),
        getAdminMFASettings()
      ]);

      const validatedMfaSettings = {
        ...mfaData,
        emailCodeValidity: typeof mfaData.emailCodeValidity === 'number' ? Math.max(1, mfaData.emailCodeValidity) : mfaData.emailCodeValidity,
        backupCodesCount: typeof mfaData.backupCodesCount === 'number' ? Math.max(1, mfaData.backupCodesCount) : mfaData.backupCodesCount,
        allowedMfaMethods: mfaData.allowedMfaMethods || ['email']
      };

      setPasswordPolicy(policyData);
      setAccountSecurity(securityData);
      setMFASettings(validatedMfaSettings);
      setError(null);
    } catch (err) {
      console.error('Failed to load settings:', err);
      setError(t('authSettings.errors.loadFailed') as string);
    } finally {
      setLoadingData(false);
    }
  };

  // --- Auth Settings Save (password policy + account security) ---
  const saveAuthSettings = useCallback(async (
    policy: PasswordPolicy,
    security: AccountSecurity
  ) => {
    const validatedSettings = {
      minPasswordLength: policy.minPasswordLength === '' ? 8 : policy.minPasswordLength,
      requireUppercase: policy.requireUppercase,
      requireLowercase: policy.requireLowercase,
      requireNumbers: policy.requireNumbers,
      requireSpecialChars: policy.requireSpecialChars,
      maxFailedAttempts: security.maxFailedAttempts === '' ? 5 : security.maxFailedAttempts,
      lockoutDuration: security.lockoutDuration === '' ? 30 : security.lockoutDuration,
      jwtExpiryMinutes: security.jwtExpiryMinutes === '' ? 60 : security.jwtExpiryMinutes,
      notificationAggregationMinutes: security.notificationAggregationMinutes === '' ? 60 : security.notificationAggregationMinutes,
      tokenCleanupIntervalSeconds: security.tokenCleanupIntervalSeconds === '' ? 60 : security.tokenCleanupIntervalSeconds,
      maxConcurrentSessions: security.maxConcurrentSessions === '' ? 0 : security.maxConcurrentSessions,
      sessionAbsoluteTimeoutHours: security.sessionAbsoluteTimeoutHours === '' ? 0 : security.sessionAbsoluteTimeoutHours
    };

    setSaving(true);
    setError(null);
    try {
      await updateAuthSettings(validatedSettings);
      enqueueSnackbar(t('settings.saved') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save auth settings:', err);
      const errorMessage = err.message || t('authSettings.errors.saveFailed') as string;
      setError(errorMessage);
      enqueueSnackbar(errorMessage, { variant: 'error' });
      await loadSettings();
    } finally {
      setSaving(false);
    }
  }, [t, enqueueSnackbar]);

  // --- MFA Settings Save ---
  const saveMFASettings = useCallback(async (settings: MFASettingsType) => {
    const validated = {
      ...settings,
      emailCodeValidity: settings.emailCodeValidity === '' ? 5 : settings.emailCodeValidity,
      backupCodesCount: settings.backupCodesCount === '' ? 8 : settings.backupCodesCount,
      mfaCodeCooldownMinutes: settings.mfaCodeCooldownMinutes === '' ? 1 : settings.mfaCodeCooldownMinutes,
      mfaCodeExpiryMinutes: settings.mfaCodeExpiryMinutes === '' ? 5 : settings.mfaCodeExpiryMinutes,
      mfaMaxAttempts: settings.mfaMaxAttempts === '' ? 3 : settings.mfaMaxAttempts
    };

    setSaving(true);
    setError(null);
    try {
      await updateMFASettings(validated);
      enqueueSnackbar(t('settings.saved') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save MFA settings:', err);
      const errorMessage = err.message || t('authSettings.errors.saveFailed') as string;
      setError(errorMessage);
      enqueueSnackbar(errorMessage, { variant: 'error' });
      await loadSettings();
    } finally {
      setSaving(false);
    }
  }, [t, enqueueSnackbar]);

  // --- WebAuthn Settings Save ---
  const saveWebAuthnSettings = useCallback(async (settings: WebAuthnSettings) => {
    if (!settings.rpId || !settings.rpOrigins?.length) return;

    setSaving(true);
    setError(null);
    try {
      await updateWebAuthnSettings(settings);
      const updatedSettings = await getWebAuthnSettings();
      setWebauthnSettings(updatedSettings);
      enqueueSnackbar(t('settings.saved') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save WebAuthn settings:', err);
      setError(t('authSettings.errors.webauthnSaveFailed') as string);
      enqueueSnackbar(t('authSettings.errors.webauthnSaveFailed') as string, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  }, [t, enqueueSnackbar]);

  // --- Switch handlers (save immediately) ---
  const handlePolicySwitchChange = useCallback((field: keyof PasswordPolicy) => async (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.checked;
    const previousPolicy = { ...passwordPolicyRef.current! };
    const updatedPolicy = { ...previousPolicy, [field]: newValue };
    setPasswordPolicy(updatedPolicy);
    try {
      await saveAuthSettings(updatedPolicy, accountSecurityRef.current!);
    } catch {
      setPasswordPolicy(previousPolicy);
    }
  }, [saveAuthSettings]);

  const handleMfaSwitchChange = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.checked;
    const previousMfa = { ...mfaSettingsRef.current! };
    const updatedMfa = { ...previousMfa, requireMfa: newValue };
    setMFASettings(updatedMfa);
    try {
      await saveMFASettings(updatedMfa);
    } catch {
      setMFASettings(previousMfa);
    }
  }, [saveMFASettings]);

  const handleMfaMethodChange = useCallback((method: string) => async (e: React.ChangeEvent<HTMLInputElement>) => {
    const previousMfa = { ...mfaSettingsRef.current! };
    const methods = new Set(previousMfa.allowedMfaMethods || []);
    if (e.target.checked) {
      methods.add(method);
    } else {
      methods.delete(method);
    }
    const updatedMfa = { ...previousMfa, allowedMfaMethods: Array.from(methods) };
    setMFASettings(updatedMfa);
    try {
      await saveMFASettings(updatedMfa);
    } catch {
      setMFASettings(previousMfa);
    }
  }, [saveMFASettings]);

  // --- Blur handlers (save on blur) ---
  const handleAuthBlurSave = useCallback(async () => {
    if (!passwordPolicyRef.current || !accountSecurityRef.current) return;
    await saveAuthSettings(passwordPolicyRef.current, accountSecurityRef.current);
  }, [saveAuthSettings]);

  const handleMfaBlurSave = useCallback(async () => {
    if (!mfaSettingsRef.current) return;
    await saveMFASettings(mfaSettingsRef.current);
  }, [saveMFASettings]);

  const handleWebauthnBlurSave = useCallback(async () => {
    if (!webauthnSettingsRef.current) return;
    await saveWebAuthnSettings(webauthnSettingsRef.current);
  }, [saveWebAuthnSettings]);

  // --- Teams toggle handler ---
  const handleTeamsToggle = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.checked;
    const previousValue = teamsEnabled;
    setTeamsEnabled(newValue);
    setSaving(true);
    try {
      await adminTeamsService.setTeamsEnabled(newValue);
      enqueueSnackbar(
        newValue ? 'Multi-team mode enabled' : 'Multi-team mode disabled',
        { variant: 'success' }
      );
    } catch (err: any) {
      console.error('Failed to update teams setting:', err);
      setTeamsEnabled(previousValue);
      enqueueSnackbar('Failed to update teams setting', { variant: 'error' });
    } finally {
      setSaving(false);
    }
  }, [teamsEnabled, enqueueSnackbar]);

  if (loadingData) {
    return (
      <Box display="flex" justifyContent="center" p={4}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Grid container spacing={3}>
        {/* Password Policy */}
        <Grid item xs={12} md={6}>
          <Card sx={{
            height: '100%',
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            '& .MuiCardContent-root': {
              height: '100%',
              display: 'flex',
              flexDirection: 'column'
            }
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                {t('authSettings.passwordPolicy.title') as string}
              </Typography>
              <FormGroup sx={{ flex: 1 }}>
                <TextField
                  label={t('authSettings.passwordPolicy.minLength') as string}
                  type="number"
                  value={passwordPolicy?.minPasswordLength ?? ''}
                  onChange={e => setPasswordPolicy(prev => ({
                    ...prev!,
                    minPasswordLength: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  disabled={saving}
                />
                <Box sx={{ mt: 2 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireUppercase}
                        onChange={handlePolicySwitchChange('requireUppercase')}
                        disabled={saving}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireUppercase') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireLowercase}
                        onChange={handlePolicySwitchChange('requireLowercase')}
                        disabled={saving}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireLowercase') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireNumbers}
                        onChange={handlePolicySwitchChange('requireNumbers')}
                        disabled={saving}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireNumbers') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireSpecialChars}
                        onChange={handlePolicySwitchChange('requireSpecialChars')}
                        disabled={saving}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireSpecialChars') as string}
                  />
                </Box>
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>

        {/* Account Security */}
        <Grid item xs={12} md={6}>
          <Card sx={{
            height: '100%',
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            '& .MuiCardContent-root': {
              height: '100%',
              display: 'flex',
              flexDirection: 'column'
            }
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                {t('authSettings.accountSecurity.title') as string}
              </Typography>
              <FormGroup sx={{ flex: 1 }}>
                <TextField
                  label={t('authSettings.accountSecurity.maxFailedAttempts') as string}
                  type="number"
                  value={accountSecurity?.maxFailedAttempts ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    maxFailedAttempts: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.accountSecurity.lockoutDuration') as string}
                  type="number"
                  value={accountSecurity?.lockoutDuration ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    lockoutDuration: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.accountSecurity.jwtExpiry') as string}
                  type="number"
                  value={accountSecurity?.jwtExpiryMinutes ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    jwtExpiryMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.accountSecurity.notificationAggregation') as string}
                  type="number"
                  value={accountSecurity?.notificationAggregationMinutes ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    notificationAggregationMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.accountSecurity.notificationAggregationHelper') as string}
                  disabled={saving}
                />
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>

        {/* MFA Settings */}
        <Grid item xs={12}>
          <Card sx={{
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            mt: 2
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                {t('authSettings.mfa.title') as string}
              </Typography>
              <FormGroup>
                <FormControlLabel
                  control={
                    <Switch
                      checked={mfaSettings?.requireMfa}
                      onChange={handleMfaSwitchChange}
                      disabled={!hasEmailGateway || saving}
                    />
                  }
                  label={hasEmailGateway ? t('authSettings.mfa.requireMfa') as string : t('authSettings.mfa.requireMfaEmailRequired') as string}
                />
                <Typography variant="subtitle1" sx={{ mt: 2, mb: 1 }}>
                  {t('authSettings.mfa.allowedMethods') as string}
                </Typography>
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={mfaSettings?.allowedMfaMethods.includes('email')}
                      onChange={handleMfaMethodChange('email')}
                      disabled={saving}
                    />
                  }
                  label={t('authSettings.mfa.methodEmail') as string}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={mfaSettings?.allowedMfaMethods.includes('authenticator')}
                      onChange={handleMfaMethodChange('authenticator')}
                      disabled={saving}
                    />
                  }
                  label={t('authSettings.mfa.methodAuthenticator') as string}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={mfaSettings?.allowedMfaMethods.includes('passkey')}
                      onChange={handleMfaMethodChange('passkey')}
                      disabled={!webAuthnSupported || saving}
                    />
                  }
                  label={!webAuthnSupported ? t('authSettings.mfa.methodPasskeyNotSupported') as string : t('authSettings.mfa.methodPasskey') as string}
                />
                <Typography variant="subtitle1" sx={{ mt: 3, mb: 1 }}>
                  {t('authSettings.mfa.codeSettings') as string}
                </Typography>
                <TextField
                  label={t('authSettings.mfa.emailCodeValidity') as string}
                  type="number"
                  value={mfaSettings?.emailCodeValidity ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    emailCodeValidity: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleMfaBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.emailCodeValidityHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.mfa.codeCooldown') as string}
                  type="number"
                  value={mfaSettings?.mfaCodeCooldownMinutes ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaCodeCooldownMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleMfaBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.codeCooldownHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.mfa.codeExpiry') as string}
                  type="number"
                  value={mfaSettings?.mfaCodeExpiryMinutes ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaCodeExpiryMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleMfaBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.codeExpiryHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.mfa.maxCodeAttempts') as string}
                  type="number"
                  value={mfaSettings?.mfaMaxAttempts ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaMaxAttempts: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleMfaBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.maxCodeAttemptsHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.mfa.backupCodesCount') as string}
                  type="number"
                  value={mfaSettings?.backupCodesCount ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    backupCodesCount: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleMfaBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.backupCodesCountHelper') as string}
                  disabled={saving}
                />
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>

        {/* WebAuthn / Passkey Configuration */}
        <Grid item xs={12}>
          <Card sx={{
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            mt: 2
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                {t('authSettings.webauthn.title') as string}
              </Typography>

              {!webAuthnSupported && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                  {t('authSettings.webauthn.notSupported') as string}
                </Alert>
              )}

              <Alert severity="info" sx={{ mb: 2 }}>
                {t('authSettings.webauthn.requiresDomain') as string}
              </Alert>

              <FormGroup>
                <TextField
                  label={t('authSettings.webauthn.rpId') as string}
                  value={webauthnSettings?.rpId ?? ''}
                  onChange={e => setWebauthnSettings(prev => ({ ...prev!, rpId: e.target.value }))}
                  onBlur={handleWebauthnBlurSave}
                  fullWidth
                  margin="normal"
                  placeholder="krakenhashes.example.com"
                  helperText={t('authSettings.webauthn.rpIdHelper') as string}
                  disabled={saving}
                />

                <TextField
                  label={t('authSettings.webauthn.rpDisplayName') as string}
                  value={webauthnSettings?.rpDisplayName ?? ''}
                  onChange={e => setWebauthnSettings(prev => ({ ...prev!, rpDisplayName: e.target.value }))}
                  onBlur={handleWebauthnBlurSave}
                  fullWidth
                  margin="normal"
                  placeholder="KrakenHashes"
                  helperText={t('authSettings.webauthn.rpDisplayNameHelper') as string}
                  disabled={saving}
                />

                <TextField
                  label={t('authSettings.webauthn.allowedOrigins') as string}
                  value={webauthnSettings?.rpOrigins?.join('\n') ?? ''}
                  onChange={e => {
                    const origins = e.target.value
                      .split(/[\n]/)
                      .map(o => o.trim())
                      .filter(o => o.length > 0);
                    setWebauthnSettings(prev => ({ ...prev!, rpOrigins: origins }));
                  }}
                  onBlur={handleWebauthnBlurSave}
                  fullWidth
                  margin="normal"
                  multiline
                  rows={3}
                  placeholder="https://localhost:3000"
                  helperText={t('authSettings.webauthn.allowedOriginsHelper') as string}
                  disabled={saving}
                />

                {webauthnSettings?.rpId && webauthnSettings?.rpOrigins?.length > 0 && (
                  <Alert severity="success" sx={{ mt: 2 }}>
                    {t('authSettings.webauthn.configured') as string}
                  </Alert>
                )}
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>

        {/* Session Management */}
        <Grid item xs={12}>
          <Card sx={{
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            mt: 2
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                {t('authSettings.session.title') as string}
              </Typography>
              <FormGroup>
                <TextField
                  label={t('authSettings.session.tokenCleanupInterval') as string}
                  type="number"
                  value={accountSecurity?.tokenCleanupIntervalSeconds ?? 60}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    tokenCleanupIntervalSeconds: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 10 }}
                  helperText={t('authSettings.session.tokenCleanupIntervalHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.session.maxConcurrentSessions') as string}
                  type="number"
                  value={accountSecurity?.maxConcurrentSessions ?? 0}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    maxConcurrentSessions: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 0 }}
                  helperText={t('authSettings.session.maxConcurrentSessionsHelper') as string}
                  disabled={saving}
                />
                <TextField
                  label={t('authSettings.session.absoluteTimeout') as string}
                  type="number"
                  value={accountSecurity?.sessionAbsoluteTimeoutHours ?? 0}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    sessionAbsoluteTimeoutHours: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  onBlur={handleAuthBlurSave}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 0 }}
                  helperText={t('authSettings.session.absoluteTimeoutHelper') as string}
                  disabled={saving}
                />
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>

        {/* Multi-Team Mode */}
        <Grid item xs={12}>
          <Card sx={{
            backgroundColor: 'background.paper',
            boxShadow: (theme) => `0 0 10px ${theme.palette.divider}`,
            mt: 2
          }}>
            <CardContent>
              <Typography variant="h6" gutterBottom sx={{
                pb: 2,
                borderBottom: (theme) => `1px solid ${theme.palette.divider}`
              }}>
                Multi-Team Mode
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                When enabled, users can only see clients, hashlists, and jobs assigned to their teams.
                Users can be members of multiple teams, and team admins can manage team membership and client assignments.
              </Typography>
              <FormControlLabel
                control={
                  <Switch
                    checked={teamsEnabled}
                    onChange={handleTeamsToggle}
                    disabled={saving}
                  />
                }
                label={teamsEnabled ? 'Enabled' : 'Disabled'}
              />
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
};

export default AuthSettingsForm;
