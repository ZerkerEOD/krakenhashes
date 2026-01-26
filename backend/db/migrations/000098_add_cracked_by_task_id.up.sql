-- Add task_id tracking for granular crack attribution
ALTER TABLE hashes ADD COLUMN cracked_by_task_id UUID REFERENCES job_tasks(id) ON DELETE SET NULL;

-- Index for efficient lookup of cracks per task (for retransmit deduplication)
CREATE INDEX idx_hashes_cracked_by_task_id ON hashes(cracked_by_task_id) WHERE cracked_by_task_id IS NOT NULL;

COMMENT ON COLUMN hashes.cracked_by_task_id IS 'The task that cracked this hash, for granular tracking and retransmit deduplication';
