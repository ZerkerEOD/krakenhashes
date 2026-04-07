import React, { useState } from 'react';
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
  FormControlLabel,
  Checkbox
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Download as DownloadIcon,
  PlayArrow as StartJobIcon,
  Add as AddIcon,
  Archive as ArchiveIcon,
  Unarchive as UnarchiveIcon
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, deleteHashlist, archiveHashlist, unarchiveHashlist } from '../../services/api';
import { useDeletionProgress } from '../../contexts/DeletionProgressContext';
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
  // Client potfile settings (populated from backend JOIN)
  client_enable_potfile?: boolean;
  client_exclude_from_potfile?: boolean;
  client_exclude_from_client_potfile?: boolean;
  client_remove_from_global_on_delete?: boolean | null;
  client_remove_from_client_on_delete?: boolean | null;
  // Eligibility flags computed by backend
  can_remove_from_global_potfile?: boolean;
  can_remove_from_client_potfile?: boolean;
  // Archive
  archived_at?: string | null;
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
  const { t } = useTranslation('hashlists');
  const [order, setOrder] = useState<'asc' | 'desc'>('desc');
  const [orderBy, setOrderBy] = useState<OrderBy>('createdAt');
  const [nameFilter, setNameFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<HashlistStatus | '' >(''); // Allow empty string for 'All'
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [hashlistToDelete, setHashlistToDelete] = useState<Hashlist | null>(null);
  const [removeFromGlobalPotfile, setRemoveFromGlobalPotfile] = useState(false);
  const [removeFromClientPotfile, setRemoveFromClientPotfile] = useState(false);
  const [downloadingId, setDownloadingId] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkDeleteDialogOpen, setBulkDeleteDialogOpen] = useState(false);

  const debouncedNameFilter = useDebounce(nameFilter, 500);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();
  const navigate = useNavigate();
  const { startTracking, isDeleting, getDeletion, startBulkDeletion, isBulkDeleting, bulkDeletion } = useDeletionProgress();

  // Update useQuery to include sorting and filtering parameters
  const { data: apiResponse, isLoading, isError: isFetchError } = useQuery<ApiHashlistResponse, AxiosError>({
    // Include filters in the queryKey
    queryKey: ['hashlists', orderBy, order, debouncedNameFilter, statusFilter, showArchived],
    queryFn: () => {
      const params: any = {
        sort_by: orderBy,
        order: order,
      };
      // Add filters if they have values
      if (debouncedNameFilter) {
        params.name_like = debouncedNameFilter;
      }
      if (statusFilter) {
        params.status = statusFilter;
      }
      if (showArchived) {
        params.include_archived = 'true';
      }
      return api.get<ApiHashlistResponse>('/api/hashlists', { params }).then((res: AxiosResponse<ApiHashlistResponse>) => res.data);
    }
  });

  // Delete Mutation - handles both sync and async deletion
  const deleteMutation = useMutation({
    mutationFn: async ({ hashlistId, removeFromGlobalPotfile, removeFromClientPotfile }: { hashlistId: string; removeFromGlobalPotfile?: boolean; removeFromClientPotfile?: boolean }) => {
      return deleteHashlist(hashlistId, removeFromGlobalPotfile, removeFromClientPotfile);
    },
    onSuccess: (result) => {
      if (result.async && hashlistToDelete) {
        // Async deletion — track via global context (non-blocking)
        startTracking(hashlistToDelete.id, hashlistToDelete.name);
        setDeleteDialogOpen(false);
        setHashlistToDelete(null);
      } else {
        // Sync deletion completed
        enqueueSnackbar(t('notifications.deleteSuccess') as string, { variant: 'success' });
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
    // Reset checkbox states - they will be shown only when conditions allow
    setRemoveFromGlobalPotfile(false);
    setRemoveFromClientPotfile(false);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = () => {
    if (hashlistToDelete) {
      deleteMutation.mutate({
        hashlistId: hashlistToDelete.id,
        removeFromGlobalPotfile,
        removeFromClientPotfile
      });
    }
  };

  const handleDeleteCancel = () => {
    setDeleteDialogOpen(false);
    setHashlistToDelete(null);
    setRemoveFromGlobalPotfile(false);
    setRemoveFromClientPotfile(false);
  };

  // Archive/Unarchive mutation
  const archiveMutation = useMutation({
    mutationFn: async ({ hashlistId, archive }: { hashlistId: string; archive: boolean }) => {
      if (archive) {
        await archiveHashlist(hashlistId);
      } else {
        await unarchiveHashlist(hashlistId);
      }
    },
    onSuccess: (_, variables) => {
      enqueueSnackbar(
        t(variables.archive ? 'notifications.archiveSuccess' : 'notifications.unarchiveSuccess') as string,
        { variant: 'success' }
      );
      queryClient.invalidateQueries({ queryKey: ['hashlists'] });
      setSelectedIds(new Set());
    },
    onError: (error: any, variables) => {
      const errorMsg = error.response?.data?.error || error.message || 'Operation failed';
      enqueueSnackbar(errorMsg, { variant: 'error' });
    },
  });

  // Bulk archive mutation
  const bulkArchiveMutation = useMutation({
    mutationFn: async ({ ids, archive }: { ids: string[]; archive: boolean }) => {
      await Promise.all(ids.map(id => archive ? archiveHashlist(id) : unarchiveHashlist(id)));
    },
    onSuccess: (_, variables) => {
      enqueueSnackbar(
        t(variables.archive ? 'notifications.bulkArchiveSuccess' : 'notifications.bulkUnarchiveSuccess', { count: variables.ids.length }) as string,
        { variant: 'success' }
      );
      queryClient.invalidateQueries({ queryKey: ['hashlists'] });
      setSelectedIds(new Set());
    },
    onError: (error: any) => {
      const errorMsg = error.response?.data?.error || error.message || 'Bulk operation failed';
      enqueueSnackbar(errorMsg, { variant: 'error' });
    },
  });

  // Selection handlers
  const handleSelectAll = (event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.checked) {
      setSelectedIds(new Set(hashlists.map(h => h.id)));
    } else {
      setSelectedIds(new Set());
    }
  };

  const handleSelectOne = (id: string) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
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
    enqueueSnackbar(t('notifications.uploadSuccess') as string, { variant: 'success' });
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
      enqueueSnackbar(t('notifications.downloaded', { filename }) as string, { variant: 'success' });

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
              label={t('filters.filterByName') as string}
              variant="outlined"
              size="small"
              value={nameFilter}
              onChange={(e) => setNameFilter(e.target.value)}
            />
          </Grid>
          <Grid item xs={12} sm={3}>
            <FormControl fullWidth size="small" variant="outlined">
              <InputLabel>{t('filters.status') as string}</InputLabel>
              <Select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as HashlistStatus | '')}
                label={t('filters.status') as string}
              >
                <MenuItem value=""><em>{t('filters.all') as string}</em></MenuItem>
                {allStatuses.map(status => (
                  <MenuItem key={status} value={status}>{t(`status.${status}`) as string}</MenuItem>
                ))}
              </Select>
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={3}>
            <FormControlLabel
              control={
                <Checkbox
                  checked={showArchived}
                  onChange={(e) => {
                    setShowArchived(e.target.checked);
                    setSelectedIds(new Set());
                  }}
                  size="small"
                />
              }
              label={t('archive.showArchived') as string}
            />
          </Grid>
        </Grid>
      </Box>

      {/* Bulk Action Bar */}
      {selectedIds.size > 0 && (
        <Box sx={{ px: 2, pb: 1 }}>
          <Paper
            elevation={0}
            sx={{
              p: 1.5,
              display: 'flex',
              alignItems: 'center',
              gap: 2,
              bgcolor: 'action.selected',
              borderRadius: 1,
            }}
          >
            <Typography variant="body2" fontWeight="medium">
              {t('archive.selectedCount', { count: selectedIds.size })}
            </Typography>
            <Button
              size="small"
              variant="outlined"
              startIcon={<ArchiveIcon />}
              onClick={() => {
                const ids = Array.from(selectedIds).filter(id => {
                  const hl = hashlists.find(h => h.id === id);
                  return hl && !hl.archived_at;
                });
                if (ids.length > 0) bulkArchiveMutation.mutate({ ids, archive: true });
              }}
              disabled={bulkArchiveMutation.isPending}
            >
              {t('archive.archiveSelected')}
            </Button>
            <Button
              size="small"
              variant="outlined"
              startIcon={<UnarchiveIcon />}
              onClick={() => {
                const ids = Array.from(selectedIds).filter(id => {
                  const hl = hashlists.find(h => h.id === id);
                  return hl && hl.archived_at;
                });
                if (ids.length > 0) bulkArchiveMutation.mutate({ ids, archive: false });
              }}
              disabled={bulkArchiveMutation.isPending}
            >
              {t('archive.unarchiveSelected')}
            </Button>
            <Button
              size="small"
              variant="outlined"
              color="error"
              startIcon={<DeleteIcon />}
              onClick={() => setBulkDeleteDialogOpen(true)}
              disabled={isBulkDeleting}
            >
              {t('archive.deleteSelected')}
            </Button>
          </Paper>
        </Box>
      )}

      {isFetchError && (
          <Alert severity="error" sx={{ mb: 2 }}>{t('errors.loadFailed') as string}</Alert>
      )}

      <TableContainer>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell padding="checkbox">
                <Checkbox
                  indeterminate={selectedIds.size > 0 && selectedIds.size < hashlists.length}
                  checked={hashlists.length > 0 && selectedIds.size === hashlists.length}
                  onChange={handleSelectAll}
                  size="small"
                />
              </TableCell>
              <TableCell sortDirection={orderBy === 'name' ? order : false}>
                <TableSortLabel
                  active={orderBy === 'name'}
                  direction={orderBy === 'name' ? order : 'asc'}
                  onClick={() => handleRequestSort('name')}
                >
                  {t('columns.name') as string}
                </TableSortLabel>
              </TableCell>
              <TableCell sortDirection={orderBy === 'clientName' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'clientName'}
                  direction={orderBy === 'clientName' ? order : 'asc'}
                  onClick={() => handleRequestSort('clientName')}
                >
                  {t('columns.client') as string}
                </TableSortLabel>
              </TableCell>
              <TableCell sortDirection={orderBy === 'status' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'status'}
                  direction={orderBy === 'status' ? order : 'asc'}
                  onClick={() => handleRequestSort('status')}
                >
                  {t('columns.status') as string}
                </TableSortLabel>
              </TableCell>
              <TableCell>{t('columns.totalHashes') as string}</TableCell>
              <TableCell>{t('columns.crackedHashes') as string}</TableCell>
              <TableCell>{t('columns.progress') as string}</TableCell>
              <TableCell sortDirection={orderBy === 'createdAt' ? order : false}>
                 <TableSortLabel
                  active={orderBy === 'createdAt'}
                  direction={orderBy === 'createdAt' ? order : 'asc'}
                  onClick={() => handleRequestSort('createdAt')}
                >
                  {t('columns.createdAt') as string}
                </TableSortLabel>
              </TableCell>
              <TableCell>{t('columns.actions') as string}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {(isLoading || deleteMutation.isPending) && (
              <TableRow>
                <TableCell colSpan={9}>
                  <LinearProgress />
                </TableCell>
              </TableRow>
            )}
            {!isLoading && !deleteMutation.isPending && hashlists.length === 0 && (
              <TableRow>
                 <TableCell colSpan={9} align="center">{t('table.noHashlists') as string}</TableCell>
              </TableRow>
            )}
            {!isLoading && !deleteMutation.isPending && hashlists.map((hashlist) => {
              const rowIsDeleting = isDeleting(hashlist.id);
              const deletionEntry = rowIsDeleting ? getDeletion(hashlist.id) : undefined;
              return (
              <TableRow key={hashlist.id} sx={{
                ...(hashlist.archived_at && !rowIsDeleting ? { opacity: 0.7 } : {}),
                ...(rowIsDeleting ? { opacity: 0.5, pointerEvents: 'none' } : {}),
              }}>
                <TableCell padding="checkbox">
                  <Checkbox
                    checked={selectedIds.has(hashlist.id)}
                    onChange={() => handleSelectOne(hashlist.id)}
                    size="small"
                  />
                </TableCell>
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
                  <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center', flexWrap: 'wrap' }}>
                    {rowIsDeleting ? (
                      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 120 }}>
                        <Chip
                          label={t('deletionProgress.title')}
                          color="warning"
                          size="small"
                        />
                        {deletionEntry?.progress ? (
                          <Box>
                            <LinearProgress
                              variant="determinate"
                              value={deletionEntry.progress.total > 0 ? Math.round((deletionEntry.progress.checked / deletionEntry.progress.total) * 100) : 0}
                              sx={{ height: 4, borderRadius: 2 }}
                            />
                            <Typography variant="caption" color="text.secondary">
                              {(() => {
                                switch (deletionEntry.status) {
                                  case 'deleting_hashes': return t('deletionProgress.phases.removingHashes');
                                  case 'clearing_references': return t('deletionProgress.phases.clearingReferences');
                                  case 'cleaning_orphans': return t('deletionProgress.phases.cleaningOrphans');
                                  case 'finalizing': return t('deletionProgress.phases.finalizing');
                                  default: return t('deletionProgress.phases.preparing');
                                }
                              })()}
                            </Typography>
                          </Box>
                        ) : (
                          <LinearProgress sx={{ height: 4, borderRadius: 2 }} />
                        )}
                      </Box>
                    ) : (
                      <>
                        <Chip
                          label={hashlist.status}
                          color={
                            hashlist.status === 'ready' ? 'success' :
                            hashlist.status === 'error' ? 'error' :
                            'primary'
                          }
                          size="small"
                        />
                        {hashlist.archived_at && (
                          <Chip
                            label={t('archive.archived')}
                            size="small"
                            variant="outlined"
                            color="warning"
                          />
                        )}
                      </>
                    )}
                  </Box>
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
                  <Tooltip title={t('actions.download') as string}>
                    <span> {/* Tooltip needs a DOM element if child is disabled */}
                      <IconButton
                        aria-label={t('actions.download') as string}
                        onClick={() => handleDownloadClick(hashlist)}
                        disabled={downloadingId === hashlist.id} // Disable while downloading this specific list
                      >
                        <DownloadIcon />
                      </IconButton>
                    </span>
                  </Tooltip>
                  <Tooltip title={t(hashlist.archived_at ? 'archive.unarchive' : 'archive.archive') as string}>
                    <span>
                      <IconButton
                        aria-label={t(hashlist.archived_at ? 'archive.unarchive' : 'archive.archive') as string}
                        onClick={() => archiveMutation.mutate({ hashlistId: hashlist.id, archive: !hashlist.archived_at })}
                        disabled={archiveMutation.isPending}
                        color="default"
                        size="small"
                      >
                        {hashlist.archived_at ? <UnarchiveIcon /> : <ArchiveIcon />}
                      </IconButton>
                    </span>
                  </Tooltip>
                  <Tooltip title={t('actions.delete') as string}>
                     <span>
                      <IconButton
                        aria-label={t('actions.delete') as string}
                        onClick={() => handleDeleteClick(hashlist)}
                        disabled={deleteMutation.isPending || !!downloadingId || rowIsDeleting}
                        color="error"
                      >
                        <DeleteIcon />
                      </IconButton>
                    </span>
                  </Tooltip>
                </TableCell>
              </TableRow>
              );
            })}
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
          {t('confirmDelete.title') as string}
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="alert-dialog-description">
            {t('confirmDelete.message', { name: hashlistToDelete?.name || '' }) as string}
          </DialogContentText>

          {/* Show global potfile removal option - only if eligible AND client allows override */}
          {hashlistToDelete?.can_remove_from_global_potfile &&
           hashlistToDelete?.client_remove_from_global_on_delete === null && (
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
          {hashlistToDelete?.can_remove_from_client_potfile &&
           hashlistToDelete?.client_remove_from_client_on_delete === null && (
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
            {t('confirmDelete.cancel') as string}
          </Button>
          <Button onClick={handleDeleteConfirm} color="error" autoFocus disabled={deleteMutation.isPending}>
            {deleteMutation.isPending ? t('confirmDelete.deleting') as string : t('confirmDelete.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Bulk Delete Confirmation Dialog */}
      <Dialog
        open={bulkDeleteDialogOpen}
        onClose={() => setBulkDeleteDialogOpen(false)}
      >
        <DialogTitle>{t('archive.confirmBulkDelete.title') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('archive.confirmBulkDelete.message', { count: selectedIds.size }) as string}
          </DialogContentText>
          <Alert severity="warning" sx={{ mt: 2 }}>
            {t('archive.confirmBulkDelete.warning') as string}
          </Alert>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setBulkDeleteDialogOpen(false)}>
            {t('confirmDelete.cancel') as string}
          </Button>
          <Button
            color="error"
            onClick={() => {
              const items = Array.from(selectedIds).map(id => {
                const hashlist = hashlists.find(h => h.id === id);
                return { id, name: hashlist?.name || id };
              });
              startBulkDeletion(items, (hashlistId) => deleteHashlist(hashlistId));
              setSelectedIds(new Set());
              setBulkDeleteDialogOpen(false);
            }}
          >
            {t('confirmDelete.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={uploadDialogOpen}
        onClose={handleUploadClose}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>{t('upload.title') as string}</DialogTitle>
        <DialogContent>
          <HashlistUploadForm onSuccess={handleUploadSuccess} />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleUploadClose}>{t('upload.cancel') as string}</Button>
        </DialogActions>
      </Dialog>

    </Paper>
  );
}