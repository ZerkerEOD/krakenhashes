-- Rollback: Remove Default Team protection and optionally the team itself
-- Note: This is destructive - only use if fully rolling back team feature

-- Drop the trigger first
DROP TRIGGER IF EXISTS tr_prevent_default_team_deletion ON teams;

-- Drop the function
DROP FUNCTION IF EXISTS prevent_default_team_deletion();

-- Optionally delete the Default Team (commented out for safety)
-- WARNING: This will orphan any clients assigned to Default Team
-- DELETE FROM teams WHERE id = '00000000-0000-0000-0000-000000000001';
