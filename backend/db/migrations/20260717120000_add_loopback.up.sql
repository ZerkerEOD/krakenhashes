-- Loopback feature (GH #64).
--
-- A "loopback" re-runs the *mutating* part of an attack (rules, or a hybrid mask)
-- against ONLY the newly-cracked plaintexts (the delta), repeating until a round
-- produces no new plaintext. It is orchestrated by a durable controller so a
-- waiting session survives a backend restart (unlike the transient 'preparing'
-- job state, which is failed on restart).
--
-- Config lives on the workflow / at job-start time, never on the preset definition:
--   * job_workflows.loopback_all_eligible  master toggle: loopback every eligible
--     step. When true, the per-step flags are ignored.
--   * job_workflow_steps.loopback_enabled  per-step toggle (used only when the
--     workflow master toggle is off).
-- Standalone preset/custom runs carry the toggle in the create-job request.

ALTER TABLE job_workflows
    ADD COLUMN IF NOT EXISTS loopback_all_eligible BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE job_workflow_steps
    ADD COLUMN IF NOT EXISTS loopback_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- One row per loopback "session": a workflow run, or a standalone preset/custom run.
CREATE TABLE IF NOT EXISTS loopback_sessions (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    hashlist_id        BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    source_type        TEXT NOT NULL CHECK (source_type IN ('workflow', 'preset', 'custom')),
    source_workflow_id UUID REFERENCES job_workflows(id) ON DELETE SET NULL,
    name               TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'waiting'
                         CHECK (status IN ('waiting', 'active', 'completed', 'failed', 'cancelled')),
    current_round      INTEGER NOT NULL DEFAULT 0,
    max_rounds         INTEGER NOT NULL DEFAULT 10,
    error_message      TEXT,
    created_by         UUID REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only 'waiting'/'active' sessions need monitoring; a partial index keeps the
-- monitor's idle-gate (EXISTS active session) cheap.
CREATE INDEX IF NOT EXISTS idx_loopback_sessions_active
    ON loopback_sessions(status) WHERE status IN ('waiting', 'active');
CREATE INDEX IF NOT EXISTS idx_loopback_sessions_hashlist
    ON loopback_sessions(hashlist_id);

CREATE TRIGGER update_loopback_sessions_updated_at
BEFORE UPDATE ON loopback_sessions
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- Links each job_execution that belongs to a session, per round.
--   round 0       = the original jobs (created by the normal create-job flow)
--   round >= 1    = re-run jobs the controller spawns against the delta
--   role          = 'original' | 'rerun'
--   is_mutatable  = TRUE if this job's attack is re-run against the delta
--                   (straight+rules, hybrid 6/7); FALSE otherwise (bruteforce,
--                   wordlist-only, association) — those only feed the delta pool
--   origin_job_id = the round-0 job this lineage descends from (NULL on originals)
CREATE TABLE IF NOT EXISTS loopback_session_jobs (
    id               BIGSERIAL PRIMARY KEY,
    session_id       UUID NOT NULL REFERENCES loopback_sessions(id) ON DELETE CASCADE,
    job_execution_id UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    round            INTEGER NOT NULL,
    role             TEXT NOT NULL CHECK (role IN ('original', 'rerun')),
    is_mutatable     BOOLEAN NOT NULL DEFAULT FALSE,
    origin_job_id    UUID REFERENCES job_executions(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_id, job_execution_id)
);

CREATE INDEX IF NOT EXISTS idx_loopback_session_jobs_session
    ON loopback_session_jobs(session_id);
CREATE INDEX IF NOT EXISTS idx_loopback_session_jobs_job
    ON loopback_session_jobs(job_execution_id);

-- Guard set of plaintexts already fed as loopback input for a session (md5 of the
-- plaintext to stay compact). Guarantees each distinct plaintext is used exactly
-- once, so the loop terminates and re-runs never repeat already-tried candidates.
CREATE TABLE IF NOT EXISTS loopback_session_plaintexts (
    session_id    UUID NOT NULL REFERENCES loopback_sessions(id) ON DELETE CASCADE,
    plaintext_md5 TEXT NOT NULL,
    PRIMARY KEY (session_id, plaintext_md5)
);

-- Admin-configurable safety cap on how many delta rounds a loopback session runs.
INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('loopback_max_rounds', '10',
     'Safety cap on how many delta rounds a loopback session runs before it stops, even if new cracks keep appearing. Applied when a session is created.',
     'integer')
ON CONFLICT (key) DO NOTHING;
