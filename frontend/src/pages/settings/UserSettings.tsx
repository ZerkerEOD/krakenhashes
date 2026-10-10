import React, { Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import SettingsShell from '../../components/settings/SettingsShell';
import { SettingsLoading } from '../../components/settings/fields';
import ErrorBoundary from '../../components/ui/ErrorBoundary';
import { ROUTES } from '../../constants/routes';
import { USER_SETTINGS_NAV } from './userSettingsNav';

/** `/settings/*`: the user's own settings behind the shared settings shell. */
const UserSettings: React.FC = () => {
  const { t } = useTranslation('settings');
  return (
    <SettingsShell nav={USER_SETTINGS_NAV} basePath={ROUTES.settings} ns="settings" title={t('title') as string} description={t('nav.description') as string}>
      <Routes>
        <Route index element={<Navigate to={ROUTES.settingsProfile} replace />} />
        {USER_SETTINGS_NAV.flatMap((g) =>
          g.sections.map((s) => (
            <Route
              key={s.id}
              path={s.path}
              element={
                <ErrorBoundary compact>
                  <Suspense fallback={<SettingsLoading panels={1} />}>
                    <s.Component />
                  </Suspense>
                </ErrorBoundary>
              }
            />
          ))
        )}
        <Route path="*" element={<Navigate to={ROUTES.settingsProfile} replace />} />
      </Routes>
    </SettingsShell>
  );
};

export default UserSettings;
