import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation, Trans } from 'react-i18next';
import { useToast } from '../ui/toast';
import { useUnsavedChangesGuard } from '../../hooks/useUnsavedChangesGuard';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  FormControlLabel,
  LinearProgress,
  MenuItem,
  Paper,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import ErrorIcon from '@mui/icons-material/Error';
import {
  getNetworkShare,
  updateNetworkShare,
  validateNetworkShare,
  startMigration,
  getMigrationStatus,
  cancelMigration,
  NetworkShareConfig,
  NetworkShareValidationResult,
  MigrationProgress,
} from '../../services/networkShare';

const ACTIVE_MIGRATION_PHASES = ['draining', 'migrating', 'validating'];

// mount_options map <-> "key=value" lines, one per line, for a simple editor.
const optionsToText = (opts: Record<string, string>): string =>
  Object.entries(opts || {})
    .map(([k, v]) => `${k}=${v}`)
    .join('\n');

const textToOptions = (text: string): Record<string, string> => {
  const out: Record<string, string> = {};
  text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l.length > 0)
    .forEach((line) => {
      const idx = line.indexOf('=');
      if (idx > 0) {
        out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
      }
    });
  return out;
};

const formatBytes = (n: number): string => {
  if (!n || n <= 0) return '—';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(1)} ${units[i]}`;
};

// Escape markup-significant characters so a value interpolated into a <Trans>
// string renders as text (Trans unescapes these entities back for display).
const escapeForTrans = (s: string): string =>
  s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const NetworkShareSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);
  const [config, setConfig] = useState<NetworkShareConfig | null>(null);
  const [health, setHealth] = useState<boolean>(false);
  // The server's actual compose-mounted share path (KH_SHARE_DIR), shown
  // read-only so the panel reflects what the server reads from.
  const [shareDir, setShareDir] = useState<string>('');

  // Editable fields
  const [shareType, setShareType] = useState<'smb' | 'nfs'>('smb');
  const [name, setName] = useState('');
  const [enabled, setEnabled] = useState(false);
  const [serverHost, setServerHost] = useState('');
  const [shareName, setShareName] = useState('');
  const [optionsText, setOptionsText] = useState('');

  const [validation, setValidation] = useState<NetworkShareValidationResult | null>(null);
  const toast = useToast();
  const setSnack = useCallback(
    (n: { msg: string; severity: 'success' | 'error' }) => toast[n.severity](n.msg),
    [toast]
  );
  const [loadError, setLoadError] = useState<string | null>(null);

  const [migration, setMigration] = useState<MigrationProgress | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [starting, setStarting] = useState(false);
  const [canceling, setCanceling] = useState(false);

  const applyConfig = useCallback((c: NetworkShareConfig) => {
    setConfig(c);
    setShareType(c.share_type === 'nfs' ? 'nfs' : 'smb');
    setName(c.name || '');
    setEnabled(!!c.enabled);
    setServerHost(c.server_host || '');
    setShareName(c.share_name || '');
    setOptionsText(optionsToText(c.mount_options || {}));
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await getNetworkShare();
      applyConfig(res.config);
      setHealth(!!res.health);
      setShareDir(res.share_dir || '');
      if (ACTIVE_MIGRATION_PHASES.includes(res.config.migration_state)) {
        try {
          setMigration(await getMigrationStatus());
        } catch {
          /* status not available yet; polling will retry */
        }
      }
    } catch (e: any) {
      setLoadError(e?.response?.data?.error || e?.message || (t('networkShare.toast.loadFailed') as string));
    } finally {
      setLoading(false);
    }
  }, [applyConfig, t]);

  useEffect(() => {
    load();
  }, [load]);

  // Poll migration status while one is active; refresh config on terminal state.
  useEffect(() => {
    if (!migration || !ACTIVE_MIGRATION_PHASES.includes(migration.phase)) return;
    const id = setInterval(async () => {
      try {
        const p = await getMigrationStatus();
        setMigration(p);
        if (!ACTIVE_MIGRATION_PHASES.includes(p.phase)) {
          load();
        }
      } catch {
        /* transient; keep polling */
      }
    }, 2000);
    return () => clearInterval(id);
  }, [migration, load]);

  const migrationInProgress =
    (!!config && ACTIVE_MIGRATION_PHASES.includes(config.migration_state)) ||
    (!!migration && ACTIVE_MIGRATION_PHASES.includes(migration.phase));

  const migrateDirection: 'to_share' | 'to_local' =
    config?.storage_backend === 'share' ? 'to_local' : 'to_share';

  const handleMigrate = async () => {
    setConfirmOpen(false);
    setStarting(true);
    try {
      const p = await startMigration(migrateDirection);
      setMigration(p);
      setSnack({ msg: t('networkShare.toast.migrationStarted') as string, severity: 'success' });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || (t('networkShare.toast.startFailed') as string),
        severity: 'error',
      });
    } finally {
      setStarting(false);
    }
  };

  const handleCancelMigration = async () => {
    setCanceling(true);
    try {
      const p = await cancelMigration();
      setMigration(p);
      setSnack({ msg: t('networkShare.toast.migrationCanceled') as string, severity: 'success' });
      // Refresh config so the locked UI releases once the engine unwinds.
      load();
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || (t('networkShare.toast.cancelFailed') as string),
        severity: 'error',
      });
    } finally {
      setCanceling(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const updated = await updateNetworkShare({
        share_type: shareType,
        name,
        enabled,
        server_host: serverHost,
        share_name: shareName,
        mount_options: textToOptions(optionsText),
      });
      applyConfig(updated);
      setSnack({ msg: t('networkShare.toast.saved') as string, severity: 'success' });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || (t('networkShare.toast.saveFailed') as string),
        severity: 'error',
      });
    } finally {
      setSaving(false);
    }
  };

  const handleValidate = async () => {
    setValidating(true);
    setValidation(null);
    try {
      const res = await validateNetworkShare();
      setValidation(res);
      setHealth(res.mounted);
      if (res.share_dir) setShareDir(res.share_dir);
      setSnack({
        msg: res.ok
          ? (t('networkShare.toast.validated') as string)
          : (t('networkShare.toast.validationProblems') as string),
        severity: res.ok ? 'success' : 'error',
      });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || (t('networkShare.toast.validationFailed') as string),
        severity: 'error',
      });
    } finally {
      setValidating(false);
    }
  };

  // Explicit-Apply area: warn before leaving with unsaved edits.
  const dirty = useMemo(() => {
    if (!config) return false;
    return (
      shareType !== (config.share_type === 'nfs' ? 'nfs' : 'smb') ||
      name !== (config.name || '') ||
      enabled !== !!config.enabled ||
      serverHost !== (config.server_host || '') ||
      shareName !== (config.share_name || '') ||
      optionsText !== optionsToText(config.mount_options || {})
    );
  }, [config, shareType, name, enabled, serverHost, shareName, optionsText]);
  const { dialog: unsavedDialog } = useUnsavedChangesGuard(dirty);

  // Build the read-only mount command an operator runs on a network_direct
  // agent host. Credentials are placeholders — the server never provides them.
  //
  // Resilience defaults: unless the operator already chose a hardness/timeout
  // option, we inject `soft` (and, for NFS, `timeo`/`retrans`) so a high-latency
  // or dropped share returns an error quickly instead of hanging the mount with
  // the kernel's `hard` default. This pairs with the agent's fail-closed share-
  // health check — a stat that times out marks the agent share-not-ready and the
  // scheduler reroutes its work; a hung hard mount would defeat that. The
  // trade-off (an I/O error on a transient blip instead of a silent wait) is the
  // right one for network_direct, which is meant for low-latency/LAN-adjacent
  // shares; remote/high-latency shares should use the download tiers instead.
  const mountCommand = (() => {
    const opts = textToOptions(optionsText);
    const hasKey = (k: string) => Object.prototype.hasOwnProperty.call(opts, k);
    const parts: string[] = ['ro'];
    // Fail-fast: default to a soft mount unless the operator picked soft/hard.
    if (!hasKey('soft') && !hasKey('hard')) parts.push('soft');
    if (shareType === 'nfs') {
      // Bound the per-request wait (deciseconds) and retry count so `soft`
      // actually fails fast: timeo=150 (15s) × retrans=2 ≈ 30s to an I/O error.
      if (!hasKey('timeo')) parts.push('timeo=150');
      if (!hasKey('retrans')) parts.push('retrans=2');
    }
    Object.entries(opts).forEach(([k, v]) => parts.push(v ? `${k}=${v}` : k));
    const host = serverHost || '<HOST>';
    if (shareType === 'nfs') {
      return `sudo mount -t nfs ${host}:${shareName || '<EXPORT>'} <MOUNT_PATH> -o ${parts.join(',')}`;
    }
    const smbOpts = [...parts, 'username=<USERNAME>', 'password=<PASSWORD>'];
    return `sudo mount -t cifs //${host}/${shareName || '<SHARE>'} <MOUNT_PATH> -o ${smbOpts.join(',')}`;
  })();

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      {unsavedDialog}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        <Trans t={t} i18nKey="networkShare.intro" components={{ code: <code /> }} />
      </Typography>

      {loadError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {loadError}
        </Alert>
      )}

      {migrationInProgress && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {t('networkShare.migrationLocked', { state: config?.migration_state })}
        </Alert>
      )}

      {/* Section A: Server storage mount — read-only status + lifecycle actions */}
      <Paper variant="outlined" sx={{ p: 3 }}>
        <Typography variant="subtitle1" gutterBottom>
          {t('networkShare.server.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          <Trans t={t} i18nKey="networkShare.server.description" components={{ code: <code /> }} />
        </Typography>

        <Stack spacing={1.5} sx={{ mb: 2 }}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {t('networkShare.server.mountedPath')}
            </Typography>
            <Typography variant="body2">
              <code>{shareDir || t('networkShare.server.shareDirNotSet')}</code>
            </Typography>
          </Box>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap alignItems="center">
            <Chip
              size="small"
              color={config?.storage_backend === 'share' ? 'primary' : 'default'}
              label={
                config?.storage_backend === 'share'
                  ? t('networkShare.server.servingFromShare')
                  : t('networkShare.server.servingFromLocal')
              }
            />
            <Chip
              size="small"
              icon={health ? <CheckCircleIcon /> : <ErrorIcon />}
              color={health ? 'success' : 'warning'}
              label={
                health
                  ? t('networkShare.server.shareReachable')
                  : t('networkShare.server.shareNotMounted')
              }
            />
            {config && config.migration_state !== 'idle' && (
              <Chip size="small" color="info" label={t('networkShare.server.migrationChip', { state: config.migration_state })} />
            )}
          </Stack>
          {config?.last_validated_at && (
            <Typography variant="caption" color="text.secondary">
              {t('networkShare.server.lastValidated', {
                date: new Date(config.last_validated_at).toLocaleString(),
              })}
              {config.last_validation_error
                ? ` — ${config.last_validation_error}`
                : ` — ${t('networkShare.server.validatedOk')}`}
            </Typography>
          )}
        </Stack>

        <FormControlLabel
          control={
            <Switch
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              disabled={migrationInProgress}
            />
          }
          label={t('networkShare.server.enable')}
        />

        <Divider sx={{ my: 2 }} />

        <Stack direction="row" spacing={2}>
          <Button variant="contained" onClick={handleSave} disabled={saving || migrationInProgress}>
            {saving ? t('networkShare.server.saving') : t('networkShare.server.save')}
          </Button>
          <Button
            variant="outlined"
            onClick={handleValidate}
            disabled={validating || migrationInProgress}
          >
            {validating ? t('networkShare.server.validating') : t('networkShare.server.validate')}
          </Button>
        </Stack>

        {validation && (
          <Alert severity={validation.ok ? 'success' : 'error'} sx={{ mt: 2 }}>
            <Typography variant="body2">
              <Trans
                t={t}
                i18nKey="networkShare.validation.mountPath"
                shouldUnescape
                values={{
                  path: escapeForTrans(validation.share_dir || (t('networkShare.validation.unset') as string)),
                }}
                components={{ code: <code /> }}
              />
            </Typography>
            {validation.error ? (
              <Typography variant="body2">{validation.error}</Typography>
            ) : (
              <Typography variant="body2">
                {t('networkShare.validation.summary', {
                  mounted: validation.mounted
                    ? t('networkShare.validation.yes')
                    : t('networkShare.validation.no'),
                  writable: validation.writable
                    ? t('networkShare.validation.yes')
                    : t('networkShare.validation.no'),
                  free: formatBytes(validation.free_bytes),
                  write: validation.write_mbps.toFixed(1),
                  read: validation.read_mbps.toFixed(1),
                })}
              </Typography>
            )}
          </Alert>
        )}

        <Divider sx={{ my: 3 }} />
        <Typography variant="subtitle2" gutterBottom>
          {t('networkShare.migration.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {migrateDirection === 'to_share'
            ? t('networkShare.migration.descriptionToShare')
            : t('networkShare.migration.descriptionToLocal')}
        </Typography>

        {migrationInProgress && migration ? (
          <Box>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1 }}>
              <CircularProgress size={18} />
              <Chip size="small" color="info" label={t('networkShare.migration.phase', { phase: migration.phase })} />
              {migration.direction && (
                <Typography variant="caption" color="text.secondary">
                  {migration.direction === 'to_share'
                    ? t('networkShare.migration.directionToShare')
                    : t('networkShare.migration.directionToLocal')}
                </Typography>
              )}
            </Stack>
            <Typography variant="body2" sx={{ mb: 1 }}>
              {migration.message}
            </Typography>
            {migration.phase === 'draining' ? (
              <>
                <LinearProgress />
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  {t('networkShare.migration.waitingTasks', { count: migration.agents_busy })}
                  {migration.drain_deadline
                    ? t('networkShare.migration.earliestLock', {
                        time: new Date(migration.drain_deadline).toLocaleTimeString(),
                      })
                    : ''}
                </Typography>
              </>
            ) : (
              <>
                <LinearProgress
                  variant={migration.bytes_total > 0 ? 'determinate' : 'indeterminate'}
                  value={
                    migration.bytes_total > 0
                      ? Math.min(100, (migration.bytes_done / migration.bytes_total) * 100)
                      : undefined
                  }
                />
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  {t('networkShare.migration.filesProgress', {
                    done: migration.files_done,
                    total: migration.files_total,
                    bytesDone: formatBytes(migration.bytes_done),
                    bytesTotal: formatBytes(migration.bytes_total),
                  })}
                  {migration.throughput_mbps > 0 ? ` · ${migration.throughput_mbps.toFixed(1)} MB/s` : ''}
                  {migration.current_file ? ` · ${migration.current_file}` : ''}
                </Typography>
              </>
            )}
            <Box sx={{ mt: 2 }}>
              <Button
                size="small"
                variant="outlined"
                color="warning"
                onClick={handleCancelMigration}
                disabled={canceling}
              >
                {canceling ? t('networkShare.migration.canceling') : t('networkShare.migration.abort')}
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                {t('networkShare.migration.abortHelp')}
              </Typography>
            </Box>
          </Box>
        ) : (
          <Stack spacing={2}>
            {migration?.phase === 'completed' && (
              <Alert severity="success">{migration.message || t('networkShare.migration.complete')}</Alert>
            )}
            {migration?.phase === 'failed' && (
              <Alert severity="error">{migration.error || migration.message || t('networkShare.migration.failed')}</Alert>
            )}
            <Box>
              <Button
                variant="contained"
                color={migrateDirection === 'to_local' ? 'warning' : 'primary'}
                onClick={() => setConfirmOpen(true)}
                disabled={starting || !config?.enabled || !health}
              >
                {migrateDirection === 'to_share'
                  ? t('networkShare.migration.migrateToShare')
                  : t('networkShare.migration.migrateToLocal')}
              </Button>
              {(!config?.enabled || !health) && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  {t('networkShare.migration.enableFirst')}
                </Typography>
              )}
            </Box>
          </Stack>
        )}
      </Paper>

      {/* Section B: On-prem agent mount command — coordinate fields only */}
      <Paper variant="outlined" sx={{ p: 3, mt: 3 }}>
        <Typography variant="subtitle1" gutterBottom>
          {t('networkShare.agent.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          <Trans t={t} i18nKey="networkShare.agent.description" components={{ b: <b /> }} />
        </Typography>

        <Stack spacing={2} sx={{ maxWidth: 640 }}>
          <TextField
            select
            label={t('networkShare.agent.protocol')}
            value={shareType}
            onChange={(e) => setShareType(e.target.value as 'smb' | 'nfs')}
            disabled={migrationInProgress}
            helperText={t('networkShare.agent.protocolHelp')}
          >
            <MenuItem value="smb">{t('networkShare.agent.smb')}</MenuItem>
            <MenuItem value="nfs">{t('networkShare.agent.nfs')}</MenuItem>
          </TextField>
          <TextField
            label={t('networkShare.agent.host')}
            value={serverHost}
            onChange={(e) => setServerHost(e.target.value)}
            placeholder="10.8.0.1"
            disabled={migrationInProgress}
            helperText={t('networkShare.agent.hostHelp')}
          />
          <TextField
            label={shareType === 'nfs' ? t('networkShare.agent.exportPath') : t('networkShare.agent.shareName')}
            value={shareName}
            onChange={(e) => setShareName(e.target.value)}
            placeholder={shareType === 'nfs' ? '/export/krakenhashes' : 'krakenhashes'}
            disabled={migrationInProgress}
          />
          <TextField
            label={t('networkShare.agent.mountOptions')}
            value={optionsText}
            onChange={(e) => setOptionsText(e.target.value)}
            multiline
            minRows={2}
            placeholder={shareType === 'nfs' ? 'vers=4.1\nro' : 'vers=3.1.1'}
            disabled={migrationInProgress}
          />
        </Stack>

        <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1 }}>
          {t('networkShare.agent.runThis')}
        </Typography>
        <Box
          component="pre"
          sx={{
            p: 2,
            bgcolor: 'action.hover',
            borderRadius: 1,
            overflowX: 'auto',
            fontSize: 13,
            m: 0,
          }}
        >
          {mountCommand}
        </Box>
        <Alert severity="warning" sx={{ mt: 2 }}>
          <Trans
            t={t}
            i18nKey={shareType !== 'nfs' ? 'networkShare.agent.replaceSmb' : 'networkShare.agent.replaceNfs'}
            shouldUnescape
            components={{ code: <code /> }}
          />
        </Alert>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          <Trans
            t={t}
            i18nKey={shareType === 'nfs' ? 'networkShare.agent.softNfs' : 'networkShare.agent.softSmb'}
            components={{ code: <code />, b: <b /> }}
          />
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          <Trans
            t={t}
            i18nKey="networkShare.agent.otherOs"
            shouldUnescape
            values={{
              uncPath: escapeForTrans(`\\\\${serverHost || '<HOST>'}\\${shareName || '<SHARE>'}`),
            }}
            components={{ code: <code />, b: <b /> }}
          />
        </Typography>
      </Paper>

      <Dialog open={confirmOpen} onClose={() => setConfirmOpen(false)}>
        <DialogTitle>{t('networkShare.confirm.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText component="div">
            {t('networkShare.confirm.intro')}
            <ul>
              <li>{t('networkShare.confirm.step1')}</li>
              <li>{t('networkShare.confirm.step2')}</li>
              <li>
                {migrateDirection === 'to_share'
                  ? t('networkShare.confirm.step3ToShare')
                  : t('networkShare.confirm.step3ToLocal')}
              </li>
            </ul>
            {t('networkShare.confirm.outro')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmOpen(false)}>{t('networkShare.confirm.cancel')}</Button>
          <Button onClick={handleMigrate} variant="contained" color="warning">
            {t('networkShare.confirm.start')}
          </Button>
        </DialogActions>
      </Dialog>

    </Box>
  );
};

export default NetworkShareSettings;
