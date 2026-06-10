DROP INDEX IF EXISTS idx_agents_update_started;

ALTER TABLE agents
    DROP COLUMN IF EXISTS update_pending,
    DROP COLUMN IF EXISTS target_version,
    DROP COLUMN IF EXISTS update_started_at,
    DROP COLUMN IF EXISTS update_attempts,
    DROP COLUMN IF EXISTS update_error,
    DROP COLUMN IF EXISTS update_last_attempt_at;
