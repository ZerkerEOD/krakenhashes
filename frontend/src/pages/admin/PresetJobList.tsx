import React, { useState } from 'react';
import { Box, Typography, Button, CircularProgress, Alert, Paper, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, IconButton, Chip, Tooltip } from '@mui/material';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link as RouterLink } from 'react-router-dom';
import { Add as AddIcon, Edit as EditIcon, Delete as DeleteIcon, Calculate as CalculateIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';

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
      enqueueSnackbar('Preset job deleted successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: Error) => {
      enqueueSnackbar(`Failed to delete preset job: ${err.message}`, { variant: 'error' });
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
      enqueueSnackbar('Keyspace recalculated successfully', { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || 'Unknown error';
      enqueueSnackbar(`Failed to recalculate keyspace: ${errorMessage}`, { variant: 'error' });
    },
  });

  const recalculateAllKeyspacesMutation = useMutation<any, Error>({
    mutationFn: async () => {
      const response = await api.post('/api/admin/preset-jobs/recalculate-all-keyspaces');
      return response.data;
    },
    onSuccess: (data) => {
      const message = `Keyspace calculation complete: ${data.updated} updated, ${data.skipped} skipped, ${data.failed} failed`;
      enqueueSnackbar(message, { variant: data.failed > 0 ? 'warning' : 'success' });
      queryClient.invalidateQueries({ queryKey: ['presetJobs'] });
    },
    onError: (err: any) => {
      const errorMessage = err.response?.data?.error || err.message || 'Unknown error';
      enqueueSnackbar(`Failed to recalculate keyspaces: ${errorMessage}`, { variant: 'error' });
    },
  });

  const handleDelete = (id: string) => {
    if (window.confirm('Are you sure you want to delete this preset job?')) {
      deleteMutation.mutate(id);
    }
  };

  const handleRecalculateKeyspace = (id: string) => {
    recalculateKeyspaceMutation.mutate(id);
  };

  // Helper function to format keyspace
  const formatKeyspace = (keyspace: number | null | undefined): string => {
    if (keyspace === null || keyspace === undefined) {
      return 'Not calculated';
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
          Preset Job Management
        </Typography>
        <Box display="flex" gap={2}>
          {hasJobsWithoutKeyspace && (
            <Button
              variant="outlined"
              onClick={() => recalculateAllKeyspacesMutation.mutate()}
              startIcon={<CalculateIcon />}
              disabled={deleteMutation.isPending || recalculateAllKeyspacesMutation.isPending || recalculateKeyspaceMutation.isPending}
            >
              {recalculateAllKeyspacesMutation.isPending ? 'Calculating...' : 'Calculate All Missing Keyspaces'}
            </Button>
          )}
          <Button
            variant="contained"
            component={RouterLink}
            to="/admin/preset-jobs/new"
            startIcon={<AddIcon />}
            disabled={deleteMutation.isPending} 
          >
            Create New Preset Job
          </Button>
        </Box>
      </Box>

      {(isLoading || deleteMutation.isPending) && <CircularProgress />} 
      {error && <Alert severity="error">Error fetching preset jobs: {error.message}</Alert>}
      {deleteMutation.error && <Alert severity="error">Error deleting preset job: {deleteMutation.error.message}</Alert>}
      
      {recalculateAllKeyspacesMutation.isPending && (
        <Alert severity="info" sx={{ mb: 2 }}>
          <Box display="flex" alignItems="center" gap={2}>
            <CircularProgress size={20} />
            <Typography>Calculating keyspaces for all preset jobs. This may take a few moments...</Typography>
          </Box>
        </Alert>
      )} 

      {!isLoading && !error && presetJobs && (
        <TableContainer component={Paper}>
          <Table sx={{ minWidth: 650 }} aria-label="preset jobs table">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Attack Mode</TableCell>
                <TableCell>Priority</TableCell>
                <TableCell>High Priority Override</TableCell>
                <TableCell>Max Agents</TableCell>
                <TableCell>Keyspace</TableCell>
                <TableCell>Binary Version</TableCell>
                <TableCell>Wordlists</TableCell>
                <TableCell>Rules</TableCell>
                <TableCell>Created At</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {Array.isArray(presetJobs) && presetJobs.length === 0 && ( 
                <TableRow>
                  <TableCell colSpan={11} align="center">
                    No preset jobs found.
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
                      <Tooltip title="This job can interrupt running jobs">
                        <Chip 
                          label="Yes" 
                          size="small" 
                          color="error"
                          variant="filled"
                        />
                      </Tooltip>
                    ) : (
                      <Chip 
                        label="No" 
                        size="small" 
                        variant="outlined"
                      />
                    )}
                  </TableCell>
                  <TableCell>{job.max_agents === 0 ? 'Unlimited' : job.max_agents}</TableCell>
                  <TableCell>
                    {calculatingJobs.has(job.id) ? (
                      <Box display="flex" alignItems="center" gap={1}>
                        <CircularProgress size={20} />
                        <Typography variant="body2" color="text.secondary">
                          Calculating...
                        </Typography>
                      </Box>
                    ) : job.keyspace === null || job.keyspace === undefined ? (
                      <Chip 
                        label="Not calculated" 
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
                      <Tooltip title="Calculate keyspace">
                        <IconButton 
                          onClick={() => handleRecalculateKeyspace(job.id)} 
                          aria-label="calculate keyspace"
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
                      aria-label="edit"
                      disabled={deleteMutation.isPending || recalculateKeyspaceMutation.isPending}
                    >
                      <EditIcon />
                    </IconButton>
                    <IconButton 
                      onClick={() => handleDelete(job.id)} 
                      aria-label="delete"
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