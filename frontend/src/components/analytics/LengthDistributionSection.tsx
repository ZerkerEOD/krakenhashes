/**
 * Length distribution section showing password lengths (0-32+).
 * Dynamically hides columns with zero values.
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
import { LengthStats } from '../../types/analytics';
import { threeColumnTableStyles } from './tableStyles';

interface LengthDistributionSectionProps {
  data: LengthStats;
}

export default function LengthDistributionSection({ data }: LengthDistributionSectionProps) {
  const { t } = useTranslation('analytics');

  // Filter out lengths with zero count
  const activeDistributions = useMemo(() => {
    const entries = Object.entries(data.distribution)
      .filter(([_, value]) => value.count > 0)
      .sort((a, b) => {
        // Sort numerically, with "32+" at the end
        const aNum = a[0] === '32+' ? 999 : parseInt(a[0]);
        const bNum = b[0] === '32+' ? 999 : parseInt(b[0]);
        return aNum - bNum;
      });
    return entries;
  }, [data.distribution]);

  if (activeDistributions.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.lengthDistribution')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.averageLength')} {data.average_length.toFixed(2)} {t('units.chars')}
      </Typography>

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.length')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {activeDistributions.map(([length, stats]) => (
              <TableRow key={length}>
                <TableCell sx={threeColumnTableStyles.labelCell}>{length} {t('units.chars')}</TableCell>
                <TableCell sx={threeColumnTableStyles.countCell}>{stats.count.toLocaleString()}</TableCell>
                <TableCell sx={threeColumnTableStyles.percentageCell}>{stats.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
