-- Revert the loopback feature (GH #64).

DELETE FROM system_settings WHERE key = 'loopback_max_rounds';

DROP TABLE IF EXISTS loopback_session_plaintexts;
DROP TABLE IF EXISTS loopback_session_jobs;
DROP TRIGGER IF EXISTS update_loopback_sessions_updated_at ON loopback_sessions;
DROP TABLE IF EXISTS loopback_sessions;

ALTER TABLE job_workflow_steps DROP COLUMN IF EXISTS loopback_enabled;
ALTER TABLE job_workflows DROP COLUMN IF EXISTS loopback_all_eligible;
