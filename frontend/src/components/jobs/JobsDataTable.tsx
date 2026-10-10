import React, { useMemo } from 'react';
import { Alert, Box, Chip, LinearProgress, Tooltip, Typography } from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import RefreshIcon from '@mui/icons-material/Refresh';
import ArchiveIcon from '@mui/icons-material/Archive';
import UnarchiveIcon from '@mui/icons-material/Unarchive';
import SpeedIcon from '@mui/icons-material/Speed';
import PeopleIcon from '@mui/icons-material/People';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { GridColDef, GridRowId } from '@mui/x-data-grid';
import { DataTable, EntityLink, StatusChip, useConfirm, useToast } from '../ui';
import type { DataTablePagination } from '../ui';
import EditableCell from '../../pages/Jobs/EditableCell';
import { api, archiveJob, unarchiveJob } from '../../services/api';
import { getMaxPriorityForUsers } from '../../services/systemSettings';
import { formatters, formatDateTime } from '../../utils/formatters';
import { calculateJobProgress, formatKeyspace, getKeyspaceTooltip } from '../../utils/jobProgress';
import { getErrorMessage } from '../../utils/errors';
import type { JobSummary } from '../../types/jobs';

const ACTIVE = ['preparing', 'pending', 'running', 'paused'];
const FINISHED = ['completed', 'cancelled'];

/** Stops the grid from treating keys/clicks inside an inline editor as cell navigation. */
const CellEditor: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Box
    onClick={(e) => e.stopPropagation()}
    onKeyDown={(e) => e.stopPropagation()}
    sx={{ width: '100%', display: 'flex', justifyContent: 'center' }}
  >
    {children}
  </Box>
);

export interface JobsDataTableProps {
  jobs: JobSummary[];
  loading?: boolean;
  fetching?: boolean;
  error?: unknown;
  onRetry?: () => void;
  pagination?: DataTablePagination;
  /** Called after any mutation so the owner can refetch. */
  onChanged?: () => void;
  selection?: { model: GridRowId[]; onChange: (ids: GridRowId[]) => void };
  bulkActions?: (ids: GridRowId[], clear: () => void) => React.ReactNode;
  toolbar?: React.ComponentProps<typeof DataTable>['toolbar'];
  /** Hide the less important columns (dashboard widget). */
  compact?: boolean;
  /** No outer Paper (use inside a flush SectionCard). */
  flat?: boolean;
  emptyState?: React.ComponentProps<typeof DataTable>['emptyState'];
  tableKey?: string;
}

/**
 * The jobs list used by the Jobs page and the dashboard: links to every
 * referenced entity, inline-editable priority/max agents, row actions, and an
 * expandable error panel for failed jobs. Server-ordered (FIFO within status
 * groups), so column sorting is off.
 */
const JobsDataTable: React.FC<JobsDataTableProps> = ({
  jobs,
  loading,
  fetching,
  error,
  onRetry,
  pagination = false,
  onChanged,
  selection,
  bulkActions,
  toolbar,
  compact,
  flat,
  emptyState,
  tableKey,
}) => {
  const { t } = useTranslation('jobs');
  const { t: tDash } = useTranslation('dashboard');
  const toast = useToast();
  const confirm = useConfirm();
  const { data: maxPriorityCfg } = useQuery({ queryKey: ['settings', 'max-priority', 'users'], queryFn: getMaxPriorityForUsers, staleTime: 60_000 });
  const maxPriority = maxPriorityCfg?.max_priority ?? 10;

  const patchJob = async (id: string, body: Record<string, number>) => {
    await api.patch(`/api/jobs/${id}`, body);
    onChanged?.();
  };

  const run = async (fn: () => Promise<unknown>, fallback: string) => {
    try {
      await fn();
      onChanged?.();
    } catch (err) {
      toast.error(getErrorMessage(err) || fallback);
    }
  };

  const columns = useMemo<GridColDef<JobSummary>[]>(() => {
    const cols: GridColDef<JobSummary>[] = [
      {
        field: 'name',
        headerName: tDash('jobsTable.columns.jobName') as string,
        flex: 1.6,
        minWidth: 220,
        sortable: false,
        renderCell: (p) => {
          const job = p.row;
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0, flexWrap: compact ? 'nowrap' : 'wrap', py: 0.5 }}>
              <EntityLink
                type="job"
                id={job.id}
                label={job.name}
                sx={{ fontWeight: 500, ...(compact ? { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } : {}) }}
              />
              <StatusChip entity="job" status={job.status} />
              {job.archived_at && <Chip size="small" variant="outlined" icon={<ArchiveIcon />} label={t('archive.archived') as string} />}
            </Box>
          );
        },
      },
      {
        field: 'hashlist_name',
        headerName: tDash('jobsTable.columns.hashlist') as string,
        flex: 1.3,
        minWidth: 180,
        sortable: false,
        renderCell: (p) => {
          const job = p.row;
          const attack = job.workflow ?? job.preset_job;
          const completedTip = compact && job.completed_at ? `${t('row.completed') as string} ${formatDateTime(job.completed_at)}` : '';
          return (
            <Tooltip title={completedTip} placement="top-start">
              <Box sx={{ py: 0.5, minWidth: 0 }}>
                <EntityLink type="hashlist" id={job.hashlist_id} label={job.hashlist_name} />
                <Typography variant="caption" color="text.secondary" component="div" noWrap>
                  {attack ? (
                    <EntityLink type={job.workflow ? 'workflow' : 'preset_job'} id={attack.id} label={attack.name} color="inherit" />
                  ) : (
                    job.preset_job_name || t('row.custom', 'Custom attack')
                  )}
                </Typography>
                {job.completed_at && !compact && (
                  <Typography variant="caption" color="text.secondary" component="div">
                    {t('row.completed') as string} {formatDateTime(job.completed_at)}
                  </Typography>
                )}
              </Box>
            </Tooltip>
          );
        },
      },
    ];

    if (!compact) {
      cols.push(
        {
          field: 'client',
          headerName: t('columns.client', 'Client') as string,
          flex: 0.9,
          minWidth: 120,
          sortable: false,
          renderCell: (p) => (p.row.client ? <EntityLink type="client" id={p.row.client.id} label={p.row.client.name} /> : '-'),
        },
        {
          field: 'created_by',
          headerName: tDash('jobsTable.columns.createdBy') as string,
          flex: 0.8,
          minWidth: 110,
          sortable: false,
          renderCell: (p) =>
            p.row.created_by ? (
              <EntityLink type="user" id={p.row.created_by.id} label={p.row.created_by.username} />
            ) : (
              <Typography variant="body2" color="text.secondary">{p.row.created_by_username || (t('row.unknown') as string)}</Typography>
            ),
        }
      );
    }

    cols.push({
      field: 'progress',
      headerName: tDash('jobsTable.columns.progress') as string,
      flex: 1.2,
      minWidth: 170,
      sortable: false,
      renderCell: (p) => {
        const job = p.row;
        const searched = job.searched_percent || 0;
        const dispatched = job.dispatched_percent || 0;
        const overall = job.overall_progress_percent || searched;
        const tone = job.status === 'failed' ? 'error' : job.status === 'completed' ? 'success' : 'primary';
        const detail = `${searched.toFixed(3)}% / ${dispatched.toFixed(3)}%${job.effective_keyspace ? ` · ${calculateJobProgress(job).displayText}` : ''}`;
        const bar = (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <LinearProgress variant="determinate" value={Math.min(100, overall)} color={tone} sx={{ flexGrow: 1, height: 6, borderRadius: 3 }} />
            <Typography variant="body2" color="text.secondary" sx={{ minWidth: 44, textAlign: 'right' }}>
              {overall.toFixed(1)}%
            </Typography>
          </Box>
        );
        // Compact (dashboard): one line, the searched/dispatched detail moves to a tooltip.
        if (compact) {
          return (
            <Tooltip title={detail}>
              <Box sx={{ width: '100%' }}>{bar}</Box>
            </Tooltip>
          );
        }
        return (
          <Box sx={{ width: '100%', py: 0.5 }}>
            {bar}
            <Typography variant="caption" color="text.secondary" component="div">
              {detail}
            </Typography>
          </Box>
        );
      },
    });

    if (!compact) {
      cols.push({
        field: 'effective_keyspace',
        headerName: tDash('jobsTable.columns.keyspace') as string,
        width: 110,
        align: 'center',
        headerAlign: 'center',
        sortable: false,
        renderCell: (p) =>
          p.row.effective_keyspace ? (
            <Tooltip title={getKeyspaceTooltip(p.row) || ''} arrow>
              <span>{formatKeyspace(p.row.effective_keyspace)}</span>
            </Tooltip>
          ) : (
            '-'
          ),
      });
    }

    cols.push(
      {
        field: 'cracked_count',
        headerName: tDash('jobsTable.columns.cracked') as string,
        width: 90,
        align: 'center',
        headerAlign: 'center',
        sortable: false,
        renderCell: (p) =>
          p.row.cracked_count > 0 ? (
            <EntityLink type="pot_job" id={p.row.id} label={p.row.cracked_count.toLocaleString()} sx={{ fontWeight: 500 }} />
          ) : (
            '0'
          ),
      },
      {
        field: 'agent_count',
        headerName: tDash('jobsTable.columns.agents') as string,
        width: compact ? 120 : 170,
        sortable: false,
        renderCell: (p) => {
          const job = p.row;
          const over = job.max_agents > 0 && job.agent_count > job.max_agents ? job.agent_count - job.max_agents : 0;
          const under =
            (job.status === 'running' || job.status === 'pending') && job.max_agents > 0 && job.agent_count < job.max_agents
              ? job.max_agents - job.agent_count
              : 0;
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
              <PeopleIcon fontSize="small" color="action" />
              <Typography variant="body2">{job.agent_count}</Typography>
              {over > 0 && (
                <Tooltip title={`+${over} from overflow — higher-priority or older jobs are saturated, so spare agents cascaded here`}>
                  <Chip label={`+${over}`} size="small" color="warning" variant="outlined" sx={{ height: 18, fontSize: '0.65rem' }} />
                </Tooltip>
              )}
              {under > 0 && (
                <Tooltip title={`${under} short of max agents — waiting on agents to free up, or the remaining keyspace is fully tiled by current chunks`}>
                  <Chip label={`-${under}`} size="small" color="info" variant="outlined" sx={{ height: 18, fontSize: '0.65rem' }} />
                </Tooltip>
              )}
              {job.total_speed > 0 && (
                <Tooltip title={t('tooltips.combinedHashRate') as string}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, ml: 0.5 }}>
                    <SpeedIcon fontSize="small" color="action" />
                    <Typography variant="caption" color="text.secondary">{formatters.formatHashRate(job.total_speed)}</Typography>
                  </Box>
                </Tooltip>
              )}
            </Box>
          );
        },
      }
    );

    if (!compact) {
      cols.push({
        field: 'priority',
        headerName: tDash('jobsTable.columns.priority') as string,
        width: 110,
        align: 'center',
        headerAlign: 'center',
        sortable: false,
        renderCell: (p) =>
          FINISHED.includes(p.row.status) ? (
            p.row.priority
          ) : (
            <CellEditor>
              <EditableCell
                value={p.row.priority}
                min={1}
                max={maxPriority}
                onSave={(v) => patchJob(p.row.id, { priority: v })}
                validation={(value) => {
                  const n = Number(value);
                  return isNaN(n) || n < 1 || n > maxPriority ? (t('validation.priorityRange', { max: maxPriority }) as string) : null;
                }}
              />
            </CellEditor>
          ),
      });
    }

    if (!compact) {
      cols.push({
        field: 'max_agents',
        headerName: tDash('jobsTable.columns.maxAgents') as string,
        width: 110,
        align: 'center',
        headerAlign: 'center',
        sortable: false,
        renderCell: (p) =>
          FINISHED.includes(p.row.status) ? (
            p.row.max_agents
          ) : (
            <CellEditor>
              <EditableCell
                value={p.row.max_agents}
                min={1}
                max={100}
                onSave={(v) => patchJob(p.row.id, { max_agents: v })}
                validation={(value) => {
                  const n = Number(value);
                  return isNaN(n) || n < 1 || n > 100 ? (t('validation.maxAgentsRange') as string) : null;
                }}
              />
            </CellEditor>
          ),
      });
    }
    return cols;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t, tDash, compact, maxPriority]);

  return (
    <DataTable<JobSummary>
      rows={jobs}
      columns={columns}
      loading={loading}
      fetching={fetching}
      error={error}
      onRetry={onRetry}
      pagination={pagination}
      sorting={false}
      toolbar={toolbar}
      selection={selection}
      bulkActions={bulkActions}
      rowClassName={(job) => (job.archived_at ? 'kh-row-muted' : undefined)}
      detail={{
        mode: 'inline',
        canExpand: (job) => Boolean(job.error_message && job.status === 'failed'),
        render: (job) => (
          <Alert severity="error" variant="outlined">
            <strong>{t('row.error') as string}</strong> {job.error_message}
          </Alert>
        ),
      }}
      rowActions={(job) => [
        {
          key: 'retry',
          label: t('tooltips.retry') as string,
          icon: <RefreshIcon fontSize="small" />,
          hidden: !['failed', 'cancelled'].includes(job.status),
          onClick: (r) => void run(() => api.post(`/api/jobs/${r.id}/retry`), 'Failed to retry job'),
        },
        {
          key: 'archive',
          label: (job.archived_at ? t('archive.unarchive') : t('archive.archive')) as string,
          icon: job.archived_at ? <UnarchiveIcon fontSize="small" /> : <ArchiveIcon fontSize="small" />,
          disabled: ACTIVE.includes(job.status),
          placement: 'menu',
          onClick: (r) => void run(() => (r.archived_at ? unarchiveJob(r.id) : archiveJob(r.id)), 'Failed to archive job'),
        },
        {
          key: 'delete',
          label: t('tooltips.delete') as string,
          icon: <DeleteIcon fontSize="small" />,
          danger: true,
          placement: 'menu',
          onClick: async (r) => {
            const ok = await confirm({
              title: t('confirmDelete.title') as string,
              message: t('confirmDelete.messageNamed', { name: r.name }) as string,
              severity: 'danger',
              confirmLabel: t('tooltips.delete') as string,
              action: () => api.delete(`/api/jobs/${r.id}`),
            });
            if (ok) onChanged?.();
          },
        },
      ]}
      emptyState={emptyState ?? { title: tDash('jobsTable.noJobs') as string, description: tDash('jobsTable.noJobsHint') as string }}
      tableKey={tableKey}
      flat={flat}
    />
  );
};

export default JobsDataTable;
