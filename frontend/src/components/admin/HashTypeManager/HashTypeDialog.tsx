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
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation('admin');
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
        newErrors.id = t('hashTypes.validation.hashIdRequired') as string;
      } else if (existingIds.includes(formData.id)) {
        newErrors.id = t('hashTypes.validation.hashIdExists') as string;
      }
    }

    if (!formData.name.trim()) {
      newErrors.name = t('hashTypes.validation.nameRequired') as string;
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
      <DialogTitle>{isEditMode ? t('hashTypes.dialog.editTitle') : t('hashTypes.dialog.addTitle')}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
          {!isEditMode && (
            <TextField
              label={t('hashTypes.dialog.hashIdLabel') as string}
              type="number"
              value={formData.id}
              onChange={(e) => setFormData({ ...formData, id: parseInt(e.target.value) || 0 })}
              error={!!errors.id}
              helperText={errors.id || t('hashTypes.dialog.hashIdHelper')}
              required
              fullWidth
            />
          )}

          <TextField
            label={t('hashTypes.dialog.nameLabel') as string}
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            error={!!errors.name}
            helperText={errors.name}
            required
            fullWidth
          />

          <TextField
            label={t('hashTypes.dialog.descriptionLabel') as string}
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            multiline
            rows={3}
            fullWidth
          />

          <TextField
            label={t('hashTypes.dialog.exampleLabel') as string}
            value={formData.example}
            onChange={(e) => setFormData({ ...formData, example: e.target.value })}
            multiline
            rows={2}
            fullWidth
            sx={{ '& .MuiInputBase-input': { fontFamily: 'monospace' } }}
            helperText={t('hashTypes.dialog.exampleHelper')}
          />

          <FormControlLabel
            control={
              <Checkbox
                checked={formData.slow}
                onChange={(e) => setFormData({ ...formData, slow: e.target.checked })}
              />
            }
            label={t('hashTypes.dialog.slowLabel') as string}
          />

          <FormControlLabel
            control={
              <Checkbox
                checked={formData.is_salted}
                onChange={(e) => setFormData({ ...formData, is_salted: e.target.checked })}
              />
            }
            label={t('hashTypes.dialog.saltedLabel') as string}
          />

          {hashType?.needs_processing && (
            <Alert severity="info">
              {t('hashTypes.dialog.processingInfo')}
            </Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={loading}>
          {t('hashTypes.dialog.cancel')}
        </Button>
        <Button onClick={handleSubmit} variant="contained" disabled={loading}>
          {loading ? t('hashTypes.dialog.saving') : t('hashTypes.dialog.save')}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default HashTypeDialog;