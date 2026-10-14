DROP INDEX IF EXISTS idx_hashes_cracked_at;
ALTER TABLE hashes DROP COLUMN IF EXISTS cracked_at;
