-- Rollback Binary Version Pattern Matching Migration
-- Note: Cannot perfectly restore INT IDs from version strings

BEGIN;

-- Drop indexes first
DROP INDEX IF EXISTS idx_job_tasks_binary_version;
DROP INDEX IF EXISTS idx_binary_versions_version;

-- Restore agents columns
ALTER TABLE agents ADD COLUMN binary_version_id INTEGER;
ALTER TABLE agents ADD COLUMN binary_override BOOLEAN DEFAULT false;
-- Note: Cannot perfectly restore INT IDs from version strings, set to NULL
ALTER TABLE agents DROP COLUMN IF EXISTS binary_version;

-- Restore preset_jobs
ALTER TABLE preset_jobs ADD COLUMN binary_version_id INTEGER;
ALTER TABLE preset_jobs DROP COLUMN IF EXISTS binary_version;

-- Restore job_executions
ALTER TABLE job_executions ADD COLUMN binary_version_id INTEGER;
ALTER TABLE job_executions DROP COLUMN IF EXISTS binary_version;

-- Remove job_tasks column
ALTER TABLE job_tasks DROP COLUMN IF EXISTS binary_version_id;

COMMIT;
