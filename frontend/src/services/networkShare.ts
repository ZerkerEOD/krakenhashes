import { api } from './api';

// Mirrors backend models.NetworkShare (feature/network-share-storage).
export interface NetworkShareConfig {
  id: string;
  share_type: 'smb' | 'nfs';
  name: string;
  enabled: boolean;
  server_host: string;
  share_name: string;
  mount_options: Record<string, string>;
  storage_backend: 'local' | 'share';
  migration_state: string;
  migration_direction?: string;
  migration_started_at?: string;
  migration_finished_at?: string;
  migration_error?: string;
  last_validated_at?: string;
  last_validation_error?: string;
  created_at: string;
  updated_at: string;
}

export interface NetworkShareGetResponse {
  config: NetworkShareConfig;
  health: boolean;
  // Server's compose-mounted share path (KH_SHARE_DIR); "" when no share is
  // mounted. Read-only — this is what the server actually reads from.
  share_dir?: string;
}

export interface NetworkShareConfigInput {
  share_type: string;
  name: string;
  enabled: boolean;
  server_host: string;
  share_name: string;
  mount_options: Record<string, string>;
}

export interface NetworkShareValidationResult {
  ok: boolean;
  share_dir: string;
  mounted: boolean;
  writable: boolean;
  free_bytes: number;
  write_mbps: number;
  read_mbps: number;
  error?: string;
  validated_at: string;
}

export const getNetworkShare = async (): Promise<NetworkShareGetResponse> => {
  const response = await api.get<NetworkShareGetResponse>('/api/admin/settings/network-share');
  return response.data;
};

export const updateNetworkShare = async (
  input: NetworkShareConfigInput
): Promise<NetworkShareConfig> => {
  const response = await api.put<{ config: NetworkShareConfig }>(
    '/api/admin/settings/network-share',
    input
  );
  return response.data.config;
};

export const validateNetworkShare = async (): Promise<NetworkShareValidationResult> => {
  const response = await api.post<NetworkShareValidationResult>(
    '/api/admin/settings/network-share/validate'
  );
  return response.data;
};

export interface MigrationProgress {
  phase: string; // idle | draining | migrating | validating | completed | failed
  direction: string;
  message: string;
  files_total: number;
  files_done: number;
  bytes_total: number;
  bytes_done: number;
  current_file: string;
  throughput_mbps: number;
  started_at?: string;
  drain_deadline?: string;
  agents_busy: number;
  error?: string;
}

export const startMigration = async (
  direction: 'to_share' | 'to_local'
): Promise<MigrationProgress> => {
  const response = await api.post<MigrationProgress>(
    '/api/admin/settings/network-share/migrate',
    { direction }
  );
  return response.data;
};

export const getMigrationStatus = async (): Promise<MigrationProgress> => {
  const response = await api.get<MigrationProgress>(
    '/api/admin/settings/network-share/migration'
  );
  return response.data;
};

// cancelMigration aborts an in-progress migration (e.g. one wedged on a slow or
// hung share). The server unwinds at its next checkpoint, leaving storage on its
// original backend and resuming normal operation. Resolves with the post-cancel
// status; the backend returns 409 when nothing is running.
export const cancelMigration = async (): Promise<MigrationProgress> => {
  const response = await api.delete<MigrationProgress>(
    '/api/admin/settings/network-share/migration'
  );
  return response.data;
};

