import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Switch,
  FormControlLabel,
  Alert,
  CircularProgress,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Chip,
  Button,
  TextField,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  Tooltip,
  Divider,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
} from '@mui/material';
import {
  Email as EmailIcon,
  Warning as WarningIcon,
  ExpandMore as ExpandMoreIcon,
  Notifications as NotificationsIcon,
  Webhook as WebhookIcon,
  Add as AddIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  PlayArrow as TestIcon,
  Work as JobIcon,
  Computer as AgentIcon,
  Security as SecurityIcon,
} from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  getNotificationPreferences,
  updateNotificationPreferences,
  getUserWebhooks,
  createUserWebhook,
  updateUserWebhook,
  deleteUserWebhook,
  testUserWebhook,
} from '../../services/notifications';
import type {
  UserNotificationPreferences,
  UserWebhook,
  NotificationType,
  TypeChannelPreference,
  CreateWebhookRequest,
  UpdateWebhookRequest,
  TaskReportMode,
} from '../../types/notifications';

interface NotificationCardProps {
  onNotificationChange?: () => void;
}

// Notification types organized by category
const NOTIFICATION_CATEGORIES: {
  key: string;
  icon: React.ReactNode;
  types: NotificationType[];
}[] = [
  {
    key: 'job',
    icon: <JobIcon />,
    types: ['job_started', 'job_completed', 'job_failed', 'first_crack', 'task_completed_with_cracks'],
  },
  {
    key: 'agent',
    icon: <AgentIcon />,
    types: ['agent_offline', 'agent_error'],
  },
  {
    key: 'security',
    icon: <SecurityIcon />,
    types: ['security_suspicious_login', 'security_mfa_disabled', 'security_password_changed'],
  },
  {
    key: 'system',
    icon: <WebhookIcon />,
    types: ['webhook_failure'],
  },
];

// Mandatory notification types that cannot be disabled
const MANDATORY_TYPES: NotificationType[] = ['security_mfa_disabled', 'security_password_changed'];

// Default webhook form state
const DEFAULT_WEBHOOK_FORM = {
  name: '',
  url: '',
  secret: '',
  is_active: true,
  notification_types: [] as string[],
};

const NotificationCard: React.FC<NotificationCardProps> = ({ onNotificationChange }): JSX.Element => {
  const { t } = useTranslation('notifications');
  const { enqueueSnackbar } = useSnackbar();

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [preferences, setPreferences] = useState<UserNotificationPreferences | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Webhook state
  const [webhooks, setWebhooks] = useState<UserWebhook[]>([]);
  const [webhooksLoading, setWebhooksLoading] = useState(true);
  const [webhookDialogOpen, setWebhookDialogOpen] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<UserWebhook | null>(null);
  const [webhookForm, setWebhookForm] = useState(DEFAULT_WEBHOOK_FORM);
  const [webhookSaving, setWebhookSaving] = useState(false);
  const [webhookTesting, setWebhookTesting] = useState<string | null>(null);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [webhookToDelete, setWebhookToDelete] = useState<UserWebhook | null>(null);

  useEffect(() => {
    loadPreferences();
    loadWebhooks();
  }, []);

  const loadPreferences = async () => {
    try {
      const prefs = await getNotificationPreferences();
      setPreferences(prefs);
      setError(null);
    } catch (err) {
      setError(t('errors.fetchFailed', 'Failed to load notification preferences'));
      console.error('Failed to load notification preferences:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadWebhooks = async () => {
    try {
      const response = await getUserWebhooks();
      setWebhooks(response.webhooks || []);
    } catch (err) {
      console.error('Failed to load webhooks:', err);
    } finally {
      setWebhooksLoading(false);
    }
  };

  const handleToggleChannel = async (
    type: NotificationType,
    channel: 'inAppEnabled' | 'emailEnabled' | 'webhookEnabled',
    enabled: boolean
  ) => {
    if (!preferences) return;

    // Check for mandatory types
    if (MANDATORY_TYPES.includes(type) && channel === 'emailEnabled' && !enabled) {
      enqueueSnackbar(t('settings.mandatory', 'This notification cannot be disabled'), { variant: 'warning' });
      return;
    }

    // Check email configuration
    if (channel === 'emailEnabled' && enabled && !preferences.emailConfigured) {
      enqueueSnackbar(t('settings.emailNotConfigured', 'Email gateway is not configured'), { variant: 'error' });
      return;
    }

    // Check webhook configuration
    if (channel === 'webhookEnabled' && enabled && preferences.webhooksActive === 0) {
      enqueueSnackbar(t('settings.noWebhooksConfigured', 'No active webhooks configured'), { variant: 'warning' });
      return;
    }

    try {
      setSaving(true);

      // Get existing preference values and merge with the change
      const existingPref = preferences.typePreferences[type] || {};
      const updatedPreference: TypeChannelPreference = {
        enabled: true,
        inAppEnabled: existingPref.inAppEnabled ?? true,
        emailEnabled: existingPref.emailEnabled ?? false,
        webhookEnabled: existingPref.webhookEnabled ?? false,
        [channel]: enabled, // Apply the change
      };

      await updateNotificationPreferences({
        typePreferences: {
          [type]: updatedPreference,
        },
      });

      // Update local state
      setPreferences({
        ...preferences,
        typePreferences: {
          ...preferences.typePreferences,
          [type]: {
            ...preferences.typePreferences[type],
            [channel]: enabled,
          },
        },
      });

      onNotificationChange?.();
    } catch (err: any) {
      console.error('Failed to update preference:', err);
      enqueueSnackbar(err.response?.data?.error || t('errors.saveFailed', 'Failed to save'), { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  // Handler for updating task report mode
  const handleTaskReportModeChange = async (mode: TaskReportMode) => {
    if (!preferences) return;

    try {
      setSaving(true);

      const existingPref = preferences.typePreferences['task_completed_with_cracks'] || {};
      const updatedPreference: TypeChannelPreference = {
        enabled: true,
        inAppEnabled: existingPref.inAppEnabled ?? true,
        emailEnabled: existingPref.emailEnabled ?? false,
        webhookEnabled: existingPref.webhookEnabled ?? false,
        settings: { mode },
      };

      await updateNotificationPreferences({
        typePreferences: {
          'task_completed_with_cracks': updatedPreference,
        },
      });

      // Update local state
      setPreferences({
        ...preferences,
        typePreferences: {
          ...preferences.typePreferences,
          'task_completed_with_cracks': {
            ...preferences.typePreferences['task_completed_with_cracks'],
            settings: { mode },
          },
        },
      });

      enqueueSnackbar(t('settings.modeUpdated', 'Task report mode updated'), { variant: 'success' });
      onNotificationChange?.();
    } catch (err: any) {
      console.error('Failed to update task report mode:', err);
      enqueueSnackbar(err.response?.data?.error || t('errors.saveFailed', 'Failed to save'), { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  // Toggle all handler for a channel column within a specific category
  const handleToggleAllChannel = async (
    channel: 'inAppEnabled' | 'emailEnabled' | 'webhookEnabled',
    categoryTypes: NotificationType[]
  ) => {
    if (!preferences) return;

    // Check prerequisites for the channel
    if (channel === 'emailEnabled' && !preferences.emailConfigured) {
      enqueueSnackbar(t('settings.emailNotConfigured', 'Email gateway is not configured'), { variant: 'error' });
      return;
    }

    if (channel === 'webhookEnabled' && preferences.webhooksActive === 0) {
      enqueueSnackbar(t('settings.noWebhooksConfigured', 'No active webhooks configured'), { variant: 'warning' });
      return;
    }

    // Check if all types in this category are currently enabled
    const allEnabled = categoryTypes.every(type => {
      const pref = preferences.typePreferences[type];
      return pref?.[channel] ?? false;
    });

    // Determine new value (toggle)
    const newValue = !allEnabled;

    // Build batch update - collect all types that need to change
    const typePreferencesUpdate: Partial<Record<NotificationType, TypeChannelPreference>> = {};
    const newTypePreferences = { ...preferences.typePreferences };

    for (const type of categoryTypes) {
      // Skip mandatory types when disabling email
      if (!newValue && channel === 'emailEnabled' && MANDATORY_TYPES.includes(type)) {
        continue;
      }

      // Check current value to avoid unnecessary updates
      const existingPref = preferences.typePreferences[type] || {};
      const currentValue = existingPref[channel] ?? false;
      if (currentValue !== newValue) {
        // Send complete preference object (not partial) to preserve other channel values
        const updatedPref: TypeChannelPreference = {
          enabled: true,
          inAppEnabled: existingPref.inAppEnabled ?? true,
          emailEnabled: existingPref.emailEnabled ?? false,
          webhookEnabled: existingPref.webhookEnabled ?? false,
          [channel]: newValue, // Apply the change
        };
        typePreferencesUpdate[type] = updatedPref;
        newTypePreferences[type] = updatedPref;
      }
    }

    // If no changes needed, return early
    if (Object.keys(typePreferencesUpdate).length === 0) {
      return;
    }

    try {
      setSaving(true);

      // Send all updates in a single API call
      await updateNotificationPreferences({
        typePreferences: typePreferencesUpdate,
      });

      // Update local state with all changes at once
      setPreferences({
        ...preferences,
        typePreferences: newTypePreferences,
      });

      onNotificationChange?.();
    } catch (err: any) {
      console.error('Failed to update preferences:', err);
      enqueueSnackbar(err.response?.data?.error || t('errors.saveFailed', 'Failed to save'), { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  // Helper to check if all items in a category's channel are enabled
  const isAllChannelEnabled = (
    channel: 'inAppEnabled' | 'emailEnabled' | 'webhookEnabled',
    categoryTypes: NotificationType[]
  ): boolean => {
    if (!preferences) return false;
    return categoryTypes.every(type => {
      const pref = preferences.typePreferences[type];
      return pref?.[channel] ?? false;
    });
  };

  // Webhook handlers
  const handleOpenWebhookDialog = (webhook?: UserWebhook) => {
    if (webhook) {
      setEditingWebhook(webhook);
      setWebhookForm({
        name: webhook.name,
        url: webhook.url,
        secret: '',
        is_active: webhook.is_active,
        notification_types: webhook.notification_types || [],
      });
    } else {
      setEditingWebhook(null);
      setWebhookForm(DEFAULT_WEBHOOK_FORM);
    }
    setWebhookDialogOpen(true);
  };

  const handleCloseWebhookDialog = () => {
    setWebhookDialogOpen(false);
    setEditingWebhook(null);
    setWebhookForm(DEFAULT_WEBHOOK_FORM);
  };

  const handleSaveWebhook = async () => {
    if (!webhookForm.name || !webhookForm.url) {
      enqueueSnackbar(t('webhooks.validation.nameAndUrl', 'Name and URL are required'), { variant: 'error' });
      return;
    }

    setWebhookSaving(true);
    try {
      if (editingWebhook) {
        const updateRequest: UpdateWebhookRequest = {
          name: webhookForm.name,
          url: webhookForm.url,
          is_active: webhookForm.is_active,
          notification_types: webhookForm.notification_types.length > 0 ? webhookForm.notification_types : undefined,
        };
        if (webhookForm.secret) {
          updateRequest.secret = webhookForm.secret;
        }
        await updateUserWebhook(editingWebhook.id, updateRequest);
        enqueueSnackbar(t('webhooks.updated', 'Webhook updated'), { variant: 'success' });
      } else {
        const createRequest: CreateWebhookRequest = {
          name: webhookForm.name,
          url: webhookForm.url,
          secret: webhookForm.secret || undefined,
          notification_types: webhookForm.notification_types.length > 0 ? webhookForm.notification_types : undefined,
        };
        await createUserWebhook(createRequest);
        enqueueSnackbar(t('webhooks.created', 'Webhook created'), { variant: 'success' });
      }
      handleCloseWebhookDialog();
      loadWebhooks();
      loadPreferences(); // Refresh to update webhook counts
    } catch (err: any) {
      console.error('Failed to save webhook:', err);
      enqueueSnackbar(err.response?.data?.error || t('errors.webhookSaveFailed', 'Failed to save webhook'), { variant: 'error' });
    } finally {
      setWebhookSaving(false);
    }
  };

  const handleTestWebhook = async (webhook: UserWebhook) => {
    setWebhookTesting(webhook.id);
    try {
      const result = await testUserWebhook(webhook.id);
      if (result.success) {
        enqueueSnackbar(t('webhooks.testSuccess', 'Webhook test successful'), { variant: 'success' });
      } else {
        enqueueSnackbar(result.error || t('webhooks.testFailed', 'Webhook test failed'), { variant: 'error' });
      }
    } catch (err: any) {
      console.error('Failed to test webhook:', err);
      enqueueSnackbar(err.response?.data?.error || t('webhooks.testFailed', 'Webhook test failed'), { variant: 'error' });
    } finally {
      setWebhookTesting(null);
    }
  };

  const handleDeleteWebhook = async () => {
    if (!webhookToDelete) return;

    try {
      await deleteUserWebhook(webhookToDelete.id);
      enqueueSnackbar(t('webhooks.deleted', 'Webhook deleted'), { variant: 'success' });
      setDeleteConfirmOpen(false);
      setWebhookToDelete(null);
      loadWebhooks();
      loadPreferences();
    } catch (err: any) {
      console.error('Failed to delete webhook:', err);
      enqueueSnackbar(err.response?.data?.error || t('errors.webhookDeleteFailed', 'Failed to delete webhook'), { variant: 'error' });
    }
  };

  if (loading) {
    return (
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Box display="flex" justifyContent="center" alignItems="center" minHeight={200}>
            <CircularProgress />
          </Box>
        </CardContent>
      </Card>
    );
  }

  if (!preferences) {
    return (
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Alert severity="error">{error || t('errors.fetchFailed', 'Failed to load notification preferences')}</Alert>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom sx={{ display: 'flex', alignItems: 'center' }}>
            <NotificationsIcon sx={{ mr: 1 }} />
            {t('settings.title', 'Notification Settings')}
          </Typography>

          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('settings.description', 'Configure how and when you receive notifications')}
          </Typography>

          {error && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          )}

          {!preferences.emailConfigured && (
            <Alert severity="warning" sx={{ mb: 2 }} icon={<WarningIcon />}>
              {t('settings.emailNotConfiguredWarning', 'Email gateway is not configured. Email notifications are currently unavailable.')}
            </Alert>
          )}

          {/* Notification Type Preferences */}
          {NOTIFICATION_CATEGORIES.map((category) => (
            <Accordion key={category.key} defaultExpanded={category.key === 'job'}>
              <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  {category.icon}
                  <Typography variant="subtitle1">
                    {t(`categories.${category.key}`, category.key)}
                  </Typography>
                </Box>
              </AccordionSummary>
              <AccordionDetails>
                <Table size="small" sx={{ tableLayout: 'fixed' }}>
                  <TableHead>
                    <TableRow>
                      <TableCell sx={{ width: '55%' }}>{t('types.header', 'Notification Type')}</TableCell>
                      <TableCell align="center" sx={{ width: '15%' }}>
                        <Tooltip title={`${t('channels.inApp', 'In-App')} - ${t('settings.toggleAll', 'Toggle all')}`}>
                          <IconButton
                            size="small"
                            onClick={() => handleToggleAllChannel('inAppEnabled', category.types)}
                            disabled={saving}
                            sx={{
                              color: isAllChannelEnabled('inAppEnabled', category.types) ? 'primary.main' : 'action.disabled',
                            }}
                          >
                            <NotificationsIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                      <TableCell align="center" sx={{ width: '15%' }}>
                        <Tooltip title={`${t('channels.email', 'Email')} - ${t('settings.toggleAll', 'Toggle all')}`}>
                          <IconButton
                            size="small"
                            onClick={() => handleToggleAllChannel('emailEnabled', category.types)}
                            disabled={saving || !preferences.emailConfigured}
                            sx={{
                              color: isAllChannelEnabled('emailEnabled', category.types) ? 'primary.main' : 'action.disabled',
                            }}
                          >
                            <EmailIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                      <TableCell align="center" sx={{ width: '15%' }}>
                        <Tooltip title={`${t('channels.webhook', 'Webhook')} - ${t('settings.toggleAll', 'Toggle all')}`}>
                          <IconButton
                            size="small"
                            onClick={() => handleToggleAllChannel('webhookEnabled', category.types)}
                            disabled={saving || preferences.webhooksActive === 0}
                            sx={{
                              color: isAllChannelEnabled('webhookEnabled', category.types) ? 'primary.main' : 'action.disabled',
                            }}
                          >
                            <WebhookIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {category.types.map((type) => {
                      const pref = preferences.typePreferences[type];
                      const isMandatory = MANDATORY_TYPES.includes(type);
                      const isTaskCompletedType = type === 'task_completed_with_cracks';
                      const taskReportMode = (pref?.settings?.mode as TaskReportMode) || 'only_if_cracks';
                      return (
                        <TableRow key={type}>
                          <TableCell>
                            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
                              <Box>
                                <Typography variant="body2">
                                  {t(`types.${type}`, type)}
                                </Typography>
                                <Typography variant="caption" color="text.secondary">
                                  {t(`typeDescriptions.${type}`, '')}
                                </Typography>
                              </Box>
                              {isMandatory && (
                                <Chip
                                  label={t('settings.mandatory', 'Mandatory')}
                                  size="small"
                                  color="warning"
                                />
                              )}
                              {/* Task Report Mode Dropdown - inline, vertically centered */}
                              {isTaskCompletedType && (
                                <FormControl size="small" sx={{ ml: 'auto', minWidth: 180 }}>
                                  <InputLabel id="task-report-mode-label">
                                    {t('settings.taskReportMode', 'When to notify')}
                                  </InputLabel>
                                  <Select
                                    labelId="task-report-mode-label"
                                    value={taskReportMode}
                                    label={t('settings.taskReportMode', 'When to notify')}
                                    onChange={(e) => handleTaskReportModeChange(e.target.value as TaskReportMode)}
                                    disabled={saving}
                                  >
                                    <MenuItem value="only_if_cracks">
                                      {t('settings.taskReportModeOnlyIfCracks', 'Only if cracks found')}
                                    </MenuItem>
                                    <MenuItem value="always">
                                      {t('settings.taskReportModeAlways', 'Always notify')}
                                    </MenuItem>
                                  </Select>
                                </FormControl>
                              )}
                            </Box>
                          </TableCell>
                          <TableCell align="center">
                            <Switch
                              size="small"
                              checked={pref?.inAppEnabled ?? true}
                              onChange={(e) => handleToggleChannel(type, 'inAppEnabled', e.target.checked)}
                              disabled={saving}
                            />
                          </TableCell>
                          <TableCell align="center">
                            <Switch
                              size="small"
                              checked={pref?.emailEnabled ?? false}
                              onChange={(e) => handleToggleChannel(type, 'emailEnabled', e.target.checked)}
                              disabled={saving || !preferences.emailConfigured || (isMandatory && pref?.emailEnabled)}
                            />
                          </TableCell>
                          <TableCell align="center">
                            <Switch
                              size="small"
                              checked={pref?.webhookEnabled ?? false}
                              onChange={(e) => handleToggleChannel(type, 'webhookEnabled', e.target.checked)}
                              disabled={saving || preferences.webhooksActive === 0}
                            />
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </AccordionDetails>
            </Accordion>
          ))}

          <Divider sx={{ my: 3 }} />

          {/* Webhook Configuration */}
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
            <Typography variant="h6" sx={{ display: 'flex', alignItems: 'center' }}>
              <WebhookIcon sx={{ mr: 1 }} />
              {t('webhooks.title', 'Webhook Configuration')}
            </Typography>
            <Button
              variant="outlined"
              startIcon={<AddIcon />}
              onClick={() => handleOpenWebhookDialog()}
              size="small"
            >
              {t('webhooks.create', 'Create Webhook')}
            </Button>
          </Box>

          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('webhooks.description', 'Manage your webhook integrations')}
          </Typography>

          {webhooksLoading ? (
            <CircularProgress size={24} />
          ) : webhooks.length === 0 ? (
            <Alert severity="info">
              {t('webhooks.noWebhooks', 'No webhooks configured. Create one to receive notifications via webhook.')}
            </Alert>
          ) : (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('webhooks.name', 'Name')}</TableCell>
                  <TableCell>{t('webhooks.url', 'URL')}</TableCell>
                  <TableCell>{t('webhooks.isActive', 'Active')}</TableCell>
                  <TableCell>{t('webhooks.totalSent', 'Sent')}</TableCell>
                  <TableCell>{t('webhooks.totalFailed', 'Failed')}</TableCell>
                  <TableCell align="right">{t('common.actions', 'Actions')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {webhooks.map((webhook) => (
                  <TableRow key={webhook.id}>
                    <TableCell>{webhook.name}</TableCell>
                    <TableCell sx={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {webhook.url}
                    </TableCell>
                    <TableCell>
                      <Chip
                        label={webhook.is_active ? t('common.active', 'Active') : t('common.inactive', 'Inactive')}
                        color={webhook.is_active ? 'success' : 'default'}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>{webhook.total_sent}</TableCell>
                    <TableCell>{webhook.total_failed}</TableCell>
                    <TableCell align="right">
                      <Tooltip title={t('webhooks.test', 'Test')}>
                        <IconButton
                          size="small"
                          onClick={() => handleTestWebhook(webhook)}
                          disabled={webhookTesting === webhook.id}
                        >
                          {webhookTesting === webhook.id ? <CircularProgress size={16} /> : <TestIcon />}
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={t('webhooks.edit', 'Edit')}>
                        <IconButton size="small" onClick={() => handleOpenWebhookDialog(webhook)}>
                          <EditIcon />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={t('webhooks.delete', 'Delete')}>
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() => {
                            setWebhookToDelete(webhook);
                            setDeleteConfirmOpen(true);
                          }}
                        >
                          <DeleteIcon />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Webhook Create/Edit Dialog */}
      <Dialog open={webhookDialogOpen} onClose={handleCloseWebhookDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingWebhook ? t('webhooks.edit', 'Edit Webhook') : t('webhooks.create', 'Create Webhook')}
        </DialogTitle>
        <DialogContent>
          <TextField
            fullWidth
            label={t('webhooks.name', 'Name')}
            value={webhookForm.name}
            onChange={(e) => setWebhookForm({ ...webhookForm, name: e.target.value })}
            margin="normal"
            required
          />
          <TextField
            fullWidth
            label={t('webhooks.url', 'URL')}
            value={webhookForm.url}
            onChange={(e) => setWebhookForm({ ...webhookForm, url: e.target.value })}
            margin="normal"
            required
            placeholder="https://example.com/webhook"
          />
          <TextField
            fullWidth
            label={t('webhooks.secret', 'Secret')}
            value={webhookForm.secret}
            onChange={(e) => setWebhookForm({ ...webhookForm, secret: e.target.value })}
            margin="normal"
            type="password"
            helperText={editingWebhook
              ? t('webhooks.secretEditHelp', 'Leave empty to keep existing secret')
              : t('webhooks.secretPlaceholder', 'Optional webhook secret')
            }
          />
          <FormControlLabel
            control={
              <Switch
                checked={webhookForm.is_active}
                onChange={(e) => setWebhookForm({ ...webhookForm, is_active: e.target.checked })}
              />
            }
            label={t('webhooks.isActive', 'Active')}
            sx={{ mt: 1 }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseWebhookDialog}>{t('common.cancel', 'Cancel')}</Button>
          <Button
            variant="contained"
            onClick={handleSaveWebhook}
            disabled={webhookSaving}
          >
            {webhookSaving ? <CircularProgress size={20} /> : t('common.save', 'Save')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteConfirmOpen} onClose={() => setDeleteConfirmOpen(false)}>
        <DialogTitle>{t('webhooks.delete', 'Delete Webhook')}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('webhooks.deleteConfirm', 'Are you sure you want to delete this webhook?')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteConfirmOpen(false)}>{t('common.cancel', 'Cancel')}</Button>
          <Button variant="contained" color="error" onClick={handleDeleteWebhook}>
            {t('common.delete', 'Delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
};

export default NotificationCard;
