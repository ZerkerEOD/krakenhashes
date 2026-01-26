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
import { useParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Typography,
  Paper,
  Grid,
  Switch,
  FormControlLabel,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  TextField,
  Button,
  CircularProgress,
  Alert,
  IconButton,
  Chip,
  Card,
  CardContent,
} from '@mui/material';
import {
  CheckCircle as CheckCircleIcon,
  Cancel as CancelIcon,
  ArrowBack as ArrowBackIcon,
  BugReport as BugReportIcon,
} from '@mui/icons-material';
import { api } from '../services/api';
import { formatDistanceToNow } from 'date-fns';
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
  /** Binary version pattern (e.g., "default", "7.x", "7.1.x", "7.1.2") */
  binaryVersion?: string;
}

interface User {
  id: string;
  username: string;
  email: string;
  role: string;
}

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

const AgentDetails: React.FC = () => {
  const { t } = useTranslation('agents');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [devices, setDevices] = useState<AgentDevice[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  
  // Monitoring state
  const [deviceMetrics, setDeviceMetrics] = useState<DeviceData[]>([]);
  const [metricsLoading, setMetricsLoading] = useState(false);
  const [timeRange, setTimeRange] = useState('10m');
  const [metricsInterval, setMetricsInterval] = useState<NodeJS.Timeout | null>(null);
  
  // Use ref to store all metrics data to avoid re-renders
  const metricsDataRef = useRef<Map<number, DeviceData>>(new Map());
  const lastFetchTimeRef = useRef<number>(0);
  
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

  // Debug configuration state (admin only)
  const [debugStatus, setDebugStatus] = useState<AgentDebugStatus | null>(null);
  const [debugLoading, setDebugLoading] = useState(false);

  useEffect(() => {
    fetchAgentDetails();
    fetchUsers();
  }, [id]);
  
  // Fetch device metrics periodically
  useEffect(() => {
    if (agent && devices.length > 0) {
      // Clear data when time range changes
      metricsDataRef.current.clear();
      lastFetchTimeRef.current = 0;
      
      // Initial fetch
      fetchDeviceMetrics(true);
      
      // Set up interval for updates every 5 seconds
      const interval = setInterval(() => {
        fetchDeviceMetrics(false);
      }, 5000);
      
      setMetricsInterval(interval);
      
      // Cleanup on unmount or when dependencies change
      return () => {
        if (interval) {
          clearInterval(interval);
        }
      };
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent, devices, timeRange]);

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
      setSuccess(debugStatus?.enabled
        ? t('messages.debugModeDisabled') as string
        : t('messages.debugModeEnabled') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.toggleDebugFailed') as string));
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

  const fetchDeviceMetrics = useCallback(async (isInitialFetch = false) => {
    if (!id) return;
    
    try {
      // Only show loading on initial fetch
      if (isInitialFetch) {
        setMetricsLoading(true);
      }

      // For initial fetch or when time range changes, fetch all data
      // Otherwise, only fetch new data since last update
      const params: any = {
        timeRange,
        metrics: 'temperature,utilization,fanspeed,hashrate'
      };
      
      // If not initial fetch and we have a last fetch time, only get new data
      if (!isInitialFetch && lastFetchTimeRef.current > 0) {
        params.since = new Date(lastFetchTimeRef.current).toISOString();
      }
      
      const response = await api.get(`/api/agents/${id}/metrics`, { params });
      
      if (response.data && response.data.devices) {
        const now = Date.now();
        const timeWindowMs = getTimeRangeMs();
        const cutoffTime = now - timeWindowMs;
        
        // Process new data
        response.data.devices.forEach((device: DeviceData) => {
          const existingDevice = metricsDataRef.current.get(device.deviceId);
          
          if (!existingDevice) {
            // New device, add it
            metricsDataRef.current.set(device.deviceId, device);
          } else {
            // Merge metrics for existing device
            Object.keys(device.metrics).forEach(metricType => {
              if (!existingDevice.metrics[metricType]) {
                existingDevice.metrics[metricType] = [];
              }
              
              // Add new metrics
              const newMetrics = device.metrics[metricType] || [];
              existingDevice.metrics[metricType].push(...newMetrics);
              
              // Remove old metrics outside the time window
              existingDevice.metrics[metricType] = existingDevice.metrics[metricType]
                .filter(m => m.timestamp >= cutoffTime)
                .sort((a, b) => a.timestamp - b.timestamp);
            });
          }
        });
        
        // Update last fetch time
        lastFetchTimeRef.current = now;
        
        // Convert map to array and update state
        const updatedDevices = Array.from(metricsDataRef.current.values());
        setDeviceMetrics(updatedDevices);
      }
    } catch (err: any) {
      console.error('Failed to fetch device metrics:', err);
    } finally {
      if (isInitialFetch) {
        setMetricsLoading(false);
      }
    }
  }, [id, timeRange, getTimeRangeMs]);

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

      setSuccess(t('messages.deviceStatusUpdated') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.updateDeviceFailed') as string));
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

      setSuccess(t('messages.runtimeUpdated', { runtime }) as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.updateRuntimeFailed') as string));
      setTimeout(() => setError(''), 5000);
    }
  };

  // Scheduling handlers
  const handleToggleScheduling = async (enabled: boolean, timezone: string) => {
    try {
      await toggleAgentScheduling(agent!.id, enabled, timezone);
      setSchedulingEnabled(enabled);
      setScheduleTimezone(timezone);
      setSuccess(t('messages.schedulingSettingsUpdated') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.toggleSchedulingFailed') as string));
    }
  };

  const handleUpdateSchedules = async (scheduleDTOs: AgentScheduleDTO[]) => {
    try {
      const result = await bulkUpdateAgentSchedules(agent!.id, scheduleDTOs);
      setSchedules(result.schedules);
      setSuccess(t('messages.schedulesUpdated') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.updateSchedulesFailed') as string));
      throw err; // Re-throw to let the component handle it
    }
  };

  const handleDeleteSchedule = async (dayOfWeek: number) => {
    try {
      await deleteAgentSchedule(agent!.id, dayOfWeek);
      setSchedules(schedules.filter(s => s.dayOfWeek !== dayOfWeek));
      setSuccess(t('messages.scheduleRemoved') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.deleteScheduleFailed') as string));
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
      setSuccess(t('messages.agentStatusUpdated') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      console.error('Failed to update agent status:', err);
      setError(err.response?.data?.error || (t('errors.updateAgentStatusFailed') as string));
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
      setSuccess(t('messages.agentOwnerUpdated') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.updateAgentOwnerFailed') as string));
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
        setSuccess(t('messages.extraParametersUpdated') as string);
        setTimeout(() => setSuccess(''), 3000);
      } catch (err: any) {
        setError(err.response?.data?.error || (t('errors.updateExtraParametersFailed') as string));
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
      setSuccess(newBinaryVersion !== 'default'
        ? t('messages.binaryVersionSet', { version: newBinaryVersion }) as string
        : t('messages.binaryResetToDefault') as string);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || (t('errors.updateBinaryFailed') as string));
      // Revert on error
      setBinaryVersion(oldVersion);
    }
  };

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
      <Box mb={3}>
        <IconButton onClick={() => navigate('/agents')} sx={{ mr: 2 }}>
          <ArrowBackIcon />
        </IconButton>
        <Typography variant="h4" component="span">
          {t('details.title') as string}
        </Typography>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

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
                      formatDistanceToNow(new Date(agent.lastHeartbeat), { addSuffix: true }) :
                      t('common.never') as string
                  )}
                </Typography>
              </Grid>

              <Grid item xs={12}>
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
                  {agent.version || (t('common.unknown') as string)}
                </Typography>
              </Grid>
            </Grid>
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

            {devices.length === 0 ? (
              <Typography color="text.secondary">{t('messages.noDevicesDetected') as string}</Typography>
            ) : (
              <TableContainer>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>{t('hardware.deviceId') as string}</TableCell>
                      <TableCell>{t('hardware.type') as string}</TableCell>
                      <TableCell>{t('hardware.name') as string}</TableCell>
                      <TableCell>{t('hardware.runtime') as string}</TableCell>
                      <TableCell>{t('hardware.specs') as string}</TableCell>
                      <TableCell>{t('fields.enabled') as string}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {devices.map((device) => (
                      <TableRow key={device.id}>
                        <TableCell>{device.device_id}</TableCell>
                        <TableCell>{device.device_type}</TableCell>
                        <TableCell>{device.device_name}</TableCell>
                        <TableCell>
                          <FormControl size="small" sx={{ minWidth: 120 }}>
                            <Select
                              value={device.selected_runtime || ''}
                              onChange={(e) => handleRuntimeChange(device.device_id, e.target.value)}
                              displayEmpty
                            >
                              {device.runtime_options?.map((option) => (
                                <MenuItem key={option.backend} value={option.backend}>
                                  {option.backend} #{option.device_id}
                                </MenuItem>
                              ))}
                            </Select>
                          </FormControl>
                        </TableCell>
                        <TableCell>
                          {device.runtime_options?.find(opt => opt.backend === device.selected_runtime) && (
                            <Typography variant="caption" display="block">
                              {device.runtime_options.find(opt => opt.backend === device.selected_runtime)!.processors} cores,
                              {' '}{device.runtime_options.find(opt => opt.backend === device.selected_runtime)!.clock} MHz,
                              {' '}{device.runtime_options.find(opt => opt.backend === device.selected_runtime)!.memory_total} MB
                            </Typography>
                          )}
                        </TableCell>
                        <TableCell>
                          <Switch
                            checked={deviceStates[device.device_id] || false}
                            onChange={() => handleToggleDevice(device.device_id)}
                            color="primary"
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            )}
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