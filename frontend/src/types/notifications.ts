/**
 * Notification system types
 */

// Notification types matching backend
export type NotificationType =
  | 'job_started'
  | 'job_completed'
  | 'job_failed'
  | 'first_crack'
  | 'task_completed_with_cracks'
  | 'agent_offline'
  | 'agent_error'
  | 'security_suspicious_login'
  | 'security_mfa_disabled'
  | 'security_password_changed'
  | 'webhook_failure';

// Notification channels
export type NotificationChannel = 'in_app' | 'email' | 'webhook';

// Task report mode for task completion notifications
export type TaskReportMode = 'only_if_cracks' | 'always';

// Notification category
export type NotificationCategory = 'job' | 'agent' | 'security' | 'system';

// Individual notification
export interface Notification {
  id: string;
  user_id: string;
  notification_type: NotificationType;
  title: string;
  message: string;
  data?: Record<string, unknown>;
  in_app_read: boolean;
  in_app_read_at?: string;
  email_sent: boolean;
  email_sent_at?: string;
  email_error?: string;
  webhook_sent: boolean;
  webhook_sent_at?: string;
  webhook_error?: string;
  source_type?: string;
  source_id?: string;
  created_at: string;
}

// Notification list response
export interface NotificationListResponse {
  notifications: Notification[];
  total: number;
  limit: number;
  offset: number;
}

// Notification list query params
export interface NotificationListParams {
  limit?: number;
  offset?: number;
  category?: NotificationCategory;
  type?: NotificationType;
  read?: boolean;
}

// User notification preference for a specific type
export interface TypeChannelPreference {
  enabled: boolean;
  inAppEnabled: boolean;
  emailEnabled: boolean;
  webhookEnabled: boolean;
  settings?: Record<string, unknown>;
}

// Full user notification preferences
export interface UserNotificationPreferences {
  notifyOnJobCompletion: boolean; // Legacy field for backward compatibility
  emailConfigured: boolean;
  typePreferences: Record<NotificationType, TypeChannelPreference>;
  webhooksConfigured: number;
  webhooksActive: number;
}

// Update preferences request
export interface UpdatePreferencesRequest {
  typePreferences: Partial<Record<NotificationType, Partial<TypeChannelPreference>>>;
}

// User webhook configuration
export interface UserWebhook {
  id: string;
  user_id: string;
  name: string;
  url: string;
  is_active: boolean;
  notification_types?: string[];
  custom_headers?: Record<string, string>;
  retry_count: number;
  timeout_seconds: number;
  last_triggered_at?: string;
  last_success_at?: string;
  last_error?: string;
  total_sent: number;
  total_failed: number;
  created_at: string;
  updated_at: string;
}

// Create webhook request
export interface CreateWebhookRequest {
  name: string;
  url: string;
  secret?: string;
  notification_types?: string[];
  custom_headers?: Record<string, string>;
  retry_count?: number;
  timeout_seconds?: number;
}

// Update webhook request
export interface UpdateWebhookRequest {
  name?: string;
  url?: string;
  secret?: string;
  is_active?: boolean;
  notification_types?: string[];
  custom_headers?: Record<string, string>;
  retry_count?: number;
  timeout_seconds?: number;
}

// Webhook test result
export interface WebhookTestResult {
  success: boolean;
  message?: string;
  error?: string;
}

// Admin webhook view (includes user info)
export interface AdminWebhookView extends UserWebhook {
  username: string;
  email: string;
}

// Global webhook settings (admin)
export interface GlobalWebhookSettings {
  url: string;
  enabled: boolean;
  has_secret: boolean;
  custom_headers?: string;
}

// Update global webhook settings request
export interface UpdateGlobalWebhookSettingsRequest {
  url?: string;
  secret?: string;
  enabled?: boolean;
  custom_headers?: string;
}

// Agent offline settings
export interface AgentOfflineSettings {
  buffer_minutes: number;
}

// WebSocket message types
export type WSMessageType =
  | 'notification'
  | 'unread_count'
  | 'mark_read'
  | 'ping'
  | 'pong';

// WebSocket message
export interface WSMessage<T = unknown> {
  type: WSMessageType;
  payload: T;
}

// WebSocket notification payload
export interface WSNotificationPayload extends Notification {}

// WebSocket unread count payload
export interface WSUnreadCountPayload {
  count: number;
}

// WebSocket mark read payload
export interface WSMarkReadPayload {
  notification_ids: string[];
}

// Notification type metadata for UI
export interface NotificationTypeMetadata {
  type: NotificationType;
  label: string;
  description: string;
  category: NotificationCategory;
  isMandatory: boolean;
  defaultChannels: {
    inApp: boolean;
    email: boolean;
    webhook: boolean;
  };
}

// All notification types with metadata
export const NOTIFICATION_TYPES: NotificationTypeMetadata[] = [
  {
    type: 'job_started',
    label: 'Job Started',
    description: 'When a job begins execution',
    category: 'job',
    isMandatory: false,
    defaultChannels: { inApp: true, email: false, webhook: false },
  },
  {
    type: 'job_completed',
    label: 'Job Completed',
    description: 'When a job finishes successfully',
    category: 'job',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'job_failed',
    label: 'Job Failed',
    description: 'When a job fails',
    category: 'job',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'first_crack',
    label: 'First Crack',
    description: 'When the first hash is cracked (immediate notification)',
    category: 'job',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: true },
  },
  {
    type: 'task_completed_with_cracks',
    label: 'Task Completion Report',
    description: 'When a task completes (configurable)',
    category: 'job',
    isMandatory: false,
    defaultChannels: { inApp: true, email: false, webhook: false },
  },
  {
    type: 'agent_offline',
    label: 'Agent Offline',
    description: 'When an agent goes offline (after buffer period)',
    category: 'agent',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'agent_error',
    label: 'Agent Error',
    description: 'When an agent reports a critical error',
    category: 'agent',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'security_suspicious_login',
    label: 'Suspicious Login',
    description: 'Login from new location or device',
    category: 'security',
    isMandatory: false,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'security_mfa_disabled',
    label: 'MFA Disabled',
    description: 'When MFA is turned off (mandatory)',
    category: 'security',
    isMandatory: true,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'security_password_changed',
    label: 'Password Changed',
    description: 'When password is changed (mandatory)',
    category: 'security',
    isMandatory: true,
    defaultChannels: { inApp: true, email: true, webhook: false },
  },
  {
    type: 'webhook_failure',
    label: 'Webhook Failure',
    description: 'When a webhook delivery fails',
    category: 'system',
    isMandatory: false,
    defaultChannels: { inApp: true, email: false, webhook: false },
  },
];

// Helper to get notification type metadata
export function getNotificationTypeMetadata(type: NotificationType): NotificationTypeMetadata | undefined {
  return NOTIFICATION_TYPES.find((t) => t.type === type);
}

// Helper to get types by category
export function getNotificationTypesByCategory(category: NotificationCategory): NotificationTypeMetadata[] {
  return NOTIFICATION_TYPES.filter((t) => t.category === category);
}

// Categories with labels
export const NOTIFICATION_CATEGORIES: { category: NotificationCategory; label: string }[] = [
  { category: 'job', label: 'Job Notifications' },
  { category: 'agent', label: 'Agent Notifications' },
  { category: 'security', label: 'Security Notifications' },
  { category: 'system', label: 'System Notifications' },
];

// ======================
// Audit Log Types (Admin)
// ======================

// Audit log severity levels
export type AuditLogSeverity = 'info' | 'warning' | 'critical';

// Audit log entry
export interface AuditLog {
  id: string;
  event_type: NotificationType;
  severity: AuditLogSeverity;
  user_id?: string;
  username: string;
  user_email: string;
  title: string;
  message: string;
  data?: Record<string, unknown>;
  source_type?: string;
  source_id?: string;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
}

// Audit log list response
export interface AuditLogListResponse {
  audit_logs: AuditLog[];
  total: number;
  limit: number;
  offset: number;
}

// Audit log query params
export interface AuditLogListParams {
  event_type?: NotificationType[];
  user_id?: string;
  severity?: AuditLogSeverity;
  start_date?: string;
  end_date?: string;
  limit?: number;
  offset?: number;
}

// Auditable event type metadata
export interface AuditableEventType {
  type: NotificationType;
  category: NotificationCategory;
  severity: AuditLogSeverity;
  description: string;
}

// ======================
// System Alert Types (Real-time Admin Alerts)
// ======================

// System alert for real-time notifications to admins
export interface SystemAlert {
  id: string;
  type: 'system_alert';
  event_type: NotificationType;
  severity: AuditLogSeverity;
  title: string;
  message: string;
  affected_user: string;
  data?: Record<string, unknown>;
  timestamp: number;
}

// Extended WebSocket message types to include system alerts
export type ExtendedWSMessageType = WSMessageType | 'system_alert';

// Extended WebSocket message
export interface ExtendedWSMessage<T = unknown> {
  type: ExtendedWSMessageType;
  payload: T;
}
