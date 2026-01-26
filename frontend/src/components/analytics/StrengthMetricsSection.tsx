/**
 * Strength metrics section showing entropy distribution and crack time estimates.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Paper,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Box,
} from '@mui/material';
import { StrengthStats } from '../../types/analytics';

interface StrengthMetricsSectionProps {
  data: StrengthStats;
}

export default function StrengthMetricsSection({ data }: StrengthMetricsSectionProps) {
  const { t } = useTranslation('analytics');

  const formatSpeed = (hps: number): string => {
    if (hps >= 1000000000) return `${(hps / 1000000000).toFixed(2)} GH/s`;
    if (hps >= 1000000) return `${(hps / 1000000).toFixed(2)} MH/s`;
    if (hps >= 1000) return `${(hps / 1000).toFixed(2)} KH/s`;
    return `${hps.toFixed(0)} H/s`;
  };

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
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('columns.entropyLevel')}</TableCell>
                <TableCell>{t('columns.range')}</TableCell>
                <TableCell align="right">{t('columns.count')}</TableCell>
                <TableCell align="right">{t('columns.percentage')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              <TableRow>
                <TableCell>{t('entropyLevels.low')}</TableCell>
                <TableCell>{t('entropyRanges.low')}</TableCell>
                <TableCell align="right">{data.entropy_distribution.low.count.toLocaleString()}</TableCell>
                <TableCell align="right">{data.entropy_distribution.low.percentage.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('entropyLevels.moderate')}</TableCell>
                <TableCell>{t('entropyRanges.moderate')}</TableCell>
                <TableCell align="right">{data.entropy_distribution.moderate.count.toLocaleString()}</TableCell>
                <TableCell align="right">{data.entropy_distribution.moderate.percentage.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('entropyLevels.high')}</TableCell>
                <TableCell>{t('entropyRanges.high')}</TableCell>
                <TableCell align="right">{data.entropy_distribution.high.count.toLocaleString()}</TableCell>
                <TableCell align="right">{data.entropy_distribution.high.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </TableContainer>
      </Box>

      {/* Crack Time Estimates */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.crackTimeEstimates')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('descriptions.crackTimeEstimates')}
        </Typography>
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('columns.speedLevel')}</TableCell>
                <TableCell align="right">{t('columns.speed')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan1Hour')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan1Day')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan1Week')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan1Month')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan6Months')}</TableCell>
                <TableCell align="right">{t('timeframes.lessThan1Year')}</TableCell>
                <TableCell align="right">{t('timeframes.greaterThan1Year')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              <TableRow>
                <TableCell>{t('speedLevels.speed50')}</TableCell>
                <TableCell align="right">{formatSpeed(data.crack_time_estimates.speed_50_percent.speed_hps)}</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_1_hour.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_1_day.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_1_week.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_1_month.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_6_months.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_under_1_year.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_50_percent.percent_over_1_year.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('speedLevels.speed75')}</TableCell>
                <TableCell align="right">{formatSpeed(data.crack_time_estimates.speed_75_percent.speed_hps)}</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_1_hour.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_1_day.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_1_week.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_1_month.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_6_months.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_under_1_year.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_75_percent.percent_over_1_year.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('speedLevels.speed100')}</TableCell>
                <TableCell align="right">{formatSpeed(data.crack_time_estimates.speed_100_percent.speed_hps)}</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_1_hour.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_1_day.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_1_week.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_1_month.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_6_months.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_under_1_year.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_100_percent.percent_over_1_year.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('speedLevels.speed150')}</TableCell>
                <TableCell align="right">{formatSpeed(data.crack_time_estimates.speed_150_percent.speed_hps)}</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_1_hour.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_1_day.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_1_week.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_1_month.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_6_months.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_under_1_year.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_150_percent.percent_over_1_year.toFixed(2)}%</TableCell>
              </TableRow>
              <TableRow>
                <TableCell>{t('speedLevels.speed200')}</TableCell>
                <TableCell align="right">{formatSpeed(data.crack_time_estimates.speed_200_percent.speed_hps)}</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_1_hour.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_1_day.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_1_week.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_1_month.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_6_months.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_under_1_year.toFixed(2)}%</TableCell>
                <TableCell align="right">{data.crack_time_estimates.speed_200_percent.percent_over_1_year.toFixed(2)}%</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </TableContainer>
      </Box>
    </Paper>
  );
}
