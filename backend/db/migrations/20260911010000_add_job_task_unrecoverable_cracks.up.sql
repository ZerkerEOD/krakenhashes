-- A crack batch the backend cannot persist made the task wait for it anyway.
--
-- HandleCrackBatch calls retryProcessCrackedHashes, and on a NON-TRANSIENT
-- error it returns early -- before IncrementReceivedCrackCount. So
-- received_crack_count never moves, CheckTaskReadyToComplete
-- (signaled AND received >= expected) can never be satisfied, and the task sits
-- in 'processing' until the stale backstop abandons it.
--
-- Retransmitting does not help. The retransmit path exists for batches lost IN
-- FLIGHT; a batch that arrived and was REJECTED will be rejected identically
-- every time, because the cause is deterministic -- a malformed job row, a
-- schema mismatch, a column the scan cannot read. The system therefore spends
-- its entire waiting budget on an outcome that was already decided.
--
-- Observed 2026-09-11 on the mock provider: a job_executions row carrying
-- wordlist_ids = '{}' (a JSON object where models.IDArray expects an array)
-- made every crack batch fail with "failed to get job execution". The task
-- reached expected=50 received=0 batches_complete_signaled=true and stopped
-- there. That particular trigger was a bad test fixture, but the waiting
-- behaviour it exposed is general: ANY non-retryable failure produces it.
--
-- WHAT IT COSTS. On-prem, a task stuck for the staleness timeout (30 min), and
-- up to 3x that while its agent stays connected -- the reachable-agent deferral
-- in checkForStaleProcessingTasks keeps extending it, because that deferral
-- assumes a connected agent might still retransmit something useful. On a
-- RENTED instance it is worse and it is money: the reaper's holdForUnsentWork
-- keeps the machine alive while a task is in 'processing', so the GPU bills
-- through cloud_crack_drain_grace_minutes (10) before teardown gives up and
-- records cloud_cracks_lost. The cracks are lost either way; the waiting buys
-- nothing.
--
-- WHAT THIS COLUMN CHANGES. Counting rejected cracks separately makes the
-- handshake's failure PROVABLE instead of merely slow: once the agent has
-- signalled it sent everything and received + unrecoverable >= expected, no
-- further batch can arrive, so the task is abandoned immediately rather than
-- waited out. Abandonment is the same terminal action the stale backstop
-- already takes -- truncate to the restore point, release the remaining
-- keyspace for re-dispatch, never write 'failed' -- so the work is redone and
-- the cracks are found again.
--
-- DELIBERATELY NOT FOLDED INTO received_crack_count. That counter means "cracks
-- we have actually persisted", and it is what the retransmit decision reads.
-- Incrementing it for cracks that were thrown away would let the task complete
-- looking perfectly healthy while silently missing passwords -- the exact
-- invisible loss cloud_cracks_lost was added to stop.

ALTER TABLE job_tasks
    ADD COLUMN IF NOT EXISTS unrecoverable_crack_count INT NOT NULL DEFAULT 0;

COMMENT ON COLUMN job_tasks.unrecoverable_crack_count IS
    'Cracks this task delivered that the backend could not persist after retries. Separate from received_crack_count, which counts only cracks actually stored: once batches_complete_signaled is set and received + unrecoverable >= expected, no further crack can arrive and the handshake is abandoned immediately instead of waiting out the stale-processing timeout. A non-zero value means passwords were lost and the keyspace was re-dispatched.';
