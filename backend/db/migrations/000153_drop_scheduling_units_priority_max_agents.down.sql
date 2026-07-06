-- Restore the priority and max_agents columns. Backfill from the
-- parent job_executions row so the rollback leaves data consistent
-- with the current job state. Best-effort: if an operator edited the
-- job post-migration, the recovered values still match what the job
-- currently says.

ALTER TABLE scheduling_units
    ADD COLUMN priority   INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN max_agents INTEGER NOT NULL DEFAULT 0;

UPDATE scheduling_units su
SET priority   = je.priority,
    max_agents = je.max_agents
FROM job_executions je
WHERE je.id = su.parent_job_id;

-- Recreate the partial dispatch index dropped by 000153 up.
CREATE INDEX IF NOT EXISTS idx_scheduling_units_dispatch
    ON scheduling_units (priority DESC, created_at)
    WHERE status IN ('pending', 'running');
