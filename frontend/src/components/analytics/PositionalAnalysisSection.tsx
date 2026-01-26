/**
 * Positional analysis section showing uppercase start and numbers/special at end.
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
import { PositionalStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface PositionalStatsSectionProps {
  data: PositionalStats;
}

export default function PositionalStatsSection({ data }: PositionalStatsSectionProps) {
  const { t } = useTranslation('analytics');

  const hasData = data.starts_uppercase.count > 0 || data.ends_number.count > 0 || data.ends_special.count > 0;

  if (!hasData) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.positionalAnalysis')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.positionalPatterns')}
      </Typography>

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.pattern')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.starts_uppercase.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.startsUppercase')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.starts_uppercase.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.starts_uppercase.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.ends_number.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.endsNumber')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.ends_number.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.ends_number.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.ends_special.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.endsSpecial')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.ends_special.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.ends_special.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
