import React, { useState, useEffect, useCallback, useRef } from 'react';
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
  MenuItem,
  FormControl,
  Select,
  InputLabel,
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
  Download as DownloadIcon,
  FilterList as FilterListIcon,
  Clear as ClearIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import { useSnackbar } from 'notistack';
import { CrackedHash, PotResponse } from '../../services/pot';

interface PotTableProps {
  title: string;
  fetchData: (limit: number, offset: number, search?: string) => Promise<PotResponse>;
  filterParam?: string;
  filterValue?: string;
  contextType: 'master' | 'hashlist' | 'client' | 'job';
  contextName: string;
  contextId?: string;
}

export default function PotTable({ title, fetchData, filterParam, filterValue, contextType, contextName, contextId }: PotTableProps) {
  const { t } = useTranslation('pot');
  const [data, setData] = useState<CrackedHash[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(500);
  const [totalCount, setTotalCount] = useState(0);

  // Server-side search state
  const [searchInput, setSearchInput] = useState('');      // What user types in search bar
  const [activeSearch, setActiveSearch] = useState('');    // Search term sent to server
  const [isSearching, setIsSearching] = useState(false);   // Loading state for search

  // Client-side filter state (filters current page)
  const [filterTerm, setFilterTerm] = useState('');

  const [openAllConfirm, setOpenAllConfirm] = useState(false);
  const [hasUsernameData, setHasUsernameData] = useState(false);
  const [hasDomainData, setHasDomainData] = useState(false);
  const [checkedForUsernames, setCheckedForUsernames] = useState(false);
  const [downloadingFormat, setDownloadingFormat] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();

  // Request ID to prevent stale responses from overwriting newer data
  const requestIdRef = useRef(0);

  const pageSizeOptions = [500, 1000, 1500, 2000, -1];

  const loadData = useCallback(async () => {
    // Increment request ID to track this specific request
    const currentRequestId = ++requestIdRef.current;

    try {
      setLoading(true);
      setIsSearching(activeSearch !== '');
      setError(null);

      const limit = rowsPerPage === -1 ? 999999 : rowsPerPage;
      const offset = page * (rowsPerPage === -1 ? 0 : rowsPerPage);

      // Pass search parameter if active
      const response = await fetchData(limit, offset, activeSearch || undefined);

      // Only update state if this is still the most recent request
      if (currentRequestId !== requestIdRef.current) {
        console.log('Ignoring stale response', { currentRequestId, latestId: requestIdRef.current });
        return;
      }

      setData(response.hashes);
      setTotalCount(response.total_count);

      // Check if any hash has username or domain data
      const hasUsername = response.hashes.some(hash => hash.username && hash.username.trim() !== '');
      const hasDomain = response.hashes.some(hash => hash.domain && hash.domain.trim() !== '');
      setHasUsernameData(hasUsername);
      setHasDomainData(hasDomain);
    } catch (err) {
      // Only show error if this is still the most recent request
      if (currentRequestId !== requestIdRef.current) {
        return;
      }
      console.error('Error loading pot data:', err);
      setError(t('errors.loadFailed') as string);
      enqueueSnackbar(t('errors.loadFailed') as string, { variant: 'error' });
    } finally {
      // Only update loading state if this is still the most recent request
      if (currentRequestId === requestIdRef.current) {
        setLoading(false);
        setIsSearching(false);
      }
    }
  }, [page, rowsPerPage, fetchData, activeSearch, enqueueSnackbar]);

  // Handle search submission (button click or Enter key)
  const handleSearch = useCallback(() => {
    const trimmedSearch = searchInput.trim();
    if (trimmedSearch !== activeSearch) {
      setActiveSearch(trimmedSearch);
      setPage(0);  // Reset to first page on new search
      setFilterTerm('');  // Clear local filter when doing server search
    }
  }, [searchInput, activeSearch]);

  // Handle Enter key in search input
  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSearch();
    }
  };

  // Clear search and reset to normal view
  const handleClearSearch = useCallback(() => {
    setSearchInput('');
    setActiveSearch('');
    setPage(0);
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Check for username data in the entire dataset on mount
  useEffect(() => {
    if (!checkedForUsernames && totalCount > 0) {
      // Make a quick request to check if any usernames exist
      // We'll check the current data, and if we don't find any, we could make a separate call
      // For now, let's just check current data and set it as checked
      setCheckedForUsernames(true);
    }
  }, [totalCount, checkedForUsernames]);

  const handleChangePage = (event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event: React.ChangeEvent<HTMLInputElement>) => {
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
    enqueueSnackbar(t('dialogs.loadingAll') as string, { variant: 'info' });
  };

  const handleCancelAll = () => {
    setOpenAllConfirm(false);
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    enqueueSnackbar(t('notifications.copiedToClipboard') as string, { variant: 'success' });
  };

  const downloadFormat = async (format: 'hash-pass' | 'user-pass' | 'user' | 'pass' | 'domain-user' | 'domain-user-pass') => {
    try {
      setDownloadingFormat(format);

      // Build the download URL based on context
      let url = '';
      if (contextType === 'master') {
        url = `/api/pot/download/${format}`;
      } else if (contextType === 'hashlist' && contextId) {
        url = `/api/pot/hashlist/${contextId}/download/${format}`;
      } else if (contextType === 'client' && contextId) {
        url = `/api/pot/client/${contextId}/download/${format}`;
      } else if (contextType === 'job' && contextId) {
        url = `/api/pot/job/${contextId}/download/${format}`;
      }

      const response = await api.get(url, { responseType: 'blob' });

      // Create blob and download
      const blob = new Blob([response.data], { type: 'text/plain' });
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = downloadUrl;

      // Get filename from Content-Disposition header or use default
      const contentDisposition = response.headers['content-disposition'];
      let filename = `${contextName}-${format}.lst`;
      if (contentDisposition) {
        // RFC-compliant regex for filename extraction
        const filenameMatch = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
        if (filenameMatch && filenameMatch[3]) {
          filename = filenameMatch[3];
        } else {
          // Fallback for unquoted filenames
          const fallbackMatch = contentDisposition.match(/filename=([^;\n]*)/i);
          if (fallbackMatch && fallbackMatch[1]) {
            filename = fallbackMatch[1].trim();
          }
        }
      }

      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(downloadUrl);

      enqueueSnackbar(t('export.downloaded', { filename }) as string, { variant: 'success' });
    } catch (err) {
      console.error('Error downloading format:', err);
      enqueueSnackbar(t('export.downloadFailed') as string, { variant: 'error' });
    } finally {
      setDownloadingFormat(null);
    }
  };

  const exportData = () => {
    const exportText = data
      .map(hash => `${hash.original_hash}:${hash.password}`)
      .join('\n');
    
    const blob = new Blob([exportText], { type: 'text/plain' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `cracked_hashes_${new Date().toISOString().split('T')[0]}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(url);
    
    enqueueSnackbar(t('notifications.exported') as string, { variant: 'success' });
  };

  // Client-side filtering (on current page data)
  const filteredData = data.filter(hash => {
    if (!filterTerm) return true;
    const filterLower = filterTerm.toLowerCase();
    return (
      hash.original_hash.toLowerCase().includes(filterLower) ||
      hash.password.toLowerCase().includes(filterLower) ||
      (hash.username && hash.username.toLowerCase().includes(filterLower)) ||
      (hash.domain && hash.domain.toLowerCase().includes(filterLower))
    );
  });

  if (loading && data.length === 0) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight={400}>
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
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 2 }}>
          <Typography variant="h6" component="div">
            {title}
            {filterParam && filterValue && (
              <Typography variant="body2" color="text.secondary">
                {t('filter.filteredBy', { param: filterParam, value: filterValue }) as string}
              </Typography>
            )}
          </Typography>
          <Box sx={{ display: 'flex', gap: 2, alignItems: 'center', flexWrap: 'wrap' }}>
            {/* Server-side search bar with button inside */}
            <TextField
              size="small"
              placeholder={t('search.placeholder') as string}
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              onKeyDown={handleSearchKeyDown}
              disabled={isSearching}
              sx={{ minWidth: 250 }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon color={activeSearch ? 'primary' : 'inherit'} />
                  </InputAdornment>
                ),
                endAdornment: (
                  <InputAdornment position="end">
                    {activeSearch && (
                      <IconButton
                        size="small"
                        onClick={handleClearSearch}
                        sx={{ mr: 0.5 }}
                      >
                        <ClearIcon fontSize="small" />
                      </IconButton>
                    )}
                    <Button
                      size="small"
                      variant="contained"
                      onClick={handleSearch}
                      disabled={isSearching || searchInput === activeSearch}
                      sx={{ minWidth: 'auto', px: 1.5 }}
                    >
                      {isSearching ? <CircularProgress size={16} color="inherit" /> : t('search.button') as string}
                    </Button>
                  </InputAdornment>
                ),
              }}
            />
            {/* Client-side filter for current page */}
            <TextField
              size="small"
              placeholder={t('search.filterPlaceholder') as string}
              value={filterTerm}
              onChange={(e) => setFilterTerm(e.target.value)}
              sx={{ minWidth: 180 }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <FilterListIcon />
                  </InputAdornment>
                ),
              }}
            />
            <Tooltip title={t('export.exportVisible') as string}>
              <IconButton onClick={exportData} disabled={filteredData.length === 0}>
                <DownloadIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </Box>

        {/* Active search indicator */}
        {activeSearch && (
          <Box sx={{ mb: 2, display: 'flex', alignItems: 'center', gap: 1 }}>
            <Chip
              label={`Search: "${activeSearch}"`}
              onDelete={handleClearSearch}
              color="primary"
              variant="outlined"
              size="small"
            />
            <Typography variant="body2" color="text.secondary">
              {t('search.resultsFound', { count: totalCount }) as string}
            </Typography>
          </Box>
        )}
        
        <Box sx={{ display: 'flex', gap: 1, mb: 2, flexWrap: 'wrap' }}>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('hash-pass')}
            disabled={downloadingFormat !== null}
          >
            {t('export.hashPass') as string}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('user-pass')}
            disabled={downloadingFormat !== null || !hasUsernameData}
          >
            {t('export.userPass') as string}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('user')}
            disabled={downloadingFormat !== null || !hasUsernameData}
          >
            {t('export.username') as string}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('pass')}
            disabled={downloadingFormat !== null}
          >
            {t('export.password') as string}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('domain-user')}
            disabled={downloadingFormat !== null || !hasUsernameData}
          >
            {t('export.domainUser') as string}
          </Button>
          <Button
            size="small"
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={() => downloadFormat('domain-user-pass')}
            disabled={downloadingFormat !== null || !hasUsernameData}
          >
            {t('export.domainUserPass') as string}
          </Button>
        </Box>
        
        <TableContainer>
          <Table size="small" aria-label="cracked hashes table" sx={{ tableLayout: 'fixed' }}>
            <TableHead>
              <TableRow>
                <TableCell sx={{ width: '45%' }}>{t('columns.originalHash') as string}</TableCell>
                <TableCell sx={{ width: '12%' }}>{t('columns.domain') as string}</TableCell>
                <TableCell sx={{ width: '12%' }}>{t('columns.username') as string}</TableCell>
                <TableCell sx={{ width: '12%' }}>{t('columns.password') as string}</TableCell>
                <TableCell sx={{ width: '9%' }}>{t('columns.hashType') as string}</TableCell>
                <TableCell sx={{ width: '10%' }} align="center">{t('columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            {/* translate="no" prevents browser translation services from translating sensitive data */}
            <TableBody translate="no" className="notranslate">
              {filteredData.map((hash) => (
                <TableRow key={hash.id} hover>
                  <TableCell sx={{
                    fontFamily: 'monospace',
                    fontSize: '0.875rem',
                    overflow: 'auto',
                    whiteSpace: 'nowrap',
                    maxWidth: 0
                  }}>
                    {hash.original_hash}
                  </TableCell>
                  {hash.domain ? (
                    <Tooltip title={t('tooltips.copyDomain') as string}>
                      <TableCell
                        onClick={() => copyToClipboard(hash.domain!)}
                        sx={{
                          cursor: 'pointer',
                          '&:hover': {
                            backgroundColor: 'action.hover',
                            textDecoration: 'underline',
                          },
                        }}
                      >
                        {hash.domain}
                      </TableCell>
                    </Tooltip>
                  ) : (
                    <TableCell>-</TableCell>
                  )}
                  {hash.username ? (
                    <Tooltip title={t('tooltips.copyUsername') as string}>
                      <TableCell
                        onClick={() => copyToClipboard(hash.username!)}
                        sx={{
                          cursor: 'pointer',
                          '&:hover': {
                            backgroundColor: 'action.hover',
                            textDecoration: 'underline',
                          },
                        }}
                      >
                        {hash.username}
                      </TableCell>
                    </Tooltip>
                  ) : (
                    <TableCell>-</TableCell>
                  )}
                  <Tooltip title={t('tooltips.copyPassword') as string}>
                    <TableCell
                      onClick={() => copyToClipboard(hash.password)}
                      sx={{
                        fontFamily: 'monospace',
                        fontSize: '0.875rem',
                        cursor: 'pointer',
                        '&:hover': {
                          backgroundColor: 'action.hover',
                          textDecoration: 'underline',
                        },
                      }}
                    >
                      {hash.password}
                    </TableCell>
                  </Tooltip>
                  <TableCell>{hash.hash_type_id}</TableCell>
                  <TableCell align="center">
                    <Tooltip title={t('tooltips.copyHash') as string}>
                      <IconButton
                        size="small"
                        onClick={() => copyToClipboard(`${hash.original_hash}:${hash.password}`)}
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
          rowsPerPageOptions={pageSizeOptions.map(size => ({
            label: size === -1 ? t('pagination.all') as string : size.toString(),
            value: size,
          }))}
          component="div"
          count={totalCount}
          rowsPerPage={rowsPerPage === -1 ? totalCount : rowsPerPage}
          page={page}
          onPageChange={handleChangePage}
          onRowsPerPageChange={handleChangeRowsPerPage}
          labelRowsPerPage={t('pagination.rowsPerPage', { ns: 'common' }) as string}
        />
      </Box>

      <Dialog open={openAllConfirm} onClose={handleCancelAll}>
        <DialogTitle>{t('dialogs.loadAllTitle') as string}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('dialogs.loadAllMessage', { value: totalCount.toLocaleString() }) as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCancelAll}>{t('dialogs.cancel') as string}</Button>
          <Button onClick={handleConfirmAll} variant="contained" color="primary">
            {t('dialogs.loadAll') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Paper>
  );
}