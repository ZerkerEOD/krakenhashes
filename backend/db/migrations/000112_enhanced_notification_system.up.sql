-- Migration 000112: Enhanced Notification System
-- Adds support for in-app notifications, user notification preferences, webhooks, and agent offline buffering

-- ============================================================
-- Create notification_type enum for type safety
-- ============================================================
CREATE TYPE notification_type AS ENUM (
    'job_started',
    'job_completed',
    'job_failed',
    'first_crack',
    'task_completed_with_cracks',
    'agent_offline',
    'agent_error',
    'security_suspicious_login',
    'security_mfa_disabled',
    'security_password_changed',
    'webhook_failure'
);

-- ============================================================
-- Table: notifications
-- Stores all notification history for audit and display
-- ============================================================
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notification_type notification_type NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    data JSONB DEFAULT '{}',

    -- In-app delivery tracking
    in_app_read BOOLEAN NOT NULL DEFAULT FALSE,
    in_app_read_at TIMESTAMPTZ,

    -- Email delivery tracking
    email_sent BOOLEAN NOT NULL DEFAULT FALSE,
    email_sent_at TIMESTAMPTZ,
    email_error TEXT,

    -- Webhook delivery tracking
    webhook_sent BOOLEAN NOT NULL DEFAULT FALSE,
    webhook_sent_at TIMESTAMPTZ,
    webhook_error TEXT,

    -- Source tracking for navigation
    source_type VARCHAR(50),
    source_id VARCHAR(255),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient queries
CREATE INDEX idx_notifications_user_id ON notifications(user_id);
CREATE INDEX idx_notifications_user_unread ON notifications(user_id, in_app_read)
    WHERE in_app_read = FALSE;
CREATE INDEX idx_notifications_created_at ON notifications(created_at DESC);
CREATE INDEX idx_notifications_type ON notifications(notification_type);
CREATE INDEX idx_notifications_source ON notifications(source_type, source_id)
    WHERE source_type IS NOT NULL;

-- ============================================================
-- Table: user_notification_preferences
-- Granular per-type, per-channel settings for each user
-- ============================================================
CREATE TABLE user_notification_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notification_type notification_type NOT NULL,

    -- Channel enables (each can be independently toggled)
    in_app_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    webhook_enabled BOOLEAN NOT NULL DEFAULT FALSE,

    -- Type-specific settings stored as JSONB
    -- For task_completed_with_cracks: {"mode": "only_if_cracks" | "always"}
    settings JSONB DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Each user can only have one preference row per notification type
    CONSTRAINT user_notification_prefs_unique UNIQUE (user_id, notification_type)
);

CREATE INDEX idx_user_notification_prefs_user ON user_notification_preferences(user_id);

-- Trigger for updated_at
CREATE TRIGGER update_user_notification_preferences_updated_at
    BEFORE UPDATE ON user_notification_preferences
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Table: user_webhooks
-- Per-user webhook configurations (generic, works with Slack/Teams/Discord/etc.)
-- ============================================================
CREATE TABLE user_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    url TEXT NOT NULL,
    secret VARCHAR(255),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Optional per-type filtering (NULL = all types)
    notification_types notification_type[],

    -- Headers to include (e.g., Authorization)
    custom_headers JSONB DEFAULT '{}',

    -- Retry configuration
    retry_count INT NOT NULL DEFAULT 3,
    timeout_seconds INT NOT NULL DEFAULT 30,

    -- Statistics
    last_triggered_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT,
    total_sent INT NOT NULL DEFAULT 0,
    total_failed INT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Each user can only have one webhook with the same name
    CONSTRAINT user_webhooks_unique_name UNIQUE (user_id, name)
);

CREATE INDEX idx_user_webhooks_user_active ON user_webhooks(user_id) WHERE is_active = TRUE;

CREATE TRIGGER update_user_webhooks_updated_at
    BEFORE UPDATE ON user_webhooks
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Table: agent_offline_buffer
-- Tracks agents pending offline notification (10-min buffer)
-- ============================================================
CREATE TABLE agent_offline_buffer (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    disconnected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notification_due_at TIMESTAMPTZ NOT NULL,
    notification_sent BOOLEAN NOT NULL DEFAULT FALSE,
    notification_sent_at TIMESTAMPTZ,
    reconnected BOOLEAN NOT NULL DEFAULT FALSE,
    reconnected_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for finding pending notifications that are due
CREATE INDEX idx_agent_offline_buffer_pending ON agent_offline_buffer(notification_due_at)
    WHERE notification_sent = FALSE AND reconnected = FALSE;
CREATE INDEX idx_agent_offline_buffer_agent ON agent_offline_buffer(agent_id);

-- ============================================================
-- System settings for global webhook configuration
-- ============================================================
INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('global_webhook_url', NULL, 'System-wide webhook URL for notifications (sends to all notification types)', 'string'),
    ('global_webhook_secret', NULL, 'Signing secret for system webhook (optional)', 'string'),
    ('global_webhook_enabled', 'false', 'Enable system-wide webhook notifications', 'boolean'),
    ('global_webhook_custom_headers', '{}', 'Custom headers for system webhook as JSON', 'string'),
    ('agent_offline_buffer_minutes', '10', 'Minutes to wait before sending agent offline notification', 'integer')
ON CONFLICT (key) DO NOTHING;

-- Note: Email templates for notification types are not added here because
-- the existing email_template_type enum does not include these notification types.
-- The notification system uses the NotificationDispatcher which can send emails
-- using the existing email infrastructure without requiring dedicated templates.
