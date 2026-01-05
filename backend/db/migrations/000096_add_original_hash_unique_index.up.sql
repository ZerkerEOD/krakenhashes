-- Add unique index on original_hash for fast deduplication during bulk import
-- This enables ON CONFLICT (original_hash) DO NOTHING instead of slow NOT EXISTS subquery
--
-- First, we must deduplicate any existing duplicate hashes that may exist in databases
-- created before this migration. The deduplication strategy:
-- 1. Prefer cracked hashes as "survivors" (preserve crack data)
-- 2. Otherwise keep the oldest hash (smallest id)
-- 3. Update hashlist_hashes to point to survivors
-- 4. Delete duplicate hashes

-- Step 1: Create temp table to identify survivors (prefer cracked, then oldest)
CREATE TEMP TABLE hash_survivors AS
WITH duplicates AS (
    SELECT original_hash
    FROM hashes
    GROUP BY original_hash
    HAVING COUNT(*) > 1
),
ranked AS (
    SELECT
        h.id,
        h.original_hash,
        ROW_NUMBER() OVER (
            PARTITION BY h.original_hash
            ORDER BY h.is_cracked DESC, h.id ASC
        ) as rn
    FROM hashes h
    INNER JOIN duplicates d ON h.original_hash = d.original_hash
)
SELECT id as survivor_id, original_hash
FROM ranked
WHERE rn = 1;

-- Step 2: Create mapping from duplicate -> survivor
CREATE TEMP TABLE hash_mapping AS
SELECT h.id as duplicate_id, s.survivor_id
FROM hashes h
INNER JOIN hash_survivors s ON h.original_hash = s.original_hash
WHERE h.id != s.survivor_id;

-- Step 3: Update hashlist_hashes to point to survivors
UPDATE hashlist_hashes hh
SET hash_id = m.survivor_id
FROM hash_mapping m
WHERE hh.hash_id = m.duplicate_id;

-- Step 4: Delete duplicate hashlist_hashes entries (now pointing to same hash)
DELETE FROM hashlist_hashes a
USING hashlist_hashes b
WHERE a.hashlist_id = b.hashlist_id
  AND a.hash_id = b.hash_id
  AND a.ctid < b.ctid;

-- Step 5: Delete duplicate hashes
DELETE FROM hashes
WHERE id IN (SELECT duplicate_id FROM hash_mapping);

-- Step 6: Clean up temp tables
DROP TABLE IF EXISTS hash_mapping;
DROP TABLE IF EXISTS hash_survivors;

-- Step 7: Create the unique index
-- Note: CONCURRENTLY removed because it cannot run inside a transaction
-- and the migration framework wraps migrations in transactions
CREATE UNIQUE INDEX IF NOT EXISTS idx_hashes_original_hash_unique
ON hashes (original_hash);
