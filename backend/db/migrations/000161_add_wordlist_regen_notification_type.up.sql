-- Add the 'wordlist_regen_failed' notification type (GH #40 follow-up).
--
-- Emitted when automatic regeneration of a filtered wordlist (after its parent
-- changed) fails. It is registered as an auditable event, so dispatching it writes
-- an audit_log row and broadcasts a system alert to admins.
--
-- ALTER TYPE ... ADD VALUE is safe inside migrate's transaction here because the
-- new value is only ADDED (not used) in this migration.

ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'wordlist_regen_failed';
