/**
 * Diagnostics service for admin troubleshooting (GH Issue #23)
 */
import { api } from './api';
import {
  AgentDebugStatus,
  AgentDebugStatusResponse,
  AgentLogsResponse,
  DebugToggleResponse,
  LogPurgeResponse,
  SystemInfoResponse,
  ToggleDebugRequest,
  ServerDebugStatus,
  AllLogStats,
  PurgeLogsResponse,
  PostgresLogsExistResponse,
  NginxLogsExistResponse,
  NginxReloadResponse
} from '../types/diagnostics';

// Get system diagnostic info
export const getSystemInfo = async (): Promise<SystemInfoResponse> => {
  const response = await api.get<SystemInfoResponse>('/api/admin/diagnostics/system-info');
  return response.data;
};

// Get debug status for all agents
export const getAgentDebugStatuses = async (): Promise<AgentDebugStatusResponse> => {
  const response = await api.get<AgentDebugStatusResponse>('/api/admin/diagnostics/agents');
  return response.data;
};

// Get debug status for a specific agent
export const getAgentDebugStatus = async (agentId: number): Promise<AgentDebugStatus> => {
  const response = await api.get<AgentDebugStatus>(`/api/admin/diagnostics/agents/${agentId}`);
  return response.data;
};

// Toggle debug mode for a specific agent
export const toggleAgentDebug = async (agentId: number, enable: boolean): Promise<DebugToggleResponse> => {
  const response = await api.post<DebugToggleResponse>(
    `/api/admin/diagnostics/agents/${agentId}/debug`,
    { enable } as ToggleDebugRequest
  );
  return response.data;
};

// Toggle debug mode for all agents
export const toggleAllAgentsDebug = async (enable: boolean): Promise<DebugToggleResponse> => {
  const response = await api.post<DebugToggleResponse>(
    '/api/admin/diagnostics/agents/debug',
    { enable } as ToggleDebugRequest
  );
  return response.data;
};

// Request logs from a specific agent
export const requestAgentLogs = async (
  agentId: number,
  hoursBack: number = 24,
  includeAll: boolean = false
): Promise<AgentLogsResponse> => {
  const params = new URLSearchParams();
  if (hoursBack) params.append('hours_back', hoursBack.toString());
  if (includeAll) params.append('include_all', 'true');

  const response = await api.get<AgentLogsResponse>(
    `/api/admin/diagnostics/agents/${agentId}/logs?${params.toString()}`
  );
  return response.data;
};

// Purge logs for a specific agent
export const purgeAgentLogs = async (agentId: number): Promise<LogPurgeResponse> => {
  const response = await api.delete<LogPurgeResponse>(`/api/admin/diagnostics/agents/${agentId}/logs`);
  return response.data;
};

// Download full diagnostic package
export const downloadDiagnostics = async (
  includeAgentLogs: boolean = false,
  hoursBack: number = 1,
  includeNginxLogs: boolean = false,
  includePostgresLogs: boolean = false
): Promise<Blob> => {
  const params = new URLSearchParams();
  params.append('hours_back', hoursBack.toString());
  if (includeAgentLogs) {
    params.append('include_agent_logs', 'true');
  }
  if (includeNginxLogs) {
    params.append('include_nginx_logs', 'true');
  }
  if (includePostgresLogs) {
    params.append('include_postgres_logs', 'true');
  }
  const response = await api.get(`/api/admin/diagnostics/download?${params.toString()}`, {
    responseType: 'blob'
  });
  return response.data;
};

// Helper function to trigger download of the diagnostic package
export const downloadDiagnosticsFile = async (
  includeAgentLogs: boolean = false,
  hoursBack: number = 1,
  includeNginxLogs: boolean = false,
  includePostgresLogs: boolean = false
): Promise<void> => {
  const blob = await downloadDiagnostics(includeAgentLogs, hoursBack, includeNginxLogs, includePostgresLogs);
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
  link.download = `krakenhashes-diagnostics-${timestamp}.zip`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.URL.revokeObjectURL(url);
};

// Get server/backend debug status
export const getServerDebugStatus = async (): Promise<ServerDebugStatus> => {
  const response = await api.get<ServerDebugStatus>('/api/admin/diagnostics/server/debug');
  return response.data;
};

// Toggle server/backend debug mode
export const toggleServerDebug = async (enable: boolean, level?: string): Promise<ServerDebugStatus> => {
  const response = await api.post<ServerDebugStatus>(
    '/api/admin/diagnostics/server/debug',
    { enable, level } as ToggleDebugRequest
  );
  return response.data;
};

// Get server log stats
export const getLogStats = async (): Promise<AllLogStats> => {
  const response = await api.get<AllLogStats>('/api/admin/diagnostics/logs/stats');
  return response.data;
};

// Purge server logs
export const purgeServerLogs = async (directory: 'backend' | 'nginx' | 'postgres' | 'all'): Promise<PurgeLogsResponse> => {
  const response = await api.delete<PurgeLogsResponse>(`/api/admin/diagnostics/logs/${directory}`);
  return response.data;
};

// Check if PostgreSQL logs exist
export const checkPostgresLogsExist = async (): Promise<PostgresLogsExistResponse> => {
  const response = await api.get<PostgresLogsExistResponse>('/api/admin/diagnostics/postgres-logs-exist');
  return response.data;
};

// Check if Nginx logs exist
export const checkNginxLogsExist = async (): Promise<NginxLogsExistResponse> => {
  const response = await api.get<NginxLogsExistResponse>('/api/admin/diagnostics/nginx-logs-exist');
  return response.data;
};

// Reload nginx configuration (hot-reload)
export const reloadNginx = async (): Promise<NginxReloadResponse> => {
  const response = await api.post<NginxReloadResponse>('/api/admin/diagnostics/nginx/reload');
  return response.data;
};
