import React, { useState, useCallback } from 'react';
import {
  Box,
  Typography,
  Paper,
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
import { api } from '../../services/api';
import { useSnackbar } from 'notistack';

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
  const { enqueueSnackbar } = useSnackbar();

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

  // Delete association wordlist mutation
  const deleteAssociationMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await api.delete(`/api/hashlists/${hashlistId}/association-wordlists/${wordlistId}`);
    },
    onSuccess: () => {
      enqueueSnackbar('Association wordlist deleted', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['association-wordlists', hashlistId] });
    },
    onError: (error: any) => {
      enqueueSnackbar(error.response?.data?.error || 'Failed to delete wordlist', { variant: 'error' });
    }
  });

  // Delete client wordlist mutation
  const deleteClientMutation = useMutation({
    mutationFn: async (wordlistId: string) => {
      await api.delete(`/api/clients/${clientId}/wordlists/${wordlistId}`);
    },
    onSuccess: () => {
      enqueueSnackbar('Client wordlist deleted', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['client-wordlists', clientId] });
    },
    onError: (error: any) => {
      enqueueSnackbar(error.response?.data?.error || 'Failed to delete wordlist', { variant: 'error' });
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

      enqueueSnackbar('Association wordlist uploaded successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['association-wordlists', hashlistId] });
    } catch (error: any) {
      enqueueSnackbar(error.response?.data?.error || 'Failed to upload wordlist', { variant: 'error' });
    } finally {
      setUploading(false);
      setUploadProgress(0);
    }
  }, [hashlistId, queryClient, enqueueSnackbar]);

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

      enqueueSnackbar('Client wordlist uploaded successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['client-wordlists', clientId] });
    } catch (error: any) {
      enqueueSnackbar(error.response?.data?.error || 'Failed to upload wordlist', { variant: 'error' });
    } finally {
      setUploadingClient(false);
      setUploadProgressClient(0);
    }
  }, [clientId, queryClient, enqueueSnackbar]);

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
          <Typography variant="h6">Client Specific Wordlists</Typography>
          {hasMixedWorkFactors && (
            <Tooltip title="Association attacks blocked due to mixed work factors in hashlist">
              <Chip
                size="small"
                icon={<WarningIcon />}
                label="Blocked"
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
              label="Association Attack"
              icon={<LinkIcon fontSize="small" />}
              iconPosition="start"
            />
            <Tab
              label="Client Wordlists"
              icon={<ClientFolderIcon fontSize="small" />}
              iconPosition="start"
              disabled={!clientId}
            />
          </Tabs>

          {/* Association Wordlists Tab */}
          <TabPanel value={tabValue} index={0}>
            {hasMixedWorkFactors && (
              <Alert severity="warning" sx={{ mb: 2 }}>
                This hashlist has mixed work factors. Association attacks cannot be run on hashlists
                with different bcrypt costs or similar variable work factors. You can still upload
                wordlists, but creating association attack jobs will be blocked.
              </Alert>
            )}

            <Alert severity="info" sx={{ mb: 2 }}>
              <Typography variant="body2">
                Association wordlists map each hash to a password candidate 1:1 by line number.
                The wordlist must have exactly <strong>{totalHashes.toLocaleString()}</strong> lines
                to match this hashlist's hash count.
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
                  Upload Association Wordlist
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

            {/* Association wordlist table */}
            {isLoadingAssociation ? (
              <LinearProgress />
            ) : errorAssociation ? (
              <Alert severity="error">Failed to load association wordlists</Alert>
            ) : associationWordlists.length === 0 ? (
              <Typography color="text.secondary" sx={{ py: 2 }}>
                No association wordlists uploaded yet.
              </Typography>
            ) : (
              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>File Name</TableCell>
                      <TableCell align="right">Line Count</TableCell>
                      <TableCell align="right">File Size</TableCell>
                      <TableCell>Status</TableCell>
                      <TableCell>Uploaded</TableCell>
                      <TableCell align="center">Actions</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {associationWordlists.map((wordlist: AssociationWordlist) => {
                      const lineCountMatch = wordlist.line_count === totalHashes;
                      return (
                        <TableRow key={wordlist.id}>
                          <TableCell>{wordlist.file_name}</TableCell>
                          <TableCell align="right">
                            {wordlist.line_count.toLocaleString()}
                          </TableCell>
                          <TableCell align="right">
                            {formatFileSize(wordlist.file_size)}
                          </TableCell>
                          <TableCell>
                            {lineCountMatch ? (
                              <Chip
                                size="small"
                                icon={<CheckIcon />}
                                label="Ready"
                                color="success"
                              />
                            ) : (
                              <Tooltip title={`Expected ${totalHashes.toLocaleString()} lines, got ${wordlist.line_count.toLocaleString()}`}>
                                <Chip
                                  size="small"
                                  icon={<WarningIcon />}
                                  label="Line count mismatch"
                                  color="error"
                                />
                              </Tooltip>
                            )}
                          </TableCell>
                          <TableCell>{formatDate(wordlist.created_at)}</TableCell>
                          <TableCell align="center">
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
                      );
                    })}
                  </TableBody>
                </Table>
              </TableContainer>
            )}
          </TabPanel>

          {/* Client Wordlists Tab */}
          <TabPanel value={tabValue} index={1}>
            {!clientId ? (
              <Alert severity="info" sx={{ mb: 2 }}>
                Client wordlists are only available for hashlists associated with a client.
              </Alert>
            ) : (
              <>
                <Alert severity="info" sx={{ mb: 2 }}>
                  <Typography variant="body2">
                    Client wordlists are available across all hashlists for this client.
                    These can be used with any attack mode, not just association attacks.
                  </Typography>
                </Alert>

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
                      Upload Client Wordlist
                    </Button>
                  </label>
                  {uploadingClient && (
                    <Box sx={{ mt: 1, width: '100%' }}>
                      <LinearProgress variant="determinate" value={uploadProgressClient} />
                      <Typography variant="caption" color="text.secondary">
                        Uploading... {uploadProgressClient}%
                      </Typography>
                    </Box>
                  )}
                </Box>

                {/* Client wordlist table */}
                {isLoadingClient ? (
                  <LinearProgress />
                ) : errorClient ? (
                  <Alert severity="error">Failed to load client wordlists</Alert>
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
                              <Tooltip title="Delete">
                                <IconButton
                                  size="small"
                                  color="error"
                                  onClick={() => deleteClientMutation.mutate(wordlist.id)}
                                  disabled={deleteClientMutation.isPending}
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
              </>
            )}
          </TabPanel>
        </Box>
      </Collapse>
    </Paper>
  );
}
