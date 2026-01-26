/**
 * Wordlists Management page for KrakenHashes frontend.
 *
 * Features:
 *   - View wordlists
 *   - Add new wordlists
 *   - Update wordlist information
 *   - Delete wordlists
 *   - Enable/disable wordlists
 */
import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Typography,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TableSortLabel,
  IconButton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  MenuItem,
  Grid,
  Divider,
  Switch,
  FormControlLabel,
  CircularProgress,
  Alert,
  Tooltip,
  InputAdornment,
  Toolbar,
  alpha,
  Tab,
  Tabs,
  Checkbox,
  FormControl,
  InputLabel,
  Select
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Edit as EditIcon,
  Refresh as RefreshIcon,
  CloudDownload as DownloadIcon,
  Search as SearchIcon,
  Add as AddIcon,
  Check as CheckIcon,
  Clear as ClearIcon,
  Verified as VerifiedIcon
} from '@mui/icons-material';
import FileUpload from '../components/common/FileUpload';
import { Wordlist, WordlistStatus, WordlistType, DeletionImpact } from '../types/wordlists';
import * as wordlistService from '../services/wordlists';
import { useSnackbar } from 'notistack';
import { formatFileSize, formatAttackMode } from '../utils/formatters';

export default function WordlistsManagement() {
  const { t } = useTranslation('admin');
  const [wordlists, setWordlists] = useState<Wordlist[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [openUploadDialog, setOpenUploadDialog] = useState(false);
  const [openEditDialog, setOpenEditDialog] = useState(false);
  const [currentWordlist, setCurrentWordlist] = useState<Wordlist | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [nameEdit, setNameEdit] = useState('');
  const [descriptionEdit, setDescriptionEdit] = useState('');
  const [wordlistTypeEdit, setWordlistTypeEdit] = useState<WordlistType>(WordlistType.GENERAL);
  const [formatEdit, setFormatEdit] = useState('plaintext');
  const [tabValue, setTabValue] = useState(0);
  const [sortBy, setSortBy] = useState<keyof Wordlist>('updated_at');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');
  const { enqueueSnackbar } = useSnackbar();
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [selectedWordlistType, setSelectedWordlistType] = useState<WordlistType>(WordlistType.GENERAL);
  const [isLoading, setIsLoading] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [wordlistToDelete, setWordlistToDelete] = useState<{id: string, name: string} | null>(null);
  const [deletionImpact, setDeletionImpact] = useState<DeletionImpact | null>(null);
  const [confirmationId, setConfirmationId] = useState('');
  const [isCheckingImpact, setIsCheckingImpact] = useState(false);

  // Fetch wordlists
  const fetchWordlists = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await wordlistService.getWordlists();
      setWordlists(response.data);
    } catch (err) {
      console.error('Error fetching wordlists:', err);
      setError(t('wordlists.errors.loadFailed') as string);
      enqueueSnackbar(t('wordlists.errors.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [enqueueSnackbar, t]);

  useEffect(() => {
    fetchWordlists();
  }, [fetchWordlists]);

  // Handle file upload
  const handleUploadWordlist = async (formData: FormData) => {
    try {
      setIsLoading(true);

      // Add the wordlist type to the form data
      formData.append('wordlist_type', selectedWordlistType);

      // Add required fields if not present
      if (!formData.has('name')) {
        const file = formData.get('file') as File;
        if (file) {
          formData.append('name', file.name.split('.')[0]);
        }
      }

      if (!formData.has('format')) {
        const file = formData.get('file') as File;
        if (file) {
          const extension = file.name.split('.').pop()?.toLowerCase() || 'txt';
          // Map file extension to the correct format enum value
          const format = ['gz', 'zip'].includes(extension) ? 'compressed' : 'plaintext';
          formData.append('format', format);
          console.debug(`[Wordlist Upload] Mapped file extension '${extension}' to format '${format}'`);
        } else {
          formData.append('format', 'plaintext');
        }
      }

      console.debug('[Wordlist Upload] Sending form data:',
        Array.from(formData.entries()).reduce((obj, [key, val]) => {
          obj[key] = key === 'file' ? '(file content)' : val;
          return obj;
        }, {} as Record<string, any>)
      );

      console.debug('[Wordlist Upload] Authentication cookies before upload:', document.cookie);
      console.debug('[Wordlist Upload] Upload URL:', '/api/wordlists/upload');

      try {
        const response = await wordlistService.uploadWordlist(formData, (progress, eta, speed) => {
          // Update progress in the FileUpload component
          const progressEvent = new CustomEvent('upload-progress', { detail: { progress, eta, speed } });
          document.dispatchEvent(progressEvent);
        });
        console.debug('[Wordlist Upload] Upload successful:', response);

        // Check if the response indicates a duplicate wordlist
        if (response.data.duplicate) {
          enqueueSnackbar(t('wordlists.messages.duplicateWordlist', { name: response.data.name }) as string, { variant: 'info' });
        } else {
          enqueueSnackbar(t('wordlists.messages.uploadSuccess') as string, { variant: 'success' });
        }

        setUploadDialogOpen(false);
        fetchWordlists();
      } catch (uploadError) {
        console.error('[Wordlist Upload] Upload error details:', uploadError);
        console.debug('[Wordlist Upload] Authentication cookies after error:', document.cookie);
        throw uploadError;
      }

      console.debug('[Wordlist Upload] Authentication cookies after upload:', document.cookie);
    } catch (error) {
      console.error('Error uploading wordlist:', error);
      enqueueSnackbar(t('wordlists.errors.uploadFailed') as string, { variant: 'error' });
    } finally {
      setIsLoading(false);
    }
  };

  // Handle wordlist deletion
  const handleDelete = async (id: string, name: string, confirmId?: number) => {
    try {
      await wordlistService.deleteWordlist(id, confirmId);
      enqueueSnackbar(t('wordlists.messages.deleteSuccess', { name }) as string, { variant: 'success' });
      fetchWordlists();
    } catch (err: any) {
      console.error('Error deleting wordlist:', err);
      // Extract error message from axios response
      const errorMessage = err.response?.data?.error || t('wordlists.errors.deleteFailed') as string;
      enqueueSnackbar(errorMessage, { variant: 'error' });
    } finally {
      closeDeleteDialog();
    }
  };

  // Open delete confirmation dialog - first check for deletion impact
  const openDeleteDialog = async (id: string, name: string) => {
    setWordlistToDelete({ id, name });
    setDeleteDialogOpen(true);
    setIsCheckingImpact(true);
    setDeletionImpact(null);
    setConfirmationId('');

    try {
      const response = await wordlistService.getWordlistDeletionImpact(id);
      setDeletionImpact(response.data);
    } catch (err: any) {
      console.error('Error getting deletion impact:', err);
      // If we can't get the impact, still allow deletion with simple confirmation
      setDeletionImpact(null);
    } finally {
      setIsCheckingImpact(false);
    }
  };

  // Close delete confirmation dialog
  const closeDeleteDialog = () => {
    setDeleteDialogOpen(false);
    setWordlistToDelete(null);
    setDeletionImpact(null);
    setConfirmationId('');
  };

  // Check if confirmation ID matches for cascade delete
  const isConfirmationValid = () => {
    if (!deletionImpact?.has_cascading_impact) return true;
    return confirmationId === String(deletionImpact.resource_id);
  };

  // Handle wordlist download
  const handleDownload = async (id: string, name: string) => {
    try {
      // Direct download without loading into memory
      // This allows streaming of large files
      const link = document.createElement('a');
      link.href = `/api/wordlists/${id}/download`;
      link.setAttribute('download', `${name}.txt`);
      link.style.display = 'none';
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    } catch (err) {
      console.error('Error downloading wordlist:', err);
      enqueueSnackbar(t('wordlists.errors.downloadFailed') as string, { variant: 'error' });
    }
  };

  // Handle edit button click
  const handleEditClick = (wordlist: Wordlist) => {
    setCurrentWordlist(wordlist);
    setNameEdit(wordlist.name);
    setDescriptionEdit(wordlist.description);
    setWordlistTypeEdit(wordlist.wordlist_type);
    setFormatEdit(wordlist.format);
    setOpenEditDialog(true);
  };

  // Handle refresh wordlist
  const handleRefreshWordlist = async (id: string) => {
    try {
      setLoading(true);
      const response = await wordlistService.refreshWordlist(id);
      enqueueSnackbar(t('wordlists.messages.refreshSuccess') as string, { variant: 'success' });
      // Refresh the wordlist data
      fetchWordlists();
    } catch (err: any) {
      console.error('Error refreshing wordlist:', err);
      const errorMessage = err.response?.data?.error || t('wordlists.errors.refreshFailed') as string;
      enqueueSnackbar(errorMessage, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  // Handle save edit
  const handleSaveEdit = async () => {
    if (!currentWordlist) return;

    try {
      console.debug('[Wordlist Edit] Updating wordlist:', currentWordlist.id, {
        name: nameEdit,
        description: descriptionEdit,
        wordlist_type: wordlistTypeEdit
      });

      const response = await wordlistService.updateWordlist(currentWordlist.id, {
        name: nameEdit,
        description: descriptionEdit,
        wordlist_type: wordlistTypeEdit
      });

      console.debug('[Wordlist Edit] Update successful:', response);
      enqueueSnackbar(t('wordlists.messages.updateSuccess') as string, { variant: 'success' });
      setOpenEditDialog(false);
      fetchWordlists();
    } catch (err: any) {
      console.error('[Wordlist Edit] Error updating wordlist:', err);

      if (err.response?.status === 401) {
        enqueueSnackbar(t('wordlists.errors.sessionExpired') as string, { variant: 'error' });
      } else {
        enqueueSnackbar(t('wordlists.errors.updateFailed', { error: err.response?.data?.message || err.message }) as string, { variant: 'error' });
      }
    }
  };

  // Handle sort change
  const handleSortChange = (column: keyof Wordlist) => {
    if (sortBy === column) {
      // If already sorting by this column, toggle order
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
    } else {
      // Otherwise, sort by this column in ascending order
      setSortBy(column);
      setSortOrder('asc');
    }
  };

  // Render sort label
  const renderSortLabel = (column: keyof Wordlist, label: string) => {
    return (
      <TableSortLabel
        active={sortBy === column}
        direction={sortBy === column ? sortOrder : 'asc'}
        onClick={() => handleSortChange(column)}
      >
        {label}
      </TableSortLabel>
    );
  };

  // Filter and sort wordlists
  const filteredWordlists = wordlists
    .filter(wordlist => {
      // Filter by search term
      const matchesSearch = wordlist.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
                           wordlist.description.toLowerCase().includes(searchTerm.toLowerCase());

      // Filter by tab
      if (tabValue === 0) return matchesSearch; // All
      if (tabValue === 1) return matchesSearch && wordlist.wordlist_type === WordlistType.GENERAL;
      if (tabValue === 2) return matchesSearch && wordlist.wordlist_type === WordlistType.SPECIALIZED;
      if (tabValue === 3) return matchesSearch && wordlist.wordlist_type === WordlistType.TARGETED;
      if (tabValue === 4) return matchesSearch && wordlist.wordlist_type === WordlistType.CUSTOM;

      return matchesSearch;
    })
    .sort((a, b) => {
      // Handle special cases for non-string fields
      if (sortBy === 'file_size' || sortBy === 'word_count') {
        return sortOrder === 'asc'
          ? a[sortBy] - b[sortBy]
          : b[sortBy] - a[sortBy];
      }

      // Handle date fields
      if (sortBy === 'created_at' || sortBy === 'updated_at' || sortBy === 'last_verified_at') {
        const dateA = new Date(a[sortBy] || 0).getTime();
        const dateB = new Date(b[sortBy] || 0).getTime();
        return sortOrder === 'asc' ? dateA - dateB : dateB - dateA;
      }

      // Default string comparison
      const valueA = String(a[sortBy] || '').toLowerCase();
      const valueB = String(b[sortBy] || '').toLowerCase();
      return sortOrder === 'asc'
        ? valueA.localeCompare(valueB)
        : valueB.localeCompare(valueA);
    });

  // Render status chip based on verification status
  const renderStatusChip = (status: string) => {
    switch (status) {
      case 'verified':
        return <Chip label={t('wordlists.status.verified') as string} color="success" size="small" />;
      case 'pending':
        return <Chip label={t('wordlists.status.pending') as string} color="warning" size="small" />;
      case 'failed':
        return <Chip label={t('wordlists.status.failed') as string} color="error" size="small" />;
      default:
        return <Chip label={status} color="default" size="small" />;
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Grid container spacing={2} alignItems="center" sx={{ mb: 3 }}>
          <Grid item xs={12} sm={6}>
            <Typography variant="h4" component="h1" gutterBottom>
              {t('wordlists.title') as string}
            </Typography>
            <Typography variant="body1" color="text.secondary">
              {t('wordlists.description') as string}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} sx={{ textAlign: { xs: 'left', sm: 'right' } }}>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => setUploadDialogOpen(true)}
              sx={{ mr: 1 }}
              disabled={isLoading}
            >
              {t('wordlists.uploadWordlist') as string}
            </Button>
            <Button
              variant="outlined"
              startIcon={<RefreshIcon />}
              onClick={() => fetchWordlists()}
            >
              {t('wordlists.refresh') as string}
            </Button>
          </Grid>
        </Grid>

        {error && (
          <Alert severity="error" sx={{ mb: 3 }}>
            {error}
          </Alert>
        )}

        <Paper sx={{ mb: 3, overflow: 'hidden' }}>
          <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
            <Tabs
              value={tabValue}
              onChange={(_, newValue) => setTabValue(newValue)}
              aria-label="wordlist tabs"
            >
              <Tab label={t('wordlists.tabs.all') as string} id="tab-0" />
              <Tab label={t('wordlists.tabs.general') as string} id="tab-1" />
              <Tab label={t('wordlists.tabs.specialized') as string} id="tab-2" />
              <Tab label={t('wordlists.tabs.targeted') as string} id="tab-3" />
              <Tab label={t('wordlists.tabs.custom') as string} id="tab-4" />
            </Tabs>
          </Box>

          <Toolbar
            sx={{
              pl: { sm: 2 },
              pr: { xs: 1, sm: 1 },
              display: 'flex',
              justifyContent: 'center'
            }}
          >
            <TextField
              margin="dense"
              placeholder={t('wordlists.searchPlaceholder') as string}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon />
                  </InputAdornment>
                ),
                endAdornment: searchTerm && (
                  <InputAdornment position="end">
                    <IconButton size="small" onClick={() => setSearchTerm('')}>
                      <ClearIcon />
                    </IconButton>
                  </InputAdornment>
                )
              }}
              size="small"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              sx={{ width: { xs: '100%', sm: '60%', md: '40%' } }}
            />
          </Toolbar>

          <Divider />

          <TableContainer>
            <Table sx={{ minWidth: 650 }} aria-label="wordlists table">
              <TableHead>
                <TableRow>
                  <TableCell>
                    {renderSortLabel('name', t('wordlists.columns.name') as string)}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('verification_status', t('wordlists.columns.status') as string)}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('wordlist_type', t('wordlists.columns.type') as string)}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('file_size', t('wordlists.columns.size') as string)}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('word_count', t('wordlists.columns.wordCount') as string)}
                  </TableCell>
                  <TableCell>
                    {renderSortLabel('updated_at', t('wordlists.columns.updated') as string)}
                  </TableCell>
                  <TableCell align="right">{t('wordlists.columns.actions') as string}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center" sx={{ py: 3 }}>
                      <CircularProgress size={40} />
                      <Typography variant="body2" sx={{ mt: 1 }}>
                        {t('wordlists.loading') as string}
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : filteredWordlists.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center" sx={{ py: 3 }}>
                      <Typography variant="body1">
                        {t('wordlists.noWordlistsFound') as string}
                      </Typography>
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                        {searchTerm ? t('wordlists.tryDifferentSearch') as string : t('wordlists.uploadToGetStarted') as string}
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredWordlists.map((wordlist) => (
                    <TableRow key={wordlist.id}>
                      <TableCell>
                        <Box>
                          <Typography variant="body2" fontWeight="medium">
                            {wordlist.name}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {wordlist.description || t('wordlists.noDescription') as string}
                          </Typography>
                        </Box>
                      </TableCell>
                      <TableCell>
                        {renderStatusChip(wordlist.verification_status)}
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={wordlist.wordlist_type}
                          size="small"
                          color="primary"
                          variant="outlined"
                          sx={{ textTransform: 'capitalize' }}
                        />
                      </TableCell>
                      <TableCell>
                        {formatFileSize(wordlist.file_size)}
                      </TableCell>
                      <TableCell>
                        {wordlist.word_count.toLocaleString()}
                      </TableCell>
                      <TableCell>
                        {new Date(wordlist.updated_at).toLocaleDateString()}
                      </TableCell>
                      <TableCell align="right">
                        {wordlist.is_potfile && (
                          <Tooltip title={t('wordlists.tooltips.refreshMetadata') as string}>
                            <IconButton
                              onClick={() => handleRefreshWordlist(wordlist.id)}
                              color="primary"
                            >
                              <RefreshIcon />
                            </IconButton>
                          </Tooltip>
                        )}
                        <Tooltip title={t('wordlists.tooltips.download') as string}>
                          <IconButton
                            onClick={() => handleDownload(wordlist.id, wordlist.name)}
                            disabled={wordlist.verification_status !== 'verified'}
                          >
                            <DownloadIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title={t('wordlists.tooltips.edit') as string}>
                          <IconButton
                            onClick={() => handleEditClick(wordlist)}
                          >
                            <EditIcon />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title={t('wordlists.tooltips.delete') as string}>
                          <IconButton
                            color="error"
                            onClick={() => openDeleteDialog(wordlist.id, wordlist.name)}
                          >
                            <DeleteIcon />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
        </Paper>

      {/* Upload Dialog */}
      <Dialog
        open={uploadDialogOpen}
        onClose={() => !isLoading && setUploadDialogOpen(false)}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>{t('wordlists.dialogs.upload.title') as string}</DialogTitle>
        <DialogContent>
          <FileUpload
            title={t('wordlists.dialogs.upload.fileUploadTitle') as string}
            description={t('wordlists.dialogs.upload.fileUploadDescription') as string}
            acceptedFileTypes=".txt,.dict,.dic,.lst,.wordlist,.wl,.gz,.zip,text/plain,application/gzip,application/zip"
            onUpload={handleUploadWordlist}
            uploadButtonText={t('wordlists.uploadWordlist') as string}
            additionalFields={
              <FormControl fullWidth margin="normal">
                <InputLabel id="wordlist-type-label">{t('wordlists.fields.wordlistType') as string}</InputLabel>
                <Select
                  labelId="wordlist-type-label"
                  id="wordlist-type"
                  name="wordlist_type"
                  value={selectedWordlistType}
                  onChange={(e) => setSelectedWordlistType(e.target.value as WordlistType)}
                  label={t('wordlists.fields.wordlistType') as string}
                >
                  <MenuItem value={WordlistType.GENERAL}>{t('wordlists.types.general') as string}</MenuItem>
                  <MenuItem value={WordlistType.SPECIALIZED}>{t('wordlists.types.specialized') as string}</MenuItem>
                  <MenuItem value={WordlistType.TARGETED}>{t('wordlists.types.targeted') as string}</MenuItem>
                  <MenuItem value={WordlistType.CUSTOM}>{t('wordlists.types.custom') as string}</MenuItem>
                </Select>
              </FormControl>
            }
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadDialogOpen(false)} color="primary" disabled={isLoading}>
            {t('common.cancel') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog
        open={openEditDialog}
        onClose={() => setOpenEditDialog(false)}
        aria-labelledby="edit-dialog-title"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="edit-dialog-title">{t('wordlists.dialogs.edit.title') as string}</DialogTitle>
        <DialogContent>
          <TextField
            margin="dense"
            label={t('wordlists.fields.name') as string}
            fullWidth
            value={nameEdit}
            onChange={(e) => setNameEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <TextField
            margin="dense"
            label={t('wordlists.fields.description') as string}
            fullWidth
            multiline
            rows={3}
            value={descriptionEdit}
            onChange={(e) => setDescriptionEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <FormControl fullWidth margin="dense" sx={{ mb: 2 }}>
            <InputLabel id="edit-wordlist-type-label">{t('wordlists.fields.wordlistType') as string}</InputLabel>
            <Select
              labelId="edit-wordlist-type-label"
              id="edit-wordlist-type"
              value={wordlistTypeEdit}
              onChange={(e) => setWordlistTypeEdit(e.target.value as WordlistType)}
              label={t('wordlists.fields.wordlistType') as string}
            >
              <MenuItem value={WordlistType.GENERAL}>{t('wordlists.types.general') as string}</MenuItem>
              <MenuItem value={WordlistType.SPECIALIZED}>{t('wordlists.types.specialized') as string}</MenuItem>
              <MenuItem value={WordlistType.TARGETED}>{t('wordlists.types.targeted') as string}</MenuItem>
              <MenuItem value={WordlistType.CUSTOM}>{t('wordlists.types.custom') as string}</MenuItem>
            </Select>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpenEditDialog(false)}>
            {t('common.cancel') as string}
          </Button>
          <Button onClick={handleSaveEdit} variant="contained" color="primary">
            {t('common.saveChanges') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog
        open={deleteDialogOpen}
        onClose={closeDeleteDialog}
        aria-labelledby="delete-dialog-title"
        aria-describedby="delete-dialog-description"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="delete-dialog-title">
          {deletionImpact?.has_cascading_impact ? t('wordlists.dialogs.delete.cascadeTitle') as string : t('wordlists.dialogs.delete.title') as string}
        </DialogTitle>
        <DialogContent>
          {isCheckingImpact ? (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', py: 3 }}>
              <CircularProgress size={24} sx={{ mr: 2 }} />
              <Typography>{t('wordlists.dialogs.delete.checkingDependencies') as string}</Typography>
            </Box>
          ) : deletionImpact?.has_cascading_impact ? (
            <Box>
              <Alert severity="warning" sx={{ mb: 2 }}>
                {t('wordlists.dialogs.delete.cascadeWarning', { name: wordlistToDelete?.name }) as string}
              </Alert>

              {deletionImpact.summary.total_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('wordlists.dialogs.delete.jobsCount', { count: deletionImpact.summary.total_jobs }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.jobs.slice(0, 5).map((job) => (
                      <li key={job.id}>
                        <Typography variant="body2" color="text.secondary">
                          {job.name} ({job.status}) - {job.hashlist_name || t('wordlists.dialogs.delete.noHashlist') as string}
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('wordlists.dialogs.delete.andMore', { count: deletionImpact.summary.total_jobs - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_preset_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('wordlists.dialogs.delete.presetJobsCount', { count: deletionImpact.summary.total_preset_jobs }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.preset_jobs.slice(0, 5).map((pj) => (
                      <li key={pj.id}>
                        <Typography variant="body2" color="text.secondary">
                          {pj.name} ({formatAttackMode(pj.attack_mode)})
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_preset_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('wordlists.dialogs.delete.andMore', { count: deletionImpact.summary.total_preset_jobs - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflow_steps > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('wordlists.dialogs.delete.workflowStepsCount', { count: deletionImpact.summary.total_workflow_steps }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflow_steps.slice(0, 5).map((step, idx) => (
                      <li key={`${step.workflow_id}-${step.step_order}-${idx}`}>
                        <Typography variant="body2" color="text.secondary">
                          {step.workflow_name} → {t('wordlists.dialogs.delete.step', { order: step.step_order }) as string} ({step.preset_job_name})
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_workflow_steps > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('wordlists.dialogs.delete.andMore', { count: deletionImpact.summary.total_workflow_steps - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflows_to_delete > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('wordlists.dialogs.delete.emptyWorkflowsCount', { count: deletionImpact.summary.total_workflows_to_delete }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflows_to_delete.map((wf) => (
                      <li key={wf.id}>
                        <Typography variant="body2" color="text.secondary">
                          {wf.name}
                        </Typography>
                      </li>
                    ))}
                  </Box>
                </Box>
              )}

              <Divider sx={{ my: 2 }} />

              <Typography variant="body2" sx={{ mb: 1 }}>
                {t('wordlists.dialogs.delete.confirmationPrompt', { id: deletionImpact.resource_id }) as string}
              </Typography>
              <TextField
                fullWidth
                size="small"
                placeholder={t('wordlists.dialogs.delete.confirmationPlaceholder', { id: deletionImpact.resource_id }) as string}
                value={confirmationId}
                onChange={(e) => setConfirmationId(e.target.value)}
                error={confirmationId !== '' && !isConfirmationValid()}
                helperText={confirmationId !== '' && !isConfirmationValid() ? t('wordlists.dialogs.delete.idMismatch') as string : ''}
              />
            </Box>
          ) : (
            <Typography variant="body1" id="delete-dialog-description">
              {t('wordlists.dialogs.delete.confirmation', { name: wordlistToDelete?.name }) as string}
            </Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={closeDeleteDialog}>{t('common.cancel') as string}</Button>
          <Button
            onClick={() => {
              if (wordlistToDelete) {
                const confirmId = deletionImpact?.has_cascading_impact ? Number(confirmationId) : undefined;
                handleDelete(wordlistToDelete.id, wordlistToDelete.name, confirmId);
              }
            }}
            color="error"
            variant="contained"
            disabled={isCheckingImpact || (deletionImpact?.has_cascading_impact && !isConfirmationValid())}
          >
            {deletionImpact?.has_cascading_impact ? t('wordlists.dialogs.delete.deleteAll') as string : t('common.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
