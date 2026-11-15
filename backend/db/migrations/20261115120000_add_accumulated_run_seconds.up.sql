-- #138: Total running time must survive running -> pending -> running.
--
-- The frontend used to compute running time as (now - started_at). But
-- started_at marks only the CURRENT running segment: InterruptExecution flips
-- a running job back to pending (leaving started_at alone), and the next
-- dispatch's StartExecution overwrites started_at with the new segment start.
-- Every prior segment was therefore discarded and the displayed total reset to
-- zero on each interrupt/resume cycle.
--
-- Fix: keep a cumulative accumulator of time actually spent in 'running', and
-- maintain it with a BEFORE UPDATE trigger on the table itself. A table-level
-- trigger cannot be bypassed: it covers every status writer (the repository
-- methods, the scheduler-v2 converter, and any future caller) and every exit
-- from running (-> pending, processing, paused, completed, failed, cancelled),
-- so no transition can silently forget to record its segment.

ALTER TABLE job_executions
    ADD COLUMN accumulated_run_seconds BIGINT NOT NULL DEFAULT 0;

-- Best-effort backfill for jobs that already finished. Only the last known
-- segment can be reconstructed (started_at -> when cracking stopped); segments
-- discarded by past interrupts were never recorded and cannot be recovered.
-- Running time excludes the post-cracking 'processing' phase, so we end the
-- segment at cracking_completed_at when it is set, else completed_at. Jobs that
-- are still live (pending/running/paused/processing/preparing) are left at the
-- default 0: a running job's current segment is added live by the UI, and
-- seeding it here would double-count.
UPDATE job_executions
SET accumulated_run_seconds = GREATEST(
        0,
        FLOOR(EXTRACT(EPOCH FROM (COALESCE(cracking_completed_at, completed_at) - started_at)))::bigint
    )
WHERE started_at IS NOT NULL
  AND status IN ('completed', 'failed', 'cancelled');

CREATE OR REPLACE FUNCTION accumulate_job_run_seconds()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'running' AND NEW.status IS DISTINCT FROM 'running' THEN
        -- Leaving running: fold the elapsed segment into the accumulator.
        -- GREATEST guards against clock skew producing a negative interval.
        IF OLD.started_at IS NOT NULL THEN
            NEW.accumulated_run_seconds := NEW.accumulated_run_seconds
                + GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (NOW() - OLD.started_at)))::bigint);
        END IF;
    ELSIF NEW.status = 'running' AND OLD.status IS DISTINCT FROM 'running' THEN
        -- Entering running from any other state begins a fresh segment, so the
        -- live (now - started_at) the UI adds measures only the current segment
        -- and never overlaps time already folded into the accumulator. This
        -- matches what StartExecution already does for pending -> running and
        -- extends it to any other entry path (e.g. task-recovery nudges).
        NEW.started_at := NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE 'plpgsql';

CREATE TRIGGER trg_accumulate_job_run_seconds
    BEFORE UPDATE ON job_executions
    FOR EACH ROW
    EXECUTE FUNCTION accumulate_job_run_seconds();

COMMENT ON COLUMN job_executions.accumulated_run_seconds IS
    'Cumulative seconds spent in the running state across all segments, folded in by trg_accumulate_job_run_seconds on each exit from running. Excludes paused/pending gaps and the post-cracking processing phase. The UI adds the live current segment on top while status = running.';
