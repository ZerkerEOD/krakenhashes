import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Typography,
  Button,
  Paper,
  Alert,
  CircularProgress,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TextField,
  Stack,
  Badge,
  ToggleButton,
  ToggleButtonGroup,
} from '@mui/material';
import { 
  Delete as DeleteIcon, 
  Refresh as RefreshIcon,
  Search as SearchIcon,
  FilterList as FilterListIcon,
} from '@mui/icons-material';
import JobsTable from './JobsTable';
import DeleteConfirm from './DeleteConfirm';
import { api } from '../../services/api';
import { JobSummary, PaginationInfo } from '../../types/jobs';

interface JobsResponse {
  jobs: JobSummary[];
  pagination: PaginationInfo;
  status_counts: Record<string, number>;
}

interface Filters {
  status: string | null;
  priority: number | null;
  search: string;
}

const Jobs: React.FC = () => {
  const { t } = useTranslation('jobs');
  // Pagination state
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  
  // Filter state
  const [filters, setFilters] = useState<Filters>({
    status: null,
    priority: null,
    search: '',
  });
  
  // Data state
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [pagination, setPagination] = useState<PaginationInfo | null>(null);
  const [statusCounts, setStatusCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  
  // UI state
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [lastUpdateTime, setLastUpdateTime] = useState(new Date());
  const [isPolling, setIsPolling] = useState(true);
  
  // Refs for cleanup
  const pollingTimer = useRef<NodeJS.Timeout | null>(null);
  const abortController = useRef<AbortController | null>(null);

  // Build query parameters from current state
  const buildQueryParams = useCallback(() => {
    const params = new URLSearchParams();
    params.append('page', page.toString());
    params.append('page_size', pageSize.toString());
    
    if (filters.status) {
      params.append('status', filters.status);
    }
    
    if (filters.priority !== null) {
      params.append('priority', filters.priority.toString());
    }
    
    if (filters.search.trim()) {
      params.append('search', filters.search.trim());
    }
    
    return params.toString();
  }, [page, pageSize, filters]);

  // Fetch jobs with current filters and pagination
  const fetchJobs = useCallback(async (showLoading = false) => {
    // Cancel any ongoing request
    if (abortController.current) {
      abortController.current.abort();
    }
    
    // Create new abort controller
    abortController.current = new AbortController();
    
    try {
      if (showLoading) {
        setLoading(true);
      }
      
      const queryString = buildQueryParams();
      const response = await api.get<JobsResponse>(
        `/api/jobs?${queryString}`,
        { signal: abortController.current.signal }
      );
      
      setJobs(response.data.jobs);
      setPagination(response.data.pagination);
      setStatusCounts(response.data.status_counts || {});
      setError(null);
      setLastUpdateTime(new Date());
    } catch (err: any) {
      // Ignore abort errors
      if (err.name !== 'AbortError') {
        console.error('Failed to fetch jobs:', err);
        setError(err);
      }
    } finally {
      setLoading(false);
    }
  }, [buildQueryParams]);

  // Initial load and when dependencies change
  useEffect(() => {
    fetchJobs(true);
  }, [page, pageSize, filters]);

  // Set up polling
  useEffect(() => {
    if (!isPolling) {
      return;
    }

    // Clear any existing timer
    if (pollingTimer.current) {
      clearInterval(pollingTimer.current);
    }

    // Set up new polling timer
    pollingTimer.current = setInterval(() => {
      fetchJobs(false); // Don't show loading indicator for polling updates
    }, 5000);

    // Cleanup on unmount or when polling is disabled
    return () => {
      if (pollingTimer.current) {
        clearInterval(pollingTimer.current);
      }
    };
  }, [fetchJobs, isPolling]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollingTimer.current) {
        clearInterval(pollingTimer.current);
      }
      if (abortController.current) {
        abortController.current.abort();
      }
    };
  }, []);

  const handleDeleteFinished = async () => {
    setIsDeleting(true);
    try {
      await api.delete('/api/jobs/finished');
      setDeleteDialogOpen(false);
      // Refresh the job list after deletion
      await fetchJobs(true);
    } catch (error) {
      console.error('Failed to delete finished jobs:', error);
    } finally {
      setIsDeleting(false);
    }
  };

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
  };

  const handlePageSizeChange = (newPageSize: number) => {
    setPageSize(newPageSize);
    setPage(1); // Reset to first page when changing page size
  };

  const handleStatusFilter = (event: React.MouseEvent<HTMLElement>, newStatus: string | null) => {
    setFilters(prev => ({ ...prev, status: newStatus === '' ? null : newStatus }));
    setPage(1); // Reset to first page when filtering
  };

  const handlePriorityFilter = (priority: number | null) => {
    setFilters(prev => ({ ...prev, priority }));
    setPage(1); // Reset to first page when filtering
  };

  const handleSearchChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setFilters(prev => ({ ...prev, search: event.target.value }));
    setPage(1); // Reset to first page when searching
  };

  const handleRefresh = () => {
    fetchJobs(true);
  };

  const togglePolling = () => {
    setIsPolling(prev => !prev);
  };

  // Get status badge color
  const getStatusColor = (status: string): 'default' | 'primary' | 'success' | 'error' | 'warning' => {
    switch (status) {
      case 'pending': return 'default';
      case 'running': return 'primary';
      case 'completed': return 'success';
      case 'failed': return 'error';
      default: return 'default';
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      {/* Header */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h4" component="h1">
          {t('page.title') as string}
        </Typography>
        
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
          {/* Polling Status */}
          <Chip
            icon={<RefreshIcon />}
            label={isPolling ? t('autoRefresh.on') as string : t('autoRefresh.off') as string}
            color={isPolling ? 'success' : 'default'}
            variant="outlined"
            size="small"
            onClick={togglePolling}
            sx={{ cursor: 'pointer' }}
          />

          {/* Manual Refresh */}
          <Button
            variant="outlined"
            size="small"
            startIcon={<RefreshIcon />}
            onClick={handleRefresh}
            disabled={loading}
          >
            {t('buttons.refresh') as string}
          </Button>

          {/* Page Size Selector */}
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>{t('pagination.jobsPerPage') as string}</InputLabel>
            <Select
              value={pageSize}
              label={t('pagination.jobsPerPage') as string}
              onChange={(e) => handlePageSizeChange(Number(e.target.value))}
            >
              <MenuItem value={25}>25</MenuItem>
              <MenuItem value={50}>50</MenuItem>
              <MenuItem value={100}>100</MenuItem>
              <MenuItem value={200}>200</MenuItem>
            </Select>
          </FormControl>

          {/* Delete Finished Button */}
          <Button
            variant="outlined"
            color="error"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
            disabled={!jobs || jobs.length === 0}
          >
            {t('buttons.deleteFinished') as string}
          </Button>
        </Box>
      </Box>

      {/* Filters Section */}
      <Paper sx={{ p: 2, mb: 2 }}>
        <Stack spacing={2}>
          <Box sx={{ display: 'flex', gap: 2, alignItems: 'center', flexWrap: 'wrap' }}>
            {/* Search Field */}
            <TextField
              size="small"
              placeholder={t('filters.searchPlaceholder') as string}
              value={filters.search}
              onChange={handleSearchChange}
              InputProps={{
                startAdornment: <SearchIcon sx={{ mr: 1, color: 'text.secondary' }} />,
              }}
              sx={{ minWidth: 300 }}
            />

            {/* Priority Filter */}
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{t('filters.priority') as string}</InputLabel>
              <Select
                value={filters.priority ?? ''}
                label={t('filters.priority') as string}
                onChange={(e) => handlePriorityFilter(e.target.value === '' ? null : Number(e.target.value))}
              >
                <MenuItem value="">{t('filters.all') as string}</MenuItem>
                <MenuItem value={1}>{t('priority.low') as string}</MenuItem>
                <MenuItem value={2}>{t('priority.medium') as string}</MenuItem>
                <MenuItem value={3}>{t('priority.high') as string}</MenuItem>
                <MenuItem value={4}>{t('priority.critical') as string}</MenuItem>
                <MenuItem value={5}>{t('priority.maximum') as string}</MenuItem>
              </Select>
            </FormControl>
          </Box>

          {/* Status Filter Buttons */}
          <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap' }}>
            <Typography variant="body2" sx={{ mr: 1 }}>
              {t('filters.status') as string}:
            </Typography>
            <ToggleButtonGroup
              value={filters.status}
              exclusive
              onChange={handleStatusFilter}
              size="small"
            >
              <ToggleButton value="">
                <Badge badgeContent={Object.values(statusCounts).reduce((a, b) => a + b, 0)} color="default">
                  {t('filters.all') as string}
                </Badge>
              </ToggleButton>
              <ToggleButton value="pending">
                <Badge badgeContent={statusCounts.pending || 0} color="default">
                  {t('status.pending') as string}
                </Badge>
              </ToggleButton>
              <ToggleButton value="running">
                <Badge badgeContent={statusCounts.running || 0} color="primary">
                  {t('status.running') as string}
                </Badge>
              </ToggleButton>
              <ToggleButton value="completed">
                <Badge badgeContent={statusCounts.completed || 0} color="success">
                  {t('status.completed') as string}
                </Badge>
              </ToggleButton>
              <ToggleButton value="failed">
                <Badge badgeContent={statusCounts.failed || 0} color="error">
                  {t('status.failed') as string}
                </Badge>
              </ToggleButton>
            </ToggleButtonGroup>
          </Box>
        </Stack>
      </Paper>

      {/* Error Alert */}
      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {t('errors.fetchFailed', { message: error.message }) as string}
          {isPolling && ` ${t('errors.willRetry') as string}`}
        </Alert>
      )}

      {/* Last Update Timestamp */}
      {!loading && jobs.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ mb: 2, display: 'block' }}>
          {t('lastUpdated', { time: lastUpdateTime.toLocaleTimeString() }) as string}
          {filters.status || filters.priority !== null || filters.search ? ` ${t('filtered') as string}` : ''}
        </Typography>
      )}

      {/* Jobs Table */}
      <Paper sx={{ width: '100%', overflow: 'hidden' }}>
        {loading && jobs.length === 0 ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', p: 4 }}>
            <CircularProgress />
            <Typography variant="body1" sx={{ ml: 2 }}>
              {t('loading') as string}
            </Typography>
          </Box>
        ) : (
          <JobsTable
            jobs={jobs}
            pagination={pagination ?? undefined}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
            currentPage={page}
            pageSize={pageSize}
            onJobUpdated={fetchJobs}
          />
        )}
      </Paper>

      {/* Delete Confirmation Dialog */}
      <DeleteConfirm
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        onConfirm={handleDeleteFinished}
        isLoading={isDeleting}
        title={t('dialogs.deleteFinished.title') as string}
        message={t('dialogs.deleteFinished.message') as string}
      />
    </Box>
  );
};

export default Jobs;