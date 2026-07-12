import { api } from './api';

export interface JobExecutionSettings {
  default_chunk_duration: number;
  agent_hashlist_retention_hours: number;
  progress_reporting_interval: number;
  max_concurrent_jobs_per_agent: number;
  job_interruption_enabled: boolean;
  benchmark_cache_duration_hours: number;
  enable_realtime_crack_notifications: boolean;
  job_refresh_interval_seconds: number;
  max_chunk_retry_attempts: number;
  jobs_per_page_default: number;
  reconnect_grace_period_minutes: number;
  // Scheduler-v2 tuning knobs
  min_chunk_seconds: number;
  task_heartbeat_timeout_seconds: number;
  task_startup_grace_seconds: number;
  network_grace_seconds: number;
  chunk_overrun_guard_enabled: boolean;
  chunk_overrun_tolerance_percent: number;
  // Keyspace calculation settings
  keyspace_calculation_timeout_minutes: number;
  // Potfile settings
  potfile_enabled: boolean;
  potfile_max_batch_size: number;
  potfile_batch_interval: number;
  // Client potfile settings
  client_potfiles_enabled: boolean;
  remove_from_global_potfile_on_hashlist_delete_default: boolean;
  remove_from_client_potfile_on_hashlist_delete_default: boolean;
  // Benchmark history settings
  benchmark_history_retention_days: number;
  // Analytics settings
  analytics_default_date_range_months: number;
}

export interface SettingsUpdateResponse {
  success: boolean;
  message: string;
  failed_keys?: string[];
  errors?: Record<string, string>;
}

export const getJobExecutionSettings = async (): Promise<JobExecutionSettings> => {
  const response = await api.get('/api/admin/settings/job-execution');
  return response.data;
};

// User-accessible job defaults (non-admin, read-only)
export interface UserJobDefaults {
  default_chunk_duration: number;
  potfile_enabled: boolean;
  default_data_retention_months: number | null;
  analytics_default_date_range_months: number;
}

export const getJobDefaultsForUsers = async (): Promise<UserJobDefaults> => {
  const response = await api.get<UserJobDefaults>('/api/settings/job-defaults');
  return response.data;
};

export const updateJobExecutionSettings = async (settings: JobExecutionSettings): Promise<SettingsUpdateResponse> => {
  const response = await api.put('/api/admin/settings/job-execution', settings);
  return response.data;
};
