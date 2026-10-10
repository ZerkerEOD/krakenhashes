/**
 * Hash Reuse Analytics Section
 * Displays hash-based password reuse analysis for NTLM/LM hashes
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Paper, Typography, Chip, Alert } from '@mui/material';
import { Fingerprint as FingerprintIcon } from '@mui/icons-material';
import CrackedPassword from '../common/CrackedPassword';
import { SimpleTable, SimpleColumn } from '../ui';
import { ExpandableUserList } from './PasswordReuseSection';

interface HashReuseItem {
  hash_value: string;
  hash_type: string;
  password?: string;
  users: Array<{
    username: string;
    hashlist_count: number;
  }>;
  total_occurrences: number;
  user_count: number;
}

interface HashReuseData {
  total_reused: number;
  percentage_reused: number;
  total_unique: number;
  hash_reuse_info: HashReuseItem[];
}

interface HashReuseSectionProps {
  data: HashReuseData;
}

export default function HashReuseSection({ data }: HashReuseSectionProps) {
  const { t } = useTranslation('analytics');

  const header = (
    <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
      <FingerprintIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
      <Typography variant="h5" component="h2">
        {t('sections.hashReuse')}
      </Typography>
    </Box>
  );

  if (!data || data.hash_reuse_info.length === 0) {
    return (
      <Paper sx={{ p: 3, mb: 3 }}>
        {header}
        <Alert severity="info">{t('messages.noHashReuse')}</Alert>
      </Paper>
    );
  }

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  const columns: SimpleColumn<HashReuseItem>[] = [
    {
      field: 'hash_value',
      headerName: t('columns.hashValue'),
      mono: true,
      noWrap: true,
      render: (item) => `${item.hash_value.substring(0, 16)}...`,
    },
    {
      field: 'hash_type',
      headerName: t('columns.type'),
      render: (item) => <Chip label={item.hash_type} size="small" color="primary" variant="outlined" />,
    },
    {
      field: 'password',
      headerName: t('columns.password'),
      mono: true,
      render: (item) =>
        item.password ? (
          <CrackedPassword password={item.password} />
        ) : (
          <Typography variant="body2" color="text.secondary" component="span">
            —
          </Typography>
        ),
    },
    {
      field: 'users_list',
      headerName: t('labels.usersWithHash'),
      render: (item) => <ExpandableUserList users={item.users} />,
    },
    { field: 'user_count', headerName: t('columns.users'), align: 'right' },
    { field: 'total_occurrences', headerName: t('columns.occurrences'), align: 'right' },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      {header}

      <Typography variant="body2" color="text.secondary" paragraph>
        {t('descriptions.hashReuseAnalysis')}
      </Typography>

      {/* Summary Statistics */}
      <Box sx={{ display: 'flex', gap: 3, mb: 3 }}>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.totalReused')}
          </Typography>
          <Typography variant="h6">{data.total_reused.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.totalUnique')}
          </Typography>
          <Typography variant="h6">{data.total_unique.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.reusePercentage')}
          </Typography>
          <Typography variant="h6" color={data.percentage_reused > 20 ? 'error.main' : 'text.primary'}>
            {formatPercentage(data.percentage_reused)}
          </Typography>
        </Box>
      </Box>

      {/* Reused Hashes Table */}
      <SimpleTable rows={data.hash_reuse_info} columns={columns} maxRows={50} />

      {data.hash_reuse_info.length >= 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          {t('messages.top50Hashes')}
        </Typography>
      )}
    </Paper>
  );
}
