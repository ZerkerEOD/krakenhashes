/**
 * Mask analysis section showing hashcat-style mask patterns.
 */
import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { MaskStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { CountPctRow, countPctColumns } from './tableStyles';

interface MaskAnalysisSectionProps {
  data: MaskStats;
}

export default function MaskAnalysisSection({ data }: MaskAnalysisSectionProps) {
  const { t } = useTranslation('analytics');

  // Filter and sort masks by count; show the top 20
  const rows = useMemo<CountPctRow[]>(
    () =>
      data.top_masks
        .filter((mask) => mask.count > 0)
        .sort((a, b) => b.count - a.count)
        .slice(0, 20)
        .map((mask, i) => ({ key: `${mask.mask}-${i}`, label: mask.mask, count: mask.count, percentage: mask.percentage })),
    [data.top_masks],
  );

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.maskAnalysis')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.maskFormat')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.maskPattern'), t('columns.count'), t('columns.percentage'), { mono: true })}
      />
    </Paper>
  );
}
