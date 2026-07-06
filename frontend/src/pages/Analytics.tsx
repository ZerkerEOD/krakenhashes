/**
 * Password Analytics page for KrakenHashes frontend.
 *
 * Features:
 *   - Select client and date range for analysis
 *   - Generate new analytics reports
 *   - View previous reports
 *   - Display comprehensive password analytics
 *   - Queue management and status tracking
 */
import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Typography,
  Paper,
  TextField,
  MenuItem,
  Grid,
  CircularProgress,
  Alert,
  Card,
  CardContent,
  CardHeader,
  Divider,
  Tab,
  Tabs,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Checkbox,
  Chip,
  LinearProgress,
  AlertTitle,
  Tooltip,
} from '@mui/material';
import {
  Add as AddIcon,
  Refresh as RefreshIcon,
  Delete as DeleteIcon,
  Replay as RetryIcon,
  Visibility as VisibilityIcon,
} from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import analyticsService from '../services/analytics';
import { AnalyticsReport, CreateAnalyticsReportRequest, HashlistSummary } from '../types/analytics';
import { api } from '../services/api';
import { getJobDefaultsForUsers } from '../services/jobSettings';

// Import display components
import AnalyticsReportDisplay from '../components/analytics/AnalyticsReportDisplay';

interface Client {
  id: string;
  name: string;
}

export default function Analytics() {
  const { t } = useTranslation('analytics');
  const [clients, setClients] = useState<Client[]>([]);
  const [selectedClient, setSelectedClient] = useState<string>('');
  const [reportType, setReportType] = useState<'new' | 'previous'>('new');
  // Date helpers
  const toDateStr = (d: Date) => d.toISOString().slice(0, 10);
  const defaultMonths = 12;
  const defaultStart = new Date();
  defaultStart.setMonth(defaultStart.getMonth() - defaultMonths);
  const [startDate, setStartDate] = useState<string>(toDateStr(defaultStart));
  const [endDate, setEndDate] = useState<string>(toDateStr(new Date()));
  const [customPatterns, setCustomPatterns] = useState<string>('');
  const [activePreset, setActivePreset] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [clientReports, setClientReports] = useState<AnalyticsReport[]>([]);
  const [currentReport, setCurrentReport] = useState<AnalyticsReport | null>(null);
  const [reportStatus, setReportStatus] = useState<string>('');
  const [pollInterval, setPollInterval] = useState<NodeJS.Timeout | null>(null);
  const [availableHashlists, setAvailableHashlists] = useState<HashlistSummary[]>([]);
  const [selectedHashlistIds, setSelectedHashlistIds] = useState<Set<number>>(new Set());
  const [hashlistsLoading, setHashlistsLoading] = useState(false);
  const { enqueueSnackbar } = useSnackbar();

  // Helper function to format dates
  const formatDate = (date: Date | string, formatStr: string): string => {
    const d = typeof date === 'string' ? new Date(date) : date;
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    const year = d.getFullYear();
    const hours = String(d.getHours()).padStart(2, '0');
    const minutes = String(d.getMinutes()).padStart(2, '0');

    if (formatStr === 'MMM d, yyyy') {
      const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
      return `${monthNames[d.getMonth()]} ${d.getDate()}, ${year}`;
    } else if (formatStr === 'MMM d, yyyy HH:mm') {
      const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
      return `${monthNames[d.getMonth()]} ${d.getDate()}, ${year} ${hours}:${minutes}`;
    }
    return d.toLocaleString();
  };

  // Fetch clients and admin defaults on mount
  useEffect(() => {
    fetchClients();
    // Fetch admin-configured default date range
    getJobDefaultsForUsers().then((defaults) => {
      const months = defaults.analytics_default_date_range_months || 12;
      const start = new Date();
      start.setMonth(start.getMonth() - months);
      setStartDate(toDateStr(start));
      setEndDate(toDateStr(new Date()));
    }).catch((err) => {
      console.error('Failed to fetch analytics default date range:', err);
    });
  }, []);

  // Poll for report status when viewing a report
  useEffect(() => {
    if (currentReport && (currentReport.status === 'queued' || currentReport.status === 'processing')) {
      const interval = setInterval(() => {
        fetchReportStatus(currentReport.id);
      }, 5000); // Poll every 5 seconds
      setPollInterval(interval);

      return () => {
        if (interval) clearInterval(interval);
      };
    } else {
      if (pollInterval) {
        clearInterval(pollInterval);
        setPollInterval(null);
      }
    }
  }, [currentReport?.id, currentReport?.status]);

  // Auto-load hashlists when client + dates are set
  useEffect(() => {
    if (!selectedClient || !startDate || !endDate) {
      setAvailableHashlists([]);
      setSelectedHashlistIds(new Set());
      return;
    }

    const fetchHashlists = async () => {
      setHashlistsLoading(true);
      try {
        const startDateTime = new Date(`${startDate}T00:00:00`).toISOString();
        const endDateTime = new Date(`${endDate}T23:59:59`).toISOString();
        const summaries = await analyticsService.getHashlistsForReport(
          selectedClient, startDateTime, endDateTime
        );
        setAvailableHashlists(summaries);

        // Auto-select: active = selected, archived = deselected
        const selected = new Set<number>();
        summaries.forEach(hl => {
          if (!hl.archived_at) {
            selected.add(hl.id);
          }
        });
        setSelectedHashlistIds(selected);
      } catch (error) {
        console.error('Error fetching hashlists:', error);
        setAvailableHashlists([]);
        setSelectedHashlistIds(new Set());
      } finally {
        setHashlistsLoading(false);
      }
    };

    fetchHashlists();
  }, [selectedClient, startDate, endDate]);

  const handleToggleHashlist = (id: number) => {
    setSelectedHashlistIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSelectAllHashlists = () => {
    setSelectedHashlistIds(new Set(availableHashlists.map(hl => hl.id)));
  };

  const handleDeselectAllHashlists = () => {
    setSelectedHashlistIds(new Set());
  };

  const fetchClients = async () => {
    try {
      const response = await api.get('/api/analytics/clients');
      setClients(response.data);
    } catch (error) {
      console.error('Error fetching clients:', error);
      enqueueSnackbar(t('messages.failedLoadClients') as string, { variant: 'error' });
    }
  };

  const fetchClientReports = async (clientId: string) => {
    try {
      setLoading(true);
      const reports = await analyticsService.getClientReports(clientId);
      setClientReports(reports);
    } catch (error) {
      console.error('Error fetching client reports:', error);
      enqueueSnackbar(t('messages.failedLoadReports') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const fetchReportStatus = async (reportId: string) => {
    try {
      const response = await analyticsService.getReport(reportId);
      setReportStatus(response.status);
      setCurrentReport(response.report);

      // Stop polling if completed or failed
      if (response.status === 'completed' || response.status === 'failed') {
        if (pollInterval) {
          clearInterval(pollInterval);
          setPollInterval(null);
        }
      }
    } catch (error) {
      console.error('Error fetching report status:', error);
    }
  };

  // Date preset helpers
  const applyDatePreset = (preset: string) => {
    const now = new Date();
    let start: Date;
    let end: Date = now;

    switch (preset) {
      case 'lastMonth': {
        start = new Date(now);
        start.setMonth(start.getMonth() - 1);
        break;
      }
      case 'lastQuarter': {
        start = new Date(now);
        start.setMonth(start.getMonth() - 3);
        break;
      }
      case 'last6Months': {
        start = new Date(now);
        start.setMonth(start.getMonth() - 6);
        break;
      }
      case 'lastYear': {
        start = new Date(now);
        start.setFullYear(start.getFullYear() - 1);
        break;
      }
      case 'priorMonth': {
        // First to last day of previous calendar month
        start = new Date(now.getFullYear(), now.getMonth() - 1, 1);
        end = new Date(now.getFullYear(), now.getMonth(), 0); // Day 0 = last day of prev month
        break;
      }
      case 'priorQuarter': {
        // Previous calendar quarter
        const currentQuarter = Math.floor(now.getMonth() / 3);
        const prevQuarter = currentQuarter === 0 ? 3 : currentQuarter - 1;
        const year = currentQuarter === 0 ? now.getFullYear() - 1 : now.getFullYear();
        start = new Date(year, prevQuarter * 3, 1);
        end = new Date(year, prevQuarter * 3 + 3, 0);
        break;
      }
      case 'prior6Months': {
        // Previous 6 calendar months (not including current month)
        start = new Date(now.getFullYear(), now.getMonth() - 6, 1);
        end = new Date(now.getFullYear(), now.getMonth(), 0);
        break;
      }
      case 'priorYear': {
        // Jan 1 to Dec 31 of previous year
        const prevYear = now.getFullYear() - 1;
        start = new Date(prevYear, 0, 1);
        end = new Date(prevYear, 11, 31);
        break;
      }
      default:
        return;
    }

    setStartDate(toDateStr(start));
    setEndDate(toDateStr(end));
    setActivePreset(preset);
  };

  const handleClientChange = (clientId: string) => {
    setSelectedClient(clientId);
    setCurrentReport(null);
    setReportStatus('');
    setAvailableHashlists([]);
    setSelectedHashlistIds(new Set());
    if (reportType === 'previous') {
      fetchClientReports(clientId);
    }
  };

  const handleReportTypeChange = (event: React.SyntheticEvent, newValue: number) => {
    const type = newValue === 0 ? 'new' : 'previous';
    setReportType(type);
    setCurrentReport(null);
    setReportStatus('');

    if (type === 'previous' && selectedClient) {
      fetchClientReports(selectedClient);
    }
  };

  const handleGenerateReport = async () => {
    if (!selectedClient) {
      enqueueSnackbar(t('messages.selectClient') as string, { variant: 'warning' });
      return;
    }

    if (availableHashlists.length > 0 && selectedHashlistIds.size === 0) {
      enqueueSnackbar(t('messages.selectHashlists') as string, { variant: 'warning' });
      return;
    }

    try {
      setLoading(true);

      const patterns = customPatterns
        ? customPatterns.split(',').map(p => p.trim()).filter(p => p)
        : [];

      // Append time components to dates (00:00:00 for start, 23:59:59 for end)
      const startDateTime = `${startDate}T00:00:00`;
      const endDateTime = `${endDate}T23:59:59`;

      const request: CreateAnalyticsReportRequest = {
        client_id: selectedClient,
        start_date: new Date(startDateTime).toISOString(),
        end_date: new Date(endDateTime).toISOString(),
        custom_patterns: patterns,
        hashlist_ids: selectedHashlistIds.size > 0 ? Array.from(selectedHashlistIds) : undefined,
      };

      const report = await analyticsService.createReport(request);
      setCurrentReport(report);
      setReportStatus('queued');
      enqueueSnackbar(t('messages.reportQueued', { position: report.queue_position }) as string, { variant: 'success' });
    } catch (error: any) {
      console.error('Error generating report:', error);
      enqueueSnackbar(error.response?.data?.error || t('messages.failedGenerateReport') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const handleViewReport = async (reportId: string) => {
    try {
      setLoading(true);
      const response = await analyticsService.getReport(reportId);
      setCurrentReport(response.report);
      setReportStatus(response.status);
    } catch (error) {
      console.error('Error viewing report:', error);
      enqueueSnackbar(t('messages.failedLoadReport') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteReport = async (reportId: string) => {
    try {
      await analyticsService.deleteReport(reportId);
      enqueueSnackbar(t('messages.reportDeleted') as string, { variant: 'success' });
      if (selectedClient) {
        fetchClientReports(selectedClient);
      }
      if (currentReport?.id === reportId) {
        setCurrentReport(null);
        setReportStatus('');
      }
    } catch (error) {
      console.error('Error deleting report:', error);
      enqueueSnackbar(t('messages.failedDeleteReport') as string, { variant: 'error' });
    }
  };

  const handleRetryReport = async (reportId: string) => {
    try {
      const report = await analyticsService.retryReport(reportId);
      setCurrentReport(report);
      setReportStatus('queued');
      enqueueSnackbar(t('messages.reportQueuedRetry', { position: report.queue_position }) as string, { variant: 'success' });
    } catch (error) {
      console.error('Error retrying report:', error);
      enqueueSnackbar(t('messages.failedRetryReport') as string, { variant: 'error' });
    }
  };

  const getStatusChip = (status: string) => {
    const statusColors: Record<string, any> = {
      queued: { color: 'info', label: t('status.queued') },
      processing: { color: 'warning', label: t('status.processing') },
      completed: { color: 'success', label: t('status.completed') },
      failed: { color: 'error', label: t('status.failed') },
    };

    const config = statusColors[status] || { color: 'default', label: status };
    return <Chip label={config.label as string} color={config.color} size="small" />;
  };

  return (
      <Box sx={{ p: 3 }}>
        {/* Header */}
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
          <Box>
            <Typography variant="h4" component="h1" gutterBottom>
              {t('title') as string}
            </Typography>
            <Typography variant="body1" color="text.secondary">
              {t('description') as string}
            </Typography>
          </Box>
        </Box>

        {/* Client Selection */}
        <Paper sx={{ p: 3, mb: 3 }}>
          <Typography variant="h6" gutterBottom>
            {t('clientSelection.title') as string}
          </Typography>
          <TextField
            select
            fullWidth
            label={t('clientSelection.label') as string}
            value={selectedClient}
            onChange={(e) => handleClientChange(e.target.value)}
            sx={{ mb: 2 }}
          >
            <MenuItem value="">
              <em>{t('clientSelection.placeholder') as string}</em>
            </MenuItem>
            {clients.map((client) => (
              <MenuItem key={client.id} value={client.id}>
                {client.name}
              </MenuItem>
            ))}
          </TextField>
        </Paper>

        {/* Report Type Tabs */}
        {selectedClient && (
          <Paper sx={{ mb: 3 }}>
            <Tabs value={reportType === 'new' ? 0 : 1} onChange={handleReportTypeChange}>
              <Tab label={t('tabs.generateNew') as string} />
              <Tab label={t('tabs.viewPrevious') as string} />
            </Tabs>

            <Divider />

            {/* New Report Form */}
            {reportType === 'new' && (
              <Box sx={{ p: 3 }}>
                {/* Date Preset Chips */}
                <Box sx={{ mb: 2 }}>
                  <Typography variant="caption" color="text.secondary" sx={{ mb: 1, display: 'block' }}>
                    Quick Select
                  </Typography>
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                    {[
                      { key: 'lastMonth', label: 'Last Month' },
                      { key: 'lastQuarter', label: 'Last Quarter' },
                      { key: 'last6Months', label: 'Last 6 Months' },
                      { key: 'lastYear', label: 'Last Year' },
                    ].map((preset) => (
                      <Chip
                        key={preset.key}
                        label={preset.label}
                        clickable
                        color={activePreset === preset.key ? 'primary' : 'default'}
                        variant={activePreset === preset.key ? 'filled' : 'outlined'}
                        onClick={() => applyDatePreset(preset.key)}
                        size="small"
                      />
                    ))}
                    <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
                    {[
                      { key: 'priorMonth', label: 'Prior Month' },
                      { key: 'priorQuarter', label: 'Prior Quarter' },
                      { key: 'prior6Months', label: 'Prior 6 Months' },
                      { key: 'priorYear', label: 'Prior Year' },
                    ].map((preset) => (
                      <Chip
                        key={preset.key}
                        label={preset.label}
                        clickable
                        color={activePreset === preset.key ? 'primary' : 'default'}
                        variant={activePreset === preset.key ? 'filled' : 'outlined'}
                        onClick={() => applyDatePreset(preset.key)}
                        size="small"
                      />
                    ))}
                  </Box>
                </Box>

                <Grid container spacing={3}>
                  <Grid item xs={12} md={6}>
                    <TextField
                      fullWidth
                      label={t('form.startDate') as string}
                      type="date"
                      value={startDate}
                      onChange={(e) => { setStartDate(e.target.value); setActivePreset(null); }}
                      InputLabelProps={{ shrink: true }}
                      sx={{
                        '& .MuiInputBase-root': {
                          backgroundColor: '#121212',
                        },
                        '& input[type="date"]': {
                          colorScheme: 'dark',
                        },
                        '& input[type="date"]::-webkit-calendar-picker-indicator': {
                          filter: 'invert(1)',
                          cursor: 'pointer',
                        },
                      }}
                    />
                  </Grid>
                  <Grid item xs={12} md={6}>
                    <TextField
                      fullWidth
                      label={t('form.endDate') as string}
                      type="date"
                      value={endDate}
                      onChange={(e) => { setEndDate(e.target.value); setActivePreset(null); }}
                      InputLabelProps={{ shrink: true }}
                      sx={{
                        '& .MuiInputBase-root': {
                          backgroundColor: '#121212',
                        },
                        '& input[type="date"]': {
                          colorScheme: 'dark',
                        },
                        '& input[type="date"]::-webkit-calendar-picker-indicator': {
                          filter: 'invert(1)',
                          cursor: 'pointer',
                        },
                      }}
                    />
                  </Grid>
                  {/* Hashlist Selection - auto-loaded after client + dates */}
                  {(availableHashlists.length > 0 || hashlistsLoading) && (
                    <Grid item xs={12}>
                      <Paper variant="outlined" sx={{ p: 2 }}>
                        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                          <Typography variant="subtitle2">
                            {t('hashlistSelection.title') as string} ({selectedHashlistIds.size}/{availableHashlists.length})
                          </Typography>
                          <Box>
                            <Button size="small" onClick={handleSelectAllHashlists} disabled={hashlistsLoading}>
                              {t('hashlistSelection.selectAll') as string}
                            </Button>
                            <Button size="small" onClick={handleDeselectAllHashlists} disabled={hashlistsLoading}>
                              {t('hashlistSelection.deselectAll') as string}
                            </Button>
                          </Box>
                        </Box>

                        {hashlistsLoading ? (
                          <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                            <CircularProgress size={24} />
                          </Box>
                        ) : availableHashlists.length === 0 ? (
                          <Alert severity="info" sx={{ mt: 1 }}>
                            {t('hashlistSelection.noHashlists') as string}
                          </Alert>
                        ) : (
                          <TableContainer sx={{ maxHeight: 300 }}>
                            <Table size="small" stickyHeader>
                              <TableHead>
                                <TableRow>
                                  <TableCell padding="checkbox" />
                                  <TableCell>{t('hashlistSelection.name') as string}</TableCell>
                                  <TableCell>{t('hashlistSelection.hashType') as string}</TableCell>
                                  <TableCell align="right">{t('hashlistSelection.hashes') as string}</TableCell>
                                  <TableCell align="right">{t('hashlistSelection.cracked') as string}</TableCell>
                                  <TableCell>{t('hashlistSelection.status') as string}</TableCell>
                                </TableRow>
                              </TableHead>
                              <TableBody>
                                {availableHashlists.map((hl) => (
                                  <TableRow
                                    key={hl.id}
                                    hover
                                    onClick={() => handleToggleHashlist(hl.id)}
                                    sx={{
                                      cursor: 'pointer',
                                      opacity: hl.archived_at ? 0.6 : 1,
                                    }}
                                  >
                                    <TableCell padding="checkbox">
                                      <Checkbox
                                        checked={selectedHashlistIds.has(hl.id)}
                                        onChange={() => handleToggleHashlist(hl.id)}
                                        size="small"
                                      />
                                    </TableCell>
                                    <TableCell>{hl.name}</TableCell>
                                    <TableCell>{hl.hash_type_name}</TableCell>
                                    <TableCell align="right">{hl.total_hashes.toLocaleString()}</TableCell>
                                    <TableCell align="right">{hl.cracked_hashes.toLocaleString()}</TableCell>
                                    <TableCell>
                                      {hl.archived_at ? (
                                        <Chip label={t('hashlistSelection.archived') as string} size="small" color="default" variant="outlined" />
                                      ) : (
                                        <Chip label={t('hashlistSelection.active') as string} size="small" color="success" variant="outlined" />
                                      )}
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          </TableContainer>
                        )}
                      </Paper>
                    </Grid>
                  )}

                  <Grid item xs={12}>
                    <TextField
                      fullWidth
                      label={t('form.customPatterns') as string}
                      placeholder={t('form.customPatternsPlaceholder') as string}
                      value={customPatterns}
                      onChange={(e) => setCustomPatterns(e.target.value)}
                      helperText={t('form.customPatternsHelper') as string}
                    />
                  </Grid>
                  <Grid item xs={12}>
                    <Button
                      variant="contained"
                      startIcon={loading ? <CircularProgress size={20} /> : <AddIcon />}
                      onClick={handleGenerateReport}
                      disabled={loading || !selectedClient || (availableHashlists.length > 0 && selectedHashlistIds.size === 0)}
                      fullWidth
                    >
                      {t('generateReport') as string}
                    </Button>
                  </Grid>
                </Grid>
              </Box>
            )}

            {/* Previous Reports List */}
            {reportType === 'previous' && (
              <Box sx={{ p: 3 }}>
                {loading ? (
                  <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
                    <CircularProgress />
                  </Box>
                ) : clientReports.length === 0 ? (
                  <Alert severity="info">{t('table.noReports') as string}</Alert>
                ) : (
                  <TableContainer>
                    <Table>
                      <TableHead>
                        <TableRow>
                          <TableCell>{t('table.dateRange') as string}</TableCell>
                          <TableCell>{t('table.generatedOn') as string}</TableCell>
                          <TableCell>{t('table.status') as string}</TableCell>
                          <TableCell align="right">{t('table.hashes') as string}</TableCell>
                          <TableCell align="right">{t('table.cracked') as string}</TableCell>
                          <TableCell align="right">{t('table.actions') as string}</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {clientReports.map((report) => (
                          <TableRow key={report.id}>
                            <TableCell>
                              {formatDate(report.start_date, 'MMM d, yyyy')} - {formatDate(report.end_date, 'MMM d, yyyy')}
                            </TableCell>
                            <TableCell>{formatDate(report.created_at, 'MMM d, yyyy HH:mm')}</TableCell>
                            <TableCell>{getStatusChip(report.status)}</TableCell>
                            <TableCell align="right">{report.total_hashes.toLocaleString()}</TableCell>
                            <TableCell align="right">{report.total_cracked.toLocaleString()}</TableCell>
                            <TableCell align="right">
                              <Tooltip title={t('actions.viewReport') as string}>
                                <IconButton
                                  size="small"
                                  onClick={() => handleViewReport(report.id)}
                                  color="primary"
                                >
                                  <VisibilityIcon />
                                </IconButton>
                              </Tooltip>
                              {report.status === 'failed' && (
                                <Tooltip title={t('actions.retryReport') as string}>
                                  <IconButton
                                    size="small"
                                    onClick={() => handleRetryReport(report.id)}
                                    color="warning"
                                  >
                                    <RetryIcon />
                                  </IconButton>
                                </Tooltip>
                              )}
                              <Tooltip title={t('actions.deleteReport') as string}>
                                <IconButton
                                  size="small"
                                  onClick={() => handleDeleteReport(report.id)}
                                  color="error"
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
              </Box>
            )}
          </Paper>
        )}

        {/* Report Display */}
        {currentReport && (
          <AnalyticsReportDisplay
            report={currentReport}
            status={reportStatus}
            onRetry={() => handleRetryReport(currentReport.id)}
            onDelete={() => handleDeleteReport(currentReport.id)}
          />
        )}
      </Box>
  );
}
