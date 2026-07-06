import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  Box,
  Typography,
  TextField,
  Alert,
  CircularProgress,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormHelperText,
  Paper,
  Divider,
  FormControlLabel,
  Switch,
} from '@mui/material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getMonitoringSettings, updateMonitoringSettings, MonitoringSettings as MonitoringSettingsData } from '../../services/monitoringSettings';

const MonitoringSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const [settings, setSettings] = useState<MonitoringSettingsData>({
    metrics_retention_realtime_days: 7,
    metrics_retention_daily_days: 30,
    metrics_retention_weekly_days: 365,
    enable_aggregation: true,
    aggregation_interval: 'daily',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();
  const settingsRef = useRef<MonitoringSettingsData>(settings);

  useEffect(() => {
    fetchSettings();
  }, []);

  // Keep ref in sync with state for blur handlers
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  const fetchSettings = async () => {
    setLoading(true);
    setError(null);
    try {
      const monitoringSettings = await getMonitoringSettings();
      setSettings(monitoringSettings);
    } catch (err) {
      console.error('Failed to fetch monitoring settings:', err);
      setError(t('monitoring.errors.loadFailed') as string);
    } finally {
      setLoading(false);
    }
  };

  const saveSettings = useCallback(async (updatedSettings: MonitoringSettingsData) => {
    setSaving(true);
    setError(null);
    try {
      await updateMonitoringSettings(updatedSettings);
      enqueueSnackbar(t('monitoring.messages.updateSuccess') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to save monitoring settings:', err);
      setError(err.response?.data?.error || t('monitoring.errors.saveFailed') as string);
      enqueueSnackbar(t('monitoring.errors.saveFailed') as string, { variant: 'error' });
      await fetchSettings();
    } finally {
      setSaving(false);
    }
  }, [t, enqueueSnackbar]);

  // Auto-save handler for switch - saves immediately
  const handleSwitchChange = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const previousSettings = { ...settings };
    const updatedSettings = { ...settings, enable_aggregation: event.target.checked };
    setSettings(updatedSettings);
    setSaving(true);
    setError(null);
    try {
      await updateMonitoringSettings(updatedSettings);
      enqueueSnackbar(t('monitoring.messages.updateSuccess') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to update setting:', err);
      setSettings(previousSettings);
      enqueueSnackbar(t('monitoring.errors.saveFailed') as string, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  }, [settings, t, enqueueSnackbar]);

  // Auto-save handler for select - saves immediately
  const handleIntervalChange = useCallback(async (value: string) => {
    const previousSettings = { ...settings };
    const updatedSettings = { ...settings, aggregation_interval: value };
    setSettings(updatedSettings);
    setSaving(true);
    setError(null);
    try {
      await updateMonitoringSettings(updatedSettings);
      enqueueSnackbar(t('monitoring.messages.updateSuccess') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to update setting:', err);
      setSettings(previousSettings);
      enqueueSnackbar(t('monitoring.errors.saveFailed') as string, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  }, [settings, t, enqueueSnackbar]);

  // Change handler for number fields - only updates local state
  const handleRetentionChange = (field: keyof MonitoringSettingsData) => (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = parseInt(event.target.value, 10);
    if (!isNaN(value) && value >= 0) {
      setSettings({ ...settings, [field]: value });
    }
  };

  // Blur handler for number fields - triggers save
  const handleBlurSave = useCallback(async () => {
    await saveSettings(settingsRef.current);
  }, [saveSettings]);

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight={200}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('monitoring.title')}
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="subtitle1" gutterBottom sx={{ fontWeight: 'bold' }}>
          {t('monitoring.metricsRetention.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          {t('monitoring.metricsRetention.description')}
        </Typography>

        <Box sx={{ mb: 3 }}>
          <TextField
            fullWidth
            label={t('monitoring.metricsRetention.realtimeData')}
            type="number"
            value={settings.metrics_retention_realtime_days}
            onChange={handleRetentionChange('metrics_retention_realtime_days')}
            onBlur={handleBlurSave}
            disabled={saving}
            helperText={t('monitoring.metricsRetention.realtimeDataHelper')}
            inputProps={{ min: 0, step: 1 }}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label={t('monitoring.metricsRetention.dailyAggregates')}
            type="number"
            value={settings.metrics_retention_daily_days}
            onChange={handleRetentionChange('metrics_retention_daily_days')}
            onBlur={handleBlurSave}
            disabled={saving}
            helperText={t('monitoring.metricsRetention.dailyAggregatesHelper')}
            inputProps={{ min: 0, step: 1 }}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label={t('monitoring.metricsRetention.weeklyAggregates')}
            type="number"
            value={settings.metrics_retention_weekly_days}
            onChange={handleRetentionChange('metrics_retention_weekly_days')}
            onBlur={handleBlurSave}
            disabled={saving}
            helperText={t('monitoring.metricsRetention.weeklyAggregatesHelper')}
            inputProps={{ min: 0, step: 1 }}
          />
        </Box>

        <Divider sx={{ my: 3 }} />

        <Typography variant="subtitle1" gutterBottom sx={{ fontWeight: 'bold' }}>
          {t('monitoring.aggregation.title')}
        </Typography>

        <FormControlLabel
          control={
            <Switch
              checked={settings.enable_aggregation}
              onChange={handleSwitchChange}
              disabled={saving}
            />
          }
          label={t('monitoring.aggregation.enableAggregation')}
          sx={{ mb: 2 }}
        />
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('monitoring.aggregation.enableAggregationDescription')}
        </Typography>

        <FormControl fullWidth sx={{ mb: 2 }}>
          <InputLabel>{t('monitoring.aggregation.interval')}</InputLabel>
          <Select
            value={settings.aggregation_interval}
            onChange={(e) => handleIntervalChange(e.target.value)}
            label={t('monitoring.aggregation.interval')}
            disabled={!settings.enable_aggregation || saving}
          >
            <MenuItem value="hourly">{t('monitoring.aggregation.hourly')}</MenuItem>
            <MenuItem value="daily">{t('monitoring.aggregation.daily')}</MenuItem>
            <MenuItem value="weekly">{t('monitoring.aggregation.weekly')}</MenuItem>
          </Select>
          <FormHelperText>
            {t('monitoring.aggregation.intervalHelper')}
          </FormHelperText>
        </FormControl>

        <Alert severity="info" sx={{ mt: 2 }}>
          <Typography variant="body2">
            <strong>{t('monitoring.cascading.title')}</strong>
            <br />
            • {t('monitoring.cascading.realtime')}
            <br />
            • {t('monitoring.cascading.daily')}
            <br />
            • {t('monitoring.cascading.weekly')}
            <br />
            • {t('monitoring.cascading.benefit')}
          </Typography>
        </Alert>
      </Paper>
    </Box>
  );
};

export default MonitoringSettings;
