/**
 * Query key factory. Keys are arrays so `invalidateQueries({ queryKey: qk.jobs.all })`
 * clears every job list/detail at once while `qk.jobs.detail(id)` stays precise.
 */
type Params = Record<string, unknown> | undefined;

export const qk = {
  dashboard: {
    all: ['dashboard'] as const,
    stats: (view: string, teamId?: string | null) => ['dashboard', 'stats', view, teamId ?? null] as const,
    recentCracks: (view: string, teamId?: string | null, limit?: number) =>
      ['dashboard', 'recent-cracks', view, teamId ?? null, limit ?? 20] as const,
    agentHealth: (view: string, teamId?: string | null) => ['dashboard', 'agent-health', view, teamId ?? null] as const,
    jobs: (view: string, p?: Params) => ['dashboard', 'jobs', view, p ?? {}] as const,
    hashlists: (view: string, p?: Params) => ['dashboard', 'hashlists', view, p ?? {}] as const,
    attention: (view: string, teamId?: string | null) => ['dashboard', 'attention', view, teamId ?? null] as const,
    crackTrend: (view: string, teamId?: string | null, days?: number) =>
      ['dashboard', 'crack-trend', view, teamId ?? null, days ?? 14] as const,
  },
  jobs: {
    all: ['jobs'] as const,
    list: (p?: Params) => ['jobs', 'list', p ?? {}] as const,
    detail: (id: string) => ['jobs', 'detail', id] as const,
    statusCounts: (p?: Params) => ['jobs', 'status-counts', p ?? {}] as const,
  },
  agents: {
    all: ['agents'] as const,
    list: (p?: Params) => ['agents', 'list', p ?? {}] as const,
    detail: (id: number | string) => ['agents', 'detail', String(id)] as const,
    activity: (id: number | string) => ['agents', 'activity', String(id)] as const,
    metrics: (id: number | string, range?: string) => ['agents', 'metrics', String(id), range ?? ''] as const,
    user: () => ['agents', 'user'] as const,
  },
  hashlists: {
    all: ['hashlists'] as const,
    list: (p?: Params) => ['hashlists', 'list', p ?? {}] as const,
    detail: (id: number | string) => ['hashlists', 'detail', String(id)] as const,
    hashes: (id: number | string, p?: Params) => ['hashlists', 'hashes', String(id), p ?? {}] as const,
  },
  clients: {
    all: ['clients'] as const,
    list: (p?: Params) => ['clients', 'list', p ?? {}] as const,
    overview: (id: string) => ['clients', 'overview', id] as const,
  },
  teams: {
    all: ['teams'] as const,
    list: () => ['teams', 'list'] as const,
    detail: (id: string) => ['teams', 'detail', id] as const,
  },
  notifications: {
    all: ['notifications'] as const,
    recent: (limit?: number) => ['notifications', 'recent', limit ?? 20] as const,
  },
  admin: {
    settingsStatus: () => ['admin', 'settings', 'status'] as const,
    systemSettings: () => ['admin', 'settings', 'system'] as const,
    group: (name: string) => ['admin', 'settings', 'group', name] as const,
  },
} as const;

export default qk;
