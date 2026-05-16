-- job_keyspace_intervals replaces the dispatched_keyspace / processed_keyspace
-- watermark pair with an explicit interval set. Coverage of a scheduling_unit
-- is the union of its non-failed interval rows. Gaps are the complement of
-- that set within [0, effective_keyspace).
--
-- This is the single load-bearing data-model change for the rewrite:
--   - Disconnect with partial progress -> truncate the interval, the rest is
--     automatically a gap that the next dispatch cycle picks up.
--   - The user's #6 example (151-200 hole between two completed ranges) is
--     just a query over this table.
--   - Completion detection becomes "coverage equals effective_keyspace AND
--     every interval is status=completed" - no post-hoc structural-completion
--     scan, no rule-vs-salt unit-mismatch class of bugs.
--
-- The btree_gist extension is required for the no-overlap exclusion
-- constraint, which guarantees two concurrent dispatch cycles can't claim
-- overlapping ranges.

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE job_keyspace_intervals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scheduling_unit_id  UUID NOT NULL REFERENCES scheduling_units(id) ON DELETE CASCADE,

    -- Half-open range [range_start, range_end). Base units. range_end is
    -- exclusive so a 100-candidate chunk has range_end - range_start = 100
    -- with no off-by-one tax.
    range_start         BIGINT NOT NULL,
    range_end           BIGINT NOT NULL,

    status              VARCHAR(50) NOT NULL,

    -- task_id links the interval back to the job_tasks row that produced it.
    -- Nullable for two reasons: (a) the source task may be deleted while the
    -- interval is preserved as evidence of coverage, and (b) a "split" on
    -- recovery may leave the truncated portion's interval orphaned from any
    -- single task.
    task_id             UUID,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT no_zero_or_negative_range CHECK (range_end > range_start),
    CONSTRAINT non_negative_start         CHECK (range_start >= 0),
    CONSTRAINT valid_interval_status      CHECK (
        status IN ('assigned', 'running', 'completed', 'failed')
    )
);

-- Hot queries:
--   1. "Coverage of unit U" -> filter by scheduling_unit_id + status filter.
--   2. "Gaps in unit U"      -> needs range ordering as well.
CREATE INDEX idx_intervals_unit_status
    ON job_keyspace_intervals (scheduling_unit_id, status);

CREATE INDEX idx_intervals_unit_range
    ON job_keyspace_intervals (scheduling_unit_id, range_start, range_end);

-- Recovery query: find all intervals for a task that's being truncated.
CREATE INDEX idx_intervals_task
    ON job_keyspace_intervals (task_id)
    WHERE task_id IS NOT NULL;

-- The exclusion constraint enforces: no two non-failed intervals for the
-- same scheduling_unit may overlap. Failed intervals are excluded because
-- a retry needs to be allowed to cover the same range. int8range with
-- '[)' bound style matches our half-open semantics.
ALTER TABLE job_keyspace_intervals
    ADD CONSTRAINT no_overlap_per_unit EXCLUDE USING gist (
        scheduling_unit_id WITH =,
        int8range(range_start, range_end, '[)') WITH &&
    ) WHERE (status <> 'failed');

CREATE TRIGGER update_intervals_updated_at
    BEFORE UPDATE ON job_keyspace_intervals
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
