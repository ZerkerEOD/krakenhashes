-- Add salt_count column to agent_benchmarks table
-- This allows caching benchmarks per salt count for salted hash types
-- For salted hashes, speed varies significantly with salt count

ALTER TABLE agent_benchmarks ADD COLUMN salt_count INT;

-- Drop the old unique constraint
ALTER TABLE agent_benchmarks DROP CONSTRAINT IF EXISTS agent_benchmarks_agent_id_attack_mode_hash_type_key;

-- Add new unique constraint that includes salt_count
-- Using IS NOT DISTINCT FROM semantics via a partial unique index
-- This allows NULL salt_count values to be treated as equal (for non-salted hashes)
ALTER TABLE agent_benchmarks ADD CONSTRAINT agent_benchmarks_agent_attack_hash_salt_key
    UNIQUE (agent_id, attack_mode, hash_type, salt_count);

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_agent_benchmarks_lookup
    ON agent_benchmarks(agent_id, attack_mode, hash_type, salt_count);

-- Add comment for documentation
COMMENT ON COLUMN agent_benchmarks.salt_count IS 'Number of salts for salted hash types. NULL for non-salted hash types. Benchmark speed varies with salt count for salted hashes.';
