import React, { useState, useEffect, useRef } from 'react';
import {
  Box,
  Paper,
  Typography,
  Chip,
  LinearProgress,
  Button,
  Divider,
  Tooltip,
  IconButton,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
  CircularProgress
} from '@mui/material';
import {
  Download as DownloadIcon,
  Delete as DeleteIcon,
  History as HistoryIcon,
  ArrowBack as ArrowBackIcon,
  PlayArrow as PlayArrowIcon,
  Edit as EditIcon
} from '@mui/icons-material';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, deleteHashlist, getDeletionProgress, DeletionProgressResponse, getProcessingProgress, ProcessingProgressResponse } from '../../services/api';
import CreateJobDialog from './CreateJobDialog';
import HashlistHashesTable from './HashlistHashesTable';
import ClientAutocomplete from './ClientAutocomplete';
import AssociationWordlistManager from './AssociationWordlistManager';
import { useSnackbar } from 'notistack';
import { AxiosResponse, AxiosError } from 'axios';

interface HashDetail {
  id: string;
  hash_value: string;
  original_hash: string;
  username?: string;
  domain?: string;
  hash_type_id: number;
  is_cracked: boolean;
  password?: string;
  last_updated: string;
  // Frontend friendly aliases
  hash?: string;
  isCracked?: boolean;
  crackedText?: string;
}

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
  const [createJobDialogOpen, setCreateJobDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletionProgressDialogOpen, setDeletionProgressDialogOpen] = useState(false);
  const [deletionProgress, setDeletionProgress] = useState<DeletionProgressResponse | null>(null);
  const [processingProgress, setProcessingProgress] = useState<ProcessingProgressResponse | null>(null);
  const [editClientDialogOpen, setEditClientDialogOpen] = useState(false);
  const [selectedClient, setSelectedClient] = useState<string | null>(null);
  const [downloadingHashlist, setDownloadingHashlist] = useState(false);
  const pollingIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const processingPollingRef = useRef<NodeJS.Timeout | null>(null);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current);
      }
      if (processingPollingRef.current) {
        clearInterval(processingPollingRef.current);
      }
    };
  }, []);

  const { data: hashlist, isLoading, refetch } = useQuery({
    queryKey: ['hashlist', id],
    queryFn: () => api.get(`/api/hashlists/${id}`).then(res => res.data)
  });

  // Poll for processing progress when status is "processing"
  useEffect(() => {
    if (!hashlist || hashlist.status !== 'processing' || !id) {
      // Clear any existing processing polling
      if (processingPollingRef.current) {
        clearInterval(processingPollingRef.current);
        processingPollingRef.current = null;
      }
      setProcessingProgress(null);
      return;
    }

    const pollProgress = async () => {
      try {
        const progress = await getProcessingProgress(id);
        setProcessingProgress(progress);

        // If processing is complete, stop polling and refresh hashlist
        if (progress.status === 'completed' || progress.status === 'failed') {
          if (processingPollingRef.current) {
            clearInterval(processingPollingRef.current);
            processingPollingRef.current = null;
          }
          // Refetch hashlist to get updated status
          refetch();
        }
      } catch (error: any) {
        // 404 means processing already completed
        if (error.response?.status === 404) {
          if (processingPollingRef.current) {
            clearInterval(processingPollingRef.current);
            processingPollingRef.current = null;
          }
          setProcessingProgress(null);
          refetch();
        }
      }
    };

    // Poll immediately then every 2 seconds
    pollProgress();
    processingPollingRef.current = setInterval(pollProgress, 2000);

    return () => {
      if (processingPollingRef.current) {
        clearInterval(processingPollingRef.current);
        processingPollingRef.current = null;
      }
    };
  }, [hashlist?.status, id, refetch]);

  // Start polling for deletion progress
  const startDeletionPolling = (hashlistId: string) => {
    // Clear any existing interval
    if (pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current);
    }

    // Poll immediately, then every 2 seconds
    const pollProgress = async () => {
      try {
        const progress = await getDeletionProgress(hashlistId);
        setDeletionProgress(progress);

        if (progress.status === 'completed') {
          // Stop polling
          if (pollingIntervalRef.current) {
            clearInterval(pollingIntervalRef.current);
            pollingIntervalRef.current = null;
          }
          enqueueSnackbar('Hashlist deleted successfully', { variant: 'success' });
          queryClient.invalidateQueries({ queryKey: ['hashlists'] });
          // Wait a moment before redirecting so user can see completion
          setTimeout(() => {
            setDeletionProgressDialogOpen(false);
            navigate('/hashlists');
          }, 1500);
        } else if (progress.status === 'failed') {
          // Stop polling
          if (pollingIntervalRef.current) {
            clearInterval(pollingIntervalRef.current);
            pollingIntervalRef.current = null;
          }
          enqueueSnackbar(`Deletion failed: ${progress.error}`, { variant: 'error' });
        }
      } catch (error: any) {
        // 404 means deletion already completed and was cleaned up
        if (error.response?.status === 404) {
          if (pollingIntervalRef.current) {
            clearInterval(pollingIntervalRef.current);
            pollingIntervalRef.current = null;
          }
          enqueueSnackbar('Hashlist deleted successfully', { variant: 'success' });
          queryClient.invalidateQueries({ queryKey: ['hashlists'] });
          setDeletionProgressDialogOpen(false);
          navigate('/hashlists');
        }
      }
    };

    pollProgress(); // Poll immediately
    pollingIntervalRef.current = setInterval(pollProgress, 2000);
  };

  // Delete Mutation - handles both sync and async deletion
  const deleteMutation = useMutation({
    mutationFn: async (hashlistId: string) => {
      return deleteHashlist(hashlistId);
    },
    onSuccess: (result) => {
      if (result.async) {
        // Async deletion - show progress dialog and start polling
        setDeleteDialogOpen(false);
        setDeletionProgress({
          hashlist_id: parseInt(id!),
          status: 'pending',
          phase: 'Preparing...',
          checked: 0,
          total: hashlist?.total_hashes || 0,
          deleted: 0,
          refs_cleared: 0,
          refs_total: 0,
          jobs_deleted: 0,
          shared_preserved: 0,
          started_at: new Date().toISOString()
        });
        setDeletionProgressDialogOpen(true);
        startDeletionPolling(id!);
      } else {
        // Sync deletion completed
        enqueueSnackbar('Hashlist deleted successfully', { variant: 'success' });
        queryClient.invalidateQueries({ queryKey: ['hashlists'] });
        navigate('/hashlists');
      }
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || 'Failed to delete hashlist';
      enqueueSnackbar(errorMsg, { variant: 'error' });
      setDeleteDialogOpen(false);
    },
  });

  const handleDeleteClick = () => {
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = () => {
    if (id) {
      deleteMutation.mutate(id);
    }
  };

  const handleDeleteCancel = () => {
    setDeleteDialogOpen(false);
  };

  // Update Client Mutation
  const updateClientMutation = useMutation({
    mutationFn: async (clientId: string | null) => {
      return api.patch(`/api/hashlists/${id}/client`, { client_id: clientId });
    },
    onSuccess: () => {
      enqueueSnackbar('Client updated successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['hashlist', id] });
      queryClient.invalidateQueries({ queryKey: ['hashlists'] });
      setEditClientDialogOpen(false);
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || 'Failed to update client';
      enqueueSnackbar(errorMsg, { variant: 'error' });
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
          enqueueSnackbar('Client not found', { variant: 'error' });
        }
      } catch (error) {
        console.error('Failed to lookup client:', error);
        enqueueSnackbar('Failed to lookup client', { variant: 'error' });
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
            enqueueSnackbar(errorJson.error || 'Failed to download file', { variant: 'error' });
          } catch (e) {
            enqueueSnackbar('Failed to download file', { variant: 'error' });
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
      enqueueSnackbar(`Downloaded ${filename}`, { variant: 'success' });

    } catch (error: any) {
      console.error("Error downloading hashlist:", error);
      let errorMsg = 'Failed to download hashlist';
      if (error.response?.data instanceof Blob && error.response.data.type === 'application/json') {
        try {
          const errorJsonText = await error.response.data.text();
          const errorJson = JSON.parse(errorJsonText);
          errorMsg = errorJson.error || `Server error (${error.response.status})`;
        } catch (parseError) {
          errorMsg = `Server error (${error.response.status})`;
        }
      } else if (error.response?.data?.error) {
        errorMsg = error.response.data.error;
      } else if (error.message) {
        errorMsg = error.message;
      }
      enqueueSnackbar(errorMsg, { variant: 'error' });
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
          Back to Hashlists
        </Button>
      </Box>
      
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
              Create Job
            </Button>
            <Tooltip title="Download">
              <span>
                <IconButton
                  onClick={handleDownloadClick}
                  disabled={downloadingHashlist}
                >
                  <DownloadIcon />
                </IconButton>
              </span>
            </Tooltip>
            <Tooltip title="Delete">
              <IconButton color="error" onClick={handleDeleteClick}>
                <DeleteIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </Box>

        <Typography variant="subtitle1" color="text.secondary" sx={{ mt: 1 }}>
          {hashlist.description || 'No description'}
        </Typography>

        <Box display="flex" gap={2} sx={{ mt: 3 }} flexWrap="wrap" alignItems="center">
          <Box display="flex" alignItems="center" gap={1}>
            <Typography component="span">Status:</Typography>
            <Chip
              label={hashlist.status}
              color={
                hashlist.status === 'ready' ? 'success' :
                hashlist.status === 'error' ? 'error' : 'primary'
              }
              size="small"
            />
            {/* Inline processing progress */}
            {hashlist.status === 'processing' && processingProgress && processingProgress.processed_lines !== undefined && (
              <Box display="flex" alignItems="center" gap={1} sx={{ ml: 1 }}>
                <CircularProgress size={16} />
                <Typography variant="body2" color="text.secondary">
                  {processingProgress.processed_lines.toLocaleString()} / {processingProgress.total_lines.toLocaleString()} lines
                  {processingProgress.lines_per_second > 0 && (
                    <> ({Math.round(processingProgress.lines_per_second).toLocaleString()}/sec)</>
                  )}
                  {processingProgress.lines_per_second > 0 && processingProgress.total_lines > processingProgress.processed_lines && (
                    <> • ETA: {formatETA((processingProgress.total_lines - processingProgress.processed_lines) / processingProgress.lines_per_second)}</>
                  )}
                </Typography>
              </Box>
            )}
          </Box>
          <Typography>
            Hash Type: {hashlist.hashTypeName}
          </Typography>
          <Typography>
            Client: {hashlist.client_name || 'None'}
            <Tooltip title="Edit Client">
              <IconButton size="small" onClick={handleEditClientClick} sx={{ ml: 1 }}>
                <EditIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Typography>
          <Typography>
            Created: {new Date(hashlist.createdAt).toLocaleString()}
          </Typography>
        </Box>

        <Box sx={{ mt: 3 }}>
          <Typography variant="subtitle2">
            Crack Progress ({hashlist.cracked_hashes || 0} of {hashlist.total_hashes || 0})
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

      <Paper sx={{ p: 3 }}>
        <Typography variant="h6" gutterBottom>
          <HistoryIcon sx={{ verticalAlign: 'middle', mr: 1 }} />
          History
        </Typography>
        <Divider sx={{ mb: 2 }} />
        <Typography color="text.secondary">
          History log will appear here
        </Typography>
      </Paper>

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
          Confirm Deletion
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="alert-dialog-description">
            Are you sure you want to delete the hashlist "{hashlist?.name || ''}"?
            This action cannot be undone.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleDeleteCancel} color="primary">
            Cancel
          </Button>
          <Button onClick={handleDeleteConfirm} color="error" autoFocus disabled={deleteMutation.isPending}>
            {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
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
          Edit Client Assignment
        </DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ mb: 2 }}>
            Select a client for this hashlist or leave empty to remove the client assignment.
          </DialogContentText>
          <ClientAutocomplete
            value={selectedClient}
            onChange={(value) => setSelectedClient(value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleEditClientCancel} color="primary">
            Cancel
          </Button>
          <Button
            onClick={handleEditClientConfirm}
            color="primary"
            variant="contained"
            disabled={updateClientMutation.isPending}
          >
            {updateClientMutation.isPending ? 'Saving...' : 'Save'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Deletion Progress Dialog */}
      <Dialog
        open={deletionProgressDialogOpen}
        maxWidth="sm"
        fullWidth
        disableEscapeKeyDown
      >
        <DialogTitle>
          {deletionProgress?.status === 'completed' ? 'Hashlist Deleted Successfully' : 'Deleting Hashlist'}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ textAlign: 'center', py: 2 }}>
            {deletionProgress?.status === 'completed' ? (
              <>
                <Typography variant="h6" color="success.main" gutterBottom>
                  Deletion Complete!
                </Typography>
                {/* Summary stats */}
                <Box sx={{ mt: 2, textAlign: 'left', bgcolor: 'grey.50', p: 2, borderRadius: 1 }}>
                  <Typography variant="subtitle2" gutterBottom sx={{ fontWeight: 'bold' }}>
                    Summary:
                  </Typography>
                  <Typography variant="body2" sx={{ mb: 0.5 }}>
                    Total hashes processed: {deletionProgress.total.toLocaleString()}
                  </Typography>
                  <Typography variant="body2" sx={{ mb: 0.5 }}>
                    Orphan hashes deleted: {deletionProgress.deleted.toLocaleString()}
                  </Typography>
                  <Typography variant="body2" sx={{ mb: 0.5 }}>
                    Shared hashes preserved: {(deletionProgress.shared_preserved || 0).toLocaleString()}
                  </Typography>
                  <Typography variant="body2" sx={{ mb: 0.5 }}>
                    Jobs deleted: {(deletionProgress.jobs_deleted || 0).toLocaleString()}
                  </Typography>
                  {deletionProgress.duration && (
                    <Typography variant="body2">
                      Duration: {deletionProgress.duration}
                    </Typography>
                  )}
                </Box>
              </>
            ) : deletionProgress?.status === 'failed' ? (
              <Typography variant="h6" color="error.main" gutterBottom>
                Deletion Failed
              </Typography>
            ) : (
              <>
                <CircularProgress size={60} sx={{ mb: 2 }} />
                {/* Phase-based progress display */}
                {(() => {
                  // Determine phase info based on status
                  const getPhaseInfo = () => {
                    switch (deletionProgress?.status) {
                      case 'deleting_hashes':
                        return { phase: 1, total: 3, label: 'Removing hashes', current: deletionProgress.checked, max: deletionProgress.total, unit: 'hashes' };
                      case 'clearing_references':
                        return { phase: 2, total: 3, label: 'Clearing task references', current: deletionProgress.refs_cleared || 0, max: deletionProgress.refs_total || 1, unit: 'references' };
                      case 'cleaning_orphans':
                        return { phase: 3, total: 3, label: 'Cleaning orphan hashes', current: deletionProgress.checked, max: deletionProgress.total, unit: 'hashes' };
                      case 'finalizing':
                        return { phase: 3, total: 3, label: 'Finalizing deletion', current: 100, max: 100, unit: '' };
                      default:
                        return { phase: 0, total: 3, label: 'Preparing...', current: 0, max: 100, unit: '' };
                    }
                  };
                  const phaseInfo = getPhaseInfo();
                  const percent = phaseInfo.max > 0 ? Math.round((phaseInfo.current / phaseInfo.max) * 100) : 0;

                  return (
                    <Box sx={{ mt: 2, width: '100%' }}>
                      <Typography variant="subtitle1" fontWeight="bold" gutterBottom>
                        Phase {phaseInfo.phase}/{phaseInfo.total}: {phaseInfo.label}
                      </Typography>
                      <LinearProgress
                        variant="determinate"
                        value={percent}
                        sx={{ height: 10, borderRadius: 5, my: 1 }}
                      />
                      <Typography variant="body2" color="text.secondary">
                        {phaseInfo.current.toLocaleString()} / {phaseInfo.max.toLocaleString()} {phaseInfo.unit} ({percent}%)
                      </Typography>
                    </Box>
                  );
                })()}
              </>
            )}

            {deletionProgress?.error && (
              <Typography variant="body2" color="error" sx={{ mt: 2 }}>
                Error: {deletionProgress.error}
              </Typography>
            )}
          </Box>
        </DialogContent>
        {(deletionProgress?.status === 'failed' || deletionProgress?.status === 'completed') && (
          <DialogActions>
            <Button
              onClick={() => {
                setDeletionProgressDialogOpen(false);
                setDeletionProgress(null);
                if (deletionProgress?.status === 'completed') {
                  queryClient.invalidateQueries({ queryKey: ['hashlists'] });
                  navigate('/hashlists');
                }
              }}
              color="primary"
              variant={deletionProgress?.status === 'completed' ? 'contained' : 'text'}
            >
              {deletionProgress?.status === 'completed' ? 'Done' : 'Close'}
            </Button>
          </DialogActions>
        )}
      </Dialog>
    </Box>
  );
}