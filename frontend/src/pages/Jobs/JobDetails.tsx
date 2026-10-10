import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Box,
  Typography,
  Paper,
  Button,
  Chip,
  CircularProgress,
  Alert,
  AlertTitle,
  Skeleton,
  TextField,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions
} from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import {
  ArrowBack,
  Edit as EditIcon,
  Save as SaveIcon,
  Cancel as CancelIcon,
  Refresh as RefreshIcon,
  CheckCircle as CheckCircleIcon
} from '@mui/icons-material';
import { getJobDetails, getJobLayers, api } from '../../services/api';
import { JobDetailsResponse, JobTask, JobIncrementLayerWithStats, EntityRef, UserRef } from '../../types/jobs';
import { DataTable, EntityLink, PageHeader, SimpleTable, StatusChip, useToast, useConfirm } from '../../components/ui';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import JobProgressBar from '../../components/JobProgressBar';
import BenchmarkBlocklistPanel from '../../components/jobs/BenchmarkBlocklistPanel';
import CloudProjectionDialog from '../../components/jobs/CloudProjectionDialog';
import { getMaxPriorityForUsers } from '../../services/systemSettings';
import { provisionInstanceForJob } from '../../services/cloud';

/** Task as returned on the job detail payload (agent_name joined server-side). */
type TaskRow = JobTask & { agent_name?: string };

/** Job detail payload plus the entity references the backend now joins in. */
type JobDetailsData = Omit<JobDetailsResponse, 'tasks'> & {
  tasks: TaskRow[];
  client?: EntityRef | null;
  preset_job?: EntityRef | null;
  workflow?: EntityRef | null;
  created_by?: UserRef | null;
};

interface InfoRow {
  key: string;
  label: React.ReactNode;
  value: React.ReactNode;
  mono?: boolean;
}

const ACTIVE_JOB_STATUSES = ['pending', 'running', 'paused'];
const SYSTEM_USER_ID = '00000000-0000-0000-0000-000000000000';

const JobDetails: React.FC = () => {
  const { t } = useTranslation('jobs');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
  const [projectionOpen, setProjectionOpen] = useState(false);
  const [provisioning, setProvisioning] = useState(false);

  /*
   * Manual provisioning is a spend, so it confirms first and reports the
   * backend's refusal verbatim — those messages name the rail that blocked it
   * (budget, window, consent, quota), which is exactly what an operator needs
   * and what a generic "failed" would throw away.
   */
  const handleProvisionNow = async () => {
    if (!id) return;
    const ok = await confirm({
      title: t('cloud.cloudBurst.provisionNow') as string,
      message: t('cloud.cloudBurst.provisionConfirm') as string,
      confirmLabel: t('cloud.cloudBurst.provisionNow') as string,
    });
    if (!ok) return;
    setProvisioning(true);
    try {
      await provisionInstanceForJob(id);
      toast.success(t('cloud.cloudBurst.provisionRequested') as string);
    } catch (err: any) {
      toast.error(err?.response?.data?.error || (t('cloud.cloudBurst.provisionFailed') as string));
    } finally {
      setProvisioning(false);
    }
  };
  // Action errors (save failures); load errors come from the query below.
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

  const isEditing = editingPriority || editingMaxAgents || editingChunkSize;

  // Job details (+ increment layers). Polls at the `live` tier while the job is
  // still active, auto-refresh is on and no inline editor is open.
  const jobQuery = useLiveQuery<{ job: JobDetailsData; layers: JobIncrementLayerWithStats[] }>(
    {
      queryKey: [...qk.jobs.detail(id ?? ''), 'with-layers'],
      queryFn: async () => {
        const data: JobDetailsData = await getJobDetails(id as string);
        let layersData: JobIncrementLayerWithStats[] = [];
        if (data.increment_mode && data.increment_mode !== 'off') {
          try {
            // Handle null/undefined response - always ensure layers is an array
            layersData = (await getJobLayers(id as string)) || [];
          } catch (layerErr) {
            // Don't fail the whole page if layers fail to load
            console.error('[JobDetails] Failed to fetch increment layers:', layerErr);
          }
        }
        return { job: data, layers: layersData };
      },
      enabled: !!id,
    },
    {
      tier: 'live',
      enabled: autoRefreshEnabled && !isEditing,
      when: (d) => !!d && ACTIVE_JOB_STATUSES.includes(d.job.status),
    }
  );
  const jobData: JobDetailsData | null = jobQuery.data?.job ?? null;
  const layers: JobIncrementLayerWithStats[] = jobQuery.data?.layers ?? [];
  const loading = jobQuery.isLoading;
  const loadError = jobQuery.isError ? (t('errors.loadDetailsFailed') as string) : null;
  const { refetch } = jobQuery;
  const fetchJobDetails = useCallback(async () => {
    await refetch();
  }, [refetch]);

  // Fetch max priority setting
  useEffect(() => {
    getMaxPriorityForUsers()
      .then(config => {
        setMaxPriority(config.max_priority);
      })
      .catch(err => {
        console.error('Failed to fetch max priority:', err);
        // Keep default of 1000 if fetch fails
      });
  }, []);
  

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
      toast.error(t('validation.priorityRange', { max: maxPriority }));
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
      toast.error(t('validation.maxAgentsPositive'));
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
      toast.error(t('validation.chunkSizeMin'));
      return;
    }
    if (chunkSizeValue > 86400) {
      toast.error(t('validation.chunkSizeMax'));
      return;
    }

    setSaving(true);
    try {
      const response = await api.patch(`/api/jobs/${id}`, { chunk_size_seconds: chunkSizeValue });

      // Show success notification with specific message
      toast.success(response.data?.message || t('success.chunkSizeUpdated'), { autoHideDuration: 5000 });

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

      toast.error(errorMessage, { autoHideDuration: 5000 });
    } finally {
      setSaving(false);
    }
  };

  const handleCancelChunkSize = () => {
    setEditingChunkSize(false);
    setAutoRefreshEnabled(true); // Resume auto-refresh after cancel
  };

  // Handle force complete job
  const handleForceComplete = async () => {
    if (!id) return;

    setForceCompleting(true);
    try {
      await api.post(`/api/jobs/${id}/force-complete`);
      await fetchJobDetails();
      setForceCompleteDialogOpen(false);
      toast.success(t('success.forceCompleted'));
    } catch (err: any) {
      console.error('Failed to force complete job:', err);
      const errorMessage = err.response?.data?.message || t('errors.forceCompleteFailed');
      toast.error(errorMessage);
    } finally {
      setForceCompleting(false);
    }
  };

  // Format helpers
  const formatDate = (dateString?: string) => {
    if (!dateString) return t('common.notAvailable');
    return new Date(dateString).toLocaleString();
  };

  const formatKeyspace = (value?: number | string): string => {
    // Effective keyspace fields arrive as decimal strings (NUMERIC, can exceed
    // 2^53); base keyspace stays a number. Coerce either to Number for the
    // abbreviated K/M/B/T display.
    const v = value === undefined || value === null ? 0 : Number(value);
    if (!v || !isFinite(v)) return t('common.notAvailable');
    if (v >= 1e12) return `${(v / 1e12).toFixed(2)}T`;
    if (v >= 1e9) return `${(v / 1e9).toFixed(2)}B`;
    if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
    if (v >= 1e3) return `${(v / 1e3).toFixed(2)}K`;
    return v.toString();
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

  const calculateTotalRunningTime = (): string => {
    if (!jobData?.started_at) return t('common.notAvailable');

    const startTime = new Date(jobData.started_at).getTime();
    const endTime = jobData.completed_at
      ? new Date(jobData.completed_at).getTime()
      : Date.now();

    const totalSeconds = Math.floor((endTime - startTime) / 1000);

    if (totalSeconds <= 0) return t('details.lessThanOneMinute');

    const duration = formatDuration(totalSeconds);
    // formatDuration prepends "~" for estimates; strip it for actual elapsed time
    return duration.startsWith('~') ? duration.substring(1).trim() : duration;
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
    // effective_keyspace / processed_keyspace are decimal strings (NUMERIC) — coerce.
    const effectiveKeyspace = Number(jobData?.effective_keyspace || 0);
    const processedKeyspace = Number(jobData?.processed_keyspace || 0);
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
  const renderAttackConfigRows = (): InfoRow[] => {
    if (!jobData) return [];

    const rows: InfoRow[] = [];
    const attackMode = jobData.attack_mode;

    switch (attackMode) {
      case 0: // Dictionary/Straight
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            { key: 'wordlists', label: t('details.wordlists'), value: jobData.wordlist_names.join(', ') }
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            { key: 'rules', label: t('details.rules'), value: jobData.rule_names.join(', ') }
          );
        }
        // Splitting mode for dictionary attacks
        rows.push(
          { key: 'splitting-mode', label: t('details.splittingMode'), value: (
              <>
                <Chip
                label={t('common.keyspace')}
                size="small"
                color="default"
                variant="outlined"
              />
              </>
            ) }
        );
        break;

      case 1: // Combination
        if (jobData.wordlist_names && jobData.wordlist_names.length >= 2) {
          rows.push(
            { key: 'first-wordlist', label: t('details.firstWordlist'), value: jobData.wordlist_names[0] }
          );
          rows.push(
            { key: 'second-wordlist', label: t('details.secondWordlist'), value: jobData.wordlist_names[1] }
          );
        } else if (jobData.wordlist_names && jobData.wordlist_names.length === 1) {
          rows.push(
            { key: 'wordlist', label: t('details.wordlist'), value: jobData.wordlist_names[0] }
          );
        }
        // Splitting mode for combination attacks
        rows.push(
          { key: 'splitting-mode', label: t('details.splittingMode'), value: (
              <>
                <Chip
                label={t('common.keyspace')}
                size="small"
                color="default"
                variant="outlined"
              />
              </>
            ) }
        );
        break;

      case 3: // Brute-force/Mask
        if (jobData.mask) {
          rows.push(
            { key: 'mask', label: t('details.mask'), value: jobData.mask, mono: true }
          );
        }
        if (jobData.increment_mode && jobData.increment_mode !== 'off') {
          rows.push(
            { key: 'increment-mode', label: t('details.incrementMode'), value: (
              <>
                <Chip
                  label={jobData.increment_mode === 'increment' ? t('details.increment') : t('details.incrementInverse')}
                  size="small"
                  color="info"
                  variant="outlined"
                />
              </>
            ) }
          );
          rows.push(
            { key: 'increment-range', label: t('details.incrementRange'), value: (
              <>
                {jobData.increment_min ?? 1} - {jobData.increment_max ?? (jobData.mask?.length || t('common.notAvailable'))}
              </>
            ) }
          );
        }
        break;

      case 6: // Hybrid Wordlist + Mask
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            { key: 'wordlists', label: t('details.wordlists'), value: jobData.wordlist_names.join(', ') }
          );
        }
        if (jobData.mask) {
          rows.push(
            { key: 'mask', label: t('details.maskSuffix'), value: jobData.mask, mono: true }
          );
        }
        break;

      case 7: // Hybrid Mask + Wordlist
        if (jobData.mask) {
          rows.push(
            { key: 'mask', label: t('details.maskPrefix'), value: jobData.mask, mono: true }
          );
        }
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            { key: 'wordlists', label: t('details.wordlists'), value: jobData.wordlist_names.join(', ') }
          );
        }
        break;

      case 9: // Association
        if (jobData.wordlist_names && jobData.wordlist_names.length > 0) {
          rows.push(
            { key: 'association-wordlist', label: t('details.associationHints'), value: jobData.wordlist_names.join(', ') }
          );
        }
        if (jobData.rule_names && jobData.rule_names.length > 0) {
          rows.push(
            { key: 'rules', label: t('details.rules'), value: jobData.rule_names.join(', ') }
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

  if (loadError && !jobData) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" sx={{ mb: 3 }}>
          {loadError}
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

  const totalKeyspace = Number(jobData.effective_keyspace || 0);

  // Calculate estimated completion once for efficiency
  const estimatedCompletion = jobData ? calculateEstimatedCompletion() : { timeRemaining: '', estimatedDate: '' };

  const isActiveJob = ACTIVE_JOB_STATUSES.includes(jobData.status);
  const displayedError = error ?? (jobData ? loadError : null);

  const renderAgent = (task: TaskRow) =>
    task.agent_id ? (
      <EntityLink type="agent" id={task.agent_id} label={task.agent_name || `#${task.agent_id}`} />
    ) : (
      (t('common.unassigned') as string)
    );

  const renderCracks = (count: number) =>
    count > 0 ? <EntityLink type="pot_job" id={jobData.id} label={String(count)} /> : count;

  const renderKeyspaceRange = (task: TaskRow) =>
    `${formatKeyspace(task.effective_keyspace_start || task.keyspace_start)} - ${formatKeyspace(
      task.effective_keyspace_end || task.keyspace_end
    )}`;

  const editableValue = (
    editing: boolean,
    display: React.ReactNode,
    temp: string,
    setTemp: (v: string) => void,
    onSave: () => void,
    onCancel: () => void,
    onEdit: () => void,
    helperText: string,
    width: number
  ) =>
    editing ? (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <TextField
          type="number"
          value={temp}
          onChange={(e) => setTemp(e.target.value)}
          size="small"
          sx={{ width }}
          disabled={saving}
          helperText={helperText}
        />
        <IconButton onClick={onSave} disabled={saving} size="small" title={t('tooltips.save')}>
          <SaveIcon />
        </IconButton>
        <IconButton onClick={onCancel} disabled={saving} size="small" title={t('tooltips.cancel')}>
          <CancelIcon />
        </IconButton>
      </Box>
    ) : (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {display}
        <IconButton onClick={onEdit} size="small">
          <EditIcon />
        </IconButton>
      </Box>
    );

  const createdBy = jobData.created_by;
  const infoRows: InfoRow[] = [
    { key: 'id', label: t('common.id'), value: jobData.id, mono: true },
    { key: 'name', label: t('common.name'), value: jobData.name },
    { key: 'status', label: t('common.status'), value: <StatusChip entity="job" status={jobData.status} /> },
    ...(jobData.client
      ? [{ key: 'client', label: t('details.client', 'Client'), value: <EntityLink type="client" id={jobData.client.id} label={jobData.client.name} /> }]
      : []),
    ...(jobData.preset_job
      ? [{ key: 'preset', label: t('details.presetJob', 'Preset Job'), value: <EntityLink type="preset_job" id={jobData.preset_job.id} label={jobData.preset_job.name} /> }]
      : []),
    ...(jobData.workflow
      ? [{ key: 'workflow', label: t('details.workflow', 'Workflow'), value: <EntityLink type="workflow" id={jobData.workflow.id} label={jobData.workflow.name} /> }]
      : []),
    ...(createdBy
      ? [{
          key: 'created-by',
          label: t('details.createdBy', 'Created By'),
          value:
            createdBy.id && createdBy.id !== SYSTEM_USER_ID ? (
              <EntityLink type="user" id={createdBy.id} label={createdBy.username} />
            ) : (
              createdBy.username || (t('details.system', 'System') as string)
            ),
        }]
      : []),
    {
      key: 'priority',
      label: t('details.priority'),
      value: editableValue(
        editingPriority,
        jobData.priority,
        tempPriority,
        setTempPriority,
        handleSavePriority,
        handleCancelPriority,
        handleEditPriority,
        t('details.priorityRange', { max: maxPriority }) as string,
        100
      ),
    },
    {
      key: 'max-agents',
      label: t('common.maxAgents'),
      value: editableValue(
        editingMaxAgents,
        jobData.max_agents,
        tempMaxAgents,
        setTempMaxAgents,
        handleSaveMaxAgents,
        handleCancelMaxAgents,
        handleEditMaxAgents,
        t('details.maxAgentsHint') as string,
        100
      ),
    },
    ...(jobData.cloud_burst_enabled
      ? [{
          key: 'cloud-burst',
          label: t('cloud.cloudBurst.badge'),
          value: (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Chip size="small" color="info" label={t('cloud.cloudBurst.badge')} />
              {jobData.cloud_max_instances
                ? t('cloud.cloudBurst.maxInstances') + ': ' + jobData.cloud_max_instances
                : null}
              {/* Answers "will this finish before the budget runs out?"
                  before any money is spent. */}
              <Button size="small" onClick={() => setProjectionOpen(true)}>
                {t('cloud.projection.title')}
              </Button>
              {/*
                * Rent one instance now, bypassing the autoscaler.
                *
                * The autoscaler applies the SOFT rules — minimum
                * starvation, skip-if-finishing-soon — and will not act
                * while any on-prem agent is idle. That is right for
                * automatic spending and wrong for an operator who has
                * decided they want capacity now, and it makes teardown
                * and provider testing nearly impossible to exercise
                * deliberately. The hard rails (window, per-job cap,
                * budget, consent) still apply.
                */}
              <Button size="small" color="warning" disabled={provisioning} onClick={handleProvisionNow}>
                {t('cloud.cloudBurst.provisionNow')}
              </Button>
            </Box>
          ),
        }]
      : []),
    {
      key: 'chunk-size',
      label: t('common.chunkSize'),
      value: editableValue(
        editingChunkSize,
        formatChunkSize(jobData.chunk_size_seconds),
        tempChunkSize,
        setTempChunkSize,
        handleSaveChunkSize,
        handleCancelChunkSize,
        handleEditChunkSize,
        t('details.chunkSizeHint') as string,
        120
      ),
    },
    {
      key: 'hashlist',
      label: t('common.hashlist'),
      value: (
        <>
          <EntityLink type="hashlist" id={jobData.hashlist_id} label={jobData.hashlist_name} />{' '}
          ({t('common.id')}: {jobData.hashlist_id})
        </>
      ),
    },
    { key: 'hash-type', label: t('common.hashType'), value: jobData.hash_type != null ? jobData.hash_type : t('common.notAvailable') },
    { key: 'attack-mode', label: t('common.attackMode'), value: getAttackModeName(jobData.attack_mode) },
    // Attack configuration rows based on attack mode
    ...renderAttackConfigRows(),
    ...(jobData.additional_args
      ? [{ key: 'additional-args', label: t('jobs:details.additionalArgs', 'Additional Arguments'), value: jobData.additional_args, mono: true }]
      : []),
    { key: 'keyspace', label: t('common.keyspace'), value: formatKeyspace(jobData.base_keyspace) },
    { key: 'effective-keyspace', label: t('common.effectiveKeyspace'), value: formatKeyspace(jobData.effective_keyspace) },
    { key: 'processed-keyspace', label: t('common.processedKeyspace'), value: formatKeyspace(jobData.processed_keyspace) },
    { key: 'dispatched-keyspace', label: t('common.dispatchedKeyspace'), value: formatKeyspace(jobData.dispatched_keyspace) },
    { key: 'progress', label: t('common.progress'), value: `${jobData.overall_progress_percent?.toFixed(2) || 0}%` },
    { key: 'cracks', label: t('common.cracksFound'), value: renderCracks(jobData.cracked_count) },
    { key: 'created-at', label: t('common.createdAt'), value: formatDate(jobData.created_at) },
    { key: 'started-at', label: t('common.startedAt'), value: formatDate(jobData.started_at) },
    { key: 'time-remaining', label: t('details.timeRemaining'), value: estimatedCompletion.timeRemaining },
    { key: 'estimated-completion', label: t('details.estimatedCompletion'), value: estimatedCompletion.estimatedDate },
    { key: 'cracking-completed-at', label: t('details.crackingCompletedAt'), value: formatDate(jobData.cracking_completed_at) },
    { key: 'completed-at', label: t('common.completedAt'), value: formatDate(jobData.completed_at) },
    ...(jobData.started_at
      ? [{ key: 'total-running-time', label: t('details.totalRunningTime'), value: calculateTotalRunningTime() }]
      : []),
    ...(jobData.error_message
      ? [{
          key: 'error',
          label: t('common.error'),
          value: (
            <Alert severity="error" sx={{ py: 0.5 }}>
              {jobData.error_message}
            </Alert>
          ),
        }]
      : []),
  ];

  const layerColumns: GridColDef<JobIncrementLayerWithStats>[] = [
    { field: 'layer_index', headerName: t('details.layerTable.layer') as string, width: 80 },
    {
      field: 'mask',
      headerName: t('details.layerTable.mask') as string,
      flex: 1,
      minWidth: 120,
      renderCell: (p) => (
        <Box component="span" sx={{ fontFamily: (theme) => theme.typography.monoFamily }}>
          {p.row.mask}
        </Box>
      ),
    },
    {
      field: 'status',
      headerName: t('details.layerTable.status') as string,
      width: 120,
      renderCell: (p) => <StatusChip entity="job" status={p.row.status} />,
    },
    {
      field: 'base_keyspace',
      headerName: t('details.layerTable.keyspace') as string,
      width: 110,
      renderCell: (p) => formatKeyspace(p.row.base_keyspace),
    },
    {
      field: 'effective_keyspace',
      headerName: t('details.layerTable.effectiveKeyspace') as string,
      width: 190,
      renderCell: (p) => (
        <>
          {formatKeyspace(p.row.effective_keyspace)}
          {!p.row.is_accurate_keyspace && (
            <Chip label={t('details.estimatedKeyspace')} size="small" color="warning" variant="outlined" sx={{ ml: 1 }} />
          )}
        </>
      ),
    },
    {
      field: 'overall_progress_percent',
      headerName: t('details.layerTable.progress') as string,
      width: 100,
      renderCell: (p) => `${p.row.overall_progress_percent?.toFixed(2) || 0}%`,
    },
    {
      field: 'tasks',
      headerName: t('details.layerTable.tasks') as string,
      flex: 1,
      minWidth: 200,
      sortable: false,
      renderCell: (p) => (
        <>
          {p.row.running_tasks || 0} {t('details.layerTable.running')} / {p.row.total_tasks || 0} {t('details.layerTable.total')}
          {p.row.failed_tasks != null && p.row.failed_tasks > 0 && (
            <Chip label={t('details.layerTable.failed', { count: p.row.failed_tasks })} size="small" color="error" sx={{ ml: 1 }} />
          )}
        </>
      ),
    },
    {
      field: 'crack_count',
      headerName: t('details.layerTable.cracks') as string,
      width: 90,
      renderCell: (p) => renderCracks(p.row.crack_count ?? 0),
    },
  ];

  const agentColumn: GridColDef<TaskRow> = {
    field: 'agent_id',
    headerName: t('details.activeTasksTable.agentId') as string,
    width: 160,
    valueGetter: (_v, row) => row.agent_name || (row.agent_id ? `#${row.agent_id}` : ''),
    renderCell: (p) => renderAgent(p.row),
  };
  const taskIdColumn: GridColDef<TaskRow> = {
    field: 'id',
    headerName: t('details.activeTasksTable.taskId') as string,
    width: 300,
    renderCell: (p) => (
      <Box component="span" sx={{ fontFamily: (theme) => theme.typography.monoFamily, fontSize: '0.75rem' }}>
        {p.row.id}
      </Box>
    ),
  };
  const cracksColumn = (headerName: string): GridColDef<TaskRow> => ({
    field: 'crack_count',
    headerName,
    width: 100,
    renderCell: (p) => renderCracks(p.row.crack_count),
  });

  const activeTaskColumns: GridColDef<TaskRow>[] = [
    agentColumn,
    taskIdColumn,
    {
      field: 'status',
      headerName: t('common.status') as string,
      width: 140,
      renderCell: (p) => <StatusChip entity="task" status={p.row.status} />,
    },
    {
      field: 'keyspace_range',
      headerName: t('details.activeTasksTable.keyspaceRange') as string,
      flex: 1,
      minWidth: 160,
      sortable: false,
      renderCell: (p) => renderKeyspaceRange(p.row),
    },
    {
      field: 'progress_percent',
      headerName: t('details.activeTasksTable.progress') as string,
      width: 100,
      renderCell: (p) => {
        const pct = p.row.progress_percent ?? 0;
        const shown = p.row.status === 'running' ? Math.min(pct, 99.99) : pct;
        return `${shown.toFixed(2)}%`;
      },
    },
    {
      field: 'benchmark_speed',
      headerName: t('details.activeTasksTable.currentSpeed') as string,
      width: 130,
      renderCell: (p) => formatSpeed(p.row.benchmark_speed),
    },
    cracksColumn(t('details.activeTasksTable.cracks') as string),
  ];

  const failedTaskColumns: GridColDef<TaskRow>[] = [
    { ...agentColumn, headerName: t('details.failedTasksTable.agentId') as string },
    { ...taskIdColumn, headerName: t('details.failedTasksTable.taskId') as string },
    {
      field: 'status',
      headerName: t('details.failedTasksTable.status') as string,
      width: 140,
      renderCell: (p) => <StatusChip entity="task" status={p.row.status} />,
    },
    {
      field: 'retry_count',
      headerName: t('details.failedTasksTable.retryCount') as string,
      width: 100,
      valueGetter: (_v, row) => row.retry_count || 0,
    },
    {
      field: 'error_message',
      headerName: t('details.failedTasksTable.errorMessage') as string,
      flex: 1,
      minWidth: 240,
      valueGetter: (_v, row) => row.error_message || row.failure_reason || (t('common.noErrorMessage') as string),
      renderCell: (p) => (
        <Typography variant="body2" sx={{ whiteSpace: 'normal', py: 0.5 }}>
          {p.value as string}
        </Typography>
      ),
    },
    {
      field: 'updated_at',
      headerName: t('details.failedTasksTable.lastUpdated') as string,
      width: 180,
      renderCell: (p) => formatDate(p.row.updated_at),
    },
    // Actions column removed: per-task Retry no longer applies in scheduler-v2.
    // Every dispatch is a fresh task with a new UUID; "retrying" a failed task
    // can't reuse the same task ID — it would just create another orphan.
    // Job-level retry (clone/recreate the job) is the v2 primitive.
  ];

  const completedTaskColumns: GridColDef<TaskRow>[] = [
    { ...agentColumn, headerName: t('details.completedTasksTable.agentId') as string },
    { ...taskIdColumn, headerName: t('details.completedTasksTable.taskId') as string },
    {
      field: 'completed_at',
      headerName: t('details.completedTasksTable.completedAt') as string,
      width: 180,
      renderCell: (p) => formatDate(p.row.completed_at),
    },
    {
      field: 'keyspace_range',
      headerName: t('details.activeTasksTable.keyspaceRange') as string,
      flex: 1,
      minWidth: 160,
      sortable: false,
      renderCell: (p) => renderKeyspaceRange(p.row),
    },
    {
      field: 'progress_percent',
      headerName: t('details.completedTasksTable.finalProgress') as string,
      width: 110,
      renderCell: (p) => `${p.row.progress_percent?.toFixed(2) || 100}%`,
    },
    {
      field: 'average_speed',
      headerName: t('details.completedTasksTable.averageSpeed') as string,
      width: 130,
      renderCell: (p) => formatSpeed(p.row.average_speed || p.row.benchmark_speed),
    },
    cracksColumn(t('details.completedTasksTable.cracksFound') as string),
  ];

  return (
    <Box sx={{ p: 3 }}>
      {/* Header */}
      <PageHeader
        title={t('details.pageTitle')}
        backTo="/jobs"
        status={
          <>
            <StatusChip entity="job" status={jobData.status} />
            {isActiveJob && (
              <Chip
                label={autoRefreshEnabled && !isEditing ? t('details.autoRefreshOn') : t('details.autoRefreshPaused')}
                color={autoRefreshEnabled && !isEditing ? 'success' : 'warning'}
                size="small"
                variant="outlined"
              />
            )}
          </>
        }
        description={
          <>
            {jobData.name}
            {jobData.client && (
              <>
                {' · '}
                {t('details.client', 'Client')}:{' '}
                <EntityLink type="client" id={jobData.client.id} label={jobData.client.name} />
              </>
            )}
            {jobData.preset_job && (
              <>
                {' · '}
                {t('details.presetJob', 'Preset Job')}:{' '}
                <EntityLink type="preset_job" id={jobData.preset_job.id} label={jobData.preset_job.name} />
              </>
            )}
            {jobData.workflow && (
              <>
                {' · '}
                {t('details.workflow', 'Workflow')}:{' '}
                <EntityLink type="workflow" id={jobData.workflow.id} label={jobData.workflow.name} />
              </>
            )}
            {createdBy && createdBy.id && createdBy.id !== SYSTEM_USER_ID && (
              <>
                {' · '}
                {t('details.createdBy', 'Created By')}:{' '}
                <EntityLink type="user" id={createdBy.id} label={createdBy.username} />
              </>
            )}
          </>
        }
        actions={
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
            <IconButton onClick={fetchJobDetails} disabled={jobQuery.isFetching} title={t('details.refreshNow')}>
              <RefreshIcon />
            </IconButton>
          </Box>
        }
      />

      {/* Error Alert */}
      {displayedError && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {displayedError}
        </Alert>
      )}

      {/*
        * Why nothing is happening.
        *
        * Placed above the job information rather than beside the cloud row on
        * purpose: the case this exists for is a job sitting at pending with no
        * agents and no instances, where there is nothing else on the page that
        * would draw the eye. Previously the only record of a provisioning
        * refusal was a line in the server log, which is no help at all to
        * someone running an entirely rented fleet — or to a remote tester.
        */}
      {(jobData.diagnostics ?? []).length > 0 && (
        <Box sx={{ mb: 3 }}>
          {(jobData.diagnostics ?? []).map((d) => (
            <Alert
              key={d.id}
              severity={d.severity === 'error' ? 'error' : d.severity === 'warning' ? 'warning' : 'info'}
              sx={{ mb: 1 }}
            >
              <AlertTitle>
                {t('details.diagnostics.' + d.reason_code, { defaultValue: d.reason_code })}
              </AlertTitle>
              {d.detail}
              {d.count > 1 && (
                <Typography variant="caption" display="block" sx={{ mt: 0.5, opacity: 0.8 }}>
                  {t('details.diagnostics.seenTimes', { count: d.count })}
                </Typography>
              )}
            </Alert>
          ))}
        </Box>
      )}

      {/* Job Information */}
      <Paper sx={{ mb: 3 }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h6">{t('details.jobInformation')}</Typography>
        </Box>
        <SimpleTable<InfoRow>
          rows={infoRows}
          getRowKey={(r) => r.key}
          dense={false}
          sx={{ '& thead': { display: 'none' } }}
          columns={[
            {
              field: 'label',
              headerName: '',
              width: '30%',
              render: (r) => <Box component="span" sx={{ fontWeight: 'bold' }}>{r.label}</Box>,
            },
            {
              field: 'value',
              headerName: '',
              render: (r) =>
                r.mono ? (
                  <Box component="span" sx={{ fontFamily: (theme) => theme.typography.monoFamily }}>
                    {r.value}
                  </Box>
                ) : (
                  r.value
                ),
            },
          ]}
        />
      </Paper>

      {/* Increment Layers Table */}
      {layers.length > 0 && (
        <Paper sx={{ mb: 3 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="h6">
              {t('details.incrementLayers', { count: layers.length })}
            </Typography>
          </Box>
          <DataTable<JobIncrementLayerWithStats>
            flat
            rows={layers}
            columns={layerColumns}
            getRowId={(r) => r.id}
            pagination={false}
            sorting={{ mode: 'client' }}
          />
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
          layers={layers}
        />
      </Paper>

      {/* Agent Performance Table */}
      <Paper>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h6">
            {t('details.activeTasks', { count: activeTasks.length })}
          </Typography>
        </Box>
        <DataTable<TaskRow>
          flat
          rows={activeTasks}
          columns={activeTaskColumns}
          getRowId={(r) => r.id}
          fetching={jobQuery.isFetching && !jobQuery.isLoading}
          pagination={activeTasks.length > 100 ? { mode: 'client', initialPageSize: 100 } : false}
          sorting={{ mode: 'client' }}
          emptyState={{ title: t('details.noActiveTasks') as string }}
        />
      </Paper>

      {/* Failed Tasks Table */}
      {failedTasks.length > 0 && (
        <Paper sx={{ mt: 3 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Typography variant="h6" color="error">
              {t('details.failedTasks', { count: failedTasks.length })}
            </Typography>
          </Box>
          <DataTable<TaskRow>
            flat
            rows={failedTasks}
            columns={failedTaskColumns}
            getRowId={(r) => r.id}
            pagination={{ mode: 'client', initialPageSize: 25 }}
            sorting={{ mode: 'client', initial: [{ field: 'updated_at', sort: 'desc' }] }}
            tableKey="job-failed-tasks"
          />
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
          <DataTable<TaskRow>
            flat
            rows={completedTasks}
            columns={completedTaskColumns}
            getRowId={(r) => r.id}
            pagination={{ mode: 'client', initialPageSize: 25, pageSizeOptions: [25, 50, 100] }}
            sorting={{ mode: 'client' }}
            emptyState={{ title: t('details.noCompletedTasks') as string }}
            tableKey="job-completed-tasks"
          />
        </Paper>
      )}

      {/* Benchmark blocklist — only renders when entries exist */}
      <BenchmarkBlocklistPanel jobId={jobData.id} />

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

      {/* Read-only here: the operator is inspecting the projection, not
          launching from this page, so confirming just closes the dialog. */}
      {jobData?.cloud_burst_enabled && (
        <CloudProjectionDialog
          open={projectionOpen}
          jobId={jobData.id}
          onClose={() => setProjectionOpen(false)}
          onConfirm={() => setProjectionOpen(false)}
        />
      )}
    </Box>
  );
};

export default JobDetails;