-- Migration: Add exclude_from_client_potfile column and fix enable_client_potfile default

-- 1. Add exclude_from_client_potfile column to hashlists table
-- When true, passwords from this hashlist are not added to client potfile
ALTER TABLE hashlists ADD COLUMN IF NOT EXISTS exclude_from_client_potfile BOOLEAN DEFAULT FALSE;

COMMENT ON COLUMN hashlists.exclude_from_client_potfile IS 'When true, cracked passwords from this hashlist are not added to client potfile';

-- 2. Change default for enable_client_potfile to TRUE
-- When the system-wide client potfiles feature is enabled, new clients should default to having their potfile enabled
ALTER TABLE clients ALTER COLUMN enable_client_potfile SET DEFAULT TRUE;

-- 3. Update existing clients to enable potfile
-- This ensures existing clients get the new default behavior
UPDATE clients SET enable_client_potfile = TRUE WHERE enable_client_potfile = FALSE;
