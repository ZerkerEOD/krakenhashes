-- Add agent-level binary override columns
-- This enables the priority hierarchy: Agent → Job → System Default

-- Add binary_version_id column with foreign key constraint
-- ON DELETE SET NULL ensures that when a binary is deleted, agents fall back to job/default
ALTER TABLE agents
ADD COLUMN binary_version_id INTEGER REFERENCES binary_versions(id) ON DELETE SET NULL,
ADD COLUMN binary_override BOOLEAN DEFAULT FALSE NOT NULL;

-- Create index for performance on binary_version_id lookups
CREATE INDEX idx_agents_binary_version ON agents(binary_version_id) WHERE binary_version_id IS NOT NULL;

-- Add column comments for documentation
COMMENT ON COLUMN agents.binary_version_id IS 'Optional override binary version for this agent. If set with binary_override=true, takes priority over job binary. Automatically NULLed when binary is deleted.';
COMMENT ON COLUMN agents.binary_override IS 'Indicates if binary_version_id was manually set by user (true) or should use job/default (false). Both false and NULL binary_version_id means use job/default.';
