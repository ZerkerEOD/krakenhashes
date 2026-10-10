import { lazy } from 'react';
import PersonIcon from '@mui/icons-material/Person';
import type { SettingsGroup } from '../../components/settings/navTypes';

/**
 * User settings rail. One flat group (empty path) so the historical URLs
 * `/settings/profile` and `/settings/charsets` keep working.
 */
export const USER_SETTINGS_NAV: SettingsGroup[] = [
  {
    id: 'account',
    path: '',
    labelKey: 'nav.groupAccount',
    icon: <PersonIcon />,
    sections: [
      { id: 'profile', path: 'profile', labelKey: 'nav.profile', descKey: 'nav.profileDesc', saveMode: 'mixed', Component: lazy(() => import('./sections/ProfileSection')) },
      { id: 'security', path: 'security', labelKey: 'nav.security', descKey: 'nav.securityDesc', saveMode: 'mixed', Component: lazy(() => import('./sections/SecuritySection')) },
      { id: 'notifications', path: 'notifications', labelKey: 'nav.notifications', descKey: 'nav.notificationsDesc', saveMode: 'autosave', Component: lazy(() => import('./sections/NotificationsSection')) },
      { id: 'api', path: 'api', labelKey: 'nav.api', descKey: 'nav.apiDesc', saveMode: 'mixed', Component: lazy(() => import('./sections/ApiSection')) },
      { id: 'charsets', path: 'charsets', labelKey: 'nav.charsets', descKey: 'nav.charsetsDesc', saveMode: 'mixed', Component: lazy(() => import('./sections/CharsetsSection')) },
    ],
  },
];
