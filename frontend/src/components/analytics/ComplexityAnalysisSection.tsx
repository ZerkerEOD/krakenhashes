/**
 * Complexity analysis section showing all 16 character type categories.
 * Includes single type, two types, three types, four types, and complex short/long.
 */
import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { ComplexityStats } from '../../types/analytics';
import { SimpleTable, SimpleColumn } from '../ui';
import { CountPctRow, nonZeroRows } from './tableStyles';

interface ComplexityAnalysisSectionProps {
  data: ComplexityStats;
}

/** A category row, or a group heading row (no count/percentage). */
type ComplexityRow = CountPctRow & { group?: boolean };

export default function ComplexityAnalysisSection({ data }: ComplexityAnalysisSectionProps) {
  const { t } = useTranslation('analytics');

  const rows = useMemo<ComplexityRow[]>(() => {
    const fromRecord = (obj: Record<string, { count: number; percentage: number }>) =>
      nonZeroRows(Object.entries(obj).map(([name, stats]) => [name, name, stats]));

    const groups: Array<[string, CountPctRow[]]> = [
      [t('categories.singleCharType'), fromRecord(data.single_type)],
      [t('categories.twoCharTypes'), fromRecord(data.two_types)],
      [t('categories.threeCharTypes'), fromRecord(data.three_types)],
      [t('categories.fourCharTypes'), nonZeroRows([['four_types', t('categories.allCharTypes'), data.four_types]])],
      [
        t('categories.complexPasswords'),
        nonZeroRows([
          ['complex_short', t('categories.complexShort'), data.complex_short],
          ['complex_long', t('categories.complexLong'), data.complex_long],
        ]),
      ],
    ];

    const out: ComplexityRow[] = [];
    groups.forEach(([heading, items], gi) => {
      if (items.length === 0) return;
      out.push({ key: `group-${gi}`, label: heading, count: 0, percentage: 0, group: true });
      items.forEach((item) => out.push({ ...item, key: `${gi}-${item.key}` }));
    });
    return out;
  }, [data, t]);

  if (rows.length === 0) {
    return null;
  }

  const columns: SimpleColumn<ComplexityRow>[] = [
    {
      field: 'label',
      headerName: t('columns.category'),
      width: '60%',
      render: (r) => (r.group ? <strong>{r.label}</strong> : r.label),
    },
    {
      field: 'count',
      headerName: t('columns.count'),
      width: '20%',
      align: 'right',
      render: (r) => (r.group ? '' : r.count.toLocaleString()),
    },
    {
      field: 'percentage',
      headerName: t('columns.percentage'),
      width: '20%',
      align: 'right',
      render: (r) => (r.group ? '' : `${r.percentage.toFixed(2)}%`),
    },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.complexityAnalysis')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.complexityDistribution')}
      </Typography>

      <SimpleTable rows={rows} getRowKey={(r) => r.key} columns={columns} isGroupRow={(r) => Boolean(r.group)} />
    </Paper>
  );
}
