import React from 'react';
import DashboardIcon from '@mui/icons-material/Dashboard';
import WorkIcon from '@mui/icons-material/Work';
import ComputerIcon from '@mui/icons-material/Computer';
import ListAltIcon from '@mui/icons-material/ListAlt';
import LockIcon from '@mui/icons-material/Lock';
import DescriptionIcon from '@mui/icons-material/Description';
import RuleIcon from '@mui/icons-material/Rule';
import AnalyticsIcon from '@mui/icons-material/Analytics';
import PeopleIcon from '@mui/icons-material/People';
import GroupsIcon from '@mui/icons-material/Groups';
import SettingsIcon from '@mui/icons-material/Settings';
import MemoryIcon from '@mui/icons-material/Memory';
import TagIcon from '@mui/icons-material/Tag';
import PlaylistAddCheckIcon from '@mui/icons-material/PlaylistAddCheck';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import TextFieldsIcon from '@mui/icons-material/TextFields';
import SupervisorAccountIcon from '@mui/icons-material/SupervisorAccount';
import ConfirmationNumberIcon from '@mui/icons-material/ConfirmationNumber';
import CloudIcon from '@mui/icons-material/Cloud';
import BugReportIcon from '@mui/icons-material/BugReport';
import HistoryIcon from '@mui/icons-material/History';
import TrendingUpIcon from '@mui/icons-material/TrendingUp';
import { ROUTES } from '../../constants/routes';

export interface NavContext {
  isAdmin: boolean;
  teamsEnabled: boolean;
}

export interface NavItem {
  id: string;
  /** Key in the `navigation` namespace. */
  labelKey: string;
  icon: React.ReactNode;
  path: string;
  /** Extra path prefixes that should also highlight this item. */
  alsoMatches?: string[];
  visible?: (ctx: NavContext) => boolean;
  /** Badge source rendered on the icon. */
  badge?: 'settingsAttention';
}

export interface NavGroup {
  id: string;
  /** Header label; groups without one render flat. */
  labelKey?: string;
  adminOnly?: boolean;
  /** Headed groups collapse; the active group is always expanded. */
  collapsible?: boolean;
  items: NavItem[];
}

/**
 * The sidebar, as data. Order here is display order. Visibility is decided
 * per item/group from the auth + team context so one config serves every role.
 */
export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'primary',
    items: [
      { id: 'dashboard', labelKey: 'menu.dashboard', icon: <DashboardIcon />, path: ROUTES.dashboard },
      { id: 'jobs', labelKey: 'menu.jobs', icon: <WorkIcon />, path: ROUTES.jobs },
      { id: 'agents', labelKey: 'menu.agents', icon: <ComputerIcon />, path: ROUTES.agents },
      { id: 'hashlists', labelKey: 'menu.hashlists', icon: <ListAltIcon />, path: ROUTES.hashlists },
      { id: 'pot', labelKey: 'menu.crackedHashes', icon: <LockIcon />, path: ROUTES.pot },
      { id: 'wordlists', labelKey: 'menu.wordlists', icon: <DescriptionIcon />, path: ROUTES.wordlists },
      { id: 'rules', labelKey: 'menu.rules', icon: <RuleIcon />, path: ROUTES.rules },
      { id: 'analytics', labelKey: 'menu.analytics', icon: <AnalyticsIcon />, path: ROUTES.analytics },
      // Non-admins reach clients/teams here; admins get them under People & Access.
      { id: 'clients', labelKey: 'menu.clientManagement', icon: <PeopleIcon />, path: ROUTES.clients, visible: (c) => !c.isAdmin },
      { id: 'teams', labelKey: 'menu.teams', icon: <GroupsIcon />, path: ROUTES.teams, visible: (c) => !c.isAdmin && c.teamsEnabled },
    ],
  },
  {
    id: 'admin-settings',
    adminOnly: true,
    items: [
      {
        id: 'settings',
        labelKey: 'admin.settings',
        icon: <SettingsIcon />,
        path: ROUTES.admin.settings,
        badge: 'settingsAttention',
      },
    ],
  },
  {
    id: 'resources',
    labelKey: 'groups.resources',
    adminOnly: true,
    collapsible: true,
    items: [
      { id: 'binaries', labelKey: 'admin.binaries', icon: <MemoryIcon />, path: ROUTES.admin.binaries },
      { id: 'hash-types', labelKey: 'admin.hashTypes', icon: <TagIcon />, path: ROUTES.admin.hashTypes },
      { id: 'preset-jobs', labelKey: 'admin.presetJobs', icon: <PlaylistAddCheckIcon />, path: ROUTES.admin.presetJobs },
      { id: 'workflows', labelKey: 'admin.jobWorkflows', icon: <AccountTreeIcon />, path: ROUTES.admin.workflows },
      { id: 'charsets', labelKey: 'admin.customCharsets', icon: <TextFieldsIcon />, path: ROUTES.admin.customCharsets },
    ],
  },
  {
    id: 'people',
    labelKey: 'groups.peopleAccess',
    adminOnly: true,
    collapsible: true,
    items: [
      { id: 'users', labelKey: 'admin.userManagement', icon: <SupervisorAccountIcon />, path: ROUTES.admin.users },
      { id: 'admin-teams', labelKey: 'admin.teams', icon: <GroupsIcon />, path: ROUTES.teams, visible: (c) => c.teamsEnabled },
      { id: 'admin-clients', labelKey: 'admin.clients', icon: <PeopleIcon />, path: ROUTES.clients },
      { id: 'vouchers', labelKey: 'admin.vouchers', icon: <ConfirmationNumberIcon />, path: ROUTES.admin.vouchers },
    ],
  },
  {
    id: 'operations',
    labelKey: 'groups.operations',
    adminOnly: true,
    collapsible: true,
    items: [
      { id: 'cloud-fleet', labelKey: 'admin.cloudFleet', icon: <CloudIcon />, path: ROUTES.admin.cloudFleet, alsoMatches: ['/admin/cloud'] },
      { id: 'diagnostics', labelKey: 'admin.diagnostics', icon: <BugReportIcon />, path: ROUTES.admin.diagnostics },
      { id: 'audit-log', labelKey: 'admin.auditLog', icon: <HistoryIcon />, path: ROUTES.admin.auditLog },
      { id: 'job-analytics', labelKey: 'admin.jobAnalytics', icon: <TrendingUpIcon />, path: ROUTES.admin.jobAnalytics },
    ],
  },
];

export const isItemActive = (item: NavItem, pathname: string): boolean => {
  const prefixes = [item.path, ...(item.alsoMatches ?? [])];
  return prefixes.some((p) => pathname === p || pathname.startsWith(`${p}/`));
};

export const visibleGroups = (ctx: NavContext): NavGroup[] =>
  NAV_GROUPS.filter((g) => !g.adminOnly || ctx.isAdmin)
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.visible || i.visible(ctx)) }))
    .filter((g) => g.items.length > 0);
