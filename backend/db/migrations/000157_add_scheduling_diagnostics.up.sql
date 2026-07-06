-- Migration 000157: Scheduling diagnostics
-- A deduplicated, low-write store for "why isn't this agent/job working"
-- reasons (binary mismatch, blocklisted, outside schedule, no compatible job,
-- benchmarking, ...). One row per (scope, scope_id, reason_code); a recurring
-- reason bumps last_seen + count IN PLACE rather than inserting a new row, so
-- the scheduler hitting the same reason every cycle does not spam the table.
-- A buffered service flushes batches on size/time/shutdown and force-flushes
-- when the UI requests the data.

CREATE TABLE scheduling_diagnostics (
    id          BIGSERIAL    PRIMARY KEY,

    -- What the diagnostic is about.
    scope       VARCHAR(20)  NOT NULL,  -- 'agent' | 'job' | 'task'
    scope_id    VARCHAR(64)  NOT NULL,  -- agent id (int as text) or job/task uuid

    -- The machine-readable reason and human-readable specifics.
    reason_code VARCHAR(64)  NOT NULL,  -- e.g. 'no_compatible_job', 'blocklisted'
    severity    VARCHAR(20)  NOT NULL DEFAULT 'info',  -- 'info' | 'warning' | 'error'
    detail      TEXT,

    -- Dedup bookkeeping.
    count       BIGINT       NOT NULL DEFAULT 1,
    first_seen  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    cleared_at  TIMESTAMPTZ,            -- set when the condition resolves

    CONSTRAINT uq_scheduling_diag UNIQUE (scope, scope_id, reason_code)
);

-- Active (uncleared) diagnostics for a given scope — the hot read path.
CREATE INDEX idx_sched_diag_active
    ON scheduling_diagnostics (scope, scope_id)
    WHERE cleared_at IS NULL;

-- Recency ordering / retention sweeps.
CREATE INDEX idx_sched_diag_last_seen
    ON scheduling_diagnostics (last_seen DESC);

COMMENT ON TABLE scheduling_diagnostics IS
    'Deduplicated scheduler/agent diagnostic reasons (why idle / why failed). Written in batches by a buffered service to avoid per-cycle write spam.';
