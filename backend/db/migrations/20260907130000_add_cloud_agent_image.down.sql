-- Reverses 20260907130000_add_cloud_agent_image.up.sql.
--
-- Removing the row returns the deployment to reading KH_CLOUD_AGENT_IMAGE,
-- which is the correct rollback: the environment variable is still consulted
-- whenever this setting is unset.
DELETE FROM system_settings WHERE key = 'cloud_agent_image';
