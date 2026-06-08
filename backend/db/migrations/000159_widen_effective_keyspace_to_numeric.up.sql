-- Widen EFFECTIVE keyspace columns from BIGINT to NUMERIC.
--
-- Effective keyspace = base_keyspace × rule_multiplier × salt_count. For the
-- large wordlists this project supports (100GB ≈ 8-10 billion lines) combined
-- with big rule files and salted hash types, that product exceeds BIGINT's max
-- (9,223,372,036,854,775,807) and silently wraps negative. NUMERIC (unbounded)
-- stores it exactly.
--
-- Base-unit columns stay BIGINT: base_keyspace, keyspace_start/end,
-- keyspace_processed, range_start/end, restore_point — these are wordlist
-- offsets bounded by hashcat's u64 --skip/--limit, never base × multiplier.
--
-- ALTER ... TYPE NUMERIC preserves NOT NULL, DEFAULT, and CHECK constraints
-- (e.g. scheduling_units.non_negative_keyspace). BIGINT -> NUMERIC is an
-- implicit cast; USING is spelled out for clarity.

-- job_executions
ALTER TABLE job_executions
    ALTER COLUMN effective_keyspace  TYPE NUMERIC USING effective_keyspace::numeric,
    ALTER COLUMN processed_keyspace  TYPE NUMERIC USING processed_keyspace::numeric,
    ALTER COLUMN dispatched_keyspace TYPE NUMERIC USING dispatched_keyspace::numeric;

-- job_tasks
ALTER TABLE job_tasks
    ALTER COLUMN effective_keyspace_start     TYPE NUMERIC USING effective_keyspace_start::numeric,
    ALTER COLUMN effective_keyspace_end       TYPE NUMERIC USING effective_keyspace_end::numeric,
    ALTER COLUMN effective_keyspace_processed TYPE NUMERIC USING effective_keyspace_processed::numeric,
    ALTER COLUMN chunk_actual_keyspace        TYPE NUMERIC USING chunk_actual_keyspace::numeric;

-- job_increment_layers
ALTER TABLE job_increment_layers
    ALTER COLUMN effective_keyspace  TYPE NUMERIC USING effective_keyspace::numeric,
    ALTER COLUMN processed_keyspace  TYPE NUMERIC USING processed_keyspace::numeric,
    ALTER COLUMN dispatched_keyspace TYPE NUMERIC USING dispatched_keyspace::numeric;

-- preset_jobs
ALTER TABLE preset_jobs
    ALTER COLUMN effective_keyspace TYPE NUMERIC USING effective_keyspace::numeric;

-- preset_increment_layers
ALTER TABLE preset_increment_layers
    ALTER COLUMN effective_keyspace TYPE NUMERIC USING effective_keyspace::numeric;

-- scheduling_units
ALTER TABLE scheduling_units
    ALTER COLUMN effective_keyspace TYPE NUMERIC USING effective_keyspace::numeric;

-- Salt-count multipliers: widen INT -> BIGINT. total_hashes feeds the salt
-- multiplier (effective = base × rules × total_hashes), and a hashlist can
-- exceed INT's 2.1e9 max; salt_count mirrors it for benchmark cache keys.
ALTER TABLE hashlists               ALTER COLUMN total_hashes TYPE BIGINT USING total_hashes::bigint;
ALTER TABLE agent_benchmarks        ALTER COLUMN salt_count   TYPE BIGINT USING salt_count::bigint;
ALTER TABLE agent_benchmark_history ALTER COLUMN salt_count   TYPE BIGINT USING salt_count::bigint;
