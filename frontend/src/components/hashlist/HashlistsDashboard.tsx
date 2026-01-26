import React, { useState, useEffect, useRef } from 'react';
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Typography,
  LinearProgress,
  Chip,
  IconButton,
  TableSortLabel,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Grid,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Button,
  Alert,
  Tooltip,
  CircularProgress
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Download as DownloadIcon,
  PlayArrow as StartJobIcon,
  Add as AddIcon
} from '@mui/icons-material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, deleteHashlist, getDeletionProgress, DeletionProgressResponse } from '../../services/api';
import { AxiosResponse, AxiosError } from 'axios';
import useDebounce from '../../hooks/useDebounce';
import { useSnackbar } from 'notistack';
import { useNavigate } from 'react-router-dom';
import HashlistUploadForm from './HashlistUploadForm';
import { format, parse, isValid, parseISO } from 'date-fns'; // Import parse and the format string

// Define the type for sortable columns
type OrderBy = 'name' | 'clientName' | 'status' | 'createdAt';

// Define Hashlist Status type/enum if not already globally defined
type HashlistStatus = 'uploading' | 'processing' | 'ready' | 'error';
const allStatuses: HashlistStatus[] = ['uploading', 'processing', 'ready', 'error'];

interface Hashlist {
  id: string;
  name: string;
  status: 'uploading' | 'processing' | 'ready' | 'error';
  total_hashes: number;
  cracked_hashes: number;
  createdAt: string;
  clientName?: string;
  client_id?: string;
  exclude_from_potfile?: boolean;
}

interface ApiHashlistResponse {
  data: Hashlist[];
  total_count: number;
  limit: number;
  offset: number;
}

// Function to extract filename from Content-Disposition header
const getFilenameFromContentDisposition = (contentDisposition: string | undefined): string | null => {
  if (!contentDisposition) return null;
  const filenameMatch = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
  if (filenameMatch && filenameMatch[3]) {
    return filenameMatch[3];
  }
  // Fallback for filename without quotes
  const filenameFallbackMatch = contentDisposition.match(/filename=([^;\n]*)/i);
  if (filenameFallbackMatch && filenameFallbackMatch[1]) {
    return filenameFallbackMatch[1].trim();
  }
  return null;
};

interface HashlistsDashboardProps {
  uploadDialogOpen: boolean;
  setUploadDialogOpen: (open: boolean) => void;
}

export default function HashlistsDashboard({ uploadDialogOpen, setUploadDialogOpen }: HashlistsDashboardProps) {
  const [order, setOrder] = useState<'asc' | 'desc'>('desc');
  const [orderBy, setOrderBy] = useState<OrderBy>('createdAt');
  const [nameFilter, setNameFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<HashlistStatus | '' >(''); // Allow empty string for 'All'
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletionProgressDialogOpen, setDeletionProgressDialogOpen] = useState(false);
  const [deletionProgress, setDeletionProgress] = useState<DeletionProgressResponse | null>(null);
  const [hashlistToDelete, setHashlistToDelete] = useState<Hashlist | null>(null);
  const [downloadingId, setDownloadingId] = useState<string | null>(null); // Track download state
  const pollingIntervalRef = useRef<NodeJS.Timeout | null>(null);

  const debouncedNameFilter = useDebounce(nameFilter, 500); // Debounce name filter input
  const queryClient = useQueryClient(); // Get query client instance
  const { enqueueSnackbar } = useSnackbar(); // Snackbar hook
  const navigate = useNavigate();

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current);
      }
    };
  }, []);

  // Update useQuery to include sorting and filtering parameters
  const { data: apiResponse, isLoading, isError: isFetchError } = useQuery<ApiHashlistResponse, AxiosError>({
    // Include filters in the queryKey
    queryKey: ['hashlists', orderBy, order, debouncedNameFilter, statusFilter],
    queryFn: () => {
      const params: any = {
        sort_by: orderBy,
        order: order,
        // Add pagination params later if needed
        // limit: 50,
        // offset: 0 
      };
      // Add filters if they have values
      if (debouncedNameFilter) {
        params.name_like = debouncedNameFilter;
      }
      if (statusFilter) {
        params.status = statusFilter;
      }
      return api.get<ApiHashlistResponse>('/api/hashlists', { params }).then((res: AxiosResponse<ApiHashlistResponse>) => res.data);
    }
  });

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
          // Wait a moment before closing so user can see completion
          setTimeout(() => {
            setDeletionProgressDialogOpen(false);
            setDeletionProgress(null);
            setHashlistToDelete(null);
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
          setDeletionProgress(null);
          setHashlistToDelete(null);
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
      if (result.async && hashlistToDelete) {
        // Async deletion - show progress dialog and start polling
        setDeleteDialogOpen(false);
        setDeletionProgress({
          hashlist_id: parseInt(hashlistToDelete.id),
          status: 'pending',
          phase: 'Preparing...',
          checked: 0,
          total: hashlistToDelete.total_hashes || 0,
          deleted: 0,
          refs_cleared: 0,
          refs_total: 0,
          jobs_deleted: 0,
          shared_preserved: 0,
          started_at: new Date().toISOString()
        });
        setDeletionProgressDialogOpen(true);
        startDeletionPolling(hashlistToDelete.id);
      } else {
        // Sync deletion completed
        enqueueSnackbar('Hashlist deleted successfully', { variant: 'success' });
        queryClient.invalidateQueries({ queryKey: ['hashlists'] });
        setDeleteDialogOpen(false);
        setHashlistToDelete(null);
      }
    },
    onError: (error: any) => {
      console.error("Error deleting hashlist:", error);
      const errorMsg = error.response?.data?.error || error.message || 'Failed to delete hashlist';
      enqueueSnackbar(errorMsg, { variant: 'error' });
      setDeleteDialogOpen(false);
      setHashlistToDelete(null);
    },
  });

  // Extract hashlists from the response, default to empty array
  const hashlists = apiResponse?.data || [];

  const handleRequestSort = (property: OrderBy) => {
    const isAsc = orderBy === property && order === 'asc';
    setOrder(isAsc ? 'desc' : 'asc');
    setOrderBy(property);
  };

  const crackPercentage = (hashlist: Hashlist) => {
    return hashlist.total_hashes > 0 
      ? Math.round((hashlist.cracked_hashes / hashlist.total_hashes) * 100)
      : 0;
  };

  // Handlers for delete dialog
  const handleDeleteClick = (hashlist: Hashlist) => {
    setHashlistToDelete(hashlist);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = () => {
    if (hashlistToDelete) {
      deleteMutation.mutate(hashlistToDelete.id);
    }
  };

  const handleDeleteCancel = () => {
    setDeleteDialogOpen(false);
    setHashlistToDelete(null);
  };

  // Handler for closing upload dialog
  const handleUploadClose = () => {
    setUploadDialogOpen(false);
  };

  // Callback for successful upload (will be passed to form)
  const handleUploadSuccess = () => {
    handleUploadClose();
    // Invalidate query to refresh list
    queryClient.invalidateQueries({ queryKey: ['hashlists'] }); 
    enqueueSnackbar('Hashlist uploaded successfully', { variant: 'success' });
  };

  // --- Download Handler ---
  const handleDownloadClick = async (hashlist: Hashlist) => {
    if (downloadingId === hashlist.id) return; // Prevent double clicks
    setDownloadingId(hashlist.id);
    
    try {
      const response = await api.get(`/api/hashlists/${hashlist.id}/download`, {
        responseType: 'blob', // Important: expect binary data
      });

      // Check if the response looks like an error (e.g., JSON instead of blob)
      if (response.data.type === 'application/json') {
          const reader = new FileReader();
          reader.onload = () => {
              try {
                  const errorJson = JSON.parse(reader.result as string);
                  enqueueSnackbar(errorJson.error || 'Failed to download file (JSON error)', { variant: 'error' });
              } catch (e) {
                  enqueueSnackbar('Failed to download file (Unknown JSON error)', { variant: 'error' });
              }
          };
          reader.onerror = () => {
               enqueueSnackbar('Failed to read error response', { variant: 'error' });
          };
          reader.readAsText(response.data);
          setDownloadingId(null);
          return;
      }

      const blob = new Blob([response.data]);
      const contentDisposition = response.headers['content-disposition'];
      const filename = getFilenameFromContentDisposition(contentDisposition) || `hashlist-${hashlist.id}.hash`;

      // Create a link element, set the download attribute, and click it
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.setAttribute('download', filename);
      document.body.appendChild(link);
      link.click();

      // Clean up
      link.parentNode?.removeChild(link);
      window.URL.revokeObjectURL(url);
      enqueueSnackbar(`Downloaded ${filename}`, { variant: 'success' });

    } catch (error) {
      console.error("Error downloading hashlist:", error);
       let errorMsg = 'Failed to download hashlist';
      if (error instanceof AxiosError && error.response) {
          if (error.response.data instanceof Blob && error.response.data.type === 'application/json') {
              // Try to read the JSON error from the blob
              try {
                  const errorJsonText = await error.response.data.text();
                  const errorJson = JSON.parse(errorJsonText);
                  errorMsg = errorJson.error || `Server error (${error.response.status})`;
              } catch (parseError) {
                  errorMsg = `Server error (${error.response.status}) - Failed to parse error details`;
              }
          } else {
             errorMsg = (error.response.data as any)?.error || error.message || `Server error (${error.response.status})`;
          }
      } else if (error instanceof Error) {
          errorMsg = error.message;
      }
      enqueueSnackbar(errorMsg, { variant: 'error' });
    } finally {
      setDownloadingId(null); // Reset download state
    }
  };
  // --- End Download Handler ---

  return (
    <Paper sx={{ width: '100%', overflow: 'hidden' }}>
      <Box sx={{ p: 2 }}>
        <Grid container spacing={2} alignItems="center">
          <Grid item xs={12} sm={4}>
            <TextField
              fullWidth
              label="Filter by Name"
              variant="outlined"
              size="small"
              value={nameFilter}
              onChange={(e) => setNameFilter(e.target.value)}
            />
          </Grid>
          <Grid item xs={12} sm={3}>
            <FormControl fullWidth size="small" variant="outlined">
              <InputLabel>Status</InputLabel>
              <Select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as HashlistStatus | '')}
                label="Status"
              >
                <MenuItem value=""><em>All</em></MenuItem>
                {allStatuses.map(status => (
                  <MenuItem key={status} value={status}>{status.charAt(0).toUpperCase() + status.slice(1)}</MenuItem>
                ))}
              </Select>
            </FormControl>
          </Grid>
        </Grid>
      </Box>

      {isFetchError && (
          <Alert severity="error" sx={{ mb: 2 }}>Error fetching hashlists.</Alert>
      )}

      <TableContainer>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell sortDirection={orderBy === 'name' ? order : false}>
                <TableSortLabel
                  active={orderBy === 'name'}
                  direction={orderBy === 'name' ? order : 'asc'}
                  onClick={() => handleRequestSort('name')}
                >
                  Name
                </TableSortLabel>
              </TableCell>
              <TableCell sortDirection={orderBy === 'clientName' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'clientName'}
                  direction={orderBy === 'clientName' ? order : 'asc'}
                  onClick={() => handleRequestSort('clientName')}
                >
                  Client
                </TableSortLabel>
              </TableCell>
              <TableCell sortDirection={orderBy === 'status' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'status'}
                  direction={orderBy === 'status' ? order : 'asc'}
                  onClick={() => handleRequestSort('status')}
                >
                  Status
                </TableSortLabel>
              </TableCell>
              <TableCell>Total Hashes</TableCell>
              <TableCell>Cracked</TableCell>
              <TableCell>Cracked (%)</TableCell>
              <TableCell sortDirection={orderBy === 'createdAt' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'createdAt'}
                  direction={orderBy === 'createdAt' ? order : 'asc'}
                  onClick={() => handleRequestSort('createdAt')}
                >
                  Created
                </TableSortLabel>
              </TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {(isLoading || deleteMutation.isPending) && (
              <TableRow>
                <TableCell colSpan={6}>
                  <LinearProgress />
                </TableCell>
              </TableRow>
            )}
            {!isLoading && !deleteMutation.isPending && hashlists.length === 0 && (
              <TableRow>
                 <TableCell colSpan={6} align="center">No hashlists found.</TableCell>
              </TableRow>
            )}
            {!isLoading && !deleteMutation.isPending && hashlists.map((hashlist) => (
              <TableRow key={hashlist.id}>
                <TableCell>
                  <Typography
                    component="a"
                    sx={{
                      cursor: 'pointer',
                      color: 'primary.main',
                      textDecoration: 'none',
                      '&:hover': {
                        textDecoration: 'underline'
                      }
                    }}
                    onClick={() => navigate(`/hashlists/${hashlist.id}`)}
                  >
                    {hashlist.name}
                  </Typography>
                </TableCell>
                <TableCell>
                  {hashlist.client_id && hashlist.clientName ? (
                    <Typography
                      component="a"
                      sx={{
                        cursor: 'pointer',
                        color: 'primary.main',
                        textDecoration: 'none',
                        '&:hover': {
                          textDecoration: 'underline'
                        }
                      }}
                      onClick={() => navigate(`/pot/client/${hashlist.client_id}`)}
                    >
                      {hashlist.clientName}
                    </Typography>
                  ) : (
                    hashlist.clientName || '-'
                  )}
                </TableCell>
                <TableCell>
                  <Chip 
                    label={hashlist.status}
                    color={
                      hashlist.status === 'ready' ? 'success' :
                      hashlist.status === 'error' ? 'error' :
                      'primary'  
                    }
                  />
                </TableCell>
                <TableCell>{hashlist.total_hashes.toLocaleString()}</TableCell>
                <TableCell>
                  <Typography
                    component="a"
                    sx={{
                      cursor: 'pointer',
                      color: 'primary.main',
                      textDecoration: 'none',
                      '&:hover': {
                        textDecoration: 'underline'
                      }
                    }}
                    onClick={() => navigate(`/pot/hashlist/${hashlist.id}`)}
                  >
                    {hashlist.cracked_hashes.toLocaleString()}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Box sx={{ display: 'flex', alignItems: 'center' }}>
                    <Box sx={{ width: '70%', mr: 1 }}> {/* Adjust width as needed */}
                      <LinearProgress 
                        variant="determinate" 
                        value={crackPercentage(hashlist)} 
                      />
                    </Box>
                    <Box sx={{ minWidth: 35 }}> {/* Ensure space for text */}
                      <Typography variant="body2" color="text.secondary">{`${crackPercentage(hashlist)}%`}</Typography>
                    </Box>
                  </Box>
                </TableCell>
                <TableCell>
                  {(() => {
                    if (!hashlist.createdAt) return 'N/A'; // Handle missing date
                    console.log('Raw createdAt:', hashlist.createdAt); // Log the raw string
                    const parsedDate = parseISO(hashlist.createdAt); // Use parseISO for standard format
                    return isValid(parsedDate)
                      ? format(parsedDate, 'yyyy-MM-dd HH:mm')
                      : 'Invalid Date'; // Fallback if parsing still fails
                  })()}
                </TableCell>
                <TableCell>
                  <Tooltip title="Download">
                    <span> {/* Tooltip needs a DOM element if child is disabled */} 
                      <IconButton 
                        aria-label="download" 
                        onClick={() => handleDownloadClick(hashlist)}
                        disabled={downloadingId === hashlist.id} // Disable while downloading this specific list
                      >
                        <DownloadIcon />
                      </IconButton>
                    </span>
                  </Tooltip>
                  <Tooltip title="Delete">
                     <span> {/* Tooltip needs a DOM element if child is disabled */} 
                      <IconButton 
                        aria-label="delete" 
                        onClick={() => handleDeleteClick(hashlist)} 
                        disabled={deleteMutation.isPending || !!downloadingId} // Also disable if any download is in progress
                        color="error"
                      >
                        <DeleteIcon />
                      </IconButton>
                    </span>
                  </Tooltip>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      <Dialog
        open={deleteDialogOpen}
        onClose={handleDeleteCancel}
        aria-labelledby="alert-dialog-title"
        aria-describedby="alert-dialog-description"
      >
        <DialogTitle id="alert-dialog-title">
          {"Confirm Deletion"}
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="alert-dialog-description">
            Are you sure you want to delete the hashlist "{hashlistToDelete?.name || ''}"?
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
                setHashlistToDelete(null);
                if (deletionProgress?.status === 'completed') {
                  queryClient.invalidateQueries({ queryKey: ['hashlists'] });
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

      <Dialog
        open={uploadDialogOpen} 
        onClose={handleUploadClose} 
        maxWidth="md"
        fullWidth 
      >
        <DialogTitle>Upload New Hashlist</DialogTitle>
        <DialogContent>
          <HashlistUploadForm onSuccess={handleUploadSuccess} />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleUploadClose}>Cancel</Button>
        </DialogActions>
      </Dialog>

    </Paper>
  );
}