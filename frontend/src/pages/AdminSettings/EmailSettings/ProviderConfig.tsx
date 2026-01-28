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
import { useTranslation } from 'react-i18next';
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
  // Mailgun-specific fields
  region?: 'us' | 'eu';
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
  const { t } = useTranslation('admin');
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
          transformedConfig.region = ac.region || 'us';
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
        onNotification(t('emailSettings.provider.messages.loadFailed'), 'error');
      }
      // No config exists, stay in create mode
      setSavedConfig(null);
      setConfig(defaultConfig);
      setMode('create');
    } finally {
      setLoading(false);
    }
  }, [onNotification, t]);

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
      onNotification(t('emailSettings.provider.messages.testSuccess'), 'success');
      setTestEmailOpen(false);
      setTestEmail('');
    } catch (error) {
      console.error('[ProviderConfig] Failed to send test email:', error);
      onNotification(`Error: ${error instanceof Error ? error.message : t('emailSettings.provider.messages.testFailed')}`, 'error');
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
      additionalConfig.region = config.region || 'us';
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
      onNotification(t('emailSettings.provider.labels.fromEmail') + ' is required', 'error');
      return;
    }

    if (mode === 'create' && !config.apiKey) {
      onNotification(t('emailSettings.provider.labels.apiKey') + '/' + t('emailSettings.provider.labels.password') + ' is required for new configuration', 'error');
      return;
    }

    if (config.provider === 'smtp') {
      if (!config.host) {
        onNotification(t('emailSettings.provider.labels.smtpHost') + ' is required', 'error');
        return;
      }
      if (!config.username) {
        onNotification(t('emailSettings.provider.labels.username') + ' is required', 'error');
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
      onNotification(t('emailSettings.provider.messages.saveSuccess'), 'success');

      // Clear sessionStorage and reload config to switch to view mode
      sessionStorage.removeItem('email-config-editing');
      await loadConfig();

      if (withTest) {
        setTestEmailOpen(true);
      }
    } catch (error) {
      console.error('[ProviderConfig] Failed to save configuration:', error);
      onNotification(`Error: ${error instanceof Error ? error.message : t('emailSettings.provider.messages.saveFailed')}`, 'error');
    } finally {
      setLoading(false);
      setSaveWithTestOpen(false);
    }
  };

  const renderViewMode = () => (
    <Paper elevation={2} sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="h6">{t('emailSettings.provider.titles.current')}</Typography>
        <Button
          variant="outlined"
          startIcon={<EditIcon />}
          onClick={handleEdit}
        >
          {t('emailSettings.provider.buttons.edit')}
        </Button>
      </Box>
      <Divider sx={{ mb: 2 }} />
      <Grid container spacing={2}>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.provider')}</Typography>
          <Typography variant="body1" sx={{ textTransform: 'capitalize' }}>
            {config.provider === 'smtp' ? t('emailSettings.provider.menuItems.smtp') : config.provider === 'sendgrid' ? t('emailSettings.provider.menuItems.sendgrid') : t('emailSettings.provider.menuItems.mailgun')}
          </Typography>
        </Grid>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.apiKey')} / {t('emailSettings.provider.labels.password')}</Typography>
          <Typography variant="body1">••••••••••••</Typography>
        </Grid>
        {config.provider === 'mailgun' && config.domain && (
          <>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.domain')}</Typography>
              <Typography variant="body1">{config.domain}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.region')}</Typography>
              <Typography variant="body1" sx={{ textTransform: 'uppercase' }}>{config.region || 'us'}</Typography>
            </Grid>
          </>
        )}
        {config.provider === 'smtp' && (
          <>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.smtpHost')}</Typography>
              <Typography variant="body1">{config.host}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.port')}</Typography>
              <Typography variant="body1">{config.port}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.username')}</Typography>
              <Typography variant="body1">{config.username}</Typography>
            </Grid>
            <Grid item xs={12} md={6}>
              <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.encryption')}</Typography>
              <Typography variant="body1" sx={{ textTransform: 'uppercase' }}>{config.encryption}</Typography>
            </Grid>
            {config.skipTLSVerify && (
              <Grid item xs={12}>
                <Alert severity="warning">{t('emailSettings.provider.warnings.tlsVerifyDisabled')}</Alert>
              </Grid>
            )}
          </>
        )}
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.fromEmail')}</Typography>
          <Typography variant="body1">{config.fromEmail}</Typography>
        </Grid>
        <Grid item xs={12} md={6}>
          <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.fromName')}</Typography>
          <Typography variant="body1">{config.fromName}</Typography>
        </Grid>
        {config.monthlyLimit && (
          <Grid item xs={12} md={6}>
            <Typography variant="body2" color="text.secondary">{t('emailSettings.provider.labels.monthlyLimit')}</Typography>
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
          {t('emailSettings.provider.buttons.testConnection')}
        </Button>
      </Box>
    </Paper>
  );

  const renderFormMode = () => (
    <Box>
      <Typography variant="h6" gutterBottom>
        {mode === 'create' ? t('emailSettings.provider.titles.create') : t('emailSettings.provider.titles.edit')}
      </Typography>

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <FormControl fullWidth>
            <InputLabel>{t('emailSettings.provider.labels.provider')}</InputLabel>
            <Select
              value={config.provider}
              label={t('emailSettings.provider.labels.provider')}
              onChange={handleChange('provider')}
            >
              <MenuItem value="sendgrid">{t('emailSettings.provider.menuItems.sendgrid')}</MenuItem>
              <MenuItem value="mailgun">{t('emailSettings.provider.menuItems.mailgun')}</MenuItem>
              <MenuItem value="smtp">{t('emailSettings.provider.menuItems.smtp')}</MenuItem>
            </Select>
          </FormControl>
        </Grid>

        <Grid item xs={12} md={6}>
          <TextField
            fullWidth
            label={config.provider === 'smtp' ? t('emailSettings.provider.labels.password') : t('emailSettings.provider.labels.apiKey')}
            type="password"
            value={config.apiKey}
            onChange={handleChange('apiKey')}
            placeholder={mode === 'edit' ? t('emailSettings.provider.helpers.keepCurrentPassword') : ''}
            helperText={mode === 'edit' ? t('emailSettings.provider.helpers.keepCurrentPassword') : ''}
          />
        </Grid>

        {config.provider === 'smtp' && (
          <>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label={t('emailSettings.provider.labels.smtpHost')}
                value={config.host || ''}
                onChange={handleChange('host')}
                placeholder={t('emailSettings.provider.placeholders.smtpHost')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label={t('emailSettings.provider.labels.port')}
                type="number"
                value={config.port || ''}
                onChange={handleChange('port')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label={t('emailSettings.provider.labels.username')}
                value={config.username || ''}
                onChange={handleChange('username')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <FormControl fullWidth required>
                <InputLabel>{t('emailSettings.provider.labels.encryption')}</InputLabel>
                <Select
                  value={config.encryption || 'starttls'}
                  label={t('emailSettings.provider.labels.encryption')}
                  onChange={handleChange('encryption')}
                >
                  <MenuItem value="none">{t('emailSettings.provider.menuItems.encryptionNone')}</MenuItem>
                  <MenuItem value="starttls">{t('emailSettings.provider.menuItems.encryptionStarttls')}</MenuItem>
                  <MenuItem value="tls">{t('emailSettings.provider.menuItems.encryptionTls')}</MenuItem>
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
                label={t('emailSettings.provider.labels.skipTlsVerify')}
              />
              {config.skipTLSVerify && (
                <Alert severity="warning" sx={{ mt: 1 }}>
                  {t('emailSettings.provider.warnings.tlsVerifyWarning')}
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
                label={t('emailSettings.provider.labels.fromEmail')}
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label={t('emailSettings.provider.labels.fromName')}
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
                label={t('emailSettings.provider.labels.domain')}
                value={config.domain || ''}
                onChange={handleChange('domain')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <FormControl fullWidth>
                <InputLabel>{t('emailSettings.provider.labels.region')}</InputLabel>
                <Select
                  value={config.region || 'us'}
                  label={t('emailSettings.provider.labels.region')}
                  onChange={handleChange('region')}
                >
                  <MenuItem value="us">{t('emailSettings.provider.menuItems.regionUS')}</MenuItem>
                  <MenuItem value="eu">{t('emailSettings.provider.menuItems.regionEU')}</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                required
                fullWidth
                label={t('emailSettings.provider.labels.fromEmail')}
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
                helperText={t('emailSettings.provider.helpers.mailgunFromEmail')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label={t('emailSettings.provider.labels.fromName')}
                value={config.fromName || ''}
                onChange={handleChange('fromName')}
                helperText={t('emailSettings.provider.helpers.fromName')}
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
                label={t('emailSettings.provider.labels.fromEmail')}
                type="email"
                value={config.fromEmail || ''}
                onChange={handleChange('fromEmail')}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label={t('emailSettings.provider.labels.fromName')}
                value={config.fromName || ''}
                onChange={handleChange('fromName')}
              />
            </Grid>
          </>
        )}

        <Grid item xs={12} md={6}>
          <TextField
            fullWidth
            label={t('emailSettings.provider.labels.monthlyLimit')}
            type="number"
            value={config.monthlyLimit || ''}
            onChange={handleChange('monthlyLimit')}
            helperText={t('emailSettings.provider.helpers.monthlyLimit')}
          />
        </Grid>

        <Grid item xs={12}>
          <Box sx={{ display: 'flex', gap: 2, justifyContent: 'flex-end' }}>
            <Button
              variant="outlined"
              onClick={handleCancel}
              disabled={loading}
            >
              {t('emailSettings.provider.buttons.cancel')}
            </Button>
            <Button
              variant="outlined"
              onClick={() => setTestEmailOpen(true)}
              disabled={loading}
            >
              {t('emailSettings.provider.buttons.testConnection')}
            </Button>
            <LoadingButton
              variant="contained"
              onClick={() => setSaveWithTestOpen(true)}
              loading={loading}
            >
              {t('emailSettings.provider.buttons.save')}
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
        <DialogTitle>{t('emailSettings.provider.dialogs.test.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('emailSettings.provider.dialogs.test.content')}
          </DialogContentText>
          <TextField
            autoFocus
            margin="dense"
            label={t('emailSettings.provider.placeholders.testEmailAddress')}
            type="email"
            fullWidth
            variant="outlined"
            value={testEmail}
            onChange={(e) => setTestEmail(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setTestEmailOpen(false)}>{t('emailSettings.provider.buttons.cancel')}</Button>
          <Button
            onClick={() => handleTest(testEmail)}
            disabled={!testEmail || loading}
          >
            {t('emailSettings.provider.buttons.sendTestEmail')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Save with Test Dialog */}
      <Dialog open={saveWithTestOpen} onClose={() => setSaveWithTestOpen(false)}>
        <DialogTitle>{t('emailSettings.provider.dialogs.save.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('emailSettings.provider.dialogs.save.content')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setSaveWithTestOpen(false)}>{t('emailSettings.provider.buttons.cancel')}</Button>
          <Button onClick={() => handleSave(false)}>{t('emailSettings.provider.buttons.saveOnly')}</Button>
          <Button onClick={() => handleSave(true)}>{t('emailSettings.provider.buttons.saveAndTest')}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};
