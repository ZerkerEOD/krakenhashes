import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Grid,
  Slider,
  Alert,
  CircularProgress,
  Tooltip,
  InputAdornment
} from '@mui/material';
import { Save as SaveIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';

interface AgentDownloadSettings {
  max_concurrent_downloads: number;
  download_timeout_minutes: number;
  download_retry_attempts: number;
  progress_interval_seconds: number;
  chunk_size_mb: number;
}

const AgentDownloadSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<AgentDownloadSettings>({
    max_concurrent_downloads: 3,
    download_timeout_minutes: 60,
    download_retry_attempts: 3,
    progress_interval_seconds: 10,
    chunk_size_mb: 10
  });

  useEffect(() => {
    fetchSettings();
  }, []);

  const fetchSettings = async () => {
    try {
      const response = await api.get<AgentDownloadSettings>('/api/admin/settings/agent-download');
      setSettings(response.data);
    } catch (error) {
      console.error('Failed to fetch agent download settings:', error);
      enqueueSnackbar(t('agentDownloads.errors.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await api.put('/api/admin/settings/agent-download', settings);
      enqueueSnackbar(t('agentDownloads.messages.updateSuccess') as string, { variant: 'success' });
    } catch (error) {
      console.error('Failed to update agent download settings:', error);
      enqueueSnackbar(t('agentDownloads.errors.saveFailed') as string, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  const handleChange = (field: keyof AgentDownloadSettings) => (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const value = parseInt(event.target.value, 10);
    if (!isNaN(value)) {
      setSettings(prev => ({ ...prev, [field]: value }));
    }
  };

  const handleSliderChange = (field: keyof AgentDownloadSettings) => (
    event: Event,
    value: number | number[]
  ) => {
    setSettings(prev => ({ ...prev, [field]: value as number }));
  };

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="400px">
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('agentDownloads.title')}
      </Typography>
      <Typography variant="body2" color="text.secondary" gutterBottom sx={{ mb: 3 }}>
        {t('agentDownloads.description')}
      </Typography>

      <Grid container spacing={3}>
        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('agentDownloads.concurrentDownloads.title')}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t('agentDownloads.concurrentDownloads.description')}
              </Typography>
              <Box sx={{ px: 2, pt: 2 }}>
                <Slider
                  value={settings.max_concurrent_downloads}
                  onChange={handleSliderChange('max_concurrent_downloads')}
                  valueLabelDisplay="on"
                  step={1}
                  marks
                  min={1}
                  max={10}
                />
              </Box>
              <TextField
                type="number"
                value={settings.max_concurrent_downloads}
                onChange={handleChange('max_concurrent_downloads')}
                fullWidth
                size="small"
                inputProps={{ min: 1, max: 10 }}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('agentDownloads.downloadTimeout.title')}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t('agentDownloads.downloadTimeout.description')}
              </Typography>
              <TextField
                type="number"
                value={settings.download_timeout_minutes}
                onChange={handleChange('download_timeout_minutes')}
                fullWidth
                size="small"
                InputProps={{
                  endAdornment: <InputAdornment position="end">{t('agentDownloads.downloadTimeout.unit')}</InputAdornment>,
                }}
                inputProps={{ min: 1, max: 1440 }}
                helperText={t('agentDownloads.downloadTimeout.helper')}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('agentDownloads.retryAttempts.title')}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t('agentDownloads.retryAttempts.description')}
              </Typography>
              <Box sx={{ px: 2, pt: 2 }}>
                <Slider
                  value={settings.download_retry_attempts}
                  onChange={handleSliderChange('download_retry_attempts')}
                  valueLabelDisplay="on"
                  step={1}
                  marks
                  min={0}
                  max={10}
                />
              </Box>
              <TextField
                type="number"
                value={settings.download_retry_attempts}
                onChange={handleChange('download_retry_attempts')}
                fullWidth
                size="small"
                inputProps={{ min: 0, max: 10 }}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('agentDownloads.progressInterval.title')}
              </Typography>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t('agentDownloads.progressInterval.description')}
              </Typography>
              <TextField
                type="number"
                value={settings.progress_interval_seconds}
                onChange={handleChange('progress_interval_seconds')}
                fullWidth
                size="small"
                InputProps={{
                  endAdornment: <InputAdornment position="end">{t('agentDownloads.progressInterval.unit')}</InputAdornment>,
                }}
                inputProps={{ min: 1, max: 300 }}
                helperText={t('agentDownloads.progressInterval.helper')}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12} md={6}>
          <Card>
            <CardContent>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('agentDownloads.chunkSize.title')}
              </Typography>
              <Tooltip title={t('agentDownloads.chunkSize.description') as string}>
                <Typography variant="body2" color="text.secondary" gutterBottom>
                  {t('agentDownloads.chunkSize.description')}
                </Typography>
              </Tooltip>
              <TextField
                type="number"
                value={settings.chunk_size_mb}
                onChange={handleChange('chunk_size_mb')}
                fullWidth
                size="small"
                InputProps={{
                  endAdornment: <InputAdornment position="end">{t('agentDownloads.chunkSize.unit')}</InputAdornment>,
                }}
                inputProps={{ min: 1, max: 100 }}
                helperText={t('agentDownloads.chunkSize.helper')}
                sx={{ mt: 2 }}
              />
            </CardContent>
          </Card>
        </Grid>

        <Grid item xs={12}>
          <Alert severity="info" sx={{ mb: 2 }}>
            {t('agentDownloads.applyNote')}
          </Alert>

          <Box display="flex" justifyContent="flex-end">
            <Button
              variant="contained"
              color="primary"
              onClick={handleSave}
              disabled={saving}
              startIcon={saving ? <CircularProgress size={20} /> : <SaveIcon />}
            >
              {saving ? t('agentDownloads.buttons.saving') : t('agentDownloads.buttons.save')}
            </Button>
          </Box>
        </Grid>
      </Grid>
    </Box>
  );
};

export default AgentDownloadSettings;