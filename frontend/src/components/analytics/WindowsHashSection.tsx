/**
 * Windows Hash Analytics Section
 * Displays comprehensive statistics for Windows-related hash types including
 * NTLM, LM, NetNTLMv1/v2, DCC/DCC2, and Kerberos
 */
import React from 'react';
import {
  Box,
  Paper,
  Typography,
  Grid,
  Card,
  CardContent,
  Chip,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Alert,
  AlertTitle,
} from '@mui/material';
import {
  Security as SecurityIcon,
  Warning as WarningIcon,
  CheckCircle as CheckCircleIcon,
  Link as LinkIcon,
} from '@mui/icons-material';

interface WindowsHashSectionProps {
  data: any; // WindowsHashStats type
}

export default function WindowsHashSection({ data }: WindowsHashSectionProps) {
  if (!data) {
    return null;
  }

  const { overview, ntlm, lm, netntlmv1, netntlmv2, dcc, dcc2, kerberos, linkedCorrelation } = data;

  // Helper to format percentage
  const formatPercentage = (value: number) => {
    return value.toFixed(2) + '%';
  };

  // Helper to render hash type card
  const renderHashTypeCard = (title: string, stats: any, color: string, showDetails?: boolean) => {
    if (!stats || stats.total === 0) {
      return null;
    }

    return (
      <Grid item xs={12} md={6} lg={4}>
        <Card>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
              <SecurityIcon sx={{ color, mr: 1 }} />
              <Typography variant="h6">{title}</Typography>
            </Box>
            <Box sx={{ mb: 1 }}>
              <Typography variant="body2" color="text.secondary">
                Total: <strong>{stats.total.toLocaleString()}</strong>
              </Typography>
              <Typography variant="body2" color="text.secondary">
                Cracked: <strong>{stats.cracked.toLocaleString()}</strong>
              </Typography>
              <Typography variant="body2" color="text.secondary">
                Percentage: <strong>{formatPercentage(stats.percentage)}</strong>
              </Typography>
            </Box>
            {showDetails && stats.under_8 !== undefined && (
              <>
                <Divider sx={{ my: 1 }} />
                <Typography variant="caption" color="text.secondary">
                  Length Distribution (based on hash):
                </Typography>
                <Typography variant="body2">
                  ≤7 chars: {stats.under_8.toLocaleString()}
                </Typography>
                <Typography variant="body2">
                  8-14 chars: {stats['8_to_14'].toLocaleString()}
                </Typography>
                {stats.partially_cracked > 0 && (
                  <>
                    <Divider sx={{ my: 1 }} />
                    <Box sx={{ display: 'flex', alignItems: 'center' }}>
                      <WarningIcon sx={{ fontSize: 16, color: 'warning.main', mr: 0.5 }} />
                      <Typography variant="body2" color="warning.main">
                        Partially Cracked: {stats.partially_cracked.toLocaleString()}
                      </Typography>
                    </Box>
                  </>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </Grid>
    );
  };

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
        <SecurityIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          Windows Hash Analytics
        </Typography>
      </Box>

      {/* Linked Hashlist Disclaimer */}
      {overview.linked_pairs > 0 && (
        <Alert severity="info" sx={{ mb: 3 }}>
          <AlertTitle>Linked Hashlist Analysis</AlertTitle>
          This analysis includes <strong>{overview.linked_pairs.toLocaleString()}</strong> LM/NTLM hash pairs that
          were linked during upload (typically from pwdump-format files). Linked pairs are counted as a single entry
          throughout all analytics. If either the LM or NTLM hash from a linked pair is cracked, it counts as one
          cracked entry.
          <Box component="span" sx={{ display: 'block', mt: 1, fontSize: '0.9em', fontStyle: 'italic' }}>
            Note: Hashes with the same username from separate uploads are counted as separate entries unless
            explicitly linked during the upload process.
          </Box>
        </Alert>
      )}

      {/* Overview Card */}
      <Card sx={{ mb: 3, bgcolor: 'primary.50' }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Overview
          </Typography>
          <Grid container spacing={2}>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                Total Hash Records
              </Typography>
              <Typography variant="h4">{overview.total_windows.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                Includes all hash types
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                Unique Users
              </Typography>
              <Typography variant="h4">{overview.unique_users.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                Distinct usernames
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                Cracked
              </Typography>
              <Typography variant="h4">{overview.cracked_windows.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                Successfully cracked
              </Typography>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Typography variant="body2" color="text.secondary">
                Success Rate
              </Typography>
              <Typography variant="h4">{formatPercentage(overview.percentage_windows)}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                Overall percentage
              </Typography>
            </Grid>
          </Grid>
          {overview.linked_pairs > 0 && (
            <Box sx={{ mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}>
              <Typography variant="body2" color="text.secondary">
                <LinkIcon sx={{ fontSize: 16, mr: 0.5, verticalAlign: 'text-bottom' }} />
                <strong>{overview.linked_pairs.toLocaleString()}</strong> LM/NTLM linked pairs found
              </Typography>
            </Box>
          )}
        </CardContent>
      </Card>

      {/* Hash Type Cards */}
      <Typography variant="h6" gutterBottom sx={{ mt: 3, mb: 2 }}>
        Hash Types
      </Typography>
      <Grid container spacing={2}>
        {renderHashTypeCard('NTLM', ntlm, 'primary.main')}
        {renderHashTypeCard('LM', lm, 'error.main', true)}
        {renderHashTypeCard('NetNTLMv1', netntlmv1, 'warning.main')}
        {renderHashTypeCard('NetNTLMv2', netntlmv2, 'info.main')}
        {renderHashTypeCard('DCC (MS Cache)', dcc, 'secondary.main')}
        {renderHashTypeCard('DCC2 (MS Cache 2)', dcc2, 'secondary.main')}
      </Grid>

      {/* Kerberos Section */}
      {kerberos && kerberos.total > 0 && (
        <Box sx={{ mt: 3 }}>
          <Typography variant="h6" gutterBottom>
            Kerberos
          </Typography>
          <Card>
            <CardContent>
              <Box sx={{ mb: 2 }}>
                <Typography variant="body2" color="text.secondary">
                  Total: <strong>{kerberos.total.toLocaleString()}</strong> | Cracked:{' '}
                  <strong>{kerberos.cracked.toLocaleString()}</strong> | Success Rate:{' '}
                  <strong>{formatPercentage(kerberos.percentage)}</strong>
                </Typography>
              </Box>
              {kerberos.by_type && Object.keys(kerberos.by_type).length > 0 && (
                <>
                  <Divider sx={{ my: 2 }} />
                  <Typography variant="subtitle2" gutterBottom>
                    Encryption Types
                  </Typography>
                  <TableContainer>
                    <Table size="small">
                      <TableHead>
                        <TableRow>
                          <TableCell>Type</TableCell>
                          <TableCell align="right">Total</TableCell>
                          <TableCell align="right">Cracked</TableCell>
                          <TableCell align="right">Percentage</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {Object.entries(kerberos.by_type).map(([type, stats]: [string, any]) => (
                          <TableRow key={type}>
                            <TableCell>
                              {type === 'etype_23' && (
                                <Chip label="RC4 (etype 23)" size="small" color="warning" />
                              )}
                              {type === 'etype_17' && (
                                <Chip label="AES128 (etype 17)" size="small" color="success" />
                              )}
                              {type === 'etype_18' && (
                                <Chip label="AES256 (etype 18)" size="small" color="success" />
                              )}
                            </TableCell>
                            <TableCell align="right">{stats.total.toLocaleString()}</TableCell>
                            <TableCell align="right">{stats.cracked.toLocaleString()}</TableCell>
                            <TableCell align="right">{formatPercentage(stats.percentage)}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </TableContainer>
                </>
              )}
            </CardContent>
          </Card>
        </Box>
      )}

      {/* Linked Hash Correlation */}
      {linkedCorrelation && linkedCorrelation.total_linked_pairs > 0 && (
        <Box sx={{ mt: 3 }}>
          <Typography variant="h6" gutterBottom sx={{ display: 'flex', alignItems: 'center' }}>
            <LinkIcon sx={{ mr: 1 }} />
            LM/NTLM Linked Hash Correlation
          </Typography>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                Total Linked Pairs: <strong>{linkedCorrelation.total_linked_pairs.toLocaleString()}</strong>
              </Typography>
              <Grid container spacing={2} sx={{ mt: 1 }}>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: 'success.50', borderRadius: 1 }}>
                    <CheckCircleIcon sx={{ color: 'success.main', fontSize: 32 }} />
                    <Typography variant="h6">{linkedCorrelation.both_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">Both Cracked</Typography>
                    <Typography variant="body2" color="text.secondary">
                      ({formatPercentage(linkedCorrelation.percentage_both)})
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: 'info.50', borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.only_ntlm_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">NTLM Only</Typography>
                    <Typography variant="body2" color="text.secondary">
                      (LM derivable)
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: 'warning.50', borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.only_lm_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">LM Only</Typography>
                    <Typography variant="body2" color="text.secondary">
                      (NTLM unknown)
                    </Typography>
                  </Box>
                </Grid>
                <Grid item xs={12} sm={6} md={3}>
                  <Box sx={{ textAlign: 'center', p: 2, bgcolor: 'grey.100', borderRadius: 1 }}>
                    <Typography variant="h6">{linkedCorrelation.neither_cracked.toLocaleString()}</Typography>
                    <Typography variant="caption">Neither Cracked</Typography>
                  </Box>
                </Grid>
              </Grid>
            </CardContent>
          </Card>
        </Box>
      )}
    </Paper>
  );
}
