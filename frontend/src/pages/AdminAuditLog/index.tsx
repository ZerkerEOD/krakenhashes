import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Typography,
  Paper,
  CircularProgress,
  Alert,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TextField,
  IconButton,
  Collapse,
  Table,
  TableBody,
  TableCell,
  TableRow,
  Tooltip,
  SelectChangeEvent,
} from '@mui/material';
import {
  DataGrid,
  GridColDef,
  GridRenderCellParams,
  GridPaginationModel,
} from '@mui/x-data-grid';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import FilterListIcon from '@mui/icons-material/FilterList';
import ClearIcon from '@mui/icons-material/Clear';
import { useTranslation } from 'react-i18next';

import {
  AuditLog,
  AuditLogSeverity,
  NotificationType,
  NotificationCategory,
  NOTIFICATION_TYPES,
} from '../../types/notifications';
import { getAuditLogs, getAuditableEventTypes } from '../../services/notifications';

// Severity color mapping
const severityColors: Record<AuditLogSeverity, 'error' | 'warning' | 'info'> = {
  critical: 'error',
  warning: 'warning',
  info: 'info',
};

// Row component with expandable details
interface ExpandableRowProps {
  row: AuditLog;
  isExpanded: boolean;
  onToggle: () => void;
}

const ExpandableDetails: React.FC<{ row: AuditLog }> = ({ row }) => {
  const { t } = useTranslation('notifications');

  return (
    <Box sx={{ p: 2, bgcolor: 'action.hover' }}>
      <Table size="small">
        <TableBody>
          <TableRow>
            <TableCell component="th" sx={{ fontWeight: 'bold', width: 150 }}>
              {t('auditLog.details.message', 'Message')}
            </TableCell>
            <TableCell>{row.message}</TableCell>
          </TableRow>
          {row.ip_address && (
            <TableRow>
              <TableCell component="th" sx={{ fontWeight: 'bold' }}>
                {t('auditLog.details.ipAddress', 'IP Address')}
              </TableCell>
              <TableCell>{row.ip_address}</TableCell>
            </TableRow>
          )}
          {row.user_agent && (
            <TableRow>
              <TableCell component="th" sx={{ fontWeight: 'bold' }}>
                {t('auditLog.details.userAgent', 'User Agent')}
              </TableCell>
              <TableCell sx={{ wordBreak: 'break-all' }}>{row.user_agent}</TableCell>
            </TableRow>
          )}
          {row.source_type && (
            <TableRow>
              <TableCell component="th" sx={{ fontWeight: 'bold' }}>
                {t('auditLog.details.source', 'Source')}
              </TableCell>
              <TableCell>
                {row.source_type}
                {row.source_id && `: ${row.source_id}`}
              </TableCell>
            </TableRow>
          )}
          {row.data && Object.keys(row.data).length > 0 && (
            <TableRow>
              <TableCell component="th" sx={{ fontWeight: 'bold', verticalAlign: 'top' }}>
                {t('auditLog.details.data', 'Additional Data')}
              </TableCell>
              <TableCell>
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                  {JSON.stringify(row.data, null, 2)}
                </pre>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </Box>
  );
};

export const AdminAuditLog: React.FC = () => {
  const { t } = useTranslation('notifications');

  // Data state
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState<number>(0);

  // Pagination state
  const [paginationModel, setPaginationModel] = useState<GridPaginationModel>({
    page: 0,
    pageSize: 25,
  });

  // Filter state
  const [showFilters, setShowFilters] = useState<boolean>(true);
  const [eventTypeFilter, setEventTypeFilter] = useState<NotificationType | ''>('');
  const [severityFilter, setSeverityFilter] = useState<AuditLogSeverity | ''>('');
  const [startDate, setStartDate] = useState<string>('');
  const [endDate, setEndDate] = useState<string>('');

  // Expanded rows state
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());

  // Auditable event types from server
  const [auditableTypes, setAuditableTypes] = useState<NotificationType[]>([]);

  // Fetch auditable event types on mount
  useEffect(() => {
    const fetchEventTypes = async () => {
      try {
        const response = await getAuditableEventTypes();
        setAuditableTypes(response.event_types.map((et) => et.type));
      } catch (err) {
        console.error('Failed to fetch auditable event types:', err);
        // Fallback to security + critical events
        setAuditableTypes([
          'security_suspicious_login',
          'security_mfa_disabled',
          'security_password_changed',
          'job_failed',
          'agent_error',
          'agent_offline',
          'webhook_failure',
        ]);
      }
    };
    fetchEventTypes();
  }, []);

  // Fetch audit logs
  const fetchAuditLogs = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getAuditLogs({
        event_type: eventTypeFilter ? [eventTypeFilter] : undefined,
        severity: severityFilter || undefined,
        start_date: startDate || undefined,
        end_date: endDate || undefined,
        limit: paginationModel.pageSize,
        offset: paginationModel.page * paginationModel.pageSize,
      });
      // Handle null response - API returns null when no entries exist
      setAuditLogs(response.audit_logs || []);
      setTotal(response.total || 0);
    } catch (err) {
      console.error('Failed to fetch audit logs:', err);
      setError(t('auditLog.errors.loadFailed', 'Failed to load audit logs'));
    } finally {
      setLoading(false);
    }
  }, [eventTypeFilter, severityFilter, startDate, endDate, paginationModel, t]);

  useEffect(() => {
    fetchAuditLogs();
  }, [fetchAuditLogs]);

  // Toggle row expansion
  const toggleRowExpansion = (id: string) => {
    setExpandedRows((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return newSet;
    });
  };

  // Clear all filters
  const clearFilters = () => {
    setEventTypeFilter('');
    setSeverityFilter('');
    setStartDate('');
    setEndDate('');
    setPaginationModel({ ...paginationModel, page: 0 });
  };

  // Get event type label (translated)
  const getEventTypeLabel = (type: NotificationType): string => {
    return t(`types.${type}`, type);
  };

  // Get category for type
  const getCategoryForType = (type: NotificationType): NotificationCategory => {
    const metadata = NOTIFICATION_TYPES.find((nt) => nt.type === type);
    return metadata?.category || 'system';
  };

  // Get translated category label
  const getCategoryLabel = (category: NotificationCategory): string => {
    return t(`categories.${category}`, category);
  };

  // Columns definition
  const columns: GridColDef[] = [
    {
      field: 'expand',
      headerName: '',
      width: 50,
      sortable: false,
      filterable: false,
      renderCell: (params: GridRenderCellParams<AuditLog>) => (
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            toggleRowExpansion(params.row.id);
          }}
        >
          {expandedRows.has(params.row.id) ? (
            <KeyboardArrowUpIcon />
          ) : (
            <KeyboardArrowDownIcon />
          )}
        </IconButton>
      ),
    },
    {
      field: 'created_at',
      headerName: t('auditLog.columns.time', 'Time'),
      width: 180,
      renderCell: (params: GridRenderCellParams<AuditLog>) => (
        <Tooltip title={new Date(params.row.created_at).toLocaleString()}>
          <span>{new Date(params.row.created_at).toLocaleString()}</span>
        </Tooltip>
      ),
    },
    {
      field: 'severity',
      headerName: t('auditLog.columns.severity', 'Severity'),
      width: 100,
      renderCell: (params: GridRenderCellParams<AuditLog>) => (
        <Chip
          label={params.row.severity.toUpperCase()}
          color={severityColors[params.row.severity]}
          size="small"
          sx={{ fontWeight: 'bold' }}
        />
      ),
    },
    {
      field: 'event_type',
      headerName: t('auditLog.columns.eventType', 'Event Type'),
      width: 180,
      renderCell: (params: GridRenderCellParams<AuditLog>) => {
        const category = getCategoryForType(params.row.event_type);
        return (
          <Box>
            <Typography variant="body2">{getEventTypeLabel(params.row.event_type)}</Typography>
            <Typography variant="caption" color="text.secondary">
              {getCategoryLabel(category)}
            </Typography>
          </Box>
        );
      },
    },
    {
      field: 'username',
      headerName: t('auditLog.columns.user', 'User'),
      width: 150,
      renderCell: (params: GridRenderCellParams<AuditLog>) => (
        <Tooltip title={params.row.user_email || ''}>
          <span>{params.row.username || 'System'}</span>
        </Tooltip>
      ),
    },
    {
      field: 'title',
      headerName: t('auditLog.columns.title', 'Title'),
      flex: 1,
      minWidth: 200,
    },
    {
      field: 'ip_address',
      headerName: t('auditLog.columns.ip', 'IP'),
      width: 130,
      renderCell: (params: GridRenderCellParams<AuditLog>) => (
        <span>{params.row.ip_address || '-'}</span>
      ),
    },
  ];

  // Custom row rendering to include expandable details
  const getRowHeight = (params: { id: string | number }) => {
    return expandedRows.has(String(params.id)) ? 'auto' : 52;
  };

  return (
    <Box sx={{ width: '100%', p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('auditLog.title', 'Audit Log')}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('auditLog.description', 'Security and critical events across all users')}
          </Typography>
        </Box>
        <IconButton onClick={() => setShowFilters(!showFilters)}>
          <FilterListIcon />
        </IconButton>
      </Box>

      {/* Filters */}
      <Collapse in={showFilters}>
        <Paper sx={{ p: 2, mb: 2 }}>
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center' }}>
            <FormControl size="small" sx={{ minWidth: 180 }}>
              <InputLabel>{t('auditLog.filters.eventType', 'Event Type')}</InputLabel>
              <Select
                value={eventTypeFilter}
                label={t('auditLog.filters.eventType', 'Event Type')}
                onChange={(e: SelectChangeEvent) =>
                  setEventTypeFilter(e.target.value as NotificationType | '')
                }
              >
                <MenuItem value="">{t('auditLog.filters.all', 'All')}</MenuItem>
                {auditableTypes.map((type) => (
                  <MenuItem key={type} value={type}>
                    {getEventTypeLabel(type)}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{t('auditLog.filters.severity', 'Severity')}</InputLabel>
              <Select
                value={severityFilter}
                label={t('auditLog.filters.severity', 'Severity')}
                onChange={(e: SelectChangeEvent) =>
                  setSeverityFilter(e.target.value as AuditLogSeverity | '')
                }
              >
                <MenuItem value="">{t('auditLog.filters.all', 'All')}</MenuItem>
                <MenuItem value="critical">{t('auditLog.severity.critical', 'Critical')}</MenuItem>
                <MenuItem value="warning">{t('auditLog.severity.warning', 'Warning')}</MenuItem>
                <MenuItem value="info">{t('auditLog.severity.info', 'Info')}</MenuItem>
              </Select>
            </FormControl>

            <TextField
              size="small"
              label={t('auditLog.filters.startDate', 'Start Date')}
              type="date"
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
              InputLabelProps={{ shrink: true }}
              sx={{ width: 160 }}
            />

            <TextField
              size="small"
              label={t('auditLog.filters.endDate', 'End Date')}
              type="date"
              value={endDate}
              onChange={(e) => setEndDate(e.target.value)}
              InputLabelProps={{ shrink: true }}
              sx={{ width: 160 }}
            />

            <IconButton onClick={clearFilters} title={t('auditLog.filters.clear', 'Clear filters')}>
              <ClearIcon />
            </IconButton>
          </Box>
        </Paper>
      </Collapse>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Paper sx={{ height: 'calc(100vh - 300px)', width: '100%' }}>
        {loading && auditLogs.length === 0 ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        ) : (
          <>
            <DataGrid
              rows={auditLogs}
              columns={columns}
              rowCount={total}
              loading={loading}
              pageSizeOptions={[10, 25, 50, 100]}
              paginationModel={paginationModel}
              onPaginationModelChange={setPaginationModel}
              paginationMode="server"
              disableRowSelectionOnClick
              getRowHeight={() => 'auto'}
              sx={{
                '& .MuiDataGrid-cell': {
                  py: 1,
                },
                '& .MuiDataGrid-row': {
                  cursor: 'pointer',
                },
              }}
              slots={{
                noRowsOverlay: () => (
                  <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
                    <Typography color="text.secondary">
                      {t('auditLog.noData', 'No audit log entries found')}
                    </Typography>
                  </Box>
                ),
              }}
            />
            {/* Expanded row details */}
            {auditLogs.map(
              (row) =>
                expandedRows.has(row.id) && (
                  <Collapse key={`detail-${row.id}`} in={expandedRows.has(row.id)}>
                    <ExpandableDetails row={row} />
                  </Collapse>
                )
            )}
          </>
        )}
      </Paper>
    </Box>
  );
};

export default AdminAuditLog;
