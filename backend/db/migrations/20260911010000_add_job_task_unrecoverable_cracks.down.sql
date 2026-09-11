-- Revert 20260911010000_add_job_task_unrecoverable_cracks.up.sql.
--
-- Dropping the column loses the record of which tasks lost cracks, and restores
-- the old behaviour: a permanently-rejected crack batch leaves the handshake
-- unsatisfiable but indistinguishable from one still in flight, so the task
-- waits out the stale-processing timeout (and, on a rented instance, bills for
-- it) before being abandoned anyway.

ALTER TABLE job_tasks
    DROP COLUMN IF EXISTS unrecoverable_crack_count;
