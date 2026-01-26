/**
 * Pattern detection section showing keyboard walks, sequences, and repeating characters.
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
import { PatternStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface PatternDetectionSectionProps {
  data: PatternStats;
}

export default function PatternDetectionSection({ data }: PatternDetectionSectionProps) {
  const { t } = useTranslation('analytics');

  const hasData = data.keyboard_walks.count > 0 || data.sequential.count > 0 || data.repeating_chars.count > 0;

  if (!hasData) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.patternDetection')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.weakPatterns')}
      </Typography>

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.patternType')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.keyboard_walks.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.keyboardWalks')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.keyboard_walks.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.keyboard_walks.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.sequential.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.sequences')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.sequential.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.sequential.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
            {data.repeating_chars.count > 0 && (
              <TableRow>
                <TableCell sx={threeColumnTableStyles.labelCell}>{t('patterns.repeatingChars')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{data.repeating_chars.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{data.repeating_chars.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
