-- Network-share storage (feature/network-share-storage, WS2).
--
-- Holds the single, server-global network-share configuration plus the
-- active storage backend and live migration state. There is at most ONE
-- row (enforced by the `singleton` unique/CHECK trick), because a
-- KrakenHashes server has exactly one wordlist/rule storage backend.
--
-- Deliberately NON-SECRET: the server mounts the share via docker-compose
-- (its credentials live in compose, not here) and network_direct agents
-- receive their credentials from the operator via a templated setup
-- command — the server never stores or distributes share credentials.
-- share_type/server_host/share_name/mount_options exist only to pre-fill
-- that templated command and to validate the server-side mount.
--
-- The server-side mount PATH is intentionally not stored here: it is an
-- infrastructure concern provided by the KH_SHARE_DIR environment variable
-- (the compose mount target), so the DB can never disagree with the mount.
CREATE TABLE IF NOT EXISTS network_shares (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Singleton guard: DEFAULT true + UNIQUE + CHECK means at most one row
    -- can ever exist. Repo upserts target this single row.
    singleton             BOOLEAN     NOT NULL DEFAULT true UNIQUE CHECK (singleton),

    share_type            VARCHAR(32) NOT NULL DEFAULT 'smb' CHECK (share_type IN ('smb', 'nfs')),
    name                  VARCHAR(255) NOT NULL DEFAULT 'Network Share',
    enabled               BOOLEAN     NOT NULL DEFAULT false,

    -- Non-secret connection info, used to generate the agent setup command
    -- and to validate the server mount. server_host is typically the VPN
    -- address of the share host; share_name is the SMB share or NFS export.
    server_host           VARCHAR(255) NOT NULL DEFAULT '',
    share_name            VARCHAR(255) NOT NULL DEFAULT '',
    mount_options         JSONB        NOT NULL DEFAULT '{}'::jsonb,

    -- Server-global storage backend. 'local' = wordlists/rules served from
    -- the local data dir (today's behavior); 'share' = served from the
    -- compose-mounted share (KH_SHARE_DIR). Flipped only by a successful
    -- migration. The runtime storage-path resolver reads this.
    storage_backend       VARCHAR(16) NOT NULL DEFAULT 'local' CHECK (storage_backend IN ('local', 'share')),

    -- Live migration lifecycle for the maintenance-mode UI.
    migration_state       VARCHAR(32) NOT NULL DEFAULT 'idle'
                              CHECK (migration_state IN ('idle', 'draining', 'migrating', 'validating', 'completed', 'failed')),
    migration_direction   VARCHAR(16) CHECK (migration_direction IN ('to_share', 'to_local')),
    migration_started_at  TIMESTAMPTZ,
    migration_finished_at TIMESTAMPTZ,
    migration_error       TEXT,

    -- Server-side mount validation results (Validate action).
    last_validated_at     TIMESTAMPTZ,
    last_validation_error TEXT,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE network_shares IS
    'Singleton network-share config + active storage backend + migration state. Non-secret; server mounts via compose, agents get creds from the operator.';
