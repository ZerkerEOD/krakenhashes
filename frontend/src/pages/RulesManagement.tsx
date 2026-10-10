/**
 * Rules Management page for KrakenHashes frontend.
 *
 * Features:
 *   - View rules
 *   - Add new rules
 *   - Update rule information
 *   - Delete rules
 *   - Enable/disable rules
 */
import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Typography,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  MenuItem,
  Divider,
  CircularProgress,
  Alert,
  FormControl,
  InputLabel,
  Select
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Edit as EditIcon,
  Refresh as RefreshIcon,
  CloudDownload as DownloadIcon,
  Add as AddIcon,
} from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import FileUpload from '../components/common/FileUpload';
import { Rule, RuleStatus, RuleType } from '../types/rules';
import { DeletionImpact } from '../types/wordlists';
import * as ruleService from '../services/rules';
import { DataTable, EntityLink, PageHeader, StatusChip, useToast } from '../components/ui';
import { formatFileSize, formatAttackMode } from '../utils/formatters';

export default function RulesManagement() {
  const { t } = useTranslation('admin');
  const [rules, setRules] = useState<Rule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [openUploadDialog, setOpenUploadDialog] = useState(false);
  const [openEditDialog, setOpenEditDialog] = useState(false);
  const [currentRule, setCurrentRule] = useState<Rule | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [nameEdit, setNameEdit] = useState('');
  const [descriptionEdit, setDescriptionEdit] = useState('');
  const [ruleTypeEdit, setRuleTypeEdit] = useState<RuleType>(RuleType.HASHCAT);
  const [typeFilter, setTypeFilter] = useState<'' | RuleType>('');
  const toast = useToast();
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);
  const [selectedRuleType, setSelectedRuleType] = useState<RuleType>(RuleType.HASHCAT);
  const [isLoading, setIsLoading] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [ruleToDelete, setRuleToDelete] = useState<{id: string, name: string} | null>(null);
  const [deletionImpact, setDeletionImpact] = useState<DeletionImpact | null>(null);
  const [confirmationId, setConfirmationId] = useState('');
  const [isCheckingImpact, setIsCheckingImpact] = useState(false);

  // Fetch rules
  const fetchRules = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await ruleService.getRules();
      setRules(response.data);
    } catch (err) {
      console.error('Error fetching rules:', err);
      setError(t('rules.errors.loadFailed') as string);
      toast.error(t('rules.errors.loadFailed') as string);
    } finally {
      setLoading(false);
    }
  }, [toast, t]);

  useEffect(() => {
    fetchRules();
  }, [fetchRules]);

  // Handle file upload
  const handleUploadRule = async (formData: FormData) => {
    try {
      setIsLoading(true);

      // Add the rule type to the form data
      formData.append('rule_type', selectedRuleType);

      // Add required fields if not present
      if (!formData.has('name')) {
        const file = formData.get('file') as File;
        if (file) {
          // Extract name without extension (everything before the last dot)
          const lastDotIndex = file.name.lastIndexOf('.');
          const nameWithoutExt = lastDotIndex > 0 ? file.name.substring(0, lastDotIndex) : file.name;
          formData.append('name', nameWithoutExt);
        }
      }

      // Remove format field as it's not needed for rules
      if (formData.has('format')) {
        formData.delete('format');
      }

      console.debug('[Rule Upload] Sending form data with rule_type:', selectedRuleType);
      console.debug('[Rule Upload] Form data contents:',
        Array.from(formData.entries()).reduce((obj, [key, val]) => {
          obj[key] = key === 'file' ? '(file content)' : val;
          return obj;
        }, {} as Record<string, any>)
      );

      const response = await ruleService.uploadRule(formData, (progress, eta, speed) => {
        // Update progress in the FileUpload component
        const progressEvent = new CustomEvent('upload-progress', { detail: { progress, eta, speed } });
        document.dispatchEvent(progressEvent);
      });
      console.debug('[Rule Upload] Upload successful:', response);

      // Check if the response indicates a duplicate rule
      if (response.data.duplicate) {
        toast.info(t('rules.messages.duplicateRule', { name: response.data.name }) as string);
      } else {
        toast.success(t('rules.messages.uploadSuccess') as string);
      }

      setUploadDialogOpen(false);
      fetchRules();
    } catch (error) {
      console.error('Error uploading rule:', error);
      toast.error(t('rules.errors.uploadFailed') as string);
    } finally {
      setIsLoading(false);
    }
  };

  // Handle rule deletion
  const handleDelete = async (id: string, name: string, confirmId?: number) => {
    try {
      await ruleService.deleteRule(id, confirmId);
      toast.success(t('rules.messages.deleteSuccess', { name }) as string);
      fetchRules();
    } catch (err: any) {
      console.error('Error deleting rule:', err);
      // Extract error message from axios response
      const errorMessage = err.response?.data?.error || t('rules.errors.deleteFailed') as string;
      toast.error(errorMessage);
    } finally {
      closeDeleteDialog();
    }
  };

  // Open delete confirmation dialog - first check for deletion impact
  const openDeleteDialog = async (id: string, name: string) => {
    setRuleToDelete({ id, name });
    setDeleteDialogOpen(true);
    setIsCheckingImpact(true);
    setDeletionImpact(null);
    setConfirmationId('');

    try {
      const response = await ruleService.getRuleDeletionImpact(id);
      setDeletionImpact(response.data);
    } catch (err: any) {
      console.error('Error getting deletion impact:', err);
      // If we can't get the impact, still allow deletion with simple confirmation
      setDeletionImpact(null);
    } finally {
      setIsCheckingImpact(false);
    }
  };

  // Close delete confirmation dialog
  const closeDeleteDialog = () => {
    setDeleteDialogOpen(false);
    setRuleToDelete(null);
    setDeletionImpact(null);
    setConfirmationId('');
  };

  // Check if confirmation ID matches for cascade delete
  const isConfirmationValid = () => {
    if (!deletionImpact?.has_cascading_impact) return true;
    return confirmationId === String(deletionImpact.resource_id);
  };

  // Handle rule download
  const handleDownload = async (id: string, name: string) => {
    try {
      const response = await ruleService.downloadRule(id);

      // Create and trigger download
      const url = window.URL.createObjectURL(new Blob([response.data]));
      const link = document.createElement('a');
      link.href = url;
      link.setAttribute('download', `${name}.rule`);
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    } catch (err) {
      console.error('Error downloading rule:', err);
      toast.error(t('rules.errors.downloadFailed') as string);
    }
  };

  // Handle edit button click
  const handleEditClick = (rule: Rule) => {
    setCurrentRule(rule);
    setNameEdit(rule.name);
    setDescriptionEdit(rule.description);
    setRuleTypeEdit(rule.rule_type);
    setOpenEditDialog(true);
  };

  // Handle save edit
  const handleSaveEdit = async () => {
    if (!currentRule) return;

    try {
      console.debug('[Rule Edit] Updating rule:', currentRule.id, {
        name: nameEdit,
        description: descriptionEdit,
        rule_type: ruleTypeEdit
      });

      const response = await ruleService.updateRule(currentRule.id, {
        name: nameEdit,
        description: descriptionEdit,
        rule_type: ruleTypeEdit
      });

      console.debug('[Rule Edit] Update successful:', response);
      toast.success(t('rules.messages.updateSuccess') as string);
      setOpenEditDialog(false);
      fetchRules();
    } catch (err: any) {
      console.error('[Rule Edit] Error updating rule:', err);

      if (err.response?.status === 401) {
        toast.error(t('rules.errors.sessionExpired') as string);
      } else {
        toast.error(t('rules.errors.updateFailed', { error: err.response?.data?.message || err.message }) as string);
      }
    }
  };

  // Handle rule verification
  const handleVerify = async (id: string, name: string) => {
    try {
      setIsLoading(true);
      await ruleService.verifyRule(id, 'verified');
      toast.success(t('rules.messages.verifySuccess', { name }) as string);
      fetchRules();
    } catch (err) {
      console.error('Error verifying rule:', err);
      toast.error(t('rules.errors.verifyFailed') as string);
    } finally {
      setIsLoading(false);
    }
  };

  // Filter rules based on search term and type (sorting is handled by the table)
  const filteredRules = useMemo(() => {
    const term = searchTerm.toLowerCase();
    return rules.filter((rule) => {
      const matchesSearch = rule.name.toLowerCase().includes(term) ||
        (rule.description || '').toLowerCase().includes(term);
      return matchesSearch && (!typeFilter || rule.rule_type === typeFilter);
    });
  }, [rules, searchTerm, typeFilter]);

  const columns: GridColDef<Rule>[] = [
    {
      field: 'name',
      headerName: t('rules.columns.name') as string,
      flex: 2,
      minWidth: 220,
      renderCell: (p) => (
        <Box sx={{ py: 1, minWidth: 0 }}>
          <Typography variant="body2" fontWeight="medium">
            {p.row.name}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {p.row.description || t('rules.noDescription') as string}
          </Typography>
        </Box>
      ),
    },
    {
      field: 'verification_status',
      headerName: t('rules.columns.status') as string,
      width: 150,
      renderCell: (p) =>
        p.row.verification_status === RuleStatus.FAILED && p.row.missing_since ? (
          <StatusChip
            entity="verification"
            status={p.row.verification_status}
            label={t('rules.status.missing') as string}
            tooltip={t('rules.status.missingSince', { date: new Date(p.row.missing_since).toLocaleString() }) as string}
          />
        ) : (
          <StatusChip
            entity="verification"
            status={p.row.verification_status}
            label={t(`rules.status.${p.row.verification_status}`, { defaultValue: p.row.verification_status }) as string}
          />
        ),
    },
    {
      field: 'rule_type',
      headerName: t('rules.columns.type') as string,
      width: 120,
      renderCell: (p) => (
        <Chip
          label={p.row.rule_type}
          size="small"
          color="primary"
          variant="outlined"
          sx={{ textTransform: 'capitalize' }}
        />
      ),
    },
    {
      field: 'file_size',
      headerName: t('rules.columns.size') as string,
      type: 'number',
      width: 110,
      valueFormatter: (v) => formatFileSize(Number(v) || 0),
    },
    {
      field: 'rule_count',
      headerName: t('rules.columns.ruleCount') as string,
      type: 'number',
      width: 120,
      valueFormatter: (v) => (Number(v) || 0).toLocaleString(),
    },
    {
      field: 'updated_at',
      headerName: t('rules.columns.updated') as string,
      width: 130,
      valueGetter: (_v, row) => (row.updated_at ? new Date(row.updated_at).getTime() : 0),
      valueFormatter: (v) => (v ? new Date(Number(v)).toLocaleDateString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('rules.title') as string}
        description={t('rules.description') as string}
        actions={
          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
            <Button
              variant="contained"
              startIcon={<AddIcon />}
              onClick={() => setUploadDialogOpen(true)}
              disabled={isLoading}
            >
              {t('rules.uploadRule') as string}
            </Button>
            <Button
              variant="outlined"
              startIcon={<RefreshIcon />}
              onClick={() => fetchRules()}
            >
              {t('rules.refresh') as string}
            </Button>
          </Box>
        }
      />

      <DataTable<Rule>
        rows={filteredRules}
        columns={columns}
        getRowId={(r) => r.id}
        loading={loading && rules.length === 0}
        fetching={loading && rules.length > 0}
        error={error && rules.length === 0 ? error : undefined}
        onRetry={() => fetchRules()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client', initial: [{ field: 'updated_at', sort: 'desc' }] }}
        toolbar={{
          search: {
            value: searchTerm,
            onChange: setSearchTerm,
            placeholder: t('rules.searchPlaceholder') as string,
            debounceMs: 150,
          },
          filters: (
            <FormControl size="small" sx={{ minWidth: 160 }}>
              <InputLabel id="rule-type-filter-label">{t('rules.columns.type') as string}</InputLabel>
              <Select
                labelId="rule-type-filter-label"
                value={typeFilter}
                label={t('rules.columns.type') as string}
                onChange={(e) => setTypeFilter(e.target.value as '' | RuleType)}
              >
                <MenuItem value="">{t('rules.tabs.all') as string}</MenuItem>
                <MenuItem value={RuleType.HASHCAT}>{t('rules.tabs.hashcat') as string}</MenuItem>
                <MenuItem value={RuleType.JOHN}>{t('rules.tabs.john') as string}</MenuItem>
              </Select>
            </FormControl>
          ),
        }}
        rowActionsInlineLimit={3}
        rowActions={(rule) => [
          {
            key: 'download',
            label: t('rules.tooltips.download') as string,
            icon: <DownloadIcon fontSize="small" />,
            disabled: rule.verification_status !== 'verified',
            onClick: (r) => handleDownload(r.id, r.name),
          },
          {
            key: 'edit',
            label: t('rules.tooltips.edit') as string,
            icon: <EditIcon fontSize="small" />,
            onClick: (r) => handleEditClick(r),
          },
          {
            key: 'delete',
            label: t('rules.tooltips.delete') as string,
            icon: <DeleteIcon fontSize="small" />,
            danger: true,
            onClick: (r) => openDeleteDialog(r.id, r.name),
          },
        ]}
        emptyState={{
          title: t('rules.noRulesFound') as string,
          description: searchTerm ? t('rules.tryDifferentSearch') as string : t('rules.uploadToGetStarted') as string,
        }}
        tableKey="rules"
      />

      {/* Upload Dialog */}
      <Dialog
        open={uploadDialogOpen}
        onClose={() => setUploadDialogOpen(false)}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>{t('rules.dialogs.upload.title') as string}</DialogTitle>
        <DialogContent>
          <FileUpload
            title={t('rules.dialogs.upload.fileUploadTitle') as string}
            description={t('rules.dialogs.upload.fileUploadDescription') as string}
            acceptedFileTypes=".rule,.rules,.txt,text/plain"
            onUpload={handleUploadRule}
            uploadButtonText={t('rules.uploadRule') as string}
            additionalFields={
              <FormControl fullWidth margin="normal">
                <InputLabel id="rule-type-label">{t('rules.fields.ruleType') as string}</InputLabel>
                <Select
                  labelId="rule-type-label"
                  id="rule-type"
                  name="rule_type"
                  value={selectedRuleType}
                  onChange={(e) => setSelectedRuleType(e.target.value as RuleType)}
                  label={t('rules.fields.ruleType') as string}
                >
                  <MenuItem value={RuleType.HASHCAT}>{t('rules.types.hashcat') as string}</MenuItem>
                  <MenuItem value={RuleType.JOHN}>{t('rules.types.john') as string}</MenuItem>
                </Select>
              </FormControl>
            }
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadDialogOpen(false)} color="primary">
            {t('common.cancel') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog
        open={openEditDialog}
        onClose={() => setOpenEditDialog(false)}
        aria-labelledby="edit-dialog-title"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="edit-dialog-title">{t('rules.dialogs.edit.title') as string}</DialogTitle>
        <DialogContent>
          <TextField
            margin="dense"
            label={t('rules.fields.name') as string}
            fullWidth
            value={nameEdit}
            onChange={(e) => setNameEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <TextField
            margin="dense"
            label={t('rules.fields.description') as string}
            fullWidth
            multiline
            rows={3}
            value={descriptionEdit}
            onChange={(e) => setDescriptionEdit(e.target.value)}
            sx={{ mb: 2 }}
          />
          <FormControl fullWidth margin="dense" sx={{ mb: 2 }}>
            <InputLabel id="edit-rule-type-label">{t('rules.fields.ruleType') as string}</InputLabel>
            <Select
              labelId="edit-rule-type-label"
              id="edit-rule-type"
              value={ruleTypeEdit}
              onChange={(e) => setRuleTypeEdit(e.target.value as RuleType)}
              label={t('rules.fields.ruleType') as string}
            >
              <MenuItem value={RuleType.HASHCAT}>{t('rules.types.hashcat') as string}</MenuItem>
              <MenuItem value={RuleType.JOHN}>{t('rules.types.john') as string}</MenuItem>
            </Select>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpenEditDialog(false)}>
            {t('common.cancel') as string}
          </Button>
          <Button onClick={handleSaveEdit} variant="contained" color="primary">
            {t('common.saveChanges') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog
        open={deleteDialogOpen}
        onClose={closeDeleteDialog}
        aria-labelledby="delete-dialog-title"
        aria-describedby="delete-dialog-description"
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle id="delete-dialog-title">
          {deletionImpact?.has_cascading_impact ? t('rules.dialogs.delete.cascadeTitle') as string : t('rules.dialogs.delete.title') as string}
        </DialogTitle>
        <DialogContent>
          {isCheckingImpact ? (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', py: 3 }}>
              <CircularProgress size={24} sx={{ mr: 2 }} />
              <Typography>{t('rules.dialogs.delete.checkingDependencies') as string}</Typography>
            </Box>
          ) : deletionImpact?.has_cascading_impact ? (
            <Box>
              <Alert severity="warning" sx={{ mb: 2 }}>
                {t('rules.dialogs.delete.cascadeWarning', { name: ruleToDelete?.name }) as string}
              </Alert>

              {deletionImpact.summary.total_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('rules.dialogs.delete.jobsCount', { count: deletionImpact.summary.total_jobs }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.jobs.slice(0, 5).map((job) => (
                      <li key={job.id}>
                        <Typography variant="body2" color="text.secondary">
                          <EntityLink type="job" id={job.id} label={job.name} /> ({job.status}) -{' '}
                          {job.hashlist_name ? (
                            <EntityLink type="hashlist" id={job.hashlist_id ?? undefined} label={job.hashlist_name} />
                          ) : (
                            t('rules.dialogs.delete.noHashlist') as string
                          )}
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('rules.dialogs.delete.andMore', { count: deletionImpact.summary.total_jobs - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_preset_jobs > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('rules.dialogs.delete.presetJobsCount', { count: deletionImpact.summary.total_preset_jobs }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.preset_jobs.slice(0, 5).map((pj) => (
                      <li key={pj.id}>
                        <Typography variant="body2" color="text.secondary">
                          <EntityLink type="preset_job" id={pj.id} label={pj.name} /> ({formatAttackMode(pj.attack_mode)})
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_preset_jobs > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('rules.dialogs.delete.andMore', { count: deletionImpact.summary.total_preset_jobs - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflow_steps > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('rules.dialogs.delete.workflowStepsCount', { count: deletionImpact.summary.total_workflow_steps }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflow_steps.slice(0, 5).map((step, idx) => (
                      <li key={`${step.workflow_id}-${step.step_order}-${idx}`}>
                        <Typography variant="body2" color="text.secondary">
                          <EntityLink type="workflow" id={step.workflow_id} label={step.workflow_name} /> → {t('rules.dialogs.delete.step', { order: step.step_order }) as string} (<EntityLink type="preset_job" id={step.preset_job_id} label={step.preset_job_name} />)
                        </Typography>
                      </li>
                    ))}
                    {deletionImpact.summary.total_workflow_steps > 5 && (
                      <li>
                        <Typography variant="body2" color="text.secondary">
                          {t('rules.dialogs.delete.andMore', { count: deletionImpact.summary.total_workflow_steps - 5 }) as string}
                        </Typography>
                      </li>
                    )}
                  </Box>
                </Box>
              )}

              {deletionImpact.summary.total_workflows_to_delete > 0 && (
                <Box sx={{ mb: 2 }}>
                  <Typography variant="subtitle2" color="error">
                    {t('rules.dialogs.delete.emptyWorkflowsCount', { count: deletionImpact.summary.total_workflows_to_delete }) as string}
                  </Typography>
                  <Box component="ul" sx={{ mt: 0.5, pl: 2, mb: 0 }}>
                    {deletionImpact.impact.workflows_to_delete.map((wf) => (
                      <li key={wf.id}>
                        <Typography variant="body2" color="text.secondary">
                          <EntityLink type="workflow" id={wf.id} label={wf.name} />
                        </Typography>
                      </li>
                    ))}
                  </Box>
                </Box>
              )}

              <Divider sx={{ my: 2 }} />

              <Typography variant="body2" sx={{ mb: 1 }}>
                {t('rules.dialogs.delete.confirmationPrompt', { id: deletionImpact.resource_id }) as string}
              </Typography>
              <TextField
                fullWidth
                size="small"
                placeholder={t('rules.dialogs.delete.confirmationPlaceholder', { id: deletionImpact.resource_id }) as string}
                value={confirmationId}
                onChange={(e) => setConfirmationId(e.target.value)}
                error={confirmationId !== '' && !isConfirmationValid()}
                helperText={confirmationId !== '' && !isConfirmationValid() ? t('rules.dialogs.delete.idMismatch') as string : ''}
              />
            </Box>
          ) : (
            <Typography variant="body1" id="delete-dialog-description">
              {t('rules.dialogs.delete.confirmation', { name: ruleToDelete?.name }) as string}
            </Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={closeDeleteDialog}>{t('common.cancel') as string}</Button>
          <Button
            onClick={() => {
              if (ruleToDelete) {
                const confirmId = deletionImpact?.has_cascading_impact ? Number(confirmationId) : undefined;
                handleDelete(ruleToDelete.id, ruleToDelete.name, confirmId);
              }
            }}
            color="error"
            variant="contained"
            disabled={isCheckingImpact || (deletionImpact?.has_cascading_impact && !isConfirmationValid())}
          >
            {deletionImpact?.has_cascading_impact ? t('rules.dialogs.delete.deleteAll') as string : t('common.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
