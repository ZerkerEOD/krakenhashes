import React, { Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { SettingsLoading } from '../../components/settings/fields';
import ErrorBoundary from '../../components/ui/ErrorBoundary';
import { ROUTES } from '../../constants/routes';
import SettingsLayout from './SettingsLayout';
import SettingsHub from './SettingsHub';
import { settingsNav } from './settingsNav';

/**
 * Admin settings: a left-rail layout whose routes are generated from
 * `settingsNav`. Mounted at /admin/settings/* by App.tsx.
 */
export const AdminSettings: React.FC = () => {
  const abs = (group: string, section?: string) => ROUTES.admin.settingsSection(group, section);
  return (
    <Routes>
      <Route element={<SettingsLayout />}>
        <Route index element={<SettingsHub />} />
        {settingsNav.flatMap((g) => [
          <Route key={g.id} path={g.path} element={<Navigate to={abs(g.path, g.sections[0].path)} replace />} />,
          ...g.sections.map((s) => (
            <Route
              key={s.id}
              path={s.nested ? `${g.path}/${s.path}/*` : `${g.path}/${s.path}`}
              element={
                <ErrorBoundary compact>
                  <Suspense fallback={<SettingsLoading />}>
                    <s.Component />
                  </Suspense>
                </ErrorBoundary>
              }
            />
          )),
        ])}
        {/* Legacy standalone email pages */}
        <Route path="email" element={<Navigate to={abs('integrations', 'email')} replace />} />
        <Route path="email/provider" element={<Navigate to={abs('integrations', 'email')} replace />} />
        <Route path="email/templates" element={<Navigate to={`${abs('integrations', 'email')}/templates`} replace />} />
        <Route path="*" element={<Navigate to={ROUTES.admin.settings} replace />} />
      </Route>
    </Routes>
  );
};

export default AdminSettings;
