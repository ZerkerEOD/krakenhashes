/**
 * Windows Hash Analytics Section
 * Displays comprehensive statistics for Windows-related hash types including
 * NTLM, LM, NetNTLMv1/v2, DCC/DCC2, and Kerberos
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Grid,
  Card,
  CardContent,
  Chip,
  Divider,
  Alert,
  AlertTitle,
} from '@mui/material';
import { alpha, Theme } from '@mui/material/styles';
import {
  Security as SecurityIcon,
  Warning as WarningIcon,
  CheckCircle as CheckCircleIcon,
  Link as LinkIcon,
} from '@mui/icons-material';
import { SimpleTable, SimpleColumn } from '../ui';

interface KerberosTypeRow {
  type: string;
  total: number;
  cracked: number;
  percentage: number;
}

/** Soft tinted background derived from a palette tone (works in light and dark). */
const tint = (tone: 'primary' | 'success' | 'info' | 'warning', amount = 0.08) => (th: Theme) =>
  alpha(th.palette[tone].main, amount);

interface WindowsHashSectionProps {
  data: any; // WindowsHashStats type
}

export default function WindowsHashSection({ data }: WindowsHashSectionProps) {
  const { t } = useTranslation('analytics');

  if (!data) {
    return null;
  }

  const { overview, ntlm, lm, netntlmv1, netntlmv2, dcc, dcc2, kerberos, linkedCorrelation } = data;

  // Helper to format percentage
  const formatPercentage = (value: number) => {
    return value.toFixed(2) + '%';
  };

  // Helper to render hash type card
  const renderHashTypeCard = (title: string, stats: any, color: string, showDetails?: boolean) => {
    if (!stats || stats.total === 0) {
      return null;
    }

    return (
      <Grid item xs={12} md={6} lg={4}>
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
              <SecurityIcon sx={{ color, mr: 1 }} />
              <Typography variant="h6">{title}</Typography>
            </Box>
            <Box sx={{ mb: 1 }}>
              <Typography variant="body2" color="text.secondary">
                {t('labels.total')}: <strong>{stats.total.toLocaleString()}</strong>
              </Typography>
              <Typography variant="body2" color="text.secondary">
                {t('labels.cracked')} <strong>{stats.cracked.toLocaleString()}</strong>
              </Typography>
              <Typography variant="body2" color="text.secondary">
                {t('labels.percentage')} <strong>{formatPercentage(stats.percentage)}</strong>
              </Typography>
            </Box>
            {showDetails && stats.under_8 !== undefined && (
              <>
                <Divider sx={{ my: 1 }} />
                <Typography variant="caption" color="text.secondary">
                  {t('descriptions.lengthDistribution')}
                </Typography>
                <Typography variant="body2">
                  {t('labels.underOrEqual7Chars')} {stats.under_8.toLocaleString()}
                </Typography>
                <Typography variant="body2">
                  {t('labels.8To14Chars')} {stats['8_to_14'].toLocaleString()}
                </Typography>
                {stats.partially_cracked > 0 && (
                  <>
                    <Divider sx={{ my: 1 }} />
                    <Box sx={{ display: 'flex', alignItems: 'center' }}>
                      <WarningIcon sx={{ fontSize: 16, color: 'warning.main', mr: 0.5 }} />
                      <Typography variant="body2" color="warning.main">
                        {t('labels.partiallyCracked')} {stats.partially_cracked.toLocaleString()}
                      </Typography>
                    </Box>
                  </>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </Grid>
    );
  };

  const kerberosRows: KerberosTypeRow[] = kerberos?.by_type
    ? Object.entries(kerberos.by_type).map(([type, stats]: [string, any]) => ({
        type,
        total: stats.total,
        cracked: stats.cracked,
        percentage: stats.percentage,
      }))
    : [];

  const kerberosColumns: SimpleColumn<KerberosTypeRow>[] = [
    {
      field: 'type',
      headerName: t('columns.type'),
      render: (r) => (
        <>
          {r.type === 'etype_23' && <Chip label={`${t('kerberosTypes.rc4')} (etype 23)`} size="small" color="warning" />}
          {r.type === 'etype_17' && <Chip label={`${t('kerberosTypes.aes128')} (etype 17)`} size="small" color="success" />}
          {r.type === 'etype_18' && <Chip label={`${t('kerberosTypes.aes256')} (etype 18)`} size="small" color="success" />}
        </>
      ),
    },
    { field: 'total', headerName: t('columns.total'), align: 'right', render: (r) => r.total.toLocaleString() },
    { field: 'cracked', headerName: t('columns.cracked'), align: 'right', render: (r) => r.cracked.toLocaleString() },
    { field: 'percentage', headerName: t('columns.percentage'), align: 'right', render: (r) => formatPercentage(r.percentage) },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <SecurityIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          {t('sections.windowsHashAnalytics')}
        </Typography>
      </Box>

      {/* Linked Hashlist Disclaimer */}
      {overview.linked_pairs > 0 && (
        <Alert severity="info" sx={{ mb: 3 }}>
          <AlertTitle>{t('warnings.linkedHashlistAnalysis')}</AlertTitle>
          {t('alerts.linkedHashlistInfo')}
        </Alert>
      )}

      {/* Overview Card */}
      <Card sx={{ mb: 3, bgcolor: tint('primary', 0.06) }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t('sections.overview')}
          </Typography>
          <Grid container spacing={2}>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                {t('labels.totalHashRecords')}
              </Typography>
              <Typography variant="h4">{overview.total_windows.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {t('descriptions.includesAllTypes')}
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                {t('labels.uniqueUsers')}
              </Typography>
              <Typography variant="h4">{overview.unique_users.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {t('descriptions.distinctUsernames')}
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                {t('cards.cracked')}
              </Typography>
              <Typography variant="h4">{overview.cracked_windows.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {t('descriptions.successfullyCracked')}
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                {t('labels.successRate')}
              </Typography>
              <Typography variant="h4">{formatPercentage(overview.percentage_windows)}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {t('descriptions.overallPercentage')}
              </Typography>
            </Grid>
          </Grid>
          {overview.linked_pairs > 0 && (
            <Box sx={{ mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}>
              <Typography variant="body2" color="text.secondary">
                <LinkIcon sx={{ fontSize: 16, mr: 0.5, verticalAlign: 'text-bottom' }} />
                <strong>{overview.linked_pairs.toLocaleString()}</strong> {t('labels.linkedPairsFound')}
              </Typography>
            </Box>
          )}
        </CardContent>
      </Card>

      {/* Hash Type Cards */}
      <Typography variant="h6" gutterBottom sx={{ mt: 3, mb: 2 }}>
        {t('sections.hashTypes')}
      </Typography>
      <Grid container spacing={2}>
        {renderHashTypeCard(t('hashTypes.ntlm'), ntlm, 'primary.main')}
        {renderHashTypeCard(t('hashTypes.lm'), lm, 'error.main', true)}
        {renderHashTypeCard(t('hashTypes.netntlmv1'), netntlmv1, 'warning.main')}
        {renderHashTypeCard(t('hashTypes.netntlmv2'), netntlmv2, 'info.main')}
        {renderHashTypeCard(t('hashTypes.dcc'), dcc, 'secondary.main')}
        {renderHashTypeCard(t('hashTypes.dcc2'), dcc2, 'secondary.main')}
      </Grid>

      {/* Kerberos Section */}
      {kerberos && kerberos.total > 0 && (
        <Box sx={{ mt: 3 }}>
          <Typography variant="h6" gutterBottom>
            {t('sections.kerberos')}
          </Typography>
          <Card>
            <CardContent>
              <Box sx={{ mb: 2 }}>
                <Typography variant="body2" color="text.secondary">
                  {t('labels.total')}: <strong>{kerberos.total.toLocaleString()}</strong> | {t('labels.cracked')}{' '}
                  <strong>{kerberos.cracked.toLocaleString()}</strong> | {t('labels.successRate')}:{' '}
                  <strong>{formatPercentage(kerberos.percentage)}</strong>
                </Typography>
              </Box>
              {kerberos.by_type && Object.keys(kerberos.by_type).length > 0 && (
                <>
                  <Divider sx={{ my: 2 }} />
                  <Typography variant="subtitle2" gutterBottom>
                    {t('sections.encryptionTypes')}
                  </Typography>
                  <SimpleTable
                    rows={kerberosRows}
                    getRowKey={(r) => r.type}
                    columns={kerberosColumns}
                  />
                </>
              )}
            </CardContent>
          </Card>
        </Box>
      )}

      {/* Linked Hash Correlation */}
      {linkedCorrelation && linkedCorrelation.total_linked_pairs > 0 && (
        <Box sx={{ mt: 3 }}>
          <Typography variant="h6" gutterBottom sx={{ display: 'flex', alignItems: 'center' }}>
            <LinkIcon sx={{ mr: 1 }} />
            {t('sections.linkedHashCorrelation')}
          </Typography>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t('labels.totalLinkedPairs')} <strong>{linkedCorrelation.total_linked_pairs.toLocaleString()}</strong>
              </Typography>
              <Grid container spacing={2} sx={{ mt: 1 }}>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: tint('success'), borderRadius: 1 }}>
                    <CheckCircleIcon sx={{ color: 'success.main', fontSize: 32 }} />
                    <Typography variant="h6">{linkedCorrelation.both_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">{t('labels.bothCracked')}</Typography>
                    <Typography variant="body2" color="text.secondary">
                      ({formatPercentage(linkedCorrelation.percentage_both)})
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: tint('info'), borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.only_ntlm_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">{t('labels.ntlmOnly')}</Typography>
                    <Typography variant="body2" color="text.secondary">
                      {t('descriptions.lmDerivable')}
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: tint('warning'), borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.only_lm_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">{t('labels.lmOnly')}</Typography>
                    <Typography variant="body2" color="text.secondary">
                      {t('descriptions.ntlmUnknown')}
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: 'surface.sunken', borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.neither_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">{t('labels.neitherCracked')}</Typography>
                  </Box>
                </Grid>
              </Grid>
            </CardContent>
          </Card>
        </Box>
      )}
    </Paper>
  );
}
