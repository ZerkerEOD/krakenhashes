-- GH #88: long hashes (e.g. mode 13100 Kerberoast tickets with a ~4 KB edata
-- field) could not be imported:
--
--   pq: index row size 4464 exceeds btree version 4 maximum 2704
--       for index "idx_hashes_hash_value"
--
-- A btree entry is capped at roughly a third of a page (2704 bytes), and three
-- indexes on hashes carried the full hash text:
--
--   idx_hashes_hash_value            (hash_value)                  000015
--   idx_hashes_uncracked             (is_cracked, hash_value)      000074
--   idx_hashes_original_hash_unique  UNIQUE (original_hash)        000096
--
-- Each is replaced with one whose key is fixed-size:
--
--   * Equality lookups and the import dedup key on md5(...) instead of the raw
--     text. md5() is built in and IMMUTABLE, so this is an expression index -
--     no new column and no table rewrite. Queries compare md5(col) = md5($x)
--     AND col = $x, so the raw-text recheck keeps lookups exact.
--   * The UNIQUE index backing ON CONFLICT in BulkImportHashes becomes
--     UNIQUE (md5(original_hash)). Accepted risk: two DIFFERENT hashes with the
--     same md5 would be treated as duplicates on import (~1e-18 at realistic
--     volumes). INCLUDE (original_hash) cannot close that gap because INCLUDE
--     payload counts toward the same 2704-byte limit.
--   * hash_value only ever rode along in idx_hashes_uncracked for ordering; the
--     index exists to find the is_cracked = FALSE rows and join them by id.
--
-- Blocking (non-CONCURRENTLY) because migrate wraps each file in a transaction.
-- On a large hashes table this holds a write lock for the duration of the build.

DROP INDEX IF EXISTS idx_hashes_hash_value;
CREATE INDEX IF NOT EXISTS idx_hashes_hash_value_md5 ON hashes (md5(hash_value));

DROP INDEX IF EXISTS idx_hashes_original_hash_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_hashes_original_hash_md5_unique ON hashes (md5(original_hash));

DROP INDEX IF EXISTS idx_hashes_uncracked;
CREATE INDEX IF NOT EXISTS idx_hashes_uncracked ON hashes (id) WHERE is_cracked = FALSE;
