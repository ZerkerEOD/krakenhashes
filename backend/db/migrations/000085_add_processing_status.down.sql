-- Remove crack tracking columns from job_tasks
ALTER TABLE job_tasks DROP COLUMN IF EXISTS batches_complete_signaled;
ALTER TABLE job_tasks DROP COLUMN IF EXISTS received_crack_count;
ALTER TABLE job_tasks DROP COLUMN IF EXISTS expected_crack_count;

-- Restore original job_executions status CHECK constraint (without 'processing')
ALTER TABLE job_executions DROP CONSTRAINT IF EXISTS valid_status;
ALTER TABLE job_executions ADD CONSTRAINT valid_status
    CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled'));

-- Restore original job_tasks status CHECK constraint (without 'processing')
ALTER TABLE job_tasks DROP CONSTRAINT IF EXISTS valid_task_status;
ALTER TABLE job_tasks ADD CONSTRAINT valid_task_status
    CHECK (status IN ('pending', 'assigned', 'reconnect_pending', 'running', 'completed', 'failed', 'cancelled'));
