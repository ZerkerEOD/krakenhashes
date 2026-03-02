-- Revert multiplication_factor back to INT
-- WARNING: This will fail if any existing values exceed INT max (2,147,483,647)

ALTER TABLE job_executions ALTER COLUMN multiplication_factor TYPE INT;
ALTER TABLE preset_jobs ALTER COLUMN multiplication_factor TYPE INT;
