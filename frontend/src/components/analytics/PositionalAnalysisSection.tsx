/**
 * Positional analysis section showing uppercase start and numbers/special at end.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { PositionalStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface PositionalStatsSectionProps {
  data: PositionalStats;
}

export default function PositionalStatsSection({ data }: PositionalStatsSectionProps) {
  const { t } = useTranslation('analytics');

  const rows = nonZeroRows([
    ['starts_uppercase', t('patterns.startsUppercase'), data.starts_uppercase],
    ['ends_number', t('patterns.endsNumber'), data.ends_number],
    ['ends_special', t('patterns.endsSpecial'), data.ends_special],
  ]);

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.positionalAnalysis')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.positionalPatterns')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.pattern'), t('columns.count'), t('columns.percentage'))}
      />
    </Paper>
  );
}
