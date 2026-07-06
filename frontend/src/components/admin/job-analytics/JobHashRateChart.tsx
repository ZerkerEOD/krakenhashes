import React from 'react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';
import { Box, Typography, ToggleButton, ToggleButtonGroup, Paper, Skeleton } from '@mui/material';
import { format, parseISO } from 'date-fns';
import { TimelinePoint } from '../../../types/jobAnalytics';

interface JobHashRateChartProps {
  data: TimelinePoint[] | undefined;
  loading: boolean;
  resolution: string;
  onResolutionChange: (resolution: string) => void;
}

const formatSpeed = (value: number): string => {
  if (value >= 1e12) return `${(value / 1e12).toFixed(1)}TH/s`;
  if (value >= 1e9) return `${(value / 1e9).toFixed(1)}GH/s`;
  if (value >= 1e6) return `${(value / 1e6).toFixed(1)}MH/s`;
  if (value >= 1e3) return `${(value / 1e3).toFixed(1)}KH/s`;
  return `${Math.round(value)}H/s`;
};

const JobHashRateChart: React.FC<JobHashRateChartProps> = ({
  data,
  loading,
  resolution,
  onResolutionChange,
}) => {
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      const point = payload[0].payload;
      return (
        <Box
          sx={{
            backgroundColor: 'rgba(30, 30, 30, 0.95)',
            border: '2px solid #d32f2f',
            borderRadius: 1,
            p: 1.5,
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.5)',
          }}
        >
          <Typography variant="body2" sx={{ color: '#fff', fontWeight: 500 }}>
            {format(parseISO(label), 'MMM d, yyyy')}
          </Typography>
          <Typography variant="body2" sx={{ color: '#ef5350' }}>
            Hash Rate: {formatSpeed(payload[0].value)}
          </Typography>
          {point.job_count !== undefined && (
            <Typography variant="body2" sx={{ color: 'rgba(255, 255, 255, 0.7)' }}>
              Jobs: {point.job_count}
            </Typography>
          )}
        </Box>
      );
    }
    return null;
  };
  if (loading) {
    return (
      <Paper sx={{ p: 2, mb: 3 }}>
        <Skeleton variant="text" width={200} />
        <Skeleton variant="rectangular" height={300} sx={{ mt: 1 }} />
      </Paper>
    );
  }

  const hasData = data && data.length > 0;

  return (
    <Paper sx={{ p: 2, mb: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="h6">Hash Rate Timeline</Typography>
        <ToggleButtonGroup
          value={resolution}
          exclusive
          onChange={(_, val) => val && onResolutionChange(val)}
          size="small"
        >
          <ToggleButton value="daily">Daily</ToggleButton>
          <ToggleButton value="weekly">Weekly</ToggleButton>
        </ToggleButtonGroup>
      </Box>
      {!hasData ? (
        <Box sx={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography color="text.secondary">No timeline data available for the selected filters</Typography>
        </Box>
      ) : (
        <ResponsiveContainer width="100%" height={300}>
          <LineChart data={data} margin={{ top: 5, right: 30, left: 60, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis
              dataKey="timestamp"
              tickFormatter={(val) => {
                try {
                  return format(parseISO(val), resolution === 'daily' ? 'MMM d' : 'MMM d');
                } catch {
                  return val;
                }
              }}
            />
            <YAxis tickFormatter={formatSpeed} />
            <Tooltip content={<CustomTooltip />} />
            <Line
              type="monotone"
              dataKey="value"
              name="Hash Rate"
              stroke="#1976d2"
              strokeWidth={2}
              dot={{ r: 3 }}
              activeDot={{ r: 5 }}
              connectNulls={false}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </Paper>
  );
};

export default JobHashRateChart;
