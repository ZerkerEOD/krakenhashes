import { useCallback, useMemo, useState } from 'react';
import {
  Box,
  Button,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControl,
  FormControlLabel,
  InputLabel,
  LinearProgress,
  MenuItem,
  Select,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import DownloadIcon from '@mui/icons-material/Download';
import ArchiveIcon from '@mui/icons-material/Archive';
import UnarchiveIcon from '@mui/icons-material/Unarchive';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient, keepPreviousData } from '@tanstack/react-query';
import type { GridColDef, GridRowId, GridSortModel } from '@mui/x-data-grid';
import { AxiosError } from 'axios';
import { api, deleteHashlist, archiveHashlist, unarchiveHashlist } from '../../services/api';
import { useDeletionProgress } from '../../contexts/DeletionProgressContext';
import useDebounce from '../../hooks/useDebounce';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { ROUTES } from '../../constants/routes';
import { DataTable, EntityLink, StatusChip, useConfirm, useToast } from '../ui';
import { getErrorMessage } from '../../utils/errors';
import HashlistUploadForm from './HashlistUploadForm';
import { formatDateTime } from '../../utils/formatters';

type HashlistStatus = 'uploading' | 'processing' | 'ready' | 'error';
const FILTER_STATUSES: HashlistStatus[] = ['uploading', 'processing', 'ready', 'error'];
const SORTABLE = new Set(['name', 'clientName', 'status', 'createdAt']);

interface Hashlist {
  id: string;
  name: string;
  status: string;
  total_hashes: number;
  cracked_hashes: number;
  createdAt: string;
  clientName?: string;
  client_id?: string;
  exclude_from_potfile?: boolean;
  client_enable_potfile?: boolean;
  client_exclude_from_potfile?: boolean;
  client_exclude_from_client_potfile?: boolean;
  client_remove_from_global_on_delete?: boolean | null;
  client_remove_from_client_on_delete?: boolean | null;
  can_remove_from_global_potfile?: boolean;
  can_remove_from_client_potfile?: boolean;
  archived_at?: string | null;
  invalid_count?: number;
  total_input_lines?: number;
  validation_notice?: string | null;
}

interface ApiHashlistResponse {
  data: Hashlist[];
  total_count: number;
  limit: number;
  offset: number;
}

const getFilenameFromContentDisposition = (contentDisposition: string | undefined): string | null => {
  if (!contentDisposition) return null;
  const m = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
  if (m && m[3]) return m[3];
  const fallback = contentDisposition.match(/filename=([^;\n]*)/i);
  return fallback && fallback[1] ? fallback[1].trim() : null;
};

interface HashlistsDashboardProps {
  uploadDialogOpen: boolean;
  setUploadDialogOpen: (open: boolean) => void;
}

/**
 * Hashlist list: server-side sort/filter/pagination, bulk archive/delete,
 * per-row download/archive/delete, and live deletion progress in the status cell.
 */
export default function HashlistsDashboard({ uploadDialogOpen, setUploadDialogOpen }: HashlistsDashboardProps) {
  const { t } = useTranslation('hashlists');
  const toast = useToast();
  const confirm = useConfirm();
  const queryClient = useQueryClient();
  const { startTracking, isDeleting, getDeletion, startBulkDeletion, isBulkDeleting } = useDeletionProgress();

  const [sortModel, setSortModel] = useState<GridSortModel>([{ field: 'createdAt', sort: 'desc' }]);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const [nameFilter, setNameFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<HashlistStatus | ''>('');
  const [showArchived, setShowArchived] = useState(false);
  const [selected, setSelected] = useState<GridRowId[]>([]);
  const [downloadingId, setDownloadingId] = useState<string | null>(null);
  const [hashlistToDelete, setHashlistToDelete] = useState<Hashlist | null>(null);
  const [removeFromGlobalPotfile, setRemoveFromGlobalPotfile] = useState(false);
  const [removeFromClientPotfile, setRemoveFromClientPotfile] = useState(false);

  const debouncedName = useDebounce(nameFilter, 400);
  const sort = sortModel[0];
  const sortBy = sort && SORTABLE.has(sort.field) ? sort.field : 'createdAt';
  const order = sort?.sort ?? 'desc';

  const params = useMemo(() => {
    const p: Record<string, string | number> = { sort_by: sortBy, order, limit: pageSize, offset: page * pageSize };
    if (debouncedName) p.name_like = debouncedName;
    if (statusFilter) p.status = statusFilter;
    if (showArchived) p.include_archived = 'true';
    return p;
  }, [sortBy, order, pageSize, page, debouncedName, statusFilter, showArchived]);

  const query = useLiveQuery<ApiHashlistResponse, AxiosError>(
    {
      queryKey: qk.hashlists.list(params),
      queryFn: async () => (await api.get<ApiHashlistResponse>('/api/hashlists', { params })).data,
      placeholderData: keepPreviousData,
    },
    { tier: 'list', when: (d) => Boolean(d?.data.some((h) => h.status === 'uploading' || h.status === 'processing' || h.status === 'deleting')) }
  );
  const hashlists = query.data?.data ?? [];
  const rowCount = query.data?.total_count ?? 0;
  const invalidate = () => queryClient.invalidateQueries({ queryKey: qk.hashlists.all });

  // ---- mutations ----------------------------------------------------------
  const deleteMutation = useMutation({
    mutationFn: (vars: { hashlistId: string; removeFromGlobalPotfile?: boolean; removeFromClientPotfile?: boolean }) =>
      deleteHashlist(vars.hashlistId, vars.removeFromGlobalPotfile, vars.removeFromClientPotfile),
    onSuccess: (result) => {
      if (result.async && hashlistToDelete) startTracking(hashlistToDelete.id, hashlistToDelete.name);
      else {
        toast.success(t('notifications.deleteSuccess') as string);
        void invalidate();
      }
      setHashlistToDelete(null);
    },
    onError: (error) => {
      toast.error(getErrorMessage(error) || 'Failed to delete hashlist');
      setHashlistToDelete(null);
    },
  });

  const archiveMutation = useMutation({
    mutationFn: async ({ ids, archive }: { ids: string[]; archive: boolean }) => {
      await Promise.all(ids.map((id) => (archive ? archiveHashlist(id) : unarchiveHashlist(id))));
    },
    onSuccess: (_r, vars) => {
      const key = vars.ids.length > 1
        ? (vars.archive ? 'notifications.bulkArchiveSuccess' : 'notifications.bulkUnarchiveSuccess')
        : (vars.archive ? 'notifications.archiveSuccess' : 'notifications.unarchiveSuccess');
      toast.success(t(key, { count: vars.ids.length }) as string);
      setSelected([]);
      void invalidate();
    },
    onError: (error) => toast.error(getErrorMessage(error) || 'Operation failed'),
  });

  // ---- handlers -----------------------------------------------------------
  const handleDownload = useCallback(
    async (hashlist: Hashlist) => {
      if (downloadingId === hashlist.id) return;
      setDownloadingId(hashlist.id);
      try {
        const response = await api.get(`/api/hashlists/${hashlist.id}/download`, { responseType: 'blob' });
        if (response.data.type === 'application/json') {
          const text = await (response.data as Blob).text();
          let message = 'Failed to download file';
          try {
            message = JSON.parse(text).error || message;
          } catch {
            /* keep default */
          }
          toast.error(message);
          return;
        }
        const filename = getFilenameFromContentDisposition(response.headers['content-disposition']) || `hashlist-${hashlist.id}.hash`;
        const url = window.URL.createObjectURL(new Blob([response.data]));
        const link = document.createElement('a');
        link.href = url;
        link.setAttribute('download', filename);
        document.body.appendChild(link);
        link.click();
        link.parentNode?.removeChild(link);
        window.URL.revokeObjectURL(url);
        toast.success(t('notifications.downloaded', { filename }) as string);
      } catch (error) {
        let message = 'Failed to download hashlist';
        if (error instanceof AxiosError && error.response?.data instanceof Blob) {
          try {
            message = JSON.parse(await error.response.data.text()).error || `Server error (${error.response.status})`;
          } catch {
            message = `Server error (${error.response.status})`;
          }
        } else {
          message = getErrorMessage(error) || message;
        }
        toast.error(message);
      } finally {
        setDownloadingId(null);
      }
    },
    [downloadingId, t, toast]
  );

  const openDelete = (hashlist: Hashlist) => {
    setRemoveFromGlobalPotfile(false);
    setRemoveFromClientPotfile(false);
    setHashlistToDelete(hashlist);
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
      confirmLabel: t('confirmDelete.delete') as string,
    });
    if (!ok) return;
    const items = selected.map((id) => ({ id: String(id), name: hashlists.find((h) => h.id === id)?.name || String(id) }));
    startBulkDeletion(items, (hashlistId) => deleteHashlist(hashlistId));
    setSelected([]);
  };

  const selectedRows = selected.map((id) => hashlists.find((h) => h.id === id)).filter(Boolean) as Hashlist[];

  // ---- columns ------------------------------------------------------------
  const columns = useMemo<GridColDef<Hashlist>[]>(
    () => [
      {
        field: 'name',
        headerName: t('columns.name') as string,
        flex: 1.6,
        minWidth: 200,
        renderCell: (p) => <EntityLink type="hashlist" id={p.row.id} label={p.row.name} />,
      },
      {
        field: 'clientName',
        headerName: t('columns.client') as string,
        flex: 1,
        minWidth: 140,
        renderCell: (p) =>
          p.row.client_id && p.row.clientName ? <EntityLink type="client" id={p.row.client_id} label={p.row.clientName} /> : p.row.clientName || '-',
      },
      {
        field: 'status',
        headerName: t('columns.status') as string,
        flex: 1.2,
        minWidth: 200,
        renderCell: (p) => {
          const h = p.row;
          if (isDeleting(h.id)) {
            const entry = getDeletion(h.id);
            const pct = entry?.progress && entry.progress.total > 0 ? Math.round((entry.progress.checked / entry.progress.total) * 100) : undefined;
            const phase = (() => {
              switch (entry?.status) {
                case 'deleting_hashes': return 'deletionProgress.phases.removingHashes';
                case 'clearing_references': return 'deletionProgress.phases.clearingReferences';
                case 'cleaning_orphans': return 'deletionProgress.phases.cleaningOrphans';
                case 'finalizing': return 'deletionProgress.phases.finalizing';
                default: return 'deletionProgress.phases.preparing';
              }
            })();
            return (
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 140, py: 0.5 }}>
                <Chip label={t('deletionProgress.title') as string} color="warning" size="small" />
                <LinearProgress variant={pct === undefined ? 'indeterminate' : 'determinate'} value={pct} sx={{ height: 4, borderRadius: 2 }} />
                <Typography variant="caption" color="text.secondary">{t(phase) as string}</Typography>
              </Box>
            );
          }
          return (
            <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center', flexWrap: 'wrap' }}>
              <StatusChip entity="hashlist" status={h.status} />
              {h.archived_at && <Chip label={t('archive.archived') as string} size="small" variant="outlined" color="warning" />}
              {h.validation_notice && (
                <Tooltip title={h.validation_notice}>
                  <Chip label="unvalidated type" size="small" variant="outlined" color="info" />
                </Tooltip>
              )}
              {(h.invalid_count ?? 0) > 0 && h.status !== 'awaiting_validation_decision' && (
                <Tooltip title={`${h.invalid_count} of ${h.total_input_lines ?? '?'} lines were malformed and skipped`}>
                  <Chip label={`${h.invalid_count} skipped`} size="small" variant="outlined" color="warning" />
                </Tooltip>
              )}
            </Box>
          );
        },
      },
      {
        field: 'total_hashes',
        headerName: t('columns.totalHashes') as string,
        width: 120,
        type: 'number',
        sortable: false,
        valueFormatter: (v: number) => (v ?? 0).toLocaleString(),
      },
      {
        field: 'cracked_hashes',
        headerName: t('columns.crackedHashes') as string,
        width: 110,
        type: 'number',
        sortable: false,
        renderCell: (p) => <EntityLink type="pot_hashlist" id={p.row.id} label={(p.row.cracked_hashes ?? 0).toLocaleString()} />,
      },
      {
        field: 'progress',
        headerName: t('columns.progress') as string,
        width: 160,
        sortable: false,
        renderCell: (p) => {
          const pct = p.row.total_hashes > 0 ? Math.round((p.row.cracked_hashes / p.row.total_hashes) * 100) : 0;
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', width: '100%', gap: 1 }}>
              <LinearProgress variant="determinate" value={pct} color="success" sx={{ flexGrow: 1, height: 6, borderRadius: 3 }} />
              <Typography variant="body2" color="text.secondary" sx={{ minWidth: 36, textAlign: 'right' }}>{pct}%</Typography>
            </Box>
          );
        },
      },
      {
        field: 'createdAt',
        headerName: t('columns.createdAt') as string,
        width: 160,
        valueFormatter: (v: string) => formatDateTime(v),
      },
    ],
    [t, isDeleting, getDeletion]
  );

  return (
    <>
      <DataTable<Hashlist>
        rows={hashlists}
        columns={columns}
        loading={query.isLoading}
        fetching={query.isFetching && !query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount,
          onChange: (m) => {
            setPage(m.page);
            setPageSize(m.pageSize);
          },
        }}
        sorting={{ mode: 'server', model: sortModel, onChange: (m) => { setSortModel(m); setPage(0); } }}
        toolbar={{
          search: { value: nameFilter, onChange: (v) => { setNameFilter(v); setPage(0); }, placeholder: t('filters.filterByName') as string },
          filters: (
            <>
              <FormControl size="small" sx={{ minWidth: 160 }}>
                <InputLabel>{t('filters.status') as string}</InputLabel>
                <Select
                  value={statusFilter}
                  label={t('filters.status') as string}
                  onChange={(e) => { setStatusFilter(e.target.value as HashlistStatus | ''); setPage(0); }}
                >
                  <MenuItem value=""><em>{t('filters.all') as string}</em></MenuItem>
                  {FILTER_STATUSES.map((s) => (
                    <MenuItem key={s} value={s}>{t(`status.${s}`) as string}</MenuItem>
                  ))}
                </Select>
              </FormControl>
              <FormControlLabel
                control={<Checkbox size="small" checked={showArchived} onChange={(e) => { setShowArchived(e.target.checked); setSelected([]); setPage(0); }} />}
                label={t('archive.showArchived') as string}
              />
            </>
          ),
        }}
        selection={{ model: selected, onChange: setSelected, isRowSelectable: (row: Hashlist) => !isDeleting(row.id) }}
        bulkActions={() => (
          <>
            {selectedRows.some((h) => !h.archived_at) && (
              <Button size="small" variant="outlined" startIcon={<ArchiveIcon />} disabled={archiveMutation.isPending}
                onClick={() => archiveMutation.mutate({ ids: selectedRows.filter((h) => !h.archived_at).map((h) => h.id), archive: true })}>
                {t('archive.archiveSelected') as string}
              </Button>
            )}
            {selectedRows.some((h) => h.archived_at) && (
              <Button size="small" variant="outlined" startIcon={<UnarchiveIcon />} disabled={archiveMutation.isPending}
                onClick={() => archiveMutation.mutate({ ids: selectedRows.filter((h) => h.archived_at).map((h) => h.id), archive: false })}>
                {t('archive.unarchiveSelected') as string}
              </Button>
            )}
            <Button size="small" variant="outlined" color="error" startIcon={<DeleteIcon />} disabled={isBulkDeleting} onClick={bulkDelete}>
              {t('archive.deleteSelected') as string}
            </Button>
          </>
        )}
        rowActions={(row) => [
          { key: 'download', label: t('actions.download') as string, icon: <DownloadIcon fontSize="small" />, disabled: downloadingId === row.id, onClick: (r) => void handleDownload(r) },
          {
            key: 'archive',
            label: t(row.archived_at ? 'archive.unarchive' : 'archive.archive') as string,
            icon: row.archived_at ? <UnarchiveIcon fontSize="small" /> : <ArchiveIcon fontSize="small" />,
            disabled: archiveMutation.isPending,
            onClick: (r) => archiveMutation.mutate({ ids: [r.id], archive: !r.archived_at }),
          },
          { key: 'delete', label: t('actions.delete') as string, icon: <DeleteIcon fontSize="small" />, danger: true, placement: 'menu', disabled: deleteMutation.isPending || isDeleting(row.id), onClick: openDelete },
        ]}
        rowLinkTo={(row) => ROUTES.hashlist(row.id)}
        emptyState={{ title: t('table.noHashlists') as string }}
        tableKey="hashlists"
        rowClassName={(row) => (row.archived_at ? 'kh-row-muted' : undefined)}
      />

      <Dialog open={Boolean(hashlistToDelete)} onClose={() => setHashlistToDelete(null)} maxWidth="xs" fullWidth>
        <DialogTitle>{t('confirmDelete.title') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText>{t('confirmDelete.message', { name: hashlistToDelete?.name || '' }) as string}</DialogContentText>
          {hashlistToDelete?.can_remove_from_global_potfile && hashlistToDelete?.client_remove_from_global_on_delete === null && (
            <FormControlLabel
              control={<Checkbox checked={removeFromGlobalPotfile} onChange={(e) => setRemoveFromGlobalPotfile(e.target.checked)} />}
              label={t('confirmDelete.removeFromGlobalPotfile') as string}
              sx={{ mt: 2, display: 'block' }}
            />
          )}
          {hashlistToDelete?.can_remove_from_client_potfile && hashlistToDelete?.client_remove_from_client_on_delete === null && (
            <FormControlLabel
              control={<Checkbox checked={removeFromClientPotfile} onChange={(e) => setRemoveFromClientPotfile(e.target.checked)} />}
              label={t('confirmDelete.removeFromClientPotfile') as string}
              sx={{ mt: 1, display: 'block' }}
            />
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setHashlistToDelete(null)}>{t('confirmDelete.cancel') as string}</Button>
          <Button
            color="error"
            variant="contained"
            autoFocus
            disabled={deleteMutation.isPending}
            onClick={() => hashlistToDelete && deleteMutation.mutate({ hashlistId: hashlistToDelete.id, removeFromGlobalPotfile, removeFromClientPotfile })}
          >
            {deleteMutation.isPending ? (t('confirmDelete.deleting') as string) : (t('confirmDelete.delete') as string)}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={uploadDialogOpen} onClose={() => setUploadDialogOpen(false)} maxWidth="md" fullWidth>
        <DialogTitle>{t('upload.title') as string}</DialogTitle>
        <DialogContent>
          <HashlistUploadForm
            onSuccess={() => {
              setUploadDialogOpen(false);
              void invalidate();
              toast.success(t('notifications.uploadSuccess') as string);
            }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadDialogOpen(false)}>{t('upload.cancel') as string}</Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
