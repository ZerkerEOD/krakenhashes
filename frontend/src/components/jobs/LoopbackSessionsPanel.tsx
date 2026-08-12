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

// Every status is kept here (the Record<LoopbackSessionStatus, …> type requires all five)
// even though the panel only ever renders the in-flight ones — the terminal entries stay
// as the fallback if a session's status changes between fetch and render.
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
 * LoopbackSessionsPanel is a **live view** of the loopback sessions currently in flight
 * (GH #64) — each one links a workflow / preset / custom run to the delta re-runs it
 * spawns, so you can see what the next round is waiting on.
 *
 * It is not a history: a session leaves the panel the moment it finishes (done, failed or
 * cancelled), and the panel hides itself entirely once nothing is in flight (GH #79). The
 * re-run jobs themselves stay in the normal Jobs list, named `<origin> (loopback R<n>)`.
 *
 * `scope='visible'` shows every session the user is allowed to see (bounded by the same
 * team scoping as the Jobs list); the default shows only the user's own sessions.
 */
const LoopbackSessionsPanel: React.FC<{ pollIntervalMs?: number; scope?: 'mine' | 'visible' }> = ({
  pollIntervalMs = 5000,
  scope,
}) => {
  const [sessions, setSessions] = useState<LoopbackSession[]>([]);
  const [loaded, setLoaded] = useState(false);

  const fetchSessions = useCallback(async () => {
    try {
      const data = await getLoopbackSessions(scope);
      setSessions(data);
    } catch (e) {
      // Non-fatal: the panel simply stays hidden/stale.
    } finally {
      setLoaded(true);
    }
  }, [scope]);

  useEffect(() => {
    fetchSessions();
    const id = setInterval(fetchSessions, pollIntervalMs);
    return () => clearInterval(id);
  }, [fetchSessions, pollIntervalMs]);

  // The backend already returns in-flight sessions only; the filter is defensive so a
  // stale frontend bundle served against a newer/older backend still never shows a
  // finished session. Most recent first.
  const visible = sessions
    .filter(isInFlight)
    .sort((a, b) => b.created_at.localeCompare(a.created_at));

  if (!loaded || visible.length === 0) {
    return null;
  }

  return (
    <Paper sx={{ p: 2, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <LoopIcon color="primary" />
        <Typography variant="h6">Loopback</Typography>
        <Chip size="small" label={`${visible.length} running`} color="primary" />
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Each loopback re-runs an attack's mutation against only the newly-cracked passwords,
        repeating until no new cracks are found.
      </Typography>

      {visible.map((session) => (
        // Everything shown here is in flight, so expand by default only while the list is
        // short — a busy Dashboard shouldn't be a wall of expanded job tables.
        <Accordion key={session.id} disableGutters defaultExpanded={visible.length <= 2}>
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
