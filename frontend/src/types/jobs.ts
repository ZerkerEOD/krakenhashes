/**
 * Type definitions for job-related data structures
 */

// Job status enum
export type JobStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';

// Job summary for list views
export interface JobSummary {
  id: string;
  name: string;
  hashlist_id: number;
  hashlist_name: string;
  status: JobStatus;
  priority: number;
  max_agents: number;
  dispatched_percent: number;
  searched_percent: number;
  cracked_count: number;
  agent_count: number;
  total_speed: number;
  created_at: string;
  updated_at: string;
  completed_at?: string;
  created_by_username?: string;
  error_message?: string;
  // Enhanced chunking fields
  effective_keyspace?: number;
  multiplication_factor?: number;
  uses_rule_splitting?: boolean;
  base_keyspace?: number;
  total_keyspace?: number;
  processed_keyspace?: number;
  dispatched_keyspace?: number;
  overall_progress_percent: number;
}

// Pagination information
export interface PaginationInfo {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

// Job detail for detailed views
export interface JobDetail extends JobSummary {
  workflow_id?: number;
  workflow_name?: string;
  client_id?: number;
  client_name?: string;
  hashlist_count: number;
  hashlist_cracked: number;
  tasks?: JobTask[];
  agents?: JobAgent[];
}

// Job execution information
export interface JobExecution {
  id: string;
  preset_job_id: string;
  preset_job_name?: string;
  name?: string;
  hashlist_id: number;
  hashlist_name?: string;
  status: string;
  priority: number;
  max_agents: number;
  total_keyspace?: number;
  processed_keyspace: number;
  attack_mode: number;
  created_by?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  updated_at: string;
  error_message?: string;
  total_hashes?: number;
  cracked_hashes?: number;
  overall_progress_percent?: number;
}

// Job task information
export interface JobTask {
  id: string;
  job_id?: string;
  job_execution_id?: string;
  agent_id?: number;
  increment_layer_id?: string;
  status: 'pending' | 'assigned' | 'running' | 'processing' | 'completed' | 'failed' | 'cancelled' | 'reconnect_pending';
  priority?: number;
  chunk_start?: number;
  chunk_end?: number;
  keyspace_start: number;
  keyspace_end: number;
  keyspace_processed: number;
  effective_keyspace_start?: number;
  effective_keyspace_end?: number;
  effective_keyspace_processed?: number;
  benchmark_speed?: number;
  average_speed?: number;
  chunk_duration?: number;
  assigned_agent_id?: string;
  assigned_at?: string;
  started_at?: string;
  completed_at?: string;
  last_checkpoint?: string;
  error_message?: string;
  crack_count: number;
  progress_percent?: number;
  detailed_status?: string;
  created_at?: string;
  updated_at?: string;
  retry_count?: number;
}

// Job increment layer status - matches job execution statuses since layers are jobs in their own right
export type JobIncrementLayerStatus =
  | 'pending'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'cancelled';

// Job increment layer information
export interface JobIncrementLayer {
  id: string;
  job_execution_id: string;
  layer_index: number;
  mask: string;
  status: JobIncrementLayerStatus;
  base_keyspace?: number;
  effective_keyspace?: number;
  processed_keyspace: number;
  dispatched_keyspace: number;
  is_accurate_keyspace: boolean;
  overall_progress_percent: number;
  created_at: string;
  updated_at: string;
}

// Job increment layer with statistics
export interface JobIncrementLayerWithStats extends JobIncrementLayer {
  total_tasks?: number;
  completed_tasks?: number;
  running_tasks?: number;
  failed_tasks?: number;
  crack_count?: number;
}

// Job agent information
export interface JobAgent {
  id: string;
  name: string;
  status: string;
  speed: number;
  last_heartbeat: string;
}

// Job list response
export interface JobListResponse {
  jobs: JobSummary[];
  pagination: PaginationInfo;
  status_counts: Record<string, number>;
}

// Job detail response from API
export interface JobDetailsResponse {
  id: string;
  name: string;
  hashlist_id: number;
  hashlist_name: string;
  status: JobStatus;
  priority: number;
  max_agents: number;
  attack_mode: number;
  total_keyspace?: number;
  effective_keyspace?: number;
  base_keyspace?: number;
  processed_keyspace?: number;
  dispatched_keyspace?: number;
  dispatched_percent: number;
  searched_percent: number;
  cracked_count: number;
  agent_count: number;
  total_speed: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  updated_at?: string;
  error_message?: string;
  tasks: JobTask[];  // All tasks for the job
  tasks_page?: number;
  tasks_per_page?: number;
  total_tasks?: number;
  multiplication_factor?: number;
  uses_rule_splitting?: boolean;
  rule_split_count?: number;
  overall_progress_percent?: number;
  consecutive_failures?: number;
  wordlist_ids?: string[];
  wordlist_names?: string[];
  rule_ids?: string[];
  rule_names?: string[];
  mask?: string;
  binary_version_id?: number;
  chunk_size_seconds?: number;
  status_updates_enabled?: boolean;
  allow_high_priority_override?: boolean;
  additional_args?: string;
  hash_type?: string;
  increment_mode?: string;
  increment_min?: number;
  increment_max?: number;
}

// Job detail response
export interface JobDetailResponse {
  job: JobDetail;
}