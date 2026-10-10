import { api } from './api';

export interface DashboardStats {
  jobs: { running: number; pending: number; paused: number; processing: number; completed_24h: number; failed_24h: number };
  agents: { online: number; total: number; error: number; updating: number; disabled: number; offline: number };
  cracks: { last_24h: number; last_7d: number };
  hash_rate: number;
  hashlists: { active: number; total_hashes: number; cracked_hashes: number };
  generated_at: string;
}

export interface RecentCrack {
  hash_id: string;
  hash: string;
  plaintext: string;
  username?: string;
  domain?: string;
  cracked_at: string;
  hashlist_id?: number;
  hashlist_name?: string;
  job_id?: string;
  job_name?: string;
  agent_id?: number;
  agent_name?: string;
}

export interface AgentHealth {
  id: number;
  name: string;
  status: string;
  is_enabled: boolean;
  last_heartbeat?: string;
  version: string;
  update_pending: boolean;
  update_error?: string;
  last_error?: string;
  owner_id?: string;
  owner_username?: string;
  current_job_id?: string;
  current_job_name?: string;
  current_job_progress?: number;
  metrics?: { gpu_utilization: number; gpu_temp: number; hash_rate: number; power_usage: number; timestamp: string };
  /** Server-computed: error, offline, update_failed, stale_heartbeat, gpu_hot, ... */
  warnings: string[];
}

/**
 * Dashboard view: `mine` (my jobs, hashlists, cracks, agents), `teams` (my
 * teams, or the app-bar team) or `all` (everything; admin-only when teams are
 * enabled). The server enforces who may use which view.
 */
export type DashboardView = 'mine' | 'teams' | 'all';

/** Query params for a view: the app-bar team narrows mine/teams, never all. */
export const viewParams = (view: DashboardView, teamId?: string | null): Record<string, string> => {
  const params: Record<string, string> = { scope: view };
  if (teamId && view !== 'all') params.team_id = teamId;
  return params;
};

export const getDashboardStats = async (view: DashboardView, teamId?: string | null): Promise<DashboardStats> =>
  (await api.get<DashboardStats>('/api/dashboard/stats', { params: viewParams(view, teamId) })).data;

export const getRecentCracks = async (view: DashboardView, teamId?: string | null, limit = 20): Promise<RecentCrack[]> =>
  (await api.get<{ cracks: RecentCrack[] }>('/api/dashboard/recent-cracks', { params: { ...viewParams(view, teamId), limit } })).data
    .cracks ?? [];

export const getAgentHealth = async (view: DashboardView, teamId?: string | null): Promise<AgentHealth[]> =>
  (await api.get<{ agents: AgentHealth[] }>('/api/dashboard/agent-health', { params: viewParams(view, teamId) })).data.agents ?? [];

export type AttentionKind =
  | 'job_failed'
  | 'job_blocked'
  | 'hashlist_error'
  | 'hashlist_awaiting_decision'
  | 'hashlist_stuck'
  | 'agent_error'
  | 'agent_update_failed';

export interface AttentionItem {
  kind: AttentionKind;
  severity: 'error' | 'warning';
  entity_type: 'job' | 'hashlist' | 'agent';
  entity_id: string;
  name: string;
  detail?: string;
  at: string;
}

export interface CrackTrend {
  days: { date: string; count: number }[];
  top_hashlists: { id: number; name: string; count: number }[];
}

export const getAttention = async (view: DashboardView, teamId?: string | null): Promise<AttentionItem[]> =>
  (await api.get<{ items: AttentionItem[] }>('/api/dashboard/attention', { params: viewParams(view, teamId) })).data.items ?? [];

export const getCrackTrend = async (view: DashboardView, teamId?: string | null, days = 14): Promise<CrackTrend> =>
  (await api.get<CrackTrend>('/api/dashboard/crack-trend', { params: { ...viewParams(view, teamId), days } })).data;
