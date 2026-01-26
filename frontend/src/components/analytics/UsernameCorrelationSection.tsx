/**
 * Username correlation section showing password-username relationships.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Paper,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
} from '@mui/material';
import { UsernameStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface UsernameCorrelationSectionProps {
  data: UsernameStats;
}

export default function UsernameCorrelationSection({ data }: UsernameCorrelationSectionProps) {
  const { t } = useTranslation('analytics');
  const hasData = data.equals_username.count > 0 || data.contains_username.count > 0 || data.username_plus_suffix.count > 0;

  if (!hasData) {
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

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.correlationType')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.equals_username.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('correlations.sameAsUsername')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.equals_username.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.equals_username.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.contains_username.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('correlations.containsUsername')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.contains_username.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.contains_username.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.username_plus_suffix.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('correlations.usernamePart')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.username_plus_suffix.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.username_plus_suffix.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
