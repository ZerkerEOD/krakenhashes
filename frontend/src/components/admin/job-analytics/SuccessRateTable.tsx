import React, { useState, useEffect, useMemo } from 'react';
import {
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TableSortLabel,
  Typography,
  Box,
  Chip,
  Skeleton,
  LinearProgress,
} from '@mui/material';
import { SuccessRateEntry, JobAnalyticsFilterParams } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';

interface SuccessRateTableProps {
  filter: JobAnalyticsFilterParams;
}

const formatDuration = (seconds: number | null): string => {
  if (seconds === null || seconds === 0) return '-';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
};

const formatNumber = (n: number): string => {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return String(n);
};

const successRateColor = (rate: number): 'success' | 'warning' | 'error' => {
  if (rate >= 30) return 'success';
  if (rate >= 10) return 'warning';
  return 'error';
};

interface ColumnDef {
  id: string;
  label: string;
  align?: 'left' | 'right';
  sortable: boolean;
}

const columns: ColumnDef[] = [
  { id: 'display_name', label: 'Configuration', sortable: true },
  { id: 'attack_mode_label', label: 'Attack Mode', sortable: true },
  { id: 'hash_type_name', label: 'Hash Type', sortable: true },
  { id: 'total_runs', label: 'Runs', sortable: true, align: 'right' },
  { id: 'total_hashes', label: 'Hashes', sortable: true, align: 'right' },
  { id: 'total_cracks', label: 'Cracks', sortable: true, align: 'right' },
  { id: 'success_rate_percent', label: 'Success Rate', sortable: true, align: 'right' },
  { id: 'avg_job_duration_seconds', label: 'Avg Duration', sortable: true, align: 'right' },
  { id: 'total_compute_seconds', label: 'Total Compute', sortable: true, align: 'right' },
];

type SortKey = keyof SuccessRateEntry;

const SuccessRateTable: React.FC<SuccessRateTableProps> = ({ filter }) => {
  const [data, setData] = useState<SuccessRateEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [sortBy, setSortBy] = useState<string>('success_rate_percent');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');

  useEffect(() => {
    setLoading(true);
    jobAnalyticsService
      .getSuccessRates(filter)
      .then(res => {
        setData(res.entries || []);
        setLoading(false);
      })
      .catch(() => {
        setData([]);
        setLoading(false);
      });
  }, [filter]);

  const sortedData = useMemo(() => {
    if (!data.length) return data;
    const sorted = [...data];
    sorted.sort((a, b) => {
      const aVal = a[sortBy as SortKey];
      const bVal = b[sortBy as SortKey];
      if (aVal === bVal) return 0;
      if (aVal === null || aVal === undefined) return 1;
      if (bVal === null || bVal === undefined) return -1;
      const cmp = aVal < bVal ? -1 : 1;
      return sortOrder === 'asc' ? cmp : -cmp;
    });
    return sorted;
  }, [data, sortBy, sortOrder]);

  const handleSort = (columnId: string) => {
    if (sortBy === columnId) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
    } else {
      setSortBy(columnId);
      setSortOrder('desc');
    }
  };

  return (
    <Paper sx={{ mb: 3 }}>
      <Typography variant="h6" sx={{ p: 2, pb: 0 }}>
        Success Rate by Job Configuration
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ px: 2, pb: 1 }}>
        Grouped by wordlist, rules, attack mode, and hash type. Preset jobs include matching custom jobs.
      </Typography>
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              {columns.map(col => (
                <TableCell key={col.id} align={col.align || 'left'}>
                  {col.sortable ? (
                    <TableSortLabel
                      active={sortBy === col.id}
                      direction={sortBy === col.id ? sortOrder : 'asc'}
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
              Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  {columns.map(col => (
                    <TableCell key={col.id}><Skeleton variant="text" /></TableCell>
                  ))}
                </TableRow>
              ))
            ) : sortedData.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length} align="center">
                  <Typography color="text.secondary" sx={{ py: 4 }}>
                    No success rate data available for the selected filters
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              sortedData.map((entry, idx) => (
                <TableRow key={idx} hover>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      {entry.is_preset && (
                        <Chip
                          label="Preset"
                          size="small"
                          color="primary"
                          variant="outlined"
                          sx={{ fontSize: '0.7rem', height: 20 }}
                        />
                      )}
                      <Typography variant="body2" noWrap sx={{ maxWidth: 300 }} title={entry.display_name}>
                        {entry.display_name}
                      </Typography>
                    </Box>
                  </TableCell>
                  <TableCell>{entry.attack_mode_label}</TableCell>
                  <TableCell>
                    <Typography variant="body2" noWrap sx={{ maxWidth: 150 }}>
                      {entry.hash_type_name}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">{entry.total_runs}</TableCell>
                  <TableCell align="right">{formatNumber(entry.total_hashes)}</TableCell>
                  <TableCell align="right">{formatNumber(entry.total_cracks)}</TableCell>
                  <TableCell align="right">
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, justifyContent: 'flex-end' }}>
                      <LinearProgress
                        variant="determinate"
                        value={Math.min(entry.success_rate_percent, 100)}
                        color={successRateColor(entry.success_rate_percent)}
                        sx={{ width: 50, height: 6, borderRadius: 3 }}
                      />
                      <Chip
                        label={`${entry.success_rate_percent.toFixed(1)}%`}
                        size="small"
                        color={successRateColor(entry.success_rate_percent)}
                        variant="outlined"
                        sx={{ fontSize: '0.75rem', height: 22, minWidth: 55 }}
                      />
                    </Box>
                  </TableCell>
                  <TableCell align="right">{formatDuration(entry.avg_job_duration_seconds)}</TableCell>
                  <TableCell align="right">{formatDuration(entry.total_compute_seconds)}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
};

export default SuccessRateTable;
