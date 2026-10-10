import React, { useEffect } from 'react';
import { Link as RouterLink, useNavigate } from 'react-router-dom';
import { Box, Button, Grid, List, ListItemButton, ListItemIcon, ListItemText, Paper, Typography } from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import { useTranslation } from 'react-i18next';
import { EmptyState, SectionCard, StatusChip } from '../../components/ui';
import { useSettingsStatus } from '../../hooks/useSettingsStatus';
import { ROUTES } from '../../constants/routes';
import type { SettingsStatus, StatusKey } from '../../services/settingsStatus';
import { settingsNav, LEGACY_TAB_PATHS } from './settingsNav';

/**
 * One-time migration of the old horizontal-tab index to the new URL. The key
 * is removed BEFORE navigating so a failed navigation cannot loop.
 */
const useLegacyTabRedirect = () => {
  const navigate = useNavigate();
  useEffect(() => {
    let raw: string | null = null;
    try {
      raw = window.localStorage.getItem('adminSettingsTab');
      if (raw !== null) window.localStorage.removeItem('adminSettingsTab');
    } catch {
      return;
    }
    if (raw === null) return;
    const idx = parseInt(raw, 10);
    const target = LEGACY_TAB_PATHS[idx];
    if (!target) return;
    navigate(target.startsWith('/') ? target : `${ROUTES.admin.settings}/${target}`, { replace: true });
  }, [navigate]);
};

interface CardSpec {
  key: StatusKey;
  to: string;
  titleKey: string;
  detail: (s: SettingsStatus, t: (k: string, o?: any) => string) => string;
}

const CARDS: CardSpec[] = [
  {
    key: 'email',
    to: 'integrations/email',
    titleKey: 'hub.cards.email',
    detail: (s, t) => (s.email.configured ? t('hub.detail.emailConfigured') : t('hub.detail.emailNotConfigured')),
  },
  {
    key: 'certificate',
    to: 'general/certificate',
    titleKey: 'hub.cards.certificate',
    detail: (s, t) =>
      !s.certificate.managed
        ? t('hub.detail.certUnmanaged', { mode: s.certificate.tls_mode })
        : s.certificate.days_remaining != null
        ? t('hub.detail.certDays', { days: s.certificate.days_remaining, mode: s.certificate.tls_mode })
        : t('hub.detail.certPending'),
  },
  {
    key: 'sso',
    to: 'security/sso',
    titleKey: 'hub.cards.sso',
    detail: (s, t) => t('hub.detail.ssoProviders', { enabled: s.sso.providers_enabled, total: s.sso.providers_total }),
  },
  {
    key: 'binaries',
    to: ROUTES.admin.binaries,
    titleKey: 'hub.cards.binaries',
    detail: (s, t) => t('hub.detail.binaries', { verified: s.binaries.verified_active, total: s.binaries.total }),
  },
  {
    key: 'cloud',
    to: 'cloud/limits',
    titleKey: 'hub.cards.cloud',
    detail: (s, t) =>
      s.cloud.providers === 0
        ? t('hub.detail.cloudNoProviders')
        : t('hub.detail.cloudCap', { providers: s.cloud.providers, cap: (s.cloud.monthly_cap_cents / 100).toLocaleString() }),
  },
  {
    key: 'storage',
    to: 'general/storage',
    titleKey: 'hub.cards.storage',
    detail: (s, t) =>
      !s.storage.enabled
        ? t('hub.detail.storageLocal')
        : s.storage.reachable
        ? t('hub.detail.storageReachable', { backend: s.storage.backend })
        : t('hub.detail.storageUnreachable'),
  },
  {
    key: 'webhook',
    to: 'integrations/webhooks',
    titleKey: 'hub.cards.webhook',
    detail: (s, t) =>
      !s.webhook.enabled ? t('hub.detail.webhookOff') : s.webhook.has_url ? t('hub.detail.webhookOn') : t('hub.detail.webhookNoUrl'),
  },
];

const SettingsHub: React.FC = () => {
  const { t } = useTranslation('admin');
  const { status, attention, attentionCount, isLoading, isError } = useSettingsStatus();
  useLegacyTabRedirect();

  const tr = (k: string, o?: any) => t(k, o) as string;
  const resolve = (to: string) => (to.startsWith('/') ? to : `${ROUTES.admin.settings}/${to}`);
  const needs = CARDS.filter((c) => attention && attention[c.key].severity !== 'ok');

  return (
    <Box>
      <SectionCard
        title={t('hub.attentionTitle') as string}
        subtitle={t('hub.attentionSubtitle') as string}
        flush
        loading={isLoading}
        error={isError ? t('hub.loadFailed') : undefined}
      >
        {!isLoading && !isError && needs.length === 0 && (
          <EmptyState
            size="sm"
            icon={<CheckCircleOutlineIcon color="success" />}
            title={t('hub.allGood') as string}
            description={t('hub.allGoodHint') as string}
          />
        )}
        {needs.length > 0 && status && attention && (
          <List disablePadding>
            {needs.map((c) => {
              const a = attention[c.key];
              return (
                <ListItemButton key={c.key} component={RouterLink} to={resolve(c.to)} divider>
                  <ListItemIcon>
                    {a.severity === 'error' ? <ErrorOutlineIcon color="error" /> : <WarningAmberIcon color="warning" />}
                  </ListItemIcon>
                  <ListItemText
                    primary={t(c.titleKey) as string}
                    secondary={t(`hub.attention.${a.messageKey}`, a.params) as string}
                  />
                  <ChevronRightIcon color="action" />
                </ListItemButton>
              );
            })}
          </List>
        )}
      </SectionCard>

      <Typography variant="h6" component="h2" sx={{ mt: 4, mb: 1.5 }}>
        {t('hub.overviewTitle') as string}
        {attentionCount > 0 && (
          <Typography component="span" variant="body2" color="text.secondary" sx={{ ml: 1 }}>
            {t('hub.attentionCount', { count: attentionCount }) as string}
          </Typography>
        )}
      </Typography>
      <Grid container spacing={2}>
        {CARDS.map((c) => {
          const a = attention?.[c.key];
          const tone = !a ? 'default' : a.severity === 'ok' ? 'success' : a.severity;
          return (
            <Grid item xs={12} sm={6} lg={4} key={c.key}>
              <Paper
                variant="outlined"
                component={RouterLink}
                to={resolve(c.to)}
                sx={{
                  display: 'block',
                  p: 2,
                  textDecoration: 'none',
                  color: 'inherit',
                  height: '100%',
                  transition: 'border-color 150ms',
                  '&:hover': { borderColor: 'primary.main' },
                }}
              >
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1, mb: 1 }}>
                  <Typography variant="subtitle2">{t(c.titleKey) as string}</Typography>
                  <StatusChip
                    entity="generic"
                    status={tone === 'default' ? 'unknown' : tone === 'success' ? 'ok' : tone}
                    label={t(`hub.severity.${tone}`) as string}
                  />
                </Box>
                <Typography variant="body2" color="text.secondary">
                  {status ? c.detail(status, tr) : isLoading ? (t('hub.loading') as string) : '—'}
                </Typography>
              </Paper>
            </Grid>
          );
        })}
      </Grid>

      <Typography variant="h6" component="h2" sx={{ mt: 4, mb: 1.5 }}>
        {t('hub.browseTitle') as string}
      </Typography>
      <Grid container spacing={2}>
        {settingsNav.map((g) => (
          <Grid item xs={12} sm={6} lg={4} key={g.id}>
            <Paper variant="outlined" sx={{ p: 2, height: '100%' }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                <Box sx={{ display: 'flex', color: 'text.secondary' }}>{g.icon}</Box>
                <Typography variant="subtitle2">{t(g.labelKey) as string}</Typography>
              </Box>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                {g.sections.map((s) => (
                  <Button
                    key={s.id}
                    size="small"
                    variant="text"
                    component={RouterLink}
                    to={`${ROUTES.admin.settings}/${g.path}/${s.path}`}
                    sx={{ px: 1 }}
                  >
                    {t(s.labelKey) as string}
                  </Button>
                ))}
              </Box>
            </Paper>
          </Grid>
        ))}
      </Grid>
    </Box>
  );
};

export default SettingsHub;
