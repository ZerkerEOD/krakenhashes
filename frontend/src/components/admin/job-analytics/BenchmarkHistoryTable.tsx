import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, FormControl, InputLabel, MenuItem, Select, SelectChangeEvent, Typography } from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { BenchmarkHistoryEntry, JobAnalyticsFilterOptions } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';
import { DataTable, EntityLink, StatusChip } from '../../ui';

interface BenchmarkHistoryTableProps {
  filterOptions: JobAnalyticsFilterOptions | undefined;
}

const formatSpeed = (speed: number): string => {
  if (speed >= 1e12) return `${(speed / 1e12).toFixed(1)} TH/s`;
  if (speed >= 1e9) return `${(speed / 1e9).toFixed(1)} GH/s`;
  if (speed >= 1e6) return `${(speed / 1e6).toFixed(1)} MH/s`;
  if (speed >= 1e3) return `${(speed / 1e3).toFixed(1)} KH/s`;
  return `${Math.round(speed)} H/s`;
};

const BenchmarkHistoryTable: React.FC<BenchmarkHistoryTableProps> = ({ filterOptions }) => {
  const { t } = useTranslation('admin');
  const attackModeLabels: Record<number, string> = useMemo(
    () => ({
      0: t('jobAnalytics.attackModes.straight') as string,
      1: t('jobAnalytics.attackModes.combination') as string,
      3: t('jobAnalytics.attackModes.bruteforce') as string,
      6: t('jobAnalytics.attackModes.hybridWlMask') as string,
      7: t('jobAnalytics.attackModes.hybridMaskWl') as string,
      9: t('jobAnalytics.attackModes.association') as string,
    }),
    [t]
  );
  const [agentId, setAgentId] = useState<number | undefined>();
  const [hashType, setHashType] = useState<number | undefined>();
  const [attackMode, setAttackMode] = useState<number | undefined>();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);

  const params = { agent_id: agentId, hash_type: hashType, attack_mode: attackMode, page, page_size: pageSize };
  const query = useQuery({
    queryKey: ['admin', 'job-analytics', 'benchmark-history', params],
    queryFn: () => jobAnalyticsService.getBenchmarkHistory(params),
    placeholderData: keepPreviousData,
  });
  const data = query.data?.items || [];
  const total = query.data?.pagination?.total || 0;

  const agentNames = useMemo(() => new Map((filterOptions?.agents ?? []).map((a) => [a.id, a.name])), [filterOptions]);
  const hashTypeNames = useMemo(() => new Map((filterOptions?.hash_types ?? []).map((h) => [h.id, h.name])), [filterOptions]);

  const handleSelectChange = (setter: (val: number | undefined) => void) => (e: SelectChangeEvent<string>) => {
    const val = e.target.value;
    setter(val === '' ? undefined : Number(val));
    setPage(1);
  };

  const columns = useMemo<GridColDef<BenchmarkHistoryEntry>[]>(
    () => [
      { field: 'recorded_at', headerName: t('jobAnalytics.benchmarkHistory.columns.date') as string, width: 190, valueFormatter: (v: string) => new Date(v).toLocaleString() },
      {
        field: 'agent_id',
        headerName: t('jobAnalytics.benchmarkChart.agent') as string,
        flex: 1,
        minWidth: 140,
        renderCell: (p) => <EntityLink type="agent" id={p.row.agent_id} label={agentNames.get(p.row.agent_id) ?? (t('jobAnalytics.benchmarkHistory.columns.agentFallback', { id: p.row.agent_id }) as string)} />,
      },
      {
        field: 'hash_type',
        headerName: t('jobAnalytics.benchmarkChart.hashType') as string,
        flex: 1,
        minWidth: 120,
        valueFormatter: (v: number) => (hashTypeNames.has(v) ? `${hashTypeNames.get(v)} (${v})` : String(v)),
      },
      {
        field: 'attack_mode',
        headerName: t('jobAnalytics.benchmarkChart.attackMode') as string,
        width: 140,
        valueFormatter: (v: number) => attackModeLabels[v] || (t('jobAnalytics.attackModes.modeNumber', { mode: v }) as string),
      },
      { field: 'speed', headerName: t('jobAnalytics.benchmarkChart.speed') as string, type: 'number', width: 120, valueFormatter: (v: number) => formatSpeed(v) },
      {
        field: 'success',
        headerName: t('jobAnalytics.benchmarkHistory.columns.status') as string,
        flex: 1.4,
        minWidth: 160,
        renderCell: (p) => (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
            <StatusChip
              entity="generic"
              status={p.row.success ? 'success' : 'failed'}
              label={p.row.success ? (t('jobAnalytics.benchmarkHistory.success') as string) : (t('jobAnalytics.benchmarkHistory.failed') as string)}
              variant="outlined"
            />
            {p.row.error_message && (
              <Typography variant="caption" color="error" noWrap title={p.row.error_message}>
                {p.row.error_message}
              </Typography>
            )}
          </Box>
        ),
      },
    ],
    [agentNames, hashTypeNames, attackModeLabels, t]
  );

  return (
    <DataTable<BenchmarkHistoryEntry>
      rows={data}
      columns={columns}
      loading={query.isLoading}
      fetching={query.isFetching && !query.isLoading}
      error={query.error}
      onRetry={() => void query.refetch()}
      pagination={{
        mode: 'server',
        page: page - 1,
        pageSize,
        rowCount: total,
        pageSizeOptions: [10, 25, 50],
        onChange: (m) => {
          if (m.pageSize !== pageSize) {
            setPageSize(m.pageSize);
            setPage(1);
          } else setPage(m.page + 1);
        },
      }}
      sorting={false}
      toolbar={{
        title: t('jobAnalytics.benchmarkHistory.title') as string,
        filters: (
          <>
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <InputLabel>{t('jobAnalytics.benchmarkChart.agent')}</InputLabel>
              <Select value={agentId !== undefined ? String(agentId) : ''} onChange={handleSelectChange(setAgentId)} label={t('jobAnalytics.benchmarkChart.agent') as string}>
                <MenuItem value="">{t('jobAnalytics.benchmarkChart.all')}</MenuItem>
                {filterOptions?.agents?.map((a) => (
                  <MenuItem key={a.id} value={String(a.id)}>{a.name}</MenuItem>
                ))}
              </Select>
            </FormControl>
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <InputLabel>{t('jobAnalytics.benchmarkChart.hashType')}</InputLabel>
              <Select value={hashType !== undefined ? String(hashType) : ''} onChange={handleSelectChange(setHashType)} label={t('jobAnalytics.benchmarkChart.hashType') as string}>
                <MenuItem value="">{t('jobAnalytics.benchmarkChart.all')}</MenuItem>
                {filterOptions?.hash_types?.map((ht) => (
                  <MenuItem key={ht.id} value={String(ht.id)}>{ht.name}</MenuItem>
                ))}
              </Select>
            </FormControl>
            <FormControl size="small" sx={{ minWidth: 150 }}>
              <InputLabel>{t('jobAnalytics.benchmarkChart.attackMode')}</InputLabel>
              <Select value={attackMode !== undefined ? String(attackMode) : ''} onChange={handleSelectChange(setAttackMode)} label={t('jobAnalytics.benchmarkChart.attackMode') as string}>
                <MenuItem value="">{t('jobAnalytics.benchmarkChart.all')}</MenuItem>
                {filterOptions?.attack_modes?.map((am) => (
                  <MenuItem key={am.value} value={String(am.value)}>{am.label}</MenuItem>
                ))}
              </Select>
            </FormControl>
          </>
        ),
      }}
      emptyState={{ title: t('jobAnalytics.benchmarkHistory.empty') as string }}
      sx={{ mb: 3 }}
    />
  );
};

export default BenchmarkHistoryTable;
