-- Revert 20260909140000_add_cloud_default_burst_enabled.up.sql.
--
-- COALESCE(..., false) in cloudEligibilityPredicate means a missing key reads
-- as "no default", so removing this row is safe in the spending direction: it
-- can only make jobs less eligible, never more. A cloud-only deployment that
-- was relying on it will stop provisioning until the flag is ticked per job.

DELETE FROM system_settings WHERE key = 'cloud_default_burst_enabled';
