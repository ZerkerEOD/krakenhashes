-- Revert 20260909150000_add_cloud_default_client.up.sql.
--
-- Safe in the spending direction: the resolution COALESCEs to NULL when the key
-- is missing and the join is an INNER JOIN, so removing this row can only make
-- jobs less eligible, never more. A deployment that was relying on it stops
-- provisioning for client-less hashlists until a client is assigned.

DELETE FROM system_settings WHERE key = 'cloud_default_client_id';
