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
  Checkbox,
  Alert,
  CircularProgress,
  Divider,
} from '@mui/material';
import { MFASettings as IMFASettings, MFAMethod, WebAuthnSettings } from '../../types/auth';
import { getAdminMFASettings, updateMFASettings, getWebAuthnSettings, updateWebAuthnSettings } from '../../services/auth';
import { getEmailConfig } from '../../services/api';
import { isWebAuthnSupported } from '../../utils/webauthn';

const MFASettings: React.FC = () => {
  const [settings, setSettings] = useState<IMFASettings>({
    requireMfa: false,
    allowedMfaMethods: ['email'],
    emailCodeValidity: 5,
    backupCodesCount: 8,
    mfaCodeCooldownMinutes: 1,
    mfaCodeExpiryMinutes: 5,
    mfaMaxAttempts: 3,
    mfaEnabled: false
  });
  const [webauthnSettings, setWebauthnSettings] = useState<WebAuthnSettings>({
    rpId: '',
    rpOrigins: [],
    rpDisplayName: 'KrakenHashes',
    configured: false
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [hasEmailGateway, setHasEmailGateway] = useState(false);

  const webAuthnSupported = isWebAuthnSupported();
  const availableMethods: MFAMethod[] = ['email', 'authenticator', 'passkey'];

  useEffect(() => {
    loadSettings();
  }, []);

  const loadSettings = async () => {
    try {
      // Check email gateway status
      try {
        const emailConfig = await getEmailConfig();
        setHasEmailGateway(!!emailConfig?.data?.provider_type && emailConfig?.data?.is_active !== false);
      } catch (err) {
        // If we can't fetch email config, assume no gateway
        setHasEmailGateway(false);
      }

      const data = await getAdminMFASettings();
      setSettings(data);

      // Load WebAuthn settings
      try {
        const webauthn = await getWebAuthnSettings();
        setWebauthnSettings(webauthn);
      } catch (err) {
        // WebAuthn settings may not be configured yet
        console.log('WebAuthn settings not configured yet');
      }

      setError(null);
    } catch (err) {
      setError('Failed to load MFA settings');
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);

    try {
      // Ensure email is always included in allowed methods if MFA is required
      const updatedSettings = {
        ...settings,
        allowedMfaMethods: settings.requireMfa
          ? Array.from(new Set([...settings.allowedMfaMethods, 'email']))
          : settings.allowedMfaMethods
      };

      await updateMFASettings(updatedSettings);
      setSuccess(true);
      setSettings(updatedSettings);
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message);
      } else {
        setError('Failed to update MFA settings');
      }
    } finally {
      setSaving(false);
    }
  };

  const handleSaveWebAuthn = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);

    try {
      await updateWebAuthnSettings(webauthnSettings);
      setSuccess(true);
      // Reload to get updated configured status
      const updated = await getWebAuthnSettings();
      setWebauthnSettings(updated);
    } catch (err) {
      if (err instanceof Error) {
        setError(err.message);
      } else {
        setError('Failed to update WebAuthn settings');
      }
    } finally {
      setSaving(false);
    }
  };

  const handleMethodToggle = (method: MFAMethod) => {
    setSettings(prev => {
      const newMethods = prev.allowedMfaMethods.includes(method)
        ? prev.allowedMfaMethods.filter(m => m !== method)
        : [...prev.allowedMfaMethods, method];

      // Ensure email is always included if MFA is required
      if (prev.requireMfa && method !== 'email') {
        if (!newMethods.includes('email')) {
          newMethods.push('email');
        }
      }

      return {
        ...prev,
        allowedMfaMethods: newMethods,
      };
    });
  };

  const handleOriginsChange = (value: string) => {
    // Split by newlines or commas and clean up
    const origins = value
      .split(/[\n,]/)
      .map(o => o.trim())
      .filter(o => o.length > 0);
    setWebauthnSettings(prev => ({ ...prev, rpOrigins: origins }));
  };

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" p={4}>
        <CircularProgress />
      </Box>
    );
  }

  const isPasskeyEnabled = settings.allowedMfaMethods.includes('passkey');
  const isPasskeyConfigured = webauthnSettings.configured && webauthnSettings.rpId && webauthnSettings.rpOrigins.length > 0;

  return (
    <Card>
      <CardContent>
        <Typography variant="h5" gutterBottom>
          Multi-Factor Authentication Settings
        </Typography>

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {success && (
          <Alert severity="success" sx={{ mb: 2 }}>
            Settings updated successfully
          </Alert>
        )}

        <FormGroup>
          <FormControlLabel
            control={
              <Switch
                checked={settings.requireMfa}
                onChange={e => {
                  const newValue = e.target.checked;
                  setSettings(prev => ({
                    ...prev,
                    requireMfa: newValue,
                    // Ensure email is included when enabling required MFA
                    allowedMfaMethods: newValue
                      ? Array.from(new Set([...prev.allowedMfaMethods, 'email']))
                      : prev.allowedMfaMethods
                  }));
                }}
                disabled={!hasEmailGateway}
              />
            }
            label={hasEmailGateway ? "Require MFA for all users" : "Require MFA for all users (Email config required)"}
          />

          <Typography variant="subtitle1" sx={{ mt: 2, mb: 1 }}>
            Allowed MFA Methods
          </Typography>

          {availableMethods.map(method => {
            let label = method.charAt(0).toUpperCase() + method.slice(1);
            let disabled = !settings.requireMfa || (method === 'email' && settings.requireMfa);

            // Special handling for passkey
            if (method === 'passkey') {
              if (!webAuthnSupported) {
                label += ' (Not supported in this browser)';
                disabled = true;
              } else if (!isPasskeyConfigured && !settings.allowedMfaMethods.includes('passkey')) {
                label += ' (Configure WebAuthn settings below first)';
              }
            }

            return (
              <FormControlLabel
                key={method}
                control={
                  <Checkbox
                    checked={settings.allowedMfaMethods.includes(method)}
                    onChange={() => handleMethodToggle(method)}
                    disabled={disabled}
                  />
                }
                label={label}
              />
            );
          })}

          <Box sx={{ mt: 2 }}>
            <TextField
              label="Email Code Validity (minutes)"
              type="number"
              value={settings.emailCodeValidity}
              onChange={e => setSettings(prev => ({ ...prev, emailCodeValidity: parseInt(e.target.value) || 5 }))}
              disabled={!settings.requireMfa || !settings.allowedMfaMethods.includes('email')}
              fullWidth
              margin="normal"
            />

            <TextField
              label="Number of Backup Codes"
              type="number"
              value={settings.backupCodesCount}
              onChange={e => setSettings(prev => ({ ...prev, backupCodesCount: parseInt(e.target.value) || 8 }))}
              disabled={!settings.requireMfa}
              fullWidth
              margin="normal"
            />

            <TextField
              label="Code Cooldown (minutes)"
              type="number"
              value={settings.mfaCodeCooldownMinutes}
              onChange={e => setSettings(prev => ({ ...prev, mfaCodeCooldownMinutes: parseInt(e.target.value) || 1 }))}
              disabled={!settings.requireMfa}
              fullWidth
              margin="normal"
            />

            <TextField
              label="Code Expiry (minutes)"
              type="number"
              value={settings.mfaCodeExpiryMinutes}
              onChange={e => setSettings(prev => ({ ...prev, mfaCodeExpiryMinutes: parseInt(e.target.value) || 5 }))}
              disabled={!settings.requireMfa}
              fullWidth
              margin="normal"
            />

            <TextField
              label="Maximum Code Attempts"
              type="number"
              value={settings.mfaMaxAttempts}
              onChange={e => setSettings(prev => ({ ...prev, mfaMaxAttempts: parseInt(e.target.value) || 3 }))}
              disabled={!settings.requireMfa}
              fullWidth
              margin="normal"
            />
          </Box>

          <Box sx={{ mt: 3 }}>
            <Button
              variant="contained"
              color="primary"
              onClick={handleSave}
              disabled={saving}
              startIcon={saving && <CircularProgress size={20} color="inherit" />}
            >
              {saving ? 'Saving...' : 'Save MFA Settings'}
            </Button>
          </Box>

          {/* WebAuthn/Passkey Configuration Section */}
          <Divider sx={{ my: 4 }} />

          <Typography variant="h6" gutterBottom>
            WebAuthn / Passkey Configuration
          </Typography>

          {!webAuthnSupported && (
            <Alert severity="warning" sx={{ mb: 2 }}>
              WebAuthn is not supported in this browser. Passkey functionality requires a modern browser.
            </Alert>
          )}

          <Alert severity="info" sx={{ mb: 2 }}>
            <Typography variant="body2">
              <strong>Important:</strong> WebAuthn requires a proper domain name (not an IP address) for the Relying Party ID.
              The RP ID should be your domain (e.g., "krakenhashes.example.com") and origins should be the full URLs
              where the application is accessed (e.g., "https://krakenhashes.example.com").
            </Typography>
          </Alert>

          <TextField
            label="Relying Party ID (Domain)"
            value={webauthnSettings.rpId}
            onChange={e => setWebauthnSettings(prev => ({ ...prev, rpId: e.target.value }))}
            fullWidth
            margin="normal"
            placeholder="krakenhashes.example.com"
            helperText="The domain name where your application is hosted (without protocol or port)"
          />

          <TextField
            label="Relying Party Display Name"
            value={webauthnSettings.rpDisplayName}
            onChange={e => setWebauthnSettings(prev => ({ ...prev, rpDisplayName: e.target.value }))}
            fullWidth
            margin="normal"
            placeholder="KrakenHashes"
            helperText="A friendly name shown to users during passkey registration"
          />

          <TextField
            label="Allowed Origins"
            value={webauthnSettings.rpOrigins.join('\n')}
            onChange={e => handleOriginsChange(e.target.value)}
            fullWidth
            margin="normal"
            multiline
            rows={3}
            placeholder="https://krakenhashes.example.com&#10;https://app.krakenhashes.example.com"
            helperText="Full URLs where the application can be accessed (one per line). Must include protocol."
          />

          {isPasskeyConfigured && (
            <Alert severity="success" sx={{ mt: 2 }}>
              WebAuthn is configured. Users can now register and use passkeys.
            </Alert>
          )}

          <Box sx={{ mt: 3 }}>
            <Button
              variant="contained"
              color="primary"
              onClick={handleSaveWebAuthn}
              disabled={saving || !webauthnSettings.rpId || webauthnSettings.rpOrigins.length === 0}
              startIcon={saving && <CircularProgress size={20} color="inherit" />}
            >
              {saving ? 'Saving...' : 'Save WebAuthn Settings'}
            </Button>
          </Box>
        </FormGroup>
      </CardContent>
    </Card>
  );
};

export default MFASettings;
