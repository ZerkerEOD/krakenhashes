import React, { lazy } from 'react';
import TuneIcon from '@mui/icons-material/Tune';
import SecurityIcon from '@mui/icons-material/Security';
import WorkIcon from '@mui/icons-material/Work';
import ComputerIcon from '@mui/icons-material/Computer';
import HubIcon from '@mui/icons-material/Hub';
import CloudIcon from '@mui/icons-material/Cloud';
import type { SettingsGroup } from '../../components/settings/navTypes';

/**
 * Admin settings information architecture. Routes, the rail and the hub are
 * all generated from this table; adding a section is one entry here plus a
 * component.
 */
export const settingsNav: SettingsGroup[] = [
  {
    id: 'general',
    path: 'general',
    labelKey: 'settingsNav.groups.general',
    icon: React.createElement(TuneIcon),
    sections: [
      {
        id: 'branding',
        path: 'branding',
        labelKey: 'settingsNav.sections.branding',
        descKey: 'settingsNav.sections.brandingDesc',
        Component: lazy(() => import('./sections/general/BrandingSection')),
        saveMode: 'autosave',
      },
      {
        id: 'data',
        path: 'data',
        labelKey: 'settingsNav.sections.data',
        descKey: 'settingsNav.sections.dataDesc',
        Component: lazy(() => import('./sections/general/DataSection')),
        saveMode: 'autosave',
      },
      {
        id: 'storage',
        path: 'storage',
        labelKey: 'settingsNav.sections.storage',
        descKey: 'settingsNav.sections.storageDesc',
        Component: lazy(() => import('./sections/general/StorageSection')),
        statusKeys: ['storage'],
        saveMode: 'apply',
      },
      {
        id: 'certificate',
        path: 'certificate',
        labelKey: 'settingsNav.sections.certificate',
        descKey: 'settingsNav.sections.certificateDesc',
        Component: lazy(() => import('./sections/general/CertificateSection')),
        statusKeys: ['certificate'],
        saveMode: 'apply',
      },
    ],
  },
  {
    id: 'security',
    path: 'security',
    labelKey: 'settingsNav.groups.security',
    icon: React.createElement(SecurityIcon),
    sections: [
      {
        id: 'authentication',
        path: 'authentication',
        labelKey: 'settingsNav.sections.authentication',
        descKey: 'settingsNav.sections.authenticationDesc',
        Component: lazy(() => import('./sections/security/AuthenticationSection')),
        saveMode: 'autosave',
      },
      {
        id: 'mfa',
        path: 'mfa',
        labelKey: 'settingsNav.sections.mfa',
        descKey: 'settingsNav.sections.mfaDesc',
        Component: lazy(() => import('./sections/security/MfaSection')),
        statusKeys: ['email'],
        saveMode: 'autosave',
      },
      {
        id: 'sso',
        path: 'sso',
        labelKey: 'settingsNav.sections.sso',
        descKey: 'settingsNav.sections.ssoDesc',
        Component: lazy(() => import('./sections/security/SsoSection')),
        statusKeys: ['sso'],
        saveMode: 'mixed',
      },
      {
        id: 'access',
        path: 'access',
        labelKey: 'settingsNav.sections.access',
        descKey: 'settingsNav.sections.accessDesc',
        Component: lazy(() => import('./sections/security/AccessSection')),
        saveMode: 'autosave',
      },
    ],
  },
  {
    id: 'jobs',
    path: 'jobs',
    labelKey: 'settingsNav.groups.jobs',
    icon: React.createElement(WorkIcon),
    sections: [
      {
        id: 'execution',
        path: 'execution',
        labelKey: 'settingsNav.sections.execution',
        descKey: 'settingsNav.sections.executionDesc',
        Component: lazy(() => import('./sections/jobs/ExecutionSection')),
        saveMode: 'autosave',
      },
      {
        id: 'scheduling',
        path: 'scheduling',
        labelKey: 'settingsNav.sections.scheduling',
        descKey: 'settingsNav.sections.schedulingDesc',
        Component: lazy(() => import('./sections/jobs/SchedulingSection')),
        saveMode: 'autosave',
      },
      {
        id: 'potfile',
        path: 'potfile',
        labelKey: 'settingsNav.sections.potfile',
        descKey: 'settingsNav.sections.potfileDesc',
        Component: lazy(() => import('./sections/jobs/PotfileSection')),
        saveMode: 'autosave',
      },
    ],
  },
  {
    id: 'agents',
    path: 'agents',
    labelKey: 'settingsNav.groups.agents',
    icon: React.createElement(ComputerIcon),
    sections: [
      {
        id: 'health',
        path: 'health',
        labelKey: 'settingsNav.sections.health',
        descKey: 'settingsNav.sections.healthDesc',
        Component: lazy(() => import('./sections/agents/HealthSection')),
        saveMode: 'autosave',
      },
      {
        id: 'downloads',
        path: 'downloads',
        labelKey: 'settingsNav.sections.downloads',
        descKey: 'settingsNav.sections.downloadsDesc',
        Component: lazy(() => import('./sections/agents/DownloadsSection')),
        saveMode: 'autosave',
      },
      {
        id: 'updates',
        path: 'updates',
        labelKey: 'settingsNav.sections.updates',
        descKey: 'settingsNav.sections.updatesDesc',
        Component: lazy(() => import('./sections/agents/UpdatesSection')),
        saveMode: 'autosave',
      },
      {
        id: 'monitoring',
        path: 'monitoring',
        labelKey: 'settingsNav.sections.monitoring',
        descKey: 'settingsNav.sections.monitoringDesc',
        Component: lazy(() => import('./sections/agents/MonitoringSection')),
        saveMode: 'autosave',
      },
    ],
  },
  {
    id: 'integrations',
    path: 'integrations',
    labelKey: 'settingsNav.groups.integrations',
    icon: React.createElement(HubIcon),
    sections: [
      {
        id: 'email',
        path: 'email',
        labelKey: 'settingsNav.sections.email',
        descKey: 'settingsNav.sections.emailDesc',
        Component: lazy(() => import('./sections/integrations/EmailSection')),
        statusKeys: ['email'],
        saveMode: 'apply',
        nested: true,
      },
      {
        id: 'webhooks',
        path: 'webhooks',
        labelKey: 'settingsNav.sections.webhooks',
        descKey: 'settingsNav.sections.webhooksDesc',
        Component: lazy(() => import('./sections/integrations/WebhooksSection')),
        statusKeys: ['webhook'],
        saveMode: 'autosave',
      },
      {
        id: 'notifications',
        path: 'notifications',
        labelKey: 'settingsNav.sections.notifications',
        descKey: 'settingsNav.sections.notificationsDesc',
        Component: lazy(() => import('./sections/integrations/NotificationsSection')),
        saveMode: 'autosave',
      },
    ],
  },
  {
    id: 'cloud',
    path: 'cloud',
    labelKey: 'settingsNav.groups.cloud',
    icon: React.createElement(CloudIcon),
    sections: [
      {
        id: 'providers',
        path: 'providers',
        labelKey: 'settingsNav.sections.providers',
        descKey: 'settingsNav.sections.providersDesc',
        Component: lazy(() => import('../../components/admin/cloud/CloudProviderSettings')),
        statusKeys: ['cloud'],
        saveMode: 'mixed',
      },
      {
        id: 'budgets',
        path: 'budgets',
        labelKey: 'settingsNav.sections.budgets',
        descKey: 'settingsNav.sections.budgetsDesc',
        Component: lazy(() => import('../../components/admin/cloud/CloudClientBudgets')),
        saveMode: 'mixed',
      },
      {
        id: 'policy',
        path: 'policy',
        labelKey: 'settingsNav.sections.policy',
        descKey: 'settingsNav.sections.policyDesc',
        Component: lazy(() => import('./sections/cloud/PolicySection')),
        saveMode: 'apply',
      },
      {
        id: 'rules',
        path: 'rules',
        labelKey: 'settingsNav.sections.rules',
        descKey: 'settingsNav.sections.rulesDesc',
        Component: lazy(() => import('./sections/cloud/RulesSection')),
        saveMode: 'autosave',
      },
      {
        id: 'limits',
        path: 'limits',
        labelKey: 'settingsNav.sections.limits',
        descKey: 'settingsNav.sections.limitsDesc',
        Component: lazy(() => import('./sections/cloud/LimitsSection')),
        statusKeys: ['cloud'],
        saveMode: 'autosave',
      },
    ],
  },
];

/** Old horizontal-tab index (localStorage `adminSettingsTab`) → new path. */
export const LEGACY_TAB_PATHS: Record<number, string> = {
  0: 'integrations/email',
  1: 'security/authentication',
  2: 'security/sso',
  3: '/admin/binaries',
  4: 'general/data',
  5: '/admin/hash-types',
  6: 'jobs/execution',
  7: 'agents/monitoring',
  8: 'agents/downloads',
  9: 'integrations/webhooks',
  10: 'cloud/providers',
  11: 'general/certificate',
};

export default settingsNav;
