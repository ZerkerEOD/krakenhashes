import type { TranslationNamespace } from '../../i18n/types';
import React from 'react';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
import {
  Badge,
  Box,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  ListSubheader,
  MenuItem,
  TextField,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { SettingsGroup } from './navTypes';
import type { Attention } from '../../services/settingsStatus';
import type { StatusKey } from '../../services/settingsStatus';

export interface SettingsRailProps {
  nav: SettingsGroup[];
  basePath: string;
  /** i18n namespace for labelKey lookups. */
  ns: TranslationNamespace;
  attentionFor?: (keys: StatusKey[] | undefined) => Attention | undefined;
  /** Label for the index (hub) entry; omit to hide it. */
  hubLabelKey?: string;
}

const sectionPath = (basePath: string, group: SettingsGroup, sectionPathSeg: string) =>
  group.path ? `${basePath}/${group.path}/${sectionPathSeg}` : `${basePath}/${sectionPathSeg}`;

/**
 * Left-hand navigation for a settings area: grouped sections with attention
 * badges. Collapses to a select below the `md` breakpoint.
 */
const SettingsRail: React.FC<SettingsRailProps> = ({ nav, basePath, ns, attentionFor, hubLabelKey }) => {
  const { t } = useTranslation(ns);
  const location = useLocation();
  const navigate = useNavigate();
  const theme = useTheme();
  const compact = useMediaQuery(theme.breakpoints.down('md'));

  const isActive = (path: string) => location.pathname === path || location.pathname.startsWith(`${path}/`);
  const badgeColor = (a?: Attention) => (a?.severity === 'error' ? 'error' : 'warning');

  if (compact) {
    const current =
      nav.flatMap((g) => g.sections.map((s) => sectionPath(basePath, g, s.path))).find(isActive) ?? basePath;
    return (
      <TextField
        select
        fullWidth
        size="small"
        value={current}
        onChange={(e) => navigate(e.target.value)}
        sx={{ mb: 2 }}
        inputProps={{ 'aria-label': t('settingsNav.ariaLabel', { defaultValue: 'Settings section' }) as string }}
      >
        {hubLabelKey && <MenuItem value={basePath}>{t(hubLabelKey) as string}</MenuItem>}
        {nav.flatMap((g) => [
          <ListSubheader key={`h-${g.id}`}>{t(g.labelKey) as string}</ListSubheader>,
          ...g.sections.map((s) => (
            <MenuItem key={s.id} value={sectionPath(basePath, g, s.path)} sx={{ pl: 3 }}>
              {t(s.labelKey) as string}
            </MenuItem>
          )),
        ])}
      </TextField>
    );
  }

  return (
    <Box
      component="nav"
      aria-label={t('settingsNav.ariaLabel', { defaultValue: 'Settings navigation' }) as string}
      sx={{ width: 232, flexShrink: 0, position: 'sticky', top: 72, alignSelf: 'flex-start' }}
    >
      <List dense disablePadding sx={{ '& .MuiListItemButton-root': { py: 0.75, pl: 1.5 } }}>
        {hubLabelKey && (
          <ListItemButton component={NavLink} to={basePath} end selected={location.pathname === basePath} sx={{ mb: 1 }}>
            <ListItemText primary={t(hubLabelKey) as string} primaryTypographyProps={{ fontWeight: 600 }} />
          </ListItemButton>
        )}
        {nav.map((group) => {
          const groupAttention = attentionFor?.(group.sections.flatMap((s) => s.statusKeys ?? []));
          return (
            <Box key={group.id} sx={{ mb: 1.5 }}>
              <ListSubheader disableSticky sx={{ display: 'flex', alignItems: 'center', gap: 1, pl: 1.5, lineHeight: '28px' }}>
                <Box sx={{ display: 'flex', '& svg': { fontSize: 16 } }}>{group.icon}</Box>
                {t(group.labelKey) as string}
                {groupAttention && (
                  <Badge variant="dot" color={badgeColor(groupAttention)} sx={{ ml: 0.5 }} />
                )}
              </ListSubheader>
              {group.sections.map((section) => {
                const path = sectionPath(basePath, group, section.path);
                const attention = attentionFor?.(section.statusKeys);
                return (
                  <ListItemButton key={section.id} component={NavLink} to={path} selected={isActive(path)}>
                    <ListItemText primary={t(section.labelKey) as string} primaryTypographyProps={{ variant: 'body2' }} />
                    {attention && (
                      <ListItemIcon sx={{ minWidth: 0 }}>
                        <Badge variant="dot" color={badgeColor(attention)} />
                      </ListItemIcon>
                    )}
                  </ListItemButton>
                );
              })}
            </Box>
          );
        })}
      </List>
    </Box>
  );
};

export default SettingsRail;
