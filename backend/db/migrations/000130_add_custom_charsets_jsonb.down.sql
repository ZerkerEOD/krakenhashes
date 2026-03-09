-- Remove custom_charsets JSONB column from preset_jobs and job_executions
ALTER TABLE preset_jobs DROP COLUMN IF EXISTS custom_charsets;
ALTER TABLE job_executions DROP COLUMN IF EXISTS custom_charsets;
