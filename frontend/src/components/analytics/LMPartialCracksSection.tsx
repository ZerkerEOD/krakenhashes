/**
 * LM Partial Cracks Section
 * Displays partially cracked LM hashes (one half cracked, other half unknown)
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  Alert,
  Grid,
  Card,
  CardContent,
} from '@mui/material';
import {
  LockOpen as LockOpenIcon,
  Lock as LockIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';

interface LMPartialCracksData {
  total_partial: number;
  first_half_only: number;
  second_half_only: number;
  percentage_partial: number;
  partial_crack_details: Array<{
    username?: string;
    domain?: string;
    first_half_cracked: boolean;
    first_half_pwd?: string;
    second_half_cracked: boolean;
    second_half_pwd?: string;
    hashlist_name: string;
  }>;
}

interface LMPartialCracksSectionProps {
  data: LMPartialCracksData | null;
}

export default function LMPartialCracksSection({ data }: LMPartialCracksSectionProps) {
  const { t } = useTranslation('analytics');

  if (!data || data.total_partial === 0) {
    return null;
  }

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <WarningIcon sx={{ fontSize: 32, color: 'warning.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          {t('sections.lmPartialCracks')}
        </Typography>
      </Box>

      <Alert severity="warning" sx={{ mb: 3 }}>
        {t('warnings.partialCracks')}
      </Alert>

      {/* Summary Statistics */}
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.totalPartial')}
              </Typography>
              <Typography variant="h5">{data.total_partial.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {formatPercentage(data.percentage_partial)} {t('descriptions.ofLmHashes')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.firstHalfOnly')}
              </Typography>
              <Typography variant="h5">{data.first_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {t('descriptions.chars1To7')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                {t('labels.secondHalfOnly')}
              </Typography>
              <Typography variant="h5">{data.second_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {t('descriptions.chars8To14')}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Partial Cracks Table */}
      <TableContainer>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>{t('columns.username')}</TableCell>
              <TableCell>{t('columns.domain')}</TableCell>
              <TableCell>{t('columns.firstHalf')}</TableCell>
              <TableCell>{t('columns.secondHalf')}</TableCell>
              <TableCell>{t('columns.hashlist')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.partial_crack_details.map((item, idx) => (
              <TableRow key={idx}>
                <TableCell>
                  <Typography variant="body2">{item.username || '—'}</Typography>
                </TableCell>
                <TableCell>
                  <Typography variant="body2">{item.domain || '—'}</Typography>
                </TableCell>
                <TableCell>
                  {item.first_half_cracked ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <LockOpenIcon sx={{ fontSize: 18, color: 'warning.main' }} />
                      <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                        {item.first_half_pwd || '???'}
                      </Typography>
                    </Box>
                  ) : (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <LockIcon sx={{ fontSize: 18, color: 'text.disabled' }} />
                      <Typography variant="body2" color="text.disabled">
                        {t('labels.unknown')}
                      </Typography>
                    </Box>
                  )}
                </TableCell>
                <TableCell>
                  {item.second_half_cracked ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <LockOpenIcon sx={{ fontSize: 18, color: 'warning.main' }} />
                      <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                        {item.second_half_pwd || '???'}
                      </Typography>
                    </Box>
                  ) : (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <LockIcon sx={{ fontSize: 18, color: 'text.disabled' }} />
                      <Typography variant="body2" color="text.disabled">
                        {t('labels.unknown')}
                      </Typography>
                    </Box>
                  )}
                </TableCell>
                <TableCell>
                  <Chip label={item.hashlist_name} size="small" variant="outlined" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      {data.partial_crack_details.length >= 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          {t('messages.top50PartialCracks')}
        </Typography>
      )}
    </Paper>
  );
}
