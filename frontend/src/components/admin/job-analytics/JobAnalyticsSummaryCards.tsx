import React from 'react';
import { Grid, Paper, Typography, Box, Skeleton } from '@mui/material';
import {
  WorkOutline as JobsIcon,
  VpnKey as CracksIcon,
  Speed as SpeedIcon,
  Timer as DurationIcon,
} from '@mui/icons-material';
import { JobAnalyticsSummary } from '../../../types/jobAnalytics';

interface JobAnalyticsSummaryCardsProps {
  summary: JobAnalyticsSummary | undefined;
  loading: boolean;
}

const formatSpeed = (speed: number): string => {
  if (speed >= 1e12) return `${(speed / 1e12).toFixed(1)} TH/s`;
  if (speed >= 1e9) return `${(speed / 1e9).toFixed(1)} GH/s`;
  if (speed >= 1e6) return `${(speed / 1e6).toFixed(1)} MH/s`;
  if (speed >= 1e3) return `${(speed / 1e3).toFixed(1)} KH/s`;
  return `${Math.round(speed)} H/s`;
};

const formatDuration = (seconds: number): string => {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds % 3600) / 60);
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
  }
  const d = Math.floor(seconds / 86400);
  const h = Math.round((seconds % 86400) / 3600);
  return h > 0 ? `${d}d ${h}h` : `${d}d`;
};

const formatNumber = (n: number): string => {
  return n.toLocaleString();
};

interface CardData {
  title: string;
  value: string;
  subtitle: string;
  icon: React.ReactNode;
  color: string;
}

const JobAnalyticsSummaryCards: React.FC<JobAnalyticsSummaryCardsProps> = ({ summary, loading }) => {
  const cards: CardData[] = summary ? [
    {
      title: 'Total Jobs',
      value: formatNumber(summary.total_jobs),
      subtitle: `${formatNumber(summary.completed_jobs)} completed, ${formatNumber(summary.failed_jobs)} failed`,
      icon: <JobsIcon sx={{ fontSize: 40 }} />,
      color: '#1976d2',
    },
    {
      title: 'Total Cracks',
      value: formatNumber(summary.total_cracks),
      subtitle: `Across all filtered jobs`,
      icon: <CracksIcon sx={{ fontSize: 40 }} />,
      color: '#2e7d32',
    },
    {
      title: 'Avg Speed',
      value: formatSpeed(summary.average_speed),
      subtitle: 'Average hash rate per job',
      icon: <SpeedIcon sx={{ fontSize: 40 }} />,
      color: '#ed6c02',
    },
    {
      title: 'Avg Duration',
      value: formatDuration(summary.average_duration_seconds),
      subtitle: `${formatNumber(summary.total_keyspace_processed)} keys processed`,
      icon: <DurationIcon sx={{ fontSize: 40 }} />,
      color: '#9c27b0',
    },
  ] : [];

  if (loading) {
    return (
      <Grid container spacing={2} sx={{ mb: 3 }}>
        {[0, 1, 2, 3].map(i => (
          <Grid item xs={12} sm={6} md={3} key={i}>
            <Paper sx={{ p: 2 }}>
              <Skeleton variant="text" width="60%" />
              <Skeleton variant="text" width="40%" height={40} />
              <Skeleton variant="text" width="80%" />
            </Paper>
          </Grid>
        ))}
      </Grid>
    );
  }

  return (
    <Grid container spacing={2} sx={{ mb: 3 }}>
      {cards.map((card) => (
        <Grid item xs={12} sm={6} md={3} key={card.title}>
          <Paper sx={{ p: 2 }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <Box>
                <Typography variant="body2" color="text.secondary">{card.title}</Typography>
                <Typography variant="h5" sx={{ fontWeight: 600, my: 0.5 }}>{card.value}</Typography>
                <Typography variant="caption" color="text.secondary">{card.subtitle}</Typography>
              </Box>
              <Box sx={{ color: card.color, opacity: 0.7 }}>
                {card.icon}
              </Box>
            </Box>
          </Paper>
        </Grid>
      ))}
    </Grid>
  );
};

export default JobAnalyticsSummaryCards;
