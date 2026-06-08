-- Revert EFFECTIVE keyspace columns NUMERIC -> BIGINT, and salt-count
-- multipliers BIGINT -> INT.
--
-- WARNING: this is effectively forward-only once large jobs exist. The
-- NUMERIC -> BIGINT cast RAISES (numeric_value_out_of_range) — it does NOT
-- silently truncate — if any stored value exceeds 9,223,372,036,854,775,807,
-- and likewise BIGINT -> INT raises if total_hashes/salt_count exceeds
-- 2,147,483,647. Down-migrate only on a dataset known to fit the narrower
-- types.

ALTER TABLE agent_benchmark_history ALTER COLUMN salt_count   TYPE INT    USING salt_count::int;
ALTER TABLE agent_benchmarks        ALTER COLUMN salt_count   TYPE INT    USING salt_count::int;
ALTER TABLE hashlists               ALTER COLUMN total_hashes TYPE INT    USING total_hashes::int;

ALTER TABLE scheduling_units
    ALTER COLUMN effective_keyspace TYPE BIGINT USING effective_keyspace::bigint;

ALTER TABLE preset_increment_layers
    ALTER COLUMN effective_keyspace TYPE BIGINT USING effective_keyspace::bigint;

ALTER TABLE preset_jobs
    ALTER COLUMN effective_keyspace TYPE BIGINT USING effective_keyspace::bigint;

ALTER TABLE job_increment_layers
    ALTER COLUMN effective_keyspace  TYPE BIGINT USING effective_keyspace::bigint,
    ALTER COLUMN processed_keyspace  TYPE BIGINT USING processed_keyspace::bigint,
    ALTER COLUMN dispatched_keyspace TYPE BIGINT USING dispatched_keyspace::bigint;

ALTER TABLE job_tasks
    ALTER COLUMN effective_keyspace_start     TYPE BIGINT USING effective_keyspace_start::bigint,
    ALTER COLUMN effective_keyspace_end       TYPE BIGINT USING effective_keyspace_end::bigint,
    ALTER COLUMN effective_keyspace_processed TYPE BIGINT USING effective_keyspace_processed::bigint,
    ALTER COLUMN chunk_actual_keyspace        TYPE BIGINT USING chunk_actual_keyspace::bigint;

ALTER TABLE job_executions
    ALTER COLUMN effective_keyspace  TYPE BIGINT USING effective_keyspace::bigint,
    ALTER COLUMN processed_keyspace  TYPE BIGINT USING processed_keyspace::bigint,
    ALTER COLUMN dispatched_keyspace TYPE BIGINT USING dispatched_keyspace::bigint;
