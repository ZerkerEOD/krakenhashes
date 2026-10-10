import React, { useState, useEffect, useMemo } from 'react';
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
  Autocomplete,
  ListSubheader
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
import { getJobDefaultsForUsers } from '../../services/jobSettings';
import { useNavigate } from 'react-router-dom';
import { useTranslation, Trans } from 'react-i18next';
import BinaryVersionSelector from '../common/BinaryVersionSelector';
import CharsetInputs from '../common/CharsetInputs';
import { CustomCharset } from '../../types/customCharsets';
import { listAccessibleCharsets } from '../../services/customCharsetService';
import FilterCriteriaForm, { isFilterEmpty } from '../wordlists/FilterCriteriaForm';
import { WordlistFilter } from '../../types/wordlists';

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
  loopback_all_eligible?: boolean; // GH #64
  loopback_step_count?: number; // Steps that actually loop back, computed server-side (GH #78)
  steps?: Array<{
    id: number;
    preset_job_id: string;
    step_order: number;
    preset_job_name?: string;
    allow_high_priority_override?: boolean;
    loopback_enabled?: boolean; // GH #64
    loopback_effective?: boolean; // Server-computed eligibility - do not recompute here (GH #78)
  }>;
}

// A loopback re-runs an attack's mutation against only the newly-cracked plaintexts.
// Eligible: straight (0) WITH rules, or a hybrid (6/7) with a mask. Everything else
// (wordlist-only, brute-force, combinator, association) is not re-run (GH #64).
const isLoopbackEligibleMode = (attackMode: number, ruleIds?: string[]): boolean => {
  if (attackMode === 0) return !!ruleIds && ruleIds.length > 0;
  return attackMode === 6 || attackMode === 7;
};

interface ClientWordlist {
  id: string;
  client_id: string;
  file_name: string;
  file_size: number;
  line_count: number;
}

interface ClientPotfile {
  id: number;
  client_id: string;
  file_size: number;
  line_count: number;
}

interface FormData {
  wordlists: Array<{ id: number; name: string; file_size: number }>;
  rules: Array<{ id: number; name: string; rule_count: number }>;
  binary_versions: Array<{ id: number; version: string; type: string }>;
  client_wordlists?: ClientWordlist[];
  client_potfile?: ClientPotfile | null;
}

// Combined wordlist option type for categorized display
interface WordlistOption {
  id: string;
  name: string;
  file_size: number;
  category: 'Client Specific' | 'Global';
  line_count?: number;
  isPotfile?: boolean;
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
  const { t } = useTranslation('hashlists');
  const [loading, setLoading] = useState(false);
  const [loadingMessage, setLoadingMessage] = useState(t('createJob.loadingDefault') as string);
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
    association_wordlist_id: undefined as string | undefined,
    custom_charsets: null as Record<string, string> | null,
    custom_charset_file_ids: null as Record<string, string> | null,
    hex_charset: false,
    additional_args: '',
    // Cloud burst is opt-in per job (and separately capped from max_agents,
    // which governs only the shared on-prem pool). Nothing is rented without it.
    cloud_burst_enabled: false,
    cloud_allow_community_hosts: false,
    cloud_max_instances: undefined as number | undefined
  });

  // Ephemeral wordlist filtering (GH #40) — applies to wordlist-based attacks only.
  const [filterEnabled, setFilterEnabled] = useState(false);
  const [customFilter, setCustomFilter] = useState<WordlistFilter>({});

  // Loopback (GH #64) — per-run toggle for preset and custom jobs.
  const [presetLoopback, setPresetLoopback] = useState(false);
  const [customLoopback, setCustomLoopback] = useState(false);

  // Saved charsets for picker
  const [savedCharsets, setSavedCharsets] = useState<CustomCharset[]>([]);

  // Available data
  const [presetJobs, setPresetJobs] = useState<PresetJob[]>([]);
  const [workflows, setWorkflows] = useState<JobWorkflow[]>([]);
  const [formData, setFormData] = useState<FormData | null>(null);
  const [loadingJobs, setLoadingJobs] = useState(true);

  // Compute combined wordlist options with categories (Client Specific above Global)
  const combinedWordlistOptions = useMemo((): WordlistOption[] => {
    const options: WordlistOption[] = [];

    // Add client potfile first if available (it's the most valuable client-specific resource)
    if (formData?.client_potfile) {
      options.push({
        id: `potfile:${formData.client_potfile.id}`,
        name: t('createJob.form.clientPotfileName', { count: formData.client_potfile.line_count }) as string,
        file_size: formData.client_potfile.file_size,
        category: 'Client Specific',
        line_count: formData.client_potfile.line_count,
        isPotfile: true
      });
    }

    // Add client wordlists
    if (formData?.client_wordlists) {
      formData.client_wordlists.forEach(wl => {
        options.push({
          id: `client:${wl.id}`,
          name: wl.file_name,
          file_size: wl.file_size,
          category: 'Client Specific',
          line_count: wl.line_count
        });
      });
    }

    // Add global wordlists
    if (formData?.wordlists) {
      formData.wordlists.forEach(wl => {
        options.push({
          id: String(wl.id),
          name: wl.name,
          file_size: wl.file_size,
          category: 'Global'
        });
      });
    }

    return options;
  }, [formData, t]);

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
      const [response, jobDefaults, assocWordlistsResponse, accessibleCharsets] = await Promise.all([
        api.get(`/api/hashlists/${hashlistId}/available-jobs`),
        getJobDefaultsForUsers().catch(() => null), // Gracefully handle if settings fetch fails
        api.get(`/api/hashlists/${hashlistId}/association-wordlists`).catch(() => ({ data: [] })),
        listAccessibleCharsets().catch(() => [])
      ]);

      setSavedCharsets(accessibleCharsets);

      setPresetJobs(response.data.preset_jobs || []);
      setWorkflows(response.data.workflows || []);
      setFormData(response.data.form_data || null);
      setAssociationWordlists(assocWordlistsResponse.data || []);
      
      // Set default chunk duration from system settings
      let systemDefaultChunkDuration = 1200; // fallback to 20 minutes
      if (jobDefaults?.default_chunk_duration) {
        systemDefaultChunkDuration = jobDefaults.default_chunk_duration;
      }

      // Update chunk duration (binary_version defaults to 'default')
      setCustomJob(prev => ({
        ...prev,
        chunk_duration: systemDefaultChunkDuration
      }));
    } catch (err: any) {
      console.error('Failed to fetch available jobs:', err);
      setError(t('createJob.errors.loadFailed') as string);
    } finally {
      setLoadingJobs(false);
    }
  };

  const handleSubmit = async () => {
    setLoading(true);
    setLoadingMessage(t('createJob.loadingCreating') as string);
    setError(null);

    try {
      let payload: any = {};

      if (tabValue === 0) {
        // Workflows
        if (selectedWorkflows.length === 0) {
          setError(t('createJob.errors.selectWorkflow') as string);
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
          setError(t('createJob.errors.selectPresetJob') as string);
          setLoading(false);
          return;
        }
        payload = {
          type: 'preset',
          preset_job_ids: selectedPresetJobs,
          custom_job_name: customJobName,
          loopback: presetLoopback
        };
      } else if (tabValue === 2) {
        // Custom job
        // Name is now optional - will use default format if not provided

        // Validate attack mode requirements
        if ([0, 6, 7].includes(customJob.attack_mode) && customJob.wordlist_ids.length === 0) {
          setError(t('createJob.errors.wordlistRequired') as string);
          setLoading(false);
          return;
        }

        // Combination attack requires exactly 2 wordlists
        if (customJob.attack_mode === 1 && customJob.wordlist_ids.length !== 2) {
          setError(t('createJob.errors.combinationRequiresTwo') as string);
          setLoading(false);
          return;
        }

        if ([3, 6, 7].includes(customJob.attack_mode) && !customJob.mask) {
          setError(t('createJob.errors.maskRequired') as string);
          setLoading(false);
          return;
        }

        // Association attack validation
        if (customJob.attack_mode === 9) {
          if (hasMixedWorkFactors) {
            setError(t('createJob.errors.associationMixedWorkFactors') as string);
            setLoading(false);
            return;
          }
          if (!customJob.association_wordlist_id) {
            setError(t('createJob.errors.associationWordlistRequired') as string);
            setLoading(false);
            return;
          }
        }

        // Validate chunk duration
        if (customJob.chunk_duration < 5) {
          setError(t('createJob.errors.chunkDurationMin') as string);
          setLoading(false);
          return;
        }
        if (customJob.chunk_duration > 86400) {
          setError(t('createJob.errors.chunkDurationMax') as string);
          setLoading(false);
          return;
        }

        // Custom jobs need keyspace calculation
        setLoadingMessage(t('createJob.loadingKeyspace') as string);

        // Map chunk_duration to chunk_size_seconds for API
        const customJobPayload: any = {
          ...customJob,
          chunk_size_seconds: customJob.chunk_duration,
          additional_args: customJob.additional_args || null
        };
        delete (customJobPayload as any).chunk_duration;

        // Attach an ephemeral wordlist filter for wordlist-based attacks (GH #40).
        if (filterEnabled && [0, 1, 6, 7].includes(customJob.attack_mode) && !isFilterEmpty(customFilter)) {
          customJobPayload.filter = customFilter;
        }

        // Loopback (GH #64): re-run the mutation against newly-cracked plaintexts until
        // dry. Not combined with the ephemeral filter (the filter path is a no-op for
        // loopback), and only meaningful for mutatable attacks.
        if (customLoopback && !filterEnabled && isLoopbackEligibleMode(customJob.attack_mode, customJob.rule_ids)) {
          customJobPayload.loopback = true;
        }

        payload = {
          type: 'custom',
          custom_job: customJobPayload,
          custom_job_name: customJobName || customJob.name
        };
      }

      const response = await api.post(`/api/hashlists/${hashlistId}/create-job`, payload);

      // Partial success: the backend created some jobs and could not create
      // others (a workflow step whose preset was deleted, a keyspace failure on
      // one preset). It still returns 201, so without this the dropped items
      // would vanish silently — the user would see "success" and a short jobs
      // list. Surface them and do not auto-navigate, so the message is read.
      const failures = response.data.failures as
        | Array<{ preset_job_id?: string; name?: string; error: string }>
        | undefined;
      if (failures && failures.length > 0) {
        const itemFallback = t('createJob.errors.unnamedItem') as string;
        setError(
          t('createJob.errors.partialFailure', {
            message: response.data.message,
            details: failures.map((f) => `${f.name || f.preset_job_id || itemFallback} — ${f.error}`).join('; ')
          }) as string
        );
        return;
      }

      setLoadingMessage(response.data.message || (t('createJob.successDefault') as string));
      setSuccess(true);

      // Navigate to jobs page after a short delay
      setTimeout(() => {
        onClose();
        navigate('/jobs');
      }, 1500);
    } catch (err: any) {
      console.error('Failed to create job:', err);
      setError(
        err.response?.data?.error ||
        (typeof err.response?.data === 'string' ? err.response.data : null) ||
        (t('createJob.errors.createFailed') as string)
      );
    } finally {
      setLoading(false);
      setLoadingMessage(t('createJob.loadingDefault') as string);
    }
  };

  const handleTabChange = (event: React.SyntheticEvent, newValue: number) => {
    setTabValue(newValue);
    setError(null);
  };

  const getAttackModeName = (mode: number) => {
    const modes: { [key: number]: string } = {
      0: t('createJob.attackModes.dictionary') as string,
      1: t('createJob.attackModes.combination') as string,
      3: t('createJob.attackModes.bruteforce') as string,
      6: t('createJob.attackModes.hybridWordlistMask') as string,
      7: t('createJob.attackModes.hybridMaskWordlist') as string,
      9: t('createJob.attackModes.association') as string
    };
    return modes[mode] || (t('createJob.attackModes.modeFallback', { mode }) as string);
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
        association_wordlist_id: undefined,
        custom_charsets: null,
        custom_charset_file_ids: null,
        hex_charset: false,
        additional_args: '',
        cloud_burst_enabled: false,
        cloud_allow_community_hosts: false,
        cloud_max_instances: undefined
      });
      setTabValue(0);
      setCustomJobName('');
      // Reset ephemeral filter state
      setFilterEnabled(false);
      setCustomFilter({});
      // Reset loopback state (GH #64)
      setPresetLoopback(false);
      setCustomLoopback(false);
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
        {t('createJob.title', { name: hashlistName })}
      </DialogTitle>

      <DialogContent>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
            {error}
          </Alert>
        )}

        {success && (
          <Alert severity="success" sx={{ mb: 2 }}>
            {t('createJob.successRedirect') as string}
          </Alert>
        )}

        <Tabs value={tabValue} onChange={handleTabChange} sx={{ mb: 3 }}>
          <Tab icon={<WorkflowIcon />} label={t('createJob.tabs.workflows') as string} />
          <Tab icon={<WorkIcon />} label={t('createJob.tabs.presetJobs') as string} />
          <Tab icon={<CustomIcon />} label={t('createJob.tabs.customJob') as string} />
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
                    {t('createJob.workflows.empty') as string}
                  </Alert>
                ) : (
                  <>
                    <TextField
                      fullWidth
                      label={t('createJob.form.jobName') as string}
                      placeholder={t('createJob.form.jobNamePlaceholder') as string}
                      value={customJobName}
                      onChange={(e) => setCustomJobName(e.target.value)}
                      helperText={t('createJob.workflows.jobNameHelper') as string}
                      sx={{ mb: 3 }}
                    />
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      {t('createJob.workflows.description') as string}
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
                                    label={t('createJob.workflows.jobsCount', { count: workflow.steps?.length || 0 }) as string}
                                    sx={{ mr: 1 }}
                                  />
                                  {(workflow.loopback_all_eligible || (workflow.loopback_step_count ?? 0) > 0) && (
                                    <Chip
                                      size="small"
                                      color="secondary"
                                      variant="outlined"
                                      label={
                                        workflow.loopback_all_eligible
                                          ? (t('createJob.workflows.loopbackAllEligible') as string)
                                          : (t('createJob.workflows.loopbackSteps', { count: workflow.loopback_step_count }) as string)
                                      }
                                      sx={{ mr: 1 }}
                                    />
                                  )}
                                  {workflow.has_high_priority_override && (
                                    <Chip
                                      size="small"
                                      label={t('createJob.canInterrupt') as string}
                                      color="error"
                                      variant="filled"
                                    />
                                  )}
                                </Box>
                                {workflow.steps && workflow.steps.length > 0 && (
                                  <Typography variant="caption" display="block" sx={{ mt: 1 }}>
                                    {t('createJob.workflows.jobsList', { names: workflow.steps.map(s => s.preset_job_name).filter(Boolean).join(', ') }) as string}
                                  </Typography>
                                )}
                              </Box>
                            }
                          />
                        </ListItem>
                      ))}
                    </List>
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
                      {t('createJob.workflows.selectedCount', { count: selectedWorkflows.length }) as string}
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
                    {t('createJob.presetJobs.empty') as string}
                  </Alert>
                ) : (
                  <>
                    <TextField
                      fullWidth
                      label={t('createJob.form.jobName') as string}
                      placeholder={t('createJob.form.jobNamePlaceholder') as string}
                      value={customJobName}
                      onChange={(e) => setCustomJobName(e.target.value)}
                      helperText={t('createJob.presetJobs.jobNameHelper') as string}
                      sx={{ mb: 3 }}
                    />
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                      {t('createJob.presetJobs.description') as string}
                    </Typography>
                    {/* Loopback toggle (GH #64) kept ABOVE the preset list so it's visible
                        without scrolling past a long list. Enables once an eligible preset
                        (straight + rules, or a hybrid) is selected. */}
                    {(() => {
                      const anyEligible = selectedPresetJobs.some(id => {
                        const p = presetJobs.find(j => j.id === id);
                        return p ? isLoopbackEligibleMode(p.attack_mode, p.rule_ids) : false;
                      });
                      const helper = anyEligible
                        ? (t('createJob.loopback.helperEligible') as string)
                        : selectedPresetJobs.length === 0
                          ? (t('createJob.loopback.helperSelectEligible') as string)
                          : (t('createJob.loopback.helperNoneEligible') as string);
                      return (
                        <Box sx={{ mb: 2, p: 2, borderRadius: 1, bgcolor: 'action.hover' }}>
                          <FormControlLabel
                            control={
                              <Checkbox
                                checked={presetLoopback && anyEligible}
                                disabled={!anyEligible}
                                onChange={(e) => setPresetLoopback(e.target.checked)}
                              />
                            }
                            label={t('createJob.loopback.label') as string}
                          />
                          <FormHelperText>{helper}</FormHelperText>
                        </Box>
                      );
                    })()}
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
                                    label={t('createJob.presetJobs.priorityLabel', { priority: job.priority }) as string}
                                    sx={{ mr: 1 }}
                                  />
                                  {job.allow_high_priority_override && (
                                    <Chip
                                      size="small"
                                      label={t('createJob.canInterrupt') as string}
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
                      {t('createJob.presetJobs.selectedCount', { count: selectedPresetJobs.length }) as string}
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
                      label={t('createJob.form.jobName') as string}
                      placeholder={t('createJob.form.jobNamePlaceholder') as string}
                      value={customJob.name}
                      onChange={(e) => setCustomJob(prev => ({ ...prev, name: e.target.value }))}
                      helperText={t('createJob.form.jobNameHelper') as string}
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <FormControl fullWidth>
                      <InputLabel>{t('createJob.form.attackModeLabel') as string}</InputLabel>
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
                            association_wordlist_id: undefined,
                            custom_charsets: null,
                            custom_charset_file_ids: null,
                            hex_charset: false
                          }));
                          // Reset combination wordlist state
                          setCombWordlist1('');
                          setCombWordlist2('');
                          // Reset association wordlist state
                          setSelectedAssociationWordlist('');
                        }}
                        label={t('createJob.form.attackModeLabel') as string}
                      >
                        <MenuItem value={0}>{t('createJob.form.attackModeDictionary') as string}</MenuItem>
                        <MenuItem value={1}>{t('createJob.form.attackModeCombination') as string}</MenuItem>
                        <MenuItem value={3}>{t('createJob.form.attackModeBruteforce') as string}</MenuItem>
                        <MenuItem value={6}>{t('createJob.form.attackModeHybridWordlistMask') as string}</MenuItem>
                        <MenuItem value={7}>{t('createJob.form.attackModeHybridMaskWordlist') as string}</MenuItem>
                        <MenuItem
                          value={9}
                          disabled={hasMixedWorkFactors || associationWordlists.length === 0}
                        >
                          {t('createJob.form.attackModeAssociation') as string}
                          {hasMixedWorkFactors && (t('createJob.form.associationBlockedMixed') as string)}
                          {!hasMixedWorkFactors && associationWordlists.length === 0 && (t('createJob.form.associationNoWordlists') as string)}
                        </MenuItem>
                      </Select>
                    </FormControl>
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <BinaryVersionSelector
                      value={customJob.binary_version}
                      onChange={(value) => setCustomJob(prev => ({ ...prev, binary_version: value }))}
                      margin="none"
                      helperText={t('createJob.form.binaryVersionHelper') as string}
                    />
                  </Grid>

                  {/* Attack mode 0 (Dictionary): Wordlists → Rules */}
                  {customJob.attack_mode === 0 && (
                    <>
                      <Grid item xs={12}>
                        <Autocomplete
                          multiple
                          options={combinedWordlistOptions}
                          groupBy={(option) => option.category}
                          getOptionLabel={(option) => {
                            const sizeStr = option.file_size >= 1024 * 1024
                              ? `${(option.file_size / 1024 / 1024).toFixed(2)} MB`
                              : `${(option.file_size / 1024).toFixed(1)} KB`;
                            if (option.line_count) {
                              return `${option.name} (${option.line_count.toLocaleString()} lines, ${sizeStr})`;
                            }
                            return `${option.name} (${sizeStr})`;
                          }}
                          value={combinedWordlistOptions.filter(w => customJob.wordlist_ids.includes(w.id))}
                          onChange={(e, newValue) => {
                            setCustomJob(prev => ({
                              ...prev,
                              wordlist_ids: newValue.map(w => w.id)
                            }));
                          }}
                          isOptionEqualToValue={(option, value) => option.id === value.id}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label={t('createJob.form.wordlists') as string}
                              placeholder={t('createJob.form.selectWordlists') as string}
                            />
                          )}
                          renderGroup={(params) => (
                            <li key={params.key}>
                              <Typography
                                sx={{
                                  fontWeight: 'bold',
                                  fontSize: '0.875rem',
                                  color: 'text.secondary',
                                  px: 2,
                                  py: 1,
                                  bgcolor: 'action.hover'
                                }}
                              >
                                {params.group === 'Client Specific' ? (t('createJob.form.categoryClientSpecific') as string) : (t('createJob.form.categoryGlobal') as string)}
                              </Typography>
                              <ul style={{ padding: 0 }}>{params.children}</ul>
                            </li>
                          )}
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <Autocomplete
                          options={formData?.rules || []}
                          getOptionLabel={(option) => t('createJob.form.ruleOptionLabel', { name: option.name, count: option.rule_count }) as string}
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
                              label={t('createJob.form.ruleOptional') as string}
                              placeholder={t('createJob.form.selectRule') as string}
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
                          <InputLabel shrink>{t('createJob.form.firstWordlist') as string}</InputLabel>
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
                            label={t('createJob.form.firstWordlist') as string}
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>{t('createJob.form.selectFirstWordlist') as string}</em></MenuItem>
                            {combinedWordlistOptions.filter(w => w.category === 'Client Specific').length > 0 && (
                              <ListSubheader>{t('createJob.form.categoryClientSpecific') as string}</ListSubheader>
                            )}
                            {combinedWordlistOptions
                              .filter(w => w.category === 'Client Specific')
                              .map((w) => (
                                <MenuItem key={`first-${w.id}`} value={w.id}>
                                  {w.line_count
                                    ? t('createJob.form.wordlistOptionWithLines', { name: w.name, lines: w.line_count.toLocaleString() })
                                    : t('createJob.form.wordlistOptionWithSize', { name: w.name, size: (w.file_size / 1024 / 1024).toFixed(2) })}
                                </MenuItem>
                              ))
                            }
                            {combinedWordlistOptions.filter(w => w.category === 'Global').length > 0 && (
                              <ListSubheader>{t('createJob.form.categoryGlobal') as string}</ListSubheader>
                            )}
                            {combinedWordlistOptions
                              .filter(w => w.category === 'Global')
                              .map((w) => (
                                <MenuItem key={`first-${w.id}`} value={w.id}>
                                  {t('createJob.form.wordlistOptionWithSize', { name: w.name, size: (w.file_size / 1024 / 1024).toFixed(2) })}
                                </MenuItem>
                              ))
                            }
                          </Select>
                        </FormControl>
                      </Grid>
                      <Grid item xs={12} sm={6}>
                        <FormControl fullWidth required>
                          <InputLabel shrink>{t('createJob.form.secondWordlist') as string}</InputLabel>
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
                            label={t('createJob.form.secondWordlist') as string}
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>{t('createJob.form.selectSecondWordlist') as string}</em></MenuItem>
                            {combinedWordlistOptions.filter(w => w.category === 'Client Specific').length > 0 && (
                              <ListSubheader>{t('createJob.form.categoryClientSpecific') as string}</ListSubheader>
                            )}
                            {combinedWordlistOptions
                              .filter(w => w.category === 'Client Specific')
                              .map((w) => (
                                <MenuItem key={`second-${w.id}`} value={w.id}>
                                  {w.line_count
                                    ? t('createJob.form.wordlistOptionWithLines', { name: w.name, lines: w.line_count.toLocaleString() })
                                    : t('createJob.form.wordlistOptionWithSize', { name: w.name, size: (w.file_size / 1024 / 1024).toFixed(2) })}
                                </MenuItem>
                              ))
                            }
                            {combinedWordlistOptions.filter(w => w.category === 'Global').length > 0 && (
                              <ListSubheader>{t('createJob.form.categoryGlobal') as string}</ListSubheader>
                            )}
                            {combinedWordlistOptions
                              .filter(w => w.category === 'Global')
                              .map((w) => (
                                <MenuItem key={`second-${w.id}`} value={w.id}>
                                  {t('createJob.form.wordlistOptionWithSize', { name: w.name, size: (w.file_size / 1024 / 1024).toFixed(2) })}
                                </MenuItem>
                              ))
                            }
                          </Select>
                        </FormControl>
                      </Grid>
                    </>
                  )}

                  {/* Attack mode 3 (Brute Force): Mask + Custom Charsets */}
                  {customJob.attack_mode === 3 && (
                    <>
                      <Grid item xs={12}>
                        <TextField
                          fullWidth
                          label={t('createJob.form.mask') as string}
                          value={customJob.mask}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, mask: e.target.value }))}
                          placeholder={t('createJob.form.maskPlaceholder') as string}
                          helperText={t('createJob.form.maskHelper') as string}
                          required
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <CharsetInputs
                          customCharsets={customJob.custom_charsets || {}}
                          charsetFileIds={customJob.custom_charset_file_ids || {}}
                          onChange={(charsets, fileIds) => setCustomJob(prev => ({
                            ...prev,
                            custom_charsets: Object.keys(charsets).length > 0 ? charsets : null,
                            custom_charset_file_ids: fileIds && Object.keys(fileIds).length > 0 ? fileIds : null
                          }))}
                          mask={customJob.mask}
                          savedCharsets={savedCharsets}
                          hexCharset={customJob.hex_charset}
                          onHexCharsetChange={(hex) => setCustomJob(prev => ({ ...prev, hex_charset: hex }))}
                        />
                      </Grid>
                    </>
                  )}

                  {/* Attack mode 6 (Hybrid Wordlist + Mask): Wordlists → Mask → Custom Charsets */}
                  {customJob.attack_mode === 6 && (
                    <>
                      <Grid item xs={12}>
                        <Autocomplete
                          multiple
                          options={combinedWordlistOptions}
                          groupBy={(option) => option.category}
                          getOptionLabel={(option) => {
                            const sizeStr = option.file_size >= 1024 * 1024
                              ? `${(option.file_size / 1024 / 1024).toFixed(2)} MB`
                              : `${(option.file_size / 1024).toFixed(1)} KB`;
                            if (option.line_count) {
                              return `${option.name} (${option.line_count.toLocaleString()} lines, ${sizeStr})`;
                            }
                            return `${option.name} (${sizeStr})`;
                          }}
                          value={combinedWordlistOptions.filter(w => customJob.wordlist_ids.includes(w.id))}
                          onChange={(e, newValue) => {
                            setCustomJob(prev => ({
                              ...prev,
                              wordlist_ids: newValue.map(w => w.id)
                            }));
                          }}
                          isOptionEqualToValue={(option, value) => option.id === value.id}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label={t('createJob.form.wordlists') as string}
                              placeholder={t('createJob.form.selectWordlists') as string}
                            />
                          )}
                          renderGroup={(params) => (
                            <li key={params.key}>
                              <Typography
                                sx={{
                                  fontWeight: 'bold',
                                  fontSize: '0.875rem',
                                  color: 'text.secondary',
                                  px: 2,
                                  py: 1,
                                  bgcolor: 'action.hover'
                                }}
                              >
                                {params.group === 'Client Specific' ? (t('createJob.form.categoryClientSpecific') as string) : (t('createJob.form.categoryGlobal') as string)}
                              </Typography>
                              <ul style={{ padding: 0 }}>{params.children}</ul>
                            </li>
                          )}
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <TextField
                          fullWidth
                          label={t('createJob.form.mask') as string}
                          value={customJob.mask}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, mask: e.target.value }))}
                          placeholder={t('createJob.form.maskPlaceholder') as string}
                          helperText={t('createJob.form.maskHelper') as string}
                          required
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <CharsetInputs
                          customCharsets={customJob.custom_charsets || {}}
                          charsetFileIds={customJob.custom_charset_file_ids || {}}
                          onChange={(charsets, fileIds) => setCustomJob(prev => ({
                            ...prev,
                            custom_charsets: Object.keys(charsets).length > 0 ? charsets : null,
                            custom_charset_file_ids: fileIds && Object.keys(fileIds).length > 0 ? fileIds : null
                          }))}
                          mask={customJob.mask}
                          savedCharsets={savedCharsets}
                          hexCharset={customJob.hex_charset}
                          onHexCharsetChange={(hex) => setCustomJob(prev => ({ ...prev, hex_charset: hex }))}
                        />
                      </Grid>
                    </>
                  )}

                  {/* Attack mode 7 (Hybrid Mask + Wordlist): Mask → Custom Charsets → Wordlists */}
                  {customJob.attack_mode === 7 && (
                    <>
                      <Grid item xs={12}>
                        <TextField
                          fullWidth
                          label={t('createJob.form.mask') as string}
                          value={customJob.mask}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, mask: e.target.value }))}
                          placeholder={t('createJob.form.maskPlaceholder') as string}
                          helperText={t('createJob.form.maskHelper') as string}
                          required
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <CharsetInputs
                          customCharsets={customJob.custom_charsets || {}}
                          charsetFileIds={customJob.custom_charset_file_ids || {}}
                          onChange={(charsets, fileIds) => setCustomJob(prev => ({
                            ...prev,
                            custom_charsets: Object.keys(charsets).length > 0 ? charsets : null,
                            custom_charset_file_ids: fileIds && Object.keys(fileIds).length > 0 ? fileIds : null
                          }))}
                          mask={customJob.mask}
                          savedCharsets={savedCharsets}
                          hexCharset={customJob.hex_charset}
                          onHexCharsetChange={(hex) => setCustomJob(prev => ({ ...prev, hex_charset: hex }))}
                        />
                      </Grid>
                      <Grid item xs={12}>
                        <Autocomplete
                          multiple
                          options={combinedWordlistOptions}
                          groupBy={(option) => option.category}
                          getOptionLabel={(option) => {
                            const sizeStr = option.file_size >= 1024 * 1024
                              ? `${(option.file_size / 1024 / 1024).toFixed(2)} MB`
                              : `${(option.file_size / 1024).toFixed(1)} KB`;
                            if (option.line_count) {
                              return `${option.name} (${option.line_count.toLocaleString()} lines, ${sizeStr})`;
                            }
                            return `${option.name} (${sizeStr})`;
                          }}
                          value={combinedWordlistOptions.filter(w => customJob.wordlist_ids.includes(w.id))}
                          onChange={(e, newValue) => {
                            setCustomJob(prev => ({
                              ...prev,
                              wordlist_ids: newValue.map(w => w.id)
                            }));
                          }}
                          isOptionEqualToValue={(option, value) => option.id === value.id}
                          renderInput={(params) => (
                            <TextField
                              {...params}
                              label={t('createJob.form.wordlists') as string}
                              placeholder={t('createJob.form.selectWordlists') as string}
                            />
                          )}
                          renderGroup={(params) => (
                            <li key={params.key}>
                              <Typography
                                sx={{
                                  fontWeight: 'bold',
                                  fontSize: '0.875rem',
                                  color: 'text.secondary',
                                  px: 2,
                                  py: 1,
                                  bgcolor: 'action.hover'
                                }}
                              >
                                {params.group === 'Client Specific' ? (t('createJob.form.categoryClientSpecific') as string) : (t('createJob.form.categoryGlobal') as string)}
                              </Typography>
                              <ul style={{ padding: 0 }}>{params.children}</ul>
                            </li>
                          )}
                        />
                      </Grid>
                    </>
                  )}

                  {/* Ephemeral wordlist filter (GH #40) - wordlist-based attacks only */}
                  {[0, 1, 6, 7].includes(customJob.attack_mode) && customJob.wordlist_ids.length > 0 && (
                    <Grid item xs={12}>
                      <FormControlLabel
                        control={
                          <Checkbox
                            checked={filterEnabled}
                            onChange={(e) => setFilterEnabled(e.target.checked)}
                          />
                        }
                        label={t('createJob.filter.enableLabel') as string}
                      />
                      {filterEnabled && (
                        <Box sx={{ pl: 1, pt: 1 }}>
                          <Alert severity="info" sx={{ mb: 2 }}>
                            {t('createJob.filter.ephemeralNotice') as string}
                          </Alert>
                          <FilterCriteriaForm
                            value={customFilter}
                            onChange={setCustomFilter}
                            showPreview={false}
                          />
                        </Box>
                      )}
                    </Grid>
                  )}

                  {/* Loopback (GH #64) - mutatable attacks only */}
                  {isLoopbackEligibleMode(customJob.attack_mode, customJob.rule_ids) && (
                    <Grid item xs={12}>
                      <FormControlLabel
                        control={
                          <Checkbox
                            checked={customLoopback && !filterEnabled}
                            disabled={filterEnabled}
                            onChange={(e) => setCustomLoopback(e.target.checked)}
                          />
                        }
                        label={t('createJob.loopback.label') as string}
                      />
                      <FormHelperText>
                        {filterEnabled
                          ? (t('createJob.loopback.helperFilterConflict') as string)
                          : (t('createJob.loopback.helperCustom') as string)}
                      </FormHelperText>
                    </Grid>
                  )}

                  {/* Increment Mode - only for mask-based attacks */}
                  {(customJob.attack_mode === 3 || customJob.attack_mode === 6 || customJob.attack_mode === 7) && (
                    <>
                      <Grid item xs={12}>
                        <FormControl fullWidth>
                          <InputLabel>{t('createJob.form.incrementModeLabel') as string}</InputLabel>
                          <Select
                            value={customJob.increment_mode}
                            onChange={(e) => setCustomJob(prev => ({ ...prev, increment_mode: e.target.value }))}
                            label={t('createJob.form.incrementModeLabel') as string}
                          >
                            <MenuItem value="off">{t('createJob.form.incrementModeOff') as string}</MenuItem>
                            <MenuItem value="increment">{t('createJob.form.incrementModeForward') as string}</MenuItem>
                            <MenuItem value="increment_inverse">{t('createJob.form.incrementModeInverse') as string}</MenuItem>
                          </Select>
                          <FormHelperText>
                            {t('createJob.form.incrementModeHelper') as string}
                          </FormHelperText>
                        </FormControl>
                      </Grid>

                      {customJob.increment_mode !== 'off' && (
                        <Grid item xs={12}>
                          <Grid container spacing={2}>
                            <Grid item xs={6}>
                              <TextField
                                fullWidth
                                label={t('createJob.form.minLength') as string}
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
                                label={t('createJob.form.maxLength') as string}
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
                            {t('createJob.association.explainer', { hashCount: totalHashes.toLocaleString() })}
                          </Typography>
                        </Alert>
                        <Alert severity="warning" sx={{ mb: 2 }}>
                          <Typography variant="body2">
                            <Trans t={t} i18nKey="createJob.association.falsePositiveWarning" components={{ strong: <strong /> }} />
                          </Typography>
                        </Alert>
                      </Grid>
                      <Grid item xs={12}>
                        <FormControl fullWidth required>
                          <InputLabel shrink>{t('createJob.association.wordlistLabel') as string}</InputLabel>
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
                            label={t('createJob.association.wordlistLabel') as string}
                            displayEmpty
                          >
                            <MenuItem value="" disabled><em>{t('createJob.association.selectWordlist') as string}</em></MenuItem>
                            {associationWordlists.map((w) => (
                              <MenuItem key={w.id} value={w.id}>
                                {t('createJob.association.wordlistOptionLabel', {
                                  name: w.file_name,
                                  lines: w.line_count.toLocaleString(),
                                  size: (w.file_size / 1024).toFixed(1)
                                }) as string}
                              </MenuItem>
                            ))}
                          </Select>
                          <FormHelperText>
                            {t('createJob.association.wordlistHelper') as string}
                          </FormHelperText>
                        </FormControl>
                      </Grid>
                      <Grid item xs={12}>
                        <Autocomplete
                          options={formData?.rules || []}
                          getOptionLabel={(option) => t('createJob.form.ruleOptionLabel', { name: option.name, count: option.rule_count }) as string}
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
                              label={t('createJob.form.ruleOptional') as string}
                              placeholder={t('createJob.form.selectRule') as string}
                            />
                          )}
                        />
                      </Grid>
                    </>
                  )}

                  <Grid item xs={12} sm={6}>
                    <TextField
                      fullWidth
                      label={t('createJob.form.priority') as string}
                      type="number"
                      value={customJob.priority}
                      onChange={(e) => {
                        const value = parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, priority: value }));
                      }}
                      inputProps={{ min: 1, max: 1000 }}
                      helperText={t('createJob.form.priorityHelper') as string}
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <TextField
                      fullWidth
                      label={t('createJob.form.chunkDuration') as string}
                      type="number"
                      value={customJob.chunk_duration}
                      onChange={(e) => {
                        const value = e.target.value === '' ? 0 : parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, chunk_duration: value }));
                      }}
                      helperText={t('createJob.form.chunkDurationHelper') as string}
                    />
                  </Grid>

                  <Grid item xs={12} sm={6}>
                    <TextField
                      fullWidth
                      label={t('createJob.form.maxAgents') as string}
                      type="number"
                      value={customJob.max_agents}
                      onChange={(e) => {
                        const value = parseInt(e.target.value) || 0;
                        setCustomJob(prev => ({ ...prev, max_agents: value }));
                      }}
                      inputProps={{ min: 0 }}
                      helperText={t('createJob.form.maxAgentsHelper') as string}
                    />
                  </Grid>

                  {/* Cloud burst (opt-in). Deliberately separate from Max Agents:
                      a rented instance is dedicated to this job and paid for by
                      the client, so it must not consume the shared-pool budget. */}
                  <Grid item xs={12} sm={6}>
                    <FormControlLabel
                      control={
                        <Checkbox
                          checked={customJob.cloud_burst_enabled}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, cloud_burst_enabled: e.target.checked }))}
                        />
                      }
                      label={t('createJob.cloud.allowBurst') as string}
                      sx={{ mt: 1 }}
                    />
                    <Typography variant="caption" color="text.secondary" display="block">
                      {t('createJob.cloud.allowBurstHelper') as string}
                    </Typography>
                  </Grid>

                  {/* Peer-host opt-in. Separate from the burst toggle above
                      because they are separate decisions: bursting is about
                      spending money, this is about WHOSE MACHINE the client's
                      hashes land on. Without it the job simply does not see
                      peer offers and may still rent secure capacity. */}
                  {customJob.cloud_burst_enabled && (
                    <Grid item xs={12}>
                      <FormControlLabel
                        control={
                          <Checkbox
                            checked={customJob.cloud_allow_community_hosts}
                            onChange={(e) => setCustomJob(prev => ({ ...prev, cloud_allow_community_hosts: e.target.checked }))}
                          />
                        }
                        label={t('createJob.cloud.allowPeerHosts') as string}
                      />
                      <Alert severity="warning" sx={{ mt: 1 }}>
                        <Trans t={t} i18nKey="createJob.cloud.peerHostsWarning" components={{ strong: <strong /> }} />
                      </Alert>
                    </Grid>
                  )}

                  {customJob.cloud_burst_enabled && (
                    <Grid item xs={12} sm={6}>
                      <TextField
                        fullWidth
                        label={t('createJob.cloud.maxInstances') as string}
                        type="number"
                        value={customJob.cloud_max_instances ?? ''}
                        onChange={(e) => {
                          const raw = e.target.value;
                          setCustomJob(prev => ({
                            ...prev,
                            cloud_max_instances: raw === '' ? undefined : Math.max(1, parseInt(raw) || 1)
                          }));
                        }}
                        inputProps={{ min: 1 }}
                        helperText={t('createJob.cloud.maxInstancesHelper') as string}
                      />
                    </Grid>
                  )}

                  <Grid item xs={12} sm={6}>
                    <FormControlLabel
                      control={
                        <Checkbox
                          checked={customJob.allow_high_priority_override}
                          onChange={(e) => setCustomJob(prev => ({ ...prev, allow_high_priority_override: e.target.checked }))}
                        />
                      }
                      label={t('createJob.form.allowHighPriorityOverride') as string}
                      sx={{ mt: 1 }}
                    />
                  </Grid>

                  {/* Additional Hashcat Arguments */}
                  <Grid item xs={12}>
                    <TextField
                      fullWidth
                      label={t('createJob.additionalArgs') as string}
                      value={customJob.additional_args || ''}
                      onChange={(e) => setCustomJob(prev => ({ ...prev, additional_args: e.target.value }))}
                      placeholder={t('createJob.form.additionalArgsPlaceholder') as string}
                      helperText={t('createJob.additionalArgsHelper') as string}
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
          {t('createJob.cancel') as string}
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
          {loading ? loadingMessage : (t('createJob.submit') as string)}
        </Button>
      </DialogActions>
    </Dialog>
  );
}