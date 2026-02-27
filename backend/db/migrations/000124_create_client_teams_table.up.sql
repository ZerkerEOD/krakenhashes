-- Migration: Create client_teams junction table
-- Purpose: Associates clients with teams for access control
-- A client can belong to multiple teams (many-to-many relationship)

CREATE TABLE client_teams (
    -- Foreign key to clients table
    client_id UUID NOT NULL,

    -- Foreign key to teams table
    team_id UUID NOT NULL,

    -- Audit fields
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by UUID,  -- User who made the assignment (nullable for system assignments)

    -- Primary key prevents duplicate assignments
    PRIMARY KEY (client_id, team_id),

    -- Foreign key constraints with CASCADE delete
    -- When client is deleted, remove all team assignments
    CONSTRAINT fk_client_teams_client
        FOREIGN KEY (client_id)
        REFERENCES clients(id)
        ON DELETE CASCADE,

    -- When team is deleted, RESTRICT deletion until app code reassigns clients
    -- Using RESTRICT (not CASCADE) to prevent orphaned clients.
    -- App code must reassign clients to Default Team in a transaction BEFORE deleting the team.
    CONSTRAINT fk_client_teams_team
        FOREIGN KEY (team_id)
        REFERENCES teams(id)
        ON DELETE RESTRICT,

    -- Track who made the assignment (optional - allows NULL for migrations/system)
    CONSTRAINT fk_client_teams_assigned_by
        FOREIGN KEY (assigned_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

-- Index for efficient lookups by client_id
-- Used when: "What teams is this client assigned to?"
CREATE INDEX idx_client_teams_client_id ON client_teams(client_id);

-- Index for efficient lookups by team_id
-- Used when: "What clients are assigned to this team?"
CREATE INDEX idx_client_teams_team_id ON client_teams(team_id);

-- Index for audit queries by assigned_by
-- Used when: "What assignments did this user make?"
CREATE INDEX idx_client_teams_assigned_by ON client_teams(assigned_by);

-- Index for time-based queries
-- Used when: "What assignments were made recently?"
CREATE INDEX idx_client_teams_assigned_at ON client_teams(assigned_at DESC);

-- Add table comment for documentation
COMMENT ON TABLE client_teams IS 'Junction table associating clients with teams for multi-team access control';
COMMENT ON COLUMN client_teams.client_id IS 'Reference to the client being assigned to a team';
COMMENT ON COLUMN client_teams.team_id IS 'Reference to the team the client is assigned to';
COMMENT ON COLUMN client_teams.assigned_at IS 'Timestamp when the assignment was created';
COMMENT ON COLUMN client_teams.assigned_by IS 'User who created this assignment (NULL for system/migration assignments)';
