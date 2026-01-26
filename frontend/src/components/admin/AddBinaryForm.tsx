import React, { useState, useRef } from 'react';
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
import { useSnackbar } from 'notistack';
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
  const { enqueueSnackbar } = useSnackbar();
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
          enqueueSnackbar('Please select a file to upload', { variant: 'error' });
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

      enqueueSnackbar('Binary added successfully', { variant: 'success' });
      onSuccess();
    } catch (error) {
      console.error('Error adding binary:', error);
      enqueueSnackbar(error instanceof Error ? error.message : 'Failed to add binary', { variant: 'error' });
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit}>
      <DialogTitle>Add New Binary</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: 2 }}>
          {/* Source Mode Toggle */}
          <FormControl fullWidth>
            <InputLabel id="source-mode-label">Source</InputLabel>
            <Select
              labelId="source-mode-label"
              value={sourceMode}
              label="Source"
              onChange={handleSourceModeChange}
            >
              <MenuItem value="url">Download from URL</MenuItem>
              <MenuItem value="upload">Upload File</MenuItem>
            </Select>
          </FormControl>

          {/* Binary Type - John disabled */}
          <TextField
            select
            label="Binary Type"
            name="binary_type"
            value={formData.binary_type}
            onChange={handleChange}
            required
            fullWidth
          >
            <MenuItem value="hashcat">Hashcat</MenuItem>
            <MenuItem value="john" disabled>
              John the Ripper (support pending)
            </MenuItem>
          </TextField>

          {/* Compression Type */}
          <TextField
            select
            label="Compression Type"
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
              label="Source URL"
              name="source_url"
              value={formData.source_url}
              onChange={handleChange}
              required
              fullWidth
              type="url"
              helperText="URL to download the binary (e.g., https://hashcat.net/beta/hashcat-6.2.6%2B813.7z)"
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
                {selectedFile ? selectedFile.name : 'Select Archive File'}
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                Upload a .7z, .zip, .tar.gz, or .tar.xz archive containing the binary
              </Typography>
            </Box>
          )}

          {/* File Name */}
          <TextField
            label="File Name"
            name="file_name"
            value={formData.file_name}
            onChange={handleChange}
            required
            fullWidth
            helperText="Auto-filled from URL/file, but can be modified if needed"
          />

          {/* Version */}
          <TextField
            label="Version"
            name="version"
            value={formData.version}
            onChange={handleChange}
            fullWidth
            helperText="Auto-detected from filename. Override for custom builds."
          />

          {/* Description */}
          <TextField
            label="Description"
            name="description"
            value={formData.description}
            onChange={handleChange}
            fullWidth
            multiline
            rows={2}
            helperText="Optional notes about this binary (e.g., custom build details)"
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
            label="Set as default binary"
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onCancel} disabled={isSubmitting}>
          Cancel
        </Button>
        <Button
          type="submit"
          variant="contained"
          disabled={isSubmitting || (sourceMode === 'upload' && !selectedFile)}
          startIcon={isSubmitting ? <CircularProgress size={20} /> : null}
        >
          {sourceMode === 'url' ? 'Add Binary' : 'Upload Binary'}
        </Button>
      </DialogActions>
    </form>
  );
};

export default AddBinaryForm;
