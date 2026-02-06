-- Rollback: Remove admin_override_teams column from agents

-- Drop the index first
DROP INDEX IF EXISTS idx_agents_admin_override_teams;

-- Drop the column
ALTER TABLE agents DROP COLUMN IF EXISTS admin_override_teams;
