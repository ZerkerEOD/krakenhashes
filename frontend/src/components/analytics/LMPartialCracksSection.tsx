/**
 * LM Partial Cracks Section
 * Displays partially cracked LM hashes (one half cracked, other half unknown)
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Paper, Typography, Alert, Grid, Card, CardContent } from '@mui/material';
import { LockOpen as LockOpenIcon, Lock as LockIcon, Warning as WarningIcon } from '@mui/icons-material';
import { LMPartialCrackStats, LMPartialCrackDetail } from '../../types/analytics';
import { EntityLink, SimpleTable, SimpleColumn } from '../ui';

interface LMPartialCracksSectionProps {
  data: LMPartialCrackStats | null;
}

export default function LMPartialCracksSection({ data }: LMPartialCracksSectionProps) {
  const { t } = useTranslation('analytics');

  if (!data || data.total_partial === 0) {
    return null;
  }

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  const renderHalf = (cracked: boolean, pwd?: string) =>
    cracked ? (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <LockOpenIcon sx={{ fontSize: 18, color: 'warning.main' }} />
        <Typography variant="body2" sx={{ fontFamily: (th) => th.typography.monoFamily }}>
          {pwd || '???'}
        </Typography>
      </Box>
    ) : (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <LockIcon sx={{ fontSize: 18, color: 'text.disabled' }} />
        <Typography variant="body2" color="text.disabled">
          {t('labels.unknown')}
        </Typography>
      </Box>
    );

  const columns: SimpleColumn<LMPartialCrackDetail>[] = [
    { field: 'username', headerName: t('columns.username'), render: (item) => item.username || '—' },
    { field: 'domain', headerName: t('columns.domain'), render: (item) => item.domain || '—' },
    {
      field: 'first_half',
      headerName: t('columns.firstHalf'),
      render: (item) => renderHalf(item.first_half_cracked, item.first_half_pwd),
    },
    {
      field: 'second_half',
      headerName: t('columns.secondHalf'),
      render: (item) => renderHalf(item.second_half_cracked, item.second_half_pwd),
    },
    {
      field: 'hashlist',
      headerName: t('columns.hashlist'),
      // Older stored reports carry only the name; link when the id is present.
      render: (item) =>
        item.hashlist_id ? (
          <EntityLink type="hashlist" id={item.hashlist_id} label={item.hashlist_name} />
        ) : (
          item.hashlist_name
        ),
    },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <WarningIcon sx={{ fontSize: 32, color: 'warning.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          {t('sections.lmPartialCracks')}
        </Typography>
      </Box>

      <Alert severity="warning" sx={{ mb: 3 }}>
        {t('warnings.partialCracks')}
      </Alert>

      {/* Summary Statistics */}
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.totalPartial')}
              </Typography>
              <Typography variant="h5">{data.total_partial.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {formatPercentage(data.percentage_partial)} {t('descriptions.ofLmHashes')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.firstHalfOnly')}
              </Typography>
              <Typography variant="h5">{data.first_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {t('descriptions.chars1To7')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.secondHalfOnly')}
              </Typography>
              <Typography variant="h5">{data.second_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {t('descriptions.chars8To14')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Partial Cracks Table */}
      <SimpleTable rows={data.partial_crack_details} columns={columns} dense={false} maxRows={50} />

      {data.partial_crack_details.length >= 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          {t('messages.top50PartialCracks')}
        </Typography>
      )}
    </Paper>
  );
}
