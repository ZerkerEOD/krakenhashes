-- Frontend redesign: entity linking and dashboard support.
--
-- 1. job_executions.workflow_id records which workflow (if any) created a job, so
--    the job list/detail can link back to it. Previously the only trace was an
--    indirect hop through loopback sessions, which only exist for loopback runs.
-- 2. A partial index for "recent cracks": the dashboard feed orders cracked hashes
--    by last_updated, and hashes is by far the largest table.

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS workflow_id UUID REFERENCES job_workflows(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_job_executions_workflow_id
    ON job_executions(workflow_id) WHERE workflow_id IS NOT NULL;

COMMENT ON COLUMN job_executions.workflow_id IS 'Workflow that created this job (NULL for preset/custom jobs); for linking only';

CREATE INDEX IF NOT EXISTS idx_hashes_cracked_recent
    ON hashes (last_updated DESC) WHERE is_cracked = true;
