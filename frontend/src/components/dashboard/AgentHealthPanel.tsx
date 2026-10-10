import React from 'react';
import { Box, Chip, LinearProgress, List, ListItem, Tooltip, Typography } from '@mui/material';
import ThermostatIcon from '@mui/icons-material/Thermostat';
import MemoryIcon from '@mui/icons-material/Memory';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import Button from '@mui/material/Button';
import { EmptyState, EntityLink, ErrorState, SectionCard, StatusChip } from '../ui';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { getAgentHealth, DashboardView } from '../../services/dashboard';
import { formatHashRate, formatRelativeTime } from '../../utils/formatters';
import { ROUTES } from '../../constants/routes';

const WARNING_TONE: Record<string, 'error' | 'warning' | 'info'> = {
  error: 'error',
  update_failed: 'error',
  offline: 'warning',
  stale_heartbeat: 'warning',
  gpu_hot: 'warning',
};

/** Agents with problems first, then the rest, with live GPU metrics. */
const AgentHealthPanel: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const q = useLiveQuery(
    { queryKey: qk.dashboard.agentHealth(view, teamId), queryFn: () => getAgentHealth(view, teamId) },
    { tier: 'fast' }
  );
  const agents = q.data ?? [];

  return (
    <SectionCard
      title={t('agentHealth.title', 'Agent health') as string}
      actions={<Button size="small" component={RouterLink} to={ROUTES.agents}>{t('agentHealth.viewAll', 'All agents') as string}</Button>}
      loading={q.isLoading}
      flush
      fill
    >
      {q.error && !q.data ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} compact />
      ) : agents.length === 0 && !q.isLoading ? (
        <EmptyState size="sm" title={t('agents.noAgents') as string} />
      ) : (
        <List dense disablePadding sx={{ flex: 1, minHeight: 0, maxHeight: { xs: 420, lg: 'none' }, overflowY: 'auto' }}>
          {agents.map((a) => (
            <ListItem key={a.id} divider sx={{ display: 'block', py: 1, px: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
                <StatusChip entity="agent" status={a.is_enabled ? a.status : 'disabled'} variant="dot" />
                <EntityLink type="agent" id={a.id} label={a.name || `#${a.id}`} sx={{ fontWeight: 500 }} noWrap />
                <Box sx={{ flexGrow: 1 }} />
                {a.metrics && a.metrics.hash_rate > 0 && (
                  <Typography variant="caption" color="text.secondary">{formatHashRate(a.metrics.hash_rate)}</Typography>
                )}
              </Box>
              {a.current_job_id && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.5 }}>
                  <Typography variant="caption" color="text.secondary" noWrap sx={{ minWidth: 0 }}>
                    <EntityLink type="job" id={a.current_job_id} label={a.current_job_name || (t('agents.unnamedJob') as string)} color="inherit" />
                  </Typography>
                  {a.current_job_progress !== undefined && (
                    <LinearProgress variant="determinate" value={Math.min(100, a.current_job_progress)} sx={{ flexGrow: 1, height: 4, borderRadius: 2 }} />
                  )}
                </Box>
              )}
              <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', mt: 0.5, alignItems: 'center' }}>
                {a.metrics && (
                  <>
                    <Tooltip title={t('agentHealth.utilization', 'GPU utilization') as string}>
                      <Chip size="small" variant="outlined" icon={<MemoryIcon />} label={`${Math.round(a.metrics.gpu_utilization)}%`} />
                    </Tooltip>
                    <Tooltip title={t('agentHealth.temperature', 'Hottest GPU') as string}>
                      <Chip size="small" variant="outlined" icon={<ThermostatIcon />} label={`${Math.round(a.metrics.gpu_temp)}°C`} color={a.warnings.includes('gpu_hot') ? 'warning' : 'default'} />
                    </Tooltip>
                  </>
                )}
                {a.warnings.map((w) => (
                  <Chip key={w} size="small" color={WARNING_TONE[w] ?? 'info'} label={t(`agentHealth.warnings.${w}`, w.replace(/_/g, ' ')) as string} />
                ))}
                {!a.current_job_id && a.last_heartbeat && a.status !== 'active' && (
                  <Typography variant="caption" color="text.secondary">
                    {t('agentHealth.lastSeen', 'Last seen {{when}}', { when: formatRelativeTime(a.last_heartbeat) }) as string}
                  </Typography>
                )}
              </Box>
            </ListItem>
          ))}
        </List>
      )}
    </SectionCard>
  );
};

export default AgentHealthPanel;
