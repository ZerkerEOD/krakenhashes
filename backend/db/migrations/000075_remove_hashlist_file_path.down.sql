-- Restore file_path column for rollback
-- This allows reverting to file-based hashlist storage if needed
ALTER TABLE hashlists ADD COLUMN IF NOT EXISTS file_path VARCHAR(512);
