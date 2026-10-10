DROP INDEX IF EXISTS idx_hashes_cracked_recent;
DROP INDEX IF EXISTS idx_job_executions_workflow_id;
ALTER TABLE job_executions DROP COLUMN IF EXISTS workflow_id;
