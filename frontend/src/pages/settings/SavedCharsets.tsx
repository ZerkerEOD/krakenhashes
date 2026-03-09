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
import { CustomCharset, CustomCharsetFormData } from '../../types/customCharsets';
import {
  listAccessibleCharsets,
  createUserCharset,
  updateUserCharset,
  deleteUserCharset,
} from '../../services/customCharsetService';
import { validateCharsetDefinition } from '../../utils/charsetUtils';

const SavedCharsetsPage: React.FC = () => {
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
    queryKey: ['accessibleCharsets'],
    queryFn: listAccessibleCharsets,
  });

  const createMutation = useMutation<CustomCharset, Error, CustomCharsetFormData>({
    mutationFn: createUserCharset,
    onSuccess: () => {
      enqueueSnackbar('Charset created successfully', { variant: 'success' });
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
      enqueueSnackbar('Charset updated successfully', { variant: 'success' });
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
      enqueueSnackbar('Charset deleted successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['accessibleCharsets'] });
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

  const handleDelete = (charset: CustomCharset) => {
    if (charset.scope === 'global') {
      enqueueSnackbar('Global charsets can only be deleted by admins in the admin panel', { variant: 'warning' });
      return;
    }
    if (window.confirm('Are you sure you want to delete this charset?')) {
      deleteMutation.mutate(charset.id);
    }
  };

  // Separate charsets by scope for display
  const globalCharsets = charsets?.filter(c => c.scope === 'global') || [];
  const userCharsets = charsets?.filter(c => c.scope === 'user') || [];
  const teamCharsets = charsets?.filter(c => c.scope === 'team') || [];

  const isMutating = createMutation.isPending || updateMutation.isPending;

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            Saved Charsets
          </Typography>
          <Typography variant="body1" color="text.secondary">
            Manage your personal custom charsets for use in mask-based attacks.
            Global charsets created by admins are also shown for reference.
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
        <>
          {/* Personal Charsets */}
          <Typography variant="h6" sx={{ mb: 1, mt: 2 }}>
            My Charsets
          </Typography>
          <TableContainer component={Paper} sx={{ mb: 3 }}>
            <Table>
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
                {userCharsets.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} align="center">
                      No personal charsets yet. Create one to save commonly used charset definitions.
                    </TableCell>
                  </TableRow>
                )}
                {userCharsets.map((charset) => (
                  <TableRow key={charset.id}>
                    <TableCell component="th" scope="row">{charset.name}</TableCell>
                    <TableCell>
                      <Chip label={charset.definition} size="small" variant="outlined" sx={{ fontFamily: 'monospace' }} />
                    </TableCell>
                    <TableCell>{charset.description || '—'}</TableCell>
                    <TableCell>{new Date(charset.created_at).toLocaleString()}</TableCell>
                    <TableCell align="right">
                      <IconButton onClick={() => handleOpenEdit(charset)} disabled={deleteMutation.isPending}>
                        <EditIcon />
                      </IconButton>
                      <IconButton onClick={() => handleDelete(charset)} disabled={deleteMutation.isPending}>
                        <DeleteIcon />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>

          {/* Team Charsets (if any) */}
          {teamCharsets.length > 0 && (
            <>
              <Typography variant="h6" sx={{ mb: 1 }}>
                Team Charsets
              </Typography>
              <TableContainer component={Paper} sx={{ mb: 3 }}>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>Definition</TableCell>
                      <TableCell>Description</TableCell>
                      <TableCell>Created</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {teamCharsets.map((charset) => (
                      <TableRow key={charset.id}>
                        <TableCell component="th" scope="row">{charset.name}</TableCell>
                        <TableCell>
                          <Chip label={charset.definition} size="small" variant="outlined" sx={{ fontFamily: 'monospace' }} />
                        </TableCell>
                        <TableCell>{charset.description || '—'}</TableCell>
                        <TableCell>{new Date(charset.created_at).toLocaleString()}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </>
          )}

          {/* Global Charsets (read-only) */}
          {globalCharsets.length > 0 && (
            <>
              <Typography variant="h6" sx={{ mb: 1 }}>
                Global Charsets
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                These charsets are managed by admins and available to everyone.
              </Typography>
              <TableContainer component={Paper}>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>Definition</TableCell>
                      <TableCell>Description</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {globalCharsets.map((charset) => (
                      <TableRow key={charset.id}>
                        <TableCell component="th" scope="row">{charset.name}</TableCell>
                        <TableCell>
                          <Chip label={charset.definition} size="small" variant="outlined" sx={{ fontFamily: 'monospace' }} />
                        </TableCell>
                        <TableCell>{charset.description || '—'}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </>
          )}
        </>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={dialogOpen} onClose={handleCloseDialog} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editingCharset ? 'Edit Charset' : 'Create Personal Charset'}
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

export default SavedCharsetsPage;
