import React, { useEffect, useState } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Switch,
  FormControlLabel,
  FormGroup,
  TextField,
  Button,
  Alert,
  CircularProgress,
  Grid,
  Checkbox,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { getPasswordPolicy, getAccountSecurity, getAdminMFASettings, updateMFASettings, getWebAuthnSettings, updateWebAuthnSettings } from '../../services/auth';
import { getEmailConfig } from '../../services/api';
import { PasswordPolicy, AccountSecurity, AuthSettingsUpdate, MFASettings as MFASettingsType, WebAuthnSettings } from '../../types/auth';
import { isWebAuthnSupported } from '../../utils/webauthn';

interface AuthSettingsFormProps {
  onSave: (settings: AuthSettingsUpdate) => Promise<void>;
  loading?: boolean;
}

const STORAGE_KEY = 'auth_settings_draft';
const LAST_FETCH_KEY = 'auth_settings_last_fetch';

const AuthSettingsForm: React.FC<AuthSettingsFormProps> = ({ onSave, loading = false }) => {
  const { t } = useTranslation('admin');
  const [passwordPolicy, setPasswordPolicy] = useState<PasswordPolicy | null>(null);
  const [accountSecurity, setAccountSecurity] = useState<AccountSecurity | null>(null);
  const [mfaSettings, setMFASettings] = useState<MFASettingsType | null>(null);
  const [webauthnSettings, setWebauthnSettings] = useState<WebAuthnSettings | null>(null);
  const [loadingData, setLoadingData] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasLocalChanges, setHasLocalChanges] = useState(false);
  const [lastSavedTimestamp, setLastSavedTimestamp] = useState<number>(0);
  const [hasEmailGateway, setHasEmailGateway] = useState(false);
  const webAuthnSupported = isWebAuthnSupported();

  // Load settings on mount or when lastSavedTimestamp changes
  useEffect(() => {
    loadSettings();
  }, [lastSavedTimestamp]);

  // Handle local storage updates
  useEffect(() => {
    if (!passwordPolicy || !accountSecurity || !mfaSettings) return;

    const lastFetch = localStorage.getItem(LAST_FETCH_KEY);
    const currentTime = Date.now();

    // Only update local storage if we have actual changes from the last fetched state
    const lastFetchData = lastFetch ? JSON.parse(lastFetch) : null;
    if (lastFetchData) {
      const hasChanges = JSON.stringify({
        passwordPolicy,
        accountSecurity,
        mfaSettings
      }) !== JSON.stringify({
        passwordPolicy: lastFetchData.passwordPolicy,
        accountSecurity: lastFetchData.accountSecurity,
        mfaSettings: lastFetchData.mfaSettings
      });

      if (hasChanges) {
        localStorage.setItem(STORAGE_KEY, JSON.stringify({
          passwordPolicy,
          accountSecurity,
          mfaSettings,
          timestamp: currentTime
        }));
        setHasLocalChanges(true);
      }
    }
  }, [passwordPolicy, accountSecurity, mfaSettings]);

  const loadSettings = async () => {
    setLoadingData(true);
    try {
      const currentTime = Date.now();
      const lastFetch = localStorage.getItem(LAST_FETCH_KEY);
      const savedDraft = localStorage.getItem(STORAGE_KEY);

      // Check email gateway status
      try {
        const emailConfig = await getEmailConfig();
        setHasEmailGateway(!!emailConfig?.data?.provider_type && emailConfig?.data?.is_active !== false);
      } catch (err) {
        // If we can't fetch email config, assume no gateway
        setHasEmailGateway(false);
      }

      // Load WebAuthn settings
      try {
        const webauthn = await getWebAuthnSettings();
        setWebauthnSettings(webauthn);
      } catch (err) {
        // WebAuthn settings may not be configured yet
        setWebauthnSettings(null);
      }

      // If we have a saved draft and it's newer than our last fetch, use it
      if (savedDraft && (!lastFetch || JSON.parse(savedDraft).timestamp > JSON.parse(lastFetch).timestamp)) {
        const parsed = JSON.parse(savedDraft);
        const validatedMfaSettings = {
          ...parsed.mfaSettings,
          emailCodeValidity: Math.max(1, parsed.mfaSettings.emailCodeValidity),
          backupCodesCount: Math.max(1, parsed.mfaSettings.backupCodesCount),
          allowedMfaMethods: parsed.mfaSettings.allowedMfaMethods || ['email']
        };
        setPasswordPolicy(parsed.passwordPolicy);
        setAccountSecurity(parsed.accountSecurity);
        setMFASettings(validatedMfaSettings);
        setHasLocalChanges(true);
      } else {
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
        setHasLocalChanges(false);

        // Store the fetched data as our new baseline
        localStorage.setItem(LAST_FETCH_KEY, JSON.stringify({
          passwordPolicy: policyData,
          accountSecurity: securityData,
          mfaSettings: validatedMfaSettings,
          timestamp: currentTime
        }));
      }
      setError(null);
    } catch (error) {
      console.error('Failed to load settings:', error);
      setError(t('authSettings.errors.loadFailed') as string);
    } finally {
      setLoadingData(false);
    }
  };

  const handleSave = async () => {
    if (!passwordPolicy || !accountSecurity || !mfaSettings) {
      setError(t('authSettings.errors.noSettingsToSave') as string);
      return;
    }

    // Validate and apply defaults for empty values
    const validatedSettings = {
      minPasswordLength: passwordPolicy.minPasswordLength === '' ? 8 : passwordPolicy.minPasswordLength,
      requireUppercase: passwordPolicy.requireUppercase,
      requireLowercase: passwordPolicy.requireLowercase,
      requireNumbers: passwordPolicy.requireNumbers,
      requireSpecialChars: passwordPolicy.requireSpecialChars,
      maxFailedAttempts: accountSecurity.maxFailedAttempts === '' ? 5 : accountSecurity.maxFailedAttempts,
      lockoutDuration: accountSecurity.lockoutDuration === '' ? 30 : accountSecurity.lockoutDuration,
      jwtExpiryMinutes: accountSecurity.jwtExpiryMinutes === '' ? 60 : accountSecurity.jwtExpiryMinutes,
      notificationAggregationMinutes: accountSecurity.notificationAggregationMinutes === '' ? 60 : accountSecurity.notificationAggregationMinutes,
      tokenCleanupIntervalSeconds: accountSecurity.tokenCleanupIntervalSeconds === '' ? 60 : accountSecurity.tokenCleanupIntervalSeconds,
      maxConcurrentSessions: accountSecurity.maxConcurrentSessions === '' ? 0 : accountSecurity.maxConcurrentSessions,
      sessionAbsoluteTimeoutHours: accountSecurity.sessionAbsoluteTimeoutHours === '' ? 0 : accountSecurity.sessionAbsoluteTimeoutHours
    };

    // Validate MFA settings
    const validatedMFASettings = {
      ...mfaSettings,
      emailCodeValidity: mfaSettings.emailCodeValidity === '' ? 5 : mfaSettings.emailCodeValidity,
      backupCodesCount: mfaSettings.backupCodesCount === '' ? 8 : mfaSettings.backupCodesCount,
      mfaCodeCooldownMinutes: mfaSettings.mfaCodeCooldownMinutes === '' ? 1 : mfaSettings.mfaCodeCooldownMinutes,
      mfaCodeExpiryMinutes: mfaSettings.mfaCodeExpiryMinutes === '' ? 5 : mfaSettings.mfaCodeExpiryMinutes,
      mfaMaxAttempts: mfaSettings.mfaMaxAttempts === '' ? 3 : mfaSettings.mfaMaxAttempts
    };

    try {
      // Save all settings in parallel
      await Promise.all([
        onSave(validatedSettings),
        updateMFASettings(validatedMFASettings)
      ]);

      // Clear draft and update last fetch
      localStorage.removeItem(STORAGE_KEY);
      localStorage.setItem(LAST_FETCH_KEY, JSON.stringify({
        passwordPolicy: { ...passwordPolicy, ...validatedSettings },
        accountSecurity: { 
          ...accountSecurity, 
          maxFailedAttempts: validatedSettings.maxFailedAttempts,
          lockoutDuration: validatedSettings.lockoutDuration,
          jwtExpiryMinutes: validatedSettings.jwtExpiryMinutes,
          notificationAggregationMinutes: validatedSettings.notificationAggregationMinutes
        },
        mfaSettings: validatedMFASettings,
        timestamp: Date.now()
      }));
      
      setHasLocalChanges(false);
      setLastSavedTimestamp(Date.now()); // Trigger a refresh
      setError(null);
    } catch (error) {
      console.error('Failed to save settings:', error);
      let errorMessage = t('authSettings.errors.saveFailed') as string;
      if (error instanceof Error) {
        if (error.message.includes('email provider')) {
          errorMessage = t('authSettings.errors.emailProviderRequired') as string;
        } else {
          errorMessage = error.message;
        }
      }
      setError(errorMessage);
    }
  };

  const handleSaveWebAuthn = async () => {
    if (!webauthnSettings) return;

    try {
      await updateWebAuthnSettings(webauthnSettings);
      // Refresh WebAuthn settings after save
      const updatedSettings = await getWebAuthnSettings();
      setWebauthnSettings(updatedSettings);
      setError(null);
    } catch (err) {
      console.error('Failed to save WebAuthn settings:', err);
      setError(t('authSettings.errors.webauthnSaveFailed') as string);
    }
  };

  const handleResetToDatabase = async () => {
    try {
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
      
      // Update storage
      localStorage.removeItem(STORAGE_KEY);
      localStorage.setItem(LAST_FETCH_KEY, JSON.stringify({
        passwordPolicy: policyData,
        accountSecurity: securityData,
        mfaSettings: validatedMfaSettings,
        timestamp: Date.now()
      }));
      
      setHasLocalChanges(false);
    } catch (error) {
      console.error('Failed to reset settings:', error);
      setError(t('authSettings.errors.resetFailed') as string);
    }
  };

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

      {hasLocalChanges && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {t('authSettings.unsavedChanges') as string}
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
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                />
                <Box sx={{ mt: 2 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireUppercase}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireUppercase: e.target.checked }))}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireUppercase') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireLowercase}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireLowercase: e.target.checked }))}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireLowercase') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireNumbers}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireNumbers: e.target.checked }))}
                      />
                    }
                    label={t('authSettings.passwordPolicy.requireNumbers') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireSpecialChars}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireSpecialChars: e.target.checked }))}
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
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                />
                <TextField
                  label={t('authSettings.accountSecurity.lockoutDuration') as string}
                  type="number"
                  value={accountSecurity?.lockoutDuration ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    lockoutDuration: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                />
                <TextField
                  label={t('authSettings.accountSecurity.jwtExpiry') as string}
                  type="number"
                  value={accountSecurity?.jwtExpiryMinutes ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    jwtExpiryMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                />
                <TextField
                  label={t('authSettings.accountSecurity.notificationAggregation') as string}
                  type="number"
                  value={accountSecurity?.notificationAggregationMinutes ?? ''}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    notificationAggregationMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.accountSecurity.notificationAggregationHelper') as string}
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
                      onChange={e => setMFASettings(prev => ({ ...prev!, requireMfa: e.target.checked }))}
                      disabled={!hasEmailGateway}
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
                      onChange={e => {
                        const methods = new Set(mfaSettings?.allowedMfaMethods || []);
                        if (e.target.checked) {
                          methods.add('email');
                        } else {
                          methods.delete('email');
                        }
                        setMFASettings(prev => ({ ...prev!, allowedMfaMethods: Array.from(methods) }));
                      }}
                    />
                  }
                  label={t('authSettings.mfa.methodEmail') as string}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={mfaSettings?.allowedMfaMethods.includes('authenticator')}
                      onChange={e => {
                        const methods = new Set(mfaSettings?.allowedMfaMethods || []);
                        if (e.target.checked) {
                          methods.add('authenticator');
                        } else {
                          methods.delete('authenticator');
                        }
                        setMFASettings(prev => ({ ...prev!, allowedMfaMethods: Array.from(methods) }));
                      }}
                    />
                  }
                  label={t('authSettings.mfa.methodAuthenticator') as string}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={mfaSettings?.allowedMfaMethods.includes('passkey')}
                      onChange={e => {
                        const methods = new Set(mfaSettings?.allowedMfaMethods || []);
                        if (e.target.checked) {
                          methods.add('passkey');
                        } else {
                          methods.delete('passkey');
                        }
                        setMFASettings(prev => ({ ...prev!, allowedMfaMethods: Array.from(methods) }));
                      }}
                      disabled={!webAuthnSupported}
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
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.emailCodeValidityHelper') as string}
                />
                <TextField
                  label={t('authSettings.mfa.codeCooldown') as string}
                  type="number"
                  value={mfaSettings?.mfaCodeCooldownMinutes ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaCodeCooldownMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.codeCooldownHelper') as string}
                />
                <TextField
                  label={t('authSettings.mfa.codeExpiry') as string}
                  type="number"
                  value={mfaSettings?.mfaCodeExpiryMinutes ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaCodeExpiryMinutes: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.codeExpiryHelper') as string}
                />
                <TextField
                  label={t('authSettings.mfa.maxCodeAttempts') as string}
                  type="number"
                  value={mfaSettings?.mfaMaxAttempts ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    mfaMaxAttempts: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.maxCodeAttemptsHelper') as string}
                />
                <TextField
                  label={t('authSettings.mfa.backupCodesCount') as string}
                  type="number"
                  value={mfaSettings?.backupCodesCount ?? ''}
                  onChange={e => setMFASettings(prev => ({
                    ...prev!,
                    backupCodesCount: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 1 }}
                  helperText={t('authSettings.mfa.backupCodesCountHelper') as string}
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
                  fullWidth
                  margin="normal"
                  placeholder="krakenhashes.example.com"
                  helperText={t('authSettings.webauthn.rpIdHelper') as string}
                />

                <TextField
                  label={t('authSettings.webauthn.rpDisplayName') as string}
                  value={webauthnSettings?.rpDisplayName ?? ''}
                  onChange={e => setWebauthnSettings(prev => ({ ...prev!, rpDisplayName: e.target.value }))}
                  fullWidth
                  margin="normal"
                  placeholder="KrakenHashes"
                  helperText={t('authSettings.webauthn.rpDisplayNameHelper') as string}
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
                  fullWidth
                  margin="normal"
                  multiline
                  rows={3}
                  placeholder="https://localhost:3000"
                  helperText={t('authSettings.webauthn.allowedOriginsHelper') as string}
                />

                {webauthnSettings?.rpId && webauthnSettings?.rpOrigins?.length > 0 && (
                  <Alert severity="success" sx={{ mt: 2 }}>
                    {t('authSettings.webauthn.configured') as string}
                  </Alert>
                )}

                <Box sx={{ mt: 2 }}>
                  <Button
                    variant="contained"
                    onClick={handleSaveWebAuthn}
                    disabled={loading || !webauthnSettings?.rpId || !webauthnSettings?.rpOrigins?.length}
                  >
                    {t('authSettings.webauthn.save') as string}
                  </Button>
                </Box>
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
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 10 }}
                  helperText={t('authSettings.session.tokenCleanupIntervalHelper') as string}
                />
                <TextField
                  label={t('authSettings.session.maxConcurrentSessions') as string}
                  type="number"
                  value={accountSecurity?.maxConcurrentSessions ?? 0}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    maxConcurrentSessions: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 0 }}
                  helperText={t('authSettings.session.maxConcurrentSessionsHelper') as string}
                />
                <TextField
                  label={t('authSettings.session.absoluteTimeout') as string}
                  type="number"
                  value={accountSecurity?.sessionAbsoluteTimeoutHours ?? 0}
                  onChange={e => setAccountSecurity(prev => ({
                    ...prev!,
                    sessionAbsoluteTimeoutHours: e.target.value === '' ? '' as any : parseInt(e.target.value)
                  }))}
                  fullWidth
                  margin="normal"
                  autoComplete="off"
                  inputProps={{ min: 0 }}
                  helperText={t('authSettings.session.absoluteTimeoutHelper') as string}
                />
              </FormGroup>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      <Box sx={{ mt: 3, display: 'flex', gap: 2 }}>
        <Button
          variant="contained"
          color="primary"
          onClick={handleSave}
          disabled={loading}
          startIcon={loading && <CircularProgress size={20} color="inherit" />}
        >
          {loading ? t('authSettings.saving') as string : t('authSettings.saveSettings') as string}
        </Button>

        {hasLocalChanges && (
          <Button
            variant="outlined"
            color="secondary"
            onClick={handleResetToDatabase}
            disabled={loading}
          >
            {t('authSettings.resetToSaved') as string}
          </Button>
        )}
      </Box>
    </Box>
  );
};

export default AuthSettingsForm; 