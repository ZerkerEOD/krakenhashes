import React, { useState, useEffect, useCallback, useRef } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Grid,
  Slider,
  Switch,
  FormControlLabel,
  Alert,
  CircularProgress,
  InputAdornment,
} from '@mui/material';
import { useSnackbar } from 'notistack';
import { api } from '../../services/api';

interface AgentUpdateSettingsData {
  agent_auto_update_enabled: boolean;
  agent_update_max_concurrent: number;
  agent_update_health_timeout_seconds: number;
  agent_update_max_attempts: number;
}

const DEFAULTS: AgentUpdateSettingsData = {
  agent_auto_update_enabled: true,
  agent_update_max_concurrent: 2,
  agent_update_health_timeout_seconds: 300,
  agent_update_max_attempts: 3,
};

const AgentAutoUpdateSettings: React.FC = () => {
  const { enqueueSnackbar } = useSnackbar();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<AgentUpdateSettingsData>(DEFAULTS);
  const settingsRef = useRef<AgentUpdateSettingsData>(settings);

  useEffect(() => {
    fetchSettings();
  }, []);

  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  const fetchSettings = async () => {
    try {
      const response = await api.get<AgentUpdateSettingsData>('/api/admin/settings/agent-update');
      setSettings(response.data);
    } catch (error) {
      console.error('Failed to fetch agent update settings:', error);
      enqueueSnackbar('Failed to load auto-update settings', { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const saveSettings = useCallback(async (updated: AgentUpdateSettingsData) => {
    setSaving(true);
    try {
      await api.put('/api/admin/settings/agent-update', updated);
      enqueueSnackbar('Auto-update settings saved', { variant: 'success' });
    } catch (error) {
      console.error('Failed to update auto-update settings:', error);
      enqueueSnackbar('Failed to save auto-update settings', { variant: 'error' });
      await fetchSettings();
    } finally {
      setSaving(false);
    }
  }, [enqueueSnackbar]);

  const handleToggle = (event: React.ChangeEvent<HTMLInputElement>) => {
    const updated = { ...settingsRef.current, agent_auto_update_enabled: event.target.checked };
    setSettings(updated);
    saveSettings(updated);
  };

  const handleNumberChange = (field: keyof AgentUpdateSettingsData) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const value = parseInt(event.target.value, 10);
    if (!isNaN(value)) {
      setSettings(prev => ({ ...prev, [field]: value }));
    }
  };

  const handleSliderChange = (field: keyof AgentUpdateSettingsData) => (
    _event: Event,
    value: number | number[]
  ) => {
    setSettings(prev => ({ ...prev, [field]: value as number }));
  };

  const handleSliderCommit = useCallback((field: keyof AgentUpdateSettingsData) => (
    _event: React.SyntheticEvent | Event,
    value: number | number[]
  ) => {
    saveSettings({ ...settingsRef.current, [field]: value as number });
  }, [saveSettings]);

  const handleBlurSave = useCallback(async () => {
    await saveSettings(settingsRef.current);
  }, [saveSettings]);

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="200px">
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ mt: 4 }}>
      <Typography variant="h6" gutterBottom>
        Agent Auto-Update
      </Typography>
      <Typography variant="body2" color="text.secondary" gutterBottom sx={{ mb: 3 }}>
        Control whether stale agents (those running an older version than the cluster expects) are
        automatically updated to the latest binary by their launcher. Updates only run while an agent
        is idle and never interrupt a running job.
      </Typography>

      <FormControlLabel
        control={
          <Switch
            checked={settings.agent_auto_update_enabled}
            onChange={handleToggle}
            disabled={saving}
          />
        }
        label="Enable automatic agent updates"
        sx={{ mb: 2 }}
      />

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                Max concurrent updates
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                How many agents may update at once. Caps the server's binary-serving bandwidth.
              </Typography>
              <Box sx={{ px: 2, pt: 2 }}>
                <Slider
                  value={settings.agent_update_max_concurrent}
                  onChange={handleSliderChange('agent_update_max_concurrent')}
                  onChangeCommitted={handleSliderCommit('agent_update_max_concurrent')}
                  valueLabelDisplay="on"
                  step={1}
                  marks
                  min={1}
                  max={10}
                  disabled={saving || !settings.agent_auto_update_enabled}
                />
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                Max attempts
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                Give up after this many failed update attempts (the agent stays on its current version).
              </Typography>
              <Box sx={{ px: 2, pt: 2 }}>
                <Slider
                  value={settings.agent_update_max_attempts}
                  onChange={handleSliderChange('agent_update_max_attempts')}
                  onChangeCommitted={handleSliderCommit('agent_update_max_attempts')}
                  valueLabelDisplay="on"
                  step={1}
                  marks
                  min={1}
                  max={10}
                  disabled={saving || !settings.agent_auto_update_enabled}
                />
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                Health-check timeout
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                How long an agent may stay in the updating state before the update is declared failed.
              </Typography>
              <TextField
                type="number"
                value={settings.agent_update_health_timeout_seconds}
                onChange={handleNumberChange('agent_update_health_timeout_seconds')}
                onBlur={handleBlurSave}
                fullWidth
                size="small"
                InputProps={{ endAdornment: <InputAdornment position="end">seconds</InputAdornment> }}
                inputProps={{ min: 60, max: 3600 }}
                helperText="Between 60 and 3600 seconds"
                disabled={saving || !settings.agent_auto_update_enabled}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12}>
          <Alert severity="info">
            Changes take effect immediately. Stale agents are updated as they become idle, respecting the
            concurrency cap above.
          </Alert>
        </Grid>
      </Grid>
    </Box>
  );
};

export default AgentAutoUpdateSettings;
