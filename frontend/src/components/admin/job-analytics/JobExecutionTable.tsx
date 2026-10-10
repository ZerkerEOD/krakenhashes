import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, LinearProgress, Skeleton, Typography } from '@mui/material';
import type { GridColDef, GridSortModel } from '@mui/x-data-grid';
import { useQuery } from '@tanstack/react-query';
import { JobAnalyticsEntry, TaskSegment } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';
import { DataTable, EntityLink, SimpleTable, StatusChip } from '../../ui';

interface JobExecutionTableProps {
  jobs: JobAnalyticsEntry[] | undefined;
  total: number;
  /** 1-based. */
  page: number;
  pageSize: number;
  sortBy: string;
  sortOrder: string;
  loading: boolean;
  /** 1-based. */
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onSortChange: (sortBy: string, sortOrder: string) => void;
}

const formatSpeed = (speed: number): string => {
  if (speed >= 1e12) return `${(speed / 1e12).toFixed(1)} TH/s`;
  if (speed >= 1e9) return `${(speed / 1e9).toFixed(1)} GH/s`;
  if (speed >= 1e6) return `${(speed / 1e6).toFixed(1)} MH/s`;
  if (speed >= 1e3) return `${(speed / 1e3).toFixed(1)} KH/s`;
  return `${Math.round(speed)} H/s`;
};

const formatDuration = (seconds: number | null): string => {
  if (seconds === null) return '-';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
};

const formatKeyspace = (ks: number): string => {
  if (ks >= 1e15) return `${(ks / 1e15).toFixed(1)}P`;
  if (ks >= 1e12) return `${(ks / 1e12).toFixed(1)}T`;
  if (ks >= 1e9) return `${(ks / 1e9).toFixed(1)}G`;
  if (ks >= 1e6) return `${(ks / 1e6).toFixed(1)}M`;
  if (ks >= 1e3) return `${(ks / 1e3).toFixed(1)}K`;
  return String(ks);
};

const fmtDate = (v: string | null) => (v ? new Date(v).toLocaleString() : '-');

/** Per-task breakdown of one job, loaded when its detail drawer opens. */
const JobTimelineDetail: React.FC<{ jobId: string }> = ({ jobId }) => {
  const { t } = useTranslation('admin');
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'job-analytics', 'timeline', jobId],
    queryFn: () => jobAnalyticsService.getJobTimeline(jobId),
  });
  const tasks = data?.tasks || [];
  const metrics = data?.metrics || [];

  if (isLoading) return <Skeleton variant="rectangular" height={100} />;

  return (
    <Box>
      <Typography variant="subtitle2" gutterBottom>
        {t('jobAnalytics.jobExecution.taskBreakdown', {
          tasksPhrase: t('jobAnalytics.jobExecution.taskCount', { count: tasks.length }),
          metricsPhrase: t('jobAnalytics.jobExecution.metricPointCount', { count: metrics.length }),
        })}
      </Typography>
      <SimpleTable<TaskSegment>
        rows={tasks}
        getRowKey={(r) => r.task_id}
        emptyState={{ title: t('jobAnalytics.jobExecution.noTaskData') as string }}
        columns={[
          {
            field: 'agent',
            headerName: t('jobAnalytics.benchmarkChart.agent') as string,
            render: (r) => <EntityLink type="agent" id={r.agent_id} label={`${r.agent_name} (#${r.agent_id})`} />,
          },
          { field: 'status', headerName: t('jobAnalytics.benchmarkHistory.columns.status') as string, render: (r) => <StatusChip entity="task" status={r.status} /> },
          { field: 'average_speed', headerName: t('jobAnalytics.summary.avgSpeed') as string, align: 'right', render: (r) => formatSpeed(r.average_speed) },
          { field: 'benchmark_speed', headerName: t('jobAnalytics.jobExecution.columns.benchmark') as string, align: 'right', render: (r) => formatSpeed(r.benchmark_speed) },
          { field: 'crack_count', headerName: t('jobAnalytics.successRate.columns.cracks') as string, align: 'right' },
          { field: 'started_at', headerName: t('jobAnalytics.jobExecution.columns.started') as string, render: (r) => fmtDate(r.started_at) },
          { field: 'completed_at', headerName: t('jobAnalytics.jobExecution.columns.completed') as string, render: (r) => fmtDate(r.completed_at) },
        ]}
      />
    </Box>
  );
};

const JobExecutionTable: React.FC<JobExecutionTableProps> = ({
  jobs,
  total,
  page,
  pageSize,
  sortBy,
  sortOrder,
  loading,
  onPageChange,
  onPageSizeChange,
  onSortChange,
}) => {
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
  const columns = useMemo<GridColDef<JobAnalyticsEntry>[]>(
    () => [
      {
        field: 'name',
        headerName: t('jobAnalytics.jobExecution.columns.name') as string,
        flex: 1.6,
        minWidth: 200,
        renderCell: (p) => (
          <Box sx={{ minWidth: 0, py: 0.5 }}>
            <Typography variant="body2" noWrap>
              <EntityLink type="job" id={p.row.id} label={p.row.name} />
            </Typography>
            <Typography variant="caption" color="text.secondary" noWrap component="div">
              <EntityLink type="hashlist" id={p.row.hashlist_id} label={p.row.hashlist_name} color="inherit" />
            </Typography>
          </Box>
        ),
      },
      {
        field: 'attack_mode',
        headerName: t('jobAnalytics.jobExecution.columns.attack') as string,
        width: 110,
        valueFormatter: (v: number) => attackModeLabels[v] || (t('jobAnalytics.attackModes.modeNumber', { mode: v }) as string),
      },
      {
        field: 'hash_type_name',
        headerName: t('jobAnalytics.benchmarkChart.hashType') as string,
        flex: 1,
        minWidth: 150,
        renderCell: (p) => (
          <Typography variant="body2" sx={{ whiteSpace: 'normal', lineHeight: 1.3 }}>
            {p.value}
          </Typography>
        ),
      },
      {
        field: 'status',
        headerName: t('jobAnalytics.benchmarkHistory.columns.status') as string,
        width: 120,
        renderCell: (p) => <StatusChip entity="job" status={p.row.status} />,
      },
      {
        field: 'duration_seconds',
        headerName: t('jobAnalytics.jobExecution.columns.duration') as string,
        type: 'number',
        width: 100,
        valueFormatter: (v: number | null) => formatDuration(v),
      },
      { field: 'avg_speed', headerName: t('jobAnalytics.summary.avgSpeed') as string, type: 'number', width: 120, valueFormatter: (v: number) => formatSpeed(v) },
      { field: 'total_cracks', headerName: t('jobAnalytics.successRate.columns.cracks') as string, type: 'number', width: 90 },
      { field: 'effective_keyspace', headerName: t('jobAnalytics.jobExecution.columns.keyspace') as string, type: 'number', width: 100, valueFormatter: (v: number) => formatKeyspace(v) },
      { field: 'unique_agents', headerName: t('jobAnalytics.jobExecution.columns.agents') as string, type: 'number', width: 80 },
      {
        field: 'overall_progress_percent',
        headerName: t('jobAnalytics.jobExecution.columns.progress') as string,
        type: 'number',
        width: 130,
        renderCell: (p) => (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, justifyContent: 'flex-end', width: '100%' }}>
            <LinearProgress
              variant="determinate"
              value={Math.min(p.row.overall_progress_percent, 100)}
              sx={{ width: 60, height: 6, borderRadius: 3 }}
            />
            <Typography variant="caption">{p.row.overall_progress_percent.toFixed(0)}%</Typography>
          </Box>
        ),
      },
    ],
    [t, attackModeLabels]
  );

  const sortModel: GridSortModel = sortBy ? [{ field: sortBy, sort: sortOrder === 'asc' ? 'asc' : 'desc' }] : [];

  return (
    <DataTable<JobAnalyticsEntry>
      rows={jobs ?? []}
      columns={columns}
      loading={loading && !jobs?.length}
      fetching={loading && Boolean(jobs?.length)}
      pagination={{
        mode: 'server',
        page: page - 1,
        pageSize,
        rowCount: total,
        pageSizeOptions: [10, 25, 50],
        onChange: (m) => {
          if (m.pageSize !== pageSize) onPageSizeChange(m.pageSize);
          else onPageChange(m.page + 1);
        },
      }}
      sorting={{
        mode: 'server',
        model: sortModel,
        onChange: (m) => {
          const s = m[0];
          if (s?.sort) onSortChange(s.field, s.sort);
        },
      }}
      toolbar={{ title: t('jobAnalytics.jobExecution.title') as string }}
      detail={{
        mode: 'drawer',
        drawerTitle: (row) => row.name,
        drawerWidth: 720,
        render: (row) => <JobTimelineDetail jobId={row.id} />,
      }}
      emptyState={{ title: t('jobAnalytics.jobExecution.empty') as string }}
      gridProps={{ sortingOrder: ['asc', 'desc'] }}
      sx={{ mb: 3 }}
    />
  );
};

export default JobExecutionTable;
