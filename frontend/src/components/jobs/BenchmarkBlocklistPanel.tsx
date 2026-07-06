import React, { useState, useEffect, useCallback } from 'react';
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
  Button,
  Chip,
  CircularProgress,
  Alert,
  Tooltip,
} from '@mui/material';
import { Replay as ReplayIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { api } from '../../services/api';

// Kept in sync with blocklistEntryDTO in backend/internal/handlers/jobs/user_jobs.go.
interface BlocklistEntry {
  id: string;
  agent_id: number;
  agent_name?: string;
  job_execution_id?: string;
  attack_mode: number;
  hash_type: number;
  reason: string;
  expires_at: string;
  created_at: string;
  failure_count?: number;
  last_error?: string;
  cleared_at?: string;
}

interface Props {
  jobId: string;
}

const formatRelativeFuture = (iso: string): string => {
  const diffMs = new Date(iso).getTime() - Date.now();
  if (diffMs <= 0) return 'expired';
  const mins = Math.round(diffMs / 60000);
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  const rem = mins % 60;
  if (hrs < 48) return rem === 0 ? `${hrs}h` : `${hrs}h ${rem}m`;
  return `${Math.round(hrs / 24)}d`;
};

const BenchmarkBlocklistPanel: React.FC<Props> = ({ jobId }) => {
  const { enqueueSnackbar } = useSnackbar();
  const [entries, setEntries] = useState<BlocklistEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [clearingId, setClearingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchEntries = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await api.get<BlocklistEntry[]>(`/api/jobs/${jobId}/benchmark-blocklist`);
      setEntries(resp.data || []);
    } catch (e: any) {
      setError(e?.response?.data || e?.message || 'Failed to load blocklist');
    } finally {
      setLoading(false);
    }
  }, [jobId]);

  useEffect(() => {
    fetchEntries();
    // Refresh every 60s so expired entries drop off without a manual reload.
    const t = setInterval(fetchEntries, 60_000);
    return () => clearInterval(t);
  }, [fetchEntries]);

  const handleRetry = async (entryId: string) => {
    setClearingId(entryId);
    try {
      await api.post(`/api/jobs/${jobId}/benchmark-blocklist/${entryId}/clear`);
      enqueueSnackbar('Blocklist entry cleared — scheduler will retry on next cycle', { variant: 'success' });
      await fetchEntries();
    } catch (e: any) {
      const msg = e?.response?.data || e?.message || 'Failed to clear entry';
      enqueueSnackbar(`Failed to clear: ${msg}`, { variant: 'error' });
    } finally {
      setClearingId(null);
    }
  };

  // Hide the panel entirely when there's nothing to show — no value in a
  // "no entries" message on every job detail page.
  if (!loading && entries.length === 0 && !error) {
    return null;
  }

  return (
    <Paper sx={{ mt: 3 }}>
      <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Box>
          <Typography variant="h6">Benchmark Cooldowns</Typography>
          <Typography variant="body2" color="text.secondary">
            Agents currently blocklisted from benchmarking this job after repeated failures. Clear an entry to retry on the next scheduling cycle.
          </Typography>
        </Box>
        {loading && <CircularProgress size={20} />}
      </Box>
      {error && (
        <Box sx={{ p: 2 }}>
          <Alert severity="error">{error}</Alert>
        </Box>
      )}
      <TableContainer>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Agent</TableCell>
              <TableCell>Scope</TableCell>
              <TableCell>Hash / Mode</TableCell>
              <TableCell>Failures</TableCell>
              <TableCell>Reason</TableCell>
              <TableCell>Expires</TableCell>
              <TableCell align="right">Action</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {entries.map((e) => (
              <TableRow key={e.id}>
                <TableCell>
                  {e.agent_name ? `${e.agent_name} (#${e.agent_id})` : `#${e.agent_id}`}
                </TableCell>
                <TableCell>
                  {e.job_execution_id ? (
                    <Chip size="small" label="this job" color="warning" variant="outlined" />
                  ) : (
                    <Chip size="small" label="global" color="error" variant="outlined" />
                  )}
                </TableCell>
                <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.8rem' }}>
                  {e.hash_type} / {e.attack_mode}
                </TableCell>
                <TableCell>
                  {e.failure_count ?? '—'}
                </TableCell>
                <TableCell>
                  <Tooltip title={e.last_error || e.reason}>
                    <Typography
                      variant="body2"
                      sx={{ maxWidth: 360, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    >
                      {e.reason}
                    </Typography>
                  </Tooltip>
                </TableCell>
                <TableCell>{formatRelativeFuture(e.expires_at)}</TableCell>
                <TableCell align="right">
                  <Button
                    size="small"
                    variant="outlined"
                    startIcon={clearingId === e.id ? <CircularProgress size={14} /> : <ReplayIcon />}
                    disabled={clearingId !== null}
                    onClick={() => handleRetry(e.id)}
                  >
                    Retry now
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
};

export default BenchmarkBlocklistPanel;
