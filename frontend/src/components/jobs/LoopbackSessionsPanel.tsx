import React from 'react';
import { useTranslation } from 'react-i18next';
import { Accordion, AccordionDetails, AccordionSummary, Box, Chip, Typography } from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import LoopIcon from '@mui/icons-material/Loop';
import { getLoopbackSessions } from '../../services/api';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { LoopbackSession, LoopbackSessionJob, LoopbackSessionStatus } from '../../types/adminJobs';
import { EntityLink, SectionCard, SimpleTable, StatusChip } from '../ui';

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
const LoopbackSessionsPanel: React.FC<{ scope?: 'mine' | 'visible' }> = ({ scope }) => {
  const { t } = useTranslation('jobs');
  const STATUS_LABEL: Record<LoopbackSessionStatus, string> = {
    waiting: t('loopback.statusLabel.waiting') as string,
    active: t('loopback.statusLabel.active') as string,
    completed: t('loopback.statusLabel.completed') as string,
    failed: t('loopback.statusLabel.failed') as string,
    cancelled: t('loopback.statusLabel.cancelled') as string,
  };
  const { data: sessions = [], isFetched } = useLiveQuery(
    {
      queryKey: ['loopback-sessions', scope ?? 'mine'],
      // Non-fatal on error: the panel simply stays hidden/stale.
      queryFn: () => getLoopbackSessions(scope),
    },
    { tier: 'fast' }
  );

  // The backend already returns in-flight sessions only; the filter is defensive so a
  // stale frontend bundle served against a newer/older backend still never shows a
  // finished session. Most recent first.
  const visible = sessions.filter(isInFlight).sort((a, b) => b.created_at.localeCompare(a.created_at));

  if (!isFetched || visible.length === 0) {
    return null;
  }

  return (
    <SectionCard
      icon={<LoopIcon color="primary" />}
      title={t('loopback.title') as string}
      subtitle={t('loopback.subtitle') as string}
      actions={<Chip size="small" label={t('loopback.runningCount', { count: visible.length }) as string} color="primary" />}
      sx={{ mb: 3 }}
    >
      {visible.map((session) => (
        // Everything shown here is in flight, so expand by default only while the list is
        // short — a busy Dashboard shouldn't be a wall of expanded job tables.
        <Accordion key={session.id} disableGutters defaultExpanded={visible.length <= 2}>
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', width: '100%' }}>
              <Typography sx={{ fontWeight: 500 }}>
                {session.source_type === 'workflow' && session.source_workflow_id ? (
                  <EntityLink type="workflow" id={session.source_workflow_id} label={session.name} />
                ) : (
                  session.name
                )}
              </Typography>
              <Chip size="small" variant="outlined" label={session.source_type} />
              <StatusChip entity="loopback" status={session.status} tooltip={session.error_message || STATUS_LABEL[session.status]} />
              <Chip size="small" variant="outlined" label={t('loopback.roundLabel', { current: session.current_round, max: session.max_rounds }) as string} />
              <Typography variant="caption" color="text.secondary">
                <EntityLink type="hashlist" id={session.hashlist_id} label={t('loopback.hashlistFallback', { id: session.hashlist_id }) as string} color="inherit" />
              </Typography>
              <Box sx={{ flexGrow: 1 }} />
              <Typography variant="caption" color="text.secondary">
                {t('loopback.jobCount', { count: session.jobs?.length ?? 0 })}
              </Typography>
            </Box>
          </AccordionSummary>
          <AccordionDetails>
            <SimpleTable<LoopbackSessionJob>
              rows={(session.jobs ?? []).slice().sort((a, b) => a.round - b.round)}
              getRowKey={(j) => j.id}
              emptyState={{ title: t('loopback.noJobsYet') as string }}
              columns={[
                { field: 'round', headerName: t('loopback.columns.round') as string, render: (j) => (j.round === 0 ? (t('loopback.columns.original') as string) : j.round) },
                {
                  field: 'job',
                  headerName: t('loopback.columns.job') as string,
                  render: (j) => <EntityLink type="job" id={j.job_execution_id} label={j.job_name || j.job_execution_id} />,
                },
                { field: 'role', headerName: t('loopback.columns.role') as string },
                { field: 'is_mutatable', headerName: t('loopback.columns.loopsBack') as string, render: (j) => (j.is_mutatable ? (t('loopback.loopsBackYes') as string) : (t('loopback.loopsBackFeedsDelta') as string)) },
                {
                  field: 'job_status',
                  headerName: t('common.status') as string,
                  render: (j) => (j.job_status ? <StatusChip entity="job" status={j.job_status} /> : '—'),
                },
              ]}
            />
          </AccordionDetails>
        </Accordion>
      ))}
    </SectionCard>
  );
};

export default LoopbackSessionsPanel;
