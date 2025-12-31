-- Add unique index on original_hash for fast deduplication during bulk import
-- This enables ON CONFLICT (original_hash) DO NOTHING instead of slow NOT EXISTS subquery
-- CONCURRENTLY avoids locking the table during index creation (important for production)
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_hashes_original_hash_unique
ON hashes (original_hash);
