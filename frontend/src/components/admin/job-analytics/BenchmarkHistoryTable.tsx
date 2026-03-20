import React, { useState, useEffect } from 'react';
import {
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Typography,
  Box,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Skeleton,
  SelectChangeEvent,
} from '@mui/material';
import { BenchmarkHistoryEntry, JobAnalyticsFilterOptions } from '../../../types/jobAnalytics';
import { jobAnalyticsService } from '../../../services/jobAnalytics';

interface BenchmarkHistoryTableProps {
  filterOptions: JobAnalyticsFilterOptions | undefined;
}

const formatSpeed = (speed: number): string => {
  if (speed >= 1e12) return `${(speed / 1e12).toFixed(1)} TH/s`;
  if (speed >= 1e9) return `${(speed / 1e9).toFixed(1)} GH/s`;
  if (speed >= 1e6) return `${(speed / 1e6).toFixed(1)} MH/s`;
  if (speed >= 1e3) return `${(speed / 1e3).toFixed(1)} KH/s`;
  return `${Math.round(speed)} H/s`;
};

const attackModeLabels: Record<number, string> = {
  0: 'Straight',
  1: 'Combination',
  3: 'Brute-force',
  6: 'Hybrid WL+Mask',
  7: 'Hybrid Mask+WL',
  9: 'Association',
};

const BenchmarkHistoryTable: React.FC<BenchmarkHistoryTableProps> = ({ filterOptions }) => {
  const [agentId, setAgentId] = useState<number | undefined>();
  const [hashType, setHashType] = useState<number | undefined>();
  const [attackMode, setAttackMode] = useState<number | undefined>();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [data, setData] = useState<BenchmarkHistoryEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    jobAnalyticsService
      .getBenchmarkHistory({
        agent_id: agentId,
        hash_type: hashType,
        attack_mode: attackMode,
        page,
        page_size: pageSize,
      })
      .then(res => {
        setData(res.items || []);
        setTotal(res.pagination?.total || 0);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, [agentId, hashType, attackMode, page, pageSize]);

  const handleSelectChange = (setter: (val: number | undefined) => void) =>
    (e: SelectChangeEvent<string>) => {
      const val = e.target.value;
      setter(val === '' ? undefined : Number(val));
      setPage(1);
    };

  return (
    <Paper sx={{ mb: 3 }}>
      <Box sx={{ p: 2, display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'center' }}>
        <Typography variant="h6" sx={{ flexGrow: 1 }}>Benchmark History</Typography>
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>Agent</InputLabel>
          <Select
            value={agentId !== undefined ? String(agentId) : ''}
            onChange={handleSelectChange(setAgentId)}
            label="Agent"
          >
            <MenuItem value="">All</MenuItem>
            {filterOptions?.agents?.map(a => (
              <MenuItem key={a.id} value={String(a.id)}>{a.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>Hash Type</InputLabel>
          <Select
            value={hashType !== undefined ? String(hashType) : ''}
            onChange={handleSelectChange(setHashType)}
            label="Hash Type"
          >
            <MenuItem value="">All</MenuItem>
            {filterOptions?.hash_types?.map(ht => (
              <MenuItem key={ht.id} value={String(ht.id)}>{ht.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 150 }}>
          <InputLabel>Attack Mode</InputLabel>
          <Select
            value={attackMode !== undefined ? String(attackMode) : ''}
            onChange={handleSelectChange(setAttackMode)}
            label="Attack Mode"
          >
            <MenuItem value="">All</MenuItem>
            {filterOptions?.attack_modes?.map(am => (
              <MenuItem key={am.value} value={String(am.value)}>{am.label}</MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Date</TableCell>
              <TableCell>Agent</TableCell>
              <TableCell>Hash Type</TableCell>
              <TableCell>Attack Mode</TableCell>
              <TableCell align="right">Speed</TableCell>
              <TableCell>Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              Array.from({ length: pageSize }).map((_, i) => (
                <TableRow key={i}>
                  {[0, 1, 2, 3, 4, 5].map(j => (
                    <TableCell key={j}><Skeleton variant="text" /></TableCell>
                  ))}
                </TableRow>
              ))
            ) : data.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} align="center">
                  <Typography color="text.secondary" sx={{ py: 4 }}>
                    No benchmark history found
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              data.map(entry => (
                <TableRow key={entry.id} hover>
                  <TableCell>{new Date(entry.recorded_at).toLocaleString()}</TableCell>
                  <TableCell>Agent #{entry.agent_id}</TableCell>
                  <TableCell>{entry.hash_type}</TableCell>
                  <TableCell>{attackModeLabels[entry.attack_mode] || `Mode ${entry.attack_mode}`}</TableCell>
                  <TableCell align="right">{formatSpeed(entry.speed)}</TableCell>
                  <TableCell>
                    <Chip
                      label={entry.success ? 'Success' : 'Failed'}
                      size="small"
                      color={entry.success ? 'success' : 'error'}
                      variant="outlined"
                    />
                    {entry.error_message && (
                      <Typography variant="caption" color="error" sx={{ ml: 1 }}>
                        {entry.error_message}
                      </Typography>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>
      <TablePagination
        component="div"
        count={total}
        page={page - 1}
        onPageChange={(_, newPage) => setPage(newPage + 1)}
        rowsPerPage={pageSize}
        onRowsPerPageChange={(e) => { setPageSize(parseInt(e.target.value, 10)); setPage(1); }}
        rowsPerPageOptions={[10, 25, 50]}
      />
    </Paper>
  );
};

export default BenchmarkHistoryTable;
