import type { TranslationNamespace } from '../../i18n/types';
import React from 'react';
import { useLocation } from 'react-router-dom';
import { Box, Chip, Tooltip, Typography } from '@mui/material';
import BoltIcon from '@mui/icons-material/Bolt';
import SaveIcon from '@mui/icons-material/Save';
import { useTranslation } from 'react-i18next';
import PageHeader from '../ui/PageHeader';
import SettingsRail from './SettingsRail';
import type { SettingsGroup, SettingsSection } from './navTypes';
import { sectionPath } from './navTypes';
import type { Attention, StatusKey } from '../../services/settingsStatus';

export interface SettingsShellProps {
  nav: SettingsGroup[];
  basePath: string;
  ns: TranslationNamespace;
  title: string;
  description?: string;
  hubLabelKey?: string;
  attentionFor?: (keys: StatusKey[] | undefined) => Attention | undefined;
  /** Header actions on the page level. */
  actions?: React.ReactNode;
  children: React.ReactNode;
}

/** Finds the active group/section for the current URL. */
export const useActiveSection = (nav: SettingsGroup[], basePath: string) => {
  const { pathname } = useLocation();
  for (const group of nav) {
    for (const section of group.sections) {
      const path = sectionPath(basePath, group, section);
      if (pathname === path || pathname.startsWith(`${path}/`)) return { group, section };
    }
  }
  return { group: undefined, section: undefined as SettingsSection | undefined };
};

/** Save-mode hint shown next to a section title. */
export const SaveModeChip: React.FC<{ mode: SettingsSection['saveMode']; ns: TranslationNamespace }> = ({ mode, ns }) => {
  const { t } = useTranslation(ns);
  if (mode === 'mixed') return null;
  const apply = mode === 'apply';
  return (
    <Tooltip title={t(apply ? 'fieldState.applyHintLong' : 'fieldState.autosaveHintLong') as string}>
      <Chip
        size="small"
        variant="outlined"
        icon={apply ? <SaveIcon /> : <BoltIcon />}
        color={apply ? 'warning' : 'success'}
        label={t(apply ? 'fieldState.applyHint' : 'fieldState.autosaveHint') as string}
        sx={{ fontWeight: 500 }}
      />
    </Tooltip>
  );
};

/**
 * Page frame for a settings area: page header, left rail, and a content column
 * that shows the active section's title, description and save-mode chip above
 * whatever route renders inside it.
 */
const SettingsShell: React.FC<SettingsShellProps> = ({
  nav,
  basePath,
  ns,
  title,
  description,
  hubLabelKey,
  attentionFor,
  actions,
  children,
}) => {
  const { t } = useTranslation(ns);
  const { group, section } = useActiveSection(nav, basePath);

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader title={title} description={description} actions={actions} />
      <Box sx={{ display: 'flex', gap: 3, alignItems: 'flex-start', flexDirection: { xs: 'column', md: 'row' } }}>
        <SettingsRail nav={nav} basePath={basePath} ns={ns} attentionFor={attentionFor} hubLabelKey={hubLabelKey} />
        <Box sx={{ flexGrow: 1, minWidth: 0, width: '100%' }}>
          {section && group && (
            <Box sx={{ mb: 2.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
                <Typography variant="overline" color="text.secondary">
                  {t(group.labelKey) as string}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
                <Typography variant="h5" component="h2">
                  {t(section.labelKey) as string}
                </Typography>
                <SaveModeChip mode={section.saveMode} ns={ns} />
              </Box>
              {section.descKey && (
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                  {t(section.descKey) as string}
                </Typography>
              )}
            </Box>
          )}
          {children}
        </Box>
      </Box>
    </Box>
  );
};

export default SettingsShell;
