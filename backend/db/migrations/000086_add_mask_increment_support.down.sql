-- Remove increment support from preset_jobs table
ALTER TABLE preset_jobs
    DROP COLUMN increment_mode,
    DROP COLUMN increment_min,
    DROP COLUMN increment_max;

-- Remove increment support from job_executions table
ALTER TABLE job_executions
    DROP COLUMN increment_mode,
    DROP COLUMN increment_min,
    DROP COLUMN increment_max;
