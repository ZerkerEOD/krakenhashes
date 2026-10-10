import React from 'react';
import { useTranslation } from 'react-i18next';
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
import i18n from '../../../i18n';
import { dateFnsLocaleFor } from '../../../i18n/locales';

/** date-fns locale for the current UI language. */
const dateLocale = () => dateFnsLocaleFor(i18n.language);
import { TimelinePoint } from '../../../types/jobAnalytics';
import { ChartTooltipBox, useChartColors } from '../../ui/charts';

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
  const { t } = useTranslation('admin');
  const colors = useChartColors();
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      const point = payload[0].payload;
      return (
        <ChartTooltipBox>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>
            {format(parseISO(label), 'MMM d, yyyy', { locale: dateLocale() })}
          </Typography>
          <Typography variant="body2" sx={{ color: 'primary.main' }}>
            {t('jobAnalytics.hashRateChart.tooltipHashRate', { speed: formatSpeed(payload[0].value) })}
          </Typography>
          {point.job_count !== undefined && (
            <Typography variant="body2" color="text.secondary">
              {t('jobAnalytics.hashRateChart.tooltipJobs', { count: point.job_count })}
            </Typography>
          )}
        </ChartTooltipBox>
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
        <Typography variant="h6">{t('jobAnalytics.hashRateChart.title')}</Typography>
        <ToggleButtonGroup
          value={resolution}
          exclusive
          onChange={(_, val) => val && onResolutionChange(val)}
          size="small"
        >
          <ToggleButton value="daily">{t('jobAnalytics.hashRateChart.daily')}</ToggleButton>
          <ToggleButton value="weekly">{t('jobAnalytics.hashRateChart.weekly')}</ToggleButton>
        </ToggleButtonGroup>
      </Box>
      {!hasData ? (
        <Box sx={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography color="text.secondary">{t('jobAnalytics.hashRateChart.empty')}</Typography>
        </Box>
      ) : (
        <ResponsiveContainer width="100%" height={300}>
          <LineChart data={data} margin={{ top: 5, right: 30, left: 60, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
            <XAxis
              dataKey="timestamp"
              stroke={colors.axis}
              tickFormatter={(val) => {
                try {
                  return format(parseISO(val), resolution === 'daily' ? 'MMM d' : 'MMM d', { locale: dateLocale() });
                } catch {
                  return val;
                }
              }}
            />
            <YAxis tickFormatter={formatSpeed} stroke={colors.axis} />
            <Tooltip content={<CustomTooltip />} />
            <Line
              type="monotone"
              dataKey="value"
              name={t('jobAnalytics.hashRateChart.hashRate') as string}
              stroke={colors.primary}
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
