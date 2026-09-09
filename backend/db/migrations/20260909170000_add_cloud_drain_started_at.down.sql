-- Revert 20260909170000_add_cloud_drain_started_at.up.sql.
--
-- Dropping the column returns the drain rung to being cosmetic: without a clock
-- there is nothing for drain_timeout_seconds to measure against. Instances
-- already draining stay bounded by TTL, idle drain and the hard-stop rung.

ALTER TABLE cloud_instances
    DROP COLUMN IF EXISTS drain_started_at;
