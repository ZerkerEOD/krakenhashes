import React, { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  TextField,
  CircularProgress,
  Paper,
  Typography,
  Grid,
  LinearProgress,
  IconButton,
  Alert
} from '@mui/material';
import { CloudUpload, Close, InsertDriveFile } from '@mui/icons-material';
import { styled } from '@mui/material/styles';

const VisuallyHiddenInput = styled('input')({
  clip: 'rect(0 0 0 0)',
  clipPath: 'inset(50%)',
  height: 1,
  overflow: 'hidden',
  position: 'absolute',
  bottom: 0,
  left: 0,
  whiteSpace: 'nowrap',
  width: 1,
});

const FilePreview = styled(Paper)(({ theme }) => ({
  padding: theme.spacing(2),
  marginTop: theme.spacing(2),
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  backgroundColor: theme.palette.background.paper,
}));

interface FileUploadProps {
  title: string;
  description: string;
  acceptedFileTypes: string;
  maxFileSize?: number; // in bytes, Infinity means no limit
  onUpload: (formData: FormData) => Promise<any>;
  uploadButtonText?: string;
  additionalFields?: React.ReactNode;
}

export default function FileUpload({
  title,
  description,
  acceptedFileTypes,
  maxFileSize = Infinity, // Default to no limit
  onUpload,
  uploadButtonText = 'Upload',
  additionalFields
}: FileUploadProps) {
  const { t } = useTranslation('common');
  const [file, setFile] = useState<File | null>(null);
  const [fileDescription, setFileDescription] = useState('');
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadETA, setUploadETA] = useState<number | null>(null);
  const [uploadSpeed, setUploadSpeed] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Listen for upload progress events
  useEffect(() => {
    const handleProgressEvent = (event: Event) => {
      const customEvent = event as CustomEvent<{progress: number, eta?: number, speed?: number}>;
      setUploadProgress(customEvent.detail.progress);
      setUploadETA(customEvent.detail.eta || null);
      setUploadSpeed(customEvent.detail.speed || null);
    };
    
    document.addEventListener('upload-progress', handleProgressEvent);
    
    return () => {
      document.removeEventListener('upload-progress', handleProgressEvent);
    };
  }, []);

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setError(null);
    if (event.target.files && event.target.files.length > 0) {
      const selectedFile = event.target.files[0];
      
      // Only check file size if maxFileSize is not Infinity
      if (maxFileSize !== Infinity && selectedFile.size > maxFileSize) {
        setError(`File size exceeds the maximum limit of ${formatFileSize(maxFileSize)}`);
        return;
      }
      
      setFile(selectedFile);
    }
  };

  const handleRemoveFile = () => {
    setFile(null);
    setError(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const handleDescriptionChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setFileDescription(event.target.value);
  };

  const handleUpload = async () => {
    if (!file) return;
    
    const formData = new FormData();
    formData.append('file', file);
    
    // Add file name as name if not provided
    if (!formData.has('name')) {
      formData.append('name', file.name.split('.')[0]);
    }
    
    if (fileDescription) {
      formData.append('description', fileDescription);
    }
    
    // Add any additional form fields from the form
    if (additionalFields) {
      const additionalInputs = document.querySelectorAll('[name]');
      additionalInputs.forEach((input: Element) => {
        const inputElement = input as HTMLInputElement;
        if (inputElement.name && inputElement.name !== 'file' && inputElement.name !== 'description') {
          formData.append(inputElement.name, inputElement.value);
        }
      });
    }
    
    try {
      setUploading(true);
      setError(null);
      setUploadProgress(0);
      
      // Log authentication status before upload
      console.debug('[FileUpload] Authentication cookies before upload:', document.cookie);
      
      console.debug('[FileUpload] Starting upload with form data:', 
        Array.from(formData.entries()).reduce((obj, [key, val]) => {
          obj[key] = key === 'file' ? '(file content)' : val;
          return obj;
        }, {} as Record<string, any>)
      );
      
      // Use the onUpload function directly, which should have its own progress tracking
      await onUpload(formData);
      
      // Log authentication status after upload
      console.debug('[FileUpload] Authentication cookies after upload:', document.cookie);
      
      // Reset form after successful upload
      setTimeout(() => {
        setFile(null);
        setFileDescription('');
        setUploadProgress(0);
        setUploadETA(null);
        setUploadSpeed(null);
        if (fileInputRef.current) {
          fileInputRef.current.value = '';
        }
      }, 1500);
    } catch (err) {
      console.error('Upload error:', err);
      setError(err instanceof Error ? err.message : 'Failed to upload file');
    } finally {
      setUploading(false);
    }
  };

  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return bytes + ' B';
    else if (bytes < 1048576) return (bytes / 1024).toFixed(2) + ' KB';
    else if (bytes < 1073741824) return (bytes / 1048576).toFixed(2) + ' MB';
    else return (bytes / 1073741824).toFixed(2) + ' GB';
  };

  const formatETA = (seconds: number): string => {
    if (seconds < 60) return `${Math.round(seconds)}s`;
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    if (hours > 0) {
      const remainingMinutes = minutes % 60;
      return `${hours}h ${remainingMinutes}m`;
    }
    return `${minutes} min`;
  };

  const formatSpeed = (bytesPerSec: number): string => {
    // Convert to bits per second for network speed display
    const bitsPerSec = bytesPerSec * 8;
    
    if (bitsPerSec < 1000) return `${bitsPerSec.toFixed(0)}bps`;
    if (bitsPerSec < 1000 * 1000) return `${(bitsPerSec / 1000).toFixed(1)}Kbps`;
    if (bitsPerSec < 1000 * 1000 * 1000) return `${(bitsPerSec / (1000 * 1000)).toFixed(1)}Mbps`;
    
    // Gigabits per second
    const gigabitsPerSec = bitsPerSec / (1000 * 1000 * 1000);
    return `${gigabitsPerSec.toFixed(1)}Gbps`;
  };

  return (
    <Box sx={{ mt: 2, mb: 4 }}>
      <Typography variant="h6" gutterBottom>
        {title}
      </Typography>
      <Typography variant="body2" color="text.secondary" paragraph>
        {description}
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Grid container spacing={2} alignItems="flex-start">
        <Grid item xs={12}>
          <Button
            component="label"
            variant="contained"
            startIcon={<CloudUpload />}
            disabled={uploading}
            sx={{ mb: 2 }}
          >
            {t('fileUpload.chooseFile')}
            <VisuallyHiddenInput
              ref={fileInputRef}
              type="file"
              accept={acceptedFileTypes}
              onChange={handleFileChange}
            />
          </Button>

          {file && (
            <FilePreview elevation={1}>
              <Box sx={{ display: 'flex', alignItems: 'center' }}>
                <InsertDriveFile sx={{ mr: 1 }} />
                <Box>
                  <Typography variant="body2" fontWeight="bold">
                    {file.name}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {formatFileSize(file.size)}
                  </Typography>
                </Box>
              </Box>
              <IconButton size="small" onClick={handleRemoveFile}>
                <Close />
              </IconButton>
            </FilePreview>
          )}
        </Grid>

        <Grid item xs={12}>
          <TextField
            fullWidth
            label={t('fileUpload.description')}
            variant="outlined"
            value={fileDescription}
            onChange={handleDescriptionChange}
            disabled={uploading || !file}
            sx={{ mb: 2 }}
          />
        </Grid>

        {additionalFields && (
          <Grid item xs={12}>
            {additionalFields}
          </Grid>
        )}

        <Grid item xs={12}>
          <Button
            variant="contained"
            color="primary"
            onClick={handleUpload}
            disabled={uploading || !file}
            startIcon={uploading ? <CircularProgress size={20} /> : null}
          >
            {uploading ? t('fileUpload.uploading') : uploadButtonText}
          </Button>
        </Grid>

        {uploading && (
          <Grid item xs={12}>
            <LinearProgress 
              variant="determinate" 
              value={uploadProgress} 
              sx={{ mt: 1, height: 8, borderRadius: 4 }}
            />
            <Typography variant="caption" align="center" display="block" sx={{ mt: 0.5 }}>
              {uploadProgress}%
              {uploadSpeed && uploadSpeed > 0 && ` (${formatSpeed(uploadSpeed)})`}
              {uploadETA && uploadETA > 0 && ` • ${formatETA(uploadETA)}`}
            </Typography>
          </Grid>
        )}
      </Grid>
    </Box>
  );
} 