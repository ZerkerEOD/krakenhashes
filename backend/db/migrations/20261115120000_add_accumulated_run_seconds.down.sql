-- Reverse 20261115120000_add_accumulated_run_seconds.

DROP TRIGGER IF EXISTS trg_accumulate_job_run_seconds ON job_executions;
DROP FUNCTION IF EXISTS accumulate_job_run_seconds();

ALTER TABLE job_executions DROP COLUMN IF EXISTS accumulated_run_seconds;
