import { api } from './api';
import {
  AnalyticsReport,
  CreateAnalyticsReportRequest,
  HashlistSummary,
  QueueStatus,
} from '../types/analytics';

export const analyticsService = {
  // Create a new analytics report
  createReport: async (data: CreateAnalyticsReportRequest): Promise<AnalyticsReport> => {
    const response = await api.post('/api/analytics/reports', data);
    return response.data;
  },

  // Get a specific report by ID
  getReport: async (id: string): Promise<{ status: string; message?: string; report: AnalyticsReport }> => {
    const response = await api.get(`/api/analytics/reports/${id}`);
    return response.data;
  },

  // Get all reports for a specific client
  getClientReports: async (clientId: string): Promise<AnalyticsReport[]> => {
    const response = await api.get(`/api/analytics/reports/client/${clientId}`);
    return response.data;
  },

  // Delete a report
  deleteReport: async (id: string): Promise<void> => {
    await api.delete(`/api/analytics/reports/${id}`);
  },

  // Retry a failed report
  retryReport: async (id: string): Promise<AnalyticsReport> => {
    const response = await api.post(`/api/analytics/reports/${id}/retry`);
    return response.data;
  },

  // Get queue status
  getQueueStatus: async (): Promise<QueueStatus> => {
    const response = await api.get('/api/analytics/queue-status');
    return response.data;
  },

  // Get hashlists available for analytics report (filtered by client + date range)
  getHashlistsForReport: async (clientId: string, startDate: string, endDate: string): Promise<HashlistSummary[]> => {
    const response = await api.get('/api/analytics/hashlists', {
      params: { client_id: clientId, start_date: startDate, end_date: endDate },
    });
    return response.data;
  },

  // Export a completed report as a PDF and trigger a browser download.
  // type 'internal' contains plaintext passwords/usernames; 'external' is an
  // aggregate-only summary redacted server-side.
  exportReportPdf: async (id: string, type: 'internal' | 'external'): Promise<void> => {
    const response = await api.get(`/api/analytics/reports/${id}/export`, {
      params: { type, format: 'pdf' },
      responseType: 'blob',
    });

    // Prefer the server-provided filename from Content-Disposition.
    let filename = `analytics_${type}_${id.slice(0, 8)}.pdf`;
    const disposition = response.headers?.['content-disposition'];
    if (disposition) {
      const match = /filename="?([^"]+)"?/.exec(disposition);
      if (match && match[1]) {
        filename = match[1];
      }
    }

    const blob = new Blob([response.data], { type: 'application/pdf' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    window.URL.revokeObjectURL(url);
  },
};

export default analyticsService;
