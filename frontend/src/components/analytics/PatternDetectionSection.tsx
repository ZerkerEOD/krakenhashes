/**
 * Pattern detection section showing keyboard walks, sequences, and repeating characters.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { PatternStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface PatternDetectionSectionProps {
  data: PatternStats;
}

export default function PatternDetectionSection({ data }: PatternDetectionSectionProps) {
  const { t } = useTranslation('analytics');

  const rows = nonZeroRows([
    ['keyboard_walks', t('patterns.keyboardWalks'), data.keyboard_walks],
    ['sequential', t('patterns.sequences'), data.sequential],
    ['repeating_chars', t('patterns.repeatingChars'), data.repeating_chars],
  ]);

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.patternDetection')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.weakPatterns')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.patternType'), t('columns.count'), t('columns.percentage'))}
      />
    </Paper>
  );
}
