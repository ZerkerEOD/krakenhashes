import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Typography, Box, Button, Alert, CircularProgress } from '@mui/material';
import { ArrowBack as ArrowBackIcon } from '@mui/icons-material';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';
import { api } from '../services/api';

interface JobExecution {
  id: string;
  name: string;
}

export default function PotJob() {
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
        setError('Failed to load job information');
      } finally {
        setLoading(false);
      }
    };

    loadJobInfo();
  }, [id]);

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
            Back to All Cracked Hashes
          </Button>
        </Box>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ mb: 3 }}>
        <Button
          startIcon={<ArrowBackIcon />}
          onClick={handleBack}
          sx={{ mb: 2 }}
        >
          Back to All Cracked Hashes
        </Button>

        <Typography variant="h4" component="h1" gutterBottom>
          Cracked Hashes for Job: {jobName || `Job ${id}`}
        </Typography>
        <Typography variant="body1" color="text.secondary">
          View all successfully cracked password hashes from this job execution.
        </Typography>
      </Box>

      <PotTable
        title={`Cracked Hashes - ${jobName || `Job ${id}`}`}
        fetchData={fetchData}
        contextType="job"
        contextName={jobName || `Job ${id}`}
      />
    </Box>
  );
}