-- Revert 'preparing' status (GH #40). Move any leftover preparing jobs to failed
-- first so the stricter constraint can be applied.
UPDATE job_executions SET status = 'failed' WHERE status = 'preparing';

ALTER TABLE job_executions DROP CONSTRAINT IF EXISTS valid_status;
ALTER TABLE job_executions ADD CONSTRAINT valid_status
    CHECK (status IN ('pending', 'running', 'paused', 'processing', 'completed', 'failed', 'cancelled'));
