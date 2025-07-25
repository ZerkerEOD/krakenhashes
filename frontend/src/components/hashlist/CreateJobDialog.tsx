import React, { useState, useEffect } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Typography,
  Box,
  Alert,
  CircularProgress,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Divider,
  Chip,
  FormHelperText,
  Tabs,
  Tab,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
  Checkbox,
  FormControlLabel,
  Stack,
  Grid,
  Autocomplete
} from '@mui/material';
import {
  Work as WorkIcon,
  AccountTree as WorkflowIcon,
  Settings as CustomIcon,
  Speed as SpeedIcon,
  Group as GroupIcon,
  Info as InfoIcon
} from '@mui/icons-material';
import { api } from '../../services/api';
import { useNavigate } from 'react-router-dom';

interface PresetJob {
  id: string;
  name: string;
  description?: string;
  attack_mode: number;
  priority: number;
  wordlist_ids?: string[];
  rule_ids?: string[];
  mask?: string;
}

interface JobWorkflow {
  id: string;
  name: string;
  description?: string;
  steps?: Array<{
    id: number;
    preset_job_id: string;
    step_order: number;
    preset_job_name?: string;
  }>;
}

interface FormData {
  wordlists: Array<{ id: number; name: string; file_size: number }>;
  rules: Array<{ id: number; name: string; rule_count: number }>;
  binary_versions: Array<{ id: number; version: string; type: string }>;
}

interface CreateJobDialogProps {
  open: boolean;
  onClose: () => void;
  hashlistId: number;
  hashlistName: string;
  hashTypeId: number;
}

export default function CreateJobDialog({
  open,
  onClose,
  hashlistId,
  hashlistName,
  hashTypeId
}: CreateJobDialogProps) {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [loadingMessage, setLoadingMessage] = useState('Creating...');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [tabValue, setTabValue] = useState(0);
  
  // Form state
  const [selectedPresetJobs, setSelectedPresetJobs] = useState<string[]>([]);
  const [selectedWorkflows, setSelectedWorkflows] = useState<string[]>([]);
  
  // Custom job state
  const [customJob, setCustomJob] = useState({
    name: '',
    attack_mode: 0,
    wordlist_ids: [] as string[],
    rule_ids: [] as string[],
    mask: '',
    priority: 5,
    max_agents: 0,
    binary_version_id: 1
  });
  
  // Available data
  const [presetJobs, setPresetJobs] = useState<PresetJob[]>([]);
  const [workflows, setWorkflows] = useState<JobWorkflow[]>([]);
  const [formData, setFormData] = useState<FormData | null>(null);
  const [loadingJobs, setLoadingJobs] = useState(true);

  // Fetch available jobs and workflows
  useEffect(() => {
    if (open && hashlistId) {
      fetchAvailableJobs();
    }
  }, [open, hashlistId]);

  const fetchAvailableJobs = async () => {
    setLoadingJobs(true);
    try {
      const response = await api.get(`/api/hashlists/${hashlistId}/available-jobs`);
      setPresetJobs(response.data.preset_jobs || []);
      setWorkflows(response.data.workflows || []);
      setFormData(response.data.form_data || null);
      
      // Set default binary version if available
      if (response.data.form_data?.binary_versions?.length > 0) {
        setCustomJob(prev => ({ 
          ...prev, 
          binary_version_id: response.data.form_data.binary_versions[0].id 
        }));
      }
    } catch (err: any) {
      console.error('Failed to fetch available jobs:', err);
      setError('Failed to load available jobs');
    } finally {
      setLoadingJobs(false);
    }
  };

  const handleSubmit = async () => {
    setLoading(true);
    setLoadingMessage('Creating job...');
    setError(null);

    try {
      let payload: any = {};

      if (tabValue === 0) {
        // Preset jobs
        if (selectedPresetJobs.length === 0) {
          setError('Please select at least one preset job');
          setLoading(false);
          return;
        }
        payload = {
          type: 'preset',
          preset_job_ids: selectedPresetJobs
        };
      } else if (tabValue === 1) {
        // Workflows
        if (selectedWorkflows.length === 0) {
          setError('Please select at least one workflow');
          setLoading(false);
          return;
        }
        payload = {
          type: 'workflow',
          workflow_ids: selectedWorkflows
        };
      } else if (tabValue === 2) {
        // Custom job
        if (!customJob.name) {
          setError('Please provide a name for the custom job');
          setLoading(false);
          return;
        }
        
        // Validate attack mode requirements
        if ([0, 1, 6, 7].includes(customJob.attack_mode) && customJob.wordlist_ids.length === 0) {
          setError('Selected attack mode requires at least one wordlist');
          setLoading(false);
          return;
        }
        
        if ([3, 6, 7].includes(customJob.attack_mode) && !customJob.mask) {
          setError('Selected attack mode requires a mask');
          setLoading(false);
          return;
        }
        
        // Custom jobs need keyspace calculation
        setLoadingMessage('Calculating keyspace...');
        
        payload = {
          type: 'custom',
          custom_job: customJob
        };
      }

      const response = await api.post(`/api/hashlists/${hashlistId}/create-job`, payload);
      
      setLoadingMessage(response.data.message || 'Job created successfully!');
      setSuccess(true);
      
      // Navigate to jobs page after a short delay
      setTimeout(() => {
        onClose();
        navigate('/jobs');
      }, 1500);
    } catch (err: any) {
      console.error('Failed to create job:', err);
      setError(err.response?.data?.error || 'Failed to create job');
    } finally {
      setLoading(false);
      setLoadingMessage('Creating...');
    }
  };

  const handleTabChange = (event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue);
    setError(null);
  };

  const getAttackModeName = (mode: number) => {
    const modes: { [key: number]: string } = {
      0: 'Dictionary',
      1: 'Combination',
      3: 'Brute-force',
      6: 'Hybrid Wordlist + Mask',
      7: 'Hybrid Mask + Wordlist',
      9: 'Association'
    };
    return modes[mode] || `Mode ${mode}`;
  };

  const handleClose = () => {
    if (!loading) {
      setError(null);
      setSuccess(false);
      setSelectedPresetJobs([]);
      setSelectedWorkflows([]);
      setCustomJob({
        name: '',
        attack_mode: 0,
        wordlist_ids: [],
        rule_ids: [],
        mask: '',
        priority: 5,
        max_agents: 0,
        binary_version_id: 1
      });
      setTabValue(0);
      onClose();
    }
  };

  const togglePresetJob = (jobId: string) => {
    setSelectedPresetJobs(prev => 
      prev.includes(jobId) 
        ? prev.filter(id => id !== jobId)
        : [...prev, jobId]
    );
  };

  const toggleWorkflow = (workflowId: string) => {
    setSelectedWorkflows(prev => 
      prev.includes(workflowId) 
        ? prev.filter(id => id !== workflowId)
        : [...prev, workflowId]
    );
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="md" fullWidth>
      <DialogTitle>
        Create Job for "{hashlistName}"
      </DialogTitle>
      
      <DialogContent>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
            {error}
          </Alert>
        )}
        
        {success && (
          <Alert severity="success" sx={{ mb: 2 }}>
            Job(s) created successfully! Redirecting to jobs page...
          </Alert>
        )}

        <Tabs value={tabValue} onChange={handleTabChange} sx={{ mb: 3 }}>
          <Tab icon={<WorkIcon />} label="Preset Jobs" />
          <Tab icon={<WorkflowIcon />} label="Workflows" />
          <Tab icon={<CustomIcon />} label="Custom Job" />
        </Tabs>

        {loadingJobs ? (
          <Box display="flex" justifyContent="center" p={3}>
            <CircularProgress />
          </Box>
        ) : (
          <>
            {/* Preset Jobs Tab */}
            {tabValue === 0 && (
              <Box>
                {presetJobs.length === 0 ? (
                  <Alert severity="info">
                    No preset jobs available. Please create preset jobs in the admin panel first.
                  </Alert>
                ) : (
                  <>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      Select one or more preset jobs to run. You can select multiple jobs - they will be created as separate job executions.
                    </Typography>
                    <List>
                      {presetJobs.map((job) => (
                        <ListItem
                          key={job.id}
                          sx={{
                            border: 1,
                            borderColor: 'divider',
                            borderRadius: 1,
                            mb: 1,
                            bgcolor: selectedPresetJobs.includes(job.id) ? 'action.selected' : 'transparent'
                          }}
                        >
                          <ListItemIcon>
                            <Checkbox
                              checked={selectedPresetJobs.includes(job.id)}
                              onChange={() => togglePresetJob(job.id)}
                            />
                          </ListItemIcon>
                          <ListItemText
                            primary={job.name}
                            secondary={
                              <Box>
                                {job.description && (
                                  <Typography variant="body2" color="text.secondary">
                                    {job.description}
                                  </Typography>
                                )}
                                <Box sx={{ mt: 1 }}>
                                  <Chip
                                    size="small"
                                    label={getAttackModeName(job.attack_mode)}
                                    sx={{ mr: 1 }}
                                  />
                                  <Chip
                                    size="small"
                                    icon={<SpeedIcon />}
                                    label={`Priority: ${job.priority}`}
                                  />
                                </Box>
                              </Box>
                            }
                          />
                        </ListItem>
                      ))}
                    </List>
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
                      {selectedPresetJobs.length} job(s) selected
                    </Typography>
                  </>
                )}
              </Box>
            )}

            {/* Workflows Tab */}
            {tabValue === 1 && (
              <Box>
                {workflows.length === 0 ? (
                  <Alert severity="info">
                    No workflows available. Please create workflows in the admin panel first.
                  </Alert>
                ) : (
                  <>
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      Select one or more workflows to run. You can select multiple workflows - each will create its own sequence of job executions.
                    </Typography>
                    <List>
                      {workflows.map((workflow) => (
                        <ListItem
                          key={workflow.id}
                          sx={{
                            border: 1,
                            borderColor: 'divider',
                            borderRadius: 1,
                            mb: 1,
                            bgcolor: selectedWorkflows.includes(workflow.id) ? 'action.selected' : 'transparent'
                          }}
                        >
                          <ListItemIcon>
                            <Checkbox
                              checked={selectedWorkflows.includes(workflow.id)}
                              onChange={() => toggleWorkflow(workflow.id)}
                            />
                          </ListItemIcon>
                          <ListItemText
                            primary={workflow.name}
                            secondary={
                              <Box>
                                {workflow.description && (
                                  <Typography variant="body2" color="text.secondary">
                                    {workflow.description}
                                  </Typography>
                                )}
                                <Box sx={{ mt: 1 }}>
                                  <Chip
                                    size="small"
                                    icon={<WorkflowIcon />}
                                    label={`${workflow.steps?.length || 0} jobs`}
                                  />
                                </Box>
                                {workflow.steps && workflow.steps.length > 0 && (
                                  <Typography variant="caption" display="block" sx={{ mt: 1 }}>
                                    Jobs: {workflow.steps.map(s => s.preset_job_name).filter(Boolean).join(', ')}
                                  </Typography>
                                )}
                              </Box>
                            }
                          />
                        </ListItem>
                      ))}
                    </List>
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
                      {selectedWorkflows.length} workflow(s) selected
                    </Typography>
                  </>
                )}
              </Box>
            )}

            {/* Custom Job Tab */}
            {tabValue === 2 && (
              <Box>
                <Grid container spacing={3}>
                  <Grid item xs={12}>
                    <TextField
                      fullWidth
                      label="Job Name"
                      value={customJob.name}
                      onChange={(e) => setCustomJob(prev => ({ ...prev, name: e.target.value }))}
                      required
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <FormControl fullWidth>
                      <InputLabel>Attack Mode</InputLabel>
                      <Select
                        value={customJob.attack_mode}
                        onChange={(e) => setCustomJob(prev => ({ ...prev, attack_mode: e.target.value as number }))}
                        label="Attack Mode"
                      >
                        <MenuItem value={0}>Dictionary Attack</MenuItem>
                        <MenuItem value={1}>Combination Attack</MenuItem>
                        <MenuItem value={3}>Brute-force Attack</MenuItem>
                        <MenuItem value={6}>Hybrid Wordlist + Mask</MenuItem>
                        <MenuItem value={7}>Hybrid Mask + Wordlist</MenuItem>
                      </Select>
                    </FormControl>
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <FormControl fullWidth>
                      <InputLabel>Binary Version</InputLabel>
                      <Select
                        value={customJob.binary_version_id}
                        onChange={(e) => setCustomJob(prev => ({ ...prev, binary_version_id: e.target.value as number }))}
                        label="Binary Version"
                      >
                        {formData?.binary_versions?.map(version => (
                          <MenuItem key={version.id} value={version.id}>
                            {version.version} ({version.type})
                          </MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                  </Grid>

                  {/* Wordlists - for attack modes 0, 1, 6, 7 */}
                  {[0, 1, 6, 7].includes(customJob.attack_mode) && (
                    <Grid item xs={12}>
                      <Autocomplete
                        multiple
                        options={formData?.wordlists || []}
                        getOptionLabel={(option) => `${option.name} (${(option.file_size / 1024 / 1024).toFixed(2)} MB)`}
                        value={formData?.wordlists?.filter(w => customJob.wordlist_ids.includes(String(w.id))) || []}
                        onChange={(e, newValue) => {
                          setCustomJob(prev => ({ 
                            ...prev, 
                            wordlist_ids: newValue.map(w => String(w.id)) 
                          }));
                        }}
                        renderInput={(params) => (
                          <TextField
                            {...params}
                            label="Wordlists"
                            placeholder="Select wordlists"
                          />
                        )}
                      />
                    </Grid>
                  )}

                  {/* Rules - for attack mode 0 */}
                  {customJob.attack_mode === 0 && (
                    <Grid item xs={12}>
                      <Autocomplete
                        multiple
                        options={formData?.rules || []}
                        getOptionLabel={(option) => `${option.name} (${option.rule_count} rules)`}
                        value={formData?.rules?.filter(r => customJob.rule_ids.includes(String(r.id))) || []}
                        onChange={(e, newValue) => {
                          setCustomJob(prev => ({ 
                            ...prev, 
                            rule_ids: newValue.map(r => String(r.id)) 
                          }));
                        }}
                        renderInput={(params) => (
                          <TextField
                            {...params}
                            label="Rules (Optional)"
                            placeholder="Select rules"
                          />
                        )}
                      />
                    </Grid>
                  )}

                  {/* Mask - for attack modes 3, 6, 7 */}
                  {[3, 6, 7].includes(customJob.attack_mode) && (
                    <Grid item xs={12}>
                      <TextField
                        fullWidth
                        label="Mask"
                        value={customJob.mask}
                        onChange={(e) => setCustomJob(prev => ({ ...prev, mask: e.target.value }))}
                        placeholder="e.g., ?u?l?l?l?l?d?d"
                        helperText="?l = lowercase, ?u = uppercase, ?d = digit, ?s = special"
                        required
                      />
                    </Grid>
                  )}

                  <Grid item xs={12} sm={6}>
                    <TextField
                      fullWidth
                      label="Priority"
                      type="number"
                      value={customJob.priority}
                      onChange={(e) => {
                        const value = parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, priority: value }));
                      }}
                      inputProps={{ min: 1, max: 1000 }}
                      helperText="Higher priority jobs are executed first (1-1000)"
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <TextField
                      fullWidth
                      label="Max Agents"
                      type="number"
                      value={customJob.max_agents}
                      onChange={(e) => {
                        const value = parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, max_agents: value }));
                      }}
                      inputProps={{ min: 0 }}
                      helperText="Maximum number of agents (0 = unlimited)"
                    />
                  </Grid>
                </Grid>
              </Box>
            )}
          </>
        )}
      </DialogContent>

      <DialogActions>
        <Button onClick={handleClose} disabled={loading}>
          Cancel
        </Button>
        <Button
          onClick={handleSubmit}
          variant="contained"
          disabled={loading || (
            tabValue === 0 && selectedPresetJobs.length === 0 ||
            tabValue === 1 && selectedWorkflows.length === 0 ||
            tabValue === 2 && !customJob.name
          )}
          startIcon={loading && <CircularProgress size={20} />}
        >
          {loading ? loadingMessage : 'Create Job(s)'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}