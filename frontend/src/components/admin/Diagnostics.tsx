import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Paper,
  Typography,
  Button,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  IconButton,
  Tooltip,
  CircularProgress,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Switch,
  FormControlLabel,
  Divider,
  Card,
  CardContent,
  Grid,
  TextField,
  MenuItem,
  Backdrop
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import DownloadIcon from '@mui/icons-material/Download';
import BugReportIcon from '@mui/icons-material/BugReport';
import DeleteIcon from '@mui/icons-material/Delete';
import VisibilityIcon from '@mui/icons-material/Visibility';
import HelpOutlineIcon from '@mui/icons-material/HelpOutline';
import WarningIcon from '@mui/icons-material/Warning';
import { useTranslation } from 'react-i18next';
import {
  AgentDebugStatus,
  SystemInfoResponse,
  AgentLogsResponse,
  LogEntry,
  ServerDebugStatus,
  AllLogStats
} from '../../types/diagnostics';
import {
  getSystemInfo,
  getAgentDebugStatuses,
  toggleAgentDebug,
  toggleAllAgentsDebug,
  requestAgentLogs,
  purgeAgentLogs,
  downloadDiagnosticsFile,
  getServerDebugStatus,
  toggleServerDebug,
  getLogStats,
  purgeServerLogs,
  checkPostgresLogsExist,
  checkNginxLogsExist,
  reloadNginx
} from '../../services/diagnostics';

const Diagnostics: React.FC = () => {
  const { t } = useTranslation('admin');
  const [systemInfo, setSystemInfo] = useState<SystemInfoResponse | null>(null);
  const [agents, setAgents] = useState<AgentDebugStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [includeAgentLogs, setIncludeAgentLogs] = useState(true);
  const [downloadHoursBack, setDownloadHoursBack] = useState(1);

  // Sensitive log toggles (default: NOT included for security)
  const [includeNginxLogs, setIncludeNginxLogs] = useState(false);
  const [includePostgresLogs, setIncludePostgresLogs] = useState(false);

  // Confirmation dialog for sensitive logs
  const [sensitiveLogDialog, setSensitiveLogDialog] = useState<{
    open: boolean;
    type: 'nginx' | 'postgres' | null;
  }>({ open: false, type: null });

  // Server debug status
  const [serverDebugStatus, setServerDebugStatus] = useState<ServerDebugStatus | null>(null);
  const [serverDebugLoading, setServerDebugLoading] = useState(false);

  // Server log stats
  const [logStats, setLogStats] = useState<AllLogStats | null>(null);
  const [logStatsLoading, setLogStatsLoading] = useState(false);
  const [purging, setPurging] = useState<string | null>(null);
  const [postgresLogsExist, setPostgresLogsExist] = useState(false);
  const [nginxLogsExist, setNginxLogsExist] = useState(false);

  // Reconnection state (for nginx reload during debug toggle)
  const [reconnecting, setReconnecting] = useState(false);

  // Confirmation dialog state
  const [confirmDialog, setConfirmDialog] = useState<{
    open: boolean;
    title: string;
    message: string;
    warningMessage?: string;
    confirmText: string;
    onConfirm: () => void;
  }>({ open: false, title: '', message: '', confirmText: 'Confirm', onConfirm: () => {} });

  // Log viewer dialog
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [selectedAgentLogs, setSelectedAgentLogs] = useState<AgentLogsResponse | null>(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState<number | null>(null);
  const [logLevelFilter, setLogLevelFilter] = useState<string>('ALL');

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [sysInfo, agentStatuses, serverStatus, stats, pgLogsCheck, nginxLogsCheck] = await Promise.all([
        getSystemInfo(),
        getAgentDebugStatuses(),
        getServerDebugStatus(),
        getLogStats(),
        checkPostgresLogsExist(),
        checkNginxLogsExist()
      ]);
      setSystemInfo(sysInfo);
      setAgents(agentStatuses.agents || []);
      setServerDebugStatus(serverStatus);
      setLogStats(stats);
      setPostgresLogsExist(pgLogsCheck.exists);
      setNginxLogsExist(nginxLogsCheck.exists);
    } catch (err) {
      setError(t('diagnostics.messages.loadFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
    } finally {
      setLoading(false);
    }
  }, [t]);

  const fetchLogStats = useCallback(async () => {
    setLogStatsLoading(true);
    try {
      const [stats, pgLogsCheck] = await Promise.all([
        getLogStats(),
        checkPostgresLogsExist()
      ]);
      setLogStats(stats);
      setPostgresLogsExist(pgLogsCheck.exists);
    } catch (err) {
      setError(t('diagnostics.messages.logStatsFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
    } finally {
      setLogStatsLoading(false);
    }
  }, [t]);

  const handlePurgeServerLogs = async (directory: 'backend' | 'nginx' | 'postgres' | 'all') => {
    const dirLabel = directory === 'all' ? t('diagnostics.allServerLogs') as string : t('diagnostics.directoryLogs', { directory }) as string;
    setConfirmDialog({
      open: true,
      title: t('diagnostics.purgeServerLogs') as string,
      message: t('diagnostics.confirmPurgeLogs', { dirLabel }) as string,
      warningMessage: t('diagnostics.cannotBeUndone') as string,
      confirmText: t('diagnostics.purge') as string,
      onConfirm: async () => {
        setConfirmDialog(prev => ({ ...prev, open: false }));
        setPurging(directory);
        try {
          await purgeServerLogs(directory);
          await fetchLogStats();
        } catch (err) {
          setError(t('diagnostics.messages.purgeFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
        } finally {
          setPurging(null);
        }
      }
    });
  };

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleToggleServerDebug = async () => {
    if (!serverDebugStatus) return;

    const newState = !serverDebugStatus.enabled;
    const action = newState ? t('diagnostics.enable') as string : t('diagnostics.disable') as string;

    setConfirmDialog({
      open: true,
      title: t('diagnostics.serverDebugModeTitle', { action }) as string,
      message: t('diagnostics.serverDebugModeMessage') as string,
      warningMessage: t('diagnostics.serverDebugModeWarning') as string,
      confirmText: action,
      onConfirm: async () => {
        setConfirmDialog(prev => ({ ...prev, open: false }));
        setServerDebugLoading(true);
        try {
          const newStatus = await toggleServerDebug(newState);
          setServerDebugStatus(newStatus);

          // Reload nginx to apply logging changes and show reconnection overlay
          await reloadNginx();
          setReconnecting(true);

          // Poll until server responds (max 30 seconds)
          const maxAttempts = 30;
          let attempts = 0;
          const checkConnection = async (): Promise<boolean> => {
            try {
              await getSystemInfo();
              return true;
            } catch {
              return false;
            }
          };

          while (attempts < maxAttempts) {
            await new Promise(resolve => setTimeout(resolve, 1000));
            if (await checkConnection()) {
              setReconnecting(false);
              // Refresh all data after successful reconnection
              await fetchData();
              return;
            }
            attempts++;
          }

          // If we get here, reconnection failed
          setReconnecting(false);
          setError(t('diagnostics.messages.reconnectionTimedOut') as string);
        } catch (err) {
          setError(t('diagnostics.messages.toggleDebugFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
        } finally {
          setServerDebugLoading(false);
        }
      }
    });
  };

  const handleDownload = async () => {
    setDownloading(true);
    try {
      await downloadDiagnosticsFile(includeAgentLogs, downloadHoursBack, includeNginxLogs, includePostgresLogs);
    } catch (err) {
      setError(t('diagnostics.messages.downloadFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
    } finally {
      setDownloading(false);
    }
  };

  // Handler for sensitive log toggle clicks (shows confirmation dialog)
  const handleSensitiveLogToggle = (type: 'nginx' | 'postgres', checked: boolean) => {
    if (checked) {
      // Show confirmation dialog before enabling
      setSensitiveLogDialog({ open: true, type });
    } else {
      // Can disable without confirmation
      if (type === 'nginx') setIncludeNginxLogs(false);
      else setIncludePostgresLogs(false);
    }
  };

  const handleConfirmSensitiveLogs = () => {
    if (sensitiveLogDialog.type === 'nginx') {
      setIncludeNginxLogs(true);
    } else if (sensitiveLogDialog.type === 'postgres') {
      setIncludePostgresLogs(true);
    }
    setSensitiveLogDialog({ open: false, type: null });
  };

  const handleCancelSensitiveLogs = () => {
    setSensitiveLogDialog({ open: false, type: null });
  };

  const handleToggleDebug = async (agentId: number, currentEnabled: boolean) => {
    try {
      await toggleAgentDebug(agentId, !currentEnabled);
      // Refresh agent statuses
      const response = await getAgentDebugStatuses();
      setAgents(response.agents || []);
    } catch (err) {
      setError(t('diagnostics.messages.toggleAgentDebugFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
    }
  };

  const handleToggleAllDebug = async (enable: boolean) => {
    try {
      await toggleAllAgentsDebug(enable);
      // Refresh agent statuses
      const response = await getAgentDebugStatuses();
      setAgents(response.agents || []);
    } catch (err) {
      setError(t('diagnostics.messages.toggleAllAgentsDebugFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
    }
  };

  const handleViewLogs = async (agentId: number) => {
    setLogsLoading(true);
    setLogDialogOpen(true);
    setSelectedAgentId(agentId);
    setLogLevelFilter('ALL'); // Reset filter when opening new logs
    try {
      // Request all buffered logs (buffer is limited to 1000 entries)
      const logs = await requestAgentLogs(agentId, 168, false); // 168 hours = 7 days to get all buffer
      setSelectedAgentLogs(logs);
    } catch (err) {
      setError(t('diagnostics.messages.fetchAgentLogsFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
      setLogDialogOpen(false);
    } finally {
      setLogsLoading(false);
    }
  };

  const handleRefreshLogs = async () => {
    if (selectedAgentId !== null) {
      await handleViewLogs(selectedAgentId);
    }
  };

  const handlePurgeLogs = async (agentId: number) => {
    setConfirmDialog({
      open: true,
      title: t('diagnostics.purgeAgentLogs') as string,
      message: t('diagnostics.confirmPurgeAgentLogs', { agentId }) as string,
      warningMessage: t('diagnostics.cannotBeUndone') as string,
      confirmText: t('diagnostics.purge') as string,
      onConfirm: async () => {
        setConfirmDialog(prev => ({ ...prev, open: false }));
        try {
          await purgeAgentLogs(agentId);
          // Refresh agent statuses
          const response = await getAgentDebugStatuses();
          setAgents(response.agents || []);
        } catch (err) {
          setError(t('diagnostics.messages.purgeAgentLogsFailed') + ' ' + (err instanceof Error ? err.message : String(err)));
        }
      }
    });
  };

  const formatTimestamp = (ts: number): string => {
    return new Date(ts).toLocaleString();
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  };

  return (
    <Box>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {postgresLogsExist && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          <strong>{t('diagnostics.securityWarning') as string}:</strong> {t('diagnostics.postgresLogsWarning') as string}
        </Alert>
      )}

      {nginxLogsExist && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          <strong>{t('diagnostics.securityWarning') as string}:</strong> {t('diagnostics.nginxLogsWarning') as string}
        </Alert>
      )}

      {/* Download Section */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">{t('diagnostics.diagnosticPackage') as string}</Typography>
          <Tooltip title={t('diagnostics.refreshData') as string}>
            <IconButton onClick={fetchData} disabled={loading}>
              <RefreshIcon />
            </IconButton>
          </Tooltip>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('diagnostics.diagnosticPackageDescription') as string}
        </Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, flexWrap: 'wrap' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <TextField
              type="number"
              label={t('diagnostics.hours') as string}
              value={downloadHoursBack || ''}
              onChange={(e) => {
                const val = parseInt(e.target.value, 10);
                if (!isNaN(val) && val >= 1) {
                  setDownloadHoursBack(val);
                } else {
                  // Allow empty/invalid while typing - will validate on blur
                  setDownloadHoursBack(0);
                }
              }}
              onBlur={(e) => {
                // Enforce minimum of 1 when field loses focus
                const val = parseInt(e.target.value, 10);
                if (isNaN(val) || val < 1) {
                  setDownloadHoursBack(1);
                }
              }}
              size="small"
              sx={{ width: 80 }}
              inputProps={{ min: 1, step: 1 }}
            />
            <Tooltip title={t('diagnostics.hoursTooltip') as string}>
              <HelpOutlineIcon sx={{ fontSize: 18, color: 'text.secondary', cursor: 'help' }} />
            </Tooltip>
          </Box>
          <FormControlLabel
            control={
              <Switch
                checked={includeAgentLogs}
                onChange={(e) => setIncludeAgentLogs(e.target.checked)}
              />
            }
            label={t('diagnostics.includeAgentLogs') as string}
          />
          <Box sx={{ display: 'flex', alignItems: 'center' }}>
            <FormControlLabel
              control={
                <Switch
                  checked={includeNginxLogs}
                  onChange={(e) => handleSensitiveLogToggle('nginx', e.target.checked)}
                />
              }
              label={t('diagnostics.includeNginxLogs') as string}
            />
            {includeNginxLogs && (
              <Tooltip title={t('diagnostics.nginxLogsTooltip') as string}>
                <WarningIcon color="warning" sx={{ ml: -1 }} />
              </Tooltip>
            )}
          </Box>
          <Box sx={{ display: 'flex', alignItems: 'center' }}>
            <FormControlLabel
              control={
                <Switch
                  checked={includePostgresLogs}
                  onChange={(e) => handleSensitiveLogToggle('postgres', e.target.checked)}
                />
              }
              label={t('diagnostics.includePostgresLogs') as string}
            />
            {includePostgresLogs && (
              <Tooltip title={t('diagnostics.postgresLogsTooltip') as string}>
                <WarningIcon color="warning" sx={{ ml: -1 }} />
              </Tooltip>
            )}
          </Box>
          <Button
            variant="contained"
            startIcon={downloading ? <CircularProgress size={20} /> : <DownloadIcon />}
            onClick={handleDownload}
            disabled={downloading}
          >
            {downloading ? t('diagnostics.preparing') as string : t('diagnostics.downloadDiagnostics') as string}
          </Button>
        </Box>
      </Paper>

      {/* System Info */}
      {systemInfo && (
        <Paper sx={{ p: 3, mb: 3 }}>
          <Typography variant="h6" gutterBottom>{t('diagnostics.systemInfo') as string}</Typography>
          <Grid container spacing={2}>
            <Grid item xs={12} md={6}>
              <Card variant="outlined">
                <CardContent>
                  <Typography variant="subtitle2" color="text.secondary">{t('diagnostics.runtime') as string}</Typography>
                  <Typography variant="body2">
                    Go {systemInfo.system_info.go_version} ({systemInfo.system_info.go_os}/{systemInfo.system_info.go_arch})
                  </Typography>
                  <Typography variant="body2">
                    {t('diagnostics.cpus') as string}: {systemInfo.system_info.num_cpu} | {t('diagnostics.goroutines') as string}: {systemInfo.system_info.num_goroutine}
                  </Typography>
                </CardContent>
              </Card>
            </Grid>
            <Grid item xs={12} md={6}>
              <Card variant="outlined">
                <CardContent>
                  <Typography variant="subtitle2" color="text.secondary">{t('diagnostics.memory') as string}</Typography>
                  <Typography variant="body2">
                    {t('diagnostics.heap') as string}: {systemInfo.system_info.memory?.heap_alloc_mb} MB |
                    {t('diagnostics.system') as string}: {systemInfo.system_info.memory?.sys_mb} MB
                  </Typography>
                  <Typography variant="body2">
                    {t('diagnostics.gcRuns') as string}: {systemInfo.system_info.memory?.num_gc}
                  </Typography>
                </CardContent>
              </Card>
            </Grid>
            {systemInfo.system_info.database && (
              <>
                <Grid item xs={12} md={6}>
                  <Card variant="outlined">
                    <CardContent>
                      <Typography variant="subtitle2" color="text.secondary">{t('diagnostics.database') as string}</Typography>
                      <Typography variant="body2">
                        {t('diagnostics.size') as string}: {systemInfo.system_info.database.database_size}
                      </Typography>
                      <Typography variant="body2">
                        {t('diagnostics.connections') as string}: {systemInfo.system_info.database.connection_stats?.open_connections} /
                        {systemInfo.system_info.database.connection_stats?.max_open}
                      </Typography>
                    </CardContent>
                  </Card>
                </Grid>
                <Grid item xs={12} md={6}>
                  <Card variant="outlined">
                    <CardContent>
                      <Typography variant="subtitle2" color="text.secondary">{t('diagnostics.agents') as string}</Typography>
                      <Typography variant="body2">
                        {t('diagnostics.connected') as string}: {systemInfo.system_info.connected_agents || 0}
                      </Typography>
                      <Typography variant="body2">
                        {t('diagnostics.withDebugStatus') as string}: {systemInfo.agent_statuses}
                      </Typography>
                    </CardContent>
                  </Card>
                </Grid>
              </>
            )}
          </Grid>
        </Paper>
      )}

      {/* Server Debug Status */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">{t('diagnostics.serverDebugStatus') as string}</Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('diagnostics.serverDebugDescription') as string}
        </Typography>
        {serverDebugStatus ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <Chip
              label={serverDebugStatus.enabled ? t('diagnostics.debugEnabled') as string : t('diagnostics.debugDisabled') as string}
              color={serverDebugStatus.enabled ? 'success' : 'default'}
              size="small"
            />
            <Typography variant="body2" color="text.secondary">
              {t('diagnostics.level') as string}: {serverDebugStatus.level}
            </Typography>
            <Button
              variant={serverDebugStatus.enabled ? 'outlined' : 'contained'}
              color={serverDebugStatus.enabled ? 'warning' : 'success'}
              size="small"
              startIcon={serverDebugLoading ? <CircularProgress size={16} /> : <BugReportIcon />}
              onClick={handleToggleServerDebug}
              disabled={serverDebugLoading}
            >
              {serverDebugStatus.enabled ? t('diagnostics.disableDebug') as string : t('diagnostics.enableDebug') as string}
            </Button>
          </Box>
        ) : (
          <Typography color="text.secondary">{t('diagnostics.loadingServerDebugStatus') as string}</Typography>
        )}
      </Paper>

      {/* Server Logs Stats */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">{t('diagnostics.serverLogs') as string}</Typography>
          <Tooltip title={t('diagnostics.refreshLogStats') as string}>
            <IconButton onClick={fetchLogStats} disabled={logStatsLoading}>
              {logStatsLoading ? <CircularProgress size={20} /> : <RefreshIcon />}
            </IconButton>
          </Tooltip>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('diagnostics.serverLogsDescription') as string}
        </Typography>
        {logStats ? (
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('diagnostics.directory') as string}</TableCell>
                  <TableCell align="right">{t('diagnostics.files') as string}</TableCell>
                  <TableCell align="right">{t('diagnostics.size') as string}</TableCell>
                  <TableCell align="right">{t('diagnostics.actions') as string}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                <TableRow>
                  <TableCell>{t('diagnostics.backend') as string}</TableCell>
                  <TableCell align="right">{logStats.backend.files}</TableCell>
                  <TableCell align="right">{formatBytes(logStats.backend.size)}</TableCell>
                  <TableCell align="right">
                    <Button
                      size="small"
                      color="error"
                      startIcon={purging === 'backend' ? <CircularProgress size={14} /> : <DeleteIcon />}
                      onClick={() => handlePurgeServerLogs('backend')}
                      disabled={purging !== null || logStats.backend.files === 0}
                    >
                      {t('diagnostics.purge') as string}
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>{t('diagnostics.nginx') as string}</TableCell>
                  <TableCell align="right">{logStats.nginx.files}</TableCell>
                  <TableCell align="right">{formatBytes(logStats.nginx.size)}</TableCell>
                  <TableCell align="right">
                    <Button
                      size="small"
                      color="error"
                      startIcon={purging === 'nginx' ? <CircularProgress size={14} /> : <DeleteIcon />}
                      onClick={() => handlePurgeServerLogs('nginx')}
                      disabled={purging !== null || logStats.nginx.files === 0}
                    >
                      {t('diagnostics.purge') as string}
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell>{t('diagnostics.postgresql') as string}</TableCell>
                  <TableCell align="right">{logStats.postgres.files}</TableCell>
                  <TableCell align="right">{formatBytes(logStats.postgres.size)}</TableCell>
                  <TableCell align="right">
                    <Button
                      size="small"
                      color="error"
                      startIcon={purging === 'postgres' ? <CircularProgress size={14} /> : <DeleteIcon />}
                      onClick={() => handlePurgeServerLogs('postgres')}
                      disabled={purging !== null || logStats.postgres.files === 0}
                    >
                      {t('diagnostics.purge') as string}
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow sx={{ '& td': { fontWeight: 'bold', borderTop: '2px solid', borderColor: 'divider' } }}>
                  <TableCell>{t('diagnostics.total') as string}</TableCell>
                  <TableCell align="right">
                    {logStats.backend.files + logStats.nginx.files + logStats.postgres.files}
                  </TableCell>
                  <TableCell align="right">
                    {formatBytes(logStats.backend.size + logStats.nginx.size + logStats.postgres.size)}
                  </TableCell>
                  <TableCell align="right">
                    <Button
                      size="small"
                      color="error"
                      variant="contained"
                      startIcon={purging === 'all' ? <CircularProgress size={14} color="inherit" /> : <DeleteIcon />}
                      onClick={() => handlePurgeServerLogs('all')}
                      disabled={purging !== null || (logStats.backend.files + logStats.nginx.files + logStats.postgres.files) === 0}
                    >
                      {t('diagnostics.purgeAll') as string}
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </TableContainer>
        ) : (
          <Typography color="text.secondary">{t('diagnostics.loadingLogStatistics') as string}</Typography>
        )}
      </Paper>

      {/* Agent Debug Status */}
      <Paper sx={{ p: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">{t('diagnostics.agentDebugStatus') as string}</Typography>
          <Box sx={{ display: 'flex', gap: 1 }}>
            <Button
              size="small"
              variant="outlined"
              startIcon={<BugReportIcon />}
              onClick={() => handleToggleAllDebug(true)}
            >
              {t('diagnostics.enableAll') as string}
            </Button>
            <Button
              size="small"
              variant="outlined"
              color="warning"
              onClick={() => handleToggleAllDebug(false)}
            >
              {t('diagnostics.disableAll') as string}
            </Button>
          </Box>
        </Box>

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
            <CircularProgress />
          </Box>
        ) : agents.length === 0 ? (
          <Typography color="text.secondary" sx={{ textAlign: 'center', py: 3 }}>
            {t('diagnostics.noAgentsWithDebugStatus') as string}
          </Typography>
        ) : (
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('diagnostics.agentId') as string}</TableCell>
                  <TableCell>{t('diagnostics.debugEnabled') as string}</TableCell>
                  <TableCell>{t('diagnostics.logLevel') as string}</TableCell>
                  <TableCell>{t('diagnostics.fileLogging') as string}</TableCell>
                  <TableCell>{t('diagnostics.logFileSize') as string}</TableCell>
                  <TableCell>{t('diagnostics.buffer') as string}</TableCell>
                  <TableCell>{t('diagnostics.lastUpdated') as string}</TableCell>
                  <TableCell>{t('diagnostics.actions') as string}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {agents.map((agent) => (
                  <TableRow key={agent.agent_id}>
                    <TableCell>{agent.agent_id}</TableCell>
                    <TableCell>
                      <Chip
                        label={agent.enabled ? t('common.on') as string : t('common.off') as string}
                        color={agent.enabled ? 'success' : 'default'}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>{agent.level}</TableCell>
                    <TableCell>
                      <Chip
                        label={agent.file_logging_enabled ? t('common.yes') as string : t('common.no') as string}
                        color={agent.file_logging_enabled ? 'info' : 'default'}
                        size="small"
                        variant="outlined"
                      />
                    </TableCell>
                    <TableCell>
                      {agent.log_file_exists
                        ? formatBytes(agent.log_file_size)
                        : '-'}
                    </TableCell>
                    <TableCell>
                      {agent.buffer_count}/{agent.buffer_capacity}
                    </TableCell>
                    <TableCell>
                      {new Date(agent.last_updated).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      <Tooltip title={agent.enabled ? t('diagnostics.disableDebug') as string : t('diagnostics.enableDebug') as string}>
                        <IconButton
                          size="small"
                          color={agent.enabled ? 'warning' : 'success'}
                          onClick={() => handleToggleDebug(agent.agent_id, agent.enabled)}
                        >
                          <BugReportIcon />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={t('diagnostics.viewLogs') as string}>
                        <IconButton
                          size="small"
                          onClick={() => handleViewLogs(agent.agent_id)}
                        >
                          <VisibilityIcon />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={t('diagnostics.purgeLogs') as string}>
                        <IconButton
                          size="small"
                          color="error"
                          onClick={() => handlePurgeLogs(agent.agent_id)}
                        >
                          <DeleteIcon />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Paper>

      {/* Log Viewer Dialog */}
      <Dialog
        open={logDialogOpen}
        onClose={() => setLogDialogOpen(false)}
        maxWidth="lg"
        fullWidth
      >
        <DialogTitle>
          {t('diagnostics.agentLogs') as string} {selectedAgentLogs && `(${t('diagnostics.agent') as string} ${selectedAgentLogs.agent_id})`}
        </DialogTitle>
        <DialogContent>
          {/* Log Level Filter */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2, mt: 1 }}>
            <TextField
              select
              label={t('diagnostics.level') as string}
              value={logLevelFilter}
              onChange={(e) => setLogLevelFilter(e.target.value)}
              size="small"
              sx={{ minWidth: 130 }}
            >
              <MenuItem value="ALL">{t('diagnostics.allLevels') as string}</MenuItem>
              <MenuItem value="DEBUG">DEBUG</MenuItem>
              <MenuItem value="INFO">INFO</MenuItem>
              <MenuItem value="WARNING">WARNING</MenuItem>
              <MenuItem value="ERROR">ERROR</MenuItem>
            </TextField>
            <Button
              variant="outlined"
              size="small"
              startIcon={<RefreshIcon />}
              onClick={handleRefreshLogs}
              disabled={logsLoading}
            >
              {t('diagnostics.refresh') as string}
            </Button>
          </Box>

          {logsLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
              <CircularProgress />
            </Box>
          ) : selectedAgentLogs ? (
            <Box>
              {(() => {
                const filteredEntries = selectedAgentLogs.entries?.filter(entry =>
                  logLevelFilter === 'ALL' || entry.level === logLevelFilter
                ) || [];
                return (
                  <>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      {t('diagnostics.showingEntries', { shown: filteredEntries.length, total: selectedAgentLogs.total_count }) as string}
                      {logLevelFilter !== 'ALL' && ` (${t('diagnostics.filtered') as string}: ${logLevelFilter})`}
                      {selectedAgentLogs.truncated && ` (${t('diagnostics.truncated') as string})`}
                    </Typography>
                    {filteredEntries.length > 0 ? (
                      <TableContainer component={Paper} sx={{ maxHeight: 400 }}>
                        <Table size="small" stickyHeader>
                          <TableHead>
                            <TableRow>
                              <TableCell>{t('diagnostics.time') as string}</TableCell>
                              <TableCell>{t('diagnostics.level') as string}</TableCell>
                              <TableCell>{t('diagnostics.message') as string}</TableCell>
                              <TableCell>{t('diagnostics.location') as string}</TableCell>
                            </TableRow>
                          </TableHead>
                          <TableBody>
                            {filteredEntries.map((entry: LogEntry, idx: number) => (
                              <TableRow key={idx}>
                                <TableCell sx={{ whiteSpace: 'nowrap' }}>
                                  {formatTimestamp(entry.timestamp)}
                                </TableCell>
                                <TableCell>
                                  <Chip
                                    label={entry.level}
                                    size="small"
                                    color={
                                      entry.level === 'ERROR' ? 'error' :
                                      entry.level === 'WARNING' ? 'warning' :
                                      entry.level === 'DEBUG' ? 'secondary' : 'default'
                                    }
                                  />
                                </TableCell>
                                <TableCell sx={{ maxWidth: 400, wordBreak: 'break-word' }}>
                                  {entry.message}
                                </TableCell>
                                <TableCell sx={{ whiteSpace: 'nowrap', fontSize: '0.75rem' }}>
                                  {entry.file && `${entry.file}:${entry.line}`}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </TableContainer>
                    ) : (
                      <Typography color="text.secondary">
                        {logLevelFilter !== 'ALL'
                          ? t('diagnostics.noEntriesForLevel', { level: logLevelFilter }) as string
                          : t('diagnostics.noLogEntriesAvailable') as string}
                      </Typography>
                    )}
                  </>
                );
              })()}
              {selectedAgentLogs.file_content && (
                <>
                  <Divider sx={{ my: 2 }} />
                  <Typography variant="subtitle2" gutterBottom>{t('diagnostics.logFileContent') as string}</Typography>
                  <Paper
                    sx={{
                      p: 1,
                      maxHeight: 300,
                      overflow: 'auto',
                      fontFamily: 'monospace',
                      fontSize: '0.75rem',
                      whiteSpace: 'pre-wrap',
                      bgcolor: 'grey.900',
                      color: 'grey.100'
                    }}
                  >
                    {selectedAgentLogs.file_content}
                  </Paper>
                </>
              )}
            </Box>
          ) : (
            <Typography color="text.secondary">{t('diagnostics.noLogsAvailable') as string}</Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setLogDialogOpen(false)}>{t('common.close') as string}</Button>
        </DialogActions>
      </Dialog>

      {/* Sensitive Log Confirmation Dialog */}
      <Dialog open={sensitiveLogDialog.open} onClose={handleCancelSensitiveLogs}>
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <WarningIcon color="warning" />
            {t('diagnostics.securityWarning') as string}
          </Box>
        </DialogTitle>
        <DialogContent>
          <Typography>
            {sensitiveLogDialog.type === 'nginx'
              ? t('diagnostics.sensitiveNginxLogsWarning') as string
              : t('diagnostics.sensitivePostgresLogsWarning') as string}
          </Typography>
          <Typography sx={{ mt: 2 }}>
            {t('diagnostics.sensitiveLogsTestEnvironmentWarning') as string}
          </Typography>
          <Typography sx={{ mt: 2, fontWeight: 'bold', color: 'warning.main' }}>
            {t('diagnostics.sensitiveLogsProductionWarning') as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCancelSensitiveLogs}>{t('common.cancel') as string}</Button>
          <Button onClick={handleConfirmSensitiveLogs} color="warning" variant="contained">
            {t('diagnostics.iUnderstandIncludeLogs') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Generic Confirmation Dialog */}
      <Dialog open={confirmDialog.open} onClose={() => setConfirmDialog(prev => ({ ...prev, open: false }))}>
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <WarningIcon color="warning" />
            {confirmDialog.title}
          </Box>
        </DialogTitle>
        <DialogContent>
          <Typography>{confirmDialog.message}</Typography>
          {confirmDialog.warningMessage && (
            <Typography sx={{ mt: 2, fontWeight: 'bold', color: 'warning.main' }}>
              {confirmDialog.warningMessage}
            </Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDialog(prev => ({ ...prev, open: false }))}>{t('common.cancel') as string}</Button>
          <Button onClick={confirmDialog.onConfirm} color="error" variant="contained">
            {confirmDialog.confirmText}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Reconnecting Overlay */}
      <Backdrop
        sx={{
          color: '#fff',
          zIndex: (theme) => theme.zIndex.drawer + 1,
          flexDirection: 'column',
          gap: 2
        }}
        open={reconnecting}
      >
        <CircularProgress color="inherit" />
        <Typography variant="h6">{t('diagnostics.reconnectingToServer') as string}</Typography>
        <Typography variant="body2">{t('diagnostics.pleaseWaitReconnecting') as string}</Typography>
      </Backdrop>
    </Box>
  );
};

export default Diagnostics;
