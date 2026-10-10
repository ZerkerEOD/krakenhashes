import React from 'react';
import { Outlet } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import SettingsShell from '../../components/settings/SettingsShell';
import { useSettingsStatus } from '../../hooks/useSettingsStatus';
import { ROUTES } from '../../constants/routes';
import { settingsNav } from './settingsNav';

/** Admin settings frame: rail + section header around the routed section. */
const SettingsLayout: React.FC = () => {
  const { t } = useTranslation('admin');
  const { attentionFor } = useSettingsStatus();
  return (
    <SettingsShell
      nav={settingsNav}
      basePath={ROUTES.admin.settings}
      ns="admin"
      title={t('title') as string}
      description={t('hub.pageDescription') as string}
      hubLabelKey="hub.navLabel"
      attentionFor={attentionFor}
    >
      <Outlet />
    </SettingsShell>
  );
};

export default SettingsLayout;
