-- Restores the original full-text indexes.
--
-- This WILL FAIL if any hash longer than ~2.7 KB has been imported since the up
-- migration (the exact condition GH #88 fixed): btree cannot index those rows.
-- Delete or shorten such rows first if a rollback is genuinely required.

DROP INDEX IF EXISTS idx_hashes_uncracked;
CREATE INDEX IF NOT EXISTS idx_hashes_uncracked ON hashes (is_cracked, hash_value) WHERE is_cracked = FALSE;

DROP INDEX IF EXISTS idx_hashes_original_hash_md5_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_hashes_original_hash_unique ON hashes (original_hash);

DROP INDEX IF EXISTS idx_hashes_hash_value_md5;
CREATE INDEX IF NOT EXISTS idx_hashes_hash_value ON hashes (hash_value);
