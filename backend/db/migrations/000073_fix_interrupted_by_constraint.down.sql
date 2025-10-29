-- Revert interrupted_by foreign key constraint to original behavior

-- Drop the SET NULL constraint
ALTER TABLE job_executions
DROP CONSTRAINT IF EXISTS job_executions_interrupted_by_fkey;

-- Recreate with original NO ACTION (prevents deletion)
ALTER TABLE job_executions
ADD CONSTRAINT job_executions_interrupted_by_fkey
FOREIGN KEY (interrupted_by)
REFERENCES job_executions(id)
ON DELETE NO ACTION;
