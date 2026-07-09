import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Alert,
  CircularProgress,
  Grid,
  Tooltip,
  IconButton,
  Switch,
  FormControlLabel,
  FormControl,
  FormLabel,
  RadioGroup,
  Radio,
} from '@mui/material';
import { Info as InfoIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getMaxPriority, updateMaxPriority, getSystemSettings, updateSystemSetting } from '../../services/systemSettings';
import { MaxPriorityConfig, SystemSettingsFormData } from '../../types/systemSettings';

interface SystemSettingsProps {
  onSave?: (settings: SystemSettingsFormData) => Promise<void>;
  loading?: boolean;
}

const SystemSettings: React.FC<SystemSettingsProps> = ({ onSave, loading = false }) => {
  const { t } = useTranslation('admin');
  const [formData, setFormData] = useState<SystemSettingsFormData>({
    max_priority: 1000,
  });
  const [agentSchedulingEnabled, setAgentSchedulingEnabled] = useState(false);
  const [requireClientForHashlist, setRequireClientForHashlist] = useState(false);
  const [hashlistBatchSize, setHashlistBatchSize] = useState<number>(100000);
  const [agentOverflowMode, setAgentOverflowMode] = useState<string>('fifo');
  const [speedTestTimeoutUncompressed, setSpeedTestTimeoutUncompressed] = useState<number>(120);
  const [speedTestTimeoutCompressed, setSpeedTestTimeoutCompressed] = useState<number>(300);
  const [speedTestMinStatusUpdates, setSpeedTestMinStatusUpdates] = useState<number>(3);
  const [loadingData, setLoadingData] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();

  useEffect(() => {
    loadSettings();
  }, []);

  const loadSettings = async () => {
    try {
      setLoadingData(true);
      const data = await getMaxPriority();
      setFormData({
        max_priority: data.max_priority,
      });
      
      // Load general system settings
      try {
        const settings = await getSystemSettings();
        const schedulingSetting = settings.data?.find((s: any) => s.key === 'agent_scheduling_enabled');
        if (schedulingSetting) {
          setAgentSchedulingEnabled(schedulingSetting.value === 'true');
        }
        const requireClientSetting = settings.data?.find((s: any) => s.key === 'require_client_for_hashlist');
        if (requireClientSetting) {
          setRequireClientForHashlist(requireClientSetting.value === 'true');
        }
        const hashlistBatchSizeSetting = settings.data?.find((s: any) => s.key === 'hashlist_bulk_batch_size');
        if (hashlistBatchSizeSetting) {
          setHashlistBatchSize(parseInt(hashlistBatchSizeSetting.value) || 100000);
        }
        const agentOverflowModeSetting = settings.data?.find((s: any) => s.key === 'agent_overflow_allocation_mode');
        if (agentOverflowModeSetting) {
          setAgentOverflowMode(agentOverflowModeSetting.value || 'fifo');
        }
        const speedTestTimeoutUncompressedSetting = settings.data?.find((s: any) => s.key === 'speed_test_timeout_seconds_uncompressed');
        if (speedTestTimeoutUncompressedSetting) {
          setSpeedTestTimeoutUncompressed(parseInt(speedTestTimeoutUncompressedSetting.value) || 120);
        }
        const speedTestTimeoutCompressedSetting = settings.data?.find((s: any) => s.key === 'speed_test_timeout_seconds_compressed');
        if (speedTestTimeoutCompressedSetting) {
          setSpeedTestTimeoutCompressed(parseInt(speedTestTimeoutCompressedSetting.value) || 300);
        }
        const speedTestMinStatusUpdatesSetting = settings.data?.find((s: any) => s.key === 'speed_test_min_status_updates');
        if (speedTestMinStatusUpdatesSetting) {
          setSpeedTestMinStatusUpdates(parseInt(speedTestMinStatusUpdatesSetting.value) || 3);
        }
      } catch (err) {
        console.error('Failed to load general settings:', err);
      }
      
      setError(null);
    } catch (error) {
      console.error('Failed to load system settings:', error);
      setError(t('systemSettings.errors.loadFailed') as string);
    } finally {
      setLoadingData(false);
    }
  };

  const handleSave = async () => {
    if (typeof formData.max_priority === 'string' && formData.max_priority.trim() === '') {
      setError(t('systemSettings.errors.maxPriorityRequired') as string);
      return;
    }

    const maxPriority = typeof formData.max_priority === 'string'
      ? parseInt(formData.max_priority)
      : formData.max_priority;

    if (isNaN(maxPriority) || maxPriority < 1) {
      setError(t('systemSettings.errors.maxPriorityPositive') as string);
      return;
    }

    if (maxPriority > 1000000) {
      setError(t('systemSettings.errors.maxPriorityMax') as string);
      return;
    }

    try {
      setSaving(true);
      setError(null);
      
      if (onSave) {
        await onSave({ max_priority: maxPriority });
      } else {
        await updateMaxPriority(maxPriority);
      }
      
      enqueueSnackbar(t('systemSettings.messages.updateSuccess') as string, { variant: 'success' });
      
      // Reload settings to get the updated values
      await loadSettings();
    } catch (error: any) {
      console.error('Failed to save system settings:', error);
      
      // Handle specific error responses
      if (error.response?.status === 409) {
        const errorData = error.response.data;
        if (errorData.conflicting_jobs) {
          setError(`${errorData.message}\n\nConflicting preset jobs:\n${errorData.conflicting_jobs.join('\n')}`);
        } else {
          setError(errorData.message || 'Cannot update maximum priority due to conflicts');
        }
      } else {
        setError(error.response?.data?.message || error.message || t('systemSettings.errors.saveFailed') as string);
      }
    } finally {
      setSaving(false);
    }
  };

  const handleMaxPriorityChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFormData(prev => ({
      ...prev,
      max_priority: e.target.value,
    }));
    setError(null);
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
        <Alert severity="error" sx={{ mb: 3 }} style={{ whiteSpace: 'pre-line' }}>
          {error}
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  {t('systemSettings.priority.title')}
                </Typography>
                <Tooltip title={t('systemSettings.priority.tooltip') as string}>
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <TextField
                fullWidth
                label={t('systemSettings.priority.maxPriority')}
                type="number"
                value={formData.max_priority}
                onChange={handleMaxPriorityChange}
                onBlur={handleSave}
                disabled={loading || saving}
                inputProps={{
                  min: 1,
                  max: 1000000,
                }}
                helperText={t('systemSettings.priority.helperText')}
              />
            </CardContent>
          </Card>
        </Grid>
        
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" component="h3" gutterBottom>
                {t('systemSettings.priorityInfo.title')}
              </Typography>

              <Typography variant="body2" color="text.secondary" paragraph>
                {t('systemSettings.priorityInfo.description')}
              </Typography>

              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>{t('systemSettings.priorityInfo.currentMax')}:</strong> {typeof formData.max_priority === 'string' ? formData.max_priority : formData.max_priority.toLocaleString()}
              </Typography>

              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>{t('common.note')}:</strong> {t('systemSettings.priorityInfo.note')}
              </Typography>

              <Typography variant="body2" color="text.secondary">
                <strong>{t('systemSettings.priorityInfo.recommended')}</strong>
                <br />• {t('systemSettings.priorityInfo.small')}
                <br />• {t('systemSettings.priorityInfo.medium')}
                <br />• {t('systemSettings.priorityInfo.large')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  {t('systemSettings.agentScheduling.title')}
                </Typography>
                <Tooltip title={t('systemSettings.agentScheduling.tooltip') as string}>
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <FormControlLabel
                control={
                  <Switch
                    checked={agentSchedulingEnabled}
                    onChange={async (e) => {
                      const newValue = e.target.checked;
                      setAgentSchedulingEnabled(newValue);
                      try {
                        await updateSystemSetting('agent_scheduling_enabled', newValue.toString());
                        enqueueSnackbar(t('systemSettings.messages.schedulingUpdated') as string, { variant: 'success' });
                      } catch (error) {
                        console.error('Failed to update scheduling setting:', error);
                        setAgentSchedulingEnabled(!newValue); // Revert on error
                        enqueueSnackbar(t('systemSettings.messages.schedulingFailed') as string, { variant: 'error' });
                      }
                    }}
                    disabled={loading || saving || loadingData}
                  />
                }
                label={t('systemSettings.agentScheduling.enable')}
              />

              <Typography variant="body2" color="text.secondary" paragraph sx={{ mt: 2 }}>
                {t('systemSettings.agentScheduling.description')}
              </Typography>

              <Typography variant="body2" color="text.secondary">
                <strong>{t('common.note')}:</strong> {t('systemSettings.agentScheduling.note')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  {t('systemSettings.agentOverflow.title')}
                </Typography>
                <Tooltip title={t('systemSettings.agentOverflow.tooltip') as string}>
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <FormControl component="fieldset" disabled={loading || saving || loadingData}>
                <FormLabel component="legend" sx={{ mb: 1 }}>
                  {t('systemSettings.agentOverflow.modeLabel')}
                </FormLabel>
                <RadioGroup
                  value={agentOverflowMode}
                  onChange={async (e) => {
                    const newValue = e.target.value;
                    const prevValue = agentOverflowMode;
                    setAgentOverflowMode(newValue);
                    try {
                      await updateSystemSetting('agent_overflow_allocation_mode', newValue);
                      enqueueSnackbar(t('systemSettings.messages.overflowUpdated') as string, { variant: 'success' });
                    } catch (error) {
                      console.error('Failed to update overflow allocation mode:', error);
                      setAgentOverflowMode(prevValue); // Revert on error
                      enqueueSnackbar(t('systemSettings.messages.overflowFailed') as string, { variant: 'error' });
                    }
                  }}
                >
                  <Tooltip title={t('systemSettings.agentOverflow.priorityFifoDescription') as string} placement="right" arrow>
                    <FormControlLabel value="fifo" control={<Radio />} label={t('systemSettings.agentOverflow.priorityFifoMode')} />
                  </Tooltip>
                  <Tooltip title={t('systemSettings.agentOverflow.priorityRoundRobinDescription') as string} placement="right" arrow>
                    <FormControlLabel value="round_robin" control={<Radio />} label={t('systemSettings.agentOverflow.priorityRoundRobinMode')} />
                  </Tooltip>
                  <Tooltip title={t('systemSettings.agentOverflow.enforceMaxAgentsDescription') as string} placement="right" arrow>
                    <FormControlLabel value="enforce_max_agents" control={<Radio />} label={t('systemSettings.agentOverflow.enforceMaxAgentsMode')} />
                  </Tooltip>
                  <Tooltip title={t('systemSettings.agentOverflow.maxAgentsFifoDescription') as string} placement="right" arrow>
                    <FormControlLabel value="max_agents_fifo" control={<Radio />} label={t('systemSettings.agentOverflow.maxAgentsFifoMode')} />
                  </Tooltip>
                  <Tooltip title={t('systemSettings.agentOverflow.maxAgentsRoundRobinDescription') as string} placement="right" arrow>
                    <FormControlLabel value="max_agents_round_robin" control={<Radio />} label={t('systemSettings.agentOverflow.maxAgentsRoundRobinMode')} />
                  </Tooltip>
                </RadioGroup>
              </FormControl>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  {t('systemSettings.hashlist.title')}
                </Typography>
                <Tooltip title={t('systemSettings.hashlist.tooltip') as string}>
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <FormControlLabel
                control={
                  <Switch
                    checked={requireClientForHashlist}
                    onChange={async (e) => {
                      const newValue = e.target.checked;
                      setRequireClientForHashlist(newValue);
                      try {
                        await updateSystemSetting('require_client_for_hashlist', newValue.toString());
                        enqueueSnackbar(t('systemSettings.messages.hashlistClientUpdated') as string, { variant: 'success' });
                      } catch (error) {
                        console.error('Failed to update client requirement setting:', error);
                        setRequireClientForHashlist(!newValue); // Revert on error
                        enqueueSnackbar(t('systemSettings.messages.updateFailed') as string, { variant: 'error' });
                      }
                    }}
                    disabled={loading || saving || loadingData}
                  />
                }
                label={t('systemSettings.hashlist.requireClient')}
              />

              <Typography variant="body2" color="text.secondary" paragraph sx={{ mt: 2 }}>
                {t('systemSettings.hashlist.requireClientDescription')}
              </Typography>

              <TextField
                fullWidth
                label={t('systemSettings.hashlist.batchSize')}
                type="number"
                value={hashlistBatchSize}
                onChange={(e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  setHashlistBatchSize(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  if (newValue < 10000 || newValue > 2000000) {
                    enqueueSnackbar(t('systemSettings.errors.batchSizeRange', { min: '10,000', max: '2,000,000' }) as string, { variant: 'warning' });
                    setHashlistBatchSize(100000);
                    return;
                  }
                  try {
                    await updateSystemSetting('hashlist_bulk_batch_size', newValue.toString());
                    enqueueSnackbar(t('systemSettings.messages.hashlistBatchUpdated') as string, { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update batch size:', error);
                    enqueueSnackbar(t('systemSettings.messages.updateFailed') as string, { variant: 'error' });
                    await loadSettings();
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{
                  min: 10000,
                  max: 2000000,
                  step: 50000,
                }}
                helperText={t('systemSettings.hashlist.batchSizeHelper')}
                sx={{ mt: 2, mb: 2 }}
              />

              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>{t('systemSettings.hashlist.performanceGuide')}</strong>
                <br />• {t('systemSettings.hashlist.performance100k')}
                <br />• {t('systemSettings.hashlist.performance500k')}
                <br />• {t('systemSettings.hashlist.performance1m')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  {t('systemSettings.speedTest.title')}
                </Typography>
                <Tooltip title={t('systemSettings.speedTest.tooltip') as string}>
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <TextField
                fullWidth
                label={t('systemSettings.speedTest.timeoutUncompressed')}
                type="number"
                value={speedTestTimeoutUncompressed}
                onChange={(e) => {
                  const newValue = parseInt(e.target.value) || 120;
                  setSpeedTestTimeoutUncompressed(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 120;
                  if (newValue < 30 || newValue > 3600) {
                    enqueueSnackbar(t('systemSettings.errors.speedTestTimeoutRange', { min: 30, max: 3600 }) as string, { variant: 'warning' });
                    setSpeedTestTimeoutUncompressed(120);
                    return;
                  }
                  try {
                    await updateSystemSetting('speed_test_timeout_seconds_uncompressed', newValue.toString());
                    enqueueSnackbar(t('systemSettings.messages.speedTestTimeoutUpdated') as string, { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update speed-test timeout (uncompressed):', error);
                    enqueueSnackbar(t('systemSettings.messages.updateFailed') as string, { variant: 'error' });
                    await loadSettings();
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{ min: 30, max: 3600 }}
                helperText={t('systemSettings.speedTest.timeoutUncompressedHelper')}
                sx={{ mb: 2 }}
              />

              <TextField
                fullWidth
                label={t('systemSettings.speedTest.timeoutCompressed')}
                type="number"
                value={speedTestTimeoutCompressed}
                onChange={(e) => {
                  const newValue = parseInt(e.target.value) || 300;
                  setSpeedTestTimeoutCompressed(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 300;
                  if (newValue < 30 || newValue > 3600) {
                    enqueueSnackbar(t('systemSettings.errors.speedTestTimeoutRange', { min: 30, max: 3600 }) as string, { variant: 'warning' });
                    setSpeedTestTimeoutCompressed(300);
                    return;
                  }
                  try {
                    await updateSystemSetting('speed_test_timeout_seconds_compressed', newValue.toString());
                    enqueueSnackbar(t('systemSettings.messages.speedTestTimeoutUpdated') as string, { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update speed-test timeout (compressed):', error);
                    enqueueSnackbar(t('systemSettings.messages.updateFailed') as string, { variant: 'error' });
                    await loadSettings();
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{ min: 30, max: 3600 }}
                helperText={t('systemSettings.speedTest.timeoutCompressedHelper')}
                sx={{ mb: 2 }}
              />

              <TextField
                fullWidth
                label={t('systemSettings.speedTest.minStatusUpdates')}
                type="number"
                value={speedTestMinStatusUpdates}
                onChange={(e) => {
                  const newValue = parseInt(e.target.value) || 3;
                  setSpeedTestMinStatusUpdates(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 3;
                  if (newValue < 1 || newValue > 20) {
                    enqueueSnackbar(t('systemSettings.errors.minStatusUpdatesRange', { min: 1, max: 20 }) as string, { variant: 'warning' });
                    setSpeedTestMinStatusUpdates(3);
                    return;
                  }
                  try {
                    await updateSystemSetting('speed_test_min_status_updates', newValue.toString());
                    enqueueSnackbar(t('systemSettings.messages.speedTestMinUpdatesUpdated') as string, { variant: 'success' });
                    if (newValue < 3) {
                      enqueueSnackbar(t('systemSettings.speedTest.minUpdatesLowWarning') as string, { variant: 'warning' });
                    }
                  } catch (error) {
                    console.error('Failed to update speed-test min status updates:', error);
                    enqueueSnackbar(t('systemSettings.messages.updateFailed') as string, { variant: 'error' });
                    await loadSettings();
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{ min: 1, max: 20 }}
                helperText={
                  speedTestMinStatusUpdates < 3
                    ? t('systemSettings.speedTest.minUpdatesLowWarning')
                    : t('systemSettings.speedTest.minStatusUpdatesHelper')
                }
                error={speedTestMinStatusUpdates < 3}
                sx={{ mb: 2 }}
              />

              <Typography variant="body2" color="text.secondary">
                {t('systemSettings.speedTest.description')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
};

export default SystemSettings; 