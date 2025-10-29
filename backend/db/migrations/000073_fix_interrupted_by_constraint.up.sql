-- Fix interrupted_by foreign key constraint to allow deletion by setting NULL
-- This allows hashlists/jobs to be deleted without being blocked by interrupt references

-- Drop the existing constraint
ALTER TABLE job_executions
DROP CONSTRAINT IF EXISTS job_executions_interrupted_by_fkey;

-- Recreate with ON DELETE SET NULL
ALTER TABLE job_executions
ADD CONSTRAINT job_executions_interrupted_by_fkey
FOREIGN KEY (interrupted_by)
REFERENCES job_executions(id)
ON DELETE SET NULL;
