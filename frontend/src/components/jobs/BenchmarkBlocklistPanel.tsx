import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Chip, CircularProgress, Tooltip, Typography } from '@mui/material';
import { Replay as ReplayIcon } from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { api } from '../../services/api';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { DataTable, EntityLink, SectionCard, useToast } from '../ui';
import { getErrorMessage } from '../../utils/errors';

// Kept in sync with blocklistEntryDTO in backend/internal/handlers/jobs/user_jobs.go.
interface BlocklistEntry {
  id: string;
  agent_id: number;
  agent_name?: string;
  job_execution_id?: string;
  attack_mode: number;
  hash_type: number;
  reason: string;
  expires_at: string;
  created_at: string;
  failure_count?: number;
  last_error?: string;
  cleared_at?: string;
}

interface Props {
  jobId: string;
}

const formatRelativeFuture = (iso: string, expiredLabel: string): string => {
  const diffMs = new Date(iso).getTime() - Date.now();
  if (diffMs <= 0) return expiredLabel;
  const mins = Math.round(diffMs / 60000);
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  const rem = mins % 60;
  if (hrs < 48) return rem === 0 ? `${hrs}h` : `${hrs}h ${rem}m`;
  return `${Math.round(hrs / 24)}d`;
};

const BenchmarkBlocklistPanel: React.FC<Props> = ({ jobId }) => {
  const { t } = useTranslation('jobs');
  const toast = useToast();
  const [clearingId, setClearingId] = useState<string | null>(null);

  // Refresh every 60s (slow tier) so expired entries drop off without a manual reload.
  const query = useLiveQuery(
    {
      queryKey: ['jobs', 'detail', jobId, 'benchmark-blocklist'],
      queryFn: async () => (await api.get<BlocklistEntry[]>(`/api/jobs/${jobId}/benchmark-blocklist`)).data || [],
    },
    { tier: 'slow' }
  );
  const entries = useMemo(() => query.data ?? [], [query.data]);

  const handleRetry = async (entryId: string) => {
    setClearingId(entryId);
    try {
      await api.post(`/api/jobs/${jobId}/benchmark-blocklist/${entryId}/clear`);
      toast.success(t('benchmarkBlocklist.clearSuccess') as string);
      await query.refetch();
    } catch (e) {
      toast.error(
        t('benchmarkBlocklist.clearFailedPrefix', {
          error: getErrorMessage(e) || (t('benchmarkBlocklist.clearFailedDefault') as string),
        }) as string
      );
    } finally {
      setClearingId(null);
    }
  };

  const columns = useMemo<GridColDef<BlocklistEntry>[]>(
    () => [
      {
        field: 'agent_id',
        headerName: t('benchmarkBlocklist.columns.agent') as string,
        flex: 1,
        minWidth: 160,
        renderCell: (p) => (
          <EntityLink
            type="agent"
            id={p.row.agent_id}
            label={p.row.agent_name ? `${p.row.agent_name} (#${p.row.agent_id})` : `#${p.row.agent_id}`}
          />
        ),
      },
      {
        field: 'job_execution_id',
        headerName: t('benchmarkBlocklist.columns.scope') as string,
        width: 110,
        renderCell: (p) =>
          p.row.job_execution_id ? (
            <Chip size="small" label={t('benchmarkBlocklist.scope.thisJob') as string} color="warning" variant="outlined" />
          ) : (
            <Chip size="small" label={t('benchmarkBlocklist.scope.global') as string} color="error" variant="outlined" />
          ),
      },
      {
        field: 'hash_type',
        headerName: t('benchmarkBlocklist.columns.hashMode') as string,
        width: 120,
        renderCell: (p) => (
          <Typography component="span" sx={{ fontFamily: (th: any) => th.typography.monoFamily, fontSize: '0.8rem' }}>
            {p.row.hash_type} / {p.row.attack_mode}
          </Typography>
        ),
      },
      { field: 'failure_count', headerName: t('benchmarkBlocklist.columns.failures') as string, width: 90, valueFormatter: (v: number | undefined) => v ?? '—' },
      {
        field: 'reason',
        headerName: t('benchmarkBlocklist.columns.reason') as string,
        flex: 2,
        minWidth: 200,
        renderCell: (p) => (
          <Tooltip title={p.row.last_error || p.row.reason}>
            <Typography variant="body2" noWrap>
              {p.row.reason}
            </Typography>
          </Tooltip>
        ),
      },
      {
        field: 'expires_at',
        headerName: t('benchmarkBlocklist.columns.expires') as string,
        width: 100,
        valueFormatter: (v: string) => formatRelativeFuture(v, t('benchmarkBlocklist.expired') as string),
      },
      {
        field: 'action',
        headerName: t('benchmarkBlocklist.columns.action') as string,
        width: 140,
        align: 'right',
        headerAlign: 'right',
        sortable: false,
        renderCell: (p) => (
          <Button
            size="small"
            variant="outlined"
            startIcon={clearingId === p.row.id ? <CircularProgress size={14} /> : <ReplayIcon />}
            disabled={clearingId !== null}
            onClick={() => void handleRetry(p.row.id)}
          >
            {t('benchmarkBlocklist.retryNow')}
          </Button>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [clearingId, t]
  );

  // Hide the panel entirely when there's nothing to show — no value in a
  // "no entries" message on every job detail page.
  if (!query.isLoading && entries.length === 0 && !query.error) {
    return null;
  }

  return (
    <SectionCard
      title={t('benchmarkBlocklist.title') as string}
      subtitle={t('benchmarkBlocklist.subtitle') as string}
      loading={query.isFetching}
      flush
      sx={{ mt: 3 }}
    >
      <DataTable<BlocklistEntry>
        flat
        rows={entries}
        columns={columns}
        loading={query.isLoading}
        error={query.error ? getErrorMessage(query.error) || (t('benchmarkBlocklist.loadFailed') as string) : undefined}
        onRetry={() => void query.refetch()}
        pagination={false}
        sorting={false}
      />
    </SectionCard>
  );
};

export default BenchmarkBlocklistPanel;
