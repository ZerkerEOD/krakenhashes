export interface JobAnalyticsFilterOptions {
  attack_modes: { value: number; label: string }[];
  hash_types: { id: number; name: string }[];
  agents: { id: number; name: string }[];
  hashlists: { id: number; name: string }[];
}

export interface JobAnalyticsSummary {
  total_jobs: number;
  completed_jobs: number;
  cancelled_jobs: number;
  failed_jobs: number;
  total_cracks: number;
  average_speed: number;
  total_keyspace_processed: number;
  average_duration_seconds: number;
}

export interface JobAnalyticsEntry {
  id: string;
  name: string;
  attack_mode: number;
  hash_type: number;
  hash_type_name: string;
  effective_keyspace: number;
  status: string;
  priority: number;
  started_at: string | null;
  completed_at: string | null;
  duration_seconds: number | null;
  task_count: number;
  total_cracks: number;
  avg_speed: number;
  max_speed: number;
  unique_agents: number;
  hashlist_id: number;
  hashlist_name: string;
  overall_progress_percent: number;
}

export interface TimelinePoint {
  timestamp: string;
  value: number;
  job_count?: number;
}

export interface TaskSegment {
  task_id: string;
  agent_id: number;
  agent_name: string;
  started_at: string | null;
  completed_at: string | null;
  average_speed: number;
  benchmark_speed: number;
  crack_count: number;
  status: string;
}

export interface BenchmarkHistoryEntry {
  id: string;
  agent_id: number;
  attack_mode: number;
  hash_type: number;
  salt_count: number | null;
  speed: number;
  success: boolean;
  error_message: string | null;
  recorded_at: string;
}

export interface PaginatedResponse<T> {
  items?: T[];
  jobs?: T[];
  pagination: {
    page: number;
    page_size: number;
    total: number;
    total_pages: number;
  };
}

export interface JobAnalyticsFilterParams {
  date_start?: string;
  date_end?: string;
  attack_mode?: number;
  hash_type?: number;
  agent_id?: number;
  hashlist_id?: number;
  status?: string[];
  min_keyspace?: number;
  max_keyspace?: number;
}
