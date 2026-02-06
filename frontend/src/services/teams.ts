import { api } from './api';
import {
  Team,
  TeamMember,
  CreateTeamRequest,
  UpdateTeamRequest,
  AddMemberRequest,
  UpdateMemberRoleRequest,
  UserSearchResult,
  TeamsEnabledResponse,
} from '../types/team';
import { Client } from '../types/client';

const TEAMS_BASE = '/api/teams';
const SETTINGS_BASE = '/api/settings';
const ADMIN_TEAMS_BASE = '/api/admin/teams';
const ADMIN_SETTINGS_BASE = '/api/admin/settings';

// =============================================================================
// User Team Operations
// =============================================================================

export const teamsService = {
  // Get user's teams
  async listUserTeams(): Promise<Team[]> {
    const response = await api.get<Team[]>(TEAMS_BASE);
    return response.data;
  },

  // Create a new team
  async createTeam(data: CreateTeamRequest): Promise<Team> {
    const response = await api.post<Team>(TEAMS_BASE, data);
    return response.data;
  },

  // Get a specific team
  async getTeam(teamId: string): Promise<Team> {
    const response = await api.get<Team>(`${TEAMS_BASE}/${teamId}`);
    return response.data;
  },

  // Update a team
  async updateTeam(teamId: string, data: UpdateTeamRequest): Promise<Team> {
    const response = await api.put<Team>(`${TEAMS_BASE}/${teamId}`, data);
    return response.data;
  },

  // =============================================================================
  // Member Management
  // =============================================================================

  // Get team members
  async getTeamMembers(teamId: string): Promise<TeamMember[]> {
    const response = await api.get<TeamMember[]>(`${TEAMS_BASE}/${teamId}/members`);
    return response.data;
  },

  // Add member to team
  async addMember(teamId: string, data: AddMemberRequest): Promise<void> {
    await api.post(`${TEAMS_BASE}/${teamId}/members`, data);
  },

  // Remove member from team
  async removeMember(teamId: string, userId: string): Promise<void> {
    await api.delete(`${TEAMS_BASE}/${teamId}/members/${userId}`);
  },

  // Update member role
  async updateMemberRole(teamId: string, userId: string, data: UpdateMemberRoleRequest): Promise<void> {
    await api.put(`${TEAMS_BASE}/${teamId}/members/${userId}`, data);
  },

  // =============================================================================
  // Client Assignment
  // =============================================================================

  // Get team's clients
  async getTeamClients(teamId: string): Promise<Client[]> {
    const response = await api.get<Client[]>(`${TEAMS_BASE}/${teamId}/clients`);
    return response.data;
  },

  // Assign client to team
  async assignClient(teamId: string, clientId: string): Promise<void> {
    await api.post(`${TEAMS_BASE}/${teamId}/clients/${clientId}`);
  },

  // Remove client from team
  async removeClient(teamId: string, clientId: string): Promise<void> {
    await api.delete(`${TEAMS_BASE}/${teamId}/clients/${clientId}`);
  },

  // =============================================================================
  // Settings (accessible to all authenticated users)
  // =============================================================================

  // Check if teams mode is enabled (non-admin endpoint)
  // NOTE: Step 5 must add GET /api/settings/teams_enabled as a non-admin endpoint.
  // This cannot use the admin endpoint because non-admin users would get 403.
  async getTeamsEnabled(): Promise<boolean> {
    const response = await api.get<TeamsEnabledResponse>(`${SETTINGS_BASE}/teams_enabled`);
    return response.data.teams_enabled;
  },

  // =============================================================================
  // User Search
  // =============================================================================

  // Search users to add to team
  async searchUsers(teamId: string, query: string): Promise<UserSearchResult[]> {
    const response = await api.get<UserSearchResult[]>('/api/users/search', {
      params: { q: query, team_id: teamId },
    });
    return response.data;
  },
};

// =============================================================================
// Admin Operations
// =============================================================================

export const adminTeamsService = {
  // List all teams
  async listAllTeams(): Promise<Team[]> {
    const response = await api.get<Team[]>(ADMIN_TEAMS_BASE);
    return response.data;
  },

  // Create team (admin)
  async createTeam(data: CreateTeamRequest): Promise<Team> {
    const response = await api.post<Team>(ADMIN_TEAMS_BASE, data);
    return response.data;
  },

  // Delete team
  async deleteTeam(teamId: string): Promise<void> {
    await api.delete(`${ADMIN_TEAMS_BASE}/${teamId}`);
  },

  // Get teams_enabled setting
  async getTeamsEnabled(): Promise<boolean> {
    const response = await api.get<TeamsEnabledResponse>(`${ADMIN_SETTINGS_BASE}/teams_enabled`);
    return response.data.teams_enabled;
  },

  // Set teams_enabled setting
  async setTeamsEnabled(enabled: boolean): Promise<void> {
    await api.put(`${ADMIN_SETTINGS_BASE}/teams_enabled`, { enabled });
  },
};

export default teamsService;
