import React, { useState } from 'react';
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
  TextField
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
import { api } from '../../services/api';
import CreateJobDialog from './CreateJobDialog';
import HashlistHashesTable from './HashlistHashesTable';
import ClientAutocomplete from './ClientAutocomplete';
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

export default function HashlistDetailView() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [createJobDialogOpen, setCreateJobDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [editClientDialogOpen, setEditClientDialogOpen] = useState(false);
  const [selectedClient, setSelectedClient] = useState<string | null>(null);
  const [downloadingHashlist, setDownloadingHashlist] = useState(false);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  const { data: hashlist, isLoading } = useQuery({
    queryKey: ['hashlist', id],
    queryFn: () => api.get(`/api/hashlists/${id}`).then(res => res.data)
  });

  // Delete Mutation
  const deleteMutation = useMutation<AxiosResponse, AxiosError, string>({
    mutationFn: (hashlistId: string) => api.delete(`/api/hashlists/${hashlistId}`),
    onSuccess: () => {
      enqueueSnackbar('Hashlist deleted successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['hashlists'] });
      navigate('/hashlists'); // Redirect to list after deletion
    },
    onError: (error) => {
      const errorMsg = (error.response?.data as any)?.error || error.message || 'Failed to delete hashlist';
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

        <Box display="flex" gap={2} sx={{ mt: 3 }}>
          <Typography>
            Status: <Chip
              label={hashlist.status}
              color={
                hashlist.status === 'ready' ? 'success' :
                hashlist.status === 'error' ? 'error' : 'primary'
              }
            />
          </Typography>
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
    </Box>
  );
}