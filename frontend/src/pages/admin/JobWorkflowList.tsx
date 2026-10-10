import React, { useState, useEffect, useCallback } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { Box, Typography, Button, Chip } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import type { GridColDef } from '@mui/x-data-grid';
import { useTranslation } from 'react-i18next';
import { listJobWorkflows, deleteJobWorkflow } from '../../services/api';
import { JobWorkflow, isLoopbackEligible } from '../../types/adminJobs';
import { DataTable, EntityLink, PageHeader, useConfirm, useToast } from '../../components/ui';
import { ROUTES } from '../../constants/routes';

// Number of steps that actually loop back when the workflow-level master toggle is off:
// the step must be flagged AND its attack must have a separable mutation (GH #64/#78).
const countLoopbackSteps = (workflow: JobWorkflow): number =>
  (workflow.steps || []).filter(
    step => step.loopback_enabled && isLoopbackEligible(step.preset_job_attack_mode, step.preset_job_rule_ids)
  ).length;

const JobWorkflowListPage: React.FC = () => {
  const { t } = useTranslation('admin');

  const toast = useToast();
  const confirm = useConfirm();
  const navigate = useNavigate();

  // State
  const [workflows, setWorkflows] = useState<JobWorkflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deleteInProgress, setDeleteInProgress] = useState(false);

  const fetchWorkflows = useCallback(async () => {
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
  }, [t]);

  // Load workflows on component mount
  useEffect(() => {
    fetchWorkflows();
  }, [fetchWorkflows]);

  // Handle workflow deletion
  const handleDelete = async (id: string, name: string) => {
    const confirmed = await confirm({
      title: t('workflows.deleteTitle') as string,
      message: t('workflows.confirmDelete', { name }) as string,
      severity: 'danger',
      confirmLabel: t('common.delete') as string,
    });

    if (confirmed) {
      try {
        setDeleteInProgress(true);
        await deleteJobWorkflow(id);

        // Remove the deleted workflow from state
        setWorkflows(prev => prev.filter(wf => wf.id !== id));
      } catch (err) {
        console.error('Error deleting workflow:', err);
        toast.error(t('workflows.messages.deleteFailed') as string);
      } finally {
        setDeleteInProgress(false);
      }
    }
  };

  const columns: GridColDef<JobWorkflow>[] = [
    {
      field: 'name',
      headerName: t('workflows.columns.name') as string,
      flex: 1.5,
      minWidth: 200,
      renderCell: (p) => <EntityLink type="workflow" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'step_count',
      headerName: t('workflows.columns.jobCount') as string,
      type: 'number',
      width: 90,
      align: 'left',
      headerAlign: 'left',
      valueGetter: (_v, row) => row.step_count ?? row.steps?.length ?? 0,
    },
    {
      field: 'has_high_priority_override',
      headerName: t('workflows.columns.highPriority') as string,
      width: 150,
      renderCell: (p) =>
        p.row.has_high_priority_override ? (
          <Chip label={t('workflows.canInterrupt') as string} color="error" size="small" variant="filled" />
        ) : (
          <Chip label={t('workflows.normal') as string} size="small" variant="outlined" />
        ),
    },
    {
      field: 'loopback',
      headerName: t('workflows.columns.loopback') as string,
      width: 150,
      sortable: false,
      renderCell: (p) => {
        const workflow = p.row;
        const count = countLoopbackSteps(workflow);
        if (workflow.loopback_all_eligible) {
          return <Chip label={t('workflows.loopbackAllEligible') as string} color="secondary" size="small" variant="outlined" />;
        }
        if (count > 0) {
          return <Chip label={t('workflows.loopbackStepCount', { count }) as string} color="secondary" size="small" variant="outlined" />;
        }
        return <Typography variant="body2" color="text.secondary">—</Typography>;
      },
    },
    {
      field: 'created_at',
      headerName: t('workflows.columns.created') as string,
      width: 180,
      valueGetter: (_v, row) => (row.created_at ? new Date(row.created_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleString() : ''),
    },
    {
      field: 'updated_at',
      headerName: t('workflows.columns.lastUpdated') as string,
      width: 180,
      valueGetter: (_v, row) => (row.updated_at ? new Date(row.updated_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('workflows.title') as string}
        actions={
          <Button
            component={RouterLink}
            to={ROUTES.admin.workflowNew}
            variant="contained"
            color="primary"
            startIcon={<AddIcon />}
            disabled={loading || deleteInProgress}
          >
            {t('workflows.create') as string}
          </Button>
        }
      />

      <DataTable<JobWorkflow>
        rows={workflows}
        columns={columns}
        getRowId={(r) => r.id}
        loading={loading && workflows.length === 0}
        fetching={(loading && workflows.length > 0) || deleteInProgress}
        error={error ? new Error(error) : undefined}
        onRetry={() => fetchWorkflows()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client' }}
        rowLinkTo={(wf) => ROUTES.admin.workflowEdit(wf.id)}
        rowClassName={(wf) => (wf.has_high_priority_override ? 'kh-row-override' : undefined)}
        sx={{
          '& .kh-row-override': {
            boxShadow: (theme) => `inset 3px 0 0 ${theme.palette.error.main}`,
          },
        }}
        rowActions={() => [
          {
            key: 'edit',
            label: t('common.edit') as string,
            icon: <EditIcon fontSize="small" />,
            disabled: deleteInProgress,
            onClick: (wf) => navigate(ROUTES.admin.workflowEdit(wf.id)),
          },
          {
            key: 'delete',
            label: t('common.delete') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            disabled: deleteInProgress,
            onClick: (wf) => handleDelete(wf.id, wf.name),
          },
        ]}
        emptyState={{ title: t('workflows.noWorkflowsFound') as string }}
        tableKey="admin-workflows"
      />
    </Box>
  );
};

export default JobWorkflowListPage;
