import React, { useState, useEffect } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import {
  Box,
  Typography,
  Button,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  IconButton,
  CircularProgress,
  Alert,
  Tooltip,
  Chip
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import { useTranslation } from 'react-i18next';
import { listJobWorkflows, deleteJobWorkflow } from '../../services/api';
import { JobWorkflow } from '../../types/adminJobs';
import { useConfirm } from '../../hooks';

const JobWorkflowListPage: React.FC = () => {
  const { t } = useTranslation('admin');

  // State
  const [workflows, setWorkflows] = useState<JobWorkflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deleteInProgress, setDeleteInProgress] = useState(false);

  // Dialog hooks
  const { ConfirmDialog, showConfirm } = useConfirm();

  // Load workflows on component mount
  useEffect(() => {
    const fetchWorkflows = async () => {
      try {
        setLoading(true);
        setError(null);

        const data = await listJobWorkflows();
        setWorkflows(data);
      } catch (err) {
        console.error('Error fetching job workflows:', err);
        setError(t('workflows.messages.loadFailed') as string);
      } finally {
        setLoading(false);
      }
    };

    fetchWorkflows();
  }, [t]);

  // Handle workflow deletion
  const handleDelete = async (id: string, name: string) => {
    const confirmed = await showConfirm(
      t('workflows.deleteTitle') as string,
      t('workflows.confirmDelete', { name }) as string
    );

    if (confirmed) {
      try {
        setDeleteInProgress(true);
        await deleteJobWorkflow(id);

        // Remove the deleted workflow from state
        setWorkflows(prev => prev.filter(wf => wf.id !== id));
      } catch (err) {
        console.error('Error deleting workflow:', err);
        setError(t('workflows.messages.deleteFailed') as string);
      } finally {
        setDeleteInProgress(false);
      }
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <ConfirmDialog />

      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3}>
        <Typography variant="h4" gutterBottom>
          {t('workflows.title') as string}
        </Typography>

        <Button
          component={RouterLink}
          to="/admin/job-workflows/new"
          variant="contained"
          color="primary"
          startIcon={<AddIcon />}
          disabled={loading || deleteInProgress}
        >
          {t('workflows.create') as string}
        </Button>
      </Box>
      
      {error && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {error}
        </Alert>
      )}
      
      {loading ? (
        <Box display="flex" justifyContent="center" p={3}>
          <CircularProgress />
        </Box>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('workflows.columns.name') as string}</TableCell>
                <TableCell>{t('workflows.columns.jobCount') as string}</TableCell>
                <TableCell>{t('workflows.columns.highPriority') as string}</TableCell>
                <TableCell>{t('workflows.columns.created') as string}</TableCell>
                <TableCell>{t('workflows.columns.lastUpdated') as string}</TableCell>
                <TableCell align="right">{t('workflows.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {workflows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} align="center">
                    <Typography variant="body1" py={2}>
                      {t('workflows.noWorkflowsFound') as string}
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                workflows.map((workflow) => (
                  <TableRow 
                    key={workflow.id}
                    sx={{
                      ...(workflow.has_high_priority_override && {
                        border: '2px solid red',
                        '& td': { borderColor: 'red' }
                      })
                    }}
                  >
                    <TableCell>
                      <RouterLink to={`/admin/job-workflows/${workflow.id}/edit`} style={{ textDecoration: 'none', color: 'inherit' }}>
                        {workflow.name}
                      </RouterLink>
                    </TableCell>
                    <TableCell>{workflow.steps?.length || 0}</TableCell>
                    <TableCell>
                      {workflow.has_high_priority_override ? (
                        <Chip
                          label={t('workflows.canInterrupt') as string}
                          color="error"
                          size="small"
                          variant="filled"
                        />
                      ) : (
                        <Chip
                          label={t('workflows.normal') as string}
                          size="small"
                          variant="outlined"
                        />
                      )}
                    </TableCell>
                    <TableCell>{new Date(workflow.created_at).toLocaleString()}</TableCell>
                    <TableCell>{new Date(workflow.updated_at).toLocaleString()}</TableCell>
                    <TableCell align="right">
                      <Tooltip title={t('common.edit') as string}>
                        <IconButton
                          component={RouterLink}
                          to={`/admin/job-workflows/${workflow.id}/edit`}
                          disabled={deleteInProgress}
                        >
                          <EditIcon />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={t('common.delete') as string}>
                        <IconButton
                          onClick={() => handleDelete(workflow.id, workflow.name)}
                          disabled={deleteInProgress}
                        >
                          <DeleteIcon />
                        </IconButton>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
};

export default JobWorkflowListPage; 