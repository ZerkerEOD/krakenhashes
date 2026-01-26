/**
 * Temporal patterns section showing years, months, and seasons in passwords.
 */
import React, { useMemo } from 'react';
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
import { TemporalStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface TemporalPatternsSectionProps {
  data: TemporalStats;
}

export default function TemporalPatternsSection({ data }: TemporalPatternsSectionProps) {
  const { t } = useTranslation('analytics');
  const years = useMemo(() =>
    Object.entries(data.year_breakdown).filter(([_, value]) => value.count > 0),
    [data.year_breakdown]
  );

  const hasData = years.length > 0 || data.contains_year.count > 0 ||
                  data.contains_month.count > 0 || data.contains_season.count > 0;

  if (!hasData) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.temporalPatterns')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.datePatterns')}
      </Typography>

      {/* Summary */}
      <TableContainer sx={{ mb: 3 }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.patternType')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.contains_year.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.containsYear')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.contains_year.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.contains_year.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.contains_month.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.containsMonth')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.contains_month.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.contains_month.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.contains_season.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.containsSeason')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.contains_season.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.contains_season.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>

      {/* Year Breakdown */}
      {years.length > 0 && (
        <Box>
          <Typography variant="h6" gutterBottom>
            {t('sections.yearBreakdown')}
          </Typography>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.year')}</TableCell>
                  <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
                  <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {years.map(([year, stats]) => (
                  <TableRow key={year}>
                    <TableCell sx={threeColumnTableStyles.labelCell}>{year}</TableCell>
                    <TableCell sx={threeColumnTableStyles.countCell}>{stats.count.toLocaleString()}</TableCell>
                    <TableCell sx={threeColumnTableStyles.percentageCell}>{stats.percentage.toFixed(2)}%</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Box>
      )}
    </Paper>
  );
}
