// Team types for multi-team dynamics
// Note: Field names use snake_case to match Go backend JSON tags (existing codebase convention)

export interface Team {
  id: string;
  name: string;
  description: string;
  user_role?: TeamRole;
  member_count?: number;
  client_count?: number;
  hashlist_count?: number;
  created_at: string;
  updated_at: string;
}

export type TeamRole = 'member' | 'admin';

export interface TeamMember {
  user_id: string;           // snake_case
  username: string;
  email: string;
  role: TeamRole;
  joined_at: string;         // snake_case
}

export interface TeamWithClients extends Team {
  clients: TeamClient[];
}

export interface TeamClient {
  id: string;
  name: string;
  description?: string;
  assigned_at: string;       // snake_case
}

// Request/Response types
export interface CreateTeamRequest {
  name: string;
  description?: string;
}

export interface UpdateTeamRequest {
  name: string;
  description?: string;
}

export interface AddMemberRequest {
  user_id: string;           // snake_case to match backend
  role: TeamRole;
}

export interface UpdateMemberRoleRequest {
  role: TeamRole;
}

export interface UserSearchResult {
  id: string;
  username: string;
  email: string;
}

export interface TeamsEnabledResponse {
  teams_enabled: boolean;
}
