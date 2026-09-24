-- Remove the purge timestamp rows.
--
-- Dropping these returns the retention service to logging an error on every
-- purge, which is the behaviour before the up migration. Only the rows this
-- migration could have created are removed.

DELETE FROM client_settings WHERE key IN ('last_purge_run', 'last_analytics_purge_run');
