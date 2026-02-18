import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  Box,
  Typography,
  TextField,
  Alert,
  CircularProgress,
  Grid,
  FormControlLabel,
  Switch,
  Divider,
  Paper,
  InputAdornment,
} from '@mui/material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getJobExecutionSettings, updateJobExecutionSettings, JobExecutionSettings } from '../../services/jobSettings';

const JobExecutionSettingsComponent: React.FC = () => {
  const { t } = useTranslation('admin');
  const [settings, setSettings] = useState<JobExecutionSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();
  const settingsRef = useRef<JobExecutionSettings | null>(null);

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
      const data = await getJobExecutionSettings();
      setSettings(data);
    } catch (err: any) {
      console.error('Failed to fetch job execution settings:', err);
      setError(err.response?.data?.error || t('jobExecution.errors.loadFailed') as string);
      enqueueSnackbar(t('jobExecution.messages.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const saveSettings = useCallback(async (updatedSettings: JobExecutionSettings) => {
    setSaving(true);
    setError(null);
    try {
      const result = await updateJobExecutionSettings(updatedSettings);
      if (result.success) {
        enqueueSnackbar(t('jobExecution.messages.updateSuccess') as string, { variant: 'success' });
      } else {
        // Partial failure - some settings saved, some failed
        const failedList = result.failed_keys?.join(', ') || 'unknown';
        enqueueSnackbar(`Some settings failed to save: ${failedList}`, { variant: 'warning' });
        setError(`Failed to update: ${failedList}`);
      }
    } catch (err: any) {
      console.error('Failed to update job execution settings:', err);
      const message = err.response?.data?.error || t('jobExecution.messages.saveFailed') as string;
      setError(message);
      enqueueSnackbar(message, { variant: 'error' });
      // Reload settings to revert to server state
      await fetchSettings();
    } finally {
      setSaving(false);
    }
  }, [t, enqueueSnackbar]);

  // Auto-save handler for switches - saves immediately
  const handleSwitchChange = useCallback((field: keyof JobExecutionSettings) => async (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    if (!settings) return;
    const previousSettings = { ...settings };
    const updatedSettings = { ...settings, [field]: event.target.checked };
    setSettings(updatedSettings);
    setSaving(true);
    setError(null);
    try {
      const result = await updateJobExecutionSettings(updatedSettings);
      if (result.success) {
        enqueueSnackbar(t('jobExecution.messages.updateSuccess') as string, { variant: 'success' });
      } else {
        const failedList = result.failed_keys?.join(', ') || 'unknown';
        enqueueSnackbar(`Some settings failed to save: ${failedList}`, { variant: 'warning' });
        setError(`Failed to update: ${failedList}`);
      }
    } catch (err: any) {
      console.error('Failed to update setting:', err);
      setSettings(previousSettings); // Revert on error
      const message = err.response?.data?.error || t('jobExecution.messages.saveFailed') as string;
      enqueueSnackbar(message, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  }, [settings, t, enqueueSnackbar]);

  // Change handler for number fields - only updates local state
  const handleNumberChange = useCallback((field: keyof JobExecutionSettings) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    if (!settings) return;
    const value = parseInt(event.target.value, 10);
    if (!isNaN(value)) {
      setSettings({ ...settings, [field]: value });
    }
  }, [settings]);

  // Blur handler for number/text fields - triggers save
  const handleBlurSave = useCallback(async () => {
    if (!settingsRef.current) return;
    await saveSettings(settingsRef.current);
  }, [saveSettings]);

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="400px">
        <CircularProgress />
      </Box>
    );
  }

  if (!settings) {
    return (
      <Alert severity="error">{t('jobExecution.messages.loadFailed')}</Alert>
    );
  }

  const convertSecondsToMinutes = (seconds: number) => Math.floor(seconds / 60);
  const convertMinutesToSeconds = (minutes: number) => minutes * 60;
  const convertHoursToDays = (hours: number) => Math.floor(hours / 24);
  const convertDaysToHours = (days: number) => days * 24;

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('jobExecution.title')}
      </Typography>
      <Typography variant="body2" color="textSecondary" gutterBottom>
        {t('jobExecution.description')}
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      <Grid container spacing={3}>
        {/* Chunking Settings */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom fontWeight="bold">
              {t('jobExecution.chunking.title')}
            </Typography>
            <Divider sx={{ mb: 2 }} />
            <Grid container spacing={2}>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.chunking.defaultChunkDuration') as string}
                  value={convertSecondsToMinutes(settings.default_chunk_duration)}
                  onChange={(e) => {
                    const minutes = parseInt(e.target.value, 10);
                    if (!isNaN(minutes)) {
                      setSettings({
                        ...settings,
                        default_chunk_duration: convertMinutesToSeconds(minutes),
                      });
                    }
                  }}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.chunking.defaultChunkDurationHelper')}
                  InputProps={{
                    inputProps: { min: 1 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.chunking.minutes')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.chunking.chunkFluctuation') as string}
                  value={settings.chunk_fluctuation_percentage}
                  onChange={handleNumberChange('chunk_fluctuation_percentage')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.chunking.chunkFluctuationHelper')}
                  InputProps={{
                    inputProps: { min: 0, max: 100 },
                    endAdornment: <InputAdornment position="end">%</InputAdornment>,
                  }}
                />
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Agent Settings */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom fontWeight="bold">
              {t('jobExecution.agentConfig.title')}
            </Typography>
            <Divider sx={{ mb: 2 }} />
            <Grid container spacing={2}>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.hashlistRetention') as string}
                  value={convertHoursToDays(settings.agent_hashlist_retention_hours)}
                  onChange={(e) => {
                    const days = parseInt(e.target.value, 10);
                    if (!isNaN(days)) {
                      setSettings({
                        ...settings,
                        agent_hashlist_retention_hours: convertDaysToHours(days),
                      });
                    }
                  }}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.hashlistRetentionHelper')}
                  InputProps={{
                    inputProps: { min: 1 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.days')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.maxConcurrentJobs') as string}
                  value={settings.max_concurrent_jobs_per_agent}
                  onChange={handleNumberChange('max_concurrent_jobs_per_agent')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.maxConcurrentJobsHelper')}
                  InputProps={{
                    inputProps: { min: 1 },
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.progressReporting') as string}
                  value={settings.progress_reporting_interval}
                  onChange={handleNumberChange('progress_reporting_interval')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.progressReportingHelper')}
                  InputProps={{
                    inputProps: { min: 1 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.seconds')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.benchmarkCache') as string}
                  value={convertHoursToDays(settings.benchmark_cache_duration_hours)}
                  onChange={(e) => {
                    const days = parseInt(e.target.value, 10);
                    if (!isNaN(days)) {
                      setSettings({
                        ...settings,
                        benchmark_cache_duration_hours: convertDaysToHours(days),
                      });
                    }
                  }}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.benchmarkCacheHelper')}
                  InputProps={{
                    inputProps: { min: 1 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.days')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.speedtestTimeout') as string}
                  value={settings.speedtest_timeout_seconds}
                  onChange={handleNumberChange('speedtest_timeout_seconds')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.speedtestTimeoutHelper')}
                  InputProps={{
                    inputProps: { min: 60, max: 600 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.seconds')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.agentConfig.reconnectGrace') as string}
                  value={settings.reconnect_grace_period_minutes}
                  onChange={handleNumberChange('reconnect_grace_period_minutes')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.agentConfig.reconnectGraceHelper')}
                  InputProps={{
                    inputProps: { min: 1, max: 60 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.minutes')}</InputAdornment>,
                  }}
                />
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Job Control Settings */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom fontWeight="bold">
              {t('jobExecution.jobControl.title')}
            </Typography>
            <Divider sx={{ mb: 2 }} />
            <Grid container spacing={2}>
              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.job_interruption_enabled}
                      onChange={handleSwitchChange('job_interruption_enabled')}
                      disabled={saving}
                    />
                  }
                  label={t('jobExecution.jobControl.allowInterruption') as string}
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  {t('jobExecution.jobControl.allowInterruptionHelper')}
                </Typography>
              </Grid>
              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.enable_realtime_crack_notifications}
                      onChange={handleSwitchChange('enable_realtime_crack_notifications')}
                      disabled={saving}
                    />
                  }
                  label={t('jobExecution.jobControl.realtimeNotifications') as string}
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  {t('jobExecution.jobControl.realtimeNotificationsHelper')}
                </Typography>
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.jobControl.refreshInterval') as string}
                  value={settings.job_refresh_interval_seconds}
                  onChange={handleNumberChange('job_refresh_interval_seconds')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.jobControl.refreshIntervalHelper')}
                  InputProps={{
                    inputProps: { min: 1, max: 60 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.seconds')}</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.jobControl.maxRetryAttempts') as string}
                  value={settings.max_chunk_retry_attempts}
                  onChange={handleNumberChange('max_chunk_retry_attempts')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.jobControl.maxRetryAttemptsHelper')}
                  InputProps={{
                    inputProps: { min: 0, max: 10 },
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.jobControl.jobsPerPage') as string}
                  value={settings.jobs_per_page_default}
                  onChange={handleNumberChange('jobs_per_page_default')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.jobControl.jobsPerPageHelper')}
                  InputProps={{
                    inputProps: { min: 5, max: 100 },
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.jobControl.keyspaceTimeout') as string}
                  value={settings.keyspace_calculation_timeout_minutes}
                  onChange={handleNumberChange('keyspace_calculation_timeout_minutes')}
                  onBlur={handleBlurSave}
                  disabled={saving}
                  helperText={t('jobExecution.jobControl.keyspaceTimeoutHelper')}
                  InputProps={{
                    inputProps: { min: 1, max: 60 },
                    endAdornment: <InputAdornment position="end">{t('jobExecution.agentConfig.minutes')}</InputAdornment>,
                  }}
                />
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Potfile Settings */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom fontWeight="bold">
              {t('jobExecution.potfile.title')}
            </Typography>
            <Divider sx={{ mb: 2 }} />
            <Grid container spacing={2}>
              {/* Global Potfile Settings */}
              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.potfile_enabled}
                      onChange={handleSwitchChange('potfile_enabled')}
                      disabled={saving}
                    />
                  }
                  label="Enable global pot-file"
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  {t('jobExecution.potfile.enablePotfileHelper')}
                </Typography>
              </Grid>

              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.remove_from_global_potfile_on_hashlist_delete_default ?? false}
                      onChange={handleSwitchChange('remove_from_global_potfile_on_hashlist_delete_default')}
                      disabled={saving}
                    />
                  }
                  label="Remove from global potfile when hashlist deleted"
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  System default. Can be overridden per-client or at deletion time.
                </Typography>
              </Grid>

              {/* Client Potfiles Section */}
              <Grid item xs={12}>
                <Divider sx={{ my: 2 }} />
                <Typography variant="subtitle2" gutterBottom sx={{ fontWeight: 'medium' }}>
                  Client Potfiles
                </Typography>
              </Grid>

              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.client_potfiles_enabled ?? true}
                      onChange={handleSwitchChange('client_potfiles_enabled')}
                      disabled={saving}
                    />
                  }
                  label="Enable client-specific potfiles"
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  When enabled, cracked passwords can be stored in client-specific potfiles
                  in addition to the global potfile. This enables data isolation between clients.
                </Typography>
              </Grid>

              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.remove_from_client_potfile_on_hashlist_delete_default ?? false}
                      onChange={handleSwitchChange('remove_from_client_potfile_on_hashlist_delete_default')}
                      disabled={saving}
                    />
                  }
                  label="Remove from client potfile when hashlist deleted"
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  System default. Can be overridden per-client or at deletion time.
                </Typography>
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Rule Splitting Settings */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom fontWeight="bold">
              {t('jobExecution.ruleSplitting.title')}
            </Typography>
            <Divider sx={{ mb: 2 }} />
            <Grid container spacing={2}>
              <Grid item xs={12} md={6}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.rule_split_enabled}
                      onChange={handleSwitchChange('rule_split_enabled')}
                      disabled={saving}
                    />
                  }
                  label={t('jobExecution.ruleSplitting.enableRuleSplitting') as string}
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  {t('jobExecution.ruleSplitting.enableRuleSplittingHelper')}
                </Typography>
              </Grid>
              <Grid item xs={12} md={6}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.ruleSplitting.threshold') as string}
                  value={settings.rule_split_threshold}
                  onChange={(e) => {
                    const value = parseFloat(e.target.value);
                    if (!isNaN(value)) {
                      setSettings({
                        ...settings,
                        rule_split_threshold: value,
                      });
                    }
                  }}
                  onBlur={handleBlurSave}
                  disabled={!settings.rule_split_enabled || saving}
                  helperText={t('jobExecution.ruleSplitting.thresholdHelper')}
                  InputProps={{
                    inputProps: { min: 1.1, max: 10, step: 0.1 },
                    endAdornment: <InputAdornment position="end">×</InputAdornment>,
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.ruleSplitting.minRules') as string}
                  value={settings.rule_split_min_rules}
                  onChange={handleNumberChange('rule_split_min_rules')}
                  onBlur={handleBlurSave}
                  disabled={!settings.rule_split_enabled || saving}
                  helperText={t('jobExecution.ruleSplitting.minRulesHelper')}
                  InputProps={{
                    inputProps: { min: 10 },
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  type="number"
                  label={t('jobExecution.ruleSplitting.maxChunks') as string}
                  value={settings.rule_split_max_chunks}
                  onChange={handleNumberChange('rule_split_max_chunks')}
                  onBlur={handleBlurSave}
                  disabled={!settings.rule_split_enabled || saving}
                  helperText={t('jobExecution.ruleSplitting.maxChunksHelper')}
                  InputProps={{
                    inputProps: { min: 2, max: 10000 },
                  }}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  label={t('jobExecution.ruleSplitting.chunkDirectory') as string}
                  value={settings.rule_chunk_temp_dir}
                  onChange={(e) => {
                    setSettings({
                      ...settings,
                      rule_chunk_temp_dir: e.target.value,
                    });
                  }}
                  onBlur={handleBlurSave}
                  disabled={!settings.rule_split_enabled || saving}
                  helperText={t('jobExecution.ruleSplitting.chunkDirectoryHelper')}
                />
              </Grid>
            </Grid>
          </Paper>
        </Grid>
      </Grid>
    </Box>
  );
};

export default JobExecutionSettingsComponent;
