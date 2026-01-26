DROP INDEX IF EXISTS idx_hashes_cracked_by_task_id;
ALTER TABLE hashes DROP COLUMN IF EXISTS cracked_by_task_id;
