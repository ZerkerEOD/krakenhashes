import React, { useState } from 'react';
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
} from '@mui/material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Add as AddIcon, Edit as EditIcon, Delete as DeleteIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { CustomCharset, CustomCharsetFormData } from '../../types/customCharsets';
import {
  listGlobalCharsets,
  createGlobalCharset,
  updateGlobalCharset,
  deleteGlobalCharset,
} from '../../services/customCharsetService';
import { validateCharsetDefinition } from '../../utils/charsetUtils';

const CustomCharsetListPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  // Dialog state
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingCharset, setEditingCharset] = useState<CustomCharset | null>(null);
  const [formData, setFormData] = useState<CustomCharsetFormData>({
    name: '',
    description: '',
    definition: '',
  });
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
    setFormData({ name: '', description: '', definition: '' });
    setFormError(null);
    setDialogOpen(true);
  };

  const handleOpenEdit = (charset: CustomCharset) => {
    setEditingCharset(charset);
    setFormData({
      name: charset.name,
      description: charset.description,
      definition: charset.definition,
    });
    setFormError(null);
    setDialogOpen(true);
  };

  const handleCloseDialog = () => {
    setDialogOpen(false);
    setEditingCharset(null);
    setFormData({ name: '', description: '', definition: '' });
    setFormError(null);
  };

  const handleSubmit = () => {
    if (!formData.name.trim()) {
      setFormError('Name is required');
      return;
    }
    if (!formData.definition.trim()) {
      setFormError('Definition is required');
      return;
    }

    const validationError = validateCharsetDefinition(formData.definition);
    if (validationError) {
      setFormError(validationError);
      return;
    }

    if (editingCharset) {
      updateMutation.mutate({ id: editingCharset.id, data: formData });
    } else {
      createMutation.mutate(formData);
    }
  };

  const handleDelete = (id: string) => {
    if (window.confirm('Are you sure you want to delete this charset?')) {
      deleteMutation.mutate(id);
    }
  };

  const isMutating = createMutation.isPending || updateMutation.isPending;

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            Custom Charset Management
          </Typography>
          <Typography variant="body1" color="text.secondary">
            Manage global custom charsets available to all preset jobs and users.
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
                <TableCell>Definition</TableCell>
                <TableCell>Description</TableCell>
                <TableCell>Created</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {charsets.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} align="center">
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
                    <Chip
                      label={charset.definition}
                      size="small"
                      variant="outlined"
                      sx={{ fontFamily: 'monospace' }}
                    />
                  </TableCell>
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
          <TextField
            autoFocus
            label="Name"
            value={formData.name}
            onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
            fullWidth
            margin="normal"
            required
            placeholder="e.g., HP iLO Charset"
          />
          <TextField
            label="Definition"
            value={formData.definition}
            onChange={(e) => setFormData(prev => ({ ...prev, definition: e.target.value }))}
            fullWidth
            margin="normal"
            required
            placeholder="e.g., ?u?d or abcdef0123456789"
            helperText="Hashcat charset definition. Use ?l, ?u, ?d, ?s, ?a, ?b, ?h, ?H or literal characters."
            sx={{ '& input': { fontFamily: 'monospace' } }}
          />
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
            {editingCharset ? 'Update' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CustomCharsetListPage;
