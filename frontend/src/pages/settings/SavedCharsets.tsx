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
import { useTranslation } from 'react-i18next';
import { DataTable, EntityLink, PageHeader, useConfirm, useToast } from '../../components/ui';
import { teamsService } from '../../services/teams';
import { CustomCharset, CustomCharsetFormData } from '../../types/customCharsets';
import {
  listAccessibleCharsets,
  createUserCharset,
  uploadUserCharsetFile,
  updateUserCharset,
  deleteUserCharset,
} from '../../services/customCharsetService';
import { validateCharsetDefinition, validateHexCharsetDefinition } from '../../utils/charsetUtils';

const SavedCharsetsPage: React.FC<{ embedded?: boolean }> = ({ embedded = false }) => {
  const { t } = useTranslation('settings');
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
    queryKey: ['accessibleCharsets'],
    queryFn: listAccessibleCharsets,
  });

  const createMutation = useMutation<CustomCharset, Error, CustomCharsetFormData>({
    mutationFn: createUserCharset,
    onSuccess: () => {
      toast.success(t('savedCharsets.messages.createSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['accessibleCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const uploadMutation = useMutation<CustomCharset, Error, FormData>({
    mutationFn: uploadUserCharsetFile,
    onSuccess: () => {
      toast.success(t('savedCharsets.messages.uploadSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['accessibleCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const updateMutation = useMutation<CustomCharset, Error, { id: string; data: CustomCharsetFormData }>({
    mutationFn: ({ id, data }) => updateUserCharset(id, data),
    onSuccess: () => {
      toast.success(t('savedCharsets.messages.updateSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['accessibleCharsets'] });
      handleCloseDialog();
    },
    onError: (err: Error) => {
      setFormError(err.message);
    },
  });

  const deleteMutation = useMutation<void, Error, string>({
    mutationFn: deleteUserCharset,
    onSuccess: () => {
      toast.success(t('savedCharsets.messages.deleteSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['accessibleCharsets'] });
    },
    onError: (err: Error) => {
      toast.error(t('savedCharsets.errors.deleteFailed', { message: err.message }) as string);
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
        setFormError(t('savedCharsets.errors.onlyHcchrAllowed') as string);
        return;
      }
      if (file.size > 1023) {
        setFormError(t('savedCharsets.errors.fileTooLarge') as string);
        return;
      }
      setSelectedFile(file);
      setFormError(null);
      if (!formData.name) {
        setFormData(prev => ({ ...prev, name: file.name.replace('.hcchr', '') }));
      }
    }
  };

  const handleSubmit = () => {
    if (!formData.name.trim()) {
      setFormError(t('savedCharsets.errors.nameRequired') as string);
      return;
    }

    if (editingCharset) {
      if (editingCharset.charset_type === 'file') {
        updateMutation.mutate({ id: editingCharset.id, data: { ...formData, definition: '' } });
      } else {
        if (!formData.definition.trim()) {
          setFormError(t('savedCharsets.errors.definitionRequired') as string);
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
      if (!selectedFile) {
        setFormError(t('savedCharsets.errors.selectFile') as string);
        return;
      }
      const fd = new FormData();
      fd.append('name', formData.name.trim());
      fd.append('description', formData.description.trim());
      fd.append('file', selectedFile);
      uploadMutation.mutate(fd);
    } else {
      if (!formData.definition.trim()) {
        setFormError(t('savedCharsets.errors.definitionRequired') as string);
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
    if (charset.scope === 'global') {
      toast.warning(t('savedCharsets.errors.globalDeleteForbidden') as string);
      return;
    }
    const ok = await confirm({
      title: t('savedCharsets.dialogs.delete.title') as string,
      message: t('savedCharsets.dialogs.delete.confirmation', { name: charset.name }) as string,
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });
    if (ok) deleteMutation.mutate(charset.id);
  };

  // Separate charsets by scope for display
  const globalCharsets = charsets?.filter(c => c.scope === 'global') || [];
  const userCharsets = charsets?.filter(c => c.scope === 'user') || [];
  const teamCharsets = charsets?.filter(c => c.scope === 'team') || [];

  // Team names for team-scoped charsets (owner_id is the team id); only fetched when needed.
  const { data: myTeams = [] } = useQuery({
    queryKey: ['teams', 'mine'],
    queryFn: () => teamsService.listUserTeams(),
    enabled: teamCharsets.length > 0,
    staleTime: 5 * 60 * 1000,
  });
  const teamName = (id?: string) => myTeams.find((tm) => tm.id === id)?.name;

  const isMutating = createMutation.isPending || updateMutation.isPending || uploadMutation.isPending;

  const renderCharsetValue = (charset: CustomCharset) => {
    if (charset.charset_type === 'file') {
      return (
        <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
          <Chip label={t('savedCharsets.typeLabels.file') as string} size="small" color="info" />
          <Chip label={t('savedCharsets.uniqueBytes', { count: charset.byte_count }) as string} size="small" variant="outlined" sx={{ fontFamily: (theme) => theme.typography.monoFamily }} />
        </Box>
      );
    }
    return (
      <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
        {charset.is_hex && <Chip label={t('savedCharsets.typeLabels.hex') as string} size="small" color="warning" />}
        <Chip
          label={charset.is_hex ? (t('savedCharsets.hexBytes', { count: Math.floor((charset.definition?.length || 0) / 2) }) as string) : charset.definition}
          size="small"
          variant="outlined"
          sx={{ fontFamily: (theme) => theme.typography.monoFamily }}
        />
      </Box>
    );
  };

  const buildColumns = (withTeam: boolean): GridColDef<CustomCharset>[] => [
    { field: 'name', headerName: t('savedCharsets.columns.name') as string, flex: 1, minWidth: 160 },
    ...(withTeam
      ? [
          {
            field: 'owner_id',
            headerName: t('savedCharsets.columns.team') as string,
            flex: 0.8,
            minWidth: 140,
            valueGetter: (_v: unknown, row: CustomCharset) => teamName(row.owner_id) || row.owner_id || '',
            renderCell: (p: { row: CustomCharset }) => (
              <EntityLink type="team" id={p.row.owner_id} label={teamName(p.row.owner_id) || (t('savedCharsets.columns.team') as string)} />
            ),
          } as GridColDef<CustomCharset>,
        ]
      : []),
    {
      field: 'definition',
      headerName: t('savedCharsets.columns.definition') as string,
      flex: 1.2,
      minWidth: 200,
      sortable: false,
      renderCell: (p) => renderCharsetValue(p.row),
    },
    {
      field: 'description',
      headerName: t('savedCharsets.columns.description') as string,
      flex: 1.5,
      minWidth: 200,
      valueFormatter: (v) => (v as string) || '—',
    },
    {
      field: 'created_at',
      headerName: t('savedCharsets.columns.created') as string,
      width: 180,
      valueGetter: (_v, row) => (row.created_at ? new Date(row.created_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleString() : ''),
    },
  ];

  const renderCharsetTable = (
    charsetList: CustomCharset[],
    showActions: boolean,
    title: string,
    tableKey: string,
    subtitle?: string,
    withTeam = false
  ) => (
    <Box sx={{ mb: 3 }}>
      <DataTable<CustomCharset>
        rows={charsetList}
        columns={buildColumns(withTeam)}
        getRowId={(r) => r.id}
        loading={isLoading}
        fetching={(isFetching && !isLoading) || deleteMutation.isPending}
        error={error ? new Error(`Failed to load charsets: ${error.message}`) : undefined}
        onRetry={() => refetch()}
        pagination={charsetList.length > 25 ? { mode: 'client', initialPageSize: 25 } : false}
        hideFooter={charsetList.length <= 25}
        sorting={{ mode: 'client' }}
        toolbar={{ title, subtitle }}
        rowActions={
          showActions
            ? () => [
                {
                  key: 'edit',
                  label: t('savedCharsets.rowActions.edit') as string,
                  icon: <EditIcon fontSize="small" />,
                  disabled: deleteMutation.isPending,
                  onClick: (c) => handleOpenEdit(c),
                },
                {
                  key: 'delete',
                  label: t('savedCharsets.rowActions.delete') as string,
                  icon: <DeleteIcon fontSize="small" />,
                  danger: true,
                  disabled: deleteMutation.isPending,
                  onClick: (c) => handleDelete(c),
                },
              ]
            : undefined
        }
        emptyState={{ title: t('savedCharsets.empty') as string }}
        tableKey={tableKey}
      />
    </Box>
  );

  const createButton = (
    <Button
      variant="contained"
      startIcon={<AddIcon />}
      onClick={handleOpenCreate}
      disabled={deleteMutation.isPending}
    >
      {t('savedCharsets.createCharset')}
    </Button>
  );

  return (
    <Box sx={{ p: embedded ? 0 : 3 }}>
      {embedded ? (
        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: 3 }}>{createButton}</Box>
      ) : (
        <PageHeader
          title={t('savedCharsets.title') as string}
          description={t('savedCharsets.description') as string}
          actions={createButton}
        />
      )}

      {/* Personal Charsets */}
      {renderCharsetTable(userCharsets, true, t('savedCharsets.sections.mine') as string, 'saved-charsets-mine')}

      {/* Team Charsets (if any) */}
      {teamCharsets.length > 0 &&
        renderCharsetTable(teamCharsets, false, t('savedCharsets.sections.team') as string, 'saved-charsets-team', undefined, true)}

      {/* Global Charsets (read-only) */}
      {globalCharsets.length > 0 &&
        renderCharsetTable(
          globalCharsets,
          false,
          t('savedCharsets.sections.global') as string,
          'saved-charsets-global',
          t('savedCharsets.sections.globalSubtitle') as string
        )}

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingCharset ? t('savedCharsets.dialogs.edit.title') : t('savedCharsets.dialogs.create.title')}
        </DialogTitle>
        <DialogContent>
          {formError && (
            <Alert severity="error" sx={{ mb: 2, mt: 1 }} onClose={() => setFormError(null)}>
              {formError}
            </Alert>
          )}

          {!editingCharset && (
            <Box sx={{ mb: 2, mt: 1 }}>
              <ToggleButtonGroup
                value={createMode}
                exclusive
                onChange={(_, v) => v && setCreateMode(v)}
                size="small"
              >
                <ToggleButton value="inline">{t('savedCharsets.dialogs.modeToggle.inline')}</ToggleButton>
                <ToggleButton value="file">
                  <UploadFileIcon sx={{ mr: 0.5 }} fontSize="small" />
                  {t('savedCharsets.dialogs.modeToggle.file')}
                </ToggleButton>
              </ToggleButtonGroup>
            </Box>
          )}

          <TextField
            autoFocus
            label={t('savedCharsets.dialogs.fields.name')}
            value={formData.name}
            onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
            fullWidth
            margin="normal"
            required
            placeholder={t('savedCharsets.dialogs.fields.namePlaceholder') as string}
          />

          {(createMode === 'inline' && !editingCharset) || (editingCharset && editingCharset.charset_type !== 'file') ? (
            <>
              <TextField
                label={t('savedCharsets.dialogs.fields.definition')}
                value={formData.definition}
                onChange={(e) => setFormData(prev => ({ ...prev, definition: e.target.value }))}
                fullWidth
                margin="normal"
                required
                placeholder={formData.is_hex ? (t('savedCharsets.dialogs.fields.definitionPlaceholderHex') as string) : (t('savedCharsets.dialogs.fields.definitionPlaceholderPlain') as string)}
                helperText={formData.is_hex
                  ? t('savedCharsets.dialogs.fields.definitionHelperHex', { bytes: formData.definition ? (t('savedCharsets.dialogs.fields.definitionHelperHexBytes', { count: Math.floor(formData.definition.length / 2) }) as string) : '' })
                  : t('savedCharsets.dialogs.fields.definitionHelperPlain')}
                sx={{ '& input': { fontFamily: 'monospace' } }}
              />
              {!editingCharset && (
                <Tooltip title={t('savedCharsets.dialogs.fields.hexTooltip') as string}>
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={formData.is_hex || false}
                        onChange={(e) => setFormData(prev => ({ ...prev, is_hex: e.target.checked }))}
                        size="small"
                      />
                    }
                    label={t('savedCharsets.dialogs.fields.hexLabel') as string}
                  />
                </Tooltip>
              )}
            </>
          ) : null}

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
                {selectedFile ? selectedFile.name : t('savedCharsets.dialogs.fields.selectFile')}
              </Button>
              {selectedFile && (
                <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                  {t('savedCharsets.dialogs.fields.fileSizeBytes', { count: selectedFile.size })}
                </Typography>
              )}
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                {t('savedCharsets.dialogs.fields.fileHelperText')}
              </Typography>
            </Box>
          )}

          {editingCharset && editingCharset.charset_type === 'file' && (
            <Alert severity="info" sx={{ mt: 2 }}>
              {t('savedCharsets.dialogs.fields.fileCharsetInfo', { count: editingCharset.byte_count })}
            </Alert>
          )}

          <TextField
            label={t('savedCharsets.dialogs.fields.description')}
            value={formData.description}
            onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
            fullWidth
            margin="normal"
            multiline
            rows={2}
            placeholder={t('savedCharsets.dialogs.fields.descriptionPlaceholder') as string}
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
            {editingCharset ? t('savedCharsets.dialogs.update') : createMode === 'file' ? t('savedCharsets.dialogs.upload') : t('common.create')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default SavedCharsetsPage;
