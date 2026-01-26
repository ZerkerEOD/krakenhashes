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
  Stack
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import AddIcon from '@mui/icons-material/Add';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import DragIndicatorIcon from '@mui/icons-material/DragIndicator';
import { DragDropContext, Droppable, Draggable, DropResult } from '@hello-pangea/dnd';
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
  JobWorkflowStep
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
  const { jobWorkflowId } = useParams<{ jobWorkflowId?: string }>();
  const navigate = useNavigate();
  const isEditing = Boolean(jobWorkflowId);

  // Form state
  const [formData, setFormData] = useState<JobWorkflowFormData>({
    name: '',
    preset_job_ids: [],
    orderedJobs: []
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
          setError('No preset jobs available. Please add preset jobs before creating workflows.');
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
              // Sort by priority (descending) then by step order
              const sortedSteps = [...workflow.steps].sort((a, b) => {
                if (a.preset_job_priority !== undefined && b.preset_job_priority !== undefined) {
                  if (a.preset_job_priority !== b.preset_job_priority) {
                    return b.preset_job_priority - a.preset_job_priority; // Descending priority
                  }
                }
                return a.step_order - b.step_order; // Fallback to step order
              });
              
              setWorkflowSteps(sortedSteps);
              
              // Create mapping of IDs to names for rendering
              const orderedJobs: PresetJobBasic[] = sortedSteps.map(step => ({
                id: step.preset_job_id,
                name: step.preset_job_name
              }));
              
              setFormData({
                name: workflow.name,
                preset_job_ids: sortedSteps.map(step => step.preset_job_id),
                orderedJobs
              });
            } else {
              setFormData({
                name: workflow.name,
                preset_job_ids: [],
                orderedJobs: []
              });
            }
          } catch (err) {
            console.error('Error fetching job workflow:', err);
            setError('Failed to load workflow. Please try again.');
          }
        }
        
        setLoading(false);
      } catch (err) {
        console.error('Error fetching form data:', err);
        setError('Failed to load form data. Please try again.');
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
        orderedJobs: newOrderedJobs
      };
    });
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
      setError('Workflow name is required');
      return false;
    }
    
    if (formData.preset_job_ids.length === 0) {
      setError('At least one preset job is required');
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
    
    // Create request payload (only need name and preset_job_ids)
    const payload: CreateWorkflowRequest = {
      name: formData.name,
      preset_job_ids: formData.preset_job_ids
    };
    
    try {
      if (isEditing && jobWorkflowId) {
        await updateJobWorkflow(jobWorkflowId, payload);
        setSuccessMessage('Workflow updated successfully');
      } else {
        await createJobWorkflow(payload);
        setSuccessMessage('Workflow created successfully');
        // Navigate back to list after a short delay
        setTimeout(() => {
          navigate('/admin/job-workflows');
        }, 1500);
      }
    } catch (err) {
      console.error('Error saving workflow:', err);
      setError('Failed to save workflow. Please try again.');
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
          {isEditing ? 'Edit Job Workflow' : 'Create New Job Workflow'}
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
            label="Workflow Name"
            value={formData.name}
            onChange={handleNameChange}
            fullWidth
            margin="normal"
            variant="outlined"
            required
            disabled={submitting}
          />
          
          <Box mt={3}>
            <Autocomplete
              options={availablePresetJobs.filter(job => !formData.preset_job_ids.includes(job.id))}
              getOptionLabel={(option) => option.name}
              onChange={(_, value) => handleAddPresetJob(value)}
              disabled={submitting}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Search and add preset jobs"
                  variant="outlined"
                  helperText="Search and select a preset job to add to the workflow"
                  fullWidth
                />
              )}
            />
          </Box>
          
          <Typography variant="h6" sx={{ mt: 4, mb: 2 }}>
            Workflow Steps {formData.orderedJobs.length > 0 && `(${formData.orderedJobs.length})`}
          </Typography>
          
          {formData.orderedJobs.length === 0 ? (
            <Alert severity="info" sx={{ mb: 2 }}>
              No preset jobs added to this workflow yet. Add jobs from the dropdown above.
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
                                            label={`Priority: ${workflowStep.preset_job_priority}`}
                                            size="small"
                                            color="primary"
                                          />
                                        )}
                                        {job.allow_high_priority_override && (
                                          <Chip
                                            label="Can Interrupt"
                                            color="error"
                                            size="small"
                                            variant="filled"
                                          />
                                        )}
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
                                              label={`Binary: ${workflowStep.preset_job_binary_name}`}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                          {workflowStep?.preset_job_wordlist_ids && (
                                            <Chip
                                              label={`${workflowStep.preset_job_wordlist_ids.length} Wordlist${workflowStep.preset_job_wordlist_ids.length !== 1 ? 's' : ''}`}
                                              size="small"
                                              variant="outlined"
                                            />
                                          )}
                                          {workflowStep?.preset_job_rule_ids && (
                                            <Chip
                                              label={`${workflowStep.preset_job_rule_ids.length} Rule${workflowStep.preset_job_rule_ids.length !== 1 ? 's' : ''}`}
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
              Cancel
            </Button>
            <Button
              type="submit"
              variant="contained"
              color="primary"
              disabled={submitting || formData.orderedJobs.length === 0}
            >
              {submitting ? <CircularProgress size={24} /> : isEditing ? 'Save Changes' : 'Create Workflow'}
            </Button>
          </Box>
        </form>
      </Paper>
    </Box>
  );
};

export default JobWorkflowFormPage; 