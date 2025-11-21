import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Button,
  Grid,
  Typography,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
  Paper,
  Divider,
  Checkbox,
  FormControlLabel,
  Alert,
} from '@mui/material';
import { LoadingButton } from '@mui/lab';
import { SelectChangeEvent } from '@mui/material/Select';
import EditIcon from '@mui/icons-material/Edit';
import { getEmailConfig, updateEmailConfig, testEmailConfig } from '../../../services/api';

interface ProviderConfigProps {
  onNotification: (message: string, severity: 'success' | 'error') => void;
}

interface EmailProviderConfig {
  id?: number;
  provider: 'sendgrid' | 'mailgun' | 'smtp';
  apiKey: string;
  // SendGrid & Mailgun fields
  fromEmail?: string;
  fromName?: string;
  domain?: string;
  // SMTP-specific fields
  host?: string;
  port?: number;
  username?: string;
  encryption?: 'none' | 'starttls' | 'tls';
  skipTLSVerify?: boolean;
  // Common
  monthlyLimit?: number;
}

const defaultConfig: EmailProviderConfig = {
  provider: 'sendgrid',
  apiKey: '',
  fromEmail: '',
  fromName: '',
  monthlyLimit: undefined,
};

type ViewMode = 'view' | 'edit' | 'create';

export const ProviderConfig: React.FC<ProviderConfigProps> = ({ onNotification }) => {
  const [loading, setLoading] = useState(false);
  const [mode, setMode] = useState<ViewMode>('create');
  const [savedConfig, setSavedConfig] = useState<EmailProviderConfig | null>(null);
  const [config, setConfig] = useState<EmailProviderConfig>(defaultConfig);
  const [testEmailOpen, setTestEmailOpen] = useState(false);
  const [testEmail, setTestEmail] = useState('');
  const [saveWithTestOpen, setSaveWithTestOpen] = useState(false);

  const loadConfig = useCallback(async () => {
    try {
      console.debug('[ProviderConfig] Loading configuration...');
      setLoading(true);

      // Check for unsaved edits in sessionStorage first
      const savedEditState = sessionStorage.getItem('email-config-editing');
      if (savedEditState) {
        try {
          const { config: savedCfg, mode: savedMode, savedConfig: savedSaved } = JSON.parse(savedEditState);
          console.debug('[ProviderConfig] Restoring unsaved edits from sessionStorage');
          setConfig(savedCfg);
          setMode(savedMode);
          setSavedConfig(savedSaved);
          setLoading(false);
          return; // Don't reload from API
        } catch (e) {
          console.error('[ProviderConfig] Failed to parse saved edit state:', e);
          sessionStorage.removeItem('email-config-editing');
        }
      }

      // Load from API
      const response = await getEmailConfig();
      const backendConfig = response.data;
      console.debug('[ProviderConfig] Loaded configuration:', backendConfig);

      // Transform backend format to frontend format
      const transformedConfig: EmailProviderConfig = {
        id: backendConfig.id,
        provider: backendConfig.provider_type,
        apiKey: backendConfig.api_key,
        monthlyLimit: backendConfig.monthly_limit,
      };

      // Parse additional_config into flat structure
      if (backendConfig.additional_config) {
        const ac = backendConfig.additional_config;

        if (backendConfig.provider_type === 'smtp') {
          transformedConfig.host = ac.host;
          transformedConfig.port = ac.port;
          transformedConfig.username = ac.username;
          transformedConfig.fromEmail = ac.from_email;
          transformedConfig.fromName = ac.from_name;
          transformedConfig.encryption = ac.encryption;
          transformedConfig.skipTLSVerify = ac.skip_tls_verify || false;
        } else if (backendConfig.provider_type === 'mailgun') {
          transformedConfig.domain = ac.domain;
          transformedConfig.fromEmail = ac.from_email;
          transformedConfig.fromName = ac.from_name;
        } else if (backendConfig.provider_type === 'sendgrid') {
          transformedConfig.fromEmail = ac.from_email;
          transformedConfig.fromName = ac.from_name;
        }
      }

      console.debug('[ProviderConfig] Transformed configuration:', transformedConfig);
      setSavedConfig(transformedConfig);
      setConfig(transformedConfig);
      setMode('view');
    } catch (error) {
      console.error('[ProviderConfig] Failed to load configuration:', error);
      // 404 is expected for new setup
      if ((error as any).response?.status !== 404) {
        onNotification('Failed to load configuration', 'error');
      }
      // No config exists, stay in create mode
      setSavedConfig(null);
      setConfig(defaultConfig);
      setMode('create');
    } finally {
      setLoading(false);
    }
  }, [onNotification]);

  // Load config on mount
  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  // Persist editing state to sessionStorage when in edit/create mode
  useEffect(() => {
    if (mode === 'edit' || mode === 'create') {
      const stateToSave = {
        config,
        mode,
        savedConfig,
      };
      sessionStorage.setItem('email-config-editing', JSON.stringify(stateToSave));
    } else if (mode === 'view') {
      sessionStorage.removeItem('email-config-editing');
    }
  }, [config, mode, savedConfig]);

  const handleChange = (field: keyof EmailProviderConfig) => (
    event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement> | SelectChangeEvent
  ) => {
    const value = event.target.value;
    setConfig(prev => {
      const newConfig = {
        ...prev,
        [field]: field === 'monthlyLimit' || field === 'port'
          ? (value === '' ? undefined : Number(value))
          : value,
      };

      // SMTP: Auto-populate port based on encryption if not manually set
      if (field === 'encryption' && newConfig.provider === 'smtp') {
        switch (value) {
          case 'none':
            newConfig.port = 25;
            break;
          case 'starttls':
            newConfig.port = 587;
            break;
          case 'tls':
            newConfig.port = 465;
            break;
        }
      }

      // Set default fromEmail for Mailgun when domain changes
      if (field === 'domain' && newConfig.provider === 'mailgun' && (!prev.fromEmail || prev.fromEmail === `noreply@${prev.domain}`)) {
        newConfig.fromEmail = `noreply@${value}`;
      }

      // Set defaults when switching to Mailgun
      if (field === 'provider' && value === 'mailgun') {
        if (newConfig.domain && (!newConfig.fromEmail || newConfig.fromEmail === '')) {
          newConfig.fromEmail = `noreply@${newConfig.domain}`;
        }
        if (!newConfig.fromName) {
          newConfig.fromName = 'KrakenHashes';
        }
      }

      // Set defaults when switching to SMTP
      if (field === 'provider' && value === 'smtp') {
        if (!newConfig.encryption) {
          newConfig.encryption = 'starttls';
          newConfig.port = 587;
        }
        if (!newConfig.fromName) {
          newConfig.fromName = 'KrakenHashes';
        }
      }

      return newConfig;
    });
  };

  const handleCheckboxChange = (field: keyof EmailProviderConfig) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    setConfig(prev => ({
      ...prev,
      [field]: event.target.checked,
    }));
  };

  const handleEdit = () => {
    // Enter edit mode with current saved config
    setConfig({ ...savedConfig!, apiKey: '' }); // Clear password for security
    setMode('edit');
  };

  const handleCancel = () => {
    console.debug('[ProviderConfig] Canceling configuration');
    sessionStorage.removeItem('email-config-editing');
    if (savedConfig) {
      setConfig(savedConfig);
      setMode('view');
    } else {
      setConfig(defaultConfig);
      setMode('create');
    }
  };

  const handleTest = async (email: string) => {
    setLoading(true);
    try {
      if (mode === 'view') {
        // Testing saved config
        const payload = {
          test_email: email,
          test_only: true
        };
        await testEmailConfig(payload);
      } else {
        // Testing form config
        const payload = {
          test_email: email,
          test_only: true,
          config: buildConfigPayload()
        };
        await testEmailConfig(payload);
      }
      onNotification('Test email sent successfully', 'success');
      setTestEmailOpen(false);
      setTestEmail('');
    } catch (error) {
      console.error('[ProviderConfig] Failed to send test email:', error);
      onNotification(`Error: ${error instanceof Error ? error.message : 'Failed to send test email'}`, 'error');
    } finally {
      setLoading(false);
    }
  };

  const buildConfigPayload = () => {
    const additionalConfig: any = {};

    if (config.provider === 'sendgrid') {
      additionalConfig.from_email = config.fromEmail;
      additionalConfig.from_name = config.fromName;
    } else if (config.provider === 'mailgun') {
      additionalConfig.domain = config.domain;
      additionalConfig.from_email = config.fromEmail;
      additionalConfig.from_name = config.fromName;
    } else if (config.provider === 'smtp') {
      additionalConfig.host = config.host;
      additionalConfig.port = config.port;
      additionalConfig.username = config.username;
      additionalConfig.from_email = config.fromEmail;
      additionalConfig.from_name = config.fromName;
      additionalConfig.encryption = config.encryption;
      if (config.skipTLSVerify) {
        additionalConfig.skip_tls_verify = config.skipTLSVerify;
      }
    }

    return {
      provider_type: config.provider,
      api_key: config.apiKey || '', // Empty string signals backend to preserve existing
      additional_config: additionalConfig,
      monthly_limit: config.monthlyLimit,
      is_active: true,
    };
  };

  const handleSave = async (withTest: boolean = false) => {
    // Validation
    if (!config.fromEmail) {
      onNotification('From Email is required', 'error');
      return;
    }

    if (mode === 'create' && !config.apiKey) {
      onNotification('API Key/Password is required for new configuration', 'error');
      return;
    }

    if (config.provider === 'smtp') {
      if (!config.host) {
        onNotification('SMTP Host is required', 'error');
        return;
      }
      if (!config.username) {
        onNotification('SMTP Username is required', 'error');
        return;
      }
    }

    setLoading(true);
    try {
      const payload = {
        config: buildConfigPayload(),
      };

      console.debug('[ProviderConfig] Saving configuration with payload:', payload);

      await updateEmailConfig(payload);
      onNotification('Configuration saved successfully', 'success');

      // Clear sessionStorage and reload config to switch to view mode
      sessionStorage.removeItem('email-config-editing');
      await loadConfig();

      if (withTest) {
        setTestEmailOpen(true);
      }
    } catch (error) {
      console.error('[ProviderConfig] Failed to save configuration:', error);
      onNotification(`Error: ${error instanceof Error ? error.message : 'Failed to save configuration'}`, 'error');
    } finally {
      setLoading(false);
      setSaveWithTestOpen(false);
    }
  };

  const renderViewMode = () => (
    <Paper elevation={2} sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="h6">Current Email Configuration</Typography>
        <Button
          variant="outlined"
          startIcon={<EditIcon />}
          onClick={handleEdit}
        >
          Edit Configuration
        </Button>
      </Box>
      <Divider sx={{ mb: 2 }} />
      <Grid container spacing={2}>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">Provider</Typography>
          <Typography variant="body1" sx={{ textTransform: 'capitalize' }}>
            {config.provider === 'smtp' ? 'SMTP' : config.provider}
          </Typography>
        </Grid>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">API Key / Password</Typography>
          <Typography variant="body1">••••••••••••</Typography>
        </Grid>
        {config.provider === 'mailgun' && config.domain && (
          <Grid item xs={12} md={6}>
            <Typography variant="body2" color="text.secondary">Domain</Typography>
            <Typography variant="body1">{config.domain}</Typography>
          </Grid>
        )}
        {config.provider === 'smtp' && (
          <>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">Host</Typography>
              <Typography variant="body1">{config.host}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">Port</Typography>
              <Typography variant="body1">{config.port}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">Username</Typography>
              <Typography variant="body1">{config.username}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">Encryption</Typography>
              <Typography variant="body1" sx={{ textTransform: 'uppercase' }}>{config.encryption}</Typography>
            </Grid>
            {config.skipTLSVerify && (
              <Grid item xs={12}>
                <Alert severity="warning">TLS certificate verification is disabled</Alert>
              </Grid>
            )}
          </>
        )}
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">From Email</Typography>
          <Typography variant="body1">{config.fromEmail}</Typography>
        </Grid>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">From Name</Typography>
          <Typography variant="body1">{config.fromName}</Typography>
        </Grid>
        {config.monthlyLimit && (
          <Grid item xs={12} md={6}>
            <Typography variant="body2" color="text.secondary">Monthly Limit</Typography>
            <Typography variant="body1">{config.monthlyLimit} emails</Typography>
          </Grid>
        )}
      </Grid>
      <Box sx={{ mt: 3, display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          variant="outlined"
          onClick={() => setTestEmailOpen(true)}
          disabled={loading}
        >
          Test Connection
        </Button>
      </Box>
    </Paper>
  );

  const renderFormMode = () => (
    <Box>
      <Typography variant="h6" gutterBottom>
        {mode === 'create' ? 'Create Email Configuration' : 'Edit Email Configuration'}
      </Typography>

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <FormControl fullWidth>
            <InputLabel>Provider</InputLabel>
            <Select
              value={config.provider}
              label="Provider"
              onChange={handleChange('provider')}
            >
              <MenuItem value="sendgrid">SendGrid</MenuItem>
              <MenuItem value="mailgun">Mailgun</MenuItem>
              <MenuItem value="smtp">SMTP</MenuItem>
            </Select>
          </FormControl>
        </Grid>

        <Grid item xs={12} md={6}>
          <TextField
            fullWidth
            label={config.provider === 'smtp' ? 'Password' : 'API Key'}
            type="password"
            value={config.apiKey}
            onChange={handleChange('apiKey')}
            placeholder={mode === 'edit' ? 'Leave empty to keep current' : ''}
            helperText={mode === 'edit' ? 'Leave empty to keep current password/key' : ''}
          />
        </Grid>

        {config.provider === 'smtp' && (
          <>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="SMTP Host"
                value={config.host || ''}
                onChange={handleChange('host')}
                placeholder="smtp.example.com"
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="Port"
                type="number"
                value={config.port || ''}
                onChange={handleChange('port')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="Username"
                value={config.username || ''}
                onChange={handleChange('username')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <FormControl fullWidth required>
                <InputLabel>Encryption</InputLabel>
                <Select
                  value={config.encryption || 'starttls'}
                  label="Encryption"
                  onChange={handleChange('encryption')}
                >
                  <MenuItem value="none">None (Port 25)</MenuItem>
                  <MenuItem value="starttls">STARTTLS (Port 587)</MenuItem>
                  <MenuItem value="tls">TLS/SSL (Port 465)</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12}>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={config.skipTLSVerify || false}
                    onChange={handleCheckboxChange('skipTLSVerify')}
                  />
                }
                label="Skip TLS Certificate Verification (insecure, not recommended)"
              />
              {config.skipTLSVerify && (
                <Alert severity="warning" sx={{ mt: 1 }}>
                  Disabling TLS verification makes your connection vulnerable to man-in-the-middle attacks.
                  Only use this for testing with self-signed certificates.
                </Alert>
              )}
            </Grid>
          </>
        )}

        {config.provider === 'sendgrid' && (
          <>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="From Email"
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="From Name"
                value={config.fromName || ''}
                onChange={handleChange('fromName')}
              />
            </Grid>
          </>
        )}

        {config.provider === 'mailgun' && (
          <>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="Domain"
                value={config.domain || ''}
                onChange={handleChange('domain')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="From Email"
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
                helperText="Usually noreply@yourdomain"
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="From Name"
                value={config.fromName || ''}
                onChange={handleChange('fromName')}
                helperText="Display name for emails"
              />
            </Grid>
          </>
        )}

        {config.provider === 'smtp' && (
          <>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label="From Email"
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="From Name"
                value={config.fromName || ''}
                onChange={handleChange('fromName')}
              />
            </Grid>
          </>
        )}

        <Grid item xs={12} md={6}>
          <TextField
            fullWidth
            label="Monthly Limit"
            type="number"
            value={config.monthlyLimit || ''}
            onChange={handleChange('monthlyLimit')}
            helperText="Leave empty for unlimited"
          />
        </Grid>

        <Grid item xs={12}>
          <Box sx={{ display: 'flex', gap: 2, justifyContent: 'flex-end' }}>
            <Button
              variant="outlined"
              onClick={handleCancel}
              disabled={loading}
            >
              Cancel
            </Button>
            <Button
              variant="outlined"
              onClick={() => setTestEmailOpen(true)}
              disabled={loading}
            >
              Test Connection
            </Button>
            <LoadingButton
              variant="contained"
              onClick={() => setSaveWithTestOpen(true)}
              loading={loading}
            >
              Save Configuration
            </LoadingButton>
          </Box>
        </Grid>
      </Grid>
    </Box>
  );

  return (
    <Box>
      {mode === 'view' ? renderViewMode() : renderFormMode()}

      {/* Test Email Dialog */}
      <Dialog open={testEmailOpen} onClose={() => setTestEmailOpen(false)}>
        <DialogTitle>Test Email Configuration</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Enter an email address to send a test email to:
          </DialogContentText>
          <TextField
            autoFocus
            margin="dense"
            label="Test Email Address"
            type="email"
            fullWidth
            variant="outlined"
            value={testEmail}
            onChange={(e) => setTestEmail(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setTestEmailOpen(false)}>Cancel</Button>
          <Button
            onClick={() => handleTest(testEmail)}
            disabled={!testEmail || loading}
          >
            Send Test Email
          </Button>
        </DialogActions>
      </Dialog>

      {/* Save with Test Dialog */}
      <Dialog open={saveWithTestOpen} onClose={() => setSaveWithTestOpen(false)}>
        <DialogTitle>Save Configuration</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Would you like to test the configuration after saving?
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setSaveWithTestOpen(false)}>Cancel</Button>
          <Button onClick={() => handleSave(false)}>Save Only</Button>
          <Button onClick={() => handleSave(true)}>Save and Test</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};
