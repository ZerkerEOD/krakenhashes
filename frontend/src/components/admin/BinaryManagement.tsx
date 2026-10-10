import React, { useState, useEffect } from 'react';
import {
  Button,
  Typography,
  Box,
  Chip,
  Dialog,
  FormControlLabel,
  Switch,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Add as AddIcon,
  Verified as VerifiedIcon,
  CloudDownload as CloudDownloadIcon,
  CloudUpload as CloudUploadIcon,
} from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { format } from 'date-fns';
import { useTranslation } from 'react-i18next';
import AddBinaryForm from './AddBinaryForm';
import { DataTable, PageHeader, StatusChip, useConfirm, useToast } from '../ui';
import { BinaryVersion, listBinaries, verifyBinary, deleteBinary, setDefaultBinary } from '../../services/binary';

/** Error bodies from the binary endpoints are plain text; fall back when they are not. */
const errorText = (error: any, fallback: string): string =>
  typeof error?.response?.data === 'string' && error.response.data ? error.response.data : fallback;

/**
 * Admin → Resources → Binaries: the page header (title + Add) lives here so the
 * add dialog's open state stays local to this component.
 */
const BinaryManagement: React.FC = () => {
  const { t } = useTranslation('admin');
  const [binaries, setBinaries] = useState<BinaryVersion[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [openAddDialog, setOpenAddDialog] = useState(false);
  const [showActiveOnly, setShowActiveOnly] = useState(true);
  const toast = useToast();
  const confirm = useConfirm();

  const fetchBinaries = async () => {
    try {
      setIsLoading(true);
      const response = await listBinaries();
      setBinaries(response.data || []);
    } catch (error) {
      console.error('Error fetching binaries:', error);
      toast.error(t('binaryManagement.messages.fetchFailed') as string);
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
      toast.success(t('binaryManagement.messages.verifySuccess') as string);
      fetchBinaries();
    } catch (error: any) {
      console.error('Error verifying binary:', error);
      toast.error(errorText(error, t('binaryManagement.messages.verifyFailed') as string));
    } finally {
      setIsLoading(false);
    }
  };

  const handleDeleteClick = async (binary: BinaryVersion) => {
    // Count active binaries of the same type
    const activeBinariesOfType = binaries.filter(
      b => b.binary_type === binary.binary_type &&
      b.is_active &&
      b.verification_status === 'verified'
    ).length;

    // Check if this is the last binary
    if (activeBinariesOfType <= 1) {
      toast.warning(t('binaryManagement.messages.cannotDeleteLast', { type: binary.binary_type }) as string);
      return;
    }

    const ok = await confirm({
      title: t('binaryManagement.deleteDialog.title') as string,
      message: t('binaryManagement.deleteDialog.message', { fileName: binary.file_name }) as string,
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });
    if (!ok) return;

    try {
      setIsLoading(true);
      await deleteBinary(binary.id);
      toast.success(t('binaryManagement.messages.deleteSuccess') as string);
      fetchBinaries();
    } catch (error: any) {
      console.error('Error deleting binary:', error);
      // Check for protection error (409 Conflict)
      if (error.response?.status === 409) {
        toast.warning(errorText(error, t('binaryManagement.messages.cannotDeleteOnly') as string));
      } else {
        toast.error(errorText(error, t('binaryManagement.messages.deleteFailed') as string));
      }
    } finally {
      setIsLoading(false);
    }
  };

  const handleSetDefault = async (id: number) => {
    try {
      setIsLoading(true);
      await setDefaultBinary(id);
      toast.success(t('binaryManagement.messages.setDefaultSuccess') as string);
      fetchBinaries();
    } catch (error: any) {
      console.error('Error setting default binary:', error);
      toast.error(errorText(error, t('binaryManagement.messages.setDefaultFailed') as string));
    } finally {
      setIsLoading(false);
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

  const columns: GridColDef<BinaryVersion>[] = [
    {
      field: 'id',
      headerName: t('binaryManagement.columns.binaryId') as string,
      type: 'number',
      width: 80,
      align: 'left',
      headerAlign: 'left',
    },
    {
      field: 'version',
      headerName: t('binaryManagement.columns.version') as string,
      flex: 1,
      minWidth: 140,
      // Use the API version field if available, otherwise extract from filename
      valueGetter: (_v, row) => row.version || extractNameAndVersion(row.file_name).version,
    },
    {
      field: 'binary_type',
      headerName: t('binaryManagement.columns.type') as string,
      width: 120,
    },
    {
      field: 'source_type',
      headerName: t('binaryManagement.columns.source') as string,
      width: 120,
      renderCell: (p) => (
        <Chip
          icon={p.row.source_type === 'upload' ? <CloudUploadIcon /> : <CloudDownloadIcon />}
          label={p.row.source_type === 'upload' ? t('binaryManagement.sourceUpload') as string : t('binaryManagement.sourceUrl') as string}
          size="small"
          variant="outlined"
        />
      ),
    },
    {
      field: 'file_size',
      headerName: t('binaryManagement.columns.size') as string,
      type: 'number',
      width: 110,
      valueFormatter: (v) => formatFileSize(Number(v) || 0),
    },
    {
      field: 'verification_status',
      headerName: t('binaryManagement.columns.status') as string,
      width: 120,
      renderCell: (p) => <StatusChip entity="verification" status={p.row.verification_status} />,
    },
    {
      field: 'is_default',
      headerName: t('binaryManagement.columns.default') as string,
      width: 150,
      renderCell: (p) => (
        <Box sx={{ display: 'flex', alignItems: 'center' }}>
          <Switch
            checked={p.row.is_default}
            onChange={() => handleSetDefault(p.row.id)}
            disabled={isLoading || p.row.verification_status !== 'verified' || p.row.is_default}
            size="small"
          />
          {p.row.is_default && (
            <Chip label={t('binaryManagement.default') as string} color="primary" size="small" sx={{ ml: 1 }} />
          )}
        </Box>
      ),
    },
    {
      field: 'last_verified_at',
      headerName: t('binaryManagement.columns.lastVerified') as string,
      width: 180,
      valueGetter: (_v, row) => (row.last_verified_at ? new Date(row.last_verified_at).getTime() : 0),
      valueFormatter: (v) => (v ? format(new Date(Number(v)), 'yyyy-MM-dd HH:mm:ss') : (t('common.never') as string)),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('binaryManagement.title') as string}
        actions={
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setOpenAddDialog(true)}>
            {t('binaryManagement.addBinary') as string}
          </Button>
        }
      />

      <DataTable<BinaryVersion>
        rows={filteredBinaries}
        columns={columns}
        getRowId={(r) => r.id}
        loading={isLoading && binaries.length === 0}
        fetching={isLoading && binaries.length > 0}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client', initial: [{ field: 'id', sort: 'desc' }] }}
        toolbar={{
          filters: (
            <FormControlLabel
              control={
                <Switch
                  checked={showActiveOnly}
                  onChange={(e) => setShowActiveOnly(e.target.checked)}
                  color="primary"
                  size="small"
                />
              }
              label={
                <Typography variant="body2" color="text.secondary">
                  {showActiveOnly ? t('binaryManagement.showingActiveOnly') as string : t('binaryManagement.showingAll') as string}
                </Typography>
              }
            />
          ),
        }}
        rowActions={(binary) => [
          {
            key: 'verify',
            label: t('binaryManagement.verifyBinary') as string,
            icon: <VerifiedIcon fontSize="small" />,
            disabled: isLoading || binary.verification_status === 'deleted',
            onClick: (b) => handleVerify(b.id),
          },
          {
            key: 'delete',
            label: t('binaryManagement.deleteBinary') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            disabled: isLoading || binary.verification_status === 'deleted',
            onClick: (b) => handleDeleteClick(b),
          },
        ]}
        emptyState={{
          title: showActiveOnly
            ? t('binaryManagement.noActiveBinaries') as string
            : t('binaryManagement.noBinaries') as string,
        }}
        tableKey="admin-binaries"
      />

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