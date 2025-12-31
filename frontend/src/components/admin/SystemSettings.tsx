import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Alert,
  CircularProgress,
  Grid,
  Tooltip,
  IconButton,
  Switch,
  FormControlLabel,
} from '@mui/material';
import { Info as InfoIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { getMaxPriority, updateMaxPriority, getSystemSettings, updateSystemSetting } from '../../services/systemSettings';
import { MaxPriorityConfig, SystemSettingsFormData } from '../../types/systemSettings';

interface SystemSettingsProps {
  onSave?: (settings: SystemSettingsFormData) => Promise<void>;
  loading?: boolean;
}

const SystemSettings: React.FC<SystemSettingsProps> = ({ onSave, loading = false }) => {
  const [formData, setFormData] = useState<SystemSettingsFormData>({
    max_priority: 1000,
  });
  const [agentSchedulingEnabled, setAgentSchedulingEnabled] = useState(false);
  const [requireClientForHashlist, setRequireClientForHashlist] = useState(false);
  const [hashlistBatchSize, setHashlistBatchSize] = useState<number>(100000);
  const [potfileBatchSize, setPotfileBatchSize] = useState<number>(100000);
  const [potfileBatchInterval, setPotfileBatchInterval] = useState<number>(60);
  const [agentOverflowMode, setAgentOverflowMode] = useState<string>('fifo');
  const [loadingData, setLoadingData] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();

  useEffect(() => {
    loadSettings();
  }, []);

  const loadSettings = async () => {
    try {
      setLoadingData(true);
      const data = await getMaxPriority();
      setFormData({
        max_priority: data.max_priority,
      });
      
      // Load general system settings
      try {
        const settings = await getSystemSettings();
        const schedulingSetting = settings.data?.find((s: any) => s.key === 'agent_scheduling_enabled');
        if (schedulingSetting) {
          setAgentSchedulingEnabled(schedulingSetting.value === 'true');
        }
        const requireClientSetting = settings.data?.find((s: any) => s.key === 'require_client_for_hashlist');
        if (requireClientSetting) {
          setRequireClientForHashlist(requireClientSetting.value === 'true');
        }
        const hashlistBatchSizeSetting = settings.data?.find((s: any) => s.key === 'hashlist_bulk_batch_size');
        if (hashlistBatchSizeSetting) {
          setHashlistBatchSize(parseInt(hashlistBatchSizeSetting.value) || 100000);
        }
        const potfileBatchSizeSetting = settings.data?.find((s: any) => s.key === 'potfile_max_batch_size');
        if (potfileBatchSizeSetting) {
          setPotfileBatchSize(parseInt(potfileBatchSizeSetting.value) || 100000);
        }
        const potfileBatchIntervalSetting = settings.data?.find((s: any) => s.key === 'potfile_batch_interval');
        if (potfileBatchIntervalSetting) {
          setPotfileBatchInterval(parseInt(potfileBatchIntervalSetting.value) || 60);
        }
        const agentOverflowModeSetting = settings.data?.find((s: any) => s.key === 'agent_overflow_allocation_mode');
        if (agentOverflowModeSetting) {
          setAgentOverflowMode(agentOverflowModeSetting.value || 'fifo');
        }
      } catch (err) {
        console.error('Failed to load general settings:', err);
      }
      
      setError(null);
    } catch (error) {
      console.error('Failed to load system settings:', error);
      setError('Failed to load system settings');
    } finally {
      setLoadingData(false);
    }
  };

  const handleSave = async () => {
    if (typeof formData.max_priority === 'string' && formData.max_priority.trim() === '') {
      setError('Maximum priority is required');
      return;
    }

    const maxPriority = typeof formData.max_priority === 'string' 
      ? parseInt(formData.max_priority) 
      : formData.max_priority;

    if (isNaN(maxPriority) || maxPriority < 1) {
      setError('Maximum priority must be a positive number');
      return;
    }

    if (maxPriority > 1000000) {
      setError('Maximum priority cannot exceed 1,000,000');
      return;
    }

    try {
      setSaving(true);
      setError(null);
      
      if (onSave) {
        await onSave({ max_priority: maxPriority });
      } else {
        await updateMaxPriority(maxPriority);
      }
      
      enqueueSnackbar('System settings updated successfully', { variant: 'success' });
      
      // Reload settings to get the updated values
      await loadSettings();
    } catch (error: any) {
      console.error('Failed to save system settings:', error);
      
      // Handle specific error responses
      if (error.response?.status === 409) {
        const errorData = error.response.data;
        if (errorData.conflicting_jobs) {
          setError(`${errorData.message}\n\nConflicting preset jobs:\n${errorData.conflicting_jobs.join('\n')}`);
        } else {
          setError(errorData.message || 'Cannot update maximum priority due to conflicts');
        }
      } else {
        setError(error.response?.data?.message || error.message || 'Failed to save system settings');
      }
    } finally {
      setSaving(false);
    }
  };

  const handleMaxPriorityChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFormData(prev => ({
      ...prev,
      max_priority: e.target.value,
    }));
    setError(null);
  };

  if (loadingData) {
    return (
      <Box display="flex" justifyContent="center" p={4}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      {error && (
        <Alert severity="error" sx={{ mb: 3 }} style={{ whiteSpace: 'pre-line' }}>
          {error}
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  Priority Settings
                </Typography>
                <Tooltip title="Configure the maximum priority value that can be assigned to jobs and preset jobs. This helps maintain consistent priority ranges across your organization.">
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>
              
              <TextField
                fullWidth
                label="Maximum Job Priority"
                type="number"
                value={formData.max_priority}
                onChange={handleMaxPriorityChange}
                disabled={loading || saving}
                inputProps={{
                  min: 1,
                  max: 1000000,
                }}
                helperText="Set the maximum priority value (1-1,000,000). Jobs and preset jobs cannot exceed this priority."
                sx={{ mb: 3 }}
              />

              <Box display="flex" gap={2}>
                <Button
                  variant="contained"
                  onClick={handleSave}
                  disabled={loading || saving || loadingData}
                  startIcon={saving ? <CircularProgress size={20} /> : null}
                >
                  {saving ? 'Saving...' : 'Save Settings'}
                </Button>
                
                <Button
                  variant="outlined"
                  onClick={loadSettings}
                  disabled={loading || saving || loadingData}
                >
                  Reset
                </Button>
              </Box>
            </CardContent>
          </Card>
        </Grid>
        
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="h6" component="h3" gutterBottom>
                Priority System Information
              </Typography>
              
              <Typography variant="body2" color="text.secondary" paragraph>
                The priority system uses a range from 0 to your configured maximum priority. 
                Higher numbers indicate higher priority.
              </Typography>
              
              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>Current Maximum:</strong> {typeof formData.max_priority === 'string' ? formData.max_priority : formData.max_priority.toLocaleString()}
              </Typography>
              
              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>Note:</strong> You cannot set a maximum priority lower than any existing 
                preset job priorities. Update or remove high-priority preset jobs first if needed.
              </Typography>
              
              <Typography variant="body2" color="text.secondary">
                <strong>Recommended ranges by organization size:</strong>
                <br />• Small organization: 0-100
                <br />• Medium/large organization: 0-1,000
                <br />• Ridiculous workload organization: 0-10,000
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        
        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  Agent Scheduling
                </Typography>
                <Tooltip title="Enable or disable the agent scheduling system globally. When enabled, agents can have daily schedules configured.">
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>
              
              <FormControlLabel
                control={
                  <Switch
                    checked={agentSchedulingEnabled}
                    onChange={async (e) => {
                      const newValue = e.target.checked;
                      setAgentSchedulingEnabled(newValue);
                      try {
                        await updateSystemSetting('agent_scheduling_enabled', newValue.toString());
                        enqueueSnackbar('Agent scheduling setting updated', { variant: 'success' });
                      } catch (error) {
                        console.error('Failed to update scheduling setting:', error);
                        setAgentSchedulingEnabled(!newValue); // Revert on error
                        enqueueSnackbar('Failed to update scheduling setting', { variant: 'error' });
                      }
                    }}
                    disabled={loading || saving || loadingData}
                  />
                }
                label="Enable Agent Scheduling System"
              />
              
              <Typography variant="body2" color="text.secondary" paragraph sx={{ mt: 2 }}>
                When enabled, agents can be configured with daily schedules. Only agents that are scheduled 
                for the current time will be assigned jobs.
              </Typography>
              
              <Typography variant="body2" color="text.secondary">
                <strong>Note:</strong> Individual agents must also have scheduling enabled and schedules 
                configured for this to take effect.
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  Agent Overflow Allocation
                </Typography>
                <Tooltip title="Configure how agents beyond max_agents limits are allocated when jobs have the same priority">
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <FormControlLabel
                control={
                  <Switch
                    checked={agentOverflowMode === 'round_robin'}
                    onChange={async (e) => {
                      const newValue = e.target.checked ? 'round_robin' : 'fifo';
                      setAgentOverflowMode(newValue);
                      try {
                        await updateSystemSetting('agent_overflow_allocation_mode', newValue);
                        enqueueSnackbar('Agent overflow allocation mode updated', { variant: 'success' });
                      } catch (error) {
                        console.error('Failed to update overflow allocation mode:', error);
                        setAgentOverflowMode(agentOverflowMode === 'round_robin' ? 'fifo' : 'round_robin'); // Revert on error
                        enqueueSnackbar('Failed to update overflow allocation mode', { variant: 'error' });
                      }
                    }}
                    disabled={loading || saving || loadingData}
                  />
                }
                label={agentOverflowMode === 'round_robin' ? 'Round-Robin Mode' : 'FIFO Mode'}
              />

              <Typography variant="body2" color="text.secondary" paragraph sx={{ mt: 2 }}>
                <strong>FIFO Mode (Default):</strong> When multiple jobs at the same priority exceed their
                max_agents limits, the oldest job (created first) receives all extra agents.
              </Typography>

              <Typography variant="body2" color="text.secondary">
                <strong>Round-Robin Mode:</strong> Extra agents are distributed evenly across all jobs at
                the same priority, one agent at a time, ensuring fair allocation.
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  Hashlist Settings
                </Typography>
                <Tooltip title="Configure settings related to hashlist uploads and management">
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <FormControlLabel
                control={
                  <Switch
                    checked={requireClientForHashlist}
                    onChange={async (e) => {
                      const newValue = e.target.checked;
                      setRequireClientForHashlist(newValue);
                      try {
                        await updateSystemSetting('require_client_for_hashlist', newValue.toString());
                        enqueueSnackbar('Hashlist client requirement updated', { variant: 'success' });
                      } catch (error) {
                        console.error('Failed to update client requirement setting:', error);
                        setRequireClientForHashlist(!newValue); // Revert on error
                        enqueueSnackbar('Failed to update setting', { variant: 'error' });
                      }
                    }}
                    disabled={loading || saving || loadingData}
                  />
                }
                label="Require Client for Hashlists"
              />

              <Typography variant="body2" color="text.secondary" paragraph sx={{ mt: 2 }}>
                When enabled, users must assign a client when uploading new hashlists. This helps maintain
                better organization and tracking of hashlists by client.
              </Typography>

              <TextField
                fullWidth
                label="Bulk Import Batch Size"
                type="number"
                value={hashlistBatchSize}
                onChange={(e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  setHashlistBatchSize(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  if (newValue < 10000 || newValue > 2000000) {
                    enqueueSnackbar('Batch size must be between 10,000 and 2,000,000', { variant: 'warning' });
                    setHashlistBatchSize(100000);
                    return;
                  }
                  try {
                    await updateSystemSetting('hashlist_bulk_batch_size', newValue.toString());
                    enqueueSnackbar('Hashlist batch size updated', { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update batch size:', error);
                    enqueueSnackbar('Failed to update batch size', { variant: 'error' });
                    await loadSettings();
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{
                  min: 10000,
                  max: 2000000,
                  step: 50000,
                }}
                helperText="Number of hashes processed per batch during uploads. Default: 100,000. Recommended: 500,000-1,000,000 for large hashlists (47M+)"
                sx={{ mt: 2, mb: 2 }}
              />

              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>Performance Guide:</strong>
                <br />• 100K (default): Good for most use cases
                <br />• 500K: Better for large hashlists, may improve throughput
                <br />• 1M: Best for very large hashlists (50M+), requires more RAM
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box display="flex" alignItems="center" mb={2}>
                <Typography variant="h6" component="h3">
                  Potfile Settings
                </Typography>
                <Tooltip title="Configure how the potfile processes cracked passwords for reuse across jobs">
                  <IconButton size="small" sx={{ ml: 1 }}>
                    <InfoIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Box>

              <TextField
                fullWidth
                label="Maximum Batch Size"
                type="number"
                value={potfileBatchSize}
                onChange={async (e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  setPotfileBatchSize(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 100000;
                  if (newValue < 1000 || newValue > 500000) {
                    enqueueSnackbar('Batch size must be between 1,000 and 500,000', { variant: 'warning' });
                    setPotfileBatchSize(100000);
                    return;
                  }
                  try {
                    await updateSystemSetting('potfile_max_batch_size', newValue.toString());
                    enqueueSnackbar('Potfile batch size updated', { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update batch size:', error);
                    enqueueSnackbar('Failed to update batch size', { variant: 'error' });
                    await loadSettings(); // Reload to revert
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{
                  min: 1000,
                  max: 500000,
                }}
                helperText="Number of staged passwords to process in each batch cycle (1,000 - 500,000)"
                sx={{ mb: 2 }}
              />

              <TextField
                fullWidth
                label="Batch Interval (seconds)"
                type="number"
                value={potfileBatchInterval}
                onChange={async (e) => {
                  const newValue = parseInt(e.target.value) || 60;
                  setPotfileBatchInterval(newValue);
                }}
                onBlur={async (e) => {
                  const newValue = parseInt(e.target.value) || 60;
                  if (newValue < 5 || newValue > 600) {
                    enqueueSnackbar('Interval must be between 5 and 600 seconds', { variant: 'warning' });
                    setPotfileBatchInterval(60);
                    return;
                  }
                  try {
                    await updateSystemSetting('potfile_batch_interval', newValue.toString());
                    enqueueSnackbar('Potfile batch interval updated', { variant: 'success' });
                  } catch (error) {
                    console.error('Failed to update batch interval:', error);
                    enqueueSnackbar('Failed to update batch interval', { variant: 'error' });
                    await loadSettings(); // Reload to revert
                  }
                }}
                disabled={loading || saving || loadingData}
                inputProps={{
                  min: 10,
                  max: 600,
                }}
                helperText="Seconds between pot-file batch processing cycles (10 - 600)"
                sx={{ mb: 2 }}
              />

              <Typography variant="body2" color="text.secondary" paragraph>
                The potfile collects cracked passwords from all jobs and reuses them in subsequent attacks.
                Higher batch sizes process more passwords at once but take longer per cycle.
              </Typography>

              <Typography variant="body2" color="text.secondary">
                <strong>Processing Rate:</strong> {(potfileBatchSize / potfileBatchInterval).toLocaleString()} passwords/second
                <br />
                <strong>Current Settings:</strong> {potfileBatchSize.toLocaleString()} passwords every {potfileBatchInterval} seconds
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
};

export default SystemSettings; 