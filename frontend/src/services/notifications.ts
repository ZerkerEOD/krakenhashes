/**
 * Notification service for API calls
 */
import { api } from './api';
import type {
  Notification,
  NotificationListResponse,
  NotificationListParams,
  UserNotificationPreferences,
  UpdatePreferencesRequest,
  UserWebhook,
  CreateWebhookRequest,
  UpdateWebhookRequest,
  WebhookTestResult,
  AdminWebhookView,
  GlobalWebhookSettings,
  UpdateGlobalWebhookSettingsRequest,
  AgentOfflineSettings,
  AuditLogListResponse,
  AuditLogListParams,
  AuditLog,
  AuditableEventType,
} from '../types/notifications';

// =====================
// User Notification API
// =====================

/**
 * Get list of notifications with optional filters
 */
export async function getNotifications(
  params?: NotificationListParams
): Promise<NotificationListResponse> {
  const queryParams = new URLSearchParams();
  if (params?.limit) queryParams.set('limit', params.limit.toString());
  if (params?.offset) queryParams.set('offset', params.offset.toString());
  if (params?.category) queryParams.set('category', params.category);
  if (params?.type) queryParams.set('type', params.type);
  if (params?.read !== undefined) queryParams.set('read', params.read.toString());

  const query = queryParams.toString();
  const url = `/api/user/notifications${query ? `?${query}` : ''}`;
  const response = await api.get<NotificationListResponse>(url);
  return response.data;
}

/**
 * Get recent notifications (for dropdown)
 */
export async function getRecentNotifications(
  limit: number = 5
): Promise<{ notifications: Notification[] }> {
  const response = await api.get<{ notifications: Notification[] }>(
    `/api/user/notifications/recent?limit=${limit}`
  );
  return response.data;
}

/**
 * Get unread notification count
 */
export async function getUnreadCount(): Promise<{ count: number }> {
  const response = await api.get<{ count: number }>(
    '/api/user/notifications/unread-count'
  );
  return response.data;
}

/**
 * Mark a notification as read
 */
export async function markAsRead(notificationId: string): Promise<void> {
  await api.put(`/api/user/notifications/${notificationId}`);
}

/**
 * Mark all notifications as read
 */
export async function markAllAsRead(): Promise<{ marked_count: number }> {
  const response = await api.put<{ status: string; marked_count: number }>(
    '/api/user/notifications/read-all'
  );
  return { marked_count: response.data.marked_count };
}

/**
 * Delete notifications
 */
export async function deleteNotifications(
  ids: string[]
): Promise<{ deleted_count: number }> {
  const response = await api.delete<{ status: string; deleted_count: number }>(
    '/api/user/notifications',
    { data: { ids } }
  );
  return { deleted_count: response.data.deleted_count };
}

// =====================
// User Preferences API
// =====================

/**
 * Get user notification preferences
 */
export async function getNotificationPreferences(): Promise<UserNotificationPreferences> {
  const response = await api.get<UserNotificationPreferences>(
    '/api/user/notification-preferences'
  );
  return response.data;
}

/**
 * Update user notification preferences
 */
export async function updateNotificationPreferences(
  request: UpdatePreferencesRequest
): Promise<void> {
  await api.put('/api/user/notification-preferences', request);
}

// =====================
// User Webhook API
// =====================

/**
 * Get all webhooks for the current user
 */
export async function getUserWebhooks(): Promise<{ webhooks: UserWebhook[] }> {
  const response = await api.get<{ webhooks: UserWebhook[] }>(
    '/api/user/webhooks'
  );
  return response.data;
}

/**
 * Get a specific webhook
 */
export async function getUserWebhook(id: string): Promise<UserWebhook> {
  const response = await api.get<UserWebhook>(`/api/user/webhooks/${id}`);
  return response.data;
}

/**
 * Create a new webhook
 */
export async function createUserWebhook(
  request: CreateWebhookRequest
): Promise<UserWebhook> {
  const response = await api.post<UserWebhook>('/api/user/webhooks', request);
  return response.data;
}

/**
 * Update a webhook
 */
export async function updateUserWebhook(
  id: string,
  request: UpdateWebhookRequest
): Promise<UserWebhook> {
  const response = await api.put<UserWebhook>(
    `/api/user/webhooks/${id}`,
    request
  );
  return response.data;
}

/**
 * Delete a webhook
 */
export async function deleteUserWebhook(id: string): Promise<void> {
  await api.delete(`/api/user/webhooks/${id}`);
}

/**
 * Test a specific webhook
 */
export async function testUserWebhook(id: string): Promise<WebhookTestResult> {
  const response = await api.post<WebhookTestResult>(
    `/api/user/webhooks/${id}/test`
  );
  return response.data;
}

/**
 * Test a webhook URL without saving
 */
export async function testWebhookUrl(
  url: string,
  secret?: string
): Promise<WebhookTestResult> {
  const response = await api.post<WebhookTestResult>(
    '/api/user/webhooks/test-url',
    { url, secret }
  );
  return response.data;
}

// =====================
// Admin Notification API
// =====================

/**
 * Get global webhook settings
 */
export async function getGlobalWebhookSettings(): Promise<GlobalWebhookSettings> {
  const response = await api.get<GlobalWebhookSettings>(
    '/api/admin/notification-settings'
  );
  return response.data;
}

/**
 * Update global webhook settings
 */
export async function updateGlobalWebhookSettings(
  request: UpdateGlobalWebhookSettingsRequest
): Promise<void> {
  await api.put('/api/admin/notification-settings', request);
}

/**
 * Test global webhook
 */
export async function testGlobalWebhook(): Promise<WebhookTestResult> {
  const response = await api.post<WebhookTestResult>(
    '/api/admin/notification-settings/test-webhook'
  );
  return response.data;
}

/**
 * Get all user webhooks (admin view)
 */
export async function getAllUserWebhooks(): Promise<{
  webhooks: AdminWebhookView[];
}> {
  const response = await api.get<{ webhooks: AdminWebhookView[] }>(
    '/api/admin/users/webhooks'
  );
  return response.data;
}

/**
 * Get agent offline buffer settings
 */
export async function getAgentOfflineSettings(): Promise<AgentOfflineSettings> {
  const response = await api.get<AgentOfflineSettings>(
    '/api/admin/notification-settings/agent-offline'
  );
  return response.data;
}

/**
 * Update agent offline buffer settings
 */
export async function updateAgentOfflineSettings(
  bufferMinutes: number
): Promise<void> {
  await api.put('/api/admin/notification-settings/agent-offline', {
    buffer_minutes: bufferMinutes,
  });
}

// =====================
// Admin Audit Log API
// =====================

/**
 * Get audit logs with optional filters (admin only)
 */
export async function getAuditLogs(
  params?: AuditLogListParams
): Promise<AuditLogListResponse> {
  const queryParams = new URLSearchParams();

  if (params?.event_type && params.event_type.length > 0) {
    params.event_type.forEach((type) => {
      queryParams.append('event_type[]', type);
    });
  }
  if (params?.user_id) queryParams.set('user_id', params.user_id);
  if (params?.severity) queryParams.set('severity', params.severity);
  if (params?.start_date) queryParams.set('start_date', params.start_date);
  if (params?.end_date) queryParams.set('end_date', params.end_date);
  if (params?.limit) queryParams.set('limit', params.limit.toString());
  if (params?.offset) queryParams.set('offset', params.offset.toString());

  const query = queryParams.toString();
  const url = `/api/admin/audit-logs${query ? `?${query}` : ''}`;
  const response = await api.get<AuditLogListResponse>(url);
  return response.data;
}

/**
 * Get a specific audit log entry (admin only)
 */
export async function getAuditLog(id: string): Promise<AuditLog> {
  const response = await api.get<AuditLog>(`/api/admin/audit-logs/${id}`);
  return response.data;
}

/**
 * Get list of auditable event types (admin only)
 */
export async function getAuditableEventTypes(): Promise<{
  event_types: AuditableEventType[];
}> {
  const response = await api.get<{ event_types: AuditableEventType[] }>(
    '/api/admin/audit-logs/event-types'
  );
  return response.data;
}

// =====================
// WebSocket URL Helper
// =====================

/**
 * Get the WebSocket URL for notifications
 */
export function getNotificationWebSocketUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = window.location.host;
  return `${protocol}//${host}/api/user/notifications/ws`;
}
