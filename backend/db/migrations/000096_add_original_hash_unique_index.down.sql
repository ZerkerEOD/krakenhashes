-- Remove the unique index on original_hash
DROP INDEX CONCURRENTLY IF EXISTS idx_hashes_original_hash_unique;
