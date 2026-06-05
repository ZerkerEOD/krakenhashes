-- The scheduler_v2_enabled gate has been removed. Scheduler-v2 is the
-- only scheduler that ticks; legacy is no longer started. The setting
-- is no longer read by any code path and is dropped here to avoid a
-- stale row in system_settings that operators could mistakenly toggle.
--
-- See migration 000152 for the original introduction of this setting
-- during the transition window.

DELETE FROM system_settings WHERE key = 'scheduler_v2_enabled';
