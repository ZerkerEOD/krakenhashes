-- Drop index
DROP INDEX IF EXISTS idx_job_tasks_increment_layer;

-- Remove increment_layer_id column from job_tasks
ALTER TABLE job_tasks
    DROP COLUMN IF EXISTS increment_layer_id;
