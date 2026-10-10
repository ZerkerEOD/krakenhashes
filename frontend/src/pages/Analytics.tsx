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
import React, { useState, useEffect } from 'react';
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
  Divider,
  Tab,
  Tabs,
  Chip,
} from '@mui/material';
import type { Theme } from '@mui/material/styles';
import type { GridColDef, GridRowId } from '@mui/x-data-grid';
import {
  Add as AddIcon,
  Delete as DeleteIcon,
  Replay as RetryIcon,
  Visibility as VisibilityIcon,
} from '@mui/icons-material';
import analyticsService from '../services/analytics';
import { AnalyticsReport, CreateAnalyticsReportRequest, HashlistSummary } from '../types/analytics';
import { api } from '../services/api';
import { getJobDefaultsForUsers } from '../services/jobSettings';
import { DataTable, EntityLink, PageHeader, StatusChip, useConfirm, useToast } from '../components/ui';
import { useLiveQuery } from '../hooks/useLiveQuery';

// Import display components
import AnalyticsReportDisplay from '../components/analytics/AnalyticsReportDisplay';

interface Client {
  id: string;
  name: string;
}

/** Native date input styled from theme tokens so it reads correctly in light and dark mode. */
const dateFieldSx = (th: Theme) => ({
  '& .MuiInputBase-root': {
    backgroundColor: th.palette.surface.sunken,
  },
  '& input[type="date"]': {
    colorScheme: th.palette.mode,
  },
  '& input[type="date"]::-webkit-calendar-picker-indicator': {
    filter: th.palette.mode === 'dark' ? 'invert(1)' : 'none',
    cursor: 'pointer',
  },
});

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
  const [availableHashlists, setAvailableHashlists] = useState<HashlistSummary[]>([]);
  const [selectedHashlistIds, setSelectedHashlistIds] = useState<Set<number>>(new Set());
  const [hashlistsLoading, setHashlistsLoading] = useState(false);
  const [bloodhoundFiles, setBloodhoundFiles] = useState<File[]>([]);
  const toast = useToast();
  const confirm = useConfirm();

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

  // Poll for report status while the viewed report is queued or processing
  const pollReportId =
    currentReport && (currentReport.status === 'queued' || currentReport.status === 'processing')
      ? currentReport.id
      : null;
  const reportPoll = useLiveQuery(
    {
      queryKey: ['analytics', 'report-status', pollReportId],
      queryFn: () => analyticsService.getReport(pollReportId as string),
      enabled: Boolean(pollReportId),
      // No cache: a retried report must not briefly resurrect its previous "failed" snapshot.
      gcTime: 0,
    },
    {
      tier: 'fast',
      when: (d) => !d || (d.status !== 'completed' && d.status !== 'failed'),
    }
  );

  useEffect(() => {
    if (!pollReportId || !reportPoll.data || reportPoll.data.report?.id !== pollReportId) return;
    setReportStatus(reportPoll.data.status);
    setCurrentReport(reportPoll.data.report);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reportPoll.data]);

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
      toast.error(t('messages.failedLoadClients') as string);
    }
  };

  const fetchClientReports = async (clientId: string) => {
    try {
      setLoading(true);
      const reports = await analyticsService.getClientReports(clientId);
      setClientReports(reports);
    } catch (error) {
      console.error('Error fetching client reports:', error);
      toast.error(t('messages.failedLoadReports') as string);
    } finally {
      setLoading(false);
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
      toast.warning(t('messages.selectClient') as string);
      return;
    }

    if (availableHashlists.length > 0 && selectedHashlistIds.size === 0) {
      toast.warning(t('messages.selectHashlists') as string);
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

      // When a BloodHound collection dump is attached, use the multipart endpoint so the report is
      // enriched with AD-privilege context. The dump is parsed in memory only and never persisted.
      const report = bloodhoundFiles.length > 0
        ? await analyticsService.createReportWithBloodhound(
            {
              clientId: selectedClient,
              hashlistIds: selectedHashlistIds.size > 0 ? Array.from(selectedHashlistIds) : [],
              startDate: request.start_date,
              endDate: request.end_date,
              customPatterns: patterns,
            },
            bloodhoundFiles,
          )
        : await analyticsService.createReport(request);
      setBloodhoundFiles([]);
      setCurrentReport(report);
      setReportStatus('queued');
      toast.success(t('messages.reportQueued', { position: report.queue_position }) as string);
    } catch (error: any) {
      console.error('Error generating report:', error);
      toast.error(error.response?.data?.error || (t('messages.failedGenerateReport') as string));
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
      toast.error(t('messages.failedLoadReport') as string);
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteReport = async (reportId: string) => {
    const ok = await confirm({
      title: t('actions.deleteReport') as string,
      message: t('messages.confirmDeleteReport', 'Delete this analytics report? This cannot be undone.') as string,
      severity: 'danger',
      confirmLabel: t('actions.delete') as string,
    });
    if (!ok) return;
    try {
      await analyticsService.deleteReport(reportId);
      toast.success(t('messages.reportDeleted') as string);
      if (selectedClient) {
        fetchClientReports(selectedClient);
      }
      if (currentReport?.id === reportId) {
        setCurrentReport(null);
        setReportStatus('');
      }
    } catch (error) {
      console.error('Error deleting report:', error);
      toast.error(t('messages.failedDeleteReport') as string);
    }
  };

  const handleRetryReport = async (reportId: string) => {
    try {
      const report = await analyticsService.retryReport(reportId);
      setCurrentReport(report);
      setReportStatus('queued');
      toast.success(t('messages.reportQueuedRetry', { position: report.queue_position }) as string);
    } catch (error) {
      console.error('Error retrying report:', error);
      toast.error(t('messages.failedRetryReport') as string);
    }
  };

  const reportColumns: GridColDef<AnalyticsReport>[] = [
    {
      field: 'start_date',
      headerName: t('table.dateRange') as string,
      flex: 1.4,
      minWidth: 220,
      renderCell: (p) =>
        `${formatDate(p.row.start_date, 'MMM d, yyyy')} - ${formatDate(p.row.end_date, 'MMM d, yyyy')}`,
    },
    {
      field: 'created_at',
      headerName: t('table.generatedOn') as string,
      flex: 1,
      minWidth: 170,
      renderCell: (p) => formatDate(p.row.created_at, 'MMM d, yyyy HH:mm'),
    },
    {
      field: 'status',
      headerName: t('table.status') as string,
      width: 130,
      renderCell: (p) => (
        <StatusChip
          entity="job"
          status={p.row.status}
          label={t(`status.${p.row.status}`, { defaultValue: p.row.status }) as string}
        />
      ),
    },
    {
      field: 'total_hashes',
      headerName: t('table.hashes') as string,
      type: 'number',
      width: 120,
      valueFormatter: (v) => (v == null ? '' : Number(v).toLocaleString()),
    },
    {
      field: 'total_cracked',
      headerName: t('table.cracked') as string,
      type: 'number',
      width: 120,
      valueFormatter: (v) => (v == null ? '' : Number(v).toLocaleString()),
    },
  ];

  const hashlistColumns: GridColDef<HashlistSummary>[] = [
    {
      field: 'name',
      headerName: t('hashlistSelection.name') as string,
      flex: 1.5,
      minWidth: 180,
      renderCell: (p) => <EntityLink type="hashlist" id={p.row.id} label={p.row.name} />,
    },
    { field: 'hash_type_name', headerName: t('hashlistSelection.hashType') as string, flex: 1, minWidth: 140 },
    {
      field: 'total_hashes',
      headerName: t('hashlistSelection.hashes') as string,
      type: 'number',
      width: 110,
      valueFormatter: (v) => (v == null ? '' : Number(v).toLocaleString()),
    },
    {
      field: 'cracked_hashes',
      headerName: t('hashlistSelection.cracked') as string,
      type: 'number',
      width: 110,
      valueFormatter: (v) => (v == null ? '' : Number(v).toLocaleString()),
    },
    {
      field: 'archived_at',
      headerName: t('hashlistSelection.status') as string,
      width: 120,
      renderCell: (p) => (
        <StatusChip
          entity="generic"
          variant="outlined"
          status={p.row.archived_at ? 'inactive' : 'active'}
          label={(p.row.archived_at ? t('hashlistSelection.archived') : t('hashlistSelection.active')) as string}
        />
      ),
    },
  ];

  return (
      <Box sx={{ p: 3 }}>
        {/* Header */}
        <PageHeader title={t('title') as string} description={t('description') as string} />

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
                    {t('form.quickSelect') as string}
                  </Typography>
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                    {[
                      { key: 'lastMonth', label: t('form.datePresets.lastMonth') as string },
                      { key: 'lastQuarter', label: t('form.datePresets.lastQuarter') as string },
                      { key: 'last6Months', label: t('form.datePresets.last6Months') as string },
                      { key: 'lastYear', label: t('form.datePresets.lastYear') as string },
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
                      { key: 'priorMonth', label: t('form.datePresets.priorMonth') as string },
                      { key: 'priorQuarter', label: t('form.datePresets.priorQuarter') as string },
                      { key: 'prior6Months', label: t('form.datePresets.prior6Months') as string },
                      { key: 'priorYear', label: t('form.datePresets.priorYear') as string },
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
                      sx={dateFieldSx}
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
                      sx={dateFieldSx}
                    />
                  </Grid>
                  {/* Hashlist Selection - auto-loaded after client + dates */}
                  {(availableHashlists.length > 0 || hashlistsLoading) && (
                    <Grid item xs={12}>
                      <Paper variant="outlined" sx={{ p: 2 }}>
                        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
                          <Typography variant="subtitle2">
                            {t('hashlistSelection.titleWithCount', {
                              title: t('hashlistSelection.title'),
                              selected: selectedHashlistIds.size,
                              total: availableHashlists.length,
                            })}
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
                          <DataTable<HashlistSummary>
                            flat
                            rows={availableHashlists}
                            columns={hashlistColumns}
                            getRowId={(r) => r.id}
                            pagination={false}
                            sorting={{ mode: 'client' }}
                            hideFooter
                            height={availableHashlists.length > 6 ? 300 : 'auto'}
                            selection={{
                              model: Array.from(selectedHashlistIds) as GridRowId[],
                              onChange: (ids) => setSelectedHashlistIds(new Set(ids.map((id) => Number(id)))),
                            }}
                            onRowClick={(row) => handleToggleHashlist(row.id)}
                            rowClassName={(row) => (row.archived_at ? 'kh-archived' : '')}
                            sx={{ '& .kh-archived': { opacity: 0.6 } }}
                          />
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
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, flexWrap: 'wrap' }}>
                      <Button variant="outlined" component="label">
                        {t('bloodhoundUpload.attachButton') as string}
                        <input
                          type="file"
                          hidden
                          multiple
                          accept=".zip,.json"
                          onChange={(e) =>
                            setBloodhoundFiles(e.target.files ? Array.from(e.target.files) : [])
                          }
                        />
                      </Button>
                      {bloodhoundFiles.length > 0 && (
                        <>
                          <Typography variant="body2" color="text.secondary">
                            {bloodhoundFiles.length === 1
                              ? bloodhoundFiles[0].name
                              : t('bloodhoundUpload.filesSelected', { count: bloodhoundFiles.length })}
                          </Typography>
                          <Button size="small" color="inherit" onClick={() => setBloodhoundFiles([])}>
                            {t('bloodhoundUpload.clear') as string}
                          </Button>
                        </>
                      )}
                    </Box>
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                      {t('bloodhoundUpload.helperText') as string}
                    </Typography>
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
                ) : (
                  <DataTable<AnalyticsReport>
                    flat
                    rows={clientReports}
                    columns={reportColumns}
                    getRowId={(r) => r.id}
                    pagination={{ mode: 'client', initialPageSize: 25 }}
                    sorting={{ mode: 'client', initial: [{ field: 'created_at', sort: 'desc' }] }}
                    onRowClick={(row) => handleViewReport(row.id)}
                    rowActionsInlineLimit={3}
                    rowActions={(row) => [
                      {
                        key: 'view',
                        label: t('actions.viewReport') as string,
                        icon: <VisibilityIcon fontSize="small" />,
                        onClick: (r) => handleViewReport(r.id),
                      },
                      {
                        key: 'retry',
                        label: t('actions.retryReport') as string,
                        icon: <RetryIcon fontSize="small" />,
                        hidden: row.status !== 'failed',
                        onClick: (r) => handleRetryReport(r.id),
                      },
                      {
                        key: 'delete',
                        label: t('actions.deleteReport') as string,
                        icon: <DeleteIcon fontSize="small" />,
                        danger: true,
                        onClick: (r) => handleDeleteReport(r.id),
                      },
                    ]}
                    emptyState={{ title: t('table.noReports') as string }}
                    tableKey="analytics-reports"
                  />
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
