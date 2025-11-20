-- Add 'processing' status to job_executions status CHECK constraint
-- Drop existing constraint
ALTER TABLE job_executions DROP CONSTRAINT IF EXISTS valid_status;

-- Add new constraint with 'processing' status
ALTER TABLE job_executions ADD CONSTRAINT valid_status
    CHECK (status IN ('pending', 'running', 'paused', 'processing', 'completed', 'failed', 'cancelled'));

-- Add 'processing' status to job_tasks status CHECK constraint
-- Drop existing constraint
ALTER TABLE job_tasks DROP CONSTRAINT IF EXISTS valid_task_status;

-- Add new constraint with 'processing' status
ALTER TABLE job_tasks ADD CONSTRAINT valid_task_status
    CHECK (status IN ('pending', 'assigned', 'reconnect_pending', 'running', 'processing', 'completed', 'failed', 'cancelled'));

-- Add crack tracking fields to job_tasks table
ALTER TABLE job_tasks ADD COLUMN IF NOT EXISTS expected_crack_count INTEGER DEFAULT 0;
ALTER TABLE job_tasks ADD COLUMN IF NOT EXISTS received_crack_count INTEGER DEFAULT 0;
ALTER TABLE job_tasks ADD COLUMN IF NOT EXISTS batches_complete_signaled BOOLEAN DEFAULT FALSE;

-- Add comments for documentation
COMMENT ON COLUMN job_tasks.expected_crack_count IS 'Number of cracks expected from progress message (for processing status tracking)';
COMMENT ON COLUMN job_tasks.received_crack_count IS 'Number of cracks received via crack_batch messages';
COMMENT ON COLUMN job_tasks.batches_complete_signaled IS 'Whether agent has signaled all crack batches sent';
