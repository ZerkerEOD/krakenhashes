import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  TextField,
  Button,
  Alert,
  CircularProgress,
  Paper,
  FormControlLabel,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  Accordion,
  AccordionSummary,
  AccordionDetails,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import WebhookIcon from '@mui/icons-material/Webhook';
import TimerIcon from '@mui/icons-material/Timer';
import PeopleIcon from '@mui/icons-material/People';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  getGlobalWebhookSettings,
  updateGlobalWebhookSettings,
  testGlobalWebhook,
  getAgentOfflineSettings,
  updateAgentOfflineSettings,
  getAllUserWebhooks,
} from '../../services/notifications';
import type {
  GlobalWebhookSettings,
  AgentOfflineSettings,
  AdminWebhookView,
} from '../../types/notifications';

// Local form state that includes writable secret field
interface GlobalWebhookFormState {
  enabled: boolean;
  url: string;
  secret: string; // For input - not returned from API
  custom_headers: string; // JSON string
  has_secret: boolean; // From API response
}

const NotificationSettings: React.FC = () => {
  const { t } = useTranslation('notifications');
  const { enqueueSnackbar } = useSnackbar();

  // Global webhook state
  const [globalSettings, setGlobalSettings] = useState<GlobalWebhookFormState>({
    enabled: false,
    url: '',
    secret: '',
    custom_headers: '{}',
    has_secret: false,
  });
  const [globalLoading, setGlobalLoading] = useState(true);
  const [globalSaving, setGlobalSaving] = useState(false);
  const [globalTesting, setGlobalTesting] = useState(false);

  // Agent offline buffer state
  const [agentOfflineSettings, setAgentOfflineSettings] = useState<AgentOfflineSettings>({
    buffer_minutes: 10,
  });
  const [agentOfflineLoading, setAgentOfflineLoading] = useState(true);
  const [agentOfflineSaving, setAgentOfflineSaving] = useState(false);

  // User webhooks state (read-only)
  const [userWebhooks, setUserWebhooks] = useState<AdminWebhookView[]>([]);
  const [userWebhooksLoading, setUserWebhooksLoading] = useState(true);

  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchAllSettings();
  }, []);

  const fetchAllSettings = async () => {
    await Promise.all([
      fetchGlobalSettings(),
      fetchAgentOfflineSettings(),
      fetchUserWebhooks(),
    ]);
  };

  const fetchGlobalSettings = async () => {
    setGlobalLoading(true);
    try {
      const settings = await getGlobalWebhookSettings();
      setGlobalSettings({
        enabled: settings.enabled,
        url: settings.url,
        secret: '', // Secret is write-only, never returned from API
        custom_headers: settings.custom_headers || '{}',
        has_secret: settings.has_secret,
      });
    } catch (err) {
      console.error('Failed to fetch global webhook settings:', err);
    } finally {
      setGlobalLoading(false);
    }
  };

  const fetchAgentOfflineSettings = async () => {
    setAgentOfflineLoading(true);
    try {
      const settings = await getAgentOfflineSettings();
      setAgentOfflineSettings(settings);
    } catch (err) {
      console.error('Failed to fetch agent offline settings:', err);
    } finally {
      setAgentOfflineLoading(false);
    }
  };

  const fetchUserWebhooks = async () => {
    setUserWebhooksLoading(true);
    try {
      const response = await getAllUserWebhooks();
      setUserWebhooks(response.webhooks || []);
    } catch (err) {
      console.error('Failed to fetch user webhooks:', err);
    } finally {
      setUserWebhooksLoading(false);
    }
  };

  const handleSaveGlobalSettings = async () => {
    setError(null);

    // Validate JSON
    if (!isValidJson(globalSettings.custom_headers)) {
      setError(t('webhooks.invalidJson', 'Invalid JSON format for custom headers'));
      return;
    }

    setGlobalSaving(true);
    try {
      await updateGlobalWebhookSettings({
        enabled: globalSettings.enabled,
        url: globalSettings.url,
        // Only send secret if user entered a new one
        ...(globalSettings.secret ? { secret: globalSettings.secret } : {}),
        custom_headers: globalSettings.custom_headers,
      });
      // Clear the secret input after successful save
      setGlobalSettings(prev => ({ ...prev, secret: '', has_secret: prev.has_secret || !!globalSettings.secret }));
      enqueueSnackbar(t('admin.globalWebhook.saved', 'Global webhook settings saved'), { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save global webhook settings:', err);
      const message = err.response?.data?.error || t('errors.saveFailed', 'Failed to save settings');
      setError(message);
      enqueueSnackbar(message, { variant: 'error' });
    } finally {
      setGlobalSaving(false);
    }
  };

  const handleTestGlobalWebhook = async () => {
    setGlobalTesting(true);
    try {
      const result = await testGlobalWebhook();
      if (result.success) {
        enqueueSnackbar(t('webhooks.testSuccess', 'Webhook test successful'), { variant: 'success' });
      } else {
        enqueueSnackbar(result.error || t('webhooks.testFailed', 'Webhook test failed'), { variant: 'error' });
      }
    } catch (err: any) {
      console.error('Failed to test global webhook:', err);
      enqueueSnackbar(err.response?.data?.error || t('webhooks.testFailed', 'Webhook test failed'), { variant: 'error' });
    } finally {
      setGlobalTesting(false);
    }
  };

  const handleSaveAgentOfflineSettings = async () => {
    setError(null);
    setAgentOfflineSaving(true);
    try {
      await updateAgentOfflineSettings(agentOfflineSettings.buffer_minutes);
      enqueueSnackbar(t('admin.agentOffline.saved', 'Agent offline settings saved'), { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save agent offline settings:', err);
      const message = err.response?.data?.error || t('errors.saveFailed', 'Failed to save settings');
      setError(message);
      enqueueSnackbar(message, { variant: 'error' });
    } finally {
      setAgentOfflineSaving(false);
    }
  };

  const handleCustomHeadersChange = (value: string) => {
    // Store raw string, validate on save
    setGlobalSettings({ ...globalSettings, custom_headers: value });
  };

  const isValidJson = (str: string): boolean => {
    try {
      JSON.parse(str);
      return true;
    } catch {
      return false;
    }
  };

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('admin.title', 'Notification Settings')}
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {/* Global Webhook Settings */}
      <Accordion defaultExpanded>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <WebhookIcon />
            <Typography variant="subtitle1">
              {t('admin.globalWebhook.title', 'Global Webhook')}
            </Typography>
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('admin.globalWebhook.description', 'Configure a system-wide webhook for all notifications')}
          </Typography>

          {globalLoading ? (
            <CircularProgress size={24} />
          ) : (
            <Box>
              <FormControlLabel
                control={
                  <Switch
                    checked={globalSettings.enabled}
                    onChange={(e) => setGlobalSettings({ ...globalSettings, enabled: e.target.checked })}
                  />
                }
                label={t('admin.globalWebhook.enabled', 'Enable global webhook')}
              />

              <TextField
                fullWidth
                label={t('admin.globalWebhook.url', 'Webhook URL')}
                value={globalSettings.url || ''}
                onChange={(e) => setGlobalSettings({ ...globalSettings, url: e.target.value })}
                margin="normal"
                disabled={!globalSettings.enabled}
              />

              <TextField
                fullWidth
                label={t('admin.globalWebhook.secret', 'Webhook Secret')}
                value={globalSettings.secret}
                onChange={(e) => setGlobalSettings({ ...globalSettings, secret: e.target.value })}
                margin="normal"
                type="password"
                disabled={!globalSettings.enabled}
                placeholder={globalSettings.has_secret ? '••••••••' : ''}
                helperText={globalSettings.has_secret
                  ? t('webhooks.secretExists', 'Secret is set. Leave empty to keep current, or enter new value to replace.')
                  : t('webhooks.secretPlaceholder', 'Optional webhook secret')
                }
              />

              <TextField
                fullWidth
                label={t('admin.globalWebhook.customHeaders', 'Custom Headers (JSON)')}
                value={globalSettings.custom_headers}
                onChange={(e) => handleCustomHeadersChange(e.target.value)}
                margin="normal"
                multiline
                rows={3}
                disabled={!globalSettings.enabled}
                error={!isValidJson(globalSettings.custom_headers)}
                helperText={!isValidJson(globalSettings.custom_headers) ? t('webhooks.invalidJson', 'Invalid JSON format') : ''}
              />

              <Box sx={{ mt: 2, display: 'flex', gap: 2 }}>
                <Button
                  variant="contained"
                  onClick={handleSaveGlobalSettings}
                  disabled={globalSaving}
                >
                  {globalSaving ? <CircularProgress size={24} /> : t('common:save', 'Save')}
                </Button>
                <Button
                  variant="outlined"
                  onClick={handleTestGlobalWebhook}
                  disabled={globalTesting || !globalSettings.enabled || !globalSettings.url}
                >
                  {globalTesting ? <CircularProgress size={24} /> : t('admin.globalWebhook.test', 'Test Global Webhook')}
                </Button>
              </Box>
            </Box>
          )}
        </AccordionDetails>
      </Accordion>

      {/* Agent Offline Buffer Settings */}
      <Accordion defaultExpanded sx={{ mt: 2 }}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <TimerIcon />
            <Typography variant="subtitle1">
              {t('admin.agentOffline.title', 'Agent Offline Buffer')}
            </Typography>
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('admin.agentOffline.description', 'Configure how long to wait before sending agent offline notifications')}
          </Typography>

          {agentOfflineLoading ? (
            <CircularProgress size={24} />
          ) : (
            <Box>
              <TextField
                type="number"
                label={t('admin.agentOffline.bufferMinutes', 'Buffer Minutes')}
                value={agentOfflineSettings.buffer_minutes}
                onChange={(e) => setAgentOfflineSettings({
                  ...agentOfflineSettings,
                  buffer_minutes: Math.max(0, parseInt(e.target.value, 10) || 0),
                })}
                margin="normal"
                helperText={t('admin.agentOffline.bufferHelp', 'Wait this many minutes before notifying about offline agents')}
                InputProps={{ inputProps: { min: 0 } }}
                sx={{ width: 200 }}
              />

              <Box sx={{ mt: 2 }}>
                <Button
                  variant="contained"
                  onClick={handleSaveAgentOfflineSettings}
                  disabled={agentOfflineSaving}
                >
                  {agentOfflineSaving ? <CircularProgress size={24} /> : t('common:save', 'Save')}
                </Button>
              </Box>
            </Box>
          )}
        </AccordionDetails>
      </Accordion>

      {/* User Webhooks Overview */}
      <Accordion sx={{ mt: 2 }}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <PeopleIcon />
            <Typography variant="subtitle1">
              {t('admin.userWebhooks.title', 'User Webhooks')}
            </Typography>
            <Chip label={userWebhooks.length} size="small" sx={{ ml: 1 }} />
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('admin.userWebhooks.description', 'View all user-configured webhooks')}
          </Typography>

          {userWebhooksLoading ? (
            <CircularProgress size={24} />
          ) : userWebhooks.length === 0 ? (
            <Typography color="text.secondary">
              {t('admin.userWebhooks.empty', 'No user webhooks configured')}
            </Typography>
          ) : (
            <TableContainer component={Paper} variant="outlined">
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>{t('admin.userWebhooks.username', 'Username')}</TableCell>
                    <TableCell>{t('admin.userWebhooks.email', 'Email')}</TableCell>
                    <TableCell>{t('webhooks.name', 'Name')}</TableCell>
                    <TableCell>{t('webhooks.url', 'URL')}</TableCell>
                    <TableCell>{t('webhooks.isActive', 'Active')}</TableCell>
                    <TableCell>{t('webhooks.totalSent', 'Total Sent')}</TableCell>
                    <TableCell>{t('webhooks.totalFailed', 'Total Failed')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {userWebhooks.map((webhook) => (
                    <TableRow key={webhook.id}>
                      <TableCell>{webhook.username}</TableCell>
                      <TableCell>{webhook.email}</TableCell>
                      <TableCell>{webhook.name}</TableCell>
                      <TableCell sx={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                        {webhook.url}
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={webhook.is_active ? t('common:active', 'Active') : t('common:inactive', 'Inactive')}
                          color={webhook.is_active ? 'success' : 'default'}
                          size="small"
                        />
                      </TableCell>
                      <TableCell>{webhook.total_sent}</TableCell>
                      <TableCell>{webhook.total_failed}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </AccordionDetails>
      </Accordion>
    </Box>
  );
};

export default NotificationSettings;
