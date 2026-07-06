// Well-known UUID for the system Default Team
export const DEFAULT_TEAM_ID = '00000000-0000-0000-0000-000000000001';
export const DEFAULT_TEAM_NAME = 'Default Team';

// Valid team roles
export const TEAM_ROLES = {
  MEMBER: 'member' as const,
  ADMIN: 'admin' as const,
};

// Re-export TeamRole from types for convenience
export type { TeamRole } from '../types/team';

// System setting key
export const SYSTEM_SETTING_TEAMS_ENABLED = 'teams_enabled';
