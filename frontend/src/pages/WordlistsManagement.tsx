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
import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Trans, useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Typography,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  TextField,
  MenuItem,
  Divider,
  CircularProgress,
  Alert,
  Tooltip,
  FormControl,
  InputLabel,
  Select,
  Autocomplete
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Edit as EditIcon,
  Refresh as RefreshIcon,
  CloudDownload as DownloadIcon,
  Add as AddIcon,
  FilterAlt as FilterAltIcon,
  Autorenew as AutorenewIcon
} from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import FileUpload from '../components/common/FileUpload';
import { Wordlist, WordlistType, DeletionImpact, WordlistFilter } from '../types/wordlists';
import * as wordlistService from '../services/wordlists';
import FilterCriteriaForm, { isFilterEmpty } from '../components/wordlists/FilterCriteriaForm';
import { DataTable, EntityLink, PageHeader, StatusChip, useToast } from '../components/ui';
import { formatFileSize, formatAttackMode } from '../utils/formatters';

const TYPE_FILTERS: { value: '' | WordlistType; labelKey: string }[] = [
  { value: '', labelKey: 'wordlists.tabs.all' },
  { value: WordlistType.GENERAL, labelKey: 'wordlists.tabs.general' },
  { value: WordlistType.SPECIALIZED, labelKey: 'wordlists.tabs.specialized' },
  { value: WordlistType.TARGETED, labelKey: 'wordlists.tabs.targeted' },
  { value: WordlistType.CUSTOM, labelKey: 'wordlists.tabs.custom' },
];

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
  const [typeFilter, setTypeFilter] = useState<'' | WordlistType>('');
  const toast = useToast();
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [selectedWordlistType, setSelectedWordlistType] = useState<WordlistType>(WordlistType.GENERAL);
  const [isLoading, setIsLoading] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [wordlistToDelete, setWordlistToDelete] = useState<{id: string, name: string} | null>(null);
  const [deletionImpact, setDeletionImpact] = useState<DeletionImpact | null>(null);
  const [confirmationId, setConfirmationId] = useState('');
  const [isCheckingImpact, setIsCheckingImpact] = useState(false);

  // Filtered (derived) wordlist creation (GH #40)
  const [filterDialogOpen, setFilterDialogOpen] = useState(false);
  // Manual full-regeneration confirmation (GH #40 follow-up)
  const [regenConfirm, setRegenConfirm] = useState<Wordlist | null>(null);
  const [filterParent, setFilterParent] = useState<Wordlist | null>(null);
  const [filterName, setFilterName] = useState('');
  const [filterCriteria, setFilterCriteria] = useState<WordlistFilter>({});
  const [creatingFilter, setCreatingFilter] = useState(false);

  const openFilterDialog = () => {
    setFilterParent(null);
    setFilterName('');
    setFilterCriteria({});
    setFilterDialogOpen(true);
  };

  const handleCreateFilteredWordlist = async () => {
    if (!filterParent || isFilterEmpty(filterCriteria)) {
      return;
    }
    try {
      setCreatingFilter(true);
      await wordlistService.createFilteredWordlist({
        parent_wordlist_id: parseInt(filterParent.id, 10),
        name: filterName.trim() || (t('wordlists.filtered.namePlaceholder', { name: filterParent.name }) as string),
        filter: filterCriteria,
      });
      toast.success(t('wordlists.filtered.messages.generating') as string);
      setFilterDialogOpen(false);
      fetchWordlists();
    } catch (err: any) {
      const msg = err?.response?.data?.error || (t('wordlists.filtered.messages.createFailed') as string);
      toast.error(msg);
    } finally {
      setCreatingFilter(false);
    }
  };

  // Describe a wordlist's on-disk format for the source picker so otherwise
  // identical-looking lists (same word count) can be told apart (GH #40).
  const wordlistExtension = (w: Wordlist): string => {
    const base = (w.file_name || w.name).split('/').pop() || '';
    const dot = base.lastIndexOf('.');
    return dot >= 0 ? base.slice(dot + 1).toLowerCase() : '';
  };
  const isCompressedWordlist = (w: Wordlist): boolean => {
    return w.format === 'compressed' || ['gz', 'zip', '7z', 'bz2', 'xz'].includes(wordlistExtension(w));
  };

  const handleRegenerateFiltered = async (id: string) => {
    try {
      await wordlistService.regenerateFilteredWordlist(id);
      toast.info(t('wordlists.filtered.messages.regenerating') as string);
      fetchWordlists();
    } catch (err) {
      toast.error(t('wordlists.filtered.messages.regenerateFailed') as string);
    }
  };

  // The manual Regenerate button forces a FULL rebuild, so confirm intent first.
  const confirmRegenerateFiltered = async () => {
    if (!regenConfirm) return;
    const id = regenConfirm.id;
    setRegenConfirm(null);
    await handleRegenerateFiltered(id);
  };

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
      toast.error(t('wordlists.errors.loadFailed') as string);
    } finally {
      setLoading(false);
    }
  }, [toast, t]);

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
          toast.info(t('wordlists.messages.duplicateWordlist', { name: response.data.name }) as string);
        } else {
          toast.success(t('wordlists.messages.uploadSuccess') as string);
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
      toast.error(t('wordlists.errors.uploadFailed') as string);
    } finally {
      setIsLoading(false);
    }
  };

  // Handle wordlist deletion
  const handleDelete = async (id: string, name: string, confirmId?: number) => {
    try {
      await wordlistService.deleteWordlist(id, confirmId);
      toast.success(t('wordlists.messages.deleteSuccess', { name }) as string);
      fetchWordlists();
    } catch (err: any) {
      console.error('Error deleting wordlist:', err);
      // Extract error message from axios response
      const errorMessage = err.response?.data?.error || t('wordlists.errors.deleteFailed') as string;
      toast.error(errorMessage);
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
      toast.error(t('wordlists.errors.downloadFailed') as string);
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
      toast.success(t('wordlists.messages.refreshSuccess') as string);
      // Refresh the wordlist data
      fetchWordlists();
    } catch (err: any) {
      console.error('Error refreshing wordlist:', err);
      const errorMessage = err.response?.data?.error || t('wordlists.errors.refreshFailed') as string;
      toast.error(errorMessage);
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
      toast.success(t('wordlists.messages.updateSuccess') as string);
      setOpenEditDialog(false);
      fetchWordlists();
    } catch (err: any) {
      console.error('[Wordlist Edit] Error updating wordlist:', err);

      if (err.response?.status === 401) {
        toast.error(t('wordlists.errors.sessionExpired') as string);
      } else {
        toast.error(t('wordlists.errors.updateFailed', { error: err.response?.data?.message || err.message }) as string);
      }
    }
  };

  // Filter by search term and type tab (sorting is handled by the table)
  const filteredWordlists = useMemo(() => {
    const term = searchTerm.toLowerCase();
    return wordlists.filter((wordlist) => {
      const matchesSearch = wordlist.name.toLowerCase().includes(term) ||
        (wordlist.description || '').toLowerCase().includes(term);
      return matchesSearch && (!typeFilter || wordlist.wordlist_type === typeFilter);
    });
  }, [wordlists, searchTerm, typeFilter]);

  // Render status chip based on verification status
  const renderStatusChip = (status: string, missingSince?: string) => {
    // A file-gone failure (GH #93) is actionable in a way a generic verification
    // failure is not, so label it distinctly when missing_since is set.
    if (status === 'failed' && missingSince) {
      return (
        <StatusChip
          entity="verification"
          status={status}
          label={t('wordlists.status.missing') as string}
          tooltip={t('wordlists.status.missingSince', { date: new Date(missingSince).toLocaleString() }) as string}
        />
      );
    }
    return <StatusChip entity="verification" status={status} />;
  };

  const columns: GridColDef<Wordlist>[] = [
    {
      field: 'name',
      headerName: t('wordlists.columns.name') as string,
      flex: 2,
      minWidth: 240,
      renderCell: (p) => {
        const wordlist = p.row;
        return (
          <Box sx={{ py: 1, minWidth: 0 }}>
            <Typography variant="body2" fontWeight="medium" component="div">
              {wordlist.name}
              {wordlist.parent_wordlist_id && (
                <Chip label={t('wordlists.filtered.badge') as string} size="small" color="info" variant="outlined" sx={{ ml: 1 }} />
              )}
              {wordlist.parent_wordlist_id && wordlist.verification_status === 'pending' && (
                <Tooltip title={t('wordlists.filtered.regeneratingTooltip') as string}>
                  <Chip label={t('wordlists.filtered.regeneratingBadge') as string} size="small" color="info" sx={{ ml: 1 }} />
                </Tooltip>
              )}
              {wordlist.is_stale && wordlist.verification_status !== 'pending' && (
                <Tooltip title={t('wordlists.filtered.staleTooltip') as string}>
                  <Chip label={t('wordlists.filtered.staleBadge') as string} size="small" color="warning" sx={{ ml: 1 }} />
                </Tooltip>
              )}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {wordlist.description || t('wordlists.noDescription') as string}
            </Typography>
          </Box>
        );
      },
    },
    {
      field: 'verification_status',
      headerName: t('wordlists.columns.status') as string,
      width: 150,
      renderCell: (p) => renderStatusChip(p.row.verification_status, p.row.missing_since),
    },
    {
      field: 'wordlist_type',
      headerName: t('wordlists.columns.type') as string,
      width: 130,
      renderCell: (p) => (
        <Chip
          label={p.row.wordlist_type}
          size="small"
          color="primary"
          variant="outlined"
          sx={{ textTransform: 'capitalize' }}
        />
      ),
    },
    {
      field: 'file_size',
      headerName: t('wordlists.columns.size') as string,
      type: 'number',
      width: 110,
      valueFormatter: (v) => formatFileSize(Number(v) || 0),
    },
    {
      field: 'word_count',
      headerName: t('wordlists.columns.wordCount') as string,
      type: 'number',
      width: 130,
      valueFormatter: (v) => (Number(v) || 0).toLocaleString(),
    },
    {
      field: 'updated_at',
      headerName: t('wordlists.columns.updated') as string,
      width: 130,
      valueGetter: (_v, row) => (row.updated_at ? new Date(row.updated_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleDateString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('wordlists.title') as string}
        description={t('wordlists.description') as string}
        actions={
          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => setUploadDialogOpen(true)}
              disabled={isLoading}
            >
              {t('wordlists.uploadWordlist') as string}
            </Button>
            <Button
              variant="outlined"
              startIcon={<FilterAltIcon />}
              onClick={openFilterDialog}
              disabled={isLoading}
            >
              Filtered Wordlist
            </Button>
            <Button
              variant="outlined"
              startIcon={<RefreshIcon />}
              onClick={() => fetchWordlists()}
            >
              {t('wordlists.refresh') as string}
            </Button>
          </Box>
        }
      />

      <DataTable<Wordlist>
        rows={filteredWordlists}
        columns={columns}
        getRowId={(r) => r.id}
        loading={loading && wordlists.length === 0}
        fetching={loading && wordlists.length > 0}
        error={error && wordlists.length === 0 ? error : undefined}
        onRetry={() => fetchWordlists()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client', initial: [{ field: 'updated_at', sort: 'desc' }] }}
        toolbar={{
          search: {
            value: searchTerm,
            onChange: setSearchTerm,
            placeholder: t('wordlists.searchPlaceholder') as string,
            debounceMs: 150,
          },
          filters: (
            <FormControl size="small" sx={{ minWidth: 180 }}>
              <InputLabel id="wordlist-type-filter-label">{t('wordlists.columns.type') as string}</InputLabel>
              <Select
                labelId="wordlist-type-filter-label"
                value={typeFilter}
                label={t('wordlists.columns.type') as string}
                onChange={(e) => setTypeFilter(e.target.value as '' | WordlistType)}
              >
                {TYPE_FILTERS.map((f) => (
                  <MenuItem key={f.value || 'all'} value={f.value}>
                    {t(f.labelKey) as string}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          ),
        }}
        rowActionsInlineLimit={4}
        rowActions={(wordlist) => [
          {
            key: 'refresh',
            label: t('wordlists.tooltips.refreshMetadata') as string,
            icon: <RefreshIcon fontSize="small" />,
            hidden: !wordlist.is_potfile,
            onClick: (w) => handleRefreshWordlist(w.id),
          },
          {
            key: 'regenerate',
            label: 'Regenerate',
            tooltip: 'Force a full regenerate from the parent wordlist. Filtered wordlists already regenerate automatically when their parent changes.',
            icon: <AutorenewIcon fontSize="small" color={wordlist.is_stale ? 'warning' : undefined} />,
            hidden: !wordlist.parent_wordlist_id,
            disabled: wordlist.verification_status === 'pending',
            onClick: (w) => setRegenConfirm(w),
          },
          {
            key: 'download',
            label: t('wordlists.tooltips.download') as string,
            icon: <DownloadIcon fontSize="small" />,
            disabled: wordlist.verification_status !== 'verified',
            onClick: (w) => handleDownload(w.id, w.name),
          },
          {
            key: 'edit',
            label: t('wordlists.tooltips.edit') as string,
            icon: <EditIcon fontSize="small" />,
            onClick: (w) => handleEditClick(w),
          },
          {
            key: 'delete',
            label: t('wordlists.tooltips.delete') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            onClick: (w) => openDeleteDialog(w.id, w.name),
          },
        ]}
        emptyState={{
          title: t('wordlists.noWordlistsFound') as string,
          description: searchTerm ? t('wordlists.tryDifferentSearch') as string : t('wordlists.uploadToGetStarted') as string,
        }}
        tableKey="wordlists"
      />

      {/* Create Filtered Wordlist Dialog (GH #40) */}
      <Dialog
        open={filterDialogOpen}
        onClose={() => !creatingFilter && setFilterDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('wordlists.filtered.createDialog.title') as string}</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('wordlists.filtered.createDialog.description') as string}
          </Typography>
          <Autocomplete
            options={wordlists.filter((w) => !w.parent_wordlist_id && !w.is_potfile && w.verification_status === 'verified')}
            getOptionLabel={(w) => {
              const ext = wordlistExtension(w);
              return `${w.name} — ${t('wordlists.filtered.wordCount', { count: w.word_count })}${ext ? ` · .${ext}` : ''}`;
            }}
            value={filterParent}
            onChange={(_, v) => setFilterParent(v)}
            renderOption={(props, w) => (
              <li {...props} key={w.id}>
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                  <Box>
                    <Typography variant="body2">{w.name}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {t('wordlists.filtered.wordCount', { count: w.word_count })} · {formatFileSize(w.file_size)}
                    </Typography>
                  </Box>
                  {isCompressedWordlist(w) ? (
                    <Chip size="small" color="warning" variant="outlined" label={`${t('wordlists.filtered.compressedChip')}${wordlistExtension(w) ? ` (.${wordlistExtension(w)})` : ''}`} />
                  ) : (
                    <Chip size="small" variant="outlined" label={t('wordlists.filtered.plaintextChip') as string} />
                  )}
                </Box>
              </li>
            )}
            renderInput={(params) => (
              <TextField {...params} label={t('wordlists.filtered.createDialog.sourceWordlistLabel') as string} margin="normal" required />
            )}
          />
          <TextField
            label={t('wordlists.filtered.createDialog.nameLabel') as string}
            fullWidth
            margin="normal"
            value={filterName}
            onChange={(e) => setFilterName(e.target.value)}
            placeholder={filterParent ? (t('wordlists.filtered.namePlaceholder', { name: filterParent.name }) as string) : ''}
          />
          <Box sx={{ mt: 1 }}>
            <FilterCriteriaForm
              parentWordlistId={filterParent ? parseInt(filterParent.id, 10) : undefined}
              value={filterCriteria}
              onChange={setFilterCriteria}
              showPreview
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setFilterDialogOpen(false)} disabled={creatingFilter}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="contained"
            onClick={handleCreateFilteredWordlist}
            disabled={creatingFilter || !filterParent || isFilterEmpty(filterCriteria)}
            startIcon={creatingFilter ? <CircularProgress size={16} /> : <FilterAltIcon />}
          >
            {t('wordlists.filtered.createDialog.create')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Manual full-regeneration confirmation (GH #40 follow-up) */}
      <Dialog
        open={regenConfirm !== null}
        onClose={() => setRegenConfirm(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('wordlists.filtered.regenerateDialog.title') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText component="div">
            <Typography variant="body2" gutterBottom>
              <Trans t={t} i18nKey="wordlists.filtered.regenerateDialog.autoInfo" components={{ strong: <strong /> }} />
            </Typography>
            <Typography variant="body2" gutterBottom>
              <Trans t={t} i18nKey="wordlists.filtered.regenerateDialog.manualHint" components={{ strong: <strong /> }} />
            </Typography>
            <Typography variant="body2" color="warning.main">
              {regenConfirm ? (
                <Trans
                  t={t}
                  i18nKey="wordlists.filtered.regenerateDialog.warningWithExample"
                  values={{ name: regenConfirm.name }}
                  components={{ strong: <strong /> }}
                />
              ) : (
                <Trans t={t} i18nKey="wordlists.filtered.regenerateDialog.warningNoExample" components={{ strong: <strong /> }} />
              )}
            </Typography>
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRegenConfirm(null)}>{t('common.cancel')}</Button>
          <Button variant="contained" color="warning" startIcon={<AutorenewIcon />} onClick={confirmRegenerateFiltered}>
            {t('wordlists.filtered.regenerateDialog.confirmButton')}
          </Button>
        </DialogActions>
      </Dialog>

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
                          <EntityLink type="job" id={job.id} label={job.name} /> ({job.status}) -{' '}
                          {job.hashlist_name ? (
                            <EntityLink type="hashlist" id={job.hashlist_id ?? undefined} label={job.hashlist_name} />
                          ) : (
                            t('wordlists.dialogs.delete.noHashlist') as string
                          )}
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
                          <EntityLink type="preset_job" id={pj.id} label={pj.name} /> ({formatAttackMode(pj.attack_mode)})
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
                          <EntityLink type="workflow" id={step.workflow_id} label={step.workflow_name} /> → {t('wordlists.dialogs.delete.step', { order: step.step_order }) as string} (<EntityLink type="preset_job" id={step.preset_job_id} label={step.preset_job_name} />)
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
                          <EntityLink type="workflow" id={wf.id} label={wf.name} />
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
