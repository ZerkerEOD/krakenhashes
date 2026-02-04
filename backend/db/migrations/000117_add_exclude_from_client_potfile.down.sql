-- Rollback: Remove exclude_from_client_potfile column and revert enable_client_potfile default

-- 1. Remove exclude_from_client_potfile column from hashlists
ALTER TABLE hashlists DROP COLUMN IF EXISTS exclude_from_client_potfile;

-- 2. Revert default for enable_client_potfile to FALSE
ALTER TABLE clients ALTER COLUMN enable_client_potfile SET DEFAULT FALSE;

-- Note: We don't revert existing client data as that could cause data loss
