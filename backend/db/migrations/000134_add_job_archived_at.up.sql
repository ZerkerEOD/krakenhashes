ALTER TABLE job_executions ADD COLUMN archived_at TIMESTAMPTZ DEFAULT NULL;
CREATE INDEX idx_job_executions_archived_at ON job_executions(archived_at);
