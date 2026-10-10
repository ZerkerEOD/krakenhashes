import { useState, useEffect } from 'react';
import {
  Box,
  Paper,
  Typography,
  LinearProgress,
  Button,
  Tooltip,
  IconButton,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  CircularProgress,
  FormControlLabel,
  Checkbox,
  Alert
} from '@mui/material';
import {
  Download as DownloadIcon,
  Delete as DeleteIcon,
  ArrowBack as ArrowBackIcon,
  PlayArrow as PlayArrowIcon,
  Edit as EditIcon,
  Visibility as VisibilityIcon
} from '@mui/icons-material';
import { useParams, useNavigate } from 'react-router-dom';
import { useTranslation, Trans } from 'react-i18next';
import { useQuery, useMutation, useQueryClient, keepPreviousData } from '@tanstack/react-query';
import { api, deleteHashlist, getProcessingProgress, ProcessingProgressResponse } from '../../services/api';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { EntityLink, SectionCard, StatusChip, useToast } from '../ui';
import JobsDataTable from '../jobs/JobsDataTable';
import type { JobSummary } from '../../types/jobs';
import { useDeletionProgress } from '../../contexts/DeletionProgressContext';
import CreateJobDialog from './CreateJobDialog';
import HashlistHashesTable from './HashlistHashesTable';
import ClientAutocomplete from './ClientAutocomplete';
import AssociationWordlistManager from './AssociationWordlistManager';
import ValidationPreviewDialog, { ValidationInvalidEntry } from './ValidationPreviewDialog';

interface HashlistJobsResponse {
  jobs: JobSummary[];
  pagination: { total: number };
}

/** Result of one processing-progress poll; `gone` means the backend no longer tracks it (404 = finished). */
type ProcessingPoll = { progress: ProcessingProgressResponse | null; gone: boolean };

// Helper function to format ETA in human-readable format
const formatETA = (seconds: number): string => {
  if (!isFinite(seconds) || seconds < 0) return '--';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.round((seconds % 3600) / 60);
  return `${hours}h ${mins}m`;
};

export default function HashlistDetailView() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { t } = useTranslation('hashlists');
  const [createJobDialogOpen, setCreateJobDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [removeFromGlobalPotfile, setRemoveFromGlobalPotfile] = useState(false);
  const [removeFromClientPotfile, setRemoveFromClientPotfile] = useState(false);
  const [editClientDialogOpen, setEditClientDialogOpen] = useState(false);
  const [selectedClient, setSelectedClient] = useState<string | null>(null);
  const [downloadingHashlist, setDownloadingHashlist] = useState(false);
  const [jobsPage, setJobsPage] = useState(0);
  const [jobsPageSize, setJobsPageSize] = useState(25);

  // Validation preview state (GitHub issue #38). Re-opens the dialog from the
  // detail view when the hashlist is stuck in awaiting_validation_decision —
  // e.g. user closed the tab after upload without choosing proceed/cancel.
  const [resumeValidationOpen, setResumeValidationOpen] = useState(false);
  const [resumeSample, setResumeSample] = useState<ValidationInvalidEntry[]>([]);
  const [resumeLoading, setResumeLoading] = useState(false);
  const queryClient = useQueryClient();
  const toast = useToast();
  const { startTracking, isDeleting, getDeletion } = useDeletionProgress();

  // Redirect to hashlists page when async deletion completes
  const deletionEntry = id ? getDeletion(id) : undefined;
  useEffect(() => {
    if (deletionEntry?.status === 'completed') {
      navigate('/hashlists');
    }
  }, [deletionEntry?.status, navigate]);

  const { data: hashlist, isLoading, refetch } = useQuery({
    queryKey: ['hashlist', id],
    queryFn: () => api.get(`/api/hashlists/${id}`).then(res => res.data)
  });

  // Poll processing progress (live tier) while the hashlist is processing.
  const isProcessing = Boolean(hashlist && hashlist.status === 'processing' && id);
  const processingQuery = useLiveQuery<ProcessingPoll>(
    {
      queryKey: ['hashlist', id, 'processing-progress'],
      queryFn: async () => {
        try {
          return { progress: await getProcessingProgress(id!), gone: false };
        } catch (error: any) {
          // 404 means processing already completed
          if (error.response?.status === 404) return { progress: null, gone: true };
          throw error;
        }
      },
      enabled: isProcessing,
    },
    {
      tier: 'live',
      enabled: isProcessing,
      when: (d) => !d || (!d.gone && d.progress?.status !== 'completed' && d.progress?.status !== 'failed'),
    }
  );
  const processingProgress = isProcessing ? processingQuery.data?.progress ?? null : null;
  const processingDone =
    processingQuery.data?.gone || processingQuery.data?.progress?.status === 'completed' || processingQuery.data?.progress?.status === 'failed';

  // Once processing finishes, refetch the hashlist to pick up its new status.
  useEffect(() => {
    if (isProcessing && processingDone) refetch();
  }, [isProcessing, processingDone, processingQuery.dataUpdatedAt, refetch]);

  // Jobs that ran (or are running) against this hashlist, archived included.
  const jobsQuery = useLiveQuery<HashlistJobsResponse>(
    {
      queryKey: ['jobs', 'list', { hashlist_id: id, include_archived: true, page: jobsPage + 1, page_size: jobsPageSize }],
      queryFn: async () =>
        (
          await api.get<HashlistJobsResponse>('/api/jobs', {
            params: { hashlist_id: id, include_archived: true, page: jobsPage + 1, page_size: jobsPageSize },
          })
        ).data,
      enabled: Boolean(id),
      placeholderData: keepPreviousData,
    },
    { tier: 'list', enabled: Boolean(id) }
  );

  // Delete Mutation - handles both sync and async deletion
  const deleteMutation = useMutation({
    mutationFn: async ({ hashlistId, removeFromGlobalPotfile, removeFromClientPotfile }: { hashlistId: string; removeFromGlobalPotfile?: boolean; removeFromClientPotfile?: boolean }) => {
      return deleteHashlist(hashlistId, removeFromGlobalPotfile, removeFromClientPotfile);
    },
    onSuccess: (result) => {
      if (result.async) {
        // Async deletion — track via global context (non-blocking)
        startTracking(id!, hashlist?.name || `Hashlist ${id}`);
        setDeleteDialogOpen(false);
        // User can stay on page and see the inline banner, or navigate away
      } else {
        // Sync deletion completed
        toast.success(t('notifications.deleteSuccess') as string);
        queryClient.invalidateQueries({ queryKey: ['hashlists'] });
        navigate('/hashlists');
      }
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || (t('detail.errors.deleteFailed') as string);
      toast.error(errorMsg);
      setDeleteDialogOpen(false);
    },
  });

  const handleDeleteClick = () => {
    // Reset checkbox states - they will be shown only when conditions allow
    setRemoveFromGlobalPotfile(false);
    setRemoveFromClientPotfile(false);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = () => {
    if (id) {
      deleteMutation.mutate({
        hashlistId: id,
        removeFromGlobalPotfile,
        removeFromClientPotfile
      });
    }
  };

  const handleDeleteCancel = () => {
    setDeleteDialogOpen(false);
    setRemoveFromGlobalPotfile(false);
    setRemoveFromClientPotfile(false);
  };

  // Update Client Mutation
  const updateClientMutation = useMutation({
    mutationFn: async (clientId: string | null) => {
      return api.patch(`/api/hashlists/${id}/client`, { client_id: clientId });
    },
    onSuccess: () => {
      toast.success(t('detail.notifications.clientUpdated') as string);
      queryClient.invalidateQueries({ queryKey: ['hashlist', id] });
      queryClient.invalidateQueries({ queryKey: ['hashlists'] });
      setEditClientDialogOpen(false);
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || (t('detail.errors.clientUpdateFailed') as string);
      toast.error(errorMsg);
    },
  });

  const handleEditClientClick = () => {
    // Get client name from hashlist
    const clientName = hashlist?.client_name;
    setSelectedClient(clientName || null);
    setEditClientDialogOpen(true);
  };

  const handleEditClientConfirm = async () => {
    // Look up client by name if selectedClient is a string
    if (selectedClient) {
      try {
        const response = await api.get(`/api/clients/search?q=${selectedClient}`);
        const clients = Array.isArray(response.data) ? response.data : [];
        const matchingClient = clients.find((c: any) => c.name === selectedClient);

        if (matchingClient) {
          updateClientMutation.mutate(matchingClient.id);
        } else {
          toast.error(t('detail.errors.clientNotFound') as string);
        }
      } catch (error) {
        console.error('Failed to lookup client:', error);
        toast.error(t('detail.errors.clientLookupFailed') as string);
      }
    } else {
      // Clear the client (set to null)
      updateClientMutation.mutate(null);
    }
  };

  const handleEditClientCancel = () => {
    setEditClientDialogOpen(false);
  };

  const handleDownloadClick = async () => {
    if (!id || downloadingHashlist) return;
    setDownloadingHashlist(true);

    try {
      const response = await api.get(`/api/hashlists/${id}/download`, {
        responseType: 'blob',
      });

      // Check if the response looks like an error
      if (response.data.type === 'application/json') {
        const reader = new FileReader();
        reader.onload = () => {
          try {
            const errorJson = JSON.parse(reader.result as string);
            toast.error(errorJson.error || (t('detail.errors.downloadFailed') as string));
          } catch {
            toast.error(t('detail.errors.downloadFailed') as string);
          }
        };
        reader.readAsText(response.data);
        setDownloadingHashlist(false);
        return;
      }

      const blob = new Blob([response.data]);
      const contentDisposition = response.headers['content-disposition'];

      // Extract filename from Content-Disposition header
      let filename = `${hashlist?.name || 'hashlist'}.txt`;
      if (contentDisposition) {
        const filenameMatch = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
        if (filenameMatch && filenameMatch[3]) {
          filename = filenameMatch[3];
        }
      }

      // Create download link
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.setAttribute('download', filename);
      document.body.appendChild(link);
      link.click();

      // Cleanup
      link.parentNode?.removeChild(link);
      window.URL.revokeObjectURL(url);
      toast.success(t('notifications.downloaded', { filename }) as string);

    } catch (error: any) {
      console.error("Error downloading hashlist:", error);
      let errorMsg = t('detail.errors.downloadHashlistFailed') as string;
      if (error.response?.data instanceof Blob && error.response.data.type === 'application/json') {
        try {
          const errorJsonText = await error.response.data.text();
          const errorJson = JSON.parse(errorJsonText);
          errorMsg = errorJson.error || (t('detail.errors.serverError', { status: error.response.status }) as string);
        } catch {
          errorMsg = t('detail.errors.serverError', { status: error.response.status }) as string;
        }
      } else if (error.response?.data?.error) {
        errorMsg = error.response.data.error;
      } else if (error.message) {
        errorMsg = error.message;
      }
      toast.error(errorMsg);
    } finally {
      setDownloadingHashlist(false);
    }
  };

  if (isLoading) return <LinearProgress />;

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ mb: 2 }}>
        <Button
          startIcon={<ArrowBackIcon />}
          onClick={() => navigate('/hashlists')}
          size="small"
        >
          {t('detail.backToHashlists') as string}
        </Button>
      </Box>

      {/* Non-blocking deletion progress banner */}
      {id && isDeleting(id) && (() => {
        const entry = getDeletion(id);
        const phaseLabel = (() => {
          switch (entry?.status) {
            case 'deleting_hashes': return t('deletionProgress.phases.removingHashes') as string;
            case 'clearing_references': return t('deletionProgress.phases.clearingReferences') as string;
            case 'cleaning_orphans': return t('deletionProgress.phases.cleaningOrphans') as string;
            case 'finalizing': return t('deletionProgress.phases.finalizing') as string;
            default: return t('deletionProgress.phases.preparing') as string;
          }
        })();
        const progress = entry?.progress;
        const percent = progress && progress.total > 0 ? Math.round((progress.checked / progress.total) * 100) : 0;
        return (
          <Alert severity="warning" sx={{ mb: 2 }}>
            <Typography variant="subtitle2" gutterBottom>
              {t('detail.deletionBanner.title', { phase: phaseLabel }) as string}
            </Typography>
            <LinearProgress
              variant={progress ? 'determinate' : 'indeterminate'}
              value={percent}
              sx={{ height: 6, borderRadius: 3 }}
            />
            {progress && (
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                {t('detail.deletionBanner.progress', { checked: progress.checked.toLocaleString(), total: progress.total.toLocaleString(), percent }) as string}
              </Typography>
            )}
          </Alert>
        );
      })()}

      {/* No-validator notice (GitHub issue #38). Persists on the hashlist
          row so users see it on revisit, not just at upload time. */}
      {(hashlist as any).validation_notice && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {(hashlist as any).validation_notice}
        </Alert>
      )}

      {/* Awaiting validation decision (GitHub issue #38) — the user closed
          the upload dialog without proceeding or cancelling. Surface a clear
          banner that re-opens the dialog. */}
      {hashlist.status === 'awaiting_validation_decision' && (
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={
            <Button
              color="inherit"
              size="small"
              disabled={resumeLoading}
              onClick={async () => {
                if (!id) return;
                setResumeLoading(true);
                try {
                  const resp = await api.get(`/api/hashlists/${id}/invalid-hashes`, {
                    params: { page: 1, page_size: 20 },
                  });
                  setResumeSample(resp.data?.items ?? []);
                  setResumeValidationOpen(true);
                } catch (e: any) {
                  toast.error(e?.response?.data?.error || (t('detail.errors.loadInvalidHashesFailed') as string));
                } finally {
                  setResumeLoading(false);
                }
              }}
            >
              {resumeLoading ? (t('detail.validationBanner.loading') as string) : (t('detail.validationBanner.resumeReview') as string)}
            </Button>
          }
        >
          <Typography variant="subtitle2" gutterBottom>
            {t('detail.validationBanner.title') as string}
          </Typography>
          <Typography variant="body2">
            <Trans
              t={t}
              i18nKey="detail.validationBanner.message"
              values={{
                invalidCount: (hashlist as any).invalid_count?.toLocaleString() ?? 0,
                totalLines: (hashlist as any).total_input_lines?.toLocaleString() ?? 0,
              }}
              components={{ strong: <strong /> }}
            />
          </Typography>
        </Alert>
      )}

      <Paper sx={{ p: 3, mb: 3 }}>
        <Box display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h5">{hashlist.name}</Typography>
          <Box display="flex" gap={1}>
            <Button
              variant="contained"
              startIcon={<PlayArrowIcon />}
              onClick={() => setCreateJobDialogOpen(true)}
              disabled={hashlist.status !== 'ready'}
            >
              {t('actions.createJob') as string}
            </Button>
            <Button
              variant="outlined"
              startIcon={<VisibilityIcon />}
              onClick={() => navigate(`/pot/hashlist/${id}`)}
              disabled={!hashlist.cracked_hashes || hashlist.cracked_hashes === 0}
            >
              {t('detail.viewCrackedHashes') as string}
            </Button>
            <Tooltip title={t('actions.download') as string}>
              <span>
                <IconButton
                  onClick={handleDownloadClick}
                  disabled={downloadingHashlist}
                >
                  <DownloadIcon />
                </IconButton>
              </span>
            </Tooltip>
            <Tooltip title={t('actions.delete') as string}>
              <IconButton color="error" onClick={handleDeleteClick}>
                <DeleteIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </Box>

        <Typography variant="subtitle1" color="text.secondary" sx={{ mt: 1 }}>
          {hashlist.description || (t('detail.noDescription') as string)}
        </Typography>

        <Box display="flex" gap={2} sx={{ mt: 3 }} flexWrap="wrap" alignItems="center">
          <Box display="flex" alignItems="center" gap={1}>
            <Typography component="span">{t('detail.statusLabel') as string}</Typography>
            <StatusChip entity="hashlist" status={hashlist.status} />
            {/* Inline processing progress */}
            {hashlist.status === 'processing' && processingProgress && processingProgress.processed_lines !== undefined && (
              <Box display="flex" alignItems="center" gap={1} sx={{ ml: 1 }}>
                <CircularProgress size={16} />
                <Typography variant="body2" color="text.secondary">
                  {t('detail.processingProgress.lines', {
                    processed: processingProgress.processed_lines.toLocaleString(),
                    total: processingProgress.total_lines.toLocaleString(),
                  }) as string}
                  {processingProgress.lines_per_second > 0 && (
                    <> {t('detail.processingProgress.rate', { rate: Math.round(processingProgress.lines_per_second).toLocaleString() }) as string}</>
                  )}
                  {processingProgress.lines_per_second > 0 && processingProgress.total_lines > processingProgress.processed_lines && (
                    <> {t('detail.processingProgress.eta', { eta: formatETA((processingProgress.total_lines - processingProgress.processed_lines) / processingProgress.lines_per_second) }) as string}</>
                  )}
                </Typography>
              </Box>
            )}
          </Box>
          <Typography>
            {t('detail.hashType', { name: hashlist.hashTypeName }) as string}
          </Typography>
          <Typography>
            {t('detail.clientLabel') as string}{' '}
            {hashlist.client_id && hashlist.client_name ? (
              <EntityLink type="client" id={hashlist.client_id} label={hashlist.client_name} />
            ) : (
              hashlist.client_name || (t('detail.noClient') as string)
            )}
            <Tooltip title={t('detail.editClient') as string}>
              <IconButton size="small" onClick={handleEditClientClick} sx={{ ml: 1 }}>
                <EditIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Typography>
          <Typography>
            {t('detail.created', { date: new Date(hashlist.createdAt).toLocaleString() }) as string}
          </Typography>
        </Box>

        <Box sx={{ mt: 3 }}>
          <Typography variant="subtitle2">
            {t('detail.crackProgress', { cracked: hashlist.cracked_hashes || 0, total: hashlist.total_hashes || 0 }) as string}
          </Typography>
          <Box display="flex" alignItems="center" gap={2}>
            <Box width="100%">
              <LinearProgress
                variant="determinate"
                value={hashlist.total_hashes > 0
                  ? ((hashlist.cracked_hashes || 0) / hashlist.total_hashes) * 100
                  : 0
                }
              />
            </Box>
            <Typography>
              {hashlist.total_hashes > 0
                ? Math.round(((hashlist.cracked_hashes || 0) / hashlist.total_hashes) * 100)
                : 0
              }%
            </Typography>
          </Box>
        </Box>
      </Paper>

      {hashlist && (
        <AssociationWordlistManager
          hashlistId={parseInt(id!)}
          totalHashes={hashlist.total_hashes || 0}
          hasMixedWorkFactors={hashlist.has_mixed_work_factors || false}
          clientId={hashlist.client_id}
        />
      )}

      {hashlist && (
        <HashlistHashesTable
          hashlistId={id!}
          hashlistName={hashlist.name}
          totalHashes={hashlist.total_hashes || 0}
          crackedHashes={hashlist.cracked_hashes || 0}
        />
      )}

      <SectionCard title={t('detail.jobsRun.title') as string} subtitle={t('detail.jobsRun.subtitle') as string} sx={{ mt: 3 }}>
        <JobsDataTable
          jobs={jobsQuery.data?.jobs ?? []}
          loading={jobsQuery.isLoading}
          fetching={jobsQuery.isFetching && !jobsQuery.isLoading}
          error={jobsQuery.error}
          onRetry={() => void jobsQuery.refetch()}
          onChanged={() => void jobsQuery.refetch()}
          pagination={{
            mode: 'server',
            page: jobsPage,
            pageSize: jobsPageSize,
            rowCount: jobsQuery.data?.pagination?.total ?? 0,
            pageSizeOptions: [10, 25, 50, 100],
            onChange: (m) => {
              setJobsPage(m.pageSize !== jobsPageSize ? 0 : m.page);
              setJobsPageSize(m.pageSize);
            },
          }}
          emptyState={{ title: t('detail.jobsRun.empty') as string }}
          tableKey="hashlist-jobs"
        />
      </SectionCard>

      {hashlist && (
        <CreateJobDialog
          open={createJobDialogOpen}
          onClose={() => setCreateJobDialogOpen(false)}
          hashlistId={parseInt(id!)}
          hashlistName={hashlist.name}
          hashTypeId={hashlist.hashTypeID || hashlist.hash_type_id}
          hasMixedWorkFactors={hashlist.has_mixed_work_factors || false}
          totalHashes={hashlist.total_hashes || 0}
        />
      )}

      <Dialog
        open={deleteDialogOpen}
        onClose={handleDeleteCancel}
        aria-labelledby="alert-dialog-title"
        aria-describedby="alert-dialog-description"
      >
        <DialogTitle id="alert-dialog-title">
          {t('confirmDelete.title') as string}
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="alert-dialog-description">
            {t('confirmDelete.message', { name: hashlist?.name || '' }) as string}
          </DialogContentText>

          {/* Show global potfile removal option - only if eligible AND client allows override */}
          {hashlist?.can_remove_from_global_potfile &&
           hashlist?.client_remove_from_global_on_delete === null && (
            <FormControlLabel
              control={
                <Checkbox
                  checked={removeFromGlobalPotfile}
                  onChange={(e) => setRemoveFromGlobalPotfile(e.target.checked)}
                />
              }
              label={t('confirmDelete.removeFromGlobalPotfile') as string}
              sx={{ mt: 2, display: 'block' }}
            />
          )}

          {/* Show client potfile removal option - only if eligible AND client allows override */}
          {hashlist?.can_remove_from_client_potfile &&
           hashlist?.client_remove_from_client_on_delete === null && (
            <FormControlLabel
              control={
                <Checkbox
                  checked={removeFromClientPotfile}
                  onChange={(e) => setRemoveFromClientPotfile(e.target.checked)}
                />
              }
              label={t('confirmDelete.removeFromClientPotfile') as string}
              sx={{ mt: 1, display: 'block' }}
            />
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={handleDeleteCancel} color="primary">
            {t('confirmDelete.cancel') as string}
          </Button>
          <Button onClick={handleDeleteConfirm} color="error" autoFocus disabled={deleteMutation.isPending}>
            {deleteMutation.isPending ? (t('confirmDelete.deleting') as string) : (t('confirmDelete.delete') as string)}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={editClientDialogOpen}
        onClose={handleEditClientCancel}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>
          {t('detail.editClientDialog.title') as string}
        </DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ mb: 2 }}>
            {t('detail.editClientDialog.description') as string}
          </DialogContentText>
          <ClientAutocomplete
            value={selectedClient}
            onChange={(value) => setSelectedClient(value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleEditClientCancel} color="primary">
            {t('confirmDelete.cancel') as string}
          </Button>
          <Button
            onClick={handleEditClientConfirm}
            color="primary"
            variant="contained"
            disabled={updateClientMutation.isPending}
          >
            {updateClientMutation.isPending ? (t('detail.editClientDialog.saving') as string) : (t('detail.editClientDialog.save') as string)}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Resume validation review (GitHub issue #38) */}
      <ValidationPreviewDialog
        open={resumeValidationOpen}
        hashlistId={hashlist?.id ? Number(hashlist.id) : null}
        hashlistName={hashlist?.name || (t('detail.fallbackHashlistName') as string)}
        currentHashTypeId={hashlist?.hash_type_id ?? 0}
        totalInputLines={(hashlist as any)?.total_input_lines ?? 0}
        validCount={Math.max(
          0,
          ((hashlist as any)?.total_input_lines ?? 0) - ((hashlist as any)?.invalid_count ?? 0),
        )}
        invalidCount={(hashlist as any)?.invalid_count ?? 0}
        truncated={false}
        initialSample={resumeSample}
        onProceed={() => {
          setResumeValidationOpen(false);
          setResumeSample([]);
          queryClient.invalidateQueries({ queryKey: ['hashlist', id] });
          toast.success(t('detail.notifications.processingResumed') as string);
          refetch();
        }}
        onCancel={() => {
          setResumeValidationOpen(false);
          setResumeSample([]);
          toast.info(t('detail.notifications.uploadCancelled') as string);
          navigate('/hashlists');
        }}
      />

    </Box>
  );
}