-- Add custom_charsets JSONB column to preset_jobs and job_executions
-- Stores a map of charset slot ("1"-"4") to charset definition (e.g., {"1": "?u?d", "3": "?s?l"})

ALTER TABLE preset_jobs ADD COLUMN custom_charsets JSONB DEFAULT NULL;
ALTER TABLE job_executions ADD COLUMN custom_charsets JSONB DEFAULT NULL;
