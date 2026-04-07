DROP INDEX IF EXISTS idx_job_executions_archived_at;
ALTER TABLE job_executions DROP COLUMN IF EXISTS archived_at;
