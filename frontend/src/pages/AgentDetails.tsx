/**
 * Agent Details page component for KrakenHashes frontend.
 * 
 * Features:
 *   - Display detailed agent information
 *   - Enable/disable agent status
 *   - Manage agent devices (GPUs)
 *   - Set agent owner
 *   - Configure agent-specific hashcat parameters
 * 
 * @packageDocumentation
 */

import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Typography,
  Paper,
  Grid,
  Switch,
  FormControlLabel,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  TextField,
  Button,
  CircularProgress,
  Alert,
  Chip,
  Card,
  CardContent,
  LinearProgress,
} from '@mui/material';
import { BugReport as BugReportIcon } from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { DataTable, EntityLink, PageHeader, StatusChip, useToast } from '../components/ui';
import { useLiveQuery } from '../hooks/useLiveQuery';
import { qk } from '../services/queryKeys';
import { api } from '../services/api';
import { formatDistanceToNow } from 'date-fns';
import i18n from '../i18n';
import { dateFnsLocaleFor } from '../i18n/locales';
import { useAuth } from '../contexts/AuthContext';
import { useTeamFilter } from '../contexts/TeamFilterContext';
import DeviceMetricsChart from '../components/agent/DeviceMetricsChart';
import BinaryVersionSelector from '../components/common/BinaryVersionSelector';
import AgentScheduling from '../components/agent/AgentScheduling';
import {
  getAgentSchedules,
  toggleAgentScheduling,
  bulkUpdateAgentSchedules,
  deleteAgentSchedule
} from '../services/api';
import { AgentSchedule, AgentScheduleDTO } from '../types/scheduling';
import { AgentDevice } from '../types/agent';
import { formatAgentVersion } from '../utils/agentVersion';
import { AgentDebugStatus } from '../types/diagnostics';
import { getAgentDebugStatus, toggleAgentDebug } from '../services/diagnostics';

interface Agent {
  id: number;
  name: string;
  status: string;
  lastHeartbeat: string | null;
  version: string;
  osInfo: {
    platform?: string;
    hostname?: string;
    release?: string;
  };
  createdBy?: {
    id: string;
    username: string;
  };
  createdAt: string;
  apiKey?: string;
  /** Assigned owner's username (JOIN on owner_id); absent for system/ownerless agents. */
  ownerUsername?: string;
  metadata?: {
    lastAction?: string;
    lastActionTime?: string;
    ipAddress?: string;
    machineId?: string;
    teamId?: number;
  };
  ownerId?: string;
  extraParameters?: string;
  isEnabled?: boolean;
  /** Storage tier: full_cache | on_demand | network_direct (network-share feature) */
  storageTier?: string;
  /** Agent-side read-only mount path for the network_direct tier */
  networkShareMountPath?: string;
  /** Binary version pattern (e.g., "default", "7.x", "7.1.x", "7.1.2") */
  binaryVersion?: string;
  /** Auto-update: version-stale but busy; updates when it goes idle */
  updatePending?: boolean;
  /** Auto-update: the version this agent is being updated to */
  targetVersion?: string;
  /** Auto-update: last update failure message */
  updateError?: string;
}

interface User {
  id: string;
  username: string;
  email: string;
  role: string;
}

interface DiagnosticReason {
  id: number;
  reason_code: string;
  severity: string;
  detail: string;
  count: number;
  first_seen: string;
  last_seen: string;
}

interface AgentActivity {
  currentTask?: {
    id: string;
    job_execution_id: string;
    status: string;
    keyspace_start?: number;
    keyspace_end?: number;
    keyspace_processed?: number;
    progress_percent?: number;
    assigned_at?: string;
    started_at?: string;
    last_checkpoint?: string;
  };
  jobExecution?: {
    id: string;
    name: string;
    status: string;
    overall_progress_percent?: number;
    hash_type?: number;
    priority?: number;
  };
  diagnostics: DiagnosticReason[];
  recentFailures?: Array<{
    id: string;
    job_execution_id: string;
    status: string;
    error_message?: string;
    completed_at?: string;
  }>;
}

// Formats an optional 0-100 percentage for display, or an em dash when absent.
const formatPct = (v?: number): string => (v != null ? `${v.toFixed(2)}%` : '—');

interface DeviceData {
  deviceId: number;
  deviceName: string;
  metrics: {
    [metricType: string]: Array<{
      timestamp: number;
      value: number;
    }>;
  };
}

const SYSTEM_USER_ID = '00000000-0000-0000-0000-000000000000';

const AgentDetails: React.FC = () => {
  const { t } = useTranslation('agents');
  // Maps a diagnostic reason_code to a human-readable label for the agent page.
  const DIAG_REASON_LABELS: Record<string, string> = {
    no_compatible_job: t('diagReasons.noCompatibleJob') as string,
    blocklisted: t('diagReasons.blocklisted') as string,
    benchmarking: t('diagReasons.benchmarking') as string,
    outside_schedule: t('diagReasons.outsideSchedule') as string,
    agent_disabled: t('diagReasons.agentDisabled') as string,
    shutting_down: t('diagReasons.shuttingDown') as string,
    rejection_cooldown: t('diagReasons.rejectionCooldown') as string,
    no_schedulable_work: t('diagReasons.noSchedulableWork') as string,
    at_capacity: t('diagReasons.atCapacity') as string,
  };
  const { id } = useParams<{ id: string }>();
  const toast = useToast();
  const { userRole } = useAuth();
  const { teamsEnabled } = useTeamFilter();
  const canChangeOwner = !teamsEnabled || userRole === 'admin';
  const [agent, setAgent] = useState<Agent | null>(null);
  const [devices, setDevices] = useState<AgentDevice[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  
  // Monitoring state
  const [timeRange, setTimeRange] = useState('10m');
  
  // Use ref to store all metrics data to avoid re-renders
  const metricsDataRef = useRef<Map<number, DeviceData>>(new Map());
  const lastFetchTimeRef = useRef<number>(0);
  // The time range the accumulated metrics belong to; a change resets the buffer.
  const metricsRangeRef = useRef<string | null>(null);
  
  // Form state
  const [isEnabled, setIsEnabled] = useState(true);
  const [ownerId, setOwnerId] = useState('');
  const [extraParameters, setExtraParameters] = useState('');
  const [deviceStates, setDeviceStates] = useState<{ [key: number]: boolean }>({});
  
  // Scheduling state
  const [schedulingEnabled, setSchedulingEnabled] = useState(false);
  const [scheduleTimezone, setScheduleTimezone] = useState('UTC');
  const [schedules, setSchedules] = useState<AgentSchedule[]>([]);

  // Binary configuration state
  const [binaryVersion, setBinaryVersion] = useState<string>('default');

  // Storage tier (network-share feature)
  const [storageTier, setStorageTier] = useState<string>('full_cache');
  const [networkShareMountPath, setNetworkShareMountPath] = useState<string>('');

  // Debug configuration state (admin only)
  const [debugStatus, setDebugStatus] = useState<AgentDebugStatus | null>(null);
  const [debugLoading, setDebugLoading] = useState(false);

  // Runtime activity: current task/job + "why idle" diagnostics. Non-fatal:
  // on error the activity card just doesn't render.
  const { data: activityData } = useLiveQuery<AgentActivity>(
    {
      queryKey: qk.agents.activity(id ?? ''),
      queryFn: async () => (await api.get(`/api/agents/${id}/activity`)).data,
      enabled: !!id,
    },
    { tier: 'fast', enabled: !!id }
  );
  const activity: AgentActivity | null = activityData ?? null;

  useEffect(() => {
    fetchAgentDetails();
    if (canChangeOwner) {
      fetchUsers();
    }
  }, [id, canChangeOwner]);

  

  const fetchAgentDetails = async () => {
    try {
      setLoading(true);
      setError('');
      
      // Fetch agent details with devices
      const agentResponse = await api.get(`/api/agents/${id}/with-devices`);
      const agentData = agentResponse.data.agent;
      const devicesData = agentResponse.data.devices || [];
      
      setAgent(agentData);
      setDevices(devicesData);
      
      // Initialize form state
      setIsEnabled(agentData.isEnabled !== undefined ? agentData.isEnabled : true);
      setOwnerId(agentData.ownerId || '');
      setExtraParameters(agentData.extraParameters || '');
      setStorageTier(agentData.storageTier || 'full_cache');
      setNetworkShareMountPath(agentData.networkShareMountPath || '');
      setBinaryVersion(agentData.binaryVersion || 'default');
      
      // Initialize device states using device_id as the key
      const initialDeviceStates: { [key: number]: boolean } = {};
      devicesData.forEach((device: AgentDevice) => {
        initialDeviceStates[device.device_id] = device.enabled;
      });
      setDeviceStates(initialDeviceStates);
      
      // Fetch scheduling information
      try {
        const schedulingInfo = await getAgentSchedules(agentData.id);
        setSchedulingEnabled(schedulingInfo.schedulingEnabled);
        setScheduleTimezone(schedulingInfo.scheduleTimezone);
        setSchedules(schedulingInfo.schedules || []);
      } catch (err) {
        console.error('Failed to fetch agent schedules:', err);
        // Don't fail the whole page load if scheduling fetch fails
      }

      // Fetch debug status (admin only, non-blocking)
      fetchDebugStatus(agentData.id);

    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.fetchDetailsFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  const fetchUsers = async () => {
    try {
      const response = await api.get('/api/users');
      setUsers(response.data || []);
    } catch (err) {
      console.error('Failed to fetch users:', err);
    }
  };

  const fetchDebugStatus = async (agentId: number) => {
    try {
      const status = await getAgentDebugStatus(agentId);
      setDebugStatus(status);
    } catch (err) {
      // Debug status not available - agent may not have reported yet
      console.debug('Debug status not available for agent:', agentId);
      setDebugStatus(null);
    }
  };

  const handleToggleDebug = async () => {
    if (!agent) return;
    setDebugLoading(true);
    try {
      await toggleAgentDebug(agent.id, !debugStatus?.enabled);
      // Refresh debug status after a short delay to allow agent to respond
      setTimeout(() => fetchDebugStatus(agent.id), 1000);
      toast.success(debugStatus?.enabled
        ? t('messages.debugModeDisabled') as string
        : t('messages.debugModeEnabled') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.toggleDebugFailed') as string));
    } finally {
      setDebugLoading(false);
    }
  };
  
  // Helper to get time range in milliseconds
  const getTimeRangeMs = useCallback(() => {
    switch (timeRange) {
      case '10m': return 10 * 60 * 1000;
      case '20m': return 20 * 60 * 1000;
      case '1h': return 60 * 60 * 1000;
      case '5h': return 5 * 60 * 60 * 1000;
      case '24h': return 24 * 60 * 60 * 1000;
      default: return 10 * 60 * 1000;
    }
  }, [timeRange]);

  // Incremental metrics fetch: the first call for a time range pulls the whole
  // window, later polls only ask for points since the last fetch and merge them
  // into the buffer (trimming anything that fell out of the window).
  const fetchDeviceMetrics = useCallback(async (): Promise<DeviceData[]> => {
    if (metricsRangeRef.current !== timeRange) {
      metricsDataRef.current.clear();
      lastFetchTimeRef.current = 0;
      metricsRangeRef.current = timeRange;
    }
    const isInitialFetch = lastFetchTimeRef.current === 0;

    const params: any = {
      timeRange,
      metrics: 'temperature,utilization,fanspeed,hashrate'
    };
    if (!isInitialFetch) {
      params.since = new Date(lastFetchTimeRef.current).toISOString();
    }

    const response = await api.get(`/api/agents/${id}/metrics`, { params });

    if (response.data && response.data.devices) {
      const now = Date.now();
      const cutoffTime = now - getTimeRangeMs();

      response.data.devices.forEach((device: DeviceData) => {
        const existingDevice = metricsDataRef.current.get(device.deviceId);
        if (!existingDevice) {
          metricsDataRef.current.set(device.deviceId, device);
        } else {
          Object.keys(device.metrics).forEach(metricType => {
            if (!existingDevice.metrics[metricType]) {
              existingDevice.metrics[metricType] = [];
            }
            const newMetrics = device.metrics[metricType] || [];
            existingDevice.metrics[metricType].push(...newMetrics);
            existingDevice.metrics[metricType] = existingDevice.metrics[metricType]
              .filter(m => m.timestamp >= cutoffTime)
              .sort((a, b) => a.timestamp - b.timestamp);
          });
        }
      });

      lastFetchTimeRef.current = now;
    }
    return Array.from(metricsDataRef.current.values());
  }, [id, timeRange, getTimeRangeMs]);

  const metricsEnabled = !!id && !!agent && devices.length > 0;
  const { data: deviceMetricsData } = useLiveQuery<DeviceData[]>(
    {
      queryKey: qk.agents.metrics(id ?? '', timeRange),
      queryFn: fetchDeviceMetrics,
      enabled: metricsEnabled,
    },
    { tier: 'fast', enabled: metricsEnabled }
  );
  const deviceMetrics: DeviceData[] = deviceMetricsData ?? [];

  const handleToggleDevice = async (deviceId: number) => {
    try {
      const newState = !deviceStates[deviceId];
      await api.put(`/api/agents/${id}/devices/${deviceId}`, {
        enabled: newState
      });

      setDeviceStates(prev => ({
        ...prev,
        [deviceId]: newState
      }));

      toast.success(t('messages.deviceStatusUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.updateDeviceFailed') as string));
    }
  };

  const handleRuntimeChange = async (deviceId: number, runtime: string) => {
    try {
      await api.patch(`/api/agents/${id}/devices/${deviceId}/runtime`, {
        runtime: runtime
      });

      // Update local state
      setDevices(prevDevices =>
        prevDevices.map(device =>
          device.device_id === deviceId
            ? { ...device, selected_runtime: runtime }
            : device
        )
      );

      toast.success(t('messages.runtimeUpdated', { runtime }) as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.updateRuntimeFailed') as string));
    }
  };

  // Scheduling handlers
  const handleToggleScheduling = async (enabled: boolean, timezone: string) => {
    try {
      await toggleAgentScheduling(agent!.id, enabled, timezone);
      setSchedulingEnabled(enabled);
      setScheduleTimezone(timezone);
      toast.success(t('messages.schedulingSettingsUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.toggleSchedulingFailed') as string));
    }
  };

  const handleUpdateSchedules = async (scheduleDTOs: AgentScheduleDTO[]) => {
    try {
      const result = await bulkUpdateAgentSchedules(agent!.id, scheduleDTOs);
      setSchedules(result.schedules);
      toast.success(t('messages.schedulesUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.updateSchedulesFailed') as string));
      throw err; // Re-throw to let the component handle it
    }
  };

  const handleDeleteSchedule = async (dayOfWeek: number) => {
    try {
      await deleteAgentSchedule(agent!.id, dayOfWeek);
      setSchedules(schedules.filter(s => s.dayOfWeek !== dayOfWeek));
      toast.success(t('messages.scheduleRemoved') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.deleteScheduleFailed') as string));
    }
  };

  // Auto-save agent enabled status
  const handleIsEnabledChange = async (newValue: boolean) => {
    console.log('Updating agent enabled status to:', newValue);
    setIsEnabled(newValue);
    try {
      await api.put(`/api/agents/${id}`, {
        isEnabled: newValue,
        ownerId: ownerId || null,
        extraParameters: extraParameters.trim()
      });
      toast.success(t('messages.agentStatusUpdated') as string);
    } catch (err: any) {
      console.error('Failed to update agent status:', err);
      toast.error(err.response?.data?.error || (t('errors.updateAgentStatusFailed') as string));
      // Revert on error
      setIsEnabled(!newValue);
    }
  };

  // Auto-save owner change
  const handleOwnerChange = async (newOwnerId: string) => {
    const previousOwnerId = ownerId;
    setOwnerId(newOwnerId);
    try {
      await api.put(`/api/agents/${id}`, {
        isEnabled: isEnabled,
        ownerId: newOwnerId || null,
        extraParameters: extraParameters.trim()
      });
      toast.success(t('messages.agentOwnerUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.updateAgentOwnerFailed') as string));
      // Revert on error
      setOwnerId(previousOwnerId);
    }
  };

  // Auto-save extra parameters with debounce
  const [parametersSaving, setParametersSaving] = useState(false);
  const parametersTimeoutRef = useRef<NodeJS.Timeout>();
  
  const handleExtraParametersChange = (value: string) => {
    setExtraParameters(value);
    
    // Clear existing timeout
    if (parametersTimeoutRef.current) {
      clearTimeout(parametersTimeoutRef.current);
    }
    
    // Set new timeout for debounced save
    parametersTimeoutRef.current = setTimeout(async () => {
      setParametersSaving(true);
      try {
        await api.put(`/api/agents/${id}`, {
          isEnabled: isEnabled,
          ownerId: ownerId || null,
          extraParameters: value.trim()
        });
        toast.success(t('messages.extraParametersUpdated') as string);
      } catch (err: any) {
        toast.error(err.response?.data?.error || (t('errors.updateExtraParametersFailed') as string));
      } finally {
        setParametersSaving(false);
      }
    }, 1000); // 1 second debounce
  };

  // Handle binary configuration change
  const handleBinaryChange = async (newBinaryVersion: string) => {
    const oldVersion = binaryVersion;
    setBinaryVersion(newBinaryVersion);

    try {
      await api.put(`/api/agents/${id}`, {
        isEnabled: isEnabled,
        ownerId: ownerId || null,
        extraParameters: extraParameters.trim(),
        binaryVersion: newBinaryVersion
      });
      toast.success(newBinaryVersion !== 'default'
        ? t('messages.binaryVersionSet', { version: newBinaryVersion }) as string
        : t('messages.binaryResetToDefault') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('errors.updateBinaryFailed') as string));
      // Revert on error
      setBinaryVersion(oldVersion);
    }
  };

  // Persist the storage tier + mount path. Sends the other managed settings
  // alongside so the partial-PUT preserves them; the backend also preserves
  // tier/mount-path when omitted, so this is belt-and-suspenders.
  const saveStorageSettings = async (tier: string, mountPath: string) => {
    await api.put(`/api/agents/${id}`, {
      isEnabled,
      ownerId: ownerId || null,
      extraParameters: extraParameters.trim(),
      storageTier: tier,
      networkShareMountPath: mountPath,
    });
  };

  const handleStorageTierChange = async (newTier: string) => {
    const old = storageTier;
    setStorageTier(newTier);
    // network_direct requires a mount path. Don't save (and don't revert) when
    // switching to it with no path yet — just reveal the path field and let the
    // operator enter one; the save happens on the path field's blur
    // (handleMountPathSave). Saving here with an empty path would fail server-side
    // and the catch below would revert the tier, hiding the field again (the
    // "rebound").
    if (newTier === 'network_direct' && !networkShareMountPath.trim()) {
      toast.info(t('storage.messages.enterMountPathHint') as string);
      return;
    }
    try {
      await saveStorageSettings(newTier, networkShareMountPath);
      toast.success(t('storage.messages.tierUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('storage.messages.tierUpdateFailed') as string));
      setStorageTier(old);
    }
  };

  const handleMountPathSave = async () => {
    // Avoid firing an invalid save on blur before a path is entered.
    if (storageTier === 'network_direct' && !networkShareMountPath.trim()) {
      return;
    }
    try {
      await saveStorageSettings(storageTier, networkShareMountPath);
      toast.success(t('storage.messages.mountPathUpdated') as string);
    } catch (err: any) {
      toast.error(err.response?.data?.error || (t('storage.messages.mountPathUpdateFailed') as string));
    }
  };

  const deviceColumns: GridColDef<AgentDevice>[] = [
    { field: 'device_id', headerName: t('hardware.deviceId') as string, width: 100 },
    { field: 'device_type', headerName: t('hardware.type') as string, width: 110 },
    { field: 'device_name', headerName: t('hardware.name') as string, flex: 1, minWidth: 200 },
    {
      field: 'selected_runtime',
      headerName: t('hardware.runtime') as string,
      width: 170,
      renderCell: (p) => (
        <Box onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()} sx={{ py: 0.5 }}>
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <Select
              value={p.row.selected_runtime || ''}
              onChange={(e) => handleRuntimeChange(p.row.device_id, e.target.value)}
              displayEmpty
            >
              {p.row.runtime_options?.map((option) => (
                <MenuItem key={option.backend} value={option.backend}>
                  {option.backend} #{option.device_id}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </Box>
      ),
    },
    {
      field: 'specs',
      headerName: t('hardware.specs') as string,
      flex: 1,
      minWidth: 200,
      sortable: false,
      renderCell: (p) => {
        const opt = p.row.runtime_options?.find((o) => o.backend === p.row.selected_runtime);
        if (!opt) return null;
        return (
          <Box sx={{ py: 0.5 }}>
            <Typography variant="caption" display="block">
              {opt.processors} cores, {opt.clock} MHz, {opt.memory_total} MB
            </Typography>
            {opt.pci_address && (
              <Typography variant="caption" display="block" color="text.secondary">
                PCI {opt.pci_address}
              </Typography>
            )}
          </Box>
        );
      },
    },
    {
      field: 'enabled',
      headerName: t('fields.enabled') as string,
      width: 100,
      renderCell: (p) => (
        <Box onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          <Switch
            checked={deviceStates[p.row.device_id] || false}
            onChange={() => handleToggleDevice(p.row.device_id)}
            color="primary"
          />
        </Box>
      ),
    },
  ];

  if (loading) {
    return (
      <Box sx={{ p: 3, display: 'flex', justifyContent: 'center', alignItems: 'center', height: '50vh' }}>
        <CircularProgress />
      </Box>
    );
  }

  if (!agent) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">{t('errors.agentNotFound') as string}</Alert>
      </Box>
    );
  }


  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('details.title') as string}
        description={agent.name}
        backTo="/agents"
        status={<StatusChip entity="agent" status={agent.status} />}
      />

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {/* Current work / Why-idle: live view of what the agent is doing or why it isn't */}
      {activity && (
        <Card sx={{ mb: 3 }}>
          <CardContent>
            {activity.currentTask ? (
              <>
                <Typography variant="h6" gutterBottom>{t('activity.currentWork') as string}</Typography>
                <Grid container spacing={2}>
                  <Grid item xs={12} md={6}>
                    <Typography variant="body2" color="text.secondary">{t('activity.job') as string}</Typography>
                    <Typography variant="body1">
                      <EntityLink
                        type="job"
                        id={activity.currentTask.job_execution_id}
                        label={activity.jobExecution?.name || activity.currentTask.job_execution_id}
                      />
                    </Typography>
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.taskStatus') as string}</Typography>
                    <StatusChip entity="task" status={activity.currentTask.status} />
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.jobProgress') as string}</Typography>
                    <Typography variant="body1">{formatPct(activity.jobExecution?.overall_progress_percent)}</Typography>
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.taskProgress') as string}</Typography>
                    <Typography variant="body1">{formatPct(activity.currentTask.progress_percent)}</Typography>
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.keyspaceRange') as string}</Typography>
                    <Typography variant="body1">
                      {activity.currentTask.keyspace_start != null && activity.currentTask.keyspace_end != null
                        ? `${activity.currentTask.keyspace_start.toLocaleString()} – ${activity.currentTask.keyspace_end.toLocaleString()}`
                        : '—'}
                    </Typography>
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.started') as string}</Typography>
                    <Typography variant="body1">
                      {activity.currentTask.started_at
                        ? formatDistanceToNow(new Date(activity.currentTask.started_at), { addSuffix: true, locale: dateFnsLocaleFor(i18n.language) })
                        : '—'}
                    </Typography>
                  </Grid>
                  <Grid item xs={6} md={3}>
                    <Typography variant="body2" color="text.secondary">{t('activity.lastCheckpoint') as string}</Typography>
                    <Typography variant="body1">
                      {activity.currentTask.last_checkpoint
                        ? formatDistanceToNow(new Date(activity.currentTask.last_checkpoint), { addSuffix: true, locale: dateFnsLocaleFor(i18n.language) })
                        : '—'}
                    </Typography>
                  </Grid>
                </Grid>
              </>
            ) : (activity.recentFailures && activity.recentFailures.length > 0) ||
                (activity.diagnostics && activity.diagnostics.length > 0) ? (
              <>
                <Typography variant="h6" gutterBottom>{t('activity.whyNotWorking') as string}</Typography>
                {activity.recentFailures && activity.recentFailures.length > 0 && (
                  <Alert severity="error" sx={{ mb: 1 }}>
                    <Typography variant="body2" sx={{ fontWeight: 600 }}>
                      {t('activity.recentFailuresAlert', { count: activity.recentFailures.length })}
                    </Typography>
                    <Typography variant="body2">
                      {t('activity.lastFailedJob') as string}{' '}
                      <EntityLink
                        type="job"
                        id={activity.recentFailures[0].job_execution_id}
                        label={activity.recentFailures[0].job_execution_id.slice(0, 8)}
                        mono
                      />
                    </Typography>
                    {activity.recentFailures[0].error_message && (
                      <Typography variant="body2" sx={{ wordBreak: 'break-word' }}>
                        {activity.recentFailures[0].error_message}
                      </Typography>
                    )}
                    {activity.recentFailures[0].completed_at && (
                      <Typography variant="caption" color="text.secondary">
                        {t('activity.lastFailureTime', {
                          time: formatDistanceToNow(new Date(activity.recentFailures[0].completed_at), { addSuffix: true, locale: dateFnsLocaleFor(i18n.language) }),
                        })}
                      </Typography>
                    )}
                  </Alert>
                )}
                {activity.diagnostics.map((d) => (
                  <Alert
                    key={d.id}
                    severity={d.severity === 'error' ? 'error' : d.severity === 'warning' ? 'warning' : 'info'}
                    sx={{ mb: 1 }}
                  >
                    <Typography variant="body2" sx={{ fontWeight: 600 }}>
                      {DIAG_REASON_LABELS[d.reason_code] || d.reason_code}
                    </Typography>
                    {d.detail && <Typography variant="body2">{d.detail}</Typography>}
                    <Typography variant="caption" color="text.secondary">
                      {t('activity.lastSeenTime', { time: formatDistanceToNow(new Date(d.last_seen), { addSuffix: true, locale: dateFnsLocaleFor(i18n.language) }) })}
                      {d.count > 1 ? t('activity.occurrencesSuffix', { count: d.count, countDisplay: d.count.toLocaleString() }) : ''}
                    </Typography>
                  </Alert>
                ))}
              </>
            ) : (
              <>
                <Typography variant="h6" gutterBottom>{t('activity.currentWork') as string}</Typography>
                <Typography variant="body2" color="text.secondary">
                  {t('activity.idleNoDiagnostics') as string}
                </Typography>
              </>
            )}
          </CardContent>
        </Card>
      )}

      <Grid container spacing={3}>
        {/* Basic Information */}
        <Grid item xs={12} md={6}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>{t('sections.basicInfo') as string}</Typography>

            <Grid container spacing={2}>
              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">{t('fields.agentId') as string}</Typography>
                <Typography variant="body1">{agent.id}</Typography>
              </Grid>

              <Grid item xs={12}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={isEnabled}
                      onChange={(e) => handleIsEnabledChange(e.target.checked)}
                      color="primary"
                    />
                  }
                  label={t('fields.enabled') as string}
                />
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary" gutterBottom>{t('fields.binaryConfiguration') as string}</Typography>
                <BinaryVersionSelector
                  value={binaryVersion}
                  onChange={handleBinaryChange}
                  label={t('fields.hashcatBinary') as string}
                  size="small"
                  margin="none"
                  helperText={binaryVersion !== 'default'
                    ? t('messages.binaryVersionPattern', { version: binaryVersion }) as string
                    : t('messages.binaryDefault') as string}
                />
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary" gutterBottom>{t('storage.title') as string}</Typography>
                <TextField
                  select
                  fullWidth
                  size="small"
                  value={storageTier}
                  onChange={(e) => handleStorageTierChange(e.target.value)}
                  helperText={
                    storageTier === 'full_cache'
                      ? (t('storage.helperText.fullCache') as string)
                      : storageTier === 'on_demand'
                      ? (t('storage.helperText.onDemand') as string)
                      : (t('storage.helperText.networkDirect') as string)
                  }
                >
                  <MenuItem value="full_cache">{t('storage.tiers.fullCache') as string}</MenuItem>
                  <MenuItem value="on_demand">{t('storage.tiers.onDemand') as string}</MenuItem>
                  <MenuItem value="network_direct">{t('storage.tiers.networkDirect') as string}</MenuItem>
                </TextField>
                {storageTier === 'network_direct' && (
                  <TextField
                    fullWidth
                    size="small"
                    sx={{ mt: 2 }}
                    label={t('storage.mountPathLabel') as string}
                    value={networkShareMountPath}
                    onChange={(e) => setNetworkShareMountPath(e.target.value)}
                    onBlur={handleMountPathSave}
                    placeholder="/mnt/krakenhashes-share"
                    helperText={t('storage.mountPathHelper') as string}
                  />
                )}
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">{t('fields.lastActivity') as string}</Typography>
                <Typography variant="body1">
                  {agent.metadata?.lastAction && agent.metadata?.lastActionTime ? (
                    <>
                      {t('fields.action') as string}: {agent.metadata.lastAction}<br />
                      {t('fields.time') as string}: {new Date(agent.metadata.lastActionTime).toLocaleString()}<br />
                      {agent.metadata.ipAddress && `${t('fields.ip') as string}: ${agent.metadata.ipAddress}`}
                    </>
                  ) : (
                    agent.lastHeartbeat ?
                      formatDistanceToNow(new Date(agent.lastHeartbeat), { addSuffix: true, locale: dateFnsLocaleFor(i18n.language) }) :
                      t('common.never') as string
                  )}
                </Typography>
              </Grid>

              <Grid item xs={12}>
                {canChangeOwner ? (
                  <FormControl fullWidth>
                    <InputLabel>{t('fields.owner') as string}</InputLabel>
                    <Select
                      value={ownerId}
                      onChange={(e) => handleOwnerChange(e.target.value)}
                      label={t('fields.owner') as string}
                    >
                      <MenuItem value="">
                        <em>{t('common.none') as string}</em>
                      </MenuItem>
                      {users.map((user) => (
                        <MenuItem key={user.id} value={user.id}>
                          {user.username}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                ) : (
                  <>
                    <Typography variant="body2" color="text.secondary">{t('fields.owner') as string}</Typography>
                    <Typography variant="body1">
                      {agent.ownerId && agent.ownerId !== SYSTEM_USER_ID ? (
                        <EntityLink type="user" id={agent.ownerId} label={agent.ownerUsername || agent.ownerId} />
                      ) : (
                        t('fields.systemOwner', 'System') as string
                      )}
                    </Typography>
                    {agent.createdBy?.username && (
                      <Typography variant="caption" color="text.secondary">
                        {t('fields.registeredBy', { name: agent.createdBy.username, defaultValue: 'Registered by {{name}}' }) as string}
                      </Typography>
                    )}
                  </>
                )}
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* System Information */}
        <Grid item xs={12} md={6}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>{t('sections.systemInfo') as string}</Typography>

            <Grid container spacing={2}>
              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">{t('fields.machineName') as string}</Typography>
                <Typography variant="body1">{agent.osInfo?.hostname || agent.name}</Typography>
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">{t('fields.operatingSystem') as string}</Typography>
                <Typography variant="body1">
                  {agent.osInfo?.platform || (t('common.notDetected') as string)}
                </Typography>
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">{t('fields.agentVersion') as string}</Typography>
                <Typography variant="body1">
                  {agent.version ? formatAgentVersion(agent.version) : (t('common.unknown') as string)}
                </Typography>
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Update Status */}
        <Grid item xs={12} md={6}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>{t('status.updateStatus') as string}</Typography>

            <Box sx={{ mb: 2 }}>
              <StatusChip entity="agent" status={agent.status} />
            </Box>

            <Typography variant="body2" color="text.secondary">{t('status.currentVersion') as string}</Typography>
            <Typography variant="body1" sx={{ mb: 1 }}>{agent.version ? formatAgentVersion(agent.version) : (t('common.unknown') as string)}</Typography>

            {agent.status === 'updating' && agent.targetVersion && (
              <Box sx={{ mb: 1 }}>
                <Typography variant="body2" color="text.secondary">
                  {t('status.targetVersion') as string} {formatAgentVersion(agent.targetVersion)}
                </Typography>
                <LinearProgress sx={{ mt: 1 }} />
              </Box>
            )}

            {agent.updatePending && agent.status !== 'updating' && (
              <Chip label={t('status.updatePending') as string} color="warning" size="small" variant="outlined" sx={{ mt: 1 }} />
            )}

            {agent.updateError && (
              <Alert
                severity="error"
                sx={{ mt: 1 }}
                action={
                  <Button
                    color="inherit"
                    size="small"
                    onClick={async () => {
                      try {
                        await api.post(`/api/agents/${id}/retry-update`);
                        await fetchAgentDetails();
                      } catch (err) {
                        console.error('Failed to retry update:', err);
                      }
                    }}
                  >
                    {t('actions.retry', 'Retry') as string}
                  </Button>
                }
              >
                {agent.updateError}
              </Alert>
            )}
          </Paper>
        </Grid>

        {/* Debug Configuration (Admin Only) */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <BugReportIcon color={debugStatus?.enabled ? 'success' : 'disabled'} />
                <Typography variant="h6">{t('sections.debugConfiguration') as string}</Typography>
              </Box>
              <Button
                variant={debugStatus?.enabled ? 'outlined' : 'contained'}
                color={debugStatus?.enabled ? 'warning' : 'success'}
                startIcon={debugLoading ? <CircularProgress size={16} /> : <BugReportIcon />}
                onClick={handleToggleDebug}
                disabled={debugLoading}
                size="small"
              >
                {debugStatus?.enabled ? (t('buttons.disableDebug') as string) : (t('buttons.enableDebug') as string)}
              </Button>
            </Box>

            {debugStatus ? (
              <Grid container spacing={2}>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">{t('debug.status') as string}</Typography>
                  <Chip
                    label={debugStatus.enabled ? (t('debug.enabled') as string) : (t('debug.disabled') as string)}
                    color={debugStatus.enabled ? 'success' : 'default'}
                    size="small"
                  />
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">{t('debug.logLevel') as string}</Typography>
                  <Typography variant="body1">{debugStatus.level}</Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">{t('debug.fileLogging') as string}</Typography>
                  <Chip
                    label={debugStatus.file_logging_enabled ? (t('debug.active') as string) : (t('debug.inactive') as string)}
                    color={debugStatus.file_logging_enabled ? 'info' : 'default'}
                    size="small"
                    variant="outlined"
                  />
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">{t('debug.buffer') as string}</Typography>
                  <Typography variant="body1">
                    {debugStatus.buffer_count} / {debugStatus.buffer_capacity}
                  </Typography>
                </Grid>
                {debugStatus.log_file_exists && (
                  <Grid item xs={12}>
                    <Typography variant="body2" color="text.secondary">{t('debug.logFileSize') as string}</Typography>
                    <Typography variant="body1">
                      {(debugStatus.log_file_size / 1024).toFixed(2)} KB
                    </Typography>
                  </Grid>
                )}
                <Grid item xs={12}>
                  <Typography variant="caption" color="text.secondary">
                    {t('debug.lastUpdated') as string}: {new Date(debugStatus.last_updated).toLocaleString()}
                  </Typography>
                </Grid>
              </Grid>
            ) : (
              <Typography color="text.secondary">
                {t('messages.debugStatusNotReported') as string}
              </Typography>
            )}
          </Paper>
        </Grid>

        {/* Hardware Configuration */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>{t('sections.hardwareConfiguration') as string}</Typography>

            <DataTable<AgentDevice>
              flat
              rows={devices}
              columns={deviceColumns}
              getRowId={(r) => r.id}
              pagination={false}
              sorting={false}
              emptyState={{ title: t('messages.noDevicesDetected') as string }}
            />
          </Paper>
        </Grid>

        {/* Extra Parameters */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>{t('sections.extraParameters') as string}</Typography>
            <Typography variant="body2" color="text.secondary" gutterBottom>
              {t('messages.extraParametersDescription') as string}
            </Typography>

            <TextField
              fullWidth
              value={extraParameters}
              onChange={(e) => handleExtraParametersChange(e.target.value)}
              placeholder={t('placeholders.enterHashcatParameters') as string}
              variant="outlined"
              sx={{ mt: 2 }}
              InputProps={{
                endAdornment: parametersSaving && <CircularProgress size={20} />
              }}
            />
          </Paper>
        </Grid>

        {/* Scheduling */}
        <Grid item xs={12}>
          <AgentScheduling
            agentId={agent!.id}
            schedulingEnabled={schedulingEnabled}
            scheduleTimezone={scheduleTimezone}
            schedules={schedules}
            onToggleScheduling={handleToggleScheduling}
            onUpdateSchedules={handleUpdateSchedules}
            onDeleteSchedule={handleDeleteSchedule}
          />
        </Grid>

        
        {/* Device Monitoring Section */}
        {devices.length > 0 && (
          <>
            <Grid item xs={12}>
              <Typography variant="h5" sx={{ mt: 3, mb: 2 }}>
                {t('sections.deviceMonitoring') as string}
              </Typography>
              <Box sx={{ mb: 2 }}>
                <FormControl size="small">
                  <InputLabel>{t('monitoring.timeRange') as string}</InputLabel>
                  <Select
                    value={timeRange}
                    onChange={(e) => setTimeRange(e.target.value)}
                    label={t('monitoring.timeRange') as string}
                  >
                    <MenuItem value="10m">{t('monitoring.10minutes') as string}</MenuItem>
                    <MenuItem value="20m">{t('monitoring.20minutes') as string}</MenuItem>
                    <MenuItem value="1h">{t('monitoring.1hour') as string}</MenuItem>
                    <MenuItem value="5h">{t('monitoring.5hours') as string}</MenuItem>
                    <MenuItem value="24h">{t('monitoring.24hours') as string}</MenuItem>
                  </Select>
                </FormControl>
              </Box>
            </Grid>
            
            {/* Temperature Chart */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <DeviceMetricsChart
                    title={t('monitoring.temperature') as string}
                    metricType="temperature"
                    devices={deviceMetrics}
                    deviceStatuses={devices}
                    unit="°C"
                    yAxisDomain={[0, 100]}
                    timeRange={timeRange}
                  />
                </CardContent>
              </Card>
            </Grid>

            {/* Utilization Chart */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <DeviceMetricsChart
                    title={t('monitoring.utilization') as string}
                    metricType="utilization"
                    devices={deviceMetrics}
                    deviceStatuses={devices}
                    unit="%"
                    yAxisDomain={[0, 100]}
                    timeRange={timeRange}
                  />
                </CardContent>
              </Card>
            </Grid>

            {/* Fan Speed Chart */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <DeviceMetricsChart
                    title={t('monitoring.fanSpeed') as string}
                    metricType="fanspeed"
                    devices={deviceMetrics}
                    deviceStatuses={devices}
                    unit="%"
                    yAxisDomain={[0, 100]}
                    timeRange={timeRange}
                  />
                </CardContent>
              </Card>
            </Grid>

            {/* Hash Rate Chart */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <DeviceMetricsChart
                    title={t('monitoring.hashRate') as string}
                    metricType="hashrate"
                    devices={deviceMetrics}
                    deviceStatuses={devices}
                    unit=""
                    showCumulative={true}
                    timeRange={timeRange}
                  />
                </CardContent>
              </Card>
            </Grid>
          </>
        )}
      </Grid>
    </Box>
  );
};

export default AgentDetails;