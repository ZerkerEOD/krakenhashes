-- Rollback: Restore original column names and add back contribute_to_global_potfile

-- 1. Add back the redundant column
ALTER TABLE clients ADD COLUMN IF NOT EXISTS contribute_to_global_potfile BOOLEAN DEFAULT TRUE;

-- 2. Rename exclude_from_client_potfile back to enable_client_potfile
ALTER TABLE clients RENAME COLUMN exclude_from_client_potfile TO enable_client_potfile;

-- 3. Invert all values back (FALSE→TRUE, TRUE→FALSE)
UPDATE clients SET enable_client_potfile = NOT enable_client_potfile;

-- 4. Change default back to TRUE
ALTER TABLE clients ALTER COLUMN enable_client_potfile SET DEFAULT TRUE;

-- 5. Restore original comment
COMMENT ON COLUMN clients.enable_client_potfile IS 'When true, cracked passwords are added to client-specific potfile';
