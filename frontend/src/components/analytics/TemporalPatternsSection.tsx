/**
 * Temporal patterns section showing years, months, and seasons in passwords.
 */
import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography, Box } from '@mui/material';
import { TemporalStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface TemporalPatternsSectionProps {
  data: TemporalStats;
}

export default function TemporalPatternsSection({ data }: TemporalPatternsSectionProps) {
  const { t } = useTranslation('analytics');
  const yearRows = useMemo(
    () => nonZeroRows(Object.entries(data.year_breakdown).map(([year, stats]) => [year, year, stats])),
    [data.year_breakdown],
  );

  const summaryRows = nonZeroRows([
    ['contains_year', t('patterns.containsYear'), data.contains_year],
    ['contains_month', t('patterns.containsMonth'), data.contains_month],
    ['contains_season', t('patterns.containsSeason'), data.contains_season],
  ]);

  if (yearRows.length === 0 && summaryRows.length === 0) {
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
      {summaryRows.length > 0 && (
        <SimpleTable
          sx={{ mb: 3 }}
          rows={summaryRows}
          getRowKey={(r) => r.key}
          columns={countPctColumns(t('columns.patternType'), t('columns.count'), t('columns.percentage'))}
        />
      )}

      {/* Year Breakdown */}
      {yearRows.length > 0 && (
        <Box>
          <Typography variant="h6" gutterBottom>
            {t('sections.yearBreakdown')}
          </Typography>
          <SimpleTable
            rows={yearRows}
            getRowKey={(r) => r.key}
            columns={countPctColumns(t('columns.year'), t('columns.count'), t('columns.percentage'))}
          />
        </Box>
      )}
    </Paper>
  );
}
