-- Agent auto-update tracking fields.
--
-- The auto-update pipeline drives stale agents (those whose reported
-- version lags versions.json's `agent` value) to the latest binary via a
-- launcher/supervisor. These columns track an agent through that lifecycle:
--
--   update_pending        Agent is version-stale but currently BUSY. The
--                         readiness check flips it to status='updating' the
--                         moment it goes idle (and a concurrency slot frees).
--   target_version        The version this agent is being updated to. Bounds
--                         the in-flight update and lets the UI show "updating
--                         to X". Cleared on success.
--   update_started_at     When status flipped to 'updating'. The health-check
--                         sweeper fails the update if this exceeds the
--                         agent_update_health_timeout_seconds window without
--                         the agent reconnecting on target_version.
--   update_attempts       Incremented on each BeginUpdate. Drives the
--                         give-up policy (agent_update_max_attempts).
--   update_error          Last update failure message (health-check timeout,
--                         send failure, rollback). Surfaced in the UI.
--   update_last_attempt_at Base for exponential retry backoff.
--
-- 'updating' is a new value for the plain VARCHAR(50) agents.status column,
-- which has no CHECK constraint, so no constraint change is needed here.

ALTER TABLE agents
    ADD COLUMN update_pending         BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN target_version         TEXT,
    ADD COLUMN update_started_at      TIMESTAMPTZ,
    ADD COLUMN update_attempts        INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN update_error           TEXT,
    ADD COLUMN update_last_attempt_at TIMESTAMPTZ;

-- Health-check sweeper hot query: "which agents are mid-update?" Partial
-- index so only in-flight updates occupy index space.
CREATE INDEX idx_agents_update_started
    ON agents(update_started_at)
    WHERE update_started_at IS NOT NULL;
