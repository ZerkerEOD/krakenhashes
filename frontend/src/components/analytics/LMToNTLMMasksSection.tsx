/**
 * LM-to-NTLM Mask Generation Section
 * Displays generated hashcat masks from cracked LM passwords
 * to assist in cracking the case-sensitive NTLM versions
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Chip,
  Alert,
  Button,
  Grid,
  Card,
  CardContent,
} from '@mui/material';
import {
  VpnKey as MaskIcon,
  Download as DownloadIcon,
  TrendingUp as TrendingUpIcon,
} from '@mui/icons-material';
import { SimpleTable, SimpleColumn } from '../ui';

interface LMToNTLMMask {
  mask: string;
  lm_pattern: string;
  count: number;
  percentage: number;
  match_percentage: number;
  estimated_keyspace: number;
  example_lm: string;
}

interface LMToNTLMMaskData {
  total_lm_cracked: number;
  total_masks_generated: number;
  total_estimated_keyspace: number;
  masks: LMToNTLMMask[];
}

interface LMToNTLMMasksSectionProps {
  data: LMToNTLMMaskData | null;
}

export default function LMToNTLMMasksSection({ data }: LMToNTLMMasksSectionProps) {
  const { t } = useTranslation('analytics');

  if (!data || data.total_masks_generated === 0 || !data.masks || data.masks.length === 0) {
    return null;
  }

  const handleExportHCMask = () => {
    // Generate .hcmask file format
    const hcmaskContent = data.masks.map((m) => m.mask).join('\n');
    const blob = new Blob([hcmaskContent], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'lm_to_ntlm_masks.hcmask';
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleExportTxt = () => {
    // Generate detailed .txt format with statistics
    const txtContent = [
      t('export.header'),
      t('export.generatedFrom', { count: data.total_lm_cracked }),
      t('export.totalMasks', { count: data.total_masks_generated }),
      t('export.totalKeyspace', { keyspace: data.total_estimated_keyspace.toLocaleString() }),
      '#',
      t('export.format'),
      '',
      ...data.masks.map(
        (m) =>
          `${m.mask} | ${m.lm_pattern} | ${m.count} | ${m.match_percentage.toFixed(2)}% | ${m.estimated_keyspace} | ${m.example_lm}`
      ),
    ].join('\n');

    const blob = new Blob([txtContent], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'lm_to_ntlm_masks_detailed.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  const formatKeyspace = (keyspace: number) => {
    if (keyspace >= 1e12) return `${(keyspace / 1e12).toFixed(2)}T`;
    if (keyspace >= 1e9) return `${(keyspace / 1e9).toFixed(2)}B`;
    if (keyspace >= 1e6) return `${(keyspace / 1e6).toFixed(2)}M`;
    if (keyspace >= 1e3) return `${(keyspace / 1e3).toFixed(2)}K`;
    return keyspace.toString();
  };

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  const columns: SimpleColumn<LMToNTLMMask>[] = [
    {
      field: 'mask',
      headerName: t('columns.mask'),
      mono: true,
      render: (item) => <strong>{item.mask}</strong>,
    },
    {
      field: 'lm_pattern',
      headerName: t('columns.lmPattern'),
      render: (item) => <Chip label={item.lm_pattern} size="small" variant="outlined" />,
    },
    { field: 'count', headerName: t('columns.count'), align: 'right', render: (item) => item.count.toLocaleString() },
    {
      field: 'match_percentage',
      headerName: (
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5 }}>
          <TrendingUpIcon sx={{ fontSize: 16, color: 'success.main' }} />
          {t('columns.matchPercentage')}
        </Box>
      ),
      align: 'right',
      render: (item) => (
        <Chip
          label={formatPercentage(item.match_percentage)}
          size="small"
          color={item.match_percentage > 10 ? 'success' : item.match_percentage > 5 ? 'warning' : 'default'}
          sx={{ fontWeight: 'bold' }}
        />
      ),
    },
    {
      field: 'estimated_keyspace',
      headerName: t('columns.estimatedKeyspace'),
      align: 'right',
      render: (item) => (
        <Typography variant="body2" color="text.secondary" component="span">
          {formatKeyspace(item.estimated_keyspace)}
        </Typography>
      ),
    },
    { field: 'example_lm', headerName: t('columns.exampleLm'), mono: true },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Box sx={{ display: 'flex', alignItems: 'center' }}>
          <MaskIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
          <Typography variant="h5" component="h2">
            {t('sections.lmToNtlmMasks')}
          </Typography>
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button variant="outlined" size="small" startIcon={<DownloadIcon />} onClick={handleExportHCMask}>
            {t('actions.exportHcmask')}
          </Button>
          <Button variant="outlined" size="small" startIcon={<DownloadIcon />} onClick={handleExportTxt}>
            {t('actions.exportTxt')}
          </Button>
        </Box>
      </Box>

      <Alert severity="info" sx={{ mb: 3 }}>
        {t('descriptions.maskGeneration')}
      </Alert>

      {/* Summary Statistics */}
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.lmPasswordsAnalyzed')}
              </Typography>
              <Typography variant="h5">{data.total_lm_cracked.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.masksGenerated')}
              </Typography>
              <Typography variant="h5">{data.total_masks_generated.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.totalEstimatedKeyspace')}
              </Typography>
              <Typography variant="h5">{formatKeyspace(data.total_estimated_keyspace)}</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Masks Table */}
      <SimpleTable rows={data.masks} columns={columns} dense={false} maxRows={50} />

      {data.masks.length > 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          {t('descriptions.maskSorting')}
        </Typography>
      )}
    </Paper>
  );
}
