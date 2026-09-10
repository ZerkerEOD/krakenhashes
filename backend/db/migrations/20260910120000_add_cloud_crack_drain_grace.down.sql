-- Revert 20260910120000_add_cloud_crack_drain_grace.up.sql.
--
-- Removing the key does NOT reopen the bug on its own: cloud.DefaultSettings()
-- compiles in the same 10-minute default, and LoadSettings only overrides it
-- when the row exists. Only rolling back the code as well restores the
-- unconditional teardown that loses cracks.

DELETE FROM system_settings WHERE key = 'cloud_crack_drain_grace_minutes';
