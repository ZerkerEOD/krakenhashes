/**
 * LM Partial Cracks Section
 * Displays partially cracked LM hashes (one half cracked, other half unknown)
 */
import React from 'react';
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
  if (!data || data.total_partial === 0) {
    return null;
  }

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <WarningIcon sx={{ fontSize: 32, color: 'warning.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          LM Partial Cracks
        </Typography>
      </Box>

      <Alert severity="warning" sx={{ mb: 3 }}>
        These LM hashes are partially cracked, making full compromise significantly easier. Immediate password
        reset and LM hash storage disablement required.
      </Alert>

      {/* Summary Statistics */}
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                Total Partial
              </Typography>
              <Typography variant="h5">{data.total_partial.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                {formatPercentage(data.percentage_partial)} of LM hashes
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                First Half Only
              </Typography>
              <Typography variant="h5">{data.first_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                Chars 1-7 cracked
              </Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={3}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                Second Half Only
              </Typography>
              <Typography variant="h5">{data.second_half_only.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">
                Chars 8-14 cracked
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
              <TableCell>Username</TableCell>
              <TableCell>Domain</TableCell>
              <TableCell>First Half (1-7)</TableCell>
              <TableCell>Second Half (8-14)</TableCell>
              <TableCell>Hashlist</TableCell>
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
                        Unknown
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
                        Unknown
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
          Showing top 50 partial cracks
        </Typography>
      )}
    </Paper>
  );
}
