/**
 * Mask analysis section showing hashcat-style mask patterns.
 */
import React, { useMemo } from 'react';
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
import { MaskStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface MaskAnalysisSectionProps {
  data: MaskStats;
}

export default function MaskAnalysisSection({ data }: MaskAnalysisSectionProps) {
  const { t } = useTranslation('analytics');

  // Filter and sort masks by count
  const topMasks = useMemo(() => {
    return data.top_masks
      .filter(mask => mask.count > 0)
      .sort((a, b) => b.count - a.count)
      .slice(0, 20); // Show top 20 masks
  }, [data.top_masks]);

  if (topMasks.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.maskAnalysis')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.maskFormat')}
      </Typography>

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.maskPattern')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {topMasks.map((mask, index) => (
              <TableRow key={index}>
                <TableCell sx={{ ...threeColumnTableStyles.labelCell, fontFamily: 'monospace' }}>{mask.mask}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{mask.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{mask.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
