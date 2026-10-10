import React, { useState } from 'react';
import { Alert, Box, Button, Typography } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import HashTypeTable from './HashTypeTable';
import HashTypeDialog from './HashTypeDialog';
import { PageHeader, useConfirm, useToast } from '../../ui';
import { HashType, HashTypeCreateRequest, HashTypeUpdateRequest } from '../../../types/hashType';
import {
  getHashTypes,
  createHashType,
  updateHashType,
  deleteHashType,
} from '../../../services/hashType';

const HashTypeManager: React.FC = () => {
  const { t } = useTranslation('admin');
  const toast = useToast();
  const confirm = useConfirm();
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedHashType, setSelectedHashType] = useState<HashType | null>(null);

  // Fetch hash types
  const { data: hashTypes = [], isLoading, isFetching, error, refetch } = useQuery<HashType[], Error>({
    queryKey: ['hashTypes'],
    queryFn: () => getHashTypes(false),
  });

  // Create mutation
  const createMutation = useMutation<HashType, Error, HashTypeCreateRequest>({
    mutationFn: createHashType,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      toast.success(t('hashTypes.messages.createSuccess') as string);
      setDialogOpen(false);
      setSelectedHashType(null);
    },
    onError: (error: any) => {
      const message = error.response?.data?.error || t('hashTypes.messages.createFailed') as string;
      toast.error(message);
    },
  });

  // Update mutation
  const updateMutation = useMutation<HashType, Error, { id: number; data: HashTypeUpdateRequest }>({
    mutationFn: ({ id, data }) => updateHashType(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      toast.success(t('hashTypes.messages.updateSuccess') as string);
      setDialogOpen(false);
      setSelectedHashType(null);
    },
    onError: (error: any) => {
      const message = error.response?.data?.error || t('hashTypes.messages.updateFailed') as string;
      toast.error(message);
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

  // Confirm, then delete inside the dialog; failures show inline in the dialog.
  const handleDelete = async (hashType: HashType) => {
    const ok = await confirm({
      title: t('hashTypes.confirmDelete.title') as string,
      message: (
        <>
          <Typography component="span" variant="body2">
            {t('common.dialogs.confirmDeleteNamed', { name: `${hashType.name} (ID: ${hashType.id})` }) as string}
          </Typography>
          {hashType.is_enabled && (
            <Alert severity="warning" sx={{ mt: 2 }}>
              {t('hashTypes.confirmDelete.warning') as string}
            </Alert>
          )}
        </>
      ),
      severity: 'danger',
      confirmLabel: t('hashTypes.confirmDelete.delete') as string,
      cancelLabel: t('hashTypes.confirmDelete.cancel') as string,
      action: async () => {
        try {
          await deleteHashType(hashType.id);
        } catch (error: any) {
          const message: string = error?.response?.data?.error || (t('hashTypes.messages.deleteFailed') as string);
          throw new Error(
            message.includes('still referenced') ? (t('hashTypes.messages.deleteInUse') as string) : message
          );
        }
      },
    });
    if (ok) {
      queryClient.invalidateQueries({ queryKey: ['hashTypes'] });
      toast.success(t('hashTypes.messages.deleteSuccess') as string);
    }
  };

  const handleSave = async (data: HashTypeCreateRequest | HashTypeUpdateRequest, id?: number) => {
    if (id !== undefined) {
      await updateMutation.mutateAsync({ id, data: data as HashTypeUpdateRequest });
    } else {
      await createMutation.mutateAsync(data as HashTypeCreateRequest);
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('hashTypes.title') as string}
        description={t('hashTypes.description') as string}
        actions={
          <Button variant="contained" startIcon={<AddIcon />} onClick={handleAdd}>
            {t('hashTypes.addHashType') as string}
          </Button>
        }
      />

      <HashTypeTable
        hashTypes={hashTypes}
        onEdit={handleEdit}
        onDelete={handleDelete}
        loading={isLoading}
        fetching={isFetching && !isLoading}
        error={error ? new Error(t('hashTypes.messages.loadFailed') as string) : undefined}
        onRetry={() => refetch()}
      />

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
    </Box>
  );
};

export default HashTypeManager;
