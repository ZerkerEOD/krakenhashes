/**
 * Custom patterns section showing organization name pattern matches.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { CustomPatternStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface CustomPatternsSectionProps {
  data: CustomPatternStats;
}

export default function CustomPatternsSection({ data }: CustomPatternsSectionProps) {
  const { t } = useTranslation('analytics');

  const rows = nonZeroRows(Object.entries(data.patterns_detected).map(([name, stats]) => [name, name, stats]));

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.customPatterns')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.customPatterns')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.pattern'), t('columns.count'), t('columns.percentage'))}
      />
    </Paper>
  );
}
