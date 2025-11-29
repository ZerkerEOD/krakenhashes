-- Add is_keyspace_split flag to job_tasks table
-- This flag controls whether --skip/--limit should be used when building hashcat commands
-- Only true for dictionary-only attacks that use keyspace splitting
-- False for rule chunking (combinator, dict+rules) and increment mode
ALTER TABLE job_tasks
    ADD COLUMN is_keyspace_split BOOLEAN DEFAULT FALSE;

COMMENT ON COLUMN job_tasks.is_keyspace_split IS 'True if task uses keyspace splitting (--skip/--limit), false for rule chunking or increment mode';
