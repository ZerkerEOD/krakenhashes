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
import { useTranslation } from 'react-i18next';
import AddBinaryForm from './AddBinaryForm';
import { useSnackbar } from 'notistack';
import { BinaryVersion, listBinaries, verifyBinary, deleteBinary, setDefaultBinary } from '../../services/binary';

const BinaryManagement: React.FC = () => {
  const { t } = useTranslation('admin');
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
      enqueueSnackbar(t('binaryManagement.messages.fetchFailed') as string, { variant: 'error' });
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
      enqueueSnackbar(t('binaryManagement.messages.verifySuccess') as string, { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error verifying binary:', error);
      enqueueSnackbar(error.response?.data || t('binaryManagement.messages.verifyFailed') as string, { variant: 'error' });
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
        t('binaryManagement.messages.cannotDeleteLast', { type: binary.binary_type }) as string,
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
      enqueueSnackbar(t('binaryManagement.messages.deleteSuccess') as string, { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error deleting binary:', error);
      // Check for protection error (409 Conflict)
      if (error.response?.status === 409) {
        enqueueSnackbar(error.response?.data || t('binaryManagement.messages.cannotDeleteOnly') as string, { variant: 'warning' });
      } else {
        enqueueSnackbar(error.response?.data || t('binaryManagement.messages.deleteFailed') as string, { variant: 'error' });
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
      enqueueSnackbar(t('binaryManagement.messages.setDefaultSuccess') as string, { variant: 'success' });
      fetchBinaries();
    } catch (error: any) {
      console.error('Error setting default binary:', error);
      enqueueSnackbar(error.response?.data || t('binaryManagement.messages.setDefaultFailed') as string, { variant: 'error' });
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
          {t('binaryManagement.title') as string}
        </Typography>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={() => setOpenAddDialog(true)}
        >
          {t('binaryManagement.addBinary') as string}
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
              {showActiveOnly ? t('binaryManagement.showingActiveOnly') as string : t('binaryManagement.showingAll') as string}
            </Typography>
          }
        />
      </Box>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>{t('binaryManagement.columns.binaryId') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.version') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.type') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.source') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.size') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.status') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.default') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.lastVerified') as string}</TableCell>
              <TableCell>{t('binaryManagement.columns.actions') as string}</TableCell>
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
                      ? t('binaryManagement.noActiveBinaries') as string
                      : t('binaryManagement.noBinaries') as string}
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
                        label={binary.source_type === 'upload' ? t('binaryManagement.sourceUpload') as string : t('binaryManagement.sourceUrl') as string}
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
                          label={t('binaryManagement.default') as string}
                          color="primary"
                          size="small"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      {binary.last_verified_at ? format(new Date(binary.last_verified_at), 'yyyy-MM-dd HH:mm:ss') : t('common.never') as string}
                    </TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1}>
                        <Tooltip title={t('binaryManagement.verifyBinary') as string}>
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
                        <Tooltip title={t('binaryManagement.deleteBinary') as string}>
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
          {t('binaryManagement.deleteDialog.title') as string}
        </DialogTitle>
        <DialogContent>
          <DialogContentText id="delete-dialog-description">
            {t('binaryManagement.deleteDialog.message', { fileName: selectedBinary?.file_name }) as string}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleDeleteCancel} disabled={isLoading}>
            {t('common.cancel') as string}
          </Button>
          <Button
            onClick={handleDeleteConfirm}
            color="error"
            variant="contained"
            disabled={isLoading}
            startIcon={isLoading ? <CircularProgress size={20} /> : null}
          >
            {t('common.delete') as string}
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