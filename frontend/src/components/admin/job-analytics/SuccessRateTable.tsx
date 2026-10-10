import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Chip, LinearProgress, Typography } from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import { useQuery } from '@tanstack/react-query';
import { SuccessRateEntry, JobAnalyticsFilterParams } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';
import { DataTable, EntityLink } from '../../ui';

interface SuccessRateTableProps {
  filter: JobAnalyticsFilterParams;
}

type Row = SuccessRateEntry & { __id: number };

const formatDuration = (seconds: number | null): string => {
  if (seconds === null || seconds === 0) return '-';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
};

const formatNumber = (n: number): string => {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return String(n);
};

const successRateColor = (rate: number): 'success' | 'warning' | 'error' => {
  if (rate >= 30) return 'success';
  if (rate >= 10) return 'warning';
  return 'error';
};

const SuccessRateTable: React.FC<SuccessRateTableProps> = ({ filter }) => {
  const { t } = useTranslation('admin');
  const query = useQuery({
    queryKey: ['admin', 'job-analytics', 'success-rates', filter],
    queryFn: () => jobAnalyticsService.getSuccessRates(filter),
  });
  const rows = useMemo<Row[]>(() => (query.data?.entries || []).map((e, i) => ({ ...e, __id: i })), [query.data]);

  const columns = useMemo<GridColDef<Row>[]>(
    () => [
      {
        field: 'display_name',
        headerName: t('jobAnalytics.successRate.columns.configuration') as string,
        flex: 2,
        minWidth: 240,
        renderCell: (p) => (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
            {p.row.is_preset && (
              <Chip label={t('jobAnalytics.successRate.preset') as string} size="small" color="primary" variant="outlined" sx={{ fontSize: '0.7rem', height: 20 }} />
            )}
            <Typography variant="body2" noWrap title={p.row.display_name}>
              {p.row.is_preset && p.row.preset_id ? (
                <EntityLink type="preset_job" id={p.row.preset_id} label={p.row.display_name} />
              ) : (
                p.row.display_name
              )}
            </Typography>
          </Box>
        ),
      },
      { field: 'attack_mode_label', headerName: t('jobAnalytics.benchmarkChart.attackMode') as string, width: 140 },
      { field: 'hash_type_name', headerName: t('jobAnalytics.benchmarkChart.hashType') as string, flex: 1, minWidth: 130 },
      { field: 'total_runs', headerName: t('jobAnalytics.successRate.columns.runs') as string, type: 'number', width: 80 },
      { field: 'total_hashes', headerName: t('jobAnalytics.successRate.columns.hashes') as string, type: 'number', width: 100, valueFormatter: (v: number) => formatNumber(v) },
      { field: 'total_cracks', headerName: t('jobAnalytics.successRate.columns.cracks') as string, type: 'number', width: 100, valueFormatter: (v: number) => formatNumber(v) },
      {
        field: 'success_rate_percent',
        headerName: t('jobAnalytics.successRate.columns.successRate') as string,
        type: 'number',
        width: 150,
        renderCell: (p) => {
          const rate = p.row.success_rate_percent;
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, justifyContent: 'flex-end', width: '100%' }}>
              <LinearProgress
                variant="determinate"
                value={Math.min(rate, 100)}
                color={successRateColor(rate)}
                sx={{ width: 50, height: 6, borderRadius: 3 }}
              />
              <Chip
                label={`${rate.toFixed(1)}%`}
                size="small"
                color={successRateColor(rate)}
                variant="outlined"
                sx={{ fontSize: '0.75rem', height: 22, minWidth: 55 }}
              />
            </Box>
          );
        },
      },
      {
        field: 'avg_job_duration_seconds',
        headerName: t('jobAnalytics.successRate.columns.avgDuration') as string,
        type: 'number',
        width: 110,
        valueFormatter: (v: number | null) => formatDuration(v),
      },
      {
        field: 'total_compute_seconds',
        headerName: t('jobAnalytics.successRate.columns.totalCompute') as string,
        type: 'number',
        width: 120,
        valueFormatter: (v: number | null) => formatDuration(v),
      },
    ],
    [t]
  );

  return (
    <DataTable<Row>
      rows={rows}
      columns={columns}
      getRowId={(r) => r.__id}
      loading={query.isLoading}
      fetching={query.isFetching && !query.isLoading}
      error={query.error}
      onRetry={() => void query.refetch()}
      pagination={{ mode: 'client', initialPageSize: 25 }}
      sorting={{ mode: 'client', initial: [{ field: 'success_rate_percent', sort: 'desc' }] }}
      toolbar={{
        title: t('jobAnalytics.successRate.title') as string,
        subtitle: t('jobAnalytics.successRate.subtitle') as string,
      }}
      emptyState={{ title: t('jobAnalytics.successRate.empty') as string }}
      sx={{ mb: 3 }}
    />
  );
};

export default SuccessRateTable;
