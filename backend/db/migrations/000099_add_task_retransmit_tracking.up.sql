-- Add columns for better task lifecycle tracking
ALTER TABLE job_tasks ADD COLUMN retransmit_count INTEGER DEFAULT 0;
ALTER TABLE job_tasks ADD COLUMN last_retransmit_at TIMESTAMPTZ;

COMMENT ON COLUMN job_tasks.retransmit_count IS 'Number of crack retransmission attempts';
COMMENT ON COLUMN job_tasks.last_retransmit_at IS 'Timestamp of last retransmission request';
