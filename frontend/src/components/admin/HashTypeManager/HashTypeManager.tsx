import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  Alert,
  CircularProgress,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import HashTypeTable from './HashTypeTable';
import HashTypeDialog from './HashTypeDialog';
import { HashType, HashTypeCreateRequest, HashTypeUpdateRequest } from '../../../types/hashType';
import {
  getHashTypes,
  createHashType,
  updateHashType,
  deleteHashType,
} from '../../../services/hashType';

const HashTypeManager: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedHashType, setSelectedHashType] = useState<HashType | null>(null);
  const [hashTypeToDelete, setHashTypeToDelete] = useState<HashType | null>(null);

  // Fetch hash types
  const { data: hashTypes = [], isLoading, error } = useQuery<HashType[], Error>({
    queryKey: ['hashTypes'],
    queryFn: () => getHashTypes(false),
  });

  // Create mutation
  const createMutation = useMutation<HashType, Error, HashTypeCreateRequest>({
    mutationFn: createHashType,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      enqueueSnackbar(t('hashTypes.messages.createSuccess') as string, { variant: 'success' });
      setDialogOpen(false);
      setSelectedHashType(null);
    },
    onError: (error: any) => {
      const message = error.response?.data?.error || t('hashTypes.messages.createFailed') as string;
      enqueueSnackbar(message, { variant: 'error' });
    },
  });

  // Update mutation
  const updateMutation = useMutation<HashType, Error, { id: number; data: HashTypeUpdateRequest }>({
    mutationFn: ({ id, data }) => updateHashType(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      enqueueSnackbar(t('hashTypes.messages.updateSuccess') as string, { variant: 'success' });
      setDialogOpen(false);
      setSelectedHashType(null);
    },
    onError: (error: any) => {
      const message = error.response?.data?.error || t('hashTypes.messages.updateFailed') as string;
      enqueueSnackbar(message, { variant: 'error' });
    },
  });

  // Delete mutation
  const deleteMutation = useMutation<void, Error, number>({
    mutationFn: deleteHashType,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      enqueueSnackbar(t('hashTypes.messages.deleteSuccess') as string, { variant: 'success' });
      setDeleteDialogOpen(false);
      setHashTypeToDelete(null);
    },
    onError: (error: any) => {
      const message = error.response?.data?.error || t('hashTypes.messages.deleteFailed') as string;
      if (message.includes('still referenced')) {
        enqueueSnackbar(t('hashTypes.messages.deleteInUse') as string, { variant: 'error' });
      } else {
        enqueueSnackbar(message, { variant: 'error' });
      }
    },
  });

  const handleAdd = () => {
    setSelectedHashType(null);
    setDialogOpen(true);
  };

  const handleEdit = (hashType: HashType) => {
    setSelectedHashType(hashType);
    setDialogOpen(true);
  };

  const handleDelete = (hashType: HashType) => {
    setHashTypeToDelete(hashType);
    setDeleteDialogOpen(true);
  };

  const handleSave = async (data: HashTypeCreateRequest | HashTypeUpdateRequest, id?: number) => {
    if (id !== undefined) {
      await updateMutation.mutateAsync({ id, data: data as HashTypeUpdateRequest });
    } else {
      await createMutation.mutateAsync(data as HashTypeCreateRequest);
    }
  };

  const confirmDelete = () => {
    if (hashTypeToDelete) {
      deleteMutation.mutate(hashTypeToDelete.id);
    }
  };

  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          {t('hashTypes.messages.loadFailed')}
        </Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('hashTypes.title')}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('hashTypes.description')}
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={handleAdd}
        >
          {t('hashTypes.addHashType')}
        </Button>
      </Box>

      {isLoading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', mt: 4 }}>
          <CircularProgress />
        </Box>
      ) : (
        <HashTypeTable
          hashTypes={hashTypes}
          onEdit={handleEdit}
          onDelete={handleDelete}
          loading={isLoading}
        />
      )}

      <HashTypeDialog
        open={dialogOpen}
        onClose={() => {
          setDialogOpen(false);
          setSelectedHashType(null);
        }}
        onSave={handleSave}
        hashType={selectedHashType}
        existingIds={hashTypes.map(ht => ht.id)}
      />

      <Dialog
        open={deleteDialogOpen}
        onClose={() => {
          setDeleteDialogOpen(false);
          setHashTypeToDelete(null);
        }}
      >
        <DialogTitle>{t('hashTypes.confirmDelete.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('common.dialogs.confirmDeleteNamed', { name: `${hashTypeToDelete?.name} (ID: ${hashTypeToDelete?.id})` })}
            {hashTypeToDelete?.is_enabled && (
              <Alert severity="warning" sx={{ mt: 2 }}>
                {t('hashTypes.confirmDelete.warning')}
              </Alert>
            )}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => {
              setDeleteDialogOpen(false);
              setHashTypeToDelete(null);
            }}
          >
            {t('hashTypes.confirmDelete.cancel')}
          </Button>
          <Button
            onClick={confirmDelete}
            color="error"
            variant="contained"
            disabled={deleteMutation.isPending}
          >
            {deleteMutation.isPending ? t('hashTypes.confirmDelete.deleting') : t('hashTypes.confirmDelete.delete')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default HashTypeManager; 