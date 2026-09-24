-- Add the cloud GPU provisioning notification types.
--
-- These are deliberately in their own migration, separate from the tables that
-- reference them: ALTER TYPE ... ADD VALUE is only safe inside migrate's
-- transaction when the new value is ADDED but not USED in the same migration.
--
--   cloud_budget_threshold        a client crossed a configured spend threshold
--   cloud_provision_failed        an instance could not be launched (no VPN
--                                 credential, no capacity, quota, budget)
--   cloud_teardown_failed         an instance could NOT be destroyed after
--                                 repeated attempts — this one is money
--                                 actively burning and needs a human
--   cloud_vpn_credential_expiring a reusable VPN enrollment key is close to
--                                 expiry; once it lapses, provisioning stops
--
-- All four are registered as auditable events on the Go side, so dispatching
-- one also writes an audit_log row (audit_log.event_type is this enum).

ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'cloud_budget_threshold';
ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'cloud_provision_failed';
ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'cloud_teardown_failed';
ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'cloud_vpn_credential_expiring';
