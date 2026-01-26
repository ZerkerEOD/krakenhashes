-- Restore the total_keyspace column (for rollback purposes)
-- Note: Data will be lost on rollback; effective_keyspace should be used as the source of truth
ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS total_keyspace BIGINT;
