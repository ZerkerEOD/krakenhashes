/**
 * Single source of truth for application paths. Every link to an entity goes
 * through `entityRoute` so a route change touches one file.
 */
export type EntityType =
  | 'job'
  | 'agent'
  | 'hashlist'
  | 'client'
  | 'team'
  | 'user'
  | 'preset_job'
  | 'workflow'
  | 'job_workflow'
  | 'task'
  | 'cloud_instance'
  | 'wordlist'
  | 'rule'
  | 'binary'
  | 'security'
  | 'webhook'
  | 'pot'
  | 'pot_hashlist'
  | 'pot_client'
  | 'pot_job'
  | 'notification';

type Id = string | number;

export const ROUTES = {
  login: '/login',
  dashboard: '/dashboard',
  jobs: '/jobs',
  job: (id: Id) => `/jobs/${id}`,
  agents: '/agents',
  agent: (id: Id) => `/agents/${id}`,
  hashlists: '/hashlists',
  hashlist: (id: Id) => `/hashlists/${id}`,
  wordlists: '/wordlists',
  rules: '/rules',
  clients: '/clients',
  client: (id: Id) => `/clients/${id}`,
  analytics: '/analytics',
  pot: '/pot',
  potHashlist: (id: Id) => `/pot/hashlist/${id}`,
  potClient: (id: Id) => `/pot/client/${id}`,
  potJob: (id: Id) => `/pot/job/${id}`,
  teams: '/teams',
  team: (id: Id) => `/teams/${id}`,
  notifications: '/notifications',
  about: '/about',
  settings: '/settings',
  settingsProfile: '/settings/profile',
  settingsSecurity: '/settings/security',
  settingsNotifications: '/settings/notifications',
  settingsApi: '/settings/api',
  settingsCharsets: '/settings/charsets',
  admin: {
    root: '/admin',
    settings: '/admin/settings',
    settingsSection: (group: string, section?: string) =>
      section ? `/admin/settings/${group}/${section}` : `/admin/settings/${group}`,
    users: '/admin/users',
    user: (id: Id) => `/admin/users/${id}`,
    presetJobs: '/admin/preset-jobs',
    presetJobNew: '/admin/preset-jobs/new',
    presetJobEdit: (id: Id) => `/admin/preset-jobs/${id}/edit`,
    workflows: '/admin/job-workflows',
    workflowNew: '/admin/job-workflows/new',
    workflowEdit: (id: Id) => `/admin/job-workflows/${id}/edit`,
    customCharsets: '/admin/custom-charsets',
    binaries: '/admin/binaries',
    hashTypes: '/admin/hash-types',
    vouchers: '/admin/vouchers',
    diagnostics: '/admin/diagnostics',
    auditLog: '/admin/audit-log',
    jobAnalytics: '/admin/job-analytics',
    cloudFleet: '/admin/cloud/fleet',
  },
} as const;

export interface EntityRouteContext {
  /** For `task`: the job that owns the task. */
  parentJobId?: Id;
}

/**
 * Path for an entity reference. Types without a detail page resolve to the
 * closest useful place (list page, parent, or settings); unknown types fall
 * back to the notification centre.
 */
export const entityRoute = (type: EntityType | string, id?: Id | null, ctx?: EntityRouteContext): string => {
  const has = id !== undefined && id !== null && id !== '';
  switch (type) {
    case 'job':
      return has ? ROUTES.job(id as Id) : ROUTES.jobs;
    case 'agent':
      return has ? ROUTES.agent(id as Id) : ROUTES.agents;
    case 'hashlist':
      return has ? ROUTES.hashlist(id as Id) : ROUTES.hashlists;
    case 'client':
      return has ? ROUTES.client(id as Id) : ROUTES.clients;
    case 'team':
      return has ? ROUTES.team(id as Id) : ROUTES.teams;
    case 'user':
      return has ? ROUTES.admin.user(id as Id) : ROUTES.admin.users;
    case 'preset_job':
      return has ? ROUTES.admin.presetJobEdit(id as Id) : ROUTES.admin.presetJobs;
    case 'workflow':
    case 'job_workflow':
      return has ? ROUTES.admin.workflowEdit(id as Id) : ROUTES.admin.workflows;
    case 'task':
      return ctx?.parentJobId !== undefined ? ROUTES.job(ctx.parentJobId) : ROUTES.jobs;
    case 'cloud_instance':
      return ROUTES.admin.cloudFleet;
    case 'wordlist':
      return ROUTES.wordlists;
    case 'rule':
      return ROUTES.rules;
    case 'binary':
      return ROUTES.admin.binaries;
    case 'security':
      return ROUTES.settingsSecurity;
    case 'webhook':
      return ROUTES.settingsNotifications;
    case 'pot':
      return ROUTES.pot;
    case 'pot_hashlist':
      return has ? ROUTES.potHashlist(id as Id) : ROUTES.pot;
    case 'pot_client':
      return has ? ROUTES.potClient(id as Id) : ROUTES.pot;
    case 'pot_job':
      return has ? ROUTES.potJob(id as Id) : ROUTES.pot;
    default:
      return ROUTES.notifications;
  }
};

export const isAdminRoute = (path: string): boolean => path.startsWith('/admin/') || path === '/admin';

export default ROUTES;
