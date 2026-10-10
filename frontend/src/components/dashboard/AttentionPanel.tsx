import React from 'react';
import { Box, Chip, List, ListItemButton, Tooltip, Typography } from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { EmptyState, ErrorState, SectionCard } from '../ui';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { getAttention, AttentionItem, DashboardView } from '../../services/dashboard';
import { entityRoute } from '../../constants/routes';
import { formatRelativeTime } from '../../utils/formatters';
import { feedListSx } from './layout';

/**
 * Things that are broken or waiting on the user in the selected view: failed or
 * blocked jobs, problem hashlists, agents in error. Each row opens the entity.
 */
const AttentionPanel: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const navigate = useNavigate();
  const q = useLiveQuery(
    { queryKey: qk.dashboard.attention(view, teamId), queryFn: () => getAttention(view, teamId) },
    { tier: 'list' }
  );
  const items = q.data ?? [];
  const errors = items.filter((i) => i.severity === 'error').length;

  const title = (item: AttentionItem) => t(`attention.kind.${item.kind}`, { name: item.name }) as string;

  return (
    <SectionCard
      title={t('attention.title', 'Needs attention') as string}
      actions={
        items.length > 0 ? (
          <Chip size="small" color={errors > 0 ? 'error' : 'warning'} variant="outlined" label={items.length} />
        ) : undefined
      }
      loading={q.isLoading}
      flush
      fill
    >
      {q.error && !q.data ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} compact />
      ) : items.length === 0 ? (
        !q.isLoading && (
          <EmptyState
            size="sm"
            icon={<CheckCircleOutlineIcon color="success" />}
            title={t('attention.allClear', 'All clear') as string}
            description={t('attention.allClearHint', 'Nothing is failing or waiting on you.') as string}
          />
        )
      ) : (
        <List dense disablePadding sx={feedListSx}>
          {items.map((item) => (
            <ListItemButton
              key={`${item.kind}:${item.entity_id}`}
              divider
              onClick={() => navigate(entityRoute(item.entity_type, item.entity_id))}
              sx={{ alignItems: 'flex-start', py: 1, gap: 1.5 }}
            >
              {item.severity === 'error' ? (
                <ErrorOutlineIcon fontSize="small" color="error" sx={{ mt: 0.25 }} />
              ) : (
                <WarningAmberIcon fontSize="small" color="warning" sx={{ mt: 0.25 }} />
              )}
              <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <Typography variant="body2" sx={{ fontWeight: 500, flexGrow: 1 }} noWrap>
                    {title(item)}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
                    {formatRelativeTime(item.at)}
                  </Typography>
                </Box>
                {item.detail && (
                  <Tooltip title={item.detail} placement="bottom-start">
                    <Typography variant="caption" color="text.secondary" component="div" noWrap>
                      {item.detail}
                    </Typography>
                  </Tooltip>
                )}
              </Box>
            </ListItemButton>
          ))}
        </List>
      )}
    </SectionCard>
  );
};

export default AttentionPanel;
