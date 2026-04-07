-- Remove archived_at column and index
DROP INDEX IF EXISTS idx_hashlists_archived_at;
ALTER TABLE hashlists DROP COLUMN IF EXISTS archived_at;
