import React, { useState, useCallback } from 'react';
import {
  Box,
  Typography,
  Button,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
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
import { useSnackbar } from 'notistack';
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
  const [tabValue, setTabValue] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

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
      enqueueSnackbar('Client wordlist deleted', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['client-wordlists-mgmt', clientId] });
    },
    onError: (error: any) => {
      enqueueSnackbar(error.response?.data?.error || 'Failed to delete wordlist', { variant: 'error' });
    }
  });

  // Delete association wordlist mutation
  const deleteAssociationMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await deleteAssociationWordlist(wordlistId);
    },
    onSuccess: () => {
      enqueueSnackbar('Association wordlist deleted', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['client-association-wordlists-mgmt', clientId] });
    },
    onError: (error: any) => {
      enqueueSnackbar(error.response?.data?.error || 'Failed to delete wordlist', { variant: 'error' });
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

      enqueueSnackbar('Client wordlist uploaded successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['client-wordlists-mgmt', clientId] });
    } catch (error: any) {
      enqueueSnackbar(error.response?.data?.error || 'Failed to upload wordlist', { variant: 'error' });
    } finally {
      setUploading(false);
      setUploadProgress(0);
    }
  }, [clientId, queryClient, enqueueSnackbar]);

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
      enqueueSnackbar(`Downloaded ${filename}`, { variant: 'success' });
    } catch (error: any) {
      enqueueSnackbar(error.response?.data?.error || 'Failed to download file', { variant: 'error' });
    }
  }, [enqueueSnackbar]);

  const handleTabChange = (_event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue);
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        <Box display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h6">
            Wordlist Management - {client?.name || ''}
          </Typography>
          <IconButton size="small" onClick={onClose}>
            <CloseIcon />
          </IconButton>
        </Box>
      </DialogTitle>
      <DialogContent dividers>
        <Tabs value={tabValue} onChange={handleTabChange} sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tab
            label={`Client Wordlists (${clientWordlists.length})`}
            icon={<WordlistIcon fontSize="small" />}
            iconPosition="start"
          />
          <Tab
            label="Client Potfile"
            icon={<ClientFolderIcon fontSize="small" />}
            iconPosition="start"
          />
          <Tab
            label={`Association Wordlists (${associationWordlists.length})`}
            icon={<AssociationIcon fontSize="small" />}
            iconPosition="start"
          />
        </Tabs>

        {/* Tab 0: Client Wordlists */}
        <TabPanel value={tabValue} index={0}>
          <Alert severity="info" sx={{ mb: 2 }}>
            <Typography variant="body2">
              Client wordlists are available across all hashlists for this client.
              These can be used with any attack mode.
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
                Upload Wordlist
              </Button>
            </label>
            {uploading && (
              <Box sx={{ mt: 1, width: '100%' }}>
                <LinearProgress variant="determinate" value={uploadProgress} />
                <Typography variant="caption" color="text.secondary">
                  Uploading... {uploadProgress}%
                </Typography>
              </Box>
            )}
          </Box>

          {/* Client wordlist table */}
          {isLoadingWordlists ? (
            <LinearProgress />
          ) : clientWordlists.length === 0 ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              No client wordlists uploaded yet.
            </Typography>
          ) : (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>File Name</TableCell>
                    <TableCell align="right">Line Count</TableCell>
                    <TableCell align="right">File Size</TableCell>
                    <TableCell>Uploaded</TableCell>
                    <TableCell align="center">Actions</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {clientWordlists.map((wordlist: ClientWordlist) => (
                    <TableRow key={wordlist.id}>
                      <TableCell>{wordlist.file_name}</TableCell>
                      <TableCell align="right">
                        {wordlist.line_count?.toLocaleString() || '-'}
                      </TableCell>
                      <TableCell align="right">
                        {wordlist.file_size ? formatFileSize(wordlist.file_size) : '-'}
                      </TableCell>
                      <TableCell>{formatDate(wordlist.created_at)}</TableCell>
                      <TableCell align="center">
                        <Tooltip title="Download">
                          <IconButton
                            size="small"
                            color="primary"
                            onClick={() => clientId && handleDownloadBlob(
                              () => downloadClientWordlist(clientId, wordlist.id),
                              wordlist.file_name
                            )}
                          >
                            <DownloadIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Delete">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => deleteWordlistMutation.mutate(wordlist.id)}
                            disabled={deleteWordlistMutation.isPending}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </TabPanel>

        {/* Tab 1: Client Potfile */}
        <TabPanel value={tabValue} index={1}>
          {isLoadingPotfile ? (
            <LinearProgress />
          ) : !clientPotfile ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              No client potfile exists yet. It will be automatically created when hashes are cracked
              for this client's hashlists.
            </Typography>
          ) : (
            <Paper variant="outlined" sx={{ p: 3 }}>
              <Box display="flex" alignItems="center" gap={1} mb={2}>
                <ClientFolderIcon color="primary" />
                <Typography variant="subtitle1" fontWeight="bold">
                  Client Potfile (Auto-generated)
                </Typography>
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                This potfile contains all cracked passwords for this client.
                It is automatically updated when hashes are cracked.
              </Typography>
              <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mb: 2 }}>
                <Chip
                  label={`${clientPotfile.line_count.toLocaleString()} passwords`}
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
                    label={`MD5: ${clientPotfile.md5_hash.substring(0, 12)}...`}
                    size="small"
                    variant="outlined"
                  />
                )}
                <Chip
                  label={`Updated: ${formatDate(clientPotfile.updated_at)}`}
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
                Download Potfile
              </Button>
            </Paper>
          )}
        </TabPanel>

        {/* Tab 2: Association Wordlists */}
        <TabPanel value={tabValue} index={2}>
          <Alert severity="info" sx={{ mb: 2 }}>
            <Typography variant="body2">
              Association wordlists are per-hashlist and map each hash to a password candidate 1:1 by line number.
              They are uploaded and managed from the hashlist detail page. This view shows all association wordlists
              across all hashlists for this client.
            </Typography>
          </Alert>

          {isLoadingAssociation ? (
            <LinearProgress />
          ) : associationWordlists.length === 0 ? (
            <Typography color="text.secondary" sx={{ py: 2 }}>
              No association wordlists found for this client's hashlists.
            </Typography>
          ) : (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>File Name</TableCell>
                    <TableCell>Hashlist</TableCell>
                    <TableCell align="right">Line Count</TableCell>
                    <TableCell align="right">File Size</TableCell>
                    <TableCell>Uploaded</TableCell>
                    <TableCell align="center">Actions</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {associationWordlists.map((wordlist: AssociationWordlistWithHashlist) => (
                    <TableRow key={wordlist.id}>
                      <TableCell>{wordlist.file_name}</TableCell>
                      <TableCell>
                        <Chip
                          label={wordlist.hashlist_name || `Hashlist #${wordlist.hashlist_id}`}
                          size="small"
                          variant="outlined"
                        />
                      </TableCell>
                      <TableCell align="right">
                        {wordlist.line_count?.toLocaleString() || '-'}
                      </TableCell>
                      <TableCell align="right">
                        {wordlist.file_size ? formatFileSize(wordlist.file_size) : '-'}
                      </TableCell>
                      <TableCell>{formatDate(wordlist.created_at)}</TableCell>
                      <TableCell align="center">
                        <Tooltip title="Download">
                          <IconButton
                            size="small"
                            color="primary"
                            onClick={() => handleDownloadBlob(
                              () => downloadAssociationWordlist(wordlist.id),
                              wordlist.file_name
                            )}
                          >
                            <DownloadIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Delete">
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => deleteAssociationMutation.mutate(wordlist.id)}
                            disabled={deleteAssociationMutation.isPending}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </TabPanel>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
