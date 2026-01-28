-- Migration 000112 DOWN: Remove Enhanced Notification System

-- Remove email templates for new notification types
DELETE FROM email_templates WHERE type IN (
    'job_started',
    'first_crack',
    'job_failed',
    'agent_offline',
    'agent_error',
    'security_suspicious_login',
    'security_mfa_disabled',
    'security_password_changed',
    'task_completed_with_cracks',
    'webhook_failure'
);

-- Remove system settings
DELETE FROM system_settings WHERE key IN (
    'global_webhook_url',
    'global_webhook_secret',
    'global_webhook_enabled',
    'global_webhook_custom_headers',
    'agent_offline_buffer_minutes'
);

-- Drop tables in reverse order of creation (respecting dependencies)
DROP TABLE IF EXISTS agent_offline_buffer;
DROP TABLE IF EXISTS user_webhooks;
DROP TABLE IF EXISTS user_notification_preferences;
DROP TABLE IF EXISTS notifications;

-- Drop the notification_type enum
DROP TYPE IF EXISTS notification_type;
