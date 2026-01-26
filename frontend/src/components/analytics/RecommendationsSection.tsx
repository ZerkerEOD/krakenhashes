/**
 * Recommendations section showing auto-generated password policy recommendations.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Paper,
  Typography,
  List,
  ListItem,
  ListItemText,
  Alert,
  Chip,
  Box,
} from '@mui/material';
import { Recommendation } from '../../types/analytics';

interface RecommendationsSectionProps {
  data: Recommendation[];
}

export default function RecommendationsSection({ data }: RecommendationsSectionProps) {
  const { t } = useTranslation('analytics');

  if (data.length === 0) {
    return (
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h5" gutterBottom>
          {t('sections.recommendations')}
        </Typography>
        <Alert severity="success">
          {t('messages.noRecommendations')}
        </Alert>
      </Paper>
    );
  }

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'CRITICAL': return 'error';
      case 'HIGH': return 'warning';
      case 'MEDIUM': return 'info';
      case 'INFO': return 'default';
      default: return 'default';
    }
  };

  const getSeverityLabel = (severity: string) => {
    switch (severity) {
      case 'CRITICAL': return t('severities.critical');
      case 'HIGH': return t('severities.high');
      case 'MEDIUM': return t('severities.medium');
      case 'INFO': return t('severities.info');
      default: return severity;
    }
  };

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.recommendations')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.recommendations')}
      </Typography>

      <List>
        {data.map((recommendation, index) => (
          <ListItem key={index}>
            <Box sx={{ display: 'flex', alignItems: 'center', width: '100%', gap: 2 }}>
              <Chip
                label={getSeverityLabel(recommendation.severity)}
                color={getSeverityColor(recommendation.severity) as any}
                size="small"
              />
              <ListItemText
                primary={recommendation.message}
                secondary={`${recommendation.count.toLocaleString()} ${(t('charts.password') as string).toLowerCase()}s (${recommendation.percentage.toFixed(2)}%)`}
                primaryTypographyProps={{
                  variant: 'body1',
                  sx: { fontWeight: 500 },
                }}
              />
            </Box>
          </ListItem>
        ))}
      </List>
    </Paper>
  );
}
