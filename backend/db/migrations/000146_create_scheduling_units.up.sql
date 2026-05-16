-- Scheduling units are the new "atom of scheduling" for the rewrite. Each
-- row is one thing the dispatcher allocates agents to. A non-increment job
-- has exactly one row; an --increment 1-4 job has four rows (one per length).
-- This eliminates the originalID / entryID / actualJobID aliasing that drove
-- the c439089d, 2da634b6, and increment-layer bug cluster.
--
-- The dispatcher reads from this table exclusively. job_executions stays as
-- the user-facing identity for "a job"; scheduling_units is the internal
-- queue.
--
-- Cutover policy: pause + drain + cancel after 1 hour at deploy. No backfill
-- of in-flight job_executions is required — they're cancelled before this
-- migration matters.

CREATE TABLE scheduling_units (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_job_id          UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,

    -- 0 for non-increment jobs; 0..N-1 for the N sub-units of an increment
    -- job. (parent_job_id, layer_index) is unique so a job can't accidentally
    -- have two units at the same layer.
    layer_index            INT NOT NULL DEFAULT 0,

    status                 VARCHAR(50) NOT NULL DEFAULT 'pending',

    -- Priority and max_agents are snapshotted from the parent job at unit
    -- creation. They could in principle differ per layer; today they always
    -- match the parent. Storing them locally keeps the dispatcher's hot
    -- queries off the parent table.
    priority               INT NOT NULL,
    max_agents             INT NOT NULL DEFAULT 0,

    attack_mode            INT NOT NULL,

    -- Effective keyspace in BASE units (what --skip / --limit operate on).
    -- For increment-mode units, this is the per-length mask keyspace,
    -- computed at unit creation. Updated to actual value once a chunk's
    -- progress[1] arrives if the initial estimate was inaccurate.
    effective_keyspace     BIGINT NOT NULL DEFAULT 0,
    is_accurate_keyspace   BOOLEAN NOT NULL DEFAULT false,

    -- Attack-input refs. Stored as the same shape as job_executions' columns
    -- for the v1; populated from the parent at creation. Nullable because
    -- different attack modes use different combinations.
    --
    -- wordlist_refs is a TEXT[] so:
    --   - -a 0 / -a 9: typically one entry
    --   - -a 1 (combinator): two entries (dict1, dict2)
    --   - -a 6 (hybrid wl+mask): one entry plus mask_string
    --   - -a 7 (hybrid mask+wl): one entry plus mask_string
    -- The agent's payload already carries WordlistPaths as []string at
    -- hashcat_executor.go:72.
    --
    -- rule_file_refs is a TEXT[] so multiple -r flags can be
    -- stacked at hashcat invocation. The agent already iterates RulePaths[]
    -- at hashcat_executor.go:755-761 — this column supplies that list.
    -- Rule STACKING (cartesian product of N rule files) is a real feature;
    -- rule CHUNKING (splitting one rule file into pieces) is what the
    -- rewrite removes.
    wordlist_refs          TEXT[],
    rule_file_refs         TEXT[],
    mask_string            TEXT,
    custom_charsets        JSONB,

    retry_budget_remaining INT NOT NULL DEFAULT 5,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (parent_job_id, layer_index),
    CONSTRAINT valid_unit_status CHECK (
        status IN ('pending', 'running', 'completed', 'failed', 'cancelled')
    ),
    CONSTRAINT non_negative_keyspace CHECK (effective_keyspace >= 0)
);

-- Hot query: SelectSchedulableUnits — pending or running units, ordered by
-- priority DESC, created_at ASC. The partial index narrows to those rows.
CREATE INDEX idx_scheduling_units_dispatch
    ON scheduling_units (priority DESC, created_at ASC)
    WHERE status IN ('pending', 'running');

-- Cascade-from-parent queries (e.g., "is this whole job done?") read all
-- units for one parent.
CREATE INDEX idx_scheduling_units_parent
    ON scheduling_units (parent_job_id);

-- Touch updated_at on row updates, matching the codebase pattern from
-- migration 000019.
CREATE TRIGGER update_scheduling_units_updated_at
    BEFORE UPDATE ON scheduling_units
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
