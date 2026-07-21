import React, { useEffect, useState, useCallback } from 'react';
import {
  Paper,
  Box,
  Typography,
  Chip,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import LoopIcon from '@mui/icons-material/Loop';
import { getLoopbackSessions } from '../../services/api';
import { LoopbackSession, LoopbackSessionStatus } from '../../types/adminJobs';

const STATUS_COLOR: Record<LoopbackSessionStatus, 'default' | 'info' | 'primary' | 'success' | 'error'> = {
  waiting: 'info',
  active: 'primary',
  completed: 'success',
  failed: 'error',
  cancelled: 'default',
};

const STATUS_LABEL: Record<LoopbackSessionStatus, string> = {
  waiting: 'Waiting for round to finish',
  active: 'Looping',
  completed: 'Done',
  failed: 'Failed',
  cancelled: 'Cancelled',
};

// A loopback session is "in flight" while waiting or active.
const isInFlight = (s: LoopbackSession) => s.status === 'waiting' || s.status === 'active';

/**
 * LoopbackSessionsPanel shows loopback sessions (GH #64) — each one links a workflow /
 * preset / custom run to the delta re-runs it spawns. It polls alongside the jobs table
 * and renders nothing when there are no sessions to show.
 */
const LoopbackSessionsPanel: React.FC<{ pollIntervalMs?: number }> = ({ pollIntervalMs = 5000 }) => {
  const [sessions, setSessions] = useState<LoopbackSession[]>([]);
  const [loaded, setLoaded] = useState(false);

  const fetchSessions = useCallback(async () => {
    try {
      const data = await getLoopbackSessions();
      setSessions(data);
    } catch (e) {
      // Non-fatal: the panel simply stays hidden/stale.
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    fetchSessions();
    const id = setInterval(fetchSessions, pollIntervalMs);
    return () => clearInterval(id);
  }, [fetchSessions, pollIntervalMs]);

  // Only surface sessions that are in flight or ended recently-ish; hide the panel
  // entirely when there's nothing to show to avoid clutter.
  const visible = sessions
    .slice()
    .sort((a, b) => {
      // In-flight first, then most recent.
      if (isInFlight(a) !== isInFlight(b)) return isInFlight(a) ? -1 : 1;
      return b.created_at.localeCompare(a.created_at);
    });

  if (!loaded || visible.length === 0) {
    return null;
  }

  const inFlightCount = visible.filter(isInFlight).length;

  return (
    <Paper sx={{ p: 2, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <LoopIcon color="primary" />
        <Typography variant="h6">Loopback</Typography>
        <Chip size="small" label={`${inFlightCount} active`} color={inFlightCount > 0 ? 'primary' : 'default'} />
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Each loopback re-runs an attack's mutation against only the newly-cracked passwords,
        repeating until no new cracks are found.
      </Typography>

      {visible.map((session) => (
        <Accordion key={session.id} disableGutters defaultExpanded={isInFlight(session)}>
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', width: '100%' }}>
              <Typography sx={{ fontWeight: 500 }}>{session.name}</Typography>
              <Chip size="small" variant="outlined" label={session.source_type} />
              <Tooltip title={session.error_message || STATUS_LABEL[session.status]}>
                <Chip size="small" color={STATUS_COLOR[session.status]} label={session.status} />
              </Tooltip>
              <Chip size="small" variant="outlined" label={`Round ${session.current_round} / ${session.max_rounds}`} />
              <Box sx={{ flexGrow: 1 }} />
              <Typography variant="caption" color="text.secondary">
                {session.jobs?.length ?? 0} job(s)
              </Typography>
            </Box>
          </AccordionSummary>
          <AccordionDetails>
            {session.jobs && session.jobs.length > 0 ? (
              <Box sx={{ overflowX: 'auto' }}>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Round</TableCell>
                      <TableCell>Job</TableCell>
                      <TableCell>Role</TableCell>
                      <TableCell>Loops back</TableCell>
                      <TableCell>Status</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {session.jobs
                      .slice()
                      .sort((a, b) => a.round - b.round)
                      .map((job) => (
                        <TableRow key={job.id}>
                          <TableCell>{job.round === 0 ? 'Original' : job.round}</TableCell>
                          <TableCell>{job.job_name || job.job_execution_id}</TableCell>
                          <TableCell>{job.role}</TableCell>
                          <TableCell>{job.is_mutatable ? 'Yes' : 'Feeds delta'}</TableCell>
                          <TableCell>{job.job_status || '—'}</TableCell>
                        </TableRow>
                      ))}
                  </TableBody>
                </Table>
              </Box>
            ) : (
              <Typography variant="body2" color="text.secondary">
                No jobs recorded yet.
              </Typography>
            )}
          </AccordionDetails>
        </Accordion>
      ))}
    </Paper>
  );
};

export default LoopbackSessionsPanel;
