/**
 * Length distribution section showing password lengths (0-32+).
 * Dynamically hides rows with zero values.
 */
import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { LengthStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface LengthDistributionSectionProps {
  data: LengthStats;
}

export default function LengthDistributionSection({ data }: LengthDistributionSectionProps) {
  const { t } = useTranslation('analytics');

  // Filter out lengths with zero count, sorted numerically with "32+" last
  const rows = useMemo(() => {
    const entries = Object.entries(data.distribution).sort((a, b) => {
      const aNum = a[0] === '32+' ? 999 : parseInt(a[0]);
      const bNum = b[0] === '32+' ? 999 : parseInt(b[0]);
      return aNum - bNum;
    });
    return nonZeroRows(entries.map(([length, stats]) => [length, `${length} ${t('units.chars')}`, stats]));
  }, [data.distribution, t]);

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.lengthDistribution')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.averageLength')} {data.average_length.toFixed(2)} {t('units.chars')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.length'), t('columns.count'), t('columns.percentage'))}
      />
    </Paper>
  );
}
