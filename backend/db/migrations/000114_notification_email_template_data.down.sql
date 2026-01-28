-- Migration 114 Down: Remove notification email templates
-- Note: ENUM values from migration 113 cannot be removed in PostgreSQL

DELETE FROM email_templates WHERE template_type IN (
    'security_password_changed',
    'security_mfa_disabled',
    'security_suspicious_login',
    'job_started',
    'job_failed',
    'first_crack',
    'task_completed',
    'agent_offline',
    'agent_error',
    'webhook_failure'
);

-- Drop the unique index added in the up migration
DROP INDEX IF EXISTS email_templates_template_type_unique;
