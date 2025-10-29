-- Optimize queries for uncracked hashes (agent hashlist downloads)
-- Partial index: only indexes rows where is_cracked = FALSE (smaller, faster)
-- This significantly improves performance for streaming uncracked hashes to agents
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hashes_uncracked
ON hashes(is_cracked, hash_value)
WHERE is_cracked = FALSE;
