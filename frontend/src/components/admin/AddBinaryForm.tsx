import React, { useState, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  MenuItem,
  CircularProgress,
  FormControlLabel,
  Checkbox,
  Typography,
  FormControl,
  InputLabel,
  Select,
  SelectChangeEvent,
} from '@mui/material';
import CloudUploadIcon from '@mui/icons-material/CloudUpload';
import { useToast } from '../ui/toast';
import { AddBinaryRequest, UploadBinaryRequest, addBinary, uploadBinary } from '../../services/binary';

interface AddBinaryFormProps {
  onSuccess: () => void;
  onCancel: () => void;
}

// Helper function to extract version from filename
const extractVersionFromFileName = (fileName: string): string => {
  // Match common version patterns like:
  // hashcat-6.2.6+813.7z -> 6.2.6+813
  // hashcat-6.2.6.7z -> 6.2.6
  // john-1.9.0-jumbo-1.tar.gz -> 1.9.0-jumbo-1
  const versionMatch = fileName.match(/[-_](\d+\.\d+(?:\.\d+)?(?:[+\-]\w+(?:\.\d+)?)?)/);
  return versionMatch ? versionMatch[1] : '';
};

const AddBinaryForm: React.FC<AddBinaryFormProps> = ({ onSuccess, onCancel }) => {
  const { t } = useTranslation('admin');
  const [sourceMode, setSourceMode] = useState<'url' | 'upload'>('url');
  const [formData, setFormData] = useState({
    binary_type: 'hashcat' as 'hashcat' | 'john',
    compression_type: '7z' as '7z' | 'zip' | 'tar.gz' | 'tar.xz',
    source_url: '',
    file_name: '',
    version: '',
    description: '',
    set_as_default: false,
  });
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const toast = useToast();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, type, checked } = e.target;

    if (name === 'source_url' && value) {
      // Extract file name and version from URL
      try {
        const url = new URL(value);
        const fileName = decodeURIComponent(url.pathname.split('/').pop() || '');
        const version = extractVersionFromFileName(fileName);
        setFormData((prev) => ({
          ...prev,
          source_url: value,
          file_name: fileName,
          version: version,
        }));
        return;
      } catch (error) {
        console.warn('Failed to parse URL:', error);
      }
    }

    setFormData((prev) => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : value
    }));
  };

  const handleSelectChange = (e: SelectChangeEvent<string>) => {
    const { name, value } = e.target;
    setFormData((prev) => ({ ...prev, [name]: value }));
  };

  const handleSourceModeChange = (e: SelectChangeEvent<string>) => {
    const newMode = e.target.value as 'url' | 'upload';
    setSourceMode(newMode);
    // Reset source-specific fields
    setFormData((prev) => ({
      ...prev,
      source_url: '',
      file_name: '',
      version: '',
    }));
    setSelectedFile(null);
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      setSelectedFile(file);
      const version = extractVersionFromFileName(file.name);
      setFormData((prev) => ({
        ...prev,
        file_name: file.name,
        version: version,
      }));
    }
  };

  const handleFileButtonClick = () => {
    fileInputRef.current?.click();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    try {
      setIsSubmitting(true);

      if (sourceMode === 'url') {
        const request: AddBinaryRequest = {
          binary_type: formData.binary_type,
          compression_type: formData.compression_type,
          source_url: formData.source_url,
          file_name: formData.file_name,
          set_as_default: formData.set_as_default,
          description: formData.description || undefined,
          version: formData.version || undefined,
        };
        await addBinary(request);
      } else {
        if (!selectedFile) {
          toast.error(t('binaryManagement.form.selectFileRequired') as string);
          return;
        }
        const request: UploadBinaryRequest = {
          binary_type: formData.binary_type,
          compression_type: formData.compression_type,
          file: selectedFile,
          file_name: formData.file_name,
          set_as_default: formData.set_as_default,
          description: formData.description || undefined,
          version: formData.version || undefined,
        };
        await uploadBinary(request);
      }

      toast.success(t('binaryManagement.messages.addSuccess') as string);
      onSuccess();
    } catch (error) {
      console.error('Error adding binary:', error);
      toast.error(error instanceof Error ? error.message : (t('binaryManagement.messages.addFailed') as string));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit}>
      <DialogTitle>{t('binaryManagement.form.addDialogTitle')}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: 2 }}>
          {/* Source Mode Toggle */}
          <FormControl fullWidth>
            <InputLabel id="source-mode-label">{t('binaryManagement.form.sourceLabel')}</InputLabel>
            <Select
              labelId="source-mode-label"
              value={sourceMode}
              label={t('binaryManagement.form.sourceLabel') as string}
              onChange={handleSourceModeChange}
            >
              <MenuItem value="url">{t('binaryManagement.form.downloadFromUrl')}</MenuItem>
              <MenuItem value="upload">{t('binaryManagement.form.uploadFile')}</MenuItem>
            </Select>
          </FormControl>

          {/* Binary Type - John disabled */}
          <TextField
            select
            label={t('binaryManagement.form.binaryTypeLabel')}
            name="binary_type"
            value={formData.binary_type}
            onChange={handleChange}
            required
            fullWidth
          >
            <MenuItem value="hashcat">Hashcat</MenuItem>
            <MenuItem value="john" disabled>
              {t('binaryManagement.form.johnPending')}
            </MenuItem>
          </TextField>

          {/* Compression Type */}
          <TextField
            select
            label={t('binaryManagement.form.compressionTypeLabel')}
            name="compression_type"
            value={formData.compression_type}
            onChange={handleChange}
            required
            fullWidth
          >
            <MenuItem value="7z">7z</MenuItem>
            <MenuItem value="zip">ZIP</MenuItem>
            <MenuItem value="tar.gz">TAR.GZ</MenuItem>
            <MenuItem value="tar.xz">TAR.XZ</MenuItem>
          </TextField>

          {/* Source-specific field: URL or File Upload */}
          {sourceMode === 'url' ? (
            <TextField
              label={t('binaryManagement.form.sourceUrlLabel')}
              name="source_url"
              value={formData.source_url}
              onChange={handleChange}
              required
              fullWidth
              type="url"
              helperText={t('binaryManagement.form.sourceUrlHelp') as string}
            />
          ) : (
            <Box>
              <input
                type="file"
                ref={fileInputRef}
                onChange={handleFileChange}
                style={{ display: 'none' }}
                accept=".7z,.zip,.tar.gz,.tar.xz"
              />
              <Button
                variant="outlined"
                onClick={handleFileButtonClick}
                startIcon={<CloudUploadIcon />}
                fullWidth
                sx={{ height: 56, justifyContent: 'flex-start', pl: 2 }}
              >
                {selectedFile ? selectedFile.name : t('binaryManagement.form.selectArchiveFile')}
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                {t('binaryManagement.form.uploadHelp')}
              </Typography>
            </Box>
          )}

          {/* File Name */}
          <TextField
            label={t('binaryManagement.form.fileNameLabel')}
            name="file_name"
            value={formData.file_name}
            onChange={handleChange}
            required
            fullWidth
            helperText={t('binaryManagement.form.fileNameHelp') as string}
          />

          {/* Version */}
          <TextField
            label={t('binaryManagement.form.versionLabel')}
            name="version"
            value={formData.version}
            onChange={handleChange}
            fullWidth
            helperText={t('binaryManagement.form.versionHelp') as string}
          />

          {/* Description */}
          <TextField
            label={t('binaryManagement.form.descriptionLabel')}
            name="description"
            value={formData.description}
            onChange={handleChange}
            fullWidth
            multiline
            rows={2}
            helperText={t('binaryManagement.form.descriptionHelp') as string}
          />

          {/* Set as Default */}
          <FormControlLabel
            control={
              <Checkbox
                name="set_as_default"
                checked={formData.set_as_default}
                onChange={handleChange}
              />
            }
            label={t('binaryManagement.form.setAsDefault') as string}
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel} disabled={isSubmitting}>
          {t('common.cancel')}
        </Button>
        <Button
          type="submit"
          variant="contained"
          disabled={isSubmitting || (sourceMode === 'upload' && !selectedFile)}
          startIcon={isSubmitting ? <CircularProgress size={20} /> : null}
        >
          {sourceMode === 'url' ? t('binaryManagement.addBinary') : t('binaryManagement.form.uploadBinarySubmit')}
        </Button>
      </DialogActions>
    </form>
  );
};

export default AddBinaryForm;
