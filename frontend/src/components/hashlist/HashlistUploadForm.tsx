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
  DialogActions,
} from '@mui/material';
import { Clear as ClearIcon } from '@mui/icons-material';
import ClientAutocomplete, { NewClientData } from './ClientAutocomplete';
import HashTypeSelect from './HashTypeSelect';
import ValidationPreviewDialog, { ValidationInvalidEntry } from './ValidationPreviewDialog';
import { useForm, Controller } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../services/api';
import { useNavigate } from 'react-router-dom';
import { useToast } from '../ui/toast';
import { useTranslation } from 'react-i18next';
import { getJobDefaultsForUsers } from '../../services/jobSettings';
import { teamsService } from '../../services/teams';
import { Team } from '../../types/team';

// Create schema function to dynamically set client requirement
const createSchema = (requireClient: boolean, t: (key: string) => string) => {
  const baseSchema = z.object({
    name: z.string().min(1, t('upload.errors.nameRequired')),
    description: z.string().optional(),
    hashTypeId: z.number().min(0, t('upload.errors.hashTypeRequired')),
    clientName: z.string().nullish(),
    excludeFromPotfile: z.boolean().optional(),
    excludeFromClientPotfile: z.boolean().optional(),
  });

  if (requireClient) {
    return baseSchema.refine(
      (data) => data.clientName !== null && data.clientName !== undefined && data.clientName.trim() !== '',
      {
        message: t('upload.errors.clientRequired'),
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
  excludeFromClientPotfile?: boolean;
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
  const [clientPotfilesSystemEnabled, setClientPotfilesSystemEnabled] = useState(true);
  const [requireClient, setRequireClient] = useState(false);
  const [userTeams, setUserTeams] = useState<Team[]>([]);
  const [detectionResult, setDetectionResult] = useState<any>(null);
  const [showLinkDialog, setShowLinkDialog] = useState(false);
  const [isDetecting, setIsDetecting] = useState(false);
  const [createLinked, setCreateLinked] = useState(false);
  const [defaultRetention, setDefaultRetention] = useState<number | null>(null);
  const [newClientData, setNewClientData] = useState<NewClientData | null>(null);

  // Validation preview dialog state (GitHub issue #38). Populated when the
  // backend pauses the upload at the awaiting_validation_decision state.
  const [validationPreview, setValidationPreview] = useState<{
    hashlistId: number;
    hashlistName: string;
    currentHashTypeId: number;
    totalInputLines: number;
    validCount: number;
    invalidCount: number;
    truncated: boolean;
    initialSample: ValidationInvalidEntry[];
  } | null>(null);

  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const { t } = useTranslation('hashlists');

  const { control, handleSubmit, reset, formState: { errors } } = useForm<FormData>({
    resolver: zodResolver(createSchema(requireClient, t)),
    defaultValues: {
      name: '',
      description: '',
      hashTypeId: undefined,
      clientName: null,
      excludeFromPotfile: false,
      excludeFromClientPotfile: false,
    }
  });

  // Calculate hash count from pasted content
  const hashCount = pastedHashes
    .split('\n')
    .filter(line => line.trim().length > 0)
    .length;

  // Detect executable files by reading magic bytes (file signatures)
  // MIME types are unreliable — browsers often report application/octet-stream for binaries
  const isExecutableFile = async (file: File): Promise<boolean> => {
    const header = await file.slice(0, 8).arrayBuffer();
    const bytes = new Uint8Array(header);
    if (bytes.length < 2) return false;

    // ELF binary (Linux, including Go binaries): \x7fELF
    if (bytes[0] === 0x7f && bytes[1] === 0x45 && bytes[2] === 0x4c && bytes[3] === 0x46) return true;
    // Windows PE/EXE: MZ
    if (bytes[0] === 0x4d && bytes[1] === 0x5a) return true;
    // macOS Mach-O (32-bit): \xfe\xed\xfa\xce
    if (bytes[0] === 0xfe && bytes[1] === 0xed && bytes[2] === 0xfa && bytes[3] === 0xce) return true;
    // macOS Mach-O (64-bit): \xfe\xed\xfa\xcf
    if (bytes[0] === 0xfe && bytes[1] === 0xed && bytes[2] === 0xfa && bytes[3] === 0xcf) return true;
    // macOS Mach-O (reverse byte order 32-bit): \xce\xfa\xed\xfe
    if (bytes[0] === 0xce && bytes[1] === 0xfa && bytes[2] === 0xed && bytes[3] === 0xfe) return true;
    // macOS Mach-O (reverse byte order 64-bit): \xcf\xfa\xed\xfe
    if (bytes[0] === 0xcf && bytes[1] === 0xfa && bytes[2] === 0xed && bytes[3] === 0xfe) return true;
    // macOS Universal binary (fat binary): \xca\xfe\xba\xbe
    if (bytes[0] === 0xca && bytes[1] === 0xfe && bytes[2] === 0xba && bytes[3] === 0xbe) return true;

    return false;
  };

  const onDrop = useCallback(async (acceptedFiles: File[]) => {
    if (acceptedFiles.length > 0) {
      const selectedFile = acceptedFiles[0];

      // Block executable files using magic bytes detection
      if (await isExecutableFile(selectedFile)) {
        toast.error(t('upload.executableBlocked') as string);
        return;
      }

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
    maxFiles: 1
  });

  // Fetch global potfile setting and client requirement setting on mount
  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const defaults = await getJobDefaultsForUsers();
        setPotfileGloballyEnabled(defaults.potfile_enabled);
        if (defaults.default_data_retention_months !== undefined) {
          setDefaultRetention(defaults.default_data_retention_months);
        }
      } catch (error) {
        console.error('Failed to fetch potfile setting:', error);
        // Default to true if fetch fails
        setPotfileGloballyEnabled(true);
      }

      // Fetch require client setting or check if teams are enabled
      try {
        // Check if teams are enabled first (takes precedence)
        let teamsEnabled = false;
        try {
          const teamsResponse = await api.get('/api/settings/teams_enabled');
          teamsEnabled = teamsResponse.data?.teams_enabled === true;
        } catch (err) {
          console.error('Failed to fetch teams_enabled setting:', err);
        }

        if (teamsEnabled) {
          // If teams are enabled, client is always required
          setRequireClient(true);
          // Fetch the user's teams so a NEW client can be filed under the
          // right team (only prompted when the user belongs to >1 team).
          try {
            const teams = await teamsService.listUserTeams();
            setUserTeams(teams);
          } catch (teamErr) {
            console.error('Failed to fetch user teams:', teamErr);
          }
        } else {
          // Otherwise, check the require_client_for_hashlist setting
          const response = await api.get('/api/admin/settings/require_client_for_hashlist');
          const requireClientValue = response.data?.value === 'true';
          setRequireClient(requireClientValue);
        }
      } catch (error) {
        console.error('Failed to fetch require client setting:', error);
        // Default to false if fetch fails
        setRequireClient(false);
      }

      // Fetch client potfiles system setting
      try {
        const response = await api.get('/api/admin/settings/client_potfiles_enabled');
        const clientPotfilesEnabled = response.data?.value === 'true';
        setClientPotfilesSystemEnabled(clientPotfilesEnabled);
      } catch (error) {
        console.error('Failed to fetch client potfiles setting:', error);
        // Default to true if fetch fails
        setClientPotfilesSystemEnabled(true);
      }
    };
    fetchSettings();
  }, []);

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
        throw new Error(t('upload.errors.noHashesToUpload') as string);
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
      if (data.excludeFromClientPotfile !== undefined) {
        formData.append('exclude_from_client_potfile', data.excludeFromClientPotfile.toString());
      }
      if (createLinked) {
        formData.append('create_linked', 'true');
      }

      // Append new client override fields when creating a new client
      if (newClientData) {
        // Only a NEW client needs an explicit team — existing clients derive
        // their team from client_teams server-side.
        if (newClientData.teamId) {
          formData.append('team_id', newClientData.teamId);
        }
        if (newClientData.description) {
          formData.append('client_description', newClientData.description);
        }
        if (newClientData.contactInfo) {
          formData.append('client_contact_info', newClientData.contactInfo);
        }
        if (newClientData.dataRetentionMonths !== undefined && newClientData.dataRetentionMonths !== null) {
          formData.append('client_data_retention_months', newClientData.dataRetentionMonths.toString());
        }
        if (newClientData.excludeFromPotfile) {
          formData.append('client_exclude_from_potfile', 'true');
        }
        if (newClientData.excludeFromClientPotfile) {
          formData.append('client_exclude_from_client_potfile', 'true');
        }
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
      // Validation paused for user decision (GitHub issue #38). Show the
      // preview dialog instead of navigating — the user picks proceed/cancel
      // before the hashlist starts processing.
      if (response.data?.validation_status === 'awaiting_decision' && response.data?.id) {
        setValidationPreview({
          hashlistId: response.data.id,
          hashlistName: response.data.name || (t('detail.fallbackHashlistName') as string),
          currentHashTypeId: response.data.hash_type_id ?? 0,
          totalInputLines: response.data.total_input_lines ?? 0,
          validCount: response.data.valid_count ?? 0,
          invalidCount: response.data.invalid_count ?? 0,
          truncated: !!response.data.truncated,
          initialSample: response.data.sample_invalid ?? [],
        });
        setUploadProgress(0);
        return;
      }

      // Non-blocking notice for hash types with no validator coverage.
      if (response.data?.validation_notice) {
        toast.info(response.data.validation_notice);
      }

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
    onError: (error: any) => {
      console.error("Upload failed:", error);
      const data = error?.response?.data;
      const status = error?.response?.status;
      let msg: string;
      if (data?.error) {
        msg = data.error;
      } else if (status) {
        msg = t('upload.errors.uploadFailedWithStatus', { status }) as string;
      } else if (error?.message) {
        msg = t('upload.errors.uploadFailedWithMessage', { message: error.message }) as string;
      } else {
        msg = t('upload.errors.uploadFailedUnknown') as string;
      }
      toast.error(msg);
      setUploadProgress(0);
    }
  });

  const onSubmit = (data: FormData): void => {
    // Creating a new client while the user belongs to more than one team
    // requires an explicit team so it isn't silently filed under the wrong one.
    if (newClientData && userTeams.length > 1 && !newClientData.teamId) {
      toast.error(t('upload.selectTeamForNewClient') as string);
      return;
    }
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
            label={t('upload.name') as string}
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
        label={t('upload.hashType') as string}
      />

      <Controller
        name="description"
        control={control}
        render={({ field }) => (
          <TextField
            {...field}
            label={t('upload.description') as string}
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
              teams={userTeams}
              defaultRetention={defaultRetention}
              onNewClientDataChange={setNewClientData}
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
          {t('upload.methodLabel') as string}
        </Typography>
        <ToggleButtonGroup
          value={uploadMode}
          exclusive
          onChange={(e, newMode) => newMode && setUploadMode(newMode)}
          aria-label={t('upload.methodAriaLabel') as string}
          size="small"
        >
          <ToggleButton value="file" aria-label={t('upload.fileAriaLabel') as string}>
            {t('upload.uploadFile') as string}
          </ToggleButton>
          <ToggleButton value="paste" aria-label={t('upload.pasteAriaLabel') as string}>
            {t('upload.pasteHashes') as string}
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
                  {t('upload.fileSizeKB', { size: (file.size / 1024).toFixed(2) }) as string}
                </Typography>
              </Box>
            ) : isDragActive ? (
              <Typography>{t('upload.dropHere') as string}</Typography>
            ) : (
              <>
                <Typography>{t('upload.dragAndDrop') as string}</Typography>
                <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
                  {t('upload.allFileTypesExceptExecutables') as string}
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
              {t('upload.pasteHashes') as string}
            </Typography>
            {hashCount > 0 && (
              <Chip
                label={t('upload.hashCount', { count: hashCount }) as string}
                size="small"
                color="primary"
                sx={{ mr: 1 }}
              />
            )}
            {pastedHashes && (
              <IconButton size="small" onClick={handleClearPaste} title={t('upload.clear') as string}>
                <ClearIcon fontSize="small" />
              </IconButton>
            )}
          </Box>
          <TextField
            label={t('upload.pasteHashesLabel') as string}
            multiline
            rows={10}
            fullWidth
            value={pastedHashes}
            onChange={(e) => setPastedHashes(e.target.value)}
            placeholder={t('upload.pasteHashesPlaceholder') as string}
            variant="outlined"
            helperText={hashCount > 0 ? (t('upload.hashCountDetected', { count: hashCount }) as string) : (t('upload.pasteHashesHelper') as string)}
          />
        </Box>
      )}

      {uploadProgress > 0 && uploadProgress < 100 && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <CircularProgress variant="determinate" value={uploadProgress} />
          <Typography>{uploadProgress}%</Typography>
        </Box>
      )}

      {/* Global Potfile Exclusion Checkbox - shown when global potfile enabled by admin */}
      {potfileGloballyEnabled && (
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
                label={t('upload.excludeFromGlobalPotfile') as string}
                sx={{ mt: 2 }}
              />
            )}
          />
          <Typography variant="caption" color="textSecondary" display="block" sx={{ ml: 4, mt: -1 }}>
            {t('upload.excludeFromGlobalPotfileHelper') as string}
          </Typography>
        </>
      )}

      {!potfileGloballyEnabled && (
        <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 2 }}>
          {t('upload.globalPotfileDisabledNotice') as string}
        </Typography>
      )}

      {/* Client Potfile Exclusion Checkbox - shown when client potfiles enabled by admin */}
      {clientPotfilesSystemEnabled && (
        <>
          <Controller
            name="excludeFromClientPotfile"
            control={control}
            render={({ field }) => (
              <FormControlLabel
                control={
                  <Checkbox
                    checked={field.value || false}
                    onChange={(e) => field.onChange(e.target.checked)}
                  />
                }
                label={t('upload.excludeFromClientPotfile') as string}
                sx={{ mt: 1 }}
              />
            )}
          />
          <Typography variant="caption" color="textSecondary" display="block" sx={{ ml: 4, mt: -1, mb: 2 }}>
            {t('upload.excludeFromClientPotfileHelper') as string}
          </Typography>
        </>
      )}

      {!clientPotfilesSystemEnabled && (
        <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 1, mb: 2 }}>
          {t('upload.clientPotfilesDisabledNotice') as string}
        </Typography>
      )}

      <Button
        type="submit"
        variant="contained"
        disabled={uploadMutation.isPending || !hasValidInput}
        sx={{ mt: 2 }}
      >
        {uploadMutation.isPending ? (t('upload.uploading') as string) : (t('upload.uploadButtonLabel') as string)}
      </Button>

      {uploadMutation.isError && (
        <Typography color="error" sx={{ mt: 2 }}>
          {t('upload.errors.uploadErrorPrefix', { message: (uploadMutation.error as Error)?.message || (t('upload.errors.unknownError') as string) }) as string}
        </Typography>
      )}

      {/* Detection Dialog */}
      <Dialog open={showLinkDialog} onClose={() => setShowLinkDialog(false)}>
        <DialogTitle>{t('upload.linkDialog.title') as string}</DialogTitle>
        <DialogContent>
          <Typography variant="body1" gutterBottom>
            {t('upload.linkDialog.detected') as string}
          </Typography>
          <Box component="ul" sx={{ mt: 1 }}>
            <li><Typography variant="body2">{t('upload.linkDialog.lmCount', { count: detectionResult?.lm_count || 0 }) as string}</Typography></li>
            <li><Typography variant="body2">{t('upload.linkDialog.ntlmCount', { count: detectionResult?.ntlm_count || 0 }) as string}</Typography></li>
            <li><Typography variant="body2">{t('upload.linkDialog.blankLmCount', { count: detectionResult?.blank_lm_count || 0 }) as string}</Typography></li>
          </Box>
          <Typography variant="body2" sx={{ mt: 2 }}>
            {t('upload.linkDialog.question') as string}
          </Typography>
          <Typography variant="caption" color="textSecondary" display="block" sx={{ mt: 1 }}>
            {t('upload.linkDialog.explanation', { lmName: `${control._formValues.name}-LM`, ntlmName: `${control._formValues.name}-NTLM` }) as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => {
            setShowLinkDialog(false);
            setCreateLinked(false);
          }}>
            {t('upload.linkDialog.uploadSingle') as string}
          </Button>
          <Button variant="contained" onClick={() => {
            setShowLinkDialog(false);
            setCreateLinked(true);
          }}>
            {t('upload.linkDialog.createLinked') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Hash validator preview (GitHub issue #38) */}
      <ValidationPreviewDialog
        open={!!validationPreview}
        hashlistId={validationPreview?.hashlistId ?? null}
        hashlistName={validationPreview?.hashlistName ?? ''}
        currentHashTypeId={validationPreview?.currentHashTypeId ?? 0}
        totalInputLines={validationPreview?.totalInputLines ?? 0}
        validCount={validationPreview?.validCount ?? 0}
        invalidCount={validationPreview?.invalidCount ?? 0}
        truncated={validationPreview?.truncated ?? false}
        initialSample={validationPreview?.initialSample ?? []}
        onProceed={() => {
          const id = validationPreview?.hashlistId;
          setValidationPreview(null);
          reset();
          setFile(null);
          setPastedHashes('');
          queryClient.invalidateQueries({ queryKey: ['hashlists'] });
          if (id) {
            navigate(`/hashlists/${id}`);
          } else if (onSuccess) {
            onSuccess();
          }
        }}
        onCancel={() => {
          setValidationPreview(null);
          toast.info(t('upload.validationCancelled') as string);
        }}
      />
    </Box>
  );
}