import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Typography, Box, Button, Alert, CircularProgress } from '@mui/material';
import { ArrowBack as ArrowBackIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';
import { api } from '../services/api';

interface JobExecution {
  id: string;
  name: string;
}

export default function PotJob() {
  const { t } = useTranslation('pot');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [jobName, setJobName] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadJobInfo = async () => {
      if (!id) return;

      try {
        setLoading(true);
        const response = await api.get<JobExecution>(`/api/jobs/${id}`);
        setJobName(response.data.name);
      } catch (err) {
        console.error('Error loading job info:', err);
        setError(t('errors.loadJobFailed') as string);
      } finally {
        setLoading(false);
      }
    };

    loadJobInfo();
  }, [id, t]);

  const fetchData = async (limit: number, offset: number, search?: string) => {
    if (!id) throw new Error('No job ID provided');
    return await potService.getPotByJob(id, { limit, offset, search });
  };

  const handleBack = () => {
    navigate('/pot');
  };

  if (loading) {
    return (
      <Box sx={{ p: 3 }}>
        <Box display="flex" justifyContent="center" alignItems="center" minHeight={400}>
          <CircularProgress />
        </Box>
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">{error}</Alert>
        <Box sx={{ mt: 2 }}>
          <Button startIcon={<ArrowBackIcon />} onClick={handleBack}>
            {t('navigation.backToAll') as string}
          </Button>
        </Box>
      </Box>
    );
  }

  const displayName = jobName || t('job.defaultName', { id }) as string;

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ mb: 3 }}>
        <Button
          startIcon={<ArrowBackIcon />}
          onClick={handleBack}
          sx={{ mb: 2 }}
        >
          {t('navigation.backToAll') as string}
        </Button>

        <Typography variant="h4" component="h1" gutterBottom>
          {t('job.title', { jobName: displayName }) as string}
        </Typography>
        <Typography variant="body1" color="text.secondary">
          {t('job.description') as string}
        </Typography>
      </Box>

      <PotTable
        title={t('job.tableTitle', { jobName: displayName }) as string}
        fetchData={fetchData}
        contextType="job"
        contextName={displayName}
      />
    </Box>
  );
}