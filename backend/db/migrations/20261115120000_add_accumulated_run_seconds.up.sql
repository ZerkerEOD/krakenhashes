-- #138: Total running time must survive running -> pending -> running, and the
-- job's "Started At" must stay the FIRST start (not jump on every re-dispatch).
--
-- Two problems, one root cause. StartExecution set started_at = NOW() on every
-- pending -> running transition, so (a) the frontend's (now - started_at)
-- running time reset to the latest segment on each interrupt/resume, and
-- (b) the displayed "Started At" jumped forward every time the job picked up a
-- new task. (Per-task start times live on job_tasks and are unaffected.)
--
-- Fix, maintained by one table-level BEFORE UPDATE trigger that cannot be
-- bypassed by any status writer or exit path:
--   * started_at              -> the job's first start, set once and never
--                                overwritten thereafter (immutable).
--   * running_since           -> the CURRENT running segment's start; set when
--                                the job enters running, cleared when it leaves.
--                                The UI adds (now - running_since) live while
--                                running; the trigger folds it on exit.
--   * accumulated_run_seconds -> cumulative seconds spent running across all
--                                segments. Excludes pending/paused gaps and the
--                                post-cracking 'processing' phase.

ALTER TABLE job_executions
    ADD COLUMN accumulated_run_seconds BIGINT NOT NULL DEFAULT 0;
ALTER TABLE job_executions
    ADD COLUMN running_since TIMESTAMPTZ;

-- Best-effort backfill for jobs that already finished. Only the last known
-- segment can be reconstructed; segments discarded by past interrupts were
-- never recorded. Running time ends when cracking stops, so use
-- cracking_completed_at when set, else completed_at.
UPDATE job_executions
SET accumulated_run_seconds = GREATEST(
        0,
        FLOOR(EXTRACT(EPOCH FROM (COALESCE(cracking_completed_at, completed_at) - started_at)))::bigint
    )
WHERE started_at IS NOT NULL
  AND status IN ('completed', 'failed', 'cancelled');

-- A job that is running right now has its current segment anchored at the
-- existing started_at (the old column stored the last-segment start). Seed
-- running_since so the live segment keeps counting across the upgrade. Going
-- forward started_at is the first start and running_since is the segment start.
UPDATE job_executions
SET running_since = started_at
WHERE status = 'running' AND started_at IS NOT NULL;

CREATE OR REPLACE FUNCTION accumulate_job_run_seconds()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'running' AND NEW.status IS DISTINCT FROM 'running' THEN
        -- Leaving running: fold the current segment into the accumulator and
        -- close it. GREATEST guards against clock skew.
        IF OLD.running_since IS NOT NULL THEN
            NEW.accumulated_run_seconds := NEW.accumulated_run_seconds
                + GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (NOW() - OLD.running_since)))::bigint);
        END IF;
        NEW.running_since := NULL;
    ELSIF NEW.status = 'running' AND OLD.status IS DISTINCT FROM 'running' THEN
        -- Entering running: open a fresh segment, and stamp the first start once
        -- so started_at never moves on later re-dispatches (COALESCE keeps the
        -- original even if a caller, e.g. StartExecution, also sets started_at).
        NEW.running_since := NOW();
        NEW.started_at := COALESCE(OLD.started_at, NOW());
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE 'plpgsql';

CREATE TRIGGER trg_accumulate_job_run_seconds
    BEFORE UPDATE ON job_executions
    FOR EACH ROW
    EXECUTE FUNCTION accumulate_job_run_seconds();

COMMENT ON COLUMN job_executions.accumulated_run_seconds IS
    'Cumulative seconds spent running across all segments, folded in by trg_accumulate_job_run_seconds on each exit from running. Excludes paused/pending gaps and the post-cracking processing phase. The UI adds the live current segment (now - running_since) on top while status = running.';
COMMENT ON COLUMN job_executions.running_since IS
    'Start of the current running segment; set on entry to running and cleared on exit. NULL when not running. started_at, by contrast, is the immutable first start.';
