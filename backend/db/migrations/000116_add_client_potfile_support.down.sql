-- Rollback: Remove client-specific potfile and wordlist support

-- Remove system settings
DELETE FROM system_settings WHERE key IN (
    'client_potfiles_enabled',
    'remove_passwords_on_hashlist_delete_default'
);

-- Remove client columns
ALTER TABLE clients DROP COLUMN IF EXISTS enable_client_potfile;
ALTER TABLE clients DROP COLUMN IF EXISTS contribute_to_global_potfile;
ALTER TABLE clients DROP COLUMN IF EXISTS remove_passwords_on_hashlist_delete;

-- Drop tables
DROP TABLE IF EXISTS client_potfiles;
DROP TABLE IF EXISTS client_wordlists;

-- Remove client_id from potfile_staging
DROP INDEX IF EXISTS idx_potfile_staging_client_id;
ALTER TABLE potfile_staging DROP COLUMN IF EXISTS client_id;
