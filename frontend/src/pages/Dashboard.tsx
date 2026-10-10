import React, { useEffect, useState } from 'react';
import { Box, Chip, Stack, ToggleButton, ToggleButtonGroup } from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../components/ui';
import { useTeamFilter } from '../contexts/TeamFilterContext';
import { usePolling } from '../contexts/PollingContext';
import { useAuth } from '../contexts/AuthContext';
import KpiTiles from '../components/dashboard/KpiTiles';
import ActiveJobsWidget from '../components/dashboard/ActiveJobsWidget';
import AgentHealthPanel from '../components/dashboard/AgentHealthPanel';
import RecentCracksFeed from '../components/dashboard/RecentCracksFeed';
import ActivityFeed from '../components/dashboard/ActivityFeed';
import HashlistOverview from '../components/dashboard/HashlistOverview';
import AttentionPanel from '../components/dashboard/AttentionPanel';
import CrackTrendCard from '../components/dashboard/CrackTrendCard';
import LoopbackSessionsPanel from '../components/jobs/LoopbackSessionsPanel';
import type { DashboardView } from '../services/dashboard';

const VIEW_STORAGE_KEY = 'kh.dashboard.view';

const readView = (): DashboardView | null => {
  try {
    const v = localStorage.getItem(VIEW_STORAGE_KEY);
    return v === 'mine' || v === 'teams' || v === 'all' ? v : null;
  } catch {
    return null;
  }
};

const writeView = (v: DashboardView) => {
  try {
    localStorage.setItem(VIEW_STORAGE_KEY, v);
  } catch {
    /* storage unavailable: the choice just isn't remembered */
  }
};

/**
 * Personal dashboard. Every widget fetches independently (and fails
 * independently), polls on its own tier, and follows the view switch:
 *  - Mine (default): my jobs, hashlists, cracks and agents.
 *  - My teams (teams enabled): everything in my teams, or the app-bar team.
 *  - All: everything; offered to everyone when teams are off (one shared
 *    workspace) and only to admins when teams are on. The server enforces this.
 */
const Dashboard: React.FC = () => {
  const { t } = useTranslation('dashboard');
  const { user, userRole } = useAuth();
  const { teamsEnabled, selectedTeamId } = useTeamFilter();
  const { enabled: polling, setEnabled: setPolling } = usePolling();
  const teamId = teamsEnabled ? selectedTeamId : null;

  const available: DashboardView[] = ['mine'];
  if (teamsEnabled) available.push('teams');
  if (!teamsEnabled || userRole === 'admin') available.push('all');
  const [stored, setStored] = useState<DashboardView>(() => readView() ?? 'mine');
  const view: DashboardView = available.includes(stored) ? stored : 'mine';
  // Settle a remembered view that is no longer allowed (role or teams setting changed).
  useEffect(() => {
    if (view !== stored) setStored(view);
  }, [view, stored]);
  const changeView = (_: unknown, next: DashboardView | null) => {
    if (!next) return;
    setStored(next);
    writeView(next);
  };
  const viewLabels: Record<DashboardView, string> = {
    mine: t('view.mine', 'Mine') as string,
    teams: t('view.teams', 'My teams') as string,
    all: t('view.all', 'All') as string,
  };

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('title') as string}
        description={user?.username ? (t('welcomeUser', 'Welcome back, {{name}}', { name: user.username }) as string) : undefined}
        actions={
          <Stack direction="row" spacing={1.5} alignItems="center">
            {available.length > 1 && (
              <ToggleButtonGroup
                size="small"
                exclusive
                value={view}
                onChange={changeView}
                aria-label={t('view.label', 'Dashboard view') as string}
              >
                {available.map((v) => (
                  <ToggleButton key={v} value={v} sx={{ px: 1.5, py: 0.25, textTransform: 'none' }}>
                    {viewLabels[v]}
                  </ToggleButton>
                ))}
              </ToggleButtonGroup>
            )}
            <Chip
              icon={<RefreshIcon />}
              label={polling ? (t('autoRefresh.on', 'Live') as string) : (t('autoRefresh.off', 'Paused') as string)}
              color={polling ? 'success' : 'default'}
              variant="outlined"
              size="small"
              onClick={() => setPolling(!polling)}
            />
          </Stack>
        }
      />

      <Box sx={{ mb: 3 }}>
        <KpiTiles view={view} teamId={teamId} />
      </Box>

      {/*
        One CSS grid with named areas: every card shares the same columns and gap,
        cards in a row stretch to equal height, and agent health is pinned to the
        jobs row on wide screens, scrolling inside its own card instead of growing the page.
      */}
      <Box
        sx={{
          display: 'grid',
          gap: 3,
          gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(3, minmax(0, 1fr))' },
          gridTemplateAreas: {
            xs: '"jobs" "attention" "agents" "cracks" "activity" "trend" "hashlists"',
            md: '"jobs jobs" "agents attention" "cracks activity" "hashlists trend"',
            lg: '"jobs jobs agents" "cracks activity attention" "hashlists hashlists trend"',
          },
        }}
      >
        <Stack spacing={3} sx={{ gridArea: 'jobs', minWidth: 0 }}>
          <ActiveJobsWidget view={view} teamId={teamId} />
          <LoopbackSessionsPanel scope={view === 'mine' ? 'mine' : 'visible'} />
        </Stack>
        {/* On lg the card is pinned to the area so its long list never sets the row height. */}
        <Box sx={{ gridArea: 'agents', position: 'relative', minWidth: 0, minHeight: { lg: 360 } }}>
          <Box sx={{ position: { lg: 'absolute' }, inset: { lg: 0 }, height: { lg: '100%' } }}>
            <AgentHealthPanel view={view} teamId={teamId} />
          </Box>
        </Box>
        <Box sx={{ gridArea: 'cracks', minWidth: 0 }}>
          <RecentCracksFeed view={view} teamId={teamId} />
        </Box>
        <Box sx={{ gridArea: 'activity', minWidth: 0 }}>
          <ActivityFeed />
        </Box>
        <Box sx={{ gridArea: 'attention', minWidth: 0 }}>
          <AttentionPanel view={view} teamId={teamId} />
        </Box>
        <Box sx={{ gridArea: 'hashlists', minWidth: 0 }}>
          <HashlistOverview view={view} teamId={teamId} />
        </Box>
        <Box sx={{ gridArea: 'trend', minWidth: 0 }}>
          <CrackTrendCard view={view} teamId={teamId} />
        </Box>
      </Box>
    </Box>
  );
};

export default Dashboard;
