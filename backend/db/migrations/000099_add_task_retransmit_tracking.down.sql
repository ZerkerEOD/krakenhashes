ALTER TABLE job_tasks DROP COLUMN IF EXISTS retransmit_count;
ALTER TABLE job_tasks DROP COLUMN IF EXISTS last_retransmit_at;
