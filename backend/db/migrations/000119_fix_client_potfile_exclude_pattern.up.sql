-- Migration 000119: Align client potfile columns with exclude pattern
-- Changes "enable" logic to "exclude" logic for consistency across all levels

-- 1. Rename enable_client_potfile to exclude_from_client_potfile
-- Current logic: enable_client_potfile=TRUE means "write to client potfile"
-- New logic: exclude_from_client_potfile=FALSE means "write to client potfile"
ALTER TABLE clients RENAME COLUMN enable_client_potfile TO exclude_from_client_potfile;

-- 2. Invert all existing values (TRUE→FALSE, FALSE→TRUE)
-- This maintains the same effective behavior after the rename
UPDATE clients SET exclude_from_client_potfile = NOT exclude_from_client_potfile;

-- 3. Change default to FALSE (not excluded = writes to client potfile by default)
ALTER TABLE clients ALTER COLUMN exclude_from_client_potfile SET DEFAULT FALSE;

-- 4. Update column comment
COMMENT ON COLUMN clients.exclude_from_client_potfile IS 'When true, cracks for this client are NOT added to client potfile';

-- 5. Remove redundant contribute_to_global_potfile (replaced by exclude_from_potfile)
ALTER TABLE clients DROP COLUMN IF EXISTS contribute_to_global_potfile;

-- 6. Update comment on existing exclude_from_potfile for clarity
COMMENT ON COLUMN clients.exclude_from_potfile IS 'When true, cracks for this client are NOT added to global potfile';
