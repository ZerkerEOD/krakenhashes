-- Backfill completed_at for jobs that have terminal status but NULL completed_at
-- This fixes job ordering in job lists (dashboard and /jobs page)
UPDATE job_executions
SET completed_at = COALESCE(updated_at, created_at)
WHERE status IN ('completed', 'failed', 'cancelled')
AND completed_at IS NULL;
