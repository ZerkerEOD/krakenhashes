/**
 * Top passwords section showing most common passwords (2+ uses, with plaintext).
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography, Alert, Chip } from '@mui/material';
import { Warning as WarningIcon } from '@mui/icons-material';
import { TopPassword } from '../../types/analytics';
import CrackedPassword from '../common/CrackedPassword';
import { SimpleTable, SimpleColumn } from '../ui';

interface TopPasswordsSectionProps {
  data: TopPassword[];
}

export default function TopPasswordsSection({ data }: TopPasswordsSectionProps) {
  const { t } = useTranslation('analytics');

  if (data.length === 0) {
    return null;
  }

  const columns: SimpleColumn<TopPassword>[] = [
    { field: 'rank', headerName: t('columns.rank'), render: (_r, i) => i + 1 },
    {
      field: 'password',
      headerName: t('columns.password'),
      render: (pwd) => (
        <Chip
          label={<CrackedPassword password={pwd.password} />}
          size="small"
          sx={{ fontFamily: (th) => th.typography.monoFamily }}
        />
      ),
    },
    { field: 'count', headerName: t('columns.count'), align: 'right', render: (pwd) => pwd.count.toLocaleString() },
    {
      field: 'percentage',
      headerName: t('columns.percentage'),
      align: 'right',
      render: (pwd) => `${pwd.percentage.toFixed(2)}%`,
    },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.topPasswords')}
      </Typography>
      <Alert severity="warning" icon={<WarningIcon />} sx={{ mb: 2 }}>
        {t('warnings.internalUseOnly')}
      </Alert>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.topPasswords')}
      </Typography>

      <SimpleTable rows={data} getRowKey={(_r, i) => i} columns={columns} />
    </Paper>
  );
}
