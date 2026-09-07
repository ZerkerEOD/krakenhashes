import React, { useState, useEffect, useCallback } from 'react';
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
  MenuItem,
} from '@mui/material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getSystemSettings, updateSystemSetting } from '../../services/systemSettings';

/**
 * Job Execution settings.
 *
 * Organising principle: this page owns everything that governs how a JOB runs.
 * System Settings owns everything that governs the system as a whole. Settings
 * that were on the wrong side of that line (the agent speed-test timeouts, the
 * scheduling/overflow toggles) have moved here; ones that are really display or
 * analytics preferences have moved out.
 *
 * Two deliberate departures from the previous implementation:
 *
 *  1. Reads and writes go through the generic settings API (`GET /admin/settings`,
 *     `PUT /admin/settings/{key}`) rather than the typed bulk endpoint. The bulk
 *     endpoint PUT *every* key on any field blur, so saving this page could
 *     silently overwrite a value an operator had just changed on another page —
 *     and it made the whole page's `updated_at` identical, destroying the audit
 *     trail. Each field now writes only itself.
 *
 *  2. Panels group settings by the stage of work they affect, so related knobs
 *     sit together. The Keyspace & Benchmark panel in particular exists because
 *     the keyspace timeout and the agent speed-test timeouts were on different
 *     pages in different units, which is how an operator ends up raising the
 *     wrong one for hours.
 */

type SettingsMap = Record<string, string>;


const JobExecutionSettingsComponent: React.FC = () => {
  const { t } = useTranslation('admin');
  const [values, setValues] = useState<SettingsMap>({});
  const [loading, setLoading] = useState(true);
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();

  const fetchSettings = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getSystemSettings();
      const map: SettingsMap = {};
      data.forEach((s) => {
        map[s.key] = s.value ?? '';
      });
      setValues(map);
    } catch (err: any) {
      console.error('Failed to fetch settings:', err);
      setError(err.response?.data?.error || (t('jobExecution.errors.loadFailed') as string));
      enqueueSnackbar(t('jobExecution.messages.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [t, enqueueSnackbar]);

  useEffect(() => {
    fetchSettings();
  }, [fetchSettings]);

  /**
   * Persist a single setting. Only the key being edited is written, so a save
   * here can never clobber an unrelated setting.
   */
  const saveOne = useCallback(
    async (key: string, value: string, previous: string) => {
      setSavingKey(key);
      setError(null);
      try {
        await updateSystemSetting(key, value);
        enqueueSnackbar(t('jobExecution.messages.updateSuccess') as string, { variant: 'success' });
      } catch (err: any) {
        console.error(`Failed to update ${key}:`, err);
        // Revert just this field — the rest of the page is still server-accurate
        // because nothing else was sent.
        setValues((v) => ({ ...v, [key]: previous }));
        const message = err.response?.data?.error || (t('jobExecution.messages.saveFailed') as string);
        setError(`${key}: ${message}`);
        enqueueSnackbar(message, { variant: 'error' });
      } finally {
        setSavingKey(null);
      }
    },
    [t, enqueueSnackbar]
  );

  const numberValue = (key: string, fallback = 0): number => {
    const parsed = parseInt(values[key] ?? '', 10);
    return isNaN(parsed) ? fallback : parsed;
  };

  const boolValue = (key: string): boolean => values[key] === 'true';

  /** Number field bound to one setting key; saves on blur. */
  const NumberSetting: React.FC<{
    settingKey: string;
    label: string;
    helper: string;
    min?: number;
    max?: number;
    unit?: string;
    /** Displayed unit differs from the stored unit (e.g. stored seconds, shown minutes). */
    toDisplay?: (stored: number) => number;
    toStored?: (shown: number) => number;
  }> = ({ settingKey, label, helper, min, max, unit, toDisplay, toStored }) => {
    const stored = numberValue(settingKey);
    const shown = toDisplay ? toDisplay(stored) : stored;
    return (
      <TextField
        fullWidth
        type="number"
        label={label}
        value={shown}
        onChange={(e) => {
          const parsed = parseInt(e.target.value, 10);
          if (isNaN(parsed)) return;
          const next = toStored ? toStored(parsed) : parsed;
          setValues((v) => ({ ...v, [settingKey]: String(next) }));
        }}
        onBlur={(e) => {
          const parsed = parseInt(e.target.value, 10);
          if (isNaN(parsed)) return;
          const next = String(toStored ? toStored(parsed) : parsed);
          if (next === values[settingKey]) return;
          saveOne(settingKey, next, values[settingKey] ?? '');
        }}
        disabled={loading || savingKey === settingKey}
        helperText={helper}
        InputProps={{
          inputProps: { min, max },
          endAdornment: unit ? <InputAdornment position="end">{unit}</InputAdornment> : undefined,
        }}
      />
    );
  };

  /** Toggle bound to one setting key; saves immediately. */
  const SwitchSetting: React.FC<{ settingKey: string; label: string; helper?: string }> = ({
    settingKey,
    label,
    helper,
  }) => (
    <>
      <FormControlLabel
        control={
          <Switch
            checked={boolValue(settingKey)}
            onChange={(e) => {
              const previous = values[settingKey] ?? '';
              const next = String(e.target.checked);
              setValues((v) => ({ ...v, [settingKey]: next }));
              saveOne(settingKey, next, previous);
            }}
            disabled={loading || savingKey === settingKey}
          />
        }
        label={label}
      />
      {helper && (
        <Typography variant="caption" color="textSecondary" display="block">
          {helper}
        </Typography>
      )}
    </>
  );

  /** Select bound to one setting key; saves immediately. */
  const SelectSetting: React.FC<{
    settingKey: string;
    label: string;
    helper: string;
    options: { value: string; label: string }[];
  }> = ({ settingKey, label, helper, options }) => (
    <TextField
      select
      fullWidth
      label={label}
      value={values[settingKey] ?? ''}
      onChange={(e) => {
        const previous = values[settingKey] ?? '';
        setValues((v) => ({ ...v, [settingKey]: e.target.value }));
        saveOne(settingKey, e.target.value, previous);
      }}
      disabled={loading || savingKey === settingKey}
      helperText={helper}
    >
      {options.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  );

  const Panel: React.FC<{ title: string; children: React.ReactNode; caption?: string }> = ({
    title,
    caption,
    children,
  }) => (
    <Grid item xs={12}>
      <Paper sx={{ p: 3 }}>
        <Typography variant="subtitle1" gutterBottom fontWeight="bold">
          {title}
        </Typography>
        {caption && (
          <Typography variant="body2" color="textSecondary" sx={{ mb: 1 }}>
            {caption}
          </Typography>
        )}
        <Divider sx={{ mb: 2 }} />
        <Grid container spacing={2}>
          {children}
        </Grid>
      </Paper>
    </Grid>
  );

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="400px">
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('jobExecution.title')}
      </Typography>
      <Typography variant="body2" color="textSecondary" gutterBottom>
        {t('jobExecution.description')}
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      <Grid container spacing={3}>
        {/* ---------------- Chunking ---------------- */}
        <Panel title={t('jobExecution.chunking.title') as string}>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="default_chunk_duration"
              label={t('jobExecution.chunking.defaultChunkDuration') as string}
              helper={t('jobExecution.chunking.defaultChunkDurationHelper') as string}
              min={1}
              max={1440}
              unit={t('jobExecution.agentConfig.minutes') as string}
              toDisplay={(s) => Math.floor(s / 60)}
              toStored={(m) => m * 60}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="min_chunk_seconds"
              label={t('jobExecution.chunking.minChunkSeconds') as string}
              helper={t('jobExecution.chunking.minChunkSecondsHelper') as string}
              min={1}
              max={300}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="chunk_overrun_tolerance_percent"
              label={t('jobExecution.chunking.overrunTolerance') as string}
              helper={t('jobExecution.chunking.overrunToleranceHelper') as string}
              min={0}
              max={500}
              unit="%"
            />
          </Grid>
          <Grid item xs={12}>
            <SwitchSetting
              settingKey="chunk_overrun_guard_enabled"
              label={t('jobExecution.chunking.overrunGuard') as string}
              helper={t('jobExecution.chunking.overrunGuardHelper') as string}
            />
          </Grid>
        </Panel>

        {/* ------- Keyspace & Benchmark (consolidated) ------- */}
        <Panel
          title={t('jobExecution.keyspaceBenchmark.title') as string}
          caption={t('jobExecution.keyspaceBenchmark.caption') as string}
        >
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="keyspace_calculation_timeout_minutes"
              label={t('jobExecution.keyspaceBenchmark.keyspaceTimeout') as string}
              helper={t('jobExecution.keyspaceBenchmark.keyspaceTimeoutHelper') as string}
              min={1}
              max={1440}
              unit={t('jobExecution.agentConfig.minutes') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="speed_test_timeout_seconds_uncompressed"
              label={t('jobExecution.keyspaceBenchmark.speedTestUncompressed') as string}
              helper={t('jobExecution.keyspaceBenchmark.speedTestUncompressedHelper') as string}
              min={30}
              max={3600}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="speed_test_timeout_seconds_compressed"
              label={t('jobExecution.keyspaceBenchmark.speedTestCompressed') as string}
              helper={t('jobExecution.keyspaceBenchmark.speedTestCompressedHelper') as string}
              min={30}
              max={7200}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="speed_test_min_status_updates"
              label={t('jobExecution.keyspaceBenchmark.minStatusUpdates') as string}
              helper={t('jobExecution.keyspaceBenchmark.minStatusUpdatesHelper') as string}
              min={1}
              max={20}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_cache_duration_hours"
              label={t('jobExecution.keyspaceBenchmark.cacheDuration') as string}
              helper={t('jobExecution.keyspaceBenchmark.cacheDurationHelper') as string}
              min={1}
              max={8760}
              unit={t('jobExecution.keyspaceBenchmark.hours') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_history_retention_days"
              label={t('jobExecution.keyspaceBenchmark.historyRetention') as string}
              helper={t('jobExecution.keyspaceBenchmark.historyRetentionHelper') as string}
              min={1}
              max={3650}
              unit={t('jobExecution.keyspaceBenchmark.days') as string}
            />
          </Grid>
        </Panel>

        {/* -------- Benchmark reliability (previously hidden) -------- */}
        <Panel
          title={t('jobExecution.benchmarkReliability.title') as string}
          caption={t('jobExecution.benchmarkReliability.caption') as string}
        >
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_failure_threshold"
              label={t('jobExecution.benchmarkReliability.failureThreshold') as string}
              helper={t('jobExecution.benchmarkReliability.failureThresholdHelper') as string}
              min={1}
              max={50}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_hard_failure_cap"
              label={t('jobExecution.benchmarkReliability.hardFailureCap') as string}
              helper={t('jobExecution.benchmarkReliability.hardFailureCapHelper') as string}
              min={1}
              max={100}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_blocklist_cooldown_hours"
              label={t('jobExecution.benchmarkReliability.blocklistCooldown') as string}
              helper={t('jobExecution.benchmarkReliability.blocklistCooldownHelper') as string}
              min={1}
              max={720}
              unit={t('jobExecution.keyspaceBenchmark.hours') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_storm_threshold"
              label={t('jobExecution.benchmarkReliability.stormThreshold') as string}
              helper={t('jobExecution.benchmarkReliability.stormThresholdHelper') as string}
              min={1}
              max={100}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="benchmark_storm_window_minutes"
              label={t('jobExecution.benchmarkReliability.stormWindow') as string}
              helper={t('jobExecution.benchmarkReliability.stormWindowHelper') as string}
              min={1}
              max={1440}
              unit={t('jobExecution.agentConfig.minutes') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_benchmark_streak_reset_minutes"
              label={t('jobExecution.benchmarkReliability.streakReset') as string}
              helper={t('jobExecution.benchmarkReliability.streakResetHelper') as string}
              min={1}
              max={1440}
              unit={t('jobExecution.agentConfig.minutes') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_benchmark_quarantine_streak"
              label={t('jobExecution.benchmarkReliability.quarantineStreak') as string}
              helper={t('jobExecution.benchmarkReliability.quarantineStreakHelper') as string}
              min={1}
              max={200}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_benchmark_quarantine_distinct"
              label={t('jobExecution.benchmarkReliability.quarantineDistinct') as string}
              helper={t('jobExecution.benchmarkReliability.quarantineDistinctHelper') as string}
              min={1}
              max={50}
            />
          </Grid>
        </Panel>

        {/* -------- Scheduling & allocation (moved from System Settings) -------- */}
        <Panel title={t('jobExecution.scheduling.title') as string}>
          <Grid item xs={12} md={6}>
            <SelectSetting
              settingKey="agent_overflow_allocation_mode"
              label={t('jobExecution.scheduling.overflowMode') as string}
              helper={t('jobExecution.scheduling.overflowModeHelper') as string}
              options={[
                { value: 'fifo', label: 'FIFO' },
                { value: 'round_robin', label: 'Round robin' },
                { value: 'enforce_max_agents', label: 'Enforce max agents' },
                { value: 'max_agents_fifo', label: 'Max agents, then FIFO' },
                { value: 'max_agents_round_robin', label: 'Max agents, then round robin' },
              ]}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="max_concurrent_jobs_per_agent"
              label={t('jobExecution.scheduling.maxConcurrentJobs') as string}
              helper={t('jobExecution.scheduling.maxConcurrentJobsHelper') as string}
              min={1}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="agent_scheduling_enabled"
              label={t('jobExecution.scheduling.agentScheduling') as string}
              helper={t('jobExecution.scheduling.agentSchedulingHelper') as string}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="job_interruption_enabled"
              label={t('jobExecution.scheduling.jobInterruption') as string}
              helper={t('jobExecution.scheduling.jobInterruptionHelper') as string}
            />
          </Grid>
        </Panel>

        {/* -------- Task health -------- */}
        <Panel
          title={t('jobExecution.taskHealth.title') as string}
          caption={t('jobExecution.taskHealth.caption') as string}
        >
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="task_heartbeat_timeout_seconds"
              label={t('jobExecution.taskHealth.heartbeatTimeout') as string}
              helper={t('jobExecution.taskHealth.heartbeatTimeoutHelper') as string}
              min={30}
              max={3600}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="task_startup_grace_seconds"
              label={t('jobExecution.taskHealth.startupGrace') as string}
              helper={t('jobExecution.taskHealth.startupGraceHelper') as string}
              min={30}
              max={3600}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="network_grace_seconds"
              label={t('jobExecution.taskHealth.networkGrace') as string}
              helper={t('jobExecution.taskHealth.networkGraceHelper') as string}
              min={0}
              max={600}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="reconnect_grace_period_minutes"
              label={t('jobExecution.taskHealth.reconnectGrace') as string}
              helper={t('jobExecution.taskHealth.reconnectGraceHelper') as string}
              min={1}
              max={120}
              unit={t('jobExecution.agentConfig.minutes') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_offline_buffer_minutes"
              label={t('jobExecution.taskHealth.offlineBuffer') as string}
              helper={t('jobExecution.taskHealth.offlineBufferHelper') as string}
              min={1}
              max={120}
              unit={t('jobExecution.agentConfig.minutes') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="max_chunk_retry_attempts"
              label={t('jobExecution.taskHealth.maxRetryAttempts') as string}
              helper={t('jobExecution.taskHealth.maxRetryAttemptsHelper') as string}
              min={0}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="progress_reporting_interval"
              label={t('jobExecution.taskHealth.progressInterval') as string}
              helper={t('jobExecution.taskHealth.progressIntervalHelper') as string}
              min={1}
              max={60}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_hashlist_retention_hours"
              label={t('jobExecution.taskHealth.hashlistRetention') as string}
              helper={t('jobExecution.taskHealth.hashlistRetentionHelper') as string}
              min={1}
              max={8760}
              unit={t('jobExecution.keyspaceBenchmark.hours') as string}
            />
          </Grid>
        </Panel>

        {/* -------- Loopback -------- */}
        <Panel title={t('jobExecution.loopback.title') as string}>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="loopback_max_rounds"
              label={t('jobExecution.loopback.maxRounds') as string}
              helper={t('jobExecution.loopback.maxRoundsHelper') as string}
              min={1}
              max={100}
            />
          </Grid>
        </Panel>

        {/* -------- Potfile -------- */}
        <Panel title={t('jobExecution.potfile.title') as string}>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="potfile_enabled"
              label={t('jobExecution.potfile.enablePotfile') as string}
              helper={t('jobExecution.potfile.enablePotfileHelper') as string}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="client_potfiles_enabled"
              label={t('jobExecution.potfile.enableClientPotfiles') as string}
              helper={t('jobExecution.potfile.enableClientPotfilesHelper') as string}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="remove_from_global_potfile_on_hashlist_delete_default"
              label={t('jobExecution.potfile.removeGlobalDefault') as string}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SwitchSetting
              settingKey="remove_from_client_potfile_on_hashlist_delete_default"
              label={t('jobExecution.potfile.removeClientDefault') as string}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="potfile_max_batch_size"
              label={t('jobExecution.potfile.maxBatchSize') as string}
              helper={t('jobExecution.potfile.maxBatchSizeHelper') as string}
              min={1000}
              max={1000000}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="potfile_batch_interval"
              label={t('jobExecution.potfile.batchInterval') as string}
              helper={t('jobExecution.potfile.batchIntervalHelper') as string}
              min={1}
              max={300}
              unit={t('jobExecution.agentConfig.seconds') as string}
            />
          </Grid>
        </Panel>

        {/* -------- Notifications relating to jobs -------- */}
        <Panel title={t('jobExecution.jobNotifications.title') as string}>
          <Grid item xs={12}>
            <SwitchSetting
              settingKey="enable_realtime_crack_notifications"
              label={t('jobExecution.jobNotifications.realtimeCracks') as string}
              helper={t('jobExecution.jobNotifications.realtimeCracksHelper') as string}
            />
          </Grid>
        </Panel>
      </Grid>
    </Box>
  );
};

export default JobExecutionSettingsComponent;
