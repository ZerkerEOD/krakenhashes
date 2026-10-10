import React, { useEffect, useRef } from 'react';
import { Box, IconButton, List, ListItem, Tooltip, Typography } from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import Button from '@mui/material/Button';
import { EmptyState, EntityLink, ErrorState, SectionCard, useToast } from '../ui';
import { feedListSx } from './layout';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { getRecentCracks, DashboardView } from '../../services/dashboard';
import { useNotifications } from '../../contexts/NotificationContext';
import { formatRelativeTime } from '../../utils/formatters';
import { ROUTES } from '../../constants/routes';

/**
 * Most recent cracks, plaintext shown (internal tool, operators need it).
 * Refreshes on a timer and immediately when a crack notification arrives.
 */
const RecentCracksFeed: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const toast = useToast();
  const queryClient = useQueryClient();
  const { recentNotifications } = useNotifications();
  const key = qk.dashboard.recentCracks(view, teamId, 15);
  const q = useLiveQuery({ queryKey: key, queryFn: () => getRecentCracks(view, teamId, 15) }, { tier: 'list' });

  const lastSeen = useRef<string | undefined>(undefined);
  useEffect(() => {
    const newest = recentNotifications[0];
    if (!newest || newest.id === lastSeen.current) return;
    const first = lastSeen.current === undefined;
    lastSeen.current = newest.id;
    if (!first && /crack/i.test(String(newest.notification_type))) void queryClient.invalidateQueries({ queryKey: key });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recentNotifications]);

  const cracks = q.data ?? [];
  const mono = (theme: any) => theme.typography.monoFamily;

  return (
    <SectionCard
      title={t('recentCracks.title', 'Recent cracks') as string}
      actions={<Button size="small" component={RouterLink} to={ROUTES.pot}>{t('recentCracks.viewAll', 'All cracked hashes') as string}</Button>}
      loading={q.isLoading}
      flush
      fill
    >
      {q.error && !q.data ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} compact />
      ) : cracks.length === 0 && !q.isLoading ? (
        <EmptyState size="sm" title={t('recentCracks.empty', 'Nothing cracked yet') as string} />
      ) : (
        <List dense disablePadding sx={feedListSx}>
          {cracks.map((c) => (
            <ListItem key={c.hash_id} divider sx={{ display: 'block', py: 0.75, px: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
                <Typography variant="body2" sx={{ fontFamily: mono, fontWeight: 600, wordBreak: 'break-all' }}>
                  {c.plaintext}
                </Typography>
                <Tooltip title={t('recentCracks.copy', 'Copy plaintext') as string}>
                  <IconButton
                    size="small"
                    aria-label={t('recentCracks.copy', 'Copy plaintext') as string}
                    onClick={() => {
                      void navigator.clipboard.writeText(c.plaintext);
                      toast.success(t('recentCracks.copied', 'Copied') as string);
                    }}
                  >
                    <ContentCopyIcon sx={{ fontSize: 14 }} />
                  </IconButton>
                </Tooltip>
                <Box sx={{ flexGrow: 1 }} />
                <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
                  {formatRelativeTime(c.cracked_at)}
                </Typography>
              </Box>
              <Typography variant="caption" color="text.secondary" component="div" noWrap>
                {c.username && (
                  <>
                    {c.domain ? `${c.domain}\\` : ''}
                    {c.username}
                    {' · '}
                  </>
                )}
                {c.hashlist_id ? <EntityLink type="hashlist" id={c.hashlist_id} label={c.hashlist_name} color="inherit" /> : null}
                {c.job_id && (
                  <>
                    {' · '}
                    <EntityLink type="job" id={c.job_id} label={c.job_name} color="inherit" />
                  </>
                )}
                {c.agent_id && (
                  <>
                    {' · '}
                    <EntityLink type="agent" id={c.agent_id} label={c.agent_name} color="inherit" />
                  </>
                )}
              </Typography>
            </ListItem>
          ))}
        </List>
      )}
    </SectionCard>
  );
};

export default RecentCracksFeed;
