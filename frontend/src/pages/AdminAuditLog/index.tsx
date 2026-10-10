import React, { useMemo, useState } from 'react';
import {
  Box,
  FormControl,
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  SelectChangeEvent,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import ClearIcon from '@mui/icons-material/Clear';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { DataTable, EntityLink, PageHeader, SimpleTable, StatusChip } from '../../components/ui';
import { formatDateTime } from '../../utils/formatters';
import {
  AuditLog,
  AuditLogSeverity,
  NotificationType,
  NotificationCategory,
  NOTIFICATION_TYPES,
} from '../../types/notifications';
import { getAuditLogs, getAuditableEventTypes } from '../../services/notifications';

const FALLBACK_EVENT_TYPES: NotificationType[] = [
  'security_suspicious_login',
  'security_mfa_disabled',
  'security_password_changed',
  'job_failed',
  'agent_error',
  'agent_offline',
  'webhook_failure',
];

interface DetailRow {
  key: string;
  label: string;
  value: React.ReactNode;
}

const AuditDetails: React.FC<{ row: AuditLog }> = ({ row }) => {
  const { t } = useTranslation('notifications');
  const items: DetailRow[] = [{ key: 'message', label: t('auditLog.details.message', 'Message'), value: row.message }];
  if (row.ip_address) items.push({ key: 'ip', label: t('auditLog.details.ipAddress', 'IP Address'), value: row.ip_address });
  if (row.user_agent) {
    items.push({
      key: 'ua',
      label: t('auditLog.details.userAgent', 'User Agent'),
      value: <Box component="span" sx={{ wordBreak: 'break-all' }}>{row.user_agent}</Box>,
    });
  }
  if (row.source_type) {
    items.push({
      key: 'source',
      label: t('auditLog.details.source', 'Source'),
      value: row.source_id ? (
        <EntityLink type={row.source_type} id={row.source_id} label={`${row.source_type}: ${row.source_id}`} mono />
      ) : (
        row.source_type
      ),
    });
  }
  if (row.data && Object.keys(row.data).length > 0) {
    items.push({
      key: 'data',
      label: t('auditLog.details.data', 'Additional Data'),
      value: (
        <Box
          component="pre"
          sx={{ m: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontFamily: (th: any) => th.typography.monoFamily, fontSize: '0.8125rem' }}
        >
          {JSON.stringify(row.data, null, 2)}
        </Box>
      ),
    });
  }
  return (
    <Box sx={{ p: 2 }}>
      <SimpleTable<DetailRow>
        rows={items}
        getRowKey={(r) => r.key}
        columns={[
          { field: 'label', headerName: '', width: 150, render: (r) => <Typography variant="body2" fontWeight={600}>{r.label}</Typography> },
          { field: 'value', headerName: '', render: (r) => r.value },
        ]}
      />
    </Box>
  );
};

export const AdminAuditLog: React.FC = () => {
  const { t } = useTranslation('notifications');

  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const [eventTypeFilter, setEventTypeFilter] = useState<NotificationType | ''>('');
  const [severityFilter, setSeverityFilter] = useState<AuditLogSeverity | ''>('');
  const [startDate, setStartDate] = useState<string>('');
  const [endDate, setEndDate] = useState<string>('');

  const eventTypesQuery = useQuery({
    queryKey: ['audit-log', 'event-types'],
    queryFn: async () => {
      try {
        const response = await getAuditableEventTypes();
        return response.event_types.map((et) => et.type);
      } catch (err) {
        console.error('Failed to fetch auditable event types:', err);
        // Fallback to security + critical events
        return FALLBACK_EVENT_TYPES;
      }
    },
    staleTime: 5 * 60_000,
  });
  const auditableTypes = eventTypesQuery.data ?? [];

  const params = useMemo(
    () => ({
      event_type: eventTypeFilter ? [eventTypeFilter] : undefined,
      severity: severityFilter || undefined,
      start_date: startDate || undefined,
      end_date: endDate || undefined,
      limit: pageSize,
      offset: page * pageSize,
    }),
    [eventTypeFilter, severityFilter, startDate, endDate, pageSize, page]
  );

  const query = useQuery({
    queryKey: ['audit-log', 'list', params],
    queryFn: async () => {
      const response = await getAuditLogs(params);
      // API returns null audit_logs when no entries exist
      return { logs: response.audit_logs || [], total: response.total || 0 };
    },
    placeholderData: keepPreviousData,
  });
  const auditLogs = query.data?.logs ?? [];
  const total = query.data?.total ?? 0;

  const clearFilters = () => {
    setEventTypeFilter('');
    setSeverityFilter('');
    setStartDate('');
    setEndDate('');
    setPage(0);
  };

  const getEventTypeLabel = (type: NotificationType): string => t(`types.${type}`, type);
  const getCategoryForType = (type: NotificationType): NotificationCategory =>
    NOTIFICATION_TYPES.find((nt) => nt.type === type)?.category || 'system';
  const getCategoryLabel = (category: NotificationCategory): string => t(`categories.${category}`, category);

  const columns: GridColDef<AuditLog>[] = [
    {
      field: 'created_at',
      headerName: t('auditLog.columns.time', 'Time'),
      width: 180,
      renderCell: (p) => (
        <Tooltip title={new Date(p.row.created_at).toLocaleString()}>
          <span>{formatDateTime(p.row.created_at)}</span>
        </Tooltip>
      ),
    },
    {
      field: 'severity',
      headerName: t('auditLog.columns.severity', 'Severity'),
      width: 110,
      renderCell: (p) => <StatusChip entity="audit" status={p.row.severity} />,
    },
    {
      field: 'event_type',
      headerName: t('auditLog.columns.eventType', 'Event Type'),
      width: 200,
      renderCell: (p) => (
        <Box sx={{ py: 0.5 }}>
          <Typography variant="body2">{getEventTypeLabel(p.row.event_type)}</Typography>
          <Typography variant="caption" color="text.secondary">
            {getCategoryLabel(getCategoryForType(p.row.event_type))}
          </Typography>
        </Box>
      ),
    },
    {
      field: 'username',
      headerName: t('auditLog.columns.user', 'User'),
      width: 160,
      renderCell: (p) =>
        p.row.username ? (
          <Tooltip title={p.row.user_email || ''}>
            <span>
              <EntityLink type="user" id={p.row.user_id} label={p.row.username} />
            </span>
          </Tooltip>
        ) : (
          t('auditLog.system', 'System')
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
      width: 140,
      valueFormatter: (v: string | undefined) => v || '-',
    },
  ];

  const hasFilters = Boolean(eventTypeFilter || severityFilter || startDate || endDate);

  return (
    <Box sx={{ width: '100%', p: 3 }}>
      <PageHeader
        title={t('auditLog.title', 'Audit Log')}
        description={t('auditLog.description', 'Security and critical events across all users')}
      />

      <DataTable<AuditLog>
        rows={auditLogs}
        columns={columns}
        loading={query.isLoading}
        fetching={query.isFetching && !query.isLoading}
        error={query.error ? t('auditLog.errors.loadFailed', 'Failed to load audit logs') : undefined}
        onRetry={() => void query.refetch()}
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount: total,
          pageSizeOptions: [10, 25, 50, 100],
          onChange: (m) => {
            setPage(m.page);
            setPageSize(m.pageSize);
          },
        }}
        sorting={false}
        toolbar={{
          filters: (
            <>
              <FormControl size="small" sx={{ minWidth: 180 }}>
                <InputLabel>{t('auditLog.filters.eventType', 'Event Type')}</InputLabel>
                <Select
                  value={eventTypeFilter}
                  label={t('auditLog.filters.eventType', 'Event Type')}
                  onChange={(e: SelectChangeEvent) => {
                    setEventTypeFilter(e.target.value as NotificationType | '');
                    setPage(0);
                  }}
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
                  onChange={(e: SelectChangeEvent) => {
                    setSeverityFilter(e.target.value as AuditLogSeverity | '');
                    setPage(0);
                  }}
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
                onChange={(e) => {
                  setStartDate(e.target.value);
                  setPage(0);
                }}
                InputLabelProps={{ shrink: true }}
                sx={{ width: 160 }}
              />
              <TextField
                size="small"
                label={t('auditLog.filters.endDate', 'End Date')}
                type="date"
                value={endDate}
                onChange={(e) => {
                  setEndDate(e.target.value);
                  setPage(0);
                }}
                InputLabelProps={{ shrink: true }}
                sx={{ width: 160 }}
              />
              <Tooltip title={t('auditLog.filters.clear', 'Clear filters')}>
                <span>
                  <IconButton onClick={clearFilters} disabled={!hasFilters} aria-label={t('auditLog.filters.clear', 'Clear filters')}>
                    <ClearIcon />
                  </IconButton>
                </span>
              </Tooltip>
            </>
          ),
        }}
        detail={{ mode: 'inline', render: (row) => <AuditDetails row={row} /> }}
        emptyState={{ title: t('auditLog.noData', 'No audit log entries found') }}
        tableKey="admin-audit-log"
      />
    </Box>
  );
};

export default AdminAuditLog;
