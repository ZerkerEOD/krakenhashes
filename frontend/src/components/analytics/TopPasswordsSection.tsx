/**
 * Top passwords section showing most common passwords (2+ uses, with plaintext).
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
  Alert,
  Chip,
} from '@mui/material';
import { Warning as WarningIcon } from '@mui/icons-material';
import { TopPassword } from '../../types/analytics';

interface TopPasswordsSectionProps {
  data: TopPassword[];
}

export default function TopPasswordsSection({ data }: TopPasswordsSectionProps) {
  const { t } = useTranslation('analytics');

  if (data.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.topPasswords')}
      </Typography>
      <Alert severity="warning" icon={<WarningIcon />} sx={{ mb: 2 }}>
        {t('warnings.internalUseOnly')}
      </Alert>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('descriptions.topPasswords')}
      </Typography>

      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>{t('columns.rank')}</TableCell>
              <TableCell>{t('columns.password')}</TableCell>
              <TableCell align="right">{t('columns.count')}</TableCell>
              <TableCell align="right">{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.map((pwd, index) => (
              <TableRow key={index}>
                <TableCell>{index + 1}</TableCell>
                <TableCell>
                  <Chip
                    label={pwd.password}
                    size="small"
                    sx={{ fontFamily: 'monospace' }}
                  />
                </TableCell>
                <TableCell align="right">{pwd.count.toLocaleString()}</TableCell>
                <TableCell align="right">{pwd.percentage.toFixed(2)}%</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}
