import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation('jobs');
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
      setError(t('errors.loadDetailsFailed'));
    } finally {
      setLoading(false);
    }
  }, [id, t]);

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
      enqueueSnackbar(t('validation.priorityRange', { max: maxPriority }), { variant: 'error' });
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
      setError(t('errors.updatePriorityFailed'));
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
      enqueueSnackbar(t('validation.maxAgentsPositive'), { variant: 'error' });
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
      setError(t('errors.updateMaxAgentsFailed'));
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
      enqueueSnackbar(t('validation.chunkSizeMin'), { variant: 'error' });
      return;
    }
    if (chunkSizeValue > 86400) {
      enqueueSnackbar(t('validation.chunkSizeMax'), { variant: 'error' });
      return;
    }

    setSaving(true);
    try {
      const response = await api.patch(`/api/jobs/${id}`, { chunk_size_seconds: chunkSizeValue });

      // Show success notification with specific message
      enqueueSnackbar(response.data?.message || t('success.chunkSizeUpdated'), {
        variant: 'success',
        autoHideDuration: 5000,
      });

      await fetchJobDetails();
      setEditingChunkSize(false);
      setAutoRefreshEnabled(true); // Resume auto-refresh after save
    } catch (err: any) {
      console.error('Failed to update chunk size:', err);

      // Parse error message from response if available
      let errorMessage: string = t('errors.updateChunkSizeFailed') as string;
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
      setError(t('errors.retryTaskFailed'));
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
      enqueueSnackbar(t('success.forceCompleted'), { variant: 'success' });
    } catch (err: any) {
      console.error('Failed to force complete job:', err);
      const errorMessage = err.response?.data?.message || t('errors.forceCompleteFailed');
      enqueueSnackbar(errorMessage, { variant: 'error' });
    } finally {
      setForceCompleting(false);
    }
  };

  // Format helpers
  const formatDate = (dateString?: string) => {
    if (!dateString) return t('common.notAvailable');
    return new Date(dateString).toLocaleString();
  };

  const formatKeyspace = (value?: number): string => {
    if (!value) return t('common.notAvailable');
    if (value >= 1e12) return `${(value / 1e12).toFixed(2)}T`;
    if (value >= 1e9) return `${(value / 1e9).toFixed(2)}B`;
    if (value >= 1e6) return `${(value / 1e6).toFixed(2)}M`;
    if (value >= 1e3) return `${(value / 1e3).toFixed(2)}K`;
    return value.toString();
  };

  const formatSpeed = (speed?: number): string => {
    if (!speed) return t('common.notAvailable');
    if (speed >= 1e12) return `${(speed / 1e12).toFixed(2)} TH/s`;
    if (speed >= 1e9) return `${(speed / 1e9).toFixed(2)} GH/s`;
    if (speed >= 1e6) return `${(speed / 1e6).toFixed(2)} MH/s`;
    if (speed >= 1e3) return `${(speed / 1e3).toFixed(2)} KH/s`;
    return `${speed} H/s`;
  };

  const formatChunkSize = (seconds?: number): string => {
    if (seconds === undefined || seconds === null) return t('common.notAvailable');
    if (seconds === 0) return t('details.chunkSizeDefault');

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    if (hours > 0 && minutes > 0) {
      const hourLabel = hours > 1 ? t('details.timeUnits.hours') : t('details.timeUnits.hour');
      const minuteLabel = minutes !== 1 ? t('details.timeUnits.minutes') : t('details.timeUnits.minute');
      return `${hours} ${hourLabel} ${minutes} ${minuteLabel}`;
    } else if (hours > 0) {
      const hourLabel = hours > 1 ? t('details.timeUnits.hours') : t('details.timeUnits.hour');
      return `${hours} ${hourLabel}`;
    } else if (minutes > 0) {
      const minuteLabel = minutes !== 1 ? t('details.timeUnits.minutes') : t('details.timeUnits.minute');
      return `${minutes} ${minuteLabel}`;
    } else {
      const secondLabel = secs !== 1 ? t('details.timeUnits.seconds') : t('details.timeUnits.second');
      return `${secs} ${secondLabel}`;
    }
  };

  const formatDuration = (seconds: number): string => {
    if (!isFinite(seconds) || seconds <= 0) {
      return t('details.noActiveTasksEstimate');
    }

    const years = Math.floor(seconds / (365 * 24 * 3600));
    const months = Math.floor((seconds % (365 * 24 * 3600)) / (30 * 24 * 3600));
    const days = Math.floor((seconds % (30 * 24 * 3600)) / (24 * 3600));
    const hours = Math.floor((seconds % (24 * 3600)) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    const parts = [];

    if (years > 0) {
      const label = years !== 1 ? t('details.timeUnits.years') : t('details.timeUnits.year');
      parts.push(`${years} ${label}`);
    }
    if (months > 0) {
      const label = months !== 1 ? t('details.timeUnits.months') : t('details.timeUnits.month');
      parts.push(`${months} ${label}`);
    }
    if (days > 0 && years === 0) { // Only show days if less than a year
      const label = days !== 1 ? t('details.timeUnits.days') : t('details.timeUnits.day');
      parts.push(`${days} ${label}`);
    }
    if (hours > 0 && years === 0 && months === 0) { // Only show hours if less than a month
      const label = hours !== 1 ? t('details.timeUnits.hours') : t('details.timeUnits.hour');
      parts.push(`${hours} ${label}`);
    }
    if (minutes > 0 && years === 0 && months === 0 && days === 0) { // Only show minutes if less than a day
      const label = minutes !== 1 ? t('details.timeUnits.minutes') : t('details.timeUnits.minute');
      parts.push(`${minutes} ${label}`);
    }

    // Show at most 2 units for readability
    const displayParts = parts.slice(0, 2);

    if (displayParts.length === 0) {
      return t('details.lessThanOneMinute');
    }

    return `~${displayParts.join(' ')}`;
  };

  const calculateEstimatedCompletion = (): { timeRemaining: string; estimatedDate: string } => {
    // Check if job is completed
    if (jobData?.status === 'completed') {
      return {
        timeRemaining: t('details.jobCompleted'),
        estimatedDate: t('details.jobCompleted')
      };
    }

    // MODE 2: Job is in 'processing' state - use crack-count-based calculation
    if (jobData?.status === 'processing') {
      return calculateProcessingTimeRemaining();
    }

    // MODE 1: Job is running - use keyspace-based calculation
    // Calculate remaining keyspace
    const effectiveKeyspace = jobData?.effective_keyspace || 0;
    const processedKeyspace = jobData?.processed_keyspace || 0;
    const remainingKeyspace = effectiveKeyspace - processedKeyspace;

    // If nothing left to process
    if (remainingKeyspace <= 0) {
      return {
        timeRemaining: t('details.jobCompleted'),
        estimatedDate: t('details.jobCompleted')
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
        timeRemaining: t('details.noActiveTasksEstimate'),
        estimatedDate: t('details.noActiveTasksEstimate')
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
        timeRemaining: t('details.finishingUp'),
        estimatedDate: t('details.completingShortly')
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
        timeRemaining: t('details.finishingUp'),
        estimatedDate: t('details.completingShortly')
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
      timeRemaining: t('details.processingDuration', { duration: timeRemaining }),
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
    if (mode === undefined) return t('common.notAvailable');
    const modes: Record<number, string> = {
      0: t('details.attackModes.dictionary'),
      1: t('details.attackModes.combination'),
      3: t('details.attackModes.bruteforce'),
      6: t('details.attackModes.hybridWordlistMask'),
      7: t('details.attackModes.hybridMaskWordlist'),
      9: t('details.attackModes.association'),
    };
    return modes[mode] || t('details.attackModes.modeNumber', { mode });
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
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.wordlists')}</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            <TableRow key="rules">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.rules')}</TableCell>
              <TableCell>{jobData.rule_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        // Splitting mode for dictionary attacks
        rows.push(
          <TableRow key="splitting-mode">
            <TableCell sx={{ fontWeight: 'bold' }}>{t('details.splittingMode')}</TableCell>
            <TableCell>
              <Chip
                label={jobData.uses_rule_splitting ? t('details.ruleSplitting') : t('common.keyspace')}
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
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.firstWordlist')}</TableCell>
              <TableCell>{jobData.wordlist_names[0]}</TableCell>
            </TableRow>
          );
          rows.push(
            <TableRow key="second-wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.secondWordlist')}</TableCell>
              <TableCell>{jobData.wordlist_names[1]}</TableCell>
            </TableRow>
          );
        } else if (jobData.wordlist_names && jobData.wordlist_names.length === 1) {
          rows.push(
            <TableRow key="wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.wordlist')}</TableCell>
              <TableCell>{jobData.wordlist_names[0]}</TableCell>
            </TableRow>
          );
        }
        // Splitting mode for combination attacks
        rows.push(
          <TableRow key="splitting-mode">
            <TableCell sx={{ fontWeight: 'bold' }}>{t('details.splittingMode')}</TableCell>
            <TableCell>
              <Chip
                label={jobData.uses_rule_splitting ? t('details.ruleSplitting') : t('common.keyspace')}
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
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.mask')}</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        if (jobData.increment_mode && jobData.increment_mode !== 'off') {
          rows.push(
            <TableRow key="increment-mode">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.incrementMode')}</TableCell>
              <TableCell>
                <Chip
                  label={jobData.increment_mode === 'increment' ? t('details.increment') : t('details.incrementInverse')}
                  size="small"
                  color="info"
                  variant="outlined"
                />
              </TableCell>
            </TableRow>
          );
          rows.push(
            <TableRow key="increment-range">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.incrementRange')}</TableCell>
              <TableCell>
                {jobData.increment_min ?? 1} - {jobData.increment_max ?? (jobData.mask?.length || t('common.notAvailable'))}
              </TableCell>
            </TableRow>
          );
        }
        break;

      case 6: // Hybrid Wordlist + Mask
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="wordlists">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.wordlists')}</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.mask) {
          rows.push(
            <TableRow key="mask">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.maskSuffix')}</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        break;

      case 7: // Hybrid Mask + Wordlist
        if (jobData.mask) {
          rows.push(
            <TableRow key="mask">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.maskPrefix')}</TableCell>
              <TableCell sx={{ fontFamily: 'monospace' }}>{jobData.mask}</TableCell>
            </TableRow>
          );
        }
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="wordlists">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.wordlists')}</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        break;

      case 9: // Association
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            <TableRow key="association-wordlist">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.associationHints')}</TableCell>
              <TableCell>{jobData.wordlist_names.join(', ')}</TableCell>
            </TableRow>
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            <TableRow key="rules">
              <TableCell sx={{ fontWeight: 'bold' }}>{t('details.rules')}</TableCell>
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
          {t('common.back')}
        </Button>
      </Box>
    );
  }

  if (!jobData) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">{t('details.jobNotFound')}</Alert>
        <Button startIcon={<ArrowBack />} onClick={() => navigate(-1)} sx={{ mt: 2 }}>
          {t('common.back')}
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

  const totalKeyspace = jobData.effective_keyspace || 0;

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
            {t('common.back')}
          </Button>
          <Typography variant="h4" component="h1">
            {t('details.pageTitle')}
          </Typography>
          <Chip
            label={jobData.status}
            color={getStatusColor(jobData.status) as any}
            size="small"
          />
          {['pending', 'running', 'paused'].includes(jobData.status) && (
            <Chip
              label={autoRefreshEnabled && !isEditingRef.current ? t('details.autoRefreshOn') : t('details.autoRefreshPaused')}
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
              {t('details.forceComplete')}
            </Button>
          )}
          <IconButton onClick={fetchJobDetails} disabled={loading} title={t('details.refreshNow')}>
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
          <Typography variant="h6">{t('details.jobInformation')}</Typography>
        </Box>
        <TableContainer>
          <Table>
            <TableBody>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold', width: '30%' }}>{t('common.id')}</TableCell>
                <TableCell>{jobData.id}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.name')}</TableCell>
                <TableCell>{jobData.name}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.status')}</TableCell>
                <TableCell>
                  <Chip
                    label={jobData.status}
                    color={getStatusColor(jobData.status) as any}
                    size="small"
                  />
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('details.priority')}</TableCell>
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
                        helperText={t('details.priorityRange', { max: maxPriority })}
                      />
                      <IconButton onClick={handleSavePriority} disabled={saving} size="small" title={t('tooltips.save')}>
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelPriority} disabled={saving} size="small" title={t('tooltips.cancel')}>
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
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.maxAgents')}</TableCell>
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
                        helperText={t('details.maxAgentsHint')}
                      />
                      <IconButton onClick={handleSaveMaxAgents} disabled={saving} size="small" title={t('tooltips.save')}>
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelMaxAgents} disabled={saving} size="small" title={t('tooltips.cancel')}>
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
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.chunkSize')}</TableCell>
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
                        helperText={t('details.chunkSizeHint')}
                      />
                      <IconButton onClick={handleSaveChunkSize} disabled={saving} size="small" title={t('tooltips.save')}>
                        <SaveIcon />
                      </IconButton>
                      <IconButton onClick={handleCancelChunkSize} disabled={saving} size="small" title={t('tooltips.cancel')}>
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
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.hashlist')}</TableCell>
                <TableCell>
                  <Link
                    component="button"
                    onClick={() => navigate(`/hashlists/${jobData.hashlist_id}`)}
                    sx={{ cursor: 'pointer' }}
                  >
                    {jobData.hashlist_name}
                  </Link>
                  {' '}({t('common.id')}: {jobData.hashlist_id})
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.hashType')}</TableCell>
                <TableCell>{jobData.hash_type || t('common.notAvailable')}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.attackMode')}</TableCell>
                <TableCell>{getAttackModeName(jobData.attack_mode)}</TableCell>
              </TableRow>
              {/* Attack configuration rows based on attack mode */}
              {renderAttackConfigRows()}
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.keyspace')}</TableCell>
                <TableCell>{formatKeyspace(jobData.base_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.effectiveKeyspace')}</TableCell>
                <TableCell>
                  {formatKeyspace(jobData.effective_keyspace)}
                  {jobData.multiplication_factor && jobData.multiplication_factor > 1 && (
                    <Chip
                      label={`×${jobData.multiplication_factor}${jobData.uses_rule_splitting ? ` ${t('common.rulesMultiplier')}` : ''}`}
                      size="small"
                      color="error"
                      variant="filled"
                      sx={{ ml: 1 }}
                    />
                  )}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.processedKeyspace')}</TableCell>
                <TableCell>{formatKeyspace(jobData.processed_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.dispatchedKeyspace')}</TableCell>
                <TableCell>{formatKeyspace(jobData.dispatched_keyspace)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.progress')}</TableCell>
                <TableCell>{jobData.overall_progress_percent?.toFixed(2) || 0}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.cracksFound')}</TableCell>
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
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.createdAt')}</TableCell>
                <TableCell>{formatDate(jobData.created_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.startedAt')}</TableCell>
                <TableCell>{formatDate(jobData.started_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('details.timeRemaining')}</TableCell>
                <TableCell>{estimatedCompletion.timeRemaining}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('details.estimatedCompletion')}</TableCell>
                <TableCell>{estimatedCompletion.estimatedDate}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('details.crackingCompletedAt')}</TableCell>
                <TableCell>{formatDate(jobData.cracking_completed_at)}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell sx={{ fontWeight: 'bold' }}>{t('common.completedAt')}</TableCell>
                <TableCell>{formatDate(jobData.completed_at)}</TableCell>
              </TableRow>
              {jobData.error_message && (
                <TableRow>
                  <TableCell sx={{ fontWeight: 'bold' }}>{t('common.error')}</TableCell>
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
              {t('details.incrementLayers', { count: layers.length })}
            </Typography>
          </Box>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>{t('details.layerTable.layer')}</TableCell>
                  <TableCell>{t('details.layerTable.mask')}</TableCell>
                  <TableCell>{t('details.layerTable.status')}</TableCell>
                  <TableCell>{t('details.layerTable.keyspace')}</TableCell>
                  <TableCell>{t('details.layerTable.effectiveKeyspace')}</TableCell>
                  <TableCell>{t('details.layerTable.progress')}</TableCell>
                  <TableCell>{t('details.layerTable.tasks')}</TableCell>
                  <TableCell>{t('details.layerTable.cracks')}</TableCell>
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
                          label={t('details.estimatedKeyspace')}
                          size="small"
                          color="warning"
                          variant="outlined"
                          sx={{ ml: 1 }}
                        />
                      )}
                    </TableCell>
                    <TableCell>{layer.overall_progress_percent?.toFixed(2) || 0}%</TableCell>
                    <TableCell>
                      {layer.running_tasks || 0} {t('details.layerTable.running')} / {layer.total_tasks || 0} {t('details.layerTable.total')}
                      {layer.failed_tasks != null && layer.failed_tasks > 0 && (
                        <Chip
                          label={t('details.layerTable.failed', { count: layer.failed_tasks })}
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
          {t('details.taskProgressVisualization')}
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
            {t('details.activeTasks', { count: activeTasks.length })}
          </Typography>
        </Box>
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('details.activeTasksTable.agentId')}</TableCell>
                <TableCell>{t('details.activeTasksTable.taskId')}</TableCell>
                <TableCell>{t('common.status')}</TableCell>
                <TableCell>{t('details.activeTasksTable.keyspaceRange')}</TableCell>
                <TableCell>{t('details.activeTasksTable.progress')}</TableCell>
                <TableCell>{t('details.activeTasksTable.currentSpeed')}</TableCell>
                <TableCell>{t('details.activeTasksTable.cracks')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {activeTasks.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} align="center">
                    <Typography color="text.secondary" sx={{ py: 2 }}>
                      {t('details.noActiveTasks')}
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                activeTasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>{task.agent_id || t('common.unassigned')}</TableCell>
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
              {t('details.failedTasks', { count: failedTasks.length })}
            </Typography>
          </Box>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('details.failedTasksTable.agentId')}</TableCell>
                  <TableCell>{t('details.failedTasksTable.taskId')}</TableCell>
                  <TableCell>{t('details.failedTasksTable.status')}</TableCell>
                  <TableCell>{t('details.failedTasksTable.retryCount')}</TableCell>
                  <TableCell>{t('details.failedTasksTable.errorMessage')}</TableCell>
                  <TableCell>{t('details.failedTasksTable.lastUpdated')}</TableCell>
                  <TableCell align="center">{t('details.failedTasksTable.actions')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {failedTasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>{task.agent_id || t('common.unassigned')}</TableCell>
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
                        {task.error_message || t('common.noErrorMessage')}
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
                        {t('buttons.retry')}
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
              {t('details.completedTasks', { count: completedTasks.length })}
            </Typography>
          </Box>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('details.completedTasksTable.agentId')}</TableCell>
                  <TableCell>{t('details.completedTasksTable.taskId')}</TableCell>
                  <TableCell>{t('details.completedTasksTable.completedAt')}</TableCell>
                  <TableCell>{t('details.activeTasksTable.keyspaceRange')}</TableCell>
                  <TableCell>{t('details.completedTasksTable.finalProgress')}</TableCell>
                  <TableCell>{t('details.completedTasksTable.averageSpeed')}</TableCell>
                  <TableCell>{t('details.completedTasksTable.cracksFound')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {paginatedCompletedTasks.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} align="center">
                      <Typography color="text.secondary" sx={{ py: 2 }}>
                        {t('details.noCompletedTasks')}
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : (
                  paginatedCompletedTasks.map((task) => (
                    <TableRow key={task.id}>
                      <TableCell>{task.agent_id || t('common.unassigned')}</TableCell>
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
              labelRowsPerPage={t('pagination.rowsPerPage', { ns: 'common' }) as string}
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
        <DialogTitle>{t('dialogs.forceComplete.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            <strong>{t('common.warning')}:</strong> {t('dialogs.forceComplete.warning')}
          </DialogContentText>
          <DialogContentText sx={{ mt: 2 }}>
            <strong>{t('dialogs.forceComplete.onlyUseIf')}</strong>
          </DialogContentText>
          <DialogContentText component="ul" sx={{ mt: 1, pl: 2 }}>
            <li>{t('dialogs.forceComplete.reason1')}</li>
            <li>{t('dialogs.forceComplete.reason2')}</li>
            <li>{t('dialogs.forceComplete.reason3')}</li>
          </DialogContentText>
          <DialogContentText sx={{ mt: 2, fontStyle: 'italic', fontSize: '0.9em' }}>
            {t('dialogs.forceComplete.note')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setForceCompleteDialogOpen(false)} disabled={forceCompleting}>
            {t('buttons.cancel')}
          </Button>
          <Button
            onClick={handleForceComplete}
            color="warning"
            variant="contained"
            disabled={forceCompleting}
            startIcon={forceCompleting ? <CircularProgress size={20} /> : <CheckCircleIcon />}
          >
            {forceCompleting ? t('dialogs.forceComplete.completing') : t('dialogs.forceComplete.button')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default JobDetails;