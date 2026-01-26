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
      setError(err.response?.data?.error || 'Failed to fetch agent details');
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
      setSuccess(`Debug mode ${debugStatus?.enabled ? 'disabled' : 'enabled'} for agent`);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to toggle debug mode');
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

      setSuccess('Device status updated successfully');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to update device status');
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

      setSuccess(`Runtime updated to ${runtime} successfully`);
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to update device runtime');
      setTimeout(() => setError(''), 5000);
    }
  };

  // Scheduling handlers
  const handleToggleScheduling = async (enabled: boolean, timezone: string) => {
    try {
      await toggleAgentScheduling(agent!.id, enabled, timezone);
      setSchedulingEnabled(enabled);
      setScheduleTimezone(timezone);
      setSuccess('Scheduling settings updated');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to toggle scheduling');
    }
  };

  const handleUpdateSchedules = async (scheduleDTOs: AgentScheduleDTO[]) => {
    try {
      const result = await bulkUpdateAgentSchedules(agent!.id, scheduleDTOs);
      setSchedules(result.schedules);
      setSuccess('Schedules updated successfully');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to update schedules');
      throw err; // Re-throw to let the component handle it
    }
  };

  const handleDeleteSchedule = async (dayOfWeek: number) => {
    try {
      await deleteAgentSchedule(agent!.id, dayOfWeek);
      setSchedules(schedules.filter(s => s.dayOfWeek !== dayOfWeek));
      setSuccess('Schedule removed');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to delete schedule');
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
      setSuccess('Agent status updated');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      console.error('Failed to update agent status:', err);
      setError(err.response?.data?.error || 'Failed to update agent status');
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
      setSuccess('Agent owner updated');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to update agent owner');
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
        setSuccess('Extra parameters updated');
        setTimeout(() => setSuccess(''), 3000);
      } catch (err: any) {
        setError(err.response?.data?.error || 'Failed to update extra parameters');
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
        ? `Agent binary version set to ${newBinaryVersion}`
        : 'Agent binary reset to default');
      setTimeout(() => setSuccess(''), 3000);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to update binary configuration');
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
        <Alert severity="error">Agent not found</Alert>
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
          Agent Details
        </Typography>
      </Box>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Grid container spacing={3}>
        {/* Basic Information */}
        <Grid item xs={12} md={6}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>Basic Information</Typography>
            
            <Grid container spacing={2}>
              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">Agent ID</Typography>
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
                  label="Enabled"
                />
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary" gutterBottom>Agent Binary Configuration</Typography>
                <BinaryVersionSelector
                  value={binaryVersion}
                  onChange={handleBinaryChange}
                  label="Hashcat Binary"
                  size="small"
                  margin="none"
                  helperText={binaryVersion !== 'default'
                    ? `Agent will use ${binaryVersion} pattern`
                    : 'Using job or system default binary'}
                />
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">Last Activity</Typography>
                <Typography variant="body1">
                  {agent.metadata?.lastAction && agent.metadata?.lastActionTime ? (
                    <>
                      Action: {agent.metadata.lastAction}<br />
                      Time: {new Date(agent.metadata.lastActionTime).toLocaleString()}<br />
                      {agent.metadata.ipAddress && `IP: ${agent.metadata.ipAddress}`}
                    </>
                  ) : (
                    agent.lastHeartbeat ? 
                      formatDistanceToNow(new Date(agent.lastHeartbeat), { addSuffix: true }) :
                      'Never'
                  )}
                </Typography>
              </Grid>
              
              <Grid item xs={12}>
                <FormControl fullWidth>
                  <InputLabel>Owner</InputLabel>
                  <Select
                    value={ownerId}
                    onChange={(e) => handleOwnerChange(e.target.value)}
                    label="Owner"
                  >
                    <MenuItem value="">
                      <em>None</em>
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
            <Typography variant="h6" gutterBottom>System Information</Typography>

            <Grid container spacing={2}>
              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">Machine Name</Typography>
                <Typography variant="body1">{agent.osInfo?.hostname || agent.name}</Typography>
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">Operating System</Typography>
                <Typography variant="body1">
                  {agent.osInfo?.platform || 'Not detected'}
                </Typography>
              </Grid>

              <Grid item xs={12}>
                <Typography variant="body2" color="text.secondary">Agent Version</Typography>
                <Typography variant="body1">
                  {agent.version || 'Unknown'}
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
                <Typography variant="h6">Debug Configuration</Typography>
              </Box>
              <Button
                variant={debugStatus?.enabled ? 'outlined' : 'contained'}
                color={debugStatus?.enabled ? 'warning' : 'success'}
                startIcon={debugLoading ? <CircularProgress size={16} /> : <BugReportIcon />}
                onClick={handleToggleDebug}
                disabled={debugLoading}
                size="small"
              >
                {debugStatus?.enabled ? 'Disable Debug' : 'Enable Debug'}
              </Button>
            </Box>

            {debugStatus ? (
              <Grid container spacing={2}>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">Status</Typography>
                  <Chip
                    label={debugStatus.enabled ? 'Enabled' : 'Disabled'}
                    color={debugStatus.enabled ? 'success' : 'default'}
                    size="small"
                  />
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">Log Level</Typography>
                  <Typography variant="body1">{debugStatus.level}</Typography>
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">File Logging</Typography>
                  <Chip
                    label={debugStatus.file_logging_enabled ? 'Active' : 'Inactive'}
                    color={debugStatus.file_logging_enabled ? 'info' : 'default'}
                    size="small"
                    variant="outlined"
                  />
                </Grid>
                <Grid item xs={6} md={3}>
                  <Typography variant="body2" color="text.secondary">Buffer</Typography>
                  <Typography variant="body1">
                    {debugStatus.buffer_count} / {debugStatus.buffer_capacity}
                  </Typography>
                </Grid>
                {debugStatus.log_file_exists && (
                  <Grid item xs={12}>
                    <Typography variant="body2" color="text.secondary">Log File Size</Typography>
                    <Typography variant="body1">
                      {(debugStatus.log_file_size / 1024).toFixed(2)} KB
                    </Typography>
                  </Grid>
                )}
                <Grid item xs={12}>
                  <Typography variant="caption" color="text.secondary">
                    Last updated: {new Date(debugStatus.last_updated).toLocaleString()}
                  </Typography>
                </Grid>
              </Grid>
            ) : (
              <Typography color="text.secondary">
                Debug status not yet reported by agent. The agent will report its debug status after connecting.
              </Typography>
            )}
          </Paper>
        </Grid>

        {/* Hardware Configuration */}
        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="h6" gutterBottom>Hardware Configuration</Typography>
            
            {devices.length === 0 ? (
              <Typography color="text.secondary">No devices detected</Typography>
            ) : (
              <TableContainer>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>Device ID</TableCell>
                      <TableCell>Type</TableCell>
                      <TableCell>Name</TableCell>
                      <TableCell>Runtime</TableCell>
                      <TableCell>Specs</TableCell>
                      <TableCell>Enabled</TableCell>
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
            <Typography variant="h6" gutterBottom>Extra Parameters</Typography>
            <Typography variant="body2" color="text.secondary" gutterBottom>
              Agent-specific hashcat parameters (e.g., -d 1 -w 4 -O)
            </Typography>
            
            <TextField
              fullWidth
              value={extraParameters}
              onChange={(e) => handleExtraParametersChange(e.target.value)}
              placeholder="Enter hashcat parameters..."
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
                Device Monitoring
              </Typography>
              <Box sx={{ mb: 2 }}>
                <FormControl size="small">
                  <InputLabel>Time Range</InputLabel>
                  <Select
                    value={timeRange}
                    onChange={(e) => setTimeRange(e.target.value)}
                    label="Time Range"
                  >
                    <MenuItem value="10m">10 minutes</MenuItem>
                    <MenuItem value="20m">20 minutes</MenuItem>
                    <MenuItem value="1h">1 hour</MenuItem>
                    <MenuItem value="5h">5 hours</MenuItem>
                    <MenuItem value="24h">24 hours</MenuItem>
                  </Select>
                </FormControl>
              </Box>
            </Grid>
            
            {/* Temperature Chart */}
            <Grid item xs={12} md={6}>
              <Card>
                <CardContent>
                  <DeviceMetricsChart
                    title="Temperature"
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
                    title="Utilization"
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
                    title="Fan Speed"
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
                    title="Hash Rate"
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