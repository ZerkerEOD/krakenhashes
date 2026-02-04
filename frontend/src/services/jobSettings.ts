import { api } from './api';

export interface JobExecutionSettings {
  default_chunk_duration: number;
  chunk_fluctuation_percentage: number;
  agent_hashlist_retention_hours: number;
  progress_reporting_interval: number;
  max_concurrent_jobs_per_agent: number;
  job_interruption_enabled: boolean;
  benchmark_cache_duration_hours: number;
  speedtest_timeout_seconds: number;
  enable_realtime_crack_notifications: boolean;
  job_refresh_interval_seconds: number;
  max_chunk_retry_attempts: number;
  jobs_per_page_default: number;
  reconnect_grace_period_minutes: number;
  // Rule splitting settings
  rule_split_enabled: boolean;
  rule_split_threshold: number;
  rule_split_min_rules: number;
  rule_split_max_chunks: number;
  rule_chunk_temp_dir: string;
  // Potfile settings
  potfile_enabled: boolean;
  // Client potfile settings
  client_potfiles_enabled: boolean;
  remove_from_global_potfile_on_hashlist_delete_default: boolean;
  remove_from_client_potfile_on_hashlist_delete_default: boolean;
}

export const getJobExecutionSettings = async (): Promise<JobExecutionSettings> => {
  const response = await api.get('/api/admin/settings/job-execution');
  return response.data;
};

export const updateJobExecutionSettings = async (settings: JobExecutionSettings): Promise<void> => {
  await api.put('/api/admin/settings/job-execution', settings);
};