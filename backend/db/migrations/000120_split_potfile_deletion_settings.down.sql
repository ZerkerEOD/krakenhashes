-- Migration 000120 DOWN: Restore original column names

-- Rename column back to original name
ALTER TABLE clients RENAME COLUMN remove_from_client_potfile_on_hashlist_delete
    TO remove_passwords_on_hashlist_delete;

-- Drop the new global potfile column
ALTER TABLE clients DROP COLUMN IF EXISTS remove_from_global_potfile_on_hashlist_delete;
