import React, { useState, useEffect } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  FormControlLabel,
  Checkbox,
  Box,
  Alert,
} from '@mui/material';
import { HashType, HashTypeCreateRequest, HashTypeUpdateRequest } from '../../../types/hashType';

interface HashTypeDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: (data: HashTypeCreateRequest | HashTypeUpdateRequest, id?: number) => Promise<void>;
  hashType?: HashType | null;
  existingIds?: number[];
}

const HashTypeDialog: React.FC<HashTypeDialogProps> = ({
  open,
  onClose,
  onSave,
  hashType,
  existingIds = [],
}) => {
  const [formData, setFormData] = useState({
    id: 0,
    name: '',
    description: '',
    example: '',
    slow: false,
    is_salted: false,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);

  const isEditMode = !!hashType;

  useEffect(() => {
    if (hashType) {
      setFormData({
        id: hashType.id,
        name: hashType.name,
        description: hashType.description || '',
        example: hashType.example || '',
        slow: hashType.slow,
        is_salted: hashType.is_salted,
      });
    } else {
      setFormData({
        id: 0,
        name: '',
        description: '',
        example: '',
        slow: false,
        is_salted: false,
      });
    }
    setErrors({});
  }, [hashType, open]);

  const validate = (): boolean => {
    const newErrors: Record<string, string> = {};

    if (!isEditMode) {
      if (!formData.id || formData.id <= 0) {
        newErrors.id = 'Hash ID is required and must be positive';
      } else if (existingIds.includes(formData.id)) {
        newErrors.id = 'This Hash ID already exists';
      }
    }

    if (!formData.name.trim()) {
      newErrors.name = 'Name is required';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validate()) return;

    setLoading(true);
    try {
      if (isEditMode) {
        const updateData: HashTypeUpdateRequest = {
          name: formData.name,
          description: formData.description || null,
          example: formData.example || null,
          is_enabled: true,
          slow: formData.slow,
          is_salted: formData.is_salted,
        };
        await onSave(updateData, hashType.id);
      } else {
        const createData: HashTypeCreateRequest = {
          id: formData.id,
          name: formData.name,
          description: formData.description || null,
          example: formData.example || null,
          is_enabled: true,
          slow: formData.slow,
          is_salted: formData.is_salted,
        };
        await onSave(createData);
      }
      onClose();
    } catch (error) {
      console.error('Error saving hash type:', error);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>{isEditMode ? 'Edit Hash Type' : 'Add New Hash Type'}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
          {!isEditMode && (
            <TextField
              label="Hash ID (Hashcat Mode Number)"
              type="number"
              value={formData.id}
              onChange={(e) => setFormData({ ...formData, id: parseInt(e.target.value) || 0 })}
              error={!!errors.id}
              helperText={errors.id || 'e.g., 1000 for NTLM'}
              required
              fullWidth
            />
          )}
          
          <TextField
            label="Name"
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            error={!!errors.name}
            helperText={errors.name}
            required
            fullWidth
          />
          
          <TextField
            label="Description"
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            multiline
            rows={3}
            fullWidth
          />
          
          <TextField
            label="Example Hash"
            value={formData.example}
            onChange={(e) => setFormData({ ...formData, example: e.target.value })}
            multiline
            rows={2}
            fullWidth
            sx={{ '& .MuiInputBase-input': { fontFamily: 'monospace' } }}
            helperText="Example of this hash format"
          />
          
          <FormControlLabel
            control={
              <Checkbox
                checked={formData.slow}
                onChange={(e) => setFormData({ ...formData, slow: e.target.checked })}
              />
            }
            label="Slow Hash Algorithm"
          />

          <FormControlLabel
            control={
              <Checkbox
                checked={formData.is_salted}
                onChange={(e) => setFormData({ ...formData, is_salted: e.target.checked })}
              />
            }
            label="Salted Hash Type (e.g., NTLMv2, bcrypt)"
          />

          {hashType?.needs_processing && (
            <Alert severity="info">
              This hash type requires special processing. Processing logic is managed programmatically.
            </Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={loading}>
          Cancel
        </Button>
        <Button onClick={handleSubmit} variant="contained" disabled={loading}>
          {loading ? 'Saving...' : 'Save'}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default HashTypeDialog;