import React, { useState } from 'react';
import {
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  TableSortLabel,
  Typography,
  Box,
  Chip,
  Collapse,
  IconButton,
  Skeleton,
  LinearProgress,
} from '@mui/material';
import {
  KeyboardArrowDown as ExpandIcon,
  KeyboardArrowUp as CollapseIcon,
} from '@mui/icons-material';
import { JobAnalyticsEntry, TaskSegment, TimelinePoint } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';

interface JobExecutionTableProps {
  jobs: JobAnalyticsEntry[] | undefined;
  total: number;
  page: number;
  pageSize: number;
  sortBy: string;
  sortOrder: string;
  loading: boolean;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onSortChange: (sortBy: string, sortOrder: string) => void;
}

const formatSpeed = (speed: number): string => {
  if (speed >= 1e12) return `${(speed / 1e12).toFixed(1)} TH/s`;
  if (speed >= 1e9) return `${(speed / 1e9).toFixed(1)} GH/s`;
  if (speed >= 1e6) return `${(speed / 1e6).toFixed(1)} MH/s`;
  if (speed >= 1e3) return `${(speed / 1e3).toFixed(1)} KH/s`;
  return `${Math.round(speed)} H/s`;
};

const formatDuration = (seconds: number | null): string => {
  if (seconds === null) return '-';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
};

const formatKeyspace = (ks: number): string => {
  if (ks >= 1e15) return `${(ks / 1e15).toFixed(1)}P`;
  if (ks >= 1e12) return `${(ks / 1e12).toFixed(1)}T`;
  if (ks >= 1e9) return `${(ks / 1e9).toFixed(1)}G`;
  if (ks >= 1e6) return `${(ks / 1e6).toFixed(1)}M`;
  if (ks >= 1e3) return `${(ks / 1e3).toFixed(1)}K`;
  return String(ks);
};

const attackModeLabels: Record<number, string> = {
  0: 'Straight',
  1: 'Combination',
  3: 'Brute-force',
  6: 'Hybrid WL+Mask',
  7: 'Hybrid Mask+WL',
  9: 'Association',
};

const statusColor = (status: string): 'success' | 'error' | 'warning' | 'info' | 'default' => {
  switch (status) {
    case 'completed': return 'success';
    case 'failed': return 'error';
    case 'running': return 'info';
    case 'cancelled': return 'warning';
    case 'paused': return 'warning';
    default: return 'default';
  }
};

interface SortableColumns {
  id: string;
  label: string;
  sortable: boolean;
  align?: 'left' | 'right' | 'center';
}

const columns: SortableColumns[] = [
  { id: 'expand', label: '', sortable: false },
  { id: 'name', label: 'Name', sortable: true },
  { id: 'attack_mode', label: 'Attack', sortable: true },
  { id: 'hash_type_name', label: 'Hash Type', sortable: true },
  { id: 'status', label: 'Status', sortable: true },
  { id: 'duration_seconds', label: 'Duration', sortable: true, align: 'right' },
  { id: 'avg_speed', label: 'Avg Speed', sortable: true, align: 'right' },
  { id: 'total_cracks', label: 'Cracks', sortable: true, align: 'right' },
  { id: 'effective_keyspace', label: 'Keyspace', sortable: true, align: 'right' },
  { id: 'unique_agents', label: 'Agents', sortable: true, align: 'right' },
  { id: 'overall_progress_percent', label: 'Progress', sortable: true, align: 'right' },
];

interface ExpandedRowProps {
  jobId: string;
}

const ExpandedRow: React.FC<ExpandedRowProps> = ({ jobId }) => {
  const [tasks, setTasks] = React.useState<TaskSegment[]>([]);
  const [metrics, setMetrics] = React.useState<TimelinePoint[]>([]);
  const [loading, setLoading] = React.useState(true);

  React.useEffect(() => {
    let cancelled = false;
    jobAnalyticsService.getJobTimeline(jobId).then(data => {
      if (!cancelled) {
        setTasks(data.tasks || []);
        setMetrics(data.metrics || []);
        setLoading(false);
      }
    }).catch(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; };
  }, [jobId]);

  if (loading) {
    return (
      <Box sx={{ p: 2 }}>
        <Skeleton variant="rectangular" height={100} />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 2 }}>
      <Typography variant="subtitle2" gutterBottom>
        Task Breakdown ({tasks.length} tasks, {metrics.length} metric points)
      </Typography>
      {tasks.length === 0 ? (
        <Typography variant="body2" color="text.secondary">No task data available</Typography>
      ) : (
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Agent</TableCell>
              <TableCell>Status</TableCell>
              <TableCell align="right">Avg Speed</TableCell>
              <TableCell align="right">Benchmark</TableCell>
              <TableCell align="right">Cracks</TableCell>
              <TableCell>Started</TableCell>
              <TableCell>Completed</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {tasks.map(task => (
              <TableRow key={task.task_id}>
                <TableCell>{task.agent_name} (#{task.agent_id})</TableCell>
                <TableCell>
                  <Chip label={task.status} size="small" color={statusColor(task.status)} variant="outlined" />
                </TableCell>
                <TableCell align="right">{formatSpeed(task.average_speed)}</TableCell>
                <TableCell align="right">{formatSpeed(task.benchmark_speed)}</TableCell>
                <TableCell align="right">{task.crack_count}</TableCell>
                <TableCell>{task.started_at ? new Date(task.started_at).toLocaleString() : '-'}</TableCell>
                <TableCell>{task.completed_at ? new Date(task.completed_at).toLocaleString() : '-'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Box>
  );
};

const JobExecutionTable: React.FC<JobExecutionTableProps> = ({
  jobs,
  total,
  page,
  pageSize,
  sortBy,
  sortOrder,
  loading,
  onPageChange,
  onPageSizeChange,
  onSortChange,
}) => {
  const [expandedRow, setExpandedRow] = useState<string | null>(null);

  const handleSort = (columnId: string) => {
    const isAsc = sortBy === columnId && sortOrder === 'asc';
    onSortChange(columnId, isAsc ? 'desc' : 'asc');
  };

  return (
    <Paper sx={{ mb: 3 }}>
      <Typography variant="h6" sx={{ p: 2, pb: 0 }}>Job Execution Details</Typography>
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              {columns.map(col => (
                <TableCell key={col.id} align={col.align || 'left'}>
                  {col.sortable ? (
                    <TableSortLabel
                      active={sortBy === col.id}
                      direction={sortBy === col.id ? (sortOrder as 'asc' | 'desc') : 'asc'}
                      onClick={() => handleSort(col.id)}
                    >
                      {col.label}
                    </TableSortLabel>
                  ) : col.label}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              Array.from({ length: pageSize }).map((_, i) => (
                <TableRow key={i}>
                  {columns.map(col => (
                    <TableCell key={col.id}><Skeleton variant="text" /></TableCell>
                  ))}
                </TableRow>
              ))
            ) : !jobs || jobs.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length} align="center">
                  <Typography color="text.secondary" sx={{ py: 4 }}>
                    No jobs found for the selected filters
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              jobs.map(job => (
                <React.Fragment key={job.id}>
                  <TableRow
                    hover
                    sx={{ cursor: 'pointer', '& > *': { borderBottom: expandedRow === job.id ? 'unset' : undefined } }}
                    onClick={() => setExpandedRow(expandedRow === job.id ? null : job.id)}
                  >
                    <TableCell padding="checkbox">
                      <IconButton size="small">
                        {expandedRow === job.id ? <CollapseIcon /> : <ExpandIcon />}
                      </IconButton>
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2" noWrap sx={{ maxWidth: 200 }}>
                        {job.name}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {job.hashlist_name}
                      </Typography>
                    </TableCell>
                    <TableCell>{attackModeLabels[job.attack_mode] || `Mode ${job.attack_mode}`}</TableCell>
                    <TableCell>
                      <Typography variant="body2" noWrap sx={{ maxWidth: 150 }}>
                        {job.hash_type_name}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Chip label={job.status} size="small" color={statusColor(job.status)} />
                    </TableCell>
                    <TableCell align="right">{formatDuration(job.duration_seconds)}</TableCell>
                    <TableCell align="right">{formatSpeed(job.avg_speed)}</TableCell>
                    <TableCell align="right">{job.total_cracks}</TableCell>
                    <TableCell align="right">{formatKeyspace(job.effective_keyspace)}</TableCell>
                    <TableCell align="right">{job.unique_agents}</TableCell>
                    <TableCell align="right">
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, justifyContent: 'flex-end' }}>
                        <LinearProgress
                          variant="determinate"
                          value={Math.min(job.overall_progress_percent, 100)}
                          sx={{ width: 60, height: 6, borderRadius: 3 }}
                        />
                        <Typography variant="caption">
                          {job.overall_progress_percent.toFixed(0)}%
                        </Typography>
                      </Box>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell style={{ paddingBottom: 0, paddingTop: 0 }} colSpan={columns.length}>
                      <Collapse in={expandedRow === job.id} timeout="auto" unmountOnExit>
                        <ExpandedRow jobId={job.id} />
                      </Collapse>
                    </TableCell>
                  </TableRow>
                </React.Fragment>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>
      <TablePagination
        component="div"
        count={total}
        page={page - 1}
        onPageChange={(_, newPage) => onPageChange(newPage + 1)}
        rowsPerPage={pageSize}
        onRowsPerPageChange={(e) => onPageSizeChange(parseInt(e.target.value, 10))}
        rowsPerPageOptions={[10, 25, 50]}
      />
    </Paper>
  );
};

export default JobExecutionTable;
