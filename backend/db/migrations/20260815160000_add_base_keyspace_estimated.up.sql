-- Track when base_keyspace is an upper-bound estimate rather than hashcat's own
-- --keyspace output.
--
-- Background: base_keyspace is the coordinate space chunk ranges are expressed
-- in, and unlike effective_keyspace it has no downstream corrector — the forced
-- agent benchmark refines effective_keyspace from progress[1] but never touches
-- base. When the --keyspace pre-flight times out on a very large wordlist we now
-- fall back to the stored wordlists.word_count so the job can still run, but that
-- count is an UPPER bound (hashcat skips over-length words), so the final chunk
-- can ask for a range hashcat cannot process. Flagging it lets the dispatcher
-- tolerate a short tail instead of stranding the job just below 100%.
--
-- Distinct from is_accurate_keyspace, which is about effective_keyspace and is
-- expected to be false until the benchmark lands.

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS base_keyspace_estimated BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE scheduling_units
    ADD COLUMN IF NOT EXISTS base_keyspace_estimated BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN job_executions.base_keyspace_estimated IS
    'TRUE when base_keyspace came from stored word counts because hashcat --keyspace timed out. Upper bound: the dispatcher must tolerate a short final gap.';

COMMENT ON COLUMN scheduling_units.base_keyspace_estimated IS
    'Mirrors job_executions.base_keyspace_estimated for the unit the scheduler dispatches against.';
