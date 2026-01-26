/**
 * Dashboard - Main dashboard component for authenticated users
 * 
 * Features:
 *   - Overview of system status
 *   - Quick access to key features
 *   - User session management
 *   - System metrics display
 * 
 * Dependencies:
 *   - react-router-dom for navigation
 *   - @mui/material for UI components
 *   - ../services/auth for authentication
 *   - ../types/auth for type definitions
 * 
 * Error Scenarios:
 *   - Session expiration handling
 *   - Logout failures: Network errors, server errors
 *   - Navigation errors: Route access denied
 *   - Data loading failures: API timeouts, invalid responses
 * 
 * Usage Example:
 * ```tsx
 * // In protected route
 * <Route 
 *   path="/dashboard" 
 *   element={<Dashboard />} 
 * />
 * 
 * // With error boundary
 * <ErrorBoundary>
 *   <Dashboard />
 * </ErrorBoundary>
 * ```
 * 
 * Performance Considerations:
 *   - Lazy loading of dashboard widgets using React.lazy
 *   - Data fetching with caching and invalidation
 *   - Memoized component state using useMemo
 *   - Debounced logout handling to prevent multiple calls
 * 
 * @returns {JSX.Element} Dashboard component
 */

import React, { useMemo, useState, useEffect, useCallback, useRef } from 'react';
import {
  Box,
  Typography,
  Grid,
  Paper,
  Divider,
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
  Button,
  LinearProgress
} from '@mui/material';
import { 
  Delete as DeleteIcon, 
  Refresh as RefreshIcon,
  Search as SearchIcon,
  FilterList as FilterListIcon,
} from '@mui/icons-material';
import JobsTable from '../pages/Jobs/JobsTable';
import DeleteConfirm from '../pages/Jobs/DeleteConfirm';
import { api, getUserAgents } from '../services/api';
import { JobSummary, PaginationInfo } from '../types/jobs';
import { AgentWithTask } from '../types/agent';
import { calculateJobProgress, formatKeyspace } from '../utils/jobProgress';
import { formatHashRate } from '../utils/formatters';
import { useNavigate } from 'react-router-dom';
import HashlistOverview from '../components/dashboard/HashlistOverview';
// import JobStatusMonitor from '../components/JobStatusMonitor'; // Removed to improve page load performance

/**
 * Dashboard component for displaying system overview and metrics
 * 
 * @component
 * @example
 * return (
 *   <Dashboard />
 * )
 */
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

const Dashboard: React.FC = () => {
  const navigate = useNavigate();
  
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
  const [pagination, setPagination] = useState<PaginationInfo | undefined>(undefined);
  const [statusCounts, setStatusCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [agents, setAgents] = useState<AgentWithTask[]>([]);
  const [agentsLoading, setAgentsLoading] = useState(true);
  const [agentsError, setAgentsError] = useState<Error | null>(null);

  // Agent pagination state
  const [agentPage, setAgentPage] = useState(1);
  const agentsPerPage = 5;
  
  // UI state
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [lastUpdateTime, setLastUpdateTime] = useState(new Date());
  const [isPolling, setIsPolling] = useState(true);
  
  // Refs for cleanup
  const pollingTimer = useRef<NodeJS.Timeout | null>(null);
  const abortController = useRef<AbortController | null>(null);

  // Fetch agents data
  const fetchAgents = useCallback(async () => {
    try {
      setAgentsLoading(true);
      const data = await getUserAgents();
      setAgents(data);
      setAgentsError(null);
    } catch (err: any) {
      console.error('Failed to fetch agents:', err);
      setAgentsError(err);
    } finally {
      setAgentsLoading(false);
    }
  }, []);

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
        `/api/user/jobs?${queryString}`,  // Changed to user-specific endpoint
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

  // Fetch agents on component mount and with polling
  useEffect(() => {
    fetchAgents();
    
    // Poll agents data along with jobs
    const interval = setInterval(() => {
      if (isPolling) {
        fetchAgents();
      }
    }, 5000);
    
    return () => clearInterval(interval);
  }, [fetchAgents, isPolling]);

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

  // Handle page change
  const handlePageChange = (newPage: number) => {
    setPage(newPage);
  };

  // Handle page size change
  const handlePageSizeChange = (newPageSize: number) => {
    setPageSize(newPageSize);
    setPage(1); // Reset to first page when changing page size
  };

  // Handle filter changes
  const handleStatusChange = (event: any) => {
    setFilters(prev => ({ ...prev, status: event.target.value || null }));
    setPage(1);
  };

  const handlePriorityChange = (event: any) => {
    const value = event.target.value;
    setFilters(prev => ({ ...prev, priority: value === '' ? null : parseInt(value) }));
    setPage(1);
  };

  const handleSearchChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setFilters(prev => ({ ...prev, search: event.target.value }));
    setPage(1);
  };

  // Handle delete all finished jobs
  const handleDeleteFinished = async () => {
    setIsDeleting(true);
    try {
      await api.delete('/api/jobs/finished');
      await fetchJobs(true);
      setDeleteDialogOpen(false);
    } catch (err) {
      console.error('Failed to delete finished jobs:', err);
      alert('Failed to delete finished jobs');
    } finally {
      setIsDeleting(false);
    }
  };

  // Handle job actions
  const handleJobDelete = async (jobId: string) => {
    try {
      await api.delete(`/api/jobs/${jobId}`);
      await fetchJobs(false);
    } catch (err) {
      console.error('Failed to delete job:', err);
      alert('Failed to delete job');
    }
  };

  const handleJobRetry = async (jobId: string) => {
    try {
      await api.post(`/api/jobs/${jobId}/retry`);
      await fetchJobs(false);
    } catch (err) {
      console.error('Failed to retry job:', err);
      alert('Failed to retry job');
    }
  };

  const handleJobUpdate = async (jobId: string, updates: { priority?: number; max_agents?: number }) => {
    try {
      await api.patch(`/api/jobs/${jobId}`, updates);
      await fetchJobs(false);
    } catch (err) {
      console.error('Failed to update job:', err);
      alert('Failed to update job');
    }
  };

  // Handle polling toggle
  const handlePollingToggle = (event: React.MouseEvent<HTMLElement>, newValue: boolean | null) => {
    if (newValue !== null) {
      setIsPolling(newValue);
    }
  };

  // Calculate status statistics
  const totalJobs = Object.values(statusCounts).reduce((sum, count) => sum + count, 0);

  // Agent pagination
  const paginatedAgents = useMemo(() => {
    const startIndex = (agentPage - 1) * agentsPerPage;
    return agents.slice(startIndex, startIndex + agentsPerPage);
  }, [agents, agentPage, agentsPerPage]);

  const totalAgentPages = Math.ceil(agents.length / agentsPerPage);

  // Memoize grid items to prevent unnecessary re-renders
  const gridItems = useMemo(() => (
    <>
      <Grid item xs={12} md={8}>
        <HashlistOverview />
      </Grid>

      <Grid item xs={12} md={4}>
        <Paper sx={{ p: 2, display: 'flex', flexDirection: 'column' }}>
          <Typography variant="h6" gutterBottom>
            Agent Status
          </Typography>
          {agentsLoading && agents.length === 0 ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
              <CircularProgress size={24} />
            </Box>
          ) : agentsError ? (
            <Alert severity="error" sx={{ mb: 2 }}>
              Failed to load agents: {agentsError.message}
            </Alert>
          ) : agents.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No agents found
            </Typography>
          ) : (
            <Stack spacing={2}>
              {paginatedAgents.map(agent => (
                <Box key={agent.id} sx={{
                  p: 1.5,
                  border: '1px solid',
                  borderColor: 'divider',
                  borderRadius: 1,
                  bgcolor: 'background.paper'
                }}>
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1 }}>
                    <Typography
                      variant="subtitle2"
                      sx={{
                        cursor: 'pointer',
                        '&:hover': { textDecoration: 'underline' }
                      }}
                      onClick={() => navigate(`/agents/${agent.id}`)}
                    >
                      {agent.name}
                    </Typography>
                    <Chip
                      label={agent.status}
                      size="small"
                      color={agent.status === 'active' ? 'success' : 'default'}
                    />
                  </Box>

                  {agent.currentTask ? (
                    <>
                      <Typography variant="caption" color="text.secondary" display="block">
                        Hash Rate: {agent.currentTask.benchmark_speed ?
                          formatHashRate(agent.currentTask.benchmark_speed, 2) :
                          'N/A'
                        }
                      </Typography>
                      {agent.jobExecution && (
                        <Typography
                          variant="caption"
                          color="primary"
                          sx={{
                            cursor: 'pointer',
                            display: 'block',
                            '&:hover': { textDecoration: 'underline' }
                          }}
                          onClick={() => navigate(`/jobs/${agent.jobExecution!.id}`)}
                        >
                          Job: {agent.jobExecution.name || 'Unnamed Job'}
                        </Typography>
                      )}
                    </>
                  ) : (
                    <Typography variant="caption" color="text.secondary" display="block">
                      No active task
                    </Typography>
                  )}
                </Box>
              ))}
              {totalAgentPages > 1 && (
                <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 2, mt: 2 }}>
                  <Button
                    size="small"
                    disabled={agentPage === 1}
                    onClick={() => setAgentPage(p => p - 1)}
                  >
                    Previous
                  </Button>
                  <Typography variant="caption">
                    Page {agentPage} of {totalAgentPages}
                  </Typography>
                  <Button
                    size="small"
                    disabled={agentPage >= totalAgentPages}
                    onClick={() => setAgentPage(p => p + 1)}
                  >
                    Next
                  </Button>
                </Box>
              )}
            </Stack>
          )}
        </Paper>
      </Grid>

    </>
  ), [jobs, paginatedAgents, agentsLoading, agentsError, navigate, agentPage, totalAgentPages]);

  return (
    <Box sx={{ p: 3 }}>
      <Grid container spacing={3}>
          <Grid item xs={12}>
            <Typography variant="h4" component="h1" gutterBottom>
              Dashboard
            </Typography>
          </Grid>
          {gridItems}
          
          <Grid item xs={12}>
            <Divider sx={{ my: 2 }} />
            <Typography variant="h5" component="h2" gutterBottom>
              Your Jobs
            </Typography>
            <Paper sx={{ p: 3 }}>
              {/* Status badges */}
              <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
                <Badge badgeContent={totalJobs} color="primary">
                  <Chip label="Total" size="small" />
                </Badge>
                {Object.entries(statusCounts).map(([status, count]) => (
                  <Badge key={status} badgeContent={count} color="default">
                    <Chip 
                      label={status} 
                      size="small"
                      color={
                        status === 'running' ? 'primary' :
                        status === 'pending' ? 'warning' :
                        status === 'completed' ? 'success' :
                        status === 'failed' ? 'error' :
                        'default'
                      }
                    />
                  </Badge>
                ))}
                
                <Box sx={{ flexGrow: 1 }} />
                
                <ToggleButtonGroup
                  value={isPolling}
                  exclusive
                  onChange={handlePollingToggle}
                  size="small"
                >
                  <ToggleButton value={true}>
                    <RefreshIcon sx={{ mr: 0.5 }} />
                    Auto-refresh ON
                  </ToggleButton>
                  <ToggleButton value={false}>
                    Auto-refresh OFF
                  </ToggleButton>
                </ToggleButtonGroup>
                
                <Button
                  variant="outlined"
                  size="small"
                  onClick={() => fetchJobs(true)}
                  disabled={loading}
                  startIcon={<RefreshIcon />}
                >
                  Refresh
                </Button>
                
                <Button
                  variant="outlined"
                  size="small"
                  color="error"
                  onClick={() => setDeleteDialogOpen(true)}
                  disabled={statusCounts.completed === 0 && statusCounts.failed === 0}
                  startIcon={<DeleteIcon />}
                >
                  Delete Finished
                </Button>
              </Stack>
              
              {/* Filters */}
              <Stack direction="row" spacing={2} sx={{ mb: 2 }}>
                <FormControl size="small" sx={{ minWidth: 120 }}>
                  <InputLabel>Status</InputLabel>
                  <Select
                    value={filters.status || ''}
                    onChange={handleStatusChange}
                    label="Status"
                  >
                    <MenuItem value="">All</MenuItem>
                    <MenuItem value="pending">Pending</MenuItem>
                    <MenuItem value="running">Running</MenuItem>
                    <MenuItem value="paused">Paused</MenuItem>
                    <MenuItem value="completed">Completed</MenuItem>
                    <MenuItem value="failed">Failed</MenuItem>
                    <MenuItem value="cancelled">Cancelled</MenuItem>
                  </Select>
                </FormControl>
                
                <FormControl size="small" sx={{ minWidth: 120 }}>
                  <InputLabel>Priority</InputLabel>
                  <Select
                    value={filters.priority?.toString() || ''}
                    onChange={handlePriorityChange}
                    label="Priority"
                  >
                    <MenuItem value="">All</MenuItem>
                    {[10, 9, 8, 7, 6, 5, 4, 3, 2, 1].map(p => (
                      <MenuItem key={p} value={p.toString()}>{p}</MenuItem>
                    ))}
                  </Select>
                </FormControl>
                
                <TextField
                  size="small"
                  placeholder="Search jobs..."
                  value={filters.search}
                  onChange={handleSearchChange}
                  InputProps={{
                    startAdornment: <SearchIcon sx={{ mr: 1, color: 'text.secondary' }} />,
                  }}
                  sx={{ flexGrow: 1, maxWidth: 400 }}
                />
                
                {(filters.status || filters.priority !== null || filters.search) && (
                  <Button
                    size="small"
                    onClick={() => {
                      setFilters({ status: null, priority: null, search: '' });
                      setPage(1);
                    }}
                    startIcon={<FilterListIcon />}
                  >
                    Clear Filters
                  </Button>
                )}
              </Stack>
              
              {/* Last update time */}
              <Typography variant="caption" color="text.secondary" sx={{ mb: 2, display: 'block' }}>
                Last updated: {lastUpdateTime.toLocaleTimeString()}
              </Typography>
              
              {/* Content */}
              {loading && jobs.length === 0 ? (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
                  <CircularProgress />
                </Box>
              ) : error ? (
                <Alert severity="error" sx={{ mb: 2 }}>
                  Failed to load jobs: {error.message}
                </Alert>
              ) : jobs.length === 0 ? (
                <Alert severity="info">
                  No jobs found. Create a new job from the Hashlists page.
                </Alert>
              ) : (
                <JobsTable
                  jobs={jobs}
                  pagination={pagination}
                  currentPage={page}
                  pageSize={pageSize}
                  onPageChange={handlePageChange}
                  onPageSizeChange={handlePageSizeChange}
                  onJobUpdated={() => fetchJobs(false)}
                />
              )}
            </Paper>
          </Grid>
        </Grid>
      
      {/* Delete confirmation dialog */}
      <DeleteConfirm
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        onConfirm={handleDeleteFinished}
        isLoading={isDeleting}
        title="Delete All Finished Jobs"
        message="This will permanently delete all completed and failed jobs. This action cannot be undone."
      />
    </Box>
  );
};

export default Dashboard; 