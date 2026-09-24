-- Revert 20260909130000_add_cloud_default_max_instances.up.sql.
--
-- Removing the key returns a blank per-job cloud_max_instances to meaning
-- unlimited, which in a cloud-only deployment leaves the client budget as the
-- only thing bounding how many instances a single starving job can rent.

DELETE FROM system_settings WHERE key = 'cloud_default_max_instances_per_job';
