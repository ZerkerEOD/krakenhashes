-- Agent explicit revoke + mTLS per-agent certificate support.
--
-- Disable (status / is_enabled) is intentionally unchanged: it is a scheduler
-- pause and the agent's API key stays valid. Revoke is a separate, harder state
-- that invalidates credentials and retains the row for history/tracing.

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS is_revoked         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS revoked_at         TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS revoked_by         UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS client_cert_serial TEXT;

-- Revocation list consulted by the mTLS verify callback. A serial lands here
-- when an agent is revoked; re-issuing a new per-agent cert produces a new
-- serial, so old serials remain revoked unambiguously.
CREATE TABLE IF NOT EXISTS revoked_cert_serials (
    serial     TEXT PRIMARY KEY,
    agent_id   INTEGER REFERENCES agents(id) ON DELETE SET NULL,
    revoked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agents_client_cert_serial ON agents(client_cert_serial);
