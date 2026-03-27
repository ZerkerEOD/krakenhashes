import { api } from './api';
import {
  JobAnalyticsFilterOptions,
  JobAnalyticsSummary,
  JobAnalyticsEntry,
  TimelinePoint,
  TaskSegment,
  BenchmarkHistoryEntry,
  SuccessRateEntry,
  PaginatedResponse,
  JobAnalyticsFilterParams,
} from '../types/jobAnalytics';

const buildParams = (filter: JobAnalyticsFilterParams, extra?: Record<string, string | number | undefined>) => {
  const params = new URLSearchParams();
  if (filter.date_start) params.append('date_start', filter.date_start);
  if (filter.date_end) params.append('date_end', filter.date_end);
  if (filter.attack_mode !== undefined) params.append('attack_mode', String(filter.attack_mode));
  if (filter.hash_type !== undefined) params.append('hash_type', String(filter.hash_type));
  if (filter.agent_id !== undefined) params.append('agent_id', String(filter.agent_id));
  if (filter.hashlist_id !== undefined) params.append('hashlist_id', String(filter.hashlist_id));
  if (filter.min_keyspace !== undefined) params.append('min_keyspace', String(filter.min_keyspace));
  if (filter.max_keyspace !== undefined) params.append('max_keyspace', String(filter.max_keyspace));
  if (filter.status) filter.status.forEach(s => params.append('status', s));
  if (extra) {
    Object.entries(extra).forEach(([k, v]) => {
      if (v !== undefined) params.append(k, String(v));
    });
  }
  return params.toString();
};

export const jobAnalyticsService = {
  getFilters: async (): Promise<JobAnalyticsFilterOptions> => {
    const response = await api.get('/api/admin/job-analytics/filters');
    return response.data;
  },

  getSummary: async (filter: JobAnalyticsFilterParams): Promise<JobAnalyticsSummary> => {
    const qs = buildParams(filter);
    const response = await api.get(`/api/admin/job-analytics/summary?${qs}`);
    return response.data;
  },

  getJobs: async (
    filter: JobAnalyticsFilterParams,
    page: number,
    pageSize: number,
    sortBy: string,
    sortOrder: string
  ): Promise<PaginatedResponse<JobAnalyticsEntry>> => {
    const qs = buildParams(filter, { page, page_size: pageSize, sort_by: sortBy, sort_order: sortOrder });
    const response = await api.get(`/api/admin/job-analytics/jobs?${qs}`);
    return response.data;
  },

  getTimeline: async (
    filter: JobAnalyticsFilterParams,
    resolution: string
  ): Promise<{ points: TimelinePoint[] }> => {
    const qs = buildParams(filter, { resolution });
    const response = await api.get(`/api/admin/job-analytics/timeline?${qs}`);
    return response.data;
  },

  getJobTimeline: async (
    jobId: string
  ): Promise<{ metrics: TimelinePoint[]; tasks: TaskSegment[] }> => {
    const response = await api.get(`/api/admin/job-analytics/jobs/${jobId}/timeline`);
    return response.data;
  },

  getSuccessRates: async (
    filter: JobAnalyticsFilterParams
  ): Promise<{ entries: SuccessRateEntry[] }> => {
    const qs = buildParams(filter);
    const response = await api.get(`/api/admin/job-analytics/success-rates?${qs}`);
    return response.data;
  },

  getBenchmarkHistory: async (
    params: { agent_id?: number; hash_type?: number; attack_mode?: number; page?: number; page_size?: number }
  ): Promise<PaginatedResponse<BenchmarkHistoryEntry>> => {
    const qs = new URLSearchParams();
    if (params.agent_id !== undefined) qs.append('agent_id', String(params.agent_id));
    if (params.hash_type !== undefined) qs.append('hash_type', String(params.hash_type));
    if (params.attack_mode !== undefined) qs.append('attack_mode', String(params.attack_mode));
    if (params.page) qs.append('page', String(params.page));
    if (params.page_size) qs.append('page_size', String(params.page_size));
    const response = await api.get(`/api/admin/job-analytics/benchmarks?${qs.toString()}`);
    return response.data;
  },

  getBenchmarkTrends: async (
    agentId: number,
    hashType?: number,
    attackMode?: number,
    timeRange?: string
  ): Promise<{ points: BenchmarkHistoryEntry[] }> => {
    const qs = new URLSearchParams();
    qs.append('agent_id', String(agentId));
    if (hashType !== undefined) qs.append('hash_type', String(hashType));
    if (attackMode !== undefined) qs.append('attack_mode', String(attackMode));
    if (timeRange) qs.append('time_range', timeRange);
    const response = await api.get(`/api/admin/job-analytics/benchmarks/trends?${qs.toString()}`);
    return response.data;
  },
};
