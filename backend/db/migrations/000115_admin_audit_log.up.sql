-- Migration 000115: Admin Audit Log System
-- Creates a separate audit_log table for admin visibility into security/critical events
-- This allows admins to view security events across all users without accessing user notifications

CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Event metadata
    event_type notification_type NOT NULL,
    severity VARCHAR(20) NOT NULL DEFAULT 'info',  -- 'info', 'warning', 'critical'

    -- User context (who the event happened to)
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    username VARCHAR(255),      -- Stored separately for visibility when user is deleted
    user_email VARCHAR(255),    -- Stored separately for visibility when user is deleted

    -- Event details
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    data JSONB DEFAULT '{}',

    -- Source tracking (links to related entities)
    source_type VARCHAR(50),
    source_id VARCHAR(255),

    -- Request context
    ip_address INET,
    user_agent TEXT,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient queries
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);
CREATE INDEX idx_audit_log_event_type ON audit_log(event_type);
CREATE INDEX idx_audit_log_user_id ON audit_log(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX idx_audit_log_severity ON audit_log(severity);
CREATE INDEX idx_audit_log_type_date ON audit_log(event_type, created_at DESC);

-- Comment on table
COMMENT ON TABLE audit_log IS 'Admin-visible audit log for security and critical events across all users';
COMMENT ON COLUMN audit_log.severity IS 'Event severity: info, warning, or critical';
COMMENT ON COLUMN audit_log.username IS 'Cached username for visibility when user is deleted';
COMMENT ON COLUMN audit_log.user_email IS 'Cached email for visibility when user is deleted';
