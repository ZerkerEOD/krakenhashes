-- Addresses observed reaching this server that the certificate does not cover.
--
-- The backend runs on a Docker bridge network, so net.Interfaces() inside the
-- container only ever sees the 172.x bridge address -- never the host's LAN or
-- Tailscale address, which is exactly what an agent dials. Enumerating local
-- interfaces therefore cannot discover the addresses that matter. Instead we
-- record what actually arrives: inbound Host headers, TLS SNI, and explicit
-- failure reports from agents that could not complete a handshake.
--
-- Nothing here is ever applied automatically. A row is a suggestion an
-- administrator reviews; issuing a certificate always requires an explicit
-- action in the UI.
CREATE TABLE IF NOT EXISTS tls_san_candidates (
    id              BIGSERIAL PRIMARY KEY,

    -- Normalised at ingest: IPs through net.IP.String(), DNS names lowercased
    -- with any trailing dot stripped. This is half the dedup key, so
    -- normalisation has to happen before insert, not at read time.
    address         TEXT        NOT NULL,
    kind            VARCHAR(8)  NOT NULL CHECK (kind IN ('ip', 'dns')),

    -- Kept in the key rather than collapsed, because the sources carry very
    -- different weight. An agent_tls_failure row is a hard signal from an
    -- authenticated agent that cannot connect. A host_header row is attacker
    -- influenceable: anyone who can reach this server chooses what it contains.
    -- The UI has to be able to say which, so the same address can appear once
    -- per source and is grouped for display in Go.
    source          VARCHAR(24) NOT NULL
                    CHECK (source IN ('agent_tls_failure', 'host_header', 'tls_sni', 'manual')),

    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    hit_count       BIGINT      NOT NULL DEFAULT 1,

    last_agent_id   INT         REFERENCES agents(id) ON DELETE SET NULL,
    last_agent_name TEXT,
    last_user_agent TEXT,
    last_port       INT,

    dismissed_at    TIMESTAMPTZ,
    dismissed_by    UUID        REFERENCES users(id) ON DELETE SET NULL,

    UNIQUE (address, source)
);

CREATE INDEX IF NOT EXISTS idx_tls_san_candidates_active
    ON tls_san_candidates (last_seen_at DESC)
    WHERE dismissed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tls_san_candidates_agent_failures
    ON tls_san_candidates (last_seen_at DESC)
    WHERE dismissed_at IS NULL AND source = 'agent_tls_failure';
