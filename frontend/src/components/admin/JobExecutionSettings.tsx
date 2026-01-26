import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  TextField,
  Button,
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

  useEffect(() => {
    fetchSettings();
  }, []);

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

  const handleSave = async () => {
    if (!settings) return;
    
    setError(null);
    setSaving(true);
    
    try {
      await updateJobExecutionSettings(settings);
      enqueueSnackbar(t('jobExecution.messages.updateSuccess') as string, { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to update job execution settings:', err);
      const message = err.response?.data?.error || t('jobExecution.messages.saveFailed') as string;
      setError(message);
      enqueueSnackbar(message, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  const handleChange = (field: keyof JobExecutionSettings) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    if (!settings) return;
    
    const value = event.target.type === 'checkbox' 
      ? event.target.checked 
      : parseInt(event.target.value, 10);
    
    setSettings({
      ...settings,
      [field]: value,
    });
  };

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
                    setSettings({
                      ...settings,
                      default_chunk_duration: convertMinutesToSeconds(minutes),
                    });
                  }}
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
                  onChange={handleChange('chunk_fluctuation_percentage')}
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
                    setSettings({
                      ...settings,
                      agent_hashlist_retention_hours: convertDaysToHours(days),
                    });
                  }}
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
                  onChange={handleChange('max_concurrent_jobs_per_agent')}
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
                  onChange={handleChange('progress_reporting_interval')}
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
                    setSettings({
                      ...settings,
                      benchmark_cache_duration_hours: convertDaysToHours(days),
                    });
                  }}
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
                  onChange={handleChange('speedtest_timeout_seconds')}
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
                  onChange={handleChange('reconnect_grace_period_minutes')}
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
                      onChange={handleChange('job_interruption_enabled')}
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
                      onChange={handleChange('enable_realtime_crack_notifications')}
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
                  onChange={handleChange('job_refresh_interval_seconds')}
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
                  onChange={handleChange('max_chunk_retry_attempts')}
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
                  onChange={handleChange('jobs_per_page_default')}
                  helperText={t('jobExecution.jobControl.jobsPerPageHelper')}
                  InputProps={{
                    inputProps: { min: 5, max: 100 },
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
              <Grid item xs={12}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings.potfile_enabled}
                      onChange={handleChange('potfile_enabled')}
                    />
                  }
                  label={t('jobExecution.potfile.enablePotfile') as string}
                />
                <Typography variant="caption" color="textSecondary" display="block">
                  {t('jobExecution.potfile.enablePotfileHelper')}
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
                      onChange={handleChange('rule_split_enabled')}
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
                    setSettings({
                      ...settings,
                      rule_split_threshold: value,
                    });
                  }}
                  disabled={!settings.rule_split_enabled}
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
                  onChange={handleChange('rule_split_min_rules')}
                  disabled={!settings.rule_split_enabled}
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
                  onChange={handleChange('rule_split_max_chunks')}
                  disabled={!settings.rule_split_enabled}
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
                  disabled={!settings.rule_split_enabled}
                  helperText={t('jobExecution.ruleSplitting.chunkDirectoryHelper')}
                />
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Metrics Retention Settings */}

        {/* Save Button */}
        <Grid item xs={12}>
          <Box display="flex" justifyContent="flex-end">
            <Button
              variant="contained"
              color="primary"
              onClick={handleSave}
              disabled={saving || loading}
              size="large"
            >
              {saving ? <CircularProgress size={24} /> : t('jobExecution.buttons.save')}
            </Button>
          </Box>
        </Grid>
      </Grid>
    </Box>
  );
};

export default JobExecutionSettingsComponent;