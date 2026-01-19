/**
 * Diagnostics types for admin troubleshooting (GH Issue #23)
 */

// Agent debug status from backend
export interface AgentDebugStatus {
  agent_id: number;
  enabled: boolean;
  level: string;
  file_logging_enabled: boolean;
  log_file_path?: string;
  log_file_exists: boolean;
  log_file_size: number;
  log_file_modified: number;
  buffer_count: number;
  buffer_capacity: number;
  last_updated: string;
}

// Response for all agent debug statuses
export interface AgentDebugStatusResponse {
  agents: AgentDebugStatus[];
  count: number;
}

// Log entry from agent
export interface LogEntry {
  timestamp: number; // Unix milliseconds
  level: string;
  message: string;
  file?: string;
  line?: number;
  function?: string;
}

// Response for agent logs request
export interface AgentLogsResponse {
  request_id: string;
  agent_id: number;
  entries: LogEntry[];
  file_content?: string;
  total_count: number;
  truncated: boolean;
  error?: string;
}

// Response for debug toggle
export interface DebugToggleResponse {
  status: string;
  message: string;
  enable: boolean;
  agent_id?: number;
  succeeded?: number;
  failed?: number;
}

// Response for log purge
export interface LogPurgeResponse {
  request_id: string;
  success: boolean;
  message?: string;
}

// System info response
export interface SystemInfoResponse {
  system_info: {
    go_version?: string;
    go_os?: string;
    go_arch?: string;
    num_cpu?: number;
    num_goroutine?: number;
    memory?: {
      alloc_mb: number;
      total_alloc_mb: number;
      sys_mb: number;
      num_gc: number;
      heap_objects: number;
      heap_alloc_mb: number;
    };
    hostname?: string;
    working_directory?: string;
    environment?: Record<string, string>;
    database?: {
      database_version?: string;
      database_size?: string;
      table_counts?: Record<string, number>;
      connection_stats?: {
        open_connections: number;
        in_use: number;
        idle: number;
        max_open: number;
        wait_count: number;
        wait_duration_ms: number;
      };
    };
    connected_agents?: number;
    collected_at?: string;
  };
  generated_at: string;
  version: string;
  errors: string[];
  agent_statuses: number;
}

// Request types
export interface ToggleDebugRequest {
  enable: boolean;
  level?: string;
}

// Server debug status
export interface ServerDebugStatus {
  enabled: boolean;
  level: string;
  success?: boolean;
}

// Log directory stats
export interface LogDirStats {
  files: number;
  size: number;
}

// All server log stats
export interface AllLogStats {
  backend: LogDirStats;
  nginx: LogDirStats;
  postgres: LogDirStats;
}

// Purge logs response
export interface PurgeLogsResponse {
  success: boolean;
  message: string;
  directory: string;
}

// Postgres logs exist response
export interface PostgresLogsExistResponse {
  exists: boolean;
}

// Nginx logs exist response
export interface NginxLogsExistResponse {
  exists: boolean;
}

// Nginx reload response
export interface NginxReloadResponse {
  success: boolean;
  message: string;
}
