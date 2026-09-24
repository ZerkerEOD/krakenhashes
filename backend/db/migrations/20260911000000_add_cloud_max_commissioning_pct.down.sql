-- Revert 20260911000000_add_cloud_max_commissioning_pct.up.sql.
--
-- Removing the key does NOT restore the old 5-minute floor: cloud.DefaultSettings()
-- compiles in the same 33% default, and LoadSettings only overrides it when the
-- row exists. Only rolling back the code as well brings back minUsefulTTL, and
-- with it the ability to rent an instance for less time than it needs to
-- finish starting up.

DELETE FROM system_settings WHERE key = 'cloud_max_commissioning_pct';
