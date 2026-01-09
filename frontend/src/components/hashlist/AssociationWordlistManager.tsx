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
  Collapse
} from '@mui/material';
import {
  CloudUpload as UploadIcon,
  Delete as DeleteIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Link as LinkIcon,
  Warning as WarningIcon,
  CheckCircle as CheckIcon
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

interface AssociationWordlistManagerProps {
  hashlistId: number;
  totalHashes: number;
  hasMixedWorkFactors?: boolean;
}

export default function AssociationWordlistManager({
  hashlistId,
  totalHashes,
  hasMixedWorkFactors = false
}: AssociationWordlistManagerProps) {
  const [expanded, setExpanded] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  // Fetch association wordlists
  const { data: wordlists = [], isLoading, error } = useQuery({
    queryKey: ['association-wordlists', hashlistId],
    queryFn: async () => {
      const response = await api.get(`/api/hashlists/${hashlistId}/association-wordlists`);
      return response.data || [];
    },
    enabled: expanded
  });

  // Delete mutation
  const deleteMutation = useMutation({
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

  // Handle file upload
  const handleFileUpload = useCallback(async (event: React.ChangeEvent<HTMLInputElement>) => {
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

  const formatFileSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString();
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
          <Typography variant="h6">Association Wordlists</Typography>
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
              onChange={handleFileUpload}
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

          {/* Wordlist table */}
          {isLoading ? (
            <LinearProgress />
          ) : error ? (
            <Alert severity="error">Failed to load association wordlists</Alert>
          ) : wordlists.length === 0 ? (
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
                  {wordlists.map((wordlist: AssociationWordlist) => {
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
                              onClick={() => deleteMutation.mutate(wordlist.id)}
                              disabled={deleteMutation.isPending}
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
        </Box>
      </Collapse>
    </Paper>
  );
}
