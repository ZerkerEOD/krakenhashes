/**
 * LM-to-NTLM Mask Generation Section
 * Displays generated hashcat masks from cracked LM passwords
 * to assist in cracking the case-sensitive NTLM versions
 */
import React, { useState } from 'react';
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
  Button,
  Grid,
  Card,
  CardContent,
  TablePagination,
} from '@mui/material';
import {
  VpnKey as MaskIcon,
  Download as DownloadIcon,
  TrendingUp as TrendingUpIcon,
} from '@mui/icons-material';

interface LMToNTLMMaskData {
  total_lm_cracked: number;
  total_masks_generated: number;
  total_estimated_keyspace: number;
  masks: Array<{
    mask: string;
    lm_pattern: string;
    count: number;
    percentage: number;
    match_percentage: number;
    estimated_keyspace: number;
    example_lm: string;
  }>;
}

interface LMToNTLMMasksSectionProps {
  data: LMToNTLMMaskData | null;
}

export default function LMToNTLMMasksSection({ data }: LMToNTLMMasksSectionProps) {
  const [page, setPage] = useState(0);
  const [rowsPerPage] = useState(50);

  if (!data || data.total_masks_generated === 0 || !data.masks || data.masks.length === 0) {
    return null;
  }

  const handleChangePage = (_event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleExportHCMask = () => {
    // Generate .hcmask file format
    const hcmaskContent = data.masks.map((m) => m.mask).join('\n');
    const blob = new Blob([hcmaskContent], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'lm_to_ntlm_masks.hcmask';
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleExportTxt = () => {
    // Generate detailed .txt format with statistics
    const txtContent = [
      '# LM-to-NTLM Hashcat Masks',
      `# Generated from ${data.total_lm_cracked} cracked LM passwords`,
      `# Total masks: ${data.total_masks_generated}`,
      `# Total estimated keyspace: ${data.total_estimated_keyspace.toLocaleString()}`,
      '#',
      '# Format: mask | pattern | count | match% | keyspace | example',
      '',
      ...data.masks.map(
        (m) =>
          `${m.mask} | ${m.lm_pattern} | ${m.count} | ${m.match_percentage.toFixed(2)}% | ${m.estimated_keyspace} | ${m.example_lm}`
      ),
    ].join('\n');

    const blob = new Blob([txtContent], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'lm_to_ntlm_masks_detailed.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  const formatKeyspace = (keyspace: number) => {
    if (keyspace >= 1e12) return `${(keyspace / 1e12).toFixed(2)}T`;
    if (keyspace >= 1e9) return `${(keyspace / 1e9).toFixed(2)}B`;
    if (keyspace >= 1e6) return `${(keyspace / 1e6).toFixed(2)}M`;
    if (keyspace >= 1e3) return `${(keyspace / 1e3).toFixed(2)}K`;
    return keyspace.toString();
  };

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  const paginatedMasks = data.masks.slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage);

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
        <Box sx={{ display: 'flex', alignItems: 'center' }}>
          <MaskIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
          <Typography variant="h5" component="h2">
            LM-to-NTLM Mask Generation
          </Typography>
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button variant="outlined" size="small" startIcon={<DownloadIcon />} onClick={handleExportHCMask}>
            Export .hcmask
          </Button>
          <Button variant="outlined" size="small" startIcon={<DownloadIcon />} onClick={handleExportTxt}>
            Export .txt
          </Button>
        </Box>
      </Box>

      <Alert severity="info" sx={{ mb: 3 }}>
        These hashcat masks are generated from cracked LM passwords to help crack the case-sensitive NTLM versions.
        Masks are sorted by <strong>match percentage</strong> (effectiveness) to prioritize the most likely patterns.
        Use with hashcat's <code>-a 3</code> (mask attack) mode.
      </Alert>

      {/* Summary Statistics */}
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                LM Passwords Analyzed
              </Typography>
              <Typography variant="h5">{data.total_lm_cracked.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                Masks Generated
              </Typography>
              <Typography variant="h5">{data.total_masks_generated.toLocaleString()}</Typography>
            </CardContent>
          </Card>
        </Grid>
        <Grid item xs={12} sm={6} md={4}>
          <Card>
            <CardContent>
              <Typography variant="body2" color="text.secondary">
                Total Estimated Keyspace
              </Typography>
              <Typography variant="h5">{formatKeyspace(data.total_estimated_keyspace)}</Typography>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Masks Table */}
      <TableContainer>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Mask</TableCell>
              <TableCell>LM Pattern</TableCell>
              <TableCell align="right">Count</TableCell>
              <TableCell align="right">
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5 }}>
                  <TrendingUpIcon sx={{ fontSize: 16, color: 'success.main' }} />
                  Match %
                </Box>
              </TableCell>
              <TableCell align="right">Est. Keyspace</TableCell>
              <TableCell>Example LM</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {paginatedMasks.map((item, idx) => (
              <TableRow key={idx}>
                <TableCell>
                  <Typography variant="body2" sx={{ fontFamily: 'monospace', fontWeight: 'bold' }}>
                    {item.mask}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Chip label={item.lm_pattern} size="small" variant="outlined" />
                </TableCell>
                <TableCell align="right">
                  <Typography variant="body2">{item.count.toLocaleString()}</Typography>
                </TableCell>
                <TableCell align="right">
                  <Chip
                    label={formatPercentage(item.match_percentage)}
                    size="small"
                    color={item.match_percentage > 10 ? 'success' : item.match_percentage > 5 ? 'warning' : 'default'}
                    sx={{ fontWeight: 'bold' }}
                  />
                </TableCell>
                <TableCell align="right">
                  <Typography variant="body2" color="text.secondary">
                    {formatKeyspace(item.estimated_keyspace)}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                    {item.example_lm}
                  </Typography>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      {/* Pagination */}
      <TablePagination
        component="div"
        count={data.masks.length}
        page={page}
        onPageChange={handleChangePage}
        rowsPerPage={rowsPerPage}
        rowsPerPageOptions={[50]}
        labelDisplayedRows={({ from, to, count }) => `${from}-${to} of ${count} masks`}
      />

      {data.masks.length > 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          Masks sorted by match percentage (most effective first). Higher match percentage indicates the mask matches
          more LM passwords.
        </Typography>
      )}
    </Paper>
  );
}
