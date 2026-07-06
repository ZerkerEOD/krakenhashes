-- Network grace period for hard WebSocket disconnects.
--
-- When an agent's WebSocket connection drops (network blip, agent process
-- killed, host reboot), the new scheduler's sweeper needs a way to
-- distinguish "transient blip, agent will reconnect" from "agent is gone,
-- redispatch its tasks." disconnect_grace_expires_at is set when the
-- WebSocket OnClose handler fires and is cleared on reconnect. The
-- sweeper evicts the agent's running tasks when the grace expires.
--
-- network_grace_seconds (system_setting from migration 000149) is added
-- to NOW() at disconnect time to produce this value. Nullable: NULL
-- means "not currently disconnected" (the steady state).

ALTER TABLE agents
    ADD COLUMN disconnect_grace_expires_at TIMESTAMPTZ;

-- Sweeper hot query: "which agents are past their grace?" — partial
-- index so only the currently-disconnected agents take up index space.
CREATE INDEX idx_agents_disconnect_grace
    ON agents(disconnect_grace_expires_at)
    WHERE disconnect_grace_expires_at IS NOT NULL;
