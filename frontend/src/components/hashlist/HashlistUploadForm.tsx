import React, { useState, useCallback, useEffect } from 'react';
import { useDropzone } from 'react-dropzone';
import {
  Box,
  Button,
  CircularProgress,
  TextField,
  Typography,
  ToggleButton,
  ToggleButtonGroup,
  IconButton,
  Chip,
  FormControlLabel,
  Checkbox,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions
} from '@mui/material';
import { Clear as ClearIcon } from '@mui/icons-material';
import ClientAutocomplete from './ClientAutocomplete';
import HashTypeSelect from './HashTypeSelect';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../services/api';
import { useNavigate } from 'react-router-dom';
import { getJobExecutionSettings } from '../../services/jobSettings';

// Create schema function to dynamically set client requirement
const createSchema = (requireClient: boolean) => {
  const baseSchema = z.object({
    name: z.string().min(1, 'Name is required'),
    description: z.string().optional(),
    hashTypeId: z.number().min(1, 'Hash type is required'),
    clientName: z.string().nullish(),
    excludeFromPotfile: z.boolean().optional(),
  });

  if (requireClient) {
    return baseSchema.refine(
      (data) => data.clientName !== null && data.clientName !== undefined && data.clientName.trim() !== '',
      {
        message: 'Client is required',
        path: ['clientName'],
      }
    );
  }

  return baseSchema;
};

type FormData = {
  name: string;
  description?: string;
  hashTypeId: number;
  clientName?: string | null;
  excludeFromPotfile?: boolean;
};

interface HashlistUploadFormProps {
  onSuccess?: () => void;
}

export default function HashlistUploadForm({ onSuccess }: HashlistUploadFormProps) {
  const [file, setFile] = useState<File | null>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadMode, setUploadMode] = useState<'file' | 'paste'>('file');
  const [pastedHashes, setPastedHashes] = useState('');
  const [potfileGloballyEnabled, setPotfileGloballyEnabled] = useState(true);
  const [clientPotfileEnabled, setClientPotfileEnabled] = useState(true);
  const [requireClient, setRequireClient] = useState(false);
  const [detectionResult, setDetectionResult] = useState<any>(null);
  const [showLinkDialog, setShowLinkDialog] = useState(false);
  const [isDetecting, setIsDetecting] = useState(false);
  const [createLinked, setCreateLinked] = useState(false);
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const { control, handleSubmit, reset, setValue, formState: { errors } } = useForm<FormData>({
    resolver: zodResolver(createSchema(requireClient)),
    defaultValues: {
      name: '',
      description: '',
      hashTypeId: undefined,
      clientName: null,
      excludeFromPotfile: false,
    }
  });

  // Calculate hash count from pasted content
  const hashCount = pastedHashes
    .split('\n')
    .filter(line => line.trim().length > 0)
    .length;

  const onDrop = useCallback(async (acceptedFiles: File[]) => {
    if (acceptedFiles.length > 0) {
      const selectedFile = acceptedFiles[0];
      setFile(selectedFile);
      setPastedHashes('');

      // Call detection endpoint to check for LM/NTLM hashes
      setIsDetecting(true);
      try {
        const formData = new FormData();
        formData.append('file', selectedFile);
        const response = await api.post('/api/hashlists/detect-linked', formData);

        if (response.data.has_both_types) {
          setDetectionResult(response.data);
          setShowLinkDialog(true);
        }
      } catch (error) {
        console.error('Detection failed:', error);
        // Continue with normal upload on detection failure
      } finally {
        setIsDetecting(false);
      }
    }
  }, []);

  const { getRootProps, getInputProps, isDragActive } = useDropzone({
    onDrop,
    accept: {
      'text/plain': ['.txt', '.hash', '.hashes', '.lst'],
      'text/csv': ['.csv'],
      'application/octet-stream': ['.hash', '.hashes'],
      '*/*': ['.hash', '.hashes', '.txt', '.csv', '.lst', '.pot']
    },
    maxFiles: 1
  });

  // Fetch global potfile setting and client requirement setting on mount
  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const settings = await getJobExecutionSettings();
        setPotfileGloballyEnabled(settings.potfile_enabled);
      } catch (error) {
        console.error('Failed to fetch potfile setting:', error);
        // Default to true if fetch fails
        setPotfileGloballyEnabled(true);
      }

      // Fetch require client setting
      try {
        const response = await api.get('/api/admin/settings/require_client_for_hashlist');
        const requireClientValue = response.data?.value === 'true';
        setRequireClient(requireClientValue);
      } catch (error) {
        console.error('Failed to fetch require client setting:', error);
        // Default to false if fetch fails
        setRequireClient(false);
      }
    };
    fetchSettings();
  }, []);

  // Fetch client potfile setting when client changes
  useEffect(() => {
    const fetchClientPotfileSetting = async () => {
      const clientName = control._formValues.clientName;
      if (!clientName) {
        setClientPotfileEnabled(true); // Default when no client
        return;
      }

      try {
        // Search for the client to get the full client object
        const response = await api.get(`/api/clients/search?q=${clientName}`);
        const clients = Array.isArray(response.data) ? response.data : [];
        const matchingClient = clients.find((c: any) => c.name === clientName);

        if (matchingClient) {
          setClientPotfileEnabled(!matchingClient.exclude_from_potfile);
        } else {
          setClientPotfileEnabled(true); // Default if client not found
        }
      } catch (error) {
        console.error('Failed to fetch client potfile setting:', error);
        setClientPotfileEnabled(true); // Default on error
      }
    };

    fetchClientPotfileSetting();
  }, [control._formValues.clientName]);

  // Clear the other input when mode changes
  useEffect(() => {
    if (uploadMode === 'file') {
      setPastedHashes('');
    } else {
      setFile(null);
    }
  }, [uploadMode]);

  const uploadMutation = useMutation({
    mutationFn: async (data: FormData) => {
      let fileToUpload: File;

      if (uploadMode === 'file' && file) {
        fileToUpload = file;
      } else if (uploadMode === 'paste' && pastedHashes) {
        // Create a Blob from pasted text
        const blob = new Blob([pastedHashes], { type: 'text/plain' });
        fileToUpload = new File([blob], 'pasted_hashes.txt', { type: 'text/plain' });
      } else {
        throw new Error('No hashes to upload');
      }

      const formData = new FormData();
      formData.append('hashlist_file', fileToUpload);
      formData.append('name', data.name);
      formData.append('hash_type_id', data.hashTypeId.toString());
      if (data.description) formData.append('description', data.description);
      if (data.clientName) formData.append('client_name', data.clientName);
      if (data.excludeFromPotfile !== undefined) {
        formData.append('exclude_from_potfile', data.excludeFromPotfile.toString());
      }
      if (createLinked) {
        formData.append('create_linked', 'true');
      }

      return api.post('/api/hashlists', formData, {
        onUploadProgress: (progressEvent) => {
          const percentCompleted = Math.round(
            (progressEvent.loaded * 100) / (progressEvent.total || 1)
          );
          setUploadProgress(percentCompleted);
        }
      });
    },
    onSuccess: (response) => {
      // The backend returns the created hashlist data
      const hashlistId = response.data?.id || response.data?.data?.id;

      if (hashlistId) {
        // Navigate to the hashlist detail page
        navigate(`/hashlists/${hashlistId}`);
      } else if (onSuccess) {
        // Fallback to the provided callback if no ID is returned
        onSuccess();
      }

      reset();
      setFile(null);
      setPastedHashes('');
      setUploadProgress(0);
    },
    onError: (error) => {
      console.error("Upload failed:", error);
      setUploadProgress(0);
    }
  });

  const onSubmit = (data: FormData): void => {
    uploadMutation.mutate(data);
  };

  const handleClearPaste = () => {
    setPastedHashes('');
  };

  // Check if we have valid input for submission
  const hasValidInput = uploadMode === 'file' ? !!file : pastedHashes.trim().length > 0;

  return (
    <Box component="form" onSubmit={handleSubmit(onSubmit)} sx={{ mt: 3 }}>
      <Controller
        name="name"
        control={control}
        render={({ field }) => (
          <TextField
            {...field}
            label="Hashlist Name"
            fullWidth
            margin="normal"
            error={!!errors.name}
            helperText={errors.name?.message}
          />
        )}
      />

      <HashTypeSelect
        control={control}
        name="hashTypeId"
        label="Hash Type"
      />

      <Controller
        name="description"
        control={control}
        render={({ field }) => (
          <TextField
            {...field}
            label="Description"
            fullWidth
            margin="normal"
            multiline
            rows={3}
          />
        )}
      />

      <Controller
        name="clientName"
        control={control}
        render={({ field }) => (
          <Box>
            <ClientAutocomplete
              value={field.value ?? null}
              onChange={field.onChange}
            />
            {errors.clientName && (
              <Box sx={{ color: 'error.main', fontSize: '0.75rem', mt: 0.5, ml: 1.75 }}>
                {errors.clientName.message}
              </Box>
            )}
          </Box>
        )}
      />

      {/* Mode Toggle */}
      <Box sx={{ mt: 3, mb: 2 }}>
        <Typography variant="subtitle2" gutterBottom>
          Upload Method
        </Typography>
        <ToggleButtonGroup
          value={uploadMode}
          exclusive
          onChange={(e, newMode) => newMode && setUploadMode(newMode)}
          aria-label="upload mode"
          size="small"
        >
          <ToggleButton value="file" aria-label="file upload">
            Upload File
          </ToggleButton>
          <ToggleButton value="paste" aria-label="paste hashes">
            Paste Hashes
          </ToggleButton>
        </ToggleButtonGroup>
      </Box>

      {/* File Upload Mode */}
      {uploadMode === 'file' ? (
        <>
          <Box
            {...getRootProps()}
            sx={{
              border: '2px dashed',
              borderColor: isDragActive ? 'primary.main' : 'grey.400',
              borderRadius: 1,
              p: 4,
              textAlign: 'center',
              cursor: 'pointer',
              my: 2,
              backgroundColor: isDragActive ? 'action.hover' : 'background.paper'
            }}
          >
            <input {...getInputProps()} />
            {file ? (
              <Box>
                <Typography>{file.name}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {(file.size / 1024).toFixed(2)} KB
                </Typography>
              </Box>
            ) : isDragActive ? (
              <Typography>Drop the hashlist file here...</Typography>
            ) : (
              <>
                <Typography>Drag and drop a hashlist file, or click to select</Typography>
                <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
                  Supported: .txt, .hash, .hashes, .csv, .lst, .pot
                </Typography>
              </>
            )}
          </Box>
        </>
      ) : (
        /* Paste Mode */
        <Box sx={{ my: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <Typography variant="subtitle2" sx={{ flexGrow: 1 }}>
              Paste Hashes
            </Typography>
            {hashCount > 0 && (
              <Chip
                label={`${hashCount} hash${hashCount !== 1 ? 'es' : ''}`}
                size="small"
                color="primary"
                sx={{ mr: 1 }}
              />
            )}
            {pastedHashes && (
              <IconButton size="small" onClick={handleClearPaste} title="Clear">
                <ClearIcon fontSize="small" />
              </IconButton>
            )}
          </Box>
          <TextField
            label="Paste hashes here (one per line)"
            multiline
            rows={10}
            fullWidth
            value={pastedHashes}
            onChange={(e) => setPastedHashes(e.target.value)}
            placeholder="Enter hashes, one per line...&#10;&#10;Example:&#10;5f4dcc3b5aa765d61d8327deb882cf99&#10;098f6bcd4621d373cade4e832627b4f6&#10;5d41402abc4b2a76b9719d911017c592"
            variant="outlined"
            helperText={hashCount > 0 ? `${hashCount} hash${hashCount !== 1 ? 'es' : ''} detected` : 'Paste your hashes above, one per line'}
          />
        </Box>
      )}

      {uploadProgress > 0 && uploadProgress < 100 && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <CircularProgress variant="determinate" value={uploadProgress} />
          <Typography>{uploadProgress}%</Typography>
        </Box>
      )}

      {potfileGloballyEnabled && clientPotfileEnabled && (
        <>
          <Controller
            name="excludeFromPotfile"
            control={control}
            render={({ field }) => (
              <FormControlLabel
                control={
                  <Checkbox
                    checked={field.value || false}
                    onChange={(e) => field.onChange(e.target.checked)}
                  />
                }
                label="Exclude from potfile (don't save cracked passwords)"
                sx={{ mt: 2 }}
              />
            )}
          />
          <Typography variant="caption" color="textSecondary" display="block" sx={{ ml: 4, mt: -1, mb: 2 }}>
            Enable this for clients with strict data retention requirements
          </Typography>
        </>
      )}

      {potfileGloballyEnabled && !clientPotfileEnabled && (
        <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 2, mb: 2 }}>
          Note: Potfile is disabled for this client. No cracked passwords will be saved.
        </Typography>
      )}

      {!potfileGloballyEnabled && (
        <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 2, mb: 2 }}>
          Note: Potfile is currently disabled by admin settings. No cracked passwords will be saved.
        </Typography>
      )}

      <Button
        type="submit"
        variant="contained"
        disabled={uploadMutation.isPending || !hasValidInput}
        sx={{ mt: 2 }}
      >
        {uploadMutation.isPending ? 'Uploading...' : 'Upload Hashlist'}
      </Button>

      {uploadMutation.isError && (
        <Typography color="error" sx={{ mt: 2 }}>
          Error uploading hashlist: {(uploadMutation.error as Error)?.message || 'An unknown error occurred'}
        </Typography>
      )}

      {/* Detection Dialog */}
      <Dialog open={showLinkDialog} onClose={() => setShowLinkDialog(false)}>
        <DialogTitle>LM/NTLM Hashes Detected</DialogTitle>
        <DialogContent>
          <Typography variant="body1" gutterBottom>
            Detected pwdump format file with:
          </Typography>
          <Box component="ul" sx={{ mt: 1 }}>
            <li><Typography variant="body2">{detectionResult?.lm_count || 0} LM hashes (non-blank)</Typography></li>
            <li><Typography variant="body2">{detectionResult?.ntlm_count || 0} NTLM hashes</Typography></li>
            <li><Typography variant="body2">{detectionResult?.blank_lm_count || 0} blank LM hashes (empty password - will be skipped)</Typography></li>
          </Box>
          <Typography variant="body2" sx={{ mt: 2 }}>
            Would you like to create two linked hashlists for separate cracking workflows?
          </Typography>
          <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 1 }}>
            This will create "{control._formValues.name}-LM" and "{control._formValues.name}-NTLM" hashlists that are linked together.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => {
            setShowLinkDialog(false);
            setCreateLinked(false);
          }}>
            Upload as Single List
          </Button>
          <Button variant="contained" onClick={() => {
            setShowLinkDialog(false);
            setCreateLinked(true);
          }}>
            Create Linked Lists
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}