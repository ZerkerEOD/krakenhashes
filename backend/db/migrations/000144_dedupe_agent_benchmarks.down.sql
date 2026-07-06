-- Restore the old NULLS-DISTINCT unique constraint. We do NOT reinsert
-- the deduped rows — historical entries can be rebuilt from
-- agent_benchmark_history if needed.

ALTER TABLE agent_benchmarks
    DROP CONSTRAINT IF EXISTS agent_benchmarks_agent_attack_hash_salt_key;

ALTER TABLE agent_benchmarks
    ADD CONSTRAINT agent_benchmarks_agent_attack_hash_salt_key
    UNIQUE (agent_id, attack_mode, hash_type, salt_count);
