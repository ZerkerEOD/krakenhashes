-- Roll back the new job_tasks columns. The constraints drop with the
-- columns but are listed for clarity.

DROP INDEX IF EXISTS idx_job_tasks_activity_status;
DROP INDEX IF EXISTS idx_job_tasks_scheduling_unit;

ALTER TABLE job_tasks
    DROP CONSTRAINT IF EXISTS new_restore_within_range,
    DROP CONSTRAINT IF EXISTS new_range_non_negative,
    DROP CONSTRAINT IF EXISTS new_range_positive;

ALTER TABLE job_tasks
    DROP COLUMN IF EXISTS failure_reason,
    DROP COLUMN IF EXISTS last_activity_at,
    DROP COLUMN IF EXISTS restore_point,
    DROP COLUMN IF EXISTS range_end,
    DROP COLUMN IF EXISTS range_start,
    DROP COLUMN IF EXISTS scheduling_unit_id;
