import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Checkbox,
  Chip,
  FormControl,
  FormControlLabel,
  InputLabel,
  MenuItem,
  Select,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import RefreshIcon from '@mui/icons-material/Refresh';
import ArchiveIcon from '@mui/icons-material/Archive';
import UnarchiveIcon from '@mui/icons-material/Unarchive';
import { keepPreviousData, useQueryClient } from '@tanstack/react-query';
import type { GridRowId } from '@mui/x-data-grid';
import LoopbackSessionsPanel from '../../components/jobs/LoopbackSessionsPanel';
import JobsDataTable from '../../components/jobs/JobsDataTable';
import { PageHeader, useConfirm, useToast } from '../../components/ui';
import { api, archiveJob, unarchiveJob } from '../../services/api';
import { JobSummary, PaginationInfo } from '../../types/jobs';
import { useTeamFilter } from '../../contexts/TeamFilterContext';
import { usePolling } from '../../contexts/PollingContext';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import useDebounce from '../../hooks/useDebounce';
import { qk } from '../../services/queryKeys';
import { getErrorMessage } from '../../utils/errors';

interface JobsResponse {
  jobs: JobSummary[];
  pagination: PaginationInfo;
  status_counts: Record<string, number>;
}

const STATUS_FILTERS = ['pending', 'running', 'completed', 'failed'] as const;
const BADGE_COLOR: Record<string, 'default' | 'primary' | 'success' | 'error'> = {
  pending: 'default',
  running: 'primary',
  completed: 'success',
  failed: 'error',
};

/** Count shown inside a status filter button (inline, never overlapping the label). */
const FilterCount: React.FC<{ value: number; tone?: 'default' | 'primary' | 'success' | 'error' }> = ({ value, tone = 'default' }) => (
  <Box
    component="span"
    sx={{
      minWidth: 22,
      px: 0.75,
      py: 0.125,
      borderRadius: 10,
      fontSize: '0.7rem',
      fontWeight: 600,
      lineHeight: 1.6,
      textAlign: 'center',
      bgcolor: tone === 'default' ? 'action.selected' : `${tone}.main`,
      color: tone === 'default' ? 'text.primary' : `${tone}.contrastText`,
      opacity: value === 0 ? 0.6 : 1,
    }}
  >
    {value.toLocaleString()}
  </Box>
);

const Jobs: React.FC = () => {
  const { t } = useTranslation('jobs');
  const toast = useToast();
  const confirm = useConfirm();
  const queryClient = useQueryClient();
  const { teamsEnabled, selectedTeamId } = useTeamFilter();
  const { enabled: polling, setEnabled: setPolling } = usePolling();

  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const [status, setStatus] = useState<string | null>(null);
  const [priority, setPriority] = useState<number | null>(null);
  const [search, setSearch] = useState('');
  const [showArchived, setShowArchived] = useState(false);
  const [selected, setSelected] = useState<GridRowId[]>([]);
  const [bulkBusy, setBulkBusy] = useState(false);
  const debouncedSearch = useDebounce(search, 400);

  const params = useMemo(() => {
    const p: Record<string, string> = { page: String(page + 1), page_size: String(pageSize) };
    if (status) p.status = status;
    if (priority !== null) p.priority = String(priority);
    if (debouncedSearch.trim()) p.search = debouncedSearch.trim();
    if (teamsEnabled && selectedTeamId) p.team_id = selectedTeamId;
    if (showArchived) p.include_archived = 'true';
    return p;
  }, [page, pageSize, status, priority, debouncedSearch, teamsEnabled, selectedTeamId, showArchived]);

  const query = useLiveQuery<JobsResponse>(
    {
      queryKey: qk.jobs.list(params),
      queryFn: async ({ signal }) => (await api.get<JobsResponse>('/api/jobs', { params, signal })).data,
      placeholderData: keepPreviousData,
    },
    { tier: 'live' }
  );
  const jobs = query.data?.jobs ?? [];
  const counts = query.data?.status_counts ?? {};
  const total = Object.values(counts).reduce((a, b) => a + b, 0);
  const refresh = () => queryClient.invalidateQueries({ queryKey: qk.jobs.all });
  const resetPage = () => {
    setPage(0);
    setSelected([]);
  };

  const selectedJobs = selected.map((id) => jobs.find((j) => j.id === id)).filter(Boolean) as JobSummary[];

  const runBulk = async (ids: string[], fn: (id: string) => Promise<unknown>, successKey: string) => {
    setBulkBusy(true);
    let ok = 0;
    for (const id of ids) {
      try {
        await fn(id);
        ok++;
      } catch (err) {
        const name = jobs.find((j) => j.id === id)?.name || id;
        toast.error(`${name}: ${getErrorMessage(err)}`);
      }
    }
    if (ok > 0) toast.success(t(successKey, { count: ok }) as string);
    setSelected([]);
    setBulkBusy(false);
    void refresh();
  };

  const bulkDelete = async () => {
    const ok = await confirm({
      title: t('archive.confirmBulkDelete.title') as string,
      message: (
        <>
          {t('archive.confirmBulkDelete.message', { count: selected.length }) as string}
          <br />
          <br />
          {t('archive.confirmBulkDelete.warning') as string}
        </>
      ),
      severity: 'danger',
      confirmLabel: t('archive.confirmBulkDelete.delete') as string,
    });
    if (ok) await runBulk(selected.map(String), (id) => api.delete(`/api/jobs/${id}`), 'archive.bulkDeleteSuccess');
  };

  const deleteFinished = async () => {
    const ok = await confirm({
      title: t('dialogs.deleteFinished.title') as string,
      message: t('dialogs.deleteFinished.message') as string,
      severity: 'danger',
      confirmLabel: t('buttons.deleteFinished') as string,
      action: () => api.delete('/api/jobs/finished'),
    });
    if (ok) void refresh();
  };

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('page.title') as string}
        actions={
          <Box sx={{ display: 'flex', gap: 1.5, alignItems: 'center', flexWrap: 'wrap' }}>
            <Chip
              icon={<RefreshIcon />}
              label={polling ? (t('autoRefresh.on') as string) : (t('autoRefresh.off') as string)}
              color={polling ? 'success' : 'default'}
              variant="outlined"
              size="small"
              onClick={() => setPolling(!polling)}
            />
            <Button variant="outlined" size="small" startIcon={<RefreshIcon />} onClick={() => void refresh()} disabled={query.isFetching}>
              {t('buttons.refresh') as string}
            </Button>
            <Button variant="outlined" color="error" size="small" startIcon={<DeleteIcon />} onClick={deleteFinished} disabled={jobs.length === 0}>
              {t('buttons.deleteFinished') as string}
            </Button>
          </Box>
        }
      />

      <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap', mb: 2 }}>
        <Typography variant="body2" color="text.secondary" sx={{ mr: 1 }}>
          {t('filters.status') as string}:
        </Typography>
        <ToggleButtonGroup
          value={status ?? ''}
          exclusive
          size="small"
          onChange={(_e, v: string | null) => {
            if (v === null) return;
            setStatus(v === '' ? null : v);
            resetPage();
          }}
        >
          <ToggleButton value="" sx={{ gap: 1, px: 1.5 }}>
            {t('filters.all') as string}
            <FilterCount value={total} />
          </ToggleButton>
          {STATUS_FILTERS.map((s) => (
            <ToggleButton key={s} value={s} sx={{ gap: 1, px: 1.5 }}>
              {t(`status.${s}`) as string}
              <FilterCount value={counts[s] || 0} tone={BADGE_COLOR[s]} />
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
      </Box>

      <LoopbackSessionsPanel scope="visible" />

      <JobsDataTable
        jobs={jobs}
        loading={query.isLoading}
        fetching={query.isFetching && !query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
        onChanged={() => void refresh()}
        tableKey="jobs"
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount: query.data?.pagination.total ?? 0,
          pageSizeOptions: [25, 50, 100],
          onChange: (m) => {
            if (m.pageSize !== pageSize) {
              setPageSize(m.pageSize);
              setPage(0);
            } else setPage(m.page);
            setSelected([]);
          },
        }}
        selection={{ model: selected, onChange: setSelected }}
        toolbar={{
          search: {
            value: search,
            onChange: (v) => {
              setSearch(v);
              resetPage();
            },
            placeholder: t('filters.searchPlaceholder') as string,
          },
          filters: (
            <>
              <FormControl size="small" sx={{ minWidth: 140 }}>
                <InputLabel>{t('filters.priority') as string}</InputLabel>
                <Select
                  value={priority ?? ''}
                  label={t('filters.priority') as string}
                  onChange={(e) => {
                    setPriority(e.target.value === '' ? null : Number(e.target.value));
                    resetPage();
                  }}
                >
                  <MenuItem value="">{t('filters.all') as string}</MenuItem>
                  <MenuItem value={1}>{t('priority.low') as string}</MenuItem>
                  <MenuItem value={2}>{t('priority.medium') as string}</MenuItem>
                  <MenuItem value={3}>{t('priority.high') as string}</MenuItem>
                  <MenuItem value={4}>{t('priority.critical') as string}</MenuItem>
                  <MenuItem value={5}>{t('priority.maximum') as string}</MenuItem>
                </Select>
              </FormControl>
              <FormControlLabel
                control={
                  <Checkbox
                    size="small"
                    checked={showArchived}
                    onChange={(e) => {
                      setShowArchived(e.target.checked);
                      resetPage();
                    }}
                  />
                }
                label={t('archive.showArchived') as string}
              />
            </>
          ),
        }}
        bulkActions={() => (
          <>
            {selectedJobs.some((j) => !j.archived_at) && (
              <Button size="small" variant="outlined" startIcon={<ArchiveIcon />} disabled={bulkBusy}
                onClick={() => void runBulk(selectedJobs.filter((j) => !j.archived_at).map((j) => j.id), archiveJob, 'archive.bulkArchiveSuccess')}>
                {t('archive.archiveSelected') as string}
              </Button>
            )}
            {selectedJobs.some((j) => j.archived_at) && (
              <Button size="small" variant="outlined" startIcon={<UnarchiveIcon />} disabled={bulkBusy}
                onClick={() => void runBulk(selectedJobs.filter((j) => j.archived_at).map((j) => j.id), unarchiveJob, 'archive.bulkUnarchiveSuccess')}>
                {t('archive.unarchiveSelected') as string}
              </Button>
            )}
            <Button size="small" variant="outlined" color="error" startIcon={<DeleteIcon />} disabled={bulkBusy} onClick={bulkDelete}>
              {t('archive.deleteSelected') as string}
            </Button>
          </>
        )}
      />
    </Box>
  );
};

export default Jobs;
