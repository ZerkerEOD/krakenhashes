/**
 * Overview section showing high-level statistics and hash mode breakdown.
 */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Paper,
  Typography,
  Grid,
  Card,
  CardContent,
  Box,
  Tabs,
  Tab,
} from '@mui/material';
import { AnalyticsReport, AnalyticsData, DomainStats, HashModeStats } from '../../types/analytics';
import { SimpleTable, SimpleColumn } from '../ui';

/** Hash-mode row, plus the synthetic totals row (rendered bold). */
type HashModeRow = Pick<HashModeStats, 'mode_id' | 'mode_name' | 'total' | 'cracked' | 'percentage'> & { isTotal?: boolean };

interface OverviewSectionProps {
  report: AnalyticsReport;
  data: AnalyticsData;
  filteredData: AnalyticsData;
  selectedDomain: string | null;
  onDomainChange: (domain: string | null) => void;
}

export default function OverviewSection({ report, data, filteredData, selectedDomain, onDomainChange }: OverviewSectionProps) {
  const { t } = useTranslation('analytics');
  const domains = data.overview.domain_breakdown || [];

  // Use filtered data's overview directly (already filtered by parent)
  const filteredStats = {
    total_hashes: filteredData.overview.total_hashes,
    total_cracked: filteredData.overview.total_cracked,
    crack_percentage: filteredData.overview.crack_percentage,
  };

  const crackPercentage = filteredStats.crack_percentage.toFixed(2);

  const handleTabChange = (event: React.SyntheticEvent, newValue: number) => {
    if (newValue === 0) {
      onDomainChange(null); // "All" tab
    } else {
      const domain = domains[newValue - 1]?.domain;
      if (domain) {
        onDomainChange(domain);
      }
    }
  };

  const getCurrentTabValue = () => {
    if (!selectedDomain) return 0;
    const index = domains.findIndex(d => d.domain === selectedDomain);
    return index >= 0 ? index + 1 : 0;
  };

  const domainColumns: SimpleColumn<DomainStats>[] = [
    { field: 'domain', headerName: t('columns.domain') },
    { field: 'total_hashes', headerName: t('columns.totalHashes'), align: 'right', render: (d) => d.total_hashes.toLocaleString() },
    { field: 'cracked_hashes', headerName: t('columns.cracked'), align: 'right', render: (d) => d.cracked_hashes.toLocaleString() },
    { field: 'crack_percentage', headerName: t('columns.percentage'), align: 'right', render: (d) => `${d.crack_percentage.toFixed(2)}%` },
  ];

  const hashModeRows: HashModeRow[] = [
    ...filteredData.overview.hash_modes,
    {
      mode_id: -1,
      mode_name: t('labels.total'),
      total: filteredStats.total_hashes,
      cracked: filteredStats.total_cracked,
      percentage: filteredStats.crack_percentage,
      isTotal: true,
    },
  ];

  const bold = (r: HashModeRow, node: React.ReactNode) => (r.isTotal ? <strong>{node}</strong> : node);

  const hashModeColumns: SimpleColumn<HashModeRow>[] = [
    { field: 'mode_name', headerName: t('columns.hashType'), render: (r) => bold(r, r.mode_name) },
    { field: 'total', headerName: t('columns.totalHashes'), align: 'right', render: (r) => bold(r, r.total.toLocaleString()) },
    { field: 'cracked', headerName: t('columns.cracked'), align: 'right', render: (r) => bold(r, r.cracked.toLocaleString()) },
    {
      field: 'percentage',
      headerName: t('columns.percentage'),
      align: 'right',
      render: (r) => bold(r, `${r.percentage.toFixed(2)}%`),
    },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      {/* Domain Tabs - Only show if there are multiple domains */}
      {domains.length > 0 && (
        <Box sx={{ borderBottom: 1, borderColor: 'divider', mb: 3 }}>
          <Tabs value={getCurrentTabValue()} onChange={handleTabChange} aria-label={t('labels.domainFilterTabs') as string}>
            <Tab label={t('tabs.all')} />
            {domains.map((domainStat) => (
              <Tab key={domainStat.domain} label={domainStat.domain} />
            ))}
          </Tabs>
        </Box>
      )}

      <Typography variant="h5" gutterBottom>
        {selectedDomain
          ? t('sections.overviewWithDomain', { base: t('sections.overview'), domain: selectedDomain })
          : t('sections.overview')}
      </Typography>

      {/* Summary Cards */}
      <Grid container spacing={3} sx={{ mb: 3 }}>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="text.secondary" gutterBottom>
                {t('cards.totalHashes')}
              </Typography>
              <Typography variant="h4">
                {filteredStats.total_hashes.toLocaleString()}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="text.secondary" gutterBottom>
                {t('cards.cracked')}
              </Typography>
              <Typography variant="h4" color="success.main">
                {filteredStats.total_cracked.toLocaleString()}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="text.secondary" gutterBottom>
                {t('cards.crackRate')}
              </Typography>
              <Typography variant="h4" color="primary">
                {crackPercentage}%
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} md={3}>
          <Card>
            <CardContent>
              <Typography color="text.secondary" gutterBottom>
                {t('cards.hashlistsAnalyzed')}
              </Typography>
              <Typography variant="h4">
                {report.total_hashlists}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Domain Breakdown Table - Only show when "All" is selected and domains exist */}
      {!selectedDomain && domains.length > 0 && (
        <Box sx={{ mb: 3 }}>
          <Typography variant="h6" gutterBottom>
            {t('sections.domainBreakdown')}
          </Typography>
          <SimpleTable rows={domains} getRowKey={(d) => d.domain} columns={domainColumns} dense={false} />
        </Box>
      )}

      {/* Hash Mode Breakdown */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.hashModeBreakdown')}
        </Typography>
        <SimpleTable rows={hashModeRows} getRowKey={(r) => (r.isTotal ? 'total' : r.mode_id)} columns={hashModeColumns} dense={false} getRowSx={(r) => (r.isTotal ? { bgcolor: 'action.hover' } : undefined)} />
      </Box>
    </Paper>
  );
}
