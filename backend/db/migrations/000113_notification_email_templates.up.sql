-- Migration 113: Extend email_template_type ENUM for notification types
-- NOTE: Cannot use new ENUM values in same transaction where they're added (PostgreSQL limitation)
-- The INSERT statements for template data are in migration 114

ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'security_password_changed';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'security_mfa_disabled';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'security_suspicious_login';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'job_started';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'job_failed';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'first_crack';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'task_completed';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'agent_offline';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'agent_error';
ALTER TYPE email_template_type ADD VALUE IF NOT EXISTS 'webhook_failure';
