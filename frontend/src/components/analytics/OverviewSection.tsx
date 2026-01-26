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
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Box,
  Tabs,
  Tab,
} from '@mui/material';
import { AnalyticsReport, AnalyticsData, DomainStats } from '../../types/analytics';

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

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      {/* Domain Tabs - Only show if there are multiple domains */}
      {domains.length > 0 && (
        <Box sx={{ borderBottom: 1, borderColor: 'divider', mb: 3 }}>
          <Tabs value={getCurrentTabValue()} onChange={handleTabChange} aria-label="Domain filter tabs">
            <Tab label={t('tabs.all')} />
            {domains.map((domainStat) => (
              <Tab key={domainStat.domain} label={domainStat.domain} />
            ))}
          </Tabs>
        </Box>
      )}

      <Typography variant="h5" gutterBottom>
        {t('sections.overview')}{selectedDomain ? ` - ${selectedDomain}` : ''}
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
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>{t('columns.domain')}</TableCell>
                  <TableCell align="right">{t('columns.totalHashes')}</TableCell>
                  <TableCell align="right">{t('columns.cracked')}</TableCell>
                  <TableCell align="right">{t('columns.percentage')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {domains.map((domainStat) => (
                  <TableRow key={domainStat.domain}>
                    <TableCell>{domainStat.domain}</TableCell>
                    <TableCell align="right">{domainStat.total_hashes.toLocaleString()}</TableCell>
                    <TableCell align="right">{domainStat.cracked_hashes.toLocaleString()}</TableCell>
                    <TableCell align="right">{domainStat.crack_percentage.toFixed(2)}%</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </Box>
      )}

      {/* Hash Mode Breakdown */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.hashModeBreakdown')}
        </Typography>
        <TableContainer>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('columns.hashType')}</TableCell>
                <TableCell align="right">{t('columns.totalHashes')}</TableCell>
                <TableCell align="right">{t('columns.cracked')}</TableCell>
                <TableCell align="right">{t('columns.percentage')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {filteredData.overview.hash_modes.map((stat) => (
                <TableRow key={stat.mode_id}>
                  <TableCell>{stat.mode_name}</TableCell>
                  <TableCell align="right">{stat.total.toLocaleString()}</TableCell>
                  <TableCell align="right">{stat.cracked.toLocaleString()}</TableCell>
                  <TableCell align="right">{stat.percentage.toFixed(2)}%</TableCell>
                </TableRow>
              ))}
              {/* Totals Row */}
              <TableRow sx={{ fontWeight: 'bold', backgroundColor: 'action.hover' }}>
                <TableCell>
                  <strong>{t('labels.total')}</strong>
                </TableCell>
                <TableCell align="right">
                  <strong>{filteredStats.total_hashes.toLocaleString()}</strong>
                </TableCell>
                <TableCell align="right">
                  <strong>{filteredStats.total_cracked.toLocaleString()}</strong>
                </TableCell>
                <TableCell align="right">
                  <strong>{crackPercentage}%</strong>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </TableContainer>
      </Box>
    </Paper>
  );
}
