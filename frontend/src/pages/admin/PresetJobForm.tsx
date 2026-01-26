import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  TextField,
  Button,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Checkbox,
  FormControlLabel,
  SelectChangeEvent,
  Chip,
  OutlinedInput,
  FormHelperText,
  Grid,
  CircularProgress,
  Alert,
  Paper,
  Tooltip
} from '@mui/material';
import { useParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { 
  getPresetJobFormData, 
  getPresetJob, 
  createPresetJob, 
  updatePresetJob 
} from '../../services/api';
import { getMaxPriorityForUsers } from '../../services/systemSettings';
import { getJobExecutionSettings } from '../../services/jobSettings';
import {
  PresetJob,
  PresetJobInput,
  PresetJobFormData,
  PresetJobApiData,
  AttackMode,
  WordlistBasic,
  RuleBasic
} from '../../types/adminJobs';
import BinaryVersionSelector from '../../components/common/BinaryVersionSelector';

const ITEM_HEIGHT = 48;
const ITEM_PADDING_TOP = 8;
const MenuProps = {
  PaperProps: {
    style: {
      maxHeight: ITEM_HEIGHT * 4.5 + ITEM_PADDING_TOP,
      width: 250,
    },
  },
};

// Initial form state with default values - chunk_size_seconds will be set dynamically
const getInitialFormState = (defaultChunkDuration: number = 300): PresetJobFormData => ({
  name: '',
  wordlist_ids: [],
  rule_ids: [],
  attack_mode: AttackMode.Straight,
  priority: '', // Empty string to show placeholder
  chunk_size_seconds: defaultChunkDuration,
  binary_version: 'default',
  allow_high_priority_override: false,
  mask: '',
  max_agents: 0,
  increment_mode: 'off',
  increment_min: undefined as number | undefined,
  increment_max: undefined as number | undefined
});

// Attack mode descriptions and requirements
const attackModeInfo = {
  [AttackMode.Straight]: {
    name: 'Straight',
    description: 'Uses words from a wordlist, optionally applying a rule to transform them',
    wordlistRequirement: 'Exactly 1 wordlist required',
    rulesRequirement: '0 or 1 rule file',
    maskRequirement: 'No mask needed'
  },
  [AttackMode.Combination]: {
    name: 'Combination',
    description: 'Combines words from two wordlists (first_word + second_word)',
    wordlistRequirement: 'Exactly 2 wordlists required',
    rulesRequirement: 'No rules needed',
    maskRequirement: 'No mask needed'
  },
  [AttackMode.BruteForce]: {
    name: 'Brute Force (Mask)',
    description: 'Generates passwords based on a pattern/mask',
    wordlistRequirement: 'No wordlist needed',
    rulesRequirement: 'No rules needed',
    maskRequirement: 'Mask required (e.g., ?u?l?l?l?d?d)'
  },
  [AttackMode.HybridWordlistMask]: {
    name: 'Hybrid: Wordlist + Mask',
    description: 'Appends mask-generated characters to words from a wordlist',
    wordlistRequirement: 'Exactly 1 wordlist required',
    rulesRequirement: 'No rules needed',
    maskRequirement: 'Mask required (e.g., ?d?d?d?d)'
  },
  [AttackMode.HybridMaskWordlist]: {
    name: 'Hybrid: Mask + Wordlist',
    description: 'Prepends mask-generated characters to words from a wordlist',
    wordlistRequirement: 'Exactly 1 wordlist required',
    rulesRequirement: 'No rules needed',
    maskRequirement: 'Mask required (e.g., ?d?d?d?d)'
  },
  [AttackMode.Association]: {
    name: 'Association (Not Implemented)',
    description: 'This attack mode is not currently implemented',
    wordlistRequirement: 'N/A',
    rulesRequirement: 'N/A',
    maskRequirement: 'N/A'
  }
};

const PresetJobFormPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const { presetJobId } = useParams<{ presetJobId?: string }>();
  const navigate = useNavigate();
  const isEditing = Boolean(presetJobId);
  
  // Form state - initialize with a temporary default, will be updated after fetching settings
  const [formData, setFormData] = useState<PresetJobFormData>(getInitialFormState(300));
  const [defaultChunkDuration, setDefaultChunkDuration] = useState<number>(1200);
  
  // Form options from API
  const [wordlists, setWordlists] = useState<WordlistBasic[]>([]);
  const [rules, setRules] = useState<RuleBasic[]>([]);
  
  // Loading and error states
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [maxPriority, setMaxPriority] = useState<number>(1000);

  // Get current attack mode info
  const currentModeInfo = attackModeInfo[formData.attack_mode];

  // Additional state for combination attack mode
  const [firstWordlist, setFirstWordlist] = useState<string>('');
  const [secondWordlist, setSecondWordlist] = useState<string>('');

  // Fetch form data and preset job if in edit mode
  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        
        // Fetch form options (wordlists, rules, binary versions), max priority, and job execution settings
        const [formDataResponse, maxPriorityResponse, jobExecutionSettings] = await Promise.all([
          getPresetJobFormData(),
          getMaxPriorityForUsers(),
          getJobExecutionSettings().catch(() => null) // Gracefully handle if settings fetch fails
        ]);
        
        setMaxPriority(maxPriorityResponse.max_priority);
        
        // Set default chunk duration from system settings
        let systemDefaultChunkDuration = 1200; // fallback to 20 minutes
        if (jobExecutionSettings?.default_chunk_duration) {
          systemDefaultChunkDuration = jobExecutionSettings.default_chunk_duration;
        }
        setDefaultChunkDuration(systemDefaultChunkDuration);

        if (!formDataResponse.wordlists?.length) {
          setError(t('presetJobs.form.errors.noWordlistsAvailable') as string);
          setLoading(false);
          return;
        }

        setWordlists(formDataResponse.wordlists);
        setRules(formDataResponse.rules || []);
        
        // If editing, fetch the preset job data
        if (isEditing && presetJobId) {
          try {
            const presetJob = await getPresetJob(presetJobId);
            setFormData({
              name: presetJob.name,
              // Convert string UUIDs to numbers for form handling
              wordlist_ids: presetJob.wordlist_ids.map(id => parseInt(id)),
              rule_ids: presetJob.rule_ids.map(id => parseInt(id)),
              attack_mode: presetJob.attack_mode,
              priority: presetJob.priority,
              chunk_size_seconds: presetJob.chunk_size_seconds,
              binary_version: presetJob.binary_version || 'default',
              allow_high_priority_override: presetJob.allow_high_priority_override,
              mask: presetJob.mask || '',
              max_agents: presetJob.max_agents || 0,
              increment_mode: presetJob.increment_mode || 'off',
              increment_min: presetJob.increment_min ?? undefined,
              increment_max: presetJob.increment_max ?? undefined
            });

            // Initialize combination wordlists if in combination mode
            if (presetJob.attack_mode === AttackMode.Combination) {
              if (presetJob.wordlist_ids?.length >= 1) {
                setFirstWordlist(presetJob.wordlist_ids[0]);
              }
              if (presetJob.wordlist_ids?.length >= 2) {
                setSecondWordlist(presetJob.wordlist_ids[1]);
              }
            }
          } catch (err) {
            console.error('Error fetching preset job:', err);
            setError(t('presetJobs.form.errors.loadFailed') as string);
          }
        } else {
          // For new jobs, set default chunk duration (binary_version already defaults to 'default')
          setFormData(prev => ({
            ...prev,
            chunk_size_seconds: systemDefaultChunkDuration
          }));
        }
        
        setLoading(false);
      } catch (err) {
        console.error('Error fetching form data:', err);
        setError(t('presetJobs.form.errors.loadFormDataFailed') as string);
        setLoading(false);
      }
    };

    fetchData();
  }, [isEditing, presetJobId]);

  // Handle form field changes
  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, type, checked } = e.target;
    
    let convertedValue: any = type === 'checkbox' ? checked : value;
    
    // Convert numeric fields to numbers, but allow empty values for better UX
    if (name === 'priority' || name === 'chunk_size_seconds' || name === 'max_agents') {
      // Allow empty string during editing, convert to number otherwise
      convertedValue = value === '' ? '' : parseInt(value) || 0;
    }
    
    setFormData(prev => ({
      ...prev,
      [name]: convertedValue
    }));
  };

  // Handle field blur to ensure numeric fields have valid values
  const handleBlur = (e: React.FocusEvent<HTMLInputElement>) => {
    const { name, value } = e.target;
    
    if (name === 'priority') {
      // If priority is empty on blur, set it to default 10
      if (value === '' || isNaN(parseInt(value))) {
        setFormData(prev => ({
          ...prev,
          priority: 10
        }));
      }
    } else if (name === 'chunk_size_seconds') {
      // If chunk_size_seconds is empty on blur, set it to system default
      if (value === '' || isNaN(parseInt(value))) {
        setFormData(prev => ({
          ...prev,
          chunk_size_seconds: defaultChunkDuration
        }));
      }
    }
  };

  // Handle select changes
  const handleSelectChange = (e: SelectChangeEvent<unknown>, name: string) => {
    const value = e.target.value;
    
    // If changing attack mode, we need to reset certain fields based on the new mode
    if (name === 'attack_mode') {
      const newAttackMode = value as AttackMode;
      
      // Prepare updates based on the new attack mode
      const updates: Partial<typeof formData> = {
        attack_mode: newAttackMode
      };
      
      // Reset wordlist selection based on attack mode
      if (newAttackMode === AttackMode.Straight || 
          newAttackMode === AttackMode.HybridWordlistMask || 
          newAttackMode === AttackMode.HybridMaskWordlist) {
        // For modes requiring exactly one wordlist, keep only the first selected if any
        updates.wordlist_ids = formData.wordlist_ids.length > 0 ? [formData.wordlist_ids[0]] : [];
      } else if (newAttackMode === AttackMode.Combination) {
        // For combination mode, initialize separate wordlist selectors
        if (formData.wordlist_ids.length > 0) {
          setFirstWordlist(formData.wordlist_ids[0].toString());
          setSecondWordlist(formData.wordlist_ids.length > 1 ? formData.wordlist_ids[1].toString() : formData.wordlist_ids[0].toString());
        } else {
          // Start with empty selections to show placeholders
          setFirstWordlist('');
          setSecondWordlist('');
        }
        
        // Initialize wordlist_ids based on the above selections
        updates.wordlist_ids = [parseInt(firstWordlist), parseInt(secondWordlist)].filter(id => !isNaN(id));
      } else if (newAttackMode === AttackMode.BruteForce) {
        // Brute force doesn't use wordlists
        updates.wordlist_ids = [];
      }
      
      // Reset rules selection based on attack mode
      if (newAttackMode !== AttackMode.Straight) {
        // Only straight mode uses rules
        updates.rule_ids = [];
      }
      
      // Update form data with the new values
      setFormData(prev => ({
        ...prev,
        ...updates
      }));
    } else if (name === 'firstWordlist') {
      // Update the first wordlist for combination mode
      setFirstWordlist(value as string);
      // Update the wordlist_ids array
      const newWordlistIds = [parseInt(value as string), parseInt(secondWordlist)].filter(id => !isNaN(id));
      setFormData(prev => ({
        ...prev,
        wordlist_ids: newWordlistIds
      }));
    } else if (name === 'secondWordlist') {
      // Update the second wordlist for combination mode
      setSecondWordlist(value as string);
      // Update the wordlist_ids array
      const newWordlistIds = [parseInt(firstWordlist), parseInt(value as string)].filter(id => !isNaN(id));
      setFormData(prev => ({
        ...prev,
        wordlist_ids: newWordlistIds
      }));
    } else {
      // For other fields, just update the value
      setFormData(prev => ({
        ...prev,
        [name]: value
      }));
    }
  };

  // Handle multi-select changes
  const handleMultiSelectChange = (e: SelectChangeEvent<number[]>, name: string) => {
    const value = e.target.value as number[];
    
    // Apply special rules based on attack mode for wordlist and rule selection
    if (name === 'wordlist_ids') {
      // Enforce wordlist limits based on attack mode
      if (formData.attack_mode === AttackMode.Straight || 
          formData.attack_mode === AttackMode.HybridWordlistMask || 
          formData.attack_mode === AttackMode.HybridMaskWordlist) {
        // These modes require exactly one wordlist
        setFormData(prev => ({
          ...prev,
          wordlist_ids: value.slice(0, 1)
        }));
      } else if (formData.attack_mode === AttackMode.Combination) {
        // Combination mode requires exactly two wordlists
        setFormData(prev => ({
          ...prev,
          wordlist_ids: value.slice(0, 2)
        }));
      } else if (formData.attack_mode === AttackMode.BruteForce) {
        // Brute force doesn't use wordlists
        setFormData(prev => ({
          ...prev,
          wordlist_ids: []
        }));
      } else {
        // Default behavior for other modes
        setFormData(prev => ({
          ...prev,
          wordlist_ids: value
        }));
      }
    } else if (name === 'rule_ids') {
      // Rule selection only matters for certain attack modes
      if (formData.attack_mode === AttackMode.Straight) {
        // Straight mode can use rules
        setFormData(prev => ({
          ...prev,
          rule_ids: value
        }));
      } else {
        // Other modes don't use rules
        setFormData(prev => ({
          ...prev,
          rule_ids: []
        }));
      }
    }
  };

  // Validate form based on attack mode
  const validateForm = (): boolean => {
    if (!formData.name.trim()) {
      setError(t('presetJobs.form.errors.nameRequired') as string);
      return false;
    }

    if (!formData.binary_version) {
      setError(t('presetJobs.form.errors.binaryVersionRequired') as string);
      return false;
    }

    // Attack mode specific validation
    switch (formData.attack_mode) {
      case AttackMode.Straight:
        if (formData.wordlist_ids.length !== 1) {
          setError(t('presetJobs.form.errors.straightModeWordlist') as string);
          return false;
        }
        break;

      case AttackMode.Combination:
        if (formData.wordlist_ids.length !== 2) {
          setError(t('presetJobs.form.errors.combinationModeWordlists') as string);
          return false;
        }
        // Also check that both dropdown selections are made
        if (firstWordlist === '' || secondWordlist === '') {
          setError(t('presetJobs.form.errors.selectBothWordlists') as string);
          return false;
        }
        break;

      case AttackMode.BruteForce:
        if (!formData.mask) {
          setError(t('presetJobs.form.errors.bruteForceModeMask') as string);
          return false;
        }
        break;

      case AttackMode.HybridWordlistMask:
      case AttackMode.HybridMaskWordlist:
        if (formData.wordlist_ids.length !== 1) {
          setError(t('presetJobs.form.errors.hybridModeWordlist') as string);
          return false;
        }
        if (!formData.mask) {
          setError(t('presetJobs.form.errors.hybridModeMask') as string);
          return false;
        }
        break;

      case AttackMode.Association:
        setError(t('presetJobs.form.errors.associationModeNotImplemented') as string);
        return false;
    }

    return true;
  };

  // Handle form submission
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // For combination attack, ensure the wordlist_ids array is up to date
    if (formData.attack_mode === AttackMode.Combination) {
      // Update the formData with the current wordlist selections
      setFormData(prev => ({
        ...prev,
        wordlist_ids: [parseInt(firstWordlist), parseInt(secondWordlist)]
      }));
    }
    
    if (!validateForm()) {
      return;
    }
    
    setSubmitting(true);
    setError(null);
    setSuccessMessage(null);

    // Debug logging
    console.log('Submitting form data:', JSON.stringify(formData, null, 2));
    
    // Prepare form data for submission, applying defaults for empty fields
    const submissionData = {
      ...formData,
      priority: formData.priority === '' ? 10 : (typeof formData.priority === 'string' ? parseInt(formData.priority) || 10 : formData.priority)
    };
    
    try {
      if (isEditing && presetJobId) {
        console.log('Updating preset job:', presetJobId);
        // Type casting to handle the mismatch in types
        await updatePresetJob(presetJobId, submissionData as any);
        setSuccessMessage(t('presetJobs.form.messages.updateSuccess') as string);
      } else {
        console.log('Creating new preset job');
        // Type casting to handle the mismatch in types
        await createPresetJob(submissionData as any);
        setSuccessMessage(t('presetJobs.form.messages.createSuccess') as string);
        // Reset form after successful creation
        setFormData(getInitialFormState(defaultChunkDuration));
        // Navigate back to the preset jobs list
        setTimeout(() => {
          navigate('/admin/preset-jobs');
        }, 1500);
      }
    } catch (err) {
      console.error('Error submitting form:', err);
      setError(t('presetJobs.form.errors.saveFailed') as string);
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

  // Determine if rules should be disabled based on attack mode
  const isRulesDisabled = formData.attack_mode !== AttackMode.Straight;
  
  // Determine if wordlists should be disabled based on attack mode
  const isWordlistsDisabled = formData.attack_mode === AttackMode.BruteForce;
  
  // Determine if mask input should be shown
  const showMaskInput = formData.attack_mode === AttackMode.BruteForce || 
                        formData.attack_mode === AttackMode.HybridWordlistMask || 
                        formData.attack_mode === AttackMode.HybridMaskWordlist;

  // Get the maximum number of wordlists allowed for current mode
  const getMaxWordlists = () => {
    switch (formData.attack_mode) {
      case AttackMode.Straight:
      case AttackMode.HybridWordlistMask:
      case AttackMode.HybridMaskWordlist:
        return 1;
      case AttackMode.Combination:
        return 2;
      default:
        return 0;
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box component="form" onSubmit={handleSubmit} sx={{ maxWidth: 800, mx: 'auto' }}>
        <Typography variant="h4" gutterBottom>
          {isEditing ? t('presetJobs.form.editTitle') as string : t('presetJobs.form.createTitle') as string}
        </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {successMessage && (
        <Alert severity="success" sx={{ mb: 2 }}>
          {successMessage}
        </Alert>
      )}

      <Grid container spacing={2}>
        {/* Basic Info */}
        <Grid item xs={12}>
          <TextField
            name="name"
            label={t('presetJobs.form.fields.jobName') as string}
            value={formData.name}
            onChange={handleChange}
            fullWidth
            required
            margin="normal"
          />
        </Grid>

        {/* Attack Mode */}
        <Grid item xs={12}>
          <FormControl fullWidth margin="normal" required>
            <InputLabel id="attack-mode-label">{t('presetJobs.form.fields.attackMode') as string}</InputLabel>
            <Select
              labelId="attack-mode-label"
              name="attack_mode"
              value={formData.attack_mode}
              onChange={(e) => handleSelectChange(e, 'attack_mode')}
              label={t('presetJobs.form.fields.attackMode') as string}
            >
              <MenuItem value={AttackMode.Straight}>{attackModeInfo[AttackMode.Straight].name}</MenuItem>
              <MenuItem value={AttackMode.Combination}>{attackModeInfo[AttackMode.Combination].name}</MenuItem>
              <MenuItem value={AttackMode.BruteForce}>{attackModeInfo[AttackMode.BruteForce].name}</MenuItem>
              <MenuItem value={AttackMode.HybridWordlistMask}>{attackModeInfo[AttackMode.HybridWordlistMask].name}</MenuItem>
              <MenuItem value={AttackMode.HybridMaskWordlist}>{attackModeInfo[AttackMode.HybridMaskWordlist].name}</MenuItem>
              <MenuItem value={AttackMode.Association} disabled>{attackModeInfo[AttackMode.Association].name}</MenuItem>
            </Select>
            <FormHelperText>{currentModeInfo.description}</FormHelperText>
          </FormControl>
        </Grid>

        {/* Attack Mode Info Card */}
        <Grid item xs={12}>
          <Paper
            elevation={0}
            sx={{
              p: 2,
              backgroundColor: 'rgba(0, 0, 0, 0.04)',
              borderRadius: 1
            }}
          >
            <Typography variant="subtitle2" gutterBottom>
              {t('presetJobs.form.attackModeRequirements') as string}
            </Typography>
            <Typography variant="body2">• {t('presetJobs.form.fields.wordlists') as string}: {currentModeInfo.wordlistRequirement}</Typography>
            <Typography variant="body2">• {t('presetJobs.form.fields.rules') as string}: {currentModeInfo.rulesRequirement}</Typography>
            <Typography variant="body2">• {t('presetJobs.form.fields.mask') as string}: {currentModeInfo.maskRequirement}</Typography>
          </Paper>
        </Grid>

        {/* Binary Version */}
        <Grid item xs={12} sm={6}>
          <BinaryVersionSelector
            value={formData.binary_version}
            onChange={(value) => setFormData(prev => ({ ...prev, binary_version: value }))}
            required
            helperText={t('presetJobs.form.helperText.binaryVersion') as string}
          />
        </Grid>

        {/* Job Configuration */}
        <Grid item xs={12} sm={6}>
          <TextField
            name="priority"
            label={t('presetJobs.form.fields.priority') as string}
            type="number"
            value={formData.priority}
            onChange={handleChange}
            fullWidth
            margin="normal"
            inputProps={{ min: 0, max: maxPriority }}
            placeholder="10"
            helperText={t('presetJobs.form.helperText.priority', { maxPriority: maxPriority.toLocaleString() }) as string}
          />
        </Grid>

        {/* Mask Input - only show for certain attack modes */}
        {showMaskInput && (
          <Grid item xs={12}>
            <TextField
              name="mask"
              label={t('presetJobs.form.fields.maskPattern') as string}
              value={formData.mask || ''}
              onChange={handleChange}
              fullWidth
              required
              margin="normal"
              placeholder="?u?l?l?l?d?d?d?d"
              helperText={
                <span>
                  {t('presetJobs.form.helperText.maskPattern') as string}
                  <Tooltip title={t('presetJobs.form.helperText.maskPatternExamples') as string}>
                    <span style={{ marginLeft: 8, cursor: 'help' }}>i</span>
                  </Tooltip>
                </span>
              }
            />
          </Grid>
        )}

        {/* Increment Mode - only show for mask-based attack modes */}
        {showMaskInput && (
          <>
            <Grid item xs={12}>
              <FormControl fullWidth margin="normal">
                <InputLabel id="increment-mode-label">{t('presetJobs.form.fields.incrementMode') as string}</InputLabel>
                <Select
                  labelId="increment-mode-label"
                  name="increment_mode"
                  value={formData.increment_mode}
                  onChange={(e) => setFormData({...formData, increment_mode: e.target.value as string})}
                  label={t('presetJobs.form.fields.incrementMode') as string}
                >
                  <MenuItem value="off">{t('presetJobs.form.incrementModes.off') as string}</MenuItem>
                  <MenuItem value="increment">{t('presetJobs.form.incrementModes.increment') as string}</MenuItem>
                  <MenuItem value="increment_inverse">{t('presetJobs.form.incrementModes.incrementInverse') as string}</MenuItem>
                </Select>
                <FormHelperText>
                  {t('presetJobs.form.helperText.incrementMode') as string}
                </FormHelperText>
              </FormControl>
            </Grid>

            {formData.increment_mode !== 'off' && (
              <Grid item xs={12}>
                <Grid container spacing={2}>
                  <Grid item xs={6}>
                    <TextField
                      name="increment_min"
                      label={t('presetJobs.form.fields.incrementMin') as string}
                      type="number"
                      value={formData.increment_min ?? ''}
                      onChange={(e) => setFormData(prev => ({
                        ...prev,
                        increment_min: e.target.value ? parseInt(e.target.value, 10) : undefined
                      }))}
                      fullWidth
                      margin="normal"
                      inputProps={{ min: 1 }}
                      helperText={t('presetJobs.form.helperText.incrementMin') as string}
                    />
                  </Grid>
                  <Grid item xs={6}>
                    <TextField
                      name="increment_max"
                      label={t('presetJobs.form.fields.incrementMax') as string}
                      type="number"
                      value={formData.increment_max ?? ''}
                      onChange={(e) => setFormData(prev => ({
                        ...prev,
                        increment_max: e.target.value ? parseInt(e.target.value, 10) : undefined
                      }))}
                      fullWidth
                      margin="normal"
                      inputProps={{ min: 1 }}
                      helperText={t('presetJobs.form.helperText.incrementMax') as string}
                    />
                  </Grid>
                </Grid>
              </Grid>
            )}
          </>
        )}

        {/* Wordlists - Special handling for Combination mode */}
        {formData.attack_mode === AttackMode.Combination ? (
          <Grid item xs={12}>
            <Typography variant="subtitle2" gutterBottom>
              {t('presetJobs.form.wordlistSelectionCombination') as string}
            </Typography>
            <Grid container spacing={2}>
              <Grid item xs={12} sm={6}>
                <FormControl fullWidth margin="normal" required>
                  <InputLabel id="first-wordlist-label" shrink>{t('presetJobs.form.fields.firstWordlist') as string}</InputLabel>
                  <Select
                    labelId="first-wordlist-label"
                    value={firstWordlist}
                    onChange={(e) => handleSelectChange(e, 'firstWordlist')}
                    label={t('presetJobs.form.fields.firstWordlist') as string}
                    displayEmpty
                  >
                    <MenuItem value="" disabled>
                      <em>{t('presetJobs.form.selectFirstWordlist') as string}</em>
                    </MenuItem>
                    {wordlists.map((wordlist) => (
                      <MenuItem key={`first-${wordlist.id}`} value={wordlist.id}>
                        {wordlist.name}
                      </MenuItem>
                    ))}
                  </Select>
                  <FormHelperText>{t('presetJobs.form.helperText.firstWordlist') as string}</FormHelperText>
                </FormControl>
              </Grid>
              <Grid item xs={12} sm={6}>
                <FormControl fullWidth margin="normal" required>
                  <InputLabel id="second-wordlist-label" shrink>{t('presetJobs.form.fields.secondWordlist') as string}</InputLabel>
                  <Select
                    labelId="second-wordlist-label"
                    value={secondWordlist}
                    onChange={(e) => handleSelectChange(e, 'secondWordlist')}
                    label={t('presetJobs.form.fields.secondWordlist') as string}
                    displayEmpty
                  >
                    <MenuItem value="" disabled>
                      <em>{t('presetJobs.form.selectSecondWordlist') as string}</em>
                    </MenuItem>
                    {wordlists.map((wordlist) => (
                      <MenuItem key={`second-${wordlist.id}`} value={wordlist.id}>
                        {wordlist.name}
                      </MenuItem>
                    ))}
                  </Select>
                  <FormHelperText>{t('presetJobs.form.helperText.secondWordlist') as string}</FormHelperText>
                </FormControl>
              </Grid>
            </Grid>
          </Grid>
        ) : (
          /* Regular wordlist selection for other attack modes */
          <Grid item xs={12}>
            <FormControl
              fullWidth
              margin="normal"
              required={!isWordlistsDisabled}
              error={!isWordlistsDisabled && formData.wordlist_ids.length !== getMaxWordlists()}
              disabled={isWordlistsDisabled}
            >
              <InputLabel id="wordlist-label">{t('presetJobs.form.fields.wordlists') as string}</InputLabel>
              <Select
                labelId="wordlist-label"
                multiple
                value={formData.wordlist_ids}
                onChange={(e) => handleMultiSelectChange(e as SelectChangeEvent<number[]>, 'wordlist_ids')}
                input={<OutlinedInput label={t('presetJobs.form.fields.wordlists') as string} />}
                renderValue={(selected) => (
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                    {(selected as number[]).map((id) => {
                      const wordlist = wordlists.find(w => w.id === id);
                      return (
                        <Chip key={id} label={wordlist?.name || id} />
                      );
                    })}
                  </Box>
                )}
                MenuProps={MenuProps}
              >
                {wordlists.map((wordlist) => (
                  <MenuItem
                    key={wordlist.id}
                    value={wordlist.id}
                    disabled={
                      formData.wordlist_ids.length >= getMaxWordlists() &&
                      !formData.wordlist_ids.includes(wordlist.id)
                    }
                  >
                    {wordlist.name}
                  </MenuItem>
                ))}
              </Select>
              <FormHelperText>
                {isWordlistsDisabled ?
                  t('presetJobs.form.helperText.wordlistsNotUsed') as string :
                  t('presetJobs.form.helperText.selectWordlists', { count: getMaxWordlists() }) as string
                }
              </FormHelperText>
            </FormControl>
          </Grid>
        )}

        {/* Rules - Single select (only one rule file supported) */}
        <Grid item xs={12}>
          <FormControl
            fullWidth
            margin="normal"
            disabled={isRulesDisabled}
          >
            <InputLabel id="rules-label">{t('presetJobs.form.fields.ruleOptional') as string}</InputLabel>
            <Select
              labelId="rules-label"
              value={isRulesDisabled || formData.rule_ids.length === 0 ? '' : formData.rule_ids[0]}
              onChange={(e) => {
                const value = e.target.value;
                setFormData(prev => ({
                  ...prev,
                  rule_ids: value ? [value as number] : []
                }));
              }}
              label={t('presetJobs.form.fields.ruleOptional') as string}
            >
              <MenuItem value="">
                <em>{t('common.none') as string}</em>
              </MenuItem>
              {rules.map((rule) => (
                <MenuItem key={rule.id} value={rule.id}>
                  {rule.name}
                </MenuItem>
              ))}
            </Select>
            <FormHelperText>
              {isRulesDisabled ?
                t('presetJobs.form.helperText.rulesNotUsed') as string :
                t('presetJobs.form.helperText.selectRule') as string
              }
            </FormHelperText>
          </FormControl>
        </Grid>

        <Grid item xs={12} sm={6}>
          <TextField
            name="chunk_size_seconds"
            label={t('presetJobs.form.fields.chunkSize') as string}
            type="number"
            value={formData.chunk_size_seconds}
            onChange={handleChange}
            onBlur={handleBlur}
            fullWidth
            margin="normal"
            inputProps={{ min: 60 }}
            helperText={t('presetJobs.form.helperText.chunkSize') as string}
          />
        </Grid>

        <Grid item xs={12} sm={6}>
          <TextField
            name="max_agents"
            label={t('presetJobs.form.fields.maxAgents') as string}
            type="number"
            value={formData.max_agents || 0}
            onChange={handleChange}
            fullWidth
            margin="normal"
            inputProps={{ min: 0 }}
            helperText={t('presetJobs.form.helperText.maxAgents') as string}
          />
        </Grid>

        {/* Checkboxes */}
        <Grid item xs={12}>
          <FormControlLabel
            control={
              <Checkbox
                name="allow_high_priority_override"
                checked={formData.allow_high_priority_override || false}
                onChange={handleChange}
              />
            }
            label={t('presetJobs.form.fields.allowHighPriorityOverride') as string}
          />
          <FormHelperText>
            {t('presetJobs.form.helperText.allowHighPriorityOverride') as string}
          </FormHelperText>
        </Grid>

        {/* Submit Button */}
        <Grid item xs={12}>
          <Button
            type="submit"
            variant="contained"
            color="primary"
            disabled={submitting}
            sx={{ mt: 2 }}
          >
            {submitting ? <CircularProgress size={24} /> : (isEditing ? t('presetJobs.form.updateJob') as string : t('presetJobs.form.createJob') as string)}
          </Button>

          <Button
            variant="outlined"
            onClick={() => navigate('/admin/preset-jobs')}
            sx={{ mt: 2, ml: 2 }}
            disabled={submitting}
          >
            {t('common.cancel') as string}
          </Button>
        </Grid>
      </Grid>
      </Box>
    </Box>
  );
};

export default PresetJobFormPage; 