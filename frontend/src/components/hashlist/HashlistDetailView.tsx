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
  CircularProgress,
  FormControlLabel,
  Checkbox,
  Alert
} from '@mui/material';
import {
  Download as DownloadIcon,
  Delete as DeleteIcon,
  History as HistoryIcon,
  ArrowBack as ArrowBackIcon,
  PlayArrow as PlayArrowIcon,
  Edit as EditIcon,
  Visibility as VisibilityIcon
} from '@mui/icons-material';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, deleteHashlist, getProcessingProgress, ProcessingProgressResponse } from '../../services/api';
import { useDeletionProgress } from '../../contexts/DeletionProgressContext';
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
  const [removeFromGlobalPotfile, setRemoveFromGlobalPotfile] = useState(false);
  const [removeFromClientPotfile, setRemoveFromClientPotfile] = useState(false);
  const [processingProgress, setProcessingProgress] = useState<ProcessingProgressResponse | null>(null);
  const [editClientDialogOpen, setEditClientDialogOpen] = useState(false);
  const [selectedClient, setSelectedClient] = useState<string | null>(null);
  const [downloadingHashlist, setDownloadingHashlist] = useState(false);
  const processingPollingRef = useRef<NodeJS.Timeout | null>(null);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();
  const { startTracking, isDeleting, getDeletion } = useDeletionProgress();

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (processingPollingRef.current) {
        clearInterval(processingPollingRef.current);
      }
    };
  }, []);

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

      {/* Non-blocking deletion progress banner */}
      {id && isDeleting(id) && (() => {
        const entry = getDeletion(id);
        const phaseLabel = (() => {
          switch (entry?.status) {
            case 'deleting_hashes': return 'Removing hashes';
            case 'clearing_references': return 'Clearing task references';
            case 'cleaning_orphans': return 'Cleaning orphan hashes';
            case 'finalizing': return 'Finalizing deletion';
            default: return 'Preparing...';
          }
        })();
        const progress = entry?.progress;
        const percent = progress && progress.total > 0 ? Math.round((progress.checked / progress.total) * 100) : 0;
        return (
          <Alert severity="warning" sx={{ mb: 2 }}>
            <Typography variant="subtitle2" gutterBottom>
              This hashlist is being deleted — {phaseLabel}
            </Typography>
            <LinearProgress
              variant={progress ? 'determinate' : 'indeterminate'}
              value={percent}
              sx={{ height: 6, borderRadius: 3 }}
            />
            {progress && (
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                {progress.checked.toLocaleString()} / {progress.total.toLocaleString()} ({percent}%)
              </Typography>
            )}
          </Alert>
        );
      })()}

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
            <Button
              variant="outlined"
              startIcon={<VisibilityIcon />}
              onClick={() => navigate(`/pot/hashlist/${id}`)}
              disabled={!hashlist.cracked_hashes || hashlist.cracked_hashes === 0}
            >
              View Cracked Hashes
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
              label="Remove cracked passwords from global potfile"
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
              label="Remove cracked passwords from client potfile"
              sx={{ mt: 1, display: 'block' }}
            />
          )}
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

    </Box>
  );
}