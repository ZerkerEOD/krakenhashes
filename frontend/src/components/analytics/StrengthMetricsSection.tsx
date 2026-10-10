/**
 * Strength metrics section showing entropy distribution and crack time estimates.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography, Box } from '@mui/material';
import { StrengthStats } from '../../types/analytics';
import { SimpleTable, SimpleColumn } from '../ui';

interface StrengthMetricsSectionProps {
  data: StrengthStats;
}

type CrackEstimate = StrengthStats['crack_time_estimates']['speed_50_percent'];

interface EntropyRow {
  key: string;
  level: string;
  range: string;
  count: number;
  percentage: number;
}

interface CrackRow {
  key: string;
  label: string;
  est: CrackEstimate;
}

const formatSpeed = (hps: number): string => {
  if (hps >= 1000000000) return `${(hps / 1000000000).toFixed(2)} GH/s`;
  if (hps >= 1000000) return `${(hps / 1000000).toFixed(2)} MH/s`;
  if (hps >= 1000) return `${(hps / 1000).toFixed(2)} KH/s`;
  return `${hps.toFixed(0)} H/s`;
};

export default function StrengthMetricsSection({ data }: StrengthMetricsSectionProps) {
  const { t } = useTranslation('analytics');

  const entropyRows: EntropyRow[] = (['low', 'moderate', 'high'] as const).map((lvl) => ({
    key: lvl,
    level: t(`entropyLevels.${lvl}`),
    range: t(`entropyRanges.${lvl}`),
    count: data.entropy_distribution[lvl].count,
    percentage: data.entropy_distribution[lvl].percentage,
  }));

  const entropyColumns: SimpleColumn<EntropyRow>[] = [
    { field: 'level', headerName: t('columns.entropyLevel') },
    { field: 'range', headerName: t('columns.range') },
    { field: 'count', headerName: t('columns.count'), align: 'right', render: (r) => r.count.toLocaleString() },
    { field: 'percentage', headerName: t('columns.percentage'), align: 'right', render: (r) => `${r.percentage.toFixed(2)}%` },
  ];

  const est = data.crack_time_estimates;
  const crackRows: CrackRow[] = [
    { key: '50', label: t('speedLevels.speed50'), est: est.speed_50_percent },
    { key: '75', label: t('speedLevels.speed75'), est: est.speed_75_percent },
    { key: '100', label: t('speedLevels.speed100'), est: est.speed_100_percent },
    { key: '150', label: t('speedLevels.speed150'), est: est.speed_150_percent },
    { key: '200', label: t('speedLevels.speed200'), est: est.speed_200_percent },
  ];

  const pct = (field: keyof CrackEstimate, header: string): SimpleColumn<CrackRow> => ({
    field,
    headerName: header,
    align: 'right',
    render: (r) => `${(r.est[field] as number).toFixed(2)}%`,
  });

  const crackColumns: SimpleColumn<CrackRow>[] = [
    { field: 'label', headerName: t('columns.speedLevel') },
    { field: 'speed', headerName: t('columns.speed'), align: 'right', noWrap: true, render: (r) => formatSpeed(r.est.speed_hps) },
    pct('percent_under_1_hour', t('timeframes.lessThan1Hour')),
    pct('percent_under_1_day', t('timeframes.lessThan1Day')),
    pct('percent_under_1_week', t('timeframes.lessThan1Week')),
    pct('percent_under_1_month', t('timeframes.lessThan1Month')),
    pct('percent_under_6_months', t('timeframes.lessThan6Months')),
    pct('percent_under_1_year', t('timeframes.lessThan1Year')),
    pct('percent_over_1_year', t('timeframes.greaterThan1Year')),
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.strengthMetrics')}
      </Typography>

      {/* Entropy Distribution */}
      <Box sx={{ mb: 4 }}>
        <Typography variant="h6" gutterBottom>
          {t('sections.entropyDistribution')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('descriptions.shannonEntropy')}
        </Typography>
        <SimpleTable rows={entropyRows} getRowKey={(r) => r.key} columns={entropyColumns} />
      </Box>

      {/* Crack Time Estimates */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.crackTimeEstimates')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('descriptions.crackTimeEstimates')}
        </Typography>
        <SimpleTable rows={crackRows} getRowKey={(r) => r.key} columns={crackColumns} />
      </Box>
    </Paper>
  );
}
