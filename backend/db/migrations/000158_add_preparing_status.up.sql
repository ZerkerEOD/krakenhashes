-- Add 'preparing' status to job_executions (GH #40).
-- A job is 'preparing' while its inputs are generated (ephemeral filtered
-- wordlist) before it becomes schedulable. It is never a job_tasks status.

ALTER TABLE job_executions DROP CONSTRAINT IF EXISTS valid_status;
ALTER TABLE job_executions ADD CONSTRAINT valid_status
    CHECK (status IN ('preparing', 'pending', 'running', 'paused', 'processing', 'completed', 'failed', 'cancelled'));
