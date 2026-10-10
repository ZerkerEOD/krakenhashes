import React from 'react';
import { Grid } from '@mui/material';
import WorkIcon from '@mui/icons-material/Work';
import ComputerIcon from '@mui/icons-material/Computer';
import LockOpenIcon from '@mui/icons-material/LockOpen';
import SpeedIcon from '@mui/icons-material/Speed';
import ListAltIcon from '@mui/icons-material/ListAlt';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import { useTranslation } from 'react-i18next';
import { StatTile, ErrorState } from '../ui';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { getDashboardStats, DashboardView } from '../../services/dashboard';
import { formatHashRate } from '../../utils/formatters';
import { ROUTES } from '../../constants/routes';

/** Six headline numbers for the selected dashboard view. */
const KpiTiles: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const q = useLiveQuery(
    { queryKey: qk.dashboard.stats(view, teamId), queryFn: () => getDashboardStats(view, teamId) },
    { tier: 'fast' }
  );
  if (q.error && !q.data) return <ErrorState error={q.error} onRetry={() => void q.refetch()} compact />;
  const s = q.data;
  const loading = q.isLoading;
  const crackPct = s && s.hashlists.total_hashes > 0 ? (s.hashlists.cracked_hashes / s.hashlists.total_hashes) * 100 : 0;
  const agentProblems = s ? s.agents.error + s.agents.offline : 0;

  type Tile = { key: string; label: unknown; value: React.ReactNode; caption?: unknown; icon: React.ReactNode; tone: string; to?: string };
  const tiles: Tile[] = [
    {
      key: 'running',
      label: t('kpi.runningJobs', 'Running jobs'),
      value: s ? s.jobs.running : '-',
      caption: s ? t('kpi.queued', '{{count}} queued', { count: s.jobs.pending + s.jobs.paused }) : undefined,
      icon: <WorkIcon />,
      tone: s && s.jobs.running > 0 ? 'running' : 'neutral',
      to: ROUTES.jobs,
    },
    {
      key: 'agents',
      label: t('kpi.agentsOnline', 'Agents online'),
      value: s ? `${s.agents.online} / ${s.agents.total}` : '-',
      caption: s && agentProblems > 0 ? t('kpi.agentProblems', '{{count}} need attention', { count: agentProblems }) : undefined,
      icon: <ComputerIcon />,
      tone: agentProblems > 0 ? 'warning' : 'success',
      to: ROUTES.agents,
    },
    {
      key: 'hashrate',
      label: t('kpi.hashRate', 'Hash rate'),
      value: s ? formatHashRate(s.hash_rate) : '-',
      icon: <SpeedIcon />,
      tone: 'primary',
    },
    {
      key: 'cracks',
      label: t('kpi.cracks24h', 'Cracked (24h)'),
      value: s ? s.cracks.last_24h.toLocaleString() : '-',
      caption: s ? t('kpi.cracks7d', '{{count}} in 7 days', { count: s.cracks.last_7d }) : undefined,
      icon: <LockOpenIcon />,
      tone: 'success',
      to: ROUTES.pot,
    },
    {
      key: 'hashlists',
      label: t('kpi.crackRate', 'Crack rate'),
      value: s ? `${crackPct.toFixed(1)}%` : '-',
      caption: s ? t('kpi.hashlistTotals', '{{cracked}} of {{total}} hashes', { cracked: s.hashlists.cracked_hashes.toLocaleString(), total: s.hashlists.total_hashes.toLocaleString() }) : undefined,
      icon: <ListAltIcon />,
      tone: 'neutral',
      to: ROUTES.hashlists,
    },
    {
      key: 'failed',
      label: t('kpi.failed24h', 'Failed (24h)'),
      value: s ? s.jobs.failed_24h : '-',
      caption: s ? t('kpi.completed24h', '{{count}} completed', { count: s.jobs.completed_24h }) : undefined,
      icon: <ErrorOutlineIcon />,
      tone: s && s.jobs.failed_24h > 0 ? 'error' : 'neutral',
      to: ROUTES.jobs,
    },
  ];

  return (
    <Grid container spacing={2}>
      {tiles.map((tile) => (
        <Grid item key={tile.key} xs={6} sm={4} lg={2}>
          <StatTile
            label={String(tile.label)}
            value={tile.value}
            caption={tile.caption ? String(tile.caption) : undefined}
            icon={tile.icon}
            tone={tile.tone as any}
            to={tile.to}
            loading={loading}
          />
        </Grid>
      ))}
    </Grid>
  );
};

export default KpiTiles;
