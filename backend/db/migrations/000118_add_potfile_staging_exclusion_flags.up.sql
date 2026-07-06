-- Migration: Add exclusion flags to potfile_staging for cascade logic

-- Add columns to track hashlist-level exclusions
-- These allow proper cascade: System → Client → Hashlist
ALTER TABLE potfile_staging ADD COLUMN IF NOT EXISTS exclude_from_global BOOLEAN DEFAULT FALSE;
ALTER TABLE potfile_staging ADD COLUMN IF NOT EXISTS exclude_from_client BOOLEAN DEFAULT FALSE;

COMMENT ON COLUMN potfile_staging.exclude_from_global IS 'When true, entry should not go to global potfile (from hashlist.exclude_from_potfile)';
COMMENT ON COLUMN potfile_staging.exclude_from_client IS 'When true, entry should not go to client potfile (from hashlist.exclude_from_client_potfile)';
