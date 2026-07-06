import React, { useState, useRef } from 'react';
import {
  Box,
  Typography,
  Button,
  CircularProgress,
  Alert,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
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
import { useSnackbar } from 'notistack';
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
  const { enqueueSnackbar } = useSnackbar();
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

  const { data: charsets, isLoading, error } = useQuery<CustomCharset[], Error>({
    queryKey: ['globalCharsets'],
    queryFn: listGlobalCharsets,
  });

  const createMutation = useMutation<CustomCharset, Error, CustomCharsetFormData>({
    mutationFn: createGlobalCharset,
    onSuccess: () => {
      enqueueSnackbar('Charset created successfully', { variant: 'success' });
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
      enqueueSnackbar('File charset uploaded successfully', { variant: 'success' });
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
      enqueueSnackbar('Charset updated successfully', { variant: 'success' });
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
      enqueueSnackbar('Charset deleted successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['globalCharsets'] });
    },
    onError: (err: Error) => {
      enqueueSnackbar(`Failed to delete charset: ${err.message}`, { variant: 'error' });
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
        setFormError('Only .hcchr files are allowed');
        return;
      }
      if (file.size > 1023) {
        setFormError('File too large (max 1023 bytes — hashcat read buffer limit)');
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
      setFormError('Name is required');
      return;
    }

    if (editingCharset) {
      // Edit mode — only name/description for file charsets
      if (editingCharset.charset_type === 'file') {
        updateMutation.mutate({ id: editingCharset.id, data: { ...formData, definition: '' } });
      } else {
        if (!formData.definition.trim()) {
          setFormError('Definition is required');
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
        setFormError('Please select a .hcchr file');
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
        setFormError('Definition is required');
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

  const handleDelete = (id: string) => {
    if (window.confirm('Are you sure you want to delete this charset?')) {
      deleteMutation.mutate(id);
    }
  };

  const isMutating = createMutation.isPending || updateMutation.isPending || uploadMutation.isPending;

  const renderCharsetValue = (charset: CustomCharset) => {
    if (charset.charset_type === 'file') {
      return (
        <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
          <Chip label="File" size="small" color="info" />
          <Chip label={`${charset.byte_count} unique bytes`} size="small" variant="outlined" sx={{ fontFamily: 'monospace' }} />
        </Box>
      );
    }
    return (
      <Chip label={charset.definition} size="small" variant="outlined" sx={{ fontFamily: 'monospace' }} />
    );
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            Custom Charset Management
          </Typography>
          <Typography variant="body1" color="text.secondary">
            Manage global custom charsets available to all preset jobs and users.
            Supports both inline definitions and binary .hcchr charset files.
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={handleOpenCreate}
          disabled={deleteMutation.isPending}
        >
          Create Charset
        </Button>
      </Box>

      {isLoading && <CircularProgress />}
      {error && <Alert severity="error">Failed to load charsets: {error.message}</Alert>}

      {!isLoading && !error && charsets && (
        <TableContainer component={Paper}>
          <Table sx={{ minWidth: 650 }}>
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Type</TableCell>
                <TableCell>Definition / Info</TableCell>
                <TableCell>Description</TableCell>
                <TableCell>Created</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {charsets.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    No custom charsets found. Create one to get started.
                  </TableCell>
                </TableRow>
              )}
              {charsets.map((charset) => (
                <TableRow key={charset.id}>
                  <TableCell component="th" scope="row">
                    {charset.name}
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', gap: 0.5 }}>
                      <Chip
                        label={charset.charset_type === 'file' ? 'File' : 'Inline'}
                        size="small"
                        color={charset.charset_type === 'file' ? 'info' : 'default'}
                      />
                      {charset.is_hex && (
                        <Chip label="Hex" size="small" color="warning" />
                      )}
                    </Box>
                  </TableCell>
                  <TableCell>{renderCharsetValue(charset)}</TableCell>
                  <TableCell>{charset.description || '—'}</TableCell>
                  <TableCell>{new Date(charset.created_at).toLocaleString()}</TableCell>
                  <TableCell align="right">
                    <IconButton
                      onClick={() => handleOpenEdit(charset)}
                      disabled={deleteMutation.isPending}
                    >
                      <EditIcon />
                    </IconButton>
                    <IconButton
                      onClick={() => handleDelete(charset.id)}
                      disabled={deleteMutation.isPending}
                    >
                      <DeleteIcon />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingCharset ? 'Edit Custom Charset' : 'Create Custom Charset'}
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
                <ToggleButton value="inline">Inline Definition</ToggleButton>
                <ToggleButton value="file">
                  <UploadFileIcon sx={{ mr: 0.5 }} fontSize="small" />
                  File Upload (.hcchr)
                </ToggleButton>
              </ToggleButtonGroup>
            </Box>
          )}

          <TextField
            autoFocus
            label="Name"
            value={formData.name}
            onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
            fullWidth
            margin="normal"
            required
            placeholder="e.g., DES Full Charset"
          />

          {/* Inline definition field */}
          {(createMode === 'inline' && !editingCharset) || (editingCharset && editingCharset.charset_type !== 'file') ? (
            <>
              <TextField
                label="Definition"
                value={formData.definition}
                onChange={(e) => setFormData(prev => ({ ...prev, definition: e.target.value }))}
                fullWidth
                margin="normal"
                required
                placeholder={formData.is_hex ? 'e.g., 41424344 (hex byte pairs)' : 'e.g., ?u?d or abcdef0123456789'}
                helperText={formData.is_hex
                  ? `Hex byte pairs — each pair = one charset byte. ${formData.definition ? Math.floor(formData.definition.length / 2) + ' bytes' : ''}`
                  : 'Hashcat charset definition. Use ?l, ?u, ?d, ?s, ?a, ?b, ?h, ?H or literal characters.'}
                sx={{ '& input': { fontFamily: 'monospace' } }}
              />
              {!editingCharset && (
                <Tooltip title="When enabled, the definition is interpreted as hex byte pairs (e.g., 41424344 = bytes A, B, C, D). Jobs using this charset will auto-inject --hex-charset.">
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={formData.is_hex || false}
                        onChange={(e) => setFormData(prev => ({ ...prev, is_hex: e.target.checked }))}
                        size="small"
                      />
                    }
                    label="Hex-encoded definition"
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
                {selectedFile ? selectedFile.name : 'Select .hcchr File'}
              </Button>
              {selectedFile && (
                <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                  {selectedFile.size} bytes
                </Typography>
              )}
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                Binary charset file containing raw byte values (max 1023 bytes, up to 256 unique bytes).
                Used for DES cracking, NTLMv1, and language-specific encodings.
              </Typography>
            </Box>
          )}

          {/* File charset info (editing) */}
          {editingCharset && editingCharset.charset_type === 'file' && (
            <Alert severity="info" sx={{ mt: 2 }}>
              File charset: {editingCharset.byte_count} unique bytes. Only name and description can be edited.
            </Alert>
          )}

          <TextField
            label="Description"
            value={formData.description}
            onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
            fullWidth
            margin="normal"
            multiline
            rows={2}
            placeholder="Optional description of what this charset is used for"
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog} disabled={isMutating}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            variant="contained"
            disabled={isMutating}
            startIcon={isMutating ? <CircularProgress size={20} /> : undefined}
          >
            {editingCharset ? 'Update' : createMode === 'file' ? 'Upload' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CustomCharsetListPage;
