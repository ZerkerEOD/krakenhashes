-- Remove rule splitting.
--
-- Scheduler-v2 keyspace-splits every job via hashcat --skip/--limit and derives
-- keyspace from the job's own attack params. The rule-splitting columns, indexes
-- and settings are no longer read by any code path (the legacy scheduler that used
-- them is no longer started). This migration drops them, plus two now-unused knobs:
--   - chunk_fluctuation_percentage: v2 has no look-ahead remainder merge (its tail
--     guard is min_chunk_seconds).
--   - speedtest_timeout_seconds: superseded by speed_test_timeout_seconds_uncompressed
--     / _compressed (+ a fixed grace) in scheduler/speedtest.go.

-- Indexes first (Postgres would auto-drop them with the columns, but be explicit).
DROP INDEX IF EXISTS idx_job_executions_rule_splitting;
DROP INDEX IF EXISTS idx_job_tasks_rule_split;

ALTER TABLE job_executions
    DROP COLUMN IF EXISTS uses_rule_splitting,
    DROP COLUMN IF EXISTS rule_split_count;

ALTER TABLE job_tasks
    DROP COLUMN IF EXISTS rule_start_index,
    DROP COLUMN IF EXISTS rule_end_index,
    DROP COLUMN IF EXISTS rule_chunk_path,
    DROP COLUMN IF EXISTS is_rule_split_task;

ALTER TABLE preset_jobs
    DROP COLUMN IF EXISTS use_rule_splitting;

DELETE FROM system_settings WHERE key IN (
    'rule_split_enabled',
    'rule_split_threshold',
    'rule_split_min_rules',
    'rule_split_max_chunks',
    'rule_chunk_temp_dir',
    'chunk_fluctuation_percentage',
    'speedtest_timeout_seconds'
);
