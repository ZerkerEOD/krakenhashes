/**
 * Main display component for analytics reports.
 * Handles status-aware rendering and coordinates all section components.
 */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Alert,
  AlertTitle,
  Button,
  LinearProgress,
} from '@mui/material';
import { Replay as RetryIcon, Delete as DeleteIcon } from '@mui/icons-material';
import { AnalyticsReport } from '../../types/analytics';
import OverviewSection from './OverviewSection';
import WindowsHashSection from './WindowsHashSection';
import LengthDistributionSection from './LengthDistributionSection';
import ComplexityAnalysisSection from './ComplexityAnalysisSection';
import PositionalAnalysisSection from './PositionalAnalysisSection';
import PatternDetectionSection from './PatternDetectionSection';
import UsernameCorrelationSection from './UsernameCorrelationSection';
import PasswordReuseSection from './PasswordReuseSection';
import HashReuseSection from './HashReuseSection';
import TemporalPatternsSection from './TemporalPatternsSection';
import MaskAnalysisSection from './MaskAnalysisSection';
import CustomPatternsSection from './CustomPatternsSection';
import StrengthMetricsSection from './StrengthMetricsSection';
import TopPasswordsSection from './TopPasswordsSection';
import LMPartialCracksSection from './LMPartialCracksSection';
import LMToNTLMMasksSection from './LMToNTLMMasksSection';
import RecommendationsSection from './RecommendationsSection';

interface AnalyticsReportDisplayProps {
  report: AnalyticsReport;
  status: string;
  onRetry?: () => void;
  onDelete?: () => void;
}

export default function AnalyticsReportDisplay({
  report,
  status,
  onRetry,
  onDelete,
}: AnalyticsReportDisplayProps) {
  const { t } = useTranslation('analytics');
  const [selectedDomain, setSelectedDomain] = useState<string | null>(null);
  // Render status-specific UI
  const renderStatusUI = () => {
    switch (status) {
      case 'queued':
        return (
          <Alert severity="info" sx={{ mb: 3 }}>
            <AlertTitle>{t('status.pendingGeneration')}</AlertTitle>
            {t('messages.queuedPosition')} {report.queue_position || 'N/A'}
            <LinearProgress sx={{ mt: 2 }} />
          </Alert>
        );

      case 'processing':
        return (
          <Alert severity="info" sx={{ mb: 3 }}>
            <AlertTitle>{t('status.generating')}</AlertTitle>
            {t('messages.processingDescription')}
            <LinearProgress sx={{ mt: 2 }} />
          </Alert>
        );

      case 'failed':
        return (
          <Alert
            severity="error"
            sx={{ mb: 3 }}
            action={
              <Box>
                {onRetry && (
                  <Button color="inherit" size="small" startIcon={<RetryIcon />} onClick={onRetry}>
                    {t('actions.retry')}
                  </Button>
                )}
                {onDelete && (
                  <Button color="inherit" size="small" startIcon={<DeleteIcon />} onClick={onDelete}>
                    {t('actions.delete')}
                  </Button>
                )}
              </Box>
            }
          >
            <AlertTitle>{t('status.failed')}</AlertTitle>
            {report.error_message || t('messages.generationError')}
          </Alert>
        );

      default:
        return null;
    }
  };

  // Don't render full report if not completed
  if (status !== 'completed' || !report.analytics_data) {
    return (
      <Paper sx={{ p: 3 }}>
        {renderStatusUI()}
        <Typography variant="body2" color="text.secondary">
          {t('labels.reportId')} {report.id}
        </Typography>
      </Paper>
    );
  }

  const data = report.analytics_data;

  // Get the analytics data based on selected domain
  const getAnalyticsData = () => {
    if (!selectedDomain || !data.domain_analytics) {
      return data; // Return "All" data
    }

    // Find domain-specific analytics
    const domainData = data.domain_analytics.find((d) => d.domain === selectedDomain);
    if (!domainData) {
      return data; // Fallback to "All"
    }

    // Return domain-filtered analytics
    return {
      overview: domainData.overview,
      windows_hashes: domainData.windows_hashes,
      length_distribution: domainData.length_distribution,
      complexity_analysis: domainData.complexity_analysis,
      positional_analysis: domainData.positional_analysis,
      pattern_detection: domainData.pattern_detection,
      username_correlation: domainData.username_correlation,
      password_reuse: domainData.password_reuse,
      hash_reuse: domainData.hash_reuse,
      temporal_patterns: domainData.temporal_patterns,
      mask_analysis: domainData.mask_analysis,
      custom_patterns: domainData.custom_patterns,
      strength_metrics: domainData.strength_metrics,
      top_passwords: domainData.top_passwords,
      lm_partial_cracks: domainData.lm_partial_cracks,
      lm_to_ntlm_masks: domainData.lm_to_ntlm_masks,
      recommendations: data.recommendations, // Keep global recommendations
      domain_analytics: data.domain_analytics, // Keep for reference
    };
  };

  const filteredData = getAnalyticsData();

  return (
    <Box>
      {/* Status indicator */}
      {renderStatusUI()}

      {/* Report ID for debugging */}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('labels.reportId')} {report.id}
      </Typography>

      {/* Overview Section - Full Width */}
      <OverviewSection
        report={report}
        data={data}
        filteredData={filteredData}
        selectedDomain={selectedDomain}
        onDomainChange={setSelectedDomain}
      />

      {/* Windows Hash Analytics - Full Width */}
      {filteredData.windows_hashes && <WindowsHashSection data={filteredData.windows_hashes} />}

      {/* Masonry-style layout using CSS columns - cards flow vertically to fill space */}
      <Box
        sx={{
          columnCount: { xs: 1, md: 2 },
          columnGap: 3,
          '& > *': {
            breakInside: 'avoid',
            marginBottom: 3,
            display: 'inline-block',
            width: '100%',
          },
        }}
      >
        <LengthDistributionSection data={filteredData.length_distribution} />

        <ComplexityAnalysisSection data={filteredData.complexity_analysis} />

        <PositionalAnalysisSection data={filteredData.positional_analysis} />

        <PatternDetectionSection data={filteredData.pattern_detection} />

        <UsernameCorrelationSection data={filteredData.username_correlation} />

        <TemporalPatternsSection data={filteredData.temporal_patterns} />

        <MaskAnalysisSection data={filteredData.mask_analysis} />

        {filteredData.custom_patterns && Object.keys(filteredData.custom_patterns.patterns_detected).length > 0 && (
          <CustomPatternsSection data={filteredData.custom_patterns} />
        )}
      </Box>

      {/* Full-Width Sections Below Grid */}
      <StrengthMetricsSection data={filteredData.strength_metrics} />

      <PasswordReuseSection data={filteredData.password_reuse} />

      {/* Hash Reuse - Full Width */}
      {filteredData.hash_reuse && <HashReuseSection data={filteredData.hash_reuse} />}

      <TopPasswordsSection data={filteredData.top_passwords} />

      {/* LM Partial Cracks - Full Width */}
      {filteredData.lm_partial_cracks && <LMPartialCracksSection data={filteredData.lm_partial_cracks} />}

      {/* LM-to-NTLM Masks - Full Width */}
      {filteredData.lm_to_ntlm_masks && <LMToNTLMMasksSection data={filteredData.lm_to_ntlm_masks} />}

      <RecommendationsSection data={filteredData.recommendations} />
    </Box>
  );
}
