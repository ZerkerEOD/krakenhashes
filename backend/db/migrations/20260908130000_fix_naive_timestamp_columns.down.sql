-- Revert 20260908130000_fix_naive_timestamp_columns.up.sql.
--
-- `AT TIME ZONE 'UTC'` applied to a timestamptz yields the wall clock in UTC,
-- which is the inverse of the up migration and restores exactly the bytes that
-- were there before it ran.
--
-- Note this reinstates the bug the up migration fixes: comparisons against
-- NOW() become wrong by the UTC offset for any backend not running as UTC.

ALTER TABLE agents
    ALTER COLUMN sync_started_at   TYPE TIMESTAMP USING sync_started_at   AT TIME ZONE 'UTC',
    ALTER COLUMN sync_completed_at TYPE TIMESTAMP USING sync_completed_at AT TIME ZONE 'UTC';

ALTER TABLE benchmark_requests
    ALTER COLUMN requested_at TYPE TIMESTAMP USING requested_at AT TIME ZONE 'UTC',
    ALTER COLUMN completed_at TYPE TIMESTAMP USING completed_at AT TIME ZONE 'UTC';

ALTER TABLE job_executions
    ALTER COLUMN last_progress_update     TYPE TIMESTAMP USING last_progress_update     AT TIME ZONE 'UTC',
    ALTER COLUMN completion_email_sent_at TYPE TIMESTAMP USING completion_email_sent_at AT TIME ZONE 'UTC';
