import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Box,
  Typography,
  Paper,
  Button,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Chip,
  CircularProgress,
  Alert,
  Skeleton,
  TextField,
  IconButton,
  Link,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions
} from '@mui/material';
import {
  ArrowBack,
  Edit as EditIcon,
  Save as SaveIcon,
  Cancel as CancelIcon,
  Refresh as RefreshIcon,
  Replay as ReplayIcon,
  CheckCircle as CheckCircleIcon
} from '@mui/icons-material';
import { getJobDetails, getJobLayers, api } from '../../services/api';
import { JobDetailsResponse, JobTask, JobIncrementLayerWithStats } from '../../types/jobs';
import JobProgressBar from '../../components/JobProgressBar';
import { useSnackbar } from 'notistack';
import { getMaxPriorityForUsers } from '../../services/systemSettings';

const JobDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { enqueueSnackbar } = useSnackbar();
  
  const [jobData, setJobData] = useState<JobDetailsResponse | null>(null);
  const [layers, setLayers] = useState<JobIncrementLayerWithStats[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true);
  const [maxPriority, setMaxPriority] = useState<number>(1000); // Default to 1000
  
  // Edit states
  const [editingPriority, setEditingPriority] = useState(false);
  const [editingMaxAgents, setEditingMaxAgents] = useState(false);
  const [editingChunkSize, setEditingChunkSize] = useState(false);
  const [tempPriority, setTempPriority] = useState<string>('');
  const [tempMaxAgents, setTempMaxAgents] = useState<string>('');
  const [tempChunkSize, setTempChunkSize] = useState<string>('');
  const [saving, setSaving] = useState(false);

  // Force complete dialog state
  const [forceCompleteDialogOpen, setForceCompleteDialogOpen] = useState(false);
  const [forceCompleting, setForceCompleting] = useState(false);

  // Completed tasks pagination state
  const [completedTasksPage, setCompletedTasksPage] = useState(0);
  const [completedTasksPageSize, setCompletedTasksPageSize] = useState(25);
  
  // Refs to track current state for polling
  const pollingIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const currentStatusRef = useRef<string>('');
  const isEditingRef = useRef<boolean>(false);

  // Update editing ref when editing state changes
  useEffect(() => {
    isEditingRef.current = editingPriority || editingMaxAgents || editingChunkSize;
  }, [editingPriority, editingMaxAgents, editingChunkSize]);
  
  // Update status ref when job data changes
  useEffect(() => {
    if (jobData) {
      currentStatusRef.current = jobData.status;
    }
  }, [jobData?.status]);
  
  // Fetch job details
  const fetchJobDetails = useCallback(async () => {
    if (!id) return;

    // Don't fetch if user is editing
    if (isEditingRef.current) {
      return;
    }

    try {
      const data = await getJobDetails(id);
      console.log('[JobDetails] Job data received:', {
        id: data.id,
        increment_mode: data.increment_mode,
        has_increment_mode: !!(data.increment_mode && data.increment_mode !== 'off')
      });
      setJobData(data);

      // Fetch layers if this is an increment mode job
      if (data.increment_mode && data.increment_mode !== 'off') {
        console.log('[JobDetails] Fetching layers for increment mode job');
        try {
          const layersData = await getJobLayers(id);
          console.log('[JobDetails] Layers received:', layersData);
          // Handle null/undefined response - always ensure layers is an array
          setLayers(layersData || []);
        } catch (layerErr) {
          console.error('[JobDetails] Failed to fetch increment layers:', layerErr);
          // Don't fail the whole page if layers fail to load
          setLayers([]);
        }
      } else {
        console.log('[JobDetails] Not an increment job, skipping layers');
        setLayers([]);
      }

      setError(null);
    } catch (err) {
      console.error('Failed to fetch job details:', err);
      setError('Failed to load job details');
    } finally {
      setLoading(false);
    }
  }, [id]);

  // Initial fetch
  useEffect(() => {
    fetchJobDetails();
    // Fetch max priority setting
    getMaxPriorityForUsers()
      .then(config => {
        setMaxPriority(config.max_priority);
      })
      .catch(err => {
        console.error('Failed to fetch max priority:', err);
        // Keep default of 1000 if fetch fails
      });
  }, [fetchJobDetails]);
  
  // Setup and manage polling
  useEffect(() => {
    // Clear any existing interval
    if (pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current);
      pollingIntervalRef.current = null;
    }
    
    // Determine if we should poll
    const shouldPoll = jobData && 
                      ['pending', 'running', 'paused'].includes(jobData.status) &&
                      autoRefreshEnabled &&
                      !isEditingRef.current;
    
    if (shouldPoll) {
      // Set up polling interval
      const interval = setInterval(() => {
        // Check conditions again inside the interval
        const activeStatuses = ['pending', 'running', 'paused'];
        if (activeStatuses.includes(currentStatusRef.current) && 
            !isEditingRef.current &&
            autoRefreshEnabled) {
          fetchJobDetails();
        }
      }, 5000);
      
      pollingIntervalRef.current = interval;
    }
    
    // Cleanup on unmount or when dependencies change
    return () => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current);
        pollingIntervalRef.current = null;
      }
    };
  }, [jobData?.status, autoRefreshEnabled, fetchJobDetails]);

  // Handle priority edit
  const handleEditPriority = () => {
    setTempPriority(String(jobData?.priority || 0));
    setEditingPriority(true);
    setAutoRefreshEnabled(false); // Pause auto-refresh during edit
  };

  const handleSavePriority = async () => {
    if (!id) return;

    // Validate priority before saving
    const priorityValue = parseInt(tempPriority) || 0;
    if (priorityValue < 0 || priorityValue > maxPriority) {
      enqueueSnackbar(`Priority must be between 0 and ${maxPriority}`, { variant: 'error' });
      return;
    }

    setSaving(true);
    try {
      await api.patch(`/api/jobs/${id}`, { priority: priorityValue });
      await fetchJobDetails();
      setEditingPriority(false);
      setAutoRefreshEnabled(true); // Resume auto-refresh after save
    } catch (err) {
      console.error('Failed to update priority:', err);
      setError('Failed to update priority');
    } finally {
      setSaving(false);
    }
  };
  
  const handleCancelPriority = () => {
    setEditingPriority(false);
    setAutoRefreshEnabled(true); // Resume auto-refresh after cancel
  };

  // Handle max agents edit
  const handleEditMaxAgents = () => {
    setTempMaxAgents(String(jobData?.max_agents || 0));
    setEditingMaxAgents(true);
    setAutoRefreshEnabled(false); // Pause auto-refresh during edit
  };

  const handleSaveMaxAgents = async () => {
    if (!id) return;

    // Validate max agents before saving (0 = unlimited)
    const maxAgentsValue = parseInt(tempMaxAgents) || 0;
    if (maxAgentsValue < 0) {
      enqueueSnackbar('Max agents must be 0 (unlimited) or positive', { variant: 'error' });
      return;
    }

    setSaving(true);
    try {
      await api.patch(`/api/jobs/${id}`, { max_agents: maxAgentsValue });
      await fetchJobDetails();
      setEditingMaxAgents(false);
      setAutoRefreshEnabled(true); // Resume auto-refresh after save
    } catch (err) {
      console.error('Failed to update max agents:', err);
      setError('Failed to update max agents');
    } finally {
      setSaving(false);
    }
  };
  
  const handleCancelMaxAgents = () => {
    setEditingMaxAgents(false);
    setAutoRefreshEnabled(true); // Resume auto-refresh after cancel
  };

  // Handle chunk size edit
  const handleEditChunkSize = () => {
    setTempChunkSize(String(jobData?.chunk_size_seconds || 1200));
    setEditingChunkSize(true);
    setAutoRefreshEnabled(false); // Pause auto-refresh during edit
  };

  const handleSaveChunkSize = async () => {
    if (!id) return;

    // Validate chunk size before saving
    const chunkSizeValue = parseInt(tempChunkSize) || 0;
    if (chunkSizeValue < 5) {
      enqueueSnackbar('Chunk size must be at least 5 seconds', { variant: 'error' });
      return;
    }
    if (chunkSizeValue > 86400) {
      enqueueSnackbar('Chunk size cannot exceed 24 hours (86400 seconds)', { variant: 'error' });
      return;
    }

    setSaving(true);
    try {
      const response = await api.patch(`/api/jobs/${id}`, { chunk_size_seconds: chunkSizeValue });

      // Show success notification with specific message
      enqueueSnackbar(response.data?.message || 'Chunk size updated successfully. Changes will take effect on next task creation.', {
        variant: 'success',
        autoHideDuration: 5000,
      });

      await fetchJobDetails();
      setEditingChunkSize(false);
      setAutoRefreshEnabled(true); // Resume auto-refresh after save
    } catch (err: any) {
      console.error('Failed to update chunk size:', err);

      // Parse error message from response if available
      let errorMessage = 'Failed to update chunk size';
      if (err.response?.data) {
        errorMessage = typeof err.response.data === 'string' ? err.response.data : err.response.data.message || errorMessage;
      } else if (err.message) {
        errorMessage = err.message;
      }

      enqueueSnackbar(errorMessage, {
        variant: 'error',
        autoHideDuration: 5000,
      });
    } finally {
      setSaving(false);
    }
  };

  const handleCancelChunkSize = () => {
    setEditingChunkSize(false);
    setAutoRefreshEnabled(true); // Resume auto-refresh after cancel
  };

  // Handle retry task
  const handleRetryTask = async (taskId: string) => {
    if (!id) return;

    try {
      await api.post(`/api/jobs/${id}/tasks/${taskId}/retry`);
      await fetchJobDetails();
    } catch (err) {
      console.error('Failed to retry task:', err);
      setError('Failed to retry task');
    }
  };

  // Handle force complete job
  const handleForceComplete = async () => {
    if (!id) return;

    setForceCompleting(true);
    try {
      await api.post(`/api/jobs/${id}/force-complete`);
      await fetchJobDetails();
      setForceCompleteDialogOpen(false);
      enqueueSnackbar('Job force completed successfully', { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to force complete job:', err);
      const errorMessage = err.response?.data?.message || 'Failed to force complete job';
      enqueueSnackbar(errorMessage, { variant: 'error' });
    } finally {
      setForceCompleting(false);
    }
  };

  // Format helpers
  const formatDate = (dateString?: string) => {
    if (!dateString) return 'N/A';
    return new Date(dateString).toLocaleString();
  };

  const formatKeyspace = (value?: number): string => {
    if (!value) return 'N/A';
    if (value >= 1e12) return `${(value / 1e12).toFixed(2)}T`;
    if (value >= 1e9) return `${(value / 1e9).toFixed(2)}B`;
    if (value >= 1e6) return `${(value / 1e6).toFixed(2)}M`;
    if (value >= 1e3) return `${(value / 1e3).toFixed(2)}K`;
    return value.toString();
  };

  const formatSpeed = (speed?: number): string => {
    if (!speed) return 'N/A';
    if (speed >= 1e12) return `${(speed / 1e12).toFixed(2)} TH/s`;
    if (speed >= 1e9) return `${(speed / 1e9).toFixed(2)} GH/s`;
    if (speed >= 1e6) return `${(speed / 1e6).toFixed(2)} MH/s`;
    if (speed >= 1e3) return `${(speed / 1e3).toFixed(2)} KH/s`;
    return `${speed} H/s`;
  };

  const formatChunkSize = (seconds?: number): string => {
    if (seconds === undefined || seconds === null) return 'N/A';
    if (seconds === 0) return 'Not set (using default)';

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    if (hours > 0 && minutes > 0) {
      return `${hours} hour${hours > 1 ? 's' : ''} ${minutes} minute${minutes !== 1 ? 's' : ''}`;
    } else if (hours > 0) {
      return `${hours} hour${hours > 1 ? 's' : ''}`;
    } else if (minutes > 0) {
      return `${minutes} minute${minutes !== 1 ? 's' : ''}`;
    } else {
      return `${secs} second${secs !== 1 ? 's' : ''}`;
    }
  };

  const formatDuration = (seconds: number): string => {
    if (!isFinite(seconds) || seconds <= 0) {
      return 'Cannot estimate - no tasks currently running';
    }

    const years = Math.floor(seconds / (365 * 24 * 3600));
    const months = Math.floor((seconds % (365 * 24 * 3600)) / (30 * 24 * 3600));
    const days = Math.floor((seconds % (30 * 24 * 3600)) / (24 * 3600));
    const hours = Math.floor((seconds % (24 * 3600)) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    const parts = [];

    if (years > 0) {
      parts.push(`${years} year${years !== 1 ? 's' : ''}`);
    }
    if (months > 0) {
      parts.push(`${months} month${months !== 1 ? 's' : ''}`);
    }
    if (days > 0 && years === 0) { // Only show days if less than a year
      parts.push(`${days} day${days !== 1 ? 's' : ''}`);
    }
    if (hours > 0 && years === 0 && months === 0) { // Only show hours if less than a month
      parts.push(`${hours} hour${hours !== 1 ? 's' : ''}`);
    }
    if (minutes > 0 && years === 0 && months === 0 && days === 0) { // Only show minutes if less than a day
      parts.push(`${minutes} minute${minutes !== 1 ? 's' : ''}`);
    }

    // Show at most 2 units for readability
    const displayParts = parts.slice(0, 2);

    if (displayParts.length === 0) {
      return 'Less than 1 minute';
    }

    return `~${displayParts.join(' ')}`;
  };

  const calculateEstimatedCompletion = (): { timeRemaining: string; estimatedDate: string } => {
    // Check if job is completed
    if (jobData?.status === 'completed') {
      return {
        timeRemaining: 'Job completed',
        estimatedDate: 'Job completed'
      };
    }

    // MODE 2: Job is in 'processing' state - use crack-count-based calculation
    if (jobData?.status === 'processing') {
      return calculateProcessingTimeRemaining();
    }

    // MODE 1: Job is running - use keyspace-based calculation
    // Calculate remaining keyspace
    const effectiveKeyspace = jobData?.effective_keyspace || jobData?.total_keyspace || 0;
    const processedKeyspace = jobData?.processed_keyspace || 0;
    const remainingKeyspace = effectiveKeyspace - processedKeyspace;

    // If nothing left to process
    if (remainingKeyspace <= 0) {
      return {
        timeRemaining: 'Job completed',
        estimatedDate: 'Job completed'
      };
    }

    // Calculate total speed from active tasks
    let totalSpeed = 0;
    const activeTasks = (jobData?.tasks || []).filter(task =>
      ['running'].includes(task.status)
    );

    for (const task of activeTasks) {
      if (task.benchmark_speed && task.benchmark_speed > 0) {
        totalSpeed += task.benchmark_speed;
      }
    }

    // If no active tasks or zero speed
    if (totalSpeed === 0) {
      return {
        timeRemaining: 'Cannot estimate - no tasks currently running',
        estimatedDate: 'Cannot estimate - no tasks currently running'
      };
    }

    // Calculate seconds remaining
    const secondsRemaining = remainingKeyspace / totalSpeed;

    // Format duration
    const timeRemaining = formatDuration(secondsRemaining);

    // Calculate estimated completion date
    const now = new Date();
    const estimatedDate = new Date(now.getTime() + secondsRemaining * 1000);

    return {
      timeRemaining,
      estimatedDate: estimatedDate.toLocaleString()
    };
  };

  // Calculate time remaining for processing phase (crack-count-based)
  const calculateProcessingTimeRemaining = (): { timeRemaining: string; estimatedDate: string } => {
    // Sum expected and received cracks from all processing tasks
    const processingTasks = (jobData?.tasks || []).filter(task =>
      task.status === 'processing'
    );

    if (processingTasks.length === 0) {
      return {
        timeRemaining: 'Finishing up...',
        estimatedDate: 'Completing shortly'
      };
    }

    const totalExpected = processingTasks.reduce(
      (sum, task) => sum + (task.expected_crack_count || 0), 0
    );
    const totalReceived = processingTasks.reduce(
      (sum, task) => sum + (task.received_crack_count || 0), 0
    );

    const remaining = totalExpected - totalReceived;

    if (remaining <= 0) {
      return {
        timeRemaining: 'Finishing up...',
        estimatedDate: 'Completing shortly'
      };
    }

    // Calculate actual processing rate from elapsed time since cracking finished
    let processingRate = 500; // Default fallback (500 cracks/sec)

    // Find the earliest cracking_completed_at among processing tasks
    const crackingCompletedTimes = processingTasks
      .filter(task => task.cracking_completed_at)
      .map(task => new Date(task.cracking_completed_at!).getTime());

    if (crackingCompletedTimes.length > 0 && totalReceived > 0) {
      const earliestCrackingComplete = Math.min(...crackingCompletedTimes);
      const elapsedMs = Date.now() - earliestCrackingComplete;
      const elapsedSec = elapsedMs / 1000;

      // Only use dynamic rate if we have enough elapsed time to calculate
      if (elapsedSec > 1) {
        processingRate = totalReceived / elapsedSec;
      }
    }

    const secondsRemaining = remaining / processingRate;

    // Format duration
    const timeRemaining = formatDuration(secondsRemaining);

    // Calculate estimated completion date
    const now = new Date();
    const estimatedDate = new Date(now.getTime() + secondsRemaining * 1000);

    return {
      timeRemaining: `Processing: ${timeRemaining}`,
      estimatedDate: estimatedDate.toLocaleString()
    };
  };

  const getStatusColor = (status: string) => {
    switch (status.toLowerCase()) {
      case 'running': return 'success';
      case 'pending': return 'warning';
      case 'reconnect_pending': return 'warning';
      case 'processing': return 'info';  // Blue - hashcat done, saving to DB
      case 'processing_error': return 'warning';  // Orange - processing issue
      case 'completed': return 'info';
      case 'failed': return 'error';
      case 'cancelled': return 'default';
      case 'paused': return 'warning';
      default: return 'default';
    }
  };

  const getAttackModeName = (mode?: number): string => {
    const modes: Record<number, string> = {
      0: 'Dictionary',
      1: 'Combination',
      3: 'Brute-force',
      6: 'Hybrid Wordlist + Mask',
      7: 'Hybrid Mask + Wordlist',
      9: 'Association',
    };
    return mode !== undefined ? modes[mode] || `Mode ${mode}` : 'N/A';
  };

  // Render attack configuration rows based on attack mode
  const renderAttackConfigRows = () => {
    if (!jobData) return null;

    const rows: JSX.Element[] = [];
    const attackMode = jobData.attack_mode;

    switch (attackMode) {
      case 0: // Dictionary/Straight
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="wordlists">
              <TableCell sx={{ fontWeight: 'bold' }}>Wordlist(s)</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            <TableRow key="rules">
              <TableCell sx={{ fontWeight: 'bold' }}>Rules</TableCell>
              <TableCell>{jobData.rule_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        // Splitting mode for dictionary attacks
        rows.push(
          <TableRow key="splitting-mode">
            <TableCell sx={{ fontWeight: 'bold' }}>Splitting Mode</TableCell>
            <TableCell>
              <Chip
                label={jobData.uses_rule_splitting ? "Rule Splitting" : "Keyspace"}
                size="small"
                color={jobData.uses_rule_splitting ? "primary" : "default"}
                variant="outlined"
              />
            </TableCell>
          </TableRow>
        );
        break;

      case 1: // Combination
        if (jobData.wordlist_names && jobData.wordlist_names.length >= 2) {
          rows.push(
            <TableRow key="first-wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>First Wordlist</TableCell>
              <TableCell>{jobData.wordlist_names[0]}</TableCell>
            </TableRow>
          );
          rows.push(
            <TableRow key="second-wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>Second Wordlist</TableCell>
              <TableCell>{jobData.wordlist_names[1]}</TableCell>
            </TableRow>
          );
        } else if (jobData.wordlist_names && jobData.wordlist_names.length === 1) {
          rows.push(
            <TableRow key="wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>Wordlist</TableCell>
              <TableCell>{jobData.wordlist_names[0]}</TableCell>
            </TableRow>
          );
        }
        // Splitting mode for combination attacks
        rows.push(
          <TableRow key="splitting-mode">
            <TableCell sx={{ fontWeight: 'bold' }}>Splitting Mode</TableCell>
            <TableCell>
              <Chip
                label={jobData.uses_rule_splitting ? "Rule Splitting" : "Keyspace"}
                size="small"
                color={jobData.uses_rule_splitting ? "primary" : "default"}
                variant="outlined"
              />
            </TableCell>
          </TableRow>
        );
        break;

      case 3: // Brute-force/Mask
        if (jobData.mask) {
          rows.push(
            <TableRow key="mask">
              <TableCell sx={{ fontWeight: 'bold' }}>Mask</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        if (jobData.increment_mode && jobData.increment_mode !== 'off') {
          rows.push(
            <TableRow key="increment-mode">
              <TableCell sx={{ fontWeight: 'bold' }}>Increment Mode</TableCell>
              <TableCell>
                <Chip
                  label={jobData.increment_mode === 'increment' ? 'Increment' : 'Increment Inverse'}
                  size="small"
                  color="info"
                  variant="outlined"
                />
              </TableCell>
            </TableRow>
          );
          rows.push(
            <TableRow key="increment-range">
              <TableCell sx={{ fontWeight: 'bold' }}>Increment Range</TableCell>
              <TableCell>
                {jobData.increment_min ?? 1} - {jobData.increment_max ?? (jobData.mask?.length || 'N/A')}
              </TableCell>
            </TableRow>
          );
        }
        break;

      case 6: // Hybrid Wordlist + Mask
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="wordlists">
              <TableCell sx={{ fontWeight: 'bold' }}>Wordlist(s)</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.mask) {
          rows.push(
            <TableRow key="mask">
              <TableCell sx={{ fontWeight: 'bold' }}>Mask (suffix)</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        break;

      case 7: // Hybrid Mask + Wordlist
        if (jobData.mask) {
          rows.push(
            <TableRow key="mask">
              <TableCell sx={{ fontWeight: 'bold' }}>Mask (prefix)</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="wordlists">
              <TableCell sx={{ fontWeight: 'bold' }}>Wordlist(s)</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        break;

      case 9: // Association
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="association-wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>Association Hints</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            <TableRow key="rules">
              <TableCell sx={{ fontWeight: 'bold' }}>Rules</TableCell>
              <TableCell>{jobData.rule_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        break;
    }

    return rows;
  };

  if (loading) {
    return (
      <Box sx={{ p: 3 }}>
        <Skeleton variant="rectangular" height={60} sx={{ mb: 3 }} />
        <Skeleton variant="rectangular" height={400} sx={{ mb: 3 }} />
        <Skeleton variant="rectangular" height={200} />
      </Box>
    );
  }

  if (error && !jobData) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" sx={{ mb: 3 }}>
          {error}
        </Alert>
        <Button startIcon={<ArrowBack />} onClick={() => navigate(-1)}>
          Back
        </Button>
      </Box>
    );
  }

  if (!jobData) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">Job not found</Alert>
        <Button startIcon={<ArrowBack />} onClick={() => navigate(-1)} sx={{ mt: 2 }}>
          Back
        </Button>
      </Box>
    );
  }

  // Filter tasks client-side from the complete list
  const allTasks = jobData.tasks || [];

  // Get active tasks (running, assigned, pending, reconnect_pending)
  const activeTasks = allTasks.filter(task =>
    ['running', 'assigned', 'pending', 'reconnect_pending'].includes(task.status)
  );

  // Get failed tasks (including processing_error)
  const failedTasks = allTasks.filter(task =>
    task.status === 'failed' || task.status === 'processing_error'
  );

  // Get completed tasks - includes 'processing' since hashcat work is done, just waiting for DB persistence
  const completedTasks = allTasks.filter(task =>
    task.status === 'completed' || task.status === 'processing'
  );

  // Paginate completed tasks
  const paginatedCompletedTasks = completedTasks.slice(
    completedTasksPage * completedTasksPageSize,
    (completedTasksPage + 1) * completedTasksPageSize
  );

  const totalKeyspace = jobData.effective_keyspace || jobData.total_keyspace || 0;

  // Calculate estimated completion once for efficiency
  const estimatedCompletion = jobData ? calculateEstimatedCompletion() : { timeRemaining: '', estimatedDate: '' };

  return (
    <Box sx={{ p: 3 }}>
      {/* Header */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button
            startIcon={<ArrowBack />}
            onClick={() => navigate(-1)}
          >
            Back
          </Button>
          <Typography variant="h4" component="h1">
            Job Details
          </Typography>
          <Chip 
            label={jobData.status} 
            color={getStatusColor(jobData.status) as any}
            size="small"
          />
          {['pending', 'running', 'paused'].includes(jobData.status) && (
            <Chip
              label={autoRefreshEnabled && !isEditingRef.current ? 'Auto-refresh: ON' : 'Auto-refresh: PAUSED'}
              color={autoRefreshEnabled && !isEditingRef.current ? 'success' : 'warning'}
              size="small"
              variant="outlined"
            />
          )}
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          {(jobData.status === 'running' || jobData.status === 'pending') && (
            <Button
              variant="contained"
              color="warning"
              startIcon={<CheckCircleIcon />}
              onClick={() => setForceCompleteDialogOpen(true)}
              size="small"
            >
              Force Complete
            </Button>
          )}
          <IconButton onClick={fetchJobDetails} disabled={loading} title="Refresh now">
            <RefreshIcon />
          </IconButton>
        </Box>
      </Box>

      {/* Error Alert */}
      {error && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* Job Information Table */}
      <Paper sx={{ mb: 3 }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h6">Job Information</Typography>
        </Box>
        <TableContainer>
          <Table>
            <TableBody>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold', width: '30%' }}>ID</TableCell>
                <TableCell>{jobData.id}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Name</TableCell>
                <TableCell>{jobData.name}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Status</TableCell>
                <TableCell>
                  <Chip 
                    label={jobData.status} 
                    color={getStatusColor(jobData.status) as any}
                    size="small"
                  />
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Priority</TableCell>
                <TableCell>
                  {editingPriority ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <TextField
                        type="number"
                        value={tempPriority}
                        onChange={(e) => setTempPriority(e.target.value)}
                        size="small"
                        sx={{ width: 100 }}
                        disabled={saving}
                        helperText={`0-${maxPriority}`}
                      />
                      <IconButton onClick={handleSavePriority} disabled={saving} size="small" title="Save">
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelPriority} disabled={saving} size="small" title="Cancel">
                        <CancelIcon />
                      </IconButton>
                    </Box>
                  ) : (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {jobData.priority}
                      <IconButton onClick={handleEditPriority} size="small">
                        <EditIcon />
                      </IconButton>
                    </Box>
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Max Agents</TableCell>
                <TableCell>
                  {editingMaxAgents ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <TextField
                        type="number"
                        value={tempMaxAgents}
                        onChange={(e) => setTempMaxAgents(e.target.value)}
                        size="small"
                        sx={{ width: 100 }}
                        disabled={saving}
                        helperText="0=unlimited"
                      />
                      <IconButton onClick={handleSaveMaxAgents} disabled={saving} size="small" title="Save">
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelMaxAgents} disabled={saving} size="small" title="Cancel">
                        <CancelIcon />
                      </IconButton>
                    </Box>
                  ) : (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {jobData.max_agents}
                      <IconButton onClick={handleEditMaxAgents} size="small">
                        <EditIcon />
                      </IconButton>
                    </Box>
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Chunk Size</TableCell>
                <TableCell>
                  {editingChunkSize ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <TextField
                        type="number"
                        value={tempChunkSize}
                        onChange={(e) => setTempChunkSize(e.target.value)}
                        size="small"
                        sx={{ width: 120 }}
                        disabled={saving}
                        helperText="Seconds (5-86400)"
                      />
                      <IconButton onClick={handleSaveChunkSize} disabled={saving} size="small" title="Save">
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelChunkSize} disabled={saving} size="small" title="Cancel">
                        <CancelIcon />
                      </IconButton>
                    </Box>
                  ) : (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {formatChunkSize(jobData.chunk_size_seconds)}
                      <IconButton onClick={handleEditChunkSize} size="small">
                        <EditIcon />
                      </IconButton>
                    </Box>
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Hashlist</TableCell>
                <TableCell>
                  <Link
                    component="button"
                    onClick={() => navigate(`/hashlists/${jobData.hashlist_id}`)}
                    sx={{ cursor: 'pointer' }}
                  >
                    {jobData.hashlist_name}
                  </Link>
                  {' '}(ID: {jobData.hashlist_id})
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Hash Type</TableCell>
                <TableCell>{jobData.hash_type || 'N/A'}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Attack Mode</TableCell>
                <TableCell>{getAttackModeName(jobData.attack_mode)}</TableCell>
              </TableRow>
              {/* Attack configuration rows based on attack mode */}
              {renderAttackConfigRows()}
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Keyspace</TableCell>
                <TableCell>{formatKeyspace(jobData.base_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Effective Keyspace</TableCell>
                <TableCell>
                  {formatKeyspace(jobData.effective_keyspace)}
                  {jobData.multiplication_factor && jobData.multiplication_factor > 1 && (
                    <Chip 
                      label={`×${jobData.multiplication_factor}${jobData.uses_rule_splitting ? ' (rules)' : ''}`} 
                      size="small" 
                      color="error" 
                      variant="filled"
                      sx={{ ml: 1 }}
                    />
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Processed Keyspace</TableCell>
                <TableCell>{formatKeyspace(jobData.processed_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Dispatched Keyspace</TableCell>
                <TableCell>{formatKeyspace(jobData.dispatched_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Progress</TableCell>
                <TableCell>{jobData.overall_progress_percent?.toFixed(2) || 0}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Cracks Found</TableCell>
                <TableCell>
                  {jobData.cracked_count > 0 ? (
                    <Link
                      component="button"
                      variant="body2"
                      onClick={() => navigate(`/pot/job/${jobData.id}`)}
                      sx={{ fontWeight: 'medium' }}
                    >
                      {jobData.cracked_count}
                    </Link>
                  ) : (
                    jobData.cracked_count
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Created At</TableCell>
                <TableCell>{formatDate(jobData.created_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Started At</TableCell>
                <TableCell>{formatDate(jobData.started_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Time Remaining</TableCell>
                <TableCell>{estimatedCompletion.timeRemaining}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Estimated Completion</TableCell>
                <TableCell>{estimatedCompletion.estimatedDate}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Cracking Completed At</TableCell>
                <TableCell>{formatDate(jobData.cracking_completed_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>Completed At</TableCell>
                <TableCell>{formatDate(jobData.completed_at)}</TableCell>
              </TableRow>
              {jobData.error_message && (
                <TableRow>
                  <TableCell sx={{ fontWeight: 'bold' }}>Error</TableCell>
                  <TableCell>
                    <Alert severity="error" sx={{ py: 0.5 }}>
                      {jobData.error_message}
                    </Alert>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

      {/* Increment Layers Table */}
      {layers.length > 0 && (
        <Paper sx={{ mb: 3 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="h6">
              Increment Layers ({layers.length} total)
            </Typography>
          </Box>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>Layer</TableCell>
                  <TableCell>Mask</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell>Keyspace</TableCell>
                  <TableCell>Effective Keyspace</TableCell>
                  <TableCell>Progress</TableCell>
                  <TableCell>Tasks</TableCell>
                  <TableCell>Cracks</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {layers.map((layer) => (
                  <TableRow key={layer.id}>
                    <TableCell>{layer.layer_index}</TableCell>
                    <TableCell sx={{ fontFamily: 'monospace' }}>{layer.mask}</TableCell>
                    <TableCell>
                      <Chip
                        label={layer.status}
                        color={getStatusColor(layer.status) as any}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>{formatKeyspace(layer.base_keyspace)}</TableCell>
                    <TableCell>
                      {formatKeyspace(layer.effective_keyspace)}
                      {!layer.is_accurate_keyspace && (
                        <Chip
                          label="Estimated"
                          size="small"
                          color="warning"
                          variant="outlined"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </TableCell>
                    <TableCell>{layer.overall_progress_percent?.toFixed(2) || 0}%</TableCell>
                    <TableCell>
                      {layer.running_tasks || 0} running / {layer.total_tasks || 0} total
                      {layer.failed_tasks != null && layer.failed_tasks > 0 && (
                        <Chip
                          label={`${layer.failed_tasks} failed`}
                          size="small"
                          color="error"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      {(layer.crack_count ?? 0) > 0 ? (
                        <Link
                          component="button"
                          variant="body2"
                          onClick={() => navigate(`/pot/job/${jobData.id}`)}
                          sx={{ fontWeight: 'medium' }}
                        >
                          {layer.crack_count}
                        </Link>
                      ) : (
                        layer.crack_count ?? 0
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Paper>
      )}

      {/* Visual Progress Tracking */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" sx={{ mb: 2 }}>
          Task Progress Visualization
        </Typography>
        <JobProgressBar
          tasks={allTasks}
          totalKeyspace={totalKeyspace}
          height={50}
        />
      </Paper>

      {/* Agent Performance Table */}
      <Paper>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h6">
            Active Tasks ({activeTasks.length} running)
          </Typography>
        </Box>
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Agent ID</TableCell>
                <TableCell>Task ID</TableCell>
                <TableCell>Status</TableCell>
                <TableCell>Keyspace Range</TableCell>
                <TableCell>Progress</TableCell>
                <TableCell>Current Speed</TableCell>
                <TableCell>Cracks</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {activeTasks.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} align="center">
                    <Typography color="text.secondary" sx={{ py: 2 }}>
                      No active tasks
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                activeTasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>{task.agent_id || 'Unassigned'}</TableCell>
                    <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.75rem', padding: '6px 8px' }}>{task.id}</TableCell>
                    <TableCell>
                      <Chip 
                        label={task.status} 
                        color={getStatusColor(task.status) as any}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>
                      {formatKeyspace(task.effective_keyspace_start || task.keyspace_start)} - {formatKeyspace(task.effective_keyspace_end || task.keyspace_end)}
                    </TableCell>
                    <TableCell>{task.progress_percent?.toFixed(2) || 0}%</TableCell>
                    <TableCell>{formatSpeed(task.benchmark_speed)}</TableCell>
                    <TableCell>
                      {task.crack_count > 0 ? (
                        <Link
                          component="button"
                          variant="body2"
                          onClick={() => navigate(`/pot/job/${jobData.id}`)}
                          sx={{ fontWeight: 'medium' }}
                        >
                          {task.crack_count}
                        </Link>
                      ) : (
                        task.crack_count
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </Paper>

      {/* Failed Tasks Table */}
      {failedTasks.length > 0 && (
        <Paper sx={{ mt: 3 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="h6" color="error">
              Failed Tasks ({failedTasks.length} total)
            </Typography>
          </Box>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Agent ID</TableCell>
                  <TableCell>Task ID</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell>Retry Count</TableCell>
                  <TableCell>Error Message</TableCell>
                  <TableCell>Last Updated</TableCell>
                  <TableCell align="center">Actions</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {failedTasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>{task.agent_id || 'Unassigned'}</TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.75rem' }}>
                        {task.id}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Chip
                        label={task.status}
                        color="error"
                        size="small"
                      />
                    </TableCell>
                    <TableCell>{task.retry_count || 0}</TableCell>
                    <TableCell>
                      <Typography variant="body2" sx={{ maxWidth: 300 }}>
                        {task.error_message || 'No error message'}
                      </Typography>
                    </TableCell>
                    <TableCell>{formatDate(task.updated_at)}</TableCell>
                    <TableCell align="center">
                      <Button
                        variant="outlined"
                        size="small"
                        startIcon={<ReplayIcon />}
                        onClick={() => handleRetryTask(task.id)}
                        sx={{ textTransform: 'none' }}
                      >
                        Retry
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Paper>
      )}

      {/* Completed Tasks Table */}
      {completedTasks.length > 0 && (
        <Paper sx={{ mt: 3 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="h6">
              Completed Tasks ({completedTasks.length} total)
            </Typography>
          </Box>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Agent ID</TableCell>
                  <TableCell>Task ID</TableCell>
                  <TableCell>Completed At</TableCell>
                  <TableCell>Keyspace Range</TableCell>
                  <TableCell>Final Progress</TableCell>
                  <TableCell>Average Speed</TableCell>
                  <TableCell>Cracks Found</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {paginatedCompletedTasks.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      <Typography color="text.secondary" sx={{ py: 2 }}>
                        No completed tasks
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : (
                  paginatedCompletedTasks.map((task) => (
                    <TableRow key={task.id}>
                      <TableCell>{task.agent_id || 'Unassigned'}</TableCell>
                      <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.75rem', padding: '6px 8px' }}>{task.id}</TableCell>
                      <TableCell>{formatDate(task.completed_at)}</TableCell>
                      <TableCell>
                        {formatKeyspace(task.effective_keyspace_start || task.keyspace_start)} - {formatKeyspace(task.effective_keyspace_end || task.keyspace_end)}
                      </TableCell>
                      <TableCell>{task.progress_percent?.toFixed(2) || 100}%</TableCell>
                      <TableCell>{formatSpeed(task.average_speed || task.benchmark_speed)}</TableCell>
                      <TableCell>
                        {task.crack_count > 0 ? (
                          <Link
                            component="button"
                            variant="body2"
                            onClick={() => navigate(`/pot/job/${jobData.id}`)}
                            sx={{ fontWeight: 'medium' }}
                          >
                            {task.crack_count}
                          </Link>
                        ) : (
                          task.crack_count
                        )}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
          {completedTasks.length > completedTasksPageSize && (
            <TablePagination
              rowsPerPageOptions={[25, 50, 100, 200]}
              component="div"
              count={completedTasks.length}
              rowsPerPage={completedTasksPageSize}
              page={completedTasksPage}
              onPageChange={(event, newPage) => setCompletedTasksPage(newPage)}
              onRowsPerPageChange={(event) => {
                setCompletedTasksPageSize(parseInt(event.target.value, 10));
                setCompletedTasksPage(0);
              }}
              showFirstButton
              showLastButton
            />
          )}
        </Paper>
      )}

      {/* Force Complete Warning Dialog */}
      <Dialog
        open={forceCompleteDialogOpen}
        onClose={() => !forceCompleting && setForceCompleteDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>Force Complete Job</DialogTitle>
        <DialogContent>
          <DialogContentText>
            <strong>Warning:</strong> This will mark all incomplete tasks as completed and set the job status to completed.
          </DialogContentText>
          <DialogContentText sx={{ mt: 2 }}>
            <strong>Only use this if:</strong>
          </DialogContentText>
          <DialogContentText component="ul" sx={{ mt: 1, pl: 2 }}>
            <li>The job has actually finished processing all work</li>
            <li>The job is stuck in running/pending status</li>
            <li>You have verified the keyspace has been fully searched</li>
          </DialogContentText>
          <DialogContentText sx={{ mt: 2, fontStyle: 'italic', fontSize: '0.9em' }}>
            Note: We are working on better ways to handle keyspace calculations and job completion detection under various circumstances.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setForceCompleteDialogOpen(false)} disabled={forceCompleting}>
            Cancel
          </Button>
          <Button
            onClick={handleForceComplete}
            color="warning"
            variant="contained"
            disabled={forceCompleting}
            startIcon={forceCompleting ? <CircularProgress size={20} /> : <CheckCircleIcon />}
          >
            {forceCompleting ? 'Completing...' : 'Force Complete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default JobDetails;