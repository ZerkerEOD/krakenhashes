-- Migration: Add admin_override_teams flag to agents table
-- Purpose: Controls whether agent uses explicit team assignments or inherits from owner
--
-- Behavior:
--   admin_override_teams = FALSE (default):
--     Agent inherits teams from its owner (agent.owner_id → user_teams)
--     When owner's teams change, agent's effective teams change automatically
--
--   admin_override_teams = TRUE:
--     Agent uses explicit entries in agent_teams table
--     Admin must manually manage agent's team assignments
--     Useful for shared agents or agents without owners

ALTER TABLE agents
ADD COLUMN admin_override_teams BOOLEAN NOT NULL DEFAULT FALSE;

-- Add comment for documentation
COMMENT ON COLUMN agents.admin_override_teams IS
    'When TRUE, agent uses explicit agent_teams entries for team membership. When FALSE (default), agent inherits teams from its owner (owner_id). Set to TRUE for shared agents or when admin needs explicit control over agent team access.';

-- Create index for queries that filter by this flag
-- Used when: "Find all agents with explicit team assignments"
CREATE INDEX idx_agents_admin_override_teams
ON agents(admin_override_teams)
WHERE admin_override_teams = TRUE;
