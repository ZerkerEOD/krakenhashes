import React, { useState } from 'react';
import { Box, Typography, Button, CircularProgress, Alert, Paper, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, IconButton, Chip, Tooltip } from '@mui/material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link as RouterLink } from 'react-router-dom';
import { Add as AddIcon, Edit as EditIcon, Delete as DeleteIcon, Calculate as CalculateIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';

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
  const { enqueueSnackbar } = useSnackbar();
  const [calculatingJobs, setCalculatingJobs] = useState<Set<string>>(new Set());

  // Correct useQuery signature: options object only
  const { data: presetJobs, isLoading, error } = useQuery<PresetJob[], Error>({
    queryKey: ['presetJobs'],
    queryFn: listPresetJobs,
  });

  // Correct useMutation signature: options object with mutationFn
  const deleteMutation = useMutation<void, Error, string>({
    mutationFn: deletePresetJob, // Specify mutation function here
    onSuccess: () => {
      enqueueSnackbar(t('presetJobs.messages.deleteSuccess') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: Error) => {
      enqueueSnackbar(t('presetJobs.messages.deleteFailed', { error: err.message }) as string, { variant: 'error' });
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
      enqueueSnackbar(t('presetJobs.messages.keyspaceRecalculateSuccess') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || t('common.unknownError');
      enqueueSnackbar(t('presetJobs.messages.keyspaceRecalculateFailed', { error: errorMessage }) as string, { variant: 'error' });
    },
  });

  const recalculateAllKeyspacesMutation = useMutation<any, Error>({
    mutationFn: async () => {
      const response = await api.post('/api/admin/preset-jobs/recalculate-all-keyspaces');
      return response.data;
    },
    onSuccess: (data) => {
      const message = t('presetJobs.messages.keyspaceAllComplete', { updated: data.updated, skipped: data.skipped, failed: data.failed }) as string;
      enqueueSnackbar(message, { variant: data.failed > 0 ? 'warning' : 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || t('common.unknownError');
      enqueueSnackbar(t('presetJobs.messages.keyspaceAllFailed', { error: errorMessage }) as string, { variant: 'error' });
    },
  });

  const handleDelete = (id: string) => {
    if (window.confirm(t('presetJobs.confirmDelete') as string)) {
      deleteMutation.mutate(id);
    }
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

  return (
    <Box sx={{ p: 3 }}>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3}>
        <Typography variant="h4">
          {t('presetJobs.title') as string}
        </Typography>
        <Box display="flex" gap={2}>
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
            to="/admin/preset-jobs/new"
            startIcon={<AddIcon />}
            disabled={deleteMutation.isPending}
          >
            {t('presetJobs.createNew') as string}
          </Button>
        </Box>
      </Box>

      {(isLoading || deleteMutation.isPending) && <CircularProgress />}
      {error && <Alert severity="error">{t('presetJobs.messages.fetchError', { error: error.message }) as string}</Alert>}
      {deleteMutation.error && <Alert severity="error">{t('presetJobs.messages.deleteError', { error: deleteMutation.error.message }) as string}</Alert>}

      {recalculateAllKeyspacesMutation.isPending && (
        <Alert severity="info" sx={{ mb: 2 }}>
          <Box display="flex" alignItems="center" gap={2}>
            <CircularProgress size={20} />
            <Typography>{t('presetJobs.calculatingAllKeyspacesMessage') as string}</Typography>
          </Box>
        </Alert>
      )} 

      {!isLoading && !error && presetJobs && (
        <TableContainer component={Paper}>
          <Table sx={{ minWidth: 650 }} aria-label="preset jobs table">
            <TableHead>
              <TableRow>
                <TableCell>{t('presetJobs.columns.name') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.attackMode') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.priority') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.highPriorityOverride') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.maxAgents') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.keyspace') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.binaryVersion') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.wordlists') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.rules') as string}</TableCell>
                <TableCell>{t('presetJobs.columns.createdAt') as string}</TableCell>
                <TableCell align="right">{t('presetJobs.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {Array.isArray(presetJobs) && presetJobs.length === 0 && (
                <TableRow>
                  <TableCell colSpan={11} align="center">
                    {t('presetJobs.noJobsFound') as string}
                  </TableCell>
                </TableRow>
              )}
              {Array.isArray(presetJobs) && presetJobs?.map((job: PresetJob) => ( 
                <TableRow
                  key={job.id}
                  sx={{ 
                    '&:last-child td, &:last-child th': { border: 0 },
                    ...(job.allow_high_priority_override && {
                      border: '2px solid red',
                      '& td': { borderColor: 'red' }
                    })
                  }}
                >
                  <TableCell component="th" scope="row">
                    {job.name}
                  </TableCell>
                  <TableCell>{formatAttackMode(job.attack_mode)}</TableCell>
                  <TableCell>{job.priority}</TableCell>
                  <TableCell>
                    {job.allow_high_priority_override ? (
                      <Tooltip title={t('presetJobs.canInterruptRunningJobs') as string}>
                        <Chip
                          label={t('common.yes') as string}
                          size="small"
                          color="error"
                          variant="filled"
                        />
                      </Tooltip>
                    ) : (
                      <Chip
                        label={t('common.no') as string}
                        size="small"
                        variant="outlined"
                      />
                    )}
                  </TableCell>
                  <TableCell>{job.max_agents === 0 ? t('common.unlimited') as string : job.max_agents}</TableCell>
                  <TableCell>
                    {calculatingJobs.has(job.id) ? (
                      <Box display="flex" alignItems="center" gap={1}>
                        <CircularProgress size={20} />
                        <Typography variant="body2" color="text.secondary">
                          {t('presetJobs.calculating') as string}
                        </Typography>
                      </Box>
                    ) : job.keyspace === null || job.keyspace === undefined ? (
                      <Chip
                        label={t('presetJobs.keyspaceNotCalculated') as string}
                        size="small"
                        color="warning"
                      />
                    ) : (
                      <Tooltip title={formatKeyspace(job.keyspace)}>
                        <span>{formatKeyspace(job.keyspace)}</span>
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell>{job.binary_version_name || job.binary_version}</TableCell>
                  <TableCell>{job.wordlist_ids?.length || 0}</TableCell>
                  <TableCell>{job.rule_ids?.length || 0}</TableCell>
                  <TableCell>{new Date(job.created_at).toLocaleString()}</TableCell>
                  <TableCell align="right">
                    {(job.keyspace === null || job.keyspace === undefined) && !calculatingJobs.has(job.id) && (
                      <Tooltip title={t('presetJobs.calculateKeyspace') as string}>
                        <IconButton
                          onClick={() => handleRecalculateKeyspace(job.id)}
                          aria-label={t('presetJobs.calculateKeyspace') as string}
                          disabled={deleteMutation.isPending || recalculateKeyspaceMutation.isPending || calculatingJobs.size > 0}
                          color="warning"
                        >
                          <CalculateIcon />
                        </IconButton>
                      </Tooltip>
                    )}
                    <IconButton
                      component={RouterLink}
                      to={`/admin/preset-jobs/${job.id}/edit`}
                      aria-label={t('common.edit') as string}
                      disabled={deleteMutation.isPending || recalculateKeyspaceMutation.isPending}
                    >
                      <EditIcon />
                    </IconButton>
                    <IconButton
                      onClick={() => handleDelete(job.id)}
                      aria-label={t('common.delete') as string}
                      disabled={deleteMutation.isPending || recalculateKeyspaceMutation.isPending}
                    >
                      <DeleteIcon />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
};

export default PresetJobListPage; 