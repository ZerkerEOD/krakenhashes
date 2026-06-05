-- Phase A additive change: introduce the new task columns alongside the
-- existing schema. Existing columns (keyspace_start, keyspace_end,
-- keyspace_processed, rule_start_index, rule_end_index, is_keyspace_split,
-- detailed_status, effective_keyspace_*, chunk_actual_keyspace,
-- is_actual_keyspace) stay in place; later phases stop writing them, and
-- migration 000150 drops them after at least one stable release.
--
-- The new columns:
--   - scheduling_unit_id: replaces the implicit "task belongs to the job"
--     link with an explicit "task belongs to a scheduling unit" link. A
--     unit may be one of N increment sub-units, not always the parent job.
--   - range_start / range_end: half-open task range in BASE units. Mirrors
--     job_keyspace_intervals.range_start/end.
--   - restore_point: hashcat's restore_point (already shipped by the agent
--     as KeyspaceProcessed per hashcat_executor.go:112-129). Nullable until
--     the first status update arrives. Used by recovery to decide truncation.
--   - last_activity_at: any liveness signal bumps this (progress update,
--     liveness ping, task_loading message, or new outfile crack). The
--     heartbeat sweep in §11 reads this column.
--   - failure_reason: optional text; e.g., "heartbeat timeout", "agent
--     disconnected", "preempted by higher priority".
--
-- The new columns are nullable so existing rows survive the migration
-- without backfill. New scheduler code writes the new columns and ignores
-- the old ones.

ALTER TABLE job_tasks
    ADD COLUMN scheduling_unit_id  UUID REFERENCES scheduling_units(id) ON DELETE SET NULL,
    ADD COLUMN range_start         BIGINT,
    ADD COLUMN range_end           BIGINT,
    ADD COLUMN restore_point       BIGINT,
    ADD COLUMN last_activity_at    TIMESTAMPTZ,
    ADD COLUMN failure_reason      TEXT;

-- Sanity invariants for the new columns. They are only enforced when the
-- new columns are populated (e.g., legacy rows with NULL stay legal).
ALTER TABLE job_tasks
    ADD CONSTRAINT new_range_positive
        CHECK (range_start IS NULL OR range_end IS NULL OR range_end > range_start),
    ADD CONSTRAINT new_range_non_negative
        CHECK (range_start IS NULL OR range_start >= 0),
    ADD CONSTRAINT new_restore_within_range
        CHECK (
            restore_point IS NULL
            OR (
                range_start IS NOT NULL
                AND range_end IS NOT NULL
                AND restore_point >= range_start
                AND restore_point <= range_end
            )
        );

-- Hot queries:
--   - "What tasks does scheduling_unit U have?" -> drives interval
--     reconciliation and unit-status updates.
--   - "Which tasks are quiet?" -> heartbeat sweep.
CREATE INDEX idx_job_tasks_scheduling_unit
    ON job_tasks (scheduling_unit_id)
    WHERE scheduling_unit_id IS NOT NULL;

CREATE INDEX idx_job_tasks_activity_status
    ON job_tasks (last_activity_at)
    WHERE status IN ('assigned', 'running');
