-- Cloud GPU provisioning: rent ephemeral GPU instances from Vast.ai or AWS,
-- run the KrakenHashes agent on them, and charge the spend to a client budget.
--
-- The governing constraint is cost safety. A rented GPU costs $0.50-$22/hr and
-- neither provider will stop it for you, so the schema is built around two
-- ideas:
--
--   1. RESERVATION accounting. Spend is committed at launch, not measured
--      after the fact, so "a $10 budget" cannot be exceeded by an instance
--      that is already running. cloud_spend_ledger is the append-only record.
--
--   2. LABEL-based reconciliation. cloud_instances.label is written BEFORE the
--      provider API is called, so a crash or a lost response still leaves a
--      row that the reaper can match against provider inventory. Vast.ai has
--      no idempotency token at all, so the label IS the idempotency key.

-- ---------------------------------------------------------------------------
-- Provider configuration
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS cloud_provider_configs (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider                  VARCHAR(32)  NOT NULL CHECK (provider IN ('vastai', 'aws', 'mock')),
    name                      VARCHAR(255) NOT NULL UNIQUE,
    enabled                   BOOLEAN      NOT NULL DEFAULT false,

    -- AES-256-GCM via internal/crypto. Never returned by the API (json:"-").
    credentials_encrypted     TEXT,
    -- Provider-specific, non-secret settings: AWS region/subnet/instance
    -- profile, Vast.ai offer filters. JSONB so each adapter owns its shape.
    settings                  JSONB        NOT NULL DEFAULT '{}'::jsonb,

    -- Ceilings enforced before the budget is even consulted.
    max_concurrent_instances  INT          NOT NULL DEFAULT 0,
    max_instance_hourly_cents INT          NOT NULL DEFAULT 0,

    -- VPN enrollment. We join the operator's EXISTING network; we never build
    -- one. vpn_credential_kind decides whether we mint a short-lived key per
    -- instance (oauth/pat) or reuse an operator-supplied one.
    vpn_provider              VARCHAR(32)  CHECK (vpn_provider IN ('tailscale', 'netbird', 'wireguard')),
    vpn_credential_kind       VARCHAR(32)  CHECK (vpn_credential_kind IN ('oauth', 'pat', 'reusable_key', 'static_config')),
    vpn_credential_encrypted  TEXT,
    vpn_credential_expires_at TIMESTAMPTZ,
    vpn_tag_or_group          VARCHAR(255),
    -- The address cloud agents use for KH_HOST. May differ from what on-prem
    -- agents use, and MUST be covered by the server certificate's SANs
    -- (KH_ADDITIONAL_DNS_NAMES / KH_ADDITIONAL_IP_ADDRESSES) or WSS fails
    -- hostname verification.
    backend_vpn_host          VARCHAR(255),

    -- Vast.ai rents GPUs on individually-owned machines whose operators have
    -- root over the container. Enabling it requires an explicit, attributed
    -- acknowledgement that hashes and potfiles leave the perimeter.
    third_party_ack_at        TIMESTAMPTZ,
    third_party_ack_by        UUID REFERENCES users(id) ON DELETE SET NULL,

    created_at                TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE cloud_provider_configs IS 'Admin-configured cloud GPU providers. Credentials are AES-256-GCM encrypted via internal/crypto.';
COMMENT ON COLUMN cloud_provider_configs.max_concurrent_instances IS '0 means unlimited (still bounded by budget and provider quota).';
COMMENT ON COLUMN cloud_provider_configs.backend_vpn_host IS 'KH_HOST value handed to cloud agents. Must appear in the server certificate SANs.';

CREATE INDEX IF NOT EXISTS idx_cloud_provider_configs_enabled
    ON cloud_provider_configs (provider) WHERE enabled = true;

-- ---------------------------------------------------------------------------
-- Per-client budget and consent
-- ---------------------------------------------------------------------------

ALTER TABLE clients
    ADD COLUMN IF NOT EXISTS cloud_enabled            BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS cloud_provider_allowlist TEXT[]  NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS cloud_budget_cents       BIGINT,
    ADD COLUMN IF NOT EXISTS max_instance_ttl_minutes INT,
    ADD COLUMN IF NOT EXISTS provider_ack             JSONB   NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN clients.cloud_provider_allowlist IS
    'Providers this client may burst to. EMPTY BY DEFAULT: a client must be explicitly opted in to each provider. Allowing aws while forbidding vastai is a first-class configuration, because AWS runs in the operator''s own account and Vast.ai does not.';
COMMENT ON COLUMN clients.cloud_budget_cents IS 'Spend ceiling for the current budget window. NULL means cloud burst is not funded for this client.';
COMMENT ON COLUMN clients.provider_ack IS 'Per-provider data-exposure acknowledgement: {"vastai": {"at": "...", "by": "<user uuid>"}}.';

-- ---------------------------------------------------------------------------
-- Budget policy: what happens as spend approaches the cap
-- ---------------------------------------------------------------------------
--
-- One row with client_id IS NULL is the system default; a row with a client_id
-- overrides it for that client. Every threshold is admin-configurable because
-- "stop at 99%, hard stop at 100%, never notify" is a legitimate policy and so
-- is "warn me at 50%".
--
-- allow_overage=false (the default) means the cap is absolute. Note that the
-- ladder only decides how GRACEFULLY the cap is reached; what actually
-- prevents exceeding it is reservation accounting in cloud_spend_ledger.

CREATE TABLE IF NOT EXISTS cloud_budget_policies (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id             UUID UNIQUE REFERENCES clients(id) ON DELETE CASCADE,

    notify_pct            INT,
    stop_provision_pct    INT     NOT NULL DEFAULT 95,
    drain_pct             INT     NOT NULL DEFAULT 99,
    hard_stop_pct         INT     NOT NULL DEFAULT 100,
    allow_overage         BOOLEAN NOT NULL DEFAULT false,
    drain_timeout_seconds INT     NOT NULL DEFAULT 300,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT cloud_budget_policies_pct_ordering CHECK (
        (notify_pct IS NULL OR (notify_pct > 0 AND notify_pct <= stop_provision_pct))
        AND stop_provision_pct <= drain_pct
        AND drain_pct <= hard_stop_pct
    )
);

COMMENT ON TABLE cloud_budget_policies IS 'Spend threshold ladder. The row with client_id IS NULL is the system default.';
COMMENT ON COLUMN cloud_budget_policies.notify_pct IS 'NULL disables threshold notifications entirely.';
COMMENT ON COLUMN cloud_budget_policies.allow_overage IS 'false makes the cap absolute: reservations may not exceed remaining budget.';

-- Only one system-default row may exist (client_id IS NULL is not covered by
-- the UNIQUE constraint above, since NULLs are distinct).
CREATE UNIQUE INDEX IF NOT EXISTS idx_cloud_budget_policies_system_default
    ON cloud_budget_policies ((client_id IS NULL)) WHERE client_id IS NULL;

INSERT INTO cloud_budget_policies (client_id, notify_pct, stop_provision_pct, drain_pct, hard_stop_pct, allow_overage, drain_timeout_seconds)
VALUES (NULL, 80, 95, 99, 100, false, 300)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Rented instances
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS cloud_instances (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_config_id    UUID NOT NULL REFERENCES cloud_provider_configs(id) ON DELETE RESTRICT,

    -- label is written before the provider call and is the reconciliation key.
    -- provider_instance_id stays NULL until the launch response comes back.
    label                 VARCHAR(255) NOT NULL UNIQUE,
    idempotency_key       VARCHAR(255) NOT NULL UNIQUE,
    provider_instance_id  VARCHAR(255),

    -- SET NULL rather than CASCADE: deleting a job must never orphan a live
    -- rented instance by deleting the only row that knows it exists.
    agent_id              INT  REFERENCES agents(id) ON DELETE SET NULL,
    job_execution_id      UUID REFERENCES job_executions(id) ON DELETE SET NULL,
    client_id             UUID REFERENCES clients(id) ON DELETE SET NULL,
    -- Denormalized so historical spend stays attributable after a client is
    -- deleted and client_id goes NULL.
    client_name_snapshot  VARCHAR(255),

    state                 VARCHAR(32) NOT NULL DEFAULT 'requested'
        CHECK (state IN ('requested','launching','provisioning','syncing','running','draining','terminating','terminated','failed')),

    gpu_model             VARCHAR(255),
    gpu_count             INT,
    hourly_rate_cents     INT  NOT NULL DEFAULT 0,
    disk_gb               INT,
    -- Total bytes of the job's file set. Drives disk_gb, which is IMMUTABLE on
    -- Vast.ai and fixed at launch on AWS.
    fileset_bytes         BIGINT,

    reserved_cents        BIGINT NOT NULL DEFAULT 0,
    estimated_cost_cents  BIGINT NOT NULL DEFAULT 0,
    actual_cost_cents     BIGINT,

    -- Deadlines. A row past launch_deadline_at with no provider_instance_id
    -- forces label reconciliation; past ready_deadline_at with no agent_id is
    -- destroyed. ttl_epoch is the absolute kill time, also armed in-guest.
    launch_deadline_at    TIMESTAMPTZ,
    ready_deadline_at     TIMESTAMPTZ,
    ttl_epoch             TIMESTAMPTZ,

    launched_at           TIMESTAMPTZ,
    ready_at              TIMESTAMPTZ,
    terminated_at         TIMESTAMPTZ,
    termination_reason    TEXT,
    terminate_attempts    INT NOT NULL DEFAULT 0,
    last_terminate_error  TEXT,

    vpn_credential_ref    VARCHAR(255),
    provider_raw          JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE cloud_instances IS 'Ephemeral rented GPU instances. A row exists BEFORE the provider is called so a lost response is still reconcilable.';
COMMENT ON COLUMN cloud_instances.label IS 'Provider-side label/tag (kh-<uuid>). The reconciliation key, and the idempotency key on Vast.ai, which has none.';
COMMENT ON COLUMN cloud_instances.ttl_epoch IS 'Absolute kill time. Also armed inside the instance so it dies even if the backend is gone.';
COMMENT ON COLUMN cloud_instances.terminate_attempts IS 'Consecutive failed teardowns. Past a threshold this raises cloud_teardown_failed: money is actively burning.';

-- Partial index over instances the reaper cares about: anything not yet
-- terminated is a potential cost leak.
CREATE INDEX IF NOT EXISTS idx_cloud_instances_live
    ON cloud_instances (state, ttl_epoch)
    WHERE state NOT IN ('terminated', 'failed');
CREATE INDEX IF NOT EXISTS idx_cloud_instances_job      ON cloud_instances (job_execution_id);
CREATE INDEX IF NOT EXISTS idx_cloud_instances_client   ON cloud_instances (client_id);
CREATE INDEX IF NOT EXISTS idx_cloud_instances_agent    ON cloud_instances (agent_id);

-- ---------------------------------------------------------------------------
-- Append-only spend ledger
-- ---------------------------------------------------------------------------
--
-- available = cap - incurred - outstanding_reservations
--
--   reservation    committed at launch (rate x ttl). Makes the cap real.
--   incurred       converted from a reservation as wall-clock accrues.
--   release        unused remainder returned when an instance terminates early.
--   reconciliation provider-authoritative correction (AWS Cost Explorer lags
--                  ~24h). MAY BE NEGATIVE, and must not retroactively free
--                  budget that earlier decisions already relied on.
--
-- Budget windows are computed on read (recorded_at >= date_trunc('month', now()))
-- because there is no cron infrastructure here to roll a period over.

CREATE TABLE IF NOT EXISTS cloud_spend_ledger (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id         UUID REFERENCES clients(id) ON DELETE SET NULL,
    job_execution_id  UUID REFERENCES job_executions(id) ON DELETE SET NULL,
    cloud_instance_id UUID REFERENCES cloud_instances(id) ON DELETE SET NULL,
    cents             BIGINT NOT NULL,
    kind              VARCHAR(32) NOT NULL
        CHECK (kind IN ('reservation','incurred','release','reconciliation')),
    note              TEXT,
    recorded_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE cloud_spend_ledger IS 'Append-only spend record. Reservation accounting is what makes a $10 budget mean $10.';
COMMENT ON COLUMN cloud_spend_ledger.cents IS 'Signed. reconciliation entries may be negative.';

CREATE INDEX IF NOT EXISTS idx_cloud_spend_ledger_client_time
    ON cloud_spend_ledger (client_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_cloud_spend_ledger_instance
    ON cloud_spend_ledger (cloud_instance_id);

-- ---------------------------------------------------------------------------
-- Observed GPU speeds, for PRE-LAUNCH ESTIMATION ONLY
-- ---------------------------------------------------------------------------
--
-- This answers "how fast is an RTX 4090 on -m 1000, so what will this cost and
-- will it finish before the cap". It is deliberately NOT written into
-- agent_benchmarks: a synthetic row there would make
-- CountAgentsWithRecentBenchmark treat an invented number as corroborating
-- evidence and could quarantine real on-prem hardware. Cloud agents run a real
-- benchmark like everyone else.

CREATE TABLE IF NOT EXISTS cloud_gpu_benchmarks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     VARCHAR(32)  NOT NULL,
    gpu_model    VARCHAR(255) NOT NULL,
    gpu_count    INT          NOT NULL DEFAULT 1,
    attack_mode  INT          NOT NULL,
    hash_type    INT          NOT NULL,
    salt_count   INT,
    speed        BIGINT       NOT NULL,
    sample_count INT          NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT cloud_gpu_benchmarks_unique
        UNIQUE NULLS NOT DISTINCT (provider, gpu_model, gpu_count, attack_mode, hash_type, salt_count)
);

COMMENT ON TABLE cloud_gpu_benchmarks IS 'EWMA of observed cloud-agent speeds, keyed by GPU model. Read only by the cost/ETA estimator; never written to agent_benchmarks.';

-- ---------------------------------------------------------------------------
-- Agent and job wiring
-- ---------------------------------------------------------------------------

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS cloud_instance_id UUID REFERENCES cloud_instances(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS retired_at        TIMESTAMPTZ;

COMMENT ON COLUMN agents.cloud_instance_id IS
    'Set INSIDE the registration INSERT (not afterwards, which would be a TOCTOU window). Non-NULL marks the agent ephemeral: it is locked to one job, skips the full-corpus file sync, and is exempt from the offline monitor.';
COMMENT ON COLUMN agents.retired_at IS
    'Soft retirement for finished cloud agents. Preferred over deletion (which NULLs job_tasks.agent_id and destroys cost attribution) and over is_enabled=false (which the compat cache and offline monitor mishandle).';

CREATE INDEX IF NOT EXISTS idx_agents_cloud_instance ON agents (cloud_instance_id) WHERE cloud_instance_id IS NOT NULL;

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS cloud_burst_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS cloud_max_instances INT;

COMMENT ON COLUMN job_executions.cloud_burst_enabled IS 'Opt-in. Nothing bursts to paid capacity by accident.';
COMMENT ON COLUMN job_executions.cloud_max_instances IS
    'Cloud concurrency cap, SEPARATE from max_agents. max_agents governs the shared on-prem pool (fleet fairness); a rented instance is dedicated to this job and paid for by its client, so it must not consume that budget. NULL derives the cap from remaining budget.';

ALTER TABLE preset_jobs
    ADD COLUMN IF NOT EXISTS cloud_burst_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS cloud_max_instances INT;

ALTER TABLE job_workflows
    ADD COLUMN IF NOT EXISTS cloud_burst_enabled BOOLEAN NOT NULL DEFAULT false;

-- ---------------------------------------------------------------------------
-- System settings
-- ---------------------------------------------------------------------------
--
-- Seeded here because SystemSettingsRepository.SetSetting is UPDATE-only:
-- a key that was never inserted can never be written.

INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('cloud_global_monthly_cap_cents', '0',
     'System-wide monthly cloud spend ceiling in cents, across all clients. 0 disables cloud provisioning entirely.', 'integer'),
    ('cloud_global_concurrent_instance_cap', '0',
     'Maximum rented instances across all providers and clients at once. 0 means unlimited (still bounded by budget and provider quotas).', 'integer'),
    ('cloud_chunk_duration_seconds', '3600',
     'Target chunk duration for cloud agents, ~3x the on-prem default_chunk_duration. Per-chunk overhead (hashcat startup, kernel autotune, wordlist load) is billed at rental rates. Always clamped to the instance''s remaining TTL.', 'integer'),
    ('cloud_teardown_slack_seconds', '120',
     'Reserved at the end of an instance TTL for drain and destroy, so a chunk is never planned past the instance''s life.', 'integer'),
    ('cloud_idle_drain_minutes', '5',
     'Destroy a rented instance after this many minutes with no pending work. Bounds the tail-idle waste.', 'integer'),
    ('cloud_ttl_extension_max_pct', '25',
     'Maximum TTL extension when an instance is mid-chunk and would finish the job. Requires a fresh budget reservation and never crosses hard_stop_pct.', 'integer'),
    ('cloud_reaper_interval_seconds', '60',
     'How often to reconcile the database against provider inventory and destroy anything that should not exist.', 'integer'),
    ('cloud_orphan_grace_minutes', '10',
     'Destroy instances found on the provider account with no matching cloud_instances row after this long. The only recovery for a launch whose label never landed.', 'integer'),
    ('scheduler_endgame_tapering_enabled', 'true',
     'Near job completion, size every agent''s chunk speed-proportionally so agents finish together. Prevents a rented GPU idling (and billing) while a slow on-prem agent finishes a long chunk. Applies fleet-wide because the on-prem chunk is the cause.', 'boolean'),
    ('endgame_threshold_multiple', '2',
     'Remaining work below this multiple of the chunk duration switches a unit into endgame tapering.', 'integer')
ON CONFLICT (key) DO NOTHING;
