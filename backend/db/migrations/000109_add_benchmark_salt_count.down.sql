-- Remove salt_count column and restore original unique constraint

DROP INDEX IF EXISTS idx_agent_benchmarks_lookup;

ALTER TABLE agent_benchmarks DROP CONSTRAINT IF EXISTS agent_benchmarks_agent_attack_hash_salt_key;

ALTER TABLE agent_benchmarks ADD CONSTRAINT agent_benchmarks_agent_id_attack_mode_hash_type_key
    UNIQUE (agent_id, attack_mode, hash_type);

ALTER TABLE agent_benchmarks DROP COLUMN IF EXISTS salt_count;
