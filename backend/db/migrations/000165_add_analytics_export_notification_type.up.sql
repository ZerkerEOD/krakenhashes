-- Add the 'analytics_export' notification type.
--
-- Emitted when a user exports an analytics report to PDF (internal or external).
-- It is registered as an auditable event, so dispatching it writes an audit_log row
-- (audit_log.event_type is the notification_type enum) recording who exported which
-- report, at what classification.
--
-- ALTER TYPE ... ADD VALUE is safe inside migrate's transaction here because the
-- new value is only ADDED (not used) in this migration.

ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'analytics_export';
