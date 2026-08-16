import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Box,
  Typography,
  TextField,
  Button,
  Paper,
  List,
  ListItem,
  ListItemText,
  ListItemSecondaryAction,
  IconButton,
  Divider,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Alert,
  CircularProgress,
  FormHelperText,
  SelectChangeEvent,
  Grid,
  Autocomplete,
  Chip,
  Stack,
  Switch,
  Checkbox,
  FormControlLabel,
  Tooltip
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import AddIcon from '@mui/icons-material/Add';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import DragIndicatorIcon from '@mui/icons-material/DragIndicator';
import { DragDropContext, Droppable, Draggable, DropResult } from '@hello-pangea/dnd';
import { useTranslation } from 'react-i18next';
import { 
  getJobWorkflowFormData, 
  getJobWorkflow, 
  createJobWorkflow, 
  updateJobWorkflow 
} from '../../services/api';
import {
  PresetJobBasic,
  JobWorkflowFormData,
  CreateWorkflowRequest,
  AttackMode,
  JobWorkflowStep,
  isLoopbackEligible
} from '../../types/adminJobs';

// Helper function to get attack mode display name
const getAttackModeName = (mode?: AttackMode): string => {
  switch (mode) {
    case AttackMode.Straight: return 'Straight';
    case AttackMode.Combination: return 'Combination';
    case AttackMode.BruteForce: return 'Brute Force';
    case AttackMode.HybridWordlistMask: return 'Hybrid: Wordlist + Mask';
    case AttackMode.HybridMaskWordlist: return 'Hybrid: Mask + Wordlist';
    case AttackMode.Association: return 'Association';
    default: return 'Unknown';
  }
};

const JobWorkflowFormPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const { jobWorkflowId } = useParams<{ jobWorkflowId?: string }>();
  const navigate = useNavigate();
  const isEditing = Boolean(jobWorkflowId);

  // Form state
  const [formData, setFormData] = useState<JobWorkflowFormData>({
    name: '',
    preset_job_ids: [],
    orderedJobs: [],
    loopback_all_eligible: false,
    loopback_preset_job_ids: [],
    cloud_burst_enabled: false
  });

  // Store detailed workflow steps separately
  const [workflowSteps, setWorkflowSteps] = useState<JobWorkflowStep[]>([]);

  // Available preset jobs for selection
  const [availablePresetJobs, setAvailablePresetJobs] = useState<PresetJobBasic[]>([]);
  
  // Currently selected preset job in the dropdown
  const [selectedPresetJobId, setSelectedPresetJobId] = useState<string>('');
  
  // UI state
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // Fetch form data and workflow details if editing
  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        
        // Fetch available preset jobs
        const formDataResponse = await getJobWorkflowFormData();
        if (!formDataResponse.preset_jobs?.length) {
          setError(t('workflows.form.errors.noPresetJobsAvailable') as string);
          setLoading(false);
          return;
        }
        
        setAvailablePresetJobs(formDataResponse.preset_jobs);
        setSelectedPresetJobId(formDataResponse.preset_jobs[0]?.id || '');
        
        // If editing, fetch the workflow data
        if (isEditing && jobWorkflowId) {
          try {
            const workflow = await getJobWorkflow(jobWorkflowId);
            
            // Store the detailed workflow steps
            if (workflow.steps?.length) {
              // Sort by priority (descending) then by step order. Missing priorities
              // coalesce to 0 so the comparator stays consistent (a partially-defined
              // comparator makes Array.prototype.sort implementation-defined).
              const sortedSteps = [...workflow.steps].sort((a, b) => {
                const aPriority = a.preset_job_priority ?? 0;
                const bPriority = b.preset_job_priority ?? 0;
                if (aPriority !== bPriority) {
                  return bPriority - aPriority; // Descending priority
                }
                return a.step_order - b.step_order; // Fallback to step order
              });

              setWorkflowSteps(sortedSteps);

              // Create mapping of IDs to names for rendering. Prefer the preset records
              // from the form-data response: they always carry attack_mode, rule_ids and
              // allow_high_priority_override, whereas the JOINed step fields can be absent.
              // Fall back to the step fields for presets no longer in the available list.
              const presetsById = new Map<string, PresetJobBasic>();
              formDataResponse.preset_jobs.forEach(preset => {
                presetsById.set(preset.id, preset);
              });

              const orderedJobs: PresetJobBasic[] = sortedSteps.map(step => {
                const preset = presetsById.get(step.preset_job_id);
                return {
                  id: step.preset_job_id,
                  name: preset?.name ?? step.preset_job_name,
                  allow_high_priority_override: preset?.allow_high_priority_override,
                  attack_mode: preset?.attack_mode ?? step.preset_job_attack_mode,
                  rule_ids: preset?.rule_ids ?? step.preset_job_rule_ids
                };
              });

              setFormData({
                name: workflow.name,
                preset_job_ids: sortedSteps.map(step => step.preset_job_id),
                orderedJobs,
                loopback_all_eligible: workflow.loopback_all_eligible ?? false,
                loopback_preset_job_ids: sortedSteps
                  .filter(step => step.loopback_enabled)
                  .map(step => step.preset_job_id),
                cloud_burst_enabled: workflow.cloud_burst_enabled ?? false
              });
            } else {
              setFormData({
                name: workflow.name,
                preset_job_ids: [],
                orderedJobs: [],
                loopback_all_eligible: workflow.loopback_all_eligible ?? false,
                loopback_preset_job_ids: [],
                cloud_burst_enabled: workflow.cloud_burst_enabled ?? false
              });
            }
          } catch (err) {
            console.error('Error fetching job workflow:', err);
            setError(t('workflows.form.errors.loadFailed') as string);
          }
        }

        setLoading(false);
      } catch (err) {
        console.error('Error fetching form data:', err);
        setError(t('workflows.form.errors.loadFormDataFailed') as string);
        setLoading(false);
      }
    };

    fetchData();
  }, [isEditing, jobWorkflowId]);

  // Handle form field changes
  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFormData(prev => ({
      ...prev,
      name: e.target.value
    }));
  };

  // Handle preset job selection
  const handlePresetJobSelect = (e: SelectChangeEvent<string>) => {
    setSelectedPresetJobId(e.target.value);
  };

  // Add selected preset job to workflow
  const handleAddPresetJob = (job: PresetJobBasic | null) => {
    if (!job) return;
    
    // Skip if already in the list
    if (formData.preset_job_ids.includes(job.id)) {
      return;
    }
    
    // Add to orderedJobs and preset_job_ids
    setFormData(prev => {
      const newOrderedJobs = [...prev.orderedJobs, job];
      const newPresetJobIds = [...prev.preset_job_ids, job.id];
      
      return {
        ...prev,
        preset_job_ids: newPresetJobIds,
        orderedJobs: newOrderedJobs
      };
    });
  };

  // Remove preset job from workflow
  const handleRemovePresetJob = (jobId: string) => {
    setFormData(prev => {
      const newOrderedJobs = prev.orderedJobs.filter(job => job.id !== jobId);
      const newPresetJobIds = prev.preset_job_ids.filter(id => id !== jobId);

      return {
        ...prev,
        preset_job_ids: newPresetJobIds,
        orderedJobs: newOrderedJobs,
        loopback_preset_job_ids: prev.loopback_preset_job_ids.filter(id => id !== jobId)
      };
    });
  };

  // Toggle the workflow-level master loopback (GH #64). Mutually exclusive with the
  // per-step toggles: while this is on, per-step checkboxes are disabled.
  const handleToggleMasterLoopback = (checked: boolean) => {
    setFormData(prev => ({ ...prev, loopback_all_eligible: checked }));
  };

  // Toggle per-step loopback for a preset (only used while the master toggle is off).
  const handleToggleStepLoopback = (presetId: string, checked: boolean) => {
    setFormData(prev => ({
      ...prev,
      loopback_preset_job_ids: checked
        ? [...prev.loopback_preset_job_ids, presetId]
        : prev.loopback_preset_job_ids.filter(id => id !== presetId)
    }));
  };

  // Handle drag end for reordering workflow steps
  const handleDragEnd = (result: DropResult) => {
    if (!result.destination) return;

    const sourceIndex = result.source.index;
    const destIndex = result.destination.index;

    if (sourceIndex === destIndex) return;

    setFormData(prev => {
      const newOrderedJobs = [...prev.orderedJobs];
      const [removed] = newOrderedJobs.splice(sourceIndex, 1);
      newOrderedJobs.splice(destIndex, 0, removed);

      return {
        ...prev,
        orderedJobs: newOrderedJobs,
        preset_job_ids: newOrderedJobs.map(job => job.id)
      };
    });
  };

  // Form validation
  const validateForm = (): boolean => {
    if (!formData.name.trim()) {
      setError(t('workflows.form.errors.nameRequired') as string);
      return false;
    }

    if (formData.preset_job_ids.length === 0) {
      setError(t('workflows.form.errors.presetJobRequired') as string);
      return false;
    }

    return true;
  };

  // Handle form submission
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    if (!validateForm()) {
      return;
    }
    
    setSubmitting(true);
    setError(null);
    setSuccessMessage(null);
    
    // Create request payload
    const payload: CreateWorkflowRequest = {
      name: formData.name,
      preset_job_ids: formData.preset_job_ids,
      loopback_all_eligible: formData.loopback_all_eligible,
      // Only send per-step loopback selections when the master toggle is off (they are
      // ignored otherwise); keep only IDs still present in the workflow.
      loopback_preset_job_ids: formData.loopback_all_eligible
        ? []
        : formData.loopback_preset_job_ids.filter(id => formData.preset_job_ids.includes(id)),
      cloud_burst_enabled: formData.cloud_burst_enabled
    };
    
    try {
      if (isEditing && jobWorkflowId) {
        await updateJobWorkflow(jobWorkflowId, payload);
        setSuccessMessage(t('workflows.form.messages.updateSuccess') as string);
      } else {
        await createJobWorkflow(payload);
        setSuccessMessage(t('workflows.form.messages.createSuccess') as string);
        // Navigate back to list after a short delay
        setTimeout(() => {
          navigate('/admin/job-workflows');
        }, 1500);
      }
    } catch (err) {
      console.error('Error saving workflow:', err);
      setError(t('workflows.form.errors.saveFailed') as string);
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" height="60vh">
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box mb={3} display="flex" alignItems="center">
        <IconButton
          onClick={() => navigate('/admin/job-workflows')}
          sx={{ mr: 1 }}
          disabled={submitting}
        >
          <ArrowBackIcon />
        </IconButton>
        <Typography variant="h4">
          {isEditing ? t('workflows.form.editTitle') as string : t('workflows.form.createTitle') as string}
        </Typography>
      </Box>
      
      {error && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {error}
        </Alert>
      )}
      
      {successMessage && (
        <Alert severity="success" sx={{ mb: 3 }}>
          {successMessage}
        </Alert>
      )}
      
      <Paper sx={{ p: 3, mb: 3 }}>
        <form onSubmit={handleSubmit}>
          <TextField
            label={t('workflows.form.fields.workflowName') as string}
            value={formData.name}
            onChange={handleNameChange}
            fullWidth
            margin="normal"
            variant="outlined"
            required
            disabled={submitting}
          />

          <Box mt={3} sx={{ p: 2, borderRadius: 1, bgcolor: 'action.hover' }}>
            <FormControlLabel
              control={
                <Switch
                  checked={formData.loopback_all_eligible}
                  onChange={(e) => handleToggleMasterLoopback(e.target.checked)}
                  disabled={submitting}
                />
              }
              label={t('workflows.form.loopback.masterToggle') as string}
            />
            <FormHelperText sx={{ mt: 0 }}>
              {t('workflows.form.loopback.masterHelperText') as string}
            </FormHelperText>
          </Box>

          {/* Workflow-level only: a per-step cloud toggle would rent and tear
              down an instance between steps, paying boot and file sync each time. */}
          <Box mt={2} sx={{ p: 2, borderRadius: 1, bgcolor: 'action.hover' }}>
            <FormControlLabel
              control={
                <Switch
                  checked={formData.cloud_burst_enabled}
                  onChange={(e) => setFormData(prev => ({ ...prev, cloud_burst_enabled: e.target.checked }))}
                  disabled={submitting}
                />
              }
              label={t('workflows.form.cloudBurst.toggle') as string}
            />
            <FormHelperText sx={{ mt: 0 }}>
              {t('workflows.form.cloudBurst.helperText') as string}
            </FormHelperText>
          </Box>

          <Box mt={3}>
            <Autocomplete
              options={availablePresetJobs.filter(job => !formData.preset_job_ids.includes(job.id))}
              getOptionLabel={(option) => option.name}
              onChange={(_, value) => handleAddPresetJob(value)}
              disabled={submitting}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('workflows.form.fields.searchPresetJobs') as string}
                  variant="outlined"
                  helperText={t('workflows.form.helperText.searchPresetJobs') as string}
                  fullWidth
                />
              )}
            />
          </Box>

          <Typography variant="h6" sx={{ mt: 4, mb: 2 }}>
            {t('workflows.form.workflowSteps') as string} {formData.orderedJobs.length > 0 && `(${formData.orderedJobs.length})`}
          </Typography>

          {formData.orderedJobs.length === 0 ? (
            <Alert severity="info" sx={{ mb: 2 }}>
              {t('workflows.form.noJobsAdded') as string}
            </Alert>
          ) : (
            <Paper variant="outlined" sx={{ mb: 3 }}>
              <DragDropContext onDragEnd={handleDragEnd}>
                <Droppable droppableId="workflow-steps" isDropDisabled={submitting}>
                  {(provided) => (
                    <List ref={provided.innerRef} {...provided.droppableProps} disablePadding>
                      {formData.orderedJobs.map((job, index) => {
                        // Find the corresponding workflow step for detailed info
                        const workflowStep = workflowSteps.find(step => step.preset_job_id === job.id);

                        // Loopback eligibility + state (GH #64)
                        const eligible = isLoopbackEligible(job.attack_mode, job.rule_ids);
                        const masterOn = formData.loopback_all_eligible;
                        const loopbackChecked = masterOn ? eligible : formData.loopback_preset_job_ids.includes(job.id);
                        const loopbackDisabled = masterOn || !eligible || submitting;
                        const loopbackTooltip = !eligible
                          ? t('workflows.form.loopback.tooltipIneligible') as string
                          : masterOn
                            ? t('workflows.form.loopback.tooltipMasterOn') as string
                            : t('workflows.form.loopback.tooltipStep') as string;

                        return (
                          <Draggable key={job.id} draggableId={job.id} index={index} isDragDisabled={submitting}>
                            {(provided, snapshot) => (
                              <div ref={provided.innerRef} {...provided.draggableProps}>
                                <ListItem
                                  sx={{
                                    py: 2,
                                    bgcolor: snapshot.isDragging ? 'action.hover' : 'inherit',
                                    ...(job.allow_high_priority_override && {
                                      border: '2px solid red',
                                      borderRadius: 1,
                                      '& .MuiListItemText-root': { pl: 1 }
                                    })
                                  }}
                                >
                                  <Box
                                    {...provided.dragHandleProps}
                                    sx={{
                                      display: 'flex',
                                      alignItems: 'center',
                                      mr: 2,
                                      color: submitting ? 'text.disabled' : 'text.secondary',
                                      cursor: submitting ? 'default' : 'grab',
                                      '&:active': { cursor: submitting ? 'default' : 'grabbing' }
                                    }}
                                  >
                                    <DragIndicatorIcon />
                                  </Box>
                                  <ListItemText
                                    primary={
                                      <Box display="flex" alignItems="center" gap={1}>
                                        <Typography variant="h6" component="span">
                                          {index + 1}. {job.name}
                                        </Typography>
                                        {workflowStep?.preset_job_priority !== undefined && (
                                          <Chip
                                            label={t('workflows.form.priority', { priority: workflowStep.preset_job_priority }) as string}
                                            size="small"
                                            color="primary"
                                          />
                                        )}
                                        {job.allow_high_priority_override && (
                                          <Chip
                                            label={t('workflows.canInterrupt') as string}
                                            color="error"
                                            size="small"
                                            variant="filled"
                                          />
                                        )}
                                        <Tooltip title={loopbackTooltip}>
                                          <span>
                                            <FormControlLabel
                                              sx={{ ml: 0.5, mr: 0 }}
                                              control={
                                                <Checkbox
                                                  size="small"
                                                  checked={loopbackChecked}
                                                  disabled={loopbackDisabled}
                                                  onChange={(e) => handleToggleStepLoopback(job.id, e.target.checked)}
                                                />
                                              }
                                              label={<Typography variant="caption">{t('workflows.form.loopback.stepLabel') as string}</Typography>}
                                            />
                                          </span>
                                        </Tooltip>
                                      </Box>
                                    }
                                    secondary={
                                      <Stack spacing={1} sx={{ mt: 1 }}>
                                        <Box display="flex" flexWrap="wrap" gap={1}>
                                          {workflowStep?.preset_job_attack_mode !== undefined && (
                                            <Chip
                                              label={getAttackModeName(workflowStep.preset_job_attack_mode)}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                          {workflowStep?.preset_job_binary_name && (
                                            <Chip
                                              label={t('workflows.form.binary', { name: workflowStep.preset_job_binary_name }) as string}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                          {workflowStep?.preset_job_wordlist_ids && (
                                            <Chip
                                              label={t('workflows.form.wordlistCount', { count: workflowStep.preset_job_wordlist_ids.length }) as string}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                          {workflowStep?.preset_job_rule_ids && (
                                            <Chip
                                              label={t('workflows.form.ruleCount', { count: workflowStep.preset_job_rule_ids.length }) as string}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                        </Box>
                                      </Stack>
                                    }
                                  />
                                  <ListItemSecondaryAction>
                                    <IconButton
                                      edge="end"
                                      onClick={() => handleRemovePresetJob(job.id)}
                                      disabled={submitting}
                                    >
                                      <DeleteIcon />
                                    </IconButton>
                                  </ListItemSecondaryAction>
                                </ListItem>
                                {index < formData.orderedJobs.length - 1 && <Divider />}
                              </div>
                            )}
                          </Draggable>
                        );
                      })}
                      {provided.placeholder}
                    </List>
                  )}
                </Droppable>
              </DragDropContext>
            </Paper>
          )}
          
          <Box display="flex" justifyContent="flex-end" mt={4}>
            <Button
              variant="outlined"
              color="inherit"
              onClick={() => navigate('/admin/job-workflows')}
              sx={{ mr: 2 }}
              disabled={submitting}
            >
              {t('common.cancel') as string}
            </Button>
            <Button
              type="submit"
              variant="contained"
              color="primary"
              disabled={submitting || formData.orderedJobs.length === 0}
            >
              {submitting ? <CircularProgress size={24} /> : isEditing ? t('workflows.form.saveChanges') as string : t('workflows.form.createWorkflow') as string}
            </Button>
          </Box>
        </form>
      </Paper>
    </Box>
  );
};

export default JobWorkflowFormPage; 