import React, { useState } from 'react';
import { Box, Typography, Button, CircularProgress, Alert, Chip, Tooltip } from '@mui/material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { Add as AddIcon, Edit as EditIcon, Delete as DeleteIcon, Calculate as CalculateIcon } from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { useTranslation } from 'react-i18next';
import { DataTable, EntityLink, PageHeader, useConfirm, useToast } from '../../components/ui';
import { ROUTES } from '../../constants/routes';

// Import types and API functions from existing services
// Ensure AttackMode enum is imported if needed for display formatting
import { PresetJob, AttackMode } from '../../types/adminJobs'; 
import { listPresetJobs, deletePresetJob, api } from '../../services/api';

// Helper function to format AttackMode enum for display
const formatAttackMode = (mode: AttackMode): string => {
  switch (mode) {
    case AttackMode.Straight: return 'Straight';
    case AttackMode.Combination: return 'Combination';
    case AttackMode.BruteForce: return 'Brute-Force';
    case AttackMode.HybridWordlistMask: return 'Hybrid (Wordlist + Mask)';
    case AttackMode.HybridMaskWordlist: return 'Hybrid (Mask + Wordlist)';
    case AttackMode.Association: return 'Association';
    default: return `Unknown (${mode})`;
  }
};

const PresetJobListPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const [calculatingJobs, setCalculatingJobs] = useState<Set<string>>(new Set());

  // Correct useQuery signature: options object only
  const { data: presetJobs, isLoading, isFetching, error, refetch } = useQuery<PresetJob[], Error>({
    queryKey: ['presetJobs'],
    queryFn: listPresetJobs,
  });

  // Correct useMutation signature: options object with mutationFn
  const deleteMutation = useMutation<void, Error, string>({
    mutationFn: deletePresetJob, // Specify mutation function here
    onSuccess: () => {
      toast.success(t('presetJobs.messages.deleteSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: Error) => {
      toast.error(t('presetJobs.messages.deleteFailed', { error: err.message }) as string);
    },
  });

  // Mutation for recalculating keyspace
  const recalculateKeyspaceMutation = useMutation<PresetJob, Error, string>({
    mutationFn: async (id: string) => {
      setCalculatingJobs(prev => new Set(prev).add(id));
      try {
        const response = await api.post(`/api/admin/preset-jobs/${id}/recalculate-keyspace`);
        return response.data;
      } finally {
        setCalculatingJobs(prev => {
          const newSet = new Set(prev);
          newSet.delete(id);
          return newSet;
        });
      }
    },
    onSuccess: () => {
      toast.success(t('presetJobs.messages.keyspaceRecalculateSuccess') as string);
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || t('common.unknownError');
      toast.error(t('presetJobs.messages.keyspaceRecalculateFailed', { error: errorMessage }) as string);
    },
  });

  const recalculateAllKeyspacesMutation = useMutation<any, Error>({
    mutationFn: async () => {
      const response = await api.post('/api/admin/preset-jobs/recalculate-all-keyspaces');
      return response.data;
    },
    onSuccess: (data) => {
      const message = t('presetJobs.messages.keyspaceAllComplete', { updated: data.updated, skipped: data.skipped, failed: data.failed }) as string;
      if (data.failed > 0) toast.warning(message); else toast.success(message);
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || t('common.unknownError');
      toast.error(t('presetJobs.messages.keyspaceAllFailed', { error: errorMessage }) as string);
    },
  });

  const handleDelete = async (job: PresetJob) => {
    const ok = await confirm({
      title: t('common.delete') as string,
      message: (
        <>
          {t('presetJobs.confirmDelete') as string}
          <Typography component="div" variant="body2" sx={{ mt: 1, fontWeight: 600 }}>
            {job.name}
          </Typography>
        </>
      ),
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });
    if (ok) deleteMutation.mutate(job.id);
  };

  const handleRecalculateKeyspace = (id: string) => {
    recalculateKeyspaceMutation.mutate(id);
  };

  // Helper function to format keyspace
  const formatKeyspace = (keyspace: number | null | undefined): string => {
    if (keyspace === null || keyspace === undefined) {
      return t('presetJobs.keyspaceNotCalculated') as string;
    }
    // Format large numbers with commas
    return keyspace.toLocaleString();
  };

  // Check if any jobs need keyspace calculation
  const hasJobsWithoutKeyspace = presetJobs?.some(job => job.keyspace === null || job.keyspace === undefined) || false;

  const actionsBusy = deleteMutation.isPending || recalculateKeyspaceMutation.isPending;

  const columns: GridColDef<PresetJob>[] = [
    {
      field: 'name',
      headerName: t('presetJobs.columns.name') as string,
      flex: 1.5,
      minWidth: 200,
      renderCell: (p) => <EntityLink type="preset_job" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'attack_mode',
      headerName: t('presetJobs.columns.attackMode') as string,
      width: 190,
      valueFormatter: (v) => formatAttackMode(v as AttackMode),
    },
    {
      field: 'priority',
      headerName: t('presetJobs.columns.priority') as string,
      type: 'number',
      width: 90,
      align: 'left',
      headerAlign: 'left',
    },
    {
      field: 'allow_high_priority_override',
      headerName: t('presetJobs.columns.highPriorityOverride') as string,
      width: 130,
      renderCell: (p) =>
        p.row.allow_high_priority_override ? (
          <Tooltip title={t('presetJobs.canInterruptRunningJobs') as string}>
            <Chip label={t('common.yes') as string} size="small" color="error" variant="filled" />
          </Tooltip>
        ) : (
          <Chip label={t('common.no') as string} size="small" variant="outlined" />
        ),
    },
    {
      field: 'max_agents',
      headerName: t('presetJobs.columns.maxAgents') as string,
      width: 110,
      valueFormatter: (v) => (v === 0 ? (t('common.unlimited') as string) : String(v ?? '')),
    },
    {
      field: 'keyspace',
      headerName: t('presetJobs.columns.keyspace') as string,
      width: 170,
      valueGetter: (_v, row) => (row.keyspace === null || row.keyspace === undefined ? -1 : Number(row.keyspace)),
      renderCell: (p) => {
        const job = p.row;
        if (calculatingJobs.has(job.id)) {
          return (
            <Box display="flex" alignItems="center" gap={1}>
              <CircularProgress size={20} />
              <Typography variant="body2" color="text.secondary">
                {t('presetJobs.calculating') as string}
              </Typography>
            </Box>
          );
        }
        if (job.keyspace === null || job.keyspace === undefined) {
          return <Chip label={t('presetJobs.keyspaceNotCalculated') as string} size="small" color="warning" />;
        }
        return (
          <Tooltip title={formatKeyspace(job.keyspace)}>
            <span>{formatKeyspace(job.keyspace)}</span>
          </Tooltip>
        );
      },
    },
    {
      field: 'binary_version',
      headerName: t('presetJobs.columns.binaryVersion') as string,
      width: 140,
      valueGetter: (_v, row) => row.binary_version_name || row.binary_version,
    },
    {
      field: 'wordlists',
      headerName: t('presetJobs.columns.wordlists') as string,
      type: 'number',
      width: 100,
      valueGetter: (_v, row) => row.wordlist_ids?.length || 0,
    },
    {
      field: 'rules',
      headerName: t('presetJobs.columns.rules') as string,
      type: 'number',
      width: 90,
      valueGetter: (_v, row) => row.rule_ids?.length || 0,
    },
    {
      field: 'created_at',
      headerName: t('presetJobs.columns.createdAt') as string,
      width: 180,
      valueGetter: (_v, row) => (row.created_at ? new Date(row.created_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('presetJobs.title') as string}
        actions={
          <Box display="flex" gap={2} flexWrap="wrap">
            {hasJobsWithoutKeyspace && (
              <Button
                variant="outlined"
                onClick={() => recalculateAllKeyspacesMutation.mutate()}
                startIcon={<CalculateIcon />}
                disabled={deleteMutation.isPending || recalculateAllKeyspacesMutation.isPending || recalculateKeyspaceMutation.isPending}
              >
                {recalculateAllKeyspacesMutation.isPending ? t('presetJobs.calculatingKeyspaces') as string : t('presetJobs.calculateAllMissingKeyspaces') as string}
              </Button>
            )}
            <Button
              variant="contained"
              component={RouterLink}
              to={ROUTES.admin.presetJobNew}
              startIcon={<AddIcon />}
              disabled={deleteMutation.isPending}
            >
              {t('presetJobs.createNew') as string}
            </Button>
          </Box>
        }
      />

      {recalculateAllKeyspacesMutation.isPending && (
        <Alert severity="info" sx={{ mb: 2 }}>
          <Box display="flex" alignItems="center" gap={2}>
            <CircularProgress size={20} />
            <Typography>{t('presetJobs.calculatingAllKeyspacesMessage') as string}</Typography>
          </Box>
        </Alert>
      )}

      <DataTable<PresetJob>
        rows={Array.isArray(presetJobs) ? presetJobs : []}
        columns={columns}
        getRowId={(r) => r.id}
        loading={isLoading}
        fetching={(isFetching && !isLoading) || deleteMutation.isPending}
        error={error ? new Error(t('presetJobs.messages.fetchError', { error: error.message }) as string) : undefined}
        onRetry={() => refetch()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client' }}
        rowLinkTo={(job) => ROUTES.admin.presetJobEdit(job.id)}
        rowClassName={(job) => (job.allow_high_priority_override ? 'kh-row-override' : undefined)}
        sx={{
          '& .kh-row-override': {
            boxShadow: (theme) => `inset 3px 0 0 ${theme.palette.error.main}`,
          },
        }}
        rowActionsInlineLimit={3}
        rowActions={(job) => [
          {
            key: 'keyspace',
            label: t('presetJobs.calculateKeyspace') as string,
            icon: <CalculateIcon fontSize="small" color="warning" />,
            hidden: !((job.keyspace === null || job.keyspace === undefined) && !calculatingJobs.has(job.id)),
            disabled: actionsBusy || calculatingJobs.size > 0,
            onClick: (j) => handleRecalculateKeyspace(j.id),
          },
          {
            key: 'edit',
            label: t('common.edit') as string,
            icon: <EditIcon fontSize="small" />,
            disabled: actionsBusy,
            onClick: (j) => navigate(ROUTES.admin.presetJobEdit(j.id)),
          },
          {
            key: 'delete',
            label: t('common.delete') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            disabled: actionsBusy,
            onClick: (j) => handleDelete(j),
          },
        ]}
        emptyState={{ title: t('presetJobs.noJobsFound') as string }}
        tableKey="admin-preset-jobs"
      />
    </Box>
  );
};

export default PresetJobListPage;
