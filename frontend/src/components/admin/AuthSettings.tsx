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
import { getPasswordPolicy, getAccountSecurity, getAdminMFASettings, updateMFASettings } from '../../services/auth';
import { PasswordPolicy, AccountSecurity, AuthSettingsUpdate, MFASettings as MFASettingsType } from '../../types/auth';

interface AuthSettingsFormProps {
  onSave: (settings: AuthSettingsUpdate) => Promise<void>;
  loading?: boolean;
}

const STORAGE_KEY = 'auth_settings_draft';
const LAST_FETCH_KEY = 'auth_settings_last_fetch';

const AuthSettingsForm: React.FC<AuthSettingsFormProps> = ({ onSave, loading = false }) => {
  const [passwordPolicy, setPasswordPolicy] = useState<PasswordPolicy | null>(null);
  const [accountSecurity, setAccountSecurity] = useState<AccountSecurity | null>(null);
  const [mfaSettings, setMFASettings] = useState<MFASettingsType | null>(null);
  const [loadingData, setLoadingData] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasLocalChanges, setHasLocalChanges] = useState(false);
  const [lastSavedTimestamp, setLastSavedTimestamp] = useState<number>(0);

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
      setError('Failed to load authentication settings');
    } finally {
      setLoadingData(false);
    }
  };

  const handleSave = async () => {
    if (!passwordPolicy || !accountSecurity || !mfaSettings) {
      setError('No settings to save');
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
      notificationAggregationMinutes: accountSecurity.notificationAggregationMinutes === '' ? 60 : accountSecurity.notificationAggregationMinutes
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
      let errorMessage = 'Failed to save settings';
      if (error instanceof Error) {
        if (error.message.includes('email provider')) {
          errorMessage = 'Cannot enable global MFA without configuring an email provider first. Please configure an email provider in the Email Settings section.';
        } else {
          errorMessage = error.message;
        }
      }
      setError(errorMessage);
      throw error;
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
      setError('Failed to reset to database values');
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
          You have unsaved changes
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
                Password Policy
              </Typography>
              <FormGroup sx={{ flex: 1 }}>
                <TextField
                  label="Minimum Password Length"
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
                    label="Require Uppercase Letters"
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireLowercase}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireLowercase: e.target.checked }))}
                      />
                    }
                    label="Require Lowercase Letters"
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireNumbers}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireNumbers: e.target.checked }))}
                      />
                    }
                    label="Require Numbers"
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={passwordPolicy?.requireSpecialChars}
                        onChange={e => setPasswordPolicy(prev => ({ ...prev!, requireSpecialChars: e.target.checked }))}
                      />
                    }
                    label="Require Special Characters"
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
                Account Security
              </Typography>
              <FormGroup sx={{ flex: 1 }}>
                <TextField
                  label="Maximum Failed Login Attempts"
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
                  label="Account Lockout Duration (minutes)"
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
                  label="JWT Token Expiry (minutes)"
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
                  label="Notification Aggregation Interval (minutes)"
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
                  helperText="How often to aggregate and send security notifications"
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
                Multi-Factor Authentication Settings
              </Typography>
              <FormGroup>
                <FormControlLabel
                  control={
                    <Switch
                      checked={mfaSettings?.requireMfa}
                      onChange={e => setMFASettings(prev => ({ ...prev!, requireMfa: e.target.checked }))}
                    />
                  }
                  label="Require MFA for all users"
                />
                <Typography variant="subtitle1" sx={{ mt: 2, mb: 1 }}>
                  Allowed MFA Methods
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
                  label="Email"
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
                  label="Authenticator"
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
                    />
                  }
                  label="Passkey"
                />
                <Typography variant="subtitle1" sx={{ mt: 3, mb: 1 }}>
                  Code Settings
                </Typography>
                <TextField
                  label="Email Code Validity (minutes)"
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
                  helperText="Must be at least 1 minute"
                />
                <TextField
                  label="Code Cooldown Period (minutes)"
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
                  helperText="Time required between code requests"
                />
                <TextField
                  label="Code Expiry Time (minutes)"
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
                  helperText="Time before a code expires"
                />
                <TextField
                  label="Maximum Code Attempts"
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
                  helperText="Maximum attempts before code invalidation"
                />
                <TextField
                  label="Number of Backup Codes"
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
                  helperText="Must be at least 1 code"
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
          {loading ? 'Saving...' : 'Save Settings'}
        </Button>

        {hasLocalChanges && (
          <Button
            variant="outlined"
            color="secondary"
            onClick={handleResetToDatabase}
            disabled={loading}
          >
            Reset to Saved Values
          </Button>
        )}
      </Box>
    </Box>
  );
};

export default AuthSettingsForm; 