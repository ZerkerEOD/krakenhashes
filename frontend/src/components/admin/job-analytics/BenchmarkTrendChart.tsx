import React, { useState, useEffect } from 'react';
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
import {
  Box,
  Typography,
  ToggleButton,
  ToggleButtonGroup,
  Paper,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Skeleton,
  SelectChangeEvent,
} from '@mui/material';
import { format, parseISO } from 'date-fns';
import i18n from '../../../i18n';
import { dateFnsLocaleFor } from '../../../i18n/locales';

/** date-fns locale for the current UI language. */
const dateLocale = () => dateFnsLocaleFor(i18n.language);
import { BenchmarkHistoryEntry, JobAnalyticsFilterOptions } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';
import { ChartTooltipBox, useChartColors } from '../../ui/charts';

interface BenchmarkTrendChartProps {
  filterOptions: JobAnalyticsFilterOptions | undefined;
}

const formatSpeed = (value: number): string => {
  if (value >= 1e12) return `${(value / 1e12).toFixed(1)}TH/s`;
  if (value >= 1e9) return `${(value / 1e9).toFixed(1)}GH/s`;
  if (value >= 1e6) return `${(value / 1e6).toFixed(1)}MH/s`;
  if (value >= 1e3) return `${(value / 1e3).toFixed(1)}KH/s`;
  return `${Math.round(value)}H/s`;
};

const CustomTooltip = ({ active, payload, label }: any) => {
  const { t } = useTranslation('admin');
  if (active && payload && payload.length) {
    return (
      <ChartTooltipBox>
        <Typography variant="body2">
          {format(parseISO(label), 'MMM d, yyyy HH:mm', { locale: dateLocale() })}
        </Typography>
        <Typography variant="body2" sx={{ color: 'success.main' }}>
          {t('jobAnalytics.benchmarkChart.tooltipSpeed', { speed: formatSpeed(payload[0].value) })}
        </Typography>
      </ChartTooltipBox>
    );
  }
  return null;
};

const BenchmarkTrendChart: React.FC<BenchmarkTrendChartProps> = ({ filterOptions }) => {
  const { t } = useTranslation('admin');
  const colors = useChartColors();
  const [agentId, setAgentId] = useState<number | ''>('');
  const [hashType, setHashType] = useState<number | undefined>();
  const [attackMode, setAttackMode] = useState<number | undefined>();
  const [timeRange, setTimeRange] = useState('365d');
  const [data, setData] = useState<BenchmarkHistoryEntry[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (agentId === '') return;
    setLoading(true);
    jobAnalyticsService
      .getBenchmarkTrends(agentId as number, hashType, attackMode, timeRange)
      .then(res => {
        setData(res.points || []);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, [agentId, hashType, attackMode, timeRange]);

  return (
    <Paper sx={{ p: 2, mb: 3 }}>
      <Typography variant="h6" gutterBottom>{t('jobAnalytics.benchmarkChart.title')}</Typography>
      <Box sx={{ display: 'flex', gap: 2, mb: 2, flexWrap: 'wrap', alignItems: 'center' }}>
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel>{t('jobAnalytics.benchmarkChart.agent')}</InputLabel>
          <Select
            value={agentId === '' ? '' : String(agentId)}
            onChange={(e: SelectChangeEvent<string>) => {
              const val = e.target.value;
              setAgentId(val === '' ? '' : Number(val));
            }}
            label={t('jobAnalytics.benchmarkChart.agent')}
          >
            <MenuItem value="">{t('jobAnalytics.benchmarkChart.selectAgent')}</MenuItem>
            {filterOptions?.agents?.map(a => (
              <MenuItem key={a.id} value={String(a.id)}>{a.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel>{t('jobAnalytics.benchmarkChart.hashType')}</InputLabel>
          <Select
            value={hashType !== undefined ? String(hashType) : ''}
            onChange={(e: SelectChangeEvent<string>) => {
              const val = e.target.value;
              setHashType(val === '' ? undefined : Number(val));
            }}
            label={t('jobAnalytics.benchmarkChart.hashType')}
          >
            <MenuItem value="">{t('jobAnalytics.benchmarkChart.all')}</MenuItem>
            {filterOptions?.hash_types?.map(ht => (
              <MenuItem key={ht.id} value={String(ht.id)}>{ht.name} ({ht.id})</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>{t('jobAnalytics.benchmarkChart.attackMode')}</InputLabel>
          <Select
            value={attackMode !== undefined ? String(attackMode) : ''}
            onChange={(e: SelectChangeEvent<string>) => {
              const val = e.target.value;
              setAttackMode(val === '' ? undefined : Number(val));
            }}
            label={t('jobAnalytics.benchmarkChart.attackMode')}
          >
            <MenuItem value="">{t('jobAnalytics.benchmarkChart.all')}</MenuItem>
            {filterOptions?.attack_modes?.map(am => (
              <MenuItem key={am.value} value={String(am.value)}>{am.label}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <ToggleButtonGroup
          value={timeRange}
          exclusive
          onChange={(_, val) => val && setTimeRange(val)}
          size="small"
        >
          <ToggleButton value="90d">{t('jobAnalytics.benchmarkChart.range90d')}</ToggleButton>
          <ToggleButton value="365d">{t('jobAnalytics.benchmarkChart.range1y')}</ToggleButton>
          <ToggleButton value="all">{t('jobAnalytics.benchmarkChart.rangeAll')}</ToggleButton>
        </ToggleButtonGroup>
      </Box>

      {agentId === '' ? (
        <Box sx={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography color="text.secondary">{t('jobAnalytics.benchmarkChart.selectAgentPrompt')}</Typography>
        </Box>
      ) : loading ? (
        <Skeleton variant="rectangular" height={300} />
      ) : data.length === 0 ? (
        <Box sx={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography color="text.secondary">{t('jobAnalytics.benchmarkChart.empty')}</Typography>
        </Box>
      ) : (
        <ResponsiveContainer width="100%" height={300}>
          <LineChart data={data} margin={{ top: 5, right: 30, left: 20, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
            <XAxis
              dataKey="recorded_at"
              stroke={colors.axis}
              tickFormatter={(val) => {
                try {
                  return format(parseISO(val), 'MMM d', { locale: dateLocale() });
                } catch {
                  return val;
                }
              }}
            />
            <YAxis tickFormatter={formatSpeed} stroke={colors.axis} />
            <Tooltip content={<CustomTooltip />} />
            <Line
              type="monotone"
              dataKey="speed"
              name={t('jobAnalytics.benchmarkChart.speed') as string}
              stroke={colors.success}
              strokeWidth={2}
              dot={{ r: 3 }}
              activeDot={{ r: 5 }}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </Paper>
  );
};

export default BenchmarkTrendChart;
