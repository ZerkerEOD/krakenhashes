-- Add cracking_completed_at timestamp to track when hashcat finishes work
-- This is separate from completed_at which tracks when processing finishes

-- Task level
ALTER TABLE job_tasks ADD COLUMN cracking_completed_at TIMESTAMPTZ;
COMMENT ON COLUMN job_tasks.cracking_completed_at IS 'Timestamp when hashcat finished for this task (enters processing state)';

-- Job level
ALTER TABLE job_executions ADD COLUMN cracking_completed_at TIMESTAMPTZ;
COMMENT ON COLUMN job_executions.cracking_completed_at IS 'Timestamp when all tasks finished hashcat processing (job enters processing state)';

-- Index for efficient querying of tasks by completion state
CREATE INDEX idx_job_tasks_cracking_completed_at ON job_tasks(cracking_completed_at) WHERE cracking_completed_at IS NOT NULL;
