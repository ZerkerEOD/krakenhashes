-- Revert cracking_completed_at timestamp additions

DROP INDEX IF EXISTS idx_job_tasks_cracking_completed_at;

ALTER TABLE job_executions DROP COLUMN IF EXISTS cracking_completed_at;
ALTER TABLE job_tasks DROP COLUMN IF EXISTS cracking_completed_at;
