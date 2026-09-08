-- Revert 20260909120000_add_cloud_commissioning_grace.up.sql.
--
-- Removing the key returns the reaper to judging a never-commissioned instance
-- with cloud_idle_drain_minutes, which reinstates the rent/kill/rent loop the up
-- migration exists to close. The code's compiled-in default (30 minutes) still
-- applies, so a rollback of the schema WITHOUT a rollback of the binary is safe;
-- it is only the pair that reopens the bug.

DELETE FROM system_settings WHERE key = 'cloud_commissioning_grace_minutes';
