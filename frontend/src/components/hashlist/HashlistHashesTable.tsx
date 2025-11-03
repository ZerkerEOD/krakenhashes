import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Typography,
  CircularProgress,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  InputAdornment,
  IconButton,
  Tooltip,
  Chip,
} from '@mui/material';
import {
  Search as SearchIcon,
  ContentCopy as CopyIcon,
} from '@mui/icons-material';
import { api } from '../../services/api';
import { useSnackbar } from 'notistack';

interface HashDetail {
  id: string;
  hash_value: string;
  original_hash: string;
  username?: string;
  domain?: string;
  hash_type_id: number;
  is_cracked: boolean;
  password?: string;
  last_updated: string;
  // LM hash partial crack status (only for hash_type_id 3000)
  is_partially_lm_cracked?: boolean;
  lm_first_half_password?: string;
  lm_second_half_password?: string;
}

interface HashlistHashesTableProps {
  hashlistId: string;
  hashlistName: string;
  totalHashes: number;
  crackedHashes: number;
}

export default function HashlistHashesTable({
  hashlistId,
  hashlistName,
  totalHashes,
  crackedHashes,
}: HashlistHashesTableProps) {
  const [data, setData] = useState<HashDetail[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(500);
  const [totalCount, setTotalCount] = useState(0);
  const [searchTerm, setSearchTerm] = useState('');
  const [openAllConfirm, setOpenAllConfirm] = useState(false);
  const { enqueueSnackbar } = useSnackbar();

  const pageSizeOptions = [500, 1000, 1500, 2000, -1];

  const loadData = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      const limit = rowsPerPage === -1 ? -1 : rowsPerPage;
      const offset = page * (rowsPerPage === -1 ? 0 : rowsPerPage);

      const response = await api.get(
        `/api/hashlists/${hashlistId}/hashes?limit=${limit}&offset=${offset}`
      );

      setData(response.data.hashes || []);
      setTotalCount(response.data.total || 0);
    } catch (err) {
      console.error('Error loading hash data:', err);
      setError('Failed to load hashes');
      enqueueSnackbar('Failed to load hashes', { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [page, rowsPerPage, hashlistId, enqueueSnackbar]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const handleChangePage = (event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const newRowsPerPage = parseInt(event.target.value, 10);

    if (newRowsPerPage === -1) {
      setOpenAllConfirm(true);
    } else {
      setRowsPerPage(newRowsPerPage);
      setPage(0);
    }
  };

  const handleConfirmAll = () => {
    setRowsPerPage(-1);
    setPage(0);
    setOpenAllConfirm(false);
    enqueueSnackbar('Loading all results. This may take some time...', {
      variant: 'info',
    });
  };

  const handleCancelAll = () => {
    setOpenAllConfirm(false);
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    enqueueSnackbar('Copied to clipboard', { variant: 'success' });
  };

  const filteredData = data.filter((hash) => {
    if (!searchTerm) return true;
    const searchLower = searchTerm.toLowerCase();
    return (
      hash.original_hash.toLowerCase().includes(searchLower) ||
      (hash.password && hash.password.toLowerCase().includes(searchLower)) ||
      (hash.username && hash.username.toLowerCase().includes(searchLower)) ||
      (hash.domain && hash.domain.toLowerCase().includes(searchLower))
    );
  });

  if (loading && data.length === 0) {
    return (
      <Box
        display="flex"
        justifyContent="center"
        alignItems="center"
        minHeight={400}
      >
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Alert severity="error" sx={{ mt: 2 }}>
        {error}
      </Alert>
    );
  }

  return (
    <Paper sx={{ width: '100%', mb: 2 }}>
      <Box sx={{ p: 2 }}>
        <Box
          sx={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            mb: 2,
            flexWrap: 'wrap',
            gap: 2,
          }}
        >
          <Box>
            <Typography variant="h6" component="div">
              Hashes
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {crackedHashes} of {totalHashes} cracked (
              {totalHashes > 0
                ? Math.round((crackedHashes / totalHashes) * 100)
                : 0}
              %)
            </Typography>
          </Box>
          <TextField
            size="small"
            placeholder="Search hashes..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon />
                </InputAdornment>
              ),
            }}
          />
        </Box>

        <TableContainer sx={{ overflowX: 'auto' }}>
          <Table size="small" aria-label="hashlist hashes table">
            <TableHead>
              <TableRow>
                <TableCell sx={{ minWidth: 300, maxWidth: 600 }}>
                  Original Hash
                </TableCell>
                <TableCell sx={{ minWidth: 120, width: 150 }}>
                  Username
                </TableCell>
                <TableCell sx={{ minWidth: 120, width: 150 }}>
                  Domain
                </TableCell>
                <TableCell sx={{ minWidth: 120, width: 150 }}>
                  Password
                </TableCell>
                <TableCell sx={{ width: 100 }}>Status</TableCell>
                <TableCell sx={{ width: 80 }} align="center">
                  Actions
                </TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {filteredData.map((hash) => (
                <TableRow key={hash.id} hover>
                  <TableCell
                    sx={{
                      fontFamily: 'monospace',
                      fontSize: '0.875rem',
                      wordBreak: 'break-all',
                      maxWidth: 600,
                    }}
                  >
                    {hash.original_hash}
                  </TableCell>
                  <TableCell
                    sx={{
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {hash.username || '-'}
                  </TableCell>
                  <TableCell
                    sx={{
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {hash.domain || '-'}
                  </TableCell>
                  <TableCell
                    sx={{
                      fontFamily: 'monospace',
                      fontSize: '0.875rem',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {hash.is_cracked
                      ? hash.password || '-'
                      : hash.is_partially_lm_cracked
                      ? `[${hash.lm_first_half_password || '?'}][${hash.lm_second_half_password || '?'}]`
                      : '-'}
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={
                        hash.is_cracked
                          ? 'Cracked'
                          : hash.is_partially_lm_cracked
                          ? 'Partial'
                          : 'Pending'
                      }
                      color={
                        hash.is_cracked
                          ? 'success'
                          : hash.is_partially_lm_cracked
                          ? 'warning'
                          : 'default'
                      }
                      size="small"
                    />
                  </TableCell>
                  <TableCell align="center">
                    <Tooltip
                      title={
                        hash.is_cracked && hash.password
                          ? 'Copy password'
                          : 'Copy hash'
                      }
                    >
                      <IconButton
                        size="small"
                        onClick={() =>
                          copyToClipboard(
                            hash.is_cracked && hash.password
                              ? hash.password
                              : hash.original_hash
                          )
                        }
                      >
                        <CopyIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>

        <TablePagination
          rowsPerPageOptions={pageSizeOptions.map((size) => ({
            label: size === -1 ? 'All' : size.toString(),
            value: size,
          }))}
          component="div"
          count={totalCount}
          rowsPerPage={rowsPerPage === -1 ? totalCount : rowsPerPage}
          page={page}
          onPageChange={handleChangePage}
          onRowsPerPageChange={handleChangeRowsPerPage}
        />
      </Box>

      <Dialog open={openAllConfirm} onClose={handleCancelAll}>
        <DialogTitle>Load All Results?</DialogTitle>
        <DialogContent>
          <Typography>
            Loading all {totalCount.toLocaleString()} results may take a
            significant amount of time and could impact performance. Are you
            sure you want to continue?
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCancelAll}>Cancel</Button>
          <Button
            onClick={handleConfirmAll}
            variant="contained"
            color="primary"
          >
            Load All
          </Button>
        </DialogActions>
      </Dialog>
    </Paper>
  );
}
