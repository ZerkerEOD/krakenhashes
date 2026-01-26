import React, { useState, useEffect } from 'react';
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Button,
  IconButton,
  Typography,
  Box,
  Chip,
  Dialog,
  useTheme,
  CircularProgress,
  Stack,
  Tooltip,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  FormControlLabel,
  Switch,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Refresh as RefreshIcon,
  Add as AddIcon,
  Verified as VerifiedIcon,
  CloudDownload as CloudDownloadIcon,
  CloudUpload as CloudUploadIcon,
} from '@mui/icons-material';
import { format } from 'date-fns';
import AddBinaryForm from './AddBinaryForm';
import { useSnackbar } from 'notistack';
import { BinaryVersion, listBinaries, verifyBinary, deleteBinary, setDefaultBinary } from '../../services/binary';

const BinaryManagement: React.FC = () => {
  const [binaries, setBinaries] = useState<BinaryVersion[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [openAddDialog, setOpenAddDialog] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedBinary, setSelectedBinary] = useState<BinaryVersion | null>(null);
  const [showActiveOnly, setShowActiveOnly] = useState(true);
  const { enqueueSnackbar } = useSnackbar();
  const theme = useTheme();

  const fetchBinaries = async () => {
    try {
      setIsLoading(true);
      const response = await listBinaries();
      setBinaries(response.data || []);
    } catch (error) {
      console.error('Error fetching binaries:', error);
      enqueueSnackbar('Failed to fetch binaries', { variant: 'error' });
      setBinaries([]); // Ensure we set an empty array on error
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchBinaries();
  }, []);

  const handleVerify = async (id: number) => {
    try {
      setIsLoading(true);
      await verifyBinary(id);
      enqueueSnackbar('Binary verification completed successfully', { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error verifying binary:', error);
      enqueueSnackbar(error.response?.data || 'Failed to verify binary', { variant: 'error' });
    } finally {
      setIsLoading(false);
    }
  };

  const handleDeleteClick = (binary: BinaryVersion) => {
    // Count active binaries of the same type
    const activeBinariesOfType = binaries.filter(
      b => b.binary_type === binary.binary_type && 
      b.is_active && 
      b.verification_status === 'verified'
    ).length;

    // Check if this is the last binary
    if (activeBinariesOfType <= 1) {
      enqueueSnackbar(
        `Cannot delete the only remaining ${binary.binary_type} binary. Add another binary version before deleting this one.`, 
        { variant: 'warning' }
      );
      return;
    }

    setSelectedBinary(binary);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!selectedBinary) return;

    try {
      setIsLoading(true);
      await deleteBinary(selectedBinary.id);
      enqueueSnackbar('Binary deleted successfully', { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error deleting binary:', error);
      // Check for protection error (409 Conflict)
      if (error.response?.status === 409) {
        enqueueSnackbar(error.response?.data || 'Cannot delete the only remaining binary', { variant: 'warning' });
      } else {
        enqueueSnackbar(error.response?.data || 'Failed to delete binary', { variant: 'error' });
      }
    } finally {
      setIsLoading(false);
      setDeleteDialogOpen(false);
      setSelectedBinary(null);
    }
  };

  const handleSetDefault = async (id: number) => {
    try {
      setIsLoading(true);
      await setDefaultBinary(id);
      enqueueSnackbar('Binary set as default successfully', { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error setting default binary:', error);
      enqueueSnackbar(error.response?.data || 'Failed to set default binary', { variant: 'error' });
    } finally {
      setIsLoading(false);
    }
  };

  const handleDeleteCancel = () => {
    setDeleteDialogOpen(false);
    setSelectedBinary(null);
  };

  const getVerificationStatusColor = (status: string) => {
    switch (status) {
      case 'verified':
        return 'success';
      case 'pending':
        return 'warning';
      case 'failed':
        return 'error';
      case 'deleted':
        return 'default';
      default:
        return 'default';
    }
  };

  const formatFileSize = (bytes: number) => {
    const units = ['B', 'KB', 'MB', 'GB'];
    let size = bytes;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    return `${size.toFixed(2)} ${units[unitIndex]}`;
  };

  const extractNameAndVersion = (fileName: string): { name: string; version: string } => {
    // Example: hashcat-6.2.6+813.7z -> { name: "hashcat", version: "6.2.6+813" }
    const match = fileName.match(/^([^-]+)-(.+?)\.[^.]+$/);
    if (match) {
      return { name: match[1], version: match[2] };
    }
    return { name: fileName, version: 'unknown' };
  };

  const filteredBinaries = showActiveOnly 
    ? binaries.filter(binary => 
        binary.is_active && 
        binary.verification_status === 'verified'
      )
    : binaries;

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 3 }}>
        <Typography variant="h5" component="h2">
          Binary Management
        </Typography>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={() => setOpenAddDialog(true)}
        >
          Add Binary
        </Button>
      </Box>

      <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: 2 }}>
        <FormControlLabel
          control={
            <Switch
              checked={showActiveOnly}
              onChange={(e) => setShowActiveOnly(e.target.checked)}
              color="primary"
            />
          }
          label={
            <Typography variant="body2" color="textSecondary">
              {showActiveOnly ? "Showing Active Binaries Only" : "Showing All Binaries"}
            </Typography>
          }
        />
      </Box>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Binary ID</TableCell>
              <TableCell>Version</TableCell>
              <TableCell>Type</TableCell>
              <TableCell>Source</TableCell>
              <TableCell>Size</TableCell>
              <TableCell>Status</TableCell>
              <TableCell>Default</TableCell>
              <TableCell>Last Verified</TableCell>
              <TableCell>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 3 }}>
                  <CircularProgress />
                </TableCell>
              </TableRow>
            ) : filteredBinaries.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} align="center" sx={{ py: 3 }}>
                  <Typography variant="body1" color="textSecondary">
                    {showActiveOnly
                      ? "No active binaries found. Click 'Add Binary' to add one or switch to 'Show All' to view deleted binaries."
                      : "No binaries found. Click 'Add Binary' to add one."}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              filteredBinaries.map((binary) => {
                // Use the API version field if available, otherwise extract from filename
                const displayVersion = binary.version || extractNameAndVersion(binary.file_name).version;
                return (
                  <TableRow key={binary.id}>
                    <TableCell>{binary.id}</TableCell>
                    <TableCell>{displayVersion}</TableCell>
                    <TableCell>{binary.binary_type}</TableCell>
                    <TableCell>
                      <Chip
                        icon={binary.source_type === 'upload' ? <CloudUploadIcon /> : <CloudDownloadIcon />}
                        label={binary.source_type === 'upload' ? 'Upload' : 'URL'}
                        size="small"
                        variant="outlined"
                      />
                    </TableCell>
                    <TableCell>{formatFileSize(binary.file_size)}</TableCell>
                    <TableCell>
                      <Chip
                        label={binary.verification_status}
                        color={getVerificationStatusColor(binary.verification_status)}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={binary.is_default}
                        onChange={() => handleSetDefault(binary.id)}
                        disabled={isLoading || binary.verification_status !== 'verified' || binary.is_default}
                        size="small"
                      />
                      {binary.is_default && (
                        <Chip
                          label="Default"
                          color="primary"
                          size="small"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      {binary.last_verified_at ? format(new Date(binary.last_verified_at), 'yyyy-MM-dd HH:mm:ss') : 'Never'}
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1}>
                        <Tooltip title="Verify binary">
                          <span>
                            <IconButton
                              onClick={() => handleVerify(binary.id)}
                              disabled={isLoading || binary.verification_status === 'deleted'}
                              color="primary"
                              size="small"
                            >
                              <VerifiedIcon />
                            </IconButton>
                          </span>
                        </Tooltip>
                        <Tooltip title="Delete binary">
                          <span>
                            <IconButton
                              onClick={() => handleDeleteClick(binary)}
                              disabled={isLoading || binary.verification_status === 'deleted'}
                              color="error"
                              size="small"
                            >
                              <DeleteIcon />
                            </IconButton>
                          </span>
                        </Tooltip>
                      </Stack>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </TableContainer>

      {/* Delete Confirmation Dialog */}
      <Dialog
        open={deleteDialogOpen}
        onClose={handleDeleteCancel}
        aria-labelledby="delete-dialog-title"
        aria-describedby="delete-dialog-description"
      >
        <DialogTitle id="delete-dialog-title">
          Delete Binary
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="delete-dialog-description">
            Are you sure you want to delete {selectedBinary?.file_name}? This action cannot be undone.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleDeleteCancel} disabled={isLoading}>
            Cancel
          </Button>
          <Button
            onClick={handleDeleteConfirm}
            color="error"
            variant="contained"
            disabled={isLoading}
            startIcon={isLoading ? <CircularProgress size={20} /> : null}
          >
            Delete
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Binary Dialog */}
      <Dialog
        open={openAddDialog}
        onClose={() => setOpenAddDialog(false)}
        maxWidth="md"
        fullWidth
      >
        <AddBinaryForm
          onSuccess={() => {
            setOpenAddDialog(false);
            fetchBinaries();
          }}
          onCancel={() => setOpenAddDialog(false)}
        />
      </Dialog>
    </Box>
  );
};

export default BinaryManagement; 