-- Migration 000120: Split potfile deletion settings into global and client potfile
-- This migration separates the single remove_passwords_on_hashlist_delete setting
-- into two separate settings: one for global potfile and one for client potfile.

-- Add new column for global potfile removal setting
ALTER TABLE clients ADD COLUMN IF NOT EXISTS remove_from_global_potfile_on_hashlist_delete BOOLEAN DEFAULT NULL;

-- Rename existing column for clarity (this is for client potfile)
ALTER TABLE clients RENAME COLUMN remove_passwords_on_hashlist_delete
    TO remove_from_client_potfile_on_hashlist_delete;

-- Add comments for documentation
COMMENT ON COLUMN clients.remove_from_global_potfile_on_hashlist_delete
    IS 'NULL=use system default, true=always remove from global potfile on hashlist delete, false=never remove';
COMMENT ON COLUMN clients.remove_from_client_potfile_on_hashlist_delete
    IS 'NULL=use system default, true=always remove from client potfile on hashlist delete, false=never remove';
