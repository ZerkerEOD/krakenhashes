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
import { getJobExecutionSettings } from '../../services/jobSettings';
import { useNavigate } from 'react-router-dom';
import BinaryVersionSelector from '../common/BinaryVersionSelector';

interface PresetJob {
  id: string;
  name: string;
  description?: string;
  attack_mode: number;
  priority: number;
  wordlist_ids?: string[];
  rule_ids?: string[];
  mask?: string;
  allow_high_priority_override?: boolean;
}

interface JobWorkflow {
  id: string;
  name: string;
  description?: string;
  has_high_priority_override?: boolean;
  steps?: Array<{
    id: number;
    preset_job_id: string;
    step_order: number;
    preset_job_name?: string;
    allow_high_priority_override?: boolean;
  }>;
}

interface FormData {
  wordlists: Array<{ id: number; name: string; file_size: number }>;
  rules: Array<{ id: number; name: string; rule_count: number }>;
  binary_versions: Array<{ id: number; version: string; type: string }>;
}

interface AssociationWordlist {
  id: string;
  file_name: string;
  file_size: number;
  line_count: number;
}

interface CreateJobDialogProps {
  open: boolean;
  onClose: () => void;
  hashlistId: number;
  hashlistName: string;
  hashTypeId: number;
  hasMixedWorkFactors?: boolean;
  totalHashes?: number;
}

export default function CreateJobDialog({
  open,
  onClose,
  hashlistId,
  hashlistName,
  hashTypeId,
  hasMixedWorkFactors = false,
  totalHashes = 0
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
  const [customJobName, setCustomJobName] = useState<string>('');

  // Association attack state
  const [associationWordlists, setAssociationWordlists] = useState<AssociationWordlist[]>([]);
  const [selectedAssociationWordlist, setSelectedAssociationWordlist] = useState<string>('');
  
  // Custom job state
  const [combWordlist1, setCombWordlist1] = useState<string>('');
  const [combWordlist2, setCombWordlist2] = useState<string>('');
  const [customJob, setCustomJob] = useState({
    name: '',
    attack_mode: 0,
    wordlist_ids: [] as string[],
    rule_ids: [] as string[],
    mask: '',
    priority: 5,
    max_agents: 0,
    binary_version: 'default',
    allow_high_priority_override: false,
    chunk_duration: 1200, // Default to 20 minutes (will be updated from system settings)
    increment_mode: 'off' as string,
    increment_min: undefined as number | undefined,
    increment_max: undefined as number | undefined,
    association_wordlist_id: undefined as string | undefined
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
      // Fetch available jobs, job execution settings, and association wordlists in parallel
      const [response, jobExecutionSettings, assocWordlistsResponse] = await Promise.all([
        api.get(`/api/hashlists/${hashlistId}/available-jobs`),
        getJobExecutionSettings().catch(() => null), // Gracefully handle if settings fetch fails
        api.get(`/api/hashlists/${hashlistId}/association-wordlists`).catch(() => ({ data: [] }))
      ]);

      setPresetJobs(response.data.preset_jobs || []);
      setWorkflows(response.data.workflows || []);
      setFormData(response.data.form_data || null);
      setAssociationWordlists(assocWordlistsResponse.data || []);
      
      // Set default chunk duration from system settings
      let systemDefaultChunkDuration = 1200; // fallback to 20 minutes
      if (jobExecutionSettings?.default_chunk_duration) {
        systemDefaultChunkDuration = jobExecutionSettings.default_chunk_duration;
      }

      // Update chunk duration (binary_version defaults to 'default')
      setCustomJob(prev => ({
        ...prev,
        chunk_duration: systemDefaultChunkDuration
      }));
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
        // Workflows
        if (selectedWorkflows.length === 0) {
          setError('Please select at least one workflow');
          setLoading(false);
          return;
        }
        payload = {
          type: 'workflow',
          workflow_ids: selectedWorkflows,
          custom_job_name: customJobName
        };
      } else if (tabValue === 1) {
        // Preset jobs
        if (selectedPresetJobs.length === 0) {
          setError('Please select at least one preset job');
          setLoading(false);
          return;
        }
        payload = {
          type: 'preset',
          preset_job_ids: selectedPresetJobs,
          custom_job_name: customJobName
        };
      } else if (tabValue === 2) {
        // Custom job
        // Name is now optional - will use default format if not provided
        
        // Validate attack mode requirements
        if ([0, 6, 7].includes(customJob.attack_mode) && customJob.wordlist_ids.length === 0) {
          setError('Selected attack mode requires at least one wordlist');
          setLoading(false);
          return;
        }

        // Combination attack requires exactly 2 wordlists
        if (customJob.attack_mode === 1 && customJob.wordlist_ids.length !== 2) {
          setError('Combination attack requires both wordlists to be selected');
          setLoading(false);
          return;
        }

        if ([3, 6, 7].includes(customJob.attack_mode) && !customJob.mask) {
          setError('Selected attack mode requires a mask');
          setLoading(false);
          return;
        }

        // Association attack validation
        if (customJob.attack_mode === 9) {
          if (hasMixedWorkFactors) {
            setError('Association attacks cannot be run on hashlists with mixed work factors');
            setLoading(false);
            return;
          }
          if (!customJob.association_wordlist_id) {
            setError('Association attack requires an association wordlist');
            setLoading(false);
            return;
          }
        }

        // Validate chunk duration
        if (customJob.chunk_duration < 5) {
          setError('Chunk duration must be at least 5 seconds');
          setLoading(false);
          return;
        }
        if (customJob.chunk_duration > 86400) {
          setError('Chunk duration cannot exceed 24 hours (86400 seconds)');
          setLoading(false);
          return;
        }

        // Custom jobs need keyspace calculation
        setLoadingMessage('Calculating keyspace...');

        // Map chunk_duration to chunk_size_seconds for API
        const customJobPayload = {
          ...customJob,
          chunk_size_seconds: customJob.chunk_duration
        };
        delete (customJobPayload as any).chunk_duration;

        payload = {
          type: 'custom',
          custom_job: customJobPayload,
          custom_job_name: customJobName || customJob.name
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
        binary_version: 'default',
        allow_high_priority_override: false,
        chunk_duration: 1200, // Default to 20 minutes
        increment_mode: 'off',
        increment_min: undefined,
        increment_max: undefined,
        association_wordlist_id: undefined
      });
      setTabValue(0);
      setCustomJobName('');
      // Reset combination wordlist state
      setCombWordlist1('');
      setCombWordlist2('');
      // Reset association wordlist state
      setSelectedAssociationWordlist('');
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
          <Tab icon={<WorkflowIcon />} label="Workflows" />
          <Tab icon={<WorkIcon />} label="Preset Jobs" />
          <Tab icon={<CustomIcon />} label="Custom Job" />
        </Tabs>

        {loadingJobs ? (
          <Box display="flex" justifyContent="center" p={3}>
            <CircularProgress />
          </Box>
        ) : (
          <>
            {/* Workflows Tab */}
            {tabValue === 0 && (
              <Box>
                {workflows.length === 0 ? (
                  <Alert severity="info">
                    No workflows available. Please create workflows in the admin panel first.
                  </Alert>
                ) : (
                  <>
                    <TextField
                      fullWidth
                      label="Job Name (Optional)"
                      placeholder="Leave empty for auto-generated name"
                      value={customJobName}
                      onChange={(e) => setCustomJobName(e.target.value)}
                      helperText="Your name will be appended with each workflow name"
                      sx={{ mb: 3 }}
                    />
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      Select one or more workflows to run. You can select multiple workflows - each will create its own sequence of job executions.
                    </Typography>
                    <List>
                      {workflows.map((workflow) => (
                        <ListItem
                          key={workflow.id}
                          sx={{
                            border: workflow.has_high_priority_override ? 2 : 1,
                            borderColor: workflow.has_high_priority_override ? 'error.main' : 'divider',
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
                                    sx={{ mr: 1 }}
                                  />
                                  {workflow.has_high_priority_override && (
                                    <Chip
                                      size="small"
                                      label="Can Interrupt"
                                      color="error"
                                      variant="filled"
                                    />
                                  )}
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

            {/* Preset Jobs Tab */}
            {tabValue === 1 && (
              <Box>
                {presetJobs.length === 0 ? (
                  <Alert severity="info">
                    No preset jobs available. Please create preset jobs in the admin panel first.
                  </Alert>
                ) : (
                  <>
                    <TextField
                      fullWidth
                      label="Job Name (Optional)"
                      placeholder="Leave empty for auto-generated name"
                      value={customJobName}
                      onChange={(e) => setCustomJobName(e.target.value)}
                      helperText="Your name will be appended with each job type (e.g., 'My Name - Potfile Run')"
                      sx={{ mb: 3 }}
                    />
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      Select one or more preset jobs to run. You can select multiple jobs - they will be created as separate job executions.
                    </Typography>
                    <List>
                      {presetJobs.map((job) => (
                        <ListItem
                          key={job.id}
                          sx={{
                            border: job.allow_high_priority_override ? 2 : 1,
                            borderColor: job.allow_high_priority_override ? 'error.main' : 'divider',
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
                                    sx={{ mr: 1 }}
                                  />
                                  {job.allow_high_priority_override && (
                                    <Chip
                                      size="small"
                                      label="Can Interrupt"
                                      color="error"
                                      variant="filled"
                                    />
                                  )}
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

            {/* Custom Job Tab */}
            {tabValue === 2 && (
              <Box>
                <Grid container spacing={3}>
                  <Grid item xs={12}>
                    <TextField
                      fullWidth
                      label="Job Name (Optional)"
                      placeholder="Leave empty for auto-generated name"
                      value={customJob.name}
                      onChange={(e) => setCustomJob(prev => ({ ...prev, name: e.target.value }))}
                      helperText="Leave empty for auto-generated name based on attack configuration"
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <FormControl fullWidth>
                      <InputLabel>Attack Mode</InputLabel>
                      <Select
                        value={customJob.attack_mode}
                        onChange={(e) => {
                          const newMode = e.target.value as number;
                          setCustomJob(prev => ({
                            ...prev,
                            attack_mode: newMode,
                            wordlist_ids: [],
                            rule_ids: [],
                            mask: '',
                            association_wordlist_id: undefined
                          }));
                          // Reset combination wordlist state
                          setCombWordlist1('');
                          setCombWordlist2('');
                          // Reset association wordlist state
                          setSelectedAssociationWordlist('');
                        }}
                        label="Attack Mode"
                      >
                        <MenuItem value={0}>Dictionary Attack</MenuItem>
                        <MenuItem value={1}>Combination Attack</MenuItem>
                        <MenuItem value={3}>Brute-force Attack</MenuItem>
                        <MenuItem value={6}>Hybrid Wordlist + Mask</MenuItem>
                        <MenuItem value={7}>Hybrid Mask + Wordlist</MenuItem>
                        <MenuItem
                          value={9}
                          disabled={hasMixedWorkFactors || associationWordlists.length === 0}
                        >
                          Association Attack
                          {hasMixedWorkFactors && ' (Blocked: Mixed work factors)'}
                          {!hasMixedWorkFactors && associationWordlists.length === 0 && ' (No wordlists uploaded)'}
                        </MenuItem>
                      </Select>
                    </FormControl>
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <BinaryVersionSelector
                      value={customJob.binary_version}
                      onChange={(value) => setCustomJob(prev => ({ ...prev, binary_version: value }))}
                      margin="none"
                      helperText="Select binary version pattern"
                    />
                  </Grid>

                  {/* Attack mode 0 (Dictionary): Wordlists → Rules */}
                  {customJob.attack_mode === 0 && (
                    <>
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
                      <Grid item xs={12}>
                        <Autocomplete
                          options={formData?.rules || []}
                          getOptionLabel={(option) => `${option.name} (${option.rule_count} rules)`}
                          value={formData?.rules?.find(r => customJob.rule_ids.includes(String(r.id))) || null}
                          onChange={(e, newValue) => {
                            setCustomJob(prev => ({
                              ...prev,
                              rule_ids: newValue ? [String(newValue.id)] : []
                            }));
                          }}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label="Rule (Optional)"
                              placeholder="Select a rule"
                            />
                          )}
                        />
                      </Grid>
                    </>
                  )}

                  {/* Attack mode 1 (Combination): Two separate wordlist selectors */}
                  {customJob.attack_mode === 1 && (
                    <>
                      <Grid item xs={12} sm={6}>
                        <FormControl fullWidth required>
                          <InputLabel shrink>First Wordlist</InputLabel>
                          <Select
                            value={combWordlist1}
                            onChange={(e) => {
                              const value = e.target.value as string;
                              setCombWordlist1(value);
                              setCustomJob(prev => ({
                                ...prev,
                                wordlist_ids: [value, combWordlist2].filter(Boolean)
                              }));
                            }}
                            label="First Wordlist"
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>Select first wordlist</em></MenuItem>
                            {formData?.wordlists?.map((w) => (
                              <MenuItem key={`first-${w.id}`} value={String(w.id)}>
                                {w.name} ({(w.file_size / 1024 / 1024).toFixed(2)} MB)
                              </MenuItem>
                            ))}
                          </Select>
                        </FormControl>
                      </Grid>
                      <Grid item xs={12} sm={6}>
                        <FormControl fullWidth required>
                          <InputLabel shrink>Second Wordlist</InputLabel>
                          <Select
                            value={combWordlist2}
                            onChange={(e) => {
                              const value = e.target.value as string;
                              setCombWordlist2(value);
                              setCustomJob(prev => ({
                                ...prev,
                                wordlist_ids: [combWordlist1, value].filter(Boolean)
                              }));
                            }}
                            label="Second Wordlist"
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>Select second wordlist</em></MenuItem>
                            {formData?.wordlists?.map((w) => (
                              <MenuItem key={`second-${w.id}`} value={String(w.id)}>
                                {w.name} ({(w.file_size / 1024 / 1024).toFixed(2)} MB)
                              </MenuItem>
                            ))}
                          </Select>
                        </FormControl>
                      </Grid>
                    </>
                  )}

                  {/* Attack mode 3 (Brute Force): Mask only */}
                  {customJob.attack_mode === 3 && (
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

                  {/* Attack mode 6 (Hybrid Wordlist + Mask): Wordlists → Mask */}
                  {customJob.attack_mode === 6 && (
                    <>
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
                    </>
                  )}

                  {/* Attack mode 7 (Hybrid Mask + Wordlist): Mask → Wordlists */}
                  {customJob.attack_mode === 7 && (
                    <>
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
                    </>
                  )}

                  {/* Increment Mode - only for mask-based attacks */}
                  {(customJob.attack_mode === 3 || customJob.attack_mode === 6 || customJob.attack_mode === 7) && (
                    <>
                      <Grid item xs={12}>
                        <FormControl fullWidth>
                          <InputLabel>Increment Mode</InputLabel>
                          <Select
                            value={customJob.increment_mode}
                            onChange={(e) => setCustomJob(prev => ({ ...prev, increment_mode: e.target.value }))}
                            label="Increment Mode"
                          >
                            <MenuItem value="off">Off</MenuItem>
                            <MenuItem value="increment">Increment (L→R)</MenuItem>
                            <MenuItem value="increment_inverse">Increment Inverse (R→L)</MenuItem>
                          </Select>
                          <FormHelperText>
                            Increment tries shorter masks first, growing progressively
                          </FormHelperText>
                        </FormControl>
                      </Grid>

                      {customJob.increment_mode !== 'off' && (
                        <Grid item xs={12}>
                          <Grid container spacing={2}>
                            <Grid item xs={6}>
                              <TextField
                                fullWidth
                                label="Min Length"
                                type="number"
                                value={customJob.increment_min || ''}
                                onChange={(e) => setCustomJob(prev => ({
                                  ...prev,
                                  increment_min: e.target.value ? parseInt(e.target.value) : undefined
                                }))}
                                inputProps={{ min: 1 }}
                              />
                            </Grid>
                            <Grid item xs={6}>
                              <TextField
                                fullWidth
                                label="Max Length"
                                type="number"
                                value={customJob.increment_max || ''}
                                onChange={(e) => setCustomJob(prev => ({
                                  ...prev,
                                  increment_max: e.target.value ? parseInt(e.target.value) : undefined
                                }))}
                                inputProps={{ min: 1 }}
                              />
                            </Grid>
                          </Grid>
                        </Grid>
                      )}
                    </>
                  )}

                  {/* Attack mode 9 (Association): Association wordlist + optional rules */}
                  {customJob.attack_mode === 9 && (
                    <>
                      <Grid item xs={12}>
                        <Alert severity="info" sx={{ mb: 2 }}>
                          <Typography variant="body2">
                            Association attack maps each hash to a corresponding wordlist line 1:1.
                            The wordlist line count must match the total hash count ({totalHashes.toLocaleString()}).
                          </Typography>
                        </Alert>
                        <Alert severity="warning" sx={{ mb: 2 }}>
                          <Typography variant="body2">
                            <strong>Note:</strong> Association attacks can produce false positives.
                            It is recommended to verify results by downloading cracked hashes,
                            deleting the hashlist, re-uploading, and confirming with a dictionary attack.
                          </Typography>
                        </Alert>
                      </Grid>
                      <Grid item xs={12}>
                        <FormControl fullWidth required>
                          <InputLabel shrink>Association Wordlist</InputLabel>
                          <Select
                            value={selectedAssociationWordlist}
                            onChange={(e) => {
                              const value = e.target.value as string;
                              setSelectedAssociationWordlist(value);
                              setCustomJob(prev => ({
                                ...prev,
                                association_wordlist_id: value || undefined
                              }));
                            }}
                            label="Association Wordlist"
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>Select association wordlist</em></MenuItem>
                            {associationWordlists.map((w) => (
                              <MenuItem key={w.id} value={w.id}>
                                {w.file_name} ({w.line_count.toLocaleString()} lines, {(w.file_size / 1024).toFixed(1)} KB)
                              </MenuItem>
                            ))}
                          </Select>
                          <FormHelperText>
                            Upload association wordlists via the Hashlist's Association Wordlists section
                          </FormHelperText>
                        </FormControl>
                      </Grid>
                      <Grid item xs={12}>
                        <Autocomplete
                          options={formData?.rules || []}
                          getOptionLabel={(option) => `${option.name} (${option.rule_count} rules)`}
                          value={formData?.rules?.find(r => customJob.rule_ids.includes(String(r.id))) || null}
                          onChange={(e, newValue) => {
                            setCustomJob(prev => ({
                              ...prev,
                              rule_ids: newValue ? [String(newValue.id)] : []
                            }));
                          }}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label="Rule (Optional)"
                              placeholder="Select a rule"
                            />
                          )}
                        />
                      </Grid>
                    </>
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
                      label="Chunk Duration (seconds)"
                      type="number"
                      value={customJob.chunk_duration}
                      onChange={(e) => {
                        const value = e.target.value === '' ? 0 : parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, chunk_duration: value }));
                      }}
                      helperText="Time in seconds for each chunk (5-86400 seconds)"
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

                  <Grid item xs={12} sm={6}>
                    <FormControlLabel
                      control={
                        <Checkbox
                          checked={customJob.allow_high_priority_override}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, allow_high_priority_override: e.target.checked }))}
                        />
                      }
                      label="Allow High Priority Override"
                      sx={{ mt: 1 }}
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
            tabValue === 0 && selectedWorkflows.length === 0 ||
            tabValue === 1 && selectedPresetJobs.length === 0
          )}
          startIcon={loading && <CircularProgress size={20} />}
        >
          {loading ? loadingMessage : 'Create Job(s)'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}