import React, { useState, useRef } from 'react';
import {
  Box,
  Typography,
  Button,
  CircularProgress,
  Alert,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  ToggleButtonGroup,
  ToggleButton,
  FormControlLabel,
  Checkbox,
  Tooltip,
} from '@mui/material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Add as AddIcon, Edit as EditIcon, Delete as DeleteIcon, UploadFile as UploadFileIcon } from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { DataTable, PageHeader, useConfirm, useToast } from '../../components/ui';
import { useTranslation } from 'react-i18next';
import { CustomCharset, CustomCharsetFormData } from '../../types/customCharsets';
import {
  listGlobalCharsets,
  createGlobalCharset,
  uploadGlobalCharsetFile,
  updateGlobalCharset,
  deleteGlobalCharset,
} from '../../services/customCharsetService';
import { validateCharsetDefinition, validateHexCharsetDefinition } from '../../utils/charsetUtils';

const CustomCharsetListPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Dialog state
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingCharset, setEditingCharset] = useState<CustomCharset | null>(null);
  const [createMode, setCreateMode] = useState<'inline' | 'file'>('inline');
  const [formData, setFormData] = useState<CustomCharsetFormData>({
    name: '',
    description: '',
    definition: '',
  });
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const { data: charsets, isLoading, isFetching, error, refetch } = useQuery<CustomCharset[], Error>({
    queryKey: ['globalCharsets'],
    queryFn: listGlobalCharsets,
  });

  const createMutation = useMutation<CustomCharset, Error, CustomCharsetFormData>({
    mutationFn: createGlobalCharset,
    onSuccess: () => {
      toast.success(t('customCharsets.messages.createSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['globalCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const uploadMutation = useMutation<CustomCharset, Error, FormData>({
    mutationFn: uploadGlobalCharsetFile,
    onSuccess: () => {
      toast.success(t('customCharsets.messages.uploadSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['globalCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const updateMutation = useMutation<CustomCharset, Error, { id: string; data: CustomCharsetFormData }>({
    mutationFn: ({ id, data }) => updateGlobalCharset(id, data),
    onSuccess: () => {
      toast.success(t('customCharsets.messages.updateSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['globalCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const deleteMutation = useMutation<void, Error, string>({
    mutationFn: deleteGlobalCharset,
    onSuccess: () => {
      toast.success(t('customCharsets.messages.deleteSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['globalCharsets'] });
    },
    onError: (err: Error) => {
      toast.error(t('customCharsets.errors.deleteFailed', { message: err.message }) as string);
    },
  });

  const handleOpenCreate = () => {
    setEditingCharset(null);
    setCreateMode('inline');
    setFormData({ name: '', description: '', definition: '', is_hex: false });
    setSelectedFile(null);
    setFormError(null);
    setDialogOpen(true);
  };

  const handleOpenEdit = (charset: CustomCharset) => {
    setEditingCharset(charset);
    setCreateMode(charset.charset_type === 'file' ? 'file' : 'inline');
    setFormData({
      name: charset.name,
      description: charset.description,
      definition: charset.definition || '',
      is_hex: charset.is_hex || false,
    });
    setSelectedFile(null);
    setFormError(null);
    setDialogOpen(true);
  };

  const handleCloseDialog = () => {
    setDialogOpen(false);
    setEditingCharset(null);
    setFormData({ name: '', description: '', definition: '', is_hex: false });
    setSelectedFile(null);
    setFormError(null);
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      if (!file.name.endsWith('.hcchr')) {
        setFormError(t('customCharsets.errors.onlyHcchrAllowed') as string);
        return;
      }
      if (file.size > 1023) {
        setFormError(t('customCharsets.errors.fileTooLarge') as string);
        return;
      }
      setSelectedFile(file);
      setFormError(null);
      // Auto-fill name from filename if empty
      if (!formData.name) {
        setFormData(prev => ({ ...prev, name: file.name.replace('.hcchr', '') }));
      }
    }
  };

  const handleSubmit = () => {
    if (!formData.name.trim()) {
      setFormError(t('customCharsets.errors.nameRequired') as string);
      return;
    }

    if (editingCharset) {
      // Edit mode — only name/description for file charsets
      if (editingCharset.charset_type === 'file') {
        updateMutation.mutate({ id: editingCharset.id, data: { ...formData, definition: '' } });
      } else {
        if (!formData.definition.trim()) {
          setFormError(t('customCharsets.errors.definitionRequired') as string);
          return;
        }
        const validationError = validateCharsetDefinition(formData.definition);
        if (validationError) {
          setFormError(validationError);
          return;
        }
        updateMutation.mutate({ id: editingCharset.id, data: formData });
      }
    } else if (createMode === 'file') {
      // File upload
      if (!selectedFile) {
        setFormError(t('customCharsets.errors.selectFile') as string);
        return;
      }
      const fd = new FormData();
      fd.append('name', formData.name.trim());
      fd.append('description', formData.description.trim());
      fd.append('file', selectedFile);
      uploadMutation.mutate(fd);
    } else {
      // Inline create
      if (!formData.definition.trim()) {
        setFormError(t('customCharsets.errors.definitionRequired') as string);
        return;
      }
      const validationError = formData.is_hex
        ? validateHexCharsetDefinition(formData.definition)
        : validateCharsetDefinition(formData.definition);
      if (validationError) {
        setFormError(validationError);
        return;
      }
      createMutation.mutate(formData);
    }
  };

  const handleDelete = async (charset: CustomCharset) => {
    const ok = await confirm({
      title: t('customCharsets.dialogs.delete.title') as string,
      message: t('customCharsets.dialogs.delete.confirmation', { name: charset.name }) as string,
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });
    if (ok) deleteMutation.mutate(charset.id);
  };

  const isMutating = createMutation.isPending || updateMutation.isPending || uploadMutation.isPending;

  const renderCharsetValue = (charset: CustomCharset) => {
    if (charset.charset_type === 'file') {
      return (
        <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
          <Chip label={t('customCharsets.typeLabels.file') as string} size="small" color="info" />
          <Chip label={t('customCharsets.uniqueBytes', { count: charset.byte_count }) as string} size="small" variant="outlined" sx={{ fontFamily: (theme) => theme.typography.monoFamily }} />
        </Box>
      );
    }
    return (
      <Chip label={charset.definition} size="small" variant="outlined" sx={{ fontFamily: (theme) => theme.typography.monoFamily }} />
    );
  };

  const columns: GridColDef<CustomCharset>[] = [
    { field: 'name', headerName: t('customCharsets.columns.name') as string, flex: 1, minWidth: 160 },
    {
      field: 'charset_type',
      headerName: t('customCharsets.columns.type') as string,
      width: 140,
      renderCell: (p) => (
        <Box sx={{ display: 'flex', gap: 0.5 }}>
          <Chip
            label={p.row.charset_type === 'file' ? (t('customCharsets.typeLabels.file') as string) : (t('customCharsets.typeLabels.inline') as string)}
            size="small"
            color={p.row.charset_type === 'file' ? 'info' : 'default'}
          />
          {p.row.is_hex && <Chip label={t('customCharsets.typeLabels.hex') as string} size="small" color="warning" />}
        </Box>
      ),
    },
    {
      field: 'definition',
      headerName: t('customCharsets.columns.definition') as string,
      flex: 1.2,
      minWidth: 200,
      sortable: false,
      renderCell: (p) => renderCharsetValue(p.row),
    },
    {
      field: 'description',
      headerName: t('customCharsets.columns.description') as string,
      flex: 1.5,
      minWidth: 200,
      valueFormatter: (v) => (v as string) || '—',
    },
    {
      field: 'created_at',
      headerName: t('customCharsets.columns.created') as string,
      width: 180,
      valueGetter: (_v, row) => (row.created_at ? new Date(row.created_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('customCharsets.title') as string}
        description={t('customCharsets.description') as string}
        actions={
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={handleOpenCreate}
            disabled={deleteMutation.isPending}
          >
            {t('customCharsets.createCharset')}
          </Button>
        }
      />

      <DataTable<CustomCharset>
        rows={charsets ?? []}
        columns={columns}
        getRowId={(r) => r.id}
        loading={isLoading}
        fetching={(isFetching && !isLoading) || deleteMutation.isPending}
        error={error ? new Error(t('customCharsets.errors.loadFailed', { message: error.message }) as string) : undefined}
        onRetry={() => refetch()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client' }}
        rowActions={() => [
          {
            key: 'edit',
            label: t('customCharsets.rowActions.edit') as string,
            icon: <EditIcon fontSize="small" />,
            disabled: deleteMutation.isPending,
            onClick: (c) => handleOpenEdit(c),
          },
          {
            key: 'delete',
            label: t('customCharsets.rowActions.delete') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            disabled: deleteMutation.isPending,
            onClick: (c) => handleDelete(c),
          },
        ]}
        emptyState={{ title: t('customCharsets.empty') as string }}
        tableKey="admin-custom-charsets"
      />

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingCharset ? t('customCharsets.dialogs.edit.title') : t('customCharsets.dialogs.create.title')}
        </DialogTitle>
        <DialogContent>
          {formError && (
            <Alert severity="error" sx={{ mb: 2, mt: 1 }} onClose={() => setFormError(null)}>
              {formError}
            </Alert>
          )}

          {/* Mode toggle (only for create) */}
          {!editingCharset && (
            <Box sx={{ mb: 2, mt: 1 }}>
              <ToggleButtonGroup
                value={createMode}
                exclusive
                onChange={(_, v) => v && setCreateMode(v)}
                size="small"
              >
                <ToggleButton value="inline">{t('customCharsets.dialogs.modeToggle.inline')}</ToggleButton>
                <ToggleButton value="file">
                  <UploadFileIcon sx={{ mr: 0.5 }} fontSize="small" />
                  {t('customCharsets.dialogs.modeToggle.file')}
                </ToggleButton>
              </ToggleButtonGroup>
            </Box>
          )}

          <TextField
            autoFocus
            label={t('customCharsets.dialogs.fields.name')}
            value={formData.name}
            onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
            fullWidth
            margin="normal"
            required
            placeholder={t('customCharsets.dialogs.fields.namePlaceholder') as string}
          />

          {/* Inline definition field */}
          {(createMode === 'inline' && !editingCharset) || (editingCharset && editingCharset.charset_type !== 'file') ? (
            <>
              <TextField
                label={t('customCharsets.dialogs.fields.definition')}
                value={formData.definition}
                onChange={(e) => setFormData(prev => ({ ...prev, definition: e.target.value }))}
                fullWidth
                margin="normal"
                required
                placeholder={formData.is_hex ? (t('customCharsets.dialogs.fields.definitionPlaceholderHex') as string) : (t('customCharsets.dialogs.fields.definitionPlaceholderPlain') as string)}
                helperText={formData.is_hex
                  ? t('customCharsets.dialogs.fields.definitionHelperHex', { bytes: formData.definition ? (t('customCharsets.dialogs.fields.definitionHelperHexBytes', { count: Math.floor(formData.definition.length / 2) }) as string) : '' })
                  : t('customCharsets.dialogs.fields.definitionHelperPlain')}
                sx={{ '& input': { fontFamily: 'monospace' } }}
              />
              {!editingCharset && (
                <Tooltip title={t('customCharsets.dialogs.fields.hexTooltip') as string}>
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={formData.is_hex || false}
                        onChange={(e) => setFormData(prev => ({ ...prev, is_hex: e.target.checked }))}
                        size="small"
                      />
                    }
                    label={t('customCharsets.dialogs.fields.hexLabel') as string}
                  />
                </Tooltip>
              )}
            </>
          ) : null}

          {/* File upload field */}
          {createMode === 'file' && !editingCharset && (
            <Box sx={{ mt: 2, mb: 1 }}>
              <input
                ref={fileInputRef}
                type="file"
                accept=".hcchr"
                onChange={handleFileChange}
                style={{ display: 'none' }}
              />
              <Button
                variant="outlined"
                startIcon={<UploadFileIcon />}
                onClick={() => fileInputRef.current?.click()}
              >
                {selectedFile ? selectedFile.name : t('customCharsets.dialogs.fields.selectFile')}
              </Button>
              {selectedFile && (
                <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                  {t('customCharsets.dialogs.fields.fileSizeBytes', { count: selectedFile.size })}
                </Typography>
              )}
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                {t('customCharsets.dialogs.fields.fileHelperText')}
              </Typography>
            </Box>
          )}

          {/* File charset info (editing) */}
          {editingCharset && editingCharset.charset_type === 'file' && (
            <Alert severity="info" sx={{ mt: 2 }}>
              {t('customCharsets.dialogs.fields.fileCharsetInfo', { count: editingCharset.byte_count })}
            </Alert>
          )}

          <TextField
            label={t('customCharsets.dialogs.fields.description')}
            value={formData.description}
            onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
            fullWidth
            margin="normal"
            multiline
            rows={2}
            placeholder={t('customCharsets.dialogs.fields.descriptionPlaceholder') as string}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog} disabled={isMutating}>
            {t('common.cancel')}
          </Button>
          <Button
            onClick={handleSubmit}
            variant="contained"
            disabled={isMutating}
            startIcon={isMutating ? <CircularProgress size={20} /> : undefined}
          >
            {editingCharset ? t('customCharsets.dialogs.update') : createMode === 'file' ? t('customCharsets.dialogs.upload') : t('common.create')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CustomCharsetListPage;
