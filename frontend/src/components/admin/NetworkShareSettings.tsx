import React, { useCallback, useEffect, useState } from 'react';
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
  Snackbar,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import StorageIcon from '@mui/icons-material/Storage';
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

const NetworkShareSettings: React.FC = () => {
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
  const [snack, setSnack] = useState<{ msg: string; severity: 'success' | 'error' } | null>(null);
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
      setLoadError(e?.response?.data?.error || e?.message || 'Failed to load network share config');
    } finally {
      setLoading(false);
    }
  }, [applyConfig]);

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
      setSnack({ msg: 'Migration started.', severity: 'success' });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || 'Failed to start migration',
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
      setSnack({ msg: 'Migration canceled — storage left unchanged.', severity: 'success' });
      // Refresh config so the locked UI releases once the engine unwinds.
      load();
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || 'Failed to cancel migration',
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
      setSnack({ msg: 'Network share configuration saved.', severity: 'success' });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || 'Failed to save configuration',
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
        msg: res.ok ? 'Share validated successfully.' : 'Share validation reported problems.',
        severity: res.ok ? 'success' : 'error',
      });
    } catch (e: any) {
      setSnack({
        msg: e?.response?.data?.error || 'Validation failed to run',
        severity: 'error',
      });
    } finally {
      setValidating(false);
    }
  };

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
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <StorageIcon color="primary" />
        <Typography variant="h5" component="h2">
          Network Share Storage
        </Typography>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Store wordlists and rules on a network share so the server doesn't need a large local disk.
        The operator mounts the share on the server host (via docker-compose, as{' '}
        <code>KH_SHARE_DIR</code>) and on any network-direct agent host; KrakenHashes only reads the
        mounted path — it never connects to the share itself or stores its credentials. Cloud GPU
        agents don't mount the share: they download wordlists/rules from the server over the VPN.
      </Typography>

      {loadError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {loadError}
        </Alert>
      )}

      {migrationInProgress && (
        <Alert severity="info" sx={{ mb: 2 }}>
          A migration is in progress ({config?.migration_state}). Configuration is locked until it
          completes.
        </Alert>
      )}

      {/* Section A: Server storage mount — read-only status + lifecycle actions */}
      <Paper variant="outlined" sx={{ p: 3 }}>
        <Typography variant="subtitle1" gutterBottom>
          Server storage mount
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          The server reads wordlists and rules from this operator-mounted path (set in docker-compose
          as <code>KH_SHARE_DIR</code>). KrakenHashes does not connect to the share itself — mounting
          is the operator's responsibility.
        </Typography>

        <Stack spacing={1.5} sx={{ mb: 2 }}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Mounted path (read-only)
            </Typography>
            <Typography variant="body2">
              <code>{shareDir || 'KH_SHARE_DIR not set — mount the share in docker-compose'}</code>
            </Typography>
          </Box>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap alignItems="center">
            <Chip
              size="small"
              color={config?.storage_backend === 'share' ? 'primary' : 'default'}
              label={
                config?.storage_backend === 'share'
                  ? 'Serving from: network share'
                  : 'Serving from: local disk'
              }
            />
            <Chip
              size="small"
              icon={health ? <CheckCircleIcon /> : <ErrorIcon />}
              color={health ? 'success' : 'warning'}
              label={health ? 'Share reachable' : 'Share not mounted'}
            />
            {config && config.migration_state !== 'idle' && (
              <Chip size="small" color="info" label={`Migration: ${config.migration_state}`} />
            )}
          </Stack>
          {config?.last_validated_at && (
            <Typography variant="caption" color="text.secondary">
              Last validated {new Date(config.last_validated_at).toLocaleString()}
              {config.last_validation_error ? ` — ${config.last_validation_error}` : ' — OK'}
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
          label="Enable network share"
        />

        <Divider sx={{ my: 2 }} />

        <Stack direction="row" spacing={2}>
          <Button variant="contained" onClick={handleSave} disabled={saving || migrationInProgress}>
            {saving ? 'Saving…' : 'Save configuration'}
          </Button>
          <Button
            variant="outlined"
            onClick={handleValidate}
            disabled={validating || migrationInProgress}
          >
            {validating ? 'Validating…' : 'Validate server mount'}
          </Button>
        </Stack>

        {validation && (
          <Alert severity={validation.ok ? 'success' : 'error'} sx={{ mt: 2 }}>
            <Typography variant="body2">
              Mount path: <code>{validation.share_dir || '(unset)'}</code>
            </Typography>
            {validation.error ? (
              <Typography variant="body2">{validation.error}</Typography>
            ) : (
              <Typography variant="body2">
                Mounted: {validation.mounted ? 'yes' : 'no'} · Writable:{' '}
                {validation.writable ? 'yes' : 'no'} · Free: {formatBytes(validation.free_bytes)} ·
                Write: {validation.write_mbps.toFixed(1)} MB/s · Read:{' '}
                {validation.read_mbps.toFixed(1)} MB/s
              </Typography>
            )}
          </Alert>
        )}

        <Divider sx={{ my: 3 }} />
        <Typography variant="subtitle2" gutterBottom>
          Data migration
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Copy existing wordlists and rules {migrateDirection === 'to_share' ? 'onto the network share' : 'back to local disk'} and switch the
          active storage backend. This is a maintenance operation: new jobs stop dispatching while
          the fleet drains, then wordlist/rule uploads are frozen and files are copied and
          checksum-verified before the switch. Depending on data size this can take a long time and
          the server will be unavailable for uploads and new jobs meanwhile. Client and association
          wordlists, binaries, charsets and hashlists always stay local.
        </Typography>

        {migrationInProgress && migration ? (
          <Box>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1 }}>
              <CircularProgress size={18} />
              <Chip size="small" color="info" label={`Phase: ${migration.phase}`} />
              {migration.direction && (
                <Typography variant="caption" color="text.secondary">
                  {migration.direction === 'to_share' ? 'local → share' : 'share → local'}
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
                  Waiting for {migration.agents_busy} task(s) to finish
                  {migration.drain_deadline
                    ? ` — earliest lock ${new Date(migration.drain_deadline).toLocaleTimeString()}`
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
                  {migration.files_done}/{migration.files_total} files · {formatBytes(migration.bytes_done)} /{' '}
                  {formatBytes(migration.bytes_total)}
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
                {canceling ? 'Canceling…' : 'Abort migration'}
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                Stops the migration at its next checkpoint (useful if the share is slow or hung).
                Storage is left on its current backend and dispatch resumes; already-copied files
                remain and are reused if you retry.
              </Typography>
            </Box>
          </Box>
        ) : (
          <Stack spacing={2}>
            {migration?.phase === 'completed' && (
              <Alert severity="success">{migration.message || 'Migration complete.'}</Alert>
            )}
            {migration?.phase === 'failed' && (
              <Alert severity="error">{migration.error || migration.message || 'Migration failed.'}</Alert>
            )}
            <Box>
              <Button
                variant="contained"
                color={migrateDirection === 'to_local' ? 'warning' : 'primary'}
                onClick={() => setConfirmOpen(true)}
                disabled={starting || !config?.enabled || !health}
              >
                {migrateDirection === 'to_share'
                  ? 'Migrate wordlists & rules to the share'
                  : 'Migrate back to local disk'}
              </Button>
              {(!config?.enabled || !health) && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  Enable the share and confirm it validates before migrating.
                </Typography>
              )}
            </Box>
          </Stack>
        )}
      </Paper>

      {/* Section B: On-prem agent mount command — coordinate fields only */}
      <Paper variant="outlined" sx={{ p: 3, mt: 3 }}>
        <Typography variant="subtitle1" gutterBottom>
          On-prem agent mount command (optional)
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          These fields are used <b>only</b> to generate the mount command for on-prem agents set to the{' '}
          <b>network_direct</b> tier. The server (above) and cloud GPU agents do <b>not</b> use them —
          cloud agents download over the VPN. They are non-secret; credentials are never stored or
          sent.
        </Typography>

        <Stack spacing={2} sx={{ maxWidth: 640 }}>
          <TextField
            select
            label="Protocol"
            value={shareType}
            onChange={(e) => setShareType(e.target.value as 'smb' | 'nfs')}
            disabled={migrationInProgress}
            helperText="SMB is the most portable across Windows/macOS/Linux agents; NFS works well over the VPN with a read-only export."
          >
            <MenuItem value="smb">SMB / CIFS</MenuItem>
            <MenuItem value="nfs">NFS</MenuItem>
          </TextField>
          <TextField
            label="Share host agents mount from (VPN address)"
            value={serverHost}
            onChange={(e) => setServerHost(e.target.value)}
            placeholder="10.8.0.1"
            disabled={migrationInProgress}
            helperText="The share's address as agents reach it over the VPN — not a server setting (the server uses its mounted path above)."
          />
          <TextField
            label={shareType === 'nfs' ? 'Export path' : 'Share name'}
            value={shareName}
            onChange={(e) => setShareName(e.target.value)}
            placeholder={shareType === 'nfs' ? '/export/krakenhashes' : 'krakenhashes'}
            disabled={migrationInProgress}
          />
          <TextField
            label="Mount options (one key=value per line)"
            value={optionsText}
            onChange={(e) => setOptionsText(e.target.value)}
            multiline
            minRows={2}
            placeholder={shareType === 'nfs' ? 'vers=4.1\nro' : 'vers=3.1.1'}
            disabled={migrationInProgress}
          />
        </Stack>

        <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1 }}>
          Run this on the agent host, then set that agent's Storage tier to “Network direct” with the
          mount path on its Agent page:
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
          Replace <code>&lt;MOUNT_PATH&gt;</code>
          {shareType !== 'nfs' && (
            <>
              , <code>&lt;USERNAME&gt;</code> and <code>&lt;PASSWORD&gt;</code>
            </>
          )}{' '}
          before running — the command still contains placeholders.
        </Alert>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          The command includes <code>soft</code>
          {shareType === 'nfs' && (
            <>
              , <code>timeo</code> and <code>retrans</code>
            </>
          )}{' '}
          so the mount fails fast if the share is slow or drops, instead of hanging
          (the kernel's <code>hard</code> default). This pairs with the agent's fail-closed
          share check, which reroutes work when the mount isn't readable.{' '}
          <b>network_direct</b> is for low-latency/LAN-adjacent shares — put remote or
          high-latency shares on the <b>on_demand</b>/<b>full_cache</b> download tiers instead.
          Override any option by setting it above.
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          macOS: use <code>mount_smbfs</code> (SMB) or <code>mount -t nfs</code> (NFS). Windows: use{' '}
          <code>net use</code> against <code>\\{serverHost || '&lt;HOST&gt;'}\{shareName || '&lt;SHARE&gt;'}</code>. The agent only ever reads from the mount. These fields are saved with{' '}
          <b>Save configuration</b> above.
        </Typography>
      </Paper>

      <Dialog open={confirmOpen} onClose={() => setConfirmOpen(false)}>
        <DialogTitle>Start data migration?</DialogTitle>
        <DialogContent>
          <DialogContentText component="div">
            This will:
            <ul>
              <li>Stop dispatching new jobs and wait for running tasks to finish (up to ~12 minutes for agents to reconnect, longer if long jobs are running).</li>
              <li>Freeze wordlist/rule uploads and serve a maintenance page while files are copied and checksum-verified.</li>
              <li>Switch the active storage backend to{' '}
                {migrateDirection === 'to_share' ? 'the network share' : 'local disk'} when finished.</li>
            </ul>
            Paused jobs resume automatically afterward. If anything fails, storage is left unchanged
            and the server stays operational.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmOpen(false)}>Cancel</Button>
          <Button onClick={handleMigrate} variant="contained" color="warning">
            Start migration
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={!!snack}
        autoHideDuration={5000}
        onClose={() => setSnack(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
      >
        {snack ? (
          <Alert severity={snack.severity} onClose={() => setSnack(null)} sx={{ width: '100%' }}>
            {snack.msg}
          </Alert>
        ) : undefined}
      </Snackbar>
    </Box>
  );
};

export default NetworkShareSettings;
