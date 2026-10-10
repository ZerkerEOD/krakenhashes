import React, { useState, useCallback } from 'react';
import {
  Box,
  Typography,
  Paper,
  Button,
  IconButton,
  LinearProgress,
  Alert,
  Tooltip,
  Chip,
  Collapse,
  Tabs,
  Tab
} from '@mui/material';
import {
  CloudUpload as UploadIcon,
  Delete as DeleteIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Link as LinkIcon,
  Warning as WarningIcon,
  CheckCircle as CheckIcon,
  FolderSpecial as ClientFolderIcon
} from '@mui/icons-material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation, Trans } from 'react-i18next';
import { api } from '../../services/api';
import { SimpleTable, SimpleColumn, useConfirm, useToast } from '../ui';

interface AssociationWordlist {
  id: string;
  file_name: string;
  file_size: number;
  line_count: number;
  created_at: string;
}

interface ClientWordlist {
  id: string;
  client_id: string;
  file_name: string;
  file_path: string;
  file_size: number;
  line_count: number;
  md5_hash?: string;
  created_at: string;
}

interface ClientPotfile {
  id: number;
  client_id: string;
  file_path: string;
  file_size: number;
  line_count: number;
  md5_hash?: string;
  created_at: string;
  updated_at: string;
}

interface AssociationWordlistManagerProps {
  hashlistId: number;
  totalHashes: number;
  hasMixedWorkFactors?: boolean;
  clientId?: string; // Optional client ID for client-specific wordlists
}

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

function TabPanel({ children, value, index }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ pt: 2 }}>{children}</Box>}
    </div>
  );
}

export default function AssociationWordlistManager({
  hashlistId,
  totalHashes,
  hasMixedWorkFactors = false,
  clientId
}: AssociationWordlistManagerProps) {
  const [expanded, setExpanded] = useState(false);
  const [tabValue, setTabValue] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadingClient, setUploadingClient] = useState(false);
  const [uploadProgressClient, setUploadProgressClient] = useState(0);
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const { t } = useTranslation('hashlists');

  // Fetch association wordlists
  const { data: associationWordlists = [], isLoading: isLoadingAssociation, error: errorAssociation } = useQuery({
    queryKey: ['association-wordlists', hashlistId],
    queryFn: async () => {
      const response = await api.get(`/api/hashlists/${hashlistId}/association-wordlists`);
      return response.data || [];
    },
    enabled: expanded
  });

  // Fetch client wordlists (only if clientId is provided)
  const { data: clientWordlists = [], isLoading: isLoadingClient, error: errorClient } = useQuery({
    queryKey: ['client-wordlists', clientId],
    queryFn: async () => {
      if (!clientId) return [];
      const response = await api.get(`/api/clients/${clientId}/wordlists`);
      return response.data || [];
    },
    enabled: expanded && !!clientId
  });

  // Fetch client potfile (auto-generated potfile containing cracked passwords)
  const { data: clientPotfile, isLoading: isLoadingPotfile } = useQuery<ClientPotfile | null>({
    queryKey: ['client-potfile', clientId],
    queryFn: async () => {
      if (!clientId) return null;
      const response = await api.get(`/api/clients/${clientId}/potfile`);
      return response.data as ClientPotfile | null;
    },
    enabled: expanded && !!clientId
  });

  // Delete association wordlist mutation
  const deleteAssociationMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await api.delete(`/api/hashlists/${hashlistId}/association-wordlists/${wordlistId}`);
    },
    onSuccess: () => {
      toast.success(t('associationWordlists.notifications.associationDeleted') as string);
      queryClient.invalidateQueries({ queryKey: ['association-wordlists', hashlistId] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || (t('associationWordlists.errors.deleteFailed') as string));
    }
  });

  // Delete client wordlist mutation
  const deleteClientMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await api.delete(`/api/clients/${clientId}/wordlists/${wordlistId}`);
    },
    onSuccess: () => {
      toast.success(t('associationWordlists.notifications.clientDeleted') as string);
      queryClient.invalidateQueries({ queryKey: ['client-wordlists', clientId] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || (t('associationWordlists.errors.deleteFailed') as string));
    }
  });

  // Handle association wordlist file upload
  const handleAssociationUpload = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;

    // Reset the input so the same file can be uploaded again
    event.target.value = '';

    setUploading(true);
    setUploadProgress(0);

    try {
      const formData = new FormData();
      formData.append('file', file);

      await api.post(`/api/hashlists/${hashlistId}/association-wordlists`, formData, {
        headers: {
          'Content-Type': 'multipart/form-data'
        },
        onUploadProgress: (progressEvent) => {
          if (progressEvent.total) {
            const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total);
            setUploadProgress(progress);
          }
        }
      });

      toast.success(t('associationWordlists.notifications.associationUploaded') as string);
      queryClient.invalidateQueries({ queryKey: ['association-wordlists', hashlistId] });
    } catch (error: any) {
      // 422 line-count mismatch carries a structured payload with the
      // expected vs. actual counts (GitHub issue #38). Surface that as a
      // persistent error so the user can act on it.
      const status = error?.response?.status;
      const data = error?.response?.data;
      if (status === 422 && data?.error === 'line_count_mismatch') {
        toast.error(
          data.message ||
            (t('associationWordlists.errors.lineCountMismatch', {
              wordlistLines: data.wordlist_lines,
              hashlistLines: data.hashlist_lines,
            }) as string),
          { persist: true },
        );
      } else {
        toast.error(data?.error || (t('associationWordlists.errors.uploadFailed') as string));
      }
    } finally {
      setUploading(false);
      setUploadProgress(0);
    }
  }, [hashlistId, queryClient, toast, t]);

  // Handle client wordlist file upload
  const handleClientUpload = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file || !clientId) return;

    // Reset the input so the same file can be uploaded again
    event.target.value = '';

    setUploadingClient(true);
    setUploadProgressClient(0);

    try {
      const formData = new FormData();
      formData.append('file', file);

      await api.post(`/api/clients/${clientId}/wordlists`, formData, {
        headers: {
          'Content-Type': 'multipart/form-data'
        },
        onUploadProgress: (progressEvent) => {
          if (progressEvent.total) {
            const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total);
            setUploadProgressClient(progress);
          }
        }
      });

      toast.success(t('associationWordlists.notifications.clientUploaded') as string);
      queryClient.invalidateQueries({ queryKey: ['client-wordlists', clientId] });
    } catch (error: any) {
      toast.error(error.response?.data?.error || (t('associationWordlists.errors.uploadFailed') as string));
    } finally {
      setUploadingClient(false);
      setUploadProgressClient(0);
    }
  }, [clientId, queryClient, toast, t]);

  const formatFileSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString();
  };

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue);
  };

  const renderDelete = (fileName: string, onConfirmed: () => void, deleting: boolean) => (
    <Tooltip title={t('associationWordlists.delete') as string}>
      <span>
        <IconButton
          size="small"
          color="error"
          aria-label={t('associationWordlists.delete') as string}
          disabled={deleting}
          onClick={async () => {
            const ok = await confirm({
              title: t('associationWordlists.deleteConfirm.title') as string,
              message: t('associationWordlists.deleteConfirm.message', { fileName }) as string,
              severity: 'danger',
              confirmLabel: t('associationWordlists.delete') as string,
            });
            if (ok) onConfirmed();
          }}
        >
          <DeleteIcon fontSize="small" />
        </IconButton>
      </span>
    </Tooltip>
  );

  const associationColumns: SimpleColumn<AssociationWordlist>[] = [
    { field: 'file_name', headerName: t('associationWordlists.columns.fileName') as string },
    {
      field: 'line_count',
      headerName: t('associationWordlists.columns.lineCount') as string,
      align: 'right',
      render: (w) => w.line_count.toLocaleString(),
    },
    { field: 'file_size', headerName: t('associationWordlists.columns.fileSize') as string, align: 'right', render: (w) => formatFileSize(w.file_size) },
    {
      field: 'status',
      headerName: t('associationWordlists.columns.status') as string,
      render: (w) =>
        w.line_count === totalHashes ? (
          <Chip size="small" icon={<CheckIcon />} label={t('associationWordlists.status.ready') as string} color="success" />
        ) : (
          <Tooltip
            title={t('associationWordlists.status.lineCountMismatchTooltip', {
              expected: totalHashes.toLocaleString(),
              actual: w.line_count.toLocaleString(),
            }) as string}
          >
            <Chip size="small" icon={<WarningIcon />} label={t('associationWordlists.status.lineCountMismatch') as string} color="error" />
          </Tooltip>
        ),
    },
    { field: 'created_at', headerName: t('associationWordlists.columns.uploaded') as string, noWrap: true, render: (w) => formatDate(w.created_at) },
    {
      field: 'actions',
      headerName: t('associationWordlists.columns.actions') as string,
      align: 'center',
      render: (w) =>
        renderDelete(w.file_name, () => deleteAssociationMutation.mutate(w.id), deleteAssociationMutation.isPending),
    },
  ];

  const clientColumns: SimpleColumn<ClientWordlist>[] = [
    { field: 'file_name', headerName: t('associationWordlists.columns.fileName') as string },
    {
      field: 'line_count',
      headerName: t('associationWordlists.columns.lineCount') as string,
      align: 'right',
      render: (w) => w.line_count?.toLocaleString() || '-',
    },
    {
      field: 'file_size',
      headerName: t('associationWordlists.columns.fileSize') as string,
      align: 'right',
      render: (w) => (w.file_size ? formatFileSize(w.file_size) : '-'),
    },
    { field: 'created_at', headerName: t('associationWordlists.columns.uploaded') as string, noWrap: true, render: (w) => formatDate(w.created_at) },
    {
      field: 'actions',
      headerName: t('associationWordlists.columns.actions') as string,
      align: 'center',
      render: (w) => renderDelete(w.file_name, () => deleteClientMutation.mutate(w.id), deleteClientMutation.isPending),
    },
  ];

  return (
    <Paper sx={{ p: 2, mb: 3 }}>
      <Box
        display="flex"
        justifyContent="space-between"
        alignItems="center"
        sx={{ cursor: 'pointer' }}
        onClick={() => setExpanded(!expanded)}
      >
        <Box display="flex" alignItems="center" gap={1}>
          <LinkIcon color="primary" />
          <Typography variant="h6">{t('associationWordlists.header') as string}</Typography>
          {hasMixedWorkFactors && (
            <Tooltip title={t('associationWordlists.blockedTooltip') as string}>
              <Chip
                size="small"
                icon={<WarningIcon />}
                label={t('associationWordlists.blocked') as string}
                color="warning"
              />
            </Tooltip>
          )}
        </Box>
        <IconButton size="small">
          {expanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        </IconButton>
      </Box>

      <Collapse in={expanded}>
        <Box sx={{ mt: 2 }}>
          {/* Tabs for switching between wordlist types */}
          <Tabs value={tabValue} onChange={handleTabChange} sx={{ borderBottom: 1, borderColor: 'divider' }}>
            <Tab
              label={t('associationWordlists.tabs.associationAttack') as string}
              icon={<LinkIcon fontSize="small" />}
              iconPosition="start"
            />
            <Tab
              label={t('associationWordlists.tabs.clientWordlists') as string}
              icon={<ClientFolderIcon fontSize="small" />}
              iconPosition="start"
              disabled={!clientId}
            />
          </Tabs>

          {/* Association Wordlists Tab */}
          <TabPanel value={tabValue} index={0}>
            {hasMixedWorkFactors && (
              <Alert severity="warning" sx={{ mb: 2 }}>
                {t('associationWordlists.mixedWorkFactorsWarning') as string}
              </Alert>
            )}

            <Alert severity="info" sx={{ mb: 2 }}>
              <Typography variant="body2">
                <Trans
                  t={t}
                  i18nKey="associationWordlists.explainer"
                  values={{ lines: totalHashes.toLocaleString() }}
                  components={{ strong: <strong /> }}
                />
              </Typography>
            </Alert>

            {/* Upload section */}
            <Box sx={{ mb: 2 }}>
              <input
                accept=".txt,.lst,.dict"
                style={{ display: 'none' }}
                id="association-wordlist-upload"
                type="file"
                onChange={handleAssociationUpload}
                disabled={uploading}
              />
              <label htmlFor="association-wordlist-upload">
                <Button
                  variant="outlined"
                  component="span"
                  startIcon={<UploadIcon />}
                  disabled={uploading}
                >
                  {t('associationWordlists.uploadAssociation') as string}
                </Button>
              </label>
              {uploading && (
                <Box sx={{ mt: 1, width: '100%' }}>
                  <LinearProgress variant="determinate" value={uploadProgress} />
                  <Typography variant="caption" color="text.secondary">
                    {t('associationWordlists.uploadingPercent', { percent: uploadProgress }) as string}
                  </Typography>
                </Box>
              )}
            </Box>

            {/* Association wordlist table */}
            {isLoadingAssociation ? (
              <LinearProgress />
            ) : errorAssociation ? (
              <Alert severity="error">{t('associationWordlists.errors.loadAssociationFailed') as string}</Alert>
            ) : associationWordlists.length === 0 ? (
              <Typography color="text.secondary" sx={{ py: 2 }}>
                {t('associationWordlists.emptyAssociation') as string}
              </Typography>
            ) : (
              <SimpleTable<AssociationWordlist>
                rows={associationWordlists}
                columns={associationColumns}
                getRowKey={(w) => w.id}
              />
            )}
          </TabPanel>

          {/* Client Wordlists Tab */}
          <TabPanel value={tabValue} index={1}>
            {!clientId ? (
              <Alert severity="info" sx={{ mb: 2 }}>
                {t('associationWordlists.clientWordlists.noClientNotice') as string}
              </Alert>
            ) : (
              <>
                <Alert severity="info" sx={{ mb: 2 }}>
                  <Typography variant="body2">
                    {t('associationWordlists.clientWordlists.description') as string}
                  </Typography>
                </Alert>

                {/* Client Potfile Section */}
                {isLoadingPotfile ? (
                  <LinearProgress sx={{ mb: 2 }} />
                ) : clientPotfile && (
                  <Paper variant="outlined" sx={{ p: 2, mb: 2, bgcolor: 'action.hover' }}>
                    <Box display="flex" alignItems="center" gap={1} mb={1}>
                      <ClientFolderIcon color="primary" />
                      <Typography variant="subtitle2" fontWeight="bold">
                        {t('associationWordlists.clientPotfile.title') as string}
                      </Typography>
                    </Box>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                      {t('associationWordlists.clientPotfile.description') as string}
                    </Typography>
                    <Box sx={{ display: 'flex', gap: 1 }}>
                      <Chip
                        label={t('associationWordlists.clientPotfile.passwordCount', { count: clientPotfile.line_count }) as string}
                        size="small"
                        color="success"
                      />
                      <Chip
                        label={formatFileSize(clientPotfile.file_size)}
                        size="small"
                        variant="outlined"
                      />
                      <Chip
                        label={t('associationWordlists.clientPotfile.updated', { date: formatDate(clientPotfile.updated_at) }) as string}
                        size="small"
                        variant="outlined"
                      />
                    </Box>
                  </Paper>
                )}

                {/* Upload section */}
                <Box sx={{ mb: 2 }}>
                  <input
                    accept=".txt,.lst,.dict"
                    style={{ display: 'none' }}
                    id="client-wordlist-upload"
                    type="file"
                    onChange={handleClientUpload}
                    disabled={uploadingClient}
                  />
                  <label htmlFor="client-wordlist-upload">
                    <Button
                      variant="outlined"
                      component="span"
                      startIcon={<UploadIcon />}
                      disabled={uploadingClient}
                    >
                      {t('associationWordlists.uploadClient') as string}
                    </Button>
                  </label>
                  {uploadingClient && (
                    <Box sx={{ mt: 1, width: '100%' }}>
                      <LinearProgress variant="determinate" value={uploadProgressClient} />
                      <Typography variant="caption" color="text.secondary">
                        {t('associationWordlists.uploadingPercent', { percent: uploadProgressClient }) as string}
                      </Typography>
                    </Box>
                  )}
                </Box>

                {/* Client wordlist table */}
                {isLoadingClient ? (
                  <LinearProgress />
                ) : errorClient ? (
                  <Alert severity="error">{t('associationWordlists.errors.loadClientFailed') as string}</Alert>
                ) : clientWordlists.length === 0 ? (
                  <Typography color="text.secondary" sx={{ py: 2 }}>
                    {t('associationWordlists.emptyClient') as string}
                  </Typography>
                ) : (
                  <SimpleTable<ClientWordlist>
                    rows={clientWordlists}
                    columns={clientColumns}
                    getRowKey={(w) => w.id}
                  />
                )}
              </>
            )}
          </TabPanel>
        </Box>
      </Collapse>
    </Paper>
  );
}
