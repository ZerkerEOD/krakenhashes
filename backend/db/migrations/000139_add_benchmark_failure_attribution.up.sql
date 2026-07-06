-- Benchmark failure attribution: track per-(agent, job, attack_mode, hash_type) failures
-- and a cooldown blocklist so the scheduler can stop retrying combos that clearly don't work.

-- Per-(agent, job, attack_mode, hash_type) failure counter.
-- One row per combination; upserted on each benchmark failure.
CREATE TABLE benchmark_failure_attempts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id          INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    job_execution_id  UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    attack_mode       INTEGER NOT NULL,
    hash_type         INTEGER NOT NULL,
    failure_count     INTEGER NOT NULL DEFAULT 0,
    first_failure_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_failure_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    UNIQUE (agent_id, job_execution_id, attack_mode, hash_type)
);

CREATE INDEX idx_bench_fail_attempts_job
    ON benchmark_failure_attempts(job_execution_id);

-- Active blocklist entries (cooldowns). Job-scoped when job_execution_id is non-null.
-- cleared_at = NULL AND expires_at > NOW() means the entry is active.
CREATE TABLE agent_benchmark_blocklist (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id          INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    job_execution_id  UUID REFERENCES job_executions(id) ON DELETE CASCADE,
    attack_mode       INTEGER NOT NULL,
    hash_type         INTEGER NOT NULL,
    reason            TEXT NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at        TIMESTAMPTZ,
    cleared_by        UUID REFERENCES users(id) ON DELETE SET NULL
);

-- Partial unique: only one active entry per (agent, job-or-global, combo)
-- Two separate indexes handle the NULL vs non-NULL job_execution_id cases since
-- PostgreSQL treats NULLs as distinct in a UNIQUE constraint.
CREATE UNIQUE INDEX idx_bench_blocklist_active_job_scoped
    ON agent_benchmark_blocklist(agent_id, job_execution_id, attack_mode, hash_type)
    WHERE cleared_at IS NULL AND job_execution_id IS NOT NULL;

CREATE UNIQUE INDEX idx_bench_blocklist_active_global
    ON agent_benchmark_blocklist(agent_id, attack_mode, hash_type)
    WHERE cleared_at IS NULL AND job_execution_id IS NULL;

-- Fast lookup: "is this (agent, job, combo) blocklisted right now?"
CREATE INDEX idx_bench_blocklist_lookup
    ON agent_benchmark_blocklist(agent_id, job_execution_id, attack_mode, hash_type, expires_at)
    WHERE cleared_at IS NULL;

-- UI lookup: list all entries for a job
CREATE INDEX idx_bench_blocklist_by_job
    ON agent_benchmark_blocklist(job_execution_id)
    WHERE cleared_at IS NULL;

-- Source tag on benchmark history so observed (EMA) samples can be distinguished
-- from explicit speed tests in audits.
ALTER TABLE agent_benchmark_history
    ADD COLUMN source TEXT NOT NULL DEFAULT 'speedtest';

COMMENT ON COLUMN agent_benchmark_history.source IS 'speedtest = explicit benchmark; observed_task = EMA update from task completion';

-- Configurable cooldown duration (hours) for blocklist entries.
INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'benchmark_blocklist_cooldown_hours',
    '24',
    'Hours to keep a (agent, hash_type, attack_mode) combination blocklisted after repeated benchmark failures before auto-retry.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;

-- Threshold: failures on the same (agent, job) before blocklisting when no other agent
-- has a successful benchmark for the combo (uncertain attribution).
INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'benchmark_failure_threshold',
    '3',
    'Number of benchmark failures on (agent, job) before blocklisting when attribution is ambiguous.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
