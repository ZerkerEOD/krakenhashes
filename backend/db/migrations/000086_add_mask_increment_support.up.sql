-- Add increment support to preset_jobs table
ALTER TABLE preset_jobs
    ADD COLUMN increment_mode VARCHAR(20) DEFAULT 'off'
        CHECK (increment_mode IN ('off', 'increment', 'increment_inverse')),
    ADD COLUMN increment_min INTEGER CHECK (increment_min IS NULL OR increment_min > 0),
    ADD COLUMN increment_max INTEGER CHECK (increment_max IS NULL OR increment_max > 0);

-- Add increment support to job_executions table
ALTER TABLE job_executions
    ADD COLUMN increment_mode VARCHAR(20) DEFAULT 'off'
        CHECK (increment_mode IN ('off', 'increment', 'increment_inverse')),
    ADD COLUMN increment_min INTEGER CHECK (increment_min IS NULL OR increment_min > 0),
    ADD COLUMN increment_max INTEGER CHECK (increment_max IS NULL OR increment_max > 0);

-- Add helpful comments
COMMENT ON COLUMN preset_jobs.increment_mode IS 'Mask increment mode: off (default), increment (-i), increment_inverse (-ii)';
COMMENT ON COLUMN preset_jobs.increment_min IS 'Starting mask length for increment mode (--increment-min)';
COMMENT ON COLUMN preset_jobs.increment_max IS 'Maximum mask length for increment mode (--increment-max)';
COMMENT ON COLUMN job_executions.increment_mode IS 'Mask increment mode: off (default), increment (-i), increment_inverse (-ii)';
COMMENT ON COLUMN job_executions.increment_min IS 'Starting mask length for increment mode (--increment-min)';
COMMENT ON COLUMN job_executions.increment_max IS 'Maximum mask length for increment mode (--increment-max)';
