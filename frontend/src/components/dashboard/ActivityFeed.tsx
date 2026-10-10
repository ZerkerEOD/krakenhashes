import React, { useEffect } from 'react';
import { Box, List, ListItemButton, Typography } from '@mui/material';
import CircleIcon from '@mui/icons-material/Circle';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import Button from '@mui/material/Button';
import { EmptyState, SectionCard } from '../ui';
import { feedListSx } from './layout';
import { useNotifications } from '../../contexts/NotificationContext';
import { entityRoute, ROUTES } from '../../constants/routes';
import { formatRelativeTime } from '../../utils/formatters';

/** The user's latest notifications, pushed live over the notification socket. */
const ActivityFeed: React.FC = () => {
  const { t } = useTranslation('dashboard');
  const navigate = useNavigate();
  const { recentNotifications, refreshRecentNotifications, markAsRead, isLoading } = useNotifications();

  useEffect(() => {
    void refreshRecentNotifications();
  }, [refreshRecentNotifications]);

  return (
    <SectionCard
      title={t('recentActivity.title') as string}
      actions={<Button size="small" component={RouterLink} to={ROUTES.notifications}>{t('recentActivity.viewAll', 'All notifications') as string}</Button>}
      loading={isLoading && recentNotifications.length === 0}
      flush
      fill
    >
      {recentNotifications.length === 0 ? (
        <EmptyState size="sm" title={t('recentActivity.noActivity') as string} />
      ) : (
        <List dense disablePadding sx={feedListSx}>
          {recentNotifications.map((n) => (
            <ListItemButton
              key={n.id}
              divider
              onClick={() => {
                if (!n.in_app_read) void markAsRead(n.id);
                navigate(entityRoute(n.source_type ?? 'notification', n.source_id, { parentJobId: (n.data?.job_id as string | undefined) ?? undefined }));
              }}
              sx={{ alignItems: 'flex-start', py: 1 }}
            >
              <CircleIcon sx={{ fontSize: 8, mt: 0.9, mr: 1.5, color: n.in_app_read ? 'transparent' : 'primary.main' }} />
              <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <Typography variant="body2" sx={{ fontWeight: n.in_app_read ? 400 : 600, flexGrow: 1 }} noWrap>
                    {n.title}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
                    {formatRelativeTime(n.created_at)}
                  </Typography>
                </Box>
                <Typography variant="caption" color="text.secondary" component="div" sx={{ display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                  {n.message}
                </Typography>
              </Box>
            </ListItemButton>
          ))}
        </List>
      )}
    </SectionCard>
  );
};

export default ActivityFeed;
