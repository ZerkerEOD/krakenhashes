-- Drop the deprecated total_keyspace column from job_executions
-- This column is redundant as we now use:
-- - base_keyspace: for --skip/--limit chunking (wordlist line count)
-- - effective_keyspace: for progress calculations (base × rules × salts)
ALTER TABLE job_executions DROP COLUMN IF EXISTS total_keyspace;
