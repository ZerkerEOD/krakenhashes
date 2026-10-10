/**
 * Username correlation section showing password-username relationships.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography } from '@mui/material';
import { UsernameStats } from '../../types/analytics';
import { SimpleTable } from '../ui';
import { countPctColumns, nonZeroRows } from './tableStyles';

interface UsernameCorrelationSectionProps {
  data: UsernameStats;
}

export default function UsernameCorrelationSection({ data }: UsernameCorrelationSectionProps) {
  const { t } = useTranslation('analytics');

  const rows = nonZeroRows([
    ['equals_username', t('correlations.sameAsUsername'), data.equals_username],
    ['contains_username', t('correlations.containsUsername'), data.contains_username],
    ['username_plus_suffix', t('correlations.usernamePart'), data.username_plus_suffix],
  ]);

  if (rows.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.usernameCorrelation')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.usernameCorrelation')}
      </Typography>

      <SimpleTable
        rows={rows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.correlationType'), t('columns.count'), t('columns.percentage'))}
      />
    </Paper>
  );
}
