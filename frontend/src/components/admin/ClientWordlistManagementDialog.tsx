import React, { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Typography,
  Button,
  IconButton,
  LinearProgress,
  Alert,
  Tooltip,
  Chip,
  Tabs,
  Tab,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Paper
} from '@mui/material';
import {
  CloudUpload as UploadIcon,
  Delete as DeleteIcon,
  Download as DownloadIcon,
  FolderSpecial as ClientFolderIcon,
  ListAlt as WordlistIcon,
  Link as AssociationIcon,
  Close as CloseIcon
} from '@mui/icons-material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Client, ClientWordlist, ClientPotfile, AssociationWordlistWithHashlist } from '../../types/client';
import {
  listClientWordlists,
  uploadClientWordlist,
  deleteClientWordlist,
  downloadClientWordlist,
  getClientPotfile,
  downloadClientPotfile,
  listClientAssociationWordlists,
  downloadAssociationWordlist,
  deleteAssociationWordlist
} from '../../services/api';
import { EntityLink, SimpleTable, SimpleColumn, useConfirm, useToast } from '../ui';

interface ClientWordlistManagementDialogProps {
  open: boolean;
  client: Client | null;
  onClose: () => void;
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

const formatFileSize = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
};

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString();
};

export default function ClientWordlistManagementDialog({
  open,
  client,
  onClose
}: ClientWordlistManagementDialogProps) {
  const { t } = useTranslation('admin');
  const [tabValue, setTabValue] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();

  const clientId = client?.id;

  // Fetch client wordlists
  const { data: clientWordlists = [], isLoading: isLoadingWordlists } = useQuery<ClientWordlist[]>({
    queryKey: ['client-wordlists-mgmt', clientId],
    queryFn: async () => {
      if (!clientId) return [];
      const response = await listClientWordlists(clientId);
      return response.data || [];
    },
    enabled: open && !!clientId
  });

  // Fetch client potfile
  const { data: clientPotfile, isLoading: isLoadingPotfile } = useQuery<ClientPotfile | null>({
    queryKey: ['client-potfile-mgmt', clientId],
    queryFn: async () => {
      if (!clientId) return null;
      const response = await getClientPotfile(clientId);
      return response.data as ClientPotfile | null;
    },
    enabled: open && !!clientId
  });

  // Fetch association wordlists for client
  const { data: associationWordlists = [], isLoading: isLoadingAssociation } = useQuery<AssociationWordlistWithHashlist[]>({
    queryKey: ['client-association-wordlists-mgmt', clientId],
    queryFn: async () => {
      if (!clientId) return [];
      const response = await listClientAssociationWordlists(clientId);
      return response.data || [];
    },
    enabled: open && !!clientId
  });

  // Delete client wordlist mutation
  const deleteWordlistMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      if (!clientId) return;
      await deleteClientWordlist(clientId, wordlistId);
    },
    onSuccess: () => {
      toast.success(t('clientWordlistDialog.messages.wordlistDeleted') as string);
      queryClient.invalidateQueries({ queryKey: ['client-wordlists-mgmt', clientId] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || (t('clientWordlistDialog.messages.deleteFailed') as string));
    }
  });

  // Delete association wordlist mutation
  const deleteAssociationMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await deleteAssociationWordlist(wordlistId);
    },
    onSuccess: () => {
      toast.success(t('clientWordlistDialog.messages.associationDeleted') as string);
      queryClient.invalidateQueries({ queryKey: ['client-association-wordlists-mgmt', clientId] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || (t('clientWordlistDialog.messages.deleteFailed') as string));
    }
  });

  // Handle client wordlist file upload
  const handleUpload = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file || !clientId) return;

    event.target.value = '';
    setUploading(true);
    setUploadProgress(0);

    try {
      const formData = new FormData();
      formData.append('file', file);

      await uploadClientWordlist(clientId, formData, (progressEvent) => {
        if (progressEvent.total) {
          const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total);
          setUploadProgress(progress);
        }
      });

      toast.success(t('clientWordlistDialog.messages.uploadSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['client-wordlists-mgmt', clientId] });
    } catch (error: any) {
      toast.error(error.response?.data?.error || (t('clientWordlistDialog.messages.uploadFailed') as string));
    } finally {
      setUploading(false);
      setUploadProgress(0);
    }
  }, [clientId, queryClient, toast, t]);

  // Handle file download
  const handleDownloadBlob = useCallback(async (downloadFn: () => Promise<any>, fallbackFilename: string) => {
    try {
      const response = await downloadFn();
      const blob = new Blob([response.data]);

      // Try to get filename from Content-Disposition header
      let filename = fallbackFilename;
      const contentDisposition = response.headers?.['content-disposition'];
      if (contentDisposition) {
        const match = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
        if (match?.[3]) filename = match[3];
        else if (match?.[1]) filename = match[1].replace(/['"]/g, '');
      }

      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.setAttribute('download', filename);
      document.body.appendChild(link);
      link.click();
      link.parentNode?.removeChild(link);
      window.URL.revokeObjectURL(url);
      toast.success(t('clientWordlistDialog.messages.downloaded', { filename }) as string);
    } catch (error: any) {
      toast.error(error.response?.data?.error || (t('clientWordlistDialog.messages.downloadFailed') as string));
    }
  }, [toast, t]);

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue);
  };

  const confirmDelete = async (fileName: string, onConfirmed: () => void) => {
    const ok = await confirm({
      title: t('clientWordlistDialog.deleteConfirm.title') as string,
      message: t('clientWordlistDialog.deleteConfirm.message', { fileName }) as string,
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });
    if (ok) onConfirmed();
  };

  const renderActions = (onDownload: () => void, onDelete: () => void, deleting: boolean) => (
    <>
      <Tooltip title={t('common.download') as string}>
        <IconButton size="small" color="primary" onClick={onDownload} aria-label={t('common.download') as string}>
          <DownloadIcon fontSize="small" />
        </IconButton>
      </Tooltip>
      <Tooltip title={t('common.delete') as string}>
        <span>
          <IconButton size="small" color="error" onClick={onDelete} disabled={deleting} aria-label={t('common.delete') as string}>
            <DeleteIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
    </>
  );

  const lineCountCol = <R extends { line_count?: number }>(): SimpleColumn<R> => ({
    field: 'line_count',
    headerName: t('clientWordlistDialog.columns.lineCount') as string,
    align: 'right',
    render: (w) => w.line_count?.toLocaleString() || '-',
  });
  const fileSizeCol = <R extends { file_size?: number }>(): SimpleColumn<R> => ({
    field: 'file_size',
    headerName: t('clientWordlistDialog.columns.fileSize') as string,
    align: 'right',
    render: (w) => (w.file_size ? formatFileSize(w.file_size) : '-'),
  });
  const uploadedCol = <R extends { created_at: string }>(): SimpleColumn<R> => ({
    field: 'created_at',
    headerName: t('clientWordlistDialog.columns.uploaded') as string,
    noWrap: true,
    render: (w) => formatDate(w.created_at),
  });

  const clientWordlistColumns: SimpleColumn<ClientWordlist>[] = [
    { field: 'file_name', headerName: t('clientWordlistDialog.columns.fileName') as string },
    lineCountCol<ClientWordlist>(),
    fileSizeCol<ClientWordlist>(),
    uploadedCol<ClientWordlist>(),
    {
      field: 'actions',
      headerName: t('clientWordlistDialog.columns.actions') as string,
      align: 'center',
      noWrap: true,
      render: (wordlist) =>
        renderActions(
          () =>
            clientId &&
            handleDownloadBlob(() => downloadClientWordlist(clientId, wordlist.id), wordlist.file_name),
          () => confirmDelete(wordlist.file_name, () => deleteWordlistMutation.mutate(wordlist.id)),
          deleteWordlistMutation.isPending
        ),
    },
  ];

  const associationColumns: SimpleColumn<AssociationWordlistWithHashlist>[] = [
    { field: 'file_name', headerName: t('clientWordlistDialog.columns.fileName') as string },
    {
      field: 'hashlist',
      headerName: t('clientWordlistDialog.columns.hashlist') as string,
      render: (wordlist) => (
        <EntityLink
          type="hashlist"
          id={wordlist.hashlist_id}
          label={wordlist.hashlist_name || (t('clientWordlistDialog.hashlistFallback', { id: wordlist.hashlist_id }) as string)}
        />
      ),
    },
    lineCountCol<AssociationWordlistWithHashlist>(),
    fileSizeCol<AssociationWordlistWithHashlist>(),
    uploadedCol<AssociationWordlistWithHashlist>(),
    {
      field: 'actions',
      headerName: t('clientWordlistDialog.columns.actions') as string,
      align: 'center',
      noWrap: true,
      render: (wordlist) =>
        renderActions(
          () => handleDownloadBlob(() => downloadAssociationWordlist(wordlist.id), wordlist.file_name),
          () => confirmDelete(wordlist.file_name, () => deleteAssociationMutation.mutate(wordlist.id)),
          deleteAssociationMutation.isPending
        ),
    },
  ];

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        <Box display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h6">
            {t('clientWordlistDialog.title', { clientName: client?.name || '' })}
          </Typography>
          <IconButton size="small" onClick={onClose}>
            <CloseIcon />
          </IconButton>
        </Box>
      </DialogTitle>
      <DialogContent dividers>
        <Tabs value={tabValue} onChange={handleTabChange} sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tab
            label={t('clientWordlistDialog.tabs.clientWordlists', { count: clientWordlists.length })}
            icon={<WordlistIcon fontSize="small" />}
            iconPosition="start"
          />
          <Tab
            label={t('clientWordlistDialog.tabs.clientPotfile') as string}
            icon={<ClientFolderIcon fontSize="small" />}
            iconPosition="start"
          />
          <Tab
            label={t('clientWordlistDialog.tabs.associationWordlists', { count: associationWordlists.length })}
            icon={<AssociationIcon fontSize="small" />}
            iconPosition="start"
          />
        </Tabs>

        {/* Tab 0: Client Wordlists */}
        <TabPanel value={tabValue} index={0}>
          <Alert severity="info" sx={{ mb: 2 }}>
            <Typography variant="body2">
              {t('clientWordlistDialog.clientTab.info')}
            </Typography>
          </Alert>

          {/* Upload section */}
          <Box sx={{ mb: 2 }}>
            <input
              accept=".txt,.lst,.dict"
              style={{ display: 'none' }}
              id="client-wordlist-mgmt-upload"
              type="file"
              onChange={handleUpload}
              disabled={uploading}
            />
            <label htmlFor="client-wordlist-mgmt-upload">
              <Button
                variant="outlined"
                component="span"
                startIcon={<UploadIcon />}
                disabled={uploading}
              >
                {t('clientWordlistDialog.clientTab.uploadWordlist')}
              </Button>
            </label>
            {uploading && (
              <Box sx={{ mt: 1, width: '100%' }}>
                <LinearProgress variant="determinate" value={uploadProgress} />
                <Typography variant="caption" color="text.secondary">
                  {t('clientWordlistDialog.clientTab.uploading', { progress: uploadProgress })}
                </Typography>
              </Box>
            )}
          </Box>

          {/* Client wordlist table */}
          {isLoadingWordlists ? (
            <LinearProgress />
          ) : clientWordlists.length === 0 ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              {t('clientWordlistDialog.clientTab.noWordlists')}
            </Typography>
          ) : (
            <SimpleTable rows={clientWordlists} columns={clientWordlistColumns} getRowKey={(w) => w.id} />
          )}
        </TabPanel>

        {/* Tab 1: Client Potfile */}
        <TabPanel value={tabValue} index={1}>
          {isLoadingPotfile ? (
            <LinearProgress />
          ) : !clientPotfile ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              {t('clientWordlistDialog.potfileTab.noPotfile')}
            </Typography>
          ) : (
            <Paper variant="outlined" sx={{ p: 3 }}>
              <Box display="flex" alignItems="center" gap={1} mb={2}>
                <ClientFolderIcon color="primary" />
                <Typography variant="subtitle1" fontWeight="bold">
                  {t('clientWordlistDialog.potfileTab.autoGenerated')}
                </Typography>
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                {t('clientWordlistDialog.potfileTab.description')}
              </Typography>
              <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mb: 2 }}>
                <Chip
                  label={t('clientWordlistDialog.potfileTab.passwordCount', {
                    count: clientPotfile.line_count,
                    countDisplay: clientPotfile.line_count.toLocaleString(),
                  })}
                  size="small"
                  color="success"
                />
                <Chip
                  label={formatFileSize(clientPotfile.file_size)}
                  size="small"
                  variant="outlined"
                />
                {clientPotfile.md5_hash && (
                  <Chip
                    label={t('clientWordlistDialog.potfileTab.md5Chip', { hash: clientPotfile.md5_hash.substring(0, 12) }) as string}
                    size="small"
                    variant="outlined"
                  />
                )}
                <Chip
                  label={t('clientWordlistDialog.potfileTab.updatedChip', { date: formatDate(clientPotfile.updated_at) }) as string}
                  size="small"
                  variant="outlined"
                />
              </Box>
              <Button
                variant="outlined"
                startIcon={<DownloadIcon />}
                onClick={() => clientId && handleDownloadBlob(
                  () => downloadClientPotfile(clientId),
                  `potfile_${client?.name || clientId}.txt`
                )}
              >
                {t('clientWordlistDialog.potfileTab.downloadPotfile')}
              </Button>
            </Paper>
          )}
        </TabPanel>

        {/* Tab 2: Association Wordlists */}
        <TabPanel value={tabValue} index={2}>
          <Alert severity="info" sx={{ mb: 2 }}>
            <Typography variant="body2">
              {t('clientWordlistDialog.associationTab.info')}
            </Typography>
          </Alert>

          {isLoadingAssociation ? (
            <LinearProgress />
          ) : associationWordlists.length === 0 ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              {t('clientWordlistDialog.associationTab.noWordlists')}
            </Typography>
          ) : (
            <SimpleTable rows={associationWordlists} columns={associationColumns} getRowKey={(w) => w.id} />
          )}
        </TabPanel>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}
