-- Rollback agent-level binary override

-- Drop index
DROP INDEX IF EXISTS idx_agents_binary_version;

-- Remove columns
ALTER TABLE agents
DROP COLUMN IF EXISTS binary_override,
DROP COLUMN IF EXISTS binary_version_id;
